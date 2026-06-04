package services

import (
	"context"
	"errors"

	"github.com/lozzow/termx/termx-tui-v3/input"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

// RequestID 标识一个异步 service 请求。
type RequestID uint64

// Valid 表示 request id 能否对应一个 in-flight 请求。
func (id RequestID) Valid() bool {
	return id != 0
}

type HistoryLatestRequest struct {
	RequestID  RequestID
	TerminalID string
	Cols       int
	Rows       int
}

type HistoryOlderRequest struct {
	RequestID  RequestID
	TerminalID string
	Cols       int
	Rows       int
	Token      string
	Generation uint64
	Cursor     state.HistoryCursor
	Boundary   state.HistoryBoundary
}

type HistoryResult struct {
	RequestID RequestID
	Window    state.HistoryWindow
}

type CoreClient interface {
	HistoryLatest(context.Context, HistoryLatestRequest) (HistoryResult, error)
	HistoryOlder(context.Context, HistoryOlderRequest) (HistoryResult, error)
}

type TerminalService interface {
	Attach(context.Context, TerminalAttachRequest) (TerminalAttachResult, error)
	List(context.Context, TerminalListRequest) (TerminalListResult, error)
	Create(context.Context, TerminalCreateRequest) (TerminalCreateResult, error)
	Restart(context.Context, TerminalRestartRequest) error
	Reconnect(context.Context, TerminalReconnectRequest) (TerminalAttachResult, error)
	SendInput(context.Context, TerminalInputRequest) error
	Resize(context.Context, TerminalResizeRequest) error
}

type TerminalPoolItem struct {
	TerminalID string
	Title      string
	State      string
	CWD        string
	Tags       map[string]string
}

type TerminalListRequest struct{}

type TerminalListResult struct {
	Items []TerminalPoolItem
}

type TerminalCreateRequest struct {
	TerminalID string
	Title      string
	Command    []string
	Cols       int
	Rows       int
}

type TerminalCreateResult struct {
	TerminalID string
	State      string
}

type TerminalAttachRequest struct {
	TerminalID   string
	Cols         int
	Rows         int
	Mode         string
	ResizePolicy string
	SurfaceID    string
	ViewID       string
}

type TerminalAttachResult struct {
	TerminalID string
	Channel    uint16
	Cols       int
	Rows       int
	CanResize  bool
}

type TerminalRestartRequest struct {
	TerminalID string
}

type TerminalReconnectRequest struct {
	TerminalID   string
	Cols         int
	Rows         int
	Mode         string
	ResizePolicy string
	SurfaceID    string
	ViewID       string
}

type TerminalInputRequest struct {
	TerminalID string
	Channel    uint16
	Event      input.InputEvent
	Bytes      []byte
}

type TerminalResizeRequest struct {
	TerminalID string
	Channel    uint16
	Cols       int
	Rows       int
	SurfaceID  string
	ViewID     string
}

type TerminalSurfaceResult struct {
	Snapshot state.LiveSurfaceSnapshot
}

type SessionService interface {
	Load(context.Context) (SessionSnapshot, error)
	Save(context.Context, SessionSnapshot) error
}

type SessionSnapshot struct {
	ActiveTerminalID string
}

type ClipboardService interface {
	Write(context.Context, ClipboardWriteRequest) error
}

type ClipboardWriteRequest struct {
	Text string
}

var (
	ErrMissingHistoryResponse = errors.New("missing history response")
	ErrUnexpectedHistoryCall  = errors.New("unexpected history call")
	ErrMissingTerminalClient  = errors.New("missing terminal client")
)

type FakeCoreClient struct {
	LatestResponses []HistoryResult
	OlderResponses  []HistoryResult
	LatestRequests  []HistoryLatestRequest
	OlderRequests   []HistoryOlderRequest
}

func (client *FakeCoreClient) HistoryLatest(_ context.Context, req HistoryLatestRequest) (HistoryResult, error) {
	client.LatestRequests = append(client.LatestRequests, req)
	if len(client.LatestResponses) == 0 {
		return HistoryResult{}, ErrMissingHistoryResponse
	}
	result := client.LatestResponses[0]
	client.LatestResponses = client.LatestResponses[1:]
	result.RequestID = req.RequestID
	return result, nil
}

func (client *FakeCoreClient) HistoryOlder(_ context.Context, req HistoryOlderRequest) (HistoryResult, error) {
	client.OlderRequests = append(client.OlderRequests, req)
	if len(client.OlderResponses) == 0 {
		return HistoryResult{}, ErrMissingHistoryResponse
	}
	result := client.OlderResponses[0]
	client.OlderResponses = client.OlderResponses[1:]
	result.RequestID = req.RequestID
	return result, nil
}

type FakeTerminalService struct {
	AttachResult TerminalAttachResult
	ListResult   TerminalListResult
	CreateResult TerminalCreateResult
	AttachErr    error
	ListErr      error
	CreateErr    error
	RestartErr   error
	ReconnectErr error
	InputErr     error
	ResizeErr    error
	Attaches     []TerminalAttachRequest
	Lists        []TerminalListRequest
	Creates      []TerminalCreateRequest
	Restarts     []TerminalRestartRequest
	Reconnects   []TerminalReconnectRequest
	Inputs       []TerminalInputRequest
	Resizes      []TerminalResizeRequest
}

func (service *FakeTerminalService) Attach(_ context.Context, req TerminalAttachRequest) (TerminalAttachResult, error) {
	service.Attaches = append(service.Attaches, req)
	if service.AttachErr != nil {
		return TerminalAttachResult{}, service.AttachErr
	}
	result := service.AttachResult
	if result.TerminalID == "" {
		result.TerminalID = req.TerminalID
	}
	if result.Cols == 0 {
		result.Cols = req.Cols
	}
	if result.Rows == 0 {
		result.Rows = req.Rows
	}
	return result, nil
}

func (service *FakeTerminalService) List(_ context.Context, req TerminalListRequest) (TerminalListResult, error) {
	service.Lists = append(service.Lists, req)
	if service.ListErr != nil {
		return TerminalListResult{}, service.ListErr
	}
	return TerminalListResult{Items: cloneTerminalPoolItems(service.ListResult.Items)}, nil
}

func (service *FakeTerminalService) Create(_ context.Context, req TerminalCreateRequest) (TerminalCreateResult, error) {
	service.Creates = append(service.Creates, req)
	if service.CreateErr != nil {
		return TerminalCreateResult{}, service.CreateErr
	}
	result := service.CreateResult
	if result.TerminalID == "" {
		result.TerminalID = req.TerminalID
	}
	if result.State == "" {
		result.State = "running"
	}
	return result, nil
}

func (service *FakeTerminalService) Restart(_ context.Context, req TerminalRestartRequest) error {
	service.Restarts = append(service.Restarts, req)
	return service.RestartErr
}

func (service *FakeTerminalService) Reconnect(ctx context.Context, req TerminalReconnectRequest) (TerminalAttachResult, error) {
	service.Reconnects = append(service.Reconnects, req)
	if service.ReconnectErr != nil {
		return TerminalAttachResult{}, service.ReconnectErr
	}
	return service.Attach(ctx, TerminalAttachRequest{
		TerminalID:   req.TerminalID,
		Cols:         req.Cols,
		Rows:         req.Rows,
		Mode:         req.Mode,
		ResizePolicy: req.ResizePolicy,
		SurfaceID:    req.SurfaceID,
		ViewID:       req.ViewID,
	})
}

func (service *FakeTerminalService) SendInput(_ context.Context, req TerminalInputRequest) error {
	service.Inputs = append(service.Inputs, req)
	return service.InputErr
}

func (service *FakeTerminalService) Resize(_ context.Context, req TerminalResizeRequest) error {
	service.Resizes = append(service.Resizes, req)
	return service.ResizeErr
}

func cloneTerminalPoolItems(items []TerminalPoolItem) []TerminalPoolItem {
	if len(items) == 0 {
		return nil
	}
	cloned := make([]TerminalPoolItem, len(items))
	for i, item := range items {
		cloned[i] = item
		if len(item.Tags) > 0 {
			cloned[i].Tags = make(map[string]string, len(item.Tags))
			for key, value := range item.Tags {
				cloned[i].Tags[key] = value
			}
		}
	}
	return cloned
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

type FakeSessionService struct {
	Snapshot SessionSnapshot
	Saved    []SessionSnapshot
}

func (service *FakeSessionService) Load(context.Context) (SessionSnapshot, error) {
	return service.Snapshot, nil
}

func (service *FakeSessionService) Save(_ context.Context, snapshot SessionSnapshot) error {
	service.Saved = append(service.Saved, snapshot)
	return nil
}

type FakeClipboardService struct {
	Writes []ClipboardWriteRequest
}

func (service *FakeClipboardService) Write(_ context.Context, req ClipboardWriteRequest) error {
	service.Writes = append(service.Writes, req)
	return nil
}
