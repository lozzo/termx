// Package servicecredential 签发和验证 Control Plane 的短期服务凭据。
//
// v1 固定使用 Ed25519，不暴露 caller-selected algorithm。Hub admission、Relay lease
// 与 usage event 使用不同 domain separator，避免一种凭据被另一服务误接受。
package servicecredential

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	// ErrUnknownKey 表示凭据引用了 verifier 未配置的 key ID。
	ErrUnknownKey = errors.New("unknown service credential signing key")
	// ErrRevokedKey 表示签名 key 已被紧急吊销。
	ErrRevokedKey = errors.New("revoked service credential signing key")
	// ErrKeyWindow 表示签名时间或验签时间不在 key 的有效窗口内。
	ErrKeyWindow = errors.New("service credential signing key outside validity window")
	// ErrInvalidSignature 表示 Ed25519 签名不匹配。
	ErrInvalidSignature = errors.New("invalid service credential signature")
)

// VerificationKey 是分发给 Hub、Relay 和 usage collector 的 Control Plane 公钥记录。
// NotBefore/NotAfter 支持轮换时重叠验证窗口；Revoked 提供紧急全局拒绝。
type VerificationKey struct {
	ID        string
	PublicKey ed25519.PublicKey
	NotBefore time.Time
	NotAfter  time.Time
	Revoked   bool
}

// Signer 持有 Control Plane 当前 active Ed25519 私钥。
// 私钥只存在 issuer 进程内；Signer 的 String 表示不会泄漏 key material。
type Signer struct {
	keyID      string
	privateKey ed25519.PrivateKey
	notBefore  time.Time
	notAfter   time.Time
}

// NewSigner 创建固定算法的服务凭据签名器。
// key ID、私钥长度或有效窗口非法时返回错误，防止用零值或长期无界 key 签发凭据。
func NewSigner(keyID string, privateKey ed25519.PrivateKey, notBefore, notAfter time.Time) (Signer, error) {
	if keyID == "" || len(privateKey) != ed25519.PrivateKeySize || !notAfter.After(notBefore) {
		return Signer{}, fmt.Errorf("invalid Ed25519 signing key")
	}
	return Signer{
		keyID:      keyID,
		privateKey: append(ed25519.PrivateKey(nil), privateKey...),
		notBefore:  notBefore.UTC(),
		notAfter:   notAfter.UTC(),
	}, nil
}

// KeyID 返回凭据内写入的非秘密 key reference。
func (signer Signer) KeyID() string {
	return signer.keyID
}

// PublicKey 返回可离线分发的验签 key 副本。
// 调用方可以把旧 key 与新 key 同时放入 KeyRing，形成轮换重叠窗口。
func (signer Signer) PublicKey() VerificationKey {
	publicKey := signer.privateKey.Public().(ed25519.PublicKey)
	return VerificationKey{
		ID:        signer.keyID,
		PublicKey: append(ed25519.PublicKey(nil), publicKey...),
		NotBefore: signer.notBefore,
		NotAfter:  signer.notAfter,
	}
}

// Sign 对已经带 domain separator 的 canonical bytes 生成 Ed25519 签名。
// 签发时间必须位于 key window 内；调用方不能选择或降级签名算法。
func (signer Signer) Sign(canonical []byte, issuedAt time.Time) ([]byte, error) {
	issuedAt = issuedAt.UTC()
	if issuedAt.Before(signer.notBefore) || !issuedAt.Before(signer.notAfter) {
		return nil, ErrKeyWindow
	}
	return ed25519.Sign(signer.privateKey, canonical), nil
}

// String 返回脱敏描述，避免结构化日志意外记录私钥。
func (signer Signer) String() string {
	return "servicecredential.Signer{key_id=" + signer.keyID + ", private_key=[REDACTED]}"
}

// KeyRing 是 Hub、Relay 和 usage collector 使用的并发安全离线验签 key 集。
// ReplaceKeys 用于正常轮换，Revoke 用于无需等待 TTL 的紧急 key 吊销。
type KeyRing struct {
	mu   sync.RWMutex
	keys map[string]VerificationKey
}

// NewKeyRing 建立验签 key 集并复制所有 public key bytes。
// 重复或非法 key ID 会返回错误，避免部署配置中静默覆盖。
func NewKeyRing(keys ...VerificationKey) (*KeyRing, error) {
	ring := &KeyRing{}
	if err := ring.ReplaceKeys(keys...); err != nil {
		return nil, err
	}
	return ring, nil
}

// ReplaceKeys 原子替换离线验签集合。
// 轮换期间应同时提供旧 key 与新 key，直到旧凭据 TTL 和允许时钟偏差全部结束。
func (ring *KeyRing) ReplaceKeys(keys ...VerificationKey) error {
	next := make(map[string]VerificationKey, len(keys))
	for _, key := range keys {
		if key.ID == "" || len(key.PublicKey) != ed25519.PublicKeySize || !key.NotAfter.After(key.NotBefore) {
			return fmt.Errorf("invalid verification key %q", key.ID)
		}
		if _, exists := next[key.ID]; exists {
			return fmt.Errorf("duplicate verification key %q", key.ID)
		}
		key.PublicKey = append(ed25519.PublicKey(nil), key.PublicKey...)
		key.NotBefore = key.NotBefore.UTC()
		key.NotAfter = key.NotAfter.UTC()
		next[key.ID] = key
	}
	ring.mu.Lock()
	ring.keys = next
	ring.mu.Unlock()
	return nil
}

// Revoke 紧急吊销指定 key。
// 已签发但尚未过期的 admission、lease 和 usage event 会立即验签失败。
func (ring *KeyRing) Revoke(keyID string) error {
	ring.mu.Lock()
	defer ring.mu.Unlock()
	key, ok := ring.keys[keyID]
	if !ok {
		return ErrUnknownKey
	}
	key.Revoked = true
	ring.keys[keyID] = key
	return nil
}

// Verify 使用 key ID、验证时刻和固定 Ed25519 算法验证 canonical bytes。
// KeyRing 不解析业务 claims，调用方仍需验证 audience、session、TTL 和 operation。
func (ring *KeyRing) Verify(keyID string, canonical, signature []byte, now time.Time) error {
	if ring == nil {
		return ErrUnknownKey
	}
	ring.mu.RLock()
	key, ok := ring.keys[keyID]
	ring.mu.RUnlock()
	if !ok {
		return ErrUnknownKey
	}
	if key.Revoked {
		return ErrRevokedKey
	}
	now = now.UTC()
	if now.Before(key.NotBefore) || !now.Before(key.NotAfter) {
		return ErrKeyWindow
	}
	if !ed25519.Verify(key.PublicKey, canonical, signature) {
		return ErrInvalidSignature
	}
	return nil
}
