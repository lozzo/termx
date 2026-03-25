package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tui/app"
	"github.com/lozzow/termx/tui/state/types"
)

type stubClient struct {
	createResult *protocol.CreateResult
	attachResult *protocol.AttachResult
	snapshot     *protocol.Snapshot

	lastCreateCommand []string
	lastCreateName    string
	lastAttachID      string
	lastAttachMode    string
	lastKilledID      string
	lastRemovedID     string
	lastMetadataID    string
	lastMetadataName  string
	lastMetadataTags  map[string]string
	attachErr         error
	snapshotErr       error
	attachErrByID     map[string]error
	snapshotErrByID   map[string]error
	listResult        *protocol.ListResult
}

func (c *stubClient) Close() error { return nil }

func (c *stubClient) Create(_ context.Context, command []string, name string, size protocol.Size) (*protocol.CreateResult, error) {
	c.lastCreateCommand = append([]string(nil), command...)
	c.lastCreateName = name
	if c.createResult != nil {
		return c.createResult, nil
	}
	return &protocol.CreateResult{TerminalID: "term-1", State: "running"}, nil
}

func (c *stubClient) SetTags(context.Context, string, map[string]string) error { return nil }
func (c *stubClient) SetMetadata(_ context.Context, terminalID string, name string, tags map[string]string) error {
	c.lastMetadataID = terminalID
	c.lastMetadataName = name
	c.lastMetadataTags = cloneStubTags(tags)
	return nil
}
func (c *stubClient) List(context.Context) (*protocol.ListResult, error) {
	if c.listResult != nil {
		return c.listResult, nil
	}
	return &protocol.ListResult{}, nil
}
func (c *stubClient) Events(context.Context, protocol.EventsParams) (<-chan protocol.Event, error) {
	ch := make(chan protocol.Event)
	close(ch)
	return ch, nil
}

func (c *stubClient) Attach(_ context.Context, terminalID string, mode string) (*protocol.AttachResult, error) {
	c.lastAttachID = terminalID
	c.lastAttachMode = mode
	if err, ok := c.attachErrByID[terminalID]; ok {
		return nil, err
	}
	if c.attachErr != nil {
		return nil, c.attachErr
	}
	if c.attachResult != nil {
		return c.attachResult, nil
	}
	return &protocol.AttachResult{Channel: 7, Mode: mode}, nil
}

func (c *stubClient) Snapshot(_ context.Context, terminalID string, _ int, _ int) (*protocol.Snapshot, error) {
	if err, ok := c.snapshotErrByID[terminalID]; ok {
		return nil, err
	}
	if c.snapshotErr != nil {
		return nil, c.snapshotErr
	}
	if c.snapshot != nil {
		return c.snapshot, nil
	}
	return &protocol.Snapshot{TerminalID: "term-1", Size: protocol.Size{Cols: 80, Rows: 24}}, nil
}

func (c *stubClient) Input(context.Context, uint16, []byte) error          { return nil }
func (c *stubClient) Resize(context.Context, uint16, uint16, uint16) error { return nil }
func (c *stubClient) Stream(uint16) (<-chan protocol.StreamFrame, func()) {
	ch := make(chan protocol.StreamFrame)
	return ch, func() { close(ch) }
}
func (c *stubClient) Kill(_ context.Context, terminalID string) error {
	c.lastKilledID = terminalID
	return nil
}

func (c *stubClient) Remove(_ context.Context, terminalID string) error {
	c.lastRemovedID = terminalID
	return nil
}

func cloneStubTags(tags map[string]string) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	out := make(map[string]string, len(tags))
	for key, value := range tags {
		out[key] = value
	}
	return out
}

func TestBootstrapCreatesTemporaryWorkspaceWithLiveShellPane(t *testing.T) {
	client := &stubClient{}
	model, err := Bootstrap(context.Background(), client, BootstrapConfig{
		DefaultShell: "/bin/sh",
		Workspace:    "main",
	})
	if err != nil {
		t.Fatalf("Bootstrap returned error: %v", err)
	}

	tab := model.Workspace.ActiveTab()
	pane, ok := tab.ActivePane()
	if !ok {
		t.Fatal("expected active pane")
	}
	if pane.TerminalID == "" {
		t.Fatal("expected default pane to attach a terminal")
	}
	if pane.TerminalID != types.TerminalID("term-1") {
		t.Fatalf("expected terminal id term-1, got %q", pane.TerminalID)
	}
	if session, ok := model.Sessions[pane.TerminalID]; !ok || session.Channel != 7 {
		t.Fatalf("expected live session for %q, got %#v", pane.TerminalID, session)
	}
}

