package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lozzow/termx/internal/protocol"
	termx "github.com/lozzow/termx/termx-core"
	corev2 "github.com/lozzow/termx/termx-core-v2"
	"github.com/lozzow/termx/termx-remote/discovery"
	remoteprotocol "github.com/lozzow/termx/termx-remote/protocol"
	pb "github.com/lozzow/termx/termx-remote/protocol/hubgrpc"
	"github.com/lozzow/termx/termx-shared/transport"
	tuiv3 "github.com/lozzow/termx/termx-tui-v3"
	"github.com/lozzow/termx/termx-tui-v3/app"
	tuiinput "github.com/lozzow/termx/termx-tui-v3/input"
	"github.com/lozzow/termx/termx-tui-v3/render"
	tuiservices "github.com/lozzow/termx/termx-tui-v3/services"
	tuistate "github.com/lozzow/termx/termx-tui-v3/state"
	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/spf13/cobra"
)

func TestRootCmdRoutesToTUIv3ByDefault(t *testing.T) {
	oldInteractive := isInteractiveTerminal
	oldRunRoot := runV3Root
	oldRunv2 := runTUIv2
	t.Cleanup(func() {
		isInteractiveTerminal = oldInteractive
		runV3Root = oldRunRoot
		runTUIv2 = oldRunv2
	})

	isInteractiveTerminal = func() bool { return true }
	socketPath := filepath.Join(t.TempDir(), "termx-v2.sock")
	logPath := filepath.Join(t.TempDir(), "termx.log")
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)

	var gotCfg v3RootConfig
	calledRoot := false
	runV3Root = func(ctx context.Context, cfg v3RootConfig) error {
		calledRoot = true
		gotCfg = cfg
		return nil
	}
	runTUIv2 = func(cfg shared.Config, stdin io.Reader, stdout io.Writer) error {
		t.Fatal("default root command must not call tuiv2")
		return nil
	}

	cmd := newRootCmd()
	cmd.SetArgs([]string{"--socket", socketPath, "--log-file", logPath, "--config", filepath.Join(t.TempDir(), "ignored.yaml")})
	cmd.SetIn(bytes.NewBuffer(nil))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !calledRoot {
		t.Fatal("expected default root command to call tui-v3 root runner")
	}
	if gotCfg.SocketPath != socketPath || gotCfg.LogFile != logPath {
		t.Fatalf("unexpected v3 root config %#v", gotCfg)
	}
	configPath := filepath.Join(configHome, "termx", "termx.yaml")
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("default root must not create tuiv2 config at %s, stat err=%v", configPath, err)
	}
}

