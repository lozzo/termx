package render

import (
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/lozzow/termx/tuiv2/workbench"
	localvterm "github.com/lozzow/termx/vterm"
)

func makeTestState() VisibleRenderState {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "shell", TerminalID: "term-1"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})
	rt := runtime.New(nil)
	rt.Registry().GetOrCreate("term-1").Name = "demo"
	rt.Registry().Get("term-1").State = "running"
	return WithTermSize(AdaptVisibleStateWithSize(wb, rt, 100, 28), 100, 30)
}

func TestRenderFrameNonEmpty(t *testing.T) {
	state := makeTestState()
	c := NewCoordinator(func() VisibleRenderState { return state })
	frame := c.RenderFrame()
	if frame == "" {
		t.Fatal("RenderFrame() returned empty string")
	}
}

func TestRenderFrameContainsWorkspaceName(t *testing.T) {
	state := makeTestState()
	c := NewCoordinator(func() VisibleRenderState { return state })
	frame := xansi.Strip(c.RenderFrame())
	if !strings.Contains(frame, "main") {
		t.Fatalf("frame missing workspace name:\n%s", frame)
	}
}

func TestRenderFrameContainsTabInfo(t *testing.T) {
	state := makeTestState()
	c := NewCoordinator(func() VisibleRenderState { return state })
	frame := xansi.Strip(c.RenderFrame())
	if !strings.Contains(frame, "tab 1") {
		t.Fatalf("frame missing tab info:\n%s", frame)
	}
}

func TestRenderFrameContainsPaneBorder(t *testing.T) {
	state := makeTestState()
	c := NewCoordinator(func() VisibleRenderState { return state })
	frame := xansi.Strip(c.RenderFrame())
	// Pane border should prefer runtime metadata name over pane title.
	if !strings.Contains(frame, "demo") {
		t.Fatalf("frame missing pane title 'demo':\n%s", frame)
	}
	// Should have box drawing characters
	if !strings.Contains(frame, "┌") || !strings.Contains(frame, "┘") {
		t.Fatalf("frame missing pane border box characters:\n%s", frame)
	}
}

func TestRenderFrameShowsCopyModeRowTimestampInPaneChrome(t *testing.T) {
	state := makeTestState()
	ts := time.Date(2026, 4, 7, 12, 34, 56, 0, time.UTC)
	snapshot := &protocol.Snapshot{
		TerminalID:           "term-1",
		Size:                 protocol.Size{Cols: 80, Rows: 2},
		Scrollback:           [][]protocol.Cell{{{Content: "o", Width: 1}, {Content: "l", Width: 1}, {Content: "d", Width: 1}}},
		ScrollbackTimestamps: []time.Time{ts},
		Screen:               protocol.ScreenData{Cells: [][]protocol.Cell{{{Content: "n", Width: 1}, {Content: "e", Width: 1}, {Content: "w", Width: 1}}}},
		ScreenTimestamps:     []time.Time{ts.Add(time.Second)},
		Cursor:               protocol.CursorState{Row: 0, Col: 0, Visible: true},
		Modes:                protocol.TerminalModes{AutoWrap: true},
		Timestamp:            ts.Add(2 * time.Second),
	}
	state.Runtime = &VisibleRuntimeStateProxy{Terminals: []runtime.VisibleTerminal{{
		TerminalID: "term-1",
		Name:       "demo",
		State:      "running",
		Snapshot:   snapshot,
	}}}
	state = WithCopyMode(state, "pane-1", 0, 0, 0, false, 0, 0)

	frame := xansi.Strip(NewCoordinator(func() VisibleRenderState { return state }).RenderFrame())
	if !strings.Contains(frame, copyModeTimestampLabel(snapshot, 0)) {
		t.Fatalf("expected copy mode timestamp in pane chrome:\n%s", frame)
	}
	if !strings.Contains(frame, copyModeRowPositionLabel(snapshot, 0)) {
		t.Fatalf("expected copy mode row position in pane chrome:\n%s", frame)
	}
}

func TestRenderFrameShowsCopyModeTimestampForBlankRow(t *testing.T) {
	state := makeTestState()
	ts := time.Date(2026, 4, 7, 12, 34, 56, 0, time.UTC)
	snapshot := &protocol.Snapshot{
		TerminalID:           "term-1",
		Size:                 protocol.Size{Cols: 80, Rows: 1},
		Scrollback:           [][]protocol.Cell{{}},
		ScrollbackTimestamps: []time.Time{ts},
		Screen:               protocol.ScreenData{Cells: [][]protocol.Cell{{{Content: "x", Width: 1}}}},
		ScreenTimestamps:     []time.Time{ts.Add(time.Second)},
		Cursor:               protocol.CursorState{Row: 0, Col: 0, Visible: true},
		Modes:                protocol.TerminalModes{AutoWrap: true},
		Timestamp:            ts.Add(2 * time.Second),
	}
	state.Runtime = &VisibleRuntimeStateProxy{Terminals: []runtime.VisibleTerminal{{
		TerminalID: "term-1",
		Name:       "demo",
		State:      "running",
		Snapshot:   snapshot,
	}}}
	state = WithCopyMode(state, "pane-1", 0, 0, 0, false, 0, 0)

	frame := xansi.Strip(NewCoordinator(func() VisibleRenderState { return state }).RenderFrame())
	if !strings.Contains(frame, copyModeTimestampLabel(snapshot, 0)) {
		t.Fatalf("expected blank row timestamp in pane chrome:\n%s", frame)
	}
}

func TestDrawPaneFrameUsesTieredChromeStylesForActivePane(t *testing.T) {
	canvas := newComposedCanvas(40, 6)
	rect := workbench.Rect{X: 0, Y: 0, W: 40, H: 6}
	theme := uiThemeFromHostColors("#0b1020", "#dbeafe", nil)
	border := paneBorderInfo{StateLabel: "●", ShareLabel: "⇄2", RoleLabel: "◆ owner"}

	drawPaneFrame(canvas, rect, false, false, "demo", border, theme, paneOverflowHints{}, true, false)
	layout, ok := paneTopBorderLabelsLayout(rect, "demo", border, paneChromeActionTokensForFrame(rect, "demo", border, false))
	if !ok {
		t.Fatal("expected pane chrome layout")
	}
	if len(layout.actionSlots) == 0 {
		t.Fatal("expected action slots in pane chrome")
	}

	titleFG := canvas.cells[rect.Y][layout.titleX].Style.FG
	metaFG := canvas.cells[rect.Y][layout.stateX].Style.FG
	actionFG := canvas.cells[rect.Y][layout.actionSlots[0].X].Style.FG

	if titleFG == "" || metaFG == "" || actionFG == "" {
		t.Fatalf("expected pane chrome styles to set explicit colors, got title=%q meta=%q action=%q", titleFG, metaFG, actionFG)
	}
	if titleFG == metaFG {
		t.Fatalf("expected active pane title to differ from meta, both %q", titleFG)
	}
	if actionFG == metaFG {
		t.Fatalf("expected action slots to differ from meta, both %q", actionFG)
	}
}

func TestDrawPaneFrameKeepsTopRightCornerAlignedWithWideBorderLabels(t *testing.T) {
	canvas := newComposedCanvas(40, 6)
	rect := workbench.Rect{X: 0, Y: 0, W: 40, H: 6}
	theme := uiThemeFromHostColors("#0b1020", "#dbeafe", nil)
	border := paneBorderInfo{StateLabel: paneRunningIcon(), RoleLabel: "◆ owner"}

	drawPaneFrame(canvas, rect, false, false, "demo界", border, theme, paneOverflowHints{}, true, false)

	lines := strings.Split(xansi.Strip(canvas.String()), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected at least two rendered lines, got %d", len(lines))
	}
	if got := xansi.StringWidth(lines[0]); got != rect.W {
		t.Fatalf("expected top border visual width %d, got %d: %q", rect.W, got, lines[0])
	}
	if got := xansi.StringWidth(lines[1]); got != rect.W {
		t.Fatalf("expected second row visual width %d, got %d: %q", rect.W, got, lines[1])
	}
	if !strings.HasSuffix(lines[0], "┐") {
		t.Fatalf("expected top row to end at the right corner, got %q", lines[0])
	}
	if !strings.HasSuffix(lines[1], "│") {
		t.Fatalf("expected second row to end at the right border, got %q", lines[1])
	}
}

