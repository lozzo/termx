package history

import (
	"fmt"
	"os"
	"strings"
	"testing"

	vterm "github.com/lozzow/termx/termx-vterm/vterm"
)

func TestR382JournalRendererBatchesOrdinaryLinesWithoutPerOpReducer(t *testing.T) {
	source := vterm.NewSemanticSource(120, 24, 0, nil)
	var raw strings.Builder
	for i := 0; i < 1000; i++ {
		raw.WriteString(fmt.Sprintf("line-%04d\r\n", i))
	}
	tx, err := source.ApplyPTYWrite([]byte(raw.String()))
	if err != nil {
		t.Fatalf("apply ordinary tx: %v", err)
	}
	journal := HistoryJournalFromTransaction("term-r382", tx)
	batches := journalOrdinaryBatches(journal)
	if len(batches) != 1 || len(batches[0].Lines) != 1000 {
		t.Fatalf("ordinary transaction should collapse to one line batch, batches=%d lines=%d labels=%v", len(batches), len(batches[0].Lines), journalLabels(journal))
	}

	renderer := NewHistoryJournalRenderer()
	batch, err := renderer.ApplyJournal(journal)
	if err != nil {
		t.Fatalf("apply ordinary journal: %v", err)
	}
	if len(batch.Mutations) != 2000 {
		t.Fatalf("1000 sealed lines should produce seal+timeline mutations only, got %d", len(batch.Mutations))
	}
	store := NewInMemoryHistoryStore("term-r382")
	if err := store.Apply(batch); err != nil {
		t.Fatalf("apply store batch: %v", err)
	}
	window, err := store.LatestWindow(HistoryWindowRequest{TerminalID: "term-r382", Mode: HistoryWindowModeLatest, Cols: 120, Limit: 3})
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	if got := strings.Join(rowTexts(window.Rows), "|"); got != "line-0997|line-0998|line-0999" {
		t.Fatalf("journal reducer should preserve latest ordinary history, got %q", got)
	}
}

func TestR407JournalRendererOrdinarySealPathDoesNotCloneOwnedCells(t *testing.T) {
	source, err := os.ReadFile("journal_renderer.go")
	if err != nil {
		t.Fatalf("read journal renderer source: %v", err)
	}
	sealJournalLine := sourceFunctionBody(t, string(source), "func (renderer *journalRenderer) sealJournalLine")
	for _, forbidden := range []string{"cloneHistoryCells", "trimTrailingBlankCells("} {
		if strings.Contains(sealJournalLine, forbidden) {
			t.Fatalf("ordinary journal sealed-line path must use queue-owned cells without extra clone: %s", forbidden)
		}
	}
	if !strings.Contains(sealJournalLine, "trimTrailingBlankCellsInPlace") {
		t.Fatal("ordinary journal sealed-line path should trim queue-owned cells in place")
	}
	sealStandaloneLine := sourceFunctionBody(t, string(source), "func (renderer *journalRenderer) sealStandaloneLine")
	if strings.Contains(sealStandaloneLine, "cloneLogicalLine(line)") {
		t.Fatal("ordinary journal standalone seal must not clone logical line before store Apply")
	}
}

func TestR407JournalRendererKeepsTrailingSpaceTrimSemantics(t *testing.T) {
	renderer := NewHistoryJournalRenderer()
	journal := HistoryJournal{
		TerminalID: "term-r407-trim",
		Seq:        1,
		Size:       TerminalSemanticSize{Cols: 20, Rows: 5},
		Source:     HistoryJournalSourceSemanticTapTransaction,
		Items: []HistoryJournalItem{{
			Kind: HistoryJournalItemOrdinaryLineBatch,
			Ordinary: &OrdinaryLineBatch{
				Lines: []JournalLogicalLine{{Cells: []Cell{
					{Text: "x", Width: 1},
					{Text: " ", Width: 1},
				}}},
				Origin: HistoryJournalOriginOrdinaryPrimary,
			},
		}},
	}
	batch, err := renderer.ApplyJournal(journal)
	if err != nil {
		t.Fatalf("apply journal: %v", err)
	}
	if len(batch.Mutations) != 2 {
		t.Fatalf("expected seal+timeline mutations, got %#v", batch.Mutations)
	}
	if len(batch.Mutations[0].LineIDs) != 0 || len(batch.Mutations[1].LineIDs) != 0 || len(batch.Mutations[1].Record.LineIDs) != 1 {
		t.Fatalf("ordinary journal mutations should avoid duplicate LineIDs slices and keep record ids, mutations=%#v", batch.Mutations)
	}
	store := NewInMemoryHistoryStore("term-r407-trim")
	if err := store.Apply(batch); err != nil {
		t.Fatalf("apply store: %v", err)
	}
	window, err := store.LatestWindow(HistoryWindowRequest{TerminalID: "term-r407-trim", Mode: HistoryWindowModeLatest, Cols: 20, Limit: 1})
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	if got := strings.Join(rowTexts(window.Rows), "|"); got != "x" {
		t.Fatalf("ordinary sealed line should keep trailing blank trim semantics, got %q", got)
	}
}

