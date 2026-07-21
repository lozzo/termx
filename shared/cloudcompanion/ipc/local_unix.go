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

	"github.com/muxvia/muxvia/proto/cloudpb"
	"github.com/muxvia/muxvia/shared/cloudcompanion"
)

// DefaultEndpoint 返回当前用户固定的 Cloud Companion Unix socket 路径。
// 路径只由受信任的本地平台规则决定，不能来自 Hub、endpoint 配置或下载 manifest。
func DefaultEndpoint() string {
	if runtimeDir := strings.TrimSpace(os.Getenv("XDG_RUNTIME_DIR")); runtimeDir != "" && filepath.IsAbs(runtimeDir) {
		return filepath.Join(runtimeDir, "termx", "cloud-companion.sock")
	}
	cacheDir, err := os.UserCacheDir()
	if err == nil && filepath.IsAbs(cacheDir) {
		return filepath.Join(cacheDir, "termx", "run", "cloud-companion.sock")
	}
	return filepath.Join(os.TempDir(), "termx-"+strconv.Itoa(os.Getuid()), "cloud-companion.sock")
}

// Listen 创建 owner-only 的固定 Cloud Companion Unix socket listener。
// 已存在路径只有在它是当前用户拥有的 socket 时才会删除，禁止覆盖 symlink、普通文件或其他用户 socket。
func Listen(endpoint string) (net.Listener, error) {
	endpoint = resolvedEndpoint(endpoint)
	if !filepath.IsAbs(endpoint) {
		return nil, fmt.Errorf("Cloud Companion socket path must be absolute")
	}
	if err := ensurePrivateDirectory(filepath.Dir(endpoint)); err != nil {
		return nil, err
	}
	if info, err := os.Lstat(endpoint); err == nil {
		if info.Mode()&os.ModeSocket == 0 || !ownedByCurrentUser(info) {
			return nil, fmt.Errorf("existing Cloud Companion endpoint is not a trusted owner socket")
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
	if err := os.Chmod(endpoint, 0o600); err != nil {
		_ = listener.Close()
		_ = os.Remove(endpoint)
		return nil, fmt.Errorf("secure Cloud Companion socket: %w", err)
	}
	return &removingListener{Listener: listener, path: endpoint}, nil
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
	path string
	once sync.Once
}

func (listener *removingListener) Close() error {
	var closeErr error
	listener.once.Do(func() {
		closeErr = listener.Listener.Close()
		_ = os.Remove(listener.path)
	})
	return closeErr
}
