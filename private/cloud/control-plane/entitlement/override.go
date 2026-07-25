package entitlement

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/muxvia/muxvia/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

var (
	// ErrOverrideInvalid 表示覆盖字段、时间窗或审计信息不完整。
	ErrOverrideInvalid = errors.New("invalid entitlement override")
	// ErrOverrideConflict 表示 revision 不匹配或同一时间窗内重复拥有同一能力字段。
	ErrOverrideConflict = errors.New("entitlement override conflict")
	// ErrOverrideNotFound 表示账号下不存在指定覆盖。
	ErrOverrideNotFound = errors.New("entitlement override not found")
)

var allowedOverridePaths = map[string]func(*cloudpb.PlanCapability, *cloudpb.PlanCapability){
	"managed_p2p_enabled": func(dst, src *cloudpb.PlanCapability) { dst.ManagedP2PEnabled = src.GetManagedP2PEnabled() },
	"managed_p2p_max_concurrency": func(dst, src *cloudpb.PlanCapability) {
		dst.ManagedP2PMaxConcurrency = src.GetManagedP2PMaxConcurrency()
	},
	"standard_relay_enabled": func(dst, src *cloudpb.PlanCapability) { dst.StandardRelayEnabled = src.GetStandardRelayEnabled() },
	"cloud_device_limit":     func(dst, src *cloudpb.PlanCapability) { dst.CloudDeviceLimit = src.GetCloudDeviceLimit() },
	"relay.allowed_regions": func(dst, src *cloudpb.PlanCapability) {
		ensureRelay(dst).AllowedRegions = append([]string(nil), src.GetRelay().GetAllowedRegions()...)
	},
	"relay.allow_relay_mesh": func(dst, src *cloudpb.PlanCapability) {
		ensureRelay(dst).AllowRelayMesh = src.GetRelay().GetAllowRelayMesh()
	},
	"relay.max_lease_seconds": func(dst, src *cloudpb.PlanCapability) {
		ensureRelay(dst).MaxLeaseSeconds = src.GetRelay().GetMaxLeaseSeconds()
	},
	"relay.max_bytes_per_lease": func(dst, src *cloudpb.PlanCapability) {
		ensureRelay(dst).MaxBytesPerLease = src.GetRelay().GetMaxBytesPerLease()
	},
	"relay.max_bitrate_kbps": func(dst, src *cloudpb.PlanCapability) {
		ensureRelay(dst).MaxBitrateKbps = src.GetRelay().GetMaxBitrateKbps()
	},
	"relay.max_concurrency": func(dst, src *cloudpb.PlanCapability) {
		ensureRelay(dst).MaxConcurrency = src.GetRelay().GetMaxConcurrency()
	},
	"relay.max_bytes_per_period": func(dst, src *cloudpb.PlanCapability) {
		ensureRelay(dst).MaxBytesPerPeriod = src.GetRelay().GetMaxBytesPerPeriod()
	},
}

// OverrideStore 是类型化覆盖、Entitlement 与运营审计的持久事务边界。
type OverrideStore interface {
	Subscription(context.Context, string) (*cloudpb.SubscriptionProjection, error)
	Entitlement(context.Context, string) (*cloudpb.EntitlementProjection, error)
	EntitlementOverrides(context.Context, string, bool, int) ([]*cloudpb.EntitlementOverrideProjection, error)
	CommitEntitlementOverride(context.Context, *cloudpb.EntitlementOverrideProjection, uint64, *cloudpb.EntitlementProjection, *cloudpb.OperatorMutationAuditProjection, time.Time) error
	RevokeEntitlementOverride(context.Context, *cloudpb.EntitlementOverrideProjection, uint64, *cloudpb.EntitlementProjection, *cloudpb.OperatorMutationAuditProjection, time.Time) error
	ReconcileEntitlementOverrides(context.Context, string, *cloudpb.EntitlementProjection, *cloudpb.OperatorMutationAuditProjection, time.Time) error
	EntitlementOverrideAccountsDue(context.Context, time.Time, int) ([]string, error)
}

