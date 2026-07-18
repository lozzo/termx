package managed

import (
	"context"
	"fmt"
	"io"
	"slices"
	"sync"
	"testing"
	"time"

	peeradapter "github.com/lozzow/termx/client/adapter/peer"
	"github.com/lozzow/termx/client/endpoint"
	"github.com/lozzow/termx/client/port"
	clientruntime "github.com/lozzow/termx/client/runtime"
	internalprotocol "github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/proto/apipb"
	"github.com/lozzow/termx/proto/cloudpb"
	"github.com/lozzow/termx/proto/wire"
	"github.com/lozzow/termx/shared/cloudcompanion"
	"github.com/lozzow/termx/shared/remoteauth"
	"github.com/lozzow/termx/shared/transport"
	"google.golang.org/protobuf/proto"
)

func TestDialBuildsAuthorizedProtocolReadyPeerSession(t *testing.T) {
	attempt := managedAttempt(t)
	channel := newScriptedProtocolChannel(t)
	peer := &fakeManagedPeer{channel: channel, observedPath: endpoint.PathDirect, fingerprint: "sha-256:aa:bb"}
	order := make([]string, 0, 4)
	authorizer := &fakeAuthorizer{prepare: func(request clientruntime.AttemptRequest) (peeradapter.PreparedAuthorization, error) {
		order = append(order, "prepare")
		if request.Stamp() != attempt.Stamp() {
			t.Fatalf("authorization attempt = %#v, want %#v", request.Stamp(), attempt.Stamp())
		}
		return &fakePreparedAuthorization{authenticate: func(_ transport.Transport, fingerprint string) error {
			order = append(order, "authorize")
			if fingerprint != peer.fingerprint {
				t.Fatalf("fingerprint = %q", fingerprint)
			}
			return nil
		}}, nil
	}}
	stream := cloudcompanion.NewFakeSignalingStream(1)
	if err := stream.Push(&cloudpb.SignalingEvent{Payload: &cloudpb.SignalingEvent_Answer{Answer: &cloudpb.SignalingAnswer{Sdp: "answer-sdp"}}}); err != nil {
		t.Fatal(err)
	}
	cloud := &cloudcompanion.FakeClient{
		ResolveEndpointFunc: func(context.Context, *cloudpb.ResolveEndpointRequest) (*cloudpb.ResolvedEndpoint, error) {
			order = append(order, "resolve")
			return &cloudpb.ResolvedEndpoint{EndpointId: "studio", TargetDeviceId: "device-1", ManagedSessionId: "managed-1"}, nil
		},
		CreateSignalingSessionFunc: func(context.Context, *cloudpb.CreateSignalingSessionRequest) (cloudcompanion.SignalingStream, error) {
			order = append(order, "signal")
			return stream, nil
		},
	}
	phases := make([]clientruntime.EndpointPhase, 0, 6)
	dialer := &Dialer{
		Cloud: cloud, Peers: fakePeerFactory{peer: peer}, Authorization: authorizer,
		ClientName: "managed-engine-test", Phase: func(phase clientruntime.EndpointPhase) { phases = append(phases, phase) },
	}
	ready, err := dialer.Connect(context.Background(), attempt)
	if err != nil {
		t.Fatal(err)
	}
	defer ready.Close()
	if ready.Stamp() != attempt.Stamp() || ready.ObservedPath() != string(endpoint.PathDirect) {
		t.Fatalf("ready session = stamp %#v path %q", ready.Stamp(), ready.ObservedPath())
	}
	application, ok := ready.(clientruntime.ApplicationReadyPeerSession)
	if !ok {
		t.Fatalf("ready session %T does not expose Proto application API", ready)
	}
	result, err := application.ExecuteApplication(context.Background(), &apipb.CommandEnvelope{
		Command: &apipb.CommandEnvelope_TerminalList{TerminalList: &apipb.TerminalListCommand{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.GetTerminalList() == nil || channel.helloClient != "managed-engine-test" || channel.applicationCalls != 1 {
		t.Fatalf("protocol result=%#v hello=%q calls=%d", result, channel.helloClient, channel.applicationCalls)
	}
	terminal, ok := ready.(clientruntime.TerminalResponseApplicationExecutor)
	if !ok {
		t.Fatalf("managed ready session %T does not expose terminal responses", ready)
	}
	result, err = terminal.ExecuteApplicationTerminal(context.Background(), &apipb.CommandEnvelope{
		Command: &apipb.CommandEnvelope_FileUploadOpen{FileUploadOpen: &apipb.FileUploadOpenCommand{Path: "/tmp/demo", Size: 8}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.GetFileTransferOpen() == nil || channel.applicationCalls != 2 {
		t.Fatalf("terminal protocol result=%#v calls=%d", result, channel.applicationCalls)
	}
	if !slices.Equal(order, []string{"prepare", "resolve", "signal", "authorize"}) {
		t.Fatalf("managed order = %v", order)
	}
	wantPhases := []clientruntime.EndpointPhase{
		clientruntime.EndpointPhaseResolving, clientruntime.EndpointPhaseSignaling, clientruntime.EndpointPhaseConnecting,
		clientruntime.EndpointPhaseAuthorizing, clientruntime.EndpointPhaseReady,
	}
	if !slices.Equal(phases, wantPhases) {
		t.Fatalf("phases = %v, want %v", phases, wantPhases)
	}
	if peer.offerCalls != 1 || peer.answer != "answer-sdp" || !peer.waited {
		t.Fatalf("peer orchestration = offer %d answer %q waited %v", peer.offerCalls, peer.answer, peer.waited)
	}
}

func TestDialRejectsAuthorizationBeforeCloudResolution(t *testing.T) {
	attempt := managedAttempt(t)
	cloud := &cloudcompanion.FakeClient{}
	dialer := &Dialer{
		Cloud: cloud, Peers: fakePeerFactory{},
		Authorization: &fakeAuthorizer{prepare: func(clientruntime.AttemptRequest) (peeradapter.PreparedAuthorization, error) {
			return nil, fmt.Errorf("credential unavailable")
		}},
	}
	if _, err := dialer.Connect(context.Background(), attempt); err == nil {
		t.Fatal("missing credential must fail")
	}
	if requests := cloud.Requests(); len(requests.ResolveEndpoint) != 0 || len(requests.CreateSignalingSession) != 0 {
		t.Fatalf("Cloud observed request before local authorization: %+v", requests)
	}
}

func managedAttempt(t *testing.T) clientruntime.AttemptRequest {
	t.Helper()
	identity := endpoint.DaemonIdentity{DeviceID: "device-1", DeviceFingerprint: "device-fingerprint"}
	target := endpoint.Endpoint{
		ID: "studio", DaemonIdentity: identity,
		Routes: map[endpoint.RouteID]endpoint.AccessRoute{
			"cloud": {
				ID: "cloud", Kind: endpoint.RouteManagedWebRTC, Enabled: true, Source: endpoint.SourceCloud, PolicySource: endpoint.SourceUser,
				CredentialRef: "credential:studio", TargetDeviceID: "device-1", AccountProfileRef: "default", RelayMode: endpoint.RelayDirect,
			},
		},
	}
	attempt, err := clientruntime.NewAttemptRequest(target, "cloud", 7, clientruntime.ConnectIntentInteractive)
	if err != nil {
		t.Fatal(err)
	}
	return attempt
}

type fakeAuthorizer struct {
	prepare func(clientruntime.AttemptRequest) (peeradapter.PreparedAuthorization, error)
}

func (authorizer *fakeAuthorizer) Prepare(_ context.Context, request clientruntime.AttemptRequest) (peeradapter.PreparedAuthorization, error) {
	return authorizer.prepare(request)
}

type fakePreparedAuthorization struct {
	authenticate func(transport.Transport, string) error
}

func (authorization *fakePreparedAuthorization) Authenticate(_ context.Context, connection transport.Transport, fingerprint string) (remoteauth.Claims, error) {
	return remoteauth.Claims{}, authorization.authenticate(connection, fingerprint)
}

type fakePeerFactory struct{ peer *fakeManagedPeer }

func (factory fakePeerFactory) OpenManagedPeer(_ context.Context, _ []*cloudpb.IceServer, preference cloudpb.RoutePreference, relayOnly bool) (port.ManagedPeer, error) {
	if factory.peer == nil {
		return nil, fmt.Errorf("peer unavailable")
	}
	if preference != cloudpb.RoutePreference_ROUTE_PREFERENCE_DIRECT_ONLY || relayOnly {
		return nil, fmt.Errorf("unexpected peer policy %s relay=%v", preference, relayOnly)
	}
	return factory.peer, nil
}

type fakeManagedPeer struct {
	channel       port.ManagedMessageChannel
	fingerprint   string
	observedPath  endpoint.Path
	offerCalls    int
	answer        string
	waited        bool
	closed        bool
	snapshots     []port.ManagedPeerSnapshot
	snapshotIndex int
}

func (peer *fakeManagedPeer) Channel() port.ManagedMessageChannel { return peer.channel }
func (peer *fakeManagedPeer) CreateOffer(context.Context) (string, error) {
	peer.offerCalls++
	return "offer-sdp", nil
}
func (peer *fakeManagedPeer) ApplyAnswer(_ context.Context, answer string, _ []*cloudpb.IceCandidate) error {
	peer.answer = answer
	return nil
}
func (peer *fakeManagedPeer) WaitReady(context.Context) error { peer.waited = true; return nil }
func (peer *fakeManagedPeer) RemoteCertificateFingerprint() (string, error) {
	return peer.fingerprint, nil
}
func (peer *fakeManagedPeer) ObservedPath() endpoint.Path { return peer.observedPath }
func (peer *fakeManagedPeer) Snapshot(time.Time) (port.ManagedPeerSnapshot, bool) {
	if peer.snapshotIndex >= len(peer.snapshots) {
		return port.ManagedPeerSnapshot{}, false
	}
	snapshot := peer.snapshots[peer.snapshotIndex]
	peer.snapshotIndex++
	return snapshot, true
}
func (peer *fakeManagedPeer) Close() error { peer.closed = true; return nil }

type scriptedProtocolChannel struct {
	t                *testing.T
	mu               sync.Mutex
	messageHandler   func([]byte)
	closeHandler     func()
	closed           bool
	helloClient      string
	applicationCalls int
}

func newScriptedProtocolChannel(t *testing.T) *scriptedProtocolChannel {
	return &scriptedProtocolChannel{t: t}
}

func (channel *scriptedProtocolChannel) SetMessageHandler(handler func([]byte)) {
	channel.messageHandler = handler
}
func (channel *scriptedProtocolChannel) SetCloseHandler(handler func()) {
	channel.closeHandler = handler
}
func (channel *scriptedProtocolChannel) BufferedAmount() uint64               { return 0 }
func (channel *scriptedProtocolChannel) SetBufferedAmountLowThreshold(uint64) {}
func (channel *scriptedProtocolChannel) SetBufferedAmountLowHandler(func())   {}

func (channel *scriptedProtocolChannel) Send(frame []byte) error {
	channel.mu.Lock()
	defer channel.mu.Unlock()
	if channel.closed {
		return io.EOF
	}
	controlChannel, frameType, payload, err := wire.DecodeFrame(frame)
	if err != nil || controlChannel != 0 {
		return fmt.Errorf("decode client frame channel=%d: %w", controlChannel, err)
	}
	switch frameType {
	case wire.TypeHello:
		hello, err := internalprotocol.DecodeHelloPayload(payload)
		if err != nil {
			return err
		}
		channel.helloClient = hello.Client
		response, err := internalprotocol.EncodeHelloPayload(internalprotocol.Hello{Version: wire.Version, Server: "scripted-daemon"})
		if err != nil {
			return err
		}
		return channel.deliver(wire.TypeHello, response)
	case wire.TypeRequest:
		request, err := internalprotocol.DecodeRequestPayload(payload)
		if err != nil {
			return err
		}
		if request.Method != "api.execute" {
			return fmt.Errorf("unexpected method %q", request.Method)
		}
		command, err := internalprotocol.DecodeApplicationCommand(request.Params)
		if err != nil {
			return err
		}
		channel.applicationCalls++
		result := &apipb.ResultEnvelope{
			RequestId: command.GetContext().GetRequestId(), OriginSession: proto.Clone(command.GetContext().GetSession()).(*apipb.EndpointSessionStamp),
			Result: &apipb.ResultEnvelope_TerminalList{TerminalList: &apipb.TerminalListResult{}},
		}
		if command.GetFileUploadOpen() != nil {
			result.Result = &apipb.ResultEnvelope_FileTransferOpen{FileTransferOpen: &apipb.FileTransferOpenResult{Transfer: &apipb.FileTransferHandle{
				Resource: &apipb.ResourceHandle{Kind: apipb.ResourceKind_RESOURCE_KIND_FILE_TRANSFER, OpaqueToken: []byte{1}},
				Resume:   &apipb.FileUploadResumeHandle{OpaqueToken: []byte{2}},
			}}}
		}
		resultPayload, err := internalprotocol.EncodeApplicationResult(result)
		if err != nil {
			return err
		}
		response, err := internalprotocol.EncodeResponsePayload(internalprotocol.Response{ID: request.ID, Result: resultPayload})
		if err != nil {
			return err
		}
		return channel.deliver(wire.TypeResponse, response)
	default:
		return fmt.Errorf("unexpected frame type %d", frameType)
	}
}

func (channel *scriptedProtocolChannel) deliver(frameType uint8, payload []byte) error {
	frame, err := wire.EncodeFrame(0, frameType, payload)
	if err != nil {
		return err
	}
	channel.messageHandler(frame)
	return nil
}

func (channel *scriptedProtocolChannel) Close() error {
	channel.mu.Lock()
	if channel.closed {
		channel.mu.Unlock()
		return nil
	}
	channel.closed = true
	handler := channel.closeHandler
	channel.mu.Unlock()
	if handler != nil {
		handler()
	}
	return nil
}
