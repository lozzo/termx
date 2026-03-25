package runtime

import (
	"context"
	"testing"

	"github.com/lozzow/termx/protocol"
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
func (c *stubClient) SetMetadata(context.Context, string, string, map[string]string) error {
	return nil
}
func (c *stubClient) List(context.Context) (*protocol.ListResult, error) { return &protocol.ListResult{}, nil }
func (c *stubClient) Events(context.Context, protocol.EventsParams) (<-chan protocol.Event, error) {
	ch := make(chan protocol.Event)
	close(ch)
	return ch, nil
}

func (c *stubClient) Attach(_ context.Context, terminalID string, mode string) (*protocol.AttachResult, error) {
	c.lastAttachID = terminalID
	c.lastAttachMode = mode
	if c.attachResult != nil {
		return c.attachResult, nil
	}
	return &protocol.AttachResult{Channel: 7, Mode: mode}, nil
}

func (c *stubClient) Snapshot(context.Context, string, int, int) (*protocol.Snapshot, error) {
	if c.snapshot != nil {
		return c.snapshot, nil
	}
	return &protocol.Snapshot{TerminalID: "term-1", Size: protocol.Size{Cols: 80, Rows: 24}}, nil
}

func (c *stubClient) Input(context.Context, uint16, []byte) error { return nil }
func (c *stubClient) Resize(context.Context, uint16, uint16, uint16) error { return nil }
func (c *stubClient) Stream(uint16) (<-chan protocol.StreamFrame, func()) {
	ch := make(chan protocol.StreamFrame)
	return ch, func() { close(ch) }
}
func (c *stubClient) Kill(context.Context, string) error { return nil }

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