// PlanSource 返回订阅固定版本对应的历史套餐。
type PlanSource interface {
	Plan(context.Context, string, uint64) (*cloudpb.PlanDefinition, error)
}

// OverrideServiceConfig 固定事务 Store、历史套餐、时间、随机源和 policy 通知。
type OverrideServiceConfig struct {
	Store              OverrideStore
	Plans              PlanSource
	Now                func() time.Time
	Random             io.Reader
	NotifyPolicyChange func(string)
}

// OverrideService 拥有覆盖 mutation 与自然生效/到期重算。
type OverrideService struct {
	store  OverrideStore
	plans  PlanSource
	now    func() time.Time
	random io.Reader
	notify func(string)
}

// NewOverrideService 创建覆盖服务；缺少持久 owner 或历史套餐来源时 fail closed。
func NewOverrideService(config OverrideServiceConfig) (*OverrideService, error) {
	if config.Store == nil || config.Plans == nil {
		return nil, ErrOverrideInvalid
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Random == nil {
		config.Random = rand.Reader
	}
	return &OverrideService{store: config.Store, plans: config.Plans, now: config.Now, random: config.Random, notify: config.NotifyPolicyChange}, nil
}

// Put 创建或以 expected revision 更新覆盖，并在当前已生效时同事务重算 Entitlement。
func (service *OverrideService) Put(ctx context.Context, request *cloudpb.PutEntitlementOverrideRequest, actorID string) (*cloudpb.PutEntitlementOverrideResponse, error) {
	if request == nil || request.GetOverride() == nil || strings.TrimSpace(actorID) == "" || strings.TrimSpace(request.GetRequestId()) == "" {
		return nil, ErrOverrideInvalid
	}
	now := service.now().UTC()
	next := proto.Clone(request.GetOverride()).(*cloudpb.EntitlementOverrideProjection)
	next.AccountId, next.ActorId, next.Reason = strings.TrimSpace(next.GetAccountId()), strings.TrimSpace(actorID), strings.TrimSpace(next.GetReason())
	if next.GetOverrideId() == "" {
		if request.GetExpectedRevision() != 0 {
			return nil, ErrOverrideConflict
		}
		id, err := service.randomID()
		if err != nil {
			return nil, err
		}
		next.OverrideId, next.Revision, next.CreatedAtUnixMillis = id, 1, now.UnixMilli()
	} else {
		if request.GetExpectedRevision() == 0 {
			return nil, ErrOverrideConflict
		}
		next.Revision = request.GetExpectedRevision() + 1
	}
	next.UpdatedAtUnixMillis, next.RevokedAtUnixMillis = now.UnixMilli(), 0
	if err := validateOverride(next); err != nil {
		return nil, err
	}
	existing, err := service.store.EntitlementOverrides(ctx, next.GetAccountId(), true, 200)
	if err != nil {
		return nil, err
	}
	if request.GetExpectedRevision() > 0 {
		for _, current := range existing {
			if current.GetOverrideId() == next.GetOverrideId() {
				next.CreatedAtUnixMillis = current.GetCreatedAtUnixMillis()
				break
			}
		}
	}
	if err := validateOverrideMutation(existing, next, request.GetExpectedRevision()); err != nil {
		return nil, err
	}
	entitlementProjection, err := service.recompute(ctx, next.GetAccountId(), replaceOverride(existing, next), now)
	if err != nil {
		return nil, err
	}
	current, err := service.store.Entitlement(ctx, next.GetAccountId())
	if err != nil {
		return nil, err
	}
	changed := !proto.Equal(current.GetCapability(), entitlementProjection.GetCapability())
	if !changed {
		entitlementProjection = nil
	}
	audit := mutationAudit(next.GetAccountId(), actorID, "entitlement_override.put", next.GetOverrideId(), next.GetReason(), request.GetRequestId(), request.GetExpectedRevision(), next.GetRevision(), now)
	if err := service.store.CommitEntitlementOverride(ctx, next, request.GetExpectedRevision(), entitlementProjection, audit, now); err != nil {
		return nil, err
	}
	if changed {
		service.notifyPolicy(next.GetAccountId())
		current = entitlementProjection
	}
	return &cloudpb.PutEntitlementOverrideResponse{Override: proto.Clone(next).(*cloudpb.EntitlementOverrideProjection), Entitlement: proto.Clone(current).(*cloudpb.EntitlementProjection)}, nil
}

// Revoke 以 expected revision 撤销覆盖，并同事务移除其当前能力影响。
func (service *OverrideService) Revoke(ctx context.Context, request *cloudpb.RevokeEntitlementOverrideRequest, actorID string) (*cloudpb.RevokeEntitlementOverrideResponse, error) {
	if request == nil || request.GetAccountId() == "" || request.GetOverrideId() == "" || request.GetExpectedRevision() == 0 || strings.TrimSpace(request.GetReason()) == "" || strings.TrimSpace(request.GetRequestId()) == "" || actorID == "" {
		return nil, ErrOverrideInvalid
	}
	now := service.now().UTC()
	values, err := service.store.EntitlementOverrides(ctx, request.GetAccountId(), true, 200)
	if err != nil {
		return nil, err
	}
	var revoked *cloudpb.EntitlementOverrideProjection
	for _, value := range values {
		if value.GetOverrideId() == request.GetOverrideId() {
			revoked = proto.Clone(value).(*cloudpb.EntitlementOverrideProjection)
			break
		}
	}
	if revoked == nil {
		return nil, ErrOverrideNotFound
	}
	if revoked.GetRevision() != request.GetExpectedRevision() || revoked.GetRevokedAtUnixMillis() != 0 {
		return nil, ErrOverrideConflict
	}
	revoked.Revision++
	revoked.RevokedAtUnixMillis, revoked.UpdatedAtUnixMillis = now.UnixMilli(), now.UnixMilli()
	revoked.ActorId, revoked.Reason = actorID, strings.TrimSpace(request.GetReason())
	entitlementProjection, err := service.recompute(ctx, request.GetAccountId(), replaceOverride(values, revoked), now)
	if err != nil {
		return nil, err
	}
	current, err := service.store.Entitlement(ctx, request.GetAccountId())
	if err != nil {
		return nil, err
	}
	changed := !proto.Equal(current.GetCapability(), entitlementProjection.GetCapability())
	if !changed {
		entitlementProjection = nil
	}
	audit := mutationAudit(request.GetAccountId(), actorID, "entitlement_override.revoke", request.GetOverrideId(), request.GetReason(), request.GetRequestId(), request.GetExpectedRevision(), revoked.GetRevision(), now)
	if err := service.store.RevokeEntitlementOverride(ctx, revoked, request.GetExpectedRevision(), entitlementProjection, audit, now); err != nil {
		return nil, err
	}
	if changed {
		service.notifyPolicy(request.GetAccountId())
		current = entitlementProjection
	}
	return &cloudpb.RevokeEntitlementOverrideResponse{Override: revoked, Entitlement: proto.Clone(current).(*cloudpb.EntitlementProjection)}, nil
}

// ReconcileDue 有界处理自然生效/到期的账号，成功事务后发布 policy change。
func (service *OverrideService) ReconcileDue(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 200 {
		return 0, ErrOverrideInvalid
	}
	now := service.now().UTC()
	accounts, err := service.store.EntitlementOverrideAccountsDue(ctx, now, limit)
	if err != nil {
		return 0, err
	}
	for _, accountID := range accounts {
		values, loadErr := service.store.EntitlementOverrides(ctx, accountID, false, 200)
		if loadErr != nil {
			return 0, loadErr
		}
		next, computeErr := service.recompute(ctx, accountID, values, now)
		if computeErr != nil {
			return 0, computeErr
		}
		audit := mutationAudit(accountID, "system", "entitlement_override.reconcile", accountID, "scheduled override activation or expiry", fmt.Sprintf("reconcile-%s-%d", accountID, next.GetRevision()), next.GetRevision()-1, next.GetRevision(), now)
		if commitErr := service.store.ReconcileEntitlementOverrides(ctx, accountID, next, audit, now); commitErr != nil {
			return 0, commitErr
		}
		service.notifyPolicy(accountID)
	}
	return len(accounts), nil
}

// List 返回账号覆盖历史；includeRevoked 由 operator 明确选择。
func (service *OverrideService) List(ctx context.Context, accountID string, includeRevoked bool, limit int) ([]*cloudpb.EntitlementOverrideProjection, error) {
	if accountID == "" || limit < 1 || limit > 200 {
		return nil, ErrOverrideInvalid
	}
	return service.store.EntitlementOverrides(ctx, accountID, includeRevoked, limit)
}

func (service *OverrideService) recompute(ctx context.Context, accountID string, values []*cloudpb.EntitlementOverrideProjection, now time.Time) (*cloudpb.EntitlementProjection, error) {
	subscription, err := service.store.Subscription(ctx, accountID)
	if err != nil {
		return nil, err
	}
	plan, err := service.plans.Plan(ctx, subscription.GetPlanId(), subscription.GetPlanVersion())
	if err != nil {
		return nil, err
	}
	base, err := Normalize(subscription, plan, now)
	if err != nil {
		return nil, err
	}
	capability, err := ApplyOverrides(base.Capability, values, now)
	if err != nil {
		return nil, err
	}
	base.Capability, base.UpdatedAt = capability, now
	projection := base.Projection()
	current, err := service.store.Entitlement(ctx, accountID)
	if err != nil {
		return nil, err
	}
	projection.Revision = current.GetRevision() + 1
	return projection, nil
}

// ApplyOverrides 按稳定顺序把当前有效覆盖应用到基础能力，并重新执行完整能力校验。
func ApplyOverrides(base *cloudpb.PlanCapability, values []*cloudpb.EntitlementOverrideProjection, now time.Time) (*cloudpb.PlanCapability, error) {
	if base == nil || now.IsZero() {
		return nil, ErrOverrideInvalid
	}
	ordered := append([]*cloudpb.EntitlementOverrideProjection(nil), values...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].GetEffectiveFromUnixMillis() != ordered[j].GetEffectiveFromUnixMillis() {
			return ordered[i].GetEffectiveFromUnixMillis() < ordered[j].GetEffectiveFromUnixMillis()
		}
		return ordered[i].GetOverrideId() < ordered[j].GetOverrideId()
	})
	result := ClonePlanCapability(base)
	for _, value := range ordered {
		if value.GetRevokedAtUnixMillis() != 0 || now.UnixMilli() < value.GetEffectiveFromUnixMillis() || now.UnixMilli() >= value.GetEffectiveUntilUnixMillis() {
			continue
		}
		if err := validateOverride(value); err != nil {
			return nil, err
		}
		for _, path := range value.GetCapabilityMask().GetPaths() {
			allowedOverridePaths[path](result, value.GetCapability())
		}
	}
	if err := ValidatePlanCapability(result); err != nil {
		return nil, err
	}
	return result, nil
}

