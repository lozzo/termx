package services

import (
	"context"
	"errors"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	return nil
}

type fakeProtocolTerminalClient struct {
	attachParams       []protocol.AttachParams
	detachParams       []protocol.DetachParams
	listCalls          int
	createParams       []protocol.CreateParams
	restartIDs         []string
	killIDs            []string
	removeIDs          []string
	metadataIDs        []string
	metadataNames      []string
	metadataTags       []map[string]string
	tagIDs             []string
	tagSets            []map[string]string
	inputChannel       []uint16
	inputData          [][]byte
	inputParams        []protocol.InputParams
	resizeChannels     []uint16
	resizes            []protocol.Size
	ensureParams       []protocol.EnsureResizeParams
	eventParams        []protocol.EventsParams
	eventCh            chan protocol.Event
	eventSubscribers   []fakeProtocolEventSubscriber
	eventFanoutStarted bool
	snapshotIDs        []string
	snapshotResult     *protocol.Snapshot
	snapshotResults    map[string]*protocol.Snapshot
	attachResult       *protocol.AttachResult
	listResult         *protocol.ListResult
	createResult       *protocol.CreateResult
	storageGets        []protocol.StorageGetParams
	storagePuts        []protocol.StoragePutParams
	storageEntry       *protocol.StorageEntry
	storageGetErr      error
	storagePutErr      error
}

type fakeProtocolEventSubscriber struct {
	params protocol.EventsParams
	ch     chan protocol.Event
}

func (client *fakeProtocolTerminalClient) AttachWithOptions(_ context.Context, params protocol.AttachParams) (*protocol.AttachResult, error) {
	client.attachParams = append(client.attachParams, params)
	if client.attachResult != nil {
		return client.attachResult, nil
	}
	return &protocol.AttachResult{Channel: 11}, nil
}

func (client *fakeProtocolTerminalClient) Detach(_ context.Context, params protocol.DetachParams) error {
	client.detachParams = append(client.detachParams, params)
	return nil
}

func (client *fakeProtocolTerminalClient) Events(_ context.Context, params protocol.EventsParams) (<-chan protocol.Event, error) {
	client.eventParams = append(client.eventParams, params)
	ch := make(chan protocol.Event, 8)
	client.eventSubscribers = append(client.eventSubscribers, fakeProtocolEventSubscriber{params: params, ch: ch})
	if client.eventCh == nil {
		client.eventCh = make(chan protocol.Event, 8)
	}
	if !client.eventFanoutStarted {
		client.eventFanoutStarted = true
		go func() {
			for event := range client.eventCh {
				client.publishEvent(event)
			}
			for _, sub := range client.eventSubscribers {
				close(sub.ch)
			}
		}()
	}
	return ch, nil
}

func (client *fakeProtocolTerminalClient) publishEvent(event protocol.Event) {
	for _, sub := range client.eventSubscribers {
		if !fakeProtocolEventMatches(event, sub.params) {
			continue
		}
		select {
		case sub.ch <- event:
		default:
		}
	}
}

func fakeProtocolEventMatches(event protocol.Event, params protocol.EventsParams) bool {
	if params.TerminalID != "" && params.TerminalID != event.TerminalID {
		return false
	}
	if (event.Storage != nil || fakeHasStorageEventParams(params)) && !fakeProtocolStorageEventMatches(event.Storage, params) {
		return false
	}
	if (event.Workbench != nil || params.WorkbenchID != "") && !fakeProtocolWorkbenchEventMatches(event.Workbench, params) {
		return false
	}
	if len(params.Types) == 0 {
		return true
	}
	for _, typ := range params.Types {
		if typ == event.Type {
			return true
		}
	}
	return false
}

func fakeHasStorageEventParams(params protocol.EventsParams) bool {
	return params.StorageAppID != "" || params.StorageScope != "" || params.StorageOwnerID != "" || params.StorageKeyPrefix != ""
}

func fakeProtocolStorageEventMatches(storage *protocol.StorageChangedData, params protocol.EventsParams) bool {
	if storage == nil {
		return params.StorageAppID == "" && params.StorageScope == "" && params.StorageOwnerID == "" && params.StorageKeyPrefix == ""
	}
	if params.StorageAppID != "" && params.StorageAppID != storage.AppID {
		return false
	}
	if params.StorageScope != "" && params.StorageScope != storage.Scope {
		return false
	}
	if params.StorageOwnerID != "" && params.StorageOwnerID != storage.OwnerID {
		return false
	}
	if params.StorageKeyPrefix != "" && !strings.HasPrefix(storage.Key, params.StorageKeyPrefix) {
		return false
	}
	return true
}

