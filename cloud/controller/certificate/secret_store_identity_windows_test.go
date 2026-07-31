//go:build windows

package certificate

import (
	"path/filepath"
	"testing"

	"github.com/anytty/anytty/shared/securefs"
)

func TestFileSecretStoreRootAndMarkerUseProtectedDACL(t *testing.T) {
	root := filepath.Join(physicalTempDir(t), "certificates")
	store, err := NewFileSecretStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	directory, err := openDirectoryNoFollow(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := securefs.ValidatePrivateDirectoryHandle(directory); err != nil {
		_ = directory.Close()
		t.Fatalf("validate certificate root DACL: %v", err)
	}
	if err := directory.Close(); err != nil {
		t.Fatal(err)
	}
	marker, err := openFileNoFollow(filepath.Join(root, storeMarkerFile))
	if err != nil {
		t.Fatal(err)
	}
	if err := securefs.ValidatePrivateFileHandle(marker); err != nil {
		_ = marker.Close()
		t.Fatalf("validate certificate marker DACL: %v", err)
	}
	if err := marker.Close(); err != nil {
		t.Fatal(err)
	}
}
