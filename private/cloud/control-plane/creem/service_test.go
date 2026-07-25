package creem_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/muxvia/muxvia/private/cloud/control-plane/commerce"
	"github.com/muxvia/muxvia/private/cloud/control-plane/creem"
	postgrestest "github.com/muxvia/muxvia/private/cloud/control-plane/postgrestest"
	"github.com/muxvia/muxvia/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

func TestCheckoutPollingAndRestartUseOneDurableJournal(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	store, err := postgrestest.Open(t, filepath.Join(t.TempDir(), "creem-reconcile"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	commerceService := commerceService(t, store, clock)
	registered, err := commerceService.Register(ctx, &cloudpb.RegisterAccountRequest{Email: "buyer@example.com", Password: "secure-password"})
	if err != nil {
		t.Fatal(err)
	}
	account := registered.GetSession().GetAccount()
	orderResponse, err := commerceService.CreateCheckout(ctx, account.GetAccountId(), account.GetUserId(), &cloudpb.CreateCheckoutRequest{PlanId: "pro", RequestedTransition: cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_UPGRADE, BillingCadence: cloudpb.BillingCadence_BILLING_CADENCE_MONTHLY})
	if err != nil {
		t.Fatal(err)
	}
	api := newFakeAPI(orderResponse.GetOrder())
	provider := newProvider(t, api, commerceService, clock)
	attempt, checkoutURL, err := provider.StartCheckout(ctx, account, orderResponse.GetOrder())
	if err != nil || checkoutURL != "https://checkout.creem.test/session" || attempt.GetProviderReference() != "ch_test" {
		t.Fatalf("start checkout = %v, %q, %v", attempt, checkoutURL, err)
	}
	beforePayment, err := commerceService.AccountCommerce(ctx, account.GetAccountId())
	if err != nil || beforePayment.GetSubscription().GetPlanId() != "free" || beforePayment.GetOrders()[0].GetStatus() != cloudpb.OrderStatus_ORDER_STATUS_PENDING {
		t.Fatalf("checkout changed entitlement = %v, %v", beforePayment, err)
	}

	// 新 Service 实例模拟 Controller 重启；pending attempt 只从 PostgreSQL 恢复。
	now = now.Add(6 * time.Second)
	api.checkout.Status = "completed"
	api.checkout.Order.Status = "paid"
	api.checkout.Order.Transaction = "tran_test"
	api.checkout.Subscription = "sub_test"
	api.transaction = &creem.Transaction{ID: "tran_test", Status: "paid", Amount: 1000, DiscountAmount: 0, Currency: "USD", Order: "ord_creem", Subscription: "sub_test", CreatedAt: now.UnixMilli()}
	api.subscription = &creem.Subscription{ID: "sub_test", Status: "active", Product: "prod_pro_monthly", LastTransactionID: "tran_test", UpdatedAt: now.Format(time.RFC3339)}
	restarted := newProvider(t, api, commerceService, clock)
	processed, err := restarted.ReconcileOnce(ctx, 10)
	if err != nil || processed != 1 {
		t.Fatalf("reconcile after restart = %d, %v", processed, err)
	}
	afterPayment, err := commerceService.AccountCommerce(ctx, account.GetAccountId())
	if err != nil || afterPayment.GetSubscription().GetPlanId() != "pro" || afterPayment.GetSubscription().GetProviderReference() != "sub_test" || len(afterPayment.GetPaymentEvents()) != 1 || afterPayment.GetPaymentEvents()[0].GetState() != cloudpb.PaymentEventState_PAYMENT_EVENT_STATE_APPLIED {
		t.Fatalf("paid commerce = %v, %v", afterPayment, err)
	}
	if processed, err = restarted.ReconcileOnce(ctx, 10); err != nil || processed != 0 {
		t.Fatalf("terminal attempt reconciled twice = %d, %v", processed, err)
	}

	// Webhook 被阻断时，持久 subscription poll 仍从 Creem 服务端状态进入同一个 journal。
	now = now.Add(5*time.Minute + time.Second)
	api.subscription.Status = "scheduled_cancel"
	api.subscription.UpdatedAt = now.Format(time.RFC3339)
	processed, err = restarted.ReconcileOnce(ctx, 10)
	if err != nil || processed != 1 {
		t.Fatalf("subscription reconciliation = %d, %v", processed, err)
	}
	afterCancel, err := commerceService.AccountCommerce(ctx, account.GetAccountId())
	if err != nil || afterCancel.GetSubscription().GetStatus() != cloudpb.SubscriptionStatus_SUBSCRIPTION_STATUS_CANCEL_AT_PERIOD_END || len(afterCancel.GetPaymentEvents()) != 2 {
		t.Fatalf("reconciled subscription state = %v, %v", afterCancel, err)
	}

	// 运营立即对账不等待下一轮 reconcile_after，但仍只消费 provider API 并进入同一 journal。
	api.subscription.Status = "canceled"
	api.subscription.UpdatedAt = now.Add(time.Second).Format(time.RFC3339)
	latest, err := restarted.ReconcilePaymentAttempt(ctx, attempt.GetPaymentAttemptId())
	if err != nil || latest.GetLastProviderStatus() != "canceled" || latest.GetReconcileAfterUnixMillis() != 0 {
		t.Fatalf("operator immediate reconciliation = %v, %v", latest, err)
	}
	afterImmediate, err := commerceService.AccountCommerce(ctx, account.GetAccountId())
	if err != nil || afterImmediate.GetSubscription().GetStatus() != cloudpb.SubscriptionStatus_SUBSCRIPTION_STATUS_CANCELED || len(afterImmediate.GetPaymentEvents()) != 3 {
		t.Fatalf("immediate reconciliation journal = %v, %v", afterImmediate, err)
	}
}

func TestWebhookRequiresRawBodyHMACAndReplaysPayment(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 25, 13, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	store, err := postgrestest.Open(t, filepath.Join(t.TempDir(), "creem-webhook"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	commerceService := commerceService(t, store, clock)
	registered, _ := commerceService.Register(ctx, &cloudpb.RegisterAccountRequest{Email: "webhook@example.com", Password: "secure-password"})
	account := registered.GetSession().GetAccount()
	orderResponse, _ := commerceService.CreateCheckout(ctx, account.GetAccountId(), account.GetUserId(), &cloudpb.CreateCheckoutRequest{PlanId: "pro", RequestedTransition: cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_UPGRADE, BillingCadence: cloudpb.BillingCadence_BILLING_CADENCE_MONTHLY})
	api := newFakeAPI(orderResponse.GetOrder())
	const secret = "webhook-test-secret"
	provider, err := creem.NewService(creem.ServiceConfig{API: api, Commerce: commerceService, SuccessURL: "https://muxvia.com/account?payment=return", WebhookSecret: secret, Now: clock})
	if err != nil {
		t.Fatal(err)
	}
	attempt, _, err := provider.StartCheckout(ctx, account, orderResponse.GetOrder())
	if err != nil {
		t.Fatal(err)
	}
	api.checkout.Status = "completed"
	api.checkout.Order.Status = "paid"
	api.checkout.Order.Transaction = "tran_webhook"
	api.checkout.Subscription = "sub_webhook"
	api.transaction = &creem.Transaction{ID: "tran_webhook", Status: "paid", Amount: 1000, Currency: "USD", Order: "ord_creem", Subscription: "sub_webhook", CreatedAt: now.UnixMilli()}
	api.subscription = &creem.Subscription{ID: "sub_webhook", Status: "active", Product: "prod_pro_monthly", LastTransactionID: "tran_webhook", UpdatedAt: now.Format(time.RFC3339)}
	body := []byte(`{"id":"evt_paid","eventType":"subscription.paid","created_at":1784970000000,"object":{"id":"sub_webhook","status":"active","last_transaction_id":"tran_webhook","product":{"id":"prod_pro_monthly","currency":"USD"},"metadata":{"muxvia_payment_attempt_id":"` + attempt.GetPaymentAttemptId() + `"}}}`)

	invalid := httptest.NewRequest(http.MethodPost, "/pay/creem", strings.NewReader(string(body)))
	invalid.Header.Set("creem-signature", strings.Repeat("0", sha256.Size*2))
	invalidResponse := httptest.NewRecorder()
	provider.WebhookHandler().ServeHTTP(invalidResponse, invalid)
	if invalidResponse.Code != http.StatusUnauthorized {
		t.Fatalf("invalid signature status = %d", invalidResponse.Code)
	}
	before, _ := commerceService.AccountCommerce(ctx, account.GetAccountId())
	if len(before.GetPaymentEvents()) != 0 || before.GetSubscription().GetPlanId() != "free" {
		t.Fatalf("invalid signature changed commerce = %v", before)
	}

	signature := hmac.New(sha256.New, []byte(secret))
	_, _ = signature.Write(body)
	for index := 0; index < 2; index++ {
		request := httptest.NewRequest(http.MethodPost, "/pay/creem", strings.NewReader(string(body)))
		request.Header.Set("creem-signature", hex.EncodeToString(signature.Sum(nil)))
		response := httptest.NewRecorder()
		provider.WebhookHandler().ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("valid webhook %d status = %d", index, response.Code)
		}
	}
	after, _ := commerceService.AccountCommerce(ctx, account.GetAccountId())
	if after.GetSubscription().GetPlanId() != "pro" || after.GetSubscription().GetRevision() != 2 || len(after.GetPaymentEvents()) != 1 {
		t.Fatalf("webhook replay changed commerce = %v", after)
	}
}

func TestPromotionRegistrationReadsCreemDiscountTruth(t *testing.T) {
	now := time.Date(2026, 7, 25, 14, 0, 0, 0, time.UTC)
	api := &fakeAPI{discount: &creem.Discount{ID: "dis_launch20", Status: "active", Code: "LAUNCH20", Type: "percentage", Percentage: 20, MaxRedemptions: 100, ExpiryDate: now.Add(48 * time.Hour).Format(time.RFC3339), AppliesToProducts: []string{"prod_pro_monthly"}}}
	validator, err := creem.NewPromotionValidator(api, catalogSource{catalog: catalogFixture()})
	if err != nil {
		t.Fatal(err)
	}
	value := &cloudpb.PromotionProjection{PromotionId: "promotion_test", Code: "LOCAL20", DiscountKind: cloudpb.PromotionDiscountKind_PROMOTION_DISCOUNT_KIND_PERCENT, PercentBasisPoints: 2000, PlanIds: []string{"pro"}, EffectiveFromUnixMillis: now.UnixMilli(), EffectiveUntilUnixMillis: now.Add(24 * time.Hour).UnixMilli(), MaxRedemptions: 50, MaxRedemptionsPerAccount: 1, CreemDiscountCode: "LAUNCH20", State: cloudpb.PromotionState_PROMOTION_STATE_ACTIVE, Revision: 1, ActorId: "operator", Reason: "launch", CreatedAtUnixMillis: now.UnixMilli(), UpdatedAtUnixMillis: now.UnixMilli()}
	if err := validator.ValidatePromotion(context.Background(), value); err != nil {
		t.Fatalf("valid Creem discount mapping = %v", err)
	}
	api.discount.Percentage = 10
	if err := validator.ValidatePromotion(context.Background(), value); err == nil {
		t.Fatal("mismatched Creem economics were accepted")
	}
}

func newProvider(t *testing.T, api *fakeAPI, commerceService *commerce.Service, clock func() time.Time) *creem.Service {
	t.Helper()
	service, err := creem.NewService(creem.ServiceConfig{API: api, Commerce: commerceService, SuccessURL: "https://muxvia.com/account?payment=return", Now: clock})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func commerceService(t *testing.T, store commerce.Store, clock func() time.Time) *commerce.Service {
	t.Helper()
	service, err := commerce.New(commerce.Config{Store: store, Catalog: catalogSource{catalog: catalogFixture()}, Now: clock})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

type catalogSource struct{ catalog *cloudpb.PlanCatalogContract }

func (source catalogSource) Active(context.Context) (*cloudpb.PlanCatalogContract, error) {
	return proto.Clone(source.catalog).(*cloudpb.PlanCatalogContract), nil
}

func (source catalogSource) Plan(_ context.Context, planID string, version uint64) (*cloudpb.PlanDefinition, error) {
	for _, plan := range source.catalog.GetPlans() {
		if plan.GetPlanId() == planID && plan.GetPlanVersion() == version {
			return proto.Clone(plan).(*cloudpb.PlanDefinition), nil
		}
	}
	return nil, commerce.ErrNotFound
}

func catalogFixture() *cloudpb.PlanCatalogContract {
	capability := func(concurrency uint32) *cloudpb.PlanCapability {
		return &cloudpb.PlanCapability{ManagedP2PEnabled: true, ManagedP2PMaxConcurrency: concurrency, CloudDeviceLimit: 5}
	}
	return &cloudpb.PlanCatalogContract{CatalogVersion: 1, Plans: []*cloudpb.PlanDefinition{
		{PlanId: "free", PlanVersion: 1, BillingPeriodDays: 30, Included: true, Capability: capability(1), Price: &cloudpb.PlanPriceDefinition{Mode: cloudpb.CatalogPriceMode_CATALOG_PRICE_MODE_INCLUDED, Label: "Free"}, Presentation: &cloudpb.PlanPresentation{Name: "Free"}},
		{PlanId: "pro", PlanVersion: 1, BillingPeriodDays: 30, Capability: capability(4), Price: &cloudpb.PlanPriceDefinition{Mode: cloudpb.CatalogPriceMode_CATALOG_PRICE_MODE_CONFIGURED, Currency: "USD", MonthlyMinor: 1000, Label: "$10"}, Presentation: &cloudpb.PlanPresentation{Name: "Pro"}, Creem: &cloudpb.CreemProductMapping{MonthlyProductId: "prod_pro_monthly"}},
	}}
}

type fakeAPI struct {
	product      *creem.Product
	checkout     *creem.Checkout
	transaction  *creem.Transaction
	subscription *creem.Subscription
	createErr    error
	discount     *creem.Discount
}

func newFakeAPI(order *cloudpb.OrderProjection) *fakeAPI {
	return &fakeAPI{
		product:  &creem.Product{ID: order.GetProviderProductId(), Status: "active", Price: order.GetSubtotalMinor(), Currency: order.GetPrice().GetCurrency(), BillingType: "recurring", BillingPeriod: "every-month"},
		checkout: &creem.Checkout{ID: "ch_test", Status: "pending", Product: creem.Reference(order.GetProviderProductId()), RequestID: order.GetOrderId(), CheckoutURL: "https://checkout.creem.test/session", Metadata: map[string]string{}, Order: &creem.Order{ID: "ord_creem", Product: creem.Reference(order.GetProviderProductId()), Amount: order.GetSubtotalMinor(), Subtotal: order.GetSubtotalMinor(), Currency: order.GetPrice().GetCurrency(), Status: "pending"}},
	}
}

func (api *fakeAPI) CreateCheckout(_ context.Context, _, _, _, _, _ string, metadata map[string]string) (*creem.Checkout, error) {
	if api.createErr != nil {
		return nil, api.createErr
	}
	result := *api.checkout
	result.Metadata = metadata
	api.checkout.Metadata = metadata
	return &result, nil
}
func (api *fakeAPI) Checkout(context.Context, string) (*creem.Checkout, error) {
	return api.checkout, nil
}
func (api *fakeAPI) Transaction(context.Context, string) (*creem.Transaction, error) {
	if api.transaction == nil {
		return nil, errors.New("transaction unavailable")
	}
	return api.transaction, nil
}
func (api *fakeAPI) Subscription(context.Context, string) (*creem.Subscription, error) {
	if api.subscription == nil {
		return nil, errors.New("subscription unavailable")
	}
	return api.subscription, nil
}
func (api *fakeAPI) Product(context.Context, string) (*creem.Product, error) { return api.product, nil }
func (api *fakeAPI) Discount(context.Context, string) (*creem.Discount, error) {
	if api.discount == nil {
		return nil, errors.New("discount unavailable")
	}
	return api.discount, nil
}
