package vterm

import (
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/protocol"
)

func TestLoadSnapshotRestoresScreenAndCursor(t *testing.T) {
	vt := New(10, 4, 100, nil)
	vt.LoadSnapshot(ScreenData{
		Cells: [][]Cell{
			{
				{Content: "h", Width: 1},
				{Content: "i", Width: 1},
			},
		},
	}, CursorState{Row: 0, Col: 2, Visible: true}, TerminalModes{AutoWrap: true})

	screen := vt.ScreenContent()
	if got := screen.Cells[0][0].Content + screen.Cells[0][1].Content; got != "hi" {
		t.Fatalf("expected restored content %q, got %q", "hi", got)
	}
	cursor := vt.CursorState()
	if cursor.Col != 2 || cursor.Row != 0 {
		t.Fatalf("expected restored cursor at (2,0), got (%d,%d)", cursor.Col, cursor.Row)
	}

	if _, err := vt.Write([]byte("!")); err != nil {
		t.Fatalf("write after snapshot failed: %v", err)
	}
	screen = vt.ScreenContent()
	if got := screen.Cells[0][0].Content + screen.Cells[0][1].Content + screen.Cells[0][2].Content; got != "hi!" {
		t.Fatalf("expected continued output %q, got %q", "hi!", got)
	}
}

func TestLoadSnapshotWithScrollbackRestoresHistory(t *testing.T) {
	vt := New(6, 3, 100, nil)
	vt.LoadSnapshotWithScrollback([][]Cell{
		{{Content: "o", Width: 1}, {Content: "l", Width: 1}, {Content: "d", Width: 1}},
	}, ScreenData{
		Cells: [][]Cell{
			{
				{Content: "n", Width: 1},
				{Content: "e", Width: 1},
				{Content: "w", Width: 1},
			},
		},
	}, CursorState{Row: 0, Col: 3, Visible: true}, TerminalModes{AutoWrap: true})

	scrollback := vt.ScrollbackContent()
	if len(scrollback) != 1 {
		t.Fatalf("expected 1 restored scrollback row, got %d", len(scrollback))
	}
	if got := scrollback[0][0].Content + scrollback[0][1].Content + scrollback[0][2].Content; got != "old" {
		t.Fatalf("expected restored scrollback %q, got %q", "old", got)
	}
}

func TestLoadSnapshotPreservesWideCellContinuationsAcrossSubsequentWrites(t *testing.T) {
	vt := New(8, 2, 100, nil)
	vt.LoadSnapshot(ScreenData{
		Cells: [][]Cell{
			{
				{Content: "你", Width: 2},
				{Content: "", Width: 0},
				{Content: "好", Width: 2},
				{Content: "", Width: 0},
				{Content: "A", Width: 1},
			},
		},
	}, CursorState{Row: 0, Col: 5, Visible: true}, TerminalModes{AutoWrap: true})

	screen := vt.ScreenContent()
	if got := screen.Cells[0][0]; got.Content != "你" || got.Width != 2 {
		t.Fatalf("expected first wide cell restored, got %#v", got)
	}
	if got := screen.Cells[0][1]; got.Content != "" || got.Width != 0 {
		t.Fatalf("expected wide-cell continuation placeholder at x=1, got %#v", got)
	}
	if got := screen.Cells[0][2]; got.Content != "好" || got.Width != 2 {
		t.Fatalf("expected second wide cell restored, got %#v", got)
	}
	if got := screen.Cells[0][3]; got.Content != "" || got.Width != 0 {
		t.Fatalf("expected wide-cell continuation placeholder at x=3, got %#v", got)
	}

	if _, err := vt.Write([]byte("!")); err != nil {
		t.Fatalf("write after wide-cell snapshot failed: %v", err)
	}

	screen = vt.ScreenContent()
	if got := screen.Cells[0][0]; got.Content != "你" || got.Width != 2 {
		t.Fatalf("expected first wide cell preserved after write, got %#v", got)
	}
	if got := screen.Cells[0][1]; got.Content != "" || got.Width != 0 {
		t.Fatalf("expected continuation placeholder preserved at x=1 after write, got %#v", got)
	}
	if got := screen.Cells[0][2]; got.Content != "好" || got.Width != 2 {
		t.Fatalf("expected second wide cell preserved after write, got %#v", got)
	}
	if got := screen.Cells[0][3]; got.Content != "" || got.Width != 0 {
		t.Fatalf("expected continuation placeholder preserved at x=3 after write, got %#v", got)
	}
	if got := screen.Cells[0][5]; got.Content != "!" || got.Width != 1 {
		t.Fatalf("expected trailing ASCII write after restored wide cells, got %#v", got)
	}
}

