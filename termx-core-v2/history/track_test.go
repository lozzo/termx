package history

import (
	"errors"
	"reflect"
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
	applyHistoryEvents(t, track, HistoryEvent{Kind: EventResize, ResizeDirection: ResizeSame})
	if track.Generation() == before {
		t.Fatal("same-size resize should still invalidate active windows")
	}
	if got := track.CommittedIDs(); !reflect.DeepEqual(got, []LogicalLineID{1}) {
		t.Fatalf("same-size resize must not create history, got %v", got)
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
