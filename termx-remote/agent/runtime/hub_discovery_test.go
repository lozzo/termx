package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	remoteconfig "github.com/lozzow/termx/termx-remote/config"
	"github.com/lozzow/termx/termx-remote/discovery"
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
					"capacity": 1,
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

func TestManagerDiscoversHubUsingRegionAndWeightPolicy(t *testing.T) {
	t.Parallel()

	var selectedHubRegisterPath string
	selectedHub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		selectedHubRegisterPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{
			"version":                    "remote.hub.v1",
			"hub_id":                     "hub-selected",
			"agent_session_id":           "agent-session-selected",
			"heartbeat_interval_seconds": 15,
			"rtc_config":                 map[string]any{"ice_servers": []any{}},
			"relay_policy":               map[string]any{"allow_relay": false, "allow_relay_transfer": false},
		})
	}))
	defer selectedHub.Close()
	unselectedHub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unselected hub should not receive registration: %s", r.URL.Path)
	}))
	defer unselectedHub.Close()
	now := time.Now().UTC()
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/devices/register":
			_ = json.NewEncoder(w).Encode(map[string]any{"device": map[string]any{"id": "device-1"}})
		case "/api/v1/hubs":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"hubs": []map[string]any{
					{
						"id":         "remote-heavy",
						"region":     "iad",
						"http_url":   unselectedHub.URL,
						"status":     "online",
						"capacity":   200,
						"weight":     100,
						"health":     `{"ok":true}`,
						"expires_at": now.Add(time.Hour).Format(time.RFC3339),
					},
					{
						"id":         "preferred-light",
						"region":     "sin",
						"http_url":   unselectedHub.URL,
						"status":     "online",
						"capacity":   20,
						"weight":     1,
						"health":     `{"ok":true}`,
						"expires_at": now.Add(time.Hour).Format(time.RFC3339),
					},
					{
						"id":         "preferred-weighted",
						"region":     "sin",
						"http_url":   selectedHub.URL,
						"status":     "online",
						"capacity":   10,
						"weight":     10,
						"health":     `{"ok":true}`,
						"expires_at": now.Add(time.Hour).Format(time.RFC3339),
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer control.Close()

	manager := NewManager(remoteconfig.Config{
		Enabled:     true,
		DataDir:     t.TempDir(),
		DeviceName:  "hub-select-daemon",
		ControlURL:  control.URL,
		AccessToken: "control-secret",
		Region:      "sin",
	}, inventoryProviderStub{}, nil)
	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	status := manager.Status()
	if status.State != StateOnline {
		t.Fatalf("expected online after selected hub registration, got %+v", status)
	}
	if status.HubURL != selectedHub.URL {
		t.Fatalf("expected selected hub URL %q, got %q", selectedHub.URL, status.HubURL)
	}
	if selectedHubRegisterPath != "/api/v1/agents/register" {
		t.Fatalf("expected selected hub registration, got %q", selectedHubRegisterPath)
	}
}

func TestManagerReevaluatesDiscoveredHubOnReconcile(t *testing.T) {
	t.Parallel()

	var firstHubRegisters atomic.Int32
	firstHub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstHubRegisters.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"version":                    "remote.hub.v1",
			"hub_id":                     "hub-first",
			"agent_session_id":           "agent-session-first",
			"heartbeat_interval_seconds": 15,
			"rtc_config":                 map[string]any{"ice_servers": []any{}},
			"relay_policy":               map[string]any{"allow_relay": false, "allow_relay_transfer": false},
		})
	}))
	defer firstHub.Close()
	var secondHubRegisters atomic.Int32
	secondHub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondHubRegisters.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"version":                    "remote.hub.v1",
			"hub_id":                     "hub-second",
			"agent_session_id":           "agent-session-second",
			"heartbeat_interval_seconds": 15,
			"rtc_config":                 map[string]any{"ice_servers": []any{}},
			"relay_policy":               map[string]any{"allow_relay": false, "allow_relay_transfer": false},
		})
	}))
	defer secondHub.Close()

	var useSecond atomic.Bool
	now := time.Now().UTC()
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/devices/register":
			_ = json.NewEncoder(w).Encode(map[string]any{"device": map[string]any{"id": "device-1"}})
		case "/api/v1/hubs":
			hubs := []map[string]any{{
				"id":         "hub-first",
				"region":     "sin",
				"http_url":   firstHub.URL,
				"status":     "online",
				"capacity":   10,
				"weight":     10,
				"health":     `{"ok":true}`,
				"expires_at": now.Add(time.Hour).Format(time.RFC3339),
			}}
			if useSecond.Load() {
				hubs = []map[string]any{
					{
						"id":         "hub-first",
						"region":     "sin",
						"http_url":   firstHub.URL,
						"status":     "online",
						"capacity":   0,
						"weight":     10,
						"health":     `{"ok":true}`,
						"expires_at": now.Add(time.Hour).Format(time.RFC3339),
					},
					{
						"id":         "hub-second",
						"region":     "sin",
						"http_url":   secondHub.URL,
						"status":     "online",
						"capacity":   10,
						"weight":     9,
						"health":     `{"ok":true}`,
						"expires_at": now.Add(time.Hour).Format(time.RFC3339),
					},
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"hubs": hubs})
		default:
			http.NotFound(w, r)
		}
	}))
	defer control.Close()

	manager := NewManager(remoteconfig.Config{
		Enabled:     true,
		DataDir:     t.TempDir(),
		DeviceName:  "hub-reevaluate-daemon",
		ControlURL:  control.URL,
		AccessToken: "control-secret",
		Region:      "sin",
	}, inventoryProviderStub{}, nil)
	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if status := manager.Status(); status.HubURL != firstHub.URL {
		t.Fatalf("initial selected hub URL = %q, want %q", status.HubURL, firstHub.URL)
	}

	useSecond.Store(true)
	state, detail, err := manager.reconcile(context.Background())
	if err != nil {
		t.Fatalf("reconcile after hub policy change: %v", err)
	}
	manager.setStatus(state, detail)
	status := manager.Status()
	if status.HubURL != secondHub.URL {
		t.Fatalf("reevaluated hub URL = %q, want %q", status.HubURL, secondHub.URL)
	}
	if secondHubRegisters.Load() == 0 {
		t.Fatal("second hub did not receive registration after policy changed")
	}
}