func TestDrawPaneFrameKeepsTopRightCornerAlignedWithEmojiVariationTitle(t *testing.T) {
	canvas := newComposedCanvas(40, 6)
	canvas.hostEmojiVS16Mode = shared.AmbiguousEmojiVariationSelectorAdvance
	rect := workbench.Rect{X: 0, Y: 0, W: 40, H: 6}
	theme := uiThemeFromHostColors("#0b1020", "#dbeafe", nil)
	border := paneBorderInfo{StateLabel: paneRunningIcon(), RoleLabel: "◆ owner"}

	drawPaneFrame(canvas, rect, false, false, "RedmiBook♻️", border, theme, paneOverflowHints{}, true, false)

	lines := strings.Split(xansi.Strip(canvas.String()), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected at least two rendered lines, got %d", len(lines))
	}
	if got := xansi.StringWidth(lines[0]); got != rect.W {
		t.Fatalf("expected top border visual width %d, got %d: %q", rect.W, got, lines[0])
	}
	if got := xansi.StringWidth(lines[1]); got != rect.W {
		t.Fatalf("expected second row visual width %d, got %d: %q", rect.W, got, lines[1])
	}
	if !strings.HasSuffix(lines[0], "┐") {
		t.Fatalf("expected top row to end at the right corner, got %q", lines[0])
	}
	if !strings.HasSuffix(lines[1], "│") {
		t.Fatalf("expected second row to end at the right border, got %q", lines[1])
	}
}

func TestDrawPaneFrameKeepsTopRightCornerAlignedWithEmojiVariationTitleAcrossHostWidths(t *testing.T) {
	canvas := newComposedCanvas(40, 6)
	canvas.hostEmojiVS16Mode = shared.AmbiguousEmojiVariationSelectorStrip
	rect := workbench.Rect{X: 0, Y: 0, W: 40, H: 6}
	theme := uiThemeFromHostColors("#0b1020", "#dbeafe", nil)
	border := paneBorderInfo{StateLabel: paneRunningIcon(), RoleLabel: "◆ owner"}

	drawPaneFrame(canvas, rect, false, false, "RedmiBook♻️", border, theme, paneOverflowHints{}, true, false)

	for _, ambiguousWidth := range []int{1, 2} {
		host := newFakeHostFrame(rect.W, rect.H)
		host.apply(canvas.String(), ambiguousWidth)
		lines := host.lines()
		if len(lines) < 2 {
			t.Fatalf("expected at least two rendered lines, got %d", len(lines))
		}
		if !strings.HasSuffix(lines[0], "┐") {
			t.Fatalf("expected top row to keep the right corner when host advances ♻️ by %d column(s), got %q", ambiguousWidth, lines[0])
		}
		if !strings.HasSuffix(lines[1], "│") {
			t.Fatalf("expected second row to keep the right border when host advances ♻️ by %d column(s), got %q", ambiguousWidth, lines[1])
		}
	}
}

func TestRenderBodyKeepsPaneWidthStableWithEmojiVariationSnapshotNearRightEdge(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "shell", TerminalID: "term-1"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})

	row := []protocol.Cell{
		{Content: "ζ", Width: 1},
		{Content: " ", Width: 1},
		{Content: "♻️", Width: 2},
		{Content: "", Width: 0},
		{Content: ":", Width: 1},
		{Content: "♻️", Width: 2},
		{Content: "", Width: 0},
		{Content: ":", Width: 1},
		{Content: "♻️", Width: 2},
		{Content: "", Width: 0},
		{Content: ":", Width: 1},
	}
	bodyWidth := 14
	bodyHeight := 5
	state := WithTermSize(AdaptVisibleStateWithSize(wb, runtime.New(nil), bodyWidth, bodyHeight), bodyWidth, bodyHeight+2)
	state.Runtime = &VisibleRuntimeStateProxy{
		HostEmojiVS16Mode: shared.AmbiguousEmojiVariationSelectorAdvance,
		Terminals: []runtime.VisibleTerminal{{
			TerminalID: "term-1",
			Snapshot: &protocol.Snapshot{
				TerminalID: "term-1",
				Size:       protocol.Size{Cols: 11, Rows: 1},
				Screen:     protocol.ScreenData{Cells: [][]protocol.Cell{row}},
				Cursor:     protocol.CursorState{Visible: false},
				Modes:      protocol.TerminalModes{AutoWrap: true},
			},
		}}}

	body := xansi.Strip(renderBody(state, bodyWidth, bodyHeight))
	lines := strings.Split(body, "\n")
	if len(lines) != bodyHeight {
		t.Fatalf("expected %d body rows, got %d:\n%s", bodyHeight, len(lines), body)
	}
	for i, line := range lines {
		if got := xansi.StringWidth(line); got != bodyWidth {
			t.Fatalf("expected body row %d to stay width %d, got %d: %q", i, bodyWidth, got, line)
		}
	}
	if got := strings.Count(lines[1], "│"); got != 2 {
		t.Fatalf("expected content row to keep a single left/right border pair, got %d in %q", got, lines[1])
	}
}

func TestRenderBodyUsesStableFallbackForOneColumnAmbiguousEmojiVariationSelectorHosts(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "shell", TerminalID: "term-1"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})

	bodyWidth := 20
	bodyHeight := 4
	state := WithTermSize(AdaptVisibleStateWithSize(wb, runtime.New(nil), bodyWidth, bodyHeight), bodyWidth, bodyHeight+2)
	state.Runtime = &VisibleRuntimeStateProxy{
		HostEmojiVS16Mode: shared.AmbiguousEmojiVariationSelectorAdvance,
		Terminals: []runtime.VisibleTerminal{{
			TerminalID: "term-1",
			Snapshot: &protocol.Snapshot{
				TerminalID: "term-1",
				Size:       protocol.Size{Cols: 17, Rows: 1},
				Screen: protocol.ScreenData{Cells: [][]protocol.Cell{{
					{Content: "#", Width: 1},
					{Content: " ", Width: 1},
					{Content: "♻️", Width: 2},
					{Content: "", Width: 0},
					{Content: ":", Width: 1},
				}}},
				Cursor: protocol.CursorState{Visible: false},
				Modes:  protocol.TerminalModes{AutoWrap: true},
			},
		}},
	}

	body := renderBody(state, bodyWidth, bodyHeight)
	if !strings.Contains(body, "♻ :") {
		t.Fatalf("expected renderBody to use the stable fallback instead of cursor movement, got %q", body)
	}
}

func TestProtocolRowDisplayWidthIgnoresContinuationCells(t *testing.T) {
	row := []protocol.Cell{
		{Content: "ζ", Width: 1},
		{Content: " ", Width: 1},
		{Content: "♻️", Width: 2},
		{Content: "", Width: 0},
		{Content: ":", Width: 1},
	}

	if got := protocolRowDisplayWidth(row); got != 5 {
		t.Fatalf("expected row display width 5, got %d for %#v", got, row)
	}
}

