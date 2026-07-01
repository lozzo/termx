package history

import (
	"strings"
	"testing"

	vterm "github.com/lozzow/termx/termx-vterm/vterm"
)

func TestR380HistoryJournalCoverageMatrixFromSemanticTransaction(t *testing.T) {
	tx := TerminalSemanticTransaction{
		Seq:  380,
		Raw:  "raw must not be journal input",
		Size: TerminalSemanticSize{Cols: 80, Rows: 24},
		Ops: []TerminalSemanticOp{
			{Code: vterm.ScreenOpWriteSpan, Row: 0, Col: 0, Cells: []TerminalSemanticCell{
				{Content: "o", Width: 1, Style: TerminalSemanticStyle{FG: "ansi:1", Bold: true}},
				{Content: "k", Width: 1, Style: TerminalSemanticStyle{BG: "#102030"}},
			}},
			{Code: vterm.ScreenOpControl, Control: "lf", Row: 0, Col: 2},
			{Code: vterm.ScreenOpWriteSpan, Row: 1, Col: 0, Cells: []TerminalSemanticCell{{Content: "中", Width: 2}}},
			{Code: vterm.ScreenOpControl, Control: "ed", Mode: 2, ScrollOut: []vterm.ScrollbackRowAppend{{Runs: []vterm.CellRun{{Text: "clear-old"}}}}},
			{Code: vterm.ScreenOpControl, Control: "ed", Mode: 3},
			{Code: vterm.ScreenOpControl, Control: "ris"},
			{Code: vterm.ScreenOpModes, Private: true, Mode: 2026, Enabled: true},
			{Code: vterm.ScreenOpModes, Private: true, Mode: 2026, Enabled: false},
			{Code: vterm.ScreenOpModes, Private: true, Mode: 1049, Enabled: true},
			{Code: vterm.ScreenOpModes, Private: true, Mode: 1049, Enabled: false},
			{Code: vterm.ScreenOpResize, Size: vterm.Size{Cols: 100, Rows: 30}},
		},
		PrimaryScrollOut: []TerminalSemanticScrollOut{{
			Runs:       []TerminalSemanticCellRun{{Text: "payload-gone"}},
			Wrapped:    true,
			WrappedSet: true,
		}},
		PrimaryFrame:            &TerminalSemanticFrame{Cols: 80, Rows: [][]TerminalSemanticCell{{semanticCell("p")}}},
		PrimaryFrameTouchedRows: []int{0},
		AltFrame:                &TerminalSemanticFrame{Cols: 80, Rows: [][]TerminalSemanticCell{{semanticCell("a")}}},
		AltEntered:              true,
		AltExited:               true,
		SourceDamage: vterm.WriteDamage{
			Ops: []vterm.DamageOp{{Code: vterm.ScreenOpWriteSpan, Cells: []vterm.Cell{{Content: "source-damage-must-not-appear", Width: 1}}}},
		},
	}

	journal := HistoryJournalFromTransaction("term-r380", tx)
	if journal.TerminalID != "term-r380" || journal.Seq != tx.Seq || journal.Size != tx.Size {
		t.Fatalf("journal lost transaction envelope: %#v", journal)
	}
	if journal.Source != HistoryJournalSourceSemanticTapTransaction {
		t.Fatalf("journal source must stay behind history semantic transaction boundary, got %q", journal.Source)
	}
	assertJournalOrders(t, journal)

	ordinary := journalOrdinaryBatches(journal)
	if len(ordinary) != 1 {
		t.Fatalf("ordinary writes before boundary should become compact line/open batches, got %#v labels=%v", ordinary, journalLabels(journal))
	}
	if got := journalLineText(ordinary[0].Lines[0]); got != "ok" {
		t.Fatalf("sealed ordinary line batch lost text, got %q batch=%#v", got, ordinary[0])
	}
	if ordinary[0].Lines[0].Cells[0].Style.FG != "ansi:1" || !ordinary[0].Lines[0].Cells[0].Style.Bold || ordinary[0].Lines[0].Cells[1].Style.BG != "#102030" {
		t.Fatalf("ordinary batch must preserve SGR semantic attrs, got %#v", ordinary[0].Lines[0].Cells)
	}
	if ordinary[0].OpenUpdate == nil || journalCellsText(ordinary[0].OpenUpdate.Cells) != "中" || ordinary[0].OpenUpdate.CursorCol != 2 {
		t.Fatalf("CJK open line update should preserve width/cursor command, got %#v", ordinary[0])
	}

	for _, want := range []string{
		"batch:ok",
		"batch:open=中",
		"scroll-out:clear-old",
		"boundary:ed2",
		"frame:clear-primary",
		"boundary:ed3",
		"boundary:ris",
		"frame:clear-primary",
		"frame:clear-alt",
		"boundary:sync-begin",
		"boundary:sync-end",
		"boundary:alt-enter",
		"frame:archive-primary",
		"boundary:alt-exit",
		"frame:clear-alt",
		"boundary:resize",
		"scroll-out:payload-gone",
		"frame:replace-primary:p",
		"frame:replace-alt:a",
	} {
		if !containsLabel(journalLabels(journal), want) {
			t.Fatalf("journal coverage missing %q labels=%v journal=%#v", want, journalLabels(journal), journal)
		}
	}
	for _, label := range journalLabels(journal) {
		if strings.Contains(label, "source-damage-must-not-appear") || strings.Contains(label, "raw must not") {
			t.Fatalf("journal must not consume raw PTY or SourceDamage payload, labels=%v", journalLabels(journal))
		}
	}
}

