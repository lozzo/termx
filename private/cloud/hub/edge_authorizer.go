package hub

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/lozzow/termx/private/cloud/control-plane/servicecredential"
	"github.com/lozzow/termx/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

var (
	// ErrPolicySnapshot 表示授权投影缺失、回滚、断档或超过最大陈旧窗口。
	ErrPolicySnapshot = errors.New("Hub authorization snapshot unavailable")
	// ErrEdgeAuthorization 表示 edge token、账号 epoch、target ownership 或 revoke 状态拒绝连接。
	ErrEdgeAuthorization = errors.New("Hub edge authorization rejected")
	// ErrTargetUnavailable 表示已认证 client 请求的 daemon 已移除、类型错误或不属于当前账号。
	// 所有 target 失败统一使用该错误，避免跨账号探测设备是否存在。
	ErrTargetUnavailable = errors.New("Hub target device unavailable")
)

// DeviceAuthorization 是 Control Plane 同步给 Hub 的最小 daemon 授权投影。
// PublicKey 供后续 fresh presence proof 使用；Revoked 优先于账号 ownership 和订阅 allow。
type DeviceAuthorization struct {
	DeviceID    string
	AccountID   string
	Kind        string
	DisplayName string
	Platform    string
	PublicKey   []byte
	Revoked     bool
}

// AccountAuthorization 是 Hub 判断账号 edge token epoch 和 managed service 能力的本地投影。
// AuthEpoch 变化会使旧 token 立即失效；Capability 直接复用 PlanCatalog/Entitlement 的 generated schema。
type AccountAuthorization struct {
	AccountID                     string
	AuthEpoch                     uint64
	Revoked                       bool
	EntitlementStatus             cloudpb.EntitlementStatus
	EntitlementEffectiveUntilUnix int64
	Capability                    *cloudpb.PlanCapability
}

// AuthorizationSnapshot 是 Hub 原子应用的、带严格单调 revision 的授权快照。
// GeneratedAt 用于 max staleness；Devices/Accounts 在应用时深拷贝，caller 后续修改不影响运行时真值。
type AuthorizationSnapshot struct {
	Revision    uint64
	GeneratedAt time.Time
	Accounts    []AccountAuthorization
	Devices     []DeviceAuthorization
}

// EdgeAuthorizerConfig 固定 Hub identity、edge token issuer、公钥和授权快照最大陈旧窗口。
// 请求路径没有 Control Plane callback；任何 cache miss 都直接 fail closed。
type EdgeAuthorizerConfig struct {
	HubID        string
	Issuer       string
	KeyRing      *servicecredential.KeyRing
	Clock        Clock
	MaxStaleness time.Duration
}

// EdgeAuthorizer 是 Hub managed direct 授权决策的内存 owner。
// 它只接受完整 snapshot 的原子替换；生产持久快照/WAL 由外层同步组件负责验证后恢复。
type EdgeAuthorizer struct {
	mu           sync.RWMutex
	hubID        string
	issuer       string
	keyRing      *servicecredential.KeyRing
	clock        Clock
	maxStaleness time.Duration
	revision     uint64
	generatedAt  time.Time
	accounts     map[string]AccountAuthorization
	devices      map[string]DeviceAuthorization
}

// Revision 返回当前已验签并发布的 policy revision。
// 该值只用于 Control Plane 重启后继续生成严格递增快照，不暴露账号或设备内容。
func (authorizer *EdgeAuthorizer) Revision() uint64 {
	if authorizer == nil {
		return 0
	}
	authorizer.mu.RLock()
	defer authorizer.mu.RUnlock()
	return authorizer.revision
}

// NewEdgeAuthorizer 创建没有授权快照的 Hub authorizer。
// 在首次成功 ApplySnapshot 前所有连接均 fail closed。
func NewEdgeAuthorizer(config EdgeAuthorizerConfig) (*EdgeAuthorizer, error) {
	if config.HubID == "" || config.Issuer == "" || config.KeyRing == nil || config.MaxStaleness <= 0 {
		return nil, fmt.Errorf("invalid Hub edge authorizer configuration")
	}
	if config.Clock == nil {
		config.Clock = realClock{}
	}
	return &EdgeAuthorizer{hubID: config.HubID, issuer: config.Issuer, keyRing: config.KeyRing, clock: config.Clock, maxStaleness: config.MaxStaleness}, nil
}

