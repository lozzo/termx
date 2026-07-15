// Package session 管理 Cloud Companion 的账号与 daemon device 云会话。
//
// 持久化只能通过平台 OS credential store adapter；本包不提供明文文件、环境变量
// 或内存 fallback。公开 termx 配置只保存 profile reference，不保存 AccountAccessToken。
package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"time"
)

const sessionSchemaVersion = 2

var (
	// ErrNotFound 表示当前 companion profile 尚未登录或完成 daemon enrollment。
	ErrNotFound = errors.New("cloud companion session not found")
	// ErrExpired 表示 OS credential store 中的云会话已经过期。
	ErrExpired = errors.New("cloud companion session expired")
	// ErrInvalid 表示 credential store 内容损坏、版本未知或缺少安全字段。
	ErrInvalid = errors.New("invalid cloud companion session")
)

// OSCredentialStore 是平台 Keychain、Keystore 或 Credential Manager adapter 的最小接口。
// 实现必须由 owning user ACL 保护并复制输入 bytes；失败不得 fallback 到普通文件或环境变量。
type OSCredentialStore interface {
	// LoadSecret 读取 profile 对应的 opaque secret bytes；缺失时返回 ErrNotFound。
	LoadSecret(context.Context, string) ([]byte, error)
	// StoreSecret 原子覆盖 profile secret；实现不得记录 value 或把它放入命令行参数。
	StoreSecret(context.Context, string, []byte) error
	// DeleteSecret 删除 profile secret；重复删除应保持幂等或返回 ErrNotFound。
	DeleteSecret(context.Context, string) error
}

// Kind 区分交互账号登录会话与 headless daemon device enrollment 会话。
// 两者不能跨 caller role 使用，也不能替代 DeviceIdentity 或 CapabilityGrant。
type Kind string

const (
	// KindAccount 表示 TUI、CLI 或移动客户端使用的账号云会话。
	KindAccount Kind = "account"
	// KindDevice 表示 daemon 使用的设备 enrollment 云会话。
	KindDevice Kind = "device"
)

// Metadata 是可以投影到 Status 的非秘密会话信息。
// 它不包含 token body、refresh secret、Relay credential 或 terminal 数据。
type Metadata struct {
	Kind                Kind
	AccountID           string
	AccountLabel        string
	DeviceID            string
	ExpiresAt           time.Time
	HubID               string
	HubURL              string
	HubRegion           string
	HubDirectoryVersion uint64
}

// Authorization 是 companion 调用私有 Control Plane/Hub 时使用的短期账号或设备凭据。
// String 永远脱敏；只有私有 service adapter 可以显式调用 Bytes 生成 TLS request authorization。
type Authorization struct {
	raw      []byte
	metadata Metadata
}

// RefreshAuthorization 是 Companion 仅向 Control Plane 提交的轮换凭据。
// 它绝不能发送到 Hub、公开 IPC、WebView、日志或配置；String 始终脱敏。
type RefreshAuthorization struct {
	raw      []byte
	metadata Metadata
}

// Bytes 返回 refresh secret 副本供私有 Control Plane adapter 发送。
// adapter 必须在请求结束后清理副本。
func (authorization RefreshAuthorization) Bytes() []byte {
	return append([]byte(nil), authorization.raw...)
}

// Metadata 返回 refresh credential 绑定的账号、设备和会话类型。
func (authorization RefreshAuthorization) Metadata() Metadata { return authorization.metadata }

// Destroy 清理当前 refresh secret 副本。
func (authorization *RefreshAuthorization) Destroy() {
	if authorization == nil {
		return
	}
	clear(authorization.raw)
	authorization.raw = nil
}

// String 返回固定脱敏文本。
func (authorization RefreshAuthorization) String() string {
	return "CloudRefreshAuthorization{[REDACTED]}"
}

// Bytes 返回 token 副本供私有网络 adapter 使用。
// 调用方应在请求完成后尽快清理副本，禁止写日志、URL、metric label 或公开 IPC。
func (authorization Authorization) Bytes() []byte {
	return append([]byte(nil), authorization.raw...)
}

// Destroy 清理当前 Authorization 持有的 token bytes。
// Companion 在同步 adapter 调用返回后必须立即调用；adapter 若需异步续期必须从 OS store 重新取得新会话。
func (authorization *Authorization) Destroy() {
	if authorization == nil {
		return
	}
	clear(authorization.raw)
	authorization.raw = nil
}

