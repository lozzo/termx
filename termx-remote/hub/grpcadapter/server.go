package grpcadapter

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/lozzow/termx/termx-remote/hub/cloud"
	grpcapi "github.com/lozzow/termx/termx-remote/hub/grpcapi"
	hubice "github.com/lozzow/termx/termx-remote/hub/ice"
	"github.com/lozzow/termx/termx-remote/hub/registry"
	pb "github.com/lozzow/termx/termx-remote/protocol/hubgrpc"
	hubv1 "github.com/lozzow/termx/termx-remote/protocol/hubv1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type hubRegistryAdapter struct {
	registry   *registry.Registry
	cloud      *cloud.Service
	ice        *hubice.Service
	iceServers []hubv1.RTCIceServerConfig
	allowRelay bool
	sessionTTL time.Duration
	now        func() time.Time

	mu       sync.Mutex
	sessions map[string]agentSessionState
}

type agentSession struct {
	AgentID   string
	MachineID string
}

type agentSessionState struct {
	Session   agentSession
	ExpiresAt time.Time
}

type ServerConfig struct {
	Registry   *registry.Registry
	Cloud      *cloud.Service
	ICE        *hubice.Service
	ICEServers []hubv1.RTCIceServerConfig
	AllowRelay bool
	SessionTTL time.Duration
}

var answerProofChallengeGenerator = randomHubGRPCID

const hubLongPollTimeout = 30 * time.Second

func NewServer(reg *registry.Registry, cloudSvc *cloud.Service, iceServers []hubv1.RTCIceServerConfig) *grpc.Server {
	return NewServerWithConfig(ServerConfig{
		Registry:   reg,
		Cloud:      cloudSvc,
		ICEServers: iceServers,
	})
}

func NewServerWithConfig(cfg ServerConfig) *grpc.Server {
	sessionTTL := cfg.SessionTTL
	if sessionTTL <= 0 {
		sessionTTL = 5 * time.Minute
	}
	grpcSrv := grpc.NewServer(grpc.StreamInterceptor(grpcStreamAuth))
	pb.RegisterAgentHubServer(grpcSrv, grpcapi.NewServer(&hubRegistryAdapter{
		registry:   cfg.Registry,
		cloud:      cfg.Cloud,
		ice:        cfg.ICE,
		iceServers: cloneHubICEServers(cfg.ICEServers),
		allowRelay: cfg.AllowRelay,
		sessionTTL: sessionTTL,
		sessions:   make(map[string]agentSessionState),
	}))
	return grpcSrv
}

func grpcStreamAuth(srv interface{}, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	if _, err := grpcapi.ExtractBearerToken(ss.Context()); err != nil {
		return status.Error(codes.Unauthenticated, err.Error())
	}
	return handler(srv, ss)
}

func (a *hubRegistryAdapter) RegisterAgent(in grpcapi.RegisterAgentInput) (grpcapi.RegisterAgentOutput, error) {
	if a == nil || a.registry == nil {
		return grpcapi.RegisterAgentOutput{}, errors.New("registry is not configured")
	}
	agentID := strings.TrimSpace(in.AgentID)
	if agentID == "" {
		agentID = randomHubGRPCID("agent")
	}
	machineID := strings.TrimSpace(in.MachineID)
	if machineID == "" {
		machineID = strings.TrimSpace(in.DeviceID)
	}
	terminals := make([]registry.Terminal, 0, len(in.Terminals))
	for _, terminal := range in.Terminals {
		if strings.TrimSpace(terminal.TerminalID) == "" {
			continue
		}
		terminals = append(terminals, registry.Terminal{
			ID:    terminal.TerminalID,
			Name:  terminal.Name,
			State: "running",
		})
	}
	if _, err := a.registry.Register(context.Background(), registry.RegisterInput{
		MachineID: machineID,
		AgentID:   agentID,
		Terminals: terminals,
	}); err != nil {
		return grpcapi.RegisterAgentOutput{}, grpcAgentStateError(err)
	}
	sessionID := randomHubGRPCID("grpc_session")
	a.mu.Lock()
	a.cleanupExpiredLocked(a.nowUTC())
	if a.sessions == nil {
		a.sessions = make(map[string]agentSessionState)
	}
	a.sessions[sessionID] = agentSessionState{
		Session:   agentSession{AgentID: agentID, MachineID: machineID},
		ExpiresAt: a.nowUTC().Add(a.sessionTTLDuration()),
	}
	a.mu.Unlock()
	iceServers, allowRelay, err := a.registerAgentICEServers(context.Background(), machineID)
	if err != nil {
		return grpcapi.RegisterAgentOutput{}, err
	}
	return grpcapi.RegisterAgentOutput{
		SessionID:                sessionID,
		ICEServers:               grpcICEServersFromHub(iceServers),
		HeartbeatIntervalSeconds: 5,
		AllowRelay:               allowRelay,
	}, nil
}

