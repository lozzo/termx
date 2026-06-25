package termxcorev2

import (
	"strings"
	"testing"

	"github.com/lozzow/termx/termx-core-v2/history"
	vterm "github.com/lozzow/termx/termx-vterm/vterm"
)

func TestHistoryProjectorOrdinaryOutputCommitsLogicalLines(t *testing.T) {
	track := history.NewHistoryTrack()
	projector := NewHistoryTrackProjector(track)
	tx := fakeTransaction(24, 4,
		writeOp(0, 0, "hello"),
	)
	tx.PrimaryScrollOut = []TerminalSemanticScrollOut{{}}
	decision := SimpleScreenAppClassifier{}.Classify(tx, ScreenSessionState{})
	mutation, err := projector.Apply(tx, decision)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !mutationContains(mutation, history.EventPrimaryScrollOut) {
		t.Fatalf("ordinary output should commit from primary scroll-out proof, mutation=%#v", mutation)
	}
	window, err := track.LatestWindow(history.HistoryWindowRequest{Cols: 24, Rows: 4})
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if window.TotalLines != 1 || !windowContainsAll(window, "hello") {
		t.Fatalf("ordinary logical line should be committed, total=%d rows=%#v", window.TotalLines, window.Rows)
	}
}

func TestHistoryProjectorPrimaryScreenFrameDoesNotGrowCommittedDepth(t *testing.T) {
	track := history.NewHistoryTrack()
	projector := NewHistoryTrackProjector(track)
	seed := fakeTransaction(40, 4, writeOp(0, 0, "shell"), controlOp("lf", 0, 5))
	if _, err := projector.Apply(seed, SimpleScreenAppClassifier{}.Classify(seed, ScreenSessionState{})); err != nil {
		t.Fatalf("seed apply: %v", err)
	}
	if _, err := projector.ForceClose(CloseReasonProcessExit); err != nil {
		t.Fatalf("seed force close: %v", err)
	}
	tx := fakeTransaction(40, 4, modeOp(2026, true), modeOp(2026, false))
	tx.PrimaryFrame = &TerminalSemanticFrame{Cols: 40, Rows: [][]TerminalSemanticCell{
		projectorVTermCells("current frame"),
		projectorVTermCells("prompt"),
	}}
	decision := SimpleScreenAppClassifier{}.Classify(tx, ScreenSessionState{})
	if _, err := projector.Apply(tx, decision); err != nil {
		t.Fatalf("apply frame: %v", err)
	}
	window, err := track.LatestWindow(history.HistoryWindowRequest{Cols: 40, Rows: 8})
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if window.TotalLines != 1 || !windowContainsAll(window, "shell", "current frame", "prompt") {
		t.Fatalf("primary frame should be latest projection without committed growth, total=%d rows=%#v", window.TotalLines, window.Rows)
	}
	for _, row := range window.Rows {
		if strings.Contains(row.Text, "current frame") && (row.Committed || row.Kind != history.RowKindScreenFrame) {
			t.Fatalf("primary frame row should be mutable screen-frame, row=%#v", row)
		}
	}
}

func TestHistoryProjectorAltTransientDoesNotCommitPrimaryHistory(t *testing.T) {
	track := history.NewHistoryTrack()
	projector := NewHistoryTrackProjector(track)
	seed := fakeTransaction(30, 4, writeOp(0, 0, "primary"), controlOp("lf", 0, 7))
	if _, err := projector.Apply(seed, SimpleScreenAppClassifier{}.Classify(seed, ScreenSessionState{})); err != nil {
		t.Fatalf("seed apply: %v", err)
	}
	if _, err := projector.ForceClose(CloseReasonProcessExit); err != nil {
		t.Fatalf("seed force close: %v", err)
	}
	tx := fakeTransaction(30, 4, modeOp(1049, true))
	tx.AltFrame = &TerminalSemanticFrame{Cols: 30, Rows: [][]TerminalSemanticCell{projectorVTermCells("alt picker")}}
	decision := SimpleScreenAppClassifier{}.Classify(tx, ScreenSessionState{})
	if _, err := projector.Apply(tx, decision); err != nil {
		t.Fatalf("apply alt: %v", err)
	}
	window, err := track.LatestWindow(history.HistoryWindowRequest{Cols: 30, Rows: 6})
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if window.TotalLines != 1 || !windowContainsAll(window, "alt picker") {
		t.Fatalf("alt transient should be selectable without committed growth, total=%d rows=%#v", window.TotalLines, window.Rows)
	}
}

func TestHistoryProjectorForceCloseCommitsPrimaryFrontier(t *testing.T) {
	track := history.NewHistoryTrack()
	projector := NewHistoryTrackProjector(track)
	tx := fakeTransaction(20, 3, writeOp(0, 0, "tail"))
	if _, err := projector.Apply(tx, SimpleScreenAppClassifier{}.Classify(tx, ScreenSessionState{})); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if _, err := projector.ForceClose(CloseReasonProcessExit); err != nil {
		t.Fatalf("force close: %v", err)
	}
	window, err := track.LatestWindow(history.HistoryWindowRequest{Cols: 20, Rows: 4})
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if window.TotalLines != 1 || !windowContainsAll(window, "tail") {
		t.Fatalf("force close should commit primary frontier, total=%d rows=%#v", window.TotalLines, window.Rows)
	}
}

func fakeTransaction(cols int, rows int, ops ...TerminalSemanticOp) TerminalSemanticTransaction {
	return TerminalSemanticTransaction{
		Seq:  1,
		Size: TerminalSemanticSize{Cols: cols, Rows: rows},
		Ops:  ops,
	}
}

func writeOp(row int, col int, text string) TerminalSemanticOp {
	return TerminalSemanticOp{Code: vterm.ScreenOpWriteSpan, Row: row, Col: col, Cells: projectorVTermCells(text)}
}

func controlOp(control string, row int, col int) TerminalSemanticOp {
	return TerminalSemanticOp{Code: vterm.ScreenOpControl, Control: control, Row: row, Col: col}
}

func modeOp(mode int, enabled bool) TerminalSemanticOp {
	return TerminalSemanticOp{Code: vterm.ScreenOpModes, Private: true, Mode: mode, Enabled: enabled}
}

func projectorVTermCells(text string) []TerminalSemanticCell {
	if text == "" {
		return nil
	}
	return []TerminalSemanticCell{{Content: text, Width: len([]rune(text))}}
}

func mutationContains(mutation HistoryMutation, kind history.EventKind) bool {
	for _, got := range mutation.Events {
		if got == kind {
			return true
		}
	}
	return false
}

func windowContainsAll(window history.HistoryWindow, wants ...string) bool {
	text := strings.Join(rowTextsFromHistoryWindow(window.Rows), "\n")
	for _, want := range wants {
		if !strings.Contains(text, want) {
			return false
		}
	}
	return true
}

func rowTextsFromHistoryWindow(rows []history.VisualRow) []string {
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = row.Text
	}
	return out
}
