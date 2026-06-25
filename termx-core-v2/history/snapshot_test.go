package history

import "testing"

func TestPinnedFrozenSnapshotDoesNotMaterializeCommittedPayloadLines(t *testing.T) {
	track := NewHistoryTrack()
	for i := 0; i < 3; i++ {
		if err := track.Apply(HistoryEvent{Kind: EventWritePrimaryCells, Cells: []Cell{{Text: string(rune('a' + i)), Width: 1}}}); err != nil {
			t.Fatalf("write: %v", err)
		}
		if err := track.Apply(HistoryEvent{Kind: EventForceCommitFrontier}); err != nil {
			t.Fatalf("commit: %v", err)
		}
	}

	snapshot := track.FreezePinnedSnapshot()
	if len(snapshot.Lines) != 0 {
		t.Fatalf("pinned snapshot must not hold committed payload lines, got %d", len(snapshot.Lines))
	}
	if len(snapshot.CommittedIDs) != 0 {
		t.Fatalf("pinned snapshot must not hold the full committed id list, got %d", len(snapshot.CommittedIDs))
	}
	if snapshot.VisibleLineCount() != 3 || snapshot.CommittedLines != 3 {
		t.Fatalf("unexpected snapshot boundary, count=%d committed=%d", snapshot.VisibleLineCount(), snapshot.CommittedLines)
	}
	line, ok := snapshot.LineAt(2)
	if !ok || line.Line.ID != 3 || line.Line.Cells[0].Text != "c" || !line.Committed {
		t.Fatalf("pinned snapshot should load committed line on demand, got %#v ok=%v", line, ok)
	}
	snapshot.ReleaseObserver()
}

func TestPinnedFrozenSnapshotKeepsWholeAltScreenTransientFrame(t *testing.T) {
	track := NewHistoryTrack()
	commitLine(t, track, "committed")
	applyHistoryEvents(t, track,
		HistoryEvent{Kind: EventSwitchAltScreen, EnterAltScreen: true},
		HistoryEvent{Kind: EventAppendAltScreenFrame, Rows: [][]Cell{
			cells("/resume"),
			cells("restored conversation"),
		}},
	)

	snapshot := track.FreezePinnedSnapshot()
	defer snapshot.ReleaseObserver()

	if snapshot.VisibleLineCount() != 3 || snapshot.CommittedLines != 1 || len(snapshot.FrozenFrontier) != 2 {
		t.Fatalf("snapshot should pin committed history plus whole transient frame, count=%d committed=%d frontier=%d", snapshot.VisibleLineCount(), snapshot.CommittedLines, len(snapshot.FrozenFrontier))
	}
	first, ok := snapshot.LineAt(1)
	if !ok || lineText(first.Line) != "/resume" || first.Committed {
		t.Fatalf("expected first transient frame line, got %#v ok=%v", first, ok)
	}
	second, ok := snapshot.LineAt(2)
	if !ok || lineText(second.Line) != "restored conversation" || second.Committed {
		t.Fatalf("expected second transient frame line, got %#v ok=%v", second, ok)
	}
}

func TestPinnedFrozenSnapshotSeesDeletedCommittedLineUntilReleased(t *testing.T) {
	store := NewMemoryLogicalLineStore(nil)
	track := NewHistoryTrackWith(store, nil, nil)
	commitLine(t, track, "first")
	commitLine(t, track, "second")

	snapshot := track.FreezePinnedSnapshot()
	applyHistoryEvents(t, track, HistoryEvent{Kind: EventTruncateCommittedHistory, LineIDs: []LogicalLineID{1}})

	if _, ok := track.Line(1); ok {
		t.Fatal("new readers must not see truncated committed line")
	}
	line, ok := snapshot.LineAt(0)
	if !ok || lineText(line.Line) != "first" || !line.Committed {
		t.Fatalf("old observer should still see deleted line, got %#v ok=%v", line, ok)
	}
	if got := store.RetainedLineCount(); got != 1 {
		t.Fatalf("expected one retained deleted payload, got %d", got)
	}
	snapshot.ReleaseObserver()
	if got := store.RetainedLineCount(); got != 0 {
		t.Fatalf("release should clean retained deleted payload, got %d", got)
	}
}

