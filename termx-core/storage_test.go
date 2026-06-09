package termx

import (
	"errors"
	"testing"
)

func TestStoragePublicPrivateAndCAS(t *testing.T) {
	store := NewStorage()

	public, err := store.Put(StoragePutRequest{
		AppID: "tui",
		Scope: StorageScopePublic,
		Key:   "locks/copy-mode-owner",
		Value: []byte("view-a"),
	})
	if err != nil {
		t.Fatalf("put public: %v", err)
	}
	if public.Version != 1 || public.OwnerID != "" || string(public.Value) != "view-a" {
		t.Fatalf("unexpected public entry: %#v", public)
	}

	private, err := store.Put(StoragePutRequest{
		AppID:   "tui",
		Scope:   StorageScopePrivate,
		OwnerID: "client-a",
		Key:     "viewport",
		Value:   []byte("top=10"),
	})
	if err != nil {
		t.Fatalf("put private: %v", err)
	}
	if private.OwnerID != "client-a" || private.Version != 1 {
		t.Fatalf("unexpected private entry: %#v", private)
	}

	if _, err := store.Get(StorageGetRequest{
		AppID:   "tui",
		Scope:   StorageScopePrivate,
		OwnerID: "client-b",
		Key:     "viewport",
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected private owner isolation, got %v", err)
	}

	if _, err := store.Put(StoragePutRequest{
		AppID:           "tui",
		Scope:           StorageScopePublic,
		Key:             "locks/copy-mode-owner",
		Value:           []byte("view-b"),
		CheckVersion:    true,
		ExpectedVersion: 0,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected CAS conflict, got %v", err)
	}

	updated, err := store.Put(StoragePutRequest{
		AppID:           "tui",
		Scope:           StorageScopePublic,
		Key:             "locks/copy-mode-owner",
		Value:           []byte("view-b"),
		CheckVersion:    true,
		ExpectedVersion: public.Version,
	})
	if err != nil {
		t.Fatalf("cas put: %v", err)
	}
	if updated.Version != 2 || string(updated.Value) != "view-b" {
		t.Fatalf("unexpected CAS update: %#v", updated)
	}
}

func TestStorageListSortsByKeyAndFiltersPrefix(t *testing.T) {
	store := NewStorage()
	for _, key := range []string{"groups/z", "locks/owner", "groups/a"} {
		if _, err := store.Put(StoragePutRequest{
			AppID: "tui",
			Scope: StorageScopePublic,
			Key:   key,
			Value: []byte(key),
		}); err != nil {
			t.Fatalf("put %s: %v", key, err)
		}
	}

	entries, err := store.List(StorageListRequest{
		AppID:  "tui",
		Scope:  StorageScopePublic,
		Prefix: "groups/",
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 2 || entries[0].Key != "groups/a" || entries[1].Key != "groups/z" {
		t.Fatalf("unexpected list entries: %#v", entries)
	}
}

func TestStorageRequiresPrivateOwner(t *testing.T) {
	store := NewStorage()
	if _, err := store.Put(StoragePutRequest{
		AppID: "tui",
		Scope: StorageScopePrivate,
		Key:   "viewport",
	}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("expected permission error for private storage without owner, got %v", err)
	}
}
