package creem

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/muxvia/muxvia/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

const (
	defaultReconcileWindow = 20 * time.Minute
	initialReconcileDelay  = 5 * time.Second
	subscriptionPollDelay  = 5 * time.Minute
)

// Commerce 是 Creem adapter 可以调用的唯一产品边界。
// adapter 不得直接读写 PostgreSQL、Subscription 或 Entitlement。
type Commerce interface {
	CreatePaymentAttempt(context.Context, string, string, *cloudpb.CreatePaymentAttemptRequest) (*cloudpb.CreatePaymentAttemptResponse, error)
	UpdateProviderPaymentAttempt(context.Context, *cloudpb.PaymentAttemptProjection, uint64) error
	ProviderPaymentContext(context.Context, string) (*cloudpb.AccountProjection, *cloudpb.OrderProjection, *cloudpb.PaymentAttemptProjection, error)
	PendingProviderPaymentAttempts(context.Context, string, time.Time, int) ([]*cloudpb.PaymentAttemptProjection, error)
	ProviderPaymentAttemptByReference(context.Context, string, string) (*cloudpb.PaymentAttemptProjection, error)
	ApplyPaymentEvent(context.Context, *cloudpb.ApplyPaymentEventRequest) (*cloudpb.ApplyPaymentEventResponse, error)
}

// API 是 Service 使用的 Creem REST surface，便于用真实 HTTP harness 验证协议。
type API interface {
	CreateCheckout(context.Context, string, string, string, string, string, map[string]string) (*Checkout, error)
	Checkout(context.Context, string) (*Checkout, error)
	Transaction(context.Context, string) (*Transaction, error)
	Subscription(context.Context, string) (*Subscription, error)
	Product(context.Context, string) (*Product, error)
	Discount(context.Context, string) (*Discount, error)
}

// ServiceConfig 固定 provider API、Commerce、success URL 与 reconciliation 时间窗。
type ServiceConfig struct {
	API             API
	Commerce        Commerce
	SuccessURL      string
	Now             func() time.Time
	ReconcileWindow time.Duration
	WebhookSecret   string
}

// Service 编排 Creem checkout、服务端核验与 normalized journal。
type Service struct {
	api             API
	commerce        Commerce
	successURL      string
	now             func() time.Time
	reconcileWindow time.Duration
	webhookSecret   []byte
}

// NewService 创建 Creem provider 应用边界；success URL 只用于返回页面，不作为支付真值。
func NewService(config ServiceConfig) (*Service, error) {
	parsed, err := url.Parse(config.SuccessURL)
	if config.API == nil || config.Commerce == nil || err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return nil, ErrInvalid
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.ReconcileWindow == 0 {
		config.ReconcileWindow = defaultReconcileWindow
	}
	if config.ReconcileWindow < time.Minute || config.ReconcileWindow > time.Hour {
		return nil, ErrInvalid
	}
	return &Service{api: config.API, commerce: config.Commerce, successURL: config.SuccessURL, now: config.Now, reconcileWindow: config.ReconcileWindow, webhookSecret: []byte(config.WebhookSecret)}, nil
}

// WebhookHandler 返回固定 `POST /pay/creem` handler；未配置 secret 时 fail closed。
func (service *Service) WebhookHandler() http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "no-store")
		if request.Method != http.MethodPost {
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if len(service.webhookSecret) == 0 {
			http.Error(writer, "payment webhook unavailable", http.StatusServiceUnavailable)
			return
		}
		body, err := io.ReadAll(io.LimitReader(request.Body, maxResponseBody+1))
		if err != nil || len(body) > maxResponseBody {
			http.Error(writer, "invalid webhook body", http.StatusBadRequest)
			return
		}
		if !verifySignature(body, request.Header.Get("creem-signature"), service.webhookSecret) {
			http.Error(writer, "invalid webhook signature", http.StatusUnauthorized)
			return
		}
		if err := service.handleWebhook(request.Context(), body); err != nil {
			http.Error(writer, "payment webhook processing unavailable", http.StatusServiceUnavailable)
			return
		}
		writer.WriteHeader(http.StatusOK)
	})
}