// String 返回固定脱敏文本，避免 fmt 或结构化 logger 泄漏账号 token。
func (authorization Authorization) String() string {
	return "CloudAuthorization{[REDACTED]}"
}

// Metadata 返回与 token 同源的非秘密账号、设备和会话类型绑定。
// 私有 adapter 可将其作为请求上下文发送，服务端仍必须以签名 token 验证，不能信任该值覆盖 claims。
func (authorization Authorization) Metadata() Metadata {
	return authorization.metadata
}

// Session 是从 OS credential store 解出的 companion 私有会话。
// accessToken 不导出；Metadata 可以进入 Status，Authorization 只能交给私有云 adapter。
type Session struct {
	metadata         Metadata
	accessToken      []byte
	refreshToken     []byte
	refreshExpiresAt time.Time
}

// New 创建待写入 OS credential store 的云会话。
// token 会被复制；kind、账号、expiry 或 token 非法时 fail closed。
func New(metadata Metadata, accessToken []byte, now time.Time) (Session, error) {
	return newSession(metadata, accessToken, nil, time.Time{}, now)
}

// NewRefreshable 创建同时包含短期 edge token 和长期轮换 secret 的私有会话。
// refresh secret 只会随整个 Session 写入 OS credential store，不进入 Metadata 或公开状态。
func NewRefreshable(metadata Metadata, accessToken, refreshToken []byte, refreshExpiresAt, now time.Time) (Session, error) {
	return newSession(metadata, accessToken, refreshToken, refreshExpiresAt, now)
}

func newSession(metadata Metadata, accessToken, refreshToken []byte, refreshExpiresAt, now time.Time) (Session, error) {
	metadata.ExpiresAt = metadata.ExpiresAt.UTC()
	if metadata.Kind != KindAccount && metadata.Kind != KindDevice || metadata.AccountID == "" || metadata.ExpiresAt.IsZero() || !now.Before(metadata.ExpiresAt) || len(accessToken) == 0 {
		return Session{}, ErrInvalid
	}
	if metadata.Kind == KindDevice && metadata.DeviceID == "" {
		return Session{}, ErrInvalid
	}
	if len(refreshToken) > 0 {
		refreshExpiresAt = refreshExpiresAt.UTC()
		if len(refreshToken) < 32 || !now.Before(refreshExpiresAt) || !refreshExpiresAt.After(metadata.ExpiresAt) {
			return Session{}, ErrInvalid
		}
	} else if !refreshExpiresAt.IsZero() {
		return Session{}, ErrInvalid
	}
	if metadata.HubID != "" || metadata.HubURL != "" || metadata.HubRegion != "" || metadata.HubDirectoryVersion != 0 {
		parsed, err := url.Parse(metadata.HubURL)
		if metadata.HubID == "" || metadata.HubRegion == "" || metadata.HubDirectoryVersion == 0 || err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" || parsed.Scheme != "http" && parsed.Scheme != "https" {
			return Session{}, ErrInvalid
		}
	}
	return Session{metadata: metadata, accessToken: append([]byte(nil), accessToken...), refreshToken: append([]byte(nil), refreshToken...), refreshExpiresAt: refreshExpiresAt}, nil
}

// Metadata 返回非秘密会话信息副本。
func (session Session) Metadata() Metadata {
	return session.metadata
}

// Authorization 返回私有网络 adapter 使用的 token 副本。
// 该方法不能被公开 IPC response、Status 或 diagnostics 调用。
func (session Session) Authorization() Authorization {
	return Authorization{raw: append([]byte(nil), session.accessToken...), metadata: session.metadata}
}

// RefreshAuthorization 返回只供 Control Plane refresh endpoint 使用的 secret 副本。
// 没有 refresh credential 的测试/旧会话返回 ErrNotFound。
func (session Session) RefreshAuthorization(now time.Time) (RefreshAuthorization, error) {
	if len(session.refreshToken) == 0 {
		return RefreshAuthorization{}, ErrNotFound
	}
	if !now.Before(session.refreshExpiresAt) {
		return RefreshAuthorization{}, ErrExpired
	}
	return RefreshAuthorization{raw: append([]byte(nil), session.refreshToken...), metadata: session.metadata}, nil
}

