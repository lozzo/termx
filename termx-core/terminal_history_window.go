package termx

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/termx-vterm/vterm"
)

// HistoryWindowOp 描述一次 history window 投影相对客户端当前窗口应如何接纳。
//
// 这是 core 对外权威语义：客户端不再自己判断 older page 是否连续、latest 是否
// 应该替换；core 直接给出 replace 或 prepend。
type HistoryWindowOp string

const (
	// HistoryWindowReplace 表示整窗替换，用于 latest（offset=0）窗口。
	HistoryWindowReplace HistoryWindowOp = "replace"
	// HistoryWindowPrepend 表示在客户端当前窗口顶部前插，用于 older 窗口。
	HistoryWindowPrepend HistoryWindowOp = "prepend"
)

// HistoryLineSpan 是一条逻辑行片段在当前投影宽度下覆盖的 visual row 区间。
//
// 客户端 copy mode 用它理解窗口内逻辑行片段边界，不再根据 wrapped flag 自行拼接，
// 也不能把被窗口裁断的片段当作完整逻辑行。
type HistoryLineSpan struct {
	StartRow      int
	EndRow        int
	RowKind       string
	LogicalLineID uint64
	ClippedBefore bool
	ClippedAfter  bool
}

// HistoryRow 是 history window 中的一条 visual row 及其权威元数据。
type HistoryRow struct {
	Cells     protocol.CompactRow
	RowKind   string
	Ownership string
	Wrapped   bool
	Timestamp time.Time
}

// HistoryWindow 是 core 对外提供的权威历史投影窗口。
//
// 它只表达 core 选定的逻辑行集合在某个宽度下的投影结果，以及足以支撑 copy mode
// 的最小边界元数据。客户端只消费结果与 Op，不重建历史真相。
type HistoryWindow struct {
	TerminalID string
	Token      string
	Op         HistoryWindowOp
	Size       Size
	Rows       []HistoryRow
	Lines      []HistoryLineSpan
	// BeforeOffset 是本窗口返回后可用于请求 older window 的 before cursor。
	// 它表示当前窗口从最新端已经覆盖的 committed projection row 深度，
	// 不一定等于本次请求 offset。
	BeforeOffset int
	// LoadedRows 是本窗口覆盖到的已提交行深度（含 BeforeOffset）。
	LoadedRows int
	// LoadedLines 是本窗口实际返回且包含起点的逻辑行数量；clipped-before 片段不计入。
	LoadedLines int
	// TotalRows 是当前宽度下可投影的总 visual row 数。
	TotalRows int
	// LogicalTotal 是当前 authoritative history window 投影空间中的逻辑行总数。
	LogicalTotal int
	HasMore      bool
	Generation   uint64
	FirstRowID   uint64
	LastRowID    uint64
	FirstLineID  uint64
	LastLineID   uint64
	Timestamp    time.Time
}

// HistoryWindowOptions 控制一次 history window 请求。
type HistoryWindowOptions struct {
	// BeforeOffset 为 0 表示 latest 窗口（含 live tail），>0 表示 older 窗口。
	BeforeOffset int
	Limit        int
	Cols         int
}

// HistoryWindow 返回终端在给定宽度下的权威历史投影窗口。
func (s *Server) HistoryWindow(ctx context.Context, id string, opts ...HistoryWindowOptions) (*HistoryWindow, error) {
	_ = ctx
	var opt HistoryWindowOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	viewport, err := s.historyCoreGridViewport(id, GridViewportOptions{
		ScrollbackOffset: opt.BeforeOffset,
		ScrollbackLimit:  opt.Limit,
		Cols:             opt.Cols,
	})
	if err != nil {
		return nil, err
	}
	return historyWindowFromCoreGridViewport(id, opt.BeforeOffset, viewport), nil
}

