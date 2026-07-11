package main

import (
	"context"
	"fmt"
	"os/signal"
	"syscall"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/shared/connection"
	"github.com/lozzow/termx/shared/perftrace"
	"github.com/lozzow/termx/tui/app"
	"github.com/lozzow/termx/tui/state"
	"github.com/spf13/cobra"
)

const v3RootTerminalID = "termx-main"

type v3RootRunner func(context.Context, v3RootConfig) error
type v3RootEmptyRunner func(context.Context, v3RootEmptyConfig) error

type v3RootConfig struct {
	SocketPath         string
	LogFile            string
	TUIConfig          state.TUIConfigStore
	ConnectionRegistry connection.Registry
}

type v3RootEmptyConfig struct {
	SocketPath         string
	LogFile            string
	TUIConfig          state.TUIConfigStore
	ConnectionRegistry connection.Registry
}

var runV3Root = runV3RootRuntime
var runV3RootEmpty = runV3RootEmptyRuntime

func runV3RootCommand(cmd *cobra.Command, socket string, logFile string) error {
	if !isInteractiveTerminal() {
		return fmt.Errorf("termx TUI requires an interactive terminal; use `termx --help` or subcommands like `new`, `ls`, `attach`, `kill`, `rm`, `daemon`")
	}
	if err := rejectNestedTUI(); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	tuiConfig, err := loadV3TUIConfig()
	if err != nil {
		return err
	}
	connectionRegistry, err := loadV3ConnectionRegistry()
	if err != nil {
		return err
	}
	return runV3Root(ctx, v3RootConfig{
		SocketPath:         resolveV3SocketForConnectionRegistry(socket, connectionRegistry),
		LogFile:            resolveV3LogFilePath(logFile),
		TUIConfig:          tuiConfig,
		ConnectionRegistry: connectionRegistry,
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
	terminalID, ok, err := selectV3RootAttachTerminal(ctx, client)
	closeErr := client.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if !ok {
		logger.Info("starting tui-v3 root empty command", "socket", cfg.SocketPath, "log_file", logPath)
		return runV3RootEmpty(ctx, v3RootEmptyConfig{
			SocketPath:         cfg.SocketPath,
			LogFile:            logPath,
			TUIConfig:          cfg.TUIConfig,
			ConnectionRegistry: cfg.ConnectionRegistry,
		})
	}
	logger.Info("starting tui-v3 root command", "terminal_id", terminalID, "socket", cfg.SocketPath, "log_file", logPath)
	return runV3Attach(ctx, v3AttachConfig{
		TerminalID:         terminalID,
		SocketPath:         cfg.SocketPath,
		LogFile:            logPath,
		TUIConfig:          cfg.TUIConfig,
		ConnectionRegistry: cfg.ConnectionRegistry,
	})
}

func runV3RootEmptyRuntime(ctx context.Context, cfg v3RootEmptyConfig) error {
	logger, closeLogger, logPath, err := openLogFileLogger(cfg.LogFile)
	if err != nil {
		return err
	}
	defer closeLogger()
	stopPerfTrace, perfTracePath, perfTraceEnabled := perftrace.EnableFromEnvWithProcess(ctx, "tui-v3")
	defer stopPerfTrace()
	if perfTraceEnabled {
		logger.Info("tui-v3 perftrace enabled", "path", perfTracePath)
	}
	client, err := dialOrStartV3Client(cfg.SocketPath, logPath, logger)
	if err != nil {
		return err
	}
	defer client.Close()
	workbenchStorageClient, err := v3DialClient(cfg.SocketPath)
	if err != nil {
		return fmt.Errorf("dial core-v2 workbench storage events client: %w", err)
	}
	defer workbenchStorageClient.Close()
	clipboardStorageClient, err := v3DialClient(cfg.SocketPath)
	if err != nil {
		return fmt.Errorf("dial core-v2 clipboard storage events client: %w", err)
	}
	defer clipboardStorageClient.Close()

	host := newV3TerminalHost()
	if loggerHost, ok := host.(v3TerminalHostLogger); ok {
		loggerHost.SetLogger(logger)
	}
	if err := host.Enter(ctx); err != nil {
		return err
	}
	defer host.Close()

	cols, rows, err := host.Size()
	if err != nil || cols <= 0 || rows <= 0 {
		cols, rows = 80, 24
	}
	runtime := newV3InteractiveRuntimeWithOptions("", cols, rows, client, workbenchStorageClient, clipboardStorageClient, host, logger, v3InteractiveRuntimeOptions{
		SkipWorkbenchInitialLoad: true,
		TUIConfig:                cfg.TUIConfig,
		ConnectionRegistry:       cfg.ConnectionRegistry,
		EndpointContext:          ctx,
	})
	// root 空启动不创建 terminal；先让用户在 picker 中显式选择创建或连接。
	if err := runtime.Post(app.ShellOpenTerminalPickerMsg{}); err != nil {
		return err
	}
	for {
		if err := runtime.Run(ctx); err != nil {
			return err
		}
		return ctx.Err()
	}
}

func selectV3RootAttachTerminal(ctx context.Context, client *protocol.Client) (string, bool, error) {
	list, err := client.List(ctx)
	if err != nil {
		return "", false, fmt.Errorf("list core-v2 terminals for root: %w", err)
	}
	if id := selectV3RootTerminal(list.Terminals); id != "" {
		return id, true, nil
	}
	// 固定 root terminal 退出后仍会留在 core 里；root 入口只选择连接对象。
	// restart 必须是用户显式动作，不能在重进 TUI 时自动 HUP 旧 PTY。
	if item, ok := findV3RootTerminal(list.Terminals); ok {
		return item.ID, true, nil
	}
	return "", false, nil
}

func selectV3RootTerminal(items []protocol.TerminalInfo) string {
	for _, item := range items {
		if item.State == "running" {
			return item.ID
		}
	}
	return ""
}

func findV3RootTerminal(items []protocol.TerminalInfo) (protocol.TerminalInfo, bool) {
	for _, item := range items {
		if item.ID == v3RootTerminalID {
			return item, true
		}
	}
	return protocol.TerminalInfo{}, false
}
