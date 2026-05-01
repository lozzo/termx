package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	termx "github.com/lozzow/termx/termx-core"
	"github.com/lozzow/termx/termx-core/protocol"
	"github.com/lozzow/termx/tuiv2/shared"
)

func TestRootCmdRoutesToTUIv2ByDefault(t *testing.T) {
	oldInteractive := isInteractiveTerminal
	oldRunv2 := runTUIv2
	t.Cleanup(func() {
		isInteractiveTerminal = oldInteractive
		runTUIv2 = oldRunv2
	})

	isInteractiveTerminal = func() bool { return true }
	stateHome := t.TempDir()
	configHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("XDG_CONFIG_HOME", configHome)

	var gotCfg shared.Config
	called := false
	runTUIv2 = func(cfg shared.Config, stdin io.Reader, stdout io.Writer) error {
		called = true
		gotCfg = cfg
		return nil
	}

	cmd := newRootCmd()
	cmd.SetArgs([]string{"--log-file", filepath.Join(t.TempDir(), "termx.log")})
	cmd.SetIn(bytes.NewBuffer(nil))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !called {
		t.Fatal("expected runTUIv2 to be called")
	}
	if gotCfg.Workspace != "main" {
		t.Fatalf("expected workspace=main, got %q", gotCfg.Workspace)
	}
	if gotCfg.SessionID != "main" {
		t.Fatalf("expected session=main, got %q", gotCfg.SessionID)
	}
	if gotCfg.AttachID != "" {
		t.Fatalf("expected empty attach id for root command, got %q", gotCfg.AttachID)
	}
	if want := filepath.Join(stateHome, "termx", "workspace-state.json"); gotCfg.WorkspaceStatePath != want {
		t.Fatalf("expected workspace state path %q, got %q", want, gotCfg.WorkspaceStatePath)
	}
	if want := filepath.Join(configHome, "termx", "termx.yaml"); gotCfg.ConfigPath != want {
		t.Fatalf("expected config path %q, got %q", want, gotCfg.ConfigPath)
	}
	if _, err := os.Stat(gotCfg.ConfigPath); err != nil {
		t.Fatalf("expected default config file to be created: %v", err)
	}
}

func TestRootCmdBlocksNestedTUIByDefault(t *testing.T) {
	oldInteractive := isInteractiveTerminal
	oldRunv2 := runTUIv2
	t.Cleanup(func() {
		isInteractiveTerminal = oldInteractive
		runTUIv2 = oldRunv2
	})

	isInteractiveTerminal = func() bool { return true }
	runTUIv2 = func(cfg shared.Config, stdin io.Reader, stdout io.Writer) error {
		t.Fatal("runTUIv2 should not be called when nested TUI is blocked")
		return nil
	}

	t.Setenv("TERMX", "1")
	t.Setenv("TERMX_ALLOW_NESTED", "")

	cmd := newRootCmd()
	cmd.SetArgs([]string{"--log-file", filepath.Join(t.TempDir(), "termx.log")})
	cmd.SetIn(bytes.NewBuffer(nil))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "refusing to start termx TUI inside a termx-managed terminal") {
		t.Fatalf("expected nested TUI rejection, got %v", err)
	}
}

func TestAttachCmdRoutesToTUIv2WithAttachID(t *testing.T) {
	oldRunv2 := runTUIv2
	t.Cleanup(func() {
		runTUIv2 = oldRunv2
	})

	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)

	var gotCfg shared.Config
	called := false
	runTUIv2 = func(cfg shared.Config, stdin io.Reader, stdout io.Writer) error {
		called = true
		gotCfg = cfg
		return nil
	}

	cmd := newRootCmd()
	cmd.SetArgs([]string{"attach", "term-001"})
	cmd.SetIn(bytes.NewBuffer(nil))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected attach command to succeed, got %v", err)
	}
	if !called {
		t.Fatal("expected attach command to call runTUIv2")
	}
	if gotCfg.AttachID != "term-001" {
		t.Fatalf("expected attach id term-001, got %q", gotCfg.AttachID)
	}
	if gotCfg.SessionID != "main" {
		t.Fatalf("expected attach command session main, got %q", gotCfg.SessionID)
	}
	if gotCfg.WorkspaceStatePath != "" {
		t.Fatalf("expected attach command to avoid workspace persistence path, got %q", gotCfg.WorkspaceStatePath)
	}
	if want := filepath.Join(configHome, "termx", "termx.yaml"); gotCfg.ConfigPath != want {
		t.Fatalf("expected config path %q, got %q", want, gotCfg.ConfigPath)
	}
}

