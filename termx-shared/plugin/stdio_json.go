package plugin

import (
	"encoding/json"
	"fmt"
	"time"
)

// StdioJSONProtocol 是 one-shot external runner 使用的稳定 stdio JSON 协议名。
// host 写入 stdin 的 request 和 runner 写入 stdout 的 response 都必须携带该值，避免把普通脚本输出误判为插件响应。
const StdioJSONProtocol = "termx.plugin.stdio.v1"

// StdioJSONInvocationKind 描述 one-shot runner 本次被激活的入口类型。
// 它只区分 host 已裁决的 action 或 after-event hook，不允许 runner 自行声明系统 mutation 来源。
type StdioJSONInvocationKind string

const (
	// StdioJSONInvocationAction 表示 runner 正在处理一个 action 入口。
	StdioJSONInvocationAction StdioJSONInvocationKind = "action"
	// StdioJSONInvocationHook 表示 runner 正在处理一个 after-event hook 入口。
	StdioJSONInvocationHook StdioJSONInvocationKind = "hook"
)

// StdioJSONContext 是 host 注入 one-shot runner 的运行上下文。
// 这些字段来自已认证的 daemon/client/workspace 边界；外部进程只能读取，不能通过 response 回写覆盖。
type StdioJSONContext struct {
	ClientKind       ClientKind   `json:"client_kind,omitempty"`
	ClientSessionID  string       `json:"client_session_id,omitempty"`
	WorkspaceID      string       `json:"workspace_id,omitempty"`
	EndpointID       EndpointID   `json:"endpoint_id,omitempty"`
	TerminalRef      *TerminalRef `json:"terminal_ref,omitempty"`
	DaemonID         string       `json:"daemon_id,omitempty"`
	DaemonTerminalID TerminalID   `json:"daemon_terminal_id,omitempty"`
	GrantRef         string       `json:"grant_ref,omitempty"`
}

// Clone 返回 stdio JSON 上下文的深拷贝。
// TerminalRef 是跨 endpoint side effect 的边界字段，复制后才能安全交给 runner adapter 修改环境变量。
func (context StdioJSONContext) Clone() StdioJSONContext {
	out := context
	if context.TerminalRef != nil {
		ref := *context.TerminalRef
		out.TerminalRef = &ref
	}
	return out
}