func TestRenderBodyKeepsWidthStableForPromptWithEmojiVariationHostAcrossWidths(t *testing.T) {
	prompts := []struct {
		name   string
		prompt string
	}{
		{name: "emoji-variation", prompt: "# lozzow@RedmiBook♻️: ~/Documents/workdir/termx <>                                                                                             (23:17:15)"},
		{name: "single-wide-emoji", prompt: "# lozzow🙂: ~/Documents/workdir/termx <>                                                                                                       (23:17:15)"},
		{name: "zwj-emoji", prompt: "# lozzow👩‍💻: ~/Documents/workdir/termx <>                                                                                                      (23:17:15)"},
		{name: "cjk-wide", prompt: "# 终端界面: ~/Documents/workdir/termx <>                                                                                                         (23:17:15)"},
	}

	for _, promptCase := range prompts {
		t.Run(promptCase.name, func(t *testing.T) {
			for _, bodyWidth := range []int{100, 110, 118, 119, 120, 121, 122, 123, 124, 125} {
				wb := workbench.NewWorkbench()
				wb.AddWorkspace("main", &workbench.WorkspaceState{
					Name:      "main",
					ActiveTab: 0,
					Tabs: []*workbench.TabState{{
						ID:           "tab-1",
						Name:         "tab 1",
						ActivePaneID: "pane-1",
						Panes: map[string]*workbench.PaneState{
							"pane-1": {ID: "pane-1", Title: "shell", TerminalID: "term-1"},
						},
						Root: workbench.NewLeaf("pane-1"),
					}},
				})

				contentWidth := bodyWidth - 3
				vt := localvterm.New(contentWidth, 3, 10, nil)
				if _, err := vt.Write([]byte(promptCase.prompt)); err != nil {
					t.Fatalf("write prompt into vterm: %v", err)
				}

				state := WithTermSize(AdaptVisibleStateWithSize(wb, runtime.New(nil), bodyWidth, 6), bodyWidth, 8)
				state.Runtime = &VisibleRuntimeStateProxy{
					HostEmojiVS16Mode: shared.AmbiguousEmojiVariationSelectorAdvance,
					Terminals: []runtime.VisibleTerminal{{
						TerminalID: "term-1",
						Snapshot: &protocol.Snapshot{
							TerminalID: "term-1",
							Size:       protocol.Size{Cols: uint16(contentWidth), Rows: 3},
							Screen:     protocol.ScreenData{Cells: protocolRowsFromVTermCells(vt.ScreenContent().Cells)},
							Cursor:     protocol.CursorState{Visible: false},
							Modes:      protocol.TerminalModes{AutoWrap: true},
						},
					}}}

				rawBody := renderBody(state, bodyWidth, 6)
				if promptCase.name == "emoji-variation" && !strings.Contains(rawBody, "RedmiBook♻ :") {
					t.Fatalf("expected render output to use the stable fallback for ambiguous emoji variation selectors, got body:\n%q", rawBody)
				}
				body := xansi.Strip(rawBody)
				lines := strings.Split(body, "\n")
				if len(lines) != 6 {
					t.Fatalf("expected 6 body rows at width %d, got %d:\n%s", bodyWidth, len(lines), body)
				}
				for i, line := range lines {
					if got := xansi.StringWidth(line); got != bodyWidth {
						t.Fatalf("expected body row %d to stay width %d at frame width %d, got %d: %q", i, bodyWidth, bodyWidth, got, line)
					}
				}
			}
		})
	}
}

func TestRenderBodyKeepsSingleRightBorderForAmbiguousEmojiAdvanceModeWhenHostAdvancesOneColumn(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "shell", TerminalID: "term-1"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})

	bodyWidth := 120
	bodyHeight := 6
	contentWidth := bodyWidth - 3
	prompt := "# lozzow@RedmiBook♻️: ~/Documents/workdir/termx <>                                                                                             (23:17:15)"

	vt := localvterm.New(contentWidth, 3, 10, nil)
	if _, err := vt.Write([]byte(prompt)); err != nil {
		t.Fatalf("write prompt into vterm: %v", err)
	}

	makeState := func(mode shared.AmbiguousEmojiVariationSelectorMode) VisibleRenderState {
		state := WithTermSize(AdaptVisibleStateWithSize(wb, runtime.New(nil), bodyWidth, bodyHeight), bodyWidth, bodyHeight+2)
		state.Runtime = &VisibleRuntimeStateProxy{
			HostEmojiVS16Mode: mode,
			Terminals: []runtime.VisibleTerminal{{
				TerminalID: "term-1",
				Snapshot: &protocol.Snapshot{
					TerminalID: "term-1",
					Size:       protocol.Size{Cols: uint16(contentWidth), Rows: 3},
					Screen:     protocol.ScreenData{Cells: protocolRowsFromVTermCells(vt.ScreenContent().Cells)},
					Cursor:     protocol.CursorState{Visible: false},
					Modes:      protocol.TerminalModes{AutoWrap: true},
				},
			}},
		}
		return state
	}

	host := newFakeHostFrame(bodyWidth, bodyHeight)
	host.apply(renderBody(makeState(shared.AmbiguousEmojiVariationSelectorStrip), bodyWidth, bodyHeight), 1)
	host.apply(renderBody(makeState(shared.AmbiguousEmojiVariationSelectorAdvance), bodyWidth, bodyHeight), 1)

	promptLine := ""
	for _, line := range host.lines() {
		if strings.Contains(line, "RedmiBook") {
			promptLine = line
			break
		}
	}
	if promptLine == "" {
		t.Fatalf("expected prompt line in fake host frame:\n%s", strings.Join(host.lines(), "\n"))
	}
	if got := strings.Count(promptLine, "│"); got != 2 {
		t.Fatalf("expected advance-mode render to keep a single left/right border pair when host advances ♻️ by one column, got %d in %q", got, promptLine)
	}
	if !strings.HasSuffix(promptLine, "│") {
		t.Fatalf("expected prompt line to keep the right border in the last column, got %q", promptLine)
	}
}

func TestRenderBodyKeepsSingleRightBorderForAmbiguousEmojiRawModeWhenHostAdvancesTwoColumns(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "shell", TerminalID: "term-1"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})

	bodyWidth := 120
	bodyHeight := 6
	contentWidth := bodyWidth - 3
	prompt := "# lozzow@RedmiBook♻️: ~/Documents/workdir/termx <>                                                                                             (23:17:15)"

	vt := localvterm.New(contentWidth, 3, 10, nil)
	if _, err := vt.Write([]byte(prompt)); err != nil {
		t.Fatalf("write prompt into vterm: %v", err)
	}

	state := WithTermSize(AdaptVisibleStateWithSize(wb, runtime.New(nil), bodyWidth, bodyHeight), bodyWidth, bodyHeight+2)
	state.Runtime = &VisibleRuntimeStateProxy{
		HostEmojiVS16Mode: shared.AmbiguousEmojiVariationSelectorRaw,
		Terminals: []runtime.VisibleTerminal{{
			TerminalID: "term-1",
			Snapshot: &protocol.Snapshot{
				TerminalID: "term-1",
				Size:       protocol.Size{Cols: uint16(contentWidth), Rows: 3},
				Screen:     protocol.ScreenData{Cells: protocolRowsFromVTermCells(vt.ScreenContent().Cells)},
				Cursor:     protocol.CursorState{Visible: false},
				Modes:      protocol.TerminalModes{AutoWrap: true},
			},
		}},
	}

	host := newFakeHostFrame(bodyWidth, bodyHeight)
	host.apply(renderBody(state, bodyWidth, bodyHeight), 2)

	promptLine := ""
	for _, line := range host.lines() {
		if strings.Contains(line, "RedmiBook") {
			promptLine = line
			break
		}
	}
	if promptLine == "" {
		t.Fatalf("expected prompt line in fake host frame:\n%s", strings.Join(host.lines(), "\n"))
	}
	if got := strings.Count(promptLine, "│"); got != 2 {
		t.Fatalf("expected raw-mode render to keep a single left/right border pair when the host advances ♻️ by 2 columns, got %d in %q", got, promptLine)
	}
	if !strings.HasSuffix(promptLine, "│") {
		t.Fatalf("expected prompt line to keep the right border in the last column when the host advances ♻️ by 2 columns, got %q", promptLine)
	}
}

