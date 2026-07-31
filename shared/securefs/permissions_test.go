package securefs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSecureFileAndDirectoryArePrivate(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "private")
	if err := os.Mkdir(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := SecureDirectory(dir); err != nil {
		t.Fatalf("secure directory: %v", err)
	}
	path := filepath.Join(dir, "secret")
	if err := os.WriteFile(path, []byte("secret"), 0o666); err != nil {
		t.Fatal(err)
	}
	if err := SecureFile(path); err != nil {
		t.Fatalf("secure file: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !IsPrivateFile(path, info) {
		t.Fatal("secured file did not satisfy the platform privacy contract")
	}
}

func TestCreatePrivateDirectoryDoesNotAdoptExistingPath(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "create-only")
	handle, err := CreatePrivateDirectory(directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidatePrivateDirectoryHandle(handle); err != nil {
		_ = handle.Close()
		t.Fatal(err)
	}
	if err := handle.Close(); err != nil {
		t.Fatal(err)
	}
	if adopted, err := CreatePrivateDirectory(directory); err == nil {
		_ = adopted.Close()
		t.Fatal("create-only private directory adopted an existing path")
	}
}