func TestAttachCmdAllowsNestedTUIWhenOverrideIsSet(t *testing.T) {
	oldRunv2 := runTUIv2
	t.Cleanup(func() {
		runTUIv2 = oldRunv2
	})

	called := false
	runTUIv2 = func(cfg shared.Config, stdin io.Reader, stdout io.Writer) error {
		called = true
		return nil
	}

	t.Setenv("TERMX", "1")
	t.Setenv("TERMX_ALLOW_NESTED", "1")

	cmd := newRootCmd()
	cmd.SetArgs([]string{"attach", "term-001"})
	cmd.SetIn(bytes.NewBuffer(nil))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected attach override to succeed, got %v", err)
	}
	if !called {
		t.Fatal("expected attach command to reach runTUIv2 when override is set")
	}
}

func TestAttachCmdBlocksNestedTUIByDefault(t *testing.T) {
	oldRunv2 := runTUIv2
	t.Cleanup(func() {
		runTUIv2 = oldRunv2
	})

	runTUIv2 = func(cfg shared.Config, stdin io.Reader, stdout io.Writer) error {
		t.Fatal("runTUIv2 should not be called when nested attach is blocked")
		return nil
	}

	t.Setenv("TERMX", "1")
	t.Setenv("TERMX_ALLOW_NESTED", "")

	cmd := newRootCmd()
	cmd.SetArgs([]string{"attach", "term-001"})
	cmd.SetIn(bytes.NewBuffer(nil))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "refusing to start termx TUI inside a termx-managed terminal") {
		t.Fatalf("expected nested attach rejection, got %v", err)
	}
}

func TestRootCmdUsesExplicitConfigPath(t *testing.T) {
	oldInteractive := isInteractiveTerminal
	oldRunv2 := runTUIv2
	t.Cleanup(func() {
		isInteractiveTerminal = oldInteractive
		runTUIv2 = oldRunv2
	})

	isInteractiveTerminal = func() bool { return true }
	configPath := filepath.Join(t.TempDir(), "custom-termx.yaml")

	var gotCfg shared.Config
	runTUIv2 = func(cfg shared.Config, stdin io.Reader, stdout io.Writer) error {
		gotCfg = cfg
		return nil
	}

	cmd := newRootCmd()
	cmd.SetArgs([]string{"--config", configPath})
	cmd.SetIn(bytes.NewBuffer(nil))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if gotCfg.ConfigPath != configPath {
		t.Fatalf("expected explicit config path %q, got %q", configPath, gotCfg.ConfigPath)
	}
}

func TestRemoteConfigFromEnv(t *testing.T) {
	t.Setenv("TERMX_REMOTE_ENABLE", "true")
	t.Setenv("TERMX_REMOTE_CONTROL_URL", "https://control.example.test")
	t.Setenv("TERMX_REMOTE_HUB_URL", "https://hub.example.test")
	t.Setenv("TERMX_REMOTE_ACCESS_TOKEN", "secret")
	t.Setenv("TERMX_REMOTE_DATA_DIR", "/tmp/termx-remote")
	t.Setenv("TERMX_REMOTE_DEVICE_NAME", "device-a")

	cfg := remoteConfigFromEnv()
	if !cfg.Enabled {
		t.Fatal("expected remote config to be enabled")
	}
	if cfg.ControlURL != "https://control.example.test" {
		t.Fatalf("unexpected control url: %q", cfg.ControlURL)
	}
	if cfg.HubURL != "https://hub.example.test" {
		t.Fatalf("unexpected hub url: %q", cfg.HubURL)
	}
	if cfg.AccessToken != "secret" {
		t.Fatalf("unexpected access token: %q", cfg.AccessToken)
	}
	if cfg.DataDir != "/tmp/termx-remote" {
		t.Fatalf("unexpected data dir: %q", cfg.DataDir)
	}
	if cfg.DeviceName != "device-a" {
		t.Fatalf("unexpected device name: %q", cfg.DeviceName)
	}
}

func TestRemoteConfigFromEnvAutoEnablesWhenRemoteFieldsExist(t *testing.T) {
	t.Setenv("TERMX_REMOTE_ENABLE", "")
	t.Setenv("TERMX_REMOTE_CONTROL_URL", "https://control.example.test")

	cfg := remoteConfigFromEnv()
	if !cfg.Enabled {
		t.Fatal("expected remote config to auto-enable when remote fields exist")
	}
}

