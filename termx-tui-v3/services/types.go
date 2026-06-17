package services

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"

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
	PaneID     string
	ViewID     string
	TerminalID string
	Cols       int
	Rows       int
}

type HistoryOlderRequest struct {
	RequestID  RequestID
	PaneID     string
	ViewID     string
	TerminalID string
	Cols       int
	Rows       int
	Token      string
	Generation uint64
	Cursor     state.HistoryCursor
	Boundary   state.HistoryBoundary
}

type HistoryOldestRequest struct {
	RequestID  RequestID
	PaneID     string
	ViewID     string
	TerminalID string
	Cols       int
	Rows       int
	Token      string
	Generation uint64
	Boundary   state.HistoryBoundary
}

type HistoryResult struct {
	RequestID RequestID
	Window    state.HistoryWindow
}

type CoreClient interface {
	HistoryLatest(context.Context, HistoryLatestRequest) (HistoryResult, error)
	HistoryOlder(context.Context, HistoryOlderRequest) (HistoryResult, error)
	HistoryOldest(context.Context, HistoryOldestRequest) (HistoryResult, error)
}

type TerminalService interface {
	Attach(context.Context, TerminalAttachRequest) (TerminalAttachResult, error)
	List(context.Context, TerminalListRequest) (TerminalListResult, error)
	Create(context.Context, TerminalCreateRequest) (TerminalCreateResult, error)
	Restart(context.Context, TerminalRestartRequest) error
	Reconnect(context.Context, TerminalReconnectRequest) (TerminalAttachResult, error)
	Kill(context.Context, TerminalKillRequest) error
	Remove(context.Context, TerminalRemoveRequest) error
	EditMetadata(context.Context, TerminalEditMetadataRequest) error
	EditTags(context.Context, TerminalEditTagsRequest) error
	SendInput(context.Context, TerminalInputRequest) error
	Resize(context.Context, TerminalResizeRequest) (TerminalResizeResult, error)
}

type TerminalSurfaceService interface {
	LiveSurface(context.Context, TerminalSurfaceRequest) (TerminalSurfaceResult, error)
}

type TerminalLiveEventService interface {
	LiveEvents(context.Context, TerminalLiveEventRequest) (<-chan TerminalLiveEvent, error)
}

type TerminalPoolItem struct {
	TerminalID string
	Title      string
	State      string
	CWD        string
	Tags       map[string]string
	Cols       int
	Rows       int
}

type TerminalListRequest struct{}

type TerminalListResult struct {
	Items []TerminalPoolItem
}

type TerminalCreateRequest struct {
	TerminalID string
	Title      string
	Command    []string
	CWD        string
	Tags       map[string]string
	Cols       int
	Rows       int
}

type TerminalCreateResult struct {
	TerminalID string
	State      string
}

func DefaultTerminalCommand() []string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	// TUI-v3 的 first-party create 必须总是给 core-v2 一个真实命令，不能发送空 command。
	return []string{shell}
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
	TerminalID     string
	Channel        uint16
	Cols           int
	Rows           int
	CanResize      bool
	SizeLocked     bool
	ControlReason  string
	OwnerSurfaceID string
	OwnerViewID    string
	ResizeEpoch    uint64
	ResizePolicy   string
	SurfaceID      string
	ViewID         string
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

type TerminalKillRequest struct {
	TerminalID string
}

type TerminalRemoveRequest struct {
	TerminalID string
}

type TerminalEditMetadataRequest struct {
	TerminalID string
	Title      string
	Tags       map[string]string
}

type TerminalEditTagsRequest struct {
	TerminalID string
	Tags       map[string]string
}

type TerminalInputRequest struct {
	TerminalID string
	Channel    uint16
	Event      input.InputEvent
	Bytes      []byte
}

type TerminalResizeRequest struct {
	TerminalID   string
	Channel      uint16
	Cols         int
	Rows         int
	ResizePolicy string
	SurfaceID    string
	ViewID       string
}

type TerminalResizeResult struct {
	TerminalID     string
	Cols           int
	Rows           int
	Resized        bool
	CanResize      bool
	SizeLocked     bool
	ControlReason  string
	OwnerSurfaceID string
	OwnerViewID    string
	ResizeEpoch    uint64
	ResizePolicy   string
	SurfaceID      string
	ViewID         string
}

