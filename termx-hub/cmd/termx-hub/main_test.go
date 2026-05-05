package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	hubv1 "github.com/lozzow/termx/termx-remote/protocol/hubv1"
)

func TestNewHubHandlerFromEnvBuildsRunnableDumbRelayWithoutControlConfiguration(t *testing.T) {
	handler, _, cleanup, err := newHubHandlerFromEnv()
	if err != nil {
		t.Fatalf("new hub handler: %v", err)
	}
	defer cleanup()
	if handler == nil {
		t.Fatal("handler is nil")
	}
	if _, ok := handler.(interface {
		StartCleanup(context.Context, <-chan time.Time)
	}); !ok {
		t.Fatal("handler does not expose cleanup loop for bounded hub memory")
	}
	req, err := http.NewRequest(http.MethodGet, "/api/health", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("health status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHubHandlerCleanupLoopRemovesExpiredRegistryState(t *testing.T) {
	handler, _, cleanup, err := newHubHandlerFromEnv()
	if err != nil {
		t.Fatalf("new hub handler: %v", err)
	}
	defer cleanup()
	cleaner, ok := handler.(interface {
		StartCleanup(context.Context, <-chan time.Time)
	})
	if !ok {
		t.Fatal("handler does not expose cleanup")
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ticks := make(chan time.Time, 1)
	done := make(chan struct{})
	go func() {
		cleaner.StartCleanup(ctx, ticks)
		close(done)
	}()
	ticks <- time.Date(2026, 5, 3, 18, 25, 0, 0, time.UTC)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cleanup loop did not stop after context cancellation")
	}
}

func TestNewHubHandlerFromEnvPassesSTUNServersToAgentRegister(t *testing.T) {
	t.Setenv("TERMX_HUB_ID", "hub-env-test")
	t.Setenv("TERMX_HUB_STUN_SERVERS", "stun:stun.example.com:3478, turn:turn.example.com:3478")

	handler, _, cleanup, err := newHubHandlerFromEnv()
	if err != nil {
		t.Fatalf("new hub handler: %v", err)
	}
	defer cleanup()
	reqBody := `{
		"version":"remote.hub.v1",
		"device_id":"machine_1",
		"agent_id":"agent-main-test",
		"display_name":"Machine 1",
		"terminals":[{"id":"terminal_1","state":"running"}]
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/register", strings.NewReader(reqBody))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("agent register status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp hubv1.HubRegisterResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode register response: %v", err)
	}
	if len(resp.RTCConfig.IceServers) != 1 || resp.RTCConfig.IceServers[0].URLs[0] != "stun:stun.example.com:3478" {
		t.Fatalf("unexpected ICE servers: %+v", resp.RTCConfig.IceServers)
	}
	if resp.HubID != "hub-env-test" {
		t.Fatalf("register response hub id = %q", resp.HubID)
	}
	if resp.RelayPolicy.AllowRelay {
		t.Fatalf("STUN-only env unexpectedly enabled relay policy: %+v", resp.RelayPolicy)
	}
}

func TestNewHubHandlerFromEnvWiresCloudTurnWithoutRegistrationRelayPolicy(t *testing.T) {
	t.Setenv("TERMX_HUB_STUN_SERVERS", "stun:stun.example.com:3478")
	t.Setenv("TERMX_HUB_TURN_SERVERS", "turn:turn.example.com:3478?transport=udp")
	t.Setenv("TERMX_HUB_TURN_SHARED_SECRET", "turn-secret")

	handler, _, cleanup, err := newHubHandlerFromEnv()
	if err != nil {
		t.Fatalf("new hub handler: %v", err)
	}
	defer cleanup()
	register := httptest.NewRecorder()
	registerReq := httptest.NewRequest(http.MethodPost, "/api/v1/agents/register", strings.NewReader(`{"device_id":"machine_1","agent_id":"agent_1","terminals":[{"id":"term_1","state":"running"}]}`))
	registerReq.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(register, registerReq)
	if register.Code != http.StatusOK {
		t.Fatalf("agent register status = %d body=%s", register.Code, register.Body.String())
	}
	var resp hubv1.HubRegisterResponse
	if err := json.NewDecoder(register.Body).Decode(&resp); err != nil {
		t.Fatalf("decode register response: %v", err)
	}
	if resp.RelayPolicy.AllowRelay || len(resp.RTCConfig.IceServers) != 1 ||
		strings.HasPrefix(resp.RTCConfig.IceServers[0].URLs[0], "turn:") {
		t.Fatalf("registration exposed cloud relay policy: %+v", resp)
	}
}

func TestNewHubHandlerFromEnvStartsEmbeddedTurnWhenSecretSet(t *testing.T) {
	t.Setenv("TERMX_HUB_STUN_SERVERS", "stun:stun.example.com:3478")
	t.Setenv("TERMX_HUB_TURN_SECRET", "embedded-secret")
	t.Setenv("TERMX_HUB_TURN_ADDR", "127.0.0.1:0")
	t.Setenv("TERMX_HUB_TURN_PUBLIC_IP", "127.0.0.1")

	handler, _, cleanup, err := newHubHandlerFromEnv()
	if err != nil {
		t.Fatalf("new hub handler: %v", err)
	}
	defer cleanup()
	if handler == nil {
		t.Fatal("handler is nil")
	}

	register := httptest.NewRecorder()
	handler.ServeHTTP(register, httptest.NewRequest(http.MethodPost, "/api/v1/agents/register", strings.NewReader(`{"device_id":"machine_1","agent_id":"agent_1"}`)))
	if register.Code != http.StatusOK {
		t.Fatalf("agent register status = %d body=%s", register.Code, register.Body.String())
	}
	sessionDone := make(chan int, 1)
	go func() {
		resp := httptest.NewRecorder()
		body := `{"connect_ticket":"ticket_1","machine_id":"machine_1","terminal_id":"term_1","app_certificate":{"payload":{"machine_id":"machine_1"},"signature":"cert"},"offer":{"session_id":"session_1","sdp":"v=0\r\no=- offer 1 IN IP4 127.0.0.1\r\ns=-\r\nt=0 0\r\nm=application 9 UDP/DTLS/SCTP webrtc-datachannel","ice_candidates":[]},"signature":{"algorithm":"ed25519","nonce":"nonce","timestamp":1770000000,"value":"sig"}}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		handler.ServeHTTP(resp, req)
		sessionDone <- resp.Code
	}()
	poll := httptest.NewRecorder()
	pollBody := `{"agent_session_id":"agent-session-1","device_id":"machine_1","timeout_seconds":1}`
	var regResp hubv1.HubRegisterResponse
	if err := json.NewDecoder(register.Body).Decode(&regResp); err != nil {
		t.Fatalf("decode register response: %v", err)
	}
	pollBody = strings.ReplaceAll(pollBody, "agent-session-1", regResp.AgentSessionID)
	pollReq := httptest.NewRequest(http.MethodPost, "/api/v1/agents/signaling/poll", strings.NewReader(pollBody))
	pollReq.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(poll, pollReq)
	if poll.Code != http.StatusOK {
		t.Fatalf("poll status = %d body=%s", poll.Code, poll.Body.String())
	}
	var polled hubv1.SignalingPollResponse
	if err := json.NewDecoder(poll.Body).Decode(&polled); err != nil {
		t.Fatalf("decode poll response: %v", err)
	}
	if polled.Offer == nil || !polled.Offer.AllowRelay {
		t.Fatalf("embedded turn should enable cloud relay offers: %+v", polled.Offer)
	}
	if len(polled.Offer.RTCConfig.IceServers) < 2 {
		t.Fatalf("expected STUN plus embedded TURN ICE servers: %+v", polled.Offer.RTCConfig.IceServers)
	}
	turnICE := polled.Offer.RTCConfig.IceServers[len(polled.Offer.RTCConfig.IceServers)-1]
	if len(turnICE.URLs) != 2 || !strings.HasPrefix(turnICE.URLs[0], "turn:") || !strings.Contains(turnICE.URLs[0], "transport=udp") ||
		!strings.Contains(turnICE.URLs[1], "transport=tcp") || turnICE.Username == "" || turnICE.Credential == "" {
		t.Fatalf("embedded turn ICE server = %+v", turnICE)
	}
	if code := <-sessionDone; code != http.StatusAccepted {
		t.Fatalf("pending session status = %d", code)
	}
}

func TestTurnServerFromEnvRejectsUnspecifiedAddressWithoutPublicIP(t *testing.T) {
	t.Setenv("TERMX_HUB_TURN_SECRET", "embedded-secret")
	t.Setenv("TERMX_HUB_TURN_ADDR", "0.0.0.0:0")

	server, err := turnServerFromEnv()
	if err == nil {
		if server != nil {
			_ = server.Close()
		}
		t.Fatal("expected public IP requirement for unspecified TURN listen address")
	}
}
