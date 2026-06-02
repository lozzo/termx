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
	if got := window.logicalLineIDs; !reflect.DeepEqual(got, []uint64{41, 41, 0}) {
		t.Fatalf("expected reclaimed rows to keep persisted logical line id and live rows to stay anonymous, got %#v", got)
	}

	records := tail.logicalLineRecords()
	want := []terminalLiveTailLogicalLineRecord{
		{id: 41, startRow: 0, endRow: 1, sealState: terminalLiveTailSealed, origin: terminalLiveTailOriginReclaimed},
		{id: 0, startRow: 2, endRow: 2, sealState: terminalLiveTailOpen, origin: terminalLiveTailOriginLive},
	}
	if !reflect.DeepEqual(records, want) {
		t.Fatalf("unexpected live tail logical line records got %#v want %#v", records, want)
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
	if len(records) != 1 || records[0].id != 99 || records[0].origin != terminalLiveTailOriginReclaimed || records[0].sealState != terminalLiveTailSealed {
		t.Fatalf("expected explicit reclaimed id in record view, got %#v", records)
	}
}
