package history

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestHistoryTrackWritesSealsAndCommitsLogicalLines(t *testing.T) {
	track := NewHistoryTrack()

	applyHistoryEvents(t, track,
		HistoryEvent{Kind: EventWritePrimaryCells, Cells: cells("hello")},
		HistoryEvent{Kind: EventWritePrimaryCells, Cells: cells(" world")},
	)
	if got := track.CommittedIDs(); len(got) != 0 {
		t.Fatalf("write must not create committed history, got %v", got)
	}
	if got := track.FrontierIDs(); !reflect.DeepEqual(got, []LogicalLineID{1}) {
		t.Fatalf("unexpected frontier ids %v", got)
	}
	line := requireLine(t, track, 1)
	if lineText(line) != "hello world" {
		t.Fatalf("unexpected line text %q", lineText(line))
	}
	if line.Seal != SealStateOpen {
		t.Fatalf("expected open line, got %q", line.Seal)
	}
	if !line.Dirty {
		t.Fatal("expected written line to be dirty")
	}

	applyHistoryEvents(t, track,
		HistoryEvent{Kind: EventSealLogicalLine},
		HistoryEvent{Kind: EventCommitFrontier},
	)
	if got := track.CommittedIDs(); !reflect.DeepEqual(got, []LogicalLineID{1}) {
		t.Fatalf("unexpected committed ids %v", got)
	}
	if got := track.FrontierIDs(); len(got) != 0 {
		t.Fatalf("expected empty frontier after commit, got %v", got)
	}
	line = requireLine(t, track, 1)
	if line.Seal != SealStateSealed {
		t.Fatalf("expected sealed committed line, got %q", line.Seal)
	}
	if line.Dirty {
		t.Fatal("expected commit to clean line")
	}
}

func TestHistoryTrackCommitFrontierRequiresLeavingPrimaryScreenOwnership(t *testing.T) {
	track := NewHistoryTrack()
	track.SetPrimaryScreenRows(2)

	applyHistoryEvents(t, track,
		HistoryEvent{Kind: EventWritePrimaryCells, Cells: cells("one")},
		HistoryEvent{Kind: EventSealLogicalLine},
		HistoryEvent{Kind: EventCommitFrontier},
	)
	if got := track.CommittedIDs(); len(got) != 0 {
		t.Fatalf("sealed visible line must stay out of committed history, got %v", got)
	}
	if got := track.CommittableIDs(); len(got) != 0 {
		t.Fatalf("visible sealed line must not be committable yet, got %v", got)
	}

	applyHistoryEvents(t, track,
		HistoryEvent{Kind: EventWritePrimaryCells, Cells: cells("two")},
		HistoryEvent{Kind: EventSealLogicalLine},
		HistoryEvent{Kind: EventCommitFrontier},
	)
	if got := track.CommittedIDs(); !reflect.DeepEqual(got, []LogicalLineID{1}) {
		t.Fatalf("newline at screen bottom should scroll out the oldest line, got committed %v", got)
	}

	applyHistoryEvents(t, track,
		HistoryEvent{Kind: EventWritePrimaryCells, Cells: cells("three")},
		HistoryEvent{Kind: EventSealLogicalLine},
	)
	if got := track.CommittableIDs(); !reflect.DeepEqual(got, []LogicalLineID{2}) {
		t.Fatalf("second sealed line should become committable after next scroll, got %v", got)
	}

	applyHistoryEvents(t, track, HistoryEvent{Kind: EventCommitFrontier})
	if got := track.CommittedIDs(); !reflect.DeepEqual(got, []LogicalLineID{1, 2}) {
		t.Fatalf("scrolled-out sealed lines should commit in order, got %v", got)
	}
	if got := track.FrontierIDs(); !reflect.DeepEqual(got, []LogicalLineID{3}) {
		t.Fatalf("newer visible sealed line should remain frontier-owned, got %v", got)
	}
}

func TestHistoryTrackWritesNewLogicalLineAfterSeal(t *testing.T) {
	track := NewHistoryTrack()

	applyHistoryEvents(t, track,
		HistoryEvent{Kind: EventWritePrimaryCells, Cells: cells("a")},
		HistoryEvent{Kind: EventSealLogicalLine},
		HistoryEvent{Kind: EventWritePrimaryCells, Cells: cells("b")},
	)

	if got := track.FrontierIDs(); !reflect.DeepEqual(got, []LogicalLineID{1, 2}) {
		t.Fatalf("unexpected frontier ids %v", got)
	}
	if got := lineText(requireLine(t, track, 1)); got != "a" {
		t.Fatalf("unexpected first line text %q", got)
	}
	if got := lineText(requireLine(t, track, 2)); got != "b" {
		t.Fatalf("unexpected second line text %q", got)
	}
}

