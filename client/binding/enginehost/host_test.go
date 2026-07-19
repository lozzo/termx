package enginehost

import (
	"context"
	"testing"

	"github.com/lozzow/termx/client/adapter/direct"
	"github.com/lozzow/termx/client/endpoint"
	"github.com/lozzow/termx/client/port"
	clientruntime "github.com/lozzow/termx/client/runtime"
)

type credentialAvailability map[string]bool

func (values credentialAvailability) Available(_ context.Context, _, reference string) bool {
	return values[reference]
}

type cloudEligibility map[endpoint.RouteID]bool

func (values cloudEligibility) Available(_ context.Context, route endpoint.AccessRoute) bool {
	return values[route.ID]
}

func TestPairingTargetKeepsManagedFieldsOutOfDirectRoute(t *testing.T) {
	identity := endpoint.DaemonIdentity{DeviceID: "daemon-1", DeviceFingerprint: "ed25519-sha256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}
	route := endpoint.AccessRoute{
		ID: "direct", Kind: endpoint.RouteDirectWebRTCTCP, Enabled: true, Source: endpoint.SourceBootstrap, PolicySource: endpoint.SourceBootstrap,
		SignalingAddresses: []string{"127.0.0.1:41120"}, ICETCPAddresses: []string{"127.0.0.1:41121"},
	}
	target := pairingTarget("daemon-1", identity, route, "android-access-daemon-1")
	direct, ok := target.Route("direct")
	if !ok {
		t.Fatal("Direct pairing target route is missing")
	}
	if direct.TargetDeviceID != "" {
		t.Fatalf("Direct pairing target leaked managed target_device_id %q", direct.TargetDeviceID)
	}
	if direct.CredentialRef != "android-access-daemon-1" {
		t.Fatalf("Direct pairing credential_ref = %q", direct.CredentialRef)
	}
	if _, err := clientruntime.NewAttemptRequest(target, "direct", 1, clientruntime.ConnectIntentInteractive); err != nil {
		t.Fatalf("Direct pairing attempt rejected: %v", err)
	}
}

func TestDirectPairingRouteRejectsManagedOnlyBundle(t *testing.T) {
	direct := endpoint.AccessRoute{ID: "direct", Kind: endpoint.RouteDirectWebRTCTCP, Enabled: true}
	cloud := endpoint.AccessRoute{ID: "cloud", Kind: endpoint.RouteManagedWebRTC, Enabled: true}
	target := endpoint.EndpointCandidate{Routes: []endpoint.AccessRoute{cloud, direct}}
	route, err := directPairingRoute(target)
	if err != nil || route.ID != direct.ID {
		t.Fatalf("Direct + Cloud pairing route = %#v err=%v", route, err)
	}
	if _, err := directPairingRoute(endpoint.EndpointCandidate{Routes: []endpoint.AccessRoute{cloud}}); err == nil {
		t.Fatal("managed-only pairing bundle was accepted")
	}
}

func TestRoutePlanEnvironmentFiltersOnlyUnavailableManagedRoute(t *testing.T) {
	directPriority, cloudPriority := 10, 20
	target := endpoint.Endpoint{
		ID: "studio", Label: "Studio", LabelSource: endpoint.SourceUser, Enabled: true, ConnectMode: endpoint.ConnectOnDemand,
		DaemonIdentity: endpoint.DaemonIdentity{DeviceID: "daemon-1", DeviceFingerprint: "ed25519-sha256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"},
		Routes: map[endpoint.RouteID]endpoint.AccessRoute{
			"direct": {ID: "direct", Kind: endpoint.RouteDirectWebRTCTCP, Enabled: true, Priority: &directPriority, CredentialRef: "grant:studio", Source: endpoint.SourceManual, PolicySource: endpoint.SourceUser, SignalingAddresses: []string{"studio:41120"}, ICETCPAddresses: []string{"studio:41121"}},
			"cloud":  {ID: "cloud", Kind: endpoint.RouteManagedWebRTC, Enabled: true, Priority: &cloudPriority, CredentialRef: "grant:studio", Source: endpoint.SourceCloud, PolicySource: endpoint.SourceUser, TargetDeviceID: "daemon-1", RelayMode: endpoint.RelayAuto},
		},
	}
	planning, environment, err := routePlanEnvironment(context.Background(), target, Options{DirectPeers: fakeDirectPeerFactory{}, ManagedPeers: fakeManagedPeerFactory{}}, credentialAvailability{"grant:studio": true}, cloudEligibility{"cloud": false})
	if err != nil {
		t.Fatal(err)
	}
	if route, _ := planning.Route("cloud"); route.Enabled {
		t.Fatal("logged-out managed route remained eligible")
	}
	if route, _ := target.Route("cloud"); !route.Enabled {
		t.Fatal("eligibility projection mutated persistent endpoint")
	}
	if len(environment.SupportedRouteKinds) != 1 || environment.SupportedRouteKinds[0] != endpoint.RouteDirectWebRTCTCP {
		t.Fatalf("supported route kinds = %#v", environment.SupportedRouteKinds)
	}
	plan, err := (endpoint.RouteSelectionPlanner{}).Plan(endpoint.RouteSelectionRequest{
		Endpoint: planning, Intent: endpoint.ConnectIntent{Kind: "interactive"}, Generation: 1,
		SupportedRouteKinds: environment.SupportedRouteKinds, AvailableCredentialRefs: environment.AvailableCredentialRefs,
	})
	if err != nil {
		t.Fatal(err)
	}
	if attempts := plan.Groups()[0].Attempts(); len(attempts) != 1 || attempts[0].Route.ID != "direct" {
		t.Fatalf("Cloud failure affected Direct plan: %#v", attempts)
	}
}

type fakeDirectPeerFactory struct{ direct.PeerFactory }
type fakeManagedPeerFactory struct{ port.ManagedPeerFactory }
