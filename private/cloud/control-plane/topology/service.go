// Package topology 校验并持久化 Controller 对 Hub runtime 的最后一次可信观察。
//
// Hub snapshot 不携带可信账号归属；Service 必须从持久 assignment 与 device ownership 推导账号，
// 再把完整 replacement 交给 Store。投影不是活跃连接真值，断流只能降级为 UNKNOWN/STALE。
package topology

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/lozzow/termx/private/cloud/control-plane/hubregistry"
	"github.com/lozzow/termx/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

var (
	// ErrTopologyRejected 表示 snapshot digest、assignment、ownership 或 fencing 不成立。
	ErrTopologyRejected = errors.New("Hub topology snapshot rejected")
	// ErrOwnershipNotFound 表示设备没有持久 ownership 真值。
	ErrOwnershipNotFound = errors.New("cloud device ownership not found")
)

// DeviceOwnership 是 Controller 持久设备目录中用于 topology 校验的最小归属。
type DeviceOwnership struct {
	DeviceID  string
	AccountID string
	Kind      cloudpb.ManagedDeviceKind
	AuthEpoch uint64
	Revoked   bool
	PublicKey []byte
}

// StoredPeerSession 是 CommandOutbox target planner 使用的账号与 owning Hub 精确投影。
// Value 仍是 generated Proto；该 wrapper 不跨 Controller 领域边界。
type StoredPeerSession struct {
	AccountID string
	HubID     string
	Value     *cloudpb.ManagedPeerSessionProjection
}

// ValidatedPresence 是已经由 assignment 与 ownership 推导账号后的 Presence 投影。
type ValidatedPresence struct {
	AccountID string
	Value     *cloudpb.PresenceProjection
}

// ValidatedPeerSession 是已经验证 client/daemon 同账号及精确 fencing 的 session 投影。
type ValidatedPeerSession struct {
	AccountID string
	Value     *cloudpb.ManagedPeerSessionProjection
}

// ValidatedSnapshot 是 Store 可以原子完整替换的 Controller 内部模型。
type ValidatedSnapshot struct {
	HubID             string
	ControlGeneration uint64
	TopologyRevision  uint64
	Digest            []byte
	ObservedAt        time.Time
	Presences         []ValidatedPresence
	PeerSessions      []ValidatedPeerSession
}

// Store 是 topology ownership 与持久 projection 的事务边界。
type Store interface {
	PutDeviceOwnership(context.Context, DeviceOwnership) error
	DeviceOwnership(context.Context, string) (DeviceOwnership, error)
	DevicePolicies(context.Context) ([]*cloudpb.CloudDevicePolicy, error)
	PeerSessionProjection(context.Context, *cloudpb.ManagedPeerSessionTarget) (StoredPeerSession, error)
	PeerSessionsByClient(context.Context, string) ([]StoredPeerSession, error)
	ApplyTopologySnapshot(context.Context, ValidatedSnapshot) error
	MarkHubTopologyUnknown(context.Context, string, uint64, time.Time) error
	PresenceProjection(context.Context, string) (string, *cloudpb.PresenceProjection, error)
}

// Service 是 Controller 对 Hub topology snapshot 的唯一校验入口。
type Service struct {
	registry *hubregistry.Registry
	store    Store
}

// New 创建 topology service；registry 与 store 缺失时 fail closed。
func New(registry *hubregistry.Registry, store Store) (*Service, error) {
	if registry == nil || store == nil {
		return nil, fmt.Errorf("topology registry and store are required")
	}
	return &Service{registry: registry, store: store}, nil
}

// PutDeviceOwnership 写入 Controller 持久设备归属，用于后续 snapshot 账号校验。
func (service *Service) PutDeviceOwnership(ctx context.Context, policy *cloudpb.CloudDevicePolicy) error {
	if service == nil || policy == nil || policy.GetDeviceId() == "" || policy.GetAccountId() == "" || policy.GetDeviceKind() == cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_UNSPECIFIED {
		return ErrTopologyRejected
	}
	if policy.GetAuthEpoch() == 0 {
		return ErrTopologyRejected
	}
	return service.store.PutDeviceOwnership(ctx, DeviceOwnership{DeviceID: policy.GetDeviceId(), AccountID: policy.GetAccountId(), Kind: policy.GetDeviceKind(), AuthEpoch: policy.GetAuthEpoch(), Revoked: policy.GetRevoked(), PublicKey: append([]byte(nil), policy.GetPublicKey()...)})
}