func (s *Server) historyCoreGridViewport(id string, opt GridViewportOptions) (terminalGridViewport, error) {
	var result terminalGridViewport
	term, err := s.getTerminal(id)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			return result, err
		}
		store, storeErr := openTerminalGridStoreForReplay(s.cfg.gridRoot, id)
		if storeErr != nil {
			return result, storeErr
		}
		defer store.Close()
		beforeOffset, limit := sanitizeGridViewportWindow(opt.ScrollbackOffset, opt.ScrollbackLimit)
		cols := opt.Cols
		if cols <= 0 {
			cols = int(s.cfg.defaultSize.Cols)
		}
		if cols <= 0 {
			cols = 80
		}
		viewport, viewportErr := storeViewportWithRecoveredLiveTail(store, beforeOffset, limit, cols)
		viewport.Size.Rows = s.cfg.defaultSize.Rows
		return viewport, viewportErr
	}
	term.flushGridAppender()
	offset, limit := sanitizeGridViewportWindow(opt.ScrollbackOffset, opt.ScrollbackLimit)
	term.mu.RLock()
	size := term.size
	liveTail := term.primaryLiveTail.clone()
	term.mu.RUnlock()
	cols := opt.Cols
	if cols <= 0 {
		cols = int(size.Cols)
	}
	if cols <= 0 {
		cols = 80
	}
	viewport, viewportErr := term.combinedGridViewport(offset, limit, cols, liveTail)
	viewport.Size.Rows = size.Rows
	return viewport, viewportErr
}

func combinedGridViewportFromStore(store *terminalGridStore, offset, limit, cols int, liveTail terminalPrimaryLiveTail) (terminalGridViewport, error) {
	var result terminalGridViewport
	if store == nil {
		return result, nil
	}
	offset, limit = sanitizeGridViewportWindow(offset, limit)
	_, generation, persistedRows := store.coordinates()
	visiblePersistedRows := persistedRows - liveTail.reclaimedCommittedRowCount()
	if firstReclaimedRowID, ok := liveTail.earliestReclaimedRowID(); ok {
		baseRowID, _, _ := store.coordinates()
		if firstReclaimedRowID >= baseRowID {
			coveredStart := int(firstReclaimedRowID - baseRowID)
			if coveredStart < visiblePersistedRows {
				visiblePersistedRows = coveredStart
			}
		}
	}
	if visiblePersistedRows < 0 {
		visiblePersistedRows = 0
	}
	if offset > 0 {
		persistedViewport, err := store.Viewport(offset, limit, cols)
		if err != nil {
			return result, err
		}
		if persistedViewport.TotalRows > 0 {
			return persistedViewport, nil
		}
		result.BeforeOffset = offset
		result.Limit = limit
		result.TotalRows = visiblePersistedRows
		result.LogicalTotal = store.LogicalLineCount()
		result.LoadedRows = minInt(offset, visiblePersistedRows)
		if visiblePersistedRows > 0 {
			result.Generation = generation
		}
		return result, nil
	}
	liveTailRowsWithIDs := liveTail.rowsWithLogicalLineIDs()
	liveTailRows := liveTailRowsWithIDs.rows
	liveTailLineIDs := liveTailRowsWithIDs.logicalLineIDs
	totalRows := visiblePersistedRows + len(liveTailRows)
	if totalRows <= 0 {
		return result, nil
	}
	logicalTotal := store.logicalLineCountForPrefix(visiblePersistedRows) + projectedLogicalLineCount(liveTailLineIDs)
	end := totalRows
	start := end - limit
	if start < 0 {
		start = 0
	}
	persistedStart := minInt(start, visiblePersistedRows)
	persistedEnd := minInt(end, visiblePersistedRows)
	liveTailStart := maxInt(start-visiblePersistedRows, 0)
	liveTailEnd := maxInt(end-visiblePersistedRows, 0)

	result.BeforeOffset = offset
	result.Limit = limit
	result.TotalRows = totalRows
	result.LogicalTotal = store.LogicalLineCount()
	result.WindowLogicalTotal = logicalTotal
	if visiblePersistedRows > 0 {
		result.Generation = generation
	}

	displayStart := start
	if persistedEnd > persistedStart {
		beforeOffsetPersisted := persistedRows - persistedEnd
		limitPersisted := persistedEnd - persistedStart
		persistedViewport, err := store.Viewport(beforeOffsetPersisted, limitPersisted, cols)
		if err != nil {
			return result, err
		}
		persistedLoadedRows := persistedViewport.LoadedRows - beforeOffsetPersisted
		if persistedLoadedRows < 0 {
			persistedLoadedRows = 0
		}
		result = persistedViewport
		result.BeforeOffset = offset
		result.Limit = limit
		result.TotalRows = totalRows
		result.LogicalTotal = persistedViewport.LogicalTotal
		result.WindowLogicalTotal = logicalTotal
		result.FirstLineClippedBefore = persistedViewport.FirstLineClippedBefore
		result.Generation = persistedViewport.Generation
		result.LoadedRows = persistedLoadedRows
		result.FirstRowID = persistedViewport.FirstRowID
		result.LastRowID = persistedViewport.LastRowID
		displayStart = visiblePersistedRows - persistedLoadedRows
		if displayStart < 0 {
			displayStart = 0
		}
	}
	if liveTailEnd > liveTailStart {
		liveTailStart = expandDamageWindowStartToLogicalLine(liveTailRows, liveTailLineIDs, liveTailStart)
		if persistedEnd <= persistedStart {
			displayStart = visiblePersistedRows + liveTailStart
		}
		if liveTailEnd > len(liveTailRows) {
			liveTailEnd = len(liveTailRows)
		}
		window := liveTail.window(liveTailStart, liveTailEnd)
		for i, row := range window.rows {
			result.Rows = append(result.Rows, damageOpCells(row))
			result.Timestamps = append(result.Timestamps, row.Timestamp)
			result.RowKinds = append(result.RowKinds, row.RowKind)
			result.Wrapped = append(result.Wrapped, row.WrappedSet && row.Wrapped)
			result.Ownership = append(result.Ownership, stringAt(window.ownership, i))
			result.LogicalLineIDs = append(result.LogicalLineIDs, uint64At(window.logicalLineIDs, i))
		}
		if window.hasCommitted {
			result.LoadedRows += window.committed
			if result.Generation == 0 {
				result.Generation = window.generation
			}
			if result.FirstRowID == 0 && result.LastRowID == 0 {
				result.FirstRowID = window.firstRowID
				result.LastRowID = window.lastRowID
			} else {
				if window.firstRowID < result.FirstRowID {
					result.FirstRowID = window.firstRowID
				}
				if window.lastRowID > result.LastRowID {
					result.LastRowID = window.lastRowID
				}
			}
		}
	}
	if visiblePersistedRows <= 0 && result.LoadedRows == 0 {
		result.LoadedRows = 0
		result.Generation = 0
		result.FirstRowID = 0
		result.LastRowID = 0
	}
	result.HasMore = displayStart > 0
	return result, nil
}

