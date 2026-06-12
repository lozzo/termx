package services

import (
	"context"
	"errors"
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
	killIDs        []string
	removeIDs      []string
	metadataIDs    []string
	metadataNames  []string
	metadataTags   []map[string]string
	inputChannel   []uint16
	inputData      [][]byte
	resizeChannels []uint16
	resizes        []protocol.Size
	ensureParams   []protocol.EnsureResizeParams
	eventParams    []protocol.EventsParams
	eventCh        chan protocol.Event
	snapshotIDs    []string
	snapshotResult *protocol.Snapshot
	attachResult   *protocol.AttachResult
	listResult     *protocol.ListResult
	createResult   *protocol.CreateResult
	storageGets    []protocol.StorageGetParams
	storagePuts    []protocol.StoragePutParams
	storageEntry   *protocol.StorageEntry
	storagePutErr  error
}

func (client *fakeProtocolTerminalClient) AttachWithOptions(_ context.Context, params protocol.AttachParams) (*protocol.AttachResult, error) {
	client.attachParams = append(client.attachParams, params)
	if client.attachResult != nil {
		return client.attachResult, nil
	}
	return &protocol.AttachResult{Channel: 11}, nil
}

func (client *fakeProtocolTerminalClient) Events(_ context.Context, params protocol.EventsParams) (<-chan protocol.Event, error) {
	client.eventParams = append(client.eventParams, params)
	if client.eventCh == nil {
		client.eventCh = make(chan protocol.Event, 1)
	}
	return client.eventCh, nil
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

func (client *fakeProtocolTerminalClient) Kill(_ context.Context, terminalID string) error {
	client.killIDs = append(client.killIDs, terminalID)
	return nil
}

func (client *fakeProtocolTerminalClient) Remove(_ context.Context, terminalID string) error {
	client.removeIDs = append(client.removeIDs, terminalID)
	return nil
}

func (client *fakeProtocolTerminalClient) SetMetadata(_ context.Context, terminalID string, name string, tags map[string]string) error {
	client.metadataIDs = append(client.metadataIDs, terminalID)
	client.metadataNames = append(client.metadataNames, name)
	client.metadataTags = append(client.metadataTags, cloneStringMap(tags))
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
	return &protocol.EnsureResizeResult{
		Size:    protocol.Size{Cols: params.Cols, Rows: params.Rows},
		Resized: true,
		ResizeControl: &protocol.ResizeControl{
			CanResize:      true,
			Reason:         protocol.ResizeControlReasonOwner,
			SurfaceID:      params.SurfaceID,
			OwnerSurfaceID: params.SurfaceID,
			OwnerViewID:    params.ViewID,
			ResizeOwnership: &protocol.ResizeOwnership{
				OwnerSurfaceID: params.SurfaceID,
				OwnerViewID:    params.ViewID,
				Size:           protocol.Size{Cols: params.Cols, Rows: params.Rows},
				Epoch:          2,
			},
		},
	}, nil
}

func (client *fakeProtocolTerminalClient) Snapshot(_ context.Context, terminalID string, _ int, _ int) (*protocol.Snapshot, error) {
	client.snapshotIDs = append(client.snapshotIDs, terminalID)
	if client.snapshotResult != nil {
		return client.snapshotResult, nil
	}
	return &protocol.Snapshot{TerminalID: terminalID}, nil
}

func (client *fakeProtocolTerminalClient) StorageGet(_ context.Context, params protocol.StorageGetParams) (*protocol.StorageEntry, error) {
	client.storageGets = append(client.storageGets, params)
	if client.storageEntry != nil {
		return client.storageEntry, nil
	}
	return &protocol.StorageEntry{}, nil
}

func (client *fakeProtocolTerminalClient) StoragePut(_ context.Context, params protocol.StoragePutParams) (*protocol.StorageEntry, error) {
	client.storagePuts = append(client.storagePuts, params)
	if client.storagePutErr != nil {
		return nil, client.storagePutErr
	}
	return &protocol.StorageEntry{
		AppID:   params.AppID,
		Scope:   params.Scope,
		OwnerID: params.OwnerID,
		Key:     params.Key,
		Value:   append([]byte(nil), params.Value...),
		Version: params.ExpectedVersion + 1,
	}, nil
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

func TestProtocolCoreClientAdapterMapsStyledHistoryCells(t *testing.T) {
	client := &fakeProtocolHistoryClient{
		window: &protocol.HistoryWindow{
			TerminalID: "term-1",
			Token:      "tok-1",
			Op:         protocol.HistoryWindowReplace,
			Size:       protocol.Size{Cols: 80, Rows: 24},
			Rows: []protocol.CompactRow{protocol.CompactRowFromCells([]protocol.Cell{
				{Content: "ERR", Width: 3, Style: protocol.CellStyle{FG: "ansi:1", Bold: true}},
				{Content: " ", Width: 1},
				{Content: "好", Width: 2, Style: protocol.CellStyle{FG: "#ffcc00", Underline: true}, LinkURL: "file://build.log", LinkParams: "line=7"},
			})},
			RowLineIDs: []uint64{42},
			RowInLine:  []int{0},
		},
	}
	adapter := ProtocolCoreClientAdapter{Client: client}

	result, err := adapter.HistoryLatest(context.Background(), HistoryLatestRequest{RequestID: 1, TerminalID: "term-1", Cols: 80, Rows: 20})
	if err != nil {
		t.Fatalf("latest: %v", err)
	}

	row := result.Window.Rows[0]
	if row.Text != "ERR 好" || len(row.Cells) != 3 {
		t.Fatalf("adapter should keep styled cells and derived text, got %#v", row)
	}
	if row.Cells[0].Text != "ERR" || row.Cells[0].Width != 3 || row.Cells[0].Style.FG != "ansi:1" || !row.Cells[0].Style.Bold {
		t.Fatalf("lost first styled cell %#v", row.Cells[0])
	}
	if row.Cells[2].Text != "好" || row.Cells[2].Width != 2 || row.Cells[2].Style.FG != "#ffcc00" || !row.Cells[2].Style.Underline || row.Cells[2].LinkURL == "" || row.Cells[2].LinkParams == "" {
		t.Fatalf("lost wide linked cell %#v", row.Cells[2])
	}
}

func TestProtocolCoreClientAdapterPreservesTrailingBlankHistoryCells(t *testing.T) {
	client := &fakeProtocolHistoryClient{
		window: &protocol.HistoryWindow{
			TerminalID: "term-1",
			Token:      "tok-1",
			Op:         protocol.HistoryWindowReplace,
			Size:       protocol.Size{Cols: 80, Rows: 24},
			Rows: []protocol.CompactRow{protocol.CompactRowFromCellsPreserveTrailingBlankCells([]protocol.Cell{
				{Content: "cmd", Width: 3},
				{Content: " ", Width: 1},
				{Content: " ", Width: 1},
			}, true)},
			RowLineIDs: []uint64{42},
			RowInLine:  []int{0},
		},
	}
	adapter := ProtocolCoreClientAdapter{Client: client}

	result, err := adapter.HistoryLatest(context.Background(), HistoryLatestRequest{RequestID: 1, TerminalID: "term-1", Cols: 80, Rows: 20})
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	row := result.Window.Rows[0]
	if row.Text != "cmd  " || len(row.Cells) != 3 || row.Cells[1].Text != " " || row.Cells[2].Text != " " {
		t.Fatalf("adapter should preserve trailing blank history cells, got %#v", row)
	}
}

func TestProtocolCoreClientAdapterMergesSameLogicalLineRowsIntoFrozenSource(t *testing.T) {
	client := &fakeProtocolHistoryClient{
		window: &protocol.HistoryWindow{
			TerminalID: "term-1",
			Token:      "tok-1",
			Op:         protocol.HistoryWindowReplace,
			Size:       protocol.Size{Cols: 3, Rows: 24},
			Rows: []protocol.CompactRow{
				protocol.CompactRowFromCells([]protocol.Cell{{Content: "abc", Width: 3}}),
				protocol.CompactRowFromCells([]protocol.Cell{{Content: "def", Width: 3}}),
			},
			Lines:       []protocol.HistoryLineSpan{{LogicalLineID: 42, StartRow: 0, EndRow: 1}},
			RowLineIDs:  []uint64{42, 42},
			RowInLine:   []int{0, 1},
			LoadedLines: 1,
		},
	}
	adapter := ProtocolCoreClientAdapter{Client: client}

	result, err := adapter.HistoryLatest(context.Background(), HistoryLatestRequest{RequestID: 1, TerminalID: "term-1", Cols: 3, Rows: 20})
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if len(result.Window.SourceLines) != 1 || result.Window.SourceLines[0].LineID != 42 || result.Window.SourceLines[0].Text != "abcdef" {
		t.Fatalf("adapter should merge same-line protocol rows into one frozen source line, got %#v", result.Window.SourceLines)
	}
	if got := rowTextsForProtocolAdapter(result.Window.Rows); len(got) != 2 || got[0] != "abc" || got[1] != "def" {
		t.Fatalf("adapter should preserve visible rows at source cols, got %#v", result.Window.Rows)
	}

	reflowedRows, _ := state.ReflowHistoryLogicalLines(result.Window.SourceLines, 6)
	if got := rowTextsForProtocolAdapter(reflowedRows); len(got) != 1 || got[0] != "abcdef" {
		t.Fatalf("merged frozen source should support wider local reflow, got %#v", reflowedRows)
	}
}

func TestProtocolTerminalServiceAdapterMapsRemove(t *testing.T) {
	client := &fakeProtocolTerminalClient{}
	adapter := ProtocolTerminalServiceAdapter{Client: client}

	if err := adapter.Remove(context.Background(), TerminalRemoveRequest{TerminalID: "term-1"}); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if len(client.removeIDs) != 1 || client.removeIDs[0] != "term-1" {
		t.Fatalf("remove should call protocol remove, got %#v", client.removeIDs)
	}
}

func rowTextsForProtocolAdapter(rows []state.HistoryRow) []string {
	texts := make([]string, len(rows))
	for i, row := range rows {
		texts[i] = row.Text
	}
	return texts
}

func TestProtocolWorkbenchStorageAdapterUsesOpaqueStorageMethods(t *testing.T) {
	shell := state.DefaultShell().
		SplitActivePane(state.PaneState{ID: "pane-logs", Title: "logs", Kind: state.PaneTerminalLive, TerminalID: "term-logs"}, state.SplitDirectionVertical).
		FocusPane(state.PaneCommandTarget{PaneID: "pane-logs"})
	snapshot := state.SnapshotWorkbenchForStorage(shell)
	payload, err := state.EncodeWorkbenchStorageSnapshotValue(snapshot)
	if err != nil {
		t.Fatalf("encode fixture: %v", err)
	}
	client := &fakeProtocolTerminalClient{storageEntry: &protocol.StorageEntry{
		AppID:   state.WorkbenchStorageAppID,
		Scope:   protocol.StorageScopePublic,
		OwnerID: state.DefaultWorkspaceID,
		Key:     state.WorkbenchStorageKeyRoot,
		Value:   payload,
		Version: 9,
	}}
	adapter := ProtocolWorkbenchStorageAdapter{Client: client}
	ref := state.DefaultWorkbenchStorageRef("")

	loaded, err := adapter.LoadWorkbench(context.Background(), ref)
	if err != nil {
		t.Fatalf("load workbench: %v", err)
	}
	if !loaded.Found || loaded.Version != 9 || loaded.Snapshot.ActivePaneID != "pane-logs" {
		t.Fatalf("unexpected loaded snapshot %#v", loaded)
	}
	if len(client.storageGets) != 1 || client.storageGets[0].AppID != state.WorkbenchStorageAppID || client.storageGets[0].Key != state.WorkbenchStorageKeyRoot {
		t.Fatalf("adapter must use storage.get with TUI-owned key, got %#v", client.storageGets)
	}

	saved, err := adapter.SaveWorkbench(context.Background(), WorkbenchStorageSaveRequest{
		Ref:             ref,
		Snapshot:        snapshot,
		CheckVersion:    true,
		ExpectedVersion: 9,
	})
	if err != nil {
		t.Fatalf("save workbench: %v", err)
	}
	if saved.Version != 10 || saved.Ref.Version != 10 {
		t.Fatalf("unexpected save result %#v", saved)
	}
	if len(client.storagePuts) != 1 {
		t.Fatalf("expected one storage.put, got %#v", client.storagePuts)
	}
	put := client.storagePuts[0]
	if put.AppID != state.WorkbenchStorageAppID || put.Scope != protocol.StorageScopePublic || put.OwnerID != state.DefaultWorkspaceID || put.Key != state.WorkbenchStorageKeyRoot || !put.CheckVersion || put.ExpectedVersion != 9 {
		t.Fatalf("unexpected storage.put params %#v", put)
	}
	decoded, err := state.DecodeWorkbenchStorageSnapshot(put.Value)
	if err != nil {
		t.Fatalf("decode storage.put payload: %v", err)
	}
	if decoded.Schema != state.WorkbenchStorageSchema || decoded.ActivePaneID != "pane-logs" {
		t.Fatalf("unexpected storage payload %#v", decoded)
	}

	eventCh := make(chan protocol.Event, 1)
	client.eventCh = eventCh
	watch, err := adapter.WatchWorkbench(context.Background(), ref)
	if err != nil {
		t.Fatalf("watch workbench: %v", err)
	}
	if len(client.eventParams) != 1 {
		t.Fatalf("expected one events subscription, got %#v", client.eventParams)
	}
	filter := client.eventParams[0]
	if len(filter.Types) != 1 || filter.Types[0] != protocol.EventStorageChanged || filter.StorageAppID != state.WorkbenchStorageAppID || filter.StorageScope != protocol.StorageScopePublic || filter.StorageOwnerID != state.DefaultWorkspaceID || filter.StorageKeyPrefix != "workbench/" {
		t.Fatalf("watch must subscribe to storage.changed with workbench prefix, got %#v", filter)
	}
	eventCh <- protocol.Event{Type: protocol.EventStorageChanged, Storage: &protocol.StorageChangedData{
		AppID:   state.WorkbenchStorageAppID,
		Scope:   protocol.StorageScopePublic,
		OwnerID: state.DefaultWorkspaceID,
		Key:     state.WorkbenchStorageKeyRoot,
		Version: 11,
		Op:      "put",
	}}
	changed := <-watch
	if changed.Ref.Key != state.WorkbenchStorageKeyRoot || changed.Version != 11 || changed.Op != "put" {
		t.Fatalf("unexpected storage changed event %#v", changed)
	}
}

func TestProtocolWorkbenchStorageAdapterWrapsStorageConflict(t *testing.T) {
	client := &fakeProtocolTerminalClient{storagePutErr: errors.New("protocol error 400: storage version conflict")}
	adapter := ProtocolWorkbenchStorageAdapter{Client: client}

	_, err := adapter.SaveWorkbench(context.Background(), WorkbenchStorageSaveRequest{
		Ref:             state.DefaultWorkbenchStorageRef(""),
		Snapshot:        state.SnapshotWorkbenchForStorage(state.DefaultShell()),
		CheckVersion:    true,
		ExpectedVersion: 4,
	})
	if !errors.Is(err, ErrWorkbenchStorageConflict) {
		t.Fatalf("expected typed storage conflict, got %v", err)
	}
	if len(client.storagePuts) != 1 || !client.storagePuts[0].CheckVersion || client.storagePuts[0].ExpectedVersion != 4 {
		t.Fatalf("conflict path should still send CAS params, puts=%#v", client.storagePuts)
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
	resized, err := adapter.Resize(context.Background(), TerminalResizeRequest{
		TerminalID: "term-1",
		Channel:    11,
		Cols:       120,
		Rows:       50,
		SurfaceID:  "surface-1",
		ViewID:     "view-1",
	})
	if err != nil {
		t.Fatalf("resize: %v", err)
	}
	if !resized.Resized || !resized.CanResize || resized.ResizeEpoch != 2 || resized.OwnerViewID != "view-1" {
		t.Fatalf("unexpected resize result %#v", resized)
	}
	if len(client.ensureParams) != 1 || client.ensureParams[0].Cols != 120 || client.ensureParams[0].Rows != 50 {
		t.Fatalf("unexpected ensure resize params %#v", client.ensureParams)
	}
}

func TestProtocolTerminalServiceAdapterMapsLiveSurfaceSnapshot(t *testing.T) {
	client := &fakeProtocolTerminalClient{
		snapshotResult: &protocol.Snapshot{
			TerminalID: "term-1",
			Size:       protocol.Size{Cols: 12, Rows: 2},
			Screen: protocol.ScreenData{Cells: [][]protocol.Cell{
				{{Content: "$ "}, {Content: "你好", Width: 2, Style: protocol.CellStyle{FG: "ansi:2"}}, {Content: "🚀", Width: 2}},
				{{Content: "done"}},
			}},
			Cursor: protocol.CursorState{Visible: true, Row: 1, Col: 4, Shape: "bar"},
			Modes:  protocol.TerminalModes{MouseTracking: true, MouseButtonEvent: true, MouseSGR: true},
		},
	}
	adapter := ProtocolTerminalServiceAdapter{Client: client}

	result, err := adapter.LiveSurface(context.Background(), TerminalSurfaceRequest{
		TerminalID: "term-1",
		Cols:       12,
		Rows:       2,
	})
	if err != nil {
		t.Fatalf("live surface: %v", err)
	}

	if len(client.snapshotIDs) != 1 || client.snapshotIDs[0] != "term-1" {
		t.Fatalf("expected snapshot request, got %#v", client.snapshotIDs)
	}
	if !result.Ready || result.Snapshot.Cols != 12 || result.Snapshot.Rows != 2 || len(result.Snapshot.Lines) != 2 {
		t.Fatalf("unexpected live surface result %#v", result)
	}
	if result.Snapshot.Lines[0] != "$ 你好🚀" || result.Snapshot.Lines[1] != "done" {
		t.Fatalf("unexpected live surface lines %#v", result.Snapshot.Lines)
	}
	if len(result.Snapshot.Screen) != 2 || len(result.Snapshot.Screen[0]) != 3 || result.Snapshot.Screen[0][1].FG != "ansi:2" || result.Snapshot.Screen[0][1].Width != 2 {
		t.Fatalf("expected live surface cells and style to be preserved, got %#v", result.Snapshot.Screen)
	}
	if !result.Snapshot.Cursor.Visible || result.Snapshot.Cursor.Row != 1 || result.Snapshot.Cursor.Col != 4 || result.Snapshot.Cursor.Shape != "bar" {
		t.Fatalf("unexpected live cursor %#v", result.Snapshot.Cursor)
	}
	if !result.Snapshot.Modes.MousePassthroughEnabled() || !result.Snapshot.Modes.MouseButton || !result.Snapshot.Modes.MouseSGR {
		t.Fatalf("expected protocol terminal mouse modes to be preserved, got %#v", result.Snapshot.Modes)
	}
}

func TestProtocolTerminalServiceAdapterSkipsZeroWidthContinuationPlaceholders(t *testing.T) {
	client := &fakeProtocolTerminalClient{
		snapshotResult: &protocol.Snapshot{
			TerminalID: "term-1",
			Size:       protocol.Size{Cols: 8, Rows: 1},
			Screen: protocol.ScreenData{Cells: [][]protocol.Cell{
				{
					{Content: "♻️", Width: 2},
					{Content: "", Width: 0},
					{Content: "♻️", Width: 2},
					{Content: "", Width: 0},
					{Content: "♻️", Width: 2},
					{Content: "", Width: 0},
					{Content: "·", Width: 1},
					{Content: "·", Width: 1},
				},
			}},
		},
	}
	adapter := ProtocolTerminalServiceAdapter{Client: client}

	result, err := adapter.LiveSurface(context.Background(), TerminalSurfaceRequest{
		TerminalID: "term-1",
		Cols:       8,
		Rows:       1,
	})
	if err != nil {
		t.Fatalf("live surface: %v", err)
	}

	row := result.Snapshot.Screen[0]
	if len(row) != 5 {
		t.Fatalf("zero-width continuation placeholders should be dropped from live cells, got %#v", row)
	}
	for index, cell := range row[:3] {
		if cell.Text != "♻️" || cell.Width != 2 {
			t.Fatalf("emoji cell %d should keep protocol footprint, got %#v row=%#v", index, cell, row)
		}
	}
	if row[3].Text != "·" || row[3].Width != 1 || row[4].Text != "·" || row[4].Width != 1 {
		t.Fatalf("dots should stay immediately after the FE0F footprint, got %#v", row)
	}
}

func TestProtocolTerminalServiceAdapterMapsLiveEventsToSurfaceSnapshot(t *testing.T) {
	eventCh := make(chan protocol.Event, 1)
	client := &fakeProtocolTerminalClient{
		eventCh: eventCh,
		snapshotResult: &protocol.Snapshot{
			TerminalID: "term-1",
			Size:       protocol.Size{Cols: 80, Rows: 24},
			Screen: protocol.ScreenData{Cells: [][]protocol.Cell{
				{{Content: "backend", Width: 7, Style: protocol.CellStyle{FG: "ansi:3"}}},
				{{Content: "update"}},
			}},
		},
	}
	adapter := ProtocolTerminalServiceAdapter{Client: client}
	events, err := adapter.LiveEvents(context.Background(), TerminalLiveEventRequest{TerminalID: "term-1", Cols: 80, Rows: 24})
	if err != nil {
		t.Fatalf("live events: %v", err)
	}
	eventCh <- protocol.Event{Type: protocol.EventTerminalStateChanged, TerminalID: "term-1"}

	got := <-events
	if !got.Ready || got.Snapshot.TerminalID != "term-1" || len(got.Snapshot.Lines) != 2 || got.Snapshot.Lines[0] != "backend" || got.Snapshot.Lines[1] != "update" {
		t.Fatalf("unexpected live event %#v", got)
	}
	if len(got.Snapshot.Screen) != 2 || got.Snapshot.Screen[0][0].FG != "ansi:3" {
		t.Fatalf("expected live event to keep styled screen cells, got %#v", got.Snapshot.Screen)
	}
	if len(client.eventParams) != 1 || client.eventParams[0].TerminalID != "term-1" {
		t.Fatalf("expected protocol events subscription, got %#v", client.eventParams)
	}
	if len(client.snapshotIDs) != 1 || client.snapshotIDs[0] != "term-1" {
		t.Fatalf("expected live event to refresh snapshot, got %#v", client.snapshotIDs)
	}
}

func TestProtocolTerminalServiceAdapterMapsTerminalPoolActions(t *testing.T) {
	client := &fakeProtocolTerminalClient{
		listResult: &protocol.ListResult{Terminals: []protocol.TerminalInfo{{
			ID:    "term-pool",
			Name:  "日志🚀",
			State: "running",
			CWD:   "/tmp",
			Size:  protocol.Size{Cols: 120, Rows: 36},
			Tags:  map[string]string{"role": "shell"},
		}}},
		createResult: &protocol.CreateResult{TerminalID: "term-new", State: "running"},
	}
	adapter := ProtocolTerminalServiceAdapter{Client: client}

	list, err := adapter.List(context.Background(), TerminalListRequest{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if client.listCalls != 1 || len(list.Items) != 1 || list.Items[0].TerminalID != "term-pool" || list.Items[0].Title != "日志🚀" || list.Items[0].Tags["role"] != "shell" || list.Items[0].Cols != 120 || list.Items[0].Rows != 36 {
		t.Fatalf("unexpected list mapping calls=%d result=%#v", client.listCalls, list)
	}

	created, err := adapter.Create(context.Background(), TerminalCreateRequest{TerminalID: "term-new", Title: "new", Command: []string{"sh"}, CWD: "/tmp/app", Tags: map[string]string{"role": "dev"}, Cols: 100, Rows: 30})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.TerminalID != "term-new" || len(client.createParams) != 1 || client.createParams[0].Size.Cols != 100 || client.createParams[0].Name != "new" || client.createParams[0].Dir != "/tmp/app" || client.createParams[0].Tags["role"] != "dev" {
		t.Fatalf("unexpected create mapping created=%#v params=%#v", created, client.createParams)
	}
	if _, err := adapter.Create(context.Background(), TerminalCreateRequest{TerminalID: "term-default", Title: "default", Cols: 80, Rows: 24}); err != nil {
		t.Fatalf("create with default command: %v", err)
	}
	if len(client.createParams) != 2 || len(client.createParams[1].Command) == 0 {
		t.Fatalf("adapter must not send empty create command, params=%#v", client.createParams)
	}
	if err := adapter.Restart(context.Background(), TerminalRestartRequest{TerminalID: "term-pool"}); err != nil {
		t.Fatalf("restart: %v", err)
	}
	if len(client.restartIDs) != 1 || client.restartIDs[0] != "term-pool" {
		t.Fatalf("unexpected restart ids %#v", client.restartIDs)
	}
	if err := adapter.Kill(context.Background(), TerminalKillRequest{TerminalID: "term-pool"}); err != nil {
		t.Fatalf("kill: %v", err)
	}
	if len(client.killIDs) != 1 || client.killIDs[0] != "term-pool" {
		t.Fatalf("unexpected kill ids %#v", client.killIDs)
	}
	if err := adapter.EditMetadata(context.Background(), TerminalEditMetadataRequest{TerminalID: "term-pool", Title: "renamed", Tags: map[string]string{"role": "ops"}}); err != nil {
		t.Fatalf("edit metadata: %v", err)
	}
	if len(client.metadataIDs) != 1 || client.metadataIDs[0] != "term-pool" || client.metadataNames[0] != "renamed" || client.metadataTags[0]["role"] != "ops" {
		t.Fatalf("unexpected metadata calls ids=%#v names=%#v tags=%#v", client.metadataIDs, client.metadataNames, client.metadataTags)
	}
}