func TestPinnedFrozenSnapshotKeepsOldVersionAcrossReplace(t *testing.T) {
	store := NewMemoryLogicalLineStore(nil)
	track := NewHistoryTrackWith(store, nil, nil)
	commitLine(t, track, "old")

	snapshot := track.FreezePinnedSnapshot()
	line, ok := track.Line(1)
	if !ok {
		t.Fatal("expected committed line")
	}
	line.Cells = cells("new")
	line.Dirty = true
	if _, err := store.ReplaceLine(line); err != nil {
		t.Fatalf("replace line: %v", err)
	}

	oldLine, ok := snapshot.LineAt(0)
	if !ok || lineText(oldLine.Line) != "old" {
		t.Fatalf("old observer should see old version, got %#v ok=%v", oldLine, ok)
	}
	current, ok := track.Line(1)
	if !ok || lineText(current) != "new" {
		t.Fatalf("new reader should see replaced version, got %#v ok=%v", current, ok)
	}
	if got := store.RetainedLineCount(); got != 1 {
		t.Fatalf("expected one retained old version, got %d", got)
	}
	snapshot.ReleaseObserver()
	if got := store.RetainedLineCount(); got != 0 {
		t.Fatalf("release should clean retained old version, got %d", got)
	}
}

func TestPinnedFrozenSnapshotKeepsOldVersionAcrossTrackMutation(t *testing.T) {
	store := NewMemoryLogicalLineStore(nil)
	track := NewHistoryTrackWith(store, nil, nil)
	commitLine(t, track, "old")

	oldSnapshot := track.FreezePinnedSnapshot()
	applyHistoryEvents(t, track,
		HistoryEvent{Kind: EventReclaimCommittedSuffix, Count: 1},
		HistoryEvent{Kind: EventMutateFrontier, LineID: 1, Cells: cells("new")},
	)
	newSnapshot := track.FreezePinnedSnapshot()

	oldLine, ok := oldSnapshot.LineAt(0)
	if !ok || lineText(oldLine.Line) != "old" {
		t.Fatalf("old observer should keep old version, got %#v ok=%v", oldLine, ok)
	}
	newLine, ok := newSnapshot.LineAt(0)
	if !ok || lineText(newLine.Line) != "new" {
		t.Fatalf("new observer should see mutated version, got %#v ok=%v", newLine, ok)
	}
	if got := store.RetainedLineCount(); got != 1 {
		t.Fatalf("expected one retained version while old observer is active, got %d", got)
	}
	oldSnapshot.ReleaseObserver()
	if got := store.RetainedLineCount(); got != 0 {
		t.Fatalf("old observer release should cleanup retained old version, got %d", got)
	}
	newSnapshot.ReleaseObserver()
}

func TestPinnedFrozenSnapshotSelectsVersionByObserverEpoch(t *testing.T) {
	store := NewMemoryLogicalLineStore(nil)
	track := NewHistoryTrackWith(store, nil, nil)
	commitLine(t, track, "old")

	oldSnapshot := track.FreezePinnedSnapshot()
	line, _ := track.Line(1)
	line.Cells = cells("mid")
	if _, err := store.ReplaceLine(line); err != nil {
		t.Fatalf("replace mid: %v", err)
	}
	midSnapshot := track.FreezePinnedSnapshot()
	line, _ = track.Line(1)
	line.Cells = cells("new")
	if _, err := store.ReplaceLine(line); err != nil {
		t.Fatalf("replace new: %v", err)
	}
	newSnapshot := track.FreezePinnedSnapshot()

	oldLine, ok := oldSnapshot.LineAt(0)
	if !ok || lineText(oldLine.Line) != "old" {
		t.Fatalf("old observer should see old version, got %#v ok=%v", oldLine, ok)
	}
	midLine, ok := midSnapshot.LineAt(0)
	if !ok || lineText(midLine.Line) != "mid" {
		t.Fatalf("mid observer should see mid version, got %#v ok=%v", midLine, ok)
	}
	newLine, ok := newSnapshot.LineAt(0)
	if !ok || lineText(newLine.Line) != "new" {
		t.Fatalf("new observer should see current version, got %#v ok=%v", newLine, ok)
	}
	if got := store.RetainedLineCount(); got != 2 {
		t.Fatalf("expected two retained versions while old/mid observers are active, got %d", got)
	}
	oldSnapshot.ReleaseObserver()
	if got := store.RetainedLineCount(); got != 1 {
		t.Fatalf("old release should keep only mid version, got %d", got)
	}
	midSnapshot.ReleaseObserver()
	if got := store.RetainedLineCount(); got != 0 {
		t.Fatalf("mid release should cleanup all retained versions, got %d", got)
	}
	newSnapshot.ReleaseObserver()
}

