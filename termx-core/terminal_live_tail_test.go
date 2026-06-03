package termx

import (
	"reflect"
	"testing"
	"time"

	localvterm "github.com/lozzow/termx/termx-vterm/vterm"
)

func TestTerminalPrimaryLiveTailSegmentsTrackOriginAndSealState(t *testing.T) {
	tail := newTerminalPrimaryLiveTail([]localvterm.DamageOp{{
		Cells:      localVTermCellsFromString("wrap"),
		WrappedSet: true,
		Wrapped:    true,
	}}, false)
	if len(tail.segments) != 1 {
		t.Fatalf("expected one live-tail segment, got %d", len(tail.segments))
	}
	if got := tail.segments[0].origin; got != terminalLiveTailOriginLive {
		t.Fatalf("expected live origin, got %q", got)
	}
	if got := tail.segments[0].sealState; got != terminalLiveTailOpen {
		t.Fatalf("expected wrapped segment to stay open, got %q", got)
	}
	if got := tail.segments[0].logicalLineIDs; len(got) != 1 || got[0] < terminalLiveTailLogicalLineIDBase {
		t.Fatalf("expected live segment to store runtime logical line id, got %#v", got)
	}

	tail.replaceRows([]localvterm.DamageOp{{
		Cells:      localVTermCellsFromString("done"),
		WrappedSet: true,
		Wrapped:    false,
	}}, terminalLiveTailOriginReclaimed, false)
	if got := tail.segments[0].origin; got != terminalLiveTailOriginReclaimed {
		t.Fatalf("expected reclaimed origin, got %q", got)
	}
	if got := tail.segments[0].sealState; got != terminalLiveTailSealed {
		t.Fatalf("expected unwrapped reclaimed segment to be sealed, got %q", got)
	}

	tail.replaceLiveRows([]localvterm.DamageOp{{
		Cells:      localVTermCellsFromString("next"),
		WrappedSet: true,
		Wrapped:    false,
	}}, true)
	tail.setWrapPending(true)
	if !tail.wrapPending || !tail.segments[1].wrapPending {
		t.Fatal("expected wrap pending to be stored on tail and live segment")
	}
	if got := tail.segments[0].sealState; got != terminalLiveTailSealed {
		t.Fatalf("expected reclaimed segment to remain sealed, got %q", got)
	}
	if got := tail.segments[1].sealState; got != terminalLiveTailOpen {
		t.Fatalf("expected wrap pending to keep live segment open, got %q", got)
	}

	tail.reset()
	if tail.hasState() {
		t.Fatalf("expected reset live tail to have no state, got %#v", tail)
	}
}

func TestTerminalPrimaryLiveTailLogicalLineRecordsTrackReclaimedAndLiveRows(t *testing.T) {
	var tail terminalPrimaryLiveTail
	tail.replaceReclaimedPrefixWithLogicalLineIDs([]localvterm.DamageOp{
		{Cells: localVTermCellsFromString("aa"), WrappedSet: true, Wrapped: true},
		{Cells: localVTermCellsFromString("bb"), WrappedSet: true, Wrapped: false},
	}, []uint64{41, 41}, 7, 40, 41)
	tail.replaceLiveRows([]localvterm.DamageOp{
		{Cells: localVTermCellsFromString("cc"), WrappedSet: true, Wrapped: true},
	}, true)

	window := tail.window(0, tail.rowCount())
	if got := damageRowsToStrings(window.rows); !reflect.DeepEqual(got, []string{"aa", "bb", "cc"}) {
		t.Fatalf("expected reclaimed and live rows in window, got %#v", got)
	}
	if got := window.logicalLineIDs; len(got) != 3 || got[0] != 41 || got[1] != 41 || got[2] < terminalLiveTailLogicalLineIDBase {
		t.Fatalf("expected reclaimed rows to keep persisted logical line id and live row to get runtime id, got %#v", got)
	}

	records := tail.logicalLineRecords()
	if len(records) != 2 {
		t.Fatalf("expected two live tail logical line records, got %#v", records)
	}
	if records[0] != (terminalLiveTailLogicalLineRecord{id: 41, startRow: 0, endRow: 1, sealState: terminalLiveTailSealed, origin: terminalLiveTailOriginReclaimed, residency: terminalLogicalLineResidencyLiveTail, dirty: false, generation: 7, rowIDKnown: true, firstRowID: 40, lastRowID: 41}) {
		t.Fatalf("unexpected reclaimed record: %#v", records[0])
	}
	if records[1].id != window.logicalLineIDs[2] || records[1].id < terminalLiveTailLogicalLineIDBase || records[1].startRow != 2 || records[1].endRow != 2 || records[1].sealState != terminalLiveTailOpen || records[1].origin != terminalLiveTailOriginLive || records[1].residency != terminalLogicalLineResidencyLiveTail || !records[1].dirty {
		t.Fatalf("unexpected live record: %#v", records[1])
	}
}

