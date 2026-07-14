package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/proto/wire"
	"github.com/lozzow/termx/shared/connection"
	"github.com/lozzow/termx/shared/perftrace"
	sshtransport "github.com/lozzow/termx/shared/transport/ssh"
	"github.com/lozzow/termx/tui/app"
	"github.com/lozzow/termx/tui/services"
	"github.com/lozzow/termx/tui/state"
	"github.com/lozzow/termx/tui/terminalhost"
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
	EndpointID         state.EndpointID
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
			return runLocalAttachCommand(cmd, args[0], *socket, *logFile, "")
		},
	}
}

func runLocalAttachCommand(cmd *cobra.Command, terminalID, socket, logFile, configPath string) error {
	return runAttachCommand(cmd, string(state.DefaultEndpointID), terminalID, socket, logFile, configPath)
}

func runAttachCommand(cmd *cobra.Command, endpointID, terminalID, socket, logFile, configPath string) error {
	if !isInteractiveTerminal() {
		return usageCLIError("termx terminal attach requires an interactive terminal")
	}
	if err := rejectNestedTUI(); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	tuiConfig, err := loadV3TUIConfig(configPath)
	if err != nil {
		return err
	}
	connectionRegistry, err := loadV3ConnectionRegistry()
	if err != nil {
		return err
	}
	resolvedEndpointID := connection.EndpointID(endpointID)
	if resolvedEndpointID == "" {
		resolvedEndpointID = connectionRegistry.Default
	}
	endpoint, ok := connectionRegistry.Connections[resolvedEndpointID]
	if !ok {
		return &cliError{code: 3, message: fmt.Sprintf("endpoint %s was not found", resolvedEndpointID)}
	}
	if !endpoint.Enabled {
		return &cliError{code: 4, message: fmt.Sprintf("endpoint %s is disabled", resolvedEndpointID)}
	}
	socketPath := resolveV3SocketForConnectionRegistry(socket, connectionRegistry)
	if endpoint.Transport == connection.TransportLocal {
		socketPath = strings.TrimSpace(endpoint.Socket)
		if strings.TrimSpace(socket) != "" {
			socketPath = socket
		}
		if socketPath == "" || socketPath == "auto" {
			socketPath = resolveV3Socket("")
		}
	}
	return runV3Attach(ctx, v3AttachConfig{
		EndpointID: state.EndpointID(resolvedEndpointID), TerminalID: terminalID, SocketPath: socketPath,
		LogFile: resolveV3LogFilePath(logFile), TUIConfig: tuiConfig, ConnectionRegistry: connectionRegistry,
	})
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
	client, workbenchStorageClient, clipboardStorageClient, closeClients, err := openV3AttachProtocolClients(ctx, cfg, logPath, logger)
	if err != nil {
		return err
	}
	defer closeClients()

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
		InitialEndpointID:  cfg.EndpointID,
		RuntimeSurfaceID:   surfaceID,
		TUIConfig:          cfg.TUIConfig,
		ConnectionRegistry: cfg.ConnectionRegistry,
		EndpointContext:    ctx,
	})
	if err := runtime.Post(app.LiveAttachMsg{Config: app.LiveConfig{
		EndpointID:   cfg.EndpointID,
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
	InitialEndpointID        state.EndpointID
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
	initialEndpointID := state.NormalizeEndpointID(opts.InitialEndpointID)
	initial := state.Root{
		RuntimeSurfaceID: runtimeSurfaceID,
		Session: state.TerminalSessionStore{
			EndpointID: initialEndpointID,
			TerminalID: terminalID,
			Cols:       cols,
			Rows:       rows,
		},
		Surface: state.TerminalSurfaceStore{
			EndpointID: initialEndpointID,
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
		connection.TransportLocal:  v3LocalEndpointDialer(logger),
		connection.TransportSSH:    v3SSHEndpointDialer(endpointCtx),
		connection.TransportHubP2P: v3ManagedCloudEndpointDialer(),
	}, services.EndpointServiceBundle{
		EndpointID: initialEndpointID,
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

func openV3AttachProtocolClients(ctx context.Context, cfg v3AttachConfig, logPath string, logger *slog.Logger) (*protocol.Client, *protocol.Client, *protocol.Client, func(), error) {
	endpointID := state.NormalizeEndpointID(cfg.EndpointID)
	endpoint, ok := cfg.ConnectionRegistry.Connections[connection.EndpointID(endpointID)]
	if !ok && endpointID == state.DefaultEndpointID {
		endpoint = connection.DefaultRegistry().Connections[connection.DefaultEndpointID]
		ok = true
	}
	if !ok {
		return nil, nil, nil, func() {}, fmt.Errorf("attach endpoint %q is not registered", endpointID)
	}
	if endpoint.Transport == connection.TransportLocal {
		client, err := dialOrStartV3Client(cfg.SocketPath, logPath, logger)
		if err != nil {
			return nil, nil, nil, func() {}, err
		}
		workbench, err := v3DialClient(cfg.SocketPath)
		if err != nil {
			_ = client.Close()
			return nil, nil, nil, func() {}, fmt.Errorf("dial core-v2 workbench storage events client: %w", err)
		}
		clipboard, err := v3DialClient(cfg.SocketPath)
		if err != nil {
			_ = workbench.Close()
			_ = client.Close()
			return nil, nil, nil, func() {}, fmt.Errorf("dial core-v2 clipboard storage events client: %w", err)
		}
		return client, workbench, clipboard, func() {
			_ = clipboard.Close()
			_ = workbench.Close()
			_ = client.Close()
		}, nil
	}
	// 远程 attach 的 terminal/live/history 共用一条 protocol session；
	// workbench/clipboard storage 不应把一次 attach 放大成额外 WebRTC session。
	client, closeClient, err := openEndpointProtocolClient(ctx, endpoint, "", logPath)
	if err != nil {
		return nil, nil, nil, func() {}, err
	}
	return client, nil, nil, closeClient, nil
}

func v3LocalEndpointDialer(logger *slog.Logger) services.EndpointDialer {
	return func(_ context.Context, cfg connection.Config) (services.EndpointServiceBundle, error) {
		socketPath := strings.TrimSpace(cfg.Socket)
		if socketPath == "" || socketPath == "auto" {
			socketPath = resolveV3Socket("")
		}
		client, err := dialOrStartV3Client(socketPath, resolveV3LogFilePath(""), logger)
		if err != nil {
			return services.EndpointServiceBundle{}, err
		}
		terminal := services.ProtocolTerminalServiceAdapter{Client: client}
		return services.EndpointServiceBundle{
			EndpointID: state.EndpointID(cfg.ID), Terminal: terminal,
			Core: services.ProtocolCoreClientAdapter{Client: client}, Surface: terminal, LiveEvents: terminal,
			Path: services.ProtocolPathServiceAdapter{Client: client}, Lifecycle: services.EndpointLifecycle{Done: client.Done(), Err: client.Err},
		}, nil
	}
}

func v3SSHEndpointDialer(endpointCtx context.Context) services.EndpointDialer {
	if endpointCtx == nil {
		endpointCtx = context.Background()
	}
	return func(ctx context.Context, cfg connection.Config) (services.EndpointServiceBundle, error) {
		client, err := dialV3SSHEndpointClient(endpointCtx, ctx, cfg)
		if err != nil {
			return services.EndpointServiceBundle{}, err
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

func dialV3SSHEndpointClient(endpointCtx, helloContext context.Context, cfg connection.Config) (*protocol.Client, error) {
	transport, err := sshtransport.Dial(endpointCtx, sshtransport.DialOptions{
		Address: cfg.Address, AuthRef: cfg.AuthRef, RemoteSocket: cfg.RemoteSocket,
	})
	if err != nil {
		return nil, fmt.Errorf("ssh endpoint %q dial: %w", cfg.ID, err)
	}
	client := protocol.NewClient(transport)
	if err := client.Hello(helloContext, protocol.Hello{Version: wire.Version, Client: "cmd/termx:ssh:" + string(cfg.ID)}); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ssh endpoint %q hello: %w", cfg.ID, err)
	}
	return client, nil
}

func newV3RuntimeSurfaceID() string {
	return fmt.Sprintf("cmd/termx-v3:%d:%d", os.Getpid(), time.Now().UnixNano())
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