func TestR382JournalRendererKeepsOpenLineEditingCJKAndStyle(t *testing.T) {
	renderer := NewHistoryJournalRenderer()
	store := NewInMemoryHistoryStore("term-r382-edit")

	first := HistoryJournalFromTransaction("term-r382-edit", TerminalSemanticTransaction{
		Seq:  1,
		Size: TerminalSemanticSize{Cols: 40, Rows: 4},
		Ops: []TerminalSemanticOp{
			{Code: vterm.ScreenOpWriteSpan, Row: 0, Col: 0, Cells: []TerminalSemanticCell{
				{Content: "中", Width: 2, Style: TerminalSemanticStyle{FG: "ansi:2"}},
				{Content: "x", Width: 1, Style: TerminalSemanticStyle{Bold: true}},
			}},
			{Code: vterm.ScreenOpControl, Control: "bs"},
			{Code: vterm.ScreenOpWriteSpan, Row: 0, Col: 2, Cells: []TerminalSemanticCell{{Content: "y", Width: 1, Style: TerminalSemanticStyle{BG: "#010203"}}}},
		},
	})
	batch, err := renderer.ApplyJournal(first)
	if err != nil {
		t.Fatalf("apply first journal: %v", err)
	}
	if err := store.Apply(batch); err != nil {
		t.Fatalf("apply first batch: %v", err)
	}
	window, err := store.LatestWindow(HistoryWindowRequest{TerminalID: "term-r382-edit", Mode: HistoryWindowModeLatest, Cols: 40, Limit: 5})
	if err != nil {
		t.Fatalf("latest open window: %v", err)
	}
	if got := strings.Join(rowTexts(window.Rows), "|"); got != "中y" {
		t.Fatalf("open line edit should project mutable text, got %q rows=%#v", got, window.Rows)
	}
	if window.Rows[0].Cells[0].Style.FG != "ansi:2" || window.Rows[0].Cells[1].Style.BG != "#010203" {
		t.Fatalf("journal renderer must preserve style attrs, cells=%#v", window.Rows[0].Cells)
	}

	second := HistoryJournalFromTransaction("term-r382-edit", TerminalSemanticTransaction{
		Seq:  2,
		Size: TerminalSemanticSize{Cols: 40, Rows: 4},
		Ops:  []TerminalSemanticOp{{Code: vterm.ScreenOpControl, Control: "lf"}},
	})
	batch, err = renderer.ApplyJournal(second)
	if err != nil {
		t.Fatalf("apply seal journal: %v", err)
	}
	if err := store.Apply(batch); err != nil {
		t.Fatalf("apply seal batch: %v", err)
	}
	window, err = store.LatestWindow(HistoryWindowRequest{TerminalID: "term-r382-edit", Mode: HistoryWindowModeLatest, Cols: 40, Limit: 5})
	if err != nil {
		t.Fatalf("latest sealed window: %v", err)
	}
	if got := strings.Join(rowTexts(window.Rows), "|"); got != "中y" || !window.Rows[0].Committed {
		t.Fatalf("LF journal command should seal previous open line, got %q rows=%#v", got, window.Rows)
	}
}

func TestR383JournalRendererAppliesED2BoundaryAndSealsOpenLine(t *testing.T) {
	journal := HistoryJournalFromTransaction("term-r382-boundary", TerminalSemanticTransaction{
		Seq:  1,
		Size: TerminalSemanticSize{Cols: 20, Rows: 4},
		Ops: []TerminalSemanticOp{
			{Code: vterm.ScreenOpWriteSpan, Row: 0, Col: 0, Cells: []TerminalSemanticCell{{Content: "x", Width: 1}}},
			{Code: vterm.ScreenOpControl, Control: "ed", Mode: 2},
		},
	})
	batch, err := NewHistoryJournalRenderer().ApplyJournal(journal)
	if err != nil {
		t.Fatalf("ED2 boundary should be handled by R383 state machine: %v journal=%#v", err, journal)
	}
	if got := joinedLineTexts(sealedMutationLines(batch.Mutations)); got != "x" {
		t.Fatalf("ED2 must seal ordinary open line before clearing ownership, got %q batch=%#v", got, batch)
	}
}

