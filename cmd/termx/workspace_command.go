package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/shared/connection"
	"github.com/spf13/cobra"
)

type workspaceCommandRuntime struct {
	socket     *string
	logFile    *string
	endpointID string
	timeout    time.Duration
}

type workspaceView struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	Active      bool               `json:"active"`
	ActiveTabID string             `json:"active_tab_id"`
	Tabs        []workspaceTabView `json:"tabs,omitempty"`
	TabCount    int                `json:"tab_count"`
	PaneCount   int                `json:"pane_count"`
}

type workspaceTabView struct {
	ID           string              `json:"id"`
	Title        string              `json:"title"`
	Active       bool                `json:"active"`
	ActivePaneID string              `json:"active_pane_id"`
	Panes        []workspacePaneView `json:"panes"`
	RootSplit    workspaceSplitView  `json:"root_split"`
}

type workspacePaneView struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Kind       string `json:"kind"`
	TerminalID string `json:"terminal_id,omitempty"`
}

type workspaceSplitView struct {
	PaneID      string               `json:"pane_id,omitempty"`
	Direction   string               `json:"direction,omitempty"`
	Children    []workspaceSplitView `json:"children,omitempty"`
	Ratio       float64              `json:"ratio,omitempty"`
	BiasCells   int                  `json:"bias_cells,omitempty"`
	FixedPaneID string               `json:"fixed_pane_id,omitempty"`
	FixedCols   int                  `json:"fixed_cols,omitempty"`
	FixedRows   int                  `json:"fixed_rows,omitempty"`
}

func newWorkspaceCommand(socket, logFile *string) *cobra.Command {
	runtime := &workspaceCommandRuntime{socket: socket, logFile: logFile}
	command := &cobra.Command{Use: "workspace", Short: "Manage daemon-owned workbench workspace projections"}
	command.PersistentFlags().StringVar(&runtime.endpointID, "endpoint", "", "owning endpoint ID (default: registry default)")
	command.PersistentFlags().DurationVar(&runtime.timeout, "timeout", 30*time.Second, "operation timeout")
	command.AddCommand(
		newWorkspaceListCommand(runtime), newWorkspaceShowCommand(runtime), newWorkspaceCreateCommand(runtime),
		newWorkspaceRenameCommand(runtime), newWorkspaceRemoveCommand(runtime), newWorkspaceExportCommand(runtime),
	)
	return command
}

func (runtime *workspaceCommandRuntime) open(cmd *cobra.Command) (context.Context, *protocol.Client, connection.Endpoint, func(), error) {
	if runtime.timeout <= 0 {
		return nil, nil, connection.Endpoint{}, func() {}, usageCLIError("--timeout must be positive")
	}
	registry, err := loadNormalizedConnectionRegistry()
	if err != nil {
		return nil, nil, connection.Endpoint{}, func() {}, err
	}
	endpoint, err := resolveEndpointConfig(runtime.endpointID, registry)
	if err != nil {
		return nil, nil, connection.Endpoint{}, func() {}, err
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), runtime.timeout)
	cmd.Root().SilenceUsage = true
	client, closeClient, err := openEndpointProtocolClient(ctx, endpoint, *runtime.socket, *runtime.logFile)
	if err != nil {
		cancel()
		return nil, nil, connection.Endpoint{}, func() {}, classifyCLIError(err)
	}
	closeAll := func() { closeClient(); cancel() }
	return ctx, client, endpoint, closeAll, nil
}

func newWorkspaceListCommand(runtime *workspaceCommandRuntime) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use: "list", Short: "List workspaces from the owning daemon", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, client, endpoint, closeClient, err := runtime.open(cmd)
			if err != nil {
				return err
			}
			defer closeClient()
			snapshot, err := client.WorkbenchGet(ctx, protocol.WorkbenchGetParams{})
			if err != nil {
				return classifyCLIError(err)
			}
			views := workspaceViews(*snapshot)
			if jsonOutput {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
					SchemaVersion int             `json:"schema_version"`
					Kind          string          `json:"kind"`
					EndpointID    string          `json:"endpoint_id"`
					Version       uint64          `json:"version"`
					Items         []workspaceView `json:"items"`
				}{1, "workspace_list", string(endpoint.ID), snapshot.Version, views})
			}
			fmt.Fprintln(cmd.OutOrStdout(), "ID\tNAME\tACTIVE\tTABS\tPANES")
			for _, view := range views {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%t\t%d\t%d\n", view.ID, view.Name, view.Active, view.TabCount, view.PaneCount)
			}
			return nil
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "print machine-readable JSON")
	return command
}

