package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	remoteconfig "github.com/lozzow/termx/termx-remote/config"
	hubv1 "github.com/lozzow/termx/termx-remote/protocol/hubv1"
)

func TestManagerHubRegistrationIncludesRuntimeIdentity(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
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
		Mode:       "local",
	}, inventoryProviderStub{
		items: []TerminalInventoryItem{{ID: "term-1", State: "running"}},
	}, nil)
	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if got.DeviceID == "" || got.AgentID == "" {
		t.Fatalf("registration missing identifiers: %+v", got)
	}
	if got.DisplayName != "signed-agent" || got.Version != "remote.hub.v1" || got.RuntimeVersion != "termx-dev" {
		t.Fatalf("registration identity = %+v", got)
	}
	if len(got.Terminals) != 1 || got.Terminals[0].ID != "term-1" {
		t.Fatalf("registration terminals = %+v", got.Terminals)
	}
}
