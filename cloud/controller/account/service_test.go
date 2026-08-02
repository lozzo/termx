package account_test

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/anytty/anytty/cloud/controller/account"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestAccountSelfServiceUsesAuthenticatedAccountAndCurrentRefreshToken(t *testing.T) {
	now := time.Unix(1_000, 0).UTC()
	hash, err := bcrypt.GenerateFromPassword([]byte("current-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	profile := &cloudv1.AccountProfile{AccountId: "account-a", Email: "a@example.com", DisplayName: "账号 A", State: cloudv1.AccountState_ACCOUNT_STATE_ACTIVE, Revision: 1, UpdatedAt: timestamppb.New(now)}
	store := &accountStoreFake{
		records: map[string]account.Record{"account-a": {Profile: profile, PasswordHash: hash, CredentialRevision: 1, Roles: []cloudv1.AccountRole{cloudv1.AccountRole_ACCOUNT_ROLE_USER}}},
		refreshTokens: []account.RefreshToken{
			{ID: "refresh-current", AccountID: "account-a", CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour), RecentAuthExpiresAt: now.Add(10 * time.Minute), Revision: 1},
			{ID: "refresh-other", AccountID: "account-a", CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour), Revision: 2},
			{ID: "refresh-b", AccountID: "account-b", CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour), Revision: 1},
		},
	}
	service, err := newAccountService(account.Config{Store: store, AccessTTL: time.Hour, RefreshTTL: 24 * time.Hour, RecentAuthenticationTTL: 10 * time.Minute, SetupTTL: time.Hour, BcryptCost: bcrypt.MinCost, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	ctx := account.ContextWithIdentity(context.Background(), account.Identity{Account: profile, Roles: []cloudv1.AccountRole{cloudv1.AccountRole_ACCOUNT_ROLE_USER}, RefreshID: "refresh-current", RecentAuthExpiresAt: now.Add(time.Minute)})

	listed, err := service.ListRefreshTokens(ctx, &cloudv1.ListAccountRefreshTokensRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.GetRefreshTokens()) != 2 || listed.GetRefreshTokens()[0].GetRefreshId() != "refresh-current" || !listed.GetRefreshTokens()[0].GetCurrent() {
		t.Fatalf("unexpected refresh token projection: %+v", listed.GetRefreshTokens())
	}
	if store.listedAccountID != "account-a" {
		t.Fatalf("refresh token list used account %q", store.listedAccountID)
	}

	if _, err := service.RevokeRefreshToken(ctx, &cloudv1.RevokeAccountRefreshTokenRequest{RefreshId: "refresh-other"}); err != nil {
		t.Fatal(err)
	}
	if store.revokedAccountID != "account-a" || store.revokedRefreshID != "refresh-other" {
		t.Fatalf("revoke scope account=%q refresh=%q", store.revokedAccountID, store.revokedRefreshID)
	}
	if _, err := service.RevokeRefreshToken(ctx, &cloudv1.RevokeAccountRefreshTokenRequest{RefreshId: "refresh-current"}); !errors.Is(err, account.ErrUnauthenticated) {
		t.Fatalf("current refresh token revoke error=%v", err)
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

func TestAccessJWTAndRefreshTokenLifecycle(t *testing.T) {
	now := time.Date(2026, 8, 2, 8, 0, 0, 0, time.UTC)
	clock := now
	hash, err := bcrypt.GenerateFromPassword([]byte("current-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	profile := &cloudv1.AccountProfile{AccountId: "account-jwt", Email: "jwt@example.com", DisplayName: "JWT User", State: cloudv1.AccountState_ACCOUNT_STATE_ACTIVE, Revision: 7}
	record := account.Record{Profile: profile, PasswordHash: hash, CredentialRevision: 1, Roles: []cloudv1.AccountRole{cloudv1.AccountRole_ACCOUNT_ROLE_USER}}
	store := &accountStoreFake{records: map[string]account.Record{profile.GetAccountId(): record}, loginRecords: map[string]account.Record{profile.GetEmail(): record}}
	service, err := newAccountService(account.Config{Store: store, AccessTTL: 15 * time.Minute, RefreshTTL: 24 * time.Hour, RecentAuthenticationTTL: 10 * time.Minute, SetupTTL: time.Hour, BcryptCost: bcrypt.MinCost, Now: func() time.Time { return clock }})
	if err != nil {
		t.Fatal(err)
	}

	login, err := service.Login(context.Background(), &cloudv1.LoginAccountRequest{Login: profile.GetEmail(), Password: "current-password"})
	if err != nil {
		t.Fatal(err)
	}
	credential := login.GetCredential()
	if credential.GetAccessToken() == "" || len(credential.GetRefreshToken()) == 0 || len(store.refreshTokens) != 1 {
		t.Fatalf("login credential=%v stored=%d", credential, len(store.refreshTokens))
	}
	identity, err := service.AuthenticateAccess(context.Background(), []byte(credential.GetAccessToken()))
	if err != nil || identity.Account.GetAccountId() != profile.GetAccountId() || identity.RefreshID != credential.GetRefreshId() {
		t.Fatalf("identity=%+v err=%v", identity, err)
	}

	tampered := []byte(credential.GetAccessToken())
	signatureStart := strings.LastIndexByte(string(tampered), '.') + 1
	if tampered[signatureStart] == 'A' {
		tampered[signatureStart] = 'B'
	} else {
		tampered[signatureStart] = 'A'
	}
	if _, err := service.AuthenticateAccess(context.Background(), tampered); !errors.Is(err, account.ErrUnauthenticated) {
		t.Fatalf("tampered JWT error=%v", err)
	}

	refreshed, err := service.Refresh(context.Background(), &cloudv1.RefreshAccountTokenRequest{RefreshToken: credential.GetRefreshToken()})
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.GetCredential().GetRefreshId() == credential.GetRefreshId() || refreshed.GetCredential().GetAccessToken() == credential.GetAccessToken() {
		t.Fatal("refresh did not rotate both credentials")
	}
	if _, err := service.Refresh(context.Background(), &cloudv1.RefreshAccountTokenRequest{RefreshToken: credential.GetRefreshToken()}); !errors.Is(err, account.ErrUnauthenticated) {
		t.Fatalf("reused refresh token error=%v", err)
	}

	refreshedIdentity, err := service.AuthenticateAccess(context.Background(), []byte(refreshed.GetCredential().GetAccessToken()))
	if err != nil {
		t.Fatal(err)
	}
	logoutContext := account.ContextWithIdentity(context.Background(), refreshedIdentity)
	if _, err := service.Logout(logoutContext, &cloudv1.LogoutAccountRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Refresh(context.Background(), &cloudv1.RefreshAccountTokenRequest{RefreshToken: refreshed.GetCredential().GetRefreshToken()}); !errors.Is(err, account.ErrUnauthenticated) {
		t.Fatalf("refresh after logout error=%v", err)
	}
	if _, err := service.AuthenticateAccess(context.Background(), []byte(refreshed.GetCredential().GetAccessToken())); err != nil {
		t.Fatalf("logout must not require a per-request JWT database lookup: %v", err)
	}

	clock = now.Add(16 * time.Minute)
	if _, err := service.AuthenticateAccess(context.Background(), []byte(refreshed.GetCredential().GetAccessToken())); !errors.Is(err, account.ErrUnauthenticated) {
		t.Fatalf("expired JWT error=%v", err)
	}
}

func TestChangePasswordDistinguishesAuthenticationFromNewPasswordValidation(t *testing.T) {
	now := time.Unix(1_500, 0).UTC()
	currentHash, err := bcrypt.GenerateFromPassword([]byte("current-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	profile := &cloudv1.AccountProfile{AccountId: "account-change", State: cloudv1.AccountState_ACCOUNT_STATE_ACTIVE, Revision: 1, UpdatedAt: timestamppb.New(now)}
	newPasswordTests := []struct {
		name     string
		password string
	}{
		{name: "seven bytes", password: strings.Repeat("a", 7)},
		{name: "seventy three bytes", password: strings.Repeat("a", 73)},
		{name: "seventy three multibyte bytes", password: strings.Repeat("界", 24) + "a"},
		{name: "invalid UTF-8", password: string([]byte{0xff, 0xfe, 'a', 'b', 'c', 'd', 'e', 'f'})},
		{name: "same as current", password: "current-password"},
	}
	for _, test := range newPasswordTests {
		t.Run(test.name, func(t *testing.T) {
			store := &accountStoreFake{records: map[string]account.Record{"account-change": {Profile: profile, PasswordHash: currentHash, CredentialRevision: 1}}}
			service, err := newAccountService(account.Config{Store: store, AccessTTL: time.Hour, RefreshTTL: 24 * time.Hour, RecentAuthenticationTTL: 10 * time.Minute, SetupTTL: time.Hour, BcryptCost: bcrypt.MinCost, Now: func() time.Time { return now }})
			if err != nil {
				t.Fatal(err)
			}
			ctx := account.ContextWithIdentity(context.Background(), account.Identity{Account: profile, RefreshID: "refresh-change"})
			if _, err := service.ChangePassword(ctx, &cloudv1.ChangeAccountPasswordRequest{CurrentPassword: "current-password", NewPassword: test.password}); !errors.Is(err, account.ErrInvalidArgument) {
				t.Fatalf("new password validation error=%v", err)
			}
			if store.passwordAccountID != "" || len(store.passwordHash) != 0 {
				t.Fatal("invalid new password reached the password store")
			}
		})
	}

	store := &accountStoreFake{records: map[string]account.Record{"account-change": {Profile: profile, PasswordHash: currentHash, CredentialRevision: 1}}}
	service, err := newAccountService(account.Config{Store: store, AccessTTL: time.Hour, RefreshTTL: 24 * time.Hour, RecentAuthenticationTTL: 10 * time.Minute, SetupTTL: time.Hour, BcryptCost: bcrypt.MinCost, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	ctx := account.ContextWithIdentity(context.Background(), account.Identity{Account: profile, RefreshID: "refresh-change"})
	if _, err := service.ChangePassword(ctx, &cloudv1.ChangeAccountPasswordRequest{CurrentPassword: "wrong-password", NewPassword: strings.Repeat("a", 7)}); !errors.Is(err, account.ErrUnauthenticated) {
		t.Fatalf("wrong current password with malformed new password error=%v", err)
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
	service, err := newAccountService(account.Config{Store: store, AccessTTL: time.Hour, RefreshTTL: 24 * time.Hour, RecentAuthenticationTTL: 10 * time.Minute, SetupTTL: time.Hour, BcryptCost: 6, Now: func() time.Time { return now }})
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

func TestPasswordContractUsesUTF8Bytes(t *testing.T) {
	tests := []struct {
		name     string
		password string
		valid    bool
	}{
		{name: "seven ASCII bytes", password: strings.Repeat("a", 7)},
		{name: "eight ASCII bytes", password: strings.Repeat("a", 8), valid: true},
		{name: "seventy two ASCII bytes", password: strings.Repeat("a", 72), valid: true},
		{name: "seventy three ASCII bytes", password: strings.Repeat("a", 73)},
		{name: "seventy two multibyte bytes", password: strings.Repeat("界", 24), valid: true},
		{name: "seventy three multibyte bytes", password: strings.Repeat("界", 24) + "a"},
		{name: "invalid UTF-8", password: string([]byte{0xff, 0xfe, 'a', 'b', 'c', 'd', 'e', 'f'})},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			persistedPassword := strings.Repeat("v", 8)
			if test.valid {
				persistedPassword = test.password
			}
			hash, err := bcrypt.GenerateFromPassword([]byte(persistedPassword), bcrypt.MinCost)
			if err != nil {
				t.Fatal(err)
			}
			store := &accountStoreFake{loginRecords: map[string]account.Record{"user@example.com": {
				Profile:      &cloudv1.AccountProfile{AccountId: "account-password", State: cloudv1.AccountState_ACCOUNT_STATE_ACTIVE, Revision: 1},
				PasswordHash: hash, CredentialRevision: 1, Roles: []cloudv1.AccountRole{cloudv1.AccountRole_ACCOUNT_ROLE_USER},
			}}}
			service, err := newAccountService(account.Config{Store: store, AccessTTL: time.Hour, RefreshTTL: 24 * time.Hour, RecentAuthenticationTTL: 10 * time.Minute, SetupTTL: time.Hour, BcryptCost: bcrypt.MinCost})
			if err != nil {
				t.Fatal(err)
			}
			response, err := service.Login(context.Background(), &cloudv1.LoginAccountRequest{Login: "user@example.com", Password: test.password})
			if test.valid && (err != nil || response.GetCredential() == nil) {
				t.Fatalf("valid password rejected: response=%v err=%v", response, err)
			}
			if !test.valid && !errors.Is(err, account.ErrUnauthenticated) {
				t.Fatalf("invalid password error=%v", err)
			}
		})
	}
}

func TestProvisionAccountRequiresRecentAdmin(t *testing.T) {
	now := time.Unix(3_000, 0).UTC()
	store := &accountStoreFake{}
	service, err := newAccountService(account.Config{Store: store, AccessTTL: time.Hour, RefreshTTL: 24 * time.Hour, RecentAuthenticationTTL: 10 * time.Minute, SetupTTL: time.Hour, BcryptCost: bcrypt.MinCost, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	profile := &cloudv1.AccountProfile{AccountId: "admin"}
	request := &cloudv1.ProvisionAccountRequest{Email: " User@Example.com ", DisplayName: " New User ", Reason: " approved request "}
	userContext := account.ContextWithIdentity(context.Background(), account.Identity{Account: profile, Roles: []cloudv1.AccountRole{cloudv1.AccountRole_ACCOUNT_ROLE_USER}, RefreshID: "refresh", RecentAuthExpiresAt: now.Add(time.Minute)})
	if _, err := service.ProvisionAccount(userContext, request); !errors.Is(err, account.ErrForbidden) {
		t.Fatalf("non-admin provision error = %v", err)
	}
	adminContext := account.ContextWithIdentity(context.Background(), account.Identity{Account: profile, Roles: []cloudv1.AccountRole{cloudv1.AccountRole_ACCOUNT_ROLE_ADMIN}, RefreshID: "refresh", RecentAuthExpiresAt: now.Add(time.Minute)})
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
	service, err := newAccountService(account.Config{Store: store, AccessTTL: time.Hour, RefreshTTL: 24 * time.Hour, RecentAuthenticationTTL: 10 * time.Minute, SetupTTL: time.Hour, BcryptCost: bcrypt.MinCost, Now: func() time.Time { return now }})
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
			service, err := newAccountService(account.Config{Store: store, AccessTTL: time.Hour, RefreshTTL: 24 * time.Hour, RecentAuthenticationTTL: 10 * time.Minute, SetupTTL: time.Hour, BcryptCost: bcrypt.MinCost})
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
	refreshTokens                      []account.RefreshToken
	listedAccountID                    string
	revokedAccountID, revokedRefreshID string
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

func newAccountService(config account.Config) (*account.Service, error) {
	config.AccessSigningKey = ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	config.AccessSigningKeyID = "account-test-key"
	config.AccessIssuer = "https://cloud.example.test"
	config.AccessAudience = "anytty-cloud-web"
	return account.New(config)
}

func (store *accountStoreFake) CreateRefreshToken(_ context.Context, record account.Record, refresh account.RefreshToken, _ time.Time) (account.Record, error) {
	store.refreshTokens = append(store.refreshTokens, refresh)
	return record, nil
}
func (store *accountStoreFake) RefreshTokenByDigest(_ context.Context, digest [sha256.Size]byte) (account.RefreshToken, error) {
	for _, refresh := range store.refreshTokens {
		if refresh.TokenDigest == digest {
			return refresh, nil
		}
	}
	return account.RefreshToken{}, errors.New("not found")
}
func (store *accountStoreFake) RotateRefreshToken(_ context.Context, previous, next account.RefreshToken, _ time.Time) (account.Record, error) {
	for index := range store.refreshTokens {
		if store.refreshTokens[index].ID == previous.ID {
			store.refreshTokens[index].Revoked = true
		}
	}
	store.refreshTokens = append(store.refreshTokens, next)
	return store.records[previous.AccountID], nil
}
func (store *accountStoreFake) RevokeRefreshToken(_ context.Context, accountID, refreshID string, all bool) error {
	store.revokedAccountID, store.revokedRefreshID = accountID, refreshID
	for index := range store.refreshTokens {
		if store.refreshTokens[index].AccountID == accountID && (all || store.refreshTokens[index].ID == refreshID) {
			store.refreshTokens[index].Revoked = true
		}
	}
	return nil
}
func (store *accountStoreFake) SetRecentAuthentication(context.Context, string, string, account.Record, time.Time, time.Time) error {
	return nil
}
func (store *accountStoreFake) ListAccountRefreshTokens(_ context.Context, accountID string, now time.Time) ([]account.RefreshToken, error) {
	store.listedAccountID = accountID
	var result []account.RefreshToken
	for _, refresh := range store.refreshTokens {
		if refresh.AccountID == accountID && !refresh.Revoked && now.Before(refresh.ExpiresAt) {
			result = append(result, refresh)
		}
	}
	return result, nil
}
func (store *accountStoreFake) UpdatePassword(_ context.Context, accountID, _ string, _ account.Record, hash []byte, now time.Time) (*cloudv1.AccountProfile, error) {
	store.passwordAccountID, store.passwordHash = accountID, append([]byte(nil), hash...)
	profile := proto.Clone(store.records[accountID].Profile).(*cloudv1.AccountProfile)
	profile.Revision++
	profile.UpdatedAt = timestamppb.New(now)
	return profile, nil
}
func (store *accountStoreFake) RedeemAccountSetup(context.Context, [sha256.Size]byte, []byte, account.RefreshToken, time.Time) (account.Record, error) {
	return account.Record{}, account.ErrSetupCredentialInvalid
}
func (store *accountStoreFake) ResetAccountSetup(context.Context, string, string, string, [sha256.Size]byte, time.Time, time.Time) (*cloudv1.AccountProfile, error) {
	return nil, nil
}
