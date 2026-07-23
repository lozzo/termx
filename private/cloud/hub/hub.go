// Package hub 实现私有 regional presence 与 WebRTC signaling runtime。
//
// Hub 只离线验证 Control Plane 签发的 edge credential/policy，并维护 TTL presence、offer、answer
// 和 ICE candidate。它不保存 terminal inventory，不接收 CapabilityGrant，不代理 DataChannel，
// 也不在 signaling 热路径查询 entitlement 或 billing 数据库。
package hub

import (
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/muxvia/muxvia/proto/cloudpb"
	"google.golang.org/protobuf/proto"
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
	// ErrRuntimeReport 表示 daemon runtime report 与当前 Presence、assignment 或 revision 冲突。
	ErrRuntimeReport = errors.New("invalid or stale Hub daemon runtime report")
)

// Clock 是 Hub TTL harness 使用的时间来源。
// 生产配置为空时使用 UTC wall clock；测试 clock 不能改变 credential 的绝对时间语义。
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now().UTC() }

// AssignmentSource 是 Hub admission 查询当前 daemon assignment 的纯内存边界。
// 返回的 epoch 来自 Projection；缺失或过期时 Presence/signaling 必须 fail closed。
type AssignmentSource interface {
	ActiveAssignment(deviceID string) (assignmentEpoch uint64, ok bool)
}

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
	// AssignmentSource 是 per-Hub assignment 真值投影；Hub 不得只凭 device ownership 接纳 Presence。
	AssignmentSource AssignmentSource
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
	assignmentSource      AssignmentSource
	presenceChallengeTTL  time.Duration
	maxPresenceChallenges int
	random                io.Reader

	presences          map[string]*presenceState
	sessions           map[string]*sessionState
	relayIntents       map[string]relayIntentState
	presenceChallenges map[string]edgePresenceChallengeState
	nextIncarnation    uint64

	topologyRevision uint64
	topologyChanges  chan struct{}
	presenceTopology map[string]*cloudpb.PresenceProjection
	runtimeTopology  map[string]*daemonRuntimeTopology
	runtimeEvents    chan *cloudpb.HubRuntimeEnvelope
	commands         map[string]*hubCommandState
}

type daemonRuntimeTopology struct {
	presenceSessionID string
	assignmentEpoch   uint64
	runtimeGeneration string
	registryRevision  uint64
	accessRevision    uint64
	digest            [sha256.Size]byte
	superseded        map[string]struct{}
	peerSessions      []*cloudpb.ManagedPeerSessionProjection
	terminalAccesses  *cloudpb.TerminalAccessInventorySnapshot
}

