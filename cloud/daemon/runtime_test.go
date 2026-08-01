package daemon

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anytty/anytty/cloud/edge/agentgateway"
	"github.com/anytty/anytty/cloud/ticket"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"github.com/anytty/anytty/remote/webrtc"
	"github.com/anytty/anytty/shared/remoteauth"
	"github.com/anytty/anytty/shared/transport"
	pion "github.com/pion/webrtc/v4"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestConnectEdgeJoinsSuccessfulPeerAfterParentCancel(t *testing.T) {
	api := daemonLoopbackWebRTCAPI()
	peerClosed := make(chan struct{})
	runtime, clientPublicKey := daemonRuntimeFixture(t, webrtc.Answerer{
		Handler:         daemonBlockingHandler{},
		PeerConnections: api.NewPeerConnection,
		OnPeerClosed:    func() { close(peerClosed) },
	})
	clientPeer := daemonOfferPeer(t, api, "not-protocol")
	defer clientPeer.Close()
	gateway := &daemonTestAgentGateway{
		offer:  daemonAgentOffer(t, clientPeer, clientPublicKey),
		answer: make(chan *cloudv1.AgentAnswer, 1),
	}
	locator := startDaemonTestAgentGateway(t, gateway)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runtime.connectEdge(ctx, &cloudv1.SignedEnvelope{KeyId: "test-binding"}, locator)
	}()
	waitDaemonAnswer(t, gateway.answer)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("connectEdge error = %v, want context cancellation", err)
		}
		select {
		case <-peerClosed:
		default:
			t.Fatal("connectEdge returned before the successful peer finalizer completed")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("connectEdge did not join the successful peer after parent cancellation")
	}
}

