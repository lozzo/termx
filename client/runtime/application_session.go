package runtime

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/lozzow/termx/proto/apipb"
	"google.golang.org/protobuf/proto"
)

// ProtoApplicationExecutor 是 client runtime 到 protocol/platform binding 的唯一公共 API 执行边界。
// 实现只运输完整 CommandEnvelope/ResultEnvelope，不解释 terminal 字段，也不选择 route 或 generation。
type ProtoApplicationExecutor interface {
	ExecuteApplication(context.Context, *apipb.CommandEnvelope) (*apipb.ResultEnvelope, error)
}

// ApplicationSession 把 generated Proto command 绑定到一个不可变 ReadySession generation。
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

// Execute 克隆 command、写入当前 session context 和每次调用唯一的 operation stamp，再交给 protocol binding。
// transport 失败与 ResultEnvelope typed error 都转换为 runtime error；失败不会自动重放 command。
func (session *ApplicationSession) Execute(ctx context.Context, command *apipb.CommandEnvelope) (*apipb.ResultEnvelope, error) {
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
	result, err := session.executor.ExecuteApplication(ctx, snapshot)
	if err != nil {
		return nil, &Error{Code: CodeOf(err), Message: err.Error(), Cause: err, Attempted: true}
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
	operation := &apipb.OperationStamp{Session: proto.Clone(stamp).(*apipb.EndpointSessionStamp), OperationId: requestID}
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
	case apipb.ApiErrorCode_API_ERROR_CODE_STALE_SESSION:
		code = ErrorStaleSession
	case apipb.ApiErrorCode_API_ERROR_CODE_CANCELLED:
		code = ErrorCanceled
	}
	return &Error{Code: code, Message: apiError.GetMessage(), Attempted: apiError.GetAttempted()}
}
