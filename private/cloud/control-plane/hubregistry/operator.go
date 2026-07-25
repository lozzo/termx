package hubregistry

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/muxvia/muxvia/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

// CreateDeployment 创建一个尚未批准的持久 Hub directory。
// 公钥来自 Edge 部署输出，Controller 只持有验证公钥；批准前不允许 control attachment 或 assignment。
func (registry *Registry) CreateDeployment(ctx context.Context, request *cloudpb.CreateHubDeploymentRequest, actorID string, now time.Time) (Deployment, error) {
	if request == nil || actorID == "" || now.IsZero() || request.GetHubId() == "" || request.GetEdgeDeploymentId() == "" || request.GetRelayId() == "" || request.GetRegion() == "" || strings.TrimSpace(request.GetPublicLabel()) == "" || request.GetMaxAssignments() == 0 || strings.TrimSpace(request.GetReason()) == "" || request.GetRequestId() == "" || !validDirectoryURL(request.GetPublicHubUrl(), false) || !validDirectoryURL(request.GetHealthUrl(), true) || len(request.GetHubControlPublicKey()) != ed25519.PublicKeySize || len(request.GetRelayControlPublicKey()) != ed25519.PublicKeySize {
		return Deployment{}, ErrDeploymentConflict
	}
	hubKey := append(ed25519.PublicKey(nil), request.GetHubControlPublicKey()...)
	relayKey := append(ed25519.PublicKey(nil), request.GetRelayControlPublicKey()...)
	value := Deployment{
		Metadata: &cloudpb.EdgeDeploymentMetadata{
			EdgeDeploymentId: request.GetEdgeDeploymentId(), Region: request.GetRegion(), PublicLabel: strings.TrimSpace(request.GetPublicLabel()), HubId: request.GetHubId(),
			HubControlIdentityFingerprint: IdentityFingerprint(hubKey), RelayId: request.GetRelayId(), RelayControlIdentityFingerprint: IdentityFingerprint(relayKey),
		},
		ControlPublicKey: hubKey, RelayControlPublicKey: relayKey, PublicHubURL: request.GetPublicHubUrl(), HealthURL: request.GetHealthUrl(), MaxAssignments: request.GetMaxAssignments(), DirectoryRevision: 1, UpdatedAt: now.UTC(),
	}
	if err := registry.store.CreateDeployment(ctx, value, deploymentAudit(actorID, "hub.create", value.Metadata.GetHubId(), request.GetReason(), request.GetRequestId(), 0, 1, now)); err != nil {
		return Deployment{}, err
	}
	return registry.store.Deployment(ctx, value.Metadata.GetHubId())
}

// UpdateDeployment 更新 URL、region、label 与容量，并用 directory revision 执行 CAS。
// identity、control generation 和现有 assignment 不受目录属性更新影响。
func (registry *Registry) UpdateDeployment(ctx context.Context, request *cloudpb.UpdateHubDeploymentRequest, actorID string, now time.Time) (Deployment, error) {
	if request == nil || actorID == "" || now.IsZero() || request.GetHubId() == "" || request.GetExpectedRevision() == 0 || request.GetRegion() == "" || strings.TrimSpace(request.GetPublicLabel()) == "" || request.GetMaxAssignments() == 0 || strings.TrimSpace(request.GetReason()) == "" || request.GetRequestId() == "" || !validDirectoryURL(request.GetPublicHubUrl(), false) || !validDirectoryURL(request.GetHealthUrl(), true) {
		return Deployment{}, ErrDeploymentConflict
	}
	value, err := registry.store.Deployment(ctx, request.GetHubId())
	if err != nil {
		return Deployment{}, err
	}
	if value.Archived || value.DirectoryRevision != request.GetExpectedRevision() {
		return Deployment{}, ErrDeploymentConflict
	}
	value.Metadata.Region = request.GetRegion()
	value.Metadata.PublicLabel = strings.TrimSpace(request.GetPublicLabel())
	value.PublicHubURL, value.HealthURL, value.MaxAssignments = request.GetPublicHubUrl(), request.GetHealthUrl(), request.GetMaxAssignments()
	value.DirectoryRevision++
	value.UpdatedAt = now.UTC()
	if err := registry.store.UpdateDeployment(ctx, value, request.GetExpectedRevision(), deploymentAudit(actorID, "hub.update", request.GetHubId(), request.GetReason(), request.GetRequestId(), request.GetExpectedRevision(), value.DirectoryRevision, now)); err != nil {
		return Deployment{}, err
	}
	return registry.store.Deployment(ctx, request.GetHubId())
}

// ApproveDeploymentIdentity 比对两个持久 fingerprint，批准后原子启用 Hub。
// Operator 不能在批准动作中替换公钥，避免审阅对象与最终启用 identity 不一致。
func (registry *Registry) ApproveDeploymentIdentity(ctx context.Context, request *cloudpb.ApproveHubDeploymentIdentityRequest, actorID string, now time.Time) (Deployment, error) {
	if request == nil || actorID == "" || now.IsZero() || request.GetHubId() == "" || request.GetExpectedRevision() == 0 || request.GetHubControlIdentityFingerprint() == "" || request.GetRelayControlIdentityFingerprint() == "" || strings.TrimSpace(request.GetReason()) == "" || request.GetRequestId() == "" {
		return Deployment{}, ErrDeploymentConflict
	}
	value, err := registry.store.Deployment(ctx, request.GetHubId())
	if err != nil {
		return Deployment{}, err
	}
	if value.Archived || value.IdentityApproved || value.DirectoryRevision != request.GetExpectedRevision() || value.Metadata.GetHubControlIdentityFingerprint() != request.GetHubControlIdentityFingerprint() || value.Metadata.GetRelayControlIdentityFingerprint() != request.GetRelayControlIdentityFingerprint() {
		return Deployment{}, ErrDeploymentLifecycle
	}
	value.IdentityApproved, value.Enabled = true, true
	value.DirectoryRevision++
	value.UpdatedAt = now.UTC()
	if err := registry.store.UpdateDeployment(ctx, value, request.GetExpectedRevision(), deploymentAudit(actorID, "hub.identity.approve", request.GetHubId(), request.GetReason(), request.GetRequestId(), request.GetExpectedRevision(), value.DirectoryRevision, now)); err != nil {
		return Deployment{}, err
	}
	return registry.store.Deployment(ctx, request.GetHubId())
}