func validateOverride(value *cloudpb.EntitlementOverrideProjection) error {
	if value == nil || value.GetOverrideId() == "" || value.GetAccountId() == "" || value.GetRevision() == 0 || value.GetCapability() == nil || value.GetCapabilityMask() == nil || len(value.GetCapabilityMask().GetPaths()) == 0 || value.GetEffectiveFromUnixMillis() <= 0 || value.GetEffectiveUntilUnixMillis() <= value.GetEffectiveFromUnixMillis() || value.GetReason() == "" || value.GetActorId() == "" {
		return ErrOverrideInvalid
	}
	seen := make(map[string]struct{}, len(value.GetCapabilityMask().GetPaths()))
	for _, path := range value.GetCapabilityMask().GetPaths() {
		if _, ok := allowedOverridePaths[path]; !ok {
			return ErrOverrideInvalid
		}
		if _, exists := seen[path]; exists {
			return ErrOverrideInvalid
		}
		seen[path] = struct{}{}
		if strings.HasPrefix(path, "relay.") && value.GetCapability().GetRelay() == nil {
			return ErrOverrideInvalid
		}
	}
	return nil
}

func validateOverrideMutation(existing []*cloudpb.EntitlementOverrideProjection, next *cloudpb.EntitlementOverrideProjection, expected uint64) error {
	found := false
	nextPaths := pathSet(next)
	for _, current := range existing {
		if current.GetOverrideId() == next.GetOverrideId() {
			found = true
			if current.GetRevision() != expected || current.GetRevokedAtUnixMillis() != 0 {
				return ErrOverrideConflict
			}
			continue
		}
		if current.GetRevokedAtUnixMillis() != 0 || current.GetEffectiveFromUnixMillis() >= next.GetEffectiveUntilUnixMillis() || next.GetEffectiveFromUnixMillis() >= current.GetEffectiveUntilUnixMillis() {
			continue
		}
		for path := range pathSet(current) {
			if _, overlaps := nextPaths[path]; overlaps {
				return ErrOverrideConflict
			}
		}
	}
	if expected > 0 && !found {
		return ErrOverrideNotFound
	}
	return nil
}

