package render

import (
	"strings"
	"testing"
	"time"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func TestComposedCanvasDrawSnapshot(t *testing.T) {
	snapshot := &protocol.Snapshot{
		Screen: protocol.ScreenData{Cells: [][]protocol.Cell{{{Content: "h", Width: 1}, {Content: "i", Width: 1}}}},
	}
	canvas := newComposedCanvas(4, 2)
	canvas.drawSnapshot(snapshot)
	output := canvas.rawString()
	if !strings.Contains(output, "hi") {
		t.Fatalf("expected snapshot text in canvas output, got %q", output)
	}
}

func TestDrawSnapshotWithOffsetShowsRestartMarker(t *testing.T) {
	snapshot := &protocol.Snapshot{
		Size:                 protocol.Size{Cols: 30, Rows: 1},
		Scrollback:           [][]protocol.Cell{{{Content: "o", Width: 1}, {Content: "l", Width: 1}, {Content: "d", Width: 1}}, {}},
		ScrollbackTimestamps: []time.Time{time.Date(2026, 4, 7, 12, 34, 55, 0, time.UTC), time.Date(2026, 4, 7, 12, 34, 56, 0, time.UTC)},
		ScrollbackRowKinds:   []string{"", protocol.SnapshotRowKindRestart},
		Screen:               protocol.ScreenData{Cells: [][]protocol.Cell{{{Content: "n", Width: 1}, {Content: "e", Width: 1}, {Content: "w", Width: 1}}}},
	}
	canvas := newComposedCanvas(30, 2)
	drawSnapshotWithOffset(canvas, workbench.Rect{X: 0, Y: 0, W: 30, H: 2}, snapshot, 1, defaultUITheme())
	output := xansi.Strip(canvas.rawString())
	if !strings.Contains(output, "restarted") {
		t.Fatalf("expected restart marker in drawn snapshot, got %q", output)
	}
}

func TestRenderPaneSnapshotUsesRect(t *testing.T) {
	snapshot := &protocol.Snapshot{
		Screen: protocol.ScreenData{Cells: [][]protocol.Cell{
			{{Content: "a", Width: 1}, {Content: "b", Width: 1}},
			{{Content: "c", Width: 1}, {Content: "d", Width: 1}},
		}},
	}
	visibleRuntime := &runtime.VisibleRuntime{Terminals: []runtime.VisibleTerminal{{TerminalID: "term-1", Snapshot: snapshot}}}
	pane := workbench.VisiblePane{ID: "pane-1", TerminalID: "term-1", Rect: workbench.Rect{W: 2, H: 2}}
	lines := renderPaneSnapshot(workbench.Rect{W: 2, H: 2}, pane, visibleRuntime)
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"ab", "cd"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected pane snapshot output to contain %q, got %q", want, joined)
		}
	}
}

func TestComposedCanvasStyleANSI(t *testing.T) {
	canvas := newComposedCanvas(5, 1)
	canvas.set(0, 0, drawCell{Content: "A", Width: 1, Style: drawStyle{FG: "#ff0000", Bold: true}})
	canvas.set(1, 0, drawCell{Content: "B", Width: 1})
	output := canvas.String()
	if !strings.Contains(output, "A") || !strings.Contains(output, "B") {
		t.Fatalf("expected styled output, got %q", output)
	}
	// Should contain ANSI escape
	if !strings.Contains(output, "\x1b[") {
		t.Fatalf("expected ANSI escapes in styled output, got %q", output)
	}
}

func TestComposedCanvasDrawText(t *testing.T) {
	canvas := newComposedCanvas(10, 1)
	canvas.drawText(2, 0, "hello", drawStyle{FG: "#00ff00"})
	output := canvas.rawString()
	if !strings.Contains(output, "hello") {
		t.Fatalf("expected 'hello' in canvas, got %q", output)
	}
}

func TestComposedCanvasDrawTextAdvancesByDisplayWidth(t *testing.T) {
	canvas := newComposedCanvas(8, 1)
	canvas.drawText(0, 0, "[界]X", drawStyle{FG: "#00ff00"})

	if got := canvas.cells[0][1].Content; got != "界" {
		t.Fatalf("expected wide glyph at x=1, got %q", got)
	}
	if !canvas.cells[0][2].Continuation {
		t.Fatalf("expected x=2 to be marked as continuation for wide glyph, got %#v", canvas.cells[0][2])
	}
	if got := canvas.cells[0][4].Content; got != "X" {
		t.Fatalf("expected trailing text to land after wide glyph width, got %q", got)
	}
}

func TestComposedCanvasDrawTextKeepsEmojiVariationSelectorClusterIntact(t *testing.T) {
	canvas := newComposedCanvas(6, 1)
	canvas.hostEmojiVS16Mode = shared.AmbiguousEmojiVariationSelectorAdvance
	canvas.drawText(0, 0, "♻️X", drawStyle{FG: "#00ff00"})

	if got := canvas.cells[0][0].Content; got != "♻️" {
		t.Fatalf("expected emoji variation sequence to stay in one cell, got %q", got)
	}
	// 中文说明：现在所有宿主模式都会把 FE0F 歧义 emoji 后一列物化成补偿空格；
	// 真正序列化时，再根据整行上下文决定是“隐藏成空格”还是“保留 raw 并补 ECH”。
	// 这里不再断言 Continuation 标记，重点是 emoji 仍然占 2 列宽度，下一列落在 index=2。
	if got := canvas.cells[0][2].Content; got != "X" {
		t.Fatalf("expected trailing text to land after emoji cluster width, got %q", got)
	}
}

// 中文说明：原先这里有一批 TestSerializeCellContent* 用来覆盖 Strip /
// Advance 模式下的 fallback 行为，以及 "advance 模式 contentString 用
// stable fallback" 的测试。那些都是基于 "可以把 FE0F 去掉并用空格补齐
// 宽度" 的假设，但在 kitty 这类即使去掉 FE0F 也会把 base char 按 emoji
// 渲染 2 列的终端上根本不成立。新策略里 serializeCellContent 整个函数
// 已经被移除；现在只保留 raw+ECH 的序列化，真正让 emoji 消失的是后续
// 覆盖写入对整个 footprint 的清理，所以这组 fallback 测试一并删掉。

func TestComposedCanvasDrawSnapshotRawModeContinuationSpaceOnlyForAmbiguousEmoji(t *testing.T) {
	// Regular wide characters (e.g. CJK) must NOT get a continuation space.
	canvas := newComposedCanvas(6, 1)
	canvas.hostEmojiVS16Mode = shared.AmbiguousEmojiVariationSelectorRaw
	canvas.drawSnapshot(&protocol.Snapshot{
		Screen: protocol.ScreenData{
			Cells: [][]protocol.Cell{{
				{Content: "界", Width: 2},
				{Content: "", Width: 0},
				{Content: "X", Width: 1},
			}},
		},
	})

	rendered := canvas.contentString()
	if strings.Contains(rendered, "界 X") {
		t.Fatalf("regular wide char must NOT get a continuation space, got %q", rendered)
	}
	if !strings.Contains(rendered, "界X") {
		t.Fatalf("expected wide char directly followed by next char, got %q", rendered)
	}
}

