// Package commerce 拥有套餐、订单、支付事件、订阅和 EffectiveEntitlement 状态机。
package commerce

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/muxvia/muxvia/cloud/controller/account"
	"github.com/muxvia/muxvia/cloud/controller/control"
	cloudv1 "github.com/muxvia/muxvia/proto/cloud/v1"
)

var (
	// ErrInvalidTransition 表示商业对象不允许当前状态变化。
	ErrInvalidTransition = errors.New("commerce transition is invalid")
	// ErrCommerceConflict 表示 revision、幂等键或支付事件发生冲突。
	ErrCommerceConflict = errors.New("commerce state conflicts")
	// ErrEntitlementUnavailable 表示账号当前没有可用于签发票据的有效能力。
	ErrEntitlementUnavailable = errors.New("effective entitlement is unavailable")
)

// Store 是 commerce 的持久事务边界；每个 mutation 方法必须原子写入审计。
type Store interface {
	ListPlans(context.Context, bool) ([]*cloudv1.PlanDefinition, error)
	CreatePlanVersion(context.Context, *cloudv1.CreatePlanVersionRequest, string, time.Time) (*cloudv1.PlanDefinition, error)
	PublishPlanVersion(context.Context, *cloudv1.PublishPlanVersionRequest, string, time.Time) (*cloudv1.PlanDefinition, error)
	CreateOrder(context.Context, *cloudv1.CreateOrderRequest, string, time.Time) (*cloudv1.CreateOrderResponse, error)
	ApplyPaymentEvent(context.Context, *cloudv1.ApplyPaymentEventRequest, string, time.Time) (*cloudv1.ApplyPaymentEventResponse, error)
	TransitionSubscription(context.Context, *cloudv1.TransitionSubscriptionRequest, string, time.Time) (*cloudv1.TransitionSubscriptionResponse, error)
	GetAccountCommerce(context.Context, string, time.Time) (*cloudv1.GetAccountCommerceResponse, error)
	EffectiveEntitlement(context.Context, string, time.Time) (*cloudv1.EffectiveEntitlement, error)
}

// Config 固定 commerce Store 与时间来源。
type Config struct {
	Store Store
	Now   func() time.Time
}

// Service 是 CommerceService 的应用边界，不持有套餐或 Entitlement 的第二份缓存真值。
type Service struct {
	cloudv1.UnimplementedCommerceServiceServer
	store Store
	now   func() time.Time
}

// EffectiveEntitlement 返回签发票据所需的即时不可变能力投影。
func (service *Service) EffectiveEntitlement(ctx context.Context, accountID string) (*cloudv1.EffectiveEntitlement, error) {
	if strings.TrimSpace(accountID) == "" {
		return nil, ErrEntitlementUnavailable
	}
	return service.store.EffectiveEntitlement(ctx, accountID, service.now().UTC())
}

