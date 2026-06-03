package termx

import (
	"time"

	"github.com/lozzow/termx/termx-vterm/vterm"
)

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

func terminalLiveTailSealStateKnown(sealState terminalLiveTailSealState) bool {
	switch sealState {
	case terminalLiveTailOpen, terminalLiveTailSealed:
		return true
	default:
		return false
	}
}

const terminalLiveTailLogicalLineIDBase uint64 = 1 << 63

type terminalLiveTailSegment struct {
	origin            terminalLiveTailOrigin
	sealState         terminalLiveTailSealState
	rows              []vterm.DamageOp
	logicalLineIDs    []uint64
	wrapPending       bool
	generation        uint64
	firstRowID        uint64
	lastRowID         uint64
	replaceFirstRowID uint64
	replaceLastRowID  uint64
}

type terminalPrimaryLiveTail struct {
	segments                 []terminalLiveTailSegment
	wrapPending              bool
	nextRuntimeLogicalLineID uint64
}

type terminalLiveTailWindow struct {
	rows               []vterm.DamageOp
	ownership          []string
	logicalLineIDs     []uint64
	lineTimestampStart []time.Time
	lineTimestampEnd   []time.Time
	rowIDRanges        []terminalGridRowIDRange
	committed          int
	generation         uint64
	firstRowID         uint64
	lastRowID          uint64
	hasCommitted       bool
}

type terminalLiveTailLogicalLineRecord struct {
	id             uint64
	startRow       int
	endRow         int
	sealState      terminalLiveTailSealState
	origin         terminalLiveTailOrigin
	residency      terminalLogicalLineResidency
	rowKind        string
	timestampStart time.Time
	timestampEnd   time.Time
	dirty          bool
	generation     uint64
	rowIDKnown     bool
	firstRowID     uint64
	lastRowID      uint64
}

type terminalLiveTailLogicalLinePayload struct {
	rows []vterm.DamageOp
}

func terminalLiveTailLogicalLineRecordPayload(record terminalLiveTailLogicalLineRecord, rows []vterm.DamageOp) (terminalLiveTailLogicalLinePayload, bool) {
	if record.startRow < 0 || record.endRow < record.startRow || record.endRow >= len(rows) {
		return terminalLiveTailLogicalLinePayload{}, false
	}
	return terminalLiveTailLogicalLinePayload{rows: rows[record.startRow : record.endRow+1]}, true
}

func (payload terminalLiveTailLogicalLinePayload) rowKind() string {
	return terminalLogicalLineRowKindFromDamageRows(payload.rows)
}

func (payload terminalLiveTailLogicalLinePayload) timestampStart() time.Time {
	return terminalLogicalLineTimestampStartFromDamageRows(payload.rows)
}

func (payload terminalLiveTailLogicalLinePayload) timestampEnd() time.Time {
	return terminalLogicalLineTimestampEndFromDamageRows(payload.rows)
}

func (payload terminalLiveTailLogicalLinePayload) cellRows() [][]vterm.Cell {
	if len(payload.rows) == 0 {
		return nil
	}
	out := make([][]vterm.Cell, 0, len(payload.rows))
	for _, row := range payload.rows {
		out = append(out, damageOpCells(row))
	}
	return out
}

func (payload terminalLiveTailLogicalLinePayload) wrappedRows() []bool {
	if len(payload.rows) == 0 {
		return nil
	}
	out := make([]bool, 0, len(payload.rows))
	for _, row := range payload.rows {
		out = append(out, row.WrappedSet && row.Wrapped)
	}
	return out
}

type terminalLiveTailRowsWithLogicalLineIDs struct {
	rows           []vterm.DamageOp
	logicalLineIDs []uint64
}

func newTerminalPrimaryLiveTail(rows []vterm.DamageOp, wrapPending bool) terminalPrimaryLiveTail {
	var tail terminalPrimaryLiveTail
	tail.replaceRows(rows, terminalLiveTailOriginLive, wrapPending)
	return tail
}

