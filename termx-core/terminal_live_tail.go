package termx

import "github.com/lozzow/termx/termx-vterm/vterm"

type terminalLiveTailOrigin string

const (
	terminalLiveTailOriginLive      terminalLiveTailOrigin = "live"
	terminalLiveTailOriginReclaimed terminalLiveTailOrigin = "reclaimed"
)

type terminalLiveTailSealState string

const (
	terminalLiveTailOpen   terminalLiveTailSealState = "open"
	terminalLiveTailSealed terminalLiveTailSealState = "sealed"
)

type terminalLiveTailSegment struct {
	origin      terminalLiveTailOrigin
	sealState   terminalLiveTailSealState
	rows        []vterm.DamageOp
	wrapPending bool
}

type terminalPrimaryLiveTail struct {
	segments    []terminalLiveTailSegment
	wrapPending bool
}

func newTerminalPrimaryLiveTail(rows []vterm.DamageOp, wrapPending bool) terminalPrimaryLiveTail {
	var tail terminalPrimaryLiveTail
	tail.replaceRows(rows, terminalLiveTailOriginLive, wrapPending)
	return tail
}

func (tail terminalPrimaryLiveTail) clone() terminalPrimaryLiveTail {
	if len(tail.segments) == 0 {
		return terminalPrimaryLiveTail{wrapPending: tail.wrapPending}
	}
	out := terminalPrimaryLiveTail{
		segments:    make([]terminalLiveTailSegment, 0, len(tail.segments)),
		wrapPending: tail.wrapPending,
	}
	for _, segment := range tail.segments {
		out.segments = append(out.segments, terminalLiveTailSegment{
			origin:      segment.origin,
			sealState:   segment.sealState,
			rows:        cloneGridDamageOps(segment.rows),
			wrapPending: segment.wrapPending,
		})
	}
	return out
}

func (tail terminalPrimaryLiveTail) rows() []vterm.DamageOp {
	count := tail.rowCount()
	if count == 0 {
		return nil
	}
	out := make([]vterm.DamageOp, 0, count)
	for _, segment := range tail.segments {
		out = append(out, cloneGridDamageOps(segment.rows)...)
	}
	return out
}

func (tail terminalPrimaryLiveTail) rowCount() int {
	total := 0
	for _, segment := range tail.segments {
		total += len(segment.rows)
	}
	return total
}

func (tail terminalPrimaryLiveTail) hasState() bool {
	return tail.rowCount() > 0 || tail.wrapPending
}

func (tail *terminalPrimaryLiveTail) reset() {
	if tail == nil {
		return
	}
	tail.segments = nil
	tail.wrapPending = false
}

func (tail *terminalPrimaryLiveTail) replaceRows(rows []vterm.DamageOp, origin terminalLiveTailOrigin, wrapPending bool) {
	if tail == nil {
		return
	}
	tail.wrapPending = wrapPending
	if len(rows) == 0 {
		tail.segments = nil
		return
	}
	tail.segments = []terminalLiveTailSegment{{
		origin:      origin,
		sealState:   terminalLiveTailSealStateForRows(rows, wrapPending),
		rows:        cloneGridDamageOps(rows),
		wrapPending: wrapPending,
	}}
}

func (tail *terminalPrimaryLiveTail) setWrapPending(wrapPending bool) {
	if tail == nil {
		return
	}
	tail.wrapPending = wrapPending
	if len(tail.segments) == 0 {
		return
	}
	tail.segments[len(tail.segments)-1].wrapPending = wrapPending
	tail.segments[len(tail.segments)-1].sealState = terminalLiveTailSealStateForRows(tail.segments[len(tail.segments)-1].rows, wrapPending)
}

func terminalLiveTailSealStateForRows(rows []vterm.DamageOp, wrapPending bool) terminalLiveTailSealState {
	if wrapPending {
		return terminalLiveTailOpen
	}
	if len(rows) == 0 {
		return terminalLiveTailOpen
	}
	last := rows[len(rows)-1]
	if last.WrappedSet && last.Wrapped {
		return terminalLiveTailOpen
	}
	return terminalLiveTailSealed
}
