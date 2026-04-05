package runtime

import (
	"bytes"
	"context"
	"fmt"
	"image/color"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lozzow/termx"
	"github.com/lozzow/termx/protocol"
	unixtransport "github.com/lozzow/termx/transport/unix"
	"github.com/lozzow/termx/tuiv2/bridge"
)

func newTestRuntime(t *testing.T) (*Runtime, context.Context) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	socketPath := filepath.Join(t.TempDir(), "termx.sock")
	srv := termx.NewServer(termx.WithSocketPath(socketPath))
	done := make(chan error, 1)
	go func() {
		done <- srv.ListenAndServe(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		_ = srv.Shutdown(context.Background())
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("server did not stop in time")
		}
	})

	var transport *unixtransport.Transport
	var err error
	deadline := time.Now().Add(2 * time.Second)
	for {
		transport, err = unixtransport.Dial(socketPath)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("dial: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	client := protocol.NewClient(transport)
	t.Cleanup(func() { _ = client.Close() })

	helloCtx, helloCancel := context.WithTimeout(ctx, 2*time.Second)
	defer helloCancel()
	if err := client.Hello(helloCtx, protocol.Hello{Version: protocol.Version}); err != nil {
		t.Fatalf("hello: %v", err)
	}

	return New(bridge.NewProtocolClient(client)), ctx
}

func TestRuntimeListTerminalsDoesNotPopulateRegistry(t *testing.T) {
	rt, ctx := newTestRuntime(t)

	created, err := rt.client.Create(ctx, protocol.CreateParams{
		Command: []string{"sh"},
		Name:    "demo",
		Size:    protocol.Size{Cols: 80, Rows: 24},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	terminals, err := rt.ListTerminals(ctx)
	if err != nil {
		t.Fatalf("list terminals: %v", err)
	}
	if len(terminals) != 1 {
		t.Fatalf("expected 1 terminal, got %d", len(terminals))
	}
	stored := rt.Registry().Get(created.TerminalID)
	if stored != nil {
		t.Fatalf("expected list to avoid populating registry, got %#v", stored)
	}
}

func TestRuntimeAttachAndLoadSnapshotInitializesVTermCache(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 7, Mode: "collaborator"}
	client.listResult = &protocol.ListResult{Terminals: []protocol.TerminalInfo{{
		ID:    "term-1",
		Name:  "shell",
		State: "running",
	}}}
	client.snapshotByTerminal["term-1"] = snapshotWithLines("term-1", 6, 3, []string{
		"hello",
		"world",
	})

	rt := New(client)

	terminal, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator")
	if err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if terminal.VTerm == nil {
		t.Fatal("expected attach to initialize a vterm")
	}

	snapshot, err := rt.LoadSnapshot(ctx, "term-1", 0, 10)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}

	stored := rt.Registry().Get("term-1")
	if stored == nil || stored.Snapshot == nil {
		t.Fatal("expected snapshot cached on terminal runtime")
	}
	if stored.Name != "shell" {
		t.Fatalf("expected attach to hydrate terminal metadata name, got %q", stored.Name)
	}
	screen := stored.VTerm.ScreenContent()
	if len(screen.Cells) < 2 || len(screen.Cells[0]) < 5 || len(screen.Cells[1]) < 5 {
		t.Fatalf("unexpected vterm screen dimensions: %#v", screen.Cells)
	}
	if got := screen.Cells[0][0].Content + screen.Cells[0][1].Content + screen.Cells[0][2].Content + screen.Cells[0][3].Content + screen.Cells[0][4].Content; got != "hello" {
		t.Fatalf("expected first row to contain hello, got %q", got)
	}
	if got := screen.Cells[1][0].Content + screen.Cells[1][1].Content + screen.Cells[1][2].Content + screen.Cells[1][3].Content + screen.Cells[1][4].Content; got != "world" {
		t.Fatalf("expected second row to contain world, got %q", got)
	}
}

func TestRuntimeStartStreamRefreshesSnapshotAndInvalidates(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 9, Mode: "collaborator"}

	var invalidateCount atomic.Int32
	rt := New(client, WithInvalidate(func() {
		invalidateCount.Add(1)
	}))

	terminal, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator")
	if err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if terminal.VTerm == nil {
		t.Fatal("expected attach to initialize a vterm")
	}

	if err := rt.StartStream(ctx, "term-1"); err != nil {
		t.Fatalf("start stream: %v", err)
	}

	client.sendFrame(9, protocol.StreamFrame{Type: protocol.TypeOutput, Payload: []byte("hi")})

	waitFor(t, func() bool {
		stored := rt.Registry().Get("term-1")
		return stored != nil && stored.Snapshot != nil && snapshotContains(stored.Snapshot, "hi")
	})

	if invalidateCount.Load() == 0 {
		t.Fatal("expected stream refresh to invalidate rendering")
	}
	if !snapshotContains(rt.Registry().Get("term-1").Snapshot, "hi") {
		t.Fatal("expected refreshed snapshot to contain streamed output")
	}
}

