package postgres

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/anytty/anytty/cloud/controller/commerce"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const planSelect = `SELECT p.plan_id,p.version,p.name,p.description,p.state,p.billing_period_days,
       monthly.currency,monthly.minor_units,yearly.currency,yearly.minor_units,
       p.managed_p2p_enabled,p.managed_p2p_max_concurrency,p.relay_enabled,p.relay_max_concurrency,
       p.relay_max_bytes_per_period,p.relay_max_bytes_per_lease,p.relay_max_rate_bytes_per_second,
       p.cloud_daemon_limit,p.allowed_regions,p.revision,p.created_at,p.published_at
  FROM plans p
  JOIN plan_prices monthly ON monthly.plan_id=p.plan_id AND monthly.plan_version=p.version AND monthly.billing_cycle='monthly'
  JOIN plan_prices yearly ON yearly.plan_id=p.plan_id AND yearly.plan_version=p.version AND yearly.billing_cycle='yearly'`

// ListPlans 返回套餐不可变版本；运营查询可包含 draft/retired。
func (database *Database) ListPlans(ctx context.Context, includeUnpublished bool) ([]*cloudv1.PlanDefinition, error) {
	query := planSelect
	if !includeUnpublished {
		query += ` WHERE p.state='published'`
	}
	query += ` ORDER BY p.plan_id,p.version DESC`
	rows, err := database.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]*cloudv1.PlanDefinition, 0)
	for rows.Next() {
		plan, err := scanPlan(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, plan)
	}
	return result, rows.Err()
}

// CreatePlanVersion 在新 draft catalog 中插入不可变套餐版本和两个价格周期。
func (database *Database) CreatePlanVersion(ctx context.Context, request *cloudv1.CreatePlanVersionRequest, actorID string, now time.Time) (*cloudv1.PlanDefinition, error) {
	tx, err := database.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var catalogVersion, version uint64
	if err := tx.QueryRow(ctx, `SELECT coalesce(max(catalog_version),0)+1 FROM plan_catalog_versions`).Scan(&catalogVersion); err != nil {
		return nil, err
	}
	if err := tx.QueryRow(ctx, `SELECT coalesce(max(version),0)+1 FROM plans WHERE plan_id=$1`, request.GetPlanId()).Scan(&version); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO plan_catalog_versions(catalog_version,state,created_by,created_at) VALUES($1,'draft',$2,$3)`, catalogVersion, actorID, now); err != nil {
		return nil, err
	}
	capability := request.GetCapability()
	if _, err := tx.Exec(ctx, `INSERT INTO plans(plan_id,version,catalog_version,name,description,state,billing_period_days,managed_p2p_enabled,managed_p2p_max_concurrency,relay_enabled,relay_max_concurrency,relay_max_bytes_per_period,relay_max_bytes_per_lease,relay_max_rate_bytes_per_second,cloud_daemon_limit,allowed_regions,revision,created_at) VALUES($1,$2,$3,$4,$5,'draft',$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,1,$16)`, request.GetPlanId(), version, catalogVersion, request.GetName(), request.GetDescription(), request.GetBillingPeriodDays(), capability.GetManagedP2PEnabled(), capability.GetManagedP2PMaxConcurrency(), capability.GetRelayEnabled(), capability.GetRelayMaxConcurrency(), capability.GetRelayMaxBytesPerPeriod(), capability.GetRelayMaxBytesPerLease(), capability.GetRelayMaxRateBytesPerSecond(), capability.GetCloudDaemonLimit(), capability.GetAllowedRegions(), now); err != nil {
		return nil, err
	}
	if err := insertPlanPrice(ctx, tx, request.GetPlanId(), version, "monthly", request.GetMonthlyPrice()); err != nil {
		return nil, err
	}
	if err := insertPlanPrice(ctx, tx, request.GetPlanId(), version, "yearly", request.GetYearlyPrice()); err != nil {
		return nil, err
	}
	if err := insertOperatorAudit(ctx, tx, actorID, "plan.create_version", "plan", fmt.Sprintf("%s:%d", request.GetPlanId(), version), "", "applied", now); err != nil {
		return nil, err
	}
	plan, err := scanPlan(tx.QueryRow(ctx, planSelect+` WHERE p.plan_id=$1 AND p.version=$2`, request.GetPlanId(), version))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return plan, nil
}

