package main

import (
	"os"
	"path/filepath"
)

// v3RemoteCredentialDir 返回 CLI host adapter 的 owner-only credential 目录。
// 它只提供平台路径，不解析 credential、不建立连接，也不持有 client runtime 状态。
func v3RemoteCredentialDir() string {
	if stateHome := os.Getenv("XDG_STATE_HOME"); stateHome != "" {
		return filepath.Join(stateHome, "muxvia", "remote-v2", "credentials")
	}
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		return filepath.Join(home, ".local", "state", "muxvia", "remote-v2", "credentials")
	}
	return filepath.Join(os.TempDir(), "muxvia-state", "remote-v2", "credentials")
}