func TestLegacyRootRoutesToTUIv2(t *testing.T) {
	oldInteractive := isInteractiveTerminal
	oldRunRoot := runV3Root
	oldRunv2 := runTUIv2
	t.Cleanup(func() {
		isInteractiveTerminal = oldInteractive
		runV3Root = oldRunRoot
		runTUIv2 = oldRunv2
	})

	isInteractiveTerminal = func() bool { return true }
	stateHome := t.TempDir()
	configHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("XDG_CONFIG_HOME", configHome)

	runV3Root = func(ctx context.Context, cfg v3RootConfig) error {
		t.Fatal("legacy root command must not call tui-v3 root runner")
		return nil
	}
	var gotCfg shared.Config
	called := false
	runTUIv2 = func(cfg shared.Config, stdin io.Reader, stdout io.Writer) error {
		called = true
		gotCfg = cfg
		return nil
	}

	cmd := newRootCmd()
	cmd.SetArgs([]string{"--log-file", filepath.Join(t.TempDir(), "termx.log"), "legacy"})
	cmd.SetIn(bytes.NewBuffer(nil))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !called {
		t.Fatal("expected legacy root command to call runTUIv2")
	}
	if gotCfg.Workspace != "main" {
		t.Fatalf("expected workspace=main, got %q", gotCfg.Workspace)
	}
	if gotCfg.SessionID != "main" {
		t.Fatalf("expected session=main, got %q", gotCfg.SessionID)
	}
	if gotCfg.AttachID != "" {
		t.Fatalf("expected empty attach id for legacy root command, got %q", gotCfg.AttachID)
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
	oldRunRoot := runV3Root
	oldRunv2 := runTUIv2
	t.Cleanup(func() {
		isInteractiveTerminal = oldInteractive
		runV3Root = oldRunRoot
		runTUIv2 = oldRunv2
	})

	isInteractiveTerminal = func() bool { return true }
	runV3Root = func(ctx context.Context, cfg v3RootConfig) error {
		t.Fatal("runV3Root should not be called when nested TUI is blocked")
		return nil
	}
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

func TestAttachCmdRoutesToTUIv3ByDefault(t *testing.T) {
	oldInteractive := isInteractiveTerminal
	oldRunAttach := runV3Attach
	oldRunv2 := runTUIv2
	t.Cleanup(func() {
		isInteractiveTerminal = oldInteractive
		runV3Attach = oldRunAttach
		runTUIv2 = oldRunv2
	})

	socketPath := filepath.Join(t.TempDir(), "termx-v2.sock")
	logPath := filepath.Join(t.TempDir(), "termx.log")
	isInteractiveTerminal = func() bool { return true }
	runTUIv2 = func(cfg shared.Config, stdin io.Reader, stdout io.Writer) error {
		t.Fatal("default attach command must not call tuiv2")
		return nil
	}
	var got v3AttachConfig
	runV3Attach = func(ctx context.Context, cfg v3AttachConfig) error {
		got = cfg
		return nil
	}

	cmd := newRootCmd()
	cmd.SetArgs([]string{"--socket", socketPath, "--log-file", logPath, "attach", "term-001"})
	cmd.SetIn(bytes.NewBuffer(nil))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected attach command to succeed, got %v", err)
	}
	if got.TerminalID != "term-001" || got.SocketPath != socketPath || got.LogFile != logPath {
		t.Fatalf("unexpected v3 attach config %#v", got)
	}
}

func TestLegacyAttachCmdRoutesToTUIv2WithAttachID(t *testing.T) {
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
	cmd.SetArgs([]string{"legacy", "attach", "term-001"})
	cmd.SetIn(bytes.NewBuffer(nil))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected legacy attach command to succeed, got %v", err)
	}
	if !called {
		t.Fatal("expected legacy attach command to call runTUIv2")
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
	oldInteractive := isInteractiveTerminal
	oldRunAttach := runV3Attach
	oldRunv2 := runTUIv2
	t.Cleanup(func() {
		isInteractiveTerminal = oldInteractive
		runV3Attach = oldRunAttach
		runTUIv2 = oldRunv2
	})

	isInteractiveTerminal = func() bool { return true }
	called := false
	runV3Attach = func(ctx context.Context, cfg v3AttachConfig) error {
		called = true
		return nil
	}
	runTUIv2 = func(cfg shared.Config, stdin io.Reader, stdout io.Writer) error {
		t.Fatal("default attach command must not call tuiv2")
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
		t.Fatal("expected attach command to reach tui-v3 runner when override is set")
	}
}

func TestAttachCmdBlocksNestedTUIByDefault(t *testing.T) {
	oldInteractive := isInteractiveTerminal
	oldRunAttach := runV3Attach
	oldRunv2 := runTUIv2
	t.Cleanup(func() {
		isInteractiveTerminal = oldInteractive
		runV3Attach = oldRunAttach
		runTUIv2 = oldRunv2
	})

	isInteractiveTerminal = func() bool { return true }
	runV3Attach = func(ctx context.Context, cfg v3AttachConfig) error {
		t.Fatal("runV3Attach should not be called when nested attach is blocked")
		return nil
	}
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

func TestLegacyRootUsesExplicitConfigPath(t *testing.T) {
	oldInteractive := isInteractiveTerminal
	oldRunRoot := runV3Root
	oldRunv2 := runTUIv2
	t.Cleanup(func() {
		isInteractiveTerminal = oldInteractive
		runV3Root = oldRunRoot
		runTUIv2 = oldRunv2
	})

	isInteractiveTerminal = func() bool { return true }
	configPath := filepath.Join(t.TempDir(), "custom-termx.yaml")

	runV3Root = func(ctx context.Context, cfg v3RootConfig) error {
		t.Fatal("legacy root command must not call tui-v3 root runner")
		return nil
	}
	var gotCfg shared.Config
	runTUIv2 = func(cfg shared.Config, stdin io.Reader, stdout io.Writer) error {
		gotCfg = cfg
		return nil
	}

	cmd := newRootCmd()
	cmd.SetArgs([]string{"--config", configPath, "legacy"})
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

func TestDefaultRootDoesNotRunTUIv2OrV3Smoke(t *testing.T) {
	oldInteractive := isInteractiveTerminal
	oldRunRoot := runV3Root
	oldRunv2 := runTUIv2
	oldRunSmoke := runTUIv3Smoke
	t.Cleanup(func() {
		isInteractiveTerminal = oldInteractive
		runV3Root = oldRunRoot
		runTUIv2 = oldRunv2
		runTUIv3Smoke = oldRunSmoke
	})

	isInteractiveTerminal = func() bool { return true }
	calledRoot := false
	calledV3Smoke := false
	runV3Root = func(ctx context.Context, cfg v3RootConfig) error {
		calledRoot = true
		return nil
	}
	runTUIv2 = func(cfg shared.Config, stdin io.Reader, stdout io.Writer) error {
		t.Fatal("default root command must not call tuiv2")
		return nil
	}
	runTUIv3Smoke = func(ctx context.Context) (render.Frame, error) {
		calledV3Smoke = true
		return render.Frame{}, nil
	}

	cmd := newRootCmd()
	cmd.SetArgs([]string{"--log-file", filepath.Join(t.TempDir(), "termx.log")})
	cmd.SetIn(bytes.NewBuffer(nil))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !calledRoot {
		t.Fatal("expected default root command to call tui-v3 root runner")
	}
	if calledV3Smoke {
		t.Fatal("default root command must not run tui-v3 smoke")
	}
}

func TestV3SmokeRunsTUIv3Smoke(t *testing.T) {
	oldRunSmoke := runTUIv3SmokeDetailed
	t.Cleanup(func() {
		runTUIv3SmokeDetailed = oldRunSmoke
	})

	called := false
	runTUIv3SmokeDetailed = func(ctx context.Context) (tuiv3.SmokeResult, error) {
		called = true
		return tuiv3.SmokeResult{Cases: []tuiv3.SmokeCase{{Name: "case-a", Frame: render.Frame{Lines: []string{"v3-line"}}}}}, nil
	}

	var out bytes.Buffer
	cmd := newRootCmd()
	cmd.SetArgs([]string{"v3", "smoke"})
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !called {
		t.Fatal("expected v3 smoke to call tui-v3 smoke runner")
	}
	text := out.String()
	if !strings.Contains(text, "termx v3 smoke ok") ||
		!strings.Contains(text, "termx-tui-v3") ||
		!strings.Contains(text, "case: case-a") ||
		!strings.Contains(text, "v3-line") {
		t.Fatalf("unexpected v3 smoke output:\n%s", text)
	}
}

func TestV3SmokeCommandIncludesVisualReviewCases(t *testing.T) {
	oldRunSmoke := runTUIv3SmokeDetailed
	t.Cleanup(func() {
		runTUIv3SmokeDetailed = oldRunSmoke
	})
	runTUIv3SmokeDetailed = tuiv3.SmokeRunDetailed

	var out bytes.Buffer
	cmd := newRootCmd()
	cmd.SetArgs([]string{"v3", "smoke"})
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	text := out.String()
	for _, want := range []string{
		"termx v3 smoke ok: tui=termx-tui-v3 cases=12",
		"case: terminal-pool-page",
		"Terminal Pool",
		"⌕ search 日志",
		"[kill]  Kill",
		"case: workbench-tree-page",
		"Workbench Tree",
		"TUI storage projection",
		"[open]  Open",
		"case: visual-audit-current",
		"visual review",
		"[]─[]",
		"case: copy-history",
		"SCROLL",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("v3 smoke output missing visual review marker %q:\n%s", want, text)
		}
	}
	for _, stale := range []string{"visual acceptance"} {
		if strings.Contains(text, stale) {
			t.Fatalf("v3 smoke output must not claim visual acceptance %q:\n%s", stale, text)
		}
	}
}

func TestV3PaneCommandAdapterParsesMiniCommand(t *testing.T) {
	var out bytes.Buffer
	cmd := newRootCmd()
	cmd.SetArgs([]string{"v3", "pane-command", "pane", "resize", "right", "delta=4", "pane=main"})
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "action=pane.resize") || !strings.Contains(text, "pane=main") || !strings.Contains(text, "source=cli-mini") {
		t.Fatalf("unexpected pane command adapter output:\n%s", text)
	}
}

func TestV3DaemonUsesCoreV2Server(t *testing.T) {
	oldNewCoreV2Server := newCoreV2Server
	oldNewServer := newServer
	t.Cleanup(func() {
		newCoreV2Server = oldNewCoreV2Server
		newServer = oldNewServer
	})

	fakeV3 := &fakeCoreV2Server{}
	newCoreV2Server = func(opts ...corev2.ServerOption) coreV2Server {
		fakeV3.newServerCalls++
		return fakeV3
	}
	newServer = func(opts ...termx.ServerOption) termxServer {
		t.Fatal("termx v3 daemon must not construct legacy termx-core server")
		return nil
	}

	cmd := newRootCmd()
	cmd.SetArgs([]string{"--socket", filepath.Join(t.TempDir(), "termx-v2.sock"), "--log-file", filepath.Join(t.TempDir(), "termx.log"), "v3", "daemon"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if fakeV3.newServerCalls != 1 || fakeV3.listenCalls != 1 || fakeV3.shutdownCalls != 1 {
		t.Fatalf("unexpected core-v2 fake server calls: new=%d listen=%d shutdown=%d", fakeV3.newServerCalls, fakeV3.listenCalls, fakeV3.shutdownCalls)
	}
}

func TestDefaultDaemonUsesCoreV2Server(t *testing.T) {
	oldNewCoreV2Server := newCoreV2Server
	oldNewServer := newServer
	t.Cleanup(func() {
		newCoreV2Server = oldNewCoreV2Server
		newServer = oldNewServer
	})

	fakeV3 := &fakeCoreV2Server{}
	newCoreV2Server = func(opts ...corev2.ServerOption) coreV2Server {
		fakeV3.newServerCalls++
		return fakeV3
	}
	newServer = func(opts ...termx.ServerOption) termxServer {
		t.Fatal("default daemon must not construct legacy termx-core server")
		return nil
	}

	cmd := newRootCmd()
	cmd.SetArgs([]string{"--socket", filepath.Join(t.TempDir(), "termx-v2.sock"), "--log-file", filepath.Join(t.TempDir(), "termx.log"), "daemon"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if fakeV3.newServerCalls != 1 || fakeV3.listenCalls != 1 || fakeV3.shutdownCalls != 1 {
		t.Fatalf("unexpected core-v2 fake server calls: new=%d listen=%d shutdown=%d", fakeV3.newServerCalls, fakeV3.listenCalls, fakeV3.shutdownCalls)
	}
}

func TestV3PingConnectsExistingCoreV2Daemon(t *testing.T) {
	oldDial := v3DialClient
	oldStart := startV3Daemon
	t.Cleanup(func() {
		v3DialClient = oldDial
		startV3Daemon = oldStart
	})

	socketPath := filepath.Join(t.TempDir(), "termx-v2.sock")
	dialed := false
	v3DialClient = func(path string) (*protocol.Client, error) {
		if path != socketPath {
			t.Fatalf("expected v3 ping to dial socket %q, got %q", socketPath, path)
		}
		dialed = true
		return nil, nil
	}
	startV3Daemon = func(path string, logFile string) error {
		t.Fatal("v3 ping must not auto-start when existing daemon is reachable")
		return nil
	}

	var out bytes.Buffer
	cmd := newRootCmd()
	cmd.SetArgs([]string{"--socket", socketPath, "--log-file", filepath.Join(t.TempDir(), "termx.log"), "v3", "ping"})
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !dialed {
		t.Fatal("expected v3 ping to dial core-v2 daemon")
	}
	if !strings.Contains(out.String(), "termx v3 daemon ok") || !strings.Contains(out.String(), socketPath) {
		t.Fatalf("unexpected v3 ping output:\n%s", out.String())
	}
}

func TestV3PingAutoStartsCoreV2Daemon(t *testing.T) {
	oldDial := v3DialClient
	oldStart := startV3Daemon
	t.Cleanup(func() {
		v3DialClient = oldDial
		startV3Daemon = oldStart
	})

	socketPath := filepath.Join(t.TempDir(), "termx-v2.sock")
	logPath := filepath.Join(t.TempDir(), "termx.log")
	dialCalls := 0
	startCalls := 0
	var startedSocket string
	var startedLog string
	v3DialClient = func(path string) (*protocol.Client, error) {
		dialCalls++
		if path != socketPath {
			t.Fatalf("expected dial socket %q, got %q", socketPath, path)
		}
		if dialCalls == 1 {
			return nil, os.ErrNotExist
		}
		return nil, nil
	}
	startV3Daemon = func(path string, logFile string) error {
		startCalls++
		startedSocket = path
		startedLog = logFile
		return nil
	}

	var out bytes.Buffer
	cmd := newRootCmd()
	cmd.SetArgs([]string{"--socket", socketPath, "--log-file", logPath, "v3", "ping"})
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if dialCalls < 2 {
		t.Fatalf("expected initial dial and post-start dial, got %d", dialCalls)
	}
	if startCalls != 1 || startedSocket != socketPath || startedLog != logPath {
		t.Fatalf("unexpected v3 daemon auto-start: calls=%d socket=%q log=%q", startCalls, startedSocket, startedLog)
	}
	if !strings.Contains(out.String(), "termx v3 daemon ok") {
		t.Fatalf("unexpected v3 ping output:\n%s", out.String())
	}
}

func TestV3PingReturnsAutoStartError(t *testing.T) {
	oldDial := v3DialClient
	oldStart := startV3Daemon
	t.Cleanup(func() {
		v3DialClient = oldDial
		startV3Daemon = oldStart
	})

	v3DialClient = func(path string) (*protocol.Client, error) {
		return nil, os.ErrNotExist
	}
	startV3Daemon = func(path string, logFile string) error {
		return os.ErrPermission
	}

	cmd := newRootCmd()
	cmd.SetArgs([]string{"--socket", filepath.Join(t.TempDir(), "termx-v2.sock"), "--log-file", filepath.Join(t.TempDir(), "termx.log"), "v3", "ping"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "start core-v2 daemon") || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("expected auto-start error, got %v", err)
	}
}

func TestV3PingConnectsRealCoreV2Daemon(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "termx-v2.sock")
	server := corev2.NewServer(corev2.WithSocketPath(socketPath))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
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
			t.Fatal("core-v2 server did not stop in time")
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

	var out bytes.Buffer
	cmd := newRootCmd()
	cmd.SetArgs([]string{"--socket", socketPath, "--log-file", filepath.Join(t.TempDir(), "termx.log"), "v3", "ping"})
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !strings.Contains(out.String(), "termx v3 daemon ok") || !strings.Contains(out.String(), socketPath) {
		t.Fatalf("unexpected v3 ping output:\n%s", out.String())
	}
}

func TestStartCoreV2DaemonCommandUsesV3Daemon(t *testing.T) {
	oldExecutable := osExecutable
	t.Cleanup(func() {
		osExecutable = oldExecutable
	})

	exe := filepath.Join(t.TempDir(), "termx")
	osExecutable = func() (string, error) {
		return exe, nil
	}
	got, err := buildStartCoreV2DaemonCommand("/tmp/termx-v2.sock", "/tmp/termx.log")
	if err != nil {
		t.Fatalf("buildStartCoreV2DaemonCommand returned error: %v", err)
	}
	if got.Path != exe {
		t.Fatalf("expected executable %q, got %q", exe, got.Path)
	}
	wantArgs := []string{exe, "--socket", "/tmp/termx-v2.sock", "--log-file", "/tmp/termx.log", "daemon"}
	if !reflect.DeepEqual(got.Args, wantArgs) {
		t.Fatalf("unexpected v3 daemon args: %#v", got.Args)
	}
}

func TestStartLegacyDaemonCommandUsesLegacyDaemon(t *testing.T) {
	oldExecutable := osExecutable
	t.Cleanup(func() {
		osExecutable = oldExecutable
	})

	exe := filepath.Join(t.TempDir(), "termx")
	osExecutable = func() (string, error) {
		return exe, nil
	}
	got, err := buildStartLegacyDaemonCommand("/tmp/termx.sock", "/tmp/termx.log")
	if err != nil {
		t.Fatalf("buildStartLegacyDaemonCommand returned error: %v", err)
	}
	if got.Path != exe {
		t.Fatalf("expected executable %q, got %q", exe, got.Path)
	}
	wantArgs := []string{exe, "--socket", "/tmp/termx.sock", "--log-file", "/tmp/termx.log", "legacy", "daemon"}
	if !reflect.DeepEqual(got.Args, wantArgs) {
		t.Fatalf("unexpected legacy daemon args: %#v", got.Args)
	}
}

func TestDefaultLocalControlCommandsUseCoreV2Protocol(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "termx-v2.sock")
	server := corev2.NewServer(corev2.WithSocketPath(socketPath))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
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
			t.Fatal("core-v2 server did not stop in time")
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

	logPath := filepath.Join(t.TempDir(), "termx.log")
	var newOut bytes.Buffer
	newCmd := newRootCmd()
	newCmd.SetArgs(append([]string{"--socket", socketPath, "--log-file", logPath, "new", "--name", "v3-demo", "--"}, testShellSleepCommand()...))
	newCmd.SetOut(&newOut)
	newCmd.SetErr(io.Discard)
	if err := newCmd.Execute(); err != nil {
		t.Fatalf("new returned error: %v", err)
	}
	terminalID := strings.TrimSpace(newOut.String())
	if terminalID == "" {
		t.Fatalf("expected v3 new to print terminal id, got %q", newOut.String())
	}

	var lsOut bytes.Buffer
	lsCmd := newRootCmd()
	lsCmd.SetArgs([]string{"--socket", socketPath, "--log-file", logPath, "ls"})
	lsCmd.SetOut(&lsOut)
	lsCmd.SetErr(io.Discard)
	if err := lsCmd.Execute(); err != nil {
		t.Fatalf("ls returned error: %v", err)
	}
	lsText := lsOut.String()
	if !strings.Contains(lsText, terminalID) ||
		!strings.Contains(lsText, "v3-demo") ||
		!strings.Contains(lsText, "/bin/sh") ||
		!strings.Contains(lsText, "running") {
		t.Fatalf("unexpected v3 ls output:\n%s", lsText)
	}

	killCmd := newRootCmd()
	killCmd.SetArgs([]string{"--socket", socketPath, "--log-file", logPath, "kill", terminalID})
	killCmd.SetOut(io.Discard)
	killCmd.SetErr(io.Discard)
	if err := killCmd.Execute(); err != nil {
		t.Fatalf("kill returned error: %v", err)
	}

	rmCmd := newRootCmd()
	rmCmd.SetArgs([]string{"--socket", socketPath, "--log-file", logPath, "rm", terminalID})
	rmCmd.SetOut(io.Discard)
	rmCmd.SetErr(io.Discard)
	if err := rmCmd.Execute(); err != nil {
		t.Fatalf("rm returned error: %v", err)
	}
	if _, err := server.GetTerminal(terminalID); err == nil || !strings.Contains(err.Error(), "terminal not found") {
		t.Fatalf("expected removed terminal lookup to fail, got %v", err)
	}

	var emptyLs bytes.Buffer
	emptyCmd := newRootCmd()
	emptyCmd.SetArgs([]string{"--socket", socketPath, "--log-file", logPath, "ls"})
	emptyCmd.SetOut(&emptyLs)
	emptyCmd.SetErr(io.Discard)
	if err := emptyCmd.Execute(); err != nil {
		t.Fatalf("ls after remove returned error: %v", err)
	}
	if strings.Contains(emptyLs.String(), terminalID) {
		t.Fatalf("removed terminal still listed:\n%s", emptyLs.String())
	}
}

func TestV3LocalControlCommandsRemainAvailable(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "termx-v2.sock")
	server := corev2.NewServer(corev2.WithSocketPath(socketPath))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
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
			t.Fatal("core-v2 server did not stop in time")
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

	var out bytes.Buffer
	cmd := newRootCmd()
	cmd.SetArgs(append([]string{"--socket", socketPath, "--log-file", filepath.Join(t.TempDir(), "termx.log"), "v3", "new", "--name", "v3-demo", "--"}, testShellSleepCommand()...))
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("v3 new returned error after default switch: %v", err)
	}
	if strings.TrimSpace(out.String()) == "" {
		t.Fatalf("expected v3 new to print terminal id, got %q", out.String())
	}
}

func TestV3AttachRejectsNonInteractiveTerminal(t *testing.T) {
	oldInteractive := isInteractiveTerminal
	oldRunAttach := runV3Attach
	t.Cleanup(func() {
		isInteractiveTerminal = oldInteractive
		runV3Attach = oldRunAttach
	})

	isInteractiveTerminal = func() bool { return false }
	runV3Attach = func(ctx context.Context, cfg v3AttachConfig) error {
		t.Fatal("non-interactive v3 attach must not start runtime")
		return nil
	}

	cmd := newRootCmd()
	cmd.SetArgs([]string{"v3", "attach", "term-1"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "termx v3 attach requires an interactive terminal") {
		t.Fatalf("expected non-interactive attach error, got %v", err)
	}
}

func TestV3AttachRoutesToTUIv3Runtime(t *testing.T) {
	oldInteractive := isInteractiveTerminal
	oldRunAttach := runV3Attach
	oldRunv2 := runTUIv2
	t.Cleanup(func() {
		isInteractiveTerminal = oldInteractive
		runV3Attach = oldRunAttach
		runTUIv2 = oldRunv2
	})

	socketPath := filepath.Join(t.TempDir(), "termx-v2.sock")
	logPath := filepath.Join(t.TempDir(), "termx.log")
	isInteractiveTerminal = func() bool { return true }
	runTUIv2 = func(cfg shared.Config, stdin io.Reader, stdout io.Writer) error {
		t.Fatal("termx v3 attach must not call tuiv2")
		return nil
	}
	var got v3AttachConfig
	runV3Attach = func(ctx context.Context, cfg v3AttachConfig) error {
		got = cfg
		return nil
	}

	cmd := newRootCmd()
	cmd.SetArgs([]string{"--socket", socketPath, "--log-file", logPath, "v3", "attach", "term-1"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if got.TerminalID != "term-1" || got.SocketPath != socketPath || got.LogFile != logPath {
		t.Fatalf("unexpected v3 attach config %#v", got)
	}
}

func TestV3RootRuntimeCreatesDefaultTerminalAndAttaches(t *testing.T) {
	server, client, closeClient := newCoreV2ProtocolClientForCLITest(t)
	defer closeClient()
	oldDial := v3DialClient
	oldStart := startV3Daemon
	oldRunAttach := runV3Attach
	t.Cleanup(func() {
		v3DialClient = oldDial
		startV3Daemon = oldStart
		runV3Attach = oldRunAttach
	})

	socketPath := filepath.Join(t.TempDir(), "termx-v2.sock")
	logPath := filepath.Join(t.TempDir(), "termx.log")
	v3DialClient = func(path string) (*protocol.Client, error) {
		if path != socketPath {
			t.Fatalf("expected v3 root to dial socket %q, got %q", socketPath, path)
		}
		return client, nil
	}
	startV3Daemon = func(path string, logFile string) error {
		t.Fatal("v3 root must not auto-start when injected client is available")
		return nil
	}
	var gotAttach v3AttachConfig
	runV3Attach = func(ctx context.Context, cfg v3AttachConfig) error {
		gotAttach = cfg
		return nil
	}

	if err := runV3RootRuntime(context.Background(), v3RootConfig{SocketPath: socketPath, LogFile: logPath}); err != nil {
		t.Fatalf("runV3RootRuntime returned error: %v", err)
	}
	if gotAttach.TerminalID != v3RootTerminalID || gotAttach.SocketPath != socketPath || gotAttach.LogFile != logPath {
		t.Fatalf("unexpected v3 root attach config %#v", gotAttach)
	}
	info, err := server.GetTerminal(v3RootTerminalID)
	if err != nil {
		t.Fatalf("expected v3 root terminal to be created: %v", err)
	}
	if info.Name != "main" || len(info.Command) == 0 || info.Size.Cols == 0 || info.Size.Rows == 0 {
		t.Fatalf("unexpected v3 root terminal info %#v", info)
	}
}

func TestV3RootRuntimeReusesRunningTerminal(t *testing.T) {
	server, client, closeClient := newCoreV2ProtocolClientForCLITest(t)
	defer closeClient()
	if _, err := client.Create(context.Background(), protocol.CreateParams{
		ID:      "term-existing",
		Name:    "existing",
		Command: testShellSleepCommand(),
		Size:    protocol.Size{Cols: 100, Rows: 30},
	}); err != nil {
		t.Fatalf("create existing terminal: %v", err)
	}
	oldDial := v3DialClient
	oldRunAttach := runV3Attach
	t.Cleanup(func() {
		v3DialClient = oldDial
		runV3Attach = oldRunAttach
	})

	socketPath := filepath.Join(t.TempDir(), "termx-v2.sock")
	logPath := filepath.Join(t.TempDir(), "termx.log")
	v3DialClient = func(path string) (*protocol.Client, error) {
		return client, nil
	}
	var gotAttach v3AttachConfig
	runV3Attach = func(ctx context.Context, cfg v3AttachConfig) error {
		gotAttach = cfg
		return nil
	}

	if err := runV3RootRuntime(context.Background(), v3RootConfig{SocketPath: socketPath, LogFile: logPath}); err != nil {
		t.Fatalf("runV3RootRuntime returned error: %v", err)
	}
	if gotAttach.TerminalID != "term-existing" {
		t.Fatalf("expected v3 root to attach existing terminal, got %#v", gotAttach)
	}
	if _, err := server.GetTerminal(v3RootTerminalID); err == nil {
		t.Fatal("v3 root should not create default terminal when a running terminal exists")
	}
}

func TestV3RemoteIsExplicitlyNotMounted(t *testing.T) {
	cmd := newRootCmd()
	cmd.SetArgs([]string{"v3", "remote", "status"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("expected v3 remote to remain unavailable, got %v", err)
	}

	root := newRootCmd()
	remote, _, err := root.Find([]string{"remote"})
	if err != nil {
		t.Fatalf("legacy/default remote command should remain mounted: %v", err)
	}
	if remote == nil || remote.Name() != "remote" {
		t.Fatalf("expected default remote command, got %#v", remote)
	}
}

func TestV3InteractiveRuntimeAttachesThroughProtocolClient(t *testing.T) {
	server, client, closeClient := newCoreV2ProtocolClientForCLITest(t)
	defer closeClient()
	if _, err := client.Create(context.Background(), protocol.CreateParams{
		ID:      "term-1",
		Name:    "attach-demo",
		Command: testShellSleepCommand(),
		Size:    protocol.Size{Cols: 100, Rows: 30},
	}); err != nil {
		t.Fatalf("create terminal: %v", err)
	}
	_ = server
	host := app.NewFakeTerminalHost(8)
	runtime := newV3InteractiveRuntime("term-1", 100, 30, client, nil, host)

	if err := runtime.Post(app.LiveAttachMsg{Config: app.LiveConfig{
		TerminalID:   "term-1",
		Cols:         100,
		Rows:         30,
		Mode:         "collaborator",
		ResizePolicy: protocol.ResizePolicyOwner,
		SurfaceID:    "test-surface",
		ViewID:       "test-view",
	}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain attach: %v", err)
	}
	if !runtime.State().Session.Attached ||
		runtime.State().Session.TerminalID != "term-1" ||
		runtime.State().Session.Channel == 0 ||
		runtime.State().Session.Cols != 100 ||
		runtime.State().Session.Rows != 30 {
		t.Fatalf("runtime did not attach through protocol client %#v", runtime.State().Session)
	}
	if len(host.Frames()) == 0 {
		t.Fatal("expected attach drain to render at least one frame")
	}
}

func TestV3InteractiveRuntimeRestoresWorkbenchFromCoreV2Storage(t *testing.T) {
	_, client, closeClient := newCoreV2ProtocolClientForCLITest(t)
	defer closeClient()
	shell := tuistate.DefaultShell().
		SplitActivePane(tuistate.PaneState{ID: "pane-restored", Title: "restored", Kind: tuistate.PaneTerminalLive, TerminalID: "term-restored"}, tuistate.SplitDirectionVertical).
		FocusPane(tuistate.PaneCommandTarget{PaneID: "pane-restored"})
	views := tuistate.TerminalViewStore{}.
		BindPane(tuistate.NewPaneTerminalView("pane-restored", "term-restored", 3, 80, 24, tuistate.TerminalResizeRoleOwner, "surface-restored", tuistate.TerminalPaneViewID("pane-restored"), true))
	value, err := tuistate.EncodeWorkbenchStorageSnapshotValue(tuistate.SnapshotRootWorkbenchForStorage(tuistate.Root{Shell: shell, TerminalViews: views}))
	if err != nil {
		t.Fatalf("encode workbench snapshot: %v", err)
	}
	ref := tuistate.DefaultWorkbenchStorageRef(tuistate.DefaultWorkspaceID)
	if _, err := client.StoragePut(context.Background(), protocol.StoragePutParams{
		AppID:   ref.AppID,
		Scope:   protocol.StorageScope(ref.Scope),
		OwnerID: ref.OwnerID,
		Key:     ref.Key,
		Value:   value,
	}); err != nil {
		t.Fatalf("put workbench snapshot: %v", err)
	}
	host := app.NewFakeTerminalHost(8)
	runtime := newV3InteractiveRuntime("term-restored", 100, 30, client, client, host)

	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain restore: %v", err)
	}
	root := runtime.State()
	if root.Shell.ActivePaneID != "pane-restored" || root.WorkbenchSync.SaveVersion() != 1 {
		t.Fatalf("runtime did not restore workbench from core-v2 storage %#v", root)
	}
	if binding, ok := root.TerminalViews.PaneBinding("pane-restored"); !ok || binding.TerminalID != "term-restored" {
		t.Fatalf("runtime did not restore terminal view binding binding=%#v ok=%v", binding, ok)
	}
}

func TestV3InteractiveRuntimeCorrectsProtocolResizeToContentRect(t *testing.T) {
	processes := newCoreV2ResizeRecordingProcessFactory()
	server, client, closeClient := newCoreV2ProtocolClientForCLITestWithOptions(t, corev2.WithProcessFactory(processes))
	defer closeClient()
	if _, err := client.Create(context.Background(), protocol.CreateParams{
		ID:      "term-1",
		Name:    "resize-demo",
		Command: testShellSleepCommand(),
		Size:    protocol.Size{Cols: 100, Rows: 30},
	}); err != nil {
		t.Fatalf("create terminal: %v", err)
	}
	_ = server
	host := app.NewFakeTerminalHost(8)
	host.SetSize(100, 30)
	runtime := newV3InteractiveRuntime("term-1", 100, 30, client, nil, host)

	if err := runtime.Post(app.LiveAttachMsg{Config: app.LiveConfig{
		TerminalID:   "term-1",
		Cols:         100,
		Rows:         30,
		Mode:         "collaborator",
		ResizePolicy: protocol.ResizePolicyOwner,
		SurfaceID:    "test-surface",
		ViewID:       "test-view",
	}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain attach: %v", err)
	}

	if runtime.State().Session.Cols != 98 || runtime.State().Session.Rows != 26 {
		t.Fatalf("runtime must correct protocol terminal size to content rect, got %#v", runtime.State().Session)
	}
	process := waitForCoreV2ResizeRecordingProcess(t, processes, "term-1")
	waitForCoreV2ProcessResize(t, process, corev2.Size{Cols: 98, Rows: 26})
	if runtime.State().Session.SurfaceID != "test-surface" || runtime.State().Session.ViewID != "test-view" {
		t.Fatalf("runtime must keep protocol resize owner metadata, session=%#v", runtime.State().Session)
	}
}

func TestV3InteractiveRuntimeLayoutResizeReachesCoreV2Process(t *testing.T) {
	processes := newCoreV2ResizeRecordingProcessFactory()
	server, client, closeClient := newCoreV2ProtocolClientForCLITestWithOptions(t, corev2.WithProcessFactory(processes))
	defer closeClient()
	if _, err := client.Create(context.Background(), protocol.CreateParams{
		ID:      "term-1",
		Name:    "resize-flow",
		Command: testShellSleepCommand(),
		Size:    protocol.Size{Cols: 100, Rows: 30},
	}); err != nil {
		t.Fatalf("create terminal: %v", err)
	}
	_ = server
	host := app.NewFakeTerminalHost(32)
	host.SetSize(100, 30)
	runtime := newV3InteractiveRuntime("term-1", 100, 30, client, nil, host)

	if err := runtime.Post(app.LiveAttachMsg{Config: app.LiveConfig{
		TerminalID:   "term-1",
		Cols:         100,
		Rows:         30,
		Mode:         "collaborator",
		ResizePolicy: protocol.ResizePolicyOwner,
		SurfaceID:    "test-surface",
		ViewID:       "test-view",
	}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	drainV3RuntimeForCLITest(t, runtime)
	process := waitForCoreV2ResizeRecordingProcess(t, processes, "term-1")
	seenResize := waitForCoreV2ProcessResize(t, process, corev2.Size{Cols: 98, Rows: 26})

	if err := host.SendResize(120, 40); err != nil {
		t.Fatalf("host resize: %v", err)
	}
	drainV3RuntimeForCLITest(t, runtime)
	seenResize = waitForCoreV2ProcessResizeAfter(t, process, corev2.Size{Cols: 118, Rows: 36}, seenResize)

	if err := runtime.Post(app.ShellSetHeaderVisibleMsg{Visible: false}); err != nil {
		t.Fatalf("post header hide: %v", err)
	}
	drainV3RuntimeForCLITest(t, runtime)
	seenResize = waitForCoreV2ProcessResizeAfter(t, process, corev2.Size{Cols: 118, Rows: 37}, seenResize)
	if err := runtime.Post(app.ShellSetFooterVisibleMsg{Visible: false}); err != nil {
		t.Fatalf("post footer hide: %v", err)
	}
	drainV3RuntimeForCLITest(t, runtime)
	seenResize = waitForCoreV2ProcessResizeAfter(t, process, corev2.Size{Cols: 118, Rows: 38}, seenResize)

	if err := runtime.Post(app.ShellPaneCommandMsg{Command: tuistate.PaneCommand{
		Action:         tuistate.PaneCommandSplit,
		SplitDirection: tuistate.SplitDirectionVertical,
		NewPane:        tuistate.PaneState{ID: "pane-2", Title: "right", Kind: tuistate.PaneTerminalLive},
		Source:         tuistate.PaneCommandSourceTest,
	}}); err != nil {
		t.Fatalf("post split: %v", err)
	}
	drainV3RuntimeForCLITest(t, runtime)
	seenResize = waitForCoreV2ProcessResizeAfter(t, process, corev2.Size{Cols: 58, Rows: 38}, seenResize)

	if err := runtime.Post(app.ShellPaneCommandMsg{Command: tuistate.PaneCommand{
		Action:          tuistate.PaneCommandResize,
		Target:          tuistate.PaneCommandTarget{PaneID: "pane-2"},
		ResizeDirection: tuistate.PaneResizeLeft,
		Delta:           6,
		Source:          tuistate.PaneCommandSourceKeyboard,
	}}); err != nil {
		t.Fatalf("post keyboard resize: %v", err)
	}
	drainV3RuntimeForCLITest(t, runtime)
	seenResize = waitForCoreV2ProcessResizeAfter(t, process, corev2.Size{Cols: 64, Rows: 38}, seenResize)

	if err := runtime.Post(app.ShellPaneCommandMsg{Command: tuistate.PaneCommand{
		Action: tuistate.PaneCommandZoom,
		Target: tuistate.PaneCommandTarget{PaneID: "pane-2"},
		Source: tuistate.PaneCommandSourceTest,
	}}); err != nil {
		t.Fatalf("post zoom: %v", err)
	}
	drainV3RuntimeForCLITest(t, runtime)
	seenResize = waitForCoreV2ProcessResizeAfter(t, process, corev2.Size{Cols: 118, Rows: 38}, seenResize)

	if err := runtime.Post(app.ShellPaneCommandMsg{Command: tuistate.PaneCommand{
		Action: tuistate.PaneCommandUnzoom,
		Source: tuistate.PaneCommandSourceTest,
	}}); err != nil {
		t.Fatalf("post unzoom: %v", err)
	}
	drainV3RuntimeForCLITest(t, runtime)
	seenResize = waitForCoreV2ProcessResizeAfter(t, process, corev2.Size{Cols: 64, Rows: 38}, seenResize)

	if err := runtime.Post(app.ShellPaneCommandMsg{Command: tuistate.PaneCommand{
		Action: tuistate.PaneCommandClose,
		Target: tuistate.PaneCommandTarget{PaneID: "pane-2"},
		Source: tuistate.PaneCommandSourceTest,
	}}); err != nil {
		t.Fatalf("post close: %v", err)
	}
	drainV3RuntimeForCLITest(t, runtime)
	waitForCoreV2ProcessResizeAfter(t, process, corev2.Size{Cols: 118, Rows: 38}, seenResize)
}

func TestV3InteractiveRuntimeMouseDividerResizeReachesCoreV2Process(t *testing.T) {
	processes := newCoreV2ResizeRecordingProcessFactory()
	server, client, closeClient := newCoreV2ProtocolClientForCLITestWithOptions(t, corev2.WithProcessFactory(processes))
	defer closeClient()
	if _, err := client.Create(context.Background(), protocol.CreateParams{
		ID:      "term-1",
		Name:    "mouse-resize-flow",
		Command: testShellSleepCommand(),
		Size:    protocol.Size{Cols: 100, Rows: 30},
	}); err != nil {
		t.Fatalf("create terminal: %v", err)
	}
	_ = server
	host := app.NewFakeTerminalHost(32)
	host.SetSize(100, 30)
	runtime := newV3InteractiveRuntime("term-1", 100, 30, client, nil, host)

	if err := runtime.Post(app.LiveAttachMsg{Config: app.LiveConfig{
		TerminalID:   "term-1",
		Cols:         100,
		Rows:         30,
		Mode:         "collaborator",
		ResizePolicy: protocol.ResizePolicyOwner,
		SurfaceID:    "test-surface",
		ViewID:       "test-view",
	}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	drainV3RuntimeForCLITest(t, runtime)
	process := waitForCoreV2ResizeRecordingProcess(t, processes, "term-1")
	seenResize := waitForCoreV2ProcessResize(t, process, corev2.Size{Cols: 98, Rows: 26})

	if err := runtime.Post(app.ShellPaneCommandMsg{Command: tuistate.PaneCommand{
		Action:         tuistate.PaneCommandSplit,
		SplitDirection: tuistate.SplitDirectionVertical,
		NewPane:        tuistate.PaneState{ID: "pane-2", Title: "right", Kind: tuistate.PaneTerminalLive},
		Source:         tuistate.PaneCommandSourceTest,
	}}); err != nil {
		t.Fatalf("post split: %v", err)
	}
	drainV3RuntimeForCLITest(t, runtime)
	seenResize = waitForCoreV2ProcessResizeAfter(t, process, corev2.Size{Cols: 48, Rows: 26}, seenResize)
	inputsBefore, _ := process.snapshot()

	divider := lastFramePaneResizeRegionForCLITest(t, host, tuistate.DefaultPaneID, tuistate.PaneResizeRight)
	start := mouseEventAtCLITest(divider.Rect)
	if err := host.SendInput(start); err != nil {
		t.Fatalf("send mouse drag start: %v", err)
	}
	drainV3RuntimeForCLITest(t, runtime)
	drag := start
	drag.Mouse = tuiinput.MouseLeftDrag
	drag.Col += 6
	if err := host.SendInput(drag); err != nil {
		t.Fatalf("send mouse drag move: %v", err)
	}
	drainV3RuntimeForCLITest(t, runtime)
	waitForCoreV2ProcessResizeAfter(t, process, corev2.Size{Cols: 42, Rows: 26}, seenResize)

	inputsAfter, _ := process.snapshot()
	if len(inputsAfter) != len(inputsBefore) {
		t.Fatalf("mouse divider drag must not leak terminal input, before=%#v after=%#v", inputsBefore, inputsAfter)
	}
}

func TestV3InteractiveRuntimeCoreV2ResizeFailureSurfacesInSession(t *testing.T) {
	processes := newCoreV2ResizeRecordingProcessFactory()
	server, client, closeClient := newCoreV2ProtocolClientForCLITestWithOptions(t, corev2.WithProcessFactory(processes))
	defer closeClient()
	if _, err := client.Create(context.Background(), protocol.CreateParams{
		ID:      "term-1",
		Name:    "resize-failure-flow",
		Command: testShellSleepCommand(),
		Size:    protocol.Size{Cols: 100, Rows: 30},
	}); err != nil {
		t.Fatalf("create terminal: %v", err)
	}
	_ = server
	host := app.NewFakeTerminalHost(32)
	host.SetSize(100, 30)
	runtime := newV3InteractiveRuntime("term-1", 100, 30, client, nil, host)

	if err := runtime.Post(app.LiveAttachMsg{Config: app.LiveConfig{
		TerminalID:   "term-1",
		Cols:         100,
		Rows:         30,
		Mode:         "collaborator",
		ResizePolicy: protocol.ResizePolicyOwner,
		SurfaceID:    "test-surface",
		ViewID:       "test-view",
	}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	drainV3RuntimeForCLITest(t, runtime)
	process := waitForCoreV2ResizeRecordingProcess(t, processes, "term-1")
	seenResize := waitForCoreV2ProcessResize(t, process, corev2.Size{Cols: 98, Rows: 26})
	process.setResizeErr(errors.New("pty resize failed"))

	if err := host.SendResize(120, 40); err != nil {
		t.Fatalf("host resize: %v", err)
	}
	drainV3RuntimeForCLITest(t, runtime)
	waitForCoreV2ProcessResizeAfter(t, process, corev2.Size{Cols: 118, Rows: 36}, seenResize)
	if !strings.Contains(runtime.State().Session.LastError, "pty resize failed") || runtime.State().Session.Attached {
		t.Fatalf("core-v2 process resize failure must surface in live session, session=%#v", runtime.State().Session)
	}
}

func TestV3TerminalServiceCreateDefaultsCommandAgainstCoreV2(t *testing.T) {
	server, client, closeClient := newCoreV2ProtocolClientForCLITest(t)
	defer closeClient()
	adapter := tuiservices.ProtocolTerminalServiceAdapter{Client: client}
	result, err := adapter.Create(context.Background(), tuiservices.TerminalCreateRequest{
		TerminalID: "term-default-command",
		Title:      "default command",
		Cols:       80,
		Rows:       24,
	})
	if err != nil {
		t.Fatalf("create through tui-v3 service adapter must not send empty command: %v", err)
	}
	t.Cleanup(func() {
		_ = server.KillTerminal(context.Background(), result.TerminalID)
		_ = server.RemoveTerminal(result.TerminalID)
	})
	info, err := server.GetTerminal(result.TerminalID)
	if err != nil {
		t.Fatalf("get created terminal: %v", err)
	}
	if len(info.Command) == 0 {
		t.Fatalf("created core-v2 terminal must have default command, info=%#v", info)
	}
}

func TestV3VisualSnapshotCommandPrintsFixedVisualFrame(t *testing.T) {
	oldRunSmoke := runTUIv3SmokeDetailed
	t.Cleanup(func() {
		runTUIv3SmokeDetailed = oldRunSmoke
	})
	runTUIv3SmokeDetailed = tuiv3.SmokeRunDetailed

	var out bytes.Buffer
	cmd := newRootCmd()
	cmd.SetArgs([]string{"v3", "visual-snapshot"})
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	text := out.String()
	for _, want := range []string{"  main", "1 main  ▎ 2 logs  ", "visual review", "[]─[]", "└──────────────────────────v┘", "[Ctrl] • [P] PANE", "ws:main float:1 terminals:1"} {
		if !strings.Contains(text, want) {
			t.Fatalf("visual snapshot missing %q:\n%s", want, text)
		}
	}
}

func TestV3VisualSnapshotCommandCanWriteANSIScreen(t *testing.T) {
	oldRunSmoke := runTUIv3SmokeDetailed
	t.Cleanup(func() {
		runTUIv3SmokeDetailed = oldRunSmoke
	})
	runTUIv3SmokeDetailed = func(ctx context.Context) (tuiv3.SmokeResult, error) {
		return tuiv3.SmokeResult{Cases: []tuiv3.SmokeCase{{
			Name: "visual-audit-current",
			Frame: render.Frame{
				Lines:     []string{"plain"},
				ANSILines: []string{"\x1b[31mstyled\x1b[0m"},
			},
		}}}, nil
	}

	var out bytes.Buffer
	cmd := newRootCmd()
	cmd.SetArgs([]string{"v3", "visual-snapshot", "--ansi"})
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "\x1b[?2026h") || !strings.Contains(text, "\x1b[31mstyled\x1b[0m") || !strings.Contains(text, "\x1b[?2026l") {
		t.Fatalf("ANSI visual snapshot should use FrameSink repaint output, got %q", text)
	}
}

func TestV3E2ESmokeCommandRunsLocalCoreAndTUIPath(t *testing.T) {
	var out bytes.Buffer
	cmd := newRootCmd()
	cmd.SetArgs([]string{"v3", "e2e-smoke"})
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("v3 e2e-smoke returned error: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "termx v3 e2e smoke ok") ||
		!strings.Contains(text, "terminal=term-") ||
		!strings.Contains(text, "frames=") ||
		!strings.Contains(text, "viewport=100x40") ||
		!strings.Contains(text, "session=98x36") ||
		!strings.Contains(text, "copy_cols=98") ||
		!strings.Contains(text, "pane_commands=5") ||
		!strings.Contains(text, "panes=1") ||
		!strings.Contains(text, "active=pane-main") ||
		!strings.Contains(text, "zoom_checked=true") {
		t.Fatalf("unexpected v3 e2e smoke output:\n%s", text)
	}
}

func TestV3TmuxSmokeHarnessCapturesArtifact(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skipf("tmux is not installed: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := runV3TmuxSmoke(ctx)
	if err != nil {
		t.Fatalf("tmux smoke: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(result.ArtifactDir)
	})
	if result.Session == "" || result.ArtifactDir == "" || result.ANSIPath == "" || result.PlainPath == "" {
		t.Fatalf("tmux smoke should return session and artifact paths, got %#v", result)
	}
	if !strings.Contains(result.Captured, "termx tmux harness ready") || !strings.Contains(result.Captured, "termx tmux input:hello-from-tmux") {
		t.Fatalf("tmux capture missing markers:\n%s", result.Captured)
	}
	for _, path := range []string{result.ANSIPath, result.PlainPath} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read artifact %s: %v", path, err)
		}
		if len(data) == 0 {
			t.Fatalf("artifact %s should not be empty", path)
		}
	}
	if err := runTmuxCommand(context.Background(), "has-session", "-t", result.Session); err == nil {
		t.Fatalf("tmux session %s should be cleaned up", result.Session)
	}
}

func TestV3TmuxSmokeCommandReportsArtifactPaths(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skipf("tmux is not installed: %v", err)
	}
	var out bytes.Buffer
	cmd := newRootCmd()
	cmd.SetArgs([]string{"v3", "tmux-smoke"})
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("v3 tmux-smoke returned error: %v", err)
	}
	text := out.String()
	if artifactDir := v3TmuxSmokeOutputValue(text, "artifact_dir"); artifactDir != "" {
		t.Cleanup(func() {
			_ = os.RemoveAll(artifactDir)
		})
	}
	if !strings.Contains(text, "termx v3 tmux smoke ok") ||
		!strings.Contains(text, "session=termx-v3-smoke-") ||
		!strings.Contains(text, "input=hello-from-tmux") ||
		!strings.Contains(text, "artifact_dir=") ||
		!strings.Contains(text, "ansi=") ||
		!strings.Contains(text, "plain=") {
		t.Fatalf("unexpected tmux smoke output:\n%s", text)
	}
}

func TestV3TmuxTerminalSmokeCreatesAttachesAndSendsInput(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skipf("tmux is not installed: %v", err)
	}
	termxBin := buildTermxBinaryForTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, err := runV3TmuxTerminalSmoke(ctx, termxBin)
	if err != nil {
		t.Fatalf("tmux terminal smoke: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(result.ArtifactDir)
	})
	if result.TerminalID == "" || result.Session == "" || result.SocketPath == "" || result.DaemonLog == "" || result.TimelinePath == "" {
		t.Fatalf("tmux terminal smoke should return ids and artifact paths, got %#v", result)
	}
	if !strings.Contains(result.Captured, "termx-pty-ready") || !strings.Contains(result.Captured, "termx-pty-echo:tmux-live-input") {
		t.Fatalf("tmux terminal capture missing live surface markers:\n%s", result.Captured)
	}
	for _, path := range []string{result.ANSIPath, result.PlainPath, result.DaemonLog, result.TimelinePath} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read artifact %s: %v", path, err)
		}
		if len(data) == 0 {
			t.Fatalf("artifact %s should not be empty", path)
		}
	}
	if err := runTmuxCommand(context.Background(), "has-session", "-t", result.Session); err == nil {
		t.Fatalf("tmux session %s should be cleaned up", result.Session)
	}
}

func TestV3TmuxTerminalSmokeCommandReportsArtifacts(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skipf("tmux is not installed: %v", err)
	}
	termxBin := buildTermxBinaryForTest(t)
	var out bytes.Buffer
	cmd := newRootCmd()
	cmd.SetArgs([]string{"v3", "tmux-terminal-smoke", "--termx-bin", termxBin})
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("v3 tmux-terminal-smoke returned error: %v", err)
	}
	text := out.String()
	if artifactDir := v3TmuxSmokeOutputValue(text, "artifact_dir"); artifactDir != "" {
		t.Cleanup(func() {
			_ = os.RemoveAll(artifactDir)
		})
	}
	if !strings.Contains(text, "termx v3 tmux terminal smoke ok") ||
		!strings.Contains(text, "terminal=term-") ||
		!strings.Contains(text, "session=termx-v3-terminal-") ||
		!strings.Contains(text, "input=tmux-live-input") ||
		!strings.Contains(text, "artifact_dir=") ||
		!strings.Contains(text, "daemon_log=") ||
		!strings.Contains(text, "socket=") ||
		!strings.Contains(text, "timeline=") {
		t.Fatalf("unexpected tmux terminal smoke output:\n%s", text)
	}
}

func TestV3TmuxResizeSmokePropagatesHostResizeToPTY(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skipf("tmux is not installed: %v", err)
	}
	termxBin := buildTermxBinaryForTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, err := runV3TmuxResizeSmoke(ctx, termxBin)
	if err != nil {
		t.Fatalf("tmux resize smoke: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(result.ArtifactDir)
	})
	if result.TerminalID == "" || result.BeforeSize == "" || result.AfterSize == "" || result.WindowSize != "120x40" {
		t.Fatalf("tmux resize smoke should return sizes and ids, got %#v", result)
	}
	if result.BeforeSize == result.AfterSize {
		t.Fatalf("resize should change PTY size, before=%s after=%s", result.BeforeSize, result.AfterSize)
	}
	if result.AfterSize != "118x36" {
		t.Fatalf("120x40 host viewport should resize PTY to tuiv2-style active content rect 118x36, got %s", result.AfterSize)
	}
	if !strings.Contains(result.Captured, "termx-pty-size:size-before:") || !strings.Contains(result.Captured, "termx-pty-size:size-after:"+result.AfterSize) {
		t.Fatalf("tmux resize capture missing PTY size markers:\n%s", result.Captured)
	}
	for _, path := range []string{result.ANSIPath, result.PlainPath, result.DaemonLog, result.TimelinePath} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read artifact %s: %v", path, err)
		}
		if len(data) == 0 {
			t.Fatalf("artifact %s should not be empty", path)
		}
	}
	if err := runTmuxCommand(context.Background(), "has-session", "-t", result.Session); err == nil {
		t.Fatalf("tmux session %s should be cleaned up", result.Session)
	}
}

func TestV3TmuxResizeSmokeCommandReportsArtifacts(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skipf("tmux is not installed: %v", err)
	}
	termxBin := buildTermxBinaryForTest(t)
	var out bytes.Buffer
	cmd := newRootCmd()
	cmd.SetArgs([]string{"v3", "tmux-resize-smoke", "--termx-bin", termxBin})
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("v3 tmux-resize-smoke returned error: %v", err)
	}
	text := out.String()
	if artifactDir := v3TmuxSmokeOutputValue(text, "artifact_dir"); artifactDir != "" {
		t.Cleanup(func() {
			_ = os.RemoveAll(artifactDir)
		})
	}
	if !strings.Contains(text, "termx v3 tmux resize smoke ok") ||
		!strings.Contains(text, "terminal=term-") ||
		!strings.Contains(text, "session=termx-v3-resize-") ||
		!strings.Contains(text, "window=120x40") ||
		!strings.Contains(text, "after=118x36") ||
		!strings.Contains(text, "artifact_dir=") ||
		!strings.Contains(text, "timeline=") {
		t.Fatalf("unexpected tmux resize smoke output:\n%s", text)
	}
}

