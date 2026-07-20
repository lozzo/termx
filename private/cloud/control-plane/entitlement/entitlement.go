// Package entitlement 归一化 Subscription、PlanCapability 与风控状态，并执行 managed P2P/Relay 准入。
//
// 该包不能依赖 terminal scope、daemon lifecycle 或 heartbeat。Entitlement 失效只拒绝新的
// managed Cloud 服务，不撤销 CapabilityGrant，也不影响 local、SSH 或 Direct Route。
package entitlement

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
	// ErrNotEntitled 表示账号当前不能取得请求的 managed Cloud 服务。
	ErrNotEntitled = errors.New("account is not entitled to managed cloud service")
	// ErrQuotaPolicy 表示请求的 region、path 或资源超过 PlanCapability。
	ErrQuotaPolicy = errors.New("relay request exceeds plan capability")
	// ErrEntitlementNotFound 表示账号尚未配置 entitlement。
	ErrEntitlementNotFound = errors.New("cloud entitlement not found")
	// ErrPlanCapability 表示 catalog 中的机器能力缺失、矛盾或仍包含未启用能力。
	ErrPlanCapability = errors.New("invalid plan capability")
)

// Status 直接复用 Proto schema 的 entitlement 状态，避免 Go-only 平行枚举。
type Status = cloudpb.EntitlementStatus

const (
	// StatusActive 允许按 PlanCapability 执行 managed P2P/Relay 准入。
	StatusActive = cloudpb.EntitlementStatus_ENTITLEMENT_STATUS_ACTIVE
	// StatusSuspended 因风控或管理策略暂停所有 managed Cloud 新准入。
	StatusSuspended = cloudpb.EntitlementStatus_ENTITLEMENT_STATUS_SUSPENDED
	// StatusExpired 表示当前 Subscription 已失效。
	StatusExpired = cloudpb.EntitlementStatus_ENTITLEMENT_STATUS_EXPIRED
)

