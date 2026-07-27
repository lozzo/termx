package runtime

import (
	"bytes"
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/anytty/anytty/client/endpoint"
	"github.com/anytty/anytty/proto/apipb"
	"google.golang.org/protobuf/proto"
)

// ConnectionSnapshot 是同一 ReadyPeerSession 的即时网络投影。
// Route/generation 来自 runtime；candidate/RTT 来自实际 peer stats，未知字段必须保持空值而不是推断。
type ConnectionSnapshot struct {
	RouteID             endpoint.RouteID
	RouteKind           endpoint.RouteKind
	ObservedPath        string
	SelectionReason     string
	SampledAt           time.Time
	RoundTrip           time.Duration
	LocalCandidateType  string
	RemoteCandidateType string
	LocalAddress        string
	RemoteAddress       string
	LocalPort           uint16
	RemotePort          uint16
	LocalProtocol       string
	RemoteProtocol      string
	RelayTransport      string
	NetworkClass        string
	BytesSent           uint64
	BytesReceived       uint64
	PacketsSent         uint64
	LossEvents          uint64
	Connected           bool
}

// ConnectionSnapshotProvider 由持有实际 transport 的 ReadySession 实现。
// 调用方只能采样当前 session，不能用快照驱动路由、鉴权或 generation 状态机。
type ConnectionSnapshotProvider interface {
	ConnectionSnapshot(time.Time) (ConnectionSnapshot, bool)
}

// ProtoApplicationExecutor 是 client runtime 到 protocol/platform binding 的唯一公共 API 执行边界。
// 实现只运输完整 CommandEnvelope/ResultEnvelope，不解释 terminal 字段，也不选择 route 或 generation。
type ProtoApplicationExecutor interface {
	ExecuteApplication(context.Context, *apipb.CommandEnvelope) (*apipb.ResultEnvelope, error)
}

// ApplicationSessionValidator 在任何具体 adapter 副作用前校验请求仍属于当前 EndpointSessionStamp。
// owner/shared lease 实现必须在 generation 被替换或 consumer lease 关闭后返回 Attempted=false 的 stale error。
type ApplicationSessionValidator interface {
	ValidateApplicationSession(EndpointSessionStamp) error
}

// ApplicationSessionInvalidator 在无法确认 session-owned 远端资源已销毁时撤销精确 generation。
// 实现必须使同一底层 ReadyPeerSession 的全部 consumer lease 失效并关闭 transport；调用方不得把它用于普通 consumer release。
type ApplicationSessionInvalidator interface {
	InvalidateApplicationSession(error) error
}

// TerminalResponseApplicationExecutor 在调用 context 取消后仍等待一个有界 terminal response。
// 只有会创建远端资源、且必须取得迟到结果完成销毁的 binding owner 可以选择该能力；transport 不得自行解释业务 command。
type TerminalResponseApplicationExecutor interface {
	ExecuteApplicationTerminal(context.Context, *apipb.CommandEnvelope) (*apipb.ResultEnvelope, error)
}

// ApplicationReadyPeerSession 是已经完成 transport、授权与 protocol Hello 的可执行 session。
// route adapter 返回该接口后，runtime/binding 只能通过 generated Proto command/event 访问 daemon；关闭和 generation fence 仍由 ReadyPeerSession 约束。
type ApplicationReadyPeerSession interface {
	ReadyPeerSession
	ProtoApplicationExecutor
	ApplicationEvents(context.Context) (<-chan *apipb.EventEnvelope, error)
}

// ResourceStreamSession 是 ready connection 对 session-bound stream resource 的内部 framing 能力。
// resource 真值来自 apipb.ResourceHandle；frame type 与 payload 只在 protocol/binding adapter 间运输，不成为第二套业务 API。
type ResourceStreamSession interface {
	OpenResourceStream(*apipb.ResourceHandle) (ResourceStream, error)
}

// ApplicationAttachmentSession 是 protocol connection 对 attachment resource 与私有 stream channel 的相关性投影。
// channel 只在当前 ReadyPeerSession 内有效；TUI 必须携带原 resource/stamp，不能把 uint16 channel 当成跨 generation API identity。
type ApplicationAttachmentSession interface {
	ApplicationAttachmentChannel(*apipb.ResourceHandle) (uint16, bool)
	ApplicationAttachment(uint16) (*apipb.ResourceHandle, bool)
}

// ResourceStream 是 attachment/file resource 对应的有界有序 frame stream。
// Receive 必须响应 context 取消；Send 必须拒绝不属于该 resource kind 的 frame type；Close 必须幂等并解除 framing registry。
type ResourceStream interface {
	Receive(context.Context) (uint8, []byte, error)
	Send(context.Context, uint8, []byte) error
	Close() error
}

type protoApplicationEventSource interface {
	ApplicationEvents(context.Context) (<-chan *apipb.EventEnvelope, error)
}

