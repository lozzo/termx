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

	"github.com/muxvia/muxvia/private/cloud/control-plane/entitlement"
	"github.com/muxvia/muxvia/private/cloud/control-plane/promotion"
	"github.com/muxvia/muxvia/proto/cloudpb"
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
	ClientDeviceID   string
	AccessTokenHash  [sha256.Size]byte
	RefreshTokenHash [sha256.Size]byte
	AccessExpiresAt  time.Time
	RefreshExpiresAt time.Time
	Revision         uint64
	Revoked          bool
}

// DeviceSessionCredential 是 Controller 扫码登录使用的持久账号 refresh session。
// Credential 仍是 generated Proto；ClientDeviceID 只用于 Controller 在轮换后重新签发绑定同一设备的 edge token。
type DeviceSessionCredential struct {
	Credential     *cloudpb.AccountSessionCredential
	ClientDeviceID string
}

// PreparedDeviceSession 是尚未持久化的设备 refresh session。
// Credential 含只应交付一次的明文 token；Record 与 Audit 供上层组合事务写入，准备失败或事务回滚时
// 调用方必须丢弃 Credential，不能把它返回给客户端。
type PreparedDeviceSession struct {
	Credential *cloudpb.AccountSessionCredential
	Record     SessionRecord
	Audit      *cloudpb.CommerceAuditProjection
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
	Accounts(context.Context, int) ([]AccountRecord, error)
	AllOrders(context.Context, int) ([]*cloudpb.OrderProjection, error)
	Subscriptions(context.Context, int) ([]*cloudpb.SubscriptionProjection, error)
	PutSession(context.Context, SessionRecord, *cloudpb.CommerceAuditProjection) error
	ReplaceDeviceSession(context.Context, SessionRecord, *cloudpb.CommerceAuditProjection) error
	SessionByAccessHash(context.Context, [sha256.Size]byte) (SessionRecord, error)
	SessionByRefreshHash(context.Context, [sha256.Size]byte) (SessionRecord, error)
	RotateSession(context.Context, string, SessionRecord, *cloudpb.CommerceAuditProjection) error
	RevokeSession(context.Context, string, string, bool, *cloudpb.CommerceAuditProjection) error
	ChangePassword(context.Context, AccountRecord, SessionRecord, *cloudpb.CommerceAuditProjection) error
	CreateOrder(context.Context, *cloudpb.OrderProjection, *cloudpb.CommerceAuditProjection) error
	Order(context.Context, string) (*cloudpb.OrderProjection, error)
	CreatePaymentAttempt(context.Context, *cloudpb.PaymentAttemptProjection, *cloudpb.CommerceAuditProjection) error
	UpdatePaymentAttempt(context.Context, *cloudpb.PaymentAttemptProjection, uint64) error
	PaymentAttempt(context.Context, string) (*cloudpb.PaymentAttemptProjection, error)
	PendingPaymentAttempts(context.Context, string, time.Time, int) ([]*cloudpb.PaymentAttemptProjection, error)
	PaymentAttemptByProviderReference(context.Context, string, string) (*cloudpb.PaymentAttemptProjection, error)
	RecordPaymentEvent(context.Context, PaymentEventRecord) (PaymentEventRecord, bool, error)
	RejectPaymentEvent(context.Context, string, *cloudpb.CommerceAuditProjection) error
	CommitPaymentEvent(context.Context, string, *cloudpb.ApplyPaymentEventResponse, *cloudpb.EntitlementProjection, *cloudpb.CommerceAuditProjection) error
	CommitSubscriptionTransition(context.Context, *cloudpb.SubscriptionProjection, *cloudpb.EntitlementProjection, *cloudpb.CommerceAuditProjection) error
	Subscription(context.Context, string) (*cloudpb.SubscriptionProjection, error)
	Entitlement(context.Context, string) (*cloudpb.EntitlementProjection, error)
	Orders(context.Context, string) ([]*cloudpb.OrderProjection, error)
	PaymentAttempts(context.Context, string) ([]*cloudpb.PaymentAttemptProjection, error)
	PaymentEvents(context.Context, string) ([]*cloudpb.PaymentEventProjection, error)
	PromotionRedemptions(context.Context, string, string, int) ([]*cloudpb.PromotionRedemptionProjection, error)
	SubscriptionAdjustments(context.Context, string, int) ([]*cloudpb.SubscriptionAdjustmentProjection, error)
	CommitSubscriptionAdjustment(context.Context, *cloudpb.SubscriptionAdjustmentProjection, *cloudpb.SubscriptionProjection, *cloudpb.EntitlementProjection, *cloudpb.OperatorMutationAuditProjection) error
	RecordOperatorAudit(context.Context, *cloudpb.OperatorMutationAuditProjection) error
	Audit(context.Context, string) ([]*cloudpb.CommerceAuditProjection, error)
}

// PromotionCheckout 原子保存带优惠 reservation 的订单。
type PromotionCheckout interface {
	ReserveCheckout(context.Context, *cloudpb.OrderProjection, *cloudpb.CommerceAuditProjection, string) (*cloudpb.PromotionRedemptionProjection, error)
}

// CatalogSource 提供当前发布目录与历史套餐快照。
// Commerce 创建新交易时读取 Active，处理既有订单/订阅时必须读取精确历史 Plan。
type CatalogSource interface {
	Active(context.Context) (*cloudpb.PlanCatalogContract, error)
	Plan(context.Context, string, uint64) (*cloudpb.PlanDefinition, error)
}

