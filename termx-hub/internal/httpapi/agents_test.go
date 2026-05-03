package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-hub/internal/httpapi"
	"github.com/lozzow/termx/termx-hub/internal/ice"
	"github.com/lozzow/termx/termx-hub/internal/managed"
	"github.com/lozzow/termx/termx-hub/internal/registry"
)

func TestAgentHTTPRegisterHeartbeatPollAndAnswer(t *testing.T) {
	t.Parallel()

	clock := fixedClock(time.Date(2026, 5, 3, 11, 35, 0, 0, time.UTC))
	verifier := &fakeTicketVerifier{now: clock.Now(), tickets: map[string]managed.Ticket{
		"ticket_allowed": {
			ID:         "ticket_allowed",
			MachineID:  "device_1",
			TerminalID: "term_1",
			Path:       managed.PathManaged,
			AllowRelay: true,
			ExpiresAt:  clock.Now().Add(time.Minute),
		},
	}}
	reg := registry.New(registry.Config{Clock: clock, Verifier: verifier})
	svc := managed.NewService(managed.Config{Registry: reg, Tickets: verifier, Clock: clock})
	router := httpapi.NewHandler(httpapi.Config{
		Managed:  svc,
		Registry: reg,
		ICE: ice.NewService(ice.Config{
			Clock:        clock,
			SharedSecret: "turn-secret",
			STUNURLs:     []string{"stun:hub.termx.test:3478"},
			TURNURLs:     []string{"turn:hub.termx.test:3478?transport=udp"},
		}),
	})

	register := postJSON(t, router, "/api/v1/agents/register", map[string]any{
		"version":         "remote.hub.v1",
		"device_id":       "device_1",
		"display_name":    "External Smoke Agent",
		"hostname":        "agent-host",
		"platform":        "linux/amd64",
		"runtime_version": "termx-dev",
		"terminals": []map[string]any{{
			"id":      "term_1",
			"name":    "Shell",
			"command": []string{"bash"},
			"cols":    80,
			"rows":    24,
			"state":   "running",
		}},
	})
	if register.Code != http.StatusOK {
		t.Fatalf("register status = %d body=%s", register.Code, register.Body.String())
	}
	var registered struct {
		Version                  string `json:"version"`
		HubID                    string `json:"hub_id"`
		AgentSessionID           string `json:"agent_session_id"`
		HeartbeatIntervalSeconds int    `json:"heartbeat_interval_seconds"`
	}
	decodeJSON(t, register, &registered)
	if registered.Version != "remote.hub.v1" || registered.HubID == "" ||
		registered.AgentSessionID == "" || registered.HeartbeatIntervalSeconds <= 0 {
		t.Fatalf("register response = %+v", registered)
	}

	heartbeat := postJSON(t, router, "/api/v1/agents/heartbeat", map[string]any{
		"agent_session_id": registered.AgentSessionID,
		"device_id":        "device_1",
		"last_seen_at":     clock.Now().Format(time.RFC3339),
		"terminals": []map[string]any{{
			"id":      "term_1",
			"name":    "Shell",
			"command": []string{"bash"},
			"cols":    80,
			"rows":    24,
			"state":   "running",
		}},
	})
	if heartbeat.Code != http.StatusOK {
		t.Fatalf("heartbeat status = %d body=%s", heartbeat.Code, heartbeat.Body.String())
	}
	var heartbeatResp struct {
		Accepted             bool `json:"accepted"`
		NextHeartbeatSeconds int  `json:"next_heartbeat_seconds"`
	}
	decodeJSON(t, heartbeat, &heartbeatResp)
	if !heartbeatResp.Accepted || heartbeatResp.NextHeartbeatSeconds <= 0 {
		t.Fatalf("heartbeat response = %+v", heartbeatResp)
	}

	sessionResponse := make(chan []byte, 1)
	go func() {
		resp := postJSON(t, router, "/api/v1/sessions", validManagedSessionRequestWithIDs("device_1", "term_1", "rtc_agent_http_1"))
		sessionResponse <- append([]byte(nil), resp.Body.Bytes()...)
	}()

	poll := postJSON(t, router, "/api/v1/agents/signaling/poll", map[string]any{
		"agent_session_id": registered.AgentSessionID,
		"device_id":        "device_1",
		"timeout_seconds":  1,
	})
	if poll.Code != http.StatusOK {
		t.Fatalf("poll status = %d body=%s", poll.Code, poll.Body.String())
	}
	var polled struct {
		Offer struct {
			SessionID          string `json:"session_id"`
			TicketID           string `json:"ticket_id"`
			DeviceID           string `json:"device_id"`
			TerminalID         string `json:"terminal_id"`
			SDP                string `json:"sdp"`
			AllowRelay         bool   `json:"allow_relay"`
			AllowRelayTransfer bool   `json:"allow_relay_transfer"`
			RTCConfig          struct {
				IceServers []struct {
					URLs       []string `json:"urls"`
					Username   string   `json:"username"`
					Credential string   `json:"credential"`
				} `json:"ice_servers"`
			} `json:"rtc_config"`
		} `json:"offer"`
	}
	decodeJSON(t, poll, &polled)
	if polled.Offer.SessionID != "rtc_agent_http_1" || polled.Offer.TicketID != "ticket_allowed" ||
		polled.Offer.DeviceID != "device_1" || polled.Offer.TerminalID != "term_1" {
		t.Fatalf("poll response = %+v", polled)
	}
	if !polled.Offer.AllowRelay || polled.Offer.AllowRelayTransfer {
		t.Fatalf("poll relay policy = %+v", polled.Offer)
	}
	if len(polled.Offer.RTCConfig.IceServers) != 2 || !strings.HasPrefix(polled.Offer.RTCConfig.IceServers[1].URLs[0], "turn:") ||
		polled.Offer.RTCConfig.IceServers[1].Username == "" || polled.Offer.RTCConfig.IceServers[1].Credential == "" {
		t.Fatalf("poll managed ICE servers = %+v", polled.Offer.RTCConfig.IceServers)
	}

	answer := postJSON(t, router, "/api/v1/agents/signaling/answer", map[string]any{
		"agent_session_id": registered.AgentSessionID,
		"device_id":        "device_1",
		"answer": map[string]any{
			"session_id":     polled.Offer.SessionID,
			"sdp":            minimalSDP("agent-http-answer"),
			"ice_candidates": []string{},
		},
	})
	if answer.Code != http.StatusNoContent {
		t.Fatalf("answer status = %d body=%s", answer.Code, answer.Body.String())
	}
	select {
	case body := <-sessionResponse:
		var session struct {
			SessionID string `json:"session_id"`
			Path      string `json:"path"`
			Answer    struct {
				SDP string `json:"sdp"`
			} `json:"answer"`
		}
		if err := json.Unmarshal(body, &session); err != nil {
			t.Fatalf("decode app session response %q: %v", string(body), err)
		}
		if session.SessionID != "rtc_agent_http_1" || session.Path != "managed" || session.Answer.SDP != minimalSDP("agent-http-answer") {
			t.Fatalf("session response = %+v", session)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("app session did not complete after agent answer")
	}
}

func TestAgentHTTPRejectsUnsignedRegistration(t *testing.T) {
	t.Parallel()

	clock := fixedClock(time.Date(2026, 5, 3, 14, 22, 0, 0, time.UTC))
	verifier := &fakeTicketVerifier{
		now:                   clock.Now(),
		requireAgentSignature: true,
		tickets: map[string]managed.Ticket{
			"ticket_allowed": {
				ID:         "ticket_allowed",
				MachineID:  "device_1",
				TerminalID: "term_1",
				Path:       managed.PathManaged,
				ExpiresAt:  clock.Now().Add(time.Minute),
			},
		},
	}
	reg := registry.New(registry.Config{Clock: clock, Verifier: verifier})
	router := httpapi.NewHandler(httpapi.Config{
		Managed:     managed.NewService(managed.Config{Registry: reg, Tickets: verifier, Clock: clock}),
		Registry:    reg,
		AgentPolicy: verifier,
	})

	resp := postJSON(t, router, "/api/v1/agents/register", map[string]any{
		"version":   "remote.hub.v1",
		"device_id": "device_1",
	})
	if resp.Code != http.StatusForbidden {
		t.Fatalf("unsigned register status = %d body=%s", resp.Code, resp.Body.String())
	}
	if _, ok := reg.GetAgent("agent_1"); ok {
		t.Fatal("unsigned registration created an agent")
	}
}

func TestAgentHTTPAppliesForceOfflinePolicy(t *testing.T) {
	t.Parallel()

	clock := fixedClock(time.Date(2026, 5, 3, 17, 44, 0, 0, time.UTC))
	verifier := &fakeTicketVerifier{
		now: clock.Now(),
		tickets: map[string]managed.Ticket{
			"ticket_allowed": {
				ID:         "ticket_allowed",
				MachineID:  "device_1",
				TerminalID: "term_1",
				Path:       managed.PathManaged,
				ExpiresAt:  clock.Now().Add(time.Minute),
			},
		},
		forceOffline: map[string]string{
			"device_1/agent_force": "owner requested",
		},
	}
	reg := registry.New(registry.Config{Clock: clock, Verifier: verifier})
	router := httpapi.NewHandler(httpapi.Config{
		Managed:     managed.NewService(managed.Config{Registry: reg, Tickets: verifier, Clock: clock}),
		Registry:    reg,
		AgentPolicy: verifier,
	})

	register := postJSON(t, router, "/api/v1/agents/register", map[string]any{
		"device_id": "device_1",
		"agent_id":  "agent_force",
	})
	if register.Code != http.StatusOK {
		t.Fatalf("register status = %d body=%s", register.Code, register.Body.String())
	}
	var registered struct {
		AgentSessionID string `json:"agent_session_id"`
	}
	decodeJSON(t, register, &registered)

	heartbeat := postJSON(t, router, "/api/v1/agents/heartbeat", map[string]any{
		"agent_session_id": registered.AgentSessionID,
		"device_id":        "device_1",
	})
	if heartbeat.Code != http.StatusForbidden {
		t.Fatalf("forced heartbeat status = %d body=%s", heartbeat.Code, heartbeat.Body.String())
	}
	poll := postJSON(t, router, "/api/v1/agents/signaling/poll", map[string]any{
		"agent_session_id": registered.AgentSessionID,
		"device_id":        "device_1",
		"timeout_seconds":  1,
	})
	if poll.Code != http.StatusForbidden {
		t.Fatalf("forced poll status = %d body=%s", poll.Code, poll.Body.String())
	}
}

func TestAgentHTTPSessionsAreTTLBounded(t *testing.T) {
	t.Parallel()

	clock := &mutableHTTPClock{value: time.Date(2026, 5, 3, 18, 20, 0, 0, time.UTC)}
	verifier := &fakeTicketVerifier{now: clock.Now(), tickets: map[string]managed.Ticket{
		"ticket_allowed": {
			ID:         "ticket_allowed",
			MachineID:  "device_1",
			TerminalID: "term_1",
			Path:       managed.PathManaged,
			ExpiresAt:  clock.Now().Add(time.Minute),
		},
	}}
	reg := registry.New(registry.Config{Clock: clock, Verifier: verifier})
	router := httpapi.NewHandler(httpapi.Config{
		Managed:          managed.NewService(managed.Config{Registry: reg, Tickets: verifier, Clock: clock}),
		Registry:         reg,
		DebugToken:       "debug-secret",
		Clock:            clock,
		AgentSessionTTL:  time.Second,
		MaxAgentSessions: 1,
	})

	first := postJSON(t, router, "/api/v1/agents/register", map[string]any{
		"device_id": "device_1",
		"agent_id":  "agent_1",
	})
	if first.Code != http.StatusOK {
		t.Fatalf("first register status = %d body=%s", first.Code, first.Body.String())
	}
	var firstRegistered struct {
		AgentSessionID string `json:"agent_session_id"`
	}
	decodeJSON(t, first, &firstRegistered)
	second := postJSON(t, router, "/api/v1/agents/register", map[string]any{
		"device_id": "device_2",
		"agent_id":  "agent_2",
	})
	if second.Code != http.StatusOK {
		t.Fatalf("second register status = %d body=%s", second.Code, second.Body.String())
	}
	if heartbeat := postJSON(t, router, "/api/v1/agents/heartbeat", map[string]any{
		"agent_session_id": firstRegistered.AgentSessionID,
		"device_id":        "device_1",
	}); heartbeat.Code != http.StatusUnauthorized {
		t.Fatalf("evicted heartbeat status = %d body=%s", heartbeat.Code, heartbeat.Body.String())
	}

	var secondRegistered struct {
		AgentSessionID string `json:"agent_session_id"`
	}
	decodeJSON(t, second, &secondRegistered)
	clock.value = clock.value.Add(2 * time.Second)
	diag := getJSON(t, router, "/api/debug/agents", "debug-secret")
	if diag.Code != http.StatusOK {
		t.Fatalf("diagnostics status = %d body=%s", diag.Code, diag.Body.String())
	}
	var got struct {
		Agents []struct {
			AgentSessionID string `json:"agent_session_id"`
		} `json:"agents"`
	}
	decodeJSON(t, diag, &got)
	if len(got.Agents) != 0 {
		t.Fatalf("expired agent sessions remained: %+v", got.Agents)
	}
	if heartbeat := postJSON(t, router, "/api/v1/agents/heartbeat", map[string]any{
		"agent_session_id": secondRegistered.AgentSessionID,
		"device_id":        "device_2",
	}); heartbeat.Code != http.StatusUnauthorized {
		t.Fatalf("expired heartbeat status = %d body=%s", heartbeat.Code, heartbeat.Body.String())
	}
}

func TestAgentDiagnosticsRequiresDebugTokenAndReportsPolls(t *testing.T) {
	t.Parallel()

	clock := fixedClock(time.Date(2026, 5, 3, 11, 50, 0, 0, time.UTC))
	verifier := &fakeTicketVerifier{now: clock.Now(), tickets: map[string]managed.Ticket{
		"ticket_allowed": {
			ID:         "ticket_allowed",
			MachineID:  "device_1",
			TerminalID: "term_1",
			Path:       managed.PathManaged,
			ExpiresAt:  clock.Now().Add(time.Minute),
		},
	}}
	reg := registry.New(registry.Config{Clock: clock, Verifier: verifier})
	svc := managed.NewService(managed.Config{Registry: reg, Tickets: verifier, Clock: clock})
	router := httpapi.NewHandler(httpapi.Config{Managed: svc, Registry: reg, DebugToken: "debug-secret"})

	register := postJSON(t, router, "/api/v1/agents/register", map[string]any{
		"device_id": "device_1",
		"terminals": []map[string]any{{
			"id":    "term_1",
			"state": "running",
		}},
	})
	if register.Code != http.StatusOK {
		t.Fatalf("register status = %d body=%s", register.Code, register.Body.String())
	}
	var registered struct {
		AgentSessionID string `json:"agent_session_id"`
	}
	decodeJSON(t, register, &registered)

	unauth := getJSON(t, router, "/api/debug/agents", "")
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("unauth diagnostics status = %d body=%s", unauth.Code, unauth.Body.String())
	}
	empty := getJSON(t, router, "/api/debug/agents", "debug-secret")
	if empty.Code != http.StatusOK {
		t.Fatalf("diagnostics status = %d body=%s", empty.Code, empty.Body.String())
	}
	var initial struct {
		Agents []struct {
			MachineID string `json:"machine_id"`
			PollCount int    `json:"poll_count"`
		} `json:"agents"`
	}
	decodeJSON(t, empty, &initial)
	if len(initial.Agents) != 1 || initial.Agents[0].MachineID != "device_1" || initial.Agents[0].PollCount != 0 {
		t.Fatalf("initial diagnostics = %+v", initial)
	}

	go func() {
		_ = postJSON(t, router, "/api/v1/sessions", validManagedSessionRequestWithIDs("device_1", "term_1", "rtc_agent_diag"))
	}()
	poll := postJSON(t, router, "/api/v1/agents/signaling/poll", map[string]any{
		"agent_session_id": registered.AgentSessionID,
		"device_id":        "device_1",
		"timeout_seconds":  1,
	})
	if poll.Code != http.StatusOK {
		t.Fatalf("poll status = %d body=%s", poll.Code, poll.Body.String())
	}
	diag := getJSON(t, router, "/api/debug/agents", "debug-secret")
	if diag.Code != http.StatusOK {
		t.Fatalf("diagnostics after poll status = %d body=%s", diag.Code, diag.Body.String())
	}
	var got struct {
		Agents []struct {
			AgentSessionID     string `json:"agent_session_id"`
			MachineID          string `json:"machine_id"`
			PollCount          int    `json:"poll_count"`
			LastOfferSessionID string `json:"last_offer_session_id"`
		} `json:"agents"`
	}
	decodeJSON(t, diag, &got)
	if len(got.Agents) != 1 || got.Agents[0].AgentSessionID != registered.AgentSessionID ||
		got.Agents[0].PollCount != 1 || got.Agents[0].LastOfferSessionID != "rtc_agent_diag" {
		t.Fatalf("diagnostics after poll = %+v", got)
	}
}

