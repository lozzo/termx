package termx

import (
	"reflect"
	"testing"

	"github.com/lozzow/termx/internal/protocol"
)

func TestApplyScreenUpdateSnapshotScrollbackAppendTrimsProjectionPadding(t *testing.T) {
	next := applyScreenUpdateSnapshot(&protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 8, Rows: 2},
	}, "term-1", protocol.ScreenUpdate{
		ScrollbackAppend: []protocol.ScrollbackRowAppend{{
			Cells:   protocolPaddedRowForTest("line", 8),
			RowKind: "append",
		}},
		Cursor: protocol.CursorState{Visible: true},
		Modes:  protocol.TerminalModes{AutoWrap: true},
	})

	if got := protocolCompactRowTextForTest(next.Scrollback[0]); got != "line" {
		t.Fatalf("expected unwrapped screen projection padding to be trimmed, got %q", got)
	}
	if got := len(next.Scrollback[0].DecodeCells()); got != 4 {
		t.Fatalf("expected compacted append row to contain only content cells, got %d cells", got)
	}
	if got := next.ScrollbackWrapped; !reflect.DeepEqual(got, []bool{false}) {
		t.Fatalf("expected unwrapped metadata, got %#v", got)
	}
	if got := next.ScrollbackOwnership; !reflect.DeepEqual(got, []string{protocol.RowOwnershipLiveTailLive}) {
		t.Fatalf("expected live-tail ownership on append row, got %#v", got)
	}
}

func TestApplyScreenUpdateSnapshotScrollbackAppendPreservesSemanticTrailingBlankCells(t *testing.T) {
	styled := protocolPaddedRowForTest("style", 8)
	styled[5].Style.BG = "#222222"
	linked := protocolPaddedRowForTest("link", 8)
	linked[4].LinkURL = "https://example.test/blank"
	wrapped := protocolPaddedRowForTest("wrap", 8)

	next := applyScreenUpdateSnapshot(nil, "term-1", protocol.ScreenUpdate{
		Size: protocol.Size{Cols: 8, Rows: 2},
		ScrollbackAppend: []protocol.ScrollbackRowAppend{
			{Cells: styled},
			{Cells: linked},
			{Cells: wrapped, Wrapped: true, WrappedSet: true},
		},
		Cursor: protocol.CursorState{Visible: true},
		Modes:  protocol.TerminalModes{AutoWrap: true},
	})

	if got := len(next.Scrollback[0].DecodeCells()); got != 6 {
		t.Fatalf("expected styled trailing blank to remain addressable, got %d cells", got)
	}
	if got := len(next.Scrollback[1].DecodeCells()); got != 5 {
		t.Fatalf("expected linked trailing blank to remain addressable, got %d cells", got)
	}
	if got := protocolCompactRowTextForTest(next.Scrollback[2]); got != "wrap    " {
		t.Fatalf("expected wrapped append row to preserve trailing blanks, got %q", got)
	}
	if got := next.ScrollbackWrapped; !reflect.DeepEqual(got, []bool{false, false, true}) {
		t.Fatalf("expected wrapped metadata to follow append rows, got %#v", got)
	}
}

func protocolPaddedRowForTest(text string, width int) []protocol.Cell {
	row := make([]protocol.Cell, 0, width)
	for _, r := range text {
		row = append(row, protocol.Cell{Content: string(r), Width: 1})
	}
	for len(row) < width {
		row = append(row, protocol.Cell{Content: " ", Width: 1})
	}
	return row
}

func protocolCompactRowTextForTest(row protocol.CompactRow) string {
	cells := row.DecodeCells()
	out := make([]rune, 0, len(cells))
	for _, cell := range cells {
		out = append(out, []rune(cell.Content)...)
	}
	return string(out)
}
