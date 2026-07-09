package grpcapi

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	pb "github.com/lozzow/termx/termx-remote/protocol/hubgrpc"
)

const bufSize = 1024 * 1024

func TestConnectRequiresBearerToken(t *testing.T) {
	client, cleanup := startTestServer(t, newFakeRegistry())
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	stream, err := client.Connect(ctx)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	_, err = stream.Recv()
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("Recv error code = %v, want %v (err=%v)", status.Code(err), codes.Unauthenticated, err)
	}
}

func TestConnectRequiresRegisterAsFirstMessage(t *testing.T) {
	client, cleanup := startTestServer(t, newFakeRegistry())
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer relay-token")

	stream, err := client.Connect(ctx)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := stream.Send(&pb.AgentToHub{Payload: &pb.AgentToHub_Heartbeat{
		Heartbeat: &pb.HeartbeatRequest{AgentSessionId: "session-1"},
	}}); err != nil {
		t.Fatalf("send heartbeat: %v", err)
	}
	_, err = stream.Recv()
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Recv error code = %v, want %v (err=%v)", status.Code(err), codes.InvalidArgument, err)
	}
}

func TestConnectRelaysMessagesThroughRegistryAdapter(t *testing.T) {
	reg := newFakeRegistry()
	reg.registerOut = RegisterAgentOutput{
		SessionID:                "agent-session-1",
		HeartbeatIntervalSeconds: 15,
		ICEServers: []ICEServer{{
			URLs:       []string{"stun:stun.example.test"},
			Username:   "ice-user",
			Credential: "ice-pass",
		}},
		AllowRelay:         true,
		AllowRelayTransfer: true,
	}
	client, cleanup := startTestServer(t, reg)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer relay-token")

	stream, err := client.Connect(ctx)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := stream.Send(registerMessage()); err != nil {
		t.Fatalf("send register: %v", err)
	}

	ackMsg, err := stream.Recv()
	if err != nil {
		t.Fatalf("recv register ack: %v", err)
	}
	ack := ackMsg.GetRegisterAck()
	if ack.GetAgentSessionId() != "agent-session-1" ||
		ack.GetHeartbeatIntervalSeconds() != 15 ||
		len(ack.GetIceServers()) != 1 ||
		!ack.GetRelayPolicy().GetAllowRelay() ||
		!ack.GetRelayPolicy().GetAllowRelayTransfer() {
		t.Fatalf("register ack = %+v", ack)
	}
	if got := <-reg.registered; got.AgentID != "agent-1" || got.DeviceID != "device-1" ||
		got.MachineID != "machine-1" || len(got.Terminals) != 1 || !got.Terminals[0].RemoteEnabled {
		t.Fatalf("registered input = %+v", got)
	}

	reg.offers <- &PendingOffer{
		SessionID:    "offer-session-1",
		MachineID:    "machine-1",
		TerminalID:   "terminal-1",
		SDP:          "v=0\r\n",
		Candidates:   []string{"candidate:1"},
		SessionToken: "agent-verifies-this-token",
	}
	offerMsg, err := stream.Recv()
	if err != nil {
		t.Fatalf("recv offer: %v", err)
	}
	offer := offerMsg.GetSignalingOffer()
	if offer.GetSessionId() != "offer-session-1" || offer.GetSessionToken() != "agent-verifies-this-token" {
		t.Fatalf("offer = %+v", offer)
	}
	if offer.GetNonce() == "" {
		t.Fatalf("offer nonce is empty")
	}
	if raw, err := base64.RawURLEncoding.DecodeString(offer.GetNonce()); err != nil || len(raw) != 32 {
		t.Fatalf("offer nonce bytes = %d err = %v", len(raw), err)
	}
	if issuedAt := offer.GetIssuedAt(); issuedAt <= 0 || time.Since(time.Unix(issuedAt, 0)) > time.Minute {
		t.Fatalf("offer issued_at = %d", issuedAt)
	}

	reg.claims <- &PendingPairingClaim{
		ClaimID:               "claim-1",
		PairSessionID:         "pair-session-1",
		PairSecret:            "pair-secret",
		AppDeviceID:           "app-device-1",
		AppName:               "TermX App",
		RequestedCapabilities: []string{"terminal"},
	}
	claimMsg, err := stream.Recv()
	if err != nil {
		t.Fatalf("recv pairing claim: %v", err)
	}
	if claim := claimMsg.GetPairingClaim(); claim.GetClaimId() != "claim-1" || claim.GetPairSecret() != "pair-secret" {
		t.Fatalf("claim = %+v", claim)
	}

	if err := stream.Send(&pb.AgentToHub{Payload: &pb.AgentToHub_Heartbeat{
		Heartbeat: &pb.HeartbeatRequest{
			AgentSessionId: "client-supplied-session-is-ignored",
			Terminals: []*pb.Terminal{{
				TerminalId:    "terminal-2",
				Name:          "zsh",
				RemoteEnabled: true,
			}},
		},
	}}); err != nil {
		t.Fatalf("send heartbeat: %v", err)
	}
	if got := <-reg.heartbeats; got.sessionID != "agent-session-1" ||
		len(got.terminals) != 1 || got.terminals[0].TerminalID != "terminal-2" {
		t.Fatalf("heartbeat call = %+v", got)
	}

	if err := stream.Send(&pb.AgentToHub{Payload: &pb.AgentToHub_SignalingAnswer{
		SignalingAnswer: &pb.SignalingAnswer{
			SessionId:     "offer-session-1",
			Sdp:           "v=0\r\n",
			IceCandidates: []string{"candidate:2"},
			AnswerProof:   "proof-1",
		},
	}}); err != nil {
		t.Fatalf("send answer: %v", err)
	}
	if got := <-reg.answers; got.sessionID != "agent-session-1" ||
		got.answerSessionID != "offer-session-1" || got.sdp != "v=0\r\n" ||
		len(got.candidates) != 1 || got.answerProof != "proof-1" {
		t.Fatalf("answer call = %+v", got)
	}

	if err := stream.Send(&pb.AgentToHub{Payload: &pb.AgentToHub_PairingResult{
		PairingResult: &pb.PairingResult{
			ClaimId:      "claim-1",
			SessionToken: "session-token-from-agent",
			ExpiresAt:    "2026-05-06T00:00:00Z",
			MachineId:    "machine-1",
			MachineName:  "devbox",
			Error:        "pairing rejected",
		},
	}}); err != nil {
		t.Fatalf("send pairing result: %v", err)
	}
	if got := <-reg.pairingResults; got.ClaimID != "claim-1" ||
		got.SessionID != "agent-session-1" || got.SessionToken != "session-token-from-agent" || got.MachineName != "devbox" || got.Error != "pairing rejected" {
		t.Fatalf("pairing result call = %+v", got)
	}
}

