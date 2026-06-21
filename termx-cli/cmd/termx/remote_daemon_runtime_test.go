package main

import (
	"context"
	"io"
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

type daemonRemoteLifecycleFake struct {
	mu               sync.Mutex
	startCalls       int
	closeCalls       int
	localEnableCalls int
	status           coreprotocol.RemoteStatus
	localStatus      coreprotocol.RemoteLocalStatus
	localParams      coreprotocol.RemoteLocalEnableParams
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
	return fake.localStatus, nil
}

func (fake *daemonRemoteLifecycleFake) LocalStatus(context.Context) (coreprotocol.RemoteLocalStatus, error) {
	return fake.localStatus, nil
}

func (fake *daemonRemoteLifecycleFake) LocalDisable(context.Context) (coreprotocol.RemoteLocalStatus, error) {
	return fake.localStatus, nil
}
