package account_test

import (
	"context"
	"testing"
	"time"

	"github.com/lozzow/termx/web-control/internal/account"
	"github.com/lozzow/termx/web-control/internal/store"
)

func TestMockPaymentProviderActivatesFailsAndExpiresSubscription(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := store.OpenSQLite(ctx, "file:termx-payment-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	payments := account.NewMockPaymentProvider()
	svc := account.NewService(account.Config{
		DB:       db,
		Clock:    fixedClock(time.Date(2026, 5, 3, 3, 12, 0, 0, time.UTC)),
		Tokens:   account.NewHMACTokenIssuer([]byte("slice-2-payment-secret")),
		Payments: payments,
	})

	registered, err := svc.Register(ctx, account.RegisterInput{Email: "carol@example.com", Password: "valid password"})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	order, err := svc.CreateSubscriptionOrder(ctx, registered.User.ID, account.PlanPro)
	if err != nil {
		t.Fatalf("create subscription order: %v", err)
	}
	if order.Status != account.PaymentPending {
		t.Fatalf("order status = %q", order.Status)
	}
	if order.ID == order.ProviderOrderID {
		t.Fatal("local payment order id should be distinct from provider order id")
	}

	if err := payments.SimulateSuccess(ctx, order.ProviderOrderID); err != nil {
		t.Fatalf("simulate success: %v", err)
	}
	active, err := svc.SyncPayment(ctx, order.ID)
	if err != nil {
		t.Fatalf("sync payment success: %v", err)
	}
	if active.Subscription.Status != account.SubscriptionActive {
		t.Fatalf("subscription status = %q", active.Subscription.Status)
	}
	if !active.Plan.AllowRelay {
		t.Fatal("paid plan should allow relay")
	}

	failedOrder, err := svc.CreateSubscriptionOrder(ctx, registered.User.ID, account.PlanPro)
	if err != nil {
		t.Fatalf("create failed order: %v", err)
	}
	if err := payments.SimulateFailure(ctx, failedOrder.ProviderOrderID); err != nil {
		t.Fatalf("simulate failure: %v", err)
	}
	failed, err := svc.SyncPayment(ctx, failedOrder.ID)
	if err != nil {
		t.Fatalf("sync payment failure: %v", err)
	}
	if failed.Subscription.Status != account.SubscriptionPastDue {
		t.Fatalf("failed subscription status = %q", failed.Subscription.Status)
	}

	if err := payments.SimulateExpiry(ctx, order.ProviderOrderID); err != nil {
		t.Fatalf("simulate expiry: %v", err)
	}
	expired, err := svc.SyncPayment(ctx, order.ID)
	if err != nil {
		t.Fatalf("sync payment expiry: %v", err)
	}
	if expired.Subscription.Status != account.SubscriptionExpired {
		t.Fatalf("expired subscription status = %q", expired.Subscription.Status)
	}
	if expired.Plan.AllowRelay {
		t.Fatal("expired subscription should fall back to non-relay policy")
	}
}

func TestServiceRequiresExplicitPaymentProviderForPaidOrders(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := store.OpenSQLite(ctx, "file:termx-payment-provider-required-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	svc := account.NewService(account.Config{
		DB:     db,
		Clock:  fixedClock(time.Date(2026, 5, 3, 3, 13, 0, 0, time.UTC)),
		Tokens: account.NewHMACTokenIssuer([]byte("slice-2-provider-required-secret")),
	})
	registered, err := svc.Register(ctx, account.RegisterInput{Email: "provider-required@example.com", Password: "valid password"})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := svc.CreateSubscriptionOrder(ctx, registered.User.ID, account.PlanPro); err == nil {
		t.Fatal("paid order without explicit payment provider succeeded")
	}
}

func TestSyncPaymentRejectsProviderOrderMismatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := store.OpenSQLite(ctx, "file:termx-payment-mismatch-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	payments := account.NewMockPaymentProvider()
	svc := account.NewService(account.Config{
		DB:       db,
		Clock:    fixedClock(time.Date(2026, 5, 3, 3, 14, 0, 0, time.UTC)),
		Tokens:   account.NewHMACTokenIssuer([]byte("slice-2-mismatch-secret")),
		Payments: payments,
	})
	registered, err := svc.Register(ctx, account.RegisterInput{Email: "mismatch@example.com", Password: "valid password"})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	order, err := svc.CreateSubscriptionOrder(ctx, registered.User.ID, account.PlanPro)
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	if err := payments.OverrideOrder(ctx, order.ProviderOrderID, account.ProviderOrder{
		ID:     order.ProviderOrderID,
		UserID: "other_user",
		PlanID: account.PlanPro,
		Status: account.PaymentPaid,
	}); err != nil {
		t.Fatalf("override provider order: %v", err)
	}
	if _, err := svc.SyncPayment(ctx, order.ID); err == nil {
		t.Fatal("mismatched provider order was accepted")
	}
}