func TestR384JournalRendererAppliesScrollOutProofAfterOpenLine(t *testing.T) {
	renderer := NewHistoryJournalRenderer()
	mixed := HistoryJournalFromTransaction("term-r382-atomic", TerminalSemanticTransaction{
		Seq:  1,
		Size: TerminalSemanticSize{Cols: 20, Rows: 4},
		Ops: []TerminalSemanticOp{
			{Code: vterm.ScreenOpWriteSpan, Row: 0, Col: 0, Cells: []TerminalSemanticCell{{Content: "open", Width: 4}}},
			{Code: vterm.ScreenOpControl, Control: "ed", Mode: 2, ScrollOut: []vterm.ScrollbackRowAppend{{Runs: []vterm.CellRun{{Text: "scroll-proof"}}}}},
		},
	})
	batch, err := renderer.ApplyJournal(mixed)
	if err != nil {
		t.Fatalf("R384 should apply scroll-out proof journal: %v journal=%#v", err, mixed)
	}
	if got := joinedLineTexts(sealedMutationLines(batch.Mutations)); got != "scroll-proof\nopen" {
		t.Fatalf("ED2 journal must preserve vterm journal order for proof then boundary seal, got %q batch=%#v", got, batch)
	}

	seal := HistoryJournalFromTransaction("term-r382-atomic", TerminalSemanticTransaction{
		Seq:  2,
		Size: TerminalSemanticSize{Cols: 20, Rows: 4},
		Ops:  []TerminalSemanticOp{{Code: vterm.ScreenOpControl, Control: "lf"}},
	})
	batch, err = renderer.ApplyJournal(seal)
	if err != nil {
		t.Fatalf("LF-only journal after rejected mixed journal should remain valid: %v", err)
	}
	if len(batch.Mutations) != 0 {
		t.Fatalf("scroll-out journal must not leave stale open-line state behind, batch=%#v", batch)
	}
}

func TestR383JournalRendererAppliesBoundaryStateMachineWithoutSnapshotFallback(t *testing.T) {
	renderer := NewHistoryJournalRenderer()
	journal := HistoryJournal{
		TerminalID: "term-r383-boundary",
		Seq:        383,
		Source:     HistoryJournalSourceSemanticTapTransaction,
		Items: []HistoryJournalItem{
			{Kind: HistoryJournalItemOrdinaryLineBatch, Ordinary: &OrdinaryLineBatch{Lines: []JournalLogicalLine{{Cells: []Cell{{Text: "before-ed3", Width: 10}}}}}},
			{Kind: HistoryJournalItemBoundary, Boundary: &HistoryJournalBoundary{Kind: HistoryJournalBoundaryED3}},
			{Kind: HistoryJournalItemBoundary, Boundary: &HistoryJournalBoundary{Kind: HistoryJournalBoundaryResize}},
			{Kind: HistoryJournalItemOrdinaryLineBatch, Ordinary: &OrdinaryLineBatch{Lines: []JournalLogicalLine{{Cells: []Cell{{Text: "after-resize", Width: 12}}}}}},
		},
	}
	batch, err := renderer.ApplyJournal(journal)
	if err != nil {
		t.Fatalf("apply boundary journal: %v", err)
	}
	var kinds []HistoryMutationKind
	var texts []string
	for _, mutation := range batch.Mutations {
		kinds = append(kinds, mutation.Kind)
		if mutation.Line != nil {
			texts = append(texts, lineText(*mutation.Line))
		}
	}
	if got := strings.Join(texts, "|"); got != "before-ed3|after-resize" {
		t.Fatalf("boundary journal should preserve ordinary order around boundaries, got %q batch=%#v", got, batch)
	}
	if !mutationKindsContainInOrder(kinds, []HistoryMutationKind{
		HistoryMutationSealLine,
		HistoryMutationAppendTimelineRecord,
		HistoryMutationClearScrollback,
		HistoryMutationNonHistoryBoundary,
		HistoryMutationSealLine,
		HistoryMutationAppendTimelineRecord,
	}) {
		t.Fatalf("ED3/resize boundary mutations should stay in journal order, kinds=%v batch=%#v", kinds, batch)
	}
}

