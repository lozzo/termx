package main

import (
	"context"
	"fmt"
	"log/slog"
	"os/signal"
	"strings"
	"syscall"

	endpointdomain "github.com/lozzow/termx/client/endpoint"
	clientruntime "github.com/lozzow/termx/client/runtime"
	"github.com/lozzow/termx/internal/protocol"
	protocoladapter "github.com/lozzow/termx/tui/adapter/protocol"
	systemadapter "github.com/lozzow/termx/tui/adapter/system"
	"github.com/lozzow/termx/tui/app"
	"github.com/lozzow/termx/tui/state"
	"github.com/spf13/cobra"
)

func runAttachCommandWithClientRuntime(cmd *cobra.Command, endpointID, terminalID, socket, logFile, configPath string) error {
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
	registry, err := loadV3ConnectionRegistry()
	if err != nil {
		return err
	}
	resolvedID := state.NormalizeEndpointID(state.EndpointID(endpointID))
	if strings.TrimSpace(endpointID) == "" {
		resolvedID = state.EndpointID(registry.Default)
	}
	endpoint, ok := registry.Endpoints[endpointIDFromState(resolvedID)]
	if !ok {
		return &cliError{code: 3, message: fmt.Sprintf("endpoint %s was not found", resolvedID)}
	}
	if !endpoint.Enabled {
		return &cliError{code: 4, message: fmt.Sprintf("endpoint %s is disabled", resolvedID)}
	}
	socketPath := resolveV3Socket(socket)
	if route, routeErr := selectCLIEndpointRoute(endpoint, ""); routeErr == nil && route.Kind == endpointdomain.RouteLocalUnix {
		if strings.TrimSpace(route.Socket) != "" && strings.TrimSpace(socket) == "" {
			socketPath = route.Socket
		}
	}
	return runV3Attach(ctx, v3AttachConfig{
		EndpointID: resolvedID, TerminalID: terminalID, SocketPath: socketPath, LogFile: resolveV3LogFilePath(logFile),
		TUIConfig: tuiConfig, ConnectionRegistry: registry,
	})
}

func newV3InteractiveRuntimeFromClientRuntime(terminalID string, cols, rows int, client, workbenchClient, clipboardClient *protocol.Client, host app.TerminalHost, logger *slog.Logger, opts v3InteractiveRuntimeOptions) *app.AppRuntime {
	endpointID := state.NormalizeEndpointID(opts.InitialEndpointID)
	if endpointID == "" {
		endpointID = state.DefaultEndpointID
	}
	routeID := "local"
	application := mustCLIApplicationSession(client, endpointID, routeID)
	terminalAdapter := protocoladapter.ProtocolTerminalServiceAdapter{Client: client, Application: application}
	coreAdapter := protocoladapter.ProtocolCoreClientAdapter{Application: application}
	pathAdapter, _ := protocoladapter.NewProtocolPathServiceAdapter(application)

	initial := state.Root{
		RuntimeSurfaceID: opts.RuntimeSurfaceID,
		Session:          state.TerminalSessionStore{EndpointID: endpointID, TerminalID: terminalID, Cols: cols, Rows: rows},
		Surface:          state.TerminalSurfaceStore{EndpointID: endpointID, TerminalID: terminalID, Cols: cols, Rows: rows},
		Config:           opts.TUIConfig,
		Endpoints:        state.EndpointStore{}.ApplyConnectionRegistry(opts.ConnectionRegistry),
	}
	if initial.RuntimeSurfaceID == "" {
		initial.RuntimeSurfaceID = app.DefaultRuntimeSurfaceID
	}
	if terminalID == "" {
		initial.Shell = v3EmptyRootShell()
	}

	var workbench app.WorkbenchDeps
	if workbenchClient != nil {
		workbench.Storage = protocoladapter.ProtocolWorkbenchStorageAdapter{Application: mustCLIApplicationSession(workbenchClient, endpointID, routeID)}
		workbench.Ref = state.DefaultWorkbenchStorageRef(state.DefaultWorkspaceID)
		workbench.Logger = logger
		workbench.SkipInitialLoad = opts.SkipWorkbenchInitialLoad
	}
	var clipboard app.ClipboardDeps
	if clipboardClient != nil {
		clipboard.Storage = protocoladapter.ProtocolClipboardStorageAdapter{Application: mustCLIApplicationSession(clipboardClient, endpointID, routeID)}
		clipboard.Ref = state.DefaultClipboardStorageRef(state.DefaultWorkspaceID)
		clipboard.Logger = logger
	}
	return app.NewInteractiveRuntimeWithStorage(
		initial, host, app.NewAsyncEffectRunner(),
		app.LiveDeps{Terminal: terminalAdapter, Path: pathAdapter, Logger: logger},
		app.CopyModeDeps{Core: coreAdapter, Clipboard: &systemadapter.ClipboardService{}, Terminal: terminalAdapter, Logger: logger, Rows: rows},
		workbench, clipboard,
	)
}

func openV3AttachProtocolClientsWithClientRuntime(ctx context.Context, cfg v3AttachConfig, logPath string, _ *slog.Logger) (*protocol.Client, *protocol.Client, *protocol.Client, func(), error) {
	endpointID := endpointIDFromState(state.NormalizeEndpointID(cfg.EndpointID))
	endpoint, ok := cfg.ConnectionRegistry.Endpoints[endpointID]
	if !ok {
		return nil, nil, nil, func() {}, fmt.Errorf("attach endpoint %q is not registered", endpointID)
	}
	route, err := selectCLIEndpointRoute(endpoint, "")
	if err != nil {
		return nil, nil, nil, func() {}, err
	}
	application, closeClient, err := openEndpointRouteProtocolClient(ctx, endpoint, route, cfg.SocketPath, logPath)
	if err != nil {
		return nil, nil, nil, func() {}, err
	}
	client := application.Client
	if route.Kind != endpointdomain.RouteLocalUnix {
		return client, nil, nil, closeClient, nil
	}
	workbench, err := dialV3Client(cfg.SocketPath)
	if err != nil {
		closeClient()
		return nil, nil, nil, func() {}, fmt.Errorf("dial workbench event session: %w", err)
	}
	clipboard, err := dialV3Client(cfg.SocketPath)
	if err != nil {
		_ = workbench.Close()
		closeClient()
		return nil, nil, nil, func() {}, fmt.Errorf("dial clipboard event session: %w", err)
	}
	return client, workbench, clipboard, func() {
		_ = clipboard.Close()
		_ = workbench.Close()
		closeClient()
	}, nil
}

func mustCLIApplicationSession(client *protocol.Client, endpointID state.EndpointID, routeID string) *clientruntime.ApplicationSession {
	session, err := clientruntime.NewApplicationSession(clientruntime.EndpointSessionStamp{
		EndpointID: endpointIDFromState(endpointID), RouteID: endpointdomain.RouteID(routeID), Generation: clientruntime.SessionGeneration(nextCLIEndpointGeneration.Add(1)),
	}, client)
	if err != nil {
		panic(err)
	}
	return session
}

func endpointIDFromState(id state.EndpointID) endpointdomain.EndpointID {
	return endpointdomain.EndpointID(id)
}
