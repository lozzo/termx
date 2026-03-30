package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tui"
	"github.com/lozzow/termx/tuiv2/shared"
)

func TestRootCmdLayoutFlagPassesStartupLayoutToTUI(t *testing.T) {
	oldInteractive := isInteractiveTerminal
	oldDial := dialOrStartTUIClient
	oldRun := runTUI
	t.Cleanup(func() {
		isInteractiveTerminal = oldInteractive
		dialOrStartTUIClient = oldDial
		runTUI = oldRun
	})

	isInteractiveTerminal = func() bool { return true }
	dialOrStartTUIClient = func(path string, logFile string, logger *slog.Logger) (tui.Client, error) {
		return &stubTUIClient{}, nil
	}

	var got tui.Config
	runTUI = func(client tui.Client, cfg tui.Config, input io.Reader, output io.Writer) error {
		got = cfg
		return nil
	}

	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	cmd := newRootCmd()
	cmd.SetArgs([]string{
		"--layout", "demo",
		"--log-file", filepath.Join(t.TempDir(), "termx.log"),
	})
	cmd.SetIn(bytes.NewBuffer(nil))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if got.StartupLayout != "demo" {
		t.Fatalf("expected startup layout demo, got %q", got.StartupLayout)
	}
	if !got.StartupAutoLayout {
		t.Fatal("expected root command to enable startup auto layout")
	}
	if want := filepath.Join(stateHome, "termx", "workspace-state.json"); got.WorkspaceStatePath != want {
		t.Fatalf("expected workspace state path %q, got %q", want, got.WorkspaceStatePath)
	}
	if got.AttachID != "" {
		t.Fatalf("expected root command not to set attach id, got %q", got.AttachID)
	}
}

func TestRootCmdBlocksNestedTUIByDefault(t *testing.T) {
	oldInteractive := isInteractiveTerminal
	oldDial := dialOrStartTUIClient
	oldRun := runTUI
	t.Cleanup(func() {
		isInteractiveTerminal = oldInteractive
		dialOrStartTUIClient = oldDial
		runTUI = oldRun
	})

	isInteractiveTerminal = func() bool { return true }
	dialOrStartTUIClient = func(path string, logFile string, logger *slog.Logger) (tui.Client, error) {
		t.Fatal("expected nested TUI block before dialing daemon")
		return nil, nil
	}
	runTUI = func(client tui.Client, cfg tui.Config, input io.Reader, output io.Writer) error {
		t.Fatal("expected nested TUI block before starting TUI")
		return nil
	}

	t.Setenv("TERMX", "1")
	t.Setenv("TERMX_ALLOW_NESTED", "")

	cmd := newRootCmd()
	cmd.SetArgs(nil)
	cmd.SetIn(bytes.NewBuffer(nil))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "refusing to start termx TUI inside a termx-managed terminal") {
		t.Fatalf("expected nested TUI rejection, got %v", err)
	}
}

func TestAttachCmdAllowsNestedTUIWhenOverrideIsSet(t *testing.T) {
	oldDial := dialOrStartTUIClient
	oldRun := runTUI
	t.Cleanup(func() {
		dialOrStartTUIClient = oldDial
		runTUI = oldRun
	})

	dialOrStartTUIClient = func(path string, logFile string, logger *slog.Logger) (tui.Client, error) {
		return &stubTUIClient{}, nil
	}

	var got tui.Config
	runTUI = func(client tui.Client, cfg tui.Config, input io.Reader, output io.Writer) error {
		got = cfg
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
	if got.AttachID != "term-001" {
		t.Fatalf("expected attach id to pass through, got %q", got.AttachID)
	}
}

func TestAttachCmdPassesPrefixTimeoutToTUI(t *testing.T) {
	oldDial := dialOrStartTUIClient
	oldRun := runTUI
	t.Cleanup(func() {
		dialOrStartTUIClient = oldDial
		runTUI = oldRun
	})

	dialOrStartTUIClient = func(path string, logFile string, logger *slog.Logger) (tui.Client, error) {
		return &stubTUIClient{}, nil
	}

	var got tui.Config
	runTUI = func(client tui.Client, cfg tui.Config, input io.Reader, output io.Writer) error {
		got = cfg
		return nil
	}

	cmd := newRootCmd()
	cmd.SetArgs([]string{"--prefix-timeout", "5s", "attach", "term-001"})
	cmd.SetIn(bytes.NewBuffer(nil))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected attach command to succeed, got %v", err)
	}
	if got.PrefixTimeout != 5*time.Second {
		t.Fatalf("expected prefix timeout 5s, got %s", got.PrefixTimeout)
	}
	if got.WorkspaceStatePath != "" {
		t.Fatalf("expected attach command to avoid workspace persistence path, got %q", got.WorkspaceStatePath)
	}
}

