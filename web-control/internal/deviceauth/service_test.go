package deviceauth_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/web-control/internal/account"
	"github.com/lozzow/termx/web-control/internal/deviceauth"
	"github.com/lozzow/termx/web-control/internal/store"
)

func TestDeviceCodeApprovePollRejectExpireAndHashStorage(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := store.OpenSQLite(ctx, "file:termx-deviceauth-service-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	clock := &mutableClock{now: time.Date(2026, 5, 3, 19, 0, 0, 0, time.UTC)}
	accounts := account.NewService(account.Config{
		DB:     db,
		Clock:  clock,
		Tokens: account.NewHMACTokenIssuer([]byte("slice-17-deviceauth-secret")),
	})
	auth, err := accounts.Register(ctx, account.RegisterInput{Email: "device@example.com", Password: "valid password"})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	svc := deviceauth.NewService(deviceauth.Config{DB: db, Accounts: accounts, Clock: clock})

	created, err := svc.Create(ctx, deviceauth.CreateInput{ClientName: "termx cli", VerificationURI: "https://control.example.test/device"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.DeviceCode == "" || created.UserCode == "" || created.ExpiresInSeconds <= 0 ||
		created.IntervalSeconds <= 0 || !strings.Contains(created.VerificationURIComplete, created.UserCode) {
		t.Fatalf("unexpected create result: %+v", created)
	}
	pending, err := svc.Poll(ctx, deviceauth.PollInput{DeviceCode: created.DeviceCode})
	if err != nil {
		t.Fatalf("pending Poll: %v", err)
	}
	if pending.Status != deviceauth.StatusPending || pending.Auth.AccessToken != "" {
		t.Fatalf("expected pending without auth, got %+v", pending)
	}

	if err := svc.Approve(ctx, deviceauth.DecisionInput{UserID: auth.User.ID, UserCode: created.UserCode}); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	approved, err := svc.Poll(ctx, deviceauth.PollInput{DeviceCode: created.DeviceCode})
	if err != nil {
		t.Fatalf("approved Poll: %v", err)
	}
	if approved.Status != deviceauth.StatusApproved || approved.Auth.AccessToken == "" || approved.Auth.RefreshToken == "" {
		t.Fatalf("expected approved auth result, got %+v", approved)
	}
	if me, err := accounts.Me(ctx, approved.Auth.AccessToken); err != nil || me.User.ID != auth.User.ID {
		t.Fatalf("approved token did not authenticate approved user: me=%+v err=%v", me, err)
	}
	if _, err := svc.Poll(ctx, deviceauth.PollInput{DeviceCode: created.DeviceCode}); !deviceauth.IsAlreadyConsumed(err) {
		t.Fatalf("expected second poll to be rejected as consumed, got %v", err)
	}

	rejected, err := svc.Create(ctx, deviceauth.CreateInput{ClientName: "termx cli"})
	if err != nil {
		t.Fatalf("Create rejected code: %v", err)
	}
	if err := svc.Reject(ctx, deviceauth.DecisionInput{UserID: auth.User.ID, UserCode: rejected.UserCode, Reason: "user denied"}); err != nil {
		t.Fatalf("Reject: %v", err)
	}
	if _, err := svc.Poll(ctx, deviceauth.PollInput{DeviceCode: rejected.DeviceCode}); !deviceauth.IsAccessDenied(err) {
		t.Fatalf("expected rejected poll access denied, got %v", err)
	}

	expired, err := svc.Create(ctx, deviceauth.CreateInput{ClientName: "termx cli", ExpiresIn: time.Minute})
	if err != nil {
		t.Fatalf("Create expired code: %v", err)
	}
	clock.now = clock.now.Add(2 * time.Minute)
	if _, err := svc.Poll(ctx, deviceauth.PollInput{DeviceCode: expired.DeviceCode}); !deviceauth.IsExpired(err) {
		t.Fatalf("expected expired poll, got %v", err)
	}

	var plainCount int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(1)
		FROM device_auth_codes
		WHERE device_code_hash = ? OR user_code_hash = ?
	`, created.DeviceCode, created.UserCode).Scan(&plainCount); err != nil {
		t.Fatalf("query stored hashes: %v", err)
	}
	if plainCount != 0 {
		t.Fatal("device auth codes stored plaintext code material")
	}
}

func TestDeviceAuthCleanupExpiresPendingCodes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := store.OpenSQLite(ctx, "file:termx-deviceauth-cleanup-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	clock := &mutableClock{now: time.Date(2026, 5, 3, 19, 30, 0, 0, time.UTC)}
	accounts := account.NewService(account.Config{
		DB:     db,
		Clock:  clock,
		Tokens: account.NewHMACTokenIssuer([]byte("slice-17-deviceauth-cleanup-secret")),
	})
	auth, err := accounts.Register(ctx, account.RegisterInput{Email: "cleanup@example.com", Password: "valid password"})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	svc := deviceauth.NewService(deviceauth.Config{DB: db, Accounts: accounts, Clock: clock})
	expiring, err := svc.Create(ctx, deviceauth.CreateInput{ClientName: "termx cli", ExpiresIn: time.Minute})
	if err != nil {
		t.Fatalf("create expiring: %v", err)
	}
	approved, err := svc.Create(ctx, deviceauth.CreateInput{ClientName: "termx cli", ExpiresIn: time.Minute})
	if err != nil {
		t.Fatalf("create approved: %v", err)
	}
	if err := svc.Approve(ctx, deviceauth.DecisionInput{UserID: auth.User.ID, UserCode: approved.UserCode}); err != nil {
		t.Fatalf("approve: %v", err)
	}
	cleanup, err := svc.CleanupExpired(ctx, deviceauth.CleanupInput{Now: clock.now.Add(2 * time.Minute)})
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if cleanup.Expired != 1 {
		t.Fatalf("expected one pending code to expire, got %+v", cleanup)
	}
	if _, err := svc.Poll(ctx, deviceauth.PollInput{DeviceCode: expiring.DeviceCode}); !deviceauth.IsExpired(err) {
		t.Fatalf("expected expiring code to be expired, got %v", err)
	}
	result, err := svc.Poll(ctx, deviceauth.PollInput{DeviceCode: approved.DeviceCode})
	if err != nil {
		t.Fatalf("approved code should remain redeemable after cleanup: %v", err)
	}
	if result.Auth.AccessToken == "" {
		t.Fatalf("approved cleanup survivor missing auth: %+v", result)
	}
	cleanup, err = svc.CleanupExpired(ctx, deviceauth.CleanupInput{Now: clock.now.Add(3 * time.Hour), Retention: time.Hour})
	if err != nil {
		t.Fatalf("retention cleanup: %v", err)
	}
	if cleanup.Deleted == 0 {
		t.Fatalf("expected cleanup to delete retained terminal rows, got %+v", cleanup)
	}
}

func TestDeviceAuthBoundsActiveCodesAndLocksAfterBadAttempts(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := store.OpenSQLite(ctx, "file:termx-deviceauth-bounds-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	clock := &mutableClock{now: time.Date(2026, 5, 3, 19, 50, 0, 0, time.UTC)}
	accounts := account.NewService(account.Config{
		DB:     db,
		Clock:  clock,
		Tokens: account.NewHMACTokenIssuer([]byte("slice-17-deviceauth-bounds-secret")),
	})
	auth, err := accounts.Register(ctx, account.RegisterInput{Email: "bounds@example.com", Password: "valid password"})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	svc := deviceauth.NewService(deviceauth.Config{DB: db, Accounts: accounts, Clock: clock})
	first, err := svc.Create(ctx, deviceauth.CreateInput{ClientName: "termx cli"})
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	for i := 0; i < 5; i++ {
		if err := svc.Approve(ctx, deviceauth.DecisionInput{UserID: auth.User.ID, UserCode: "WRONG-CODE"}); err == nil {
			t.Fatalf("bad attempt %d unexpectedly succeeded", i)
		}
	}
	if err := svc.Approve(ctx, deviceauth.DecisionInput{UserID: auth.User.ID, UserCode: first.UserCode}); !deviceauth.IsAttemptLocked(err) {
		t.Fatalf("expected locked code after bad attempts, got %v", err)
	}

	for i := 0; i < 127; i++ {
		if _, err := svc.Create(ctx, deviceauth.CreateInput{ClientName: "termx cli"}); err != nil {
			t.Fatalf("create active code %d: %v", i, err)
		}
	}
	if _, err := svc.Create(ctx, deviceauth.CreateInput{ClientName: "termx cli"}); !deviceauth.IsRateLimited(err) {
		t.Fatalf("expected active-code rate limit, got %v", err)
	}
}

type mutableClock struct {
	now time.Time
}

func (c *mutableClock) Now() time.Time {
	return c.now
}