func TestR404JournalRendererIgnoresManualOrdinaryPayloadInsideSyncAlt(t *testing.T) {
	renderer := NewHistoryJournalRenderer()
	mixed := HistoryJournal{
		TerminalID: "term-r383-sync",
		Seq:        1,
		Source:     HistoryJournalSourceSemanticTapTransaction,
		Items: []HistoryJournalItem{
			{Kind: HistoryJournalItemBoundary, Boundary: &HistoryJournalBoundary{Kind: HistoryJournalBoundarySyncBegin}},
			{Kind: HistoryJournalItemOrdinaryLineBatch, Ordinary: &OrdinaryLineBatch{Lines: []JournalLogicalLine{{Cells: []Cell{{Text: "frame-like", Width: 10}}}}}},
			{Kind: HistoryJournalItemBoundary, Boundary: &HistoryJournalBoundary{Kind: HistoryJournalBoundarySyncEnd}},
		},
	}
	batch, err := renderer.ApplyJournal(mixed)
	if err != nil {
		t.Fatalf("sync payload journal must stay inside journal reducer: %v journal=%#v", err, mixed)
	}
	if got := joinedLineTexts(sealedMutationLines(batch.Mutations)); got != "" {
		t.Fatalf("sync ordinary payload must not become ordinary timeline, got %q batch=%#v", got, batch)
	}
	after := HistoryJournal{
		TerminalID: "term-r383-sync",
		Seq:        2,
		Source:     HistoryJournalSourceSemanticTapTransaction,
		Items: []HistoryJournalItem{
			{Kind: HistoryJournalItemOrdinaryLineBatch, Ordinary: &OrdinaryLineBatch{Lines: []JournalLogicalLine{{Cells: []Cell{{Text: "ordinary", Width: 8}}}}}},
		},
	}
	batch, err = renderer.ApplyJournal(after)
	if err != nil {
		t.Fatalf("ordinary journal after rejected sync payload should still apply: %v", err)
	}
	if got := joinedLineTexts(sealedMutationLines(batch.Mutations)); got != "ordinary" {
		t.Fatalf("sync journal must not leave renderer in sync mode, got %q batch=%#v", got, batch)
	}

	alt := HistoryJournal{
		TerminalID: "term-r383-alt",
		Seq:        3,
		Source:     HistoryJournalSourceSemanticTapTransaction,
		Items: []HistoryJournalItem{
			{Kind: HistoryJournalItemBoundary, Boundary: &HistoryJournalBoundary{Kind: HistoryJournalBoundaryAltEnter}},
			{Kind: HistoryJournalItemOrdinaryLineBatch, Ordinary: &OrdinaryLineBatch{Lines: []JournalLogicalLine{{Cells: []Cell{{Text: "alt", Width: 3}}}}}},
			{Kind: HistoryJournalItemBoundary, Boundary: &HistoryJournalBoundary{Kind: HistoryJournalBoundaryAltExit}},
		},
	}
	batch, err = renderer.ApplyJournal(alt)
	if err != nil {
		t.Fatalf("alt payload journal must stay inside journal reducer: %v journal=%#v", err, alt)
	}
	if got := joinedLineTexts(sealedMutationLines(batch.Mutations)); got != "" {
		t.Fatalf("alt ordinary payload must not enter primary timeline, got %q batch=%#v", got, batch)
	}
}

func TestR383JournalRendererRISResetsOpenLineState(t *testing.T) {
	renderer := NewHistoryJournalRenderer()
	open := HistoryJournalFromTransaction("term-r383-ris", TerminalSemanticTransaction{
		Seq:  1,
		Size: TerminalSemanticSize{Cols: 20, Rows: 4},
		Ops:  []TerminalSemanticOp{{Code: vterm.ScreenOpWriteSpan, Row: 0, Col: 0, Cells: []TerminalSemanticCell{{Content: "open", Width: 4}}}},
	})
	if _, err := renderer.ApplyJournal(open); err != nil {
		t.Fatalf("seed open journal: %v", err)
	}
	ris := HistoryJournalFromTransaction("term-r383-ris", TerminalSemanticTransaction{
		Seq:  2,
		Size: TerminalSemanticSize{Cols: 20, Rows: 4},
		Ops:  []TerminalSemanticOp{{Code: vterm.ScreenOpControl, Control: "ris"}},
	})
	batch, err := renderer.ApplyJournal(ris)
	if err != nil {
		t.Fatalf("apply RIS journal: %v", err)
	}
	if got := joinedLineTexts(sealedMutationLines(batch.Mutations)); got != "open" {
		t.Fatalf("RIS must seal journal-owned open line before reset, got %q batch=%#v", got, batch)
	}
	lf := HistoryJournalFromTransaction("term-r383-ris", TerminalSemanticTransaction{
		Seq:  3,
		Size: TerminalSemanticSize{Cols: 20, Rows: 4},
		Ops:  []TerminalSemanticOp{{Code: vterm.ScreenOpControl, Control: "lf"}},
	})
	batch, err = renderer.ApplyJournal(lf)
	if err != nil {
		t.Fatalf("apply LF after RIS: %v", err)
	}
	if len(sealedMutationLines(batch.Mutations)) != 0 {
		t.Fatalf("LF after RIS must not reseal stale open line, batch=%#v", batch)
	}
}

