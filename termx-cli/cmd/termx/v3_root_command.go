package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/spf13/cobra"
)

const v3RootTerminalID = "termx-main"

type v3RootRunner func(context.Context, v3RootConfig) error

type v3RootConfig struct {
	SocketPath string
	LogFile    string
}

var runV3Root = runV3RootRuntime

func runV3RootCommand(cmd *cobra.Command, socket string, logFile string) error {
	if !isInteractiveTerminal() {
		return fmt.Errorf("termx TUI requires an interactive terminal; use `termx --help` or subcommands like `new`, `ls`, `attach`, `kill`, `rm`, `daemon`")
	}
	if err := rejectNestedTUI(); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return runV3Root(ctx, v3RootConfig{
		SocketPath: resolveV3Socket(socket),
		LogFile:    resolveV3LogFilePath(logFile),
	})
}

func runV3RootRuntime(ctx context.Context, cfg v3RootConfig) error {
	logger, closeLogger, logPath, err := openLogFileLogger(cfg.LogFile)
	if err != nil {
		return err
	}
	defer closeLogger()

	client, err := dialOrStartV3Client(cfg.SocketPath, logPath, logger)
	if err != nil {
		return err
	}
	terminalID, err := ensureV3RootTerminal(ctx, client)
	closeErr := client.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	logger.Info("starting tui-v3 root command", "terminal_id", terminalID, "socket", cfg.SocketPath, "log_file", logPath)
	return runV3Attach(ctx, v3AttachConfig{
		TerminalID: terminalID,
		SocketPath: cfg.SocketPath,
		LogFile:    logPath,
	})
}

func ensureV3RootTerminal(ctx context.Context, client *protocol.Client) (string, error) {
	list, err := client.List(ctx)
	if err != nil {
		return "", fmt.Errorf("list core-v2 terminals for root: %w", err)
	}
	if id := selectV3RootTerminal(list.Terminals); id != "" {
		return id, nil
	}
	created, err := client.Create(ctx, protocol.CreateParams{
		ID:      v3RootTerminalID,
		Name:    "main",
		Command: defaultV3RootCommand(),
		Size:    defaultV3RootSize(),
	})
	if err != nil {
		refreshed, listErr := client.List(ctx)
		if listErr == nil {
			if id := selectV3RootTerminal(refreshed.Terminals); id != "" {
				return id, nil
			}
		}
		return "", fmt.Errorf("create core-v2 root terminal: %w", err)
	}
	return created.TerminalID, nil
}

func selectV3RootTerminal(items []protocol.TerminalInfo) string {
	for _, item := range items {
		if item.State == "running" {
			return item.ID
		}
	}
	return ""
}

func defaultV3RootCommand() []string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	return []string{shell}
}

func defaultV3RootSize() protocol.Size {
	size := currentSize()
	if size.Cols == 0 || size.Rows == 0 {
		return protocol.Size{Cols: 80, Rows: 24}
	}
	return size
}