func TestTerminalPrimaryLiveTailWindowCarriesLogicalLineTimestampRange(t *testing.T) {
	start := time.Unix(10, 0).UTC()
	end := time.Unix(30, 0).UTC()
	var tail terminalPrimaryLiveTail
	tail.replaceLiveRowsWithLogicalLineIDs([]localvterm.DamageOp{
		{Cells: localVTermCellsFromString("aa"), Timestamp: end, WrappedSet: true, Wrapped: true},
		{Cells: localVTermCellsFromString("bb"), Timestamp: start, WrappedSet: true, Wrapped: false},
	}, []uint64{terminalLiveTailLogicalLineIDBase + 1, terminalLiveTailLogicalLineIDBase + 1}, false)

	window := tail.window(0, tail.rowCount())
	if got := damageRowsToStrings(window.rows); !reflect.DeepEqual(got, []string{"aa", "bb"}) {
		t.Fatalf("expected live tail rows in window, got %#v", got)
	}
	if len(window.lineTimestampStart) != 2 || !window.lineTimestampStart[0].Equal(start) || !window.lineTimestampStart[1].Equal(start) {
		t.Fatalf("expected line start timestamp range on both rows, got %#v", window.lineTimestampStart)
	}
	if len(window.lineTimestampEnd) != 2 || !window.lineTimestampEnd[0].Equal(end) || !window.lineTimestampEnd[1].Equal(end) {
		t.Fatalf("expected line end timestamp range on both rows, got %#v", window.lineTimestampEnd)
	}

	clipped := tail.window(1, tail.rowCount())
	if got := damageRowsToStrings(clipped.rows); !reflect.DeepEqual(got, []string{"bb"}) {
		t.Fatalf("expected clipped live tail row in window, got %#v", got)
	}
	if len(clipped.lineTimestampStart) != 1 || !clipped.lineTimestampStart[0].Equal(start) {
		t.Fatalf("expected clipped row to keep full logical line start timestamp, got %#v", clipped.lineTimestampStart)
	}
	if len(clipped.lineTimestampEnd) != 1 || !clipped.lineTimestampEnd[0].Equal(end) {
		t.Fatalf("expected clipped row to keep full logical line end timestamp, got %#v", clipped.lineTimestampEnd)
	}
}

func TestTerminalPrimaryLiveTailLogicalLineRecordsUsePayloadMetadata(t *testing.T) {
	start := time.Unix(10, 0).UTC()
	end := time.Unix(30, 0).UTC()
	var tail terminalPrimaryLiveTail
	tail.replaceLiveRowsWithLogicalLineIDs([]localvterm.DamageOp{
		{Cells: localVTermCellsFromString("aa"), RowKind: "live-kind", Timestamp: end, WrappedSet: true, Wrapped: true},
		{Cells: localVTermCellsFromString("bb"), Timestamp: start, WrappedSet: true, Wrapped: false},
	}, []uint64{terminalLiveTailLogicalLineIDBase + 1, terminalLiveTailLogicalLineIDBase + 1}, false)

	records := tail.logicalLineRecords()
	if len(records) != 1 {
		t.Fatalf("expected one live-tail logical line record, got %#v", records)
	}
	record := records[0]
	if record.rowKind != "live-kind" || !record.timestampStart.Equal(start) || !record.timestampEnd.Equal(end) {
		t.Fatalf("expected live-tail record metadata from logical line payload, got %#v", record)
	}
}

func TestTerminalPrimaryLiveTailReclaimedZeroCoordinatesRequirePersistedIDs(t *testing.T) {
	var tail terminalPrimaryLiveTail
	tail.replaceRows([]localvterm.DamageOp{{
		Cells:      localVTermCellsFromString("aa"),
		WrappedSet: true,
		Wrapped:    false,
	}}, terminalLiveTailOriginReclaimed, false)

	records := tail.logicalLineRecords()
	if len(records) != 0 {
		t.Fatalf("expected reclaimed rows without persisted ids to skip recoverable records, got %#v", records)
	}
	metas := terminalGridLineRecordMetasFromLiveTailRecords(records)
	if len(metas) != 0 {
		t.Fatalf("expected metadata to omit reclaimed rows without persisted ids, got %#v", metas)
	}
}

func TestTerminalPrimaryLiveTailReclaimedRowCoordinatesDoNotFallbackLogicalLineIDs(t *testing.T) {
	var tail terminalPrimaryLiveTail
	tail.replaceReclaimedPrefix([]localvterm.DamageOp{
		{Cells: localVTermCellsFromString("aa"), WrappedSet: true, Wrapped: true},
		{Cells: localVTermCellsFromString("bb"), WrappedSet: true, Wrapped: false},
	}, 7, 40, 41)

	window := tail.window(0, tail.rowCount())
	if got := window.logicalLineIDs; !reflect.DeepEqual(got, []uint64{0, 0}) {
		t.Fatalf("expected reclaimed row coordinates without explicit ids to suppress logical line ids, got %#v", got)
	}
	if records := tail.logicalLineRecords(); len(records) != 0 {
		t.Fatalf("expected reclaimed rows without explicit ids to suppress record metadata, got %#v", records)
	}
}