// PublishPlanVersion CAS 发布 draft 并退休同一 plan 的旧发布版本。
func (database *Database) PublishPlanVersion(ctx context.Context, request *cloudv1.PublishPlanVersionRequest, actorID string, now time.Time) (*cloudv1.PlanDefinition, error) {
	tx, err := database.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `UPDATE plans SET state='retired',revision=revision+1 WHERE plan_id=$1 AND state='published' AND version<>$2`, request.GetPlanId(), request.GetVersion()); err != nil {
		return nil, err
	}
	var catalogVersion uint64
	err = tx.QueryRow(ctx, `UPDATE plans SET state='published',published_at=$1,revision=revision+1 WHERE plan_id=$2 AND version=$3 AND state='draft' AND revision=$4 RETURNING catalog_version`, now, request.GetPlanId(), request.GetVersion(), request.GetExpectedRevision()).Scan(&catalogVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, commerce.ErrCommerceConflict
	}
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE plan_catalog_versions SET state='published',published_at=$1 WHERE catalog_version=$2`, now, catalogVersion); err != nil {
		return nil, err
	}
	if err := insertOperatorAudit(ctx, tx, actorID, "plan.publish", "plan", fmt.Sprintf("%s:%d", request.GetPlanId(), request.GetVersion()), "", "applied", now); err != nil {
		return nil, err
	}
	plan, err := scanPlan(tx.QueryRow(ctx, planSelect+` WHERE p.plan_id=$1 AND p.version=$2`, request.GetPlanId(), request.GetVersion()))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return plan, nil
}

// CreateOrder 原子创建订单、支付尝试和运营审计；幂等键重复返回原对象。
func (database *Database) CreateOrder(ctx context.Context, request *cloudv1.CreateOrderRequest, actorID string, now time.Time) (*cloudv1.CreateOrderResponse, error) {
	tx, err := database.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	order, attempt, err := findOrderByIdempotency(ctx, tx, request.GetAccountId(), request.GetIdempotencyKey())
	if err == nil {
		if order.GetPlanId() != request.GetPlanId() || order.GetPlanVersion() != request.GetPlanVersion() || order.GetProvider() != request.GetProvider() {
			return nil, commerce.ErrCommerceConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return &cloudv1.CreateOrderResponse{Order: order, PaymentAttempt: attempt}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	plan, err := scanPlan(tx.QueryRow(ctx, planSelect+` WHERE p.plan_id=$1 AND p.version=$2 AND p.state='published'`, request.GetPlanId(), request.GetPlanVersion()))
	if err != nil {
		return nil, commerce.ErrInvalidTransition
	}
	if err := validatePaidTransition(ctx, tx, request); err != nil {
		return nil, err
	}
	price := plan.GetMonthlyPrice()
	if request.GetYearly() {
		price = plan.GetYearlyPrice()
	}
	orderID, attemptID := uuid.NewString(), uuid.NewString()
	transition := subscriptionTransitionName(request.GetRequestedTransition())
	if _, err := tx.Exec(ctx, `INSERT INTO orders(order_id,account_id,plan_id,plan_version,status,currency,amount_minor,provider,idempotency_key,requested_transition,revision,created_at) VALUES($1,$2,$3,$4,'pending',$5,$6,$7,$8,$9,1,$10)`, orderID, request.GetAccountId(), plan.GetPlanId(), plan.GetVersion(), price.GetCurrency(), price.GetMinorUnits(), request.GetProvider(), request.GetIdempotencyKey(), transition, now); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO payment_attempts(payment_attempt_id,order_id,account_id,provider,status,revision,created_at,updated_at) VALUES($1,$2,$3,$4,'pending',1,$5,$5)`, attemptID, orderID, request.GetAccountId(), request.GetProvider(), now); err != nil {
		return nil, err
	}
	if err := insertOperatorAudit(ctx, tx, actorID, "order.create", "order", orderID, "", "applied", now); err != nil {
		return nil, err
	}
	order, err = scanOrder(tx.QueryRow(ctx, orderSelect+` WHERE o.order_id=$1`, orderID))
	if err != nil {
		return nil, err
	}
	attempt, err = scanPaymentAttempt(tx.QueryRow(ctx, paymentAttemptSelect+` WHERE payment_attempt_id=$1`, attemptID))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &cloudv1.CreateOrderResponse{Order: order, PaymentAttempt: attempt}, nil
}