type webhookEnvelope struct {
	ID        string          `json:"id"`
	EventType string          `json:"eventType"`
	CreatedAt int64           `json:"created_at"`
	Object    json.RawMessage `json:"object"`
}

type webhookProduct struct {
	ID       string `json:"id"`
	Currency string `json:"currency"`
}

type webhookSubscription struct {
	ID                string            `json:"id"`
	Status            string            `json:"status"`
	Product           webhookProduct    `json:"product"`
	LastTransactionID string            `json:"last_transaction_id"`
	UpdatedAt         string            `json:"updated_at"`
	Metadata          map[string]string `json:"metadata"`
}

type webhookRefund struct {
	ID          string       `json:"id"`
	Status      string       `json:"status"`
	Transaction *Transaction `json:"transaction"`
}

func verifySignature(body []byte, signature string, secret []byte) bool {
	provided, err := hex.DecodeString(strings.TrimSpace(signature))
	if err != nil || len(provided) != sha256.Size {
		return false
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(body)
	return hmac.Equal(mac.Sum(nil), provided)
}

func (service *Service) handleWebhook(ctx context.Context, body []byte) error {
	envelope := &webhookEnvelope{}
	if json.Unmarshal(body, envelope) != nil || envelope.ID == "" || envelope.EventType == "" || len(envelope.Object) == 0 {
		return ErrInvalid
	}
	switch envelope.EventType {
	case "checkout.completed":
		checkout := &Checkout{}
		if json.Unmarshal(envelope.Object, checkout) != nil || checkout.ID == "" {
			return ErrInvalid
		}
		attempt, err := service.attemptFromMetadataOrReference(ctx, checkout.Metadata, checkout.ID)
		if err != nil {
			return err
		}
		return service.reconcileAttempt(ctx, attempt, service.now().UTC())
	case "subscription.paid":
		subscription := &webhookSubscription{}
		if json.Unmarshal(envelope.Object, subscription) != nil || subscription.ID == "" || subscription.LastTransactionID == "" {
			return ErrInvalid
		}
		attempt, err := service.attemptFromMetadataOrReference(ctx, subscription.Metadata, subscription.ID)
		if err != nil {
			return err
		}
		_, order, attempt, err := service.commerce.ProviderPaymentContext(ctx, attempt.GetPaymentAttemptId())
		if err != nil {
			return err
		}
		checkout, err := service.api.Checkout(ctx, attempt.GetProviderReference())
		if err != nil {
			return err
		}
		transaction, err := service.api.Transaction(ctx, subscription.LastTransactionID)
		if err != nil {
			return err
		}
		return service.applyTransaction(ctx, order, attempt, checkout, transaction, service.now().UTC())
	case "refund.created", "dispute.created":
		refund := &webhookRefund{}
		if json.Unmarshal(envelope.Object, refund) != nil || refund.Transaction == nil || refund.Transaction.ID == "" {
			return ErrInvalid
		}
		attempt, err := service.providerAttempt(refund.Transaction.ID, ctx)
		if err != nil && refund.Transaction.Subscription != "" {
			attempt, err = service.providerAttempt(string(refund.Transaction.Subscription), ctx)
		}
		if err != nil {
			return err
		}
		_, order, attempt, err := service.commerce.ProviderPaymentContext(ctx, attempt.GetPaymentAttemptId())
		if err != nil {
			return err
		}
		checkout, err := service.api.Checkout(ctx, attempt.GetProviderReference())
		if err != nil {
			return err
		}
		transaction, err := service.api.Transaction(ctx, refund.Transaction.ID)
		if err != nil {
			return err
		}
		return service.applyTransaction(ctx, order, attempt, checkout, transaction, service.now().UTC())
	case "subscription.active", "subscription.scheduled_cancel", "subscription.canceled", "subscription.past_due", "subscription.expired", "subscription.paused":
		return service.applySubscriptionLifecycle(ctx, envelope)
	default:
		// 未识别事件不改变产品状态；Creem 新增事件不会触发错误授权或无界重试。
		return nil
	}
}

func (service *Service) attemptFromMetadataOrReference(ctx context.Context, metadata map[string]string, reference string) (*cloudpb.PaymentAttemptProjection, error) {
	if attemptID := metadata["muxvia_payment_attempt_id"]; attemptID != "" {
		_, _, attempt, err := service.commerce.ProviderPaymentContext(ctx, attemptID)
		return attempt, err
	}
	return service.providerAttempt(reference, ctx)
}

func (service *Service) applySubscriptionLifecycle(ctx context.Context, envelope *webhookEnvelope) error {
	subscription := &webhookSubscription{}
	if json.Unmarshal(envelope.Object, subscription) != nil || subscription.ID == "" || subscription.Product.ID == "" || subscription.Product.Currency == "" {
		return ErrInvalid
	}
	attempt, err := service.attemptFromMetadataOrReference(ctx, subscription.Metadata, subscription.ID)
	if err != nil {
		return err
	}
	_, order, attempt, err := service.commerce.ProviderPaymentContext(ctx, attempt.GetPaymentAttemptId())
	if err != nil {
		return err
	}
	return service.applySubscriptionState(ctx, order, attempt, subscription, envelope.ID)
}

func (service *Service) applySubscriptionState(ctx context.Context, order *cloudpb.OrderProjection, attempt *cloudpb.PaymentAttemptProjection, subscription *webhookSubscription, source string) error {
	if order == nil || attempt == nil || subscription == nil || subscription.ID == "" || subscription.Product.ID == "" || subscription.Product.Currency == "" {
		return ErrInvalid
	}
	if subscription.Status == "active" && attempt.GetStatus() != cloudpb.PaymentAttemptStatus_PAYMENT_ATTEMPT_STATUS_SUCCEEDED {
		return nil
	}
	eventType := map[string]cloudpb.PaymentEventType{
		"active":           cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_SUBSCRIPTION_ACTIVE_SYNC,
		"scheduled_cancel": cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_SUBSCRIPTION_SCHEDULED_CANCEL,
		"canceled":         cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_SUBSCRIPTION_CANCELED,
		"past_due":         cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_SUBSCRIPTION_PAST_DUE,
		"unpaid":           cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_SUBSCRIPTION_PAST_DUE,
		"expired":          cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_SUBSCRIPTION_EXPIRED,
		"paused":           cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_SUBSCRIPTION_PAUSED,
	}[subscription.Status]
	if eventType == cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_UNSPECIFIED {
		return ErrInvalid
	}
	revision := strings.NewReplacer(":", "-", ".", "-", "+", "-").Replace(subscription.UpdatedAt)
	if revision == "" {
		revision = source
	}
	event := service.event(order, attempt, "creem:subscription:"+subscription.ID+":"+subscription.Status+":"+revision, eventType, subscription.ID, service.now().UTC())
	event.ProviderProductId = subscription.Product.ID
	event.Currency = subscription.Product.Currency
	event.ProviderSubscriptionReference = subscription.ID
	_, err := service.commerce.ApplyPaymentEvent(ctx, &cloudpb.ApplyPaymentEventRequest{Event: event})
	return err
}

// StartCheckout 创建持久 payment attempt 后调用 Creem；API timeout 只留下可恢复 attempt。
func (service *Service) StartCheckout(ctx context.Context, account *cloudpb.AccountProjection, order *cloudpb.OrderProjection) (*cloudpb.PaymentAttemptProjection, string, error) {
	if account == nil || order == nil || account.GetAccountId() != order.GetAccountId() || account.GetEmail() == "" || order.GetStatus() != cloudpb.OrderStatus_ORDER_STATUS_PENDING || order.GetProviderProductId() == "" {
		return nil, "", ErrInvalid
	}
	created, err := service.commerce.CreatePaymentAttempt(ctx, account.GetAccountId(), account.GetUserId(), &cloudpb.CreatePaymentAttemptRequest{OrderId: order.GetOrderId(), Provider: ProviderName})
	if err != nil {
		return nil, "", err
	}
	attempt := created.GetPaymentAttempt()
	now := service.now().UTC()
	attempt, err = service.scheduleAttempt(ctx, attempt, now, "creating")
	if err != nil {
		return nil, "", err
	}
	checkout, err := service.createCheckout(ctx, account, order, attempt)
	if err != nil {
		_, _ = service.rescheduleAttempt(ctx, attempt, now, "create_error")
		return attempt, "", err
	}
	attempt, err = service.bindCheckout(ctx, attempt, checkout, now)
	if err != nil {
		return nil, "", err
	}
	return attempt, checkout.CheckoutURL, nil
}

// ReconcileOnce 处理有界批次的到期 attempt；Webhook 与本方法最终调用同一 ApplyPaymentEvent。
func (service *Service) ReconcileOnce(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 200 {
		return 0, ErrInvalid
	}
	now := service.now().UTC()
	attempts, err := service.commerce.PendingProviderPaymentAttempts(ctx, ProviderName, now, limit)
	if err != nil {
		return 0, err
	}
	processed := 0
	for _, attempt := range attempts {
		var reconcileErr error
		if attempt.GetStatus() == cloudpb.PaymentAttemptStatus_PAYMENT_ATTEMPT_STATUS_SUCCEEDED {
			reconcileErr = service.reconcileSubscription(ctx, attempt, now)
		} else {
			reconcileErr = service.reconcileAttempt(ctx, attempt, now)
		}
		if reconcileErr != nil {
			_, _, latest, loadErr := service.commerce.ProviderPaymentContext(ctx, attempt.GetPaymentAttemptId())
			if loadErr != nil || !reconcileableAttempt(latest) {
				continue
			}
			if _, updateErr := service.rescheduleAttempt(ctx, latest, now, "api_error"); updateErr != nil {
				return processed, updateErr
			}
			continue
		}
		processed++
	}
	return processed, nil
}

// ReconcilePaymentAttempt 立即核对一个持久 Creem attempt，供受权限保护的运营入口使用。
// 它不会信任浏览器返回值；订单和订阅只能由 Creem API 响应经同一 normalized journal 改变。
func (service *Service) ReconcilePaymentAttempt(ctx context.Context, attemptID string) (*cloudpb.PaymentAttemptProjection, error) {
	if attemptID == "" {
		return nil, ErrInvalid
	}
	_, _, attempt, err := service.commerce.ProviderPaymentContext(ctx, attemptID)
	if err != nil {
		return nil, err
	}
	if attempt.GetProvider() != ProviderName || !reconcileableAttempt(attempt) {
		return nil, ErrInvalid
	}
	now := service.now().UTC()
	if attempt.GetStatus() == cloudpb.PaymentAttemptStatus_PAYMENT_ATTEMPT_STATUS_SUCCEEDED {
		err = service.reconcileSubscription(ctx, attempt, now)
	} else {
		err = service.reconcileAttempt(ctx, attempt, now)
	}
	if err != nil {
		_, _, latest, loadErr := service.commerce.ProviderPaymentContext(ctx, attemptID)
		if loadErr == nil && reconcileableAttempt(latest) {
			_, _ = service.rescheduleAttempt(ctx, latest, now, "operator_api_error")
		}
		return nil, err
	}
	_, _, latest, err := service.commerce.ProviderPaymentContext(ctx, attemptID)
	return latest, err
}

func reconcileableAttempt(attempt *cloudpb.PaymentAttemptProjection) bool {
	return attempt != nil && (attempt.GetStatus() == cloudpb.PaymentAttemptStatus_PAYMENT_ATTEMPT_STATUS_PENDING || attempt.GetStatus() == cloudpb.PaymentAttemptStatus_PAYMENT_ATTEMPT_STATUS_SUCCEEDED && attempt.GetProviderSubscriptionReference() != "")
}

func (service *Service) reconcileSubscription(ctx context.Context, attempt *cloudpb.PaymentAttemptProjection, now time.Time) error {
	_, order, current, err := service.commerce.ProviderPaymentContext(ctx, attempt.GetPaymentAttemptId())
	if err != nil || current.GetStatus() != cloudpb.PaymentAttemptStatus_PAYMENT_ATTEMPT_STATUS_SUCCEEDED || current.GetProviderSubscriptionReference() == "" {
		return err
	}
	subscription, err := service.api.Subscription(ctx, current.GetProviderSubscriptionReference())
	if err != nil {
		return err
	}
	if subscription.ID != current.GetProviderSubscriptionReference() || string(subscription.Product) != order.GetProviderProductId() || subscription.Status == "" {
		return ErrInvalid
	}
	product, err := service.api.Product(ctx, string(subscription.Product))
	if err != nil || product.ID != order.GetProviderProductId() || !strings.EqualFold(product.Currency, order.GetPrice().GetCurrency()) {
		return ErrInvalid
	}
	providerSubscription := &webhookSubscription{ID: subscription.ID, Status: subscription.Status, Product: webhookProduct{ID: product.ID, Currency: product.Currency}, LastTransactionID: subscription.LastTransactionID, UpdatedAt: subscription.UpdatedAt, Metadata: subscription.Metadata}
	if err := service.applySubscriptionState(ctx, order, current, providerSubscription, "poll"); err != nil {
		return err
	}
	_, _, latest, err := service.commerce.ProviderPaymentContext(ctx, current.GetPaymentAttemptId())
	if err != nil {
		return err
	}
	if subscription.Status == "canceled" || subscription.Status == "expired" {
		return service.stopSubscriptionPoll(ctx, latest, now, subscription.Status)
	}
	return service.scheduleSubscriptionPoll(ctx, latest, now)
}

func (service *Service) reconcileAttempt(ctx context.Context, attempt *cloudpb.PaymentAttemptProjection, now time.Time) error {
	account, order, current, err := service.commerce.ProviderPaymentContext(ctx, attempt.GetPaymentAttemptId())
	if err != nil || current.GetStatus() != cloudpb.PaymentAttemptStatus_PAYMENT_ATTEMPT_STATUS_PENDING {
		return err
	}
	if current.GetReconcileDeadlineUnixMillis() > 0 && now.UnixMilli() >= current.GetReconcileDeadlineUnixMillis() {
		return service.applyCheckoutFailure(ctx, order, current, "deadline", now)
	}
	var checkout *Checkout
	if current.GetProviderReference() == "" {
		checkout, err = service.createCheckout(ctx, account, order, current)
		if err == nil {
			current, err = service.bindCheckout(ctx, current, checkout, now)
		}
	} else {
		checkout, err = service.api.Checkout(ctx, current.GetProviderReference())
	}
	if err != nil {
		return err
	}
	if !validCheckoutIdentity(checkout, order) {
		return ErrInvalid
	}
	switch checkout.Status {
	case "pending", "processing":
		_, err = service.rescheduleAttempt(ctx, current, now, checkout.Status)
		return err
	case "expired":
		return service.applyCheckoutFailure(ctx, order, current, "expired", now)
	case "completed":
		if checkout.Order == nil || checkout.Order.Transaction == "" {
			return ErrInvalid
		}
		transaction, transactionErr := service.api.Transaction(ctx, string(checkout.Order.Transaction))
		if transactionErr != nil {
			return transactionErr
		}
		return service.applyTransaction(ctx, order, current, checkout, transaction, now)
	default:
		return ErrInvalid
	}
}

func (service *Service) createCheckout(ctx context.Context, account *cloudpb.AccountProjection, order *cloudpb.OrderProjection, attempt *cloudpb.PaymentAttemptProjection) (*Checkout, error) {
	product, err := service.api.Product(ctx, order.GetProviderProductId())
	if err != nil {
		return nil, err
	}
	period := "every-month"
	if order.GetBillingCadence() == cloudpb.BillingCadence_BILLING_CADENCE_YEARLY {
		period = "every-year"
	}
	if product.ID != order.GetProviderProductId() || product.Status != "active" || product.Price != order.GetSubtotalMinor() || !strings.EqualFold(product.Currency, order.GetPrice().GetCurrency()) || product.BillingType != "recurring" || product.BillingPeriod != period {
		return nil, ErrInvalid
	}
	discountCode := ""
	if order.GetPromotion() != nil {
		discountCode = order.GetPromotion().GetCreemDiscountCode()
	}
	metadata := map[string]string{"muxvia_order_id": order.GetOrderId(), "muxvia_payment_attempt_id": attempt.GetPaymentAttemptId()}
	checkout, err := service.api.CreateCheckout(ctx, order.GetProviderProductId(), order.GetOrderId(), account.GetEmail(), discountCode, service.successURL, metadata)
	if err != nil {
		return nil, err
	}
	if !validCheckoutIdentity(checkout, order) || !validCheckoutURL(checkout.CheckoutURL) {
		return nil, ErrInvalid
	}
	if order.GetPromotion() != nil {
		if checkout.Discount == nil || checkout.Discount.ID == "" || !strings.EqualFold(checkout.Discount.Code, discountCode) {
			return nil, ErrInvalid
		}
	} else if checkout.Discount != nil && checkout.Discount.ID != "" {
		return nil, ErrInvalid
	}
	return checkout, nil
}

func validCheckoutIdentity(checkout *Checkout, order *cloudpb.OrderProjection) bool {
	if checkout == nil || checkout.ID == "" || checkout.RequestID != order.GetOrderId() || string(checkout.Product) != order.GetProviderProductId() {
		return false
	}
	if checkout.Order == nil {
		return true
	}
	providerSubtotal := checkout.Order.Subtotal
	if providerSubtotal == 0 {
		providerSubtotal = checkout.Order.Amount
	}
	return string(checkout.Order.Product) == order.GetProviderProductId() && strings.EqualFold(checkout.Order.Currency, order.GetPrice().GetCurrency()) && providerSubtotal == order.GetSubtotalMinor() && checkout.Order.DiscountAmount == order.GetDiscountMinor()
}

func validCheckoutURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "https" && parsed.Host != ""
}

