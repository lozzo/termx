package services

import (
	"context"
	"testing"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

type fakeProtocolHistoryClient struct {
	requests []protocol.HistoryWindowParams
	window   *protocol.HistoryWindow
}

func (client *fakeProtocolHistoryClient) HistoryWindow(_ context.Context, params protocol.HistoryWindowParams) (*protocol.HistoryWindow, error) {
	client.requests = append(client.requests, params)
	return client.window, nil
}

func TestProtocolCoreClientAdapterMapsLatestAndOlder(t *testing.T) {
	client := &fakeProtocolHistoryClient{
		window: &protocol.HistoryWindow{
			TerminalID:   "term-1",
			Token:        "tok-1",
			Op:           protocol.HistoryWindowReplace,
			Size:         protocol.Size{Cols: 80, Rows: 24},
			Rows:         []protocol.CompactRow{protocol.CompactRowFromCells([]protocol.Cell{{Content: "h"}, {Content: "i"}})},
			Lines:        []protocol.HistoryLineSpan{{LogicalLineID: 42, StartRow: 0, EndRow: 0}},
			RowLineIDs:   []uint64{42},
			RowInLine:    []int{0},
			CursorValid:  true,
			CursorLineID: 42,
			CursorRow:    1,
			HasMore:      true,
			Generation:   7,
			FirstLineID:  42,
			LastLineID:   43,
			LoadedLines:  1,
			LogicalTotal: 2,
		},
	}
	adapter := ProtocolCoreClientAdapter{Client: client}

	latest, err := adapter.HistoryLatest(context.Background(), HistoryLatestRequest{RequestID: 1, TerminalID: "term-1", Cols: 80, Rows: 20})
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if latest.RequestID != 1 || latest.Window.Rows[0].Text != "hi" || latest.Window.Rows[0].LineID != 42 {
		t.Fatalf("unexpected latest result %#v", latest)
	}
	if len(client.requests) != 1 || client.requests[0].Token != "" || client.requests[0].Cols != 80 {
		t.Fatalf("unexpected latest params %#v", client.requests)
	}

	client.window.Op = protocol.HistoryWindowPrepend
	older, err := adapter.HistoryOlder(context.Background(), HistoryOlderRequest{
		RequestID:  2,
		TerminalID: "term-1",
		Cols:       80,
		Rows:       10,
		Token:      "tok-1",
		Generation: 7,
		Cursor:     state.HistoryCursor{Valid: true, BeforeLineID: 42, BeforeRowInLine: 1},
		Boundary:   state.HistoryBoundary{FirstLineID: 42, LastLineID: 43},
	})
	if err != nil {
		t.Fatalf("older: %v", err)
	}
	if older.RequestID != 2 || older.Window.Op != state.HistoryWindowPrepend {
		t.Fatalf("unexpected older result %#v", older)
	}
	params := client.requests[1]
	if params.Token != "tok-1" || params.Generation != 7 || !params.CursorValid || params.BeforeLineID != 42 || params.BeforeRowInLine != 1 || params.BoundaryLastLineID != 43 {
		t.Fatalf("unexpected older params %#v", params)
	}
}
