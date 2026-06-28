package history

import (
	"os"
	"strings"
	"testing"
)

func TestR323InMemoryStoreLatestWindowIncludesSealedTailAndActiveFrame(t *testing.T) {
	store := NewInMemoryHistoryStore("term-1")
	applyStoreBatch(t, store,
		sealedLineMutations(1, 1, "old"),
		sealedLineMutations(2, 2, "tail"),
		replacePrimaryMutation(10, 1, 80, "current"),
	)

	window, err := store.LatestWindow(HistoryWindowRequest{TerminalID: "term-1", Limit: 2, Cols: 80})
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	if got := rowTexts(window.Rows); strings.Join(got, "|") != "tail|current" {
		t.Fatalf("latest must be sealed tail + active current frame, got %v window=%#v", got, window)
	}
	if window.Rows[0].Segment != HistorySegmentCommitted || !window.Rows[0].Committed {
		t.Fatalf("sealed row lost timeline segment: %#v", window.Rows[0])
	}
	if window.Rows[1].Segment != HistorySegmentCurrentPrimaryFrame || window.Rows[1].Committed {
		t.Fatalf("current frame row lost mutable segment: %#v", window.Rows[1])
	}
}

func TestR323InMemoryStoreOlderWalksUnifiedTimelineWithArchiveInOrder(t *testing.T) {
	store := NewInMemoryHistoryStore("term-1")
	applyStoreBatch(t, store,
		sealedLineMutations(1, 1, "before"),
		archivedFrameMutationsForStore(20, 2, 80, "archive"),
		sealedLineMutations(3, 3, "after"),
		replacePrimaryMutation(30, 4, 80, "current"),
	)

	latest, err := store.LatestWindow(HistoryWindowRequest{TerminalID: "term-1", Limit: 2, Cols: 80})
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	if got := rowTexts(latest.Rows); strings.Join(got, "|") != "after|current" {
		t.Fatalf("latest must not append archive at tail, got %v", got)
	}
	older, err := store.OlderWindow(HistoryWindowRequest{TerminalID: "term-1", Limit: 2, Cols: 80, Cursor: latest.Boundary.Cursor})
	if err != nil {
		t.Fatalf("older window: %v", err)
	}
	if got := rowTexts(older.Rows); strings.Join(got, "|") != "before|archive" {
		t.Fatalf("older must walk unified timeline and include archive at sequence position, got %v window=%#v", got, older)
	}
	if older.Rows[1].Segment != HistorySegmentArchivedPrimaryFrame || older.Rows[1].FrameID != 20 {
		t.Fatalf("archive row lost segment identity: %#v", older.Rows[1])
	}
}

func TestR323InMemoryStoreFreezeKeepsMutableFrameSnapshot(t *testing.T) {
	store := NewInMemoryHistoryStore("term-1")
	applyStoreBatch(t, store, sealedLineMutations(1, 1, "sealed"), replacePrimaryMutation(10, 2, 80, "v1"))

	snapshot, err := store.Freeze(FreezeHistoryRequest{TerminalID: "term-1", Limit: 2, Cols: 80})
	if err != nil {
		t.Fatalf("freeze: %v", err)
	}
	applyStoreBatch(t, store, replacePrimaryMutation(10, 2, 80, "v2"))

	latest, err := store.LatestWindow(HistoryWindowRequest{TerminalID: "term-1", Limit: 2, Cols: 80})
	if err != nil {
		t.Fatalf("latest after repaint: %v", err)
	}
	frozen, err := store.LatestWindow(HistoryWindowRequest{TerminalID: "term-1", Token: snapshot.Token, Limit: 2, Cols: 80})
	if err != nil {
		t.Fatalf("frozen latest: %v", err)
	}
	if got := rowTexts(latest.Rows); strings.Join(got, "|") != "sealed|v2" {
		t.Fatalf("live latest should see repaint, got %v", got)
	}
	if got := rowTexts(frozen.Rows); strings.Join(got, "|") != "sealed|v1" {
		t.Fatalf("frozen latest must keep old mutable frame payload, got %v", got)
	}
	if len(snapshot.FrozenPrimaryFrames) != 1 || snapshot.FrozenPrimaryFrames[0].Rows[0][0].Text != "v" {
		t.Fatalf("freeze snapshot should carry primary frame payload for copy mode, got %#v", snapshot)
	}
}