func TestRootCmdEnablesStartupPickerByDefault(t *testing.T) {
	oldInteractive := isInteractiveTerminal
	oldDial := dialOrStartTUIClient
	oldRun := runTUI
	t.Cleanup(func() {
		isInteractiveTerminal = oldInteractive
		dialOrStartTUIClient = oldDial
		runTUI = oldRun
	})

	isInteractiveTerminal = func() bool { return true }
	dialOrStartTUIClient = func(path string, logFile string, logger *slog.Logger) (tui.Client, error) {
		return &stubTUIClient{}, nil
	}

	var got tui.Config
	runTUI = func(client tui.Client, cfg tui.Config, input io.Reader, output io.Writer) error {
		got = cfg
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
	if !got.StartupPicker {
		t.Fatalf("expected root command to enable startup picker by default")
	}
}

func TestRootCmdPassesPrefixTimeoutFlag(t *testing.T) {
	oldInteractive := isInteractiveTerminal
	oldDial := dialOrStartTUIClient
	oldRun := runTUI
	t.Cleanup(func() {
		isInteractiveTerminal = oldInteractive
		dialOrStartTUIClient = oldDial
		runTUI = oldRun
	})

	isInteractiveTerminal = func() bool { return true }
	dialOrStartTUIClient = func(path string, logFile string, logger *slog.Logger) (tui.Client, error) {
		return &stubTUIClient{}, nil
	}

	var got tui.Config
	runTUI = func(client tui.Client, cfg tui.Config, input io.Reader, output io.Writer) error {
		got = cfg
		return nil
	}

	cmd := newRootCmd()
	cmd.SetArgs([]string{"--prefix-timeout", "5s", "--log-file", filepath.Join(t.TempDir(), "termx.log")})
	cmd.SetIn(bytes.NewBuffer(nil))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if got.PrefixTimeout != 5*time.Second {
		t.Fatalf("expected root command to pass prefix timeout, got %v", got.PrefixTimeout)
	}
}

func TestRootCmdTUIv2FlagRoutesToTUIv2(t *testing.T) {
	oldInteractive := isInteractiveTerminal
	oldRunv2 := runTUIv2
	oldDial := dialOrStartTUIClient
	oldRun := runTUI
	t.Cleanup(func() {
		isInteractiveTerminal = oldInteractive
		runTUIv2 = oldRunv2
		dialOrStartTUIClient = oldDial
		runTUI = oldRun
	})

	isInteractiveTerminal = func() bool { return true }

	v2Called := false
	var gotCfg shared.Config
	runTUIv2 = func(cfg shared.Config, stdin io.Reader, stdout io.Writer) error {
		v2Called = true
		gotCfg = cfg
		return nil
	}

	dialOrStartTUIClient = func(path string, logFile string, logger *slog.Logger) (tui.Client, error) {
		t.Fatal("v1 dial should not be called when --tui-v2 is set")
		return nil, nil
	}
	runTUI = func(client tui.Client, cfg tui.Config, input io.Reader, output io.Writer) error {
		t.Fatal("v1 runTUI should not be called when --tui-v2 is set")
		return nil
	}

	cmd := newRootCmd()
	cmd.SetArgs([]string{
		"--tui-v2",
		"--log-file", filepath.Join(t.TempDir(), "termx.log"),
	})
	cmd.SetIn(bytes.NewBuffer(nil))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !v2Called {
		t.Fatal("expected runTUIv2 to be called when --tui-v2 flag is set")
	}
	if gotCfg.Workspace != "main" {
		t.Fatalf("expected workspace=main, got %q", gotCfg.Workspace)
	}
}

func TestRootCmdWithoutTUIv2FlagRoutesToV1(t *testing.T) {
	oldInteractive := isInteractiveTerminal
	oldRunv2 := runTUIv2
	oldDial := dialOrStartTUIClient
	oldRun := runTUI
	t.Cleanup(func() {
		isInteractiveTerminal = oldInteractive
		runTUIv2 = oldRunv2
		dialOrStartTUIClient = oldDial
		runTUI = oldRun
	})

	isInteractiveTerminal = func() bool { return true }

	runTUIv2 = func(cfg shared.Config, stdin io.Reader, stdout io.Writer) error {
		t.Fatal("runTUIv2 should not be called without --tui-v2 flag")
		return nil
	}
	dialOrStartTUIClient = func(path string, logFile string, logger *slog.Logger) (tui.Client, error) {
		return &stubTUIClient{}, nil
	}

	v1Called := false
	runTUI = func(client tui.Client, cfg tui.Config, input io.Reader, output io.Writer) error {
		v1Called = true
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
	if !v1Called {
		t.Fatal("expected v1 runTUI to be called when --tui-v2 flag is absent")
	}
}

func TestRootCmdTUIv2BlocksNestedTUI(t *testing.T) {
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
	cmd.SetArgs([]string{
		"--tui-v2",
		"--log-file", filepath.Join(t.TempDir(), "termx.log"),
	})
	cmd.SetIn(bytes.NewBuffer(nil))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "refusing to start termx TUI inside a termx-managed terminal") {
		t.Fatalf("expected nested TUI rejection, got %v", err)
	}
}

type stubTUIClient struct{}

func (c *stubTUIClient) Close() error { return nil }

func (c *stubTUIClient) Create(ctx context.Context, command []string, name string, size protocol.Size) (*protocol.CreateResult, error) {
	return nil, nil
}

func (c *stubTUIClient) SetTags(ctx context.Context, terminalID string, tags map[string]string) error {
	return nil
}

func (c *stubTUIClient) SetMetadata(ctx context.Context, terminalID string, name string, tags map[string]string) error {
	return nil
}

func (c *stubTUIClient) List(ctx context.Context) (*protocol.ListResult, error) {
	return nil, nil
}

func (c *stubTUIClient) Events(ctx context.Context, params protocol.EventsParams) (<-chan protocol.Event, error) {
	ch := make(chan protocol.Event)
	close(ch)
	return ch, nil
}

func (c *stubTUIClient) Attach(ctx context.Context, terminalID string, mode string) (*protocol.AttachResult, error) {
	return nil, nil
}

func (c *stubTUIClient) Snapshot(ctx context.Context, terminalID string, offset, limit int) (*protocol.Snapshot, error) {
	return nil, nil
}

func (c *stubTUIClient) Input(ctx context.Context, channel uint16, data []byte) error {
	return nil
}

func (c *stubTUIClient) Resize(ctx context.Context, channel uint16, cols, rows uint16) error {
	return nil
}

func (c *stubTUIClient) Stream(channel uint16) (<-chan protocol.StreamFrame, func()) {
	ch := make(chan protocol.StreamFrame)
	close(ch)
	return ch, func() {}
}

func (c *stubTUIClient) Kill(ctx context.Context, terminalID string) error {
	return nil
}

func (c *stubTUIClient) Remove(ctx context.Context, terminalID string) error {
	return nil
}
