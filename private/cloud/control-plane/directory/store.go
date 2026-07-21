// Package directory 实现账号、设备所有权与 managed session 目录。
package directory

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/muxvia/muxvia/private/cloud/control-plane/domain"
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
	path          string
	organizations map[string]domain.Organization
	accounts      map[string]domain.Account
	users         map[string]domain.User
	devices       map[string]domain.DeviceRegistration
	sessions      map[string]domain.ManagedSession
}

// SecuritySnapshot 是 Control Plane 可持久化的账号、用户与设备安全目录。
// ManagedSession 不进入该快照；它是短期连接意图，重启后必须由客户端重新创建。
type SecuritySnapshot struct {
	Organizations []domain.Organization       `json:"organizations,omitempty"`
	Accounts      []domain.Account            `json:"accounts"`
	Users         []domain.User               `json:"users"`
	Devices       []domain.DeviceRegistration `json:"devices"`
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

// OpenStore 打开或创建一个使用 0600 原子快照的持久安全目录。
// 磁盘记录会通过同一领域校验重新装载；未知字段、超大文件或损坏内容直接 fail closed。
func OpenStore(path string) (*Store, error) {
	if path == "" || filepath.Base(path) == "." {
		return nil, fmt.Errorf("security directory path is required")
	}
	store := NewStore()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		store.path = path
		return store, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read security directory: %w", err)
	}
	defer clear(data)
	if len(data) == 0 || len(data) > 16<<20 {
		return nil, fmt.Errorf("security directory file is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var snapshot SecuritySnapshot
	if err := decoder.Decode(&snapshot); err != nil {
		return nil, fmt.Errorf("decode security directory: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, err
	}
	for _, organization := range snapshot.Organizations {
		if err := store.PutOrganization(organization); err != nil {
			return nil, fmt.Errorf("restore organization: %w", err)
		}
	}
	for _, account := range snapshot.Accounts {
		if err := store.PutAccount(account); err != nil {
			return nil, fmt.Errorf("restore account: %w", err)
		}
	}
	for _, user := range snapshot.Users {
		if err := store.PutUser(user); err != nil {
			return nil, fmt.Errorf("restore user: %w", err)
		}
	}
	for _, device := range snapshot.Devices {
		if err := store.RegisterDevice(device); err != nil {
			return nil, fmt.Errorf("restore device: %w", err)
		}
	}
	store.path = path
	return store, nil
}

// PutOrganization 写入新的团队治理与账单归属记录。
// 相同 ID 的不同内容会被拒绝，避免账号在未审计情况下切换 organization 真值。
func (store *Store) PutOrganization(organization domain.Organization) error {
	if organization.ID == "" || organization.CreatedAt.IsZero() {
		return fmt.Errorf("invalid organization: %w", ErrConflict)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	current, existed := store.organizations[organization.ID]
	if existed && current != organization {
		return ErrConflict
	}
	store.organizations[organization.ID] = organization
	if err := store.persistLocked(); err != nil {
		if existed {
			store.organizations[organization.ID] = current
		} else {
			delete(store.organizations, organization.ID)
		}
		return err
	}
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
	current, existed := store.accounts[account.ID]
	if existed && current != account {
		return ErrConflict
	}
	store.accounts[account.ID] = account
	if err := store.persistLocked(); err != nil {
		if existed {
			store.accounts[account.ID] = current
		} else {
			delete(store.accounts, account.ID)
		}
		return err
	}
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
	current, existed := store.users[user.ID]
	if existed && current != user {
		return ErrConflict
	}
	store.users[user.ID] = user
	if err := store.persistLocked(); err != nil {
		if existed {
			store.users[user.ID] = current
		} else {
			delete(store.users, user.ID)
		}
		return err
	}
	return nil
}

// RegisterDevice 建立账号与设备的所有权记录。
// PublicKey 会被复制，调用方后续修改输入切片不会改变目录真值。
func (store *Store) RegisterDevice(device domain.DeviceRegistration) error {
	clientKeyInvalid := device.Kind == domain.DeviceKindClient && !((len(device.PublicKey) == 0 && device.Fingerprint == "") || (len(device.PublicKey) == ed25519.PublicKeySize && device.Fingerprint != ""))
	if device.ID == "" || device.AccountID == "" || device.OwnerUserID == "" || device.RegisteredAt.IsZero() || device.Kind == domain.DeviceKindDaemon && (len(device.PublicKey) != ed25519.PublicKeySize || device.Fingerprint == "") || clientKeyInvalid {
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
	current, existed := store.devices[device.ID]
	if existed && !sameDevice(current, device) {
		return ErrConflict
	}
	store.devices[device.ID] = device
	if err := store.persistLocked(); err != nil {
		if existed {
			store.devices[device.ID] = current
		} else {
			delete(store.devices, device.ID)
		}
		return err
	}
	return nil
}

// EnrollDaemon 用新的账号注册码与 daemon 私钥证明建立或恢复 daemon ownership。
// 目录以 DeviceIdentity public key 为设备连续性真值：同一 key 可从 revoked 状态恢复或迁移到新账号，
// 不同 key、client 记录或非法 owner 一律拒绝。返回旧记录供调用方撤销旧账号 session、Presence 与 Web 投影。
func (store *Store) EnrollDaemon(device domain.DeviceRegistration) (domain.DeviceRegistration, bool, error) {
	if device.ID == "" || device.AccountID == "" || device.OwnerUserID == "" || device.Kind != domain.DeviceKindDaemon || device.RegisteredAt.IsZero() || len(device.PublicKey) != ed25519.PublicKeySize || device.Fingerprint == "" || device.RevokedAt != nil {
		return domain.DeviceRegistration{}, false, fmt.Errorf("invalid daemon enrollment: %w", ErrConflict)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	owner, ok := store.users[device.OwnerUserID]
	if !ok || owner.AccountID != device.AccountID {
		return domain.DeviceRegistration{}, false, ErrOwnership
	}
	device.PublicKey = append([]byte(nil), device.PublicKey...)
	current, existed := store.devices[device.ID]
	if existed && (current.Kind != domain.DeviceKindDaemon || current.Fingerprint != device.Fingerprint || !bytes.Equal(current.PublicKey, device.PublicKey)) {
		return domain.DeviceRegistration{}, false, ErrConflict
	}
	store.devices[device.ID] = device
	if err := store.persistLocked(); err != nil {
		if existed {
			store.devices[device.ID] = current
		} else {
			delete(store.devices, device.ID)
		}
		return domain.DeviceRegistration{}, false, err
	}
	current.PublicKey = append([]byte(nil), current.PublicKey...)
	return current, existed, nil
}

// RevokeDevice 持久化设备撤销时间。
// 调用方仍需把新的签名 policy 推送给 Hub；本方法不直接关闭 Hub runtime 状态。
func (store *Store) RevokeDevice(accountID, deviceID string, revokedAt time.Time) error {
	if accountID == "" || deviceID == "" || revokedAt.IsZero() {
		return fmt.Errorf("invalid device revocation: %w", ErrConflict)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	device, ok := store.devices[deviceID]
	if !ok {
		return ErrNotFound
	}
	if device.AccountID != accountID {
		return ErrOwnership
	}
	if revokedAt.Before(device.RegisteredAt) {
		return ErrConflict
	}
	previous := device.RevokedAt
	when := revokedAt.UTC()
	device.RevokedAt = &when
	store.devices[deviceID] = device
	if err := store.persistLocked(); err != nil {
		device.RevokedAt = previous
		store.devices[deviceID] = device
		return err
	}
	return nil
}

// Snapshot 返回持久安全目录的深拷贝和稳定排序投影。
// caller 可据此生成 Hub policy，但不能修改 Store 内部真值。
func (store *Store) Snapshot() SecuritySnapshot {
	store.mu.RLock()
	defer store.mu.RUnlock()
	return store.snapshotLocked()
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

func (store *Store) persistLocked() error {
	if store.path == "" {
		return nil
	}
	directoryPath := filepath.Dir(store.path)
	if err := os.MkdirAll(directoryPath, 0o700); err != nil {
		return fmt.Errorf("create security directory parent: %w", err)
	}
	file, err := os.CreateTemp(directoryPath, ".security-directory-*")
	if err != nil {
		return fmt.Errorf("create security directory snapshot: %w", err)
	}
	temporary := file.Name()
	committed := false
	defer func() {
		_ = file.Close()
		if !committed {
			_ = os.Remove(temporary)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return err
	}
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(store.snapshotLocked()); err != nil {
		return fmt.Errorf("encode security directory: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync security directory: %w", err)
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporary, store.path); err != nil {
		return fmt.Errorf("publish security directory: %w", err)
	}
	committed = true
	return nil
}

func (store *Store) snapshotLocked() SecuritySnapshot {
	snapshot := SecuritySnapshot{
		Organizations: make([]domain.Organization, 0, len(store.organizations)),
		Accounts:      make([]domain.Account, 0, len(store.accounts)),
		Users:         make([]domain.User, 0, len(store.users)),
		Devices:       make([]domain.DeviceRegistration, 0, len(store.devices)),
	}
	for _, organization := range store.organizations {
		snapshot.Organizations = append(snapshot.Organizations, organization)
	}
	for _, account := range store.accounts {
		snapshot.Accounts = append(snapshot.Accounts, account)
	}
	for _, user := range store.users {
		snapshot.Users = append(snapshot.Users, user)
	}
	for _, device := range store.devices {
		device.PublicKey = append([]byte(nil), device.PublicKey...)
		if device.RevokedAt != nil {
			when := *device.RevokedAt
			device.RevokedAt = &when
		}
		snapshot.Devices = append(snapshot.Devices, device)
	}
	sort.Slice(snapshot.Organizations, func(left, right int) bool { return snapshot.Organizations[left].ID < snapshot.Organizations[right].ID })
	sort.Slice(snapshot.Accounts, func(left, right int) bool { return snapshot.Accounts[left].ID < snapshot.Accounts[right].ID })
	sort.Slice(snapshot.Users, func(left, right int) bool { return snapshot.Users[left].ID < snapshot.Users[right].ID })
	sort.Slice(snapshot.Devices, func(left, right int) bool { return snapshot.Devices[left].ID < snapshot.Devices[right].ID })
	return snapshot
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return fmt.Errorf("security directory has trailing JSON")
	}
	return nil
}
