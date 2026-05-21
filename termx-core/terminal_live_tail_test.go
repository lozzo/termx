package termx

import (
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

	tail.setWrapPending(true)
	if !tail.wrapPending || !tail.segments[0].wrapPending {
		t.Fatal("expected wrap pending to be stored on tail and segment")
	}
	if got := tail.segments[0].sealState; got != terminalLiveTailOpen {
		t.Fatalf("expected wrap pending to reopen segment mutability, got %q", got)
	}

	tail.reset()
	if tail.hasState() {
		t.Fatalf("expected reset live tail to have no state, got %#v", tail)
	}
}