func (service *Service) scheduleAttempt(ctx context.Context, attempt *cloudpb.PaymentAttemptProjection, now time.Time, status string) (*cloudpb.PaymentAttemptProjection, error) {
	next := proto.Clone(attempt).(*cloudpb.PaymentAttemptProjection)
	next.Revision++
	next.UpdatedAtUnixMillis = now.UnixMilli()
	next.ReconcileAfterUnixMillis = now.Add(initialReconcileDelay).UnixMilli()
	next.ReconcileDeadlineUnixMillis = now.Add(service.reconcileWindow).UnixMilli()
	next.LastProviderStatus = status
	if err := service.commerce.UpdateProviderPaymentAttempt(ctx, next, attempt.GetRevision()); err != nil {
		return nil, err
	}
	return next, nil
}

func (service *Service) bindCheckout(ctx context.Context, attempt *cloudpb.PaymentAttemptProjection, checkout *Checkout, now time.Time) (*cloudpb.PaymentAttemptProjection, error) {
	next := proto.Clone(attempt).(*cloudpb.PaymentAttemptProjection)
	next.Revision++
	next.ProviderReference = checkout.ID
	if checkout.Discount != nil {
		next.ProviderDiscountReference = checkout.Discount.ID
	}
	next.ProviderSubscriptionReference = string(checkout.Subscription)
	next.UpdatedAtUnixMillis = now.UnixMilli()
	next.ReconcileAttempts++
	next.ReconcileAfterUnixMillis = now.Add(initialReconcileDelay).UnixMilli()
	next.LastProviderStatus = checkout.Status
	if err := service.commerce.UpdateProviderPaymentAttempt(ctx, next, attempt.GetRevision()); err != nil {
		return nil, err
	}
	return next, nil
}