func TestR332FrozenHistoryCanPageOlderThanLatestLimit(t *testing.T) {
	store := NewInMemoryHistoryStore("term-1")
	applyStoreBatch(t, store,
		sealedLineMutations(1, 1, "line-1"),
		sealedLineMutations(2, 2, "line-2"),
		sealedLineMutations(3, 3, "line-3"),
		sealedLineMutations(4, 4, "line-4"),
		replacePrimaryRowsMutation(10, 5, 80, "frame-1", "frame-2"),
	)

	snapshot, err := store.Freeze(FreezeHistoryRequest{TerminalID: "term-1", Limit: 2, Cols: 80})
	if err != nil {
		t.Fatalf("freeze: %v", err)
	}
	applyStoreBatch(t, store, replacePrimaryRowsMutation(10, 5, 80, "changed-1", "changed-2"))

	latest, err := store.LatestWindow(HistoryWindowRequest{TerminalID: "term-1", Token: snapshot.Token, Limit: 2, Cols: 80})
	if err != nil {
		t.Fatalf("frozen latest: %v", err)
	}
	if got := rowTexts(latest.Rows); strings.Join(got, "|") != "frame-1|frame-2" {
		t.Fatalf("frozen latest should return stable tail, got %v", got)
	}
	if !latest.Boundary.Cursor.Valid || latest.Boundary.Cursor.BeforeRowIndex != 4 || latest.Boundary.Cursor.RowInLine != 0 {
		t.Fatalf("frozen latest cursor must point before the latest page in full snapshot, got %#v", latest.Boundary.Cursor)
	}

	older, err := store.OlderWindow(HistoryWindowRequest{
		TerminalID: "term-1",
		Token:      snapshot.Token,
		Limit:      2,
		Cols:       80,
		Cursor:     latest.Boundary.Cursor,
	})
	if err != nil {
		t.Fatalf("frozen older: %v", err)
	}
	if got := rowTexts(older.Rows); strings.Join(got, "|") != "line-3|line-4" {
		t.Fatalf("frozen older must page from full frozen projection, got %v window=%#v", got, older)
	}
	if latest.Rows[0].ProjectionRowIndex != 4 || older.Rows[0].ProjectionRowIndex != 2 {
		t.Fatalf("windows must carry projection row indexes, latest=%#v older=%#v", latest.Rows, older.Rows)
	}
}

func TestR360FrozenHistoryOldestWindowReplacesWithProjectionHead(t *testing.T) {
	store := NewInMemoryHistoryStore("term-1")
	applyStoreBatch(t, store,
		sealedLineMutations(1, 1, "line-1"),
		sealedLineMutations(2, 2, "line-2"),
		sealedLineMutations(3, 3, "line-3"),
		sealedLineMutations(4, 4, "line-4"),
		replacePrimaryRowsMutation(10, 5, 80, "frame-1", "frame-2"),
	)

	snapshot, err := store.Freeze(FreezeHistoryRequest{TerminalID: "term-1", Limit: 2, Cols: 80})
	if err != nil {
		t.Fatalf("freeze: %v", err)
	}
	oldest, err := store.OldestWindow(HistoryWindowRequest{TerminalID: "term-1", Token: snapshot.Token, Limit: 2, Cols: 80})
	if err != nil {
		t.Fatalf("oldest: %v", err)
	}
	if oldest.Op != HistoryWindowReplace {
		t.Fatalf("oldest must replace local visible window, got %s", oldest.Op)
	}
	if got := rowTexts(oldest.Rows); strings.Join(got, "|") != "line-1|line-2" {
		t.Fatalf("oldest must return frozen projection head, got %v window=%#v", got, oldest)
	}
	if oldest.Rows[0].ProjectionRowIndex != 0 || oldest.Rows[1].ProjectionRowIndex != 1 {
		t.Fatalf("oldest rows must keep absolute projection indexes from head, got %#v", oldest.Rows)
	}
	if oldest.Boundary.FirstLineID != 1 || oldest.Boundary.LastLineID != 2 {
		t.Fatalf("oldest replace boundary must describe visible head page, got %#v", oldest.Boundary)
	}
	if oldest.HasMore {
		t.Fatalf("oldest replace is already at projection head and must not advertise older pages: %#v", oldest.Boundary.Cursor)
	}
}

