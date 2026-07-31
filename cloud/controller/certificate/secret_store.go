package certificate

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/anytty/anytty/shared/filepublish"
	"github.com/anytty/anytty/shared/securefs"
	"github.com/google/uuid"
)

const (
	certificateFile = "fullchain.pem"
	privateKeyFile  = "privkey.pem"
	deletePrefix    = ".delete-"
	pendingPrefix   = ".pending-"
	storeMarkerFile = ".anytty-certificate-store-v1"
	storeMarkerText = "anytty-certificate-store-v1\n"
)

// FileSecretStore 在 Controller 本机仅服务用户可访问的目录保存当前证书材料。
// 每次 Put 都创建不可猜测的新引用，数据库提交后才由业务层淘汰旧引用。
type FileSecretStore struct {
	mu            sync.Mutex
	root          string
	rootDirectory *os.File
	rename        func(string, string) error
	remove        func(string) error
	syncDirectory func(string) error
}

// NewFileSecretStore 创建或验证一个带固定 marker 的私有 secret 根目录。
func NewFileSecretStore(root string) (*FileSecretStore, error) {
	clean := filepath.Clean(root)
	if clean == "." || !filepath.IsAbs(clean) {
		return nil, errors.New("certificate secret directory must be an absolute path")
	}
	if isFilesystemRoot(clean) {
		return nil, errors.New("certificate secret directory must not be a filesystem root")
	}
	parent, err := canonicalExistingDirectory(filepath.Dir(clean))
	if err != nil {
		return nil, fmt.Errorf("validate certificate secret parent: %w", err)
	}
	physicalRoot := filepath.Join(parent, filepath.Base(clean))
	info, err := os.Lstat(physicalRoot)
	created := false
	var directory *os.File
	switch {
	case errors.Is(err, os.ErrNotExist):
		directory, err = securefs.CreatePrivateDirectory(physicalRoot)
		created = err == nil
	case err != nil:
		return nil, err
	default:
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("certificate secret path must be a real directory")
		}
		physicalRoot, err = canonicalExistingDirectory(physicalRoot)
		if err == nil {
			directory, err = openDirectoryNoFollow(physicalRoot)
		}
	}
	if err != nil {
		return nil, err
	}
	store := &FileSecretStore{
		root:          physicalRoot,
		rootDirectory: directory,
		rename:        filepublish.Rename,
		remove:        os.Remove,
		syncDirectory: filepublish.SyncDirectory,
	}
	if err := securefs.ValidatePrivateDirectoryHandle(directory); err != nil {
		_ = directory.Close()
		return nil, err
	}
	if created {
		if err := store.initializeMarker(parent); err != nil {
			_ = directory.Close()
			return nil, err
		}
	} else {
		if err := store.validateMarker(); err != nil {
			_ = directory.Close()
			return nil, err
		}
		if err := store.syncDirectory(parent); err != nil {
			_ = directory.Close()
			return nil, err
		}
	}
	return store, nil
}

// Close releases the root identity handle retained for path-retarget checks.
func (store *FileSecretStore) Close() error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.rootDirectory == nil {
		return nil
	}
	err := store.rootDirectory.Close()
	store.rootDirectory = nil
	return err
}

func (store *FileSecretStore) initializeMarker(parent string) error {
	markerPath := filepath.Join(store.root, storeMarkerFile)
	marker, err := os.OpenFile(markerPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if err := securefs.SecureFile(markerPath); err != nil {
		return errors.Join(err, marker.Close())
	}
	if err := securefs.ValidatePrivateFileHandle(marker); err != nil {
		return errors.Join(err, marker.Close())
	}
	written, err := io.WriteString(marker, storeMarkerText)
	if err == nil && written != len(storeMarkerText) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = marker.Sync()
	}
	err = errors.Join(err, marker.Close())
	if err != nil {
		return err
	}
	if err := store.syncDirectory(store.root); err != nil {
		return err
	}
	return store.syncDirectory(parent)
}

func (store *FileSecretStore) validateMarker() error {
	marker, err := openFileNoFollow(filepath.Join(store.root, storeMarkerFile))
	if err != nil {
		return errors.New("certificate secret directory marker is missing or unsafe")
	}
	defer marker.Close()
	if err := securefs.ValidatePrivateFileHandle(marker); err != nil {
		return errors.New("certificate secret directory marker is missing or unsafe")
	}
	payload, err := io.ReadAll(io.LimitReader(marker, int64(len(storeMarkerText)+1)))
	if err != nil || string(payload) != storeMarkerText {
		return errors.New("certificate secret directory marker has an invalid version")
	}
	return nil
}

