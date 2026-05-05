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
	remoteprotocol "github.com/lozzow/termx/termx-remote/protocol"
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
	if err == nil || !strings.Contains(err.Error(), "refusing to start termx TUI inside a termx remote terminal") {
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
	if err == nil || !strings.Contains(err.Error(), "refusing to start termx TUI inside a termx remote terminal") {
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
	t.Setenv("TERMX_REMOTE_CONNECTION_KEY", "secret")
	t.Setenv("TERMX_REMOTE_DATA_DIR", "/tmp/termx-remote")
	t.Setenv("TERMX_REMOTE_DEVICE_NAME", "device-a")
	t.Setenv("TERMX_REMOTE_REGION", "sin")

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
		t.Fatalf("unexpected connection key: %q", cfg.AccessToken)
	}
	if cfg.DataDir != "/tmp/termx-remote" {
		t.Fatalf("unexpected data dir: %q", cfg.DataDir)
	}
	if cfg.DeviceName != "device-a" {
		t.Fatalf("unexpected device name: %q", cfg.DeviceName)
	}
	if cfg.Region != "sin" {
		t.Fatalf("unexpected region: %q", cfg.Region)
	}
}

func TestRemoteConfigFromEnvAcceptsLegacyAccessToken(t *testing.T) {
	t.Setenv("TERMX_REMOTE_ACCESS_TOKEN", "legacy-secret")

	cfg := remoteConfigFromEnv()
	if cfg.AccessToken != "legacy-secret" {
		t.Fatalf("expected legacy access token env to remain supported, got %q", cfg.AccessToken)
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
  connectionKeyEnv: TERMX_TEST_REMOTE_KEY
  accessToken: should-not-be-used
  dataDir: /tmp/termx-remote-file
  deviceName: file-device
  region: fra
`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	t.Setenv("TERMX_TEST_REMOTE_KEY", "file-secret")

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
		t.Fatalf("expected connection key from env reference, got %q", cfg.AccessToken)
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
	if cfg.Region != "fra" {
		t.Fatalf("unexpected region: %q", cfg.Region)
	}
}

func TestRemoteConfigEnvOverridesFile(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "termx.yaml")
	content := `remote:
  enabled: true
  controlURL: https://control-file.example.test
  hubURL: https://hub-file.example.test
  connectionKeyEnv: TERMX_TEST_REMOTE_KEY_FILE
  dataDir: /tmp/termx-remote-file
  deviceName: file-device
`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	t.Setenv("TERMX_TEST_REMOTE_KEY_FILE", "file-secret")
	t.Setenv("TERMX_REMOTE_CONTROL_URL", "https://control-env.example.test")
	t.Setenv("TERMX_REMOTE_HUB_URL", "https://hub-env.example.test")
	t.Setenv("TERMX_REMOTE_CONNECTION_KEY", "env-secret")
	t.Setenv("TERMX_REMOTE_DATA_DIR", "/tmp/termx-remote-env")
	t.Setenv("TERMX_REMOTE_DEVICE_NAME", "env-device")
	t.Setenv("TERMX_REMOTE_REGION", "sin")

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
	if cfg.Region != "sin" {
		t.Fatalf("expected env region override, got %q", cfg.Region)
	}
}

func TestRemoteConfigIgnoresLegacyAuthStoreHubURL(t *testing.T) {
	configDir := t.TempDir()
	authStore := filepath.Join(configDir, "remote-auth.json")
	if err := saveRemoteAuthRecord(authStore, remoteAuthRecord{
		ControlURL:  "https://control-auth.example.test",
		HubURL:      "https://legacy-hub-auth.example.test",
		AccessToken: "auth-secret",
	}); err != nil {
		t.Fatalf("save auth record: %v", err)
	}
	configPath := filepath.Join(configDir, "termx.yaml")
	content := `remote:
  enabled: true
  authStore: ` + authStore + `
`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	cfg, err := remoteConfigFromFileAndEnv(configPath)
	if err != nil {
		t.Fatalf("remoteConfigFromFileAndEnv returned error: %v", err)
	}
	if cfg.ControlURL != "https://control-auth.example.test" || cfg.AccessToken != "auth-secret" {
		t.Fatalf("unexpected auth-store bootstrap: %#v", cfg)
	}
	if cfg.HubURL != "" {
		t.Fatalf("legacy auth-store hub url bypassed discovery policy: %q", cfg.HubURL)
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
	oldRemoteRuntimeHost := newRemoteRuntimeHostFn
	t.Cleanup(func() {
		remoteConfigLoader = oldLoader
		newServer = oldNewServer
		newRemoteRuntimeHostFn = oldRemoteRuntimeHost
	})

	configPath := filepath.Join(t.TempDir(), "termx.yaml")
	var gotConfigPath string
	var gotRemoteConfig remoteprotocol.Config
	remoteConfigLoader = func(path string) (remoteprotocol.Config, error) {
		gotConfigPath = path
		return remoteprotocol.Config{
			Enabled:     true,
			ControlURL:  "https://control-config.example.test",
			HubURL:      "https://hub-config.example.test",
			AccessToken: "loader-secret",
			DataDir:     t.TempDir(),
			DeviceName:  "config-device",
		}, nil
	}
	newRemoteRuntimeHostFn = func(core remoteRuntimeCore, cfg remoteprotocol.Config) *remoteRuntimeHost {
		gotRemoteConfig = cfg
		return nil
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

func TestStartRemoteLocalRuntimeServesEmbeddedHub(t *testing.T) {
	srv := termx.NewServer()
	remoteHost := newRemoteRuntimeHost(srv, remoteprotocol.Config{
		Enabled:    true,
		DataDir:    t.TempDir(),
		DeviceName: "cli-local-hub",
	})
	defer srv.Shutdown(context.Background())
	defer func() { _ = remoteHost.Close(context.Background()) }()

	if err := remoteHost.Start(context.Background()); err != nil {
		t.Fatalf("remote start returned error: %v", err)
	}
	localRuntime, err := remoteHost.service.LocalEnable(context.Background(), remoteprotocol.LocalEnableParams{
		LocalWebAddr: "127.0.0.1:0",
		ICETCPAddr:   "127.0.0.1:0",
	})
	if err != nil {
		t.Fatalf("RemoteLocalEnable returned error: %v", err)
	}
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer shutdownCancel()
		if _, err := remoteHost.service.LocalDisable(shutdownCtx); err != nil {
			t.Fatalf("shutdown local web: %v", err)
		}
	}()
	baseURL := localRuntime.HTTPURL

	client := http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(baseURL + "/api/health")
	if err != nil {
		t.Fatalf("GET local hub health: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read local hub health: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected local hub health status 200, got %d: %s", resp.StatusCode, string(body))
	}
	if !strings.Contains(string(body), "termx-hub") {
		t.Fatalf("expected hub health response, got %s", string(body))
	}

	registerPayload := strings.NewReader(`{
		"version":"remote.hub.v1",
		"device_id":"` + remoteHost.service.Status().DeviceID + `",
		"agent_id":"agent-cli-local-hub",
		"display_name":"TermX CLI Local Hub Test",
		"runtime_version":"test",
		"terminals":[]
	}`)
	resp, err = client.Post(baseURL+"/api/v1/agents/register", "application/json", registerPayload)
	if err != nil {
		t.Fatalf("POST local hub register: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected local hub register 200, got %d: %s", resp.StatusCode, string(body))
	}
	var registerResp map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&registerResp); err != nil {
		t.Fatalf("decode local hub register: %v", err)
	}
	if registerResp["agent_session_id"] == "" {
		t.Fatalf("expected agent_session_id in register response, got %#v", registerResp)
	}
	rawRegister, _ := json.Marshal(registerResp)
	if strings.Contains(strings.ToLower(string(rawRegister)), "turn:") || strings.Contains(strings.ToLower(string(rawRegister)), "turns:") {
		t.Fatalf("local hub register must not expose TURN credentials: %s", rawRegister)
	}

	session, err := remoteHost.service.PairStart(remoteprotocol.PairStartParams{
		LocalPairURL: baseURL + "/api/v1/pairing/claims",
		TTLSeconds:   int(time.Minute.Seconds()),
	})
	if err != nil {
		t.Fatalf("RemotePairStart returned error: %v", err)
	}
	appPublic, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey returned error: %v", err)
	}
	pairPayload := strings.NewReader(`{
		"machine_id":"` + session.MachineID + `",
		"pair_session_id":"` + session.PairSessionID + `",
		"pair_secret":"` + session.PairSecret + `",
		"app_device_id":"app-cli-local-web",
		"app_name":"TermX CLI Local Web Test",
		"app_public_key":"` + base64.StdEncoding.EncodeToString(appPublic) + `",
		"requested_capabilities":["terminal","file_manager"]
	}`)
	resp, err = client.Post(baseURL+"/api/v1/pairing/claims", "application/json", pairPayload)
	if err != nil {
		t.Fatalf("POST local pairing claim: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected local pairing claim 200, got %d: %s", resp.StatusCode, string(body))
	}
	var pair map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&pair); err != nil {
		t.Fatalf("decode local pairing claim: %v", err)
	}
	if pair["machine_id"] != session.MachineID {
		t.Fatalf("expected pair machine_id %q, got %#v", session.MachineID, pair)
	}
	if pair["app_certificate"] == nil || pair["machine_public_key"] == "" {
		t.Fatalf("expected completed pairing response, got %#v", pair)
	}

	if _, err := remoteHost.service.LocalDisable(context.Background()); err != nil {
		t.Fatalf("LocalDisable returned error: %v", err)
	}
	if _, err := client.Get(baseURL + "/api/health"); err == nil {
		t.Fatalf("expected disabled local hub to reject health requests")
	}
}

func TestRootCmdHasRemoteStatusAndPairCommands(t *testing.T) {
	cmd := newRootCmd()
	for _, args := range [][]string{
		{"remote", "status"},
		{"remote", "info"},
		{"remote", "show"},
		{"remote", "enable"},
		{"remote", "local"},
		{"remote", "local_only"},
		{"remote", "disable"},
		{"remote", "pair"},
		{"remote", "qrcode"},
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

	var gotParams remoteprotocol.LocalEnableParams
	remoteLocalEnableClient = func(ctx context.Context, socketPath string, logFile string, params remoteprotocol.LocalEnableParams) (*remoteprotocol.LocalStatus, error) {
		gotParams = params
		return &remoteprotocol.LocalStatus{
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
	cmd.SetArgs([]string{"remote", "enable", "--local", "--addr", "127.0.0.1:18888", "--ice-tcp-addr", "127.0.0.1:18889"})
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

	remoteLocalStatusClient = func(ctx context.Context, socketPath string, logFile string) (*remoteprotocol.LocalStatus, error) {
		return &remoteprotocol.LocalStatus{
			Enabled:      true,
			LocalPairURL: "http://127.0.0.1:19999/api/local/pair",
		}, nil
	}
	var gotParams remoteprotocol.PairStartParams
	pairStartClient = func(ctx context.Context, socketPath string, logFile string, params remoteprotocol.PairStartParams) (*remoteprotocol.PairStartResult, error) {
		gotParams = params
		return &remoteprotocol.PairStartResult{
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

func TestRemoteQRCodeEmitsTermxPairURIWithCloudMetadata(t *testing.T) {
	oldStatus := remoteLocalStatusClient
	oldEnable := remoteLocalEnableClient
	oldRemoteStatus := remoteStatusClient
	oldPair := pairStartClient
	t.Cleanup(func() {
		remoteLocalStatusClient = oldStatus
		remoteLocalEnableClient = oldEnable
		remoteStatusClient = oldRemoteStatus
		pairStartClient = oldPair
	})

	remoteLocalStatusClient = func(ctx context.Context, socketPath string, logFile string) (*remoteprotocol.LocalStatus, error) {
		return &remoteprotocol.LocalStatus{
			Enabled:      true,
			LocalPairURL: "http://127.0.0.1:18888/api/local/pair",
		}, nil
	}
	remoteLocalEnableClient = func(ctx context.Context, socketPath string, logFile string, params remoteprotocol.LocalEnableParams) (*remoteprotocol.LocalStatus, error) {
		t.Fatalf("remote qrcode should not enable local web when it is already running")
		return nil, nil
	}
	remoteStatusClient = func(ctx context.Context, socketPath string, logFile string) (*remoteprotocol.Status, error) {
		return &remoteprotocol.Status{
			DeviceID:   "mach_test",
			DeviceName: "MacBook Pro",
			ControlURL: "http://114.66.58.243:12306",
			HubURL:     "http://114.66.58.243:8447",
			UpdatedAt:  time.Date(2026, 5, 1, 0, 5, 0, 0, time.UTC),
		}, nil
	}
	var gotParams remoteprotocol.PairStartParams
	pairStartClient = func(ctx context.Context, socketPath string, logFile string, params remoteprotocol.PairStartParams) (*remoteprotocol.PairStartResult, error) {
		gotParams = params
		return &remoteprotocol.PairStartResult{
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
	cmd.SetArgs([]string{"remote", "qrcode", "--json", "--ttl", "2m"})
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if gotParams.LocalPairURL != "http://127.0.0.1:18888/api/local/pair" || gotParams.TTLSeconds != 120 {
		t.Fatalf("unexpected pair params: %#v", gotParams)
	}
	var decoded struct {
		URI     string         `json:"uri"`
		Payload map[string]any `json:"payload"`
	}
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("qrcode output is not JSON: %v\n%s", err, out.String())
	}
	if !strings.HasPrefix(decoded.URI, "termx://pair?payload=") {
		t.Fatalf("unexpected URI: %s", decoded.URI)
	}
	if decoded.Payload["preferred_path"] != "public_p2p" {
		t.Fatalf("expected public_p2p payload, got %#v", decoded.Payload)
	}
	endpoints, ok := decoded.Payload["endpoints"].(map[string]any)
	if !ok || endpoints["web_control"] != "http://114.66.58.243:12306" || endpoints["hub"] != "http://114.66.58.243:8447" {
		t.Fatalf("unexpected endpoints: %#v", decoded.Payload["endpoints"])
	}
	pairing, ok := decoded.Payload["pairing"].(map[string]any)
	if !ok || pairing["session_id"] != "pair_test" || pairing["secret"] != "secret" {
		t.Fatalf("unexpected pairing: %#v", decoded.Payload["pairing"])
	}
}

func TestRemoteQRCodeStartsLocalWebWhenNeeded(t *testing.T) {
	oldStatus := remoteLocalStatusClient
	oldEnable := remoteLocalEnableClient
	oldRemoteStatus := remoteStatusClient
	oldPair := pairStartClient
	t.Cleanup(func() {
		remoteLocalStatusClient = oldStatus
		remoteLocalEnableClient = oldEnable
		remoteStatusClient = oldRemoteStatus
		pairStartClient = oldPair
	})

	remoteLocalStatusClient = func(ctx context.Context, socketPath string, logFile string) (*remoteprotocol.LocalStatus, error) {
		return &remoteprotocol.LocalStatus{}, nil
	}
	var gotEnable remoteprotocol.LocalEnableParams
	remoteLocalEnableClient = func(ctx context.Context, socketPath string, logFile string, params remoteprotocol.LocalEnableParams) (*remoteprotocol.LocalStatus, error) {
		gotEnable = params
		return &remoteprotocol.LocalStatus{
			Enabled:      true,
			LocalPairURL: "http://127.0.0.1:18888/api/local/pair",
		}, nil
	}
	remoteStatusClient = func(ctx context.Context, socketPath string, logFile string) (*remoteprotocol.Status, error) {
		return &remoteprotocol.Status{
			DeviceID:   "mach_test",
			DeviceName: "MacBook Pro",
			ControlURL: "http://114.66.58.243:12306",
			HubURL:     "http://114.66.58.243:8447",
			UpdatedAt:  time.Date(2026, 5, 1, 0, 5, 0, 0, time.UTC),
		}, nil
	}
	pairStartClient = func(ctx context.Context, socketPath string, logFile string, params remoteprotocol.PairStartParams) (*remoteprotocol.PairStartResult, error) {
		return &remoteprotocol.PairStartResult{
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

	cmd := newRootCmd()
	cmd.SetArgs([]string{"remote", "qrcode", "--payload"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if gotEnable.LocalWebAddr != "127.0.0.1:18888" || gotEnable.ICETCPAddr != "127.0.0.1:18889" {
		t.Fatalf("unexpected local web enable params: %#v", gotEnable)
	}
}

func TestRemoteStatusIncludesLocalRuntime(t *testing.T) {
	oldRemoteStatus := remoteStatusClient
	oldStatus := remoteLocalStatusClient
	t.Cleanup(func() {
		remoteStatusClient = oldRemoteStatus
		remoteLocalStatusClient = oldStatus
	})
	remoteStatusClient = func(ctx context.Context, socketPath string, logFile string) (*remoteprotocol.Status, error) {
		return &remoteprotocol.Status{
			State:      "disabled",
			DeviceName: "RedmiBook",
			UpdatedAt:  time.Date(2026, 5, 1, 0, 5, 0, 0, time.UTC),
		}, nil
	}
	remoteLocalStatusClient = func(ctx context.Context, socketPath string, logFile string) (*remoteprotocol.LocalStatus, error) {
		return &remoteprotocol.LocalStatus{
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
	remoteStatusClient = func(ctx context.Context, socketPath string, logFile string) (*remoteprotocol.Status, error) {
		return &remoteprotocol.Status{
			State:      "disabled",
			DeviceID:   "mach_json",
			DeviceName: "JSON Machine",
			UpdatedAt:  time.Date(2026, 5, 1, 0, 5, 0, 0, time.UTC),
		}, nil
	}
	remoteLocalStatusClient = func(ctx context.Context, socketPath string, logFile string) (*remoteprotocol.LocalStatus, error) {
		return &remoteprotocol.LocalStatus{
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
		Remote remoteprotocol.Status      `json:"remote"`
		Local  remoteprotocol.LocalStatus `json:"local"`
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
	remoteLocalDisableClient = func(ctx context.Context, socketPath string, logFile string) (*remoteprotocol.LocalStatus, error) {
		called = true
		return &remoteprotocol.LocalStatus{
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
	var decoded remoteprotocol.LocalStatus
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
	remoteLocalStatusClient = func(ctx context.Context, socketPath string, logFile string) (*remoteprotocol.LocalStatus, error) {
		return &remoteprotocol.LocalStatus{
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
	remoteLocalStatusClient = func(ctx context.Context, socketPath string, logFile string) (*remoteprotocol.LocalStatus, error) {
		return &remoteprotocol.LocalStatus{Enabled: false}, nil
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

func TestRemoteEnableCloudPersistsBootstrapOutsideConfigFile(t *testing.T) {
	oldEnable := remoteLocalEnableClient
	oldLogin := remoteLoginHTTPClient
	oldStore := remoteAuthStorePath
	t.Cleanup(func() {
		remoteLocalEnableClient = oldEnable
		remoteLoginHTTPClient = oldLogin
		remoteAuthStorePath = oldStore
	})
	remoteLocalEnableClient = func(ctx context.Context, socketPath string, logFile string, params remoteprotocol.LocalEnableParams) (*remoteprotocol.LocalStatus, error) {
		t.Fatal("remote local enable client must not be called for cloud enable")
		return nil, nil
	}
	remoteAuthStorePath = func(configPath string) (string, error) {
		return filepath.Join(t.TempDir(), "remote-auth.json"), nil
	}
	var validatedToken string
	remoteLoginHTTPClient = remoteLoginHTTPClientFunc{
		meFunc: func(ctx context.Context, controlURL string, token string) (remoteLoginUser, error) {
			validatedToken = token
			return remoteLoginUser{Email: "cloud@example.com"}, nil
		},
	}

	configPath := filepath.Join(t.TempDir(), "termx.yaml")
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetArgs([]string{"--config", configPath, "remote", "enable", "--cloud", "--server", "https://control.example.test", "--token", "cloud-secret", "--json"})
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("cloud enable returned error: %v", err)
	}
	if validatedToken != "cloud-secret" {
		t.Fatalf("expected token validation, got %q", validatedToken)
	}
	if data, err := os.ReadFile(configPath); err == nil && strings.Contains(string(data), "cloud-secret") {
		t.Fatal("raw connection key was written to termx config")
	}
	var decoded struct {
		Enabled    bool   `json:"enabled"`
		ControlURL string `json:"control_url"`
		AuthStore  string `json:"auth_store"`
	}
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("cloud enable output is not JSON: %v\n%s", err, out.String())
	}
	if !decoded.Enabled || decoded.ControlURL != "https://control.example.test" || decoded.AuthStore == "" {
		t.Fatalf("unexpected cloud enable JSON: %#v", decoded)
	}
	cfg, err := remoteConfigFromFileAndEnv(configPath)
	if err != nil {
		t.Fatalf("load cloud config: %v", err)
	}
	if !cfg.Enabled || cfg.ControlURL != "https://control.example.test" || cfg.AccessToken != "cloud-secret" {
		t.Fatalf("unexpected cloud remote config: %#v", cfg)
	}
}

func TestRemoteEnableBothPassesCloudHubToRunningLocalDaemon(t *testing.T) {
	oldEnable := remoteLocalEnableClient
	oldLogin := remoteLoginHTTPClient
	oldStore := remoteAuthStorePath
	t.Cleanup(func() {
		remoteLocalEnableClient = oldEnable
		remoteLoginHTTPClient = oldLogin
		remoteAuthStorePath = oldStore
	})
	remoteAuthStorePath = func(configPath string) (string, error) {
		return filepath.Join(t.TempDir(), "remote-auth.json"), nil
	}
	remoteLoginHTTPClient = remoteLoginHTTPClientFunc{
		meFunc: func(ctx context.Context, controlURL string, token string) (remoteLoginUser, error) {
			return remoteLoginUser{Email: "cloud@example.com"}, nil
		},
	}
	var gotParams remoteprotocol.LocalEnableParams
	remoteLocalEnableClient = func(ctx context.Context, socketPath string, logFile string, params remoteprotocol.LocalEnableParams) (*remoteprotocol.LocalStatus, error) {
		gotParams = params
		return &remoteprotocol.LocalStatus{
			Enabled:      true,
			HTTPURL:      "http://127.0.0.1:18888",
			LocalPairURL: "http://127.0.0.1:18888/api/v1/pairing/claims",
			UpdatedAt:    time.Date(2026, 5, 1, 0, 5, 0, 0, time.UTC),
		}, nil
	}

	configPath := filepath.Join(t.TempDir(), "termx.yaml")
	cmd := newRootCmd()
	cmd.SetArgs([]string{
		"--config", configPath,
		"remote", "enable",
		"--local",
		"--cloud",
		"--server", "https://control.example.test",
		"--hub-url", "https://hub.example.test",
		"--token", "cloud-secret",
	})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("both enable returned error: %v", err)
	}
	if gotParams.HubURLs == nil || len(gotParams.HubURLs) != 1 || gotParams.HubURLs[0] != "https://hub.example.test" {
		t.Fatalf("running local daemon did not receive cloud hub URL: %#v", gotParams)
	}
}

func TestRemoteEnableBothPassesCloudDiscoveryToRunningLocalDaemon(t *testing.T) {
	oldEnable := remoteLocalEnableClient
	oldLogin := remoteLoginHTTPClient
	oldStore := remoteAuthStorePath
	t.Cleanup(func() {
		remoteLocalEnableClient = oldEnable
		remoteLoginHTTPClient = oldLogin
		remoteAuthStorePath = oldStore
	})
	remoteAuthStorePath = func(configPath string) (string, error) {
		return filepath.Join(t.TempDir(), "remote-auth.json"), nil
	}
	remoteLoginHTTPClient = remoteLoginHTTPClientFunc{
		meFunc: func(ctx context.Context, controlURL string, token string) (remoteLoginUser, error) {
			return remoteLoginUser{Email: "cloud@example.com"}, nil
		},
	}
	var gotParams remoteprotocol.LocalEnableParams
	remoteLocalEnableClient = func(ctx context.Context, socketPath string, logFile string, params remoteprotocol.LocalEnableParams) (*remoteprotocol.LocalStatus, error) {
		gotParams = params
		return &remoteprotocol.LocalStatus{
			Enabled:      true,
			HTTPURL:      "http://127.0.0.1:18888",
			LocalPairURL: "http://127.0.0.1:18888/api/v1/pairing/claims",
			UpdatedAt:    time.Date(2026, 5, 1, 0, 5, 0, 0, time.UTC),
		}, nil
	}

	configPath := filepath.Join(t.TempDir(), "termx.yaml")
	cmd := newRootCmd()
	cmd.SetArgs([]string{
		"--config", configPath,
		"remote", "enable",
		"--local",
		"--cloud",
		"--server", "https://control.example.test",
		"--token", "cloud-secret",
	})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("both enable returned error: %v", err)
	}
	if gotParams.ControlURL != "https://control.example.test" || gotParams.AccessToken != "cloud-secret" {
		t.Fatalf("running local daemon did not receive cloud discovery config: %#v", gotParams)
	}
	if len(gotParams.HubURLs) != 0 {
		t.Fatalf("expected discovery path without explicit hub URLs, got %#v", gotParams.HubURLs)
	}
}

func TestRemoteEnableCloudUsesBrowserLoginWhenKeyMissing(t *testing.T) {
	oldEnable := remoteLocalEnableClient
	oldLogin := remoteLoginHTTPClient
	oldStore := remoteAuthStorePath
	oldOpen := openBrowser
	t.Cleanup(func() {
		remoteLocalEnableClient = oldEnable
		remoteLoginHTTPClient = oldLogin
		remoteAuthStorePath = oldStore
		openBrowser = oldOpen
	})
	remoteLocalEnableClient = func(ctx context.Context, socketPath string, logFile string, params remoteprotocol.LocalEnableParams) (*remoteprotocol.LocalStatus, error) {
		t.Fatal("remote local enable client must not be called for cloud enable")
		return nil, nil
	}
	authStore := filepath.Join(t.TempDir(), "remote-auth.json")
	remoteAuthStorePath = func(configPath string) (string, error) {
		return authStore, nil
	}
	var opened []string
	openBrowser = func(rawURL string) error {
		opened = append(opened, rawURL)
		return nil
	}
	var validatedToken string
	var pollCalls int
	remoteLoginHTTPClient = remoteLoginHTTPClientFunc{
		createBrowserLoginFunc: func(ctx context.Context, controlURL string, clientName string) (remoteBrowserLoginResult, error) {
			if controlURL != "https://control.example.test" || clientName != "termx local service" {
				t.Fatalf("unexpected browser request control=%q client=%q", controlURL, clientName)
			}
			return remoteBrowserLoginResult{
				BrowserLoginCode:        "browser-1",
				UserCode:                "USER-CODE",
				VerificationURIComplete: "https://control.example.test/device-login?code=USER-CODE",
				ExpiresAt:               time.Now().Add(time.Minute),
				Interval:                time.Nanosecond,
			}, nil
		},
		pollBrowserLoginFunc: func(ctx context.Context, controlURL string, browserLoginCode string) (remoteLoginAuthResult, bool, error) {
			pollCalls++
			if pollCalls == 1 {
				return remoteLoginAuthResult{}, false, nil
			}
			return remoteLoginAuthResult{
				AccessToken:  "device-cloud-secret",
				RefreshToken: "device-refresh",
				User:         remoteLoginUser{Email: "cloud@example.com"},
			}, true, nil
		},
		meFunc: func(ctx context.Context, controlURL string, token string) (remoteLoginUser, error) {
			validatedToken = token
			return remoteLoginUser{Email: "cloud@example.com"}, nil
		},
	}

	configPath := filepath.Join(t.TempDir(), "termx.yaml")
	cmd := newRootCmd()
	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd.SetArgs([]string{"--config", configPath, "remote", "enable", "--cloud", "--server", "https://control.example.test", "--json"})
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("cloud enable returned error: %v", err)
	}
	if len(opened) != 1 || opened[0] != "https://control.example.test/device-login?code=USER-CODE" {
		t.Fatalf("unexpected opened login urls: %#v", opened)
	}
	if validatedToken != "device-cloud-secret" {
		t.Fatalf("expected browser token validation, got %q", validatedToken)
	}
	if strings.Contains(out.String(), "login_url") {
		t.Fatalf("JSON stdout should not contain progress output: %q", out.String())
	}
	if !strings.Contains(errOut.String(), "login_url:") {
		t.Fatalf("expected login progress on stderr, got %q", errOut.String())
	}
	var decoded struct {
		Enabled    bool   `json:"enabled"`
		ControlURL string `json:"control_url"`
		AuthStore  string `json:"auth_store"`
	}
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("cloud enable output is not JSON: %v\n%s", err, out.String())
	}
	if !decoded.Enabled || decoded.ControlURL != "https://control.example.test" || decoded.AuthStore == "" {
		t.Fatalf("unexpected cloud enable JSON: %#v", decoded)
	}
	cfg, err := remoteConfigFromFileAndEnv(configPath)
	if err != nil {
		t.Fatalf("load cloud config: %v", err)
	}
	if !cfg.Enabled || cfg.ControlURL != "https://control.example.test" || cfg.AccessToken != "device-cloud-secret" {
		t.Fatalf("unexpected cloud remote config: %#v", cfg)
	}
	var stored struct {
		RefreshToken string `json:"refresh_token"`
	}
	if data, err := os.ReadFile(authStore); err != nil {
		t.Fatalf("read auth store: %v", err)
	} else if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatalf("decode auth store: %v", err)
	}
	if stored.RefreshToken != "device-refresh" {
		t.Fatalf("expected refresh token to be persisted, got %q", stored.RefreshToken)
	}
}

func TestRemoteLocalOnlyAliasEnablesRuntime(t *testing.T) {
	oldEnable := remoteLocalEnableClient
	t.Cleanup(func() {
		remoteLocalEnableClient = oldEnable
	})
	var gotParams remoteprotocol.LocalEnableParams
	remoteLocalEnableClient = func(ctx context.Context, socketPath string, logFile string, params remoteprotocol.LocalEnableParams) (*remoteprotocol.LocalStatus, error) {
		gotParams = params
		return &remoteprotocol.LocalStatus{
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

	var gotParams remoteprotocol.PairStartParams
	pairStartClient = func(ctx context.Context, socketPath string, logFile string, params remoteprotocol.PairStartParams) (*remoteprotocol.PairStartResult, error) {
		gotParams = params
		return &remoteprotocol.PairStartResult{
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

	var decoded remoteprotocol.PairStartResult
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