func TestHistoryTrackPreservesExplicitBlankLines(t *testing.T) {
	track := NewHistoryTrack()
	track.SetPrimaryScreenRows(4)
	applyHistoryEvents(t, track,
		HistoryEvent{Kind: EventWritePrimaryCells, Cells: cells("top")},
		HistoryEvent{Kind: EventSealLogicalLine},
		HistoryEvent{Kind: EventSealLogicalLine},
		HistoryEvent{Kind: EventWritePrimaryCells, Cells: cells("bottom")},
	)

	if got := track.LineIDs(); !reflect.DeepEqual(got, []LogicalLineID{1, 2, 3}) {
		t.Fatalf("explicit blank line should have a logical line id, got %v", got)
	}
	if got := lineText(requireLine(t, track, 2)); got != "" {
		t.Fatalf("blank line should preserve empty text, got %q", got)
	}
	window, err := track.LatestWindow(HistoryWindowRequest{Cols: 20, Rows: 4})
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	if got := rowTextsFromWindow(window.Rows); !reflect.DeepEqual(got, []string{"top", "", "bottom"}) {
		t.Fatalf("latest should preserve explicit blank row, got %v", got)
	}
}

func TestHistoryTrackCarriageReturnOverwritesCurrentMutableLine(t *testing.T) {
	track := NewHistoryTrack()
	applyHistoryEvents(t, track,
		HistoryEvent{Kind: EventWritePrimaryCells, Cells: cells("one")},
		HistoryEvent{Kind: EventCarriageReturn},
		HistoryEvent{Kind: EventWritePrimaryCells, Cells: cells("T")},
	)

	line := requireLine(t, track, 1)
	if got := lineText(line); got != "Tne" {
		t.Fatalf("carriage return overwrite should mutate current open line, got %q", got)
	}
	if got := track.CommittedIDs(); len(got) != 0 {
		t.Fatalf("carriage return overwrite must stay in mutable frontier, got committed %v", got)
	}
	if got := track.FrontierIDs(); !reflect.DeepEqual(got, []LogicalLineID{1}) {
		t.Fatalf("overwritten line should remain frontier-owned, got %v", got)
	}
}

func TestHistoryTrackCursorMovementOverwritesCurrentMutableLine(t *testing.T) {
	track := NewHistoryTrack()
	applyHistoryEvents(t, track,
		HistoryEvent{Kind: EventWritePrimaryCells, Cells: cells("abcdef")},
		HistoryEvent{Kind: EventCursorBackward, Count: 3},
		HistoryEvent{Kind: EventWritePrimaryCells, Cells: cells("XY")},
	)

	line := requireLine(t, track, 1)
	if got := lineText(line); got != "abcXYf" {
		t.Fatalf("cursor movement should overwrite from active column, got %q", got)
	}
}

func TestHistoryTrackCursorAbsoluteColumnOverwritesCurrentMutableLine(t *testing.T) {
	track := NewHistoryTrack()
	applyHistoryEvents(t, track,
		HistoryEvent{Kind: EventWritePrimaryCells, Cells: cells("abcdef")},
		HistoryEvent{Kind: EventCursorHorizontalAbsolute, Count: 2},
		HistoryEvent{Kind: EventWritePrimaryCells, Cells: cells("Z")},
	)

	line := requireLine(t, track, 1)
	if got := lineText(line); got != "aZcdef" {
		t.Fatalf("absolute column should use 1-based terminal columns, got %q", got)
	}
}

func TestHistoryTrackCursorForwardPastLineEndPadsBeforeWrite(t *testing.T) {
	track := NewHistoryTrack()
	applyHistoryEvents(t, track,
		HistoryEvent{Kind: EventWritePrimaryCells, Cells: cells("a")},
		HistoryEvent{Kind: EventCursorForward, Count: 3},
		HistoryEvent{Kind: EventWritePrimaryCells, Cells: cells("X")},
	)

	line := requireLine(t, track, 1)
	if got := lineText(line); got != "a   X" {
		t.Fatalf("cursor forward past line end should preserve blank columns, got %q", got)
	}
}

