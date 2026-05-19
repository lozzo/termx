package termx

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/termx-proto/wire"
	"github.com/lozzow/termx/termx-shared/transport/memory"
	vterm "github.com/lozzow/termx/termx-vterm/vterm"
)

func TestServerCreateListTagsSubscribeSnapshotAndRemoval(t *testing.T) {
	ctx := context.Background()
	srv := NewServer(
		WithDefaultKeepAfterExit(200*time.Millisecond),
		WithDefaultScrollback(128),
	)

	eventsCtx, cancelEvents := context.WithCancel(ctx)
	defer cancelEvents()
	events := srv.Events(eventsCtx)

	term, err := srv.Create(ctx, CreateOptions{
		Name:    "dev",
		Command: []string{"bash", "--noprofile", "--norc"},
		Tags:    map[string]string{"group": "dev"},
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted in this environment: %v", err)
		}
		t.Fatalf("create failed: %v", err)
	}

	select {
	case evt := <-events:
		if evt.Type != EventTerminalCreated || evt.TerminalID != term.ID {
			t.Fatalf("unexpected created event: %#v", evt)
		}
		list, err := srv.List(ctx, ListOptions{})
		if err != nil {
			t.Fatalf("list after create event failed: %v", err)
		}
		if len(list) != 1 || list[0].ID != term.ID {
			t.Fatalf("created terminal was not visible when create event arrived: %#v", list)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for create event")
	}

	list, err := srv.List(ctx, ListOptions{Tags: map[string]string{"group": "dev"}})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(list) != 1 || list[0].ID != term.ID {
		t.Fatalf("unexpected list result: %#v", list)
	}

	streamCtx, cancelStream := context.WithCancel(ctx)
	defer cancelStream()
	stream, err := srv.Subscribe(streamCtx, term.ID)
	if err != nil {
		t.Fatalf("subscribe failed: %v", err)
	}

	if err := srv.SendKeys(ctx, term.ID, "echo integration", "Enter"); err != nil {
		t.Fatalf("send keys failed: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		msg := <-stream
		if streamMessageContainsText(msg, 80, 24, "integration") {
			break
		}
	}

	snap, err := srv.Snapshot(ctx, term.ID, SnapshotOptions{ScrollbackLimit: 50})
	if err != nil {
		t.Fatalf("snapshot failed: %v", err)
	}
	if !snapshotContains(snap, "integration") {
		t.Fatalf("snapshot missing output: %#v", snap)
	}

	if err := srv.SetTags(ctx, term.ID, map[string]string{"status": "idle"}); err != nil {
		t.Fatalf("set tags failed: %v", err)
	}
	got, err := srv.Get(ctx, term.ID)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if got.Tags["group"] != "dev" || got.Tags["status"] != "idle" {
		t.Fatalf("unexpected tags: %#v", got.Tags)
	}

	if err := srv.Kill(ctx, term.ID); err != nil {
		t.Fatalf("kill failed: %v", err)
	}

	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		msg := <-stream
		if msg.Type == StreamClosed {
			goto removedCheck
		}
	}
	t.Fatal("timed out waiting for stream close")

removedCheck:
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_, err := srv.Get(ctx, term.ID)
		if errors.Is(err, ErrNotFound) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}

	t.Fatal("terminal was not auto-removed")
}

func TestServerListReturnsDistinctTerminalIDs(t *testing.T) {
	srv := NewServer()
	srv.terminals["1"] = &Terminal{
		id:        "1",
		name:      "one",
		command:   []string{"/bin/bash"},
		tags:      map[string]string{"group": "test"},
		size:      Size{Cols: 80, Rows: 24},
		state:     StateRunning,
		createdAt: time.Unix(1, 0).UTC(),
	}
	srv.terminals["3"] = &Terminal{
		id:        "3",
		name:      "three",
		command:   []string{"/bin/bash"},
		tags:      map[string]string{"group": "test"},
		size:      Size{Cols: 80, Rows: 24},
		state:     StateRunning,
		createdAt: time.Unix(3, 0).UTC(),
	}

	list, err := srv.List(context.Background())
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected two terminals, got %#v", list)
	}
	if list[0].ID != "1" || list[1].ID != "3" {
		t.Fatalf("expected distinct sorted terminal IDs [1 3], got [%s %s]", list[0].ID, list[1].ID)
	}
}