// Destroy 清理当前 Session 实例持有的 token bytes。
// 该操作不删除 OS credential store；Logout 应调用 Manager.Delete。
func (session *Session) Destroy() {
	if session == nil {
		return
	}
	clear(session.accessToken)
	session.accessToken = nil
	clear(session.refreshToken)
	session.refreshToken = nil
}

// String 返回脱敏会话摘要，只包含非秘密 identity reference 和 expiry。
func (session Session) String() string {
	return fmt.Sprintf("CloudSession{kind=%s account_id=%s device_id=%s expires_at=%s token=[REDACTED]}", session.metadata.Kind, session.metadata.AccountID, session.metadata.DeviceID, session.metadata.ExpiresAt.Format(time.RFC3339))
}

// Manager 把 profile-scoped cloud session 序列化到 OS credential store。
// profile 是固定 companion 配置引用，不是 endpoint label、Hub URL 或账号 token。
type Manager struct {
	store   OSCredentialStore
	profile string
}

// NewManager 创建 session manager。
// store 或 profile 缺失时返回错误；不创建任何不安全的持久化 fallback。
func NewManager(store OSCredentialStore, profile string) (*Manager, error) {
	if store == nil || profile == "" {
		return nil, fmt.Errorf("OS credential store and profile are required")
	}
	return &Manager{store: store, profile: profile}, nil
}

type wireSession struct {
	Version             uint32 `json:"version"`
	Kind                Kind   `json:"kind"`
	AccountID           string `json:"account_id"`
	AccountLabel        string `json:"account_label,omitempty"`
	DeviceID            string `json:"device_id,omitempty"`
	ExpiresAt           int64  `json:"expires_at_unix"`
	AccessToken         []byte `json:"access_token"`
	HubID               string `json:"hub_id,omitempty"`
	HubURL              string `json:"hub_url,omitempty"`
	HubRegion           string `json:"hub_region,omitempty"`
	HubDirectoryVersion uint64 `json:"hub_directory_version,omitempty"`
	RefreshToken        []byte `json:"refresh_token,omitempty"`
	RefreshExpiresAt    int64  `json:"refresh_expires_at_unix,omitempty"`
}

// Save 原子写入账号或 device 云会话。
// JSON bytes 只作为 OS secret value，写入返回后立即清理本地编码缓冲区。
func (manager *Manager) Save(ctx context.Context, session Session, now time.Time) error {
	metadata := session.Metadata()
	if current, err := manager.Load(ctx, metadata.Kind, now); err == nil {
		currentMetadata := current.Metadata()
		current.Destroy()
		if currentMetadata.HubID != "" && (metadata.HubID != currentMetadata.HubID || metadata.HubDirectoryVersion < currentMetadata.HubDirectoryVersion) {
			return ErrInvalid
		}
	} else if !errors.Is(err, ErrNotFound) && !errors.Is(err, ErrExpired) {
		return err
	}
	validated, err := newSession(metadata, session.accessToken, session.refreshToken, session.refreshExpiresAt, now)
	if err != nil {
		return err
	}
	wire := wireSession{
		Version:      sessionSchemaVersion,
		Kind:         metadata.Kind,
		AccountID:    metadata.AccountID,
		AccountLabel: metadata.AccountLabel,
		DeviceID:     metadata.DeviceID,
		ExpiresAt:    metadata.ExpiresAt.Unix(),
		AccessToken:  append([]byte(nil), validated.accessToken...),
		HubID:        metadata.HubID, HubURL: metadata.HubURL, HubRegion: metadata.HubRegion, HubDirectoryVersion: metadata.HubDirectoryVersion,
		RefreshToken: append([]byte(nil), validated.refreshToken...),
	}
	if !validated.refreshExpiresAt.IsZero() {
		wire.RefreshExpiresAt = validated.refreshExpiresAt.Unix()
	}
	encoded, err := json.Marshal(wire)
	clear(wire.AccessToken)
	clear(wire.RefreshToken)
	if err != nil {
		return fmt.Errorf("encode cloud session: %w", err)
	}
	defer clear(encoded)
	if err := manager.store.StoreSecret(ctx, manager.key(metadata.Kind), encoded); err != nil {
		return fmt.Errorf("store cloud session in OS credential store: %w", err)
	}
	return nil
}

