package history

import (
	"fmt"
	"strings"
	"testing"
)

func TestR341BackendStorePagesSealedTimelineWithoutMaterializingAllRows(t *testing.T) {
	backend := newCountingLineBackend()
	store := NewBackendHistoryStore("term-r341", backend)
	const total = 20000
	mutations := make([]HistoryMutation, 0, total*2)
	for i := 1; i <= total; i++ {
		mutations = append(mutations, sealedLineMutations(LogicalLineID(i), HistoryRecordID(i), fmt.Sprintf("line-%06d", i))...)
	}
	if err := store.Apply(HistoryMutationBatch{Mutations: mutations}); err != nil {
		t.Fatalf("apply large history batch: %v", err)
	}
	raw := store.(*inMemoryHistoryStore)
	if got := len(raw.lines); got != 0 {
		t.Fatalf("backend store must not retain sealed payloads in hot memory, got %d lines", got)
	}
	if got := backend.maxBatch; got != total {
		t.Fatalf("backend should receive changed payloads once for this batch, got max batch %d", got)
	}
	backend.resetCounts()

	latest, err := store.LatestWindow(HistoryWindowRequest{TerminalID: "term-r341", Limit: 5, Cols: 80})
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	if got := strings.Join(rowTexts(latest.Rows), "|"); got != "line-019996|line-019997|line-019998|line-019999|line-020000" {
		t.Fatalf("latest should return tail rows, got %q", got)
	}
	if backend.getLineCount != 5 || backend.recoverCount != 0 {
		t.Fatalf("latest must read only window payloads from backend, get=%d recover=%d", backend.getLineCount, backend.recoverCount)
	}
	if !latest.Boundary.Cursor.Valid || latest.Boundary.Cursor.BeforeRowIndex != total-5 {
		t.Fatalf("latest cursor should point before returned tail, cursor=%#v", latest.Boundary.Cursor)
	}

	backend.resetCounts()
	older, err := store.OlderWindow(HistoryWindowRequest{
		TerminalID: "term-r341",
		Limit:      5,
		Cols:       80,
		Cursor:     latest.Boundary.Cursor,
	})
	if err != nil {
		t.Fatalf("older window: %v", err)
	}
	if got := strings.Join(rowTexts(older.Rows), "|"); got != "line-019991|line-019992|line-019993|line-019994|line-019995" {
		t.Fatalf("older should return previous rows, got %q", got)
	}
	if backend.getLineCount != 5 || backend.recoverCount != 0 {
		t.Fatalf("older must read only window payloads from backend, get=%d recover=%d", backend.getLineCount, backend.recoverCount)
	}
}

func TestR362BackendStoreCompactsOrdinaryTimelineMetadata(t *testing.T) {
	backend := newCountingLineBackend()
	store := NewBackendHistoryStore("term-r362", backend)
	const total = 4096
	for i := 1; i <= total; i++ {
		applyStoreBatch(t, store, sealedLineMutations(LogicalLineID(i), HistoryRecordID(i), fmt.Sprintf("line-%04d", i)))
	}
	raw := store.(*inMemoryHistoryStore)
	if got := len(raw.lines); got != 0 {
		t.Fatalf("backend store must not retain sealed payloads in hot memory, got %d lines", got)
	}
	if got := len(raw.timeline); got != 1 {
		t.Fatalf("ordinary sealed timeline metadata should compact into one record, got %d", got)
	}
	if got := len(raw.timeline[0].LineIDs); got != total {
		t.Fatalf("compacted ordinary timeline record should reference every logical line, got %d", got)
	}
	if raw.nextRecordID != HistoryRecordID(total+1) || raw.nextLineID != LogicalLineID(total+1) {
		t.Fatalf("compaction must still observe incoming ids, nextRecord=%d nextLine=%d", raw.nextRecordID, raw.nextLineID)
	}

	backend.resetCounts()
	latest, err := store.LatestWindow(HistoryWindowRequest{TerminalID: "term-r362", Limit: 3, Cols: 80})
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	if got := strings.Join(rowTexts(latest.Rows), "|"); got != "line-4094|line-4095|line-4096" {
		t.Fatalf("latest should still read tail rows from compacted metadata, got %q", got)
	}
	if backend.getLineCount != 3 {
		t.Fatalf("latest should only load visible payloads, get=%d", backend.getLineCount)
	}

	backend.resetCounts()
	older, err := store.OlderWindow(HistoryWindowRequest{
		TerminalID: "term-r362",
		Limit:      3,
		Cols:       80,
		Cursor:     latest.Boundary.Cursor,
	})
	if err != nil {
		t.Fatalf("older window: %v", err)
	}
	if got := strings.Join(rowTexts(older.Rows), "|"); got != "line-4091|line-4092|line-4093" {
		t.Fatalf("older should page through compacted metadata, got %q", got)
	}
	if backend.getLineCount != 3 {
		t.Fatalf("older should only load visible payloads, get=%d", backend.getLineCount)
	}

	backend.resetCounts()
	snapshot, err := store.Freeze(FreezeHistoryRequest{TerminalID: "term-r362", Limit: 3, Cols: 80})
	if err != nil {
		t.Fatalf("freeze: %v", err)
	}
	frozen := raw.frozen[snapshot.Token]
	if len(frozen.projection.records) != 1 || frozen.projection.total != total {
		t.Fatalf("frozen copy token must keep compact projection metadata, records=%d total=%d", len(frozen.projection.records), frozen.projection.total)
	}
	if backend.getLineCount != 3 {
		t.Fatalf("freeze should only load latest visible payloads, get=%d", backend.getLineCount)
	}

	backend.resetCounts()
	oldest, err := store.OldestWindow(HistoryWindowRequest{TerminalID: "term-r362", Token: snapshot.Token, Limit: 3, Cols: 80})
	if err != nil {
		t.Fatalf("frozen oldest: %v", err)
	}
	if got := strings.Join(rowTexts(oldest.Rows), "|"); got != "line-0001|line-0002|line-0003" {
		t.Fatalf("frozen oldest should read projection head from compact metadata, got %q", got)
	}
	if backend.getLineCount != 3 {
		t.Fatalf("frozen oldest should only load visible payloads, get=%d", backend.getLineCount)
	}
}

