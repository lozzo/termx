package termx

import (
	"context"
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
	var opt HistoryWindowOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	viewport, err := s.GridViewport(ctx, id, GridViewportOptions{
		ScrollbackOffset: opt.BeforeOffset,
		ScrollbackLimit:  opt.Limit,
		Cols:             opt.Cols,
	})
	if err != nil {
		return nil, err
	}
	return historyWindowFromGridViewport(id, opt.BeforeOffset, viewport), nil
}

func historyWindowFromGridViewport(id string, beforeOffset int, viewport *protocol.GridViewport) *HistoryWindow {
	if viewport == nil {
		return &HistoryWindow{TerminalID: id, Op: historyWindowOpForOffset(beforeOffset), Timestamp: time.Now().UTC()}
	}
	rows := make([]HistoryRow, len(viewport.Rows))
	for i := range viewport.Rows {
		rows[i] = HistoryRow{
			Cells:     viewport.Rows[i],
			RowKind:   stringAt(viewport.ScrollbackRowKinds, i),
			Ownership: stringAt(viewport.RowOwnership, i),
			Wrapped:   boolAt(viewport.ScrollbackWrapped, i),
			Timestamp: timeAt(viewport.ScrollbackTimestamps, i),
		}
	}
	return &HistoryWindow{
		TerminalID:   id,
		Token:        historyWindowToken(viewport),
		Op:           historyWindowOpForOffset(beforeOffset),
		Size:         Size{Cols: viewport.Size.Cols, Rows: viewport.Size.Rows},
		Rows:         rows,
		Lines:        historyLineSpans(viewport.ScrollbackWrapped, viewport.ScrollbackRowKinds, len(viewport.Rows), beforeOffset, viewport.FirstRowID),
		BeforeOffset: historyWindowBeforeCursor(beforeOffset, viewport),
		LoadedRows:   viewport.LoadedRows,
		TotalRows:    viewport.ScrollbackTotal,
		LogicalTotal: viewport.ScrollbackLogicalTotal,
		HasMore:      viewport.ScrollbackHasMore,
		Generation:   viewport.HistoryGeneration,
		FirstRowID:   viewport.FirstRowID,
		LastRowID:    viewport.LastRowID,
		Timestamp:    time.Now().UTC(),
	}
}

func historyWindowBeforeCursor(requestBeforeOffset int, viewport *protocol.GridViewport) int {
	if viewport == nil {
		return requestBeforeOffset
	}
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
func historyWindowToken(viewport *protocol.GridViewport) string {
	if viewport == nil {
		return ""
	}
	return fmt.Sprintf("g%d:%d-%d:c%d", viewport.HistoryGeneration, viewport.FirstRowID, viewport.LastRowID, viewport.Size.Cols)
}

// historyLineSpans 根据 wrapped 元数据把 visual rows 归并成逻辑行区间。
//
// wrapped[i]==true 表示第 i 行与下一行属于同一条逻辑行。
func historyLineSpans(wrapped []bool, rowKinds []string, rowCount int, beforeOffset int, firstRowID uint64) []HistoryLineSpan {
	if rowCount <= 0 {
		return nil
	}
	spans := make([]HistoryLineSpan, 0, rowCount)
	start := 0
	for row := 0; row < rowCount; row++ {
		if boolAt(wrapped, row) && row < rowCount-1 {
			continue
		}
		spans = append(spans, HistoryLineSpan{
			StartRow:      start,
			EndRow:        row,
			RowKind:       stringAt(rowKinds, start),
			LogicalLineID: historyLineSpanLogicalLineID(firstRowID, start),
			ClippedBefore: beforeOffset > 0 && start == 0,
			ClippedAfter:  row == rowCount-1 && boolAt(wrapped, row),
		})
		start = row + 1
	}
	return spans
}

func historyLineSpanLogicalLineID(firstRowID uint64, startRow int) uint64 {
	if firstRowID == 0 && startRow == 0 {
		return 0
	}
	return firstRowID + uint64(startRow)
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
