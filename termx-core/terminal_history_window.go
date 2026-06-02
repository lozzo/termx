package termx

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/lozzow/termx/internal/protocol"
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
	// TotalRows 是当前宽度下可投影的总 visual row 数。
	TotalRows int
	// LogicalTotal 是已提交逻辑行总数。
	LogicalTotal int
	HasMore      bool
	Generation   uint64
	FirstRowID   uint64
	LastRowID    uint64
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
		if beforeOffset == 0 {
			if liveTail, ok := store.recoveredLiveTailFromMetadata(); ok {
				viewport, viewportErr := combinedGridViewportFromStore(store, beforeOffset, limit, cols, liveTail)
				viewport.Size.Rows = s.cfg.defaultSize.Rows
				return viewport, viewportErr
			}
		}
		viewport, viewportErr := store.Viewport(beforeOffset, limit, cols)
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
	liveTailRows := liveTail.rows()
	totalRows := visiblePersistedRows + len(liveTailRows)
	if totalRows <= 0 {
		return result, nil
	}
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
		result = persistedViewport
		result.BeforeOffset = offset
		result.Limit = limit
		result.TotalRows = totalRows
		result.LogicalTotal = persistedViewport.LogicalTotal
		result.Generation = persistedViewport.Generation
		result.LoadedRows = persistedViewport.LoadedRows
		result.FirstRowID = persistedViewport.FirstRowID
		result.LastRowID = persistedViewport.LastRowID
		displayStart = visiblePersistedRows - persistedViewport.LoadedRows
	}
	if liveTailEnd > liveTailStart {
		liveTailStart = expandDamageWindowStartToLogicalLine(liveTailRows, liveTailStart)
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
	return &HistoryWindow{
		TerminalID:   id,
		Token:        historyWindowToken(viewport),
		Op:           historyWindowOpForOffset(beforeOffset),
		Size:         Size{Cols: viewport.Size.Cols, Rows: viewport.Size.Rows},
		Rows:         rows,
		Lines:        historyLineSpans(viewport.Wrapped, viewport.RowKinds, viewport.LogicalLineIDs, len(viewport.Rows), beforeOffset),
		BeforeOffset: historyWindowBeforeCursor(beforeOffset, viewport),
		LoadedRows:   viewport.LoadedRows,
		TotalRows:    viewport.TotalRows,
		LogicalTotal: viewport.LogicalTotal,
		HasMore:      viewport.HasMore,
		Generation:   viewport.Generation,
		FirstRowID:   viewport.FirstRowID,
		LastRowID:    viewport.LastRowID,
		Timestamp:    time.Now().UTC(),
	}
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
	return fmt.Sprintf("g%d:%d-%d:c%d", viewport.Generation, viewport.FirstRowID, viewport.LastRowID, viewport.Size.Cols)
}

// historyLineSpans 优先按 core projection 给出的 stable logical line id
// 归并 visual rows；旧 projection 缺失 id 时才回退 wrapped 元数据。
func historyLineSpans(wrapped []bool, rowKinds []string, logicalLineIDs []uint64, rowCount int, beforeOffset int) []HistoryLineSpan {
	if rowCount <= 0 {
		return nil
	}
	spans := make([]HistoryLineSpan, 0, rowCount)
	start := 0
	for row := 0; row < rowCount; row++ {
		if historyLineSpanContinues(wrapped, logicalLineIDs, row, rowCount) {
			continue
		}
		spans = append(spans, HistoryLineSpan{
			StartRow:      start,
			EndRow:        row,
			RowKind:       stringAt(rowKinds, start),
			LogicalLineID: uint64At(logicalLineIDs, start),
			ClippedBefore: beforeOffset > 0 && start == 0,
			ClippedAfter:  historyLineSpanClippedAfter(wrapped, logicalLineIDs, row, rowCount),
		})
		start = row + 1
	}
	return spans
}

func historyLineSpanContinues(wrapped []bool, logicalLineIDs []uint64, row int, rowCount int) bool {
	if row < 0 || row >= rowCount-1 {
		return false
	}
	currentID := uint64At(logicalLineIDs, row)
	nextID := uint64At(logicalLineIDs, row+1)
	if currentID != 0 && nextID != 0 {
		return currentID == nextID
	}
	return boolAt(wrapped, row)
}

func historyLineSpanClippedAfter(wrapped []bool, logicalLineIDs []uint64, row int, rowCount int) bool {
	if row != rowCount-1 {
		return false
	}
	if uint64At(logicalLineIDs, row) != 0 {
		return boolAt(wrapped, row)
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
		LogicalTotal:  window.LogicalTotal,
		HasMore:       window.HasMore,
		Generation:    window.Generation,
		FirstRowID:    window.FirstRowID,
		LastRowID:     window.LastRowID,
		Timestamp:     window.Timestamp,
	}
}
