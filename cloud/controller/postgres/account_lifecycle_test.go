package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anytty/anytty/cloud/controller/account"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
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
	type redeemResult struct {
		response *cloudv1.RedeemAccountSetupResponse
		err      error
	}
	results := make(chan redeemResult, contenders)
	for index := 0; index < contenders; index++ {
		go func() {
			<-start
			response, redeemErr := service.RedeemAccountSetup(ctx, &cloudv1.RedeemAccountSetupRequest{SetupCredential: provisioned.GetSetupCredential(), NewPassword: testUserPassword})
			results <- redeemResult{response: response, err: redeemErr}
		}()
	}
	close(start)
	succeeded := 0
	var redeemed *cloudv1.RedeemAccountSetupResponse
	for index := 0; index < contenders; index++ {
		result := <-results
		if result.err == nil {
			succeeded++
			redeemed = result.response
			continue
		}
		if !errors.Is(result.err, account.ErrSetupCredentialInvalid) {
			t.Fatalf("concurrent redeem error=%v", result.err)
		}
	}
	if succeeded != 1 {
		t.Fatalf("successful concurrent redeems=%d, want 1", succeeded)
	}
	if redeemed.GetAccount().GetState() != cloudv1.AccountState_ACCOUNT_STATE_ACTIVE || len(redeemed.GetRoles()) != 1 || redeemed.GetSession().GetSessionId() == "" || len(redeemed.GetSession().GetAccessToken()) == 0 || len(redeemed.GetSession().GetRefreshToken()) == 0 || len(redeemed.GetSession().GetCsrfToken()) == 0 {
		t.Fatalf("successful redeem did not return login-equivalent identity: %v", redeemed)
	}
	var liveSessions int
	if err := database.pool.QueryRow(ctx, `SELECT count(*) FROM account_sessions WHERE account_id=$1 AND revoked_at IS NULL`, provisioned.GetAccount().GetAccountId()).Scan(&liveSessions); err != nil {
		t.Fatal(err)
	}
	if liveSessions != 1 {
		t.Fatalf("live sessions after single redeem=%d", liveSessions)
	}
	if _, err := service.RedeemAccountSetup(ctx, &cloudv1.RedeemAccountSetupRequest{SetupCredential: provisioned.GetSetupCredential(), NewPassword: testUserPassword}); !errors.Is(err, account.ErrSetupCredentialInvalid) {
		t.Fatalf("replayed setup credential error=%v", err)
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
	if err := database.pool.QueryRow(ctx, `SELECT count(*) FROM account_sessions WHERE account_id=$1 AND revoked_at IS NULL`, provisioned.GetAccount().GetAccountId()).Scan(&liveSessions); err != nil {
		t.Fatal(err)
	}
	if liveSessions != 0 {
		t.Fatalf("live sessions after reset=%d", liveSessions)
	}
	if _, err := service.AuthenticateAccess(ctx, redeemed.GetSession().GetAccessToken()); !errors.Is(err, account.ErrUnauthenticated) {
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
	database, ctx, now := accountLifecycleDatabase(t)
	databaseURL := os.Getenv("ANYTTY_CLOUD_TEST_DATABASE_URL")
	authService := accountLifecycleService(t, database, now)
	mutationService := accountLifecycleService(t, database, now)
	admin := bootstrapAccount(t, ctx, authService, "race-admin-"+uuid.NewString()+"@example.com")
	adminContext := recentAdminContext(ctx, admin, now)

	for _, authKind := range []string{"login", "refresh"} {
		for _, mutationKind := range []string{"reset", "disable", "change-password"} {
			for _, order := range []string{"authentication-first", "mutation-first"} {
				t.Run(authKind+"-vs-"+mutationKind+"/"+order, func(t *testing.T) {
					provisioned, err := authService.ProvisionAccount(adminContext, &cloudv1.ProvisionAccountRequest{
						Email: uuid.NewString() + "@race.example.com", DisplayName: "Race User", Reason: "transaction race",
					})
					if err != nil {
						t.Fatal(err)
					}
					redeemed, err := authService.RedeemAccountSetup(ctx, &cloudv1.RedeemAccountSetupRequest{SetupCredential: provisioned.GetSetupCredential(), NewPassword: testUserPassword})
					if err != nil {
						t.Fatal(err)
					}

					runAuthentication := func(service *account.Service) error {
						if authKind == "login" {
							_, authErr := service.Login(ctx, &cloudv1.LoginAccountRequest{Login: provisioned.GetAccount().GetEmail(), Password: testUserPassword})
							return authErr
						}
						_, authErr := service.Refresh(ctx, &cloudv1.RefreshAccountSessionRequest{RefreshToken: redeemed.GetSession().GetRefreshToken()})
						return authErr
					}
					runMutation := func(store *Database, service *account.Service) error {
						switch mutationKind {
						case "reset":
							setupDigest := sha256.Sum256([]byte(uuid.NewString()))
							_, mutationErr := store.ResetAccountSetup(ctx, provisioned.GetAccount().GetAccountId(), admin.GetAccountId(), "race reset", setupDigest, now.Add(time.Hour), now)
							return mutationErr
						case "disable":
							_, mutationErr := store.SetAccountState(ctx, &cloudv1.SetAccountStateRequest{AccountId: provisioned.GetAccount().GetAccountId(), State: cloudv1.AccountState_ACCOUNT_STATE_DISABLED, ExpectedRevision: redeemed.GetAccount().GetRevision(), Reason: "race disable"}, admin.GetAccountId(), now)
							return mutationErr
						case "change-password":
							identityContext := account.ContextWithIdentity(ctx, account.Identity{Account: redeemed.GetAccount(), Roles: redeemed.GetRoles(), SessionID: redeemed.GetSession().GetSessionId(), RecentAuthExpiresAt: now.Add(time.Hour)})
							_, mutationErr := service.ChangePassword(identityContext, &cloudv1.ChangeAccountPasswordRequest{CurrentPassword: testUserPassword, NewPassword: "changed-user-password"})
							return mutationErr
						default:
							return errors.New("unknown mutation kind")
						}
					}

					gate := newAccountLockGate()
					tracer := &accountLockTracer{gate: gate, arrived: make(chan string, 1)}
					tracedDatabase := openTracedAccountDatabase(t, ctx, databaseURL, tracer)
					tracedService := accountLifecycleService(t, tracedDatabase, now)
					defer gate.release()

					if order == "authentication-first" {
						mutationResult := make(chan error, 1)
						go func() { mutationResult <- runMutation(tracedDatabase, tracedService) }()
						waitForAccountLockGate(t, ctx, tracer.arrived)
						if err := runAuthentication(authService); err != nil {
							t.Fatalf("authentication before mutation: %v", err)
						}
						gate.release()
						if err := waitForRaceResult(t, ctx, mutationResult); err != nil {
							t.Fatalf("mutation after authentication: %v", err)
						}
					} else {
						authResult := make(chan error, 1)
						go func() { authResult <- runAuthentication(tracedService) }()
						waitForAccountLockGate(t, ctx, tracer.arrived)
						if err := runMutation(database, mutationService); err != nil {
							t.Fatalf("mutation before authentication: %v", err)
						}
						gate.release()
						if err := waitForRaceResult(t, ctx, authResult); err == nil {
							t.Fatal("authentication committed after account mutation")
						}
					}

					var liveSessions int
					if err := database.pool.QueryRow(ctx, `SELECT count(*) FROM account_sessions WHERE account_id=$1 AND revoked_at IS NULL`, provisioned.GetAccount().GetAccountId()).Scan(&liveSessions); err != nil {
						t.Fatal(err)
					}
					if liveSessions != 0 {
						t.Fatalf("race left %d live sessions", liveSessions)
					}
					assertRaceMutationState(t, ctx, database, provisioned.GetAccount().GetAccountId(), mutationKind)
				})
			}
		}
	}
}

func TestActorTargetAccountLocksUseGlobalUUIDOrder(t *testing.T) {
	database, ctx, now := accountLifecycleDatabase(t)
	const lowerID = "11111111-1111-4111-8111-111111111111"
	const higherID = "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
	lower := insertActiveAccountWithID(t, ctx, database, lowerID, "lock-lower@example.com", true, now)
	higher := insertActiveAccountWithID(t, ctx, database, higherID, "lock-higher@example.com", true, now)
	databaseURL := os.Getenv("ANYTTY_CLOUD_TEST_DATABASE_URL")
	gate := newAccountLockGate()
	defer gate.release()
	leftTracer := &accountLockTracer{gate: gate, arrived: make(chan string, 1)}
	rightTracer := &accountLockTracer{gate: gate, arrived: make(chan string, 1)}
	leftDatabase := openTracedAccountDatabase(t, ctx, databaseURL, leftTracer)
	rightDatabase := openTracedAccountDatabase(t, ctx, databaseURL, rightTracer)
	results := make(chan error, 2)
	go func() {
		_, err := leftDatabase.SetAccountRole(ctx, &cloudv1.SetAccountRoleRequest{AccountId: higher.GetAccountId(), Role: cloudv1.AccountRole_ACCOUNT_ROLE_OPERATOR, Enabled: true, Reason: "lower acts on higher"}, lower.GetAccountId(), now)
		results <- err
	}()
	go func() {
		_, err := rightDatabase.SetAccountRole(ctx, &cloudv1.SetAccountRoleRequest{AccountId: lower.GetAccountId(), Role: cloudv1.AccountRole_ACCOUNT_ROLE_OPERATOR, Enabled: true, Reason: "higher acts on lower"}, higher.GetAccountId(), now)
		results <- err
	}()
	if locked := waitForAccountLockGate(t, ctx, leftTracer.arrived); locked != lowerID {
		t.Fatalf("lower-to-higher first lock=%q, want %q", locked, lowerID)
	}
	if locked := waitForAccountLockGate(t, ctx, rightTracer.arrived); locked != lowerID {
		t.Fatalf("higher-to-lower first lock=%q, want %q", locked, lowerID)
	}
	gate.release()
	for index := 0; index < 2; index++ {
		if err := waitForRaceResult(t, ctx, results); err != nil {
			t.Fatalf("reciprocal account operation %d: %v", index, err)
		}
	}
}

func TestSetAccountStatePendingAndDisableInvariants(t *testing.T) {
	database, ctx, now := accountLifecycleDatabase(t)
	service := accountLifecycleService(t, database, now)
	admin := bootstrapAccount(t, ctx, service, "state-admin-"+uuid.NewString()+"@example.com")
	adminContext := recentAdminContext(ctx, admin, now)
	pending, err := service.ProvisionAccount(adminContext, &cloudv1.ProvisionAccountRequest{Email: uuid.NewString() + "@state.example.com", DisplayName: "State User", Reason: "state invariants"})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(pending.GetSetupCredential()))
	if _, err := database.SetAccountState(ctx, &cloudv1.SetAccountStateRequest{AccountId: pending.GetAccount().GetAccountId(), State: cloudv1.AccountState_ACCOUNT_STATE_DISABLED, ExpectedRevision: 1, Reason: "must reject pending"}, admin.GetAccountId(), now); !errors.Is(err, account.ErrAccountConflict) {
		t.Fatalf("pending state transition error=%v", err)
	}
	var pendingState string
	var pendingPassword, pendingDigest []byte
	var pendingExpiry *time.Time
	if err := database.pool.QueryRow(ctx, `SELECT a.state,c.password_hash,c.setup_digest,c.setup_expires_at FROM accounts a JOIN account_credentials c USING(account_id) WHERE a.account_id=$1`, pending.GetAccount().GetAccountId()).Scan(&pendingState, &pendingPassword, &pendingDigest, &pendingExpiry); err != nil {
		t.Fatal(err)
	}
	if pendingState != "pending" || len(pendingPassword) != 0 || !bytes.Equal(pendingDigest, digest[:]) || pendingExpiry == nil {
		t.Fatalf("pending state mutated: state=%q password=%d digest=%x expiry=%v", pendingState, len(pendingPassword), pendingDigest, pendingExpiry)
	}

	redeemed, err := service.RedeemAccountSetup(ctx, &cloudv1.RedeemAccountSetupRequest{SetupCredential: pending.GetSetupCredential(), NewPassword: testUserPassword})
	if err != nil {
		t.Fatal(err)
	}
	disabled, err := database.SetAccountState(ctx, &cloudv1.SetAccountStateRequest{AccountId: redeemed.GetAccount().GetAccountId(), State: cloudv1.AccountState_ACCOUNT_STATE_DISABLED, ExpectedRevision: redeemed.GetAccount().GetRevision(), Reason: "disable active"}, admin.GetAccountId(), now)
	if err != nil {
		t.Fatal(err)
	}
	var disabledState string
	var disabledPassword, disabledDigest []byte
	var disabledExpiry *time.Time
	var liveSessions int
	if err := database.pool.QueryRow(ctx, `SELECT a.state,c.password_hash,c.setup_digest,c.setup_expires_at,(SELECT count(*) FROM account_sessions s WHERE s.account_id=a.account_id AND s.revoked_at IS NULL) FROM accounts a JOIN account_credentials c USING(account_id) WHERE a.account_id=$1`, disabled.GetAccountId()).Scan(&disabledState, &disabledPassword, &disabledDigest, &disabledExpiry, &liveSessions); err != nil {
		t.Fatal(err)
	}
	if disabledState != "disabled" || bcrypt.CompareHashAndPassword(disabledPassword, []byte(testUserPassword)) != nil || len(disabledDigest) != 0 || disabledExpiry != nil || liveSessions != 0 {
		t.Fatalf("disabled invariant state=%q password=%d digest=%d expiry=%v live_sessions=%d", disabledState, len(disabledPassword), len(disabledDigest), disabledExpiry, liveSessions)
	}
	if _, err := database.AccountByID(ctx, disabled.GetAccountId()); err != nil {
		t.Fatalf("disabled persisted record violates credential invariant: %v", err)
	}
}

