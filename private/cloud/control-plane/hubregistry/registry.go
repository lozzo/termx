// Package hubregistry 拥有 Cloud Edge Hub deployment、control generation 与 daemon assignment lease 真值。
//
// Hub 只能消费这里签发的 generation 和 assignment；Web、Hub runtime 与 composition root
// 不得各自维护“当前 Hub”或“当前 epoch”的第二份状态。
package hubregistry

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/muxvia/muxvia/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

var (
	// ErrDeploymentNotFound 表示请求的 Hub deployment 不存在或已禁用。
	ErrDeploymentNotFound = errors.New("hub deployment not found")
	// ErrDeploymentIdentity 表示 control identity 与持久 registry 不匹配。
	ErrDeploymentIdentity = errors.New("hub deployment identity mismatch")
	// ErrStaleControlGeneration 表示 handler 使用的 generation 已被后续连接替换。
	ErrStaleControlGeneration = errors.New("hub control generation is stale")
	// ErrAssignmentFenceRequired 表示旧 Hub lease 尚未 fence 或过期，不能激活目标 Hub。
	ErrAssignmentFenceRequired = errors.New("hub assignment fence is required")
	// ErrAssignmentConflict 表示 assignment epoch、owner 或时间窗口与当前真值冲突。
	ErrAssignmentConflict = errors.New("hub assignment conflicts with current state")
)

// Deployment 是 Controller 持久保存的 Hub control identity。
// PublicKey 只验证 Hub challenge proof；Relay 使用独立 registry 与 key role。
type Deployment struct {
	Metadata               *cloudpb.EdgeDeploymentMetadata
	ControlPublicKey       ed25519.PublicKey
	RelayControlPublicKey  ed25519.PublicKey
	Enabled                bool
	ControlGeneration      uint64
	RelayControlGeneration uint64
	UpdatedAt              time.Time
}

// Assignment 是 daemon 到 owning Hub 的持久 lease 与 fencing 状态。
type Assignment struct {
	Value          *cloudpb.HubAssignment
	FenceSatisfied bool
	PreviousHubID  string
	PreviousEpoch  uint64
	UpdatedAt      time.Time
}

// Store 是 hubregistry 的持久事务边界。
// AdvanceControlGeneration 与 MoveAssignment 必须在各自单个事务内完成 CAS。
type Store interface {
	PutDeployment(context.Context, Deployment) error
	Deployment(context.Context, string) (Deployment, error)
	DeploymentByRelay(context.Context, string) (Deployment, error)
	Deployments(context.Context) ([]Deployment, error)
	AdvanceControlGeneration(context.Context, string, string, string, time.Time) (Deployment, error)
	ControlGenerationCurrent(context.Context, string, uint64) (bool, error)
	AdvanceRelayControlGeneration(context.Context, string, string, string, time.Time) (Deployment, error)
	RelayControlGenerationCurrent(context.Context, string, uint64) (bool, error)
	MoveAssignment(context.Context, *cloudpb.HubAssignment, time.Time) (Assignment, error)
	FenceAssignment(context.Context, string, string, uint64, time.Time) (Assignment, error)
	Assignment(context.Context, string) (Assignment, error)
	AssignmentsForHub(context.Context, string, time.Time) ([]Assignment, error)
}

// Registry 执行 deployment identity 与 assignment 业务约束；SQLite 只实现 Store 事务。
type Registry struct{ store Store }

// New 创建 Hub registry application service。
func New(store Store) (*Registry, error) {
	if store == nil {
		return nil, fmt.Errorf("hub registry store is required")
	}
	return &Registry{store: store}, nil
}

// RegisterDeployment 写入或更新一个显式部署记录。
// metadata fingerprint 必须与 public key 一致，防止配置把 Hub ID 绑定到错误 key。
func (registry *Registry) RegisterDeployment(ctx context.Context, deployment Deployment) error {
	if deployment.Metadata == nil || deployment.Metadata.GetHubId() == "" || deployment.Metadata.GetEdgeDeploymentId() == "" || deployment.Metadata.GetRegion() == "" || len(deployment.ControlPublicKey) != ed25519.PublicKeySize || len(deployment.RelayControlPublicKey) != ed25519.PublicKeySize {
		return ErrDeploymentIdentity
	}
	if deployment.Metadata.GetHubControlIdentityFingerprint() != IdentityFingerprint(deployment.ControlPublicKey) {
		return ErrDeploymentIdentity
	}
	if deployment.Metadata.GetRelayControlIdentityFingerprint() != IdentityFingerprint(deployment.RelayControlPublicKey) {
		return ErrDeploymentIdentity
	}
	deployment.Metadata = proto.Clone(deployment.Metadata).(*cloudpb.EdgeDeploymentMetadata)
	deployment.ControlPublicKey = append(ed25519.PublicKey(nil), deployment.ControlPublicKey...)
	deployment.RelayControlPublicKey = append(ed25519.PublicKey(nil), deployment.RelayControlPublicKey...)
	return registry.store.PutDeployment(ctx, deployment)
}

