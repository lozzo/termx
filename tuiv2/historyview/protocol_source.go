package historyview

import (
	"context"
	"errors"
	"time"

	"github.com/lozzow/termx/internal/protocol"
)

type ProtocolClient interface {
	Snapshot(ctx context.Context, terminalID string, offset, limit int) (*protocol.Snapshot, error)
	HistoryWindow(ctx context.Context, params protocol.HistoryWindowParams) (*protocol.HistoryWindow, error)
}

type ProtocolSource struct {
	client ProtocolClient
}

func NewProtocolSource(client ProtocolClient) *ProtocolSource {
	return &ProtocolSource{client: client}
}

func (s *ProtocolSource) LiveSurface(ctx context.Context, terminalID string) (LiveSurface, error) {
	if s == nil || s.client == nil {
		return LiveSurface{}, errors.New("historyview: nil protocol source client")
	}
	snapshot, err := s.client.Snapshot(ctx, terminalID, 0, 0)
	if err != nil {
		return LiveSurface{}, err
	}
	if snapshot == nil {
		return LiveSurface{}, errors.New("historyview: nil live surface snapshot")
	}
	out := LiveSurface{
		TerminalID: snapshot.TerminalID,
		Size:       snapshot.Size,
		Screen: protocol.ScreenData{
			Cells:             cloneProtocolRows(snapshot.Screen.Cells),
			IsAlternateScreen: snapshot.Screen.IsAlternateScreen,
		},
		Cursor:    snapshot.Cursor,
		Modes:     snapshot.Modes,
		Timestamp: snapshot.Timestamp,
	}
	if out.TerminalID == "" {
		out.TerminalID = terminalID
	}
	return out, nil
}

func (s *ProtocolSource) LatestHistoryWindow(ctx context.Context, request WindowRequest) (HistoryWindow, error) {
	return s.historyWindow(ctx, request, 0)
}

func (s *ProtocolSource) OlderHistoryWindow(ctx context.Context, request WindowRequest) (HistoryWindow, error) {
	if request.BeforeCursor <= 0 {
		return HistoryWindow{}, errors.New("historyview: older history window requires before cursor")
	}
	return s.historyWindow(ctx, request, request.BeforeCursor)
}

func (s *ProtocolSource) historyWindow(ctx context.Context, request WindowRequest, beforeOffset int) (HistoryWindow, error) {
	if s == nil || s.client == nil {
		return HistoryWindow{}, errors.New("historyview: nil protocol source client")
	}
	window, err := s.client.HistoryWindow(ctx, protocol.HistoryWindowParams{
		TerminalID:   request.TerminalID,
		BeforeOffset: beforeOffset,
		Limit:        request.Limit,
		Cols:         request.Cols,
	})
	if err != nil {
		return HistoryWindow{}, err
	}
	if window == nil {
		return HistoryWindow{}, errors.New("historyview: nil history window")
	}
	return historyWindowFromProtocol(window), nil
}

func historyWindowFromProtocol(window *protocol.HistoryWindow) HistoryWindow {
	if window == nil {
		return HistoryWindow{}
	}
	rows := make([]HistoryRow, len(window.Rows))
	for i, row := range window.Rows {
		rows[i] = HistoryRow{
			Cells:     protocol.CloneCompactRow(row),
			Kind:      historyRowKind(historyStringAt(window.RowOwnership, i), historyStringAt(window.RowKinds, i)),
			Wrapped:   historyBoolAt(window.RowWrapped, i),
			Timestamp: historyTimeAt(window.RowTimestamps, i),
		}
	}
	lines := make([]LineSpan, len(window.Lines))
	for i, span := range window.Lines {
		lines[i] = LineSpan{
			StartRow:       span.StartRow,
			EndRow:         span.EndRow,
			Kind:           historyLineKind(span, window),
			LogicalLineID:  span.LogicalLineID,
			TimestampStart: span.TimestampStart,
			TimestampEnd:   span.TimestampEnd,
			ClippedBefore:  span.ClippedBefore,
			ClippedAfter:   span.ClippedAfter,
		}
	}
	return HistoryWindow{
		TerminalID:      window.TerminalID,
		Token:           WindowToken(window.Token),
		Op:              WindowOp(window.Op),
		Size:            window.Size,
		Rows:            rows,
		Lines:           lines,
		BeforeCursor:    window.BeforeOffset,
		LoadedRows:      window.LoadedRows,
		TotalRows:       window.TotalRows,
		LoadedLines:     historyWindowLoadedLines(window, historyLoadedLineStarts(lines)),
		TotalLines:      historyWindowTotalLines(window, len(lines)),
		HasMore:         window.HasMore,
		Generation:      window.Generation,
		FirstLineID:     firstNonZero(window.FirstLineID, firstLineID(lines)),
		LastLineID:      firstNonZero(window.LastLineID, lastLineID(lines)),
		FirstBoundaryID: window.FirstRowID,
		LastBoundaryID:  window.LastRowID,
		Timestamp:       window.Timestamp,
	}
}

func historyRowKind(ownership, rowKind string) RowKind {
	if ownership != "" {
		return RowKind(ownership)
	}
	return RowKind(rowKind)
}

func historyWindowLoadedLines(window *protocol.HistoryWindow, loadedLines int) int {
	if window == nil || window.LoadedLines <= 0 {
		return loadedLines
	}
	return window.LoadedLines
}

func historyLineKind(span protocol.HistoryLineSpan, window *protocol.HistoryWindow) RowKind {
	if span.RowKind != "" {
		return RowKind(span.RowKind)
	}
	if window == nil {
		return ""
	}
	index := span.StartRow
	if index < 0 {
		index = 0
	}
	return historyRowKind(historyStringAt(window.RowOwnership, index), historyStringAt(window.RowKinds, index))
}

func historyWindowTotalLines(window *protocol.HistoryWindow, loadedLines int) int {
	if window == nil || window.LogicalTotal <= 0 {
		return loadedLines
	}
	return window.LogicalTotal
}

func firstLineID(lines []LineSpan) uint64 {
	if len(lines) == 0 {
		return 0
	}
	for _, line := range lines {
		if line.ClippedBefore {
			continue
		}
		if line.LogicalLineID != 0 {
			return line.LogicalLineID
		}
	}
	return 0
}

func lastLineID(lines []LineSpan) uint64 {
	if len(lines) == 0 {
		return 0
	}
	for i := len(lines) - 1; i >= 0; i-- {
		if lines[i].ClippedBefore {
			continue
		}
		if lines[i].LogicalLineID != 0 {
			return lines[i].LogicalLineID
		}
	}
	return 0
}

func historyLoadedLineStarts(lines []LineSpan) int {
	count := 0
	for _, line := range lines {
		if !line.ClippedBefore {
			count++
		}
	}
	return count
}

func historyStringAt(values []string, index int) string {
	if index < 0 || index >= len(values) {
		return ""
	}
	return values[index]
}

func historyBoolAt(values []bool, index int) bool {
	if index < 0 || index >= len(values) {
		return false
	}
	return values[index]
}

func historyTimeAt(values []time.Time, index int) time.Time {
	if index < 0 || index >= len(values) {
		return time.Time{}
	}
	return values[index]
}