// StdioJSONActionInvocation 是 host 投递给 one-shot runner 的 action 入口。
// SourcePluginID、RequiredCaps 和完整 MessageTrace 不在这里出现，必须由 host 从 manifest、grant 和 trace manager 推导。
type StdioJSONActionInvocation struct {
	ActionID       ActionID        `json:"action_id"`
	Params         json.RawMessage `json:"params,omitempty"`
	Target         ActionTarget    `json:"target,omitempty"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
}

// Clone 返回 action invocation 的深拷贝。
// Params 是 runner 可读取的 JSON payload，复制后避免调用方复用缓冲区污染协议 envelope。
func (action StdioJSONActionInvocation) Clone() StdioJSONActionInvocation {
	out := action
	out.Params = cloneJSONRaw(action.Params)
	if action.Target.TerminalRef != nil {
		ref := *action.Target.TerminalRef
		out.Target.TerminalRef = &ref
	}
	return out
}

// StdioJSONRequest 是 host 通过 stdin 发送给 one-shot external runner 的唯一请求。
// 该请求是 host-derived envelope：插件身份、host placement、deadline 和 trace parent 都由 host 填充，不从外部进程读取。
type StdioJSONRequest struct {
	Protocol       string                     `json:"protocol"`
	RequestID      string                     `json:"request_id"`
	PluginID       PluginID                   `json:"plugin_id"`
	Host           HostPlacement              `json:"host"`
	Handler        string                     `json:"handler"`
	Kind           StdioJSONInvocationKind    `json:"kind"`
	Context        StdioJSONContext           `json:"context,omitempty"`
	Action         *StdioJSONActionInvocation `json:"action,omitempty"`
	Hook           *HookEvent                 `json:"hook,omitempty"`
	TraceParent    TraceParent                `json:"trace_parent,omitempty"`
	DeadlineUnixNS int64                      `json:"deadline_unix_ns"`
}

// Clone 返回 stdio JSON request 的深拷贝。
// host 在进入 os/exec runner 前调用它，可避免 hook payload 或 params 被异步修改。
func (request StdioJSONRequest) Clone() StdioJSONRequest {
	out := request
	out.Context = request.Context.Clone()
	if request.Action != nil {
		action := request.Action.Clone()
		out.Action = &action
	}
	if request.Hook != nil {
		hook := request.Hook.Clone()
		out.Hook = &hook
	}
	return out
}

// NormalizeStdioJSONRequest 填充 stdio JSON request 的协议默认值并返回深拷贝。
// 它不补 deadline、identity 或 trace；这些字段必须由调用方的 host 边界明确提供。
func NormalizeStdioJSONRequest(request StdioJSONRequest) StdioJSONRequest {
	out := request.Clone()
	if out.Protocol == "" {
		out.Protocol = StdioJSONProtocol
	}
	return out
}

// ValidateStdioJSONRequest 校验 host 即将发送给 one-shot runner 的 envelope。
// 失败表示 host 构造的调用不完整，不能启动外部进程，以免产生无 deadline 或无身份的脚本执行。
func ValidateStdioJSONRequest(request StdioJSONRequest) error {
	if request.Protocol != StdioJSONProtocol {
		return fmt.Errorf("stdio_json request protocol is invalid")
	}
	if request.RequestID == "" {
		return fmt.Errorf("stdio_json request id is required")
	}
	if err := validatePluginID(request.PluginID); err != nil {
		return err
	}
	if !validHostPlacement(request.Host) {
		return fmt.Errorf("stdio_json request host is invalid")
	}
	if request.Handler == "" {
		return fmt.Errorf("stdio_json request handler is required")
	}
	if request.TraceParent.TraceID == "" || request.TraceParent.Token == "" {
		return fmt.Errorf("stdio_json request trace parent is required")
	}
	if request.DeadlineUnixNS <= 0 {
		return fmt.Errorf("stdio_json request deadline is required")
	}
	if time.Unix(0, request.DeadlineUnixNS).IsZero() {
		return fmt.Errorf("stdio_json request deadline is invalid")
	}
	switch request.Kind {
	case StdioJSONInvocationAction:
		if request.Action == nil {
			return fmt.Errorf("stdio_json action request payload is required")
		}
		if request.Action.ActionID == "" {
			return fmt.Errorf("stdio_json action id is required")
		}
		if !validJSONRaw(request.Action.Params) {
			return fmt.Errorf("stdio_json action params must be valid json")
		}
	case StdioJSONInvocationHook:
		if request.Hook == nil {
			return fmt.Errorf("stdio_json hook event is required")
		}
		if request.Hook.EventID == "" || request.Hook.Type == "" {
			return fmt.Errorf("stdio_json hook event identity is required")
		}
		if !validEventSourceHost(request.Hook.SourceHost) {
			return fmt.Errorf("stdio_json hook source host is invalid")
		}
		if request.Hook.Trace.TraceID == "" {
			return fmt.Errorf("stdio_json hook trace is required")
		}
		if request.TraceParent.TraceID != request.Hook.Trace.TraceID {
			return fmt.Errorf("stdio_json hook trace parent mismatch")
		}
		if !validJSONRaw(request.Hook.Payload) {
			return fmt.Errorf("stdio_json hook payload must be valid json")
		}
	default:
		return fmt.Errorf("stdio_json invocation kind is invalid")
	}
	return nil
}

// StdioJSONStatus 描述 one-shot runner 的执行结果。
// 它只表达插件处理结果，不代表 action call 已经被 daemon 或 client 执行成功。
type StdioJSONStatus string

const (
	// StdioJSONStatusOK 表示 runner 完成并返回可选 result/action calls。
	StdioJSONStatusOK StdioJSONStatus = "ok"
	// StdioJSONStatusDenied 表示 runner 自身按插件逻辑拒绝处理。
	StdioJSONStatusDenied StdioJSONStatus = "denied"
	// StdioJSONStatusUnsupported 表示 runner 不支持该 handler 或输入。
	StdioJSONStatusUnsupported StdioJSONStatus = "unsupported"
	// StdioJSONStatusFailed 表示 runner 执行失败。
	StdioJSONStatusFailed StdioJSONStatus = "failed"
)

// StdioJSONActionCall 是 runner 请求 host 后续执行的 action intent。
// SourcePluginID、capability 和 trace 仍由 host 根据当前 runner session 派生，runner 不能在这里自报。
type StdioJSONActionCall struct {
	RequestID      string          `json:"request_id,omitempty"`
	ActionID       ActionID        `json:"action_id"`
	Params         json.RawMessage `json:"params,omitempty"`
	Target         ActionTarget    `json:"target,omitempty"`
	DeadlineUnixNS int64           `json:"deadline_unix_ns"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
}