func TestRenderBodyKeepsSingleRightBorderForAmbiguousEmojiRawModeWhenHostAdvancesOneColumn(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "shell", TerminalID: "term-1"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})

	bodyWidth := 120
	bodyHeight := 6
	contentWidth := bodyWidth - 3
	prompt := "# lozzow@RedmiBook♻️: ~/Documents/workdir/termx <>                                                                                             (23:17:15)"

	vt := localvterm.New(contentWidth, 3, 10, nil)
	if _, err := vt.Write([]byte(prompt)); err != nil {
		t.Fatalf("write prompt into vterm: %v", err)
	}

	state := WithTermSize(AdaptVisibleStateWithSize(wb, runtime.New(nil), bodyWidth, bodyHeight), bodyWidth, bodyHeight+2)
	state.Runtime = &VisibleRuntimeStateProxy{
		HostEmojiVS16Mode: shared.AmbiguousEmojiVariationSelectorRaw,
		Terminals: []runtime.VisibleTerminal{{
			TerminalID: "term-1",
			Snapshot: &protocol.Snapshot{
				TerminalID: "term-1",
				Size:       protocol.Size{Cols: uint16(contentWidth), Rows: 3},
				Screen:     protocol.ScreenData{Cells: protocolRowsFromVTermCells(vt.ScreenContent().Cells)},
				Cursor:     protocol.CursorState{Visible: false},
				Modes:      protocol.TerminalModes{AutoWrap: true},
			},
		}},
	}

	host := newFakeHostFrame(bodyWidth, bodyHeight)
	host.apply(renderBody(state, bodyWidth, bodyHeight), 1)

	promptLine := ""
	for _, line := range host.lines() {
		if strings.Contains(line, "RedmiBook") {
			promptLine = line
			break
		}
	}
	if promptLine == "" {
		t.Fatalf("expected prompt line in fake host frame:\n%s", strings.Join(host.lines(), "\n"))
	}
	if got := strings.Count(promptLine, "│"); got != 2 {
		t.Fatalf("expected raw-mode render to keep a single left/right border pair when the host advances ♻️ by 1 column, got %d in %q", got, promptLine)
	}
	if !strings.HasSuffix(promptLine, "│") {
		t.Fatalf("expected prompt line to keep the right border in the last column when the host advances ♻️ by 1 column, got %q", promptLine)
	}
}

func TestRenderBodyKeepsDistinctVerticalPaneBordersBetweenSplitPanes(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "left", TerminalID: "term-1"},
				"pane-2": {ID: "pane-2", Title: "right", TerminalID: "term-2"},
			},
			Root: &workbench.LayoutNode{
				Direction: workbench.SplitVertical,
				Ratio:     0.5,
				First:     workbench.NewLeaf("pane-1"),
				Second:    workbench.NewLeaf("pane-2"),
			},
		}},
	})

	bodyWidth := 40
	bodyHeight := 8
	state := WithTermSize(AdaptVisibleStateWithSize(wb, runtime.New(nil), bodyWidth, bodyHeight), bodyWidth, bodyHeight+2)

	body := xansi.Strip(renderBody(state, bodyWidth, bodyHeight))
	lines := strings.Split(body, "\n")
	if len(lines) != bodyHeight {
		t.Fatalf("expected %d body rows, got %d:\n%s", bodyHeight, len(lines), body)
	}
	if got := strings.Count(lines[1], "│"); got != 4 {
		t.Fatalf("expected split panes to keep distinct left/right borders with a double divider, got %d in %q", got, lines[1])
	}
	if !strings.Contains(lines[1], "││") {
		t.Fatalf("expected split panes to keep both middle border columns, got %q", lines[1])
	}
}

func TestRenderBodyKeepsDistinctHorizontalPaneBordersBetweenSplitPanes(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "top", TerminalID: "term-1"},
				"pane-2": {ID: "pane-2", Title: "bottom", TerminalID: "term-2"},
			},
			Root: &workbench.LayoutNode{
				Direction: workbench.SplitHorizontal,
				Ratio:     0.5,
				First:     workbench.NewLeaf("pane-1"),
				Second:    workbench.NewLeaf("pane-2"),
			},
		}},
	})

	bodyWidth := 40
	bodyHeight := 12
	state := WithTermSize(AdaptVisibleStateWithSize(wb, runtime.New(nil), bodyWidth, bodyHeight), bodyWidth, bodyHeight+2)

	body := xansi.Strip(renderBody(state, bodyWidth, bodyHeight))
	lines := strings.Split(body, "\n")
	if len(lines) != bodyHeight {
		t.Fatalf("expected %d body rows, got %d:\n%s", bodyHeight, len(lines), body)
	}
	mid := bodyHeight / 2
	if !strings.HasPrefix(lines[mid-1], "└") || !strings.HasSuffix(lines[mid-1], "┘") {
		t.Fatalf("expected upper pane to keep its own bottom border row, got %q", lines[mid-1])
	}
	if !strings.HasPrefix(lines[mid], "┌") || !strings.HasSuffix(lines[mid], "┐") {
		t.Fatalf("expected lower pane to keep its own top border row, got %q", lines[mid])
	}
}

type fakeHostFrame struct {
	width  int
	height int
	cells  [][]string
}

func newFakeHostFrame(width, height int) *fakeHostFrame {
	cells := make([][]string, height)
	for y := 0; y < height; y++ {
		cells[y] = make([]string, width)
		for x := 0; x < width; x++ {
			cells[y][x] = " "
		}
	}
	return &fakeHostFrame{width: width, height: height, cells: cells}
}

func (f *fakeHostFrame) apply(frame string, ambiguousWidth int) {
	if f == nil {
		return
	}
	row, col := 0, 0
	for i := 0; i < len(frame); {
		switch frame[i] {
		case '\x1b':
			consumed, nextRow, nextCol := consumeFakeHostEscape(frame[i:], row, col)
			if consumed <= 0 {
				i++
				continue
			}
			i += consumed
			row, col = nextRow, nextCol
		case '\n':
			row++
			col = 0
			i++
		default:
			if strings.HasPrefix(frame[i:], "♻️") {
				f.put(row, col, "♻️")
				col += ambiguousWidth
				i += len("♻️")
				continue
			}
			r, size := utf8.DecodeRuneInString(frame[i:])
			if r == utf8.RuneError && size == 1 {
				i++
				continue
			}
			f.put(row, col, string(r))
			col++
			i += size
		}
	}
}

func consumeFakeHostEscape(src string, row, col int) (int, int, int) {
	if len(src) < 2 || src[0] != '\x1b' || src[1] != '[' {
		return 0, row, col
	}
	i := 2
	for i < len(src) {
		b := src[i]
		if b >= 0x40 && b <= 0x7e {
			params := src[2:i]
			switch b {
			case 'C':
				col += fakeHostFirstParam(params, 1)
			case 'G':
				col = maxInt(0, fakeHostFirstParam(params, 1)-1)
			case 'H':
				parts := strings.Split(strings.TrimPrefix(params, "?"), ";")
				if len(parts) >= 1 {
					row = maxInt(0, fakeHostParseParam(parts[0], 1)-1)
				}
				if len(parts) >= 2 {
					col = maxInt(0, fakeHostParseParam(parts[1], 1)-1)
				}
			}
			return i + 1, row, col
		}
		i++
	}
	return 0, row, col
}