func TestTerminalGridLineRecordMetasOnlyWriteReclaimedRowIDs(t *testing.T) {
	records := []terminalLiveTailLogicalLineRecord{
		{
			id:         41,
			startRow:   0,
			endRow:     0,
			sealState:  terminalLiveTailSealed,
			origin:     terminalLiveTailOriginReclaimed,
			residency:  terminalLogicalLineResidencyLiveTail,
			generation: 7,
			rowIDKnown: true,
			firstRowID: 40,
			lastRowID:  40,
		},
		{
			id:         terminalLiveTailLogicalLineIDBase + 1,
			startRow:   1,
			endRow:     1,
			sealState:  terminalLiveTailOpen,
			origin:     terminalLiveTailOriginLive,
			residency:  terminalLogicalLineResidencyLiveTail,
			dirty:      true,
			rowIDKnown: true,
			firstRowID: 50,
			lastRowID:  50,
		},
		{
			id:         terminalLiveTailLogicalLineIDBase + 2,
			startRow:   2,
			endRow:     2,
			sealState:  terminalLiveTailOpen,
			origin:     terminalLiveTailOriginResize,
			residency:  terminalLogicalLineResidencyLiveTail,
			dirty:      true,
			rowIDKnown: true,
			firstRowID: 60,
			lastRowID:  60,
		},
	}

	metas := terminalGridLineRecordMetasFromLiveTailRecords(records)
	if len(metas) != 3 {
		t.Fatalf("expected three metadata records, got %#v", metas)
	}
	if !metas[0].RowIDKnown || metas[0].FirstRowID != 40 || metas[0].LastRowID != 40 {
		t.Fatalf("expected reclaimed record to write explicit row ids, got %#v", metas[0])
	}
	for _, meta := range metas[1:] {
		if meta.RowIDKnown || meta.FirstRowID != 0 || meta.LastRowID != 0 {
			t.Fatalf("expected non-reclaimed record to omit row ids, got %#v", meta)
		}
	}
}

func TestTerminalGridStoreRecordLiveTailLineStateWritesPayloadMetadata(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	start := time.Unix(9, 10).UTC()
	end := time.Unix(10, 20).UTC()
	rows := []localvterm.DamageOp{
		{Cells: localVTermCellsFromString("a"), Timestamp: end, RowKind: "output", WrappedSet: true, Wrapped: true},
		{Cells: localVTermCellsFromString("b"), Timestamp: start, RowKind: "continuation", WrappedSet: true, Wrapped: false},
	}
	records := []terminalLiveTailLogicalLineRecord{{
		id:        terminalLiveTailLogicalLineIDBase + 1,
		startRow:  0,
		endRow:    1,
		sealState: terminalLiveTailSealed,
		origin:    terminalLiveTailOriginLive,
		residency: terminalLogicalLineResidencyLiveTail,
		dirty:     true,
	}}

	if err := store.recordLiveTailLineState(records, rows); err != nil {
		t.Fatalf("record live tail line state: %v", err)
	}
	metadata, err := readTerminalGridLineMetadata(store.dir)
	if err != nil {
		t.Fatalf("read live tail metadata: %v", err)
	}
	if len(metadata.LiveRecords) != 1 {
		t.Fatalf("expected one live tail record, got %#v", metadata.LiveRecords)
	}
	record := metadata.LiveRecords[0]
	if record.RowKind != "output" {
		t.Fatalf("expected row kind from first payload row, got %#v", record)
	}
	if record.TimestampStartUnixNano == nil || *record.TimestampStartUnixNano != start.UnixNano() {
		t.Fatalf("expected timestamp range start %d, got %#v", start.UnixNano(), record)
	}
	if record.TimestampEndUnixNano == nil || *record.TimestampEndUnixNano != end.UnixNano() {
		t.Fatalf("expected timestamp range end %d, got %#v", end.UnixNano(), record)
	}
}