func TestHistoryTrackCursorUpRewritesScreenOwnedCommittedLine(t *testing.T) {
	track := NewHistoryTrack()
	track.SetPrimaryScreenRows(3)
	applyHistoryEvents(t, track,
		HistoryEvent{Kind: EventWritePrimaryCells, Cells: cells("Working")},
		HistoryEvent{Kind: EventSealLogicalLine},
		HistoryEvent{Kind: EventWritePrimaryCells, Cells: cells("")},
		HistoryEvent{Kind: EventSealLogicalLine},
		HistoryEvent{Kind: EventCommitFrontier},
	)
	if got := track.CommittedIDs(); len(got) != 0 {
		t.Fatalf("screen-owned sealed line should not commit yet, got %v", got)
	}

	applyHistoryEvents(t, track,
		HistoryEvent{Kind: EventCursorUp, Count: 2},
		HistoryEvent{Kind: EventCursorHorizontalAbsolute, Count: 1},
		HistoryEvent{Kind: EventWritePrimaryCells, Cells: cells("Done")},
		HistoryEvent{Kind: EventEraseInLine, EraseMode: 0},
	)

	if got := track.CommittedIDs(); len(got) != 0 {
		t.Fatalf("screen row rewrite must not reorder committed history, got %v", got)
	}
	if got := lineText(requireLine(t, track, 1)); strings.TrimRight(got, " ") != "Done" {
		t.Fatalf("cursor-up rewrite should replace original screen-owned line, got %q", got)
	}
	if got := track.LineIDs(); !reflect.DeepEqual(got, []LogicalLineID{1, 2}) {
		t.Fatalf("rewrite must not append duplicate Working lines, got %v", got)
	}
}

func TestHistoryTrackEraseInLineMutatesCurrentMutableLine(t *testing.T) {
	track := NewHistoryTrack()
	applyHistoryEvents(t, track,
		HistoryEvent{Kind: EventWritePrimaryCells, Cells: cells("hello")},
		HistoryEvent{Kind: EventCarriageReturn},
		HistoryEvent{Kind: EventWritePrimaryCells, Cells: cells("he")},
		HistoryEvent{Kind: EventEraseInLine, EraseMode: 0},
	)

	line := requireLine(t, track, 1)
	if got := strings.TrimRight(lineText(line), " "); got != "he" {
		t.Fatalf("EL from cursor to end should clear mutable tail, got %q", got)
	}
	if got := track.CommittedIDs(); len(got) != 0 {
		t.Fatalf("erase-in-line must stay in mutable frontier, got committed %v", got)
	}
}

func TestHistoryTrackEraseInLinePreservesStyledBlankToVisualRowEnd(t *testing.T) {
	track := NewHistoryTrack()
	style := CellStyle{BG: "idx:24"}
	applyHistoryEvents(t, track,
		HistoryEvent{Kind: EventWritePrimaryCells, Cells: []Cell{{Text: "BG", Width: 2, Style: style}}},
		HistoryEvent{Kind: EventEraseInLine, EraseMode: 0, EraseCols: 8, Style: style},
	)

	line := requireLine(t, track, 1)
	if got := lineText(line); got != "BG      " {
		t.Fatalf("styled EL 0 should preserve blank footprint to visual row end, got %q", got)
	}
	if got := logicalLineWidth(line.Cells); got != 8 {
		t.Fatalf("styled EL 0 should extend to primary cols, got width %d cells=%#v", got, line.Cells)
	}
	for i, cell := range line.Cells {
		if cell.Style.BG != "idx:24" {
			t.Fatalf("styled EL 0 should keep bg on cell %d, got %#v", i, cell)
		}
	}
}

func TestHistoryTrackPlainSGRDoesNotPadToVisualRowEnd(t *testing.T) {
	track := NewHistoryTrack()
	style := CellStyle{BG: "idx:24"}
	applyHistoryEvents(t, track,
		HistoryEvent{Kind: EventWritePrimaryCells, Cells: []Cell{{Text: "BG", Width: 2, Style: style}}},
	)

	line := requireLine(t, track, 1)
	if got := lineText(line); got != "BG" {
		t.Fatalf("plain styled text should not synthesize trailing blanks, got %q", got)
	}
	if got := logicalLineWidth(line.Cells); got != 2 {
		t.Fatalf("plain styled text should keep only emitted columns, got width %d cells=%#v", got, line.Cells)
	}
}

func TestHistoryTrackMutationPreservesEmptyStyledFootprint(t *testing.T) {
	track := NewHistoryTrack()
	style := CellStyle{BG: "idx:24"}
	applyHistoryEvents(t, track,
		HistoryEvent{Kind: EventWritePrimaryCells, Cells: []Cell{
			{Text: "X", Width: 1},
			{Text: "", Width: 3, Style: style},
			{Text: "Y", Width: 1},
		}},
		HistoryEvent{Kind: EventCursorBackward, Count: 1},
		HistoryEvent{Kind: EventWritePrimaryCells, Cells: []Cell{{Text: "Z", Width: 1}}},
	)

	line := requireLine(t, track, 1)
	if got := lineText(line); got != "X   Z" {
		t.Fatalf("mutation should preserve empty styled footprint as spaces, got %q cells=%#v", got, line.Cells)
	}
	for i := 1; i <= 3; i++ {
		if line.Cells[i].Text != " " || line.Cells[i].Width != 1 || line.Cells[i].Style.BG != "idx:24" {
			t.Fatalf("empty styled footprint cell %d should survive mutation, got %#v cells=%#v", i, line.Cells[i], line.Cells)
		}
	}
}

