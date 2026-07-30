package postgres

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anytty/anytty/cloud/relayquota"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type relayFixture struct {
	edgeID, accountID, daemonID, subscriptionID, planID string
	periodStart, periodEnd                              time.Time
}

type relayReserveResult struct {
	response *cloudv1.RelayReserveResponse
	err      error
}

func TestRelayReserveSerializesQuotaAndConcurrency(t *testing.T) {
	database, ctx := relayTestDatabase(t)
	now := time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC)

	quota := seedRelayFixture(t, ctx, database, now, 100, 75, 8)
	quotaSecondEdge := seedRelayEdge(t, ctx, database, now)
	requests := []*cloudv1.RelayReserveRequest{
		newReserveRequest(t, quota, uuid.NewString(), uuid.NewString(), now),
		newReserveRequest(t, quota, uuid.NewString(), uuid.NewString(), now),
	}
	responses := concurrentReserve(t, ctx, database, []string{quota.edgeID, quotaSecondEdge}, requests, now)
	assertOneApplied(t, responses)
	assertUsage(t, ctx, database, quota, 0, 0, 0, 75)

	slots := seedRelayFixture(t, ctx, database, now, 1000, 10, 1)
	slotsSecondEdge := seedRelayEdge(t, ctx, database, now)
	requests = []*cloudv1.RelayReserveRequest{
		newReserveRequest(t, slots, uuid.NewString(), uuid.NewString(), now),
		newReserveRequest(t, slots, uuid.NewString(), uuid.NewString(), now),
	}
	responses = concurrentReserve(t, ctx, database, []string{slots.edgeID, slotsSecondEdge}, requests, now)
	assertOneApplied(t, responses)
	assertUsage(t, ctx, database, slots, 0, 0, 0, 10)
}

func TestRelayConcurrencySpansSubscriptionPeriods(t *testing.T) {
	database, ctx := relayTestDatabase(t)
	now := time.Date(2026, 7, 31, 8, 30, 0, 0, time.UTC)
	fixture := seedRelayFixture(t, ctx, database, now, 1000, 100, 1)
	old := mustReserve(t, ctx, database, fixture.edgeID, newReserveRequest(t, fixture, uuid.NewString(), uuid.NewString(), now), now)
	if old.GetCode() != cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_APPLIED {
		t.Fatalf("old-period reserve=%v", old)
	}

	nextNow := now.Add(time.Minute)
	next := fixture
	next.periodStart, next.periodEnd = nextNow, nextNow.AddDate(0, 1, 0)
	if _, err := database.pool.Exec(ctx, `UPDATE subscriptions SET period_start=$1,period_end=$2,revision=revision+1,updated_at=$1 WHERE subscription_id=$3`, next.periodStart, next.periodEnd, fixture.subscriptionID); err != nil {
		t.Fatal(err)
	}
	blocked := mustReserve(t, ctx, database, fixture.edgeID, newReserveRequest(t, next, uuid.NewString(), uuid.NewString(), nextNow), nextNow)
	if blocked.GetCode() != cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_REJECTED {
		t.Fatalf("cross-period concurrency reserve=%v", blocked)
	}
	assertUsage(t, ctx, database, fixture, 0, 0, 0, 100)

	afterExpiry := old.GetGrant().GetAuthorizedUntil().AsTime().Add(time.Nanosecond)
	recovered := mustReserve(t, ctx, database, fixture.edgeID, newReserveRequest(t, next, uuid.NewString(), uuid.NewString(), afterExpiry), afterExpiry)
	if recovered.GetCode() != cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_APPLIED {
		t.Fatalf("reserve after old-period recovery=%v", recovered)
	}
	assertUsage(t, ctx, database, fixture, 0, 0, 100, 0)
	assertUsage(t, ctx, database, next, 0, 0, 0, 100)
}

