package certificate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/anytty/anytty/shared/filepublish"
	"github.com/google/uuid"
)

const (
	certificateFile = "fullchain.pem"
	privateKeyFile  = "privkey.pem"
)

// FileSecretStore 在 Controller 本机仅服务用户可访问的目录保存当前证书材料。
// 每次 Put 都创建不可猜测的新引用，数据库提交后才由业务层淘汰旧引用。
type FileSecretStore struct {
	root          string
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
	return &FileSecretStore{root: clean, syncDirectory: filepublish.SyncDirectory}, nil
}

// Put 原子发布包含 fullchain.pem 和 privkey.pem 的新 secret 目录。
func (store *FileSecretStore) Put(certificatePEM, privateKeyPEM []byte) (string, error) {
	reference := uuid.NewString()
	temporary, err := os.MkdirTemp(store.root, ".pending-")
	if err != nil {
		return "", err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(filepath.Join(temporary, certificateFile))
			_ = os.Remove(filepath.Join(temporary, privateKeyFile))
			_ = os.Remove(temporary)
		}
	}()
	if err := os.Chmod(temporary, 0o700); err != nil {
		return "", err
	}
	for name, payload := range map[string][]byte{certificateFile: certificatePEM, privateKeyFile: privateKeyPEM} {
		file, err := os.OpenFile(filepath.Join(temporary, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return "", err
		}
		if _, err = file.Write(payload); err == nil {
			err = file.Sync()
		}
		closeErr := file.Close()
		if err != nil {
			return "", err
		}
		if closeErr != nil {
			return "", closeErr
		}
	}
	directory, err := os.Open(temporary)
	if err != nil {
		return "", err
	}
	err = directory.Sync()
	closeErr := directory.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	target := filepath.Join(store.root, reference)
	if err := filepublish.Rename(temporary, target); err != nil {
		return "", err
	}
	cleanup = false
	if err := store.syncDirectory(store.root); err != nil {
		return "", errors.Join(err, removeSecretDirectory(target))
	}
	return reference, nil
}

// Read 读取指定不可猜测引用；引用不允许携带路径组件。
func (store *FileSecretStore) Read(reference string) ([]byte, []byte, error) {
	directory, err := store.resolve(reference)
	if err != nil {
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

// Delete 只删除通过 UUID 校验的单个 secret 引用及两个已知文件。
func (store *FileSecretStore) Delete(reference string) error {
	directory, err := store.resolve(reference)
	if err != nil {
		return err
	}
	if err := removeSecretDirectory(directory); err != nil {
		return err
	}
	return store.syncDirectory(store.root)
}

func removeSecretDirectory(directory string) error {
	var result error
	for _, name := range []string{certificateFile, privateKeyFile} {
		if err := os.Remove(filepath.Join(directory, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			result = errors.Join(result, err)
		}
	}
	if err := os.Remove(directory); err != nil && !errors.Is(err, os.ErrNotExist) {
		result = errors.Join(result, err)
	}
	return result
}

func (store *FileSecretStore) resolve(reference string) (string, error) {
	parsed, err := uuid.Parse(reference)
	if err != nil || parsed.String() != reference {
		return "", fmt.Errorf("invalid certificate secret reference")
	}
	return filepath.Join(store.root, reference), nil
}
