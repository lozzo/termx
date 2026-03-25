package runtime

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tui/app"
	"github.com/lozzow/termx/tui/state/layout"
	"github.com/lozzow/termx/tui/state/terminal"
	"github.com/lozzow/termx/tui/state/types"
	"github.com/lozzow/termx/tui/state/workspace"
)

type stubTerminalService struct {
	lastInputChannel  uint16
	lastInputData     []byte
	lastResizeChannel uint16
	lastResizeCols    uint16
	lastResizeRows    uint16
}

func (s *stubTerminalService) Create(context.Context, []string, string, protocol.Size) (*protocol.CreateResult, error) {
	return nil, nil
}
func (s *stubTerminalService) Attach(context.Context, string, string) (*protocol.AttachResult, error) {
	return nil, nil
}
func (s *stubTerminalService) Snapshot(context.Context, string, int, int) (*protocol.Snapshot, error) {
	return nil, nil
}
func (s *stubTerminalService) Events(context.Context, protocol.EventsParams) (<-chan protocol.Event, error) {
	return nil, nil
}
func (s *stubTerminalService) Input(_ context.Context, channel uint16, data []byte) error {
	s.lastInputChannel = channel
	s.lastInputData = append([]byte(nil), data...)
	return nil
}
func (s *stubTerminalService) Resize(_ context.Context, channel uint16, cols, rows uint16) error {
	s.lastResizeChannel = channel
	s.lastResizeCols = cols
	s.lastResizeRows = rows
	return nil
}
func (s *stubTerminalService) Stream(uint16) (<-chan protocol.StreamFrame, func()) {
	return nil, func() {}
}
func (s *stubTerminalService) Kill(context.Context, string) error { return nil }

func TestInputRouterSendsKeysToFocusedWorkbenchPaneAndResizesOwnedTerminal(t *testing.T) {
	service := &stubTerminalService{}
	router := NewInputRouter(service)
	state := sampleFocusedLivePaneState()

	if err := router.HandleKey(context.Background(), state, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")}); err != nil {
		t.Fatalf("HandleKey returned error: %v", err)
	}
	if got := string(service.lastInputData); got != "a" || service.lastInputChannel != 7 {
		t.Fatalf("expected key to reach channel 7, got channel=%d data=%q", service.lastInputChannel, got)
	}

	if err := router.HandleResize(context.Background(), state, 120, 40); err != nil {
		t.Fatalf("HandleResize returned error: %v", err)
	}
	if service.lastResizeChannel != 7 || service.lastResizeCols != 120 || service.lastResizeRows != 40 {
		t.Fatalf("expected resize to reach channel 7 with 120x40, got channel=%d size=%dx%d", service.lastResizeChannel, service.lastResizeCols, service.lastResizeRows)
	}
}

func sampleFocusedLivePaneState() app.Model {
	ws := workspace.NewTemporary("main")
	tab := ws.ActiveTab()
	tab.Layout = layout.NewLeaf(types.PaneID("pane-1"))
	tab.TrackPane(workspace.PaneState{
		ID:         types.PaneID("pane-1"),
		Kind:       types.PaneKindTiled,
		SlotState:  types.PaneSlotLive,
		TerminalID: types.TerminalID("term-1"),
	})
	tab.ActivePaneID = types.PaneID("pane-1")

	model := app.NewModel()
	model.Workspace = ws
	model.Terminals = map[types.TerminalID]terminal.Metadata{
		types.TerminalID("term-1"): {ID: types.TerminalID("term-1"), Name: "shell", State: terminal.StateRunning},
	}
	model.Sessions = map[types.TerminalID]app.TerminalSession{
		types.TerminalID("term-1"): {
			TerminalID: types.TerminalID("term-1"),
			Channel:    7,
			Attached:   true,
		},
	}
	return model
}