// ApplicationSession 把 generated Proto command 绑定到一个不可变 ReadyPeerSession generation。
// request ID 与 operation ID 由该对象单调分配；调用方不得自行重建 session stamp 或跨 generation 复用资源。
type ApplicationSession struct {
	stamp    EndpointSessionStamp
	executor ProtoApplicationExecutor
	nextID   atomic.Uint64
}

// NewApplicationSession 建立 connection-bound Proto API session。
// stamp 不完整或 executor 缺失时立即失败，禁止创建可在运行期 fallback 的半初始化对象。
func NewApplicationSession(stamp EndpointSessionStamp, executor ProtoApplicationExecutor) (*ApplicationSession, error) {
	if err := stamp.Validate(); err != nil {
		return nil, err
	}
	if executor == nil {
		return nil, runtimeError(ErrorUnavailable, "application executor is required", nil)
	}
	return &ApplicationSession{stamp: stamp, executor: executor}, nil
}

// Stamp 返回该 application session 的不可变 generation fence。
func (session *ApplicationSession) Stamp() EndpointSessionStamp {
	if session == nil {
		return EndpointSessionStamp{}
	}
	return session.stamp
}

// ProtoStamp 返回当前 application session 的 generated Proto stamp 快照。
// 调用方可以把它保存为 attachment/view projection，但不得修改后再作为新的 generation truth。
func (session *ApplicationSession) ProtoStamp() *apipb.EndpointSessionStamp {
	if session == nil {
		return nil
	}
	return session.protoStamp()
}

// ValidateCurrent 在进入 protocol/resource adapter 前验证 executor 仍绑定当前 generation。
// 不支持动态 generation 的单连接 executor 只校验本地 stamp；runtime-owned executor 必须实现 ApplicationSessionValidator。
func (session *ApplicationSession) ValidateCurrent() error {
	if session == nil || session.executor == nil {
		return runtimeError(ErrorUnavailable, "application session is unavailable", nil)
	}
	if validator, ok := session.executor.(ApplicationSessionValidator); ok {
		return validator.ValidateApplicationSession(session.stamp)
	}
	return session.stamp.Validate()
}

// Execute 克隆 command、写入当前 session context 和每次调用唯一的 operation stamp，再交给 protocol binding。
// transport 失败与 ResultEnvelope typed error 都转换为 runtime error；失败不会自动重放 command。
func (session *ApplicationSession) Execute(ctx context.Context, command *apipb.CommandEnvelope) (*apipb.ResultEnvelope, error) {
	return session.execute(ctx, command, false)
}

// ExecuteTerminal 为必须取得迟到资源结果的调用保留有界 terminal response，同时复用本 session 的 request/operation stamp 与结果校验。
// executor 不支持该能力时立即失败，禁止绕过 ApplicationSession 自行复制 generation 或 correlation 逻辑。
func (session *ApplicationSession) ExecuteTerminal(ctx context.Context, command *apipb.CommandEnvelope) (*apipb.ResultEnvelope, error) {
	return session.execute(ctx, command, true)
}

func (session *ApplicationSession) execute(ctx context.Context, command *apipb.CommandEnvelope, terminal bool) (*apipb.ResultEnvelope, error) {
	if session == nil || session.executor == nil {
		return nil, runtimeError(ErrorUnavailable, "application session is unavailable", nil)
	}
	if command == nil {
		return nil, runtimeError(ErrorInvalidRequest, "application command is required", nil)
	}
	sequence := session.nextID.Add(1)
	requestID := fmt.Sprintf("%s-%d", session.stamp.EndpointID, sequence)
	stamp := session.protoStamp()
	snapshot := proto.Clone(command).(*apipb.CommandEnvelope)
	snapshot.Context = &apipb.RequestContext{RequestId: requestID, ApiVersion: &apipb.ApiVersion{Major: 1}, Session: stamp}
	bindOperationStamp(snapshot, stamp, requestID)
	var result *apipb.ResultEnvelope
	var err error
	if terminal {
		executor, ok := session.executor.(TerminalResponseApplicationExecutor)
		if !ok {
			return nil, runtimeError(ErrorUnavailable, "application executor does not support terminal responses", nil)
		}
		result, err = executor.ExecuteApplicationTerminal(ctx, snapshot)
	} else {
		result, err = session.executor.ExecuteApplication(ctx, snapshot)
	}
	if err != nil {
		return nil, &Error{Code: CodeOf(err), Message: err.Error(), Cause: err, Attempted: WasAttempted(err)}
	}
	if result == nil {
		return nil, &Error{Code: ErrorUnavailable, Message: "application executor returned no result", Attempted: true}
	}
	if result.GetRequestId() != requestID {
		return nil, &Error{Code: ErrorUnavailable, Message: "application result request correlation mismatch", Attempted: true}
	}
	if !applicationSessionStampsEqual(result.GetOriginSession(), stamp) {
		return nil, &Error{Code: ErrorStaleSession, Message: "application result belongs to a different endpoint session", Attempted: true}
	}
	if apiError := result.GetError(); apiError != nil {
		return nil, runtimeErrorFromProto(apiError)
	}
	return result, nil
}

