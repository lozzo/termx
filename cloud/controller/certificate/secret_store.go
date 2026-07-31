package certificate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/anytty/anytty/shared/filepublish"
	"github.com/google/uuid"
)

const (
	certificateFile = "fullchain.pem"
	privateKeyFile  = "privkey.pem"
	deletePrefix    = ".delete-"
	pendingPrefix   = ".pending-"
)

// FileSecretStore 在 Controller 本机仅服务用户可访问的目录保存当前证书材料。
// 每次 Put 都创建不可猜测的新引用，数据库提交后才由业务层淘汰旧引用。
type FileSecretStore struct {
	mu            sync.Mutex
	root          string
	rename        func(string, string) error
	remove        func(string) error
	syncDirectory func(string) error
}

// NewFileSecretStore 创建并收紧 secret 根目录权限；符号链接根目录会被拒绝。
func NewFileSecretStore(root string) (*FileSecretStore, error) {
	clean := filepath.Clean(root)
	if clean == "." || !filepath.IsAbs(clean) {
		return nil, errors.New("certificate secret directory must be an absolute path")
	}
	if err := os.MkdirAll(clean, 0o700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(clean)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("certificate secret path must be a real directory")
	}
	if err := os.Chmod(clean, 0o700); err != nil {
		return nil, err
	}
	return &FileSecretStore{
		root:          clean,
		rename:        filepublish.Rename,
		remove:        os.Remove,
		syncDirectory: filepublish.SyncDirectory,
	}, nil
}