func TestTerminalGridStoreRecoveredLiveTailRejectsMismatchedPayloadMetadata(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*terminalGridLineRecordMeta)
	}{
		{
			name: "row-kind",
			mutate: func(record *terminalGridLineRecordMeta) {
				record.RowKind = "wrong-kind"
			},
		},
		{
			name: "timestamp-start",
			mutate: func(record *terminalGridLineRecordMeta) {
				wrong := time.Unix(30, 0).UTC().UnixNano()
				record.TimestampStartUnixNano = &wrong
			},
		},
		{
			name: "timestamp-end",
			mutate: func(record *terminalGridLineRecordMeta) {
				wrong := time.Unix(31, 0).UTC().UnixNano()
				record.TimestampEndUnixNano = &wrong
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newMemoryTerminalGridStoreForTest(t)
			defer store.Close()
			rows, err := terminalGridLineRowMetasFromDamageRows([]localvterm.DamageOp{{
				Cells:      localVTermCellsFromString("tail"),
				Timestamp:  time.Unix(20, 0).UTC(),
				RowKind:    "actual-kind",
				WrappedSet: true,
				Wrapped:    false,
			}})
			if err != nil {
				t.Fatalf("encode live row metadata: %v", err)
			}
			timestampStart := time.Unix(20, 0).UTC().UnixNano()
			timestampEnd := time.Unix(20, 0).UTC().UnixNano()
			record := terminalGridLineRecordMeta{
				ID:                     terminalLiveTailLogicalLineIDBase + 1,
				StartRow:               0,
				EndRow:                 0,
				Sealed:                 true,
				Origin:                 terminalLiveTailOriginLive,
				Residency:              terminalLogicalLineResidencyLiveTail,
				RowKind:                "actual-kind",
				TimestampStartUnixNano: &timestampStart,
				TimestampEndUnixNano:   &timestampEnd,
				Dirty:                  true,
			}
			tc.mutate(&record)
			if err := writeTerminalGridLineMetadata(store.dir, terminalGridLineMetadata{
				LiveRecords: []terminalGridLineRecordMeta{record},
				LiveRows:    rows,
			}); err != nil {
				t.Fatalf("write live tail metadata: %v", err)
			}

			if tail, ok := store.recoveredLiveTailFromMetadata(); ok {
				t.Fatalf("expected mismatched payload metadata to be rejected, got %#v", tail)
			}
		})
	}
}

func TestTerminalGridStoreRecordLiveTailLineStateValidatesRecordsBeforeWrite(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	rows := []localvterm.DamageOp{{
		Cells:      localVTermCellsFromString("tail"),
		WrappedSet: true,
		Wrapped:    false,
	}}
	valid := []terminalLiveTailLogicalLineRecord{{
		id:        terminalLiveTailLogicalLineIDBase + 1,
		startRow:  0,
		endRow:    0,
		sealState: terminalLiveTailSealed,
		origin:    terminalLiveTailOriginLive,
		residency: terminalLogicalLineResidencyLiveTail,
		dirty:     true,
	}}
	if err := store.recordLiveTailLineState(valid, rows); err != nil {
		t.Fatalf("record valid live tail line state: %v", err)
	}
	metadata, err := readTerminalGridLineMetadata(store.dir)
	if err != nil {
		t.Fatalf("read valid live tail line metadata: %v", err)
	}
	if len(metadata.LiveRecords) != 1 || len(metadata.LiveRows) != 1 {
		t.Fatalf("expected valid live tail metadata to be written, got %#v", metadata)
	}
	if metadata.LiveRecords[0].Source != "" {
		t.Fatalf("expected live tail metadata not to carry persisted record source, got %#v", metadata.LiveRecords[0])
	}

	invalid := valid
	invalid[0].id = 0
	if err := store.recordLiveTailLineState(invalid, rows); err != nil {
		t.Fatalf("record invalid live tail line state: %v", err)
	}
	metadata, err = readTerminalGridLineMetadata(store.dir)
	if err != nil {
		t.Fatalf("read invalid live tail line metadata: %v", err)
	}
	if len(metadata.LiveRecords) != 0 || len(metadata.LiveRows) != 0 {
		t.Fatalf("expected invalid live tail metadata to be cleared, got %#v", metadata)
	}
}

func TestTerminalGridStoreRecordLiveTailLineStateRejectsPartialCoverage(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	rows := []localvterm.DamageOp{
		{Cells: localVTermCellsFromString("a"), WrappedSet: true, Wrapped: false},
		{Cells: localVTermCellsFromString("b"), WrappedSet: true, Wrapped: false},
	}
	records := []terminalLiveTailLogicalLineRecord{{
		id:        terminalLiveTailLogicalLineIDBase + 1,
		startRow:  0,
		endRow:    0,
		sealState: terminalLiveTailSealed,
		origin:    terminalLiveTailOriginLive,
		residency: terminalLogicalLineResidencyLiveTail,
		dirty:     true,
	}}
	if err := store.recordLiveTailLineState(records, rows); err != nil {
		t.Fatalf("record partial live tail line state: %v", err)
	}
	metadata, err := readTerminalGridLineMetadata(store.dir)
	if err != nil {
		t.Fatalf("read partial live tail line metadata: %v", err)
	}
	if len(metadata.LiveRecords) != 0 || len(metadata.LiveRows) != 0 {
		t.Fatalf("expected partial live tail metadata to be cleared, got %#v", metadata)
	}
}

