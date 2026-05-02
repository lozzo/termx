package relay_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/web-control/internal/account"
	"github.com/lozzow/termx/web-control/internal/relay"
	"github.com/lozzow/termx/web-control/internal/store"
)

func TestRelayLeasePolicyDeniesFreeAndIssuesPaidManagedLease(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openRelayTestDB(t, "file:termx-relay-lease-policy?mode=memory&cache=shared")
	clock := fixedClock(time.Date(2026, 5, 3, 6, 31, 0, 0, time.UTC))
	svc := relay.NewService(relay.Config{DB: db, Clock: clock})
	seedRelayHub(t, ctx, db, "hub_1")
	seedRelayUserMachine(t, ctx, db, "usr_free", "mach_free")
	seedRelayUserMachine(t, ctx, db, "usr_paid", "mach_paid")
	seedRelaySubscription(t, ctx, db, "usr_paid", account.PlanPro, account.SubscriptionActive, clock.Now().Add(-time.Hour), clock.Now().Add(time.Hour))

	if _, err := svc.CreateLease(ctx, relay.CreateLeaseInput{
		UserID:    "usr_free",
		MachineID: "mach_free",
		HubID:     "hub_1",
		TTL:       time.Minute,
	}); !errors.Is(err, relay.ErrRelayNotAllowed) {
		t.Fatalf("free relay lease err = %v", err)
	}

	lease, err := svc.CreateLease(ctx, relay.CreateLeaseInput{
		UserID:    "usr_paid",
		MachineID: "mach_paid",
		HubID:     "hub_1",
		TTL:       time.Minute,
	})
	if err != nil {
		t.Fatalf("paid relay lease: %v", err)
	}
	if lease.Path != relay.PathManaged || !lease.AllowRelay || lease.RelayInUse {
		t.Fatalf("lease path/relay state = %+v", lease)
	}
	if lease.RelayBytesRemaining <= 0 {
		t.Fatalf("relay bytes remaining = %d", lease.RelayBytesRemaining)
	}
	if !lease.ExpiresAt.Equal(clock.Now().Add(time.Minute)) {
		t.Fatalf("expires_at = %v", lease.ExpiresAt)
	}
	payload, err := json.Marshal(lease)
	if err != nil {
		t.Fatalf("marshal lease: %v", err)
	}
	if strings.Contains(strings.ToLower(string(payload)), `"path":"relay"`) || strings.Contains(strings.ToLower(string(payload)), "paid_relay") {
		t.Fatalf("lease exposed relay as client path: %s", payload)
	}
	var status string
	if err := db.QueryRowContext(ctx, `SELECT status FROM relay_sessions WHERE id = ?`, lease.ID).Scan(&status); err != nil {
		t.Fatalf("load relay session: %v", err)
	}
	if status != relay.SessionLeased {
		t.Fatalf("relay session status = %q", status)
	}
}

