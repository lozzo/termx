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

type fakeProtocolTerminalClient struct {
	attachParams   []protocol.AttachParams
	listCalls      int
	createParams   []protocol.CreateParams
	restartIDs     []string
	inputChannel   []uint16
	inputData      [][]byte
	resizeChannels []uint16
	resizes        []protocol.Size
	ensureParams   []protocol.EnsureResizeParams
	attachResult   *protocol.AttachResult
	listResult     *protocol.ListResult
	createResult   *protocol.CreateResult
}

func (client *fakeProtocolTerminalClient) AttachWithOptions(_ context.Context, params protocol.AttachParams) (*protocol.AttachResult, error) {
	client.attachParams = append(client.attachParams, params)
	if client.attachResult != nil {
		return client.attachResult, nil
	}
	return &protocol.AttachResult{Channel: 11}, nil
}

func (client *fakeProtocolTerminalClient) List(context.Context) (*protocol.ListResult, error) {
	client.listCalls++
	if client.listResult != nil {
		return client.listResult, nil
	}
	return &protocol.ListResult{}, nil
}

func (client *fakeProtocolTerminalClient) Create(_ context.Context, params protocol.CreateParams) (*protocol.CreateResult, error) {
	client.createParams = append(client.createParams, params)
	if client.createResult != nil {
		return client.createResult, nil
	}
	return &protocol.CreateResult{TerminalID: params.ID, State: "running"}, nil
}

func (client *fakeProtocolTerminalClient) Restart(_ context.Context, terminalID string) error {
	client.restartIDs = append(client.restartIDs, terminalID)
	return nil
}

func (client *fakeProtocolTerminalClient) Input(_ context.Context, channel uint16, data []byte) error {
	client.inputChannel = append(client.inputChannel, channel)
	client.inputData = append(client.inputData, append([]byte(nil), data...))
	return nil
}

func (client *fakeProtocolTerminalClient) Resize(_ context.Context, channel uint16, cols uint16, rows uint16) error {
	client.resizeChannels = append(client.resizeChannels, channel)
	client.resizes = append(client.resizes, protocol.Size{Cols: cols, Rows: rows})
	return nil
}

func (client *fakeProtocolTerminalClient) EnsureResize(_ context.Context, params protocol.EnsureResizeParams) (*protocol.EnsureResizeResult, error) {
	client.ensureParams = append(client.ensureParams, params)
	return &protocol.EnsureResizeResult{Size: protocol.Size{Cols: params.Cols, Rows: params.Rows}, Resized: true}, nil
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

func TestProtocolTerminalServiceAdapterMapsAttachInputAndResize(t *testing.T) {
	client := &fakeProtocolTerminalClient{
		attachResult: &protocol.AttachResult{
			Channel: 11,
			ResizeControl: &protocol.ResizeControl{
				CanResize: true,
				ResizeOwnership: &protocol.ResizeOwnership{
					Size: protocol.Size{Cols: 100, Rows: 40},
				},
			},
		},
	}
	adapter := ProtocolTerminalServiceAdapter{Client: client}

	attached, err := adapter.Attach(context.Background(), TerminalAttachRequest{
		TerminalID:   "term-1",
		Cols:         80,
		Rows:         24,
		Mode:         "collaborator",
		ResizePolicy: protocol.ResizePolicyOwner,
		SurfaceID:    "surface-1",
		ViewID:       "view-1",
	})
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if attached.Channel != 11 || attached.Cols != 100 || attached.Rows != 40 || !attached.CanResize {
		t.Fatalf("unexpected attach result %#v", attached)
	}
	params := client.attachParams[0]
	if params.TerminalID != "term-1" || params.SurfaceID != "surface-1" || params.ViewID != "view-1" {
		t.Fatalf("unexpected attach params %#v", params)
	}

	if err := adapter.SendInput(context.Background(), TerminalInputRequest{TerminalID: "term-1", Channel: 11, Bytes: []byte("x")}); err != nil {
		t.Fatalf("input: %v", err)
	}
	if len(client.inputData) != 1 || string(client.inputData[0]) != "x" || client.inputChannel[0] != 11 {
		t.Fatalf("unexpected input call channels=%#v data=%#v", client.inputChannel, client.inputData)
	}
	if err := adapter.Resize(context.Background(), TerminalResizeRequest{
		TerminalID: "term-1",
		Channel:    11,
		Cols:       120,
		Rows:       50,
		SurfaceID:  "surface-1",
		ViewID:     "view-1",
	}); err != nil {
		t.Fatalf("resize: %v", err)
	}
	if len(client.ensureParams) != 1 || client.ensureParams[0].Cols != 120 || client.ensureParams[0].Rows != 50 {
		t.Fatalf("unexpected ensure resize params %#v", client.ensureParams)
	}
}

func TestProtocolTerminalServiceAdapterMapsTerminalPoolActions(t *testing.T) {
	client := &fakeProtocolTerminalClient{
		listResult: &protocol.ListResult{Terminals: []protocol.TerminalInfo{{
			ID:    "term-pool",
			Name:  "日志🚀",
			State: "running",
			CWD:   "/tmp",
			Tags:  map[string]string{"role": "shell"},
		}}},
		createResult: &protocol.CreateResult{TerminalID: "term-new", State: "running"},
	}
	adapter := ProtocolTerminalServiceAdapter{Client: client}

	list, err := adapter.List(context.Background(), TerminalListRequest{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if client.listCalls != 1 || len(list.Items) != 1 || list.Items[0].TerminalID != "term-pool" || list.Items[0].Title != "日志🚀" || list.Items[0].Tags["role"] != "shell" {
		t.Fatalf("unexpected list mapping calls=%d result=%#v", client.listCalls, list)
	}

	created, err := adapter.Create(context.Background(), TerminalCreateRequest{TerminalID: "term-new", Title: "new", Command: []string{"sh"}, Cols: 100, Rows: 30})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.TerminalID != "term-new" || len(client.createParams) != 1 || client.createParams[0].Size.Cols != 100 || client.createParams[0].Name != "new" {
		t.Fatalf("unexpected create mapping created=%#v params=%#v", created, client.createParams)
	}
	if err := adapter.Restart(context.Background(), TerminalRestartRequest{TerminalID: "term-pool"}); err != nil {
		t.Fatalf("restart: %v", err)
	}
	if len(client.restartIDs) != 1 || client.restartIDs[0] != "term-pool" {
		t.Fatalf("unexpected restart ids %#v", client.restartIDs)
	}
}
