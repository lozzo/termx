package termxcorev2

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/termx-proto/wire"
	"github.com/lozzow/termx/termx-shared/transport/memory"
)

func TestProtocolServiceCreateListMetadataRestartRemove(t *testing.T) {
	server, client, closeClient := newProtocolClient(t)
	defer closeClient()

	created, err := client.Create(context.Background(), protocol.CreateParams{
		ID:      "term-1",
		Name:    "demo",
		Tags:    map[string]string{"role": "test"},
		Command: []string{"shell"},
		Size:    protocol.Size{Cols: 12, Rows: 4},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.TerminalID != "term-1" || created.State != string(TerminalStateRunning) {
		t.Fatalf("unexpected create result %#v", created)
	}

	list, err := client.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list.Terminals) != 1 || list.Terminals[0].Name != "demo" || list.Terminals[0].Tags["role"] != "test" {
		t.Fatalf("unexpected list result %#v", list)
	}

	if err := client.SetMetadata(context.Background(), "term-1", "renamed", map[string]string{"role": "updated"}); err != nil {
		t.Fatalf("set metadata: %v", err)
	}
	var info protocol.TerminalInfo
	if err := client.Call(context.Background(), "get", protocol.GetParams{TerminalID: "term-1"}, &info); err != nil {
		t.Fatalf("get: %v", err)
	}
	if info.Name != "renamed" || info.Tags["role"] != "updated" {
		t.Fatalf("unexpected terminal metadata %#v", info)
	}

	if err := client.Restart(context.Background(), "term-1"); err != nil {
		t.Fatalf("restart: %v", err)
	}
	if process := serverProcess(t, server, "term-1"); process == nil {
		t.Fatal("expected restarted process")
	}

	if err := client.Remove(context.Background(), "term-1"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := server.GetTerminal("term-1"); !errors.Is(err, ErrTerminalNotFound) {
		t.Fatalf("expected removed terminal, got %v", err)
	}
}

func TestProtocolServiceHistoryWindowUsesCoreTruth(t *testing.T) {
	server, client, closeClient := newProtocolClient(t)
	defer closeClient()

	if _, err := client.Create(context.Background(), protocol.CreateParams{ID: "term-1", Command: []string{"shell"}, Size: protocol.Size{Cols: 10, Rows: 3}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "one\ntwo\nthree"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}

	latest, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID: "term-1",
		Cols:       10,
		Limit:      2,
	})
	if err != nil {
		t.Fatalf("latest history.window: %v", err)
	}
	if latest.Op != protocol.HistoryWindowReplace || latest.Size.Cols != 10 || latest.LogicalTotal != 2 || len(latest.Rows) != 2 {
		t.Fatalf("unexpected latest window %#v", latest)
	}
	if rowText(latest.Rows[0]) != "two" || rowText(latest.Rows[1]) != "three" {
		t.Fatalf("unexpected latest rows %#v", latest.Rows)
	}
	if latest.RowLineIDs[0] == 0 || latest.RowInLine[0] != 0 || latest.Generation == 0 || latest.Token == "" {
		t.Fatalf("expected line mapping, generation and token, got %#v", latest)
	}
	if latest.RowOwnership[0] != protocol.RowOwnershipPersisted || latest.RowOwnership[1] != protocol.RowOwnershipLiveTailLive {
		t.Fatalf("unexpected row ownership %#v", latest.RowOwnership)
	}

	older, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID:      "term-1",
		Cols:            10,
		Limit:           1,
		CursorValid:     latest.CursorValid,
		BeforeLineID:    latest.CursorLineID,
		BeforeRowInLine: latest.CursorRow,
		Token:           latest.Token,
		Generation:      latest.Generation,
	})
	if err != nil {
		t.Fatalf("older history.window: %v", err)
	}
	if older.Op != protocol.HistoryWindowPrepend || older.Token != latest.Token || len(older.Rows) != 1 || rowText(older.Rows[0]) != "one" {
		t.Fatalf("unexpected older window %#v", older)
	}

	_, err = client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID:          "term-1",
		Cols:                10,
		Limit:               1,
		CursorValid:         true,
		BeforeLineID:        latest.CursorLineID,
		BeforeRowInLine:     latest.CursorRow,
		Token:               "stale-token",
		Generation:          latest.Generation,
		BoundaryFirstLineID: latest.FirstLineID,
		BoundaryLastLineID:  latest.LastLineID,
	})
	if err == nil || !strings.Contains(err.Error(), ErrStaleHistoryWindow.Error()) {
		t.Fatalf("expected stale history window error, got %v", err)
	}
	_, err = client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID:          "term-1",
		Cols:                11,
		Limit:               1,
		CursorValid:         true,
		BeforeLineID:        latest.CursorLineID,
		BeforeRowInLine:     latest.CursorRow,
		Token:               latest.Token,
		Generation:          latest.Generation,
		BoundaryFirstLineID: latest.FirstLineID,
		BoundaryLastLineID:  latest.LastLineID,
	})
	if err == nil || !strings.Contains(err.Error(), ErrStaleHistoryWindow.Error()) {
		t.Fatalf("expected cols stale history window error, got %v", err)
	}
}

