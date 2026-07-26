package commerce_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/muxvia/muxvia/cloud/controller/account"
	"github.com/muxvia/muxvia/cloud/controller/commerce"
	cloudv1 "github.com/muxvia/muxvia/proto/cloud/v1"
)

func TestCommerceSelfServiceDerivesAccountAndDevelopmentProvider(t *testing.T) {
	now := time.Unix(2_000, 0).UTC()
	store := &commerceStoreFake{aggregate: &cloudv1.GetAccountCommerceResponse{
		Orders:          []*cloudv1.OrderProjection{{OrderId: "order-a", AccountId: "account-a", Provider: "development"}},
		PaymentAttempts: []*cloudv1.PaymentAttemptProjection{{PaymentAttemptId: "attempt-a", OrderId: "order-a", Provider: "development"}},
	}}
	service, err := commerce.New(commerce.Config{Store: store, DevelopmentPayments: true, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	ctx := account.ContextWithIdentity(context.Background(), account.Identity{Account: &cloudv1.AccountProfile{AccountId: "account-a"}, Roles: []cloudv1.AccountRole{cloudv1.AccountRole_ACCOUNT_ROLE_USER}, SessionID: "session-a"})

	_, err = service.CreateMyOrder(ctx, &cloudv1.CreateMyOrderRequest{PlanId: "pro", PlanVersion: 2, IdempotencyKey: "idem-a", RequestedTransition: cloudv1.SubscriptionTransition_SUBSCRIPTION_TRANSITION_ACTIVATE})
	if err != nil {
		t.Fatal(err)
	}
	if store.created.GetAccountId() != "account-a" || store.created.GetProvider() != "development" || store.createActor != "account-a" {
		t.Fatalf("created request=%+v actor=%q", store.created, store.createActor)
	}

	if _, err := service.GetMyCommerce(ctx, &cloudv1.GetMyCommerceRequest{}); err != nil {
		t.Fatal(err)
	}
	if store.readAccount != "account-a" {
		t.Fatalf("commerce read account=%q", store.readAccount)
	}

	if _, err := service.CompleteDevelopmentPayment(ctx, &cloudv1.CompleteDevelopmentPaymentRequest{OrderId: "order-a", PaymentAttemptId: "attempt-a"}); err != nil {
		t.Fatal(err)
	}
	if store.applied.GetProvider() != "development" || store.applied.GetProviderEventId() != "development:attempt-a:succeeded" || store.applyActor != "account-a" {
		t.Fatalf("payment event=%+v actor=%q", store.applied, store.applyActor)
	}
	store.aggregate = &cloudv1.GetAccountCommerceResponse{
		Subscription: &cloudv1.SubscriptionProjection{SubscriptionId: "subscription-a", AccountId: "account-a", Revision: 4},
		Entitlement:  &cloudv1.EffectiveEntitlement{AccountId: "account-a", RelayUsedBytes: 360, RelayRemainingBytes: 640},
		Orders: []*cloudv1.OrderProjection{{
			OrderId: "order-a", AccountId: "account-a", Provider: "development", Status: cloudv1.OrderStatus_ORDER_STATUS_PAID,
		}},
		PaymentAttempts: []*cloudv1.PaymentAttemptProjection{{
			PaymentAttemptId: "attempt-a", OrderId: "order-a", Provider: "development", Status: cloudv1.PaymentAttemptStatus_PAYMENT_ATTEMPT_STATUS_SUCCEEDED,
		}},
	}
	now = now.Add(time.Hour)
	duplicate, err := service.CompleteDevelopmentPayment(ctx, &cloudv1.CompleteDevelopmentPaymentRequest{OrderId: "order-a", PaymentAttemptId: "attempt-a"})
	if err != nil {
		t.Fatal(err)
	}
	if !duplicate.GetDuplicate() || duplicate.GetEntitlement().GetRelayUsedBytes() != 360 || store.applyCalls != 1 {
		t.Fatalf("development payment retry=%+v apply calls=%d", duplicate, store.applyCalls)
	}
	if _, err := service.CompleteDevelopmentPayment(ctx, &cloudv1.CompleteDevelopmentPaymentRequest{OrderId: "order-b", PaymentAttemptId: "attempt-b"}); !errors.Is(err, account.ErrUnauthenticated) {
		t.Fatalf("cross-account development payment error=%v", err)
	}

	if _, err := service.ChangeMySubscription(ctx, &cloudv1.ChangeMySubscriptionRequest{ExpectedRevision: 3, Transition: cloudv1.SubscriptionTransition_SUBSCRIPTION_TRANSITION_CANCEL_AT_PERIOD_END}); err != nil {
		t.Fatal(err)
	}
	if store.transition.GetAccountId() != "account-a" || store.transitionActor != "account-a" || store.transition.GetReason() == "" {
		t.Fatalf("subscription transition=%+v actor=%q", store.transition, store.transitionActor)
	}
}

func TestDevelopmentPaymentIsExplicitlyDisabledByDefault(t *testing.T) {
	service, err := commerce.New(commerce.Config{Store: &commerceStoreFake{}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := account.ContextWithIdentity(context.Background(), account.Identity{Account: &cloudv1.AccountProfile{AccountId: "account-a"}, SessionID: "session-a"})
	if _, err := service.CreateMyOrder(ctx, &cloudv1.CreateMyOrderRequest{}); !errors.Is(err, commerce.ErrInvalidTransition) {
		t.Fatalf("disabled development order error=%v", err)
	}
}

type commerceStoreFake struct {
	aggregate                *cloudv1.GetAccountCommerceResponse
	created                  *cloudv1.CreateOrderRequest
	createActor, readAccount string
	applied                  *cloudv1.ApplyPaymentEventRequest
	applyActor               string
	applyCalls               int
	transition               *cloudv1.TransitionSubscriptionRequest
	transitionActor          string
}

func (store *commerceStoreFake) ListPlans(context.Context, bool) ([]*cloudv1.PlanDefinition, error) {
	return nil, nil
}
func (store *commerceStoreFake) CreatePlanVersion(context.Context, *cloudv1.CreatePlanVersionRequest, string, time.Time) (*cloudv1.PlanDefinition, error) {
	return nil, nil
}
func (store *commerceStoreFake) PublishPlanVersion(context.Context, *cloudv1.PublishPlanVersionRequest, string, time.Time) (*cloudv1.PlanDefinition, error) {
	return nil, nil
}
func (store *commerceStoreFake) CreateOrder(_ context.Context, request *cloudv1.CreateOrderRequest, actor string, _ time.Time) (*cloudv1.CreateOrderResponse, error) {
	store.created, store.createActor = request, actor
	return &cloudv1.CreateOrderResponse{}, nil
}
func (store *commerceStoreFake) ApplyPaymentEvent(_ context.Context, request *cloudv1.ApplyPaymentEventRequest, actor string, _ time.Time) (*cloudv1.ApplyPaymentEventResponse, error) {
	store.applied, store.applyActor = request, actor
	store.applyCalls++
	return &cloudv1.ApplyPaymentEventResponse{}, nil
}
func (store *commerceStoreFake) TransitionSubscription(_ context.Context, request *cloudv1.TransitionSubscriptionRequest, actor string, _ time.Time) (*cloudv1.TransitionSubscriptionResponse, error) {
	store.transition, store.transitionActor = request, actor
	return &cloudv1.TransitionSubscriptionResponse{}, nil
}
func (store *commerceStoreFake) GetAccountCommerce(_ context.Context, accountID string, _ time.Time) (*cloudv1.GetAccountCommerceResponse, error) {
	store.readAccount = accountID
	if store.aggregate == nil {
		return &cloudv1.GetAccountCommerceResponse{}, nil
	}
	return store.aggregate, nil
}
func (store *commerceStoreFake) EffectiveEntitlement(context.Context, string, time.Time) (*cloudv1.EffectiveEntitlement, error) {
	return nil, commerce.ErrEntitlementUnavailable
}