func TestAgentHTTPRejectsRuntimePayloadAsAnswer(t *testing.T) {
	t.Parallel()

	clock := fixedClock(time.Date(2026, 5, 3, 11, 45, 0, 0, time.UTC))
	verifier := &fakeTicketVerifier{now: clock.Now(), tickets: map[string]managed.Ticket{
		"ticket_allowed": {
			ID:         "ticket_allowed",
			MachineID:  "device_1",
			TerminalID: "term_1",
			Path:       managed.PathManaged,
			ExpiresAt:  clock.Now().Add(time.Minute),
		},
	}}
	reg := registry.New(registry.Config{Clock: clock, Verifier: verifier})
	svc := managed.NewService(managed.Config{Registry: reg, Tickets: verifier, Clock: clock})
	router := httpapi.NewHandler(httpapi.Config{Managed: svc, Registry: reg})
	register := postJSON(t, router, "/api/v1/agents/register", map[string]any{
		"device_id": "device_1",
	})
	if register.Code != http.StatusOK {
		t.Fatalf("register status = %d body=%s", register.Code, register.Body.String())
	}
	var registered struct {
		AgentSessionID string `json:"agent_session_id"`
	}
	decodeJSON(t, register, &registered)

	go func() {
		_ = postJSON(t, router, "/api/v1/sessions", validManagedSessionRequestWithIDs("device_1", "term_1", "rtc_agent_bad_answer"))
	}()
	poll := postJSON(t, router, "/api/v1/agents/signaling/poll", map[string]any{
		"agent_session_id": registered.AgentSessionID,
		"device_id":        "device_1",
		"timeout_seconds":  1,
	})
	if poll.Code != http.StatusOK {
		t.Fatalf("poll status = %d body=%s", poll.Code, poll.Body.String())
	}
	answer := postJSON(t, router, "/api/v1/agents/signaling/answer", map[string]any{
		"agent_session_id": registered.AgentSessionID,
		"device_id":        "device_1",
		"answer": map[string]any{
			"session_id": "rtc_agent_bad_answer",
			"sdp":        "v=0\r\nterminal_data=must-not-forward",
		},
	})
	if answer.Code != http.StatusForbidden {
		t.Fatalf("runtime answer status = %d body=%s", answer.Code, answer.Body.String())
	}
}
