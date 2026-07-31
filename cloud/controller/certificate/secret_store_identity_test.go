package certificate

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/anytty/anytty/shared/securefs"
	"github.com/google/uuid"
)

func TestNewFileSecretStoreRejectsFilesystemRootWithoutMutation(t *testing.T) {
	volumeRoot := filepath.Clean(filepath.VolumeName(physicalTempDir(t)) + string(os.PathSeparator))
	before, err := os.Lstat(volumeRoot)
	if err != nil {
		t.Fatal(err)
	}
	if store, err := NewFileSecretStore(volumeRoot); err == nil {
		_ = store.Close()
		t.Fatal("filesystem root was accepted as the certificate store")
	}
	after, err := os.Lstat(volumeRoot)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) || before.Mode() != after.Mode() {
		t.Fatal("filesystem root metadata changed after rejection")
	}
}

func TestNewFileSecretStoreRequiresExistingUnlinkedParent(t *testing.T) {
	base := physicalTempDir(t)
	missingParent := filepath.Join(base, "missing", "certificates")
	if store, err := NewFileSecretStore(missingParent); err == nil {
		_ = store.Close()
		t.Fatal("missing parent was created")
	}
	if _, err := os.Lstat(filepath.Join(base, "missing")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing parent changed: %v", err)
	}

	realParent := filepath.Join(base, "real-parent")
	if err := os.Mkdir(realParent, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(base, "parent-alias")
	if err := os.Symlink(realParent, alias); err != nil {
		t.Skipf("directory symlink unavailable: %v", err)
	}
	if store, err := NewFileSecretStore(filepath.Join(alias, "certificates")); err == nil {
		_ = store.Close()
		t.Fatal("symlink parent was accepted")
	}
	if _, err := os.Lstat(filepath.Join(realParent, "certificates")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("symlink target was changed: %v", err)
	}
}

func TestNewFileSecretStoreDoesNotAdoptUnmarkedDirectoryOrUUIDData(t *testing.T) {
	root := filepath.Join(physicalTempDir(t), "certificates")
	createPrivateDirectoryForTest(t, root)
	reference := uuid.NewString()
	secret := filepath.Join(root, reference)
	createPrivateDirectoryForTest(t, secret)
	sentinel := filepath.Join(secret, certificateFile)
	writePrivateFileForTest(t, sentinel, []byte("not owned by certificate store"))
	writePrivateFileForTest(t, filepath.Join(secret, privateKeyFile), []byte("external key"))

	if store, err := NewFileSecretStore(root); err == nil {
		_ = store.Close()
		t.Fatal("unmarked existing directory was adopted")
	}
	payload, err := os.ReadFile(sentinel)
	if err != nil || string(payload) != "not owned by certificate store" {
		t.Fatalf("fake UUID data changed: payload=%q err=%v", payload, err)
	}
	if _, err := os.Lstat(filepath.Join(root, storeMarkerFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("marker was added to an existing directory: %v", err)
	}
}

func TestNewFileSecretStoreRejectsWrongMarkerWithoutRepair(t *testing.T) {
	root := filepath.Join(physicalTempDir(t), "certificates")
	createPrivateDirectoryForTest(t, root)
	marker := filepath.Join(root, storeMarkerFile)
	wrong := []byte("anytty-certificate-store-v2\n")
	writePrivateFileForTest(t, marker, wrong)
	if store, err := NewFileSecretStore(root); err == nil {
		_ = store.Close()
		t.Fatal("incorrect marker version was accepted")
	}
	payload, err := os.ReadFile(marker)
	if err != nil || !bytes.Equal(payload, wrong) {
		t.Fatalf("wrong marker was modified: payload=%q err=%v", payload, err)
	}
}

func TestNewFileSecretStorePersistsMarkerAndReopensExistingRoot(t *testing.T) {
	root := filepath.Join(physicalTempDir(t), "certificates")
	store, err := NewFileSecretStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Reconcile(nil); err != nil {
		t.Fatalf("marker was not ignored by Reconcile: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(filepath.Join(root, storeMarkerFile))
	if err != nil || string(payload) != storeMarkerText {
		t.Fatalf("persisted marker payload=%q err=%v", payload, err)
	}
	reopened, err := NewFileSecretStore(root)
	if err != nil {
		t.Fatalf("reopen correctly marked root: %v", err)
	}
	defer reopened.Close()
	if !sameFilesystemPath(reopened.root, root) {
		t.Fatalf("stored physical root=%q want=%q", reopened.root, root)
	}
}

func TestNewFileSecretStoreRejectsFinalSymlinkWithoutChangingTarget(t *testing.T) {
	base := physicalTempDir(t)
	target := filepath.Join(base, "external")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(target, "sentinel")
	if err := os.WriteFile(sentinel, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "certificates")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("directory symlink unavailable: %v", err)
	}
	before, err := os.Lstat(target)
	if err != nil {
		t.Fatal(err)
	}
	if store, err := NewFileSecretStore(link); err == nil {
		_ = store.Close()
		t.Fatal("final directory symlink was accepted")
	}
	after, err := os.Lstat(target)
	if err != nil {
		t.Fatal(err)
	}
	payload, readErr := os.ReadFile(sentinel)
	if !os.SameFile(before, after) || before.Mode() != after.Mode() || readErr != nil || string(payload) != "outside" {
		t.Fatalf("final symlink target changed: payload=%q readErr=%v", payload, readErr)
	}
}

func TestFileSecretStoreReconcileDoesNotFollowUUIDSymlink(t *testing.T) {
	base := physicalTempDir(t)
	root := filepath.Join(base, "certificates")
	store, err := NewFileSecretStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	external := filepath.Join(base, "external")
	if err := os.Mkdir(external, 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(external, "sentinel")
	if err := os.WriteFile(sentinel, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(root, uuid.NewString())); err != nil {
		t.Skipf("directory symlink unavailable: %v", err)
	}
	if err := store.Reconcile(nil); err == nil {
		t.Fatal("UUID symlink was accepted by Reconcile")
	}
	payload, err := os.ReadFile(sentinel)
	if err != nil || string(payload) != "outside" {
		t.Fatalf("external UUID target changed: payload=%q err=%v", payload, err)
	}
}

func createPrivateDirectoryForTest(t *testing.T, path string) {
	t.Helper()
	directory, err := securefs.CreatePrivateDirectory(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := directory.Close(); err != nil {
		t.Fatal(err)
	}
}

func writePrivateFileForTest(t *testing.T, path string, payload []byte) {
	t.Helper()
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := securefs.SecureFile(path); err != nil {
		t.Fatal(err)
	}
}
