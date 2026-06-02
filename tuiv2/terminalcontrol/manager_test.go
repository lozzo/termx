package terminalcontrol

import (
	"context"
	"testing"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/tuiv2/bridge"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/sessionstore"
)

func TestManagerReleaseLeaseRemovesLeaseAndReappliesLocalOwner(t *testing.T) {
	client := &fakeClient{}
	rt := runtime.New(client)
	leases := map[string]sessionstore.LeaseInfo{
		"term-1": {TerminalID: "term-1", SessionID: "session-main", ViewID: "view-remote", PaneID: "pane-remote"},
	}
	terminal := rt.Registry().GetOrCreate("term-1")
	terminal.BoundPaneIDs = []string{"pane-1"}
	binding := rt.BindPane("pane-1")
	binding.Channel = 1
	binding.Connected = true
	rt.ApplySessionLeases("view-local", currentLeases(leases))

	manager := NewManager(rt, SessionLeaseHooks{
		SessionID: "session-main",
		ViewID:    "view-local",
		Release:   client.ReleaseLease,
		Remove: func(terminalID string) {
			delete(leases, terminalID)
		},
		Apply: func() {
			rt.ApplySessionLeases("view-local", currentLeases(leases))
		},
	})
	if err := manager.ReleaseLease(context.Background(), "term-1"); err != nil {
		t.Fatalf("ReleaseLease: %v", err)
	}
	if len(client.releaseLeaseCalls) != 1 {
		t.Fatalf("expected one release lease call, got %#v", client.releaseLeaseCalls)
	}
	if len(leases) != 0 {
		t.Fatalf("expected lease store cleared, got %#v", leases)
	}
	if terminal.OwnerPaneID != "" || terminal.ControlPaneID != "" || !terminal.RequiresExplicitOwner {
		t.Fatalf("expected lease release to leave terminal awaiting explicit owner, got %#v", terminal)
	}
}

func TestManagerSyncExplicitSessionTakeoverAcquiresLeaseAndResizes(t *testing.T) {
	client := &fakeClient{}
	rt := runtime.New(client)
	leases := map[string]sessionstore.LeaseInfo{
		"term-1": {TerminalID: "term-1", SessionID: "session-main", ViewID: "view-remote", PaneID: "pane-remote"},
	}

	terminal := rt.Registry().GetOrCreate("term-1")
	terminal.State = "running"
	terminal.Channel = 1
	terminal.OwnerPaneID = "pane-1"
	terminal.BoundPaneIDs = []string{"pane-1", "pane-2"}
	terminal.Snapshot = &protocol.Snapshot{TerminalID: "term-1", Size: protocol.Size{Cols: 80, Rows: 24}}

	ownerBinding := rt.BindPane("pane-1")
	ownerBinding.Channel = 1
	ownerBinding.Connected = true
	ownerBinding.Role = runtime.BindingRoleOwner

	followerBinding := rt.BindPane("pane-2")
	followerBinding.Channel = 2
	followerBinding.Connected = true
	followerBinding.Role = runtime.BindingRoleFollower

	manager := NewManager(rt, SessionLeaseHooks{
		SessionID: "session-main",
		ViewID:    "view-local",
		Acquire:   client.AcquireLease,
		NeedsAcquire: func(terminalID, paneID string) bool {
			return false
		},
		Store: func(lease sessionstore.LeaseInfo) {
			leases[lease.TerminalID] = lease
		},
		Apply: func() {
			rt.ApplySessionLeases("view-local", currentLeases(leases))
		},
	})
	if err := manager.Sync(context.Background(), SyncRequest{
		PaneID:           "pane-2",
		TerminalID:       "term-1",
		TargetCols:       40,
		TargetRows:       20,
		ResizeIfNeeded:   true,
		ExplicitTakeover: true,
	}); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(client.acquireLeaseCalls) != 1 {
		t.Fatalf("expected one lease acquire, got %#v", client.acquireLeaseCalls)
	}
	if got := client.acquireLeaseCalls[0]; got.ViewID != "view-local" || got.PaneID != "pane-2" || got.TerminalID != "term-1" {
		t.Fatalf("unexpected lease acquire params: %#v", got)
	}
	if terminal.OwnerPaneID != "pane-2" {
		t.Fatalf("expected pane-2 to become owner, got %q", terminal.OwnerPaneID)
	}
	if ownerBinding.Role != runtime.BindingRoleFollower || followerBinding.Role != runtime.BindingRoleOwner {
		t.Fatalf("expected roles to swap after lease acquire, owner=%#v follower=%#v", ownerBinding, followerBinding)
	}
	if len(client.resizes) != 1 || client.resizes[0].channel != 2 {
		t.Fatalf("expected one resize on pane-2 channel, got %#v", client.resizes)
	}
	if lease := leases["term-1"]; lease.ViewID != "view-local" || lease.PaneID != "pane-2" {
		t.Fatalf("expected local lease stored after acquire, got %#v", lease)
	}
}

