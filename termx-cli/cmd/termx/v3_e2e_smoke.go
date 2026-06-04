package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/lozzow/termx/internal/protocol"
	corev2 "github.com/lozzow/termx/termx-core-v2"
	"github.com/lozzow/termx/termx-tui-v3/app"
	"github.com/lozzow/termx/termx-tui-v3/input"
)

type v3E2ESmokeResult struct {
	TerminalID string
	Frames     int
}

func runV3E2ESmoke(ctx context.Context) (v3E2ESmokeResult, error) {
	socketDir, err := os.MkdirTemp("", "termx-v3-smoke-*")
	if err != nil {
		return v3E2ESmokeResult{}, err
	}
	defer os.RemoveAll(socketDir)
	socketPath := filepath.Join(socketDir, fmt.Sprintf("termx-v2-smoke-%d.sock", time.Now().UnixNano()))
	server := corev2.NewServer(
		corev2.WithSocketPath(socketPath),
		corev2.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
	)
	serverCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- server.ListenAndServe(serverCtx)
	}()
	defer func() {
		cancel()
		_ = server.Shutdown(context.Background())
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	}()
	if err := waitForSocket(socketPath, 2*time.Second, func() error {
		client, err := dialV3Client(socketPath)
		if err != nil {
			return err
		}
		return client.Close()
	}); err != nil {
		return v3E2ESmokeResult{}, err
	}
	client, err := dialV3Client(socketPath)
	if err != nil {
		return v3E2ESmokeResult{}, err
	}
	defer client.Close()

	created, err := client.Create(ctx, protocol.CreateParams{
		ID:      newV3TerminalID(),
		Name:    "v3-e2e-smoke",
		Command: []string{"smoke-shell"},
		Size:    protocol.Size{Cols: 80, Rows: 24},
	})
	if err != nil {
		return v3E2ESmokeResult{}, err
	}
	if err := server.IngestOutput(ctx, created.TerminalID, "alpha\nbeta\n"); err != nil {
		return v3E2ESmokeResult{}, err
	}

	host := app.NewFakeTerminalHost(16)
	runtime := newV3InteractiveRuntime(created.TerminalID, 80, 24, client, host)
	if err := runtime.Post(app.LiveAttachMsg{Config: app.LiveConfig{
		TerminalID:   created.TerminalID,
		Cols:         80,
		Rows:         24,
		Mode:         "collaborator",
		ResizePolicy: protocol.ResizePolicyOwner,
		SurfaceID:    "v3-e2e-smoke",
		ViewID:       "v3-e2e-smoke-main",
	}}); err != nil {
		return v3E2ESmokeResult{}, err
	}
	if err := runtime.Drain(ctx); err != nil {
		return v3E2ESmokeResult{}, err
	}
	if !runtime.State().Session.Attached {
		return v3E2ESmokeResult{}, fmt.Errorf("v3 e2e smoke: attach did not update tui-v3 state")
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "x"}); err != nil {
		return v3E2ESmokeResult{}, err
	}
	if err := runtime.Drain(ctx); err != nil {
		return v3E2ESmokeResult{}, err
	}
	if err := runtime.Post(app.LiveResizeMsg{Cols: 100, Rows: 30}); err != nil {
		return v3E2ESmokeResult{}, err
	}
	if err := runtime.Drain(ctx); err != nil {
		return v3E2ESmokeResult{}, err
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyPageUp}); err != nil {
		return v3E2ESmokeResult{}, err
	}
	if err := runtime.Drain(ctx); err != nil {
		return v3E2ESmokeResult{}, err
	}
	if !runtime.State().CopyMode.Active || len(runtime.State().History.Rows) == 0 {
		return v3E2ESmokeResult{}, fmt.Errorf("v3 e2e smoke: copy mode did not load authoritative history")
	}
	if err := client.Kill(ctx, created.TerminalID); err != nil {
		return v3E2ESmokeResult{}, err
	}
	if err := client.Remove(ctx, created.TerminalID); err != nil {
		return v3E2ESmokeResult{}, err
	}
	return v3E2ESmokeResult{TerminalID: created.TerminalID, Frames: len(host.Frames())}, nil
}
