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
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestConsumeDaemonEnrollmentReactivatesSameDeviceIdentity(t *testing.T) {
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
	if _, err := database.EnsureBootstrapOperator(ctx, account.Record{
		Profile: &cloudv1.AccountProfile{
			AccountId: accountID, Email: "reenroll-" + accountID + "@example.com", DisplayName: "Re-enrollment account",
			State: cloudv1.AccountState_ACCOUNT_STATE_ACTIVE, Revision: 1, CreatedAt: timestamppb.New(now), UpdatedAt: timestamppb.New(now),
		},
		PasswordHash: []byte("integration-test-hash"), Roles: []cloudv1.AccountRole{cloudv1.AccountRole_ACCOUNT_ROLE_USER},
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
	first, err := database.ConsumeDaemonEnrollment(ctx, firstDigest[:], deviceID, fingerprint, publicKey, now)
	if err != nil {
		t.Fatal(err)
	}
	revoked, err := database.RevokeDaemon(ctx, accountID, first.ID, first.Revision, "test re-enrollment", now.Add(time.Minute))
	if err != nil || !revoked.Revoked {
		t.Fatalf("revoke daemon=%+v err=%v", revoked, err)
	}
	secondDigest := sha256.Sum256([]byte("second-" + uuid.NewString()))
	if _, err := database.CreateDaemonEnrollment(ctx, accountID, "", "Restored name", secondDigest[:], now.Add(time.Hour), now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	restored, err := database.ConsumeDaemonEnrollment(ctx, secondDigest[:], deviceID, fingerprint, publicKey, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if restored.ID != first.ID || restored.Revoked || restored.Revision != revoked.Revision+1 || restored.DisplayName != "Restored name" || !restored.CreatedAt.Equal(first.CreatedAt) {
		t.Fatalf("restored daemon=%+v first=%+v revoked=%+v", restored, first, revoked)
	}
	thirdDigest := sha256.Sum256([]byte("third-" + uuid.NewString()))
	if _, err := database.CreateDaemonEnrollment(ctx, accountID, "", "Latest name", thirdDigest[:], now.Add(time.Hour), now.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	latest, err := database.ConsumeDaemonEnrollment(ctx, thirdDigest[:], deviceID, fingerprint, publicKey, now.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if latest.ID != first.ID || latest.Revoked || latest.Revision != restored.Revision+1 || latest.DisplayName != "Latest name" {
		t.Fatalf("latest daemon=%+v restored=%+v", latest, restored)
	}
	daemons, err := database.ListDaemonsByAccount(ctx, accountID)
	if err != nil || len(daemons) != 1 || daemons[0].ID != first.ID {
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
	if _, err := database.EnsureBootstrapOperator(ctx, account.Record{
		Profile: &cloudv1.AccountProfile{
			AccountId: accountID, Email: "identity-conflict-" + accountID + "@example.com", DisplayName: "Identity conflict account",
			State: cloudv1.AccountState_ACCOUNT_STATE_ACTIVE, Revision: 1, CreatedAt: timestamppb.New(now), UpdatedAt: timestamppb.New(now),
		},
		PasswordHash: []byte("integration-test-hash"), Roles: []cloudv1.AccountRole{cloudv1.AccountRole_ACCOUNT_ROLE_USER},
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
