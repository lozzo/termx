package client

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/internal/protocol"
	termxcorev2 "github.com/lozzow/termx/termx-core-v2"
	"github.com/lozzow/termx/termx-proto/cloudpb"
	"github.com/lozzow/termx/termx-proto/remoteauthpb"
	"github.com/lozzow/termx/termx-proto/wire"
	remotev2daemon "github.com/lozzow/termx/termx-remote-v2/daemon"
	remotev2webrtc "github.com/lozzow/termx/termx-remote-v2/webrtc"
	"github.com/lozzow/termx/termx-shared/cloudcompanion"
	"github.com/lozzow/termx/termx-shared/remoteauth"
	"github.com/lozzow/termx/termx-shared/transport"
	pion "github.com/pion/webrtc/v4"
)

func TestDialRunsE2EHandshakeBeforeTermxProtocolWithoutSendingGrantToCompanion(t *testing.T) {
	identity, grant, now := dialIdentityFixture(t, "device-1")
	core := termxcorev2.NewServer()
	answerer := remotev2webrtc.Answerer{Handler: remotev2daemon.SessionAcceptor{
		Core: core, Identity: identity, Revocations: remoteauth.NewRevocations(), Now: fixedDialNow(now),
	}}
	companion := signalingCompanion(answerer, "device-1")
	connection, err := Dial(context.Background(), DialOptions{
		Companion: companion, EndpointID: "lab", TargetDeviceID: "device-1",
		DeviceFingerprint: identity.Fingerprint, CapabilityGrant: grant,
		RoutePreference: cloudpb.RoutePreference_ROUTE_PREFERENCE_DIRECT_ONLY,
		Now:             now,
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	client := protocol.NewClient(connection)
	defer client.Close()
	if err := client.Hello(context.Background(), protocol.Hello{Version: wire.Version, Client: "remote-v2-e2e-test"}); err != nil {
		t.Fatalf("protocol Hello after capability accepted: %v", err)
	}
	if _, err := client.List(context.Background()); err != nil {
		t.Fatalf("protocol List after capability accepted: %v", err)
	}
	recorded := companion.Requests()
	if len(recorded.ResolveEndpoint) != 1 || len(recorded.CreateSignalingSession) != 1 {
		t.Fatalf("recorded companion requests = %+v", recorded)
	}
	signaling := recorded.CreateSignalingSession[0]
	if signaling.GetEndpointId() != "lab" || signaling.GetTargetDeviceId() != "device-1" || signaling.GetOfferSdp() == "" {
		t.Fatalf("signaling request = %+v", signaling)
	}
}

func TestDialRejectsGrantDeviceMismatchBeforeCompanionRequest(t *testing.T) {
	identity, grant, now := dialIdentityFixture(t, "device-1")
	companion := &cloudcompanion.FakeClient{}
	if _, err := Dial(context.Background(), DialOptions{
		Companion: companion, EndpointID: "lab", TargetDeviceID: "device-2",
		DeviceFingerprint: identity.Fingerprint, CapabilityGrant: grant,
		RoutePreference: cloudpb.RoutePreference_ROUTE_PREFERENCE_DIRECT_ONLY,
		Now:             now,
	}); err == nil {
		t.Fatal("grant device mismatch must fail before Companion request")
	}
	if requests := companion.Requests(); len(requests.ResolveEndpoint) != 0 {
		t.Fatalf("Companion saw request before local grant validation: %+v", requests)
	}
}

func TestDialRejectsCompanionRoutedImpostorByDeviceHelloPin(t *testing.T) {
	trustedIdentity, grant, now := dialIdentityFixture(t, "device-1")
	impostorIdentity, _, _ := dialIdentityFixture(t, "device-impostor")
	core := &recordingScopedCore{}
	answerer := remotev2webrtc.Answerer{Handler: remotev2daemon.SessionAcceptor{
		Core: core, Identity: impostorIdentity, Revocations: remoteauth.NewRevocations(), Now: fixedDialNow(now),
	}}
	companion := signalingCompanion(answerer, "device-1")
	_, err := Dial(context.Background(), DialOptions{
		Companion: companion, EndpointID: "lab", TargetDeviceID: "device-1",
		DeviceFingerprint: trustedIdentity.Fingerprint, CapabilityGrant: grant,
		RoutePreference: cloudpb.RoutePreference_ROUTE_PREFERENCE_DIRECT_ONLY,
		Now:             now,
	})
	if remoteauth.HandshakeCodeOf(err) != remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_DEVICE_IDENTITY_MISMATCH {
		t.Fatalf("Dial error = %v, want DEVICE_IDENTITY_MISMATCH", err)
	}
	if core.calls != 0 {
		t.Fatalf("impostor channel reached core %d times", core.calls)
	}
}

func TestDialSingleTerminalGrantCannotEscapeCoreScope(t *testing.T) {
	identity, grant, now := dialIdentityFixtureWithScope(t, "device-1", remoteauth.Scope{TerminalID: "allowed"})
	core := termxcorev2.NewServer()
	answerer := remotev2webrtc.Answerer{Handler: remotev2daemon.SessionAcceptor{
		Core: core, Identity: identity, Revocations: remoteauth.NewRevocations(), Now: fixedDialNow(now),
	}}
	connection, err := Dial(context.Background(), DialOptions{
		Companion: signalingCompanion(answerer, "device-1"), EndpointID: "lab", TargetDeviceID: "device-1",
		DeviceFingerprint: identity.Fingerprint, CapabilityGrant: grant,
		RoutePreference: cloudpb.RoutePreference_ROUTE_PREFERENCE_DIRECT_ONLY,
		Now:             now,
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	client := protocol.NewClient(connection)
	defer client.Close()
	if err := client.Hello(context.Background(), protocol.Hello{Version: wire.Version, Client: "remote-v2-scope-test"}); err != nil {
		t.Fatalf("protocol Hello: %v", err)
	}
	if _, err := client.List(context.Background()); err == nil || !strings.Contains(err.Error(), "transport scope") {
		t.Fatalf("single-terminal grant escaped through List: %v", err)
	}
	var info protocol.TerminalInfo
	if err := client.Call(context.Background(), "get", protocol.GetParams{TerminalID: "denied"}, &info); err == nil || !strings.Contains(err.Error(), "transport scope") {
		t.Fatalf("single-terminal grant escaped through get: %v", err)
	}
}

func TestPeerConfigurationEnforcesRelayOnlyPolicy(t *testing.T) {
	servers := []*cloudpb.IceServer{{Urls: []string{"stun:stun.example.com", "turn:turn.example.com"}, Username: "client", Credential: "secret"}}
	configuration, err := peerConfiguration(servers, cloudpb.RoutePreference_ROUTE_PREFERENCE_STANDARD_RELAY, true)
	if err != nil {
		t.Fatalf("relay-only configuration: %v", err)
	}
	if configuration.ICETransportPolicy != pion.ICETransportPolicyRelay || len(configuration.ICEServers) != 1 {
		t.Fatalf("relay-only policy not enforced: %#v", configuration)
	}
	if _, err := peerConfiguration(servers, cloudpb.RoutePreference_ROUTE_PREFERENCE_DIRECT_ONLY, true); err == nil {
		t.Fatal("direct-only route must reject relay-only ICE policy")
	}
}

func signalingCompanion(answerer remotev2webrtc.Answerer, targetDeviceID string) *cloudcompanion.FakeClient {
	return &cloudcompanion.FakeClient{
		ResolveEndpointFunc: func(context.Context, *cloudpb.ResolveEndpointRequest) (*cloudpb.ResolvedEndpoint, error) {
			return &cloudpb.ResolvedEndpoint{EndpointId: "lab", TargetDeviceId: targetDeviceID, ManagedSessionId: "managed-1"}, nil
		},
		CreateSignalingSessionFunc: func(ctx context.Context, request *cloudpb.CreateSignalingSessionRequest) (cloudcompanion.SignalingStream, error) {
			answer, answerErr := answerer.Answer(ctx, &cloudpb.SignalingOffer{
				SignalingSessionId: "signal-1", ManagedSessionId: request.GetManagedSessionId(), Sdp: request.GetOfferSdp(),
			}, nil)
			if answerErr != nil {
				return nil, answerErr
			}
			stream := cloudcompanion.NewFakeSignalingStream(1)
			if err := stream.Push(&cloudpb.SignalingEvent{Payload: &cloudpb.SignalingEvent_Answer{Answer: answer}}); err != nil {
				return nil, err
			}
			return stream, nil
		},
	}
}

func dialIdentityFixture(t *testing.T, deviceID string) (remoteauth.Identity, string, time.Time) {
	return dialIdentityFixtureWithScope(t, deviceID, remoteauth.Scope{AllowDaemon: true})
}

func dialIdentityFixtureWithScope(t *testing.T, deviceID string, scope remoteauth.Scope) (remoteauth.Identity, string, time.Time) {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate identity: %v", err)
	}
	identity, err := remoteauth.NewIdentity(deviceID, privateKey)
	if err != nil {
		t.Fatalf("NewIdentity: %v", err)
	}
	now := time.Now().UTC()
	grant, err := remoteauth.Issue(privateKey, remoteauth.Claims{
		GrantID: "grant-1", IssuerDeviceID: deviceID, Scope: scope,
		IssuedAt: now.Add(-time.Minute), NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
		RevocationID: "grant-1", Nonce: "dial-test-nonce",
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	return identity, grant, now
}

func fixedDialNow(now time.Time) func() time.Time {
	return func() time.Time { return now }
}

type recordingScopedCore struct {
	calls int
}

func (core *recordingScopedCore) ServeScopedTransport(context.Context, transport.Transport, termxcorev2.TransportScope) error {
	core.calls++
	return nil
}
