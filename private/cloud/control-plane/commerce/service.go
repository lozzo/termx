// Package commerce 拥有 Cloud 账号 session、订单、provider event 与 Subscription 状态机。
//
// 所有公开输入输出都使用 generated cloudpb。Store 只持久化内部 credential hash 与经过验证的
// Proto projection；测试 provider 也必须先生成 NormalizedPaymentEvent，不能直接写 Entitlement。
package commerce

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"strings"
	"time"

	"github.com/lozzow/termx/private/cloud/control-plane/entitlement"
	"github.com/lozzow/termx/proto/cloudpb"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/protobuf/proto"
)

var (
	// ErrUnauthorized 表示 credential 无效、过期或已被轮换。
	ErrUnauthorized = errors.New("commerce credential is unauthorized")
	// ErrConflict 表示账号、订单、event replay 或 Subscription transition 与当前状态冲突。
	ErrConflict = errors.New("commerce state conflict")
	// ErrNotFound 表示请求的商业资源不存在。
	ErrNotFound = errors.New("commerce resource not found")
)

// AccountRecord 是 Store 持久化的账号 projection 与密码 verifier。
type AccountRecord struct {
	Projection   *cloudpb.AccountProjection
	PasswordHash []byte
}

// SessionRecord 只保存 token hash；原始 access/refresh token 只存在于创建响应。
type SessionRecord struct {
	SessionID        string
	AccountID        string
	AccessTokenHash  [sha256.Size]byte
	RefreshTokenHash [sha256.Size]byte
	AccessExpiresAt  time.Time
	RefreshExpiresAt time.Time
	Revision         uint64
	Revoked          bool
}

// PaymentEventRecord 是 durable provider journal 的当前处理状态。
type PaymentEventRecord struct {
	Event  *cloudpb.NormalizedPaymentEvent
	Digest [sha256.Size]byte
	State  cloudpb.PaymentEventState
	Result *cloudpb.ApplyPaymentEventResponse
}

// Store 是 commerce 状态机所需的持久事务边界。
type Store interface {
	CreateAccount(context.Context, AccountRecord, SessionRecord, *cloudpb.SubscriptionProjection, *cloudpb.EntitlementProjection, *cloudpb.CommerceAuditProjection) error
	AccountByEmail(context.Context, string) (AccountRecord, error)
	Account(context.Context, string) (AccountRecord, error)
	PutSession(context.Context, SessionRecord, *cloudpb.CommerceAuditProjection) error
	SessionByAccessHash(context.Context, [sha256.Size]byte) (SessionRecord, error)
	SessionByRefreshHash(context.Context, [sha256.Size]byte) (SessionRecord, error)
	RotateSession(context.Context, string, SessionRecord, *cloudpb.CommerceAuditProjection) error
	RevokeSession(context.Context, string, string, bool, *cloudpb.CommerceAuditProjection) error
	ChangePassword(context.Context, AccountRecord, SessionRecord, *cloudpb.CommerceAuditProjection) error
	CreateOrder(context.Context, *cloudpb.OrderProjection, *cloudpb.CommerceAuditProjection) error
	Order(context.Context, string) (*cloudpb.OrderProjection, error)
	CreatePaymentAttempt(context.Context, *cloudpb.PaymentAttemptProjection, *cloudpb.CommerceAuditProjection) error
	PaymentAttempt(context.Context, string) (*cloudpb.PaymentAttemptProjection, error)
	RecordPaymentEvent(context.Context, PaymentEventRecord) (PaymentEventRecord, bool, error)
	RejectPaymentEvent(context.Context, string, *cloudpb.CommerceAuditProjection) error
	CommitPaymentEvent(context.Context, string, *cloudpb.ApplyPaymentEventResponse, *cloudpb.EntitlementProjection, *cloudpb.CommerceAuditProjection) error
	CommitSubscriptionTransition(context.Context, *cloudpb.SubscriptionProjection, *cloudpb.EntitlementProjection, *cloudpb.CommerceAuditProjection) error
	Subscription(context.Context, string) (*cloudpb.SubscriptionProjection, error)
	Entitlement(context.Context, string) (*cloudpb.EntitlementProjection, error)
	Orders(context.Context, string) ([]*cloudpb.OrderProjection, error)
	PaymentAttempts(context.Context, string) ([]*cloudpb.PaymentAttemptProjection, error)
	Audit(context.Context, string) ([]*cloudpb.CommerceAuditProjection, error)
}

// Config 固定 commerce Store、versioned catalog、时间与随机来源。
type Config struct {
	Store   Store
	Catalog *cloudpb.PlanCatalogContract
	Now     func() time.Time
	Random  io.Reader
}

// Service 是 Controller 内账号、交易与 Subscription 的应用边界。
type Service struct {
	store             Store
	catalog           *cloudpb.PlanCatalogContract
	now               func() time.Time
	random            io.Reader
	dummyPasswordHash []byte
}