func TestHistoryTrackEraseInLineModeOneClearsMutablePrefix(t *testing.T) {
	track := NewHistoryTrack()
	applyHistoryEvents(t, track,
		HistoryEvent{Kind: EventWritePrimaryCells, Cells: cells("hello")},
		HistoryEvent{Kind: EventCarriageReturn},
		HistoryEvent{Kind: EventWritePrimaryCells, Cells: cells("he")},
		HistoryEvent{Kind: EventEraseInLine, EraseMode: 1},
	)

	line := requireLine(t, track, 1)
	if got := lineText(line); got != "   lo" {
		t.Fatalf("EL 1 should clear active prefix through cursor, got %q", got)
	}
	if got := track.CommittedIDs(); len(got) != 0 {
		t.Fatalf("erase-in-line mode 1 must stay in mutable frontier, got committed %v", got)
	}
}

func TestHistoryTrackEraseInLineModeTwoClearsWholeMutableLine(t *testing.T) {
	track := NewHistoryTrack()
	applyHistoryEvents(t, track,
		HistoryEvent{Kind: EventWritePrimaryCells, Cells: cells("hello")},
		HistoryEvent{Kind: EventCarriageReturn},
		HistoryEvent{Kind: EventWritePrimaryCells, Cells: cells("he")},
		HistoryEvent{Kind: EventEraseInLine, EraseMode: 2},
	)

	line := requireLine(t, track, 1)
	if got := lineText(line); got != "     " {
		t.Fatalf("EL 2 should clear entire mutable line, got %q", got)
	}
	if got := track.CommittedIDs(); len(got) != 0 {
		t.Fatalf("erase-in-line mode 2 must stay in mutable frontier, got committed %v", got)
	}
}

func TestHistoryTrackMutatesReclaimedCommittedSuffixAndRecommitsReplacement(t *testing.T) {
	track := NewHistoryTrack()
	commitLine(t, track, "old")

	applyHistoryEvents(t, track,
		HistoryEvent{Kind: EventReclaimCommittedSuffix, Count: 1},
		HistoryEvent{Kind: EventMutateFrontier, LineID: 1, Cells: cells("new")},
		HistoryEvent{Kind: EventForceCommitFrontier},
	)

	if got := track.CommittedIDs(); !reflect.DeepEqual(got, []LogicalLineID{1}) {
		t.Fatalf("unexpected committed ids %v", got)
	}
	if got := track.FrontierIDs(); len(got) != 0 {
		t.Fatalf("expected empty frontier, got %v", got)
	}
	line := requireLine(t, track, 1)
	if got := lineText(line); got != "new" {
		t.Fatalf("expected replacement payload, got %q", got)
	}
	if line.Dirty {
		t.Fatal("expected force commit to clean replacement")
	}
	if line.Generation < 5 {
		t.Fatalf("expected generation to reflect mutate and recommit, got %d", line.Generation)
	}
}

func TestHistoryTrackResetFrontierDoesNotCreateCommittedHistory(t *testing.T) {
	track := NewHistoryTrack()
	commitLine(t, track, "kept")
	applyHistoryEvents(t, track, HistoryEvent{Kind: EventWritePrimaryCells, Cells: cells("draft")})

	if got := track.FrontierIDs(); !reflect.DeepEqual(got, []LogicalLineID{2}) {
		t.Fatalf("unexpected frontier before reset %v", got)
	}
	applyHistoryEvents(t, track, HistoryEvent{Kind: EventResetFrontier})
	if got := track.CommittedIDs(); !reflect.DeepEqual(got, []LogicalLineID{1}) {
		t.Fatalf("reset should preserve committed history only, got %v", got)
	}
	if got := track.FrontierIDs(); len(got) != 0 {
		t.Fatalf("expected empty frontier after reset, got %v", got)
	}
	if _, ok := track.Line(2); ok {
		t.Fatal("uncommitted reset line should be deleted from store")
	}
}