type TerminalSurfaceResult struct {
	Snapshot state.LiveSurfaceSnapshot
	Ready    bool
}

type TerminalSurfaceRequest struct {
	TerminalID string
	Cols       int
	Rows       int
}

type TerminalLiveEventRequest struct {
	TerminalID string
	Cols       int
	Rows       int
}

type TerminalLiveEvent struct {
	TerminalID string
	Snapshot   state.LiveSurfaceSnapshot
	Exited     bool
	ExitCode   int
	Reason     string
	Err        error
	Ready      bool
}

type SessionService interface {
	Load(context.Context) (SessionSnapshot, error)
	Save(context.Context, SessionSnapshot) error
}

type SessionSnapshot struct {
	ActiveTerminalID string
}

type ClipboardService interface {
	Read(context.Context) (ClipboardReadResult, error)
	Write(context.Context, ClipboardWriteRequest) error
	LastCopy() string
}

type ClipboardReadResult struct {
	Text string
}

type ClipboardWriteRequest struct {
	Text string
}

type SystemClipboardService struct {
	lastCopied string
}

const clipboardCommandTimeout = 1500 * time.Millisecond

func (service *SystemClipboardService) Read(ctx context.Context) (ClipboardReadResult, error) {
	readCtx, cancel := context.WithTimeout(ctx, clipboardCommandTimeout)
	defer cancel()
	for _, spec := range clipboardReadCommands() {
		cmd := exec.CommandContext(readCtx, spec.name, spec.args...)
		out, err := cmd.Output()
		if err == nil {
			return ClipboardReadResult{Text: string(out)}, nil
		}
	}
	return ClipboardReadResult{}, fmt.Errorf("no system clipboard command available")
}

func (service *SystemClipboardService) Write(ctx context.Context, req ClipboardWriteRequest) error {
	service.lastCopied = req.Text
	if req.Text == "" {
		return nil
	}
	writeCtx, cancel := context.WithTimeout(ctx, clipboardCommandTimeout)
	defer cancel()
	for _, spec := range clipboardWriteCommands() {
		cmd := exec.CommandContext(writeCtx, spec.name, spec.args...)
		cmd.Stdin = bytes.NewBufferString(req.Text)
		if err := cmd.Run(); err == nil {
			return nil
		}
	}
	return fmt.Errorf("no system clipboard command available")
}

func (service *SystemClipboardService) LastCopy() string {
	return service.lastCopied
}

type clipboardCommandSpec struct {
	name string
	args []string
}

func clipboardWriteCommands() []clipboardCommandSpec {
	return []clipboardCommandSpec{
		{name: "wl-copy"},
		{name: "xclip", args: []string{"-selection", "clipboard", "-in"}},
		{name: "xsel", args: []string{"--clipboard", "--input"}},
		{name: "pbcopy"},
	}
}

func clipboardReadCommands() []clipboardCommandSpec {
	return []clipboardCommandSpec{
		{name: "wl-paste"},
		{name: "xclip", args: []string{"-selection", "clipboard", "-out"}},
		{name: "xsel", args: []string{"--clipboard", "--output"}},
		{name: "pbpaste"},
	}
}

type WorkbenchStorageService interface {
	LoadWorkbench(context.Context, state.WorkbenchStorageRef) (WorkbenchStorageLoadResult, error)
	SaveWorkbench(context.Context, WorkbenchStorageSaveRequest) (WorkbenchStorageSaveResult, error)
	WatchWorkbench(context.Context, state.WorkbenchStorageRef) (<-chan WorkbenchStorageEvent, error)
}

type WorkbenchStorageLoadResult struct {
	Snapshot state.WorkbenchStorageSnapshot
	Version  uint64
	Found    bool
}

type WorkbenchStorageSaveRequest struct {
	Ref             state.WorkbenchStorageRef
	Snapshot        state.WorkbenchStorageSnapshot
	CheckVersion    bool
	ExpectedVersion uint64
}

type WorkbenchStorageSaveResult struct {
	Ref     state.WorkbenchStorageRef
	Version uint64
}

type WorkbenchStorageEvent struct {
	Ref     state.WorkbenchStorageRef
	Version uint64
	Op      string
}