func TestVTermResizeRoundTripPreservesBackgroundStyleAcrossExpandedTail(t *testing.T) {
	vt := New(8, 3, 100, nil)
	bg := CellStyle{BG: "#444444"}
	screen := make([][]Cell, 3)
	for y := range screen {
		screen[y] = make([]Cell, 8)
		for x := range screen[y] {
			screen[y][x] = Cell{Content: " ", Width: 1, Style: bg}
		}
		screen[y][0].Content = "~"
	}
	vt.LoadSnapshot(ScreenData{Cells: screen}, CursorState{Row: 0, Col: 0, Visible: true}, TerminalModes{AlternateScreen: true, MouseTracking: true})

	vt.Resize(4, 2)
	vt.Resize(8, 3)

	restored := vt.ScreenContent()
	if len(restored.Cells) < 2 || len(restored.Cells[1]) < 8 {
		t.Fatalf("unexpected restored screen dimensions: %#v", restored.Cells)
	}
	if got := restored.Cells[1][6].Style.BG; got == "" {
		t.Fatalf("expected expanded tail cell to retain background style, got %#v", restored.Cells[1][6])
	}
}

func TestVTermResizeRoundTripUsesNearestTrailingBackgroundWhenEdgeCellHasNoBG(t *testing.T) {
	vt := New(4, 2, 100, nil)
	bg := CellStyle{BG: "#444444"}
	screen := [][]Cell{
		{
			{Content: "f", Width: 1, Style: bg},
			{Content: "o", Width: 1, Style: bg},
			{Content: "o", Width: 1, Style: bg},
			{Content: " ", Width: 1},
		},
		{
			{Content: " ", Width: 1},
			{Content: " ", Width: 1},
			{Content: " ", Width: 1},
			{Content: " ", Width: 1},
		},
	}
	vt.LoadSnapshot(ScreenData{Cells: screen}, CursorState{Row: 0, Col: 0, Visible: true}, TerminalModes{AlternateScreen: true, MouseTracking: true})

	vt.Resize(8, 2)

	restored := vt.ScreenContent()
	if len(restored.Cells) == 0 || len(restored.Cells[0]) < 8 {
		t.Fatalf("unexpected restored screen dimensions: %#v", restored.Cells)
	}
	if got := restored.Cells[0][6].Style.BG; got != bg.BG {
		t.Fatalf("expected expanded tail to use nearest trailing background %q, got %#v", bg.BG, restored.Cells[0][6])
	}
	if got := restored.Cells[1][6].Style.BG; got != "" {
		t.Fatalf("expected row without any trailing background to stay unfilled, got %#v", restored.Cells[1][6])
	}
}

func TestVTermWidthGrowFillPersistsThroughPartialWrite(t *testing.T) {
	const termBG = "#1e1e2e"
	vt := New(4, 2, 100, nil)
	bg := CellStyle{BG: termBG}
	screen := make([][]Cell, 2)
	for y := range screen {
		screen[y] = make([]Cell, 4)
		for x := range screen[y] {
			screen[y][x] = Cell{Content: " ", Width: 1, Style: bg}
		}
	}
	vt.LoadSnapshot(ScreenData{Cells: screen}, CursorState{Row: 0, Col: 0, Visible: true}, TerminalModes{AlternateScreen: true, MouseTracking: true})

	vt.Resize(8, 2)

	partialRedraw := "\x1b[?1049h" +
		"\x1b[1;1H\x1b[48;2;30;30;46mABCD" +
		"\x1b[2;1H\x1b[48;2;30;30;46mWXYZ"
	if _, err := vt.Write([]byte(partialRedraw)); err != nil {
		t.Fatalf("Write: %v", err)
	}

	restored := vt.ScreenContent()
	for _, point := range []struct {
		row int
		col int
	}{
		{row: 0, col: 6},
		{row: 1, col: 6},
	} {
		if got := restored.Cells[point.row][point.col].Style.BG; got != termBG {
			t.Fatalf("expected tail fill at row=%d col=%d to keep background %q, got %#v", point.row, point.col, termBG, restored.Cells[point.row][point.col])
		}
	}
}

