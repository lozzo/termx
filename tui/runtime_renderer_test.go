package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/protocol"
	btui "github.com/lozzow/termx/tui/bt"
	layoutresolvedomain "github.com/lozzow/termx/tui/domain/layoutresolve"
	promptdomain "github.com/lozzow/termx/tui/domain/prompt"
	terminalmanagerdomain "github.com/lozzow/termx/tui/domain/terminalmanager"
	terminalpickerdomain "github.com/lozzow/termx/tui/domain/terminalpicker"
	workspacedomain "github.com/lozzow/termx/tui/domain/workspace"
	"github.com/lozzow/termx/tui/domain/types"
)

func TestRuntimeRendererRendersActivePaneSnapshot(t *testing.T) {
	state := connectedRunAppState()
	state.Domain.Terminals[types.TerminalID("term-1")] = types.TerminalRef{
		ID:      types.TerminalID("term-1"),
		Name:    "api-dev",
		State:   types.TerminalRunStateRunning,
		Command: []string{"npm", "run", "dev"},
		Tags:    map[string]string{"env": "dev", "service": "api"},
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
	if !strings.Contains(view, "terminal_state: running") {
		t.Fatalf("expected terminal state in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "terminal_command: npm run dev") {
		t.Fatalf("expected terminal command in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "terminal_tags: env=dev,service=api") {
		t.Fatalf("expected terminal tags in rendered view, got:\n%s", view)
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

func TestRuntimeRendererRendersTerminalManagerOverlay(t *testing.T) {
	state := runtimeStateWithTerminalManagerTargets()
	manager := terminalmanagerdomain.NewState(state.Domain, state.UI.Focus)
	state.UI.Overlay = types.OverlayState{
		Kind: types.OverlayTerminalManager,
		Data: manager,
	}
	state.UI.Focus.Layer = types.FocusLayerOverlay

	view := runtimeRenderer{}.Render(state, nil)
	if !strings.Contains(view, "terminal_manager_query: ") {
		t.Fatalf("expected manager query in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "terminal_manager_rows:") {
		t.Fatalf("expected manager rows section in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "> [terminal] api-dev") {
		t.Fatalf("expected selected manager row in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "terminal_manager_detail: api-dev") {
		t.Fatalf("expected manager detail header in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "detail_command: npm run dev") {
		t.Fatalf("expected manager detail command in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "detail_connected_panes: 1") {
		t.Fatalf("expected manager detail connection count in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "detail_locations:") || !strings.Contains(view, "- main/shell/pane:pane-1") {
		t.Fatalf("expected manager detail locations in rendered view, got:\n%s", view)
	}
}

func TestRuntimeRendererRendersPromptOverlay(t *testing.T) {
	state := runtimeStateWithTerminalManagerTargets()
	state.UI.Overlay = types.OverlayState{
		Kind: types.OverlayPrompt,
		Data: &promptdomain.State{
			Kind:       promptdomain.KindEditTerminalMetadata,
			Title:      "edit terminal metadata",
			TerminalID: types.TerminalID("term-2"),
			Fields: []promptdomain.Field{
				{Key: "name", Label: "Name", Value: "build-log"},
				{Key: "tags", Label: "Tags", Value: "group=build"},
			},
			Active: 1,
		},
	}
	state.UI.Focus.Layer = types.FocusLayerPrompt

	view := runtimeRenderer{}.Render(state, nil)
	if !strings.Contains(view, "prompt_title: edit terminal metadata") {
		t.Fatalf("expected prompt title in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "prompt_fields:") {
		t.Fatalf("expected prompt fields section in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "  [name] Name: build-log") {
		t.Fatalf("expected name field in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "> [tags] Tags: group=build") {
		t.Fatalf("expected active tags field in rendered view, got:\n%s", view)
	}
}

func TestRuntimeRendererRendersTerminalPickerOverlay(t *testing.T) {
	state := runtimeStateWithTerminalManagerTargets()
	picker := terminalpickerdomain.NewState(state.Domain, state.UI.Focus)
	picker.AppendQuery("ops")
	state.UI.Overlay = types.OverlayState{
		Kind: types.OverlayTerminalPicker,
		Data: picker,
	}
	state.UI.Focus.Layer = types.FocusLayerOverlay

	view := runtimeRenderer{}.Render(state, nil)
	if !strings.Contains(view, "terminal_picker_query: ops") {
		t.Fatalf("expected picker query in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "terminal_picker_rows:") {
		t.Fatalf("expected picker rows section in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "> [terminal] ops-watch") {
		t.Fatalf("expected selected picker row in rendered view, got:\n%s", view)
	}
}

func TestRuntimeRendererRendersLayoutResolveOverlay(t *testing.T) {
	state := buildSinglePaneAppState("main", "shell", types.PaneSlotWaiting)
	resolve := layoutresolvedomain.NewState(types.PaneID("pane-1"), "backend-dev", "env=dev service=api")
	resolve.MoveSelection(1)
	state.UI.Overlay = types.OverlayState{
		Kind: types.OverlayLayoutResolve,
		Data: resolve,
	}
	state.UI.Focus.Layer = types.FocusLayerOverlay

	view := runtimeRenderer{}.Render(state, nil)
	if !strings.Contains(view, "layout_resolve_role: backend-dev") {
		t.Fatalf("expected resolve role in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "layout_resolve_hint: env=dev service=api") {
		t.Fatalf("expected resolve hint in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "layout_resolve_rows:") {
		t.Fatalf("expected resolve rows section in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "> [create_new] create new") {
		t.Fatalf("expected selected resolve row in rendered view, got:\n%s", view)
	}
}

func TestRuntimeRendererRendersActiveMode(t *testing.T) {
	state := buildSinglePaneAppState("main", "shell", types.PaneSlotEmpty)
	deadline := time.Date(2026, 3, 23, 12, 0, 0, 0, time.UTC)
	state.UI.Mode = types.ModeState{
		Active:     types.ModeGlobal,
		Sticky:     false,
		DeadlineAt: &deadline,
	}

	view := runtimeRenderer{}.Render(state, nil)
	if !strings.Contains(view, "mode: global") {
		t.Fatalf("expected active mode in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "mode_sticky: false") {
		t.Fatalf("expected sticky flag in rendered view, got:\n%s", view)
	}
}

func TestRuntimeRendererRendersFocusLayer(t *testing.T) {
	view := runtimeRenderer{}.Render(buildSinglePaneAppState("main", "shell", types.PaneSlotEmpty), nil)
	if !strings.Contains(view, "focus_layer: tiled") {
		t.Fatalf("expected focus layer in rendered view, got:\n%s", view)
	}
}

func TestRuntimeRendererRendersFocusOverlayTarget(t *testing.T) {
	view := runtimeRenderer{}.Render(runtimeStateWithLayoutResolveTarget(), nil)
	if !strings.Contains(view, "focus_layer: overlay") {
		t.Fatalf("expected overlay focus layer in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "focus_overlay_target: layout_resolve") {
		t.Fatalf("expected overlay focus target in rendered view, got:\n%s", view)
	}
}

func TestRuntimeRendererRendersExitedPaneState(t *testing.T) {
	state := connectedRunAppState()
	exitCode := 7
	ws := state.Domain.Workspaces[types.WorkspaceID("ws-1")]
	tab := ws.Tabs[types.TabID("tab-1")]
	pane := tab.Panes[types.PaneID("pane-1")]
	pane.SlotState = types.PaneSlotExited
	pane.LastExitCode = &exitCode
	tab.Panes[types.PaneID("pane-1")] = pane
	ws.Tabs[types.TabID("tab-1")] = tab
	state.Domain.Workspaces[types.WorkspaceID("ws-1")] = ws
	terminal := state.Domain.Terminals[types.TerminalID("term-1")]
	terminal.State = types.TerminalRunStateExited
	terminal.ExitCode = &exitCode
	state.Domain.Terminals[types.TerminalID("term-1")] = terminal

	view := runtimeRenderer{}.Render(state, nil)
	if !strings.Contains(view, "terminal_state: exited") {
		t.Fatalf("expected exited terminal state in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "terminal_exit_code: 7") {
		t.Fatalf("expected terminal exit code in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "pane_exit_code: 7") {
		t.Fatalf("expected pane exit code in rendered view, got:\n%s", view)
	}
}

func TestRuntimeRendererRendersConnectionRoleOwner(t *testing.T) {
	view := runtimeRenderer{}.Render(connectedRunAppState(), nil)
	if !strings.Contains(view, "connection_role: owner") {
		t.Fatalf("expected owner connection role in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "connected_panes: 1") {
		t.Fatalf("expected connected pane count in rendered view, got:\n%s", view)
	}
}

func TestRuntimeRendererRendersConnectionRoleFollower(t *testing.T) {
	view := runtimeRenderer{}.Render(runtimeStateWithFollowerPaneConnection(), nil)
	if !strings.Contains(view, "connection_role: follower") {
		t.Fatalf("expected follower connection role in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "connected_panes: 2") {
		t.Fatalf("expected shared connected pane count in rendered view, got:\n%s", view)
	}
}
