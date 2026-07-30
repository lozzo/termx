package account_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/anytty/anytty/cloud/controller/account"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestAccountSelfServiceUsesAuthenticatedAccountAndCurrentSession(t *testing.T) {
	now := time.Unix(1_000, 0).UTC()
	hash, err := bcrypt.GenerateFromPassword([]byte("current-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	profile := &cloudv1.AccountProfile{AccountId: "account-a", Email: "a@example.com", DisplayName: "账号 A", State: cloudv1.AccountState_ACCOUNT_STATE_ACTIVE, Revision: 1, UpdatedAt: timestamppb.New(now)}
	store := &accountStoreFake{
		records: map[string]account.Record{"account-a": {Profile: profile, PasswordHash: hash, CredentialRevision: 1, Roles: []cloudv1.AccountRole{cloudv1.AccountRole_ACCOUNT_ROLE_USER}}},
		sessions: []account.Session{
			{ID: "session-current", AccountID: "account-a", CreatedAt: now, AccessExpiresAt: now.Add(time.Hour), RefreshExpiresAt: now.Add(24 * time.Hour), RecentAuthExpiresAt: now.Add(10 * time.Minute), Revision: 1},
			{ID: "session-other", AccountID: "account-a", CreatedAt: now, AccessExpiresAt: now.Add(time.Hour), RefreshExpiresAt: now.Add(24 * time.Hour), Revision: 2},
			{ID: "session-b", AccountID: "account-b", CreatedAt: now, AccessExpiresAt: now.Add(time.Hour), RefreshExpiresAt: now.Add(24 * time.Hour), Revision: 1},
		},
	}
	service, err := account.New(account.Config{Store: store, AccessTTL: time.Hour, RefreshTTL: 24 * time.Hour, RecentAuthenticationTTL: 10 * time.Minute, SetupTTL: time.Hour, BcryptCost: bcrypt.MinCost, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	ctx := account.ContextWithIdentity(context.Background(), account.Identity{Account: profile, Roles: []cloudv1.AccountRole{cloudv1.AccountRole_ACCOUNT_ROLE_USER}, SessionID: "session-current", RecentAuthExpiresAt: now.Add(time.Minute)})

	listed, err := service.ListSessions(ctx, &cloudv1.ListAccountSessionsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.GetSessions()) != 2 || listed.GetSessions()[0].GetSessionId() != "session-current" || !listed.GetSessions()[0].GetCurrent() {
		t.Fatalf("unexpected session projection: %+v", listed.GetSessions())
	}
	if store.listedAccountID != "account-a" {
		t.Fatalf("session list used account %q", store.listedAccountID)
	}

	if _, err := service.RevokeSession(ctx, &cloudv1.RevokeAccountSessionRequest{SessionId: "session-other"}); err != nil {
		t.Fatal(err)
	}
	if store.revokedAccountID != "account-a" || store.revokedSessionID != "session-other" {
		t.Fatalf("revoke scope account=%q session=%q", store.revokedAccountID, store.revokedSessionID)
	}
	if _, err := service.RevokeSession(ctx, &cloudv1.RevokeAccountSessionRequest{SessionId: "session-current"}); !errors.Is(err, account.ErrUnauthenticated) {
		t.Fatalf("current session revoke error=%v", err)
	}

	changed, err := service.ChangePassword(ctx, &cloudv1.ChangeAccountPasswordRequest{CurrentPassword: "current-password", NewPassword: "new-password"})
	if err != nil {
		t.Fatal(err)
	}
	if changed.GetAccount().GetRevision() != 2 || store.passwordAccountID != "account-a" {
		t.Fatalf("password update response=%+v account=%q", changed, store.passwordAccountID)
	}
	if bcrypt.CompareHashAndPassword(store.passwordHash, []byte("new-password")) != nil {
		t.Fatal("new password hash does not verify")
	}
}

func TestUnknownLoginPerformsDummyBcryptTimingContract(t *testing.T) {
	now := time.Unix(2_000, 0).UTC()
	hash, err := bcrypt.GenerateFromPassword([]byte("known-password"), 6)
	if err != nil {
		t.Fatal(err)
	}
	store := &accountStoreFake{loginRecords: map[string]account.Record{
		"known@example.com": {Profile: &cloudv1.AccountProfile{AccountId: "known", State: cloudv1.AccountState_ACCOUNT_STATE_ACTIVE}, PasswordHash: hash, CredentialRevision: 1},
	}}
	service, err := account.New(account.Config{Store: store, AccessTTL: time.Hour, RefreshTTL: 24 * time.Hour, RecentAuthenticationTTL: 10 * time.Minute, SetupTTL: time.Hour, BcryptCost: 6, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	measure := func(login string) time.Duration {
		start := time.Now()
		for index := 0; index < 4; index++ {
			if _, err := service.Login(context.Background(), &cloudv1.LoginAccountRequest{Login: login, Password: "wrong-password"}); !errors.Is(err, account.ErrUnauthenticated) {
				t.Fatalf("login %q error = %v", login, err)
			}
		}
		return time.Since(start)
	}
	knownDuration := measure("known@example.com")
	unknownDuration := measure("  UNKNOWN@Example.com ")
	if store.lastLogin != "unknown@example.com" {
		t.Fatalf("normalized lookup = %q", store.lastLogin)
	}
	if knownDuration > 4*unknownDuration || unknownDuration > 4*knownDuration {
		t.Fatalf("known wrong-password duration=%v unknown duration=%v", knownDuration, unknownDuration)
	}
}

func TestProvisionAccountRequiresRecentAdmin(t *testing.T) {
	now := time.Unix(3_000, 0).UTC()
	store := &accountStoreFake{}
	service, err := account.New(account.Config{Store: store, AccessTTL: time.Hour, RefreshTTL: 24 * time.Hour, RecentAuthenticationTTL: 10 * time.Minute, SetupTTL: time.Hour, BcryptCost: bcrypt.MinCost, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	profile := &cloudv1.AccountProfile{AccountId: "admin"}
	request := &cloudv1.ProvisionAccountRequest{Email: " User@Example.com ", DisplayName: " New User ", Reason: " approved request "}
	userContext := account.ContextWithIdentity(context.Background(), account.Identity{Account: profile, Roles: []cloudv1.AccountRole{cloudv1.AccountRole_ACCOUNT_ROLE_USER}, SessionID: "session", RecentAuthExpiresAt: now.Add(time.Minute)})
	if _, err := service.ProvisionAccount(userContext, request); !errors.Is(err, account.ErrForbidden) {
		t.Fatalf("non-admin provision error = %v", err)
	}
	adminContext := account.ContextWithIdentity(context.Background(), account.Identity{Account: profile, Roles: []cloudv1.AccountRole{cloudv1.AccountRole_ACCOUNT_ROLE_ADMIN}, SessionID: "session", RecentAuthExpiresAt: now.Add(time.Minute)})
	response, err := service.ProvisionAccount(adminContext, request)
	if err != nil {
		t.Fatal(err)
	}
	if response.GetAccount().GetEmail() != "user@example.com" || response.GetAccount().GetDisplayName() != "New User" || response.GetAccount().GetState() != cloudv1.AccountState_ACCOUNT_STATE_PENDING || response.GetSetupCredential() == "" || store.provisionActor != "admin" || store.provisionReason != "approved request" {
		t.Fatalf("response=%+v actor=%q", response, store.provisionActor)
	}
	if len(store.provisioned.PasswordHash) != 0 || len(store.provisioned.SetupDigest) != sha256.Size {
		t.Fatal("provision persisted password material or omitted setup digest")
	}
}

func TestBootstrapCreatesOnceAndValidatesExistingWithoutUsingCandidatePassword(t *testing.T) {
	now := time.Unix(3_500, 0).UTC()
	hash, err := bcrypt.GenerateFromPassword([]byte("persisted-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	valid := account.Record{Profile: &cloudv1.AccountProfile{AccountId: "admin", Email: "admin@example.com", State: cloudv1.AccountState_ACCOUNT_STATE_ACTIVE}, PasswordHash: hash, CredentialRevision: 1, Roles: []cloudv1.AccountRole{cloudv1.AccountRole_ACCOUNT_ROLE_USER, cloudv1.AccountRole_ACCOUNT_ROLE_ADMIN}}
	store := &accountStoreFake{loginRecords: map[string]account.Record{"admin@example.com": valid}}
	service, err := account.New(account.Config{Store: store, AccessTTL: time.Hour, RefreshTTL: 24 * time.Hour, RecentAuthenticationTTL: 10 * time.Minute, SetupTTL: time.Hour, BcryptCost: bcrypt.MinCost, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := service.EnsureBootstrapOperator(context.Background(), " ADMIN@Example.com ", "x")
	if err != nil || profile.GetAccountId() != "admin" || store.ensureCalls != 0 {
		t.Fatalf("existing bootstrap profile=%v calls=%d err=%v", profile, store.ensureCalls, err)
	}
	delete(store.loginRecords, "admin@example.com")
	created, err := service.EnsureBootstrapOperator(context.Background(), "admin@example.com", "new-bootstrap-password")
	if err != nil || created.GetState() != cloudv1.AccountState_ACCOUNT_STATE_ACTIVE || store.ensureCalls != 1 || !store.ensuredHasAdmin {
		t.Fatalf("created bootstrap profile=%v calls=%d admin=%v err=%v", created, store.ensureCalls, store.ensuredHasAdmin, err)
	}
}

func TestBootstrapRejectsInvalidExistingAccountBranches(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("persisted-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		record account.Record
	}{
		{name: "not active", record: account.Record{Profile: &cloudv1.AccountProfile{Email: "admin@example.com", State: cloudv1.AccountState_ACCOUNT_STATE_DISABLED}, PasswordHash: hash, Roles: []cloudv1.AccountRole{cloudv1.AccountRole_ACCOUNT_ROLE_ADMIN}}},
		{name: "not admin", record: account.Record{Profile: &cloudv1.AccountProfile{Email: "admin@example.com", State: cloudv1.AccountState_ACCOUNT_STATE_ACTIVE}, PasswordHash: hash, Roles: []cloudv1.AccountRole{cloudv1.AccountRole_ACCOUNT_ROLE_USER}}},
		{name: "malformed hash", record: account.Record{Profile: &cloudv1.AccountProfile{Email: "admin@example.com", State: cloudv1.AccountState_ACCOUNT_STATE_ACTIVE}, PasswordHash: []byte("bad"), Roles: []cloudv1.AccountRole{cloudv1.AccountRole_ACCOUNT_ROLE_ADMIN}}},
		{name: "email mismatch", record: account.Record{Profile: &cloudv1.AccountProfile{Email: "other@example.com", State: cloudv1.AccountState_ACCOUNT_STATE_ACTIVE}, PasswordHash: hash, Roles: []cloudv1.AccountRole{cloudv1.AccountRole_ACCOUNT_ROLE_ADMIN}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &accountStoreFake{loginRecords: map[string]account.Record{"admin@example.com": test.record}}
			service, err := account.New(account.Config{Store: store, AccessTTL: time.Hour, RefreshTTL: 24 * time.Hour, RecentAuthenticationTTL: 10 * time.Minute, SetupTTL: time.Hour, BcryptCost: bcrypt.MinCost})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.EnsureBootstrapOperator(context.Background(), "admin@example.com", "ignored-password"); err == nil {
				t.Fatal("invalid existing bootstrap account was accepted")
			}
		})
	}
}

type accountStoreFake struct {
	records                            map[string]account.Record
	sessions                           []account.Session
	listedAccountID                    string
	revokedAccountID, revokedSessionID string
	passwordAccountID                  string
	passwordHash                       []byte
	loginRecords                       map[string]account.Record
	lastLogin                          string
	provisioned                        account.Record
	provisionActor, provisionReason    string
	ensureCalls                        int
	ensuredHasAdmin                    bool
}

func (store *accountStoreFake) EnsureBootstrapOperator(_ context.Context, record account.Record) (account.Record, error) {
	store.ensureCalls++
	for _, role := range record.Roles {
		store.ensuredHasAdmin = store.ensuredHasAdmin || role == cloudv1.AccountRole_ACCOUNT_ROLE_ADMIN
	}
	return record, nil
}

func (store *accountStoreFake) ProvisionAccount(_ context.Context, record account.Record, actor, reason string, _ time.Time) error {
	store.provisioned, store.provisionActor, store.provisionReason = record, actor, reason
	return nil
}
func (store *accountStoreFake) AccountByLogin(_ context.Context, login string) (account.Record, error) {
	store.lastLogin = login
	if record, ok := store.loginRecords[login]; ok {
		return record, nil
	}
	return account.Record{}, account.ErrAccountNotFound
}
func (store *accountStoreFake) AccountByExactEmail(_ context.Context, email string) (account.Record, error) {
	if record, ok := store.loginRecords[email]; ok {
		return record, nil
	}
	return account.Record{}, account.ErrAccountNotFound
}
func (store *accountStoreFake) AccountByID(_ context.Context, accountID string) (account.Record, error) {
	record, ok := store.records[accountID]
	if !ok {
		return account.Record{}, errors.New("not found")
	}
	return record, nil
}
func (store *accountStoreFake) CreateSession(_ context.Context, record account.Record, _ account.Session, _ time.Time) (account.Record, error) {
	return record, nil
}
func (store *accountStoreFake) SessionByAccessDigest(context.Context, [sha256.Size]byte) (account.Session, error) {
	return account.Session{}, errors.New("not found")
}
func (store *accountStoreFake) SessionByRefreshDigest(context.Context, [sha256.Size]byte) (account.Session, error) {
	return account.Session{}, errors.New("not found")
}
func (store *accountStoreFake) RotateSession(context.Context, account.Session, account.Session, time.Time) (account.Record, error) {
	return account.Record{}, nil
}
func (store *accountStoreFake) RevokeSession(_ context.Context, accountID, sessionID string, _ bool) error {
	store.revokedAccountID, store.revokedSessionID = accountID, sessionID
	return nil
}
func (store *accountStoreFake) SetRecentAuthentication(context.Context, string, string, account.Record, time.Time, time.Time) error {
	return nil
}
func (store *accountStoreFake) ListAccountSessions(_ context.Context, accountID string, now time.Time) ([]account.Session, error) {
	store.listedAccountID = accountID
	var result []account.Session
	for _, session := range store.sessions {
		if session.AccountID == accountID && !session.Revoked && now.Before(session.RefreshExpiresAt) {
			result = append(result, session)
		}
	}
	return result, nil
}
func (store *accountStoreFake) UpdatePassword(_ context.Context, accountID string, _ account.Record, hash []byte, now time.Time) (*cloudv1.AccountProfile, error) {
	store.passwordAccountID, store.passwordHash = accountID, append([]byte(nil), hash...)
	profile := proto.Clone(store.records[accountID].Profile).(*cloudv1.AccountProfile)
	profile.Revision++
	profile.UpdatedAt = timestamppb.New(now)
	return profile, nil
}
func (store *accountStoreFake) RedeemAccountSetup(context.Context, [sha256.Size]byte, []byte, time.Time) (*cloudv1.AccountProfile, error) {
	return nil, account.ErrSetupCredentialInvalid
}
func (store *accountStoreFake) ResetAccountSetup(context.Context, string, string, string, [sha256.Size]byte, time.Time, time.Time) (*cloudv1.AccountProfile, error) {
	return nil, nil
}
