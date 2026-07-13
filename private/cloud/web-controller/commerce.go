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
)

var (
	ErrCommerceUnauthorized = errors.New("commerce session is unauthorized")
	ErrCommerceConflict     = errors.New("commerce state conflict")
	ErrPaymentSignature     = errors.New("payment webhook signature is invalid")
)

// EntitlementProjectionPublisher 是付款事务对 Control Plane 的唯一写边界。
// 实现必须在持久 entitlement 更新后发布 Hub snapshot；它不得接触 terminal capability。
type EntitlementProjectionPublisher interface {
	Activate(accountID, planID, orderID string, validUntil time.Time) error
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
	ID        string    `json:"id"`
	AccountID string    `json:"account_id"`
	PlanID    string    `json:"plan_id"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	PaidAt    time.Time `json:"paid_at,omitempty"`
}

// PaymentEvent 是 provider webhook 的规范化输入；EventID 是幂等键。
type PaymentEvent struct {
	EventID   string `json:"event_id"`
	Type      string `json:"type"`
	OrderID   string `json:"order_id"`
	AccountID string `json:"account_id"`
	PlanID    string `json:"plan_id"`
}

// CommerceService 拥有 staging 浏览器 session、订单状态机和 webhook 幂等记录。
// 生产部署应替换 store/provider，但必须保持相同事务顺序和失败语义。
type CommerceService struct {
	mu        sync.Mutex
	now       func() time.Time
	publisher EntitlementProjectionPublisher
	secret    []byte
	sessions  map[[sha256.Size]byte]CommerceSession
	orders    map[string]Order
	events    map[string]struct{}
}

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
func NewCommerceService(secret []byte, publisher EntitlementProjectionPublisher, now func() time.Time) (*CommerceService, error) {
	if len(secret) < 32 || publisher == nil {
		return nil, fmt.Errorf("commerce secret and entitlement publisher are required")
	}
	if now == nil {
		now = time.Now
	}
	return &CommerceService{secret: append([]byte(nil), secret...), publisher: publisher, now: now, sessions: make(map[[sha256.Size]byte]CommerceSession), orders: make(map[string]Order), events: make(map[string]struct{})}, nil
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
	service.sessions[sha256.Sum256([]byte(token))] = stored
	service.mu.Unlock()
	return session, nil
}

// Authenticate 读取 browser token 摘要并执行到期检查。
func (service *CommerceService) Authenticate(token string) (CommerceSession, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	key := sha256.Sum256([]byte(token))
	session, ok := service.sessions[key]
	if !ok || !service.now().UTC().Before(session.ExpiresAt) {
		delete(service.sessions, key)
		return CommerceSession{}, ErrCommerceUnauthorized
	}
	return session, nil
}

// CreateCheckout 创建 pending order；当前仅允许 catalog 已声明的 Pro plan，绝不在此时更新 entitlement。
func (service *CommerceService) CreateCheckout(session CommerceSession, planID string) (Order, error) {
	if session.AccountID == "" || planID != "pro" {
		return Order{}, ErrCommerceConflict
	}
	id, err := randomToken(18)
	if err != nil {
		return Order{}, err
	}
	order := Order{ID: "order-" + id, AccountID: session.AccountID, PlanID: planID, Status: "pending", CreatedAt: service.now().UTC()}
	service.mu.Lock()
	service.orders[order.ID] = order
	service.mu.Unlock()
	return order, nil
}

// AccountView 返回当前 session 自己的订单和由 paid order 推导的套餐投影。
func (service *CommerceService) AccountView(session CommerceSession) AccountCommerceView {
	service.mu.Lock()
	defer service.mu.Unlock()
	view := AccountCommerceView{AccountID: session.AccountID, UserID: session.UserID, Email: session.Email, PlanID: "managed-free", Orders: []Order{}}
	for _, order := range service.orders {
		if order.AccountID != session.AccountID {
			continue
		}
		view.Orders = append(view.Orders, order)
		if order.Status == "paid" {
			view.PlanID = order.PlanID
			validUntil := order.PaidAt.Add(30 * 24 * time.Hour)
			view.ValidUntil = &validUntil
		}
	}
	return view
}

// ConfirmStagingPayment 模拟显式 staging provider 的 checkout 完成，但仍通过签名 webhook 事务提交。
func (service *CommerceService) ConfirmStagingPayment(session CommerceSession, orderID string) (Order, error) {
	service.mu.Lock()
	order, ok := service.orders[orderID]
	service.mu.Unlock()
	if !ok || order.AccountID != session.AccountID || order.Status != "pending" {
		return Order{}, ErrCommerceConflict
	}
	eventID, err := randomToken(18)
	if err != nil {
		return Order{}, err
	}
	body, _ := json.Marshal(PaymentEvent{EventID: "event-" + eventID, Type: "payment.succeeded", OrderID: order.ID, AccountID: order.AccountID, PlanID: order.PlanID})
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
	if !ok || order.AccountID != event.AccountID || order.PlanID != event.PlanID {
		return Order{}, ErrCommerceConflict
	}
	if _, duplicate := service.events[event.EventID]; duplicate {
		return order, nil
	}
	if order.Status != "pending" {
		return Order{}, ErrCommerceConflict
	}
	paidAt := service.now().UTC()
	if err := service.publisher.Activate(order.AccountID, order.PlanID, order.ID, paidAt.Add(30*24*time.Hour)); err != nil {
		return Order{}, err
	}
	order.Status, order.PaidAt = "paid", paidAt
	service.orders[order.ID] = order
	service.events[event.EventID] = struct{}{}
	return order, nil
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
