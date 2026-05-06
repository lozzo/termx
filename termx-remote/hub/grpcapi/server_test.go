package grpcapi

import (
	"context"
	"net"
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
		},
	}}); err != nil {
		t.Fatalf("send answer: %v", err)
	}
	if got := <-reg.answers; got.sessionID != "agent-session-1" ||
		got.answerSessionID != "offer-session-1" || got.sdp != "v=0\r\n" ||
		len(got.candidates) != 1 {
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
		got.SessionToken != "session-token-from-agent" || got.MachineName != "devbox" || got.Error != "pairing rejected" {
		t.Fatalf("pairing result call = %+v", got)
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
}

type answerErrorCall struct {
	sessionID       string
	answerSessionID string
	reason          string
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
	return f.registerOut, nil
}

func (f *fakeRegistry) HeartbeatAgent(sessionID string, terminals []TerminalInput) error {
	f.heartbeats <- heartbeatCall{sessionID: sessionID, terminals: terminals}
	return nil
}

func (f *fakeRegistry) GetPendingOffer(ctx context.Context, sessionID string) (*PendingOffer, error) {
	_ = sessionID
	select {
	case offer := <-f.offers:
		return offer, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (f *fakeRegistry) SubmitAnswer(sessionID, answerSessionID, sdp string, candidates []string) error {
	f.answers <- answerCall{
		sessionID:       sessionID,
		answerSessionID: answerSessionID,
		sdp:             sdp,
		candidates:      candidates,
	}
	return nil
}

func (f *fakeRegistry) SubmitAnswerError(sessionID, answerSessionID, reason string) error {
	f.answerErrors <- answerErrorCall{
		sessionID:       sessionID,
		answerSessionID: answerSessionID,
		reason:          reason,
	}
	return nil
}

func (f *fakeRegistry) GetPendingPairingClaim(ctx context.Context, sessionID string) (*PendingPairingClaim, error) {
	_ = sessionID
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