func TestR384JournalRendererPrimaryFrameRepaintIsMutableOnly(t *testing.T) {
	renderer := NewHistoryJournalRenderer()
	first := HistoryJournal{
		TerminalID: "term-r384-primary",
		Seq:        1,
		Source:     HistoryJournalSourceSemanticTapTransaction,
		Items: []HistoryJournalItem{
			{Kind: HistoryJournalItemBoundary, Boundary: &HistoryJournalBoundary{Kind: HistoryJournalBoundarySyncBegin}},
			{Kind: HistoryJournalItemFrameEvent, Frame: &HistoryJournalFrameEvent{Kind: HistoryJournalFrameReplacePrimary, Frame: &TerminalSemanticFrame{Cols: 20, Rows: [][]TerminalSemanticCell{historyCellsForJournalTest("old")}}}},
			{Kind: HistoryJournalItemBoundary, Boundary: &HistoryJournalBoundary{Kind: HistoryJournalBoundarySyncEnd}},
		},
	}
	if _, err := renderer.ApplyJournal(first); err != nil {
		t.Fatalf("apply first primary frame journal: %v", err)
	}
	second := HistoryJournal{
		TerminalID: "term-r384-primary",
		Seq:        2,
		Source:     HistoryJournalSourceSemanticTapTransaction,
		Items: []HistoryJournalItem{
			{Kind: HistoryJournalItemBoundary, Boundary: &HistoryJournalBoundary{Kind: HistoryJournalBoundarySyncBegin}},
			{Kind: HistoryJournalItemFrameEvent, Frame: &HistoryJournalFrameEvent{Kind: HistoryJournalFrameReplacePrimary, Frame: &TerminalSemanticFrame{Cols: 20, Rows: [][]TerminalSemanticCell{historyCellsForJournalTest("new")}}}},
			{Kind: HistoryJournalItemBoundary, Boundary: &HistoryJournalBoundary{Kind: HistoryJournalBoundarySyncEnd}},
		},
	}
	batch, err := renderer.ApplyJournal(second)
	if err != nil {
		t.Fatalf("apply second primary frame journal: %v", err)
	}
	if len(archivedFrameMutations(batch.Mutations)) != 0 || len(closedFrameMutations(batch.Mutations)) != 0 {
		t.Fatalf("primary frame repaint must update mutable current only, batch=%#v", batch)
	}
	var current []string
	for _, mutation := range batch.Mutations {
		if mutation.Mutable != nil {
			current = append(current, frameDraftText(mutation.Mutable.Rows))
		}
	}
	if got := strings.Join(current, "|"); got != "new" {
		t.Fatalf("primary frame journal should replace current payload, got %q batch=%#v", got, batch)
	}
}

func TestR384JournalRendererAltArchiveAndTransientFrame(t *testing.T) {
	renderer := NewHistoryJournalRenderer()
	seed := HistoryJournal{
		TerminalID: "term-r384-alt",
		Seq:        1,
		Source:     HistoryJournalSourceSemanticTapTransaction,
		Items: []HistoryJournalItem{
			{Kind: HistoryJournalItemFrameEvent, Frame: &HistoryJournalFrameEvent{Kind: HistoryJournalFrameReplacePrimary, Frame: &TerminalSemanticFrame{Cols: 12, Rows: [][]TerminalSemanticCell{historyCellsForJournalTest("primary")}}}},
		},
	}
	if _, err := renderer.ApplyJournal(seed); err != nil {
		t.Fatalf("seed primary frame journal: %v", err)
	}
	alt := HistoryJournal{
		TerminalID: "term-r384-alt",
		Seq:        2,
		Source:     HistoryJournalSourceSemanticTapTransaction,
		Items: []HistoryJournalItem{
			{Kind: HistoryJournalItemBoundary, Boundary: &HistoryJournalBoundary{Kind: HistoryJournalBoundaryAltEnter}},
			{Kind: HistoryJournalItemFrameEvent, Frame: &HistoryJournalFrameEvent{Kind: HistoryJournalFrameArchivePrimary}},
			{Kind: HistoryJournalItemFrameEvent, Frame: &HistoryJournalFrameEvent{Kind: HistoryJournalFrameReplaceAlt, Frame: &TerminalSemanticFrame{Cols: 12, Rows: [][]TerminalSemanticCell{historyCellsForJournalTest("alt")}}}},
			{Kind: HistoryJournalItemBoundary, Boundary: &HistoryJournalBoundary{Kind: HistoryJournalBoundaryAltExit}},
			{Kind: HistoryJournalItemFrameEvent, Frame: &HistoryJournalFrameEvent{Kind: HistoryJournalFrameClearAlt}},
		},
	}
	batch, err := renderer.ApplyJournal(alt)
	if err != nil {
		t.Fatalf("apply alt journal: %v", err)
	}
	if got := logicalLinesText(archivedFrameMutations(batch.Mutations)[0].Lines); got != "primary" {
		t.Fatalf("alt enter must archive existing primary frame, got %q batch=%#v", got, batch)
	}
	replaceAlt, clearAlt := 0, 0
	for _, mutation := range batch.Mutations {
		if mutation.Kind == HistoryMutationReplaceAltFrame && mutation.Transient != nil && frameDraftText(mutation.Transient.Rows) == "alt" {
			replaceAlt++
		}
		if mutation.Kind == HistoryMutationClearAltFrame {
			clearAlt++
		}
	}
	if replaceAlt != 1 || clearAlt != 1 {
		t.Fatalf("alt frame journal must publish transient alt then clear it, replace=%d clear=%d batch=%#v", replaceAlt, clearAlt, batch)
	}
}