func TestLoadSnapshotWithTimestampsRestoresRowTimes(t *testing.T) {
	vt := New(6, 3, 100, nil)
	scrollbackTS := []time.Time{time.Date(2026, 4, 7, 10, 0, 0, 0, time.UTC)}
	screenTS := []time.Time{time.Date(2026, 4, 7, 10, 0, 1, 0, time.UTC)}

	vt.LoadSnapshotWithTimestamps([][]Cell{
		{{Content: "o", Width: 1}, {Content: "l", Width: 1}, {Content: "d", Width: 1}},
	}, scrollbackTS, ScreenData{
		Cells: [][]Cell{
			{
				{Content: "n", Width: 1},
				{Content: "e", Width: 1},
				{Content: "w", Width: 1},
			},
		},
	}, screenTS, CursorState{Row: 0, Col: 3, Visible: true}, TerminalModes{AutoWrap: true})

	if got := vt.ScrollbackTimestamps(); len(got) != 1 || !got[0].Equal(scrollbackTS[0]) {
		t.Fatalf("unexpected restored scrollback timestamps: %#v", got)
	}
	if got := vt.ScreenTimestamps(); len(got) == 0 || !got[0].Equal(screenTS[0]) {
		t.Fatalf("unexpected restored screen timestamps: %#v", got)
	}
}

func TestLoadSnapshotWithMetadataRestoresRowKinds(t *testing.T) {
	vt := New(6, 3, 100, nil)

	vt.LoadSnapshotWithMetadata([][]Cell{{}}, []time.Time{time.Date(2026, 4, 7, 10, 0, 0, 0, time.UTC)}, []string{"restart"}, ScreenData{
		Cells: [][]Cell{
			{
				{Content: "n", Width: 1},
				{Content: "e", Width: 1},
				{Content: "w", Width: 1},
			},
		},
	}, []time.Time{time.Date(2026, 4, 7, 10, 0, 1, 0, time.UTC)}, []string{""}, CursorState{Row: 0, Col: 3, Visible: true}, TerminalModes{AutoWrap: true})

	if got := vt.ScrollbackRowKinds(); len(got) != 1 || got[0] != "restart" {
		t.Fatalf("unexpected restored scrollback row kinds: %#v", got)
	}
}

func TestVTermWriteAssignsRowTimestamps(t *testing.T) {
	vt := New(6, 2, 100, nil)

	if _, err := vt.Write([]byte("one\ntwo\nthree\n")); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	scrollbackTS := vt.ScrollbackTimestamps()
	if len(scrollbackTS) == 0 || scrollbackTS[0].IsZero() {
		t.Fatalf("expected scrollback timestamp after scroll, got %#v", scrollbackTS)
	}
	screenTS := vt.ScreenTimestamps()
	if len(screenTS) == 0 || screenTS[0].IsZero() {
		t.Fatalf("expected screen timestamps for visible rows, got %#v", screenTS)
	}
}