func fakeHostFirstParam(params string, fallback int) int {
	params = strings.TrimPrefix(params, "?")
	if params == "" {
		return fallback
	}
	if idx := strings.IndexByte(params, ';'); idx >= 0 {
		params = params[:idx]
	}
	return fakeHostParseParam(params, fallback)
}

func fakeHostParseParam(raw string, fallback int) int {
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func (f *fakeHostFrame) put(row, col int, content string) {
	if f == nil || row < 0 || row >= f.height || col < 0 || col >= f.width {
		return
	}
	f.cells[row][col] = content
}

func (f *fakeHostFrame) lines() []string {
	if f == nil {
		return nil
	}
	lines := make([]string, 0, f.height)
	for _, row := range f.cells {
		var b strings.Builder
		for _, cell := range row {
			if cell == "" {
				cell = " "
			}
			b.WriteString(cell)
		}
		lines = append(lines, b.String())
	}
	return lines
}

func TestRenderFrameKeepsSplitBoundaryStableAcrossRepeatedEmojiVariationUpdates(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "left", TerminalID: "term-1"},
				"pane-2": {ID: "pane-2", Title: "right", TerminalID: "term-2"},
			},
			Root: &workbench.LayoutNode{
				Direction: workbench.SplitVertical,
				Ratio:     0.5,
				First:     workbench.NewLeaf("pane-1"),
				Second:    workbench.NewLeaf("pane-2"),
			},
		}},
	})

	bodyWidth := 80
	bodyHeight := 8
	var state VisibleRenderState
	coordinator := NewCoordinator(func() VisibleRenderState { return state })

	patterns := []struct {
		name   string
		repeat string
	}{
		{name: "emoji-variation", repeat: "♻️:"},
		{name: "single-wide-emoji", repeat: "🙂:"},
		{name: "zwj-emoji", repeat: "👩‍💻:"},
		{name: "cjk-wide", repeat: "漢字:"},
	}

	for _, pattern := range patterns {
		t.Run(pattern.name, func(t *testing.T) {
			for count := 1; count <= 24; count++ {
				state = WithTermSize(AdaptVisibleStateWithSize(wb, runtime.New(nil), bodyWidth, bodyHeight), bodyWidth, bodyHeight+2)
				visible := state.Workbench
				leftPane := visible.Tabs[visible.ActiveTab].Panes[0]
				contentRect, ok := workbench.FramedPaneContentRect(leftPane.Rect, leftPane.SharedLeft, leftPane.SharedTop)
				if !ok {
					t.Fatal("expected left pane content rect")
				}

				vt := localvterm.New(contentRect.W, 4, 10, nil)
				if _, err := vt.Write([]byte("ζ " + strings.Repeat(pattern.repeat, count))); err != nil {
					t.Fatalf("write repeated prompt into vterm: %v", err)
				}

				state.Runtime = &VisibleRuntimeStateProxy{Terminals: []runtime.VisibleTerminal{
					{
						TerminalID: "term-1",
						Snapshot: &protocol.Snapshot{
							TerminalID: "term-1",
							Size:       protocol.Size{Cols: uint16(contentRect.W), Rows: 4},
							Screen:     protocol.ScreenData{Cells: protocolRowsFromVTermCells(vt.ScreenContent().Cells)},
							Cursor:     protocol.CursorState{Visible: false},
							Modes:      protocol.TerminalModes{AutoWrap: true},
						},
					},
					{
						TerminalID: "term-2",
						Snapshot: &protocol.Snapshot{
							TerminalID: "term-2",
							Size:       protocol.Size{Cols: 4, Rows: 4},
							Screen:     protocol.ScreenData{Cells: [][]protocol.Cell{{{Content: " ", Width: 1}}}},
							Cursor:     protocol.CursorState{Visible: false},
							Modes:      protocol.TerminalModes{AutoWrap: true},
						},
					},
				}}

				frame := xansi.Strip(coordinator.RenderFrame())
				lines := strings.Split(frame, "\n")
				for i, line := range lines {
					got := xansi.StringWidth(line)
					if got != bodyWidth {
						t.Fatalf("expected rendered row %d width %d at update %d, got %d: %q", i, bodyWidth, count, got, line)
					}
					if strings.Count(line, "│") > 4 {
						t.Fatalf("expected split layout to keep distinct pane borders without extra divider ghosts at update %d, got %q", count, line)
					}
				}
			}
		})
	}
}

func TestDrawPaneContentWithKeyClearsReservedRightGutterGhosts(t *testing.T) {
	canvas := newComposedCanvas(16, 6)
	entry := paneRenderEntry{
		PaneID:     "pane-1",
		TerminalID: "term-1",
		Rect:       workbench.Rect{X: 0, Y: 0, W: 16, H: 6},
		Theme:      defaultUITheme(),
	}
	contentRect := contentRectForEntry(entry)
	gutterX := entry.Rect.X + entry.Rect.W - 2
	gutterY := contentRect.Y
	canvas.set(gutterX, gutterY, drawCell{Content: "│", Width: 1})

	runtimeState := &VisibleRuntimeStateProxy{Terminals: []runtime.VisibleTerminal{{
		TerminalID: "term-1",
		Snapshot: &protocol.Snapshot{
			TerminalID: "term-1",
			Size:       protocol.Size{Cols: uint16(contentRect.W), Rows: uint16(contentRect.H)},
			Screen: protocol.ScreenData{
				Cells: [][]protocol.Cell{{
					{Content: "o", Width: 1},
					{Content: "k", Width: 1},
				}},
			},
			Cursor: protocol.CursorState{Visible: false},
			Modes:  protocol.TerminalModes{AutoWrap: true},
		},
	}}}

	drawPaneContentWithKey(canvas, entry.Rect, entry, runtimeState)

	if got := canvas.cells[gutterY][gutterX]; got.Content != " " || got.Continuation {
		t.Fatalf("expected reserved right gutter to be cleared during content redraw, got %#v", got)
	}
}

func protocolRowsFromVTermCells(rows [][]localvterm.Cell) [][]protocol.Cell {
	out := make([][]protocol.Cell, len(rows))
	for y, row := range rows {
		out[y] = make([]protocol.Cell, len(row))
		for x, cell := range row {
			out[y][x] = protocol.Cell{
				Content: cell.Content,
				Width:   cell.Width,
			}
		}
	}
	return out
}

func TestPaneEntriesForTabMarksViewportClippedPaneOverflow(t *testing.T) {
	tab := workbench.VisibleTab{
		ID:           "tab-1",
		ActivePaneID: "pane-1",
		Panes: []workbench.VisiblePane{{
			ID:   "pane-1",
			Rect: workbench.Rect{X: 8, Y: 1, W: 10, H: 6},
		}},
	}

	entries := paneEntriesForTab(tab, nil, 12, 6, runtimeLookup{}, "", "", -1, "", -1, true, VisibleRenderState{}, defaultUITheme())
	if len(entries) != 1 {
		t.Fatalf("expected one visible pane entry, got %d", len(entries))
	}
	if !entries[0].Overflow.Right {
		t.Fatalf("expected right overflow marker when pane extends past viewport, got %#v", entries[0].Overflow)
	}
	if !entries[0].Overflow.Bottom {
		t.Fatalf("expected bottom overflow marker when pane extends past viewport, got %#v", entries[0].Overflow)
	}
}