func (store *FileSecretStore) ensureRootIdentityLocked() error {
	if store.rootDirectory == nil {
		return errors.New("certificate secret store is closed")
	}
	canonical, err := canonicalExistingDirectory(store.root)
	if err != nil || !sameFilesystemPath(canonical, store.root) {
		return errors.New("certificate secret root path identity changed")
	}
	pathInfo, err := os.Stat(store.root)
	if err != nil {
		return errors.New("certificate secret root path identity changed")
	}
	handleInfo, err := store.rootDirectory.Stat()
	if err != nil || !os.SameFile(pathInfo, handleInfo) {
		return errors.New("certificate secret root path identity changed")
	}
	if err := securefs.ValidatePrivateDirectoryHandle(store.rootDirectory); err != nil {
		return err
	}
	return store.validateMarker()
}

// Put 原子发布包含 fullchain.pem 和 privkey.pem 的新 secret 目录。
func (store *FileSecretStore) Put(certificatePEM, privateKeyPEM []byte) (reference string, resultErr error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.ensureRootIdentityLocked(); err != nil {
		return "", err
	}

	reference = uuid.NewString()
	temporary := filepath.Join(store.root, pendingPrefix+uuid.NewString())
	temporaryDirectory, err := securefs.CreatePrivateDirectory(temporary)
	if err != nil {
		return "", err
	}
	defer func() {
		if temporaryDirectory != nil {
			_ = temporaryDirectory.Close()
		}
	}()
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
		if err = securefs.SecureFile(filepath.Join(temporary, item.name)); err == nil {
			err = securefs.ValidatePrivateFileHandle(file)
		}
		if err == nil {
			if _, err = file.Write(item.payload); err == nil {
				err = file.Sync()
			}
		}
		closeErr := file.Close()
		if err != nil || closeErr != nil {
			return "", errors.Join(err, closeErr)
		}
	}
	err = syncOpenedDirectory(temporaryDirectory)
	if err != nil {
		return "", err
	}
	if err := temporaryDirectory.Close(); err != nil {
		return "", err
	}
	temporaryDirectory = nil
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
	if err := store.ensureRootIdentityLocked(); err != nil {
		return nil, nil, err
	}

	directory, err := store.resolve(reference)
	if err != nil {
		return nil, nil, err
	}
	if err := inspectSecretDirectory(directory, true); err != nil {
		return nil, nil, err
	}
	certificatePEM, err := readPrivateFile(filepath.Join(directory, certificateFile))
	if err != nil {
		return nil, nil, err
	}
	privateKeyPEM, err := readPrivateFile(filepath.Join(directory, privateKeyFile))
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
	if err := store.ensureRootIdentityLocked(); err != nil {
		return err
	}
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
	if err := store.ensureRootIdentityLocked(); err != nil {
		return err
	}
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
		if name == storeMarkerFile {
			continue
		}
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
	handle, err := openDirectoryNoFollow(directory)
	if err != nil {
		return err
	}
	defer handle.Close()
	if err := securefs.ValidatePrivateDirectoryHandle(handle); err != nil {
		return err
	}
	entries, err := handle.ReadDir(-1)
	if err != nil {
		return err
	}
	found := make(map[string]bool, 2)
	for _, entry := range entries {
		name := entry.Name()
		if name != certificateFile && name != privateKeyFile {
			return errors.New("certificate secret directory contains an unmanaged entry")
		}
		file, err := openFileNoFollow(filepath.Join(directory, name))
		if err != nil {
			return err
		}
		validateErr := securefs.ValidatePrivateFileHandle(file)
		closeErr := file.Close()
		if validateErr != nil || closeErr != nil {
			return errors.New("certificate secret file must be a regular file")
		}
		found[name] = true
	}
	if requireComplete && (!found[certificateFile] || !found[privateKeyFile]) {
		return errors.New("certificate secret directory is incomplete")
	}
	return nil
}

func readPrivateFile(path string) ([]byte, error) {
	file, err := openFileNoFollow(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if err := securefs.ValidatePrivateFileHandle(file); err != nil {
		return nil, err
	}
	return io.ReadAll(file)
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

func canonicalExistingDirectory(path string) (string, error) {
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) {
		return "", errors.New("directory path must be absolute")
	}
	chain := make([]string, 0, 8)
	for current := clean; ; current = filepath.Dir(current) {
		chain = append(chain, current)
		if parent := filepath.Dir(current); parent == current {
			break
		}
	}
	for index := len(chain) - 1; index >= 0; index-- {
		directory, err := openDirectoryNoFollow(chain[index])
		if err != nil {
			return "", err
		}
		if err := directory.Close(); err != nil {
			return "", err
		}
	}
	physical, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return "", err
	}
	return filepath.Clean(physical), nil
}

func isFilesystemRoot(path string) bool {
	volume := filepath.VolumeName(path)
	root := filepath.Clean(volume + string(os.PathSeparator))
	return sameFilesystemPath(filepath.Clean(path), root)
}
