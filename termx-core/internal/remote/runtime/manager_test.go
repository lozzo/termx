package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	remoteconfig "github.com/lozzow/termx/termx-core/internal/remote/config"
	remotertc "github.com/lozzow/termx/termx-core/internal/remote/rtc"
)

type inventoryProviderStub struct {
	items []TerminalInventoryItem
}

func (s inventoryProviderStub) ListRemoteTerminals(context.Context) []TerminalInventoryItem {
	return append([]TerminalInventoryItem(nil), s.items...)
}

func TestManagerStartDisabled(t *testing.T) {
	mgr := NewManager(remoteconfig.Config{}, nil, nil)
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	status := mgr.Status()
	if status.State != StateDisabled {
		t.Fatalf("expected disabled state, got %q", status.State)
	}
}

func TestManagerStartConfiguredWithoutEndpoints(t *testing.T) {
	mgr := NewManager(remoteconfig.Config{
		Enabled:    true,
		DataDir:    t.TempDir(),
		DeviceName: "device-a",
	}, inventoryProviderStub{
		items: []TerminalInventoryItem{{ID: "1"}, {ID: "2"}},
	}, nil)
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	status := mgr.Status()
	if status.State != StateConfigured {
		t.Fatalf("expected configured state, got %q", status.State)
	}
	if status.DeviceID == "" {
		t.Fatal("expected device id to be populated")
	}
	if status.TerminalCount != 2 {
		t.Fatalf("expected terminal count 2, got %d", status.TerminalCount)
	}
}

func TestManagerStartDegradedWhenControlTokenMissing(t *testing.T) {
	mgr := NewManager(remoteconfig.Config{
		Enabled:    true,
		DataDir:    t.TempDir(),
		ControlURL: "https://control.example.test",
	}, nil, nil)
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	status := mgr.Status()
	if status.State != StateDegraded {
		t.Fatalf("expected degraded state, got %q", status.State)
	}
}

func TestManagerStartRegisteringWhenEndpointsConfigured(t *testing.T) {
	var gotPath string
	var gotAuth string
	var gotBody map[string]any
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"device":{"id":"device-1"}}`))
	}))
	defer control.Close()

	var hubRegisterPath string
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hubRegisterPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version":"remote.hub.v1","hub_id":"hub-local","agent_session_id":"agent-session-1","heartbeat_interval_seconds":15,"rtc_config":{"ice_servers":[]},"relay_policy":{"allow_relay":false,"allow_relay_transfer":false}}`))
	}))
	defer hub.Close()

	mgr := NewManager(remoteconfig.Config{
		Enabled:     true,
		DataDir:     t.TempDir(),
		DeviceName:  "device-b",
		ControlURL:  control.URL,
		HubURL:      hub.URL,
		AccessToken: "secret",
	}, inventoryProviderStub{
		items: []TerminalInventoryItem{{
			ID:      "term-1",
			Name:    "shell",
			State:   "running",
			Command: []string{"bash"},
			Cols:    80,
			Rows:    24,
		}},
	}, nil)
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	status := mgr.Status()
	if status.State != StateOnline {
		t.Fatalf("expected online state, got %q", status.State)
	}
	if status.DeviceName != "device-b" {
		t.Fatalf("expected device name device-b, got %q", status.DeviceName)
	}
	if gotPath != "/api/devices/register" {
		t.Fatalf("expected control registration path /api/devices/register, got %q", gotPath)
	}
	if gotAuth != "Bearer secret" {
		t.Fatalf("expected bearer auth, got %q", gotAuth)
	}
	if gotBody["deviceId"] == "" {
		t.Fatalf("expected deviceId in control registration body, got %#v", gotBody)
	}
	if hubRegisterPath != "/api/v1/agents/register" {
		t.Fatalf("expected hub registration path /api/v1/agents/register, got %q", hubRegisterPath)
	}
}

func TestManagerReregistersHubWhenHeartbeatUnauthorized(t *testing.T) {
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"device":{"id":"device-1"}}`))
	}))
	defer control.Close()

	var registerCount atomic.Int32
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/agents/register":
			count := registerCount.Add(1)
			_, _ = w.Write([]byte(`{"version":"remote.hub.v1","hub_id":"hub-local","agent_session_id":"agent-session-` + string(rune('0'+count)) + `","heartbeat_interval_seconds":15,"rtc_config":{"ice_servers":[]},"relay_policy":{"allow_relay":false,"allow_relay_transfer":false}}`))
		case "/api/v1/agents/heartbeat":
			var req struct {
				AgentSessionID string `json:"agent_session_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode heartbeat: %v", err)
			}
			if req.AgentSessionID == "agent-session-1" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"unknown device or agent session"}`))
				return
			}
			_, _ = w.Write([]byte(`{"accepted":true,"next_heartbeat_seconds":15}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer hub.Close()

	mgr := NewManager(remoteconfig.Config{
		Enabled:     true,
		DataDir:     t.TempDir(),
		DeviceName:  "device-c",
		ControlURL:  control.URL,
		HubURL:      hub.URL,
		AccessToken: "secret",
	}, inventoryProviderStub{}, nil)
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	if err := mgr.syncHubPresence(context.Background()); err != nil {
		t.Fatalf("syncHubPresence returned error: %v", err)
	}

	mgr.mu.RLock()
	hubSessionID := mgr.hubSessionID
	mgr.mu.RUnlock()
	if hubSessionID != "agent-session-2" {
		t.Fatalf("expected hub session to refresh to agent-session-2, got %q", hubSessionID)
	}
	if got := registerCount.Load(); got != 2 {
		t.Fatalf("expected two hub registrations, got %d", got)
	}
}

func TestManagerProvidesTerminalManagementRouterForManagedRTC(t *testing.T) {
	manager := NewManager(remoteconfig.Config{}, managementProviderStub{}, nil)
	if manager.terminalManagementRouter() == nil {
		t.Fatal("expected managed runtime to provide terminal management router")
	}
	status, _, errMsg := manager.terminalManagementRouter().RouteTerminalManagementRequest(context.Background(), remotertc.TerminalManagementRequest{
		Method: "create",
		Path:   "create",
		Body:   json.RawMessage(`{"command":["/bin/sh"],"name":"managed shell"}`),
	})
	if status == http.StatusForbidden || errMsg == "terminal management is not allowed by connection policy" {
		t.Fatalf("managed terminal management router must not be nil or forbidden, got status=%d err=%q", status, errMsg)
	}
}

type managementProviderStub struct {
	inventoryProviderStub
}

func (s managementProviderStub) RouteTerminalManagementRequest(_ context.Context, req remotertc.TerminalManagementRequest) (int32, []byte, string) {
	if req.Path != "create" {
		return http.StatusNotFound, nil, "unknown terminal management route"
	}
	return http.StatusOK, []byte(`{"terminal_id":"managed-terminal-1"}`), ""
}
