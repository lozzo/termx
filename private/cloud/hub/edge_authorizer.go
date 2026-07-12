package hub

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/lozzow/termx/private/cloud/control-plane/servicecredential"
)

var (
	// ErrPolicySnapshot 表示授权投影缺失、回滚、断档或超过最大陈旧窗口。
	ErrPolicySnapshot = errors.New("Hub authorization snapshot unavailable")
	// ErrEdgeAuthorization 表示 edge token、账号 epoch、target ownership 或 revoke 状态拒绝连接。
	ErrEdgeAuthorization = errors.New("Hub edge authorization rejected")
)

// DeviceAuthorization 是 Control Plane 同步给 Hub 的最小 daemon 授权投影。
// PublicKey 供后续 fresh presence proof 使用；Revoked 优先于账号 ownership 和订阅 allow。
type DeviceAuthorization struct {
	DeviceID  string
	AccountID string
	PublicKey []byte
	Revoked   bool
}

// AccountAuthorization 是 Hub 判断账号 edge token epoch 和 managed direct 能力的本地投影。
// AuthEpoch 变化会使旧 token 立即失效；ManagedDirectEnabled 只控制托管 direct，不扩大 daemon capability。
type AccountAuthorization struct {
	AccountID            string
	AuthEpoch            uint64
	ManagedDirectEnabled bool
	Revoked              bool
	StandardRelayEnabled bool
	RelayMaxLeaseSeconds uint32
	RelayMaxBytes        uint64
	RelayMaxBitrateKbps  uint32
	RelayMaxConcurrency  uint32
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
	// SnapshotStore 可选持久化已验签快照；恢复时仍会重新验签。
	SnapshotStore EdgeSnapshotStore
}

// EdgeAuthorizer 是 Hub managed direct 授权决策的内存 owner。
// 它只接受完整 snapshot 的原子替换；生产持久快照/WAL 由外层同步组件负责验证后恢复。
type EdgeAuthorizer struct {
	mu            sync.RWMutex
	hubID         string
	issuer        string
	keyRing       *servicecredential.KeyRing
	clock         Clock
	maxStaleness  time.Duration
	snapshotStore EdgeSnapshotStore
	revision      uint64
	generatedAt   time.Time
	accounts      map[string]AccountAuthorization
	devices       map[string]DeviceAuthorization
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
	return &EdgeAuthorizer{hubID: config.HubID, issuer: config.Issuer, keyRing: config.KeyRing, clock: config.Clock, maxStaleness: config.MaxStaleness, snapshotStore: config.SnapshotStore}, nil
}

// ApplySignedSnapshot 验证 Control Plane 签名快照、持久化原始 token，再原子发布内存投影。
// 持久化失败时不发布新 revision，避免重启后恢复到已经落后的授权状态。
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
	if authorizer.snapshotStore != nil {
		if err := authorizer.snapshotStore.Save(append([]byte(nil), encoded...)); err != nil {
			return fmt.Errorf("%w: persist snapshot: %v", ErrPolicySnapshot, err)
		}
	}
	return authorizer.ApplySnapshot(snapshot)
}

