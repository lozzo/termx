package render

import (
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/perftrace"
	"github.com/lozzow/termx/protocol"
	runtimestate "github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/workbench"
)

type spriteTestSurface struct {
	size             protocol.Size
	cursor           protocol.CursorState
	modes            protocol.TerminalModes
	screen           [][]protocol.Cell
	scrollback       [][]protocol.Cell
	screenTimestamps []time.Time
	scrollTimestamps []time.Time
	screenKinds      []string
	scrollKinds      []string
}

func (s *spriteTestSurface) Size() protocol.Size { return s.size }

func (s *spriteTestSurface) Cursor() protocol.CursorState { return s.cursor }

func (s *spriteTestSurface) Modes() protocol.TerminalModes { return s.modes }

func (s *spriteTestSurface) IsAlternateScreen() bool { return false }

func (s *spriteTestSurface) ScreenRows() int { return len(s.screen) }

func (s *spriteTestSurface) ScrollbackRows() int { return len(s.scrollback) }

func (s *spriteTestSurface) TotalRows() int { return s.ScrollbackRows() + s.ScreenRows() }

func (s *spriteTestSurface) Row(rowIndex int) []protocol.Cell {
	if rowIndex < 0 {
		return nil
	}
	if rowIndex < len(s.scrollback) {
		return append([]protocol.Cell(nil), s.scrollback[rowIndex]...)
	}
	rowIndex -= len(s.scrollback)
	if rowIndex < 0 || rowIndex >= len(s.screen) {
		return nil
	}
	return append([]protocol.Cell(nil), s.screen[rowIndex]...)
}

func (s *spriteTestSurface) RowTimestamp(rowIndex int) time.Time {
	if rowIndex < 0 {
		return time.Time{}
	}
	if rowIndex < len(s.scrollTimestamps) {
		return s.scrollTimestamps[rowIndex]
	}
	rowIndex -= len(s.scrollback)
	if rowIndex < 0 || rowIndex >= len(s.screenTimestamps) {
		return time.Time{}
	}
	return s.screenTimestamps[rowIndex]
}

func (s *spriteTestSurface) RowKind(rowIndex int) string {
	if rowIndex < 0 {
		return ""
	}
	if rowIndex < len(s.scrollKinds) {
		return s.scrollKinds[rowIndex]
	}
	rowIndex -= len(s.scrollback)
	if rowIndex < 0 || rowIndex >= len(s.screenKinds) {
		return ""
	}
	return s.screenKinds[rowIndex]
}

func protocolRowFromText(text string) []protocol.Cell {
	runes := []rune(text)
	row := make([]protocol.Cell, 0, len(runes))
	for _, r := range runes {
		row = append(row, protocol.Cell{Content: string(r), Width: 1})
	}
	return row
}

func TestBuildPaneRenderEntryUsesLiveSurfaceVersionAndKeepsCursorOnlySpriteReuse(t *testing.T) {
	now := time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC)
	surface := &spriteTestSurface{
		size: protocol.Size{Cols: 6, Rows: 2},
		screen: [][]protocol.Cell{
			protocolRowFromText("alpha"),
			protocolRowFromText("bravo"),
		},
		screenTimestamps: []time.Time{now, now},
		screenKinds:      []string{"", ""},
	}
	lookup := runtimeLookup{
		terminals: map[string]*runtimestate.VisibleTerminal{
			"term-1": {
				TerminalID:     "term-1",
				Name:           "shell",
				State:          "running",
				Surface:        surface,
				SurfaceVersion: 1,
			},
		},
	}
	pane := workbench.VisiblePane{
		ID:         "pane-1",
		TerminalID: "term-1",
		Rect:       workbench.Rect{X: 0, Y: 0, W: 8, H: 4},
	}

	entryBefore := buildPaneRenderEntry(pane, pane.Rect, pane.Rect, true, pane.ID, 0, lookup, bodyProjectionOptions{}, defaultUITheme())
	runtimeState := &VisibleRuntimeStateProxy{
		Terminals: []runtimestate.VisibleTerminal{*lookup.terminals["term-1"]},
	}
	cache := &bodyRenderCache{}
	spriteBefore := cache.contentSprite(entryBefore, runtimeState)
	if spriteBefore == nil {
		t.Fatal("expected initial sprite")
	}
	surface.cursor = protocol.CursorState{Row: 1, Col: 2, Visible: true}
	lookup.terminals["term-1"].SurfaceVersion = 2
	runtimeState.Terminals[0].SurfaceVersion = 2
	entryCursorOnly := buildPaneRenderEntry(pane, pane.Rect, pane.Rect, true, pane.ID, 0, lookup, bodyProjectionOptions{}, defaultUITheme())
	if entryBefore.ContentKey.SurfaceVersion == entryCursorOnly.ContentKey.SurfaceVersion {
		t.Fatalf("expected live surfaces to use cheap surfaceVersion invalidation, got before=%d after=%d", entryBefore.ContentKey.SurfaceVersion, entryCursorOnly.ContentKey.SurfaceVersion)
	}
	spriteCursorOnly := cache.contentSprite(entryCursorOnly, runtimeState)
	if spriteCursorOnly != spriteBefore {
		t.Fatal("expected cursor-only surface changes to reuse the cached sprite canvas")
	}

	surface.screen[1] = protocolRowFromText("brawn")
	surface.screenTimestamps[1] = now.Add(time.Second)
	lookup.terminals["term-1"].SurfaceVersion = 3
	entryContentChanged := buildPaneRenderEntry(pane, pane.Rect, pane.Rect, true, pane.ID, 0, lookup, bodyProjectionOptions{}, defaultUITheme())
	if entryBefore.ContentKey.SurfaceVersion == entryContentChanged.ContentKey.SurfaceVersion {
		t.Fatalf("expected content update to change content version, got version=%d", entryBefore.ContentKey.SurfaceVersion)
	}
}