func historyWindowFromCoreGridViewport(id string, beforeOffset int, viewport terminalGridViewport) *HistoryWindow {
	if len(viewport.Rows) == 0 && viewport.TotalRows == 0 {
		return &HistoryWindow{TerminalID: id, Op: historyWindowOpForOffset(beforeOffset), Timestamp: time.Now().UTC()}
	}
	viewport = historyWindowTrimViewportToLimit(viewport)
	viewport = historyWindowFilterViewportToAuthoritativeRows(viewport)
	rows := make([]HistoryRow, len(viewport.Rows))
	coreRows := convertRows(viewport.Rows)
	for i := range viewport.Rows {
		rows[i] = HistoryRow{
			Cells:     protocolCompactRowFromCoreWithOptions(coreRows[i], true),
			RowKind:   stringAt(viewport.RowKinds, i),
			Ownership: stringAt(viewport.Ownership, i),
			Wrapped:   boolAt(viewport.Wrapped, i),
			Timestamp: timeAt(viewport.Timestamps, i),
		}
	}
	lines := historyLineSpans(viewport.Wrapped, viewport.RowKinds, viewport.LogicalLineIDs, len(viewport.Rows), viewport.FirstLineClippedBefore)
	firstLineID, lastLineID := historyLineSpanIDBoundary(lines)
	logicalTotal := viewport.LogicalTotal
	if viewport.WindowLogicalTotal > 0 {
		logicalTotal = viewport.WindowLogicalTotal
	}
	return &HistoryWindow{
		TerminalID:   id,
		Token:        historyWindowToken(viewport),
		Op:           historyWindowOpForOffset(beforeOffset),
		Size:         Size{Cols: viewport.Size.Cols, Rows: viewport.Size.Rows},
		Rows:         rows,
		Lines:        lines,
		BeforeOffset: historyWindowBeforeCursor(beforeOffset, viewport),
		LoadedRows:   viewport.LoadedRows,
		LoadedLines:  historyLoadedLogicalLineCount(lines),
		TotalRows:    viewport.TotalRows,
		LogicalTotal: logicalTotal,
		HasMore:      viewport.HasMore,
		Generation:   viewport.Generation,
		FirstRowID:   viewport.FirstRowID,
		LastRowID:    viewport.LastRowID,
		FirstLineID:  firstLineID,
		LastLineID:   lastLineID,
		Timestamp:    time.Now().UTC(),
	}
}