func TestR384JournalRendererClearTimeScrollOutUsesCurrentFrameOwnership(t *testing.T) {
	renderer := NewHistoryJournalRenderer()
	seed := HistoryJournal{
		TerminalID: "term-r384-clear-proof",
		Seq:        1,
		Source:     HistoryJournalSourceSemanticTapTransaction,
		Items: []HistoryJournalItem{
			{Kind: HistoryJournalItemFrameEvent, Frame: &HistoryJournalFrameEvent{
				Kind:        HistoryJournalFrameReplacePrimary,
				Frame:       &TerminalSemanticFrame{Cols: 30, Rows: [][]TerminalSemanticCell{historyCellsForJournalTest("shell-1"), historyCellsForJournalTest("shell-2"), historyCellsForJournalTest("frame-1"), historyCellsForJournalTest("frame-2")}},
				TouchedRows: []int{2, 3},
			}},
		},
	}
	if _, err := renderer.ApplyJournal(seed); err != nil {
		t.Fatalf("seed touched primary frame journal: %v", err)
	}
	clear := HistoryJournal{
		TerminalID: "term-r384-clear-proof",
		Seq:        2,
		Source:     HistoryJournalSourceSemanticTapTransaction,
		Items: []HistoryJournalItem{
			{Kind: HistoryJournalItemScrollOutProof, ScrollOut: &HistoryJournalScrollOutProof{
				ClearTime: true,
				Rows: []TerminalSemanticScrollOut{
					{Runs: []TerminalSemanticCellRun{{Text: "shell-1"}}, Row: 0, RowSet: true},
					{Runs: []TerminalSemanticCellRun{{Text: "shell-2"}}, Row: 1, RowSet: true},
					{Runs: []TerminalSemanticCellRun{{Text: "frame-1"}}, Row: 2, RowSet: true},
					{Runs: []TerminalSemanticCellRun{{Text: "frame-2"}}, Row: 3, RowSet: true},
				},
			}},
			{Kind: HistoryJournalItemFrameEvent, Frame: &HistoryJournalFrameEvent{Kind: HistoryJournalFrameClearPrimary}},
		},
	}
	batch, err := renderer.ApplyJournal(clear)
	if err != nil {
		t.Fatalf("apply clear-time proof journal: %v", err)
	}
	if got := joinedLineTexts(sealedMutationLines(batch.Mutations)); got != "frame-1\nframe-2" {
		t.Fatalf("clear-time proof must seal only current frame owned rows, got %q batch=%#v", got, batch)
	}
}

func TestR404JournalRendererDecisionClosesPrimaryBeforeOrdinaryPrompt(t *testing.T) {
	renderer := NewHistoryJournalRenderer()
	store := NewInMemoryHistoryStore("term-r404-prompt")
	seed := HistoryJournalFromDecision("term-r404-prompt", TerminalSemanticTransaction{
		Seq:                     1,
		Size:                    TerminalSemanticSize{Cols: 20, Rows: 4},
		PrimaryFrameTouchedRows: []int{0, 1},
		PrimaryFrame:            &TerminalSemanticFrame{Cols: 20, Rows: [][]TerminalSemanticCell{historyCellsForJournalTest("codex header"), historyCellsForJournalTest("Shutting down...")}},
	}, HistoryDecision{
		Mode:                               HistoryOutputModePrimaryFrameSession,
		PublishPrimaryFrame:                true,
		PublishPrimaryFrameTouchedRowsOnly: true,
	})
	batch, err := renderer.ApplyJournal(seed)
	if err != nil {
		t.Fatalf("seed primary journal: %v", err)
	}
	if err := store.Apply(batch); err != nil {
		t.Fatalf("store seed journal: %v", err)
	}

	promptFrame := TerminalSemanticFrame{Cols: 20, Rows: [][]TerminalSemanticCell{historyCellsForJournalTest("codex header"), historyCellsForJournalTest("shell prompt")}}
	prompt := HistoryJournalFromDecision("term-r404-prompt", TerminalSemanticTransaction{
		Seq:  2,
		Size: TerminalSemanticSize{Cols: 20, Rows: 4},
		Ops: []TerminalSemanticOp{{
			Code:  vterm.ScreenOpWriteSpan,
			Row:   1,
			Col:   0,
			Cells: historyCellsForJournalTest("shell prompt"),
		}},
		PrimaryFrame: &promptFrame,
	}, HistoryDecision{
		Mode:                          HistoryOutputModeOrdinaryStream,
		ClosePrimaryFrameBeforeStream: true,
	})
	batch, err = renderer.ApplyJournal(prompt)
	if err != nil {
		t.Fatalf("apply prompt journal: %v labels=%v", err, journalLabels(prompt))
	}
	if got := strings.Join(journalLabels(prompt), "|"); !strings.Contains(got, "frame:close-primary:codex header") {
		t.Fatalf("decision journal must encode close-primary proof before prompt, labels=%v", journalLabels(prompt))
	}
	if err := store.Apply(batch); err != nil {
		t.Fatalf("store prompt journal: %v", err)
	}
	window, err := store.LatestWindow(HistoryWindowRequest{TerminalID: "term-r404-prompt", Mode: HistoryWindowModeLatest, Cols: 20, Limit: 10})
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	texts := rowTexts(window.Rows)
	joined := strings.Join(texts, "|")
	if !strings.Contains(joined, "codex header") || !strings.Contains(joined, "shell prompt") {
		t.Fatalf("journal pipeline should keep final frame row and prompt, got %q rows=%#v", joined, window.Rows)
	}
	if strings.Contains(joined, "Shutting down") {
		t.Fatalf("journal close-primary must exclude prompt-overwritten transient row, got %q rows=%#v", joined, window.Rows)
	}
	if journalRendererTextIndex(texts, "shell prompt") < journalRendererTextIndex(texts, "codex header") {
		t.Fatalf("prompt must project after closed frame row, rows=%#v", window.Rows)
	}
}

