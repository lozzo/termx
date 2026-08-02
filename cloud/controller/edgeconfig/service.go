// Package edgeconfig 拥有 Edge desired config、一次性安装 claim 和配置签名领域。
// 在线状态不进入本 package；它只保存重启后仍成立的部署事实。
package edgeconfig

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/anytty/anytty/cloud/configsignature"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"github.com/google/uuid"
)

var (
	// ErrClaimInvalid 表示 claim 不存在、已消费、过期或不属于目标 Edge。
	ErrClaimInvalid = errors.New("Edge claim is invalid")
	// ErrRevisionConflict 表示运营编辑基于过期的 config revision。
	ErrRevisionConflict = errors.New("Edge config revision conflict")
	// ErrEdgeNotFound 表示目标 Edge deployment 已不存在。
	ErrEdgeNotFound = errors.New("Edge deployment does not exist")
	// ErrEdgeEnabled 表示删除前尚未停用目标 Edge。
	ErrEdgeEnabled = errors.New("Edge must be disabled before deletion")
)

// Edge 是持久 desired state；Runtime 在线字段由 Controller Directory 单独提供。
type Edge struct {
	ID             string
	Name           string
	Region         string
	Capacity       uint64
	PublicEndpoint string
	Enabled        bool
	ConfigVersion  uint64
	Revision       uint64
	SignedConfig   *cloudv1.SignedEdgeDesiredConfig
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// CreateInput 只包含运营人员允许填写的部署意图。
type CreateInput struct {
	Name           string
	Region         string
	Capacity       uint64
	PublicEndpoint string
}

// UpdateInput 使用 optimistic revision 修改可编辑 desired state。
type UpdateInput struct {
	EdgeID           string
	ExpectedRevision uint64
	Name             string
	Region           string
	Capacity         uint64
	PublicEndpoint   string
	Enabled          bool
}

// DeleteInput 固定删除的 CAS、审计操作者和原因。
type DeleteInput struct {
	EdgeID           string
	ExpectedRevision uint64
	ActorID          string
	Reason           string
	DeletedAt        time.Time
}

// Store 是 Edge 持久事务边界；实现必须把配置版本、claim 消费和对应变更原子提交。
type Store interface {
	ListEdges(context.Context) ([]Edge, error)
	GetEdge(context.Context, string) (Edge, error)
	CreateEdge(context.Context, Edge, []byte, time.Time) error
	UpdateEdge(context.Context, UpdateInput, Edge) error
	DeleteEdge(context.Context, DeleteInput) error
	ConsumeInstallClaim(context.Context, []byte, []byte, time.Time) (Edge, error)
	ConsumeBootstrapClaim(context.Context, []byte, string, []byte) (Edge, error)
}

type IdentityRecoveryStore interface {
	CreateIdentityRecoveryClaim(context.Context, string, []byte, time.Time, string, string, time.Time) error
	ConsumeIdentityRecoveryClaim(context.Context, []byte, string, []byte, time.Time) (Edge, error)
}

// Config 提供独立配置签名密钥和一次性 claim 生命周期。
type Config struct {
	Store        Store
	SigningKey   ed25519.PrivateKey
	SigningKeyID string
	ClaimTTL     time.Duration
	Now          func() time.Time
}

// Service 编排 Edge 持久配置与 claim，不读取或缓存实时 Directory。
type Service struct {
	store        Store
	signingKey   ed25519.PrivateKey
	signingKeyID string
	claimTTL     time.Duration
	now          func() time.Time
}

// NewService 校验 store、Ed25519 key 和 claim TTL。
func NewService(config Config) (*Service, error) {
	config.SigningKeyID = strings.TrimSpace(config.SigningKeyID)
	if config.Store == nil || len(config.SigningKey) != ed25519.PrivateKeySize || config.SigningKeyID == "" || config.ClaimTTL <= 0 {
		return nil, errors.New("Edge config store, Ed25519 signing key, key ID, and positive claim TTL are required")
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{store: config.Store, signingKey: config.SigningKey, signingKeyID: config.SigningKeyID, claimTTL: config.ClaimTTL, now: config.Now}, nil
}

// CreateEdge 持久化首个配置版本并返回只显示一次的 256-bit install claim。
func (service *Service) CreateEdge(ctx context.Context, input CreateInput) (Edge, string, time.Time, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Region = strings.TrimSpace(input.Region)
	input.PublicEndpoint = strings.TrimSpace(input.PublicEndpoint)
	if err := validateFields(input.Name, input.Region, input.Capacity, input.PublicEndpoint); err != nil {
		return Edge{}, "", time.Time{}, err
	}
	now := service.now()
	edge := Edge{ID: uuid.NewString(), Name: input.Name, Region: input.Region, Capacity: input.Capacity, PublicEndpoint: input.PublicEndpoint, Enabled: true, ConfigVersion: 1, Revision: 1, CreatedAt: now, UpdatedAt: now}
	signed, err := service.sign(edge)
	if err != nil {
		return Edge{}, "", time.Time{}, err
	}
	edge.SignedConfig = signed
	token, digest, err := newToken()
	if err != nil {
		return Edge{}, "", time.Time{}, err
	}
	expiresAt := now.Add(service.claimTTL)
	if err := service.store.CreateEdge(ctx, edge, digest, expiresAt); err != nil {
		return Edge{}, "", time.Time{}, err
	}
	return edge, token, expiresAt, nil
}

// UpdateEdge 创建不可变的新 config version；revision 冲突不会覆盖他人修改。
func (service *Service) UpdateEdge(ctx context.Context, input UpdateInput) (Edge, error) {
	input.EdgeID = strings.TrimSpace(input.EdgeID)
	input.Name = strings.TrimSpace(input.Name)
	input.Region = strings.TrimSpace(input.Region)
	input.PublicEndpoint = strings.TrimSpace(input.PublicEndpoint)
	if input.EdgeID == "" || input.ExpectedRevision == 0 {
		return Edge{}, errors.New("Edge ID and expected revision are required")
	}
	if err := validateFields(input.Name, input.Region, input.Capacity, input.PublicEndpoint); err != nil {
		return Edge{}, err
	}
	current, err := service.store.GetEdge(ctx, input.EdgeID)
	if err != nil {
		return Edge{}, err
	}
	updated := current
	updated.Name, updated.Region, updated.Capacity, updated.PublicEndpoint, updated.Enabled = input.Name, input.Region, input.Capacity, input.PublicEndpoint, input.Enabled
	updated.ConfigVersion++
	updated.Revision++
	updated.UpdatedAt = service.now()
	updated.SignedConfig, err = service.sign(updated)
	if err != nil {
		return Edge{}, err
	}
	if err := service.store.UpdateEdge(ctx, input, updated); err != nil {
		return Edge{}, err
	}
	return updated, nil
}

// ListEdges 返回持久配置，调用方再与 Directory 的只读在线投影组合。
func (service *Service) ListEdges(ctx context.Context) ([]Edge, error) {
	return service.store.ListEdges(ctx)
}

// GetEdge 返回一个 Edge 的当前持久配置和签名版本。
func (service *Service) GetEdge(ctx context.Context, edgeID string) (Edge, error) {
	return service.store.GetEdge(ctx, strings.TrimSpace(edgeID))
}

// DeleteEdge 删除已停用 deployment；实时在线检查由持有 Directory 的调用方完成。
func (service *Service) DeleteEdge(ctx context.Context, input DeleteInput) error {
	input.EdgeID = strings.TrimSpace(input.EdgeID)
	input.ActorID = strings.TrimSpace(input.ActorID)
	input.Reason = strings.TrimSpace(input.Reason)
	if _, err := uuid.Parse(input.EdgeID); err != nil || input.ExpectedRevision == 0 || input.ActorID == "" || input.Reason == "" {
		return errors.New("valid Edge deletion revision, actor, and reason are required")
	}
	edge, err := service.store.GetEdge(ctx, input.EdgeID)
	if err != nil {
		return err
	}
	if edge.Enabled {
		return ErrEdgeEnabled
	}
	input.DeletedAt = service.now()
	return service.store.DeleteEdge(ctx, input)
}

// ConsumeInstallClaim 原子消费 URL claim 并生成脚本内使用的第二个一次性 bootstrap credential。
func (service *Service) ConsumeInstallClaim(ctx context.Context, token string) (Edge, string, time.Time, error) {
	bootstrapToken, bootstrapDigest, err := newToken()
	if err != nil {
		return Edge{}, "", time.Time{}, err
	}
	expiresAt := service.now().Add(service.claimTTL)
	edge, err := service.store.ConsumeInstallClaim(ctx, tokenDigest(token), bootstrapDigest, expiresAt)
	if err != nil {
		return Edge{}, "", time.Time{}, err
	}
	return edge, bootstrapToken, expiresAt, nil
}

// ConsumeBootstrapClaim 把注册请求原子绑定到目标 Edge，重复注册必须重新生成 claim。
func (service *Service) ConsumeBootstrapClaim(ctx context.Context, token, edgeID string, csrDigest []byte) (Edge, error) {
	if len(csrDigest) != sha256.Size {
		return Edge{}, errors.New("identity CSR digest is required")
	}
	return service.store.ConsumeBootstrapClaim(ctx, tokenDigest(token), strings.TrimSpace(edgeID), csrDigest)
}

// CreateIdentityRecoveryClaim creates a separately audited, short-lived claim
// for an Edge that can no longer authenticate with its expired certificate.
func (service *Service) CreateIdentityRecoveryClaim(ctx context.Context, edgeID, actorID, reason string) (string, time.Time, error) {
	edgeID = strings.TrimSpace(edgeID)
	actorID = strings.TrimSpace(actorID)
	reason = strings.TrimSpace(reason)
	if edgeID == "" || actorID == "" || reason == "" {
		return "", time.Time{}, errors.New("Edge identity recovery requires Edge, actor, and reason")
	}
	token, digest, err := newToken()
	if err != nil {
		return "", time.Time{}, err
	}
	now := service.now()
	expiresAt := now.Add(service.claimTTL)
	recoveryStore, ok := service.store.(IdentityRecoveryStore)
	if !ok {
		return "", time.Time{}, errors.New("Edge identity recovery store is unavailable")
	}
	if err := recoveryStore.CreateIdentityRecoveryClaim(ctx, edgeID, digest, expiresAt, actorID, reason, now); err != nil {
		return "", time.Time{}, err
	}
	return token, expiresAt, nil
}

func (service *Service) ConsumeIdentityRecoveryClaim(ctx context.Context, token, edgeID string, csrDigest []byte) (Edge, error) {
	if len(csrDigest) != sha256.Size {
		return Edge{}, errors.New("identity recovery CSR digest is required")
	}
	recoveryStore, ok := service.store.(IdentityRecoveryStore)
	if !ok {
		return Edge{}, errors.New("Edge identity recovery store is unavailable")
	}
	return recoveryStore.ConsumeIdentityRecoveryClaim(ctx, tokenDigest(token), strings.TrimSpace(edgeID), csrDigest, service.now())
}

// SigningPublicKey 返回 Edge 安装后用于 desired config 验签的公开 key。
func (service *Service) SigningPublicKey() (string, ed25519.PublicKey) {
	publicKey := service.signingKey.Public().(ed25519.PublicKey)
	return service.signingKeyID, append(ed25519.PublicKey(nil), publicKey...)
}

// VerifySignedConfig 校验独立签名域和确定性 Proto payload，供 Edge runtime 使用。
func VerifySignedConfig(signed *cloudv1.SignedEdgeDesiredConfig, expectedKeyID string, publicKey ed25519.PublicKey) (*cloudv1.EdgeDesiredConfig, error) {
	return configsignature.Verify(signed, expectedKeyID, publicKey)
}

func (service *Service) sign(edge Edge) (*cloudv1.SignedEdgeDesiredConfig, error) {
	config := &cloudv1.EdgeDesiredConfig{EdgeId: edge.ID, Version: edge.ConfigVersion, Name: edge.Name, Region: edge.Region, Capacity: edge.Capacity, PublicEndpoint: edge.PublicEndpoint, Enabled: edge.Enabled}
	return configsignature.Sign(config, service.signingKeyID, service.signingKey)
}

func validateFields(name, region string, capacity uint64, endpoint string) error {
	if name == "" || region == "" || capacity == 0 || endpoint == "" {
		return errors.New("Edge name, region, positive capacity, and public endpoint are required")
	}
	host := endpoint
	if parsedHost, _, err := net.SplitHostPort(endpoint); err == nil {
		host = parsedHost
	} else if strings.Contains(endpoint, ":") {
		return errors.New("public endpoint must be a domain, IP address, or host:port")
	}
	if net.ParseIP(host) != nil {
		return nil
	}
	if !strings.Contains(host, ".") || strings.ContainsAny(host, " /?#") {
		return errors.New("public endpoint must use a DNS domain or IP address")
	}
	return nil
}

func newToken() (string, []byte, error) {
	payload := make([]byte, 32)
	if _, err := rand.Read(payload); err != nil {
		return "", nil, fmt.Errorf("generate Edge claim: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(payload)
	return token, tokenDigest(token), nil
}

func tokenDigest(token string) []byte {
	digest := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return digest[:]
}
