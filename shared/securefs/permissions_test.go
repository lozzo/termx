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