func TestR404JournalRendererCoversSyncAltResizeWithoutUnsupportedFallback(t *testing.T) {
	renderer := NewHistoryJournalRenderer()
	journal := HistoryJournalFromDecision("term-r404-coverage", TerminalSemanticTransaction{
		Seq:                     1,
		Size:                    TerminalSemanticSize{Cols: 30, Rows: 5},
		SynchronizedBegin:       true,
		SynchronizedEnd:         true,
		PrimaryFrameTouchedRows: []int{0},
		PrimaryFrame:            &TerminalSemanticFrame{Cols: 30, Rows: [][]TerminalSemanticCell{historyCellsForJournalTest("sync frame")}},
		Ops: []TerminalSemanticOp{
			{Code: vterm.ScreenOpModes, Private: true, Mode: 2026, Enabled: true},
			{Code: vterm.ScreenOpWriteSpan, Row: 0, Col: 0, Cells: historyCellsForJournalTest("sync frame")},
			{Code: vterm.ScreenOpModes, Private: true, Mode: 2026, Enabled: false},
		},
	}, HistoryDecision{
		Mode:                HistoryOutputModePrimaryFrameSession,
		PublishPrimaryFrame: true,
	})
	if _, err := renderer.ApplyJournal(journal); err != nil {
		t.Fatalf("sync primary journal must be fully handled, err=%v labels=%v", err, journalLabels(journal))
	}
	alt := HistoryJournalFromDecision("term-r404-coverage", TerminalSemanticTransaction{
		Seq:        2,
		Size:       TerminalSemanticSize{Cols: 30, Rows: 5},
		AltEntered: true,
		AltFrame:   &TerminalSemanticFrame{Cols: 30, Rows: [][]TerminalSemanticCell{historyCellsForJournalTest("alt frame")}},
	}, HistoryDecision{
		Mode:                    HistoryOutputModeAltTransient,
		ArchivePrimaryBeforeAlt: true,
		PublishAltFrame:         true,
	})
	if _, err := renderer.ApplyJournal(alt); err != nil {
		t.Fatalf("alt journal must be fully handled, err=%v labels=%v", err, journalLabels(alt))
	}
	resize := HistoryJournalFromDecision("term-r404-coverage", TerminalSemanticTransaction{
		Seq:                 3,
		Size:                TerminalSemanticSize{Cols: 40, Rows: 8},
		RequiresFullReplace: true,
		FullReplaceReason:   "resize",
	}, HistoryDecision{Mode: HistoryOutputModeBoundaryOnly, NonHistoryBoundary: true})
	if _, err := renderer.ApplyJournal(resize); err != nil {
		t.Fatalf("resize journal must be fully handled, err=%v labels=%v", err, journalLabels(resize))
	}
}

func TestR404JournalRendererArchivesPrimaryAfterFrameBeforeAlt(t *testing.T) {
	renderer := NewHistoryJournalRenderer()
	store := NewInMemoryHistoryStore("term-r404-sync-alt")
	journal := HistoryJournalFromDecision("term-r404-sync-alt", TerminalSemanticTransaction{
		Seq:                     1,
		Size:                    TerminalSemanticSize{Cols: 16, Rows: 4},
		SynchronizedBegin:       true,
		SynchronizedEnd:         true,
		AltEntered:              true,
		PrimaryFrameTouchedRows: []int{0, 1},
		PrimaryFrame: &TerminalSemanticFrame{Cols: 16, Rows: [][]TerminalSemanticCell{
			historyCellsForJournalTest("sync01"),
			historyCellsForJournalTest("sync02"),
		}},
		Ops: []TerminalSemanticOp{
			{Code: vterm.ScreenOpModes, Private: true, Mode: 2026, Enabled: true},
			{Code: vterm.ScreenOpWriteSpan, Row: 0, Col: 0, Cells: historyCellsForJournalTest("sync01")},
			{Code: vterm.ScreenOpWriteSpan, Row: 1, Col: 0, Cells: historyCellsForJournalTest("sync02")},
			{Code: vterm.ScreenOpModes, Private: true, Mode: 2026, Enabled: false},
			{Code: vterm.ScreenOpModes, Private: true, Mode: 1049, Enabled: true},
		},
	}, HistoryDecision{
		Mode:                               HistoryOutputModePrimaryFrameSession,
		PublishPrimaryFrame:                true,
		PublishPrimaryFrameTouchedRowsOnly: true,
		ArchivePrimaryAfterPrimaryFrame:    true,
	})
	if labels := strings.Join(journalLabels(journal), "|"); !strings.Contains(labels, "frame:replace-primary:sync01") || !strings.Contains(labels, "frame:archive-primary") {
		t.Fatalf("journal must encode primary frame publish then archive, labels=%s", labels)
	}
	batch, err := renderer.ApplyJournal(journal)
	if err != nil {
		t.Fatalf("apply sync-alt journal: %v labels=%v", err, journalLabels(journal))
	}
	if err := store.Apply(batch); err != nil {
		t.Fatalf("store sync-alt journal: %v", err)
	}
	window, err := store.LatestWindow(HistoryWindowRequest{TerminalID: "term-r404-sync-alt", Mode: HistoryWindowModeLatest, Cols: 16, Limit: 4})
	if err != nil {
		t.Fatalf("latest sync-alt window: %v", err)
	}
	if got := strings.Join(rowTexts(window.Rows), "|"); got != "sync01|sync02" {
		t.Fatalf("archived primary frame should remain visible without alt payload, got %q rows=%#v", got, window.Rows)
	}
	for _, row := range window.Rows {
		if row.Segment != HistorySegmentArchivedPrimaryFrame {
			t.Fatalf("sync-alt primary rows must be archived before alt transient, row=%#v", row)
		}
	}
}

