package directory_test

import (
	"crypto/ed25519"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/muxvia/muxvia/private/cloud/control-plane/directory"
	"github.com/muxvia/muxvia/private/cloud/control-plane/domain"
)

func TestPersistentSecurityDirectoryRestoresDeviceKeyAndRevocation(t *testing.T) {
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "security-directory.json")
	store, err := directory.OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PutAccount(domain.Account{ID: "account-1", DisplayName: "Account", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.PutUser(domain.User{ID: "user-1", AccountID: "account-1", Email: "user@example.test", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	publicKey, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RegisterDevice(domain.DeviceRegistration{ID: "daemon-1", AccountID: "account-1", OwnerUserID: "user-1", Kind: domain.DeviceKindDaemon, Label: "Daemon", PublicKey: publicKey, Fingerprint: "sha256:test", RegisteredAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.RevokeDevice("account-1", "daemon-1", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("security directory mode = (%v, %v)", info, err)
	}
	restarted, err := directory.OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	device, err := restarted.Device("account-1", "daemon-1")
	if err != nil {
		t.Fatal(err)
	}
	if device.RevokedAt == nil || !device.RevokedAt.Equal(now.Add(time.Minute)) || !ed25519.PublicKey(device.PublicKey).Equal(publicKey) {
		t.Fatalf("restored device = %#v", device)
	}
}

func TestEnrollDaemonRestoresRevokedIdentityAndMovesOwnership(t *testing.T) {
	now := time.Date(2026, 7, 15, 11, 0, 0, 0, time.UTC)
	store := directory.NewStore()
	for _, account := range []domain.Account{{ID: "account-a", CreatedAt: now}, {ID: "account-b", CreatedAt: now}} {
		if err := store.PutAccount(account); err != nil {
			t.Fatal(err)
		}
	}
	for _, user := range []domain.User{{ID: "user-a", AccountID: "account-a", CreatedAt: now}, {ID: "user-b", AccountID: "account-b", CreatedAt: now}} {
		if err := store.PutUser(user); err != nil {
			t.Fatal(err)
		}
	}
	publicKey, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	original := domain.DeviceRegistration{ID: "daemon", AccountID: "account-a", OwnerUserID: "user-a", Kind: domain.DeviceKindDaemon, Label: "old", PublicKey: publicKey, Fingerprint: "fingerprint", RegisteredAt: now}
	if err := store.RegisterDevice(original); err != nil {
		t.Fatal(err)
	}
	if err := store.RevokeDevice("account-a", "daemon", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	reenrolled := domain.DeviceRegistration{ID: "daemon", AccountID: "account-b", OwnerUserID: "user-b", Kind: domain.DeviceKindDaemon, Label: "new", PublicKey: publicKey, Fingerprint: "fingerprint", RegisteredAt: now.Add(2 * time.Minute)}
	previous, existed, err := store.EnrollDaemon(reenrolled)
	if err != nil || !existed || previous.AccountID != "account-a" || previous.RevokedAt == nil {
		t.Fatalf("previous enrollment = (%#v, %v, %v)", previous, existed, err)
	}
	current, err := store.Device("account-b", "daemon")
	if err != nil || current.RevokedAt != nil || current.Label != "new" {
		t.Fatalf("current enrollment = (%#v, %v)", current, err)
	}
	wrongKey, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	reenrolled.PublicKey = wrongKey
	if _, _, err := store.EnrollDaemon(reenrolled); !errors.Is(err, directory.ErrConflict) {
		t.Fatalf("wrong-key reenrollment error = %v", err)
	}
}

func TestStoreRequiresDeviceOwnershipAndCreatesManagedSession(t *testing.T) {
	now := time.Date(2026, 7, 11, 8, 0, 0, 0, time.UTC)
	store := directory.NewStore()
	if err := store.PutOrganization(domain.Organization{ID: "org-a", DisplayName: "Org A", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.PutAccount(domain.Account{ID: "account-a", OrganizationID: "org-a", DisplayName: "A", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.PutAccount(domain.Account{ID: "account-b", DisplayName: "B", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.PutUser(domain.User{ID: "user-a", AccountID: "account-a", Email: "a@example.test", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.PutUser(domain.User{ID: "user-b", AccountID: "account-b", Email: "b@example.test", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}

	register := func(device domain.DeviceRegistration) {
		t.Helper()
		if err := store.RegisterDevice(device); err != nil {
			t.Fatal(err)
		}
	}
	register(domain.DeviceRegistration{ID: "client-a", AccountID: "account-a", OwnerUserID: "user-a", Kind: domain.DeviceKindClient, PublicKey: make([]byte, 32), Fingerprint: "client-fingerprint", RegisteredAt: now})
	register(domain.DeviceRegistration{ID: "daemon-a", AccountID: "account-a", OwnerUserID: "user-a", Kind: domain.DeviceKindDaemon, PublicKey: make([]byte, 32), Fingerprint: "daemon-fingerprint", RegisteredAt: now})
	register(domain.DeviceRegistration{ID: "daemon-b", AccountID: "account-b", OwnerUserID: "user-b", Kind: domain.DeviceKindDaemon, PublicKey: make([]byte, 32), Fingerprint: "other-fingerprint", RegisteredAt: now})

	if _, err := store.Device("account-b", "daemon-a"); !errors.Is(err, directory.ErrOwnership) {
		t.Fatalf("cross-account device query error = %v, want ownership error", err)
	}
	session := domain.ManagedSession{
		ID:             "managed-1",
		AccountID:      "account-a",
		ClientDeviceID: "client-a",
		TargetDeviceID: "daemon-a",
		Hub:            domain.HubAssignment{HubID: "hub-eu-1", Region: "eu-west"},
		CreatedAt:      now,
		ExpiresAt:      now.Add(time.Hour),
	}
	if err := store.CreateManagedSession(session, now); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.ManagedSession("account-a", session.ID, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if loaded != session {
		t.Fatalf("loaded session = %#v, want %#v", loaded, session)
	}

	session.ID = "managed-cross-account"
	session.TargetDeviceID = "daemon-b"
	if err := store.CreateManagedSession(session, now); !errors.Is(err, directory.ErrOwnership) {
		t.Fatalf("cross-account session error = %v, want ownership error", err)
	}
}

func TestStoreCopiesDevicePublicKey(t *testing.T) {
	now := time.Date(2026, 7, 11, 8, 0, 0, 0, time.UTC)
	store := directory.NewStore()
	_ = store.PutAccount(domain.Account{ID: "account", CreatedAt: now})
	_ = store.PutUser(domain.User{ID: "user", AccountID: "account", CreatedAt: now})
	publicKey := make([]byte, 32)
	copy(publicKey, []byte("public-key"))
	if err := store.RegisterDevice(domain.DeviceRegistration{ID: "device", AccountID: "account", OwnerUserID: "user", Kind: domain.DeviceKindClient, PublicKey: publicKey, Fingerprint: "fingerprint", RegisteredAt: now}); err != nil {
		t.Fatal(err)
	}
	publicKey[0] = 'X'
	loaded, err := store.Device("account", "device")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.PublicKey[0] != 'p' {
		t.Fatalf("stored public key prefix = %q, want copied bytes", loaded.PublicKey[:10])
	}
}
