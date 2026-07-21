package daemon

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/muxvia/muxvia/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

var (
	// ErrManagedSessionRegistryTarget 表示调用方提供的 daemon、generation、assignment 或 Presence fence 不属于当前 registry。
	ErrManagedSessionRegistryTarget = errors.New("managed session registry target mismatch")
	// ErrManagedSessionRegistryTransition 表示 session lifecycle 发生非法或重复状态转换。
	ErrManagedSessionRegistryTransition = errors.New("managed session registry transition rejected")
)

// ManagedSessionCloser 是持有 protocol/DataChannel/PeerConnection lifecycle 的内部关闭端口。
// registry 只能请求关闭并等待 Done；RequestClose 必须幂等，最终 CLOSED 必须由真实 runtime owner 在完整 teardown 后通过 handle 提交。
type ManagedSessionCloser interface {
	RequestClose()
	Done() <-chan struct{}
}

// ManagedSessionRegistryPort 是后续 SessionAcceptor、Answerer 与 runtime reporter 使用的窄内部端口。
// 跨进程 payload 始终是 generated cloudpb；该接口不暴露 Go session struct、PeerConnection 或 channel。
type ManagedSessionRegistryPort interface {
	Begin(*cloudpb.ManagedPeerSessionProjection, ManagedSessionCloser, time.Time) (*ManagedSessionHandle, *cloudpb.PeerSessionLifecycleEvent, error)
	ReplaceControlPresence(string, string, uint64, string, time.Time) (*cloudpb.PeerSessionInventorySnapshot, error)
	Inventory(string, time.Time) (*cloudpb.PeerSessionInventorySnapshot, error)
	CloseExact(context.Context, *cloudpb.ManagedPeerSessionTarget, time.Time) (*cloudpb.ExactSessionCloseResult, error)
	CloseAccess(context.Context, string, time.Time) (uint32, uint64, error)
	Changes() <-chan struct{}
}

// CloseAccess 请求关闭所有绑定同一 opaque access reference 的 active managed session。
// AccessStore revoke 必须先提交；本方法只负责等待 protocol/DataChannel/PeerConnection owner 完整结束。
func (registry *ManagedSessionRegistry) CloseAccess(ctx context.Context, opaqueAccessReference string, observedAt time.Time) (uint32, uint64, error) {
	if registry == nil || opaqueAccessReference == "" || observedAt.IsZero() {
		return 0, 0, ErrManagedSessionRegistryTarget
	}
	registry.mu.Lock()
	var targets []*cloudpb.ManagedPeerSessionTarget
	for _, entry := range registry.sessions {
		if entry.projection.GetOpaqueAccessReference() == opaqueAccessReference && entry.projection.GetState() != cloudpb.ManagedPeerSessionState_MANAGED_PEER_SESSION_STATE_CLOSED {
			targets = append(targets, proto.Clone(entry.projection.GetTarget()).(*cloudpb.ManagedPeerSessionTarget))
		}
	}
	revision := registry.revision
	registry.mu.Unlock()
	var closed uint32
	for _, target := range targets {
		result, err := registry.CloseExact(ctx, target, observedAt)
		if err != nil {
			return closed, revision, err
		}
		revision = result.GetRegistryRevision()
		if result.GetDisposition() == cloudpb.ExactSessionCloseDisposition_EXACT_SESSION_CLOSE_DISPOSITION_REQUESTED || result.GetDisposition() == cloudpb.ExactSessionCloseDisposition_EXACT_SESSION_CLOSE_DISPOSITION_ALREADY_CLOSED {
			closed++
		}
	}
	return closed, revision, nil
}

type managedSessionKey struct {
	managedSessionID   string
	sessionIncarnation uint64
}

type managedSessionEntry struct {
	projection *cloudpb.ManagedPeerSessionProjection
	closer     ManagedSessionCloser
}

