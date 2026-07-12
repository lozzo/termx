package hub

import (
	"fmt"
	"os"
	"path/filepath"
)

// EdgeSnapshotStore 持久化已经由 EdgeAuthorizer 验签的原始签名快照。
// Load 返回后必须重新验签，磁盘内容不能直接成为内存授权真值。
type EdgeSnapshotStore interface {
	Save([]byte) error
	Load() ([]byte, error)
}

// FileEdgeSnapshotStore 使用临时文件、fsync 和 rename 原子保存单个签名快照。
// 它不保存 presence、signaling、EdgeManagedSession 或 capability/data-plane 内容。
type FileEdgeSnapshotStore struct{ path string }

// NewFileEdgeSnapshotStore 创建固定文件路径的快照 store。
func NewFileEdgeSnapshotStore(path string) (*FileEdgeSnapshotStore, error) {
	if path == "" || filepath.Base(path) == "." {
		return nil, fmt.Errorf("edge snapshot path is required")
	}
	return &FileEdgeSnapshotStore{path: path}, nil
}

// Save 以 0600 权限原子替换当前签名快照，并在 rename 前同步文件内容。
func (store *FileEdgeSnapshotStore) Save(encoded []byte) error {
	if store == nil || store.path == "" || len(encoded) == 0 {
		return fmt.Errorf("edge snapshot is required")
	}
	directory := filepath.Dir(store.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create edge snapshot directory: %w", err)
	}
	file, err := os.CreateTemp(directory, ".edge-snapshot-*")
	if err != nil {
		return fmt.Errorf("create edge snapshot: %w", err)
	}
	temporary := file.Name()
	committed := false
	defer func() {
		_ = file.Close()
		if !committed {
			_ = os.Remove(temporary)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return err
	}
	if _, err := file.Write(encoded); err != nil {
		return fmt.Errorf("write edge snapshot: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync edge snapshot: %w", err)
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporary, store.path); err != nil {
		return fmt.Errorf("publish edge snapshot: %w", err)
	}
	committed = true
	return nil
}

// Load 读取原始签名快照并限制为 16 MiB；caller 必须重新验签和检查 expiry/revision。
func (store *FileEdgeSnapshotStore) Load() ([]byte, error) {
	if store == nil || store.path == "" {
		return nil, fmt.Errorf("edge snapshot store is unavailable")
	}
	info, err := os.Stat(store.path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 16<<20 {
		return nil, fmt.Errorf("edge snapshot file is invalid")
	}
	return os.ReadFile(store.path)
}
