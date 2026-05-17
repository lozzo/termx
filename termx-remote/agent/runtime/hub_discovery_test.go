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
	remotertc "github.com/lozzow/termx/termx-remote/session/rtc"
)

func TestManagerDiscoversAndSelectsHubAfterControlRegistration(t *testing.T) {
	t.Parallel()

	var controlRegisterPath string
	var controlHubPath string
	var controlAuth string
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
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
	if status.State != StateRegistering {
		t.Fatalf("expected registering while discovered hub gRPC registration is pending, got %+v", status)
	}
	if status.HubURL != hub.URL {
		t.Fatalf("expected selected hub URL %q, got %q", hub.URL, status.HubURL)
	}
	if len(status.Hubs) != 1 ||
		status.Hubs[0].Kind != HubKindHub ||
		status.Hubs[0].Source != HubSourceWebControl ||
		status.Hubs[0].State != HubConnectionConnecting {
		t.Fatalf("unexpected discovered hub status: %+v", status.Hubs)
	}
	if controlRegisterPath != "/api/devices/register" || controlHubPath != "/api/v1/hubs" {
		t.Fatalf("control endpoints not called as expected: register=%q hubs=%q", controlRegisterPath, controlHubPath)
	}
	if controlAuth != "Bearer control-secret" {
		t.Fatalf("expected bearer auth to control, got %q", controlAuth)
	}
}

func TestManagerDiscoversHubUsingRegionAndWeightPolicy(t *testing.T) {
	t.Parallel()

	selectedHub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer selectedHub.Close()
	unselectedHub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
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
	if status.State != StateRegistering {
		t.Fatalf("expected registering while selected hub gRPC registration is pending, got %+v", status)
	}
	if status.HubURL != selectedHub.URL {
		t.Fatalf("expected selected hub URL %q, got %q", selectedHub.URL, status.HubURL)
	}
}

func TestManagerReevaluatesDiscoveredHubOnReconcile(t *testing.T) {
	t.Parallel()

	firstHub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer firstHub.Close()
	secondHub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
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
}

func TestManagerAddsDiscoveredHubWithoutDroppingExistingLocalHub(t *testing.T) {
	localHub := newDiscoveryHubTestServer(t, false)
	defer localHub.Close()
	cloudHub := newDiscoveryHubTestServer(t, false)
	defer cloudHub.Close()
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/devices/register":
			_ = json.NewEncoder(w).Encode(map[string]any{"device": map[string]any{"id": "device-1"}})
		case "/api/v1/hubs":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"hubs": []map[string]any{{
					"id":       "hub-cloud",
					"region":   "sin",
					"http_url": cloudHub.URL,
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
		Enabled:    true,
		DataDir:    t.TempDir(),
		DeviceName: "both-mode-daemon",
		Mode:       "local",
	}, inventoryProviderStub{}, nil)
	manager.AddHubURLWithAnswerOptions(localHub.URL, remotertc.AnswerOptions{})
	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	defer manager.Close()
	waitForHubLoop(t, manager, localHub.URL)
	localAgentID := manager.buildGRPCRegisterRequest().GetAgentId()

	manager.ConfigureCloud(control.URL, "control-secret", "")
	state, detail, err := manager.reconcile(context.Background())
	if err != nil {
		t.Fatalf("reconcile after cloud discovery config: %v", err)
	}
	manager.setStatus(state, detail)

	manager.mu.RLock()
	hubURLs := append([]string(nil), manager.cfg.HubURLs...)
	manager.mu.RUnlock()
	waitForHubLoop(t, manager, localHub.URL)
	waitForHubLoop(t, manager, cloudHub.URL)
	agentID := manager.buildGRPCRegisterRequest().GetAgentId()
	if !containsString(hubURLs, localHub.URL) || !containsString(hubURLs, cloudHub.URL) {
		t.Fatalf("discovery dropped an existing hub URL: %+v", hubURLs)
	}
	if agentID == "" || localAgentID != agentID {
		t.Fatalf("expected discovered cloud hub to reuse local agent id, local=%q cloud=%q", localAgentID, agentID)
	}
}

func TestManagerKeepsLocalHubWhenExplicitCloudHubConfigured(t *testing.T) {
	localHub := newDiscoveryHubTestServer(t, false)
	defer localHub.Close()
	cloudHub := newDiscoveryHubTestServer(t, false)
	defer cloudHub.Close()
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/devices/register":
			_ = json.NewEncoder(w).Encode(map[string]any{"device": map[string]any{"id": "device-1"}})
		case "/api/v1/hubs":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"hubs": []map[string]any{{
					"id":       "hub-cloud",
					"region":   "sin",
					"http_url": cloudHub.URL,
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
		Enabled:    true,
		DataDir:    t.TempDir(),
		DeviceName: "both-mode-explicit-daemon",
		Mode:       "local",
	}, inventoryProviderStub{}, nil)
	manager.AddExplicitHubURL(cloudHub.URL)
	manager.ConfigureCloud(control.URL, "control-secret", "")
	manager.AddHubURLWithAnswerOptions(localHub.URL, remotertc.AnswerOptions{})
	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	defer manager.Close()

	manager.mu.RLock()
	hubURLs := append([]string(nil), manager.cfg.HubURLs...)
	manager.mu.RUnlock()
	waitForHubLoop(t, manager, localHub.URL)
	waitForHubLoop(t, manager, cloudHub.URL)
	agentID := manager.buildGRPCRegisterRequest().GetAgentId()
	if !containsString(hubURLs, localHub.URL) || !containsString(hubURLs, cloudHub.URL) {
		t.Fatalf("explicit cloud discovery dropped an existing hub URL: %+v", hubURLs)
	}
	if agentID == "" {
		t.Fatal("expected explicit cloud hub to share generated agent id")
	}
}

type discoveryHubTestServer struct {
	*httptest.Server
}

func newDiscoveryHubTestServer(t *testing.T, fail bool) *discoveryHubTestServer {
	t.Helper()
	s := &discoveryHubTestServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail {
			http.Error(w, "hub unavailable", http.StatusServiceUnavailable)
			return
		}
		switch r.URL.Path {
		case "/api/health":
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	return s
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
