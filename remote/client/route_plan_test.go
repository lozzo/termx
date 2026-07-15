package client

import (
	"context"
	"testing"
	"time"

	core "github.com/lozzow/termx/core"
	"github.com/lozzow/termx/proto/cloudpb"
	remotev2daemon "github.com/lozzow/termx/remote/daemon"
	remotev2webrtc "github.com/lozzow/termx/remote/webrtc"
	"github.com/lozzow/termx/shared/cloudcompanion"
)

func TestDialSessionExecutesSmartRoutePlanWithoutAcquiringRelayLease(t *testing.T) {
	identity, grant, store, now := dialIdentityFixture(t, "device-1")
	answerer := remotev2webrtc.Answerer{Handler: remotev2daemon.SessionAcceptor{
		Core: core.NewServer(), Identity: identity, AccessStore: store, Now: fixedDialNow(now),
	}}
	companion := signalingCompanion(answerer, "device-1")
	companion.PlanManagedRouteFunc = func(_ context.Context, request *cloudpb.PlanManagedRouteRequest) (*cloudpb.ManagedRoutePlan, error) {
		return validDirectRoutePlan(request, now), nil
	}
	session, err := DialSession(context.Background(), DialOptions{
		Companion: companion, EndpointID: "lab", TargetDeviceID: "device-1",
		DeviceFingerprint: identity.Fingerprint, Credential: grant,
		RoutePreference: cloudpb.RoutePreference_ROUTE_PREFERENCE_SMART_ROUTE, Now: now,
	})
	if err != nil {
		t.Fatalf("DialSession smart route: %v", err)
	}
	defer session.Transport.Close()
	if session.ObservedPath != cloudcompanion.PathDirect || session.RouteSelectionReason != cloudcompanion.RouteReasonInitialBest {
		t.Fatalf("smart route projection = path %q reason %q", session.ObservedPath, session.RouteSelectionReason)
	}
	recorded := companion.Requests()
	if len(recorded.PlanManagedRoute) != 1 || recorded.PlanManagedRoute[0].GetManagedSessionId() != "managed-1" ||
		recorded.PlanManagedRoute[0].GetRoutePreference() != cloudpb.RoutePreference_ROUTE_PREFERENCE_SMART_ROUTE {
		t.Fatalf("managed route requests = %+v", recorded.PlanManagedRoute)
	}
	if len(recorded.AcquireRelayLease) != 0 {
		t.Fatalf("public dialer acquired RelayLease: %+v", recorded.AcquireRelayLease)
	}
	if len(recorded.CreateSignalingSession) != 1 || recorded.CreateSignalingSession[0].GetRoutePreference() != cloudpb.RoutePreference_ROUTE_PREFERENCE_SMART_ROUTE {
		t.Fatalf("signaling did not preserve smart route intent: %+v", recorded.CreateSignalingSession)
	}
}

func TestResolveDialRouteAcquiresLeaseForExplicitRelayOnly(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	companion := &cloudcompanion.FakeClient{AcquireRelayLeaseFunc: func(_ context.Context, request *cloudpb.AcquireRelayLeaseRequest) (*cloudpb.RelayLease, error) {
		return &cloudpb.RelayLease{
			LeaseId: "lease-1", SignedLease: []byte("signed-lease"), ExpiresAtUnix: uint64(now.Add(5 * time.Minute).Unix()),
			PathKind:   cloudpb.ObservedPath_OBSERVED_PATH_SINGLE_RELAY,
			IceServers: []*cloudpb.IceServer{{Urls: []string{"turn:127.0.0.1:3478?transport=udp"}, Username: "client-short", Credential: "client-secret"}},
		}, nil
	}}
	resolved := &cloudpb.ResolvedEndpoint{ManagedSessionId: "managed-1", TargetDeviceId: "device-1"}
	route, err := resolveDialRoute(context.Background(), DialOptions{
		Companion: companion, RoutePreference: cloudpb.RoutePreference_ROUTE_PREFERENCE_STANDARD_RELAY, RelayOnly: true, Now: now,
	}, "lab", "device-1", resolved)
	if err != nil {
		t.Fatal(err)
	}
	if route.preference != cloudpb.RoutePreference_ROUTE_PREFERENCE_STANDARD_RELAY || !route.relayOnly || route.expectedPath != cloudcompanion.PathSingleRelay || len(route.iceServers) != 1 {
		t.Fatalf("explicit Relay route = %#v", route)
	}
	requests := companion.Requests().AcquireRelayLease
	if len(requests) != 1 || requests[0].GetManagedSessionId() != "managed-1" || requests[0].GetTargetDeviceId() != "device-1" || requests[0].GetRoutePreference() != cloudpb.RoutePreference_ROUTE_PREFERENCE_STANDARD_RELAY {
		t.Fatalf("Relay lease requests = %#v", requests)
	}
}

func TestValidateManagedRoutePlanProducesRelayOnlyICEPolicy(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	request := routePlanRequest()
	plan := validDirectRoutePlan(request, now)
	plan.SelectedPath = cloudpb.ObservedPath_OBSERVED_PATH_SINGLE_RELAY
	plan.RelayOnly = true
	plan.RelayRegion = "eu-west"
	plan.IceServers = []*cloudpb.IceServer{{Urls: []string{"turns:relay.example.com:5349"}, Username: "short-user", Credential: "short-secret"}}
	route, err := validateManagedRoutePlan(request, plan, now)
	if err != nil {
		t.Fatalf("validate relay plan: %v", err)
	}
	if route.preference != cloudpb.RoutePreference_ROUTE_PREFERENCE_STANDARD_RELAY || !route.relayOnly ||
		route.expectedPath != cloudcompanion.PathSingleRelay || route.selectionReason != cloudcompanion.RouteReasonInitialBest {
		t.Fatalf("relay dial route = %+v", route)
	}
}

