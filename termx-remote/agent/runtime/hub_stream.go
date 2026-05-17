package runtime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	goruntime "runtime"
	"strings"
	"sync"
	"time"

	"github.com/lozzow/termx/termx-remote/discovery"
	"github.com/lozzow/termx/termx-remote/identity"
	"github.com/lozzow/termx/termx-remote/pairing"
	pb "github.com/lozzow/termx/termx-remote/protocol/hubgrpc"
	hubv1 "github.com/lozzow/termx/termx-remote/protocol/hubv1"
	remotertc "github.com/lozzow/termx/termx-remote/session/rtc"
	"github.com/lozzow/termx/termx-remote/session/token"
)

const (
	offerReplayWindow     = 60 * time.Second
	sessionTokenPathHub   = "hub"
	sessionTokenPathLocal = "local"
)

func randomAgentID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("agent-%d", time.Now().UnixNano())
	}
	return "agent-" + hex.EncodeToString(raw[:])
}

func (m *Manager) buildGRPCTerminals() []*pb.Terminal {
	var terminals []TerminalInventoryItem
	if m.provider != nil {
		terminals = m.provider.ListRemoteTerminals(context.Background())
	}
	out := make([]*pb.Terminal, 0, len(terminals))
	for _, terminal := range terminals {
		out = append(out, &pb.Terminal{
			TerminalId:    terminal.ID,
			Name:          terminal.Name,
			RemoteEnabled: true,
		})
	}
	return out
}

func (m *Manager) buildGRPCRegisterRequest() *pb.RegisterRequest {
	m.mu.Lock()
	if strings.TrimSpace(m.agentID) == "" {
		m.agentID = randomAgentID()
	}
	agentID := m.agentID
	deviceID := m.identity.DeviceID
	displayName := firstNonEmpty(m.identity.DisplayName, m.cfg.DeviceName)
	m.mu.Unlock()
	hostname, _ := os.Hostname()
	return &pb.RegisterRequest{
		AgentId:     agentID,
		DeviceId:    deviceID,
		MachineId:   deviceID,
		DisplayName: displayName,
		Hostname:    hostname,
		Platform:    fmt.Sprintf("%s/%s", goruntime.GOOS, goruntime.GOARCH),
		Version:     AgentVersion,
		Terminals:   m.buildGRPCTerminals(),
	}
}

func (m *Manager) ensureGRPCHubLoop(ctx context.Context, hubURL string) error {
	m.mu.Lock()
	state := m.hubStateLocked(hubURL)
	if state.ForcedOffline {
		reason := state.ForcedReason
		m.mu.Unlock()
		return forcedOfflineError(reason)
	}
	if state.SessionCancel != nil {
		m.mu.Unlock()
		return nil
	}
	loopCtx, cancel := context.WithCancel(ctx)
	state.SessionContext = loopCtx
	state.SessionCancel = cancel
	state.ConnectionState = HubConnectionConnecting
	state.LastError = ""
	m.mu.Unlock()
	go m.runGRPCHubLoop(loopCtx, hubURL)
	return nil
}

func (m *Manager) runGRPCHubLoop(ctx context.Context, hubURL string) {
	backoff := time.Second
	for ctx.Err() == nil {
		err := m.connectAndServeGRPC(ctx, hubURL)
		m.cleanupHubOfferNonces(hubURL)
		if err != nil && ctx.Err() == nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff *= 2
			if backoff > 60*time.Second {
				backoff = 60 * time.Second
			}
			continue
		}
		backoff = time.Second
	}
}

