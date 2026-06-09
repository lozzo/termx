package protocol

import "testing"

func TestTrimSnapshotScrollbackScreenVisualOverlapDropsDuplicatedScrollbackSuffix(t *testing.T) {
	snapshot := &Snapshot{
		Scrollback: CompactRowsFromCells([][]Cell{
			protocolOverlapRow("hist-076"),
			protocolOverlapRow("hist-077"),
			protocolOverlapRow("hist-078"),
			protocolOverlapRow("hist-079"),
			protocolOverlapRow("hist-080"),
		}),
		ScrollbackOwnership:  []string{RowOwnershipPersisted, RowOwnershipPersisted, RowOwnershipPersisted, RowOwnershipPersisted, RowOwnershipPersisted},
		ScrollbackWrapped:    []bool{false, false, false, false, false},
		ScrollbackLoadedRows: 81,
		ScrollbackTotal:      81,
		HistoryGeneration:    8,
		ScrollbackFirstRowID: 0,
		ScrollbackLastRowID:  80,
		Screen: ScreenData{Cells: [][]Cell{
			protocolOverlapRow("hist-078"),
			protocolOverlapRow("hist-079"),
			protocolOverlapRow("hist-080"),
			protocolOverlapRow("prompt"),
		}},
	}

	if got := TrimSnapshotScrollbackScreenVisualOverlap(snapshot); got != 3 {
		t.Fatalf("expected three duplicated projection rows trimmed, got %d", got)
	}
	if got, want := len(snapshot.Scrollback), 2; got != want {
		t.Fatalf("expected scrollback suffix trimmed to %d rows, got %d", want, got)
	}
	if got := protocolOverlapRowText(snapshot.Scrollback[0].DecodeCells()); got != "hist-076" {
		t.Fatalf("expected first retained row hist-076, got %q", got)
	}
	if got := protocolOverlapRowText(snapshot.Scrollback[1].DecodeCells()); got != "hist-077" {
		t.Fatalf("expected second retained row hist-077, got %q", got)
	}
	if got, want := snapshot.ScrollbackLoadedRows, 81; got != want {
		t.Fatalf("expected committed loaded depth to stay %d, got %d", want, got)
	}
	if got, want := snapshot.ScrollbackLastRowID, uint64(80); got != want {
		t.Fatalf("expected canonical row coordinates to stay on loaded history window, got %d", got)
	}
	if got, want := len(snapshot.ScrollbackOwnership), 2; got != want {
		t.Fatalf("expected ownership metadata trimmed with materialized rows, got %d want %d", got, want)
	}
}

func TestTrimSnapshotScrollbackScreenVisualOverlapIgnoresSingleRepeatedLine(t *testing.T) {
	snapshot := &Snapshot{
		Scrollback:          CompactRowsFromCells([][]Cell{protocolOverlapRow("repeat")}),
		ScrollbackOwnership: []string{RowOwnershipPersisted},
		Screen:              ScreenData{Cells: [][]Cell{protocolOverlapRow("repeat")}},
	}

	if got := TrimSnapshotScrollbackScreenVisualOverlap(snapshot); got != 0 {
		t.Fatalf("expected single repeated line not to be treated as duplicate projection, got %d", got)
	}
	if got, want := len(snapshot.Scrollback), 1; got != want {
		t.Fatalf("expected scrollback unchanged, got %d rows", got)
	}
}

func TestTrimSnapshotScrollbackScreenVisualOverlapIgnoresBlankOverlap(t *testing.T) {
	blank := []Cell{{Content: " ", Width: 1}, {Content: " ", Width: 1}}
	snapshot := &Snapshot{
		Scrollback:          CompactRowsFromCells([][]Cell{blank, blank}),
		ScrollbackOwnership: []string{RowOwnershipPersisted, RowOwnershipPersisted},
		Screen:              ScreenData{Cells: [][]Cell{blank, blank}},
	}

	if got := TrimSnapshotScrollbackScreenVisualOverlap(snapshot); got != 0 {
		t.Fatalf("expected blank-only rows not to be treated as duplicate projection, got %d", got)
	}
	if got, want := len(snapshot.Scrollback), 2; got != want {
		t.Fatalf("expected blank scrollback unchanged, got %d rows", got)
	}
}

func protocolOverlapRow(text string) []Cell {
	cells := make([]Cell, 0, len(text))
	for _, r := range text {
		cells = append(cells, Cell{Content: string(r), Width: 1})
	}
	cells = append(cells, Cell{Content: " ", Width: 1}, Cell{Content: " ", Width: 1})
	return cells
}

func protocolOverlapRowText(row []Cell) string {
	out := ""
	for _, cell := range row {
		out += cell.Content
	}
	for len(out) > 0 && out[len(out)-1] == ' ' {
		out = out[:len(out)-1]
	}
	return out
}
