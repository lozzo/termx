package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	coreprotocol "github.com/lozzow/termx/internal/protocol"
	corev2 "github.com/lozzow/termx/termx-core-v2"
	remote "github.com/lozzow/termx/termx-remote"
	remoteprotocol "github.com/lozzow/termx/termx-remote/protocol"
)

func TestConfigureCoreV2DaemonRemoteRuntimeInjectsHookAndAutoEnablesLocal(t *testing.T) {
	oldNewService := newCoreV2DaemonRemoteLifecycleService
	t.Cleanup(func() {
		newCoreV2DaemonRemoteLifecycleService = oldNewService
	})
	fake := &daemonRemoteLifecycleFake{
		status: coreprotocol.RemoteStatus{State: "online", DeviceID: "machine-1"},
		localStatus: coreprotocol.RemoteLocalStatus{
			Enabled:      true,
			HTTPURL:      "http://127.0.0.1:18888",
			LocalWebAddr: "127.0.0.1:0",
			UpdatedAt:    time.Now().UTC(),
		},
	}
	var gotCfg remoteprotocol.Config
	var gotDaemon remote.Daemon
	newCoreV2DaemonRemoteLifecycleService = func(cfg remoteprotocol.Config, daemon remote.Daemon) coreV2RemoteLifecycleService {
		gotCfg = cfg
		gotDaemon = daemon
		return fake
	}
	server := corev2.NewServer(corev2.WithProcessFactory(&remoteAdapterProcessFactory{}))
	cfg := remoteprotocol.Config{
		Enabled:      true,
		Mode:         "local",
		LocalWebAddr: "127.0.0.1:0",
		ICETCPAddr:   "127.0.0.1:0",
		HubURLs:      []string{"https://hub.example"},
		ControlURL:   "https://control.example",
		AccessToken:  "token",
		Region:       "local",
	}

	runtime, err := configureCoreV2DaemonRemoteRuntime(context.Background(), server, cfg, nil)
	if err != nil {
		t.Fatalf("configureCoreV2DaemonRemoteRuntime returned error: %v", err)
	}
	if runtime == nil {
		t.Fatal("expected remote runtime for enabled config")
	}
	if gotCfg.ControlURL != "https://control.example" || gotDaemon == nil {
		t.Fatalf("remote lifecycle service was not built with config/daemon, cfg=%#v daemon=%#v", gotCfg, gotDaemon)
	}
	if fake.startCalls != 1 || fake.localEnableCalls != 1 {
		t.Fatalf("expected start and local auto-enable, start=%d local=%d", fake.startCalls, fake.localEnableCalls)
	}
	if fake.localParams.LocalWebAddr != "127.0.0.1:0" || fake.localParams.ICETCPAddr != "127.0.0.1:0" || fake.localParams.AccessToken != "token" {
		t.Fatalf("unexpected local auto-enable params %#v", fake.localParams)
	}
	status, err := server.RemoteService().Status(context.Background())
	if err != nil {
		t.Fatalf("remote hook status returned error: %v", err)
	}
	if status.DeviceID != "machine-1" {
		t.Fatalf("remote hook not injected into core-v2 server: %#v", status)
	}
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatalf("runtime close returned error: %v", err)
	}
	if fake.closeCalls != 1 {
		t.Fatalf("expected close call, got %d", fake.closeCalls)
	}
	if server.RemoteService() != nil {
		t.Fatalf("remote hook should be cleared after runtime close, got %#v", server.RemoteService())
	}
}

func TestConfigureCoreV2DaemonRemoteRuntimeSkipsDisabledConfig(t *testing.T) {
	server := corev2.NewServer(corev2.WithProcessFactory(&remoteAdapterProcessFactory{}))
	runtime, err := configureCoreV2DaemonRemoteRuntime(context.Background(), server, remoteprotocol.Config{}, nil)
	if err != nil {
		t.Fatalf("configure disabled remote returned error: %v", err)
	}
	if runtime != nil || server.RemoteService() != nil {
		t.Fatalf("disabled remote must not inject service, runtime=%#v service=%#v", runtime, server.RemoteService())
	}
}

