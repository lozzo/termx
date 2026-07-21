package session_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/muxvia/muxvia/private/cloud/companion/session"
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
	account, err := session.New(session.Metadata{Kind: session.KindAccount, AccountID: "account-1", AccountLabel: "Alice", DeviceID: "client-1", ExpiresAt: now.Add(time.Hour), HubID: "hub-1", HubURL: "https://hub.example.test", HubRegion: "eu-1", HubDirectoryVersion: 2}, []byte("account-access-token"), now)
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
	if loadedAccount.Metadata().HubDirectoryVersion != 2 || loadedAccount.Metadata().HubID != "hub-1" {
		t.Fatalf("cached HubDirectory metadata = %#v", loadedAccount.Metadata())
	}
	rollback, err := session.New(session.Metadata{Kind: session.KindAccount, AccountID: "account-1", DeviceID: "client-1", ExpiresAt: now.Add(2 * time.Hour), HubID: "hub-1", HubURL: "https://hub.example.test", HubRegion: "eu-1", HubDirectoryVersion: 1}, []byte("rollback-token"), now)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Save(context.Background(), rollback, now); !errors.Is(err, session.ErrInvalid) {
		t.Fatalf("HubDirectory rollback save error = %v", err)
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
	store.secrets["default/account/v2"] = []byte(`{"version":2,"kind":"account","account_id":"account","expires_at_unix":9999999999,"access_token":"dG9rZW4=","unknown":true}`)
	store.mu.Unlock()
	if _, err := manager.Load(context.Background(), session.KindAccount, now); !errors.Is(err, session.ErrInvalid) {
		t.Fatalf("unknown field error = %v", err)
	}
}

func TestManagerLoadsRefreshAfterAccessExpiry(t *testing.T) {
	now := time.Date(2026, 7, 15, 11, 0, 0, 0, time.UTC)
	store := newCredentialStore()
	manager, err := session.NewManager(store, "refresh-profile")
	if err != nil {
		t.Fatal(err)
	}
	cloudSession, err := session.NewRefreshable(session.Metadata{Kind: session.KindAccount, AccountID: "account-1", DeviceID: "client-1", ExpiresAt: now.Add(time.Minute)}, []byte("short-edge-token"), bytes.Repeat([]byte{0x71}, 32), now.Add(time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Save(context.Background(), cloudSession, now); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Load(context.Background(), session.KindAccount, now.Add(2*time.Minute)); !errors.Is(err, session.ErrExpired) {
		t.Fatalf("expired access load error = %v", err)
	}
	refresh, err := manager.LoadRefreshAuthorization(context.Background(), session.KindAccount, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	defer refresh.Destroy()
	if len(refresh.Bytes()) != 32 || refresh.Metadata().DeviceID != "client-1" {
		t.Fatalf("refresh authorization metadata = %#v", refresh.Metadata())
	}
}