func (service *Service) rescheduleAttempt(ctx context.Context, attempt *cloudpb.PaymentAttemptProjection, now time.Time, status string) (*cloudpb.PaymentAttemptProjection, error) {
	next := proto.Clone(attempt).(*cloudpb.PaymentAttemptProjection)
	next.Revision++
	next.UpdatedAtUnixMillis = now.UnixMilli()
	next.ReconcileAttempts++
	delay := time.Duration(1<<min(next.GetReconcileAttempts(), uint32(6))) * time.Second
	next.ReconcileAfterUnixMillis = now.Add(delay).UnixMilli()
	next.LastProviderStatus = status
	if err := service.commerce.UpdateProviderPaymentAttempt(ctx, next, attempt.GetRevision()); err != nil {
		return nil, err
	}
	return next, nil
}

func (service *Service) applyCheckoutFailure(ctx context.Context, order *cloudpb.OrderProjection, attempt *cloudpb.PaymentAttemptProjection, status string, now time.Time) error {
	reference := attempt.GetProviderReference()
	if reference == "" {
		reference = attempt.GetPaymentAttemptId()
	}
	event := service.event(order, attempt, "creem:checkout:"+reference+":"+status, cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_FAILED, attempt.GetProviderReference(), now)
	_, err := service.commerce.ApplyPaymentEvent(ctx, &cloudpb.ApplyPaymentEventRequest{Event: event})
	return err
}