func TestRuntimeSetHostDefaultColorsRefreshesVisibleState(t *testing.T) {
	var invalidateCount atomic.Int32
	rt := New(nil, WithInvalidate(func() {
		invalidateCount.Add(1)
	}))

	initial := rt.Visible()
	if initial == nil {
		t.Fatal("expected visible runtime")
	}
	if initial.HostDefaultFG != "" || initial.HostDefaultBG != "" {
		t.Fatalf("expected empty initial host colors, got fg=%q bg=%q", initial.HostDefaultFG, initial.HostDefaultBG)
	}

	rt.SetHostDefaultColors(color.RGBA{R: 0xaa, G: 0xbb, B: 0xcc, A: 0xff}, color.RGBA{R: 0x11, G: 0x22, B: 0x33, A: 0xff})

	visible := rt.Visible()
	if visible.HostDefaultFG != "#aabbcc" || visible.HostDefaultBG != "#112233" {
		t.Fatalf("expected visible host colors to refresh, got fg=%q bg=%q", visible.HostDefaultFG, visible.HostDefaultBG)
	}
	if invalidateCount.Load() == 0 {
		t.Fatal("expected host default color update to invalidate rendering")
	}
}

func TestRuntimeSetHostPaletteColorRefreshesVisibleState(t *testing.T) {
	var invalidateCount atomic.Int32
	rt := New(nil, WithInvalidate(func() {
		invalidateCount.Add(1)
	}))

	if visible := rt.Visible(); visible == nil {
		t.Fatal("expected visible runtime")
	}

	rt.SetHostPaletteColor(5, color.RGBA{R: 0x44, G: 0x88, B: 0xcc, A: 0xff})

	visible := rt.Visible()
	if got := visible.HostPalette[5]; got != "#4488cc" {
		t.Fatalf("expected visible host palette to refresh, got %q", got)
	}
	if invalidateCount.Load() == 0 {
		t.Fatal("expected host palette update to invalidate rendering")
	}
}

func TestRuntimeResizePaneUsesBindingChannelAndRefreshesSnapshot(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 11, Mode: "collaborator"}
	client.snapshotByTerminal["term-1"] = snapshotWithLines("term-1", 6, 3, []string{"seed"})

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if _, err := rt.LoadSnapshot(ctx, "term-1", 0, 10); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}

	if err := rt.ResizePane(ctx, "pane-1", "term-1", 100, 40); err != nil {
		t.Fatalf("resize pane: %v", err)
	}

	if len(client.resizeCalls) != 1 {
		t.Fatalf("expected 1 resize call, got %d", len(client.resizeCalls))
	}
	call := client.resizeCalls[0]
	if call.channel != 11 || call.cols != 100 || call.rows != 40 {
		t.Fatalf("unexpected resize call: %+v", call)
	}
	stored := rt.Registry().Get("term-1")
	if stored == nil || stored.Snapshot == nil {
		t.Fatal("expected terminal snapshot after resize")
	}
	if stored.Snapshot.Size.Cols != 100 || stored.Snapshot.Size.Rows != 40 {
		t.Fatalf("expected resized snapshot size 100x40, got %dx%d", stored.Snapshot.Size.Cols, stored.Snapshot.Size.Rows)
	}
}