func TestBootstrapAttachIDHydratesMetadataFromDaemonTruth(t *testing.T) {
	client := &stubClient{
		listResult: &protocol.ListResult{
			Terminals: []protocol.TerminalInfo{{
				ID:      "term-9",
				Name:    "api-dev",
				Command: []string{"bash", "-lc", "npm run dev"},
				Tags:    map[string]string{"env": "dev"},
				State:   "exited",
				Size:    protocol.Size{Cols: 100, Rows: 30},
			}},
		},
		attachResult: &protocol.AttachResult{Channel: 8, Mode: "rw"},
		snapshot:     &protocol.Snapshot{TerminalID: "term-9", Size: protocol.Size{Cols: 100, Rows: 30}},
	}

	model, err := Bootstrap(context.Background(), client, BootstrapConfig{
		Workspace: "main",
		AttachID:  "term-9",
	})
	if err != nil {
		t.Fatalf("Bootstrap returned error: %v", err)
	}

	meta := model.Terminals[types.TerminalID("term-9")]
	if meta.Name != "api-dev" {
		t.Fatalf("expected daemon name, got %#v", meta)
	}
	if len(meta.Command) != 3 || meta.Command[2] != "npm run dev" {
		t.Fatalf("expected daemon command, got %#v", meta.Command)
	}
	if meta.State != "exited" || meta.Tags["env"] != "dev" {
		t.Fatalf("expected daemon metadata, got %#v", meta)
	}
}

func TestBootstrapAttachIDExitedTerminalKeepsPaneAndSessionStateConsistent(t *testing.T) {
	client := &stubClient{
		listResult: &protocol.ListResult{
			Terminals: []protocol.TerminalInfo{{
				ID:      "term-9",
				Name:    "api-dev",
				Command: []string{"bash", "-lc", "npm run dev"},
				State:   "exited",
			}},
		},
		attachResult: &protocol.AttachResult{Channel: 8, Mode: "rw"},
		snapshot:     &protocol.Snapshot{TerminalID: "term-9", Size: protocol.Size{Cols: 100, Rows: 30}},
	}

	model, err := Bootstrap(context.Background(), client, BootstrapConfig{
		Workspace: "main",
		AttachID:  "term-9",
	})
	if err != nil {
		t.Fatalf("Bootstrap returned error: %v", err)
	}

	pane, _ := model.Workspace.ActiveTab().ActivePane()
	if pane.SlotState != types.PaneSlotExited {
		t.Fatalf("expected exited pane slot, got %+v", pane)
	}
	session := model.Sessions[types.TerminalID("term-9")]
	if session.Attached {
		t.Fatalf("expected exited bootstrap session to stay detached, got %#v", session)
	}
	if model.Terminals[types.TerminalID("term-9")].State != "exited" {
		t.Fatalf("expected metadata exited state, got %#v", model.Terminals[types.TerminalID("term-9")])
	}
}

