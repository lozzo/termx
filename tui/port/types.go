package port

import (
	"context"
	"errors"
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
// Missing 表示 base path 不存在或不可读，属于 prompt 空态；EndpointID 由 client runtime
// adapter 回填，确保异步结果不会覆盖其它 endpoint 的输入。
type PathListDirectoriesResult struct {
	EndpointID state.EndpointID
	BasePath   string
	Entries    []PathDirectoryEntry
	Missing    bool
	Truncated  bool
}

// PathDefaultsRequest 描述一次 endpoint 创建默认值查询。
// EndpointID 只在 client runtime adapter 层用于选择 daemon；进入 protocol adapter 前必须清空。
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

// EndpointRuntimeEvent 是共享 client runtime 发布给 TUI adapter 的连接生命周期事件。
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