func TestTerminalLiveTailLogicalLineRecordPayloadCarriesCellsAndMetadata(t *testing.T) {
	start := time.Unix(10, 0).UTC()
	end := time.Unix(30, 0).UTC()
	rows := []localvterm.DamageOp{
		{Cells: localVTermCellsFromString("aa"), RowKind: "live-kind", Timestamp: end, WrappedSet: true, Wrapped: true},
		{Cells: localVTermCellsFromString("bb"), Timestamp: start, WrappedSet: true, Wrapped: false},
		{Cells: localVTermCellsFromString("cc"), RowKind: "next-kind", Timestamp: end, WrappedSet: true, Wrapped: false},
	}
	record := terminalLiveTailLogicalLineRecord{
		id:        terminalLiveTailLogicalLineIDBase + 1,
		startRow:  0,
		endRow:    1,
		sealState: terminalLiveTailSealed,
		origin:    terminalLiveTailOriginLive,
		residency: terminalLogicalLineResidencyLiveTail,
		dirty:     true,
	}

	payload, ok := terminalLiveTailLogicalLineRecordPayload(record, rows)
	if !ok {
		t.Fatal("expected live-tail logical line payload")
	}
	if got := vtermRowsToStrings(payload.cellRows()); !reflect.DeepEqual(got, []string{"aa", "bb"}) {
		t.Fatalf("expected live-tail payload cells from record row range, got %#v", got)
	}
	if payload.rowKind() != "live-kind" || !payload.timestampStart().Equal(start) || !payload.timestampEnd().Equal(end) {
		t.Fatalf("unexpected live-tail payload metadata row_kind=%q start=%v end=%v", payload.rowKind(), payload.timestampStart(), payload.timestampEnd())
	}

	metas := terminalGridLineRecordMetasFromLiveTailRecordsWithRows([]terminalLiveTailLogicalLineRecord{record}, rows)
	if len(metas) != 1 || metas[0].RowKind != "live-kind" || metas[0].TimestampStartUnixNano == nil || *metas[0].TimestampStartUnixNano != start.UnixNano() || metas[0].TimestampEndUnixNano == nil || *metas[0].TimestampEndUnixNano != end.UnixNano() {
		t.Fatalf("expected live-tail metadata to come from logical line payload, got %#v", metas)
	}
}

func TestTerminalGridStoreRecordLiveTailLineStateRejectsReclaimedWithoutGeneration(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	rows := []localvterm.DamageOp{{
		Cells:      localVTermCellsFromString("reclaimed"),
		WrappedSet: true,
		Wrapped:    false,
	}}
	records := []terminalLiveTailLogicalLineRecord{{
		id:         41,
		startRow:   0,
		endRow:     0,
		sealState:  terminalLiveTailSealed,
		origin:     terminalLiveTailOriginReclaimed,
		residency:  terminalLogicalLineResidencyLiveTail,
		rowIDKnown: true,
		firstRowID: 40,
		lastRowID:  40,
	}}
	if err := store.recordLiveTailLineState(records, rows); err != nil {
		t.Fatalf("record reclaimed live tail without generation: %v", err)
	}
	metadata, err := readTerminalGridLineMetadata(store.dir)
	if err != nil {
		t.Fatalf("read reclaimed live tail metadata: %v", err)
	}
	if len(metadata.LiveRecords) != 0 || len(metadata.LiveRows) != 0 {
		t.Fatalf("expected reclaimed live tail without generation to be cleared, got %#v", metadata)
	}
}

func TestTerminalPrimaryLiveTailPrefersExplicitReclaimedLogicalLineIDs(t *testing.T) {
	var tail terminalPrimaryLiveTail
	tail.replaceReclaimedPrefixWithLogicalLineIDs([]localvterm.DamageOp{
		{Cells: localVTermCellsFromString("abcd"), WrappedSet: true, Wrapped: true},
		{Cells: localVTermCellsFromString("ef"), WrappedSet: true, Wrapped: false},
	}, []uint64{99, 99}, 7, 40, 41)

	window := tail.window(0, tail.rowCount())
	if got := window.logicalLineIDs; !reflect.DeepEqual(got, []uint64{99, 99}) {
		t.Fatalf("expected explicit reclaimed logical line ids to be preserved, got %#v", got)
	}
	records := tail.logicalLineRecords()
	if len(records) != 1 || records[0].id != 99 || records[0].origin != terminalLiveTailOriginReclaimed || records[0].sealState != terminalLiveTailSealed || records[0].residency != terminalLogicalLineResidencyLiveTail || records[0].dirty || records[0].generation != 7 || !records[0].rowIDKnown || records[0].firstRowID != 40 || records[0].lastRowID != 41 {
		t.Fatalf("expected explicit reclaimed id in record view, got %#v", records)
	}
}

