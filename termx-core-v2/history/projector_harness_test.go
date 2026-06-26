package history

import (
	"strings"
	"testing"

	vterm "github.com/lozzow/termx/termx-vterm/vterm"
)

func TestR303OrdinaryScrollOutCommitsLogicalLine(t *testing.T) {
	store, projector := newR303Harness()

	tx := fakeTx(1, 12, 4)
	tx.PrimaryScrollOut = []TerminalSemanticScrollOut{{Cells: fakeTerminalCells("first line")}}
	applyR303(t, store, projector, tx, ScreenAppDecision{Mode: ScreenOutputModeOrdinary})

	window := latestR303(t, store)
	if got := committedPlainRows(window); len(got) != 1 || got[0] != "first line" {
		t.Fatalf("ordinary scroll-out should commit one logical line, got %#v in %#v", got, window.Rows)
	}
	if window.LogicalTotal != 1 {
		t.Fatalf("ordinary committed depth should be 1, got %d", window.LogicalTotal)
	}
}

func TestR303CarriageReturnMutatesFrontierWithoutIntermediateCommit(t *testing.T) {
	store, projector := newR303Harness()

	tx := fakeTx(1, 20, 3)
	tx.Ops = []TerminalSemanticOp{
		{Code: vterm.ScreenOpWriteSpan, Cells: fakeTerminalCells("progress 10%")},
		{Code: vterm.ScreenOpCursor, Row: 0, Col: 0},
		{Code: vterm.ScreenOpClearToEOL, Row: 0, Col: 0},
		{Code: vterm.ScreenOpWriteSpan, Cells: fakeTerminalCells("progress 20%")},
	}
	applyR303(t, store, projector, tx, ScreenAppDecision{Mode: ScreenOutputModeOrdinary})

	window := latestR303(t, store)
	if window.LogicalTotal != 0 {
		t.Fatalf("CR/CUP/EL frontier mutation must not commit intermediate progress, total=%d", window.LogicalTotal)
	}
	if got := allPlainRows(window); len(got) != 1 || !strings.Contains(got[0], "progress 20%") {
		t.Fatalf("latest should expose current mutable frontier only, got %#v", got)
	}
}

func TestR303PrimarySynchronizedFrameIsCurrentOnly(t *testing.T) {
	store, projector := newR303Harness()
	classifier := fakeR303Classifier{
		decisions: map[uint64]ScreenAppDecision{
			1: {
				Mode:         ScreenOutputModePrimaryScreenSession,
				PublishFrame: true,
			},
		},
	}

	tx := fakeTx(1, 18, 2)
	tx.SynchronizedBegin = true
	tx.SynchronizedEnd = true
	tx.PrimaryFrame = fakeTerminalFrame(18, "status ready", "next action")
	applyR303Classified(t, store, projector, classifier, tx, ScreenSessionState{})

	window := latestR303(t, store)
	rows := rowsBySegment(window, HistorySegmentCurrentPrimaryFrame)
	if len(rows) != 2 {
		t.Fatalf("primary synchronized frame should publish current rows only, got %#v", rows)
	}
	if window.LogicalTotal != 0 {
		t.Fatalf("current primary frame must not grow ordinary committed depth, total=%d", window.LogicalTotal)
	}
	if !rows[0].FixedGrid || rows[0].Committed {
		t.Fatalf("current primary frame should be fixed-grid and uncommitted: %#v", rows[0])
	}
}

func TestR311PrimarySessionCommitsScrollOutProofBeforeCurrentFrame(t *testing.T) {
	store, projector := newR303Harness()

	tx := fakeTx(1, 12, 3)
	tx.SynchronizedBegin = true
	tx.SynchronizedEnd = true
	tx.PrimaryScrollOut = []TerminalSemanticScrollOut{
		{Runs: []TerminalSemanticCellRun{{Text: "line01"}}},
		{Runs: []TerminalSemanticCellRun{{Text: "line02"}}},
	}
	tx.PrimaryFrame = fakeTerminalFrame(12, "line03", "line04")
	applyR303(t, store, projector, tx, ScreenAppDecision{
		Mode:         ScreenOutputModePrimaryScreenSession,
		PublishFrame: true,
	})

	window := latestR303(t, store)
	if got := committedPlainRows(window); len(got) != 2 || got[0] != "line01" || got[1] != "line02" {
		t.Fatalf("primary session scroll-out proof should commit logical history, got %#v in %#v", got, window.Rows)
	}
	current := plainRowsBySegment(window, HistorySegmentCurrentPrimaryFrame)
	if len(current) != 2 || current[0] != "line03" || current[1] != "line04" {
		t.Fatalf("current primary frame should remain latest fixed-grid frame, got %#v", current)
	}
}

