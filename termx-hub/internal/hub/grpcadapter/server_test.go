package grpcadapter

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-hub/internal/hub/cloud"
	grpcapi "github.com/lozzow/termx/termx-hub/internal/hub/grpcapi"
	"github.com/lozzow/termx/termx-hub/internal/hub/ice"
	"github.com/lozzow/termx/termx-hub/internal/hub/registry"
	hubv1 "github.com/lozzow/termx/termx-hub/internal/protocol/hubv1"
)

func TestGRPCAgentRegisterReceivesDynamicTURNLease(t *testing.T) {
	reg := registry.New(registry.Config{})
	iceSvc := ice.NewService(ice.Config{
		SharedSecret: "turn-secret",
		STUNURLs:     []string{"stun:hub.termx.test:3478"},
		TURNURLs:     []string{"turn:hub.termx.test:3478?transport=udp"},
	})
	adapter := &hubRegistryAdapter{
		registry:   reg,
		ice:        iceSvc,
		allowRelay: true,
	}

	out, err := adapter.RegisterAgent(grpcRegisterInput("agent-1", "machine-1"))
	if err != nil {
		t.Fatalf("RegisterAgent returned error: %v", err)
	}
	if !out.AllowRelay {
		t.Fatalf("AllowRelay = false, want true")
	}
	if len(out.ICEServers) != 2 {
		t.Fatalf("ICE servers = %+v, want STUN + TURN", out.ICEServers)
	}
	turn := out.ICEServers[1]
	if len(turn.URLs) != 1 || turn.URLs[0] != "turn:hub.termx.test:3478?transport=udp" || turn.Username == "" || turn.Credential == "" {
		t.Fatalf("TURN server missing credentials: %+v", turn)
	}
	if !iceSvc.VerifyCredential(turn.Username, turn.Credential, testNow(), "machine-1") {
		t.Fatalf("TURN credential does not verify for machine lease: %+v", turn)
	}
}

func TestGRPCAgentRegisterFallsBackToStaticICEServers(t *testing.T) {
	reg := registry.New(registry.Config{})
	adapter := &hubRegistryAdapter{
		registry:   reg,
		iceServers: []hubv1.RTCIceServerConfig{{URLs: []string{"stun:static.termx.test:3478"}}},
	}

	out, err := adapter.RegisterAgent(grpcRegisterInput("agent-1", "machine-1"))
	if err != nil {
		t.Fatalf("RegisterAgent returned error: %v", err)
	}
	if out.AllowRelay {
		t.Fatalf("AllowRelay = true, want false")
	}
	if len(out.ICEServers) != 1 || out.ICEServers[0].URLs[0] != "stun:static.termx.test:3478" {
		t.Fatalf("ICE servers = %+v", out.ICEServers)
	}
}

func TestGRPCAdapterSessionExpires(t *testing.T) {
	now := time.Date(2026, 5, 6, 10, 0, 0, 0, time.UTC)
	adapter := &hubRegistryAdapter{
		registry:   registry.New(registry.Config{}),
		sessionTTL: time.Second,
		now: func() time.Time {
			return now
		},
	}

	out, err := adapter.RegisterAgent(grpcRegisterInput("agent-1", "machine-1"))
	if err != nil {
		t.Fatalf("RegisterAgent returned error: %v", err)
	}
	if _, ok := adapter.session(out.SessionID); !ok {
		t.Fatal("registered session was not available before expiry")
	}

	now = now.Add(2 * time.Second)
	if _, ok := adapter.session(out.SessionID); ok {
		t.Fatal("expired session remained available")
	}
}

func TestGRPCAdapterHeartbeatRenewsSessionExpiry(t *testing.T) {
	now := time.Date(2026, 5, 6, 10, 0, 0, 0, time.UTC)
	reg := registry.New(registry.Config{AgentTTL: time.Minute})
	adapter := &hubRegistryAdapter{
		registry:   reg,
		sessionTTL: time.Second,
		now: func() time.Time {
			return now
		},
	}

	out, err := adapter.RegisterAgent(grpcRegisterInput("agent-1", "machine-1"))
	if err != nil {
		t.Fatalf("RegisterAgent returned error: %v", err)
	}

	now = now.Add(500 * time.Millisecond)
	if err := adapter.HeartbeatAgent(out.SessionID, []grpcapi.TerminalInput{{TerminalID: "terminal-1"}}); err != nil {
		t.Fatalf("HeartbeatAgent returned error: %v", err)
	}

	now = now.Add(750 * time.Millisecond)
	if _, ok := adapter.session(out.SessionID); !ok {
		t.Fatal("heartbeat did not renew adapter session expiry")
	}
}