// Entitlement 是账号当前 Subscription、套餐能力和风控决策的归一化结果。
// 业务判断只能读取 Status、有效窗口和 generated PlanCapability，不能根据 plan ID 或日期猜能力。
type Entitlement struct {
	AccountID            string
	Status               Status
	EffectiveFrom        time.Time
	EffectiveUntil       time.Time
	SourceSubscriptionID string
	SourceOrderID        string
	SourcePlanID         string
	SourcePlanVersion    uint64
	Capability           *cloudpb.PlanCapability
	UpdatedAt            time.Time
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

// Normalize 把一个 SubscriptionProjection 与其精确 versioned PlanDefinition 归一化为 Entitlement。
// plan ID/version、周期和状态不匹配时 fail closed，不能 fallback 到默认套餐。
func Normalize(subscription *cloudpb.SubscriptionProjection, plan *cloudpb.PlanDefinition, updatedAt time.Time) (Entitlement, error) {
	if subscription == nil || plan == nil || updatedAt.IsZero() || subscription.GetAccountId() == "" || subscription.GetSubscriptionId() == "" ||
		subscription.GetPlanId() != plan.GetPlanId() || subscription.GetPlanVersion() != plan.GetPlanVersion() || plan.GetPlanId() == "" || plan.GetPlanVersion() == 0 {
		return Entitlement{}, fmt.Errorf("normalize entitlement: %w", ErrPlanCapability)
	}
	from := time.UnixMilli(subscription.GetCurrentPeriodStartUnixMillis()).UTC()
	until := time.UnixMilli(subscription.GetCurrentPeriodEndUnixMillis()).UTC()
	if subscription.GetCurrentPeriodStartUnixMillis() <= 0 || subscription.GetCurrentPeriodEndUnixMillis() <= 0 || !until.After(from) {
		return Entitlement{}, fmt.Errorf("normalize entitlement period: %w", ErrPlanCapability)
	}
	if err := ValidatePlanCapability(plan.GetCapability()); err != nil {
		return Entitlement{}, err
	}
	status := StatusExpired
	switch subscription.GetStatus() {
	case cloudpb.SubscriptionStatus_SUBSCRIPTION_STATUS_ACTIVE,
		cloudpb.SubscriptionStatus_SUBSCRIPTION_STATUS_CANCEL_AT_PERIOD_END:
		status = StatusActive
	case cloudpb.SubscriptionStatus_SUBSCRIPTION_STATUS_SUSPENDED:
		status = StatusSuspended
	case cloudpb.SubscriptionStatus_SUBSCRIPTION_STATUS_CANCELED,
		cloudpb.SubscriptionStatus_SUBSCRIPTION_STATUS_EXPIRED:
		status = StatusExpired
	default:
		return Entitlement{}, fmt.Errorf("normalize entitlement status: %w", ErrNotEntitled)
	}
	return Entitlement{
		AccountID: subscription.GetAccountId(), Status: status,
		EffectiveFrom: from, EffectiveUntil: until,
		SourceSubscriptionID: subscription.GetSubscriptionId(), SourceOrderID: subscription.GetSourceOrderId(),
		SourcePlanID: plan.GetPlanId(), SourcePlanVersion: plan.GetPlanVersion(),
		Capability: ClonePlanCapability(plan.GetCapability()), UpdatedAt: updatedAt.UTC(),
	}, nil
}

// ValidatePlanCapability 验证 catalog、Entitlement 与 Hub policy 共用的机器能力。
// 当前阶段只允许 managed P2P 和 single Relay；Relay Mesh 继续保持延后。
func ValidatePlanCapability(capability *cloudpb.PlanCapability) error {
	if capability == nil || capability.GetCloudDeviceLimit() == 0 || !capability.GetManagedP2PEnabled() && !capability.GetStandardRelayEnabled() {
		return ErrPlanCapability
	}
	if capability.GetManagedP2PEnabled() != (capability.GetManagedP2PMaxConcurrency() > 0) {
		return ErrPlanCapability
	}
	relay := capability.GetRelay()
	if !capability.GetStandardRelayEnabled() {
		if relay != nil && (len(relay.GetAllowedRegions()) != 0 || relay.GetAllowRelayMesh() || relay.GetMaxLeaseSeconds() != 0 || relay.GetMaxBytesPerLease() != 0 || relay.GetMaxBitrateKbps() != 0 || relay.GetMaxConcurrency() != 0 || relay.GetMaxBytesPerPeriod() != 0) {
			return ErrPlanCapability
		}
		return nil
	}
	if relay == nil || relay.GetAllowRelayMesh() || len(relay.GetAllowedRegions()) == 0 || relay.GetMaxLeaseSeconds() == 0 || relay.GetMaxBytesPerLease() == 0 || relay.GetMaxBitrateKbps() == 0 || relay.GetMaxConcurrency() == 0 || relay.GetMaxBytesPerPeriod() < relay.GetMaxBytesPerLease() {
		return ErrPlanCapability
	}
	regions := make(map[string]struct{}, len(relay.GetAllowedRegions()))
	for _, region := range relay.GetAllowedRegions() {
		if region == "" {
			return ErrPlanCapability
		}
		if _, exists := regions[region]; exists {
			return ErrPlanCapability
		}
		regions[region] = struct{}{}
	}
	return nil
}

// ClonePlanCapability 返回 generated PlanCapability 的深拷贝，防止调用方修改 store 或签名投影真值。
func ClonePlanCapability(capability *cloudpb.PlanCapability) *cloudpb.PlanCapability {
	if capability == nil {
		return nil
	}
	return proto.Clone(capability).(*cloudpb.PlanCapability)
}

// AuthorizeManagedP2P 判断当前 entitlement 是否允许新的 managed P2P signaling。
func (entitlement Entitlement) AuthorizeManagedP2P(now time.Time) error {
	if !entitlement.activeAt(now) || entitlement.Capability == nil || !entitlement.Capability.GetManagedP2PEnabled() {
		return ErrNotEntitled
	}
	return nil
}

// AuthorizeRelay 判断账号能否取得新的 Relay lease 并返回 PlanCapability quota。
// 已经签发的 lease 由其自身短 TTL 和 Relay enforcement 管理，不在这里被远程撤销。
func (entitlement Entitlement) AuthorizeRelay(request RelayRequest, now time.Time) (RelayAllocation, error) {
	if !entitlement.activeAt(now) || entitlement.Capability == nil || !entitlement.Capability.GetStandardRelayEnabled() {
		return RelayAllocation{}, ErrNotEntitled
	}
	policy := entitlement.Capability.GetRelay()
	if err := ValidatePlanCapability(entitlement.Capability); err != nil {
		return RelayAllocation{}, ErrQuotaPolicy
	}
	if request.Region == "" || !contains(policy.GetAllowedRegions(), request.Region) {
		return RelayAllocation{}, ErrQuotaPolicy
	}
	if request.PathKind != servicecredential.RelayPathSingle || policy.GetAllowRelayMesh() {
		return RelayAllocation{}, ErrQuotaPolicy
	}
	maxLeaseDuration := time.Duration(policy.GetMaxLeaseSeconds()) * time.Second
	ttl := request.RequestedTTL
	if ttl <= 0 || ttl > maxLeaseDuration {
		ttl = maxLeaseDuration
	}
	remaining := entitlement.EffectiveUntil.Sub(now.UTC())
	if ttl > remaining {
		ttl = remaining
	}
	if ttl <= 0 {
		return RelayAllocation{}, ErrNotEntitled
	}
	return RelayAllocation{
		TTL: ttl, MaxBytes: policy.GetMaxBytesPerLease(), MaxBitrateKbps: policy.GetMaxBitrateKbps(), MaxConcurrency: policy.GetMaxConcurrency(),
	}, nil
}

// Projection 返回跨进程 API 与 Hub policy mapping 使用的 generated EntitlementProjection。
func (entitlement Entitlement) Projection() *cloudpb.EntitlementProjection {
	return &cloudpb.EntitlementProjection{
		AccountId: entitlement.AccountID, Status: entitlement.Status,
		SourceSubscriptionId: entitlement.SourceSubscriptionID, SourceOrderId: entitlement.SourceOrderID,
		SourcePlanId: entitlement.SourcePlanID, SourcePlanVersion: entitlement.SourcePlanVersion,
		EffectiveFromUnixMillis: entitlement.EffectiveFrom.UnixMilli(), EffectiveUntilUnixMillis: entitlement.EffectiveUntil.UnixMilli(),
		Capability: ClonePlanCapability(entitlement.Capability), UpdatedAtUnixMillis: entitlement.UpdatedAt.UnixMilli(),
	}
}

func (entitlement Entitlement) activeAt(now time.Time) bool {
	now = now.UTC()
	return entitlement.AccountID != "" && entitlement.Status == StatusActive && !now.Before(entitlement.EffectiveFrom) && now.Before(entitlement.EffectiveUntil)
}

// Store 是 Control Plane entitlement 的并发安全内存 contract 实现。
// 生产 adapter 可以换成数据库，但不能把 terminal scope 或 daemon connection 写入该接口。
type Store struct {
	mu      sync.RWMutex
	entries map[string]Entitlement
}

// NewStore 创建空 entitlement store。
func NewStore() *Store { return &Store{entries: make(map[string]Entitlement)} }

// Put 原子写入账号 entitlement，并深拷贝 generated capability。
func (store *Store) Put(value Entitlement) error {
	if value.AccountID == "" || value.EffectiveFrom.IsZero() || value.EffectiveUntil.IsZero() || !value.EffectiveUntil.After(value.EffectiveFrom) || value.UpdatedAt.IsZero() || value.SourcePlanID == "" || value.SourcePlanVersion == 0 {
		return fmt.Errorf("invalid entitlement")
	}
	if err := ValidatePlanCapability(value.Capability); err != nil {
		return err
	}
	value.Capability = ClonePlanCapability(value.Capability)
	store.mu.Lock()
	store.entries[value.AccountID] = value
	store.mu.Unlock()
	return nil
}

// Entitlement 返回账号当前的套餐能力快照，并深拷贝 generated capability。
func (store *Store) Entitlement(accountID string) (Entitlement, error) {
	store.mu.RLock()
	value, ok := store.entries[accountID]
	store.mu.RUnlock()
	if !ok {
		return Entitlement{}, ErrEntitlementNotFound
	}
	value.Capability = ClonePlanCapability(value.Capability)
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
