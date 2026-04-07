package vterm

import (
	"testing"
	"time"
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
