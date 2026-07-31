package certificate

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

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
	mu       sync.Mutex
	root     *os.Root
	rename   func(string, string) error
	remove   func(string) error
	syncRoot func() error
}

// NewFileSecretStore 创建或验证一个带固定 marker 的私有 secret 根目录。
func NewFileSecretStore(path string) (*FileSecretStore, error) {
	clean := filepath.Clean(path)
	if clean == "." || !filepath.IsAbs(clean) {
		return nil, errors.New("certificate secret directory must be an absolute path")
	}
	if isFilesystemRoot(clean) {
		return nil, errors.New("certificate secret directory must not be a filesystem root")
	}

	parent, name, err := openPinnedParent(clean)
	if err != nil {
		return nil, fmt.Errorf("validate certificate secret parent: %w", err)
	}
	store, err := openFileSecretStore(parent, name)
	closeErr := parent.Close()
	if err != nil {
		return nil, errors.Join(err, closeErr)
	}
	if closeErr != nil {
		return nil, errors.Join(closeErr, store.Close())
	}
	return store, nil
}

func openFileSecretStore(parent *os.Root, name string) (*FileSecretStore, error) {
	info, err := parent.Lstat(name)
	created := false
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err := parent.Mkdir(name, 0o700); err != nil {
			return nil, err
		}
		created = true
	case err != nil:
		return nil, err
	case info.Mode().Type() != os.ModeDir:
		return nil, errors.New("certificate secret path must be a real directory")
	}

	root, directory, err := openPinnedChild(parent, name)
	if err != nil {
		return nil, err
	}
	if created {
		err = securefs.SecureDirectoryHandle(directory)
	}
	if err == nil {
		err = securefs.ValidatePrivateDirectoryHandle(directory)
	}
	err = errors.Join(err, directory.Close())
	if err != nil {
		_ = root.Close()
		return nil, err
	}

	store := &FileSecretStore{root: root}
	store.rename = root.Rename
	store.remove = root.Remove
	store.syncRoot = func() error { return syncOpenedRoot(root) }
	if created {
		err = store.initializeMarker()
		if err == nil {
			err = syncOpenedRoot(parent)
		}
	} else {
		err = store.validateMarker()
	}
	if err != nil {
		return nil, errors.Join(err, store.Close())
	}
	return store, nil
}

// Close releases the pinned secret root. It is safe to call more than once.
func (store *FileSecretStore) Close() error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.root == nil {
		return nil
	}
	err := store.root.Close()
	store.root = nil
	return err
}

