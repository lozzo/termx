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
	"github.com/lozzow/termx/tui/domain/types"
	workspacedomain "github.com/lozzow/termx/tui/domain/workspace"
)

func TestRuntimeRendererRendersActivePaneSnapshot(t *testing.T) {
	state := connectedRunAppState()
	state.Domain.Terminals[types.TerminalID("term-1")] = types.TerminalRef{
		ID:      types.TerminalID("term-1"),
		Name:    "api-dev",
		State:   types.TerminalRunStateRunning,
		Command: []string{"npm", "run", "dev"},
		Tags:    map[string]string{"env": "dev", "service": "api"},
		Visible: true,
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
	if !strings.Contains(view, "termx") || !strings.Contains(view, "header_bar: ws=main | tab=shell | pane=pane-1 | slot=connected | overlay=none | focus=tiled") {
		t.Fatalf("expected renderer header bar, got:\n%s", view)
	}
	if !strings.Contains(view, "chrome_header:") || !strings.Contains(view, "chrome_body:") || !strings.Contains(view, "chrome_footer:") {
		t.Fatalf("expected chrome wrappers in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "body_bar: terminal=term-1:running | screen=preview:2/2 | overlay=none") {
		t.Fatalf("expected body bar in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "terminal_bar: id=term-1 | title=api-dev | state=running | role=owner") {
		t.Fatalf("expected terminal bar in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "screen_bar: state=preview | rows=2/2") {
		t.Fatalf("expected screen bar in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "overlay_bar: kind=none") {
		t.Fatalf("expected overlay bar in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "section_status:") || !strings.Contains(view, "section_terminal:") || !strings.Contains(view, "section_screen:") {
		t.Fatalf("expected renderer sections for status/terminal/screen, got:\n%s", view)
	}
	if !(strings.Index(view, "chrome_header:") < strings.Index(view, "section_status:") &&
		strings.Index(view, "section_status:") < strings.Index(view, "chrome_body:") &&
		strings.Index(view, "chrome_body:") < strings.Index(view, "section_terminal:") &&
		strings.Index(view, "section_overlay:") < strings.Index(view, "chrome_footer:") &&
		strings.Index(view, "chrome_footer:") < strings.Index(view, "section_notices:")) {
		t.Fatalf("expected header/body/footer ordering in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "title: api-dev") {
		t.Fatalf("expected terminal title in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "tab_layer: tiled") {
		t.Fatalf("expected tab layer in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "pane_kind: tiled") {
		t.Fatalf("expected pane kind in rendered view, got:\n%s", view)
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
	if !strings.Contains(view, "terminal_visibility: true") {
		t.Fatalf("expected terminal visibility in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "screen:") {
		t.Fatalf("expected screen section in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "screen_rows: 2/2") {
		t.Fatalf("expected screen row metadata in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "$ pwd") || !strings.Contains(view, "/tmp") {
		t.Fatalf("expected snapshot rows in rendered view, got:\n%s", view)
	}
	if lines := strings.Count(view, "\n") + 1; lines > 23 {
		t.Fatalf("expected compact active pane view, got %d lines:\n%s", lines, view)
	}
}

func TestRuntimeRendererTruncatesLargeSnapshotPreview(t *testing.T) {
	state := connectedRunAppState()
	state.Domain.Terminals[types.TerminalID("term-1")] = types.TerminalRef{
		ID:      types.TerminalID("term-1"),
		Name:    "api-dev",
		State:   types.TerminalRunStateRunning,
		Command: []string{"npm", "run", "dev"},
		Visible: true,
	}
	rows := make([][]protocol.Cell, 0, 12)
	for i := 0; i < 12; i++ {
		rows = append(rows, []protocol.Cell{{Content: "r"}, {Content: "o"}, {Content: "w"}, {Content: ":"}, {Content: string(rune('0' + i/10))}, {Content: string(rune('0' + i%10))}})
	}
	renderer := runtimeRenderer{
		Screens: NewRuntimeTerminalStore(RuntimeSessions{
			Terminals: map[types.TerminalID]TerminalRuntimeSession{
				types.TerminalID("term-1"): {
					TerminalID: types.TerminalID("term-1"),
					Snapshot: &protocol.Snapshot{
						TerminalID: "term-1",
						Screen:     protocol.ScreenData{Cells: rows},
					},
				},
			},
		}),
	}

	view := renderer.Render(state, nil)
	if !strings.Contains(view, "workspace: main") || !strings.Contains(view, "pane: pane-1") {
		t.Fatalf("expected header lines to stay visible with long snapshot, got:\n%s", view)
	}
	if !strings.Contains(view, "screen_rows: 8/12") || !strings.Contains(view, "screen_truncated: true") {
		t.Fatalf("expected truncated screen metadata, got:\n%s", view)
	}
	if strings.Contains(view, "row:00") || strings.Contains(view, "row:03") {
		t.Fatalf("expected old snapshot rows to be truncated, got:\n%s", view)
	}
	if !strings.Contains(view, "row:04") || !strings.Contains(view, "row:11") {
		t.Fatalf("expected latest snapshot rows to remain visible, got:\n%s", view)
	}
}

func TestRuntimeRendererRendersScreenPlaceholderWhenNoSnapshot(t *testing.T) {
	view := runtimeRenderer{}.Render(connectedRunAppState(), nil)
	if !strings.Contains(view, "section_screen:") || !strings.Contains(view, "screen: <unavailable>") {
		t.Fatalf("expected renderer without runtime screen store to keep screen placeholder, got:\n%s", view)
	}
}

func TestRuntimeRendererRendersStableSectionSkeletonForEmptyPane(t *testing.T) {
	view := runtimeRenderer{}.Render(buildSinglePaneAppState("main", "shell", types.PaneSlotEmpty), nil)
	if !strings.Contains(view, "chrome_body:") || !strings.Contains(view, "chrome_footer:") {
		t.Fatalf("expected chrome body/footer wrappers in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "body_bar: terminal=disconnected | screen=unavailable | overlay=none") {
		t.Fatalf("expected empty-pane body bar in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "terminal_bar: disconnected") {
		t.Fatalf("expected disconnected terminal bar in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "screen_bar: state=unavailable") {
		t.Fatalf("expected unavailable screen bar in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "overlay_bar: kind=none") {
		t.Fatalf("expected overlay none bar in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "footer_bar: notices=0 | overlay=none") {
		t.Fatalf("expected footer bar placeholder in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "section_terminal:") || !strings.Contains(view, "terminal: <disconnected>") {
		t.Fatalf("expected terminal placeholder section in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "section_screen:") || !strings.Contains(view, "screen: <unavailable>") {
		t.Fatalf("expected screen placeholder section in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "section_overlay:") || !strings.Contains(view, "overlay: none") {
		t.Fatalf("expected overlay placeholder section in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "section_notices:") || !strings.Contains(view, "notices: 0") {
		t.Fatalf("expected notice placeholder section in rendered view, got:\n%s", view)
	}
	if !strings.HasSuffix(strings.TrimSpace(view), "notices: 0") {
		t.Fatalf("expected footer notice placeholder to stay at bottom, got:\n%s", view)
	}
}

func TestRuntimeRendererRendersNoticeSection(t *testing.T) {
	view := runtimeRenderer{}.Render(connectedRunAppState(), []btui.Notice{{
		Level: btui.NoticeLevelError,
		Text:  "terminal switched to observer-only mode",
		Count: 2,
	}})

	if !strings.Contains(view, "chrome_footer:") {
		t.Fatalf("expected footer wrapper in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "footer_bar: notices=1 | last=error | overlay=none") {
		t.Fatalf("expected footer status bar in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "section_notices:") {
		t.Fatalf("expected notices section wrapper in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "notices:") {
		t.Fatalf("expected notice section in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "[error] terminal switched to observer-only mode (x2)") {
		t.Fatalf("expected aggregated notice line in rendered view, got:\n%s", view)
	}
}

func TestRuntimeRendererKeepsOverlayAboveFooter(t *testing.T) {
	state := runtimeStateWithTerminalManagerTargets()
	manager := terminalmanagerdomain.NewState(state.Domain, state.UI.Focus)
	state.UI.Overlay = types.OverlayState{
		Kind: types.OverlayTerminalManager,
		Data: manager,
	}
	state.UI.Focus.Layer = types.FocusLayerOverlay

	view := runtimeRenderer{}.Render(state, []btui.Notice{{
		Level: btui.NoticeLevelError,
		Text:  "boom",
	}})
	if !(strings.Index(view, "section_overlay:") < strings.Index(view, "chrome_footer:") &&
		strings.Index(view, "chrome_footer:") < strings.Index(view, "section_notices:")) {
		t.Fatalf("expected overlay to stay in body and notices to stay in footer, got:\n%s", view)
	}
}

func TestRuntimeRendererRendersHeaderBarWithMode(t *testing.T) {
	state := buildSinglePaneAppState("main", "shell", types.PaneSlotWaiting)
	state.UI.Mode = types.ModeState{Active: types.ModeGlobal, Sticky: false}

	view := runtimeRenderer{}.Render(state, nil)
	if !strings.Contains(view, "header_bar: ws=main | tab=shell | pane=pane-1 | slot=waiting | overlay=none | focus=tiled | mode=global") {
		t.Fatalf("expected header bar to include active mode, got:\n%s", view)
	}
}

func TestRuntimeRendererTruncatesNoticeSectionToLatestEntries(t *testing.T) {
	view := runtimeRenderer{}.Render(connectedRunAppState(), []btui.Notice{
		{Level: btui.NoticeLevelError, Text: "n1"},
		{Level: btui.NoticeLevelError, Text: "n2"},
		{Level: btui.NoticeLevelError, Text: "n3"},
		{Level: btui.NoticeLevelError, Text: "n4"},
		{Level: btui.NoticeLevelError, Text: "n5"},
	})

	if !strings.Contains(view, "notices_rendered: 4") || !strings.Contains(view, "notices_truncated: true") {
		t.Fatalf("expected truncated notice metadata, got:\n%s", view)
	}
	if strings.Contains(view, "[error] n1") {
		t.Fatalf("expected oldest notice to be truncated, got:\n%s", view)
	}
	if !strings.Contains(view, "[error] n5") {
		t.Fatalf("expected latest notice to remain visible, got:\n%s", view)
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
	if !strings.Contains(view, "section_overlay:") {
		t.Fatalf("expected overlay section wrapper in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "workspace_picker_query: ops") {
		t.Fatalf("expected picker query in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "workspace_picker_selected: ws-2") {
		t.Fatalf("expected picker selected node key in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "workspace_picker_selected_kind: workspace") {
		t.Fatalf("expected picker selected node kind in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "workspace_picker_selected_label: ops") {
		t.Fatalf("expected picker selected node label in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "workspace_picker_selected_expanded: true") {
		t.Fatalf("expected picker selected node expanded flag in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "workspace_picker_selected_match: true") {
		t.Fatalf("expected picker selected node match flag in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "workspace_picker_selected_depth: 0") {
		t.Fatalf("expected picker selected node depth in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "workspace_picker_row_count: 6") {
		t.Fatalf("expected picker row count in rendered view, got:\n%s", view)
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

func TestRuntimeRendererTruncatesWorkspacePickerRowsAroundSelection(t *testing.T) {
	state := buildSinglePaneAppState("main", "shell", types.PaneSlotEmpty)
	for i := 2; i <= 12; i++ {
		workspaceID := types.WorkspaceID("ws-" + twoDigitLabel(i))
		tabID := types.TabID("tab-1")
		paneID := types.PaneID("pane-1")
		state.Domain.WorkspaceOrder = append(state.Domain.WorkspaceOrder, workspaceID)
		state.Domain.Workspaces[workspaceID] = types.WorkspaceState{
			ID:          workspaceID,
			Name:        "ws-" + twoDigitLabel(i),
			ActiveTabID: tabID,
			TabOrder:    []types.TabID{tabID},
			Tabs: map[types.TabID]types.TabState{
				tabID: {
					ID:           tabID,
					Name:         "tab-1",
					ActivePaneID: paneID,
					ActiveLayer:  types.FocusLayerTiled,
					Panes: map[types.PaneID]types.PaneState{
						paneID: {ID: paneID, Kind: types.PaneKindTiled, SlotState: types.PaneSlotEmpty},
					},
				},
			},
		}
	}
	picker := workspacedomain.NewPickerState(state.Domain)
	picker.MoveSelection(100)
	state.UI.Overlay = types.OverlayState{Kind: types.OverlayWorkspacePicker, Data: picker}
	state.UI.Focus.Layer = types.FocusLayerOverlay

	view := runtimeRenderer{}.Render(state, nil)
	if !strings.Contains(view, "workspace_picker_row_count: 15") {
		t.Fatalf("expected full workspace picker row count, got:\n%s", view)
	}
	if !strings.Contains(view, "workspace_picker_rows_rendered: 8") || !strings.Contains(view, "workspace_picker_rows_truncated: true") {
		t.Fatalf("expected truncated workspace picker metadata, got:\n%s", view)
	}
	if !strings.Contains(view, "> [workspace] ws-12") {
		t.Fatalf("expected bottom selection to stay visible in preview, got:\n%s", view)
	}
	if strings.Contains(view, "[workspace] ws-01") || strings.Contains(view, "[workspace] ws-02") {
		t.Fatalf("expected leading workspace rows to be truncated, got:\n%s", view)
	}
}

func TestRuntimeRendererRendersTerminalManagerOverlay(t *testing.T) {
	state := runtimeStateWithTerminalManagerTargets()
	manager := terminalmanagerdomain.NewState(state.Domain, state.UI.Focus)
	manager.MoveSelection(1)
	state.UI.Overlay = types.OverlayState{
		Kind: types.OverlayTerminalManager,
		Data: manager,
	}
	state.UI.Focus.Layer = types.FocusLayerOverlay

	view := runtimeRenderer{}.Render(state, nil)
	if !strings.Contains(view, "terminal_manager_query: ") {
		t.Fatalf("expected manager query in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "terminal_manager_selected: term-2") {
		t.Fatalf("expected manager selected terminal id in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "terminal_manager_selected_label: build-log") {
		t.Fatalf("expected manager selected terminal label in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "terminal_manager_selected_kind: terminal") {
		t.Fatalf("expected manager selected row kind in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "terminal_manager_selected_section: PARKED") {
		t.Fatalf("expected manager selected terminal section in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "terminal_manager_selected_state: running") {
		t.Fatalf("expected manager selected terminal state in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "terminal_manager_selected_visible: false") {
		t.Fatalf("expected manager selected terminal visible flag in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "terminal_manager_selected_visibility: hidden") {
		t.Fatalf("expected manager selected terminal visibility label in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "terminal_manager_selected_connected_panes: 0") {
		t.Fatalf("expected manager selected terminal connection count in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "terminal_manager_selected_location_count: 0") {
		t.Fatalf("expected manager selected terminal location count in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "terminal_manager_selected_command: tail -f build.log") {
		t.Fatalf("expected manager selected terminal command in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "terminal_manager_selected_owner: ") {
		t.Fatalf("expected manager selected terminal owner field in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "terminal_manager_selected_tags: group=build") {
		t.Fatalf("expected manager selected terminal tags in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "terminal_manager_row_count: 7") {
		t.Fatalf("expected manager row count in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "terminal_manager_rows:") {
		t.Fatalf("expected manager rows section in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "> [terminal] build-log") {
		t.Fatalf("expected selected manager row in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "terminal_manager_detail: build-log") {
		t.Fatalf("expected manager detail header in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "detail_terminal: term-2") {
		t.Fatalf("expected manager detail terminal id in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "detail_state: running") {
		t.Fatalf("expected manager detail state in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "detail_visible: false") {
		t.Fatalf("expected manager detail visible flag in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "detail_visibility: hidden") {
		t.Fatalf("expected manager detail visibility label in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "detail_command: tail -f build.log") {
		t.Fatalf("expected manager detail command in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "detail_connected_panes: 0") {
		t.Fatalf("expected manager detail connection count in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "detail_location_count: 0") {
		t.Fatalf("expected manager detail location count in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "detail_tags: group=build") {
		t.Fatalf("expected manager detail tags in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "detail_owner: ") {
		t.Fatalf("expected manager detail owner in rendered view, got:\n%s", view)
	}
	if lines := strings.Count(view, "\n") + 1; lines > 34 {
		t.Fatalf("expected overlay view to remain within compact budget, got %d lines:\n%s", lines, view)
	}
}

func TestRuntimeRendererCompressesBodyWhenOverlayIsActive(t *testing.T) {
	state := runtimeStateWithTerminalManagerTargets()
	manager := terminalmanagerdomain.NewState(state.Domain, state.UI.Focus)
	state.UI.Overlay = types.OverlayState{
		Kind: types.OverlayTerminalManager,
		Data: manager,
	}
	state.UI.Focus.Layer = types.FocusLayerOverlay

	renderer := runtimeRenderer{
		Screens: NewRuntimeTerminalStore(RuntimeSessions{
			Terminals: map[types.TerminalID]TerminalRuntimeSession{
				types.TerminalID("term-1"): {
					TerminalID: types.TerminalID("term-1"),
					Snapshot: &protocol.Snapshot{
						TerminalID: "term-1",
						Screen: protocol.ScreenData{
							Cells: [][]protocol.Cell{
								{{Content: "$"}, {Content: " "}, {Content: "p"}, {Content: "w"}, {Content: "d"}},
								{{Content: "/"}, {Content: "t"}, {Content: "m"}, {Content: "p"}},
							},
						},
					},
				},
			},
		}),
	}

	view := renderer.Render(state, nil)
	if !strings.Contains(view, "body_bar: terminal=term-1:running | screen=suppressed | overlay=terminal_manager") {
		t.Fatalf("expected overlay body bar to reflect compressed mode, got:\n%s", view)
	}
	if !strings.Contains(view, "terminal_bar: id=term-1 | title=api-dev | state=running | role=owner") {
		t.Fatalf("expected terminal bar to remain visible during overlay compression, got:\n%s", view)
	}
	if !strings.Contains(view, "screen_bar: state=suppressed | rows=2/2") {
		t.Fatalf("expected suppressed screen bar during overlay compression, got:\n%s", view)
	}
	if !strings.Contains(view, "overlay_bar: kind=terminal_manager | focus=overlay") {
		t.Fatalf("expected overlay bar to expose active overlay kind, got:\n%s", view)
	}
	if !strings.Contains(view, "screen: <suppressed by overlay>") {
		t.Fatalf("expected screen preview to yield to overlay, got:\n%s", view)
	}
	if strings.Contains(view, "$ pwd") || strings.Contains(view, "/tmp") {
		t.Fatalf("expected screen rows to be suppressed while overlay is active, got:\n%s", view)
	}
	if strings.Contains(view, "terminal_tags:") {
		t.Fatalf("expected noncritical terminal detail to be suppressed while overlay is active, got:\n%s", view)
	}
	if lines := strings.Count(view, "\n") + 1; lines > 31 {
		t.Fatalf("expected overlay-active body to stay tightly compressed, got %d lines:\n%s", lines, view)
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
	if !strings.Contains(view, "section_overlay:") {
		t.Fatalf("expected prompt overlay section wrapper in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "prompt_title: edit terminal metadata") {
		t.Fatalf("expected prompt title in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "prompt_terminal: term-2") {
		t.Fatalf("expected prompt terminal id in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "prompt_active_field: tags") {
		t.Fatalf("expected prompt active field in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "prompt_active_label: Tags") {
		t.Fatalf("expected prompt active label in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "prompt_active_value: group=build") {
		t.Fatalf("expected prompt active value in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "prompt_active_index: 1") {
		t.Fatalf("expected prompt active index in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "prompt_field_count: 2") {
		t.Fatalf("expected prompt field count in rendered view, got:\n%s", view)
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

func TestRuntimeRendererTruncatesPromptFieldsAroundActiveField(t *testing.T) {
	state := runtimeStateWithTerminalManagerTargets()
	state.UI.Overlay = types.OverlayState{
		Kind: types.OverlayPrompt,
		Data: &promptdomain.State{
			Kind:       promptdomain.KindEditTerminalMetadata,
			Title:      "edit terminal metadata",
			TerminalID: types.TerminalID("term-2"),
			Fields: []promptdomain.Field{
				{Key: "f1", Label: "F1", Value: "v1"},
				{Key: "f2", Label: "F2", Value: "v2"},
				{Key: "f3", Label: "F3", Value: "v3"},
				{Key: "f4", Label: "F4", Value: "v4"},
				{Key: "f5", Label: "F5", Value: "v5"},
				{Key: "f6", Label: "F6", Value: "v6"},
			},
			Active: 5,
		},
	}
	state.UI.Focus.Layer = types.FocusLayerPrompt

	view := runtimeRenderer{}.Render(state, nil)
	if !strings.Contains(view, "prompt_field_count: 6") || !strings.Contains(view, "prompt_fields_rendered: 4") || !strings.Contains(view, "prompt_fields_truncated: true") {
		t.Fatalf("expected truncated prompt metadata, got:\n%s", view)
	}
	if !strings.Contains(view, "> [f6] F6: v6") {
		t.Fatalf("expected active field to stay visible, got:\n%s", view)
	}
	if strings.Contains(view, "[f1] F1: v1") || strings.Contains(view, "[f2] F2: v2") {
		t.Fatalf("expected leading prompt fields to be truncated, got:\n%s", view)
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
	if !strings.Contains(view, "terminal_picker_selected: term-3") {
		t.Fatalf("expected picker selected terminal id in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "terminal_picker_selected_label: ops-watch") {
		t.Fatalf("expected picker selected terminal label in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "terminal_picker_selected_kind: terminal") {
		t.Fatalf("expected picker selected row kind in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "terminal_picker_selected_state: running") {
		t.Fatalf("expected picker selected terminal state in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "terminal_picker_selected_command: journalctl -f") {
		t.Fatalf("expected picker selected terminal command in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "terminal_picker_selected_visible: false") {
		t.Fatalf("expected picker selected terminal visible flag in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "terminal_picker_selected_tags: team=ops") {
		t.Fatalf("expected picker selected terminal tags in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "terminal_picker_selected_connected_panes: 0") {
		t.Fatalf("expected picker selected terminal connection count in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "terminal_picker_row_count: 2") {
		t.Fatalf("expected picker row count in rendered view, got:\n%s", view)
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
	if !strings.Contains(view, "layout_resolve_pane: pane-1") {
		t.Fatalf("expected resolve pane id in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "layout_resolve_selected: create_new") {
		t.Fatalf("expected resolve selected action in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "layout_resolve_selected_label: create new") {
		t.Fatalf("expected resolve selected label in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "layout_resolve_row_count: 3") {
		t.Fatalf("expected resolve row count in rendered view, got:\n%s", view)
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

func TestRuntimeRendererRendersFloatingPaneKind(t *testing.T) {
	view := runtimeRenderer{}.Render(runtimeStateWithFloatingActivePane(), nil)
	if !strings.Contains(view, "tab_layer: floating") {
		t.Fatalf("expected floating tab layer in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "pane_kind: floating") {
		t.Fatalf("expected floating pane kind in rendered view, got:\n%s", view)
	}
}

func twoDigitLabel(v int) string {
	return string(rune('0'+v/10)) + string(rune('0'+v%10))
}
