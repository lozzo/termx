package grpcadapter

import (
	"testing"
	"time"

	grpcapi "github.com/lozzow/termx/termx-remote/hub/grpcapi"
	"github.com/lozzow/termx/termx-remote/hub/ice"
	"github.com/lozzow/termx/termx-remote/hub/registry"
	hubv1 "github.com/lozzow/termx/termx-remote/protocol/hubv1"
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

func TestGRPCAdapterCleanupRemovesExpiredState(t *testing.T) {
	now := time.Date(2026, 5, 6, 10, 0, 0, 0, time.UTC)
	adapter := &hubRegistryAdapter{
		now: func() time.Time {
			return now
		},
		sessions: map[string]hubGRPCSessionState{
			"expired-session": {Session: hubGRPCSession{AgentID: "agent-expired"}, ExpiresAt: now.Add(-time.Second)},
			"live-session":    {Session: hubGRPCSession{AgentID: "agent-live"}, ExpiresAt: now.Add(time.Minute)},
		},
		offers: map[string]hubGRPCOfferState{
			"expired-offer": {OfferID: "offer-expired", ExpiresAt: now.Add(-time.Second)},
			"live-offer":    {OfferID: "offer-live", ExpiresAt: now.Add(time.Minute)},
		},
		claims: map[string]hubGRPCClaimState{
			"expired-claim": {Session: hubGRPCSession{AgentID: "agent-expired"}, ExpiresAt: now.Add(-time.Second)},
			"live-claim":    {Session: hubGRPCSession{AgentID: "agent-live"}, ExpiresAt: now.Add(time.Minute)},
		},
	}

	if removed := adapter.cleanupExpired(); removed != 3 {
		t.Fatalf("cleanupExpired removed %d entries, want 3", removed)
	}
	if len(adapter.sessions) != 1 || len(adapter.offers) != 1 || len(adapter.claims) != 1 {
		t.Fatalf("unexpected map sizes after cleanup: sessions=%d offers=%d claims=%d", len(adapter.sessions), len(adapter.offers), len(adapter.claims))
	}
	if _, ok := adapter.sessions["live-session"]; !ok {
		t.Fatal("live session was removed")
	}
	if _, ok := adapter.offers["live-offer"]; !ok {
		t.Fatal("live offer was removed")
	}
	if _, ok := adapter.claims["live-claim"]; !ok {
		t.Fatal("live claim was removed")
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