func TestContentSpriteIncrementallyUpdatesChangedRowsForSameDimensions(t *testing.T) {
	now := time.Date(2026, 4, 16, 12, 30, 0, 0, time.UTC)
	surface := &spriteTestSurface{
		size: protocol.Size{Cols: 5, Rows: 2},
		screen: [][]protocol.Cell{
			protocolRowFromText("hello"),
			protocolRowFromText("world"),
		},
		screenTimestamps: []time.Time{now, now},
	}
	runtimeState := &VisibleRuntimeStateProxy{
		Terminals: []runtimestate.VisibleTerminal{
			{
				TerminalID:     "term-1",
				Name:           "shell",
				State:          "running",
				Surface:        surface,
				SurfaceVersion: 1,
			},
		},
	}
	entry := paneRenderEntry{
		PaneID:     "pane-1",
		Rect:       workbench.Rect{X: 0, Y: 0, W: 5, H: 2},
		Frameless:  true,
		TerminalID: "term-1",
		Theme:      defaultUITheme(),
		ContentKey: paneContentKey{
			TerminalID:    "term-1",
			TerminalKnown: true,
		},
	}
	entry.ContentKey.SurfaceVersion = runtimeState.Terminals[0].SurfaceVersion

	cache := &bodyRenderCache{}
	sprite := cache.contentSprite(entry, runtimeState)
	if sprite == nil {
		t.Fatal("expected sprite on first render")
	}
	if got := strings.TrimSpace(sprite.rawString()); !strings.Contains(got, "hello") || !strings.Contains(got, "world") {
		t.Fatalf("unexpected initial sprite content: %q", got)
	}

	surface.screen[1] = protocolRowFromText("there")
	surface.screenTimestamps[1] = now.Add(time.Second)
	runtimeState.Terminals[0].SurfaceVersion = 2
	entry.ContentKey.SurfaceVersion = runtimeState.Terminals[0].SurfaceVersion

	perftrace.Enable()
	perftrace.Reset()
	defer perftrace.Disable()
	nextSprite := cache.contentSprite(entry, runtimeState)
	snapshot := perftrace.SnapshotCurrent()
	event, ok := snapshot.Event("render.pane_content_sprite.incremental")
	if !ok || event.Count == 0 {
		t.Fatalf("expected incremental sprite update, got events=%#v", snapshot.Events)
	}
	if nextSprite != sprite {
		t.Fatal("expected same sprite canvas reused when dimensions match")
	}
	got := strings.TrimSpace(nextSprite.rawString())
	if !strings.Contains(got, "hello") || !strings.Contains(got, "there") {
		t.Fatalf("expected sprite rows to preserve unchanged row and update changed row, got %q", got)
	}
}

func TestContentSpriteIgnoresTimestampOnlySurfaceChanges(t *testing.T) {
	now := time.Date(2026, 4, 16, 12, 45, 0, 0, time.UTC)
	surface := &spriteTestSurface{
		size: protocol.Size{Cols: 5, Rows: 2},
		screen: [][]protocol.Cell{
			protocolRowFromText("hello"),
			protocolRowFromText("world"),
		},
		screenTimestamps: []time.Time{now, now},
	}
	runtimeState := &VisibleRuntimeStateProxy{
		Terminals: []runtimestate.VisibleTerminal{{
			TerminalID:     "term-1",
			Name:           "shell",
			State:          "running",
			Surface:        surface,
			SurfaceVersion: 1,
		}},
	}
	entry := paneRenderEntry{
		PaneID:     "pane-1",
		Rect:       workbench.Rect{X: 0, Y: 0, W: 5, H: 2},
		Frameless:  true,
		TerminalID: "term-1",
		Theme:      defaultUITheme(),
		ContentKey: paneContentKey{
			TerminalID:    "term-1",
			TerminalKnown: true,
		},
	}
	entry.ContentKey.SurfaceVersion = 1

	cache := &bodyRenderCache{}
	sprite := cache.contentSprite(entry, runtimeState)
	if sprite == nil {
		t.Fatal("expected initial sprite")
	}
	previousWindow := cache.contentSprites[entry.PaneID].window

	surface.screenTimestamps = []time.Time{now.Add(time.Second), now.Add(2 * time.Second)}
	runtimeState.Terminals[0].SurfaceVersion = 2
	entry.ContentKey.SurfaceVersion = 2

	resolved := resolvePaneContent(entry, runtimeState, true)
	nextWindow := buildTerminalSourceWindowState(resolved.source, resolved.contentRect.H, resolved.renderOffset)
	if deltaPlan := planTerminalWindowDelta(previousWindow, nextWindow, terminalScreenUpdateHint{}); len(deltaPlan.changedRows) != 0 || deltaPlan.usesScroll() {
		t.Fatalf("expected timestamp-only change to remain a no-op plan, got %#v", deltaPlan)
	}

	perftrace.Enable()
	perftrace.Reset()
	defer perftrace.Disable()
	nextSprite := cache.contentSprite(entry, runtimeState)
	snapshot := perftrace.SnapshotCurrent()
	if event, ok := snapshot.Event("render.pane_content_sprite.incremental.row_redraw_rows"); ok && event.Bytes > 0 {
		t.Fatalf("expected timestamp-only change to avoid row redraws, got events=%#v", snapshot.Events)
	}
	if nextSprite != sprite {
		t.Fatal("expected timestamp-only change to reuse sprite canvas")
	}
	if got := nextSprite.rawString(); got != "hello\nworld" {
		t.Fatalf("expected sprite content to remain unchanged, got %q", got)
	}
}

