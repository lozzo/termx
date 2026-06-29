package services

import (
	"context"
	"errors"
	"testing"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

type fakeProtocolHistoryClient struct {
	requests        []protocol.HistoryWindowParams
	copyRequests    []protocol.HistoryWindowParams
	releaseRequests []protocol.HistoryWindowParams
	window          *protocol.HistoryWindow
	windowErr       error
	copyText        string
	copyErr         error
	releaseErr      error
}

func (client *fakeProtocolHistoryClient) HistoryWindow(_ context.Context, params protocol.HistoryWindowParams) (*protocol.HistoryWindow, error) {
	client.requests = append(client.requests, params)
	if client.windowErr != nil {
		return nil, client.windowErr
	}
	return client.window, nil
}

func (client *fakeProtocolHistoryClient) HistoryCopy(_ context.Context, params protocol.HistoryWindowParams) (string, error) {
	client.copyRequests = append(client.copyRequests, params)
	if client.copyErr != nil {
		return "", client.copyErr
	}
	return client.copyText, nil
}

func (client *fakeProtocolHistoryClient) ReleaseHistory(_ context.Context, params protocol.HistoryWindowParams) error {
	client.releaseRequests = append(client.releaseRequests, params)
	return client.releaseErr
}

func TestProtocolCoreClientAdapterMapsHistoryLatestWindow(t *testing.T) {
	row := protocol.CompactRowFromCellsPreserveTrailingBlankCells([]protocol.Cell{{
		Content: "h",
		Width:   1,
		Style: protocol.CellStyle{
			Bold: true,
			FG:   "red",
		},
		LinkURL: "https://example.test",
	}, {
		Content: "i",
		Width:   1,
	}}, true)
	row.TailFill = &protocol.CompactRowStyle{BG: "blue"}
	client := &fakeProtocolHistoryClient{window: &protocol.HistoryWindow{
		TerminalID:     "term-1",
		Token:          "tok-1",
		Op:             protocol.HistoryWindowReplace,
		Size:           protocol.Size{Cols: 80, Rows: 24},
		Rows:           []protocol.CompactRow{row},
		RowLineIDs:     []uint64{42},
		RowInLine:      []int{0},
		RowKinds:       []string{"output"},
		RowSegments:    []string{protocol.HistoryCursorSegmentCommitted},
		RowSessionIDs:  []uint64{2},
		RowFrameIDs:    []uint64{3},
		RowFixedGrid:   []bool{true},
		RowScreenCols:  []int{80},
		RowIndexes:     []int{7},
		RowOwnership:   []string{protocol.RowOwnershipLiveTailLive},
		Lines:          []protocol.HistoryLineSpan{{StartRow: 0, EndRow: 0, LogicalLineID: 42, RowKind: "output", SessionID: 2, FrameID: 3, FixedGrid: true, ScreenCols: 80}},
		CursorValid:    true,
		CursorLineID:   42,
		CursorRow:      0,
		CursorRowIndex: 7,
		CursorSegment:  protocol.HistoryCursorSegmentCommitted,
		FirstLineID:    42,
		LastLineID:     42,
		HasMore:        true,
		Generation:     9,
		LoadedLines:    1,
		LogicalTotal:   100,
	}}
	adapter := ProtocolCoreClientAdapter{Client: client}

	result, err := adapter.HistoryLatest(context.Background(), HistoryLatestRequest{
		RequestID:          5,
		PaneID:             "pane-main",
		ViewID:             "pane:pane-main",
		TerminalID:         "term-1",
		Cols:               80,
		Rows:               24,
		GenerationBoundary: 8,
	})
	if err != nil {
		t.Fatalf("history latest: %v", err)
	}
	if len(client.requests) != 1 {
		t.Fatalf("expected one history.window call, got %#v", client.requests)
	}
	req := client.requests[0]
	if req.TerminalID != "term-1" || req.Limit != 24 || req.Cols != 80 || req.Generation != 8 || req.Mode != "" {
		t.Fatalf("latest request not mapped: %#v", req)
	}
	if result.RequestID != 5 {
		t.Fatalf("request id not preserved: %#v", result)
	}
	window := result.Window
	if window.PaneID != "pane-main" || window.ViewID != "pane:pane-main" || window.TerminalID != "term-1" || window.Token != "tok-1" {
		t.Fatalf("window identity not mapped: %#v", window)
	}
	if window.Op != state.HistoryWindowReplace || window.Cols != 80 || !window.HasMore || window.Generation != 9 {
		t.Fatalf("window metadata not mapped: %#v", window)
	}
	if window.Cursor.BeforeLineID != 42 || window.Cursor.BeforeRowIndex != 7 || window.Boundary.FirstLineID != 42 || window.Boundary.LastLineID != 42 {
		t.Fatalf("cursor/boundary not mapped: %#v", window)
	}
	if len(window.Rows) != 1 || window.Rows[0].Text != "hi" || !window.Rows[0].LiveTail || window.Rows[0].TailFill == nil || window.Rows[0].TailFill.BG != "blue" {
		t.Fatalf("row payload not mapped: %#v", window.Rows)
	}
	if len(window.Rows[0].Cells) != 2 || !window.Rows[0].Cells[0].Style.Bold || window.Rows[0].Cells[0].LinkURL == "" {
		t.Fatalf("cell payload not mapped: %#v", window.Rows[0].Cells)
	}
	if len(window.SourceLines) != 1 || window.SourceLines[0].Text != "hi" || !window.SourceLines[0].LiveTail {
		t.Fatalf("source lines not derived from authoritative rows: %#v", window.SourceLines)
	}
}

func TestProtocolCoreClientAdapterMapsHistoryOlderCopyAndRelease(t *testing.T) {
	client := &fakeProtocolHistoryClient{
		window: &protocol.HistoryWindow{
			TerminalID:     "term-1",
			Token:          "tok-1",
			Op:             protocol.HistoryWindowPrepend,
			Size:           protocol.Size{Cols: 80},
			CursorValid:    true,
			CursorLineID:   4,
			CursorRowIndex: 1,
			Generation:     3,
		},
		copyText: "selected text",
	}
	adapter := ProtocolCoreClientAdapter{Client: client}

	_, err := adapter.HistoryOlder(context.Background(), HistoryOlderRequest{
		RequestID:  6,
		PaneID:     "pane-main",
		ViewID:     "pane:pane-main",
		TerminalID: "term-1",
		Cols:       80,
		Rows:       10,
		Token:      "tok-1",
		Generation: 3,
		Cursor: state.HistoryCursor{
			Valid:           true,
			BeforeLineID:    5,
			BeforeRowInLine: 2,
			BeforeRowIndex:  9,
			Segment:         state.HistoryCursorSegmentCommitted,
		},
		Boundary: state.HistoryBoundary{FirstLineID: 5, LastLineID: 9},
	})
	if err != nil {
		t.Fatalf("history older: %v", err)
	}
	if len(client.requests) != 1 {
		t.Fatalf("expected one history.window call, got %#v", client.requests)
	}
	req := client.requests[0]
	if req.Mode != string(state.HistoryRequestOlder) || req.Token != "tok-1" || req.BeforeLineID != 5 || req.BeforeRowInLine != 2 || req.BeforeRowIndex != 9 || req.BoundaryFirstLineID != 5 || req.BoundaryLastLineID != 9 {
		t.Fatalf("older request not mapped: %#v", req)
	}

	copyResult, err := adapter.HistoryCopyRange(context.Background(), HistoryCopyRangeRequest{
		TerminalID: "term-1",
		Cols:       80,
		Token:      "tok-1",
		Generation: 3,
		Boundary:   state.HistoryBoundary{FirstLineID: 5, LastLineID: 9},
		Start:      state.CopyLogicalPosition{Valid: true, LineID: 5, Col: 1},
		End:        state.CopyLogicalPosition{Valid: true, LineID: 9, Col: 4},
	})
	if err != nil {
		t.Fatalf("history copy: %v", err)
	}
	if copyResult.Text != "selected text" {
		t.Fatalf("copy text not returned: %#v", copyResult)
	}
	if len(client.copyRequests) != 1 {
		t.Fatalf("expected one history.copy call, got %#v", client.copyRequests)
	}
	copyReq := client.copyRequests[0]
	if !copyReq.RangeValid || copyReq.RangeStartLineID != 5 || copyReq.RangeStartCol != 1 || copyReq.RangeEndLineID != 9 || copyReq.RangeEndCol != 4 || copyReq.BoundaryLastLineID != 9 {
		t.Fatalf("copy request not mapped: %#v", copyReq)
	}

	if err := adapter.ReleaseHistory(context.Background(), HistoryReleaseRequest{TerminalID: "term-1", Token: "tok-1"}); err != nil {
		t.Fatalf("release history: %v", err)
	}
	if len(client.releaseRequests) != 1 || client.releaseRequests[0].TerminalID != "term-1" || client.releaseRequests[0].Token != "tok-1" {
		t.Fatalf("release request not mapped: %#v", client.releaseRequests)
	}
}

func TestProtocolCoreClientAdapterNormalizesStaleHistoryError(t *testing.T) {
	client := &fakeProtocolHistoryClient{windowErr: errors.New("protocol error: stale history window")}
	adapter := ProtocolCoreClientAdapter{Client: client}

	_, err := adapter.HistoryLatest(context.Background(), HistoryLatestRequest{TerminalID: "term-1", Cols: 80, Rows: 10})
	if !errors.Is(err, ErrStaleHistoryWindow) {
		t.Fatalf("stale protocol error must be normalized, got %v", err)
	}
}
