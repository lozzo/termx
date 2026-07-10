package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/termx-proto/cloudpb"
	"github.com/lozzow/termx/termx-proto/wire"
	"github.com/lozzow/termx/termx-shared/cloudcompanion"
	"github.com/lozzow/termx/termx-shared/connection"
	"github.com/lozzow/termx/termx-shared/perftrace"
	sshtransport "github.com/lozzow/termx/termx-shared/transport/ssh"
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
	TerminalID         string
	SocketPath         string
	LogFile            string
	TUIConfig          state.TUIConfigStore
	ConnectionRegistry connection.Registry
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
			connectionRegistry, err := loadV3ConnectionRegistry()
			if err != nil {
				return err
			}
			return runV3Attach(ctx, v3AttachConfig{
				TerminalID:         args[0],
				SocketPath:         resolveV3SocketForConnectionRegistry(*socket, connectionRegistry),
				LogFile:            logPath,
				TUIConfig:          tuiConfig,
				ConnectionRegistry: connectionRegistry,
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
	surfaceID := newV3RuntimeSurfaceID()
	runtime := newV3InteractiveRuntimeWithOptions(cfg.TerminalID, cols, rows, client, workbenchStorageClient, clipboardStorageClient, host, logger, v3InteractiveRuntimeOptions{
		RuntimeSurfaceID:   surfaceID,
		TUIConfig:          cfg.TUIConfig,
		ConnectionRegistry: cfg.ConnectionRegistry,
		EndpointContext:    ctx,
	})
	if err := runtime.Post(app.LiveAttachMsg{Config: app.LiveConfig{
		TerminalID:   cfg.TerminalID,
		Cols:         cols,
		Rows:         rows,
		Mode:         "collaborator",
		ResizePolicy: protocol.ResizePolicyFollower,
		SurfaceID:    surfaceID,
		ViewID:       state.TerminalPaneViewID(state.DefaultPaneID),
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
	RuntimeSurfaceID         string
	TUIConfig                state.TUIConfigStore
	ConnectionRegistry       connection.Registry
	EndpointContext          context.Context
}

func newV3InteractiveRuntimeWithOptions(terminalID string, cols int, rows int, client *protocol.Client, workbenchStorageClient *protocol.Client, clipboardStorageClient *protocol.Client, host app.TerminalHost, logger *slog.Logger, opts v3InteractiveRuntimeOptions) *app.AppRuntime {
	runtimeSurfaceID := opts.RuntimeSurfaceID
	if runtimeSurfaceID == "" {
		runtimeSurfaceID = app.DefaultRuntimeSurfaceID
	}
	initial := state.Root{
		RuntimeSurfaceID: runtimeSurfaceID,
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
	terminalAdapter := services.ProtocolTerminalServiceAdapter{Client: client}
	coreAdapter := services.ProtocolCoreClientAdapter{Client: client}
	pathAdapter := services.ProtocolPathServiceAdapter{Client: client}
	endpointCtx := opts.EndpointContext
	if endpointCtx == nil {
		endpointCtx = context.Background()
	}
	endpointManager := services.NewEndpointManagerWithDialers(normalizeV3ConnectionRegistry(opts.ConnectionRegistry), map[connection.TransportKind]services.EndpointDialer{
		connection.TransportSSH:    v3SSHEndpointDialer(endpointCtx),
		connection.TransportHubP2P: v3ManagedCloudEndpointDialer(),
	}, services.EndpointServiceBundle{
		EndpointID: state.DefaultEndpointID,
		Terminal:   terminalAdapter,
		Core:       coreAdapter,
		Surface:    terminalAdapter,
		LiveEvents: terminalAdapter,
		Path:       pathAdapter,
		Lifecycle:  services.EndpointLifecycle{Done: client.Done(), Err: client.Err},
	})
	initial.Endpoints = endpointManager.EndpointStore()
	terminal := endpointManager
	core := endpointManager
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
		app.LiveDeps{Terminal: terminal, Path: endpointManager, Logger: logger},
		app.CopyModeDeps{Core: core, Clipboard: &services.SystemClipboardService{}, Terminal: terminal, Logger: logger, Rows: rows},
		app.WorkbenchDeps{Storage: storage, Ref: state.DefaultWorkbenchStorageRef(state.DefaultWorkspaceID), Logger: logger, SkipInitialLoad: opts.SkipWorkbenchInitialLoad},
		app.ClipboardDeps{Storage: clipboardStorage, Ref: state.DefaultClipboardStorageRef(state.DefaultWorkspaceID), Logger: logger},
	)
}

func v3ManagedCloudEndpointDialer() services.EndpointDialer {
	return func(_ context.Context, cfg connection.Config) (services.EndpointServiceBundle, error) {
		// RP006 接入签名安装的本机 IPC；当前删除旧 Hub HTTP/grant-in-signaling 后必须明确 fail closed。
		err := cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_MISSING, "Cloud Companion is not installed or connected")
		return services.EndpointServiceBundle{}, fmt.Errorf("managed cloud endpoint %q: %w", cfg.ID, err)
	}
}

func v3SSHEndpointDialer(endpointCtx context.Context) services.EndpointDialer {
	if endpointCtx == nil {
		endpointCtx = context.Background()
	}
	return func(ctx context.Context, cfg connection.Config) (services.EndpointServiceBundle, error) {
		transport, err := sshtransport.Dial(endpointCtx, sshtransport.DialOptions{
			Address:      cfg.Address,
			AuthRef:      cfg.AuthRef,
			RemoteSocket: cfg.RemoteSocket,
		})
		if err != nil {
			return services.EndpointServiceBundle{}, fmt.Errorf("ssh endpoint %q dial: %w", cfg.ID, err)
		}
		client := protocol.NewClient(transport)
		if err := client.Hello(ctx, protocol.Hello{Version: wire.Version, Client: "termx-cli-v3:ssh:" + string(cfg.ID)}); err != nil {
			_ = client.Close()
			return services.EndpointServiceBundle{}, fmt.Errorf("ssh endpoint %q hello: %w", cfg.ID, err)
		}
		terminal := services.ProtocolTerminalServiceAdapter{Client: client}
		core := services.ProtocolCoreClientAdapter{Client: client}
		path := services.ProtocolPathServiceAdapter{Client: client}
		return services.EndpointServiceBundle{
			EndpointID: state.EndpointID(cfg.ID),
			Terminal:   terminal,
			Core:       core,
			Surface:    terminal,
			LiveEvents: terminal,
			Path:       path,
			Lifecycle:  services.EndpointLifecycle{Done: client.Done(), Err: client.Err},
		}, nil
	}
}

func newV3RuntimeSurfaceID() string {
	return fmt.Sprintf("termx-cli-v3:%d:%d", os.Getpid(), time.Now().UnixNano())
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