func TestHistoryTrackEraseDisplayClearScreenCommitsCurrentScreenPage(t *testing.T) {
	track := NewHistoryTrack()
	commitLine(t, track, "kept")
	track.SetPrimaryScreenRows(2)
	applyHistoryEvents(t, track, HistoryEvent{Kind: EventWritePrimaryCells, Cells: cells("draft")})

	applyHistoryEvents(t, track, HistoryEvent{Kind: EventEraseInDisplay, EraseMode: 2})
	if got := track.CommittedIDs(); !reflect.DeepEqual(got, []LogicalLineID{1, 2}) {
		t.Fatalf("ED 2 should commit current screen page before clearing, got %v", got)
	}
	if got := track.FrontierIDs(); len(got) != 0 {
		t.Fatalf("ED 2 should clear frontier ownership after page break, got %v", got)
	}
	line := requireLine(t, track, 2)
	if got := lineText(line); got != "draft" {
		t.Fatalf("ED 2 should preserve clear-screen tail payload, got %q", got)
	}
	if line.Seal != SealStateSealed {
		t.Fatalf("ED 2 should seal open screen line, got %q", line.Seal)
	}
	if line.Dirty {
		t.Fatal("ED 2 should clean committed screen line")
	}

	applyHistoryEvents(t, track, HistoryEvent{Kind: EventWritePrimaryCells, Cells: cells("ui")})
	if got := track.CommittedIDs(); !reflect.DeepEqual(got, []LogicalLineID{1, 2}) {
		t.Fatalf("new UI after ED 2 must not rewrite committed page, got %v", got)
	}
	if got := track.FrontierIDs(); !reflect.DeepEqual(got, []LogicalLineID{3}) {
		t.Fatalf("new UI after ED 2 should start a fresh frontier line, got %v", got)
	}
	if got := lineText(requireLine(t, track, 2)); got != "draft" {
		t.Fatalf("clear-screen page payload should remain stable, got %q", got)
	}
	if got := lineText(requireLine(t, track, 3)); got != "ui" {
		t.Fatalf("new UI should be written to a fresh line, got %q", got)
	}
}

func TestHistoryTrackEraseDisplayFromCursorClearsMutableTailOnly(t *testing.T) {
	track := NewHistoryTrack()
	commitLine(t, track, "kept")
	track.SetPrimaryScreenRows(4)
	applyHistoryEvents(t, track,
		HistoryEvent{Kind: EventWritePrimaryCells, Cells: cells("top")},
		HistoryEvent{Kind: EventSealLogicalLine},
		HistoryEvent{Kind: EventWritePrimaryCells, Cells: cells("bottom")},
		HistoryEvent{Kind: EventCarriageReturn},
		HistoryEvent{Kind: EventWritePrimaryCells, Cells: cells("bo")},
		HistoryEvent{Kind: EventEraseInDisplay, EraseMode: 0},
	)

	if got := track.CommittedIDs(); !reflect.DeepEqual(got, []LogicalLineID{1}) {
		t.Fatalf("ED 0 must not change committed history, got %v", got)
	}
	if got := track.FrontierIDs(); !reflect.DeepEqual(got, []LogicalLineID{2, 3}) {
		t.Fatalf("ED 0 should keep current mutable line and drop only lower mutable lines, got %v", got)
	}
	if got := lineText(requireLine(t, track, 2)); got != "top" {
		t.Fatalf("sealed mutable line above cursor should survive ED 0, got %q", got)
	}
	if got := strings.TrimRight(lineText(requireLine(t, track, 3)), " "); got != "bo" {
		t.Fatalf("ED 0 should clear active mutable tail from cursor down, got %q", got)
	}
}

func TestHistoryTrackEraseDisplayToCursorClearsMutablePrefixOnly(t *testing.T) {
	track := NewHistoryTrack()
	commitLine(t, track, "kept")
	track.SetPrimaryScreenRows(4)
	applyHistoryEvents(t, track,
		HistoryEvent{Kind: EventWritePrimaryCells, Cells: cells("top")},
		HistoryEvent{Kind: EventSealLogicalLine},
		HistoryEvent{Kind: EventWritePrimaryCells, Cells: cells("bottom")},
		HistoryEvent{Kind: EventCarriageReturn},
		HistoryEvent{Kind: EventWritePrimaryCells, Cells: cells("bo")},
		HistoryEvent{Kind: EventEraseInDisplay, EraseMode: 1},
	)

	if got := track.CommittedIDs(); !reflect.DeepEqual(got, []LogicalLineID{1}) {
		t.Fatalf("ED 1 must not change committed history, got %v", got)
	}
	if got := track.FrontierIDs(); !reflect.DeepEqual(got, []LogicalLineID{3}) {
		t.Fatalf("ED 1 should drop mutable lines above cursor and keep active line, got %v", got)
	}
	if _, ok := track.Line(2); ok {
		t.Fatal("ED 1 should delete mutable line above cursor from store")
	}
	if got := lineText(requireLine(t, track, 3)); got != "   tom" {
		t.Fatalf("ED 1 should clear active line prefix through cursor, got %q", got)
	}
}

