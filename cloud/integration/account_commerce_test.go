package integration_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/anytty/anytty/cloud/controller/account"
	"github.com/anytty/anytty/cloud/controller/commerce"
	"github.com/anytty/anytty/cloud/controller/edgeconfig"
	"github.com/anytty/anytty/cloud/controller/postgres"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestR7AccountPaymentSubscriptionAndEntitlementInPostgreSQL(t *testing.T) {
	databaseURL := os.Getenv("ANYTTY_CLOUD_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("ANYTTY_CLOUD_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
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
	commercial, err := commerce.New(commerce.Config{Store: database, DevelopmentPayments: true})
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
	created, err := commercial.CreateMyOrder(userContext, &cloudv1.CreateMyOrderRequest{PlanId: professional.GetPlanId(), PlanVersion: professional.GetVersion(), IdempotencyKey: uuid.NewString(), RequestedTransition: cloudv1.SubscriptionTransition_SUBSCRIPTION_TRANSITION_UPGRADE})
	if err != nil {
		t.Fatal(err)
	}
	if created.GetOrder().GetStatus() != cloudv1.OrderStatus_ORDER_STATUS_PENDING || created.GetPaymentAttempt().GetStatus() != cloudv1.PaymentAttemptStatus_PAYMENT_ATTEMPT_STATUS_PENDING {
		t.Fatalf("new checkout = %+v", created)
	}
	applied, err := commercial.CompleteDevelopmentPayment(userContext, &cloudv1.CompleteDevelopmentPaymentRequest{PaymentAttemptId: created.GetPaymentAttempt().GetPaymentAttemptId(), OrderId: created.GetOrder().GetOrderId()})
	if err != nil {
		t.Fatal(err)
	}
	if applied.GetDuplicate() || applied.GetOrder().GetStatus() != cloudv1.OrderStatus_ORDER_STATUS_PAID || applied.GetSubscription().GetPlanId() != "professional" || applied.GetEntitlement().GetState() != cloudv1.EntitlementState_ENTITLEMENT_STATE_ACTIVE {
		t.Fatalf("applied payment = %+v", applied)
	}
	capability := applied.GetEntitlement().GetCapability()
	if !capability.GetManagedP2PEnabled() || !capability.GetRelayEnabled() || capability.GetRelayMaxBytesPerPeriod() == 0 || applied.GetEntitlement().GetRelayRemainingBytes() != capability.GetRelayMaxBytesPerPeriod() {
		t.Fatalf("purchased capability or initial quota = %+v", applied.GetEntitlement())
	}
	aggregate, err := commercial.GetMyCommerce(userContext, &cloudv1.GetMyCommerceRequest{})
	if err != nil || len(aggregate.GetOrders()) == 0 || aggregate.GetOrders()[0].GetAccountId() != registered.GetAccount().GetAccountId() {
		t.Fatalf("self commerce aggregate=%+v err=%v", aggregate, err)
	}

	otherEmail := "r7-other-" + uuid.NewString() + "@example.com"
	otherRegistered, err := accounts.Register(ctx, &cloudv1.RegisterAccountRequest{Email: otherEmail, Password: "r7-other-password", DisplayName: "R7 隔离测试"})
	if err != nil {
		t.Fatal(err)
	}
	otherIdentity, err := accounts.AuthenticateAccess(ctx, otherRegistered.GetSession().GetAccessToken())
	if err != nil {
		t.Fatal(err)
	}
	otherContext := account.ContextWithIdentity(ctx, otherIdentity)
	otherAggregate, err := commercial.GetMyCommerce(otherContext, &cloudv1.GetMyCommerceRequest{})
	if err != nil || otherAggregate.GetSubscription().GetAccountId() != otherRegistered.GetAccount().GetAccountId() || len(otherAggregate.GetOrders()) != 0 {
		t.Fatalf("other account aggregate=%+v err=%v", otherAggregate, err)
	}
	if _, err := commercial.CompleteDevelopmentPayment(otherContext, &cloudv1.CompleteDevelopmentPaymentRequest{PaymentAttemptId: created.GetPaymentAttempt().GetPaymentAttemptId(), OrderId: created.GetOrder().GetOrderId()}); !errors.Is(err, account.ErrUnauthenticated) {
		t.Fatalf("other account completed first account order: %v", err)
	}

	edgeID := uuid.NewString()
	if err := database.CreateEdge(ctx, edgeconfig.Edge{
		ID: edgeID, Name: "R7 commerce usage Edge", Region: "test", Capacity: 10, PublicEndpoint: "edge.test:41102", Enabled: true, ConfigVersion: 1, Revision: 1,
		SignedConfig: &cloudv1.SignedEdgeDesiredConfig{KeyId: "test", Payload: []byte("payload"), Signature: []byte("signature")}, CreatedAt: clock, UpdatedAt: clock,
	}, []byte("r7-commerce-edge-claim-"+edgeID), clock.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	enrollmentDigest := sha256.Sum256([]byte(uuid.NewString()))
	if _, err := database.CreateDaemonEnrollment(ctx, registered.GetAccount().GetAccountId(), registered.GetAccount().GetDisplayName(), "R7 commerce usage daemon", enrollmentDigest[:], clock.Add(time.Hour), clock); err != nil {
		t.Fatal(err)
	}
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	daemon, err := database.ConsumeDaemonEnrollment(ctx, enrollmentDigest[:], "device-"+uuid.NewString(), "fingerprint-"+uuid.NewString(), publicKey, clock)
	if err != nil {
		t.Fatal(err)
	}
	usageBytes := uint64(360)
	usageEvent := &cloudv1.UsageEvent{
		SchemaVersion: 1, EventId: uuid.NewString(), EdgeId: edgeID, LeaseId: uuid.NewString(), AccountId: registered.GetAccount().GetAccountId(), DaemonId: daemon.ID,
		ClientId: "client-r7-commerce", SessionId: uuid.NewString(), AllocationId: uuid.NewString(), Transport: cloudv1.RelayTransport_RELAY_TRANSPORT_UDP,
		IngressBytes: 120, EgressBytes: usageBytes - 120, StartedAt: timestamppb.New(clock.Add(-time.Second)), EndedAt: timestamppb.New(clock),
	}
	acknowledged, err := database.CommitRelayUsage(ctx, edgeID, []*cloudv1.UsageEvent{usageEvent})
	if err != nil || len(acknowledged) != 1 || acknowledged[0] != usageEvent.GetEventId() {
		t.Fatalf("commit Relay usage ack=%v err=%v", acknowledged, err)
	}
	aggregateAfterUsage, err := commercial.GetMyCommerce(userContext, &cloudv1.GetMyCommerceRequest{})
	if err != nil || aggregateAfterUsage.GetUsage().GetRelayTotalBytes() != usageBytes || aggregateAfterUsage.GetEntitlement().GetRelayUsedBytes() != usageBytes || aggregateAfterUsage.GetEntitlement().GetRelayRemainingBytes() != capability.GetRelayMaxBytesPerPeriod()-usageBytes {
		t.Fatalf("commerce usage aggregate=%+v err=%v", aggregateAfterUsage, err)
	}
	policy := commerce.EntitlementRelayPolicy{Service: commercial}
	limitsAfterUsage, err := policy.Limits(ctx, &cloudv1.ClientSessionSummary{AccountId: registered.GetAccount().GetAccountId()})
	expectedLeaseBytes := capability.GetRelayMaxBytesPerLease()
	if expectedLeaseBytes > aggregateAfterUsage.GetEntitlement().GetRelayRemainingBytes() {
		expectedLeaseBytes = aggregateAfterUsage.GetEntitlement().GetRelayRemainingBytes()
	}
	if err != nil || limitsAfterUsage.MaxBytes != expectedLeaseBytes || limitsAfterUsage.MaxRateBytesPerSecond != capability.GetRelayMaxRateBytesPerSecond() || limitsAfterUsage.MaxConcurrentAllocations != capability.GetRelayMaxConcurrency() {
		t.Fatalf("Relay limits after usage=%+v err=%v", limitsAfterUsage, err)
	}
	duplicate, err := commercial.CompleteDevelopmentPayment(userContext, &cloudv1.CompleteDevelopmentPaymentRequest{PaymentAttemptId: created.GetPaymentAttempt().GetPaymentAttemptId(), OrderId: created.GetOrder().GetOrderId()})
	if err != nil {
		t.Fatal(err)
	}
	if !duplicate.GetDuplicate() || duplicate.GetSubscription().GetRevision() != applied.GetSubscription().GetRevision() || duplicate.GetEntitlement().GetRelayUsedBytes() != usageBytes {
		t.Fatalf("duplicate payment did not return current commerce truth: %+v", duplicate)
	}

	cancelled, err := commercial.ChangeMySubscription(userContext, &cloudv1.ChangeMySubscriptionRequest{Transition: cloudv1.SubscriptionTransition_SUBSCRIPTION_TRANSITION_CANCEL_AT_PERIOD_END, ExpectedRevision: applied.GetSubscription().GetRevision()})
	if err != nil || !cancelled.GetSubscription().GetCancelAtPeriodEnd() {
		t.Fatalf("self cancel subscription=%+v err=%v", cancelled, err)
	}
	resumed, err := commercial.ChangeMySubscription(userContext, &cloudv1.ChangeMySubscriptionRequest{Transition: cloudv1.SubscriptionTransition_SUBSCRIPTION_TRANSITION_RESUME, ExpectedRevision: cancelled.GetSubscription().GetRevision()})
	if err != nil || resumed.GetSubscription().GetCancelAtPeriodEnd() {
		t.Fatalf("self resume subscription=%+v err=%v", resumed, err)
	}
	suspended, err := commercial.TransitionSubscription(adminContext, &cloudv1.TransitionSubscriptionRequest{AccountId: registered.GetAccount().GetAccountId(), Transition: cloudv1.SubscriptionTransition_SUBSCRIPTION_TRANSITION_SUSPEND, ExpectedRevision: resumed.GetSubscription().GetRevision(), Reason: "R7 test suspend"})
	if err != nil {
		t.Fatal(err)
	}
	if suspended.GetEntitlement().GetState() != cloudv1.EntitlementState_ENTITLEMENT_STATE_SUSPENDED {
		t.Fatalf("suspended entitlement = %+v", suspended.GetEntitlement())
	}
	if _, err := policy.Limits(ctx, &cloudv1.ClientSessionSummary{AccountId: registered.GetAccount().GetAccountId()}); err == nil {
		t.Fatal("suspended account received Relay limits")
	}
	restored, err := commercial.TransitionSubscription(adminContext, &cloudv1.TransitionSubscriptionRequest{AccountId: registered.GetAccount().GetAccountId(), Transition: cloudv1.SubscriptionTransition_SUBSCRIPTION_TRANSITION_RESTORE, ExpectedRevision: suspended.GetSubscription().GetRevision(), Reason: "R7 test restore"})
	if err != nil || restored.GetSubscription().GetState() != cloudv1.SubscriptionState_SUBSCRIPTION_STATE_ACTIVE || restored.GetEntitlement().GetState() != cloudv1.EntitlementState_ENTITLEMENT_STATE_ACTIVE {
		t.Fatalf("restored subscription=%+v err=%v", restored, err)
	}
	limitsAfterRestore, err := policy.Limits(ctx, &cloudv1.ClientSessionSummary{AccountId: registered.GetAccount().GetAccountId()})
	if err != nil || limitsAfterRestore != limitsAfterUsage {
		t.Fatalf("Relay limits after restore=%+v want=%+v err=%v", limitsAfterRestore, limitsAfterUsage, err)
	}
	refund := &cloudv1.ApplyPaymentEventRequest{Provider: "development", ProviderEventId: uuid.NewString(), PaymentAttemptId: created.GetPaymentAttempt().GetPaymentAttemptId(), OrderId: created.GetOrder().GetOrderId(), EventType: cloudv1.PaymentEventType_PAYMENT_EVENT_TYPE_REFUNDED, ProviderReference: "r7-refund", OccurredAt: timestamppb.Now()}
	refunded, err := commercial.ApplyPaymentEvent(adminContext, refund)
	if err != nil {
		t.Fatal(err)
	}
	if refunded.GetOrder().GetStatus() != cloudv1.OrderStatus_ORDER_STATUS_REFUNDED || refunded.GetPaymentAttempt().GetStatus() != cloudv1.PaymentAttemptStatus_PAYMENT_ATTEMPT_STATUS_SUCCEEDED {
		t.Fatalf("refund changed payment truth incorrectly: %+v", refunded)
	}
	lateFailure := &cloudv1.ApplyPaymentEventRequest{Provider: refund.GetProvider(), ProviderEventId: uuid.NewString(), PaymentAttemptId: refund.GetPaymentAttemptId(), OrderId: refund.GetOrderId(), EventType: cloudv1.PaymentEventType_PAYMENT_EVENT_TYPE_FAILED, ProviderReference: "r7-late-failure", OccurredAt: timestamppb.Now()}
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