func (a *hubRegistryAdapter) registerAgentICEServers(ctx context.Context, machineID string) ([]hubv1.RTCIceServerConfig, bool, error) {
	if a == nil {
		return nil, false, nil
	}
	if a.ice == nil {
		return cloneHubICEServers(a.iceServers), false, nil
	}
	rtc, err := a.ice.ConfigForLease(ctx, hubice.Lease{
		ID:         strings.TrimSpace(machineID),
		Path:       hubice.PathCloud,
		AllowRelay: a.allowRelay,
	})
	if err != nil {
		return nil, false, err
	}
	return hubICEServersFromICE(rtc.ICEServers), a.allowRelay, nil
}

func (a *hubRegistryAdapter) HeartbeatAgent(sessionID string, terminals []grpcapi.TerminalInput) error {
	session, ok := a.session(sessionID)
	if !ok {
		return grpcSessionInvalidError(registry.ErrAgentNotFound)
	}
	if a.registry == nil {
		return errors.New("registry is not configured")
	}
	regTerminals := make([]registry.Terminal, 0, len(terminals))
	for _, terminal := range terminals {
		if strings.TrimSpace(terminal.TerminalID) == "" {
			continue
		}
		regTerminals = append(regTerminals, registry.Terminal{ID: terminal.TerminalID, Name: terminal.Name, State: "running"})
	}
	if err := a.registry.Heartbeat(context.Background(), registry.HeartbeatInput{
		AgentID:   session.AgentID,
		MachineID: session.MachineID,
		Terminals: regTerminals,
	}); err != nil {
		if grpcAgentSessionTerminated(err) {
			a.dropSession(sessionID)
		}
		return grpcAgentStateError(err)
	}
	if !a.renewSession(sessionID) {
		return grpcSessionInvalidError(registry.ErrAgentNotFound)
	}
	return nil
}

func (a *hubRegistryAdapter) WaitForOffer(ctx context.Context, sessionID string) (*grpcapi.PendingOffer, error) {
	session, ok := a.session(sessionID)
	if !ok {
		return nil, grpcSessionInvalidError(registry.ErrAgentNotFound)
	}
	if a.cloud == nil {
		return nil, waitForHubLongPoll(ctx)
	}
	pollCtx, cancel := context.WithTimeout(ctx, hubLongPollTimeout)
	defer cancel()
	offer, err := a.cloud.PollAgentOffer(pollCtx, cloud.PollAgentOfferInput{
		AgentID:   session.AgentID,
		MachineID: session.MachineID,
		Timeout:   hubLongPollTimeout,
	})
	if err != nil {
		if grpcLongPollTimeoutError(err) {
			return nil, grpcapi.ErrPollTimeout
		}
		if grpcAgentSessionTerminated(err) {
			a.dropSession(sessionID)
		}
		return nil, grpcAgentStateError(err)
	}
	publicID := grpcPublicSessionID(offer)
	challenge := strings.TrimSpace(offer.AnswerProofChallenge)
	if challenge == "" {
		challenge = answerProofChallengeGenerator("answer_challenge")
	}
	return &grpcapi.PendingOffer{
		SessionID:            publicID,
		MachineID:            offer.MachineID,
		TerminalID:           offer.TerminalID,
		SDP:                  offer.SDP,
		SessionToken:         offer.SessionToken,
		AnswerProofChallenge: challenge,
		Candidates:           append([]string(nil), offer.ICECandidates...),
	}, nil
}