func TestV3TmuxANSISmokeCapturesLiveSurfaceSGR(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skipf("tmux is not installed: %v", err)
	}
	termxBin := buildTermxBinaryForTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, err := runV3TmuxANSISmoke(ctx, termxBin)
	if err != nil {
		t.Fatalf("tmux ansi smoke: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(result.ArtifactDir)
	})
	for _, marker := range []string{"ANSI16_RED", "ANSI16_BLUE_BOLD", "ANSI256_ORANGE", "ANSI256_BG", "TRUECOLOR_MINT", "CR_REPLACED", "PRIMARY_AFTER_ALT"} {
		if !strings.Contains(result.Captured, marker) {
			t.Fatalf("plain capture missing marker %q:\n%s", marker, result.Captured)
		}
	}
	for _, sgr := range []string{"\x1b[31m", "\x1b[1m", "\x1b[34m", "\x1b[38;5;202m", "\x1b[48;5;24m", "\x1b[38;2;12;200;155m"} {
		if !strings.Contains(result.ANSICaptured, sgr) {
			t.Fatalf("ANSI capture missing SGR %q:\n%q", sgr, result.ANSICaptured)
		}
	}
	if strings.Contains(result.Captured, "\x1b]") {
		t.Fatalf("OSC theme responses must not leak into plain terminal capture:\n%q", result.Captured)
	}
	for _, path := range []string{result.ANSIPath, result.PlainPath, result.DaemonLog, result.TimelinePath} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read artifact %s: %v", path, err)
		}
		if len(data) == 0 {
			t.Fatalf("artifact %s should not be empty", path)
		}
	}
	if err := runTmuxCommand(context.Background(), "has-session", "-t", result.Session); err == nil {
		t.Fatalf("tmux session %s should be cleaned up", result.Session)
	}
}

