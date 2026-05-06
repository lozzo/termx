package grpcapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	pb "github.com/lozzow/termx/termx-remote/protocol/hubgrpc"
)

const pushPollInterval = 200 * time.Millisecond

// RegistryAdapter decouples grpcapi from registry/cloud packages.
type RegistryAdapter interface {
	RegisterAgent(RegisterAgentInput) (RegisterAgentOutput, error)
	HeartbeatAgent(sessionID string, terminals []TerminalInput) error
	GetPendingOffer(ctx context.Context, sessionID string) (*PendingOffer, error)
	SubmitAnswer(sessionID, answerSessionID, sdp string, candidates []string) error
	GetPendingPairingClaim(ctx context.Context, sessionID string) (*PendingPairingClaim, error)
	SubmitPairingResult(PairingResultInput) error
}

type RegisterAgentInput struct {
	AgentID     string
	DeviceID    string
	MachineID   string
	DisplayName string
	Hostname    string
	Platform    string
	Version     string
	Terminals   []TerminalInput
}

type TerminalInput struct {
	TerminalID    string
	Name          string
	RemoteEnabled bool
}

type RegisterAgentOutput struct {
	SessionID                string
	ICEServers               []ICEServer
	HeartbeatIntervalSeconds int32
	AllowRelay               bool
	AllowRelayTransfer       bool
}

type ICEServer struct {
	URLs       []string
	Username   string
	Credential string
}

type PendingOffer struct {
	SessionID    string
	MachineID    string
	TerminalID   string
	SDP          string
	SessionToken string
	Candidates   []string
}

type PendingPairingClaim struct {
	ClaimID               string
	PairSessionID         string
	PairSecret            string
	AppDeviceID           string
	AppName               string
	RequestedCapabilities []string
}

type PairingResultInput struct {
	ClaimID      string
	SessionToken string
	ExpiresAt    string
	MachineID    string
	MachineName  string
}

type Server struct {
	pb.UnimplementedAgentHubServer

	registry RegistryAdapter
}

func NewServer(registry RegistryAdapter) *Server {
	return &Server{registry: registry}
}

func (s *Server) Connect(stream pb.AgentHub_ConnectServer) error {
	if err := requireBearer(stream.Context()); err != nil {
		return err
	}
	if s.registry == nil {
		return status.Error(codes.FailedPrecondition, "registry is not configured")
	}

	first, err := stream.Recv()
	if err != nil {
		return err
	}
	reg := first.GetRegister()
	if reg == nil {
		return status.Error(codes.InvalidArgument, "first message must be register")
	}

	out, err := s.registry.RegisterAgent(RegisterAgentInput{
		AgentID:     reg.GetAgentId(),
		DeviceID:    reg.GetDeviceId(),
		MachineID:   reg.GetMachineId(),
		DisplayName: reg.GetDisplayName(),
		Hostname:    reg.GetHostname(),
		Platform:    reg.GetPlatform(),
		Version:     reg.GetVersion(),
		Terminals:   terminalInputs(reg.GetTerminals()),
	})
	if err != nil {
		return status.Errorf(codes.Internal, "register: %v", err)
	}

	sender := lockedSender{stream: stream}
	if err := sender.Send(&pb.HubToAgent{Payload: &pb.HubToAgent_RegisterAck{
		RegisterAck: &pb.RegisterResponse{
			AgentSessionId:           out.SessionID,
			IceServers:               iceServers(out.ICEServers),
			HeartbeatIntervalSeconds: out.HeartbeatIntervalSeconds,
			RelayPolicy: &pb.RelayPolicy{
				AllowRelay:         out.AllowRelay,
				AllowRelayTransfer: out.AllowRelayTransfer,
			},
		},
	}}); err != nil {
		return err
	}

	ctx := stream.Context()
	go s.pushOffers(ctx, &sender, out.SessionID)
	go s.pushPairing(ctx, &sender, out.SessionID)

	for {
		msg, err := stream.Recv()
		if errors.Is(err, io.EOF) || ctx.Err() != nil {
			return nil
		}
		if err != nil {
			return err
		}
		switch p := msg.GetPayload().(type) {
		case *pb.AgentToHub_Heartbeat:
			_ = s.registry.HeartbeatAgent(out.SessionID, terminalInputs(p.Heartbeat.GetTerminals()))
		case *pb.AgentToHub_SignalingAnswer:
			answer := p.SignalingAnswer
			_ = s.registry.SubmitAnswer(out.SessionID, answer.GetSessionId(), answer.GetSdp(), answer.GetIceCandidates())
		case *pb.AgentToHub_PairingResult:
			result := p.PairingResult
			_ = s.registry.SubmitPairingResult(PairingResultInput{
				ClaimID:      result.GetClaimId(),
				SessionToken: result.GetSessionToken(),
				ExpiresAt:    result.GetExpiresAt(),
				MachineID:    result.GetMachineId(),
				MachineName:  result.GetMachineName(),
			})
		default:
			return status.Error(codes.InvalidArgument, "unsupported message")
		}
	}
}