// Clone 返回 action call 的深拷贝。
// host 将这些 intent 转成 client.control.call 或 daemon action 前，必须保持 payload 不共享底层缓冲区。
func (call StdioJSONActionCall) Clone() StdioJSONActionCall {
	out := call
	out.Params = cloneJSONRaw(call.Params)
	if call.Target.TerminalRef != nil {
		ref := *call.Target.TerminalRef
		out.Target.TerminalRef = &ref
	}
	return out
}

// StdioJSONResponse 是 one-shot runner 从 stdout 返回给 host 的唯一响应。
// 响应不包含 SourcePluginID、ResolvedCaps 或 MessageTrace，避免外部进程伪造身份、权限和因果链。
type StdioJSONResponse struct {
	Protocol    string                `json:"protocol"`
	RequestID   string                `json:"request_id"`
	Status      StdioJSONStatus       `json:"status"`
	Result      json.RawMessage       `json:"result,omitempty"`
	Error       string                `json:"error,omitempty"`
	ActionCalls []StdioJSONActionCall `json:"action_calls,omitempty"`
}

// Clone 返回 stdio JSON response 的深拷贝。
// runner adapter 解码后先复制再交给上层 dispatcher，避免测试或日志复用修改 action call payload。
func (response StdioJSONResponse) Clone() StdioJSONResponse {
	out := response
	out.Result = cloneJSONRaw(response.Result)
	if len(response.ActionCalls) > 0 {
		out.ActionCalls = make([]StdioJSONActionCall, 0, len(response.ActionCalls))
		for _, call := range response.ActionCalls {
			out.ActionCalls = append(out.ActionCalls, call.Clone())
		}
	}
	return out
}

// ValidateStdioJSONResponse 校验 one-shot runner stdout 中的响应。
// expectedRequestID 来自 host request；不匹配时必须拒绝，避免旧进程或错误脚本响应串入当前调用。
// maxActionDeadlineUnixNS 是可选的 host-side 上限；真实 runner 输出校验必须传入 invocation deadline，防止 runner 放大后续 action 有效窗口。
func ValidateStdioJSONResponse(response StdioJSONResponse, expectedRequestID string, maxActionDeadlineUnixNS ...int64) error {
	if response.Protocol != StdioJSONProtocol {
		return fmt.Errorf("stdio_json response protocol is invalid")
	}
	if response.RequestID == "" || response.RequestID != expectedRequestID {
		return fmt.Errorf("stdio_json response request id mismatch")
	}
	if !validStdioJSONStatus(response.Status) {
		return fmt.Errorf("stdio_json response status is invalid")
	}
	if (response.Status == StdioJSONStatusDenied || response.Status == StdioJSONStatusUnsupported || response.Status == StdioJSONStatusFailed) && response.Error == "" {
		return fmt.Errorf("stdio_json response error is required")
	}
	if response.Status != StdioJSONStatusOK && len(response.ActionCalls) > 0 {
		return fmt.Errorf("stdio_json response action calls require ok status")
	}
	if !validJSONRaw(response.Result) {
		return fmt.Errorf("stdio_json response result must be valid json")
	}
	maxDeadline := int64(0)
	if len(maxActionDeadlineUnixNS) > 0 {
		maxDeadline = maxActionDeadlineUnixNS[0]
	}
	for _, call := range response.ActionCalls {
		if call.ActionID == "" {
			return fmt.Errorf("stdio_json action call id is required")
		}
		if call.DeadlineUnixNS <= 0 {
			return fmt.Errorf("stdio_json action call deadline is required")
		}
		if maxDeadline > 0 && call.DeadlineUnixNS > maxDeadline {
			return fmt.Errorf("stdio_json action call deadline exceeds invocation deadline")
		}
		if !validJSONRaw(call.Params) {
			return fmt.Errorf("stdio_json action call params must be valid json")
		}
	}
	return nil
}

func validStdioJSONStatus(status StdioJSONStatus) bool {
	switch status {
	case StdioJSONStatusOK, StdioJSONStatusDenied, StdioJSONStatusUnsupported, StdioJSONStatusFailed:
		return true
	default:
		return false
	}
}

func validJSONRaw(raw json.RawMessage) bool {
	return len(raw) == 0 || json.Valid(raw)
}

func cloneJSONRaw(raw json.RawMessage) json.RawMessage {
	if raw == nil {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}
