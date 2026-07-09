package protocol

import (
	"fmt"
	"strings"

	"github.com/lozzow/termx/termx-shared/plugin"
)

// ClientControlCallValidationPolicy 描述 broker 投递 client action 前的 host-side 校验上下文。
// trace 必须由 host 从 runner session、grant 和 TraceManager 派生；本策略只接收已知事实并做一致性检查，
// 不允许调用方在请求体里自报 capability 或绕过 destructive broadcast 保护。
type ClientControlCallValidationPolicy struct {
	TraceValidator            func(plugin.TraceParent) error
	ActionSpec                *ClientControlActionSpec
	AllowDestructiveBroadcast bool
}

// ClientControlResponseValidationContext 描述 broker 接受 client action response 时的原始 delivery 上下文。
// RequestID、SessionID 和 TraceParent 都来自 broker 保存的投递记录，用来防止目标 client 回写其他请求或替换 trace。
type ClientControlResponseValidationContext struct {
	RequestID      string
	SessionID      string
	TraceParent    plugin.TraceParent
	TraceValidator func(plugin.TraceParent) error
}

// ValidateClientSessionRegister 校验 client session register 的最小协议前置条件。
// 它只检查 broker 路由所需的身份和 action catalog 基本完整性；不会读取或解释 client UI state。
func ValidateClientSessionRegister(params ClientSessionRegisterParams) error {
	if params.SessionID == "" {
		return fmt.Errorf("client session id is required")
	}
	if params.ClientKind == "" {
		return fmt.Errorf("client kind is required")
	}
	sessionCaps := capabilitySetForValidation(params.Capabilities)
	for index, action := range params.Actions {
		if action.ID == "" {
			return fmt.Errorf("client action id at index %d is required", index)
		}
		if action.OwnerPluginID == "" {
			return fmt.Errorf("client action %s owner plugin id is required", action.ID)
		}
		if strings.HasPrefix(string(action.ID), "termx.") && !strings.HasPrefix(string(action.OwnerPluginID), "termx.builtin.") {
			return fmt.Errorf("client action %s uses termx namespace without builtin owner", action.ID)
		}
		if !validationCapsSubset(sessionCaps, action.RequiredCaps) || !validationCapsSubset(sessionCaps, action.ClientRequiredCaps) {
			return fmt.Errorf("client action %s requires capabilities not registered by session", action.ID)
		}
		if action.Danger == plugin.DangerDestructive && len(action.RequiredCaps) == 0 && len(action.ClientRequiredCaps) == 0 && len(action.DaemonRequiredCaps) == 0 {
			return fmt.Errorf("client action %s destructive action requires explicit capability", action.ID)
		}
	}
	return nil
}

// ValidateClientControlCall 校验 daemon broker 接受 client action 请求前的基础路由条件。
// SessionID 与 Broadcast 互斥；ActivePanel 这类 selector 只允许透传给目标 client，本函数不会解析它。
func ValidateClientControlCall(params ClientControlCallParams) error {
	if params.RequestID == "" {
		return fmt.Errorf("client control request id is required")
	}
	if params.ActionID == "" {
		return fmt.Errorf("client control action id is required")
	}
	if params.TraceParent.TraceID == "" || params.TraceParent.Token == "" {
		return fmt.Errorf("client control trace parent is required")
	}
	if params.Target.SessionID == "" && !params.Target.Broadcast {
		return fmt.Errorf("client control target must set session id or broadcast")
	}
	if params.Target.SessionID != "" && params.Target.Broadcast {
		return fmt.Errorf("client control target cannot set both session id and broadcast")
	}
	if params.Target.Broadcast && params.Target.ClientKind == "" && params.Target.WorkspaceID == "" {
		return fmt.Errorf("client control broadcast requires explicit client kind or workspace scope")
	}
	if params.Target.TerminalRef != nil {
		if params.Target.TerminalRef.EndpointID == "" || params.Target.TerminalRef.TerminalID == "" {
			return fmt.Errorf("client control terminal ref requires endpoint id and terminal id")
		}
	}
	return nil
}

// ValidateClientControlCallWithPolicy 在基础路由校验之外检查 host 派生的 trace 和 action 风险策略。
// broker 在真实实现中应把 TraceValidator 接到 TraceManager；这里不执行 UI state，也不替代 daemon side effect 授权。
func ValidateClientControlCallWithPolicy(params ClientControlCallParams, policy ClientControlCallValidationPolicy) error {
	if err := ValidateClientControlCall(params); err != nil {
		return err
	}
	if policy.TraceValidator != nil {
		if err := policy.TraceValidator(params.TraceParent); err != nil {
			return fmt.Errorf("client control trace parent rejected: %w", err)
		}
	}
	if policy.ActionSpec != nil {
		if policy.ActionSpec.ID != params.ActionID {
			return fmt.Errorf("client control action spec %s does not match call action %s", policy.ActionSpec.ID, params.ActionID)
		}
		if params.Target.Broadcast && !policy.ActionSpec.BroadcastAllowed {
			return fmt.Errorf("client control broadcast requires action policy")
		}
		if policy.ActionSpec.Danger == plugin.DangerDestructive && params.Target.Broadcast && !policy.AllowDestructiveBroadcast {
			return fmt.Errorf("client control destructive broadcast requires explicit allow policy")
		}
	}
	return nil
}