func (s *Server) pushOffers(ctx context.Context, sender interface{ Send(*pb.HubToAgent) error }, sessionID string) {
	for ctx.Err() == nil {
		offer, err := s.registry.GetPendingOffer(ctx, sessionID)
		if err != nil || offer == nil {
			waitOrDone(ctx, pushPollInterval)
			continue
		}
		if err := sender.Send(&pb.HubToAgent{Payload: &pb.HubToAgent_SignalingOffer{
			SignalingOffer: &pb.SignalingOffer{
				SessionId:     offer.SessionID,
				MachineId:     offer.MachineID,
				TerminalId:    offer.TerminalID,
				Sdp:           offer.SDP,
				IceCandidates: offer.Candidates,
				SessionToken:  offer.SessionToken,
			},
		}}); err != nil {
			return
		}
	}
}

func (s *Server) pushPairing(ctx context.Context, sender interface{ Send(*pb.HubToAgent) error }, sessionID string) {
	for ctx.Err() == nil {
		claim, err := s.registry.GetPendingPairingClaim(ctx, sessionID)
		if err != nil || claim == nil {
			waitOrDone(ctx, pushPollInterval)
			continue
		}
		if err := sender.Send(&pb.HubToAgent{Payload: &pb.HubToAgent_PairingClaim{
			PairingClaim: &pb.PairingClaim{
				ClaimId:               claim.ClaimID,
				PairSessionId:         claim.PairSessionID,
				PairSecret:            claim.PairSecret,
				AppDeviceId:           claim.AppDeviceID,
				AppName:               claim.AppName,
				RequestedCapabilities: claim.RequestedCapabilities,
			},
		}}); err != nil {
			return
		}
	}
}

type lockedSender struct {
	mu     sync.Mutex
	stream pb.AgentHub_ConnectServer
}

func (s *lockedSender) Send(msg *pb.HubToAgent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stream.Send(msg)
}

func waitOrDone(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

func requireBearer(ctx context.Context) error {
	token, err := ExtractBearerToken(ctx)
	if err != nil {
		return status.Error(codes.Unauthenticated, err.Error())
	}
	if token == "" {
		return status.Error(codes.Unauthenticated, "token is empty")
	}
	return nil
}

// ExtractBearerToken extracts the Bearer token value for callers that need to
// pass the relay credential through to this gRPC service.
func ExtractBearerToken(ctx context.Context) (string, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", fmt.Errorf("metadata required")
	}
	auths := md.Get("authorization")
	if len(auths) == 0 {
		return "", fmt.Errorf("Bearer token required")
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(auths[0], prefix) {
		return "", fmt.Errorf("Bearer token required")
	}
	return strings.TrimSpace(strings.TrimPrefix(auths[0], prefix)), nil
}

func terminalInputs(in []*pb.Terminal) []TerminalInput {
	if len(in) == 0 {
		return nil
	}
	out := make([]TerminalInput, len(in))
	for i, terminal := range in {
		out[i] = TerminalInput{
			TerminalID:    terminal.GetTerminalId(),
			Name:          terminal.GetName(),
			RemoteEnabled: terminal.GetRemoteEnabled(),
		}
	}
	return out
}

func iceServers(in []ICEServer) []*pb.RTCIceServer {
	if len(in) == 0 {
		return nil
	}
	out := make([]*pb.RTCIceServer, len(in))
	for i, ice := range in {
		out[i] = &pb.RTCIceServer{
			Urls:       append([]string(nil), ice.URLs...),
			Username:   ice.Username,
			Credential: ice.Credential,
		}
	}
	return out
}