func TestRelayCommerceAccountSubscriptionLockOrder(t *testing.T) {
	database, ctx := relayTestDatabase(t)
	now := time.Date(2026, 7, 31, 8, 45, 0, 0, time.UTC)
	fixture := seedRelayFixture(t, ctx, database, now, 1000, 100, 1)
	actorID := "00000000-0000-4000-8000-" + strings.ReplaceAll(fixture.accountID, "-", "")[:12]
	seedRelayActor(t, ctx, database, actorID, now)

	blockerConnection, err := database.pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blockerConnection.Release()
	blocker, err := blockerConnection.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = blocker.Rollback(context.Background()) }()
	if _, err := blocker.Exec(ctx, `SELECT account_id FROM accounts WHERE account_id=$1 FOR UPDATE`, actorID); err != nil {
		t.Fatal(err)
	}

	waiterConnection, err := database.pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer waiterConnection.Release()
	waiter, err := waiterConnection.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = waiter.Rollback(context.Background()) }()
	var waiterPID int32
	if err := waiter.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&waiterPID); err != nil {
		t.Fatal(err)
	}
	waiterDone := make(chan error, 1)
	go func() {
		if err := lockCommerceAccounts(ctx, waiter, fixture.accountID, actorID); err != nil {
			waiterDone <- err
			return
		}
		_, err := waiter.Exec(ctx, `SELECT subscription_id FROM subscriptions WHERE account_id=$1 FOR UPDATE`, fixture.accountID)
		waiterDone <- err
	}()
	waitForPostgresBlock(t, ctx, database, waiterPID)

	probe, err := database.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := probe.Exec(ctx, `SELECT account_id FROM accounts WHERE account_id=$1 FOR UPDATE NOWAIT`, fixture.accountID); err != nil {
		_ = probe.Rollback(context.Background())
		t.Fatalf("account locking was not UUID-sorted: %v", err)
	}
	if _, err := probe.Exec(ctx, `SELECT subscription_id FROM subscriptions WHERE account_id=$1 FOR UPDATE NOWAIT`, fixture.accountID); err != nil {
		_ = probe.Rollback(context.Background())
		t.Fatalf("subscription was locked before all accounts: %v", err)
	}
	if err := probe.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-waiterDone; err != nil {
		t.Fatal(err)
	}
}

func TestLockCommerceAccountsCanonicalUUIDOrder(t *testing.T) {
	database, ctx := relayTestDatabase(t)
	now := time.Date(2026, 7, 31, 8, 50, 0, 0, time.UTC)
	first := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaa1"
	second := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbb2"
	seedRelayActor(t, ctx, database, first, now)
	seedRelayActor(t, ctx, database, second, now)

	blocker, err := database.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = blocker.Rollback(context.Background()) }()
	if _, err := blocker.Exec(ctx, `SELECT account_id FROM accounts WHERE account_id=$1 FOR UPDATE`, first); err != nil {
		t.Fatal(err)
	}
	waiterConnection, err := database.pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer waiterConnection.Release()
	waiter, err := waiterConnection.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = waiter.Rollback(context.Background()) }()
	var waiterPID int32
	if err := waiter.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&waiterPID); err != nil {
		t.Fatal(err)
	}
	waiterDone := make(chan error, 1)
	go func() {
		waiterDone <- lockCommerceAccounts(ctx, waiter, strings.ToUpper(second), first, strings.ToUpper(first))
	}()
	waitForPostgresBlock(t, ctx, database, waiterPID)

	probe, err := database.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := probe.Exec(ctx, `SELECT account_id FROM accounts WHERE account_id=$1 FOR UPDATE NOWAIT`, second); err != nil {
		_ = probe.Rollback(context.Background())
		t.Fatalf("raw UUID spelling changed account lock order: %v", err)
	}
	if err := probe.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-waiterDone; err != nil {
		t.Fatal(err)
	}
}

