package remoteauth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const revocationStoreFile = "remote_grant_revocations.json"
const revocationStoreVersion = 2

var grantRefPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// CredentialStore 是客户端本地 capability grant 凭据存储。
// connections.yaml 只能保存 grant_ref；真实 bearer grant 由该 store 按 ref 解析，不能回写 registry、日志或 endpoint label。
type CredentialStore struct {
	dir string
}

// NewCredentialStore 创建文件型客户端凭据存储。
// dir 属于本地客户端安全域，不应与 daemon identity、Hub registry 或 workbench storage 混用。
func NewCredentialStore(dir string) *CredentialStore {
	return &CredentialStore{dir: strings.TrimSpace(dir)}
}

// Put 保存 remote-issued bearer capability grant。
// ref 是稳定引用而非 secret；凭据文件固定为 0600，写入失败时不产生 registry fallback。
func (store *CredentialStore) Put(ref string, grant string) error {
	if store == nil || strings.TrimSpace(store.dir) == "" {
		return fmt.Errorf("remote credential store directory is not configured")
	}
	if err := validateGrantRef(ref); err != nil {
		return err
	}
	grant = strings.TrimSpace(grant)
	if grant == "" {
		return fmt.Errorf("remote credential grant is empty")
	}
	if err := os.MkdirAll(store.dir, 0o700); err != nil {
		return fmt.Errorf("create remote credential directory: %w", err)
	}
	return writePrivateFile(filepath.Join(store.dir, credentialFileName(ref)), []byte(grant+"\n"))
}

// Resolve 按 grant_ref 读取 bearer capability grant。
// 找不到、权限异常或内容为空都必须失败，调用方不得回退到 connections.yaml、旧 session token 或其他 endpoint。
func (store *CredentialStore) Resolve(ref string) (string, error) {
	if store == nil || strings.TrimSpace(store.dir) == "" {
		return "", fmt.Errorf("remote credential store directory is not configured")
	}
	if err := validateGrantRef(ref); err != nil {
		return "", err
	}
	path := filepath.Join(store.dir, credentialFileName(ref))
	payload, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("resolve remote credential %q: %w", ref, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return "", fmt.Errorf("secure remote credential %q: %w", ref, err)
	}
	grant := strings.TrimSpace(string(payload))
	if grant == "" {
		return "", fmt.Errorf("remote credential %q is empty", ref)
	}
	return grant, nil
}

func validateGrantRef(ref string) error {
	ref = strings.TrimSpace(ref)
	if !grantRefPattern.MatchString(ref) {
		return fmt.Errorf("invalid remote grant_ref %q", ref)
	}
	return nil
}

func credentialFileName(ref string) string {
	return "grant_" + base64.RawURLEncoding.EncodeToString([]byte(ref)) + ".token"
}

// RevocationStore 是 daemon-local 持久化 grant 撤销真值。
// Hub 只中继连接数据；remote daemon 重启后仍必须拒绝已撤销 grant。
type RevocationStore struct {
	mu          sync.RWMutex
	path        string
	revocations map[string]time.Time
}

type storedRevocations struct {
	Version     int                  `json:"version"`
	Revocations map[string]time.Time `json:"revocations"`
}

// LoadRevocationStore 加载 daemon-local grant 撤销集合。
// 文件缺失表示尚无撤销；损坏文件会失败，不能按空集合继续接受连接。
func LoadRevocationStore(dir string) (*RevocationStore, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, fmt.Errorf("remote revocation store requires directory")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create remote revocation directory: %w", err)
	}
	store := &RevocationStore{path: filepath.Join(dir, revocationStoreFile), revocations: map[string]time.Time{}}
	payload, err := os.ReadFile(store.path)
	if os.IsNotExist(err) {
		return store, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read remote grant revocations: %w", err)
	}
	var stored storedRevocations
	if err := json.Unmarshal(payload, &stored); err != nil {
		return nil, fmt.Errorf("decode remote grant revocations: %w", err)
	}
	if stored.Version != revocationStoreVersion {
		return nil, fmt.Errorf("unsupported remote grant revocation version %d", stored.Version)
	}
	for revocationID, revokedAt := range stored.Revocations {
		if revocationID = strings.TrimSpace(revocationID); revocationID != "" {
			store.revocations[revocationID] = revokedAt.UTC()
		}
	}
	if err := os.Chmod(store.path, 0o600); err != nil {
		return nil, fmt.Errorf("secure remote grant revocations: %w", err)
	}
	return store, nil
}

// Revoke 持久化撤销一个 revocation ID。
// 撤销是幂等的，只影响指定 capability，不改变 terminal lifecycle 或其他 endpoint session。
func (store *RevocationStore) Revoke(revocationID string) error {
	if store == nil {
		return fmt.Errorf("remote revocation store is nil")
	}
	revocationID = strings.TrimSpace(revocationID)
	if revocationID == "" {
		return fmt.Errorf("remote revocation requires revocation_id")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, ok := store.revocations[revocationID]; ok {
		return nil
	}
	store.revocations[revocationID] = time.Now().UTC()
	if err := store.persistLocked(); err != nil {
		delete(store.revocations, revocationID)
		return err
	}
	return nil
}

// Revoked 返回 revocation ID 是否已被签发 daemon 撤销。
func (store *RevocationStore) Revoked(revocationID string) bool {
	if store == nil {
		return false
	}
	store.mu.RLock()
	_, ok := store.revocations[strings.TrimSpace(revocationID)]
	store.mu.RUnlock()
	return ok
}

func (store *RevocationStore) persistLocked() error {
	ordered := make([]string, 0, len(store.revocations))
	for revocationID := range store.revocations {
		ordered = append(ordered, revocationID)
	}
	sort.Strings(ordered)
	revocations := make(map[string]time.Time, len(ordered))
	for _, revocationID := range ordered {
		revocations[revocationID] = store.revocations[revocationID]
	}
	payload, err := json.MarshalIndent(storedRevocations{Version: revocationStoreVersion, Revocations: revocations}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode remote grant revocations: %w", err)
	}
	if err := writePrivateFile(store.path, append(payload, '\n')); err != nil {
		return fmt.Errorf("persist remote grant revocations: %w", err)
	}
	return nil
}