// Ingest 校验当前 Hub generation 的完整 topology snapshot 并替换持久投影。
func (service *Service) Ingest(ctx context.Context, snapshot *cloudpb.HubTopologySnapshot, now time.Time) error {
	if service == nil || snapshot == nil || snapshot.GetHubId() == "" || snapshot.GetControlGeneration() == 0 || len(snapshot.GetTopologyDigest()) != sha256.Size || snapshot.GetObservedAtUnixMillis() <= 0 || now.IsZero() {
		return ErrTopologyRejected
	}
	unsigned := proto.Clone(snapshot).(*cloudpb.HubTopologySnapshot)
	unsigned.TopologyDigest = nil
	payload, err := proto.MarshalOptions{Deterministic: true}.Marshal(unsigned)
	if err != nil {
		return ErrTopologyRejected
	}
	digest := sha256.Sum256(payload)
	if !bytes.Equal(digest[:], snapshot.GetTopologyDigest()) {
		return ErrTopologyRejected
	}
	validated := ValidatedSnapshot{HubID: snapshot.GetHubId(), ControlGeneration: snapshot.GetControlGeneration(), TopologyRevision: snapshot.GetTopologyRevision(), Digest: append([]byte(nil), snapshot.GetTopologyDigest()...), ObservedAt: time.UnixMilli(snapshot.GetObservedAtUnixMillis()).UTC()}
	presenceByDaemon := make(map[string]*cloudpb.PresenceProjection, len(snapshot.GetPresences()))
	for _, presence := range snapshot.GetPresences() {
		_, duplicate := presenceByDaemon[presence.GetDaemonDeviceId()]
		if presence == nil || duplicate || presence.GetDaemonDeviceId() == "" || presence.GetControlOwnerHubId() != snapshot.GetHubId() || presence.GetAssignmentEpoch() == 0 || presence.GetPresenceSessionId() == "" || presence.GetAvailability() == cloudpb.Availability_AVAILABILITY_UNSPECIFIED || presence.GetFreshness() == cloudpb.Freshness_FRESHNESS_UNSPECIFIED {
			return ErrTopologyRejected
		}
		assignment, err := service.registry.Assignment(ctx, presence.GetDaemonDeviceId())
		if err != nil || assignment.Value.GetHubId() != snapshot.GetHubId() || assignment.Value.GetAssignmentEpoch() != presence.GetAssignmentEpoch() {
			return ErrTopologyRejected
		}
		ownership, err := service.store.DeviceOwnership(ctx, presence.GetDaemonDeviceId())
		if err != nil || ownership.Kind != cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_DAEMON || ownership.AccountID != assignment.Value.GetAccountId() {
			return ErrTopologyRejected
		}
		presenceByDaemon[presence.GetDaemonDeviceId()] = presence
		validated.Presences = append(validated.Presences, ValidatedPresence{AccountID: assignment.Value.GetAccountId(), Value: proto.Clone(presence).(*cloudpb.PresenceProjection)})
	}
	seenSessions := make(map[string]struct{}, len(snapshot.GetPeerSessions()))
	for _, session := range snapshot.GetPeerSessions() {
		target := session.GetTarget()
		key := fmt.Sprintf("%s\x00%s\x00%d", target.GetDaemonDeviceId(), target.GetManagedSessionId(), target.GetSessionIncarnation())
		_, duplicate := seenSessions[key]
		presence := presenceByDaemon[target.GetDaemonDeviceId()]
		if session == nil || target == nil || duplicate || presence == nil || target.GetDaemonDeviceId() == "" || target.GetManagedSessionId() == "" || target.GetSessionIncarnation() == 0 || target.GetAssignmentEpoch() == 0 || target.GetControlPresenceSessionId() == "" || target.GetDaemonRuntimeGeneration() == "" || target.GetAssignmentEpoch() != presence.GetAssignmentEpoch() || target.GetControlPresenceSessionId() != presence.GetPresenceSessionId() || presence.GetDaemonRuntimeGeneration() != "" && target.GetDaemonRuntimeGeneration() != presence.GetDaemonRuntimeGeneration() || session.GetClientDeviceId() == "" || session.GetControlOwnerHubId() != snapshot.GetHubId() || session.GetObservedDataPath() == cloudpb.ObservedPath_OBSERVED_PATH_UNSPECIFIED || session.GetState() == cloudpb.ManagedPeerSessionState_MANAGED_PEER_SESSION_STATE_UNSPECIFIED || session.GetFreshness() == cloudpb.Freshness_FRESHNESS_UNSPECIFIED {
			return ErrTopologyRejected
		}
		seenSessions[key] = struct{}{}
		assignment, err := service.registry.Assignment(ctx, target.GetDaemonDeviceId())
		if err != nil || assignment.Value.GetHubId() != snapshot.GetHubId() || assignment.Value.GetAssignmentEpoch() != target.GetAssignmentEpoch() {
			return ErrTopologyRejected
		}
		daemonOwner, daemonErr := service.store.DeviceOwnership(ctx, target.GetDaemonDeviceId())
		clientOwner, clientErr := service.store.DeviceOwnership(ctx, session.GetClientDeviceId())
		if daemonErr != nil || clientErr != nil || daemonOwner.Kind != cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_DAEMON || clientOwner.Kind != cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_CLIENT || daemonOwner.AccountID != assignment.Value.GetAccountId() || clientOwner.AccountID != assignment.Value.GetAccountId() {
			return ErrTopologyRejected
		}
		validated.PeerSessions = append(validated.PeerSessions, ValidatedPeerSession{AccountID: assignment.Value.GetAccountId(), Value: proto.Clone(session).(*cloudpb.ManagedPeerSessionProjection)})
	}
	return service.store.ApplyTopologySnapshot(ctx, validated)
}