func TestContentSpriteUsesExplicitChangedRowsHintWhenRowMetadataChurns(t *testing.T) {
	now := time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)
	surface := &spriteTestSurface{
		size: protocol.Size{Cols: 5, Rows: 3},
		screen: [][]protocol.Cell{
			protocolRowFromText("row00"),
			protocolRowFromText("row01"),
			protocolRowFromText("row02"),
		},
		screenTimestamps: []time.Time{
			now,
			now.Add(1 * time.Second),
			now.Add(2 * time.Second),
		},
		screenKinds: []string{"data", "data", "data"},
	}
	runtimeState := &VisibleRuntimeStateProxy{
		Terminals: []runtimestate.VisibleTerminal{{
			TerminalID:     "term-1",
			Name:           "shell",
			State:          "running",
			Surface:        surface,
			SurfaceVersion: 1,
		}},
	}
	entry := paneRenderEntry{
		PaneID:     "pane-1",
		Rect:       workbench.Rect{X: 0, Y: 0, W: 5, H: 3},
		Frameless:  true,
		TerminalID: "term-1",
		Theme:      defaultUITheme(),
		ContentKey: paneContentKey{
			TerminalID:    "term-1",
			TerminalKnown: true,
		},
		SurfaceVersion: 1,
	}
	entry.ContentKey.SurfaceVersion = 1

	cache := &bodyRenderCache{}
	sprite := cache.contentSprite(entry, runtimeState)
	if sprite == nil {
		t.Fatal("expected initial sprite")
	}

	surface.screen[1] = protocolRowFromText("rowXX")
	surface.screenTimestamps = []time.Time{
		now.Add(3 * time.Second),
		now.Add(4 * time.Second),
		now.Add(5 * time.Second),
	}
	runtimeState.Terminals[0].SurfaceVersion = 2
	runtimeState.Terminals[0].ScreenUpdate = runtimestate.VisibleScreenUpdateSummary{
		SurfaceVersion: 2,
		ChangedRows:    []int{1},
	}
	entry.SurfaceVersion = 2
	entry.ContentKey.SurfaceVersion = 2

	perftrace.Enable()
	perftrace.Reset()
	defer perftrace.Disable()

	nextSprite := cache.contentSprite(entry, runtimeState)
	if nextSprite != sprite {
		t.Fatal("expected incremental path to reuse sprite")
	}
	cached := cache.contentSprites[entry.PaneID]
	if cached == nil {
		t.Fatal("expected cached sprite entry")
	}
	if got := cached.delta.changedRows; len(got) != 1 || got[0] != 1 {
		t.Fatalf("expected explicit changed row hint to limit redraw to row 1, got %#v", got)
	}
	snapshot := perftrace.SnapshotCurrent()
	if event, ok := snapshot.Event("render.pane_content_sprite.incremental.explicit_changed_rows_hit"); !ok || event.Count == 0 {
		t.Fatalf("expected explicit changed rows hint hit, got events=%#v", snapshot.Events)
	}
	if got := nextSprite.rawString(); got != "row00\nrowXX\nrow02" {
		t.Fatalf("expected explicit changed rows hint to preserve untouched rows, got %q", got)
	}

	fullCache := &bodyRenderCache{}
	fullSprite := fullCache.contentSprite(entry, runtimeState)
	if fullSprite == nil {
		t.Fatal("expected full redraw sprite")
	}
	if got, want := nextSprite.rawString(), fullSprite.rawString(); got != want {
		t.Fatalf("expected explicit changed rows hint to match full redraw, got %q want %q", got, want)
	}
}

func TestContentSpriteIncrementalRowUpdatePreservesExtentHints(t *testing.T) {
	now := time.Date(2026, 4, 16, 13, 0, 0, 0, time.UTC)
	surface := &spriteTestSurface{
		size: protocol.Size{Cols: 4, Rows: 2},
		screen: [][]protocol.Cell{
			protocolRowFromText("ab"),
			protocolRowFromText("cd"),
		},
		screenTimestamps: []time.Time{now, now},
	}
	runtimeState := &VisibleRuntimeStateProxy{
		Terminals: []runtimestate.VisibleTerminal{{
			TerminalID:     "term-1",
			Name:           "shell",
			State:          "running",
			Surface:        surface,
			SurfaceVersion: 1,
		}},
	}
	entry := paneRenderEntry{
		PaneID:     "pane-1",
		Rect:       workbench.Rect{X: 0, Y: 0, W: 6, H: 2},
		Frameless:  true,
		TerminalID: "term-1",
		Theme:      defaultUITheme(),
		ContentKey: paneContentKey{
			TerminalID:    "term-1",
			TerminalKnown: true,
		},
	}
	entry.ContentKey.SurfaceVersion = runtimeState.Terminals[0].SurfaceVersion

	cache := &bodyRenderCache{}
	sprite := cache.contentSprite(entry, runtimeState)
	if sprite == nil {
		t.Fatal("expected initial sprite")
	}
	if got := sprite.rawString(); got != "ab····\ncd····" {
		t.Fatalf("expected initial extent hints, got %q", got)
	}

	surface.screen[1] = protocolRowFromText("xy")
	surface.screenTimestamps[1] = now.Add(time.Second)
	runtimeState.Terminals[0].SurfaceVersion = 2
	entry.ContentKey.SurfaceVersion = runtimeState.Terminals[0].SurfaceVersion

	nextSprite := cache.contentSprite(entry, runtimeState)
	if nextSprite != sprite {
		t.Fatal("expected incremental path to reuse sprite")
	}
	if got := nextSprite.rawString(); got != "ab····\nxy····" {
		t.Fatalf("expected incremental row redraw to preserve extent hints, got %q", got)
	}
}

