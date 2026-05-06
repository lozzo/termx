package remote

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
	offerTTL   time.Duration
	claimTTL   time.Duration
	now        func() time.Time

	mu       sync.Mutex
	sessions map[string]hubGRPCSessionState
	offers   map[string]hubGRPCOfferState
	claims   map[string]hubGRPCClaimState
}

type hubGRPCSession struct {
	AgentID   string
	MachineID string
}

type hubGRPCSessionState struct {
	Session   hubGRPCSession
	ExpiresAt time.Time
}

type hubGRPCOfferState struct {
	OfferID   string
	ExpiresAt time.Time
}

type hubGRPCClaimState struct {
	Session   hubGRPCSession
	ExpiresAt time.Time
}

type HubGRPCServerConfig struct {
	Registry   *registry.Registry
	Cloud      *cloud.Service
	ICE        *hubice.Service
	ICEServers []hubv1.RTCIceServerConfig
	AllowRelay bool
}

func NewHubGRPCServer(reg *registry.Registry, cloudSvc *cloud.Service, iceServers []hubv1.RTCIceServerConfig) *grpc.Server {
	return NewHubGRPCServerWithConfig(HubGRPCServerConfig{
		Registry:   reg,
		Cloud:      cloudSvc,
		ICEServers: iceServers,
	})
}

