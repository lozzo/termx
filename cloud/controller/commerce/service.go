// Package commerce 拥有套餐、订单、支付事件、订阅和 EffectiveEntitlement 状态机。
package commerce

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/anytty/anytty/cloud/controller/account"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
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
	Store               Store
	Now                 func() time.Time
	DevelopmentPayments bool
}

// Service 是 CommerceService 的应用边界，不持有套餐或 Entitlement 的第二份缓存真值。
type Service struct {
	cloudv1.UnimplementedCommerceServiceServer
	store               Store
	now                 func() time.Time
	developmentPayments bool
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
	return &Service{store: config.Store, now: config.Now, developmentPayments: config.DevelopmentPayments}, nil
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

// CreateMyOrder 从认证 session 派生账号和 Development provider，浏览器不能替用户选择这两个安全字段。
func (service *Service) CreateMyOrder(ctx context.Context, request *cloudv1.CreateMyOrderRequest) (*cloudv1.CreateOrderResponse, error) {
	identity, ok := account.IdentityFromContext(ctx)
	if !ok || !service.developmentPayments || request == nil {
		return nil, ErrInvalidTransition
	}
	return service.CreateOrder(ctx, &cloudv1.CreateOrderRequest{
		AccountId: identity.Account.GetAccountId(), PlanId: request.GetPlanId(), PlanVersion: request.GetPlanVersion(),
		Provider: "development", IdempotencyKey: request.GetIdempotencyKey(), RequestedTransition: request.GetRequestedTransition(), Yearly: request.GetYearly(),
	})
}

// GetMyCommerce 返回认证账号自己的商业聚合，不接受浏览器提供 account_id。
func (service *Service) GetMyCommerce(ctx context.Context, _ *cloudv1.GetMyCommerceRequest) (*cloudv1.GetAccountCommerceResponse, error) {
	identity, ok := account.IdentityFromContext(ctx)
	if !ok {
		return nil, account.ErrUnauthenticated
	}
	return service.store.GetAccountCommerce(ctx, identity.Account.GetAccountId(), service.now().UTC())
}

// ChangeMySubscription 只允许用户执行到期取消和恢复；付费换档仍必须创建订单。
func (service *Service) ChangeMySubscription(ctx context.Context, request *cloudv1.ChangeMySubscriptionRequest) (*cloudv1.TransitionSubscriptionResponse, error) {
	identity, ok := account.IdentityFromContext(ctx)
	if !ok || request == nil || request.GetExpectedRevision() == 0 || (request.GetTransition() != cloudv1.SubscriptionTransition_SUBSCRIPTION_TRANSITION_CANCEL_AT_PERIOD_END && request.GetTransition() != cloudv1.SubscriptionTransition_SUBSCRIPTION_TRANSITION_RESUME) {
		return nil, ErrInvalidTransition
	}
	reason := "用户自助取消到期"
	if request.GetTransition() == cloudv1.SubscriptionTransition_SUBSCRIPTION_TRANSITION_RESUME {
		reason = "用户自助恢复订阅"
	}
	return service.store.TransitionSubscription(ctx, &cloudv1.TransitionSubscriptionRequest{AccountId: identity.Account.GetAccountId(), Transition: request.GetTransition(), ExpectedRevision: request.GetExpectedRevision(), Reason: reason}, identity.Account.GetAccountId(), service.now().UTC())
}

// CompleteDevelopmentPayment 为显式 Development 环境提供可重复验收的账号隔离支付适配器。
func (service *Service) CompleteDevelopmentPayment(ctx context.Context, request *cloudv1.CompleteDevelopmentPaymentRequest) (*cloudv1.ApplyPaymentEventResponse, error) {
	identity, ok := account.IdentityFromContext(ctx)
	if !ok || !service.developmentPayments || request == nil || strings.TrimSpace(request.GetOrderId()) == "" || strings.TrimSpace(request.GetPaymentAttemptId()) == "" {
		return nil, ErrInvalidTransition
	}
	aggregate, err := service.store.GetAccountCommerce(ctx, identity.Account.GetAccountId(), service.now().UTC())
	if err != nil {
		return nil, err
	}
	var order *cloudv1.OrderProjection
	for _, candidate := range aggregate.GetOrders() {
		if candidate.GetOrderId() == request.GetOrderId() {
			order = candidate
			break
		}
	}
	var attempt *cloudv1.PaymentAttemptProjection
	for _, candidate := range aggregate.GetPaymentAttempts() {
		if candidate.GetPaymentAttemptId() == request.GetPaymentAttemptId() && candidate.GetOrderId() == request.GetOrderId() {
			attempt = candidate
			break
		}
	}
	if order == nil || attempt == nil || order.GetProvider() != "development" || attempt.GetProvider() != "development" {
		return nil, account.ErrUnauthenticated
	}
	// 已结算状态是 Development 支付重试的持久真值；不能用新的 occurred_at 重建同一 provider event。
	if order.GetStatus() == cloudv1.OrderStatus_ORDER_STATUS_PAID && attempt.GetStatus() == cloudv1.PaymentAttemptStatus_PAYMENT_ATTEMPT_STATUS_SUCCEEDED {
		return &cloudv1.ApplyPaymentEventResponse{
			Order: order, PaymentAttempt: attempt, Subscription: aggregate.GetSubscription(), Entitlement: aggregate.GetEntitlement(), Duplicate: true,
		}, nil
	}
	now := service.now().UTC()
	event := &cloudv1.ApplyPaymentEventRequest{
		Provider: "development", ProviderEventId: fmt.Sprintf("development:%s:succeeded", attempt.GetPaymentAttemptId()),
		PaymentAttemptId: attempt.GetPaymentAttemptId(), OrderId: order.GetOrderId(),
		EventType: cloudv1.PaymentEventType_PAYMENT_EVENT_TYPE_SUCCEEDED, ProviderReference: "development-confirmed", OccurredAt: timestamppb.New(now),
	}
	return service.store.ApplyPaymentEvent(ctx, event, identity.Account.GetAccountId(), now)
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
	return capability.GetManagedP2PEnabled() && capability.GetCloudDaemonLimit() > 0 && (!capability.GetRelayEnabled() || capability.GetRelayMaxConcurrency() > 0 && capability.GetRelayMaxBytesPerPeriod() > 0 && capability.GetRelayMaxBytesPerLease() > 0 && capability.GetRelayMaxRateBytesPerSecond() > 0)
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