func TestDefaultDaemonConfiguresRemoteLifecycleFromEnvironment(t *testing.T) {
	oldNewService := newCoreV2DaemonRemoteLifecycleService
	t.Cleanup(func() {
		newCoreV2DaemonRemoteLifecycleService = oldNewService
	})
	fake := &daemonRemoteLifecycleFake{
		status: coreprotocol.RemoteStatus{State: "configured", DeviceID: "machine-env"},
		localStatus: coreprotocol.RemoteLocalStatus{
			Enabled:      true,
			HTTPURL:      "http://127.0.0.1:18888",
			LocalWebAddr: "127.0.0.1:0",
			UpdatedAt:    time.Now().UTC(),
		},
	}
	var gotCfg remoteprotocol.Config
	newCoreV2DaemonRemoteLifecycleService = func(cfg remoteprotocol.Config, daemon remote.Daemon) coreV2RemoteLifecycleService {
		gotCfg = cfg
		return fake
	}
	t.Setenv("TERMX_REMOTE_ENABLE", "true")
	t.Setenv("TERMX_REMOTE_MODE", "local")
	t.Setenv("TERMX_REMOTE_LOCAL_WEB_ADDR", "127.0.0.1:0")
	t.Setenv("TERMX_REMOTE_LOCAL_ICE_TCP_ADDR", "127.0.0.1:0")
	t.Setenv("TERMX_REMOTE_DATA_DIR", t.TempDir())
	t.Setenv("TERMX_REMOTE_DEVICE_NAME", "daemon-test")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cmd := newRootCmd()
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--socket", t.TempDir() + "/termx.sock", "--log-file", t.TempDir() + "/termx.log", "daemon"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("daemon command returned error: %v", err)
	}
	if !gotCfg.Enabled || gotCfg.Mode != "local" || gotCfg.DeviceName != "daemon-test" {
		t.Fatalf("daemon did not load remote config from env: %#v", gotCfg)
	}
	if fake.startCalls != 1 || fake.localEnableCalls != 1 || fake.closeCalls != 1 {
		t.Fatalf("unexpected lifecycle calls start=%d local=%d close=%d", fake.startCalls, fake.localEnableCalls, fake.closeCalls)
	}
}

func TestConfigureRemoteRuntimeKeepsDaemonAliveWhenLocalAutoEnableFails(t *testing.T) {
	oldNewService := newCoreV2DaemonRemoteLifecycleService
	t.Cleanup(func() {
		newCoreV2DaemonRemoteLifecycleService = oldNewService
	})
	fake := &daemonRemoteLifecycleFake{
		status:         coreprotocol.RemoteStatus{State: "configured", DeviceID: "machine-bind-fail"},
		localEnableErr: fmt.Errorf("listen tcp 192.168.0.103:18888: bind: can't assign requested address"),
	}
	newCoreV2DaemonRemoteLifecycleService = func(cfg remoteprotocol.Config, daemon remote.Daemon) coreV2RemoteLifecycleService {
		return fake
	}
	server := corev2.NewServer()
	runtime, err := configureCoreV2DaemonRemoteRuntime(context.Background(), server, remoteprotocol.Config{
		Enabled:      true,
		Mode:         "local",
		LocalWebAddr: "192.168.0.103:18888",
		ICETCPAddr:   "127.0.0.1:0",
	}, nil)
	if err != nil {
		t.Fatalf("local auto-enable failure must not stop core daemon startup: %v", err)
	}
	if runtime == nil || server.RemoteService() == nil {
		t.Fatalf("remote hook should remain installed after local auto-enable warning, runtime=%#v service=%#v", runtime, server.RemoteService())
	}
	if fake.startCalls != 1 || fake.localEnableCalls != 1 || fake.closeCalls != 0 {
		t.Fatalf("unexpected lifecycle calls start=%d local=%d close=%d", fake.startCalls, fake.localEnableCalls, fake.closeCalls)
	}
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatalf("close remote runtime: %v", err)
	}
}