func (m *Manager) connectAndServeGRPC(ctx context.Context, hubURL string) error {
	client, err := discovery.NewGRPCHubClient(hubURL, m.grpcAccessTokenForHub(hubURL))
	if err != nil {
		m.markHubConnectionError(hubURL, err)
		return fmt.Errorf("create grpc client: %w", err)
	}
	defer client.Close()

	stream, err := client.Connect(ctx)
	if err != nil {
		m.markHubConnectionError(hubURL, err)
		return fmt.Errorf("connect: %w", err)
	}
	sender := &lockedGRPCClientSender{stream: stream}
	if err := sender.Send(&pb.AgentToHub{Payload: &pb.AgentToHub_Register{
		Register: m.buildGRPCRegisterRequest(),
	}}); err != nil {
		m.markHubConnectionError(hubURL, err)
		return err
	}
	msg, err := stream.Recv()
	if err != nil {
		m.markHubConnectionError(hubURL, err)
		return err
	}
	ack := msg.GetRegisterAck()
	if ack == nil {
		m.markHubConnectionError(hubURL, fmt.Errorf("expected register_ack"))
		return fmt.Errorf("expected register_ack")
	}
	m.updateFromGRPCRegisterAck(hubURL, ack)
	m.TriggerSync()

	interval := time.Duration(ack.GetHeartbeatIntervalSeconds()) * time.Second
	if interval <= 0 {
		interval = 30 * time.Second
	}
	heartbeatCtx, heartbeatCancel := context.WithCancel(ctx)
	defer heartbeatCancel()
	heartbeatErrCh := make(chan error, 1)
	go m.grpcHeartbeatLoop(heartbeatCtx, sender, ack.GetAgentSessionId(), interval, heartbeatErrCh)

	recvCh := make(chan recvResult, 1)
	go func() {
		for {
			msg, err := stream.Recv()
			select {
			case recvCh <- recvResult{msg: msg, err: err}:
			case <-heartbeatCtx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()

	for {
		var msg *pb.HubToAgent
		var err error
		select {
		case hbErr := <-heartbeatErrCh:
			m.markHubConnectionError(hubURL, hbErr)
			m.TriggerSync()
			return hbErr
		case r := <-recvCh:
			msg, err = r.msg, r.err
		}
		if err != nil {
			m.markHubConnectionError(hubURL, err)
			m.TriggerSync()
			return err
		}
		switch p := msg.GetPayload().(type) {
		case *pb.HubToAgent_SignalingOffer:
			m.mu.Lock()
			state := m.hubStates[strings.TrimSpace(hubURL)]
			var offerSem chan struct{}
			if state != nil {
				offerSem = state.offerSem
			}
			m.mu.Unlock()
			if offerSem == nil {
				go m.handleGRPCOffer(ctx, hubURL, p.SignalingOffer, sender)
			} else {
				select {
				case offerSem <- struct{}{}:
					pbOffer := p.SignalingOffer
					go func() {
						defer func() { <-offerSem }()
						m.handleGRPCOffer(ctx, hubURL, pbOffer, sender)
					}()
				default:
					// offer semaphore full; drop offer to prevent goroutine exhaustion
				}
			}
		case *pb.HubToAgent_PairingClaim:
			go m.handleGRPCPairingClaim(ctx, p.PairingClaim, sender)
		case *pb.HubToAgent_Kick:
			return fmt.Errorf("kicked: %s", p.Kick.GetReason())
		}
	}
}

func (m *Manager) grpcAccessTokenForHub(hubURL string) string {
	m.mu.RLock()
	accessToken := strings.TrimSpace(m.cfg.AccessToken)
	kind := HubKind("")
	if state := m.hubStates[strings.TrimSpace(hubURL)]; state != nil {
		kind = state.Kind
	}
	m.mu.RUnlock()
	if accessToken != "" {
		return accessToken
	}
	if kind == HubKindLocal {
		return "local"
	}
	return ""
}

func (m *Manager) stopHubSignalingLoopLocked() {
	for _, state := range m.hubStates {
		if state == nil {
			continue
		}
		if state.SessionCancel != nil {
			state.SessionCancel()
		}
		state.SessionID = ""
		state.RTCServers = nil
		state.RelayPolicy = hubv1.RelayPolicy{}
		state.SessionContext = nil
		state.SessionCancel = nil
		state.ConnectionState = HubConnectionDisconnected
		state.ConnectedAt = time.Time{}
	}
}

func (m *Manager) stopRemovedHubSignalingLocked(keep []string) {
	keepSet := make(map[string]struct{}, len(keep))
	for _, hubURL := range keep {
		if hubURL = strings.TrimSpace(hubURL); hubURL != "" {
			keepSet[hubURL] = struct{}{}
		}
	}
	for hubURL, state := range m.hubStates {
		if _, ok := keepSet[hubURL]; ok {
			continue
		}
		if state != nil && state.SessionCancel != nil {
			state.SessionCancel()
		}
		delete(m.hubStates, hubURL)
	}
	if len(keepSet) == 0 {
		m.stopHubSignalingLoopLocked()
		return
	}
	if _, ok := keepSet[strings.TrimSpace(m.cfg.HubURL)]; !ok {
		m.stopHubSignalingLoopLocked()
	}
}

type recvResult struct {
	msg *pb.HubToAgent
	err error
}

type lockedGRPCClientSender struct {
	mu     sync.Mutex
	stream pb.AgentHub_ConnectClient
}

func (s *lockedGRPCClientSender) Send(msg *pb.AgentToHub) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stream.Send(msg)
}

func (m *Manager) grpcHeartbeatLoop(ctx context.Context, sender interface{ Send(*pb.AgentToHub) error }, sessionID string, interval time.Duration, errCh chan<- error) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := sender.Send(&pb.AgentToHub{Payload: &pb.AgentToHub_Heartbeat{
				Heartbeat: &pb.HeartbeatRequest{
					AgentSessionId: sessionID,
					Terminals:      m.buildGRPCTerminals(),
				},
			}}); err != nil {
				select {
				case errCh <- err:
				default:
				}
				return
			}
		}
	}
}

