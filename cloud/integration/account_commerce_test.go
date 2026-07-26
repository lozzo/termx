package integration_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/muxvia/muxvia/cloud/controller/account"
	"github.com/muxvia/muxvia/cloud/controller/commerce"
	"github.com/muxvia/muxvia/cloud/controller/postgres"
	cloudv1 "github.com/muxvia/muxvia/proto/cloud/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestR7AccountPaymentSubscriptionAndEntitlementInPostgreSQL(t *testing.T) {
	databaseURL := os.Getenv("MUXVIA_CLOUD_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("MUXVIA_CLOUD_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	database, err := postgres.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	clock := time.Now().UTC()
	accounts, err := account.New(account.Config{Store: database, AccessTTL: 15 * time.Minute, RefreshTTL: time.Hour, RecentAuthenticationTTL: 10 * time.Minute, BcryptCost: 4, Now: func() time.Time { return clock }})
	if err != nil {
		t.Fatal(err)
	}
	commercial, err := commerce.New(commerce.Config{Store: database})
	if err != nil {
		t.Fatal(err)
	}
	adminLogin := "r7-admin-" + uuid.NewString()
	if _, err := accounts.EnsureBootstrapOperator(ctx, adminLogin, "r7-test-password"); err != nil {
		t.Fatal(err)
	}
	adminLoginResponse, err := accounts.Login(ctx, &cloudv1.LoginAccountRequest{Login: adminLogin, Password: "r7-test-password"})
	if err != nil {
		t.Fatal(err)
	}
	adminIdentity, err := accounts.AuthenticateAccess(ctx, adminLoginResponse.GetSession().GetAccessToken())
	if err != nil {
		t.Fatal(err)
	}
	adminContext := account.ContextWithIdentity(ctx, adminIdentity)
	email := "r7-" + uuid.NewString() + "@example.com"
	registered, err := accounts.Register(ctx, &cloudv1.RegisterAccountRequest{Email: email, Password: "r7-user-password", DisplayName: "R7 交易测试"})
	if err != nil {
		t.Fatal(err)
	}
	userIdentity, err := accounts.AuthenticateAccess(ctx, registered.GetSession().GetAccessToken())
	if err != nil {
		t.Fatal(err)
	}
	userContext := account.ContextWithIdentity(ctx, userIdentity)
	plans, err := commercial.ListPlans(userContext, &cloudv1.ListPlansRequest{})
	if err != nil {
		t.Fatal(err)
	}
	var professional *cloudv1.PlanDefinition
	for _, plan := range plans.GetPlans() {
		if plan.GetPlanId() == "professional" {
			professional = plan
		}
	}
	if professional == nil || professional.GetState() != cloudv1.PlanState_PLAN_STATE_PUBLISHED {
		t.Fatal("published professional plan is missing")
	}
	created, err := commercial.CreateOrder(userContext, &cloudv1.CreateOrderRequest{AccountId: registered.GetAccount().GetAccountId(), PlanId: professional.GetPlanId(), PlanVersion: professional.GetVersion(), Provider: "development", IdempotencyKey: uuid.NewString(), RequestedTransition: cloudv1.SubscriptionTransition_SUBSCRIPTION_TRANSITION_UPGRADE})
	if err != nil {
		t.Fatal(err)
	}
	if created.GetOrder().GetStatus() != cloudv1.OrderStatus_ORDER_STATUS_PENDING || created.GetPaymentAttempt().GetStatus() != cloudv1.PaymentAttemptStatus_PAYMENT_ATTEMPT_STATUS_PENDING {
		t.Fatalf("new checkout = %+v", created)
	}
	event := &cloudv1.ApplyPaymentEventRequest{Provider: "development", ProviderEventId: uuid.NewString(), PaymentAttemptId: created.GetPaymentAttempt().GetPaymentAttemptId(), OrderId: created.GetOrder().GetOrderId(), EventType: cloudv1.PaymentEventType_PAYMENT_EVENT_TYPE_SUCCEEDED, ProviderReference: "r7-test", OccurredAt: timestamppb.Now()}
	applied, err := commercial.ApplyPaymentEvent(adminContext, event)
	if err != nil {
		t.Fatal(err)
	}
	if applied.GetDuplicate() || applied.GetOrder().GetStatus() != cloudv1.OrderStatus_ORDER_STATUS_PAID || applied.GetSubscription().GetPlanId() != "professional" || applied.GetEntitlement().GetState() != cloudv1.EntitlementState_ENTITLEMENT_STATE_ACTIVE {
		t.Fatalf("applied payment = %+v", applied)
	}
	duplicate, err := commercial.ApplyPaymentEvent(adminContext, event)
	if err != nil {
		t.Fatal(err)
	}
	if !duplicate.GetDuplicate() || duplicate.GetSubscription().GetRevision() != applied.GetSubscription().GetRevision() {
		t.Fatalf("duplicate payment changed subscription: %+v", duplicate)
	}
	suspended, err := commercial.TransitionSubscription(adminContext, &cloudv1.TransitionSubscriptionRequest{AccountId: registered.GetAccount().GetAccountId(), Transition: cloudv1.SubscriptionTransition_SUBSCRIPTION_TRANSITION_SUSPEND, ExpectedRevision: applied.GetSubscription().GetRevision(), Reason: "R7 test suspend"})
	if err != nil {
		t.Fatal(err)
	}
	if suspended.GetEntitlement().GetState() != cloudv1.EntitlementState_ENTITLEMENT_STATE_SUSPENDED {
		t.Fatalf("suspended entitlement = %+v", suspended.GetEntitlement())
	}
	policy := commerce.EntitlementRelayPolicy{Service: commercial}
	if _, err := policy.Limits(ctx, &cloudv1.ClientSessionSummary{AccountId: registered.GetAccount().GetAccountId()}); err == nil {
		t.Fatal("suspended account received Relay limits")
	}
	refund := &cloudv1.ApplyPaymentEventRequest{Provider: event.GetProvider(), ProviderEventId: uuid.NewString(), PaymentAttemptId: event.GetPaymentAttemptId(), OrderId: event.GetOrderId(), EventType: cloudv1.PaymentEventType_PAYMENT_EVENT_TYPE_REFUNDED, ProviderReference: "r7-refund", OccurredAt: timestamppb.Now()}
	refunded, err := commercial.ApplyPaymentEvent(adminContext, refund)
	if err != nil {
		t.Fatal(err)
	}
	if refunded.GetOrder().GetStatus() != cloudv1.OrderStatus_ORDER_STATUS_REFUNDED || refunded.GetPaymentAttempt().GetStatus() != cloudv1.PaymentAttemptStatus_PAYMENT_ATTEMPT_STATUS_SUCCEEDED {
		t.Fatalf("refund changed payment truth incorrectly: %+v", refunded)
	}
	lateFailure := &cloudv1.ApplyPaymentEventRequest{Provider: event.GetProvider(), ProviderEventId: uuid.NewString(), PaymentAttemptId: event.GetPaymentAttemptId(), OrderId: event.GetOrderId(), EventType: cloudv1.PaymentEventType_PAYMENT_EVENT_TYPE_FAILED, ProviderReference: "r7-late-failure", OccurredAt: timestamppb.Now()}
	if _, err := commercial.ApplyPaymentEvent(adminContext, lateFailure); !errors.Is(err, commerce.ErrInvalidTransition) {
		t.Fatalf("late payment failure after refund err=%v", err)
	}

	olderAuditAt := clock.Add(-time.Minute)
	newerAuditAt := clock
	if err := database.AuditRuntimeCommand(ctx, adminIdentity.Account.GetAccountId(), "daemon.disconnect", "older", "R7 audit order", cloudv1.RuntimeCommandResult_RUNTIME_COMMAND_RESULT_APPLIED, olderAuditAt); err != nil {
		t.Fatal(err)
	}
	if err := database.AuditRuntimeCommand(ctx, adminIdentity.Account.GetAccountId(), "daemon.disconnect", "newer", "R7 audit order", cloudv1.RuntimeCommandResult_RUNTIME_COMMAND_RESULT_APPLIED, newerAuditAt); err != nil {
		t.Fatal(err)
	}
	firstAuditPage, cursor, err := database.ListOperatorAudit(ctx, &cloudv1.PageRequest{Query: "daemon.disconnect", PageSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(firstAuditPage) != 1 || firstAuditPage[0].GetResourceId() != "newer" || cursor == "" {
		t.Fatalf("first audit page = %+v cursor=%q", firstAuditPage, cursor)
	}
	secondAuditPage, _, err := database.ListOperatorAudit(ctx, &cloudv1.PageRequest{Query: "daemon.disconnect", PageSize: 1, Cursor: cursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(secondAuditPage) != 1 || secondAuditPage[0].GetResourceId() != "older" {
		t.Fatalf("second audit page = %+v", secondAuditPage)
	}

	clock = clock.Add(11 * time.Minute)
	refreshed, err := accounts.Refresh(ctx, &cloudv1.RefreshAccountSessionRequest{RefreshToken: registered.GetSession().GetRefreshToken()})
	if err != nil {
		t.Fatal(err)
	}
	refreshedIdentity, err := accounts.AuthenticateAccess(ctx, refreshed.GetSession().GetAccessToken())
	if err != nil {
		t.Fatal(err)
	}
	if clock.Before(refreshedIdentity.RecentAuthExpiresAt) || !refreshedIdentity.RecentAuthExpiresAt.Equal(userIdentity.RecentAuthExpiresAt) {
		t.Fatalf("refresh extended recent-auth: before=%s after=%s now=%s", userIdentity.RecentAuthExpiresAt, refreshedIdentity.RecentAuthExpiresAt, clock)
	}
}