func TestContentSpriteReusesSpriteForScrollOffsetShift(t *testing.T) {
	now := time.Date(2026, 4, 17, 10, 0, 0, 0, time.UTC)
	surface := &spriteTestSurface{
		size: protocol.Size{Cols: 5, Rows: 2},
		scrollback: [][]protocol.Cell{
			protocolRowFromText("hist1"),
			protocolRowFromText("hist2"),
		},
		screen: [][]protocol.Cell{
			protocolRowFromText("live1"),
			protocolRowFromText("live2"),
		},
		scrollTimestamps: []time.Time{now, now},
		screenTimestamps: []time.Time{now, now},
	}
	runtimeState := &VisibleRuntimeStateProxy{
		Terminals: []runtimestate.VisibleTerminal{{
			TerminalID:     "term-1",
			Name:           "shell",
			State:          "running",
			Surface:        surface,
			SurfaceVersion: 1,
		}},
	}
	entry := paneRenderEntry{
		PaneID:     "pane-1",
		Rect:       workbench.Rect{X: 0, Y: 0, W: 5, H: 2},
		Frameless:  true,
		TerminalID: "term-1",
		Theme:      defaultUITheme(),
		ContentKey: paneContentKey{
			TerminalID:    "term-1",
			TerminalKnown: true,
			ScrollOffset:  0,
		},
		ScrollOffset: 0,
	}

	cache := &bodyRenderCache{}
	sprite := cache.contentSprite(entry, runtimeState)
	if sprite == nil {
		t.Fatal("expected initial sprite")
	}
	if got := sprite.rawString(); got != "live1\nlive2" {
		t.Fatalf("expected initial visible rows, got %q", got)
	}

	entry.ContentKey.ScrollOffset = 1
	entry.ScrollOffset = 1

	perftrace.Enable()
	perftrace.Reset()
	defer perftrace.Disable()
	nextSprite := cache.contentSprite(entry, runtimeState)
	snapshot := perftrace.SnapshotCurrent()
	event, ok := snapshot.Event("render.pane_content_sprite.incremental")
	if !ok || event.Count == 0 {
		t.Fatalf("expected scroll offset change to use incremental sprite update, got events=%#v", snapshot.Events)
	}
	if nextSprite != sprite {
		t.Fatal("expected scroll offset change to reuse sprite canvas")
	}
	if got := nextSprite.rawString(); got != "hist2\nlive1" {
		t.Fatalf("expected scroll offset shift to reuse moved rows and redraw edge row, got %q", got)
	}
}

func TestContentSpriteReusesSpriteForPhysicalScreenScroll(t *testing.T) {
	now := time.Date(2026, 4, 18, 9, 0, 0, 0, time.UTC)
	surface := &spriteTestSurface{
		size: protocol.Size{Cols: 5, Rows: 4},
		screen: [][]protocol.Cell{
			protocolRowFromText("row01"),
			protocolRowFromText("row02"),
			protocolRowFromText("row03"),
			protocolRowFromText("row04"),
		},
		screenTimestamps: []time.Time{
			now,
			now.Add(1 * time.Second),
			now.Add(2 * time.Second),
			now.Add(3 * time.Second),
		},
	}
	runtimeState := &VisibleRuntimeStateProxy{
		Terminals: []runtimestate.VisibleTerminal{{
			TerminalID:     "term-1",
			Name:           "shell",
			State:          "running",
			Surface:        surface,
			SurfaceVersion: 1,
		}},
	}
	entry := paneRenderEntry{
		PaneID:     "pane-1",
		Rect:       workbench.Rect{X: 0, Y: 0, W: 5, H: 4},
		Frameless:  true,
		TerminalID: "term-1",
		Theme:      defaultUITheme(),
		ContentKey: paneContentKey{
			TerminalID:    "term-1",
			TerminalKnown: true,
		},
	}
	entry.ContentKey.SurfaceVersion = runtimeState.Terminals[0].SurfaceVersion

	cache := &bodyRenderCache{}
	sprite := cache.contentSprite(entry, runtimeState)
	if sprite == nil {
		t.Fatal("expected initial sprite")
	}

	surface.screen = [][]protocol.Cell{
		protocolRowFromText("row02"),
		protocolRowFromText("row03"),
		protocolRowFromText("row04"),
		protocolRowFromText("row05"),
	}
	surface.screenTimestamps = []time.Time{
		now.Add(1 * time.Second),
		now.Add(2 * time.Second),
		now.Add(3 * time.Second),
		now.Add(4 * time.Second),
	}
	runtimeState.Terminals[0].SurfaceVersion = 2
	entry.ContentKey.SurfaceVersion = 2

	perftrace.Enable()
	perftrace.Reset()
	defer perftrace.Disable()
	nextSprite := cache.contentSprite(entry, runtimeState)
	snapshot := perftrace.SnapshotCurrent()
	event, ok := snapshot.Event("render.pane_content_sprite.incremental.scroll_hit")
	if !ok || event.Count == 0 {
		t.Fatalf("expected physical screen scroll to use scroll-hit incremental path, got events=%#v", snapshot.Events)
	}
	if nextSprite != sprite {
		t.Fatal("expected physical screen scroll to reuse sprite canvas")
	}
	if got := nextSprite.rawString(); got != "row02\nrow03\nrow04\nrow05" {
		t.Fatalf("expected screen scroll to shift visible rows and redraw edge row, got %q", got)
	}
}

