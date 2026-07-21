//go:build windows

package main

import "os"

// validateV3DevelopmentCompanionFile 固定 Windows 测试套件的本地文件类型边界；内容身份仍由 SHA-256 验证。
func validateV3DevelopmentCompanionFile(info os.FileInfo) error {
	if !info.Mode().IsRegular() {
		return companionDevelopmentTrustError("development Cloud Companion is not a regular file")
	}
	return nil
}