func TestServerHistorySurvivesServerRestart(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := newTerminalGridStore(root, "persist-server-1")
	if err != nil {
		t.Fatalf("new grid store: %v", err)
	}
	for i := 0; i < 12; i++ {
		rows := [][]vterm.Cell{{{Content: fmt.Sprintf("disk-%02d", i), Width: 1}}}
		if err := store.AppendRows(rows); err != nil {
			t.Fatalf("append grid row %d: %v", i, err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close grid store: %v", err)
	}

	restarted := NewServer(WithGridRoot(root), WithDefaultSize(12, 2), WithDefaultScrollback(3))
	replay, err := restarted.HistoryReplay(ctx, "persist-server-1", HistoryReplayOptions{BeforeOffset: 0, Limit: 4})
	if err != nil {
		t.Fatalf("history replay after restart failed: %v", err)
	}
	if plain := stripANSIForTest(replay.Replay); !strings.Contains(plain, "disk-11") {
		t.Fatalf("expected replay after restart to include persisted output, got %q", plain)
	}
	screenOnly, err := restarted.Snapshot(ctx, "persist-server-1")
	if err != nil {
		t.Fatalf("screen-only snapshot after restart failed: %v", err)
	}
	if len(screenOnly.Scrollback) != 0 || screenOnly.ScrollbackTotal != 12 || !screenOnly.ScrollbackHasMore {
		t.Fatalf("expected restarted screen-only snapshot with disk history metadata, got rows=%d total=%d has_more=%v", len(screenOnly.Scrollback), screenOnly.ScrollbackTotal, screenOnly.ScrollbackHasMore)
	}
	if len(screenOnly.Screen.Cells) == 0 || !strings.Contains(rowToString(screenOnly.Screen.Cells[len(screenOnly.Screen.Cells)-1]), "disk-11") {
		t.Fatalf("expected persisted tail on screen-only snapshot after restart, got %#v", screenOnly.Screen.Cells)
	}
	snap, err := restarted.Snapshot(ctx, "persist-server-1", SnapshotOptions{ScrollbackLimit: 6})
	if err != nil {
		t.Fatalf("snapshot after restart failed: %v", err)
	}
	if !snapshotContains(snap, "disk-11") {
		t.Fatalf("expected snapshot after restart to include persisted output, got %#v", snap)
	}
	if len(snap.Screen.Cells) == 0 || !strings.Contains(rowToString(snap.Screen.Cells[len(snap.Screen.Cells)-1]), "disk-11") {
		t.Fatalf("expected persisted tail on visible screen after restart, got %#v", snap.Screen.Cells)
	}
	viewport, err := restarted.GridViewport(ctx, "persist-server-1", GridViewportOptions{ScrollbackOffset: 6, ScrollbackLimit: 4, Cols: 12})
	if err != nil {
		t.Fatalf("grid viewport after restart failed: %v", err)
	}
	if viewport.ScrollbackTotal != 12 || !viewport.ScrollbackHasMore {
		t.Fatalf("unexpected viewport metadata after restart: %#v", viewport)
	}
	if len(viewport.Rows) == 0 || !strings.Contains(protocolTestRowToString(viewport.Rows[0].DecodeCells()), "disk-02") {
		t.Fatalf("expected older persisted rows from viewport, got %#v", viewport.Rows)
	}
}

func TestServerGridViewportFromStoreUsesDefaultCanonicalCols(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := newTerminalGridStore(root, "persist-canonical-1")
	if err != nil {
		t.Fatalf("new grid store: %v", err)
	}
	if err := store.appendRows([]terminalGridRow{
		{cells: []vterm.Cell{{Content: "a", Width: 1}, {Content: "b", Width: 1}, {Content: "c", Width: 1}, {Content: "d", Width: 1}}, wrapped: true},
		{cells: []vterm.Cell{{Content: "e", Width: 1}}},
	}); err != nil {
		t.Fatalf("append grid rows: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close grid store: %v", err)
	}

	srv := NewServer(WithGridRoot(root), WithDefaultSize(5, 2))
	viewport, err := srv.GridViewport(ctx, "persist-canonical-1", GridViewportOptions{ScrollbackLimit: 10, Cols: 2})
	if err != nil {
		t.Fatalf("grid viewport after restart: %v", err)
	}
	if viewport.Size.Cols != 5 {
		t.Fatalf("expected persisted viewport to report default canonical cols 5, got %d", viewport.Size.Cols)
	}
	gotRows := make([]string, 0, len(viewport.Rows))
	for _, row := range viewport.Rows {
		gotRows = append(gotRows, protocolTestRowToString(row.DecodeCells()))
	}
	if !reflect.DeepEqual(gotRows, []string{"abcde"}) {
		t.Fatalf("expected persisted viewport to ignore caller cols and reflow at default canonical width, got %#v", gotRows)
	}
}

func TestServerHistoryFrameUsesStructuredGridViewport(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	root := t.TempDir()
	srv := NewServer(WithGridRoot(root), WithDefaultSize(2, 2), WithDefaultScrollback(100))
	created, err := srv.Create(ctx, CreateOptions{
		ID:      "history-frame-1",
		Command: []string{"sleep", "60"},
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted in this environment: %v", err)
		}
		t.Fatalf("create terminal failed: %v", err)
	}
	term, err := srv.getTerminal(created.ID)
	if err != nil {
		t.Fatalf("get terminal failed: %v", err)
	}
	if term.grid == nil {
		t.Fatal("expected terminal grid store")
	}
	if err := term.grid.appendRows([]terminalGridRow{
		{cells: []vterm.Cell{{Content: "A", Width: 1}}, wrapped: true},
		{cells: []vterm.Cell{{Content: "B", Width: 1}}},
		{cells: []vterm.Cell{{Content: "C", Width: 1}}},
	}); err != nil {
		t.Fatalf("append grid rows: %v", err)
	}

	serverConn, clientConn := memory.NewPair()
	defer clientConn.Close()
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.handleTransportScoped(ctx, serverConn, "memory", transportScope{})
	}()

	sendClientFrame := func(channel uint16, typ uint8, payload []byte) {
		t.Helper()
		frame, err := wire.EncodeFrame(channel, typ, payload)
		if err != nil {
			t.Fatalf("encode frame failed: %v", err)
		}
		if err := clientConn.Send(frame); err != nil {
			t.Fatalf("send frame failed: %v", err)
		}
	}
	helloPayload, err := protocol.EncodeHelloPayload(protocol.Hello{Version: wire.Version, Client: "test"})
	if err != nil {
		t.Fatalf("encode hello payload failed: %v", err)
	}
	sendClientFrame(0, wire.TypeHello, helloPayload)
	ch, typ, payload := recvDecodedFrame(t, clientConn)
	if ch != 0 || typ != wire.TypeHello {
		t.Fatalf("unexpected hello response channel=%d type=%d", ch, typ)
	}
	req := protocol.Request{
		ID:     1,
		Method: "attach",
		Params: mustProtoParams(t, protocol.AttachParams{TerminalID: "history-frame-1", Mode: string(ModeCollaborator)}),
	}
	requestPayload, err := protocol.EncodeRequestPayload(req)
	if err != nil {
		t.Fatalf("encode attach request failed: %v", err)
	}
	sendClientFrame(0, wire.TypeRequest, requestPayload)
	ch, typ, payload = recvDecodedFrame(t, clientConn)
	if ch != 0 || typ != wire.TypeResponse {
		t.Fatalf("unexpected attach response channel=%d type=%d", ch, typ)
	}
	responseID, _, err := protocol.DecodeBinaryResponsePayload(payload)
	if err != nil {
		t.Fatalf("decode attach response failed: %v", err)
	}
	if responseID != 1 {
		t.Fatalf("unexpected attach response id %d", responseID)
	}

	sendClientFrame(1, wire.TypeHistoryRequest, wire.EncodeHistoryRequestPayload(0, 2))
	for {
		ch, typ, payload = recvDecodedFrame(t, clientConn)
		if ch == 1 && typ == wire.TypeHistoryReplay {
			break
		}
	}
	rows, hasMore, viewportPayload, err := wire.DecodeHistoryReplayPayload(payload)
	if err != nil {
		t.Fatalf("decode history payload failed: %v", err)
	}
	if rows != 3 || hasMore {
		t.Fatalf("unexpected history metadata rows=%d has_more=%v", rows, hasMore)
	}
	viewport, err := protocol.DecodeGridViewportPayload(viewportPayload)
	if err != nil {
		t.Fatalf("decode history grid viewport failed: %v", err)
	}
	if len(viewport.Rows) != 2 {
		t.Fatalf("expected structured wrapped rows, got %#v", viewport)
	}
	if got := protocolTestRowToString(viewport.Rows[0].DecodeCells()) + protocolTestRowToString(viewport.Rows[1].DecodeCells()); got != "ABC" {
		t.Fatalf("unexpected structured history rows %q", got)
	}

	cancel()
	if err := clientConn.Close(); err != nil {
		t.Fatalf("close client conn failed: %v", err)
	}
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("server transport failed: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for server transport")
	}
}

