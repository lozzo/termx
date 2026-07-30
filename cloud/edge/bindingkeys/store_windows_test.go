//go:build windows

package bindingkeys

import (
	"crypto/ed25519"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anytty/anytty/shared/securefs"
)

func TestOpenRejectsUnprotectedWindowsCacheDirectory(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "unprotected-state")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(filepath.Join(directory, "binding.pb")); err == nil {
		t.Fatal("unprotected cache directory was accepted")
	}
}

func TestPublishedCacheUsesPrivateWindowsDACL(t *testing.T) {
	path := privateCachePath(t)
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := store.Update(bundle(1, now, time.Hour, "key-a", make([]byte, ed25519.PublicKeySize))); err != nil {
		t.Fatal(err)
	}
	file, err := openBundleFileNoFollow(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := securefs.ValidatePrivateFileHandle(file); err != nil {
		t.Fatalf("published cache DACL: %v", err)
	}
}