// RestoreSignedSnapshot 从 store 读取原始快照并重新验证签名、audience、expiry 和 schema 后发布。
// 缺少 store、磁盘损坏或快照过期均 fail closed，不恢复 presence 或 signaling 状态。
func (authorizer *EdgeAuthorizer) RestoreSignedSnapshot() error {
	if authorizer.snapshotStore == nil {
		return ErrPolicySnapshot
	}
	encoded, err := authorizer.snapshotStore.Load()
	if err != nil {
		return fmt.Errorf("%w: load snapshot: %v", ErrPolicySnapshot, err)
	}
	claims, err := servicecredential.VerifyEdgePolicy(authorizer.keyRing, encoded, authorizer.issuer, authorizer.hubID, authorizer.clock.Now().UTC())
	clear(encoded)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrPolicySnapshot, err)
	}
	return authorizer.ApplySnapshot(snapshotFromPolicyClaims(claims))
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
		accounts = append(accounts, AccountAuthorization{AccountID: account.AccountID, AuthEpoch: account.AuthEpoch, ManagedDirectEnabled: account.ManagedDirectEnabled, Revoked: account.Revoked, StandardRelayEnabled: account.StandardRelayEnabled, RelayMaxLeaseSeconds: account.RelayMaxLeaseSeconds, RelayMaxBytes: account.RelayMaxBytes, RelayMaxBitrateKbps: account.RelayMaxBitrateKbps, RelayMaxConcurrency: account.RelayMaxConcurrency})
	}
	devices := make([]DeviceAuthorization, 0, len(claims.Devices))
	for _, device := range claims.Devices {
		devices = append(devices, DeviceAuthorization{DeviceID: device.DeviceID, AccountID: device.AccountID, PublicKey: append([]byte(nil), device.PublicKey...), Revoked: device.Revoked})
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
	if authorizer.revision == 0 || now.Sub(authorizer.generatedAt) > authorizer.maxStaleness || !ok || account.Revoked || !account.StandardRelayEnabled || account.RelayMaxLeaseSeconds == 0 || account.RelayMaxBytes == 0 || account.RelayMaxBitrateKbps == 0 || account.RelayMaxConcurrency == 0 {
		return RelayBudget{}, ErrEdgeAuthorization
	}
	return RelayBudget{MaxLeaseDuration: time.Duration(account.RelayMaxLeaseSeconds) * time.Second, MaxBytes: account.RelayMaxBytes, MaxBitrateKbps: account.RelayMaxBitrateKbps, MaxConcurrency: account.RelayMaxConcurrency}, nil
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
		if account.AccountID == "" || account.AuthEpoch == 0 {
			return ErrPolicySnapshot
		}
		if _, exists := accounts[account.AccountID]; exists {
			return ErrPolicySnapshot
		}
		accounts[account.AccountID] = account
	}
	devices := make(map[string]DeviceAuthorization, len(snapshot.Devices))
	for _, device := range snapshot.Devices {
		if device.DeviceID == "" || device.AccountID == "" {
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
	device, deviceOK := authorizer.devices[targetDeviceID]
	if !accountOK || !deviceOK || account.Revoked || !account.ManagedDirectEnabled || account.AuthEpoch != claims.AuthEpoch || device.Revoked || device.AccountID != accountID {
		return servicecredential.EdgeAccessClaims{}, ErrEdgeAuthorization
	}
	return claims, nil
}

// AuthorizeDaemon 离线验证 daemon edge token、账号 epoch 与本地 device ownership/revoke 投影。
// 它只允许完成已由该 device active presence 接收的 signaling，不授予 client offer 或 terminal capability。
func (authorizer *EdgeAuthorizer) AuthorizeDaemon(token []byte, accountID, deviceID string) (servicecredential.EdgeAccessClaims, error) {
	now := authorizer.clock.Now().UTC()
	claims, err := servicecredential.VerifyEdgeAccess(authorizer.keyRing, token, servicecredential.EdgeAccessExpectation{Issuer: authorizer.issuer, AudienceHubID: authorizer.hubID, AccountID: accountID, ClientDeviceID: deviceID, PrincipalKind: servicecredential.EdgePrincipalDaemon}, now)
	if err != nil {
		return servicecredential.EdgeAccessClaims{}, fmt.Errorf("%w: %v", ErrEdgeAuthorization, err)
	}
	authorizer.mu.RLock()
	defer authorizer.mu.RUnlock()
	if authorizer.revision == 0 || now.Sub(authorizer.generatedAt) > authorizer.maxStaleness {
		return servicecredential.EdgeAccessClaims{}, ErrPolicySnapshot
	}
	account, accountOK := authorizer.accounts[accountID]
	device, deviceOK := authorizer.devices[deviceID]
	if !accountOK || !deviceOK || account.Revoked || account.AuthEpoch != claims.AuthEpoch || device.Revoked || device.AccountID != accountID {
		return servicecredential.EdgeAccessClaims{}, ErrEdgeAuthorization
	}
	return claims, nil
}
