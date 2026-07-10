// Package hub 实现私有 regional presence 与 WebRTC signaling runtime。
//
// Hub 只离线验证 Control Plane 签发的短期 admission，并维护 TTL presence、offer、answer
// 和 ICE candidate。它不保存 terminal inventory，不接收 CapabilityGrant，不代理 DataChannel，
// 也不在 signaling 热路径查询 entitlement 或 billing 数据库。
package hub

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/lozzow/termx/private/termx-cloud/control-plane/servicecredential"
)

var (
	// ErrAdmission 表示 Hub admission 无效、过期、重放或与请求 identity 不匹配。
	ErrAdmission = errors.New("Hub admission rejected")
	// ErrPresenceNotFound 表示 target daemon 当前没有有效 presence。
	ErrPresenceNotFound = errors.New("Hub target presence not found")
	// ErrPresenceConflict 表示同一 device 已由另一个 active presence 占用。
	ErrPresenceConflict = errors.New("Hub device presence already active")
	// ErrSessionNotFound 表示 signaling session 不存在、已过期或已关闭。
	ErrSessionNotFound = errors.New("Hub signaling session not found")
	// ErrSessionConflict 表示 signaling ID 已被不同 session 使用或 answer 重复提交。
	ErrSessionConflict = errors.New("Hub signaling session conflict")
	// ErrBackpressure 表示目标 presence 或 client event queue 已满。
	ErrBackpressure = errors.New("Hub signaling queue is full")
	// ErrCapacity 表示 Hub 全局或单设备短期状态已达到配置上限。
	ErrCapacity = errors.New("Hub signaling capacity exhausted")
	// ErrInvalidSignal 表示 SDP、candidate 或 identity binding 非法。
	ErrInvalidSignal = errors.New("invalid Hub signaling payload")
)

// Clock 是 Hub TTL harness 使用的时间来源。
// 生产配置为空时使用 UTC wall clock；测试 clock 不能改变 admission claims 的绝对时间语义。
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now().UTC() }

// Config 固定 regional Hub identity、票据 issuer、TTL 上限和有界队列容量。
// KeyRing 由 Control Plane 公钥分发/轮换流程更新；Hub 不持有签名私钥。
type Config struct {
	HubID                string
	AdmissionIssuer      string
	KeyRing              *servicecredential.KeyRing
	Clock                Clock
	MaxPresenceTTL       time.Duration
	MaxSignalingTTL      time.Duration
	PresenceQueueSize    int
	ClientQueueSize      int
	MaxSDPBytes          int
	MaxCandidates        int
	MaxPresences         int
	MaxSessions          int
	MaxSessionsPerClient int
	MaxReplayEntries     int
}

// Service 是 Hub 的内存 TTL 状态 owner。
// 所有 maps 都是短期 runtime projection；进程重启后由 caller 重新 admission，不做 durable recovery。
type Service struct {
	mu sync.Mutex

	hubID                string
	issuer               string
	keyRing              *servicecredential.KeyRing
	clock                Clock
	maxPresenceTTL       time.Duration
	maxSignalingTTL      time.Duration
	presenceQueue        int
	clientQueue          int
	maxSDPBytes          int
	maxCandidates        int
	maxPresences         int
	maxSessions          int
	maxSessionsPerClient int
	maxReplayEntries     int

	presences  map[string]*presenceState
	sessions   map[string]*sessionState
	usedTicket map[string]time.Time
}

