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

func TestRemoteConfigFromFileLoadsCloudBootstrapWithoutRawToken(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "termx.yaml")
	content := `remote:
  enabled: true
  controlURL: https://control-file.example.test
  hubURL: https://hub-file.example.test
  accessTokenEnv: TERMX_TEST_REMOTE_TOKEN
  accessToken: should-not-be-used
  dataDir: /tmp/termx-remote-file
  deviceName: file-device
`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	t.Setenv("TERMX_TEST_REMOTE_TOKEN", "file-secret")

	cfg, err := remoteConfigFromFileAndEnv(configPath)
	if err != nil {
		t.Fatalf("remoteConfigFromFileAndEnv returned error: %v", err)
	}
	if !cfg.Enabled {
		t.Fatal("expected remote config to be enabled from file")
	}
	if cfg.ControlURL != "https://control-file.example.test" {
		t.Fatalf("unexpected control url: %q", cfg.ControlURL)
	}
	if cfg.HubURL != "https://hub-file.example.test" {
		t.Fatalf("unexpected hub url: %q", cfg.HubURL)
	}
	if cfg.AccessToken != "file-secret" {
		t.Fatalf("expected token from env reference, got %q", cfg.AccessToken)
	}
	if strings.Contains(cfg.AccessToken, "should-not-be-used") {
		t.Fatal("raw accessToken from config must not be used")
	}
	if cfg.DataDir != "/tmp/termx-remote-file" {
		t.Fatalf("unexpected data dir: %q", cfg.DataDir)
	}
	if cfg.DeviceName != "file-device" {
		t.Fatalf("unexpected device name: %q", cfg.DeviceName)
	}
}

func TestRemoteConfigEnvOverridesFile(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "termx.yaml")
	content := `remote:
  enabled: true
  controlURL: https://control-file.example.test
  hubURL: https://hub-file.example.test
  accessTokenEnv: TERMX_TEST_REMOTE_TOKEN_FILE
  dataDir: /tmp/termx-remote-file
  deviceName: file-device
`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	t.Setenv("TERMX_TEST_REMOTE_TOKEN_FILE", "file-secret")
	t.Setenv("TERMX_REMOTE_CONTROL_URL", "https://control-env.example.test")
	t.Setenv("TERMX_REMOTE_HUB_URL", "https://hub-env.example.test")
	t.Setenv("TERMX_REMOTE_ACCESS_TOKEN", "env-secret")
	t.Setenv("TERMX_REMOTE_DATA_DIR", "/tmp/termx-remote-env")
	t.Setenv("TERMX_REMOTE_DEVICE_NAME", "env-device")

	cfg, err := remoteConfigFromFileAndEnv(configPath)
	if err != nil {
		t.Fatalf("remoteConfigFromFileAndEnv returned error: %v", err)
	}
	if cfg.ControlURL != "https://control-env.example.test" {
		t.Fatalf("expected env control url override, got %q", cfg.ControlURL)
	}
	if cfg.HubURL != "https://hub-env.example.test" {
		t.Fatalf("expected env hub url override, got %q", cfg.HubURL)
	}
	if cfg.AccessToken != "env-secret" {
		t.Fatalf("expected env token override, got %q", cfg.AccessToken)
	}
	if cfg.DataDir != "/tmp/termx-remote-env" {
		t.Fatalf("expected env data dir override, got %q", cfg.DataDir)
	}
	if cfg.DeviceName != "env-device" {
		t.Fatalf("expected env device name override, got %q", cfg.DeviceName)
	}
}

func TestRemoteConfigEnvCanDisableFileEnabledRemote(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "termx.yaml")
	content := `remote:
  enabled: true
  controlURL: https://control-file.example.test
  hubURL: https://hub-file.example.test
`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	t.Setenv("TERMX_REMOTE_ENABLE", "false")

	cfg, err := remoteConfigFromFileAndEnv(configPath)
	if err != nil {
		t.Fatalf("remoteConfigFromFileAndEnv returned error: %v", err)
	}
	if cfg.Enabled {
		t.Fatal("expected env false to disable file-enabled remote config")
	}
}