func TestRelayPublishPlanVersionRaceHasNoDeadlock(t *testing.T) {
	database, ctx := relayTestDatabase(t)
	now := time.Date(2026, 7, 31, 9, 0, 0, 0, time.UTC)
	fixture := seedRelayFixture(t, ctx, database, now, 1000, 100, 1)
	if _, err := database.pool.Exec(ctx, `INSERT INTO plans(plan_id,version,catalog_version,name,description,state,billing_period_days,managed_p2p_enabled,managed_p2p_max_concurrency,relay_enabled,relay_max_concurrency,relay_max_bytes_per_period,relay_max_bytes_per_lease,relay_max_rate_bytes_per_second,cloud_daemon_limit,allowed_regions,revision,created_at) VALUES($1,2,1,'relay test v2','relay test v2','draft',30,true,1,true,1,1000,100,1000,1,ARRAY['test'],1,$2)`, fixture.planID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := database.pool.Exec(ctx, `INSERT INTO plan_prices(plan_id,plan_version,billing_cycle,currency,minor_units) VALUES($1,2,'monthly','USD',100),($1,2,'yearly','USD',1000)`, fixture.planID); err != nil {
		t.Fatal(err)
	}
	const advisoryKey int64 = 748392099
	if _, err := database.pool.Exec(ctx, `CREATE FUNCTION relay_publish_test_barrier() RETURNS trigger LANGUAGE plpgsql AS $body$ BEGIN PERFORM pg_advisory_xact_lock(748392099); RETURN NULL; END $body$`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.pool.Exec(ctx, `CREATE TRIGGER relay_publish_test_barrier AFTER UPDATE ON plans FOR EACH STATEMENT EXECUTE FUNCTION relay_publish_test_barrier()`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = database.pool.Exec(context.Background(), `DROP TRIGGER IF EXISTS relay_publish_test_barrier ON plans`)
		_, _ = database.pool.Exec(context.Background(), `DROP FUNCTION IF EXISTS relay_publish_test_barrier()`)
	})

	barrier, err := database.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = barrier.Rollback(context.Background()) }()
	if _, err := barrier.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, advisoryKey); err != nil {
		t.Fatal(err)
	}
	type publishResult struct {
		plan *cloudv1.PlanDefinition
		err  error
	}
	publishDone := make(chan publishResult, 1)
	go func() {
		plan, err := database.PublishPlanVersion(ctx, &cloudv1.PublishPlanVersionRequest{PlanId: fixture.planID, Version: 2, ExpectedRevision: 1}, fixture.accountID, now)
		publishDone <- publishResult{plan: plan, err: err}
	}()
	waitForPostgresQueryWait(t, ctx, database, `%UPDATE plans SET state='retired'%`)

	probe, err := database.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := probe.Exec(ctx, `SELECT account_id FROM accounts WHERE account_id=$1 FOR UPDATE NOWAIT`, fixture.accountID); err == nil {
		_ = probe.Rollback(context.Background())
		t.Fatal("PublishPlanVersion updated plan before locking actor account")
	}
	_ = probe.Rollback(context.Background())

	request := newReserveRequest(t, fixture, uuid.NewString(), uuid.NewString(), now)
	reserveDone := make(chan relayReserveResult, 1)
	go func() {
		response, err := database.reserveRelayAt(ctx, fixture.edgeID, request, now)
		reserveDone <- relayReserveResult{response: response, err: err}
	}()
	waitForPostgresQueryWait(t, ctx, database, `%SELECT state,revision FROM accounts WHERE account_id=$1 FOR UPDATE%`)
	if err := barrier.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	published := <-publishDone
	reserved := <-reserveDone
	if published.err != nil || published.plan.GetVersion() != 2 || published.plan.GetState() != cloudv1.PlanState_PLAN_STATE_PUBLISHED {
		t.Fatalf("publish=%v err=%v", published.plan, published.err)
	}
	if reserved.err != nil || reserved.response.GetCode() != cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_APPLIED {
		t.Fatalf("reserve=%v err=%v", reserved.response, reserved.err)
	}
}

func TestRelayTransitionSubscriptionRaceHasNoDeadlock(t *testing.T) {
	database, ctx := relayTestDatabase(t)
	for round := 0; round < 8; round++ {
		now := time.Date(2026, 7, 31, 10, round, 0, 0, time.UTC)
		fixture := seedRelayFixture(t, ctx, database, now, 1000, 100, 1)
		actorID := uuid.NewString()
		seedRelayActor(t, ctx, database, actorID, now)
		request := newReserveRequest(t, fixture, uuid.NewString(), uuid.NewString(), now)
		start := make(chan struct{})
		var wait sync.WaitGroup
		wait.Add(2)
		var reserve *cloudv1.RelayReserveResponse
		var transition *cloudv1.TransitionSubscriptionResponse
		var reserveErr, transitionErr error
		go func() {
			defer wait.Done()
			<-start
			reserve, reserveErr = database.reserveRelayAt(ctx, fixture.edgeID, request, now)
		}()
		go func() {
			defer wait.Done()
			<-start
			transition, transitionErr = database.TransitionSubscription(ctx, &cloudv1.TransitionSubscriptionRequest{AccountId: fixture.accountID, Transition: cloudv1.SubscriptionTransition_SUBSCRIPTION_TRANSITION_SUSPEND, ExpectedRevision: 1, Reason: "Relay lock-order race"}, actorID, now)
		}()
		close(start)
		wait.Wait()
		if reserveErr != nil || transitionErr != nil || transition.GetSubscription().GetState() != cloudv1.SubscriptionState_SUBSCRIPTION_STATE_SUSPENDED {
			t.Fatalf("round %d reserve=%v/%v transition=%v/%v", round, reserve, reserveErr, transition, transitionErr)
		}
		if reserve.GetCode() != cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_APPLIED && reserve.GetCode() != cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_REJECTED {
			t.Fatalf("round %d unexpected reserve=%v", round, reserve)
		}
	}
}