type ClipboardStorageService interface {
	LoadClipboard(context.Context, state.ClipboardStorageRef) (ClipboardStorageLoadResult, error)
	SaveClipboard(context.Context, ClipboardStorageSaveRequest) (ClipboardStorageSaveResult, error)
	WatchClipboard(context.Context, state.ClipboardStorageRef) (<-chan ClipboardStorageEvent, error)
}

type ClipboardStorageLoadResult struct {
	Snapshot state.ClipboardStorageSnapshot
	Version  uint64
	Found    bool
}

type ClipboardStorageSaveRequest struct {
	Ref             state.ClipboardStorageRef
	Snapshot        state.ClipboardStorageSnapshot
	CheckVersion    bool
	ExpectedVersion uint64
}

type ClipboardStorageSaveResult struct {
	Ref     state.ClipboardStorageRef
	Version uint64
}

type ClipboardStorageEvent struct {
	Ref     state.ClipboardStorageRef
	Version uint64
	Op      string
}

var (
	ErrMissingHistoryResponse   = errors.New("missing history response")
	ErrUnexpectedHistoryCall    = errors.New("unexpected history call")
	ErrMissingTerminalClient    = errors.New("missing terminal client")
	ErrWorkbenchStorageConflict = errors.New("workbench storage version conflict")
	ErrClipboardStorageConflict = errors.New("clipboard storage version conflict")
)