// ApplySignedSnapshot 验证 Control Plane 签名快照并原子发布纯内存投影。
// Edge 重启不得恢复该 token；新的 control generation 必须重新发送 full projection。
func (authorizer *EdgeAuthorizer) ApplySignedSnapshot(encoded []byte) error {
	now := authorizer.clock.Now().UTC()
	claims, err := servicecredential.VerifyEdgePolicy(authorizer.keyRing, encoded, authorizer.issuer, authorizer.hubID, now)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrPolicySnapshot, err)
	}
	snapshot := snapshotFromPolicyClaims(claims)
	if err := authorizer.validateNextSnapshot(snapshot); err != nil {
		return err
	}
	return authorizer.ApplySnapshot(snapshot)
}

func (authorizer *EdgeAuthorizer) validateNextSnapshot(snapshot AuthorizationSnapshot) error {
	authorizer.mu.RLock()
	defer authorizer.mu.RUnlock()
	if snapshot.Revision == 0 || snapshot.Revision <= authorizer.revision {
		return ErrPolicySnapshot
	}
	return nil
}

func snapshotFromPolicyClaims(claims servicecredential.EdgePolicyClaims) AuthorizationSnapshot {
	accounts := make([]AccountAuthorization, 0, len(claims.Accounts))
	for _, account := range claims.Accounts {
		accounts = append(accounts, AccountAuthorization{AccountID: account.AccountID, AuthEpoch: account.AuthEpoch, Revoked: account.Revoked, EntitlementStatus: account.EntitlementStatus, EntitlementEffectiveUntilUnix: account.EntitlementEffectiveUntilUnix, Capability: cloneHubPlanCapability(account.Capability)})
	}
	devices := make([]DeviceAuthorization, 0, len(claims.Devices))
	for _, device := range claims.Devices {
		devices = append(devices, DeviceAuthorization{DeviceID: device.DeviceID, AccountID: device.AccountID, Kind: device.Kind, DisplayName: device.DisplayName, Platform: device.Platform, PublicKey: append([]byte(nil), device.PublicKey...), Revoked: device.Revoked})
	}
	return AuthorizationSnapshot{Revision: claims.Revision, GeneratedAt: time.Unix(claims.GeneratedUnix, 0).UTC(), Accounts: accounts, Devices: devices}
}

// RelayBudget 是 Hub 从签名账号投影读取的 single Relay 区域预算。
// 它只限制付费 Relay 服务，不扩大 daemon terminal capability。
type RelayBudget struct {
	MaxLeaseDuration time.Duration
	MaxBytes         uint64
	MaxBitrateKbps   uint32
	MaxConcurrency   uint32
}

// RelayBudget 返回账号当前签名投影中的 single Relay 预算。
// 快照陈旧、账号撤销、未订阅或字段不完整时 fail closed。
func (authorizer *EdgeAuthorizer) RelayBudget(accountID string) (RelayBudget, error) {
	now := authorizer.clock.Now().UTC()
	authorizer.mu.RLock()
	defer authorizer.mu.RUnlock()
	account, ok := authorizer.accounts[accountID]
	if authorizer.revision == 0 || now.Sub(authorizer.generatedAt) > authorizer.maxStaleness || !ok || !account.activeAt(now) || account.Capability == nil || !account.Capability.GetStandardRelayEnabled() {
		return RelayBudget{}, ErrEdgeAuthorization
	}
	relay := account.Capability.GetRelay()
	if relay == nil || relay.GetMaxLeaseSeconds() == 0 || relay.GetMaxBytesPerLease() == 0 || relay.GetMaxBitrateKbps() == 0 || relay.GetMaxConcurrency() == 0 {
		return RelayBudget{}, ErrEdgeAuthorization
	}
	return RelayBudget{MaxLeaseDuration: time.Duration(relay.GetMaxLeaseSeconds()) * time.Second, MaxBytes: relay.GetMaxBytesPerLease(), MaxBitrateKbps: relay.GetMaxBitrateKbps(), MaxConcurrency: relay.GetMaxConcurrency()}, nil
}

