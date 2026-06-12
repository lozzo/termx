package main

import (
	"context"
	"fmt"
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

type v3AttachRunner func(context.Context, v3AttachConfig) error

type v3AttachConfig struct {
	TerminalID string
	SocketPath string
	LogFile    string
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
			return runV3Attach(ctx, v3AttachConfig{
				TerminalID: args[0],
				SocketPath: resolveV3Socket(*socket),
				LogFile:    logPath,
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
	storageClient, err := v3DialClient(cfg.SocketPath)
	if err != nil {
		return fmt.Errorf("dial core-v2 storage events client: %w", err)
	}
	defer storageClient.Close()

	host := newV3TerminalHost()
	if err := host.Enter(ctx); err != nil {
		return err
	}
	defer host.Close()

	cols, rows, err := host.Size()
	if err != nil || cols <= 0 || rows <= 0 {
		cols, rows = 80, 24
	}
	runtime := newV3InteractiveRuntime(cfg.TerminalID, cols, rows, client, storageClient, host)
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

func newV3InteractiveRuntime(terminalID string, cols int, rows int, client *protocol.Client, storageClient *protocol.Client, host app.TerminalHost) *app.AppRuntime {
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
	}
	terminal := services.ProtocolTerminalServiceAdapter{Client: client}
	core := services.ProtocolCoreClientAdapter{Client: client}
	var storage services.WorkbenchStorageService
	if storageClient != nil {
		// core-v2 当前每个 protocol session 只有一个 events stream；
		// workbench storage.changed 必须独立于 terminal live events，避免互相取消。
		storage = services.ProtocolWorkbenchStorageAdapter{Client: storageClient}
	}
	return app.NewInteractiveRuntimeWithWorkbench(
		initial,
		host,
		app.NewAsyncEffectRunner(),
		app.LiveDeps{Terminal: terminal},
		app.CopyModeDeps{Core: core, Clipboard: &services.SystemClipboardService{}, Terminal: terminal, Rows: rows},
		app.WorkbenchDeps{Storage: storage, Ref: state.DefaultWorkbenchStorageRef(state.DefaultWorkspaceID)},
	)
}