type FakeCoreClient struct {
	LatestResponses []HistoryResult
	OlderResponses  []HistoryResult
	OldestResponses []HistoryResult
	LatestRequests  []HistoryLatestRequest
	OlderRequests   []HistoryOlderRequest
	OldestRequests  []HistoryOldestRequest
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

func (client *FakeCoreClient) HistoryOldest(_ context.Context, req HistoryOldestRequest) (HistoryResult, error) {
	client.OldestRequests = append(client.OldestRequests, req)
	if len(client.OldestResponses) == 0 {
		return HistoryResult{}, ErrMissingHistoryResponse
	}
	result := client.OldestResponses[0]
	client.OldestResponses = client.OldestResponses[1:]
	result.RequestID = req.RequestID
	return result, nil
}

type FakeTerminalService struct {
	AttachResult      TerminalAttachResult
	ListResult        TerminalListResult
	CreateResult      TerminalCreateResult
	SurfaceResult     TerminalSurfaceResult
	AttachErr         error
	ListErr           error
	CreateErr         error
	RestartErr        error
	ReconnectErr      error
	KillErr           error
	RemoveErr         error
	EditErr           error
	EditTagsErr       error
	InputErr          error
	ResizeErr         error
	ResizeResult      TerminalResizeResult
	SurfaceErr        error
	LiveEventsCh      chan TerminalLiveEvent
	LiveEventsErr     error
	Attaches          []TerminalAttachRequest
	Lists             []TerminalListRequest
	Creates           []TerminalCreateRequest
	Restarts          []TerminalRestartRequest
	Reconnects        []TerminalReconnectRequest
	Kills             []TerminalKillRequest
	Removes           []TerminalRemoveRequest
	Edits             []TerminalEditMetadataRequest
	TagEdits          []TerminalEditTagsRequest
	Inputs            []TerminalInputRequest
	Resizes           []TerminalResizeRequest
	Surfaces          []TerminalSurfaceRequest
	LiveEventRequests []TerminalLiveEventRequest
}

type FakeWorkbenchStorageService struct {
	LoadResult     WorkbenchStorageLoadResult
	LoadErr        error
	SaveResult     WorkbenchStorageSaveResult
	SaveErr        error
	CurrentVersion uint64
	WatchCh        chan WorkbenchStorageEvent
	WatchErr       error
	Loads          []state.WorkbenchStorageRef
	Saves          []WorkbenchStorageSaveRequest
	Watches        []state.WorkbenchStorageRef
}

type FakeClipboardStorageService struct {
	LoadResult     ClipboardStorageLoadResult
	LoadErr        error
	SaveResult     ClipboardStorageSaveResult
	SaveErr        error
	CurrentVersion uint64
	WatchCh        chan ClipboardStorageEvent
	WatchErr       error
	Loads          []state.ClipboardStorageRef
	Saves          []ClipboardStorageSaveRequest
	Watches        []state.ClipboardStorageRef
}

func (service *FakeWorkbenchStorageService) LoadWorkbench(_ context.Context, ref state.WorkbenchStorageRef) (WorkbenchStorageLoadResult, error) {
	service.Loads = append(service.Loads, ref)
	if service.LoadErr != nil {
		return WorkbenchStorageLoadResult{}, service.LoadErr
	}
	if service.LoadResult.Found && service.CurrentVersion == 0 {
		service.CurrentVersion = service.LoadResult.Version
	}
	return service.LoadResult, nil
}

func (service *FakeWorkbenchStorageService) SaveWorkbench(_ context.Context, req WorkbenchStorageSaveRequest) (WorkbenchStorageSaveResult, error) {
	req.Snapshot = cloneWorkbenchStorageSnapshot(req.Snapshot)
	service.Saves = append(service.Saves, req)
	if service.SaveErr != nil {
		return WorkbenchStorageSaveResult{}, service.SaveErr
	}
	if req.CheckVersion && service.CurrentVersion != req.ExpectedVersion {
		return WorkbenchStorageSaveResult{}, ErrWorkbenchStorageConflict
	}
	result := service.SaveResult
	if result.Ref.AppID == "" {
		result.Ref = req.Ref
	}
	if result.Version == 0 {
		result.Version = req.ExpectedVersion + 1
	}
	service.CurrentVersion = result.Version
	return result, nil
}

func (service *FakeWorkbenchStorageService) WatchWorkbench(_ context.Context, ref state.WorkbenchStorageRef) (<-chan WorkbenchStorageEvent, error) {
	service.Watches = append(service.Watches, ref)
	if service.WatchErr != nil {
		return nil, service.WatchErr
	}
	if service.WatchCh == nil {
		service.WatchCh = make(chan WorkbenchStorageEvent, 16)
	}
	return service.WatchCh, nil
}

func (service *FakeClipboardStorageService) LoadClipboard(_ context.Context, ref state.ClipboardStorageRef) (ClipboardStorageLoadResult, error) {
	service.Loads = append(service.Loads, ref)
	if service.LoadErr != nil {
		return ClipboardStorageLoadResult{}, service.LoadErr
	}
	return service.LoadResult, nil
}

func (service *FakeClipboardStorageService) SaveClipboard(_ context.Context, req ClipboardStorageSaveRequest) (ClipboardStorageSaveResult, error) {
	req.Snapshot = cloneClipboardStorageSnapshot(req.Snapshot)
	service.Saves = append(service.Saves, req)
	if service.SaveErr != nil {
		return ClipboardStorageSaveResult{}, service.SaveErr
	}
	if req.CheckVersion && service.CurrentVersion != req.ExpectedVersion {
		return ClipboardStorageSaveResult{}, ErrClipboardStorageConflict
	}
	result := service.SaveResult
	if result.Ref.AppID == "" {
		result.Ref = req.Ref
	}
	if result.Version == 0 {
		result.Version = req.ExpectedVersion + 1
	}
	service.CurrentVersion = result.Version
	return result, nil
}

func (service *FakeClipboardStorageService) WatchClipboard(_ context.Context, ref state.ClipboardStorageRef) (<-chan ClipboardStorageEvent, error) {
	service.Watches = append(service.Watches, ref)
	if service.WatchErr != nil {
		return nil, service.WatchErr
	}
	if service.WatchCh == nil {
		service.WatchCh = make(chan ClipboardStorageEvent, 16)
	}
	return service.WatchCh, nil
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
	if result.ResizePolicy == "" {
		result.ResizePolicy = req.ResizePolicy
	}
	if result.SurfaceID == "" {
		result.SurfaceID = req.SurfaceID
	}
	if result.ViewID == "" {
		result.ViewID = req.ViewID
	}
	if !result.SizeLocked && result.ControlReason == "" && result.ResizePolicy == state.TerminalResizeRoleOwner {
		result.CanResize = true
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
	req.Tags = cloneStringMap(req.Tags)
	req.Command = append([]string(nil), req.Command...)
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

func (service *FakeTerminalService) Kill(_ context.Context, req TerminalKillRequest) error {
	service.Kills = append(service.Kills, req)
	return service.KillErr
}

func (service *FakeTerminalService) Remove(_ context.Context, req TerminalRemoveRequest) error {
	service.Removes = append(service.Removes, req)
	return service.RemoveErr
}

func (service *FakeTerminalService) EditMetadata(_ context.Context, req TerminalEditMetadataRequest) error {
	service.Edits = append(service.Edits, TerminalEditMetadataRequest{
		TerminalID: req.TerminalID,
		Title:      req.Title,
		Tags:       cloneStringMap(req.Tags),
	})
	return service.EditErr
}

func (service *FakeTerminalService) EditTags(_ context.Context, req TerminalEditTagsRequest) error {
	service.TagEdits = append(service.TagEdits, TerminalEditTagsRequest{
		TerminalID: req.TerminalID,
		Tags:       cloneStringMap(req.Tags),
	})
	if service.EditTagsErr != nil {
		return service.EditTagsErr
	}
	return service.EditErr
}

func (service *FakeTerminalService) SendInput(_ context.Context, req TerminalInputRequest) error {
	service.Inputs = append(service.Inputs, req)
	return service.InputErr
}

func (service *FakeTerminalService) Resize(_ context.Context, req TerminalResizeRequest) (TerminalResizeResult, error) {
	service.Resizes = append(service.Resizes, req)
	if service.ResizeErr != nil {
		return TerminalResizeResult{}, service.ResizeErr
	}
	result := service.ResizeResult
	if result.TerminalID == "" {
		result.TerminalID = req.TerminalID
	}
	if result.Cols == 0 {
		result.Cols = req.Cols
	}
	if result.Rows == 0 {
		result.Rows = req.Rows
	}
	if result.ResizePolicy == "" {
		result.ResizePolicy = req.ResizePolicy
	}
	if result.SurfaceID == "" {
		result.SurfaceID = req.SurfaceID
	}
	if result.ViewID == "" {
		result.ViewID = req.ViewID
	}
	if !result.SizeLocked && result.ControlReason == "" {
		result.Resized = true
		result.CanResize = true
	}
	return result, nil
}

func (service *FakeTerminalService) LiveSurface(_ context.Context, req TerminalSurfaceRequest) (TerminalSurfaceResult, error) {
	service.Surfaces = append(service.Surfaces, req)
	if service.SurfaceErr != nil {
		return TerminalSurfaceResult{}, service.SurfaceErr
	}
	result := service.SurfaceResult
	if result.Snapshot.TerminalID == "" {
		result.Snapshot.TerminalID = req.TerminalID
	}
	if result.Snapshot.Cols == 0 {
		result.Snapshot.Cols = req.Cols
	}
	if result.Snapshot.Rows == 0 {
		result.Snapshot.Rows = req.Rows
	}
	if result.Ready || len(result.Snapshot.Lines) > 0 || len(result.Snapshot.Screen) > 0 || result.Snapshot.Cursor.Visible {
		result.Ready = true
	}
	return result, nil
}

func (service *FakeTerminalService) LiveEvents(ctx context.Context, req TerminalLiveEventRequest) (<-chan TerminalLiveEvent, error) {
	service.LiveEventRequests = append(service.LiveEventRequests, req)
	if service.LiveEventsErr != nil {
		return nil, service.LiveEventsErr
	}
	if service.LiveEventsCh != nil {
		return service.LiveEventsCh, nil
	}
	ch := make(chan TerminalLiveEvent)
	go func() {
		<-ctx.Done()
		close(ch)
	}()
	return ch, nil
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
	ReadResult ClipboardReadResult
	ReadErr    error
	LastCopied string
	Writes     []ClipboardWriteRequest
}

func (service *FakeClipboardService) Read(_ context.Context) (ClipboardReadResult, error) {
	if service.ReadErr != nil {
		return ClipboardReadResult{}, service.ReadErr
	}
	return service.ReadResult, nil
}

func (service *FakeClipboardService) Write(_ context.Context, req ClipboardWriteRequest) error {
	service.Writes = append(service.Writes, req)
	service.LastCopied = req.Text
	return nil
}

func (service *FakeClipboardService) LastCopy() string {
	return service.LastCopied
}