// New 创建一个无持久状态的 regional Hub service。
// 缺少 Hub identity、issuer、公钥或有限 TTL/queue 配置时 fail closed。
func New(config Config) (*Service, error) {
	if config.HubID == "" || config.AdmissionIssuer == "" || config.KeyRing == nil {
		return nil, fmt.Errorf("Hub identity, admission issuer and key ring are required")
	}
	if config.Clock == nil {
		config.Clock = realClock{}
	}
	if config.MaxPresenceTTL <= 0 || config.MaxPresenceTTL > 10*time.Minute || config.MaxSignalingTTL <= 0 || config.MaxSignalingTTL > 10*time.Minute || config.PresenceQueueSize < 1 || config.ClientQueueSize < 1 || config.MaxSDPBytes < 1 || config.MaxCandidates < 1 || config.MaxPresences < 1 || config.MaxSessions < 1 || config.MaxSessionsPerClient < 1 || config.MaxReplayEntries < config.MaxPresences+config.MaxSessions {
		return nil, fmt.Errorf("invalid Hub TTL or capacity configuration")
	}
	return &Service{
		hubID:           config.HubID,
		issuer:          config.AdmissionIssuer,
		keyRing:         config.KeyRing,
		clock:           config.Clock,
		maxPresenceTTL:  config.MaxPresenceTTL,
		maxSignalingTTL: config.MaxSignalingTTL,
		presenceQueue:   config.PresenceQueueSize,
		clientQueue:     config.ClientQueueSize,
		maxSDPBytes:     config.MaxSDPBytes,
		maxCandidates:   config.MaxCandidates,
		maxPresences:    config.MaxPresences, maxSessions: config.MaxSessions,
		maxSessionsPerClient: config.MaxSessionsPerClient, maxReplayEntries: config.MaxReplayEntries,
		presences:  make(map[string]*presenceState),
		sessions:   make(map[string]*sessionState),
		usedTicket: make(map[string]time.Time),
	}, nil
}

// OpenPresenceRequest 绑定 daemon cloud identity 与一次短期 presence registration。
// Admission 不包含 terminal list、grant、长期 agent token 或 heartbeat bearer。
type OpenPresenceRequest struct {
	Admission       []byte
	AccountID       string
	DeviceID        string
	PresenceSession string
}

// OpenPresence 离线验签并注册一个 device-scoped TTL presence stream。
// 同一 active DeviceID 只允许一个 owner；context cancel 或 ticket expiry 会关闭 presence 和关联 sessions。
func (service *Service) OpenPresence(ctx context.Context, request OpenPresenceRequest) (*Presence, error) {
	if ctx == nil || request.AccountID == "" || request.DeviceID == "" || request.PresenceSession == "" {
		return nil, ErrInvalidSignal
	}
	now := service.clock.Now().UTC()
	claims, err := servicecredential.VerifyHubAdmission(service.keyRing, request.Admission, servicecredential.HubAdmissionExpectation{
		Issuer: service.issuer, AudienceHubID: service.hubID, PrincipalKind: servicecredential.PrincipalDaemon,
		AccountID: request.AccountID, DeviceID: request.DeviceID, ManagedSessionID: request.PresenceSession,
		Operation: servicecredential.HubOperationPresence,
	}, now)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAdmission, err)
	}
	expiresAt := minTime(time.Unix(claims.ExpiresAtUnix, 0).UTC(), now.Add(service.maxPresenceTTL))
	state := &presenceState{deviceID: request.DeviceID, accountID: request.AccountID, sessionID: request.PresenceSession, expiresAt: expiresAt, events: make(chan PresenceEvent, service.presenceQueue), done: make(chan struct{})}
	presence := &Presence{service: service, state: state}

	service.mu.Lock()
	service.cleanupLocked(now)
	if err := service.consumeTicketLocked(claims.TicketID, expiresAt, now); err != nil {
		service.mu.Unlock()
		return nil, err
	}
	if current := service.presences[request.DeviceID]; current != nil && !current.closed {
		service.mu.Unlock()
		return nil, ErrPresenceConflict
	}
	if len(service.presences) >= service.maxPresences {
		service.mu.Unlock()
		return nil, ErrCapacity
	}
	service.presences[request.DeviceID] = state
	service.mu.Unlock()

	go func() {
		timer := time.NewTimer(expiresAt.Sub(now))
		defer timer.Stop()
		select {
		case <-ctx.Done():
			_ = presence.Close()
		case <-timer.C:
			_ = presence.Close()
		case <-state.done:
		}
	}()
	return presence, nil
}

// CreateSessionRequest 描述 client 发往固定 target daemon 的单个 WebRTC offer。
// Admission claims 与请求字段必须完全一致；Hub 只看到 SDP/ICE 和 routing metadata。
type CreateSessionRequest struct {
	Admission          []byte
	AccountID          string
	ClientDeviceID     string
	TargetDeviceID     string
	ManagedSessionID   string
	SignalingSessionID string
	SDP                string
	Candidates         []Candidate
}

