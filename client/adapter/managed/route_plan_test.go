package managed

import (
	"context"
	"testing"
	"time"

	"github.com/muxvia/muxvia/client/endpoint"
	clientruntime "github.com/muxvia/muxvia/client/runtime"
	"github.com/muxvia/muxvia/proto/cloudpb"
	"github.com/muxvia/muxvia/shared/cloudcompanion"
)

func TestResolveDialRouteRelayOnlyUsesOnlyRelayLease(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	attempt := managedAttemptWithRelayMode(t, endpoint.RelayOnly)
	cloud := &cloudcompanion.FakeClient{AcquireRelayLeaseFunc: func(_ context.Context, request *cloudpb.AcquireRelayLeaseRequest) (*cloudpb.RelayLease, error) {
		return &cloudpb.RelayLease{
			LeaseId: "lease-1", SignedLease: []byte("signed"), ExpiresAtUnix: uint64(now.Add(time.Minute).Unix()),
			PathKind:   cloudpb.ObservedPath_OBSERVED_PATH_SINGLE_RELAY,
			IceServers: []*cloudpb.IceServer{{Urls: []string{"turn:relay.example.com"}, Username: "short", Credential: "secret"}},
		}, nil
	}}
	policy, err := cloudcompanion.DialPolicyForRelayMode(endpoint.RelayOnly)
	if err != nil {
		t.Fatal(err)
	}
	route, err := resolveDialRoute(context.Background(), cloud, attempt, &cloudpb.ResolvedEndpoint{ManagedSessionId: "managed-1"}, policy, now)
	if err != nil {
		t.Fatal(err)
	}
	if !route.relayOnly || route.expectedPath != endpoint.PathSingleRelay || len(route.iceServers) != 1 || len(cloud.Requests().AcquireRelayLease) != 1 {
		t.Fatalf("relay route = %#v requests=%+v", route, cloud.Requests())
	}
}

func TestResolveDialRouteAutoAddsRelayWithoutForcingRelay(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	attempt := managedAttemptWithRelayMode(t, endpoint.RelayAuto)
	cloud := &cloudcompanion.FakeClient{AcquireRelayLeaseFunc: func(_ context.Context, request *cloudpb.AcquireRelayLeaseRequest) (*cloudpb.RelayLease, error) {
		return &cloudpb.RelayLease{
			LeaseId: "lease-auto", SignedLease: []byte("signed"), ExpiresAtUnix: uint64(now.Add(time.Minute).Unix()),
			PathKind:   cloudpb.ObservedPath_OBSERVED_PATH_SINGLE_RELAY,
			IceServers: []*cloudpb.IceServer{{Urls: []string{"turn:relay.example.com"}, Username: "short", Credential: "secret"}},
		}, nil
	}}
	policy, err := cloudcompanion.DialPolicyForRelayMode(endpoint.RelayAuto)
	if err != nil {
		t.Fatal(err)
	}
	resolved := &cloudpb.ResolvedEndpoint{
		ManagedSessionId: "managed-1",
		IceServers:       []*cloudpb.IceServer{{Urls: []string{"stun:stun.example.com"}}},
	}
	route, err := resolveDialRoute(context.Background(), cloud, attempt, resolved, policy, now)
	if err != nil {
		t.Fatal(err)
	}
	if route.relayOnly || route.expectedPath != "" || route.preference != cloudpb.RoutePreference_ROUTE_PREFERENCE_STANDARD_RELAY || len(route.iceServers) != 2 || len(cloud.Requests().AcquireRelayLease) != 1 {
		t.Fatalf("auto route = %#v requests=%+v", route, cloud.Requests())
	}
}

func TestResolveDialRouteAutoKeepsP2PWhenRelayCapabilityIsUnavailable(t *testing.T) {
	for _, code := range []cloudpb.CloudErrorCode{
		cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_ENTITLEMENT_DENIED,
		cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_QUOTA_EXHAUSTED,
	} {
		t.Run(code.String(), func(t *testing.T) {
			attempt := managedAttemptWithRelayMode(t, endpoint.RelayAuto)
			cloud := &cloudcompanion.FakeClient{AcquireRelayLeaseFunc: func(context.Context, *cloudpb.AcquireRelayLeaseRequest) (*cloudpb.RelayLease, error) {
				return nil, &cloudcompanion.Error{Code: code}
			}}
			policy, err := cloudcompanion.DialPolicyForRelayMode(endpoint.RelayAuto)
			if err != nil {
				t.Fatal(err)
			}
			resolved := &cloudpb.ResolvedEndpoint{ManagedSessionId: "managed-1", IceServers: []*cloudpb.IceServer{{Urls: []string{"stun:stun.example.com"}}}}
			route, err := resolveDialRoute(context.Background(), cloud, attempt, resolved, policy, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if route.relayOnly || len(route.iceServers) != 1 || route.iceServers[0].GetUrls()[0] != "stun:stun.example.com" {
				t.Fatalf("auto P2P fallback = %#v", route)
			}
		})
	}
}

func TestResolveDialRouteRejectsUnsafeSmartPlan(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	attempt := managedAttemptWithRelayMode(t, endpoint.RelaySmart)
	cloud := &cloudcompanion.FakeClient{PlanManagedRouteFunc: func(_ context.Context, request *cloudpb.PlanManagedRouteRequest) (*cloudpb.ManagedRoutePlan, error) {
		return &cloudpb.ManagedRoutePlan{
			PlanId: "plan-1", ManagedSessionId: request.GetManagedSessionId(), TargetDeviceId: request.GetTargetDeviceId(),
			SelectedPath: cloudpb.ObservedPath_OBSERVED_PATH_RELAY_MESH, SelectionReason: cloudpb.RouteSelectionReason_ROUTE_SELECTION_REASON_INITIAL_BEST,
			ValidUntilUnix: uint64(now.Add(time.Minute).Unix()),
		}, nil
	}}
	policy, err := cloudcompanion.DialPolicyForRelayMode(endpoint.RelaySmart)
	if err != nil {
		t.Fatal(err)
	}
	_, err = resolveDialRoute(context.Background(), cloud, attempt, &cloudpb.ResolvedEndpoint{ManagedSessionId: "managed-1"}, policy, now)
	if !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL) {
		t.Fatalf("unsafe smart plan error = %v", err)
	}
}

func managedAttemptWithRelayMode(t *testing.T, mode endpoint.RelayMode) clientruntime.AttemptRequest {
	t.Helper()
	identity := endpoint.DaemonIdentity{DeviceID: "device-1", DeviceFingerprint: "device-fingerprint"}
	target := endpoint.Endpoint{
		ID: "studio", DaemonIdentity: identity,
		Routes: map[endpoint.RouteID]endpoint.AccessRoute{
			"cloud": {
				ID: "cloud", Kind: endpoint.RouteManagedWebRTC, Enabled: true, Source: endpoint.SourceCloud, PolicySource: endpoint.SourceUser,
				TargetDeviceID: identity.DeviceID, AccountProfileRef: "default", RelayMode: mode,
			},
		},
	}
	attempt, err := clientruntime.NewAttemptRequest(target, "cloud", 1, clientruntime.ConnectIntentInteractive)
	if err != nil {
		t.Fatal(err)
	}
	return attempt
}