// Put 原子发布包含 fullchain.pem 和 privkey.pem 的新 secret 目录。
func (store *FileSecretStore) Put(certificatePEM, privateKeyPEM []byte) (reference string, resultErr error) {
	store.mu.Lock()
	defer store.mu.Unlock()

	reference = uuid.NewString()
	temporary, err := os.MkdirTemp(store.root, pendingPrefix)
	if err != nil {
		return "", err
	}
	cleanupTemporary := true
	defer func() {
		if !cleanupTemporary {
			return
		}
		cleanupErr := store.removeKnownDirectory(temporary, false)
		if cleanupErr == nil {
			cleanupErr = store.syncDirectory(store.root)
		}
		resultErr = errors.Join(resultErr, cleanupErr)
	}()
	if err := os.Chmod(temporary, 0o700); err != nil {
		return "", err
	}
	files := []struct {
		name    string
		payload []byte
	}{
		{name: certificateFile, payload: certificatePEM},
		{name: privateKeyFile, payload: privateKeyPEM},
	}
	for _, item := range files {
		file, err := os.OpenFile(filepath.Join(temporary, item.name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return "", err
		}
		if _, err = file.Write(item.payload); err == nil {
			err = file.Sync()
		}
		closeErr := file.Close()
		if err != nil || closeErr != nil {
			return "", errors.Join(err, closeErr)
		}
	}
	directory, err := os.Open(temporary)
	if err != nil {
		return "", err
	}
	err = errors.Join(directory.Sync(), directory.Close())
	if err != nil {
		return "", err
	}
	target := filepath.Join(store.root, reference)
	if err := store.rename(temporary, target); err != nil {
		return "", err
	}
	cleanupTemporary = false
	if err := store.syncDirectory(store.root); err != nil {
		return "", errors.Join(err, store.deleteLocked(reference))
	}
	return reference, nil
}

// Read 读取指定不可猜测引用；引用不允许携带路径组件。
func (store *FileSecretStore) Read(reference string) ([]byte, []byte, error) {
	store.mu.Lock()
	defer store.mu.Unlock()

	directory, err := store.resolve(reference)
	if err != nil {
		return nil, nil, err
	}
	if err := inspectSecretDirectory(directory, true); err != nil {
		return nil, nil, err
	}
	certificatePEM, err := os.ReadFile(filepath.Join(directory, certificateFile))
	if err != nil {
		return nil, nil, err
	}
	privateKeyPEM, err := os.ReadFile(filepath.Join(directory, privateKeyFile))
	if err != nil {
		return nil, nil, err
	}
	return certificatePEM, privateKeyPEM, nil
}

// Delete 把 UUID secret 原子改名为持久 tombstone，再只删除两个已知文件。
func (store *FileSecretStore) Delete(reference string) error {
	if _, err := canonicalReference(reference); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.deleteLocked(reference)
}

// Reconcile 以数据库当前引用为真值恢复活跃 tombstone 并清理其余受管目录。
func (store *FileSecretStore) Reconcile(liveReferences []string) error {
	live := make(map[string]struct{}, len(liveReferences))
	for _, reference := range liveReferences {
		canonical, err := canonicalReference(reference)
		if err != nil {
			return err
		}
		live[canonical] = struct{}{}
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	inventory, err := store.inspectInventory()
	if err != nil {
		return err
	}
	for reference := range live {
		_, hasLive := inventory.live[reference]
		_, hasTombstone := inventory.tombstones[reference]
		if hasLive == hasTombstone {
			return errors.New("active certificate secret has ambiguous or missing filesystem state")
		}
		if hasTombstone {
			if err := inspectSecretDirectory(store.tombstonePath(reference), true); err != nil {
				return errors.New("active certificate secret tombstone is incomplete or unsafe")
			}
		}
	}
	for reference := range inventory.live {
		if _, duplicated := inventory.tombstones[reference]; duplicated {
			return errors.New("certificate secret has both live and tombstone directories")
		}
	}

	restored := false
	for _, reference := range inventory.tombstoneOrder {
		if _, active := live[reference]; !active {
			continue
		}
		if err := store.rename(store.tombstonePath(reference), filepath.Join(store.root, reference)); err != nil {
			return err
		}
		restored = true
	}
	if restored {
		if err := store.syncDirectory(store.root); err != nil {
			return err
		}
	}

	for _, reference := range inventory.liveOrder {
		if _, active := live[reference]; active {
			continue
		}
		if err := store.deleteLocked(reference); err != nil {
			return err
		}
	}
	for _, reference := range inventory.tombstoneOrder {
		if _, active := live[reference]; active {
			continue
		}
		if err := store.deleteLocked(reference); err != nil {
			return err
		}
	}
	for _, path := range inventory.pending {
		if err := store.removeKnownDirectory(path, false); err != nil {
			return err
		}
		if err := store.syncDirectory(store.root); err != nil {
			return err
		}
	}
	return store.syncDirectory(store.root)
}

func (store *FileSecretStore) deleteLocked(reference string) error {
	livePath := filepath.Join(store.root, reference)
	tombstonePath := store.tombstonePath(reference)
	liveExists, err := inspectManagedDirectory(livePath, true)
	if err != nil {
		return err
	}
	tombstoneExists, err := inspectManagedDirectory(tombstonePath, false)
	if err != nil {
		return err
	}
	if liveExists && tombstoneExists {
		return errors.New("certificate secret has both live and tombstone directories")
	}
	if liveExists {
		if err := store.rename(livePath, tombstonePath); err != nil {
			return err
		}
		tombstoneExists = true
	}
	if !tombstoneExists {
		return store.syncDirectory(store.root)
	}
	if err := store.syncDirectory(store.root); err != nil {
		return err
	}
	if err := store.removeKnownDirectory(tombstonePath, false); err != nil {
		return err
	}
	return store.syncDirectory(store.root)
}

type secretInventory struct {
	live           map[string]struct{}
	liveOrder      []string
	tombstones     map[string]struct{}
	tombstoneOrder []string
	pending        []string
}

func (store *FileSecretStore) inspectInventory() (secretInventory, error) {
	entries, err := os.ReadDir(store.root)
	if err != nil {
		return secretInventory{}, err
	}
	result := secretInventory{live: make(map[string]struct{}), tombstones: make(map[string]struct{})}
	for _, entry := range entries {
		name := entry.Name()
		path := filepath.Join(store.root, name)
		if reference, err := canonicalReference(name); err == nil {
			if _, err := inspectManagedDirectory(path, true); err != nil {
				return secretInventory{}, err
			}
			result.live[reference] = struct{}{}
			result.liveOrder = append(result.liveOrder, reference)
			continue
		}
		if strings.HasPrefix(name, deletePrefix) {
			reference, err := canonicalReference(strings.TrimPrefix(name, deletePrefix))
			if err != nil {
				return secretInventory{}, errors.New("certificate secret root contains an invalid tombstone")
			}
			if _, err := inspectManagedDirectory(path, false); err != nil {
				return secretInventory{}, err
			}
			result.tombstones[reference] = struct{}{}
			result.tombstoneOrder = append(result.tombstoneOrder, reference)
			continue
		}
		if strings.HasPrefix(name, pendingPrefix) && len(name) > len(pendingPrefix) {
			if _, err := inspectManagedDirectory(path, false); err != nil {
				return secretInventory{}, err
			}
			result.pending = append(result.pending, path)
			continue
		}
		return secretInventory{}, errors.New("certificate secret root contains an unmanaged entry")
	}
	return result, nil
}

func (store *FileSecretStore) removeKnownDirectory(directory string, requireComplete bool) error {
	exists, err := inspectManagedDirectory(directory, requireComplete)
	if err != nil || !exists {
		return err
	}
	var result error
	for _, name := range []string{certificateFile, privateKeyFile} {
		if err := store.remove(filepath.Join(directory, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			result = errors.Join(result, err)
		}
	}
	if result != nil {
		return result
	}
	if err := store.remove(directory); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func inspectManagedDirectory(directory string, requireComplete bool) (bool, error) {
	info, err := os.Lstat(directory)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false, errors.New("certificate secret entry must be a real directory")
	}
	if err := inspectSecretDirectory(directory, requireComplete); err != nil {
		return false, err
	}
	return true, nil
}

func inspectSecretDirectory(directory string, requireComplete bool) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	found := make(map[string]bool, 2)
	for _, entry := range entries {
		name := entry.Name()
		if name != certificateFile && name != privateKeyFile {
			return errors.New("certificate secret directory contains an unmanaged entry")
		}
		info, err := os.Lstat(filepath.Join(directory, name))
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("certificate secret file must be a regular file")
		}
		found[name] = true
	}
	if requireComplete && (!found[certificateFile] || !found[privateKeyFile]) {
		return errors.New("certificate secret directory is incomplete")
	}
	return nil
}

func (store *FileSecretStore) tombstonePath(reference string) string {
	return filepath.Join(store.root, deletePrefix+reference)
}

func (store *FileSecretStore) resolve(reference string) (string, error) {
	canonical, err := canonicalReference(reference)
	if err != nil {
		return "", err
	}
	return filepath.Join(store.root, canonical), nil
}

func canonicalReference(reference string) (string, error) {
	parsed, err := uuid.Parse(reference)
	if err != nil || parsed.String() != reference {
		return "", fmt.Errorf("invalid certificate secret reference")
	}
	return reference, nil
}