func newWorkspaceShowCommand(runtime *workspaceCommandRuntime) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use: "show ID", Short: "Show one daemon-owned workspace", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, client, endpoint, closeClient, err := runtime.open(cmd)
			if err != nil {
				return err
			}
			defer closeClient()
			snapshot, err := client.WorkbenchGet(ctx, protocol.WorkbenchGetParams{})
			if err != nil {
				return classifyCLIError(err)
			}
			view, ok := workspaceViewByID(*snapshot, args[0])
			if !ok {
				return &cliError{code: 3, message: fmt.Sprintf("workspace %s was not found", args[0])}
			}
			if jsonOutput {
				return writeWorkspaceEnvelope(cmd, "workspace", endpoint.ID, snapshot.Version, view)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "ID: %s\nName: %s\nActive: %t\nTabs: %d\nPanes: %d\n", view.ID, view.Name, view.Active, view.TabCount, view.PaneCount)
			return nil
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "print machine-readable JSON")
	return command
}

func newWorkspaceCreateCommand(runtime *workspaceCommandRuntime) *cobra.Command {
	var id, name string
	var jsonOutput bool
	command := &cobra.Command{
		Use: "create", Short: "Create a workspace through the owning daemon", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(name) == "" {
				return usageCLIError("--name is required")
			}
			return runWorkspaceMutation(cmd, runtime, protocol.WorkbenchMutateParams{Action: protocol.WorkbenchMutationWorkspaceCreate, TargetID: strings.TrimSpace(id), Name: strings.TrimSpace(name)}, jsonOutput)
		},
	}
	command.Flags().StringVar(&id, "id", "", "stable workspace ID (daemon generates one when omitted)")
	command.Flags().StringVar(&name, "name", "", "workspace display name")
	command.Flags().BoolVar(&jsonOutput, "json", false, "print machine-readable JSON")
	return command
}

func newWorkspaceRenameCommand(runtime *workspaceCommandRuntime) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use: "rename ID NAME", Short: "Rename a daemon-owned workspace", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(args[1]) == "" {
				return usageCLIError("workspace name cannot be empty")
			}
			return runWorkspaceMutation(cmd, runtime, protocol.WorkbenchMutateParams{Action: protocol.WorkbenchMutationWorkspaceRename, WorkspaceID: args[0], Name: strings.TrimSpace(args[1])}, jsonOutput)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "print machine-readable JSON")
	return command
}

func newWorkspaceRemoveCommand(runtime *workspaceCommandRuntime) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use: "remove ID", Short: "Remove a daemon-owned workspace", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkspaceMutation(cmd, runtime, protocol.WorkbenchMutateParams{Action: protocol.WorkbenchMutationWorkspaceDelete, WorkspaceID: args[0]}, jsonOutput)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "print machine-readable JSON")
	return command
}

func newWorkspaceExportCommand(runtime *workspaceCommandRuntime) *cobra.Command {
	var outputPath string
	command := &cobra.Command{
		Use: "export ID", Short: "Export one daemon-owned workspace projection", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, client, endpoint, closeClient, err := runtime.open(cmd)
			if err != nil {
				return err
			}
			defer closeClient()
			snapshot, err := client.WorkbenchGet(ctx, protocol.WorkbenchGetParams{})
			if err != nil {
				return classifyCLIError(err)
			}
			view, ok := workspaceViewByID(*snapshot, args[0])
			if !ok {
				return &cliError{code: 3, message: fmt.Sprintf("workspace %s was not found", args[0])}
			}
			payload, err := json.MarshalIndent(struct {
				SchemaVersion int           `json:"schema_version"`
				Kind          string        `json:"kind"`
				EndpointID    string        `json:"endpoint_id"`
				Version       uint64        `json:"version"`
				Item          workspaceView `json:"item"`
			}{1, "workspace_export", string(endpoint.ID), snapshot.Version, view}, "", "  ")
			if err != nil {
				return err
			}
			payload = append(payload, '\n')
			if strings.TrimSpace(outputPath) == "" {
				_, err = cmd.OutOrStdout().Write(payload)
				return err
			}
			if err := writeV3PrivateFile(outputPath, payload); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Workspace %s exported to %s\n", args[0], outputPath)
			return nil
		},
	}
	command.Flags().StringVar(&outputPath, "out", "", "write export to an owner-only JSON file")
	return command
}

