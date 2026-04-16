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

func TestBuildPaneRenderEntryContentVersionIgnoresCursorOnlySurfaceChanges(t *testing.T) {
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
	surface.cursor = protocol.CursorState{Row: 1, Col: 2, Visible: true}
	lookup.terminals["term-1"].SurfaceVersion = 2
	entryCursorOnly := buildPaneRenderEntry(pane, pane.Rect, pane.Rect, true, pane.ID, 0, lookup, bodyProjectionOptions{}, defaultUITheme())
	if entryBefore.ContentKey.SurfaceVersion != entryCursorOnly.ContentKey.SurfaceVersion {
		t.Fatalf("expected cursor-only surface changes to keep content version stable, got before=%d after=%d", entryBefore.ContentKey.SurfaceVersion, entryCursorOnly.ContentKey.SurfaceVersion)
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
	entry.ContentKey.SurfaceVersion = terminalSourceWindowSignature(renderSource(nil, surface), 2, 0)

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
	entry.ContentKey.SurfaceVersion = terminalSourceWindowSignature(renderSource(nil, surface), 2, 0)

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