func (m *Manager) updateFromGRPCRegisterAck(hubURL string, ack *pb.RegisterResponse) {
	m.mu.Lock()
	state := m.hubStateLocked(hubURL)
	state.SessionID = ack.GetAgentSessionId()
	state.RTCServers = grpcIceServersToHub(ack.GetIceServers())
	if policy := ack.GetRelayPolicy(); policy != nil {
		state.RelayPolicy = hubv1.RelayPolicy{
			AllowRelay:         policy.GetAllowRelay(),
			AllowRelayTransfer: policy.GetAllowRelayTransfer(),
		}
	} else {
		state.RelayPolicy = hubv1.RelayPolicy{}
	}
	now := time.Now().UTC()
	state.ConnectionState = HubConnectionConnected
	if state.ConnectedAt.IsZero() {
		state.ConnectedAt = now
	}
	state.LastAckAt = now
	state.LastError = ""
	m.mu.Unlock()
}

func (m *Manager) markHubConnectionError(hubURL string, err error) {
	if err == nil {
		return
	}
	m.mu.Lock()
	state := m.hubStateLocked(hubURL)
	if state.ConnectionState == HubConnectionConnected {
		state.ConnectionState = HubConnectionReconnecting
	} else if state.ConnectionState != HubConnectionForcedOffline {
		state.ConnectionState = HubConnectionConnecting
	}
	state.LastError = err.Error()
	m.mu.Unlock()
}

func grpcIceServersToHub(in []*pb.RTCIceServer) []hubv1.RTCIceServerConfig {
	out := make([]hubv1.RTCIceServerConfig, 0, len(in))
	for _, server := range in {
		if server == nil {
			continue
		}
		out = append(out, hubv1.RTCIceServerConfig{
			URLs:       append([]string(nil), server.GetUrls()...),
			Username:   server.GetUsername(),
			Credential: server.GetCredential(),
		})
	}
	return out
}

func (m *Manager) handleGRPCOffer(ctx context.Context, hubURL string, pbOffer *pb.SignalingOffer, sender interface{ Send(*pb.AgentToHub) error }) {
	if pbOffer == nil {
		return
	}
	offer := hubv1.SignalingOffer{
		SessionID:            pbOffer.GetSessionId(),
		MachineID:            pbOffer.GetMachineId(),
		TerminalID:           pbOffer.GetTerminalId(),
		Path:                 m.offerSessionPath(hubURL, pbOffer.GetPath()),
		SDP:                  pbOffer.GetSdp(),
		Candidates:           append([]string(nil), pbOffer.GetIceCandidates()...),
		SessionToken:         pbOffer.GetSessionToken(),
		AnswerProofChallenge: pbOffer.GetAnswerProofChallenge(),
	}
	answer := hubv1.SignalingAnswer{SessionID: offer.SessionID}
	err := m.verifyGRPCOfferReplay(hubURL, pbOffer.GetNonce(), pbOffer.GetIssuedAt())
	if err == nil {
		answer, err = m.answerOffer(ctx, hubURL, offer)
	}
	if err != nil {
		answer = hubv1.SignalingAnswer{SessionID: offer.SessionID, Error: err.Error()}
	}
	_ = sender.Send(&pb.AgentToHub{Payload: &pb.AgentToHub_SignalingAnswer{
		SignalingAnswer: &pb.SignalingAnswer{
			SessionId:     answer.SessionID,
			Sdp:           answer.SDP,
			IceCandidates: append([]string(nil), answer.ICECandidates...),
			Error:         answer.Error,
			AnswerProof:   answer.AnswerProof,
		},
	}})
}

