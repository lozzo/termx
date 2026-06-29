package history

import "testing"

func TestR327CJKContinuationCellsDoNotBecomeSpaces(t *testing.T) {
	reducer := NewFrameReducer()
	_, err := reducer.ReplacePrimaryCurrent(TerminalSemanticFrame{
		Cols: 8,
		Rows: [][]TerminalSemanticCell{{
			{Content: "中", Width: 2},
			{Content: "", Width: 0},
			{Content: "文", Width: 2},
			{Content: "", Width: 0},
		}},
	}, FrameReasonPrimaryRepaint)
	if err != nil {
		t.Fatalf("replace primary frame: %v", err)
	}
	current := currentPrimaryFrame(t, reducer)
	if got := frameDraftText(current.Rows); got != "中文" {
		t.Fatalf("wide-cell continuation must not project as plain spaces, got %q rows=%#v", got, current.Rows)
	}
	if got := current.Rows[0].Line.Cells; len(got) != 2 || got[0].Width != 2 || got[1].Width != 2 {
		t.Fatalf("history cells should keep only printable wide cells with authoritative widths, got %#v", got)
	}
}

func TestR327RepeatedScrollOutProofsAreDistinctTransactions(t *testing.T) {
	reducer := NewStreamLineReducer()
	first, err := reducer.SealScrollOut(TerminalSemanticScrollOut{Runs: []TerminalSemanticCellRun{{Text: "same"}}})
	if err != nil {
		t.Fatalf("seal first scroll-out proof: %v", err)
	}
	second, err := reducer.SealScrollOut(TerminalSemanticScrollOut{Runs: []TerminalSemanticCellRun{{Text: "same"}}})
	if err != nil {
		t.Fatalf("seal second scroll-out proof: %v", err)
	}
	if got := joinedLineTexts(sealedMutationLines(first)); got != "same" {
		t.Fatalf("first proof text mismatch: %q", got)
	}
	if got := joinedLineTexts(sealedMutationLines(second)); got != "same" {
		t.Fatalf("second equal payload proof must still be recorded as a new terminal event, got %q mutations=%#v", got, second)
	}
}
