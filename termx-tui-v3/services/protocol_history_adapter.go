package services

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

type ProtocolHistoryClient interface {
	HistoryWindow(context.Context, protocol.HistoryWindowParams) (*protocol.HistoryWindow, error)
	HistoryCopy(context.Context, protocol.HistoryWindowParams) (string, error)
	ReleaseHistory(context.Context, protocol.HistoryWindowParams) error
}

// ErrStaleHistoryWindow 是 core 拒绝过期 token/cursor 时的 typed sentinel。
var ErrStaleHistoryWindow = errors.New("stale history window")

// ProtocolCoreClientAdapter 只把 core-v2 history.window/copy/release 映射成 service result。
// stale 接纳必须留给 reducer-owned HistoryStore，service 不能持有或修改 TUI 状态。
type ProtocolCoreClientAdapter struct {
	Client ProtocolHistoryClient
}

func (adapter ProtocolCoreClientAdapter) HistoryLatest(ctx context.Context, req HistoryLatestRequest) (HistoryResult, error) {
	window, err := adapter.historyWindow(ctx, protocol.HistoryWindowParams{
		TerminalID: req.TerminalID,
		Limit:      req.Rows,
		Cols:       req.Cols,
		Generation: req.GenerationBoundary,
	})
	if err != nil {
		return HistoryResult{RequestID: req.RequestID}, normalizeProtocolHistoryError(err)
	}
	window.PaneID = req.PaneID
	window.ViewID = req.ViewID
	return HistoryResult{RequestID: req.RequestID, Window: window}, nil
}

func (adapter ProtocolCoreClientAdapter) HistoryOlder(ctx context.Context, req HistoryOlderRequest) (HistoryResult, error) {
	window, err := adapter.historyWindow(ctx, protocol.HistoryWindowParams{
		TerminalID:          req.TerminalID,
		Limit:               req.Rows,
		Cols:                req.Cols,
		Mode:                string(state.HistoryRequestOlder),
		Token:               req.Token,
		Generation:          req.Generation,
		CursorValid:         req.Cursor.Valid,
		BeforeLineID:        req.Cursor.BeforeLineID,
		BeforeRowInLine:     req.Cursor.BeforeRowInLine,
		BeforeRowIndex:      req.Cursor.BeforeRowIndex,
		CursorSegment:       req.Cursor.Segment,
		BoundaryFirstLineID: req.Boundary.FirstLineID,
		BoundaryLastLineID:  req.Boundary.LastLineID,
	})
	if err != nil {
		return HistoryResult{RequestID: req.RequestID}, normalizeProtocolHistoryError(err)
	}
	window.PaneID = req.PaneID
	window.ViewID = req.ViewID
	return HistoryResult{RequestID: req.RequestID, Window: window}, nil
}

func (adapter ProtocolCoreClientAdapter) HistoryNewer(ctx context.Context, req HistoryNewerRequest) (HistoryResult, error) {
	window, err := adapter.historyWindow(ctx, protocol.HistoryWindowParams{
		TerminalID:          req.TerminalID,
		Limit:               req.Rows,
		Cols:                req.Cols,
		Mode:                string(state.HistoryRequestNewer),
		Token:               req.Token,
		Generation:          req.Generation,
		AfterCursorValid:    req.Cursor.Valid,
		AfterLineID:         req.Cursor.BeforeLineID,
		AfterRowInLine:      req.Cursor.BeforeRowInLine,
		AfterRowIndex:       req.Cursor.BeforeRowIndex,
		AfterCursorSegment:  req.Cursor.Segment,
		BoundaryFirstLineID: req.Boundary.FirstLineID,
		BoundaryLastLineID:  req.Boundary.LastLineID,
	})
	if err != nil {
		return HistoryResult{RequestID: req.RequestID}, normalizeProtocolHistoryError(err)
	}
	window.PaneID = req.PaneID
	window.ViewID = req.ViewID
	return HistoryResult{RequestID: req.RequestID, Window: window}, nil
}

func (adapter ProtocolCoreClientAdapter) HistoryOldest(ctx context.Context, req HistoryOldestRequest) (HistoryResult, error) {
	window, err := adapter.historyWindow(ctx, protocol.HistoryWindowParams{
		TerminalID:          req.TerminalID,
		Limit:               req.Rows,
		Cols:                req.Cols,
		Mode:                string(state.HistoryRequestOldest),
		Token:               req.Token,
		Generation:          req.Generation,
		BoundaryFirstLineID: req.Boundary.FirstLineID,
		BoundaryLastLineID:  req.Boundary.LastLineID,
	})
	if err != nil {
		return HistoryResult{RequestID: req.RequestID}, normalizeProtocolHistoryError(err)
	}
	window.PaneID = req.PaneID
	window.ViewID = req.ViewID
	return HistoryResult{RequestID: req.RequestID, Window: window}, nil
}

func (adapter ProtocolCoreClientAdapter) ReleaseHistory(ctx context.Context, req HistoryReleaseRequest) error {
	if adapter.Client == nil || req.Token == "" {
		return nil
	}
	return adapter.Client.ReleaseHistory(ctx, protocol.HistoryWindowParams{
		TerminalID: req.TerminalID,
		Token:      req.Token,
	})
}