// TerminalDefaults 查询当前 owning endpoint 的 daemon 默认 shell 与 cwd。
func (session *ApplicationSession) TerminalDefaults(ctx context.Context, command *apipb.TerminalDefaultsCommand) (*apipb.TerminalDefaultsResult, error) {
	result, err := session.Execute(ctx, &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_TerminalDefaults{TerminalDefaults: command}})
	if err != nil {
		return nil, err
	}
	if result.GetTerminalDefaults() == nil {
		return nil, missingApplicationResult("terminal_defaults")
	}
	return result.GetTerminalDefaults(), nil
}

// TerminalCreate 在当前 owning endpoint 创建 terminal，lifecycle truth 仍由 daemon core 持有。
func (session *ApplicationSession) TerminalCreate(ctx context.Context, command *apipb.TerminalCreateCommand) (*apipb.TerminalCreateResult, error) {
	result, err := session.Execute(ctx, &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_TerminalCreate{TerminalCreate: command}})
	if err != nil {
		return nil, err
	}
	if result.GetTerminalCreate() == nil {
		return nil, missingApplicationResult("terminal_create")
	}
	return result.GetTerminalCreate(), nil
}

// TerminalList 读取当前 owning endpoint 的 terminal inventory，不合并其它 endpoint 缓存。
func (session *ApplicationSession) TerminalList(ctx context.Context, command *apipb.TerminalListCommand) (*apipb.TerminalListResult, error) {
	result, err := session.Execute(ctx, &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_TerminalList{TerminalList: command}})
	if err != nil {
		return nil, err
	}
	if result.GetTerminalList() == nil {
		return nil, missingApplicationResult("terminal_list")
	}
	return result.GetTerminalList(), nil
}

// TerminalGet 读取单个 daemon-owned terminal 的 lifecycle 与 metadata 投影。
func (session *ApplicationSession) TerminalGet(ctx context.Context, command *apipb.TerminalGetCommand) (*apipb.TerminalGetResult, error) {
	result, err := session.Execute(ctx, &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_TerminalGet{TerminalGet: command}})
	if err != nil {
		return nil, err
	}
	if result.GetTerminalGet() == nil {
		return nil, missingApplicationResult("terminal_get")
	}
	return result.GetTerminalGet(), nil
}

// TerminalRestart 请求 daemon 按其保存的 process specification 重启 terminal。
func (session *ApplicationSession) TerminalRestart(ctx context.Context, command *apipb.TerminalRestartCommand) error {
	return session.executeAcknowledge(ctx, &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_TerminalRestart{TerminalRestart: command}})
}

// TerminalKill 请求 daemon 终止 terminal process，但不删除 record/history truth。
func (session *ApplicationSession) TerminalKill(ctx context.Context, command *apipb.TerminalKillCommand) error {
	return session.executeAcknowledge(ctx, &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_TerminalKill{TerminalKill: command}})
}

// TerminalRemove 请求 daemon 删除符合 lifecycle 条件的 terminal record。
func (session *ApplicationSession) TerminalRemove(ctx context.Context, command *apipb.TerminalRemoveCommand) error {
	return session.executeAcknowledge(ctx, &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_TerminalRemove{TerminalRemove: command}})
}

// TerminalSetMetadata 原子更新 daemon-owned terminal name/tags metadata。
func (session *ApplicationSession) TerminalSetMetadata(ctx context.Context, command *apipb.TerminalSetMetadataCommand) error {
	return session.executeAcknowledge(ctx, &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_TerminalSetMetadata{TerminalSetMetadata: command}})
}

// TerminalSetTags 原子替换 daemon-owned terminal tags。
func (session *ApplicationSession) TerminalSetTags(ctx context.Context, command *apipb.TerminalSetTagsCommand) error {
	return session.executeAcknowledge(ctx, &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_TerminalSetTags{TerminalSetTags: command}})
}

// TerminalAttach 建立 session-bound attachment resource；返回 handle 必须用于后续 input/resize/detach。
func (session *ApplicationSession) TerminalAttach(ctx context.Context, command *apipb.TerminalAttachCommand) (*apipb.TerminalAttachResult, error) {
	result, err := session.Execute(ctx, &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_TerminalAttach{TerminalAttach: command}})
	if err != nil {
		return nil, err
	}
	if result.GetTerminalAttach() == nil {
		return nil, missingApplicationResult("terminal_attach")
	}
	return result.GetTerminalAttach(), nil
}