func TestHistoryTrackTruncateDeletesCommittedOnlyButKeepsMutablePayload(t *testing.T) {
	track := NewHistoryTrack()
	commitLine(t, track, "first")
	commitLine(t, track, "second")
	applyHistoryEvents(t, track, HistoryEvent{Kind: EventReclaimCommittedSuffix, Count: 1})

	applyHistoryEvents(t, track, HistoryEvent{Kind: EventTruncateCommittedHistory, LineIDs: []LogicalLineID{1, 2}})

	if got := track.CommittedIDs(); len(got) != 0 {
		t.Fatalf("expected committed history to be truncated, got %v", got)
	}
	if got := track.FrontierIDs(); !reflect.DeepEqual(got, []LogicalLineID{2}) {
		t.Fatalf("expected reclaimed line to remain mutable, got %v", got)
	}
	if _, ok := track.Line(1); ok {
		t.Fatal("non-frontier truncated line should be deleted")
	}
	if got := lineText(requireLine(t, track, 2)); got != "second" {
		t.Fatalf("frontier payload should survive truncate, got %q", got)
	}

	applyHistoryEvents(t, track, HistoryEvent{Kind: EventForceCommitFrontier})
	if got := track.CommittedIDs(); !reflect.DeepEqual(got, []LogicalLineID{2}) {
		t.Fatalf("expected frontier to recommit to new boundary, got %v", got)
	}
}

func TestHistoryTrackEraseDisplayClearScrollbackTruncatesCommittedHistory(t *testing.T) {
	track := NewHistoryTrack()
	commitLine(t, track, "first")
	commitLine(t, track, "second")
	applyHistoryEvents(t, track, HistoryEvent{Kind: EventWritePrimaryCells, Cells: cells("draft")})

	applyHistoryEvents(t, track, HistoryEvent{Kind: EventEraseInDisplay, EraseMode: 3})
	if got := track.CommittedIDs(); len(got) != 0 {
		t.Fatalf("ED 3 should clear committed history, got %v", got)
	}
	if got := track.FrontierIDs(); !reflect.DeepEqual(got, []LogicalLineID{3}) {
		t.Fatalf("ED 3 must not delete current mutable frontier, got %v", got)
	}
	if got := lineText(requireLine(t, track, 3)); got != "draft" {
		t.Fatalf("ED 3 must preserve mutable payload, got %q", got)
	}
}

func TestHistoryTrackAltScreenDoesNotWritePrimaryHistory(t *testing.T) {
	track := NewHistoryTrack()
	commitLine(t, track, "primary")

	applyHistoryEvents(t, track,
		HistoryEvent{Kind: EventSwitchAltScreen, EnterAltScreen: true},
		HistoryEvent{Kind: EventWritePrimaryCells, Cells: cells("alt")},
		HistoryEvent{Kind: EventCommitFrontier},
	)
	if !track.InAltScreen() {
		t.Fatal("expected alt-screen to stay active")
	}
	if got := track.CommittedIDs(); !reflect.DeepEqual(got, []LogicalLineID{1}) {
		t.Fatalf("alt-screen output must not enter primary history, got %v", got)
	}
	if got := track.LineIDs(); !reflect.DeepEqual(got, []LogicalLineID{1}) {
		t.Fatalf("alt-screen output should not allocate primary lines, got %v", got)
	}

	applyHistoryEvents(t, track,
		HistoryEvent{Kind: EventSwitchAltScreen, EnterAltScreen: false},
		HistoryEvent{Kind: EventWritePrimaryCells, Cells: cells("after")},
	)
	if track.InAltScreen() {
		t.Fatal("expected alt-screen to exit")
	}
	if got := track.FrontierIDs(); !reflect.DeepEqual(got, []LogicalLineID{2}) {
		t.Fatalf("unexpected primary frontier after alt exit %v", got)
	}
}

func TestHistoryTrackEnterAltScreenCommitsPrimaryFrontierFirst(t *testing.T) {
	track := NewHistoryTrack()
	track.SetPrimaryScreenRows(2)
	applyHistoryEvents(t, track,
		HistoryEvent{Kind: EventWritePrimaryCells, Cells: cells("one")},
		HistoryEvent{Kind: EventSealLogicalLine},
		HistoryEvent{Kind: EventWritePrimaryCells, Cells: cells("two")},
		HistoryEvent{Kind: EventSealLogicalLine},
		HistoryEvent{Kind: EventWritePrimaryCells, Cells: cells("three")},
		HistoryEvent{Kind: EventSealLogicalLine},
		HistoryEvent{Kind: EventWritePrimaryCells, Cells: cells("four")},
	)

	applyHistoryEvents(t, track,
		HistoryEvent{Kind: EventSwitchAltScreen, EnterAltScreen: true},
		HistoryEvent{Kind: EventEraseInDisplay, EraseMode: 2},
		HistoryEvent{Kind: EventWritePrimaryCells, Cells: cells("alt")},
		HistoryEvent{Kind: EventSwitchAltScreen, EnterAltScreen: false},
		HistoryEvent{Kind: EventWritePrimaryCells, Cells: cells("after")},
	)

	if got := track.CommittedIDs(); !reflect.DeepEqual(got, []LogicalLineID{1, 2, 3, 4}) {
		t.Fatalf("enter alt-screen should commit primary page first, got %v", got)
	}
	if got := track.FrontierIDs(); !reflect.DeepEqual(got, []LogicalLineID{5}) {
		t.Fatalf("primary output after alt exit should start a fresh frontier, got %v", got)
	}
	if got := lineText(requireLine(t, track, 5)); got != "after" {
		t.Fatalf("alt-screen payload must not enter primary history, got %q", got)
	}
}

