package hub

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/lozzow/termx/private/cloud/control-plane/servicecredential"
)

// Candidate 是 Hub 允许转发的最小 trickle ICE metadata。
// SignalingSessionID 在 private Hub event envelope 中单独绑定；candidate 不含 grant 或 application payload。
type Candidate struct {
	Candidate        string
	SDPMid           string
	SDPMLineIndex    uint32
	UsernameFragment string
}

// RoutePreference 是 Hub 透明转发的 managed service intent。
// Hub 只验证它属于冻结枚举；是否签发 RelayLease 仍由 Control Plane 决定，terminal capability 不进入该字段。
type RoutePreference int32

const (
	// RoutePreferenceDirectOnly 表示本次 signaling 不允许付费 Relay。
	RoutePreferenceDirectOnly RoutePreference = 1
	// RoutePreferenceStandardRelay 表示账号允许单 Relay，但是否强制 Relay 由独立 RelayOnly 决定。
	RoutePreferenceStandardRelay RoutePreference = 2
	// RoutePreferenceSmartRoute 表示客户端已经取得短期 SmartRoute 计划。
	RoutePreferenceSmartRoute RoutePreference = 3
	// RoutePreferenceGlobalAccelerator 是延后的受控全球加速 intent；CLOUD004 不执行该路径。
	RoutePreferenceGlobalAccelerator RoutePreference = 4
)

// Offer 是 client admission 验证后投递给固定 target presence 的 WebRTC offer。
type Offer struct {
	SignalingSessionID string
	ManagedSessionID   string
	SourceDeviceID     string
	TargetDeviceID     string
	SDP                string
	Candidates         []Candidate
	RoutePreference    RoutePreference
	RelayOnly          bool
}

// Answer 是 daemon answer admission 验证后投递给 owning client 的 WebRTC answer。
type Answer struct {
	SignalingSessionID string
	SDP                string
	Candidates         []Candidate
}

// CandidateEvent 把 trickle candidate 显式绑定到 signaling session。
// 这避免 daemon presence 中并发 offer 使用“最近一个 offer”的隐式路由。
type CandidateEvent struct {
	SignalingSessionID string
	Candidate          Candidate
}

// Closed 表示单个 presence 或 signaling session 结束的稳定 metadata。
type Closed struct {
	Reason string
}

// Failure 是 daemon 对单个 signaling session 返回的稳定失败分类。
// Hub 只路由数值错误码与 retryable，不接收原始错误文本、credential、terminal 或 capability 数据。
type Failure struct {
	Code      int32
	Retryable bool
}

// PresenceEvent 是 daemon presence stream 的有界下行事件。
// v1 private contract 始终为 candidate 携带 SignalingSessionID；public DTO 未升级前 companion 对缺失绑定 fail closed。
type PresenceEvent struct {
	Offer     *Offer
	Candidate *CandidateEvent
	Closed    *Closed
}

// ClientEvent 是单个 client signaling session 的下行 answer/candidate/closed 事件。
type ClientEvent struct {
	Answer    *Answer
	Candidate *CandidateEvent
	Failure   *Failure
	Closed    *Closed
}

type presenceState struct {
	deviceID  string
	accountID string
	sessionID string
	expiresAt time.Time
	events    chan PresenceEvent
	done      chan struct{}
	closed    bool
}

// Presence 是一个 daemon device 的短期 Hub presence owner。
// Receive/Close 只操作当前 registration，不包含 heartbeat、inventory 或 forced kick API。
type Presence struct {
	service *Service
	state   *presenceState
	once    sync.Once
}

// Receive 阻塞读取下一个 offer/candidate/closed event，并响应 caller context cancel。
func (presence *Presence) Receive(ctx context.Context) (PresenceEvent, error) {
	if presence == nil || presence.state == nil || ctx == nil {
		return PresenceEvent{}, io.EOF
	}
	select {
	case <-presence.state.done:
		return PresenceEvent{}, io.EOF
	default:
	}
	select {
	case event := <-presence.state.events:
		return clonePresenceEvent(event), nil
	case <-presence.state.done:
		return PresenceEvent{}, io.EOF
	case <-ctx.Done():
		return PresenceEvent{}, ctx.Err()
	}
}

// Close 幂等注销当前 device presence，并关闭所有路由到该 target 的 signaling session。
func (presence *Presence) Close() error {
	if presence == nil || presence.service == nil || presence.state == nil {
		return nil
	}
	presence.once.Do(func() {
		presence.service.mu.Lock()
		if current := presence.service.presences[presence.state.deviceID]; current == presence.state {
			delete(presence.service.presences, presence.state.deviceID)
		}
		presence.service.closePresenceLocked(presence.state)
		presence.service.mu.Unlock()
	})
	return nil
}

type sessionState struct {
	id                 string
	accountID          string
	managedSessionID   string
	clientDeviceID     string
	clientConnectionID string
	targetDeviceID     string
	allowedOperations  []servicecredential.HubOperation
	expiresAt          time.Time
	clientEvents       chan ClientEvent
	done               chan struct{}
	answered           bool
	daemonOperations   []servicecredential.HubOperation
	closed             bool
}

// DaemonSession 是 answer admission 验证后绑定到一个 signaling session 的 daemon handle。
// 它只能向该 session 的 owning client 发送 candidate，不提供其他 device 或 session lookup。
type DaemonSession struct {
	service  *Service
	state    *sessionState
	deviceID string
}

