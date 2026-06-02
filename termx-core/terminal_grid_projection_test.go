package termx

import (
	"reflect"
	"testing"

	localvterm "github.com/lozzow/termx/termx-vterm/vterm"
)

func TestTerminalGridProjectionReflowsLogicalLinesAtRequestedWidth(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	if err := store.appendRows([]terminalGridRow{
		{cells: localVTermCellsFromString("abcd"), rowKind: "output", wrapped: true},
		{cells: localVTermCellsFromString("ef"), rowKind: "output", wrapped: false},
		{cells: localVTermCellsFromString("WXYZ"), rowKind: "prompt", wrapped: false},
	}); err != nil {
		t.Fatalf("append logical rows: %v", err)
	}

	wide, err := store.Viewport(0, 10, 6)
	if err != nil {
		t.Fatalf("wide viewport: %v", err)
	}
	if got := vtermRowsToStrings(wide.Rows); !reflect.DeepEqual(got, []string{"abcdef", "WXYZ"}) {
		t.Fatalf("expected wide projection by logical line, got %#v", got)
	}
	if got := wide.Wrapped; !reflect.DeepEqual(got, []bool{false, false}) {
		t.Fatalf("expected wide projection wrapped flags to terminate each logical line, got %#v", got)
	}
	if wide.LogicalTotal != 2 || store.LogicalLineCount() != 2 {
		t.Fatalf("expected two persisted logical lines, viewport=%d store=%d", wide.LogicalTotal, store.LogicalLineCount())
	}

	narrow, err := store.Viewport(0, 10, 3)
	if err != nil {
		t.Fatalf("narrow viewport: %v", err)
	}
	if got := vtermRowsToStrings(narrow.Rows); !reflect.DeepEqual(got, []string{"abc", "def", "WXY", "Z"}) {
		t.Fatalf("expected narrow projection to reflow the same logical lines, got %#v", got)
	}
	if got := narrow.Wrapped; !reflect.DeepEqual(got, []bool{true, false, true, false}) {
		t.Fatalf("expected narrow projection wrapped flags to preserve logical line boundaries, got %#v", got)
	}
	if got := narrow.Ownership; !reflect.DeepEqual(got, []string{RowOwnershipPersisted, RowOwnershipPersisted, RowOwnershipPersisted, RowOwnershipPersisted}) {
		t.Fatalf("expected projected rows to stay persisted ownership, got %#v", got)
	}
}

func TestTerminalGridProjectionUsesLogicalLineRecordIDs(t *testing.T) {
	rows := []terminalGridRow{
		{cells: localVTermCellsFromString("abcd"), rowKind: "line0", wrapped: true},
		{cells: localVTermCellsFromString("ef"), rowKind: "line0", wrapped: false},
		{cells: localVTermCellsFromString("gh"), rowKind: "line1", wrapped: false},
	}
	records := []terminalGridLogicalLineRecord{
		{id: 101, startRow: 0, endRow: 1, sealed: true, origin: terminalLiveTailOriginReclaimed, residency: terminalLogicalLineResidencyPersisted},
		{id: 103, startRow: 2, endRow: 2, sealed: true, origin: terminalLiveTailOriginReclaimed, residency: terminalLogicalLineResidencyPersisted},
	}
	projected, _, _, _, lineIDs := reflowTerminalGridRows(rows, 3, records)
	if got := vtermRowsToStrings(projected); !reflect.DeepEqual(got, []string{"abc", "def", "gh"}) {
		t.Fatalf("expected narrow projection rows, got %#v", got)
	}
	if !reflect.DeepEqual(lineIDs, []uint64{101, 101, 103}) {
		t.Fatalf("expected projection line ids from logical line records, got %#v", lineIDs)
	}
}