func TestR380HistoryJournalDeepCopiesSemanticPayload(t *testing.T) {
	tx := TerminalSemanticTransaction{
		Seq:  1,
		Size: TerminalSemanticSize{Cols: 12, Rows: 3},
		Ops: []TerminalSemanticOp{
			{Code: vterm.ScreenOpWriteSpan, Cells: []TerminalSemanticCell{{Content: "x", Width: 1}}},
			{Code: vterm.ScreenOpControl, Control: "lf"},
		},
		PrimaryScrollOut: []TerminalSemanticScrollOut{{Runs: []TerminalSemanticCellRun{{Text: "gone"}}}},
		PrimaryFrame:     &TerminalSemanticFrame{Cols: 12, Rows: [][]TerminalSemanticCell{{semanticCell("p")}}},
		AltFrame:         &TerminalSemanticFrame{Cols: 12, Rows: [][]TerminalSemanticCell{{semanticCell("a")}}},
	}
	journal := HistoryJournalFromTransaction("term-r380", tx)

	tx.Ops[0].Cells[0].Content = "mutated"
	tx.PrimaryScrollOut[0].Runs[0].Text = "mutated"
	tx.PrimaryFrame.Rows[0][0].Content = "mutated"
	tx.AltFrame.Rows[0][0].Content = "mutated"

	labels := journalLabels(journal)
	for _, want := range []string{"batch:x", "scroll-out:gone", "frame:replace-primary:p", "frame:replace-alt:a"} {
		if !containsLabel(labels, want) {
			t.Fatalf("journal payload must be history-owned copy, missing %q labels=%v", want, labels)
		}
	}
	for _, label := range labels {
		if strings.Contains(label, "mutated") {
			t.Fatalf("journal leaked mutable transaction payload, labels=%v", labels)
		}
	}
}

func TestR380RealVTermJournalComesFromSingleSemanticPass(t *testing.T) {
	source := vterm.NewSemanticSource(16, 4, 0, nil)
	tx, err := source.ApplyPTYWrite([]byte("old-a\r\nold-b\x1b[?2026h\x1b[2J\x1b[Hnew-a\r\nnew-b\x1b[?2026l"))
	if err != nil {
		t.Fatalf("apply real semantic transaction: %v", err)
	}
	journal := HistoryJournalFromTransaction("term-r380", tx)
	labels := journalLabels(journal)

	for _, want := range []string{
		"boundary:sync-begin",
		"scroll-out:old-a",
		"boundary:ed2",
		"boundary:sync-end",
	} {
		if !containsLabel(labels, want) {
			t.Fatalf("real vterm synchronized transaction missing journal label %q labels=%v tx=%#v", want, labels, tx)
		}
	}

	altTx, err := source.ApplyPTYWrite([]byte("\x1b[?1049halt"))
	if err != nil {
		t.Fatalf("apply real alt semantic transaction: %v", err)
	}
	altLabels := journalLabels(HistoryJournalFromTransaction("term-r380", altTx))
	for _, want := range []string{
		"boundary:alt-enter",
		"frame:replace-alt:alt",
	} {
		if !containsLabel(altLabels, want) {
			t.Fatalf("real vterm alt transaction missing journal label %q labels=%v tx=%#v", want, altLabels, altTx)
		}
	}

	altExitTx, err := source.ApplyPTYWrite([]byte("\x1b[?1049l"))
	if err != nil {
		t.Fatalf("apply real alt exit semantic transaction: %v", err)
	}
	altExitLabels := journalLabels(HistoryJournalFromTransaction("term-r380", altExitTx))
	for _, want := range []string{
		"boundary:alt-exit",
	} {
		if !containsLabel(altExitLabels, want) {
			t.Fatalf("real vterm alt exit transaction missing journal label %q labels=%v tx=%#v", want, altExitLabels, altExitTx)
		}
	}
	if journal.Source != HistoryJournalSourceSemanticTapTransaction {
		t.Fatalf("real journal must declare semantic tap transaction source, got %#v", journal)
	}
	for _, item := range journal.Items {
		if item.OrderSource == "" {
			t.Fatalf("journal item must preserve event order source, item=%#v labels=%v", item, labels)
		}
	}
}

