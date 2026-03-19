package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tui"
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

type stubTUIClient struct{}

func (c *stubTUIClient) Close() error { return nil }

func (c *stubTUIClient) Create(ctx context.Context, command []string, name string, size protocol.Size) (*protocol.CreateResult, error) {
	return nil, nil
}

func (c *stubTUIClient) SetTags(ctx context.Context, terminalID string, tags map[string]string) error {
	return nil
}

func (c *stubTUIClient) List(ctx context.Context) (*protocol.ListResult, error) {
	return nil, nil
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
