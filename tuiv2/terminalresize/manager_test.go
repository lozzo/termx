package terminalresize

import (
	"context"
	"testing"

	"github.com/lozzow/termx/termx-core/protocol"
	"github.com/lozzow/termx/tuiv2/bridge"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/terminalcontrol"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func TestManagerEnsureSizedResizesTerminalUsingViewportRect(t *testing.T) {
	client := &fakeClient{}
	rt := runtime.New(client)
	terminal := rt.Registry().GetOrCreate("term-1")
	terminal.Snapshot = &protocol.Snapshot{TerminalID: "term-1", Size: protocol.Size{Cols: 80, Rows: 24}}
	terminal.BoundPaneIDs = []string{"pane-1"}
	terminal.OwnerPaneID = "pane-1"
	terminal.ControlPaneID = "pane-1"
	binding := rt.BindPane("pane-1")
	binding.Channel = 7
	binding.Connected = true

	control := terminalcontrol.NewManager(rt, terminalcontrol.SessionLeaseHooks{})
	manager := NewManager(rt, control, func(string, workbench.Rect) (workbench.Rect, bool) {
		return workbench.Rect{W: 50, H: 18}, true
	})
	if err := manager.EnsureSized(context.Background(), Target{
		PaneID:     "pane-1",
		TerminalID: "term-1",
		Rect:       workbench.Rect{W: 60, H: 20},
	}); err != nil {
		t.Fatalf("EnsureSized: %v", err)
	}
	if len(client.resizes) != 1 {
		t.Fatalf("expected one resize call, got %#v", client.resizes)
	}
	if got := client.resizes[0]; got.channel != 7 || got.cols != 50 || got.rows != 18 {
		t.Fatalf("unexpected resize call: %#v", got)
	}
}

func TestManagerEnsureSizedSupportsExplicitTakeover(t *testing.T) {
	client := &fakeClient{}
	rt := runtime.New(client)
	terminal := rt.Registry().GetOrCreate("term-1")
	terminal.Snapshot = &protocol.Snapshot{TerminalID: "term-1", Size: protocol.Size{Cols: 20, Rows: 10}}
	terminal.BoundPaneIDs = []string{"pane-1", "pane-2"}
	terminal.OwnerPaneID = "pane-1"
	terminal.ControlPaneID = "pane-1"
	ownerBinding := rt.BindPane("pane-1")
	ownerBinding.Channel = 1
	ownerBinding.Connected = true
	followerBinding := rt.BindPane("pane-2")
	followerBinding.Channel = 2
	followerBinding.Connected = true

	control := terminalcontrol.NewManager(rt, terminalcontrol.SessionLeaseHooks{})
	manager := NewManager(rt, control, func(string, workbench.Rect) (workbench.Rect, bool) {
		return workbench.Rect{W: 40, H: 16}, true
	})
	if err := manager.EnsureSized(context.Background(), Target{
		PaneID:           "pane-2",
		TerminalID:       "term-1",
		Rect:             workbench.Rect{W: 40, H: 16},
		ExplicitTakeover: true,
	}); err != nil {
		t.Fatalf("EnsureSized: %v", err)
	}
	if terminal.OwnerPaneID != "pane-2" || terminal.ControlPaneID != "pane-2" {
		t.Fatalf("expected explicit takeover to promote pane-2, got %#v", terminal)
	}
	if len(client.resizes) != 1 || client.resizes[0].channel != 2 {
		t.Fatalf("expected resize through promoted pane-2 channel, got %#v", client.resizes)
	}
}

func TestManagerPendingSatisfiedMatchesViewportSize(t *testing.T) {
	client := &fakeClient{}
	rt := runtime.New(client)
	terminal := rt.Registry().GetOrCreate("term-1")
	terminal.Snapshot = &protocol.Snapshot{TerminalID: "term-1", Size: protocol.Size{Cols: 50, Rows: 18}}

	manager := NewManager(rt, terminalcontrol.NewManager(rt, terminalcontrol.SessionLeaseHooks{}), func(string, workbench.Rect) (workbench.Rect, bool) {
		return workbench.Rect{W: 50, H: 18}, true
	})
	if !manager.PendingSatisfied(Target{PaneID: "pane-1", TerminalID: "term-1", Rect: workbench.Rect{W: 60, H: 20}}) {
		t.Fatal("expected matching viewport size to satisfy pending resize")
	}
	terminal.Snapshot.Size = protocol.Size{Cols: 49, Rows: 18}
	if manager.PendingSatisfied(Target{PaneID: "pane-1", TerminalID: "term-1", Rect: workbench.Rect{W: 60, H: 20}}) {
		t.Fatal("expected mismatched size not to satisfy pending resize")
	}
}

