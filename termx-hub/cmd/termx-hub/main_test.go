package main

import (
	"context"
	"encoding/json"
	"os"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	hubheartbeat "github.com/lozzow/termx/termx-remote/hub/heartbeat"
	"github.com/lozzow/termx/termx-remote/hub/registry"
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

func TestHeartbeatConfigFromEnvBuildsManagementConfig(t *testing.T) {
	t.Setenv("TERMX_HUB_ID", "hub-env")
	t.Setenv("TERMX_HUB_CONTROL_URL", "https://control.example.test")
	t.Setenv("TERMX_HUB_CONTROL_SECRET", "control-secret")
	t.Setenv("TERMX_HUB_PUBLIC_HTTP_URL", "https://hub.example.test")
	t.Setenv("TERMX_HUB_NAME", "iad-1")
	t.Setenv("TERMX_HUB_REGION", "iad")
	t.Setenv("TERMX_HUB_HEARTBEAT_INTERVAL", "250ms")
	t.Setenv("TERMX_HUB_MAX_AGENTS", "42")
	t.Setenv("TERMX_HUB_BANDWIDTH_MBPS", "1000.5")
	t.Setenv("TERMX_HUB_CPU_CORES", "8")
	t.Setenv("TERMX_HUB_MEMORY_GB", "16")

	cfg := heartbeatConfigFromEnv()
	if !cfg.Enabled() {
		t.Fatalf("heartbeat config should be enabled: %+v", cfg)
	}
	if cfg.ControlURL != "https://control.example.test" || cfg.Secret != "control-secret" ||
		cfg.HubID != "hub-env" || cfg.HTTPURL != "https://hub.example.test" ||
		cfg.Name != "iad-1" || cfg.Region != "iad" || cfg.Interval != 250*time.Millisecond ||
		cfg.MaxAgents != 42 || cfg.BandwidthMbps != 1000.5 || cfg.CPUCores != 8 || cfg.MemoryGB != 16 {
		t.Fatalf("unexpected heartbeat config: %+v", cfg)
	}
}

func TestStartHubHeartbeatLoopPostsAtLeastOnce(t *testing.T) {
	var posts atomic.Int32
	posted := make(chan struct{}, 1)
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		var body struct {
			HubID    string   `json:"hub_id"`
			AgentIDs []string `json:"agent_ids"`
			Static   struct {
				HTTPURL string `json:"http_url"`
			} `json:"static"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode heartbeat body: %v", err)
		}
		if body.HubID != "hub-loop" || body.Static.HTTPURL != "https://hub.example.test" ||
			len(body.AgentIDs) != 1 || body.AgentIDs[0] != "machine_loop" {
			t.Fatalf("unexpected heartbeat body: %+v", body)
		}
		select {
		case posted <- struct{}{}:
		default:
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer control.Close()

	reg := registry.New(registry.Config{})
	if _, err := reg.Register(context.Background(), registry.RegisterInput{
		MachineID: "machine_loop",
		AgentID:   "agent_loop",
	}); err != nil {
		t.Fatalf("register agent: %v", err)
	}

	stop := startHubHeartbeatLoop(hubheartbeat.Config{
		ControlURL: control.URL,
		Secret:     "hub-secret",
		HubID:      "hub-loop",
		HTTPURL:    "https://hub.example.test",
		Interval:   time.Hour,
		Client:     control.Client(),
	}, reg)
	defer stop()

	select {
	case <-posted:
	case <-time.After(time.Second):
		t.Fatal("heartbeat loop did not post")
	}
	if posts.Load() < 1 {
		t.Fatal("heartbeat loop did not reach control server")
	}
}

func TestDisabledHubHeartbeatLoopDoesNotPost(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  hubheartbeat.Config
	}{
		{
			name: "missing control url",
			cfg: hubheartbeat.Config{
				Secret:   "hub-secret",
				HubID:    "hub-disabled",
				HTTPURL:  "https://hub.example.test",
				Interval: time.Millisecond,
			},
		},
		{
			name: "missing secret",
			cfg: hubheartbeat.Config{
				ControlURL: "https://control.example.test",
				HubID:      "hub-disabled",
				HTTPURL:    "https://hub.example.test",
				Interval:   time.Millisecond,
			},
		},
		{
			name: "missing public http url",
			cfg: hubheartbeat.Config{
				ControlURL: "https://control.example.test",
				Secret:     "hub-secret",
				HubID:      "hub-disabled",
				Interval:   time.Millisecond,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var posts atomic.Int32
			control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				posts.Add(1)
				t.Fatalf("unexpected heartbeat request to %s", r.URL.Path)
			}))
			defer control.Close()

			cfg := tc.cfg
			if cfg.ControlURL == "https://control.example.test" {
				cfg.ControlURL = control.URL
			}
			cfg.Client = control.Client()
			stop := startHubHeartbeatLoop(cfg, registry.New(registry.Config{}))
			defer stop()

			time.Sleep(50 * time.Millisecond)
			if got := posts.Load(); got != 0 {
				t.Fatalf("disabled heartbeat posted %d time(s)", got)
			}
		})
	}
}

func TestDeployEnvExampleDocumentsCloudHeartbeatConfig(t *testing.T) {
	content, err := os.ReadFile("../../deploy/termx-hub.env.example")
	if err != nil {
		t.Fatalf("read deploy env example: %v", err)
	}
	env := string(content)
	for _, name := range []string{
		"TERMX_HUB_ID",
		"TERMX_HUB_NAME",
		"TERMX_HUB_PUBLIC_HTTP_URL",
		"TERMX_HUB_CONTROL_URL",
		"TERMX_HUB_CONTROL_SECRET",
		"TERMX_HUB_TURN_SERVERS",
		"TERMX_HUB_TURN_SHARED_SECRET",
		"TERMX_HUB_TURN_SECRET",
		"TERMX_HUB_TURN_ADDR",
		"TERMX_HUB_TURN_PUBLIC_IP",
	} {
		if !strings.Contains(env, name+"=") {
			t.Fatalf("%s missing from deploy env example:\n%s", name, env)
		}
		commentToken := "# " + name
		if !strings.Contains(env, commentToken) {
			t.Fatalf("%s is missing an explanatory comment token %q", name, commentToken)
		}
	}
	if !strings.Contains(env, "HUB_SECRET") {
		t.Fatalf("control secret comment must mention matching web-control HUB_SECRET:\n%s", env)
	}
	if !strings.Contains(env, "curl http://127.0.0.1:8447/api/health") {
		t.Fatalf("deploy env example should include a minimal health check command:\n%s", env)
	}
	for _, line := range strings.Split(env, "\n") {
		if strings.Contains(line, "=") && !strings.HasPrefix(line, "#") &&
			strings.Contains(line, " ") && !strings.Contains(line, "\"") && !strings.Contains(line, "'") {
			t.Fatalf("env assignment with spaces must be quoted so the file can be sourced: %q", line)
		}
	}
	readme, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	if !strings.Contains(string(readme), "/etc/systemd/system/termx-hub.service") {
		t.Fatalf("README must document installing the systemd unit file")
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

func TestNewHubRuntimeFromEnvExposesEmbeddedTurnTrafficReader(t *testing.T) {
	t.Setenv("TERMX_HUB_TURN_SECRET", "embedded-secret")
	t.Setenv("TERMX_HUB_TURN_ADDR", "127.0.0.1:0")
	t.Setenv("TERMX_HUB_TURN_PUBLIC_IP", "127.0.0.1")

	runtime, err := newHubRuntimeFromEnv()
	if err != nil {
		t.Fatalf("new hub runtime: %v", err)
	}
	defer runtime.Cleanup()
	if runtime.Handler == nil || runtime.Registry == nil {
		t.Fatalf("runtime missing handler or registry: %+v", runtime)
	}
	if runtime.TrafficReader == nil {
		t.Fatal("embedded TURN runtime did not expose heartbeat traffic reader")
	}
	if traffic := runtime.TrafficReader.DrainTraffic(); len(traffic) != 0 {
		t.Fatalf("new runtime should not report fabricated relay traffic: %+v", traffic)
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