func TestR303PrimaryFullscreenRepaintReplacesCurrentOnly(t *testing.T) {
	store, projector := newR303Harness()
	decision := ScreenAppDecision{Mode: ScreenOutputModePrimaryScreenSession, PublishFrame: true}

	tx1 := fakeTx(1, 16, 2)
	tx1.RequiresFullReplace = true
	tx1.PrimaryFrame = fakeTerminalFrame(16, "old top", "old bottom")
	applyR303(t, store, projector, tx1, decision)

	tx2 := fakeTx(2, 16, 2)
	tx2.RequiresFullReplace = true
	tx2.PrimaryFrame = fakeTerminalFrame(16, "new top", "new bottom")
	applyR303(t, store, projector, tx2, decision)

	window := latestR303(t, store)
	got := plainRowsBySegment(window, HistorySegmentCurrentPrimaryFrame)
	if len(got) != 2 || got[0] != "new top" || got[1] != "new bottom" {
		t.Fatalf("repaint should replace current primary frame, got %#v", got)
	}
	if strings.Contains(strings.Join(allPlainRows(window), "\n"), "old top") {
		t.Fatalf("repaint leaked previous current frame into history: %#v", allPlainRows(window))
	}
}

func TestR303AltTransientFrameIsSelectableButNotCommitted(t *testing.T) {
	store, projector := newR303Harness()

	tx := fakeTx(1, 20, 3)
	tx.AltEntered = true
	tx.AltFrame = fakeTerminalFrame(20, "picker one", "picker two")
	applyR303(t, store, projector, tx, ScreenAppDecision{
		Mode:                   ScreenOutputModeAltTransient,
		EnterAltTransientFrame: true,
		PublishFrame:           true,
	})

	window := latestR303(t, store)
	rows := rowsBySegment(window, HistorySegmentCurrentAltFrame)
	if len(rows) != 2 {
		t.Fatalf("alt transient frame should be visible for selection, got %#v", rows)
	}
	if window.LogicalTotal != 0 || rows[0].Committed {
		t.Fatalf("alt transient must not ordinary commit: total=%d row=%#v", window.LogicalTotal, rows[0])
	}
}

func TestR303PrimaryArchiveBeforeAltAndNewPrimaryFrameDoesNotRevivePreAltCurrent(t *testing.T) {
	store, projector := newR303Harness()

	primary := fakeTx(1, 24, 3)
	primary.PrimaryFrame = fakeTerminalFrame(24, "pre alt primary")
	applyR303(t, store, projector, primary, ScreenAppDecision{
		Mode:         ScreenOutputModePrimaryScreenSession,
		PublishFrame: true,
	})

	enterAlt := fakeTx(2, 24, 3)
	enterAlt.AltEntered = true
	enterAlt.AltFrame = fakeTerminalFrame(24, "transient picker")
	applyR303(t, store, projector, enterAlt, ScreenAppDecision{
		Mode:                      ScreenOutputModeAltTransient,
		ArchivePrimaryBeforeAlt:   true,
		ClearPrimaryCurrentForAlt: true,
		EnterAltTransientFrame:    true,
		PublishFrame:              true,
	})

	duringAlt := latestR303(t, store)
	if got := plainRowsBySegment(duringAlt, HistorySegmentArchivedPrimaryFrame); len(got) != 1 || got[0] != "pre alt primary" {
		t.Fatalf("alt enter should archive the previous primary current frame, got %#v", got)
	}
	if got := plainRowsBySegment(duringAlt, HistorySegmentCurrentPrimaryFrame); len(got) != 0 {
		t.Fatalf("alt enter should hide pre-alt primary current frame, got %#v", got)
	}
	if got := plainRowsBySegment(duringAlt, HistorySegmentCurrentAltFrame); len(got) != 1 || got[0] != "transient picker" {
		t.Fatalf("alt transient frame should be current selectable state, got %#v", got)
	}

	leaveAlt := fakeTx(3, 24, 3)
	leaveAlt.AltExited = true
	applyR303(t, store, projector, leaveAlt, ScreenAppDecision{
		Mode:                  ScreenOutputModeAltTransient,
		ExitAltTransientFrame: true,
	})

	nextPrimary := fakeTx(4, 24, 3)
	nextPrimary.PrimaryFrame = fakeTerminalFrame(24, "post alt primary")
	applyR303(t, store, projector, nextPrimary, ScreenAppDecision{
		Mode:         ScreenOutputModePrimaryScreenSession,
		PublishFrame: true,
	})

	afterAlt := latestR303(t, store)
	if got := plainRowsBySegment(afterAlt, HistorySegmentCurrentPrimaryFrame); len(got) != 1 || got[0] != "post alt primary" {
		t.Fatalf("post-alt primary output should publish a new current frame, got %#v", got)
	}
	if strings.Contains(strings.Join(plainRowsBySegment(afterAlt, HistorySegmentCurrentPrimaryFrame), "\n"), "pre alt primary") {
		t.Fatalf("post-alt current frame revived pre-alt primary current: %#v", afterAlt.Rows)
	}
}

