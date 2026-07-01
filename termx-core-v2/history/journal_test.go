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

func TestR407EditedOrdinaryJournalKeepsCommandReplay(t *testing.T) {
	journal := HistoryJournalFromTransaction("term-r407-edited", TerminalSemanticTransaction{
		Seq:  1,
		Size: TerminalSemanticSize{Cols: 20, Rows: 4},
		Ops: []TerminalSemanticOp{
			{Code: vterm.ScreenOpWriteSpan, Row: 0, Col: 0, Cells: []TerminalSemanticCell{{Content: "a", Width: 1}, {Content: "b", Width: 1}}},
			{Code: vterm.ScreenOpControl, Control: "bs"},
			{Code: vterm.ScreenOpWriteSpan, Row: 0, Col: 1, Cells: []TerminalSemanticCell{{Content: "X", Width: 1}}},
		},
	})
	batches := journalOrdinaryBatches(journal)
	if len(batches) != 1 {
		t.Fatalf("expected one edited ordinary batch, got %#v labels=%v", batches, journalLabels(journal))
	}
	if len(batches[0].Commands) == 0 {
		t.Fatalf("edited ordinary journal must keep command replay, batch=%#v", batches[0])
	}
	if batches[0].OpenUpdate == nil || journalCellsText(batches[0].OpenUpdate.Cells) != "aX" {
		t.Fatalf("edited ordinary journal should still expose current open update, batch=%#v", batches[0])
	}
}

func TestR407OrdinaryJournalWriteSpanRunsKeepInlineSpaces(t *testing.T) {
	journal := HistoryJournalFromTransaction("term-r407-run-space", TerminalSemanticTransaction{
		Seq:  1,
		Size: TerminalSemanticSize{Cols: 20, Rows: 4},
		Ops: []TerminalSemanticOp{
			{Code: vterm.ScreenOpWriteSpan, Row: 0, Col: 0, Runs: []TerminalSemanticCellRun{{Text: "000001"}}},
			{Code: vterm.ScreenOpWriteSpan, Row: 0, Col: 6, Runs: []TerminalSemanticCellRun{{Text: " "}}},
			{Code: vterm.ScreenOpWriteSpan, Row: 0, Col: 7, Runs: []TerminalSemanticCellRun{{Text: "[WARN]"}}},
			{Code: vterm.ScreenOpControl, Control: "cr", Row: 0, Col: 13},
			{Code: vterm.ScreenOpControl, Control: "lf", Row: 0, Col: 0},
		},
	})
	batches := journalOrdinaryBatches(journal)
	if len(batches) != 1 || len(batches[0].Lines) != 1 {
		t.Fatalf("inline styled runs should stay on direct line path, batches=%#v labels=%v", batches, journalLabels(journal))
	}
	if got := journalLineText(batches[0].Lines[0]); got != "000001 [WARN]" {
		t.Fatalf("WriteSpan run spaces are history text, got %q labels=%v", got, journalLabels(journal))
	}
	if len(batches[0].Lines[0].Runs) == 0 || len(batches[0].Lines[0].Cells) != 0 {
		t.Fatalf("linear WriteSpan runs should stay compact until projection, line=%#v", batches[0].Lines[0])
	}
	if len(batches[0].Commands) != 0 || batches[0].OpenUpdate != nil {
		t.Fatalf("space run must not desync cursor into command replay, batch=%#v", batches[0])
	}
}

func TestR407OrdinaryJournalSoftWrapStaysOneLogicalLine(t *testing.T) {
	journal := HistoryJournalFromTransaction("term-r407-wrap", TerminalSemanticTransaction{
		Seq:  1,
		Size: TerminalSemanticSize{Cols: 4, Rows: 4},
		Ops: []TerminalSemanticOp{
			{Code: vterm.ScreenOpWriteSpan, Row: 0, Col: 0, Cells: []TerminalSemanticCell{{Content: "a", Width: 1}, {Content: "b", Width: 1}, {Content: "c", Width: 1}, {Content: "d", Width: 1}}},
			{Code: vterm.ScreenOpControl, Control: "soft-wrap", Row: 0, Col: 4},
			{Code: vterm.ScreenOpWriteSpan, Row: 1, Col: 0, Cells: []TerminalSemanticCell{{Content: "e", Width: 1}, {Content: "f", Width: 1}}},
			{Code: vterm.ScreenOpControl, Control: "lf", Row: 1, Col: 2},
		},
	})
	batches := journalOrdinaryBatches(journal)
	if len(batches) != 1 || len(batches[0].Lines) != 1 {
		t.Fatalf("soft-wrapped stdout should seal one logical line, got %#v labels=%v", batches, journalLabels(journal))
	}
	if got := journalLineText(batches[0].Lines[0]); got != "abcdef" {
		t.Fatalf("soft-wrap must append visual row into logical line, got %q labels=%v", got, journalLabels(journal))
	}
	if batches[0].OpenUpdate != nil || len(batches[0].Commands) != 0 {
		t.Fatalf("linear soft-wrap line must stay on sealed-line fast path, batch=%#v", batches[0])
	}
}

