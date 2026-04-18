package render

import (
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/perftrace"
	"github.com/lozzow/termx/protocol"
	runtimestate "github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func TestRenderBodyCanvasOverlapSameRectContentChangeUsesDamagedRectPath(t *testing.T) {
	now := time.Date(2026, 4, 18, 14, 0, 0, 0, time.UTC)
	baseSurface := &spriteTestSurface{
		size: protocol.Size{Cols: 14, Rows: 5},
		screen: [][]protocol.Cell{
			protocolRowFromText("under layer 01"),
			protocolRowFromText("under layer 02"),
			protocolRowFromText("under layer 03"),
			protocolRowFromText("under layer 04"),
			protocolRowFromText("under layer 05"),
		},
		screenTimestamps: []time.Time{now, now, now, now, now},
	}
	floatSurface := &spriteTestSurface{
		size: protocol.Size{Cols: 8, Rows: 3},
		screen: [][]protocol.Cell{
			protocolRowFromText("FLOAT-1"),
			protocolRowFromText("FLOAT-2"),
			protocolRowFromText("FLOAT-3"),
		},
		screenTimestamps: []time.Time{now, now, now},
	}
	runtimeState := &VisibleRuntimeStateProxy{
		HostEmojiVS16Mode: shared.AmbiguousEmojiVariationSelectorRaw,
		Terminals: []runtimestate.VisibleTerminal{
			{
				TerminalID:     "term-base",
				Name:           "base",
				State:          "running",
				Surface:        baseSurface,
				SurfaceVersion: 1,
			},
			{
				TerminalID:     "term-float",
				Name:           "float",
				State:          "running",
				Surface:        floatSurface,
				SurfaceVersion: 1,
			},
		},
	}
	theme := defaultUITheme()
	entries1 := []paneRenderEntry{
		testPaneRenderEntry("pane-base", "term-base", workbench.Rect{X: 0, Y: 0, W: 16, H: 7}, false, false, theme, 1),
		testPaneRenderEntry("pane-float", "term-float", workbench.Rect{X: 5, Y: 2, W: 10, H: 5}, true, true, theme, 1),
	}

	coordinator := &Coordinator{}
	_ = renderBodyCanvas(coordinator, runtimeState, false, entries1, 20, 10)

	baseSurface.screen[1] = protocolRowFromText("under layer ZZ")
	baseSurface.screenTimestamps[1] = now.Add(time.Second)
	runtimeState.Terminals[0].SurfaceVersion = 2
	entries2 := []paneRenderEntry{
		testPaneRenderEntry("pane-base", "term-base", workbench.Rect{X: 0, Y: 0, W: 16, H: 7}, false, false, theme, 2),
		testPaneRenderEntry("pane-float", "term-float", workbench.Rect{X: 5, Y: 2, W: 10, H: 5}, true, true, theme, 1),
	}

	perftrace.Enable()
	perftrace.Reset()
	defer perftrace.Disable()

	got := renderBodyCanvas(coordinator, runtimeState, false, entries2, 20, 10)
	want := rebuildBodyCanvas(nil, entries2, 20, 10, emojiVariationSelectorModeForRuntime(runtimeState), TopChromeRows, nil, runtimeState)

	snapshot := perftrace.SnapshotCurrent()
	if event, ok := snapshot.Event("render.body.canvas.path.overlap_damaged_rect"); !ok || event.Count == 0 {
		t.Fatalf("expected overlap damaged-rect path, got events=%#v", snapshot.Events)
	}
	if event, ok := snapshot.Event("render.body.canvas.path.overlap_same_rect_dirty"); !ok || event.Count == 0 {
		t.Fatalf("expected same-rect overlap dirty path, got events=%#v", snapshot.Events)
	}
	if event, ok := snapshot.Event("render.body.canvas.path.overlap_full_rebuild"); ok && event.Count > 0 {
		t.Fatalf("expected overlap same-rect content change to avoid full rebuild, got events=%#v", snapshot.Events)
	}
	if gotRaw, wantRaw := strings.TrimRight(got.rawString(), "\n"), strings.TrimRight(want.rawString(), "\n"); gotRaw != wantRaw {
		t.Fatalf("expected damaged-rect canvas to match full rebuild raw output,\n got: %q\nwant: %q", gotRaw, wantRaw)
	}
	if gotANSI, wantANSI := got.String(), want.String(); gotANSI != wantANSI {
		t.Fatalf("expected damaged-rect canvas to match full rebuild styled output")
	}
}