func TestValidateManagedRoutePlanRejectsUnsafeMaterial(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	tests := []struct {
		name   string
		mutate func(*cloudpb.ManagedRoutePlan)
	}{
		{name: "session mismatch", mutate: func(plan *cloudpb.ManagedRoutePlan) { plan.ManagedSessionId = "other" }},
		{name: "expired", mutate: func(plan *cloudpb.ManagedRoutePlan) { plan.ValidUntilUnix = uint64(now.Unix()) }},
		{name: "overlong", mutate: func(plan *cloudpb.ManagedRoutePlan) { plan.ValidUntilUnix = uint64(now.Add(11 * time.Minute).Unix()) }},
		{name: "unknown reason", mutate: func(plan *cloudpb.ManagedRoutePlan) {
			plan.SelectionReason = cloudpb.RouteSelectionReason_ROUTE_SELECTION_REASON_UNSPECIFIED
		}},
		{name: "mesh", mutate: func(plan *cloudpb.ManagedRoutePlan) {
			plan.SelectedPath = cloudpb.ObservedPath_OBSERVED_PATH_RELAY_MESH
		}},
		{name: "direct turn", mutate: func(plan *cloudpb.ManagedRoutePlan) {
			plan.IceServers = []*cloudpb.IceServer{{Urls: []string{"turn:relay.example.com"}, Username: "u", Credential: "p"}}
		}},
		{name: "unsupported ICE", mutate: func(plan *cloudpb.ManagedRoutePlan) {
			plan.IceServers = []*cloudpb.IceServer{{Urls: []string{"https://relay.example.com"}}}
		}},
		{name: "relay without TURN", mutate: func(plan *cloudpb.ManagedRoutePlan) {
			plan.SelectedPath = cloudpb.ObservedPath_OBSERVED_PATH_SINGLE_RELAY
			plan.RelayOnly = true
			plan.RelayRegion = "eu-west"
		}},
		{name: "noncanonical region", mutate: func(plan *cloudpb.ManagedRoutePlan) {
			plan.SelectedPath = cloudpb.ObservedPath_OBSERVED_PATH_SINGLE_RELAY
			plan.RelayOnly = true
			plan.RelayRegion = " EU-West "
			plan.IceServers = []*cloudpb.IceServer{{Urls: []string{"turn:relay.example.com"}, Username: "u", Credential: "p"}}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := routePlanRequest()
			plan := validDirectRoutePlan(request, now)
			test.mutate(plan)
			if _, err := validateManagedRoutePlan(request, plan, now); !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL) {
				t.Fatalf("unsafe plan error = %v", err)
			}
		})
	}
}

func TestDialRejectsInvalidSmartRoutePlanBeforeSignaling(t *testing.T) {
	identity, grant, _, now := dialIdentityFixture(t, "device-1")
	companion := &cloudcompanion.FakeClient{
		ResolveEndpointFunc: func(context.Context, *cloudpb.ResolveEndpointRequest) (*cloudpb.ResolvedEndpoint, error) {
			return &cloudpb.ResolvedEndpoint{EndpointId: "lab", TargetDeviceId: "device-1", ManagedSessionId: "managed-1"}, nil
		},
		PlanManagedRouteFunc: func(_ context.Context, request *cloudpb.PlanManagedRouteRequest) (*cloudpb.ManagedRoutePlan, error) {
			plan := validDirectRoutePlan(request, now)
			plan.SelectedPath = cloudpb.ObservedPath_OBSERVED_PATH_RELAY_MESH
			return plan, nil
		},
	}
	_, err := Dial(context.Background(), DialOptions{
		Companion: companion, EndpointID: "lab", TargetDeviceID: "device-1",
		DeviceFingerprint: identity.Fingerprint, Credential: grant,
		RoutePreference: cloudpb.RoutePreference_ROUTE_PREFERENCE_SMART_ROUTE, Now: now,
	})
	if !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL) {
		t.Fatalf("invalid smart route error = %v", err)
	}
	if requests := companion.Requests(); len(requests.CreateSignalingSession) != 0 {
		t.Fatalf("invalid route plan reached signaling: %+v", requests.CreateSignalingSession)
	}
}

func routePlanRequest() *cloudpb.PlanManagedRouteRequest {
	return &cloudpb.PlanManagedRouteRequest{
		EndpointId: "lab", ManagedSessionId: "managed-1", TargetDeviceId: "device-1",
		RoutePreference: cloudpb.RoutePreference_ROUTE_PREFERENCE_SMART_ROUTE,
	}
}

func validDirectRoutePlan(request *cloudpb.PlanManagedRouteRequest, now time.Time) *cloudpb.ManagedRoutePlan {
	return &cloudpb.ManagedRoutePlan{
		PlanId: "plan-1", ManagedSessionId: request.GetManagedSessionId(), TargetDeviceId: request.GetTargetDeviceId(),
		SelectedPath:    cloudpb.ObservedPath_OBSERVED_PATH_DIRECT,
		SelectionReason: cloudpb.RouteSelectionReason_ROUTE_SELECTION_REASON_INITIAL_BEST,
		ValidUntilUnix:  uint64(now.Add(5 * time.Minute).Unix()),
	}
}
