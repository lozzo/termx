package history

import (
	"strconv"
	"strings"
	"testing"

	vterm "github.com/lozzow/termx/termx-vterm/vterm"
)

func TestR320OrderedSemanticEventsCoverMatrixInputs(t *testing.T) {
	tx := TerminalSemanticTransaction{
		Seq:  42,
		Size: TerminalSemanticSize{Cols: 80, Rows: 24},
		Ops: []TerminalSemanticOp{
			{Code: vterm.ScreenOpWriteSpan, Row: 2, Col: 3, Cells: []TerminalSemanticCell{semanticCell("a"), semanticCell("b")}, Wrapped: true, WrappedSet: true},
			{Code: vterm.ScreenOpControl, Control: "cr", Row: 2, Col: 0},
			{Code: vterm.ScreenOpControl, Control: "cup", Row: 5, Col: 7},
			{Code: vterm.ScreenOpControl, Control: "el", Row: 5, Col: 7, Mode: 0},
			{Code: vterm.ScreenOpControl, Control: "ech", Row: 5, Col: 7, Mode: 2},
			{Code: vterm.ScreenOpControl, Control: "dch", Row: 5, Col: 7, Mode: 2},
			{Code: vterm.ScreenOpControl, Control: "ich", Row: 5, Col: 7, Mode: 2},
			{Code: vterm.ScreenOpControl, Control: "ed", Row: 0, Col: 0, Mode: 2, ScrollOut: []vterm.ScrollbackRowAppend{{Runs: []vterm.CellRun{{Text: "clear-old"}}}}},
			{Code: vterm.ScreenOpScrollRect, Rect: vterm.DamageRect{X: 0, Y: 1, Width: 80, Height: 10}, Dy: -1},
			{Code: vterm.ScreenOpCopyRect, Src: vterm.DamageRect{X: 0, Y: 0, Width: 6, Height: 1}, DstX: 0, DstY: 2},
			{Code: vterm.ScreenOpModes, Private: true, Mode: 1049, Enabled: true},
			{Code: vterm.ScreenOpModes, Private: true, Mode: 1049, Enabled: false},
			{Code: vterm.ScreenOpResize, Size: vterm.Size{Cols: 100, Rows: 30}},
		},
		PrimaryScrollOut: []TerminalSemanticScrollOut{{
			Runs:       []TerminalSemanticCellRun{{Text: "gone"}},
			Wrapped:    true,
			WrappedSet: true,
		}},
		PrimaryFrame:        &TerminalSemanticFrame{Cols: 80, Rows: [][]TerminalSemanticCell{{semanticCell("p")}}},
		AltFrame:            &TerminalSemanticFrame{Cols: 80, Rows: [][]TerminalSemanticCell{{semanticCell("a")}}},
		AltEntered:          true,
		AltExited:           true,
		RequiresFullReplace: true,
		FullReplaceReason:   "resize",
	}

	events := HistorySemanticEventsFromTransaction(tx)
	assertSequentialEventEnvelope(t, events, tx.Seq, tx.Size)

	got := eventLabels(events)
	want := []string{
		"write:2:3:ab",
		"control:cr",
		"control:cup",
		"control:el",
		"control:ech:2",
		"control:dch:2",
		"control:ich:2",
		"scroll-out:clear-old",
		"control:ed:2",
		"scroll-rect:0,1:80x10:-1",
		"copy-rect:0,0:6x1->0,2",
		"alt-enter:1049",
		"alt-exit:1049",
		"resize-op:100x30",
		"primary-frame:p",
		"alt-frame:a",
		"full-replace:resize",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("ordered semantic event coverage mismatch\ngot  %v\nwant %v", got, want)
	}

	for _, event := range events[:14] {
		if event.OrderSource != HistorySemanticEventOrderFromOps {
			t.Fatalf("op-derived event must keep vterm op order source, got %#v", event)
		}
	}
	for _, event := range events[14:] {
		if event.OrderSource != HistorySemanticEventOrderFromTransactionSideProof {
			t.Fatalf("side proof must be marked transaction scoped until vterm exposes exact proof order, got %#v", event)
		}
	}
}

func TestR320TransactionFlagsBecomeSideProofWhenOpsDoNotCarryBoundary(t *testing.T) {
	tx := TerminalSemanticTransaction{
		Seq:                 7,
		Size:                TerminalSemanticSize{Cols: 120, Rows: 40},
		AltEntered:          true,
		AltExited:           true,
		RequiresFullReplace: true,
		FullReplaceReason:   "resize",
	}

	events := HistorySemanticEventsFromTransaction(tx)
	got := eventLabels(events)
	want := []string{
		"alt-enter",
		"alt-exit",
		"resize:resize",
		"full-replace:resize",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("transaction-level boundary flags must enter event chain, got %v want %v", got, want)
	}
	for _, event := range events {
		if event.OrderSource != HistorySemanticEventOrderFromTransactionSideProof {
			t.Fatalf("flag-derived event cannot claim op-level order, got %#v", event)
		}
	}
}