// ManagedSessionRegistry 是 daemon Go runtime 对 authenticated Cloud managed PeerSession 的进程内唯一真值。
// Direct/SSH session 不进入该 registry；inventory 与 lifecycle event 共用 daemon runtime generation 和单调 revision。
type ManagedSessionRegistry struct {
	mu sync.Mutex

	daemonDeviceID           string
	runtimeGeneration        string
	controlOwnerHubID        string
	assignmentEpoch          uint64
	controlPresenceSessionID string
	revision                 uint64
	sessions                 map[managedSessionKey]*managedSessionEntry
	maxIncarnation           map[string]uint64
	changes                  chan struct{}
}

var _ ManagedSessionRegistryPort = (*ManagedSessionRegistry)(nil)

// NewManagedSessionRegistry 创建一个 daemon process generation 的 managed session registry。
// 参数来自当前 authenticated Presence；空 identity、epoch 或 generation 会 fail closed。
func NewManagedSessionRegistry(daemonDeviceID, runtimeGeneration, controlOwnerHubID string, assignmentEpoch uint64, controlPresenceSessionID string) (*ManagedSessionRegistry, error) {
	if daemonDeviceID == "" || runtimeGeneration == "" || controlOwnerHubID == "" || assignmentEpoch == 0 || controlPresenceSessionID == "" {
		return nil, fmt.Errorf("create managed session registry: %w", ErrManagedSessionRegistryTarget)
	}
	return &ManagedSessionRegistry{
		daemonDeviceID:           daemonDeviceID,
		runtimeGeneration:        runtimeGeneration,
		controlOwnerHubID:        controlOwnerHubID,
		assignmentEpoch:          assignmentEpoch,
		controlPresenceSessionID: controlPresenceSessionID,
		sessions:                 make(map[managedSessionKey]*managedSessionEntry),
		maxIncarnation:           make(map[string]uint64),
		changes:                  make(chan struct{}, 1),
	}, nil
}

// ManagedSessionHandle 绑定 registry 内一个精确 managed session incarnation。
// handle 只允许真实 runtime owner 提交 READY/CLOSED，不暴露 registry map 或网络对象。
type ManagedSessionHandle struct {
	registry *ManagedSessionRegistry
	key      managedSessionKey
}

