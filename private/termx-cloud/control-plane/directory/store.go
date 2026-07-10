// Package directory 实现账号、设备所有权与 managed session 目录。
package directory

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/lozzow/termx/private/termx-cloud/control-plane/domain"
)

var (
	// ErrNotFound 表示请求的 Control Plane 目录实体不存在。
	ErrNotFound = errors.New("control plane directory entity not found")
	// ErrOwnership 表示设备或 managed session 不属于请求账号。
	ErrOwnership = errors.New("control plane device ownership mismatch")
	// ErrConflict 表示相同 ID 已被不同实体占用。
	ErrConflict = errors.New("control plane directory entity conflict")
)

// Store 是 Control Plane 目录的并发安全内存实现。
// 它用于领域 harness 和后续持久化 adapter 的 contract 基线；生产数据库必须保持相同所有权检查。
type Store struct {
	mu            sync.RWMutex
	organizations map[string]domain.Organization
	accounts      map[string]domain.Account
	users         map[string]domain.User
	devices       map[string]domain.DeviceRegistration
	sessions      map[string]domain.ManagedSession
}

// NewStore 创建空目录。
// 返回值不启动后台任务，也不存在 heartbeat 驱动的设备踢除路径。
func NewStore() *Store {
	return &Store{
		organizations: make(map[string]domain.Organization),
		accounts:      make(map[string]domain.Account),
		users:         make(map[string]domain.User),
		devices:       make(map[string]domain.DeviceRegistration),
		sessions:      make(map[string]domain.ManagedSession),
	}
}

