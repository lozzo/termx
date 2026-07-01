package history

import (
	"errors"
	"fmt"
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
		t.Fatalf("journal fast path should preserve latest ordinary history, got %q", got)
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

func TestR383JournalRendererRejectsScrollOutJournalAtomically(t *testing.T) {
	renderer := NewHistoryJournalRenderer()
	mixed := HistoryJournalFromTransaction("term-r382-atomic", TerminalSemanticTransaction{
		Seq:  1,
		Size: TerminalSemanticSize{Cols: 20, Rows: 4},
		Ops: []TerminalSemanticOp{
			{Code: vterm.ScreenOpWriteSpan, Row: 0, Col: 0, Cells: []TerminalSemanticCell{{Content: "open", Width: 4}}},
			{Code: vterm.ScreenOpControl, Control: "ed", Mode: 2, ScrollOut: []vterm.ScrollbackRowAppend{{Runs: []vterm.CellRun{{Text: "scroll-proof"}}}}},
		},
	})
	if _, err := renderer.ApplyJournal(mixed); !errors.Is(err, ErrHistoryJournalUnsupported) {
		t.Fatalf("scroll-out proof journal must be rejected atomically until R384, err=%v journal=%#v", err, mixed)
	}

	seal := HistoryJournalFromTransaction("term-r382-atomic", TerminalSemanticTransaction{
		Seq:  2,
		Size: TerminalSemanticSize{Cols: 20, Rows: 4},
		Ops:  []TerminalSemanticOp{{Code: vterm.ScreenOpControl, Control: "lf"}},
	})
	batch, err := renderer.ApplyJournal(seal)
	if err != nil {
		t.Fatalf("LF-only journal after rejected mixed journal should remain valid: %v", err)
	}
	if len(batch.Mutations) != 0 {
		t.Fatalf("rejected journal must not leave renderer open-line state behind, batch=%#v", batch)
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

func TestR383JournalRendererRejectsSyncAltPayloadAtomically(t *testing.T) {
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
	if _, err := renderer.ApplyJournal(mixed); !errors.Is(err, ErrHistoryJournalUnsupported) {
		t.Fatalf("sync payload must fallback to full renderer, err=%v journal=%#v", err, mixed)
	}
	after := HistoryJournal{
		TerminalID: "term-r383-sync",
		Seq:        2,
		Source:     HistoryJournalSourceSemanticTapTransaction,
		Items: []HistoryJournalItem{
			{Kind: HistoryJournalItemOrdinaryLineBatch, Ordinary: &OrdinaryLineBatch{Lines: []JournalLogicalLine{{Cells: []Cell{{Text: "ordinary", Width: 8}}}}}},
		},
	}
	batch, err := renderer.ApplyJournal(after)
	if err != nil {
		t.Fatalf("ordinary journal after rejected sync payload should still apply: %v", err)
	}
	if got := joinedLineTexts(sealedMutationLines(batch.Mutations)); got != "ordinary" {
		t.Fatalf("rejected sync journal must not leave renderer in sync mode, got %q batch=%#v", got, batch)
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
	if _, err := renderer.ApplyJournal(alt); !errors.Is(err, ErrHistoryJournalUnsupported) {
		t.Fatalf("alt payload must fallback to full renderer, err=%v journal=%#v", err, alt)
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
