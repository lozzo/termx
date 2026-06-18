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
}
