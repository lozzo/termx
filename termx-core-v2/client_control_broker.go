package termxcorev2

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/termx-shared/plugin"
)

const (
	defaultClientControlInboxSize = 64
	clientControlSourcePlugin     = "termx.builtin.protocol"
	clientControlSourceProtocol   = "protocol"
)

// clientControlBroker 是 daemon 内的 client session/mailbox broker。
// domain owner 是 daemon presence 和 mailbox 路由；它不拥有 TUI focus/panel/tab truth，也不执行 terminal lifecycle side effect。
type clientControlBroker struct {
	mu        sync.Mutex
	inboxSize int
	now       func() time.Time
	sessions  map[string]*clientControlSession
}

type clientControlSession struct {
	info           protocol.ClientSessionInfo
	owner          uint64
	inbox          chan protocol.ClientControlInvocation
	watcherChannel uint16
	pending        map[clientControlPendingKey]clientControlPending
}

type clientControlPendingKey struct {
	RequestID string
	SessionID string
}

type clientControlPending struct {
	TraceParent    plugin.TraceParent
	Deadline       time.Time
	IdempotencyKey string
}

func newClientControlBroker(inboxSize int) *clientControlBroker {
	if inboxSize <= 0 {
		inboxSize = defaultClientControlInboxSize
	}
	return &clientControlBroker{
		inboxSize: inboxSize,
		now:       time.Now,
		sessions:  make(map[string]*clientControlSession),
	}
}

// register 记录某个 protocol session 拥有的 client session presence 和 action catalog。
// broker 只保存路由投影；active panel、tab、float 仍由目标 client 本地解释。
func (broker *clientControlBroker) register(owner uint64, params protocol.ClientSessionRegisterParams) (protocol.ClientSessionRegisterResult, error) {
	if err := protocol.ValidateClientSessionRegister(params); err != nil {
		return protocol.ClientSessionRegisterResult{}, err
	}
	broker.mu.Lock()
	defer broker.mu.Unlock()
	now := broker.now()
	session := broker.sessions[params.SessionID]
	if session != nil && session.owner != owner {
		// 中文说明：SessionID 重新绑定到另一条 protocol session 时，旧 inbox
		// 必须关闭，防止断线重连后的旧 watcher 继续抢读新 session 的控制消息。
		close(session.inbox)
		session = nil
	}
	if session == nil {
		session = &clientControlSession{
			inbox:   make(chan protocol.ClientControlInvocation, broker.inboxSize),
			pending: make(map[clientControlPendingKey]clientControlPending),
		}
		broker.sessions[params.SessionID] = session
	}
	connectedAt := session.info.ConnectedAt
	if connectedAt.IsZero() {
		connectedAt = now
	}
	session.owner = owner
	session.info = protocol.ClientSessionInfo{
		SessionID:    params.SessionID,
		ClientKind:   params.ClientKind,
		WorkspaceID:  params.WorkspaceID,
		InstanceID:   params.InstanceID,
		PID:          params.PID,
		Capabilities: cloneClientControlCapabilities(params.Capabilities),
		Actions:      cloneClientControlActions(params.Actions),
		ConnectedAt:  connectedAt,
		LastSeenAt:   now,
		Metadata:     cloneStringMap(params.Metadata),
	}
	return protocol.ClientSessionRegisterResult{Session: cloneClientSessionInfo(session.info, true)}, nil
}

func (broker *clientControlBroker) unregister(owner uint64, sessionID string) {
	broker.mu.Lock()
	defer broker.mu.Unlock()
	session := broker.sessions[sessionID]
	if session == nil || session.owner != owner {
		return
	}
	delete(broker.sessions, sessionID)
	close(session.inbox)
}

func (broker *clientControlBroker) list(params protocol.ClientSessionListParams) protocol.ClientSessionListResult {
	broker.mu.Lock()
	defer broker.mu.Unlock()
	out := protocol.ClientSessionListResult{}
	for _, session := range broker.sessions {
		info := session.info
		if params.ClientKind != "" && info.ClientKind != params.ClientKind {
			continue
		}
		if params.WorkspaceID != "" && info.WorkspaceID != params.WorkspaceID {
			continue
		}
		out.Sessions = append(out.Sessions, cloneClientSessionInfo(info, params.IncludeActions))
	}
	return out
}

