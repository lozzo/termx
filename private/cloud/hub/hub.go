// Package hub 实现私有 regional presence 与 WebRTC signaling runtime。
//
// Hub 只离线验证 Control Plane 签发的 edge credential/policy，并维护 TTL presence、offer、answer
// 和 ICE candidate。它不保存 terminal inventory，不接收 CapabilityGrant，不代理 DataChannel，
// 也不在 signaling 热路径查询 entitlement 或 billing 数据库。
package hub

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

var (
	// ErrAdmission 表示 Hub edge authorization/proof 无效、过期、重放或与请求 identity 不匹配。
	ErrAdmission = errors.New("Hub authorization rejected")
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
// 生产配置为空时使用 UTC wall clock；测试 clock 不能改变 credential 的绝对时间语义。
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now().UTC() }

// Config 固定 regional Hub identity、TTL 上限、本地 edge authorizer 和有界队列容量。
type Config struct {
	HubID                string
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
	// PresenceChallengeTTL 是 Hub fresh DeviceProof challenge 的一次性有效期。
	PresenceChallengeTTL time.Duration
	// MaxPresenceChallenges 限制尚未消费的 daemon Presence challenge 数量。
	MaxPresenceChallenges int
	// Random 只用于生成 Hub 短期 challenge/ID；为空时使用 crypto/rand.Reader。
	Random io.Reader
	// EdgeAuthorizer 是 managed Presence/direct/Relay 的本地授权 owner；缺失时 fail closed。
	EdgeAuthorizer *EdgeAuthorizer
}

// Service 是 Hub 的内存 TTL 状态 owner。
// Presence/signaling maps 是短期 runtime projection；edge policy 由 EdgeAuthorizer 的 verified snapshot 恢复。
type Service struct {
	mu sync.Mutex

	hubID                 string
	clock                 Clock
	maxPresenceTTL        time.Duration
	maxSignalingTTL       time.Duration
	presenceQueue         int
	clientQueue           int
	maxSDPBytes           int
	maxCandidates         int
	maxPresences          int
	maxSessions           int
	maxSessionsPerClient  int
	edgeAuthorizer        *EdgeAuthorizer
	presenceChallengeTTL  time.Duration
	maxPresenceChallenges int
	random                io.Reader

	presences          map[string]*presenceState
	sessions           map[string]*sessionState
	presenceChallenges map[string]edgePresenceChallengeState
}

// New 创建一个无持久状态的 regional Hub service。
// 缺少 Hub identity、本地 authorizer 或有限 TTL/queue 配置时 fail closed。
func New(config Config) (*Service, error) {
	if config.HubID == "" || config.EdgeAuthorizer == nil {
		return nil, fmt.Errorf("Hub identity and edge authorizer are required")
	}
	if config.Clock == nil {
		config.Clock = realClock{}
	}
	if config.Random == nil {
		config.Random = rand.Reader
	}
	if config.MaxPresenceTTL <= 0 || config.MaxPresenceTTL > 10*time.Minute || config.MaxSignalingTTL <= 0 || config.MaxSignalingTTL > 10*time.Minute || config.PresenceChallengeTTL <= 0 || config.PresenceChallengeTTL > 2*time.Minute || config.MaxPresenceChallenges < 1 || config.PresenceQueueSize < 1 || config.ClientQueueSize < 1 || config.MaxSDPBytes < 1 || config.MaxCandidates < 1 || config.MaxPresences < 1 || config.MaxSessions < 1 || config.MaxSessionsPerClient < 1 {
		return nil, fmt.Errorf("invalid Hub TTL or capacity configuration")
	}
	return &Service{
		hubID:           config.HubID,
		clock:           config.Clock,
		maxPresenceTTL:  config.MaxPresenceTTL,
		maxSignalingTTL: config.MaxSignalingTTL,
		presenceQueue:   config.PresenceQueueSize,
		clientQueue:     config.ClientQueueSize,
		maxSDPBytes:     config.MaxSDPBytes,
		maxCandidates:   config.MaxCandidates,
		maxPresences:    config.MaxPresences, maxSessions: config.MaxSessions,
		maxSessionsPerClient: config.MaxSessionsPerClient, edgeAuthorizer: config.EdgeAuthorizer,
		presenceChallengeTTL: config.PresenceChallengeTTL, maxPresenceChallenges: config.MaxPresenceChallenges, random: config.Random,
		presences: make(map[string]*presenceState), sessions: make(map[string]*sessionState), presenceChallenges: make(map[string]edgePresenceChallengeState),
	}, nil
}

// Cleanup 删除过期 presence、signaling session 和 replay entries。
// 调用方可以周期执行；所有请求路径也会 opportunistic cleanup，因此缺少定时器不会无限增长。
func (service *Service) Cleanup() {
	service.mu.Lock()
	service.cleanupLocked(service.clock.Now().UTC())
	service.mu.Unlock()
}

// HasPresence 返回指定 daemon DeviceID 是否拥有尚未过期的 device-scoped presence。
// 该投影只供 Control Plane resolve/dev readiness 判断 online/offline；它不返回 terminal inventory、ManagedSession 或 capability 信息。
func (service *Service) HasPresence(deviceID string) bool {
	if service == nil || deviceID == "" {
		return false
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	now := service.clock.Now().UTC()
	service.cleanupLocked(now)
	presence := service.presences[deviceID]
	return presence != nil && !presence.closed && now.Before(presence.expiresAt)
}

// RevokeDevice 关闭指定 client/daemon 的 Hub 短期状态。
// 调用方必须先应用 Control Plane 签名撤销投影；该方法不接收账号请求，也不修改 CapabilityGrant。
func (service *Service) RevokeDevice(deviceID string) {
	if service == nil || deviceID == "" {
		return
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if presence := service.presences[deviceID]; presence != nil {
		service.closePresenceLocked(presence)
		delete(service.presences, deviceID)
	}
	for sessionID, state := range service.sessions {
		if state.clientDeviceID == deviceID || state.targetDeviceID == deviceID {
			service.closeSessionLocked(state, "device revoked")
			delete(service.sessions, sessionID)
		}
	}
}

func (service *Service) validateOffer(accountID, clientDeviceID, targetDeviceID, managedSessionID, signalingSessionID, sdp string, candidates []Candidate, routePreference RoutePreference, relayOnly bool) error {
	if accountID == "" || clientDeviceID == "" || targetDeviceID == "" || clientDeviceID == targetDeviceID || managedSessionID == "" || signalingSessionID == "" || sdp == "" || len(sdp) > service.maxSDPBytes || len(candidates) > service.maxCandidates || !validCandidates(candidates) || !validRoutePreference(routePreference) || relayOnly && routePreference == RoutePreferenceDirectOnly {
		return ErrInvalidSignal
	}
	return nil
}

func validRoutePreference(preference RoutePreference) bool {
	switch preference {
	case RoutePreferenceDirectOnly, RoutePreferenceStandardRelay, RoutePreferenceSmartRoute, RoutePreferenceGlobalAccelerator:
		return true
	default:
		return false
	}
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
	for sessionID, challenge := range service.presenceChallenges {
		if !now.Before(challenge.challenge.ExpiresAt) {
			clear(challenge.challenge.Value)
			clear(challenge.publicKey)
			delete(service.presenceChallenges, sessionID)
		}
	}
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
