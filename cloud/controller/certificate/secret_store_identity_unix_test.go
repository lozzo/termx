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

func TestNewFileSecretStoreRejectsWideImmediateParentWithoutMutation(t *testing.T) {
	base := physicalTempDir(t)
	parent := filepath.Join(base, "wide-parent")
	if err := os.Mkdir(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(parent, "certificates")
	if store, err := NewFileSecretStore(root); err == nil {
		_ = store.Close()
		t.Fatal("group-accessible immediate parent was accepted")
	}
	info, err := os.Lstat(parent)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("immediate parent mode was repaired to %#o", info.Mode().Perm())
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("root was created under unsafe parent: %v", err)
	}
}

func TestPinnedManagedDirectorySurvivesCheckThenReplacement(t *testing.T) {
	root := filepath.Join(physicalTempDir(t), "certificates")
	store, err := NewFileSecretStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	reference, err := store.Put([]byte("original certificate"), []byte("original private key"))
	if err != nil {
		t.Fatal(err)
	}
	directory, exists, err := openManagedDirectory(store.root, reference, true)
	if err != nil || !exists {
		t.Fatalf("pin managed directory: exists=%v err=%v", exists, err)
	}
	defer directory.Close()
	moved := filepath.Join(root, ".checked-original")
	if err := os.Rename(filepath.Join(root, reference), moved); err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(root, reference)
	createPrivateDirectoryForTest(t, replacement)
	writePrivateFileForTest(t, filepath.Join(replacement, certificateFile), []byte("replacement certificate"))
	writePrivateFileForTest(t, filepath.Join(replacement, privateKeyFile), []byte("replacement private key"))
	certificatePEM, err := readPrivateFile(directory, certificateFile)
	if err != nil {
		t.Fatal(err)
	}
	privateKeyPEM, err := readPrivateFile(directory, privateKeyFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(certificatePEM) != "original certificate" || string(privateKeyPEM) != "original private key" {
		t.Fatalf("pinned read followed replacement: certificate=%q key=%q", certificatePEM, privateKeyPEM)
	}
	replacementCertificate, err := os.ReadFile(filepath.Join(replacement, certificateFile))
	if err != nil || string(replacementCertificate) != "replacement certificate" {
		t.Fatalf("replacement was modified: payload=%q err=%v", replacementCertificate, err)
	}
}

func TestFileSecretStoreKeepsPinnedRootAfterAncestorRetarget(t *testing.T) {
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
	certificatePEM, privateKeyPEM, err := store.Read(reference)
	if err != nil || string(certificatePEM) != "certificate" || string(privateKeyPEM) != "private key" {
		t.Fatalf("pinned store read changed after retarget: certificate=%q key=%q err=%v", certificatePEM, privateKeyPEM, err)
	}
	if _, err := os.Lstat(filepath.Join(movedParent, "certificates", reference)); err != nil {
		t.Fatalf("original pinned store was not used: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(externalRoot, externalReference)); err != nil {
		t.Fatalf("retargeted external store was modified: %v", err)
	}
}