func TestGRPCAdapterPreservesTerminalNamesInRegistry(t *testing.T) {
	reg := registry.New(registry.Config{AgentTTL: time.Minute})
	adapter := &hubRegistryAdapter{registry: reg}
	input := grpcRegisterInput("agent-1", "machine-1")
	input.Terminals = []grpcapi.TerminalInput{{TerminalID: "terminal-1", Name: "dev shell"}}

	out, err := adapter.RegisterAgent(input)
	if err != nil {
		t.Fatalf("RegisterAgent returned error: %v", err)
	}
	agent, ok := reg.GetAgent("agent-1")
	if !ok || len(agent.Terminals) != 1 || agent.Terminals[0].Name != "dev shell" {
		t.Fatalf("registry terminal after register = %+v ok=%v", agent.Terminals, ok)
	}

	if err := adapter.HeartbeatAgent(out.SessionID, []grpcapi.TerminalInput{{TerminalID: "terminal-2", Name: "worker"}}); err != nil {
		t.Fatalf("HeartbeatAgent returned error: %v", err)
	}
	agent, ok = reg.GetAgent("agent-1")
	if !ok || len(agent.Terminals) != 1 || agent.Terminals[0].Name != "worker" {
		t.Fatalf("registry terminal after heartbeat = %+v ok=%v", agent.Terminals, ok)
	}
}

func TestGRPCAdapterHeartbeatForcedOfflineDropsSession(t *testing.T) {
	reg := registry.New(registry.Config{AgentTTL: time.Minute})
	adapter := &hubRegistryAdapter{registry: reg}

	out, err := adapter.RegisterAgent(grpcRegisterInput("agent-1", "machine-1"))
	if err != nil {
		t.Fatalf("RegisterAgent returned error: %v", err)
	}
	reg.ForceOffline(registry.ForceOfflineInput{
		MachineID: "machine-1",
		AgentID:   "agent-1",
		Reason:    "kick",
		TTL:       time.Minute,
	})

	err = adapter.HeartbeatAgent(out.SessionID, nil)
	if !errors.Is(err, grpcapi.ErrAgentForcedOffline) {
		t.Fatalf("HeartbeatAgent error = %v, want ErrAgentForcedOffline", err)
	}
	if _, ok := adapter.session(out.SessionID); ok {
		t.Fatal("forced-offline heartbeat did not drop adapter session")
	}
}

