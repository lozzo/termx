//go:build windows

package main

import (
	"errors"
	"os"
)

// ensureV3DevelopmentCompanionDirectory 创建 Windows 当前用户的内嵌 Companion 释放目录。
func ensureV3DevelopmentCompanionDirectory(path string) error {
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return companionDevelopmentTrustError("embedded development Cloud Companion directory is untrusted")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return companionDevelopmentTrustError("embedded development Cloud Companion directory cannot be inspected")
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return companionDevelopmentTrustError("embedded development Cloud Companion directory cannot be created")
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return companionDevelopmentTrustError("embedded development Cloud Companion directory is untrusted")
	}
	return nil
}

// validateV3DevelopmentCompanionFile 固定 Windows 测试套件的本地文件类型边界；内容身份仍由 SHA-256 验证。
func validateV3DevelopmentCompanionFile(info os.FileInfo) error {
	if !info.Mode().IsRegular() {
		return companionDevelopmentTrustError("development Cloud Companion is not a regular file")
	}
	return nil
}