func TestR380ResizeOnlyJournalIsBoundaryNotFrameSnapshot(t *testing.T) {
	source := vterm.NewSemanticSource(8, 2, 0, nil)
	if _, err := source.ApplyPTYWrite([]byte("line-a\r\nline-b")); err != nil {
		t.Fatalf("seed source: %v", err)
	}
	tx, err := source.Resize(vterm.TerminalSemanticSize{Cols: 10, Rows: 3})
	if err != nil {
		t.Fatalf("resize semantic source: %v", err)
	}
	journal := HistoryJournalFromTransaction("term-r380", tx)
	labels := journalLabels(journal)
	if !containsLabel(labels, "boundary:resize") {
		t.Fatalf("resize must become journal boundary, labels=%v tx=%#v", labels, tx)
	}
	for _, label := range labels {
		if strings.HasPrefix(label, "batch:") || strings.HasPrefix(label, "frame:replace-primary") {
			t.Fatalf("resize journal must not create ordinary history or frame from live snapshot, labels=%v tx=%#v", labels, tx)
		}
	}
}

func journalOrdinaryBatches(journal HistoryJournal) []OrdinaryLineBatch {
	var out []OrdinaryLineBatch
	for _, item := range journal.Items {
		if item.Kind == HistoryJournalItemOrdinaryLineBatch && item.Ordinary != nil {
			out = append(out, *item.Ordinary)
		}
	}
	return out
}

func journalLabels(journal HistoryJournal) []string {
	out := make([]string, 0, len(journal.Items))
	for _, item := range journal.Items {
		switch item.Kind {
		case HistoryJournalItemOrdinaryLineBatch:
			if item.Ordinary == nil {
				out = append(out, "batch:<nil>")
				continue
			}
			if len(item.Ordinary.Lines) == 0 && item.Ordinary.OpenUpdate != nil {
				out = append(out, "batch:open="+journalCellsText(item.Ordinary.OpenUpdate.Cells))
				continue
			}
			for _, line := range item.Ordinary.Lines {
				out = append(out, "batch:"+journalLineText(line))
			}
			if item.Ordinary.OpenUpdate != nil {
				out = append(out, "batch:open="+journalCellsText(item.Ordinary.OpenUpdate.Cells))
			}
		case HistoryJournalItemBoundary:
			if item.Boundary == nil {
				out = append(out, "boundary:<nil>")
				continue
			}
			out = append(out, "boundary:"+string(item.Boundary.Kind))
		case HistoryJournalItemScrollOutProof:
			if item.ScrollOut == nil {
				out = append(out, "scroll-out:<nil>")
				continue
			}
			for _, row := range item.ScrollOut.Rows {
				rowCopy := row
				out = append(out, "scroll-out:"+semanticScrollOutText(&rowCopy))
			}
		case HistoryJournalItemFrameEvent:
			if item.Frame == nil {
				out = append(out, "frame:<nil>")
				continue
			}
			label := "frame:" + string(item.Frame.Kind)
			if item.Frame.Frame != nil {
				label += ":" + strings.TrimRight(semanticFrameText(item.Frame.Frame), " ")
			}
			out = append(out, label)
		}
	}
	return out
}

func assertJournalOrders(t *testing.T, journal HistoryJournal) {
	t.Helper()
	for index, item := range journal.Items {
		if item.Order != index {
			t.Fatalf("journal item order must be dense and stable, index=%d item=%#v journal=%#v", index, item, journal)
		}
		if item.OrderSource == "" {
			t.Fatalf("journal item must preserve semantic order source, item=%#v", item)
		}
	}
}

func journalLineText(line JournalLogicalLine) string {
	return journalCellsText(line.Cells)
}

func journalCellsText(cells []Cell) string {
	var out string
	for _, cell := range cells {
		out += cell.Text
	}
	return out
}