// PutOrganization 写入新的团队治理与账单归属记录。
// 相同 ID 的不同内容会被拒绝，避免账号在未审计情况下切换 organization 真值。
func (store *Store) PutOrganization(organization domain.Organization) error {
	if organization.ID == "" || organization.CreatedAt.IsZero() {
		return fmt.Errorf("invalid organization: %w", ErrConflict)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if current, ok := store.organizations[organization.ID]; ok && current != organization {
		return ErrConflict
	}
	store.organizations[organization.ID] = organization
	return nil
}

// PutAccount 写入新的账号记录。
// 相同 ID 的不同内容会被拒绝，避免隐式覆盖计费主体。
func (store *Store) PutAccount(account domain.Account) error {
	if account.ID == "" || account.CreatedAt.IsZero() {
		return fmt.Errorf("invalid account: %w", ErrConflict)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if account.OrganizationID != "" {
		if _, ok := store.organizations[account.OrganizationID]; !ok {
			return ErrNotFound
		}
	}
	if current, ok := store.accounts[account.ID]; ok && current != account {
		return ErrConflict
	}
	store.accounts[account.ID] = account
	return nil
}

// PutUser 写入账号下的用户。
// 用户账号必须已经存在；该关系只用于 Control Plane 管理权限。
func (store *Store) PutUser(user domain.User) error {
	if user.ID == "" || user.AccountID == "" || user.CreatedAt.IsZero() {
		return fmt.Errorf("invalid user: %w", ErrConflict)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, ok := store.accounts[user.AccountID]; !ok {
		return ErrNotFound
	}
	if current, ok := store.users[user.ID]; ok && current != user {
		return ErrConflict
	}
	store.users[user.ID] = user
	return nil
}

// RegisterDevice 建立账号与设备的所有权记录。
// PublicKey 会被复制，调用方后续修改输入切片不会改变目录真值。
func (store *Store) RegisterDevice(device domain.DeviceRegistration) error {
	if device.ID == "" || device.AccountID == "" || device.OwnerUserID == "" || device.RegisteredAt.IsZero() || len(device.PublicKey) != ed25519.PublicKeySize || device.Fingerprint == "" {
		return fmt.Errorf("invalid device registration: %w", ErrConflict)
	}
	if device.RevokedAt != nil && device.RevokedAt.Before(device.RegisteredAt) {
		return fmt.Errorf("invalid device revocation time: %w", ErrConflict)
	}
	if device.Kind != domain.DeviceKindClient && device.Kind != domain.DeviceKindDaemon {
		return fmt.Errorf("invalid device kind %q: %w", device.Kind, ErrConflict)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	owner, ok := store.users[device.OwnerUserID]
	if !ok || owner.AccountID != device.AccountID {
		return ErrOwnership
	}
	device.PublicKey = append([]byte(nil), device.PublicKey...)
	if current, ok := store.devices[device.ID]; ok && !sameDevice(current, device) {
		return ErrConflict
	}
	store.devices[device.ID] = device
	return nil
}

// Device 返回账号拥有的设备记录。
// 查询必须同时提供 AccountID，防止仅凭可枚举 DeviceID 穿透租户边界。
func (store *Store) Device(accountID, deviceID string) (domain.DeviceRegistration, error) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	device, ok := store.devices[deviceID]
	if !ok {
		return domain.DeviceRegistration{}, ErrNotFound
	}
	if device.AccountID != accountID {
		return domain.DeviceRegistration{}, ErrOwnership
	}
	device.PublicKey = append([]byte(nil), device.PublicKey...)
	return device, nil
}

// CreateManagedSession 建立 caller 与 target 设备之间的云托管连接 metadata。
// 两个设备必须属于同一账号且未撤销；该方法不会创建 terminal session 或授权 scope。
func (store *Store) CreateManagedSession(session domain.ManagedSession, now time.Time) error {
	if session.ID == "" || session.AccountID == "" || session.ClientDeviceID == "" || session.TargetDeviceID == "" || session.Hub.HubID == "" || session.Hub.Region == "" || session.CreatedAt.IsZero() {
		return fmt.Errorf("invalid managed session: %w", ErrConflict)
	}
	if !session.ExpiresAt.After(now) || session.CreatedAt.After(now) {
		return fmt.Errorf("invalid managed session lifetime: %w", ErrConflict)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	client, clientOK := store.devices[session.ClientDeviceID]
	target, targetOK := store.devices[session.TargetDeviceID]
	if !clientOK || !targetOK {
		return ErrNotFound
	}
	if client.AccountID != session.AccountID || target.AccountID != session.AccountID {
		return ErrOwnership
	}
	if client.Kind != domain.DeviceKindClient || target.Kind != domain.DeviceKindDaemon || client.RevokedAt != nil || target.RevokedAt != nil {
		return ErrOwnership
	}
	if current, ok := store.sessions[session.ID]; ok && current != session {
		return ErrConflict
	}
	store.sessions[session.ID] = session
	return nil
}

// ManagedSession 返回账号拥有且当前仍有效的 managed session。
// 过期只阻止新服务票据签发，不触发 daemon disconnect 或 capability revoke。
func (store *Store) ManagedSession(accountID, sessionID string, now time.Time) (domain.ManagedSession, error) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	session, ok := store.sessions[sessionID]
	if !ok {
		return domain.ManagedSession{}, ErrNotFound
	}
	if session.AccountID != accountID {
		return domain.ManagedSession{}, ErrOwnership
	}
	if !now.Before(session.ExpiresAt) {
		return domain.ManagedSession{}, ErrNotFound
	}
	return session, nil
}

func sameDevice(left, right domain.DeviceRegistration) bool {
	if left.ID != right.ID || left.AccountID != right.AccountID || left.OwnerUserID != right.OwnerUserID || left.Kind != right.Kind || left.Label != right.Label || left.Fingerprint != right.Fingerprint || !left.RegisteredAt.Equal(right.RegisteredAt) {
		return false
	}
	if len(left.PublicKey) != len(right.PublicKey) {
		return false
	}
	for index := range left.PublicKey {
		if left.PublicKey[index] != right.PublicKey[index] {
			return false
		}
	}
	return (left.RevokedAt == nil && right.RevokedAt == nil) || (left.RevokedAt != nil && right.RevokedAt != nil && left.RevokedAt.Equal(*right.RevokedAt))
}