func TestRemoteLocalWebAddrFromEnv(t *testing.T) {
	t.Setenv("TERMX_REMOTE_LOCAL_WEB_ENABLE", "")
	t.Setenv("TERMX_REMOTE_LOCAL_WEB_ADDR", "")
	if got := remoteLocalWebAddrFromEnv(); got != "" {
		t.Fatalf("expected local web to be disabled by default, got %q", got)
	}

	t.Setenv("TERMX_REMOTE_LOCAL_WEB_ENABLE", "true")
	if got := remoteLocalWebAddrFromEnv(); got != "127.0.0.1:18888" {
		t.Fatalf("expected default local web addr, got %q", got)
	}

	t.Setenv("TERMX_REMOTE_LOCAL_WEB_ADDR", "127.0.0.1:19999")
	if got := remoteLocalWebAddrFromEnv(); got != "127.0.0.1:19999" {
		t.Fatalf("expected explicit local web addr, got %q", got)
	}
}

func TestRemoteLocalICETCPAddrFromEnv(t *testing.T) {
	t.Setenv("TERMX_REMOTE_LOCAL_ICE_TCP_ENABLE", "")
	t.Setenv("TERMX_REMOTE_LOCAL_ICE_TCP_ADDR", "")
	if got := remoteLocalICETCPAddrFromEnv(); got != "" {
		t.Fatalf("expected local ICE TCP to be disabled by default, got %q", got)
	}

	t.Setenv("TERMX_REMOTE_LOCAL_ICE_TCP_ENABLE", "true")
	if got := remoteLocalICETCPAddrFromEnv(); got != "127.0.0.1:18889" {
		t.Fatalf("expected default ICE TCP addr, got %q", got)
	}

	t.Setenv("TERMX_REMOTE_LOCAL_ICE_TCP_ADDR", "127.0.0.1:0")
	if got := remoteLocalICETCPAddrFromEnv(); got != "127.0.0.1:0" {
		t.Fatalf("expected explicit ICE TCP addr, got %q", got)
	}
}

func TestStartRemoteLocalWebServesEmbeddedPageAndStatus(t *testing.T) {
	srv := termx.NewServer(termx.WithRemoteConfig(termx.RemoteConfig{
		Enabled:    true,
		DataDir:    t.TempDir(),
		DeviceName: "cli-local-web",
	}))
	defer srv.Shutdown(context.Background())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	iceMux, err := termx.StartLocalICETCPMux(ctx, "127.0.0.1:0")
	if err != nil {
		t.Fatalf("StartLocalICETCPMux returned error: %v", err)
	}
	defer iceMux.Close()

	baseURL, shutdown, err := startRemoteLocalWeb(ctx, srv, "127.0.0.1:0", iceMux, nil)
	if err != nil {
		t.Fatalf("startRemoteLocalWeb returned error: %v", err)
	}
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer shutdownCancel()
		if err := shutdown(shutdownCtx); err != nil {
			t.Fatalf("shutdown local web: %v", err)
		}
	}()

	client := http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(baseURL + "/")
	if err != nil {
		t.Fatalf("GET embedded page: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read embedded page: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected embedded page status 200, got %d: %s", resp.StatusCode, string(body))
	}
	if !strings.Contains(string(body), "TermX Remote") {
		t.Fatalf("expected embedded TermX Remote page, got %s", string(body))
	}

	resp, err = client.Get(baseURL + "/api/local/status")
	if err != nil {
		t.Fatalf("GET local status: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected local status 200, got %d: %s", resp.StatusCode, string(body))
	}
	var status map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("decode local status: %v", err)
	}
	if machineID, _ := status["machine_id"].(string); machineID == "" {
		t.Fatalf("expected local status machine_id, got %#v", status)
	}
	if _, ok := status["device_id"]; ok {
		t.Fatalf("local status must not expose legacy device_id: %#v", status)
	}
	localRTC, ok := status["local_rtc"].(map[string]any)
	if !ok {
		t.Fatalf("expected local_rtc status, got %#v", status)
	}
	if localRTC["ice_tcp_enabled"] != true || int(localRTC["ice_tcp_port"].(float64)) == 0 {
		t.Fatalf("expected ICE TCP endpoint in local status, got %#v", localRTC)
	}
	rawStatus, _ := json.Marshal(status)
	if strings.Contains(strings.ToLower(string(rawStatus)), "turn:") || strings.Contains(strings.ToLower(string(rawStatus)), "turns:") {
		t.Fatalf("local status must not expose TURN credentials: %s", rawStatus)
	}

	resp, err = client.Get(baseURL + "/api/local/terminals")
	if err != nil {
		t.Fatalf("GET local terminals: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected local terminals 200, got %d: %s", resp.StatusCode, string(body))
	}
	var terminals map[string][]map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&terminals); err != nil {
		t.Fatalf("decode local terminals: %v", err)
	}
	if len(terminals["terminals"]) != 0 {
		t.Fatalf("expected no terminals on fresh CLI server, got %#v", terminals)
	}

	session, err := srv.RemotePairStart(termx.PairStartOptions{
		LocalPairURL: baseURL + "/api/local/pair",
		TTL:          time.Minute,
	})
	if err != nil {
		t.Fatalf("RemotePairStart returned error: %v", err)
	}
	appPublic, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey returned error: %v", err)
	}
	pairPayload := strings.NewReader(`{
		"pair_session_id":"` + session.PairSessionID + `",
		"pair_secret":"` + session.PairSecret + `",
		"app_device_id":"app-cli-local-web",
		"app_name":"TermX CLI Local Web Test",
		"app_public_key":"` + base64.StdEncoding.EncodeToString(appPublic) + `",
		"requested_capabilities":["terminal","file_manager"]
	}`)
	resp, err = client.Post(baseURL+"/api/local/pair", "application/json", pairPayload)
	if err != nil {
		t.Fatalf("POST local pair: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected local pair 200, got %d: %s", resp.StatusCode, string(body))
	}
	var pair map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&pair); err != nil {
		t.Fatalf("decode local pair: %v", err)
	}
	if pair["machine_id"] != session.MachineID {
		t.Fatalf("expected pair machine_id %q, got %#v", session.MachineID, pair)
	}
	if pair["machine_public_key_fingerprint"] != session.MachinePublicKeyFingerprint {
		t.Fatalf("expected machine fingerprint %q, got %#v", session.MachinePublicKeyFingerprint, pair)
	}
	if pair["app_certificate"] == nil || pair["machine_private_key"] != nil {
		t.Fatalf("expected app certificate without machine private key, got %#v", pair)
	}
}

