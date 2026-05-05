package runtime

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	remoteconfig "github.com/lozzow/termx/termx-remote/config"
	"github.com/lozzow/termx/termx-remote/identity"
	hubv1 "github.com/lozzow/termx/termx-remote/protocol/hubv1"
)

func TestManagerHubRegistrationIncludesMachineSignedProof(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	machineKey, err := identity.LoadOrCreateMachineKey(dataDir)
	if err != nil {
		t.Fatalf("load machine key: %v", err)
	}
	var got hubv1.HubRegisterRequest
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/agents/register" {
			t.Fatalf("unexpected hub path %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode hub register: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"version":                    "remote.hub.v1",
			"hub_id":                     "hub-test",
			"agent_session_id":           "agent-session-1",
			"heartbeat_interval_seconds": 15,
			"rtc_config":                 map[string]any{"ice_servers": []any{}},
			"relay_policy":               map[string]any{"allow_relay": false, "allow_relay_transfer": false},
		})
	}))
	defer hub.Close()

	manager := NewManager(remoteconfig.Config{
		Enabled:    true,
		DataDir:    dataDir,
		DeviceName: "signed-agent",
		HubURL:     hub.URL,
	}, inventoryProviderStub{
		items: []TerminalInventoryItem{{ID: "term-1", State: "running"}},
	}, nil)
	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if got.DeviceID == "" || got.AgentID == "" {
		t.Fatalf("registration missing identifiers: %+v", got)
	}
	if got.Signature.Algorithm != "ed25519" || got.Signature.Nonce == "" ||
		got.Signature.Timestamp == 0 || got.Signature.Value == "" {
		t.Fatalf("registration missing signature: %+v", got.Signature)
	}
	rawSignature, err := base64.StdEncoding.DecodeString(got.Signature.Value)
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	message := hubv1.CanonicalAgentRegistrationSignatureMessage(hubv1.AgentRegistrationSignatureFields{
		MachineID: got.DeviceID,
		AgentID:   got.AgentID,
		Nonce:     got.Signature.Nonce,
		Timestamp: time.Unix(got.Signature.Timestamp, 0).UTC(),
	})
	if !ed25519.Verify(machineKey.PublicKey, message, rawSignature) {
		t.Fatalf("registration signature does not verify")
	}
}