func TestR320SemanticEventsCloneTransactionPayload(t *testing.T) {
	tx := TerminalSemanticTransaction{
		Seq: 1,
		Ops: []TerminalSemanticOp{{
			Code:  vterm.ScreenOpWriteSpan,
			Cells: []TerminalSemanticCell{semanticCell("x")},
		}},
		PrimaryScrollOut: []TerminalSemanticScrollOut{{
			Cells: []TerminalSemanticCell{semanticCell("s")},
			Runs:  []TerminalSemanticCellRun{{Text: "scroll"}},
		}},
		PrimaryFrame: &TerminalSemanticFrame{Rows: [][]TerminalSemanticCell{{semanticCell("p")}}, Cols: 10},
	}
	events := HistorySemanticEventsFromTransaction(tx)

	tx.Ops[0].Cells[0].Content = "mutated"
	tx.PrimaryScrollOut[0].Cells[0].Content = "mutated"
	tx.PrimaryScrollOut[0].Runs[0].Text = "mutated"
	tx.PrimaryFrame.Rows[0][0].Content = "mutated"

	got := eventLabels(events)
	want := []string{"write:0:0:x", "scroll-out:scroll", "primary-frame:p"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("event chain must own a stable copy of transaction payload, got %v want %v", got, want)
	}
}

func TestR320RealVTermTransactionFeedsOrderedEventContract(t *testing.T) {
	source := vterm.NewSemanticSource(10, 3, 100, nil)
	tx, err := source.ApplyPTYWrite([]byte("abc\rX\x1b[2;4HZ\x1b[2Ktail\nnext"))
	if err != nil {
		t.Fatalf("apply pty write: %v", err)
	}
	events := HistorySemanticEventsFromTransaction(tx)
	got := eventLabels(events)
	for _, want := range []string{"write:0:0:abc", "control:cr", "write:0:0:X", "control:cup", "write:1:3:Z", "control:el:2"} {
		if !containsLabel(got, want) {
			t.Fatalf("real vterm transaction did not expose %q through ordered events: %v", want, got)
		}
	}

	altTx, err := source.ApplyPTYWrite([]byte("\x1b[?1049h\x1b[2Jalt\x1b[?1049l"))
	if err != nil {
		t.Fatalf("apply alt pty write: %v", err)
	}
	altEvents := HistorySemanticEventsFromTransaction(altTx)
	altLabels := eventLabels(altEvents)
	if !containsLabel(altLabels, "alt-enter:1049") || !containsLabel(altLabels, "alt-exit:1049") {
		t.Fatalf("real vterm alt boundaries must enter ordered events, got %v", altLabels)
	}
	for _, event := range altEvents {
		if (event.Kind == HistorySemanticEventAltEnter || event.Kind == HistorySemanticEventAltExit) && event.OrderSource != HistorySemanticEventOrderFromOps {
			t.Fatalf("real vterm mode ops must preserve op-level order source, got %#v", event)
		}
	}

	resizeTx, err := source.Resize(vterm.TerminalSemanticSize{Cols: 12, Rows: 4})
	if err != nil {
		t.Fatalf("resize semantic source: %v", err)
	}
	resizeLabels := eventLabels(HistorySemanticEventsFromTransaction(resizeTx))
	if !containsLabel(resizeLabels, "resize:resize") || !containsLabel(resizeLabels, "full-replace:resize") {
		t.Fatalf("real vterm resize must enter event chain as non-history boundary, got %v", resizeLabels)
	}
}

func TestR328RealVTermED2ScrollOutIsOrderedBeforeRedraw(t *testing.T) {
	source := vterm.NewSemanticSource(20, 4, 0, nil)
	if _, err := source.ApplyPTYWrite([]byte("old-a\r\nold-b")); err != nil {
		t.Fatalf("seed vterm: %v", err)
	}
	tx, err := source.ApplyPTYWrite([]byte("\x1b[H\x1b[2Jnew-a\r\nnew-b"))
	if err != nil {
		t.Fatalf("apply ED2 redraw: %v", err)
	}
	labels := eventLabels(HistorySemanticEventsFromTransaction(tx))
	want := []string{
		"control:cup",
		"scroll-out:old-a",
		"scroll-out:old-b",
		"control:ed:2",
		"write:0:0:new-a",
		"control:cr",
		"control:lf",
		"write:1:0:new-b",
	}
	for _, label := range want {
		if !containsLabel(labels, label) {
			t.Fatalf("ED2 clear/redraw missing ordered label %q, got %v", label, labels)
		}
	}
	if strings.Join(labels[:len(want)], "|") != strings.Join(want, "|") {
		t.Fatalf("ED2 scroll-out proof must stay before redraw ops\ngot  %v\nwant prefix %v", labels, want)
	}
}