func TestHistoryTrackProcessExitForceCommitsPrimaryFrontier(t *testing.T) {
	track := NewHistoryTrack()
	applyHistoryEvents(t, track, HistoryEvent{Kind: EventWritePrimaryCells, Cells: cells("tail")})

	applyHistoryEvents(t, track, HistoryEvent{Kind: EventForceCommitFrontier})

	if got := track.CommittedIDs(); !reflect.DeepEqual(got, []LogicalLineID{1}) {
		t.Fatalf("expected force committed tail, got %v", got)
	}
	line := requireLine(t, track, 1)
	if line.Seal != SealStateSealed {
		t.Fatalf("expected forced line to be sealed, got %q", line.Seal)
	}
	if line.Dirty {
		t.Fatal("expected forced line to be clean")
	}
}

func TestHistoryTrackResizeSemantics(t *testing.T) {
	track := NewHistoryTrack()
	commitLine(t, track, "one")
	commitLine(t, track, "two")
	track.SetPrimaryScreenRows(2)

	before := track.Generation()
	applyHistoryEvents(t, track, HistoryEvent{Kind: EventResize, ResizeDirection: ResizeGrow, Count: 1})
	if track.Generation() == before {
		t.Fatal("grow resize should invalidate generation")
	}
	if got := track.CommittedIDs(); !reflect.DeepEqual(got, []LogicalLineID{1}) {
		t.Fatalf("grow resize should reclaim committed suffix, got %v", got)
	}
	if got := track.FrontierIDs(); !reflect.DeepEqual(got, []LogicalLineID{2}) {
		t.Fatalf("grow resize should expose reclaimed frontier, got %v", got)
	}

	before = track.Generation()
	applyHistoryEvents(t, track, HistoryEvent{Kind: EventResize, ResizeDirection: ResizeShrink, Count: 1})
	if track.Generation() == before {
		t.Fatal("shrink resize should invalidate generation")
	}
	if got := track.HiddenFrontierIDs(); !reflect.DeepEqual(got, []LogicalLineID{2}) {
		t.Fatalf("shrink resize should hide frontier tail, got %v", got)
	}

	before = track.Generation()
	applyHistoryEvents(t, track, HistoryEvent{Kind: EventResize, ResizeDirection: ResizeGrow, Count: 1})
	if track.Generation() == before {
		t.Fatal("grow resize should invalidate generation when revealing hidden frontier")
	}
	if got := track.HiddenFrontierIDs(); len(got) != 0 {
		t.Fatalf("grow resize should reveal hidden frontier before reclaiming committed suffix, got %v", got)
	}
	if got := track.FrontierIDs(); !reflect.DeepEqual(got, []LogicalLineID{2}) {
		t.Fatalf("grow resize should keep revealed frontier mutable, got %v", got)
	}

	before = track.Generation()
	applyHistoryEvents(t, track, HistoryEvent{Kind: EventResize, ResizeDirection: ResizeSame})
	if track.Generation() == before {
		t.Fatal("same-size resize should still invalidate active windows")
	}
	if got := track.CommittedIDs(); !reflect.DeepEqual(got, []LogicalLineID{1}) {
		t.Fatalf("same-size resize must not create history, got %v", got)
	}
}

func TestHistoryTrackGrowResizeReclaimsCommittedSuffixInOrder(t *testing.T) {
	track := NewHistoryTrack()
	commitLine(t, track, "one")
	commitLine(t, track, "two")
	commitLine(t, track, "three")

	applyHistoryEvents(t, track, HistoryEvent{Kind: EventResize, ResizeDirection: ResizeGrow, Count: 2})

	if got := track.CommittedIDs(); !reflect.DeepEqual(got, []LogicalLineID{1}) {
		t.Fatalf("grow resize should keep older committed prefix, got %v", got)
	}
	if got := track.FrontierIDs(); !reflect.DeepEqual(got, []LogicalLineID{2, 3}) {
		t.Fatalf("reclaimed suffix must keep logical order, got %v", got)
	}
	if got := lineText(requireLine(t, track, 2)) + "," + lineText(requireLine(t, track, 3)); got != "two,three" {
		t.Fatalf("unexpected reclaimed line order %q", got)
	}
}

