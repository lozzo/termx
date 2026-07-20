package webcontroller

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/lozzow/termx/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

var (
	ErrCommerceUnauthorized = errors.New("commerce session is unauthorized")
	ErrCommerceConflict     = errors.New("commerce state conflict")
	ErrPaymentSignature     = errors.New("payment webhook signature is invalid")
)

// EntitlementProjectionPublisher 是付款事务对 Control Plane 的唯一写边界。
// 实现必须接收 versioned Subscription，再由 Control Plane 归一化 Entitlement；它不得接触 terminal capability。
type EntitlementProjectionPublisher interface {
	PublishSubscription(subscription *cloudpb.SubscriptionProjection) error
}

// CommerceSession 是 BFF 持有的浏览器账号会话投影；原始 token 只返回一次，store 仅保存摘要。
type CommerceSession struct {
	Token     string    `json:"token,omitempty"`
	AccountID string    `json:"account_id"`
	UserID    string    `json:"user_id"`
	Email     string    `json:"email"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Order 是 checkout 与 provider webhook 之间的幂等事务记录。
type Order struct {
	ID          string    `json:"id"`
	AccountID   string    `json:"account_id"`
	PlanID      string    `json:"plan_id"`
	PlanVersion uint64    `json:"plan_version"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	PaidAt      time.Time `json:"paid_at,omitempty"`
}

// PaymentEvent 是 provider webhook 的规范化输入；EventID 是幂等键。
type PaymentEvent struct {
	EventID     string `json:"event_id"`
	Type        string `json:"type"`
	OrderID     string `json:"order_id"`
	AccountID   string `json:"account_id"`
	PlanID      string `json:"plan_id"`
	PlanVersion uint64 `json:"plan_version"`
}

// CommerceService 拥有 staging 浏览器 session、订单状态机和 webhook 幂等记录。
// 生产部署应替换 store/provider，但必须保持相同事务顺序和失败语义。
type CommerceService struct {
	mu        sync.Mutex
	now       func() time.Time
	publisher EntitlementProjectionPublisher
	catalog   Catalog
	secret    []byte
	sessions  map[[sha256.Size]byte]CommerceSession
	orders    map[string]Order
	events    map[string]struct{}
	center    *UserCenterStore
}

// AttachUserCenter 接入支付成功后的 AFF 奖励账本；奖励仍由签名 webhook 唯一触发。
func (service *CommerceService) AttachUserCenter(center *UserCenterStore) { service.center = center }

// AccountCommerceView 是登录用户可见的最小订阅投影，不包含 provider secret 或 terminal metadata。
type AccountCommerceView struct {
	AccountID  string     `json:"account_id"`
	UserID     string     `json:"user_id"`
	Email      string     `json:"email"`
	PlanID     string     `json:"plan_id"`
	ValidUntil *time.Time `json:"valid_until,omitempty"`
	Orders     []Order    `json:"orders"`
}

// NewCommerceService 创建 commerce transaction owner；secret 至少 32 bytes，避免伪造 staging webhook。
func NewCommerceService(secret []byte, publisher EntitlementProjectionPublisher, catalog Catalog, now func() time.Time) (*CommerceService, error) {
	if len(secret) < 32 || publisher == nil {
		return nil, fmt.Errorf("commerce secret and entitlement publisher are required")
	}
	if err := validateCatalog(catalog); err != nil {
		return nil, err
	}
	if now == nil {
		now = time.Now
	}
	return &CommerceService{secret: append([]byte(nil), secret...), publisher: publisher, catalog: catalog, now: now, sessions: make(map[[sha256.Size]byte]CommerceSession), orders: make(map[string]Order), events: make(map[string]struct{})}, nil
}

// BeginStagingSession 为已由显式 staging identity provider 确认的固定账号签发短期浏览器 session。
func (service *CommerceService) BeginStagingSession(accountID, userID, email string) (CommerceSession, error) {
	if accountID == "" || userID == "" || email == "" {
		return CommerceSession{}, ErrCommerceUnauthorized
	}
	token, err := randomToken(32)
	if err != nil {
		return CommerceSession{}, err
	}
	now := service.now().UTC()
	session := CommerceSession{Token: token, AccountID: accountID, UserID: userID, Email: email, ExpiresAt: now.Add(8 * time.Hour)}
	stored := session
	stored.Token = ""
	service.mu.Lock()
	hash := sha256.Sum256([]byte(token))
	if service.center != nil {
		err = service.center.saveSession(hash, stored)
	} else {
		service.sessions[hash] = stored
	}
	service.mu.Unlock()
	if err != nil {
		return CommerceSession{}, err
	}
	return session, nil
}