func TestRelayPaidSubscriptionRolloverRaceHasNoDeadlock(t *testing.T) {
	database, ctx := relayTestDatabase(t)
	for round := 0; round < 8; round++ {
		now := time.Date(2026, 7, 31, 11, round, 0, 0, time.UTC)
		fixture := seedRelayFixture(t, ctx, database, now, 1000, 100, 1)
		actorID := uuid.NewString()
		seedRelayActor(t, ctx, database, actorID, now)
		orderID, attemptID := seedRelayPayment(t, ctx, database, fixture, now)
		request := newReserveRequest(t, fixture, uuid.NewString(), uuid.NewString(), now)
		event := &cloudv1.ApplyPaymentEventRequest{Provider: "relay-test", ProviderEventId: uuid.NewString(), PaymentAttemptId: attemptID, OrderId: orderID, EventType: cloudv1.PaymentEventType_PAYMENT_EVENT_TYPE_SUCCEEDED, ProviderReference: "paid", OccurredAt: timestamppb.New(now)}
		start := make(chan struct{})
		var wait sync.WaitGroup
		wait.Add(2)
		var reserve *cloudv1.RelayReserveResponse
		var payment *cloudv1.ApplyPaymentEventResponse
		var reserveErr, paymentErr error
		go func() {
			defer wait.Done()
			<-start
			reserve, reserveErr = database.reserveRelayAt(ctx, fixture.edgeID, request, now)
		}()
		go func() {
			defer wait.Done()
			<-start
			payment, paymentErr = database.ApplyPaymentEvent(ctx, event, actorID, now)
		}()
		close(start)
		wait.Wait()
		if reserveErr != nil || paymentErr != nil || reserve.GetCode() != cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_APPLIED || !payment.GetSubscription().GetPeriodStart().AsTime().Equal(now) {
			t.Fatalf("round %d reserve=%v/%v payment=%v/%v", round, reserve, reserveErr, payment, paymentErr)
		}
	}
}

func TestRelayReservationIdentityAndRenewSequence(t *testing.T) {
	database, ctx := relayTestDatabase(t)
	now := time.Date(2026, 7, 31, 9, 0, 0, 0, time.UTC)
	fixture := seedRelayFixture(t, ctx, database, now, 1000, 100, 2)
	request := newReserveRequest(t, fixture, uuid.NewString(), uuid.NewString(), now)
	first := mustReserve(t, ctx, database, fixture.edgeID, request, now)
	if first.GetCode() != cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_APPLIED || first.GetGrant().GetRenewSequence() != 0 {
		t.Fatalf("initial reserve=%v", first)
	}
	replay := mustReserve(t, ctx, database, fixture.edgeID, request, now.Add(time.Second))
	if replay.GetCode() != cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_REPLAY || !replay.GetGrant().GetAuthorizedUntil().AsTime().Equal(first.GetGrant().GetAuthorizedUntil().AsTime()) {
		t.Fatalf("same-ID replay changed grant: %v", replay)
	}
	conflicting := newReserveRequest(t, fixture, request.GetReservationId(), uuid.NewString(), now)
	if response := mustReserve(t, ctx, database, fixture.edgeID, conflicting, now); response.GetCode() != cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_CONFLICT {
		t.Fatalf("same ID with another digest=%v", response)
	}
	otherEdge := seedRelayEdge(t, ctx, database, now)
	if response := mustReserve(t, ctx, database, otherEdge, request, now); response.GetCode() != cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_CONFLICT {
		t.Fatalf("same ID replay crossed Edge ownership=%v", response)
	}
	anotherID := newReserveRequest(t, fixture, uuid.NewString(), request.GetSessionId(), now)
	if response := mustReserve(t, ctx, database, fixture.edgeID, anotherID, now); response.GetCode() != cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_CONFLICT {
		t.Fatalf("same session with another ID=%v", response)
	}

	digest := first.GetGrant().GetPolicyDigest()
	seq0 := &cloudv1.RelayRenewRequest{ReservationId: request.GetReservationId(), RenewSequence: 0, PolicyDigest: digest, ObservedAt: timestamppb.New(now.Add(time.Second))}
	if response := mustRenew(t, ctx, database, fixture.edgeID, seq0, now.Add(time.Second)); response.GetCode() != cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_REPLAY || !response.GetGrant().GetAuthorizedUntil().AsTime().Equal(first.GetGrant().GetAuthorizedUntil().AsTime()) {
		t.Fatalf("renew seq=0 replay=%v", response)
	}
	jump := &cloudv1.RelayRenewRequest{ReservationId: request.GetReservationId(), RenewSequence: 2, PolicyDigest: digest, ObservedAt: timestamppb.New(now.Add(time.Second))}
	if response := mustRenew(t, ctx, database, fixture.edgeID, jump, now.Add(time.Second)); response.GetCode() != cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_REJECTED {
		t.Fatalf("renew sequence gap=%v", response)
	}
	seq1 := &cloudv1.RelayRenewRequest{ReservationId: request.GetReservationId(), RenewSequence: 1, PolicyDigest: digest, ObservedAt: timestamppb.New(now.Add(30 * time.Second))}
	renewed := mustRenew(t, ctx, database, fixture.edgeID, seq1, now.Add(30*time.Second))
	if renewed.GetCode() != cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_APPLIED || renewed.GetGrant().GetRenewSequence() != 1 {
		t.Fatalf("renew seq=1=%v", renewed)
	}
	lostResponseReplay := mustRenew(t, ctx, database, fixture.edgeID, seq1, now.Add(40*time.Second))
	if lostResponseReplay.GetCode() != cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_REPLAY || !lostResponseReplay.GetGrant().GetAuthorizedUntil().AsTime().Equal(renewed.GetGrant().GetAuthorizedUntil().AsTime()) {
		t.Fatalf("lost renewal response replay=%v", lostResponseReplay)
	}

	if _, err := database.pool.Exec(ctx, `UPDATE accounts SET revision=revision+1,updated_at=$1 WHERE account_id=$2`, now.Add(time.Minute), fixture.accountID); err != nil {
		t.Fatal(err)
	}
	seq2 := &cloudv1.RelayRenewRequest{ReservationId: request.GetReservationId(), RenewSequence: 2, PolicyDigest: digest, ObservedAt: timestamppb.New(now.Add(time.Minute))}
	if response := mustRenew(t, ctx, database, fixture.edgeID, seq2, now.Add(time.Minute)); response.GetCode() != cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_REJECTED {
		t.Fatalf("policy update did not stop renewal: %v", response)
	}
}