func TestR360BackendHistoryOldestWindowReadsProjectionHead(t *testing.T) {
	store := NewBackendHistoryStore("term-1", NewMemoryStorageBackend())
	applyStoreBatch(t, store,
		sealedLineMutations(1, 1, "line-1"),
		sealedLineMutations(2, 2, "line-2"),
		sealedLineMutations(3, 3, "line-3"),
		sealedLineMutations(4, 4, "line-4"),
		replacePrimaryRowsMutation(10, 5, 80, "frame-1", "frame-2"),
	)

	snapshot, err := store.Freeze(FreezeHistoryRequest{TerminalID: "term-1", Limit: 2, Cols: 80})
	if err != nil {
		t.Fatalf("freeze: %v", err)
	}
	oldest, err := store.OldestWindow(HistoryWindowRequest{TerminalID: "term-1", Token: snapshot.Token, Limit: 2, Cols: 80})
	if err != nil {
		t.Fatalf("oldest: %v", err)
	}
	if got := rowTexts(oldest.Rows); strings.Join(got, "|") != "line-1|line-2" {
		t.Fatalf("backend oldest must read projection head from payload backend, got %v window=%#v", got, oldest)
	}
	if oldest.Op != HistoryWindowReplace || oldest.HasMore {
		t.Fatalf("backend oldest must be a head replace without older pages, got op=%s hasMore=%v cursor=%#v", oldest.Op, oldest.HasMore, oldest.Boundary.Cursor)
	}
	if oldest.Boundary.FirstLineID != 1 || oldest.Boundary.LastLineID != 2 {
		t.Fatalf("backend oldest boundary must describe visible head page, got %#v", oldest.Boundary)
	}
}

func TestR333InMemoryStoreOlderPagesFrozenProjectionWithoutDuplicates(t *testing.T) {
	store := NewInMemoryHistoryStore("term-1")
	var batches [][]HistoryMutation
	for i := 1; i <= 300; i++ {
		batches = append(batches, sealedLineMutations(LogicalLineID(i), HistoryRecordID(i), "line"))
	}
	applyStoreBatch(t, store, batches...)

	snapshot, err := store.Freeze(FreezeHistoryRequest{TerminalID: "term-1", Limit: 40, Cols: 80})
	if err != nil {
		t.Fatalf("freeze: %v", err)
	}
	window, err := store.LatestWindow(HistoryWindowRequest{TerminalID: "term-1", Token: snapshot.Token, Limit: 40, Cols: 80})
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	var indexes []int
	for {
		for _, row := range window.Rows {
			indexes = append(indexes, row.ProjectionRowIndex)
		}
		if !window.Boundary.Cursor.Valid {
			break
		}
		window, err = store.OlderWindow(HistoryWindowRequest{TerminalID: "term-1", Token: snapshot.Token, Limit: 40, Cols: 80, Cursor: window.Boundary.Cursor})
		if err != nil {
			t.Fatalf("older: %v", err)
		}
		if len(window.Rows) == 0 {
			break
		}
	}
	if len(indexes) != 300 {
		t.Fatalf("expected all projection rows exactly once, got %d", len(indexes))
	}
	seen := make(map[int]bool, len(indexes))
	for _, index := range indexes {
		if seen[index] {
			t.Fatalf("projection row %d repeated in older paging: %v", index, indexes)
		}
		seen[index] = true
	}
	for index := 0; index < 300; index++ {
		if !seen[index] {
			t.Fatalf("projection row %d missing from older paging", index)
		}
	}
}