func TestGRPCAdapterPublicSessionAnswerRoundTripWithoutOfferCache(t *testing.T) {
	ctx := context.Background()
	reg := registry.New(registry.Config{AgentTTL: time.Minute})
	if _, err := reg.Register(ctx, registry.RegisterInput{MachineID: "machine-1", AgentID: "agent-1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	cloudSvc := cloud.NewService(cloud.Config{Registry: reg})
	adapter := &hubRegistryAdapter{registry: reg, cloud: cloudSvc}
	out, err := adapter.RegisterAgent(grpcRegisterInput("agent-1", "machine-1"))
	if err != nil {
		t.Fatalf("RegisterAgent returned error: %v", err)
	}
	offer, err := cloudSvc.SubmitOffer(ctx, cloud.SubmitOfferInput{
		SessionID:  "public-session-1",
		MachineID:  "machine-1",
		TerminalID: "terminal-1",
		SDP:        minimalGRPCAdapterSDP("offer"),
	})
	if err != nil {
		t.Fatalf("submit cloud offer: %v", err)
	}
	pending, err := adapter.WaitForOffer(ctx, out.SessionID)
	if err != nil {
		t.Fatalf("WaitForOffer returned error: %v", err)
	}
	if pending.SessionID != "public-session-1" {
		t.Fatalf("pending public session id = %q", pending.SessionID)
	}
	if err := adapter.SubmitAnswer(out.SessionID, pending.SessionID, minimalGRPCAdapterSDP("answer"), nil, "proof-1"); err != nil {
		t.Fatalf("SubmitAnswer by public session id returned error: %v", err)
	}
	answer, err := cloudSvc.GetAnswer(ctx, cloud.GetAnswerInput{OfferID: offer.ID, MachineID: "machine-1"})
	if err != nil {
		t.Fatalf("GetAnswer returned error: %v", err)
	}
	if answer.OfferID != offer.ID || answer.SDP != minimalGRPCAdapterSDP("answer") || answer.AnswerProof != "proof-1" {
		t.Fatalf("answer = %+v", answer)
	}
}

func TestGRPCAdapterPairingResultUsesCurrentSession(t *testing.T) {
	ctx := context.Background()
	reg := registry.New(registry.Config{AgentTTL: time.Minute})
	if _, err := reg.Register(ctx, registry.RegisterInput{MachineID: "machine-1", AgentID: "agent-1"}); err != nil {
		t.Fatalf("register agent-1: %v", err)
	}
	if _, err := reg.Register(ctx, registry.RegisterInput{MachineID: "machine-1", AgentID: "agent-2"}); err != nil {
		t.Fatalf("register agent-2: %v", err)
	}
	adapter := &hubRegistryAdapter{registry: reg}
	first, err := adapter.RegisterAgent(grpcRegisterInput("agent-1", "machine-1"))
	if err != nil {
		t.Fatalf("RegisterAgent first returned error: %v", err)
	}
	second, err := adapter.RegisterAgent(grpcRegisterInput("agent-2", "machine-1"))
	if err != nil {
		t.Fatalf("RegisterAgent second returned error: %v", err)
	}
	claim, err := reg.SubmitPairingClaim(ctx, registry.PairingClaimInput{
		MachineID:             "machine-1",
		PairSessionID:         "pair-session-1",
		PairSecret:            "pair-secret-1",
		AppDeviceID:           "app-device-1",
		AppName:               "TermX App",
		RequestedCapabilities: []string{"terminal"},
	})
	if err != nil {
		t.Fatalf("SubmitPairingClaim returned error: %v", err)
	}
	pending, err := adapter.WaitForPairingClaim(ctx, first.SessionID)
	if err != nil {
		t.Fatalf("WaitForPairingClaim returned error: %v", err)
	}
	if pending.ClaimID != claim.ID {
		t.Fatalf("pending claim = %q, want %q", pending.ClaimID, claim.ID)
	}
	if err := adapter.SubmitPairingResult(grpcapi.PairingResultInput{
		SessionID:    second.SessionID,
		ClaimID:      claim.ID,
		SessionToken: "wrong-session-token",
	}); !errors.Is(err, grpcapi.ErrAgentSessionInvalid) {
		t.Fatalf("wrong session SubmitPairingResult err = %v, want ErrAgentSessionInvalid", err)
	}
	if err := adapter.SubmitPairingResult(grpcapi.PairingResultInput{
		SessionID:    first.SessionID,
		ClaimID:      claim.ID,
		SessionToken: "session-token-1",
	}); err != nil {
		t.Fatalf("SubmitPairingResult returned error: %v", err)
	}
	result, err := reg.GetPairingResult(ctx, claim.ID)
	if err != nil {
		t.Fatalf("GetPairingResult returned error: %v", err)
	}
	if result.SessionToken != "session-token-1" || result.MachineID != "machine-1" {
		t.Fatalf("pairing result = %+v", result)
	}
}

func TestGRPCAdapterCleanupRemovesExpiredSessions(t *testing.T) {
	now := time.Date(2026, 5, 6, 10, 0, 0, 0, time.UTC)
	adapter := &hubRegistryAdapter{
		now: func() time.Time {
			return now
		},
		sessions: map[string]agentSessionState{
			"expired-session": {Session: agentSession{AgentID: "agent-expired"}, ExpiresAt: now.Add(-time.Second)},
			"live-session":    {Session: agentSession{AgentID: "agent-live"}, ExpiresAt: now.Add(time.Minute)},
		},
	}

	if removed := adapter.cleanupExpired(); removed != 1 {
		t.Fatalf("cleanupExpired removed %d entries, want 1", removed)
	}
	if len(adapter.sessions) != 1 {
		t.Fatalf("unexpected session map size after cleanup: %d", len(adapter.sessions))
	}
	if _, ok := adapter.sessions["live-session"]; !ok {
		t.Fatal("live session was removed")
	}
}

func grpcRegisterInput(agentID, machineID string) grpcapi.RegisterAgentInput {
	return grpcapi.RegisterAgentInput{
		AgentID:   agentID,
		DeviceID:  machineID,
		MachineID: machineID,
	}
}

func testNow() time.Time { return time.Now().UTC() }

func minimalGRPCAdapterSDP(sessionID string) string {
	return "v=0\r\no=- " + sessionID + " 1 IN IP4 127.0.0.1\r\ns=-\r\nt=0 0\r\nm=application 9 UDP/DTLS/SCTP webrtc-datachannel"
}