func historyWindowTrimViewportToLimit(viewport terminalGridViewport) terminalGridViewport {
	if viewport.Limit <= 0 || len(viewport.Rows) <= viewport.Limit {
		return viewport
	}
	trimStart := len(viewport.Rows) - viewport.Limit
	trimmedCommittedRows := terminalHistoryWindowCommittedOwnershipCount(viewport.Ownership[:trimStart])
	if trimTerminalGridViewportToTail(&viewport, viewport.Limit) {
		viewport.LoadedRows -= trimmedCommittedRows
		if viewport.LoadedRows < viewport.BeforeOffset {
			viewport.LoadedRows = viewport.BeforeOffset
		}
	}
	return viewport
}

func historyWindowFilterViewportToAuthoritativeRows(viewport terminalGridViewport) terminalGridViewport {
	if len(viewport.Rows) == 0 {
		return viewport
	}
	keep := make([]int, 0, len(viewport.Rows))
	for i := range viewport.Rows {
		if uint64At(viewport.LogicalLineIDs, i) != 0 {
			keep = append(keep, i)
		}
	}
	if len(keep) == len(viewport.Rows) {
		return viewport
	}
	removedCommittedRows := 0
	for i := range viewport.Rows {
		if uint64At(viewport.LogicalLineIDs, i) != 0 {
			continue
		}
		switch stringAt(viewport.Ownership, i) {
		case RowOwnershipPersisted, RowOwnershipLiveTailReclaimed:
			removedCommittedRows++
		}
	}
	filtered := viewport
	filtered.Rows = filterVTermRowsByIndexes(viewport.Rows, keep)
	filtered.Timestamps = filterTimesByIndexes(viewport.Timestamps, keep)
	filtered.RowKinds = filterStringsByIndexes(viewport.RowKinds, keep)
	filtered.Wrapped = filterBoolsByIndexes(viewport.Wrapped, keep)
	filtered.Ownership = filterStringsByIndexes(viewport.Ownership, keep)
	filtered.LogicalLineIDs = filterUint64sByIndexes(viewport.LogicalLineIDs, keep)
	filtered.LoadedRows -= removedCommittedRows
	if filtered.LoadedRows < filtered.BeforeOffset {
		filtered.LoadedRows = filtered.BeforeOffset
	}
	if viewport.TotalRows > 0 {
		filtered.TotalRows = maxInt(0, viewport.TotalRows-(len(viewport.Rows)-len(keep)))
	}
	if len(keep) == 0 {
		filtered.FirstLineClippedBefore = false
		filtered.HasMore = false
		filtered.LoadedRows = filtered.BeforeOffset
		filtered.TotalRows = 0
		filtered.LogicalTotal = 0
		filtered.WindowLogicalTotal = 0
		filtered.Generation = 0
		filtered.FirstRowID = 0
		filtered.LastRowID = 0
	} else if keep[0] != 0 {
		filtered.FirstLineClippedBefore = false
	}
	return filtered
}

