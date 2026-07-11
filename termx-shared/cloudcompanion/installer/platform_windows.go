//go:build windows

package installer

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// DefaultRootDir 返回当前 Windows user-scoped LocalAppData 下的 Companion version root。
func DefaultRootDir() string {
	if localAppData := os.Getenv("LOCALAPPDATA"); filepath.IsAbs(localAppData) {
		return filepath.Join(localAppData, "TermX", "CloudCompanion")
	}
	directory, _ := os.UserCacheDir()
	return filepath.Join(directory, "TermX", "CloudCompanion")
}

// ExecutableName 返回 Windows archive 中唯一允许的 Companion executable 名称。
func ExecutableName() string { return "termx-cloud.exe" }

func trustedFileOwner(path string, _ os.FileInfo) bool {
	descriptor, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil || descriptor == nil || !descriptor.IsValid() {
		return false
	}
	owner, _, err := descriptor.Owner()
	if err != nil || owner == nil || !owner.IsValid() {
		return false
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	return err == nil && user != nil && user.User.Sid != nil && owner.Equals(user.User.Sid)
}

func executableMode() os.FileMode { return 0o700 }

func untrustedExecutableMode(os.FileMode) bool { return false }

func untrustedPrivateMode(os.FileMode) bool { return false }

func replaceFile(source, target string) error {
	return windows.MoveFileEx(windows.StringToUTF16Ptr(source), windows.StringToUTF16Ptr(target), windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}

func syncDirectory(string) error { return nil }