func TestRemoteConfigInvalidEnvEnableDoesNotAutoEnable(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "termx.yaml")
	content := `remote:
  controlURL: https://control-file.example.test
  hubURL: https://hub-file.example.test
`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	t.Setenv("TERMX_REMOTE_ENABLE", "flase")

	cfg, err := remoteConfigFromFileAndEnv(configPath)
	if err != nil {
		t.Fatalf("remoteConfigFromFileAndEnv returned error: %v", err)
	}
	if cfg.Enabled {
		t.Fatal("expected invalid env enable value to fail closed instead of auto-enabling")
	}
}

func TestRemoteConfigFromFileHonorsExplicitDisabledWithRemoteFields(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "termx.yaml")
	content := `remote:
  enabled: false
  controlURL: https://control-file.example.test
  hubURL: https://hub-file.example.test
  dataDir: /tmp/termx-remote-file
  deviceName: staged-device
`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	cfg, err := remoteConfigFromFileAndEnv(configPath)
	if err != nil {
		t.Fatalf("remoteConfigFromFileAndEnv returned error: %v", err)
	}
	if cfg.Enabled {
		t.Fatal("expected file enabled false to keep staged remote config disabled")
	}
	if cfg.ControlURL != "https://control-file.example.test" {
		t.Fatalf("expected staged control url to remain available, got %q", cfg.ControlURL)
	}
}

func TestRemoteConfigFromFileRejectsInvalidEnabledValue(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "termx.yaml")
	content := `remote:
  enabled: flase
  controlURL: https://control-file.example.test
`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	_, err := remoteConfigFromFileAndEnv(configPath)
	if err == nil || !strings.Contains(err.Error(), "enabled") {
		t.Fatalf("expected invalid enabled value error, got %v", err)
	}
}

func TestRemoteConfigFromFileRejectsMalformedRemoteSection(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "termx.yaml")
	content := `remote:
  enabled true
  controlURL: https://control-file.example.test
`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	_, err := remoteConfigFromFileAndEnv(configPath)
	if err == nil || !strings.Contains(err.Error(), "remote config") {
		t.Fatalf("expected malformed remote config error, got %v", err)
	}
}

