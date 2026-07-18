package main

import (
	"context"
	"fmt"
	"log/slog"
	"os/signal"
	"strings"
	"syscall"

	clientprotocol "github.com/lozzow/termx/client/adapter/protocol"
	endpointdomain "github.com/lozzow/termx/client/endpoint"
	clientruntime "github.com/lozzow/termx/client/runtime"
	clientruntimeadapter "github.com/lozzow/termx/tui/adapter/clientruntime"
	tuiprotocol "github.com/lozzow/termx/tui/adapter/protocol"
	systemadapter "github.com/lozzow/termx/tui/adapter/system"
	"github.com/lozzow/termx/tui/app"
	tuiport "github.com/lozzow/termx/tui/port"
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
	socketPath := strings.TrimSpace(socket)
	if socketPath != "" {
		socketPath = resolveV3Socket(socketPath)
	}
	return runV3Attach(ctx, v3AttachConfig{
		EndpointID: resolvedID, TerminalID: terminalID, SocketPath: socketPath, LogFile: resolveV3LogFilePath(logFile),
		TUIConfig: tuiConfig, ConnectionRegistry: registry,
	})
}

func newV3InteractiveRuntimeFromClientRuntime(terminalID string, cols, rows int, client *clientprotocol.ApplicationClient, host app.TerminalHost, logger *slog.Logger, opts v3InteractiveRuntimeOptions) *app.AppRuntime {
	endpointID := state.NormalizeEndpointID(opts.InitialEndpointID)
	if endpointID == "" {
		endpointID = state.DefaultEndpointID
	}
	var application *clientruntime.ApplicationSession
	var terminalClient tuiprotocol.ProtocolTerminalClient
	if client != nil {
		application = client.ApplicationSession
		terminalClient = client
	}
	terminalAdapter := tuiprotocol.ProtocolTerminalServiceAdapter{Client: terminalClient, Application: application}
	coreAdapter := tuiprotocol.ProtocolCoreClientAdapter{Application: application}
	pathAdapter, _ := tuiprotocol.NewProtocolPathServiceAdapter(application)
	var endpointEvents tuiport.EndpointEventSource
	if client != nil && client.ConnectionRuntime() != nil {
		endpointEvents = clientruntimeadapter.EndpointEventSource{Runtime: client.ConnectionRuntime(), EndpointID: endpointID}
	}

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
	if client != nil {
		workbench.Storage = tuiprotocol.ProtocolWorkbenchStorageAdapter{Application: client.ApplicationSession}
		workbench.Ref = state.DefaultWorkbenchStorageRef(state.DefaultWorkspaceID)
		workbench.Logger = logger
		workbench.SkipInitialLoad = opts.SkipWorkbenchInitialLoad
	}
	var clipboard app.ClipboardDeps
	if client != nil {
		clipboard.Storage = tuiprotocol.ProtocolClipboardStorageAdapter{Application: client.ApplicationSession}
		clipboard.Ref = state.DefaultClipboardStorageRef(state.DefaultWorkspaceID)
		clipboard.Logger = logger
	}
	return app.NewInteractiveRuntimeWithStorage(
		initial, host, app.NewAsyncEffectRunner(),
		app.LiveDeps{Terminal: terminalAdapter, Path: pathAdapter, EndpointEvents: endpointEvents, Logger: logger},
		app.CopyModeDeps{Core: coreAdapter, Clipboard: &systemadapter.ClipboardService{}, Terminal: terminalAdapter, Logger: logger, Rows: rows},
		workbench, clipboard,
	)
}

func openV3AttachProtocolClientsWithClientRuntime(ctx context.Context, cfg v3AttachConfig, logPath string, _ *slog.Logger) (*clientprotocol.ApplicationClient, func(), error) {
	endpointID := endpointIDFromState(state.NormalizeEndpointID(cfg.EndpointID))
	endpoint, ok := cfg.ConnectionRegistry.Endpoints[endpointID]
	if !ok {
		return nil, func() {}, fmt.Errorf("attach endpoint %q is not registered", endpointID)
	}
	application, closeClient, err := openEndpointProtocolClient(ctx, endpoint, cfg.SocketPath, logPath)
	if err != nil {
		return nil, func() {}, err
	}
	// terminal/workbench/clipboard 是同一 endpoint session 上的独立 Proto consumer；
	// subscription resource 已经提供隔离，不得再为它们伪造三条 generation。
	return application, closeClient, nil
}

func endpointIDFromState(id state.EndpointID) endpointdomain.EndpointID {
	return endpointdomain.EndpointID(id)
}