func (a *hubRegistryAdapter) SubmitAnswer(sessionID, answerSessionID, sdp string, candidates []string, answerProof string) error {
	session, ok := a.session(sessionID)
	if !ok {
		return grpcSessionInvalidError(registry.ErrAgentNotFound)
	}
	if a.cloud == nil {
		return errors.New("cloud service is not configured")
	}
	if err := a.cloud.SubmitAnswer(context.Background(), cloud.SubmitAnswerInput{
		AgentID:     session.AgentID,
		MachineID:   session.MachineID,
		OfferID:     answerSessionID,
		SDP:         sdp,
		AnswerProof: answerProof,
	}); err != nil {
		if grpcAgentSessionTerminated(err) {
			a.dropSession(sessionID)
		}
		return grpcAgentStateError(err)
	}
	return nil
}

func (a *hubRegistryAdapter) SubmitAnswerError(sessionID, answerSessionID, reason string) error {
	session, ok := a.session(sessionID)
	if !ok {
		return grpcSessionInvalidError(registry.ErrAgentNotFound)
	}
	if a.cloud == nil {
		return errors.New("cloud service is not configured")
	}
	if err := a.cloud.SubmitAnswer(context.Background(), cloud.SubmitAnswerInput{
		AgentID:   session.AgentID,
		MachineID: session.MachineID,
		OfferID:   answerSessionID,
		Error:     reason,
	}); err != nil {
		if grpcAgentSessionTerminated(err) {
			a.dropSession(sessionID)
		}
		return grpcAgentStateError(err)
	}
	return nil
}

func (a *hubRegistryAdapter) WaitForPairingClaim(ctx context.Context, sessionID string) (*grpcapi.PendingPairingClaim, error) {
	session, ok := a.session(sessionID)
	if !ok {
		return nil, grpcSessionInvalidError(registry.ErrAgentNotFound)
	}
	if a.registry == nil {
		return nil, errors.New("registry is not configured")
	}
	pollCtx, cancel := context.WithTimeout(ctx, hubLongPollTimeout)
	defer cancel()
	claim, err := a.registry.PollPairingClaim(pollCtx, registry.PairingPollInput{
		AgentID:   session.AgentID,
		MachineID: session.MachineID,
		Timeout:   hubLongPollTimeout,
	})
	if err != nil {
		if grpcLongPollTimeoutError(err) {
			return nil, grpcapi.ErrPollTimeout
		}
		if grpcAgentSessionTerminated(err) {
			a.dropSession(sessionID)
		}
		return nil, grpcAgentStateError(err)
	}
	return &grpcapi.PendingPairingClaim{
		ClaimID:               claim.ID,
		PairSessionID:         claim.PairSessionID,
		PairSecret:            claim.PairSecret,
		AppDeviceID:           claim.AppDeviceID,
		AppName:               claim.AppName,
		RequestedCapabilities: append([]string(nil), claim.RequestedCapabilities...),
		AllowedPaths:          append([]string(nil), claim.AllowedPaths...),
	}, nil
}

func (a *hubRegistryAdapter) SubmitPairingResult(in grpcapi.PairingResultInput) error {
	session, ok := a.session(in.SessionID)
	if !ok || a.registry == nil {
		return registry.ErrAgentNotFound
	}
	_, err := a.registry.SubmitPairingResult(context.Background(), registry.PairingResultInput{
		AgentID:      session.AgentID,
		MachineID:    session.MachineID,
		ClaimID:      in.ClaimID,
		MachineName:  in.MachineName,
		SessionToken: in.SessionToken,
		ExpiresAt:    in.ExpiresAt,
		Error:        in.Error,
	})
	if grpcAgentSessionTerminated(err) {
		a.dropSession(in.SessionID)
	}
	return grpcAgentStateError(err)
}

func (a *hubRegistryAdapter) session(sessionID string) (agentSession, bool) {
	if a == nil {
		return agentSession{}, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.nowUTC()
	a.cleanupExpiredLocked(now)
	state, ok := a.sessions[strings.TrimSpace(sessionID)]
	if !ok || !state.ExpiresAt.After(now) {
		return agentSession{}, false
	}
	return state.Session, true
}

func (a *hubRegistryAdapter) renewSession(sessionID string) bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.nowUTC()
	a.cleanupExpiredLocked(now)
	key := strings.TrimSpace(sessionID)
	state, ok := a.sessions[key]
	if !ok || !state.ExpiresAt.After(now) {
		return false
	}
	state.ExpiresAt = now.Add(a.sessionTTLDuration())
	a.sessions[key] = state
	return true
}

