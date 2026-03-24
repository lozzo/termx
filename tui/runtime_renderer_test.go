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
						Size:       protocol.Size{Cols: 120, Rows: 40},
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
	if !strings.Contains(view, "VIEWPORT[96x40]") {
		t.Fatalf("expected wireframe viewport to adapt to runtime size, got:\n%s", view)
	}
	if !strings.Contains(view, "screen_shell:") || !strings.Contains(view, "SHELL[96x40 overlay=none]") || !strings.Contains(view, "HEADER[main] [shell] pane:pane-1 term:term-1 float:0") {
		t.Fatalf("expected renderer to expose visible shell frame header, got:\n%s", view)
	}
	if !strings.Contains(view, "STATE[tiled focus=tiled mode=none overlay=none]") || !strings.Contains(view, "BODY[tiled t=1 f=0]") || !strings.Contains(view, "TARGET[main/shell/pane-1] TERM[term-1] FLOAT[0]") {
		t.Fatalf("expected renderer to expose shell frame state lines, got:\n%s", view)
	}
	if !strings.Contains(view, "+ api-dev [owner] [tiled]") || !strings.Contains(view, "FT[api-dev tiled none]") || !strings.Contains(view, "<p> PANE <t> TAB <w> WS <o> FLOAT <f> PICK <g> GLOBAL") {
		t.Fatalf("expected renderer to expose visible shell frame body/footer, got:\n%s", view)
	}
	if !strings.Contains(view, "$ pwd") || !strings.Contains(view, "/tmp") || !strings.Contains(view, "term-1 running owner") {
		t.Fatalf("expected screen shell to render pane canvas preview, got:\n%s", view)
	}
	if !strings.Contains(view, "termx") || !strings.Contains(view, "header_bar: ws=main | tab=shell | pane=pane-1 | slot=connected | overlay=none | focus=tiled") {
		t.Fatalf("expected renderer header bar, got:\n%s", view)
	}
	if !strings.Contains(view, "workspace_bar: [main]") {
		t.Fatalf("expected workspace bar in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "workspace_summary: tabs=1 | panes=1 | terminals=1 | floating=0") {
		t.Fatalf("expected workspace summary in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "tab_strip: [shell]") {
		t.Fatalf("expected tab strip in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "tab_path_bar: path=main/shell/tiled:pane-1 | target=api-dev") {
		t.Fatalf("expected tab path bar in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "tab_layer_bar: tiled_root=pane-1 | floating_top=<none> | floating_total=0") {
		t.Fatalf("expected tab layer bar in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "chrome_header:") || !strings.Contains(view, "chrome_body:") || !strings.Contains(view, "chrome_footer:") {
		t.Fatalf("expected chrome wrappers in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "body_bar: terminal=term-1:running | screen=preview:2/2 | overlay=none") {
		t.Fatalf("expected body bar in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "pane_bar: title=api-dev | role=owner | kind=tiled") {
		t.Fatalf("expected pane bar in rendered view, got:\n%s", view)
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
	if !strings.Contains(view, "focus_bar: target=api-dev | layer=tiled | role=owner") {
		t.Fatalf("expected focus bar in rendered view, got:\n%s", view)
	}
	if lines := strings.Count(view, "\n") + 1; lines > 64 {
		t.Fatalf("expected compact active pane view, got %d lines:\n%s", lines, view)
	}
}

func TestRuntimeRendererRendersTiledOutlineForSplitTab(t *testing.T) {
	state := runtimeStateWithSplitPaneTargets()
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
									{Content: "n"},
									{Content: "p"},
									{Content: "m"},
									{Content: " "},
									{Content: "r"},
									{Content: "u"},
									{Content: "n"},
									{Content: " "},
									{Content: "d"},
									{Content: "e"},
									{Content: "v"},
								},
								{
									{Content: "r"},
									{Content: "e"},
									{Content: "a"},
									{Content: "d"},
									{Content: "y"},
									{Content: " "},
									{Content: "o"},
									{Content: "n"},
									{Content: " "},
									{Content: ":"},
									{Content: "3"},
									{Content: "0"},
									{Content: "0"},
									{Content: "0"},
								},
							},
						},
					},
				},
				types.TerminalID("term-2"): {
					TerminalID: types.TerminalID("term-2"),
					Snapshot: &protocol.Snapshot{
						TerminalID: "term-2",
						Screen: protocol.ScreenData{
							Cells: [][]protocol.Cell{
								{
									{Content: ">"},
									{Content: " "},
									{Content: "t"},
									{Content: "s"},
									{Content: "c"},
									{Content: " "},
									{Content: "-"},
									{Content: "w"},
								},
								{
									{Content: "F"},
									{Content: "o"},
									{Content: "u"},
									{Content: "n"},
									{Content: "d"},
									{Content: " "},
									{Content: "0"},
									{Content: " "},
									{Content: "e"},
									{Content: "r"},
									{Content: "r"},
									{Content: "o"},
									{Content: "r"},
									{Content: "s"},
								},
							},
						},
					},
				},
			},
		}),
	}

	view := renderer.Render(state, nil)
	if !strings.Contains(view, "tiled_outline_bar: active=pane-1 | total=2") {
		t.Fatalf("expected tiled outline bar in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "tiled_layout: root=vertical | depth=2 | leaves=2 | ratio=0.50") {
		t.Fatalf("expected tiled layout summary in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "tiled_outline:") {
		t.Fatalf("expected tiled outline section in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "> [tiled] api-dev | role=owner | state=running | preview=ready on :3000") {
		t.Fatalf("expected active tiled pane row in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "  [tiled] build-log | role=owner | state=running | preview=Found 0 errors") {
		t.Fatalf("expected sibling tiled pane row in rendered view, got:\n%s", view)
	}
	if !(strings.Index(view, "pane_bar:") < strings.Index(view, "tiled_outline_bar:") &&
		strings.Index(view, "tiled_layout:") < strings.Index(view, "tiled_outline:") &&
		strings.Index(view, "tiled_outline:") < strings.Index(view, "section_terminal:")) {
		t.Fatalf("expected tiled outline to stay between pane bar and terminal section, got:\n%s", view)
	}
}

func TestRuntimeRendererRendersFollowerRoleInsideTiledOutline(t *testing.T) {
	state := runtimeStateWithFollowerActivePane()
	terminal := state.Domain.Terminals[types.TerminalID("term-1")]
	terminal.Name = "api-dev"
	terminal.Visible = true
	state.Domain.Terminals[types.TerminalID("term-1")] = terminal

	view := runtimeRenderer{}.Render(state, nil)
	if !strings.Contains(view, "tiled_outline_bar: active=pane-2 | total=2") {
		t.Fatalf("expected follower tiled outline bar in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "tiled_layout: root=<implicit> | depth=1 | leaves=2") {
		t.Fatalf("expected fallback tiled layout summary in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "  [tiled] api-dev | role=owner") {
		t.Fatalf("expected owner row to remain visible in tiled outline, got:\n%s", view)
	}
	if !strings.Contains(view, "> [tiled] api-dev | role=follower") {
		t.Fatalf("expected follower row to be marked in tiled outline, got:\n%s", view)
	}
}

func TestRuntimeRendererRendersNestedTiledTree(t *testing.T) {
	state := runtimeStateWithNestedSplitPaneTargets()
	renderer := runtimeRenderer{
		Screens: NewRuntimeTerminalStore(RuntimeSessions{
			Terminals: map[types.TerminalID]TerminalRuntimeSession{
				types.TerminalID("term-1"): {
					TerminalID: types.TerminalID("term-1"),
					Snapshot: &protocol.Snapshot{
						TerminalID: "term-1",
						Screen: protocol.ScreenData{
							Cells: [][]protocol.Cell{
								{{Content: "r"}, {Content: "e"}, {Content: "a"}, {Content: "d"}, {Content: "y"}},
							},
						},
					},
				},
				types.TerminalID("term-2"): {
					TerminalID: types.TerminalID("term-2"),
					Snapshot: &protocol.Snapshot{
						TerminalID: "term-2",
						Screen: protocol.ScreenData{
							Cells: [][]protocol.Cell{
								{{Content: "b"}, {Content: "u"}, {Content: "i"}, {Content: "l"}, {Content: "d"}},
							},
						},
					},
				},
				types.TerminalID("term-3"): {
					TerminalID: types.TerminalID("term-3"),
					Snapshot: &protocol.Snapshot{
						TerminalID: "term-3",
						Screen: protocol.ScreenData{
							Cells: [][]protocol.Cell{
								{{Content: "w"}, {Content: "a"}, {Content: "t"}, {Content: "c"}, {Content: "h"}},
							},
						},
					},
				},
			},
		}),
	}

	view := renderer.Render(state, nil)
	if !strings.Contains(view, "tiled_tree:") {
		t.Fatalf("expected tiled tree section in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "split horizontal ratio=0.60") {
		t.Fatalf("expected root split row in tiled tree, got:\n%s", view)
	}
	if !strings.Contains(view, "|- > [tiled] api-dev | role=owner | state=running | preview=ready") {
		t.Fatalf("expected active pane row in tiled tree, got:\n%s", view)
	}
	if !strings.Contains(view, "\\- split vertical ratio=0.50") {
		t.Fatalf("expected nested split row in tiled tree, got:\n%s", view)
	}
	if !strings.Contains(view, "   |- [tiled] watcher | role=owner | state=running | preview=watch") {
		t.Fatalf("expected nested first child row in tiled tree, got:\n%s", view)
	}
	if !strings.Contains(view, "   \\- [tiled] build-log | role=owner | state=running | preview=build") {
		t.Fatalf("expected nested second child row in tiled tree, got:\n%s", view)
	}
}

func TestRuntimeRendererTruncatesLongBarLines(t *testing.T) {
	state := buildSinglePaneAppState(
		"workspace-with-a-very-long-name-that-should-be-truncated-in-the-header-bar-output",
		"tab-with-a-very-long-name-that-should-be-truncated-too",
		types.PaneSlotEmpty,
	)
	state.UI.Mode = types.ModeState{Active: types.ModeGlobal, Sticky: false}
	view := runtimeRenderer{}.Render(state, []btui.Notice{{
		Level: btui.NoticeLevelError,
		Text:  "notice-with-a-very-long-text-that-should-not-break-the-footer-bar-layout-when-summary-is-rendered",
	}})

	headerBar := findLineWithPrefix(view, "header_bar:")
	if headerBar == "" || len(headerBar) > runtimeBarMaxWidth || !strings.Contains(headerBar, "...") {
		t.Fatalf("expected truncated header bar within max width, got:\n%s", view)
	}

	footerBar := findLineWithPrefix(view, "footer_bar:")
	if footerBar == "" || len(footerBar) > runtimeBarMaxWidth {
		t.Fatalf("expected footer bar within max width, got:\n%s", view)
	}

	noticeBar := findLineWithPrefix(view, "notice_bar:")
	if noticeBar == "" || len(noticeBar) > runtimeBarMaxWidth {
		t.Fatalf("expected notice bar within max width, got:\n%s", view)
	}
}

func TestRuntimeRendererTruncatesLongSummaryLines(t *testing.T) {
	state := connectedRunAppState()
	workspace := state.Domain.Workspaces[state.Domain.ActiveWorkspaceID]
	tab := workspace.Tabs[workspace.ActiveTabID]
	pane := tab.Panes[tab.ActivePaneID]
	workspace.Name = "workspace-with-an-extremely-long-name-that-should-not-let-the-status-summary-line-grow-without-bound"
	tab.Name = "tab-with-an-extremely-long-name-that-should-also-be-compacted-inside-the-status-summary-line"
	tab.Panes[pane.ID] = pane
	workspace.Tabs[tab.ID] = tab
	state.Domain.Workspaces[workspace.ID] = workspace
	state.Domain.Terminals[pane.TerminalID] = types.TerminalRef{
		ID:      pane.TerminalID,
		Name:    "terminal-with-a-very-long-title-that-should-not-let-terminal-summary-lines-grow-without-bound",
		State:   types.TerminalRunStateRunning,
		Visible: true,
	}
	picker := terminalpickerdomain.NewState(state.Domain, state.UI.Focus)
	picker.AppendQuery("query-with-a-very-long-value-that-should-not-let-overlay-summary-lines-grow-without-bound")
	state.UI.Overlay = types.OverlayState{
		Kind: types.OverlayTerminalPicker,
		Data: picker,
	}
	state.UI.Focus.Layer = types.FocusLayerOverlay

	view := runtimeRenderer{}.Render(state, nil)

	statusSummary := findLineWithPrefix(view, "workspace:")
	if statusSummary == "" || len(statusSummary) > runtimeSummaryMaxWidth || !strings.Contains(statusSummary, "...") {
		t.Fatalf("expected truncated status summary within max width, got:\n%s", view)
	}
	if !strings.Contains(statusSummary, "slot: connected") {
		t.Fatalf("expected status summary to preserve trailing state fields, got:\n%s", view)
	}

	terminalSummary := findLineWithPrefix(view, "terminal_bar:")
	if terminalSummary == "" || len(terminalSummary) > runtimeSummaryMaxWidth || !strings.Contains(terminalSummary, "...") {
		t.Fatalf("expected truncated terminal summary within max width, got:\n%s", view)
	}
	if !strings.Contains(terminalSummary, "terminal_bar: id=term-1") || !strings.Contains(terminalSummary, "grow-without-bound") {
		t.Fatalf("expected terminal summary to preserve head/tail terminal semantics, got:\n%s", view)
	}

	overlaySummary := findLineWithPrefix(view, "overlay_bar:")
	if overlaySummary == "" || len(overlaySummary) > runtimeSummaryMaxWidth || !strings.Contains(overlaySummary, "...") {
		t.Fatalf("expected truncated overlay summary within max width, got:\n%s", view)
	}
	if !strings.Contains(overlaySummary, "terminal_picker_row_count: 1") {
		t.Fatalf("expected overlay summary to preserve trailing row metadata, got:\n%s", view)
	}
}

func TestRuntimeRendererTruncatesLongDetailLines(t *testing.T) {
	state := runtimeStateWithTerminalManagerTargets()
	terminal := state.Domain.Terminals[types.TerminalID("term-2")]
	terminal.Command = []string{
		"tail",
		"-f",
		"build-log-with-a-very-long-name-that-should-not-let-terminal-manager-detail-lines-grow-without-bound",
		"--profile=build-pipeline-with-a-very-long-profile-name-that-keeps-the-detail-line-growing",
		"--region=us-east-1-development-cluster",
	}
	state.Domain.Terminals[types.TerminalID("term-2")] = terminal
	manager := terminalmanagerdomain.NewState(state.Domain, state.UI.Focus)
	manager.MoveSelection(1)
	state.UI.Overlay = types.OverlayState{
		Kind: types.OverlayTerminalManager,
		Data: manager,
	}
	state.UI.Focus.Layer = types.FocusLayerOverlay

	view := runtimeRenderer{}.Render(state, nil)

	selectedCommand := findLineWithPrefix(view, "terminal_manager_selected_command:")
	if selectedCommand == "" || len(selectedCommand) > runtimeDetailMaxWidth || !strings.Contains(selectedCommand, "...") {
		t.Fatalf("expected truncated terminal manager selected command line, got:\n%s", view)
	}
	if !strings.Contains(selectedCommand, "terminal_manager_selected_owner:") {
		t.Fatalf("expected selected command line to preserve trailing owner metadata, got:\n%s", view)
	}

	detailCommand := findLineWithPrefix(view, "detail_connected_panes:")
	if detailCommand == "" || len(detailCommand) > runtimeDetailMaxWidth || !strings.Contains(detailCommand, "...") {
		t.Fatalf("expected truncated terminal manager detail command line, got:\n%s", view)
	}
	if !strings.Contains(detailCommand, "detail_command: tail -f") {
		t.Fatalf("expected detail command line to preserve command head, got:\n%s", view)
	}
}

func TestRuntimeRendererTruncatesLongPromptAndResolveDetailLines(t *testing.T) {
	state := runtimeStateWithTerminalManagerTargets()
	state.UI.Overlay = types.OverlayState{
		Kind: types.OverlayPrompt,
		Data: &promptdomain.State{
			Kind:       promptdomain.KindEditTerminalMetadata,
			Title:      "edit terminal metadata",
			TerminalID: types.TerminalID("term-2"),
			Fields: []promptdomain.Field{
				{
					Key:   "tags",
					Label: "Tags",
					Value: "group=build,service=very-long-service-name-that-should-not-let-prompt-detail-lines-grow-without-bound,owner=platform-team,region=us-east-1,environment=development-cluster",
				},
			},
			Active: 0,
		},
	}
	state.UI.Focus.Layer = types.FocusLayerPrompt

	promptView := runtimeRenderer{}.Render(state, nil)
	promptActive := findLineWithPrefix(promptView, "prompt_active_field:")
	if promptActive == "" || len(promptActive) > runtimeDetailMaxWidth || !strings.Contains(promptActive, "...") {
		t.Fatalf("expected truncated prompt active line, got:\n%s", promptView)
	}
	if !strings.Contains(promptActive, "prompt_active_label: Tags") {
		t.Fatalf("expected prompt active line to preserve trailing label metadata, got:\n%s", promptView)
	}

	resolveState := buildSinglePaneAppState("main", "shell", types.PaneSlotWaiting)
	resolve := layoutresolvedomain.NewState(
		types.PaneID("pane-1"),
		"backend-dev",
		"env=dev service=api hint-with-a-very-long-description-that-should-not-let-layout-resolve-detail-lines-grow-without-bound owner=platform-team region=us-east-1-development-cluster branch=feature/super-long-layout-resolve-context",
	)
	resolveState.UI.Overlay = types.OverlayState{
		Kind: types.OverlayLayoutResolve,
		Data: resolve,
	}
	resolveState.UI.Focus.Layer = types.FocusLayerOverlay

	resolveView := runtimeRenderer{}.Render(resolveState, nil)
	resolveHint := findLineWithPrefix(resolveView, "layout_resolve_hint:")
	if resolveHint == "" || len(resolveHint) > runtimeDetailMaxWidth || !strings.Contains(resolveHint, "...") {
		t.Fatalf("expected truncated layout resolve hint line, got:\n%s", resolveView)
	}
	if !strings.Contains(resolveHint, "layout_resolve_row_count: 3") {
		t.Fatalf("expected resolve hint line to preserve trailing row count, got:\n%s", resolveView)
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
	if !strings.Contains(view, "no terminal connected") || !strings.Contains(view, "n new | a connect | m manager") {
		t.Fatalf("expected screen shell empty pane sections, got:\n%s", view)
	}
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
	if !strings.Contains(view, "workspace_summary: tabs=1 | panes=1 | terminals=0 | floating=0") {
		t.Fatalf("expected empty-pane workspace summary in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "pane_bar: title=unconnected pane | slot=empty | kind=tiled") {
		t.Fatalf("expected empty-pane pane bar in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "section_terminal:") || !strings.Contains(view, "terminal: <disconnected>") {
		t.Fatalf("expected terminal placeholder section in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "pane_slot_detail: terminal removed or not connected") || !strings.Contains(view, "pane_actions:") || !strings.Contains(view, "[n] start new terminal") || !strings.Contains(view, "[a] connect existing terminal") || !strings.Contains(view, "[m] open terminal manager") || !strings.Contains(view, "[x] close pane") {
		t.Fatalf("expected empty pane actions in rendered view, got:\n%s", view)
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
	if !strings.Contains(view, "shortcut_bar: n new | a connect | m manager | x close | ? help") {
		t.Fatalf("expected empty pane shortcut bar in rendered view, got:\n%s", view)
	}
}

func TestRuntimeRendererRendersExitedPaneActions(t *testing.T) {
	state := connectedRunAppState()
	ws := state.Domain.Workspaces[types.WorkspaceID("ws-1")]
	tab := ws.Tabs[types.TabID("tab-1")]
	pane := tab.Panes[types.PaneID("pane-1")]
	exitCode := 7
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
	if !strings.Contains(view, "process exited") || !strings.Contains(view, "history retained") || !strings.Contains(view, "r restart | a connect") {
		t.Fatalf("expected screen shell exited pane sections, got:\n%s", view)
	}
	if !strings.Contains(view, "pane_slot_detail: terminal program exited") || !strings.Contains(view, "pane_history: retained") || !strings.Contains(view, "[r] restart terminal") || !strings.Contains(view, "[a] connect another terminal") || !strings.Contains(view, "[x] close pane") {
		t.Fatalf("expected exited pane actions in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "shortcut_bar: r restart | a connect | x close | ? help") {
		t.Fatalf("expected exited pane shortcut bar in rendered view, got:\n%s", view)
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
	if !strings.Contains(view, "notice_bar: total=1 | showing=1 | last=error | notices:") {
		t.Fatalf("expected notice bar in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "notice_group_bar: error=1") {
		t.Fatalf("expected notice group bar in rendered view, got:\n%s", view)
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
	headerBar := findLineWithPrefix(view, "header_bar:")
	if headerBar == "" {
		t.Fatalf("expected header bar in rendered view, got:\n%s", view)
	}
	if len(headerBar) > runtimeBarMaxWidth {
		t.Fatalf("expected header bar within max width, got:\n%s", view)
	}
	if !strings.Contains(headerBar, "ws=main") || !strings.Contains(headerBar, "mode=global") {
		t.Fatalf("expected header bar to include active mode, got:\n%s", view)
	}
}

func TestRuntimeRendererTruncatesNoticeSectionToLatestEntries(t *testing.T) {
	view := runtimeRenderer{}.Render(connectedRunAppState(), []btui.Notice{
		{Level: btui.NoticeLevelError, Text: "n1"},
		{Level: btui.NoticeLevelInfo, Text: "i1"},
		{Level: btui.NoticeLevelError, Text: "n2"},
		{Level: btui.NoticeLevelError, Text: "n3"},
		{Level: btui.NoticeLevelError, Text: "n4"},
		{Level: btui.NoticeLevelError, Text: "n5"},
	})

	if !strings.Contains(view, "notice_bar: total=6 | showing=4 | last=error | notices:") {
		t.Fatalf("expected notice summary bar, got:\n%s", view)
	}
	if !strings.Contains(view, "notice_group_bar: error=5 | info=1") {
		t.Fatalf("expected grouped notice counts, got:\n%s", view)
	}
	if !strings.Contains(view, "notices_rendered: 4") || !strings.Contains(view, "notices_truncated: true") {
		t.Fatalf("expected truncated notice metadata, got:\n%s", view)
	}
	if strings.Contains(view, "[error] n1") || strings.Contains(view, "[info] i1") {
		t.Fatalf("expected oldest notice to be truncated, got:\n%s", view)
	}
	if !strings.Contains(view, "[error] n5") {
		t.Fatalf("expected latest notice to remain visible, got:\n%s", view)
	}
}

func TestRuntimeRendererRendersNoticeBarForEmptyNotices(t *testing.T) {
	view := runtimeRenderer{}.Render(connectedRunAppState(), nil)
	if !strings.Contains(view, "notice_bar: total=0 | showing=0 | notices: 0") {
		t.Fatalf("expected empty notice bar in rendered view, got:\n%s", view)
	}
	if strings.Contains(view, "notice_group_bar:") {
		t.Fatalf("expected no grouped notice bar when empty, got:\n%s", view)
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
	if !strings.Contains(view, "workspace_picker_bar: selected=ws-2 | kind=workspace | depth=0") {
		t.Fatalf("expected workspace picker bar in rendered view, got:\n%s", view)
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
	if !strings.Contains(view, "terminal_manager_bar: selected=term-2 | section=PARKED | kind=terminal") {
		t.Fatalf("expected terminal manager bar in rendered view, got:\n%s", view)
	}
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
	if !strings.Contains(view, "terminal_manager_actions:") || !strings.Contains(view, "[jump] jump to connected pane") || !strings.Contains(view, "[connect_here] connect here") || !strings.Contains(view, "[new_tab] open in new tab") || !strings.Contains(view, "[floating] open in floating pane") || !strings.Contains(view, "[edit] edit metadata") || !strings.Contains(view, "[acquire_owner] acquire owner") || !strings.Contains(view, "[stop] stop terminal") {
		t.Fatalf("expected manager actions in rendered view, got:\n%s", view)
	}
	if lines := strings.Count(view, "\n") + 1; lines > 96 {
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
	if !strings.Contains(view, "terminal_manager_bar: selected=term-1 | section=VISIBLE | kind=terminal") {
		t.Fatalf("expected terminal manager bar in compressed overlay view, got:\n%s", view)
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
	if lines := strings.Count(view, "\n") + 1; lines > 98 {
		t.Fatalf("expected overlay-active body to stay tightly compressed, got %d lines:\n%s", lines, view)
	}
}

func TestRuntimeRendererTruncatesLongSectionAndOverlayBars(t *testing.T) {
	state := connectedRunAppState()
	state.Domain.Terminals[types.TerminalID("term-1")] = types.TerminalRef{
		ID:      types.TerminalID("term-1"),
		Name:    "terminal-with-a-very-long-title-that-should-be-truncated-inside-terminal-bar",
		State:   types.TerminalRunStateRunning,
		Visible: true,
	}
	picker := terminalpickerdomain.NewState(state.Domain, state.UI.Focus)
	picker.AppendQuery("query-with-a-very-long-value-that-should-be-truncated-inside-picker-bar-output")
	state.UI.Overlay = types.OverlayState{
		Kind: types.OverlayTerminalPicker,
		Data: picker,
	}
	state.UI.Focus.Layer = types.FocusLayerOverlay

	view := runtimeRenderer{}.Render(state, nil)

	workspace := state.Domain.Workspaces[state.Domain.ActiveWorkspaceID]
	tab := workspace.Tabs[workspace.ActiveTabID]
	pane := tab.Panes[tab.ActivePaneID]

	terminalBar := renderTerminalBar(state, pane)
	if len(terminalBar) > runtimeBarMaxWidth || !strings.Contains(terminalBar, "...") {
		t.Fatalf("expected truncated terminal bar within max width, got: %q", terminalBar)
	}
	terminalSummary := findLineWithPrefix(view, "terminal_bar:")
	if terminalSummary == "" || len(terminalSummary) > runtimeSummaryMaxWidth || !strings.Contains(terminalSummary, "terminal_bar: id=term-1") || !strings.Contains(terminalSummary, "inside-terminal-bar") {
		t.Fatalf("expected rendered view to include compact terminal summary, got:\n%s", view)
	}

	overlayBar := renderTerminalPickerBar(picker)
	if len(overlayBar) > runtimeBarMaxWidth || !strings.Contains(overlayBar, "...") {
		t.Fatalf("expected truncated terminal picker bar within max width, got: %q", overlayBar)
	}
	overlaySummary := findLineWithPrefix(view, "overlay_bar:")
	if overlaySummary == "" || len(overlaySummary) > runtimeSummaryMaxWidth || !strings.Contains(overlaySummary, "overlay_bar: kind=terminal_picker") || !strings.Contains(overlaySummary, "terminal_picker_row_count: 1") {
		t.Fatalf("expected rendered view to include compact overlay summary, got:\n%s", view)
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
	if !strings.Contains(view, "prompt_bar: kind=edit_terminal_metadata | terminal=term-2 | active=tags") {
		t.Fatalf("expected prompt bar in rendered view, got:\n%s", view)
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
	if !strings.Contains(view, "prompt_actions:") || !strings.Contains(view, "prompt_actions_rendered: 2") {
		t.Fatalf("expected prompt actions metadata in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "  [name] Name: build-log") {
		t.Fatalf("expected name field in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "> [tags] Tags: group=build") {
		t.Fatalf("expected active tags field in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "  [submit] submit") || !strings.Contains(view, "  [cancel] cancel") {
		t.Fatalf("expected prompt action rows in rendered view, got:\n%s", view)
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

func TestRuntimeRendererRendersPromptActionsForDraftPrompt(t *testing.T) {
	state := runtimeStateWithWorkspacePickerTarget()
	state.UI.Overlay = types.OverlayState{
		Kind: types.OverlayPrompt,
		Data: &promptdomain.State{
			Kind:  promptdomain.KindCreateWorkspace,
			Title: "create workspace",
			Draft: "ops-center",
		},
	}
	state.UI.Focus.Layer = types.FocusLayerPrompt

	view := runtimeRenderer{}.Render(state, nil)
	if !strings.Contains(view, "prompt_actions:") || !strings.Contains(view, "prompt_actions_rendered: 2") {
		t.Fatalf("expected draft prompt action metadata in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "  [submit] submit") || !strings.Contains(view, "  [cancel] cancel") {
		t.Fatalf("expected draft prompt action rows in rendered view, got:\n%s", view)
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
	if !strings.Contains(view, "terminal_picker_bar: query=ops | selected=term-3 | kind=terminal") {
		t.Fatalf("expected terminal picker bar in rendered view, got:\n%s", view)
	}
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
	if !strings.Contains(view, "layout_resolve_bar: pane=pane-1 | role=backend-dev | selected=create_new") {
		t.Fatalf("expected layout resolve bar in rendered view, got:\n%s", view)
	}
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
	if !strings.Contains(view, "floating_stack: pane-float") {
		t.Fatalf("expected floating stack summary in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "pane_bar: title=float-dev | role=owner | kind=floating") {
		t.Fatalf("expected floating pane bar in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "shortcut_bar: Ctrl-p pane | Ctrl-t tab | Ctrl-w ws | Ctrl-o float | Ctrl-f pick | Ctrl-g global | ? help") {
		t.Fatalf("expected connected pane shortcut bar in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "focus_bar: target=float-dev | layer=floating | role=owner") {
		t.Fatalf("expected floating focus bar in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "tab_path_bar: path=main/shell/floating:pane-float | target=float-dev") {
		t.Fatalf("expected floating tab path bar in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "tab_layer_bar: tiled_root=<none> | floating_top=pane-float | floating_total=1") {
		t.Fatalf("expected floating layer bar in rendered view, got:\n%s", view)
	}
}

func TestRuntimeRendererRendersFloatingOutline(t *testing.T) {
	state := runtimeStateWithFloatingOverviewTargets()
	renderer := runtimeRenderer{
		Screens: NewRuntimeTerminalStore(RuntimeSessions{
			Terminals: map[types.TerminalID]TerminalRuntimeSession{
				types.TerminalID("term-1"): {
					TerminalID: types.TerminalID("term-1"),
					Snapshot: &protocol.Snapshot{
						TerminalID: "term-1",
						Screen: protocol.ScreenData{
							Cells: [][]protocol.Cell{
								{{Content: "a"}, {Content: "p"}, {Content: "i"}, {Content: " "}, {Content: "r"}, {Content: "e"}, {Content: "a"}, {Content: "d"}, {Content: "y"}},
							},
						},
					},
				},
				types.TerminalID("term-2"): {
					TerminalID: types.TerminalID("term-2"),
					Snapshot: &protocol.Snapshot{
						TerminalID: "term-2",
						Screen: protocol.ScreenData{
							Cells: [][]protocol.Cell{
								{{Content: "b"}, {Content: "u"}, {Content: "i"}, {Content: "l"}, {Content: "d"}, {Content: " "}, {Content: "o"}, {Content: "k"}},
							},
						},
					},
				},
			},
		}),
	}

	view := renderer.Render(state, nil)
	if !strings.Contains(view, "floating_outline_bar: active=float-1 | total=2 | top=float-2") {
		t.Fatalf("expected floating outline bar in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "floating_outline:") {
		t.Fatalf("expected floating outline section in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "> [floating] api-dev | role=owner | rect=10,8 30x12 | state=running | preview=api ready") {
		t.Fatalf("expected active floating row in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "  [floating] build-log | role=owner | rect=45,14 28x10 | state=running | preview=build ok") {
		t.Fatalf("expected top floating row in rendered view, got:\n%s", view)
	}
	if !(strings.Index(view, "tiled_outline:") < strings.Index(view, "floating_outline_bar:") &&
		strings.Index(view, "floating_outline:") < strings.Index(view, "section_terminal:")) {
		t.Fatalf("expected floating outline to stay in body before terminal section, got:\n%s", view)
	}
}

func TestRuntimeRendererRendersTabSummaryAndMixedPaneSlots(t *testing.T) {
	state := runtimeStateWithMixedPaneSlots()
	renderer := runtimeRenderer{
		Screens: NewRuntimeTerminalStore(RuntimeSessions{
			Terminals: map[types.TerminalID]TerminalRuntimeSession{
				types.TerminalID("term-1"): {
					TerminalID: types.TerminalID("term-1"),
					Snapshot: &protocol.Snapshot{
						TerminalID: "term-1",
						Screen: protocol.ScreenData{
							Cells: [][]protocol.Cell{
								{{Content: "a"}, {Content: "p"}, {Content: "i"}, {Content: " "}, {Content: "u"}, {Content: "p"}},
							},
						},
					},
				},
			},
		}),
	}

	view := renderer.Render(state, nil)
	if !strings.Contains(view, "tab_summary: tiled=3 | floating=1 | connected=1 | waiting=1 | exited=1 | empty=1 | active_layer=tiled") {
		t.Fatalf("expected tab summary in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "tiled_layout: root=horizontal | depth=3 | leaves=3 | ratio=0.50") {
		t.Fatalf("expected mixed layout summary in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "|- [tiled] waiting pane | slot=waiting | detail=layout pending") {
		t.Fatalf("expected waiting pane in tiled tree, got:\n%s", view)
	}
	if !strings.Contains(view, "\\- [tiled] deploy-log | slot=exited | exit=7 | detail=history retained | state=exited") {
		t.Fatalf("expected exited pane in tiled tree, got:\n%s", view)
	}
	if !strings.Contains(view, "floating_outline_bar: active=pane-1 | total=1 | top=float-empty") {
		t.Fatalf("expected floating bar for mixed slot state, got:\n%s", view)
	}
	if !strings.Contains(view, "  [floating] unconnected pane | slot=empty | detail=terminal missing | rect=60,2 20x8") {
		t.Fatalf("expected empty floating pane in outline, got:\n%s", view)
	}
}

func TestRuntimeRendererRendersTabStripForMultipleTabs(t *testing.T) {
	view := runtimeRenderer{}.Render(runtimeStateWithTwoTabTargets(), nil)
	if !strings.Contains(view, "workspace_bar: [main]") {
		t.Fatalf("expected workspace bar for multi-tab state, got:\n%s", view)
	}
	if !strings.Contains(view, "workspace_summary: tabs=2 | panes=2 | terminals=2 | floating=0") {
		t.Fatalf("expected workspace summary for multi-tab state, got:\n%s", view)
	}
	if !strings.Contains(view, "tab_strip: [shell] | logs") {
		t.Fatalf("expected tab strip to expose active and inactive tabs, got:\n%s", view)
	}
	if !strings.Contains(view, "focus_bar: target=api-dev | layer=tiled | role=owner") {
		t.Fatalf("expected focus bar for multi-tab state, got:\n%s", view)
	}
}

func TestRuntimeRendererRendersWireframeWorkbenchForActivePane(t *testing.T) {
	state := runtimeStateWithActiveTerminalMetadata()
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
	if !strings.Contains(view, "wireframe_view:") {
		t.Fatalf("expected wireframe section in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "WORKSPACE[main] TAB[shell] LAYER[tiled] FOCUS[tiled] OVERLAY[none]") {
		t.Fatalf("expected wireframe header summary in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "ACTIVE[api-dev] ROLE[owner] KIND[tiled] SLOT[connected]") {
		t.Fatalf("expected wireframe active pane summary in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "TERM[term-1] STATE[running]") {
		t.Fatalf("expected wireframe terminal summary in rendered view, got:\n%s", view)
	}
}

func TestRuntimeRendererRendersWireframeSplitWorkbench(t *testing.T) {
	state := runtimeStateWithSplitPaneTargets()
	renderer := runtimeRenderer{
		Screens: NewRuntimeTerminalStore(RuntimeSessions{
			Terminals: map[types.TerminalID]TerminalRuntimeSession{
				types.TerminalID("term-1"): {
					TerminalID: types.TerminalID("term-1"),
					Snapshot: &protocol.Snapshot{
						TerminalID: "term-1",
						Screen: protocol.ScreenData{
							Cells: [][]protocol.Cell{
								{{Content: "$"}, {Content: " "}, {Content: "n"}, {Content: "p"}, {Content: "m"}},
								{{Content: "r"}, {Content: "e"}, {Content: "a"}, {Content: "d"}, {Content: "y"}},
							},
						},
					},
				},
				types.TerminalID("term-2"): {
					TerminalID: types.TerminalID("term-2"),
					Snapshot: &protocol.Snapshot{
						TerminalID: "term-2",
						Screen: protocol.ScreenData{
							Cells: [][]protocol.Cell{
								{{Content: ">"}, {Content: " "}, {Content: "t"}, {Content: "s"}, {Content: "c"}},
								{{Content: "o"}, {Content: "k"}},
							},
						},
					},
				},
			},
		}),
	}

	view := renderer.Render(state, nil)
	if !strings.Contains(view, "SPLIT SHELL[vertical 50/50]") || !strings.Contains(view, "LAYOUT[split] root=vertical ratio=50/50 leaves=2") || !strings.Contains(view, "+ api-dev [owner]") || !strings.Contains(view, "+ build-log [owner]") {
		t.Fatalf("expected split shell frame in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "$ npm") || !strings.Contains(view, "ready") || !strings.Contains(view, "term-1 running owner") || !strings.Contains(view, "> tsc") || !strings.Contains(view, "ok") || !strings.Contains(view, "term-2 running owner") {
		t.Fatalf("expected split pane shell canvas rows in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "SPLIT[vertical] RATIO[0.50] LEAVES[2]") {
		t.Fatalf("expected wireframe split summary in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "BAR[===============|===============]") {
		t.Fatalf("expected split ratio bar in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "ACTIVE[api-dev] ROLE[owner] STATE[running]") {
		t.Fatalf("expected active split pane box in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "PANE[build-log] ROLE[owner] STATE[running]") {
		t.Fatalf("expected sibling split pane box in rendered view, got:\n%s", view)
	}
}

func TestRuntimeRendererRendersWireframeFloatingStack(t *testing.T) {
	state := runtimeStateWithFloatingOverviewTargets()
	renderer := runtimeRenderer{
		Screens: NewRuntimeTerminalStore(RuntimeSessions{
			Terminals: map[types.TerminalID]TerminalRuntimeSession{
				types.TerminalID("term-1"): {
					TerminalID: types.TerminalID("term-1"),
					Snapshot: &protocol.Snapshot{
						TerminalID: "term-1",
						Screen: protocol.ScreenData{
							Cells: [][]protocol.Cell{
								{{Content: "a"}, {Content: "p"}, {Content: "i"}, {Content: " "}, {Content: "r"}, {Content: "e"}, {Content: "a"}, {Content: "d"}, {Content: "y"}},
							},
						},
					},
				},
				types.TerminalID("term-2"): {
					TerminalID: types.TerminalID("term-2"),
					Snapshot: &protocol.Snapshot{
						TerminalID: "term-2",
						Screen: protocol.ScreenData{
							Cells: [][]protocol.Cell{
								{{Content: "b"}, {Content: "u"}, {Content: "i"}, {Content: "l"}, {Content: "d"}, {Content: " "}, {Content: "o"}, {Content: "k"}},
							},
						},
					},
				},
			},
		}),
	}

	view := renderer.Render(state, nil)
	if !strings.Contains(view, "FLOAT SHELL[2]") || !strings.Contains(view, "STACK[windows] total=2") || !strings.Contains(view, "FOCUS[float-1] api-dev") || !strings.Contains(view, "WINDOWS[2]") || !strings.Contains(view, "WINDOW CARD[float-1] api-dev") || !strings.Contains(view, "GEOMETRY[10,8 30x12]") || !strings.Contains(view, "WINDOW CARD[float-2] build-log") || !strings.Contains(view, "GEOMETRY[45,14 28x10]") {
		t.Fatalf("expected floating shell window summary in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "api ready") || !strings.Contains(view, "term-1 running owner") || !strings.Contains(view, "build ok") || !strings.Contains(view, "term-2 running owner") {
		t.Fatalf("expected floating shell canvas rows in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "FLOATING STACK") {
		t.Fatalf("expected wireframe floating stack heading in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "FLOATING MAP") || !strings.Contains(view, "MAP[y08]") || !strings.Contains(view, "MAP[y14]") {
		t.Fatalf("expected floating geometry map in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "FLOAT[float-1] api-dev owner 10,8 30x12") {
		t.Fatalf("expected first floating pane card in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "FLOAT[float-2] build-log owner 45,14 28x10") {
		t.Fatalf("expected second floating pane card in rendered view, got:\n%s", view)
	}
}

func TestRuntimeRendererRendersWireframeOverlayDialog(t *testing.T) {
	state := runtimeStateWithTerminalManagerTargets()
	manager := terminalmanagerdomain.NewState(state.Domain, state.UI.Focus)
	manager.MoveSelection(1)
	state.UI.Overlay = types.OverlayState{
		Kind: types.OverlayTerminalManager,
		Data: manager,
	}
	state.UI.Focus.Layer = types.FocusLayerOverlay
	state.UI.Focus.OverlayTarget = types.OverlayTerminalManager

	view := runtimeRenderer{}.Render(state, nil)
	if !strings.Contains(view, "DIALOG[terminal_manager]") || !strings.Contains(view, "TITLE[terminal_manager]") || !strings.Contains(view, "FOOTER[enter here esc close]") || !strings.Contains(view, "ACTIONS[enter here esc close]") {
		t.Fatalf("expected shell overlay dialog layering in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "LIST[terminals]") || !strings.Contains(view, "DETAIL[terminal]") || !strings.Contains(view, "BODY[list] rows=7") || !strings.Contains(view, "selected=term-2 query=") || !strings.Contains(view, "DETAIL[build-log]") || !strings.Contains(view, "state=running vis=hidden") || !strings.Contains(view, "owner=-") || !strings.Contains(view, "conn=0 loc=0") || !strings.Contains(view, "BODY[command]") || !strings.Contains(view, "tail -f build.log") {
		t.Fatalf("expected structured terminal manager shell dialog body, got:\n%s", view)
	}
	if !strings.Contains(view, "OVERLAY[terminal_manager] FOCUS[overlay]") {
		t.Fatalf("expected wireframe overlay heading in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "ROWS[7] SELECTED[term-2]") {
		t.Fatalf("expected wireframe overlay selection summary in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "> [terminal] build-log") {
		t.Fatalf("expected wireframe overlay selected row in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "DETAIL[build-log] STATE[running] VIS[hidden]") {
		t.Fatalf("expected wireframe overlay detail summary in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "ACTIONS[jump connect_here new_tab floating]") {
		t.Fatalf("expected wireframe overlay action summary in rendered view, got:\n%s", view)
	}
}

func TestRuntimeRendererRendersWireframeWorkspacePickerTree(t *testing.T) {
	state := runtimeStateWithWorkspacePickerTarget()
	picker := workspacedomain.NewPickerState(state.Domain)
	picker.AppendQuery("ops")
	picker.ExpandSelected()
	state.UI.Overlay = types.OverlayState{
		Kind: types.OverlayWorkspacePicker,
		Data: picker,
	}
	state.UI.Focus.Layer = types.FocusLayerOverlay
	state.UI.Focus.OverlayTarget = types.OverlayWorkspacePicker

	view := runtimeRenderer{}.Render(state, nil)
	if !strings.Contains(view, "DIALOG[workspace_picker]") || !strings.Contains(view, "TREE[workspace]") || !strings.Contains(view, "TARGET[node]") || !strings.Contains(view, "BODY[tree] rows=6") || !strings.Contains(view, "selected=ws-2") || !strings.Contains(view, "query=ops") || !strings.Contains(view, "DETAIL[target]") || !strings.Contains(view, "kind=workspace depth=0") || !strings.Contains(view, "label=ops") {
		t.Fatalf("expected structured workspace picker shell dialog body, got:\n%s", view)
	}
	if !strings.Contains(view, "OVERLAY[workspace_picker] FOCUS[overlay]") {
		t.Fatalf("expected workspace picker wireframe overlay heading in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "ROWS[6] QUERY[ops] SELECTED[ws-2]") {
		t.Fatalf("expected workspace picker wireframe summary in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "TARGET[workspace] LABEL[ops] DEPTH[0]") {
		t.Fatalf("expected workspace picker target summary in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "  [tab] logs") || !strings.Contains(view, "    [pane] unconnected pane") {
		t.Fatalf("expected workspace picker tree rows in wireframe overlay, got:\n%s", view)
	}
}

func TestRuntimeRendererRendersWireframeLayoutResolveDialog(t *testing.T) {
	state := runtimeStateWithLayoutResolveTarget()

	view := runtimeRenderer{}.Render(state, nil)
	if !strings.Contains(view, "OVERLAY[layout_resolve] FOCUS[overlay]") {
		t.Fatalf("expected layout resolve wireframe heading in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "ROWS[3] PANE[pane-1] ROLE[backend-dev]") {
		t.Fatalf("expected layout resolve summary in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "HINT[env=dev service=api]") {
		t.Fatalf("expected layout resolve hint in wireframe dialog, got:\n%s", view)
	}
	if !strings.Contains(view, "> [connect_existing] connect existing") {
		t.Fatalf("expected selected layout resolve row in wireframe dialog, got:\n%s", view)
	}
}

func TestRuntimeRendererRendersWireframePromptDialog(t *testing.T) {
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
	state.UI.Focus.OverlayTarget = types.OverlayPrompt

	view := runtimeRenderer{}.Render(state, nil)
	if !strings.Contains(view, "DIALOG[prompt]") || !strings.Contains(view, "FIELDS[prompt]") || !strings.Contains(view, "ACTIVE[field]") || !strings.Contains(view, "BODY[fields] count=2") || !strings.Contains(view, "active=tags") || !strings.Contains(view, "DETAIL[active]") || !strings.Contains(view, "label=Tags") || !strings.Contains(view, "terminal=term-2") || !strings.Contains(view, "BODY[actions]") || !strings.Contains(view, "submit | cancel") {
		t.Fatalf("expected structured prompt shell dialog body, got:\n%s", view)
	}
	if !strings.Contains(view, "OVERLAY[prompt] FOCUS[prompt]") {
		t.Fatalf("expected prompt wireframe heading in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "PROMPT[edit_terminal_metadata]") || !strings.Contains(view, "TITLE[edit terminal metadata]") {
		t.Fatalf("expected prompt summary in wireframe dialog, got:\n%s", view)
	}
	if !strings.Contains(view, "ACTIVE[tags] VALUE[group=build]") {
		t.Fatalf("expected prompt active field summary in wireframe dialog, got:\n%s", view)
	}
	if !strings.Contains(view, "ACTIONS[submit cancel]") {
		t.Fatalf("expected prompt action summary in wireframe dialog, got:\n%s", view)
	}
}

func TestRuntimeRendererRendersWireframeMixedSlotWorkbench(t *testing.T) {
	state := runtimeStateWithMixedPaneSlots()
	renderer := runtimeRenderer{
		Screens: NewRuntimeTerminalStore(RuntimeSessions{
			Terminals: map[types.TerminalID]TerminalRuntimeSession{
				types.TerminalID("term-1"): {
					TerminalID: types.TerminalID("term-1"),
					Snapshot: &protocol.Snapshot{
						TerminalID: "term-1",
						Screen: protocol.ScreenData{
							Cells: [][]protocol.Cell{
								{{Content: "a"}, {Content: "p"}, {Content: "i"}, {Content: " "}, {Content: "u"}, {Content: "p"}},
							},
						},
					},
				},
			},
		}),
	}

	view := renderer.Render(state, nil)
	if !strings.Contains(view, "waiting for connect") || !strings.Contains(view, "n new | a connect") || !strings.Contains(view, "CARD[pane-3] deploy-log [owner]") || !strings.Contains(view, "process exited") || !strings.Contains(view, "history retained") || !strings.Contains(view, "FLOATING WINDOWS[1]") || !strings.Contains(view, "WINDOW CARD[float-empty] unconnected pane") || !strings.Contains(view, "GEOMETRY[60,2 20x8]") || !strings.Contains(view, "no terminal connected") {
		t.Fatalf("expected mixed-slot shell summaries in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "WORKBENCH split") {
		t.Fatalf("expected mixed slot workbench to use split wireframe, got:\n%s", view)
	}
	if !strings.Contains(view, "PANE[waiting pane] SLOT[waiting]") {
		t.Fatalf("expected waiting pane summary in wireframe workbench, got:\n%s", view)
	}
	if !strings.Contains(view, "PANE[deploy-log] ROLE[owner] STATE[exited]") {
		t.Fatalf("expected exited pane summary in wireframe workbench, got:\n%s", view)
	}
	if !strings.Contains(view, "FLOAT[float-empty] unconnected pane empty 60,2 20x8") {
		t.Fatalf("expected empty floating pane summary in wireframe workbench, got:\n%s", view)
	}
}

func TestRuntimeRendererRendersWireframeNestedSplitWorkbench(t *testing.T) {
	state := runtimeStateWithNestedSplitPaneTargets()
	renderer := runtimeRenderer{
		Screens: NewRuntimeTerminalStore(RuntimeSessions{
			Terminals: map[types.TerminalID]TerminalRuntimeSession{
				types.TerminalID("term-1"): {
					TerminalID: types.TerminalID("term-1"),
					Snapshot: &protocol.Snapshot{
						TerminalID: "term-1",
						Screen: protocol.ScreenData{
							Cells: [][]protocol.Cell{{{Content: "r"}, {Content: "e"}, {Content: "a"}, {Content: "d"}, {Content: "y"}}},
						},
					},
				},
				types.TerminalID("term-2"): {
					TerminalID: types.TerminalID("term-2"),
					Snapshot: &protocol.Snapshot{
						TerminalID: "term-2",
						Screen: protocol.ScreenData{
							Cells: [][]protocol.Cell{{{Content: "b"}, {Content: "u"}, {Content: "i"}, {Content: "l"}, {Content: "d"}}},
						},
					},
				},
				types.TerminalID("term-3"): {
					TerminalID: types.TerminalID("term-3"),
					Snapshot: &protocol.Snapshot{
						TerminalID: "term-3",
						Screen: protocol.ScreenData{
							Cells: [][]protocol.Cell{{{Content: "w"}, {Content: "a"}, {Content: "t"}, {Content: "c"}, {Content: "h"}}},
						},
					},
				},
			},
		}),
	}

	view := renderer.Render(state, nil)
	if !strings.Contains(view, "LAYOUT TREE") {
		t.Fatalf("expected nested split wireframe tree heading, got:\n%s", view)
	}
	if !strings.Contains(view, "split[horizontal] ratio[0.60] width[46]") {
		t.Fatalf("expected root split ratio/width in wireframe tree, got:\n%s", view)
	}
	if !strings.Contains(view, "split[vertical] ratio[0.50] width[30]") {
		t.Fatalf("expected nested split ratio/width in wireframe tree, got:\n%s", view)
	}
	if !strings.Contains(view, "> pane[api-dev] role[owner] state[running]") || !strings.Contains(view, "  pane[watcher] role[owner] state[running]") || !strings.Contains(view, "  pane[build-log] role[owner] state[running]") {
		t.Fatalf("expected nested pane states in wireframe tree, got:\n%s", view)
	}
}

func TestRuntimeRendererRendersWireframeOverlayBackdropAndReturnFocus(t *testing.T) {
	state := runtimeStateWithLayoutResolveTarget()

	view := runtimeRenderer{}.Render(state, nil)
	if !strings.Contains(view, "DIALOG[layout_resolve]") || !strings.Contains(view, "overlay active: layout_resolve") {
		t.Fatalf("expected shell overlay dialog summary, got:\n%s", view)
	}
	if !strings.Contains(view, "BACKDROP[active]") {
		t.Fatalf("expected wireframe overlay backdrop summary, got:\n%s", view)
	}
	if !strings.Contains(view, "CENTER[offset=10 width=58]") {
		t.Fatalf("expected wireframe overlay center summary, got:\n%s", view)
	}
	if !strings.Contains(view, "RETURN[tiled:ws-1/tab-1/pane-1]") {
		t.Fatalf("expected wireframe overlay return focus summary, got:\n%s", view)
	}
}

func TestRuntimeRendererRendersHelpOverlay(t *testing.T) {
	state := connectedRunAppState()
	state.UI.Overlay = types.OverlayState{
		Kind:        types.OverlayHelp,
		ReturnFocus: state.UI.Focus,
	}
	state.UI.Focus.Layer = types.FocusLayerOverlay
	state.UI.Focus.OverlayTarget = types.OverlayHelp
	state.UI.Mode = types.ModeState{Active: types.ModePicker}

	view := runtimeRenderer{}.Render(state, nil)
	if !strings.Contains(view, "SHELL[78x24 overlay=help]") || !strings.Contains(view, "STATE[tiled focus=overlay mode=picker overlay=help]") || !strings.Contains(view, "BODY[tiled t=1 f=0]") || !strings.Contains(view, "MASK[dimmed 78x24 help]") || !strings.Contains(view, "OVERLAY[help return=tiled:ws-1/tab-1/pane-1]") || !strings.Contains(view, "DIALOG[help]") || !strings.Contains(view, "TITLE[help]") || !strings.Contains(view, "RETURN TO[tiled:ws-1/tab-1/pane-1]") || !strings.Contains(view, "FOOTER[esc close]") || !strings.Contains(view, "ACTIONS[esc close]") {
		t.Fatalf("expected help overlay shell mask/dialog in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "overlay_bar: kind=help | focus=overlay") {
		t.Fatalf("expected help overlay bar in rendered view, got:\n%s", view)
	}
	if !strings.Contains(view, "help_bar: layer=tiled | pane=pane-1") {
		t.Fatalf("expected help overlay to describe current layer, got:\n%s", view)
	}
	if !strings.Contains(view, "help_most_used: Ctrl-p pane | Ctrl-t tab | Ctrl-w workspace | Ctrl-f picker | Ctrl-o floating | Ctrl-g global") {
		t.Fatalf("expected help overlay to list main entries, got:\n%s", view)
	}
	if !strings.Contains(view, "help_shared: owner controls terminal-level operations | follower observes without control") {
		t.Fatalf("expected help overlay to explain owner/follower, got:\n%s", view)
	}
	if !strings.Contains(view, "help_exit: close pane != stop terminal != detach TUI") {
		t.Fatalf("expected help overlay to explain close/stop/detach, got:\n%s", view)
	}
	if !strings.Contains(view, "shortcut_bar: Esc close | ? help") {
		t.Fatalf("expected help overlay shortcut bar in rendered view, got:\n%s", view)
	}
}

func twoDigitLabel(v int) string {
	return string(rune('0'+v/10)) + string(rune('0'+v%10))
}

func findLineWithPrefix(view string, prefix string) string {
	for _, line := range strings.Split(view, "\n") {
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	return ""
}

func findLineIndexWithPrefix(view string, prefix string) int {
	for index, line := range strings.Split(view, "\n") {
		if strings.HasPrefix(line, prefix) {
			return index
		}
	}
	return -1
}
