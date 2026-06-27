package history

import "testing"

func TestR322FrameReducerPrimaryRepaintIsCurrentOnly(t *testing.T) {
	reducer := NewFrameReducer()
	first, err := reducer.ReplacePrimaryCurrent(semanticFrame(12, "old"), FrameReasonPrimaryRepaint)
	if err != nil {
		t.Fatalf("replace primary current: %v", err)
	}
	second, err := reducer.ReplacePrimaryCurrent(semanticFrame(12, "new"), FrameReasonPrimaryRepaint)
	if err != nil {
		t.Fatalf("replace primary current again: %v", err)
	}
	if len(archivedFrameMutations(first)) != 0 || len(archivedFrameMutations(second)) != 0 {
		t.Fatalf("primary repaint must not append archived timeline records: first=%#v second=%#v", first, second)
	}
	current := currentPrimaryFrame(t, reducer)
	if got := frameDraftText(current.Rows); got != "new" {
		t.Fatalf("primary repaint must replace current frame payload, got %q", got)
	}
	if current.Cols != 12 || current.Source != FrameSourcePrimarySemantic {
		t.Fatalf("current frame lost fixed-grid semantic metadata: %#v", current)
	}
}

func TestR334FrameReducerTouchedRowsDoNotAdoptUntouchedShellTail(t *testing.T) {
	reducer := NewFrameReducer()
	mutations, err := reducer.ReplacePrimaryTouchedRows(semanticFrame(20,
		"sealed shell 1",
		"sealed shell 2",
		"",
		"codex ui",
	), []int{3}, FrameReasonPrimaryRepaint)
	if err != nil {
		t.Fatalf("replace touched primary rows: %v", err)
	}
	if len(mutations) != 1 || mutations[0].Mutable == nil {
		t.Fatalf("expected mutable frame mutation, got %#v", mutations)
	}
	if got := frameDraftText(mutations[0].Mutable.Rows); got != "codex ui" {
		t.Fatalf("touched-row frame must not copy untouched shell rows, got %q mutations=%#v", got, mutations)
	}
}

func TestR322FrameReducerAltTransientDoesNotEnterPrimaryTimeline(t *testing.T) {
	reducer := NewFrameReducer()
	if _, err := reducer.ReplaceAltCurrent(semanticFrame(20, "alt")); err != nil {
		t.Fatalf("replace alt current: %v", err)
	}
	state := frameDebugState(t, reducer)
	if state.AltCurrent == nil || frameDraftText(state.AltCurrent.Rows) != "alt" {
		t.Fatalf("alt frame should be transient current, got %#v", state.AltCurrent)
	}
	if state.PrimaryCurrent != nil || len(state.PrimaryArchived) != 0 {
		t.Fatalf("pure alt transient must not create primary frame/timeline state: %#v", state)
	}

	mutations, err := reducer.ClearAltCurrent(FrameReasonAltExit)
	if err != nil {
		t.Fatalf("clear alt current: %v", err)
	}
	if len(archivedFrameMutations(mutations)) != 0 || len(closedFrameMutations(mutations)) != 0 {
		t.Fatalf("alt exit must not seal alt content into primary timeline: %#v", mutations)
	}
	if frameDebugState(t, reducer).AltCurrent != nil {
		t.Fatalf("alt exit must clear transient frame")
	}
}

func TestR322FrameReducerArchivePrimaryOnAltEnterPreservesSequence(t *testing.T) {
	reducer := NewFrameReducer()
	if _, err := reducer.ReplacePrimaryCurrent(semanticFrame(9, "primary"), FrameReasonPrimaryRepaint); err != nil {
		t.Fatalf("replace primary current: %v", err)
	}
	mutations, err := reducer.ArchivePrimaryCurrent(SealReasonAltEnter)
	if err != nil {
		t.Fatalf("archive primary current: %v", err)
	}
	if frameDebugState(t, reducer).PrimaryCurrent != nil {
		t.Fatalf("alt enter archive must clear primary current")
	}
	archived := archivedFrameMutations(mutations)
	if len(archived) != 1 {
		t.Fatalf("expected one archived frame mutation, got %#v", mutations)
	}
	if got := logicalLinesText(archived[0].Lines); got != "primary" {
		t.Fatalf("archived frame must preserve current payload, got %q", got)
	}
	if archived[0].Cols != 9 || archived[0].Reason != SealReasonAltEnter {
		t.Fatalf("archived frame lost fixed width or reason: %#v", archived[0])
	}
	for _, line := range archived[0].Lines {
		if line.ScreenCols != 9 || line.Seal != SealStateSealed {
			t.Fatalf("archived frame line must be sealed at original width, got %#v", line)
		}
	}

	if _, err := reducer.ReplaceAltCurrent(semanticFrame(9, "alt")); err != nil {
		t.Fatalf("replace alt current: %v", err)
	}
	state := frameDebugState(t, reducer)
	if len(state.PrimaryArchived) != 1 || state.AltCurrent == nil {
		t.Fatalf("alt enter must retain archived primary and publish alt transient: %#v", state)
	}
}