func TestRelayLeaseRejectsWrongOwnerExpiredSubscriptionAndCapsTTL(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openRelayTestDB(t, "file:termx-relay-lease-negative?mode=memory&cache=shared")
	clock := fixedClock(time.Date(2026, 5, 3, 6, 32, 0, 0, time.UTC))
	svc := relay.NewService(relay.Config{DB: db, Clock: clock})
	seedRelayHub(t, ctx, db, "hub_1")
	seedRelayUserMachine(t, ctx, db, "usr_owner", "mach_1")
	seedRelayUserMachine(t, ctx, db, "usr_other", "mach_2")
	seedRelaySubscription(t, ctx, db, "usr_owner", account.PlanPro, account.SubscriptionActive, clock.Now().Add(-2*time.Hour), clock.Now().Add(-time.Hour))

	if _, err := svc.CreateLease(ctx, relay.CreateLeaseInput{
		UserID:    "usr_other",
		MachineID: "mach_1",
		HubID:     "hub_1",
		TTL:       time.Minute,
	}); !errors.Is(err, relay.ErrMachineNotOwned) {
		t.Fatalf("wrong owner lease err = %v", err)
	}
	if _, err := svc.CreateLease(ctx, relay.CreateLeaseInput{
		UserID:    "usr_owner",
		MachineID: "mach_1",
		HubID:     "hub_1",
		TTL:       time.Minute,
	}); !errors.Is(err, relay.ErrRelayNotAllowed) {
		t.Fatalf("expired subscription lease err = %v", err)
	}

	seedRelaySubscription(t, ctx, db, "usr_owner", account.PlanPro, account.SubscriptionActive, clock.Now().Add(-time.Hour), clock.Now().Add(time.Hour))
	lease, err := svc.CreateLease(ctx, relay.CreateLeaseInput{
		UserID:    "usr_owner",
		MachineID: "mach_1",
		HubID:     "hub_1",
		TTL:       24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("ttl capped lease: %v", err)
	}
	if !lease.ExpiresAt.Equal(clock.Now().Add(relay.MaxLeaseTTL)) {
		t.Fatalf("expires_at = %v, want capped %v", lease.ExpiresAt, clock.Now().Add(relay.MaxLeaseTTL))
	}
}

func TestRelayLeaseDoesNotOutliveSubscriptionPeriod(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openRelayTestDB(t, "file:termx-relay-lease-subscription-cap?mode=memory&cache=shared")
	clock := fixedClock(time.Date(2026, 5, 3, 6, 39, 0, 0, time.UTC))
	svc := relay.NewService(relay.Config{DB: db, Clock: clock})
	seedRelayHub(t, ctx, db, "hub_1")
	seedRelayUserMachine(t, ctx, db, "usr_paid", "mach_1")
	periodEnd := clock.Now().Add(time.Minute)
	seedRelaySubscription(t, ctx, db, "usr_paid", account.PlanPro, account.SubscriptionActive, clock.Now().Add(-time.Hour), periodEnd)

	lease, err := svc.CreateLease(ctx, relay.CreateLeaseInput{
		UserID:    "usr_paid",
		MachineID: "mach_1",
		HubID:     "hub_1",
		TTL:       relay.MaxLeaseTTL,
	})
	if err != nil {
		t.Fatalf("lease: %v", err)
	}
	if !lease.ExpiresAt.Equal(periodEnd) {
		t.Fatalf("lease expires_at = %v, want subscription end %v", lease.ExpiresAt, periodEnd)
	}
}

func TestRelayLeaseEnforcesActiveSessionLimitAndMonthlyUsage(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openRelayTestDB(t, "file:termx-relay-lease-limit-usage?mode=memory&cache=shared")
	clock := fixedClock(time.Date(2026, 5, 3, 6, 43, 0, 0, time.UTC))
	svc := relay.NewService(relay.Config{DB: db, Clock: clock})
	seedRelayHub(t, ctx, db, "hub_1")
	seedRelayUserMachine(t, ctx, db, "usr_paid", "mach_1")
	seedRelaySubscription(t, ctx, db, "usr_paid", account.PlanPro, account.SubscriptionActive, clock.Now().Add(-time.Hour), clock.Now().Add(time.Hour))
	if _, err := db.ExecContext(ctx, `
		INSERT INTO relay_usage_monthly(user_id, month, bytes_used)
		VALUES (?, ?, ?)
	`, "usr_paid", "2026-05", int64(1024)); err != nil {
		t.Fatalf("seed usage: %v", err)
	}

	first, err := svc.CreateLease(ctx, relay.CreateLeaseInput{
		UserID:    "usr_paid",
		MachineID: "mach_1",
		HubID:     "hub_1",
		TTL:       time.Minute,
	})
	if err != nil {
		t.Fatalf("first lease: %v", err)
	}
	wantRemaining := int64(5*1024*1024*1024) - 1024
	if first.RelayBytesRemaining != wantRemaining {
		t.Fatalf("remaining = %d, want %d", first.RelayBytesRemaining, wantRemaining)
	}
	if _, err := svc.CreateLease(ctx, relay.CreateLeaseInput{
		UserID:    "usr_paid",
		MachineID: "mach_1",
		HubID:     "hub_1",
		TTL:       time.Minute,
	}); err != nil {
		t.Fatalf("second lease within limit: %v", err)
	}
	if _, err := svc.CreateLease(ctx, relay.CreateLeaseInput{
		UserID:    "usr_paid",
		MachineID: "mach_1",
		HubID:     "hub_1",
		TTL:       time.Minute,
	}); !errors.Is(err, relay.ErrRelaySessionLimit) {
		t.Fatalf("third lease err = %v", err)
	}
}

func TestRelayLeaseRejectsExhaustedMonthlyQuota(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openRelayTestDB(t, "file:termx-relay-lease-quota-exhausted?mode=memory&cache=shared")
	clock := fixedClock(time.Date(2026, 5, 3, 6, 47, 0, 0, time.UTC))
	svc := relay.NewService(relay.Config{DB: db, Clock: clock})
	seedRelayHub(t, ctx, db, "hub_1")
	seedRelayUserMachine(t, ctx, db, "usr_paid", "mach_1")
	seedRelaySubscription(t, ctx, db, "usr_paid", account.PlanPro, account.SubscriptionActive, clock.Now().Add(-time.Hour), clock.Now().Add(time.Hour))
	if _, err := db.ExecContext(ctx, `
		INSERT INTO relay_usage_monthly(user_id, month, bytes_used)
		VALUES (?, ?, ?)
	`, "usr_paid", "2026-05", int64(5*1024*1024*1024)); err != nil {
		t.Fatalf("seed usage: %v", err)
	}
	lease, err := svc.CreateLease(ctx, relay.CreateLeaseInput{
		UserID:    "usr_paid",
		MachineID: "mach_1",
		HubID:     "hub_1",
		TTL:       time.Minute,
	})
	if err != nil {
		t.Fatalf("exhausted quota should get throttled lease, got err = %v", err)
	}
	if lease.Path != relay.PathManaged || !lease.AllowRelay || !lease.RelayThrottled {
		t.Fatalf("exhausted quota lease policy = %+v", lease)
	}
	if lease.RelayBytesRemaining != 0 {
		t.Fatalf("remaining = %d, want 0", lease.RelayBytesRemaining)
	}
	if lease.RelayThrottleBps <= 0 {
		t.Fatalf("throttle bps = %d, want positive", lease.RelayThrottleBps)
	}
	var rateLimit sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT rate_limit_bps FROM relay_sessions WHERE id = ?`, lease.ID).Scan(&rateLimit); err != nil {
		t.Fatalf("load rate limit: %v", err)
	}
	if !rateLimit.Valid || rateLimit.Int64 != lease.RelayThrottleBps {
		t.Fatalf("stored rate limit = %+v, want %d", rateLimit, lease.RelayThrottleBps)
	}
}

func TestRelayUsageHeartbeatPersistsDeltasAndThrottlesOverQuota(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openRelayTestDB(t, "file:termx-relay-heartbeat-usage?mode=memory&cache=shared")
	clock := fixedClock(time.Date(2026, 5, 3, 7, 8, 0, 0, time.UTC))
	svc := relay.NewService(relay.Config{DB: db, Clock: clock})
	seedRelayHub(t, ctx, db, "hub_1")
	seedRelayUserMachine(t, ctx, db, "usr_paid", "mach_1")
	seedRelaySubscription(t, ctx, db, "usr_paid", account.PlanPro, account.SubscriptionActive, clock.Now().Add(-time.Hour), clock.Now().Add(time.Hour))
	lease, err := svc.CreateLease(ctx, relay.CreateLeaseInput{
		UserID:    "usr_paid",
		MachineID: "mach_1",
		HubID:     "hub_1",
		TTL:       time.Minute,
	})
	if err != nil {
		t.Fatalf("lease: %v", err)
	}

	first, err := svc.RecordHeartbeat(ctx, relay.HeartbeatInput{
		SessionID:          lease.ID,
		AuthenticatedHubID: "hub_1",
		BytesRXTotal:       700,
		BytesTXTotal:       300,
	})
	if err != nil {
		t.Fatalf("record heartbeat: %v", err)
	}
	if first.Status != relay.SessionActive || first.RelayBytesUsed != 1000 || first.RelayThrottled {
		t.Fatalf("first heartbeat = %+v", first)
	}
	var rx, tx, monthly int64
	if err := db.QueryRowContext(ctx, `SELECT bytes_rx, bytes_tx FROM relay_sessions WHERE id = ?`, lease.ID).Scan(&rx, &tx); err != nil {
		t.Fatalf("load session bytes: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT bytes_used FROM relay_usage_monthly WHERE user_id = ? AND month = ?`, "usr_paid", "2026-05").Scan(&monthly); err != nil {
		t.Fatalf("load monthly usage: %v", err)
	}
	if rx != 700 || tx != 300 || monthly != 1000 {
		t.Fatalf("persisted usage rx=%d tx=%d monthly=%d", rx, tx, monthly)
	}

	second, err := svc.RecordHeartbeat(ctx, relay.HeartbeatInput{
		SessionID:          lease.ID,
		AuthenticatedHubID: "hub_1",
		BytesRXTotal:       5*1024*1024*1024 + 700,
		BytesTXTotal:       301,
	})
	if err != nil {
		t.Fatalf("record over-quota heartbeat: %v", err)
	}
	if !second.RelayThrottled || second.RateLimitBps <= 0 || second.RelayBytesRemaining != 0 {
		t.Fatalf("over-quota heartbeat policy = %+v", second)
	}
	var rateLimit sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT rate_limit_bps FROM relay_sessions WHERE id = ?`, lease.ID).Scan(&rateLimit); err != nil {
		t.Fatalf("load heartbeat rate limit: %v", err)
	}
	if !rateLimit.Valid || rateLimit.Int64 != second.RateLimitBps {
		t.Fatalf("stored heartbeat rate limit = %+v, want %d", rateLimit, second.RateLimitBps)
	}
}

func TestRelayUsageHeartbeatIsIdempotentForRetriedCumulativeTotals(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openRelayTestDB(t, "file:termx-relay-heartbeat-idempotent?mode=memory&cache=shared")
	clock := fixedClock(time.Date(2026, 5, 3, 7, 18, 0, 0, time.UTC))
	svc := relay.NewService(relay.Config{DB: db, Clock: clock})
	seedRelayHub(t, ctx, db, "hub_1")
	seedRelayUserMachine(t, ctx, db, "usr_paid", "mach_1")
	seedRelaySubscription(t, ctx, db, "usr_paid", account.PlanPro, account.SubscriptionActive, clock.Now().Add(-time.Hour), clock.Now().Add(time.Hour))
	lease, err := svc.CreateLease(ctx, relay.CreateLeaseInput{
		UserID:    "usr_paid",
		MachineID: "mach_1",
		HubID:     "hub_1",
		TTL:       time.Minute,
	})
	if err != nil {
		t.Fatalf("lease: %v", err)
	}

	for i := 0; i < 2; i++ {
		result, err := svc.RecordHeartbeat(ctx, relay.HeartbeatInput{
			SessionID:          lease.ID,
			AuthenticatedHubID: "hub_1",
			BytesRXTotal:       700,
			BytesTXTotal:       300,
		})
		if err != nil {
			t.Fatalf("heartbeat %d: %v", i, err)
		}
		if result.RelayBytesUsed != 1000 {
			t.Fatalf("heartbeat %d used = %d, want 1000", i, result.RelayBytesUsed)
		}
	}
	next, err := svc.RecordHeartbeat(ctx, relay.HeartbeatInput{
		SessionID:          lease.ID,
		AuthenticatedHubID: "hub_1",
		BytesRXTotal:       900,
		BytesTXTotal:       500,
	})
	if err != nil {
		t.Fatalf("next heartbeat: %v", err)
	}
	if next.RelayBytesUsed != 1400 {
		t.Fatalf("next used = %d, want 1400", next.RelayBytesUsed)
	}
	var monthly int64
	if err := db.QueryRowContext(ctx, `SELECT bytes_used FROM relay_usage_monthly WHERE user_id = ? AND month = ?`, "usr_paid", "2026-05").Scan(&monthly); err != nil {
		t.Fatalf("load monthly usage: %v", err)
	}
	if monthly != 1400 {
		t.Fatalf("monthly = %d, want 1400", monthly)
	}
	if _, err := svc.RecordHeartbeat(ctx, relay.HeartbeatInput{
		SessionID:          lease.ID,
		AuthenticatedHubID: "hub_1",
		BytesRXTotal:       899,
		BytesTXTotal:       500,
	}); !errors.Is(err, relay.ErrInvalidUsageDelta) {
		t.Fatalf("regressed heartbeat err = %v", err)
	}
}

func TestRelayUsageHeartbeatRejectsInvalidOrExpiredSessions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openRelayTestDB(t, "file:termx-relay-heartbeat-invalid?mode=memory&cache=shared")
	clock := fixedClock(time.Date(2026, 5, 3, 7, 12, 0, 0, time.UTC))
	svc := relay.NewService(relay.Config{DB: db, Clock: clock})
	seedRelayHub(t, ctx, db, "hub_1")
	seedRelayHub(t, ctx, db, "hub_2")
	seedRelayUserMachine(t, ctx, db, "usr_paid", "mach_1")
	seedRelaySubscription(t, ctx, db, "usr_paid", account.PlanPro, account.SubscriptionActive, clock.Now().Add(-time.Hour), clock.Now().Add(time.Hour))
	lease, err := svc.CreateLease(ctx, relay.CreateLeaseInput{
		UserID:    "usr_paid",
		MachineID: "mach_1",
		HubID:     "hub_1",
		TTL:       time.Minute,
	})
	if err != nil {
		t.Fatalf("lease: %v", err)
	}
	if _, err := svc.RecordHeartbeat(ctx, relay.HeartbeatInput{
		SessionID:          lease.ID,
		AuthenticatedHubID: "hub_1",
		BytesRXTotal:       -1,
	}); !errors.Is(err, relay.ErrInvalidUsageDelta) {
		t.Fatalf("negative delta err = %v", err)
	}
	if _, err := svc.RecordHeartbeat(ctx, relay.HeartbeatInput{
		SessionID:          lease.ID,
		AuthenticatedHubID: "hub_2",
		BytesRXTotal:       1,
	}); !errors.Is(err, relay.ErrRelaySessionNotFound) {
		t.Fatalf("wrong hub heartbeat err = %v", err)
	}

	expiredSvc := relay.NewService(relay.Config{DB: db, Clock: fixedClock(clock.Now().Add(2 * time.Minute))})
	if _, err := expiredSvc.RecordHeartbeat(ctx, relay.HeartbeatInput{
		SessionID:          lease.ID,
		AuthenticatedHubID: "hub_1",
		BytesRXTotal:       1,
	}); !errors.Is(err, relay.ErrRelaySessionNotFound) {
		t.Fatalf("expired heartbeat err = %v", err)
	}
}

func TestRelayUsageHeartbeatRejectsOfflineHubWithoutCountingUsage(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openRelayTestDB(t, "file:termx-relay-heartbeat-offline-hub?mode=memory&cache=shared")
	clock := fixedClock(time.Date(2026, 5, 3, 7, 19, 0, 0, time.UTC))
	svc := relay.NewService(relay.Config{DB: db, Clock: clock})
	seedRelayHub(t, ctx, db, "hub_1")
	seedRelayUserMachine(t, ctx, db, "usr_paid", "mach_1")
	seedRelaySubscription(t, ctx, db, "usr_paid", account.PlanPro, account.SubscriptionActive, clock.Now().Add(-time.Hour), clock.Now().Add(time.Hour))
	lease, err := svc.CreateLease(ctx, relay.CreateLeaseInput{
		UserID:    "usr_paid",
		MachineID: "mach_1",
		HubID:     "hub_1",
		TTL:       time.Minute,
	})
	if err != nil {
		t.Fatalf("lease: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE hubs SET status = 'offline' WHERE id = ?`, "hub_1"); err != nil {
		t.Fatalf("mark hub offline: %v", err)
	}
	if _, err := svc.RecordHeartbeat(ctx, relay.HeartbeatInput{
		SessionID:          lease.ID,
		AuthenticatedHubID: "hub_1",
		BytesRXTotal:       700,
		BytesTXTotal:       300,
	}); !errors.Is(err, relay.ErrHubUnavailable) {
		t.Fatalf("offline hub heartbeat err = %v", err)
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(1) FROM relay_usage_monthly WHERE user_id = ?`, "usr_paid").Scan(&count); err != nil {
		t.Fatalf("count usage: %v", err)
	}
	if count != 0 {
		t.Fatalf("usage rows after rejected heartbeat = %d, want 0", count)
	}
}

func TestRelaySessionCleanupExpiresStaleSessionsAndFreesCapacity(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openRelayTestDB(t, "file:termx-relay-cleanup-capacity?mode=memory&cache=shared")
	clock := fixedClock(time.Date(2026, 5, 3, 7, 9, 0, 0, time.UTC))
	svc := relay.NewService(relay.Config{DB: db, Clock: clock})
	seedRelayHub(t, ctx, db, "hub_1")
	seedRelayUserMachine(t, ctx, db, "usr_paid", "mach_1")
	seedRelaySubscription(t, ctx, db, "usr_paid", account.PlanPro, account.SubscriptionActive, clock.Now().Add(-time.Hour), clock.Now().Add(time.Hour))
	first, err := svc.CreateLease(ctx, relay.CreateLeaseInput{
		UserID:    "usr_paid",
		MachineID: "mach_1",
		HubID:     "hub_1",
		TTL:       time.Minute,
	})
	if err != nil {
		t.Fatalf("first lease: %v", err)
	}
	second, err := svc.CreateLease(ctx, relay.CreateLeaseInput{
		UserID:    "usr_paid",
		MachineID: "mach_1",
		HubID:     "hub_1",
		TTL:       time.Minute,
	})
	if err != nil {
		t.Fatalf("second lease: %v", err)
	}
	if _, err := svc.CreateLease(ctx, relay.CreateLeaseInput{
		UserID:    "usr_paid",
		MachineID: "mach_1",
		HubID:     "hub_1",
		TTL:       time.Minute,
	}); !errors.Is(err, relay.ErrRelaySessionLimit) {
		t.Fatalf("third lease err = %v", err)
	}

	cleaned, err := svc.CleanupExpired(ctx, relay.CleanupInput{
		Now:               clock.Now().Add(2 * time.Minute),
		HeartbeatTimeout:  30 * time.Second,
		ExpireLeasedAfter: 30 * time.Second,
		ExpireActiveAfter: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if cleaned != 2 {
		t.Fatalf("cleaned = %d, want 2", cleaned)
	}
	for _, id := range []string{first.ID, second.ID} {
		var status string
		if err := db.QueryRowContext(ctx, `SELECT status FROM relay_sessions WHERE id = ?`, id).Scan(&status); err != nil {
			t.Fatalf("load status %s: %v", id, err)
		}
		if status != relay.SessionExpired {
			t.Fatalf("status for %s = %q", id, status)
		}
	}
	if _, err := svc.CreateLease(ctx, relay.CreateLeaseInput{
		UserID:    "usr_paid",
		MachineID: "mach_1",
		HubID:     "hub_1",
		TTL:       time.Minute,
	}); err != nil {
		t.Fatalf("lease after cleanup should fit session limit: %v", err)
	}
}

func TestRelaySessionCleanupKeepsFreshActiveSessions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openRelayTestDB(t, "file:termx-relay-cleanup-fresh?mode=memory&cache=shared")
	clock := fixedClock(time.Date(2026, 5, 3, 7, 14, 0, 0, time.UTC))
	svc := relay.NewService(relay.Config{DB: db, Clock: clock})
	seedRelayHub(t, ctx, db, "hub_1")
	seedRelayUserMachine(t, ctx, db, "usr_paid", "mach_1")
	seedRelaySubscription(t, ctx, db, "usr_paid", account.PlanPro, account.SubscriptionActive, clock.Now().Add(-time.Hour), clock.Now().Add(time.Hour))
	lease, err := svc.CreateLease(ctx, relay.CreateLeaseInput{
		UserID:    "usr_paid",
		MachineID: "mach_1",
		HubID:     "hub_1",
		TTL:       5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("lease: %v", err)
	}
	if _, err := svc.RecordHeartbeat(ctx, relay.HeartbeatInput{
		SessionID:          lease.ID,
		AuthenticatedHubID: "hub_1",
		BytesRXTotal:       1,
	}); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}

	cleaned, err := svc.CleanupExpired(ctx, relay.CleanupInput{
		Now:               clock.Now().Add(10 * time.Second),
		HeartbeatTimeout:  30 * time.Second,
		ExpireLeasedAfter: 30 * time.Second,
		ExpireActiveAfter: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if cleaned != 0 {
		t.Fatalf("cleaned = %d, want 0", cleaned)
	}
	var status string
	if err := db.QueryRowContext(ctx, `SELECT status FROM relay_sessions WHERE id = ?`, lease.ID).Scan(&status); err != nil {
		t.Fatalf("load status: %v", err)
	}
	if status != relay.SessionActive {
		t.Fatalf("fresh active status = %q", status)
	}
}

func openRelayTestDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	ctx := context.Background()
	db, err := store.OpenSQLite(ctx, dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	seedRelayPlans(t, ctx, db)
	return db
}

func seedRelayPlans(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
		INSERT OR IGNORE INTO plans(id, name, monthly_relay_bytes, relay_session_limit, relay_throttle_bps)
		VALUES
			(?, 'Registered Free', 0, 0, 0),
			(?, 'Pro', ?, 2, ?)
	`, account.PlanRegisteredFree, account.PlanPro, int64(5*1024*1024*1024), int64(256*1024)); err != nil {
		t.Fatalf("seed plans: %v", err)
	}
}