func TestR343InMemoryStoreApplyHotPathDoesNotReindexWholeStore(t *testing.T) {
	source, err := os.ReadFile("in_memory_store.go")
	if err != nil {
		t.Fatalf("read store source: %v", err)
	}
	// 中文说明：Apply 是 PTY 输出链路上的同步热路径，不能为了维护辅助计数器
	// 每次扫描已存在的 lines/timeline/frame records；恢复路径才允许全量 reindex。
	hotFunctions := []string{
		"func (store *inMemoryHistoryStore) Apply",
		"func (store *inMemoryHistoryStore) applyMutation",
		"func (store *inMemoryHistoryStore) upsertLogicalLines",
		"func (store *inMemoryHistoryStore) upsertDraftLines",
		"func (store *inMemoryHistoryStore) upsertFrameRecord",
		"func (store *inMemoryHistoryStore) flushStorage",
	}
	for _, signature := range hotFunctions {
		body := sourceFunctionBody(t, string(source), signature)
		if strings.Contains(body, "reindexCounters(") {
			t.Fatalf("%s must not call reindexCounters on the history write hot path", signature)
		}
	}
}

func TestR343InMemoryStoreIncrementalCountersSurviveSequentialApplies(t *testing.T) {
	store := NewInMemoryHistoryStore("term-r343").(*inMemoryHistoryStore)
	for i := 1; i <= 1024; i++ {
		id := LogicalLineID(i)
		applyStoreBatch(t, store, sealedLineMutations(id, HistoryRecordID(i), "line"))
	}
	if store.nextLineID != 1025 {
		t.Fatalf("line counter should advance incrementally without full reindex, got %d", store.nextLineID)
	}
	if store.nextRecordID != 1025 {
		t.Fatalf("record counter should advance incrementally without full reindex, got %d", store.nextRecordID)
	}

	applyStoreBatch(t, store, replacePrimaryRowsMutation(55, 2000, 80, "frame"))
	if store.nextFrameID != 56 || store.nextSessionID != 2 {
		t.Fatalf("primary frame counters should observe mutable frame ids, frame=%d session=%d", store.nextFrameID, store.nextSessionID)
	}

	window, err := store.LatestWindow(HistoryWindowRequest{TerminalID: "term-r343", Limit: 2, Cols: 80})
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	if got := rowTexts(window.Rows); strings.Join(got, "|") != "line|frame" {
		t.Fatalf("sequential hot-path applies lost authoritative projection, got %v", got)
	}
}

func TestR323InMemoryStoreCopyUsesFrozenAuthoritativeRows(t *testing.T) {
	store := NewInMemoryHistoryStore("term-1")
	applyStoreBatch(t, store, sealedLineMutations(1, 1, "alpha"), replacePrimaryMutation(10, 2, 80, "beta"))
	snapshot, err := store.Freeze(FreezeHistoryRequest{TerminalID: "term-1", Limit: 2, Cols: 80})
	if err != nil {
		t.Fatalf("freeze: %v", err)
	}
	applyStoreBatch(t, store, replacePrimaryMutation(10, 2, 80, "changed"))

	text, err := store.Copy(HistoryCopyRequest{
		TerminalID: "term-1",
		Token:      snapshot.Token,
		Start:      HistoryCursor{LineID: 1, Valid: true},
		End:        HistoryCursor{LineID: 2, Valid: true},
	})
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	if text != "alpha\nbeta" {
		t.Fatalf("copy must use frozen authoritative rows, got %q", text)
	}
}

func TestR323InMemoryStoreOlderDoesNotDuplicateFullyIncludedTimeline(t *testing.T) {
	store := NewInMemoryHistoryStore("term-1")
	applyStoreBatch(t, store,
		sealedLineMutations(1, 1, "sealed"),
		replacePrimaryMutation(10, 2, 80, "current"),
	)
	latest, err := store.LatestWindow(HistoryWindowRequest{TerminalID: "term-1", Limit: 10, Cols: 80})
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	if latest.HasMore {
		t.Fatalf("latest should report no older rows when sealed timeline is fully included: %#v", latest.Boundary.Cursor)
	}
	older, err := store.OlderWindow(HistoryWindowRequest{TerminalID: "term-1", Limit: 10, Cols: 80, Cursor: latest.Boundary.Cursor})
	if err != nil {
		t.Fatalf("older window: %v", err)
	}
	if len(older.Rows) != 0 {
		t.Fatalf("older must not duplicate fully included sealed timeline, got %#v", older.Rows)
	}
}