func TestManagerSyncImplicitInteractiveOwnerAcquiresLocalOwnership(t *testing.T) {
	client := &fakeClient{}
	rt := runtime.New(client)
	terminal := rt.Registry().GetOrCreate("term-1")
	terminal.OwnerPaneID = "pane-1"
	terminal.BoundPaneIDs = []string{"pane-1", "pane-2"}
	terminal.Snapshot = &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 80, Rows: 24},
		Cursor:     protocol.CursorState{Visible: true},
	}
	ownerBinding := rt.BindPane("pane-1")
	ownerBinding.Channel = 1
	ownerBinding.Connected = true
	ownerBinding.Role = runtime.BindingRoleOwner
	followerBinding := rt.BindPane("pane-2")
	followerBinding.Channel = 2
	followerBinding.Connected = true
	followerBinding.Role = runtime.BindingRoleFollower

	manager := NewManager(rt, SessionLeaseHooks{})
	if err := manager.Sync(context.Background(), SyncRequest{
		PaneID:                   "pane-2",
		TerminalID:               "term-1",
		ImplicitInteractiveOwner: true,
	}); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if terminal.OwnerPaneID != "pane-2" {
		t.Fatalf("expected local ownership moved to pane-2, got %q", terminal.OwnerPaneID)
	}
	if ownerBinding.Role != runtime.BindingRoleFollower || followerBinding.Role != runtime.BindingRoleOwner {
		t.Fatalf("expected local ownership roles swapped, owner=%#v follower=%#v", ownerBinding, followerBinding)
	}
	if len(client.acquireLeaseCalls) != 0 {
		t.Fatalf("expected local ownership path not to hit session lease rpc, got %#v", client.acquireLeaseCalls)
	}
}

func TestManagerSyncResizeIfNeededForcesPendingOwnerResize(t *testing.T) {
	client := &fakeClient{}
	rt := runtime.New(client)
	terminal := rt.Registry().GetOrCreate("term-1")
	terminal.Snapshot = &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 40, Rows: 20},
	}
	terminal.PendingOwnerResize = true
	binding := rt.BindPane("pane-1")
	binding.Channel = 1
	binding.Connected = true
	terminal.BoundPaneIDs = []string{"pane-1"}
	terminal.OwnerPaneID = "pane-1"
	terminal.ControlPaneID = "pane-1"

	manager := NewManager(rt, SessionLeaseHooks{})
	if err := manager.Sync(context.Background(), SyncRequest{
		PaneID:         "pane-1",
		TerminalID:     "term-1",
		TargetCols:     40,
		TargetRows:     20,
		ResizeIfNeeded: true,
	}); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(client.resizes) != 1 {
		t.Fatalf("expected forced resize despite matching size, got %#v", client.resizes)
	}
}

type fakeClient struct {
	acquireLeaseCalls []sessionstore.AcquireLeaseParams
	releaseLeaseCalls []sessionstore.ReleaseLeaseParams
	releaseLeaseErr   error
	resizes           []resizeCall
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
func (c *fakeClient) Attach(context.Context, protocol.AttachParams) (*protocol.AttachResult, error) {
	return nil, nil
}

func (c *fakeClient) EnsureResize(_ context.Context, params protocol.EnsureResizeParams) (*protocol.EnsureResizeResult, error) {
	c.resizes = append(c.resizes, resizeCall{channel: params.Channel, cols: params.Cols, rows: params.Rows})
	return &protocol.EnsureResizeResult{
		ResizeControl: &protocol.ResizeControl{CanResize: true, Reason: protocol.ResizeControlReasonOwner},
		Size:          protocol.Size{Cols: params.Cols, Rows: params.Rows},
		Resized:       true,
	}, nil
}

func (c *fakeClient) Snapshot(context.Context, string, int, int) (*protocol.Snapshot, error) {
	return nil, nil
}

func (c *fakeClient) GridViewport(context.Context, string, int, int, int) (*protocol.GridViewport, error) {
	return nil, nil
}

func (c *fakeClient) HistoryWindow(context.Context, protocol.HistoryWindowParams) (*protocol.HistoryWindow, error) {
	return nil, nil
}

func (c *fakeClient) Input(context.Context, uint16, []byte) error { return nil }
func (c *fakeClient) Resize(_ context.Context, channel uint16, cols, rows uint16) error {
	c.resizes = append(c.resizes, resizeCall{channel: channel, cols: cols, rows: rows})
	return nil
}
func (c *fakeClient) StreamReady(context.Context, uint16, uint64) error { return nil }
func (c *fakeClient) Stream(uint16) (<-chan protocol.StreamFrame, func()) {
	ch := make(chan protocol.StreamFrame)
	close(ch)
	return ch, func() {}
}
func (c *fakeClient) Kill(context.Context, string) error    { return nil }
func (c *fakeClient) Remove(context.Context, string) error  { return nil }
func (c *fakeClient) Restart(context.Context, string) error { return nil }
func (c *fakeClient) AcquireLease(_ context.Context, params sessionstore.AcquireLeaseParams) (*sessionstore.LeaseInfo, error) {
	c.acquireLeaseCalls = append(c.acquireLeaseCalls, params)
	return &sessionstore.LeaseInfo{
		TerminalID: params.TerminalID,
		SessionID:  params.SessionID,
		ViewID:     params.ViewID,
		PaneID:     params.PaneID,
	}, nil
}
func (c *fakeClient) ReleaseLease(_ context.Context, params sessionstore.ReleaseLeaseParams) error {
	c.releaseLeaseCalls = append(c.releaseLeaseCalls, params)
	return c.releaseLeaseErr
}

func currentLeases(leases map[string]sessionstore.LeaseInfo) []sessionstore.LeaseInfo {
	if len(leases) == 0 {
		return nil
	}
	result := make([]sessionstore.LeaseInfo, 0, len(leases))
	for _, lease := range leases {
		result = append(result, lease)
	}
	return result
}
