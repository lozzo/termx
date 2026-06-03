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
	SendInput(context.Context, TerminalInputRequest) error
	Resize(context.Context, TerminalResizeRequest) error
}

type TerminalInputRequest struct {
	TerminalID string
	Event      input.InputEvent
	Bytes      []byte
}

type TerminalResizeRequest struct {
	TerminalID string
	Cols       int
	Rows       int
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
	Inputs  []TerminalInputRequest
	Resizes []TerminalResizeRequest
}

func (service *FakeTerminalService) SendInput(_ context.Context, req TerminalInputRequest) error {
	service.Inputs = append(service.Inputs, req)
	return nil
}

func (service *FakeTerminalService) Resize(_ context.Context, req TerminalResizeRequest) error {
	service.Resizes = append(service.Resizes, req)
	return nil
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