func TestR407OrdinaryStreamIgnoresVisualScrollOutProof(t *testing.T) {
	journal := HistoryJournalFromTransaction("term-r407-ordinary-proof", TerminalSemanticTransaction{
		Seq:  1,
		Size: TerminalSemanticSize{Cols: 4, Rows: 2},
		Ops: []TerminalSemanticOp{
			{Code: vterm.ScreenOpWriteSpan, Row: 1, Col: 0, Cells: []TerminalSemanticCell{{Content: "a", Width: 1}, {Content: "b", Width: 1}, {Content: "c", Width: 1}, {Content: "d", Width: 1}}},
			{Code: vterm.ScreenOpControl, Control: "soft-wrap", Row: 1, Col: 4, ScrollOut: []vterm.ScrollbackRowAppend{{Runs: []vterm.CellRun{{Text: "abcd"}}}}},
			{Code: vterm.ScreenOpWriteSpan, Row: 1, Col: 0, Cells: []TerminalSemanticCell{{Content: "e", Width: 1}, {Content: "f", Width: 1}}},
			{Code: vterm.ScreenOpControl, Control: "lf", Row: 1, Col: 2},
		},
		PrimaryScrollOut: []TerminalSemanticScrollOut{{Runs: []TerminalSemanticCellRun{{Text: "abcd"}}}},
	})
	labels := journalLabels(journal)
	for _, label := range labels {
		if strings.HasPrefix(label, "scroll-out:") {
			t.Fatalf("ordinary stream must not turn visual scroll-out proof into history truth, labels=%v", labels)
		}
	}
	batches := journalOrdinaryBatches(journal)
	if len(batches) != 1 || len(batches[0].Lines) != 1 {
		t.Fatalf("ordinary wrapped line should be owned by ordered ops, batches=%#v labels=%v", batches, labels)
	}
	if got := journalLineText(batches[0].Lines[0]); got != "abcdef" {
		t.Fatalf("ordinary logical line should include wrapped continuation once, got %q labels=%v", got, labels)
	}
}

func TestR407RealVTermLongLineLFOnlyStaysOneLogicalLine(t *testing.T) {
	source := vterm.NewSemanticSource(20, 4, 0, nil)
	tx, err := source.ApplyPTYWrite([]byte("000007 " + strings.Repeat("payload-", 9) + "\n"))
	if err != nil {
		t.Fatalf("apply real long line: %v", err)
	}
	journal := HistoryJournalFromTransaction("term-r407-real-wrap", tx)
	batches := journalOrdinaryBatches(journal)
	var lines []JournalLogicalLine
	for _, batch := range batches {
		lines = append(lines, batch.Lines...)
	}
	if len(lines) != 1 {
		t.Fatalf("real vterm LF-only wrapped line should produce one logical line, got lines=%d labels=%v tx=%#v", len(lines), journalLabels(journal), tx)
	}
	text := journalLineText(lines[0])
	if !strings.HasPrefix(text, "000007 payload-") || strings.Count(text, "payload-") != 9 {
		t.Fatalf("wrapped line lost prefix or continuation text, got %q labels=%v", text, journalLabels(journal))
	}
	for index, cell := range lines[0].Cells {
		if cell.Width > 1 && isASCIIText(cell.Text) {
			t.Fatalf("history logical line must not store ASCII run as one wide Cell, index=%d cell=%#v text=%q", index, cell, text)
		}
	}
}

func TestR407RealVTermLongLineStoreProjectionDoesNotSplitWrappedFragments(t *testing.T) {
	source := vterm.NewSemanticSource(40, 6, 0, nil)
	renderer := NewHistoryJournalRenderer()
	store := NewInMemoryHistoryStore("term-r407-store-wrap")

	var raw strings.Builder
	for line := 0; line < 20; line++ {
		raw.WriteString("R407 ")
		raw.WriteString(strings.Repeat("segment-", 8))
		raw.WriteString(" id=")
		raw.WriteString(string(rune('A' + line)))
		raw.WriteString("\r\n")
	}
	tx, err := source.ApplyPTYWrite([]byte(raw.String()))
	if err != nil {
		t.Fatalf("apply real wrapped stress: %v", err)
	}
	journal := HistoryJournalFromTransaction("term-r407-store-wrap", tx)
	batch, err := renderer.ApplyJournal(journal)
	if err != nil {
		t.Fatalf("apply journal: %v labels=%v", err, journalLabels(journal))
	}
	if err := store.Apply(batch); err != nil {
		t.Fatalf("apply store: %v", err)
	}
	window, err := store.LatestWindow(HistoryWindowRequest{TerminalID: "term-r407-store-wrap", Mode: HistoryWindowModeLatest, Cols: 40, Limit: 25})
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	texts := rowTexts(window.Rows)
	if got := len(texts); got != 20 {
		t.Fatalf("store projection should keep one row per logical line, got %d rows=%#v labels=%v tx=%#v", got, texts, journalLabels(journal), tx)
	}
	for index, text := range texts {
		if !strings.HasPrefix(text, "R407 segment-") || !strings.Contains(text, " id=") {
			t.Fatalf("wrapped logical line projected as fragment at row %d: %q rows=%#v labels=%v", index, text, texts, journalLabels(journal))
		}
		for _, cell := range window.Rows[index].Cells {
			if cell.Width > 1 && isASCIIText(cell.Text) {
				t.Fatalf("store projection must keep history cells addressable, row=%d cell=%#v text=%q", index, cell, text)
			}
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
	if len(line.Runs) > 0 && len(line.Cells) == 0 {
		var out string
		for _, run := range line.Runs {
			out += run.Text
		}
		return out
	}
	return journalCellsText(line.Cells)
}

func journalCellsText(cells []Cell) string {
	var out string
	for _, cell := range cells {
		out += cell.Text
	}
	return out
}