func TestSelectDiscoveredHubFiltersUnusableAndPrefersRegionThenWeight(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 3, 18, 0, 0, 0, time.UTC)
	selected, ok := selectDiscoveredHub([]discovery.Hub{
		{
			ID:        "expired",
			Region:    "iad",
			HTTPURL:   "https://expired.termx.test",
			Status:    "online",
			Capacity:  100,
			Weight:    100,
			Health:    `{"ok":true}`,
			ExpiresAt: now.Add(-time.Minute).Format(time.RFC3339),
		},
		{
			ID:        "offline",
			Region:    "sin",
			HTTPURL:   "https://offline.termx.test",
			Status:    "offline",
			Capacity:  100,
			Weight:    100,
			Health:    `{"ok":true}`,
			ExpiresAt: now.Add(time.Minute).Format(time.RFC3339),
		},
		{
			ID:        "unhealthy",
			Region:    "sin",
			HTTPURL:   "https://unhealthy.termx.test",
			Status:    "online",
			Capacity:  100,
			Weight:    100,
			Health:    `{"ok":false}`,
			ExpiresAt: now.Add(time.Minute).Format(time.RFC3339),
		},
		{
			ID:        "zero-capacity",
			Region:    "sin",
			HTTPURL:   "https://zero.termx.test",
			Status:    "online",
			Capacity:  0,
			Weight:    100,
			Health:    `{"ok":true}`,
			ExpiresAt: now.Add(time.Minute).Format(time.RFC3339),
		},
		{
			ID:        "remote-heavy",
			Region:    "iad",
			HTTPURL:   "https://remote-heavy.termx.test",
			Status:    "online",
			Capacity:  200,
			Weight:    200,
			Health:    `{"ok":true}`,
			ExpiresAt: now.Add(time.Hour).Format(time.RFC3339),
		},
		{
			ID:        "preferred",
			Region:    "sin",
			HTTPURL:   "https://preferred.termx.test",
			Status:    "online",
			Capacity:  20,
			Weight:    4,
			Health:    `{"ok":true}`,
			ExpiresAt: now.Add(time.Hour).Format(time.RFC3339),
		},
		{
			ID:        "preferred-weighted",
			Region:    "sin",
			HTTPURL:   "https://preferred-weighted.termx.test",
			Status:    "online",
			Capacity:  10,
			Weight:    8,
			Health:    `{"ok":true}`,
			ExpiresAt: now.Add(time.Hour).Format(time.RFC3339),
		},
	}, hubSelectionOptions{PreferredRegion: "sin", Now: now})
	if !ok {
		t.Fatal("expected usable hub")
	}
	if selected.ID != "preferred-weighted" {
		t.Fatalf("selected hub = %+v", selected)
	}
}

func TestSelectDiscoveredHubRanksByWeightThenCapacityWithoutPreferredRegion(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 3, 18, 0, 0, 0, time.UTC)
	selected, ok := selectDiscoveredHub([]discovery.Hub{
		{
			ID:        "capacity-only",
			Region:    "iad",
			HTTPURL:   "https://capacity.termx.test",
			Status:    "online",
			Capacity:  200,
			Weight:    1,
			Health:    `{"status":"healthy"}`,
			ExpiresAt: now.Add(time.Hour).Format(time.RFC3339),
		},
		{
			ID:        "weighted",
			Region:    "fra",
			HTTPURL:   "https://weighted.termx.test",
			Status:    "online",
			Capacity:  20,
			Weight:    10,
			Health:    `{"status":"healthy"}`,
			ExpiresAt: now.Add(time.Hour).Format(time.RFC3339),
		},
	}, hubSelectionOptions{Now: now})
	if !ok {
		t.Fatal("expected usable hub")
	}
	if selected.ID != "weighted" {
		t.Fatalf("selected hub = %+v", selected)
	}
}