func TestManagerResizeVisibleProcessesEachTarget(t *testing.T) {
	client := &fakeClient{}
	rt := runtime.New(client)
	term1 := rt.Registry().GetOrCreate("term-1")
	term1.Snapshot = &protocol.Snapshot{TerminalID: "term-1", Size: protocol.Size{Cols: 20, Rows: 10}}
	term1.BoundPaneIDs = []string{"pane-1"}
	term1.OwnerPaneID = "pane-1"
	term1.ControlPaneID = "pane-1"
	binding1 := rt.BindPane("pane-1")
	binding1.Channel = 1
	binding1.Connected = true
	term2 := rt.Registry().GetOrCreate("term-2")
	term2.Snapshot = &protocol.Snapshot{TerminalID: "term-2", Size: protocol.Size{Cols: 20, Rows: 10}}
	term2.BoundPaneIDs = []string{"pane-2"}
	term2.OwnerPaneID = "pane-2"
	term2.ControlPaneID = "pane-2"
	binding2 := rt.BindPane("pane-2")
	binding2.Channel = 2
	binding2.Connected = true

	manager := NewManager(rt, terminalcontrol.NewManager(rt, terminalcontrol.SessionLeaseHooks{}), func(paneID string, rect workbench.Rect) (workbench.Rect, bool) {
		if paneID == "pane-1" {
			return workbench.Rect{W: 30, H: 12}, true
		}
		return workbench.Rect{W: 40, H: 16}, true
	})
	if err := manager.ResizeVisible(context.Background(), []Target{
		{PaneID: "pane-1", TerminalID: "term-1", Rect: workbench.Rect{W: 30, H: 12}},
		{PaneID: "pane-2", TerminalID: "term-2", Rect: workbench.Rect{W: 40, H: 16}},
	}); err != nil {
		t.Fatalf("ResizeVisible: %v", err)
	}
	if len(client.resizes) != 2 {
		t.Fatalf("expected two resize calls, got %#v", client.resizes)
	}
}

type fakeClient struct {
	resizes []resizeCall
}

type resizeCall struct {
	channel uint16
	cols    uint16
	rows    uint16
}

var _ bridge.Client = (*fakeClient)(nil)

func (c *fakeClient) Close() error { return nil }
func (c *fakeClient) Create(context.Context, protocol.CreateParams) (*protocol.CreateResult, error) {
	return nil, nil
}
func (c *fakeClient) SetTags(context.Context, string, map[string]string) error { return nil }
func (c *fakeClient) SetMetadata(context.Context, string, string, map[string]string) error {
	return nil
}
func (c *fakeClient) List(context.Context) (*protocol.ListResult, error) {
	return &protocol.ListResult{}, nil
}
func (c *fakeClient) Events(context.Context, protocol.EventsParams) (<-chan protocol.Event, error) {
	return nil, nil
}
func (c *fakeClient) Attach(context.Context, string, string) (*protocol.AttachResult, error) {
	return nil, nil
}
func (c *fakeClient) Snapshot(context.Context, string, int, int) (*protocol.Snapshot, error) {
	return nil, nil
}
func (c *fakeClient) Input(context.Context, uint16, []byte) error { return nil }
func (c *fakeClient) Resize(_ context.Context, channel uint16, cols, rows uint16) error {
	c.resizes = append(c.resizes, resizeCall{channel: channel, cols: cols, rows: rows})
	return nil
}
func (c *fakeClient) Stream(uint16) (<-chan protocol.StreamFrame, func()) {
	ch := make(chan protocol.StreamFrame)
	close(ch)
	return ch, func() {}
}
func (c *fakeClient) Kill(context.Context, string) error    { return nil }
func (c *fakeClient) Restart(context.Context, string) error { return nil }
func (c *fakeClient) CreateSession(context.Context, protocol.CreateSessionParams) (*protocol.SessionSnapshot, error) {
	return &protocol.SessionSnapshot{}, nil
}
func (c *fakeClient) ListSessions(context.Context) (*protocol.ListSessionsResult, error) {
	return &protocol.ListSessionsResult{}, nil
}
func (c *fakeClient) GetSession(context.Context, string) (*protocol.SessionSnapshot, error) {
	return &protocol.SessionSnapshot{}, nil
}
func (c *fakeClient) AttachSession(context.Context, protocol.AttachSessionParams) (*protocol.SessionSnapshot, error) {
	return &protocol.SessionSnapshot{}, nil
}
func (c *fakeClient) DetachSession(context.Context, string, string) error { return nil }
func (c *fakeClient) ApplySession(context.Context, protocol.ApplySessionParams) (*protocol.SessionSnapshot, error) {
	return &protocol.SessionSnapshot{}, nil
}
func (c *fakeClient) ReplaceSession(context.Context, protocol.ReplaceSessionParams) (*protocol.SessionSnapshot, error) {
	return &protocol.SessionSnapshot{}, nil
}
func (c *fakeClient) UpdateSessionView(context.Context, protocol.UpdateSessionViewParams) (*protocol.ViewInfo, error) {
	return &protocol.ViewInfo{}, nil
}
func (c *fakeClient) AcquireSessionLease(context.Context, protocol.AcquireSessionLeaseParams) (*protocol.LeaseInfo, error) {
	return nil, nil
}
func (c *fakeClient) ReleaseSessionLease(context.Context, protocol.ReleaseSessionLeaseParams) error {
	return nil
}
