package account_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/lozzow/termx/web-control/internal/account"
	"github.com/lozzow/termx/web-control/internal/store"
)

func TestRegisterLoginRefreshAndDefaultFreePlan(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := store.OpenSQLite(ctx, "file:termx-account-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	svc := account.NewService(account.Config{
		DB:       db,
		Clock:    fixedClock(time.Date(2026, 5, 3, 3, 10, 0, 0, time.UTC)),
		Tokens:   account.NewHMACTokenIssuer([]byte("slice-2-test-secret")),
		Payments: account.NewMockPaymentProvider(),
	})

	registered, err := svc.Register(ctx, account.RegisterInput{
		Email:    "alice@example.com",
		Password: "correct horse battery staple",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if registered.User.Email != "alice@example.com" {
		t.Fatalf("email = %q", registered.User.Email)
	}
	if registered.Plan.ID != account.PlanRegisteredFree {
		t.Fatalf("default plan = %q, want registered_free", registered.Plan.ID)
	}
	if registered.Plan.AllowPublicP2P != true {
		t.Fatal("registered free should allow public_p2p")
	}
	if registered.Plan.AllowRelay != false {
		t.Fatal("registered free must not allow TermX TURN relay")
	}
	if registered.AccessToken == "" || registered.RefreshToken == "" {
		t.Fatal("register did not return access and refresh tokens")
	}

	login, err := svc.Login(ctx, account.LoginInput{
		Email:    "alice@example.com",
		Password: "correct horse battery staple",
	})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if login.User.ID != registered.User.ID {
		t.Fatalf("login user id = %q, want %q", login.User.ID, registered.User.ID)
	}

	refreshed, err := svc.Refresh(ctx, login.RefreshToken)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if refreshed.AccessToken == "" || refreshed.RefreshToken == "" {
		t.Fatal("refresh did not return rotated tokens")
	}
	if refreshed.RefreshToken == login.RefreshToken {
		t.Fatal("refresh token was not rotated")
	}
	if _, err := svc.Refresh(ctx, login.RefreshToken); err == nil {
		t.Fatal("old refresh token remained usable after rotation")
	}
}

func TestLoginRejectsWrongPasswordAndMeRejectsMissingToken(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := store.OpenSQLite(ctx, "file:termx-account-negative-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	svc := account.NewService(account.Config{
		DB:       db,
		Clock:    fixedClock(time.Date(2026, 5, 3, 3, 11, 0, 0, time.UTC)),
		Tokens:   account.NewHMACTokenIssuer([]byte("slice-2-negative-secret")),
		Payments: account.NewMockPaymentProvider(),
	})
	if _, err := svc.Register(ctx, account.RegisterInput{Email: "bob@example.com", Password: "valid password"}); err != nil {
		t.Fatalf("register: %v", err)
	}

	if _, err := svc.Login(ctx, account.LoginInput{Email: "bob@example.com", Password: "wrong password"}); err == nil {
		t.Fatal("login with wrong password succeeded")
	}
	if _, err := svc.Me(ctx, ""); err == nil {
		t.Fatal("me without token succeeded")
	}
}

