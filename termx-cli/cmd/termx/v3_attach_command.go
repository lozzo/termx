package main

import (
	"context"
	"fmt"
	"log/slog"
	"os/signal"
	"syscall"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/termx-tui-v3/app"
	"github.com/lozzow/termx/termx-tui-v3/services"
	"github.com/lozzow/termx/termx-tui-v3/state"
	"github.com/lozzow/termx/termx-tui-v3/terminalhost"
	"github.com/spf13/cobra"
)

type v3TerminalHost interface {
	app.TerminalHost
	Enter(context.Context) error
	Close() error
	Size() (int, int, error)
}

type v3TerminalHostLogger interface {
	SetLogger(*slog.Logger)
}

type v3AttachRunner func(context.Context, v3AttachConfig) error

type v3AttachConfig struct {
	TerminalID string
	SocketPath string
	LogFile    string
	TUIConfig  state.TUIConfigStore
}

var (
	newV3TerminalHost = func() v3TerminalHost {
		return terminalhost.New()
	}
	runV3Attach = runV3AttachRuntime
)

func v3AttachCommand(socket *string, logFile *string) *cobra.Command {
	return &cobra.Command{
		Use:  "attach <id>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !isInteractiveTerminal() {
				return fmt.Errorf("termx v3 attach requires an interactive terminal; use `termx v3 ping` or `termx v3 smoke` for non-interactive checks")
			}
			if err := rejectNestedTUI(); err != nil {
				return err
			}
			logPath := resolveV3LogFilePath(*logFile)
			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			tuiConfig, err := loadV3TUIConfig()
			if err != nil {
				return err
			}
			return runV3Attach(ctx, v3AttachConfig{
				TerminalID: args[0],
				SocketPath: resolveV3Socket(*socket),
				LogFile:    logPath,
				TUIConfig:  tuiConfig,
			})
		},
	}
}

func runV3AttachRuntime(ctx context.Context, cfg v3AttachConfig) error {
	logger, closeLogger, logPath, err := openLogFileLogger(cfg.LogFile)
	if err != nil {
		return err
	}
	defer closeLogger()
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
	runtime := newV3InteractiveRuntimeWithOptions(cfg.TerminalID, cols, rows, client, workbenchStorageClient, clipboardStorageClient, host, logger, v3InteractiveRuntimeOptions{
		TUIConfig: cfg.TUIConfig,
	})
	if err := runtime.Post(app.LiveAttachMsg{Config: app.LiveConfig{
		TerminalID:   cfg.TerminalID,
		Cols:         cols,
		Rows:         rows,
		Mode:         "collaborator",
		ResizePolicy: protocol.ResizePolicyOwner,
		SurfaceID:    "termx-cli-v3",
		ViewID:       "termx-cli-v3-main",
	}}); err != nil {
		return err
	}
	for {
		if err := runtime.Run(ctx); err != nil {
			return err
		}
		return ctx.Err()
	}
}

func newV3InteractiveRuntime(terminalID string, cols int, rows int, client *protocol.Client, workbenchStorageClient *protocol.Client, clipboardStorageClient *protocol.Client, host app.TerminalHost, logger *slog.Logger) *app.AppRuntime {
	return newV3InteractiveRuntimeWithOptions(terminalID, cols, rows, client, workbenchStorageClient, clipboardStorageClient, host, logger, v3InteractiveRuntimeOptions{})
}

type v3InteractiveRuntimeOptions struct {
	SkipWorkbenchInitialLoad bool
	TUIConfig                state.TUIConfigStore
}

func newV3InteractiveRuntimeWithOptions(terminalID string, cols int, rows int, client *protocol.Client, workbenchStorageClient *protocol.Client, clipboardStorageClient *protocol.Client, host app.TerminalHost, logger *slog.Logger, opts v3InteractiveRuntimeOptions) *app.AppRuntime {
	initial := state.Root{
		Session: state.TerminalSessionStore{
			TerminalID: terminalID,
			Cols:       cols,
			Rows:       rows,
		},
		Surface: state.TerminalSurfaceStore{
			TerminalID: terminalID,
			Cols:       cols,
			Rows:       rows,
		},
		Config: opts.TUIConfig,
	}
	if terminalID == "" {
		initial.Shell = v3EmptyRootShell()
	}
	terminal := services.ProtocolTerminalServiceAdapter{Client: client}
	core := services.ProtocolCoreClientAdapter{Client: client}
	var storage services.WorkbenchStorageService
	var clipboardStorage services.ClipboardStorageService
	if workbenchStorageClient != nil {
		// core-v2 当前每个 protocol session 只有一个 events stream；
		// workbench storage.changed 必须独立于 terminal live events，避免互相取消。
		storage = services.ProtocolWorkbenchStorageAdapter{Client: workbenchStorageClient}
	}
	if clipboardStorageClient != nil {
		// clipboard storage.changed 使用独立 session，避免覆盖 workbench watch。
		clipboardStorage = services.ProtocolClipboardStorageAdapter{Client: clipboardStorageClient}
	}
	return app.NewInteractiveRuntimeWithStorage(
		initial,
		host,
		app.NewAsyncEffectRunner(),
		app.LiveDeps{Terminal: terminal, Logger: logger},
		app.CopyModeDeps{Core: core, Clipboard: &services.SystemClipboardService{}, Terminal: terminal, Rows: rows},
		app.WorkbenchDeps{Storage: storage, Ref: state.DefaultWorkbenchStorageRef(state.DefaultWorkspaceID), Logger: logger, SkipInitialLoad: opts.SkipWorkbenchInitialLoad},
		app.ClipboardDeps{Storage: clipboardStorage, Ref: state.DefaultClipboardStorageRef(state.DefaultWorkspaceID), Logger: logger},
	)
}

func v3EmptyRootShell() state.ShellStore {
	shell := state.DefaultShell()
	shell.Workspace.Tabs[0].Panes[0] = state.PaneState{
		ID:     state.DefaultPaneID,
		Title:  "unconnected",
		Kind:   state.PaneEmpty,
		Active: true,
	}
	return shell
}
