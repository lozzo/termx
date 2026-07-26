// Package runtimepath 提供本机 daemon 与客户端共享的运行时路径策略。
package runtimepath

import (
	"os"
	"path/filepath"
	"strings"
)

// SocketPath 返回当前用户的默认本地 socket 路径。
// XDG_RUNTIME_DIR 是显式 truth；否则使用系统临时目录和平台用户标识，避免多用户争用同一 listener。
func SocketPath(name string) string {
	name = strings.TrimSpace(name)
	if runtimeDir := strings.TrimSpace(os.Getenv("XDG_RUNTIME_DIR")); runtimeDir != "" {
		return filepath.Join(runtimeDir, name)
	}
	extension := filepath.Ext(name)
	base := strings.TrimSuffix(name, extension)
	return filepath.Join(os.TempDir(), base+"-"+userDiscriminator()+extension)
}