func (m *Manager) offerSessionPath(hubURL string, explicit string) string {
	switch strings.TrimSpace(explicit) {
	case sessionTokenPathLocal:
		return sessionTokenPathLocal
	case sessionTokenPathHub:
		return sessionTokenPathHub
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	state := m.hubStates[strings.TrimSpace(hubURL)]
	if state != nil && state.Kind == HubKindLocal {
		return sessionTokenPathLocal
	}
	return sessionTokenPathHub
}

func (m *Manager) handleGRPCPairingClaim(ctx context.Context, claim *pb.PairingClaim, sender interface{ Send(*pb.AgentToHub) error }) {
	if claim == nil {
		return
	}
	result := m.claimHubPairing(ctx, hubv1.PairingClaim{
		ClaimID:               claim.GetClaimId(),
		MachineID:             m.identity.DeviceID,
		PairSessionID:         claim.GetPairSessionId(),
		PairSecret:            claim.GetPairSecret(),
		AppDeviceID:           claim.GetAppDeviceId(),
		AppName:               claim.GetAppName(),
		RequestedCapabilities: append([]string(nil), claim.GetRequestedCapabilities()...),
		AllowedPaths:          append([]string(nil), claim.GetAllowedPaths()...),
	})
	_ = sender.Send(&pb.AgentToHub{Payload: &pb.AgentToHub_PairingResult{
		PairingResult: &pb.PairingResult{
			ClaimId:      result.ClaimID,
			SessionToken: result.SessionToken,
			ExpiresAt:    result.ExpiresAt,
			MachineId:    result.MachineID,
			MachineName:  result.MachineName,
			Error:        result.Error,
		},
	}})
}

func (m *Manager) verifyGRPCOfferReplay(hubURL, nonce string, issuedAt int64) error {
	nonce = strings.TrimSpace(nonce)
	if nonce == "" {
		return fmt.Errorf("offer nonce is required")
	}
	if issuedAt <= 0 {
		return fmt.Errorf("offer issued_at is required")
	}
	now := time.Now().UTC()
	issued := time.Unix(issuedAt, 0).UTC()
	if issued.Before(now.Add(-offerReplayWindow)) || issued.After(now.Add(offerReplayWindow)) {
		return fmt.Errorf("offer issued_at is outside replay window")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.hubStateLocked(hubURL)
	m.pruneHubOfferNoncesLocked(state, now)
	if state.seenNonces == nil {
		state.seenNonces = make(map[string]time.Time)
	}
	if _, ok := state.seenNonces[nonce]; ok {
		return fmt.Errorf("offer nonce replay detected")
	}
	state.seenNonces[nonce] = issued.Add(offerReplayWindow)
	return nil
}

func (m *Manager) cleanupHubOfferNonces(hubURL string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	hubURL = strings.TrimSpace(hubURL)
	state := m.hubStates[hubURL]
	if state == nil {
		return
	}
	m.pruneHubOfferNoncesLocked(state, time.Now().UTC())
}

func (m *Manager) pruneHubOfferNoncesLocked(state *hubRuntimeState, now time.Time) {
	if state == nil || len(state.seenNonces) == 0 {
		return
	}
	for nonce, expiresAt := range state.seenNonces {
		if !expiresAt.After(now) {
			delete(state.seenNonces, nonce)
		}
	}
	if len(state.seenNonces) == 0 {
		state.seenNonces = nil
	}
}

func (m *Manager) answerOffer(ctx context.Context, hubURL string, offer hubv1.SignalingOffer) (hubv1.SignalingAnswer, error) {
	m.mu.RLock()
	iceServers, answerOptions, sessionCtx := m.hubAnswerSnapshotLocked(hubURL)
	m.mu.RUnlock()
	answerOptions.SessionContext = combineSessionContext(sessionCtx, answerOptions.SessionContext)
	return m.answerCloudOfferWithOptions(ctx, offer, iceServers, answerOptions), nil
}

func (m *Manager) hubAnswerSnapshotLocked(hubURL string) ([]hubv1.RTCIceServerConfig, remotertc.AnswerOptions, context.Context) {
	state := m.hubStates[strings.TrimSpace(hubURL)]
	if state == nil {
		return nil, remotertc.AnswerOptions{}, nil
	}
	return cloneRTCIceServers(state.RTCServers), state.AnswerOptions, state.SessionContext
}

func cloneRTCIceServers(in []hubv1.RTCIceServerConfig) []hubv1.RTCIceServerConfig {
	if len(in) == 0 {
		return nil
	}
	out := make([]hubv1.RTCIceServerConfig, 0, len(in))
	for _, server := range in {
		server.URLs = append([]string(nil), server.URLs...)
		out = append(out, server)
	}
	return out
}

func (m *Manager) claimHubPairing(ctx context.Context, claim hubv1.PairingClaim) hubv1.PairingResult {
	result := hubv1.PairingResult{
		ClaimID:   claim.ClaimID,
		MachineID: claim.MachineID,
	}
	m.mu.RLock()
	pairing := m.pairing
	m.mu.RUnlock()
	if pairing == nil {
		result.Error = "remote pairing is not available"
		return result
	}
	out, err := pairing.ClaimPairSession(ctx, pairingpkgClaimRequest(claim))
	if err != nil {
		result.Error = err.Error()
		return result
	}
	if strings.TrimSpace(out.MachineID) != strings.TrimSpace(claim.MachineID) {
		result.Error = fmt.Sprintf("pairing response machine mismatch: %s != %s", out.MachineID, claim.MachineID)
		return result
	}
	result.MachineName = out.MachineName
	result.SessionToken = out.SessionToken
	result.ExpiresAt = out.ExpiresAt.Format(time.RFC3339)
	return result
}

func pairingpkgClaimRequest(claim hubv1.PairingClaim) pairing.ClaimRequest {
	return pairing.ClaimRequest{
		PairSessionID:         claim.PairSessionID,
		PairSecret:            claim.PairSecret,
		AppDeviceID:           claim.AppDeviceID,
		AppName:               claim.AppName,
		RequestedCapabilities: append([]string(nil), claim.RequestedCapabilities...),
		AllowedPaths:          append([]string(nil), claim.AllowedPaths...),
	}
}

func (m *Manager) answerCloudOffer(ctx context.Context, offer hubv1.SignalingOffer, iceServers []hubv1.RTCIceServerConfig) hubv1.SignalingAnswer {
	m.mu.RLock()
	answerOptions := m.answerOptions
	m.mu.RUnlock()
	return m.answerCloudOfferWithOptions(ctx, offer, iceServers, answerOptions)
}

func (m *Manager) answerCloudOfferWithOptions(ctx context.Context, offer hubv1.SignalingOffer, iceServers []hubv1.RTCIceServerConfig, answerOptions remotertc.AnswerOptions) hubv1.SignalingAnswer {
	claims, err := m.verifyOfferSession(ctx, offer)
	if err != nil {
		return hubv1.SignalingAnswer{
			SessionID: offer.SessionID,
			Error:     err.Error(),
		}
	}
	terminalManagement := m.terminalManagementRouter()
	answerer := m.answerer
	if answerer == nil {
		answerer = defaultCloudOfferAnswerer{}
	}
	policy := cloudOfferChannelPolicy(offer, claims.Capabilities, terminalManagement)
	offerICEServers := cloneHubICEServers(iceServers)
	answerOptions.ChannelPolicy = policy
	answerOptions.TerminalManagement = terminalManagement
	answerOptions.Storage = m.storageRouter()
	answerOptions.Events = m.eventRouter()
	answer, err := answerer.AnswerOffer(ctx, offer, offerICEServers, m.host, m.files, answerOptions)
	if err != nil {
		return hubv1.SignalingAnswer{
			SessionID: offer.SessionID,
			Error:     err.Error(),
		}
	}
	answer.AnswerProof = m.answerProof(offer, claims)
	return answer
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

func (m *Manager) verifyOfferSession(ctx context.Context, offer hubv1.SignalingOffer) (token.Claims, error) {
	if m == nil {
		return token.Claims{}, fmt.Errorf("remote manager is nil")
	}
	claims, err := m.verifySessionToken(m.identity.DeviceID, offer.SessionToken)
	if err != nil {
		return token.Claims{}, err
	}
	path := normalizeOfferSessionPath(offer.Path)
	if !sessionTokenAllowsPath(claims, path) {
		return token.Claims{}, fmt.Errorf("session token is not authorized for %s connections", path)
	}
	terminalID := strings.TrimSpace(offer.TerminalID)
	if terminalID != "" {
		if !m.hasTerminal(ctx, terminalID) {
			return token.Claims{}, fmt.Errorf("terminal %q is not available for remote access", terminalID)
		}
	}
	return claims, nil
}

func normalizeOfferSessionPath(path string) string {
	switch strings.TrimSpace(path) {
	case sessionTokenPathLocal:
		return sessionTokenPathLocal
	default:
		return sessionTokenPathHub
	}
}

func sessionTokenAllowsPath(claims token.Claims, path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	if len(claims.Paths) == 0 {
		return true
	}
	for _, allowed := range claims.Paths {
		if strings.TrimSpace(allowed) == path {
			return true
		}
	}
	return false
}

func (m *Manager) answerProof(offer hubv1.SignalingOffer, claims token.Claims) string {
	challenge := strings.TrimSpace(offer.AnswerProofChallenge)
	if challenge == "" {
		return ""
	}
	secret, err := m.machineSecretBytes()
	if err != nil {
		return ""
	}
	proof, err := token.ComputeAnswerProof(secret, claims, offer.SessionID, challenge)
	if err != nil {
		return ""
	}
	return proof
}

func (m *Manager) verifySessionToken(machineID, sessionToken string) (token.Claims, error) {
	if strings.TrimSpace(sessionToken) == "" {
		return token.Claims{}, fmt.Errorf("session_token is required")
	}
	secret, err := m.machineSecretBytes()
	if err != nil {
		return token.Claims{}, fmt.Errorf("load machine secret: %w", err)
	}
	claims, err := token.Verify(sessionToken, secret, time.Now().UTC())
	if err != nil {
		return token.Claims{}, fmt.Errorf("invalid session token: %w", err)
	}
	if claims.MachineID != machineID {
		return token.Claims{}, fmt.Errorf("session token machine_id mismatch: got %q want %q", claims.MachineID, machineID)
	}
	return claims, nil
}

func (m *Manager) machineSecretBytes() ([]byte, error) {
	if m == nil {
		return nil, fmt.Errorf("remote manager is nil")
	}
	m.mu.RLock()
	if len(m.machineSecret) > 0 {
		secret := append([]byte(nil), m.machineSecret...)
		m.mu.RUnlock()
		return secret, nil
	}
	dataDir := m.cfg.DataDir
	m.mu.RUnlock()

	secret, err := identity.LoadOrCreateMachineSecret(dataDir)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	if len(m.machineSecret) == 0 {
		m.machineSecret = append([]byte(nil), secret...)
	}
	secret = append([]byte(nil), m.machineSecret...)
	m.mu.Unlock()
	return secret, nil
}

func (m *Manager) hasTerminal(ctx context.Context, terminalID string) bool {
	if m == nil || m.provider == nil {
		return false
	}
	for _, terminal := range m.provider.ListRemoteTerminals(ctx) {
		if strings.TrimSpace(terminal.ID) == terminalID {
			return true
		}
	}
	return false
}

func cloudOfferChannelPolicy(offer hubv1.SignalingOffer, _ []string, terminalManagement remotertc.TerminalManagementRouter) remotertc.ChannelPolicy {
	terminalID := strings.TrimSpace(offer.TerminalID)
	return remotertc.ChannelPolicy{
		TerminalID:              terminalID,
		AllowTerminal:           true,
		AllowAPI:                true,
		AllowFileManager:        true,
		AllowTerminalInventory:  terminalManagement != nil,
		AllowTerminalManagement: terminalManagement != nil,
		AllowEvents:             true,
		AllowRelayTransfer:      true,
	}
}

func (m *Manager) terminalManagementRouter() remotertc.TerminalManagementRouter {
	if m == nil {
		return nil
	}
	if router, ok := m.host.(remotertc.TerminalManagementRouter); ok {
		return router
	}
	if router, ok := m.provider.(remotertc.TerminalManagementRouter); ok {
		return router
	}
	return nil
}

func (m *Manager) storageRouter() remotertc.StorageRouter {
	if m == nil {
		return nil
	}
	if router, ok := m.host.(remotertc.StorageRouter); ok {
		return router
	}
	if router, ok := m.provider.(remotertc.StorageRouter); ok {
		return router
	}
	return nil
}

func (m *Manager) eventRouter() remotertc.EventRouter {
	if m == nil {
		return nil
	}
	if router, ok := m.host.(remotertc.EventRouter); ok {
		return router
	}
	if router, ok := m.provider.(remotertc.EventRouter); ok {
		return router
	}
	return nil
}

func (m *Manager) resetHubSession(hubURL string) {
	m.mu.Lock()
	var cancels []context.CancelFunc
	if hubURL = strings.TrimSpace(hubURL); hubURL != "" {
		state := m.hubStateLocked(hubURL)
		if state.SessionCancel != nil {
			cancels = append(cancels, state.SessionCancel)
		}
		state.SessionID = ""
		state.RTCServers = nil
		state.RelayPolicy = hubv1.RelayPolicy{}
		state.SessionContext = nil
		state.SessionCancel = nil
		state.ConnectionState = HubConnectionDisconnected
		state.ConnectedAt = time.Time{}
	} else {
		for _, state := range m.hubStates {
			if state == nil {
				continue
			}
			if state.SessionCancel != nil {
				cancels = append(cancels, state.SessionCancel)
			}
			state.SessionID = ""
			state.RTCServers = nil
			state.RelayPolicy = hubv1.RelayPolicy{}
			state.SessionContext = nil
			state.SessionCancel = nil
			state.ConnectionState = HubConnectionDisconnected
			state.ConnectedAt = time.Time{}
		}
	}
	m.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

func (m *Manager) forceHubOffline(hubURL string, cause error) {
	reason := "hub forced offline"
	if cause != nil {
		reason = cause.Error()
	}
	m.mu.Lock()
	var cancels []context.CancelFunc
	if hubURL = strings.TrimSpace(hubURL); hubURL != "" {
		state := m.hubStateLocked(hubURL)
		if state.SessionCancel != nil {
			cancels = append(cancels, state.SessionCancel)
		}
		state.SessionID = ""
		state.RTCServers = nil
		state.RelayPolicy = hubv1.RelayPolicy{}
		state.SessionContext = nil
		state.SessionCancel = nil
		state.ConnectionState = HubConnectionForcedOffline
		state.ConnectedAt = time.Time{}
		state.ForcedOffline = true
		state.ForcedOfflineAt = time.Now().UTC()
		state.ForcedReason = reason
	}
	m.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	m.setStatus(StateDegraded, reason)
}

func forcedOfflineError(reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return errHubForcedOffline
	}
	return fmt.Errorf("%w: %s", errHubForcedOffline, reason)
}

func combineSessionContext(hubCtx, optionCtx context.Context) context.Context {
	switch {
	case hubCtx == nil:
		return optionCtx
	case optionCtx == nil:
		return hubCtx
	}
	combined, cancel := context.WithCancel(hubCtx)
	go func() {
		select {
		case <-optionCtx.Done():
			cancel()
		case <-combined.Done():
		}
	}()
	return combined
}