func (service *Service) applyTransaction(ctx context.Context, order *cloudpb.OrderProjection, attempt *cloudpb.PaymentAttemptProjection, checkout *Checkout, transaction *Transaction, now time.Time) error {
	if transaction == nil || transaction.ID == "" || checkout == nil || checkout.Order == nil || string(checkout.Order.Transaction) != transaction.ID || string(transaction.Order) != checkout.Order.ID || transaction.Amount != order.GetSubtotalMinor() || transaction.DiscountAmount != order.GetDiscountMinor() || !strings.EqualFold(transaction.Currency, order.GetPrice().GetCurrency()) {
		return ErrInvalid
	}
	subscriptionReference := string(transaction.Subscription)
	if subscriptionReference == "" {
		subscriptionReference = string(checkout.Subscription)
	}
	latest := proto.Clone(attempt).(*cloudpb.PaymentAttemptProjection)
	latest.ProviderSubscriptionReference = subscriptionReference
	switch transaction.Status {
	case "paid":
		return service.applyTransactionEvent(ctx, order, latest, transaction, cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_SUCCEEDED, "paid", now)
	case "refunded":
		if latest.GetStatus() == cloudpb.PaymentAttemptStatus_PAYMENT_ATTEMPT_STATUS_PENDING {
			if err := service.applyTransactionEvent(ctx, order, latest, transaction, cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_SUCCEEDED, "paid", now); err != nil {
				return err
			}
			_, order, latest, _ = service.commerce.ProviderPaymentContext(ctx, latest.GetPaymentAttemptId())
		}
		return service.applyTransactionEvent(ctx, order, latest, transaction, cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_REFUNDED, "refunded", now)
	case "chargedBack", "chargeback":
		if latest.GetStatus() == cloudpb.PaymentAttemptStatus_PAYMENT_ATTEMPT_STATUS_PENDING {
			if err := service.applyTransactionEvent(ctx, order, latest, transaction, cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_SUCCEEDED, "paid", now); err != nil {
				return err
			}
			_, order, latest, _ = service.commerce.ProviderPaymentContext(ctx, latest.GetPaymentAttemptId())
		}
		return service.applyTransactionEvent(ctx, order, latest, transaction, cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_CHARGEBACK, "chargeback", now)
	case "declined", "uncollectible", "void", "canceled":
		return service.applyTransactionEvent(ctx, order, latest, transaction, cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_FAILED, transaction.Status, now)
	case "pending", "partialRefund", "partially_refunded":
		return nil
	default:
		return ErrInvalid
	}
}