func TestR322FrameReducerPostAltPrimaryIsNewFrame(t *testing.T) {
	reducer := NewFrameReducer()
	if _, err := reducer.ReplacePrimaryCurrent(semanticFrame(10, "before"), FrameReasonPrimaryRepaint); err != nil {
		t.Fatalf("replace primary current: %v", err)
	}
	beforeID := currentPrimaryFrame(t, reducer).ID
	if _, err := reducer.ArchivePrimaryCurrent(SealReasonAltEnter); err != nil {
		t.Fatalf("archive primary: %v", err)
	}
	if _, err := reducer.ReplaceAltCurrent(semanticFrame(10, "alt")); err != nil {
		t.Fatalf("replace alt: %v", err)
	}
	if _, err := reducer.ClearAltCurrent(FrameReasonAltExit); err != nil {
		t.Fatalf("clear alt: %v", err)
	}
	if _, err := reducer.ReplacePrimaryCurrent(semanticFrame(10, "after"), FrameReasonPrimaryRepaint); err != nil {
		t.Fatalf("replace post-alt primary: %v", err)
	}
	after := currentPrimaryFrame(t, reducer)
	if after.ID == beforeID {
		t.Fatalf("post-alt primary frame must not revive pre-alt current id=%d", beforeID)
	}
	if got := frameDraftText(after.Rows); got != "after" {
		t.Fatalf("post-alt primary payload mismatch: %q", got)
	}
}

func TestR333FrameReducerClearPrimaryForcesNewSessionEpoch(t *testing.T) {
	reducer := NewFrameReducer()
	if _, err := reducer.ReplacePrimaryCurrent(semanticFrame(10, "old"), FrameReasonPrimaryRepaint); err != nil {
		t.Fatalf("replace primary current: %v", err)
	}
	before := currentPrimaryFrame(t, reducer)
	if _, err := reducer.ClearPrimaryCurrent(FrameReasonPrimaryRepaint); err != nil {
		t.Fatalf("clear primary current: %v", err)
	}
	if _, err := reducer.ReplacePrimaryCurrent(semanticFrame(10, "new"), FrameReasonPrimaryRepaint); err != nil {
		t.Fatalf("replace primary current after clear: %v", err)
	}
	after := currentPrimaryFrame(t, reducer)
	if after.SessionID == before.SessionID || after.ID == before.ID {
		t.Fatalf("repaint clear must start a new frame/session epoch, before=%#v after=%#v", before, after)
	}
}

func TestR322FrameReducerResizeBoundaryDoesNotSealHistory(t *testing.T) {
	reducer := NewFrameReducer()
	if _, err := reducer.ReplacePrimaryCurrent(semanticFrame(80, "wide"), FrameReasonPrimaryRepaint); err != nil {
		t.Fatalf("replace primary current: %v", err)
	}
	mutations, err := reducer.ApplyNonHistoryBoundary(FrameReasonResize)
	if err != nil {
		t.Fatalf("apply resize boundary: %v", err)
	}
	if len(archivedFrameMutations(mutations)) != 0 || len(closedFrameMutations(mutations)) != 0 {
		t.Fatalf("resize boundary must not seal frame history: %#v", mutations)
	}
	current := currentPrimaryFrame(t, reducer)
	if current.Cols != 80 || frameDraftText(current.Rows) != "wide" {
		t.Fatalf("resize boundary must not rewrite current frame, got %#v", current)
	}
}