func (a *hubRegistryAdapter) dropSession(sessionID string) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.sessions, strings.TrimSpace(sessionID))
}

func (a *hubRegistryAdapter) cleanupExpiredLocked(now time.Time) int {
	removed := 0
	for sessionID, state := range a.sessions {
		if !state.ExpiresAt.After(now) {
			delete(a.sessions, sessionID)
			removed++
		}
	}
	return removed
}

func (a *hubRegistryAdapter) cleanupExpired() int {
	if a == nil {
		return 0
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cleanupExpiredLocked(a.nowUTC())
}

func (a *hubRegistryAdapter) nowUTC() time.Time {
	if a != nil && a.now != nil {
		if now := a.now().UTC(); !now.IsZero() {
			return now
		}
	}
	return time.Now().UTC()
}

func (a *hubRegistryAdapter) sessionTTLDuration() time.Duration {
	if a != nil && a.sessionTTL > 0 {
		return a.sessionTTL
	}
	return 2 * time.Minute
}

func waitForHubLongPoll(ctx context.Context) error {
	timer := time.NewTimer(hubLongPollTimeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return grpcapi.ErrPollTimeout
	}
}

func grpcLongPollTimeoutError(err error) bool {
	return errors.Is(err, registry.ErrPollTimeout) || errors.Is(err, context.DeadlineExceeded)
}

func grpcPublicSessionID(offer cloud.Offer) string {
	if strings.TrimSpace(offer.SessionID) != "" {
		return strings.TrimSpace(offer.SessionID)
	}
	return offer.ID
}

func grpcICEServersFromHub(in []hubv1.RTCIceServerConfig) []grpcapi.ICEServer {
	out := make([]grpcapi.ICEServer, 0, len(in))
	for _, server := range in {
		out = append(out, grpcapi.ICEServer{
			URLs:       append([]string(nil), server.URLs...),
			Username:   server.Username,
			Credential: server.Credential,
		})
	}
	return out
}

func hubICEServersFromICE(in []hubice.ICEServer) []hubv1.RTCIceServerConfig {
	out := make([]hubv1.RTCIceServerConfig, 0, len(in))
	for _, server := range in {
		if len(server.URLs) == 0 {
			continue
		}
		out = append(out, hubv1.RTCIceServerConfig{
			URLs:       append([]string(nil), server.URLs...),
			Username:   server.Username,
			Credential: server.Credential,
		})
	}
	return out
}

func cloneHubICEServers(in []hubv1.RTCIceServerConfig) []hubv1.RTCIceServerConfig {
	out := make([]hubv1.RTCIceServerConfig, 0, len(in))
	for _, server := range in {
		out = append(out, hubv1.RTCIceServerConfig{
			URLs:       append([]string(nil), server.URLs...),
			Username:   server.Username,
			Credential: server.Credential,
		})
	}
	return out
}

func randomHubGRPCID(prefix string) string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	}
	return prefix + "_" + base64.RawURLEncoding.EncodeToString(raw[:])
}

func grpcAgentStateError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, registry.ErrAgentForcedOffline):
		return fmt.Errorf("%w: %w", grpcapi.ErrAgentForcedOffline, err)
	case errors.Is(err, registry.ErrAgentNotFound), errors.Is(err, registry.ErrUnauthorizedAgent):
		return grpcSessionInvalidError(err)
	default:
		return err
	}
}

func grpcSessionInvalidError(err error) error {
	if err == nil {
		return grpcapi.ErrAgentSessionInvalid
	}
	return fmt.Errorf("%w: %w", grpcapi.ErrAgentSessionInvalid, err)
}

func grpcAgentSessionTerminated(err error) bool {
	return errors.Is(err, registry.ErrAgentForcedOffline) ||
		errors.Is(err, registry.ErrAgentNotFound) ||
		errors.Is(err, registry.ErrUnauthorizedAgent)
}