func TestTerminalGridProjectionRestoresLogicalLineBoundariesFromMetadata(t *testing.T) {
	root := t.TempDir()
	store, err := newTerminalGridStore(root, "projection-metadata-line-boundary")
	if err != nil {
		t.Fatalf("new grid store: %v", err)
	}
	if err := store.appendRows([]terminalGridRow{
		{cells: localVTermCellsFromString("abcd"), rowKind: "line0", wrapped: false},
		{cells: localVTermCellsFromString("ef"), rowKind: "line0", wrapped: false},
	}); err != nil {
		t.Fatalf("append rows: %v", err)
	}
	dir := store.dir
	_, generation, _ := store.coordinates()
	if err := store.Close(); err != nil {
		t.Fatalf("close grid store: %v", err)
	}
	if err := writeTerminalGridLineMetadata(dir, terminalGridLineMetadata{Records: []terminalGridLineRecordMeta{{
		ID:         77,
		StartRow:   0,
		EndRow:     1,
		Sealed:     true,
		Origin:     terminalLiveTailOriginReclaimed,
		Residency:  terminalLogicalLineResidencyPersisted,
		Generation: generation,
	}}}); err != nil {
		t.Fatalf("write line metadata: %v", err)
	}

	reopened, err := openTerminalGridStoreForReplay(root, "projection-metadata-line-boundary")
	if err != nil {
		t.Fatalf("reopen grid store: %v", err)
	}
	defer reopened.Close()
	viewport, err := reopened.Viewport(0, 10, 6)
	if err != nil {
		t.Fatalf("viewport: %v", err)
	}
	if got := vtermRowsToStrings(viewport.Rows); !reflect.DeepEqual(got, []string{"abcdef"}) {
		t.Fatalf("expected metadata-restored logical line projection, got %#v", got)
	}
	if got := viewport.LogicalLineIDs; !reflect.DeepEqual(got, []uint64{77}) {
		t.Fatalf("expected metadata-restored logical line id, got %#v", got)
	}
	if viewport.LogicalTotal != 1 || reopened.LogicalLineCount() != 1 {
		t.Fatalf("expected one metadata-restored logical line, viewport=%d store=%d", viewport.LogicalTotal, reopened.LogicalLineCount())
	}
}

func TestTerminalGridProjectionAppliesMetadataLineMigrations(t *testing.T) {
	root := t.TempDir()
	store, err := newTerminalGridStore(root, "projection-metadata-migration")
	if err != nil {
		t.Fatalf("new grid store: %v", err)
	}
	if err := store.appendRows([]terminalGridRow{
		{cells: localVTermCellsFromString("abcd"), rowKind: "line0", wrapped: false},
	}); err != nil {
		t.Fatalf("append rows: %v", err)
	}
	dir := store.dir
	_, generation, _ := store.coordinates()
	if err := store.Close(); err != nil {
		t.Fatalf("close grid store: %v", err)
	}
	runtimeID := terminalLiveTailLogicalLineIDBase + 9
	if err := writeTerminalGridLineMetadata(dir, terminalGridLineMetadata{
		Records: []terminalGridLineRecordMeta{{
			ID:         runtimeID,
			StartRow:   0,
			EndRow:     0,
			Sealed:     true,
			Origin:     terminalLiveTailOriginLive,
			Residency:  terminalLogicalLineResidencyPersisted,
			Generation: generation,
		}},
		Migrations: []terminalGridLineMigration{{RuntimeID: runtimeID, PersistedID: 1}},
	}); err != nil {
		t.Fatalf("write line metadata: %v", err)
	}

	reopened, err := openTerminalGridStoreForReplay(root, "projection-metadata-migration")
	if err != nil {
		t.Fatalf("reopen grid store: %v", err)
	}
	defer reopened.Close()
	viewport, err := reopened.Viewport(0, 10, 4)
	if err != nil {
		t.Fatalf("viewport: %v", err)
	}
	if got := viewport.LogicalLineIDs; !reflect.DeepEqual(got, []uint64{1}) {
		t.Fatalf("expected migrated persisted logical line id, got %#v", got)
	}
}