func seedRelayHub(t *testing.T, ctx context.Context, db *sql.DB, hubID string) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO hubs(id, region, http_url, status)
		VALUES (?, 'test', 'http://hub.test', 'online')
	`, hubID); err != nil {
		t.Fatalf("seed hub: %v", err)
	}
}

func seedRelayUserMachine(t *testing.T, ctx context.Context, db *sql.DB, userID string, machineID string) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO users(id, email, password_hash, role)
		VALUES (?, ?, 'hash', 'user')
	`, userID, userID+"@example.com"); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO machines(id, owner_user_id, machine_public_key, display_name)
		VALUES (?, ?, ?, ?)
	`, machineID, userID, "pub_"+machineID, "Machine "+machineID); err != nil {
		t.Fatalf("seed machine: %v", err)
	}
}

func seedRelaySubscription(t *testing.T, ctx context.Context, db *sql.DB, userID string, planID string, status string, start time.Time, end time.Time) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO subscriptions(id, user_id, plan_id, status, provider_order_id, current_period_start, current_period_end)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, "sub_"+userID+"_"+start.Format("150405"), userID, planID, status, "provider_"+userID+"_"+start.Format("150405"), start.Format(time.RFC3339Nano), end.Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("seed subscription: %v", err)
	}
}

type fixedClock time.Time

func (c fixedClock) Now() time.Time {
	return time.Time(c)
}