func TestDefaultDaemonLoadsRemoteConfigFromExplicitPath(t *testing.T) {
	oldNewService := newCoreV2DaemonRemoteLifecycleService
	t.Cleanup(func() {
		newCoreV2DaemonRemoteLifecycleService = oldNewService
	})
	fake := &daemonRemoteLifecycleFake{
		status: coreprotocol.RemoteStatus{State: "configured", DeviceID: "machine-file"},
		localStatus: coreprotocol.RemoteLocalStatus{
			Enabled:      true,
			HTTPURL:      "http://127.0.0.1:18888",
			LocalWebAddr: "127.0.0.1:0",
			UpdatedAt:    time.Now().UTC(),
		},
	}
	var gotCfg remoteprotocol.Config
	newCoreV2DaemonRemoteLifecycleService = func(cfg remoteprotocol.Config, daemon remote.Daemon) coreV2RemoteLifecycleService {
		gotCfg = cfg
		return fake
	}
	configPath := filepath.Join(t.TempDir(), "custom-termx.yaml")
	if err := os.WriteFile(configPath, []byte(`remote:
  enabled: true
  mode: local
  device_name: daemon-file-device
  local_web_addr: 127.0.0.1:0
  ice_tcp_addr: 127.0.0.1:0
`), 0o600); err != nil {
		t.Fatalf("write remote config: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cmd := newRootCmd()
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--socket", filepath.Join(t.TempDir(), "termx.sock"), "--log-file", filepath.Join(t.TempDir(), "termx.log"), "--config", configPath, "daemon"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("daemon command returned error: %v", err)
	}
	if !gotCfg.Enabled || gotCfg.DeviceName != "daemon-file-device" || gotCfg.Mode != "local" {
		t.Fatalf("daemon did not load explicit remote config: %#v", gotCfg)
	}
	if fake.startCalls != 1 || fake.localEnableCalls != 1 || fake.closeCalls != 1 {
		t.Fatalf("unexpected lifecycle calls start=%d local=%d close=%d", fake.startCalls, fake.localEnableCalls, fake.closeCalls)
	}
}

func TestV3DaemonLoadsRemoteConfigFromExplicitPath(t *testing.T) {
	oldNewService := newCoreV2DaemonRemoteLifecycleService
	t.Cleanup(func() {
		newCoreV2DaemonRemoteLifecycleService = oldNewService
	})
	fake := &daemonRemoteLifecycleFake{
		status: coreprotocol.RemoteStatus{State: "configured", DeviceID: "machine-v3-file"},
		localStatus: coreprotocol.RemoteLocalStatus{
			Enabled:      true,
			HTTPURL:      "http://127.0.0.1:18888",
			LocalWebAddr: "127.0.0.1:0",
			UpdatedAt:    time.Now().UTC(),
		},
	}
	var gotCfg remoteprotocol.Config
	newCoreV2DaemonRemoteLifecycleService = func(cfg remoteprotocol.Config, daemon remote.Daemon) coreV2RemoteLifecycleService {
		gotCfg = cfg
		return fake
	}
	configPath := filepath.Join(t.TempDir(), "v3-termx.yaml")
	if err := os.WriteFile(configPath, []byte(`remote:
  enabled: true
  mode: local
  device_name: daemon-v3-file-device
  local_web_addr: 127.0.0.1:0
  ice_tcp_addr: 127.0.0.1:0
`), 0o600); err != nil {
		t.Fatalf("write remote config: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cmd := newRootCmd()
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--socket", filepath.Join(t.TempDir(), "termx.sock"), "--log-file", filepath.Join(t.TempDir(), "termx.log"), "--config", configPath, "v3", "daemon"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("v3 daemon command returned error: %v", err)
	}
	if !gotCfg.Enabled || gotCfg.DeviceName != "daemon-v3-file-device" || gotCfg.Mode != "local" {
		t.Fatalf("v3 daemon did not load explicit remote config: %#v", gotCfg)
	}
}

func TestRemoteStatusLocalPairCommandsRouteThroughCoreV2DaemonService(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "termx.sock")
	remoteDataDir := filepath.Join(t.TempDir(), "remote-data")
	server := corev2.NewServer(corev2.WithSocketPath(socketPath), corev2.WithProcessFactory(&remoteAdapterProcessFactory{}))
	cfg := remoteprotocol.Config{
		Enabled:      true,
		Mode:         "local",
		DataDir:      remoteDataDir,
		DeviceName:   "smoke-device",
		LocalWebAddr: "127.0.0.1:0",
		ICETCPAddr:   "127.0.0.1:0",
	}
	runtime, err := configureCoreV2DaemonRemoteRuntime(context.Background(), server, cfg, nil)
	if err != nil {
		t.Fatalf("configure remote runtime: %v", err)
	}
	defer func() {
		if err := runtime.Close(context.Background()); err != nil {
			t.Fatalf("close remote runtime: %v", err)
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- server.ListenAndServe(ctx)
	}()
	defer func() {
		cancel()
		_ = server.Shutdown(context.Background())
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("core-v2 server did not stop")
		}
	}()
	if err := waitForSocket(socketPath, 2*time.Second, func() error {
		client, err := dialV3Client(socketPath)
		if err != nil {
			return err
		}
		return client.Close()
	}); err != nil {
		t.Fatalf("core-v2 daemon did not become ready: %v", err)
	}

	statusOut := runRemoteCLISmokeCommand(t, "--socket", socketPath, "remote", "status")
	if !strings.Contains(statusOut, "device_name:\tsmoke-device") ||
		!strings.Contains(statusOut, "local_enabled:\ttrue") ||
		!strings.Contains(statusOut, "local_web_url:\thttp://") ||
		!strings.Contains(statusOut, "ice_tcp_enabled:\ttrue") {
		t.Fatalf("remote status did not route through running core-v2 remote service:\n%s", statusOut)
	}

	enableOut := runRemoteCLISmokeCommand(t, "--socket", socketPath, "remote", "enable", "--mode", "local", "--addr", "127.0.0.1:0", "--ice-tcp-addr", "127.0.0.1:0")
	if !strings.Contains(enableOut, "local_enabled:\ttrue") || !strings.Contains(enableOut, "local_pair_url:\thttp://") {
		t.Fatalf("remote local enable did not return service local status:\n%s", enableOut)
	}

	pairOut := runRemoteCLISmokeCommand(t, "--socket", socketPath, "remote", "pair", "--json", "--ttl", "30s", "--auth-ttl", "2h")
	var pair struct {
		URI     string         `json:"uri"`
		Payload map[string]any `json:"payload"`
	}
	if err := json.Unmarshal([]byte(pairOut), &pair); err != nil {
		t.Fatalf("pair output is not JSON: %v\n%s", err, pairOut)
	}
	if !strings.HasPrefix(pair.URI, "termx://pair?payload=") {
		t.Fatalf("unexpected pair URI: %s", pair.URI)
	}
	machine, ok := pair.Payload["machine"].(map[string]any)
	if !ok || machine["name"] != "smoke-device" {
		t.Fatalf("pair payload did not come from termx-remote service identity: %#v", pair.Payload)
	}
	pairing, ok := pair.Payload["pairing"].(map[string]any)
	if !ok || strings.TrimSpace(asString(pairing["session_id"])) == "" || strings.TrimSpace(asString(pairing["answer_proof_secret"])) == "" {
		t.Fatalf("pair payload missing termx-remote session fields: %#v", pair.Payload)
	}

	disableOut := runRemoteCLISmokeCommand(t, "--socket", socketPath, "remote", "disable", "--json")
	var disabled remoteprotocol.LocalStatus
	if err := json.Unmarshal([]byte(disableOut), &disabled); err != nil {
		t.Fatalf("disable output is not JSON: %v\n%s", err, disableOut)
	}
	if disabled.Enabled {
		t.Fatalf("remote disable should stop local runtime, got %#v", disabled)
	}
	var finalLocal coreprotocol.RemoteLocalStatus
	client, err := dialV3Client(socketPath)
	if err != nil {
		t.Fatalf("dial final local status: %v", err)
	}
	defer client.Close()
	if err := client.Call(context.Background(), "remote.local.status", map[string]any{}, &finalLocal); err != nil {
		t.Fatalf("remote.local.status after CLI disable: %v", err)
	}
	if finalLocal.Enabled {
		t.Fatalf("core-v2 remote hook still reports local enabled after CLI disable: %#v", finalLocal)
	}
}

func runRemoteCLISmokeCommand(t *testing.T, args ...string) string {
	t.Helper()
	cmd := newRootCmd()
	cmd.SetArgs(args)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("termx %s returned error: %v", strings.Join(args, " "), err)
	}
	return out.String()
}