func TestContentSpriteReusesSpriteForPartialScrollBand(t *testing.T) {
	now := time.Date(2026, 4, 18, 10, 0, 0, 0, time.UTC)
	surface := &spriteTestSurface{
		size: protocol.Size{Cols: 3, Rows: 8},
		screen: [][]protocol.Cell{
			protocolRowFromText("hdr"),
			protocolRowFromText("aaa"),
			protocolRowFromText("bbb"),
			protocolRowFromText("ccc"),
			protocolRowFromText("ddd"),
			protocolRowFromText("eee"),
			protocolRowFromText("fff"),
			protocolRowFromText("ftr"),
		},
		screenTimestamps: []time.Time{
			now,
			now.Add(1 * time.Second),
			now.Add(2 * time.Second),
			now.Add(3 * time.Second),
			now.Add(4 * time.Second),
			now.Add(5 * time.Second),
			now.Add(6 * time.Second),
			now.Add(7 * time.Second),
		},
	}
	runtimeState := &VisibleRuntimeStateProxy{
		Terminals: []runtimestate.VisibleTerminal{{
			TerminalID:     "term-1",
			Name:           "shell",
			State:          "running",
			Surface:        surface,
			SurfaceVersion: 1,
		}},
	}
	entry := paneRenderEntry{
		PaneID:     "pane-1",
		Rect:       workbench.Rect{X: 0, Y: 0, W: 3, H: 8},
		Frameless:  true,
		TerminalID: "term-1",
		Theme:      defaultUITheme(),
		ContentKey: paneContentKey{
			TerminalID:    "term-1",
			TerminalKnown: true,
		},
	}
	entry.ContentKey.SurfaceVersion = 1

	cache := &bodyRenderCache{}
	sprite := cache.contentSprite(entry, runtimeState)
	if sprite == nil {
		t.Fatal("expected initial sprite")
	}
	previousWindow := cache.contentSprites[entry.PaneID].window

	surface.screen = [][]protocol.Cell{
		protocolRowFromText("hdr"),
		protocolRowFromText("bbb"),
		protocolRowFromText("ccc"),
		protocolRowFromText("ddd"),
		protocolRowFromText("eee"),
		protocolRowFromText("fff"),
		protocolRowFromText("ggg"),
		protocolRowFromText("ftr"),
	}
	surface.screenTimestamps = []time.Time{
		now,
		now.Add(2 * time.Second),
		now.Add(3 * time.Second),
		now.Add(4 * time.Second),
		now.Add(5 * time.Second),
		now.Add(6 * time.Second),
		now.Add(8 * time.Second),
		now.Add(7 * time.Second),
	}
	runtimeState.Terminals[0].SurfaceVersion = 2
	entry.ContentKey.SurfaceVersion = 2

	resolved := resolvePaneContent(entry, runtimeState, true)
	nextWindow := buildTerminalSourceWindowState(resolved.source, resolved.contentRect.H, resolved.renderOffset)
	baseChangedRows := terminalWindowChangedRows(previousWindow, nextWindow, terminalWindowScrollPlan{})
	if plan, ok := detectTerminalWindowPartialScroll(previousWindow, nextWindow, len(baseChangedRows)); !ok {
		t.Fatalf("expected planner to find partial scroll plan, baseChanged=%#v prevIdentity=%#v nextIdentity=%#v prevExact=%#v nextExact=%#v", baseChangedRows, previousWindow.rowIdentityHashes, nextWindow.rowIdentityHashes, previousWindow.exactRowHashes, nextWindow.exactRowHashes)
	} else if plan.direction != terminalWindowScrollUp {
		t.Fatalf("expected planner to choose scroll up, got %#v", plan)
	}

	perftrace.Enable()
	perftrace.Reset()
	defer perftrace.Disable()

	nextSprite := cache.contentSprite(entry, runtimeState)
	if nextSprite != sprite {
		t.Fatal("expected partial scroll band update to reuse the sprite canvas")
	}
	cached := cache.contentSprites[entry.PaneID]
	if cached == nil {
		t.Fatal("expected cached sprite entry")
	}
	if cached.delta.scrollPlan.direction != terminalWindowScrollUp {
		t.Fatalf("expected partial scroll band to scroll up, got %#v", cached.delta.scrollPlan)
	}
	if cached.delta.scrollPlan.wholeWindow(8) {
		t.Fatalf("expected partial band instead of whole-window scroll, got %#v", cached.delta.scrollPlan)
	}
	if cached.delta.scrollPlan.start != 1 || cached.delta.scrollPlan.end != 6 || cached.delta.scrollPlan.shift != 1 || cached.delta.scrollPlan.reused != 5 {
		t.Fatalf("unexpected partial scroll plan: %#v", cached.delta.scrollPlan)
	}
	if got := cached.delta.changedRows; len(got) != 1 || got[0] != 6 {
		t.Fatalf("expected only the inserted gap row to redraw, got %#v", got)
	}
	snapshot := perftrace.SnapshotCurrent()
	if event, ok := snapshot.Event("render.pane_content_sprite.incremental.partial_scroll_hit"); !ok || event.Count == 0 {
		t.Fatalf("expected partial scroll perf hit, got events=%#v", snapshot.Events)
	}
	if got := nextSprite.rawString(); got != "hdr\nbbb\nccc\nddd\neee\nfff\nggg\nftr" {
		t.Fatalf("unexpected partial-scroll sprite content: %q", got)
	}

	fullCache := &bodyRenderCache{}
	fullSprite := fullCache.contentSprite(entry, runtimeState)
	if fullSprite == nil {
		t.Fatal("expected full redraw sprite")
	}
	if got, want := nextSprite.rawString(), fullSprite.rawString(); got != want {
		t.Fatalf("expected partial-scroll sprite to match full redraw, got %q want %q", got, want)
	}
}