func NewHubGRPCServerWithConfig(cfg HubGRPCServerConfig) *grpc.Server {
	grpcSrv := grpc.NewServer(grpc.StreamInterceptor(grpcStreamAuth))
	pb.RegisterAgentHubServer(grpcSrv, grpcapi.NewServer(&hubRegistryAdapter{
		registry:   cfg.Registry,
		cloud:      cfg.Cloud,
		ice:        cfg.ICE,
		iceServers: cloneHubICEServers(cfg.ICEServers),
		allowRelay: cfg.AllowRelay,
		sessions:   make(map[string]hubGRPCSessionState),
		offers:     make(map[string]hubGRPCOfferState),
		claims:     make(map[string]hubGRPCClaimState),
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
			State: "running",
		})
	}
	if _, err := a.registry.Register(context.Background(), registry.RegisterInput{
		MachineID: machineID,
		AgentID:   agentID,
		Terminals: terminals,
	}); err != nil {
		return grpcapi.RegisterAgentOutput{}, err
	}
	sessionID := randomHubGRPCID("grpc_session")
	a.mu.Lock()
	a.cleanupExpiredLocked(a.nowUTC())
	if a.sessions == nil {
		a.sessions = make(map[string]hubGRPCSessionState)
	}
	a.sessions[sessionID] = hubGRPCSessionState{
		Session:   hubGRPCSession{AgentID: agentID, MachineID: machineID},
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
	if !ok || a.registry == nil {
		return registry.ErrAgentNotFound
	}
	regTerminals := make([]registry.Terminal, 0, len(terminals))
	for _, terminal := range terminals {
		if strings.TrimSpace(terminal.TerminalID) == "" {
			continue
		}
		regTerminals = append(regTerminals, registry.Terminal{ID: terminal.TerminalID, State: "running"})
	}
	return a.registry.Heartbeat(context.Background(), registry.HeartbeatInput{
		AgentID:   session.AgentID,
		MachineID: session.MachineID,
		Terminals: regTerminals,
	})
}

func (a *hubRegistryAdapter) GetPendingOffer(ctx context.Context, sessionID string) (*grpcapi.PendingOffer, error) {
	session, ok := a.session(sessionID)
	if !ok || a.cloud == nil {
		return nil, registry.ErrAgentNotFound
	}
	offer, err := a.cloud.PollAgentOffer(ctx, cloud.PollAgentOfferInput{
		AgentID:   session.AgentID,
		MachineID: session.MachineID,
		Timeout:   200 * time.Millisecond,
	})
	if err != nil {
		if errors.Is(err, registry.ErrPollTimeout) {
			return nil, nil
		}
		return nil, err
	}
	publicID := grpcPublicSessionID(offer)
	a.mu.Lock()
	a.cleanupExpiredLocked(a.nowUTC())
	if a.offers == nil {
		a.offers = make(map[string]hubGRPCOfferState)
	}
	a.offers[sessionOfferKey(sessionID, publicID)] = hubGRPCOfferState{
		OfferID:   offer.ID,
		ExpiresAt: a.nowUTC().Add(a.offerTTLDuration()),
	}
	a.mu.Unlock()
	return &grpcapi.PendingOffer{
		SessionID:    publicID,
		MachineID:    offer.MachineID,
		TerminalID:   offer.TerminalID,
		SDP:          offer.SDP,
		SessionToken: offer.SessionToken,
		Candidates:   append([]string(nil), offer.ICECandidates...),
	}, nil
}

func (a *hubRegistryAdapter) SubmitAnswer(sessionID, answerSessionID, sdp string, candidates []string) error {
	session, ok := a.session(sessionID)
	if !ok || a.cloud == nil {
		return registry.ErrAgentNotFound
	}
	offerID := a.resolveOfferID(sessionID, answerSessionID)
	return a.cloud.SubmitAnswer(context.Background(), cloud.SubmitAnswerInput{
		AgentID:   session.AgentID,
		MachineID: session.MachineID,
		OfferID:   offerID,
		SDP:       sdp,
	})
}

func (a *hubRegistryAdapter) SubmitAnswerError(sessionID, answerSessionID, reason string) error {
	session, ok := a.session(sessionID)
	if !ok || a.cloud == nil {
		return registry.ErrAgentNotFound
	}
	offerID := a.resolveOfferID(sessionID, answerSessionID)
	return a.cloud.SubmitAnswer(context.Background(), cloud.SubmitAnswerInput{
		AgentID:   session.AgentID,
		MachineID: session.MachineID,
		OfferID:   offerID,
		Error:     reason,
	})
}

func (a *hubRegistryAdapter) GetPendingPairingClaim(ctx context.Context, sessionID string) (*grpcapi.PendingPairingClaim, error) {
	session, ok := a.session(sessionID)
	if !ok || a.registry == nil {
		return nil, registry.ErrAgentNotFound
	}
	claim, err := a.registry.PollPairingClaim(ctx, registry.PairingPollInput{
		AgentID:   session.AgentID,
		MachineID: session.MachineID,
		Timeout:   200 * time.Millisecond,
	})
	if err != nil {
		if errors.Is(err, registry.ErrPollTimeout) {
			return nil, nil
		}
		return nil, err
	}
	a.mu.Lock()
	a.cleanupExpiredLocked(a.nowUTC())
	if a.claims == nil {
		a.claims = make(map[string]hubGRPCClaimState)
	}
	a.claims[claim.ID] = hubGRPCClaimState{
		Session:   session,
		ExpiresAt: a.nowUTC().Add(a.claimTTLDuration()),
	}
	a.mu.Unlock()
	return &grpcapi.PendingPairingClaim{
		ClaimID:               claim.ID,
		PairSessionID:         claim.PairSessionID,
		PairSecret:            claim.PairSecret,
		AppDeviceID:           claim.AppDeviceID,
		AppName:               claim.AppName,
		RequestedCapabilities: append([]string(nil), claim.RequestedCapabilities...),
	}, nil
}

func (a *hubRegistryAdapter) SubmitPairingResult(in grpcapi.PairingResultInput) error {
	session, ok := a.claimSession(in.ClaimID)
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
	return err
}

func (a *hubRegistryAdapter) session(sessionID string) (hubGRPCSession, bool) {
	if a == nil {
		return hubGRPCSession{}, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.nowUTC()
	a.cleanupExpiredLocked(now)
	state, ok := a.sessions[strings.TrimSpace(sessionID)]
	if !ok || !state.ExpiresAt.After(now) {
		return hubGRPCSession{}, false
	}
	return state.Session, true
}

func (a *hubRegistryAdapter) claimSession(claimID string) (hubGRPCSession, bool) {
	if a == nil {
		return hubGRPCSession{}, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.nowUTC()
	a.cleanupExpiredLocked(now)
	state, ok := a.claims[strings.TrimSpace(claimID)]
	if !ok || !state.ExpiresAt.After(now) {
		return hubGRPCSession{}, false
	}
	return state.Session, true
}

func (a *hubRegistryAdapter) resolveOfferID(sessionID string, answerSessionID string) string {
	if a == nil {
		return strings.TrimSpace(answerSessionID)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.nowUTC()
	a.cleanupExpiredLocked(now)
	if state := a.offers[sessionOfferKey(sessionID, answerSessionID)]; state.ExpiresAt.After(now) && strings.TrimSpace(state.OfferID) != "" {
		return strings.TrimSpace(state.OfferID)
	}
	return strings.TrimSpace(answerSessionID)
}

func (a *hubRegistryAdapter) cleanupExpiredLocked(now time.Time) int {
	removed := 0
	for sessionID, state := range a.sessions {
		if !state.ExpiresAt.After(now) {
			delete(a.sessions, sessionID)
			removed++
		}
	}
	for key, state := range a.offers {
		if !state.ExpiresAt.After(now) {
			delete(a.offers, key)
			removed++
		}
	}
	for claimID, state := range a.claims {
		if !state.ExpiresAt.After(now) {
			delete(a.claims, claimID)
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

func (a *hubRegistryAdapter) offerTTLDuration() time.Duration {
	if a != nil && a.offerTTL > 0 {
		return a.offerTTL
	}
	return 5 * time.Minute
}

func (a *hubRegistryAdapter) claimTTLDuration() time.Duration {
	if a != nil && a.claimTTL > 0 {
		return a.claimTTL
	}
	return 5 * time.Minute
}

func sessionOfferKey(sessionID string, offerSessionID string) string {
	return strings.TrimSpace(sessionID) + "\x00" + strings.TrimSpace(offerSessionID)
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
