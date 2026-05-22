package termx

import "github.com/lozzow/termx/termx-vterm/vterm"

type terminalLiveTailOrigin string

const (
	terminalLiveTailOriginLive      terminalLiveTailOrigin = "live"
	terminalLiveTailOriginReclaimed terminalLiveTailOrigin = "reclaimed"
	terminalLiveTailOriginResize    terminalLiveTailOrigin = "resize"
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
	generation  uint64
	firstRowID  uint64
	lastRowID   uint64
}

type terminalPrimaryLiveTail struct {
	segments    []terminalLiveTailSegment
	wrapPending bool
}

type terminalLiveTailWindow struct {
	rows         []vterm.DamageOp
	ownership    []string
	committed    int
	generation   uint64
	firstRowID   uint64
	lastRowID    uint64
	hasCommitted bool
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
			generation:  segment.generation,
			firstRowID:  segment.firstRowID,
			lastRowID:   segment.lastRowID,
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

func (tail terminalPrimaryLiveTail) reclaimedRowCount() int {
	total := 0
	for _, segment := range tail.segments {
		if segment.origin != terminalLiveTailOriginReclaimed {
			continue
		}
		total += len(segment.rows)
	}
	return total
}

func (tail terminalPrimaryLiveTail) resizeRowCount() int {
	total := 0
	for _, segment := range tail.segments {
		if segment.origin != terminalLiveTailOriginResize {
			continue
		}
		total += len(segment.rows)
	}
	return total
}

func (tail terminalPrimaryLiveTail) reclaimedCommittedRowCount() int {
	total := 0
	for _, segment := range tail.segments {
		if segment.origin != terminalLiveTailOriginReclaimed {
			continue
		}
		if segment.lastRowID >= segment.firstRowID {
			total += int(segment.lastRowID-segment.firstRowID) + 1
			continue
		}
		total += len(segment.rows)
	}
	return total
}

func (tail terminalPrimaryLiveTail) earliestReclaimedRowID() (uint64, bool) {
	var earliest uint64
	found := false
	for _, segment := range tail.segments {
		if segment.origin != terminalLiveTailOriginReclaimed {
			continue
		}
		if segment.firstRowID == 0 && segment.lastRowID == 0 {
			continue
		}
		if !found || segment.firstRowID < earliest {
			earliest = segment.firstRowID
			found = true
		}
	}
	return earliest, found
}

func (tail terminalPrimaryLiveTail) liveRows() []vterm.DamageOp {
	if len(tail.segments) == 0 {
		return nil
	}
	out := make([]vterm.DamageOp, 0, tail.rowCount())
	for _, segment := range tail.segments {
		if segment.origin == terminalLiveTailOriginReclaimed {
			continue
		}
		out = append(out, cloneGridDamageOps(segment.rows)...)
	}
	return out
}

func (tail terminalPrimaryLiveTail) openLiveRowsForResizePrefix() []vterm.DamageOp {
	if len(tail.segments) == 0 {
		return nil
	}
	out := make([]vterm.DamageOp, 0, tail.rowCount())
	for _, segment := range tail.segments {
		if segment.origin != terminalLiveTailOriginLive || segment.sealState != terminalLiveTailOpen {
			continue
		}
		out = append(out, cloneGridDamageOps(segment.rows)...)
	}
	return out
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

func (tail *terminalPrimaryLiveTail) replaceResizeRows(rows []vterm.DamageOp, wrapPending bool) {
	tail.replaceRows(rows, terminalLiveTailOriginResize, wrapPending)
}

func (tail *terminalPrimaryLiveTail) replaceLiveRows(rows []vterm.DamageOp, wrapPending bool) {
	if tail == nil {
		return
	}
	tail.wrapPending = wrapPending
	next := make([]terminalLiveTailSegment, 0, len(tail.segments)+1)
	for _, segment := range tail.segments {
		if segment.origin != terminalLiveTailOriginReclaimed {
			continue
		}
		next = append(next, terminalLiveTailSegment{
			origin:      segment.origin,
			sealState:   segment.sealState,
			rows:        cloneGridDamageOps(segment.rows),
			wrapPending: segment.wrapPending,
			generation:  segment.generation,
			firstRowID:  segment.firstRowID,
			lastRowID:   segment.lastRowID,
		})
	}
	if len(rows) > 0 {
		next = append(next, terminalLiveTailSegment{
			origin:      terminalLiveTailOriginLive,
			sealState:   terminalLiveTailSealStateForRows(rows, wrapPending),
			rows:        cloneGridDamageOps(rows),
			wrapPending: wrapPending,
		})
	}
	tail.segments = next
}

func (tail *terminalPrimaryLiveTail) replaceReclaimedPrefix(rows []vterm.DamageOp, generation uint64, firstRowID uint64, lastRowID uint64) {
	if tail == nil {
		return
	}
	liveSegments := make([]terminalLiveTailSegment, 0, len(tail.segments)+1)
	for _, segment := range tail.segments {
		if segment.origin == terminalLiveTailOriginReclaimed {
			continue
		}
		if segment.origin == terminalLiveTailOriginResize {
			resizeRows := trimLiveTailResizeRowsCoveredByReclaimedPrefix(segment.rows, rows)
			if len(resizeRows) == 0 {
				continue
			}
			liveSegments = append(liveSegments, terminalLiveTailSegment{
				origin:      segment.origin,
				sealState:   terminalLiveTailSealStateForRows(resizeRows, segment.wrapPending),
				rows:        resizeRows,
				wrapPending: segment.wrapPending,
				generation:  segment.generation,
				firstRowID:  segment.firstRowID,
				lastRowID:   segment.lastRowID,
			})
			continue
		}
		liveSegments = append(liveSegments, terminalLiveTailSegment{
			origin:      segment.origin,
			sealState:   segment.sealState,
			rows:        cloneGridDamageOps(segment.rows),
			wrapPending: segment.wrapPending,
			generation:  segment.generation,
			firstRowID:  segment.firstRowID,
			lastRowID:   segment.lastRowID,
		})
	}
	if len(rows) == 0 {
		tail.segments = liveSegments
		return
	}
	reclaimed := terminalLiveTailSegment{
		origin:     terminalLiveTailOriginReclaimed,
		sealState:  terminalLiveTailSealed,
		rows:       cloneGridDamageOps(rows),
		generation: generation,
		firstRowID: firstRowID,
		lastRowID:  lastRowID,
	}
	tail.segments = append([]terminalLiveTailSegment{reclaimed}, liveSegments...)
}

func trimLiveTailResizeRowsCoveredByReclaimedPrefix(resizeRows []vterm.DamageOp, reclaimedRows []vterm.DamageOp) []vterm.DamageOp {
	if len(resizeRows) == 0 {
		return nil
	}
	if len(reclaimedRows) == 0 {
		return cloneGridDamageOps(resizeRows)
	}
	maxOverlap := minInt(len(resizeRows), len(reclaimedRows))
	for overlap := maxOverlap; overlap > 0; overlap-- {
		if terminalDamageRowsEqual(reclaimedRows[len(reclaimedRows)-overlap:], resizeRows[:overlap]) {
			return cloneGridDamageOps(resizeRows[overlap:])
		}
	}
	if label, ok := terminalDamageRowsLastLabel(reclaimedRows); ok {
		if prefix := terminalDamageRowsPrefixThroughLabel(resizeRows, label); prefix > 0 {
			return cloneGridDamageOps(resizeRows[prefix:])
		}
	}
	return cloneGridDamageOps(resizeRows)
}

func terminalDamageRowsLastLabel(rows []vterm.DamageOp) (string, bool) {
	for i := len(rows) - 1; i >= 0; i-- {
		if label, ok := terminalProjectionRowLabel(damageOpCells(rows[i])); ok {
			return label, true
		}
	}
	return "", false
}

func terminalDamageRowsPrefixThroughLabel(rows []vterm.DamageOp, label string) int {
	if label == "" {
		return 0
	}
	lastMatch := -1
	for i, row := range rows {
		got, ok := terminalProjectionRowLabel(damageOpCells(row))
		if ok && got == label {
			lastMatch = i
		}
		if lastMatch >= 0 && i > lastMatch {
			got, ok := terminalProjectionRowLabel(damageOpCells(row))
			if ok && got != label {
				break
			}
		}
	}
	if lastMatch < 0 {
		return 0
	}
	end := lastMatch + 1
	for end < len(rows) {
		if _, ok := terminalProjectionRowLabel(damageOpCells(rows[end])); ok {
			break
		}
		end++
	}
	return end
}

func (tail terminalPrimaryLiveTail) window(start int, end int) terminalLiveTailWindow {
	var out terminalLiveTailWindow
	if start < 0 {
		start = 0
	}
	if end > tail.rowCount() {
		end = tail.rowCount()
	}
	if end <= start {
		return out
	}
	cursor := 0
	for _, segment := range tail.segments {
		segmentStart := cursor
		segmentEnd := cursor + len(segment.rows)
		cursor = segmentEnd
		if end <= segmentStart || start >= segmentEnd {
			continue
		}
		localStart := maxInt(start, segmentStart) - segmentStart
		localEnd := minInt(end, segmentEnd) - segmentStart
		out.rows = append(out.rows, cloneGridDamageOps(segment.rows[localStart:localEnd])...)
		ownership := RowOwnershipLiveTailLive
		if segment.origin == terminalLiveTailOriginReclaimed {
			ownership = RowOwnershipLiveTailReclaimed
		}
		out.ownership = appendRepeatedString(out.ownership, ownership, localEnd-localStart)
		if segment.origin != terminalLiveTailOriginReclaimed {
			continue
		}
		out.hasCommitted = true
		if out.generation == 0 {
			out.generation = segment.generation
		}
		committedRows := localEnd - localStart
		out.committed += committedRows
		first, last := terminalLiveTailSegmentRowIDWindow(segment, localStart, localEnd)
		if out.firstRowID == 0 && out.lastRowID == 0 {
			out.firstRowID = first
			out.lastRowID = last
			continue
		}
		if first < out.firstRowID {
			out.firstRowID = first
		}
		if last > out.lastRowID {
			out.lastRowID = last
		}
	}
	return out
}

func terminalLiveTailSegmentRowIDWindow(segment terminalLiveTailSegment, localStart int, localEnd int) (uint64, uint64) {
	if segment.lastRowID >= segment.firstRowID && int(segment.lastRowID-segment.firstRowID)+1 == len(segment.rows) {
		first := segment.firstRowID + uint64(localStart)
		last := segment.firstRowID + uint64(localEnd-1)
		return first, last
	}
	return segment.firstRowID, segment.lastRowID
}

func (tail *terminalPrimaryLiveTail) setWrapPending(wrapPending bool) {
	if tail == nil {
		return
	}
	tail.wrapPending = wrapPending
	for i := len(tail.segments) - 1; i >= 0; i-- {
		if tail.segments[i].origin == terminalLiveTailOriginReclaimed {
			continue
		}
		tail.segments[i].wrapPending = wrapPending
		tail.segments[i].sealState = terminalLiveTailSealStateForRows(tail.segments[i].rows, wrapPending)
		return
	}
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