func TestComposedCanvasRawModeAmbiguousEmojiRowReemitOverwritesCompensationColumn(t *testing.T) {
	// 中文说明：第一帧含 FE0F emoji，第二帧同一行变成完全不同的内容。
	// 只要第一帧把 emoji 物化成真实空格而不是依赖宿主 advance，第二帧就
	// 不会留下旧帧残影。
	canvas := newComposedCanvas(6, 1)
	canvas.hostEmojiVS16Mode = shared.AmbiguousEmojiVariationSelectorRaw
	canvas.drawText(0, 0, "A❄️XY", drawStyle{})
	_ = canvas.contentString() // prime cache

	// Second frame: no emoji, completely different content at same row.
	canvas2 := newComposedCanvas(6, 1)
	canvas2.hostEmojiVS16Mode = shared.AmbiguousEmojiVariationSelectorRaw
	canvas2.drawText(0, 0, "ZZZZZZ", drawStyle{})
	rendered := canvas2.contentString()

	// 每一列都必须被显式写到：stripped text 必须正好是 6 个 Z，且不能
	// 出现跳过补偿列导致的 "ZZZZZ" 少一位（第一帧 emoji 补偿列的残影）。
	stripped := xansi.Strip(rendered)
	if !strings.Contains(stripped, "ZZZZZZ") {
		t.Fatalf("expected second frame to overwrite every column, got %q (stripped %q)", rendered, stripped)
	}
}

func TestComposedCanvasRawModeAmbiguousEmojiClearsWhenLaterBorderOverwritesCompensationColumn(t *testing.T) {
	canvas := newComposedCanvas(8, 1)
	canvas.hostEmojiVS16Mode = shared.AmbiguousEmojiVariationSelectorRaw
	canvas.drawText(0, 0, "A♻️:", drawStyle{})
	canvas.set(2, 0, drawCell{Content: "│", Width: 1})

	rendered := canvas.contentString()
	if strings.Contains(rendered, "♻️") {
		t.Fatalf("expected border overwrite on the emoji's second column to clear the whole emoji footprint, got %q", rendered)
	}
	if strings.Contains(rendered, xansi.ECH(1)) {
		t.Fatalf("expected cleared overlap row to avoid the raw+ECH path, got %q", rendered)
	}
	if got := xansi.Strip(rendered); !strings.Contains(got, "A │") {
		t.Fatalf("expected cleared row to keep the border and leave a blank where the emoji used to be, got %q", got)
	}
}

func TestComposedCanvasRawModeAmbiguousEmojiClearsWhenLaterBorderOverwritesLeadColumn(t *testing.T) {
	canvas := newComposedCanvas(8, 1)
	canvas.hostEmojiVS16Mode = shared.AmbiguousEmojiVariationSelectorRaw
	canvas.drawText(0, 0, "A♻️:", drawStyle{})
	canvas.set(1, 0, drawCell{Content: "│", Width: 1})

	rendered := canvas.contentString()
	if strings.Contains(rendered, "♻️") {
		t.Fatalf("expected border overwrite on the emoji lead column to clear the whole emoji footprint, got %q", rendered)
	}
	if strings.Contains(rendered, xansi.ECH(1)) {
		t.Fatalf("expected lead-column overwrite to avoid the raw+ECH path, got %q", rendered)
	}
	if got := xansi.Strip(rendered); !strings.Contains(got, "A│") {
		t.Fatalf("expected lead-column overwrite to place the border directly at the emoji lead, got %q", got)
	}
}

func TestComposedCanvasSetClearsWideCellContinuationWhenLeadOverwritten(t *testing.T) {
	canvas := newComposedCanvas(4, 1)
	canvas.set(0, 0, drawCell{Content: "♻️", Width: 2})
	canvas.set(0, 0, drawCell{Content: "A", Width: 1})

	if got := canvas.cells[0][0].Content; got != "A" {
		t.Fatalf("expected overwritten lead cell to contain replacement glyph, got %#v", canvas.cells[0][0])
	}
	if canvas.cells[0][1].Continuation {
		t.Fatalf("expected overwrite to clear stale continuation cell, got %#v", canvas.cells[0][1])
	}
	line := strings.Split(xansi.Strip(canvas.String()), "\n")[0]
	if got := xansi.StringWidth(line); got != 4 {
		t.Fatalf("expected rendered row width 4 after overwrite, got %d: %q", got, line)
	}
}

func TestComposedCanvasSetClearsWideCellLeadWhenContinuationOverwritten(t *testing.T) {
	canvas := newComposedCanvas(4, 1)
	canvas.set(0, 0, drawCell{Content: "♻️", Width: 2})
	canvas.set(1, 0, drawCell{Content: "│", Width: 1})

	if got := canvas.cells[0][1].Content; got != "│" {
		t.Fatalf("expected overwrite inside wide-cell footprint to preserve replacement glyph, got %#v", canvas.cells[0][1])
	}
	if got := canvas.cells[0][0].Content; got != " " || canvas.cells[0][0].Continuation {
		t.Fatalf("expected overwrite inside continuation to clear original lead cell, got %#v", canvas.cells[0][0])
	}
	line := strings.Split(xansi.Strip(canvas.String()), "\n")[0]
	if got := xansi.StringWidth(line); got != 4 {
		t.Fatalf("expected rendered row width 4 after continuation overwrite, got %d: %q", got, line)
	}
}

func TestComposedCanvasDirtyIntervalRebuildsOnlyTouchedChunks(t *testing.T) {
	canvas := newComposedCanvas(96, 1)
	canvas.drawText(0, 0, strings.Repeat("a", 96), drawStyle{FG: "#ffffff"})
	_ = canvas.contentString()

	// Full-row renders now populate chunk cache too, so later partial updates
	// can keep using the same chunk-stitched row representation.
	if len(canvas.rowChunks) == 0 {
		t.Fatalf("expected rowChunks slice to exist, got %#v", canvas.rowChunks)
	}
	if canvas.rowChunks[0] == nil || len(canvas.rowChunks[0]) < 3 {
		t.Fatalf("expected chunk cache to be populated after full-row fast path, got %#v", canvas.rowChunks[0])
	}

	// First partial update: touch only chunk 1 (x=40 falls in [32,63]).
	// Chunk 0 and chunk 2 must be reused; chunk 1 must be rebuilt.
	chunk0Before := canvas.rowChunks[0][0]
	chunk1Before := canvas.rowChunks[0][1]
	chunk2Before := canvas.rowChunks[0][2]
	canvas.set(40, 0, drawCell{Content: "Z", Width: 1, Style: drawStyle{FG: "#ff0000"}})
	_ = canvas.contentString()

	if len(canvas.rowChunks[0]) < 3 {
		t.Fatalf("expected chunk cache to be populated after partial update, got %#v", canvas.rowChunks[0])
	}
	if got := canvas.rowChunks[0][0]; got != chunk0Before {
		t.Fatalf("expected untouched chunk[0] to be reused after first partial update")
	}
	if got := canvas.rowChunks[0][1]; got == chunk1Before {
		t.Fatalf("expected dirty chunk[1] to be rebuilt after first partial update")
	}
	if got := canvas.rowChunks[0][2]; got != chunk2Before {
		t.Fatalf("expected untouched chunk[2] to be reused after first partial update")
	}
	chunk0After1 := canvas.rowChunks[0][0]
	chunk1After1 := canvas.rowChunks[0][1]
	chunk2After1 := canvas.rowChunks[0][2]

	// Second partial update: touch only chunk 2 (x=70 falls in [64,95]).
	// Chunk 0 and chunk 1 must be reused; chunk 2 must be rebuilt.
	canvas.set(70, 0, drawCell{Content: "W", Width: 1, Style: drawStyle{FG: "#00ff00"}})
	_ = canvas.contentString()

	if got := canvas.rowChunks[0][0]; got != chunk0After1 {
		t.Fatalf("expected untouched chunk[0] to be reused after second partial update")
	}
	if got := canvas.rowChunks[0][1]; got != chunk1After1 {
		t.Fatalf("expected untouched chunk[1] to be reused after second partial update")
	}
	if got := canvas.rowChunks[0][2]; got == chunk2After1 {
		t.Fatalf("expected dirty chunk[2] to be rebuilt after second partial update")
	}
	if canvas.rowDirty[0] || canvas.rowDirtyMin[0] != -1 || canvas.rowDirtyMax[0] != -1 {
		t.Fatalf("expected dirty interval to be cleared after ensureRowCache, got rowDirty=%v min=%d max=%d", canvas.rowDirty[0], canvas.rowDirtyMin[0], canvas.rowDirtyMax[0])
	}
}