// CreateSession 离线验签、查找 target presence，并把 offer 放入有界 daemon queue。
// 返回的 ClientSession 是 client 下行 answer/candidate stream owner；创建失败不留下半成品 session。
func (service *Service) CreateSession(_ context.Context, request CreateSessionRequest) (*ClientSession, error) {
	if err := service.validateOffer(request); err != nil {
		return nil, err
	}
	now := service.clock.Now().UTC()
	claims, err := servicecredential.VerifyHubAdmission(service.keyRing, request.Admission, servicecredential.HubAdmissionExpectation{
		Issuer: service.issuer, AudienceHubID: service.hubID, PrincipalKind: servicecredential.PrincipalClient,
		AccountID: request.AccountID, DeviceID: request.ClientDeviceID, ManagedSessionID: request.ManagedSessionID,
		TargetDeviceID: request.TargetDeviceID, Operation: servicecredential.HubOperationOffer,
	}, now)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAdmission, err)
	}
	expiresAt := minTime(time.Unix(claims.ExpiresAtUnix, 0).UTC(), now.Add(service.maxSignalingTTL))
	state := &sessionState{
		id: request.SignalingSessionID, accountID: request.AccountID, managedSessionID: request.ManagedSessionID,
		clientDeviceID: request.ClientDeviceID, targetDeviceID: request.TargetDeviceID,
		allowedOperations: append([]servicecredential.HubOperation(nil), claims.AllowedOperations...), expiresAt: expiresAt,
		clientEvents: make(chan ClientEvent, service.clientQueue), done: make(chan struct{}),
	}
	offer := Offer{SignalingSessionID: state.id, ManagedSessionID: state.managedSessionID, SourceDeviceID: state.clientDeviceID, TargetDeviceID: state.targetDeviceID, SDP: request.SDP, Candidates: cloneCandidates(request.Candidates)}

	service.mu.Lock()
	service.cleanupLocked(now)
	if err := service.consumeTicketLocked(claims.TicketID, expiresAt, now); err != nil {
		service.mu.Unlock()
		return nil, err
	}
	presence := service.presences[request.TargetDeviceID]
	if presence == nil || presence.closed || !now.Before(presence.expiresAt) {
		service.mu.Unlock()
		return nil, ErrPresenceNotFound
	}
	if _, exists := service.sessions[state.id]; exists {
		service.mu.Unlock()
		return nil, ErrSessionConflict
	}
	if len(service.sessions) >= service.maxSessions || service.sessionsForClientLocked(request.ClientDeviceID) >= service.maxSessionsPerClient {
		service.mu.Unlock()
		return nil, ErrCapacity
	}
	select {
	case presence.events <- PresenceEvent{Offer: &offer}:
		service.sessions[state.id] = state
	default:
		service.mu.Unlock()
		return nil, ErrBackpressure
	}
	service.mu.Unlock()
	return &ClientSession{service: service, state: state}, nil
}

// CompleteAnswerRequest 绑定 daemon answer 与目标 signaling session。
// daemon 必须提交另一个 session-specific answer admission，presence ticket 不能替代它。
type CompleteAnswerRequest struct {
	Admission          []byte
	AccountID          string
	DaemonDeviceID     string
	ManagedSessionID   string
	SignalingSessionID string
	SDP                string
	Candidates         []Candidate
}