// TerminalDetach 释放一个 session-owned attachment handle，不触发隐式 reconnect。
func (session *ApplicationSession) TerminalDetach(ctx context.Context, command *apipb.TerminalDetachCommand) error {
	return session.executeAcknowledge(ctx, &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_TerminalDetach{TerminalDetach: command}})
}

// TerminalInput 向 attachment owning terminal 写入 bytes；失败后 runtime 不自动重放 payload。
func (session *ApplicationSession) TerminalInput(ctx context.Context, command *apipb.TerminalInputCommand) error {
	return session.executeAcknowledge(ctx, &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_TerminalInput{TerminalInput: command}})
}

// TerminalResize 请求 attachment owner 协调 terminal size，并返回 daemon 确认后的控制状态。
func (session *ApplicationSession) TerminalResize(ctx context.Context, command *apipb.TerminalResizeCommand) (*apipb.TerminalResizeResult, error) {
	result, err := session.Execute(ctx, &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_TerminalResize{TerminalResize: command}})
	if err != nil {
		return nil, err
	}
	if result.GetTerminalResize() == nil {
		return nil, missingApplicationResult("terminal_resize")
	}
	return result.GetTerminalResize(), nil
}

// TerminalResizeLock 修改 attachment owner 的显式 size lock，并返回最终控制状态。
func (session *ApplicationSession) TerminalResizeLock(ctx context.Context, command *apipb.TerminalResizeLockCommand) (*apipb.TerminalResizeResult, error) {
	result, err := session.Execute(ctx, &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_TerminalResizeLock{TerminalResizeLock: command}})
	if err != nil {
		return nil, err
	}
	if result.GetTerminalResize() == nil {
		return nil, missingApplicationResult("terminal_resize")
	}
	return result.GetTerminalResize(), nil
}

// PathListDirectories 查询当前 endpoint daemon 文件系统中的目录候选。
func (session *ApplicationSession) PathListDirectories(ctx context.Context, command *apipb.PathListDirectoriesCommand) (*apipb.PathListDirectoriesResult, error) {
	result, err := session.Execute(ctx, &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_PathListDirectories{PathListDirectories: command}})
	if err != nil {
		return nil, err
	}
	if result.GetPathListDirectories() == nil {
		return nil, missingApplicationResult("path_list_directories")
	}
	return result.GetPathListDirectories(), nil
}

// HistoryWindow 查询 core authoritative history window；token/cursor 必须由调用方原样回传。
func (session *ApplicationSession) HistoryWindow(ctx context.Context, command *apipb.HistoryWindowCommand) (*apipb.HistoryWindowResult, error) {
	result, err := session.Execute(ctx, &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_HistoryWindow{HistoryWindow: command}})
	if err != nil {
		return nil, err
	}
	if result.GetHistoryWindow() == nil {
		return nil, missingApplicationResult("history_window")
	}
	return result.GetHistoryWindow(), nil
}

// HistoryCopy 从 frozen history token 复制 authoritative text。
func (session *ApplicationSession) HistoryCopy(ctx context.Context, command *apipb.HistoryCopyCommand) (*apipb.HistoryCopyResult, error) {
	result, err := session.Execute(ctx, &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_HistoryCopy{HistoryCopy: command}})
	if err != nil {
		return nil, err
	}
	if result.GetHistoryCopy() == nil {
		return nil, missingApplicationResult("history_copy")
	}
	return result.GetHistoryCopy(), nil
}

// HistoryRelease 释放 current-session 使用的 frozen history token。
func (session *ApplicationSession) HistoryRelease(ctx context.Context, command *apipb.HistoryReleaseCommand) error {
	return session.executeAcknowledge(ctx, &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_HistoryRelease{HistoryRelease: command}})
}

// HistoryBacklogStatus 返回 history ingest 的诊断状态。
func (session *ApplicationSession) HistoryBacklogStatus(ctx context.Context, command *apipb.HistoryBacklogStatusCommand) (*apipb.HistoryBacklogStatusResult, error) {
	result, err := session.Execute(ctx, &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_HistoryBacklogStatus{HistoryBacklogStatus: command}})
	if err != nil {
		return nil, err
	}
	if result.GetHistoryBacklogStatus() == nil {
		return nil, missingApplicationResult("history_backlog_status")
	}
	return result.GetHistoryBacklogStatus(), nil
}

// LiveScreen 读取 latest-only native screen projection。
func (session *ApplicationSession) LiveScreen(ctx context.Context, command *apipb.LiveScreenGetCommand) (*apipb.NativeScreenResult, error) {
	result, err := session.Execute(ctx, &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_LiveScreenGet{LiveScreenGet: command}})
	if err != nil {
		return nil, err
	}
	if result.GetLiveScreen() == nil {
		return nil, missingApplicationResult("live_screen")
	}
	return result.GetLiveScreen(), nil
}