func TestComposedCanvasChunkBoundaryRealignsAfterContinuation(t *testing.T) {
	width := rowDirtyChunkWidth + 2
	canvas := newComposedCanvas(width, 1)
	canvas.set(rowDirtyChunkWidth-1, 0, drawCell{Content: "界", Width: 2})
	canvas.set(rowDirtyChunkWidth+1, 0, drawCell{Content: "X", Width: 1})

	_ = canvas.contentString()
	line := strings.Split(canvas.rawString(), "\n")[0]
	if got := xansi.StringWidth(line); got != width {
		t.Fatalf("expected rendered row width %d, got %d: %q", width, got, line)
	}
	if !strings.Contains(line, "界") || !strings.Contains(line, "X") {
		t.Fatalf("expected chunk boundary row to preserve both wide glyph and trailing cell, got %q", line)
	}
}

func TestFillRectBlankClearsWideCellFootprintsCrossingClearBoundary(t *testing.T) {
	canvas := newComposedCanvas(5, 1)
	canvas.set(1, 0, drawCell{Content: "♻️", Width: 2})

	fillRect(canvas, workbench.Rect{X: 2, Y: 0, W: 2, H: 1}, blankDrawCell())

	if got := canvas.cells[0][1]; got.Content != " " || got.Continuation {
		t.Fatalf("expected partial blank fill to clear overlapping wide-cell lead, got %#v", got)
	}
	if got := canvas.cells[0][2]; got.Content != " " || got.Continuation {
		t.Fatalf("expected partial blank fill to clear overlapping continuation, got %#v", got)
	}
	line := strings.Split(xansi.Strip(canvas.String()), "\n")[0]
	if got := xansi.StringWidth(line); got != 5 {
		t.Fatalf("expected rendered row width 5 after partial blank fill, got %d: %q", got, line)
	}
}

func TestDrawPaneFrameKeepsDistinctVerticalDividerColumns(t *testing.T) {
	canvas := newComposedCanvas(40, 8)
	theme := uiTheme{}

	drawPaneFrame(canvas, workbench.Rect{X: 0, Y: 0, W: 20, H: 8}, false, false, "left", paneBorderInfo{}, theme, paneOverflowHints{}, true, false)
	drawPaneFrame(canvas, workbench.Rect{X: 20, Y: 0, W: 20, H: 8}, true, false, "right", paneBorderInfo{}, theme, paneOverflowHints{}, false, false)

	frame := canvas.rawString()
	if !strings.Contains(frame, "││") {
		t.Fatalf("expected split panes to keep both middle border columns, got:\n%s", frame)
	}
	if strings.Contains(frame, "┬") || strings.Contains(frame, "┴") {
		t.Fatalf("expected split panes to avoid merged divider junctions, got:\n%s", frame)
	}
}

func TestDrawPaneFrameFloatingPaneOverwritesUnderlyingBorderIntersections(t *testing.T) {
	canvas := newComposedCanvas(20, 10)
	theme := defaultUITheme()

	drawPaneFrame(canvas, workbench.Rect{X: 0, Y: 0, W: 20, H: 10}, false, false, "base", paneBorderInfo{}, theme, paneOverflowHints{}, false, false)
	drawPaneFrame(canvas, workbench.Rect{X: 0, Y: 3, W: 10, H: 5}, false, false, "float", paneBorderInfo{}, theme, paneOverflowHints{}, true, true)

	if got := canvas.cells[3][0].Content; got != "┌" {
		t.Fatalf("expected floating top-left corner to overwrite the inactive border instead of merging, got %q", got)
	}
	if got := canvas.cells[7][0].Content; got != "└" {
		t.Fatalf("expected floating bottom-left corner to overwrite the inactive border instead of merging, got %q", got)
	}
	if got := canvas.cells[3][0].Style.FG; got != theme.chromeAccent {
		t.Fatalf("expected floating active corner to keep active border color %q, got %q", theme.chromeAccent, got)
	}
	if got := canvas.cells[2][0].Style.FG; got != theme.panelBorder2 {
		t.Fatalf("expected underlying tiled border above the float to keep inactive color %q, got %q", theme.panelBorder2, got)
	}
}

func TestHexToRGB(t *testing.T) {
	rgb, ok := hexToRGB("#ff8000")
	if !ok {
		t.Fatal("expected valid hex")
	}
	if rgb[0] != 255 || rgb[1] != 128 || rgb[2] != 0 {
		t.Fatalf("unexpected rgb: %v", rgb)
	}
}

func TestParsePaletteColorHelpers(t *testing.T) {
	if got, ok := parseAnsiColor("ansi:12"); !ok || got != 12 {
		t.Fatalf("expected ansi palette parse, got value=%d ok=%v", got, ok)
	}
	if got, ok := parseIdxColor("idx:203"); !ok || got != 203 {
		t.Fatalf("expected idx palette parse, got value=%d ok=%v", got, ok)
	}
	if _, ok := parseAnsiColor("ansi:99"); ok {
		t.Fatal("expected out-of-range ansi palette to fail")
	}
	if _, ok := parseIdxColor("idx:-1"); ok {
		t.Fatal("expected negative idx palette to fail")
	}
}

func TestWriteSimpleCSIEmitsExpectedSequence(t *testing.T) {
	var b strings.Builder
	writeSimpleCSI(&b, 'G', 12)
	if got, want := b.String(), "\x1b[12G"; got != want {
		t.Fatalf("unexpected CSI sequence %q want %q", got, want)
	}
}

func TestStyleDiffANSIResetsWhenReturningToDefault(t *testing.T) {
	if got, want := styleDiffANSI(drawStyle{FG: "#ffffff", BG: "#000000", Bold: true}, drawStyle{}), "\x1b[0m"; got != want {
		t.Fatalf("unexpected reset diff %q want %q", got, want)
	}
}