// Deployment 返回 Hub control handshake 使用的持久 identity 深拷贝。
func (registry *Registry) Deployment(ctx context.Context, hubID string) (Deployment, error) {
	return registry.store.Deployment(ctx, hubID)
}

// DeploymentByRelay 返回 Relay control handshake 使用的同一 deployment 记录。
func (registry *Registry) DeploymentByRelay(ctx context.Context, relayID string) (Deployment, error) {
	return registry.store.DeploymentByRelay(ctx, relayID)
}

// Deployments 返回 operator fleet 使用的持久 deployment 快照。
func (registry *Registry) Deployments(ctx context.Context) ([]Deployment, error) {
	return registry.store.Deployments(ctx)
}

// AttachHub 验证 HubHello metadata 后原子签发唯一递增 control generation。
func (registry *Registry) AttachHub(ctx context.Context, hello *cloudpb.HubHello, now time.Time) (Deployment, error) {
	if hello == nil || hello.GetDeployment() == nil || now.IsZero() {
		return Deployment{}, ErrDeploymentIdentity
	}
	metadata := hello.GetDeployment()
	return registry.store.AdvanceControlGeneration(ctx, metadata.GetHubId(), metadata.GetEdgeDeploymentId(), metadata.GetHubControlIdentityFingerprint(), now.UTC())
}

// RequireCurrentGeneration 在每个旧 handler 处理 envelope 前重新检查 CAS 真值。
func (registry *Registry) RequireCurrentGeneration(ctx context.Context, hubID string, generation uint64) error {
	current, err := registry.store.ControlGenerationCurrent(ctx, hubID, generation)
	if err != nil {
		return err
	}
	if !current {
		return ErrStaleControlGeneration
	}
	return nil
}

// AttachRelay 验证 RelayHello metadata 后原子签发独立于 Hub 的递增 control generation。
func (registry *Registry) AttachRelay(ctx context.Context, hello *cloudpb.RelayHello, now time.Time) (Deployment, error) {
	if hello == nil || hello.GetDeployment() == nil || now.IsZero() {
		return Deployment{}, ErrDeploymentIdentity
	}
	metadata := hello.GetDeployment()
	return registry.store.AdvanceRelayControlGeneration(ctx, metadata.GetRelayId(), metadata.GetEdgeDeploymentId(), metadata.GetRelayControlIdentityFingerprint(), now.UTC())
}

// RequireCurrentRelayGeneration 拒绝被新 Relay attachment 替换的旧 generation。
func (registry *Registry) RequireCurrentRelayGeneration(ctx context.Context, relayID string, generation uint64) error {
	current, err := registry.store.RelayControlGenerationCurrent(ctx, relayID, generation)
	if err != nil {
		return err
	}
	if !current {
		return ErrStaleControlGeneration
	}
	return nil
}

// Assign 激活或续租 daemon assignment；跨 Hub 时旧 lease 必须已 fence 或过期。
func (registry *Registry) Assign(ctx context.Context, value *cloudpb.HubAssignment, now time.Time) (Assignment, error) {
	if value == nil || value.GetDaemonDeviceId() == "" || value.GetAccountId() == "" || value.GetHubId() == "" || value.GetAssignmentEpoch() == 0 || value.GetExpiresAtUnixMillis() <= value.GetNotBeforeUnixMillis() || now.IsZero() {
		return Assignment{}, ErrAssignmentConflict
	}
	return registry.store.MoveAssignment(ctx, proto.Clone(value).(*cloudpb.HubAssignment), now.UTC())
}

// Fence 确认旧 Hub 已关闭精确 assignment epoch；错误 Hub 或旧 epoch 不产生副作用。
func (registry *Registry) Fence(ctx context.Context, daemonDeviceID, sourceHubID string, sourceEpoch uint64, now time.Time) (Assignment, error) {
	if daemonDeviceID == "" || sourceHubID == "" || sourceEpoch == 0 || now.IsZero() {
		return Assignment{}, ErrAssignmentConflict
	}
	return registry.store.FenceAssignment(ctx, daemonDeviceID, sourceHubID, sourceEpoch, now.UTC())
}

// Assignment 返回精确 daemon 的持久 owner 投影。
func (registry *Registry) Assignment(ctx context.Context, daemonDeviceID string) (Assignment, error) {
	return registry.store.Assignment(ctx, daemonDeviceID)
}

// AssignmentsForHub 返回当前时刻属于 Hub 的未过期 assignment，用于生成 per-Hub projection。
func (registry *Registry) AssignmentsForHub(ctx context.Context, hubID string, now time.Time) ([]Assignment, error) {
	return registry.store.AssignmentsForHub(ctx, hubID, now.UTC())
}

// IdentityFingerprint 返回 deployment registry 使用的稳定 Ed25519 public key 摘要。
func IdentityFingerprint(publicKey ed25519.PublicKey) string {
	digest := sha256.Sum256(publicKey)
	return base64.RawURLEncoding.EncodeToString(digest[:])
}
