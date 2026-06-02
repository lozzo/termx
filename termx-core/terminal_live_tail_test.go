package termx

import (
	"reflect"
	"testing"

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
	tail.replaceReclaimedPrefix([]localvterm.DamageOp{
		{Cells: localVTermCellsFromString("aa"), WrappedSet: true, Wrapped: true},
		{Cells: localVTermCellsFromString("bb"), WrappedSet: true, Wrapped: false},
	}, 7, 40, 41)
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

func TestTerminalGridLineRecordMetasOnlyWriteReclaimedRowIDs(t *testing.T) {
	records := []terminalLiveTailLogicalLineRecord{
		{
			id:         41,
			startRow:   0,
			endRow:     0,
			sealState:  terminalLiveTailSealed,
			origin:     terminalLiveTailOriginReclaimed,
			residency:  terminalLogicalLineResidencyLiveTail,
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