func TestLoginAccountRevisionRejectsDisableRestoreABA(t *testing.T) {
	database, ctx, now := accountLifecycleDatabase(t)
	service := accountLifecycleService(t, database, now)
	admin := bootstrapAccount(t, ctx, service, "aba-admin-"+uuid.NewString()+"@example.com")
	adminContext := recentAdminContext(ctx, admin, now)
	pending, err := service.ProvisionAccount(adminContext, &cloudv1.ProvisionAccountRequest{Email: uuid.NewString() + "@aba.example.com", DisplayName: "ABA User", Reason: "account revision ABA"})
	if err != nil {
		t.Fatal(err)
	}
	redeemed, err := service.RedeemAccountSetup(ctx, &cloudv1.RedeemAccountSetupRequest{SetupCredential: pending.GetSetupCredential(), NewPassword: testUserPassword})
	if err != nil {
		t.Fatal(err)
	}
	expected, err := database.AccountByLogin(ctx, redeemed.GetAccount().GetEmail())
	if err != nil {
		t.Fatal(err)
	}
	disabled, err := database.SetAccountState(ctx, &cloudv1.SetAccountStateRequest{AccountId: expected.Profile.GetAccountId(), State: cloudv1.AccountState_ACCOUNT_STATE_DISABLED, ExpectedRevision: expected.Profile.GetRevision(), Reason: "ABA disable"}, admin.GetAccountId(), now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.SetAccountState(ctx, &cloudv1.SetAccountStateRequest{AccountId: expected.Profile.GetAccountId(), State: cloudv1.AccountState_ACCOUNT_STATE_ACTIVE, ExpectedRevision: disabled.GetRevision(), Reason: "ABA restore"}, admin.GetAccountId(), now); err != nil {
		t.Fatal(err)
	}
	// Isolate the account revision check by restoring the credential projection to its pre-disable values.
	if _, err := database.pool.Exec(ctx, `UPDATE account_credentials SET revision=$1,updated_at=$2 WHERE account_id=$3`, expected.CredentialRevision, expected.CredentialUpdatedAt, expected.Profile.GetAccountId()); err != nil {
		t.Fatal(err)
	}
	session := account.Session{
		ID: uuid.NewString(), AccountID: expected.Profile.GetAccountId(), Revision: 1, CreatedAt: now,
		AccessDigest: sha256.Sum256([]byte("aba-access")), RefreshDigest: sha256.Sum256([]byte("aba-refresh")), CSRFDigest: sha256.Sum256([]byte("aba-csrf")),
		AccessExpiresAt: now.Add(15 * time.Minute), RefreshExpiresAt: now.Add(time.Hour),
	}
	if _, err := database.CreateSession(ctx, expected, session, now); !errors.Is(err, account.ErrAccountConflict) {
		t.Fatalf("stale pre-disable login snapshot error=%v", err)
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
		if state != "disabled" || bcrypt.CompareHashAndPassword(passwordHash, []byte(testUserPassword)) != nil || len(setupDigest) != 0 {
			t.Fatalf("disable state=%q password invalid setup=%d", state, len(setupDigest))
		}
	case "change-password":
		if state != "active" || bcrypt.CompareHashAndPassword(passwordHash, []byte("changed-user-password")) != nil {
			t.Fatalf("change-password state=%q password invalid", state)
		}
	}
}

type accountLockGate struct {
	done chan struct{}
	once sync.Once
}

func newAccountLockGate() *accountLockGate {
	return &accountLockGate{done: make(chan struct{})}
}

func (gate *accountLockGate) release() {
	gate.once.Do(func() { close(gate.done) })
}

type accountLockTracer struct {
	gate    *accountLockGate
	arrived chan string
	once    sync.Once
}

func (tracer *accountLockTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if !strings.Contains(data.SQL, `FROM accounts WHERE account_id=$1 FOR UPDATE`) {
		return ctx
	}
	tracer.once.Do(func() {
		accountID, _ := data.Args[0].(string)
		tracer.arrived <- accountID
		<-tracer.gate.done
	})
	return ctx
}

func (*accountLockTracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func openTracedAccountDatabase(t *testing.T, ctx context.Context, databaseURL string, tracer pgx.QueryTracer) *Database {
	t.Helper()
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.Tracer = tracer
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	database := &Database{pool: pool}
	t.Cleanup(database.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	return database
}

func waitForAccountLockGate(t *testing.T, ctx context.Context, arrived <-chan string) string {
	t.Helper()
	select {
	case accountID := <-arrived:
		return accountID
	case <-ctx.Done():
		t.Fatalf("account lock gate was not reached: %v", ctx.Err())
		return ""
	}
}

func waitForRaceResult(t *testing.T, ctx context.Context, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		t.Fatalf("transaction race did not complete: %v", ctx.Err())
		return ctx.Err()
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
	return insertActiveAccountWithID(t, ctx, database, uuid.NewString(), email, admin, now)
}

func insertActiveAccountWithID(t *testing.T, ctx context.Context, database *Database, accountID, email string, admin bool, now time.Time) *cloudv1.AccountProfile {
	t.Helper()
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(testAdminPassword), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	roles := []cloudv1.AccountRole{cloudv1.AccountRole_ACCOUNT_ROLE_USER}
	if admin {
		roles = append(roles, cloudv1.AccountRole_ACCOUNT_ROLE_ADMIN)
	}
	profile := &cloudv1.AccountProfile{AccountId: accountID, Email: email, DisplayName: "Database Account", State: cloudv1.AccountState_ACCOUNT_STATE_ACTIVE, Revision: 1, CreatedAt: timestamppb.New(now), UpdatedAt: timestamppb.New(now)}
	if _, err := database.EnsureBootstrapOperator(ctx, account.Record{Profile: profile, PasswordHash: passwordHash, CredentialRevision: 1, CredentialUpdatedAt: now, Roles: roles}); err != nil {
		t.Fatal(err)
	}
	return profile
}