func (broker *clientControlBroker) watch(owner uint64, channel uint16, params protocol.ClientControlWatchParams) (<-chan protocol.ClientControlInvocation, error) {
	if params.SessionID == "" {
		return nil, fmt.Errorf("client control watch session id is required")
	}
	if channel == 0 {
		return nil, fmt.Errorf("client control watch channel is required")
	}
	broker.mu.Lock()
	defer broker.mu.Unlock()
	session := broker.sessions[params.SessionID]
	if session == nil {
		return nil, fmt.Errorf("client session %q is not registered", params.SessionID)
	}
	if session.owner != owner {
		return nil, fmt.Errorf("client session %q is owned by another protocol session", params.SessionID)
	}
	if session.watcherChannel != 0 {
		return nil, fmt.Errorf("client session %q already has an active control watcher", params.SessionID)
	}
	session.watcherChannel = channel
	return session.inbox, nil
}

func (broker *clientControlBroker) unwatch(owner uint64, sessionID string, channel uint16) bool {
	broker.mu.Lock()
	defer broker.mu.Unlock()
	session := broker.sessions[sessionID]
	if session == nil || session.owner != owner || session.watcherChannel != channel {
		return false
	}
	session.watcherChannel = 0
	return true
}

func (broker *clientControlBroker) call(ctx context.Context, source protocol.ClientControlSource, params protocol.ClientControlCallParams) (protocol.ClientControlCallResult, error) {
	if err := protocol.ValidateClientControlCall(params); err != nil {
		return protocol.ClientControlCallResult{}, err
	}
	broker.mu.Lock()
	defer broker.mu.Unlock()
	targets := broker.resolveTargets(params.Target)
	result := protocol.ClientControlCallResult{RequestID: params.RequestID, Broadcast: params.Target.Broadcast}
	if len(targets) == 0 {
		if params.Target.SessionID != "" {
			result.Deliveries = append(result.Deliveries, protocol.ClientControlDelivery{
				SessionID: params.Target.SessionID,
				Status:    protocol.ClientControlStatusNotFound,
				Error:     &protocol.ClientControlError{Code: "session_not_found", Message: "client session is not registered"},
			})
		}
		return result, nil
	}
	for _, session := range targets {
		delivery := broker.deliver(ctx, session, source, params)
		result.Deliveries = append(result.Deliveries, delivery)
	}
	return result, nil
}

func (broker *clientControlBroker) respond(owner uint64, params protocol.ClientControlResponseParams) (protocol.ClientControlResponseResult, error) {
	broker.mu.Lock()
	defer broker.mu.Unlock()
	session := broker.sessions[params.SessionID]
	if session == nil {
		return protocol.ClientControlResponseResult{}, fmt.Errorf("client session %q is not registered", params.SessionID)
	}
	if session.owner != owner {
		return protocol.ClientControlResponseResult{}, fmt.Errorf("client session %q is owned by another protocol session", params.SessionID)
	}
	key := clientControlPendingKey{RequestID: params.RequestID, SessionID: params.SessionID}
	pending, ok := session.pending[key]
	if !ok {
		return protocol.ClientControlResponseResult{}, fmt.Errorf("client control response has no pending delivery")
	}
	if !pending.Deadline.IsZero() && !broker.now().Before(pending.Deadline) {
		delete(session.pending, key)
		return protocol.ClientControlResponseResult{}, fmt.Errorf("client control response deadline expired")
	}
	if err := protocol.ValidateClientControlResponseFor(params, protocol.ClientControlResponseValidationContext{
		RequestID:   params.RequestID,
		SessionID:   params.SessionID,
		TraceParent: pending.TraceParent,
	}); err != nil {
		return protocol.ClientControlResponseResult{}, err
	}
	delete(session.pending, key)
	return protocol.ClientControlResponseResult{RequestID: params.RequestID, Accepted: true}, nil
}

func (broker *clientControlBroker) resolveTargets(target protocol.ClientControlTarget) []*clientControlSession {
	if !target.Broadcast {
		if target.SessionID == "" {
			return nil
		}
		session := broker.sessions[target.SessionID]
		if session == nil {
			return nil
		}
		return []*clientControlSession{session}
	}
	var out []*clientControlSession
	for _, session := range broker.sessions {
		info := session.info
		if target.ClientKind != "" && info.ClientKind != target.ClientKind {
			continue
		}
		if target.WorkspaceID != "" && info.WorkspaceID != target.WorkspaceID {
			continue
		}
		out = append(out, session)
	}
	return out
}

