//go:build !windows

package installer

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// DefaultRootDir 返回当前用户固定的 versioned Cloud Companion libexec root。
// 该路径不包含账号凭据、DeviceIdentity、CapabilityGrant 或 endpoint 配置。
func DefaultRootDir() string {
	if dataHome := os.Getenv("XDG_DATA_HOME"); filepath.IsAbs(dataHome) {
		return filepath.Join(dataHome, "termx", "cloud-companion")
	}
	home, err := os.UserHomeDir()
	if err == nil {
		return filepath.Join(home, ".local", "share", "termx", "cloud-companion")
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("termx-cloud-companion-%d", os.Getuid()))
}

// ExecutableName 返回当前平台 archive 中唯一允许的 Companion executable 名称。
func ExecutableName() string { return "termx-cloud" }

func trustedFileOwner(_ string, info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Getuid())
}

func executableMode() os.FileMode { return 0o700 }

func untrustedExecutableMode(mode os.FileMode) bool {
	return mode.Perm()&0o077 != 0 || mode.Perm()&0o100 == 0
}

func untrustedPrivateMode(mode os.FileMode) bool { return mode.Perm()&0o077 != 0 }

func replaceFile(source, target string) error { return os.Rename(source, target) }

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
