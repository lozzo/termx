package runtime

import (
	"reflect"
	"testing"
	"time"

	"github.com/lozzow/termx/protocol"
)

func TestApplyScreenUpdateSnapshotChangedRowsDoesNotMutatePreviousSnapshot(t *testing.T) {
	now := time.Date(2026, 4, 18, 10, 0, 0, 0, time.UTC)
	current := &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 4, Rows: 3},
		Screen: protocol.ScreenData{Cells: [][]protocol.Cell{
			snapshotTestRow("one1"),
			snapshotTestRow("two2"),
			snapshotTestRow("thr3"),
		}},
		ScreenTimestamps: []time.Time{now, now.Add(time.Second), now.Add(2 * time.Second)},
		ScreenRowKinds:   []string{"a", "b", "c"},
		Cursor:           protocol.CursorState{Row: 2, Col: 1, Visible: true},
		Modes:            protocol.TerminalModes{AutoWrap: true},
		Timestamp:        now,
	}
	previous := cloneProtocolSnapshot(current)

	next := applyScreenUpdateSnapshot(current, "term-1", protocol.ScreenUpdate{
		ChangedRows: []protocol.ScreenRowUpdate{{
			Row:       1,
			Cells:     snapshotTestRow("edit"),
			Timestamp: now.Add(3 * time.Second),
			RowKind:   "edited",
		}},
		Cursor: protocol.CursorState{Row: 1, Col: 2, Visible: true},
		Modes:  protocol.TerminalModes{AutoWrap: true},
	})

	if !reflect.DeepEqual(current, previous) {
		t.Fatalf("expected previous snapshot to remain unchanged, got %#v want %#v", current, previous)
	}
	if got := rowToString(next.Screen.Cells[1]); got != "edit" {
		t.Fatalf("expected updated row in next snapshot, got %q", got)
	}
	if got := rowToString(current.Screen.Cells[1]); got != "two2" {
		t.Fatalf("expected previous snapshot row to remain unchanged, got %q", got)
	}
}

func TestApplyScreenUpdateSnapshotScrollbackTrimAppendDoesNotMutatePreviousSnapshot(t *testing.T) {
	now := time.Date(2026, 4, 18, 11, 0, 0, 0, time.UTC)
	current := &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 5, Rows: 2},
		Scrollback: [][]protocol.Cell{
			snapshotTestRow("hist1"),
			snapshotTestRow("hist2"),
			snapshotTestRow("hist3"),
		},
		ScrollbackTimestamps: []time.Time{now, now.Add(time.Second), now.Add(2 * time.Second)},
		ScrollbackRowKinds:   []string{"a", "b", "c"},
		Screen: protocol.ScreenData{Cells: [][]protocol.Cell{
			snapshotTestRow("live1"),
			snapshotTestRow("live2"),
		}},
		Cursor:    protocol.CursorState{Visible: false},
		Modes:     protocol.TerminalModes{AutoWrap: true},
		Timestamp: now,
	}
	previous := cloneProtocolSnapshot(current)

	next := applyScreenUpdateSnapshot(current, "term-1", protocol.ScreenUpdate{
		ScrollbackTrim: 1,
		ScrollbackAppend: []protocol.ScrollbackRowAppend{{
			Cells:     snapshotTestRow("hist4"),
			Timestamp: now.Add(3 * time.Second),
			RowKind:   "d",
		}},
		Cursor: protocol.CursorState{Visible: false},
		Modes:  protocol.TerminalModes{AutoWrap: true},
	})

	if !reflect.DeepEqual(current, previous) {
		t.Fatalf("expected previous scrollback snapshot to remain unchanged, got %#v want %#v", current, previous)
	}
	if got := []string{
		rowToString(next.Scrollback[0]),
		rowToString(next.Scrollback[1]),
		rowToString(next.Scrollback[2]),
	}; !reflect.DeepEqual(got, []string{"hist2", "hist3", "hist4"}) {
		t.Fatalf("unexpected next scrollback rows: %#v", got)
	}
	if got := []string{
		rowToString(current.Scrollback[0]),
		rowToString(current.Scrollback[1]),
		rowToString(current.Scrollback[2]),
	}; !reflect.DeepEqual(got, []string{"hist1", "hist2", "hist3"}) {
		t.Fatalf("expected previous scrollback rows to remain unchanged, got %#v", got)
	}
}