func TestR327InMemoryStoreOlderPagesWithinCurrentFrameBeforeTimeline(t *testing.T) {
	store := NewInMemoryHistoryStore("term-1")
	applyStoreBatch(t, store,
		sealedLineMutations(1, 1, "sealed"),
		replacePrimaryRowsMutation(10, 2, 80, "frame-1", "frame-2", "frame-3"),
	)
	latest, err := store.LatestWindow(HistoryWindowRequest{TerminalID: "term-1", Limit: 2, Cols: 80})
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	if got := rowTexts(latest.Rows); strings.Join(got, "|") != "frame-2|frame-3" {
		t.Fatalf("latest should be current frame tail, got %v", got)
	}
	older, err := store.OlderWindow(HistoryWindowRequest{TerminalID: "term-1", Limit: 2, Cols: 80, Cursor: latest.Boundary.Cursor})
	if err != nil {
		t.Fatalf("older window: %v", err)
	}
	if got := rowTexts(older.Rows); strings.Join(got, "|") != "sealed|frame-1" {
		t.Fatalf("older must include frame rows hidden above latest tail before falling back to timeline, got %v window=%#v", got, older)
	}
}

func TestR323InMemoryStoreOpenLineIsMutableProjection(t *testing.T) {
	store := NewInMemoryHistoryStore("term-1")
	openLine := OpenLine{Active: true, Draft: LogicalLineDraft{Line: lineForStore(7, "draft", string(LineKindOrdinary), 0), Row: 0}}
	applyStoreBatch(t, store, []HistoryMutation{{Kind: HistoryMutationUpsertOpenLine, OpenLine: &openLine}})
	window, err := store.LatestWindow(HistoryWindowRequest{TerminalID: "term-1", Limit: 5, Cols: 80})
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	if got := rowTexts(window.Rows); strings.Join(got, "|") != "draft" {
		t.Fatalf("latest should include open line projection, got %v", got)
	}
	if window.Rows[0].Committed || window.Rows[0].LineID != 7 {
		t.Fatalf("open line projection must stay mutable, got %#v", window.Rows[0])
	}
}

func TestR323InMemoryStoreRecoveryRestoresTimelineAndFrameRecords(t *testing.T) {
	backend := NewMemoryStorageBackend()
	if err := backend.Apply(StorageTransaction{
		Generation: 9,
		Lines: []LogicalLine{
			lineForStore(1, "sealed", string(LineKindOrdinary), 0),
			lineForStore(2, "archived", string(LineKindArchivedScreenFrame), 80),
		},
		Timeline: []HistoryRecord{
			{ID: 1, Seq: 1, Kind: HistoryRecordOrdinaryLine, LineIDs: []LogicalLineID{1}},
			{ID: 2, Seq: 2, Kind: HistoryRecordArchivedPrimaryFrame, FrameID: 22, LineIDs: []LogicalLineID{2}, Reason: SealReasonAltEnter},
		},
		Frames: []FrameRecord{{
			SessionID:  1,
			FrameID:    22,
			Sequence:   2,
			LineIDs:    []LogicalLineID{2},
			ScreenSize: TerminalSemanticSize{Cols: 80, Rows: 1},
			SealReason: SealReasonAltEnter,
		}},
	}); err != nil {
		t.Fatalf("apply storage tx: %v", err)
	}
	recovered, err := backend.Recover()
	if err != nil {
		t.Fatalf("recover backend: %v", err)
	}
	store := NewInMemoryHistoryStoreFromRecovered("term-1", recovered)
	window, err := store.LatestWindow(HistoryWindowRequest{TerminalID: "term-1", Limit: 2, Cols: 80})
	if err != nil {
		t.Fatalf("latest recovered: %v", err)
	}
	if got := rowTexts(window.Rows); strings.Join(got, "|") != "sealed|archived" {
		t.Fatalf("recovered store lost timeline/frame payloads, got %v window=%#v", got, window)
	}
	if window.Rows[1].Segment != HistorySegmentArchivedPrimaryFrame || window.Generation != 9 {
		t.Fatalf("recovered archive row lost segment/generation: %#v", window)
	}
}

func applyStoreBatch(t *testing.T, store HistoryStore, mutationGroups ...[]HistoryMutation) {
	t.Helper()
	var mutations []HistoryMutation
	for _, group := range mutationGroups {
		mutations = append(mutations, group...)
	}
	if err := store.Apply(HistoryMutationBatch{Mutations: mutations}); err != nil {
		t.Fatalf("apply store batch: %v", err)
	}
}

