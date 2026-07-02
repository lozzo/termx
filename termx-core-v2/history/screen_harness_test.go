package history

import "testing"

func TestR423ScreenBufferSoftWrapProjectsAsOneLogicalLine(t *testing.T) {
	buffer := NewScreenHistoryBuffer(4, 2)
	if err := buffer.ApplyTransaction(TerminalSemanticTransaction{
		Seq: 1,
		Ops: []TerminalSemanticOp{
			screenBufferWriteOp(0, 0, "abcd"),
			screenBufferControlAt("soft-wrap", 0, 4, 0),
			screenBufferWriteOp(1, 0, "e"),
		},
	}); err != nil {
		t.Fatalf("apply soft-wrap transaction: %v", err)
	}
	rows := buffer.VisibleRows()
	if !rows[0].Wrapped || !rows[1].Continued {
		t.Fatalf("soft-wrap must mark physical row continuation, rows=%#v", rows)
	}
	projection := buffer.ProjectHistory()
	if len(projection.Lines) != 1 || rowText(projection.Lines[0].Cells) != "abcde" {
		t.Fatalf("soft-wrapped physical rows should project to one logical line, projection=%#v", projection)
	}
}