func TestVTermWriteSelectivelyInvalidatesOnlyChangedScreenRows(t *testing.T) {
	vt := New(6, 2, 100, nil)
	vt.LoadSnapshot(ScreenData{
		Cells: [][]Cell{
			{
				{Content: "t", Width: 1},
				{Content: "o", Width: 1},
				{Content: "p", Width: 1},
			},
			{
				{Content: "b", Width: 1},
				{Content: "o", Width: 1},
				{Content: "t", Width: 1},
			},
		},
	}, CursorState{Row: 1, Col: 0, Visible: true}, TerminalModes{AutoWrap: true})

	topBefore := vt.ScreenRowView(0)
	bottomBefore := vt.ScreenRowView(1)
	if len(topBefore) == 0 || len(bottomBefore) == 0 {
		t.Fatalf("expected cached rows, got top=%#v bottom=%#v", topBefore, bottomBefore)
	}

	if _, err := vt.Write([]byte("\x1b[2;1Hnew")); err != nil {
		t.Fatalf("write updated row: %v", err)
	}

	topAfter := vt.ScreenRowView(0)
	bottomAfter := vt.ScreenRowView(1)
	if strings.TrimSpace(rowToString(topAfter)) != "top" {
		t.Fatalf("expected unchanged top row preserved, got %q", rowToString(topAfter))
	}
	if strings.TrimSpace(rowToString(bottomAfter)) != "new" {
		t.Fatalf("expected updated bottom row, got %q", rowToString(bottomAfter))
	}
	if &topAfter[0] != &topBefore[0] {
		t.Fatal("expected unchanged screen row cache to be reused")
	}
	if &bottomAfter[0] == &bottomBefore[0] {
		t.Fatal("expected changed screen row cache to be invalidated")
	}
}

func TestVTermWriteReusesScrolledCachesAcrossScreenAndScrollback(t *testing.T) {
	vt := New(6, 2, 100, nil)
	vt.LoadSnapshotWithScrollback([][]Cell{
		{
			{Content: "h", Width: 1},
			{Content: "i", Width: 1},
			{Content: "s", Width: 1},
			{Content: "t", Width: 1},
		},
	}, ScreenData{
		Cells: [][]Cell{
			{
				{Content: "t", Width: 1},
				{Content: "o", Width: 1},
				{Content: "p", Width: 1},
			},
			{
				{Content: "b", Width: 1},
				{Content: "o", Width: 1},
				{Content: "t", Width: 1},
			},
		},
	}, CursorState{Row: 1, Col: 0, Visible: true}, TerminalModes{AutoWrap: true})

	scrollbackBefore := vt.ScrollbackRowView(0)
	topBefore := vt.ScreenRowView(0)
	bottomBefore := vt.ScreenRowView(1)
	if len(scrollbackBefore) == 0 || len(topBefore) == 0 || len(bottomBefore) == 0 {
		t.Fatalf("expected primed caches, got scrollback=%#v top=%#v bottom=%#v", scrollbackBefore, topBefore, bottomBefore)
	}

	if _, err := vt.Write([]byte("\n")); err != nil {
		t.Fatalf("scroll write: %v", err)
	}

	scrollbackAfter0 := vt.ScrollbackRowView(0)
	scrollbackAfter1 := vt.ScrollbackRowView(1)
	screenAfter0 := vt.ScreenRowView(0)
	screenAfter1 := vt.ScreenRowView(1)
	if strings.TrimSpace(rowToString(scrollbackAfter0)) == "" {
		t.Fatalf("expected existing scrollback row preserved, got %q", rowToString(scrollbackAfter0))
	}
	if strings.TrimSpace(rowToString(scrollbackAfter1)) != "top" {
		t.Fatalf("expected scrolled-off top row in scrollback, got %q", rowToString(scrollbackAfter1))
	}
	if strings.TrimSpace(rowToString(screenAfter0)) != "bot" {
		t.Fatalf("expected bottom row to move into first screen row, got %q", rowToString(screenAfter0))
	}
	if &scrollbackAfter0[0] != &scrollbackBefore[0] {
		t.Fatal("expected retained scrollback cache to be reused")
	}
	if &scrollbackAfter1[0] != &topBefore[0] {
		t.Fatal("expected scrolled-off screen row cache to move into scrollback cache")
	}
	if &screenAfter0[0] != &bottomBefore[0] {
		t.Fatal("expected shifted screen row cache to be reused")
	}
	if len(screenAfter1) == 0 || &screenAfter1[0] == &topBefore[0] {
		t.Fatal("expected newly blank screen row to allocate a fresh cache")
	}
}

