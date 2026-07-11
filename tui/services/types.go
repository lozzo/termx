package services

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"github.com/lozzow/termx/proto/cloudpb"
	"github.com/lozzow/termx/shared/cloudcompanion"
	"github.com/lozzow/termx/tui/input"
	"github.com/lozzow/termx/tui/state"
)

// RequestID 标识一个异步 service 请求。
type RequestID uint64

// Valid 表示 request id 能否对应一个 in-flight 请求。
func (id RequestID) Valid() bool {
	return id != 0
}

type HistoryLatestRequest struct {
	EndpointID state.EndpointID
	RequestID  RequestID
	PaneID     string
	ViewID     string
	TerminalID string
	Cols       int
	Rows       int
	// GenerationBoundary 是显式请求历史 generation 上界的可选参数；
	// copy 入口不能用 live surface revision 填它，frozen snapshot 边界由 core 建立。
	GenerationBoundary uint64
}

type HistoryOlderRequest struct {
	EndpointID state.EndpointID
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

type HistoryNewerRequest struct {
	EndpointID state.EndpointID
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
	EndpointID state.EndpointID
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

type HistoryReleaseRequest struct {
	EndpointID state.EndpointID
	TerminalID string
	Token      string
}

type HistoryCopyRangeRequest struct {
	EndpointID state.EndpointID
	TerminalID string
	Cols       int
	Token      string
	Generation uint64
	Boundary   state.HistoryBoundary
	Start      state.CopyLogicalPosition
	End        state.CopyLogicalPosition
}

type HistoryResult struct {
	RequestID RequestID
	Window    state.HistoryWindow
}

type HistoryCopyRangeResult struct {
	Text string
}

type CoreClient interface {
	HistoryLatest(context.Context, HistoryLatestRequest) (HistoryResult, error)
	HistoryOlder(context.Context, HistoryOlderRequest) (HistoryResult, error)
	HistoryNewer(context.Context, HistoryNewerRequest) (HistoryResult, error)
	HistoryOldest(context.Context, HistoryOldestRequest) (HistoryResult, error)
	HistoryCopyRange(context.Context, HistoryCopyRangeRequest) (HistoryCopyRangeResult, error)
	ReleaseHistory(context.Context, HistoryReleaseRequest) error
}

type TerminalService interface {
	Attach(context.Context, TerminalAttachRequest) (TerminalAttachResult, error)
	Detach(context.Context, TerminalDetachRequest) error
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

// PathService 是 TUI/client 对 endpoint daemon 文件系统只读查询的 service 边界。
// 它只用于 prompt/workdir 这类 endpoint-scoped path completion 和创建默认值；
// 目录、默认 shell 与默认 cwd truth 来自 owning daemon 机器，service 不缓存、
// 不持久化，也不改写 terminal lifecycle。
type PathService interface {
	ListDirectories(context.Context, PathListDirectoriesRequest) (PathListDirectoriesResult, error)
	Defaults(context.Context, PathDefaultsRequest) (PathDefaultsResult, error)
}

// NativeScreenSource 是 TUI live render loop 拉取 core latest native screen 的唯一接口。
// 它不返回 history token、scrollback 或 lifecycle truth；调用方只能把结果用于当前实时显示。
type NativeScreenSource interface {
	LiveSurface(context.Context, TerminalSurfaceRequest) (TerminalSurfaceResult, error)
}

// LiveInvalidationSource 只提供 one-shot live screen 失效唤醒。
// 调用方每次 arm 最多得到一次通知；通知不是 frame delivery，也不保证中间 revision 可补取。
type LiveInvalidationSource interface {
	ArmLiveInvalidation(context.Context, TerminalLiveEventRequest) (TerminalLiveEvent, error)
}

type TerminalSurfaceService interface {
	NativeScreenSource
}

type TerminalLiveEventService interface {
	LiveInvalidationSource
}

type TerminalPoolItem struct {
	EndpointID      state.EndpointID
	TerminalID      string
	Title           string
	State           string
	CWD             string
	Command         []string
	Tags            map[string]string
	ExitCode        *int
	ExitedAt        time.Time
	Cols            int
	Rows            int
	AttachmentCount int
	Resources       TerminalResourceUsage
}

// TerminalResourceUsage 是 services 层从 core protocol 映射来的 terminal 资源诊断投影；
// 真值来自 core-v2 TerminalProcess 的 OS 采样，service 不缓存也不推断进程状态。
type TerminalResourceUsage struct {
	PID            int
	CPUPercentX100 int
	MemoryBytes    uint64
	SampledAt      time.Time
}

type TerminalListRequest struct {
	EndpointID state.EndpointID
}

type TerminalListResult struct {
	Items []TerminalPoolItem
}

// PathListDirectoriesRequest 描述一次目录候选查询。
// EndpointID 只在 client/TUI manager 层用于路由；进入单 daemon adapter 前必须剥离。
type PathListDirectoriesRequest struct {
	EndpointID state.EndpointID
	Prefix     string
	Limit      int
}

// PathDirectoryEntry 是可直接写回 prompt 的目录候选投影。
// Path 保留用户输入风格（例如 ~/ 或相对路径），Name 只用于排序和测试断言。
type PathDirectoryEntry struct {
	Name string
	Path string
}

// PathListDirectoriesResult 是目录候选查询结果。
// Missing 表示 base path 不存在或不可读，属于 prompt 空态；EndpointID 由 EndpointManager
// 回填，确保异步结果不会覆盖其它 endpoint 的输入。
type PathListDirectoriesResult struct {
	EndpointID state.EndpointID
	BasePath   string
	Entries    []PathDirectoryEntry
	Missing    bool
	Truncated  bool
}

// PathDefaultsRequest 描述一次 endpoint 创建默认值查询。
// EndpointID 只在 EndpointManager 层用于选择 daemon；进入 protocol adapter 前必须清空。
type PathDefaultsRequest struct {
	EndpointID state.EndpointID
}

// PathDefaultsResult 是 endpoint daemon 返回给 TUI 的创建默认值投影。
// DefaultCommand/DefaultCWD 来自 daemon 进程所在机器，TUI 不得用本地 SHELL 或 cwd 覆盖。
type PathDefaultsResult struct {
	EndpointID     state.EndpointID
	DefaultCommand []string
	DefaultCWD     string
}

type TerminalCreateRequest struct {
	EndpointID state.EndpointID
	TerminalID string
	Title      string
	Command    []string
	CWD        string
	Tags       map[string]string
	Cols       int
	Rows       int
}

type TerminalCreateResult struct {
	EndpointID state.EndpointID
	TerminalID string
	State      string
}

type TerminalAttachRequest struct {
	EndpointID   state.EndpointID
	TerminalID   string
	Cols         int
	Rows         int
	Mode         string
	ResizePolicy string
	SurfaceID    string
	ViewID       string
}

type TerminalDetachRequest struct {
	EndpointID state.EndpointID
	TerminalID string
	Channel    uint16
	SurfaceID  string
	ViewID     string
}

type TerminalAttachResult struct {
	EndpointID      state.EndpointID
	TerminalID      string
	Channel         uint16
	Cols            int
	Rows            int
	CanResize       bool
	SizeLocked      bool
	ControlReason   string
	OwnerSurfaceID  string
	OwnerViewID     string
	ResizeEpoch     uint64
	ResizePolicy    string
	SurfaceID       string
	ViewID          string
	AttachmentCount int
}

type TerminalRestartRequest struct {
	EndpointID state.EndpointID
	TerminalID string
}

type TerminalReconnectRequest struct {
	EndpointID   state.EndpointID
	TerminalID   string
	Cols         int
	Rows         int
	Mode         string
	ResizePolicy string
	SurfaceID    string
	ViewID       string
}

type TerminalKillRequest struct {
	EndpointID state.EndpointID
	TerminalID string
}

type TerminalRemoveRequest struct {
	EndpointID state.EndpointID
	TerminalID string
}

type TerminalEditMetadataRequest struct {
	EndpointID state.EndpointID
	TerminalID string
	Title      string
	Tags       map[string]string
}

type TerminalEditTagsRequest struct {
	EndpointID state.EndpointID
	TerminalID string
	Tags       map[string]string
}

type TerminalInputRequest struct {
	EndpointID state.EndpointID
	TerminalID string
	Channel    uint16
	SurfaceID  string
	ViewID     string
	Event      input.InputEvent
	Bytes      []byte
}

type TerminalResizeRequest struct {
	EndpointID   state.EndpointID
	TerminalID   string
	Channel      uint16
	Cols         int
	Rows         int
	ResizePolicy string
	SurfaceID    string
	ViewID       string
}

type TerminalResizeResult struct {
	EndpointID      state.EndpointID
	TerminalID      string
	Cols            int
	Rows            int
	Resized         bool
	CanResize       bool
	SizeLocked      bool
	ControlReason   string
	OwnerSurfaceID  string
	OwnerViewID     string
	ResizeEpoch     uint64
	ResizePolicy    string
	SurfaceID       string
	ViewID          string
	AttachmentCount int
}

type TerminalSurfaceResult struct {
	Snapshot state.LiveSurfaceSnapshot
	Ready    bool
	// 中文说明：只标记这一次 service result 来自 core lifecycle 查询；不要写进 TUI store。
	LifecycleKnown bool
}

type TerminalSurfaceRequest struct {
	EndpointID state.EndpointID
	TerminalID string
	Cols       int
	Rows       int
}

// TerminalLiveEventRequest 是 TUI service 层 one-shot live wake 请求。
// ObservedRevision 来自 core native screen/wake 的已观察版本，只用于补 arm 间隙；
// 它不是 FrameSink 写出进度，不能把 TUI 渲染状态反传成 core truth。
type TerminalLiveEventRequest struct {
	EndpointID       state.EndpointID
	TerminalID       string
	Cols             int
	Rows             int
	ObservedRevision uint64
}

type TerminalLiveEvent struct {
	EndpointID state.EndpointID
	TerminalID string
	Snapshot   state.LiveSurfaceSnapshot
	Refresh    bool
	// 中文说明：只标记这一次 event 承载 core lifecycle 变化；reducer 用完即丢。
	LifecycleKnown       bool
	Exited               bool
	ExitCode             int
	ExitedAt             time.Time
	Command              []string
	Reason               string
	Tags                 map[string]string
	Metadata             bool
	AttachmentProjection bool
	AttachmentCount      int
	OwnerSurfaceID       string
	OwnerViewID          string
	ResizeEpoch          uint64
	SizeLocked           bool
	Err                  error
	Ready                bool
}

// EndpointRuntimeEvent 是 endpoint manager 主动发布的连接生命周期事件。
// 它只描述某个 EndpointID 的 transport/protocol 状态，不携带 terminal lifecycle truth；
// TUI reducer 应把它投影到对应 pane/manager/picker，而不是升级成全局 toast。
type EndpointRuntimeEvent struct {
	EndpointID state.EndpointID
	Status     state.EndpointStatusKind
	ErrorKind  state.EndpointErrorKind
	// Phase 是 managed WebRTC 当前 resolving/signaling/connecting/authorizing/connected/failed 阶段。
	// local/SSH 保持空值；它只用于 reducer 展示，不能替代 Status、授权结果或实际 ObservedPath。
	Phase cloudcompanion.EndpointPhase
	// ObservedPath 是 managed WebRTC 已建立连接的 direct/single_relay/relay_mesh 运行时投影。
	// 它不参与 endpoint 路由或授权，空值表示 local/SSH 或尚未观测到 candidate path。
	ObservedPath string
	// RouteSelectionReason 是 SmartRoute 的稳定公开诊断，不包含候选分数、成本或内部权重。
	RouteSelectionReason string
	Message              string
	Err                  error
}

// ReportEndpointDialPhase 把 managed dialer 的公开阶段写回当前 EndpointManager dial context。
// 缺少 manager sink 时保持 no-op；调用方不得通过该函数修改 reducer state、选择其他 transport 或携带 credential。
func ReportEndpointDialPhase(ctx context.Context, phase cloudcompanion.EndpointPhase) {
	if ctx == nil {
		return
	}
	if sink, ok := ctx.Value(endpointDialProgressContextKey{}).(func(cloudcompanion.EndpointPhase)); ok && sink != nil {
		sink(phase)
	}
}

type endpointDialProgressContextKey struct{}

// EndpointEventSource 提供 endpoint-scoped 生命周期事件订阅。
// 该接口用于主动侦测 transport 关闭；订阅者只能通过 message path 回写 reducer state。
type EndpointEventSource interface {
	WatchEndpointEvents(context.Context) (<-chan EndpointRuntimeEvent, error)
}

// EndpointLifecycle 描述一个已连接 service bundle 的底层连接生命周期。
// Done 来自 transport/protocol close signal；Err 在 Done 后返回关闭原因，供 UI 分类展示。
type EndpointLifecycle struct {
	Done <-chan struct{}
	Err  func() error
}

// ClassifyEndpointError 把 service/transport 错误归类为 endpoint UI 错误类型。
// 分类结果不参与重试或安全判断，只让 picker、manager 和 workbench 展示一致的失败标志。
func ClassifyEndpointError(err error) state.EndpointErrorKind {
	if err == nil {
		return state.EndpointErrorUnknown
	}
	switch cloudcompanion.CodeOf(err) {
	case cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_LOGIN_REQUIRED,
		cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_DEVICE_ENROLLMENT_REQUIRED,
		cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED:
		return state.EndpointErrorAuth
	case cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_INCOMPATIBLE,
		cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_UNTRUSTED,
		cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL:
		return state.EndpointErrorProtocol
	case cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_MISSING,
		cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_NOT_RUNNING,
		cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_DEVICE_NOT_FOUND,
		cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_ENTITLEMENT_DENIED,
		cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_QUOTA_EXHAUSTED,
		cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_REGION_UNAVAILABLE,
		cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_ROUTE_UNAVAILABLE,
		cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_BACKPRESSURE,
		cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY:
		return state.EndpointErrorUnavailable
	}
	return state.ClassifyEndpointErrorText(err.Error())
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
	ErrStaleHistoryWindow       = errors.New("stale history window")
	ErrMissingTerminalClient    = errors.New("missing terminal client")
	ErrWorkbenchStorageConflict = errors.New("workbench storage version conflict")
	ErrClipboardStorageConflict = errors.New("clipboard storage version conflict")
)

type FakeCoreClient struct {
	LatestResponses []HistoryResult
	OlderResponses  []HistoryResult
	NewerResponses  []HistoryResult
	OldestResponses []HistoryResult
	CopyResponses   []HistoryCopyRangeResult
	LatestRequests  []HistoryLatestRequest
	OlderRequests   []HistoryOlderRequest
	NewerRequests   []HistoryNewerRequest
	OldestRequests  []HistoryOldestRequest
	CopyRequests    []HistoryCopyRangeRequest
	ReleaseRequests []HistoryReleaseRequest
	ReleaseErr      error
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

func (client *FakeCoreClient) HistoryNewer(_ context.Context, req HistoryNewerRequest) (HistoryResult, error) {
	client.NewerRequests = append(client.NewerRequests, req)
	if len(client.NewerResponses) == 0 {
		return HistoryResult{}, ErrMissingHistoryResponse
	}
	result := client.NewerResponses[0]
	client.NewerResponses = client.NewerResponses[1:]
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

func (client *FakeCoreClient) HistoryCopyRange(_ context.Context, req HistoryCopyRangeRequest) (HistoryCopyRangeResult, error) {
	client.CopyRequests = append(client.CopyRequests, req)
	if len(client.CopyResponses) == 0 {
		return HistoryCopyRangeResult{}, ErrMissingHistoryResponse
	}
	result := client.CopyResponses[0]
	client.CopyResponses = client.CopyResponses[1:]
	return result, nil
}

func (client *FakeCoreClient) ReleaseHistory(_ context.Context, req HistoryReleaseRequest) error {
	client.ReleaseRequests = append(client.ReleaseRequests, req)
	return client.ReleaseErr
}

type FakeTerminalService struct {
	AttachResult             TerminalAttachResult
	ListResult               TerminalListResult
	CreateResult             TerminalCreateResult
	SurfaceResult            TerminalSurfaceResult
	AttachErr                error
	ListErr                  error
	CreateErr                error
	RestartErr               error
	ReconnectErr             error
	KillErr                  error
	RemoveErr                error
	EditErr                  error
	EditTagsErr              error
	InputErr                 error
	ResizeErr                error
	ResizeResult             TerminalResizeResult
	SurfaceErr               error
	LiveInvalidationsCh      chan TerminalLiveEvent
	LiveInvalidationsErr     error
	PathResult               PathListDirectoriesResult
	PathErr                  error
	PathDefaultsResult       PathDefaultsResult
	PathDefaultsErr          error
	Attaches                 []TerminalAttachRequest
	Detaches                 []TerminalDetachRequest
	Lists                    []TerminalListRequest
	Creates                  []TerminalCreateRequest
	Restarts                 []TerminalRestartRequest
	Reconnects               []TerminalReconnectRequest
	Kills                    []TerminalKillRequest
	Removes                  []TerminalRemoveRequest
	Edits                    []TerminalEditMetadataRequest
	TagEdits                 []TerminalEditTagsRequest
	Inputs                   []TerminalInputRequest
	Resizes                  []TerminalResizeRequest
	Surfaces                 []TerminalSurfaceRequest
	LiveInvalidationRequests []TerminalLiveEventRequest
	PathRequests             []PathListDirectoriesRequest
	PathDefaultsRequests     []PathDefaultsRequest
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
	if result.EndpointID == "" {
		result.EndpointID = req.EndpointID
	}
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

func (service *FakeTerminalService) Detach(_ context.Context, req TerminalDetachRequest) error {
	service.Detaches = append(service.Detaches, req)
	return nil
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
	if result.EndpointID == "" {
		result.EndpointID = req.EndpointID
	}
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
		EndpointID:   req.EndpointID,
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
		EndpointID: req.EndpointID,
		TerminalID: req.TerminalID,
		Title:      req.Title,
		Tags:       cloneStringMap(req.Tags),
	})
	return service.EditErr
}

func (service *FakeTerminalService) EditTags(_ context.Context, req TerminalEditTagsRequest) error {
	service.TagEdits = append(service.TagEdits, TerminalEditTagsRequest{
		EndpointID: req.EndpointID,
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
	if result.EndpointID == "" {
		result.EndpointID = req.EndpointID
	}
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
	if result.CanResize && result.ResizePolicy == state.TerminalResizeRoleOwner {
		if result.OwnerSurfaceID == "" {
			result.OwnerSurfaceID = result.SurfaceID
		}
		if result.OwnerViewID == "" {
			result.OwnerViewID = result.ViewID
		}
	}
	return result, nil
}

func (service *FakeTerminalService) ListDirectories(_ context.Context, req PathListDirectoriesRequest) (PathListDirectoriesResult, error) {
	service.PathRequests = append(service.PathRequests, req)
	if service.PathErr != nil {
		return PathListDirectoriesResult{}, service.PathErr
	}
	result := service.PathResult
	result.Entries = clonePathDirectoryEntries(result.Entries)
	if result.EndpointID == "" {
		result.EndpointID = req.EndpointID
	}
	return result, nil
}

func (service *FakeTerminalService) Defaults(_ context.Context, req PathDefaultsRequest) (PathDefaultsResult, error) {
	service.PathDefaultsRequests = append(service.PathDefaultsRequests, req)
	if service.PathDefaultsErr != nil {
		return PathDefaultsResult{}, service.PathDefaultsErr
	}
	result := service.PathDefaultsResult
	result.DefaultCommand = append([]string(nil), result.DefaultCommand...)
	if result.EndpointID == "" {
		result.EndpointID = req.EndpointID
	}
	return result, nil
}

func (service *FakeTerminalService) LiveSurface(_ context.Context, req TerminalSurfaceRequest) (TerminalSurfaceResult, error) {
	service.Surfaces = append(service.Surfaces, req)
	if service.SurfaceErr != nil {
		return TerminalSurfaceResult{}, service.SurfaceErr
	}
	result := service.SurfaceResult
	if result.Snapshot.EndpointID == "" {
		result.Snapshot.EndpointID = req.EndpointID
	}
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

func (service *FakeTerminalService) ArmLiveInvalidation(ctx context.Context, req TerminalLiveEventRequest) (TerminalLiveEvent, error) {
	service.LiveInvalidationRequests = append(service.LiveInvalidationRequests, req)
	if service.LiveInvalidationsErr != nil {
		return TerminalLiveEvent{}, service.LiveInvalidationsErr
	}
	if service.LiveInvalidationsCh != nil {
		select {
		case event, ok := <-service.LiveInvalidationsCh:
			if !ok {
				return TerminalLiveEvent{}, context.Canceled
			}
			if event.EndpointID == "" {
				event.EndpointID = req.EndpointID
			}
			if event.Snapshot.EndpointID == "" {
				event.Snapshot.EndpointID = req.EndpointID
			}
			return event, nil
		case <-ctx.Done():
			return TerminalLiveEvent{}, ctx.Err()
		}
	}
	<-ctx.Done()
	return TerminalLiveEvent{}, ctx.Err()
}

func clonePathDirectoryEntries(entries []PathDirectoryEntry) []PathDirectoryEntry {
	if len(entries) == 0 {
		return nil
	}
	cloned := make([]PathDirectoryEntry, len(entries))
	copy(cloned, entries)
	return cloned
}

func cloneTerminalPoolItems(items []TerminalPoolItem) []TerminalPoolItem {
	if len(items) == 0 {
		return nil
	}
	cloned := make([]TerminalPoolItem, len(items))
	for i, item := range items {
		cloned[i] = item
		cloned[i].Command = append([]string(nil), item.Command...)
		if item.ExitCode != nil {
			code := *item.ExitCode
			cloned[i].ExitCode = &code
		}
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
