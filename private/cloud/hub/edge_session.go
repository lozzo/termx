package hub

import (
	"context"

	"github.com/lozzow/termx/private/cloud/control-plane/servicecredential"
)

// CreateEdgeSessionRequest 描述 client 使用启动阶段 edge token 发起的 managed direct offer。
// ClientConnectionID 来自 Hub 已认证连接，不能由不可信 payload 覆盖；ManagedSessionID 由 Hub 生成。
type CreateEdgeSessionRequest struct {
	EdgeToken          []byte
	AccountID          string
	ClientDeviceID     string
	ClientConnectionID string
	TargetDeviceID     string
	SignalingSessionID string
	SDP                string
	Candidates         []Candidate
}

// CreateEdgeSession 只读取 Hub 本地授权投影并创建 Hub-owned EdgeManagedSession。
// cache miss、陈旧快照或无 active target presence 时 fail closed，且没有同步 Control Plane fallback。
func (service *Service) CreateEdgeSession(_ context.Context, request CreateEdgeSessionRequest) (*ClientSession, error) {
	if service.edgeAuthorizer == nil || request.ClientConnectionID == "" {
		return nil, ErrAdmission
	}
	if _, err := service.edgeAuthorizer.AuthorizeDirect(request.EdgeToken, request.AccountID, request.ClientDeviceID, request.TargetDeviceID); err != nil {
		return nil, err
	}
	managedSessionID := "edge-" + request.SignalingSessionID
	legacy := CreateSessionRequest{AccountID: request.AccountID, ClientDeviceID: request.ClientDeviceID, TargetDeviceID: request.TargetDeviceID, ManagedSessionID: managedSessionID, SignalingSessionID: request.SignalingSessionID, SDP: request.SDP, Candidates: request.Candidates, RoutePreference: RoutePreferenceDirectOnly}
	if err := service.validateOffer(legacy); err != nil {
		return nil, err
	}
	now := service.clock.Now().UTC()
	state := &sessionState{id: request.SignalingSessionID, accountID: request.AccountID, managedSessionID: managedSessionID, clientDeviceID: request.ClientDeviceID, clientConnectionID: request.ClientConnectionID, targetDeviceID: request.TargetDeviceID, allowedOperations: []servicecredential.HubOperation{servicecredential.HubOperationOffer, servicecredential.HubOperationCandidate}, expiresAt: now.Add(service.maxSignalingTTL), clientEvents: make(chan ClientEvent, service.clientQueue), done: make(chan struct{})}
	offer := Offer{SignalingSessionID: state.id, ManagedSessionID: state.managedSessionID, SourceDeviceID: state.clientDeviceID, TargetDeviceID: state.targetDeviceID, SDP: request.SDP, Candidates: cloneCandidates(request.Candidates), RoutePreference: RoutePreferenceDirectOnly}
	service.mu.Lock()
	defer service.mu.Unlock()
	service.cleanupLocked(now)
	presence := service.presences[request.TargetDeviceID]
	if presence == nil || presence.closed || !now.Before(presence.expiresAt) || presence.accountID != request.AccountID {
		return nil, ErrPresenceNotFound
	}
	if _, exists := service.sessions[state.id]; exists {
		return nil, ErrSessionConflict
	}
	if len(service.sessions) >= service.maxSessions || service.sessionsForClientLocked(request.ClientDeviceID) >= service.maxSessionsPerClient {
		return nil, ErrCapacity
	}
	select {
	case presence.events <- PresenceEvent{Offer: &offer}:
		service.sessions[state.id] = state
	default:
		return nil, ErrBackpressure
	}
	return &ClientSession{service: service, state: state}, nil
}

// CompleteEdgeAnswerRequest 绑定 daemon answer 与承接 offer 的 active presence。
// PresenceSessionID 必须来自服务端已认证 stream，不能由 daemon 任意声明其他 presence。
type CompleteEdgeAnswerRequest struct {
	AccountID          string
	DaemonDeviceID     string
	PresenceSessionID  string
	SignalingSessionID string
	SDP                string
	Candidates         []Candidate
}

// CompleteEdgeAnswer 以 active presence ownership 完成 Hub-owned session，不领取 Control Plane answer ticket。
func (service *Service) CompleteEdgeAnswer(_ context.Context, request CompleteEdgeAnswerRequest) (*DaemonSession, error) {
	if request.AccountID == "" || request.DaemonDeviceID == "" || request.PresenceSessionID == "" || request.SignalingSessionID == "" || request.SDP == "" || len(request.SDP) > service.maxSDPBytes || len(request.Candidates) > service.maxCandidates || !validCandidates(request.Candidates) {
		return nil, ErrInvalidSignal
	}
	now := service.clock.Now().UTC()
	service.mu.Lock()
	defer service.mu.Unlock()
	service.cleanupLocked(now)
	presence := service.presences[request.DaemonDeviceID]
	state := service.sessions[request.SignalingSessionID]
	if presence == nil || presence.closed || presence.accountID != request.AccountID || presence.sessionID != request.PresenceSessionID || state == nil || state.closed || state.accountID != request.AccountID || state.targetDeviceID != request.DaemonDeviceID {
		return nil, ErrAdmission
	}
	if state.answered {
		return nil, ErrSessionConflict
	}
	event := ClientEvent{Answer: &Answer{SignalingSessionID: request.SignalingSessionID, SDP: request.SDP, Candidates: cloneCandidates(request.Candidates)}}
	select {
	case state.clientEvents <- event:
		state.answered = true
		state.daemonOperations = []servicecredential.HubOperation{servicecredential.HubOperationAnswer, servicecredential.HubOperationCandidate}
		return &DaemonSession{service: service, state: state, deviceID: request.DaemonDeviceID}, nil
	default:
		return nil, ErrBackpressure
	}
}