func TestVTermWriteAssignsTimestampsToBlankRows(t *testing.T) {
	vt := New(6, 3, 100, nil)

	if _, err := vt.Write([]byte("\n")); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	screenTS := vt.ScreenTimestamps()
	if len(screenTS) < 2 || screenTS[0].IsZero() || screenTS[1].IsZero() {
		t.Fatalf("expected blank rows created by newline to receive timestamps, got %#v", screenTS)
	}
}

func TestVTermTracksMouseModesFromEscapeSequences(t *testing.T) {
	vt := New(20, 5, 100, nil)

	if vt.Modes().MouseTracking {
		t.Fatal("expected mouse tracking disabled by default")
	}
	if _, err := vt.Write([]byte("\x1b[?1002h\x1b[?1006h")); err != nil {
		t.Fatalf("enable mouse tracking failed: %v", err)
	}
	if !vt.Modes().MouseTracking {
		t.Fatal("expected mouse tracking after enabling button-event mode")
	}
	if !vt.Modes().MouseButtonEvent || !vt.Modes().MouseSGR {
		t.Fatalf("expected button-event+SGR mouse mode flags, got %#v", vt.Modes())
	}
	if _, err := vt.Write([]byte("\x1b[?1006l")); err != nil {
		t.Fatalf("disable sgr mode failed: %v", err)
	}
	if !vt.Modes().MouseTracking {
		t.Fatal("expected mouse tracking to remain enabled after disabling sgr encoding only")
	}
	if vt.Modes().MouseSGR {
		t.Fatalf("expected SGR flag cleared after disabling 1006, got %#v", vt.Modes())
	}
	if _, err := vt.Write([]byte("\x1b[?1002l")); err != nil {
		t.Fatalf("disable mouse tracking failed: %v", err)
	}
	if vt.Modes().MouseTracking {
		t.Fatal("expected mouse tracking disabled after reset")
	}
}

func TestLoadSnapshotRestoresMouseTrackingMode(t *testing.T) {
	vt := New(10, 4, 100, nil)
	vt.LoadSnapshot(ScreenData{
		Cells: [][]Cell{{{Content: "x", Width: 1}}},
	}, CursorState{Row: 0, Col: 1, Visible: true}, TerminalModes{AutoWrap: true, MouseTracking: true, MouseNormal: true})

	if !vt.Modes().MouseTracking {
		t.Fatal("expected snapshot restore to preserve mouse tracking")
	}
	if !vt.Modes().MouseNormal || vt.Modes().MouseSGR {
		t.Fatalf("expected snapshot restore to preserve legacy mouse encoding, got %#v", vt.Modes())
	}
}

func TestLoadSnapshotRestoresSGRMouseEncodingMode(t *testing.T) {
	vt := New(10, 4, 100, nil)
	vt.LoadSnapshot(ScreenData{
		Cells: [][]Cell{{{Content: "x", Width: 1}}},
	}, CursorState{Row: 0, Col: 1, Visible: true}, TerminalModes{AutoWrap: true, MouseTracking: true, MouseButtonEvent: true, MouseSGR: true})

	if !vt.Modes().MouseTracking || !vt.Modes().MouseButtonEvent || !vt.Modes().MouseSGR {
		t.Fatalf("expected snapshot restore to preserve SGR mouse encoding, got %#v", vt.Modes())
	}
}

func TestVTermTracksAlternateScrollModeFromEscapeSequences(t *testing.T) {
	vt := New(20, 5, 100, nil)

	if vt.Modes().AlternateScroll {
		t.Fatal("expected alternate scroll disabled by default")
	}
	if _, err := vt.Write([]byte("\x1b[?1007h")); err != nil {
		t.Fatalf("enable alternate scroll failed: %v", err)
	}
	if !vt.Modes().AlternateScroll {
		t.Fatal("expected alternate scroll enabled after escape sequence")
	}
	if _, err := vt.Write([]byte("\x1b[?1007l")); err != nil {
		t.Fatalf("disable alternate scroll failed: %v", err)
	}
	if vt.Modes().AlternateScroll {
		t.Fatal("expected alternate scroll disabled after reset")
	}
}