func TestProtocolServiceAttachRoutesInputResizeAndEvents(t *testing.T) {
	server, client, closeClient := newProtocolClient(t)
	defer closeClient()

	if _, err := client.Create(context.Background(), protocol.CreateParams{ID: "term-1", Command: []string{"shell"}, Size: protocol.Size{Cols: 10, Rows: 3}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	events, err := client.Events(context.Background(), protocol.EventsParams{
		TerminalID: "term-1",
		Types:      []protocol.EventType{protocol.EventTerminalResized},
	})
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	attach, err := client.AttachWithOptions(context.Background(), protocol.AttachParams{
		TerminalID:   "term-1",
		Mode:         "collaborator",
		ResizePolicy: protocol.ResizePolicyOwner,
		SurfaceID:    "surface-1",
		ViewID:       "view-1",
	})
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if attach.Channel == 0 || attach.ResizeControl == nil || !attach.ResizeControl.CanResize {
		t.Fatalf("unexpected attach result %#v", attach)
	}

	if err := client.Input(context.Background(), attach.Channel, []byte("echo hi\n")); err != nil {
		t.Fatalf("input: %v", err)
	}
	if err := client.Resize(context.Background(), attach.Channel, 20, 5); err != nil {
		t.Fatalf("resize frame: %v", err)
	}
	if _, err := client.EnsureResize(context.Background(), protocol.EnsureResizeParams{
		TerminalID: "missing",
		Channel:    attach.Channel,
		Cols:       30,
		Rows:       10,
	}); err == nil {
		t.Fatal("expected ensure_resize to reject mismatched terminal/channel")
	}
	process := waitForProcessTraffic(t, server, "term-1", 1, 1)
	inputs, resizes, _, _ := process.snapshot()
	if string(inputs[0]) != "echo hi\n" {
		t.Fatalf("input did not reach process %#v", inputs)
	}
	if resizes[0] != (Size{Cols: 20, Rows: 5}) {
		t.Fatalf("resize did not reach process %#v", resizes)
	}

	evt := requireProtocolEvent(t, events)
	if evt.Type != protocol.EventTerminalResized || evt.Resized == nil || evt.Resized.NewSize != (protocol.Size{Cols: 20, Rows: 5}) {
		t.Fatalf("unexpected resize event %#v", evt)
	}
}

func TestProtocolServiceStorageMethodsAndEvents(t *testing.T) {
	_, client, closeClient := newProtocolClient(t)
	defer closeClient()

	events, err := client.Events(context.Background(), protocol.EventsParams{
		Types:            []protocol.EventType{protocol.EventStorageChanged},
		StorageAppID:     "termx-tui-v3",
		StorageScope:     protocol.StorageScopePublic,
		StorageOwnerID:   "workspace-main",
		StorageKeyPrefix: "workbench/",
	})
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	entry, err := client.StoragePut(context.Background(), protocol.StoragePutParams{
		AppID:   "termx-tui-v3",
		Scope:   protocol.StorageScopePublic,
		OwnerID: "workspace-main",
		Key:     "workbench/root",
		Value:   []byte("v1"),
	})
	if err != nil {
		t.Fatalf("storage put: %v", err)
	}
	if entry.Version != 1 || string(entry.Value) != "v1" {
		t.Fatalf("unexpected put entry %#v", entry)
	}
	event := requireProtocolEvent(t, events)
	if event.Type != protocol.EventStorageChanged || event.Storage == nil || event.Storage.Key != "workbench/root" || event.Storage.Version != 1 || event.Storage.Op != "put" {
		t.Fatalf("unexpected storage event %#v", event)
	}
	got, err := client.StorageGet(context.Background(), protocol.StorageGetParams{
		AppID:   "termx-tui-v3",
		Scope:   protocol.StorageScopePublic,
		OwnerID: "workspace-main",
		Key:     "workbench/root",
	})
	if err != nil {
		t.Fatalf("storage get: %v", err)
	}
	if got.Version != 1 || string(got.Value) != "v1" {
		t.Fatalf("unexpected get entry %#v", got)
	}
	if _, err := client.StoragePut(context.Background(), protocol.StoragePutParams{
		AppID:           "termx-tui-v3",
		Scope:           protocol.StorageScopePublic,
		OwnerID:         "workspace-main",
		Key:             "workbench/root",
		Value:           []byte("stale"),
		CheckVersion:    true,
		ExpectedVersion: 99,
	}); err == nil || !strings.Contains(err.Error(), ErrStorageVersionConflict.Error()) {
		t.Fatalf("expected storage conflict, got %v", err)
	}
	list, err := client.StorageList(context.Background(), protocol.StorageListParams{
		AppID:   "termx-tui-v3",
		Scope:   protocol.StorageScopePublic,
		OwnerID: "workspace-main",
		Prefix:  "workbench/",
	})
	if err != nil {
		t.Fatalf("storage list: %v", err)
	}
	if len(list.Entries) != 1 || list.Entries[0].Key != "workbench/root" {
		t.Fatalf("unexpected list %#v", list)
	}
	deleted, err := client.StorageDelete(context.Background(), protocol.StorageDeleteParams{
		AppID:           "termx-tui-v3",
		Scope:           protocol.StorageScopePublic,
		OwnerID:         "workspace-main",
		Key:             "workbench/root",
		CheckVersion:    true,
		ExpectedVersion: 1,
	})
	if err != nil {
		t.Fatalf("storage delete: %v", err)
	}
	if !deleted.Deleted || deleted.Version != 2 {
		t.Fatalf("unexpected delete result %#v", deleted)
	}
}

func TestProtocolServiceWorkbenchMethodsAndEvents(t *testing.T) {
	_, client, closeClient := newProtocolClient(t)
	defer closeClient()

	events, err := client.Events(context.Background(), protocol.EventsParams{
		Types:       []protocol.EventType{protocol.EventWorkbenchChanged},
		WorkbenchID: "workspace-main",
	})
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	snapshot, err := client.WorkbenchGet(context.Background(), protocol.WorkbenchGetParams{})
	if err != nil {
		t.Fatalf("workbench get: %v", err)
	}
	if snapshot.Version != 1 || snapshot.ActiveWorkspaceID != "workspace-main" {
		t.Fatalf("unexpected initial snapshot %#v", snapshot)
	}
	result, err := client.WorkbenchApply(context.Background(), protocol.WorkbenchMutateParams{
		Action:          protocol.WorkbenchMutationPaneSplit,
		WorkspaceID:     "workspace-main",
		TabID:           "tab-main",
		PaneID:          "pane-main",
		TargetID:        "pane-two",
		Name:            "logs",
		SplitDirection:  protocol.WorkbenchSplitHorizontal,
		CheckVersion:    true,
		ExpectedVersion: snapshot.Version,
	})
	if err != nil {
		t.Fatalf("workbench apply: %v", err)
	}
	if result.Snapshot.Version != 2 || result.ResourceID != "pane-two" {
		t.Fatalf("unexpected apply result %#v", result)
	}
	event := requireProtocolEvent(t, events)
	if event.Type != protocol.EventWorkbenchChanged || event.Workbench == nil || event.Workbench.ResourceID != "pane-two" || event.Workbench.Version != 2 {
		t.Fatalf("unexpected workbench event %#v", event)
	}
	if _, err := client.WorkbenchApply(context.Background(), protocol.WorkbenchMutateParams{
		Action:          protocol.WorkbenchMutationPaneRename,
		WorkspaceID:     "workspace-main",
		TabID:           "tab-main",
		PaneID:          "pane-two",
		Name:            "stale",
		CheckVersion:    true,
		ExpectedVersion: 1,
	}); err == nil || !strings.Contains(err.Error(), ErrWorkbenchVersionConflict.Error()) {
		t.Fatalf("expected workbench conflict, got %v", err)
	}
}

func TestProtocolServiceSnapshotReturnsLiveSurfaceRows(t *testing.T) {
	server, client, closeClient := newProtocolClient(t)
	defer closeClient()

	if _, err := client.Create(context.Background(), protocol.CreateParams{ID: "term-1", Command: []string{"shell"}, Size: protocol.Size{Cols: 12, Rows: 4}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "alpha\n\x1b[31mERR\x1b[0m ok\r\x1b[32mOK\x1b[0m"); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	snapshot, err := client.Snapshot(context.Background(), "term-1", 0, 2)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snapshot.TerminalID != "term-1" || snapshot.Size != (protocol.Size{Cols: 12, Rows: 4}) {
		t.Fatalf("unexpected snapshot metadata %#v", snapshot)
	}
	if len(snapshot.Screen.Cells) != 4 {
		t.Fatalf("expected size-bound live screen rows, got %#v", snapshot.Screen.Cells)
	}
	got := []string{cellsText(snapshot.Screen.Cells[0]), cellsText(snapshot.Screen.Cells[1])}
	if got[0] != "alpha       " || !strings.HasPrefix(got[1], "OK   ERR ok ") || len(snapshot.Scrollback) != 0 {
		t.Fatalf("snapshot must expose live screen cell matrix without scrollback truth, got rows=%#v scrollback=%#v", got, snapshot.Scrollback)
	}
	if snapshot.Screen.Cells[1][0].Style.FG != "ansi:2" {
		t.Fatalf("snapshot must preserve live cell style, got %#v", snapshot.Screen.Cells[1][0])
	}
	if !snapshot.Cursor.Visible || snapshot.Cursor.Row != 1 || snapshot.Cursor.Col == 0 {
		t.Fatalf("snapshot must preserve live cursor, got %#v", snapshot.Cursor)
	}
}

func newProtocolClient(t *testing.T) (*Server, *protocol.Client, func()) {
	t.Helper()
	clientTransport, serverTransport := memory.NewPair()
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	errCh := make(chan error, 1)
	go func() {
		errCh <- newProtocolSession(server, serverTransport).run(context.Background())
	}()
	client := protocol.NewClient(clientTransport)
	if err := client.Hello(context.Background(), protocol.Hello{Version: wire.Version, Client: "test"}); err != nil {
		t.Fatalf("hello: %v", err)
	}
	closeClient := func() {
		_ = client.Close()
		select {
		case err := <-errCh:
			if err != nil && !strings.Contains(err.Error(), "EOF") {
				t.Fatalf("server session returned error: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("server session did not stop")
		}
	}
	return server, client, closeClient
}

func serverProcess(t *testing.T, server *Server, terminalID string) *recordingProcess {
	t.Helper()
	terminal, err := server.Terminal(terminalID)
	if err != nil {
		t.Fatalf("terminal: %v", err)
	}
	process, ok := terminal.process.(*recordingProcess)
	if !ok {
		t.Fatalf("expected recording process, got %T", terminal.process)
	}
	return process
}

func waitForProcessTraffic(t *testing.T, server *Server, terminalID string, inputs int, resizes int) *recordingProcess {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		process := serverProcess(t, server, terminalID)
		processInputs, processResizes, _, _ := process.snapshot()
		if len(processInputs) >= inputs && len(processResizes) >= resizes {
			return process
		}
		time.Sleep(time.Millisecond)
	}
	process := serverProcess(t, server, terminalID)
	processInputs, processResizes, _, _ := process.snapshot()
	t.Fatalf("timed out waiting for process traffic inputs=%d/%d resizes=%d/%d", len(processInputs), inputs, len(processResizes), resizes)
	return process
}

func requireProtocolEvent(t *testing.T, events <-chan protocol.Event) protocol.Event {
	t.Helper()
	select {
	case event, ok := <-events:
		if !ok {
			t.Fatal("protocol events channel closed")
		}
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for protocol event")
	}
	return protocol.Event{}
}

func rowText(row protocol.CompactRow) string {
	var builder strings.Builder
	for _, cell := range row.DecodeCells() {
		builder.WriteString(cell.Content)
	}
	return builder.String()
}

func cellsText(row []protocol.Cell) string {
	var builder strings.Builder
	for _, cell := range row {
		builder.WriteString(cell.Content)
	}
	return builder.String()
}
