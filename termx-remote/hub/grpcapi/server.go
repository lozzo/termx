package grpcapi

import (
	"context"
	"crypto/rand"
	"encoding/base64"
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

var (
	ErrAgentForcedOffline  = errors.New("agent forced offline")
	ErrAgentSessionInvalid = errors.New("agent session invalid")
	ErrPollTimeout         = errors.New("poll timeout")
)

// RegistryAdapter decouples grpcapi from registry/cloud packages.
type RegistryAdapter interface {
	RegisterAgent(RegisterAgentInput) (RegisterAgentOutput, error)
	HeartbeatAgent(sessionID string, terminals []TerminalInput) error
	WaitForOffer(ctx context.Context, sessionID string) (*PendingOffer, error)
	SubmitAnswer(sessionID, answerSessionID, sdp string, candidates []string, answerProof string) error
	SubmitAnswerError(sessionID, answerSessionID, reason string) error
	WaitForPairingClaim(ctx context.Context, sessionID string) (*PendingPairingClaim, error)
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
	SessionID            string
	MachineID            string
	TerminalID           string
	Path                 string
	SDP                  string
	SessionToken         string
	AnswerProofChallenge string
	Candidates           []string
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
	SessionID    string
	ClaimID      string
	SessionToken string
	ExpiresAt    string
	MachineID    string
	MachineName  string
	Error        string
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

	sender := lockedSender{stream: stream}
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
		return closeAgentStream(&sender, "register", err)
	}

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

	ctx, cancel := context.WithCancel(stream.Context())
	defer cancel()
	stopStream := newAgentStreamStopper(cancel, &sender)
	go s.pushOffersWithStop(ctx, &sender, out.SessionID, stopStream.stop)
	go s.pushPairingWithStop(ctx, &sender, out.SessionID, stopStream.stop)

	recvCh := make(chan agentStreamRecv, 1)
	go recvAgentMessages(ctx, stream, recvCh)

	for {
		select {
		case err := <-stopStream.errCh:
			return err
		case recv := <-recvCh:
			if ctx.Err() != nil {
				if err := stopStream.err(); err != nil {
					return err
				}
				return nil
			}
			if errors.Is(recv.err, io.EOF) {
				return nil
			}
			if recv.err != nil {
				return recv.err
			}
			if err := s.handleAgentMessage(&sender, out.SessionID, recv.msg); err != nil {
				cancel()
				return err
			}
		case <-ctx.Done():
			if err := stopStream.err(); err != nil {
				return err
			}
			return nil
		}
	}
}

func (s *Server) handleAgentMessage(sender interface{ Send(*pb.HubToAgent) error }, sessionID string, msg *pb.AgentToHub) error {
	switch p := msg.GetPayload().(type) {
	case *pb.AgentToHub_Heartbeat:
		if err := s.registry.HeartbeatAgent(sessionID, terminalInputs(p.Heartbeat.GetTerminals())); err != nil {
			return closeAgentStream(sender, "heartbeat", err)
		}
	case *pb.AgentToHub_SignalingAnswer:
		answer := p.SignalingAnswer
		var err error
		if strings.TrimSpace(answer.GetError()) != "" {
			err = s.registry.SubmitAnswerError(sessionID, answer.GetSessionId(), answer.GetError())
		} else {
			err = s.registry.SubmitAnswer(sessionID, answer.GetSessionId(), answer.GetSdp(), answer.GetIceCandidates(), answer.GetAnswerProof())
		}
		if err != nil {
			return closeAgentStream(sender, "submit answer", err)
		}
	case *pb.AgentToHub_PairingResult:
		result := p.PairingResult
		if err := s.registry.SubmitPairingResult(PairingResultInput{
			SessionID:    sessionID,
			ClaimID:      result.GetClaimId(),
			SessionToken: result.GetSessionToken(),
			ExpiresAt:    result.GetExpiresAt(),
			MachineID:    result.GetMachineId(),
			MachineName:  result.GetMachineName(),
			Error:        result.GetError(),
		}); err != nil {
			return closeAgentStream(sender, "submit pairing result", err)
		}
	default:
		return status.Error(codes.InvalidArgument, "unsupported message")
	}
	return nil
}

func (s *Server) pushOffers(ctx context.Context, sender interface{ Send(*pb.HubToAgent) error }, sessionID string) {
	s.pushOffersWithStop(ctx, sender, sessionID, func(error) {})
}