// LiveInvalidation 等待 observed revision 之后的一次 daemon wake edge。
func (session *ApplicationSession) LiveInvalidation(ctx context.Context, command *apipb.LiveInvalidationNextCommand) (*apipb.LiveInvalidationResult, error) {
	result, err := session.Execute(ctx, &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_LiveInvalidationNext{LiveInvalidationNext: command}})
	if err != nil {
		return nil, err
	}
	if result.GetLiveInvalidation() == nil {
		return nil, missingApplicationResult("live_invalidation")
	}
	return result.GetLiveInvalidation(), nil
}

// EventSubscribe 建立 daemon session-owned subscription，并返回对应的 Proto event stream。
func (session *ApplicationSession) EventSubscribe(ctx context.Context, command *apipb.EventSubscribeCommand) (*apipb.EventSubscriptionResult, <-chan *apipb.EventEnvelope, error) {
	source, ok := session.executor.(protoApplicationEventSource)
	if !ok {
		return nil, nil, runtimeError(ErrorUnavailable, "application event source is unavailable", nil)
	}
	eventCtx, cancel := context.WithCancel(ctx)
	events, err := source.ApplicationEvents(eventCtx)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	result, err := session.Execute(ctx, &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_EventSubscribe{EventSubscribe: command}})
	if err != nil {
		cancel()
		return nil, nil, err
	}
	if result.GetEventSubscription() == nil {
		cancel()
		return nil, nil, missingApplicationResult("event_subscription")
	}
	subscription := result.GetEventSubscription().GetSubscription()
	filtered := make(chan *apipb.EventEnvelope, 64)
	go func() {
		defer cancel()
		defer close(filtered)
		for {
			select {
			case <-eventCtx.Done():
				return
			case event, ok := <-events:
				if !ok {
					return
				}
				if !sameApplicationResource(event.GetSubscription(), subscription) {
					continue
				}
				select {
				case filtered <- event:
				case <-eventCtx.Done():
					return
				}
			}
		}
	}()
	return result.GetEventSubscription(), filtered, nil
}

func sameApplicationResource(left, right *apipb.ResourceHandle) bool {
	return left != nil && right != nil && left.GetKind() == right.GetKind() && left.GetGeneration() == right.GetGeneration() && applicationSessionStampsEqual(left.GetSession(), right.GetSession()) && bytes.Equal(left.GetOpaqueToken(), right.GetOpaqueToken())
}

// FileList 读取 owning daemon 的目录窗口。
func (session *ApplicationSession) FileList(ctx context.Context, command *apipb.FileListCommand) (*apipb.FileListResult, error) {
	result, err := session.Execute(ctx, &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_FileList{FileList: command}})
	if err != nil {
		return nil, err
	}
	if result.GetFileList() == nil {
		return nil, missingApplicationResult("file_list")
	}
	return result.GetFileList(), nil
}

// FileStat 读取 owning daemon path metadata。
func (session *ApplicationSession) FileStat(ctx context.Context, command *apipb.FileStatCommand) (*apipb.FileStatResult, error) {
	result, err := session.Execute(ctx, &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_FileStat{FileStat: command}})
	if err != nil {
		return nil, err
	}
	if result.GetFileStat() == nil {
		return nil, missingApplicationResult("file_stat")
	}
	return result.GetFileStat(), nil
}

// FilePreview 读取有界文件前缀。
func (session *ApplicationSession) FilePreview(ctx context.Context, command *apipb.FilePreviewCommand) (*apipb.FilePreviewResult, error) {
	result, err := session.Execute(ctx, &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_FilePreview{FilePreview: command}})
	if err != nil {
		return nil, err
	}
	if result.GetFilePreview() == nil {
		return nil, missingApplicationResult("file_preview")
	}
	return result.GetFilePreview(), nil
}

// FileMkdir 创建 daemon-owned directory。
func (session *ApplicationSession) FileMkdir(ctx context.Context, command *apipb.FileMkdirCommand) (*apipb.FileOperationResult, error) {
	return session.fileOperation(ctx, &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_FileMkdir{FileMkdir: command}})
}

// FileRename 原子重命名 daemon-owned path。
func (session *ApplicationSession) FileRename(ctx context.Context, command *apipb.FileRenameCommand) (*apipb.FileOperationResult, error) {
	return session.fileOperation(ctx, &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_FileRename{FileRename: command}})
}

// FileDelete 删除 daemon-owned path。
func (session *ApplicationSession) FileDelete(ctx context.Context, command *apipb.FileDeleteCommand) (*apipb.FileOperationResult, error) {
	return session.fileOperation(ctx, &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_FileDelete{FileDelete: command}})
}

// FileCopy 批量复制 daemon-owned paths。
func (session *ApplicationSession) FileCopy(ctx context.Context, command *apipb.FileCopyCommand) (*apipb.FileBatchResult, error) {
	return session.fileBatch(ctx, &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_FileCopy{FileCopy: command}})
}