func TestTerminalGridProjectionRejectsCorruptPersistedLineMetadata(t *testing.T) {
	for _, tc := range []struct {
		name   string
		record terminalGridLineRecordMeta
	}{
		{
			name: "dirty-persisted-record",
			record: terminalGridLineRecordMeta{
				Dirty: true,
			},
		},
		{
			name: "unknown-origin",
			record: terminalGridLineRecordMeta{
				Origin: terminalLiveTailOrigin("bad-origin"),
			},
		},
		{
			name: "unmigrated-live-origin",
			record: terminalGridLineRecordMeta{
				Origin: terminalLiveTailOriginLive,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			store, err := newTerminalGridStore(root, "projection-corrupt-persisted-metadata-"+tc.name)
			if err != nil {
				t.Fatalf("new grid store: %v", err)
			}
			if err := store.appendRows([]terminalGridRow{
				{cells: localVTermCellsFromString("abcd"), rowKind: "line0", wrapped: false},
			}); err != nil {
				t.Fatalf("append rows: %v", err)
			}
			dir := store.dir
			_, generation, _ := store.coordinates()
			if err := store.Close(); err != nil {
				t.Fatalf("close grid store: %v", err)
			}
			record := tc.record
			record.ID = 77
			record.StartRow = 0
			record.EndRow = 0
			record.Sealed = true
			if record.Origin == "" {
				record.Origin = terminalLiveTailOriginReclaimed
			}
			record.Residency = terminalLogicalLineResidencyPersisted
			record.Generation = generation
			if err := writeTerminalGridLineMetadata(dir, terminalGridLineMetadata{Records: []terminalGridLineRecordMeta{record}}); err != nil {
				t.Fatalf("write line metadata: %v", err)
			}

			reopened, err := openTerminalGridStoreForReplay(root, "projection-corrupt-persisted-metadata-"+tc.name)
			if err != nil {
				t.Fatalf("reopen grid store: %v", err)
			}
			defer reopened.Close()
			viewport, err := reopened.Viewport(0, 10, 4)
			if err != nil {
				t.Fatalf("viewport: %v", err)
			}
			if got := viewport.LogicalLineIDs; !reflect.DeepEqual(got, []uint64{1}) {
				t.Fatalf("expected corrupt persisted metadata to fall back to index/wrapped ids, got %#v", got)
			}
		})
	}
}

func TestTerminalGridRecoveredLiveTailRejectsCorruptRecordState(t *testing.T) {
	for _, tc := range []struct {
		name       string
		id         uint64
		origin     terminalLiveTailOrigin
		dirty      bool
		rowIDKnown bool
		firstRowID uint64
		lastRowID  uint64
		sealed     bool
	}{
		{name: "unknown-origin", origin: terminalLiveTailOrigin("bad-origin"), dirty: true},
		{name: "clean-live", origin: terminalLiveTailOriginLive, dirty: false},
		{name: "live-persisted-id", id: 1, origin: terminalLiveTailOriginLive, dirty: true},
		{name: "live-row-ids", origin: terminalLiveTailOriginLive, dirty: true, rowIDKnown: true, firstRowID: 40, lastRowID: 40},
		{name: "dirty-reclaimed", id: 1, origin: terminalLiveTailOriginReclaimed, dirty: true, sealed: true},
		{name: "reclaimed-runtime-id", origin: terminalLiveTailOriginReclaimed, dirty: false, rowIDKnown: true, firstRowID: 40, lastRowID: 40, sealed: true},
		{name: "open-reclaimed", id: 1, origin: terminalLiveTailOriginReclaimed, dirty: false, rowIDKnown: true, firstRowID: 40, lastRowID: 40},
		{name: "reclaimed-missing-row-ids", id: 1, origin: terminalLiveTailOriginReclaimed, dirty: false, sealed: true},
		{name: "reclaimed-row-id-span-mismatch", id: 1, origin: terminalLiveTailOriginReclaimed, dirty: false, rowIDKnown: true, firstRowID: 40, lastRowID: 42, sealed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newMemoryTerminalGridStoreForTest(t)
			defer store.Close()
			rows, err := terminalGridLineRowMetasFromDamageRows([]localvterm.DamageOp{{
				Cells:      localVTermCellsFromString("tail"),
				WrappedSet: true,
				Wrapped:    true,
			}})
			if err != nil {
				t.Fatalf("encode live row metadata: %v", err)
			}
			id := tc.id
			if id == 0 {
				id = terminalLiveTailLogicalLineIDBase + 1
			}
			if err := writeTerminalGridLineMetadata(store.dir, terminalGridLineMetadata{
				LiveRecords: []terminalGridLineRecordMeta{{
					ID:         id,
					StartRow:   0,
					EndRow:     0,
					RowIDKnown: tc.rowIDKnown,
					FirstRowID: tc.firstRowID,
					LastRowID:  tc.lastRowID,
					Sealed:     tc.sealed,
					Origin:     tc.origin,
					Residency:  terminalLogicalLineResidencyLiveTail,
					Dirty:      tc.dirty,
				}},
				LiveRows: rows,
			}); err != nil {
				t.Fatalf("write live tail metadata: %v", err)
			}
			if _, ok := store.recoveredLiveTailFromMetadata(); ok {
				t.Fatalf("expected corrupt live tail metadata %q to be ignored", tc.name)
			}
		})
	}
}

