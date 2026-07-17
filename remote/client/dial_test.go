package client

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	clientendpoint "github.com/lozzow/termx/client/endpoint"
	clientruntime "github.com/lozzow/termx/client/runtime"
	core "github.com/lozzow/termx/core"
	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/proto/apipb"
	"github.com/lozzow/termx/proto/cloudpb"
	"github.com/lozzow/termx/proto/remoteauthpb"
	"github.com/lozzow/termx/proto/wire"
	remotev2daemon "github.com/lozzow/termx/remote/daemon"
	remotev2webrtc "github.com/lozzow/termx/remote/webrtc"
	"github.com/lozzow/termx/shared/cloudcompanion"
	"github.com/lozzow/termx/shared/cloudcompanion/pathquality"
	"github.com/lozzow/termx/shared/remoteauth"
	"github.com/lozzow/termx/shared/transport"
	pion "github.com/pion/webrtc/v4"
)

func TestDialRunsE2EHandshakeBeforeTermxProtocolWithoutSendingGrantToCompanion(t *testing.T) {
	identity, grant, store, now := dialIdentityFixture(t, "device-1")
	core := core.NewServer()
	answerer := remotev2webrtc.Answerer{Handler: remotev2daemon.SessionAcceptor{
		Core: core, Identity: identity, AccessStore: store, Now: fixedDialNow(now),
	}}
	companion := signalingCompanion(answerer, "device-1")
	connection, err := Dial(context.Background(), DialOptions{
		Companion: companion, EndpointID: "lab", TargetDeviceID: "device-1",
		DeviceFingerprint: identity.Fingerprint, Credential: grant,
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
	application := newRemoteApplicationSession(t, "lab", client)
	if _, err := application.TerminalList(context.Background(), &apipb.TerminalListCommand{}); err != nil {
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

func TestDialCarriesFileDownloadOverAuthenticatedProtocolChannel(t *testing.T) {
	identity, grant, store, now := dialIdentityFixtureWithScope(t, "device-file", remoteauth.FullDaemonScope())
	grant.EndpointID = "lab-file"
	grant.Identity.EndpointID = "lab-file"
	path := filepath.Join(t.TempDir(), "remote-file.txt")
	content := []byte("file payload stays inside authenticated data channel")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	answerer := remotev2webrtc.Answerer{Handler: remotev2daemon.SessionAcceptor{Core: core.NewServer(), Identity: identity, AccessStore: store, Now: fixedDialNow(now)}}
	connection, err := Dial(context.Background(), DialOptions{Companion: signalingCompanion(answerer, "device-file"), EndpointID: "lab-file", TargetDeviceID: "device-file", DeviceFingerprint: identity.Fingerprint, Credential: grant, RoutePreference: cloudpb.RoutePreference_ROUTE_PREFERENCE_DIRECT_ONLY, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	client := protocol.NewClient(connection)
	defer client.Close()
	if err := client.Hello(context.Background(), protocol.Hello{Version: wire.Version, Client: "remote-file-test"}); err != nil {
		t.Fatal(err)
	}
	entry, err := client.FileStat(context.Background(), path)
	if err != nil || entry.Size != int64(len(content)) {
		t.Fatalf("stat %#v %v", entry, err)
	}
	opened, err := client.FileDownloadOpen(context.Background(), protocol.FileDownloadOpenParams{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	stream, stop := client.Stream(opened.Channel)
	defer stop()
	var received []byte
	for {
		select {
		case frame := <-stream:
			switch frame.Type {
			case wire.TypeFileData:
				data, err := protocol.DecodeFileTransferData(frame.Payload)
				if err != nil {
					t.Fatal(err)
				}
				received = append(received, data.Data...)
			case wire.TypeFileFinish:
				if !bytes.Equal(received, content) {
					t.Fatalf("remote file mismatch %q", received)
				}
				return
			}
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for remote file")
		}
	}
}

func TestDialReportsStableManagedConnectionPhases(t *testing.T) {
	identity, grant, store, now := dialIdentityFixture(t, "device-1")
	answerer := remotev2webrtc.Answerer{Handler: remotev2daemon.SessionAcceptor{
		Core: core.NewServer(), Identity: identity, AccessStore: store, Now: fixedDialNow(now),
	}}
	phases := make([]cloudcompanion.EndpointPhase, 0, 5)
	connection, err := Dial(context.Background(), DialOptions{
		Companion: signalingCompanion(answerer, "device-1"), EndpointID: "lab", TargetDeviceID: "device-1",
		DeviceFingerprint: identity.Fingerprint, Credential: grant,
		RoutePreference: cloudpb.RoutePreference_ROUTE_PREFERENCE_DIRECT_ONLY, Now: now,
		Phase: func(phase cloudcompanion.EndpointPhase) { phases = append(phases, phase) },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	want := []cloudcompanion.EndpointPhase{
		cloudcompanion.EndpointPhaseResolving, cloudcompanion.EndpointPhaseSignaling,
		cloudcompanion.EndpointPhaseConnecting, cloudcompanion.EndpointPhaseAuthorizing,
		cloudcompanion.EndpointPhaseConnected,
	}
	if !slices.Equal(phases, want) {
		t.Fatalf("managed phases = %v, want %v", phases, want)
	}
}

func TestDialRejectsGrantDeviceMismatchBeforeCompanionRequest(t *testing.T) {
	identity, grant, _, now := dialIdentityFixture(t, "device-1")
	companion := &cloudcompanion.FakeClient{}
	if _, err := Dial(context.Background(), DialOptions{
		Companion: companion, EndpointID: "lab", TargetDeviceID: "device-2",
		DeviceFingerprint: identity.Fingerprint, Credential: grant,
		RoutePreference: cloudpb.RoutePreference_ROUTE_PREFERENCE_DIRECT_ONLY,
		Now:             now,
	}); err == nil {
		t.Fatal("grant device mismatch must fail before Companion request")
	}
	if requests := companion.Requests(); len(requests.ResolveEndpoint) != 0 {
		t.Fatalf("Companion saw request before local grant validation: %+v", requests)
	}
}

func TestDialRejectsCredentialFromAnotherEndpointBeforeCompanionRequest(t *testing.T) {
	identity, grant, _, now := dialIdentityFixture(t, "device-1")
	companion := &cloudcompanion.FakeClient{}
	if _, err := Dial(context.Background(), DialOptions{
		Companion: companion, EndpointID: "other-endpoint", TargetDeviceID: "device-1",
		DeviceFingerprint: identity.Fingerprint, Credential: grant,
		RoutePreference: cloudpb.RoutePreference_ROUTE_PREFERENCE_DIRECT_ONLY, Now: now,
	}); err == nil || !strings.Contains(err.Error(), "belongs to endpoint") {
		t.Fatalf("cross-endpoint credential error = %v", err)
	}
	if requests := companion.Requests(); len(requests.ResolveEndpoint) != 0 {
		t.Fatalf("Companion saw cross-endpoint credential request: %+v", requests)
	}
}

func TestDialRejectsCompanionRoutedImpostorByDeviceHelloPin(t *testing.T) {
	trustedIdentity, grant, _, now := dialIdentityFixture(t, "device-1")
	impostorIdentity, _, impostorStore, _ := dialIdentityFixture(t, "device-impostor")
	core := &recordingScopedCore{}
	answerer := remotev2webrtc.Answerer{Handler: remotev2daemon.SessionAcceptor{
		Core: core, Identity: impostorIdentity, AccessStore: impostorStore, Now: fixedDialNow(now),
	}}
	companion := signalingCompanion(answerer, "device-1")
	_, err := Dial(context.Background(), DialOptions{
		Companion: companion, EndpointID: "lab", TargetDeviceID: "device-1",
		DeviceFingerprint: trustedIdentity.Fingerprint, Credential: grant,
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

func TestDialRejectsNonCanonicalManagedSessionCorrelationID(t *testing.T) {
	identity, grant, _, now := dialIdentityFixture(t, "device-1")
	companion := &cloudcompanion.FakeClient{ResolveEndpointFunc: func(context.Context, *cloudpb.ResolveEndpointRequest) (*cloudpb.ResolvedEndpoint, error) {
		return &cloudpb.ResolvedEndpoint{EndpointId: "lab", TargetDeviceId: "device-1", ManagedSessionId: " managed-1 "}, nil
	}}
	_, err := Dial(context.Background(), DialOptions{
		Companion: companion, EndpointID: "lab", TargetDeviceID: "device-1",
		DeviceFingerprint: identity.Fingerprint, Credential: grant,
		RoutePreference: cloudpb.RoutePreference_ROUTE_PREFERENCE_DIRECT_ONLY, Now: now,
	})
	if !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL) {
		t.Fatalf("non-canonical managed session error = %v", err)
	}
	if requests := companion.Requests(); len(requests.CreateSignalingSession) != 0 {
		t.Fatalf("invalid managed session reached signaling: %+v", requests.CreateSignalingSession)
	}
}

func TestDialSingleTerminalGrantCannotEscapeCoreScope(t *testing.T) {
	identity, grant, store, now := dialIdentityFixtureWithScope(t, "device-1", remoteauth.Scope{TerminalID: "allowed"})
	core := core.NewServer()
	answerer := remotev2webrtc.Answerer{Handler: remotev2daemon.SessionAcceptor{
		Core: core, Identity: identity, AccessStore: store, Now: fixedDialNow(now),
	}}
	connection, err := Dial(context.Background(), DialOptions{
		Companion: signalingCompanion(answerer, "device-1"), EndpointID: "lab", TargetDeviceID: "device-1",
		DeviceFingerprint: identity.Fingerprint, Credential: grant,
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
	application := newRemoteApplicationSession(t, "lab", client)
	if _, err := application.TerminalList(context.Background(), &apipb.TerminalListCommand{}); err == nil || !strings.Contains(err.Error(), "forbidden") {
		t.Fatalf("single-terminal grant escaped through List: %v", err)
	}
	if _, err := application.TerminalGet(context.Background(), &apipb.TerminalGetCommand{Terminal: &apipb.TerminalRef{EndpointId: "lab", TerminalId: "denied"}}); err == nil || !strings.Contains(err.Error(), "forbidden") {
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

func TestDialSessionReportsQualityWithoutChangingRoute(t *testing.T) {
	identity, grant, store, now := dialIdentityFixture(t, "device-1")
	answerer := remotev2webrtc.Answerer{Handler: remotev2daemon.SessionAcceptor{
		Core: core.NewServer(), Identity: identity, AccessStore: store, Now: fixedDialNow(now),
	}}
	companion := signalingCompanion(answerer, "device-1")
	session, err := DialSession(context.Background(), DialOptions{
		Companion: companion, EndpointID: "lab", TargetDeviceID: "device-1",
		DeviceFingerprint: identity.Fingerprint, Credential: grant,
		RoutePreference: cloudpb.RoutePreference_ROUTE_PREFERENCE_DIRECT_ONLY,
		QualityObservation: QualityObservationOptions{
			Enabled: true, SampleInterval: 5 * time.Millisecond, Window: 20 * time.Millisecond, NetworkClass: "test-network",
		},
		Now: now,
	})
	if err != nil {
		t.Fatalf("DialSession: %v", err)
	}
	client := protocol.NewClient(session.Transport)
	if err := client.Hello(context.Background(), protocol.Hello{Version: wire.Version, Client: "remote-v2-quality-test"}); err != nil {
		_ = client.Close()
		t.Fatalf("protocol Hello: %v", err)
	}
	application := newRemoteApplicationSession(t, "lab", client)
	if _, err := application.TerminalList(context.Background(), &apipb.TerminalListCommand{}); err != nil {
		_ = client.Close()
		t.Fatalf("protocol List: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for len(companion.Requests().ReportPathQuality) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case <-session.ObservationDone:
	case <-time.After(3 * time.Second):
		t.Fatal("quality reporter did not finish after transport close")
	}
	recorded := companion.Requests()
	if len(recorded.ReportPathQuality) == 0 {
		t.Fatal("managed WebRTC session produced no quality window")
	}
	window, err := pathquality.Decode(recorded.ReportPathQuality[0].GetSummary())
	if err != nil {
		t.Fatalf("reported quality window: %v", err)
	}
	if window.ManagedSessionID != "managed-1" || window.ObservedPath != cloudpb.ObservedPath_OBSERVED_PATH_DIRECT || window.NetworkClass != "test-network" {
		t.Fatalf("quality window identity = %+v", window.Metadata)
	}
	if len(recorded.AcquireRelayLease) != 0 {
		t.Fatalf("quality observation acquired a new route: %+v", recorded.AcquireRelayLease)
	}
	if len(recorded.ReportConnectionOutcome) != 1 || recorded.ReportConnectionOutcome[0].GetOutcome().GetObservedPath() != cloudpb.ObservedPath_OBSERVED_PATH_DIRECT {
		t.Fatalf("connection outcomes = %+v", recorded.ReportConnectionOutcome)
	}
}

func newRemoteApplicationSession(t *testing.T, endpointID string, client *protocol.Client) *clientruntime.ApplicationSession {
	t.Helper()
	session, err := clientruntime.NewApplicationSession(clientruntime.EndpointSessionStamp{EndpointID: clientendpoint.EndpointID(endpointID), RouteID: "webrtc", Generation: 1}, client)
	if err != nil {
		t.Fatal(err)
	}
	return session
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
		ReportPathQualityFunc: func(context.Context, *cloudpb.ReportPathQualityRequest) (*cloudpb.ReportPathQualityResponse, error) {
			return &cloudpb.ReportPathQualityResponse{}, nil
		},
		ReportConnectionOutcomeFunc: func(context.Context, *cloudpb.ReportConnectionOutcomeRequest) (*cloudpb.ReportConnectionOutcomeResponse, error) {
			return &cloudpb.ReportConnectionOutcomeResponse{}, nil
		},
	}
}

func dialIdentityFixture(t *testing.T, deviceID string) (remoteauth.Identity, remoteauth.ClientAccessCredential, *remoteauth.AccessStore, time.Time) {
	return dialIdentityFixtureWithScope(t, deviceID, remoteauth.Scope{AllowDaemon: true})
}

func dialIdentityFixtureWithScope(t *testing.T, deviceID string, scope remoteauth.Scope) (remoteauth.Identity, remoteauth.ClientAccessCredential, *remoteauth.AccessStore, time.Time) {
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
	clientIdentity, err := remoteauth.GenerateClientAccessIdentity("lab", rand.Reader)
	if err != nil {
		t.Fatalf("GenerateClientAccessIdentity: %v", err)
	}
	store, err := remoteauth.LoadAccessStore(t.TempDir(), identity, remoteauth.AccessStoreOptions{Now: fixedDialNow(now)})
	if err != nil {
		t.Fatalf("LoadAccessStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	bundle, _, err := store.IssuePairingBundle(remoteauth.PairingIssueOptions{Scope: scope, TicketTTL: time.Hour, GrantLifetime: time.Hour, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := remoteauth.EncodePairingBundle(bundle)
	if err != nil {
		t.Fatal(err)
	}
	result, err := store.RedeemPairingBundle(payload, clientIdentity.PublicKey, "dial-fixture", now)
	if err != nil {
		t.Fatal(err)
	}
	return identity, remoteauth.ClientAccessCredential{
		Version: 1, EndpointID: clientIdentity.EndpointID, Identity: clientIdentity,
		CapabilityGrant: result.Grant, UpdatedAt: now,
	}, store, now
}

func fixedDialNow(now time.Time) func() time.Time {
	return func() time.Time { return now }
}

type recordingScopedCore struct {
	calls int
}

func (core *recordingScopedCore) ServeScopedTransport(context.Context, transport.Transport, core.TransportScope) error {
	core.calls++
	return nil
}