func TestRelayRevokeInterleavingAndOldPeriodSettlement(t *testing.T) {
	database, ctx := relayTestDatabase(t)
	now := time.Date(2026, 7, 31, 10, 0, 0, 0, time.UTC)
	fixture := seedRelayFixture(t, ctx, database, now, 1000, 100, 3)

	tx, err := database.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE daemons SET revoked=true,revision=revision+1,updated_at=$1 WHERE account_id=$2 AND daemon_id=$3`, now, fixture.accountID, fixture.daemonID); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	result := make(chan *cloudv1.RelayReserveResponse, 1)
	errors := make(chan error, 1)
	request := newReserveRequest(t, fixture, uuid.NewString(), uuid.NewString(), now)
	go func() {
		close(started)
		response, reserveErr := database.reserveRelayAt(ctx, fixture.edgeID, request, now)
		if reserveErr != nil {
			errors <- reserveErr
			return
		}
		result <- response
	}()
	<-started
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errors:
		t.Fatal(err)
	case response := <-result:
		if response.GetCode() != cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_REJECTED {
			t.Fatalf("reserve crossed committed daemon revoke: %v", response)
		}
	}

	if _, err := database.pool.Exec(ctx, `UPDATE daemons SET revoked=false,revision=revision+1,updated_at=$1 WHERE daemon_id=$2`, now, fixture.daemonID); err != nil {
		t.Fatal(err)
	}
	oldRequest := newReserveRequest(t, fixture, uuid.NewString(), uuid.NewString(), now)
	old := mustReserve(t, ctx, database, fixture.edgeID, oldRequest, now)
	nextStart, nextEnd := fixture.periodEnd, fixture.periodEnd.AddDate(0, 1, 0)
	if _, err := database.pool.Exec(ctx, `UPDATE subscriptions SET period_start=$1,period_end=$2,revision=revision+1,updated_at=$1 WHERE subscription_id=$3`, nextStart, nextEnd, fixture.subscriptionID); err != nil {
		t.Fatal(err)
	}
	settlement := exactSettlement(old.GetGrant(), 30, 40, now.Add(time.Hour))
	ack, err := database.settleRelayAt(ctx, fixture.edgeID, settlement, now.Add(time.Hour))
	if err != nil || ack.GetCode() != cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_APPLIED {
		t.Fatalf("settle old period ack=%v err=%v", ack, err)
	}
	assertUsage(t, ctx, database, fixture, 30, 40, 0, 0)
}

func TestRelaySettlementReplayConflictAndLazyRecoveryRace(t *testing.T) {
	database, ctx := relayTestDatabase(t)
	now := time.Date(2026, 7, 31, 11, 0, 0, 0, time.UTC)
	fixture := seedRelayFixture(t, ctx, database, now, 1000, 100, 3)
	request := newReserveRequest(t, fixture, uuid.NewString(), uuid.NewString(), now)
	reserved := mustReserve(t, ctx, database, fixture.edgeID, request, now)
	settlement := exactSettlement(reserved.GetGrant(), 20, 30, now.Add(time.Minute))
	ack, err := database.settleRelayAt(ctx, fixture.edgeID, settlement, now.Add(time.Minute))
	if err != nil || ack.GetCode() != cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_APPLIED {
		t.Fatalf("settlement=%v err=%v", ack, err)
	}
	replayed, err := database.settleRelayAt(ctx, fixture.edgeID, settlement, now.Add(2*time.Minute))
	if err != nil || replayed.GetCode() != cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_REPLAY {
		t.Fatalf("lost ACK replay=%v err=%v", replayed, err)
	}
	conflicting := exactSettlement(reserved.GetGrant(), 21, 30, now.Add(time.Minute))
	conflict, err := database.settleRelayAt(ctx, fixture.edgeID, conflicting, now.Add(2*time.Minute))
	if err != nil || conflict.GetCode() != cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_CONFLICT {
		t.Fatalf("different counters=%v err=%v", conflict, err)
	}

	raceRequest := newReserveRequest(t, fixture, uuid.NewString(), uuid.NewString(), now)
	raceGrant := mustReserve(t, ctx, database, fixture.edgeID, raceRequest, now).GetGrant()
	afterExpiry := raceGrant.GetAuthorizedUntil().AsTime().Add(time.Nanosecond)
	exact := exactSettlement(raceGrant, 10, 15, afterExpiry)
	start := make(chan struct{})
	var wait sync.WaitGroup
	wait.Add(2)
	var query *cloudv1.RelayQueryResponse
	var settle *cloudv1.RelaySettlementAck
	var queryErr, settleErr error
	go func() {
		defer wait.Done()
		<-start
		query, queryErr = database.queryRelayAt(ctx, fixture.edgeID, &cloudv1.RelayQueryRequest{ReservationId: raceGrant.GetReservationId()}, afterExpiry)
	}()
	go func() {
		defer wait.Done()
		<-start
		settle, settleErr = database.settleRelayAt(ctx, fixture.edgeID, exact, afterExpiry)
	}()
	close(start)
	wait.Wait()
	if queryErr != nil || settleErr != nil || query.GetCode() != cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_TERMINAL || settle.GetCode() == cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_CONFLICT {
		t.Fatalf("lazy recovery race query=%v/%v settle=%v/%v", query, queryErr, settle, settleErr)
	}
	var kind string
	var ingress, egress, recovery int64
	if err := database.pool.QueryRow(ctx, `SELECT settlement_kind,settled_ingress_bytes,settled_egress_bytes,recovery_bytes FROM relay_reservations WHERE reservation_id=$1`, raceGrant.GetReservationId()).Scan(&kind, &ingress, &egress, &recovery); err != nil {
		t.Fatal(err)
	}
	if kind == "exact" && (ingress != 10 || egress != 15 || recovery != 0) {
		t.Fatalf("exact terminal changed: %s %d/%d/%d", kind, ingress, egress, recovery)
	}
	if kind == "recovery_max" && (ingress != 0 || egress != 0 || recovery != 100) {
		t.Fatalf("recovery terminal changed: %s %d/%d/%d", kind, ingress, egress, recovery)
	}
	assertUsageInvariant(t, ctx, database, fixture)
}

func TestRelaySchemaRejectsInvalidCountersAndTerminalRows(t *testing.T) {
	database, ctx := relayTestDatabase(t)
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	fixture := seedRelayFixture(t, ctx, database, now, 1000, 100, 2)
	request := newReserveRequest(t, fixture, uuid.NewString(), uuid.NewString(), now)
	reserved := mustReserve(t, ctx, database, fixture.edgeID, request, now)
	if reserved.GetCode() != cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_APPLIED {
		t.Fatalf("reserve=%v", reserved)
	}

	invalid := []struct {
		name string
		sql  string
		args []any
	}{
		{"numeric aggregate overflow", `UPDATE usage_periods SET committed_ingress_bytes=9223372036854775807 WHERE account_id=$1 AND subscription_id=$2 AND period_start=$3 AND period_end=$4`, []any{fixture.accountID, fixture.subscriptionID, fixture.periodStart, fixture.periodEnd}},
		{"authority past period", `UPDATE relay_reservations SET authorized_until=period_end+interval '1 second' WHERE reservation_id=$1`, []any{request.GetReservationId()}},
		{"incomplete terminal row", `UPDATE relay_reservations SET state='settled' WHERE reservation_id=$1`, []any{request.GetReservationId()}},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			if _, err := database.pool.Exec(ctx, test.sql, test.args...); err == nil {
				t.Fatal("schema accepted invalid Relay state")
			}
		})
	}
	assertUsage(t, ctx, database, fixture, 0, 0, 0, 100)
	assertUsageInvariant(t, ctx, database, fixture)
}

func relayTestDatabase(t *testing.T) (*Database, context.Context) {
	t.Helper()
	databaseURL := os.Getenv("ANYTTY_CLOUD_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("ANYTTY_CLOUD_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	database, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return database, ctx
}

func seedRelayFixture(t *testing.T, ctx context.Context, database *Database, now time.Time, quota, maxSession int64, concurrency int32) relayFixture {
	t.Helper()
	fixture := relayFixture{edgeID: uuid.NewString(), accountID: uuid.NewString(), daemonID: uuid.NewString(), subscriptionID: uuid.NewString(), planID: "relay-" + uuid.NewString(), periodStart: now.Add(-time.Hour), periodEnd: now.Add(24 * time.Hour)}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("relay-test-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := database.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	statements := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO edge_deployments(edge_id,name,region,capacity,public_endpoint,enabled,desired_config_version,revision,created_at,updated_at) VALUES($1,'relay test','test',10,'edge.test:41102',true,1,1,$2,$2)`, []any{fixture.edgeID, now}},
		{`INSERT INTO accounts(account_id,display_name,state,revision,created_at,updated_at) VALUES($1,'relay test','active',1,$2,$2)`, []any{fixture.accountID, now}},
		{`INSERT INTO account_credentials(account_id,password_hash,revision,updated_at) VALUES($1,$2,1,$3)`, []any{fixture.accountID, passwordHash, now}},
		{`INSERT INTO plans(plan_id,version,catalog_version,name,description,state,billing_period_days,managed_p2p_enabled,managed_p2p_max_concurrency,relay_enabled,relay_max_concurrency,relay_max_bytes_per_period,relay_max_bytes_per_lease,relay_max_rate_bytes_per_second,cloud_daemon_limit,allowed_regions,revision,created_at,published_at) VALUES($1,1,1,'relay test','relay test','published',30,true,1,true,$2,$3,$4,1000,1,ARRAY['test'],1,$5,$5)`, []any{fixture.planID, concurrency, quota, maxSession, now}},
		{`INSERT INTO plan_prices(plan_id,plan_version,billing_cycle,currency,minor_units) VALUES($1,1,'monthly','USD',100),($1,1,'yearly','USD',1000)`, []any{fixture.planID}},
		{`INSERT INTO subscriptions(subscription_id,account_id,plan_id,plan_version,state,cancel_at_period_end,period_start,period_end,revision,updated_at) VALUES($1,$2,$3,1,'active',false,$4,$5,1,$6)`, []any{fixture.subscriptionID, fixture.accountID, fixture.planID, fixture.periodStart, fixture.periodEnd, now}},
		{`INSERT INTO daemons(daemon_id,account_id,display_name,device_id,device_public_key,device_fingerprint,revoked,revision,created_at,updated_at) VALUES($1,$2,'relay test',$3,$4,$5,false,1,$6,$6)`, []any{fixture.daemonID, fixture.accountID, "device-" + uuid.NewString(), make([]byte, 32), "fingerprint-" + uuid.NewString(), now}},
	}
	for _, statement := range statements {
		if _, err := tx.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func seedRelayEdge(t *testing.T, ctx context.Context, database *Database, now time.Time) string {
	t.Helper()
	edgeID := uuid.NewString()
	if _, err := database.pool.Exec(ctx, `INSERT INTO edge_deployments(edge_id,name,region,capacity,public_endpoint,enabled,desired_config_version,revision,created_at,updated_at) VALUES($1,'relay peer','test',10,$2,true,1,1,$3,$3)`, edgeID, "edge-"+edgeID+":41102", now); err != nil {
		t.Fatal(err)
	}
	return edgeID
}

func seedRelayActor(t *testing.T, ctx context.Context, database *Database, actorID string, now time.Time) {
	t.Helper()
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("relay-actor-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := database.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(ctx, `INSERT INTO accounts(account_id,display_name,state,revision,created_at,updated_at) VALUES($1,'relay actor','active',1,$2,$2) ON CONFLICT (account_id) DO NOTHING`, actorID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO account_credentials(account_id,password_hash,revision,updated_at) VALUES($1,$2,1,$3) ON CONFLICT (account_id) DO NOTHING`, actorID, passwordHash, now); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func seedRelayPayment(t *testing.T, ctx context.Context, database *Database, fixture relayFixture, now time.Time) (string, string) {
	t.Helper()
	orderID, attemptID := uuid.NewString(), uuid.NewString()
	tx, err := database.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(ctx, `INSERT INTO orders(order_id,account_id,plan_id,plan_version,status,currency,amount_minor,provider,idempotency_key,requested_transition,revision,created_at) VALUES($1,$2,$3,1,'pending','USD',100,'relay-test',$4,'renew',1,$5)`, orderID, fixture.accountID, fixture.planID, uuid.NewString(), now); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO payment_attempts(payment_attempt_id,order_id,account_id,provider,status,revision,created_at,updated_at) VALUES($1,$2,$3,'relay-test','pending',1,$4,$4)`, attemptID, orderID, fixture.accountID, now); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return orderID, attemptID
}

func waitForPostgresBlock(t *testing.T, ctx context.Context, database *Database, backendPID int32) {
	t.Helper()
	for {
		var blocked bool
		if err := database.pool.QueryRow(ctx, `SELECT cardinality(pg_blocking_pids($1)) > 0`, backendPID).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		default:
		}
	}
}

func waitForPostgresQueryWait(t *testing.T, ctx context.Context, database *Database, pattern string) {
	t.Helper()
	for {
		var waiting bool
		if err := database.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND wait_event_type='Lock' AND query LIKE $1)`, pattern).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		default:
		}
	}
}