// MarkHubUnknown 在 control stream 丢失时保留最后观察，但把 availability/freshness 降级。
func (service *Service) MarkHubUnknown(ctx context.Context, hubID string, generation uint64, observedAt time.Time) error {
	if service == nil || hubID == "" || generation == 0 || observedAt.IsZero() {
		return ErrTopologyRejected
	}
	return service.store.MarkHubTopologyUnknown(ctx, hubID, generation, observedAt.UTC())
}

// Presence 返回 Web/Operator 查询使用的账号隔离 Presence projection。
func (service *Service) Presence(ctx context.Context, daemonDeviceID string) (string, *cloudpb.PresenceProjection, error) {
	if service == nil || daemonDeviceID == "" {
		return "", nil, ErrTopologyRejected
	}
	return service.store.PresenceProjection(ctx, daemonDeviceID)
}

// Device 返回 Controller 持久设备 authority；调用方必须继续执行账号隔离。
func (service *Service) Device(ctx context.Context, deviceID string) (DeviceOwnership, error) {
	if service == nil || deviceID == "" {
		return DeviceOwnership{}, ErrTopologyRejected
	}
	return service.store.DeviceOwnership(ctx, deviceID)
}

// DevicePolicies 返回 signed Hub projection 使用的持久设备策略。
func (service *Service) DevicePolicies(ctx context.Context) ([]*cloudpb.CloudDevicePolicy, error) {
	if service == nil {
		return nil, ErrTopologyRejected
	}
	return service.store.DevicePolicies(ctx)
}

// PeerSession 返回账号隔离前的精确 session projection；target 全字段必须匹配。
func (service *Service) PeerSession(ctx context.Context, target *cloudpb.ManagedPeerSessionTarget) (StoredPeerSession, error) {
	if service == nil || target == nil {
		return StoredPeerSession{}, ErrTopologyRejected
	}
	return service.store.PeerSessionProjection(ctx, target)
}

// PeerSessionsForClient 返回当前 topology 中该 client 的全部 active 精确 session。
func (service *Service) PeerSessionsForClient(ctx context.Context, clientDeviceID string) ([]StoredPeerSession, error) {
	if service == nil || clientDeviceID == "" {
		return nil, ErrTopologyRejected
	}
	return service.store.PeerSessionsByClient(ctx, clientDeviceID)
}