// FileMove 批量移动 daemon-owned paths。
func (session *ApplicationSession) FileMove(ctx context.Context, command *apipb.FileMoveCommand) (*apipb.FileBatchResult, error) {
	return session.fileBatch(ctx, &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_FileMove{FileMove: command}})
}

// FileDownloadOpen 创建 session-bound download resource。
func (session *ApplicationSession) FileDownloadOpen(ctx context.Context, command *apipb.FileDownloadOpenCommand) (*apipb.FileTransferOpenResult, error) {
	return session.fileTransferOpen(ctx, &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_FileDownloadOpen{FileDownloadOpen: command}})
}

// FileUploadOpen 创建或恢复 session-bound upload resource。
func (session *ApplicationSession) FileUploadOpen(ctx context.Context, command *apipb.FileUploadOpenCommand) (*apipb.FileTransferOpenResult, error) {
	return session.fileTransferOpen(ctx, &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_FileUploadOpen{FileUploadOpen: command}})
}

// FileTransferCancel 取消 current-session file transfer。
func (session *ApplicationSession) FileTransferCancel(ctx context.Context, command *apipb.FileTransferCancelCommand) (*apipb.FileTransferCancelResult, error) {
	result, err := session.Execute(ctx, &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_FileTransferCancel{FileTransferCancel: command}})
	if err != nil {
		return nil, err
	}
	if result.GetFileTransferCancel() == nil {
		return nil, missingApplicationResult("file_transfer_cancel")
	}
	return result.GetFileTransferCancel(), nil
}

// StorageGet 读取 opaque storage entry。
func (session *ApplicationSession) StorageGet(ctx context.Context, command *apipb.StorageGetCommand) (*apipb.StorageGetResult, error) {
	result, err := session.Execute(ctx, &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_StorageGet{StorageGet: command}})
	if err != nil {
		return nil, err
	}
	if result.GetStorageGet() == nil {
		return nil, missingApplicationResult("storage_get")
	}
	return result.GetStorageGet(), nil
}

// StoragePut 执行 opaque value CAS put。
func (session *ApplicationSession) StoragePut(ctx context.Context, command *apipb.StoragePutCommand) (*apipb.StoragePutResult, error) {
	result, err := session.Execute(ctx, &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_StoragePut{StoragePut: command}})
	if err != nil {
		return nil, err
	}
	if result.GetStoragePut() == nil {
		return nil, missingApplicationResult("storage_put")
	}
	return result.GetStoragePut(), nil
}

// StorageDelete 执行 opaque value CAS delete。
func (session *ApplicationSession) StorageDelete(ctx context.Context, command *apipb.StorageDeleteCommand) (*apipb.StorageDeleteResult, error) {
	result, err := session.Execute(ctx, &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_StorageDelete{StorageDelete: command}})
	if err != nil {
		return nil, err
	}
	if result.GetStorageDelete() == nil {
		return nil, missingApplicationResult("storage_delete")
	}
	return result.GetStorageDelete(), nil
}

// StorageList 列出 opaque storage entries。
func (session *ApplicationSession) StorageList(ctx context.Context, command *apipb.StorageListCommand) (*apipb.StorageListResult, error) {
	result, err := session.Execute(ctx, &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_StorageList{StorageList: command}})
	if err != nil {
		return nil, err
	}
	if result.GetStorageList() == nil {
		return nil, missingApplicationResult("storage_list")
	}
	return result.GetStorageList(), nil
}

// ClientAccessIdentity 读取 daemon DeviceIdentity 的公开投影。
func (session *ApplicationSession) ClientAccessIdentity(ctx context.Context, command *apipb.ClientAccessIdentityCommand) (*apipb.ClientAccessIdentityResult, error) {
	result, err := session.Execute(ctx, &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_ClientAccessIdentity{ClientAccessIdentity: command}})
	if err != nil {
		return nil, err
	}
	if result.GetClientAccessIdentity() == nil {
		return nil, missingApplicationResult("client_access_identity")
	}
	return result.GetClientAccessIdentity(), nil
}

// ClientAccessList 读取 daemon 持久化 grant 的脱敏投影。
func (session *ApplicationSession) ClientAccessList(ctx context.Context, command *apipb.ClientAccessListCommand) (*apipb.ClientAccessListResult, error) {
	result, err := session.Execute(ctx, &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_ClientAccessList{ClientAccessList: command}})
	if err != nil {
		return nil, err
	}
	if result.GetClientAccessList() == nil {
		return nil, missingApplicationResult("client_access_list")
	}
	return result.GetClientAccessList(), nil
}

// ClientAccessTicketCreate 原子签发并登记一次性 pairing ticket。
func (session *ApplicationSession) ClientAccessTicketCreate(ctx context.Context, command *apipb.ClientAccessTicketCreateCommand) (*apipb.ClientAccessTicketCreateResult, error) {
	result, err := session.Execute(ctx, &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_ClientAccessTicketCreate{ClientAccessTicketCreate: command}})
	if err != nil {
		return nil, err
	}
	if result.GetClientAccessTicketCreate() == nil {
		return nil, missingApplicationResult("client_access_ticket_create")
	}
	return result.GetClientAccessTicketCreate(), nil
}

