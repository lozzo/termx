// Package remoteauth 定义 managed WebRTC DataChannel 使用的公开端到端设备身份、capability 与授权状态机。
package remoteauth

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

const grantPrefix = "termx-grant-v1"

var (
	// ErrGrantMalformed 表示 capability envelope、claims 或必填字段不符合当前版本。
	ErrGrantMalformed = errors.New("remote capability grant malformed")
	// ErrGrantFingerprintMismatch 表示 grant 签发 key 与本地 pin 或 claims 中 issuer fingerprint 不一致。
	ErrGrantFingerprintMismatch = errors.New("remote capability grant fingerprint mismatch")
	// ErrGrantSignatureInvalid 表示 Ed25519 capability 签名无效。
	ErrGrantSignatureInvalid = errors.New("remote capability grant signature invalid")
	// ErrGrantNotActive 表示当前时间早于 capability not_before。
	ErrGrantNotActive = errors.New("remote capability grant not active")
	// ErrGrantExpired 表示 capability 已达到或超过 expires_at。
	ErrGrantExpired = errors.New("remote capability grant expired")
	// ErrGrantRevoked 表示 daemon-local revocation truth 已撤销该 capability。
	ErrGrantRevoked = errors.New("remote capability grant revoked")
	// ErrGrantScopeInvalid 表示 capability scope 为空、互相冲突或无法映射为 core-v2 scope。
	ErrGrantScopeInvalid = errors.New("remote capability grant scope invalid")
)

// Scope 描述一条获授权 protocol session 可见的 core-v2 能力边界。
// daemon、单 terminal 和 machine-events-only 三种基础 scope 互斥；文件权限只能附着在 daemon scope。
type Scope struct {
	// AllowDaemon 允许 daemon terminal 管理，但不隐式授予文件或未来新增能力。
	AllowDaemon bool `json:"allow_daemon,omitempty"`
	// TerminalID 把 session 限制到单个 daemon-local terminal。
	TerminalID string `json:"terminal_id,omitempty"`
	// MachineEventsOnly 只允许读取受限 terminal lifecycle 事件。
	MachineEventsOnly bool `json:"machine_events_only,omitempty"`
	// FileReadMetadata 允许目录枚举和 lstat metadata。
	FileReadMetadata bool `json:"file_read_metadata,omitempty"`
	// FileReadContent 允许预览和下载文件内容。
	FileReadContent bool `json:"file_read_content,omitempty"`
	// FileWriteContent 允许创建、恢复和完成上传 transfer。
	FileWriteContent bool `json:"file_write_content,omitempty"`
	// FileMutate 允许 mkdir、rename、delete、copy 和 move。
	FileMutate bool `json:"file_mutate,omitempty"`
}

// FullDaemonScope 返回官方 daemon 配对默认使用的显式能力集合。
// 文件权限逐项写入 signed claims，后续新增能力不会因 AllowDaemon 自动扩张。
func FullDaemonScope() Scope {
	return Scope{AllowDaemon: true, FileReadMetadata: true, FileReadContent: true, FileWriteContent: true, FileMutate: true}
}

// Claims 是 remote daemon 签发并验证的 capability grant 内容。
// issuer、有效期、独立 revocation ID、nonce 和 scope 都由 DeviceIdentity 签名；Hub/Companion 不读取或修改这些授权字段。
type Claims struct {
	Version                 uint32    `json:"version"`
	GrantID                 string    `json:"grant_id"`
	IssuerDeviceID          string    `json:"issuer_device_id"`
	IssuerDeviceFingerprint string    `json:"issuer_device_fingerprint"`
	Scope                   Scope     `json:"scope"`
	IssuedAt                time.Time `json:"issued_at"`
	NotBefore               time.Time `json:"not_before"`
	ExpiresAt               time.Time `json:"expires_at"`
	RevocationID            string    `json:"revocation_id"`
	Nonce                   string    `json:"nonce"`
}

// RevocationChecker 是 daemon-local capability revocation ID 查询边界。
// Hub/relay 不拥有撤销真值；连接建立和重连时由签发设备查询本地 store。
type RevocationChecker interface {
	Revoked(revocationID string) bool
}

