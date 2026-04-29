package sessionruntime

import (
	"context"
	"errors"
	"testing"

	"github.com/lozzow/termx/termx-core/protocol"
	"github.com/lozzow/termx/tuiv2/bridge"
	"github.com/lozzow/termx/tuiv2/runtime"
)

const testSnapshotScrollbackLimit = 500

func TestManagerReconcileUnbindsRemovedAndAttachesNewBindings(t *testing.T) {
	client := &fakeClient{
		attachResult: &protocol.AttachResult{Channel: 7, Mode: "collaborator"},
		snapshotByID: map[string]*protocol.Snapshot{
			"term-new": {
				TerminalID: "term-new",
				Size:       protocol.Size{Cols: 80, Rows: 24},
			},
		},
	}
	rt := runtime.New(client)
	oldTerminal := rt.Registry().GetOrCreate("term-old")
	oldTerminal.BoundPaneIDs = []string{"pane-1"}
	oldBinding := rt.BindPane("pane-1")
	oldBinding.Channel = 1
	oldBinding.Connected = true

	manager := NewManager(rt, testSnapshotScrollbackLimit)
	manager.Reconcile(context.Background(),
		map[string]string{"pane-1": "term-old"},
		map[string]string{"pane-2": "term-new"},
	)

	if got := rt.Binding("pane-1"); got != nil {
		t.Fatalf("expected pane-1 unbound after reconcile, got %#v", got)
	}
	if got := oldTerminal.BoundPaneIDs; len(got) != 0 {
		t.Fatalf("expected old terminal bindings cleared, got %#v", got)
	}
	if len(client.attachCalls) != 1 || client.attachCalls[0].terminalID != "term-new" {
		t.Fatalf("expected attach for new binding, got %#v", client.attachCalls)
	}
	if len(client.snapshotCalls) != 1 || client.snapshotCalls[0] != "term-new" {
		t.Fatalf("expected reconcile attach to bootstrap snapshot for term-new, got %#v", client.snapshotCalls)
	}
	if binding := rt.Binding("pane-2"); binding == nil || !binding.Connected {
		t.Fatalf("expected pane-2 attached and connected, got %#v", binding)
	}
}

func TestManagerReconcileSkipsUnchangedConnectedBinding(t *testing.T) {
	client := &fakeClient{
		attachResult: &protocol.AttachResult{Channel: 7, Mode: "collaborator"},
		snapshotByID: map[string]*protocol.Snapshot{},
	}
	rt := runtime.New(client)
	binding := rt.BindPane("pane-1")
	binding.Channel = 1
	binding.Connected = true
	rt.Registry().GetOrCreate("term-1").BoundPaneIDs = []string{"pane-1"}

	manager := NewManager(rt, testSnapshotScrollbackLimit)
	manager.Reconcile(context.Background(),
		map[string]string{"pane-1": "term-1"},
		map[string]string{"pane-1": "term-1"},
	)

	if len(client.attachCalls) != 0 {
		t.Fatalf("expected unchanged connected binding to skip attach, got %#v", client.attachCalls)
	}
}

func TestManagerReconcileReattachesDisconnectedBinding(t *testing.T) {
	client := &fakeClient{
		attachResult: &protocol.AttachResult{Channel: 7, Mode: "collaborator"},
		snapshotByID: map[string]*protocol.Snapshot{
			"term-1": {
				TerminalID: "term-1",
				Size:       protocol.Size{Cols: 80, Rows: 24},
			},
		},
	}
	rt := runtime.New(client)
	binding := rt.BindPane("pane-1")
	binding.Channel = 1
	binding.Connected = false
	rt.Registry().GetOrCreate("term-1").BoundPaneIDs = []string{"pane-1"}

	manager := NewManager(rt, testSnapshotScrollbackLimit)
	manager.Reconcile(context.Background(),
		map[string]string{"pane-1": "term-1"},
		map[string]string{"pane-1": "term-1"},
	)

	if len(client.attachCalls) != 1 || client.attachCalls[0].terminalID != "term-1" {
		t.Fatalf("expected disconnected binding to reattach term-1, got %#v", client.attachCalls)
	}
	if len(client.snapshotCalls) != 1 || client.snapshotCalls[0] != "term-1" {
		t.Fatalf("expected disconnected reconcile to reload snapshot, got %#v", client.snapshotCalls)
	}
	if got := rt.Binding("pane-1"); got == nil || !got.Connected {
		t.Fatalf("expected pane-1 binding reconnected after reconcile, got %#v", got)
	}
}