// ClientAccessRevoke 持久化撤销指定 grant。
func (session *ApplicationSession) ClientAccessRevoke(ctx context.Context, command *apipb.ClientAccessRevokeCommand) (*apipb.ClientAccessRevokeResult, error) {
	result, err := session.Execute(ctx, &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_ClientAccessRevoke{ClientAccessRevoke: command}})
	if err != nil {
		return nil, err
	}
	if result.GetClientAccessRevoke() == nil {
		return nil, missingApplicationResult("client_access_revoke")
	}
	return result.GetClientAccessRevoke(), nil
}

// RemoteStatus 读取 daemon remote runtime 状态。
func (session *ApplicationSession) RemoteStatus(ctx context.Context, command *apipb.RemoteStatusCommand) (*apipb.RemoteStatusResult, error) {
	result, err := session.Execute(ctx, &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_RemoteStatus{RemoteStatus: command}})
	if err != nil {
		return nil, err
	}
	if result.GetRemoteStatus() == nil {
		return nil, missingApplicationResult("remote_status")
	}
	return result.GetRemoteStatus(), nil
}

// RemotePairStart 启动 daemon local pairing session。
func (session *ApplicationSession) RemotePairStart(ctx context.Context, command *apipb.RemotePairStartCommand) (*apipb.RemotePairStartResult, error) {
	result, err := session.Execute(ctx, &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_RemotePairStart{RemotePairStart: command}})
	if err != nil {
		return nil, err
	}
	if result.GetRemotePairStart() == nil {
		return nil, missingApplicationResult("remote_pair_start")
	}
	return result.GetRemotePairStart(), nil
}

// RemoteLocalEnable 启用 daemon local remote runtime。
func (session *ApplicationSession) RemoteLocalEnable(ctx context.Context, command *apipb.RemoteLocalEnableCommand) (*apipb.RemoteLocalStatusResult, error) {
	return session.remoteLocalStatus(ctx, &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_RemoteLocalEnable{RemoteLocalEnable: command}})
}

// RemoteLocalStatus 读取 daemon local remote runtime 状态。
func (session *ApplicationSession) RemoteLocalStatus(ctx context.Context, command *apipb.RemoteLocalStatusCommand) (*apipb.RemoteLocalStatusResult, error) {
	return session.remoteLocalStatus(ctx, &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_RemoteLocalStatus{RemoteLocalStatus: command}})
}

// RemoteLocalDisable 关闭 daemon local remote runtime。
func (session *ApplicationSession) RemoteLocalDisable(ctx context.Context, command *apipb.RemoteLocalDisableCommand) (*apipb.RemoteLocalStatusResult, error) {
	return session.remoteLocalStatus(ctx, &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_RemoteLocalDisable{RemoteLocalDisable: command}})
}

func (session *ApplicationSession) remoteLocalStatus(ctx context.Context, command *apipb.CommandEnvelope) (*apipb.RemoteLocalStatusResult, error) {
	result, err := session.Execute(ctx, command)
	if err != nil {
		return nil, err
	}
	if result.GetRemoteLocalStatus() == nil {
		return nil, missingApplicationResult("remote_local_status")
	}
	return result.GetRemoteLocalStatus(), nil
}

func (session *ApplicationSession) fileOperation(ctx context.Context, command *apipb.CommandEnvelope) (*apipb.FileOperationResult, error) {
	result, err := session.Execute(ctx, command)
	if err != nil {
		return nil, err
	}
	if result.GetFileOperation() == nil {
		return nil, missingApplicationResult("file_operation")
	}
	return result.GetFileOperation(), nil
}
func (session *ApplicationSession) fileBatch(ctx context.Context, command *apipb.CommandEnvelope) (*apipb.FileBatchResult, error) {
	result, err := session.Execute(ctx, command)
	if err != nil {
		return nil, err
	}
	if result.GetFileBatch() == nil {
		return nil, missingApplicationResult("file_batch")
	}
	return result.GetFileBatch(), nil
}
func (session *ApplicationSession) fileTransferOpen(ctx context.Context, command *apipb.CommandEnvelope) (*apipb.FileTransferOpenResult, error) {
	result, err := session.Execute(ctx, command)
	if err != nil {
		return nil, err
	}
	if result.GetFileTransferOpen() == nil {
		return nil, missingApplicationResult("file_transfer_open")
	}
	return result.GetFileTransferOpen(), nil
}

func (session *ApplicationSession) executeAcknowledge(ctx context.Context, command *apipb.CommandEnvelope) error {
	result, err := session.Execute(ctx, command)
	if err != nil {
		return err
	}
	if result.GetAcknowledge() == nil {
		return missingApplicationResult("acknowledge")
	}
	return nil
}

