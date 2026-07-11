package routeplanner_test

import (
	"context"
	"testing"
	"time"

	"github.com/lozzow/termx/private/termx-cloud/route-planner"
	"github.com/lozzow/termx/private/termx-cloud/route-planner/quality"
	"github.com/lozzow/termx/private/termx-cloud/route-planner/smartroute"
	"github.com/lozzow/termx/proto/cloudpb"
)

type requestSource struct {
	request smartroute.Request
}

func (source requestSource) SmartRouteRequest(context.Context, *cloudpb.PlanManagedRouteRequest, time.Time) (smartroute.Request, error) {
	return source.request, nil
}

type materialSource struct {
	selectedID string
}

func (source *materialSource) RouteMaterial(_ context.Context, _ *cloudpb.PlanManagedRouteRequest, candidate smartroute.Candidate, now time.Time) (routeplanner.RouteMaterial, error) {
	source.selectedID = candidate.ID
	if candidate.Path == cloudpb.ObservedPath_OBSERVED_PATH_SINGLE_RELAY {
		return routeplanner.RouteMaterial{
			IceServers: []*cloudpb.IceServer{{Urls: []string{"turns:relay.example.com"}, Username: "user", Credential: "credential"}},
			ValidUntil: now.Add(time.Minute),
		}, nil
	}
	return routeplanner.RouteMaterial{IceServers: []*cloudpb.IceServer{{Urls: []string{"stun:stun.example.com"}}}, ValidUntil: now.Add(time.Minute)}, nil
}

