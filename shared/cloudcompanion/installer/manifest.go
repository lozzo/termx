// Package installer 实现公开 CLI 使用的 Cloud Companion 签名安装与原子版本切换。
//
// 该包只接受预配置 HTTPS origin 和 Ed25519 release root；Hub、Relay、endpoint 配置
// 与下载响应都不能改变 executable origin、签名 key 或本地激活路径。
package installer

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/muxvia/muxvia/proto/cloudpb"
	"github.com/muxvia/muxvia/shared/cloudcompanion"
	"golang.org/x/mod/semver"
)

const manifestSchemaVersion = 1

const maxArchiveBytes int64 = 256 << 20

// Manifest 是官方发布服务签名的单平台 Cloud Companion artifact 描述。
// Signature 覆盖除自身外的全部字段；ArchiveSHA256 和 ArchiveSize 约束下载 bytes，不能只信任 HTTPS。
type Manifest struct {
	SchemaVersion        uint32 `json:"schema_version"`
	Channel              string `json:"channel"`
	Version              string `json:"version"`
	OS                   string `json:"os"`
	Arch                 string `json:"arch"`
	DownloadURL          string `json:"download_url"`
	ArchiveSHA256        string `json:"archive_sha256"`
	ArchiveSize          int64  `json:"archive_size"`
	SigningKeyID         string `json:"signing_key_id"`
	MinCompanionProtocol uint32 `json:"min_companion_protocol"`
	MaxCompanionProtocol uint32 `json:"max_companion_protocol"`
	PublishedAt          string `json:"published_at"`
	AllowDowngrade       bool   `json:"allow_downgrade,omitempty"`
	Signature            string `json:"signature"`
}

type unsignedManifest struct {
	SchemaVersion        uint32 `json:"schema_version"`
	Channel              string `json:"channel"`
	Version              string `json:"version"`
	OS                   string `json:"os"`
	Arch                 string `json:"arch"`
	DownloadURL          string `json:"download_url"`
	ArchiveSHA256        string `json:"archive_sha256"`
	ArchiveSize          int64  `json:"archive_size"`
	SigningKeyID         string `json:"signing_key_id"`
	MinCompanionProtocol uint32 `json:"min_companion_protocol"`
	MaxCompanionProtocol uint32 `json:"max_companion_protocol"`
	PublishedAt          string `json:"published_at"`
	AllowDowngrade       bool   `json:"allow_downgrade,omitempty"`
}

// Verification 固定 installer 期望的平台、channel、protocol window、时钟和 trusted release roots。
// TrustedKeys 由官方 muxvia build 注入；空 key set 必须 fail closed，不能从 manifest 或网络补充。
type Verification struct {
	Channel     string
	OS          string
	Arch        string
	ProtocolMin uint32
	ProtocolMax uint32
	Now         time.Time
	TrustedKeys map[string]ed25519.PublicKey
}

// ParseAndVerifyManifest 严格解析并验签 release manifest。
// 未知字段、尾随 JSON、未知 key、错平台、错 channel、无 protocol 交集和非法 semver 全部返回稳定不可信/不兼容错误。
func ParseAndVerifyManifest(payload []byte, expected Verification) (Manifest, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, untrustedError("Cloud Companion release manifest is malformed")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Manifest{}, untrustedError("Cloud Companion release manifest has trailing data")
	}
	if err := validateManifest(manifest, expected); err != nil {
		return Manifest{}, err
	}
	publicKey := expected.TrustedKeys[manifest.SigningKeyID]
	signature, err := base64.RawURLEncoding.DecodeString(manifest.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize || len(publicKey) != ed25519.PublicKeySize {
		return Manifest{}, untrustedError("Cloud Companion release signature metadata is invalid")
	}
	signingBytes, err := ManifestSigningBytes(manifest)
	if err != nil || !ed25519.Verify(publicKey, signingBytes, signature) {
		return Manifest{}, untrustedError("Cloud Companion release signature verification failed")
	}
	return manifest, nil
}