func TestDaemonCommandUsesRootConfigForRemoteBootstrap(t *testing.T) {
	oldLoader := remoteConfigLoader
	oldNewServer := newServer
	oldWithRemoteConfig := withRemoteConfig
	t.Cleanup(func() {
		remoteConfigLoader = oldLoader
		newServer = oldNewServer
		withRemoteConfig = oldWithRemoteConfig
	})

	configPath := filepath.Join(t.TempDir(), "termx.yaml")
	var gotConfigPath string
	var gotRemoteConfig termx.RemoteConfig
	remoteConfigLoader = func(path string) (termx.RemoteConfig, error) {
		gotConfigPath = path
		return termx.RemoteConfig{
			Enabled:     true,
			ControlURL:  "https://control-config.example.test",
			HubURL:      "https://hub-config.example.test",
			AccessToken: "loader-secret",
			DataDir:     t.TempDir(),
			DeviceName:  "config-device",
		}, nil
	}
	withRemoteConfig = func(cfg termx.RemoteConfig) termx.ServerOption {
		gotRemoteConfig = cfg
		return termx.WithRemoteConfig(termx.RemoteConfig{})
	}
	fake := &fakeTermxServer{}
	newServer = func(opts ...termx.ServerOption) termxServer {
		fake.newServerCalls++
		return fake
	}

	cmd := newRootCmd()
	cmd.SetArgs([]string{"--config", configPath, "daemon", "--log-file", filepath.Join(t.TempDir(), "termx.log")})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if gotConfigPath != configPath {
		t.Fatalf("expected daemon to load root config path %q, got %q", configPath, gotConfigPath)
	}
	if !gotRemoteConfig.Enabled ||
		gotRemoteConfig.ControlURL != "https://control-config.example.test" ||
		gotRemoteConfig.HubURL != "https://hub-config.example.test" ||
		gotRemoteConfig.AccessToken != "loader-secret" ||
		gotRemoteConfig.DataDir == "" ||
		gotRemoteConfig.DeviceName != "config-device" {
		t.Fatalf("daemon did not pass loaded remote config to server option: %#v", gotRemoteConfig)
	}
	if fake.newServerCalls != 1 || fake.listenCalls != 1 || fake.shutdownCalls != 1 {
		t.Fatalf("unexpected fake server calls: new=%d listen=%d shutdown=%d", fake.newServerCalls, fake.listenCalls, fake.shutdownCalls)
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

	localRuntime, err := srv.RemoteLocalEnable(context.Background(), termx.RemoteLocalOptions{
		LocalWebAddr: "127.0.0.1:0",
		ICETCPAddr:   "127.0.0.1:0",
	})
	if err != nil {
		t.Fatalf("RemoteLocalEnable returned error: %v", err)
	}
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer shutdownCancel()
		if _, err := srv.RemoteLocalDisable(shutdownCtx); err != nil {
			t.Fatalf("shutdown local web: %v", err)
		}
	}()
	baseURL := localRuntime.HTTPURL

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
	assetPath := embeddedModuleAssetPath(t, string(body))
	resp, err = client.Get(baseURL + assetPath)
	if err != nil {
		t.Fatalf("GET embedded module asset: %v", err)
	}
	assetBody, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read embedded module asset: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected embedded module status 200, got %d: %s", resp.StatusCode, string(assetBody))
	}
	assetText := string(assetBody)
	for _, want := range []string{"termx-local-web-shell", "termx-local-pair-panel", "Pair ID", "Pair secret"} {
		if !strings.Contains(assetText, want) {
			t.Fatalf("expected embedded module asset to contain %q", want)
		}
	}
	for _, forbidden := range []string{"machine_private_key", "machinePrivateKey", "turn:", "turns:"} {
		if strings.Contains(strings.ToLower(assetText), strings.ToLower(forbidden)) {
			t.Fatalf("embedded module asset must not expose %q", forbidden)
		}
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

func embeddedModuleAssetPath(t *testing.T, html string) string {
	t.Helper()
	const marker = `type="module" crossorigin src="`
	start := strings.Index(html, marker)
	if start < 0 {
		t.Fatalf("missing module script in embedded html: %q", html)
	}
	start += len(marker)
	end := strings.Index(html[start:], `"`)
	if end < 0 {
		t.Fatalf("malformed module script in embedded html: %q", html)
	}
	return html[start : start+end]
}

func TestRootCmdHasRemoteStatusAndPairCommands(t *testing.T) {
	cmd := newRootCmd()
	for _, args := range [][]string{
		{"remote", "status"},
		{"remote", "info"},
		{"remote", "show"},
		{"remote", "enable"},
		{"remote", "local-only"},
		{"remote", "local_only"},
		{"remote", "disable"},
		{"remote", "pair"},
		{"remote", "open"},
	} {
		if _, _, err := cmd.Find(args); err != nil {
			t.Fatalf("expected %q command to exist: %v", strings.Join(args, " "), err)
		}
	}
	if _, _, err := cmd.Find([]string{"pair"}); err != nil {
		t.Fatalf("expected pair command to exist: %v", err)
	}
}

func TestRemoteEnableLocalOnlyEmitsLocalStatus(t *testing.T) {
	oldEnable := remoteLocalEnableClient
	t.Cleanup(func() {
		remoteLocalEnableClient = oldEnable
	})

	var gotParams protocol.RemoteLocalEnableParams
	remoteLocalEnableClient = func(ctx context.Context, socketPath string, logFile string, params protocol.RemoteLocalEnableParams) (*protocol.RemoteLocalStatus, error) {
		gotParams = params
		return &protocol.RemoteLocalStatus{
			Enabled:       true,
			HTTPURL:       "http://127.0.0.1:18888",
			LocalWebAddr:  params.LocalWebAddr,
			LocalPairURL:  "http://127.0.0.1:18888/api/local/pair",
			ICETCPEnabled: true,
			ICETCPAddr:    params.ICETCPAddr,
			ICETCPPort:    18889,
			UpdatedAt:     time.Date(2026, 5, 1, 0, 5, 0, 0, time.UTC),
		}, nil
	}

	var out bytes.Buffer
	cmd := newRootCmd()
	cmd.SetArgs([]string{"remote", "enable", "--local-only", "--addr", "127.0.0.1:18888", "--ice-tcp-addr", "127.0.0.1:18889"})
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if gotParams.LocalWebAddr != "127.0.0.1:18888" || gotParams.ICETCPAddr != "127.0.0.1:18889" {
		t.Fatalf("unexpected enable params: %#v", gotParams)
	}
	if !strings.Contains(out.String(), "local_web_url:\thttp://127.0.0.1:18888") ||
		!strings.Contains(out.String(), "local_pair_url:\thttp://127.0.0.1:18888/api/local/pair") {
		t.Fatalf("unexpected remote enable output:\n%s", out.String())
	}
}

func TestRemotePairUsesRunningLocalPairURL(t *testing.T) {
	oldStatus := remoteLocalStatusClient
	oldPair := pairStartClient
	t.Cleanup(func() {
		remoteLocalStatusClient = oldStatus
		pairStartClient = oldPair
	})

	remoteLocalStatusClient = func(ctx context.Context, socketPath string, logFile string) (*protocol.RemoteLocalStatus, error) {
		return &protocol.RemoteLocalStatus{
			Enabled:      true,
			LocalPairURL: "http://127.0.0.1:19999/api/local/pair",
		}, nil
	}
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
	cmd.SetArgs([]string{"remote", "pair", "--ttl", "2m"})
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if gotParams.LocalPairURL != "http://127.0.0.1:19999/api/local/pair" || gotParams.TTLSeconds != 120 {
		t.Fatalf("unexpected pair params: %#v", gotParams)
	}
	if !strings.Contains(out.String(), "pair_session_id:\tpair_test") || !strings.Contains(out.String(), "pair_secret:\tsecret") {
		t.Fatalf("unexpected remote pair output:\n%s", out.String())
	}
}

func TestRemoteStatusIncludesLocalRuntime(t *testing.T) {
	oldRemoteStatus := remoteStatusClient
	oldStatus := remoteLocalStatusClient
	t.Cleanup(func() {
		remoteStatusClient = oldRemoteStatus
		remoteLocalStatusClient = oldStatus
	})
	remoteStatusClient = func(ctx context.Context, socketPath string, logFile string) (*protocol.RemoteStatus, error) {
		return &protocol.RemoteStatus{
			State:      "disabled",
			DeviceName: "RedmiBook",
			UpdatedAt:  time.Date(2026, 5, 1, 0, 5, 0, 0, time.UTC),
		}, nil
	}
	remoteLocalStatusClient = func(ctx context.Context, socketPath string, logFile string) (*protocol.RemoteLocalStatus, error) {
		return &protocol.RemoteLocalStatus{
			Enabled:      true,
			HTTPURL:      "http://127.0.0.1:18888",
			LocalPairURL: "http://127.0.0.1:18888/api/local/pair",
			UpdatedAt:    time.Date(2026, 5, 1, 0, 5, 0, 0, time.UTC),
		}, nil
	}

	var out bytes.Buffer
	cmd := newRootCmd()
	cmd.SetArgs([]string{"remote", "status"})
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !strings.Contains(out.String(), "local_enabled:\ttrue") ||
		!strings.Contains(out.String(), "local_web_url:\thttp://127.0.0.1:18888") {
		t.Fatalf("expected local runtime in status output, got:\n%s", out.String())
	}
}

func TestRemoteInfoShowEmitsJSON(t *testing.T) {
	oldRemoteStatus := remoteStatusClient
	oldStatus := remoteLocalStatusClient
	t.Cleanup(func() {
		remoteStatusClient = oldRemoteStatus
		remoteLocalStatusClient = oldStatus
	})
	remoteStatusClient = func(ctx context.Context, socketPath string, logFile string) (*protocol.RemoteStatus, error) {
		return &protocol.RemoteStatus{
			State:      "disabled",
			DeviceID:   "mach_json",
			DeviceName: "JSON Machine",
			UpdatedAt:  time.Date(2026, 5, 1, 0, 5, 0, 0, time.UTC),
		}, nil
	}
	remoteLocalStatusClient = func(ctx context.Context, socketPath string, logFile string) (*protocol.RemoteLocalStatus, error) {
		return &protocol.RemoteLocalStatus{
			Enabled:      true,
			HTTPURL:      "http://127.0.0.1:18888",
			LocalPairURL: "http://127.0.0.1:18888/api/local/pair",
			UpdatedAt:    time.Date(2026, 5, 1, 0, 5, 0, 0, time.UTC),
		}, nil
	}

	var out bytes.Buffer
	cmd := newRootCmd()
	cmd.SetArgs([]string{"remote", "show", "--json"})
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	var decoded struct {
		Remote protocol.RemoteStatus      `json:"remote"`
		Local  protocol.RemoteLocalStatus `json:"local"`
	}
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("remote show output is not JSON: %v\n%s", err, out.String())
	}
	if decoded.Remote.DeviceID != "mach_json" || decoded.Local.HTTPURL != "http://127.0.0.1:18888" {
		t.Fatalf("unexpected remote show JSON: %#v", decoded)
	}
}

