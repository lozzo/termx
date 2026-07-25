// Package releasecatalog 拥有 CLI/daemon 与 Android 的不可变制品目录和 channel head。
package releasecatalog

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/muxvia/muxvia/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

var (
	// ErrNotFound 表示 release 或 channel 尚不存在。
	ErrNotFound = errors.New("release catalog value not found")
	// ErrConflict 表示 immutable release、version monotonicity 或 channel revision 冲突。
	ErrConflict = errors.New("release catalog conflict")
	// ErrInvalid 表示 metadata、hash、signature 或解析请求无效。
	ErrInvalid = errors.New("invalid release catalog value")
)

var semverPattern = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z.-]+)?$`)

// Store 是不可变 artifact、可变 channel head 与审计的持久事务边界。
type Store interface {
	PublishRelease(context.Context, *cloudpb.ReleaseArtifactProjection, *cloudpb.OperatorMutationAuditProjection) error
	ReleaseArtifact(context.Context, string) (*cloudpb.ReleaseArtifactProjection, error)
	Releases(context.Context, cloudpb.ReleaseProduct, cloudpb.ReleaseChannel, string, string, int) ([]*cloudpb.ReleaseArtifactProjection, error)
	ReleaseChannel(context.Context, cloudpb.ReleaseProduct, cloudpb.ReleaseChannel, string, string) (*cloudpb.ReleaseChannelProjection, error)
	ReleaseChannels(context.Context, int) ([]*cloudpb.ReleaseChannelProjection, error)
	ReleaseAudits(context.Context, int) ([]*cloudpb.OperatorMutationAuditProjection, error)
	SetReleaseChannel(context.Context, *cloudpb.ReleaseChannelProjection, uint64, *cloudpb.OperatorMutationAuditProjection) error
}

// Service 校验签名、版本单调性、channel CAS 和客户端 rollout 决策。
type Service struct {
	store   Store
	keys    map[string]ed25519.PublicKey
	origins map[string]struct{}
	now     func() time.Time
}

// New 创建发布目录；可信 key 缺失时发布会 fail closed。
func New(store Store, keys map[string]ed25519.PublicKey, origins []string, now func() time.Time) (*Service, error) {
	if store == nil {
		return nil, ErrInvalid
	}
	if now == nil {
		now = time.Now
	}
	cloned := make(map[string]ed25519.PublicKey, len(keys))
	for id, key := range keys {
		if strings.TrimSpace(id) == "" || len(key) != ed25519.PublicKeySize {
			return nil, ErrInvalid
		}
		cloned[id] = append(ed25519.PublicKey(nil), key...)
	}
	trustedOrigins := make(map[string]struct{}, len(origins))
	for _, raw := range origins {
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Path != "" && parsed.Path != "/" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return nil, ErrInvalid
		}
		trustedOrigins[parsed.Scheme+"://"+parsed.Host] = struct{}{}
	}
	return &Service{store: store, keys: cloned, origins: trustedOrigins, now: now}, nil
}

// SigningPayload 返回 Ed25519 签名的确定性 metadata；signature 与服务端发布时间不参与自身签名。
func SigningPayload(artifact *cloudpb.ReleaseArtifactProjection) ([]byte, error) {
	if artifact == nil {
		return nil, ErrInvalid
	}
	value := proto.Clone(artifact).(*cloudpb.ReleaseArtifactProjection)
	value.Signature = nil
	value.PublishedAtUnixMillis = 0
	return proto.MarshalOptions{Deterministic: true}.Marshal(value)
}

// Publish 验证 HTTPS origin、SHA-256 与可信 Ed25519 签名后原子写入不可变 artifact 和审计。
func (service *Service) Publish(ctx context.Context, artifact *cloudpb.ReleaseArtifactProjection, actorID, reason, requestID string) (*cloudpb.ReleaseArtifactProjection, error) {
	if err := validateArtifact(artifact); err != nil || actorID == "" || strings.TrimSpace(reason) == "" || strings.TrimSpace(requestID) == "" {
		return nil, ErrInvalid
	}
	parsedURL, _ := url.Parse(artifact.GetDownloadUrl())
	if _, trusted := service.origins[parsedURL.Scheme+"://"+parsedURL.Host]; !trusted {
		return nil, ErrInvalid
	}
	key := service.keys[artifact.GetSigningKeyId()]
	payload, err := SigningPayload(artifact)
	if err != nil || len(key) != ed25519.PublicKeySize || !ed25519.Verify(key, payload, artifact.GetSignature()) {
		return nil, ErrInvalid
	}
	next := proto.Clone(artifact).(*cloudpb.ReleaseArtifactProjection)
	next.PublishedAtUnixMillis = service.now().UTC().UnixMilli()
	audit := mutationAudit(actorID, "release.publish", next.GetReleaseId(), "release_artifact", reason, requestID, 0, next.GetVersionCode(), service.now())
	if err := service.store.PublishRelease(ctx, next, audit); err != nil {
		return nil, err
	}
	return proto.Clone(next).(*cloudpb.ReleaseArtifactProjection), nil
}

// SetChannel 使用 revision CAS 激活、暂停或显式回滚 target channel。
func (service *Service) SetChannel(ctx context.Context, request *cloudpb.SetReleaseChannelRequest, actorID string) (*cloudpb.ReleaseChannelProjection, error) {
	if request == nil || request.GetReleaseId() == "" || request.GetRequestId() == "" || strings.TrimSpace(request.GetReason()) == "" || actorID == "" {
		return nil, ErrInvalid
	}
	target, err := service.store.ReleaseArtifact(ctx, request.GetReleaseId())
	if err != nil {
		return nil, err
	}
	current, currentErr := service.store.ReleaseChannel(ctx, target.GetProduct(), target.GetChannel(), target.GetOs(), target.GetArch())
	if errors.Is(currentErr, ErrNotFound) {
		if request.GetExpectedRevision() != 0 {
			return nil, ErrConflict
		}
		current = &cloudpb.ReleaseChannelProjection{}
	} else if currentErr != nil || current.GetRevision() != request.GetExpectedRevision() {
		return nil, ErrConflict
	}
	if current.GetActiveReleaseId() != "" && current.GetActiveReleaseId() != target.GetReleaseId() {
		active, loadErr := service.store.ReleaseArtifact(ctx, current.GetActiveReleaseId())
		if loadErr != nil {
			return nil, loadErr
		}
		if target.GetVersionCode() <= active.GetVersionCode() && !request.GetAllowRollback() {
			return nil, ErrConflict
		}
		if request.GetAllowRollback() && target.GetVersionCode() >= active.GetVersionCode() {
			return nil, ErrConflict
		}
	} else if request.GetAllowRollback() {
		return nil, ErrConflict
	}
	next := &cloudpb.ReleaseChannelProjection{Product: target.GetProduct(), Channel: target.GetChannel(), Os: target.GetOs(), Arch: target.GetArch(), ActiveReleaseId: target.GetReleaseId(), Revision: request.GetExpectedRevision() + 1, Paused: request.GetPaused(), UpdatedAtUnixMillis: service.now().UTC().UnixMilli()}
	action := "release.activate"
	if request.GetPaused() {
		action = "release.pause"
	} else if current.GetActiveReleaseId() == target.GetReleaseId() && current.GetPaused() {
		action = "release.resume"
	} else if request.GetAllowRollback() {
		action = "release.rollback"
	}
	audit := mutationAudit(actorID, action, target.GetReleaseId(), "release_channel", request.GetReason(), request.GetRequestId(), request.GetExpectedRevision(), next.GetRevision(), service.now())
	if err := service.store.SetReleaseChannel(ctx, next, request.GetExpectedRevision(), audit); err != nil {
		return nil, err
	}
	return proto.Clone(next).(*cloudpb.ReleaseChannelProjection), nil
}

// List 返回有界 artifact 历史与全部 channel heads，供 Operator 页面消费。
func (service *Service) List(ctx context.Context, request *cloudpb.ListReleaseArtifactsRequest) (*cloudpb.ListReleaseArtifactsResponse, error) {
	if request == nil {
		return nil, ErrInvalid
	}
	limit := 100
	if request.GetPage().GetPageSize() > 0 && request.GetPage().GetPageSize() <= 200 {
		limit = int(request.GetPage().GetPageSize())
	}
	artifacts, err := service.store.Releases(ctx, request.GetProduct(), request.GetChannel(), request.GetOs(), request.GetArch(), limit)
	if err != nil {
		return nil, err
	}
	channels, err := service.store.ReleaseChannels(ctx, 200)
	if err != nil {
		return nil, err
	}
	audits, err := service.store.ReleaseAudits(ctx, 200)
	return &cloudpb.ListReleaseArtifactsResponse{Artifacts: artifacts, Channels: channels, Page: &cloudpb.PageResponse{}, OperatorAudit: audits}, err
}

// Resolve 以稳定客户端 ID 计算 rollout bucket，并应用暂停、兼容与强制截止策略。
func (service *Service) Resolve(ctx context.Context, request *cloudpb.ResolveClientReleaseRequest) (*cloudpb.ResolveClientReleaseResponse, error) {
	if request == nil || request.GetProduct() == cloudpb.ReleaseProduct_RELEASE_PRODUCT_UNSPECIFIED || request.GetChannel() == cloudpb.ReleaseChannel_RELEASE_CHANNEL_UNSPECIFIED || request.GetOs() == "" || request.GetArch() == "" || request.GetCurrentVersionCode() == 0 || request.GetStableClientId() == "" {
		return nil, ErrInvalid
	}
	channel, err := service.store.ReleaseChannel(ctx, request.GetProduct(), request.GetChannel(), request.GetOs(), request.GetArch())
	if err != nil {
		return nil, err
	}
	artifact, err := service.store.ReleaseArtifact(ctx, channel.GetActiveReleaseId())
	if err != nil {
		return nil, err
	}
	response := &cloudpb.ResolveClientReleaseResponse{Artifact: artifact, Channel: channel, Decision: "current"}
	if channel.GetPaused() {
		response.Decision = "paused"
		return response, nil
	}
	if request.GetCurrentVersionCode() >= artifact.GetVersionCode() {
		return response, nil
	}
	digest := sha256.Sum256([]byte(request.GetStableClientId() + "\x00" + artifact.GetReleaseId()))
	response.RolloutBucket = uint32(digest[0])<<8 | uint32(digest[1])
	response.RolloutBucket %= 10000
	response.Forced = request.GetCurrentVersionCode() < artifact.GetMinCompatibleVersionCode() || artifact.GetForceAfterUnixMillis() > 0 && service.now().UTC().UnixMilli() >= artifact.GetForceAfterUnixMillis()
	response.UpdateAvailable = response.Forced || response.RolloutBucket < artifact.GetRolloutBasisPoints()
	if response.Forced {
		response.Decision = "forced"
	} else if response.UpdateAvailable {
		response.Decision = "rollout"
	} else {
		response.Decision = "outside_rollout"
	}
	return response, nil
}

func validateArtifact(value *cloudpb.ReleaseArtifactProjection) error {
	if value == nil || value.GetReleaseId() == "" || value.GetProduct() == cloudpb.ReleaseProduct_RELEASE_PRODUCT_UNSPECIFIED || value.GetChannel() == cloudpb.ReleaseChannel_RELEASE_CHANNEL_UNSPECIFIED || !semverPattern.MatchString(value.GetVersion()) || value.GetVersionCode() == 0 || value.GetOs() == "" || value.GetArch() == "" || value.GetArtifactSize() == 0 || len(value.GetSha256()) != sha256.Size || value.GetSigningKeyId() == "" || len(value.GetSignature()) != ed25519.SignatureSize || value.GetRolloutBasisPoints() > 10000 || value.GetMinCompatibleVersionCode() > value.GetVersionCode() || value.GetPublishedAtUnixMillis() != 0 {
		return ErrInvalid
	}
	parsed, err := url.Parse(value.GetDownloadUrl())
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return ErrInvalid
	}
	return nil
}

func mutationAudit(actorID, action, resourceID, kind, reason, requestID string, before, after uint64, now time.Time) *cloudpb.OperatorMutationAuditProjection {
	return &cloudpb.OperatorMutationAuditProjection{AuditId: "audit_release_" + requestID, ActorId: actorID, Action: action, ResourceKind: kind, ResourceId: resourceID, Reason: strings.TrimSpace(reason), RequestId: requestID, BeforeRevision: before, AfterRevision: after, OccurredAtUnixMillis: now.UTC().UnixMilli()}
}