func TestRedrawDamagedRectRowBandMatchesFullRebuildForStyledWideAndAmbiguousRows(t *testing.T) {
	now := time.Date(2026, 4, 18, 15, 0, 0, 0, time.UTC)
	surface := &spriteTestSurface{
		size: protocol.Size{Cols: 8, Rows: 4},
		screen: [][]protocol.Cell{
			{
				{Content: "R", Width: 1, Style: protocol.CellStyle{FG: "#ff0000", Bold: true}},
				{Content: "e", Width: 1, Style: protocol.CellStyle{FG: "#ff0000", Bold: true}},
				{Content: "d", Width: 1, Style: protocol.CellStyle{FG: "#ff0000", Bold: true}},
			},
			{
				{Content: "界", Width: 2},
				{Content: "", Width: 0},
				{Content: "A", Width: 1},
			},
			{
				{Content: "♻\uFE0F", Width: 2, Style: protocol.CellStyle{FG: "#00ff00"}},
				{Content: "", Width: 0},
				{Content: "B", Width: 1},
			},
			protocolRowFromText("tail"),
		},
		screenTimestamps: []time.Time{now, now, now, now},
	}
	runtimeState := &VisibleRuntimeStateProxy{
		HostEmojiVS16Mode: shared.AmbiguousEmojiVariationSelectorRaw,
		Terminals: []runtimestate.VisibleTerminal{{
			TerminalID:     "term-1",
			Name:           "styled",
			State:          "running",
			Surface:        surface,
			SurfaceVersion: 1,
		}},
	}
	theme := defaultUITheme()
	entry1 := testPaneRenderEntry("pane-1", "term-1", workbench.Rect{X: 1, Y: 1, W: 10, H: 6}, false, true, theme, 1)
	entries1 := []paneRenderEntry{entry1}

	coordinator := &Coordinator{}
	previousCanvas := renderBodyCanvas(coordinator, runtimeState, false, entries1, 14, 10)
	if previousCanvas == nil || coordinator.bodyCache == nil {
		t.Fatal("expected cached body canvas after initial render")
	}

	surface.screen[1] = []protocol.Cell{
		{Content: "界", Width: 2, Style: protocol.CellStyle{FG: "#ffaa00"}},
		{Content: "", Width: 0},
		{Content: "Z", Width: 1, Style: protocol.CellStyle{FG: "#ffaa00"}},
	}
	surface.screen[2] = []protocol.Cell{
		{Content: "❄\uFE0F", Width: 2, Style: protocol.CellStyle{FG: "#00ffff", Underline: true}},
		{Content: "", Width: 0},
		{Content: "Y", Width: 1, Style: protocol.CellStyle{FG: "#00ffff", Underline: true}},
	}
	surface.screenTimestamps[1] = now.Add(time.Second)
	surface.screenTimestamps[2] = now.Add(2 * time.Second)
	runtimeState.Terminals[0].SurfaceVersion = 2
	entry2 := testPaneRenderEntry("pane-1", "term-1", workbench.Rect{X: 1, Y: 1, W: 10, H: 6}, false, true, theme, 2)
	entries2 := []paneRenderEntry{entry2}

	dirty := workbench.Rect{X: 3, Y: 3, W: 3, H: 2}
	redrawDamagedRect(previousCanvas, coordinator.bodyCache, entries2, runtimeState, dirty)
	want := rebuildBodyCanvas(nil, entries2, 14, 10, emojiVariationSelectorModeForRuntime(runtimeState), TopChromeRows, nil, runtimeState)

	if gotRaw, wantRaw := previousCanvas.rawString(), want.rawString(); gotRaw != wantRaw {
		t.Fatalf("expected row-band damaged redraw to match full rebuild raw output,\n got: %q\nwant: %q", gotRaw, wantRaw)
	}
	if gotANSI, wantANSI := previousCanvas.String(), want.String(); gotANSI != wantANSI {
		t.Fatalf("expected row-band damaged redraw to match full rebuild styled output")
	}
}

func testPaneRenderEntry(paneID, terminalID string, rect workbench.Rect, floating, active bool, theme uiTheme, surfaceVersion uint64) paneRenderEntry {
	entry := paneRenderEntry{
		PaneID:     paneID,
		Rect:       rect,
		Title:      paneID,
		Theme:      theme,
		TerminalID: terminalID,
		Active:     active,
		Floating:   floating,
		ContentKey: paneContentKey{
			TerminalID:     terminalID,
			SurfaceVersion: surfaceVersion,
			Name:           paneID,
			State:          "running",
			ThemeBG:        theme.panelBG,
			TerminalKnown:  true,
		},
		FrameKey: paneFrameKey{
			Rect:      rect,
			Title:     paneID,
			ThemeBG:   theme.panelBG,
			Active:    active,
			Floating:  floating,
			Frameless: false,
		},
	}
	return entry
}