func TestContentSpritePartialScrollBandRedrawsReusedRowsWhenExactHashChanges(t *testing.T) {
	now := time.Date(2026, 4, 18, 10, 15, 0, 0, time.UTC)
	surface := &spriteTestSurface{
		size: protocol.Size{Cols: 3, Rows: 8},
		screen: [][]protocol.Cell{
			protocolRowFromText("hdr"),
			protocolRowFromText("a00"),
			protocolRowFromText("b00"),
			protocolRowFromText("c00"),
			protocolRowFromText("d00"),
			protocolRowFromText("e00"),
			protocolRowFromText("f00"),
			protocolRowFromText("ftr"),
		},
		screenTimestamps: []time.Time{
			now,
			now.Add(1 * time.Second),
			now.Add(2 * time.Second),
			now.Add(3 * time.Second),
			now.Add(4 * time.Second),
			now.Add(5 * time.Second),
			now.Add(6 * time.Second),
			now.Add(7 * time.Second),
		},
	}
	runtimeState := &VisibleRuntimeStateProxy{
		Terminals: []runtimestate.VisibleTerminal{{
			TerminalID:     "term-1",
			Name:           "shell",
			State:          "running",
			Surface:        surface,
			SurfaceVersion: 1,
		}},
	}
	entry := paneRenderEntry{
		PaneID:     "pane-1",
		Rect:       workbench.Rect{X: 0, Y: 0, W: 3, H: 8},
		Frameless:  true,
		TerminalID: "term-1",
		Theme:      defaultUITheme(),
		ContentKey: paneContentKey{
			TerminalID:    "term-1",
			TerminalKnown: true,
		},
	}
	entry.ContentKey.SurfaceVersion = 1

	cache := &bodyRenderCache{}
	sprite := cache.contentSprite(entry, runtimeState)
	if sprite == nil {
		t.Fatal("expected initial sprite")
	}
	previousWindow := cache.contentSprites[entry.PaneID].window

	surface.screen = [][]protocol.Cell{
		protocolRowFromText("hdr"),
		protocolRowFromText("b01"),
		protocolRowFromText("c00"),
		protocolRowFromText("d00"),
		protocolRowFromText("e00"),
		protocolRowFromText("f00"),
		protocolRowFromText("g00"),
		protocolRowFromText("ftr"),
	}
	surface.screenTimestamps = []time.Time{
		now,
		now.Add(2 * time.Second),
		now.Add(3 * time.Second),
		now.Add(4 * time.Second),
		now.Add(5 * time.Second),
		now.Add(6 * time.Second),
		now.Add(8 * time.Second),
		now.Add(7 * time.Second),
	}
	runtimeState.Terminals[0].SurfaceVersion = 2
	entry.ContentKey.SurfaceVersion = 2

	resolved := resolvePaneContent(entry, runtimeState, true)
	nextWindow := buildTerminalSourceWindowState(resolved.source, resolved.contentRect.H, resolved.renderOffset)
	baseChangedRows := terminalWindowChangedRows(previousWindow, nextWindow, terminalWindowScrollPlan{})
	if plan, ok := detectTerminalWindowPartialScroll(previousWindow, nextWindow, len(baseChangedRows)); !ok {
		t.Fatalf("expected planner to find metadata-driven partial scroll plan, baseChanged=%#v prevIdentity=%#v nextIdentity=%#v prevExact=%#v nextExact=%#v", baseChangedRows, previousWindow.rowIdentityHashes, nextWindow.rowIdentityHashes, previousWindow.exactRowHashes, nextWindow.exactRowHashes)
	} else if plan.direction != terminalWindowScrollUp {
		t.Fatalf("expected planner to choose metadata-driven scroll up, got %#v", plan)
	}

	perftrace.Enable()
	perftrace.Reset()
	defer perftrace.Disable()

	nextSprite := cache.contentSprite(entry, runtimeState)
	if nextSprite != sprite {
		t.Fatal("expected partial scroll band update to reuse the sprite canvas")
	}
	cached := cache.contentSprites[entry.PaneID]
	if cached == nil {
		t.Fatal("expected cached sprite entry")
	}
	if cached.delta.scrollPlan.direction != terminalWindowScrollUp || cached.delta.scrollPlan.wholeWindow(8) {
		t.Fatalf("expected metadata-driven partial scroll plan, got %#v", cached.delta.scrollPlan)
	}
	if got := cached.delta.changedRows; len(got) != 2 || got[0] != 1 || got[1] != 6 {
		t.Fatalf("expected reused row with exact hash change plus gap row to redraw, got %#v", got)
	}
	snapshot := perftrace.SnapshotCurrent()
	if event, ok := snapshot.Event("render.pane_content_sprite.incremental.partial_scroll_hit"); !ok || event.Count == 0 {
		t.Fatalf("expected partial scroll perf hit, got events=%#v", snapshot.Events)
	}
	if got := nextSprite.rawString(); got != "hdr\nb01\nc00\nd00\ne00\nf00\ng00\nftr" {
		t.Fatalf("unexpected partial-scroll sprite content: %q", got)
	}

	fullCache := &bodyRenderCache{}
	fullSprite := fullCache.contentSprite(entry, runtimeState)
	if fullSprite == nil {
		t.Fatal("expected full redraw sprite")
	}
	if got, want := nextSprite.rawString(), fullSprite.rawString(); got != want {
		t.Fatalf("expected metadata-driven partial scroll sprite to match full redraw, got %q want %q", got, want)
	}
}