func TestApplyScreenUpdateSnapshotChangedSpansDoesNotMutatePreviousSnapshot(t *testing.T) {
	now := time.Date(2026, 4, 18, 10, 30, 0, 0, time.UTC)
	current := &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 8, Rows: 1},
		Screen: protocol.ScreenData{Cells: [][]protocol.Cell{
			snapshotTestRow("prefixXY"),
		}},
		ScreenTimestamps: []time.Time{now},
		ScreenRowKinds:   []string{"row"},
		Cursor:           protocol.CursorState{Visible: true},
		Modes:            protocol.TerminalModes{AutoWrap: true},
		Timestamp:        now,
	}
	previous := cloneProtocolSnapshot(current)

	next := applyScreenUpdateSnapshot(current, "term-1", protocol.ScreenUpdate{
		ChangedSpans: []protocol.ScreenSpanUpdate{
			{
				Row:      0,
				ColStart: 2,
				Cells: []protocol.Cell{
					{Content: "Q", Width: 1},
				},
				Op: protocol.ScreenSpanOpWrite,
			},
			{
				Row:      0,
				ColStart: 6,
				Op:       protocol.ScreenSpanOpClearToEOL,
			},
		},
		Cursor: protocol.CursorState{Visible: true},
		Modes:  protocol.TerminalModes{AutoWrap: true},
	})

	if !reflect.DeepEqual(current, previous) {
		t.Fatalf("expected previous snapshot to remain unchanged, got %#v want %#v", current, previous)
	}
	if got := rowToString(next.Screen.Cells[0]); got != "prQfix" {
		t.Fatalf("expected span-updated row in next snapshot, got %q", got)
	}
	if got := rowToString(current.Screen.Cells[0]); got != "prefixXY" {
		t.Fatalf("expected previous snapshot row to remain unchanged, got %q", got)
	}
}

func TestApplyScreenUpdateSnapshotWideSpanPreservesContinuation(t *testing.T) {
	now := time.Date(2026, 4, 18, 10, 45, 0, 0, time.UTC)
	current := &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 4, Rows: 1},
		Screen: protocol.ScreenData{Cells: [][]protocol.Cell{{
			{Content: "你", Width: 2},
			{Content: "", Width: 0},
			{Content: "a", Width: 1},
		}}},
		ScreenTimestamps: []time.Time{now},
		ScreenRowKinds:   []string{"wide"},
		Cursor:           protocol.CursorState{Visible: true},
		Modes:            protocol.TerminalModes{AutoWrap: true},
	}

	next := applyScreenUpdateSnapshot(current, "term-1", protocol.ScreenUpdate{
		ChangedSpans: []protocol.ScreenSpanUpdate{{
			Row:      0,
			ColStart: 0,
			Cells: []protocol.Cell{
				{Content: "界", Width: 2},
				{Content: "", Width: 0},
			},
			Op: protocol.ScreenSpanOpWrite,
		}},
		Cursor: protocol.CursorState{Visible: true},
		Modes:  protocol.TerminalModes{AutoWrap: true},
	})

	if got := next.Screen.Cells[0][0]; got.Content != "界" || got.Width != 2 {
		t.Fatalf("expected updated wide anchor, got %#v", got)
	}
	if got := next.Screen.Cells[0][1]; got.Content != "" || got.Width != 0 {
		t.Fatalf("expected wide continuation preserved, got %#v", got)
	}
}