func terminalHistoryWindowCommittedOwnershipCount(ownership []string) int {
	count := 0
	for _, value := range ownership {
		switch value {
		case RowOwnershipPersisted, RowOwnershipLiveTailReclaimed:
			count++
		}
	}
	return count
}

func filterVTermRowsByIndexes(values [][]vterm.Cell, indexes []int) [][]vterm.Cell {
	if len(values) == 0 || len(indexes) == 0 {
		return nil
	}
	out := make([][]vterm.Cell, 0, len(indexes))
	for _, index := range indexes {
		if index < 0 || index >= len(values) {
			continue
		}
		out = append(out, cloneVTermCells(values[index]))
	}
	return out
}

func filterTimesByIndexes(values []time.Time, indexes []int) []time.Time {
	if len(values) == 0 || len(indexes) == 0 {
		return nil
	}
	out := make([]time.Time, 0, len(indexes))
	for _, index := range indexes {
		out = append(out, timeAt(values, index))
	}
	return out
}

func filterStringsByIndexes(values []string, indexes []int) []string {
	if len(values) == 0 || len(indexes) == 0 {
		return nil
	}
	out := make([]string, 0, len(indexes))
	for _, index := range indexes {
		out = append(out, stringAt(values, index))
	}
	return out
}

func filterBoolsByIndexes(values []bool, indexes []int) []bool {
	if len(values) == 0 || len(indexes) == 0 {
		return nil
	}
	out := make([]bool, 0, len(indexes))
	for _, index := range indexes {
		out = append(out, boolAt(values, index))
	}
	return out
}

func filterUint64sByIndexes(values []uint64, indexes []int) []uint64 {
	if len(values) == 0 || len(indexes) == 0 {
		return nil
	}
	out := make([]uint64, 0, len(indexes))
	for _, index := range indexes {
		out = append(out, uint64At(values, index))
	}
	return out
}

func historyLoadedLogicalLineCount(lines []HistoryLineSpan) int {
	count := 0
	for _, span := range lines {
		if span.ClippedBefore {
			continue
		}
		count++
	}
	return count
}

func projectedLogicalLineCount(logicalLineIDs []uint64) int {
	count := 0
	for row := 0; row < len(logicalLineIDs); row++ {
		logicalLineID := logicalLineIDs[row]
		if logicalLineID == 0 {
			continue
		}
		count++
		for row+1 < len(logicalLineIDs) && logicalLineIDs[row+1] == logicalLineID {
			row++
		}
	}
	return count
}

func historyWindowBeforeCursor(requestBeforeOffset int, viewport terminalGridViewport) int {
	if viewport.LoadedRows > requestBeforeOffset {
		return viewport.LoadedRows
	}
	return requestBeforeOffset
}

func historyWindowOpForOffset(beforeOffset int) HistoryWindowOp {
	if beforeOffset > 0 {
		return HistoryWindowPrepend
	}
	return HistoryWindowReplace
}

// historyWindowToken 把窗口的权威边界编码成一个稳定 token。
//
// 客户端只把它当作不透明的边界版本：相同 token 表示相同已提交边界，token 变化
// 表示边界已经移动，旧的分页响应应当被丢弃。
func historyWindowToken(viewport terminalGridViewport) string {
	if len(viewport.Rows) == 0 && viewport.TotalRows == 0 {
		return ""
	}
	firstLineID, lastLineID := historyWindowLogicalLineIDBoundary(viewport.LogicalLineIDs)
	return fmt.Sprintf("g%d:%d-%d:l%d-%d:c%d", viewport.Generation, viewport.FirstRowID, viewport.LastRowID, firstLineID, lastLineID, viewport.Size.Cols)
}

func historyWindowLogicalLineIDBoundary(logicalLineIDs []uint64) (uint64, uint64) {
	var first uint64
	var last uint64
	for _, id := range logicalLineIDs {
		if id == 0 {
			continue
		}
		if first == 0 {
			first = id
		}
		last = id
	}
	return first, last
}