func TestApplySpriteDeltaToCanvasReusesRowsForScrollOffsetShift(t *testing.T) {
	now := time.Date(2026, 4, 17, 11, 0, 0, 0, time.UTC)
	surface := &spriteTestSurface{
		size: protocol.Size{Cols: 5, Rows: 2},
		scrollback: [][]protocol.Cell{
			protocolRowFromText("hist1"),
			protocolRowFromText("hist2"),
		},
		screen: [][]protocol.Cell{
			protocolRowFromText("live1"),
			protocolRowFromText("live2"),
		},
		scrollTimestamps: []time.Time{now, now},
		screenTimestamps: []time.Time{now, now},
	}
	runtimeState := &VisibleRuntimeStateProxy{
		Terminals: []runtimestate.VisibleTerminal{{
			TerminalID:     "term-1",
			Name:           "shell",
			State:          "running",
			Surface:        surface,
			SurfaceVersion: 1,
		}},
	}
	entry := paneRenderEntry{
		PaneID:     "pane-1",
		Rect:       workbench.Rect{X: 0, Y: 0, W: 7, H: 4},
		TerminalID: "term-1",
		Theme:      defaultUITheme(),
		ContentKey: paneContentKey{
			TerminalID:    "term-1",
			TerminalKnown: true,
			ScrollOffset:  0,
			State:         "running",
		},
		ScrollOffset: 0,
	}

	cache := &bodyRenderCache{}
	body := newComposedCanvas(7, 4)
	sprite := cache.contentSprite(entry, runtimeState)
	if sprite == nil {
		t.Fatal("expected initial sprite")
	}
	body.blit(sprite, 1, 1)
	if got := body.rawString(); got != "\n live\n live\n" {
		t.Fatalf("expected initial body content, got %q", got)
	}

	entry.ContentKey.ScrollOffset = 1
	entry.ScrollOffset = 1
	_ = cache.contentSprite(entry, runtimeState)
	if !cache.applySpriteDeltaToCanvas(body, entry) {
		t.Fatal("expected body canvas to accept sprite delta")
	}
	if got := body.rawString(); got != "\n hist\n live\n" {
		t.Fatalf("expected body canvas to shift pane rows locally, got %q", got)
	}
}

func TestApplySpriteDeltaToCanvasMatchesFullBlitForScrollUp(t *testing.T) {
	now := time.Date(2026, 4, 17, 11, 30, 0, 0, time.UTC)
	surface := &spriteTestSurface{
		size: protocol.Size{Cols: 5, Rows: 2},
		scrollback: [][]protocol.Cell{
			protocolRowFromText("hist1"),
			protocolRowFromText("hist2"),
		},
		screen: [][]protocol.Cell{
			protocolRowFromText("live1"),
			protocolRowFromText("live2"),
		},
		scrollTimestamps: []time.Time{now, now},
		screenTimestamps: []time.Time{now, now},
	}
	runtimeState := &VisibleRuntimeStateProxy{
		Terminals: []runtimestate.VisibleTerminal{{
			TerminalID:     "term-1",
			Name:           "shell",
			State:          "running",
			Surface:        surface,
			SurfaceVersion: 1,
		}},
	}
	entry := paneRenderEntry{
		PaneID:     "pane-1",
		Rect:       workbench.Rect{X: 0, Y: 0, W: 7, H: 4},
		TerminalID: "term-1",
		Theme:      defaultUITheme(),
		ContentKey: paneContentKey{
			TerminalID:    "term-1",
			TerminalKnown: true,
			ScrollOffset:  1,
			State:         "running",
		},
		ScrollOffset: 1,
	}

	cache := &bodyRenderCache{}
	assertBodyDeltaMatchesFullBlit(t, cache, entry, runtimeState, func(next *paneRenderEntry) {
		next.ContentKey.ScrollOffset = 0
		next.ScrollOffset = 0
	})
}

func TestApplySpriteDeltaToCanvasMatchesFullBlitForScrollDown(t *testing.T) {
	now := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)
	surface := &spriteTestSurface{
		size: protocol.Size{Cols: 5, Rows: 2},
		scrollback: [][]protocol.Cell{
			protocolRowFromText("hist1"),
			protocolRowFromText("hist2"),
		},
		screen: [][]protocol.Cell{
			protocolRowFromText("live1"),
			protocolRowFromText("live2"),
		},
		scrollTimestamps: []time.Time{now, now},
		screenTimestamps: []time.Time{now, now},
	}
	runtimeState := &VisibleRuntimeStateProxy{
		Terminals: []runtimestate.VisibleTerminal{{
			TerminalID:     "term-1",
			Name:           "shell",
			State:          "running",
			Surface:        surface,
			SurfaceVersion: 1,
		}},
	}
	entry := paneRenderEntry{
		PaneID:     "pane-1",
		Rect:       workbench.Rect{X: 0, Y: 0, W: 7, H: 4},
		TerminalID: "term-1",
		Theme:      defaultUITheme(),
		ContentKey: paneContentKey{
			TerminalID:    "term-1",
			TerminalKnown: true,
			ScrollOffset:  0,
			State:         "running",
		},
		ScrollOffset: 0,
	}

	cache := &bodyRenderCache{}
	assertBodyDeltaMatchesFullBlit(t, cache, entry, runtimeState, func(next *paneRenderEntry) {
		next.ContentKey.ScrollOffset = 1
		next.ScrollOffset = 1
	})
}