// Revocations 是进程内 grant 撤销集合。
// 它适用于 transport contract 与 daemon runtime；持久化策略由后续 daemon agent 装配切片决定。
type Revocations struct {
	mu  sync.RWMutex
	ids map[string]struct{}
}

// NewRevocations 创建空的 daemon-local 撤销集合。
func NewRevocations() *Revocations {
	return &Revocations{ids: map[string]struct{}{}}
}

// Revoke 按 revocation ID 撤销 capability。
// 空 ID 会被忽略；多个 grant 可以显式共享一个 revocation ID，但撤销不会影响其他 endpoint scope。
func (revocations *Revocations) Revoke(revocationID string) {
	if revocations == nil {
		return
	}
	revocationID = strings.TrimSpace(revocationID)
	if revocationID == "" {
		return
	}
	revocations.mu.Lock()
	revocations.ids[revocationID] = struct{}{}
	revocations.mu.Unlock()
}

// Revoked 返回指定 revocation ID 是否已由签发 daemon 撤销。
func (revocations *Revocations) Revoked(revocationID string) bool {
	if revocations == nil {
		return false
	}
	revocations.mu.RLock()
	_, ok := revocations.ids[strings.TrimSpace(revocationID)]
	revocations.mu.RUnlock()
	return ok
}

// Fingerprint 从 Ed25519 公钥生成稳定安全身份。
// fingerprint 是连接配置的 trust anchor，不是 hub device ID、endpoint label 或 grant ref。
func Fingerprint(publicKey ed25519.PublicKey) string {
	sum := sha256.Sum256(publicKey)
	return "ed25519-sha256:" + base64.RawURLEncoding.EncodeToString(sum[:])
}

// Issue 使用 remote daemon 的设备私钥签发 capability grant。
// grant 自包含签名公钥用于 fingerprint proof，但不包含私钥、Hub token、客户端公钥或 allowlist 信息。
func Issue(privateKey ed25519.PrivateKey, claims Claims) (string, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return "", fmt.Errorf("%w: requires ed25519 private key", ErrGrantMalformed)
	}
	publicKey := privateKey.Public().(ed25519.PublicKey)
	claims = normalizeClaims(claims)
	if claims.Version == 0 {
		claims.Version = 1
	}
	if claims.NotBefore.IsZero() {
		claims.NotBefore = claims.IssuedAt
	}
	if claims.RevocationID == "" {
		claims.RevocationID = claims.GrantID
	}
	claims.IssuerDeviceFingerprint = Fingerprint(publicKey)
	if claims.Nonce == "" {
		nonce := make([]byte, 16)
		if _, err := rand.Read(nonce); err != nil {
			return "", fmt.Errorf("generate remote capability nonce: %w", err)
		}
		claims.Nonce = base64.RawURLEncoding.EncodeToString(nonce)
	}
	if err := validateClaims(claims); err != nil {
		return "", err
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("encode remote capability grant: %w", err)
	}
	payloadPart := base64.RawURLEncoding.EncodeToString(payload)
	publicPart := base64.RawURLEncoding.EncodeToString(publicKey)
	signingInput := strings.Join([]string{grantPrefix, payloadPart, publicPart}, ".")
	signature := ed25519.Sign(privateKey, []byte(signingInput))
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