func TestRuntimeResizePaneSkipsFollowerBindings(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 11, Mode: "collaborator"}
	client.snapshotByTerminal["term-1"] = snapshotWithLines("term-1", 100, 40, []string{"seed"})

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach owner: %v", err)
	}
	if _, err := rt.LoadSnapshot(ctx, "term-1", 0, 10); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}

	client.attachResult = &protocol.AttachResult{Channel: 12, Mode: "collaborator"}
	if _, err := rt.AttachTerminal(ctx, "pane-2", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach follower: %v", err)
	}
	client.resizeCalls = nil

	if err := rt.ResizePane(ctx, "pane-2", "term-1", 50, 16); err != nil {
		t.Fatalf("resize follower: %v", err)
	}

	if len(client.resizeCalls) != 0 {
		t.Fatalf("expected follower resize to be ignored, got %#v", client.resizeCalls)
	}
	if binding := rt.Binding("pane-1"); binding == nil || binding.Role != BindingRoleOwner {
		t.Fatalf("expected pane-1 to remain owner, got %#v", binding)
	}
	if binding := rt.Binding("pane-2"); binding == nil || binding.Role != BindingRoleFollower {
		t.Fatalf("expected pane-2 to remain follower, got %#v", binding)
	}
}

func TestRuntimeUnbindOwnerLeavesTerminalWithoutOwner(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 11, Mode: "collaborator"}

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach owner: %v", err)
	}
	client.attachResult = &protocol.AttachResult{Channel: 12, Mode: "collaborator"}
	if _, err := rt.AttachTerminal(ctx, "pane-2", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach follower: %v", err)
	}

	rt.UnbindPane("pane-1", "term-1")

	terminal := rt.Registry().Get("term-1")
	if terminal == nil {
		t.Fatal("expected terminal runtime")
	}
	if terminal.OwnerPaneID != "" {
		t.Fatalf("expected terminal owner cleared, got %q", terminal.OwnerPaneID)
	}
	if !reflect.DeepEqual(terminal.BoundPaneIDs, []string{"pane-2"}) {
		t.Fatalf("expected only pane-2 to remain bound, got %#v", terminal.BoundPaneIDs)
	}
	if binding := rt.Binding("pane-2"); binding == nil || binding.Role != BindingRoleFollower {
		t.Fatalf("expected pane-2 binding to remain follower, got %#v", binding)
	}
	if binding := rt.Binding("pane-1"); binding != nil {
		t.Fatalf("expected pane-1 binding removed, got %#v", binding)
	}
}

func TestRuntimeAcquireTerminalOwnershipPromotesRequestedPane(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 11, Mode: "collaborator"}

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach owner: %v", err)
	}
	client.attachResult = &protocol.AttachResult{Channel: 12, Mode: "collaborator"}
	if _, err := rt.AttachTerminal(ctx, "pane-2", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach follower: %v", err)
	}

	if err := rt.AcquireTerminalOwnership("pane-2", "term-1"); err != nil {
		t.Fatalf("acquire ownership: %v", err)
	}

	terminal := rt.Registry().Get("term-1")
	if terminal == nil {
		t.Fatal("expected terminal runtime")
	}
	if terminal.OwnerPaneID != "pane-2" {
		t.Fatalf("expected pane-2 as owner, got %q", terminal.OwnerPaneID)
	}
	if binding := rt.Binding("pane-1"); binding == nil || binding.Role != BindingRoleFollower {
		t.Fatalf("expected pane-1 demoted to follower, got %#v", binding)
	}
	if binding := rt.Binding("pane-2"); binding == nil || binding.Role != BindingRoleOwner {
		t.Fatalf("expected pane-2 promoted to owner, got %#v", binding)
	}
}

