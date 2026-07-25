package controller

import (
	"context"
	"time"

	"github.com/muxvia/muxvia/private/cloud/control-plane/hubcontrol"
	"github.com/muxvia/muxvia/private/cloud/control-plane/hubregistry"
	"github.com/muxvia/muxvia/private/cloud/control-plane/relaycontrol"
	"github.com/muxvia/muxvia/proto/cloudpb"
)

type fleetQuery struct {
	registry     *hubregistry.Registry
	publisher    *hubcontrol.Publisher
	hubControl   *hubcontrol.Server
	relayControl *relaycontrol.Server
	onEnabled    func(string, time.Time)
}

// ListHubFleet 组合 PostgreSQL directory 与进程内 attachment 观测，不持久化 online bool。
func (query *fleetQuery) ListHubFleet(ctx context.Context, request *cloudpb.ListHubFleetRequest) (*cloudpb.ListHubFleetResponse, error) {
	deployments, err := query.registry.Deployments(ctx)
	if err != nil {
		return nil, err
	}
	response := &cloudpb.ListHubFleetResponse{Page: &cloudpb.PageResponse{}}
	limit := 50
	if requested := request.GetPage().GetPageSize(); requested > 0 && requested <= 100 {
		limit = int(requested)
	}
	for _, deployment := range deployments {
		if request.GetRegion() != "" && deployment.Metadata.GetRegion() != request.GetRegion() {
			continue
		}
		assignments, assignmentErr := query.registry.AssignmentsForHub(ctx, deployment.Metadata.GetHubId(), time.Now().UTC())
		if assignmentErr != nil {
			return nil, assignmentErr
		}
		projection := query.project(deployment, uint64(len(assignments)))
		if request.GetFreshness() == cloudpb.Freshness_FRESHNESS_UNSPECIFIED || projection.GetFreshness() == request.GetFreshness() {
			response.Hubs = append(response.Hubs, projection)
		}
		if len(response.GetHubs()) == limit {
			break
		}
	}
	return response, nil
}

// GetHubStatus 返回单个 Hub directory、assignment 数和当前 attachment readiness。
func (query *fleetQuery) GetHubStatus(ctx context.Context, hubID string) (*cloudpb.GetHubStatusResponse, error) {
	deployment, err := query.registry.Deployment(ctx, hubID)
	if err != nil {
		return nil, err
	}
	assignments, err := query.registry.AssignmentsForHub(ctx, deployment.Metadata.GetHubId(), time.Now().UTC())
	if err != nil {
		return nil, err
	}
	return &cloudpb.GetHubStatusResponse{Hub: query.project(deployment, uint64(len(assignments)))}, nil
}

// CreateHubDeployment 委托 hubregistry 创建待批准目录，HTTP 层不直接写 store。
func (query *fleetQuery) CreateHubDeployment(ctx context.Context, request *cloudpb.CreateHubDeploymentRequest, actorID string, now time.Time) (*cloudpb.CreateHubDeploymentResponse, error) {
	value, err := query.registry.CreateDeployment(ctx, request, actorID, now)
	return &cloudpb.CreateHubDeploymentResponse{Deployment: hubregistry.Projection(value)}, err
}

// UpdateHubDeployment 委托 hubregistry 执行 directory revision CAS。
func (query *fleetQuery) UpdateHubDeployment(ctx context.Context, request *cloudpb.UpdateHubDeploymentRequest, actorID string, now time.Time) (*cloudpb.UpdateHubDeploymentResponse, error) {
	value, err := query.registry.UpdateDeployment(ctx, request, actorID, now)
	return &cloudpb.UpdateHubDeploymentResponse{Deployment: hubregistry.Projection(value)}, err
}

// ApproveHubDeploymentIdentity 批准持久公钥 fingerprint，并触发该 Hub 的首个 policy projection。
func (query *fleetQuery) ApproveHubDeploymentIdentity(ctx context.Context, request *cloudpb.ApproveHubDeploymentIdentityRequest, actorID string, now time.Time) (*cloudpb.ApproveHubDeploymentIdentityResponse, error) {
	value, err := query.registry.ApproveDeploymentIdentity(ctx, request, actorID, now)
	if err == nil && query.onEnabled != nil {
		query.onEnabled(value.Metadata.GetHubId(), now)
	}
	return &cloudpb.ApproveHubDeploymentIdentityResponse{Deployment: hubregistry.Projection(value)}, err
}

// SetHubDeploymentDrain 只改变新 assignment 准入，不伪造已有 assignment 的迁移结果。
func (query *fleetQuery) SetHubDeploymentDrain(ctx context.Context, request *cloudpb.SetHubDeploymentDrainRequest, actorID string, now time.Time) (*cloudpb.SetHubDeploymentDrainResponse, error) {
	value, err := query.registry.SetDeploymentDraining(ctx, request, actorID, now)
	return &cloudpb.SetHubDeploymentDrainResponse{Deployment: hubregistry.Projection(value)}, err
}

// DisableHubDeployment 在 registry 确认 assignment 清零后 archive deployment。
func (query *fleetQuery) DisableHubDeployment(ctx context.Context, request *cloudpb.DisableHubDeploymentRequest, actorID string, now time.Time) (*cloudpb.DisableHubDeploymentResponse, error) {
	value, err := query.registry.DisableDeployment(ctx, request, actorID, now)
	return &cloudpb.DisableHubDeploymentResponse{Deployment: hubregistry.Projection(value)}, err
}

func (query *fleetQuery) project(deployment hubregistry.Deployment, activeAssignments uint64) *cloudpb.HubFleetProjection {
	hubGeneration, hubSeen, hubReady := query.hubControl.AttachmentStatus(deployment.Metadata.GetHubId())
	relayGeneration, relaySeen, relayReady := query.relayControl.AttachmentStatus(deployment.Metadata.GetRelayId())
	freshness := cloudpb.Freshness_FRESHNESS_STALE
	if hubReady && relayReady {
		freshness = cloudpb.Freshness_FRESHNESS_FRESH
	}
	lastSeen := time.Time{}
	if !hubSeen.IsZero() {
		lastSeen = hubSeen
	}
	if relaySeen.After(lastSeen) {
		lastSeen = relaySeen
	}
	head, _ := query.publisher.Head(deployment.Metadata.GetHubId())
	return &cloudpb.HubFleetProjection{Deployment: hubregistry.Projection(deployment), HubControlGeneration: hubGeneration, RelayControlGeneration: relayGeneration, ProjectionRevision: head.Revision, Freshness: freshness, HubReady: hubReady, RelayReady: relayReady, LastControlSeenAtUnixMillis: lastSeen.UnixMilli(), ActiveAssignments: activeAssignments}
}