func newReserveRequest(t *testing.T, fixture relayFixture, reservationID, sessionID string, observed time.Time) *cloudv1.RelayReserveRequest {
	t.Helper()
	request := &cloudv1.RelayReserveRequest{ReservationId: reservationID, AccountId: fixture.accountID, DaemonId: fixture.daemonID, ClientId: "client-test", SessionId: sessionID, ObservedAt: timestamppb.New(observed)}
	digest, err := relayquota.ReserveRequestDigest(request)
	if err != nil {
		t.Fatal(err)
	}
	request.RequestDigest = digest
	return request
}

func concurrentReserve(t *testing.T, ctx context.Context, database *Database, edgeIDs []string, requests []*cloudv1.RelayReserveRequest, now time.Time) []*cloudv1.RelayReserveResponse {
	t.Helper()
	if len(edgeIDs) != len(requests) {
		t.Fatal("one Edge identity is required per concurrent reserve")
	}
	start := make(chan struct{})
	responses := make([]*cloudv1.RelayReserveResponse, len(requests))
	errors := make([]error, len(requests))
	var wait sync.WaitGroup
	for index := range requests {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			responses[index], errors[index] = database.reserveRelayAt(ctx, edgeIDs[index], requests[index], now)
		}(index)
	}
	close(start)
	wait.Wait()
	for _, err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	return responses
}