func TestR322FrameReducerClosePrimaryKeepsFixedWidthFinalFrame(t *testing.T) {
	reducer := NewFrameReducer()
	if _, err := reducer.ReplacePrimaryCurrent(semanticFrame(40, "final"), FrameReasonPrimaryRepaint); err != nil {
		t.Fatalf("replace primary current: %v", err)
	}
	mutations, err := reducer.ClosePrimaryCurrent(SealReasonTerminalClose)
	if err != nil {
		t.Fatalf("close primary current: %v", err)
	}
	closed := closedFrameMutations(mutations)
	if len(closed) != 1 {
		t.Fatalf("expected one closed primary frame mutation, got %#v", mutations)
	}
	if closed[0].Cols != 40 || closed[0].Reason != SealReasonTerminalClose || logicalLinesText(closed[0].Lines) != "final" {
		t.Fatalf("closed final frame lost fixed-grid payload: %#v", closed[0])
	}
	for _, line := range closed[0].Lines {
		if line.ScreenCols != 40 || line.Seal != SealStateSealed {
			t.Fatalf("closed frame line must preserve fixed generation width, got %#v", line)
		}
	}
	if frameDebugState(t, reducer).PrimaryCurrent != nil {
		t.Fatalf("close primary current must clear mutable current")
	}
}

func TestR325FrameReducerTrimsTrailingDefaultBlankFrameRows(t *testing.T) {
	reducer := NewFrameReducer()
	frame := semanticFrame(12,
		"codex current",
		"",
		"",
	)
	if _, err := reducer.ReplacePrimaryCurrent(frame, FrameReasonPrimaryRepaint); err != nil {
		t.Fatalf("replace primary current: %v", err)
	}
	current := currentPrimaryFrame(t, reducer)
	if got := len(current.Rows); got != 1 {
		t.Fatalf("trailing default blank frame rows must not enter history payload, got rows=%d frame=%#v", got, current)
	}
	if got := frameDraftText(current.Rows); got != "codex current" {
		t.Fatalf("frame text mismatch after trimming default blank tail: %q", got)
	}
}

func TestR325FrameReducerKeepsStyledBlankFrameRows(t *testing.T) {
	reducer := NewFrameReducer()
	frame := semanticFrame(8, "body")
	frame.Rows = append(frame.Rows, []TerminalSemanticCell{{Content: " ", Width: 1, Style: TerminalSemanticStyle{BG: "idx:24"}}})
	if _, err := reducer.ReplacePrimaryCurrent(frame, FrameReasonPrimaryRepaint); err != nil {
		t.Fatalf("replace primary current: %v", err)
	}
	current := currentPrimaryFrame(t, reducer)
	if got := len(current.Rows); got != 2 {
		t.Fatalf("styled trailing blank row is terminal content and must survive, got rows=%d frame=%#v", got, current)
	}
	if current.Rows[1].Line.Cells[0].Style.BG != "idx:24" {
		t.Fatalf("styled blank row lost style payload: %#v", current.Rows[1].Line.Cells)
	}
}

func currentPrimaryFrame(t *testing.T, reducer FrameReducer) MutableFrame {
	t.Helper()
	state := frameDebugState(t, reducer)
	if state.PrimaryCurrent == nil {
		t.Fatalf("expected primary current in state %#v", state)
	}
	return *state.PrimaryCurrent
}

func frameDebugState(t *testing.T, reducer FrameReducer) FrameJournal {
	t.Helper()
	debug, ok := reducer.(interface {
		debugFrameJournal() FrameJournal
	})
	if !ok {
		t.Fatalf("frame reducer does not expose test debug journal")
	}
	return debug.debugFrameJournal()
}

func archivedFrameMutations(mutations []HistoryMutation) []SealedFrame {
	var frames []SealedFrame
	for _, mutation := range mutations {
		if mutation.Kind == HistoryMutationArchivePrimaryFrame && mutation.Sealed != nil {
			frames = append(frames, *mutation.Sealed)
		}
	}
	return frames
}

func closedFrameMutations(mutations []HistoryMutation) []SealedFrame {
	var frames []SealedFrame
	for _, mutation := range mutations {
		if mutation.Kind == HistoryMutationClosePrimaryFrame && mutation.Sealed != nil {
			frames = append(frames, *mutation.Sealed)
		}
	}
	return frames
}

func semanticFrame(cols int, rows ...string) TerminalSemanticFrame {
	frameRows := make([][]TerminalSemanticCell, 0, len(rows))
	for _, row := range rows {
		var cells []TerminalSemanticCell
		for _, r := range row {
			cells = append(cells, TerminalSemanticCell{Content: string(r), Width: 1})
		}
		frameRows = append(frameRows, cells)
	}
	return TerminalSemanticFrame{Cols: cols, Rows: frameRows}
}

func frameDraftText(rows []LogicalLineDraft) string {
	var out string
	for _, row := range rows {
		out += lineText(row.Line)
	}
	return out
}

func logicalLinesText(lines []LogicalLine) string {
	var out string
	for _, line := range lines {
		out += lineText(line)
	}
	return out
}
