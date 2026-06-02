package render

import (
	"testing"

	"github.com/lozzow/termx/internal/protocol"
)

func TestCopyModeProjectionSourceUsesViewTopAsScreenBase(t *testing.T) {
	projection := &RenderCopyModeProjectionVM{
		TerminalID: "term-1",
		Token:      "token-1",
		Size:       protocol.Size{Cols: 20, Rows: 2},
		Rows: []RenderCopyModeProjectionRowVM{
			{Cells: protocolRowFromText("row-0")},
			{Cells: protocolRowFromText("row-1")},
			{Cells: protocolRowFromText("row-2")},
			{Cells: protocolRowFromText("row-3")},
		},
	}
	source := copyModeProjectionSource(RenderCopyModeVM{
		ViewTopRow: 1,
		Projection: projection,
	}, 2)
	if source == nil {
		t.Fatal("expected projection source")
	}
	if source.ScrollbackRows() != 1 || source.ScreenRows() != 2 || source.TotalRows() != 4 {
		t.Fatalf("unexpected projection source shape scrollback=%d screen=%d total=%d", source.ScrollbackRows(), source.ScreenRows(), source.TotalRows())
	}
	if got := projectionTestRowText(source.Row(source.ScrollbackRows())); got != "row-1" {
		t.Fatalf("expected first visible row from view top, got %q", got)
	}
	if got := projectionTestRowText(source.Row(source.ScrollbackRows() + 1)); got != "row-2" {
		t.Fatalf("expected second visible row from view top, got %q", got)
	}
	if rowIndex := terminalSourceWindowRowIndex(source, 2, 0, 0); rowIndex != 1 {
		t.Fatalf("expected terminal source screen base to start at row 1, got %d", rowIndex)
	}
}

func TestCopyModeProjectionSignatureIncludesInteriorRows(t *testing.T) {
	projection := &RenderCopyModeProjectionVM{
		TerminalID: "term-1",
		Token:      "token-1",
		Generation: 10,
		Size:       protocol.Size{Cols: 20, Rows: 3},
		Rows: []RenderCopyModeProjectionRowVM{
			{Cells: protocolRowFromText("same-head")},
			{Cells: protocolRowFromText("interior-a")},
			{Cells: protocolRowFromText("same-tail")},
		},
	}
	first := copyModeProjectionSignature(projection)
	projection.Rows[1].Cells = protocolRowFromText("interior-b")
	second := copyModeProjectionSignature(projection)
	if first == second {
		t.Fatal("expected projection signature to include interior row content")
	}
}

func projectionTestRowText(cells []protocol.Cell) string {
	text := ""
	for _, cell := range cells {
		if cell.Content != "" {
			text += cell.Content
		}
	}
	return text
}