func TestHistoryTrackNonHistoryBoundaryDoesNotCreateCommittedHistory(t *testing.T) {
	track := NewHistoryTrack()
	before := track.Generation()
	applyHistoryEvents(t, track,
		HistoryEvent{Kind: EventNonHistoryBoundary},
		HistoryEvent{Kind: EventNonHistoryBoundary},
	)
	if track.Generation() <= before {
		t.Fatal("non-history boundary should invalidate generation")
	}
	if got := track.CommittedIDs(); len(got) != 0 {
		t.Fatalf("non-history boundary must not create committed history, got %v", got)
	}
	if got := track.LineIDs(); len(got) != 0 {
		t.Fatalf("non-history boundary must not create logical lines, got %v", got)
	}
}

func TestHistoryTrackLatestWindowWrapDoesNotSealOrCommitLogicalLine(t *testing.T) {
	track := NewHistoryTrack()
	applyHistoryEvents(t, track,
		HistoryEvent{Kind: EventWritePrimaryCells, Cells: cells("abcdef")},
	)

	window, err := track.LatestWindow(HistoryWindowRequest{Cols: 3, Rows: 10})
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	if got := rowTextsFromWindow(window.Rows); !reflect.DeepEqual(got, []string{"abc", "def"}) {
		t.Fatalf("expected visual wrap only, got %v", got)
	}
	if got := track.LineIDs(); !reflect.DeepEqual(got, []LogicalLineID{1}) {
		t.Fatalf("auto-wrap must not create a second logical line, got %v", got)
	}
	line := requireLine(t, track, 1)
	if line.Seal != SealStateOpen {
		t.Fatalf("auto-wrap must not seal logical line, got %#v", line)
	}
	if got := track.CommittedIDs(); len(got) != 0 {
		t.Fatalf("auto-wrap must not create committed history, got %v", got)
	}
	if got := track.FrontierIDs(); !reflect.DeepEqual(got, []LogicalLineID{1}) {
		t.Fatalf("auto-wrap must keep logical line mutable in frontier, got %v", got)
	}
}

func TestHistoryTrackRejectsInvalidEventTargets(t *testing.T) {
	track := NewHistoryTrack()
	if err := track.Apply(HistoryEvent{Kind: EventMutateFrontier, LineID: 99}); !errors.Is(err, ErrLineNotMutable) {
		t.Fatalf("expected ErrLineNotMutable, got %v", err)
	}
	if err := track.Apply(HistoryEvent{Kind: EventReclaimCommittedSuffix, LineIDs: []LogicalLineID{99}}); !errors.Is(err, ErrLineNotCommitted) {
		t.Fatalf("expected ErrLineNotCommitted, got %v", err)
	}
	if err := track.Apply(HistoryEvent{Kind: EventResize, ResizeDirection: ResizeDirection("diagonal")}); !errors.Is(err, ErrInvalidResizeDirection) {
		t.Fatalf("expected ErrInvalidResizeDirection, got %v", err)
	}
	if err := track.Apply(HistoryEvent{}); !errors.Is(err, ErrInvalidEventKind) {
		t.Fatalf("expected ErrInvalidEventKind, got %v", err)
	}
}

func commitLine(t *testing.T, track *HistoryTrack, text string) LogicalLineID {
	t.Helper()
	applyHistoryEvents(t, track,
		HistoryEvent{Kind: EventWritePrimaryCells, Cells: cells(text)},
		HistoryEvent{Kind: EventSealLogicalLine},
		HistoryEvent{Kind: EventCommitFrontier},
	)
	ids := track.CommittedIDs()
	if len(ids) == 0 {
		t.Fatal("expected committed line")
	}
	return ids[len(ids)-1]
}

func applyHistoryEvents(t *testing.T, track *HistoryTrack, events ...HistoryEvent) {
	t.Helper()
	for _, event := range events {
		if err := track.Apply(event); err != nil {
			t.Fatalf("apply %s: %v", event.Kind, err)
		}
	}
}

func requireLine(t *testing.T, track *HistoryTrack, id LogicalLineID) LogicalLine {
	t.Helper()
	line, ok := track.Line(id)
	if !ok {
		t.Fatalf("expected line %d", id)
	}
	return line
}

func cells(text string) []Cell {
	return []Cell{{Text: text}}
}

func lineText(line LogicalLine) string {
	var text string
	for _, cell := range line.Cells {
		text += cell.Text
	}
	return text
}

func rowTextsFromWindow(rows []VisualRow) []string {
	texts := make([]string, len(rows))
	for i, row := range rows {
		texts[i] = row.Text
	}
	return texts
}
