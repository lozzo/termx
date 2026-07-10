package session_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lozzow/termx/private/termx-cloud/companion/session"
)

type credentialStore struct {
	mu      sync.Mutex
	secrets map[string][]byte
}

func newCredentialStore() *credentialStore {
	return &credentialStore{secrets: make(map[string][]byte)}
}

func (store *credentialStore) LoadSecret(_ context.Context, key string) ([]byte, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	value, ok := store.secrets[key]
	if !ok {
		return nil, session.ErrNotFound
	}
	return append([]byte(nil), value...), nil
}

func (store *credentialStore) StoreSecret(_ context.Context, key string, value []byte) error {
	store.mu.Lock()
	store.secrets[key] = append([]byte(nil), value...)
	store.mu.Unlock()
	return nil
}

func (store *credentialStore) DeleteSecret(_ context.Context, key string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, ok := store.secrets[key]; !ok {
		return session.ErrNotFound
	}
	delete(store.secrets, key)
	return nil
}

func TestManagerSeparatesAccountAndDeviceCredentialSlots(t *testing.T) {
	now := time.Date(2026, 7, 11, 9, 0, 0, 0, time.UTC)
	store := newCredentialStore()
	manager, err := session.NewManager(store, "default")
	if err != nil {
		t.Fatal(err)
	}
	account, err := session.New(session.Metadata{Kind: session.KindAccount, AccountID: "account-1", AccountLabel: "Alice", DeviceID: "client-1", ExpiresAt: now.Add(time.Hour)}, []byte("account-access-token"), now)
	if err != nil {
		t.Fatal(err)
	}
	device, err := session.New(session.Metadata{Kind: session.KindDevice, AccountID: "account-1", DeviceID: "daemon-1", ExpiresAt: now.Add(2 * time.Hour)}, []byte("device-cloud-token"), now)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Save(context.Background(), account, now); err != nil {
		t.Fatal(err)
	}
	if err := manager.Save(context.Background(), device, now); err != nil {
		t.Fatal(err)
	}
	if len(store.secrets) != 2 {
		t.Fatalf("credential slots = %d, want account and device", len(store.secrets))
	}
	loadedAccount, err := manager.Load(context.Background(), session.KindAccount, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	loadedDevice, err := manager.Load(context.Background(), session.KindDevice, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if string(loadedAccount.Authorization().Bytes()) != "account-access-token" || string(loadedDevice.Authorization().Bytes()) != "device-cloud-token" {
		t.Fatal("credential slots crossed authorization values")
	}
	if strings.Contains(loadedAccount.String(), "account-access-token") || strings.Contains(loadedAccount.Authorization().String(), "account-access-token") {
		t.Fatal("session String leaked account token")
	}
	if err := manager.Delete(context.Background(), session.KindAccount); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Load(context.Background(), session.KindAccount, now); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("deleted account load error = %v", err)
	}
	if _, err := manager.Load(context.Background(), session.KindDevice, now); err != nil {
		t.Fatalf("account logout deleted daemon enrollment: %v", err)
	}
}

func TestManagerRejectsExpiredAndMalformedCredentialStoreValues(t *testing.T) {
	now := time.Date(2026, 7, 11, 9, 0, 0, 0, time.UTC)
	store := newCredentialStore()
	manager, _ := session.NewManager(store, "default")
	account, _ := session.New(session.Metadata{Kind: session.KindAccount, AccountID: "account", ExpiresAt: now.Add(time.Minute)}, []byte("token"), now)
	if err := manager.Save(context.Background(), account, now); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Load(context.Background(), session.KindAccount, now.Add(2*time.Minute)); !errors.Is(err, session.ErrExpired) {
		t.Fatalf("expired load error = %v", err)
	}
	store.mu.Lock()
	store.secrets["default/account/v1"] = []byte(`{"version":1,"kind":"account","account_id":"account","expires_at_unix":9999999999,"access_token":"dG9rZW4=","unknown":true}`)
	store.mu.Unlock()
	if _, err := manager.Load(context.Background(), session.KindAccount, now); !errors.Is(err, session.ErrInvalid) {
		t.Fatalf("unknown field error = %v", err)
	}
}
