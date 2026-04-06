package vterm

import "testing"

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
	if _, err := vt.Write([]byte("\x1b[?1006l")); err != nil {
		t.Fatalf("disable sgr mode failed: %v", err)
	}
	if !vt.Modes().MouseTracking {
		t.Fatal("expected mouse tracking to remain enabled after disabling sgr encoding only")
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
	}, CursorState{Row: 0, Col: 1, Visible: true}, TerminalModes{AutoWrap: true, MouseTracking: true})

	if !vt.Modes().MouseTracking {
		t.Fatal("expected snapshot restore to preserve mouse tracking")
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
