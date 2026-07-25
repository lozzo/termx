package commerce_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/muxvia/muxvia/private/cloud/control-plane/commerce"
	postgrestest "github.com/muxvia/muxvia/private/cloud/control-plane/postgrestest"
	"github.com/muxvia/muxvia/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

func TestCommercePersistsSessionPaymentReplayAndSubscriptionTransitions(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	path := filepath.Join(t.TempDir(), "controller-postgres")
	store, err := postgrestest.Open(t, path)
	if err != nil {
		t.Fatal(err)
	}
	service := newService(t, store, clock)
	if _, err := service.Register(ctx, &cloudpb.RegisterAccountRequest{Email: "not-an-email", Password: "correct-horse"}); !errors.Is(err, commerce.ErrConflict) {
		t.Fatalf("invalid email registration = %v", err)
	}

	registered, err := service.Register(ctx, &cloudpb.RegisterAccountRequest{Email: " User@Example.com ", Password: "correct-horse"})
	if err != nil {
		t.Fatal(err)
	}
	account := registered.GetSession().GetAccount()
	if account.GetEmail() != "user@example.com" {
		t.Fatalf("normalized email = %q", account.GetEmail())
	}
	if _, err := service.Register(ctx, &cloudpb.RegisterAccountRequest{Email: "user@example.com", Password: "another-password"}); !errors.Is(err, commerce.ErrConflict) {
		t.Fatalf("duplicate registration = %v", err)
	}
	if _, _, err := service.AuthenticateAccess(ctx, registered.GetSession().GetAccessToken()); err != nil {
		t.Fatalf("authenticate registered access token: %v", err)
	}
	refreshed, err := service.Refresh(ctx, &cloudpb.RefreshAccountSessionRequest{RefreshToken: registered.GetSession().GetRefreshToken()})
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.GetSession().GetSessionRevision() != 2 {
		t.Fatalf("refresh revision = %d", refreshed.GetSession().GetSessionRevision())
	}
	if _, err := service.Refresh(ctx, &cloudpb.RefreshAccountSessionRequest{RefreshToken: registered.GetSession().GetRefreshToken()}); !errors.Is(err, commerce.ErrUnauthorized) {
		t.Fatalf("reused refresh token = %v", err)
	}
	if _, err := service.Login(ctx, &cloudpb.PasswordLoginRequest{Email: "user@example.com", Password: "wrong-password"}); !errors.Is(err, commerce.ErrUnauthorized) {
		t.Fatalf("wrong password login = %v", err)
	}
	if _, err := service.Login(ctx, &cloudpb.PasswordLoginRequest{Email: "user@example.com", Password: "correct-horse"}); err != nil {
		t.Fatalf("password login: %v", err)
	}
	changed, err := service.ChangePassword(ctx, account.GetAccountId(), &cloudpb.ChangeAccountPasswordRequest{CurrentPassword: "correct-horse", NewPassword: "new-correct-horse"})
	if err != nil {
		t.Fatal(err)
	}
	if changed.GetSession().GetAccount().GetAuthRevision() != 2 {
		t.Fatalf("changed auth revision = %d", changed.GetSession().GetAccount().GetAuthRevision())
	}
	if _, _, err := service.AuthenticateAccess(ctx, refreshed.GetSession().GetAccessToken()); !errors.Is(err, commerce.ErrUnauthorized) {
		t.Fatalf("old session after password change = %v", err)
	}
	if _, err := service.Login(ctx, &cloudpb.PasswordLoginRequest{Email: "user@example.com", Password: "correct-horse"}); !errors.Is(err, commerce.ErrUnauthorized) {
		t.Fatalf("old password after change = %v", err)
	}
	if _, err := service.Login(ctx, &cloudpb.PasswordLoginRequest{Email: "user@example.com", Password: "new-correct-horse"}); err != nil {
		t.Fatalf("new password login: %v", err)
	}

	upgrade, err := service.CreateCheckout(ctx, account.GetAccountId(), account.GetUserId(), &cloudpb.CreateCheckoutRequest{PlanId: "pro", RequestedTransition: cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_UPGRADE})
	if err != nil {
		t.Fatal(err)
	}
	if upgrade.GetOrder().GetPrice().GetMonthlyMinor() != 1000 || upgrade.GetOrder().GetPrice().GetCurrency() != "USD" {
		t.Fatalf("order price snapshot = %v", upgrade.GetOrder().GetPrice())
	}
	upgradeAttempt := createAttempt(t, service, account, upgrade.GetOrder())
	event := paymentEvent("event-upgrade", upgrade.GetOrder(), upgradeAttempt, cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_SUCCEEDED, now)
	applied, err := service.ApplyPaymentEvent(ctx, &cloudpb.ApplyPaymentEventRequest{Event: event})
	if err != nil {
		t.Fatal(err)
	}
	if applied.GetSubscription().GetPlanId() != "pro" || applied.GetSubscription().GetRevision() != 2 {
		t.Fatalf("upgraded subscription = %v", applied.GetSubscription())
	}

	now = now.Add(time.Hour)
	canceled := transition(t, service, account, cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_CANCEL_AT_PERIOD_END, "", now)
	if !canceled.GetSubscription().GetCancelAtPeriodEnd() || canceled.GetSubscription().GetRevision() != 3 {
		t.Fatalf("cancel-at-period-end = %v", canceled.GetSubscription())
	}
	resumed := transition(t, service, account, cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_RESUME, "", now)
	if resumed.GetSubscription().GetCancelAtPeriodEnd() || resumed.GetSubscription().GetRevision() != 4 {
		t.Fatalf("resumed subscription = %v", resumed.GetSubscription())
	}
	replayed, err := service.ApplyPaymentEvent(ctx, &cloudpb.ApplyPaymentEventRequest{Event: event})
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(replayed, applied) {
		t.Fatalf("payment replay changed after later transition\nfirst=%v\nreplay=%v", applied, replayed)
	}

	renewal, err := service.CreateCheckout(ctx, account.GetAccountId(), account.GetUserId(), &cloudpb.CreateCheckoutRequest{PlanId: "pro", RequestedTransition: cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_RENEW})
	if err != nil {
		t.Fatal(err)
	}
	renewalAttempt := createAttempt(t, service, account, renewal.GetOrder())
	now = now.Add(time.Hour)
	renewed, err := service.ApplyPaymentEvent(ctx, &cloudpb.ApplyPaymentEventRequest{Event: paymentEvent("event-renew", renewal.GetOrder(), renewalAttempt, cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_SUCCEEDED, now)})
	if err != nil {
		t.Fatal(err)
	}
	if renewed.GetSubscription().GetRevision() != 5 || renewed.GetSubscription().GetCurrentPeriodStartUnixMillis() != resumed.GetSubscription().GetCurrentPeriodEndUnixMillis() {
		t.Fatalf("renewed subscription = %v", renewed.GetSubscription())
	}

	failedOrder, err := service.CreateCheckout(ctx, account.GetAccountId(), account.GetUserId(), &cloudpb.CreateCheckoutRequest{PlanId: "pro", RequestedTransition: cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_RENEW})
	if err != nil {
		t.Fatal(err)
	}
	failedAttempt := createAttempt(t, service, account, failedOrder.GetOrder())
	failed, err := service.ApplyPaymentEvent(ctx, &cloudpb.ApplyPaymentEventRequest{Event: paymentEvent("event-failed", failedOrder.GetOrder(), failedAttempt, cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_FAILED, now)})
	if err != nil {
		t.Fatal(err)
	}
	if failed.GetSubscription().GetStatus() != cloudpb.SubscriptionStatus_SUBSCRIPTION_STATUS_PAST_DUE || failed.GetSubscription().GetRevision() != 6 {
		t.Fatalf("failed payment subscription = %v", failed.GetSubscription())
	}
	restored := transition(t, service, account, cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_RESTORE, "", now)
	if restored.GetSubscription().GetStatus() != cloudpb.SubscriptionStatus_SUBSCRIPTION_STATUS_ACTIVE || restored.GetSubscription().GetRevision() != 7 {
		t.Fatalf("restored subscription = %v", restored.GetSubscription())
	}

	downgrade, err := service.CreateCheckout(ctx, account.GetAccountId(), account.GetUserId(), &cloudpb.CreateCheckoutRequest{PlanId: "basic", RequestedTransition: cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_DOWNGRADE})
	if err != nil {
		t.Fatal(err)
	}
	downgradeAttempt := createAttempt(t, service, account, downgrade.GetOrder())
	downgraded, err := service.ApplyPaymentEvent(ctx, &cloudpb.ApplyPaymentEventRequest{Event: paymentEvent("event-downgrade", downgrade.GetOrder(), downgradeAttempt, cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_SUCCEEDED, now)})
	if err != nil {
		t.Fatal(err)
	}
	if downgraded.GetSubscription().GetPlanId() != "basic" || downgraded.GetSubscription().GetRevision() != 8 {
		t.Fatalf("downgraded subscription = %v", downgraded.GetSubscription())
	}
	suspended := transition(t, service, account, cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_SUSPEND, "", now)
	if suspended.GetEntitlement().GetStatus() != cloudpb.EntitlementStatus_ENTITLEMENT_STATUS_SUSPENDED {
		t.Fatalf("suspended entitlement = %v", suspended.GetEntitlement())
	}
	transition(t, service, account, cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_RESTORE, "", now)
	expired := transition(t, service, account, cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_EXPIRE, "", now)
	if expired.GetSubscription().GetStatus() != cloudpb.SubscriptionStatus_SUBSCRIPTION_STATUS_EXPIRED || expired.GetEntitlement().GetStatus() != cloudpb.EntitlementStatus_ENTITLEMENT_STATUS_EXPIRED {
		t.Fatalf("expired state = %v / %v", expired.GetSubscription(), expired.GetEntitlement())
	}
	deviceCredential, err := service.IssueDeviceSession(ctx, account.GetAccountId(), "android-client-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Refresh(ctx, &cloudpb.RefreshAccountSessionRequest{RefreshToken: deviceCredential.GetRefreshToken()}); !errors.Is(err, commerce.ErrUnauthorized) {
		t.Fatalf("device refresh token crossed into browser session refresh: %v", err)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := postgrestest.Open(t, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restarted := newService(t, reopened, clock)
	view, err := restarted.AccountCommerce(ctx, account.GetAccountId())
	if err != nil {
		t.Fatal(err)
	}
	if view.GetSubscription().GetRevision() != 11 || len(view.GetOrders()) != 4 || len(view.GetPaymentAttempts()) != 4 || len(view.GetAudit()) < 12 {
		t.Fatalf("restarted commerce view = subscription %v, orders=%d attempts=%d audit=%d", view.GetSubscription(), len(view.GetOrders()), len(view.GetPaymentAttempts()), len(view.GetAudit()))
	}
	if next, err := restarted.Refresh(ctx, &cloudpb.RefreshAccountSessionRequest{RefreshToken: changed.GetSession().GetRefreshToken()}); err != nil || next.GetSession().GetSessionRevision() != 2 {
		t.Fatalf("password-change session was not restored: response=%v error=%v", next, err)
	}
	deviceNext, err := restarted.RefreshDeviceSession(ctx, deviceCredential.GetRefreshToken())
	if err != nil || deviceNext.ClientDeviceID != "android-client-1" || deviceNext.Credential.GetSessionRevision() != 2 {
		t.Fatalf("device session was not restored: response=%v error=%v", deviceNext, err)
	}
	if _, err := restarted.RefreshDeviceSession(ctx, deviceCredential.GetRefreshToken()); !errors.Is(err, commerce.ErrUnauthorized) {
		t.Fatalf("device refresh token replay = %v", err)
	}
}

func TestPaymentEventRejectsOrderCreatedFromStaleSubscriptionRevision(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 20, 15, 0, 0, 0, time.UTC)
	store, err := postgrestest.Open(t, filepath.Join(t.TempDir(), "controller-postgres"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := newService(t, store, func() time.Time { return now })
	registered, err := service.Register(ctx, &cloudpb.RegisterAccountRequest{Email: "stale@example.com", Password: "secure-password"})
	if err != nil {
		t.Fatal(err)
	}
	account := registered.GetSession().GetAccount()
	checkout, err := service.CreateCheckout(ctx, account.GetAccountId(), account.GetUserId(), &cloudpb.CreateCheckoutRequest{PlanId: "pro", RequestedTransition: cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_UPGRADE})
	if err != nil {
		t.Fatal(err)
	}
	attempt := createAttempt(t, service, account, checkout.GetOrder())
	transition(t, service, account, cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_SUSPEND, "", now)
	event := paymentEvent("event-stale", checkout.GetOrder(), attempt, cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_SUCCEEDED, now)
	if _, err := service.ApplyPaymentEvent(ctx, &cloudpb.ApplyPaymentEventRequest{Event: event}); !errors.Is(err, commerce.ErrConflict) {
		t.Fatalf("stale payment event = %v", err)
	}
	view, err := service.AccountCommerce(ctx, account.GetAccountId())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, audit := range view.GetAudit() {
		if audit.GetAction() == "payment.rejected_stale_order" && audit.GetResourceId() == event.GetProviderEventId() {
			found = true
		}
	}
	if !found {
		t.Fatalf("stale payment rejection audit missing: %v", view.GetAudit())
	}
}

func TestPaidOrderSupportsRefundRevokeAndChargebackTransitions(t *testing.T) {
	for _, test := range []struct {
		name        string
		eventType   cloudpb.PaymentEventType
		orderStatus cloudpb.OrderStatus
		subStatus   cloudpb.SubscriptionStatus
	}{
		{name: "refund", eventType: cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_REFUNDED, orderStatus: cloudpb.OrderStatus_ORDER_STATUS_REFUNDED, subStatus: cloudpb.SubscriptionStatus_SUBSCRIPTION_STATUS_CANCELED},
		{name: "revoke", eventType: cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_REVOKED, orderStatus: cloudpb.OrderStatus_ORDER_STATUS_REVOKED, subStatus: cloudpb.SubscriptionStatus_SUBSCRIPTION_STATUS_CANCELED},
		{name: "chargeback", eventType: cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_CHARGEBACK, orderStatus: cloudpb.OrderStatus_ORDER_STATUS_REVOKED, subStatus: cloudpb.SubscriptionStatus_SUBSCRIPTION_STATUS_SUSPENDED},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			now := time.Date(2026, 7, 20, 16, 0, 0, 0, time.UTC)
			store, err := postgrestest.Open(t, filepath.Join(t.TempDir(), "controller-postgres"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			service := newService(t, store, func() time.Time { return now })
			registered, _ := service.Register(ctx, &cloudpb.RegisterAccountRequest{Email: test.name + "@example.com", Password: "secure-password"})
			account := registered.GetSession().GetAccount()
			checkout, _ := service.CreateCheckout(ctx, account.GetAccountId(), account.GetUserId(), &cloudpb.CreateCheckoutRequest{PlanId: "pro", RequestedTransition: cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_UPGRADE})
			attempt := createAttempt(t, service, account, checkout.GetOrder())
			if _, err := service.ApplyPaymentEvent(ctx, &cloudpb.ApplyPaymentEventRequest{Event: paymentEvent("event-paid", checkout.GetOrder(), attempt, cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_SUCCEEDED, now)}); err != nil {
				t.Fatal(err)
			}
			now = now.Add(time.Hour)
			result, err := service.ApplyPaymentEvent(ctx, &cloudpb.ApplyPaymentEventRequest{Event: paymentEvent("event-"+test.name, checkout.GetOrder(), attempt, test.eventType, now)})
			if err != nil {
				t.Fatal(err)
			}
			if result.GetOrder().GetStatus() != test.orderStatus || result.GetSubscription().GetStatus() != test.subStatus {
				t.Fatalf("result = %v", result)
			}
		})
	}
}

func newService(t *testing.T, store commerce.Store, now func() time.Time) *commerce.Service {
	t.Helper()
	service, err := commerce.New(commerce.Config{Store: store, Catalog: testCatalogSource{catalog: catalogFixture()}, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

type testCatalogSource struct{ catalog *cloudpb.PlanCatalogContract }

func (source testCatalogSource) Active(context.Context) (*cloudpb.PlanCatalogContract, error) {
	return proto.Clone(source.catalog).(*cloudpb.PlanCatalogContract), nil
}

func (source testCatalogSource) Plan(_ context.Context, planID string, planVersion uint64) (*cloudpb.PlanDefinition, error) {
	for _, plan := range source.catalog.GetPlans() {
		if plan.GetPlanId() == planID && plan.GetPlanVersion() == planVersion {
			return proto.Clone(plan).(*cloudpb.PlanDefinition), nil
		}
	}
	return nil, commerce.ErrNotFound
}

func transition(t *testing.T, service *commerce.Service, account *cloudpb.AccountProjection, kind cloudpb.SubscriptionTransitionKind, targetPlan string, now time.Time) *cloudpb.TransitionSubscriptionResponse {
	t.Helper()
	response, err := service.Transition(context.Background(), &cloudpb.TransitionSubscriptionRequest{AccountId: account.GetAccountId(), Transition: kind, TargetPlanId: targetPlan, ActorId: account.GetUserId(), EffectiveAtUnixMillis: now.UnixMilli()})
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func createAttempt(t *testing.T, service *commerce.Service, account *cloudpb.AccountProjection, order *cloudpb.OrderProjection) *cloudpb.PaymentAttemptProjection {
	t.Helper()
	response, err := service.CreatePaymentAttempt(context.Background(), account.GetAccountId(), account.GetUserId(), &cloudpb.CreatePaymentAttemptRequest{OrderId: order.GetOrderId(), Provider: "test-provider"})
	if err != nil {
		t.Fatal(err)
	}
	return response.GetPaymentAttempt()
}

func paymentEvent(id string, order *cloudpb.OrderProjection, attempt *cloudpb.PaymentAttemptProjection, eventType cloudpb.PaymentEventType, now time.Time) *cloudpb.NormalizedPaymentEvent {
	return &cloudpb.NormalizedPaymentEvent{ProviderEventId: id, Provider: attempt.GetProvider(), EventType: eventType, OrderId: order.GetOrderId(), AccountId: order.GetAccountId(), PlanId: order.GetPlanId(), PlanVersion: order.GetPlanVersion(), ProviderReference: "provider-" + order.GetOrderId(), OccurredAtUnixMillis: now.UnixMilli(), PaymentAttemptId: attempt.GetPaymentAttemptId()}
}

func catalogFixture() *cloudpb.PlanCatalogContract {
	p2p := func(concurrency uint32) *cloudpb.PlanCapability {
		return &cloudpb.PlanCapability{ManagedP2PEnabled: true, ManagedP2PMaxConcurrency: concurrency, CloudDeviceLimit: 3}
	}
	return &cloudpb.PlanCatalogContract{CatalogVersion: 1, Plans: []*cloudpb.PlanDefinition{
		{PlanId: "included", PlanVersion: 1, BillingPeriodDays: 30, Capability: p2p(1), Included: true, Price: &cloudpb.PlanPriceDefinition{Mode: cloudpb.CatalogPriceMode_CATALOG_PRICE_MODE_INCLUDED, Currency: "USD", Label: "Free"}},
		{PlanId: "basic", PlanVersion: 1, BillingPeriodDays: 30, Capability: p2p(2), Price: &cloudpb.PlanPriceDefinition{Mode: cloudpb.CatalogPriceMode_CATALOG_PRICE_MODE_CONFIGURED, Currency: "USD", MonthlyMinor: 500, Label: "$5"}},
		{PlanId: "pro", PlanVersion: 1, BillingPeriodDays: 30, Capability: p2p(4), Price: &cloudpb.PlanPriceDefinition{Mode: cloudpb.CatalogPriceMode_CATALOG_PRICE_MODE_CONFIGURED, Currency: "USD", MonthlyMinor: 1000, Label: "$10"}},
	}}
}