func (tail terminalPrimaryLiveTail) clone() terminalPrimaryLiveTail {
	if len(tail.segments) == 0 {
		return terminalPrimaryLiveTail{wrapPending: tail.wrapPending, nextRuntimeLogicalLineID: tail.nextRuntimeLogicalLineID}
	}
	out := terminalPrimaryLiveTail{
		segments:                 make([]terminalLiveTailSegment, 0, len(tail.segments)),
		wrapPending:              tail.wrapPending,
		nextRuntimeLogicalLineID: tail.nextRuntimeLogicalLineID,
	}
	for _, segment := range tail.segments {
		out.segments = append(out.segments, terminalLiveTailSegment{
			origin:            segment.origin,
			sealState:         segment.sealState,
			rows:              cloneGridDamageOps(segment.rows),
			logicalLineIDs:    cloneUint64Slice(segment.logicalLineIDs),
			wrapPending:       segment.wrapPending,
			generation:        segment.generation,
			firstRowID:        segment.firstRowID,
			lastRowID:         segment.lastRowID,
			replaceFirstRowID: segment.replaceFirstRowID,
			replaceLastRowID:  segment.replaceLastRowID,
		})
	}
	return out
}

func (tail terminalPrimaryLiveTail) rows() []vterm.DamageOp {
	return tail.rowsWithLogicalLineIDs().rows
}

func (tail terminalPrimaryLiveTail) rowsWithLogicalLineIDs() terminalLiveTailRowsWithLogicalLineIDs {
	count := tail.rowCount()
	if count == 0 {
		return terminalLiveTailRowsWithLogicalLineIDs{}
	}
	out := make([]vterm.DamageOp, 0, count)
	lineIDs := make([]uint64, 0, count)
	for _, segment := range tail.segments {
		out = append(out, cloneGridDamageOps(segment.rows)...)
		lineIDs = append(lineIDs, terminalLiveTailSegmentLogicalLineIDs(segment.logicalLineIDs, segment.rows, segment.origin, segment.firstRowID, segment.lastRowID)...)
	}
	return terminalLiveTailRowsWithLogicalLineIDs{rows: out, logicalLineIDs: lineIDs}
}

func (tail terminalPrimaryLiveTail) rowCount() int {
	total := 0
	for _, segment := range tail.segments {
		total += len(segment.rows)
	}
	return total
}