func (service *Service) applyTransactionEvent(ctx context.Context, order *cloudpb.OrderProjection, attempt *cloudpb.PaymentAttemptProjection, transaction *Transaction, eventType cloudpb.PaymentEventType, status string, now time.Time) error {
	event := service.event(order, attempt, "creem:transaction:"+transaction.ID+":"+status, eventType, transaction.ID, now)
	event.ProviderSubscriptionReference = string(transaction.Subscription)
	_, err := service.commerce.ApplyPaymentEvent(ctx, &cloudpb.ApplyPaymentEventRequest{Event: event})
	if err != nil || eventType != cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_SUCCEEDED || event.GetProviderSubscriptionReference() == "" {
		return err
	}
	_, _, latest, err := service.commerce.ProviderPaymentContext(ctx, attempt.GetPaymentAttemptId())
	if err != nil {
		return err
	}
	return service.scheduleSubscriptionPoll(ctx, latest, now)
}

func (service *Service) scheduleSubscriptionPoll(ctx context.Context, attempt *cloudpb.PaymentAttemptProjection, now time.Time) error {
	next := proto.Clone(attempt).(*cloudpb.PaymentAttemptProjection)
	next.Revision++
	next.UpdatedAtUnixMillis = now.UnixMilli()
	next.ReconcileAfterUnixMillis = now.Add(subscriptionPollDelay).UnixMilli()
	next.ReconcileDeadlineUnixMillis = 0
	next.LastProviderStatus = "subscription_sync"
	return service.commerce.UpdateProviderPaymentAttempt(ctx, next, attempt.GetRevision())
}

