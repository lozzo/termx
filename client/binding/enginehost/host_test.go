package enginehost

import (
	"bytes"
	"context"
	"testing"

	"github.com/muxvia/muxvia/client/adapter/direct"
	"github.com/muxvia/muxvia/client/endpoint"
	"github.com/muxvia/muxvia/client/port"
	clientruntime "github.com/muxvia/muxvia/client/runtime"
	"github.com/muxvia/muxvia/proto/remoteauthpb"
	"github.com/muxvia/muxvia/shared/remoteauth"
)

func TestDecodeBootstrapAcceptsManualPairingClaimCode(t *testing.T) {
	payload := []byte{0x01, 0x02, 0x03, 0x04}
	decoded, err := decodeBootstrap(remoteauth.EncodePairingClaimCode(payload))
	if err != nil || !bytes.Equal(decoded, payload) {
		t.Fatalf("manual pairing code decoded=%x err=%v", decoded, err)
	}
}

func TestPairingClaimCandidatesProduceValidDirectAndCloudAttempts(t *testing.T) {
	publicKey := bytes.Repeat([]byte{0x21}, 32)
	for name, route := range map[string]*remoteauthpb.PairingRouteSeed{
		"direct": {Route: &remoteauthpb.PairingRouteSeed_DirectWebrtcTcp{DirectWebrtcTcp: &remoteauthpb.PairingDirectRouteSeed{SignalingAddress: "127.0.0.1:41001", IceTcpAddress: "127.0.0.1:41002"}}},
		"cloud":  {Route: &remoteauthpb.PairingRouteSeed_ManagedWebrtc{ManagedWebrtc: &remoteauthpb.PairingManagedRouteSeed{TargetDeviceId: "device-1"}}},
	} {
		t.Run(name, func(t *testing.T) {
			candidate, err := remoteauth.PairingClaimEndpointCandidate(&remoteauthpb.PairingClaimOfferV1{SchemaVersion: remoteauth.PairingClaimOfferVersion, Claim: bytes.Repeat([]byte{0x31}, 16), DeviceId: "device-1", DevicePublicKey: publicKey, ExpiresAtUnixNano: 1, Route: route})
			if err != nil {
				t.Fatal(err)
			}
			selected, err := pairingClaimRoute(candidate)
			if err != nil {
				t.Fatal(err)
			}
			target := pairingTarget("device-1", candidate.Identity, selected, "credential:device-1")
			if _, err := clientruntime.NewAttemptRequest(target, selected.ID, 1, clientruntime.ConnectIntentInteractive); err != nil {
				t.Fatalf("claim attempt is invalid: %#v err=%v", target, err)
			}
		})
	}
}

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

func TestPairingClaimRouteUsesTheSingleAdvertisedRoute(t *testing.T) {
	direct := endpoint.AccessRoute{ID: "direct", Kind: endpoint.RouteDirectWebRTCTCP, Enabled: true}
	cloud := endpoint.AccessRoute{ID: "cloud", Kind: endpoint.RouteManagedWebRTC, Enabled: true}
	target := endpoint.EndpointCandidate{Routes: []endpoint.AccessRoute{cloud}}
	route, err := pairingClaimRoute(target)
	if err != nil || route.ID != cloud.ID {
		t.Fatalf("Cloud pairing route = %#v err=%v", route, err)
	}
	if _, err := pairingClaimRoute(endpoint.EndpointCandidate{Routes: []endpoint.AccessRoute{{ID: direct.ID, Kind: direct.Kind}}}); err == nil {
		t.Fatal("disabled pairing route was accepted")
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