// ManifestSigningBytes 返回 Manifest 的 canonical Ed25519 签名输入。
// 字段顺序由固定 struct 决定，Signature 永不进入输入；该函数供独立私有 release tooling 复用。
func ManifestSigningBytes(manifest Manifest) ([]byte, error) {
	return json.Marshal(unsignedManifest{
		SchemaVersion: manifest.SchemaVersion, Channel: manifest.Channel, Version: manifest.Version,
		OS: manifest.OS, Arch: manifest.Arch, DownloadURL: manifest.DownloadURL,
		ArchiveSHA256: manifest.ArchiveSHA256, ArchiveSize: manifest.ArchiveSize,
		SigningKeyID: manifest.SigningKeyID, MinCompanionProtocol: manifest.MinCompanionProtocol,
		MaxCompanionProtocol: manifest.MaxCompanionProtocol, PublishedAt: manifest.PublishedAt,
		AllowDowngrade: manifest.AllowDowngrade,
	})
}

// MarshalSignedManifest 使用显式 private key 生成 canonical release manifest JSON。
// 该函数只供 private release tooling 和测试 harness；公开 CLI 不持有或生成 release signing private key。
func MarshalSignedManifest(manifest Manifest, privateKey ed25519.PrivateKey) ([]byte, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("Ed25519 release signing private key is required")
	}
	manifest.Signature = ""
	signingBytes, err := ManifestSigningBytes(manifest)
	if err != nil {
		return nil, err
	}
	manifest.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(privateKey, signingBytes))
	return json.Marshal(manifest)
}

func validateManifest(manifest Manifest, expected Verification) error {
	if manifest.SchemaVersion != manifestSchemaVersion || manifest.Channel != expected.Channel || manifest.OS != expected.OS || manifest.Arch != expected.Arch {
		return untrustedError("Cloud Companion release platform or channel does not match this request")
	}
	version := canonicalVersion(manifest.Version)
	if version == "" || version != manifest.Version || manifest.Channel == "stable" && semver.Prerelease(version) != "" {
		return untrustedError("Cloud Companion release version is invalid for its channel")
	}
	if manifest.Channel != "stable" && manifest.Channel != "beta" {
		return untrustedError("Cloud Companion release channel is unsupported")
	}
	if manifest.ArchiveSize <= 0 || manifest.ArchiveSize > maxArchiveBytes {
		return untrustedError("Cloud Companion archive size is outside the allowed range")
	}
	digest, err := hex.DecodeString(manifest.ArchiveSHA256)
	if err != nil || len(digest) != 32 || strings.ToLower(manifest.ArchiveSHA256) != manifest.ArchiveSHA256 {
		return untrustedError("Cloud Companion archive SHA-256 is invalid")
	}
	if manifest.SigningKeyID == "" || len(expected.TrustedKeys) == 0 {
		return untrustedError("Cloud Companion release root is not configured")
	}
	if manifest.MinCompanionProtocol == 0 || manifest.MaxCompanionProtocol < manifest.MinCompanionProtocol || expected.ProtocolMin == 0 || expected.ProtocolMax < expected.ProtocolMin || manifest.MinCompanionProtocol > expected.ProtocolMax || manifest.MaxCompanionProtocol < expected.ProtocolMin {
		return cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_INCOMPATIBLE, "Cloud Companion release has no compatible IPC protocol")
	}
	publishedAt, err := time.Parse(time.RFC3339, manifest.PublishedAt)
	if err != nil || publishedAt.After(expected.Now.Add(5*time.Minute)) {
		return untrustedError("Cloud Companion release timestamp is invalid")
	}
	if strings.TrimSpace(manifest.DownloadURL) == "" || strings.TrimSpace(manifest.Signature) == "" {
		return untrustedError("Cloud Companion release manifest is incomplete")
	}
	return nil
}

func canonicalVersion(version string) string {
	if !strings.HasPrefix(version, "v") {
		return ""
	}
	return semver.Canonical(version)
}

func untrustedError(message string) error {
	return cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_UNTRUSTED, message)
}
