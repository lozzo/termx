package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-hub/internal/hub/cloud"
	"github.com/lozzow/termx/termx-hub/internal/hub/httpapi"
	"github.com/lozzow/termx/termx-hub/internal/hub/registry"
)

func TestHubHTTPHandlesBrowserCORSPreflight(t *testing.T) {
	t.Parallel()

	router := httpapi.NewHandler(httpapi.Config{})
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/pairing/claims", nil)
	req.Header.Set("Origin", "http://127.0.0.1:5173")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "content-type")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("allow origin = %q", got)
	}
	if got := strings.ToLower(rec.Header().Get("Access-Control-Allow-Headers")); !strings.Contains(got, "content-type") {
		t.Fatalf("allow headers = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(got, "POST") || !strings.Contains(got, "OPTIONS") {
		t.Fatalf("allow methods = %q", got)
	}
}

func TestHubHTTPRestrictsConfiguredCORSOrigins(t *testing.T) {
	t.Parallel()

	router := httpapi.NewHandler(httpapi.Config{
		AllowedOrigins: []string{"http://allowed.termx.test"},
	})

	allowedReq := httptest.NewRequest(http.MethodOptions, "/api/v1/pairing/claims", nil)
	allowedReq.Header.Set("Origin", "http://allowed.termx.test")
	allowedReq.Header.Set("Access-Control-Request-Method", "POST")
	allowedRec := httptest.NewRecorder()
	router.ServeHTTP(allowedRec, allowedReq)
	if got := allowedRec.Header().Get("Access-Control-Allow-Origin"); got != "http://allowed.termx.test" {
		t.Fatalf("allowed origin header = %q", got)
	}

	blockedReq := httptest.NewRequest(http.MethodOptions, "/api/v1/pairing/claims", nil)
	blockedReq.Header.Set("Origin", "http://blocked.termx.test")
	blockedReq.Header.Set("Access-Control-Request-Method", "POST")
	blockedRec := httptest.NewRecorder()
	router.ServeHTTP(blockedRec, blockedReq)
	if got := blockedRec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("blocked origin header = %q", got)
	}
}

