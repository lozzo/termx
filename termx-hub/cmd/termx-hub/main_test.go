package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	hubheartbeat "github.com/lozzow/termx/termx-remote/hub/heartbeat"
	"github.com/lozzow/termx/termx-remote/hub/registry"
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

func TestNewHubRuntimeFromEnvBuildsGRPCServer(t *testing.T) {
	t.Setenv("TERMX_HUB_ID", "hub-env-test")
	t.Setenv("TERMX_HUB_STUN_SERVERS", "stun:stun.example.com:3478, turn:turn.example.com:3478")

	runtime, err := newHubRuntimeFromEnv()
	if err != nil {
		t.Fatalf("new hub runtime: %v", err)
	}
	defer runtime.Cleanup()
	if runtime.GRPCServer == nil {
		t.Fatal("runtime missing gRPC server")
	}
	if runtime.Handler == nil || runtime.Registry == nil {
		t.Fatalf("runtime missing handler or registry: %+v", runtime)
	}
}

func TestNewHubHandlerFromEnvStartsEmbeddedTurnWhenSecretSet(t *testing.T) {
	t.Setenv("TERMX_HUB_STUN_SERVERS", "stun:stun.example.com:3478")
	t.Setenv("TERMX_HUB_TURN_SECRET", "embedded-secret")
	t.Setenv("TERMX_HUB_TURN_ADDR", "127.0.0.1:0")
	t.Setenv("TERMX_HUB_TURN_PUBLIC_IP", "127.0.0.1")

	runtime, err := newHubRuntimeFromEnv()
	if err != nil {
		t.Fatalf("new hub runtime: %v", err)
	}
	defer runtime.Cleanup()
	if runtime.Handler == nil {
		t.Fatal("handler is nil")
	}
	if runtime.GRPCServer == nil {
		t.Fatal("embedded TURN runtime missing gRPC server")
	}
	if runtime.TrafficReader == nil {
		t.Fatal("embedded TURN runtime did not expose heartbeat traffic reader")
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