func fakeProtocolWorkbenchEventMatches(workbench *protocol.WorkbenchChangedData, params protocol.EventsParams) bool {
	if workbench == nil {
		return params.WorkbenchID == ""
	}
	return params.WorkbenchID == "" || params.WorkbenchID == workbench.WorkspaceID || params.WorkbenchID == workbench.ResourceID
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

func (client *fakeProtocolTerminalClient) SetTags(_ context.Context, terminalID string, tags map[string]string) error {
	client.tagIDs = append(client.tagIDs, terminalID)
	client.tagSets = append(client.tagSets, cloneStringMap(tags))
	return nil
}

func (client *fakeProtocolTerminalClient) Input(_ context.Context, channel uint16, data []byte) error {
	client.inputChannel = append(client.inputChannel, channel)
	client.inputData = append(client.inputData, append([]byte(nil), data...))
	return nil
}

func (client *fakeProtocolTerminalClient) InputWithOptions(_ context.Context, params protocol.InputParams) error {
	params.Data = append([]byte(nil), params.Data...)
	client.inputParams = append(client.inputParams, params)
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
	if client.snapshotResults != nil {
		if snapshot := client.snapshotResults[terminalID]; snapshot != nil {
			return snapshot, nil
		}
	}
	if client.snapshotResult != nil {
		return client.snapshotResult, nil
	}
	return &protocol.Snapshot{TerminalID: terminalID}, nil
}

func (client *fakeProtocolTerminalClient) StorageGet(_ context.Context, params protocol.StorageGetParams) (*protocol.StorageEntry, error) {
	client.storageGets = append(client.storageGets, params)
	if client.storageGetErr != nil {
		return nil, client.storageGetErr
	}
	if client.storageEntry != nil {
		return client.storageEntry, nil
	}
	return &protocol.StorageEntry{}, nil
}

type fakeCompactProtocolTerminalClient struct {
	*fakeProtocolTerminalClient
	compactSnapshot *protocol.CompactSnapshot
}

func (client *fakeCompactProtocolTerminalClient) SnapshotCompact(_ context.Context, terminalID string, _ int, _ int) (*protocol.CompactSnapshot, error) {
	client.snapshotIDs = append(client.snapshotIDs, terminalID)
	if client.compactSnapshot != nil {
		return client.compactSnapshot, nil
	}
	return &protocol.CompactSnapshot{TerminalID: terminalID}, nil
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

func TestProtocolClipboardStorageAdapterLoadsAndSavesClipboardHistory(t *testing.T) {
	snapshot := state.SnapshotClipboardForStorage(state.ClipboardStore{}.WithCopiedText("alpha"))
	value, err := state.EncodeClipboardStorageSnapshotValue(snapshot)
	if err != nil {
		t.Fatalf("encode clipboard snapshot: %v", err)
	}
	client := &fakeProtocolTerminalClient{
		storageEntry: &protocol.StorageEntry{
			AppID:   state.ClipboardStorageAppID,
			Scope:   protocol.StorageScopePublic,
			OwnerID: state.DefaultWorkspaceID,
			Key:     state.ClipboardStorageKeyRoot,
			Value:   value,
			Version: 7,
		},
	}
	adapter := ProtocolClipboardStorageAdapter{Client: client}
	ref := state.DefaultClipboardStorageRef(state.DefaultWorkspaceID)

	result, err := adapter.LoadClipboard(context.Background(), ref)
	if err != nil {
		t.Fatalf("load clipboard storage: %v", err)
	}
	if !result.Found || result.Version != 7 || len(result.Snapshot.Entries) != 1 || result.Snapshot.Entries[0].Text != "alpha" {
		t.Fatalf("unexpected clipboard load result %#v", result)
	}
	if len(client.storageGets) != 1 || client.storageGets[0].Key != state.ClipboardStorageKeyRoot {
		t.Fatalf("unexpected storage get %#v", client.storageGets)
	}

	save, err := adapter.SaveClipboard(context.Background(), ClipboardStorageSaveRequest{
		Ref:             ref,
		Snapshot:        state.SnapshotClipboardForStorage(state.ClipboardStore{}.WithCopiedText("beta")),
		CheckVersion:    true,
		ExpectedVersion: 7,
	})
	if err != nil {
		t.Fatalf("save clipboard storage: %v", err)
	}
	if save.Version != 8 || len(client.storagePuts) != 1 || !client.storagePuts[0].CheckVersion || client.storagePuts[0].ExpectedVersion != 7 {
		t.Fatalf("unexpected storage put save=%#v puts=%#v", save, client.storagePuts)
	}
	decoded, err := state.DecodeClipboardStorageSnapshot(client.storagePuts[0].Value)
	if err != nil {
		t.Fatalf("decode saved clipboard value: %v", err)
	}
	if len(decoded.Entries) != 1 || decoded.Entries[0].Text != "beta" {
		t.Fatalf("unexpected saved clipboard value %#v", decoded)
	}
}

func TestProtocolClipboardStorageAdapterWatchesClipboardKeyPrefix(t *testing.T) {
	client := &fakeProtocolTerminalClient{eventCh: make(chan protocol.Event, 1)}
	adapter := ProtocolClipboardStorageAdapter{Client: client}
	ref := state.DefaultClipboardStorageRef("workspace-a")
	events, err := adapter.WatchClipboard(context.Background(), ref)
	if err != nil {
		t.Fatalf("watch clipboard storage: %v", err)
	}
	if len(client.eventParams) != 1 || client.eventParams[0].StorageKeyPrefix != "clipboard/" {
		t.Fatalf("unexpected watch params %#v", client.eventParams)
	}
	client.eventCh <- protocol.Event{Type: protocol.EventStorageChanged, Storage: &protocol.StorageChangedData{
		AppID:   ref.AppID,
		Scope:   protocol.StorageScope(ref.Scope),
		OwnerID: ref.OwnerID,
		Key:     ref.Key,
		Version: 9,
		Op:      "put",
	}}
	got := <-events
	if got.Version != 9 || got.Ref.Key != ref.Key {
		t.Fatalf("unexpected clipboard storage event %#v", got)
	}
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

	client.window.Op = protocol.HistoryWindowAppend
	newer, err := adapter.HistoryNewer(context.Background(), HistoryNewerRequest{
		RequestID:  3,
		TerminalID: "term-1",
		Cols:       80,
		Rows:       10,
		Token:      "tok-1",
		Generation: 7,
		Cursor:     state.HistoryCursor{Valid: true, BeforeLineID: 43, BeforeRowInLine: 2},
		Boundary:   state.HistoryBoundary{FirstLineID: 40, LastLineID: 50},
	})
	if err != nil {
		t.Fatalf("newer: %v", err)
	}
	if newer.RequestID != 3 || newer.Window.Op != state.HistoryWindowAppend {
		t.Fatalf("unexpected newer result %#v", newer)
	}
	params = client.requests[2]
	if params.Mode != "newer" || params.Token != "tok-1" || params.Generation != 7 || !params.AfterCursorValid || params.AfterLineID != 43 || params.AfterRowInLine != 2 || params.BoundaryFirstLineID != 40 || params.BoundaryLastLineID != 50 {
		t.Fatalf("unexpected newer params %#v", params)
	}

	client.window.Op = protocol.HistoryWindowReplace
	oldest, err := adapter.HistoryOldest(context.Background(), HistoryOldestRequest{
		RequestID:  4,
		TerminalID: "term-1",
		Cols:       80,
		Rows:       10,
		Token:      "tok-1",
		Generation: 7,
		Boundary:   state.HistoryBoundary{FirstLineID: 42, LastLineID: 43},
	})
	if err != nil {
		t.Fatalf("oldest: %v", err)
	}
	if oldest.RequestID != 4 || oldest.Window.Op != state.HistoryWindowReplace {
		t.Fatalf("unexpected oldest result %#v", oldest)
	}
	params = client.requests[3]
	if params.Token != "tok-1" || params.Generation != 7 || params.CursorValid || params.BeforeLineID != 0 || params.BoundaryLastLineID != 43 {
		t.Fatalf("unexpected oldest params %#v", params)
	}

	client.copyText = "copied text"
	copied, err := adapter.HistoryCopyRange(context.Background(), HistoryCopyRangeRequest{
		TerminalID: "term-1",
		Cols:       80,
		Token:      "tok-1",
		Generation: 7,
		Boundary:   state.HistoryBoundary{FirstLineID: 42, LastLineID: 43},
		Start:      state.CopyLogicalPosition{Valid: true, LineID: 42, Col: 1},
		End:        state.CopyLogicalPosition{Valid: true, LineID: 43, Col: 3},
	})
	if err != nil {
		t.Fatalf("copy range: %v", err)
	}
	if copied.Text != "copied text" {
		t.Fatalf("unexpected copy range result %#v", copied)
	}
	if len(client.copyRequests) != 1 {
		t.Fatalf("expected copy request, got %#v", client.copyRequests)
	}
	copyParams := client.copyRequests[0]
	if copyParams.Token != "tok-1" || copyParams.Generation != 7 || copyParams.BoundaryFirstLineID != 42 || copyParams.BoundaryLastLineID != 43 || !copyParams.RangeValid || copyParams.RangeStartLineID != 42 || copyParams.RangeStartCol != 1 || copyParams.RangeEndLineID != 43 || copyParams.RangeEndCol != 3 {
		t.Fatalf("unexpected copy params %#v", copyParams)
	}
}

func TestProtocolCoreClientAdapterNormalizesStaleHistoryWindowError(t *testing.T) {
	client := &fakeProtocolHistoryClient{windowErr: errors.New("protocol error 400: stale history window")}
	adapter := ProtocolCoreClientAdapter{Client: client}

	result, err := adapter.HistoryOlder(context.Background(), HistoryOlderRequest{
		RequestID:  99,
		TerminalID: "term-1",
		Cols:       80,
		Rows:       10,
		Token:      "tok-1",
		Cursor:     state.HistoryCursor{Valid: true, BeforeLineID: 42},
	})
	if !errors.Is(err, ErrStaleHistoryWindow) {
		t.Fatalf("expected stale history sentinel, got result=%#v err=%v", result, err)
	}
	if result.RequestID != 99 {
		t.Fatalf("history errors must keep request id for reducer correlation, got %#v", result)
	}
	if len(client.requests) != 1 || client.requests[0].Token != "tok-1" {
		t.Fatalf("unexpected history request params %#v", client.requests)
	}

	client.windowErr = nil
	client.copyErr = errors.New("protocol error 400: stale history window")
	_, err = adapter.HistoryCopyRange(context.Background(), HistoryCopyRangeRequest{TerminalID: "term-1", Cols: 80, Token: "tok-1"})
	if !errors.Is(err, ErrStaleHistoryWindow) {
		t.Fatalf("expected copy range stale sentinel, got %v", err)
	}
}

func TestProtocolCoreClientAdapterReleasesHistoryToken(t *testing.T) {
	client := &fakeProtocolHistoryClient{}
	adapter := ProtocolCoreClientAdapter{Client: client}

	if err := adapter.ReleaseHistory(context.Background(), HistoryReleaseRequest{TerminalID: "term-1", Token: "tok-1"}); err != nil {
		t.Fatalf("release history: %v", err)
	}
	if len(client.releaseRequests) != 1 || client.releaseRequests[0].TerminalID != "term-1" || client.releaseRequests[0].Token != "tok-1" {
		t.Fatalf("unexpected release requests %#v", client.releaseRequests)
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
	if row.Text != "ERR 好" || state.HistoryRowDisplayWidth(row) != 6 || len(row.Cells) != 3 {
		t.Fatalf("adapter should keep styled cells and derived text, got %#v", row)
	}
	if row.Cells[0].Text != "ERR" || row.Cells[0].Width != 3 || row.Cells[0].Style.FG != "ansi:1" || !row.Cells[0].Style.Bold {
		t.Fatalf("lost first styled run %#v", row.Cells[0])
	}
	if row.Cells[2].Text != "好" || row.Cells[2].Width != 2 || row.Cells[2].Style.FG != "#ffcc00" || !row.Cells[2].Style.Underline || row.Cells[2].LinkURL == "" || row.Cells[2].LinkParams == "" {
		t.Fatalf("lost wide linked cell %#v", row.Cells[2])
	}
}

func TestProtocolCoreClientAdapterMapsStyledHistoryRunWideCells(t *testing.T) {
	style := &protocol.CompactRowStyle{BG: "idx:236"}
	client := &fakeProtocolHistoryClient{
		window: &protocol.HistoryWindow{
			TerminalID: "term-1",
			Token:      "tok-1",
			Op:         protocol.HistoryWindowReplace,
			Size:       protocol.Size{Cols: 20, Rows: 24},
			Rows: []protocol.CompactRow{{
				Runs: []protocol.CompactRowRun{
					{Text: "验证通过", Style: style},
					{Text: " ok", Style: style},
				},
				TailFill: style,
			}},
			RowLineIDs: []uint64{42},
			RowInLine:  []int{0},
		},
	}
	adapter := ProtocolCoreClientAdapter{Client: client}

	result, err := adapter.HistoryLatest(context.Background(), HistoryLatestRequest{RequestID: 1, TerminalID: "term-1", Cols: 20, Rows: 20})
	if err != nil {
		t.Fatalf("latest: %v", err)
	}

	row := result.Window.Rows[0]
	if row.Text != "验证通过 ok" || state.HistoryRowDisplayWidth(row) != 11 || len(row.Cells) != 7 {
		t.Fatalf("styled wide run should keep terminal display width, got %#v", row)
	}
	for index, want := range []int{2, 2, 2, 2, 1, 1, 1} {
		if row.Cells[index].Width != want {
			t.Fatalf("cell %d should have display width %d, got %#v", index, want, row.Cells[index])
		}
	}
	if got := state.HistoryRowSliceDisplay(row, 0, state.HistoryRowDisplayWidth(row)); got != "验证通过 ok" {
		t.Fatalf("row slice should not lose wide styled text, got %q row=%#v", got, row)
	}
	if row.TailFill == nil || row.TailFill.BG != "idx:236" {
		t.Fatalf("tail fill should stay display-only metadata, got %#v", row.TailFill)
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
	if row.Text != "cmd  " || state.HistoryRowDisplayWidth(row) != 5 || state.HistoryRowSliceDisplay(row, 0, 5) != "cmd  " {
		t.Fatalf("adapter should preserve trailing blank history cells, got %#v", row)
	}
}

func TestProtocolCoreClientAdapterPreservesStyledTrailingBlankHistoryCells(t *testing.T) {
	style := protocol.CellStyle{BG: "idx:24"}
	client := &fakeProtocolHistoryClient{
		window: &protocol.HistoryWindow{
			TerminalID: "term-1",
			Token:      "tok-1",
			Op:         protocol.HistoryWindowReplace,
			Size:       protocol.Size{Cols: 8, Rows: 24},
			Rows: []protocol.CompactRow{protocol.CompactRowFromCellsPreserveTrailingBlankCells([]protocol.Cell{
				{Content: "B", Width: 1, Style: style},
				{Content: "G", Width: 1, Style: style},
				{Content: " ", Width: 1, Style: style},
				{Content: " ", Width: 1, Style: style},
			}, true)},
			RowLineIDs: []uint64{42},
			RowInLine:  []int{0},
		},
	}
	adapter := ProtocolCoreClientAdapter{Client: client}

	result, err := adapter.HistoryLatest(context.Background(), HistoryLatestRequest{RequestID: 1, TerminalID: "term-1", Cols: 8, Rows: 20})
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	row := result.Window.Rows[0]
	if row.Text != "BG  " || state.HistoryRowDisplayWidth(row) != 4 || len(row.Cells) != 4 {
		t.Fatalf("adapter should preserve styled trailing blank row footprint, got %#v", row)
	}
	for i, cell := range row.Cells {
		if cell.Width != 1 || cell.Style.BG != "idx:24" {
			t.Fatalf("styled trailing blank cell %d should keep background, got %#v", i, cell)
		}
	}
}

func TestProtocolCoreClientAdapterPreservesHistoryTailFillWithoutCells(t *testing.T) {
	row := protocol.CompactRowFromCells([]protocol.Cell{
		{Content: "i", Width: 1, Style: protocol.CellStyle{BG: "idx:24"}},
		{Content: "j", Width: 1, Style: protocol.CellStyle{BG: "idx:24"}},
	})
	row.TailFill = &protocol.CompactRowStyle{BG: "idx:24"}
	client := &fakeProtocolHistoryClient{
		window: &protocol.HistoryWindow{
			TerminalID: "term-1",
			Token:      "tok-1",
			Op:         protocol.HistoryWindowReplace,
			Size:       protocol.Size{Cols: 8, Rows: 24},
			Rows:       []protocol.CompactRow{row},
			RowLineIDs: []uint64{42},
			RowInLine:  []int{0},
		},
	}
	adapter := ProtocolCoreClientAdapter{Client: client}

	result, err := adapter.HistoryLatest(context.Background(), HistoryLatestRequest{RequestID: 1, TerminalID: "term-1", Cols: 8, Rows: 20})
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	rowState := result.Window.Rows[0]
	if rowState.Text != "ij" || state.HistoryRowDisplayWidth(rowState) != 2 || len(rowState.Cells) != 2 {
		t.Fatalf("tail fill must not materialize as logical cells/text, got %#v", rowState)
	}
	if rowState.TailFill == nil || rowState.TailFill.BG != "idx:24" {
		t.Fatalf("expected row tail fill metadata, got %#v", rowState)
	}
	if result.Window.SourceLines[0].TailFill == nil || result.Window.SourceLines[0].TailFill.BG != "idx:24" {
		t.Fatalf("expected source line tail fill metadata, got %#v", result.Window.SourceLines[0])
	}
}

func TestProtocolCoreClientAdapterMaterializesAuthoritativeCellPadding(t *testing.T) {
	client := &fakeProtocolHistoryClient{
		window: &protocol.HistoryWindow{
			TerminalID: "term-1",
			Token:      "tok-1",
			Op:         protocol.HistoryWindowReplace,
			Size:       protocol.Size{Cols: 40, Rows: 24},
			Rows: []protocol.CompactRow{protocol.CompactRowFromCells([]protocol.Cell{
				{Content: "AGENTS.md", Width: 12},
				{Content: "go.work", Width: 9},
				{Content: "README.md", Width: 9},
			})},
			Lines:       []protocol.HistoryLineSpan{{LogicalLineID: 42, StartRow: 0, EndRow: 0}},
			RowLineIDs:  []uint64{42},
			RowInLine:   []int{0},
			LoadedLines: 1,
		},
	}
	adapter := ProtocolCoreClientAdapter{Client: client}

	result, err := adapter.HistoryLatest(context.Background(), HistoryLatestRequest{RequestID: 1, TerminalID: "term-1", Cols: 40, Rows: 20})
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if len(result.Window.SourceLines) != 1 || result.Window.SourceLines[0].Text != "AGENTS.md   go.work  README.md" {
		t.Fatalf("adapter should materialize cell padding into frozen source text, got %#v", result.Window.SourceLines)
	}
	if got := rowTextsForProtocolAdapter(result.Window.Rows); len(got) != 1 || got[0] != "AGENTS.md   go.work  README.md" {
		t.Fatalf("local reflow should keep padded history row, got %#v", result.Window.Rows)
	}

	result, err = adapter.HistoryLatest(context.Background(), HistoryLatestRequest{RequestID: 2, TerminalID: "term-1", Cols: 12, Rows: 20})
	if err != nil {
		t.Fatalf("latest narrow: %v", err)
	}
	if got := rowTextsForProtocolAdapter(result.Window.Rows); len(got) != 3 || got[0] != "AGENTS.md   " || got[1] != "go.work  REA" || got[2] != "DME.md" {
		t.Fatalf("narrow local reflow should split padded protocol cells without losing spaces, got %#v", got)
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

func TestProtocolCoreClientAdapterMergesMixedPlainAndStyledRowsIntoCompleteCells(t *testing.T) {
	client := &fakeProtocolHistoryClient{
		window: &protocol.HistoryWindow{
			TerminalID: "term-1",
			Token:      "tok-1",
			Op:         protocol.HistoryWindowReplace,
			Size:       protocol.Size{Cols: 3, Rows: 24},
			Rows: []protocol.CompactRow{
				protocol.CompactRowFromCells([]protocol.Cell{{Content: "a"}, {Content: "b"}, {Content: "c"}}),
				protocol.CompactRowFromCells([]protocol.Cell{
					{Content: "E", Style: protocol.CellStyle{FG: "ansi:1", Bold: true}},
					{Content: "R", Style: protocol.CellStyle{FG: "ansi:1", Bold: true}},
					{Content: "R", Style: protocol.CellStyle{FG: "ansi:1", Bold: true}},
				}),
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
	source := result.Window.SourceLines
	if len(source) != 1 || source[0].Text != "abcERR" || len(source[0].Cells) != 4 {
		t.Fatalf("mixed protocol rows should create complete source cells, got %#v", source)
	}
	if source[0].Cells[0].Text != "abc" || source[0].Cells[0].Width != 3 || source[0].Cells[1].Style.FG != "ansi:1" {
		t.Fatalf("mixed source cells lost plain prefix or styled suffix, got %#v", source[0].Cells)
	}
	reflowedRows, _ := state.ReflowHistoryLogicalLines(source, 6)
	if got := rowTextsForProtocolAdapter(reflowedRows); len(got) != 1 || got[0] != "abcERR" {
		t.Fatalf("complete source cells should support wider local reflow, got %#v", reflowedRows)
	}
}

func TestProtocolCoreClientAdapterSynthesizesSpansWhenProtocolOmitsLines(t *testing.T) {
	client := &fakeProtocolHistoryClient{
		window: &protocol.HistoryWindow{
			TerminalID: "term-1",
			Token:      "tok-1",
			Op:         protocol.HistoryWindowReplace,
			Size:       protocol.Size{Cols: 3, Rows: 24},
			Rows: []protocol.CompactRow{
				protocol.CompactRowFromCells([]protocol.Cell{{Content: "a"}, {Content: "b"}, {Content: "c"}}),
				protocol.CompactRowFromCells([]protocol.Cell{{Content: "d"}, {Content: "e"}, {Content: "f"}}),
				protocol.CompactRowFromCells([]protocol.Cell{{Content: "x"}, {Content: "y"}, {Content: "z"}}),
			},
			RowLineIDs: []uint64{42, 42, 43},
			RowInLine:  []int{0, 1, 0},
		},
	}
	adapter := ProtocolCoreClientAdapter{Client: client}

	result, err := adapter.HistoryLatest(context.Background(), HistoryLatestRequest{RequestID: 1, TerminalID: "term-1", Cols: 3, Rows: 20})
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if got := result.Window.Lines; len(got) != 2 || got[0].LineID != 42 || got[0].StartRow != 0 || got[0].EndRow != 1 || got[1].LineID != 43 || got[1].StartRow != 2 || got[1].EndRow != 2 {
		t.Fatalf("adapter should synthesize row spans when protocol omits lines, got %#v", got)
	}
}

func TestProtocolCoreClientAdapterPreservesClippedLogicalLineSource(t *testing.T) {
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
			Lines: []protocol.HistoryLineSpan{{
				LogicalLineID: 42,
				StartRow:      0,
				EndRow:        1,
				ClippedBefore: true,
				ClippedAfter:  true,
			}},
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
	if len(result.Window.SourceLines) != 1 || !result.Window.SourceLines[0].ClippedBefore || !result.Window.SourceLines[0].ClippedAfter {
		t.Fatalf("adapter should preserve clipped logical-line source flags, got %#v", result.Window.SourceLines)
	}
	_, reflowedSpans := state.ReflowHistoryLogicalLines(result.Window.SourceLines, 6)
	if len(reflowedSpans) != 1 || !reflowedSpans[0].ClippedBefore || !reflowedSpans[0].ClippedAfter {
		t.Fatalf("local reflow should preserve clipped logical-line span flags, got %#v", reflowedSpans)
	}
}

func TestProtocolCoreClientAdapterPreservesLiveTailOwnership(t *testing.T) {
	client := &fakeProtocolHistoryClient{
		window: &protocol.HistoryWindow{
			TerminalID: "term-1",
			Token:      "tok-1",
			Op:         protocol.HistoryWindowReplace,
			Size:       protocol.Size{Cols: 6, Rows: 24},
			Rows: []protocol.CompactRow{
				protocol.CompactRowFromCells([]protocol.Cell{{Content: "old", Width: 3}}),
				protocol.CompactRowFromCells([]protocol.Cell{{Content: "Update", Width: 6}}),
				protocol.CompactRowFromCells([]protocol.Cell{{Content: "card", Width: 4}}),
			},
			RowOwnership: []string{
				protocol.RowOwnershipPersisted,
				protocol.RowOwnershipLiveTailLive,
				protocol.RowOwnershipLiveTailLive,
			},
			Lines: []protocol.HistoryLineSpan{
				{LogicalLineID: 10, StartRow: 0, EndRow: 0},
				{LogicalLineID: 20, StartRow: 1, EndRow: 2},
			},
			RowLineIDs: []uint64{10, 20, 20},
			RowInLine:  []int{0, 0, 1},
		},
	}
	adapter := ProtocolCoreClientAdapter{Client: client}

	result, err := adapter.HistoryLatest(context.Background(), HistoryLatestRequest{RequestID: 1, TerminalID: "term-1", Cols: 6, Rows: 20})
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if result.Window.Rows[0].LiveTail || !result.Window.Rows[1].LiveTail || !result.Window.Rows[2].LiveTail {
		t.Fatalf("adapter should preserve row live-tail ownership, got %#v", result.Window.Rows)
	}
	if len(result.Window.SourceLines) != 2 || result.Window.SourceLines[0].LiveTail || !result.Window.SourceLines[1].LiveTail {
		t.Fatalf("adapter should merge live-tail ownership into source lines, got %#v", result.Window.SourceLines)
	}
	reflowedRows, _ := state.ReflowHistoryLogicalLines(result.Window.SourceLines, 10)
	if len(reflowedRows) != 2 || reflowedRows[0].LiveTail || !reflowedRows[1].LiveTail {
		t.Fatalf("local reflow should preserve live-tail ownership, got %#v", reflowedRows)
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

func TestProtocolWorkbenchStorageAdapterTreatsMissingStorageAsEmpty(t *testing.T) {
	ref := state.DefaultWorkbenchStorageRef("")
	for _, client := range []*fakeProtocolTerminalClient{
		{},
		{storageGetErr: errors.New("protocol error 404: storage entry not found")},
	} {
		adapter := ProtocolWorkbenchStorageAdapter{Client: client}

		loaded, err := adapter.LoadWorkbench(context.Background(), ref)
		if err != nil {
			t.Fatalf("load missing workbench storage: %v", err)
		}
		if loaded.Found {
			t.Fatalf("missing workbench storage should load as empty, got %#v", loaded)
		}
		if len(client.storageGets) != 1 || client.storageGets[0].AppID != state.WorkbenchStorageAppID || client.storageGets[0].Key != state.WorkbenchStorageKeyRoot {
			t.Fatalf("adapter must still request workbench storage key, got %#v", client.storageGets)
		}
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

	if err := adapter.SendInput(context.Background(), TerminalInputRequest{TerminalID: "term-1", Channel: 11, SurfaceID: "surface-1", ViewID: "view-1", Bytes: []byte("x")}); err != nil {
		t.Fatalf("input: %v", err)
	}
	if len(client.inputParams) != 1 || client.inputParams[0].TerminalID != "term-1" || client.inputParams[0].Channel != 11 || client.inputParams[0].SurfaceID != "surface-1" || client.inputParams[0].ViewID != "view-1" || string(client.inputParams[0].Data) != "x" {
		t.Fatalf("unexpected input params %#v", client.inputParams)
	}
	if len(client.inputData) != 0 {
		t.Fatalf("adapter must prefer acked input request over stream input, got %#v", client.inputData)
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
	if err := adapter.Detach(context.Background(), TerminalDetachRequest{
		TerminalID: "term-1",
		Channel:    11,
		SurfaceID:  "surface-1",
		ViewID:     "view-1",
	}); err != nil {
		t.Fatalf("detach: %v", err)
	}
	if len(client.detachParams) != 1 || client.detachParams[0].TerminalID != "term-1" || client.detachParams[0].Channel != 11 || client.detachParams[0].SurfaceID != "surface-1" || client.detachParams[0].ViewID != "view-1" {
		t.Fatalf("unexpected detach params %#v", client.detachParams)
	}
}

func TestProtocolTerminalServiceAdapterUsesResizeControlRole(t *testing.T) {
	client := &fakeProtocolTerminalClient{
		attachResult: &protocol.AttachResult{
			Channel: 12,
			ResizeControl: &protocol.ResizeControl{
				CanResize:   false,
				Reason:      protocol.ResizeControlReasonFollower,
				OwnerViewID: "owner-view",
				ResizeOwnership: &protocol.ResizeOwnership{
					OwnerViewID: "owner-view",
					Size:        protocol.Size{Cols: 100, Rows: 40},
					Epoch:       3,
				},
			},
		},
	}
	adapter := ProtocolTerminalServiceAdapter{Client: client}

	attached, err := adapter.Attach(context.Background(), TerminalAttachRequest{
		TerminalID:   "term-1",
		Cols:         80,
		Rows:         24,
		ResizePolicy: protocol.ResizePolicyOwner,
		SurfaceID:    "surface-1",
		ViewID:       "view-1",
	})
	if err != nil {
		t.Fatalf("attach: %v", err)
	}

	if attached.ResizePolicy != state.TerminalResizeRoleFollower || attached.CanResize || attached.OwnerViewID != "owner-view" {
		t.Fatalf("attach role must follow core resize control, got %#v", attached)
	}
}

func TestProtocolTerminalServiceAdapterMapsLiveSurfaceSnapshot(t *testing.T) {
	client := &fakeProtocolTerminalClient{
		listResult: &protocol.ListResult{Terminals: []protocol.TerminalInfo{{
			ID:      "term-1",
			State:   "running",
			Command: []string{"/bin/zsh"},
		}}},
		snapshotResult: &protocol.Snapshot{
			TerminalID:        "term-1",
			Size:              protocol.Size{Cols: 12, Rows: 2},
			HistoryGeneration: 42,
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
	if !result.Ready || result.Snapshot.Cols != 12 || result.Snapshot.Rows != 2 || len(result.Snapshot.Screen) != 2 {
		t.Fatalf("unexpected live surface result %#v", result)
	}
	if result.Snapshot.Revision != 42 {
		t.Fatalf("expected history generation to become live revision boundary, got %d", result.Snapshot.Revision)
	}
	if len(result.Snapshot.Lines) != 0 {
		t.Fatalf("live surface with screen cells should not keep duplicate text lines %#v", result.Snapshot.Lines)
	}
	if result.Snapshot.Screen[0][0].Text != "$ " || result.Snapshot.Screen[0][1].Text != "你好" || result.Snapshot.Screen[0][2].Text != "🚀" || result.Snapshot.Screen[1][0].Text != "done" {
		t.Fatalf("unexpected live surface screen %#v", result.Snapshot.Screen)
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
	if !result.LifecycleKnown || result.Snapshot.State != state.TerminalLiveAttached || result.Snapshot.ExitReason != "" || len(result.Snapshot.Command) != 0 {
		t.Fatalf("expected running core lifecycle on live surface, result=%#v snapshot=%#v", result, result.Snapshot)
	}
}

func TestProtocolTerminalServiceAdapterMergesPlainASCIILiveCellRuns(t *testing.T) {
	client := &fakeProtocolTerminalClient{
		listResult: &protocol.ListResult{Terminals: []protocol.TerminalInfo{{
			ID:    "term-1",
			State: "running",
		}}},
		snapshotResult: &protocol.Snapshot{
			TerminalID: "term-1",
			Size:       protocol.Size{Cols: 8, Rows: 1},
			Screen: protocol.ScreenData{Cells: [][]protocol.Cell{{
				{Content: "a", Width: 1},
				{Content: "b", Width: 1},
				{Content: "c", Width: 1},
				{Content: "d", Width: 1, Style: protocol.CellStyle{FG: "ansi:2"}},
				{Content: "e", Width: 1, Style: protocol.CellStyle{FG: "ansi:2"}},
				{Content: "f", Width: 1},
			}}},
		},
	}
	adapter := ProtocolTerminalServiceAdapter{Client: client}

	result, err := adapter.LiveSurface(context.Background(), TerminalSurfaceRequest{TerminalID: "term-1", Cols: 8, Rows: 1})
	if err != nil {
		t.Fatalf("live surface: %v", err)
	}

	row := result.Snapshot.Screen[0]
	if len(row) != 3 || row[0].Text != "abc" || row[0].Width != 3 || row[1].Text != "de" || row[1].Width != 2 || row[1].FG != "ansi:2" || row[2].Text != "f" {
		t.Fatalf("plain ASCII live cells should merge by style run, got %#v", row)
	}
}

func TestProtocolTerminalServiceAdapterMapsCompactLiveSurfaceSnapshot(t *testing.T) {
	base := &fakeProtocolTerminalClient{
		listResult: &protocol.ListResult{Terminals: []protocol.TerminalInfo{{
			ID:    "term-1",
			State: "running",
		}}},
	}
	client := &fakeCompactProtocolTerminalClient{
		fakeProtocolTerminalClient: base,
		compactSnapshot: &protocol.CompactSnapshot{
			TerminalID:        "term-1",
			Size:              protocol.Size{Cols: 12, Rows: 2},
			HistoryGeneration: 77,
			ScreenRows: []protocol.CompactRow{
				protocol.CompactRowFromCells([]protocol.Cell{
					{Content: "$", Width: 1},
					{Content: " ", Width: 1},
					{Content: "o", Width: 1, Style: protocol.CellStyle{FG: "ansi:2"}},
					{Content: "k", Width: 1, Style: protocol.CellStyle{FG: "ansi:2"}},
				}),
				protocol.CompactRowFromCells([]protocol.Cell{{Content: "done", Width: 4}}),
			},
			Cursor: protocol.CursorState{Visible: true, Row: 1, Col: 4, Shape: "block"},
			Modes:  protocol.TerminalModes{MouseTracking: true, MouseButtonEvent: true, MouseSGR: true},
		},
	}
	adapter := ProtocolTerminalServiceAdapter{Client: client}

	result, err := adapter.LiveSurface(context.Background(), TerminalSurfaceRequest{TerminalID: "term-1", Cols: 12, Rows: 2})
	if err != nil {
		t.Fatalf("compact live surface: %v", err)
	}

	if len(client.snapshotIDs) != 1 || client.snapshotIDs[0] != "term-1" {
		t.Fatalf("expected compact snapshot request only, got %#v", client.snapshotIDs)
	}
	if result.Snapshot.Revision != 77 || result.Snapshot.Cols != 12 || result.Snapshot.Rows != 2 {
		t.Fatalf("compact snapshot metadata not preserved: %#v", result.Snapshot)
	}
	if len(result.Snapshot.Lines) != 0 {
		t.Fatalf("compact screen rows should not keep duplicate lines, got %#v", result.Snapshot.Lines)
	}
	row := result.Snapshot.Screen[0]
	if len(row) != 2 || row[0].Text != "$ " || row[0].Width != 2 || row[1].Text != "ok" || row[1].FG != "ansi:2" {
		t.Fatalf("compact rows should map directly to merged live cells, got %#v", row)
	}
	if !result.Snapshot.Cursor.Visible || result.Snapshot.Cursor.Shape != "block" || !result.Snapshot.Modes.MousePassthroughEnabled() {
		t.Fatalf("compact cursor/modes not preserved: %#v %#v", result.Snapshot.Cursor, result.Snapshot.Modes)
	}
}

func BenchmarkLiveSurfaceCellsFromProtocolASCIIStyledRuns(b *testing.B) {
	cells := make([]protocol.Cell, 0, 120)
	for i := 0; i < 120; i++ {
		cell := protocol.Cell{Content: string(byte('a' + i%26)), Width: 1}
		if i >= 60 {
			cell.Style.FG = "ansi:2"
		}
		cells = append(cells, cell)
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if got := liveSurfaceCellsFromProtocol(cells); len(got) != 2 {
			b.Fatalf("expected two style runs, got %#v", got)
		}
	}
}

func TestProtocolTerminalServiceAdapterMapsExitedLiveSurfaceLifecycle(t *testing.T) {
	exitedAt := time.Date(2026, 6, 17, 12, 45, 0, 0, time.UTC)
	exitCode := 23
	client := &fakeProtocolTerminalClient{
		listResult: &protocol.ListResult{Terminals: []protocol.TerminalInfo{{
			ID:       "term-1",
			State:    "exited",
			Command:  []string{"bash", "-lc", "exit 23"},
			ExitCode: &exitCode,
			ExitedAt: exitedAt,
		}}},
		snapshotResult: &protocol.Snapshot{
			TerminalID: "term-1",
			Size:       protocol.Size{Cols: 80, Rows: 24},
			Screen: protocol.ScreenData{Cells: [][]protocol.Cell{
				{{Content: "terminal exited: term-1 code:23 exited"}},
			}},
		},
	}
	adapter := ProtocolTerminalServiceAdapter{Client: client}

	result, err := adapter.LiveSurface(context.Background(), TerminalSurfaceRequest{TerminalID: "term-1", Cols: 80, Rows: 24})
	if err != nil {
		t.Fatalf("live surface: %v", err)
	}

	if !result.LifecycleKnown || result.Snapshot.State != state.TerminalLiveExited || result.Snapshot.ExitCode != 23 || !result.Snapshot.ExitedAt.Equal(exitedAt) || strings.Join(result.Snapshot.Command, " ") != "bash -lc exit 23" {
		t.Fatalf("expected exited core lifecycle on live surface, result=%#v snapshot=%#v", result, result.Snapshot)
	}
}

func TestProtocolTerminalServiceAdapterRunningLifecycleIgnoresExitMarkerText(t *testing.T) {
	client := &fakeProtocolTerminalClient{
		listResult: &protocol.ListResult{Terminals: []protocol.TerminalInfo{{
			ID:      "term-1",
			State:   "running",
			Command: []string{"/bin/zsh"},
		}}},
		snapshotResult: &protocol.Snapshot{
			TerminalID: "term-1",
			Size:       protocol.Size{Cols: 80, Rows: 3},
			Screen: protocol.ScreenData{Cells: [][]protocol.Cell{
				{{Content: "terminal exited: term-1 code:0 exited"}},
				{{Content: "command: /bin/zsh"}},
				{{Content: "% "}},
			}},
			Cursor: protocol.CursorState{Visible: true, Row: 2, Col: 2, Shape: "bar"},
		},
	}
	adapter := ProtocolTerminalServiceAdapter{Client: client}

	result, err := adapter.LiveSurface(context.Background(), TerminalSurfaceRequest{TerminalID: "term-1", Cols: 80, Rows: 3})
	if err != nil {
		t.Fatalf("live surface: %v", err)
	}

	if result.Snapshot.State != state.TerminalLiveAttached || result.Snapshot.ExitReason != "" || result.Snapshot.ExitCode != 0 || !result.Snapshot.ExitedAt.IsZero() {
		t.Fatalf("running core lifecycle must win over marker text, got %#v", result.Snapshot)
	}
	if len(result.Snapshot.Lines) != 0 || len(result.Snapshot.Screen) != 3 || result.Snapshot.Screen[2][0].Text != "% " || !result.Snapshot.Cursor.Visible {
		t.Fatalf("live surface should still keep marker screen and cursor, got %#v", result.Snapshot)
	}
}

func TestProtocolTerminalServiceAdapterSkipsZeroWidthContinuationPlaceholders(t *testing.T) {
	client := &fakeProtocolTerminalClient{
		listResult: &protocol.ListResult{Terminals: []protocol.TerminalInfo{{
			ID:    "term-1",
			State: "running",
		}}},
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

func TestProtocolTerminalServiceAdapterMapsOrdinaryLiveEventsToRefreshInvalidation(t *testing.T) {
	eventCh := make(chan protocol.Event, 1)
	client := &fakeProtocolTerminalClient{
		eventCh: eventCh,
	}
	adapter := ProtocolTerminalServiceAdapter{Client: client}
	events, err := adapter.LiveEvents(context.Background(), TerminalLiveEventRequest{TerminalID: "term-1", Cols: 80, Rows: 24})
	if err != nil {
		t.Fatalf("live events: %v", err)
	}
	eventCh <- protocol.Event{Type: protocol.EventTerminalStateChanged, TerminalID: "term-1"}

	got := <-events
	if !got.Refresh || got.Ready || got.TerminalID != "term-1" || got.Snapshot.TerminalID != "" || len(got.Snapshot.Screen) != 0 {
		t.Fatalf("unexpected live event %#v", got)
	}
	if len(client.eventParams) != 1 || client.eventParams[0].TerminalID != "term-1" {
		t.Fatalf("expected protocol events subscription, got %#v", client.eventParams)
	}
	if len(client.snapshotIDs) != 0 {
		t.Fatalf("ordinary live event should not decode snapshot before app coalescing, got %#v", client.snapshotIDs)
	}
}

func TestProtocolTerminalServiceAdapterLiveEventsKeepIndependentTerminalStreams(t *testing.T) {
	eventCh := make(chan protocol.Event, 4)
	client := &fakeProtocolTerminalClient{
		eventCh: eventCh,
	}
	adapter := ProtocolTerminalServiceAdapter{Client: client}
	termOneEvents, err := adapter.LiveEvents(context.Background(), TerminalLiveEventRequest{TerminalID: "term-1", Cols: 80, Rows: 24})
	if err != nil {
		t.Fatalf("term-1 live events: %v", err)
	}
	termTwoEvents, err := adapter.LiveEvents(context.Background(), TerminalLiveEventRequest{TerminalID: "term-2", Cols: 80, Rows: 24})
	if err != nil {
		t.Fatalf("term-2 live events: %v", err)
	}

	eventCh <- protocol.Event{Type: protocol.EventTerminalStateChanged, TerminalID: "term-1"}
	if got := readTerminalLiveEvent(t, termOneEvents); got.TerminalID != "term-1" || !got.Refresh || got.Ready {
		t.Fatalf("term-1 stream must receive its own event without blocking behind term-2 subscription, got %#v", got)
	}
	assertNoTerminalLiveEvent(t, termTwoEvents)

	eventCh <- protocol.Event{Type: protocol.EventTerminalStateChanged, TerminalID: "term-2"}
	if got := readTerminalLiveEvent(t, termTwoEvents); got.TerminalID != "term-2" || !got.Refresh || got.Ready {
		t.Fatalf("term-2 stream must receive its own event, got %#v", got)
	}
	assertNoTerminalLiveEvent(t, termOneEvents)
	if len(client.eventParams) != 2 || client.eventParams[0].TerminalID != "term-1" || client.eventParams[1].TerminalID != "term-2" {
		t.Fatalf("expected independent protocol event subscriptions, got %#v", client.eventParams)
	}
	if len(client.snapshotIDs) != 0 {
		t.Fatalf("independent ordinary streams should not fetch snapshots in service layer, got %#v", client.snapshotIDs)
	}
}

func readTerminalLiveEvent(t *testing.T, events <-chan TerminalLiveEvent) TerminalLiveEvent {
	t.Helper()
	select {
	case event, ok := <-events:
		if !ok {
			t.Fatal("terminal live event channel closed")
		}
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for terminal live event")
	}
	return TerminalLiveEvent{}
}

func liveScreenRowText(snapshot state.LiveSurfaceSnapshot, rowIndex int) string {
	if rowIndex < 0 || rowIndex >= len(snapshot.Screen) {
		return ""
	}
	var builder strings.Builder
	for _, cell := range snapshot.Screen[rowIndex] {
		builder.WriteString(cell.Text)
	}
	return strings.TrimRight(builder.String(), " ")
}

func assertNoTerminalLiveEvent(t *testing.T, events <-chan TerminalLiveEvent) {
	t.Helper()
	select {
	case event, ok := <-events:
		if ok {
			t.Fatalf("unexpected terminal live event %#v", event)
		}
		t.Fatal("terminal live event channel closed")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestProtocolTerminalServiceAdapterMapsMetadataEventsToTags(t *testing.T) {
	eventCh := make(chan protocol.Event, 1)
	client := &fakeProtocolTerminalClient{
		eventCh: eventCh,
		listResult: &protocol.ListResult{Terminals: []protocol.TerminalInfo{{
			ID:   "term-1",
			Tags: map[string]string{"termx.size_lock": "lock", "role": "shell"},
		}}},
	}
	adapter := ProtocolTerminalServiceAdapter{Client: client}
	events, err := adapter.LiveEvents(context.Background(), TerminalLiveEventRequest{TerminalID: "term-1", Cols: 80, Rows: 24})
	if err != nil {
		t.Fatalf("live events: %v", err)
	}
	eventCh <- protocol.Event{Type: protocol.EventTerminalMetadataChanged, TerminalID: "term-1"}

	got := <-events
	if !got.Metadata || got.TerminalID != "term-1" || got.Tags["termx.size_lock"] != "lock" || got.Ready {
		t.Fatalf("metadata event should return terminal tags without surface refresh, got %#v", got)
	}
	if client.listCalls != 1 || len(client.snapshotIDs) != 0 {
		t.Fatalf("metadata event should list terminal metadata without snapshot refresh, lists=%d snapshots=%#v", client.listCalls, client.snapshotIDs)
	}
}

func TestProtocolTerminalServiceAdapterMapsExitedEventMetadata(t *testing.T) {
	exitedAt := time.Date(2026, 6, 17, 12, 45, 0, 0, time.UTC)
	exitCode := 23
	eventCh := make(chan protocol.Event, 1)
	client := &fakeProtocolTerminalClient{
		eventCh: eventCh,
		listResult: &protocol.ListResult{Terminals: []protocol.TerminalInfo{{
			ID:       "term-1",
			Command:  []string{"bash", "-lc", "make test"},
			State:    "exited",
			ExitCode: &exitCode,
			ExitedAt: exitedAt,
		}}},
		snapshotResult: &protocol.Snapshot{
			TerminalID: "term-1",
			Size:       protocol.Size{Cols: 80, Rows: 24},
		},
	}
	adapter := ProtocolTerminalServiceAdapter{Client: client}
	events, err := adapter.LiveEvents(context.Background(), TerminalLiveEventRequest{TerminalID: "term-1", Cols: 80, Rows: 24})
	if err != nil {
		t.Fatalf("live events: %v", err)
	}
	eventCh <- protocol.Event{Type: protocol.EventTerminalStateChanged, TerminalID: "term-1", StateChanged: &protocol.TerminalStateChangedData{
		NewState: "exited",
		ExitCode: &exitCode,
		ExitedAt: exitedAt,
	}}

	got := <-events
	if !got.Exited || got.ExitCode != 23 || !got.ExitedAt.Equal(exitedAt) || strings.Join(got.Command, " ") != "bash -lc make test" || got.Snapshot.State != state.TerminalLiveExited {
		t.Fatalf("expected exited metadata event, got %#v", got)
	}
}

func TestProtocolTerminalServiceAdapterCoalescesQueuedOrdinaryLiveRefreshEvents(t *testing.T) {
	eventCh := make(chan protocol.Event, 8)
	client := &fakeProtocolTerminalClient{
		eventCh: eventCh,
	}
	for i := 0; i < 5; i++ {
		eventCh <- protocol.Event{Type: protocol.EventTerminalStateChanged, TerminalID: "term-1"}
	}
	close(eventCh)

	adapter := ProtocolTerminalServiceAdapter{Client: client}
	events, err := adapter.LiveEvents(context.Background(), TerminalLiveEventRequest{TerminalID: "term-1", Cols: 80, Rows: 24})
	if err != nil {
		t.Fatalf("live events: %v", err)
	}

	select {
	case got, ok := <-events:
		if !ok {
			t.Fatal("expected coalesced live event")
		}
		if !got.Refresh || got.Ready || got.TerminalID != "term-1" {
			t.Fatalf("unexpected coalesced live event %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for coalesced live event")
	}
	select {
	case got, ok := <-events:
		if ok {
			t.Fatalf("expected ordinary live burst to produce one event, got %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for coalesced stream close")
	}
	if len(client.snapshotIDs) != 0 {
		t.Fatalf("ordinary live burst should coalesce before any snapshot request, got %#v", client.snapshotIDs)
	}
}

func TestProtocolTerminalServiceAdapterLiveEventDrainUsesQueuedBacklogOnly(t *testing.T) {
	events := make(chan protocol.Event, 1)
	events <- protocol.Event{Type: protocol.EventTerminalStateChanged, TerminalID: "term-1", Timestamp: time.Unix(2, 0)}

	got, pending, closed := drainProtocolLiveRefreshEvents(events, protocol.Event{Type: protocol.EventTerminalStateChanged, TerminalID: "term-1", Timestamp: time.Unix(1, 0)})
	if closed || pending != nil {
		t.Fatalf("unexpected drain state: pending=%#v closed=%v", pending, closed)
	}
	if !got.Timestamp.Equal(time.Unix(2, 0)) {
		t.Fatalf("expected queued latest event to win, got %#v", got)
	}

	got, pending, closed = drainProtocolLiveRefreshEvents(events, got)
	if closed || pending != nil || !got.Timestamp.Equal(time.Unix(2, 0)) {
		t.Fatalf("empty backlog should return current event immediately, got=%#v pending=%#v closed=%v", got, pending, closed)
	}
}

func TestProtocolTerminalServiceAdapterLiveEventDrainDoesNotStarveRefresh(t *testing.T) {
	eventCh := make(chan protocol.Event, maxProtocolLiveRefreshDrain+16)
	client := &fakeProtocolTerminalClient{
		eventCh: eventCh,
	}
	for i := 0; i < cap(eventCh); i++ {
		eventCh <- protocol.Event{Type: protocol.EventTerminalStateChanged, TerminalID: "term-1"}
	}

	adapter := ProtocolTerminalServiceAdapter{Client: client}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err := adapter.LiveEvents(ctx, TerminalLiveEventRequest{TerminalID: "term-1", Cols: 80, Rows: 24})
	if err != nil {
		t.Fatalf("live events: %v", err)
	}

	select {
	case got := <-events:
		cancel()
		if !got.Refresh || got.Ready || got.TerminalID != "term-1" {
			t.Fatalf("unexpected live event %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("ordinary live coalescing starved refresh invalidation")
	}
	if len(client.snapshotIDs) != 0 {
		t.Fatalf("refresh invalidation must not fetch snapshot in service layer, got %#v", client.snapshotIDs)
	}
}

func TestProtocolTerminalAdapterDoesNotUseFixedLiveFrameInterval(t *testing.T) {
	path := filepath.Join("protocol_terminal_adapter.go")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read adapter source: %v", err)
	}
	if _, err := parser.ParseFile(token.NewFileSet(), path, source, parser.ParseComments); err != nil {
		t.Fatalf("parse adapter source: %v", err)
	}
	text := string(source)
	for _, forbidden := range []string{
		"protocolLiveRefreshFrameInterval",
		"time.NewTimer(",
		"time.NewTicker(",
		"time.After(",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("protocol live refresh must be driven by queued events/backpressure, found fixed timing primitive %q", forbidden)
		}
	}
}

func TestProtocolTerminalServiceAdapterMapsTerminalPoolActions(t *testing.T) {
	exitedAt := time.Date(2026, 6, 17, 12, 46, 0, 0, time.UTC)
	exitCode := 23
	client := &fakeProtocolTerminalClient{
		listResult: &protocol.ListResult{Terminals: []protocol.TerminalInfo{{
			ID:       "term-pool",
			Name:     "日志🚀",
			Command:  []string{"bash", "-lc", "make test"},
			State:    "exited",
			CWD:      "/tmp",
			Size:     protocol.Size{Cols: 120, Rows: 36},
			Tags:     map[string]string{"role": "shell"},
			ExitCode: &exitCode,
			ExitedAt: exitedAt,
		}}},
		createResult: &protocol.CreateResult{TerminalID: "term-new", State: "running"},
	}
	adapter := ProtocolTerminalServiceAdapter{Client: client}

	list, err := adapter.List(context.Background(), TerminalListRequest{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if client.listCalls != 1 || len(list.Items) != 1 || list.Items[0].TerminalID != "term-pool" || list.Items[0].Title != "日志🚀" || list.Items[0].Tags["role"] != "shell" || list.Items[0].Cols != 120 || list.Items[0].Rows != 36 || list.Items[0].ExitCode == nil || *list.Items[0].ExitCode != 23 || !list.Items[0].ExitedAt.Equal(exitedAt) || strings.Join(list.Items[0].Command, " ") != "bash -lc make test" {
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
	if err := adapter.EditTags(context.Background(), TerminalEditTagsRequest{TerminalID: "term-pool", Tags: map[string]string{"termx.size_lock": "lock"}}); err != nil {
		t.Fatalf("edit tags: %v", err)
	}
	if len(client.tagIDs) != 1 || client.tagIDs[0] != "term-pool" || client.tagSets[0]["termx.size_lock"] != "lock" || len(client.metadataIDs) != 1 {
		t.Fatalf("unexpected tag calls tags=%#v metadata=%#v", client.tagSets, client.metadataIDs)
	}
}