// SetDeploymentDraining 原子改变新 assignment 准入；它不自动迁移已有 daemon。
func (registry *Registry) SetDeploymentDraining(ctx context.Context, request *cloudpb.SetHubDeploymentDrainRequest, actorID string, now time.Time) (Deployment, error) {
	if request == nil || actorID == "" || now.IsZero() || request.GetHubId() == "" || request.GetExpectedRevision() == 0 || strings.TrimSpace(request.GetReason()) == "" || request.GetRequestId() == "" {
		return Deployment{}, ErrDeploymentConflict
	}
	value, err := registry.store.Deployment(ctx, request.GetHubId())
	if err != nil {
		return Deployment{}, err
	}
	if value.Archived || !value.IdentityApproved || !value.Enabled || value.DirectoryRevision != request.GetExpectedRevision() || value.Draining == request.GetDraining() {
		return Deployment{}, ErrDeploymentLifecycle
	}
	value.Draining = request.GetDraining()
	value.DirectoryRevision++
	value.UpdatedAt = now.UTC()
	action := "hub.drain.start"
	if !request.GetDraining() {
		action = "hub.drain.cancel"
	}
	if err := registry.store.UpdateDeployment(ctx, value, request.GetExpectedRevision(), deploymentAudit(actorID, action, request.GetHubId(), request.GetReason(), request.GetRequestId(), request.GetExpectedRevision(), value.DirectoryRevision, now)); err != nil {
		return Deployment{}, err
	}
	return registry.store.Deployment(ctx, request.GetHubId())
}

// DisableDeployment 只在 draining Hub 已无有效 assignment 时禁用并 archive。
// archive 保留身份、generation 和审计记录，不提供 hard delete。
func (registry *Registry) DisableDeployment(ctx context.Context, request *cloudpb.DisableHubDeploymentRequest, actorID string, now time.Time) (Deployment, error) {
	if request == nil || actorID == "" || now.IsZero() || request.GetHubId() == "" || request.GetExpectedRevision() == 0 || strings.TrimSpace(request.GetReason()) == "" || request.GetRequestId() == "" {
		return Deployment{}, ErrDeploymentConflict
	}
	value, err := registry.store.Deployment(ctx, request.GetHubId())
	if err != nil {
		return Deployment{}, err
	}
	if value.Archived || !value.Enabled || !value.Draining || value.DirectoryRevision != request.GetExpectedRevision() {
		return Deployment{}, ErrDeploymentLifecycle
	}
	value.Enabled, value.Archived = false, true
	value.DirectoryRevision++
	value.UpdatedAt = now.UTC()
	if err := registry.store.ArchiveDeployment(ctx, value, request.GetExpectedRevision(), now.UTC(), deploymentAudit(actorID, "hub.disable", request.GetHubId(), request.GetReason(), request.GetRequestId(), request.GetExpectedRevision(), value.DirectoryRevision, now)); err != nil {
		return Deployment{}, err
	}
	return registry.store.Deployment(ctx, request.GetHubId())
}

// Projection 把 registry domain 值映射为 generated Proto operator projection。
func Projection(value Deployment) *cloudpb.HubDeploymentProjection {
	var metadata *cloudpb.EdgeDeploymentMetadata
	if value.Metadata != nil {
		metadata = proto.Clone(value.Metadata).(*cloudpb.EdgeDeploymentMetadata)
	}
	return &cloudpb.HubDeploymentProjection{Metadata: metadata, PublicHubUrl: value.PublicHubURL, HealthUrl: value.HealthURL, MaxAssignments: value.MaxAssignments, IdentityApproved: value.IdentityApproved, Enabled: value.Enabled, Draining: value.Draining, Archived: value.Archived, DirectoryRevision: value.DirectoryRevision, UpdatedAtUnixMillis: value.UpdatedAt.UnixMilli()}
}

func deploymentAudit(actorID, action, hubID, reason, requestID string, before, after uint64, now time.Time) *cloudpb.OperatorMutationAuditProjection {
	identifier := make([]byte, 16)
	_, _ = rand.Read(identifier)
	return &cloudpb.OperatorMutationAuditProjection{AuditId: "audit_" + hex.EncodeToString(identifier), ActorId: actorID, Action: action, ResourceKind: "hub_deployment", ResourceId: hubID, Reason: strings.TrimSpace(reason), RequestId: requestID, BeforeRevision: before, AfterRevision: after, OccurredAtUnixMillis: now.UTC().UnixMilli()}
}

func validDirectoryURL(raw string, allowPath bool) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || !allowPath && strings.Trim(parsed.Path, "/") != "" {
		return false
	}
	if parsed.Scheme == "https" {
		return true
	}
	address := net.ParseIP(parsed.Hostname())
	return parsed.Scheme == "http" && address != nil && address.IsLoopback()
}