func TestTerminalPrimaryLiveTailLogicalLineRecordsPreferExplicitIDs(t *testing.T) {
	var tail terminalPrimaryLiveTail
	tail.replaceReclaimedPrefixWithLogicalLineIDs([]localvterm.DamageOp{
		{Cells: localVTermCellsFromString("aa"), WrappedSet: true, Wrapped: true},
		{Cells: localVTermCellsFromString("bb"), WrappedSet: true, Wrapped: false},
	}, []uint64{91, 92}, 7, 40, 41)

	records := tail.logicalLineRecords()
	if len(records) != 2 {
		t.Fatalf("expected explicit logical line ids to split records despite wrapped=true, got %#v", records)
	}
	if records[0].id != 91 || records[0].startRow != 0 || records[0].endRow != 0 {
		t.Fatalf("unexpected first explicit-id record: %#v", records[0])
	}
	if records[1].id != 92 || records[1].startRow != 1 || records[1].endRow != 1 {
		t.Fatalf("unexpected second explicit-id record: %#v", records[1])
	}
}

func TestTerminalPrimaryLiveTailLiveLogicalLineIDsPreferCompleteExplicitIDs(t *testing.T) {
	var tail terminalPrimaryLiveTail
	firstID := terminalLiveTailLogicalLineIDBase + 10
	secondID := terminalLiveTailLogicalLineIDBase + 11
	tail.replaceLiveRowsWithLogicalLineIDs([]localvterm.DamageOp{
		{Cells: localVTermCellsFromString("aa"), WrappedSet: true, Wrapped: true},
		{Cells: localVTermCellsFromString("bb"), WrappedSet: true, Wrapped: false},
	}, []uint64{firstID, secondID}, false)

	window := tail.window(0, tail.rowCount())
	if got := window.logicalLineIDs; !reflect.DeepEqual(got, []uint64{firstID, secondID}) {
		t.Fatalf("expected complete explicit live ids to override wrapped grouping, got %#v", got)
	}
	records := tail.logicalLineRecords()
	if len(records) != 2 {
		t.Fatalf("expected complete explicit live ids to split records despite wrapped=true, got %#v", records)
	}
	if records[0].id != firstID || records[0].startRow != 0 || records[0].endRow != 0 {
		t.Fatalf("unexpected first explicit live record: %#v", records[0])
	}
	if records[1].id != secondID || records[1].startRow != 1 || records[1].endRow != 1 {
		t.Fatalf("unexpected second explicit live record: %#v", records[1])
	}
}

func TestTerminalPrimaryLiveTailLiveLogicalLineIDsIgnorePersistedNamespace(t *testing.T) {
	var tail terminalPrimaryLiveTail
	tail.replaceLiveRowsWithLogicalLineIDs([]localvterm.DamageOp{
		{Cells: localVTermCellsFromString("aa"), WrappedSet: true, Wrapped: true},
		{Cells: localVTermCellsFromString("bb"), WrappedSet: true, Wrapped: false},
	}, []uint64{1, 1}, false)

	window := tail.window(0, tail.rowCount())
	if got := window.logicalLineIDs; len(got) != 2 || got[0] < terminalLiveTailLogicalLineIDBase || got[0] != got[1] {
		t.Fatalf("expected live rows to replace persisted namespace ids with runtime ids, got %#v", got)
	}
}

func TestTerminalPrimaryLiveTailWindowSuppressesWrongNamespaceLogicalLineIDs(t *testing.T) {
	runtimeID := terminalLiveTailLogicalLineIDBase + 20
	tail := terminalPrimaryLiveTail{segments: []terminalLiveTailSegment{
		{
			origin:    terminalLiveTailOriginLive,
			sealState: terminalLiveTailSealed,
			rows: []localvterm.DamageOp{
				{Cells: localVTermCellsFromString("live-a"), WrappedSet: true, Wrapped: true},
				{Cells: localVTermCellsFromString("live-b"), WrappedSet: true, Wrapped: false},
			},
			logicalLineIDs: []uint64{1, 1},
		},
		{
			origin:    terminalLiveTailOriginReclaimed,
			sealState: terminalLiveTailSealed,
			rows: []localvterm.DamageOp{
				{Cells: localVTermCellsFromString("rec-a"), WrappedSet: true, Wrapped: true},
				{Cells: localVTermCellsFromString("rec-b"), WrappedSet: true, Wrapped: false},
			},
			logicalLineIDs: []uint64{runtimeID, runtimeID},
			generation:     7,
			firstRowID:     40,
			lastRowID:      41,
		},
	}}

	window := tail.window(0, tail.rowCount())
	if got := window.logicalLineIDs; !reflect.DeepEqual(got, []uint64{0, 0, 0, 0}) {
		t.Fatalf("expected wrong-namespace logical line ids to be suppressed in projection, got %#v", got)
	}
	if records := tail.logicalLineRecords(); len(records) != 0 {
		t.Fatalf("expected wrong-namespace logical line ids to suppress record metadata, got %#v", records)
	}
}

