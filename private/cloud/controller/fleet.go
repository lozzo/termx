package controller

import (
	"context"
	"time"

	"github.com/lozzow/termx/private/cloud/control-plane/hubcontrol"
	"github.com/lozzow/termx/private/cloud/control-plane/hubregistry"
	"github.com/lozzow/termx/private/cloud/control-plane/relaycontrol"
	"github.com/lozzow/termx/proto/cloudpb"
)

type fleetQuery struct {
	registry     *hubregistry.Registry
	publisher    *hubcontrol.Publisher
	hubControl   *hubcontrol.Server
	relayControl *relaycontrol.Server
}

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
		projection := query.project(deployment)
		if request.GetFreshness() == cloudpb.Freshness_FRESHNESS_UNSPECIFIED || projection.GetFreshness() == request.GetFreshness() {
			response.Hubs = append(response.Hubs, projection)
		}
		if len(response.GetHubs()) == limit {
			break
		}
	}
	return response, nil
}

func (query *fleetQuery) GetHubStatus(ctx context.Context, hubID string) (*cloudpb.GetHubStatusResponse, error) {
	deployment, err := query.registry.Deployment(ctx, hubID)
	if err != nil {
		return nil, err
	}
	return &cloudpb.GetHubStatusResponse{Hub: query.project(deployment)}, nil
}

func (query *fleetQuery) project(deployment hubregistry.Deployment) *cloudpb.HubFleetProjection {
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
	return &cloudpb.HubFleetProjection{Deployment: deployment.Metadata, HubControlGeneration: hubGeneration, RelayControlGeneration: relayGeneration, ProjectionRevision: head.Revision, Freshness: freshness, HubReady: hubReady, RelayReady: relayReady, LastControlSeenAtUnixMillis: lastSeen.UnixMilli()}
}