func TestConnectRegisterForcedOfflineSendsKickAndClosesStream(t *testing.T) {
	reg := newFakeRegistry()
	reg.registerErr = ErrAgentForcedOffline
	client, cleanup := startTestServer(t, reg)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer relay-token")

	stream, err := client.Connect(ctx)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := stream.Send(registerMessage()); err != nil {
		t.Fatalf("send register: %v", err)
	}

	kickMsg, err := stream.Recv()
	if err != nil {
		t.Fatalf("recv kick: %v", err)
	}
	if kick := kickMsg.GetKick(); kick == nil || kick.GetReason() != "forced offline" {
		t.Fatalf("kick = %+v", kickMsg)
	}
	_, err = stream.Recv()
	if status.Code(err) != codes.PermissionDenied || !strings.Contains(err.Error(), "register: forced offline") {
		t.Fatalf("stream error = %v, want PermissionDenied forced offline", err)
	}
}

func TestConnectHeartbeatErrorSendsKickAndClosesStream(t *testing.T) {
	reg := newFakeRegistry()
	reg.heartbeatErr = ErrAgentForcedOffline
	client, cleanup := startTestServer(t, reg)
	defer cleanup()

	stream := openRegisteredStream(t, client)
	if err := stream.Send(&pb.AgentToHub{Payload: &pb.AgentToHub_Heartbeat{
		Heartbeat: &pb.HeartbeatRequest{AgentSessionId: "agent-session-1"},
	}}); err != nil {
		t.Fatalf("send heartbeat: %v", err)
	}

	kickMsg, err := stream.Recv()
	if err != nil {
		t.Fatalf("recv kick: %v", err)
	}
	if kick := kickMsg.GetKick(); kick == nil || kick.GetReason() != "forced offline" {
		t.Fatalf("kick = %+v", kickMsg)
	}
	_, err = stream.Recv()
	if status.Code(err) != codes.PermissionDenied || !strings.Contains(err.Error(), "heartbeat: forced offline") {
		t.Fatalf("stream error = %v, want PermissionDenied forced offline", err)
	}
}

