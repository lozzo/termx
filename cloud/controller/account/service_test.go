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
		records: map[string]account.Record{"account-a": {Profile: profile, PasswordHash: hash, Roles: []cloudv1.AccountRole{cloudv1.AccountRole_ACCOUNT_ROLE_USER}}},
		sessions: []account.Session{
			{ID: "session-current", AccountID: "account-a", CreatedAt: now, AccessExpiresAt: now.Add(time.Hour), RefreshExpiresAt: now.Add(24 * time.Hour), RecentAuthExpiresAt: now.Add(10 * time.Minute), Revision: 1},
			{ID: "session-other", AccountID: "account-a", CreatedAt: now, AccessExpiresAt: now.Add(time.Hour), RefreshExpiresAt: now.Add(24 * time.Hour), Revision: 2},
			{ID: "session-b", AccountID: "account-b", CreatedAt: now, AccessExpiresAt: now.Add(time.Hour), RefreshExpiresAt: now.Add(24 * time.Hour), Revision: 1},
		},
	}
	service, err := account.New(account.Config{Store: store, AccessTTL: time.Hour, RefreshTTL: 24 * time.Hour, RecentAuthenticationTTL: 10 * time.Minute, BcryptCost: bcrypt.MinCost, Now: func() time.Time { return now }})
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
	if changed.GetAccount().GetRevision() != 2 || store.passwordAccountID != "account-a" || store.passwordKeepSessionID != "session-current" {
		t.Fatalf("password update response=%+v account=%q keep=%q", changed, store.passwordAccountID, store.passwordKeepSessionID)
	}
	if bcrypt.CompareHashAndPassword(store.passwordHash, []byte("new-password")) != nil {
		t.Fatal("new password hash does not verify")
	}
}

type accountStoreFake struct {
	records                                  map[string]account.Record
	sessions                                 []account.Session
	listedAccountID                          string
	revokedAccountID, revokedSessionID       string
	passwordAccountID, passwordKeepSessionID string
	passwordHash                             []byte
}

func (store *accountStoreFake) CreateAccount(context.Context, account.Record, account.Session) error {
	return nil
}
func (store *accountStoreFake) EnsureBootstrapOperator(_ context.Context, record account.Record) (account.Record, error) {
	return record, nil
}
func (store *accountStoreFake) AccountByLogin(context.Context, string) (account.Record, error) {
	return account.Record{}, errors.New("not found")
}
func (store *accountStoreFake) AccountByID(_ context.Context, accountID string) (account.Record, error) {
	record, ok := store.records[accountID]
	if !ok {
		return account.Record{}, errors.New("not found")
	}
	return record, nil
}
func (store *accountStoreFake) PutSession(context.Context, account.Session) error { return nil }
func (store *accountStoreFake) SessionByAccessDigest(context.Context, [sha256.Size]byte) (account.Session, error) {
	return account.Session{}, errors.New("not found")
}
func (store *accountStoreFake) SessionByRefreshDigest(context.Context, [sha256.Size]byte) (account.Session, error) {
	return account.Session{}, errors.New("not found")
}
func (store *accountStoreFake) RotateSession(context.Context, account.Session, account.Session) error {
	return nil
}
func (store *accountStoreFake) RevokeSession(_ context.Context, accountID, sessionID string, _ bool) error {
	store.revokedAccountID, store.revokedSessionID = accountID, sessionID
	return nil
}
func (store *accountStoreFake) SetRecentAuthentication(context.Context, string, time.Time) error {
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
func (store *accountStoreFake) UpdatePassword(_ context.Context, accountID, keepSessionID string, hash []byte, now time.Time) (*cloudv1.AccountProfile, error) {
	store.passwordAccountID, store.passwordKeepSessionID, store.passwordHash = accountID, keepSessionID, append([]byte(nil), hash...)
	profile := proto.Clone(store.records[accountID].Profile).(*cloudv1.AccountProfile)
	profile.Revision++
	profile.UpdatedAt = timestamppb.New(now)
	return profile, nil
}