func (s *Server) pushOffersWithStop(ctx context.Context, sender interface{ Send(*pb.HubToAgent) error }, sessionID string, stop func(error)) {
	for ctx.Err() == nil {
		offer, err := s.registry.WaitForOffer(ctx, sessionID)
		if err != nil {
			if pollTimeoutError(err) {
				continue
			}
			if agentStreamTerminalError(err) {
				stop(err)
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(200 * time.Millisecond):
			}
			continue
		}
		if offer == nil {
			continue
		}
		nonce, err := randomOfferNonce()
		if err != nil {
			stop(fmt.Errorf("generate offer nonce: %w", err))
			return
		}
		if err := sender.Send(&pb.HubToAgent{Payload: &pb.HubToAgent_SignalingOffer{
			SignalingOffer: &pb.SignalingOffer{
				SessionId:            offer.SessionID,
				MachineId:            offer.MachineID,
				TerminalId:           offer.TerminalID,
				Path:                 offer.Path,
				Sdp:                  offer.SDP,
				IceCandidates:        offer.Candidates,
				SessionToken:         offer.SessionToken,
				AnswerProofChallenge: offer.AnswerProofChallenge,
				Nonce:                nonce,
				IssuedAt:             timeNowUnix(),
			},
		}}); err != nil {
			return
		}
	}
}

func (s *Server) pushPairing(ctx context.Context, sender interface{ Send(*pb.HubToAgent) error }, sessionID string) {
	s.pushPairingWithStop(ctx, sender, sessionID, func(error) {})
}

func (s *Server) pushPairingWithStop(ctx context.Context, sender interface{ Send(*pb.HubToAgent) error }, sessionID string, stop func(error)) {
	for ctx.Err() == nil {
		claim, err := s.registry.WaitForPairingClaim(ctx, sessionID)
		if err != nil {
			if pollTimeoutError(err) {
				continue
			}
			if agentStreamTerminalError(err) {
				stop(err)
				return
			}
			continue
		}
		if claim == nil {
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

type agentStreamRecv struct {
	msg *pb.AgentToHub
	err error
}

func recvAgentMessages(ctx context.Context, stream pb.AgentHub_ConnectServer, out chan<- agentStreamRecv) {
	for {
		msg, err := stream.Recv()
		select {
		case out <- agentStreamRecv{msg: msg, err: err}:
		case <-ctx.Done():
			return
		}
		if err != nil {
			return
		}
	}
}

type agentStreamStopper struct {
	cancel context.CancelFunc
	sender interface{ Send(*pb.HubToAgent) error }
	errCh  chan error
	once   sync.Once
}

func newAgentStreamStopper(cancel context.CancelFunc, sender interface{ Send(*pb.HubToAgent) error }) *agentStreamStopper {
	return &agentStreamStopper{
		cancel: cancel,
		sender: sender,
		errCh:  make(chan error, 1),
	}
}

func (s *agentStreamStopper) stop(err error) {
	if err == nil {
		return
	}
	s.once.Do(func() {
		s.errCh <- closeAgentStream(s.sender, "agent stream", err)
		s.cancel()
	})
}

func (s *agentStreamStopper) err() error {
	select {
	case err := <-s.errCh:
		return err
	default:
		return nil
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

func closeAgentStream(sender interface{ Send(*pb.HubToAgent) error }, operation string, err error) error {
	if err == nil {
		return nil
	}
	if agentStreamTerminalError(err) {
		reason := agentKickReason(err)
		if sender != nil {
			_ = sender.Send(&pb.HubToAgent{Payload: &pb.HubToAgent_Kick{
				Kick: &pb.Kick{Reason: reason},
			}})
		}
		return status.Error(agentTerminalCode(err), operation+": "+reason)
	}
	return status.Errorf(codes.Internal, "%s: %v", operation, err)
}

func agentStreamTerminalError(err error) bool {
	return errors.Is(err, ErrAgentForcedOffline) || errors.Is(err, ErrAgentSessionInvalid)
}

func pollTimeoutError(err error) bool {
	return errors.Is(err, ErrPollTimeout) || errors.Is(err, context.DeadlineExceeded)
}

func randomOfferNonce() (string, error) {
	var raw [32]byte
	if _, err := randomRead(raw[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func timeNowUnix() int64 {
	return timeNow().Unix()
}

var randomRead = rand.Read

var timeNow = func() time.Time {
	return time.Now().UTC()
}

func agentKickReason(err error) string {
	if errors.Is(err, ErrAgentForcedOffline) {
		return "forced offline"
	}
	return "agent session invalid"
}

func agentTerminalCode(err error) codes.Code {
	if errors.Is(err, ErrAgentForcedOffline) {
		return codes.PermissionDenied
	}
	return codes.Unauthenticated
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