// New 创建 commerce service；catalog 或 Store 缺失时 fail closed。
func New(config Config) (*Service, error) {
	if config.Store == nil || config.Catalog == nil || config.Catalog.GetCatalogVersion() == 0 || len(config.Catalog.GetPlans()) == 0 {
		return nil, fmt.Errorf("commerce store and catalog are required")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Random == nil {
		config.Random = rand.Reader
	}
	dummyPasswordHash, err := bcrypt.GenerateFromPassword([]byte("termx-invalid-password"), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	return &Service{store: config.Store, catalog: proto.Clone(config.Catalog).(*cloudpb.PlanCatalogContract), now: config.Now, random: config.Random, dummyPasswordHash: dummyPasswordHash}, nil
}

// Register 创建账号、首个 session、included Subscription 与 Entitlement。
func (service *Service) Register(ctx context.Context, request *cloudpb.RegisterAccountRequest) (*cloudpb.RegisterAccountResponse, error) {
	if request == nil {
		return nil, ErrConflict
	}
	email := normalizeEmail(request.GetEmail())
	if !validEmail(email) || len(request.GetPassword()) < 8 {
		return nil, ErrConflict
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(request.GetPassword()), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	now := service.now().UTC()
	accountID, err := service.randomID("account")
	if err != nil {
		return nil, err
	}
	userID, err := service.randomID("user")
	if err != nil {
		return nil, err
	}
	account := &cloudpb.AccountProjection{AccountId: accountID, UserId: userID, Email: email, DisplayName: emailDisplayName(email), AuthRevision: 1, CreatedAtUnixMillis: now.UnixMilli()}
	credential, session, err := service.newSession(account, 1, now)
	if err != nil {
		return nil, err
	}
	plan, ok := includedPlan(service.catalog)
	if !ok {
		return nil, ErrConflict
	}
	subscription := initialSubscription(accountID, plan, now)
	entitlementProjection, err := normalizeEntitlement(subscription, plan, now)
	if err != nil {
		return nil, err
	}
	audit := service.audit(accountID, userID, "account.register", accountID, now)
	if err := service.store.CreateAccount(ctx, AccountRecord{Projection: account, PasswordHash: passwordHash}, session, subscription, entitlementProjection, audit); err != nil {
		return nil, err
	}
	return &cloudpb.RegisterAccountResponse{Session: credential}, nil
}

// Login 验证密码并创建新的可轮换账号 session。
func (service *Service) Login(ctx context.Context, request *cloudpb.PasswordLoginRequest) (*cloudpb.PasswordLoginResponse, error) {
	if request == nil {
		return nil, ErrUnauthorized
	}
	account, err := service.store.AccountByEmail(ctx, normalizeEmail(request.GetEmail()))
	passwordHash := service.dummyPasswordHash
	if err == nil {
		passwordHash = account.PasswordHash
	}
	if bcrypt.CompareHashAndPassword(passwordHash, []byte(request.GetPassword())) != nil || err != nil {
		return nil, ErrUnauthorized
	}
	now := service.now().UTC()
	credential, session, err := service.newSession(account.Projection, 1, now)
	if err != nil {
		return nil, err
	}
	if err := service.store.PutSession(ctx, session, service.audit(account.Projection.GetAccountId(), account.Projection.GetUserId(), "session.login", session.SessionID, now)); err != nil {
		return nil, err
	}
	return &cloudpb.PasswordLoginResponse{Session: credential}, nil
}

// AuthenticateAccess 验证短期 access token，并返回当前账号 projection。
func (service *Service) AuthenticateAccess(ctx context.Context, accessToken []byte) (*cloudpb.AccountProjection, string, error) {
	if len(accessToken) == 0 {
		return nil, "", ErrUnauthorized
	}
	record, err := service.store.SessionByAccessHash(ctx, sha256.Sum256(accessToken))
	if err != nil || record.Revoked || !service.now().UTC().Before(record.AccessExpiresAt) {
		return nil, "", ErrUnauthorized
	}
	account, err := service.store.Account(ctx, record.AccountID)
	if err != nil {
		return nil, "", ErrUnauthorized
	}
	return proto.Clone(account.Projection).(*cloudpb.AccountProjection), record.SessionID, nil
}

// Refresh 单次消费 refresh token，撤销旧 session 并返回新 token pair。
func (service *Service) Refresh(ctx context.Context, request *cloudpb.RefreshAccountSessionRequest) (*cloudpb.RefreshAccountSessionResponse, error) {
	if request == nil || len(request.GetRefreshToken()) == 0 {
		return nil, ErrUnauthorized
	}
	old, err := service.store.SessionByRefreshHash(ctx, sha256.Sum256(request.GetRefreshToken()))
	if err != nil || old.Revoked || !service.now().UTC().Before(old.RefreshExpiresAt) {
		return nil, ErrUnauthorized
	}
	account, err := service.store.Account(ctx, old.AccountID)
	if err != nil {
		return nil, ErrUnauthorized
	}
	now := service.now().UTC()
	credential, next, err := service.newSession(account.Projection, old.Revision+1, now)
	if err != nil {
		return nil, err
	}
	if err := service.store.RotateSession(ctx, old.SessionID, next, service.audit(old.AccountID, account.Projection.GetUserId(), "session.refresh", next.SessionID, now)); err != nil {
		return nil, err
	}
	return &cloudpb.RefreshAccountSessionResponse{Session: credential}, nil
}

// Logout 撤销精确 session 或当前账号的全部 session。
func (service *Service) Logout(ctx context.Context, accountID, actorID string, request *cloudpb.LogoutAccountSessionRequest) (*cloudpb.LogoutAccountSessionResponse, error) {
	if request == nil || accountID == "" || actorID == "" || !request.GetAllAccountSessions() && request.GetSessionId() == "" {
		return nil, ErrUnauthorized
	}
	now := service.now().UTC()
	if err := service.store.RevokeSession(ctx, accountID, request.GetSessionId(), request.GetAllAccountSessions(), service.audit(accountID, actorID, "session.logout", request.GetSessionId(), now)); err != nil {
		return nil, err
	}
	return &cloudpb.LogoutAccountSessionResponse{}, nil
}

// ChangePassword 验证旧密码后替换 verifier，递增 auth revision，并撤销全部旧 session。
func (service *Service) ChangePassword(ctx context.Context, accountID string, request *cloudpb.ChangeAccountPasswordRequest) (*cloudpb.ChangeAccountPasswordResponse, error) {
	if request == nil || accountID == "" || len(request.GetNewPassword()) < 8 {
		return nil, ErrUnauthorized
	}
	account, err := service.store.Account(ctx, accountID)
	if err != nil || bcrypt.CompareHashAndPassword(account.PasswordHash, []byte(request.GetCurrentPassword())) != nil {
		return nil, ErrUnauthorized
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(request.GetNewPassword()), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	now := service.now().UTC()
	projection := proto.Clone(account.Projection).(*cloudpb.AccountProjection)
	projection.AuthRevision++
	credential, session, err := service.newSession(projection, 1, now)
	if err != nil {
		return nil, err
	}
	if err := service.store.ChangePassword(ctx, AccountRecord{Projection: projection, PasswordHash: passwordHash}, session, service.audit(accountID, projection.GetUserId(), "account.password_changed", accountID, now)); err != nil {
		return nil, err
	}
	return &cloudpb.ChangeAccountPasswordResponse{Session: credential}, nil
}

// CreateCheckout 创建 pending order；included plan 或非法 transition 不修改 Subscription。
func (service *Service) CreateCheckout(ctx context.Context, accountID, actorID string, request *cloudpb.CreateCheckoutRequest) (*cloudpb.CreateCheckoutResponse, error) {
	if request == nil {
		return nil, ErrConflict
	}
	plan, ok := planByID(service.catalog, request.GetPlanId(), 0)
	transition := request.GetRequestedTransition()
	if accountID == "" || actorID == "" || !ok || plan.GetIncluded() || plan.GetPrice().GetMode() == cloudpb.CatalogPriceMode_CATALOG_PRICE_MODE_UNSPECIFIED || transition < cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_ACTIVATE || transition > cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_DOWNGRADE {
		return nil, ErrConflict
	}
	current, currentErr := service.store.Subscription(ctx, accountID)
	if transition == cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_ACTIVATE && currentErr == nil && current.GetStatus() != cloudpb.SubscriptionStatus_SUBSCRIPTION_STATUS_EXPIRED && current.GetStatus() != cloudpb.SubscriptionStatus_SUBSCRIPTION_STATUS_CANCELED {
		return nil, ErrConflict
	}
	if transition == cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_RENEW && (currentErr != nil || current.GetPlanId() != plan.GetPlanId()) {
		return nil, ErrConflict
	}
	if transition == cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_UPGRADE || transition == cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_DOWNGRADE {
		if currentErr != nil || current.GetPlanId() == plan.GetPlanId() {
			return nil, ErrConflict
		}
	}
	now := service.now().UTC()
	orderID, err := service.randomID("order")
	if err != nil {
		return nil, err
	}
	order := &cloudpb.OrderProjection{OrderId: orderID, AccountId: accountID, PlanId: plan.GetPlanId(), PlanVersion: plan.GetPlanVersion(), Status: cloudpb.OrderStatus_ORDER_STATUS_PENDING, CreatedAtUnixMillis: now.UnixMilli(), Revision: 1, RequestedTransition: transition, Price: clonePrice(plan.GetPrice())}
	if currentErr == nil {
		order.SourceSubscriptionRevision = current.GetRevision()
		order.SourcePlanId = current.GetPlanId()
		order.SourcePlanVersion = current.GetPlanVersion()
	}
	if err := service.store.CreateOrder(ctx, order, service.audit(accountID, actorID, "order.created", orderID, now)); err != nil {
		return nil, err
	}
	return &cloudpb.CreateCheckoutResponse{Order: proto.Clone(order).(*cloudpb.OrderProjection)}, nil
}

// CreatePaymentAttempt 为 pending order 创建一次持久 provider 尝试，不修改 Subscription。
func (service *Service) CreatePaymentAttempt(ctx context.Context, accountID, actorID string, request *cloudpb.CreatePaymentAttemptRequest) (*cloudpb.CreatePaymentAttemptResponse, error) {
	if request == nil || accountID == "" || actorID == "" || request.GetOrderId() == "" || request.GetProvider() == "" {
		return nil, ErrConflict
	}
	order, err := service.store.Order(ctx, request.GetOrderId())
	if err != nil || order.GetAccountId() != accountID || order.GetStatus() != cloudpb.OrderStatus_ORDER_STATUS_PENDING {
		return nil, ErrConflict
	}
	now := service.now().UTC()
	attemptID, err := service.randomID("payment-attempt")
	if err != nil {
		return nil, err
	}
	attempt := &cloudpb.PaymentAttemptProjection{PaymentAttemptId: attemptID, OrderId: order.GetOrderId(), AccountId: accountID, Provider: request.GetProvider(), Status: cloudpb.PaymentAttemptStatus_PAYMENT_ATTEMPT_STATUS_PENDING, CreatedAtUnixMillis: now.UnixMilli(), UpdatedAtUnixMillis: now.UnixMilli(), Revision: 1}
	if err := service.store.CreatePaymentAttempt(ctx, attempt, service.audit(accountID, actorID, "payment_attempt.created", attemptID, now)); err != nil {
		return nil, err
	}
	return &cloudpb.CreatePaymentAttemptResponse{PaymentAttempt: proto.Clone(attempt).(*cloudpb.PaymentAttemptProjection)}, nil
}

// ConfirmTestPayment 生成 development provider event，再走与正式 provider 相同的 ApplyPaymentEvent。
func (service *Service) ConfirmTestPayment(ctx context.Context, accountID string, request *cloudpb.ConfirmTestPaymentRequest) (*cloudpb.ConfirmTestPaymentResponse, error) {
	if request == nil {
		return nil, ErrConflict
	}
	order, err := service.store.Order(ctx, request.GetOrderId())
	if err != nil || order.GetAccountId() != accountID || request.GetEventType() == cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_UNSPECIFIED {
		return nil, ErrConflict
	}
	now := service.now().UTC()
	var attempt *cloudpb.PaymentAttemptProjection
	if request.GetEventType() == cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_SUCCEEDED || request.GetEventType() == cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_FAILED {
		attemptResponse, attemptErr := service.CreatePaymentAttempt(ctx, accountID, accountID, &cloudpb.CreatePaymentAttemptRequest{OrderId: order.GetOrderId(), Provider: "termx-test-provider"})
		if attemptErr != nil {
			return nil, attemptErr
		}
		attempt = attemptResponse.GetPaymentAttempt()
	} else {
		attempts, attemptErr := service.store.PaymentAttempts(ctx, accountID)
		if attemptErr != nil {
			return nil, attemptErr
		}
		for _, candidate := range attempts {
			if candidate.GetOrderId() == order.GetOrderId() && candidate.GetProvider() == "termx-test-provider" && candidate.GetStatus() == cloudpb.PaymentAttemptStatus_PAYMENT_ATTEMPT_STATUS_SUCCEEDED {
				if attempt != nil {
					return nil, ErrConflict
				}
				attempt = candidate
			}
		}
		if attempt == nil {
			return nil, ErrConflict
		}
	}
	eventID, err := service.randomID("test-event")
	if err != nil {
		return nil, err
	}
	result, err := service.ApplyPaymentEvent(ctx, &cloudpb.ApplyPaymentEventRequest{Event: &cloudpb.NormalizedPaymentEvent{ProviderEventId: eventID, Provider: "termx-test-provider", EventType: request.GetEventType(), OrderId: order.GetOrderId(), AccountId: order.GetAccountId(), PlanId: order.GetPlanId(), PlanVersion: order.GetPlanVersion(), ProviderReference: "test-" + order.GetOrderId(), OccurredAtUnixMillis: now.UnixMilli(), PaymentAttemptId: attempt.GetPaymentAttemptId()}})
	if err != nil {
		return nil, err
	}
	return &cloudpb.ConfirmTestPaymentResponse{Result: result}, nil
}

// ApplyPaymentEvent 先持久记录 normalized event，再提交订单、Subscription、Entitlement 与审计。
func (service *Service) ApplyPaymentEvent(ctx context.Context, request *cloudpb.ApplyPaymentEventRequest) (*cloudpb.ApplyPaymentEventResponse, error) {
	if request == nil {
		return nil, ErrConflict
	}
	event := request.GetEvent()
	if !validPaymentEvent(event) {
		return nil, ErrConflict
	}
	payload, _ := proto.MarshalOptions{Deterministic: true}.Marshal(event)
	record := PaymentEventRecord{Event: proto.Clone(event).(*cloudpb.NormalizedPaymentEvent), Digest: sha256.Sum256(payload), State: cloudpb.PaymentEventState_PAYMENT_EVENT_STATE_RECEIVED}
	stored, existing, err := service.store.RecordPaymentEvent(ctx, record)
	if err != nil {
		return nil, err
	}
	if existing && stored.Digest != record.Digest {
		return nil, ErrConflict
	}
	if stored.State == cloudpb.PaymentEventState_PAYMENT_EVENT_STATE_APPLIED && stored.Result != nil {
		return proto.Clone(stored.Result).(*cloudpb.ApplyPaymentEventResponse), nil
	}
	order, err := service.store.Order(ctx, event.GetOrderId())
	if err != nil || order.GetAccountId() != event.GetAccountId() || order.GetPlanId() != event.GetPlanId() || order.GetPlanVersion() != event.GetPlanVersion() {
		service.rejectPaymentEvent(ctx, event, "payment.rejected_order")
		return nil, ErrConflict
	}
	attempt, err := service.store.PaymentAttempt(ctx, event.GetPaymentAttemptId())
	if err != nil || attempt.GetOrderId() != order.GetOrderId() || attempt.GetAccountId() != order.GetAccountId() || attempt.GetProvider() != event.GetProvider() {
		service.rejectPaymentEvent(ctx, event, "payment.rejected_attempt")
		return nil, ErrConflict
	}
	now := service.now().UTC()
	plan, ok := planByID(service.catalog, order.GetPlanId(), order.GetPlanVersion())
	if !ok {
		service.rejectPaymentEvent(ctx, event, "payment.rejected_plan")
		return nil, ErrConflict
	}
	current, _ := service.store.Subscription(ctx, order.GetAccountId())
	if current == nil {
		service.rejectPaymentEvent(ctx, event, "payment.rejected_stale_order")
		return nil, ErrConflict
	}
	if (event.GetEventType() == cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_SUCCEEDED || event.GetEventType() == cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_FAILED) &&
		(current.GetRevision() != order.GetSourceSubscriptionRevision() || current.GetPlanId() != order.GetSourcePlanId() || current.GetPlanVersion() != order.GetSourcePlanVersion()) {
		service.rejectPaymentEvent(ctx, event, "payment.rejected_stale_order")
		return nil, ErrConflict
	}
	updatedOrder := proto.Clone(order).(*cloudpb.OrderProjection)
	updatedOrder.ProviderReference = event.GetProviderReference()
	updatedOrder.SettledAtUnixMillis = now.UnixMilli()
	updatedOrder.Revision++
	updatedAttempt := proto.Clone(attempt).(*cloudpb.PaymentAttemptProjection)
	transition := order.GetRequestedTransition()
	var subscription *cloudpb.SubscriptionProjection
	switch event.GetEventType() {
	case cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_SUCCEEDED:
		if order.GetStatus() != cloudpb.OrderStatus_ORDER_STATUS_PENDING || attempt.GetStatus() != cloudpb.PaymentAttemptStatus_PAYMENT_ATTEMPT_STATUS_PENDING {
			service.rejectPaymentEvent(ctx, event, "payment.rejected_order_state")
			return nil, ErrConflict
		}
		updatedOrder.Status = cloudpb.OrderStatus_ORDER_STATUS_PAID
		updatedAttempt.ProviderReference = event.GetProviderReference()
		updatedAttempt.UpdatedAtUnixMillis = now.UnixMilli()
		updatedAttempt.Status = cloudpb.PaymentAttemptStatus_PAYMENT_ATTEMPT_STATUS_SUCCEEDED
		updatedAttempt.Revision++
		subscription = paidSubscription(current, updatedOrder, plan, transition, now)
	case cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_FAILED:
		if order.GetStatus() != cloudpb.OrderStatus_ORDER_STATUS_PENDING || attempt.GetStatus() != cloudpb.PaymentAttemptStatus_PAYMENT_ATTEMPT_STATUS_PENDING {
			service.rejectPaymentEvent(ctx, event, "payment.rejected_order_state")
			return nil, ErrConflict
		}
		updatedOrder.Status = cloudpb.OrderStatus_ORDER_STATUS_PAYMENT_FAILED
		updatedAttempt.ProviderReference = event.GetProviderReference()
		updatedAttempt.UpdatedAtUnixMillis = now.UnixMilli()
		updatedAttempt.Status = cloudpb.PaymentAttemptStatus_PAYMENT_ATTEMPT_STATUS_FAILED
		updatedAttempt.Revision++
		subscription = transitionSubscription(current, cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_PAYMENT_FAILED, nil, now)
	case cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_REFUNDED:
		if order.GetStatus() != cloudpb.OrderStatus_ORDER_STATUS_PAID || current.GetSourceOrderId() != order.GetOrderId() || attempt.GetStatus() != cloudpb.PaymentAttemptStatus_PAYMENT_ATTEMPT_STATUS_SUCCEEDED {
			service.rejectPaymentEvent(ctx, event, "payment.rejected_order_state")
			return nil, ErrConflict
		}
		updatedOrder.Status = cloudpb.OrderStatus_ORDER_STATUS_REFUNDED
		subscription = transitionSubscription(current, cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_REFUND, nil, now)
	case cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_REVOKED:
		if (order.GetStatus() != cloudpb.OrderStatus_ORDER_STATUS_PAID && order.GetStatus() != cloudpb.OrderStatus_ORDER_STATUS_REFUNDED) || current.GetSourceOrderId() != order.GetOrderId() || attempt.GetStatus() != cloudpb.PaymentAttemptStatus_PAYMENT_ATTEMPT_STATUS_SUCCEEDED {
			service.rejectPaymentEvent(ctx, event, "payment.rejected_order_state")
			return nil, ErrConflict
		}
		updatedOrder.Status = cloudpb.OrderStatus_ORDER_STATUS_REVOKED
		subscription = transitionSubscription(current, cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_REVOKE, nil, now)
	case cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_CHARGEBACK:
		if order.GetStatus() != cloudpb.OrderStatus_ORDER_STATUS_PAID || current.GetSourceOrderId() != order.GetOrderId() || attempt.GetStatus() != cloudpb.PaymentAttemptStatus_PAYMENT_ATTEMPT_STATUS_SUCCEEDED {
			service.rejectPaymentEvent(ctx, event, "payment.rejected_order_state")
			return nil, ErrConflict
		}
		updatedOrder.Status = cloudpb.OrderStatus_ORDER_STATUS_REVOKED
		subscription = transitionSubscription(current, cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_SUSPEND, nil, now)
	default:
		service.rejectPaymentEvent(ctx, event, "payment.rejected_type")
		return nil, ErrConflict
	}
	if subscription == nil {
		service.rejectPaymentEvent(ctx, event, "payment.rejected_transition")
		return nil, ErrConflict
	}
	entitlementProjection, err := normalizeEntitlement(subscription, planForSubscription(service.catalog, subscription), now)
	if err != nil {
		return nil, err
	}
	result := &cloudpb.ApplyPaymentEventResponse{Order: updatedOrder, Subscription: subscription, EventState: cloudpb.PaymentEventState_PAYMENT_EVENT_STATE_APPLIED, PaymentAttempt: updatedAttempt}
	audit := service.audit(order.GetAccountId(), event.GetProvider(), paymentAuditAction(event.GetEventType()), event.GetProviderEventId(), now)
	if err := service.store.CommitPaymentEvent(ctx, event.GetProviderEventId(), result, entitlementProjection, audit); err != nil {
		return nil, err
	}
	return proto.Clone(result).(*cloudpb.ApplyPaymentEventResponse), nil
}

func (service *Service) rejectPaymentEvent(ctx context.Context, event *cloudpb.NormalizedPaymentEvent, action string) {
	if event == nil {
		return
	}
	_ = service.store.RejectPaymentEvent(ctx, event.GetProviderEventId(), service.audit(event.GetAccountId(), event.GetProvider(), action, event.GetProviderEventId(), service.now().UTC()))
}

// Transition 执行取消续订、恢复、暂停、恢复服务和到期等非支付状态变化。
func (service *Service) Transition(ctx context.Context, request *cloudpb.TransitionSubscriptionRequest) (*cloudpb.TransitionSubscriptionResponse, error) {
	if request == nil || request.GetAccountId() == "" || request.GetActorId() == "" {
		return nil, ErrConflict
	}
	current, err := service.store.Subscription(ctx, request.GetAccountId())
	if err != nil {
		return nil, err
	}
	now := service.now().UTC()
	if request.GetEffectiveAtUnixMillis() > 0 {
		now = time.UnixMilli(request.GetEffectiveAtUnixMillis()).UTC()
	}
	var targetPlan *cloudpb.PlanDefinition
	if request.GetTargetPlanId() != "" {
		targetPlan, _ = planByID(service.catalog, request.GetTargetPlanId(), request.GetTargetPlanVersion())
	}
	next := transitionSubscription(current, request.GetTransition(), targetPlan, now)
	if next == nil {
		return nil, ErrConflict
	}
	plan := planForSubscription(service.catalog, next)
	entitlementProjection, err := normalizeEntitlement(next, plan, now)
	if err != nil {
		return nil, err
	}
	audit := service.audit(next.GetAccountId(), request.GetActorId(), "subscription."+strings.ToLower(request.GetTransition().String()), next.GetSubscriptionId(), now)
	if err := service.store.CommitSubscriptionTransition(ctx, next, entitlementProjection, audit); err != nil {
		return nil, err
	}
	return &cloudpb.TransitionSubscriptionResponse{Subscription: next, Entitlement: entitlementProjection}, nil
}

// AccountCommerce 返回账号、Subscription、Entitlement、订单与持久审计。
func (service *Service) AccountCommerce(ctx context.Context, accountID string) (*cloudpb.GetAccountCommerceResponse, error) {
	account, err := service.store.Account(ctx, accountID)
	if err != nil {
		return nil, err
	}
	subscription, err := service.store.Subscription(ctx, accountID)
	if err != nil {
		return nil, err
	}
	entitlementProjection, err := service.store.Entitlement(ctx, accountID)
	if err != nil {
		return nil, err
	}
	orders, err := service.store.Orders(ctx, accountID)
	if err != nil {
		return nil, err
	}
	attempts, err := service.store.PaymentAttempts(ctx, accountID)
	if err != nil {
		return nil, err
	}
	audit, err := service.store.Audit(ctx, accountID)
	if err != nil {
		return nil, err
	}
	return &cloudpb.GetAccountCommerceResponse{Account: proto.Clone(account.Projection).(*cloudpb.AccountProjection), Subscription: subscription, Entitlement: entitlementProjection, Orders: orders, Audit: audit, PaymentAttempts: attempts}, nil
}

func (service *Service) newSession(account *cloudpb.AccountProjection, revision uint64, now time.Time) (*cloudpb.AccountSessionCredential, SessionRecord, error) {
	access, err := service.randomBytes(32)
	if err != nil {
		return nil, SessionRecord{}, err
	}
	refresh, err := service.randomBytes(32)
	if err != nil {
		return nil, SessionRecord{}, err
	}
	sessionID, err := service.randomID("session")
	if err != nil {
		return nil, SessionRecord{}, err
	}
	accessExpiry, refreshExpiry := now.Add(15*time.Minute), now.Add(30*24*time.Hour)
	record := SessionRecord{SessionID: sessionID, AccountID: account.GetAccountId(), AccessTokenHash: sha256.Sum256(access), RefreshTokenHash: sha256.Sum256(refresh), AccessExpiresAt: accessExpiry, RefreshExpiresAt: refreshExpiry, Revision: revision}
	credential := &cloudpb.AccountSessionCredential{SessionId: sessionID, Account: proto.Clone(account).(*cloudpb.AccountProjection), AccessToken: access, RefreshToken: refresh, AccessExpiresAtUnixMillis: accessExpiry.UnixMilli(), RefreshExpiresAtUnixMillis: refreshExpiry.UnixMilli(), SessionRevision: revision}
	return credential, record, nil
}

func (service *Service) audit(accountID, actorID, action, resourceID string, now time.Time) *cloudpb.CommerceAuditProjection {
	id, _ := service.randomID("audit")
	return &cloudpb.CommerceAuditProjection{AuditId: id, AccountId: accountID, ActorId: actorID, Action: action, ResourceId: resourceID, OccurredAtUnixMillis: now.UnixMilli()}
}

func (service *Service) randomID(prefix string) (string, error) {
	value, err := service.randomBytes(18)
	if err != nil {
		return "", err
	}
	return prefix + "-" + base64.RawURLEncoding.EncodeToString(value), nil
}

func (service *Service) randomBytes(size int) ([]byte, error) {
	value := make([]byte, size)
	if _, err := io.ReadFull(service.random, value); err != nil {
		return nil, err
	}
	return value, nil
}

func initialSubscription(accountID string, plan *cloudpb.PlanDefinition, now time.Time) *cloudpb.SubscriptionProjection {
	return &cloudpb.SubscriptionProjection{SubscriptionId: "subscription-" + accountID, AccountId: accountID, PlanId: plan.GetPlanId(), PlanVersion: plan.GetPlanVersion(), Status: cloudpb.SubscriptionStatus_SUBSCRIPTION_STATUS_ACTIVE, CurrentPeriodStartUnixMillis: now.UnixMilli(), CurrentPeriodEndUnixMillis: now.Add(time.Duration(plan.GetBillingPeriodDays()) * 24 * time.Hour).UnixMilli(), UpdatedAtUnixMillis: now.UnixMilli(), Revision: 1}
}

func paidSubscription(current *cloudpb.SubscriptionProjection, order *cloudpb.OrderProjection, plan *cloudpb.PlanDefinition, transition cloudpb.SubscriptionTransitionKind, now time.Time) *cloudpb.SubscriptionProjection {
	revision := uint64(1)
	periodStart := now
	if current != nil {
		revision = current.GetRevision() + 1
		if transition == cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_RENEW && current.GetCurrentPeriodEndUnixMillis() > now.UnixMilli() {
			periodStart = time.UnixMilli(current.GetCurrentPeriodEndUnixMillis()).UTC()
		}
	}
	return &cloudpb.SubscriptionProjection{SubscriptionId: "subscription-" + order.GetAccountId(), AccountId: order.GetAccountId(), SourceOrderId: order.GetOrderId(), PlanId: order.GetPlanId(), PlanVersion: order.GetPlanVersion(), Status: cloudpb.SubscriptionStatus_SUBSCRIPTION_STATUS_ACTIVE, CurrentPeriodStartUnixMillis: periodStart.UnixMilli(), CurrentPeriodEndUnixMillis: periodStart.Add(time.Duration(plan.GetBillingPeriodDays()) * 24 * time.Hour).UnixMilli(), UpdatedAtUnixMillis: now.UnixMilli(), ProviderReference: order.GetProviderReference(), Revision: revision}
}

func transitionSubscription(current *cloudpb.SubscriptionProjection, transition cloudpb.SubscriptionTransitionKind, targetPlan *cloudpb.PlanDefinition, now time.Time) *cloudpb.SubscriptionProjection {
	if current == nil {
		return nil
	}
	next := proto.Clone(current).(*cloudpb.SubscriptionProjection)
	next.Revision++
	next.UpdatedAtUnixMillis = now.UnixMilli()
	switch transition {
	case cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_CANCEL_AT_PERIOD_END:
		if current.GetStatus() != cloudpb.SubscriptionStatus_SUBSCRIPTION_STATUS_ACTIVE && current.GetStatus() != cloudpb.SubscriptionStatus_SUBSCRIPTION_STATUS_GRACE {
			return nil
		}
		next.Status = cloudpb.SubscriptionStatus_SUBSCRIPTION_STATUS_CANCEL_AT_PERIOD_END
		next.CancelAtPeriodEnd = true
	case cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_RESUME:
		if !current.GetCancelAtPeriodEnd() {
			return nil
		}
		next.Status = cloudpb.SubscriptionStatus_SUBSCRIPTION_STATUS_ACTIVE
		next.CancelAtPeriodEnd = false
	case cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_PAYMENT_FAILED:
		next.Status = cloudpb.SubscriptionStatus_SUBSCRIPTION_STATUS_PAST_DUE
	case cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_SUSPEND:
		next.Status = cloudpb.SubscriptionStatus_SUBSCRIPTION_STATUS_SUSPENDED
	case cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_RESTORE:
		if now.UnixMilli() >= current.GetCurrentPeriodEndUnixMillis() {
			next.Status = cloudpb.SubscriptionStatus_SUBSCRIPTION_STATUS_EXPIRED
		} else {
			next.Status = cloudpb.SubscriptionStatus_SUBSCRIPTION_STATUS_ACTIVE
		}
	case cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_EXPIRE:
		next.Status = cloudpb.SubscriptionStatus_SUBSCRIPTION_STATUS_EXPIRED
	case cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_REFUND,
		cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_REVOKE:
		next.Status = cloudpb.SubscriptionStatus_SUBSCRIPTION_STATUS_CANCELED
	case cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_UPGRADE,
		cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_DOWNGRADE:
		if targetPlan == nil || targetPlan.GetPlanId() == current.GetPlanId() {
			return nil
		}
		next.PlanId, next.PlanVersion = targetPlan.GetPlanId(), targetPlan.GetPlanVersion()
		next.CurrentPeriodStartUnixMillis = now.UnixMilli()
		next.CurrentPeriodEndUnixMillis = now.Add(time.Duration(targetPlan.GetBillingPeriodDays()) * 24 * time.Hour).UnixMilli()
		next.Status = cloudpb.SubscriptionStatus_SUBSCRIPTION_STATUS_ACTIVE
	default:
		return nil
	}
	return next
}

func normalizeEntitlement(subscription *cloudpb.SubscriptionProjection, plan *cloudpb.PlanDefinition, now time.Time) (*cloudpb.EntitlementProjection, error) {
	value, err := entitlement.Normalize(subscription, plan, now)
	if err != nil {
		return nil, err
	}
	return value.Projection(), nil
}

func planForSubscription(catalog *cloudpb.PlanCatalogContract, subscription *cloudpb.SubscriptionProjection) *cloudpb.PlanDefinition {
	plan, _ := planByID(catalog, subscription.GetPlanId(), subscription.GetPlanVersion())
	return plan
}

func planByID(catalog *cloudpb.PlanCatalogContract, planID string, version uint64) (*cloudpb.PlanDefinition, bool) {
	for _, plan := range catalog.GetPlans() {
		if plan.GetPlanId() == planID && (version == 0 || plan.GetPlanVersion() == version) {
			return proto.Clone(plan).(*cloudpb.PlanDefinition), true
		}
	}
	return nil, false
}

func includedPlan(catalog *cloudpb.PlanCatalogContract) (*cloudpb.PlanDefinition, bool) {
	var included *cloudpb.PlanDefinition
	for _, plan := range catalog.GetPlans() {
		if !plan.GetIncluded() {
			continue
		}
		if included != nil {
			return nil, false
		}
		included = plan
	}
	if included == nil {
		return nil, false
	}
	return proto.Clone(included).(*cloudpb.PlanDefinition), true
}

func validPaymentEvent(event *cloudpb.NormalizedPaymentEvent) bool {
	return event != nil && event.GetProviderEventId() != "" && event.GetProvider() != "" && event.GetEventType() != cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_UNSPECIFIED && event.GetOrderId() != "" && event.GetAccountId() != "" && event.GetPlanId() != "" && event.GetPlanVersion() > 0 && event.GetOccurredAtUnixMillis() > 0 && event.GetPaymentAttemptId() != ""
}

func clonePrice(value *cloudpb.PlanPriceDefinition) *cloudpb.PlanPriceDefinition {
	if value == nil {
		return nil
	}
	return proto.Clone(value).(*cloudpb.PlanPriceDefinition)
}

func paymentAuditAction(eventType cloudpb.PaymentEventType) string {
	switch eventType {
	case cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_SUCCEEDED:
		return "payment.succeeded"
	case cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_FAILED:
		return "payment.failed"
	case cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_REFUNDED:
		return "payment.refunded"
	case cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_REVOKED:
		return "payment.revoked"
	case cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_CHARGEBACK:
		return "payment.chargeback"
	default:
		return "payment.unknown"
	}
}

func normalizeEmail(value string) string { return strings.ToLower(strings.TrimSpace(value)) }

func validEmail(value string) bool {
	address, err := mail.ParseAddress(value)
	return err == nil && address.Address == value && strings.Contains(value, "@")
}

func emailDisplayName(email string) string {
	name, _, _ := strings.Cut(email, "@")
	if name == "" {
		return "TermX User"
	}
	return name
}
