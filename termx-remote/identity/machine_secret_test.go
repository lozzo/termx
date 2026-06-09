package identity_test

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/lozzow/termx/termx-remote/identity"
)

func TestLoadOrCreateMachineSecretNew(t *testing.T) {
	s, err := identity.LoadOrCreateMachineSecret(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(s) != 32 {
		t.Fatalf("want 32 bytes, got %d", len(s))
	}
}

func TestLoadOrCreateMachineSecretExisting(t *testing.T) {
	dir := t.TempDir()
	s1, err := identity.LoadOrCreateMachineSecret(dir)
	if err != nil {
		t.Fatal(err)
	}
	s2, err := identity.LoadOrCreateMachineSecret(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(s1, s2) {
		t.Fatal("different secret on reload")
	}
}

func TestLoadOrCreateMachineSecretRejectsWrongLength(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, identity.MachineSecretFilename), []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := identity.LoadOrCreateMachineSecret(dir); err == nil {
		t.Fatal("expected wrong length error")
	}
}

func TestLoadOrCreateMachineSecretConcurrentCreate(t *testing.T) {
	dir := t.TempDir()
	errs := make([]error, 10)
	secrets := make([][]byte, 10)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			secrets[i], errs[i] = identity.LoadOrCreateMachineSecret(dir)
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
	}
	for i := 1; i < len(secrets); i++ {
		if !bytes.Equal(secrets[0], secrets[i]) {
			t.Fatal("expected every concurrent caller to receive the persisted secret")
		}
	}
}

func TestLoadOrCreateMachineSecretPermissions(t *testing.T) {
	dir := t.TempDir()
	if _, err := identity.LoadOrCreateMachineSecret(dir); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, identity.MachineSecretFilename))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("want 0600, got %o", info.Mode().Perm())
	}
}
