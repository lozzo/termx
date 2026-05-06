package remote

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

func grpcRegisterInput(agentID, machineID string) grpcapi.RegisterAgentInput {
	return grpcapi.RegisterAgentInput{
		AgentID:   agentID,
		DeviceID:  machineID,
		MachineID: machineID,
	}
}

func testNow() time.Time { return time.Now().UTC() }