func (tail terminalPrimaryLiveTail) nonReclaimedRowCount() int {
	total := 0
	for _, segment := range tail.segments {
		if segment.origin == terminalLiveTailOriginReclaimed {
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

func (tail terminalPrimaryLiveTail) authoritativeReclaimedCommittedRowCount() int {
	total := 0
	for _, segment := range tail.segments {
		if !terminalLiveTailReclaimedSegmentAuthoritative(segment) {
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

func (tail terminalPrimaryLiveTail) earliestAuthoritativeReclaimedRowID() (uint64, bool) {
	var earliest uint64
	found := false
	for _, segment := range tail.segments {
		if !terminalLiveTailReclaimedSegmentAuthoritative(segment) {
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

func (tail terminalPrimaryLiveTail) authoritativeReclaimedTailRowCount(persistedBaseRowID uint64, persistedRows int) int {
	if persistedRows <= 0 {
		return 0
	}
	expectedLast := persistedBaseRowID + uint64(persistedRows-1)
	for _, segment := range tail.segments {
		if !terminalLiveTailReclaimedSegmentAuthoritative(segment) {
			continue
		}
		if segment.lastRowID != expectedLast {
			continue
		}
		count := len(segment.rows)
		if segment.firstRowID+uint64(count)-1 != segment.lastRowID {
			return 0
		}
		return count
	}
	return 0
}

func (tail terminalPrimaryLiveTail) dirtyLiveRowsReplaceAuthoritativeReclaimedTail(persistedBaseRowID uint64, persistedRows int) bool {
	reclaimedCount := tail.authoritativeReclaimedTailRowCount(persistedBaseRowID, persistedRows)
	if reclaimedCount <= 0 {
		return false
	}
	expectedFirst := persistedBaseRowID + uint64(persistedRows-reclaimedCount)
	expectedLast := persistedBaseRowID + uint64(persistedRows-1)
	for _, segment := range tail.segments {
		if segment.origin != terminalLiveTailOriginLive && segment.origin != terminalLiveTailOriginResize {
			continue
		}
		if segment.replaceFirstRowID == expectedFirst && segment.replaceLastRowID == expectedLast {
			return true
		}
	}
	return false
}

func (tail terminalPrimaryLiveTail) liveRows() []vterm.DamageOp {
	return tail.liveRowsWithLogicalLineIDs().rows
}

func (tail terminalPrimaryLiveTail) liveRowsWithLogicalLineIDs() terminalLiveTailRowsWithLogicalLineIDs {
	if len(tail.segments) == 0 {
		return terminalLiveTailRowsWithLogicalLineIDs{}
	}
	out := make([]vterm.DamageOp, 0, tail.rowCount())
	lineIDs := make([]uint64, 0, tail.rowCount())
	for _, segment := range tail.segments {
		if segment.origin == terminalLiveTailOriginReclaimed {
			continue
		}
		out = append(out, cloneGridDamageOps(segment.rows)...)
		lineIDs = append(lineIDs, terminalLiveTailSegmentLogicalLineIDs(segment.logicalLineIDs, segment.rows, segment.origin, segment.firstRowID, segment.lastRowID)...)
	}
	return terminalLiveTailRowsWithLogicalLineIDs{rows: out, logicalLineIDs: lineIDs}
}

func (tail terminalPrimaryLiveTail) nonReclaimedRowsForResizePrefix() []vterm.DamageOp {
	return tail.nonReclaimedRowsForResizePrefixWithLogicalLineIDs().rows
}

func (tail terminalPrimaryLiveTail) nonReclaimedRowsForResizePrefixWithLogicalLineIDs() terminalLiveTailRowsWithLogicalLineIDs {
	return tail.liveRowsWithLogicalLineIDs()
}

func (tail terminalPrimaryLiveTail) nonReclaimedRowsWithLogicalLineIDs() terminalLiveTailRowsWithLogicalLineIDs {
	return tail.liveRowsWithLogicalLineIDs()
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

func (tail *terminalPrimaryLiveTail) advanceRuntimeLogicalLineID(id uint64) {
	if tail == nil || id <= tail.nextRuntimeLogicalLineID || !terminalRuntimeLogicalLineID(id) {
		return
	}
	tail.nextRuntimeLogicalLineID = id
}

func (tail *terminalPrimaryLiveTail) reassignConflictingRuntimeLogicalLineIDs(maxRuntimeID uint64) {
	if tail == nil || !terminalRuntimeLogicalLineID(maxRuntimeID) {
		return
	}
	tail.advanceRuntimeLogicalLineID(maxRuntimeID)
	replacements := make(map[uint64]uint64)
	for segmentIndex := range tail.segments {
		segment := &tail.segments[segmentIndex]
		if segment.origin == terminalLiveTailOriginReclaimed || len(segment.logicalLineIDs) == 0 {
			continue
		}
		for i, id := range segment.logicalLineIDs {
			if id == 0 || id > maxRuntimeID || !terminalRuntimeLogicalLineID(id) {
				continue
			}
			replacement := replacements[id]
			if replacement == 0 {
				tail.nextRuntimeLogicalLineID++
				replacement = tail.nextRuntimeLogicalLineID
				replacements[id] = replacement
			}
			segment.logicalLineIDs[i] = replacement
		}
	}
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
	logicalLineIDs := terminalLiveTailSegmentFallbackLogicalLineIDs(origin, rows, 0, 0)
	if origin != terminalLiveTailOriginReclaimed {
		logicalLineIDs = tail.liveLogicalLineIDsForRows(nil, rows)
	}
	tail.segments = []terminalLiveTailSegment{{
		origin:         origin,
		sealState:      terminalLiveTailSealStateForRows(rows, wrapPending),
		rows:           cloneGridDamageOps(rows),
		logicalLineIDs: logicalLineIDs,
		wrapPending:    wrapPending,
	}}
}

func (tail *terminalPrimaryLiveTail) replaceResizeRows(rows []vterm.DamageOp, wrapPending bool) {
	tail.replaceResizeRowsWithLogicalLineIDs(rows, nil, wrapPending)
}

func (tail *terminalPrimaryLiveTail) replaceResizeRowsWithLogicalLineIDs(rows []vterm.DamageOp, logicalLineIDs []uint64, wrapPending bool) {
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
			origin:            segment.origin,
			sealState:         segment.sealState,
			rows:              cloneGridDamageOps(segment.rows),
			logicalLineIDs:    cloneUint64Slice(segment.logicalLineIDs),
			wrapPending:       segment.wrapPending,
			generation:        segment.generation,
			firstRowID:        segment.firstRowID,
			lastRowID:         segment.lastRowID,
			replaceFirstRowID: segment.replaceFirstRowID,
			replaceLastRowID:  segment.replaceLastRowID,
		})
	}
	if len(rows) > 0 {
		next = append(next, terminalLiveTailSegment{
			origin:         terminalLiveTailOriginResize,
			sealState:      terminalLiveTailSealStateForRows(rows, wrapPending),
			rows:           cloneGridDamageOps(rows),
			logicalLineIDs: tail.liveLogicalLineIDsForRows(logicalLineIDs, rows),
			wrapPending:    wrapPending,
		})
	}
	tail.segments = next
}

func (tail *terminalPrimaryLiveTail) replaceLiveRows(rows []vterm.DamageOp, wrapPending bool) {
	tail.replaceLiveRowsWithLogicalLineIDs(rows, nil, wrapPending)
}

func (tail *terminalPrimaryLiveTail) replaceReclaimedTailWithLiveRows(rows []vterm.DamageOp, wrapPending bool) {
	tail.replaceReclaimedTailWithLiveRowsAndLogicalLineIDs(rows, nil, wrapPending)
}

func (tail *terminalPrimaryLiveTail) replaceReclaimedTailWithLiveRowsAndLogicalLineIDs(rows []vterm.DamageOp, logicalLineIDs []uint64, wrapPending bool) {
	firstRowID, lastRowID := tail.reclaimedTailRowIDsForReplacement(len(rows))
	tail.replaceLiveRowsWithLogicalLineIDs(rows, logicalLineIDs, wrapPending)
	if len(tail.segments) == 0 || firstRowID == 0 && lastRowID == 0 {
		return
	}
	segment := &tail.segments[len(tail.segments)-1]
	if segment.origin != terminalLiveTailOriginLive && segment.origin != terminalLiveTailOriginResize {
		return
	}
	segment.replaceFirstRowID = firstRowID
	segment.replaceLastRowID = lastRowID
}

func (tail *terminalPrimaryLiveTail) replaceLiveRowsWithLogicalLineIDs(rows []vterm.DamageOp, logicalLineIDs []uint64, wrapPending bool) {
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
			origin:            segment.origin,
			sealState:         segment.sealState,
			rows:              cloneGridDamageOps(segment.rows),
			logicalLineIDs:    cloneUint64Slice(segment.logicalLineIDs),
			wrapPending:       segment.wrapPending,
			generation:        segment.generation,
			firstRowID:        segment.firstRowID,
			lastRowID:         segment.lastRowID,
			replaceFirstRowID: segment.replaceFirstRowID,
			replaceLastRowID:  segment.replaceLastRowID,
		})
	}
	if len(rows) > 0 {
		next = append(next, terminalLiveTailSegment{
			origin:         terminalLiveTailOriginLive,
			sealState:      terminalLiveTailSealStateForRows(rows, wrapPending),
			rows:           cloneGridDamageOps(rows),
			logicalLineIDs: tail.liveLogicalLineIDsForRows(logicalLineIDs, rows),
			wrapPending:    wrapPending,
		})
	}
	tail.segments = next
}

func (tail terminalPrimaryLiveTail) reclaimedTailRowIDsForReplacement(rowCount int) (uint64, uint64) {
	if rowCount <= 0 {
		return 0, 0
	}
	for _, segment := range tail.segments {
		if !terminalLiveTailReclaimedSegmentAuthoritative(segment) {
			continue
		}
		if rowCount >= len(segment.rows) {
			return segment.firstRowID, segment.lastRowID
		}
	}
	return 0, 0
}

func (tail *terminalPrimaryLiveTail) replaceReclaimedPrefix(rows []vterm.DamageOp, generation uint64, firstRowID uint64, lastRowID uint64) {
	tail.replaceReclaimedPrefixWithLogicalLineIDs(rows, nil, generation, firstRowID, lastRowID)
}

func (tail *terminalPrimaryLiveTail) replaceReclaimedPrefixWithLogicalLineIDs(rows []vterm.DamageOp, logicalLineIDs []uint64, generation uint64, firstRowID uint64, lastRowID uint64) {
	if tail == nil {
		return
	}
	liveSegments := make([]terminalLiveTailSegment, 0, len(tail.segments)+1)
	for _, segment := range tail.segments {
		if segment.origin == terminalLiveTailOriginReclaimed {
			continue
		}
		if segment.origin == terminalLiveTailOriginResize {
			resizeTail := trimLiveTailResizeRowsCoveredByReclaimedPrefix(segment.rows, segment.logicalLineIDs, rows)
			if len(resizeTail.rows) == 0 {
				continue
			}
			liveSegments = append(liveSegments, terminalLiveTailSegment{
				origin:         segment.origin,
				sealState:      terminalLiveTailSealStateForRows(resizeTail.rows, segment.wrapPending),
				rows:           resizeTail.rows,
				logicalLineIDs: tail.liveLogicalLineIDsForRows(resizeTail.logicalLineIDs, resizeTail.rows),
				wrapPending:    segment.wrapPending,
				generation:     segment.generation,
				firstRowID:     segment.firstRowID,
				lastRowID:      segment.lastRowID,
			})
			continue
		}
		liveSegments = append(liveSegments, terminalLiveTailSegment{
			origin:         segment.origin,
			sealState:      segment.sealState,
			rows:           cloneGridDamageOps(segment.rows),
			logicalLineIDs: cloneUint64Slice(segment.logicalLineIDs),
			wrapPending:    segment.wrapPending,
			generation:     segment.generation,
			firstRowID:     segment.firstRowID,
			lastRowID:      segment.lastRowID,
		})
	}
	if len(rows) == 0 {
		tail.segments = liveSegments
		return
	}
	reclaimed := terminalLiveTailSegment{
		origin:         terminalLiveTailOriginReclaimed,
		sealState:      terminalLiveTailSealed,
		rows:           cloneGridDamageOps(rows),
		logicalLineIDs: terminalLiveTailSegmentLogicalLineIDs(logicalLineIDs, rows, terminalLiveTailOriginReclaimed, firstRowID, lastRowID),
		generation:     generation,
		firstRowID:     firstRowID,
		lastRowID:      lastRowID,
	}
	tail.segments = append([]terminalLiveTailSegment{reclaimed}, liveSegments...)
}

func trimLiveTailResizeRowsCoveredByReclaimedPrefix(resizeRows []vterm.DamageOp, logicalLineIDs []uint64, reclaimedRows []vterm.DamageOp) terminalLiveTailRowsWithLogicalLineIDs {
	if len(resizeRows) == 0 {
		return terminalLiveTailRowsWithLogicalLineIDs{}
	}
	lineIDs := alignLiveTailUint64s(logicalLineIDs, len(resizeRows))
	if len(reclaimedRows) == 0 {
		return terminalLiveTailRowsWithLogicalLineIDs{
			rows:           cloneGridDamageOps(resizeRows),
			logicalLineIDs: lineIDs,
		}
	}
	maxOverlap := minInt(len(resizeRows), len(reclaimedRows))
	for overlap := maxOverlap; overlap > 0; overlap-- {
		if terminalDamageRowsEqual(reclaimedRows[len(reclaimedRows)-overlap:], resizeRows[:overlap]) {
			return terminalLiveTailRowsWithLogicalLineIDs{
				rows:           cloneGridDamageOps(resizeRows[overlap:]),
				logicalLineIDs: cloneUint64Slice(lineIDs[overlap:]),
			}
		}
	}
	return terminalLiveTailRowsWithLogicalLineIDs{
		rows:           cloneGridDamageOps(resizeRows),
		logicalLineIDs: lineIDs,
	}
}

func (tail *terminalPrimaryLiveTail) liveLogicalLineIDsForRows(logicalLineIDs []uint64, rows []vterm.DamageOp) []uint64 {
	lineIDs, nextID := terminalLiveTailSegmentLiveLogicalLineIDsFrom(logicalLineIDs, rows, tail.nextRuntimeLogicalLineID)
	if nextID > tail.nextRuntimeLogicalLineID {
		tail.nextRuntimeLogicalLineID = nextID
	}
	return lineIDs
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
		logicalLineIDs := terminalLiveTailSegmentLogicalLineIDsWindow(segment, localStart, localEnd)
		out.logicalLineIDs = append(out.logicalLineIDs, logicalLineIDs...)
		timestampStart, timestampEnd := terminalLiveTailSegmentLogicalLineTimestampWindow(segment, localStart, localEnd, logicalLineIDs)
		out.lineTimestampStart = append(out.lineTimestampStart, timestampStart...)
		out.lineTimestampEnd = append(out.lineTimestampEnd, timestampEnd...)
		ownership := RowOwnershipLiveTailLive
		if segment.origin == terminalLiveTailOriginReclaimed {
			ownership = RowOwnershipLiveTailReclaimed
		}
		out.ownership = appendRepeatedString(out.ownership, ownership, localEnd-localStart)
		if segment.origin != terminalLiveTailOriginReclaimed {
			out.rowIDRanges = append(out.rowIDRanges, make([]terminalGridRowIDRange, localEnd-localStart)...)
			continue
		}
		if !terminalLiveTailReclaimedSegmentAuthoritative(segment) {
			out.rowIDRanges = append(out.rowIDRanges, make([]terminalGridRowIDRange, localEnd-localStart)...)
			continue
		}
		if terminalLiveTailReclaimedSegmentHasRowIDs(segment) {
			for rowID := segment.firstRowID + uint64(localStart); rowID <= segment.firstRowID+uint64(localEnd-1); rowID++ {
				out.rowIDRanges = append(out.rowIDRanges, terminalGridRowIDRange{First: rowID, Last: rowID, Known: true})
			}
		} else {
			out.rowIDRanges = append(out.rowIDRanges, make([]terminalGridRowIDRange, localEnd-localStart)...)
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

func terminalLiveTailSegmentLogicalLineTimestampWindow(segment terminalLiveTailSegment, start int, end int, logicalLineIDs []uint64) ([]time.Time, []time.Time) {
	rowCount := maxInt(0, end-start)
	timestampStart := make([]time.Time, rowCount)
	timestampEnd := make([]time.Time, rowCount)
	if rowCount == 0 {
		return timestampStart, timestampEnd
	}
	fullLogicalLineIDs := terminalLiveTailSegmentLogicalLineIDs(segment.logicalLineIDs, segment.rows, segment.origin, segment.firstRowID, segment.lastRowID)
	if !terminalLiveTailLogicalLineIDsComplete(logicalLineIDs, rowCount) || !terminalLiveTailLogicalLineIDsComplete(fullLogicalLineIDs, len(segment.rows)) {
		for i := 0; i < rowCount; i++ {
			timestampStart[i] = segment.rows[start+i].Timestamp
			timestampEnd[i] = segment.rows[start+i].Timestamp
		}
		return timestampStart, timestampEnd
	}
	for i := 0; i < rowCount; i++ {
		row := start + i
		lineID := fullLogicalLineIDs[row]
		lineStart := row
		for lineStart > 0 && fullLogicalLineIDs[lineStart-1] == lineID {
			lineStart--
		}
		lineEnd := row
		for lineEnd+1 < len(fullLogicalLineIDs) && fullLogicalLineIDs[lineEnd+1] == lineID {
			lineEnd++
		}
		record := terminalLiveTailLogicalLineRecord{startRow: lineStart, endRow: lineEnd}
		if payload, ok := terminalLiveTailLogicalLineRecordPayload(record, segment.rows); ok {
			timestampStart[i] = payload.timestampStart()
			timestampEnd[i] = payload.timestampEnd()
		}
	}
	return timestampStart, timestampEnd
}

func (tail terminalPrimaryLiveTail) logicalLineRecords() []terminalLiveTailLogicalLineRecord {
	if len(tail.segments) == 0 {
		return nil
	}
	records := make([]terminalLiveTailLogicalLineRecord, 0, tail.rowCount())
	cursor := 0
	for _, segment := range tail.segments {
		segmentRecords := terminalLiveTailSegmentLogicalLineRecords(segment, cursor)
		if len(segment.rows) > 0 && len(segmentRecords) == 0 {
			return nil
		}
		records = append(records, segmentRecords...)
		cursor += len(segment.rows)
	}
	return records
}

func terminalLiveTailSegmentLogicalLineRecords(segment terminalLiveTailSegment, baseRow int) []terminalLiveTailLogicalLineRecord {
	if len(segment.rows) == 0 {
		return nil
	}
	records := make([]terminalLiveTailLogicalLineRecord, 0, len(segment.rows))
	logicalLineIDs := terminalLiveTailSegmentLogicalLineIDs(segment.logicalLineIDs, segment.rows, segment.origin, segment.firstRowID, segment.lastRowID)
	if !terminalLiveTailLogicalLineIDsComplete(logicalLineIDs, len(segment.rows)) {
		return nil
	}
	start := 0
	for i := range segment.rows {
		if terminalLiveTailRecordContinues(logicalLineIDs, i) {
			continue
		}
		record := terminalLiveTailLogicalLineRecord{
			id:         uint64At(logicalLineIDs, start),
			startRow:   baseRow + start,
			endRow:     baseRow + i,
			sealState:  terminalLiveTailRecordSealState(segment, i),
			origin:     segment.origin,
			residency:  terminalLogicalLineResidencyLiveTail,
			dirty:      terminalLiveTailRecordDirty(segment),
			generation: segment.generation,
		}
		localRecord := record
		localRecord.startRow = start
		localRecord.endRow = i
		if payload, ok := terminalLiveTailLogicalLineRecordPayload(localRecord, segment.rows); ok {
			record.rowKind = payload.rowKind()
			record.timestampStart = payload.timestampStart()
			record.timestampEnd = payload.timestampEnd()
		}
		if terminalLiveTailReclaimedSegmentHasRowIDs(segment) {
			record.rowIDKnown = true
			record.firstRowID = segment.firstRowID + uint64(start)
			record.lastRowID = segment.firstRowID + uint64(i)
		}
		records = append(records, record)
		start = i + 1
	}
	return records
}

func terminalLiveTailLogicalLineIDsComplete(logicalLineIDs []uint64, rowCount int) bool {
	if rowCount <= 0 || len(logicalLineIDs) != rowCount {
		return false
	}
	for _, id := range logicalLineIDs {
		if id == 0 {
			return false
		}
	}
	return true
}

func terminalLiveTailRecordContinues(logicalLineIDs []uint64, row int) bool {
	if row < 0 || row >= len(logicalLineIDs)-1 {
		return false
	}
	return logicalLineIDs[row] == logicalLineIDs[row+1]
}

func terminalLiveTailRecordDirty(segment terminalLiveTailSegment) bool {
	return terminalLiveTailOriginDirty(segment.origin)
}

func terminalLiveTailRecordSealState(segment terminalLiveTailSegment, row int) terminalLiveTailSealState {
	if segment.origin == terminalLiveTailOriginReclaimed || segment.sealState == terminalLiveTailSealed {
		return segment.sealState
	}
	if row < len(segment.rows)-1 {
		return terminalLiveTailSealed
	}
	return terminalLiveTailOpen
}

func terminalLiveTailSegmentLogicalLineIDsWindow(segment terminalLiveTailSegment, localStart int, localEnd int) []uint64 {
	if localEnd <= localStart {
		return nil
	}
	lineIDs := terminalLiveTailSegmentLogicalLineIDs(segment.logicalLineIDs, segment.rows, segment.origin, segment.firstRowID, segment.lastRowID)
	return cloneUint64Slice(lineIDs[localStart:localEnd])
}

func terminalLiveTailSegmentLogicalLineIDs(lineIDs []uint64, rows []vterm.DamageOp, origin terminalLiveTailOrigin, firstRowID uint64, lastRowID uint64) []uint64 {
	rowCount := len(rows)
	if rowCount <= 0 {
		return nil
	}
	aligned := alignLiveTailUint64s(lineIDs, rowCount)
	if hasNonZeroUint64(aligned) {
		if !terminalLiveTailLogicalLineIDsComplete(aligned, rowCount) || !terminalLiveTailLogicalLineIDsMatchOrigin(aligned, origin) {
			return make([]uint64, rowCount)
		}
		return aligned
	}
	return terminalLiveTailSegmentFallbackLogicalLineIDs(origin, rows, firstRowID, lastRowID)
}

func terminalLiveTailSegmentLiveLogicalLineIDs(lineIDs []uint64, rows []vterm.DamageOp) []uint64 {
	out, _ := terminalLiveTailSegmentLiveLogicalLineIDsFrom(lineIDs, rows, 0)
	return out
}

func terminalLiveTailSegmentLiveLogicalLineIDsFrom(lineIDs []uint64, rows []vterm.DamageOp, nextID uint64) ([]uint64, uint64) {
	rowCount := len(rows)
	if rowCount <= 0 {
		return nil, nextID
	}
	aligned := alignLiveTailUint64s(lineIDs, rowCount)
	if terminalLiveTailLogicalLineIDsComplete(aligned, rowCount) && terminalLiveTailLogicalLineIDsMatchOrigin(aligned, terminalLiveTailOriginLive) {
		return aligned, maxUint64(nextID, maxUint64Slice(aligned))
	}
	for i, id := range aligned {
		if id != 0 && !terminalLiveTailRecordIDMatchesOrigin(id, terminalLiveTailOriginLive) {
			aligned[i] = 0
		}
	}
	nextID = maxUint64(nextID, maxUint64Slice(aligned))
	if nextID < terminalLiveTailLogicalLineIDBase {
		nextID = terminalLiveTailLogicalLineIDBase
	}
	start := 0
	for i, row := range rows {
		if row.WrappedSet && row.Wrapped && i < len(rows)-1 {
			continue
		}
		id := uint64At(aligned, start)
		if id == 0 {
			nextID++
			id = nextID
		}
		for rowIndex := start; rowIndex <= i; rowIndex++ {
			aligned[rowIndex] = id
		}
		start = i + 1
	}
	return aligned, nextID
}

func terminalLiveTailLogicalLineIDsMatchOrigin(lineIDs []uint64, origin terminalLiveTailOrigin) bool {
	for _, id := range lineIDs {
		if !terminalLiveTailRecordIDMatchesOrigin(id, origin) {
			return false
		}
	}
	return true
}

func maxUint64Slice(values []uint64) uint64 {
	var maxValue uint64
	for _, value := range values {
		if value > maxValue {
			maxValue = value
		}
	}
	return maxValue
}

func maxUint64(a, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}

func alignLiveTailUint64s(values []uint64, size int) []uint64 {
	out := make([]uint64, size)
	if size <= 0 || len(values) == 0 {
		return out
	}
	if len(values) > size {
		values = values[:size]
	}
	copy(out, values)
	return out
}

func terminalLiveTailSegmentFallbackLogicalLineIDs(origin terminalLiveTailOrigin, rows []vterm.DamageOp, firstRowID uint64, lastRowID uint64) []uint64 {
	rowCount := len(rows)
	if rowCount <= 0 {
		return nil
	}
	return make([]uint64, rowCount)
}

func hasNonZeroUint64(values []uint64) bool {
	for _, value := range values {
		if value != 0 {
			return true
		}
	}
	return false
}

func terminalLiveTailSegmentRowIDWindow(segment terminalLiveTailSegment, localStart int, localEnd int) (uint64, uint64) {
	if terminalLiveTailReclaimedSegmentHasRowIDs(segment) {
		first := segment.firstRowID + uint64(localStart)
		last := segment.firstRowID + uint64(localEnd-1)
		return first, last
	}
	return segment.firstRowID, segment.lastRowID
}

func terminalLiveTailReclaimedSegmentHasRowIDs(segment terminalLiveTailSegment) bool {
	if segment.origin != terminalLiveTailOriginReclaimed || len(segment.rows) == 0 || segment.lastRowID < segment.firstRowID || int(segment.lastRowID-segment.firstRowID)+1 != len(segment.rows) {
		return false
	}
	lineIDs := terminalLiveTailSegmentLogicalLineIDs(segment.logicalLineIDs, segment.rows, segment.origin, segment.firstRowID, segment.lastRowID)
	if len(lineIDs) != len(segment.rows) {
		return false
	}
	for _, id := range lineIDs {
		if !terminalPersistedLogicalLineID(id) {
			return false
		}
	}
	return true
}

func terminalLiveTailReclaimedSegmentAuthoritative(segment terminalLiveTailSegment) bool {
	return segment.origin == terminalLiveTailOriginReclaimed && terminalLiveTailReclaimedSegmentHasRowIDs(segment)
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