func TestR303AltExitDoesNotCommitAltFrame(t *testing.T) {
	store, projector := newR303Harness()

	enter := fakeTx(1, 20, 3)
	enter.AltEntered = true
	enter.AltFrame = fakeTerminalFrame(20, "transient editor")
	applyR303(t, store, projector, enter, ScreenAppDecision{
		Mode:                   ScreenOutputModeAltTransient,
		EnterAltTransientFrame: true,
		PublishFrame:           true,
	})

	leave := fakeTx(2, 20, 3)
	leave.AltExited = true
	applyR303(t, store, projector, leave, ScreenAppDecision{
		Mode:                  ScreenOutputModeAltTransient,
		ExitAltTransientFrame: true,
	})

	window := latestR303(t, store)
	if rows := rowsBySegment(window, HistorySegmentCurrentAltFrame); len(rows) != 0 {
		t.Fatalf("alt exit should drop transient frame from latest projection, got %#v", rows)
	}
	if window.LogicalTotal != 0 {
		t.Fatalf("pure alt transient must not commit on exit, total=%d", window.LogicalTotal)
	}
}

func TestR303ForceCloseCommitsFrontierAndPrimaryFinalFrameOnce(t *testing.T) {
	store, projector := newR303Harness()

	ordinary := fakeTx(1, 20, 3)
	ordinary.Ops = []TerminalSemanticOp{{Code: vterm.ScreenOpWriteSpan, Cells: fakeTerminalCells("tail")}}
	applyR303(t, store, projector, ordinary, ScreenAppDecision{Mode: ScreenOutputModeOrdinary})

	frameTx := fakeTx(2, 20, 2)
	frameTx.PrimaryFrame = fakeTerminalFrame(20, "final top", "final bottom")
	applyR303(t, store, projector, frameTx, ScreenAppDecision{
		Mode:         ScreenOutputModePrimaryScreenSession,
		PublishFrame: true,
	})

	mutation, err := projector.ForceClose(CloseReasonProcessExit)
	if err != nil {
		t.Fatalf("force close: %v", err)
	}
	if err := store.ApplyMutation(mutation); err != nil {
		t.Fatalf("apply force close: %v", err)
	}
	mutation, err = projector.ForceClose(CloseReasonProcessExit)
	if err != nil {
		t.Fatalf("second force close: %v", err)
	}
	if err := store.ApplyMutation(mutation); err != nil {
		t.Fatalf("apply second force close: %v", err)
	}

	window := latestR303(t, store)
	plain := committedPlainRows(window)
	if countString(plain, "tail") != 1 {
		t.Fatalf("process exit should force commit primary frontier once, committed=%#v", plain)
	}
	if countString(plain, "final top") != 1 || countString(plain, "final bottom") != 1 {
		t.Fatalf("process exit should commit final fixed frame once, committed=%#v", plain)
	}
	for _, row := range window.Rows {
		if row.Kind == LineKindScreenFrame && row.Committed && !row.FixedGrid {
			t.Fatalf("final screen-frame must remain fixed-grid after commit: %#v", row)
		}
	}
}

func TestR303ResizeIsNonHistoryBoundary(t *testing.T) {
	store, projector := newR303Harness()

	tx := fakeTx(1, 30, 8)
	tx.Ops = []TerminalSemanticOp{{Code: vterm.ScreenOpResize, Size: vterm.Size{Cols: 30, Rows: 8}}}
	applyR303(t, store, projector, tx, ScreenAppDecision{
		Mode:               ScreenOutputModeNonHistoryBoundary,
		NonHistoryBoundary: true,
	})

	window := latestR303(t, store)
	if len(window.Rows) != 0 || window.LogicalTotal != 0 {
		t.Fatalf("resize boundary must not create history rows, window=%#v", window)
	}
}