func protocolTestRowToString(row []protocol.Cell) string {
	var b strings.Builder
	for _, cell := range row {
		b.WriteString(cell.Content)
	}
	return strings.TrimRight(b.String(), " ")
}

func TestServerDoesNotSpecialCaseRemoteRPCMethods(t *testing.T) {
	server := NewServer()
	for _, method := range []string{
		"remote.status",
		"remote.pair.start",
		"remote.local.enable",
		"remote.local.status",
		"remote.local.disable",
	} {
		t.Run(method, func(t *testing.T) {
			allocator := protocol.NewChannelAllocator()
			attachments := make(map[uint16]*sessionAttachment)
			var attachmentsMu sync.RWMutex
			result, code, err := server.handleRequest(
				context.Background(),
				"test",
				nil,
				allocator,
				attachments,
				&attachmentsMu,
				transportScope{},
				protocol.Request{ID: 1, Method: method, Params: mustProtoParams(t, struct{}{})}, func(uint16, uint8, []byte) error { return nil },
			)
			if err == nil {
				t.Fatalf("expected %s to be rejected, got code=%d result=%s", method, code, string(result))
			}
			if code != 400 || !strings.Contains(err.Error(), "unsupported method") {
				t.Fatalf("expected generic unsupported method for %s, got code=%d err=%v", method, code, err)
			}
		})
	}
}