// Load 从 OS credential store 读取并验证当前 profile 下指定 kind 的会话。
// 未知字段、尾随 JSON、schema mismatch、空 token 和过期会话全部 fail closed。
func (manager *Manager) Load(ctx context.Context, kind Kind, now time.Time) (Session, error) {
	if kind != KindAccount && kind != KindDevice {
		return Session{}, ErrInvalid
	}
	encoded, err := manager.store.LoadSecret(ctx, manager.key(kind))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Session{}, ErrNotFound
		}
		return Session{}, fmt.Errorf("load cloud session from OS credential store: %w", err)
	}
	defer clear(encoded)
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var wire wireSession
	if err := decoder.Decode(&wire); err != nil {
		return Session{}, ErrInvalid
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Session{}, ErrInvalid
	}
	defer clear(wire.AccessToken)
	defer clear(wire.RefreshToken)
	if wire.Version != sessionSchemaVersion {
		return Session{}, ErrInvalid
	}
	if wire.Kind != kind {
		return Session{}, ErrInvalid
	}
	expiresAt := time.Unix(wire.ExpiresAt, 0).UTC()
	if !now.Before(expiresAt) {
		return Session{}, ErrExpired
	}
	metadata := Metadata{
		Kind:         wire.Kind,
		AccountID:    wire.AccountID,
		AccountLabel: wire.AccountLabel,
		DeviceID:     wire.DeviceID,
		ExpiresAt:    expiresAt,
		HubID:        wire.HubID, HubURL: wire.HubURL, HubRegion: wire.HubRegion, HubDirectoryVersion: wire.HubDirectoryVersion,
	}
	if len(wire.RefreshToken) > 0 {
		return NewRefreshable(metadata, wire.AccessToken, wire.RefreshToken, time.Unix(wire.RefreshExpiresAt, 0).UTC(), now)
	}
	return New(metadata, wire.AccessToken, now)
}

// LoadRefreshAuthorization 从 OS credential store 读取 refresh secret，即使短期 edge token 已过期。
// 该路径只验证 refresh 自身有效期；返回值只能交给 Control PlaneAdapter.RefreshSession。
func (manager *Manager) LoadRefreshAuthorization(ctx context.Context, kind Kind, now time.Time) (RefreshAuthorization, error) {
	if kind != KindAccount && kind != KindDevice {
		return RefreshAuthorization{}, ErrInvalid
	}
	encoded, err := manager.store.LoadSecret(ctx, manager.key(kind))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return RefreshAuthorization{}, ErrNotFound
		}
		return RefreshAuthorization{}, err
	}
	defer clear(encoded)
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var wire wireSession
	if decoder.Decode(&wire) != nil || decoder.Decode(&struct{}{}) != io.EOF || wire.Version != sessionSchemaVersion || wire.Kind != kind || len(wire.RefreshToken) < 32 {
		clear(wire.AccessToken)
		clear(wire.RefreshToken)
		return RefreshAuthorization{}, ErrInvalid
	}
	defer clear(wire.AccessToken)
	defer clear(wire.RefreshToken)
	refreshExpiresAt := time.Unix(wire.RefreshExpiresAt, 0).UTC()
	if !now.Before(refreshExpiresAt) {
		return RefreshAuthorization{}, ErrExpired
	}
	metadata := Metadata{Kind: wire.Kind, AccountID: wire.AccountID, AccountLabel: wire.AccountLabel, DeviceID: wire.DeviceID, ExpiresAt: time.Unix(wire.ExpiresAt, 0).UTC(), HubID: wire.HubID, HubURL: wire.HubURL, HubRegion: wire.HubRegion, HubDirectoryVersion: wire.HubDirectoryVersion}
	return RefreshAuthorization{raw: append([]byte(nil), wire.RefreshToken...), metadata: metadata}, nil
}

// Delete 删除当前 profile 下指定 kind 的账号或 device 云会话。
// 它不删除公开 daemon 的 DeviceIdentity、CapabilityGrant store 或 endpoint 配置。
func (manager *Manager) Delete(ctx context.Context, kind Kind) error {
	if kind != KindAccount && kind != KindDevice {
		return ErrInvalid
	}
	if err := manager.store.DeleteSecret(ctx, manager.key(kind)); err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("delete cloud session from OS credential store: %w", err)
	}
	return nil
}

func (manager *Manager) key(kind Kind) string {
	return manager.profile + "/" + string(kind) + "/v2"
}
