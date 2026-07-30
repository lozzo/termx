package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/anytty/anytty/cloud/controller/account"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	testAdminPassword = "bootstrap-password"
	testUserPassword  = "initial-user-password"
)

func TestAccountLifecycleMigrationAndSetupCredential(t *testing.T) {
	database, ctx, now := accountLifecycleDatabase(t)
	service := accountLifecycleService(t, database, now)
	admin := bootstrapAccount(t, ctx, service, "lifecycle-admin-"+uuid.NewString()+"@example.com")
	adminContext := recentAdminContext(ctx, admin, now)

	var passwordColumnCount int
	if err := database.pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND table_name='accounts' AND column_name='password_hash'`).Scan(&passwordColumnCount); err != nil {
		t.Fatal(err)
	}
	if passwordColumnCount != 0 {
		t.Fatal("accounts still exposes password_hash")
	}

	provisioned, err := service.ProvisionAccount(adminContext, &cloudv1.ProvisionAccountRequest{
		Email: " Setup." + uuid.NewString() + "@Example.com ", DisplayName: " Setup User ", Reason: " approved lifecycle test ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if provisioned.GetAccount().GetState() != cloudv1.AccountState_ACCOUNT_STATE_PENDING || len(provisioned.GetSetupCredential()) != 43 || !provisioned.GetExpiresAt().AsTime().Equal(now.Add(time.Hour)) {
		t.Fatalf("provision response=%v", provisioned)
	}
	digest := sha256.Sum256([]byte(provisioned.GetSetupCredential()))
	var passwordHash, setupDigest []byte
	var expiresAt time.Time
	if err := database.pool.QueryRow(ctx, `SELECT password_hash,setup_digest,setup_expires_at FROM account_credentials WHERE account_id=$1`, provisioned.GetAccount().GetAccountId()).Scan(&passwordHash, &setupDigest, &expiresAt); err != nil {
		t.Fatal(err)
	}
	if len(passwordHash) != 0 || !bytes.Equal(setupDigest, digest[:]) || !expiresAt.Equal(provisioned.GetExpiresAt().AsTime()) || bytes.Contains(setupDigest, []byte(provisioned.GetSetupCredential())) {
		t.Fatal("provisioned credential was not persisted as only its SHA-256 digest")
	}
	if _, err := service.Login(ctx, &cloudv1.LoginAccountRequest{Login: provisioned.GetAccount().GetEmail(), Password: testUserPassword}); !errors.Is(err, account.ErrUnauthenticated) {
		t.Fatalf("pending login error=%v", err)
	}

	const contenders = 8
	start := make(chan struct{})
	results := make(chan error, contenders)
	for index := 0; index < contenders; index++ {
		go func() {
			<-start
			_, redeemErr := service.RedeemAccountSetup(ctx, &cloudv1.RedeemAccountSetupRequest{SetupCredential: provisioned.GetSetupCredential(), NewPassword: testUserPassword})
			results <- redeemErr
		}()
	}
	close(start)
	succeeded := 0
	for index := 0; index < contenders; index++ {
		redeemErr := <-results
		if redeemErr == nil {
			succeeded++
			continue
		}
		if !errors.Is(redeemErr, account.ErrSetupCredentialInvalid) {
			t.Fatalf("concurrent redeem error=%v", redeemErr)
		}
	}
	if succeeded != 1 {
		t.Fatalf("successful concurrent redeems=%d, want 1", succeeded)
	}
	if _, err := service.RedeemAccountSetup(ctx, &cloudv1.RedeemAccountSetupRequest{SetupCredential: provisioned.GetSetupCredential(), NewPassword: testUserPassword}); !errors.Is(err, account.ErrSetupCredentialInvalid) {
		t.Fatalf("replayed setup credential error=%v", err)
	}
	login, err := service.Login(ctx, &cloudv1.LoginAccountRequest{Login: provisioned.GetAccount().GetEmail(), Password: testUserPassword})
	if err != nil {
		t.Fatal(err)
	}

	expired, err := service.ProvisionAccount(adminContext, &cloudv1.ProvisionAccountRequest{Email: "expired-" + uuid.NewString() + "@example.com", DisplayName: "Expired Setup", Reason: "expiry test"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.pool.Exec(ctx, `UPDATE account_credentials SET setup_expires_at=$1 WHERE account_id=$2`, now.Add(-time.Nanosecond), expired.GetAccount().GetAccountId()); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RedeemAccountSetup(ctx, &cloudv1.RedeemAccountSetupRequest{SetupCredential: expired.GetSetupCredential(), NewPassword: testUserPassword}); !errors.Is(err, account.ErrSetupCredentialInvalid) {
		t.Fatalf("expired setup credential error=%v", err)
	}

	reset, err := service.ResetAccountSetup(adminContext, &cloudv1.ResetAccountSetupRequest{AccountId: provisioned.GetAccount().GetAccountId(), Reason: "lost password"})
	if err != nil {
		t.Fatal(err)
	}
	if reset.GetAccount().GetState() != cloudv1.AccountState_ACCOUNT_STATE_PENDING || reset.GetSetupCredential() == provisioned.GetSetupCredential() || len(reset.GetSetupCredential()) != 43 {
		t.Fatalf("reset response=%v", reset)
	}
	var liveSessions int
	if err := database.pool.QueryRow(ctx, `SELECT count(*) FROM account_sessions WHERE account_id=$1 AND revoked_at IS NULL`, provisioned.GetAccount().GetAccountId()).Scan(&liveSessions); err != nil {
		t.Fatal(err)
	}
	if liveSessions != 0 {
		t.Fatalf("live sessions after reset=%d", liveSessions)
	}
	if _, err := service.AuthenticateAccess(ctx, login.GetSession().GetAccessToken()); !errors.Is(err, account.ErrUnauthenticated) {
		t.Fatalf("old access token after reset error=%v", err)
	}
	if _, err := service.Login(ctx, &cloudv1.LoginAccountRequest{Login: provisioned.GetAccount().GetEmail(), Password: testUserPassword}); !errors.Is(err, account.ErrUnauthenticated) {
		t.Fatalf("old password after reset error=%v", err)
	}
	if _, err := service.RedeemAccountSetup(ctx, &cloudv1.RedeemAccountSetupRequest{SetupCredential: reset.GetSetupCredential(), NewPassword: "replacement-password"}); err != nil {
		t.Fatal(err)
	}
}

func TestBootstrapAccountDatabaseBranches(t *testing.T) {
	database, ctx, now := accountLifecycleDatabase(t)
	service := accountLifecycleService(t, database, now)
	email := "bootstrap-" + uuid.NewString() + "@example.com"
	created := bootstrapAccount(t, ctx, service, email)
	var originalHash []byte
	if err := database.pool.QueryRow(ctx, `SELECT password_hash FROM account_credentials WHERE account_id=$1`, created.GetAccountId()).Scan(&originalHash); err != nil {
		t.Fatal(err)
	}
	if _, err := service.EnsureBootstrapOperator(ctx, email, "x"); err != nil {
		t.Fatalf("existing bootstrap rejected ignored candidate password: %v", err)
	}
	var unchangedHash []byte
	if err := database.pool.QueryRow(ctx, `SELECT password_hash FROM account_credentials WHERE account_id=$1`, created.GetAccountId()).Scan(&unchangedHash); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(originalHash, unchangedHash) {
		t.Fatal("existing bootstrap password was modified")
	}

	tests := []struct {
		name   string
		mutate func(string)
	}{
		{name: "disabled", mutate: func(accountID string) {
			if _, err := database.pool.Exec(ctx, `UPDATE accounts SET state='disabled' WHERE account_id=$1`, accountID); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "missing admin role", mutate: func(accountID string) {
			if _, err := database.pool.Exec(ctx, `DELETE FROM account_roles WHERE account_id=$1 AND role='admin'`, accountID); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "malformed password hash", mutate: func(accountID string) {
			if _, err := database.pool.Exec(ctx, `UPDATE account_credentials SET password_hash='not-bcrypt'::bytea WHERE account_id=$1`, accountID); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			invalidEmail := "bootstrap-invalid-" + uuid.NewString() + "@example.com"
			profile := bootstrapAccount(t, ctx, service, invalidEmail)
			test.mutate(profile.GetAccountId())
			if _, err := service.EnsureBootstrapOperator(ctx, invalidEmail, "ignored-candidate"); err == nil {
				t.Fatal("invalid existing bootstrap account was accepted")
			}
		})
	}

	mixedCaseEmail := "Mixed-" + uuid.NewString() + "@Example.com"
	insertActiveAccount(t, ctx, database, mixedCaseEmail, true, now)
	if _, err := service.EnsureBootstrapOperator(ctx, strings.ToLower(mixedCaseEmail), "candidate-password"); err == nil {
		t.Fatal("bootstrap accepted a case-insensitive rather than exact persisted email")
	}
}

func TestUnknownPersistedAccountStateAndRoleFailClosedInDatabase(t *testing.T) {
	database, ctx, now := accountLifecycleDatabase(t)
	profile := insertActiveAccount(t, ctx, database, "unknown-enum-"+uuid.NewString()+"@example.com", true, now)

	t.Run("state", func(t *testing.T) {
		tx, err := database.pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(context.Background()) }()
		if _, err := tx.Exec(ctx, `ALTER TABLE accounts DROP CONSTRAINT accounts_state_check`); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, `UPDATE accounts SET state='future-state' WHERE account_id=$1`, profile.GetAccountId()); err != nil {
			t.Fatal(err)
		}
		if _, err := scanAccountRecord(tx.QueryRow(ctx, accountSelect+` WHERE a.account_id=$1`, profile.GetAccountId())); err == nil || !strings.Contains(err.Error(), "unknown persisted account state") {
			t.Fatalf("unknown DB state error=%v", err)
		}
	})

	t.Run("role", func(t *testing.T) {
		tx, err := database.pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(context.Background()) }()
		if _, err := tx.Exec(ctx, `ALTER TABLE account_roles DROP CONSTRAINT account_roles_role_check`); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, `UPDATE account_roles SET role='future-role' WHERE account_id=$1 AND role='admin'`, profile.GetAccountId()); err != nil {
			t.Fatal(err)
		}
		if _, err := scanAccountRecord(tx.QueryRow(ctx, accountSelect+` WHERE a.account_id=$1`, profile.GetAccountId())); err == nil || !strings.Contains(err.Error(), "unknown persisted account role") {
			t.Fatalf("unknown DB role error=%v", err)
		}
	})
}

func TestLoginAndRefreshRaceAccountMutations(t *testing.T) {
	authDatabase, ctx, now := accountLifecycleDatabase(t)
	databaseURL := os.Getenv("ANYTTY_CLOUD_TEST_DATABASE_URL")
	mutationDatabase, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mutationDatabase.Close)
	authService := accountLifecycleService(t, authDatabase, now)
	mutationService := accountLifecycleService(t, mutationDatabase, now)
	admin := bootstrapAccount(t, ctx, authService, "race-admin-"+uuid.NewString()+"@example.com")
	adminContext := recentAdminContext(ctx, admin, now)

	for _, authKind := range []string{"login", "refresh"} {
		for _, mutationKind := range []string{"reset", "disable", "change-password"} {
			t.Run(authKind+"-vs-"+mutationKind, func(t *testing.T) {
				for iteration := 0; iteration < 4; iteration++ {
					provisioned, err := authService.ProvisionAccount(adminContext, &cloudv1.ProvisionAccountRequest{
						Email: uuid.NewString() + "@race.example.com", DisplayName: "Race User", Reason: "transaction race",
					})
					if err != nil {
						t.Fatal(err)
					}
					if _, err := authService.RedeemAccountSetup(ctx, &cloudv1.RedeemAccountSetupRequest{SetupCredential: provisioned.GetSetupCredential(), NewPassword: testUserPassword}); err != nil {
						t.Fatal(err)
					}
					initialLogin, err := authService.Login(ctx, &cloudv1.LoginAccountRequest{Login: provisioned.GetAccount().GetEmail(), Password: testUserPassword})
					if err != nil {
						t.Fatal(err)
					}

					start := make(chan struct{})
					authResult := make(chan error, 1)
					mutationResult := make(chan error, 1)
					go func() {
						<-start
						if authKind == "login" {
							_, raceErr := authService.Login(ctx, &cloudv1.LoginAccountRequest{Login: provisioned.GetAccount().GetEmail(), Password: testUserPassword})
							authResult <- raceErr
							return
						}
						_, raceErr := authService.Refresh(ctx, &cloudv1.RefreshAccountSessionRequest{RefreshToken: initialLogin.GetSession().GetRefreshToken()})
						authResult <- raceErr
					}()
					go func() {
						<-start
						switch mutationKind {
						case "reset":
							setupDigest := sha256.Sum256([]byte(uuid.NewString()))
							_, raceErr := mutationDatabase.ResetAccountSetup(ctx, provisioned.GetAccount().GetAccountId(), admin.GetAccountId(), "race reset", setupDigest, now.Add(time.Hour), now)
							mutationResult <- raceErr
						case "disable":
							_, raceErr := mutationDatabase.SetAccountState(ctx, &cloudv1.SetAccountStateRequest{AccountId: provisioned.GetAccount().GetAccountId(), State: cloudv1.AccountState_ACCOUNT_STATE_DISABLED, ExpectedRevision: 2, Reason: "race disable"}, admin.GetAccountId(), now)
							mutationResult <- raceErr
						case "change-password":
							identityContext := account.ContextWithIdentity(ctx, account.Identity{Account: initialLogin.GetAccount(), Roles: initialLogin.GetRoles(), SessionID: initialLogin.GetSession().GetSessionId(), RecentAuthExpiresAt: now.Add(time.Hour)})
							_, raceErr := mutationService.ChangePassword(identityContext, &cloudv1.ChangeAccountPasswordRequest{CurrentPassword: testUserPassword, NewPassword: "changed-user-password"})
							mutationResult <- raceErr
						}
					}()
					close(start)
					if err := <-mutationResult; err != nil {
						t.Fatalf("mutation iteration %d: %v", iteration, err)
					}
					if err := <-authResult; err != nil && !errors.Is(err, account.ErrUnauthenticated) && !errors.Is(err, account.ErrLoginUnavailable) && !errors.Is(err, account.ErrAccountConflict) {
						t.Fatalf("authentication iteration %d: %v", iteration, err)
					}
					var liveSessions int
					if err := mutationDatabase.pool.QueryRow(ctx, `SELECT count(*) FROM account_sessions WHERE account_id=$1 AND revoked_at IS NULL`, provisioned.GetAccount().GetAccountId()).Scan(&liveSessions); err != nil {
						t.Fatal(err)
					}
					if liveSessions != 0 {
						t.Fatalf("iteration %d left %d live sessions", iteration, liveSessions)
					}
					assertRaceMutationState(t, ctx, mutationDatabase, provisioned.GetAccount().GetAccountId(), mutationKind)
				}
			})
		}
	}
}

func assertRaceMutationState(t *testing.T, ctx context.Context, database *Database, accountID, mutationKind string) {
	t.Helper()
	var state string
	var passwordHash, setupDigest []byte
	if err := database.pool.QueryRow(ctx, `SELECT a.state,c.password_hash,c.setup_digest FROM accounts a JOIN account_credentials c USING(account_id) WHERE a.account_id=$1`, accountID).Scan(&state, &passwordHash, &setupDigest); err != nil {
		t.Fatal(err)
	}
	switch mutationKind {
	case "reset":
		if state != "pending" || len(passwordHash) != 0 || len(setupDigest) != sha256.Size {
			t.Fatalf("reset state=%q password=%d setup=%d", state, len(passwordHash), len(setupDigest))
		}
	case "disable":
		if state != "disabled" || bcrypt.CompareHashAndPassword(passwordHash, []byte(testUserPassword)) != nil {
			t.Fatalf("disable state=%q password invalid", state)
		}
	case "change-password":
		if state != "active" || bcrypt.CompareHashAndPassword(passwordHash, []byte("changed-user-password")) != nil {
			t.Fatalf("change-password state=%q password invalid", state)
		}
	}
}

func accountLifecycleDatabase(t *testing.T) (*Database, context.Context, time.Time) {
	t.Helper()
	databaseURL := os.Getenv("ANYTTY_CLOUD_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("ANYTTY_CLOUD_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	database, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return database, ctx, time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC)
}

func accountLifecycleService(t *testing.T, database *Database, now time.Time) *account.Service {
	t.Helper()
	service, err := account.New(account.Config{
		Store: database, AccessTTL: 15 * time.Minute, RefreshTTL: 24 * time.Hour,
		RecentAuthenticationTTL: 10 * time.Minute, SetupTTL: time.Hour,
		BcryptCost: bcrypt.MinCost, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func bootstrapAccount(t *testing.T, ctx context.Context, service *account.Service, email string) *cloudv1.AccountProfile {
	t.Helper()
	profile, err := service.EnsureBootstrapOperator(ctx, email, testAdminPassword)
	if err != nil {
		t.Fatal(err)
	}
	return profile
}

func recentAdminContext(ctx context.Context, profile *cloudv1.AccountProfile, now time.Time) context.Context {
	return account.ContextWithIdentity(ctx, account.Identity{Account: profile, Roles: []cloudv1.AccountRole{cloudv1.AccountRole_ACCOUNT_ROLE_ADMIN}, SessionID: "admin-test-session", RecentAuthExpiresAt: now.Add(time.Hour)})
}

func insertActiveAccount(t *testing.T, ctx context.Context, database *Database, email string, admin bool, now time.Time) *cloudv1.AccountProfile {
	t.Helper()
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(testAdminPassword), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	roles := []cloudv1.AccountRole{cloudv1.AccountRole_ACCOUNT_ROLE_USER}
	if admin {
		roles = append(roles, cloudv1.AccountRole_ACCOUNT_ROLE_ADMIN)
	}
	profile := &cloudv1.AccountProfile{AccountId: uuid.NewString(), Email: email, DisplayName: "Database Account", State: cloudv1.AccountState_ACCOUNT_STATE_ACTIVE, Revision: 1, CreatedAt: timestamppb.New(now), UpdatedAt: timestamppb.New(now)}
	if _, err := database.EnsureBootstrapOperator(ctx, account.Record{Profile: profile, PasswordHash: passwordHash, CredentialRevision: 1, CredentialUpdatedAt: now, Roles: roles}); err != nil {
		t.Fatal(err)
	}
	return profile
}