// Authenticate 读取 browser token 摘要并执行到期检查。
func (service *CommerceService) Authenticate(token string) (CommerceSession, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	key := sha256.Sum256([]byte(token))
	var session CommerceSession
	var ok bool
	if service.center != nil {
		var err error
		session, err = service.center.loadSession(key)
		ok = err == nil
	} else {
		session, ok = service.sessions[key]
	}
	if !ok || !service.now().UTC().Before(session.ExpiresAt) {
		if service.center != nil {
			service.center.deleteSession(key)
		} else {
			delete(service.sessions, key)
		}
		return CommerceSession{}, ErrCommerceUnauthorized
	}
	return session, nil
}

// EndSession 立即撤销浏览器 bearer 摘要；退出后即使 Cookie 被复制也不能再次认证。
func (service *CommerceService) EndSession(token string) {
	service.mu.Lock()
	defer service.mu.Unlock()
	key := sha256.Sum256([]byte(token))
	if service.center != nil {
		service.center.deleteSession(key)
	} else {
		delete(service.sessions, key)
	}
}

// CreateCheckout 创建 pending order；只接受 catalog 中精确 versioned、可购买的套餐，绝不在此时更新 entitlement。
func (service *CommerceService) CreateCheckout(session CommerceSession, planID string) (Order, error) {
	plan, ok := service.catalog.Plan(planID)
	if session.AccountID == "" || !ok || plan.Price.Mode == "included" || plan.BillingPeriodDays == 0 {
		return Order{}, ErrCommerceConflict
	}
	id, err := randomToken(18)
	if err != nil {
		return Order{}, err
	}
	order := Order{ID: "order-" + id, AccountID: session.AccountID, PlanID: plan.ID, PlanVersion: plan.Version, Status: "pending", CreatedAt: service.now().UTC()}
	service.mu.Lock()
	if service.center != nil {
		err = service.center.saveOrder(order)
	} else {
		service.orders[order.ID] = order
	}
	service.mu.Unlock()
	if err != nil {
		return Order{}, err
	}
	return order, nil
}

// AccountView 返回当前 session 自己的订单和由 paid order 推导的套餐投影。
func (service *CommerceService) AccountView(session CommerceSession) AccountCommerceView {
	service.mu.Lock()
	defer service.mu.Unlock()
	view := AccountCommerceView{AccountID: session.AccountID, UserID: session.UserID, Email: session.Email, Orders: []Order{}}
	if included, ok := service.catalog.IncludedPlan(); ok {
		view.PlanID = included.ID
	}
	orders := service.orders
	if service.center != nil {
		orders = map[string]Order{}
		for _, order := range service.center.accountOrders(session.AccountID) {
			orders[order.ID] = order
		}
	}
	for _, order := range orders {
		if order.AccountID != session.AccountID {
			continue
		}
		view.Orders = append(view.Orders, order)
		if order.Status == "paid" {
			plan, ok := service.catalog.Plan(order.PlanID)
			if !ok || plan.Version != order.PlanVersion {
				continue
			}
			view.PlanID = order.PlanID
			bonusDays := 0
			if service.center != nil {
				bonusDays = service.center.ReferralRewardDays(session.AccountID)
			}
			validUntil := order.PaidAt.Add(time.Duration(int(plan.BillingPeriodDays)+bonusDays) * 24 * time.Hour)
			view.ValidUntil = &validUntil
		}
	}
	return view
}

// ConfirmStagingPayment 模拟显式 staging provider 的 checkout 完成，但仍通过签名 webhook 事务提交。
func (service *CommerceService) ConfirmStagingPayment(session CommerceSession, orderID string) (Order, error) {
	service.mu.Lock()
	order, ok := service.orders[orderID]
	if service.center != nil {
		var err error
		order, err = service.center.order(orderID)
		ok = err == nil
	}
	service.mu.Unlock()
	if !ok || order.AccountID != session.AccountID || order.Status != "pending" {
		return Order{}, ErrCommerceConflict
	}
	eventID, err := randomToken(18)
	if err != nil {
		return Order{}, err
	}
	body, _ := json.Marshal(PaymentEvent{EventID: "event-" + eventID, Type: "payment.succeeded", OrderID: order.ID, AccountID: order.AccountID, PlanID: order.PlanID, PlanVersion: order.PlanVersion})
	return service.ApplyWebhook(body, service.SignStagingEvent(body))
}

