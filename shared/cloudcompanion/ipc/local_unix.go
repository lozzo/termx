//go:build !windows

package ipc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/muxvia/muxvia/proto/cloudpb"
	"github.com/muxvia/muxvia/shared/cloudcompanion"
)

// DefaultEndpoint 返回当前用户固定的 Cloud Companion Unix socket 路径。
// 路径只由受信任的本地平台规则决定，不能来自 Hub、endpoint 配置或下载 manifest。
func DefaultEndpoint() string {
	if runtimeDir := strings.TrimSpace(os.Getenv("XDG_RUNTIME_DIR")); runtimeDir != "" && filepath.IsAbs(runtimeDir) {
		return filepath.Join(runtimeDir, "muxvia", "cloud-companion.sock")
	}
	cacheDir, err := os.UserCacheDir()
	if err == nil && filepath.IsAbs(cacheDir) {
		return filepath.Join(cacheDir, "muxvia", "run", "cloud-companion.sock")
	}
	return filepath.Join(os.TempDir(), "muxvia-"+strconv.Itoa(os.Getuid()), "cloud-companion.sock")
}

// Listen 创建 owner-only 的固定 Cloud Companion Unix socket listener。
// 启动锁串行化活跃探测、stale socket 回收和 bind；已运行的 Companion 是唯一运行真值，后启动进程不得覆盖它。
func Listen(endpoint string) (net.Listener, error) {
	endpoint = resolvedEndpoint(endpoint)
	if !filepath.IsAbs(endpoint) {
		return nil, fmt.Errorf("Cloud Companion socket path must be absolute")
	}
	if err := ensurePrivateDirectory(filepath.Dir(endpoint)); err != nil {
		return nil, err
	}
	unlock, err := lockCompanionStartup(endpoint + ".lock")
	if err != nil {
		return nil, err
	}
	defer unlock()
	if info, err := os.Lstat(endpoint); err == nil {
		if info.Mode()&os.ModeSocket == 0 || !ownedByCurrentUser(info) {
			return nil, fmt.Errorf("existing Cloud Companion endpoint is not a trusted owner socket")
		}
		probe, dialErr := net.DialTimeout("unix", endpoint, 250*time.Millisecond)
		if dialErr == nil {
			_ = probe.Close()
			return nil, fmt.Errorf("Cloud Companion endpoint is already active")
		}
		if err := os.Remove(endpoint); err != nil {
			return nil, fmt.Errorf("remove stale Cloud Companion socket: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect Cloud Companion socket: %w", err)
	}
	listener, err := net.Listen("unix", endpoint)
	if err != nil {
		return nil, fmt.Errorf("listen on Cloud Companion socket: %w", err)
	}
	if unixListener, ok := listener.(*net.UnixListener); ok {
		// 路径只能由 removingListener 的 inode 校验后删除，避免旧实例关闭时误删新实例的 endpoint。
		unixListener.SetUnlinkOnClose(false)
	}
	if err := os.Chmod(endpoint, 0o600); err != nil {
		_ = listener.Close()
		_ = os.Remove(endpoint)
		return nil, fmt.Errorf("secure Cloud Companion socket: %w", err)
	}
	info, err := os.Lstat(endpoint)
	if err != nil {
		_ = listener.Close()
		_ = os.Remove(endpoint)
		return nil, fmt.Errorf("inspect bound Cloud Companion socket: %w", err)
	}
	return &removingListener{Listener: listener, path: endpoint, bound: info}, nil
}

func lockCompanionStartup(path string) (func(), error) {
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() || !ownedByCurrentUser(info) || info.Mode().Perm()&0o077 != 0 {
			return nil, fmt.Errorf("Cloud Companion startup lock is untrusted")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect Cloud Companion startup lock: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open Cloud Companion startup lock: %w", err)
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || !ownedByCurrentUser(info) || info.Mode().Perm()&0o077 != 0 {
		_ = file.Close()
		return nil, fmt.Errorf("Cloud Companion startup lock is untrusted")
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("lock Cloud Companion startup: %w", err)
	}
	return func() {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
	}, nil
}

func dialLocal(ctx context.Context, endpoint string) (net.Conn, error) {
	endpoint = resolvedEndpoint(endpoint)
	info, err := os.Lstat(endpoint)
	if errors.Is(err, os.ErrNotExist) {
		return nil, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_MISSING, "Cloud Companion is not installed or has no local endpoint")
	}
	if err != nil {
		return nil, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_UNTRUSTED, "Cloud Companion endpoint cannot be inspected")
	}
	if info.Mode()&os.ModeSocket == 0 || info.Mode().Perm()&0o077 != 0 || !ownedByCurrentUser(info) {
		return nil, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_UNTRUSTED, "Cloud Companion endpoint owner or permissions are untrusted")
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", endpoint)
	if err != nil {
		return nil, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_NOT_RUNNING, "Cloud Companion is installed but not accepting local connections")
	}
	return conn, nil
}

func resolvedEndpoint(endpoint string) string {
	if endpoint = strings.TrimSpace(endpoint); endpoint != "" {
		return endpoint
	}
	return DefaultEndpoint()
}

func ensurePrivateDirectory(path string) error {
	if info, err := os.Stat(path); err == nil {
		if !info.IsDir() || !ownedByCurrentUser(info) || info.Mode().Perm()&0o077 != 0 {
			return fmt.Errorf("Cloud Companion runtime directory is not owner-only")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect Cloud Companion runtime directory: %w", err)
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("create Cloud Companion runtime directory: %w", err)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return fmt.Errorf("secure Cloud Companion runtime directory: %w", err)
	}
	return nil
}

func ownedByCurrentUser(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Getuid())
}

type removingListener struct {
	net.Listener
	path  string
	bound os.FileInfo
	once  sync.Once
}

func (listener *removingListener) Close() error {
	var closeErr error
	listener.once.Do(func() {
		closeErr = listener.Listener.Close()
		if current, err := os.Lstat(listener.path); err == nil && os.SameFile(current, listener.bound) {
			_ = os.Remove(listener.path)
		}
	})
	return closeErr
}
