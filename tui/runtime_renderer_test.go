package tui

import (
	"strings"
	"testing"

	"github.com/lozzow/termx/protocol"
	btui "github.com/lozzow/termx/tui/bt"
	workspacedomain "github.com/lozzow/termx/tui/domain/workspace"
	"github.com/lozzow/termx/tui/domain/types"
)

func TestRuntimeRendererRendersActivePaneSnapshot(t *testing.T) {
	state := connectedRunAppState()
	state.Domain.Terminals[types.TerminalID("term-1")] = types.TerminalRef{
		ID:    types.TerminalID("term-1"),
		Name:  "api-dev",
		State: types.TerminalRunStateRunning,
	}
	renderer := runtimeRenderer{
		Screens: NewRuntimeTerminalStore(RuntimeSessions{
			Terminals: map[types.TerminalID]TerminalRuntimeSession{
				types.TerminalID("term-1"): {
					TerminalID: types.TerminalID("term-1"),
					Snapshot: &protocol.Snapshot{
						TerminalID: "term-1",
						Screen: protocol.ScreenData{
							Cells: [][]protocol.Cell{
								{
									{Content: "$"},
									{Content: " "},
									{Content: "p"},
									{Content: "w"},
									{Content: "d"},
								},
								{
									{Content: "/"},
									{Content: "t"},
									{Content: "m"},
									{Content: "p"},
								},
							},
						},
					},
				},
			},
		}),
	}

	view := renderer.Render(state, nil)
	if !strings.Contains(view, "title: api-dev") {
		t.Fatalf("expected terminal title in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "screen:") {
		t.Fatalf("expected screen section in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "$ pwd") || !strings.Contains(view, "/tmp") {
		t.Fatalf("expected snapshot rows in rendered view, got:\n%s", view)
	}
}

func TestRuntimeRendererSkipsScreenSectionWhenNoSnapshot(t *testing.T) {
	view := runtimeRenderer{}.Render(connectedRunAppState(), nil)
	if strings.Contains(view, "screen:") {
		t.Fatalf("expected renderer without runtime screen store to skip screen section, got:\n%s", view)
	}
}

func TestRuntimeRendererRendersNoticeSection(t *testing.T) {
	view := runtimeRenderer{}.Render(connectedRunAppState(), []btui.Notice{{
		Level: btui.NoticeLevelError,
		Text:  "terminal switched to observer-only mode",
		Count: 2,
	}})

	if !strings.Contains(view, "notices:") {
		t.Fatalf("expected notice section in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "[error] terminal switched to observer-only mode (x2)") {
		t.Fatalf("expected aggregated notice line in rendered view, got:\n%s", view)
	}
}

func TestRuntimeRendererRendersWorkspacePickerOverlay(t *testing.T) {
	state := connectedRunAppState()
	state.Domain.WorkspaceOrder = append(state.Domain.WorkspaceOrder, types.WorkspaceID("ws-2"))
	state.Domain.Workspaces[types.WorkspaceID("ws-2")] = types.WorkspaceState{
		ID:          types.WorkspaceID("ws-2"),
		Name:        "ops",
		ActiveTabID: types.TabID("tab-2"),
		TabOrder:    []types.TabID{types.TabID("tab-2")},
		Tabs: map[types.TabID]types.TabState{
			types.TabID("tab-2"): {
				ID:           types.TabID("tab-2"),
				Name:         "logs",
				ActivePaneID: types.PaneID("pane-2"),
				ActiveLayer:  types.FocusLayerTiled,
				Panes: map[types.PaneID]types.PaneState{
					types.PaneID("pane-2"): {
						ID:        types.PaneID("pane-2"),
						Kind:      types.PaneKindTiled,
						SlotState: types.PaneSlotEmpty,
					},
				},
			},
		},
	}
	picker := workspacedomain.NewPickerState(state.Domain)
	picker.AppendQuery("ops")
	picker.ExpandSelected()
	state.UI.Overlay = types.OverlayState{
		Kind: types.OverlayWorkspacePicker,
		Data: picker,
	}
	state.UI.Focus.Layer = types.FocusLayerOverlay

	view := runtimeRenderer{}.Render(state, nil)
	if !strings.Contains(view, "workspace_picker_query: ops") {
		t.Fatalf("expected picker query in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "workspace_picker_rows:") {
		t.Fatalf("expected picker rows section in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "> [workspace] ops") {
		t.Fatalf("expected selected picker row in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "  [tab] logs") {
		t.Fatalf("expected nested tab row in rendered view, got:\n%s", view)
	}
}
