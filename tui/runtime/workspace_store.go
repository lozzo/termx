package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tui/app"
	coreterminal "github.com/lozzow/termx/tui/core/terminal"
	"github.com/lozzow/termx/tui/core/types"
	coreworkspace "github.com/lozzow/termx/tui/core/workspace"
	featureoverlay "github.com/lozzow/termx/tui/features/overlay"
	featureterminalpool "github.com/lozzow/termx/tui/features/terminalpool"
	featureworkbench "github.com/lozzow/termx/tui/features/workbench"
)

const workspaceStateVersion = 1

// WorkspaceStore 定义工作区持久化边界，只保存可恢复状态，不落运行时 session。
type WorkspaceStore interface {
	Save(ctx context.Context, model app.Model) error
	Load(ctx context.Context) (app.Model, error)
}

type JSONWorkspaceStore struct {
	path string
}

type workspaceStateFile struct {
	Version int                    `json:"version"`
	Model   persistedAppModelState `json:"model"`
}

type persistedAppModelState struct {
	WorkspaceName string                    `json:"workspace_name"`
	Screen        app.Screen                `json:"screen"`
	Workbench     persistedWorkbenchState   `json:"workbench"`
	Pool          featureterminalpool.State `json:"pool"`
	Overlay       featureoverlay.State      `json:"overlay"`
}

type persistedWorkbenchState struct {
	Workspace *coreworkspace.Workspace                   `json:"workspace"`
	Terminals map[types.TerminalID]coreterminal.Metadata `json:"terminals"`
}

type restoreSnapshotClient interface {
	Snapshot(ctx context.Context, terminalID string, offset, limit int) (*protocol.Snapshot, error)
}

func NewWorkspaceStore(path string) *JSONWorkspaceStore {
	return &JSONWorkspaceStore{path: path}
}

func (s *JSONWorkspaceStore) Save(ctx context.Context, model app.Model) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(exportWorkspaceState(model), "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o644)
}

func (s *JSONWorkspaceStore) Load(ctx context.Context) (app.Model, error) {
	if err := ctx.Err(); err != nil {
		return app.Model{}, err
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return app.Model{}, err
	}
	var file workspaceStateFile
	if err := json.Unmarshal(data, &file); err != nil {
		return app.Model{}, err
	}
	if file.Version != workspaceStateVersion {
		return app.Model{}, fmt.Errorf("unsupported workspace state version %d", file.Version)
	}
	return restoreWorkspaceState(file), nil
}

// RebindWorkspaceSessions 在恢复后重新向 daemon 拉 snapshot，把运行时会话态补回内存。
func RebindWorkspaceSessions(ctx context.Context, client restoreSnapshotClient, model app.Model) (app.Model, error) {
	if client == nil {
		return model, nil
	}
	if model.Workbench.Sessions == nil {
		model.Workbench.Sessions = make(map[types.TerminalID]featureworkbench.SessionState)
	}
	for _, terminalID := range boundTerminalIDs(model.Workbench.Workspace) {
		snapshot, err := client.Snapshot(ctx, string(terminalID), 0, 0)
		if err != nil {
			return app.Model{}, err
		}
		session := model.Workbench.Sessions[terminalID]
		session.Snapshot = snapshot
		model.Workbench.Sessions[terminalID] = session
	}
	return model, nil
}

func exportWorkspaceState(model app.Model) workspaceStateFile {
	return workspaceStateFile{
		Version: workspaceStateVersion,
		Model: persistedAppModelState{
			WorkspaceName: model.WorkspaceName,
			Screen:        model.Screen,
			Workbench: persistedWorkbenchState{
				Workspace: model.Workbench.Workspace,
				Terminals: cloneTerminalMetadataMap(model.Workbench.Terminals),
			},
			Pool:    model.Pool,
			Overlay: model.Overlay,
		},
	}
}

func restoreWorkspaceState(file workspaceStateFile) app.Model {
	model := app.NewModel(file.Model.WorkspaceName)
	model.Screen = file.Model.Screen
	model.Workbench.Workspace = normalizeWorkspace(file.Model.Workbench.Workspace, model.WorkspaceName)
	model.Workbench.Terminals = cloneTerminalMetadataMap(file.Model.Workbench.Terminals)
	model.Workbench.Sessions = make(map[types.TerminalID]featureworkbench.SessionState)
	model.Pool = file.Model.Pool
	model.Overlay = file.Model.Overlay
	return model
}

func normalizeWorkspace(workspace *coreworkspace.Workspace, workspaceName string) *coreworkspace.Workspace {
	if workspace == nil {
		return coreworkspace.New(workspaceName)
	}
	if workspace.Tabs == nil {
		workspace.Tabs = make(map[types.TabID]*coreworkspace.TabState)
	}
	for tabID, tab := range workspace.Tabs {
		if tab == nil {
			delete(workspace.Tabs, tabID)
			continue
		}
		if tab.Panes == nil {
			tab.Panes = make(map[types.PaneID]coreworkspace.PaneState)
		}
	}
	return workspace
}

func cloneTerminalMetadataMap(items map[types.TerminalID]coreterminal.Metadata) map[types.TerminalID]coreterminal.Metadata {
	if len(items) == 0 {
		return make(map[types.TerminalID]coreterminal.Metadata)
	}
	out := make(map[types.TerminalID]coreterminal.Metadata, len(items))
	for id, meta := range items {
		cloned := meta
		cloned.Command = append([]string(nil), meta.Command...)
		cloned.AttachedPaneIDs = append([]types.PaneID(nil), meta.AttachedPaneIDs...)
		if len(meta.Tags) > 0 {
			cloned.Tags = make(map[string]string, len(meta.Tags))
			for key, value := range meta.Tags {
				cloned.Tags[key] = value
			}
		}
		out[id] = cloned
	}
	return out
}

func boundTerminalIDs(workspace *coreworkspace.Workspace) []types.TerminalID {
	if workspace == nil {
		return nil
	}
	seen := make(map[types.TerminalID]bool)
	ids := make([]types.TerminalID, 0)
	for _, tab := range workspace.Tabs {
		if tab == nil {
			continue
		}
		for _, pane := range tab.Panes {
			if pane.TerminalID == "" || seen[pane.TerminalID] {
				continue
			}
			seen[pane.TerminalID] = true
			ids = append(ids, pane.TerminalID)
		}
	}
	return ids
}