func TestLoadSnapshotRestoresAlternateScrollMode(t *testing.T) {
	vt := New(10, 4, 100, nil)
	vt.LoadSnapshot(ScreenData{
		Cells: [][]Cell{{{Content: "x", Width: 1}}},
	}, CursorState{Row: 0, Col: 1, Visible: true}, TerminalModes{AutoWrap: true, AlternateScroll: true})

	if !vt.Modes().AlternateScroll {
		t.Fatal("expected snapshot restore to preserve alternate scroll")
	}
}

func TestApplyScreenUpdateUpdatesChangedRowsInPlace(t *testing.T) {
	vt := New(6, 3, 100, nil)
	now := time.Date(2026, 4, 18, 8, 0, 0, 0, time.UTC)
	vt.LoadSnapshotWithMetadata(nil, nil, nil, ScreenData{
		Cells: [][]Cell{
			{
				{Content: "o", Width: 1},
				{Content: "l", Width: 1},
				{Content: "d", Width: 1},
				{Content: " ", Width: 1},
			},
			{
				{Content: "r", Width: 1},
				{Content: "o", Width: 1},
				{Content: "w", Width: 1},
				{Content: " ", Width: 1},
			},
		},
		IsAlternateScreen: true,
	}, []time.Time{now, now}, []string{"old-0", "old-1"}, CursorState{Row: 1, Col: 3, Visible: true}, TerminalModes{AlternateScreen: true, AutoWrap: true})

	oldEmu := vt.emu
	if !vt.ApplyScreenUpdate(protocol.ScreenUpdate{
		Size: protocol.Size{Cols: 4, Rows: 2},
		ChangedRows: []protocol.ScreenRowUpdate{{
			Row: 1,
			Cells: []protocol.Cell{
				{Content: "n", Width: 1},
				{Content: "e", Width: 1},
				{Content: "w", Width: 1},
				{Content: "!", Width: 1},
			},
			Timestamp: now.Add(time.Second),
			RowKind:   "new-1",
		}},
		Cursor: protocol.CursorState{Row: 1, Col: 4, Visible: true, Shape: "bar"},
		Modes:  protocol.TerminalModes{AlternateScreen: true, AutoWrap: true},
	}) {
		t.Fatal("expected incremental screen update to apply")
	}

	if vt.emu != oldEmu {
		t.Fatal("expected incremental apply to keep the existing emulator instance")
	}
	screen := vt.ScreenContent()
	if got := screen.Cells[1][0].Content + screen.Cells[1][1].Content + screen.Cells[1][2].Content + screen.Cells[1][3].Content; got != "new!" {
		t.Fatalf("expected updated row content, got %q", got)
	}
	if got := vt.ScreenRowTimestampAt(1); !got.Equal(now.Add(time.Second)) {
		t.Fatalf("expected updated row timestamp, got %v", got)
	}
	if got := vt.ScreenRowKindAt(1); got != "new-1" {
		t.Fatalf("expected updated row kind, got %q", got)
	}
	if cursor := vt.CursorState(); cursor.Row != 1 || cursor.Col != 4 || cursor.Shape != CursorBar {
		t.Fatalf("expected updated cursor, got %#v", cursor)
	}
}

func TestApplyScreenUpdateRejectsUnsupportedScrollbackMutation(t *testing.T) {
	vt := New(6, 3, 100, nil)
	vt.LoadSnapshotWithScrollback([][]Cell{{{Content: "o", Width: 1}}}, ScreenData{
		Cells: [][]Cell{{
			{Content: "n", Width: 1},
		}},
	}, CursorState{Row: 0, Col: 1, Visible: true}, TerminalModes{AutoWrap: true})

	oldEmu := vt.emu
	if vt.ApplyScreenUpdate(protocol.ScreenUpdate{
		Size:           protocol.Size{Cols: 1, Rows: 1},
		ScrollbackTrim: 1,
		Cursor:         protocol.CursorState{Row: 0, Col: 1, Visible: true},
		Modes:          protocol.TerminalModes{AutoWrap: true},
	}) {
		t.Fatal("expected scrollback mutation to fall back instead of partial apply")
	}
	if vt.emu != oldEmu {
		t.Fatal("expected rejected partial apply to leave emulator untouched")
	}
}