// CompleteAnswer 离线验证 daemon answer admission，并异步投递给 owning client session。
// 重复 answer、错误 target 或 queue backpressure 都 fail closed，不覆盖首个结果。
func (service *Service) CompleteAnswer(_ context.Context, request CompleteAnswerRequest) (*DaemonSession, error) {
	if request.AccountID == "" || request.DaemonDeviceID == "" || request.ManagedSessionID == "" || request.SignalingSessionID == "" || request.SDP == "" || len(request.SDP) > service.maxSDPBytes || len(request.Candidates) > service.maxCandidates || !validCandidates(request.Candidates) {
		return nil, ErrInvalidSignal
	}
	now := service.clock.Now().UTC()
	claims, err := servicecredential.VerifyHubAdmission(service.keyRing, request.Admission, servicecredential.HubAdmissionExpectation{
		Issuer: service.issuer, AudienceHubID: service.hubID, PrincipalKind: servicecredential.PrincipalDaemon,
		AccountID: request.AccountID, DeviceID: request.DaemonDeviceID, ManagedSessionID: request.ManagedSessionID,
		Operation: servicecredential.HubOperationAnswer,
	}, now)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAdmission, err)
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	service.cleanupLocked(now)
	if err := service.consumeTicketLocked(claims.TicketID, time.Unix(claims.ExpiresAtUnix, 0).UTC(), now); err != nil {
		return nil, err
	}
	state := service.sessions[request.SignalingSessionID]
	if state == nil || state.closed || !now.Before(state.expiresAt) {
		return nil, ErrSessionNotFound
	}
	if state.accountID != request.AccountID || state.targetDeviceID != request.DaemonDeviceID || state.managedSessionID != request.ManagedSessionID {
		return nil, ErrAdmission
	}
	if state.answered {
		return nil, ErrSessionConflict
	}
	answer := Answer{SignalingSessionID: state.id, SDP: request.SDP, Candidates: cloneCandidates(request.Candidates)}
	select {
	case state.clientEvents <- ClientEvent{Answer: &answer}:
		state.answered = true
		state.daemonOperations = append([]servicecredential.HubOperation(nil), claims.AllowedOperations...)
		return &DaemonSession{service: service, state: state, deviceID: request.DaemonDeviceID}, nil
	default:
		return nil, ErrBackpressure
	}
}

// Cleanup 删除过期 presence、signaling session 和 replay entries。
// 调用方可以周期执行；所有请求路径也会 opportunistic cleanup，因此缺少定时器不会无限增长。
func (service *Service) Cleanup() {
	service.mu.Lock()
	service.cleanupLocked(service.clock.Now().UTC())
	service.mu.Unlock()
}

func (service *Service) validateOffer(request CreateSessionRequest) error {
	if request.AccountID == "" || request.ClientDeviceID == "" || request.TargetDeviceID == "" || request.ClientDeviceID == request.TargetDeviceID || request.ManagedSessionID == "" || request.SignalingSessionID == "" || request.SDP == "" || len(request.SDP) > service.maxSDPBytes || len(request.Candidates) > service.maxCandidates || !validCandidates(request.Candidates) {
		return ErrInvalidSignal
	}
	return nil
}

func (service *Service) consumeTicketLocked(ticketID string, expiresAt, now time.Time) error {
	for usedID, expiry := range service.usedTicket {
		if !now.Before(expiry) {
			delete(service.usedTicket, usedID)
		}
	}
	if ticketID == "" {
		return ErrAdmission
	}
	if _, used := service.usedTicket[ticketID]; used {
		return ErrAdmission
	}
	if len(service.usedTicket) >= service.maxReplayEntries {
		return ErrCapacity
	}
	service.usedTicket[ticketID] = expiresAt
	return nil
}

func (service *Service) sessionsForClientLocked(clientDeviceID string) int {
	count := 0
	for _, state := range service.sessions {
		if state.clientDeviceID == clientDeviceID && !state.closed {
			count++
		}
	}
	return count
}

func (service *Service) cleanupLocked(now time.Time) {
	for deviceID, presence := range service.presences {
		if presence.closed || !now.Before(presence.expiresAt) {
			service.closePresenceLocked(presence)
			delete(service.presences, deviceID)
		}
	}
	for sessionID, state := range service.sessions {
		if state.closed || !now.Before(state.expiresAt) {
			service.closeSessionLocked(state, "signaling session expired")
			delete(service.sessions, sessionID)
		}
	}
}

func (service *Service) closePresenceLocked(presence *presenceState) {
	if presence == nil || presence.closed {
		return
	}
	presence.closed = true
	close(presence.done)
	for sessionID, state := range service.sessions {
		if state.targetDeviceID == presence.deviceID {
			service.closeSessionLocked(state, "target presence closed")
			delete(service.sessions, sessionID)
		}
	}
}

func (service *Service) closeSessionLocked(state *sessionState, reason string) {
	if state == nil || state.closed {
		return
	}
	state.closed = true
	select {
	case state.clientEvents <- ClientEvent{Closed: &Closed{Reason: reason}}:
	default:
	}
	close(state.done)
}

func minTime(left, right time.Time) time.Time {
	if left.Before(right) {
		return left
	}
	return right
}