func TestManagerReconcileRollsBackTargetAttachOnSnapshotFailure(t *testing.T) {
	client := &fakeClient{
		attachResult: &protocol.AttachResult{Channel: 7, Mode: "collaborator"},
		snapshotErr:  errors.New("snapshot failed"),
	}
	rt := runtime.New(client)
	terminal := rt.Registry().GetOrCreate("term-old")
	terminal.OwnerPaneID = "pane-1"
	terminal.ControlPaneID = "pane-1"
	terminal.BoundPaneIDs = []string{"pane-1"}
	binding := rt.BindPane("pane-1")
	binding.Channel = 3
	binding.Connected = false
	binding.Role = runtime.BindingRoleOwner

	manager := NewManager(rt, testSnapshotScrollbackLimit)
	manager.Reconcile(context.Background(),
		map[string]string{"pane-1": "term-old"},
		map[string]string{"pane-1": "term-new"},
	)

	if got := rt.Binding("pane-1"); got != nil {
		t.Fatalf("expected failed reconcile attach not to leave a runtime binding behind, got %#v", got)
	}
	if len(terminal.BoundPaneIDs) != 0 || terminal.OwnerPaneID != "" || terminal.ControlPaneID != "" {
		t.Fatalf("expected old terminal to stay detached after failed reconcile replacement, got %#v", terminal)
	}
	if target := rt.Registry().Get("term-new"); target != nil && len(target.BoundPaneIDs) != 0 {
		t.Fatalf("expected failed target terminal to release pane binding, got %#v", target)
	}
}

func TestManagerReconcileRollsBackTargetAttachOnStreamStartFailure(t *testing.T) {
	client := &fakeClient{
		attachResult: &protocol.AttachResult{Channel: 0, Mode: "collaborator"},
		snapshotByID: map[string]*protocol.Snapshot{
			"term-new": {
				TerminalID: "term-new",
				Size:       protocol.Size{Cols: 80, Rows: 24},
			},
		},
	}
	rt := runtime.New(client)
	terminal := rt.Registry().GetOrCreate("term-old")
	terminal.OwnerPaneID = "pane-1"
	terminal.ControlPaneID = "pane-1"
	terminal.BoundPaneIDs = []string{"pane-1"}
	binding := rt.BindPane("pane-1")
	binding.Channel = 3
	binding.Connected = true
	binding.Role = runtime.BindingRoleOwner

	manager := NewManager(rt, testSnapshotScrollbackLimit)
	manager.Reconcile(context.Background(),
		map[string]string{"pane-1": "term-old"},
		map[string]string{"pane-1": "term-new"},
	)

	if got := rt.Binding("pane-1"); got != nil {
		t.Fatalf("expected failed stream start not to leave a runtime binding behind, got %#v", got)
	}
	if len(terminal.BoundPaneIDs) != 0 || terminal.OwnerPaneID != "" || terminal.ControlPaneID != "" {
		t.Fatalf("expected old terminal to stay detached after failed reconcile replacement, got %#v", terminal)
	}
	if target := rt.Registry().Get("term-new"); target != nil {
		if target.Channel != 0 || target.AttachMode != "" || len(target.BoundPaneIDs) != 0 {
			t.Fatalf("expected target terminal attachment state rolled back, got %#v", target)
		}
	}
}

type fakeClient struct {
	attachResult  *protocol.AttachResult
	attachErr     error
	snapshotByID  map[string]*protocol.Snapshot
	snapshotErr   error
	attachCalls   []attachCall
	snapshotCalls []string
}

type attachCall struct {
	terminalID string
	mode       string
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

func (c *fakeClient) Attach(_ context.Context, terminalID, mode string) (*protocol.AttachResult, error) {
	c.attachCalls = append(c.attachCalls, attachCall{terminalID: terminalID, mode: mode})
	if c.attachErr != nil {
		return nil, c.attachErr
	}
	return c.attachResult, nil
}

func (c *fakeClient) Snapshot(_ context.Context, terminalID string, _ int, _ int) (*protocol.Snapshot, error) {
	c.snapshotCalls = append(c.snapshotCalls, terminalID)
	if c.snapshotErr != nil {
		return nil, c.snapshotErr
	}
	return c.snapshotByID[terminalID], nil
}

func (c *fakeClient) Input(context.Context, uint16, []byte) error { return nil }

func (c *fakeClient) Resize(context.Context, uint16, uint16, uint16) error { return nil }

func (c *fakeClient) Stream(uint16) (<-chan protocol.StreamFrame, func()) {
	ch := make(chan protocol.StreamFrame)
	close(ch)
	return ch, func() {}
}

func (c *fakeClient) Kill(context.Context, string) error { return nil }

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