func TestStyleDiffANSIUsesMinimalTransition(t *testing.T) {
	got := styleDiffANSI(drawStyle{FG: "#ffffff", BG: "#000000", Bold: true}, drawStyle{FG: "#ff0000", BG: "#000000", Bold: true})
	if strings.HasPrefix(got, "\x1b[0;") {
		t.Fatalf("expected minimal style diff instead of full reset, got %q", got)
	}
	if !strings.Contains(got, "38;2;255;0;0") {
		t.Fatalf("expected foreground transition in diff, got %q", got)
	}
}

func TestContentLinesCompressLongBlankRunsWithECH(t *testing.T) {
	canvas := newComposedCanvas(12, 1)
	for x := 0; x < 12; x++ {
		canvas.set(x, 0, drawCell{Content: " ", Width: 1, Style: drawStyle{BG: "#111111"}})
	}
	lines := canvas.contentLines()
	if len(lines) != 1 {
		t.Fatalf("expected one line, got %#v", lines)
	}
	if !strings.Contains(lines[0], "\x1b[12X") {
		t.Fatalf("expected long blank run to use ECH compression, got %q", lines[0])
	}
}

func TestWriteFGColorAndBGColorSupportAnsiIdxAndRGB(t *testing.T) {
	tests := []struct {
		name string
		fn   func(*strings.Builder, string)
		arg  string
		want string
	}{
		{name: "fg ansi", fn: writeFGColor, arg: "ansi:12", want: ";94"},
		{name: "bg idx", fn: writeBGColor, arg: "idx:203", want: ";48;5;203"},
		{name: "fg rgb", fn: writeFGColor, arg: "#ff8000", want: ";38;2;255;128;0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b strings.Builder
			tt.fn(&b, tt.arg)
			if got := b.String(); got != tt.want {
				t.Fatalf("unexpected color sequence %q want %q", got, tt.want)
			}
		})
	}
}

func TestItoa(t *testing.T) {
	if itoa(0) != "0" {
		t.Fatalf("itoa(0) = %q", itoa(0))
	}
	if itoa(42) != "42" {
		t.Fatalf("itoa(42) = %q", itoa(42))
	}
	if itoa(-7) != "-7" {
		t.Fatalf("itoa(-7) = %q", itoa(-7))
	}
}

func TestRenderFrameProjectsHostCursorForActiveShellPane(t *testing.T) {
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
	rt.Registry().GetOrCreate("term-1").Snapshot = &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 20, Rows: 4},
		Screen: protocol.ScreenData{
			Cells: [][]protocol.Cell{{{Content: "h", Width: 1}, {Content: "i", Width: 1}}},
		},
		Cursor: protocol.CursorState{Row: 0, Col: 0, Visible: true},
	}

	state := WithTermSize(AdaptVisibleStateWithSize(wb, rt, 20, 4), 20, 6)
	coordinator := NewCoordinator(func() VisibleRenderState { return state })
	frame := coordinator.RenderFrame()

	if got, want := coordinator.CursorSequence(), hostHiddenCursorANSI(1, 2, "", false); got != want {
		t.Fatalf("expected shell pane to keep a hidden host cursor parked in-pane, got frame=%q cursor=%q want=%q", frame, got, want)
	}
	if !frameContainsSyntheticCursorHighlight(frame, "h") {
		t.Fatalf("expected shell pane content to show a synthetic cursor highlight, got frame=%q cursor=%q", frame, coordinator.CursorSequence())
	}
}

func TestRenderFrameHidesHostCursorWhenActivePaneCursorInvisible(t *testing.T) {
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
	rt.Registry().GetOrCreate("term-1").Snapshot = &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 20, Rows: 4},
		Screen: protocol.ScreenData{
			Cells: [][]protocol.Cell{{{Content: "h", Width: 1}, {Content: "i", Width: 1}}},
		},
		Cursor: protocol.CursorState{Row: 0, Col: 0, Visible: false},
	}

	state := WithTermSize(AdaptVisibleStateWithSize(wb, rt, 20, 4), 20, 6)
	coordinator := NewCoordinator(func() VisibleRenderState { return state })
	frame := coordinator.RenderFrame()

	if got, want := coordinator.CursorSequence(), hostHiddenCursorANSI(1, 2, "", false); got != want {
		t.Fatalf("expected hidden active pane cursor to keep the host cursor parked in-pane, got frame=%q cursor=%q want=%q", frame, got, want)
	}
}

func TestRenderFrameProjectsHostCursorForAlternateScreenPane(t *testing.T) {
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
	rt.Registry().GetOrCreate("term-1").Snapshot = &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 20, Rows: 4},
		Screen: protocol.ScreenData{
			Cells:             [][]protocol.Cell{{{Content: "h", Width: 1}, {Content: "i", Width: 1}}},
			IsAlternateScreen: true,
		},
		Cursor: protocol.CursorState{Row: 0, Col: 0, Visible: true},
		Modes:  protocol.TerminalModes{AlternateScreen: true},
	}

	state := WithTermSize(AdaptVisibleStateWithSize(wb, rt, 20, 4), 20, 6)
	coordinator := NewCoordinator(func() VisibleRenderState { return state })
	frame := coordinator.RenderFrame()

	if got, want := coordinator.CursorSequence(), hostHiddenCursorANSI(1, 2, "", false); got != want {
		t.Fatalf("expected alternate-screen pane to keep a hidden host cursor parked in-pane, got frame=%q cursor=%q want=%q", frame, got, want)
	}
	if !frameContainsSyntheticCursorHighlight(frame, "h") {
		t.Fatalf("expected alternate-screen pane content to show a synthetic cursor highlight, got frame=%q cursor=%q", frame, coordinator.CursorSequence())
	}
}

func TestRenderFrameImmersiveZoomHidesChromeAndOtherPanes(t *testing.T) {
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
				"pane-1": {ID: "pane-1", Title: "shell", TerminalID: "term-1"},
				"pane-2": {ID: "pane-2", Title: "logs", TerminalID: "term-2"},
			},
			Root: &workbench.LayoutNode{
				Direction: workbench.SplitVertical,
				Ratio:     0.5,
				First:     workbench.NewLeaf("pane-1"),
				Second:    workbench.NewLeaf("pane-2"),
			},
		}},
	})

	rt := runtime.New(nil)
	rt.Registry().GetOrCreate("term-1").Name = "shell"
	rt.Registry().Get("term-1").State = "running"
	rt.Registry().Get("term-1").Snapshot = &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 20, Rows: 4},
		Screen: protocol.ScreenData{
			Cells: [][]protocol.Cell{
				repeatCells("zoom-pane"),
				repeatCells("only-this"),
			},
		},
		Cursor: protocol.CursorState{Visible: false},
	}
	rt.Registry().GetOrCreate("term-2").Name = "logs"
	rt.Registry().Get("term-2").State = "running"
	rt.Registry().Get("term-2").Snapshot = &protocol.Snapshot{
		TerminalID: "term-2",
		Size:       protocol.Size{Cols: 20, Rows: 4},
		Screen: protocol.ScreenData{
			Cells: [][]protocol.Cell{
				repeatCells("hidden-pane"),
			},
		},
		Cursor: protocol.CursorState{Visible: false},
	}

	state := WithTermSize(AdaptVisibleStateWithSize(wb, rt, 40, 10), 40, 10)
	frame := xansi.Strip(NewCoordinator(func() VisibleRenderState { return state }).RenderFrame())

	if strings.Contains(frame, "tab 1") || strings.Contains(frame, "terminals:") || strings.Contains(frame, "float:") {
		t.Fatalf("expected immersive zoom to hide top/bottom chrome:\n%s", frame)
	}
	if strings.Contains(frame, "┌") || strings.Contains(frame, "│") || strings.Contains(frame, "┘") {
		t.Fatalf("expected immersive zoom to hide pane borders:\n%s", frame)
	}
	if !strings.Contains(frame, "zoom-pane") || !strings.Contains(frame, "only-this") {
		t.Fatalf("expected zoomed pane content to remain visible:\n%s", frame)
	}
	if strings.Contains(frame, "hidden-pane") || strings.Contains(frame, "logs") {
		t.Fatalf("expected non-zoomed pane content to disappear:\n%s", frame)
	}
	if got := len(strings.Split(frame, "\n")); got != 10 {
		t.Fatalf("expected immersive zoom frame height 10, got %d lines:\n%s", got, frame)
	}
}