func TestRegisterFailsCleanlyWithoutTokenIssuer(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := store.OpenSQLite(ctx, "file:termx-account-no-token-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	svc := account.NewService(account.Config{DB: db})
	if _, err := svc.Register(ctx, account.RegisterInput{Email: "no-token@example.com", Password: "valid password"}); err == nil {
		t.Fatal("register without token issuer succeeded")
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE email = 'no-token@example.com'`).Scan(&count); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if count != 0 {
		t.Fatalf("register without token issuer left %d partial users", count)
	}
}

func TestRefreshFailsCleanlyWithoutTokenIssuer(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := store.OpenSQLite(ctx, "file:termx-account-refresh-no-token-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	withTokens := account.NewService(account.Config{
		DB:     db,
		Clock:  fixedClock(time.Date(2026, 5, 3, 3, 16, 0, 0, time.UTC)),
		Tokens: account.NewHMACTokenIssuer([]byte("slice-2-refresh-no-token-secret")),
	})
	registered, err := withTokens.Register(ctx, account.RegisterInput{Email: "refresh-no-token@example.com", Password: "valid password"})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	withoutTokens := account.NewService(account.Config{
		DB:    db,
		Clock: fixedClock(time.Date(2026, 5, 3, 3, 16, 0, 0, time.UTC)),
	})
	if _, err := withoutTokens.Refresh(ctx, registered.RefreshToken); err == nil {
		t.Fatal("refresh without token issuer succeeded")
	}
}

func TestMeFailsCleanlyWithoutTokenIssuer(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := store.OpenSQLite(ctx, "file:termx-account-me-no-token-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	svc := account.NewService(account.Config{
		DB:    db,
		Clock: fixedClock(time.Date(2026, 5, 3, 3, 18, 0, 0, time.UTC)),
	})
	if _, err := svc.Me(ctx, "not-a-real-token"); err == nil {
		t.Fatal("me without token issuer succeeded")
	}
}

func TestAccessTokenRejectsExpiredToken(t *testing.T) {
	t.Parallel()

	issuer := account.NewHMACTokenIssuer([]byte("slice-2-expired-secret"))
	token, err := issuer.IssueAccess("usr_expired", time.Now().Add(-30*time.Minute))
	if err != nil {
		t.Fatalf("issue expired token: %v", err)
	}
	if _, err := issuer.VerifyAccess(token, time.Now()); err == nil {
		t.Fatal("expired access token verified successfully")
	}
}

func TestRefreshRotationAllowsOnlyOneConcurrentUse(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := store.OpenSQLite(ctx, "file:termx-account-refresh-race-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	svc := account.NewService(account.Config{
		DB:     db,
		Clock:  fixedClock(time.Date(2026, 5, 3, 3, 15, 0, 0, time.UTC)),
		Tokens: account.NewHMACTokenIssuer([]byte("slice-2-refresh-race-secret")),
	})
	registered, err := svc.Register(ctx, account.RegisterInput{Email: "refresh-race@example.com", Password: "valid password"})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := svc.Refresh(ctx, registered.RefreshToken)
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)

	successes := 0
	for err := range errs {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent refresh successes = %d, want exactly 1", successes)
	}
}

func TestMeUsesInjectedClockForAccessTokenExpiry(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := store.OpenSQLite(ctx, "file:termx-account-clock-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	clock := &mutableClock{value: time.Date(2026, 5, 3, 3, 11, 0, 0, time.UTC)}
	svc := account.NewService(account.Config{
		DB:       db,
		Clock:    clock,
		Tokens:   account.NewHMACTokenIssuer([]byte("slice-2-clock-secret")),
		Payments: account.NewMockPaymentProvider(),
	})
	registered, err := svc.Register(ctx, account.RegisterInput{Email: "clock@example.com", Password: "valid password"})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := svc.Me(ctx, registered.AccessToken); err != nil {
		t.Fatalf("fresh token rejected: %v", err)
	}
	clock.value = clock.value.Add(16 * time.Minute)
	if _, err := svc.Me(ctx, registered.AccessToken); err == nil {
		t.Fatal("expired token accepted using service clock")
	}
}

func TestActiveSubscriptionAfterPeriodEndFallsBackToFreePolicy(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := store.OpenSQLite(ctx, "file:termx-account-expired-sub-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	clock := fixedClock(time.Date(2026, 5, 3, 3, 20, 0, 0, time.UTC))
	svc := account.NewService(account.Config{
		DB:       db,
		Clock:    clock,
		Tokens:   account.NewHMACTokenIssuer([]byte("slice-2-expired-sub-secret")),
		Payments: account.NewMockPaymentProvider(),
	})
	registered, err := svc.Register(ctx, account.RegisterInput{Email: "expired-sub@example.com", Password: "valid password"})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO subscriptions(id, user_id, plan_id, status, provider_order_id, current_period_start, current_period_end)
		VALUES ('sub_expired_period', ?, ?, ?, 'manual', ?, ?)
	`, registered.User.ID, account.PlanPro, account.SubscriptionActive, clock.Now().Add(-2*time.Hour).Format(time.RFC3339Nano), clock.Now().Add(-time.Hour).Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("insert expired subscription: %v", err)
	}
	me, err := svc.Me(ctx, registered.AccessToken)
	if err != nil {
		t.Fatalf("me: %v", err)
	}
	if me.Plan.AllowRelay {
		t.Fatal("expired active subscription still allows relay")
	}
}

type fixedClock time.Time

func (c fixedClock) Now() time.Time {
	return time.Time(c)
}

type mutableClock struct {
	value time.Time
}

func (c *mutableClock) Now() time.Time {
	return c.value
}