func (store *FileSecretStore) initializeMarker() error {
	marker, err := store.root.OpenFile(storeMarkerFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if err := securefs.SecureFileHandle(marker); err != nil {
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
	return store.syncRoot()
}

func (store *FileSecretStore) validateMarker() error {
	marker, err := openPrivateFile(store.root, storeMarkerFile)
	if err != nil {
		return errors.New("certificate secret directory marker is missing or unsafe")
	}
	payload, readErr := io.ReadAll(io.LimitReader(marker, int64(len(storeMarkerText)+1)))
	closeErr := marker.Close()
	if readErr != nil || closeErr != nil {
		return errors.New("certificate secret directory marker is missing or unsafe")
	}
	if string(payload) != storeMarkerText {
		return errors.New("certificate secret directory marker has an invalid version")
	}
	return nil
}

func (store *FileSecretStore) validateOpenLocked() error {
	if store.root == nil {
		return errors.New("certificate secret store is closed")
	}
	if err := validatePrivateRoot(store.root); err != nil {
		return err
	}
	return store.validateMarker()
}

// Put 原子发布包含 fullchain.pem 和 privkey.pem 的新 secret 目录。
func (store *FileSecretStore) Put(certificatePEM, privateKeyPEM []byte) (reference string, resultErr error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.validateOpenLocked(); err != nil {
		return "", err
	}

	reference = uuid.NewString()
	temporary := pendingPrefix + uuid.NewString()
	temporaryRoot, err := createPrivateChild(store.root, temporary)
	if err != nil {
		return "", err
	}
	cleanupTemporary := true
	defer func() {
		if temporaryRoot != nil {
			resultErr = errors.Join(resultErr, temporaryRoot.Close())
			temporaryRoot = nil
		}
		if !cleanupTemporary {
			return
		}
		cleanupErr := store.removeKnownDirectory(temporary, false)
		if cleanupErr == nil {
			cleanupErr = store.syncRoot()
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
		file, err := temporaryRoot.OpenFile(item.name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return "", err
		}
		if err = securefs.SecureFileHandle(file); err == nil {
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
	if err := syncOpenedRoot(temporaryRoot); err != nil {
		return "", err
	}
	if err := temporaryRoot.Close(); err != nil {
		return "", err
	}
	temporaryRoot = nil
	if err := store.rename(temporary, reference); err != nil {
		return "", err
	}
	cleanupTemporary = false
	if err := store.syncRoot(); err != nil {
		return "", errors.Join(err, store.deleteLocked(reference))
	}
	return reference, nil
}

// Read 读取指定不可猜测引用；引用不允许携带路径组件。
func (store *FileSecretStore) Read(reference string) (certificatePEM, privateKeyPEM []byte, resultErr error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.validateOpenLocked(); err != nil {
		return nil, nil, err
	}
	canonical, err := canonicalReference(reference)
	if err != nil {
		return nil, nil, err
	}
	directory, exists, err := openManagedDirectory(store.root, canonical, true)
	if err != nil {
		return nil, nil, err
	}
	if !exists {
		return nil, nil, os.ErrNotExist
	}
	defer func() { resultErr = errors.Join(resultErr, directory.Close()) }()
	certificatePEM, err = readPrivateFile(directory, certificateFile)
	if err != nil {
		return nil, nil, err
	}
	privateKeyPEM, err = readPrivateFile(directory, privateKeyFile)
	if err != nil {
		return nil, nil, err
	}
	return certificatePEM, privateKeyPEM, nil
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
	if err := store.validateOpenLocked(); err != nil {
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
			if _, err := inspectManagedDirectory(store.root, tombstoneName(reference), true); err != nil {
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
		if err := store.rename(tombstoneName(reference), reference); err != nil {
			return err
		}
		restored = true
	}
	if restored {
		if err := store.syncRoot(); err != nil {
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
	for _, name := range inventory.pending {
		if err := store.removeKnownDirectory(name, false); err != nil {
			return err
		}
		if err := store.syncRoot(); err != nil {
			return err
		}
	}
	return store.syncRoot()
}

func (store *FileSecretStore) deleteLocked(reference string) error {
	liveExists, err := inspectManagedDirectory(store.root, reference, true)
	if err != nil {
		return err
	}
	tombstone := tombstoneName(reference)
	tombstoneExists, err := inspectManagedDirectory(store.root, tombstone, false)
	if err != nil {
		return err
	}
	if liveExists && tombstoneExists {
		return errors.New("certificate secret has both live and tombstone directories")
	}
	if liveExists {
		if err := store.rename(reference, tombstone); err != nil {
			return err
		}
		tombstoneExists = true
	}
	if !tombstoneExists {
		return store.syncRoot()
	}
	if err := store.syncRoot(); err != nil {
		return err
	}
	if err := store.removeKnownDirectory(tombstone, false); err != nil {
		return err
	}
	return store.syncRoot()
}

type secretInventory struct {
	live           map[string]struct{}
	liveOrder      []string
	tombstones     map[string]struct{}
	tombstoneOrder []string
	pending        []string
}

func (store *FileSecretStore) inspectInventory() (secretInventory, error) {
	directory, err := store.root.Open(".")
	if err != nil {
		return secretInventory{}, err
	}
	if err := securefs.ValidatePrivateDirectoryHandle(directory); err != nil {
		_ = directory.Close()
		return secretInventory{}, err
	}
	entries, err := directory.ReadDir(-1)
	err = errors.Join(err, directory.Close())
	if err != nil {
		return secretInventory{}, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	result := secretInventory{live: make(map[string]struct{}), tombstones: make(map[string]struct{})}
	for _, entry := range entries {
		name := entry.Name()
		if name == storeMarkerFile {
			continue
		}
		if reference, err := canonicalReference(name); err == nil {
			if _, err := inspectManagedDirectory(store.root, name, true); err != nil {
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
			if _, err := inspectManagedDirectory(store.root, name, false); err != nil {
				return secretInventory{}, err
			}
			result.tombstones[reference] = struct{}{}
			result.tombstoneOrder = append(result.tombstoneOrder, reference)
			continue
		}
		if strings.HasPrefix(name, pendingPrefix) && len(name) > len(pendingPrefix) {
			if _, err := inspectManagedDirectory(store.root, name, false); err != nil {
				return secretInventory{}, err
			}
			result.pending = append(result.pending, name)
			continue
		}
		return secretInventory{}, errors.New("certificate secret root contains an unmanaged entry")
	}
	return result, nil
}

func (store *FileSecretStore) removeKnownDirectory(name string, requireComplete bool) error {
	directory, exists, err := openManagedDirectory(store.root, name, requireComplete)
	if err != nil || !exists {
		return err
	}
	if err := directory.Close(); err != nil {
		return err
	}
	var result error
	for _, fileName := range []string{certificateFile, privateKeyFile} {
		if err := store.remove(filepath.Join(name, fileName)); err != nil && !errors.Is(err, os.ErrNotExist) {
			result = errors.Join(result, err)
		}
	}
	if result != nil {
		return result
	}
	if err := store.remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func inspectManagedDirectory(root *os.Root, name string, requireComplete bool) (bool, error) {
	directory, exists, err := openManagedDirectory(root, name, requireComplete)
	if err != nil || !exists {
		return exists, err
	}
	return true, directory.Close()
}

func openManagedDirectory(root *os.Root, name string, requireComplete bool) (*os.Root, bool, error) {
	info, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if info.Mode().Type() != os.ModeDir {
		return nil, false, errors.New("certificate secret entry must be a real directory")
	}
	directory, handle, err := openPinnedChild(root, name)
	if err != nil {
		return nil, false, err
	}
	if err := securefs.ValidatePrivateDirectoryHandle(handle); err != nil {
		_ = handle.Close()
		_ = directory.Close()
		return nil, false, err
	}
	if err := handle.Close(); err != nil {
		_ = directory.Close()
		return nil, false, err
	}
	if err := inspectSecretDirectory(directory, requireComplete); err != nil {
		_ = directory.Close()
		return nil, false, err
	}
	return directory, true, nil
}

func inspectSecretDirectory(directory *os.Root, requireComplete bool) error {
	handle, err := directory.Open(".")
	if err != nil {
		return err
	}
	if err := securefs.ValidatePrivateDirectoryHandle(handle); err != nil {
		_ = handle.Close()
		return err
	}
	entries, err := handle.ReadDir(-1)
	err = errors.Join(err, handle.Close())
	if err != nil {
		return err
	}
	found := make(map[string]bool, 2)
	for _, entry := range entries {
		name := entry.Name()
		if name != certificateFile && name != privateKeyFile {
			return errors.New("certificate secret directory contains an unmanaged entry")
		}
		file, err := openPrivateFile(directory, name)
		if err != nil {
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
		found[name] = true
	}
	if requireComplete && (!found[certificateFile] || !found[privateKeyFile]) {
		return errors.New("certificate secret directory is incomplete")
	}
	return nil
}

func readPrivateFile(root *os.Root, name string) ([]byte, error) {
	file, err := openPrivateFile(root, name)
	if err != nil {
		return nil, err
	}
	payload, readErr := io.ReadAll(file)
	return payload, errors.Join(readErr, file.Close())
}

func openPrivateFile(root *os.Root, name string) (*os.File, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("certificate secret file must be a regular file")
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	openedInfo, err := file.Stat()
	if err != nil || !os.SameFile(info, openedInfo) {
		_ = file.Close()
		return nil, errors.New("certificate secret file identity changed")
	}
	if err := securefs.ValidatePrivateFileHandle(file); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func createPrivateChild(parent *os.Root, name string) (*os.Root, error) {
	if err := parent.Mkdir(name, 0o700); err != nil {
		return nil, err
	}
	directory, handle, err := openPinnedChild(parent, name)
	if err != nil {
		return nil, err
	}
	if err = securefs.SecureDirectoryHandle(handle); err == nil {
		err = securefs.ValidatePrivateDirectoryHandle(handle)
	}
	err = errors.Join(err, handle.Close())
	if err != nil {
		_ = directory.Close()
		return nil, err
	}
	return directory, nil
}

func openPinnedParent(path string) (*os.Root, string, error) {
	parentPath := filepath.Dir(path)
	volumeRoot := filepath.Clean(filepath.VolumeName(parentPath) + string(os.PathSeparator))
	relative, err := filepath.Rel(volumeRoot, parentPath)
	if err != nil {
		return nil, "", err
	}
	current, err := os.OpenRoot(volumeRoot)
	if err != nil {
		return nil, "", err
	}
	if relative != "." {
		for _, component := range strings.Split(relative, string(os.PathSeparator)) {
			next, handle, err := openPinnedChild(current, component)
			if err != nil {
				_ = current.Close()
				return nil, "", err
			}
			if err := handle.Close(); err != nil {
				_ = next.Close()
				_ = current.Close()
				return nil, "", err
			}
			if err := current.Close(); err != nil {
				_ = next.Close()
				return nil, "", err
			}
			current = next
		}
	}
	if err := validatePrivateRoot(current); err != nil {
		_ = current.Close()
		return nil, "", err
	}
	return current, filepath.Base(path), nil
}

func openPinnedChild(parent *os.Root, name string) (*os.Root, *os.File, error) {
	if name == "" || name == "." || filepath.Base(name) != name {
		return nil, nil, errors.New("certificate store path component is invalid")
	}
	info, err := parent.Lstat(name)
	if err != nil {
		return nil, nil, err
	}
	if info.Mode().Type() != os.ModeDir {
		return nil, nil, errors.New("certificate store path component must be a real directory")
	}
	directory, err := parent.OpenRoot(name)
	if err != nil {
		return nil, nil, err
	}
	handle, err := directory.Open(".")
	if err != nil {
		_ = directory.Close()
		return nil, nil, err
	}
	openedInfo, err := handle.Stat()
	if err != nil || !os.SameFile(info, openedInfo) {
		_ = handle.Close()
		_ = directory.Close()
		return nil, nil, errors.New("certificate store directory identity changed")
	}
	return directory, handle, nil
}

func validatePrivateRoot(root *os.Root) error {
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	return errors.Join(securefs.ValidatePrivateDirectoryHandle(directory), directory.Close())
}

func syncOpenedRoot(root *os.Root) error {
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	return errors.Join(syncOpenedDirectory(directory), directory.Close())
}

func tombstoneName(reference string) string { return deletePrefix + reference }

func canonicalReference(reference string) (string, error) {
	parsed, err := uuid.Parse(reference)
	if err != nil || parsed.String() != reference {
		return "", fmt.Errorf("invalid certificate secret reference")
	}
	return reference, nil
}

func isFilesystemRoot(path string) bool { return filepath.Dir(path) == path }
