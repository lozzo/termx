package history

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

func TestFileHistoryStorageBackendReplaysDomainTransactions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history-state.log")
	backend, err := NewFileHistoryStorageBackend(path)
	if err != nil {
		t.Fatalf("create file history backend: %v", err)
	}
	committedLine := LogicalLine{
		ID:         1,
		Generation: 1,
		Seal:       SealStateSealed,
		Cells:      cells("committed"),
		Residency:  ResidencyMemory,
	}
	frontierLine := LogicalLine{
		ID:         2,
		Generation: 1,
		Seal:       SealStateOpen,
		Cells:      cells("frontier"),
		Dirty:      true,
		Residency:  ResidencyFile,
	}
	if err := backend.ApplyHistoryStorageTransaction(HistoryStorageTransaction{
		ReplaceLines:            true,
		UpsertLines:             []LogicalLine{committedLine, frontierLine},
		SetGeneration:           true,
		Generation:              7,
		ReplaceCommittedIndex:   true,
		CommittedIDs:            []LogicalLineID{1},
		ReplaceMutableFrontier:  true,
		FrontierIDs:             []LogicalLineID{2},
		HiddenFrontierIDs:       []LogicalLineID{2},
		ReplaceFrameJournal:     true,
		PublishedFrameLineIDs:   []LogicalLineID{2},
		ReplaceTrackState:       true,
		ActiveLine:              2,
		ActiveCol:               8,
		PrimaryFullscreenIntent: true,
		PrimaryFullscreenModes:  []int{1003, 25},
		ScreenRows:              2,
		ScreenRow:               1,
		ScreenLineIDs:           []LogicalLineID{1, 2},
	}); err != nil {
		t.Fatalf("apply initial transaction: %v", err)
	}
	frontierLine.Generation = 2
	frontierLine.Cells = cells("frontier updated")
	if err := backend.ApplyHistoryStorageTransaction(HistoryStorageTransaction{
		UpsertLines:          []LogicalLine{frontierLine},
		SetGeneration:        true,
		Generation:           8,
		ReplaceFrameJournal:  true,
		ArchivedFrameLineIDs: []LogicalLineID{2},
	}); err != nil {
		t.Fatalf("apply update transaction: %v", err)
	}

	reopened, err := NewFileHistoryStorageBackend(path)
	if err != nil {
		t.Fatalf("reopen file history backend: %v", err)
	}
	snapshot, err := reopened.RecoverHistoryStorageSnapshot()
	if err != nil {
		t.Fatalf("recover snapshot: %v", err)
	}

	if snapshot.Generation != 8 {
		t.Fatalf("expected generation 8, got %d", snapshot.Generation)
	}
	if got := snapshot.CommittedIDs; !reflect.DeepEqual(got, []LogicalLineID{1}) {
		t.Fatalf("unexpected committed ids %v", got)
	}
	if got := snapshot.FrontierIDs; !reflect.DeepEqual(got, []LogicalLineID{2}) {
		t.Fatalf("unexpected frontier ids %v", got)
	}
	if got := snapshot.HiddenFrontierIDs; !reflect.DeepEqual(got, []LogicalLineID{2}) {
		t.Fatalf("unexpected hidden frontier ids %v", got)
	}
	if got := snapshot.ArchivedFrameLineIDs; !reflect.DeepEqual(got, []LogicalLineID{2}) {
		t.Fatalf("unexpected archived frame ids %v", got)
	}
	if got := snapshot.PublishedFrameLineIDs; len(got) != 0 {
		t.Fatalf("second transaction should replace published journal with empty list, got %v", got)
	}
	if got := snapshot.ScreenLineIDs; !reflect.DeepEqual(got, []LogicalLineID{1, 2}) {
		t.Fatalf("unexpected screen line ids %v", got)
	}
	if len(snapshot.Lines) != 2 || lineText(snapshot.Lines[1]) != "frontier updated" {
		t.Fatalf("expected updated line payload, got %#v", snapshot.Lines)
	}
	if snapshot.Lines[1].Residency != ResidencyFile {
		t.Fatalf("storage recovery must preserve residency as payload metadata, got %q", snapshot.Lines[1].Residency)
	}
}