func missingApplicationResult(kind string) error {
	return &Error{Code: ErrorUnavailable, Message: fmt.Sprintf("application result %s is missing", kind), Attempted: true}
}

func (session *ApplicationSession) protoStamp() *apipb.EndpointSessionStamp {
	return &apipb.EndpointSessionStamp{
		EndpointId: string(session.stamp.EndpointID),
		RouteId:    string(session.stamp.RouteID),
		Generation: uint64(session.stamp.Generation),
	}
}

func applicationSessionStampsEqual(left, right *apipb.EndpointSessionStamp) bool {
	return left != nil && right != nil &&
		left.GetEndpointId() == right.GetEndpointId() &&
		left.GetRouteId() == right.GetRouteId() &&
		left.GetGeneration() == right.GetGeneration()
}

func bindOperationStamp(command *apipb.CommandEnvelope, stamp *apipb.EndpointSessionStamp, requestID string) {
	operationID := commandOperationID(command)
	if operationID == "" {
		operationID = requestID
	}
	operation := &apipb.OperationStamp{Session: proto.Clone(stamp).(*apipb.EndpointSessionStamp), OperationId: operationID}
	switch value := command.GetCommand().(type) {
	case *apipb.CommandEnvelope_TerminalAttach:
		value.TerminalAttach.Operation = operation
	case *apipb.CommandEnvelope_TerminalDetach:
		value.TerminalDetach.Operation = operation
	case *apipb.CommandEnvelope_TerminalInput:
		value.TerminalInput.Operation = operation
	case *apipb.CommandEnvelope_TerminalResize:
		value.TerminalResize.Operation = operation
	case *apipb.CommandEnvelope_TerminalResizeLock:
		value.TerminalResizeLock.Operation = operation
	case *apipb.CommandEnvelope_FileDownloadOpen:
		value.FileDownloadOpen.Operation = operation
	case *apipb.CommandEnvelope_FileUploadOpen:
		value.FileUploadOpen.Operation = operation
	case *apipb.CommandEnvelope_FileTransferCancel:
		value.FileTransferCancel.Operation = operation
	}
}

func commandOperationID(command *apipb.CommandEnvelope) string {
	if command == nil {
		return ""
	}
	switch value := command.GetCommand().(type) {
	case *apipb.CommandEnvelope_TerminalAttach:
		return value.TerminalAttach.GetOperation().GetOperationId()
	case *apipb.CommandEnvelope_TerminalDetach:
		return value.TerminalDetach.GetOperation().GetOperationId()
	case *apipb.CommandEnvelope_TerminalInput:
		return value.TerminalInput.GetOperation().GetOperationId()
	case *apipb.CommandEnvelope_TerminalResize:
		return value.TerminalResize.GetOperation().GetOperationId()
	case *apipb.CommandEnvelope_TerminalResizeLock:
		return value.TerminalResizeLock.GetOperation().GetOperationId()
	case *apipb.CommandEnvelope_FileDownloadOpen:
		return value.FileDownloadOpen.GetOperation().GetOperationId()
	case *apipb.CommandEnvelope_FileUploadOpen:
		return value.FileUploadOpen.GetOperation().GetOperationId()
	case *apipb.CommandEnvelope_FileTransferCancel:
		return value.FileTransferCancel.GetOperation().GetOperationId()
	default:
		return ""
	}
}

func runtimeErrorFromProto(apiError *apipb.ApiError) error {
	code := ErrorUnavailable
	switch apiError.GetCode() {
	case apipb.ApiErrorCode_API_ERROR_CODE_INVALID_REQUEST,
		apipb.ApiErrorCode_API_ERROR_CODE_UNSUPPORTED_VERSION,
		apipb.ApiErrorCode_API_ERROR_CODE_UNSUPPORTED_CAPABILITY,
		apipb.ApiErrorCode_API_ERROR_CODE_CONFLICT:
		code = ErrorInvalidRequest
	case apipb.ApiErrorCode_API_ERROR_CODE_UNAUTHORIZED,
		apipb.ApiErrorCode_API_ERROR_CODE_FORBIDDEN:
		code = ErrorAuthorization
	case apipb.ApiErrorCode_API_ERROR_CODE_NOT_FOUND:
		code = ErrorNotFound
	case apipb.ApiErrorCode_API_ERROR_CODE_STALE_SESSION:
		code = ErrorStaleSession
	case apipb.ApiErrorCode_API_ERROR_CODE_CANCELLED:
		code = ErrorCanceled
	case apipb.ApiErrorCode_API_ERROR_CODE_ENTITLEMENT_DENIED:
		code = ErrorEntitlement
	}
	return &Error{Code: code, Message: apiError.GetMessage(), Attempted: apiError.GetAttempted()}
}
