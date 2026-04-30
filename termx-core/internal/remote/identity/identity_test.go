package identity

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestLoadOrCreatePersistsAndReusesIdentity(t *testing.T) {
	dir := t.TempDir()

	first, err := LoadOrCreate(dir, "alpha")
	if err != nil {
		t.Fatalf("LoadOrCreate returned error: %v", err)
	}
	if first.DeviceID == "" {
		t.Fatal("expected device id to be generated")
	}
	if first.DisplayName != "alpha" {
		t.Fatalf("expected display name alpha, got %q", first.DisplayName)
	}

	second, err := LoadOrCreate(dir, "")
	if err != nil {
		t.Fatalf("LoadOrCreate second call returned error: %v", err)
	}
	if second.DeviceID != first.DeviceID {
		t.Fatalf("expected same device id, got %q != %q", second.DeviceID, first.DeviceID)
	}
	if second.DisplayName != first.DisplayName {
		t.Fatalf("expected display name to persist, got %q != %q", second.DisplayName, first.DisplayName)
	}
}

func TestLoadOrCreateUpdatesDisplayNameWhenRequested(t *testing.T) {
	dir := t.TempDir()

	original, err := LoadOrCreate(dir, "alpha")
	if err != nil {
		t.Fatalf("LoadOrCreate returned error: %v", err)
	}
	updated, err := LoadOrCreate(dir, "beta")
	if err != nil {
		t.Fatalf("LoadOrCreate second call returned error: %v", err)
	}
	if updated.DeviceID != original.DeviceID {
		t.Fatalf("expected same device id, got %q != %q", updated.DeviceID, original.DeviceID)
	}
	if updated.DisplayName != "beta" {
		t.Fatalf("expected updated display name beta, got %q", updated.DisplayName)
	}
	if _, err := LoadOrCreate(filepath.Join(dir, "nested"), "gamma"); err != nil {
		t.Fatalf("expected nested dir creation to succeed: %v", err)
	}
}

func TestLoadOrCreateMachineKeyPersistsAndReusesEd25519Key(t *testing.T) {
	dir := t.TempDir()

	first, err := LoadOrCreateMachineKey(dir)
	if err != nil {
		t.Fatalf("LoadOrCreateMachineKey returned error: %v", err)
	}
	if len(first.PublicKey) != ed25519.PublicKeySize {
		t.Fatalf("expected public key size %d, got %d", ed25519.PublicKeySize, len(first.PublicKey))
	}

	path := filepath.Join(dir, MachineKeyFilename)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected machine key file to be written: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("expected machine key file mode 0600, got %o", got)
	}

	second, err := LoadOrCreateMachineKey(dir)
	if err != nil {
		t.Fatalf("LoadOrCreateMachineKey second call returned error: %v", err)
	}
	if !first.PublicKey.Equal(second.PublicKey) {
		t.Fatal("expected persisted public key to be reused")
	}
	firstSignature := first.Sign([]byte("termx"))
	if !ed25519.Verify(second.PublicKey, []byte("termx"), firstSignature) {
		t.Fatal("expected first key signature to verify with reloaded public key")
	}
	if !ed25519.Verify(second.PublicKey, []byte("termx"), second.Sign([]byte("termx"))) {
		t.Fatal("expected reloaded keypair to sign and verify")
	}
}

func TestLoadOrCreateMachineKeyConcurrentFirstRunReturnsPersistedKey(t *testing.T) {
	dir := t.TempDir()
	const workers = 16

	var wg sync.WaitGroup
	results := make(chan MachineKey, workers)
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			key, err := LoadOrCreateMachineKey(dir)
			if err != nil {
				errs <- err
				return
			}
			results <- key
		}()
	}
	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		t.Fatalf("LoadOrCreateMachineKey returned error during concurrent first run: %v", err)
	}
	persisted, err := LoadOrCreateMachineKey(dir)
	if err != nil {
		t.Fatalf("LoadOrCreateMachineKey reload returned error: %v", err)
	}
	for key := range results {
		if !key.PublicKey.Equal(persisted.PublicKey) {
			t.Fatal("expected every concurrent caller to receive the persisted machine key")
		}
	}
}

func TestMachineKeyDoesNotMarshalPrivateKeyMaterial(t *testing.T) {
	key, err := LoadOrCreateMachineKey(t.TempDir())
	if err != nil {
		t.Fatalf("LoadOrCreateMachineKey returned error: %v", err)
	}

	payload, err := json.Marshal(key)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	text := string(payload)
	if strings.Contains(text, "private") || strings.Contains(text, "seed") {
		t.Fatalf("machine key JSON exposed private material marker: %s", text)
	}
	if strings.Contains(text, hex.EncodeToString(key.privateKey.Seed())) {
		t.Fatalf("machine key JSON exposed private seed: %s", text)
	}
}

func TestMachineKeyFormattingRedactsPrivateKeyMaterial(t *testing.T) {
	key, err := LoadOrCreateMachineKey(t.TempDir())
	if err != nil {
		t.Fatalf("LoadOrCreateMachineKey returned error: %v", err)
	}

	privateSeedHex := hex.EncodeToString(key.privateKey.Seed())
	for _, formatted := range []string{
		fmt.Sprintf("%v", key),
		fmt.Sprintf("%+v", key),
		fmt.Sprintf("%#v", key),
	} {
		if strings.Contains(formatted, "private") || strings.Contains(formatted, "seed") {
			t.Fatalf("machine key formatting exposed private material marker: %s", formatted)
		}
		if strings.Contains(formatted, privateSeedHex) {
			t.Fatalf("machine key formatting exposed private seed: %s", formatted)
		}
		if !strings.Contains(formatted, MachinePublicKeyFingerprint(key.PublicKey)) {
			t.Fatalf("machine key formatting should include public fingerprint, got: %s", formatted)
		}
	}
}

func TestMachineKeyFingerprintIsSHA256OfPublicKey(t *testing.T) {
	key, err := LoadOrCreateMachineKey(t.TempDir())
	if err != nil {
		t.Fatalf("LoadOrCreateMachineKey returned error: %v", err)
	}

	sum := sha256.Sum256(key.PublicKey)
	expected := "sha256:" + hex.EncodeToString(sum[:])
	if got := MachinePublicKeyFingerprint(key.PublicKey); got != expected {
		t.Fatalf("expected fingerprint %q, got %q", expected, got)
	}
	if !strings.HasPrefix(expected, "sha256:") {
		t.Fatalf("expected sha256 prefix, got %q", expected)
	}
}