func TestConnectEdgeWaitsForClaimedDataChannelHandler(t *testing.T) {
	api := daemonLoopbackWebRTCAPI()
	handler := &daemonGatedHandler{started: make(chan struct{}), release: make(chan struct{})}
	runtime, clientPublicKey := daemonRuntimeFixture(t, webrtc.Answerer{
		Handler:         handler,
		PeerConnections: api.NewPeerConnection,
	})
	clientPeer := daemonOfferPeer(t, api, "protocol")
	defer clientPeer.Close()
	gateway := &daemonTestAgentGateway{
		offer:  daemonAgentOffer(t, clientPeer, clientPublicKey),
		answer: make(chan *cloudv1.AgentAnswer, 1),
	}
	locator := startDaemonTestAgentGateway(t, gateway)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runtime.connectEdge(ctx, &cloudv1.SignedEnvelope{KeyId: "test-binding"}, locator)
	}()
	answer := waitDaemonAnswer(t, gateway.answer)
	applyDaemonAnswer(t, clientPeer, answer)
	select {
	case <-handler.started:
	case <-time.After(10 * time.Second):
		t.Fatal("protocol DataChannel handler was not claimed")
	}
	cancel()
	select {
	case err := <-done:
		t.Fatalf("connectEdge returned before claimed handler finished: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(handler.release)
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("connectEdge error = %v, want context cancellation", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("connectEdge did not finish after the claimed handler was released")
	}
}

func TestAnswerOfferFailuresDoNotLeakPeerAccounting(t *testing.T) {
	runtime, clientPublicKey := daemonRuntimeFixture(t, webrtc.Answerer{Handler: daemonBlockingHandler{}})
	offer := &cloudv1.AgentOffer{
		CorrelationId: "failure", SessionId: "failure", ClientPublicKey: clientPublicKey,
		AccessMode: cloudv1.CloudClientAccessMode_CLOUD_CLIENT_ACCESS_MODE_CAPABILITY, OfferSdp: "invalid SDP",
	}
	tests := map[string]webrtc.PeerConnectionFactory{
		"factory": func(pion.Configuration) (*pion.PeerConnection, error) {
			return nil, errors.New("injected factory failure")
		},
		"invalid SDP": daemonLoopbackWebRTCAPI().NewPeerConnection,
	}
	for name, factory := range tests {
		t.Run(name, func(t *testing.T) {
			runtime.config.Answerer.PeerConnections = factory
			var peers sync.WaitGroup
			response := runtime.answerOffer(context.Background(), offer, &peers)
			if response.GetRejected().GetCode() != "ANSWER_FAILED" {
				t.Fatalf("response = %#v, want ANSWER_FAILED", response)
			}
			waitDaemonPeers(t, &peers)
		})
	}
}

func TestAnswerOfferPeerAccountingSurvivesOriginalCallbackPanic(t *testing.T) {
	var callbackCalls atomic.Int32
	runtime, clientPublicKey := daemonRuntimeFixture(t, webrtc.Answerer{
		Handler:         daemonBlockingHandler{},
		PeerConnections: daemonLoopbackWebRTCAPI().NewPeerConnection,
		OnPeerClosed: func() {
			callbackCalls.Add(1)
			panic("injected OnPeerClosed panic")
		},
	})
	var peers sync.WaitGroup
	response := runtime.answerOffer(context.Background(), &cloudv1.AgentOffer{
		CorrelationId: "panic", SessionId: "panic", ClientPublicKey: clientPublicKey,
		AccessMode: cloudv1.CloudClientAccessMode_CLOUD_CLIENT_ACCESS_MODE_CAPABILITY, OfferSdp: "invalid SDP",
	}, &peers)
	if response.GetRejected().GetCode() != "ANSWER_FAILED" {
		t.Fatalf("response = %#v, want ANSWER_FAILED", response)
	}
	waitDaemonPeers(t, &peers)
	if got := callbackCalls.Load(); got != 1 {
		t.Fatalf("original OnPeerClosed calls = %d, want 1", got)
	}
}

func TestAnswerOfferConcurrentFactoryFailuresBalancePeerAccounting(t *testing.T) {
	const attempts = 32
	var factoryCalls atomic.Int32
	runtime, clientPublicKey := daemonRuntimeFixture(t, webrtc.Answerer{
		Handler: daemonBlockingHandler{},
		PeerConnections: func(pion.Configuration) (*pion.PeerConnection, error) {
			factoryCalls.Add(1)
			return nil, errors.New("injected factory failure")
		},
	})
	var workers sync.WaitGroup
	var peers sync.WaitGroup
	workers.Add(attempts)
	for index := range attempts {
		go func() {
			defer workers.Done()
			response := runtime.answerOffer(context.Background(), &cloudv1.AgentOffer{
				CorrelationId: fmt.Sprintf("race-%d", index), SessionId: fmt.Sprintf("race-%d", index), ClientPublicKey: clientPublicKey,
				AccessMode: cloudv1.CloudClientAccessMode_CLOUD_CLIENT_ACCESS_MODE_CAPABILITY, OfferSdp: "offer",
			}, &peers)
			if response.GetRejected().GetCode() != "ANSWER_FAILED" {
				t.Errorf("offer %d response = %#v", index, response)
			}
		}()
	}
	workers.Wait()
	waitDaemonPeers(t, &peers)
	if got := factoryCalls.Load(); got != attempts {
		t.Fatalf("factory calls = %d, want %d", got, attempts)
	}
}

type daemonBlockingHandler struct{}

func (daemonBlockingHandler) ServeDataChannel(ctx context.Context, _ transport.Transport, _ string) error {
	<-ctx.Done()
	return ctx.Err()
}

type daemonGatedHandler struct {
	started chan struct{}
	release chan struct{}
}

func (handler *daemonGatedHandler) ServeDataChannel(context.Context, transport.Transport, string) error {
	close(handler.started)
	<-handler.release
	return nil
}

func daemonRuntimeFixture(t *testing.T, answerer webrtc.Answerer) (*Runtime, ed25519.PublicKey) {
	t.Helper()
	identity, err := remoteauth.NewIdentity("daemon-runtime-test", ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x31}, ed25519.SeedSize)))
	if err != nil {
		t.Fatal(err)
	}
	store, err := remoteauth.LoadAccessStore(t.TempDir(), identity, remoteauth.AccessStoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	now := time.Now().UTC()
	bundle, _, err := store.IssuePairingBundle(remoteauth.PairingIssueOptions{
		Label: "daemon runtime test", Scope: remoteauth.FullDaemonScope(), TicketTTL: time.Hour, GrantLifetime: time.Hour, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := remoteauth.EncodePairingBundle(bundle)
	if err != nil {
		t.Fatal(err)
	}
	client, err := remoteauth.GenerateClientAccessIdentity("daemon-runtime-client", bytes.NewReader(bytes.Repeat([]byte{0x42}, 64)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RedeemPairingBundle(payload, client.PublicKey, "daemon runtime client", now); err != nil {
		t.Fatal(err)
	}
	return &Runtime{
		config: Config{
			Record: EnrollmentRecord{DaemonID: "daemon-runtime-test"}, Identity: identity, Answerer: answerer,
			AccessStore: store, SoftwareVersion: "runtime-test",
		},
		bootID:        "daemon-runtime-boot",
		daemonState:   &cloudv1.DaemonStateRecord{DaemonId: "daemon-runtime-test", State: cloudv1.DaemonState_DAEMON_STATE_ACTIVE},
		cloudSessions: make(map[string]*cloudSession),
	}, append(ed25519.PublicKey(nil), client.PublicKey...)
}

func daemonLoopbackWebRTCAPI() *pion.API {
	settings := pion.SettingEngine{}
	settings.SetNetworkTypes([]pion.NetworkType{pion.NetworkTypeUDP4})
	settings.SetIncludeLoopbackCandidate(true)
	settings.SetIPFilter(func(address net.IP) bool { return address.IsLoopback() })
	return pion.NewAPI(pion.WithSettingEngine(settings))
}

func daemonOfferPeer(t *testing.T, api *pion.API, label string) *pion.PeerConnection {
	t.Helper()
	peer, err := api.NewPeerConnection(pion.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := peer.CreateDataChannel(label, nil); err != nil {
		_ = peer.Close()
		t.Fatal(err)
	}
	return peer
}

func daemonAgentOffer(t *testing.T, peer *pion.PeerConnection, clientPublicKey ed25519.PublicKey) *cloudv1.AgentOffer {
	t.Helper()
	offer, err := peer.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	gatherComplete := pion.GatheringCompletePromise(peer)
	if err := peer.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	select {
	case <-gatherComplete:
	case <-time.After(5 * time.Second):
		t.Fatal("client ICE gathering timed out")
	}
	description := peer.LocalDescription()
	if description == nil || description.SDP == "" {
		t.Fatal("client offer has no local SDP")
	}
	return &cloudv1.AgentOffer{
		CorrelationId: "correlation", SessionId: "session", AgentGeneration: 1,
		ClientPublicKey: append([]byte(nil), clientPublicKey...), OfferSdp: description.SDP,
		AccessMode: cloudv1.CloudClientAccessMode_CLOUD_CLIENT_ACCESS_MODE_CAPABILITY,
	}
}

func applyDaemonAnswer(t *testing.T, peer *pion.PeerConnection, answer *cloudv1.AgentAnswer) {
	t.Helper()
	if err := peer.SetRemoteDescription(pion.SessionDescription{Type: pion.SDPTypeAnswer, SDP: answer.GetAnswerSdp()}); err != nil {
		t.Fatal(err)
	}
	for _, candidate := range answer.GetCandidates() {
		mid := candidate.GetSdpMid()
		lineIndex := uint16(candidate.GetSdpMlineIndex())
		usernameFragment := candidate.GetUsernameFragment()
		if err := peer.AddICECandidate(pion.ICECandidateInit{
			Candidate: candidate.GetCandidate(), SDPMid: &mid, SDPMLineIndex: &lineIndex, UsernameFragment: &usernameFragment,
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func waitDaemonAnswer(t *testing.T, answers <-chan *cloudv1.AgentAnswer) *cloudv1.AgentAnswer {
	t.Helper()
	select {
	case answer := <-answers:
		if answer == nil || answer.GetAnswerSdp() == "" {
			t.Fatalf("AgentGateway response = %#v, want answer", answer)
		}
		return answer
	case <-time.After(10 * time.Second):
		t.Fatal("daemon did not answer the AgentGateway offer")
		return nil
	}
}

func waitDaemonPeers(t *testing.T, peers *sync.WaitGroup) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		peers.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("peer accounting did not reach zero")
	}
}

type daemonTestAgentGateway struct {
	cloudv1.UnimplementedAgentGatewayServer
	offer  *cloudv1.AgentOffer
	answer chan *cloudv1.AgentAnswer
}

func (gateway *daemonTestAgentGateway) Connect(stream cloudv1.AgentGateway_ConnectServer) error {
	now := time.Now().UTC().Add(-time.Second)
	challenge := &cloudv1.EdgeChallenge{
		Nonce: bytes.Repeat([]byte{0x51}, ticket.EdgeChallengeNonceSize), EdgeId: "edge-runtime-test", EdgeBootId: "edge-runtime-boot", StreamId: "edge-runtime-stream",
		IssuedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(ticket.EdgeChallengeLifetime)), Target: cloudv1.EdgeChallengeTarget_EDGE_CHALLENGE_TARGET_AGENT_GATEWAY,
	}
	if err := stream.Send(&cloudv1.EdgeCommand{
		ProtocolVersion: agentgateway.ProtocolVersion, MessageId: "challenge", SenderId: challenge.GetEdgeId(), BootId: challenge.GetEdgeBootId(), ConnectionId: challenge.GetStreamId(),
		StreamSeq: 1, SentAt: challenge.GetIssuedAt(), Payload: &cloudv1.EdgeCommand_Challenge{Challenge: challenge},
	}); err != nil {
		return err
	}
	hello, err := stream.Recv()
	if err != nil {
		return err
	}
	if err := stream.Send(&cloudv1.EdgeCommand{
		ProtocolVersion: agentgateway.ProtocolVersion, MessageId: "ready", SenderId: challenge.GetEdgeId(), BootId: challenge.GetEdgeBootId(), ConnectionId: hello.GetConnectionId(),
		StreamSeq: 2, SentAt: timestamppb.Now(), Payload: &cloudv1.EdgeCommand_Ready{Ready: &cloudv1.AgentReady{
			Generation: 1, Heartbeat: &cloudv1.HeartbeatPolicy{Interval: durationpb.New(time.Hour), Timeout: durationpb.New(2 * time.Hour)},
			DaemonState: &cloudv1.DaemonStateRecord{DaemonId: "daemon-runtime-test", State: cloudv1.DaemonState_DAEMON_STATE_ACTIVE, StateRevision: 1},
		}},
	}); err != nil {
		return err
	}
	if err := stream.Send(&cloudv1.EdgeCommand{Payload: &cloudv1.EdgeCommand_Offer{Offer: gateway.offer}}); err != nil {
		return err
	}
	for {
		event, err := stream.Recv()
		if err != nil {
			return err
		}
		if answer := event.GetAnswer(); answer != nil {
			gateway.answer <- answer
			<-stream.Context().Done()
			return stream.Context().Err()
		}
		if rejected := event.GetRejected(); rejected != nil {
			return errors.New("daemon rejected test offer: " + rejected.GetCode())
		}
	}
}

func startDaemonTestAgentGateway(t *testing.T, gateway cloudv1.AgentGatewayServer) *cloudv1.EdgeLocator {
	t.Helper()
	const serverName = "edge-runtime.test"
	rootKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	rootTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "daemon runtime test CA"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	rootDER, err := x509.CreateCertificate(rand.Reader, rootTemplate, rootTemplate, &rootKey.PublicKey, rootKey)
	if err != nil {
		t.Fatal(err)
	}
	root, err := x509.ParseCertificate(rootDER)
	if err != nil {
		t.Fatal(err)
	}
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leafTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: serverName}, DNSNames: []string{serverName},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, root, &leafKey.PublicKey, rootKey)
	if err != nil {
		t.Fatal(err)
	}
	leafKeyDER, err := x509.MarshalPKCS8PrivateKey(leafKey)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := tls.X509KeyPair(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: leafKeyDER}),
	)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{
		MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{certificate},
	})))
	cloudv1.RegisterAgentGatewayServer(server, gateway)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})
	return &cloudv1.EdgeLocator{
		EdgeId: "edge-runtime-test", PublicEndpoint: listener.Addr().String(), ServerName: serverName,
		CaCertificatePem: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: rootDER}),
	}
}