func TestRemoteDisableEmitsJSONLocalStatus(t *testing.T) {
	oldDisable := remoteLocalDisableClient
	t.Cleanup(func() {
		remoteLocalDisableClient = oldDisable
	})
	called := false
	remoteLocalDisableClient = func(ctx context.Context, socketPath string, logFile string) (*protocol.RemoteLocalStatus, error) {
		called = true
		return &protocol.RemoteLocalStatus{
			Enabled:   false,
			UpdatedAt: time.Date(2026, 5, 1, 0, 5, 0, 0, time.UTC),
		}, nil
	}

	var out bytes.Buffer
	cmd := newRootCmd()
	cmd.SetArgs([]string{"remote", "disable", "--json"})
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !called {
		t.Fatal("expected remote local disable client to be called")
	}
	var decoded protocol.RemoteLocalStatus
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("remote disable output is not JSON: %v\n%s", err, out.String())
	}
	if decoded.Enabled {
		t.Fatalf("expected disabled local status, got %#v", decoded)
	}
}

func TestRemoteOpenPrintsOrLaunchesRunningLocalURL(t *testing.T) {
	oldStatus := remoteLocalStatusClient
	oldOpen := openBrowser
	t.Cleanup(func() {
		remoteLocalStatusClient = oldStatus
		openBrowser = oldOpen
	})
	remoteLocalStatusClient = func(ctx context.Context, socketPath string, logFile string) (*protocol.RemoteLocalStatus, error) {
		return &protocol.RemoteLocalStatus{
			Enabled: true,
			HTTPURL: "http://127.0.0.1:18888",
		}, nil
	}
	var opened []string
	openBrowser = func(rawURL string) error {
		opened = append(opened, rawURL)
		return nil
	}

	var printOut bytes.Buffer
	printCmd := newRootCmd()
	printCmd.SetArgs([]string{"remote", "open", "--print"})
	printCmd.SetOut(&printOut)
	printCmd.SetErr(io.Discard)
	if err := printCmd.Execute(); err != nil {
		t.Fatalf("remote open --print returned error: %v", err)
	}
	if strings.TrimSpace(printOut.String()) != "http://127.0.0.1:18888" {
		t.Fatalf("unexpected print output: %q", printOut.String())
	}
	if len(opened) != 0 {
		t.Fatalf("--print must not launch browser, opened=%#v", opened)
	}

	var openOut bytes.Buffer
	openCmd := newRootCmd()
	openCmd.SetArgs([]string{"remote", "open"})
	openCmd.SetOut(&openOut)
	openCmd.SetErr(io.Discard)
	if err := openCmd.Execute(); err != nil {
		t.Fatalf("remote open returned error: %v", err)
	}
	if len(opened) != 1 || opened[0] != "http://127.0.0.1:18888" {
		t.Fatalf("unexpected browser launches: %#v", opened)
	}
	if strings.TrimSpace(openOut.String()) != "http://127.0.0.1:18888" {
		t.Fatalf("unexpected open output: %q", openOut.String())
	}
}

