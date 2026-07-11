// Package entitlement 决定账号是否可以取得新的付费 Relay 服务租约。
//
// 该包不能依赖 terminal scope、daemon lifecycle 或 heartbeat。订阅失效只拒绝新的
// Relay allocation，不撤销 CapabilityGrant，也不影响 local、SSH 或 direct P2P。
package entitlement

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/lozzow/termx/private/cloud/control-plane/servicecredential"
)

var (
	// ErrNotEntitled 表示账号当前不能取得新的付费 Relay lease。
	ErrNotEntitled = errors.New("account is not entitled to managed relay")
	// ErrQuotaPolicy 表示请求的 region、path 或资源超过服务端 quota policy。
	ErrQuotaPolicy = errors.New("relay request exceeds quota policy")
	// ErrEntitlementNotFound 表示账号尚未配置 entitlement。
	ErrEntitlementNotFound = errors.New("relay entitlement not found")
)

// Status 表示云服务 entitlement 的商业状态。
// 该状态不映射成 terminal authorization，且不触发 daemon presence kick。
type Status string

const (
	// StatusActive 允许按 QuotaPolicy 签发新的 Relay lease。
	StatusActive Status = "active"
	// StatusSuspended 因风控或管理策略暂停新的付费服务准入。
	StatusSuspended Status = "suspended"
	// StatusExpired 表示套餐有效期已经结束。
	StatusExpired Status = "expired"
)

// QuotaPolicy 定义单个账号签发 Relay lease 时的服务端硬上限。
// 所有数值会被复制到短期 lease，由 Relay 离线执行；客户端不能扩大这些值。
type QuotaPolicy struct {
	AllowedRegions   []string
	AllowRelayMesh   bool
	MaxLeaseDuration time.Duration
	MaxBytesPerLease uint64
	MaxBitrateKbps   uint32
	MaxConcurrency   uint32
}

// Entitlement 是账号当前套餐和风控决策的归一化结果。
// SourcePlanRef 可关联订单或套餐，但业务判断只依赖这里的稳定服务能力字段。
type Entitlement struct {
	AccountID     string
	Status        Status
	ValidUntil    time.Time
	SourcePlanRef string
	Policy        QuotaPolicy
	UpdatedAt     time.Time
}

// RelayRequest 描述申请新 Relay lease 所需的服务能力。
// 它只包含 region、path 和期望时长，不包含 terminal、grant 或 protocol 数据。
type RelayRequest struct {
	Region       string
	PathKind     servicecredential.RelayPathKind
	RequestedTTL time.Duration
}

// RelayAllocation 是 entitlement domain clamp 后允许写入 Relay lease 的资源上限。
// Issuer 必须使用该结果，不能重新信任客户端传入的 quota 数值。
type RelayAllocation struct {
	TTL            time.Duration
	MaxBytes       uint64
	MaxBitrateKbps uint32
	MaxConcurrency uint32
}

// AuthorizeRelay 判断账号能否取得新的 Relay lease 并返回服务端 quota。
// 已经签发的 lease 由其自身短 TTL 和 Relay enforcement 管理，不在这里被远程撤销。
func (entitlement Entitlement) AuthorizeRelay(request RelayRequest, now time.Time) (RelayAllocation, error) {
	if entitlement.AccountID == "" || entitlement.Status != StatusActive || !now.Before(entitlement.ValidUntil) {
		return RelayAllocation{}, ErrNotEntitled
	}
	if request.Region == "" || !contains(entitlement.Policy.AllowedRegions, request.Region) {
		return RelayAllocation{}, ErrQuotaPolicy
	}
	if request.PathKind != servicecredential.RelayPathSingle && request.PathKind != servicecredential.RelayPathMesh {
		return RelayAllocation{}, ErrQuotaPolicy
	}
	if request.PathKind == servicecredential.RelayPathMesh && !entitlement.Policy.AllowRelayMesh {
		return RelayAllocation{}, ErrQuotaPolicy
	}
	policy := entitlement.Policy
	if policy.MaxLeaseDuration <= 0 || policy.MaxBytesPerLease == 0 || policy.MaxBitrateKbps == 0 || policy.MaxConcurrency == 0 {
		return RelayAllocation{}, ErrQuotaPolicy
	}
	ttl := request.RequestedTTL
	if ttl <= 0 || ttl > policy.MaxLeaseDuration {
		ttl = policy.MaxLeaseDuration
	}
	remaining := entitlement.ValidUntil.Sub(now)
	if ttl > remaining {
		ttl = remaining
	}
	if ttl <= 0 {
		return RelayAllocation{}, ErrNotEntitled
	}
	return RelayAllocation{
		TTL:            ttl,
		MaxBytes:       policy.MaxBytesPerLease,
		MaxBitrateKbps: policy.MaxBitrateKbps,
		MaxConcurrency: policy.MaxConcurrency,
	}, nil
}

// Store 是 Control Plane entitlement 的并发安全内存 contract 实现。
// 生产 adapter 可以换成数据库，但不能把 terminal scope 或 daemon connection 写入该接口。
type Store struct {
	mu      sync.RWMutex
	entries map[string]Entitlement
}

// NewStore 创建空 entitlement store。
func NewStore() *Store {
	return &Store{entries: make(map[string]Entitlement)}
}

// Put 原子写入账号 entitlement。
// AllowedRegions 会被复制，避免调用方修改切片后绕过后续 policy 判断。
func (store *Store) Put(value Entitlement) error {
	if value.AccountID == "" || value.ValidUntil.IsZero() || value.UpdatedAt.IsZero() {
		return fmt.Errorf("invalid entitlement")
	}
	value.Policy.AllowedRegions = append([]string(nil), value.Policy.AllowedRegions...)
	store.mu.Lock()
	store.entries[value.AccountID] = value
	store.mu.Unlock()
	return nil
}

// Entitlement 返回账号当前的套餐能力快照。
// 返回值包含复制后的 region 切片，可安全交给单次 lease issuance 使用。
func (store *Store) Entitlement(accountID string) (Entitlement, error) {
	store.mu.RLock()
	value, ok := store.entries[accountID]
	store.mu.RUnlock()
	if !ok {
		return Entitlement{}, ErrEntitlementNotFound
	}
	value.Policy.AllowedRegions = append([]string(nil), value.Policy.AllowedRegions...)
	return value, nil
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