func TestTerminalPrimaryLiveTailLogicalLineRecordsRequireCompleteIDs(t *testing.T) {
	tail := terminalPrimaryLiveTail{segments: []terminalLiveTailSegment{{
		origin:    terminalLiveTailOriginLive,
		sealState: terminalLiveTailSealed,
		rows: []localvterm.DamageOp{
			{Cells: localVTermCellsFromString("aa"), WrappedSet: true, Wrapped: true},
			{Cells: localVTermCellsFromString("bb"), WrappedSet: true, Wrapped: false},
		},
		logicalLineIDs: []uint64{terminalLiveTailLogicalLineIDBase + 1, 0},
	}}}

	if records := tail.logicalLineRecords(); len(records) != 0 {
		t.Fatalf("expected incomplete live-tail logical ids to suppress record metadata, got %#v", records)
	}
}

func TestTerminalPrimaryLiveTailWindowSuppressesPartialLogicalLineIDs(t *testing.T) {
	tail := terminalPrimaryLiveTail{segments: []terminalLiveTailSegment{{
		origin:    terminalLiveTailOriginLive,
		sealState: terminalLiveTailSealed,
		rows: []localvterm.DamageOp{
			{Cells: localVTermCellsFromString("aa"), WrappedSet: true, Wrapped: true},
			{Cells: localVTermCellsFromString("bb"), WrappedSet: true, Wrapped: false},
		},
		logicalLineIDs: []uint64{terminalLiveTailLogicalLineIDBase + 1, 0},
	}}}

	window := tail.window(0, tail.rowCount())
	if got := window.logicalLineIDs; !reflect.DeepEqual(got, []uint64{0, 0}) {
		t.Fatalf("expected partial live-tail ids to be suppressed in projection, got %#v", got)
	}
}

func TestTerminalPrimaryLiveTailLogicalLineRecordsRejectPartialSegments(t *testing.T) {
	tail := terminalPrimaryLiveTail{segments: []terminalLiveTailSegment{
		{
			origin:    terminalLiveTailOriginLive,
			sealState: terminalLiveTailSealed,
			rows: []localvterm.DamageOp{{
				Cells:      localVTermCellsFromString("bad"),
				WrappedSet: true,
				Wrapped:    false,
			}},
		},
		{
			origin:         terminalLiveTailOriginLive,
			sealState:      terminalLiveTailSealed,
			rows:           []localvterm.DamageOp{{Cells: localVTermCellsFromString("good"), WrappedSet: true, Wrapped: false}},
			logicalLineIDs: []uint64{terminalLiveTailLogicalLineIDBase + 2},
		},
	}}

	if records := tail.logicalLineRecords(); len(records) != 0 {
		t.Fatalf("expected incomplete prefix segment to suppress all live-tail records, got %#v", records)
	}
}

func TestTerminalPrimaryLiveTailKeepsLiveLogicalLineIDAcrossReplacement(t *testing.T) {
	var tail terminalPrimaryLiveTail
	tail.replaceLiveRows([]localvterm.DamageOp{
		{Cells: localVTermCellsFromString("aa"), WrappedSet: true, Wrapped: true},
		{Cells: localVTermCellsFromString("bb"), WrappedSet: true, Wrapped: true},
	}, true)
	first := tail.window(0, tail.rowCount()).logicalLineIDs
	if len(first) != 2 || first[0] == 0 || first[0] != first[1] {
		t.Fatalf("expected initial live wrapped rows to share runtime id, got %#v", first)
	}

	tail.replaceLiveRowsWithLogicalLineIDs([]localvterm.DamageOp{
		{Cells: localVTermCellsFromString("aa"), WrappedSet: true, Wrapped: true},
		{Cells: localVTermCellsFromString("bb"), WrappedSet: true, Wrapped: true},
		{Cells: localVTermCellsFromString("cc"), WrappedSet: true, Wrapped: true},
	}, []uint64{first[0], first[1], 0}, true)
	next := tail.window(0, tail.rowCount()).logicalLineIDs
	if len(next) != 3 || next[0] != first[0] || next[1] != first[0] || next[2] != first[0] {
		t.Fatalf("expected continued open live line to keep runtime id across replacement, first=%#v next=%#v", first, next)
	}
}

func TestTerminalPrimaryLiveTailOnlyExplicitReclaimedReplacementMarksPersistedTail(t *testing.T) {
	var appendTail terminalPrimaryLiveTail
	appendTail.replaceReclaimedPrefixWithLogicalLineIDs([]localvterm.DamageOp{{
		Cells:      localVTermCellsFromString("keep"),
		WrappedSet: true,
		Wrapped:    false,
	}}, []uint64{2}, 7, 1, 1)
	appendTail.replaceLiveRows([]localvterm.DamageOp{{
		Cells:      localVTermCellsFromString("live"),
		WrappedSet: true,
		Wrapped:    false,
	}}, false)
	if appendTail.dirtyLiveRowsReplaceAuthoritativeReclaimedTail(0, 2) {
		t.Fatalf("expected ordinary live append after reclaimed suffix not to mark persisted tail replacement, tail=%#v", appendTail.segments)
	}

	var replaceTail terminalPrimaryLiveTail
	replaceTail.replaceReclaimedPrefixWithLogicalLineIDs([]localvterm.DamageOp{{
		Cells:      localVTermCellsFromString("keep"),
		WrappedSet: true,
		Wrapped:    false,
	}}, []uint64{2}, 7, 1, 1)
	replaceTail.replaceReclaimedTailWithLiveRows([]localvterm.DamageOp{{
		Cells:      localVTermCellsFromString("EDIT"),
		WrappedSet: true,
		Wrapped:    false,
	}}, false)
	if !replaceTail.dirtyLiveRowsReplaceAuthoritativeReclaimedTail(0, 2) {
		t.Fatalf("expected explicit reclaimed tail replacement to mark persisted tail replacement, tail=%#v", replaceTail.segments)
	}
}

