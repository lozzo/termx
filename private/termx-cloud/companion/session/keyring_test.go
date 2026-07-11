package session

import (
	"context"
	"errors"
	"testing"

	keyring "github.com/zalando/go-keyring"
)

func TestKeyringStoreRoundTripAndNoFallback(t *testing.T) {
	backend := &fakeKeyring{values: make(map[string]string)}
	store := &KeyringStore{service: "io.termx.cloud", backend: backend}
	secret := []byte("opaque-session")
	if err := store.StoreSecret(context.Background(), "profile/account/v1", secret); err != nil {
		t.Fatal(err)
	}
	secret[0] = 'X'
	loaded, err := store.LoadSecret(context.Background(), "profile/account/v1")
	if err != nil || string(loaded) != "opaque-session" {
		t.Fatalf("LoadSecret = (%q, %v)", loaded, err)
	}
	if err := store.DeleteSecret(context.Background(), "profile/account/v1"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadSecret(context.Background(), "profile/account/v1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing LoadSecret error = %v", err)
	}
	backend.setErr = errors.New("credential manager unavailable")
	if err := store.StoreSecret(context.Background(), "profile/account/v1", []byte("secret")); err == nil {
		t.Fatal("backend failure must not fall back")
	}
}

type fakeKeyring struct {
	values map[string]string
	setErr error
}

func (backend *fakeKeyring) Get(service, user string) (string, error) {
	value, ok := backend.values[service+"/"+user]
	if !ok {
		return "", keyring.ErrNotFound
	}
	return value, nil
}

func (backend *fakeKeyring) Set(service, user, value string) error {
	if backend.setErr != nil {
		return backend.setErr
	}
	backend.values[service+"/"+user] = value
	return nil
}

func (backend *fakeKeyring) Delete(service, user string) error {
	key := service + "/" + user
	if _, ok := backend.values[key]; !ok {
		return keyring.ErrNotFound
	}
	delete(backend.values, key)
	return nil
}
