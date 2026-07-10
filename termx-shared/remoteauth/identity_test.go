package remoteauth

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateIdentityKeepsStableFingerprintAndPrivatePermissions(t *testing.T) {
	dir := t.TempDir()
	first, err := LoadOrCreateIdentity(dir, "device-1")
	if err != nil {
		t.Fatalf("create identity: %v", err)
	}
	second, err := LoadOrCreateIdentity(dir, "device-1")
	if err != nil {
		t.Fatalf("reload identity: %v", err)
	}
	if first.DeviceID != "device-1" || first.Fingerprint == "" || second.Fingerprint != first.Fingerprint {
		t.Fatalf("identity is not stable: first=%#v second=%#v", first, second)
	}
	info, err := os.Stat(filepath.Join(dir, identityPrivateKeyFile))
	if err != nil {
		t.Fatalf("stat private key: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("private key permissions = %o, want 600", info.Mode().Perm())
	}
}

func TestLoadOrCreateIdentityRejectsDifferentDeviceID(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadOrCreateIdentity(dir, "device-1"); err != nil {
		t.Fatalf("create identity: %v", err)
	}
	if _, err := LoadOrCreateIdentity(dir, "device-2"); err == nil {
		t.Fatal("identity store must not silently replace device identity")
	}
}