func TestRemoteOpenRequiresEnabledLocalRuntime(t *testing.T) {
	oldStatus := remoteLocalStatusClient
	t.Cleanup(func() {
		remoteLocalStatusClient = oldStatus
	})
	remoteLocalStatusClient = func(ctx context.Context, socketPath string, logFile string) (*protocol.RemoteLocalStatus, error) {
		return &protocol.RemoteLocalStatus{Enabled: false}, nil
	}

	cmd := newRootCmd()
	cmd.SetArgs([]string{"remote", "open", "--print"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "local remote is not enabled") {
		t.Fatalf("expected local remote disabled error, got %v", err)
	}
}

func TestRemoteEnableManagedPathIsExplicitlyDeferred(t *testing.T) {
	oldEnable := remoteLocalEnableClient
	t.Cleanup(func() {
		remoteLocalEnableClient = oldEnable
	})
	remoteLocalEnableClient = func(ctx context.Context, socketPath string, logFile string, params protocol.RemoteLocalEnableParams) (*protocol.RemoteLocalStatus, error) {
		t.Fatal("remote local enable client must not be called for managed enable")
		return nil, nil
	}

	cmd := newRootCmd()
	cmd.SetArgs([]string{"remote", "enable"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "managed remote enable is not implemented yet") {
		t.Fatalf("expected managed enable deferral error, got %v", err)
	}
}

func TestRemoteLocalOnlyAliasEnablesRuntime(t *testing.T) {
	oldEnable := remoteLocalEnableClient
	t.Cleanup(func() {
		remoteLocalEnableClient = oldEnable
	})
	var gotParams protocol.RemoteLocalEnableParams
	remoteLocalEnableClient = func(ctx context.Context, socketPath string, logFile string, params protocol.RemoteLocalEnableParams) (*protocol.RemoteLocalStatus, error) {
		gotParams = params
		return &protocol.RemoteLocalStatus{
			Enabled:      true,
			HTTPURL:      "http://127.0.0.1:18888",
			LocalPairURL: "http://127.0.0.1:18888/api/local/pair",
			UpdatedAt:    time.Date(2026, 5, 1, 0, 5, 0, 0, time.UTC),
		}, nil
	}

	cmd := newRootCmd()
	cmd.SetArgs([]string{"remote", "local_only", "--addr", "127.0.0.1:18888", "--ice-tcp-addr", "127.0.0.1:18889"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if gotParams.LocalWebAddr != "127.0.0.1:18888" || gotParams.ICETCPAddr != "127.0.0.1:18889" {
		t.Fatalf("unexpected enable params: %#v", gotParams)
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

type fakeTermxServer struct {
	newServerCalls int
	listenCalls    int
	shutdownCalls  int
}

func (s *fakeTermxServer) ListenAndServe(context.Context) error {
	s.listenCalls++
	return nil
}

func (s *fakeTermxServer) Shutdown(context.Context) error {
	s.shutdownCalls++
	return nil
}

func (s *fakeTermxServer) RemoteLocalEnable(context.Context, termx.RemoteLocalOptions) (termx.RemoteLocalStatus, error) {
	return termx.RemoteLocalStatus{}, nil
}
