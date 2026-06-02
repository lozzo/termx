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

const terminalLiveTailLogicalLineIDBase uint64 = 1 << 63

type terminalLiveTailSegment struct {
	origin         terminalLiveTailOrigin
	sealState      terminalLiveTailSealState
	rows           []vterm.DamageOp
	logicalLineIDs []uint64
	wrapPending    bool
	generation     uint64
	firstRowID     uint64
	lastRowID      uint64
}

type terminalPrimaryLiveTail struct {
	segments    []terminalLiveTailSegment
	wrapPending bool
}

type terminalLiveTailWindow struct {
	rows           []vterm.DamageOp
	ownership      []string
	logicalLineIDs []uint64
	committed      int
	generation     uint64
	firstRowID     uint64
	lastRowID      uint64
	hasCommitted   bool
}

type terminalLiveTailLogicalLineRecord struct {
	id         uint64
	startRow   int
	endRow     int
	sealState  terminalLiveTailSealState
	origin     terminalLiveTailOrigin
	residency  terminalLogicalLineResidency
	dirty      bool
	generation uint64
	rowIDKnown bool
	firstRowID uint64
	lastRowID  uint64
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
		return terminalPrimaryLiveTail{wrapPending: tail.wrapPending}
	}
	out := terminalPrimaryLiveTail{
		segments:    make([]terminalLiveTailSegment, 0, len(tail.segments)),
		wrapPending: tail.wrapPending,
	}
	for _, segment := range tail.segments {
		out.segments = append(out.segments, terminalLiveTailSegment{
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
		origin:         origin,
		sealState:      terminalLiveTailSealStateForRows(rows, wrapPending),
		rows:           cloneGridDamageOps(rows),
		logicalLineIDs: terminalLiveTailSegmentFallbackLogicalLineIDs(origin, rows, 0, 0),
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
	if len(rows) > 0 {
		next = append(next, terminalLiveTailSegment{
			origin:         terminalLiveTailOriginResize,
			sealState:      terminalLiveTailSealStateForRows(rows, wrapPending),
			rows:           cloneGridDamageOps(rows),
			logicalLineIDs: terminalLiveTailSegmentLiveLogicalLineIDs(logicalLineIDs, rows),
			wrapPending:    wrapPending,
		})
	}
	tail.segments = next
}

func (tail *terminalPrimaryLiveTail) replaceLiveRows(rows []vterm.DamageOp, wrapPending bool) {
	tail.replaceLiveRowsWithLogicalLineIDs(rows, nil, wrapPending)
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
	if len(rows) > 0 {
		next = append(next, terminalLiveTailSegment{
			origin:         terminalLiveTailOriginLive,
			sealState:      terminalLiveTailSealStateForRows(rows, wrapPending),
			rows:           cloneGridDamageOps(rows),
			logicalLineIDs: terminalLiveTailSegmentLiveLogicalLineIDs(logicalLineIDs, rows),
			wrapPending:    wrapPending,
		})
	}
	tail.segments = next
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
				logicalLineIDs: terminalLiveTailSegmentLiveLogicalLineIDs(resizeTail.logicalLineIDs, resizeTail.rows),
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
	lineIDs := terminalLiveTailSegmentLiveLogicalLineIDs(logicalLineIDs, resizeRows)
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
		out.logicalLineIDs = append(out.logicalLineIDs, terminalLiveTailSegmentLogicalLineIDsWindow(segment, localStart, localEnd)...)
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

func (tail terminalPrimaryLiveTail) logicalLineRecords() []terminalLiveTailLogicalLineRecord {
	if len(tail.segments) == 0 {
		return nil
	}
	records := make([]terminalLiveTailLogicalLineRecord, 0, tail.rowCount())
	cursor := 0
	for _, segment := range tail.segments {
		records = append(records, terminalLiveTailSegmentLogicalLineRecords(segment, cursor)...)
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
	start := 0
	for i := range segment.rows {
		if terminalLiveTailRecordContinues(logicalLineIDs, segment.rows, i) {
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

func terminalLiveTailRecordContinues(logicalLineIDs []uint64, rows []vterm.DamageOp, row int) bool {
	if row < 0 || row >= len(rows)-1 {
		return false
	}
	currentID := uint64At(logicalLineIDs, row)
	nextID := uint64At(logicalLineIDs, row+1)
	if currentID != 0 && nextID != 0 {
		return currentID == nextID
	}
	currentRow := rows[row]
	return currentRow.WrappedSet && currentRow.Wrapped
}

func terminalLiveTailRecordDirty(segment terminalLiveTailSegment) bool {
	return terminalLiveTailOriginDirty(segment.origin)
}

func terminalLiveTailRecordSealState(segment terminalLiveTailSegment, row int) terminalLiveTailSealState {
	if segment.origin == terminalLiveTailOriginReclaimed {
		return segment.sealState
	}
	if row < len(segment.rows)-1 {
		return terminalLiveTailSealed
	}
	return terminalLiveTailSealStateForRows(segment.rows, segment.wrapPending)
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
		return aligned
	}
	return terminalLiveTailSegmentFallbackLogicalLineIDs(origin, rows, firstRowID, lastRowID)
}

func terminalLiveTailSegmentLiveLogicalLineIDs(lineIDs []uint64, rows []vterm.DamageOp) []uint64 {
	rowCount := len(rows)
	if rowCount <= 0 {
		return nil
	}
	aligned := alignLiveTailUint64s(lineIDs, rowCount)
	nextID := maxUint64Slice(aligned)
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
	return aligned
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
	out := make([]uint64, rowCount)
	if origin != terminalLiveTailOriginReclaimed {
		return out
	}
	if firstRowID == 0 && lastRowID == 0 {
		return out
	}
	if lastRowID >= firstRowID && int(lastRowID-firstRowID)+1 == rowCount {
		start := 0
		for i, row := range rows {
			if row.WrappedSet && row.Wrapped && i < len(rows)-1 {
				continue
			}
			id := persistedLogicalLineIDFromRowID(firstRowID + uint64(start))
			for rowIndex := start; rowIndex <= i; rowIndex++ {
				out[rowIndex] = id
			}
			start = i + 1
		}
	}
	return out
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
	return segment.origin == terminalLiveTailOriginReclaimed && len(segment.rows) > 0 && segment.lastRowID >= segment.firstRowID && int(segment.lastRowID-segment.firstRowID)+1 == len(segment.rows)
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