func TestHistoryStorageSnapshotRoundTripPreservesWindowBoundaries(t *testing.T) {
	track := NewHistoryTrack()
	track.SetPrimaryScreenRows(4)
	applyHistoryEvents(t, track,
		HistoryEvent{Kind: EventWritePrimaryCells, Cells: cells("shell before codex")},
		HistoryEvent{Kind: EventForceCommitFrontier},
		HistoryEvent{Kind: EventBeginSynchronizedFrame},
		HistoryEvent{Kind: EventEndSynchronizedFrame},
		HistoryEvent{Kind: EventReplacePrimaryFrame, Rows: [][]Cell{cells("codex frame one")}},
		HistoryEvent{Kind: EventBeginSynchronizedFrame},
		HistoryEvent{Kind: EventEndSynchronizedFrame},
		HistoryEvent{Kind: EventReplacePrimaryFrame, Rows: [][]Cell{cells("codex frame two")}},
		HistoryEvent{Kind: EventForceCommitFrontier},
	)
	snapshot := track.ExportStorageSnapshot()
	path := filepath.Join(t.TempDir(), "history-state.log")
	backend, err := NewFileHistoryStorageBackend(path)
	if err != nil {
		t.Fatalf("create file history backend: %v", err)
	}
	if err := backend.ApplyHistoryStorageTransaction(snapshot.FullTransaction()); err != nil {
		t.Fatalf("persist snapshot transaction: %v", err)
	}
	recoveredSnapshot, err := backend.RecoverHistoryStorageSnapshot()
	if err != nil {
		t.Fatalf("recover snapshot: %v", err)
	}
	recovered, err := NewHistoryTrackFromStorageSnapshot(recoveredSnapshot)
	if err != nil {
		t.Fatalf("restore history track: %v", err)
	}

	latest, err := recovered.LatestWindow(HistoryWindowRequest{Cols: 80, Rows: 1})
	if err != nil {
		t.Fatalf("latest after restore: %v", err)
	}
	if got := rowTexts(latest.Rows); !reflect.DeepEqual(got, []string{"codex frame two"}) {
		t.Fatalf("latest should preserve final screen-frame, got %v rows=%#v", got, latest.Rows)
	}
	if !latest.Rows[0].Committed || latest.Rows[0].Kind != RowKindScreenFrame {
		t.Fatalf("final frame should stay committed screen-frame, got %#v", latest.Rows[0])
	}
	if latest.Generation != track.Generation() {
		t.Fatalf("restored window generation mismatch got=%d want=%d", latest.Generation, track.Generation())
	}

	shell, err := recovered.OlderWindow(HistoryWindowRequest{Cols: 80, Rows: 10, Cursor: latest.Cursor})
	if err != nil {
		t.Fatalf("older shell after restore: %v", err)
	}
	if got := rowTexts(shell.Rows); !reflect.DeepEqual(got, []string{"shell before codex"}) {
		t.Fatalf("older should skip replaced repaint frame and reach committed shell history, got %v rows=%#v", got, shell.Rows)
	}
	if shell.HasMore {
		t.Fatalf("shell page should exhaust history, cursor=%#v rows=%#v", shell.Cursor, shell.Rows)
	}
}

func TestHistoryStorageRecoveryDoesNotInferMutabilityFromResidency(t *testing.T) {
	snapshot := HistoryStorageSnapshot{
		Generation: 3,
		Lines: []LogicalLine{
			{ID: 1, Generation: 1, Seal: SealStateSealed, Cells: cells("file frontier"), Residency: ResidencyFile},
			{ID: 2, Generation: 1, Seal: SealStateSealed, Cells: cells("memory committed"), Residency: ResidencyMemory},
		},
		CommittedIDs:  []LogicalLineID{2},
		FrontierIDs:   []LogicalLineID{1},
		ScreenRows:    1,
		ScreenRow:     0,
		ScreenLineIDs: []LogicalLineID{1},
	}
	restored, err := NewHistoryTrackFromStorageSnapshot(snapshot)
	if err != nil {
		t.Fatalf("restore snapshot: %v", err)
	}

	if got := restored.CommittedIDs(); !reflect.DeepEqual(got, []LogicalLineID{2}) {
		t.Fatalf("committed index must come from transaction, not residency, got %v", got)
	}
	if got := restored.FrontierIDs(); !reflect.DeepEqual(got, []LogicalLineID{1}) {
		t.Fatalf("frontier must come from transaction, not residency, got %v", got)
	}

	line, ok := restored.Line(1)
	if !ok {
		t.Fatal("expected restored frontier line")
	}
	line.Cells = cells("mutated")
	replaced, err := restored.store.ReplaceLine(line)
	if err != nil {
		t.Fatalf("file-resident frontier line should remain replaceable by domain semantics: %v", err)
	}
	if got := lineText(replaced); got != "mutated" {
		t.Fatalf("unexpected replaced frontier payload %q", got)
	}
}

func TestHistoryStorageRejectsUnknownIndexReferences(t *testing.T) {
	backend := NewMemoryHistoryStorageBackend()
	err := backend.ApplyHistoryStorageTransaction(HistoryStorageTransaction{
		ReplaceCommittedIndex: true,
		CommittedIDs:          []LogicalLineID{99},
	})
	if !errors.Is(err, ErrUnknownLine) {
		t.Fatalf("expected ErrUnknownLine for index without payload, got %v", err)
	}
}
