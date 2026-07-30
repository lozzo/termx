//go:build windows

package securefs

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestValidatePrivateFileHandleRejectsAdditionalAllowACE(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := SecureDirectory(directory); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "state")
	if err := os.WriteFile(path, []byte("state"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SecureFile(path); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidatePrivateFileHandle(file); err != nil {
		_ = file.Close()
		t.Fatalf("validate secured handle: %v", err)
	}
	_ = file.Close()

	current, _, _, err := privateWindowsSIDs()
	if err != nil {
		t.Fatal(err)
	}
	descriptor, err := windows.SecurityDescriptorFromString(fmt.Sprintf("D:P(A;;FA;;;%s)(A;;FA;;;SY)(A;;FA;;;BA)(A;;FR;;;WD)", current.String()))
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
	file, err = os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := ValidatePrivateFileHandle(file); err == nil {
		t.Fatal("additional Everyone allow ACE was accepted")
	}
}
