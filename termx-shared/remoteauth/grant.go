// Package remoteauth 定义 Hub/P2P 连接使用的设备身份与 capability grant contract。
package remoteauth

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

const grantPrefix = "termx-grant-v1"

// Scope 描述一条获授权 protocol session 可见的 core-v2 能力边界。
// 当前只允许单 terminal session 或 machine-events-only session；空 scope 不得生成无限制远程 daemon 会话。
type Scope struct {
	TerminalID        string `json:"terminal_id,omitempty"`
	MachineEventsOnly bool   `json:"machine_events_only,omitempty"`
}

// Claims 是 remote daemon 签发并验证的 capability grant 内容。
// GrantID 是撤销主键，DeviceID 是 hub 发现目标，DeviceFingerprint 是签名公钥推导出的安全身份；Hub 不读取或修改这些授权字段。
type Claims struct {
	GrantID           string    `json:"grant_id"`
	DeviceID          string    `json:"device_id"`
	DeviceFingerprint string    `json:"device_fingerprint"`
	Scope             Scope     `json:"scope"`
	IssuedAt          time.Time `json:"issued_at"`
	ExpiresAt         time.Time `json:"expires_at"`
}

// RevocationChecker 是 daemon-local grant 撤销查询边界。
// Hub/relay 不拥有撤销真值；连接建立和重连时由签发设备查询本地 store。
type RevocationChecker interface {
	Revoked(grantID string) bool
}

// Revocations 是进程内 grant 撤销集合。
// 它适用于 transport contract 与 daemon runtime；持久化策略由后续 daemon agent 装配切片决定。
type Revocations struct {
	mu     sync.RWMutex
	grants map[string]struct{}
}

// NewRevocations 创建空的 daemon-local 撤销集合。
func NewRevocations() *Revocations {
	return &Revocations{grants: map[string]struct{}{}}
}

// Revoke 按 grant ID 撤销 capability。
// 空 ID 会被忽略，撤销不会影响其他 grant 或 endpoint。
func (revocations *Revocations) Revoke(grantID string) {
	if revocations == nil {
		return
	}
	grantID = strings.TrimSpace(grantID)
	if grantID == "" {
		return
	}
	revocations.mu.Lock()
	revocations.grants[grantID] = struct{}{}
	revocations.mu.Unlock()
}

// Revoked 返回指定 grant 是否已由签发 daemon 撤销。
func (revocations *Revocations) Revoked(grantID string) bool {
	if revocations == nil {
		return false
	}
	revocations.mu.RLock()
	_, ok := revocations.grants[strings.TrimSpace(grantID)]
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
		return "", fmt.Errorf("remote capability grant requires ed25519 private key")
	}
	publicKey := privateKey.Public().(ed25519.PublicKey)
	claims = normalizeClaims(claims)
	claims.DeviceFingerprint = Fingerprint(publicKey)
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
		return Claims{}, fmt.Errorf("invalid remote capability grant format")
	}
	publicKeyBytes, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(publicKeyBytes) != ed25519.PublicKeySize {
		return Claims{}, fmt.Errorf("invalid remote capability grant public key")
	}
	publicKey := ed25519.PublicKey(publicKeyBytes)
	fingerprint := Fingerprint(publicKey)
	if subtle.ConstantTimeCompare([]byte(fingerprint), []byte(strings.TrimSpace(expectedFingerprint))) != 1 {
		return Claims{}, fmt.Errorf("remote capability grant fingerprint mismatch")
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil || !ed25519.Verify(publicKey, []byte(strings.Join(parts[:3], ".")), signature) {
		return Claims{}, fmt.Errorf("invalid remote capability grant signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, fmt.Errorf("decode remote capability grant payload: %w", err)
	}
	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return Claims{}, fmt.Errorf("decode remote capability grant claims: %w", err)
	}
	claims = normalizeClaims(claims)
	if claims.DeviceFingerprint != fingerprint {
		return Claims{}, fmt.Errorf("remote capability grant signed fingerprint mismatch")
	}
	if err := validateClaims(claims); err != nil {
		return Claims{}, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	if now.Before(claims.IssuedAt) {
		return Claims{}, fmt.Errorf("remote capability grant is not active yet")
	}
	if !now.Before(claims.ExpiresAt) {
		return Claims{}, fmt.Errorf("remote capability grant expired")
	}
	if revocations != nil && revocations.Revoked(claims.GrantID) {
		return Claims{}, fmt.Errorf("remote capability grant revoked")
	}
	return claims, nil
}

func normalizeClaims(claims Claims) Claims {
	claims.GrantID = strings.TrimSpace(claims.GrantID)
	claims.DeviceID = strings.TrimSpace(claims.DeviceID)
	claims.DeviceFingerprint = strings.TrimSpace(claims.DeviceFingerprint)
	claims.Scope.TerminalID = strings.TrimSpace(claims.Scope.TerminalID)
	claims.IssuedAt = claims.IssuedAt.UTC()
	claims.ExpiresAt = claims.ExpiresAt.UTC()
	return claims
}

func validateClaims(claims Claims) error {
	if claims.GrantID == "" {
		return fmt.Errorf("remote capability grant requires grant_id")
	}
	if claims.DeviceID == "" {
		return fmt.Errorf("remote capability grant requires device_id")
	}
	if claims.DeviceFingerprint == "" {
		return fmt.Errorf("remote capability grant requires device_fingerprint")
	}
	if claims.Scope.TerminalID == "" && !claims.Scope.MachineEventsOnly {
		return fmt.Errorf("remote capability grant requires restricted scope")
	}
	if claims.Scope.TerminalID != "" && claims.Scope.MachineEventsOnly {
		return fmt.Errorf("remote capability grant scope cannot combine terminal and machine events")
	}
	if claims.IssuedAt.IsZero() || claims.ExpiresAt.IsZero() || !claims.ExpiresAt.After(claims.IssuedAt) {
		return fmt.Errorf("remote capability grant requires valid issued_at and expires_at")
	}
	return nil
}
