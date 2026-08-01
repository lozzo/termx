package postgres

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/anytty/anytty/cloud/controller/account"
	"github.com/anytty/anytty/cloud/controller/enrollment"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestConsumeDaemonEnrollmentCreatesNewIdentityAfterDelete(t *testing.T) {
	databaseURL := os.Getenv("ANYTTY_CLOUD_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("ANYTTY_CLOUD_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	database, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	accountID := uuid.NewString()
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("integration-test-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.EnsureBootstrapOperator(ctx, account.Record{
		Profile: &cloudv1.AccountProfile{
			AccountId: accountID, Email: "reenroll-" + accountID + "@example.com", DisplayName: "Re-enrollment account",
			State: cloudv1.AccountState_ACCOUNT_STATE_ACTIVE, Revision: 1, CreatedAt: timestamppb.New(now), UpdatedAt: timestamppb.New(now),
		},
		PasswordHash: passwordHash, CredentialRevision: 1, CredentialUpdatedAt: now, Roles: []cloudv1.AccountRole{cloudv1.AccountRole_ACCOUNT_ROLE_USER},
	}); err != nil {
		t.Fatal(err)
	}
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	deviceID := "device-" + uuid.NewString()
	fingerprint := "fingerprint-" + uuid.NewString()
	firstDigest := sha256.Sum256([]byte("first-" + uuid.NewString()))
	if _, err := database.CreateDaemonEnrollment(ctx, accountID, "", "First name", firstDigest[:], now.Add(time.Hour), now); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		resolvedAccountID, err := database.GetDaemonEnrollmentAccount(ctx, firstDigest[:], now)
		if err != nil || resolvedAccountID != accountID {
			t.Fatalf("resolve enrollment account=%q err=%v", resolvedAccountID, err)
		}
	}
	first, err := database.ConsumeDaemonEnrollment(ctx, firstDigest[:], deviceID, fingerprint, publicKey, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.GetDaemonEnrollmentAccount(ctx, firstDigest[:], now); !errors.Is(err, enrollment.ErrEnrollmentInvalid) {
		t.Fatalf("consumed enrollment remained readable: %v", err)
	}
	blocked, err := database.ChangeDaemonState(ctx, accountID, first.ID, cloudv1.DaemonState_DAEMON_STATE_BLOCKED, first.StateRevision, "test block", now.Add(time.Minute))
	if err != nil || blocked.State != cloudv1.DaemonState_DAEMON_STATE_BLOCKED {
		t.Fatalf("block daemon=%+v err=%v", blocked, err)
	}
	secondDigest := sha256.Sum256([]byte("second-" + uuid.NewString()))
	if _, err := database.CreateDaemonEnrollment(ctx, accountID, "", "Restored name", secondDigest[:], now.Add(time.Hour), now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ConsumeDaemonEnrollment(ctx, secondDigest[:], deviceID, fingerprint, publicKey, now.Add(2*time.Minute)); !errors.Is(err, enrollment.ErrDaemonIdentityConflict) {
		t.Fatalf("blocked daemon was replaced: %v", err)
	}
	deleted, err := database.ChangeDaemonState(ctx, accountID, first.ID, cloudv1.DaemonState_DAEMON_STATE_DELETED, blocked.StateRevision, "test delete", now.Add(3*time.Minute))
	if err != nil || deleted.State != cloudv1.DaemonState_DAEMON_STATE_DELETED {
		t.Fatalf("delete daemon=%+v err=%v", deleted, err)
	}
	restored, err := database.ConsumeDaemonEnrollment(ctx, secondDigest[:], deviceID, fingerprint, publicKey, now.Add(4*time.Minute))
	if err != nil {
		t.Fatalf("enroll after delete: %v", err)
	}
	if restored.ID == first.ID || restored.State != cloudv1.DaemonState_DAEMON_STATE_ACTIVE || restored.StateRevision != 1 || restored.DisplayName != "Restored name" {
		t.Fatalf("new daemon=%+v deleted=%+v", restored, deleted)
	}
	thirdDigest := sha256.Sum256([]byte("third-" + uuid.NewString()))
	if _, err := database.CreateDaemonEnrollment(ctx, accountID, "", "Latest name", thirdDigest[:], now.Add(time.Hour), now.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ConsumeDaemonEnrollment(ctx, thirdDigest[:], deviceID, fingerprint, publicKey, now.Add(5*time.Minute)); !errors.Is(err, enrollment.ErrDaemonIdentityConflict) {
		t.Fatalf("active daemon was replaced: %v", err)
	}
	daemons, err := database.ListDaemonsByAccount(ctx, accountID)
	if err != nil || len(daemons) != 1 || daemons[0].ID != restored.ID {
		t.Fatalf("account daemons=%+v err=%v", daemons, err)
	}
	if _, err := database.ConsumeDaemonEnrollment(ctx, secondDigest[:], deviceID, fingerprint, publicKey, now.Add(3*time.Minute)); !errors.Is(err, enrollment.ErrEnrollmentInvalid) {
		t.Fatalf("reused enrollment token error=%v", err)
	}
}

func TestConsumeDaemonEnrollmentRejectsDeviceIDKeyReplacement(t *testing.T) {
	databaseURL := os.Getenv("ANYTTY_CLOUD_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("ANYTTY_CLOUD_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	database, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	accountID := uuid.NewString()
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("integration-test-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.EnsureBootstrapOperator(ctx, account.Record{
		Profile: &cloudv1.AccountProfile{
			AccountId: accountID, Email: "identity-conflict-" + accountID + "@example.com", DisplayName: "Identity conflict account",
			State: cloudv1.AccountState_ACCOUNT_STATE_ACTIVE, Revision: 1, CreatedAt: timestamppb.New(now), UpdatedAt: timestamppb.New(now),
		},
		PasswordHash: passwordHash, CredentialRevision: 1, CredentialUpdatedAt: now, Roles: []cloudv1.AccountRole{cloudv1.AccountRole_ACCOUNT_ROLE_USER},
	}); err != nil {
		t.Fatal(err)
	}
	deviceID := "device-" + uuid.NewString()
	firstFingerprint := "fingerprint-" + uuid.NewString()
	secondFingerprint := "fingerprint-" + uuid.NewString()
	firstPublicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	secondPublicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	firstDigest := sha256.Sum256([]byte("first-" + uuid.NewString()))
	secondDigest := sha256.Sum256([]byte("second-" + uuid.NewString()))
	if _, err := database.CreateDaemonEnrollment(ctx, accountID, "", "Original", firstDigest[:], now.Add(time.Hour), now); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ConsumeDaemonEnrollment(ctx, firstDigest[:], deviceID, firstFingerprint, firstPublicKey, now); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateDaemonEnrollment(ctx, accountID, "", "Replacement", secondDigest[:], now.Add(time.Hour), now); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ConsumeDaemonEnrollment(ctx, secondDigest[:], deviceID, secondFingerprint, secondPublicKey, now); !errors.Is(err, enrollment.ErrDaemonIdentityConflict) {
		t.Fatalf("device ID key replacement error=%v", err)
	}
}