func TestPinnedFrozenSnapshotDropsUnobservedIntermediateVersions(t *testing.T) {
	store := NewMemoryLogicalLineStore(nil)
	track := NewHistoryTrackWith(store, nil, nil)
	commitLine(t, track, "old")

	snapshot := track.FreezePinnedSnapshot()
	for _, text := range []string{"mid-1", "mid-2", "new"} {
		line, ok := track.Line(1)
		if !ok {
			t.Fatal("expected current line")
		}
		line.Cells = cells(text)
		if _, err := store.ReplaceLine(line); err != nil {
			t.Fatalf("replace %q: %v", text, err)
		}
	}

	oldLine, ok := snapshot.LineAt(0)
	if !ok || lineText(oldLine.Line) != "old" {
		t.Fatalf("old observer should keep entry-time version, got %#v ok=%v", oldLine, ok)
	}
	current, ok := track.Line(1)
	if !ok || lineText(current) != "new" {
		t.Fatalf("current reader should see newest version, got %#v ok=%v", current, ok)
	}
	if got := store.RetainedLineCount(); got != 1 {
		t.Fatalf("only entry-time version should be retained for one old observer, got %d", got)
	}
	snapshot.ReleaseObserver()
	if got := store.RetainedLineCount(); got != 0 {
		t.Fatalf("release should cleanup retained version, got %d", got)
	}
}

func TestPinnedFrozenSnapshotAtGenerationExcludesFutureLines(t *testing.T) {
	track := NewHistoryTrack()
	commitLine(t, track, "visible")
	boundary := track.Generation()
	commitLine(t, track, "future")

	snapshot := track.FreezePinnedSnapshotAtGeneration(boundary)
	defer snapshot.ReleaseObserver()
	if got := snapshot.VisibleLineCount(); got != 1 {
		t.Fatalf("expected only boundary-visible line, got %d", got)
	}
	line, ok := snapshot.LineAt(0)
	if !ok || lineText(line.Line) != "visible" {
		t.Fatalf("expected boundary snapshot to keep visible line, got %#v ok=%v", line, ok)
	}
	if _, ok := snapshot.LineAt(1); ok {
		t.Fatal("boundary snapshot must not expose line created after copy entry")
	}
}

func TestPinnedFrozenSnapshotAtCurrentGenerationDoesNotScanCommittedPayload(t *testing.T) {
	track := NewHistoryTrackWith(&countingLineStore{lines: make(map[LogicalLineID]LogicalLine)}, NewCommittedHistoryIndex(), NewMutableFrontier())
	for i := 0; i < 1000; i++ {
		commitLine(t, track, "x")
	}
	store := track.store.(*countingLineStore)
	store.loads = 0

	snapshot := track.FreezePinnedSnapshotAtGeneration(track.Generation())
	defer snapshot.ReleaseObserver()
	if got := snapshot.VisibleLineCount(); got != 1000 {
		t.Fatalf("expected all current committed lines, got %d", got)
	}
	if store.loads != 0 {
		t.Fatalf("current-generation freeze must not decode committed payload, loaded %d lines", store.loads)
	}
}