func TestTerminalGridRecoveredLiveTailUsesExplicitReclaimedRowIDs(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	rows, err := terminalGridLineRowMetasFromDamageRows([]localvterm.DamageOp{
		{Cells: localVTermCellsFromString("aa"), WrappedSet: true, Wrapped: true},
		{Cells: localVTermCellsFromString("bb"), WrappedSet: true, Wrapped: false},
	})
	if err != nil {
		t.Fatalf("encode live row metadata: %v", err)
	}
	if err := writeTerminalGridLineMetadata(store.dir, terminalGridLineMetadata{
		LiveRecords: []terminalGridLineRecordMeta{{
			ID:         99,
			StartRow:   0,
			EndRow:     1,
			RowIDKnown: true,
			FirstRowID: 40,
			LastRowID:  41,
			Sealed:     true,
			Origin:     terminalLiveTailOriginReclaimed,
			Residency:  terminalLogicalLineResidencyLiveTail,
			Dirty:      false,
			Generation: 7,
		}},
		LiveRows: rows,
	}); err != nil {
		t.Fatalf("write live tail metadata: %v", err)
	}

	tail, ok := store.recoveredLiveTailFromMetadata()
	if !ok {
		t.Fatal("expected reclaimed live tail metadata to recover")
	}
	if len(tail.segments) != 1 {
		t.Fatalf("expected one recovered segment, got %#v", tail.segments)
	}
	segment := tail.segments[0]
	if segment.origin != terminalLiveTailOriginReclaimed || segment.firstRowID != 40 || segment.lastRowID != 41 {
		t.Fatalf("expected recovered row coordinates from metadata, got %#v", segment)
	}
	if got := segment.logicalLineIDs; !reflect.DeepEqual(got, []uint64{99, 99}) {
		t.Fatalf("expected recovered logical line ids from metadata, got %#v", got)
	}
}

func TestServerHistoryWindowLogicalLineIDsStayStableAcrossProjectionWidths(t *testing.T) {
	root := t.TempDir()
	store, err := newTerminalGridStore(root, "projection-stable-line-id")
	if err != nil {
		t.Fatalf("new grid store: %v", err)
	}
	if err := store.appendRows([]terminalGridRow{
		{cells: localVTermCellsFromString("abcd"), rowKind: "line0", wrapped: true},
		{cells: localVTermCellsFromString("ef"), rowKind: "line0", wrapped: false},
		{cells: localVTermCellsFromString("gh"), rowKind: "line1", wrapped: false},
	}); err != nil {
		t.Fatalf("append logical rows: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close grid store: %v", err)
	}

	srv := NewServer(WithGridRoot(root), WithDefaultSize(6, 2))
	wide, err := srv.HistoryWindow(t.Context(), "projection-stable-line-id", HistoryWindowOptions{Limit: 10, Cols: 6})
	if err != nil {
		t.Fatalf("wide history window: %v", err)
	}
	narrow, err := srv.HistoryWindow(t.Context(), "projection-stable-line-id", HistoryWindowOptions{Limit: 10, Cols: 3})
	if err != nil {
		t.Fatalf("narrow history window: %v", err)
	}
	if len(wide.Lines) != 2 || len(narrow.Lines) != 2 {
		t.Fatalf("expected two logical lines in both projections, wide=%#v narrow=%#v", wide.Lines, narrow.Lines)
	}
	if wide.Lines[0].LogicalLineID == 0 || narrow.Lines[0].LogicalLineID == 0 {
		t.Fatalf("expected non-zero persisted logical line ids, wide=%#v narrow=%#v", wide.Lines, narrow.Lines)
	}
	if wide.Lines[0].LogicalLineID != narrow.Lines[0].LogicalLineID {
		t.Fatalf("expected first logical line id stable across widths, wide=%d narrow=%d", wide.Lines[0].LogicalLineID, narrow.Lines[0].LogicalLineID)
	}
	if wide.Lines[1].LogicalLineID != narrow.Lines[1].LogicalLineID {
		t.Fatalf("expected second logical line id stable across widths, wide=%d narrow=%d", wide.Lines[1].LogicalLineID, narrow.Lines[1].LogicalLineID)
	}
	if wide.Lines[0].LogicalLineID == wide.Lines[1].LogicalLineID {
		t.Fatalf("expected distinct logical lines to have distinct ids, got %#v", wide.Lines)
	}
}

