package session

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	keyring "github.com/zalando/go-keyring"
)

// KeyringStore 把 Companion account/device session 保存到 macOS Keychain、Linux Secret Service
// 或 Windows Credential Manager。它不提供文件、环境变量或内存 fallback。
type KeyringStore struct {
	service string
	backend keyringBackend
}

type keyringBackend interface {
	Get(string, string) (string, error)
	Set(string, string, string) error
	Delete(string, string) error
}

type systemKeyring struct{}

func (systemKeyring) Get(service, user string) (string, error) { return keyring.Get(service, user) }
func (systemKeyring) Set(service, user, password string) error {
	return keyring.Set(service, user, password)
}
func (systemKeyring) Delete(service, user string) error { return keyring.Delete(service, user) }

// NewKeyringStore 创建固定 publisher service namespace 的平台 credential store。
// service 为空或含控制字符时拒绝，避免账号 secret 被写入 caller-selected credential namespace。
func NewKeyringStore(service string) (*KeyringStore, error) {
	service = strings.TrimSpace(service)
	if service == "" || strings.ContainsAny(service, "\r\n\x00") {
		return nil, fmt.Errorf("Cloud Companion keyring service is invalid")
	}
	return &KeyringStore{service: service, backend: systemKeyring{}}, nil
}

// LoadSecret 从平台 credential manager 解码 opaque session bytes。
// 不存在映射为 ErrNotFound；损坏 base64 映射为 ErrInvalid，不能 fallback 到普通文件。
func (store *KeyringStore) LoadSecret(ctx context.Context, key string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	value, err := store.backend.Get(store.service, key)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	decoded, err := base64.RawStdEncoding.DecodeString(value)
	if err != nil || len(decoded) == 0 {
		return nil, ErrInvalid
	}
	return decoded, nil
}

// StoreSecret 把 opaque session bytes 编码后原子交给平台 credential manager。
// value 为空、context 取消或 backend 失败都直接返回，不写任何明文 fallback。
func (store *KeyringStore) StoreSecret(ctx context.Context, key string, value []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if key == "" || len(value) == 0 {
		return ErrInvalid
	}
	return store.backend.Set(store.service, key, base64.RawStdEncoding.EncodeToString(value))
}

// DeleteSecret 从平台 credential manager 删除指定 session slot。
// credential 不存在映射为 ErrNotFound，由 Manager.Delete 保持幂等。
func (store *KeyringStore) DeleteSecret(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	err := store.backend.Delete(store.service, key)
	if errors.Is(err, keyring.ErrNotFound) {
		return ErrNotFound
	}
	return err
}