// ApplyPaymentEvent 以 provider event 为幂等键，在单事务推进订单、尝试和订阅。
func (database *Database) ApplyPaymentEvent(ctx context.Context, request *cloudv1.ApplyPaymentEventRequest, actorID string, now time.Time) (*cloudv1.ApplyPaymentEventResponse, error) {
	tx, err := database.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	body, err := (proto.MarshalOptions{Deterministic: true}).Marshal(request)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(body)
	tag, err := tx.Exec(ctx, `INSERT INTO payment_events(provider,provider_event_id,event_digest,payment_attempt_id,order_id,event_type,state,provider_reference,occurred_at) VALUES($1,$2,$3,$4,$5,$6,'received',$7,$8) ON CONFLICT(provider,provider_event_id) DO NOTHING`, request.GetProvider(), request.GetProviderEventId(), digest[:], request.GetPaymentAttemptId(), request.GetOrderId(), paymentEventName(request.GetEventType()), request.GetProviderReference(), request.GetOccurredAt().AsTime())
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		var storedDigest []byte
		if err := tx.QueryRow(ctx, `SELECT event_digest FROM payment_events WHERE provider=$1 AND provider_event_id=$2`, request.GetProvider(), request.GetProviderEventId()).Scan(&storedDigest); err != nil {
			return nil, err
		}
		if !equalDigest(storedDigest, digest[:]) {
			return nil, commerce.ErrCommerceConflict
		}
		response, err := paymentAggregate(ctx, tx, request.GetOrderId(), request.GetPaymentAttemptId(), now)
		if err != nil {
			return nil, err
		}
		response.Duplicate = true
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return response, nil
	}
	order, err := scanOrder(tx.QueryRow(ctx, orderSelect+` WHERE o.order_id=$1 FOR UPDATE`, request.GetOrderId()))
	if err != nil || order.GetProvider() != request.GetProvider() {
		return nil, commerce.ErrInvalidTransition
	}
	attempt, err := scanPaymentAttempt(tx.QueryRow(ctx, paymentAttemptSelect+` WHERE payment_attempt_id=$1 AND order_id=$2 FOR UPDATE`, request.GetPaymentAttemptId(), request.GetOrderId()))
	if err != nil {
		return nil, commerce.ErrInvalidTransition
	}
	if !validOrderPaymentEvent(order.GetStatus(), request.GetEventType()) {
		return nil, commerce.ErrInvalidTransition
	}
	orderStatus, attemptStatus := orderStatusForEvent(request.GetEventType()), paymentAttemptStatusForEvent(request.GetEventType(), attempt.GetStatus())
	if _, err := tx.Exec(ctx, `UPDATE orders SET status=$1,provider_reference=$2,settled_at=$3,revision=revision+1 WHERE order_id=$4`, orderStatus, request.GetProviderReference(), now, order.GetOrderId()); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE payment_attempts SET status=$1,provider_reference=$2,updated_at=$3,revision=revision+1 WHERE payment_attempt_id=$4`, attemptStatus, request.GetProviderReference(), now, attempt.GetPaymentAttemptId()); err != nil {
		return nil, err
	}
	if request.GetEventType() == cloudv1.PaymentEventType_PAYMENT_EVENT_TYPE_SUCCEEDED {
		if err := applyPaidSubscription(ctx, tx, order, now); err != nil {
			return nil, err
		}
	} else if request.GetEventType() == cloudv1.PaymentEventType_PAYMENT_EVENT_TYPE_REFUNDED || request.GetEventType() == cloudv1.PaymentEventType_PAYMENT_EVENT_TYPE_REVOKED || request.GetEventType() == cloudv1.PaymentEventType_PAYMENT_EVENT_TYPE_CHARGEBACK {
		if _, err := tx.Exec(ctx, `UPDATE subscriptions SET state='suspended',cancel_at_period_end=false,revision=revision+1,updated_at=$1 WHERE account_id=$2`, now, order.GetAccountId()); err != nil {
			return nil, err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE payment_events SET state='applied',applied_at=$1 WHERE provider=$2 AND provider_event_id=$3 AND state='received'`, now, request.GetProvider(), request.GetProviderEventId()); err != nil {
		return nil, err
	}
	if err := insertOperatorAudit(ctx, tx, actorID, "payment."+paymentEventName(request.GetEventType()), "order", order.GetOrderId(), "", "applied", now); err != nil {
		return nil, err
	}
	response, err := paymentAggregate(ctx, tx, request.GetOrderId(), request.GetPaymentAttemptId(), now)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return response, nil
}