func TestV3TmuxANSISmokeCommandReportsArtifacts(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skipf("tmux is not installed: %v", err)
	}
	termxBin := buildTermxBinaryForTest(t)
	var out bytes.Buffer
	cmd := newRootCmd()
	cmd.SetArgs([]string{"v3", "tmux-ansi-smoke", "--termx-bin", termxBin})
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("v3 tmux-ansi-smoke returned error: %v", err)
	}
	text := out.String()
	if artifactDir := v3TmuxSmokeOutputValue(text, "artifact_dir"); artifactDir != "" {
		t.Cleanup(func() {
			_ = os.RemoveAll(artifactDir)
		})
	}
	if !strings.Contains(text, "termx v3 tmux ansi smoke ok") ||
		!strings.Contains(text, "terminal=term-") ||
		!strings.Contains(text, "session=termx-v3-ansi-") ||
		!strings.Contains(text, "artifact_dir=") ||
		!strings.Contains(text, "ansi=") ||
		!strings.Contains(text, "plain=") ||
		!strings.Contains(text, "timeline=") {
		t.Fatalf("unexpected tmux ansi smoke output:\n%s", text)
	}
}

func TestV3TmuxEmojiDotsSmokeReproducesOwnerFollowerGeometry(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skipf("tmux is not installed: %v", err)
	}
	termxBin := buildTermxBinaryForTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, err := runV3TmuxEmojiDotsSmoke(ctx, termxBin)
	if err != nil {
		t.Fatalf("tmux emoji dots smoke: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(result.ArtifactDir)
	})
	if result.TerminalID == "" || result.BeforeSize == "" || result.AfterSize == "" || !result.DotsVisible {
		t.Fatalf("tmux emoji dots smoke should return terminal ids, PTY sizes and visible dots, got %#v", result)
	}
	if result.BeforeSize == result.AfterSize {
		t.Fatalf("owner/follower resize should change PTY size, before=%s after=%s", result.BeforeSize, result.AfterSize)
	}
	for _, marker := range []string{"◆ owner", "◇ follow", "termx-pty-size:size-before:", "termx-pty-size:size-after:", "termx-pty-echo:", "····"} {
		if !strings.Contains(result.Captured, marker) {
			t.Fatalf("tmux emoji dots capture missing marker %q:\n%s", marker, result.Captured)
		}
	}
	for _, path := range []string{result.ANSIPath, result.PlainPath, result.DaemonLog, result.TimelinePath} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read artifact %s: %v", path, err)
		}
		if len(data) == 0 {
			t.Fatalf("artifact %s should not be empty", path)
		}
	}
	if err := runTmuxCommand(context.Background(), "has-session", "-t", result.Session); err == nil {
		t.Fatalf("tmux session %s should be cleaned up", result.Session)
	}
}