func TestR341BackendStoreFrozenTokenDoesNotCopyWholeProjection(t *testing.T) {
	backend := newCountingLineBackend()
	store := NewBackendHistoryStore("term-r341-freeze", backend)
	for i := 1; i <= 1000; i++ {
		applyStoreBatch(t, store, sealedLineMutations(LogicalLineID(i), HistoryRecordID(i), fmt.Sprintf("line-%04d", i)))
	}
	backend.resetCounts()
	snapshot, err := store.Freeze(FreezeHistoryRequest{TerminalID: "term-r341-freeze", Limit: 4, Cols: 80})
	if err != nil {
		t.Fatalf("freeze: %v", err)
	}
	raw := store.(*inMemoryHistoryStore)
	frozen := raw.frozen[snapshot.Token]
	if len(frozen.rows) != 4 {
		t.Fatalf("frozen token should keep latest rows only, got %d", len(frozen.rows))
	}
	if len(frozen.projection.records) != 1 || frozen.projection.total != 1000 {
		t.Fatalf("frozen token should keep compact projection metadata, records=%d total=%d", len(frozen.projection.records), frozen.projection.total)
	}
	if got := len(frozen.projection.records[0].LineIDs); got != 1000 {
		t.Fatalf("compacted frozen projection should keep logical line ids in one range record, got %d", got)
	}
	if backend.getLineCount != 4 {
		t.Fatalf("freeze should read only latest window payloads, get=%d", backend.getLineCount)
	}

	backend.resetCounts()
	latest, err := store.LatestWindow(HistoryWindowRequest{TerminalID: "term-r341-freeze", Token: snapshot.Token, Limit: 4, Cols: 80})
	if err != nil {
		t.Fatalf("frozen latest: %v", err)
	}
	if got := strings.Join(rowTexts(latest.Rows), "|"); got != "line-0997|line-0998|line-0999|line-1000" {
		t.Fatalf("frozen latest should use token index, got %q", got)
	}
	if backend.getLineCount != 4 {
		t.Fatalf("frozen latest should read only latest payloads, get=%d", backend.getLineCount)
	}
}

type countingLineBackend struct {
	lines        map[LogicalLineID]LogicalLine
	getLineCount int
	recoverCount int
	maxBatch     int
}

func newCountingLineBackend() *countingLineBackend {
	return &countingLineBackend{lines: make(map[LogicalLineID]LogicalLine)}
}

func (backend *countingLineBackend) Apply(tx StorageTransaction) error {
	if len(tx.Lines) > backend.maxBatch {
		backend.maxBatch = len(tx.Lines)
	}
	for _, line := range tx.Lines {
		backend.lines[line.ID] = cloneLogicalLine(line)
	}
	for _, id := range tx.Tombstones {
		delete(backend.lines, id)
	}
	return nil
}

func (backend *countingLineBackend) Recover() (RecoveredHistoryState, error) {
	backend.recoverCount++
	return RecoveredHistoryState{}, nil
}

func (backend *countingLineBackend) Compact(StorageCompactionPolicy) error {
	return nil
}

func (backend *countingLineBackend) GetLine(id LogicalLineID) (LogicalLine, bool) {
	backend.getLineCount++
	line, ok := backend.lines[id]
	if !ok {
		return LogicalLine{}, false
	}
	return cloneLogicalLine(line), true
}

func (backend *countingLineBackend) GetLines(ids []LogicalLineID) ([]LogicalLine, error) {
	lines := make([]LogicalLine, 0, len(ids))
	for _, id := range ids {
		line, ok := backend.GetLine(id)
		if ok {
			lines = append(lines, line)
		}
	}
	return lines, nil
}

func (backend *countingLineBackend) resetCounts() {
	backend.getLineCount = 0
	backend.recoverCount = 0
}
