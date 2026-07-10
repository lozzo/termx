package directory_test

import (
	"errors"
	"testing"
	"time"

	"github.com/lozzow/termx/private/termx-cloud/control-plane/directory"
	"github.com/lozzow/termx/private/termx-cloud/control-plane/domain"
)

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