// SendCandidate 把 daemon trickle candidate 投递给 owning client stream。
// answer admission 未包含 candidate operation、session 已关闭或 client queue 满时 fail closed。
func (daemon *DaemonSession) SendCandidate(candidate Candidate) error {
	if daemon == nil || daemon.service == nil || daemon.state == nil || !validCandidate(candidate) {
		return ErrInvalidSignal
	}
	service := daemon.service
	service.mu.Lock()
	defer service.mu.Unlock()
	now := service.clock.Now().UTC()
	state := service.sessions[daemon.state.id]
	if state != daemon.state || state.closed || state.targetDeviceID != daemon.deviceID || !now.Before(state.expiresAt) {
		return ErrSessionNotFound
	}
	if !state.answered || !containsOperation(state.daemonOperations, servicecredential.HubOperationCandidate) {
		return ErrAdmission
	}
	event := ClientEvent{Candidate: &CandidateEvent{SignalingSessionID: state.id, Candidate: candidate}}
	select {
	case state.clientEvents <- event:
		return nil
	default:
		return ErrBackpressure
	}
}

// ClientSession 是一个 admission-bound client signaling session owner。
// SendCandidate 复用首次验签后的 candidate permission，不重复提交或记录 ticket body。
type ClientSession struct {
	service *Service
	state   *sessionState
	once    sync.Once
}

// Receive 阻塞读取 answer、candidate 或 closed event，并响应 caller context cancel。
func (client *ClientSession) Receive(ctx context.Context) (ClientEvent, error) {
	if client == nil || client.state == nil || ctx == nil {
		return ClientEvent{}, io.EOF
	}
	select {
	case event := <-client.state.clientEvents:
		return cloneClientEvent(event), nil
	case <-client.state.done:
		select {
		case event := <-client.state.clientEvents:
			return cloneClientEvent(event), nil
		default:
			return ClientEvent{}, io.EOF
		}
	case <-ctx.Done():
		return ClientEvent{}, ctx.Err()
	}
}

// SendCandidate 把 client trickle candidate 路由到 target presence。
// session admission 未包含 candidate operation、session 已过期或 queue 满时 fail closed。
func (client *ClientSession) SendCandidate(candidate Candidate) error {
	if client == nil || client.service == nil || client.state == nil || !validCandidate(candidate) {
		return ErrInvalidSignal
	}
	service := client.service
	service.mu.Lock()
	defer service.mu.Unlock()
	now := service.clock.Now().UTC()
	state := service.sessions[client.state.id]
	if state != client.state || state.closed || !now.Before(state.expiresAt) {
		return ErrSessionNotFound
	}
	if !containsOperation(state.allowedOperations, servicecredential.HubOperationCandidate) {
		return ErrAdmission
	}
	presence := service.presences[state.targetDeviceID]
	if presence == nil || presence.closed || !now.Before(presence.expiresAt) {
		return ErrPresenceNotFound
	}
	event := PresenceEvent{Candidate: &CandidateEvent{SignalingSessionID: state.id, Candidate: candidate}}
	select {
	case presence.events <- event:
		return nil
	default:
		return ErrBackpressure
	}
}

// Close 幂等取消当前 client signaling session，不影响同 target 的其他 sessions。
func (client *ClientSession) Close() error {
	if client == nil || client.service == nil || client.state == nil {
		return nil
	}
	client.once.Do(func() {
		client.service.mu.Lock()
		if current := client.service.sessions[client.state.id]; current == client.state {
			delete(client.service.sessions, client.state.id)
		}
		client.service.closeSessionLocked(client.state, "client canceled signaling")
		client.service.mu.Unlock()
	})
	return nil
}

func validCandidates(candidates []Candidate) bool {
	for _, candidate := range candidates {
		if !validCandidate(candidate) {
			return false
		}
	}
	return true
}

func validCandidate(candidate Candidate) bool { return candidate.Candidate != "" }

func containsOperation(operations []servicecredential.HubOperation, target servicecredential.HubOperation) bool {
	for _, operation := range operations {
		if operation == target {
			return true
		}
	}
	return false
}

func cloneCandidates(candidates []Candidate) []Candidate {
	return append([]Candidate(nil), candidates...)
}

func clonePresenceEvent(event PresenceEvent) PresenceEvent {
	if event.Offer != nil {
		offer := *event.Offer
		offer.Candidates = cloneCandidates(offer.Candidates)
		event.Offer = &offer
	}
	if event.Candidate != nil {
		candidate := *event.Candidate
		event.Candidate = &candidate
	}
	if event.Closed != nil {
		closed := *event.Closed
		event.Closed = &closed
	}
	return event
}

func cloneClientEvent(event ClientEvent) ClientEvent {
	if event.Answer != nil {
		answer := *event.Answer
		answer.Candidates = cloneCandidates(answer.Candidates)
		event.Answer = &answer
	}
	if event.Candidate != nil {
		candidate := *event.Candidate
		event.Candidate = &candidate
	}
	if event.Failure != nil {
		failure := *event.Failure
		event.Failure = &failure
	}
	if event.Closed != nil {
		closed := *event.Closed
		event.Closed = &closed
	}
	return event
}