func (adapter ProtocolCoreClientAdapter) HistoryCopyRange(ctx context.Context, req HistoryCopyRangeRequest) (HistoryCopyRangeResult, error) {
	if adapter.Client == nil {
		return HistoryCopyRangeResult{}, ErrMissingTerminalClient
	}
	text, err := adapter.Client.HistoryCopy(ctx, protocol.HistoryWindowParams{
		TerminalID:          req.TerminalID,
		Cols:                req.Cols,
		Token:               req.Token,
		Generation:          req.Generation,
		BoundaryFirstLineID: req.Boundary.FirstLineID,
		BoundaryLastLineID:  req.Boundary.LastLineID,
		RangeValid:          req.Start.Valid && req.End.Valid,
		RangeStartLineID:    req.Start.LineID,
		RangeStartCol:       req.Start.Col,
		RangeEndLineID:      req.End.LineID,
		RangeEndCol:         req.End.Col,
	})
	if err != nil {
		return HistoryCopyRangeResult{}, normalizeProtocolHistoryError(err)
	}
	return HistoryCopyRangeResult{Text: text}, nil
}

func (adapter ProtocolCoreClientAdapter) historyWindow(ctx context.Context, params protocol.HistoryWindowParams) (state.HistoryWindow, error) {
	if adapter.Client == nil {
		return state.HistoryWindow{}, ErrMissingTerminalClient
	}
	window, err := adapter.Client.HistoryWindow(ctx, params)
	if err != nil {
		return state.HistoryWindow{}, normalizeProtocolHistoryError(err)
	}
	return historyWindowFromProtocol(window, params.Cols), nil
}

func normalizeProtocolHistoryError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrStaleHistoryWindow) {
		return err
	}
	if strings.Contains(strings.ToLower(err.Error()), ErrStaleHistoryWindow.Error()) {
		return fmt.Errorf("%w: %v", ErrStaleHistoryWindow, err)
	}
	return err
}

func historyWindowFromProtocol(window *protocol.HistoryWindow, requestedCols int) state.HistoryWindow {
	if window == nil {
		return state.HistoryWindow{}
	}
	cols := requestedCols
	if cols <= 0 {
		cols = int(window.Size.Cols)
	}
	rows := make([]state.HistoryRow, 0, len(window.Rows))
	for i, row := range window.Rows {
		text, cells := historyTextAndCellsFromCompactRow(row)
		rows = append(rows, state.HistoryRow{
			Text:               text,
			Cells:              cells,
			TailFill:           historyTailFillFromProtocol(row.TailFill),
			LineID:             uint64At(window.RowLineIDs, i),
			RowInLine:          intAt(window.RowInLine, i),
			Kind:               stringAt(window.RowKinds, i),
			Segment:            stringAt(window.RowSegments, i),
			SessionID:          uint64At(window.RowSessionIDs, i),
			FrameID:            uint64At(window.RowFrameIDs, i),
			FixedGrid:          boolAt(window.RowFixedGrid, i),
			ScreenCols:         intAt(window.RowScreenCols, i),
			ProjectionRowIndex: intAt(window.RowIndexes, i),
			LiveTail:           historyRowIsLiveTail(window, i),
		})
	}
	lines := make([]state.HistoryLineSpan, 0, len(window.Lines))
	for _, span := range window.Lines {
		lines = append(lines, state.HistoryLineSpan{
			LineID:             span.LogicalLineID,
			StartRow:           span.StartRow,
			EndRow:             span.EndRow,
			Kind:               span.RowKind,
			Segment:            stringAt(window.RowSegments, span.StartRow),
			SessionID:          firstNonZeroUint64(span.SessionID, uint64At(window.RowSessionIDs, span.StartRow)),
			FrameID:            firstNonZeroUint64(span.FrameID, uint64At(window.RowFrameIDs, span.StartRow)),
			FixedGrid:          span.FixedGrid || boolAt(window.RowFixedGrid, span.StartRow),
			ScreenCols:         firstNonZeroInt(span.ScreenCols, intAt(window.RowScreenCols, span.StartRow)),
			ProjectionRowIndex: intAt(window.RowIndexes, span.StartRow),
			ClippedBefore:      span.ClippedBefore,
			ClippedAfter:       span.ClippedAfter,
		})
	}
	sourceLines := historySourceLinesFromRows(rows, lines)
	return state.HistoryWindow{
		TerminalID:  window.TerminalID,
		Token:       window.Token,
		Op:          state.HistoryWindowOp(window.Op),
		Cols:        cols,
		SourceLines: sourceLines,
		Rows:        rows,
		Lines:       lines,
		Cursor: state.HistoryCursor{
			Valid:           window.CursorValid,
			BeforeLineID:    window.CursorLineID,
			BeforeRowInLine: window.CursorRow,
			BeforeRowIndex:  window.CursorRowIndex,
			Segment:         window.CursorSegment,
		},
		HasMore:    window.HasMore,
		Generation: window.Generation,
		Boundary: state.HistoryBoundary{
			FirstLineID: window.FirstLineID,
			LastLineID:  window.LastLineID,
		},
		LoadedLines: window.LoadedLines,
		TotalLines:  window.LogicalTotal,
	}
}