func TestR404JournalRendererPreservesFixedGridSpacesAndFinalFrame(t *testing.T) {
	renderer := NewHistoryJournalRenderer()
	store := NewInMemoryHistoryStore("term-r404-fixed-grid")
	journal := HistoryJournalFromDecision("term-r404-fixed-grid", TerminalSemanticTransaction{
		Seq:                     1,
		Size:                    TerminalSemanticSize{Cols: 24, Rows: 4},
		PrimaryFrameTouchedRows: []int{0, 1},
		PrimaryFrame: &TerminalSemanticFrame{Cols: 24, Rows: [][]TerminalSemanticCell{
			historyCellsForJournalTest("model:  gpt "),
			historyCellsForJournalTest("status: ok  "),
		}},
	}, HistoryDecision{
		Mode:                               HistoryOutputModePrimaryFrameSession,
		PublishPrimaryFrame:                true,
		PublishPrimaryFrameTouchedRowsOnly: true,
	})
	batch, err := renderer.ApplyJournal(journal)
	if err != nil {
		t.Fatalf("apply fixed-grid frame journal: %v labels=%v", err, journalLabels(journal))
	}
	if err := store.Apply(batch); err != nil {
		t.Fatalf("store fixed-grid frame journal: %v", err)
	}
	final := HistoryJournal{
		TerminalID: "term-r404-fixed-grid",
		Seq:        2,
		Source:     HistoryJournalSourceSemanticTapTransaction,
		Items: []HistoryJournalItem{{
			Kind:  HistoryJournalItemFrameEvent,
			Frame: &HistoryJournalFrameEvent{Kind: HistoryJournalFrameFinalPrimary},
		}},
	}
	batch, err = renderer.ApplyJournal(final)
	if err != nil {
		t.Fatalf("apply final-frame journal: %v", err)
	}
	if err := store.Apply(batch); err != nil {
		t.Fatalf("store final-frame journal: %v", err)
	}
	window, err := store.LatestWindow(HistoryWindowRequest{TerminalID: "term-r404-fixed-grid", Mode: HistoryWindowModeLatest, Cols: 24, Limit: 4})
	if err != nil {
		t.Fatalf("latest fixed-grid window: %v", err)
	}
	texts := rowTexts(window.Rows)
	if got := strings.Join(texts, "|"); got != "model:  gpt |status: ok  " {
		t.Fatalf("journal final frame must preserve fixed-grid spaces, got %q rows=%#v", got, window.Rows)
	}
	for _, row := range window.Rows {
		if row.ScreenCols != 24 || row.Kind != LineKindScreenFrame || row.Segment != HistorySegmentCommitted {
			t.Fatalf("final frame row must preserve fixed-grid metadata, row=%#v", row)
		}
	}
}

func historyCellsForJournalTest(text string) []TerminalSemanticCell {
	cells := make([]TerminalSemanticCell, 0, len(text))
	for _, r := range text {
		cells = append(cells, TerminalSemanticCell{Content: string(r), Width: 1})
	}
	return cells
}

func journalRendererTextIndex(rows []string, needle string) int {
	for index, row := range rows {
		if strings.Contains(row, needle) {
			return index
		}
	}
	return -1
}

func mutationKindsContainInOrder(got []HistoryMutationKind, want []HistoryMutationKind) bool {
	if len(want) == 0 {
		return true
	}
	next := 0
	for _, kind := range got {
		if kind == want[next] {
			next++
			if next == len(want) {
				return true
			}
		}
	}
	return false
}