func TestServerHistoryWindowMarksClippedLogicalLineAfterProjectionLimit(t *testing.T) {
	root := t.TempDir()
	store, err := newTerminalGridStore(root, "projection-clipped-window")
	if err != nil {
		t.Fatalf("new grid store: %v", err)
	}
	if err := store.appendRows([]terminalGridRow{
		{cells: localVTermCellsFromString("aa"), rowKind: "line0", wrapped: false},
		{cells: localVTermCellsFromString("bbbb"), rowKind: "line1", wrapped: true},
		{cells: localVTermCellsFromString("cccc"), rowKind: "line1", wrapped: true},
		{cells: localVTermCellsFromString("dd"), rowKind: "line1", wrapped: false},
		{cells: localVTermCellsFromString("ee"), rowKind: "line2", wrapped: false},
	}); err != nil {
		t.Fatalf("append logical rows: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close grid store: %v", err)
	}

	srv := NewServer(WithGridRoot(root), WithDefaultSize(4, 2))
	window, err := srv.HistoryWindow(t.Context(), "projection-clipped-window", HistoryWindowOptions{BeforeOffset: 1, Limit: 2, Cols: 4})
	if err != nil {
		t.Fatalf("history window: %v", err)
	}
	if window.Op != HistoryWindowPrepend {
		t.Fatalf("expected older window to be prepend, got %q", window.Op)
	}
	if got := historyRowsToStrings(window.Rows); !reflect.DeepEqual(got, []string{"cccc", "dd"}) {
		t.Fatalf("expected projected window to contain the visible tail of the logical line, got %#v", got)
	}
	if got := historyRowsWrapped(window.Rows); !reflect.DeepEqual(got, []bool{true, false}) {
		t.Fatalf("expected projected wrapped flags for clipped logical line tail, got %#v", got)
	}
	if len(window.Lines) != 1 {
		t.Fatalf("expected one logical line span, got %#v", window.Lines)
	}
	span := window.Lines[0]
	if span.StartRow != 0 || span.EndRow != 1 || span.RowKind != "line1" || !span.ClippedBefore || span.ClippedAfter {
		t.Fatalf("expected clipped-before logical line span for visible tail, got %#v", span)
	}
	if window.FirstRowID != 1 || window.LastRowID != 3 {
		t.Fatalf("expected canonical row ids to cover the expanded logical line 1..3, got %d..%d", window.FirstRowID, window.LastRowID)
	}
	if !window.HasMore {
		t.Fatal("expected has more because an older logical line remains before the expanded window")
	}
}

func historyRowsToStrings(rows []HistoryRow) []string {
	if len(rows) == 0 {
		return nil
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, rowTextFromHistoryRow(row))
	}
	return out
}

func historyRowsWrapped(rows []HistoryRow) []bool {
	if len(rows) == 0 {
		return nil
	}
	out := make([]bool, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.Wrapped)
	}
	return out
}