func historyLineSpanIDBoundary(spans []HistoryLineSpan) (uint64, uint64) {
	var first uint64
	var last uint64
	for _, span := range spans {
		if span.ClippedBefore {
			continue
		}
		if span.LogicalLineID == 0 {
			continue
		}
		if first == 0 {
			first = span.LogicalLineID
		}
		last = span.LogicalLineID
	}
	return first, last
}

// historyLineSpans 只按 core projection 给出的 stable logical line id 归并
// visual rows。wrapped 仅用于表达窗口末尾是否裁断投影，不再作为逻辑行真相回退。
func historyLineSpans(wrapped []bool, rowKinds []string, logicalLineIDs []uint64, rowCount int, firstLineClippedBefore bool) []HistoryLineSpan {
	if rowCount <= 0 {
		return nil
	}
	spans := make([]HistoryLineSpan, 0, rowCount)
	for row := 0; row < rowCount; row++ {
		logicalLineID := uint64At(logicalLineIDs, row)
		if logicalLineID == 0 {
			continue
		}
		start := row
		for row+1 < rowCount && uint64At(logicalLineIDs, row+1) == logicalLineID {
			row++
		}
		spans = append(spans, HistoryLineSpan{
			StartRow:      start,
			EndRow:        row,
			RowKind:       stringAt(rowKinds, start),
			LogicalLineID: logicalLineID,
			ClippedBefore: firstLineClippedBefore && start == 0,
			ClippedAfter:  historyLineSpanClippedAfter(wrapped, logicalLineIDs, row, rowCount),
		})
	}
	return spans
}

func historyLineSpanClippedAfter(wrapped []bool, logicalLineIDs []uint64, row int, rowCount int) bool {
	if row != rowCount-1 {
		return false
	}
	if uint64At(logicalLineIDs, row) == 0 {
		return false
	}
	return boolAt(wrapped, row)
}

func protocolHistoryWindowFromCore(window *HistoryWindow) *protocol.HistoryWindow {
	if window == nil {
		return nil
	}
	rows := make([]protocol.CompactRow, len(window.Rows))
	rowKinds := make([]string, len(window.Rows))
	rowWrapped := make([]bool, len(window.Rows))
	rowOwnership := make([]string, len(window.Rows))
	rowTimestamps := make([]time.Time, len(window.Rows))
	for i, row := range window.Rows {
		rows[i] = row.Cells
		rowKinds[i] = row.RowKind
		rowWrapped[i] = row.Wrapped
		rowOwnership[i] = row.Ownership
		rowTimestamps[i] = row.Timestamp
	}
	lines := make([]protocol.HistoryLineSpan, len(window.Lines))
	for i, span := range window.Lines {
		lines[i] = protocol.HistoryLineSpan{
			StartRow:      span.StartRow,
			EndRow:        span.EndRow,
			RowKind:       span.RowKind,
			LogicalLineID: span.LogicalLineID,
			ClippedBefore: span.ClippedBefore,
			ClippedAfter:  span.ClippedAfter,
		}
	}
	return &protocol.HistoryWindow{
		TerminalID:    window.TerminalID,
		Token:         window.Token,
		Op:            protocol.HistoryWindowOp(window.Op),
		Size:          protocol.Size{Cols: window.Size.Cols, Rows: window.Size.Rows},
		Rows:          rows,
		RowTimestamps: rowTimestamps,
		RowKinds:      rowKinds,
		RowWrapped:    rowWrapped,
		RowOwnership:  rowOwnership,
		Lines:         lines,
		BeforeOffset:  window.BeforeOffset,
		LoadedRows:    window.LoadedRows,
		TotalRows:     window.TotalRows,
		LoadedLines:   window.LoadedLines,
		LogicalTotal:  window.LogicalTotal,
		HasMore:       window.HasMore,
		Generation:    window.Generation,
		FirstRowID:    window.FirstRowID,
		LastRowID:     window.LastRowID,
		FirstLineID:   window.FirstLineID,
		LastLineID:    window.LastLineID,
		Timestamp:     window.Timestamp,
	}
}