func TestEmptyPaneActionStylesSeparatePrimarySecondaryAndDanger(t *testing.T) {
	theme := uiThemeFromHostColors("#0b1020", "#dbeafe", nil)

	attach := emptyPaneActionDrawStyle(theme, HitRegionEmptyPaneAttach, false)
	create := emptyPaneActionDrawStyle(theme, HitRegionEmptyPaneCreate, false)
	manager := emptyPaneActionDrawStyle(theme, HitRegionEmptyPaneManager, false)
	close := emptyPaneActionDrawStyle(theme, HitRegionEmptyPaneClose, false)

	if attach.FG == "" || create.FG == "" || manager.FG == "" || close.FG == "" {
		t.Fatalf("expected empty pane action styles to define colors: %#v %#v %#v %#v", attach, create, manager, close)
	}
	if attach.FG == manager.FG {
		t.Fatalf("expected attach and manager to use different emphasis, both %q", attach.FG)
	}
	if close.FG == attach.FG {
		t.Fatalf("expected close to use danger emphasis, both %q", close.FG)
	}
	if !attach.Bold || !create.Bold || close.Bold == false {
		t.Fatalf("expected primary and danger actions to stay bold: attach=%#v create=%#v close=%#v", attach, create, close)
	}
	if manager.Bold {
		t.Fatalf("expected manager action to be secondary emphasis, got %#v", manager)
	}
}

func TestRenderFrameNilCoordinator(t *testing.T) {
	var c *Coordinator
	if got := c.RenderFrame(); got != "" {
		t.Fatalf("nil coordinator must return empty string, got %q", got)
	}
}

func TestRenderFrameNoState(t *testing.T) {
	c := NewCoordinator(func() VisibleRenderState { return VisibleRenderState{} })
	frame := xansi.Strip(c.RenderFrame())
	if !strings.Contains(frame, "tuiv2") {
		t.Fatalf("empty state frame should contain fallback 'tuiv2', got %q", frame)
	}
}

func TestRenderFrameHasTabBarAndStatusBar(t *testing.T) {
	state := makeTestState()
	c := NewCoordinator(func() VisibleRenderState { return state })
	frame := c.RenderFrame()
	lines := strings.Split(frame, "\n")
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 lines (tab bar + body + status bar), got %d", len(lines))
	}
	if !strings.Contains(lines[0], "main") {
		t.Fatalf("first line should be tab bar with workspace, got %q", lines[0])
	}
	// Last line should be status bar
	lastLine := lines[len(lines)-1]
	if !strings.Contains(lastLine, "[Ctrl]") && !strings.Contains(lastLine, "[P] PANE") {
		t.Fatalf("last line should be status bar, got %q", lastLine)
	}
}

func TestRenderBodyZoomedPaneOccupiesWholeBody(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			ZoomedPaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "left", TerminalID: "term-1"},
				"pane-2": {ID: "pane-2", Title: "right", TerminalID: "term-2"},
			},
			Root: &workbench.LayoutNode{
				Direction: workbench.SplitVertical,
				Ratio:     0.5,
				First:     workbench.NewLeaf("pane-1"),
				Second:    workbench.NewLeaf("pane-2"),
			},
		}},
	})
	state := WithTermSize(AdaptVisibleStateWithSize(wb, runtime.New(nil), 100, 28), 100, 30)

	body := renderBody(state, 100, 28)
	if !strings.Contains(body, "term-1") {
		t.Fatalf("expected zoomed pane body to remain visible:\n%s", body)
	}
	if strings.Contains(body, "right") || strings.Contains(body, "term-2") {
		t.Fatalf("expected non-zoomed pane to be hidden:\n%s", body)
	}
	if strings.Contains(body, "┌") || strings.Contains(body, "│") || strings.Contains(body, "┘") {
		t.Fatalf("expected zoomed pane body to be frameless:\n%s", body)
	}
}

func TestRenderBodyScrollbackOffsetShowsOlderRows(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			ScrollOffset: 1,
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "shell", TerminalID: "term-1"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})
	state := WithTermSize(AdaptVisibleStateWithSize(wb, runtime.New(nil), 40, 8), 40, 10)
	state.Runtime = &VisibleRuntimeStateProxy{Terminals: []runtime.VisibleTerminal{{
		TerminalID: "term-1",
		Snapshot: &protocol.Snapshot{
			Scrollback: [][]protocol.Cell{{{Content: "A", Width: 1}}},
			Screen:     protocol.ScreenData{Cells: [][]protocol.Cell{{{Content: "B", Width: 1}}, {{Content: "C", Width: 1}}}},
		},
	}}}

	body := renderBody(state, 40, 8)
	if !strings.Contains(body, "A") {
		t.Fatalf("expected scrollback row to be visible when offset > 0:\n%s", body)
	}
}

func TestRenderBodyCacheAlwaysRedrawsActivePaneContent(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "shell", TerminalID: "term-1"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})

	snapshot := &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 12, Rows: 4},
		Screen: protocol.ScreenData{Cells: [][]protocol.Cell{
			repeatCells("AAAA"),
			repeatCells("BBBB"),
			repeatCells("CCCC"),
			repeatCells("DDDD"),
		}},
	}

	state := WithTermSize(AdaptVisibleStateWithSize(wb, runtime.New(nil), 24, 8), 24, 10)
	state.Runtime = &VisibleRuntimeStateProxy{Terminals: []runtime.VisibleTerminal{{
		TerminalID: "term-1",
		Snapshot:   snapshot,
	}}}

	coordinator := NewCoordinator(func() VisibleRenderState { return state })
	first := xansi.Strip(renderBodyFrameWithCoordinator(coordinator, state, 24, 8).content)
	if !strings.Contains(first, "AAAA") {
		t.Fatalf("expected first render to contain original content, got %q", first)
	}

	snapshot.Screen.Cells[0][0].Content = "Z"

	second := xansi.Strip(renderBodyFrameWithCoordinator(coordinator, state, 24, 8).content)
	if !strings.Contains(second, "ZAAA") {
		t.Fatalf("expected cached render path to repaint active pane content, got %q", second)
	}
}

func TestRenderBodyDrawsFloatingPanesOnTop(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "base", TerminalID: "term-1"},
				"pane-2": {ID: "pane-2", Title: "float", TerminalID: "term-2"},
			},
			Root:     workbench.NewLeaf("pane-1"),
			Floating: []*workbench.FloatingState{{PaneID: "pane-2", Rect: workbench.Rect{X: 10, Y: 4, W: 24, H: 6}, Z: 1}},
		}},
	})
	state := WithTermSize(AdaptVisibleStateWithSize(wb, runtime.New(nil), 100, 28), 100, 30)

	body := renderBody(state, 100, 28)
	for _, want := range []string{"base", "flo"} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected body to contain %q:\n%s", want, body)
		}
	}
}

func TestRenderBodyFloatingPaneClearsUnderlyingContent(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-2",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "base", TerminalID: "term-1"},
				"pane-2": {ID: "pane-2", Title: "float", TerminalID: "term-2"},
			},
			Root:     workbench.NewLeaf("pane-1"),
			Floating: []*workbench.FloatingState{{PaneID: "pane-2", Rect: workbench.Rect{X: 8, Y: 3, W: 20, H: 6}, Z: 1}},
		}},
	})
	state := WithTermSize(AdaptVisibleStateWithSize(wb, runtime.New(nil), 40, 14), 40, 16)
	state.Runtime = &VisibleRuntimeStateProxy{Terminals: []runtime.VisibleTerminal{
		{
			TerminalID: "term-1",
			Snapshot: &protocol.Snapshot{
				Screen: protocol.ScreenData{Cells: [][]protocol.Cell{
					repeatCells("XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"),
					repeatCells("XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"),
					repeatCells("XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"),
					repeatCells("XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"),
					repeatCells("XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"),
					repeatCells("XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"),
				}},
			},
		},
		{
			TerminalID: "term-2",
			Name:       "float",
			State:      "running",
		},
	}}

	body := xansi.Strip(renderBody(state, 40, 14))
	lines := strings.Split(body, "\n")
	if len(lines) <= 5 {
		t.Fatalf("expected rendered body height, got %d lines:\n%s", len(lines), body)
	}
	line := []rune(lines[5])
	if len(line) <= 12 {
		t.Fatalf("expected rendered body width, got line %q", lines[5])
	}
	if got := string(line[12]); got != " " {
		t.Fatalf("expected floating interior to clear underlying content, got %q in line %q", got, lines[5])
	}
}