func TestProjectPaneCursorDrawsSyntheticCursorForPlainShell(t *testing.T) {
	canvas := newComposedCanvas(6, 3)
	rect := workbench.Rect{X: 1, Y: 1, W: 4, H: 1}
	snapshot := &protocol.Snapshot{
		Screen: protocol.ScreenData{
			Cells: [][]protocol.Cell{{{Content: "h", Width: 1}, {Content: "i", Width: 1}}},
		},
		Cursor: protocol.CursorState{Row: 0, Col: 0, Visible: true, Shape: "block"},
	}

	fillRect(canvas, rect, blankDrawCell())
	canvas.drawSnapshotInRect(rect, snapshot)
	projectPaneCursor(canvas, rect, snapshot, 0)

	cell := canvas.cells[1][1]
	if cell.Content != "h" {
		t.Fatalf("expected cursor overlay to preserve cell content, got %#v", cell)
	}
	if cell.Style.Reverse || cell.Style.FG != "#000000" || cell.Style.BG != "#ffffff" {
		t.Fatalf("expected synthetic cursor to force explicit white cursor colors, got %#v", cell.Style)
	}
	if canvas.cursorVisible {
		t.Fatalf("expected synthetic cursor path to keep host cursor hidden")
	}
}

func TestRenderFrameProjectsHostCursorOntoBlankShellCell(t *testing.T) {
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
	rt.Registry().GetOrCreate("term-1").Snapshot = &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 20, Rows: 4},
		Screen: protocol.ScreenData{
			Cells: [][]protocol.Cell{{{Content: "$", Width: 1}}},
		},
		Cursor: protocol.CursorState{Row: 0, Col: 1, Visible: true, Shape: "block"},
	}

	state := WithTermSize(AdaptVisibleStateWithSize(wb, rt, 20, 4), 20, 6)
	coordinator := NewCoordinator(func() VisibleRenderState { return state })
	frame := coordinator.RenderFrame()
	if got, want := coordinator.CursorSequence(), hostHiddenCursorANSI(2, 2, "block", false); got != want {
		t.Fatalf("expected blank shell cursor to keep a hidden host cursor on the blank cell, got frame=%q cursor=%q want=%q", frame, got, want)
	}
	if !strings.Contains(frame, styleANSI(drawStyle{FG: "#000000", BG: "#ffffff"})+" ") {
		t.Fatalf("expected blank shell cursor to render a synthetic highlight, got %q", frame)
	}
}

func TestRenderFrameProjectsHostCursorForLeftSplitPane(t *testing.T) {
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

	rt := runtime.New(nil)
	rt.Registry().GetOrCreate("term-1").Snapshot = &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 20, Rows: 4},
		Screen: protocol.ScreenData{
			IsAlternateScreen: true,
			Cells:             [][]protocol.Cell{{{Content: "$", Width: 1}}},
		},
		Cursor: protocol.CursorState{Row: 0, Col: 1, Visible: true, Shape: "block"},
		Modes:  protocol.TerminalModes{AlternateScreen: true, BracketedPaste: true, MouseTracking: true},
	}
	rt.Registry().GetOrCreate("term-2").Snapshot = &protocol.Snapshot{
		TerminalID: "term-2",
		Size:       protocol.Size{Cols: 20, Rows: 4},
		Screen: protocol.ScreenData{
			IsAlternateScreen: true,
			Cells:             [][]protocol.Cell{{{Content: "R", Width: 1}}},
		},
		Cursor: protocol.CursorState{Visible: false},
		Modes:  protocol.TerminalModes{AlternateScreen: true, BracketedPaste: true, MouseTracking: true},
	}

	state := WithTermSize(AdaptVisibleStateWithSize(wb, rt, 40, 4), 40, 6)
	entries := paneEntriesForTab(
		state.Workbench.Tabs[state.Workbench.ActiveTab],
		state.Workbench.FloatingPanes,
		40, 4,
		newRuntimeLookup(state.Runtime),
		bodyProjectionOptionsForVM(RenderVMFromVisibleState(state), true),
		uiThemeForRuntime(state.Runtime),
	)
	target, ok := activeEntryCursorRenderTarget(entries, state.Runtime)
	if !ok {
		t.Fatal("expected active cursor render target")
	}
	if !target.Visible {
		t.Fatalf("expected left split pane cursor target to remain visible, got %#v", target)
	}

	coordinator := NewCoordinator(func() VisibleRenderState { return state })
	frame := coordinator.RenderFrame()
	if got, want := coordinator.CursorSequence(), hostHiddenCursorANSI(target.X, target.Y+TopChromeRows, target.Shape, target.Blink); got != want {
		t.Fatalf("expected left split pane to keep a hidden host cursor parked in-pane, got %q want %q", got, want)
	}
	if !strings.Contains(frame, styleANSI(drawStyle{FG: "#000000", BG: "#ffffff"})+" ") {
		t.Fatalf("expected left split pane to render a synthetic cursor highlight, got %q", frame)
	}
}

func TestRenderFrameKeepsHostCursorForRightmostSplitPane(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-2",
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

	rt := runtime.New(nil)
	rt.Registry().GetOrCreate("term-1").Snapshot = &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 20, Rows: 4},
		Screen: protocol.ScreenData{
			IsAlternateScreen: true,
			Cells:             [][]protocol.Cell{{{Content: "L", Width: 1}}},
		},
		Cursor: protocol.CursorState{Visible: false},
		Modes:  protocol.TerminalModes{AlternateScreen: true, BracketedPaste: true, MouseTracking: true},
	}
	rt.Registry().GetOrCreate("term-2").Snapshot = &protocol.Snapshot{
		TerminalID: "term-2",
		Size:       protocol.Size{Cols: 20, Rows: 4},
		Screen: protocol.ScreenData{
			IsAlternateScreen: true,
			Cells:             [][]protocol.Cell{{{Content: "$", Width: 1}}},
		},
		Cursor: protocol.CursorState{Row: 0, Col: 1, Visible: true, Shape: "block"},
		Modes:  protocol.TerminalModes{AlternateScreen: true, BracketedPaste: true, MouseTracking: true},
	}

	state := WithTermSize(AdaptVisibleStateWithSize(wb, rt, 40, 4), 40, 6)
	entries := paneEntriesForTab(
		state.Workbench.Tabs[state.Workbench.ActiveTab],
		state.Workbench.FloatingPanes,
		40, 4,
		newRuntimeLookup(state.Runtime),
		bodyProjectionOptionsForVM(RenderVMFromVisibleState(state), true),
		uiThemeForRuntime(state.Runtime),
	)
	target, ok := activeEntryCursorRenderTarget(entries, state.Runtime)
	if !ok {
		t.Fatal("expected active cursor render target")
	}
	if !target.Visible {
		t.Fatalf("expected right split pane cursor target to remain visible, got %#v", target)
	}

	coordinator := NewCoordinator(func() VisibleRenderState { return state })
	_ = coordinator.RenderFrame()
	if got, want := coordinator.CursorSequence(), hostHiddenCursorANSI(target.X, target.Y+TopChromeRows, target.Shape, target.Blink); got != want {
		t.Fatalf("expected rightmost split pane to keep a hidden host cursor parked in-pane, got %q want %q", got, want)
	}
}