// ApplySnapshot 原子替换完整授权投影。
// revision 必须严格递增；rollback、重复 revision、未来时间和重复主体都会拒绝且保留旧快照。
func (authorizer *EdgeAuthorizer) ApplySnapshot(snapshot AuthorizationSnapshot) error {
	now := authorizer.clock.Now().UTC()
	if snapshot.Revision == 0 || snapshot.GeneratedAt.IsZero() || snapshot.GeneratedAt.After(now) {
		return ErrPolicySnapshot
	}
	accounts := make(map[string]AccountAuthorization, len(snapshot.Accounts))
	for _, account := range snapshot.Accounts {
		if account.AccountID == "" || account.AuthEpoch == 0 || account.Capability == nil {
			return ErrPolicySnapshot
		}
		if _, exists := accounts[account.AccountID]; exists {
			return ErrPolicySnapshot
		}
		account.Capability = cloneHubPlanCapability(account.Capability)
		accounts[account.AccountID] = account
	}
	devices := make(map[string]DeviceAuthorization, len(snapshot.Devices))
	for _, device := range snapshot.Devices {
		if device.DeviceID == "" || device.AccountID == "" || device.DisplayName == "" || device.Kind != "client" && device.Kind != "daemon" {
			return ErrPolicySnapshot
		}
		if _, exists := devices[device.DeviceID]; exists {
			return ErrPolicySnapshot
		}
		device.PublicKey = append([]byte(nil), device.PublicKey...)
		devices[device.DeviceID] = device
	}
	authorizer.mu.Lock()
	defer authorizer.mu.Unlock()
	if snapshot.Revision <= authorizer.revision {
		return ErrPolicySnapshot
	}
	authorizer.revision, authorizer.generatedAt = snapshot.Revision, snapshot.GeneratedAt.UTC()
	authorizer.accounts, authorizer.devices = accounts, devices
	return nil
}

// AuthorizeDirect 离线验证 client edge token，并与本地账号和 target device 投影取交集。
// 返回 claims 只供 Hub 创建短期 EdgeManagedSession；任何缺失、撤销、epoch 不匹配或陈旧快照都 fail closed。
func (authorizer *EdgeAuthorizer) AuthorizeDirect(token []byte, accountID, clientDeviceID, targetDeviceID string) (servicecredential.EdgeAccessClaims, error) {
	now := authorizer.clock.Now().UTC()
	claims, err := servicecredential.VerifyEdgeAccess(authorizer.keyRing, token, servicecredential.EdgeAccessExpectation{Issuer: authorizer.issuer, AudienceHubID: authorizer.hubID, AccountID: accountID, ClientDeviceID: clientDeviceID, PrincipalKind: servicecredential.EdgePrincipalClient}, now)
	if err != nil {
		return servicecredential.EdgeAccessClaims{}, fmt.Errorf("%w: %v", ErrEdgeAuthorization, err)
	}
	authorizer.mu.RLock()
	defer authorizer.mu.RUnlock()
	if authorizer.revision == 0 || now.Sub(authorizer.generatedAt) > authorizer.maxStaleness {
		return servicecredential.EdgeAccessClaims{}, ErrPolicySnapshot
	}
	account, accountOK := authorizer.accounts[accountID]
	client, clientOK := authorizer.devices[clientDeviceID]
	device, deviceOK := authorizer.devices[targetDeviceID]
	if !accountOK || !clientOK || !account.activeAt(now) || account.Capability == nil || !account.Capability.GetManagedP2PEnabled() || account.AuthEpoch != claims.AuthEpoch || client.Revoked || client.AccountID != accountID || client.Kind != "client" {
		return servicecredential.EdgeAccessClaims{}, ErrEdgeAuthorization
	}
	if !deviceOK || device.Revoked || device.AccountID != accountID || device.Kind != "daemon" {
		return servicecredential.EdgeAccessClaims{}, ErrTargetUnavailable
	}
	return claims, nil
}

func (account AccountAuthorization) activeAt(now time.Time) bool {
	return !account.Revoked && account.EntitlementStatus == cloudpb.EntitlementStatus_ENTITLEMENT_STATUS_ACTIVE && now.Unix() < account.EntitlementEffectiveUntilUnix
}

func cloneHubPlanCapability(capability *cloudpb.PlanCapability) *cloudpb.PlanCapability {
	if capability == nil {
		return nil
	}
	return proto.Clone(capability).(*cloudpb.PlanCapability)
}

