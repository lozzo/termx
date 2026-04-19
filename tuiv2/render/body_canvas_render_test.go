package render

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/perftrace"
	"github.com/lozzow/termx/protocol"
	runtimestate "github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func TestRenderBodyCanvasOverlapSameRectContentChangeUsesFullComposePath(t *testing.T) {
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
	if event, ok := snapshot.Event("render.body.canvas.path.full_compose"); !ok || event.Count == 0 {
		t.Fatalf("expected full-compose body path, got events=%#v", snapshot.Events)
	}
	if gotRaw, wantRaw := strings.TrimRight(got.rawString(), "\n"), strings.TrimRight(want.rawString(), "\n"); gotRaw != wantRaw {
		t.Fatalf("expected full-compose canvas to match full rebuild raw output,\n got: %q\nwant: %q", gotRaw, wantRaw)
	}
	if gotANSI, wantANSI := got.String(), want.String(); gotANSI != wantANSI {
		t.Fatalf("expected full-compose canvas to match full rebuild styled output")
	}
}

func TestRenderBodyCanvasOverlapLargeSameRectScrollUsesFullComposePath(t *testing.T) {
	now := time.Date(2026, 4, 18, 16, 0, 0, 0, time.UTC)
	baseRows := 38
	baseScreen := make([][]protocol.Cell, 0, baseRows)
	baseTimestamps := make([]time.Time, 0, baseRows)
	for i := 0; i < baseRows; i++ {
		baseScreen = append(baseScreen, protocolRowFromText(fmt.Sprintf("base-row-%02d %s", i, strings.Repeat("x", 24))))
		baseTimestamps = append(baseTimestamps, now.Add(time.Duration(i)*time.Second))
	}
	baseSurface := &spriteTestSurface{
		size: protocol.Size{Cols: 98, Rows: uint16(baseRows)},
		scrollback: [][]protocol.Cell{
			protocolRowFromText("hist-row-00"),
		},
		scrollTimestamps: []time.Time{now.Add(-time.Second)},
		screen:           baseScreen,
		screenTimestamps: baseTimestamps,
	}
	floatSurface := &spriteTestSurface{
		size: protocol.Size{Cols: 16, Rows: 4},
		screen: [][]protocol.Cell{
			protocolRowFromText("FLOAT-A"),
			protocolRowFromText("FLOAT-B"),
			protocolRowFromText("FLOAT-C"),
			protocolRowFromText("FLOAT-D"),
		},
		screenTimestamps: []time.Time{now, now, now, now},
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
		testPaneRenderEntry("pane-base", "term-base", workbench.Rect{X: 0, Y: 0, W: 100, H: 40}, false, true, theme, 1),
		testPaneRenderEntry("pane-float", "term-float", workbench.Rect{X: 60, Y: 8, W: 20, H: 8}, true, false, theme, 1),
	}

	coordinator := &Coordinator{}
	_ = renderBodyCanvas(coordinator, runtimeState, false, entries1, 100, 40)

	// Simulate a scroll-up frame: one row moves into scrollback and the visible
	// screen shifts, which would otherwise force a whole-body rebuild in overlap
	// layouts because the base pane interior covers almost the full viewport.
	baseSurface.scrollback = append(baseSurface.scrollback, append([]protocol.Cell(nil), baseSurface.screen[0]...))
	baseSurface.scrollTimestamps = append(baseSurface.scrollTimestamps, now.Add(40*time.Second))
	baseSurface.screen = append(baseSurface.screen[1:], protocolRowFromText("base-row-new "+strings.Repeat("z", 24)))
	baseSurface.screenTimestamps = append(baseSurface.screenTimestamps[1:], now.Add(41*time.Second))
	runtimeState.Terminals[0].SurfaceVersion = 2
	entries2 := []paneRenderEntry{
		testPaneRenderEntry("pane-base", "term-base", workbench.Rect{X: 0, Y: 0, W: 100, H: 40}, false, true, theme, 2),
		testPaneRenderEntry("pane-float", "term-float", workbench.Rect{X: 60, Y: 8, W: 20, H: 8}, true, false, theme, 1),
	}

	perftrace.Enable()
	perftrace.Reset()
	defer perftrace.Disable()

	got := renderBodyCanvas(coordinator, runtimeState, false, entries2, 100, 40)
	want := rebuildBodyCanvas(nil, entries2, 100, 40, emojiVariationSelectorModeForRuntime(runtimeState), TopChromeRows, nil, runtimeState)

	snapshot := perftrace.SnapshotCurrent()
	if event, ok := snapshot.Event("render.body.canvas.path.full_compose"); !ok || event.Count == 0 {
		t.Fatalf("expected full-compose body path, got events=%#v", snapshot.Events)
	}
	if gotRaw, wantRaw := got.rawString(), want.rawString(); gotRaw != wantRaw {
		t.Fatalf("expected full-compose canvas to match full rebuild raw output,\n got: %q\nwant: %q", gotRaw, wantRaw)
	}
	if gotANSI, wantANSI := got.String(), want.String(); gotANSI != wantANSI {
		t.Fatalf("expected full-compose canvas to match full rebuild styled output")
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