func TestR303FrozenCopyBoundarySurvivesLaterRepaint(t *testing.T) {
	store, projector := newR303Harness()

	tx := fakeTx(1, 20, 3)
	tx.Ops = []TerminalSemanticOp{{Code: vterm.ScreenOpWriteSpan, Cells: fakeTerminalCells("copy me")}}
	applyR303(t, store, projector, tx, ScreenAppDecision{Mode: ScreenOutputModeOrdinary})

	snapshot, err := store.Freeze(FreezeHistoryRequest{TerminalID: "term", Cols: 20, Limit: 10})
	if err != nil {
		t.Fatalf("freeze: %v", err)
	}
	if len(snapshot.FrozenFrontierLineIDs) != 1 {
		t.Fatalf("freeze should capture visible mutable frontier boundary, got %#v", snapshot)
	}

	repaint := fakeTx(2, 20, 3)
	repaint.PrimaryFrame = fakeTerminalFrame(20, "mutable repaint")
	applyR303(t, store, projector, repaint, ScreenAppDecision{
		Mode:         ScreenOutputModePrimaryScreenSession,
		PublishFrame: true,
	})

	copyText, err := store.Copy(HistoryCopyRequest{TerminalID: "term", Token: snapshot.Token})
	if err != nil {
		t.Fatalf("copy frozen token: %v", err)
	}
	if strings.Contains(copyText, "mutable repaint") {
		t.Fatalf("frozen copy boundary must not read later current frame: %q", copyText)
	}
	later, err := store.Freeze(FreezeHistoryRequest{TerminalID: "term", Cols: 20, Limit: 10})
	if err != nil {
		t.Fatalf("second freeze: %v", err)
	}
	if later.Token == snapshot.Token {
		t.Fatalf("freeze tokens must identify distinct copy boundaries: %q", later.Token)
	}
}

func newR303Harness() (InfiniteHistoryStore, HistoryProjector) {
	return NewMemoryHistoryStore(), NewMemoryHistoryProjector()
}

type fakeR303Classifier struct {
	decisions map[uint64]ScreenAppDecision
}

func (classifier fakeR303Classifier) Classify(tx TerminalSemanticTransaction, _ ScreenSessionState) ScreenAppDecision {
	if decision, ok := classifier.decisions[tx.Seq]; ok {
		return decision
	}
	return ScreenAppDecision{Mode: ScreenOutputModeOrdinary}
}

func applyR303Classified(
	t *testing.T,
	store InfiniteHistoryStore,
	projector HistoryProjector,
	classifier ScreenAppClassifier,
	tx TerminalSemanticTransaction,
	state ScreenSessionState,
) {
	t.Helper()
	applyR303(t, store, projector, tx, classifier.Classify(tx, state))
}

func applyR303(t *testing.T, store InfiniteHistoryStore, projector HistoryProjector, tx TerminalSemanticTransaction, decision ScreenAppDecision) {
	t.Helper()
	mutation, err := projector.Apply(tx, decision)
	if err != nil {
		t.Fatalf("project tx %d: %v", tx.Seq, err)
	}
	if err := store.ApplyMutation(mutation); err != nil {
		t.Fatalf("apply mutation %#v: %v", mutation, err)
	}
}

func latestR303(t *testing.T, store InfiniteHistoryStore) HistoryWindow {
	t.Helper()
	window, err := store.LatestWindow(HistoryWindowRequest{TerminalID: "term", Mode: HistoryWindowModeLatest, Cols: 20, Limit: 100})
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	return window
}

func fakeTx(seq uint64, cols int, rows int) TerminalSemanticTransaction {
	return TerminalSemanticTransaction{Seq: seq, Size: TerminalSemanticSize{Cols: cols, Rows: rows}}
}

func fakeTerminalFrame(cols int, rows ...string) *TerminalSemanticFrame {
	frame := &TerminalSemanticFrame{Cols: cols}
	for _, row := range rows {
		frame.Rows = append(frame.Rows, fakeTerminalCells(row))
	}
	return frame
}

func fakeTerminalCells(text string) []TerminalSemanticCell {
	cells := make([]TerminalSemanticCell, 0, len(text))
	for _, r := range text {
		cells = append(cells, TerminalSemanticCell{Content: string(r), Width: 1})
	}
	return cells
}

func allPlainRows(window HistoryWindow) []string {
	rows := make([]string, 0, len(window.Rows))
	for _, row := range window.Rows {
		rows = append(rows, plainText(row.Cells))
	}
	return rows
}

func committedPlainRows(window HistoryWindow) []string {
	var rows []string
	for _, row := range window.Rows {
		if row.Committed {
			rows = append(rows, plainText(row.Cells))
		}
	}
	return rows
}

func plainRowsBySegment(window HistoryWindow, segment HistorySegment) []string {
	var rows []string
	for _, row := range rowsBySegment(window, segment) {
		rows = append(rows, plainText(row.Cells))
	}
	return rows
}

func rowsBySegment(window HistoryWindow, segment HistorySegment) []HistoryRow {
	var rows []HistoryRow
	for _, row := range window.Rows {
		if row.Segment == segment {
			rows = append(rows, row)
		}
	}
	return rows
}

func countString(values []string, want string) int {
	count := 0
	for _, value := range values {
		if value == want {
			count++
		}
	}
	return count
}