// Begin 注册一个已经完成 DataChannel auth 与 CapabilityGrant 校验、但尚未完成 protocol Hello 的 session。
// projection 必须使用当前 daemon/runtime/assignment/Presence fence，且 state 必须为 AUTHENTICATED。
func (registry *ManagedSessionRegistry) Begin(projection *cloudpb.ManagedPeerSessionProjection, closer ManagedSessionCloser, observedAt time.Time) (*ManagedSessionHandle, *cloudpb.PeerSessionLifecycleEvent, error) {
	if registry == nil || projection == nil || projection.GetTarget() == nil || closer == nil || closer.Done() == nil || observedAt.IsZero() {
		return nil, nil, fmt.Errorf("begin managed session: %w", ErrManagedSessionRegistryTarget)
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()

	target := projection.GetTarget()
	if target.GetDaemonDeviceId() != registry.daemonDeviceID ||
		target.GetDaemonRuntimeGeneration() != registry.runtimeGeneration ||
		target.GetAssignmentEpoch() != registry.assignmentEpoch ||
		target.GetControlPresenceSessionId() != registry.controlPresenceSessionID ||
		projection.GetControlOwnerHubId() != registry.controlOwnerHubID ||
		target.GetManagedSessionId() == "" || target.GetSessionIncarnation() == 0 ||
		projection.GetClientDeviceId() == "" || projection.GetEstablishedPresenceSessionId() != registry.controlPresenceSessionID ||
		projection.GetObservedDataPath() == cloudpb.ObservedPath_OBSERVED_PATH_UNSPECIFIED ||
		projection.GetState() != cloudpb.ManagedPeerSessionState_MANAGED_PEER_SESSION_STATE_AUTHENTICATED {
		return nil, nil, fmt.Errorf("begin managed session: %w", ErrManagedSessionRegistryTarget)
	}
	key := managedSessionKey{managedSessionID: target.GetManagedSessionId(), sessionIncarnation: target.GetSessionIncarnation()}
	if _, exists := registry.sessions[key]; exists || target.GetSessionIncarnation() <= registry.maxIncarnation[target.GetManagedSessionId()] {
		return nil, nil, fmt.Errorf("begin managed session %s/%d: %w", key.managedSessionID, key.sessionIncarnation, ErrManagedSessionRegistryTransition)
	}
	owned := proto.Clone(projection).(*cloudpb.ManagedPeerSessionProjection)
	owned.ObservedAtUnixMillis = observedAt.UnixMilli()
	registry.sessions[key] = &managedSessionEntry{projection: owned, closer: closer}
	registry.maxIncarnation[key.managedSessionID] = key.sessionIncarnation
	registry.revision++
	registry.notifyLocked()
	return &ManagedSessionHandle{registry: registry, key: key}, registry.lifecycleEventLocked(owned, observedAt), nil
}

// MarkReady 在 protocol Hello 被接受且 response 已进入发送队列后线性化 READY。
// 任何重复 READY、CLOSING 或 CLOSED 转换都会被拒绝。
func (handle *ManagedSessionHandle) MarkReady(observedAt time.Time) (*cloudpb.PeerSessionLifecycleEvent, error) {
	if handle == nil || handle.registry == nil || observedAt.IsZero() {
		return nil, fmt.Errorf("mark managed session ready: %w", ErrManagedSessionRegistryTransition)
	}
	registry := handle.registry
	registry.mu.Lock()
	defer registry.mu.Unlock()
	entry := registry.sessions[handle.key]
	if entry == nil || entry.projection.GetState() != cloudpb.ManagedPeerSessionState_MANAGED_PEER_SESSION_STATE_AUTHENTICATED {
		return nil, fmt.Errorf("mark managed session ready: %w", ErrManagedSessionRegistryTransition)
	}
	entry.projection.State = cloudpb.ManagedPeerSessionState_MANAGED_PEER_SESSION_STATE_READY
	entry.projection.ObservedAtUnixMillis = observedAt.UnixMilli()
	registry.revision++
	registry.notifyLocked()
	return registry.lifecycleEventLocked(entry.projection, observedAt), nil
}

// MarkClosed 由持有 PeerConnection 的 runtime owner 在 protocol、resource、DataChannel 和 peer 全部结束后调用。
// CLOSED revision 是 Cloud 可以确认 session 已完整结束的唯一 daemon 证据。
func (handle *ManagedSessionHandle) MarkClosed(reasonCode string, observedAt time.Time) (*cloudpb.PeerSessionLifecycleEvent, error) {
	if handle == nil || handle.registry == nil || observedAt.IsZero() {
		return nil, fmt.Errorf("mark managed session closed: %w", ErrManagedSessionRegistryTransition)
	}
	registry := handle.registry
	registry.mu.Lock()
	defer registry.mu.Unlock()
	entry := registry.sessions[handle.key]
	if entry == nil || entry.projection.GetState() == cloudpb.ManagedPeerSessionState_MANAGED_PEER_SESSION_STATE_CLOSED {
		return nil, fmt.Errorf("mark managed session closed: %w", ErrManagedSessionRegistryTransition)
	}
	entry.projection.State = cloudpb.ManagedPeerSessionState_MANAGED_PEER_SESSION_STATE_CLOSED
	entry.projection.CloseReasonCode = reasonCode
	entry.projection.ObservedAtUnixMillis = observedAt.UnixMilli()
	registry.revision++
	registry.notifyLocked()
	return registry.lifecycleEventLocked(entry.projection, observedAt), nil
}

// ReplaceControlPresence 把仍存活的 session 重新绑定到同 assignment epoch 的新控制 Presence。
// established Presence 保留在 projection 中；跨 assignment epoch 不允许复活旧 session。
func (registry *ManagedSessionRegistry) ReplaceControlPresence(reportID, controlOwnerHubID string, assignmentEpoch uint64, controlPresenceSessionID string, observedAt time.Time) (*cloudpb.PeerSessionInventorySnapshot, error) {
	if registry == nil || reportID == "" || controlOwnerHubID == "" || controlPresenceSessionID == "" || observedAt.IsZero() {
		return nil, fmt.Errorf("replace managed session control presence: %w", ErrManagedSessionRegistryTarget)
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if controlOwnerHubID != registry.controlOwnerHubID || assignmentEpoch != registry.assignmentEpoch {
		return nil, fmt.Errorf("replace managed session control presence: %w", ErrManagedSessionRegistryTarget)
	}
	if controlPresenceSessionID != registry.controlPresenceSessionID {
		registry.controlPresenceSessionID = controlPresenceSessionID
		for _, entry := range registry.sessions {
			if entry.projection.GetState() != cloudpb.ManagedPeerSessionState_MANAGED_PEER_SESSION_STATE_CLOSED {
				entry.projection.Target.ControlPresenceSessionId = controlPresenceSessionID
				entry.projection.ObservedAtUnixMillis = observedAt.UnixMilli()
			}
		}
		registry.revision++
		registry.notifyLocked()
	}
	snapshot := registry.inventoryLocked(observedAt)
	snapshot.ReportId = reportID
	return snapshot, nil
}

// Inventory 返回当前 revision 的完整 active session replacement；空 sessions 仍是有效快照。
func (registry *ManagedSessionRegistry) Inventory(reportID string, observedAt time.Time) (*cloudpb.PeerSessionInventorySnapshot, error) {
	if registry == nil || reportID == "" || observedAt.IsZero() {
		return nil, fmt.Errorf("snapshot managed sessions: %w", ErrManagedSessionRegistryTarget)
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	snapshot := registry.inventoryLocked(observedAt)
	snapshot.ReportId = reportID
	return snapshot, nil
}

// CloseExact 请求关闭精确 runtime/assignment/Presence/session incarnation，并等待真实 owner 的 Done。
// topology 状态变化不能替代该等待；owner 未先提交 CLOSED 时返回 transition error。
func (registry *ManagedSessionRegistry) CloseExact(ctx context.Context, target *cloudpb.ManagedPeerSessionTarget, observedAt time.Time) (*cloudpb.ExactSessionCloseResult, error) {
	if registry == nil || target == nil || observedAt.IsZero() {
		return nil, fmt.Errorf("close exact managed session: %w", ErrManagedSessionRegistryTarget)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	key := managedSessionKey{managedSessionID: target.GetManagedSessionId(), sessionIncarnation: target.GetSessionIncarnation()}
	registry.mu.Lock()
	if target.GetDaemonDeviceId() != registry.daemonDeviceID || target.GetDaemonRuntimeGeneration() != registry.runtimeGeneration ||
		target.GetAssignmentEpoch() != registry.assignmentEpoch || target.GetControlPresenceSessionId() != registry.controlPresenceSessionID {
		revision := registry.revision
		registry.mu.Unlock()
		return &cloudpb.ExactSessionCloseResult{Target: proto.Clone(target).(*cloudpb.ManagedPeerSessionTarget), Disposition: cloudpb.ExactSessionCloseDisposition_EXACT_SESSION_CLOSE_DISPOSITION_STALE_TARGET, RegistryRevision: revision, ReasonCode: "stale_target"}, nil
	}
	entry := registry.sessions[key]
	if entry == nil {
		revision := registry.revision
		registry.mu.Unlock()
		return &cloudpb.ExactSessionCloseResult{Target: proto.Clone(target).(*cloudpb.ManagedPeerSessionTarget), Disposition: cloudpb.ExactSessionCloseDisposition_EXACT_SESSION_CLOSE_DISPOSITION_NOT_FOUND, RegistryRevision: revision, ReasonCode: "not_found"}, nil
	}
	if entry.projection.GetState() == cloudpb.ManagedPeerSessionState_MANAGED_PEER_SESSION_STATE_CLOSED {
		revision := registry.revision
		registry.mu.Unlock()
		return &cloudpb.ExactSessionCloseResult{Target: proto.Clone(target).(*cloudpb.ManagedPeerSessionTarget), Disposition: cloudpb.ExactSessionCloseDisposition_EXACT_SESSION_CLOSE_DISPOSITION_ALREADY_CLOSED, RegistryRevision: revision, ReasonCode: entry.projection.GetCloseReasonCode()}, nil
	}
	if entry.projection.GetState() != cloudpb.ManagedPeerSessionState_MANAGED_PEER_SESSION_STATE_CLOSING {
		entry.projection.State = cloudpb.ManagedPeerSessionState_MANAGED_PEER_SESSION_STATE_CLOSING
		entry.projection.ObservedAtUnixMillis = observedAt.UnixMilli()
		registry.revision++
		registry.notifyLocked()
	}
	closer := entry.closer
	registry.mu.Unlock()

	closer.RequestClose()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-closer.Done():
	}

	registry.mu.Lock()
	defer registry.mu.Unlock()
	entry = registry.sessions[key]
	if entry == nil || entry.projection.GetState() != cloudpb.ManagedPeerSessionState_MANAGED_PEER_SESSION_STATE_CLOSED {
		return nil, fmt.Errorf("close exact managed session owner completed before CLOSED: %w", ErrManagedSessionRegistryTransition)
	}
	return &cloudpb.ExactSessionCloseResult{
		Target:           proto.Clone(target).(*cloudpb.ManagedPeerSessionTarget),
		Disposition:      cloudpb.ExactSessionCloseDisposition_EXACT_SESSION_CLOSE_DISPOSITION_REQUESTED,
		RegistryRevision: registry.revision,
		ReasonCode:       entry.projection.GetCloseReasonCode(),
	}, nil
}

// Changes 返回 registry revision 的有界通知源。
// 通知可以合并；reporter 必须每次重新读取完整 Inventory，不能把通知次数当作 revision。
func (registry *ManagedSessionRegistry) Changes() <-chan struct{} {
	if registry == nil {
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	return registry.changes
}

func (registry *ManagedSessionRegistry) notifyLocked() {
	select {
	case registry.changes <- struct{}{}:
	default:
	}
}

func (registry *ManagedSessionRegistry) lifecycleEventLocked(projection *cloudpb.ManagedPeerSessionProjection, observedAt time.Time) *cloudpb.PeerSessionLifecycleEvent {
	return &cloudpb.PeerSessionLifecycleEvent{
		EventId:                  fmt.Sprintf("%s:%d", registry.runtimeGeneration, registry.revision),
		DaemonDeviceId:           registry.daemonDeviceID,
		ControlOwnerHubId:        registry.controlOwnerHubID,
		AssignmentEpoch:          registry.assignmentEpoch,
		ControlPresenceSessionId: registry.controlPresenceSessionID,
		DaemonRuntimeGeneration:  registry.runtimeGeneration,
		RegistryRevision:         registry.revision,
		Session:                  proto.Clone(projection).(*cloudpb.ManagedPeerSessionProjection),
		ObservedAtUnixMillis:     observedAt.UnixMilli(),
	}
}

func (registry *ManagedSessionRegistry) inventoryLocked(observedAt time.Time) *cloudpb.PeerSessionInventorySnapshot {
	snapshot := &cloudpb.PeerSessionInventorySnapshot{
		DaemonDeviceId:           registry.daemonDeviceID,
		ControlOwnerHubId:        registry.controlOwnerHubID,
		AssignmentEpoch:          registry.assignmentEpoch,
		ControlPresenceSessionId: registry.controlPresenceSessionID,
		DaemonRuntimeGeneration:  registry.runtimeGeneration,
		RegistryRevision:         registry.revision,
		ObservedAtUnixMillis:     observedAt.UnixMilli(),
	}
	for _, entry := range registry.sessions {
		if entry.projection.GetState() == cloudpb.ManagedPeerSessionState_MANAGED_PEER_SESSION_STATE_CLOSED {
			continue
		}
		snapshot.Sessions = append(snapshot.Sessions, proto.Clone(entry.projection).(*cloudpb.ManagedPeerSessionProjection))
	}
	sort.Slice(snapshot.Sessions, func(left, right int) bool {
		leftTarget := snapshot.Sessions[left].GetTarget()
		rightTarget := snapshot.Sessions[right].GetTarget()
		if leftTarget.GetManagedSessionId() != rightTarget.GetManagedSessionId() {
			return leftTarget.GetManagedSessionId() < rightTarget.GetManagedSessionId()
		}
		return leftTarget.GetSessionIncarnation() < rightTarget.GetSessionIncarnation()
	})
	return snapshot
}