func (broker *clientControlBroker) deliver(ctx context.Context, session *clientControlSession, source protocol.ClientControlSource, params protocol.ClientControlCallParams) protocol.ClientControlDelivery {
	spec, ok := clientControlActionForSession(session.info.Actions, params.ActionID)
	if !ok {
		return protocol.ClientControlDelivery{
			SessionID: session.info.SessionID,
			Status:    protocol.ClientControlStatusRejected,
			Error:     &protocol.ClientControlError{Code: "unsupported_action", Message: "client session does not advertise action"},
		}
	}
	if params.Target.Broadcast && spec.Danger == plugin.DangerDestructive {
		return protocol.ClientControlDelivery{
			SessionID: session.info.SessionID,
			Status:    protocol.ClientControlStatusRejected,
			Error:     &protocol.ClientControlError{Code: "destructive_broadcast_denied", Message: "destructive client action cannot be broadcast"},
		}
	}
	if err := protocol.ValidateClientControlCallWithPolicy(params, protocol.ClientControlCallValidationPolicy{ActionSpec: &spec}); err != nil {
		return protocol.ClientControlDelivery{
			SessionID: session.info.SessionID,
			Status:    protocol.ClientControlStatusRejected,
			Error:     &protocol.ClientControlError{Code: "policy_rejected", Message: err.Error()},
		}
	}
	if !params.Deadline.IsZero() && !broker.now().Before(params.Deadline) {
		return protocol.ClientControlDelivery{
			SessionID: session.info.SessionID,
			Status:    protocol.ClientControlStatusTimeout,
			Error:     &protocol.ClientControlError{Code: "deadline_expired", Message: "client control request deadline has expired", Retryable: false},
		}
	}
	pendingKey := clientControlPendingKey{RequestID: params.RequestID, SessionID: session.info.SessionID}
	if _, exists := session.pending[pendingKey]; exists {
		return protocol.ClientControlDelivery{
			SessionID: session.info.SessionID,
			Status:    protocol.ClientControlStatusRejected,
			Error:     &protocol.ClientControlError{Code: "duplicate_request", Message: "client control request is already pending"},
		}
	}
	invocation, err := protocol.DeriveClientControlInvocation(params, source)
	if err != nil {
		return protocol.ClientControlDelivery{
			SessionID: session.info.SessionID,
			Status:    protocol.ClientControlStatusRejected,
			Error:     &protocol.ClientControlError{Code: "invalid_invocation", Message: err.Error()},
		}
	}
	invocation.Target.SessionID = session.info.SessionID
	select {
	case <-ctx.Done():
		return protocol.ClientControlDelivery{
			SessionID: session.info.SessionID,
			Status:    protocol.ClientControlStatusTimeout,
			Error:     &protocol.ClientControlError{Code: "request_cancelled", Message: ctx.Err().Error(), Retryable: true},
		}
	case session.inbox <- invocation:
		session.pending[pendingKey] = clientControlPending{
			TraceParent:    invocation.TraceParent,
			Deadline:       invocation.Deadline,
			IdempotencyKey: invocation.IdempotencyKey,
		}
		return protocol.ClientControlDelivery{SessionID: session.info.SessionID, Status: protocol.ClientControlStatusQueued}
	default:
		return protocol.ClientControlDelivery{
			SessionID: session.info.SessionID,
			Status:    protocol.ClientControlStatusError,
			Error:     &protocol.ClientControlError{Code: "mailbox_full", Message: "client control mailbox is full", Retryable: true},
		}
	}
}

func clientControlActionForSession(actions []protocol.ClientControlActionSpec, id plugin.ActionID) (protocol.ClientControlActionSpec, bool) {
	for _, action := range actions {
		if action.ID == id {
			return action, true
		}
	}
	return protocol.ClientControlActionSpec{}, false
}

func cloneClientSessionInfo(info protocol.ClientSessionInfo, includeActions bool) protocol.ClientSessionInfo {
	out := info
	out.Capabilities = cloneClientControlCapabilities(info.Capabilities)
	out.Metadata = cloneStringMap(info.Metadata)
	if includeActions {
		out.Actions = cloneClientControlActions(info.Actions)
	} else {
		out.Actions = nil
	}
	return out
}

func cloneClientControlActions(actions []protocol.ClientControlActionSpec) []protocol.ClientControlActionSpec {
	if len(actions) == 0 {
		return nil
	}
	out := make([]protocol.ClientControlActionSpec, len(actions))
	copy(out, actions)
	for i := range out {
		out[i].SupportedClientKinds = append([]plugin.ClientKind(nil), actions[i].SupportedClientKinds...)
		out[i].RequiredCaps = cloneClientControlCapabilities(actions[i].RequiredCaps)
		out[i].ClientRequiredCaps = cloneClientControlCapabilities(actions[i].ClientRequiredCaps)
		out[i].DaemonRequiredCaps = cloneClientControlCapabilities(actions[i].DaemonRequiredCaps)
	}
	return out
}

func cloneClientControlCapabilities(caps []plugin.Capability) []plugin.Capability {
	return append([]plugin.Capability(nil), caps...)
}