// New 创建 commerce 服务；Store 缺失时 fail closed。
func New(config Config) (*Service, error) {
	if config.Store == nil {
		return nil, errors.New("commerce store is required")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &Service{store: config.Store, now: config.Now}, nil
}

// ListPlans 返回数据库中的套餐版本；普通账号只能看到已发布版本。
func (service *Service) ListPlans(ctx context.Context, request *cloudv1.ListPlansRequest) (*cloudv1.ListPlansResponse, error) {
	includeUnpublished := request != nil && request.GetIncludeUnpublished()
	if includeUnpublished && !operatorIdentity(ctx) {
		return nil, account.ErrUnauthenticated
	}
	plans, err := service.store.ListPlans(ctx, includeUnpublished)
	return &cloudv1.ListPlansResponse{Plans: plans}, err
}

// CreatePlanVersion 创建 draft 套餐版本；已发布版本不允许原地覆盖。
func (service *Service) CreatePlanVersion(ctx context.Context, request *cloudv1.CreatePlanVersionRequest) (*cloudv1.CreatePlanVersionResponse, error) {
	actorID, ok := operatorActor(ctx, true)
	if !ok || !validPlan(request) {
		return nil, ErrInvalidTransition
	}
	plan, err := service.store.CreatePlanVersion(ctx, request, actorID, service.now().UTC())
	return &cloudv1.CreatePlanVersionResponse{Plan: plan}, err
}

// PublishPlanVersion 使用 revision CAS 发布套餐版本并退休同 plan 的旧发布版本。
func (service *Service) PublishPlanVersion(ctx context.Context, request *cloudv1.PublishPlanVersionRequest) (*cloudv1.PublishPlanVersionResponse, error) {
	actorID, ok := operatorActor(ctx, true)
	if !ok || request == nil || strings.TrimSpace(request.GetPlanId()) == "" || request.GetVersion() == 0 || request.GetExpectedRevision() == 0 {
		return nil, ErrInvalidTransition
	}
	plan, err := service.store.PublishPlanVersion(ctx, request, actorID, service.now().UTC())
	return &cloudv1.PublishPlanVersionResponse{Plan: plan}, err
}

// CreateOrder 创建 pending order 和 payment attempt；普通账号只能为自己创建。
func (service *Service) CreateOrder(ctx context.Context, request *cloudv1.CreateOrderRequest) (*cloudv1.CreateOrderResponse, error) {
	actor, ok := account.IdentityFromContext(ctx)
	if !ok || request == nil || strings.TrimSpace(request.GetIdempotencyKey()) == "" || strings.TrimSpace(request.GetProvider()) == "" || !paidTransition(request.GetRequestedTransition()) {
		return nil, ErrInvalidTransition
	}
	if request.GetAccountId() != actor.Account.GetAccountId() && !actor.HasRole(cloudv1.AccountRole_ACCOUNT_ROLE_OPERATOR) {
		return nil, account.ErrUnauthenticated
	}
	return service.store.CreateOrder(ctx, request, actor.Account.GetAccountId(), service.now().UTC())
}

// ApplyPaymentEvent 只接受已经由 provider adapter 验签并归一化的事件。
func (service *Service) ApplyPaymentEvent(ctx context.Context, request *cloudv1.ApplyPaymentEventRequest) (*cloudv1.ApplyPaymentEventResponse, error) {
	actorID, ok := operatorActor(ctx, true)
	if !ok || !validPaymentEvent(request) {
		return nil, ErrInvalidTransition
	}
	return service.store.ApplyPaymentEvent(ctx, request, actorID, service.now().UTC())
}

// TransitionSubscription 执行无需外部支付的显式运营状态变化。
func (service *Service) TransitionSubscription(ctx context.Context, request *cloudv1.TransitionSubscriptionRequest) (*cloudv1.TransitionSubscriptionResponse, error) {
	actorID, ok := operatorActor(ctx, true)
	if !ok || request == nil || strings.TrimSpace(request.GetAccountId()) == "" || strings.TrimSpace(request.GetReason()) == "" || request.GetExpectedRevision() == 0 || !operatorTransition(request.GetTransition()) {
		return nil, ErrInvalidTransition
	}
	return service.store.TransitionSubscription(ctx, request, actorID, service.now().UTC())
}

// GetAccountCommerce 返回账号当前商业聚合；普通账号只能读取自己。
func (service *Service) GetAccountCommerce(ctx context.Context, request *cloudv1.GetAccountCommerceRequest) (*cloudv1.GetAccountCommerceResponse, error) {
	identity, ok := account.IdentityFromContext(ctx)
	if !ok || request == nil || request.GetAccountId() == "" {
		return nil, account.ErrUnauthenticated
	}
	if request.GetAccountId() != identity.Account.GetAccountId() && !identity.HasRole(cloudv1.AccountRole_ACCOUNT_ROLE_OPERATOR) {
		return nil, account.ErrUnauthenticated
	}
	return service.store.GetAccountCommerce(ctx, request.GetAccountId(), service.now().UTC())
}

// EntitlementRelayPolicy 把 commerce truth 投影为 RelayLease 的窄执行上限。
type EntitlementRelayPolicy struct{ Service *Service }

// Limits 从数据库读取当前 EffectiveEntitlement；读取失败或额度耗尽时拒绝新 Relay。
func (policy EntitlementRelayPolicy) Limits(ctx context.Context, session *cloudv1.ClientSessionSummary) (control.RelayLimits, error) {
	if policy.Service == nil || session == nil || session.GetAccountId() == "" {
		return control.RelayLimits{}, ErrEntitlementUnavailable
	}
	entitlement, err := policy.Service.store.EffectiveEntitlement(ctx, session.GetAccountId(), policy.Service.now().UTC())
	if err != nil || entitlement.GetState() != cloudv1.EntitlementState_ENTITLEMENT_STATE_ACTIVE || !entitlement.GetCapability().GetRelayEnabled() || entitlement.GetRelayRemainingBytes() == 0 {
		return control.RelayLimits{}, ErrEntitlementUnavailable
	}
	maxBytes := entitlement.GetCapability().GetRelayMaxBytesPerLease()
	if maxBytes > entitlement.GetRelayRemainingBytes() {
		maxBytes = entitlement.GetRelayRemainingBytes()
	}
	return control.RelayLimits{MaxBytes: maxBytes, MaxRateBytesPerSecond: entitlement.GetCapability().GetRelayMaxRateBytesPerSecond(), MaxConcurrentAllocations: entitlement.GetCapability().GetRelayMaxConcurrency()}, nil
}

func operatorIdentity(ctx context.Context) bool {
	_, ok := operatorActor(ctx, false)
	return ok
}

func operatorActor(ctx context.Context, requireRecent bool) (string, bool) {
	identity, ok := account.IdentityFromContext(ctx)
	if !ok || !identity.HasRole(cloudv1.AccountRole_ACCOUNT_ROLE_OPERATOR) || requireRecent && !time.Now().UTC().Before(identity.RecentAuthExpiresAt) {
		return "", false
	}
	return identity.Account.GetAccountId(), true
}

func validPlan(request *cloudv1.CreatePlanVersionRequest) bool {
	if request == nil || strings.TrimSpace(request.GetPlanId()) == "" || strings.TrimSpace(request.GetName()) == "" || request.GetBillingPeriodDays() == 0 || request.GetCapability() == nil || request.GetMonthlyPrice() == nil || request.GetYearlyPrice() == nil {
		return false
	}
	capability := request.GetCapability()
	return capability.GetManagedP2PEnabled() && capability.GetManagedP2PMaxConcurrency() > 0 && capability.GetCloudDaemonLimit() > 0 && (!capability.GetRelayEnabled() || capability.GetRelayMaxConcurrency() > 0 && capability.GetRelayMaxBytesPerPeriod() > 0 && capability.GetRelayMaxBytesPerLease() > 0 && capability.GetRelayMaxRateBytesPerSecond() > 0)
}

func paidTransition(value cloudv1.SubscriptionTransition) bool {
	return value >= cloudv1.SubscriptionTransition_SUBSCRIPTION_TRANSITION_ACTIVATE && value <= cloudv1.SubscriptionTransition_SUBSCRIPTION_TRANSITION_DOWNGRADE
}

func operatorTransition(value cloudv1.SubscriptionTransition) bool {
	return value == cloudv1.SubscriptionTransition_SUBSCRIPTION_TRANSITION_CANCEL_AT_PERIOD_END || value == cloudv1.SubscriptionTransition_SUBSCRIPTION_TRANSITION_RESUME || value == cloudv1.SubscriptionTransition_SUBSCRIPTION_TRANSITION_SUSPEND || value == cloudv1.SubscriptionTransition_SUBSCRIPTION_TRANSITION_RESTORE || value == cloudv1.SubscriptionTransition_SUBSCRIPTION_TRANSITION_EXPIRE || value == cloudv1.SubscriptionTransition_SUBSCRIPTION_TRANSITION_REVOKE
}

func validPaymentEvent(request *cloudv1.ApplyPaymentEventRequest) bool {
	return request != nil && strings.TrimSpace(request.GetProvider()) != "" && strings.TrimSpace(request.GetProviderEventId()) != "" && strings.TrimSpace(request.GetPaymentAttemptId()) != "" && strings.TrimSpace(request.GetOrderId()) != "" && request.GetOccurredAt() != nil && request.GetOccurredAt().IsValid() && request.GetEventType() >= cloudv1.PaymentEventType_PAYMENT_EVENT_TYPE_SUCCEEDED && request.GetEventType() <= cloudv1.PaymentEventType_PAYMENT_EVENT_TYPE_CHARGEBACK
}
