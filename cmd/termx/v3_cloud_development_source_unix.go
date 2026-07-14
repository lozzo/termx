//go:build !windows

package main

import (
	"os"
	"syscall"
)

// validateV3DevelopmentCompanionFile 固定 Unix 测试套件的本地文件信任边界。
func validateV3DevelopmentCompanionFile(info os.FileInfo) error {
	if info.Mode().Perm()&0o111 == 0 || info.Mode().Perm()&0o022 != 0 {
		return invalidDevelopmentCompanionMode(info.Mode())
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Geteuid() {
		return companionDevelopmentTrustError("development Cloud Companion is not owned by the current user")
	}
	return nil
}
