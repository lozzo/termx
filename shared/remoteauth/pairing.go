package remoteauth

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// PairingBundleVersion 是当前 daemon-to-client capability 传输 envelope 版本。
// 导入端只接受该精确版本，不能尝试旧 token、Hub URL payload 或宽松字段 fallback。
const PairingBundleVersion uint32 = 1

const defaultPairingLifetime = 24 * time.Hour

// PairingBundle 是 daemon 交给受信客户端的一次 capability 传输包。
// CapabilityGrant 是 bearer secret，只能通过用户选择的安全通道传递；导入后必须进入 CredentialStore，connections.yaml 只保存生成的 grant_ref。
type PairingBundle struct {
	Version           uint32 `json:"version"`
	Label             string `json:"label,omitempty"`
	DeviceID          string `json:"device_id"`
	DeviceFingerprint string `json:"device_fingerprint"`
	CapabilityGrant   string `json:"capability_grant"`
}

// PairingIssueOptions 描述 daemon-local grant 签发输入。
// Scope 决定 core-v2 protocol 能力；Lifetime 只控制 bearer 有效期，不能被云账号、Hub admission 或 Relay lease 扩大。
type PairingIssueOptions struct {
	Label    string
	Scope    Scope
	Lifetime time.Duration
	Now      time.Time
	Random   io.Reader
}

// IssuePairingBundle 使用当前 daemon DeviceIdentity 签发可导入的 pairing bundle。
// 私钥不进入结果；随机源和时间只允许 deterministic harness 注入，生产零值使用 crypto/rand 与 UTC 当前时间。
func IssuePairingBundle(identity Identity, options PairingIssueOptions) (PairingBundle, error) {
	if err := identity.Validate(); err != nil {
		return PairingBundle{}, fmt.Errorf("issue pairing bundle: %w", err)
	}
	now := options.Now.UTC()
	if options.Now.IsZero() {
		now = time.Now().UTC()
	}
	lifetime := options.Lifetime
	if lifetime == 0 {
		lifetime = defaultPairingLifetime
	}
	if lifetime <= 0 {
		return PairingBundle{}, fmt.Errorf("pairing lifetime must be positive")
	}
	randomSource := options.Random
	if randomSource == nil {
		randomSource = rand.Reader
	}
	randomID := make([]byte, 18)
	if _, err := io.ReadFull(randomSource, randomID); err != nil {
		return PairingBundle{}, fmt.Errorf("generate pairing grant id: %w", err)
	}
	grantID := "grant-" + base64.RawURLEncoding.EncodeToString(randomID)
	grant, err := Issue(identity.PrivateKey, Claims{
		GrantID: grantID, IssuerDeviceID: identity.DeviceID, Scope: options.Scope,
		IssuedAt: now, ExpiresAt: now.Add(lifetime), RevocationID: grantID,
	})
	if err != nil {
		return PairingBundle{}, err
	}
	return PairingBundle{
		Version: PairingBundleVersion, Label: strings.TrimSpace(options.Label), DeviceID: identity.DeviceID,
		DeviceFingerprint: identity.Fingerprint, CapabilityGrant: grant,
	}, nil
}

// EncodePairingBundle 校验必填 envelope 字段并编码 pairing bundle。
// 返回数据包含 bearer grant，调用方不得写入日志、connections.yaml 或 cloud signaling；签名、scope 和有效期在 ParsePairingBundle 导入边界再次完整验证。
func EncodePairingBundle(bundle PairingBundle) ([]byte, error) {
	if err := validatePairingEnvelope(bundle); err != nil {
		return nil, err
	}
	payload, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode pairing bundle: %w", err)
	}
	return append(payload, '\n'), nil
}

// ParsePairingBundle 严格解析并验证 daemon identity、grant 签名、scope 和有效期。
// 失败时调用方不得写 credential store、更新 endpoint registry 或尝试旧 pairing/token 格式。
func ParsePairingBundle(payload []byte, now time.Time) (PairingBundle, Claims, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var bundle PairingBundle
	if err := decoder.Decode(&bundle); err != nil {
		return PairingBundle{}, Claims{}, fmt.Errorf("decode pairing bundle: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return PairingBundle{}, Claims{}, fmt.Errorf("decode pairing bundle: trailing data")
	}
	claims, err := validatePairingBundle(bundle, now)
	if err != nil {
		return PairingBundle{}, Claims{}, err
	}
	bundle.Label = strings.TrimSpace(bundle.Label)
	bundle.DeviceID = strings.TrimSpace(bundle.DeviceID)
	bundle.DeviceFingerprint = strings.TrimSpace(bundle.DeviceFingerprint)
	bundle.CapabilityGrant = strings.TrimSpace(bundle.CapabilityGrant)
	return bundle, claims, nil
}

func validatePairingBundle(bundle PairingBundle, now time.Time) (Claims, error) {
	if err := validatePairingEnvelope(bundle); err != nil {
		return Claims{}, err
	}
	claims, err := Verify(bundle.CapabilityGrant, bundle.DeviceFingerprint, now, nil)
	if err != nil {
		return Claims{}, fmt.Errorf("verify pairing bundle grant: %w", err)
	}
	if claims.IssuerDeviceID != strings.TrimSpace(bundle.DeviceID) || claims.IssuerDeviceFingerprint != strings.TrimSpace(bundle.DeviceFingerprint) {
		return Claims{}, fmt.Errorf("pairing bundle device identity does not match capability issuer")
	}
	return claims, nil
}

func validatePairingEnvelope(bundle PairingBundle) error {
	if bundle.Version != PairingBundleVersion || strings.TrimSpace(bundle.DeviceID) == "" || strings.TrimSpace(bundle.DeviceFingerprint) == "" || strings.TrimSpace(bundle.CapabilityGrant) == "" {
		return fmt.Errorf("pairing bundle is incomplete or unsupported")
	}
	return nil
}