func TestSyncPaymentRejectsProviderOrderIDMismatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := store.OpenSQLite(ctx, "file:termx-payment-id-mismatch-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	payments := account.NewMockPaymentProvider()
	svc := account.NewService(account.Config{
		DB:       db,
		Clock:    fixedClock(time.Date(2026, 5, 3, 3, 17, 0, 0, time.UTC)),
		Tokens:   account.NewHMACTokenIssuer([]byte("slice-2-id-mismatch-secret")),
		Payments: payments,
	})
	registered, err := svc.Register(ctx, account.RegisterInput{Email: "id-mismatch@example.com", Password: "valid password"})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	order, err := svc.CreateSubscriptionOrder(ctx, registered.User.ID, account.PlanPro)
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	if err := payments.OverrideOrder(ctx, order.ProviderOrderID, account.ProviderOrder{
		ID:     "different_provider_order",
		UserID: registered.User.ID,
		PlanID: account.PlanPro,
		Status: account.PaymentPaid,
	}); err != nil {
		t.Fatalf("override provider order: %v", err)
	}
	if _, err := svc.SyncPayment(ctx, order.ID); err == nil {
		t.Fatal("mismatched provider order id was accepted")
	}
}

func TestSyncPaymentPendingDoesNotCreateSubscription(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := store.OpenSQLite(ctx, "file:termx-payment-pending-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	payments := account.NewMockPaymentProvider()
	svc := account.NewService(account.Config{
		DB:       db,
		Clock:    fixedClock(time.Date(2026, 5, 3, 3, 19, 0, 0, time.UTC)),
		Tokens:   account.NewHMACTokenIssuer([]byte("slice-2-pending-secret")),
		Payments: payments,
	})
	registered, err := svc.Register(ctx, account.RegisterInput{Email: "pending@example.com", Password: "valid password"})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	order, err := svc.CreateSubscriptionOrder(ctx, registered.User.ID, account.PlanPro)
	if err != nil {
		t.Fatalf("create order: %v", err)
	}

	synced, err := svc.SyncPayment(ctx, order.ID)
	if err != nil {
		t.Fatalf("sync pending payment: %v", err)
	}
	if synced.Subscription.ID != "" {
		t.Fatalf("pending payment created subscription: %+v", synced.Subscription)
	}
	var subCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscriptions WHERE user_id = ?`, registered.User.ID).Scan(&subCount); err != nil {
		t.Fatalf("count subscriptions: %v", err)
	}
	if subCount != 0 {
		t.Fatalf("pending payment left %d subscriptions", subCount)
	}
}

func TestSyncPaymentRejectsUnknownProviderStatus(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := store.OpenSQLite(ctx, "file:termx-payment-unknown-status-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	payments := account.NewMockPaymentProvider()
	svc := account.NewService(account.Config{
		DB:       db,
		Clock:    fixedClock(time.Date(2026, 5, 3, 3, 21, 0, 0, time.UTC)),
		Tokens:   account.NewHMACTokenIssuer([]byte("slice-2-unknown-status-secret")),
		Payments: payments,
	})
	registered, err := svc.Register(ctx, account.RegisterInput{Email: "unknown-status@example.com", Password: "valid password"})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	order, err := svc.CreateSubscriptionOrder(ctx, registered.User.ID, account.PlanPro)
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	if err := payments.OverrideOrder(ctx, order.ProviderOrderID, account.ProviderOrder{
		ID:     order.ProviderOrderID,
		UserID: registered.User.ID,
		PlanID: account.PlanPro,
		Status: "mystery",
	}); err != nil {
		t.Fatalf("override provider order: %v", err)
	}
	if _, err := svc.SyncPayment(ctx, order.ID); err == nil {
		t.Fatal("unknown provider status was accepted")
	}
	var subCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscriptions WHERE user_id = ?`, registered.User.ID).Scan(&subCount); err != nil {
		t.Fatalf("count subscriptions: %v", err)
	}
	if subCount != 0 {
		t.Fatalf("unknown provider status left %d subscriptions", subCount)
	}
}

func TestSyncPaymentRollsBackOrderStatusWhenSubscriptionInsertFails(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := store.OpenSQLite(ctx, "file:termx-payment-atomic-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	payments := account.NewMockPaymentProvider()
	svc := account.NewService(account.Config{
		DB:       db,
		Clock:    fixedClock(time.Date(2026, 5, 3, 3, 22, 0, 0, time.UTC)),
		Tokens:   account.NewHMACTokenIssuer([]byte("slice-2-atomic-secret")),
		Payments: payments,
	})
	registered, err := svc.Register(ctx, account.RegisterInput{Email: "atomic@example.com", Password: "valid password"})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	order, err := svc.CreateSubscriptionOrder(ctx, registered.User.ID, account.PlanPro)
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	if err := payments.SimulateSuccess(ctx, order.ProviderOrderID); err != nil {
		t.Fatalf("simulate success: %v", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TRIGGER fail_subscription_insert BEFORE INSERT ON subscriptions BEGIN SELECT RAISE(ABORT, 'forced subscription insert failure'); END`); err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}

	if _, err := svc.SyncPayment(ctx, order.ID); err == nil {
		t.Fatal("sync payment succeeded despite subscription insert failure")
	}
	var status string
	if err := db.QueryRowContext(ctx, `SELECT status FROM payment_orders WHERE id = ?`, order.ID).Scan(&status); err != nil {
		t.Fatalf("load order status: %v", err)
	}
	if status != account.PaymentPending {
		t.Fatalf("payment order status = %q after failed sync, want pending", status)
	}
}