func TestServiceReturnsOnlySelectedRelayMaterialAndStableReason(t *testing.T) {
	now := time.Date(2026, time.July, 11, 14, 0, 0, 0, time.UTC)
	config := smartroute.DefaultConfig()
	config.MinimumHold = 0
	config.SwitchCooldown = 0
	config.RequiredConsecutiveWins = 1
	planner, err := smartroute.NewPlanner(config)
	if err != nil {
		t.Fatal(err)
	}
	material := &materialSource{}
	service, err := routeplanner.NewService(routeplanner.Config{
		Engine: planner,
		Requests: requestSource{request: smartroute.Request{
			ManagedSessionID: "managed-1", CostBudget: smartroute.CostBudget{Known: true, MaxMicrounits: 100},
			Candidates: []smartroute.Candidate{
				directServiceCandidate(now, 500, 500),
				relayServiceCandidate(now, 40, 10, 20),
			},
		}},
		Materials: material, Now: func() time.Time { return now }, PlanID: func() (string, error) { return "plan-1", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := service.PlanManagedRoute(context.Background(), &cloudpb.PlanManagedRouteRequest{
		EndpointId: "studio", ManagedSessionId: "managed-1", TargetDeviceId: "daemon-1",
		RoutePreference: cloudpb.RoutePreference_ROUTE_PREFERENCE_SMART_ROUTE,
	})
	if err != nil {
		t.Fatal(err)
	}
	if material.selectedID != "relay-eu" || plan.GetSelectedPath() != cloudpb.ObservedPath_OBSERVED_PATH_SINGLE_RELAY ||
		plan.GetSelectionReason() != cloudpb.RouteSelectionReason_ROUTE_SELECTION_REASON_INITIAL_BEST || !plan.GetRelayOnly() ||
		plan.GetRelayRegion() != "eu-west" || len(plan.GetIceServers()) != 1 {
		t.Fatalf("managed route plan = %+v selected=%q", plan, material.selectedID)
	}
}

func TestServiceRejectsPathMaterialConfusion(t *testing.T) {
	now := time.Date(2026, time.July, 11, 14, 0, 0, 0, time.UTC)
	planner, _ := smartroute.NewPlanner(smartroute.DefaultConfig())
	service, _ := routeplanner.NewService(routeplanner.Config{
		Engine: planner,
		Requests: requestSource{request: smartroute.Request{
			ManagedSessionID: "managed-1", Candidates: []smartroute.Candidate{directServiceCandidate(now, 20, 0)},
		}},
		Materials: routeMaterialFunc(func(context.Context, *cloudpb.PlanManagedRouteRequest, smartroute.Candidate, time.Time) (routeplanner.RouteMaterial, error) {
			return routeplanner.RouteMaterial{
				IceServers: []*cloudpb.IceServer{{Urls: []string{"turn:relay.example.com"}, Username: "user", Credential: "credential"}},
				ValidUntil: now.Add(time.Minute),
			}, nil
		}),
		Now: func() time.Time { return now }, PlanID: func() (string, error) { return "plan-1", nil },
	})
	_, err := service.PlanManagedRoute(context.Background(), &cloudpb.PlanManagedRouteRequest{
		EndpointId: "studio", ManagedSessionId: "managed-1", TargetDeviceId: "daemon-1",
		RoutePreference: cloudpb.RoutePreference_ROUTE_PREFERENCE_SMART_ROUTE,
	})
	if err == nil {
		t.Fatal("direct plan with TURN material must fail")
	}
}

func TestServiceRejectsUnknownReasonBeforeIssuingRouteMaterial(t *testing.T) {
	now := time.Date(2026, time.July, 11, 14, 0, 0, 0, time.UTC)
	materialCalls := 0
	service, err := routeplanner.NewService(routeplanner.Config{
		Engine: decisionEngineFunc(func(smartroute.Request, time.Time) (smartroute.Decision, error) {
			return smartroute.Decision{Selected: directServiceCandidate(now, 20, 0), Reason: smartroute.Reason("private_detail")}, nil
		}),
		Requests: requestSource{request: smartroute.Request{ManagedSessionID: "managed-1"}},
		Materials: routeMaterialFunc(func(context.Context, *cloudpb.PlanManagedRouteRequest, smartroute.Candidate, time.Time) (routeplanner.RouteMaterial, error) {
			materialCalls++
			return routeplanner.RouteMaterial{}, nil
		}),
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.PlanManagedRoute(context.Background(), &cloudpb.PlanManagedRouteRequest{
		EndpointId: "studio", ManagedSessionId: "managed-1", TargetDeviceId: "daemon-1",
		RoutePreference: cloudpb.RoutePreference_ROUTE_PREFERENCE_SMART_ROUTE,
	})
	if err == nil || materialCalls != 0 {
		t.Fatalf("unknown reason error=%v materialCalls=%d", err, materialCalls)
	}
}

type decisionEngineFunc func(smartroute.Request, time.Time) (smartroute.Decision, error)

func (function decisionEngineFunc) Select(request smartroute.Request, now time.Time) (smartroute.Decision, error) {
	return function(request, now)
}

type routeMaterialFunc func(context.Context, *cloudpb.PlanManagedRouteRequest, smartroute.Candidate, time.Time) (routeplanner.RouteMaterial, error)

func (function routeMaterialFunc) RouteMaterial(ctx context.Context, request *cloudpb.PlanManagedRouteRequest, candidate smartroute.Candidate, now time.Time) (routeplanner.RouteMaterial, error) {
	return function(ctx, request, candidate, now)
}

func directServiceCandidate(now time.Time, p95 uint64, loss uint32) smartroute.Candidate {
	return smartroute.Candidate{
		ID: "direct", Path: cloudpb.ObservedPath_OBSERVED_PATH_DIRECT,
		Quality: serviceBaseline(now, cloudpb.ObservedPath_OBSERVED_PATH_DIRECT, p95, loss), QualityValidUntil: now.Add(time.Minute),
		Cost:        smartroute.CostEstimate{State: smartroute.CostNone},
		Constraints: smartroute.CandidateConstraints{Reachable: true, PolicyAllowed: true},
	}
}

func relayServiceCandidate(now time.Time, p95 uint64, loss uint32, cost uint64) smartroute.Candidate {
	return smartroute.Candidate{
		ID: "relay-eu", Path: cloudpb.ObservedPath_OBSERVED_PATH_SINGLE_RELAY, RelayID: "relay-eu", Region: "eu-west",
		Quality: serviceBaseline(now, cloudpb.ObservedPath_OBSERVED_PATH_SINGLE_RELAY, p95, loss), QualityValidUntil: now.Add(time.Minute),
		Cost:        smartroute.CostEstimate{State: smartroute.CostEstimated, EstimatedMicrounits: cost, ValidUntil: now.Add(time.Minute)},
		Constraints: smartroute.CandidateConstraints{Reachable: true, Healthy: true, CapacityAvailable: true, PolicyAllowed: true, Entitled: true},
	}
}

func serviceBaseline(now time.Time, path cloudpb.ObservedPath, p95 uint64, loss uint32) quality.Baseline {
	return quality.Baseline{
		Series:      quality.SeriesKey{ObservedPath: path, NetworkClass: "wifi", Region: "eu-west"},
		WindowCount: 2, SampleCount: 8, MeanWindowRTTP50Millis: p95 / 2, MeanWindowRTTP95Millis: p95,
		LatestWindowEndedAt:    now,
		MeanWindowJitterMillis: p95 / 10, LossBasisPoints: loss, MeanThroughputBPS: 1_000_000, ConnectedMillis: 120_000,
	}
}