// DeriveClientControlInvocation 用 host-derived source 构造投递到 client mailbox 的内部 envelope。
// 外部 runner 的 client.control.call 请求没有 Source；broker 必须在验证 runner session 和 grant 后调用这里补 source。
func DeriveClientControlInvocation(params ClientControlCallParams, source ClientControlSource) (ClientControlInvocation, error) {
	if err := ValidateClientControlCall(params); err != nil {
		return ClientControlInvocation{}, err
	}
	if err := validateClientControlSource(source); err != nil {
		return ClientControlInvocation{}, err
	}
	return ClientControlInvocation{
		RequestID:      params.RequestID,
		ActionID:       params.ActionID,
		Params:         append([]byte(nil), params.Params...),
		Source:         source,
		Target:         cloneClientControlTarget(params.Target),
		TraceParent:    params.TraceParent,
		Deadline:       params.Deadline,
		IdempotencyKey: params.IdempotencyKey,
	}, nil
}

// ValidateClientControlResponse 校验目标 client session 回写 action response 的基础条件。
// 它只验证 request/session/status 与错误体的关系，不判断上游插件是否仍在等待该 response。
func ValidateClientControlResponse(params ClientControlResponseParams) error {
	if params.RequestID == "" {
		return fmt.Errorf("client control response request id is required")
	}
	if params.SessionID == "" {
		return fmt.Errorf("client control response session id is required")
	}
	if params.TraceParent.TraceID == "" || params.TraceParent.Token == "" {
		return fmt.Errorf("client control response trace parent is required")
	}
	switch params.Status {
	case ClientControlStatusOK:
		if params.Error != nil {
			return fmt.Errorf("client control ok response cannot carry error")
		}
	case ClientControlStatusError, ClientControlStatusRejected, ClientControlStatusTimeout:
		if params.Error == nil {
			return fmt.Errorf("client control %s response requires error", params.Status)
		}
	default:
		return fmt.Errorf("client control response status %q is not allowed", params.Status)
	}
	return nil
}

// ValidateClientControlResponseFor 在基础 response 校验之外绑定原始 delivery 上下文。
// broker 必须用保存的 request/session/trace 检查回包，避免其他 client 或新 trace 注入后续 hook 因果链。
func ValidateClientControlResponseFor(params ClientControlResponseParams, context ClientControlResponseValidationContext) error {
	if err := ValidateClientControlResponse(params); err != nil {
		return err
	}
	if context.RequestID != "" && params.RequestID != context.RequestID {
		return fmt.Errorf("client control response request id does not match delivery")
	}
	if context.SessionID != "" && params.SessionID != context.SessionID {
		return fmt.Errorf("client control response session id does not match delivery")
	}
	if context.TraceParent.TraceID != "" && (params.TraceParent.TraceID != context.TraceParent.TraceID || params.TraceParent.Token != context.TraceParent.Token) {
		return fmt.Errorf("client control response trace parent does not match delivery")
	}
	if context.TraceValidator != nil {
		if err := context.TraceValidator(params.TraceParent); err != nil {
			return fmt.Errorf("client control response trace parent rejected: %w", err)
		}
	}
	return nil
}

func capabilitySetForValidation(caps []plugin.Capability) map[plugin.Capability]struct{} {
	out := make(map[plugin.Capability]struct{}, len(caps))
	for _, cap := range caps {
		out[cap] = struct{}{}
	}
	return out
}

func validationCapsSubset(available map[plugin.Capability]struct{}, required []plugin.Capability) bool {
	for _, cap := range required {
		if _, ok := available[cap]; !ok {
			return false
		}
	}
	return true
}

func validateClientControlSource(source ClientControlSource) error {
	if source.PluginID == "" {
		return fmt.Errorf("client control source plugin id is required")
	}
	if source.Kind == "" {
		return fmt.Errorf("client control source kind is required")
	}
	return nil
}

func cloneClientControlTarget(target ClientControlTarget) ClientControlTarget {
	out := target
	if target.TerminalRef != nil {
		ref := *target.TerminalRef
		out.TerminalRef = &ref
	}
	return out
}
