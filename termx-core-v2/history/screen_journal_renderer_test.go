package history

import "testing"

func TestR426ScreenBackedJournalRendererIsolatesLegacyBodyMutations(t *testing.T) {
	buffer := NewScreenHistoryBuffer(20, 2)
	renderer := NewScreenBackedHistoryJournalRenderer(buffer)
	journal := HistoryJournal{
		TerminalID: "term-r426-screen-journal",
		Seq:        1,
		Size:       TerminalSemanticSize{Cols: 20, Rows: 2},
		Source:     HistoryJournalSourceSemanticTapTransaction,
		Items: []HistoryJournalItem{
			{Kind: HistoryJournalItemOrdinaryLineBatch, Ordinary: &OrdinaryLineBatch{
				Lines: []JournalLogicalLine{{Cells: []Cell{{Text: "legacy", Width: 1}}}},
			}},
			{Kind: HistoryJournalItemFrameEvent, Frame: &HistoryJournalFrameEvent{
				Kind:        HistoryJournalFrameReplacePrimary,
				Frame:       &TerminalSemanticFrame{Cols: 20, Rows: [][]TerminalSemanticCell{historyCellsForJournalTest("screen truth")}},
				TouchedRows: []int{0},
			}},
		},
	}

	batch, err := renderer.ApplyJournal(journal)
	if err != nil {
		t.Fatalf("apply screen-backed journal: %v", err)
	}
	if len(batch.Mutations) != 0 {
		t.Fatalf("screen-backed journal renderer must not emit legacy body mutations, batch=%#v", batch)
	}
	projection := buffer.ProjectHistory()
	if got := r425HistoryTexts(projection.HistoryRows()); got != "screen truth" {
		t.Fatalf("screen-backed journal should expose only physical screen truth, got %q projection=%#v", got, projection)
	}
}

func TestR426ScreenBackedJournalRendererSealsPrimaryWithoutFrameMutations(t *testing.T) {
	buffer := NewScreenHistoryBuffer(24, 2)
	renderer := NewScreenBackedHistoryJournalRenderer(buffer)
	replace := HistoryJournal{
		TerminalID: "term-r426-close",
		Seq:        1,
		Size:       TerminalSemanticSize{Cols: 24, Rows: 2},
		Source:     HistoryJournalSourceSemanticTapTransaction,
		Items: []HistoryJournalItem{{Kind: HistoryJournalItemFrameEvent, Frame: &HistoryJournalFrameEvent{
			Kind:        HistoryJournalFrameReplacePrimary,
			Frame:       &TerminalSemanticFrame{Cols: 24, Rows: [][]TerminalSemanticCell{historyCellsForJournalTest("final frame")}},
			TouchedRows: []int{0},
		}}},
	}
	if _, err := renderer.ApplyJournal(replace); err != nil {
		t.Fatalf("replace primary: %v", err)
	}
	closeJournal := HistoryJournal{
		TerminalID: "term-r426-close",
		Seq:        2,
		Size:       TerminalSemanticSize{Cols: 24, Rows: 2},
		Source:     HistoryJournalSourceSemanticTapTransaction,
		Items:      []HistoryJournalItem{{Kind: HistoryJournalItemFrameEvent, Frame: &HistoryJournalFrameEvent{Kind: HistoryJournalFrameClosePrimary}}},
	}
	batch, err := renderer.ApplyJournal(closeJournal)
	if err != nil {
		t.Fatalf("close primary: %v", err)
	}
	if len(batch.Mutations) != 0 {
		t.Fatalf("close should seal screen buffer rows without frame mutations, batch=%#v", batch)
	}
	projection := buffer.ProjectHistory()
	if got := r425HistoryTexts(projection.HistoryRows()); got != "final frame" {
		t.Fatalf("closed primary frame should be sealed physical history, got %q projection=%#v", got, projection)
	}
	if len(projection.Rows) != 1 || !projection.Rows[0].Sealed || projection.Rows[0].Segment != HistorySegmentCommitted {
		t.Fatalf("closed primary row must become committed physical projection, projection=%#v", projection)
	}
}

func TestR426ScreenBackedJournalRendererKeepsAltTransientOutOfPrimaryHistory(t *testing.T) {
	buffer := NewScreenHistoryBuffer(16, 2)
	renderer := NewScreenBackedHistoryJournalRenderer(buffer)
	alt := HistoryJournal{
		TerminalID: "term-r426-alt",
		Seq:        1,
		Size:       TerminalSemanticSize{Cols: 16, Rows: 2},
		Source:     HistoryJournalSourceSemanticTapTransaction,
		Items: []HistoryJournalItem{
			{Kind: HistoryJournalItemBoundary, Boundary: &HistoryJournalBoundary{Kind: HistoryJournalBoundaryAltEnter}},
			{Kind: HistoryJournalItemFrameEvent, Frame: &HistoryJournalFrameEvent{
				Kind:  HistoryJournalFrameReplaceAlt,
				Frame: &TerminalSemanticFrame{Cols: 16, Rows: [][]TerminalSemanticCell{historyCellsForJournalTest("alt")}},
			}},
		},
	}
	if _, err := renderer.ApplyJournal(alt); err != nil {
		t.Fatalf("apply alt journal: %v", err)
	}
	activeAlt := buffer.ProjectHistory()
	if got := r425HistoryTexts(activeAlt.HistoryRows()); got != "alt" {
		t.Fatalf("active alt should be selectable transient projection, got %q projection=%#v", got, activeAlt)
	}
	exit := HistoryJournal{
		TerminalID: "term-r426-alt",
		Seq:        2,
		Size:       TerminalSemanticSize{Cols: 16, Rows: 2},
		Source:     HistoryJournalSourceSemanticTapTransaction,
		Items: []HistoryJournalItem{
			{Kind: HistoryJournalItemBoundary, Boundary: &HistoryJournalBoundary{Kind: HistoryJournalBoundaryAltExit}},
			{Kind: HistoryJournalItemFrameEvent, Frame: &HistoryJournalFrameEvent{Kind: HistoryJournalFrameClearAlt}},
		},
	}
	if _, err := renderer.ApplyJournal(exit); err != nil {
		t.Fatalf("apply alt exit journal: %v", err)
	}
	if rows := buffer.ProjectHistory().HistoryRows(); len(rows) != 0 {
		t.Fatalf("alt exit must not commit transient alt rows to primary history, rows=%#v", rows)
	}
}
