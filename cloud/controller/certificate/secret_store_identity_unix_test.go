//go:build unix

package certificate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewFileSecretStoreRejectsWideExistingRootWithoutChmod(t *testing.T) {
	root := filepath.Join(physicalTempDir(t), "certificates")
	createPrivateDirectoryForTest(t, root)
	writePrivateFileForTest(t, filepath.Join(root, storeMarkerFile), []byte(storeMarkerText))
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if store, err := NewFileSecretStore(root); err == nil {
		_ = store.Close()
		t.Fatal("group-accessible existing root was accepted")
	}
	info, err := os.Lstat(root)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("existing root mode was repaired to %#o", info.Mode().Perm())
	}
}

func TestFileSecretStoreFailsClosedAfterAncestorPathRetarget(t *testing.T) {
	base := physicalTempDir(t)
	parent := filepath.Join(base, "owner")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(parent, "certificates")
	store, err := NewFileSecretStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	reference, err := store.Put([]byte("certificate"), []byte("private key"))
	if err != nil {
		t.Fatal(err)
	}
	movedParent := filepath.Join(base, "original-owner")
	if err := os.Rename(parent, movedParent); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(base, "external")
	if err := os.Mkdir(external, 0o700); err != nil {
		t.Fatal(err)
	}
	externalRoot := filepath.Join(external, "certificates")
	externalStore, err := NewFileSecretStore(externalRoot)
	if err != nil {
		t.Fatal(err)
	}
	externalReference, err := externalStore.Put([]byte("external certificate"), []byte("external private key"))
	if err != nil {
		t.Fatal(err)
	}
	if err := externalStore.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, parent); err != nil {
		t.Fatal(err)
	}
	if err := store.Reconcile(nil); err == nil {
		t.Fatal("retargeted ancestor path was accepted")
	}
	if _, err := os.Lstat(filepath.Join(movedParent, "certificates", reference)); err != nil {
		t.Fatalf("original physical store was modified: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(externalRoot, externalReference)); err != nil {
		t.Fatalf("retargeted external store was modified: %v", err)
	}
}