func TestRenderFrameRestoresSplitPaneHostCursorAfterOverlayBlinkOff(t *testing.T) {
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

	rt := runtime.New(nil)
	rt.Registry().GetOrCreate("term-1").Snapshot = &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 20, Rows: 4},
		Screen: protocol.ScreenData{
			IsAlternateScreen: true,
			Cells:             [][]protocol.Cell{{{Content: "$", Width: 1}}},
		},
		Cursor: protocol.CursorState{Row: 0, Col: 1, Visible: true, Shape: "block"},
		Modes:  protocol.TerminalModes{AlternateScreen: true, BracketedPaste: true, MouseTracking: true},
	}
	rt.Registry().GetOrCreate("term-2").Snapshot = &protocol.Snapshot{
		TerminalID: "term-2",
		Size:       protocol.Size{Cols: 20, Rows: 4},
		Screen: protocol.ScreenData{
			IsAlternateScreen: true,
			Cells:             [][]protocol.Cell{{{Content: "R", Width: 1}}},
		},
		Cursor: protocol.CursorState{Visible: false},
		Modes:  protocol.TerminalModes{AlternateScreen: true, BracketedPaste: true, MouseTracking: true},
	}

	base := WithTermSize(AdaptVisibleStateWithSize(wb, rt, 40, 4), 40, 6)
	state := WithOverlayPicker(base, &modal.PickerState{
		Items:     []modal.PickerItem{{TerminalID: "term-1", Name: "demo"}},
		Query:     "demo",
		Cursor:    2,
		CursorSet: true,
	})

	entries := paneEntriesForTab(
		base.Workbench.Tabs[base.Workbench.ActiveTab],
		base.Workbench.FloatingPanes,
		40, 4,
		newRuntimeLookup(base.Runtime),
		bodyProjectionOptionsForVM(RenderVMFromVisibleState(base), true),
		uiThemeForRuntime(base.Runtime),
	)
	target, ok := activeEntryCursorRenderTarget(entries, base.Runtime)
	if !ok {
		t.Fatal("expected active cursor render target")
	}
	if !target.Visible {
		t.Fatalf("expected left split pane cursor target to remain visible, got %#v", target)
	}

	current := state
	coordinator := NewCoordinator(func() VisibleRenderState { return current })
	_ = coordinator.RenderFrame()
	coordinator.AdvanceCursorBlink()
	_ = coordinator.RenderFrame()

	current = base
	frame := coordinator.RenderFrame()
	if !strings.Contains(frame, styleANSI(drawStyle{FG: "#000000", BG: "#ffffff"})+" ") {
		t.Fatalf("expected split pane to restore the synthetic cursor highlight after overlay close, got %q", frame)
	}
	if got, want := coordinator.CursorSequence(), hostHiddenCursorANSI(target.X, target.Y+TopChromeRows, target.Shape, target.Blink); got != want {
		t.Fatalf("expected overlay close to restore the split pane hidden host cursor anchor, got %q want %q", got, want)
	}
}

func TestRenderFrameProjectsHostCursorOntoTextCellWithDefaultColors(t *testing.T) {
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
	rt.Registry().GetOrCreate("term-1").Snapshot = &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 20, Rows: 4},
		Screen: protocol.ScreenData{
			Cells: [][]protocol.Cell{{{Content: "h", Width: 1}}},
		},
		Cursor: protocol.CursorState{Row: 0, Col: 0, Visible: true, Shape: "block"},
	}

	state := WithTermSize(AdaptVisibleStateWithSize(wb, rt, 20, 4), 20, 6)
	coordinator := NewCoordinator(func() VisibleRenderState { return state })
	frame := coordinator.RenderFrame()
	if got, want := coordinator.CursorSequence(), hostHiddenCursorANSI(1, 2, "block", false); got != want {
		t.Fatalf("expected text cursor to keep a hidden host cursor on the text cell, got frame=%q cursor=%q want=%q", frame, got, want)
	}
	if !frameContainsSyntheticCursorHighlight(frame, "h") {
		t.Fatalf("expected text cursor to render a synthetic highlight, got %q", frame)
	}
}

func frameContainsSyntheticCursorHighlight(frame, glyph string) bool {
	full := styleANSI(drawStyle{FG: "#000000", BG: "#ffffff"}) + glyph
	minimal := "\x1b[38;2;0;0;0;48;2;255;255;255m" + glyph
	return strings.Contains(frame, full) || strings.Contains(frame, minimal)
}

func TestProjectPaneCursorUsesVisibleBarCursorStyleOnTextCell(t *testing.T) {
	canvas := newComposedCanvas(6, 3)
	rect := workbench.Rect{X: 1, Y: 1, W: 4, H: 1}
	snapshot := &protocol.Snapshot{
		Screen: protocol.ScreenData{
			Cells: [][]protocol.Cell{{{Content: "h", Width: 1}}},
		},
		Cursor: protocol.CursorState{Row: 0, Col: 0, Visible: true, Shape: "bar"},
	}

	fillRect(canvas, rect, blankDrawCell())
	canvas.drawSnapshotInRect(rect, snapshot)
	projectPaneCursor(canvas, rect, snapshot, 0)

	cell := canvas.cells[1][1]
	if cell.Style.Reverse {
		t.Fatalf("expected bar cursor to avoid reverse-video fallback, got %#v", cell.Style)
	}
	if cell.Style.FG != "#000000" || cell.Style.BG != "#ffffff" {
		t.Fatalf("expected bar cursor to force visible fallback colors, got %#v", cell.Style)
	}
}

func TestRenderFrameKeepsHostCursorVisibleWithoutTicks(t *testing.T) {
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
	rt.Registry().GetOrCreate("term-1").Snapshot = &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 20, Rows: 4},
		Screen: protocol.ScreenData{
			Cells: [][]protocol.Cell{{{Content: "h", Width: 1}}},
		},
		Cursor: protocol.CursorState{Row: 0, Col: 0, Visible: true, Shape: "block", Blink: true},
	}

	state := WithTermSize(AdaptVisibleStateWithSize(wb, rt, 20, 4), 20, 6)
	coordinator := NewCoordinator(func() VisibleRenderState { return state })
	_ = coordinator.RenderFrame()
	if coordinator.AdvanceCursorBlink() {
		t.Fatal("expected steady host cursor to avoid render-driven blink ticks")
	}
	if got, want := coordinator.CursorSequence(), hostHiddenCursorANSI(1, 2, "block", true); !strings.Contains(got, want) {
		t.Fatalf("expected steady hidden host cursor anchor to remain projected, got %q", got)
	}
}