func TestConnectAnswerErrorIsNotSwallowed(t *testing.T) {
	reg := newFakeRegistry()
	reg.answerErr = errors.New("answer rejected")
	client, cleanup := startTestServer(t, reg)
	defer cleanup()

	stream := openRegisteredStream(t, client)
	if err := stream.Send(&pb.AgentToHub{Payload: &pb.AgentToHub_SignalingAnswer{
		SignalingAnswer: &pb.SignalingAnswer{
			SessionId: "offer-session-1",
			Sdp:       "v=0\r\n",
		},
	}}); err != nil {
		t.Fatalf("send answer: %v", err)
	}

	_, err := stream.Recv()
	if status.Code(err) != codes.Internal || !strings.Contains(err.Error(), "submit answer: answer rejected") {
		t.Fatalf("stream error = %v, want Internal answer error", err)
	}
}

func TestConnectPushOfferSessionInvalidSendsKickAndClosesStream(t *testing.T) {
	reg := newFakeRegistry()
	reg.offerErr = ErrAgentSessionInvalid
	client, cleanup := startTestServer(t, reg)
	defer cleanup()

	stream := openRegisteredStream(t, client)
	kickMsg, err := stream.Recv()
	if err != nil {
		t.Fatalf("recv kick: %v", err)
	}
	if kick := kickMsg.GetKick(); kick == nil || kick.GetReason() != "agent session invalid" {
		t.Fatalf("kick = %+v", kickMsg)
	}
	_, err = stream.Recv()
	if status.Code(err) != codes.Unauthenticated || !strings.Contains(err.Error(), "agent stream: agent session invalid") {
		t.Fatalf("stream error = %v, want Unauthenticated session invalid", err)
	}
}

func TestConnectPushPairingForcedOfflineSendsKickAndClosesStream(t *testing.T) {
	reg := newFakeRegistry()
	reg.pairingErr = ErrAgentForcedOffline
	client, cleanup := startTestServer(t, reg)
	defer cleanup()

	stream := openRegisteredStream(t, client)
	kickMsg, err := stream.Recv()
	if err != nil {
		t.Fatalf("recv kick: %v", err)
	}
	if kick := kickMsg.GetKick(); kick == nil || kick.GetReason() != "forced offline" {
		t.Fatalf("kick = %+v", kickMsg)
	}
	_, err = stream.Recv()
	if status.Code(err) != codes.PermissionDenied || !strings.Contains(err.Error(), "agent stream: forced offline") {
		t.Fatalf("stream error = %v, want PermissionDenied forced offline", err)
	}
}

func TestPushOffersStopsOnNonceGenerationFailure(t *testing.T) {
	original := randomRead
	randomRead = func([]byte) (int, error) {
		return 0, fmt.Errorf("entropy unavailable")
	}
	t.Cleanup(func() {
		randomRead = original
	})

	reg := newFakeRegistry()
	reg.offers <- &PendingOffer{SessionID: "offer-session-1"}
	server := NewServer(reg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stopErr := make(chan error, 1)
	server.pushOffersWithStop(ctx, &hubToAgentCapture{}, "agent-session-1", func(err error) {
		stopErr <- err
		cancel()
	})

	select {
	case err := <-stopErr:
		if err == nil || !strings.Contains(err.Error(), "generate offer nonce") {
			t.Fatalf("stop err = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatalf("pushOffersWithStop did not stop after nonce generation failure")
	}
}

func startTestServer(t *testing.T, registry RegistryAdapter) (pb.AgentHubClient, func()) {
	t.Helper()

	listener := bufconn.Listen(bufSize)
	server := grpc.NewServer()
	pb.RegisterAgentHubServer(server, NewServer(registry))

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = server.Serve(listener)
	}()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc new client: %v", err)
	}

	cleanup := func() {
		_ = conn.Close()
		server.Stop()
		<-done
		_ = listener.Close()
	}
	return pb.NewAgentHubClient(conn), cleanup
}

func openRegisteredStream(t *testing.T, client pb.AgentHubClient) pb.AgentHub_ConnectClient {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)
	ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer relay-token")

	stream, err := client.Connect(ctx)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := stream.Send(registerMessage()); err != nil {
		t.Fatalf("send register: %v", err)
	}
	ackMsg, err := stream.Recv()
	if err != nil {
		t.Fatalf("recv register ack: %v", err)
	}
	if ack := ackMsg.GetRegisterAck(); ack.GetAgentSessionId() != "agent-session-1" {
		t.Fatalf("register ack = %+v", ack)
	}
	return stream
}