func assertSequentialEventEnvelope(t *testing.T, events []HistorySemanticEvent, seq uint64, size TerminalSemanticSize) {
	t.Helper()
	for i, event := range events {
		if event.Seq != seq || event.Order != i || event.Size != size {
			t.Fatalf("event %d lost transaction envelope: %#v", i, event)
		}
	}
}

func eventLabels(events []HistorySemanticEvent) []string {
	out := make([]string, 0, len(events))
	for _, event := range events {
		switch event.Kind {
		case HistorySemanticEventOp:
			out = append(out, opLabel(event.Op))
		case HistorySemanticEventAltEnter:
			out = append(out, modeBoundaryLabel("alt-enter", event.Op))
		case HistorySemanticEventAltExit:
			out = append(out, modeBoundaryLabel("alt-exit", event.Op))
		case HistorySemanticEventResize:
			if event.Op != nil && event.Op.Code == vterm.ScreenOpResize {
				out = append(out, "resize-op:"+sizeLabel(event.Op.Size))
			} else {
				out = append(out, "resize:"+event.Reason)
			}
		case HistorySemanticEventPrimaryScrollOut:
			out = append(out, "scroll-out:"+semanticScrollOutText(event.ScrollOut))
		case HistorySemanticEventPrimaryFrame:
			out = append(out, "primary-frame:"+semanticFrameText(event.Frame))
		case HistorySemanticEventAltFrame:
			out = append(out, "alt-frame:"+semanticFrameText(event.Frame))
		case HistorySemanticEventFullReplace:
			out = append(out, "full-replace:"+event.Reason)
		case HistorySemanticEventClearScrollback:
			out = append(out, "clear-scrollback:"+event.Reason)
		default:
			out = append(out, string(event.Kind))
		}
	}
	return out
}

func containsLabel(labels []string, want string) bool {
	for _, label := range labels {
		if label == want {
			return true
		}
	}
	return false
}

func opLabel(op *TerminalSemanticOp) string {
	if op == nil {
		return "<nil-op>"
	}
	switch op.Code {
	case vterm.ScreenOpWriteSpan:
		return "write:" + intString(op.Row) + ":" + intString(op.Col) + ":" + semanticCellsText(op.Cells)
	case vterm.ScreenOpControl:
		if op.Mode != 0 {
			return "control:" + op.Control + ":" + intString(op.Mode)
		}
		return "control:" + op.Control
	case vterm.ScreenOpScrollRect:
		return "scroll-rect:" + rectLabel(op.Rect) + ":" + intString(op.Dy)
	case vterm.ScreenOpCopyRect:
		return "copy-rect:" + rectLabel(op.Src) + "->" + intString(op.DstX) + "," + intString(op.DstY)
	case vterm.ScreenOpResize:
		return "resize-op:" + sizeLabel(op.Size)
	default:
		return "op"
	}
}

func modeBoundaryLabel(prefix string, op *TerminalSemanticOp) string {
	if op == nil {
		return prefix
	}
	return prefix + ":" + intString(op.Mode)
}

func rectLabel(rect vterm.DamageRect) string {
	return intString(rect.X) + "," + intString(rect.Y) + ":" + intString(rect.Width) + "x" + intString(rect.Height)
}

func sizeLabel(size vterm.Size) string {
	return intString(int(size.Cols)) + "x" + intString(int(size.Rows))
}

func semanticFrameText(frame *TerminalSemanticFrame) string {
	if frame == nil || len(frame.Rows) == 0 {
		return ""
	}
	var out string
	for _, row := range frame.Rows {
		out += semanticCellsText(row)
	}
	return out
}

func semanticScrollOutText(proof *TerminalSemanticScrollOut) string {
	if proof == nil {
		return ""
	}
	if len(proof.Runs) > 0 {
		var out string
		for _, run := range proof.Runs {
			out += run.Text
		}
		return strings.TrimRight(out, " ")
	}
	return strings.TrimRight(semanticCellsText(proof.Cells), " ")
}

func semanticCellsText(cells []TerminalSemanticCell) string {
	var out string
	for _, cell := range cells {
		out += cell.Content
	}
	return out
}

func semanticCell(content string) TerminalSemanticCell {
	return TerminalSemanticCell{Content: content, Width: 1}
}

func intString(value int) string {
	return strconv.Itoa(value)
}
