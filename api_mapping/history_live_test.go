package apimapping

import (
	"fmt"
	"testing"

	corev2 "github.com/anytty/anytty/core"
	"github.com/anytty/anytty/core/history"
	vterm "github.com/anytty/anytty/vterm/vterm"
	"google.golang.org/protobuf/proto"
)

func TestHistoryWindowToProtoCoalescesAdjacentStyleRuns(t *testing.T) {
	red := history.CellStyle{FG: "ansi:1", Bold: true}
	window := HistoryWindowToProto("machine", history.HistoryWindow{
		TerminalID: "terminal",
		Rows: []history.HistoryRow{{Cells: []history.Cell{
			{Text: "a", Width: 1, Style: red},
			{Text: "好", Width: 2, Style: red},
			{Text: "b", Width: 1, Style: red, LinkURL: "https://example.test"},
			{Text: "c", Width: 1, Style: red, LinkURL: "https://example.test"},
			{Text: "", Width: 1},
			{Text: "", Width: 2},
		}}},
	})
	cells := window.GetRows()[0].GetRow().GetCells()
	if len(cells) != 3 {
		t.Fatalf("coalesced history cells = %#v", cells)
	}
	if cells[0].GetContent() != "a好" || cells[0].GetWidth() != 3 || cells[1].GetContent() != "bc" || cells[1].GetWidth() != 2 || cells[2].GetContent() != "" || cells[2].GetWidth() != 3 {
		t.Fatalf("coalesced history runs lost content or width: %#v", cells)
	}
}

func TestNativeScreenToProtoCoalescesAdjacentStyleRuns(t *testing.T) {
	green := vterm.CellStyle{FG: "ansi:2"}
	screen := NativeScreenToProto("machine", corev2.NativeScreenSnapshot{
		TerminalID: "terminal",
		Rows: []corev2.NativeScreenRow{{Cells: []vterm.Cell{
			{Content: "x", Width: 1, Style: green},
			{Content: "y", Width: 1, Style: green},
			{Content: "z", Width: 1},
		}}},
	})
	cells := screen.GetRows()[0].GetCells()
	if len(cells) != 2 || cells[0].GetContent() != "xy" || cells[0].GetWidth() != 2 || cells[1].GetContent() != "z" {
		t.Fatalf("coalesced live runs = %#v", cells)
	}
}

func TestHistoryWindowToProtoFitsRemotePageBudget(t *testing.T) {
	styles := []history.CellStyle{
		{FG: "ansi:1", Bold: true},
		{FG: "ansi:2"},
		{FG: "ansi:4", Underline: true},
	}
	rows := make([]history.HistoryRow, 100)
	for rowIndex := range rows {
		cells := make([]history.Cell, 49)
		for column := range cells {
			cells[column] = history.Cell{
				Text:  fmt.Sprintf("%x", (rowIndex+column)%16),
				Width: 1,
				Style: styles[(column/8+rowIndex)%len(styles)],
			}
		}
		rows[rowIndex] = history.HistoryRow{Cells: cells}
	}

	window := HistoryWindowToProto("machine", history.HistoryWindow{
		TerminalID: "terminal",
		Rows:       rows,
	})
	if size := proto.Size(window); size >= 60*1024 {
		t.Fatalf("100-row history response = %d bytes, want < 60 KiB", size)
	}
}