func (service *Service) stopSubscriptionPoll(ctx context.Context, attempt *cloudpb.PaymentAttemptProjection, now time.Time, status string) error {
	next := proto.Clone(attempt).(*cloudpb.PaymentAttemptProjection)
	next.Revision++
	next.UpdatedAtUnixMillis = now.UnixMilli()
	next.ReconcileAfterUnixMillis = 0
	next.ReconcileDeadlineUnixMillis = 0
	next.LastProviderStatus = status
	return service.commerce.UpdateProviderPaymentAttempt(ctx, next, attempt.GetRevision())
}

func (service *Service) event(order *cloudpb.OrderProjection, attempt *cloudpb.PaymentAttemptProjection, eventID string, eventType cloudpb.PaymentEventType, providerReference string, now time.Time) *cloudpb.NormalizedPaymentEvent {
	return &cloudpb.NormalizedPaymentEvent{
		ProviderEventId: eventID, Provider: ProviderName, EventType: eventType, OrderId: order.GetOrderId(), AccountId: order.GetAccountId(), PlanId: order.GetPlanId(), PlanVersion: order.GetPlanVersion(), ProviderReference: providerReference, OccurredAtUnixMillis: now.UnixMilli(), PaymentAttemptId: attempt.GetPaymentAttemptId(), ProviderProductId: order.GetProviderProductId(), Currency: order.GetPrice().GetCurrency(), SubtotalMinor: order.GetSubtotalMinor(), DiscountMinor: order.GetDiscountMinor(), ProviderDiscountReference: attempt.GetProviderDiscountReference(), ProviderSubscriptionReference: attempt.GetProviderSubscriptionReference(),
	}
}

func (service *Service) providerAttempt(reference string, ctx context.Context) (*cloudpb.PaymentAttemptProjection, error) {
	if reference == "" {
		return nil, fmt.Errorf("%w: provider reference is missing", ErrInvalid)
	}
	return service.commerce.ProviderPaymentAttemptByReference(ctx, ProviderName, reference)
}