func TestRenderBodyCachedOverlapDoesNotPaintActivePaneOverFloating(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:              "tab-1",
			Name:            "tab 1",
			ActivePaneID:    "pane-1",
			FloatingVisible: true,
			Panes: map[string]*workbench.PaneState{
				"pane-1":  {ID: "pane-1", Title: "shell", TerminalID: "term-1"},
				"float-1": {ID: "float-1", Title: "float", TerminalID: "term-2"},
			},
			Root: workbench.NewLeaf("pane-1"),
			Floating: []*workbench.FloatingState{{
				PaneID: "float-1",
				Rect:   workbench.Rect{X: 10, Y: 4, W: 14, H: 6},
				Z:      0,
			}},
		}},
	})

	snapshot := &protocol.Snapshot{
		Screen: protocol.ScreenData{Cells: [][]protocol.Cell{
			repeatCells("XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"),
			repeatCells("XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"),
			repeatCells("XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"),
			repeatCells("XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"),
			repeatCells("XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"),
			repeatCells("XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"),
		}},
	}

	state := WithTermSize(AdaptVisibleStateWithSize(wb, runtime.New(nil), 40, 14), 40, 16)
	state.Runtime = &VisibleRuntimeStateProxy{Terminals: []runtime.VisibleTerminal{
		{TerminalID: "term-1", Snapshot: snapshot},
		{TerminalID: "term-2", Name: "float", State: "running"},
	}}

	coordinator := NewCoordinator(func() VisibleRenderState { return state })
	body := xansi.Strip(renderBodyFrameWithCoordinator(coordinator, state, 40, 14).content)
	lines := strings.Split(body, "\n")
	if got := string([]rune(lines[6])[12]); got != " " {
		t.Fatalf("expected floating interior blank on first render, got %q in %q", got, lines[6])
	}

	snapshot.Screen.Cells[0][0].Content = "Z"

	body = xansi.Strip(renderBodyFrameWithCoordinator(coordinator, state, 40, 14).content)
	lines = strings.Split(body, "\n")
	if got := string([]rune(lines[6])[12]); got != " " {
		t.Fatalf("expected cached overlap render to preserve floating interior, got %q in %q", got, lines[6])
	}
}

func TestRenderBodyFloatingPaneBorderCornersDoNotMergeUnderlyingPaneBorders(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:              "tab-1",
			Name:            "tab 1",
			ActivePaneID:    "float-1",
			FloatingVisible: true,
			Panes: map[string]*workbench.PaneState{
				"pane-1":  {ID: "pane-1", Title: "base", TerminalID: "term-1"},
				"float-1": {ID: "float-1", Title: "float", TerminalID: "term-2"},
			},
			Root: workbench.NewLeaf("pane-1"),
			Floating: []*workbench.FloatingState{{
				PaneID: "float-1",
				Rect:   workbench.Rect{X: 0, Y: 3, W: 18, H: 6},
				Z:      0,
			}},
		}},
	})

	state := WithTermSize(AdaptVisibleStateWithSize(wb, runtime.New(nil), 40, 12), 40, 14)
	body := xansi.Strip(renderBody(state, 40, 12))
	lines := strings.Split(body, "\n")
	if len(lines) != 12 {
		t.Fatalf("expected 12 body rows, got %d:\n%s", len(lines), body)
	}
	if strings.HasPrefix(lines[3], "├") || strings.HasPrefix(lines[3], "┼") {
		t.Fatalf("expected floating top-left corner to stay a corner instead of merging into the underlying border, got %q", lines[3])
	}
	if !strings.HasPrefix(lines[3], "┌") {
		t.Fatalf("expected floating top-left border row to start with ┌, got %q", lines[3])
	}
	if strings.HasPrefix(lines[8], "├") || strings.HasPrefix(lines[8], "┼") {
		t.Fatalf("expected floating bottom-left corner to stay a corner instead of merging into the underlying border, got %q", lines[8])
	}
	if !strings.HasPrefix(lines[8], "└") {
		t.Fatalf("expected floating bottom border row to start with └, got %q", lines[8])
	}
}

func repeatCells(text string) []protocol.Cell {
	cells := make([]protocol.Cell, 0, len(text))
	for _, ch := range text {
		cells = append(cells, protocol.Cell{Content: string(ch), Width: 1})
	}
	return cells
}

func TestRenderBodyShowsActionableEmptyStateForUnboundPane(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "shell"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})

	body := xansi.Strip(renderBody(WithTermSize(AdaptVisibleStateWithSize(wb, runtime.New(nil), 72, 12), 72, 14), 72, 12))
	for _, want := range []string{
		"unconnected",
		"No terminal attached",
		"Attach existing terminal",
		"[ Create new terminal ]",
		"[ Open terminal manager ]",
		"[ Close pane ]",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected actionable empty-state hint %q:\n%s", want, body)
		}
	}
}

func TestRenderBodyShowsRecoveryStateForEmptyWorkspace(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: -1,
	})

	body := xansi.Strip(renderBody(WithTermSize(AdaptVisibleStateWithSize(wb, runtime.New(nil), 72, 12), 72, 14), 72, 12))
	for _, want := range []string{
		"No tabs in this workspace",
		"Ctrl-F open terminal picker",
		"Ctrl-T then c create a new tab",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected empty-workspace recovery hint %q:\n%s", want, body)
		}
	}
}

func TestRenderBodyShowsRecoveryStateForEmptyTab(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:    "tab-1",
			Name:  "tab 1",
			Panes: map[string]*workbench.PaneState{},
		}},
	})

	body := xansi.Strip(renderBody(WithTermSize(AdaptVisibleStateWithSize(wb, runtime.New(nil), 72, 12), 72, 14), 72, 12))
	for _, want := range []string{
		"No panes in this tab",
		"Ctrl-F create the first pane via terminal picker",
		"Ctrl-T then c create a fresh tab",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected empty-tab recovery hint %q:\n%s", want, body)
		}
	}
}

func TestRenderBodyShowsExitedPaneMetaAndPreservesSnapshot(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "shell", TerminalID: "term-1"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})

	exitCode := 42
	state := WithTermSize(AdaptVisibleStateWithSize(wb, runtime.New(nil), 72, 12), 72, 14)
	state.Runtime = &VisibleRuntimeStateProxy{Terminals: []runtime.VisibleTerminal{{
		TerminalID: "term-1",
		Name:       "shell",
		State:      "exited",
		ExitCode:   &exitCode,
		Snapshot: &protocol.Snapshot{
			Screen: protocol.ScreenData{
				Cells: [][]protocol.Cell{
					{{Content: "l", Width: 1}, {Content: "a", Width: 1}, {Content: "s", Width: 1}, {Content: "t", Width: 1}, {Content: " ", Width: 1}, {Content: "o", Width: 1}, {Content: "u", Width: 1}, {Content: "t", Width: 1}, {Content: "p", Width: 1}, {Content: "u", Width: 1}, {Content: "t", Width: 1}},
				},
			},
		},
	}}}

	body := xansi.Strip(renderBody(state, 72, 12))
	for _, want := range []string{paneExitedIcon() + "42", "last output", "R restart current terminal", "Ctrl-F choose another terminal"} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected exited pane rendering to contain %q:\n%s", want, body)
		}
	}
}