// AuthorizeClient 验证当前连接发起端自身仍存在于 Hub 签名内存投影且未撤销。
// 目录、resolve、signaling 和 Relay 都应先经过该边界，不能只验证账号或目标 daemon。
func (authorizer *EdgeAuthorizer) AuthorizeClient(token []byte, accountID, clientDeviceID string) (servicecredential.EdgeAccessClaims, error) {
	now := authorizer.clock.Now().UTC()
	claims, err := servicecredential.VerifyEdgeAccess(authorizer.keyRing, token, servicecredential.EdgeAccessExpectation{Issuer: authorizer.issuer, AudienceHubID: authorizer.hubID, AccountID: accountID, ClientDeviceID: clientDeviceID, PrincipalKind: servicecredential.EdgePrincipalClient}, now)
	if err != nil {
		return servicecredential.EdgeAccessClaims{}, fmt.Errorf("%w: %v", ErrEdgeAuthorization, err)
	}
	authorizer.mu.RLock()
	defer authorizer.mu.RUnlock()
	account, accountOK := authorizer.accounts[accountID]
	client, clientOK := authorizer.devices[clientDeviceID]
	if authorizer.revision == 0 || now.Sub(authorizer.generatedAt) > authorizer.maxStaleness || !accountOK || !clientOK || account.Revoked || account.AuthEpoch != claims.AuthEpoch || client.Revoked || client.AccountID != accountID || client.Kind != "client" {
		return servicecredential.EdgeAccessClaims{}, ErrEdgeAuthorization
	}
	return claims, nil
}

// AccountDevices 返回账号设备投影的深拷贝；调用前必须已经通过 AuthorizeClient。
// 返回值不含 session、terminal inventory 或 CapabilityGrant，Presence 由 Hub Service 另行叠加。
func (authorizer *EdgeAuthorizer) AccountDevices(accountID string) []DeviceAuthorization {
	authorizer.mu.RLock()
	defer authorizer.mu.RUnlock()
	devices := make([]DeviceAuthorization, 0)
	for _, device := range authorizer.devices {
		if device.AccountID != accountID {
			continue
		}
		device.PublicKey = append([]byte(nil), device.PublicKey...)
		devices = append(devices, device)
	}
	return devices
}

// AuthorizeDaemon 离线验证 daemon edge token、账号 epoch 与本地 device ownership/revoke 投影。
// 它只允许完成已由该 device active presence 接收的 signaling，不授予 client offer 或 terminal capability。
func (authorizer *EdgeAuthorizer) AuthorizeDaemon(token []byte, accountID, deviceID string) (servicecredential.EdgeAccessClaims, error) {
	claims, _, err := authorizer.AuthorizeDaemonDevice(token, accountID, deviceID)
	return claims, err
}

// AuthorizeDaemonDevice 离线验证 daemon edge token，并返回与 token 同 revision 的设备公钥投影。
// Hub Presence 使用该公钥验证 fresh DeviceProof；返回值不包含 private key、terminal 或 capability。
func (authorizer *EdgeAuthorizer) AuthorizeDaemonDevice(token []byte, accountID, deviceID string) (servicecredential.EdgeAccessClaims, DeviceAuthorization, error) {
	now := authorizer.clock.Now().UTC()
	claims, err := servicecredential.VerifyEdgeAccess(authorizer.keyRing, token, servicecredential.EdgeAccessExpectation{Issuer: authorizer.issuer, AudienceHubID: authorizer.hubID, AccountID: accountID, ClientDeviceID: deviceID, PrincipalKind: servicecredential.EdgePrincipalDaemon}, now)
	if err != nil {
		return servicecredential.EdgeAccessClaims{}, DeviceAuthorization{}, fmt.Errorf("%w: %v", ErrEdgeAuthorization, err)
	}
	authorizer.mu.RLock()
	defer authorizer.mu.RUnlock()
	if authorizer.revision == 0 || now.Sub(authorizer.generatedAt) > authorizer.maxStaleness {
		return servicecredential.EdgeAccessClaims{}, DeviceAuthorization{}, ErrPolicySnapshot
	}
	account, accountOK := authorizer.accounts[accountID]
	device, deviceOK := authorizer.devices[deviceID]
	if !accountOK || !deviceOK || account.Revoked || account.AuthEpoch != claims.AuthEpoch || device.Revoked || device.AccountID != accountID || device.Kind != "daemon" {
		return servicecredential.EdgeAccessClaims{}, DeviceAuthorization{}, ErrEdgeAuthorization
	}
	device.PublicKey = append([]byte(nil), device.PublicKey...)
	return claims, device, nil
}