// Config 固定 commerce Store、数据库目录来源、时间与随机来源。
type Config struct {
	Store      Store
	Catalog    CatalogSource
	Promotions PromotionCheckout
	Now        func() time.Time
	Random     io.Reader
	// NotifyPolicyChange 在账号 auth revision 或 Entitlement 持久提交后通知 Controller。
	NotifyPolicyChange func(string)
}

// Service 是 Controller 内账号、交易与 Subscription 的应用边界。
type Service struct {
	store              Store
	catalog            CatalogSource
	promotions         PromotionCheckout
	now                func() time.Time
	random             io.Reader
	dummyPasswordHash  []byte
	notifyPolicyChange func(string)
}

// New 创建 commerce service；catalog 或 Store 缺失时 fail closed。
func New(config Config) (*Service, error) {
	if config.Store == nil || config.Catalog == nil {
		return nil, fmt.Errorf("commerce store and catalog are required")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Random == nil {
		config.Random = rand.Reader
	}
	dummyPasswordHash, err := bcrypt.GenerateFromPassword([]byte("muxvia-invalid-password"), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	return &Service{store: config.Store, catalog: config.Catalog, promotions: config.Promotions, now: config.Now, random: config.Random, dummyPasswordHash: dummyPasswordHash, notifyPolicyChange: config.NotifyPolicyChange}, nil
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
	catalog, err := service.catalog.Active(ctx)
	if err != nil {
		return nil, err
	}
	plan, ok := includedPlan(catalog)
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
	service.notifyPolicy(accountID)
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

// VerifyPassword 为已认证账号创建近期认证 proof；它不创建新 session，也不暴露 verifier。
func (service *Service) VerifyPassword(ctx context.Context, accountID, password string) error {
	if accountID == "" || password == "" {
		return ErrUnauthorized
	}
	account, err := service.store.Account(ctx, accountID)
	if err != nil || bcrypt.CompareHashAndPassword(account.PasswordHash, []byte(password)) != nil {
		return ErrUnauthorized
	}
	return nil
}

// Accounts 返回 operator 列表使用的有界账号投影。
func (service *Service) Accounts(ctx context.Context, limit int) ([]*cloudpb.AccountProjection, error) {
	if limit < 1 || limit > 200 {
		return nil, ErrConflict
	}
	records, err := service.store.Accounts(ctx, limit)
	if err != nil {
		return nil, err
	}
	result := make([]*cloudpb.AccountProjection, 0, len(records))
	for _, record := range records {
		result = append(result, proto.Clone(record.Projection).(*cloudpb.AccountProjection))
	}
	return result, nil
}

// Refresh 单次消费 refresh token，撤销旧 session 并返回新 token pair。
func (service *Service) Refresh(ctx context.Context, request *cloudpb.RefreshAccountSessionRequest) (*cloudpb.RefreshAccountSessionResponse, error) {
	if request == nil || len(request.GetRefreshToken()) == 0 {
		return nil, ErrUnauthorized
	}
	old, err := service.store.SessionByRefreshHash(ctx, sha256.Sum256(request.GetRefreshToken()))
	if err != nil || old.Revoked || old.ClientDeviceID != "" || !service.now().UTC().Before(old.RefreshExpiresAt) {
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

// IssueDeviceSession 为已由 Web 明确批准的 client device 创建持久、可单次轮换的 refresh session。
// 调用方必须先完成账号认证和设备授权；该方法不签发 Hub edge token。
func (service *Service) IssueDeviceSession(ctx context.Context, accountID, clientDeviceID string) (*cloudpb.AccountSessionCredential, error) {
	if accountID == "" || clientDeviceID == "" {
		return nil, ErrUnauthorized
	}
	account, err := service.store.Account(ctx, accountID)
	if err != nil {
		return nil, ErrUnauthorized
	}
	prepared, err := service.PrepareDeviceSession(account.Projection, clientDeviceID, service.now().UTC())
	if err != nil {
		return nil, err
	}
	if err := service.store.ReplaceDeviceSession(ctx, prepared.Record, prepared.Audit); err != nil {
		return nil, err
	}
	return prepared.Credential, nil
}

// PrepareDeviceSession 只在内存中生成设备 session 的明文 credential、持久 hash 与审计 projection。
// 它不访问 Store；调用方必须把 Record 和 Audit 放入自己的最终事务，提交成功后才能交付 Credential。
func (service *Service) PrepareDeviceSession(account *cloudpb.AccountProjection, clientDeviceID string, now time.Time) (PreparedDeviceSession, error) {
	if account == nil || account.GetAccountId() == "" || account.GetUserId() == "" || account.GetAuthRevision() == 0 || clientDeviceID == "" || now.IsZero() {
		return PreparedDeviceSession{}, ErrUnauthorized
	}
	credential, record, err := service.newSession(account, 1, now.UTC())
	if err != nil {
		return PreparedDeviceSession{}, err
	}
	record.ClientDeviceID = clientDeviceID
	return PreparedDeviceSession{
		Credential: credential,
		Record:     record,
		Audit:      service.audit(account.GetAccountId(), account.GetUserId(), "session.device.issue", clientDeviceID, now.UTC()),
	}, nil
}

// RefreshDeviceSession 单次轮换扫码登录设备的持久 refresh token，并保留原 client device 绑定。
func (service *Service) RefreshDeviceSession(ctx context.Context, refreshToken []byte) (*DeviceSessionCredential, error) {
	if len(refreshToken) == 0 {
		return nil, ErrUnauthorized
	}
	old, err := service.store.SessionByRefreshHash(ctx, sha256.Sum256(refreshToken))
	if err != nil || old.Revoked || old.ClientDeviceID == "" || !service.now().UTC().Before(old.RefreshExpiresAt) {
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
	next.ClientDeviceID = old.ClientDeviceID
	if err := service.store.RotateSession(ctx, old.SessionID, next, service.audit(old.AccountID, account.Projection.GetUserId(), "session.device.refresh", old.ClientDeviceID, now)); err != nil {
		return nil, err
	}
	return &DeviceSessionCredential{Credential: credential, ClientDeviceID: old.ClientDeviceID}, nil
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
	service.notifyPolicy(accountID)
	return &cloudpb.ChangeAccountPasswordResponse{Session: credential}, nil
}

// CreateCheckout 创建 pending order；included plan 或非法 transition 不修改 Subscription。
func (service *Service) CreateCheckout(ctx context.Context, accountID, actorID string, request *cloudpb.CreateCheckoutRequest) (*cloudpb.CreateCheckoutResponse, error) {
	if request == nil {
		return nil, ErrConflict
	}
	catalog, err := service.catalog.Active(ctx)
	if err != nil {
		return nil, err
	}
	plan, ok := planByID(catalog, request.GetPlanId(), 0)
	transition := request.GetRequestedTransition()
	if accountID == "" || actorID == "" || !ok || plan.GetIncluded() || plan.GetPrice().GetMode() != cloudpb.CatalogPriceMode_CATALOG_PRICE_MODE_CONFIGURED || transition < cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_ACTIVATE || transition > cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_DOWNGRADE {
		return nil, ErrConflict
	}
	var subtotal int64
	var providerProductID string
	switch request.GetBillingCadence() {
	case cloudpb.BillingCadence_BILLING_CADENCE_MONTHLY:
		subtotal = plan.GetPrice().GetMonthlyMinor()
		providerProductID = strings.TrimSpace(plan.GetCreem().GetMonthlyProductId())
	case cloudpb.BillingCadence_BILLING_CADENCE_YEARLY:
		subtotal = plan.GetPrice().GetYearlyMinor()
		providerProductID = strings.TrimSpace(plan.GetCreem().GetYearlyProductId())
	default:
		return nil, ErrConflict
	}
	if subtotal <= 0 {
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
	order := &cloudpb.OrderProjection{OrderId: orderID, AccountId: accountID, PlanId: plan.GetPlanId(), PlanVersion: plan.GetPlanVersion(), Status: cloudpb.OrderStatus_ORDER_STATUS_PENDING, CreatedAtUnixMillis: now.UnixMilli(), Revision: 1, RequestedTransition: transition, Price: clonePrice(plan.GetPrice()), BillingCadence: request.GetBillingCadence(), SubtotalMinor: subtotal, TotalMinor: subtotal, ProviderProductId: providerProductID}
	if currentErr == nil {
		order.SourceSubscriptionRevision = current.GetRevision()
		order.SourcePlanId = current.GetPlanId()
		order.SourcePlanVersion = current.GetPlanVersion()
	}
	audit := service.audit(accountID, actorID, "order.created", orderID, now)
	if strings.TrimSpace(request.GetPromotionCode()) != "" {
		if service.promotions == nil {
			return nil, ErrConflict
		}
		if _, err := service.promotions.ReserveCheckout(ctx, order, audit, request.GetPromotionCode()); err != nil {
			return nil, err
		}
	} else if err := service.store.CreateOrder(ctx, order, audit); err != nil {
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

// UpdateProviderPaymentAttempt 以 revision CAS 保存 provider checkout、transaction、subscription 与轮询状态。
// 这些字段的真值来自已认证 provider API；调用方不得用浏览器 redirect 或客户端上报填充。
func (service *Service) UpdateProviderPaymentAttempt(ctx context.Context, attempt *cloudpb.PaymentAttemptProjection, expectedRevision uint64) error {
	if attempt == nil || attempt.GetPaymentAttemptId() == "" || attempt.GetProvider() == "" || attempt.GetRevision() != expectedRevision+1 || expectedRevision == 0 {
		return ErrConflict
	}
	return service.store.UpdatePaymentAttempt(ctx, attempt, expectedRevision)
}

// ProviderPaymentContext 返回 adapter 对账一个 attempt 所需的持久账号、订单与尝试投影。
func (service *Service) ProviderPaymentContext(ctx context.Context, attemptID string) (*cloudpb.AccountProjection, *cloudpb.OrderProjection, *cloudpb.PaymentAttemptProjection, error) {
	attempt, err := service.store.PaymentAttempt(ctx, attemptID)
	if err != nil {
		return nil, nil, nil, err
	}
	order, err := service.store.Order(ctx, attempt.GetOrderId())
	if err != nil || order.GetAccountId() != attempt.GetAccountId() {
		return nil, nil, nil, ErrConflict
	}
	account, err := service.store.Account(ctx, attempt.GetAccountId())
	if err != nil {
		return nil, nil, nil, err
	}
	return proto.Clone(account.Projection).(*cloudpb.AccountProjection), proto.Clone(order).(*cloudpb.OrderProjection), proto.Clone(attempt).(*cloudpb.PaymentAttemptProjection), nil
}

// PendingProviderPaymentAttempts 返回到期且仍未终结的 provider attempt，供有界 reconciliation 使用。
func (service *Service) PendingProviderPaymentAttempts(ctx context.Context, provider string, before time.Time, limit int) ([]*cloudpb.PaymentAttemptProjection, error) {
	if provider == "" || before.IsZero() || limit < 1 || limit > 200 {
		return nil, ErrConflict
	}
	return service.store.PendingPaymentAttempts(ctx, provider, before.UTC(), limit)
}

// ProviderPaymentAttemptByReference 按 provider checkout、transaction 或 subscription reference 查找唯一 attempt。
func (service *Service) ProviderPaymentAttemptByReference(ctx context.Context, provider, reference string) (*cloudpb.PaymentAttemptProjection, error) {
	if provider == "" || reference == "" {
		return nil, ErrConflict
	}
	return service.store.PaymentAttemptByProviderReference(ctx, provider, reference)
}

// RecordPaymentReconciliationAudit 持久记录运营员触发 provider 对账的原因与 attempt revision 边界。
// 支付结果仍只能由已认证 provider 响应经 normalized journal 改变，本审计本身不修改订单或订阅。
func (service *Service) RecordPaymentReconciliationAudit(ctx context.Context, attemptID, actorID, reason, requestID string, beforeRevision, afterRevision uint64) error {
	if attemptID == "" || actorID == "" || strings.TrimSpace(reason) == "" || requestID == "" || beforeRevision == 0 || afterRevision < beforeRevision {
		return ErrConflict
	}
	_, order, attempt, err := service.ProviderPaymentContext(ctx, attemptID)
	if err != nil || attempt.GetRevision() != afterRevision {
		return ErrConflict
	}
	now := service.now().UTC()
	return service.store.RecordOperatorAudit(ctx, &cloudpb.OperatorMutationAuditProjection{
		AuditId:              "audit_" + requestID,
		ActorId:              actorID,
		Action:               "payment_attempt.reconcile",
		ResourceKind:         "payment_attempt",
		ResourceId:           attemptID,
		AccountId:            order.GetAccountId(),
		Reason:               strings.TrimSpace(reason),
		RequestId:            requestID,
		BeforeRevision:       beforeRevision,
		AfterRevision:        afterRevision,
		OccurredAtUnixMillis: now.UnixMilli(),
	})
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
		attemptResponse, attemptErr := service.CreatePaymentAttempt(ctx, accountID, accountID, &cloudpb.CreatePaymentAttemptRequest{OrderId: order.GetOrderId(), Provider: "muxvia-test-provider"})
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
			if candidate.GetOrderId() == order.GetOrderId() && candidate.GetProvider() == "muxvia-test-provider" && candidate.GetStatus() == cloudpb.PaymentAttemptStatus_PAYMENT_ATTEMPT_STATUS_SUCCEEDED {
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
	result, err := service.ApplyPaymentEvent(ctx, &cloudpb.ApplyPaymentEventRequest{Event: &cloudpb.NormalizedPaymentEvent{ProviderEventId: eventID, Provider: "muxvia-test-provider", EventType: request.GetEventType(), OrderId: order.GetOrderId(), AccountId: order.GetAccountId(), PlanId: order.GetPlanId(), PlanVersion: order.GetPlanVersion(), ProviderReference: "test-" + order.GetOrderId(), OccurredAtUnixMillis: now.UnixMilli(), PaymentAttemptId: attempt.GetPaymentAttemptId()}})
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
		service.notifyPolicy(stored.Result.GetSubscription().GetAccountId())
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
	if event.GetProvider() == "creem" && !validCreemEconomics(event, order, attempt) {
		service.rejectPaymentEvent(ctx, event, "payment.rejected_provider_economics")
		return nil, ErrConflict
	}
	now := service.now().UTC()
	plan, planErr := service.catalog.Plan(ctx, order.GetPlanId(), order.GetPlanVersion())
	if planErr != nil {
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
	updatedOrder.ProviderReference = event.GetProviderSubscriptionReference()
	if updatedOrder.GetProviderReference() == "" {
		updatedOrder.ProviderReference = event.GetProviderReference()
	}
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
		if attempt.GetProviderReference() != "" {
			updatedAttempt.ProviderReference = attempt.GetProviderReference()
		}
		updatedAttempt.ProviderTransactionReference = event.GetProviderReference()
		updatedAttempt.ProviderSubscriptionReference = event.GetProviderSubscriptionReference()
		updatedAttempt.ReconcileAfterUnixMillis = 0
		updatedAttempt.ReconcileDeadlineUnixMillis = 0
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
		if attempt.GetProviderReference() != "" {
			updatedAttempt.ProviderReference = attempt.GetProviderReference()
		}
		updatedAttempt.ProviderTransactionReference = event.GetProviderReference()
		updatedAttempt.ProviderSubscriptionReference = event.GetProviderSubscriptionReference()
		updatedAttempt.ReconcileAfterUnixMillis = 0
		updatedAttempt.ReconcileDeadlineUnixMillis = 0
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
	case cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_SUBSCRIPTION_SCHEDULED_CANCEL:
		if order.GetStatus() != cloudpb.OrderStatus_ORDER_STATUS_PAID || current.GetSourceOrderId() != order.GetOrderId() || attempt.GetStatus() != cloudpb.PaymentAttemptStatus_PAYMENT_ATTEMPT_STATUS_SUCCEEDED {
			service.rejectPaymentEvent(ctx, event, "payment.rejected_order_state")
			return nil, ErrConflict
		}
		subscription = transitionSubscription(current, cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_CANCEL_AT_PERIOD_END, nil, now)
	case cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_SUBSCRIPTION_CANCELED:
		if order.GetStatus() != cloudpb.OrderStatus_ORDER_STATUS_PAID || current.GetSourceOrderId() != order.GetOrderId() || attempt.GetStatus() != cloudpb.PaymentAttemptStatus_PAYMENT_ATTEMPT_STATUS_SUCCEEDED {
			service.rejectPaymentEvent(ctx, event, "payment.rejected_order_state")
			return nil, ErrConflict
		}
		subscription = transitionSubscription(current, cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_REVOKE, nil, now)
	case cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_SUBSCRIPTION_PAST_DUE:
		if order.GetStatus() != cloudpb.OrderStatus_ORDER_STATUS_PAID || current.GetSourceOrderId() != order.GetOrderId() || attempt.GetStatus() != cloudpb.PaymentAttemptStatus_PAYMENT_ATTEMPT_STATUS_SUCCEEDED {
			service.rejectPaymentEvent(ctx, event, "payment.rejected_order_state")
			return nil, ErrConflict
		}
		subscription = transitionSubscription(current, cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_PAYMENT_FAILED, nil, now)
	case cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_SUBSCRIPTION_EXPIRED:
		if order.GetStatus() != cloudpb.OrderStatus_ORDER_STATUS_PAID || current.GetSourceOrderId() != order.GetOrderId() || attempt.GetStatus() != cloudpb.PaymentAttemptStatus_PAYMENT_ATTEMPT_STATUS_SUCCEEDED {
			service.rejectPaymentEvent(ctx, event, "payment.rejected_order_state")
			return nil, ErrConflict
		}
		subscription = transitionSubscription(current, cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_EXPIRE, nil, now)
	case cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_SUBSCRIPTION_PAUSED:
		if order.GetStatus() != cloudpb.OrderStatus_ORDER_STATUS_PAID || current.GetSourceOrderId() != order.GetOrderId() || attempt.GetStatus() != cloudpb.PaymentAttemptStatus_PAYMENT_ATTEMPT_STATUS_SUCCEEDED {
			service.rejectPaymentEvent(ctx, event, "payment.rejected_order_state")
			return nil, ErrConflict
		}
		subscription = transitionSubscription(current, cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_SUSPEND, nil, now)
	case cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_SUBSCRIPTION_ACTIVE_SYNC:
		if order.GetStatus() != cloudpb.OrderStatus_ORDER_STATUS_PAID || current.GetSourceOrderId() != order.GetOrderId() || attempt.GetStatus() != cloudpb.PaymentAttemptStatus_PAYMENT_ATTEMPT_STATUS_SUCCEEDED {
			service.rejectPaymentEvent(ctx, event, "payment.rejected_order_state")
			return nil, ErrConflict
		}
		// active 只推进 provider 同步 revision，不从 past_due/paused 恢复服务权限；恢复必须来自 paid。
		subscription = proto.Clone(current).(*cloudpb.SubscriptionProjection)
		subscription.Revision++
		subscription.UpdatedAtUnixMillis = now.UnixMilli()
	default:
		service.rejectPaymentEvent(ctx, event, "payment.rejected_type")
		return nil, ErrConflict
	}
	if subscription == nil {
		service.rejectPaymentEvent(ctx, event, "payment.rejected_transition")
		return nil, ErrConflict
	}
	subscriptionPlan, err := service.catalog.Plan(ctx, subscription.GetPlanId(), subscription.GetPlanVersion())
	if err != nil {
		return nil, err
	}
	entitlementProjection, err := normalizeEntitlement(subscription, subscriptionPlan, now)
	if err != nil {
		return nil, err
	}
	result := &cloudpb.ApplyPaymentEventResponse{Order: updatedOrder, Subscription: subscription, EventState: cloudpb.PaymentEventState_PAYMENT_EVENT_STATE_APPLIED, PaymentAttempt: updatedAttempt}
	actorID := event.GetProvider()
	if event.GetActorId() != "" {
		actorID = event.GetActorId()
	}
	audit := service.audit(order.GetAccountId(), actorID, paymentAuditAction(event.GetEventType()), event.GetProviderEventId(), now)
	if err := service.store.CommitPaymentEvent(ctx, event.GetProviderEventId(), result, entitlementProjection, audit); err != nil {
		if errors.Is(err, promotion.ErrConflict) {
			service.rejectPaymentEvent(ctx, event, "payment.rejected_promotion")
			return nil, ErrConflict
		}
		return nil, err
	}
	service.notifyPolicy(order.GetAccountId())
	return proto.Clone(result).(*cloudpb.ApplyPaymentEventResponse), nil
}

// AdjustSubscription 以显式 operator adjustment 赠送、延期或变更套餐，不伪造 provider 订单。
func (service *Service) AdjustSubscription(ctx context.Context, request *cloudpb.CreateSubscriptionAdjustmentRequest, actorID string) (*cloudpb.CreateSubscriptionAdjustmentResponse, error) {
	if request == nil || request.GetAccountId() == "" || actorID == "" || strings.TrimSpace(request.GetReason()) == "" || request.GetRequestId() == "" || request.GetDurationDays() == 0 || request.GetExpectedSubscriptionRevision() == 0 {
		return nil, ErrConflict
	}
	current, err := service.store.Subscription(ctx, request.GetAccountId())
	if err != nil {
		return nil, err
	}
	existingAdjustments, err := service.store.SubscriptionAdjustments(ctx, request.GetAccountId(), 200)
	if err != nil {
		return nil, err
	}
	for _, existing := range existingAdjustments {
		if existing.GetRequestId() != request.GetRequestId() {
			continue
		}
		if existing.GetActorId() != actorID || existing.GetAdjustmentKind() != request.GetAdjustmentKind() || existing.GetExpectedSubscriptionRevision() != request.GetExpectedSubscriptionRevision() || existing.GetTargetPlanId() != request.GetTargetPlanId() || existing.GetTargetPlanVersion() != request.GetTargetPlanVersion() || existing.GetDurationDays() != request.GetDurationDays() || existing.GetReason() != strings.TrimSpace(request.GetReason()) {
			return nil, ErrConflict
		}
		entitlementProjection, entitlementErr := service.store.Entitlement(ctx, request.GetAccountId())
		if entitlementErr != nil {
			return nil, entitlementErr
		}
		return &cloudpb.CreateSubscriptionAdjustmentResponse{Adjustment: existing, Subscription: current, Entitlement: entitlementProjection}, nil
	}
	if current.GetRevision() != request.GetExpectedSubscriptionRevision() {
		return nil, ErrConflict
	}
	now := service.now().UTC()
	next := proto.Clone(current).(*cloudpb.SubscriptionProjection)
	next.Revision++
	next.UpdatedAtUnixMillis = now.UnixMilli()
	next.Status = cloudpb.SubscriptionStatus_SUBSCRIPTION_STATUS_ACTIVE
	next.CancelAtPeriodEnd = false
	var plan *cloudpb.PlanDefinition
	switch request.GetAdjustmentKind() {
	case cloudpb.SubscriptionAdjustmentKind_SUBSCRIPTION_ADJUSTMENT_KIND_GRANT, cloudpb.SubscriptionAdjustmentKind_SUBSCRIPTION_ADJUSTMENT_KIND_CHANGE_PLAN:
		if request.GetTargetPlanId() == "" || request.GetTargetPlanVersion() == 0 {
			return nil, ErrConflict
		}
		plan, err = service.catalog.Plan(ctx, request.GetTargetPlanId(), request.GetTargetPlanVersion())
		if err != nil {
			return nil, ErrConflict
		}
		next.PlanId, next.PlanVersion = plan.GetPlanId(), plan.GetPlanVersion()
		next.CurrentPeriodStartUnixMillis = now.UnixMilli()
		next.CurrentPeriodEndUnixMillis = now.Add(time.Duration(request.GetDurationDays()) * 24 * time.Hour).UnixMilli()
		next.SourceOrderId, next.ProviderReference = "", ""
	case cloudpb.SubscriptionAdjustmentKind_SUBSCRIPTION_ADJUSTMENT_KIND_EXTEND:
		if request.GetTargetPlanId() != "" || request.GetTargetPlanVersion() != 0 {
			return nil, ErrConflict
		}
		plan, err = service.catalog.Plan(ctx, next.GetPlanId(), next.GetPlanVersion())
		if err != nil {
			return nil, ErrConflict
		}
		periodEnd := now
		if next.GetCurrentPeriodEndUnixMillis() > now.UnixMilli() {
			periodEnd = time.UnixMilli(next.GetCurrentPeriodEndUnixMillis()).UTC()
		}
		next.CurrentPeriodEndUnixMillis = periodEnd.Add(time.Duration(request.GetDurationDays()) * 24 * time.Hour).UnixMilli()
	default:
		return nil, ErrConflict
	}
	entitlementProjection, err := normalizeEntitlement(next, plan, now)
	if err != nil {
		return nil, err
	}
	adjustmentID, err := service.randomID("adjustment")
	if err != nil {
		return nil, err
	}
	adjustment := &cloudpb.SubscriptionAdjustmentProjection{AdjustmentId: adjustmentID, AccountId: request.GetAccountId(), AdjustmentKind: request.GetAdjustmentKind(), TargetPlanId: next.GetPlanId(), TargetPlanVersion: next.GetPlanVersion(), DurationDays: request.GetDurationDays(), ExpectedSubscriptionRevision: current.GetRevision(), ResultingSubscriptionRevision: next.GetRevision(), ActorId: actorID, Reason: strings.TrimSpace(request.GetReason()), RequestId: request.GetRequestId(), CreatedAtUnixMillis: now.UnixMilli(), Revision: 1}
	audit := &cloudpb.OperatorMutationAuditProjection{AuditId: "audit_" + request.GetRequestId(), ActorId: actorID, Action: "subscription.adjust", ResourceKind: "subscription", ResourceId: current.GetSubscriptionId(), AccountId: request.GetAccountId(), Reason: adjustment.GetReason(), RequestId: request.GetRequestId(), BeforeRevision: current.GetRevision(), AfterRevision: next.GetRevision(), OccurredAtUnixMillis: now.UnixMilli()}
	if err := service.store.CommitSubscriptionAdjustment(ctx, adjustment, next, entitlementProjection, audit); err != nil {
		return nil, err
	}
	service.notifyPolicy(request.GetAccountId())
	return &cloudpb.CreateSubscriptionAdjustmentResponse{Adjustment: adjustment, Subscription: next, Entitlement: entitlementProjection}, nil
}

// ApplyOperatorPaymentEvent 把人工收款、退款或撤销归一化为同一 payment journal 输入。
func (service *Service) ApplyOperatorPaymentEvent(ctx context.Context, request *cloudpb.ApplyOperatorPaymentEventRequest, actorID string) (*cloudpb.ApplyOperatorPaymentEventResponse, error) {
	if request == nil || request.GetOrderId() == "" || actorID == "" || strings.TrimSpace(request.GetReason()) == "" || request.GetRequestId() == "" {
		return nil, ErrConflict
	}
	order, err := service.store.Order(ctx, request.GetOrderId())
	if err != nil {
		return nil, err
	}
	eventID := "operator:" + request.GetRequestId()
	events, err := service.store.PaymentEvents(ctx, order.GetAccountId())
	if err != nil {
		return nil, err
	}
	for _, existing := range events {
		if existing.GetEvent().GetProviderEventId() == eventID {
			if existing.GetEvent().GetOrderId() != request.GetOrderId() || existing.GetEvent().GetEventType() != request.GetEventType() || existing.GetEvent().GetActorId() != actorID || existing.GetEvent().GetReason() != strings.TrimSpace(request.GetReason()) || existing.GetEvent().GetRequestId() != request.GetRequestId() {
				return nil, ErrConflict
			}
			result, applyErr := service.ApplyPaymentEvent(ctx, &cloudpb.ApplyPaymentEventRequest{Event: existing.GetEvent()})
			return &cloudpb.ApplyOperatorPaymentEventResponse{Result: result}, applyErr
		}
	}
	if request.GetEventType() != cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_SUCCEEDED && request.GetEventType() != cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_REFUNDED && request.GetEventType() != cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_REVOKED {
		return nil, ErrConflict
	}
	var attempt *cloudpb.PaymentAttemptProjection
	if request.GetEventType() == cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_SUCCEEDED {
		attempts, attemptsErr := service.store.PaymentAttempts(ctx, order.GetAccountId())
		if attemptsErr != nil {
			return nil, attemptsErr
		}
		for _, candidate := range attempts {
			if candidate.GetOrderId() == order.GetOrderId() {
				return nil, ErrConflict
			}
		}
		created, createErr := service.CreatePaymentAttempt(ctx, order.GetAccountId(), actorID, &cloudpb.CreatePaymentAttemptRequest{OrderId: order.GetOrderId(), Provider: "operator-manual"})
		if createErr != nil {
			return nil, createErr
		}
		attempt = created.GetPaymentAttempt()
	} else {
		attempts, attemptsErr := service.store.PaymentAttempts(ctx, order.GetAccountId())
		if attemptsErr != nil {
			return nil, attemptsErr
		}
		for _, candidate := range attempts {
			if candidate.GetOrderId() == order.GetOrderId() && candidate.GetProvider() != "creem" && candidate.GetStatus() == cloudpb.PaymentAttemptStatus_PAYMENT_ATTEMPT_STATUS_SUCCEEDED {
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
	now := service.now().UTC()
	event := &cloudpb.NormalizedPaymentEvent{ProviderEventId: eventID, Provider: attempt.GetProvider(), EventType: request.GetEventType(), OrderId: order.GetOrderId(), AccountId: order.GetAccountId(), PlanId: order.GetPlanId(), PlanVersion: order.GetPlanVersion(), ProviderReference: "operator:" + request.GetRequestId(), OccurredAtUnixMillis: now.UnixMilli(), PaymentAttemptId: attempt.GetPaymentAttemptId(), ActorId: actorID, Reason: strings.TrimSpace(request.GetReason()), RequestId: request.GetRequestId()}
	result, err := service.ApplyPaymentEvent(ctx, &cloudpb.ApplyPaymentEventRequest{Event: event})
	if err != nil {
		return nil, err
	}
	return &cloudpb.ApplyOperatorPaymentEventResponse{Result: result}, nil
}

// OperatorOrders 返回带 attempt/event 时间线的有界订单列表。
func (service *Service) OperatorOrders(ctx context.Context, request *cloudpb.ListOperatorOrdersRequest) (*cloudpb.ListOperatorOrdersResponse, error) {
	limit := pageSize(request.GetPage(), 100)
	orders, err := service.store.AllOrders(ctx, limit)
	if err != nil {
		return nil, err
	}
	response := &cloudpb.ListOperatorOrdersResponse{}
	for _, order := range orders {
		if request.GetAccountId() != "" && order.GetAccountId() != request.GetAccountId() || request.GetStatus() != cloudpb.OrderStatus_ORDER_STATUS_UNSPECIFIED && order.GetStatus() != request.GetStatus() {
			continue
		}
		attempts, attemptsErr := service.store.PaymentAttempts(ctx, order.GetAccountId())
		if attemptsErr != nil {
			return nil, attemptsErr
		}
		events, eventsErr := service.store.PaymentEvents(ctx, order.GetAccountId())
		if eventsErr != nil {
			return nil, eventsErr
		}
		item := &cloudpb.OperatorOrderProjection{Order: order}
		for _, attempt := range attempts {
			if attempt.GetOrderId() == order.GetOrderId() && (request.GetProvider() == "" || attempt.GetProvider() == request.GetProvider()) {
				item.PaymentAttempts = append(item.PaymentAttempts, attempt)
			}
		}
		for _, event := range events {
			if event.GetEvent().GetOrderId() == order.GetOrderId() {
				item.PaymentEvents = append(item.PaymentEvents, event)
			}
		}
		if request.GetProvider() == "" || len(item.GetPaymentAttempts()) > 0 {
			response.Orders = append(response.Orders, item)
		}
	}
	return response, nil
}

// OperatorSubscriptions 返回有界订阅列表，并按稳定状态筛选。
func (service *Service) OperatorSubscriptions(ctx context.Context, request *cloudpb.ListOperatorSubscriptionsRequest) (*cloudpb.ListOperatorSubscriptionsResponse, error) {
	values, err := service.store.Subscriptions(ctx, pageSize(request.GetPage(), 100))
	if err != nil {
		return nil, err
	}
	response := &cloudpb.ListOperatorSubscriptionsResponse{}
	for _, value := range values {
		if request.GetStatus() == cloudpb.SubscriptionStatus_SUBSCRIPTION_STATUS_UNSPECIFIED || value.GetStatus() == request.GetStatus() {
			response.Subscriptions = append(response.Subscriptions, value)
		}
	}
	return response, nil
}

func pageSize(page *cloudpb.PageRequest, fallback int) int {
	if page != nil && page.GetPageSize() > 0 && page.GetPageSize() <= 200 {
		return int(page.GetPageSize())
	}
	return fallback
}

func (service *Service) rejectPaymentEvent(ctx context.Context, event *cloudpb.NormalizedPaymentEvent, action string) {
	if event == nil {
		return
	}
	_ = service.store.RejectPaymentEvent(ctx, event.GetProviderEventId(), service.audit(event.GetAccountId(), event.GetProvider(), action, event.GetProviderEventId(), service.now().UTC()))
}

func (service *Service) notifyPolicy(accountID string) {
	if service.notifyPolicyChange != nil && accountID != "" {
		service.notifyPolicyChange(accountID)
	}
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
		if request.GetTargetPlanVersion() == 0 {
			catalog, catalogErr := service.catalog.Active(ctx)
			if catalogErr != nil {
				return nil, catalogErr
			}
			targetPlan, _ = planByID(catalog, request.GetTargetPlanId(), 0)
		} else {
			targetPlan, _ = service.catalog.Plan(ctx, request.GetTargetPlanId(), request.GetTargetPlanVersion())
		}
	}
	next := transitionSubscription(current, request.GetTransition(), targetPlan, now)
	if next == nil {
		return nil, ErrConflict
	}
	plan, err := service.catalog.Plan(ctx, next.GetPlanId(), next.GetPlanVersion())
	if targetPlan != nil && targetPlan.GetPlanId() == next.GetPlanId() && targetPlan.GetPlanVersion() == next.GetPlanVersion() {
		plan, err = targetPlan, nil
	}
	if err != nil {
		return nil, err
	}
	entitlementProjection, err := normalizeEntitlement(next, plan, now)
	if err != nil {
		return nil, err
	}
	audit := service.audit(next.GetAccountId(), request.GetActorId(), "subscription."+strings.ToLower(request.GetTransition().String()), next.GetSubscriptionId(), now)
	if err := service.store.CommitSubscriptionTransition(ctx, next, entitlementProjection, audit); err != nil {
		return nil, err
	}
	service.notifyPolicy(next.GetAccountId())
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
	plan, err := service.catalog.Plan(ctx, subscription.GetPlanId(), subscription.GetPlanVersion())
	if err != nil {
		return nil, ErrConflict
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
	events, err := service.store.PaymentEvents(ctx, accountID)
	if err != nil {
		return nil, err
	}
	audit, err := service.store.Audit(ctx, accountID)
	if err != nil {
		return nil, err
	}
	redemptions, err := service.store.PromotionRedemptions(ctx, "", accountID, 200)
	if err != nil {
		return nil, err
	}
	adjustments, err := service.store.SubscriptionAdjustments(ctx, accountID, 200)
	if err != nil {
		return nil, err
	}
	return &cloudpb.GetAccountCommerceResponse{Account: proto.Clone(account.Projection).(*cloudpb.AccountProjection), Subscription: subscription, Entitlement: entitlementProjection, Orders: orders, Audit: audit, PaymentAttempts: attempts, PaymentEvents: events, Plan: proto.Clone(plan).(*cloudpb.PlanDefinition), PromotionRedemptions: redemptions, SubscriptionAdjustments: adjustments}, nil
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
	periodDays := plan.GetBillingPeriodDays()
	if order.GetBillingCadence() == cloudpb.BillingCadence_BILLING_CADENCE_YEARLY {
		periodDays = 365
	}
	return &cloudpb.SubscriptionProjection{SubscriptionId: "subscription-" + order.GetAccountId(), AccountId: order.GetAccountId(), SourceOrderId: order.GetOrderId(), PlanId: order.GetPlanId(), PlanVersion: order.GetPlanVersion(), Status: cloudpb.SubscriptionStatus_SUBSCRIPTION_STATUS_ACTIVE, CurrentPeriodStartUnixMillis: periodStart.UnixMilli(), CurrentPeriodEndUnixMillis: periodStart.Add(time.Duration(periodDays) * 24 * time.Hour).UnixMilli(), UpdatedAtUnixMillis: now.UnixMilli(), ProviderReference: order.GetProviderReference(), Revision: revision}
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
	projection := value.Projection()
	projection.Revision = subscription.GetRevision()
	return projection, nil
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

func validCreemEconomics(event *cloudpb.NormalizedPaymentEvent, order *cloudpb.OrderProjection, attempt *cloudpb.PaymentAttemptProjection) bool {
	if event.GetProviderProductId() != order.GetProviderProductId() || !strings.EqualFold(event.GetCurrency(), order.GetPrice().GetCurrency()) {
		return false
	}
	switch event.GetEventType() {
	case cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_SUCCEEDED,
		cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_REFUNDED,
		cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_CHARGEBACK:
		if event.GetSubtotalMinor() != order.GetSubtotalMinor() || event.GetDiscountMinor() != order.GetDiscountMinor() {
			return false
		}
		if order.GetPromotion() != nil {
			return event.GetProviderDiscountReference() != "" && event.GetProviderDiscountReference() == attempt.GetProviderDiscountReference()
		}
		return event.GetProviderDiscountReference() == "" && event.GetDiscountMinor() == 0
	default:
		return true
	}
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
	case cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_SUBSCRIPTION_SCHEDULED_CANCEL:
		return "subscription.provider_scheduled_cancel"
	case cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_SUBSCRIPTION_CANCELED:
		return "subscription.provider_canceled"
	case cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_SUBSCRIPTION_PAST_DUE:
		return "subscription.provider_past_due"
	case cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_SUBSCRIPTION_EXPIRED:
		return "subscription.provider_expired"
	case cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_SUBSCRIPTION_PAUSED:
		return "subscription.provider_paused"
	case cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_SUBSCRIPTION_ACTIVE_SYNC:
		return "subscription.provider_active_sync"
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
		return "Muxvia User"
	}
	return name
}