func TestRenderBodyShowsPaneMetaForSharedOwner(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "shell", TerminalID: "term-1"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})

	state := WithTermSize(AdaptVisibleStateWithSize(wb, runtime.New(nil), 72, 12), 72, 14)
	state.Runtime = &VisibleRuntimeStateProxy{Terminals: []runtime.VisibleTerminal{{
		TerminalID:   "term-1",
		Name:         "shell",
		State:        "running",
		OwnerPaneID:  "pane-1",
		BoundPaneIDs: []string{"pane-1", "pane-2"},
	}}, Bindings: []runtime.VisiblePaneBinding{{
		PaneID:    "pane-1",
		Role:      "owner",
		Connected: true,
	}}}

	body := xansi.Strip(renderBody(state, 72, 12))
	if !strings.Contains(body, "◆ owner") || !strings.Contains(body, "⇄2") {
		t.Fatalf("expected shared owner pane meta in frame:\n%s", body)
	}
}

func TestRenderBodyShowsPaneMetaForSharedFollower(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-2",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "owner", TerminalID: "term-1"},
				"pane-2": {ID: "pane-2", Title: "follower", TerminalID: "term-1"},
			},
			Root: &workbench.LayoutNode{
				Direction: workbench.SplitVertical,
				Ratio:     0.5,
				First:     workbench.NewLeaf("pane-1"),
				Second:    workbench.NewLeaf("pane-2"),
			},
		}},
	})

	state := WithTermSize(AdaptVisibleStateWithSize(wb, runtime.New(nil), 72, 12), 72, 14)
	state.Runtime = &VisibleRuntimeStateProxy{
		Terminals: []runtime.VisibleTerminal{{
			TerminalID:   "term-1",
			Name:         "shell",
			State:        "running",
			OwnerPaneID:  "pane-1",
			BoundPaneIDs: []string{"pane-1", "pane-2"},
		}},
		Bindings: []runtime.VisiblePaneBinding{
			{PaneID: "pane-1", Role: "owner", Connected: true},
			{PaneID: "pane-2", Role: "follower", Connected: true},
		},
	}

	body := xansi.Strip(renderBody(state, 72, 12))
	if !strings.Contains(body, "follow") {
		t.Fatalf("expected shared follower pane meta in frame:\n%s", body)
	}
}

func TestRenderBodyShowsOverflowArrowWhenTerminalLargerThanVisiblePane(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "shell", TerminalID: "term-1"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})

	state := WithTermSize(AdaptVisibleStateWithSize(wb, runtime.New(nil), 18, 6), 18, 8)
	state.Runtime = &VisibleRuntimeStateProxy{Terminals: []runtime.VisibleTerminal{{
		TerminalID: "term-1",
		Name:       "shell",
		State:      "running",
		Snapshot: &protocol.Snapshot{
			Size: protocol.Size{Cols: 40, Rows: 10},
			Screen: protocol.ScreenData{
				Cells: [][]protocol.Cell{{{Content: "h", Width: 1}, {Content: "i", Width: 1}}},
			},
		},
	}}}

	body := xansi.Strip(renderBody(state, 18, 6))
	if !strings.Contains(body, ">") {
		t.Fatalf("expected overflow arrow when terminal is wider than pane:\n%s", body)
	}
}

func TestRenderBodyShowsDotsWhenTerminalSmallerThanVisiblePane(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "shell", TerminalID: "term-1"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})

	state := WithTermSize(AdaptVisibleStateWithSize(wb, runtime.New(nil), 18, 6), 18, 8)
	state.Runtime = &VisibleRuntimeStateProxy{Terminals: []runtime.VisibleTerminal{{
		TerminalID: "term-1",
		Name:       "shell",
		State:      "running",
		Snapshot: &protocol.Snapshot{
			Size: protocol.Size{Cols: 2, Rows: 1},
			Screen: protocol.ScreenData{
				Cells: [][]protocol.Cell{{{Content: "o", Width: 1}, {Content: "k", Width: 1}}},
			},
		},
	}}}

	body := xansi.Strip(renderBody(state, 18, 6))
	if !strings.Contains(body, "··") {
		t.Fatalf("expected dot fill when terminal is smaller than pane:\n%s", body)
	}
}

func TestRenderBodyPrefersTitleOverMetaInNarrowPane(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "shell", TerminalID: "term-1"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})

	state := WithTermSize(AdaptVisibleStateWithSize(wb, runtime.New(nil), 16, 8), 16, 10)
	state.Runtime = &VisibleRuntimeStateProxy{
		Terminals: []runtime.VisibleTerminal{{
			TerminalID:   "term-1",
			Name:         "shell",
			State:        "running",
			OwnerPaneID:  "pane-1",
			BoundPaneIDs: []string{"pane-1", "pane-2"},
		}},
		Bindings: []runtime.VisiblePaneBinding{
			{PaneID: "pane-1", Role: "owner", Connected: true},
		},
	}

	body := xansi.Strip(renderBody(state, 16, 8))
	if !strings.Contains(body, "shell") {
		t.Fatalf("expected pane title to survive in narrow pane:\n%s", body)
	}
	if strings.Contains(body, "owner") {
		t.Fatalf("expected compact meta to be dropped before title in narrow pane:\n%s", body)
	}
}

func TestRenderFrameUsesDedicatedTerminalPoolPageLayout(t *testing.T) {
	state := makeTestState()
	state = AttachTerminalPool(state, &modal.TerminalManagerState{
		Title:    "Terminal Pool",
		Footer:   "[Enter] here  [Ctrl-T] tab  [Ctrl-O] float  [Ctrl-E] edit  [Ctrl-K] kill  [Esc] close",
		Selected: 0,
		Items: []modal.PickerItem{
			{TerminalID: "term-1", Name: "shell", State: "visible", Description: "running · 1 pane bound"},
			{TerminalID: "term-2", Name: "logs", State: "parked", Description: "running · 0 panes bound"},
		},
	})
	state = WithStatus(state, "", "", string(input.ModeTerminalManager))
	frame := xansi.Strip(NewCoordinator(func() VisibleRenderState { return state }).RenderFrame())

	for _, want := range []string{"Terminal Pool", "term-1", "term-2", "[Enter] here", "[Ctrl-E] edit", "TERMINAL-MANAGER", "[Enter] HERE", "[Ctrl-T] TAB"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("expected terminal pool page to contain %q:\n%s", want, frame)
		}
	}
	if strings.Contains(frame, "demo") {
		t.Fatalf("expected workbench pane body to be replaced by terminal pool page:\n%s", frame)
	}
}

func TestRenderFrameTerminalPoolPageUsesUnifiedStatusBarWhenDetailsOverflow(t *testing.T) {
	state := makeTestState()
	state = AttachTerminalPool(state, &modal.TerminalManagerState{
		Title:    "Terminal Pool",
		Footer:   "[Enter] dedicated footer",
		Selected: 0,
		Items: []modal.PickerItem{
			{
				TerminalID:  "term-1",
				Name:        "shell",
				State:       "visible",
				Command:     "bash -lc 'run-long-command'",
				Location:    "main/tab 1/pane-1",
				Description: "running · 1 pane bound",
				Observed:    true,
			},
			{
				TerminalID:  "term-2",
				Name:        "logs",
				State:       "parked",
				Description: "running · 0 panes bound",
			},
		},
	})
	state = WithTermSize(state, 180, 10)
	state = WithStatus(state, "", "", "terminal-manager")

	frame := xansi.Strip(NewCoordinator(func() VisibleRenderState { return state }).RenderFrame())
	if strings.Contains(frame, "[Enter] dedicated footer") {
		t.Fatalf("expected terminal pool page footer to be removed from body:\n%s", frame)
	}
	for _, want := range []string{"TERMINAL-MANAGER", "[Enter] HERE", "[Ctrl-T] TAB", "[Ctrl-O] FLOAT", "[Esc] BACK"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("expected terminal pool unified status hint %q:\n%s", want, frame)
		}
	}
}