func TestApplyScreenUpdateSnapshotFullReplaceBehaviorUnchanged(t *testing.T) {
	now := time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)
	current := &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 8, Rows: 4},
		Screen: protocol.ScreenData{Cells: [][]protocol.Cell{
			snapshotTestRow("stale"),
		}},
		Scrollback: [][]protocol.Cell{
			snapshotTestRow("old"),
		},
		Cursor: protocol.CursorState{Visible: false},
		Modes:  protocol.TerminalModes{AutoWrap: true},
	}
	update := protocol.ScreenUpdate{
		FullReplace: true,
		Size:        protocol.Size{Cols: 6, Rows: 2},
		Screen: protocol.ScreenData{Cells: [][]protocol.Cell{
			snapshotTestRow("fresh1"),
			snapshotTestRow("fresh2"),
		}},
		ScreenTimestamps: []time.Time{now, now.Add(time.Second)},
		ScreenRowKinds:   []string{"row-a", "row-b"},
		ScrollbackAppend: []protocol.ScrollbackRowAppend{{
			Cells:     snapshotTestRow("tail1"),
			Timestamp: now.Add(2 * time.Second),
			RowKind:   "tail",
		}},
		Cursor: protocol.CursorState{Row: 1, Col: 3, Visible: true},
		Modes:  protocol.TerminalModes{AutoWrap: true, AlternateScreen: true},
	}

	next := applyScreenUpdateSnapshot(current, "term-1", update)
	update.Screen.Cells[0][0].Content = "X"
	update.ScrollbackAppend[0].Cells[0].Content = "Y"

	if got := rowToString(next.Screen.Cells[0]); got != "fresh1" {
		t.Fatalf("expected full replace screen to deep clone update rows, got %q", got)
	}
	if got := rowToString(next.Scrollback[0]); got != "tail1" {
		t.Fatalf("expected full replace scrollback append to clone rows, got %q", got)
	}
	if len(next.Scrollback) != 1 {
		t.Fatalf("expected full replace to discard previous scrollback and keep appended rows only, got %#v", next.Scrollback)
	}
	if next.Size != update.Size {
		t.Fatalf("expected full replace to use update size, got %#v want %#v", next.Size, update.Size)
	}
	if !next.Screen.IsAlternateScreen || !next.Modes.AlternateScreen {
		t.Fatalf("expected full replace alternate-screen state to follow modes, got screen=%v modes=%#v", next.Screen.IsAlternateScreen, next.Modes)
	}
}

func TestApplyScreenUpdateSnapshotScreenScrollShiftPreservesPreviousSnapshot(t *testing.T) {
	now := time.Date(2026, 4, 18, 13, 0, 0, 0, time.UTC)
	current := &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 4, Rows: 3},
		Screen: protocol.ScreenData{Cells: [][]protocol.Cell{
			snapshotTestRow("row1"),
			snapshotTestRow("row2"),
			snapshotTestRow("row3"),
		}},
		ScreenTimestamps: []time.Time{now, now.Add(time.Second), now.Add(2 * time.Second)},
		ScreenRowKinds:   []string{"a", "b", "c"},
		Cursor:           protocol.CursorState{Visible: true},
		Modes:            protocol.TerminalModes{AutoWrap: true},
	}
	previous := cloneProtocolSnapshot(current)

	next := applyScreenUpdateSnapshot(current, "term-1", protocol.ScreenUpdate{
		ScreenScroll: 1,
		ChangedRows: []protocol.ScreenRowUpdate{{
			Row:       2,
			Cells:     snapshotTestRow("row4"),
			Timestamp: now.Add(3 * time.Second),
			RowKind:   "d",
		}},
		Cursor: protocol.CursorState{Visible: true},
		Modes:  protocol.TerminalModes{AutoWrap: true},
	})

	if !reflect.DeepEqual(current, previous) {
		t.Fatalf("expected previous snapshot to remain unchanged, got %#v want %#v", current, previous)
	}
	got := []string{
		rowToString(next.Screen.Cells[0]),
		rowToString(next.Screen.Cells[1]),
		rowToString(next.Screen.Cells[2]),
	}
	if !reflect.DeepEqual(got, []string{"row2", "row3", "row4"}) {
		t.Fatalf("unexpected shifted screen rows: %#v", got)
	}
}

func snapshotTestRow(text string) []protocol.Cell {
	row := make([]protocol.Cell, 0, len(text))
	for _, r := range text {
		row = append(row, protocol.Cell{Content: string(r), Width: 1})
	}
	return row
}

func rowToString(row []protocol.Cell) string {
	out := make([]rune, 0, len(row))
	for _, cell := range row {
		out = append(out, []rune(cell.Content)...)
	}
	return string(out)
}
