package remoteauth

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCredentialStoreResolvesGrantRefWithoutExposingGrantInRegistry(t *testing.T) {
	store := NewCredentialStore(t.TempDir())
	if err := store.Put("lab-grant", "termx-grant-v1.payload.key.signature"); err != nil {
		t.Fatalf("put grant: %v", err)
	}
	grant, err := store.Resolve("lab-grant")
	if err != nil {
		t.Fatalf("resolve grant: %v", err)
	}
	if grant != "termx-grant-v1.payload.key.signature" {
		t.Fatalf("unexpected grant %q", grant)
	}
	info, err := os.Stat(filepath.Join(store.dir, credentialFileName("lab-grant")))
	if err != nil {
		t.Fatalf("stat credential: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("credential permissions = %o, want 600", info.Mode().Perm())
	}
}

func TestCredentialStoreRejectsUnsafeOrMissingGrantRef(t *testing.T) {
	store := NewCredentialStore(t.TempDir())
	for _, ref := range []string{"", "../escape", "with space", "/absolute"} {
		if err := store.Put(ref, "grant"); err == nil {
			t.Fatalf("expected unsafe grant ref %q rejection", ref)
		}
	}
	if _, err := store.Resolve("missing"); err == nil {
		t.Fatal("missing grant ref must fail")
	}
}

func TestRevocationStorePersistsAcrossReload(t *testing.T) {
	dir := t.TempDir()
	store, err := LoadRevocationStore(dir)
	if err != nil {
		t.Fatalf("load revocations: %v", err)
	}
	if err := store.Revoke("grant-1"); err != nil {
		t.Fatalf("revoke grant: %v", err)
	}
	reloaded, err := LoadRevocationStore(dir)
	if err != nil {
		t.Fatalf("reload revocations: %v", err)
	}
	if !reloaded.Revoked("grant-1") {
		t.Fatal("revocation did not persist across daemon restart")
	}
}
