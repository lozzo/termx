package termx

import (
	"os"
	"path/filepath"
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

func TestTerminalGridProjectionRowIDRangeSpansReflowedSourceRows(t *testing.T) {
	rows := []terminalGridRow{
		{cells: localVTermCellsFromString("ab"), rowKind: "line0", wrapped: true},
		{cells: localVTermCellsFromString("cd"), rowKind: "line0", wrapped: false},
	}
	records := []terminalGridLogicalLineRecord{
		{id: 101, startRow: 0, endRow: 1, sealed: true, origin: terminalLiveTailOriginReclaimed, residency: terminalLogicalLineResidencyPersisted, source: terminalLogicalLineRecordSourceExplicit},
	}
	projected, _, _, _, lineIDs, authoritative, rowIDRanges := reflowTerminalGridRowsWithRowIDRanges(rows, 4, records, []uint64{40, 41})
	if got := vtermRowsToStrings(projected); !reflect.DeepEqual(got, []string{"abcd"}) {
		t.Fatalf("expected reflowed row spanning both source rows, got %#v", got)
	}
	if got := lineIDs; !reflect.DeepEqual(got, []uint64{101}) {
		t.Fatalf("expected reflowed row to keep logical line id, got %#v", got)
	}
	if got := authoritative; !reflect.DeepEqual(got, []bool{true}) {
		t.Fatalf("expected reflowed row id to stay authoritative, got %#v", got)
	}
	if len(rowIDRanges) != 1 || !rowIDRanges[0].Known || rowIDRanges[0].First != 40 || rowIDRanges[0].Last != 41 {
		t.Fatalf("expected reflowed row source boundary to span 40..41, got %#v", rowIDRanges)
	}
}

func TestTerminalGridProjectionSuppressesMixedAuthorityReflowedSourceRows(t *testing.T) {
	rows := []terminalGridRow{
		{cells: localVTermCellsFromString("ab"), rowKind: "line0", wrapped: true},
		{cells: localVTermCellsFromString("cd"), rowKind: "line0", wrapped: false},
	}
	records := []terminalGridLogicalLineRecord{
		{id: 101, startRow: 1, endRow: 1, sealed: true, origin: terminalLiveTailOriginReclaimed, residency: terminalLogicalLineResidencyPersisted, source: terminalLogicalLineRecordSourceExplicit},
	}
	projected, _, _, _, lineIDs, authoritative, rowIDRanges := reflowTerminalGridRowsWithRowIDRanges(rows, 4, records, []uint64{40, 41})
	if got := vtermRowsToStrings(projected); !reflect.DeepEqual(got, []string{"abcd"}) {
		t.Fatalf("expected reflowed row spanning mixed authority source rows, got %#v", got)
	}
	if got := lineIDs; !reflect.DeepEqual(got, []uint64{0}) {
		t.Fatalf("expected mixed-authority reflowed row not to expose partial logical line id, got %#v", got)
	}
	if got := authoritative; !reflect.DeepEqual(got, []bool{false}) {
		t.Fatalf("expected mixed-authority reflowed row to be non-authoritative, got %#v", got)
	}
	if len(rowIDRanges) != 1 || !rowIDRanges[0].Known || rowIDRanges[0].First != 40 || rowIDRanges[0].Last != 41 {
		t.Fatalf("expected mixed-authority row boundary to still span physical source rows, got %#v", rowIDRanges)
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
		Source:     terminalLogicalLineRecordSourceExplicit,
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

func TestTerminalGridProjectionUsesSealedMetadataPrefixForCoveredWindow(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	if err := store.appendRows([]terminalGridRow{
		{cells: localVTermCellsFromString("aa")},
		{cells: localVTermCellsFromString("bb")},
		{cells: localVTermCellsFromString("tail"), wrapped: true},
	}); err != nil {
		t.Fatalf("append rows: %v", err)
	}
	_, generation, _ := store.coordinates()
	if err := writeTerminalGridLineMetadata(store.dir, terminalGridLineMetadata{Records: []terminalGridLineRecordMeta{
		{ID: 91, StartRow: 0, EndRow: 0, Sealed: true, Origin: terminalLiveTailOriginReclaimed, Residency: terminalLogicalLineResidencyPersisted, Generation: generation, Source: terminalLogicalLineRecordSourceExplicit},
		{ID: 92, StartRow: 1, EndRow: 1, Sealed: true, Origin: terminalLiveTailOriginReclaimed, Residency: terminalLogicalLineResidencyPersisted, Generation: generation, Source: terminalLogicalLineRecordSourceExplicit},
	}}); err != nil {
		t.Fatalf("write partial line metadata: %v", err)
	}

	viewport, err := store.Viewport(1, 1, 10)
	if err != nil {
		t.Fatalf("viewport: %v", err)
	}
	if got := vtermRowsToStrings(viewport.Rows); !reflect.DeepEqual(got, []string{"bb"}) {
		t.Fatalf("expected sealed prefix row window, got %#v", got)
	}
	if got := viewport.LogicalLineIDs; !reflect.DeepEqual(got, []uint64{92}) {
		t.Fatalf("expected sealed prefix metadata logical line id, got %#v", got)
	}
}

func TestTerminalGridProjectionUsesSealedMetadataPrefixForWindowStart(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	if err := store.appendRows([]terminalGridRow{
		{cells: localVTermCellsFromString("aa")},
		{cells: localVTermCellsFromString("bb")},
		{cells: localVTermCellsFromString("tail"), wrapped: true},
	}); err != nil {
		t.Fatalf("append rows: %v", err)
	}
	_, generation, _ := store.coordinates()
	if err := writeTerminalGridLineMetadata(store.dir, terminalGridLineMetadata{Records: []terminalGridLineRecordMeta{
		{ID: 91, StartRow: 0, EndRow: 1, Sealed: true, Origin: terminalLiveTailOriginReclaimed, Residency: terminalLogicalLineResidencyPersisted, Generation: generation, Source: terminalLogicalLineRecordSourceExplicit},
	}}); err != nil {
		t.Fatalf("write partial line metadata: %v", err)
	}

	viewport, err := store.Viewport(1, 1, 10)
	if err != nil {
		t.Fatalf("viewport: %v", err)
	}
	if got := vtermRowsToStrings(viewport.Rows); !reflect.DeepEqual(got, []string{"aabb"}) {
		t.Fatalf("expected sealed prefix to rewind window to logical line start, got %#v", got)
	}
	if got := viewport.LogicalLineIDs; !reflect.DeepEqual(got, []uint64{91}) {
		t.Fatalf("expected rewound window to use sealed prefix logical line id, got %#v", got)
	}
	if viewport.LoadedRows != 3 || viewport.FirstRowID != 0 || viewport.LastRowID != 1 {
		t.Fatalf("expected rewound raw row coordinates, loaded=%d first=%d last=%d", viewport.LoadedRows, viewport.FirstRowID, viewport.LastRowID)
	}
}

func TestTerminalGridProjectionCountsSealedMetadataPrefixLogicalTotal(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	if err := store.appendRows([]terminalGridRow{
		{cells: localVTermCellsFromString("aa")},
		{cells: localVTermCellsFromString("bb")},
		{cells: localVTermCellsFromString("tail"), wrapped: true},
	}); err != nil {
		t.Fatalf("append rows: %v", err)
	}
	_, generation, _ := store.coordinates()
	if err := writeTerminalGridLineMetadata(store.dir, terminalGridLineMetadata{Records: []terminalGridLineRecordMeta{
		{ID: 91, StartRow: 0, EndRow: 1, Sealed: true, Origin: terminalLiveTailOriginReclaimed, Residency: terminalLogicalLineResidencyPersisted, Generation: generation, Source: terminalLogicalLineRecordSourceExplicit},
	}}); err != nil {
		t.Fatalf("write partial line metadata: %v", err)
	}

	viewport, err := store.Viewport(0, 10, 10)
	if err != nil {
		t.Fatalf("viewport: %v", err)
	}
	if viewport.LogicalTotal != 2 || store.LogicalLineCount() != 2 {
		t.Fatalf("expected sealed prefix plus fallback tail logical total 2, viewport=%d store=%d", viewport.LogicalTotal, store.LogicalLineCount())
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

func TestTerminalGridProjectionIgnoresInvalidLineMigrations(t *testing.T) {
	root := t.TempDir()
	store, err := newTerminalGridStore(root, "projection-invalid-metadata-migration")
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
	if err := writeTerminalGridLineMetadata(dir, terminalGridLineMetadata{
		Records: []terminalGridLineRecordMeta{{
			ID:         1,
			StartRow:   0,
			EndRow:     0,
			Sealed:     true,
			Origin:     terminalLiveTailOriginReclaimed,
			Residency:  terminalLogicalLineResidencyPersisted,
			Generation: generation,
		}},
		Migrations: []terminalGridLineMigration{{RuntimeID: 1, PersistedID: 99}},
	}); err != nil {
		t.Fatalf("write line metadata: %v", err)
	}

	reopened, err := openTerminalGridStoreForReplay(root, "projection-invalid-metadata-migration")
	if err != nil {
		t.Fatalf("reopen grid store: %v", err)
	}
	defer reopened.Close()
	viewport, err := reopened.Viewport(0, 10, 4)
	if err != nil {
		t.Fatalf("viewport: %v", err)
	}
	if got := viewport.LogicalLineIDs; !reflect.DeepEqual(got, []uint64{1}) {
		t.Fatalf("expected invalid persisted-to-persisted migration to be ignored, got %#v", got)
	}
}

func TestTerminalGridStoreRecordsOnlyRuntimeToPersistedLineMigrations(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	runtimeID := terminalLiveTailLogicalLineIDBase + 1
	if err := writeTerminalGridLineMetadata(store.dir, terminalGridLineMetadata{
		Migrations: []terminalGridLineMigration{
			{RuntimeID: runtimeID + 10, PersistedID: 2},
			{RuntimeID: 1, PersistedID: 99},
			{RuntimeID: runtimeID + 20, PersistedID: runtimeID + 21},
		},
	}); err != nil {
		t.Fatalf("seed line metadata: %v", err)
	}

	if err := store.recordLineMigrations(map[uint64]uint64{
		runtimeID:      1,
		2:              3,
		runtimeID + 30: runtimeID + 31,
	}); err != nil {
		t.Fatalf("record line migrations: %v", err)
	}

	metadata, err := readTerminalGridLineMetadata(store.dir)
	if err != nil {
		t.Fatalf("read line metadata: %v", err)
	}
	want := []terminalGridLineMigration{
		{RuntimeID: runtimeID, PersistedID: 1},
		{RuntimeID: runtimeID + 10, PersistedID: 2},
	}
	if !reflect.DeepEqual(metadata.Migrations, want) {
		t.Fatalf("expected only runtime-to-persisted migrations to be written, got %#v want %#v", metadata.Migrations, want)
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
			name: "runtime-id",
			record: terminalGridLineRecordMeta{
				ID: terminalLiveTailLogicalLineIDBase + 1,
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
		{
			name: "row-id-known",
			record: terminalGridLineRecordMeta{
				RowIDKnown: true,
			},
		},
		{
			name: "row-id-fields",
			record: terminalGridLineRecordMeta{
				FirstRowID: 40,
				LastRowID:  40,
			},
		},
		{
			name: "unsealed-persisted-record",
			record: terminalGridLineRecordMeta{
				Sealed: false,
			},
		},
		{
			name: "missing-generation",
			record: terminalGridLineRecordMeta{
				Sealed: true,
			},
		},
		{
			name: "missing-source",
			record: terminalGridLineRecordMeta{
				Sealed: true,
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
			if record.ID == 0 {
				record.ID = 77
			}
			record.StartRow = 0
			record.EndRow = 0
			if tc.name != "unsealed-persisted-record" {
				record.Sealed = true
			}
			if record.Origin == "" {
				record.Origin = terminalLiveTailOriginReclaimed
			}
			record.Residency = terminalLogicalLineResidencyPersisted
			if tc.name != "missing-generation" {
				record.Generation = generation
			}
			if tc.name != "missing-source" {
				record.Source = terminalLogicalLineRecordSourceExplicit
			}
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

func TestTerminalGridProjectionRejectsDuplicatePersistedLineMetadataIDs(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	if err := store.appendRows([]terminalGridRow{
		{cells: localVTermCellsFromString("aa"), wrapped: false},
		{cells: localVTermCellsFromString("bb"), wrapped: false},
	}); err != nil {
		t.Fatalf("append rows: %v", err)
	}
	_, generation, _ := store.coordinates()
	if err := writeTerminalGridLineMetadata(store.dir, terminalGridLineMetadata{Records: []terminalGridLineRecordMeta{
		{ID: 77, StartRow: 0, EndRow: 0, Sealed: true, Origin: terminalLiveTailOriginReclaimed, Residency: terminalLogicalLineResidencyPersisted, Generation: generation, Source: terminalLogicalLineRecordSourceExplicit},
		{ID: 77, StartRow: 1, EndRow: 1, Sealed: true, Origin: terminalLiveTailOriginReclaimed, Residency: terminalLogicalLineResidencyPersisted, Generation: generation, Source: terminalLogicalLineRecordSourceExplicit},
	}}); err != nil {
		t.Fatalf("write line metadata: %v", err)
	}

	viewport, err := store.Viewport(0, 10, 10)
	if err != nil {
		t.Fatalf("viewport: %v", err)
	}
	if got := viewport.LogicalLineIDs; !reflect.DeepEqual(got, []uint64{1, 2}) {
		t.Fatalf("expected duplicate metadata ids to fall back to distinct persisted ids, got %#v", got)
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
		generation uint64
		rowWrapped bool
	}{
		{name: "unknown-origin", origin: terminalLiveTailOrigin("bad-origin"), dirty: true},
		{name: "clean-live", origin: terminalLiveTailOriginLive, dirty: false},
		{name: "live-persisted-id", id: 1, origin: terminalLiveTailOriginLive, dirty: true},
		{name: "live-row-ids", origin: terminalLiveTailOriginLive, dirty: true, rowIDKnown: true, firstRowID: 40, lastRowID: 40},
		{name: "live-row-id-fields", origin: terminalLiveTailOriginLive, dirty: true, firstRowID: 40, lastRowID: 40},
		{name: "live-generation", origin: terminalLiveTailOriginLive, dirty: true, generation: 7},
		{name: "resize-generation", origin: terminalLiveTailOriginResize, dirty: true, generation: 7},
		{name: "sealed-live-continuation", origin: terminalLiveTailOriginLive, dirty: true, sealed: true, rowWrapped: true},
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
				Wrapped:    tc.rowWrapped,
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
					Generation: tc.generation,
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

func TestTerminalGridRecoveredLiveTailRejectsDuplicateRecordIDs(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	rows, err := terminalGridLineRowMetasFromDamageRows([]localvterm.DamageOp{
		{Cells: localVTermCellsFromString("aa"), WrappedSet: true, Wrapped: false},
		{Cells: localVTermCellsFromString("bb"), WrappedSet: true, Wrapped: false},
	})
	if err != nil {
		t.Fatalf("encode live row metadata: %v", err)
	}
	runtimeID := terminalLiveTailLogicalLineIDBase + 1
	if err := writeTerminalGridLineMetadata(store.dir, terminalGridLineMetadata{
		LiveRecords: []terminalGridLineRecordMeta{
			{ID: runtimeID, StartRow: 0, EndRow: 0, Sealed: true, Origin: terminalLiveTailOriginLive, Residency: terminalLogicalLineResidencyLiveTail, Dirty: true},
			{ID: runtimeID, StartRow: 1, EndRow: 1, Sealed: true, Origin: terminalLiveTailOriginLive, Residency: terminalLogicalLineResidencyLiveTail, Dirty: true},
		},
		LiveRows: rows,
	}); err != nil {
		t.Fatalf("write live tail metadata: %v", err)
	}
	if _, ok := store.recoveredLiveTailFromMetadata(); ok {
		t.Fatal("expected duplicate live tail record ids to be rejected")
	}
}

func TestTerminalGridRecoveredLiveTailRejectsDuplicateReclaimedRecordIDs(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	appendExplicitTerminalGridRowsForTest(t, store, []terminalGridRow{
		{cells: localVTermCellsFromString("aa")},
		{cells: localVTermCellsFromString("bb")},
	})
	rows, err := terminalGridLineRowMetasFromDamageRows([]localvterm.DamageOp{
		{Cells: localVTermCellsFromString("aa"), WrappedSet: true, Wrapped: false},
		{Cells: localVTermCellsFromString("bb"), WrappedSet: true, Wrapped: false},
	})
	if err != nil {
		t.Fatalf("encode reclaimed row metadata: %v", err)
	}
	_, generation, _ := store.coordinates()
	if err := writeTerminalGridLineMetadata(store.dir, terminalGridLineMetadata{
		LiveRecords: []terminalGridLineRecordMeta{
			{ID: 1, StartRow: 0, EndRow: 0, RowIDKnown: true, FirstRowID: 0, LastRowID: 0, Sealed: true, Origin: terminalLiveTailOriginReclaimed, Residency: terminalLogicalLineResidencyLiveTail, Dirty: false, Generation: generation},
			{ID: 1, StartRow: 1, EndRow: 1, RowIDKnown: true, FirstRowID: 1, LastRowID: 1, Sealed: true, Origin: terminalLiveTailOriginReclaimed, Residency: terminalLogicalLineResidencyLiveTail, Dirty: false, Generation: generation},
		},
		LiveRows: rows,
	}); err != nil {
		t.Fatalf("write reclaimed live tail metadata: %v", err)
	}
	if _, ok := store.recoveredLiveTailFromMetadata(); ok {
		t.Fatal("expected duplicate reclaimed live tail record ids to be rejected")
	}
}

func TestTerminalGridRecoveredLiveTailRejectsMigratedRuntimeID(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	rows, err := terminalGridLineRowMetasFromDamageRows([]localvterm.DamageOp{{
		Cells:      localVTermCellsFromString("tail"),
		WrappedSet: true,
		Wrapped:    false,
	}})
	if err != nil {
		t.Fatalf("encode live row metadata: %v", err)
	}
	runtimeID := terminalLiveTailLogicalLineIDBase + 1
	if err := writeTerminalGridLineMetadata(store.dir, terminalGridLineMetadata{
		LiveRecords: []terminalGridLineRecordMeta{{
			ID:        runtimeID,
			StartRow:  0,
			EndRow:    0,
			Sealed:    true,
			Origin:    terminalLiveTailOriginLive,
			Residency: terminalLogicalLineResidencyLiveTail,
			Dirty:     true,
		}},
		LiveRows:   rows,
		Migrations: []terminalGridLineMigration{{RuntimeID: runtimeID, PersistedID: 1}},
	}); err != nil {
		t.Fatalf("write live tail metadata: %v", err)
	}
	if _, ok := store.recoveredLiveTailFromMetadata(); ok {
		t.Fatal("expected live tail record with already migrated runtime id to be rejected")
	}
}

func TestTerminalHistoryWindowIgnoresCorruptRecoveredLiveTailMetadata(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	appendExplicitTerminalGridRowsForTest(t, store, []terminalGridRow{
		{cells: localVTermCellsFromString("hist")},
	})
	rows, err := terminalGridLineRowMetasFromDamageRows([]localvterm.DamageOp{
		{Cells: localVTermCellsFromString("tail"), WrappedSet: true, Wrapped: false},
	})
	if err != nil {
		t.Fatalf("encode corrupt live row metadata: %v", err)
	}
	metadata, err := readTerminalGridLineMetadata(store.dir)
	if err != nil {
		t.Fatalf("read persisted line metadata: %v", err)
	}
	metadata.LiveRecords = []terminalGridLineRecordMeta{{
		ID:         terminalLiveTailLogicalLineIDBase + 1,
		StartRow:   0,
		EndRow:     0,
		Sealed:     true,
		Origin:     terminalLiveTailOriginLive,
		Residency:  terminalLogicalLineResidencyLiveTail,
		Dirty:      true,
		Generation: 7,
	}}
	metadata.LiveRows = rows
	if err := writeTerminalGridLineMetadata(store.dir, metadata); err != nil {
		t.Fatalf("write corrupt live tail metadata: %v", err)
	}

	viewport, err := storeViewportWithRecoveredLiveTail(store, 0, 10, 4)
	if err != nil {
		t.Fatalf("viewport with corrupt recovered live tail: %v", err)
	}
	window := historyWindowFromCoreGridViewport("corrupt-recovered-live-tail", 0, viewport)
	if got := historyWindowRowTexts(window); !reflect.DeepEqual(got, []string{"hist"}) {
		t.Fatalf("expected corrupt recovered live tail to be ignored by history window, got %#v", got)
	}
	if window.LoadedRows != 1 || window.LoadedLines != 1 || window.LogicalTotal != 1 {
		t.Fatalf("expected corrupt recovered live tail not to change persisted history counts, loaded_rows=%d loaded_lines=%d total=%d", window.LoadedRows, window.LoadedLines, window.LogicalTotal)
	}
	if historyWindowContainsText(window, "tail") {
		t.Fatalf("expected corrupt recovered live tail not to appear in history window, got %#v", window.Rows)
	}
}

func TestTerminalGridRecoveredLiveTailAcceptsSplitRecordByLogicalLineID(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	rows, err := terminalGridLineRowMetasFromDamageRows([]localvterm.DamageOp{
		{Cells: localVTermCellsFromString("aa"), WrappedSet: true, Wrapped: false},
		{Cells: localVTermCellsFromString("bb"), WrappedSet: true, Wrapped: false},
	})
	if err != nil {
		t.Fatalf("encode live row metadata: %v", err)
	}
	if err := writeTerminalGridLineMetadata(store.dir, terminalGridLineMetadata{
		LiveRecords: []terminalGridLineRecordMeta{{
			ID:        terminalLiveTailLogicalLineIDBase + 1,
			StartRow:  0,
			EndRow:    1,
			Sealed:    true,
			Origin:    terminalLiveTailOriginLive,
			Residency: terminalLogicalLineResidencyLiveTail,
			Dirty:     true,
		}},
		LiveRows: rows,
	}); err != nil {
		t.Fatalf("write live tail metadata: %v", err)
	}
	tail, ok := store.recoveredLiveTailFromMetadata()
	if !ok {
		t.Fatal("expected split live tail record to recover by stable logical line id")
	}
	if len(tail.segments) != 1 {
		t.Fatalf("expected one recovered live segment, got %#v", tail.segments)
	}
	if got := tail.segments[0].logicalLineIDs; !reflect.DeepEqual(got, []uint64{terminalLiveTailLogicalLineIDBase + 1, terminalLiveTailLogicalLineIDBase + 1}) {
		t.Fatalf("expected recovered split rows to share logical line id, got %#v", got)
	}
}

func TestTerminalGridRecoveredLiveTailRejectsOpenRecordBeforeTail(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	rows, err := terminalGridLineRowMetasFromDamageRows([]localvterm.DamageOp{
		{Cells: localVTermCellsFromString("open"), WrappedSet: true, Wrapped: true},
		{Cells: localVTermCellsFromString("sealed"), WrappedSet: true, Wrapped: false},
	})
	if err != nil {
		t.Fatalf("encode live row metadata: %v", err)
	}
	if err := writeTerminalGridLineMetadata(store.dir, terminalGridLineMetadata{
		LiveRecords: []terminalGridLineRecordMeta{
			{ID: terminalLiveTailLogicalLineIDBase + 1, StartRow: 0, EndRow: 0, Sealed: false, Origin: terminalLiveTailOriginLive, Residency: terminalLogicalLineResidencyLiveTail, Dirty: true},
			{ID: terminalLiveTailLogicalLineIDBase + 2, StartRow: 1, EndRow: 1, Sealed: true, Origin: terminalLiveTailOriginLive, Residency: terminalLogicalLineResidencyLiveTail, Dirty: true},
		},
		LiveRows: rows,
	}); err != nil {
		t.Fatalf("write live tail metadata: %v", err)
	}
	if _, ok := store.recoveredLiveTailFromMetadata(); ok {
		t.Fatal("expected open live tail record before tail to be rejected")
	}
}

func TestTerminalGridRecoveredLiveTailUsesLiveOnlyMetadataWithoutPersistedIndex(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	if err := os.Remove(filepath.Join(store.dir, terminalGridIndexName)); err != nil {
		t.Fatalf("remove empty persisted index: %v", err)
	}
	rows, err := terminalGridLineRowMetasFromDamageRows([]localvterm.DamageOp{
		{Cells: localVTermCellsFromString("tail0"), WrappedSet: true, Wrapped: true},
		{Cells: localVTermCellsFromString("tail1"), WrappedSet: true, Wrapped: false},
	})
	if err != nil {
		t.Fatalf("encode live row metadata: %v", err)
	}
	runtimeID := terminalLiveTailLogicalLineIDBase + 1
	if err := writeTerminalGridLineMetadata(store.dir, terminalGridLineMetadata{
		LiveRecords: []terminalGridLineRecordMeta{{
			ID:        runtimeID,
			StartRow:  0,
			EndRow:    1,
			Sealed:    true,
			Origin:    terminalLiveTailOriginLive,
			Residency: terminalLogicalLineResidencyLiveTail,
			Dirty:     true,
		}},
		LiveRows: rows,
	}); err != nil {
		t.Fatalf("write live tail metadata: %v", err)
	}

	tail, ok := store.recoveredLiveTailFromMetadata()
	if !ok {
		t.Fatal("expected live-only metadata to recover without persisted index")
	}
	if len(tail.segments) != 1 {
		t.Fatalf("expected one recovered live segment, got %#v", tail.segments)
	}
	segment := tail.segments[0]
	if segment.origin != terminalLiveTailOriginLive || segment.sealState != terminalLiveTailSealed {
		t.Fatalf("expected recovered live sealed segment, got %#v", segment)
	}
	recoveredRows := make([][]localvterm.Cell, 0, len(segment.rows))
	for _, row := range segment.rows {
		recoveredRows = append(recoveredRows, row.Cells)
	}
	if got := vtermRowsToStrings(recoveredRows); !reflect.DeepEqual(got, []string{"tail0", "tail1"}) {
		t.Fatalf("expected recovered live rows, got %#v", got)
	}
	if got := segment.logicalLineIDs; !reflect.DeepEqual(got, []uint64{runtimeID, runtimeID}) {
		t.Fatalf("expected recovered runtime logical line ids, got %#v", got)
	}

	tail.replaceLiveRows([]localvterm.DamageOp{{
		Cells:      localVTermCellsFromString("next"),
		WrappedSet: true,
		Wrapped:    false,
	}}, false)
	nextIDs := tail.window(0, tail.rowCount()).logicalLineIDs
	if len(nextIDs) != 1 || nextIDs[0] <= runtimeID {
		t.Fatalf("expected recovered live tail to continue runtime id allocation, got %#v recovered=%d", nextIDs, runtimeID)
	}
}

func TestTerminalGridRecoveredLiveTailLatestLimitMarksClippedMutableLineBefore(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	rows, err := terminalGridLineRowMetasFromDamageRows([]localvterm.DamageOp{
		{Cells: localVTermCellsFromString("tail0"), RowKind: "recovered-live", WrappedSet: true, Wrapped: true},
		{Cells: localVTermCellsFromString("tail1"), RowKind: "recovered-live", WrappedSet: true, Wrapped: false},
	})
	if err != nil {
		t.Fatalf("encode live row metadata: %v", err)
	}
	runtimeID := terminalLiveTailLogicalLineIDBase + 20
	if err := writeTerminalGridLineMetadata(store.dir, terminalGridLineMetadata{
		LiveRecords: []terminalGridLineRecordMeta{{
			ID:        runtimeID,
			StartRow:  0,
			EndRow:    1,
			Sealed:    true,
			Origin:    terminalLiveTailOriginLive,
			Residency: terminalLogicalLineResidencyLiveTail,
			Dirty:     true,
		}},
		LiveRows: rows,
	}); err != nil {
		t.Fatalf("write live tail metadata: %v", err)
	}

	viewport, err := storeViewportWithRecoveredLiveTailForHistory(store, 0, 1, 4)
	if err != nil {
		t.Fatalf("history viewport with recovered live tail: %v", err)
	}
	window := historyWindowFromCoreGridViewport("recover-live-tail-limit-clipped", 0, viewport)
	if got := historyWindowRowTexts(window); !reflect.DeepEqual(got, []string{"tail1"}) {
		t.Fatalf("expected latest limit to return recovered mutable line tail, got %#v", got)
	}
	if window.BeforeOffset != 0 || window.LoadedRows != 0 || window.Generation != 0 || window.FirstRowID != 0 || window.LastRowID != 0 {
		t.Fatalf("expected recovered clipped mutable line not to invent committed cursor or row boundary, window=%#v", window)
	}
	if window.LoadedLines != 0 || window.LogicalTotal != 1 || window.TotalRows != 2 || !window.HasMore {
		t.Fatalf("expected recovered clipped mutable line to keep clipped prefix signal, loaded=%d total=%d rows=%d has_more=%v", window.LoadedLines, window.LogicalTotal, window.TotalRows, window.HasMore)
	}
	if window.FirstLineID != 0 || window.LastLineID != 0 {
		t.Fatalf("expected recovered clipped-before line not to expose loaded line boundaries, first=%d last=%d", window.FirstLineID, window.LastLineID)
	}
	if len(window.Lines) != 1 {
		t.Fatalf("expected one recovered clipped mutable line span, got %#v", window.Lines)
	}
	span := window.Lines[0]
	if span.StartRow != 0 || span.EndRow != 0 || span.RowKind != "recovered-live" || span.LogicalLineID != runtimeID || !span.ClippedBefore || span.ClippedAfter {
		t.Fatalf("expected recovered latest limit to mark clipped-before mutable line, got %#v", span)
	}
}

func TestTerminalGridRecoveredLiveTailUsesLogicalLineIDWithoutWrappedContinuation(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	rows, err := terminalGridLineRowMetasFromDamageRows([]localvterm.DamageOp{
		{Cells: localVTermCellsFromString("tail0"), WrappedSet: true, Wrapped: false},
		{Cells: localVTermCellsFromString("tail1"), WrappedSet: true, Wrapped: false},
	})
	if err != nil {
		t.Fatalf("encode live row metadata: %v", err)
	}
	runtimeID := terminalLiveTailLogicalLineIDBase + 10
	if err := writeTerminalGridLineMetadata(store.dir, terminalGridLineMetadata{
		LiveRecords: []terminalGridLineRecordMeta{{
			ID:        runtimeID,
			StartRow:  0,
			EndRow:    1,
			Sealed:    true,
			Origin:    terminalLiveTailOriginLive,
			Residency: terminalLogicalLineResidencyLiveTail,
			Dirty:     true,
		}},
		LiveRows: rows,
	}); err != nil {
		t.Fatalf("write live tail metadata: %v", err)
	}

	tail, ok := store.recoveredLiveTailFromMetadata()
	if !ok {
		t.Fatal("expected live metadata to recover by logical line id without wrapped continuation")
	}
	if len(tail.segments) != 1 {
		t.Fatalf("expected one recovered live segment, got %#v", tail.segments)
	}
	segment := tail.segments[0]
	if got := damageRowsToStrings(segment.rows); !reflect.DeepEqual(got, []string{"tail0", "tail1"}) {
		t.Fatalf("expected recovered live rows, got %#v", got)
	}
	if got := segment.logicalLineIDs; !reflect.DeepEqual(got, []uint64{runtimeID, runtimeID}) {
		t.Fatalf("expected recovered rows to share metadata logical line id, got %#v", got)
	}
}

func TestTerminalGridRecoveredLiveTailMergedSegmentPreservesLogicalLineRecords(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	rows, err := terminalGridLineRowMetasFromDamageRows([]localvterm.DamageOp{
		{Cells: localVTermCellsFromString("a"), WrappedSet: true, Wrapped: false},
		{Cells: localVTermCellsFromString("b"), WrappedSet: true, Wrapped: false},
	})
	if err != nil {
		t.Fatalf("encode live row metadata: %v", err)
	}
	firstID := terminalLiveTailLogicalLineIDBase + 30
	secondID := terminalLiveTailLogicalLineIDBase + 31
	if err := writeTerminalGridLineMetadata(store.dir, terminalGridLineMetadata{
		LiveRecords: []terminalGridLineRecordMeta{
			{ID: firstID, StartRow: 0, EndRow: 0, Sealed: true, Origin: terminalLiveTailOriginLive, Residency: terminalLogicalLineResidencyLiveTail, Dirty: true},
			{ID: secondID, StartRow: 1, EndRow: 1, Sealed: true, Origin: terminalLiveTailOriginLive, Residency: terminalLogicalLineResidencyLiveTail, Dirty: true},
		},
		LiveRows: rows,
	}); err != nil {
		t.Fatalf("write live tail metadata: %v", err)
	}

	tail, ok := store.recoveredLiveTailFromMetadata()
	if !ok {
		t.Fatal("expected adjacent live metadata to recover")
	}
	if len(tail.segments) != 1 {
		t.Fatalf("expected adjacent live records to merge into one segment, got %#v", tail.segments)
	}
	if got := tail.segments[0].logicalLineIDs; !reflect.DeepEqual(got, []uint64{firstID, secondID}) {
		t.Fatalf("expected merged segment to preserve per-row logical line ids, got %#v", got)
	}
	records := tail.logicalLineRecords()
	if len(records) != 2 || records[0].id != firstID || records[0].startRow != 0 || records[0].endRow != 0 || records[1].id != secondID || records[1].startRow != 1 || records[1].endRow != 1 {
		t.Fatalf("expected recovered merged segment to emit two logical line records, got %#v", records)
	}
}

func TestTerminalGridRecoveredLiveTailMergedSegmentKeepsOnlyTailOpen(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	rows, err := terminalGridLineRowMetasFromDamageRows([]localvterm.DamageOp{
		{Cells: localVTermCellsFromString("sealed"), WrappedSet: true, Wrapped: false},
		{Cells: localVTermCellsFromString("open"), WrappedSet: true, Wrapped: false},
	})
	if err != nil {
		t.Fatalf("encode live row metadata: %v", err)
	}
	sealedID := terminalLiveTailLogicalLineIDBase + 40
	openID := terminalLiveTailLogicalLineIDBase + 41
	if err := writeTerminalGridLineMetadata(store.dir, terminalGridLineMetadata{
		LiveRecords: []terminalGridLineRecordMeta{
			{ID: sealedID, StartRow: 0, EndRow: 0, Sealed: true, Origin: terminalLiveTailOriginLive, Residency: terminalLogicalLineResidencyLiveTail, Dirty: true},
			{ID: openID, StartRow: 1, EndRow: 1, Sealed: false, Origin: terminalLiveTailOriginLive, Residency: terminalLogicalLineResidencyLiveTail, Dirty: true},
		},
		LiveRows: rows,
	}); err != nil {
		t.Fatalf("write live tail metadata: %v", err)
	}

	tail, ok := store.recoveredLiveTailFromMetadata()
	if !ok {
		t.Fatal("expected adjacent sealed/open live metadata to recover")
	}
	if len(tail.segments) != 1 || tail.segments[0].sealState != terminalLiveTailOpen {
		t.Fatalf("expected adjacent sealed/open records to merge into open tail segment, got %#v", tail.segments)
	}
	records := tail.logicalLineRecords()
	if len(records) != 2 {
		t.Fatalf("expected merged sealed/open segment to emit two records, got %#v", records)
	}
	if records[0].id != sealedID || records[0].sealState != terminalLiveTailSealed || records[1].id != openID || records[1].sealState != terminalLiveTailOpen {
		t.Fatalf("expected merged segment to preserve sealed prefix and open tail records, got %#v", records)
	}
}

func TestTerminalGridRecoveredLiveTailAdvancesRuntimeCursorPastMigrations(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	rows, err := terminalGridLineRowMetasFromDamageRows([]localvterm.DamageOp{{
		Cells:      localVTermCellsFromString("tail"),
		WrappedSet: true,
		Wrapped:    false,
	}})
	if err != nil {
		t.Fatalf("encode live row metadata: %v", err)
	}
	liveRuntimeID := terminalLiveTailLogicalLineIDBase + 1
	migratedRuntimeID := terminalLiveTailLogicalLineIDBase + 8
	if err := writeTerminalGridLineMetadata(store.dir, terminalGridLineMetadata{
		LiveRecords: []terminalGridLineRecordMeta{{
			ID:        liveRuntimeID,
			StartRow:  0,
			EndRow:    0,
			Sealed:    true,
			Origin:    terminalLiveTailOriginLive,
			Residency: terminalLogicalLineResidencyLiveTail,
			Dirty:     true,
		}},
		LiveRows:   rows,
		Migrations: []terminalGridLineMigration{{RuntimeID: migratedRuntimeID, PersistedID: 1}},
	}); err != nil {
		t.Fatalf("write live tail metadata: %v", err)
	}

	tail, ok := store.recoveredLiveTailFromMetadata()
	if !ok {
		t.Fatal("expected live tail metadata to recover")
	}
	tail.replaceLiveRows([]localvterm.DamageOp{{
		Cells:      localVTermCellsFromString("next"),
		WrappedSet: true,
		Wrapped:    false,
	}}, false)
	nextIDs := tail.window(0, tail.rowCount()).logicalLineIDs
	if len(nextIDs) != 1 || nextIDs[0] <= migratedRuntimeID {
		t.Fatalf("expected recovered live tail cursor to advance past migrated runtime id, got %#v migrated=%d", nextIDs, migratedRuntimeID)
	}
}

func TestTerminalGridRecoveredLiveTailKeepsOpenLineSeparateFromWrapPending(t *testing.T) {
	for _, tc := range []struct {
		name            string
		rowWrapped      bool
		wantWrapPending bool
	}{
		{name: "open-unwrapped", rowWrapped: false, wantWrapPending: false},
		{name: "open-wrapped", rowWrapped: true, wantWrapPending: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newMemoryTerminalGridStoreForTest(t)
			defer store.Close()
			rows, err := terminalGridLineRowMetasFromDamageRows([]localvterm.DamageOp{{
				Cells:      localVTermCellsFromString("tail"),
				WrappedSet: true,
				Wrapped:    tc.rowWrapped,
			}})
			if err != nil {
				t.Fatalf("encode live row metadata: %v", err)
			}
			if err := writeTerminalGridLineMetadata(store.dir, terminalGridLineMetadata{
				LiveRecords: []terminalGridLineRecordMeta{{
					ID:        terminalLiveTailLogicalLineIDBase + 1,
					StartRow:  0,
					EndRow:    0,
					Sealed:    false,
					Origin:    terminalLiveTailOriginLive,
					Residency: terminalLogicalLineResidencyLiveTail,
					Dirty:     true,
				}},
				LiveRows: rows,
			}); err != nil {
				t.Fatalf("write live tail metadata: %v", err)
			}
			tail, ok := store.recoveredLiveTailFromMetadata()
			if !ok {
				t.Fatal("expected open live tail metadata to recover")
			}
			if tail.wrapPending != tc.wantWrapPending {
				t.Fatalf("expected recovered tail wrapPending=%v, got %#v", tc.wantWrapPending, tail)
			}
			if len(tail.segments) != 1 || tail.segments[0].sealState != terminalLiveTailOpen || tail.segments[0].wrapPending != tc.wantWrapPending {
				t.Fatalf("unexpected recovered open segment: %#v", tail.segments)
			}
			records := tail.logicalLineRecords()
			if len(records) != 1 || records[0].sealState != terminalLiveTailOpen {
				t.Fatalf("expected recovered open line record to stay open, got %#v", records)
			}
			if err := store.recordLiveTailLineState(records, tail.rows()); err != nil {
				t.Fatalf("rewrite recovered live tail metadata: %v", err)
			}
			metadata, err := readTerminalGridLineMetadata(store.dir)
			if err != nil {
				t.Fatalf("read rewritten live tail metadata: %v", err)
			}
			if len(metadata.LiveRecords) != 1 || metadata.LiveRecords[0].Sealed {
				t.Fatalf("expected rewritten recovered live tail metadata to keep open seal state, got %#v", metadata.LiveRecords)
			}
		})
	}
}

func TestTerminalGridLiveTailLineStateRejectsUnknownSealState(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	rows := []localvterm.DamageOp{{
		Cells:      localVTermCellsFromString("tail"),
		WrappedSet: true,
		Wrapped:    false,
	}}
	records := []terminalLiveTailLogicalLineRecord{{
		id:        terminalLiveTailLogicalLineIDBase + 1,
		startRow:  0,
		endRow:    0,
		sealState: terminalLiveTailSealState("unknown"),
		origin:    terminalLiveTailOriginLive,
		residency: terminalLogicalLineResidencyLiveTail,
		dirty:     true,
	}}

	if err := store.recordLiveTailLineState(records, rows); err != nil {
		t.Fatalf("record invalid live tail metadata: %v", err)
	}
	metadata, err := readTerminalGridLineMetadata(store.dir)
	if err != nil {
		t.Fatalf("read live tail metadata: %v", err)
	}
	if len(metadata.LiveRecords) != 0 || len(metadata.LiveRows) != 0 {
		t.Fatalf("expected invalid seal state to suppress live tail metadata, got records=%#v rows=%#v", metadata.LiveRecords, metadata.LiveRows)
	}
}

func TestTerminalGridLiveTailLineStateRejectsReclaimedAfterLiveRecord(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	rows := []localvterm.DamageOp{
		{Cells: localVTermCellsFromString("live"), WrappedSet: true, Wrapped: false},
		{Cells: localVTermCellsFromString("r0"), WrappedSet: true, Wrapped: false},
	}
	records := []terminalLiveTailLogicalLineRecord{
		{id: terminalLiveTailLogicalLineIDBase + 1, startRow: 0, endRow: 0, sealState: terminalLiveTailSealed, origin: terminalLiveTailOriginLive, residency: terminalLogicalLineResidencyLiveTail, dirty: true},
		{id: 1, startRow: 1, endRow: 1, sealState: terminalLiveTailSealed, origin: terminalLiveTailOriginReclaimed, residency: terminalLogicalLineResidencyLiveTail, generation: 7, rowIDKnown: true, firstRowID: 0, lastRowID: 0},
	}

	if err := store.recordLiveTailLineState(records, rows); err != nil {
		t.Fatalf("record invalid ordered live tail metadata: %v", err)
	}
	metadata, err := readTerminalGridLineMetadata(store.dir)
	if err != nil {
		t.Fatalf("read live tail metadata: %v", err)
	}
	if len(metadata.LiveRecords) != 0 || len(metadata.LiveRows) != 0 {
		t.Fatalf("expected invalid live-before-reclaimed record view to suppress metadata, got records=%#v rows=%#v", metadata.LiveRecords, metadata.LiveRows)
	}
}

func TestTerminalGridLiveTailLineStateRejectsStaleReclaimedGeneration(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	appendExplicitTerminalGridRowsForTest(t, store, []terminalGridRow{
		{cells: localVTermCellsFromString("r0"), wrapped: false},
	})
	rows := []localvterm.DamageOp{{
		Cells:      localVTermCellsFromString("r0"),
		WrappedSet: true,
		Wrapped:    false,
	}}
	_, generation, _ := store.coordinates()
	records := []terminalLiveTailLogicalLineRecord{{
		id:         1,
		startRow:   0,
		endRow:     0,
		sealState:  terminalLiveTailSealed,
		origin:     terminalLiveTailOriginReclaimed,
		residency:  terminalLogicalLineResidencyLiveTail,
		generation: generation + 1,
		rowIDKnown: true,
		firstRowID: 0,
		lastRowID:  0,
	}}

	if err := store.recordLiveTailLineState(records, rows); err != nil {
		t.Fatalf("record stale generation live tail metadata: %v", err)
	}
	metadata, err := readTerminalGridLineMetadata(store.dir)
	if err != nil {
		t.Fatalf("read live tail metadata: %v", err)
	}
	if len(metadata.LiveRecords) != 0 || len(metadata.LiveRows) != 0 {
		t.Fatalf("expected stale reclaimed generation to suppress metadata, got records=%#v rows=%#v current_generation=%d", metadata.LiveRecords, metadata.LiveRows, generation)
	}
}

func TestTerminalGridLiveTailLineStateRejectsStaleReclaimedRowIDs(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	appendExplicitTerminalGridRowsForTest(t, store, []terminalGridRow{
		{cells: localVTermCellsFromString("r0"), wrapped: false},
	})
	rows := []localvterm.DamageOp{{
		Cells:      localVTermCellsFromString("r0"),
		WrappedSet: true,
		Wrapped:    false,
	}}
	_, generation, _ := store.coordinates()
	records := []terminalLiveTailLogicalLineRecord{{
		id:         1,
		startRow:   0,
		endRow:     0,
		sealState:  terminalLiveTailSealed,
		origin:     terminalLiveTailOriginReclaimed,
		residency:  terminalLogicalLineResidencyLiveTail,
		generation: generation,
		rowIDKnown: true,
		firstRowID: 40,
		lastRowID:  40,
	}}

	if err := store.recordLiveTailLineState(records, rows); err != nil {
		t.Fatalf("record stale row id live tail metadata: %v", err)
	}
	metadata, err := readTerminalGridLineMetadata(store.dir)
	if err != nil {
		t.Fatalf("read live tail metadata: %v", err)
	}
	if len(metadata.LiveRecords) != 0 || len(metadata.LiveRows) != 0 {
		t.Fatalf("expected stale reclaimed row ids to suppress metadata, got records=%#v rows=%#v", metadata.LiveRecords, metadata.LiveRows)
	}
}

func TestTerminalGridLiveTailLineStateRejectsMixedMutableOrigins(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	rows := []localvterm.DamageOp{
		{Cells: localVTermCellsFromString("live"), WrappedSet: true, Wrapped: false},
		{Cells: localVTermCellsFromString("resize"), WrappedSet: true, Wrapped: false},
	}
	records := []terminalLiveTailLogicalLineRecord{
		{id: terminalLiveTailLogicalLineIDBase + 1, startRow: 0, endRow: 0, sealState: terminalLiveTailSealed, origin: terminalLiveTailOriginLive, residency: terminalLogicalLineResidencyLiveTail, dirty: true},
		{id: terminalLiveTailLogicalLineIDBase + 2, startRow: 1, endRow: 1, sealState: terminalLiveTailSealed, origin: terminalLiveTailOriginResize, residency: terminalLogicalLineResidencyLiveTail, dirty: true},
	}

	if err := store.recordLiveTailLineState(records, rows); err != nil {
		t.Fatalf("record mixed mutable origin metadata: %v", err)
	}
	metadata, err := readTerminalGridLineMetadata(store.dir)
	if err != nil {
		t.Fatalf("read live tail metadata: %v", err)
	}
	if len(metadata.LiveRecords) != 0 || len(metadata.LiveRows) != 0 {
		t.Fatalf("expected mixed live/resize record view to suppress metadata, got records=%#v rows=%#v", metadata.LiveRecords, metadata.LiveRows)
	}
}

func TestTerminalGridRecoveredLiveTailPreservesResizeOrigin(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	rows, err := terminalGridLineRowMetasFromDamageRows([]localvterm.DamageOp{
		{Cells: localVTermCellsFromString("r0"), WrappedSet: true, Wrapped: true},
		{Cells: localVTermCellsFromString("r1"), WrappedSet: true, Wrapped: true},
	})
	if err != nil {
		t.Fatalf("encode resize live row metadata: %v", err)
	}
	runtimeID := terminalLiveTailLogicalLineIDBase + 1
	if err := writeTerminalGridLineMetadata(store.dir, terminalGridLineMetadata{
		LiveRecords: []terminalGridLineRecordMeta{{
			ID:        runtimeID,
			StartRow:  0,
			EndRow:    1,
			Sealed:    false,
			Origin:    terminalLiveTailOriginResize,
			Residency: terminalLogicalLineResidencyLiveTail,
			Dirty:     true,
		}},
		LiveRows: rows,
	}); err != nil {
		t.Fatalf("write resize live tail metadata: %v", err)
	}

	tail, ok := store.recoveredLiveTailFromMetadata()
	if !ok {
		t.Fatal("expected resize live tail metadata to recover")
	}
	if len(tail.segments) != 1 {
		t.Fatalf("expected one recovered resize segment, got %#v", tail.segments)
	}
	segment := tail.segments[0]
	if segment.origin != terminalLiveTailOriginResize || segment.sealState != terminalLiveTailOpen || !segment.wrapPending {
		t.Fatalf("expected recovered resize segment to stay open resize tail, got %#v", segment)
	}
	if got := damageRowsToStrings(segment.rows); !reflect.DeepEqual(got, []string{"r0", "r1"}) {
		t.Fatalf("expected recovered resize rows, got %#v", got)
	}
	if got := segment.logicalLineIDs; !reflect.DeepEqual(got, []uint64{runtimeID, runtimeID}) {
		t.Fatalf("expected recovered resize runtime logical line ids, got %#v", got)
	}

	viewport, err := storeViewportWithRecoveredLiveTail(store, 0, 10, 2)
	if err != nil {
		t.Fatalf("resize recovered viewport: %v", err)
	}
	if got := vtermRowsToStrings(viewport.Rows); !reflect.DeepEqual(got, []string{"r0", "r1"}) {
		t.Fatalf("expected recovered resize viewport rows, got %#v", got)
	}
	if got := viewport.Ownership; !reflect.DeepEqual(got, []string{RowOwnershipLiveTailLive, RowOwnershipLiveTailLive}) {
		t.Fatalf("expected recovered resize rows to stay live-tail ownership, got %#v", got)
	}
	if got := viewport.LogicalLineIDs; !reflect.DeepEqual(got, []uint64{runtimeID, runtimeID}) {
		t.Fatalf("expected recovered resize viewport runtime ids, got %#v", got)
	}
	window := historyWindowFromCoreGridViewport("recover-resize-live-tail", 0, viewport)
	if window.LoadedRows != 0 || window.LoadedLines != 1 || window.LogicalTotal != 1 || window.Generation != 0 {
		t.Fatalf("expected recovered resize history window to count one mutable line without committed depth, loaded_rows=%d loaded_lines=%d total=%d gen=%d", window.LoadedRows, window.LoadedLines, window.LogicalTotal, window.Generation)
	}
	if len(window.Lines) != 1 || window.Lines[0].LogicalLineID != runtimeID || !window.Lines[0].ClippedAfter {
		t.Fatalf("expected recovered resize history window to expose clipped open runtime line, got %#v", window.Lines)
	}
}

func TestTerminalGridRecoveredResizeLiveTailLatestLimitMarksClippedMutableLineBefore(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	rows, err := terminalGridLineRowMetasFromDamageRows([]localvterm.DamageOp{
		{Cells: localVTermCellsFromString("r0"), RowKind: "recovered-resize", WrappedSet: true, Wrapped: true},
		{Cells: localVTermCellsFromString("r1"), RowKind: "recovered-resize", WrappedSet: true, Wrapped: false},
	})
	if err != nil {
		t.Fatalf("encode resize live row metadata: %v", err)
	}
	runtimeID := terminalLiveTailLogicalLineIDBase + 30
	if err := writeTerminalGridLineMetadata(store.dir, terminalGridLineMetadata{
		LiveRecords: []terminalGridLineRecordMeta{{
			ID:        runtimeID,
			StartRow:  0,
			EndRow:    1,
			Sealed:    true,
			Origin:    terminalLiveTailOriginResize,
			Residency: terminalLogicalLineResidencyLiveTail,
			Dirty:     true,
		}},
		LiveRows: rows,
	}); err != nil {
		t.Fatalf("write resize live tail metadata: %v", err)
	}

	viewport, err := storeViewportWithRecoveredLiveTailForHistory(store, 0, 1, 2)
	if err != nil {
		t.Fatalf("latest history viewport with recovered resize tail: %v", err)
	}
	window := historyWindowFromCoreGridViewport("recovered-resize-limit-clipped", 0, viewport)
	if got := historyWindowRowTexts(window); !reflect.DeepEqual(got, []string{"r1"}) {
		t.Fatalf("expected latest limit to return recovered resize line tail, got %#v", got)
	}
	if window.BeforeOffset != 0 || window.LoadedRows != 0 || window.Generation != 0 || window.FirstRowID != 0 || window.LastRowID != 0 {
		t.Fatalf("expected recovered clipped resize line not to invent committed cursor or row boundary, window=%#v", window)
	}
	if window.LoadedLines != 0 || window.LogicalTotal != 1 || window.TotalRows != 2 || !window.HasMore {
		t.Fatalf("expected recovered resize clipped line to preserve pagination signal without loaded line start, loaded=%d total=%d rows=%d has_more=%v", window.LoadedLines, window.LogicalTotal, window.TotalRows, window.HasMore)
	}
	if window.FirstLineID != 0 || window.LastLineID != 0 {
		t.Fatalf("expected recovered resize clipped-before line not to expose loaded line boundaries, first=%d last=%d", window.FirstLineID, window.LastLineID)
	}
	if len(window.Lines) != 1 {
		t.Fatalf("expected one recovered resize clipped line span, got %#v", window.Lines)
	}
	span := window.Lines[0]
	if span.StartRow != 0 || span.EndRow != 0 || span.RowKind != "recovered-resize" || span.LogicalLineID != runtimeID || !span.ClippedBefore || span.ClippedAfter {
		t.Fatalf("expected latest limit to mark recovered resize line clipped-before only, got %#v", span)
	}
}

func TestTerminalGridRecoveredLiveTailRejectsStaleReclaimedRowIDs(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	if err := store.appendRows([]terminalGridRow{
		{cells: localVTermCellsFromString("persisted")},
	}); err != nil {
		t.Fatalf("append persisted row: %v", err)
	}
	rows, err := terminalGridLineRowMetasFromDamageRows([]localvterm.DamageOp{
		{Cells: localVTermCellsFromString("stale"), WrappedSet: true, Wrapped: false},
	})
	if err != nil {
		t.Fatalf("encode live row metadata: %v", err)
	}
	_, generation, _ := store.coordinates()
	if err := writeTerminalGridLineMetadata(store.dir, terminalGridLineMetadata{
		LiveRecords: []terminalGridLineRecordMeta{{
			ID:         99,
			StartRow:   0,
			EndRow:     0,
			RowIDKnown: true,
			FirstRowID: 40,
			LastRowID:  40,
			Sealed:     true,
			Origin:     terminalLiveTailOriginReclaimed,
			Residency:  terminalLogicalLineResidencyLiveTail,
			Dirty:      false,
			Generation: generation,
		}},
		LiveRows: rows,
	}); err != nil {
		t.Fatalf("write live tail metadata: %v", err)
	}
	if _, ok := store.recoveredLiveTailFromMetadata(); ok {
		t.Fatal("expected stale reclaimed row ids outside persisted store to be rejected")
	}
}

func TestTerminalGridRecoveredLiveTailRejectsStaleReclaimedGeneration(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	if err := store.appendRows([]terminalGridRow{
		{cells: localVTermCellsFromString("persisted")},
	}); err != nil {
		t.Fatalf("append persisted row: %v", err)
	}
	rows, err := terminalGridLineRowMetasFromDamageRows([]localvterm.DamageOp{
		{Cells: localVTermCellsFromString("persisted"), WrappedSet: true, Wrapped: false},
	})
	if err != nil {
		t.Fatalf("encode live row metadata: %v", err)
	}
	_, generation, _ := store.coordinates()
	if err := writeTerminalGridLineMetadata(store.dir, terminalGridLineMetadata{
		LiveRecords: []terminalGridLineRecordMeta{{
			ID:         1,
			StartRow:   0,
			EndRow:     0,
			RowIDKnown: true,
			FirstRowID: 0,
			LastRowID:  0,
			Sealed:     true,
			Origin:     terminalLiveTailOriginReclaimed,
			Residency:  terminalLogicalLineResidencyLiveTail,
			Dirty:      false,
			Generation: generation + 1,
		}},
		LiveRows: rows,
	}); err != nil {
		t.Fatalf("write live tail metadata: %v", err)
	}
	if _, ok := store.recoveredLiveTailFromMetadata(); ok {
		t.Fatal("expected stale reclaimed generation to be rejected")
	}
}

func TestTerminalGridRecoveredLiveTailRejectsMissingReclaimedGeneration(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	if err := store.appendRows([]terminalGridRow{
		{cells: localVTermCellsFromString("persisted")},
	}); err != nil {
		t.Fatalf("append persisted row: %v", err)
	}
	rows, err := terminalGridLineRowMetasFromDamageRows([]localvterm.DamageOp{
		{Cells: localVTermCellsFromString("persisted"), WrappedSet: true, Wrapped: false},
	})
	if err != nil {
		t.Fatalf("encode live row metadata: %v", err)
	}
	if err := writeTerminalGridLineMetadata(store.dir, terminalGridLineMetadata{
		LiveRecords: []terminalGridLineRecordMeta{{
			ID:         1,
			StartRow:   0,
			EndRow:     0,
			RowIDKnown: true,
			FirstRowID: 0,
			LastRowID:  0,
			Sealed:     true,
			Origin:     terminalLiveTailOriginReclaimed,
			Residency:  terminalLogicalLineResidencyLiveTail,
			Dirty:      false,
		}},
		LiveRows: rows,
	}); err != nil {
		t.Fatalf("write live tail metadata: %v", err)
	}
	if _, ok := store.recoveredLiveTailFromMetadata(); ok {
		t.Fatal("expected reclaimed live tail metadata without generation to be rejected")
	}
}

func TestTerminalGridRecoveredLiveTailRejectsMismatchedReclaimedLogicalLineID(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	if err := store.appendRows([]terminalGridRow{
		{cells: localVTermCellsFromString("aa"), wrapped: true},
		{cells: localVTermCellsFromString("bb"), wrapped: false},
	}); err != nil {
		t.Fatalf("append persisted rows: %v", err)
	}
	rows, err := terminalGridLineRowMetasFromDamageRows([]localvterm.DamageOp{
		{Cells: localVTermCellsFromString("aa"), WrappedSet: true, Wrapped: true},
		{Cells: localVTermCellsFromString("bb"), WrappedSet: true, Wrapped: false},
	})
	if err != nil {
		t.Fatalf("encode live row metadata: %v", err)
	}
	_, generation, _ := store.coordinates()
	if err := writeTerminalGridLineMetadata(store.dir, terminalGridLineMetadata{
		LiveRecords: []terminalGridLineRecordMeta{{
			ID:         99,
			StartRow:   0,
			EndRow:     1,
			RowIDKnown: true,
			FirstRowID: 0,
			LastRowID:  1,
			Sealed:     true,
			Origin:     terminalLiveTailOriginReclaimed,
			Residency:  terminalLogicalLineResidencyLiveTail,
			Dirty:      false,
			Generation: generation,
		}},
		LiveRows: rows,
	}); err != nil {
		t.Fatalf("write live tail metadata: %v", err)
	}
	if _, ok := store.recoveredLiveTailFromMetadata(); ok {
		t.Fatal("expected reclaimed logical line id mismatch to be rejected")
	}
}

func TestTerminalGridRecoveredLiveTailUsesExplicitReclaimedRowIDs(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	if err := store.appendRows([]terminalGridRow{
		{cells: localVTermCellsFromString("aa"), wrapped: true},
		{cells: localVTermCellsFromString("bb"), wrapped: false},
	}); err != nil {
		t.Fatalf("append persisted rows: %v", err)
	}
	rows, err := terminalGridLineRowMetasFromDamageRows([]localvterm.DamageOp{
		{Cells: localVTermCellsFromString("aa"), WrappedSet: true, Wrapped: true},
		{Cells: localVTermCellsFromString("bb"), WrappedSet: true, Wrapped: false},
	})
	if err != nil {
		t.Fatalf("encode live row metadata: %v", err)
	}
	_, generation, _ := store.coordinates()
	if err := writeTerminalGridLineMetadata(store.dir, terminalGridLineMetadata{
		LiveRecords: []terminalGridLineRecordMeta{{
			ID:         1,
			StartRow:   0,
			EndRow:     1,
			RowIDKnown: true,
			FirstRowID: 0,
			LastRowID:  1,
			Sealed:     true,
			Origin:     terminalLiveTailOriginReclaimed,
			Residency:  terminalLogicalLineResidencyLiveTail,
			Dirty:      false,
			Generation: generation,
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
	if segment.origin != terminalLiveTailOriginReclaimed || segment.firstRowID != 0 || segment.lastRowID != 1 {
		t.Fatalf("expected recovered row coordinates from metadata, got %#v", segment)
	}
	if got := segment.logicalLineIDs; !reflect.DeepEqual(got, []uint64{1, 1}) {
		t.Fatalf("expected recovered logical line ids from metadata, got %#v", got)
	}
}

func TestTerminalGridRecoveredReclaimedLiveTailCountsCommittedRowsOnce(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	appendExplicitTerminalGridRowsForTest(t, store, []terminalGridRow{
		{cells: localVTermCellsFromString("p0"), wrapped: false},
		{cells: localVTermCellsFromString("p1"), wrapped: false},
		{cells: localVTermCellsFromString("r0"), wrapped: false},
		{cells: localVTermCellsFromString("r1"), wrapped: false},
	})
	rows, err := terminalGridLineRowMetasFromDamageRows([]localvterm.DamageOp{
		{Cells: localVTermCellsFromString("r0"), WrappedSet: true, Wrapped: false},
		{Cells: localVTermCellsFromString("r1"), WrappedSet: true, Wrapped: false},
	})
	if err != nil {
		t.Fatalf("encode reclaimed row metadata: %v", err)
	}
	_, generation, _ := store.coordinates()
	metadata, err := readTerminalGridLineMetadata(store.dir)
	if err != nil {
		t.Fatalf("read persisted line metadata: %v", err)
	}
	metadata.LiveRecords = []terminalGridLineRecordMeta{
		{
			ID:         3,
			StartRow:   0,
			EndRow:     0,
			RowIDKnown: true,
			FirstRowID: 2,
			LastRowID:  2,
			Sealed:     true,
			Origin:     terminalLiveTailOriginReclaimed,
			Residency:  terminalLogicalLineResidencyLiveTail,
			Dirty:      false,
			Generation: generation,
		},
		{
			ID:         4,
			StartRow:   1,
			EndRow:     1,
			RowIDKnown: true,
			FirstRowID: 3,
			LastRowID:  3,
			Sealed:     true,
			Origin:     terminalLiveTailOriginReclaimed,
			Residency:  terminalLogicalLineResidencyLiveTail,
			Dirty:      false,
			Generation: generation,
		},
	}
	metadata.LiveRows = rows
	if err := writeTerminalGridLineMetadata(store.dir, metadata); err != nil {
		t.Fatalf("write reclaimed live tail metadata: %v", err)
	}

	viewport, err := storeViewportWithRecoveredLiveTail(store, 0, 3, 20)
	if err != nil {
		t.Fatalf("latest viewport with recovered reclaimed tail: %v", err)
	}
	if got := vtermRowsToStrings(viewport.Rows); !reflect.DeepEqual(got, []string{"p1", "r0", "r1"}) {
		t.Fatalf("expected latest recovered projection to include visible prefix and reclaimed suffix once, got %#v", got)
	}
	if viewport.LoadedRows != 3 {
		t.Fatalf("expected latest committed depth to count recovered reclaimed rows once, got %d", viewport.LoadedRows)
	}
	latest := historyWindowFromCoreGridViewport("recovered-reclaimed-depth", 0, viewport)
	if latest.BeforeOffset != 3 {
		t.Fatalf("expected latest before cursor to count recovered reclaimed rows once, got %d", latest.BeforeOffset)
	}

	older, err := storeViewportWithRecoveredLiveTail(store, latest.BeforeOffset, 10, 20)
	if err != nil {
		t.Fatalf("older viewport with recovered reclaimed tail: %v", err)
	}
	if got := vtermRowsToStrings(older.Rows); !reflect.DeepEqual(got, []string{"p0"}) {
		t.Fatalf("expected older recovered projection to skip reclaimed suffix, got %#v", got)
	}
	if older.LoadedRows != 4 {
		t.Fatalf("expected older committed cursor to reach full persisted depth, got %d", older.LoadedRows)
	}
}

func TestTerminalGridRecoveredReclaimedAndLiveTailCountsOnlyReclaimedCommittedRows(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	appendExplicitTerminalGridRowsForTest(t, store, []terminalGridRow{
		{cells: localVTermCellsFromString("p0"), wrapped: false},
		{cells: localVTermCellsFromString("p1"), wrapped: false},
		{cells: localVTermCellsFromString("r0"), wrapped: false},
	})
	rows, err := terminalGridLineRowMetasFromDamageRows([]localvterm.DamageOp{
		{Cells: localVTermCellsFromString("r0"), WrappedSet: true, Wrapped: false},
		{Cells: localVTermCellsFromString("live"), WrappedSet: true, Wrapped: false},
	})
	if err != nil {
		t.Fatalf("encode mixed recovered tail metadata: %v", err)
	}
	_, generation, _ := store.coordinates()
	metadata, err := readTerminalGridLineMetadata(store.dir)
	if err != nil {
		t.Fatalf("read persisted line metadata: %v", err)
	}
	liveID := terminalLiveTailLogicalLineIDBase + 50
	metadata.LiveRecords = []terminalGridLineRecordMeta{
		{
			ID:         3,
			StartRow:   0,
			EndRow:     0,
			RowIDKnown: true,
			FirstRowID: 2,
			LastRowID:  2,
			Sealed:     true,
			Origin:     terminalLiveTailOriginReclaimed,
			Residency:  terminalLogicalLineResidencyLiveTail,
			Dirty:      false,
			Generation: generation,
		},
		{
			ID:        liveID,
			StartRow:  1,
			EndRow:    1,
			Sealed:    true,
			Origin:    terminalLiveTailOriginLive,
			Residency: terminalLogicalLineResidencyLiveTail,
			Dirty:     true,
		},
	}
	metadata.LiveRows = rows
	if err := writeTerminalGridLineMetadata(store.dir, metadata); err != nil {
		t.Fatalf("write mixed recovered tail metadata: %v", err)
	}

	viewport, err := storeViewportWithRecoveredLiveTailForHistory(store, 0, 2, 20)
	if err != nil {
		t.Fatalf("latest viewport with recovered mixed tail: %v", err)
	}
	latest := historyWindowFromCoreGridViewport("recovered-mixed-tail-depth", 0, viewport)
	if got := historyWindowRowTexts(latest); !reflect.DeepEqual(got, []string{"r0", "live"}) {
		t.Fatalf("expected latest limit to include recovered reclaimed suffix and dirty live tail, got %#v", got)
	}
	if latest.BeforeOffset != 1 || latest.LoadedRows != 1 {
		t.Fatalf("expected latest committed cursor to count only recovered reclaimed row, before=%d loaded=%d", latest.BeforeOffset, latest.LoadedRows)
	}
	if latest.LogicalTotal != 4 || latest.LoadedLines != 2 || latest.TotalRows != 4 || !latest.HasMore || latest.Generation != generation {
		t.Fatalf("expected latest totals to preserve hidden prefix and dirty live tail without dirty committed depth, loaded_lines=%d total_lines=%d total_rows=%d has_more=%v generation=%d", latest.LoadedLines, latest.LogicalTotal, latest.TotalRows, latest.HasMore, latest.Generation)
	}
	if latest.FirstRowID != 2 || latest.LastRowID != 2 {
		t.Fatalf("expected latest row boundary to follow only reclaimed committed row, got %d..%d", latest.FirstRowID, latest.LastRowID)
	}

	older, err := storeViewportWithRecoveredLiveTailForHistory(store, latest.BeforeOffset, 10, 20)
	if err != nil {
		t.Fatalf("older viewport after recovered mixed tail: %v", err)
	}
	if got := vtermRowsToStrings(older.Rows); !reflect.DeepEqual(got, []string{"p0", "p1"}) {
		t.Fatalf("expected older request to skip recovered tail and return persisted prefix only, got %#v", got)
	}
	if older.LoadedRows != 3 {
		t.Fatalf("expected older cursor to reach full committed persisted depth, got %d", older.LoadedRows)
	}
}

func TestTerminalGridRecoveredLiveTailRejectsReclaimedAfterLiveRecord(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	appendExplicitTerminalGridRowsForTest(t, store, []terminalGridRow{
		{cells: localVTermCellsFromString("r0"), wrapped: false},
	})
	rows, err := terminalGridLineRowMetasFromDamageRows([]localvterm.DamageOp{
		{Cells: localVTermCellsFromString("live"), WrappedSet: true, Wrapped: false},
		{Cells: localVTermCellsFromString("r0"), WrappedSet: true, Wrapped: false},
	})
	if err != nil {
		t.Fatalf("encode invalid ordered tail metadata: %v", err)
	}
	_, generation, _ := store.coordinates()
	metadata, err := readTerminalGridLineMetadata(store.dir)
	if err != nil {
		t.Fatalf("read persisted line metadata: %v", err)
	}
	metadata.LiveRecords = []terminalGridLineRecordMeta{
		{ID: terminalLiveTailLogicalLineIDBase + 60, StartRow: 0, EndRow: 0, Sealed: true, Origin: terminalLiveTailOriginLive, Residency: terminalLogicalLineResidencyLiveTail, Dirty: true},
		{ID: 1, StartRow: 1, EndRow: 1, RowIDKnown: true, FirstRowID: 0, LastRowID: 0, Sealed: true, Origin: terminalLiveTailOriginReclaimed, Residency: terminalLogicalLineResidencyLiveTail, Dirty: false, Generation: generation},
	}
	metadata.LiveRows = rows
	if err := writeTerminalGridLineMetadata(store.dir, metadata); err != nil {
		t.Fatalf("write invalid ordered tail metadata: %v", err)
	}
	if tail, ok := store.recoveredLiveTailFromMetadata(); ok {
		t.Fatalf("expected reclaimed record after live record to be rejected, got %#v", tail)
	}
}

func TestTerminalGridRecoveredLiveTailRejectsMixedMutableOrigins(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	rows, err := terminalGridLineRowMetasFromDamageRows([]localvterm.DamageOp{
		{Cells: localVTermCellsFromString("live"), WrappedSet: true, Wrapped: false},
		{Cells: localVTermCellsFromString("resize"), WrappedSet: true, Wrapped: false},
	})
	if err != nil {
		t.Fatalf("encode mixed mutable origin metadata: %v", err)
	}
	if err := writeTerminalGridLineMetadata(store.dir, terminalGridLineMetadata{
		LiveRecords: []terminalGridLineRecordMeta{
			{ID: terminalLiveTailLogicalLineIDBase + 70, StartRow: 0, EndRow: 0, Sealed: true, Origin: terminalLiveTailOriginLive, Residency: terminalLogicalLineResidencyLiveTail, Dirty: true},
			{ID: terminalLiveTailLogicalLineIDBase + 71, StartRow: 1, EndRow: 1, Sealed: true, Origin: terminalLiveTailOriginResize, Residency: terminalLogicalLineResidencyLiveTail, Dirty: true},
		},
		LiveRows: rows,
	}); err != nil {
		t.Fatalf("write mixed mutable origin metadata: %v", err)
	}
	if tail, ok := store.recoveredLiveTailFromMetadata(); ok {
		t.Fatalf("expected mixed live/resize records to be rejected, got %#v", tail)
	}
}

func TestTerminalGridRecoveredReclaimedLiveTailLatestLimitMarksClippedCommittedLineBefore(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	appendExplicitTerminalGridRowsForTest(t, store, []terminalGridRow{
		{cells: localVTermCellsFromString("p0"), wrapped: false},
		{cells: localVTermCellsFromString("p1"), wrapped: false},
		{cells: localVTermCellsFromString("r0"), wrapped: true},
		{cells: localVTermCellsFromString("r1"), wrapped: false},
	})
	rows, err := terminalGridLineRowMetasFromDamageRows([]localvterm.DamageOp{
		{Cells: localVTermCellsFromString("r0"), RowKind: "recovered-reclaimed", WrappedSet: true, Wrapped: true},
		{Cells: localVTermCellsFromString("r1"), RowKind: "recovered-reclaimed", WrappedSet: true, Wrapped: false},
	})
	if err != nil {
		t.Fatalf("encode reclaimed row metadata: %v", err)
	}
	_, generation, _ := store.coordinates()
	metadata, err := readTerminalGridLineMetadata(store.dir)
	if err != nil {
		t.Fatalf("read persisted line metadata: %v", err)
	}
	metadata.LiveRecords = []terminalGridLineRecordMeta{{
		ID:         3,
		StartRow:   0,
		EndRow:     1,
		RowIDKnown: true,
		FirstRowID: 2,
		LastRowID:  3,
		Sealed:     true,
		Origin:     terminalLiveTailOriginReclaimed,
		Residency:  terminalLogicalLineResidencyLiveTail,
		Dirty:      false,
		Generation: generation,
	}}
	metadata.LiveRows = rows
	if err := writeTerminalGridLineMetadata(store.dir, metadata); err != nil {
		t.Fatalf("write reclaimed live tail metadata: %v", err)
	}

	viewport, err := storeViewportWithRecoveredLiveTailForHistory(store, 0, 1, 4)
	if err != nil {
		t.Fatalf("latest history viewport with recovered reclaimed tail: %v", err)
	}
	latest := historyWindowFromCoreGridViewport("recovered-reclaimed-limit-clipped", 0, viewport)
	if got := historyWindowRowTexts(latest); !reflect.DeepEqual(got, []string{"r1"}) {
		t.Fatalf("expected latest limit to return recovered reclaimed line tail, got %#v", got)
	}
	if latest.BeforeOffset != 1 || latest.LoadedRows != 1 {
		t.Fatalf("expected latest cursor to count only returned recovered reclaimed row, before=%d loaded=%d", latest.BeforeOffset, latest.LoadedRows)
	}
	if latest.Generation != generation || latest.FirstRowID != 3 || latest.LastRowID != 3 {
		t.Fatalf("expected latest row boundary to follow kept recovered reclaimed row, gen=%d rows=%d..%d", latest.Generation, latest.FirstRowID, latest.LastRowID)
	}
	if latest.LoadedLines != 0 || latest.LogicalTotal != 3 || latest.TotalRows != 4 || !latest.HasMore {
		t.Fatalf("expected recovered reclaimed clipped line to preserve pagination signal, loaded=%d total=%d rows=%d has_more=%v", latest.LoadedLines, latest.LogicalTotal, latest.TotalRows, latest.HasMore)
	}
	if latest.FirstLineID != 0 || latest.LastLineID != 0 {
		t.Fatalf("expected recovered reclaimed clipped-before line not to expose loaded line boundaries, first=%d last=%d", latest.FirstLineID, latest.LastLineID)
	}
	if len(latest.Lines) != 1 {
		t.Fatalf("expected one recovered reclaimed clipped line span, got %#v", latest.Lines)
	}
	span := latest.Lines[0]
	if span.StartRow != 0 || span.EndRow != 0 || span.RowKind != "recovered-reclaimed" || span.LogicalLineID != 3 || !span.ClippedBefore || span.ClippedAfter {
		t.Fatalf("expected latest limit to mark recovered reclaimed line clipped-before only, got %#v", span)
	}

	older, err := storeViewportWithRecoveredLiveTailForHistory(store, latest.BeforeOffset, 10, 4)
	if err != nil {
		t.Fatalf("older viewport after recovered reclaimed clipped latest: %v", err)
	}
	if got := vtermRowsToStrings(older.Rows); !reflect.DeepEqual(got, []string{"p0", "p1", "r0"}) {
		t.Fatalf("expected older request to return unexposed recovered reclaimed prefix without repeating r1, got %#v", got)
	}
	if older.LoadedRows != 4 {
		t.Fatalf("expected older cursor to reach full committed depth, got %d", older.LoadedRows)
	}
}

func TestServerHistoryWindowLogicalLineIDsStayStableAcrossProjectionWidths(t *testing.T) {
	root := t.TempDir()
	store, err := newTerminalGridStore(root, "projection-stable-line-id")
	if err != nil {
		t.Fatalf("new grid store: %v", err)
	}
	appendExplicitTerminalGridRowsForTest(t, store, []terminalGridRow{
		{cells: localVTermCellsFromString("abcd"), rowKind: "line0", wrapped: true},
		{cells: localVTermCellsFromString("ef"), rowKind: "line0", wrapped: false},
		{cells: localVTermCellsFromString("gh"), rowKind: "line1", wrapped: false},
	})
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
	appendExplicitTerminalGridRowsForTest(t, store, []terminalGridRow{
		{cells: localVTermCellsFromString("aa"), rowKind: "line0", wrapped: false},
		{cells: localVTermCellsFromString("bbbb"), rowKind: "line1", wrapped: true},
		{cells: localVTermCellsFromString("cccc"), rowKind: "line1", wrapped: true},
		{cells: localVTermCellsFromString("dd"), rowKind: "line1", wrapped: false},
		{cells: localVTermCellsFromString("ee"), rowKind: "line2", wrapped: false},
	})
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
	if window.LoadedLines != 0 {
		t.Fatalf("expected clipped-before-only history window to load zero logical line starts, got %d", window.LoadedLines)
	}
	if window.FirstLineID != 0 || window.LastLineID != 0 {
		t.Fatalf("expected clipped-before-only history window not to expose loaded line boundaries, first=%d last=%d", window.FirstLineID, window.LastLineID)
	}
	if window.FirstRowID != 1 || window.LastRowID != 3 {
		t.Fatalf("expected canonical row ids to cover the expanded logical line 1..3, got %d..%d", window.FirstRowID, window.LastRowID)
	}
	if !window.HasMore {
		t.Fatal("expected has more because an older logical line remains before the expanded window")
	}
}

func TestServerHistoryWindowMarksLatestLimitClippedLogicalLineBefore(t *testing.T) {
	root := t.TempDir()
	store, err := newTerminalGridStore(root, "projection-latest-limit-clipped")
	if err != nil {
		t.Fatalf("new grid store: %v", err)
	}
	appendExplicitTerminalGridRowsForTest(t, store, []terminalGridRow{
		{cells: localVTermCellsFromString("aaaa"), rowKind: "line0", wrapped: true},
		{cells: localVTermCellsFromString("bbbb"), rowKind: "line0", wrapped: true},
		{cells: localVTermCellsFromString("cccc"), rowKind: "line0", wrapped: false},
	})
	if err := store.Close(); err != nil {
		t.Fatalf("close grid store: %v", err)
	}

	srv := NewServer(WithGridRoot(root), WithDefaultSize(4, 2))
	window, err := srv.HistoryWindow(t.Context(), "projection-latest-limit-clipped", HistoryWindowOptions{Limit: 2, Cols: 4})
	if err != nil {
		t.Fatalf("history window: %v", err)
	}
	if window.Op != HistoryWindowReplace {
		t.Fatalf("expected latest window to be replace, got %q", window.Op)
	}
	if got := historyRowsToStrings(window.Rows); !reflect.DeepEqual(got, []string{"bbbb", "cccc"}) {
		t.Fatalf("expected latest limited window to contain logical line tail, got %#v", got)
	}
	if window.BeforeOffset != 2 || window.LoadedRows != 2 {
		t.Fatalf("expected latest limited window cursor to cover only returned committed rows, before=%d loaded=%d", window.BeforeOffset, window.LoadedRows)
	}
	if len(window.Lines) != 1 {
		t.Fatalf("expected one logical line span, got %#v", window.Lines)
	}
	span := window.Lines[0]
	if span.StartRow != 0 || span.EndRow != 1 || span.RowKind != "line0" || !span.ClippedBefore || span.ClippedAfter {
		t.Fatalf("expected latest limited window to mark clipped-before logical line, got %#v", span)
	}
	if window.LoadedLines != 0 {
		t.Fatalf("expected latest clipped-before-only history window to load zero logical line starts, got %d", window.LoadedLines)
	}
	if window.FirstLineID != 0 || window.LastLineID != 0 {
		t.Fatalf("expected latest clipped-before-only history window not to expose loaded line boundaries, first=%d last=%d", window.FirstLineID, window.LastLineID)
	}
	if window.FirstRowID != 1 || window.LastRowID != 2 {
		t.Fatalf("expected latest limited window row ids to follow kept projection rows 1..2, got %d..%d", window.FirstRowID, window.LastRowID)
	}
}

func TestServerHistoryWindowLatestLimitKeepsReflowedSourceRowBoundary(t *testing.T) {
	root := t.TempDir()
	store, err := newTerminalGridStore(root, "projection-latest-limit-reflow-source-row")
	if err != nil {
		t.Fatalf("new grid store: %v", err)
	}
	appendExplicitTerminalGridRowsForTest(t, store, []terminalGridRow{
		{cells: localVTermCellsFromString("abcdef"), rowKind: "line0", wrapped: false},
	})
	if err := store.Close(); err != nil {
		t.Fatalf("close grid store: %v", err)
	}

	srv := NewServer(WithGridRoot(root), WithDefaultSize(2, 2))
	window, err := srv.HistoryWindow(t.Context(), "projection-latest-limit-reflow-source-row", HistoryWindowOptions{Limit: 1, Cols: 2})
	if err != nil {
		t.Fatalf("history window: %v", err)
	}
	if got := historyRowsToStrings(window.Rows); !reflect.DeepEqual(got, []string{"ef"}) {
		t.Fatalf("expected latest limit to keep only projected source row tail, got %#v", got)
	}
	if window.FirstRowID != 0 || window.LastRowID != 0 {
		t.Fatalf("expected row boundary to stay on the single source persisted row, got %d..%d", window.FirstRowID, window.LastRowID)
	}
	if window.BeforeOffset != 1 || window.LoadedRows != 1 {
		t.Fatalf("expected latest cursor to count one returned projected committed row, before=%d loaded=%d", window.BeforeOffset, window.LoadedRows)
	}
	if window.LoadedLines != 0 || window.LogicalTotal != 1 || window.TotalRows != 3 || !window.HasMore {
		t.Fatalf("expected clipped reflowed source row to preserve logical total and pagination signal, loaded=%d total=%d rows=%d has_more=%v", window.LoadedLines, window.LogicalTotal, window.TotalRows, window.HasMore)
	}
	if len(window.Lines) != 1 {
		t.Fatalf("expected one clipped projected logical line span, got %#v", window.Lines)
	}
	span := window.Lines[0]
	if span.StartRow != 0 || span.EndRow != 0 || span.RowKind != "line0" || !span.ClippedBefore || span.ClippedAfter {
		t.Fatalf("expected latest limit to mark projected source row tail clipped-before only, got %#v", span)
	}
}

func TestServerHistoryWindowOlderLimitKeepsReflowedSourceRowBoundary(t *testing.T) {
	root := t.TempDir()
	store, err := newTerminalGridStore(root, "projection-older-limit-reflow-source-row")
	if err != nil {
		t.Fatalf("new grid store: %v", err)
	}
	appendExplicitTerminalGridRowsForTest(t, store, []terminalGridRow{
		{cells: localVTermCellsFromString("abcdef"), rowKind: "line0", wrapped: false},
		{cells: localVTermCellsFromString("latest"), rowKind: "line1", wrapped: false},
	})
	if err := store.Close(); err != nil {
		t.Fatalf("close grid store: %v", err)
	}

	srv := NewServer(WithGridRoot(root), WithDefaultSize(2, 2))
	window, err := srv.HistoryWindow(t.Context(), "projection-older-limit-reflow-source-row", HistoryWindowOptions{BeforeOffset: 1, Limit: 1, Cols: 2})
	if err != nil {
		t.Fatalf("history window: %v", err)
	}
	if window.Op != HistoryWindowPrepend {
		t.Fatalf("expected older window to be prepend, got %q", window.Op)
	}
	if got := historyRowsToStrings(window.Rows); !reflect.DeepEqual(got, []string{"ef"}) {
		t.Fatalf("expected older limit to keep only projected source row tail, got %#v", got)
	}
	if window.FirstRowID != 0 || window.LastRowID != 0 {
		t.Fatalf("expected row boundary to stay on the older source persisted row, got %d..%d", window.FirstRowID, window.LastRowID)
	}
	if window.BeforeOffset != 2 || window.LoadedRows != 2 {
		t.Fatalf("expected older cursor to reach two committed source rows, before=%d loaded=%d", window.BeforeOffset, window.LoadedRows)
	}
	if window.LoadedLines != 0 || window.LogicalTotal != 2 || window.TotalRows != 5 || !window.HasMore {
		t.Fatalf("expected clipped older reflowed source row to preserve logical total and pagination signal, loaded=%d total=%d rows=%d has_more=%v", window.LoadedLines, window.LogicalTotal, window.TotalRows, window.HasMore)
	}
	if len(window.Lines) != 1 {
		t.Fatalf("expected one clipped projected logical line span, got %#v", window.Lines)
	}
	span := window.Lines[0]
	if span.StartRow != 0 || span.EndRow != 0 || span.RowKind != "line0" || !span.ClippedBefore || span.ClippedAfter {
		t.Fatalf("expected older limit to mark projected source row tail clipped-before only, got %#v", span)
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