// SignStagingEvent 仅供显式 staging provider 生成 webhook signature；生产 provider 使用其官方验签器。
func (service *CommerceService) SignStagingEvent(body []byte) string {
	mac := hmac.New(sha256.New, service.secret)
	_, _ = mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// ApplyWebhook 验签并幂等提交 payment.succeeded -> entitlement -> paid order。
func (service *CommerceService) ApplyWebhook(body []byte, signature string) (Order, error) {
	expected, err := hex.DecodeString(signature)
	if err != nil || !hmac.Equal(expected, mustMAC(service.secret, body)) {
		return Order{}, ErrPaymentSignature
	}
	var event PaymentEvent
	if json.Unmarshal(body, &event) != nil || event.EventID == "" || event.Type != "payment.succeeded" {
		return Order{}, ErrCommerceConflict
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	order, ok := service.orders[event.OrderID]
	if service.center != nil {
		var loadErr error
		order, loadErr = service.center.order(event.OrderID)
		ok = loadErr == nil
	}
	if !ok || order.AccountID != event.AccountID || order.PlanID != event.PlanID || order.PlanVersion != event.PlanVersion {
		return Order{}, ErrCommerceConflict
	}
	plan, ok := service.catalog.Plan(order.PlanID)
	if !ok || plan.Version != order.PlanVersion || plan.BillingPeriodDays == 0 {
		return Order{}, ErrCommerceConflict
	}
	duplicate := false
	if service.center != nil {
		duplicate = service.center.paymentApplied(event.EventID)
	} else {
		_, duplicate = service.events[event.EventID]
	}
	if duplicate {
		return order, nil
	}
	if order.Status != "pending" {
		return Order{}, ErrCommerceConflict
	}
	paidAt := service.now().UTC()
	referrer := ""
	if service.center != nil {
		referrer = service.center.referralReferrer(order.AccountID)
	}
	paidBonus := 0
	if service.center != nil {
		paidBonus = service.center.ReferralRewardDays(order.AccountID)
		if referrer != "" {
			paidBonus += 7
		}
	}
	if err := service.publisher.PublishSubscription(subscriptionForOrder(order, paidAt, plan.BillingPeriodDays, paidBonus)); err != nil {
		return Order{}, err
	}
	if referrer != "" {
		candidates := service.orders
		if service.center != nil {
			candidates = map[string]Order{}
			for _, candidate := range service.center.accountOrders(referrer) {
				candidates[candidate.ID] = candidate
			}
		}
		for _, candidate := range candidates {
			if candidate.AccountID == referrer && candidate.Status == "paid" {
				candidatePlan, exists := service.catalog.Plan(candidate.PlanID)
				if !exists || candidatePlan.Version != candidate.PlanVersion || candidatePlan.BillingPeriodDays == 0 {
					return Order{}, ErrCommerceConflict
				}
				bonus := service.center.ReferralRewardDays(referrer) + 15
				if err := service.publisher.PublishSubscription(subscriptionForOrder(candidate, candidate.PaidAt, candidatePlan.BillingPeriodDays, bonus)); err != nil {
					return Order{}, err
				}
				break
			}
		}
	}
	order.Status, order.PaidAt = "paid", paidAt
	if service.center != nil {
		if err := service.center.commitPayment(event.EventID, order, referrer); err != nil {
			return Order{}, err
		}
	} else {
		service.orders[order.ID] = order
		service.events[event.EventID] = struct{}{}
	}
	return order, nil
}

func subscriptionForOrder(order Order, periodStart time.Time, billingPeriodDays uint32, bonusDays int) *cloudpb.SubscriptionProjection {
	periodStart = periodStart.UTC()
	return &cloudpb.SubscriptionProjection{
		SubscriptionId:               "subscription-" + order.ID,
		AccountId:                    order.AccountID,
		SourceOrderId:                order.ID,
		PlanId:                       order.PlanID,
		PlanVersion:                  order.PlanVersion,
		Status:                       cloudpb.SubscriptionStatus_SUBSCRIPTION_STATUS_ACTIVE,
		CurrentPeriodStartUnixMillis: periodStart.UnixMilli(),
		CurrentPeriodEndUnixMillis:   periodStart.Add(time.Duration(int(billingPeriodDays)+bonusDays) * 24 * time.Hour).UnixMilli(),
		UpdatedAtUnixMillis:          periodStart.UnixMilli(),
	}
}

func cloneSubscription(subscription *cloudpb.SubscriptionProjection) *cloudpb.SubscriptionProjection {
	if subscription == nil {
		return nil
	}
	return proto.Clone(subscription).(*cloudpb.SubscriptionProjection)
}

func randomToken(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

func mustMAC(secret, body []byte) []byte {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(body)
	return mac.Sum(nil)
}
