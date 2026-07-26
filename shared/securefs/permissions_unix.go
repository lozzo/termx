//go:build unix

// Package securefs 统一私有状态文件的平台权限边界。
package securefs

import (
	"os"
	"syscall"
)

// SecureDirectory 把私有状态目录限制为当前 Unix 账号可访问。
// 调用方仍拥有目录内容真值；权限设置失败必须停止读取或写入秘密。
func SecureDirectory(path string) error { return os.Chmod(path, 0o700) }

// SecureFile 把私钥、credential 或 runtime record 限制为当前 Unix 账号可读写。
// 该函数不创建文件，也不替代调用方的原子发布和 Sync 边界。
func SecureFile(path string) error { return os.Chmod(path, 0o600) }

// IsPrivateFile 验证文件由当前 Unix 账号拥有且 group/other 没有权限。
// 元数据缺失或平台 owner 信息不可用时返回 false，调用方必须 fail closed。
func IsPrivateFile(_ string, info os.FileInfo) bool {
	if info == nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(stat.Uid) == os.Geteuid()
}