// New 创建一个无持久状态的 regional Hub service。
// 缺少 Hub identity、本地 authorizer 或有限 TTL/queue 配置时 fail closed。
func New(config Config) (*Service, error) {
	if config.HubID == "" || config.EdgeAuthorizer == nil || config.AssignmentSource == nil {
		return nil, fmt.Errorf("Hub identity, edge authorizer and assignment source are required")
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
		maxSessionsPerClient: config.MaxSessionsPerClient, edgeAuthorizer: config.EdgeAuthorizer, assignmentSource: config.AssignmentSource,
		presenceChallengeTTL: config.PresenceChallengeTTL, maxPresenceChallenges: config.MaxPresenceChallenges, random: config.Random,
		presences: make(map[string]*presenceState), sessions: make(map[string]*sessionState), relayIntents: make(map[string]relayIntentState), presenceChallenges: make(map[string]edgePresenceChallengeState),
		topologyChanges: make(chan struct{}, 1), presenceTopology: make(map[string]*cloudpb.PresenceProjection), runtimeTopology: make(map[string]*daemonRuntimeTopology), runtimeEvents: make(chan *cloudpb.HubRuntimeEnvelope, config.ClientQueueSize), commands: make(map[string]*hubCommandState),
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
	for managedSessionID, intent := range service.relayIntents {
		if intent.clientDeviceID == deviceID || intent.targetDeviceID == deviceID {
			delete(service.relayIntents, managedSessionID)
		}
	}
}

// FenceAssignment 只关闭仍绑定精确 assignment epoch 的 daemon Presence 和 signaling。
// 迟到的旧 epoch fence 不得影响已经重新接入的新 epoch。
func (service *Service) FenceAssignment(deviceID string, assignmentEpoch uint64) {
	if service == nil || deviceID == "" || assignmentEpoch == 0 {
		return
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	service.fenceAssignmentLocked(deviceID, assignmentEpoch)
}

func (service *Service) fenceAssignmentLocked(deviceID string, assignmentEpoch uint64) bool {
	changed := false
	service.edgeAuthorizer.ReleaseManagedP2PForAssignment(deviceID, assignmentEpoch)
	for sessionID, challenge := range service.presenceChallenges {
		if challenge.deviceID == deviceID && challenge.assignmentEpoch == assignmentEpoch {
			changed = true
			clear(challenge.challenge.Value)
			clear(challenge.publicKey)
			delete(service.presenceChallenges, sessionID)
		}
	}
	presence := service.presences[deviceID]
	if presence == nil || presence.assignmentEpoch != assignmentEpoch {
		return changed
	}
	changed = true
	service.closePresenceLocked(presence)
	delete(service.presences, deviceID)
	return changed
}

func (service *Service) validateOffer(accountID, clientDeviceID, targetDeviceID, managedSessionID, signalingSessionID, sdp string, candidates []Candidate, routePreference RoutePreference, relayOnly bool, relayTransport RelayTransport) error {
	if accountID == "" || clientDeviceID == "" || targetDeviceID == "" || clientDeviceID == targetDeviceID || managedSessionID == "" || signalingSessionID == "" || sdp == "" || len(sdp) > service.maxSDPBytes || len(candidates) > service.maxCandidates || !validCandidates(candidates) || !validRoutePreference(routePreference) || relayOnly && routePreference == RoutePreferenceDirectOnly || !validRelayTransport(relayTransport) {
		return ErrInvalidSignal
	}
	return nil
}

func validRelayTransport(transport RelayTransport) bool {
	switch transport {
	case 0, RelayTransportAuto, RelayTransportUDP, RelayTransportTCP:
		return true
	default:
		return false
	}
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
	for commandID, command := range service.commands {
		if !now.Before(command.expiresAt) {
			delete(service.commands, commandID)
		}
	}
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
	for managedSessionID, intent := range service.relayIntents {
		if !now.Before(intent.expiresAt) {
			delete(service.relayIntents, managedSessionID)
		}
	}
}

func (service *Service) closePresenceLocked(presence *presenceState) {
	if presence == nil || presence.closed {
		return
	}
	presence.closed = true
	service.observePresenceLocked(presence, cloudpb.Availability_AVAILABILITY_OFFLINE, cloudpb.ObservationSource_OBSERVATION_SOURCE_HUB_CLOSE, service.clock.Now().UTC())
	close(presence.done)
	for sessionID, state := range service.sessions {
		if state.targetDeviceID == presence.deviceID {
			service.closeSessionLocked(state, "target presence closed")
			delete(service.sessions, sessionID)
		}
	}
}

// ReportDaemonRuntime 原子接受当前 Presence 的完整 daemon runtime replacement。
// 同 runtime 只允许 revision 单调前进；被新 generation 替换的旧 generation 永远不能复活。
func (service *Service) ReportDaemonRuntime(daemonDeviceID string, request *cloudpb.ReportDaemonRuntimeRequest) (*cloudpb.ReportDaemonRuntimeResponse, error) {
	if service == nil || daemonDeviceID == "" || request == nil || request.GetReportId() == "" || request.GetHubId() != service.hubID || request.GetAssignmentEpoch() == 0 || request.GetPresenceSessionId() == "" || request.GetDaemonRuntimeGeneration() == "" || request.GetPeerSessions() == nil {
		return nil, ErrRuntimeReport
	}
	now := service.clock.Now().UTC()
	service.mu.Lock()
	defer service.mu.Unlock()
	service.cleanupLocked(now)
	presence := service.presences[daemonDeviceID]
	if presence == nil || presence.closed || presence.sessionID != request.GetPresenceSessionId() || presence.assignmentEpoch != request.GetAssignmentEpoch() || !now.Before(presence.expiresAt) {
		return nil, ErrRuntimeReport
	}
	peer := request.GetPeerSessions()
	if peer.GetReportId() != request.GetReportId() || peer.GetDaemonDeviceId() != daemonDeviceID || peer.GetControlOwnerHubId() != service.hubID || peer.GetAssignmentEpoch() != presence.assignmentEpoch || peer.GetControlPresenceSessionId() != presence.sessionID || peer.GetDaemonRuntimeGeneration() != request.GetDaemonRuntimeGeneration() || peer.GetRegistryRevision() != request.GetRegistryRevision() {
		return nil, ErrRuntimeReport
	}
	seenSessions := make(map[string]struct{}, len(peer.GetSessions()))
	for _, session := range peer.GetSessions() {
		target := session.GetTarget()
		key := fmt.Sprintf("%s\x00%d", target.GetManagedSessionId(), target.GetSessionIncarnation())
		_, duplicate := seenSessions[key]
		if session == nil || target == nil || duplicate || target.GetDaemonDeviceId() != daemonDeviceID || target.GetAssignmentEpoch() != presence.assignmentEpoch || target.GetControlPresenceSessionId() != presence.sessionID || target.GetDaemonRuntimeGeneration() != request.GetDaemonRuntimeGeneration() || session.GetControlOwnerHubId() != service.hubID || target.GetManagedSessionId() == "" || target.GetSessionIncarnation() == 0 || session.GetClientDeviceId() == "" || session.GetObservedDataPath() == cloudpb.ObservedPath_OBSERVED_PATH_UNSPECIFIED || session.GetState() == cloudpb.ManagedPeerSessionState_MANAGED_PEER_SESSION_STATE_UNSPECIFIED || session.GetFreshness() == cloudpb.Freshness_FRESHNESS_UNSPECIFIED {
			return nil, ErrRuntimeReport
		}
		seenSessions[key] = struct{}{}
	}
	accessRevision, err := validateRuntimeAccessInventory(service.hubID, daemonDeviceID, request)
	if err != nil {
		return nil, ErrRuntimeReport
	}
	digestBytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(request)
	if err != nil {
		return nil, ErrRuntimeReport
	}
	digest := sha256.Sum256(digestBytes)
	current := service.runtimeTopology[daemonDeviceID]
	peerChanged := true
	if current != nil && current.presenceSessionID == presence.sessionID && current.assignmentEpoch == presence.assignmentEpoch {
		if _, stale := current.superseded[request.GetDaemonRuntimeGeneration()]; stale {
			return nil, ErrRuntimeReport
		}
		if current.runtimeGeneration == request.GetDaemonRuntimeGeneration() {
			if request.GetRegistryRevision() < current.registryRevision || accessRevision < current.accessRevision {
				return nil, ErrRuntimeReport
			}
			if request.GetRegistryRevision() == current.registryRevision && accessRevision == current.accessRevision {
				if current.digest != digest {
					return nil, ErrRuntimeReport
				}
				return runtimeReportResponse(request), nil
			}
			peerChanged = request.GetRegistryRevision() != current.registryRevision
		} else {
			current.superseded[current.runtimeGeneration] = struct{}{}
		}
	} else {
		current = &daemonRuntimeTopology{superseded: make(map[string]struct{})}
	}
	if peerChanged {
		if err := service.edgeAuthorizer.ReconcileManagedP2P(daemonDeviceID, peer.GetSessions()); err != nil {
			return nil, ErrRuntimeReport
		}
	}
	current.presenceSessionID = presence.sessionID
	current.assignmentEpoch = presence.assignmentEpoch
	current.runtimeGeneration = request.GetDaemonRuntimeGeneration()
	current.registryRevision = request.GetRegistryRevision()
	current.accessRevision = accessRevision
	current.digest = digest
	current.peerSessions = cloneManagedPeerSessions(peer.GetSessions())
	current.terminalAccesses = cloneTerminalAccessInventory(request.GetTerminalAccesses())
	service.runtimeTopology[daemonDeviceID] = current
	service.observePresenceLocked(presence, cloudpb.Availability_AVAILABILITY_ONLINE, cloudpb.ObservationSource_OBSERVATION_SOURCE_DAEMON_INVENTORY, now)
	return runtimeReportResponse(request), nil
}

func validateRuntimeAccessInventory(hubID, daemonDeviceID string, request *cloudpb.ReportDaemonRuntimeRequest) (uint64, error) {
	inventory := request.GetTerminalAccesses()
	if inventory == nil {
		return 0, nil
	}
	if inventory.GetReportId() != request.GetReportId() || inventory.GetDaemonDeviceId() != daemonDeviceID || inventory.GetControlOwnerHubId() != hubID || inventory.GetAssignmentEpoch() != request.GetAssignmentEpoch() || inventory.GetControlPresenceSessionId() != request.GetPresenceSessionId() || inventory.GetDaemonRuntimeGeneration() != request.GetDaemonRuntimeGeneration() || inventory.GetRegistryRevision() != request.GetRegistryRevision() || inventory.GetObservedAtUnixMillis() <= 0 {
		return 0, ErrRuntimeReport
	}
	revision := inventory.GetAccessProjectionRevision()
	seen := make(map[string]struct{}, len(inventory.GetAccesses()))
	for _, access := range inventory.GetAccesses() {
		_, duplicate := seen[access.GetOpaqueAccessReference()]
		if access == nil || duplicate || revision == 0 || access.GetDaemonDeviceId() != daemonDeviceID || access.GetOpaqueAccessReference() == "" || access.GetSubjectFingerprintSummary() == "" || access.GetState() == cloudpb.TerminalAccessState_TERMINAL_ACCESS_STATE_UNSPECIFIED || access.GetIssuedAtUnixMillis() <= 0 || access.GetExpiresAtUnixMillis() <= access.GetIssuedAtUnixMillis() || access.GetAccessProjectionRevision() != revision {
			return 0, ErrRuntimeReport
		}
		seen[access.GetOpaqueAccessReference()] = struct{}{}
	}
	return revision, nil
}

// TopologyChanges 返回 Hub 内存 topology revision 的有界通知源；通知允许合并。
func (service *Service) TopologyChanges() <-chan struct{} {
	if service == nil {
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	return service.topologyChanges
}

// TopologySnapshot 返回当前 Hub control generation 的完整内存拓扑快照。
// 快照不包含 terminal identity、SDP、ICE、IP、grant body 或 DataChannel payload。
func (service *Service) TopologySnapshot(controlGeneration uint64, observedAt time.Time) *cloudpb.HubTopologySnapshot {
	service.mu.Lock()
	defer service.mu.Unlock()
	snapshot := &cloudpb.HubTopologySnapshot{HubId: service.hubID, ControlGeneration: controlGeneration, TopologyRevision: service.topologyRevision, ObservedAtUnixMillis: observedAt.UTC().UnixMilli()}
	for _, presence := range service.presenceTopology {
		snapshot.Presences = append(snapshot.Presences, proto.Clone(presence).(*cloudpb.PresenceProjection))
	}
	for _, runtime := range service.runtimeTopology {
		snapshot.PeerSessions = append(snapshot.PeerSessions, cloneManagedPeerSessions(runtime.peerSessions)...)
		if runtime.terminalAccesses != nil {
			snapshot.TerminalAccessInventories = append(snapshot.TerminalAccessInventories, cloneTerminalAccessInventory(runtime.terminalAccesses))
		}
	}
	sortTopologySnapshot(snapshot)
	unsigned := proto.Clone(snapshot).(*cloudpb.HubTopologySnapshot)
	unsigned.TopologyDigest = nil
	payload, _ := proto.MarshalOptions{Deterministic: true}.Marshal(unsigned)
	digest := sha256.Sum256(payload)
	snapshot.TopologyDigest = digest[:]
	return snapshot
}

func (service *Service) observePresenceLocked(presence *presenceState, availability cloudpb.Availability, source cloudpb.ObservationSource, observedAt time.Time) {
	if presence == nil {
		return
	}
	current := service.presenceTopology[presence.deviceID]
	if current != nil && (current.GetAssignmentEpoch() != presence.assignmentEpoch || current.GetPresenceSessionId() != presence.sessionID) && availability == cloudpb.Availability_AVAILABILITY_OFFLINE {
		return
	}
	projection := &cloudpb.PresenceProjection{DaemonDeviceId: presence.deviceID, ControlOwnerHubId: service.hubID, AssignmentEpoch: presence.assignmentEpoch, PresenceSessionId: presence.sessionID, Availability: availability, Freshness: cloudpb.Freshness_FRESHNESS_FRESH, ObservationSource: source, ObservedAtUnixMillis: observedAt.UnixMilli(), FreshUntilUnixMillis: observedAt.Add(service.maxPresenceTTL).UnixMilli()}
	if runtime := service.runtimeTopology[presence.deviceID]; runtime != nil && runtime.presenceSessionID == presence.sessionID && runtime.assignmentEpoch == presence.assignmentEpoch {
		projection.DaemonRuntimeGeneration = runtime.runtimeGeneration
		projection.RegistryRevision = runtime.registryRevision
	}
	service.presenceTopology[presence.deviceID] = projection
	service.topologyRevision++
	service.notifyTopologyLocked()
}

func (service *Service) notifyTopologyLocked() {
	select {
	case service.topologyChanges <- struct{}{}:
	default:
	}
}

func runtimeReportResponse(request *cloudpb.ReportDaemonRuntimeRequest) *cloudpb.ReportDaemonRuntimeResponse {
	response := &cloudpb.ReportDaemonRuntimeResponse{ReportId: request.GetReportId(), DaemonRuntimeGeneration: request.GetDaemonRuntimeGeneration(), AcceptedRegistryRevision: request.GetRegistryRevision()}
	if request.GetTerminalAccesses() != nil {
		response.AcceptedAccessProjectionRevision = request.GetTerminalAccesses().GetAccessProjectionRevision()
	}
	return response
}

func (service *Service) closeSessionLocked(state *sessionState, reason string) {
	if state == nil || state.closed {
		return
	}
	state.closed = true
	service.edgeAuthorizer.CloseManagedP2PSignaling(state.p2pReservationID, state.answered)
	state.p2pReservationID = ""
	select {
	case state.clientEvents <- ClientEvent{Closed: &Closed{Reason: reason}}:
	default:
	}
	close(state.done)
}