func TestCoordinatorDoesNotNeedCursorTicksForVisibleActivePane(t *testing.T) {
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
	rt.Registry().GetOrCreate("term-1").Snapshot = &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 20, Rows: 4},
		Screen: protocol.ScreenData{
			Cells: [][]protocol.Cell{{{Content: "h", Width: 1}}},
		},
		Cursor: protocol.CursorState{Row: 0, Col: 0, Visible: true, Shape: "bar"},
	}

	state := WithTermSize(AdaptVisibleStateWithSize(wb, rt, 20, 4), 20, 6)
	coordinator := NewCoordinator(func() VisibleRenderState { return state })
	if coordinator.NeedsCursorTicks() {
		t.Fatal("expected visible active pane cursor to stay steady without render ticks")
	}
}

func TestProjectPaneCursorDrawsSyntheticCursorOnCanvas(t *testing.T) {
	canvas := newComposedCanvas(6, 3)
	rect := workbench.Rect{X: 1, Y: 1, W: 4, H: 1}
	snapshot := &protocol.Snapshot{
		Screen: protocol.ScreenData{
			Cells:             [][]protocol.Cell{{{Content: "h", Width: 1}, {Content: "i", Width: 1}}},
			IsAlternateScreen: true,
		},
		Cursor: protocol.CursorState{Row: 0, Col: 0, Visible: true, Shape: "block"},
		Modes:  protocol.TerminalModes{AlternateScreen: true},
	}

	fillRect(canvas, rect, blankDrawCell())
	canvas.drawSnapshotInRect(rect, snapshot)
	projectPaneCursor(canvas, rect, snapshot, 0)

	cell := canvas.cells[1][1]
	if cell.Content != "h" {
		t.Fatalf("expected cursor overlay to preserve cell content, got %#v", cell)
	}
	if cell.Style.Reverse || cell.Style.FG != "#000000" || cell.Style.BG != "#ffffff" {
		t.Fatalf("expected cursor overlay to use explicit white cursor colors, got %#v", cell.Style)
	}
	if canvas.cursorVisible {
		t.Fatalf("expected synthetic cursor overlay to avoid host cursor movement")
	}
}

func TestProjectPaneCursorHidesWhenViewingScrollback(t *testing.T) {
	canvas := newComposedCanvas(6, 3)
	rect := workbench.Rect{X: 1, Y: 1, W: 4, H: 1}
	snapshot := &protocol.Snapshot{
		Screen: protocol.ScreenData{
			Cells: [][]protocol.Cell{{{Content: "h", Width: 1}, {Content: "i", Width: 1}}},
		},
		Cursor: protocol.CursorState{Row: 0, Col: 0, Visible: true, Shape: "block"},
	}

	fillRect(canvas, rect, blankDrawCell())
	canvas.drawSnapshotInRect(rect, snapshot)
	projectPaneCursor(canvas, rect, snapshot, 1)

	cell := canvas.cells[1][1]
	if cell.Style.Reverse {
		t.Fatalf("expected scrollback view to avoid synthetic cursor highlight, got %#v", cell.Style)
	}
	if canvas.cursorVisible {
		t.Fatalf("expected scrollback view to keep host cursor hidden")
	}
}

func TestDrawSnapshotWithOffsetMarksPanelAreaOutsideTerminalWithDots(t *testing.T) {
	canvas := newComposedCanvas(4, 3)
	rect := workbench.Rect{X: 0, Y: 0, W: 4, H: 3}
	snapshot := &protocol.Snapshot{
		Size: protocol.Size{Cols: 2, Rows: 1},
		Screen: protocol.ScreenData{
			Cells: [][]protocol.Cell{{{Content: "h", Width: 1}, {Content: "i", Width: 1}}},
		},
	}

	fillRect(canvas, rect, blankDrawCell())
	drawSnapshotWithOffset(canvas, rect, snapshot, 0, defaultUITheme())

	if got := canvas.rawString(); got != "hi··\n····\n····" {
		t.Fatalf("expected dot markers outside terminal extent, got %q", got)
	}
}

func TestDrawSnapshotWithOffsetUsesSnapshotHeightWhenRenderedRowsLagAfterShrink(t *testing.T) {
	canvas := newComposedCanvas(4, 3)
	rect := workbench.Rect{X: 0, Y: 0, W: 4, H: 3}
	snapshot := &protocol.Snapshot{
		Size: protocol.Size{Cols: 4, Rows: 1},
		Screen: protocol.ScreenData{
			Cells: [][]protocol.Cell{
				{{Content: "a", Width: 1}},
				{{Content: "b", Width: 1}},
				{{Content: "c", Width: 1}},
			},
		},
	}

	fillRect(canvas, rect, blankDrawCell())
	drawSnapshotWithOffset(canvas, rect, snapshot, 0, defaultUITheme())

	if got := canvas.rawString(); got != "a\n····\n····" {
		t.Fatalf("expected resized terminal height to blank stale lower rows with dots, got %q", got)
	}
}

func TestDrawSnapshotWithOffsetUsesSnapshotWidthWhenRenderedColsLagAfterShrink(t *testing.T) {
	canvas := newComposedCanvas(4, 1)
	rect := workbench.Rect{X: 0, Y: 0, W: 4, H: 1}
	snapshot := &protocol.Snapshot{
		Size: protocol.Size{Cols: 1, Rows: 1},
		Screen: protocol.ScreenData{
			Cells: [][]protocol.Cell{{
				{Content: "a", Width: 1},
				{Content: "b", Width: 1},
				{Content: "c", Width: 1},
			}},
		},
	}

	fillRect(canvas, rect, blankDrawCell())
	drawSnapshotWithOffset(canvas, rect, snapshot, 0, defaultUITheme())

	if got := canvas.rawString(); got != "a···" {
		t.Fatalf("expected resized terminal width to blank stale right-side cells with dots, got %q", got)
	}
}

func TestActiveEntryCursorTargetUsesFramelessRectForZoomedPane(t *testing.T) {
	entries := []paneRenderEntry{{
		PaneID:     "pane-1",
		TerminalID: "term-1",
		Rect:       workbench.Rect{X: 0, Y: 0, W: 10, H: 4},
		Frameless:  true,
		Active:     true,
	}}
	runtimeState := &runtime.VisibleRuntime{
		Terminals: []runtime.VisibleTerminal{{
			TerminalID: "term-1",
			Snapshot: &protocol.Snapshot{
				TerminalID: "term-1",
				Size:       protocol.Size{Cols: 10, Rows: 4},
				Screen: protocol.ScreenData{
					Cells: [][]protocol.Cell{{{Content: "x", Width: 1}}},
				},
				Cursor: protocol.CursorState{Row: 0, Col: 0, Visible: true},
			},
		}},
	}

	target, ok := activeEntryCursorTarget(entries, runtimeState)
	if !ok {
		t.Fatalf("expected active cursor target, got target=%#v ok=%v", target, ok)
	}
	if target.X != 0 || target.Y != 0 {
		t.Fatalf("expected frameless zoom cursor target to stay unshifted, got %#v", target)
	}
}

