package history

import (
	"testing"

	vterm "github.com/lozzow/termx/termx-vterm/vterm"
)

func TestR420ScreenBufferScrollOutSealsPhysicalRowOnce(t *testing.T) {
	buffer := NewScreenHistoryBuffer(12, 2)
	firstRowID := buffer.VisibleRows()[0].ID
	secondRowID := buffer.VisibleRows()[1].ID

	if err := buffer.ApplyTransaction(TerminalSemanticTransaction{
		Seq:  1,
		Size: TerminalSemanticSize{Cols: 12, Rows: 2},
		Ops: []TerminalSemanticOp{
			screenBufferWriteOp(0, 0, "one"),
			screenBufferControlOp("lf"),
			screenBufferWriteOp(1, 0, "two"),
		},
	}); err != nil {
		t.Fatalf("apply initial rows: %v", err)
	}
	if err := buffer.ApplyTransaction(TerminalSemanticTransaction{
		Seq:  2,
		Size: TerminalSemanticSize{Cols: 12, Rows: 2},
		Ops:  []TerminalSemanticOp{screenBufferControlOp("lf")},
	}); err != nil {
		t.Fatalf("apply scroll: %v", err)
	}

	committed := buffer.CommittedRows()
	if len(committed) != 1 {
		t.Fatalf("expected one committed physical row, got %#v", committed)
	}
	if committed[0].ID != firstRowID || !committed[0].Sealed || committed[0].Text() != "one" {
		t.Fatalf("scroll-out must seal original row identity once, committed=%#v firstID=%d", committed[0], firstRowID)
	}
	visible := buffer.VisibleRows()
	if visible[0].ID != secondRowID || visible[0].Text() != "two" {
		t.Fatalf("scroll must move second row identity into top row, visible=%#v secondID=%d", visible, secondRowID)
	}
	if visible[0].ID == committed[0].ID {
		t.Fatalf("current screen must not duplicate committed RowID, visible=%#v committed=%#v", visible, committed)
	}
}

func TestR420ScreenBufferWriteMutatesRowVersionAndKeepsRowID(t *testing.T) {
	buffer := NewScreenHistoryBuffer(12, 2)
	before := buffer.VisibleRows()[0]
	if err := buffer.ApplyTransaction(TerminalSemanticTransaction{
		Seq:  1,
		Size: TerminalSemanticSize{Cols: 12, Rows: 2},
		Ops:  []TerminalSemanticOp{screenBufferWriteOp(0, 0, "abc")},
	}); err != nil {
		t.Fatalf("apply write: %v", err)
	}
	after := buffer.VisibleRows()[0]
	if after.ID != before.ID {
		t.Fatalf("in-place write must keep RowID, before=%#v after=%#v", before, after)
	}
	if after.Version <= before.Version {
		t.Fatalf("in-place write must increment Version, before=%#v after=%#v", before, after)
	}
	if got := after.Text(); got != "abc" {
		t.Fatalf("unexpected row text %q row=%#v", got, after)
	}
}

func TestR420ScreenBufferBasicCursorControls(t *testing.T) {
	buffer := NewScreenHistoryBuffer(16, 2)
	if err := buffer.ApplyTransaction(TerminalSemanticTransaction{
		Seq:  1,
		Size: TerminalSemanticSize{Cols: 16, Rows: 2},
		Ops: []TerminalSemanticOp{
			screenBufferWriteOp(0, 0, "abc"),
			screenBufferControlOp("cr"),
			screenBufferWriteOp(0, 0, "X"),
			screenBufferControlOp("bs"),
			screenBufferControlOp("tab"),
		},
	}); err != nil {
		t.Fatalf("apply cursor controls: %v", err)
	}
	visible := buffer.VisibleRows()
	if got := visible[0].Text(); got != "Xbc" {
		t.Fatalf("CR rewrite should mutate current physical row, got %q row=%#v", got, visible[0])
	}
	if buffer.Cursor.X != 8 {
		t.Fatalf("TAB after BS should move cursor to next tab stop, cursor=%#v", buffer.Cursor)
	}
}

func TestR420ScreenBufferTransactionSeqIsIdempotent(t *testing.T) {
	buffer := NewScreenHistoryBuffer(12, 1)
	tx := TerminalSemanticTransaction{
		Seq:  7,
		Size: TerminalSemanticSize{Cols: 12, Rows: 1},
		Ops: []TerminalSemanticOp{
			screenBufferWriteOp(0, 0, "once"),
			screenBufferControlOp("lf"),
		},
	}
	if err := buffer.ApplyTransaction(tx); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	if err := buffer.ApplyTransaction(tx); err != nil {
		t.Fatalf("second apply should be no-op, got error: %v", err)
	}
	committed := buffer.CommittedRows()
	if len(committed) != 1 || committed[0].Text() != "once" {
		t.Fatalf("duplicate seq must not add committed rows, committed=%#v", committed)
	}
	if buffer.AppliedSeq != 7 {
		t.Fatalf("AppliedSeq should remain at applied tx seq, got %d", buffer.AppliedSeq)
	}
}

func screenBufferWriteOp(row int, col int, text string) TerminalSemanticOp {
	cells := make([]TerminalSemanticCell, 0, len(text))
	for _, r := range text {
		cells = append(cells, TerminalSemanticCell{Content: string(r), Width: 1})
	}
	return TerminalSemanticOp{Code: vterm.ScreenOpWriteSpan, Row: row, Col: col, Cells: cells}
}

func screenBufferControlOp(kind string) TerminalSemanticOp {
	return TerminalSemanticOp{Code: vterm.ScreenOpControl, Control: kind}
}