func sealedLineMutations(lineID LogicalLineID, recordID HistoryRecordID, text string) []HistoryMutation {
	line := lineForStore(lineID, text, string(LineKindOrdinary), 0)
	line.Seal = SealStateSealed
	record := HistoryRecord{ID: recordID, Seq: uint64(recordID), Kind: HistoryRecordOrdinaryLine, LineIDs: []LogicalLineID{lineID}, Reason: SealReasonLineFeed}
	return []HistoryMutation{
		{Kind: HistoryMutationSealLine, Line: &line, LineIDs: []LogicalLineID{lineID}, Reason: SealReasonLineFeed},
		{Kind: HistoryMutationAppendTimelineRecord, Record: &record, LineIDs: []LogicalLineID{lineID}, Reason: SealReasonLineFeed},
	}
}

func archivedFrameMutationsForStore(frameID ScreenFrameID, recordID HistoryRecordID, cols int, text string) []HistoryMutation {
	lineID := LogicalLineID(recordID * 100)
	line := lineForStore(lineID, text, string(LineKindArchivedScreenFrame), cols)
	line.Seal = SealStateSealed
	frame := SealedFrame{ID: frameID, SessionID: 1, Seq: uint64(recordID), Cols: cols, Lines: []LogicalLine{line}, Reason: SealReasonAltEnter}
	record := HistoryRecord{ID: recordID, Seq: uint64(recordID), Kind: HistoryRecordArchivedPrimaryFrame, FrameID: frameID, LineIDs: []LogicalLineID{lineID}, Reason: SealReasonAltEnter}
	return []HistoryMutation{
		{Kind: HistoryMutationArchivePrimaryFrame, Sealed: &frame, FrameID: frameID, SessionID: 1, LineIDs: []LogicalLineID{lineID}, Reason: SealReasonAltEnter},
		{Kind: HistoryMutationAppendTimelineRecord, Record: &record, FrameID: frameID, SessionID: 1, LineIDs: []LogicalLineID{lineID}, Reason: SealReasonAltEnter},
	}
}

func replacePrimaryMutation(frameID ScreenFrameID, firstLineID LogicalLineID, cols int, text string) []HistoryMutation {
	return replacePrimaryRowsMutation(frameID, firstLineID, cols, text)
}

func replacePrimaryRowsMutation(frameID ScreenFrameID, firstLineID LogicalLineID, cols int, texts ...string) []HistoryMutation {
	rows := make([]LogicalLineDraft, 0, len(texts))
	for index, text := range texts {
		line := lineForStore(firstLineID+LogicalLineID(index), text, string(LineKindScreenFrame), cols)
		line.Seal = SealStateOpen
		rows = append(rows, LogicalLineDraft{Line: line, Row: index})
	}
	frame := MutableFrame{
		ID:        frameID,
		SessionID: 1,
		Cols:      cols,
		Rows:      rows,
		Source:    FrameSourcePrimarySemantic,
	}
	return []HistoryMutation{{Kind: HistoryMutationReplacePrimaryFrame, Mutable: &frame, FrameID: frameID, SessionID: 1}}
}

func lineForStore(id LogicalLineID, text string, kind string, screenCols int) LogicalLine {
	cells := make([]Cell, 0, len(text))
	for _, r := range text {
		cells = append(cells, Cell{Text: string(r), Width: 1})
	}
	return LogicalLine{ID: id, Kind: kind, Cells: cells, ScreenCols: screenCols, Residency: ResidencyMemory}
}

func rowTexts(rows []HistoryRow) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		var text string
		for _, cell := range row.Cells {
			text += cell.Text
		}
		out = append(out, text)
	}
	return out
}

func sourceFunctionBody(t *testing.T, source string, signature string) string {
	t.Helper()
	start := strings.Index(source, signature)
	if start < 0 {
		t.Fatalf("source missing function %q", signature)
	}
	rest := source[start+len(signature):]
	next := strings.Index(rest, "\nfunc ")
	if next < 0 {
		return rest
	}
	return rest[:next]
}
