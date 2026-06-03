package services

import (
	"context"
	"strings"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

type ProtocolHistoryClient interface {
	HistoryWindow(context.Context, protocol.HistoryWindowParams) (*protocol.HistoryWindow, error)
}

// ProtocolCoreClientAdapter 是真实 protocol history.window 的 service adapter。
type ProtocolCoreClientAdapter struct {
	Client ProtocolHistoryClient
}

func (adapter ProtocolCoreClientAdapter) HistoryLatest(ctx context.Context, req HistoryLatestRequest) (HistoryResult, error) {
	window, err := adapter.historyWindow(ctx, protocol.HistoryWindowParams{
		TerminalID: req.TerminalID,
		Limit:      req.Rows,
		Cols:       req.Cols,
	})
	if err != nil {
		return HistoryResult{}, err
	}
	return HistoryResult{RequestID: req.RequestID, Window: window}, nil
}

func (adapter ProtocolCoreClientAdapter) HistoryOlder(ctx context.Context, req HistoryOlderRequest) (HistoryResult, error) {
	window, err := adapter.historyWindow(ctx, protocol.HistoryWindowParams{
		TerminalID:          req.TerminalID,
		Limit:               req.Rows,
		Cols:                req.Cols,
		Token:               req.Token,
		Generation:          req.Generation,
		CursorValid:         req.Cursor.Valid,
		BeforeLineID:        req.Cursor.BeforeLineID,
		BeforeRowInLine:     req.Cursor.BeforeRowInLine,
		BoundaryFirstLineID: req.Boundary.FirstLineID,
		BoundaryLastLineID:  req.Boundary.LastLineID,
	})
	if err != nil {
		return HistoryResult{}, err
	}
	return HistoryResult{RequestID: req.RequestID, Window: window}, nil
}

func (adapter ProtocolCoreClientAdapter) historyWindow(ctx context.Context, params protocol.HistoryWindowParams) (state.HistoryWindow, error) {
	window, err := adapter.Client.HistoryWindow(ctx, params)
	if err != nil {
		return state.HistoryWindow{}, err
	}
	return historyWindowFromProtocol(window), nil
}

func historyWindowFromProtocol(window *protocol.HistoryWindow) state.HistoryWindow {
	if window == nil {
		return state.HistoryWindow{}
	}
	rows := make([]state.HistoryRow, len(window.Rows))
	for i, row := range window.Rows {
		rows[i] = state.HistoryRow{
			Text:      protocolRowText(row),
			LineID:    uint64At(window.RowLineIDs, i),
			RowInLine: intAt(window.RowInLine, i),
		}
	}
	lines := make([]state.HistoryLineSpan, len(window.Lines))
	for i, span := range window.Lines {
		lines[i] = state.HistoryLineSpan{
			LineID:        span.LogicalLineID,
			StartRow:      span.StartRow,
			EndRow:        span.EndRow,
			ClippedBefore: span.ClippedBefore,
			ClippedAfter:  span.ClippedAfter,
		}
	}
	return state.HistoryWindow{
		TerminalID: window.TerminalID,
		Token:      window.Token,
		Op:         state.HistoryWindowOp(window.Op),
		Cols:       int(window.Size.Cols),
		Rows:       rows,
		Lines:      lines,
		Cursor: state.HistoryCursor{
			Valid:           window.CursorValid,
			BeforeLineID:    window.CursorLineID,
			BeforeRowInLine: window.CursorRow,
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

func protocolRowText(row protocol.CompactRow) string {
	var builder strings.Builder
	for _, cell := range row.DecodeCells() {
		builder.WriteString(cell.Content)
	}
	return builder.String()
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