func replaceOverride(values []*cloudpb.EntitlementOverrideProjection, next *cloudpb.EntitlementOverrideProjection) []*cloudpb.EntitlementOverrideProjection {
	result := make([]*cloudpb.EntitlementOverrideProjection, 0, len(values)+1)
	found := false
	for _, value := range values {
		if value.GetOverrideId() == next.GetOverrideId() {
			result, found = append(result, next), true
		} else {
			result = append(result, value)
		}
	}
	if !found {
		result = append(result, next)
	}
	return result
}

func pathSet(value *cloudpb.EntitlementOverrideProjection) map[string]struct{} {
	result := make(map[string]struct{}, len(value.GetCapabilityMask().GetPaths()))
	for _, path := range value.GetCapabilityMask().GetPaths() {
		result[path] = struct{}{}
	}
	return result
}

func ensureRelay(value *cloudpb.PlanCapability) *cloudpb.RelayServiceCapability {
	if value.Relay == nil {
		value.Relay = &cloudpb.RelayServiceCapability{}
	}
	return value.Relay
}

func (service *OverrideService) randomID() (string, error) {
	value := make([]byte, 18)
	if _, err := io.ReadFull(service.random, value); err != nil {
		return "", err
	}
	return "override_" + base64.RawURLEncoding.EncodeToString(value), nil
}

func (service *OverrideService) notifyPolicy(accountID string) {
	if service.notify != nil {
		service.notify(accountID)
	}
}

func mutationAudit(accountID, actorID, action, resourceID, reason, requestID string, before, after uint64, now time.Time) *cloudpb.OperatorMutationAuditProjection {
	return &cloudpb.OperatorMutationAuditProjection{AuditId: fmt.Sprintf("audit_%s_%d", resourceID, after), ActorId: actorID, Action: action, ResourceKind: "entitlement_override", ResourceId: resourceID, AccountId: accountID, Reason: strings.TrimSpace(reason), RequestId: strings.TrimSpace(requestID), BeforeRevision: before, AfterRevision: after, OccurredAtUnixMillis: now.UnixMilli()}
}