func TestAgentHTTPControlPlaneEndpointsAreRemoved(t *testing.T) {
	t.Parallel()

	reg := registry.New(registry.Config{})
	router := httpapi.NewHandler(httpapi.Config{
		Cloud:    cloud.NewService(cloud.Config{Registry: reg}),
		Registry: reg,
	})
	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/debug/agents"},
		{http.MethodPost, "/api/v1/agents/register"},
		{http.MethodPost, "/api/v1/agents/heartbeat"},
		{http.MethodPost, "/api/v1/agents/signaling/poll"},
		{http.MethodPost, "/api/v1/agents/signaling/answer"},
		{http.MethodPost, "/api/v1/agents/pairing/poll"},
		{http.MethodPost, "/api/v1/agents/pairing/result"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			var rec *httptest.ResponseRecorder
			if tc.method == http.MethodPost {
				rec = postJSON(t, router, tc.path, map[string]any{})
			} else {
				req := httptest.NewRequest(tc.method, tc.path, nil)
				req.Header.Set("X-TermX-Debug-Token", "debug-secret")
				rec = httptest.NewRecorder()
				router.ServeHTTP(rec, req)
			}
			if rec.Code != http.StatusNotFound {
				t.Fatalf("%s %s status = %d body=%s", tc.method, tc.path, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestLocalDiscoveryListsOnlineAgentsOnlyWhenEnabled(t *testing.T) {
	t.Parallel()

	reg := registry.New(registry.Config{})
	if _, err := reg.Register(context.Background(), registry.RegisterInput{
		MachineID: "device_local",
		AgentID:   "agent_local",
		Terminals: []registry.Terminal{{ID: "term_local", Name: "dev shell", State: "running"}},
	}); err != nil {
		t.Fatalf("register local agent: %v", err)
	}
	router := httpapi.NewHandler(httpapi.Config{
		Registry:       reg,
		LocalDiscovery: true,
	})
	resp := getJSON(t, router, "/api/v1/agents/online", "")
	if resp.Code != http.StatusOK {
		t.Fatalf("local discovery status = %d body=%s", resp.Code, resp.Body.String())
	}
	var body struct {
		Agents []struct {
			AgentID   string `json:"agent_id"`
			MachineID string `json:"machine_id"`
			Terminals []struct {
				TerminalID string `json:"terminal_id"`
				Title      string `json:"title"`
				Name       string `json:"name"`
				State      string `json:"state"`
			} `json:"terminals"`
		} `json:"agents"`
	}
	decodeJSON(t, resp, &body)
	if len(body.Agents) != 1 || body.Agents[0].MachineID != "device_local" ||
		body.Agents[0].AgentID != "agent_local" || len(body.Agents[0].Terminals) != 1 ||
		body.Agents[0].Terminals[0].TerminalID != "term_local" ||
		body.Agents[0].Terminals[0].Title != "dev shell" ||
		body.Agents[0].Terminals[0].Name != "dev shell" {
		t.Fatalf("unexpected local discovery body: %+v", body)
	}

	cloudRouter := httpapi.NewHandler(httpapi.Config{Registry: reg})
	cloudResp := getJSON(t, cloudRouter, "/api/v1/agents/online", "")
	if cloudResp.Code != http.StatusNotFound {
		t.Fatalf("cloud hub should not expose local discovery, got %d body=%s", cloudResp.Code, cloudResp.Body.String())
	}
}

func TestLocalDiscoveryReflectsRegistryHeartbeats(t *testing.T) {
	t.Parallel()

	clock := fixedClock(time.Date(2026, 5, 5, 10, 45, 0, 0, time.UTC))
	reg := registry.New(registry.Config{Clock: clock})
	if _, err := reg.Register(context.Background(), registry.RegisterInput{
		MachineID: "device_local",
		AgentID:   "agent_local",
	}); err != nil {
		t.Fatalf("register local agent: %v", err)
	}
	if err := reg.Heartbeat(context.Background(), registry.HeartbeatInput{
		MachineID: "device_local",
		AgentID:   "agent_local",
		Terminals: []registry.Terminal{{
			ID:    "term_after_register",
			Name:  "worker",
			State: "running",
		}},
	}); err != nil {
		t.Fatalf("heartbeat local agent: %v", err)
	}
	router := httpapi.NewHandler(httpapi.Config{
		Registry:       reg,
		LocalDiscovery: true,
		Clock:          clock,
	})

	resp := getJSON(t, router, "/api/v1/agents/online", "")
	if resp.Code != http.StatusOK {
		t.Fatalf("local discovery status = %d body=%s", resp.Code, resp.Body.String())
	}
	var body struct {
		Agents []struct {
			Terminals []struct {
				TerminalID string `json:"terminal_id"`
				Title      string `json:"title"`
				State      string `json:"state"`
			} `json:"terminals"`
		} `json:"agents"`
	}
	decodeJSON(t, resp, &body)
	if len(body.Agents) != 1 || len(body.Agents[0].Terminals) != 1 ||
		body.Agents[0].Terminals[0].TerminalID != "term_after_register" ||
		body.Agents[0].Terminals[0].Title != "worker" ||
		body.Agents[0].Terminals[0].State != "running" {
		t.Fatalf("local discovery did not reflect heartbeat terminals: %+v", body)
	}
}

func TestAgentHTTPKickForcesAgentOffline(t *testing.T) {
	t.Parallel()

	clock := fixedClock(time.Date(2026, 5, 3, 14, 45, 0, 0, time.UTC))
	reg := registry.New(registry.Config{Clock: clock})
	if _, err := reg.Register(context.Background(), registry.RegisterInput{
		MachineID: "device_1",
		AgentID:   "agent_kick",
	}); err != nil {
		t.Fatalf("register agent: %v", err)
	}
	router := httpapi.NewHandler(httpapi.Config{
		Cloud:          cloud.NewService(cloud.Config{Registry: reg, Clock: clock}),
		Registry:       reg,
		InternalSecret: "hub-secret",
		Clock:          clock,
	})

	unauthorized := postJSON(t, router, "/api/internal/kick", map[string]any{
		"agent_id": "agent_kick",
	})
	if unauthorized.Code != http.StatusForbidden {
		t.Fatalf("unauthorized kick status = %d body=%s", unauthorized.Code, unauthorized.Body.String())
	}

	kick := postJSONWithSecret(t, router, "/api/internal/kick", "hub-secret", map[string]any{
		"agent_id": "agent_kick",
		"reason":   "deleted in control",
	})
	if kick.Code != http.StatusOK {
		t.Fatalf("kick status = %d body=%s", kick.Code, kick.Body.String())
	}
	if _, ok := reg.GetAgent("agent_kick"); ok {
		t.Fatal("kicked agent is still online")
	}
}