func runWorkspaceMutation(cmd *cobra.Command, runtime *workspaceCommandRuntime, params protocol.WorkbenchMutateParams, jsonOutput bool) error {
	ctx, client, endpoint, closeClient, err := runtime.open(cmd)
	if err != nil {
		return err
	}
	defer closeClient()
	// Workbench snapshot/version 属于 daemon；CLI 只提交带 expected version 的单次 mutation。
	current, err := client.WorkbenchGet(ctx, protocol.WorkbenchGetParams{})
	if err != nil {
		return classifyCLIError(err)
	}
	params.CheckVersion = true
	params.ExpectedVersion = current.Version
	result, err := client.WorkbenchApply(ctx, params)
	if err != nil {
		return classifyCLIError(err)
	}
	if jsonOutput {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
			SchemaVersion int    `json:"schema_version"`
			Kind          string `json:"kind"`
			EndpointID    string `json:"endpoint_id"`
			Action        string `json:"action"`
			ResourceID    string `json:"resource_id"`
			Version       uint64 `json:"version"`
		}{1, "workspace_mutation", string(endpoint.ID), string(result.Action), result.ResourceID, result.Snapshot.Version})
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\tversion=%d\n", result.ResourceID, result.Action, result.Snapshot.Version)
	return nil
}

func writeWorkspaceEnvelope(cmd *cobra.Command, kind string, endpointID connection.EndpointID, version uint64, view workspaceView) error {
	return json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
		SchemaVersion int           `json:"schema_version"`
		Kind          string        `json:"kind"`
		EndpointID    string        `json:"endpoint_id"`
		Version       uint64        `json:"version"`
		Item          workspaceView `json:"item"`
	}{1, kind, string(endpointID), version, view})
}

func workspaceViews(snapshot protocol.WorkbenchSnapshot) []workspaceView {
	views := make([]workspaceView, 0, len(snapshot.Workspaces))
	for _, workspace := range snapshot.Workspaces {
		view := workspaceView{ID: workspace.ID, Name: workspace.Name, Active: workspace.ID == snapshot.ActiveWorkspaceID, ActiveTabID: workspace.ActiveTabID, TabCount: len(workspace.Tabs)}
		view.Tabs = make([]workspaceTabView, 0, len(workspace.Tabs))
		for _, tab := range workspace.Tabs {
			tabView := workspaceTabView{ID: tab.ID, Title: tab.Title, Active: tab.ID == workspace.ActiveTabID, ActivePaneID: tab.ActivePaneID, RootSplit: workspaceSplitProjection(tab.RootSplit)}
			tabView.Panes = make([]workspacePaneView, 0, len(tab.Panes))
			for _, pane := range tab.Panes {
				tabView.Panes = append(tabView.Panes, workspacePaneView{pane.ID, pane.Title, string(pane.Kind), pane.TerminalID})
			}
			view.PaneCount += len(tab.Panes)
			view.Tabs = append(view.Tabs, tabView)
		}
		views = append(views, view)
	}
	return views
}

func workspaceViewByID(snapshot protocol.WorkbenchSnapshot, id string) (workspaceView, bool) {
	for _, view := range workspaceViews(snapshot) {
		if view.ID == id {
			return view, true
		}
	}
	return workspaceView{}, false
}

func workspaceSplitProjection(split protocol.WorkbenchSplitNode) workspaceSplitView {
	view := workspaceSplitView{
		PaneID: split.PaneID, Direction: string(split.Direction), Ratio: split.Ratio, BiasCells: split.BiasCells,
		FixedPaneID: split.FixedPaneID, FixedCols: split.FixedCols, FixedRows: split.FixedRows,
	}
	view.Children = make([]workspaceSplitView, 0, len(split.Children))
	for _, child := range split.Children {
		view.Children = append(view.Children, workspaceSplitProjection(child))
	}
	return view
}