// Verify 校验 capability grant 的格式、签名、设备 fingerprint、有效期和撤销状态。
// expectedFingerprint 必须来自 connection registry 的安全身份；旧 session token、label 或 hub device ID 均不能替代。
func Verify(grant string, expectedFingerprint string, now time.Time, revocations RevocationChecker) (Claims, error) {
	parts := strings.Split(strings.TrimSpace(grant), ".")
	if len(parts) != 4 || parts[0] != grantPrefix {
		return Claims{}, fmt.Errorf("%w: unsupported envelope", ErrGrantMalformed)
	}
	publicKeyBytes, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(publicKeyBytes) != ed25519.PublicKeySize {
		return Claims{}, fmt.Errorf("%w: invalid public key", ErrGrantMalformed)
	}
	publicKey := ed25519.PublicKey(publicKeyBytes)
	fingerprint := Fingerprint(publicKey)
	if subtle.ConstantTimeCompare([]byte(fingerprint), []byte(strings.TrimSpace(expectedFingerprint))) != 1 {
		return Claims{}, ErrGrantFingerprintMismatch
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil || !ed25519.Verify(publicKey, []byte(strings.Join(parts[:3], ".")), signature) {
		return Claims{}, ErrGrantSignatureInvalid
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, fmt.Errorf("%w: decode payload: %v", ErrGrantMalformed, err)
	}
	var claims Claims
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&claims); err != nil {
		return Claims{}, fmt.Errorf("%w: decode claims: %v", ErrGrantMalformed, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Claims{}, fmt.Errorf("%w: trailing claims data", ErrGrantMalformed)
	}
	claims = normalizeClaims(claims)
	if subtle.ConstantTimeCompare([]byte(claims.IssuerDeviceFingerprint), []byte(fingerprint)) != 1 {
		return Claims{}, ErrGrantFingerprintMismatch
	}
	if err := validateClaims(claims); err != nil {
		return Claims{}, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	if now.Before(claims.NotBefore) {
		return Claims{}, ErrGrantNotActive
	}
	if !now.Before(claims.ExpiresAt) {
		return Claims{}, ErrGrantExpired
	}
	if revocations != nil && revocations.Revoked(claims.RevocationID) {
		return Claims{}, ErrGrantRevoked
	}
	return claims, nil
}

func normalizeClaims(claims Claims) Claims {
	claims.GrantID = strings.TrimSpace(claims.GrantID)
	claims.IssuerDeviceID = strings.TrimSpace(claims.IssuerDeviceID)
	claims.IssuerDeviceFingerprint = strings.TrimSpace(claims.IssuerDeviceFingerprint)
	claims.Scope.TerminalID = strings.TrimSpace(claims.Scope.TerminalID)
	claims.IssuedAt = claims.IssuedAt.UTC()
	if !claims.NotBefore.IsZero() {
		claims.NotBefore = claims.NotBefore.UTC()
	}
	claims.ExpiresAt = claims.ExpiresAt.UTC()
	claims.RevocationID = strings.TrimSpace(claims.RevocationID)
	claims.Nonce = strings.TrimSpace(claims.Nonce)
	return claims
}

func validateClaims(claims Claims) error {
	if claims.Version != 1 {
		return fmt.Errorf("%w: unsupported version %d", ErrGrantMalformed, claims.Version)
	}
	if claims.GrantID == "" {
		return fmt.Errorf("%w: requires grant_id", ErrGrantMalformed)
	}
	if claims.IssuerDeviceID == "" {
		return fmt.Errorf("%w: requires issuer_device_id", ErrGrantMalformed)
	}
	if claims.IssuerDeviceFingerprint == "" {
		return fmt.Errorf("%w: requires issuer_device_fingerprint", ErrGrantMalformed)
	}
	if claims.RevocationID == "" || claims.Nonce == "" {
		return fmt.Errorf("%w: requires revocation_id and nonce", ErrGrantMalformed)
	}
	capabilities := 0
	if claims.Scope.AllowDaemon {
		capabilities++
	}
	if claims.Scope.TerminalID != "" {
		capabilities++
	}
	if claims.Scope.MachineEventsOnly {
		capabilities++
	}
	if capabilities == 0 {
		return fmt.Errorf("%w: requires explicit scope", ErrGrantScopeInvalid)
	}
	if capabilities != 1 {
		return fmt.Errorf("%w: scopes are mutually exclusive", ErrGrantScopeInvalid)
	}
	if (claims.Scope.FileReadMetadata || claims.Scope.FileReadContent || claims.Scope.FileWriteContent || claims.Scope.FileMutate) && !claims.Scope.AllowDaemon {
		return fmt.Errorf("%w: file permissions require daemon scope", ErrGrantScopeInvalid)
	}
	if claims.IssuedAt.IsZero() || claims.NotBefore.IsZero() || claims.ExpiresAt.IsZero() ||
		claims.NotBefore.Before(claims.IssuedAt) || !claims.ExpiresAt.After(claims.NotBefore) {
		return fmt.Errorf("%w: requires valid issued_at, not_before and expires_at", ErrGrantMalformed)
	}
	return nil
}