func asString(value any) string {
	text, _ := value.(string)
	return text
}

type daemonRemoteLifecycleFake struct {
	mu               sync.Mutex
	startCalls       int
	closeCalls       int
	localEnableCalls int
	status           coreprotocol.RemoteStatus
	localStatus      coreprotocol.RemoteLocalStatus
	localParams      coreprotocol.RemoteLocalEnableParams
	localEnableErr   error
}

func (fake *daemonRemoteLifecycleFake) Start(context.Context) error {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.startCalls++
	return nil
}

func (fake *daemonRemoteLifecycleFake) Close(context.Context) error {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.closeCalls++
	return nil
}

func (fake *daemonRemoteLifecycleFake) Status(context.Context) (coreprotocol.RemoteStatus, error) {
	return fake.status, nil
}

func (fake *daemonRemoteLifecycleFake) PairStart(context.Context, coreprotocol.RemotePairStartParams) (coreprotocol.RemotePairStartResult, error) {
	return coreprotocol.RemotePairStartResult{}, nil
}

func (fake *daemonRemoteLifecycleFake) LocalEnable(_ context.Context, params coreprotocol.RemoteLocalEnableParams) (coreprotocol.RemoteLocalStatus, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.localEnableCalls++
	fake.localParams = params
	fake.localParams.HubURLs = append([]string(nil), params.HubURLs...)
	if fake.localEnableErr != nil {
		return coreprotocol.RemoteLocalStatus{}, fake.localEnableErr
	}
	return fake.localStatus, nil
}

func (fake *daemonRemoteLifecycleFake) LocalStatus(context.Context) (coreprotocol.RemoteLocalStatus, error) {
	return fake.localStatus, nil
}

func (fake *daemonRemoteLifecycleFake) LocalDisable(context.Context) (coreprotocol.RemoteLocalStatus, error) {
	return fake.localStatus, nil
}