func TestV3TmuxEmojiDotsSmokeCommandReportsArtifacts(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skipf("tmux is not installed: %v", err)
	}
	termxBin := buildTermxBinaryForTest(t)
	var out bytes.Buffer
	cmd := newRootCmd()
	cmd.SetArgs([]string{"v3", "tmux-emoji-dots-smoke", "--termx-bin", termxBin})
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("v3 tmux-emoji-dots-smoke returned error: %v", err)
	}
	text := out.String()
	if artifactDir := v3TmuxSmokeOutputValue(text, "artifact_dir"); artifactDir != "" {
		t.Cleanup(func() {
			_ = os.RemoveAll(artifactDir)
		})
	}
	if !strings.Contains(text, "termx v3 tmux emoji dots smoke ok") ||
		!strings.Contains(text, "terminal=term-") ||
		!strings.Contains(text, "session=termx-v3-emoji-dots-") ||
		!strings.Contains(text, "dots=true") ||
		!strings.Contains(text, "artifact_dir=") ||
		!strings.Contains(text, "timeline=") {
		t.Fatalf("unexpected tmux emoji dots smoke output:\n%s", text)
	}
}

func TestV3TmuxVisualCompareCapturesTargetAndDiffArtifacts(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skipf("tmux is not installed: %v", err)
	}
	termxBin := buildTermxBinaryForTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	result, err := runV3TmuxVisualCompare(ctx, termxBin)
	if err != nil {
		t.Fatalf("tmux visual compare: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(result.ArtifactDir)
	})
	if result.Session == "" || result.ArtifactDir == "" || result.CurrentPlainPath == "" || result.CurrentANSIPath == "" || result.TargetPath == "" || result.DiffPath == "" || result.StylePath == "" || result.StyleDiffPath == "" || result.CurrentStyleMapPath == "" || result.TargetStyleMapPath == "" || result.StyleMapDiffPath == "" || result.SummaryPath == "" {
		t.Fatalf("tmux visual compare should return artifact paths, got %#v", result)
	}
	for _, path := range []string{result.CurrentPlainPath, result.CurrentANSIPath, result.TargetPath, result.DiffPath, result.StylePath, result.StyleDiffPath, result.CurrentStyleMapPath, result.TargetStyleMapPath, result.StyleMapDiffPath, result.SummaryPath} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read artifact %s: %v", path, err)
		}
		if len(data) == 0 {
			t.Fatalf("artifact %s should not be empty", path)
		}
	}
	current, err := os.ReadFile(result.CurrentPlainPath)
	if err != nil {
		t.Fatalf("read current plain: %v", err)
	}
	target, err := os.ReadFile(result.TargetPath)
	if err != nil {
		t.Fatalf("read target plain: %v", err)
	}
	diff, err := os.ReadFile(result.DiffPath)
	if err != nil {
		t.Fatalf("read diff: %v", err)
	}
	style, err := os.ReadFile(result.StylePath)
	if err != nil {
		t.Fatalf("read style report: %v", err)
	}
	styleDiff, err := os.ReadFile(result.StyleDiffPath)
	if err != nil {
		t.Fatalf("read style diff: %v", err)
	}
	currentStyleMap, err := os.ReadFile(result.CurrentStyleMapPath)
	if err != nil {
		t.Fatalf("read current style map: %v", err)
	}
	targetStyleMap, err := os.ReadFile(result.TargetStyleMapPath)
	if err != nil {
		t.Fatalf("read target style map: %v", err)
	}
	styleMapDiff, err := os.ReadFile(result.StyleMapDiffPath)
	if err != nil {
		t.Fatalf("read style map diff: %v", err)
	}
	if !strings.Contains(string(current), "[]─[]") ||
		!strings.Contains(string(target), "[V] COPY • [F] PICKER • [G] GLOBAL") ||
		!strings.Contains(string(target), "float:1") ||
		!strings.Contains(string(diff), "tmux visual diff") {
		t.Fatalf("visual compare artifacts missing expected markers current=%q target=%q diff=%q", current, target, diff)
	}
	currentNormalized := normalizeVisualText(string(current), 140, 40)
	targetNormalized := normalizeVisualText(string(target), 140, 40)
	if result.Mismatches != 0 || !strings.Contains(string(diff), "no row mismatches") || currentNormalized != targetNormalized {
		t.Fatalf("visual compare should match target exactly, mismatches=%d diff=\n%s", result.Mismatches, diff)
	}
	if result.StyleMismatches != 0 || !strings.Contains(string(styleDiff), "no style mismatches") {
		t.Fatalf("visual compare style contract should match, style_mismatches=%d diff=\n%s", result.StyleMismatches, styleDiff)
	}
	if result.StyleMapMismatches != 0 || !strings.Contains(string(styleMapDiff), "no style map mismatches") {
		t.Fatalf("visual compare style map should match, stylemap_mismatches=%d diff=\n%s", result.StyleMapMismatches, styleMapDiff)
	}
	for _, marker := range []string{"legend: S=status A=accent M=muted W=warning G=success P=plain .=transparent ?=unknown", "01 SSSSSS"} {
		if !strings.Contains(string(currentStyleMap), marker) || !strings.Contains(string(targetStyleMap), marker) {
			t.Fatalf("style maps missing marker %q current=\n%s\ntarget=\n%s", marker, currentStyleMap, targetStyleMap)
		}
	}
	for _, marker := range []string{"pane-action-accent", "inactive-logs-muted", "right-pane-border-muted", "footer-no-bg", "footer-float-accent"} {
		if !strings.Contains(string(style), marker+" row=") {
			t.Fatalf("style report missing marker %q: %s", marker, style)
		}
	}
	if err := runTmuxCommand(context.Background(), "has-session", "-t", result.Session); err == nil {
		t.Fatalf("tmux session %s should be cleaned up", result.Session)
	}
}

func TestV3TmuxVisualCompareCommandReportsArtifacts(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skipf("tmux is not installed: %v", err)
	}
	termxBin := buildTermxBinaryForTest(t)
	var out bytes.Buffer
	cmd := newRootCmd()
	cmd.SetArgs([]string{"v3", "tmux-visual-compare", "--termx-bin", termxBin})
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("v3 tmux-visual-compare returned error: %v", err)
	}
	text := out.String()
	if artifactDir := v3TmuxSmokeOutputValue(text, "artifact_dir"); artifactDir != "" {
		t.Cleanup(func() {
			_ = os.RemoveAll(artifactDir)
		})
	}
	if !strings.Contains(text, "termx v3 tmux visual compare ok") ||
		!strings.Contains(text, "session=termx-v3-visual-") ||
		!strings.Contains(text, "mismatches=") ||
		!strings.Contains(text, "style_mismatches=") ||
		!strings.Contains(text, "stylemap_mismatches=") ||
		!strings.Contains(text, "current_plain=") ||
		!strings.Contains(text, "current_ansi=") ||
		!strings.Contains(text, "target=") ||
		!strings.Contains(text, "diff=") ||
		!strings.Contains(text, "style=") ||
		!strings.Contains(text, "style_diff=") ||
		!strings.Contains(text, "current_stylemap=") ||
		!strings.Contains(text, "target_stylemap=") ||
		!strings.Contains(text, "stylemap_diff=") {
		t.Fatalf("unexpected tmux visual compare output:\n%s", text)
	}
}

func TestV3TmuxStabilitySmokeRunsShortRound(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skipf("tmux is not installed: %v", err)
	}
	termxBin := buildTermxBinaryForTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	result, err := runV3TmuxStabilitySmoke(ctx, termxBin, 1)
	if err != nil {
		t.Fatalf("tmux stability smoke: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(result.ArtifactDir)
		for _, artifact := range result.Artifacts {
			_ = os.RemoveAll(artifact)
		}
	})
	if result.Rounds != 1 || len(result.Artifacts) != 3 || result.ArtifactDir == "" || result.TimelinePath == "" {
		t.Fatalf("unexpected stability result %#v", result)
	}
	timeline, err := os.ReadFile(result.TimelinePath)
	if err != nil {
		t.Fatalf("read stability timeline: %v", err)
	}
	if !strings.Contains(string(timeline), "terminal smoke ok") ||
		!strings.Contains(string(timeline), "resize smoke ok") ||
		!strings.Contains(string(timeline), "ansi smoke ok") {
		t.Fatalf("stability timeline missing child smoke results:\n%s", timeline)
	}
	for _, artifact := range result.Artifacts {
		if _, err := os.Stat(filepath.Join(artifact, "capture.txt")); err != nil {
			t.Fatalf("child artifact %s missing capture.txt: %v", artifact, err)
		}
	}
}

func TestV3TmuxStabilitySmokeCommandReportsArtifacts(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skipf("tmux is not installed: %v", err)
	}
	termxBin := buildTermxBinaryForTest(t)
	var out bytes.Buffer
	cmd := newRootCmd()
	cmd.SetArgs([]string{"v3", "tmux-stability-smoke", "--termx-bin", termxBin, "--rounds", "1"})
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("v3 tmux-stability-smoke returned error: %v", err)
	}
	text := out.String()
	if artifactDir := v3TmuxSmokeOutputValue(text, "artifact_dir"); artifactDir != "" {
		t.Cleanup(func() {
			_ = os.RemoveAll(artifactDir)
		})
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "artifact: ") {
			artifact := strings.TrimSpace(strings.TrimPrefix(line, "artifact: "))
			t.Cleanup(func() {
				_ = os.RemoveAll(artifact)
			})
		}
	}
	if !strings.Contains(text, "termx v3 tmux stability smoke ok") ||
		!strings.Contains(text, "rounds=1") ||
		!strings.Contains(text, "artifacts=3") ||
		!strings.Contains(text, "artifact_dir=") ||
		!strings.Contains(text, "timeline=") ||
		!strings.Contains(text, "artifact: ") {
		t.Fatalf("unexpected tmux stability smoke output:\n%s", text)
	}
}

func v3TmuxSmokeOutputValue(output string, key string) string {
	prefix := key + "="
	for _, field := range strings.Fields(output) {
		if strings.HasPrefix(field, prefix) {
			return strings.TrimPrefix(field, prefix)
		}
	}
	return ""
}

func buildTermxBinaryForTest(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "termx-test-bin")
	cmd := exec.Command("go", "build", "-o", path, ".")
	cmd.Dir = "."
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build termx test binary: %v\n%s", err, output)
	}
	return path
}