func TestRuntimeAcquireTerminalOwnershipForcesNextOwnerResize(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 11, Mode: "collaborator"}
	client.snapshotByTerminal["term-1"] = snapshotWithLines("term-1", 50, 16, []string{"seed"})

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach owner: %v", err)
	}
	if _, err := rt.LoadSnapshot(ctx, "term-1", 0, 10); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}

	client.attachResult = &protocol.AttachResult{Channel: 12, Mode: "collaborator"}
	if _, err := rt.AttachTerminal(ctx, "pane-2", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach follower: %v", err)
	}
	if err := rt.AcquireTerminalOwnership("pane-2", "term-1"); err != nil {
		t.Fatalf("acquire ownership: %v", err)
	}

	client.resizeCalls = nil
	if err := rt.ResizePane(ctx, "pane-2", "term-1", 50, 16); err != nil {
		t.Fatalf("resize pane: %v", err)
	}

	if len(client.resizeCalls) != 1 {
		t.Fatalf("expected forced resize after owner handoff, got %#v", client.resizeCalls)
	}
	if got := rt.Registry().Get("term-1"); got == nil || got.PendingOwnerResize {
		t.Fatalf("expected pending owner resize cleared after resize, got %#v", got)
	}
}

func TestRuntimeResizeDoesNothingWithoutExplicitOwner(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 11, Mode: "collaborator"}

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach owner: %v", err)
	}
	client.attachResult = &protocol.AttachResult{Channel: 12, Mode: "collaborator"}
	if _, err := rt.AttachTerminal(ctx, "pane-2", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach follower: %v", err)
	}

	rt.UnbindPane("pane-1", "term-1")

	if err := rt.ResizePane(ctx, "pane-2", "term-1", 100, 30); err != nil {
		t.Fatalf("resize pane: %v", err)
	}
	if len(client.resizeCalls) != 0 {
		t.Fatalf("expected no resize calls without explicit owner, got %#v", client.resizeCalls)
	}
}

func TestRuntimeApplySessionLeasesDemotesForeignLeaseAndPromotesLocalLease(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 11, Mode: "collaborator"}

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach owner: %v", err)
	}
	client.attachResult = &protocol.AttachResult{Channel: 12, Mode: "collaborator"}
	if _, err := rt.AttachTerminal(ctx, "pane-2", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach follower: %v", err)
	}

	rt.ApplySessionLeases("view-local", []protocol.LeaseInfo{{
		TerminalID: "term-1",
		ViewID:     "view-remote",
		PaneID:     "pane-9",
	}})

	if terminal := rt.Registry().Get("term-1"); terminal == nil || terminal.OwnerPaneID != "" || !terminal.RequiresExplicitOwner {
		t.Fatalf("expected foreign lease to demote local panes, got %#v", terminal)
	}
	if binding := rt.Binding("pane-1"); binding == nil || binding.Role != BindingRoleFollower {
		t.Fatalf("expected pane-1 follower under foreign lease, got %#v", binding)
	}

	rt.ApplySessionLeases("view-local", []protocol.LeaseInfo{{
		TerminalID: "term-1",
		ViewID:     "view-local",
		PaneID:     "pane-2",
	}})

	terminal := rt.Registry().Get("term-1")
	if terminal == nil || terminal.OwnerPaneID != "pane-2" || terminal.RequiresExplicitOwner {
		t.Fatalf("expected local lease to promote pane-2 owner, got %#v", terminal)
	}
	if !terminal.PendingOwnerResize {
		t.Fatalf("expected local lease promotion to force next resize, got %#v", terminal)
	}
	if binding := rt.Binding("pane-2"); binding == nil || binding.Role != BindingRoleOwner {
		t.Fatalf("expected pane-2 owner under local lease, got %#v", binding)
	}
}