func TestTerminalPrimaryLiveTailKeepsResizeLogicalLineIDAfterReclaimedTrim(t *testing.T) {
	var tail terminalPrimaryLiveTail
	tail.replaceResizeRows([]localvterm.DamageOp{
		{Cells: localVTermCellsFromString("aa"), WrappedSet: true, Wrapped: true},
		{Cells: localVTermCellsFromString("bb"), WrappedSet: true, Wrapped: true},
		{Cells: localVTermCellsFromString("cc"), WrappedSet: true, Wrapped: true},
	}, true)
	initial := tail.window(0, tail.rowCount()).logicalLineIDs
	if len(initial) != 3 || initial[0] < terminalLiveTailLogicalLineIDBase || initial[0] != initial[1] || initial[0] != initial[2] {
		t.Fatalf("expected resize rows to start with one runtime logical line id, got %#v", initial)
	}

	tail.replaceReclaimedPrefixWithLogicalLineIDs([]localvterm.DamageOp{
		{Cells: localVTermCellsFromString("aa"), WrappedSet: true, Wrapped: true},
		{Cells: localVTermCellsFromString("bb"), WrappedSet: true, Wrapped: true},
	}, []uint64{11, 11}, 7, 10, 11)

	window := tail.window(0, tail.rowCount())
	if got := damageRowsToStrings(window.rows); !reflect.DeepEqual(got, []string{"aa", "bb", "cc"}) {
		t.Fatalf("expected reclaimed prefix plus trimmed resize tail, got %#v", got)
	}
	if got := window.logicalLineIDs; len(got) != 3 || got[0] != 11 || got[1] != 11 || got[2] != initial[0] {
		t.Fatalf("expected trimmed resize row to keep runtime logical line id, got %#v initial=%#v", got, initial)
	}
	records := tail.logicalLineRecords()
	if len(records) != 2 || records[1].id != initial[0] || records[1].origin != terminalLiveTailOriginResize || !records[1].dirty {
		t.Fatalf("expected resize record to keep runtime id after reclaimed trim, got %#v initial=%#v", records, initial)
	}
}

func TestTerminalPrimaryLiveTailAllocatesFreshResizeIDAfterReclaimedTrim(t *testing.T) {
	var tail terminalPrimaryLiveTail
	tail.replaceLiveRows([]localvterm.DamageOp{{
		Cells:      localVTermCellsFromString("old"),
		WrappedSet: true,
		Wrapped:    false,
	}}, false)
	oldIDs := tail.window(0, tail.rowCount()).logicalLineIDs
	if len(oldIDs) != 1 || !terminalRuntimeLogicalLineID(oldIDs[0]) {
		t.Fatalf("expected old live runtime id, got %#v", oldIDs)
	}
	tail.reset()
	tail.segments = []terminalLiveTailSegment{{
		origin:    terminalLiveTailOriginResize,
		sealState: terminalLiveTailOpen,
		rows: []localvterm.DamageOp{
			{Cells: localVTermCellsFromString("aa"), WrappedSet: true, Wrapped: true},
			{Cells: localVTermCellsFromString("bb"), WrappedSet: true, Wrapped: true},
		},
		logicalLineIDs: []uint64{0, 0},
		wrapPending:    true,
	}}
	tail.wrapPending = true

	tail.replaceReclaimedPrefixWithLogicalLineIDs([]localvterm.DamageOp{{
		Cells:      localVTermCellsFromString("aa"),
		WrappedSet: true,
		Wrapped:    true,
	}}, []uint64{11}, 7, 10, 10)

	window := tail.window(0, tail.rowCount())
	if got := damageRowsToStrings(window.rows); !reflect.DeepEqual(got, []string{"aa", "bb"}) {
		t.Fatalf("expected reclaimed prefix plus remaining resize row, got %#v", got)
	}
	if got := window.logicalLineIDs; len(got) != 2 || got[0] != 11 || got[1] <= oldIDs[0] {
		t.Fatalf("expected remaining resize row to get fresh runtime id, got %#v old=%#v", got, oldIDs)
	}
}