func TestLegacyRemoveCmdDeletesTerminalFromDaemonInventory(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	socketPath := filepath.Join(t.TempDir(), "termx.sock")
	srv := termx.NewServer(termx.WithSocketPath(socketPath), termx.WithDefaultKeepAfterExit(10*time.Second))
	done := make(chan error, 1)
	go func() {
		done <- srv.ListenAndServe(ctx)
	}()
	defer func() {
		cancel()
		_ = srv.Shutdown(context.Background())
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("server did not stop in time")
		}
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(socketPath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for socket %s", socketPath)
		}
		time.Sleep(10 * time.Millisecond)
	}

	created, err := srv.Create(context.Background(), termx.CreateOptions{
		Command: []string{"sh", "-c", "sleep 30"},
		Name:    "remove-smoke",
	})
	if err != nil {
		t.Fatalf("create terminal: %v", err)
	}

	cmd := newRootCmd()
	cmd.SetArgs([]string{"--socket", socketPath, "--log-file", filepath.Join(t.TempDir(), "termx.log"), "legacy", "rm", created.ID})
	cmd.SetIn(bytes.NewBuffer(nil))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("rm command failed: %v", err)
	}
	if _, err := srv.Get(context.Background(), created.ID); err == nil || !strings.Contains(err.Error(), "terminal not found") {
		t.Fatalf("expected removed terminal lookup to fail, got %v", err)
	}

	aliasCmd := newRootCmd()
	aliasCmd.SetArgs([]string{"--socket", socketPath, "--log-file", filepath.Join(t.TempDir(), "termx.log"), "legacy", "delete", "missing"})
	aliasCmd.SetIn(bytes.NewBuffer(nil))
	aliasCmd.SetOut(io.Discard)
	aliasCmd.SetErr(io.Discard)
	err = aliasCmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "protocol error 404") {
		t.Fatalf("expected delete alias to call remove protocol and return 404, got %v", err)
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
	t.Setenv("TERMX_REMOTE_MODE", "both")
	t.Setenv("TERMX_REMOTE_ALLOW_LAN", "true")
	t.Setenv("TERMX_REMOTE_LAN_IPS", "192.168.0.0/16,10.0.0.5")

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
	if cfg.Mode != "both" || !cfg.AllowLAN || len(cfg.LANIPs) != 2 || cfg.LANIPs[0] != "192.168.0.0/16" {
		t.Fatalf("unexpected mode/LAN config: %#v", cfg)
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

func TestRemoteConfigFromFileLoadsHubBootstrapWithoutRawToken(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "termx.yaml")
	content := `remote:
  enabled: true
  control_url: https://control-file.example.test
  hub_urls: [https://hub-file.example.test, https://hub2-file.example.test]
  connection_key_env: TERMX_TEST_REMOTE_KEY
  accessToken: should-not-be-used
  token: ignored-by-env-key
  data_dir: /tmp/termx-remote-file
  device_name: file-device
  region: fra
  mode: hub
  local_web_addr: 127.0.0.1:19998
  ice_tcp_addr: 127.0.0.1:19999
  token_ttl: 2h
  allow_lan: true
  lan_ips: 192.168.0.0/16,10.0.0.5
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
	if len(cfg.HubURLs) != 2 || cfg.HubURLs[1] != "https://hub2-file.example.test" {
		t.Fatalf("unexpected hub urls: %#v", cfg.HubURLs)
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
	if cfg.Mode != "hub" || !cfg.AllowLAN || len(cfg.LANIPs) != 2 || cfg.LANIPs[1] != "10.0.0.5" {
		t.Fatalf("unexpected mode/LAN config: %#v", cfg)
	}
	if cfg.LocalWebAddr != "127.0.0.1:19998" || cfg.ICETCPAddr != "127.0.0.1:19999" {
		t.Fatalf("unexpected local runtime config: %#v", cfg)
	}
	if cfg.TokenTTLSeconds != int((2 * time.Hour).Seconds()) {
		t.Fatalf("unexpected token ttl seconds: %d", cfg.TokenTTLSeconds)
	}
}

func TestRemoteConfigEnvOverridesFile(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "termx.yaml")
	content := `remote:
  enabled: true
  controlURL: https://control-file.example.test
  hubURL: https://hub-file.example.test
  connectionKeyEnv: TERMX_TEST_REMOTE_KEY_FILE
  mode: both
  allow_lan: true
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
	t.Setenv("TERMX_REMOTE_MODE", "hub")
	t.Setenv("TERMX_REMOTE_LOCAL_WEB_ADDR", "127.0.0.1:18880")
	t.Setenv("TERMX_REMOTE_LOCAL_ICE_TCP_ADDR", "127.0.0.1:18881")
	t.Setenv("TERMX_REMOTE_TOKEN_TTL", "3600")
	t.Setenv("TERMX_REMOTE_ALLOW_LAN", "false")

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
	if cfg.Mode != "hub" || cfg.AllowLAN {
		t.Fatalf("expected env mode/allow_lan override, got %#v", cfg)
	}
	if cfg.LocalWebAddr != "127.0.0.1:18880" || cfg.ICETCPAddr != "127.0.0.1:18881" {
		t.Fatalf("expected env local runtime override, got %#v", cfg)
	}
	if cfg.TokenTTLSeconds != 3600 {
		t.Fatalf("expected env token ttl override, got %d", cfg.TokenTTLSeconds)
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
			Mode:        "hub",
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
	cmd.SetArgs([]string{"--config", configPath, "legacy", "daemon", "--log-file", filepath.Join(t.TempDir(), "termx.log")})
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

func TestDaemonStartsConfiguredLocalRuntime(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "termx.yaml")
	config := `remote:
  enabled: true
  mode: both
  control_url: https://control.example.test
  access_token: loader-secret
  local_web_addr: 127.0.0.1:0
  ice_tcp_addr: 127.0.0.1:0
`
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	fake := &fakeTermxServer{}
	oldNewServer := newServer
	t.Cleanup(func() {
		newServer = oldNewServer
	})
	newServer = func(opts ...termx.ServerOption) termxServer {
		fake.newServerCalls++
		return fake
	}

	cmd := newRootCmd()
	cmd.SetArgs([]string{"--config", configPath, "legacy", "daemon", "--log-file", filepath.Join(t.TempDir(), "termx.log")})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if fake.newServerCalls != 1 || fake.listenCalls != 1 || fake.shutdownCalls != 1 {
		t.Fatalf("unexpected fake server calls: new=%d listen=%d shutdown=%d", fake.newServerCalls, fake.listenCalls, fake.shutdownCalls)
	}
}

func TestDaemonLocalAddrDefaultsFromEnabledMode(t *testing.T) {
	cfg := remoteprotocol.Config{Enabled: true, Mode: "both"}
	if got := daemonLocalWebAddr(cfg); got != defaultRemoteLocalWebAddr {
		t.Fatalf("expected default local web addr, got %q", got)
	}
	if got := daemonLocalICETCPAddr(cfg); got != defaultRemoteLocalICEAddr {
		t.Fatalf("expected default ICE TCP addr, got %q", got)
	}
}

func TestDaemonLocalAddrUsesConfigAndEnvOverride(t *testing.T) {
	cfg := remoteprotocol.Config{
		Enabled:      true,
		Mode:         "local",
		LocalWebAddr: "127.0.0.1:19998",
		ICETCPAddr:   "127.0.0.1:19999",
	}
	if got := daemonLocalWebAddr(cfg); got != "127.0.0.1:19998" {
		t.Fatalf("expected config local web addr, got %q", got)
	}
	if got := daemonLocalICETCPAddr(cfg); got != "127.0.0.1:19999" {
		t.Fatalf("expected config ICE TCP addr, got %q", got)
	}

	t.Setenv("TERMX_REMOTE_LOCAL_WEB_ADDR", "127.0.0.1:18880")
	t.Setenv("TERMX_REMOTE_LOCAL_ICE_TCP_ADDR", "127.0.0.1:18881")
	if got := daemonLocalWebAddr(cfg); got != "127.0.0.1:18880" {
		t.Fatalf("expected env local web addr override, got %q", got)
	}
	if got := daemonLocalICETCPAddr(cfg); got != "127.0.0.1:18881" {
		t.Fatalf("expected env ICE TCP addr override, got %q", got)
	}
}

func TestDaemonLocalAddrDisabledForOnlineMode(t *testing.T) {
	cfg := remoteprotocol.Config{Enabled: true, Mode: "hub"}
	if got := daemonLocalWebAddr(cfg); got != "" {
		t.Fatalf("expected no local web addr for hub mode, got %q", got)
	}
	if got := daemonLocalICETCPAddr(cfg); got != "" {
		t.Fatalf("expected no ICE TCP addr for hub mode, got %q", got)
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

	grpcClient, err := discovery.NewGRPCHubClient(baseURL, "local")
	if err != nil {
		t.Fatalf("new grpc local hub client: %v", err)
	}
	defer grpcClient.Close()
	grpcCtx, grpcCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer grpcCancel()
	stream, err := grpcClient.Connect(grpcCtx)
	if err != nil {
		t.Fatalf("connect local hub grpc: %v", err)
	}
	if err := stream.Send(&pb.AgentToHub{Payload: &pb.AgentToHub_Register{
		Register: &pb.RegisterRequest{
			DeviceId:    "manual-cli-local-hub",
			MachineId:   "manual-cli-local-hub",
			AgentId:     "agent-cli-local-hub",
			DisplayName: "TermX CLI Local Hub Test",
			Version:     "test",
			Terminals:   []*pb.Terminal{},
		},
	}}); err != nil {
		t.Fatalf("send local hub grpc register: %v", err)
	}
	msg, err := stream.Recv()
	if err != nil {
		t.Fatalf("recv local hub grpc register ack: %v", err)
	}
	ack := msg.GetRegisterAck()
	if ack.GetAgentSessionId() == "" {
		t.Fatalf("expected agent_session_id in register ack, got %#v", ack)
	}
	for _, server := range ack.GetIceServers() {
		for _, url := range server.GetUrls() {
			if strings.HasPrefix(strings.ToLower(url), "turn:") || strings.HasPrefix(strings.ToLower(url), "turns:") {
				t.Fatalf("local hub register must not expose TURN credentials: %+v", ack.GetIceServers())
			}
		}
	}

	session, err := remoteHost.service.PairStart(remoteprotocol.PairStartParams{
		LocalPairURL: baseURL + "/api/v1/pairing/claims",
		TTLSeconds:   int(time.Minute.Seconds()),
	})
	if err != nil {
		t.Fatalf("RemotePairStart returned error: %v", err)
	}
	pairPayload := strings.NewReader(`{
		"machine_id":"` + session.MachineID + `",
		"pair_session_id":"` + session.PairSessionID + `",
		"pair_secret":"` + session.PairSecret + `",
		"app_device_id":"app-cli-local-web",
		"app_name":"TermX CLI Local Web Test",
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
	if pair["session_token"] == "" {
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
		{"remote", "disable"},
		{"remote", "pair"},
		{"remote", "open"},
	} {
		if _, _, err := cmd.Find(args); err != nil {
			t.Fatalf("expected %q command to exist: %v", strings.Join(args, " "), err)
		}
	}
}

func TestRootCmdDoesNotExposeRemovedRemotePairCommands(t *testing.T) {
	cmd := newRootCmd()
	if commandHasNameOrAlias(cmd, "pair") {
		t.Fatal("top-level pair command should not be exposed")
	}
	remoteCmd := commandByNameOrAlias(cmd, "remote")
	if remoteCmd == nil {
		t.Fatal("remote command should exist")
	}
	for _, name := range []string{"qrcode", "local", "local-only", "local_only"} {
		if commandHasNameOrAlias(remoteCmd, name) {
			t.Fatalf("remote %s command should not be exposed", name)
		}
	}
}

func commandByNameOrAlias(cmd *cobra.Command, name string) *cobra.Command {
	if cmd == nil {
		return nil
	}
	for _, child := range cmd.Commands() {
		if child.Name() == name {
			return child
		}
		for _, alias := range child.Aliases {
			if alias == name {
				return child
			}
		}
	}
	return nil
}

func commandHasNameOrAlias(cmd *cobra.Command, name string) bool {
	return commandByNameOrAlias(cmd, name) != nil
}

func TestRemoteEnableLocalModeEmitsLocalStatus(t *testing.T) {
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
	configPath := filepath.Join(t.TempDir(), "termx.yaml")
	cmd.SetArgs([]string{"--config", configPath, "remote", "enable", "--mode", "local", "--addr", "127.0.0.1:18888", "--ice-tcp-addr", "127.0.0.1:18889"})
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
	oldRemoteStatus := remoteStatusClient
	oldPair := pairStartClient
	t.Cleanup(func() {
		remoteLocalStatusClient = oldStatus
		remoteStatusClient = oldRemoteStatus
		pairStartClient = oldPair
	})

	remoteLocalStatusClient = func(ctx context.Context, socketPath string, logFile string) (*remoteprotocol.LocalStatus, error) {
		return &remoteprotocol.LocalStatus{
			Enabled:      true,
			LocalPairURL: "http://127.0.0.1:19999/api/local/pair",
		}, nil
	}
	remoteStatusClient = func(ctx context.Context, socketPath string, logFile string) (*remoteprotocol.Status, error) {
		return &remoteprotocol.Status{
			HubURL:    "http://114.66.58.243:8447",
			UpdatedAt: time.Date(2026, 5, 1, 0, 5, 0, 0, time.UTC),
		}, nil
	}
	var gotParams remoteprotocol.PairStartParams
	pairStartClient = func(ctx context.Context, socketPath string, logFile string, params remoteprotocol.PairStartParams) (*remoteprotocol.PairStartResult, error) {
		gotParams = params
		return &remoteprotocol.PairStartResult{
			Type:              "termx_pair",
			MachineID:         "mach_test",
			MachineName:       "MacBook Pro",
			LocalPairURL:      params.LocalPairURL,
			PairSessionID:     "pair_test",
			PairSecret:        "secret",
			AnswerProofSecret: "proof-secret",
			ExpiresAt:         time.Date(2026, 5, 1, 0, 5, 0, 0, time.UTC),
		}, nil
	}

	var out bytes.Buffer
	cmd := newRootCmd()
	cmd.SetArgs([]string{"remote", "pair", "--uri", "--ttl", "2m", "--auth-ttl", "168h"})
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if gotParams.LocalPairURL != "http://127.0.0.1:19999/api/local/pair" || gotParams.TTLSeconds != 120 || gotParams.AuthTTLSeconds != int((7*24*time.Hour).Seconds()) {
		t.Fatalf("unexpected pair params: %#v", gotParams)
	}
	if !strings.HasPrefix(strings.TrimSpace(out.String()), "termx://pair?payload=") ||
		strings.Contains(out.String(), "pair_secret:\tsecret") ||
		strings.Contains(out.String(), "answer_proof_secret:\tproof-secret") {
		t.Fatalf("unexpected remote pair output:\n%s", out.String())
	}
}

func TestRemotePairEmitsMinimalTermxPairURI(t *testing.T) {
	oldStatus := remoteLocalStatusClient
	oldRemoteStatus := remoteStatusClient
	oldPair := pairStartClient
	t.Cleanup(func() {
		remoteLocalStatusClient = oldStatus
		remoteStatusClient = oldRemoteStatus
		pairStartClient = oldPair
	})

	remoteLocalStatusClient = func(ctx context.Context, socketPath string, logFile string) (*remoteprotocol.LocalStatus, error) {
		return &remoteprotocol.LocalStatus{
			Enabled:      true,
			LocalPairURL: "http://127.0.0.1:18888/api/v1/pairing/claims",
		}, nil
	}
	remoteStatusClient = func(ctx context.Context, socketPath string, logFile string) (*remoteprotocol.Status, error) {
		t.Fatalf("remote pair must not read Hub/Web Control status for QR payload")
		return nil, nil
	}
	var gotParams remoteprotocol.PairStartParams
	pairStartClient = func(ctx context.Context, socketPath string, logFile string, params remoteprotocol.PairStartParams) (*remoteprotocol.PairStartResult, error) {
		gotParams = params
		return &remoteprotocol.PairStartResult{
			Type:              "termx_pair",
			MachineID:         "mach_test",
			MachineName:       "MacBook Pro",
			LocalPairURL:      params.LocalPairURL,
			PairSessionID:     "pair_test",
			PairSecret:        "secret",
			AnswerProofSecret: "proof-secret",
			ExpiresAt:         time.Date(2026, 5, 1, 0, 5, 0, 0, time.UTC),
		}, nil
	}

	var out bytes.Buffer
	cmd := newRootCmd()
	cmd.SetArgs([]string{"remote", "pair", "--json", "--ttl", "2m"})
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if gotParams.LocalPairURL != "http://127.0.0.1:18888/api/v1/pairing/claims" || gotParams.TTLSeconds != 120 || gotParams.AuthTTLSeconds != 0 {
		t.Fatalf("unexpected pair params: %#v", gotParams)
	}
	var decoded struct {
		URI     string         `json:"uri"`
		Payload map[string]any `json:"payload"`
	}
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("pair output is not JSON: %v\n%s", err, out.String())
	}
	if !strings.HasPrefix(decoded.URI, "termx://pair?payload=") {
		t.Fatalf("unexpected URI: %s", decoded.URI)
	}
	if decoded.Payload["schema_version"].(float64) != 4 {
		t.Fatalf("expected schema version 4 payload, got %#v", decoded.Payload)
	}
	if _, ok := decoded.Payload["preferred_path"]; ok {
		t.Fatalf("QR payload must not contain preferred_path: %#v", decoded.Payload)
	}
	local, ok := decoded.Payload["local"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected local block: %#v", decoded.Payload["local"])
	}
	localHubURLs, ok := local["hub_urls"].([]any)
	if !ok || len(localHubURLs) != 1 || localHubURLs[0] != "http://127.0.0.1:18888" {
		t.Fatalf("unexpected local hub urls: %#v", local["hub_urls"])
	}
	if _, ok := decoded.Payload["hub"]; ok {
		t.Fatalf("QR payload must not contain Hub URLs: %#v", decoded.Payload["hub"])
	}
	pairing, ok := decoded.Payload["pairing"].(map[string]any)
	if !ok || pairing["session_id"] != "pair_test" || pairing["secret"] != "secret" || pairing["answer_proof_secret"] != "proof-secret" {
		t.Fatalf("unexpected pairing: %#v", decoded.Payload["pairing"])
	}
}

func TestRemotePairDoesNotStartLocalWebWhenLocalPairURLMissing(t *testing.T) {
	oldStatus := remoteLocalStatusClient
	oldRemoteStatus := remoteStatusClient
	oldPair := pairStartClient
	t.Cleanup(func() {
		remoteLocalStatusClient = oldStatus
		remoteStatusClient = oldRemoteStatus
		pairStartClient = oldPair
	})

	remoteLocalStatusClient = func(ctx context.Context, socketPath string, logFile string) (*remoteprotocol.LocalStatus, error) {
		return &remoteprotocol.LocalStatus{}, nil
	}
	var gotParams remoteprotocol.PairStartParams
	remoteStatusClient = func(ctx context.Context, socketPath string, logFile string) (*remoteprotocol.Status, error) {
		t.Fatalf("remote pair must not read Hub/Web Control status for QR payload")
		return nil, nil
	}
	pairStartClient = func(ctx context.Context, socketPath string, logFile string, params remoteprotocol.PairStartParams) (*remoteprotocol.PairStartResult, error) {
		gotParams = params
		return &remoteprotocol.PairStartResult{
			Type:              "termx_pair",
			MachineID:         "mach_test",
			MachineName:       "MacBook Pro",
			LocalPairURL:      params.LocalPairURL,
			PairSessionID:     "pair_test",
			PairSecret:        "secret",
			AnswerProofSecret: "proof-secret",
			ExpiresAt:         time.Date(2026, 5, 1, 0, 5, 0, 0, time.UTC),
		}, nil
	}

	cmd := newRootCmd()
	cmd.SetArgs([]string{"remote", "pair", "--uri"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if gotParams.LocalPairURL != "" {
		t.Fatalf("expected empty local pair URL when runtime has no local endpoint, got %#v", gotParams)
	}
}

func TestRemotePairRejectsInvalidAuthTTL(t *testing.T) {
	cmd := newRootCmd()
	cmd.SetArgs([]string{"remote", "pair", "--uri", "--auth-ttl", "0s"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "--auth-ttl must be greater than zero") {
		t.Fatalf("expected invalid auth ttl error, got %v", err)
	}
}

func TestRemotePairOmitsLocalBlockWhenLocalPairURLMissing(t *testing.T) {
	oldStatus := remoteLocalStatusClient
	oldRemoteStatus := remoteStatusClient
	oldPair := pairStartClient
	t.Cleanup(func() {
		remoteLocalStatusClient = oldStatus
		remoteStatusClient = oldRemoteStatus
		pairStartClient = oldPair
	})

	remoteLocalStatusClient = func(ctx context.Context, socketPath string, logFile string) (*remoteprotocol.LocalStatus, error) {
		return &remoteprotocol.LocalStatus{}, nil
	}
	remoteStatusClient = func(ctx context.Context, socketPath string, logFile string) (*remoteprotocol.Status, error) {
		t.Fatalf("remote pair must not read Hub/Web Control status for QR payload")
		return nil, nil
	}
	pairStartClient = func(ctx context.Context, socketPath string, logFile string, params remoteprotocol.PairStartParams) (*remoteprotocol.PairStartResult, error) {
		return &remoteprotocol.PairStartResult{
			MachineID:         "mach_test",
			PairSessionID:     "pair_test",
			PairSecret:        "secret",
			AnswerProofSecret: "proof-secret",
			ExpiresAt:         time.Date(2026, 5, 1, 0, 5, 0, 0, time.UTC),
		}, nil
	}

	var out bytes.Buffer
	cmd := newRootCmd()
	cmd.SetArgs([]string{"remote", "pair", "--json"})
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	var decoded struct {
		Payload map[string]any `json:"payload"`
	}
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("pair output is not JSON: %v\n%s", err, out.String())
	}
	if _, ok := decoded.Payload["local"]; ok {
		t.Fatalf("hub-only QR payload must not contain local block: %#v", decoded.Payload["local"])
	}
	if _, ok := decoded.Payload["hub"]; ok {
		t.Fatalf("QR payload must not contain Hub URLs: %#v", decoded.Payload["hub"])
	}
	if _, ok := decoded.Payload["preferred_path"]; ok {
		t.Fatalf("QR payload must not contain preferred_path: %#v", decoded.Payload["preferred_path"])
	}
}

func TestRemotePairDoesNotExposeRegisteredHubURLs(t *testing.T) {
	oldStatus := remoteLocalStatusClient
	oldRemoteStatus := remoteStatusClient
	oldPair := pairStartClient
	t.Cleanup(func() {
		remoteLocalStatusClient = oldStatus
		remoteStatusClient = oldRemoteStatus
		pairStartClient = oldPair
	})

	remoteLocalStatusClient = func(ctx context.Context, socketPath string, logFile string) (*remoteprotocol.LocalStatus, error) {
		return &remoteprotocol.LocalStatus{
			Enabled:      true,
			LocalPairURL: "http://127.0.0.1:18888/api/v1/pairing/claims",
		}, nil
	}
	remoteStatusClient = func(ctx context.Context, socketPath string, logFile string) (*remoteprotocol.Status, error) {
		t.Fatalf("remote pair must not read Hub/Web Control status for QR payload")
		return nil, nil
	}
	pairStartClient = func(ctx context.Context, socketPath string, logFile string, params remoteprotocol.PairStartParams) (*remoteprotocol.PairStartResult, error) {
		return &remoteprotocol.PairStartResult{
			MachineID:         "mach_test",
			LocalPairURL:      params.LocalPairURL,
			PairSessionID:     "pair_test",
			PairSecret:        "secret",
			AnswerProofSecret: "proof-secret",
			ExpiresAt:         time.Date(2026, 5, 1, 0, 5, 0, 0, time.UTC),
		}, nil
	}

	var out bytes.Buffer
	cmd := newRootCmd()
	cmd.SetArgs([]string{"remote", "pair", "--json"})
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	var decoded struct {
		Payload map[string]any `json:"payload"`
	}
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("pair output is not JSON: %v\n%s", err, out.String())
	}
	local := decoded.Payload["local"].(map[string]any)
	localHubURLs := local["hub_urls"].([]any)
	if len(localHubURLs) != 1 || localHubURLs[0] != "http://127.0.0.1:18888" {
		t.Fatalf("unexpected local hub URLs: %#v", localHubURLs)
	}
	if _, ok := decoded.Payload["hub"]; ok {
		t.Fatalf("QR payload must not contain Hub URLs: %#v", decoded.Payload["hub"])
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
			Mode:       "both",
			AllowLAN:   true,
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
		!strings.Contains(out.String(), "local_web_url:\thttp://127.0.0.1:18888") ||
		!strings.Contains(out.String(), "mode:\tboth") ||
		!strings.Contains(out.String(), "allow_lan:\ttrue") {
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

func TestRemoteEnableHubPersistsBootstrapOutsideConfigFile(t *testing.T) {
	oldEnable := remoteLocalEnableClient
	oldLogin := remoteLoginHTTPClient
	oldStore := remoteAuthStorePath
	t.Cleanup(func() {
		remoteLocalEnableClient = oldEnable
		remoteLoginHTTPClient = oldLogin
		remoteAuthStorePath = oldStore
	})
	remoteLocalEnableClient = func(ctx context.Context, socketPath string, logFile string, params remoteprotocol.LocalEnableParams) (*remoteprotocol.LocalStatus, error) {
		t.Fatal("remote local enable client must not be called for Hub enable")
		return nil, nil
	}
	remoteAuthStorePath = func(configPath string) (string, error) {
		return filepath.Join(t.TempDir(), "remote-auth.json"), nil
	}
	var validatedToken string
	remoteLoginHTTPClient = remoteLoginHTTPClientFunc{
		meFunc: func(ctx context.Context, controlURL string, token string) (remoteLoginUser, error) {
			validatedToken = token
			return remoteLoginUser{Email: "hub@example.com"}, nil
		},
	}

	configPath := filepath.Join(t.TempDir(), "termx.yaml")
	t.Setenv("TERMX_REMOTE_CONTROL_URL", "https://control.example.test")
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetArgs([]string{"--config", configPath, "remote", "enable", "--mode", "hub", "--token", "hub-secret", "--json"})
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Hub enable returned error: %v", err)
	}
	if validatedToken != "hub-secret" {
		t.Fatalf("expected token validation, got %q", validatedToken)
	}
	if data, err := os.ReadFile(configPath); err == nil && strings.Contains(string(data), "hub-secret") {
		t.Fatal("raw connection key was written to termx config")
	}
	var decoded struct {
		Enabled    bool   `json:"enabled"`
		ControlURL string `json:"control_url"`
		Mode       string `json:"mode"`
		AuthStore  string `json:"auth_store"`
	}
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("Hub enable output is not JSON: %v\n%s", err, out.String())
	}
	if !decoded.Enabled || decoded.ControlURL != "https://control.example.test" || decoded.Mode != "hub" || decoded.AuthStore == "" {
		t.Fatalf("unexpected Hub enable JSON: %#v", decoded)
	}
	cfg, err := remoteConfigFromFileAndEnv(configPath)
	if err != nil {
		t.Fatalf("load Hub config: %v", err)
	}
	if !cfg.Enabled || cfg.ControlURL != "https://control.example.test" || cfg.AccessToken != "hub-secret" {
		t.Fatalf("unexpected Hub remote config: %#v", cfg)
	}
}

func TestRemoteEnableBothPassesHubHubToRunningLocalDaemon(t *testing.T) {
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
			return remoteLoginUser{Email: "hub@example.com"}, nil
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
	t.Setenv("TERMX_REMOTE_CONTROL_URL", "https://control.example.test")
	cmd := newRootCmd()
	cmd.SetArgs([]string{
		"--config", configPath,
		"remote", "enable",
		"--mode", "both",
		"--hub-url", "https://hub.example.test",
		"--token", "hub-secret",
	})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("both enable returned error: %v", err)
	}
	if gotParams.HubURLs == nil || len(gotParams.HubURLs) != 1 || gotParams.HubURLs[0] != "https://hub.example.test" {
		t.Fatalf("running local daemon did not receive Hub hub URL: %#v", gotParams)
	}
}

func TestRemoteEnableBothPassesHubDiscoveryToRunningLocalDaemon(t *testing.T) {
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
			return remoteLoginUser{Email: "hub@example.com"}, nil
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
	t.Setenv("TERMX_REMOTE_CONTROL_URL", "https://control.example.test")
	cmd := newRootCmd()
	cmd.SetArgs([]string{
		"--config", configPath,
		"remote", "enable",
		"--mode", "both",
		"--token", "hub-secret",
	})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("both enable returned error: %v", err)
	}
	if gotParams.ControlURL != "https://control.example.test" || gotParams.AccessToken != "hub-secret" {
		t.Fatalf("running local daemon did not receive Hub discovery config: %#v", gotParams)
	}
	if len(gotParams.HubURLs) != 0 {
		t.Fatalf("expected discovery path without explicit hub URLs, got %#v", gotParams.HubURLs)
	}
}

func TestRemoteEnableOnlineUsesBrowserLoginWhenTokenMissing(t *testing.T) {
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
		t.Fatal("remote local enable client must not be called for Hub enable")
		return nil, nil
	}
	authStore := filepath.Join(t.TempDir(), "remote-auth.json")
	remoteAuthStorePath = func(configPath string) (string, error) {
		return authStore, nil
	}
	openBrowser = func(rawURL string) error {
		t.Fatalf("--no-browser should prevent browser launch, got %s", rawURL)
		return nil
	}
	var createdForControl string
	var validatedToken string
	remoteLoginHTTPClient = remoteLoginHTTPClientFunc{
		createBrowserLoginFunc: func(ctx context.Context, controlURL string, clientName string) (remoteBrowserLoginResult, error) {
			createdForControl = controlURL
			return remoteBrowserLoginResult{
				BrowserLoginCode:        "enable-browser",
				VerificationURIComplete: "https://control.example.test/device?user_code=ENABLE",
				ExpiresAt:               time.Now().Add(time.Minute),
				Interval:                time.Nanosecond,
			}, nil
		},
		pollBrowserLoginFunc: func(ctx context.Context, controlURL string, browserLoginCode string) (remoteLoginAuthResult, bool, error) {
			return remoteLoginAuthResult{
				AccessToken:  "browser-secret",
				RefreshToken: "browser-refresh",
				User:         remoteLoginUser{Email: "browser@example.com"},
			}, true, nil
		},
		meFunc: func(ctx context.Context, controlURL string, token string) (remoteLoginUser, error) {
			validatedToken = token
			return remoteLoginUser{Email: "browser@example.com"}, nil
		},
	}

	configPath := filepath.Join(t.TempDir(), "termx.yaml")
	t.Setenv("TERMX_REMOTE_CONTROL_URL", "https://control.example.test")
	cmd := newRootCmd()
	cmd.SetArgs([]string{"--config", configPath, "remote", "enable", "--mode", "hub", "--no-browser", "--timeout", "100ms", "--json"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("hub enable returned error: %v", err)
	}
	if createdForControl != "https://control.example.test" {
		t.Fatalf("expected browser login to use control URL, got %q", createdForControl)
	}
	if validatedToken != "browser-secret" {
		t.Fatalf("expected browser token validation, got %q", validatedToken)
	}
	cfg, err := remoteConfigFromFileAndEnv(configPath)
	if err != nil {
		t.Fatalf("load browser enable config: %v", err)
	}
	if !cfg.Enabled || cfg.ControlURL != "https://control.example.test" || cfg.AccessToken != "browser-secret" {
		t.Fatalf("unexpected hub config after browser enable: %#v", cfg)
	}
}

func TestRemoteEnableBrowserForcesFreshTokenOverSavedAuth(t *testing.T) {
	oldLogin := remoteLoginHTTPClient
	oldStore := remoteAuthStorePath
	t.Cleanup(func() {
		remoteLoginHTTPClient = oldLogin
		remoteAuthStorePath = oldStore
	})
	authStore := filepath.Join(t.TempDir(), "remote-auth.json")
	remoteAuthStorePath = func(configPath string) (string, error) {
		return authStore, nil
	}
	if err := saveRemoteAuthRecord(authStore, remoteAuthRecord{
		ControlURL:  "https://control.example.test",
		AccessToken: "old-secret",
	}); err != nil {
		t.Fatalf("save old auth record: %v", err)
	}
	configPath := filepath.Join(t.TempDir(), "termx.yaml")
	if err := ensureRemoteConfigBootstrap(configPath, "https://control.example.test", "", authStore, "hub", "", ""); err != nil {
		t.Fatalf("write config: %v", err)
	}
	var validatedToken string
	remoteLoginHTTPClient = remoteLoginHTTPClientFunc{
		createBrowserLoginFunc: func(ctx context.Context, controlURL string, clientName string) (remoteBrowserLoginResult, error) {
			return remoteBrowserLoginResult{
				BrowserLoginCode:        "enable-browser-refresh",
				VerificationURIComplete: "https://control.example.test/device?user_code=REFRESH",
				ExpiresAt:               time.Now().Add(time.Minute),
				Interval:                time.Nanosecond,
			}, nil
		},
		pollBrowserLoginFunc: func(ctx context.Context, controlURL string, browserLoginCode string) (remoteLoginAuthResult, bool, error) {
			return remoteLoginAuthResult{
				AccessToken:  "fresh-secret",
				RefreshToken: "fresh-refresh",
				User:         remoteLoginUser{Email: "fresh@example.com"},
			}, true, nil
		},
		meFunc: func(ctx context.Context, controlURL string, token string) (remoteLoginUser, error) {
			validatedToken = token
			return remoteLoginUser{Email: "fresh@example.com"}, nil
		},
	}

	cmd := newRootCmd()
	cmd.SetArgs([]string{"--config", configPath, "remote", "enable", "--mode", "hub", "--browser", "--no-browser", "--timeout", "100ms"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("browser refresh enable returned error: %v", err)
	}
	if validatedToken != "fresh-secret" {
		t.Fatalf("expected fresh token validation, got %q", validatedToken)
	}
	cfg, err := remoteConfigFromFileAndEnv(configPath)
	if err != nil {
		t.Fatalf("load refreshed config: %v", err)
	}
	if cfg.AccessToken != "fresh-secret" {
		t.Fatalf("expected fresh token in auth store, got %#v", cfg)
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

func (s *fakeTermxServer) Create(context.Context, termx.CreateOptions) (*termx.TerminalInfo, error) {
	return &termx.TerminalInfo{ID: "fake-terminal"}, nil
}

func (s *fakeTermxServer) Get(context.Context, string) (*termx.TerminalInfo, error) {
	return &termx.TerminalInfo{ID: "fake-terminal"}, nil
}

func (s *fakeTermxServer) List(context.Context, ...termx.ListOptions) ([]*termx.TerminalInfo, error) {
	return nil, nil
}

func (s *fakeTermxServer) SetMetadata(context.Context, string, string, map[string]string) error {
	return nil
}

func (s *fakeTermxServer) Restart(context.Context, string) error {
	return nil
}

func (s *fakeTermxServer) Remove(context.Context, string) error {
	return nil
}

func (s *fakeTermxServer) Events(context.Context, ...termx.EventsOption) <-chan termx.Event {
	ch := make(chan termx.Event)
	close(ch)
	return ch
}

type fakeCoreV2Server struct {
	newServerCalls int
	listenCalls    int
	shutdownCalls  int
}

func (s *fakeCoreV2Server) ListenAndServe(context.Context) error {
	s.listenCalls++
	return nil
}

func (s *fakeCoreV2Server) Shutdown(context.Context) error {
	s.shutdownCalls++
	return nil
}

func newCoreV2ProtocolClientForCLITest(t *testing.T) (*corev2.Server, *protocol.Client, func()) {
	return newCoreV2ProtocolClientForCLITestWithOptions(t)
}

func newCoreV2ProtocolClientForCLITestWithOptions(t *testing.T, opts ...corev2.ServerOption) (*corev2.Server, *protocol.Client, func()) {
	t.Helper()
	socketPath := filepath.Join(t.TempDir(), "termx-v2.sock")
	serverOpts := append([]corev2.ServerOption{corev2.WithSocketPath(socketPath)}, opts...)
	server := corev2.NewServer(serverOpts...)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- server.ListenAndServe(ctx)
	}()
	if err := waitForSocket(socketPath, 2*time.Second, func() error {
		client, err := dialV3Client(socketPath)
		if err != nil {
			return err
		}
		return client.Close()
	}); err != nil {
		cancel()
		_ = server.Shutdown(context.Background())
		t.Fatalf("core-v2 daemon did not become ready: %v", err)
	}
	client, err := dialV3Client(socketPath)
	if err != nil {
		cancel()
		_ = server.Shutdown(context.Background())
		t.Fatalf("dial core-v2 daemon: %v", err)
	}
	closeFn := func() {
		_ = client.Close()
		cancel()
		_ = server.Shutdown(context.Background())
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("core-v2 server did not stop in time")
		}
	}
	return server, client, closeFn
}

type coreV2ResizeRecordingProcessFactory struct {
	mu        sync.Mutex
	processes map[string]*coreV2ResizeRecordingProcess
}

func newCoreV2ResizeRecordingProcessFactory() *coreV2ResizeRecordingProcessFactory {
	return &coreV2ResizeRecordingProcessFactory{processes: make(map[string]*coreV2ResizeRecordingProcess)}
}

func (factory *coreV2ResizeRecordingProcessFactory) Spawn(_ context.Context, spec corev2.ProcessSpec) (corev2.TerminalProcess, error) {
	process := &coreV2ResizeRecordingProcess{
		outputCh: make(chan []byte, 16),
		waitCh:   make(chan corev2.ProcessExit, 1),
	}
	factory.mu.Lock()
	factory.processes[spec.TerminalID] = process
	factory.mu.Unlock()
	return process, nil
}

func (factory *coreV2ResizeRecordingProcessFactory) process(id string) *coreV2ResizeRecordingProcess {
	factory.mu.Lock()
	defer factory.mu.Unlock()
	return factory.processes[id]
}

type coreV2ResizeRecordingProcess struct {
	mu        sync.Mutex
	inputs    [][]byte
	resizes   []corev2.Size
	resizeErr error
	outputCh  chan []byte
	waitCh    chan corev2.ProcessExit
	closed    bool
}

func (process *coreV2ResizeRecordingProcess) Input(data []byte) error {
	process.mu.Lock()
	defer process.mu.Unlock()
	if process.closed {
		return io.ErrClosedPipe
	}
	process.inputs = append(process.inputs, append([]byte(nil), data...))
	return nil
}

func (process *coreV2ResizeRecordingProcess) Resize(size corev2.Size) error {
	process.mu.Lock()
	defer process.mu.Unlock()
	if process.closed {
		return io.ErrClosedPipe
	}
	process.resizes = append(process.resizes, size)
	if process.resizeErr != nil {
		return process.resizeErr
	}
	return nil
}

func (process *coreV2ResizeRecordingProcess) Output() <-chan []byte {
	return process.outputCh
}

func (process *coreV2ResizeRecordingProcess) Kill() error {
	process.mu.Lock()
	process.closed = true
	process.mu.Unlock()
	select {
	case process.waitCh <- corev2.ProcessExit{Code: -1}:
	default:
	}
	return nil
}

func (process *coreV2ResizeRecordingProcess) Wait() <-chan corev2.ProcessExit {
	return process.waitCh
}

func (process *coreV2ResizeRecordingProcess) Close() error {
	process.mu.Lock()
	process.closed = true
	process.mu.Unlock()
	return nil
}

func (process *coreV2ResizeRecordingProcess) setResizeErr(err error) {
	process.mu.Lock()
	defer process.mu.Unlock()
	process.resizeErr = err
}

func (process *coreV2ResizeRecordingProcess) snapshot() ([][]byte, []corev2.Size) {
	process.mu.Lock()
	defer process.mu.Unlock()
	inputs := make([][]byte, len(process.inputs))
	for i, input := range process.inputs {
		inputs[i] = append([]byte(nil), input...)
	}
	resizes := append([]corev2.Size(nil), process.resizes...)
	return inputs, resizes
}

func drainV3RuntimeForCLITest(t *testing.T, runtime *app.AppRuntime) {
	t.Helper()
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain runtime: %v", err)
	}
}

func waitForCoreV2ResizeRecordingProcess(t *testing.T, factory *coreV2ResizeRecordingProcessFactory, terminalID string) *coreV2ResizeRecordingProcess {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if process := factory.process(terminalID); process != nil {
			return process
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for core-v2 recording process %s", terminalID)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitForCoreV2ProcessResize(t *testing.T, process *coreV2ResizeRecordingProcess, want corev2.Size) int {
	t.Helper()
	return waitForCoreV2ProcessResizeAfter(t, process, want, 0)
}

func waitForCoreV2ProcessResizeAfter(t *testing.T, process *coreV2ResizeRecordingProcess, want corev2.Size, after int) int {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		_, resizes := process.snapshot()
		if after < 0 {
			after = 0
		}
		for index := after; index < len(resizes); index++ {
			if resizes[index] == want {
				return index + 1
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for process resize %#v after %d, got %#v", want, after, resizes)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func mouseEventAtCLITest(rect render.Rect) tuiinput.InputEvent {
	return tuiinput.InputEvent{Kind: tuiinput.EventKindMouse, Mouse: tuiinput.MouseLeft, Row: rect.Y + 1, Col: rect.X + 1}
}

func lastFramePaneResizeRegionForCLITest(t *testing.T, host *app.FakeTerminalHost, paneID string, direction tuistate.PaneResizeDirection) render.HitRegion {
	t.Helper()
	frames := host.Frames()
	if len(frames) == 0 {
		t.Fatal("expected runtime frame")
	}
	for _, region := range frames[len(frames)-1].HitRegions {
		if region.Kind == render.HitRegionPaneResize && region.ActionID == "pane.resize" && region.PaneID == paneID && region.Direction == string(direction) {
			return region
		}
	}
	t.Fatalf("missing pane resize region pane=%s direction=%s in %#v", paneID, direction, frames[len(frames)-1].HitRegions)
	return render.HitRegion{}
}

func testShellSleepCommand() []string {
	return []string{"/bin/sh", "-c", "while true; do sleep 1; done"}
}

func (s *fakeTermxServer) StorageGet(context.Context, termx.StorageGetRequest) (termx.StorageEntry, error) {
	return termx.StorageEntry{}, termx.ErrNotFound
}

func (s *fakeTermxServer) StoragePut(_ context.Context, req termx.StoragePutRequest) (termx.StorageEntry, error) {
	return termx.StorageEntry{
		AppID:   req.AppID,
		Scope:   req.Scope,
		OwnerID: req.OwnerID,
		Key:     req.Key,
		Value:   append([]byte(nil), req.Value...),
		Version: 1,
	}, nil
}

func (s *fakeTermxServer) StorageDelete(_ context.Context, req termx.StorageDeleteRequest) (termx.StorageDeleteResult, error) {
	return termx.StorageDeleteResult{
		AppID:   req.AppID,
		Scope:   req.Scope,
		OwnerID: req.OwnerID,
		Key:     req.Key,
		Deleted: true,
		Version: 1,
	}, nil
}

func (s *fakeTermxServer) StorageList(context.Context, termx.StorageListRequest) ([]termx.StorageEntry, error) {
	return nil, nil
}

func (s *fakeTermxServer) ServeTransport(context.Context, transport.Transport, string) error {
	return nil
}

func (s *fakeTermxServer) ServeScopedTransport(context.Context, transport.Transport, string, termx.TransportScope) error {
	return nil
}