func TestRuntimeAttachSnapshotInputAndResize(t *testing.T) {
	rt, ctx := newTestRuntime(t)

	created, err := rt.client.Create(ctx, protocol.CreateParams{
		Command: []string{"sh"},
		Name:    "demo",
		Size:    protocol.Size{Cols: 80, Rows: 24},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	terminal, err := rt.AttachTerminal(ctx, "pane-1", created.TerminalID, "collaborator")
	if err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if terminal.Channel == 0 {
		t.Fatal("expected non-zero channel")
	}
	binding := rt.Binding("pane-1")
	if binding == nil || !binding.Connected {
		t.Fatal("expected connected pane binding")
	}

	snapshot, err := rt.LoadSnapshot(ctx, created.TerminalID, 0, 10)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}
	if terminal.Snapshot == nil {
		t.Fatal("expected snapshot cached on terminal runtime")
	}

	if err := rt.SendInput(ctx, "pane-1", []byte("echo hi\n")); err != nil {
		t.Fatalf("send input: %v", err)
	}
	if err := rt.ResizePane(ctx, "pane-1", created.TerminalID, 100, 40); err != nil {
		t.Fatalf("resize terminal: %v", err)
	}
}

func TestRuntimeAttachDoesNotRetainStructuralTerminalIDInBinding(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 11, Mode: "collaborator"}

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach terminal: %v", err)
	}

	if _, ok := reflect.TypeOf(PaneBinding{}).FieldByName("TerminalID"); ok {
		t.Fatal("expected PaneBinding to stop storing structural TerminalID")
	}
	binding := rt.Binding("pane-1")
	if binding == nil || !binding.Connected || binding.Channel != 11 {
		t.Fatalf("expected connected binding with channel only, got %#v", binding)
	}
}

func TestRuntimeReattachSamePaneCleansPreviousTerminalBindingState(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 11, Mode: "collaborator"}

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach term-1: %v", err)
	}

	client.attachResult = &protocol.AttachResult{Channel: 12, Mode: "collaborator"}
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-2", "collaborator"); err != nil {
		t.Fatalf("attach term-2: %v", err)
	}

	oldTerminal := rt.Registry().Get("term-1")
	if oldTerminal == nil {
		t.Fatal("expected old terminal runtime to remain present")
	}
	if oldTerminal.OwnerPaneID != "" {
		t.Fatalf("expected old terminal owner to be cleared, got %q", oldTerminal.OwnerPaneID)
	}
	if len(oldTerminal.BoundPaneIDs) != 0 {
		t.Fatalf("expected old terminal bound panes cleared, got %#v", oldTerminal.BoundPaneIDs)
	}

	newTerminal := rt.Registry().Get("term-2")
	if newTerminal == nil {
		t.Fatal("expected new terminal runtime")
	}
	if newTerminal.OwnerPaneID != "pane-1" {
		t.Fatalf("expected new terminal owner pane-1, got %q", newTerminal.OwnerPaneID)
	}
	if !reflect.DeepEqual(newTerminal.BoundPaneIDs, []string{"pane-1"}) {
		t.Fatalf("expected new terminal bound panes [pane-1], got %#v", newTerminal.BoundPaneIDs)
	}

	binding := rt.Binding("pane-1")
	if binding == nil || binding.Channel != 12 || binding.Role != BindingRoleOwner || !binding.Connected {
		t.Fatalf("expected binding reassigned to new channel/owner state, got %#v", binding)
	}
}

type fakeBridgeClient struct {
	mu                  sync.Mutex
	attachResult        *protocol.AttachResult
	listResult          *protocol.ListResult
	snapshotByTerminal  map[string]*protocol.Snapshot
	streams             map[uint16]chan protocol.StreamFrame
	streamSubscriptions map[uint16]int
	inputCalls          []inputCall
	resizeCalls         []resizeCall
}

type inputCall struct {
	channel uint16
	data    []byte
}

type resizeCall struct {
	channel uint16
	cols    uint16
	rows    uint16
}

func newFakeBridgeClient() *fakeBridgeClient {
	return &fakeBridgeClient{
		listResult:          &protocol.ListResult{},
		snapshotByTerminal:  make(map[string]*protocol.Snapshot),
		streams:             make(map[uint16]chan protocol.StreamFrame),
		streamSubscriptions: make(map[uint16]int),
	}
}