func TestApplySpriteDeltaToCanvasMatchesFullBlitForChangedRows(t *testing.T) {
	now := time.Date(2026, 4, 17, 12, 30, 0, 0, time.UTC)
	surface := &spriteTestSurface{
		size: protocol.Size{Cols: 5, Rows: 2},
		screen: [][]protocol.Cell{
			protocolRowFromText("hello"),
			protocolRowFromText("world"),
		},
		screenTimestamps: []time.Time{now, now},
	}
	runtimeState := &VisibleRuntimeStateProxy{
		Terminals: []runtimestate.VisibleTerminal{{
			TerminalID:     "term-1",
			Name:           "shell",
			State:          "running",
			Surface:        surface,
			SurfaceVersion: 1,
		}},
	}
	entry := paneRenderEntry{
		PaneID:     "pane-1",
		Rect:       workbench.Rect{X: 0, Y: 0, W: 7, H: 4},
		TerminalID: "term-1",
		Theme:      defaultUITheme(),
		ContentKey: paneContentKey{
			TerminalID:     "term-1",
			TerminalKnown:  true,
			SurfaceVersion: 1,
			State:          "running",
		},
	}

	cache := &bodyRenderCache{}
	assertBodyDeltaMatchesFullBlit(t, cache, entry, runtimeState, func(next *paneRenderEntry) {
		surface.screen[1] = protocolRowFromText("there")
		surface.screenTimestamps[1] = now.Add(time.Second)
		runtimeState.Terminals[0].SurfaceVersion = 2
		next.ContentKey.SurfaceVersion = 2
	})
}

func TestApplySpriteDeltaToCanvasMatchesFullBlitForPartialScrollBand(t *testing.T) {
	now := time.Date(2026, 4, 18, 10, 30, 0, 0, time.UTC)
	surface := &spriteTestSurface{
		size: protocol.Size{Cols: 3, Rows: 8},
		screen: [][]protocol.Cell{
			protocolRowFromText("hdr"),
			protocolRowFromText("aaa"),
			protocolRowFromText("bbb"),
			protocolRowFromText("ccc"),
			protocolRowFromText("ddd"),
			protocolRowFromText("eee"),
			protocolRowFromText("fff"),
			protocolRowFromText("ftr"),
		},
		screenTimestamps: []time.Time{
			now,
			now.Add(1 * time.Second),
			now.Add(2 * time.Second),
			now.Add(3 * time.Second),
			now.Add(4 * time.Second),
			now.Add(5 * time.Second),
			now.Add(6 * time.Second),
			now.Add(7 * time.Second),
		},
	}
	runtimeState := &VisibleRuntimeStateProxy{
		Terminals: []runtimestate.VisibleTerminal{{
			TerminalID:     "term-1",
			Name:           "shell",
			State:          "running",
			Surface:        surface,
			SurfaceVersion: 1,
		}},
	}
	entry := paneRenderEntry{
		PaneID:     "pane-1",
		Rect:       workbench.Rect{X: 0, Y: 0, W: 5, H: 10},
		TerminalID: "term-1",
		Theme:      defaultUITheme(),
		ContentKey: paneContentKey{
			TerminalID:    "term-1",
			TerminalKnown: true,
			State:         "running",
		},
	}
	entry.ContentKey.SurfaceVersion = 1

	cache := &bodyRenderCache{}
	sprite := cache.contentSprite(entry, runtimeState)
	if sprite == nil {
		t.Fatal("expected initial sprite")
	}
	body := newBodyCanvasFromSprite(entry, sprite)

	surface.screen = [][]protocol.Cell{
		protocolRowFromText("hdr"),
		protocolRowFromText("bbb"),
		protocolRowFromText("ccc"),
		protocolRowFromText("ddd"),
		protocolRowFromText("eee"),
		protocolRowFromText("fff"),
		protocolRowFromText("ggg"),
		protocolRowFromText("ftr"),
	}
	surface.screenTimestamps = []time.Time{
		now,
		now.Add(2 * time.Second),
		now.Add(3 * time.Second),
		now.Add(4 * time.Second),
		now.Add(5 * time.Second),
		now.Add(6 * time.Second),
		now.Add(8 * time.Second),
		now.Add(7 * time.Second),
	}
	runtimeState.Terminals[0].SurfaceVersion = 2
	entry.ContentKey.SurfaceVersion = 2

	nextSprite := cache.contentSprite(entry, runtimeState)
	if nextSprite == nil {
		t.Fatal("expected updated sprite")
	}
	if !cache.applySpriteDeltaToCanvas(body, entry) {
		t.Fatal("expected body canvas to accept partial scroll delta")
	}

	want := newBodyCanvasFromSprite(entry, nextSprite)
	if got := body.rawString(); got != want.rawString() {
		t.Fatalf("expected partial scroll delta apply to match full blit, got %q want %q", got, want.rawString())
	}
}

func assertBodyDeltaMatchesFullBlit(t *testing.T, cache *bodyRenderCache, entry paneRenderEntry, runtimeState *VisibleRuntimeStateProxy, mutate func(next *paneRenderEntry)) {
	t.Helper()

	sprite := cache.contentSprite(entry, runtimeState)
	if sprite == nil {
		t.Fatal("expected initial sprite")
	}
	body := newBodyCanvasFromSprite(entry, sprite)

	next := entry
	mutate(&next)
	nextSprite := cache.contentSprite(next, runtimeState)
	if nextSprite == nil {
		t.Fatal("expected updated sprite")
	}
	if !cache.applySpriteDeltaToCanvas(body, next) {
		t.Fatal("expected body canvas to accept sprite delta")
	}

	want := newBodyCanvasFromSprite(next, nextSprite)
	if got := body.rawString(); got != want.rawString() {
		t.Fatalf("expected body delta apply to match full blit, got %q want %q", got, want.rawString())
	}
}

func newBodyCanvasFromSprite(entry paneRenderEntry, sprite *composedCanvas) *composedCanvas {
	body := newComposedCanvas(entry.Rect.W, entry.Rect.H)
	interior := interiorRectForEntry(entry)
	if interior.W > 0 && interior.H > 0 {
		fillRect(body, interior, blankDrawCell())
	}
	body.blit(sprite, interior.X, interior.Y)
	return body
}