func registerMessage() *pb.AgentToHub {
	return &pb.AgentToHub{Payload: &pb.AgentToHub_Register{
		Register: &pb.RegisterRequest{
			AgentId:     "agent-1",
			DeviceId:    "device-1",
			MachineId:   "machine-1",
			DisplayName: "Dev Machine",
			Hostname:    "devbox",
			Platform:    "darwin",
			Version:     "test",
			Terminals: []*pb.Terminal{{
				TerminalId:    "terminal-1",
				Name:          "shell",
				RemoteEnabled: true,
			}},
		},
	}}
}

type fakeRegistry struct {
	registerOut RegisterAgentOutput

	registerErr    error
	heartbeatErr   error
	offerErr       error
	answerErr      error
	answerErrorErr error
	pairingErr     error

	registered     chan RegisterAgentInput
	heartbeats     chan heartbeatCall
	answers        chan answerCall
	answerErrors   chan answerErrorCall
	pairingResults chan PairingResultInput
	offers         chan *PendingOffer
	claims         chan *PendingPairingClaim
}

type heartbeatCall struct {
	sessionID string
	terminals []TerminalInput
}

type answerCall struct {
	sessionID       string
	answerSessionID string
	sdp             string
	candidates      []string
	answerProof     string
}

type answerErrorCall struct {
	sessionID       string
	answerSessionID string
	reason          string
}

type hubToAgentCapture struct {
	msg *pb.HubToAgent
}

func (s *hubToAgentCapture) Send(msg *pb.HubToAgent) error {
	s.msg = msg
	return nil
}

func newFakeRegistry() *fakeRegistry {
	return &fakeRegistry{
		registerOut: RegisterAgentOutput{
			SessionID:                "agent-session-1",
			HeartbeatIntervalSeconds: 30,
		},
		registered:     make(chan RegisterAgentInput, 1),
		heartbeats:     make(chan heartbeatCall, 1),
		answers:        make(chan answerCall, 1),
		answerErrors:   make(chan answerErrorCall, 1),
		pairingResults: make(chan PairingResultInput, 1),
		offers:         make(chan *PendingOffer, 1),
		claims:         make(chan *PendingPairingClaim, 1),
	}
}

func (f *fakeRegistry) RegisterAgent(in RegisterAgentInput) (RegisterAgentOutput, error) {
	f.registered <- in
	return f.registerOut, f.registerErr
}

func (f *fakeRegistry) HeartbeatAgent(sessionID string, terminals []TerminalInput) error {
	f.heartbeats <- heartbeatCall{sessionID: sessionID, terminals: terminals}
	return f.heartbeatErr
}

func (f *fakeRegistry) WaitForOffer(ctx context.Context, sessionID string) (*PendingOffer, error) {
	_ = sessionID
	if f.offerErr != nil {
		return nil, f.offerErr
	}
	select {
	case offer := <-f.offers:
		return offer, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (f *fakeRegistry) SubmitAnswer(sessionID, answerSessionID, sdp string, candidates []string, answerProof string) error {
	f.answers <- answerCall{
		sessionID:       sessionID,
		answerSessionID: answerSessionID,
		sdp:             sdp,
		candidates:      candidates,
		answerProof:     answerProof,
	}
	return f.answerErr
}

func (f *fakeRegistry) SubmitAnswerError(sessionID, answerSessionID, reason string) error {
	f.answerErrors <- answerErrorCall{
		sessionID:       sessionID,
		answerSessionID: answerSessionID,
		reason:          reason,
	}
	return f.answerErrorErr
}

func (f *fakeRegistry) WaitForPairingClaim(ctx context.Context, sessionID string) (*PendingPairingClaim, error) {
	_ = sessionID
	if f.pairingErr != nil {
		return nil, f.pairingErr
	}
	select {
	case claim := <-f.claims:
		return claim, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (f *fakeRegistry) SubmitPairingResult(in PairingResultInput) error {
	f.pairingResults <- in
	return nil
}