func (f *fakeBridgeClient) Close() error { return nil }

func (f *fakeBridgeClient) Create(context.Context, protocol.CreateParams) (*protocol.CreateResult, error) {
	return nil, fmt.Errorf("not implemented")
}

func (f *fakeBridgeClient) SetTags(context.Context, string, map[string]string) error { return nil }

func (f *fakeBridgeClient) SetMetadata(context.Context, string, string, map[string]string) error {
	return nil
}

func (f *fakeBridgeClient) List(context.Context) (*protocol.ListResult, error) {
	return f.listResult, nil
}

func (f *fakeBridgeClient) Events(context.Context, protocol.EventsParams) (<-chan protocol.Event, error) {
	return nil, fmt.Errorf("not implemented")
}

func (f *fakeBridgeClient) Attach(context.Context, string, string) (*protocol.AttachResult, error) {
	if f.attachResult == nil {
		return nil, fmt.Errorf("attach result not configured")
	}
	return f.attachResult, nil
}

func (f *fakeBridgeClient) Snapshot(context.Context, string, int, int) (*protocol.Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, snapshot := range f.snapshotByTerminal {
		return cloneSnapshot(snapshot), nil
	}
	return nil, fmt.Errorf("snapshot not configured")
}

func (f *fakeBridgeClient) Input(_ context.Context, channel uint16, data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inputCalls = append(f.inputCalls, inputCall{channel: channel, data: append([]byte(nil), data...)})
	return nil
}

func (f *fakeBridgeClient) Resize(_ context.Context, channel uint16, cols, rows uint16) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resizeCalls = append(f.resizeCalls, resizeCall{channel: channel, cols: cols, rows: rows})
	return nil
}

func (f *fakeBridgeClient) Stream(channel uint16) (<-chan protocol.StreamFrame, func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	stream := f.streams[channel]
	if stream == nil {
		stream = make(chan protocol.StreamFrame, 16)
		f.streams[channel] = stream
	}
	f.streamSubscriptions[channel]++
	return stream, func() {}
}

func (f *fakeBridgeClient) Kill(context.Context, string) error { return nil }

func (f *fakeBridgeClient) CreateSession(context.Context, protocol.CreateSessionParams) (*protocol.SessionSnapshot, error) {
	return nil, fmt.Errorf("not implemented")
}

func (f *fakeBridgeClient) ListSessions(context.Context) (*protocol.ListSessionsResult, error) {
	return nil, fmt.Errorf("not implemented")
}

func (f *fakeBridgeClient) GetSession(context.Context, string) (*protocol.SessionSnapshot, error) {
	return nil, fmt.Errorf("not implemented")
}

func (f *fakeBridgeClient) AttachSession(context.Context, protocol.AttachSessionParams) (*protocol.SessionSnapshot, error) {
	return nil, fmt.Errorf("not implemented")
}

func (f *fakeBridgeClient) DetachSession(context.Context, string, string) error {
	return fmt.Errorf("not implemented")
}

func (f *fakeBridgeClient) ApplySession(context.Context, protocol.ApplySessionParams) (*protocol.SessionSnapshot, error) {
	return nil, fmt.Errorf("not implemented")
}

func (f *fakeBridgeClient) ReplaceSession(context.Context, protocol.ReplaceSessionParams) (*protocol.SessionSnapshot, error) {
	return nil, fmt.Errorf("not implemented")
}

func (f *fakeBridgeClient) UpdateSessionView(context.Context, protocol.UpdateSessionViewParams) (*protocol.ViewInfo, error) {
	return nil, fmt.Errorf("not implemented")
}

func (f *fakeBridgeClient) AcquireSessionLease(context.Context, protocol.AcquireSessionLeaseParams) (*protocol.LeaseInfo, error) {
	return nil, fmt.Errorf("not implemented")
}

func (f *fakeBridgeClient) ReleaseSessionLease(context.Context, protocol.ReleaseSessionLeaseParams) error {
	return fmt.Errorf("not implemented")
}

