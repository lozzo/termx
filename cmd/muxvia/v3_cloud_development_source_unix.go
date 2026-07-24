//go:build !windows

package main

import (
	"errors"
	"os"
	"syscall"
)

// ensureV3DevelopmentCompanionDirectory 创建并复验当前用户独占的内嵌 Companion 释放目录。
func ensureV3DevelopmentCompanionDirectory(path string) error {
	if info, err := os.Lstat(path); err == nil {
		return validateV3DevelopmentCompanionDirectory(info)
	} else if !errors.Is(err, os.ErrNotExist) {
		return companionDevelopmentTrustError("embedded development Cloud Companion directory cannot be inspected")
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return companionDevelopmentTrustError("embedded development Cloud Companion directory cannot be created")
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return companionDevelopmentTrustError("embedded development Cloud Companion directory permissions cannot be set")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return companionDevelopmentTrustError("embedded development Cloud Companion directory is untrusted")
	}
	return validateV3DevelopmentCompanionDirectory(info)
}

func validateV3DevelopmentCompanionDirectory(info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return companionDevelopmentTrustError("embedded development Cloud Companion directory is untrusted")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Geteuid() {
		return companionDevelopmentTrustError("embedded development Cloud Companion directory is not owned by the current user")
	}
	return nil
}

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