func assertOneApplied(t *testing.T, responses []*cloudv1.RelayReserveResponse) {
	t.Helper()
	applied := 0
	for _, response := range responses {
		if response.GetCode() == cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_APPLIED {
			applied++
		}
	}
	if applied != 1 {
		t.Fatalf("applied=%d responses=%v", applied, responses)
	}
}

func mustReserve(t *testing.T, ctx context.Context, database *Database, edgeID string, request *cloudv1.RelayReserveRequest, now time.Time) *cloudv1.RelayReserveResponse {
	t.Helper()
	response, err := database.reserveRelayAt(ctx, edgeID, request, now)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func mustRenew(t *testing.T, ctx context.Context, database *Database, edgeID string, request *cloudv1.RelayRenewRequest, now time.Time) *cloudv1.RelayRenewResponse {
	t.Helper()
	response, err := database.renewRelayAt(ctx, edgeID, request, now)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func exactSettlement(grant *cloudv1.RelayGrant, ingress, egress uint64, observed time.Time) *cloudv1.RelaySettlement {
	return &cloudv1.RelaySettlement{ReservationId: grant.GetReservationId(), Kind: cloudv1.RelaySettlementKind_RELAY_SETTLEMENT_KIND_EXACT, IngressBytes: ingress, EgressBytes: egress, PolicyDigest: append([]byte(nil), grant.GetPolicyDigest()...), ObservedAt: timestamppb.New(observed)}
}

func assertUsage(t *testing.T, ctx context.Context, database *Database, fixture relayFixture, ingress, egress, recovery, held int64) {
	t.Helper()
	var actual [4]int64
	err := database.pool.QueryRow(ctx, `SELECT committed_ingress_bytes,committed_egress_bytes,recovery_bytes,held_bytes FROM usage_periods WHERE account_id=$1 AND subscription_id=$2 AND period_start=$3 AND period_end=$4`, fixture.accountID, fixture.subscriptionID, fixture.periodStart, fixture.periodEnd).
		Scan(&actual[0], &actual[1], &actual[2], &actual[3])
	if err != nil {
		t.Fatal(err)
	}
	want := [4]int64{ingress, egress, recovery, held}
	if actual != want {
		t.Fatalf("usage=%v want=%v", actual, want)
	}
}

func assertUsageInvariant(t *testing.T, ctx context.Context, database *Database, fixture relayFixture) {
	t.Helper()
	var valid bool
	err := database.pool.QueryRow(ctx, `SELECT committed_ingress_bytes>=0 AND committed_egress_bytes>=0 AND recovery_bytes>=0 AND held_bytes>=0 AND committed_ingress_bytes::numeric+committed_egress_bytes::numeric+recovery_bytes::numeric+held_bytes::numeric<=9223372036854775807 FROM usage_periods WHERE account_id=$1 AND subscription_id=$2 AND period_start=$3 AND period_end=$4`, fixture.accountID, fixture.subscriptionID, fixture.periodStart, fixture.periodEnd).Scan(&valid)
	if err != nil && err != pgx.ErrNoRows {
		t.Fatal(err)
	}
	if !valid {
		t.Fatal("usage period invariant is false")
	}
}