func (f *fakeBridgeClient) sendFrame(channel uint16, frame protocol.StreamFrame) {
	f.mu.Lock()
	stream := f.streams[channel]
	f.mu.Unlock()
	if stream == nil {
		panic("stream not initialized")
	}
	stream <- frame
}

func (f *fakeBridgeClient) closeStream(channel uint16) {
	f.mu.Lock()
	stream := f.streams[channel]
	delete(f.streams, channel)
	f.mu.Unlock()
	if stream != nil {
		close(stream)
	}
}

func (f *fakeBridgeClient) subscriptionCount(channel uint16) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.streamSubscriptions[channel]
}

func snapshotWithLines(terminalID string, cols, rows uint16, lines []string) *protocol.Snapshot {
	grid := make([][]protocol.Cell, rows)
	for y := range rows {
		grid[y] = make([]protocol.Cell, cols)
		for x := range cols {
			grid[y][x] = protocol.Cell{Content: " ", Width: 1}
		}
	}
	for y, line := range lines {
		if y >= int(rows) {
			break
		}
		for x := 0; x < len(line) && x < int(cols); x++ {
			grid[y][x] = protocol.Cell{Content: string(line[x]), Width: 1}
		}
	}
	return &protocol.Snapshot{
		TerminalID: terminalID,
		Size:       protocol.Size{Cols: cols, Rows: rows},
		Screen:     protocol.ScreenData{Cells: grid},
		Cursor:     protocol.CursorState{Visible: true},
		Modes:      protocol.TerminalModes{AutoWrap: true},
		Timestamp:  time.Now(),
	}
}

func cloneSnapshot(snapshot *protocol.Snapshot) *protocol.Snapshot {
	if snapshot == nil {
		return nil
	}
	cloned := *snapshot
	cloned.Screen.Cells = make([][]protocol.Cell, len(snapshot.Screen.Cells))
	for y, row := range snapshot.Screen.Cells {
		cloned.Screen.Cells[y] = append([]protocol.Cell(nil), row...)
	}
	cloned.Scrollback = make([][]protocol.Cell, len(snapshot.Scrollback))
	for y, row := range snapshot.Scrollback {
		cloned.Scrollback[y] = append([]protocol.Cell(nil), row...)
	}
	return &cloned
}

func snapshotContains(snapshot *protocol.Snapshot, want string) bool {
	if snapshot == nil {
		return false
	}
	for _, row := range snapshot.Screen.Cells {
		var buf bytes.Buffer
		for _, cell := range row {
			buf.WriteString(cell.Content)
		}
		if bytes.Contains(buf.Bytes(), []byte(want)) {
			return true
		}
	}
	return false
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}

func TestRuntimeStartStreamReconnectsAfterChannelClose(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 9, Mode: "collaborator"}
	rt := New(client)

	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if err := rt.StartStream(ctx, "term-1"); err != nil {
		t.Fatalf("start stream: %v", err)
	}

	client.sendFrame(9, protocol.StreamFrame{Type: protocol.TypeOutput, Payload: []byte("one")})
	waitFor(t, func() bool {
		stored := rt.Registry().Get("term-1")
		return stored != nil && stored.Snapshot != nil && snapshotContains(stored.Snapshot, "one")
	})

	client.closeStream(9)

	waitFor(t, func() bool {
		return client.subscriptionCount(9) >= 2
	})

	client.sendFrame(9, protocol.StreamFrame{Type: protocol.TypeOutput, Payload: []byte("two")})
	waitFor(t, func() bool {
		stored := rt.Registry().Get("term-1")
		return stored != nil && stored.Snapshot != nil && snapshotContains(stored.Snapshot, "two")
	})

	stored := rt.Registry().Get("term-1")
	if stored == nil {
		t.Fatal("expected terminal runtime after reconnect")
	}
	if stored.Stream.RetryCount != 0 {
		t.Fatalf("expected retry count reset after successful frame, got %d", stored.Stream.RetryCount)
	}
	if !stored.Stream.Active {
		t.Fatal("expected stream to be active after reconnect")
	}
}