// TransitionSubscription 使用 revision CAS 执行运营调整，并在同一事务写 adjustment 与审计。
func (database *Database) TransitionSubscription(ctx context.Context, request *cloudv1.TransitionSubscriptionRequest, actorID string, now time.Time) (*cloudv1.TransitionSubscriptionResponse, error) {
	tx, err := database.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current, err := scanSubscription(tx.QueryRow(ctx, subscriptionSelect+` WHERE s.account_id=$1 FOR UPDATE`, request.GetAccountId()))
	if err != nil || current.GetRevision() != request.GetExpectedRevision() {
		return nil, commerce.ErrCommerceConflict
	}
	state, cancel, ok := transitionSubscriptionState(current.GetState(), request.GetTransition())
	if !ok {
		return nil, commerce.ErrInvalidTransition
	}
	planID, planVersion := current.GetPlanId(), current.GetPlanVersion()
	if request.GetTargetPlanId() != "" || request.GetTargetPlanVersion() != 0 {
		if request.GetTargetPlanId() == "" || request.GetTargetPlanVersion() == 0 {
			return nil, commerce.ErrInvalidTransition
		}
		if _, err := scanPlan(tx.QueryRow(ctx, planSelect+` WHERE p.plan_id=$1 AND p.version=$2 AND p.state='published'`, request.GetTargetPlanId(), request.GetTargetPlanVersion())); err != nil {
			return nil, commerce.ErrInvalidTransition
		}
		planID, planVersion = request.GetTargetPlanId(), request.GetTargetPlanVersion()
	}
	result, err := tx.Exec(ctx, `UPDATE subscriptions SET plan_id=$1,plan_version=$2,state=$3,cancel_at_period_end=$4,revision=revision+1,updated_at=$5 WHERE account_id=$6 AND revision=$7`, planID, planVersion, subscriptionStateName(state), cancel, now, request.GetAccountId(), request.GetExpectedRevision())
	if err != nil {
		return nil, err
	}
	if result.RowsAffected() != 1 {
		return nil, commerce.ErrCommerceConflict
	}
	if _, err := tx.Exec(ctx, `INSERT INTO subscription_adjustments(adjustment_id,account_id,subscription_id,transition,reason,actor_account_id,before_revision,after_revision,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, uuid.NewString(), request.GetAccountId(), current.GetSubscriptionId(), subscriptionTransitionName(request.GetTransition()), request.GetReason(), actorID, current.GetRevision(), current.GetRevision()+1, now); err != nil {
		return nil, err
	}
	if err := insertOperatorAudit(ctx, tx, actorID, "subscription."+subscriptionTransitionName(request.GetTransition()), "subscription", current.GetSubscriptionId(), request.GetReason(), "applied", now); err != nil {
		return nil, err
	}
	subscription, err := scanSubscription(tx.QueryRow(ctx, subscriptionSelect+` WHERE s.account_id=$1`, request.GetAccountId()))
	if err != nil {
		return nil, err
	}
	entitlement, err := effectiveEntitlement(ctx, tx, request.GetAccountId(), now)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &cloudv1.TransitionSubscriptionResponse{Subscription: subscription, Entitlement: entitlement}, nil
}

// GetAccountCommerce 返回订阅、Entitlement、订单、支付尝试和当前用量。
func (database *Database) GetAccountCommerce(ctx context.Context, accountID string, now time.Time) (*cloudv1.GetAccountCommerceResponse, error) {
	subscription, err := scanSubscription(database.pool.QueryRow(ctx, subscriptionSelect+` WHERE s.account_id=$1`, accountID))
	if err != nil {
		return nil, err
	}
	entitlement, err := database.EffectiveEntitlement(ctx, accountID, now)
	if err != nil {
		return nil, err
	}
	orders, err := database.listOrders(ctx, ` WHERE o.account_id=$1 ORDER BY o.created_at DESC`, accountID)
	if err != nil {
		return nil, err
	}
	attempts, err := database.listPaymentAttempts(ctx, ` WHERE account_id=$1 ORDER BY created_at DESC`, accountID)
	if err != nil {
		return nil, err
	}
	usage, err := database.usagePeriod(ctx, accountID, subscription, entitlement.GetCapability().GetRelayMaxBytesPerPeriod(), now)
	if err != nil {
		return nil, err
	}
	return &cloudv1.GetAccountCommerceResponse{Subscription: subscription, Entitlement: entitlement, Orders: orders, PaymentAttempts: attempts, Usage: usage}, nil
}

// EffectiveEntitlement 由账号、Subscription、Plan、override 和当前用量即时计算。
func (database *Database) EffectiveEntitlement(ctx context.Context, accountID string, now time.Time) (*cloudv1.EffectiveEntitlement, error) {
	return effectiveEntitlement(ctx, database.pool, accountID, now)
}

const orderSelect = `SELECT o.order_id::text,o.account_id::text,o.plan_id,o.plan_version,o.status,o.currency,o.amount_minor,o.provider,o.provider_reference,o.idempotency_key,o.requested_transition,o.revision,o.created_at,o.settled_at FROM orders o`
const paymentAttemptSelect = `SELECT payment_attempt_id::text,order_id::text,account_id::text,provider,provider_reference,status,revision,created_at,updated_at FROM payment_attempts`
const subscriptionSelect = `SELECT s.subscription_id::text,s.account_id::text,s.plan_id,s.plan_version,coalesce(s.source_order_id::text,''),s.state,s.cancel_at_period_end,s.revision,s.period_start,s.period_end,s.updated_at FROM subscriptions s`

func scanPlan(row rowScanner) (*cloudv1.PlanDefinition, error) {
	var planID, name, description, state, monthlyCurrency, yearlyCurrency string
	var version, monthlyMinor, yearlyMinor, p2pConcurrency, relayConcurrency, periodBytes, leaseBytes, rateBytes, daemonLimit, revision uint64
	var billingDays uint32
	var p2p, relay bool
	var regions []string
	var createdAt time.Time
	var publishedAt *time.Time
	if err := row.Scan(&planID, &version, &name, &description, &state, &billingDays, &monthlyCurrency, &monthlyMinor, &yearlyCurrency, &yearlyMinor, &p2p, &p2pConcurrency, &relay, &relayConcurrency, &periodBytes, &leaseBytes, &rateBytes, &daemonLimit, &regions, &revision, &createdAt, &publishedAt); err != nil {
		return nil, err
	}
	result := &cloudv1.PlanDefinition{PlanId: planID, Version: version, Name: name, Description: description, State: parsePlanState(state), BillingPeriodDays: billingDays, MonthlyPrice: &cloudv1.Money{Currency: monthlyCurrency, MinorUnits: int64(monthlyMinor)}, YearlyPrice: &cloudv1.Money{Currency: yearlyCurrency, MinorUnits: int64(yearlyMinor)}, Capability: &cloudv1.CloudCapability{ManagedP2PEnabled: p2p, ManagedP2PMaxConcurrency: uint32(p2pConcurrency), RelayEnabled: relay, RelayMaxConcurrency: uint32(relayConcurrency), RelayMaxBytesPerPeriod: periodBytes, RelayMaxBytesPerLease: leaseBytes, RelayMaxRateBytesPerSecond: rateBytes, CloudDaemonLimit: uint32(daemonLimit), AllowedRegions: regions}, Revision: revision, CreatedAt: timestamppb.New(createdAt)}
	if publishedAt != nil {
		result.PublishedAt = timestamppb.New(*publishedAt)
	}
	return result, nil
}

func scanOrder(row rowScanner) (*cloudv1.OrderProjection, error) {
	var id, accountID, planID, status, currency, provider, providerRef, idempotency, transition string
	var planVersion, revision uint64
	var amount int64
	var createdAt time.Time
	var settledAt *time.Time
	if err := row.Scan(&id, &accountID, &planID, &planVersion, &status, &currency, &amount, &provider, &providerRef, &idempotency, &transition, &revision, &createdAt, &settledAt); err != nil {
		return nil, err
	}
	result := &cloudv1.OrderProjection{OrderId: id, AccountId: accountID, PlanId: planID, PlanVersion: planVersion, Status: parseOrderStatus(status), Amount: &cloudv1.Money{Currency: currency, MinorUnits: amount}, Provider: provider, ProviderReference: providerRef, IdempotencyKey: idempotency, RequestedTransition: parseSubscriptionTransition(transition), Revision: revision, CreatedAt: timestamppb.New(createdAt)}
	if settledAt != nil {
		result.SettledAt = timestamppb.New(*settledAt)
	}
	return result, nil
}

func scanPaymentAttempt(row rowScanner) (*cloudv1.PaymentAttemptProjection, error) {
	var id, orderID, accountID, provider, providerRef, status string
	var revision uint64
	var createdAt, updatedAt time.Time
	if err := row.Scan(&id, &orderID, &accountID, &provider, &providerRef, &status, &revision, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	return &cloudv1.PaymentAttemptProjection{PaymentAttemptId: id, OrderId: orderID, AccountId: accountID, Provider: provider, ProviderReference: providerRef, Status: parsePaymentAttemptStatus(status), Revision: revision, CreatedAt: timestamppb.New(createdAt), UpdatedAt: timestamppb.New(updatedAt)}, nil
}

func scanSubscription(row rowScanner) (*cloudv1.SubscriptionProjection, error) {
	var id, accountID, planID, sourceOrderID, state string
	var planVersion, revision uint64
	var cancel bool
	var start, end, updated time.Time
	if err := row.Scan(&id, &accountID, &planID, &planVersion, &sourceOrderID, &state, &cancel, &revision, &start, &end, &updated); err != nil {
		return nil, err
	}
	return &cloudv1.SubscriptionProjection{SubscriptionId: id, AccountId: accountID, PlanId: planID, PlanVersion: planVersion, SourceOrderId: sourceOrderID, State: parseSubscriptionState(state), CancelAtPeriodEnd: cancel, Revision: revision, PeriodStart: timestamppb.New(start), PeriodEnd: timestamppb.New(end), UpdatedAt: timestamppb.New(updated)}, nil
}

type queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func effectiveEntitlement(ctx context.Context, q queryer, accountID string, now time.Time) (*cloudv1.EffectiveEntitlement, error) {
	var accountState string
	if err := q.QueryRow(ctx, `SELECT state FROM accounts WHERE account_id=$1`, accountID).Scan(&accountState); err != nil {
		return nil, err
	}
	subscription, err := scanSubscription(q.QueryRow(ctx, subscriptionSelect+` WHERE s.account_id=$1`, accountID))
	if err != nil {
		return nil, err
	}
	plan, err := scanPlan(q.QueryRow(ctx, planSelect+` WHERE p.plan_id=$1 AND p.version=$2`, subscription.GetPlanId(), subscription.GetPlanVersion()))
	if err != nil {
		return nil, err
	}
	used, held := uint64(0), uint64(0)
	_ = q.QueryRow(ctx, `SELECT committed_ingress_bytes+committed_egress_bytes+recovery_bytes,held_bytes FROM usage_periods WHERE account_id=$1 AND subscription_id=$2 AND period_start=$3 AND period_end=$4`, accountID, subscription.GetSubscriptionId(), subscription.GetPeriodStart().AsTime(), subscription.GetPeriodEnd().AsTime()).Scan(&used, &held)
	state := cloudv1.EntitlementState_ENTITLEMENT_STATE_ACTIVE
	if accountState != "active" || subscription.GetState() == cloudv1.SubscriptionState_SUBSCRIPTION_STATE_SUSPENDED {
		state = cloudv1.EntitlementState_ENTITLEMENT_STATE_SUSPENDED
	}
	if !now.Before(subscription.GetPeriodEnd().AsTime()) || subscription.GetState() == cloudv1.SubscriptionState_SUBSCRIPTION_STATE_EXPIRED || subscription.GetState() == cloudv1.SubscriptionState_SUBSCRIPTION_STATE_CANCELED {
		state = cloudv1.EntitlementState_ENTITLEMENT_STATE_EXPIRED
	}
	quota := plan.GetCapability().GetRelayMaxBytesPerPeriod()
	remaining := uint64(0)
	if used < quota && held < quota-used {
		remaining = quota - used - held
	}
	return &cloudv1.EffectiveEntitlement{AccountId: accountID, State: state, PlanId: plan.GetPlanId(), PlanVersion: plan.GetVersion(), SubscriptionId: subscription.GetSubscriptionId(), Capability: proto.Clone(plan.GetCapability()).(*cloudv1.CloudCapability), RelayUsedBytes: used, RelayRemainingBytes: remaining, EffectiveFrom: subscription.GetPeriodStart(), EffectiveUntil: subscription.GetPeriodEnd(), ComputedAt: timestamppb.New(now)}, nil
}

func (database *Database) listOrders(ctx context.Context, suffix string, args ...any) ([]*cloudv1.OrderProjection, error) {
	rows, err := database.pool.Query(ctx, orderSelect+suffix, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]*cloudv1.OrderProjection, 0)
	for rows.Next() {
		value, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}
func (database *Database) listPaymentAttempts(ctx context.Context, suffix string, args ...any) ([]*cloudv1.PaymentAttemptProjection, error) {
	rows, err := database.pool.Query(ctx, paymentAttemptSelect+suffix, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]*cloudv1.PaymentAttemptProjection, 0)
	for rows.Next() {
		value, err := scanPaymentAttempt(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

func (database *Database) usagePeriod(ctx context.Context, accountID string, subscription *cloudv1.SubscriptionProjection, quota uint64, now time.Time) (*cloudv1.UsagePeriodProjection, error) {
	var start, end time.Time
	var ingress, egress, recovery, held, revision uint64
	err := database.pool.QueryRow(ctx, `SELECT period_start,period_end,committed_ingress_bytes,committed_egress_bytes,recovery_bytes,held_bytes,revision FROM usage_periods WHERE account_id=$1 AND subscription_id=$2 AND period_start=$3 AND period_end=$4`, accountID, subscription.GetSubscriptionId(), subscription.GetPeriodStart().AsTime(), subscription.GetPeriodEnd().AsTime()).Scan(&start, &end, &ingress, &egress, &recovery, &held, &revision)
	if errors.Is(err, pgx.ErrNoRows) {
		start = subscription.GetPeriodStart().AsTime()
		end = subscription.GetPeriodEnd().AsTime()
		err = nil
	}
	if err != nil {
		return nil, err
	}
	total := ingress + egress + recovery
	remaining := uint64(0)
	if total < quota && held < quota-total {
		remaining = quota - total - held
	}
	return &cloudv1.UsagePeriodProjection{AccountId: accountID, PeriodStart: timestamppb.New(start), PeriodEnd: timestamppb.New(end), RelayIngressBytes: ingress, RelayEgressBytes: egress, RelayTotalBytes: total, QuotaBytes: quota, RemainingBytes: remaining, Revision: revision}, nil
}

func paymentAggregate(ctx context.Context, tx pgx.Tx, orderID, attemptID string, now time.Time) (*cloudv1.ApplyPaymentEventResponse, error) {
	order, err := scanOrder(tx.QueryRow(ctx, orderSelect+` WHERE o.order_id=$1`, orderID))
	if err != nil {
		return nil, err
	}
	attempt, err := scanPaymentAttempt(tx.QueryRow(ctx, paymentAttemptSelect+` WHERE payment_attempt_id=$1`, attemptID))
	if err != nil {
		return nil, err
	}
	subscription, err := scanSubscription(tx.QueryRow(ctx, subscriptionSelect+` WHERE s.account_id=$1`, order.GetAccountId()))
	if err != nil {
		return nil, err
	}
	entitlement, err := effectiveEntitlement(ctx, tx, order.GetAccountId(), now)
	if err != nil {
		return nil, err
	}
	return &cloudv1.ApplyPaymentEventResponse{Order: order, PaymentAttempt: attempt, Subscription: subscription, Entitlement: entitlement}, nil
}

func applyPaidSubscription(ctx context.Context, tx pgx.Tx, order *cloudv1.OrderProjection, now time.Time) error {
	var periodDays uint32
	if err := tx.QueryRow(ctx, `SELECT billing_period_days FROM plans WHERE plan_id=$1 AND version=$2`, order.GetPlanId(), order.GetPlanVersion()).Scan(&periodDays); err != nil {
		return err
	}
	periodEnd := now.AddDate(0, 0, int(periodDays))
	_, err := tx.Exec(ctx, `UPDATE subscriptions SET plan_id=$1,plan_version=$2,source_order_id=$3,state='active',cancel_at_period_end=false,period_start=$4,period_end=$5,revision=revision+1,updated_at=$4 WHERE account_id=$6`, order.GetPlanId(), order.GetPlanVersion(), order.GetOrderId(), now, periodEnd, order.GetAccountId())
	return err
}

func validatePaidTransition(ctx context.Context, tx pgx.Tx, request *cloudv1.CreateOrderRequest) error {
	sub, err := scanSubscription(tx.QueryRow(ctx, subscriptionSelect+` WHERE s.account_id=$1`, request.GetAccountId()))
	if err != nil {
		return err
	}
	switch request.GetRequestedTransition() {
	case cloudv1.SubscriptionTransition_SUBSCRIPTION_TRANSITION_RENEW:
		if sub.GetPlanId() != request.GetPlanId() {
			return commerce.ErrInvalidTransition
		}
	case cloudv1.SubscriptionTransition_SUBSCRIPTION_TRANSITION_UPGRADE, cloudv1.SubscriptionTransition_SUBSCRIPTION_TRANSITION_DOWNGRADE:
		if sub.GetPlanId() == request.GetPlanId() && sub.GetPlanVersion() == request.GetPlanVersion() {
			return commerce.ErrInvalidTransition
		}
	}
	return nil
}

func findOrderByIdempotency(ctx context.Context, tx pgx.Tx, accountID, key string) (*cloudv1.OrderProjection, *cloudv1.PaymentAttemptProjection, error) {
	order, err := scanOrder(tx.QueryRow(ctx, orderSelect+` WHERE o.account_id=$1 AND o.idempotency_key=$2`, accountID, key))
	if err != nil {
		return nil, nil, err
	}
	attempt, err := scanPaymentAttempt(tx.QueryRow(ctx, paymentAttemptSelect+` WHERE order_id=$1 ORDER BY created_at LIMIT 1`, order.GetOrderId()))
	return order, attempt, err
}
func insertPlanPrice(ctx context.Context, tx pgx.Tx, planID string, version uint64, cycle string, money *cloudv1.Money) error {
	if money == nil || len(strings.TrimSpace(money.GetCurrency())) != 3 || money.GetMinorUnits() < 0 {
		return commerce.ErrInvalidTransition
	}
	_, err := tx.Exec(ctx, `INSERT INTO plan_prices(plan_id,plan_version,billing_cycle,currency,minor_units) VALUES($1,$2,$3,upper($4),$5)`, planID, version, cycle, money.GetCurrency(), money.GetMinorUnits())
	return err
}
func insertOperatorAudit(ctx context.Context, tx pgx.Tx, actorID, action, resourceType, resourceID, reason, result string, now time.Time) error {
	_, err := tx.Exec(ctx, `INSERT INTO operator_audit_events(audit_id,actor_account_id,action,resource_type,resource_id,reason,result,correlation_id,occurred_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, uuid.NewString(), actorID, action, resourceType, resourceID, reason, result, uuid.NewString(), now)
	return err
}
func equalDigest(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var value byte
	for i := range a {
		value |= a[i] ^ b[i]
	}
	return value == 0
}

func parsePlanState(value string) cloudv1.PlanState {
	switch value {
	case "draft":
		return cloudv1.PlanState_PLAN_STATE_DRAFT
	case "retired":
		return cloudv1.PlanState_PLAN_STATE_RETIRED
	default:
		return cloudv1.PlanState_PLAN_STATE_PUBLISHED
	}
}
func parseOrderStatus(value string) cloudv1.OrderStatus {
	switch value {
	case "pending":
		return cloudv1.OrderStatus_ORDER_STATUS_PENDING
	case "paid":
		return cloudv1.OrderStatus_ORDER_STATUS_PAID
	case "payment_failed":
		return cloudv1.OrderStatus_ORDER_STATUS_PAYMENT_FAILED
	case "refunded":
		return cloudv1.OrderStatus_ORDER_STATUS_REFUNDED
	default:
		return cloudv1.OrderStatus_ORDER_STATUS_REVOKED
	}
}
func parsePaymentAttemptStatus(value string) cloudv1.PaymentAttemptStatus {
	switch value {
	case "pending":
		return cloudv1.PaymentAttemptStatus_PAYMENT_ATTEMPT_STATUS_PENDING
	case "succeeded":
		return cloudv1.PaymentAttemptStatus_PAYMENT_ATTEMPT_STATUS_SUCCEEDED
	default:
		return cloudv1.PaymentAttemptStatus_PAYMENT_ATTEMPT_STATUS_FAILED
	}
}
func parseSubscriptionState(value string) cloudv1.SubscriptionState {
	switch value {
	case "active":
		return cloudv1.SubscriptionState_SUBSCRIPTION_STATE_ACTIVE
	case "cancel_at_period_end":
		return cloudv1.SubscriptionState_SUBSCRIPTION_STATE_CANCEL_AT_PERIOD_END
	case "canceled":
		return cloudv1.SubscriptionState_SUBSCRIPTION_STATE_CANCELED
	case "suspended":
		return cloudv1.SubscriptionState_SUBSCRIPTION_STATE_SUSPENDED
	case "expired":
		return cloudv1.SubscriptionState_SUBSCRIPTION_STATE_EXPIRED
	default:
		return cloudv1.SubscriptionState_SUBSCRIPTION_STATE_PAST_DUE
	}
}
func subscriptionStateName(value cloudv1.SubscriptionState) string {
	switch value {
	case cloudv1.SubscriptionState_SUBSCRIPTION_STATE_CANCEL_AT_PERIOD_END:
		return "cancel_at_period_end"
	case cloudv1.SubscriptionState_SUBSCRIPTION_STATE_CANCELED:
		return "canceled"
	case cloudv1.SubscriptionState_SUBSCRIPTION_STATE_SUSPENDED:
		return "suspended"
	case cloudv1.SubscriptionState_SUBSCRIPTION_STATE_EXPIRED:
		return "expired"
	case cloudv1.SubscriptionState_SUBSCRIPTION_STATE_PAST_DUE:
		return "past_due"
	default:
		return "active"
	}
}
func subscriptionTransitionName(value cloudv1.SubscriptionTransition) string {
	switch value {
	case cloudv1.SubscriptionTransition_SUBSCRIPTION_TRANSITION_RENEW:
		return "renew"
	case cloudv1.SubscriptionTransition_SUBSCRIPTION_TRANSITION_UPGRADE:
		return "upgrade"
	case cloudv1.SubscriptionTransition_SUBSCRIPTION_TRANSITION_DOWNGRADE:
		return "downgrade"
	case cloudv1.SubscriptionTransition_SUBSCRIPTION_TRANSITION_CANCEL_AT_PERIOD_END:
		return "cancel_at_period_end"
	case cloudv1.SubscriptionTransition_SUBSCRIPTION_TRANSITION_RESUME:
		return "resume"
	case cloudv1.SubscriptionTransition_SUBSCRIPTION_TRANSITION_SUSPEND:
		return "suspend"
	case cloudv1.SubscriptionTransition_SUBSCRIPTION_TRANSITION_RESTORE:
		return "restore"
	case cloudv1.SubscriptionTransition_SUBSCRIPTION_TRANSITION_EXPIRE:
		return "expire"
	case cloudv1.SubscriptionTransition_SUBSCRIPTION_TRANSITION_REFUND:
		return "refund"
	case cloudv1.SubscriptionTransition_SUBSCRIPTION_TRANSITION_REVOKE:
		return "revoke"
	default:
		return "activate"
	}
}
func parseSubscriptionTransition(value string) cloudv1.SubscriptionTransition {
	switch value {
	case "renew":
		return cloudv1.SubscriptionTransition_SUBSCRIPTION_TRANSITION_RENEW
	case "upgrade":
		return cloudv1.SubscriptionTransition_SUBSCRIPTION_TRANSITION_UPGRADE
	case "downgrade":
		return cloudv1.SubscriptionTransition_SUBSCRIPTION_TRANSITION_DOWNGRADE
	default:
		return cloudv1.SubscriptionTransition_SUBSCRIPTION_TRANSITION_ACTIVATE
	}
}
func paymentEventName(value cloudv1.PaymentEventType) string {
	switch value {
	case cloudv1.PaymentEventType_PAYMENT_EVENT_TYPE_FAILED:
		return "failed"
	case cloudv1.PaymentEventType_PAYMENT_EVENT_TYPE_REFUNDED:
		return "refunded"
	case cloudv1.PaymentEventType_PAYMENT_EVENT_TYPE_REVOKED:
		return "revoked"
	case cloudv1.PaymentEventType_PAYMENT_EVENT_TYPE_CHARGEBACK:
		return "chargeback"
	default:
		return "succeeded"
	}
}
func orderStatusForEvent(value cloudv1.PaymentEventType) string {
	switch value {
	case cloudv1.PaymentEventType_PAYMENT_EVENT_TYPE_FAILED:
		return "payment_failed"
	case cloudv1.PaymentEventType_PAYMENT_EVENT_TYPE_REFUNDED:
		return "refunded"
	case cloudv1.PaymentEventType_PAYMENT_EVENT_TYPE_REVOKED, cloudv1.PaymentEventType_PAYMENT_EVENT_TYPE_CHARGEBACK:
		return "revoked"
	default:
		return "paid"
	}
}
func paymentAttemptStatusForEvent(value cloudv1.PaymentEventType, current cloudv1.PaymentAttemptStatus) string {
	switch value {
	case cloudv1.PaymentEventType_PAYMENT_EVENT_TYPE_SUCCEEDED:
		return "succeeded"
	case cloudv1.PaymentEventType_PAYMENT_EVENT_TYPE_FAILED:
		return "failed"
	case cloudv1.PaymentEventType_PAYMENT_EVENT_TYPE_REFUNDED, cloudv1.PaymentEventType_PAYMENT_EVENT_TYPE_REVOKED, cloudv1.PaymentEventType_PAYMENT_EVENT_TYPE_CHARGEBACK:
		if current == cloudv1.PaymentAttemptStatus_PAYMENT_ATTEMPT_STATUS_SUCCEEDED {
			return "succeeded"
		}
	}
	return "failed"
}
func validOrderPaymentEvent(status cloudv1.OrderStatus, event cloudv1.PaymentEventType) bool {
	switch event {
	case cloudv1.PaymentEventType_PAYMENT_EVENT_TYPE_SUCCEEDED:
		return status == cloudv1.OrderStatus_ORDER_STATUS_PENDING || status == cloudv1.OrderStatus_ORDER_STATUS_PAYMENT_FAILED
	case cloudv1.PaymentEventType_PAYMENT_EVENT_TYPE_FAILED:
		return status == cloudv1.OrderStatus_ORDER_STATUS_PENDING
	case cloudv1.PaymentEventType_PAYMENT_EVENT_TYPE_REFUNDED, cloudv1.PaymentEventType_PAYMENT_EVENT_TYPE_REVOKED, cloudv1.PaymentEventType_PAYMENT_EVENT_TYPE_CHARGEBACK:
		return status == cloudv1.OrderStatus_ORDER_STATUS_PAID
	default:
		return false
	}
}
func transitionSubscriptionState(current cloudv1.SubscriptionState, transition cloudv1.SubscriptionTransition) (cloudv1.SubscriptionState, bool, bool) {
	switch transition {
	case cloudv1.SubscriptionTransition_SUBSCRIPTION_TRANSITION_CANCEL_AT_PERIOD_END:
		if current != cloudv1.SubscriptionState_SUBSCRIPTION_STATE_ACTIVE {
			return 0, false, false
		}
		return cloudv1.SubscriptionState_SUBSCRIPTION_STATE_CANCEL_AT_PERIOD_END, true, true
	case cloudv1.SubscriptionTransition_SUBSCRIPTION_TRANSITION_RESUME:
		if current != cloudv1.SubscriptionState_SUBSCRIPTION_STATE_CANCEL_AT_PERIOD_END {
			return 0, false, false
		}
		return cloudv1.SubscriptionState_SUBSCRIPTION_STATE_ACTIVE, false, true
	case cloudv1.SubscriptionTransition_SUBSCRIPTION_TRANSITION_SUSPEND:
		if current == cloudv1.SubscriptionState_SUBSCRIPTION_STATE_EXPIRED {
			return 0, false, false
		}
		return cloudv1.SubscriptionState_SUBSCRIPTION_STATE_SUSPENDED, false, true
	case cloudv1.SubscriptionTransition_SUBSCRIPTION_TRANSITION_RESTORE:
		if current != cloudv1.SubscriptionState_SUBSCRIPTION_STATE_SUSPENDED {
			return 0, false, false
		}
		return cloudv1.SubscriptionState_SUBSCRIPTION_STATE_ACTIVE, false, true
	case cloudv1.SubscriptionTransition_SUBSCRIPTION_TRANSITION_EXPIRE, cloudv1.SubscriptionTransition_SUBSCRIPTION_TRANSITION_REVOKE:
		return cloudv1.SubscriptionState_SUBSCRIPTION_STATE_EXPIRED, false, true
	default:
		return 0, false, false
	}
}