func TestRootCmdHasRemoteStatusAndPairCommands(t *testing.T) {
	cmd := newRootCmd()
	if _, _, err := cmd.Find([]string{"remote", "status"}); err != nil {
		t.Fatalf("expected remote status command to exist: %v", err)
	}
	if _, _, err := cmd.Find([]string{"pair"}); err != nil {
		t.Fatalf("expected pair command to exist: %v", err)
	}
}

func TestPairCmdEmitsJSONPairSession(t *testing.T) {
	oldPairStart := pairStartClient
	t.Cleanup(func() {
		pairStartClient = oldPairStart
	})

	var gotParams protocol.PairStartParams
	pairStartClient = func(ctx context.Context, socketPath string, logFile string, params protocol.PairStartParams) (*protocol.PairStartResult, error) {
		gotParams = params
		return &protocol.PairStartResult{
			Type:                        "termx_pair_v1",
			MachineID:                   "mach_test",
			MachineName:                 "MacBook Pro",
			MachinePublicKeyFingerprint: "sha256:test",
			LocalPairURL:                params.LocalPairURL,
			PairSessionID:               "pair_test",
			PairSecret:                  "secret",
			ExpiresAt:                   time.Date(2026, 5, 1, 0, 5, 0, 0, time.UTC),
		}, nil
	}

	var out bytes.Buffer
	cmd := newRootCmd()
	cmd.SetArgs([]string{
		"--socket", filepath.Join(t.TempDir(), "termx.sock"),
		"pair",
		"--local-url", "http://127.0.0.1:18888/api/local/pair",
		"--ttl", "5m",
		"--json",
	})
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if gotParams.LocalPairURL != "http://127.0.0.1:18888/api/local/pair" {
		t.Fatalf("unexpected local pair url param %q", gotParams.LocalPairURL)
	}
	if gotParams.TTLSeconds != 300 {
		t.Fatalf("expected ttl seconds 300, got %d", gotParams.TTLSeconds)
	}

	var decoded protocol.PairStartResult
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("pair output is not JSON: %v\n%s", err, out.String())
	}
	if decoded.MachineID != "mach_test" || decoded.PairSessionID != "pair_test" {
		t.Fatalf("unexpected pair output: %#v", decoded)
	}
}