func TestRenderFrameUsesRenderedEntrySnapshotForHostCursorProjection(t *testing.T) {
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
	rt.Registry().GetOrCreate("term-1").Snapshot = &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 20, Rows: 4},
		Screen: protocol.ScreenData{
			Cells: [][]protocol.Cell{
				{{Content: "r", Width: 1}},
				{{Content: "u", Width: 1}},
			},
		},
		Cursor: protocol.CursorState{Row: 0, Col: 0, Visible: true},
	}

	override := &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 20, Rows: 4},
		Screen: protocol.ScreenData{
			Cells: [][]protocol.Cell{
				{{Content: "r", Width: 1}},
				{{Content: "u", Width: 1}},
			},
		},
		Cursor: protocol.CursorState{Row: 1, Col: 0, Visible: true},
	}

	state := WithPaneSnapshotOverride(WithTermSize(AdaptVisibleStateWithSize(wb, rt, 20, 4), 20, 6), "pane-1", override)
	coordinator := NewCoordinator(func() VisibleRenderState { return state })
	_ = coordinator.RenderFrame()

	// Cursor must follow the actual snapshot rendered in this frame (override row=1),
	// not the stale runtime snapshot cursor (row=0).
	if got, want := coordinator.CursorSequence(), hostHiddenCursorANSI(1, 3, "", false); !strings.Contains(got, want) {
		t.Fatalf("expected host cursor projection to follow rendered entry snapshot row, got %q", got)
	}
}

func TestRenderFramePrefersVisualCursorCellWhenAlternateScreenCursorStuckAtTop(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "claude", TerminalID: "term-1"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})

	rt := runtime.New(nil)
	rt.Registry().GetOrCreate("term-1").Snapshot = &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 20, Rows: 4},
		Screen: protocol.ScreenData{
			IsAlternateScreen: true,
			Cells: [][]protocol.Cell{
				{{Content: "t", Width: 1}, {Content: "o", Width: 1}},
				{{Content: "p", Width: 1}},
				{
					{Content: ">", Width: 1},
					{Content: " ", Width: 1, Style: protocol.CellStyle{FG: "#000000", BG: "#ffffff"}},
					{Content: " ", Width: 1},
				},
			},
		},
		Cursor: protocol.CursorState{Row: 0, Col: 1, Visible: true, Shape: "block"},
		Modes:  protocol.TerminalModes{AlternateScreen: true, MouseTracking: true, BracketedPaste: true},
	}

	state := WithTermSize(AdaptVisibleStateWithSize(wb, rt, 20, 4), 20, 6)
	coordinator := NewCoordinator(func() VisibleRenderState { return state })
	_ = coordinator.RenderFrame()

	if got, want := coordinator.CursorSequence(), hostHiddenCursorANSI(2, 4, "block", false); !strings.Contains(got, want) {
		t.Fatalf("expected visual cursor fallback at bottom input row, got %q", got)
	}
}

func TestRenderFrameIgnoresLongBottomHighlightWhenChoosingVisualCursorFallback(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "claude", TerminalID: "term-1"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})

	rt := runtime.New(nil)
	rt.Registry().GetOrCreate("term-1").Snapshot = &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 20, Rows: 4},
		Screen: protocol.ScreenData{
			IsAlternateScreen: true,
			Cells: [][]protocol.Cell{
				{{Content: "t", Width: 1}, {Content: "o", Width: 1}},
				{{Content: "p", Width: 1}},
				{
					{Content: "x", Width: 1, Style: protocol.CellStyle{FG: "#000000", BG: "#ffffff"}},
					{Content: "y", Width: 1, Style: protocol.CellStyle{FG: "#000000", BG: "#ffffff"}},
					{Content: "z", Width: 1, Style: protocol.CellStyle{FG: "#000000", BG: "#ffffff"}},
				},
			},
		},
		Cursor: protocol.CursorState{Row: 0, Col: 1, Visible: true, Shape: "block"},
		Modes:  protocol.TerminalModes{AlternateScreen: true, MouseTracking: true},
	}

	state := WithTermSize(AdaptVisibleStateWithSize(wb, rt, 20, 4), 20, 6)
	coordinator := NewCoordinator(func() VisibleRenderState { return state })
	_ = coordinator.RenderFrame()

	if got, want := coordinator.CursorSequence(), hostHiddenCursorANSI(2, 2, "block", false); !strings.Contains(got, want) {
		t.Fatalf("expected regular snapshot cursor to win when bottom highlight spans multiple cells, got %q", got)
	}
}

func TestDrawPaneFrameMarksOverflowWithStableCornerIndicators(t *testing.T) {
	canvas := newComposedCanvas(6, 4)
	rect := workbench.Rect{X: 0, Y: 0, W: 6, H: 4}

	drawPaneFrame(canvas, rect, false, false, "", paneBorderInfo{}, defaultUITheme(), paneOverflowHints{Right: true, Bottom: true}, false, false)

	lines := strings.Split(canvas.rawString(), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 rendered lines, got %d", len(lines))
	}
	if !strings.HasSuffix(lines[0], "┐") {
		t.Fatalf("expected top-right corner to remain solid, got %q", lines[0])
	}
	if !strings.HasPrefix(lines[3], "└") || !strings.HasSuffix(lines[3], "┘") {
		t.Fatalf("expected bottom corners to remain solid, got %q", lines[3])
	}
	if !strings.HasSuffix(lines[2], ">") {
		t.Fatalf("expected right overflow marker near bottom-right edge, got %q", lines[2])
	}
	if !strings.Contains(lines[3], "v┘") {
		t.Fatalf("expected bottom overflow marker before bottom-right corner, got %q", lines[3])
	}
}

func TestDrawPaneFrameHighlightsOverflowMarkersForActivePane(t *testing.T) {
	canvas := newComposedCanvas(8, 5)
	rect := workbench.Rect{X: 0, Y: 0, W: 8, H: 5}
	theme := uiThemeFromHostColors("#0b1020", "#dbeafe", nil)

	drawPaneFrame(canvas, rect, false, false, "", paneBorderInfo{}, theme, paneOverflowHints{Right: true, Bottom: true}, true, false)

	rightMarker := canvas.cells[rect.Y+rect.H-2][rect.X+rect.W-1]
	bottomMarker := canvas.cells[rect.Y+rect.H-1][rect.X+rect.W-2]
	borderCell := canvas.cells[rect.Y][rect.X+rect.W-1]

	if rightMarker.Style.FG == "" || bottomMarker.Style.FG == "" {
		t.Fatalf("expected active overflow markers to use explicit colors, got right=%#v bottom=%#v", rightMarker.Style, bottomMarker.Style)
	}
	if rightMarker.Style.FG == borderCell.Style.FG {
		t.Fatalf("expected active right marker color to differ from border, both %q", rightMarker.Style.FG)
	}
	if bottomMarker.Style.FG == borderCell.Style.FG {
		t.Fatalf("expected active bottom marker color to differ from border, both %q", bottomMarker.Style.FG)
	}
	if !rightMarker.Style.Bold || !bottomMarker.Style.Bold {
		t.Fatalf("expected active overflow markers to be bold, got right=%#v bottom=%#v", rightMarker.Style, bottomMarker.Style)
	}
}