func TestBootstrappedModelUpdateCreateFailureCleansRemoteTerminalAndKeepsPaneUnbound(t *testing.T) {
	client := &stubClient{
		createResult: &protocol.CreateResult{TerminalID: "term-created", State: "running"},
		attachErrByID: map[string]error{
			"term-created": errors.New("attach failed"),
		},
		listResult:   &protocol.ListResult{Terminals: []protocol.TerminalInfo{{ID: "term-1", Name: "shell", Command: []string{"/bin/sh"}, State: "running"}}},
		attachResult: &protocol.AttachResult{Channel: 7, Mode: "rw"},
		snapshot:     &protocol.Snapshot{TerminalID: "term-1", Size: protocol.Size{Cols: 80, Rows: 24}},
	}
	model, err := Bootstrap(context.Background(), client, BootstrapConfig{
		Workspace: "main",
		AttachID:  "term-1",
	})
	if err != nil {
		t.Fatalf("Bootstrap returned error: %v", err)
	}

	teaModel, _ := model.Update(app.IntentMessage{Intent: app.IntentSplitVertical})
	split := teaModel.(app.Model)
	teaModel, _ = split.Update(app.IntentMessage{Intent: app.ConfirmCreateTerminalIntent{
		Command: []string{"/bin/sh"},
		Name:    "shell-2",
	}})
	updated := teaModel.(app.Model)
	pane, _ := updated.Workspace.ActiveTab().ActivePane()
	if pane.TerminalID != "" || pane.SlotState != types.PaneSlotUnconnected {
		t.Fatalf("expected pane to stay unbound after create cleanup, got %+v", pane)
	}
	if client.lastKilledID != "term-created" {
		t.Fatalf("expected created terminal cleanup kill, got %q", client.lastKilledID)
	}
	if updated.Notice == nil || updated.Notice.Message == "" {
		t.Fatal("expected user notice for create failure")
	}
}

func TestBootstrappedModelUpdateExecutesCreateIntentThroughRuntime(t *testing.T) {
	client := &stubClient{
		createResult: &protocol.CreateResult{TerminalID: "term-created", State: "running"},
		attachResult: &protocol.AttachResult{Channel: 12, Mode: "rw"},
		snapshot:     &protocol.Snapshot{TerminalID: "term-created", Size: protocol.Size{Cols: 80, Rows: 24}},
	}
	model, err := Bootstrap(context.Background(), client, BootstrapConfig{
		DefaultShell: "/bin/sh",
		Workspace:    "main",
		AttachID:     "term-1",
	})
	if err != nil {
		t.Fatalf("Bootstrap returned error: %v", err)
	}

	teaModel, _ := model.Update(app.IntentMessage{Intent: app.IntentSplitVertical})
	split, ok := teaModel.(app.Model)
	if !ok {
		t.Fatalf("expected app.Model after split, got %T", teaModel)
	}
	teaModel, _ = split.Update(app.IntentMessage{Intent: app.ConfirmCreateTerminalIntent{
		Command: []string{"/bin/sh"},
		Name:    "shell-2",
	}})
	updated, ok := teaModel.(app.Model)
	if !ok {
		t.Fatalf("expected app.Model after create, got %T", teaModel)
	}
	pane, _ := updated.Workspace.ActiveTab().ActivePane()
	if pane.TerminalID != types.TerminalID("term-created") || pane.SlotState != types.PaneSlotLive {
		t.Fatalf("expected runtime create to bind live pane, got %+v", pane)
	}
	if client.lastCreateName != "shell-2" || client.lastAttachID != "term-created" {
		t.Fatalf("expected runtime create path to reach client, got create=%q attach=%q", client.lastCreateName, client.lastAttachID)
	}
}

func TestBootstrappedModelUpdateExecutesKillIntentThroughRuntime(t *testing.T) {
	client := &stubClient{
		attachResult: &protocol.AttachResult{Channel: 7, Mode: "rw"},
		snapshot:     &protocol.Snapshot{TerminalID: "term-1", Size: protocol.Size{Cols: 80, Rows: 24}},
	}
	model, err := Bootstrap(context.Background(), client, BootstrapConfig{
		DefaultShell: "/bin/sh",
		Workspace:    "main",
		AttachID:     "term-1",
	})
	if err != nil {
		t.Fatalf("Bootstrap returned error: %v", err)
	}

	teaModel, _ := model.Update(app.IntentMessage{Intent: app.IntentClosePaneAndKillTerminal})
	updated, ok := teaModel.(app.Model)
	if !ok {
		t.Fatalf("expected app.Model after kill, got %T", teaModel)
	}
	if client.lastKilledID != "term-1" {
		t.Fatalf("expected runtime kill path to reach client, got %q", client.lastKilledID)
	}
	if updated.Terminals[types.TerminalID("term-1")].State != "exited" {
		t.Fatalf("expected terminal exited after runtime kill, got %#v", updated.Terminals[types.TerminalID("term-1")])
	}
}