func historySourceLinesFromRows(rows []state.HistoryRow, spans []state.HistoryLineSpan) []state.HistoryLogicalLine {
	if len(rows) == 0 {
		return nil
	}
	lines := make([]state.HistoryLogicalLine, 0, len(rows))
	for index, row := range rows {
		if len(lines) > 0 && sameHistorySource(lines[len(lines)-1], row) {
			lines[len(lines)-1].Text += row.Text
			lines[len(lines)-1].Cells = append(lines[len(lines)-1].Cells, row.Cells...)
			continue
		}
		span, _ := historySpanForRow(spans, index, row)
		lines = append(lines, state.HistoryLogicalLine{
			Text:               row.Text,
			Cells:              row.Cells,
			LineID:             row.LineID,
			Kind:               row.Kind,
			Segment:            row.Segment,
			SessionID:          row.SessionID,
			FrameID:            row.FrameID,
			FixedGrid:          row.FixedGrid,
			ScreenCols:         row.ScreenCols,
			ProjectionRowIndex: row.ProjectionRowIndex,
			TailFill:           row.TailFill,
			LiveTail:           row.LiveTail,
			ClippedBefore:      span.ClippedBefore,
			ClippedAfter:       span.ClippedAfter,
		})
	}
	return lines
}

func historySpanForRow(spans []state.HistoryLineSpan, rowIndex int, row state.HistoryRow) (state.HistoryLineSpan, bool) {
	for _, span := range spans {
		if span.LineID != 0 && row.LineID != 0 && span.LineID != row.LineID {
			continue
		}
		if rowIndex >= span.StartRow && rowIndex <= span.EndRow {
			return span, true
		}
	}
	return state.HistoryLineSpan{}, false
}

func sameHistorySource(line state.HistoryLogicalLine, row state.HistoryRow) bool {
	return line.LineID != 0 &&
		line.LineID == row.LineID &&
		line.Kind == row.Kind &&
		line.Segment == row.Segment &&
		line.SessionID == row.SessionID &&
		line.FrameID == row.FrameID &&
		line.FixedGrid == row.FixedGrid &&
		(!line.FixedGrid || line.ScreenCols == row.ScreenCols)
}

func historyTextAndCellsFromCompactRow(row protocol.CompactRow) (string, []state.HistoryCell) {
	cells := row.DecodeCells()
	if len(cells) == 0 {
		return row.Text, nil
	}
	out := make([]state.HistoryCell, 0, len(cells))
	var builder strings.Builder
	for _, cell := range cells {
		if cell.Content == "" {
			continue
		}
		out = append(out, state.HistoryCell{
			Text:       cell.Content,
			Width:      cell.Width,
			Style:      historyCellStyleFromProtocol(cell.Style),
			LinkURL:    cell.LinkURL,
			LinkParams: cell.LinkParams,
		})
		builder.WriteString(cell.Content)
	}
	if len(out) == 0 {
		return row.Text, nil
	}
	return builder.String(), out
}

func historyCellStyleFromProtocol(style protocol.CellStyle) state.HistoryCellStyle {
	return state.HistoryCellStyle{
		FG:            style.FG,
		BG:            style.BG,
		Bold:          style.Bold,
		Italic:        style.Italic,
		Underline:     style.Underline,
		Blink:         style.Blink,
		Reverse:       style.Reverse,
		Strikethrough: style.Strikethrough,
	}
}

func historyTailFillFromProtocol(style *protocol.CompactRowStyle) *state.HistoryCellStyle {
	if style == nil {
		return nil
	}
	out := state.HistoryCellStyle{
		FG:            style.FG,
		BG:            style.BG,
		Bold:          style.Bold,
		Italic:        style.Italic,
		Underline:     style.Underline,
		Blink:         style.Blink,
		Reverse:       style.Reverse,
		Strikethrough: style.Strikethrough,
	}
	if out == (state.HistoryCellStyle{}) {
		return nil
	}
	return &out
}

func historyRowIsLiveTail(window *protocol.HistoryWindow, index int) bool {
	if window == nil || index < 0 || index >= len(window.RowOwnership) {
		return false
	}
	return window.RowOwnership[index] == protocol.RowOwnershipLiveTailLive
}

func uint64At(values []uint64, index int) uint64 {
	if index < 0 || index >= len(values) {
		return 0
	}
	return values[index]
}

func intAt(values []int, index int) int {
	if index < 0 || index >= len(values) {
		return 0
	}
	return values[index]
}

func boolAt(values []bool, index int) bool {
	if index < 0 || index >= len(values) {
		return false
	}
	return values[index]
}

func stringAt(values []string, index int) string {
	if index < 0 || index >= len(values) {
		return ""
	}
	return values[index]
}

func firstNonZeroUint64(values ...uint64) uint64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func firstNonZeroInt(values ...int) int {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}
