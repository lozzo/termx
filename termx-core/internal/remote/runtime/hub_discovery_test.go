package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	remoteconfig "github.com/lozzow/termx/termx-core/internal/remote/config"
)

func TestManagerDiscoversAndSelectsHubAfterControlRegistration(t *testing.T) {
	t.Parallel()

	var controlRegisterPath string
	var controlHubPath string
	var controlAuth string
	var hubRegisterPath string
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hubRegisterPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{
			"version":                    "remote.hub.v1",
			"hub_id":                     "hub-discovered",
			"agent_session_id":           "agent-session-discovered",
			"heartbeat_interval_seconds": 15,
			"rtc_config":                 map[string]any{"ice_servers": []any{}},
			"relay_policy":               map[string]any{"allow_relay": false, "allow_relay_transfer": false},
		})
	}))
	defer hub.Close()
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		controlAuth = r.Header.Get("Authorization")
		switch r.URL.Path {
		case "/api/devices/register":
			controlRegisterPath = r.URL.Path
			_ = json.NewEncoder(w).Encode(map[string]any{"device": map[string]any{"id": "device-1"}})
		case "/api/v1/hubs":
			controlHubPath = r.URL.Path
			_ = json.NewEncoder(w).Encode(map[string]any{
				"hubs": []map[string]any{{
					"id":       "hub-discovered",
					"region":   "iad",
					"http_url": hub.URL,
					"status":   "online",
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer control.Close()

	manager := NewManager(remoteconfig.Config{
		Enabled:     true,
		DataDir:     t.TempDir(),
		DeviceName:  "hub-discover-daemon",
		ControlURL:  control.URL,
		AccessToken: "control-secret",
	}, inventoryProviderStub{}, nil)
	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	status := manager.Status()
	if status.State != StateOnline {
		t.Fatalf("expected online after discovered hub registration, got %+v", status)
	}
	if status.HubURL != hub.URL {
		t.Fatalf("expected selected hub URL %q, got %q", hub.URL, status.HubURL)
	}
	if controlRegisterPath != "/api/devices/register" || controlHubPath != "/api/v1/hubs" {
		t.Fatalf("control endpoints not called as expected: register=%q hubs=%q", controlRegisterPath, controlHubPath)
	}
	if controlAuth != "Bearer control-secret" {
		t.Fatalf("expected bearer auth to control, got %q", controlAuth)
	}
	if hubRegisterPath != "/api/v1/agents/register" {
		t.Fatalf("expected hub registration after discovery, got %q", hubRegisterPath)
	}
}
