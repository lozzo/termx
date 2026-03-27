package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/protocol"
)

type workspaceStateFile struct {
	Version         int                   `json:"version"`
	ActiveWorkspace int                   `json:"active_workspace,omitempty"`
	Workspaces      []workspaceStateEntry `json:"workspaces,omitempty"`
	Workspace       workspaceStateEntry   `json:"workspace,omitempty"`
}

type workspaceStateEntry struct {
	Name      string              `json:"name"`
	ActiveTab int                 `json:"active_tab"`
	Tabs      []workspaceStateTab `json:"tabs"`
}

type workspaceStateTab struct {
	Name              string                   `json:"name"`
	ActivePaneID      string                   `json:"active_pane_id,omitempty"`
	ZoomedPaneID      string                   `json:"zoomed_pane_id,omitempty"`
	LayoutPreset      int                      `json:"layout_preset,omitempty"`
	FloatingVisible   bool                     `json:"floating_visible"`
	AutoAcquireResize bool                     `json:"auto_acquire_resize,omitempty"`
	Root              *workspaceStateNode      `json:"root,omitempty"`
	Panes             []workspaceStatePane     `json:"panes"`
	Floating          []workspaceStateFloating `json:"floating,omitempty"`
}

type workspaceStatePane struct {
	ID            string            `json:"id"`
	Title         string            `json:"title,omitempty"`
	TerminalID    string            `json:"terminal_id,omitempty"`
	Name          string            `json:"name,omitempty"`
	Command       []string          `json:"command,omitempty"`
	Tags          map[string]string `json:"tags,omitempty"`
	TerminalState string            `json:"terminal_state,omitempty"`
	ExitCode      *int              `json:"exit_code,omitempty"`
	Mode          ViewportMode      `json:"mode,omitempty"`
	Offset        Point             `json:"offset,omitempty"`
	Pin           bool              `json:"pin,omitempty"`
	Readonly      bool              `json:"readonly,omitempty"`
}

type workspaceStateFloating struct {
	PaneID string `json:"pane_id"`
	Rect   Rect   `json:"rect"`
	Z      int    `json:"z"`
}

type workspaceStateNode struct {
	PaneID    string              `json:"pane_id,omitempty"`
	Direction SplitDirection      `json:"direction,omitempty"`
	Ratio     float64             `json:"ratio,omitempty"`
	First     *workspaceStateNode `json:"first,omitempty"`
	Second    *workspaceStateNode `json:"second,omitempty"`
}

func (m *Model) saveWorkspaceStateCmd(path string) tea.Cmd {
	return func() tea.Msg {
		if strings.TrimSpace(path) == "" {
			return nil
		}
		data, err := exportModelWorkspaceStateJSON(m)
		if err != nil {
			return errMsg{err}
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return errMsg{err}
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return errMsg{err}
		}
		return noticeMsg{text: "saved workspace state"}
	}
}

func persistWorkspaceState(model tea.Model, path string, logger *slog.Logger) error {
	if strings.TrimSpace(path) == "" || model == nil {
		return nil
	}
	current, ok := model.(*Model)
	if !ok || current == nil {
		return nil
	}
	data, err := exportModelWorkspaceStateJSON(current)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return err
	}
	if logger != nil {
		logger.Info("persisted workspace state", "path", path)
	}
	return nil
}

func (m *Model) loadWorkspaceStateCmd(path string) tea.Cmd {
	return func() tea.Msg {
		data, err := os.ReadFile(path)
		if err != nil {
			return errMsg{err}
		}
		state, err := parseWorkspaceStateJSON(data)
		if err != nil {
			return errMsg{err}
		}
		ctx, cancel := m.requestContext()
		defer cancel()
		result, err := m.client.List(ctx)
		if err != nil {
			return errMsg{m.wrapClientError("list terminals", err)}
		}
		workspace, store, order, active, err := buildWorkspaceSetFromState(state, result.Terminals)
		if err != nil {
			return errMsg{err}
		}
		if err := m.hydrateWorkspaceRuntime(workspace, result.Terminals); err != nil {
			return errMsg{err}
		}
		store[workspace.Name] = *workspace
		return workspaceStateLoadedMsg{
			workspace: *workspace,
			store:     store,
			order:     order,
			active:    active,
			notice:    "restored workspace state",
		}
	}
}

func (m *Model) resolveAutoStartupLayoutPath() (string, error) {
	projectDirs, err := projectLayoutDirs()
	if err != nil {
		return "", err
	}
	for _, dir := range projectDirs {
		for _, name := range []string{"layout.yaml", "layout.yml"} {
			path := filepath.Join(filepath.Dir(dir), name)
			if exists, err := fileExists(path); err != nil {
				return "", err
			} else if exists {
				return path, nil
			}
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	for _, name := range []string{"default-layout.yaml", "default-layout.yml"} {
		path := filepath.Join(home, ".config", "termx", name)
		if exists, err := fileExists(path); err != nil {
			return "", err
		} else if exists {
			return path, nil
		}
	}
	return "", nil
}

func exportWorkspaceStateJSON(workspace *Workspace) ([]byte, error) {
	state, err := exportWorkspaceState(workspace)
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(state, "", "  ")
}

func exportModelWorkspaceStateJSON(model *Model) ([]byte, error) {
	state, err := exportModelWorkspaceState(model)
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(state, "", "  ")
}

func exportModelWorkspaceState(model *Model) (*workspaceStateFile, error) {
	if model == nil {
		return nil, fmt.Errorf("model is nil")
	}
	model.snapshotCurrentWorkspace()
	order := append([]string(nil), model.workspaceOrder...)
	store := model.workspaceStore
	active := model.activeWorkspace
	if model.workbench != nil {
		order = model.workbench.Order()
		store = model.workbench.CloneStore()
		active = model.workbench.ActiveWorkspaceIndex()
	}
	state := &workspaceStateFile{
		Version:         1,
		ActiveWorkspace: active,
		Workspaces:      make([]workspaceStateEntry, 0, len(order)),
	}
	for _, name := range order {
		workspace, ok := store[name]
		if !ok {
			continue
		}
		entry, err := exportWorkspaceStateEntry(&workspace)
		if err != nil {
			return nil, err
		}
		state.Workspaces = append(state.Workspaces, *entry)
	}
	if len(state.Workspaces) == 1 {
		state.Workspace = state.Workspaces[0]
	}
	return state, nil
}

func exportWorkspaceState(workspace *Workspace) (*workspaceStateFile, error) {
	if workspace == nil {
		return nil, fmt.Errorf("workspace is nil")
	}
	state := &workspaceStateFile{
		Version: 1,
	}
	entry, err := exportWorkspaceStateEntry(workspace)
	if err != nil {
		return nil, err
	}
	state.Workspace = *entry
	state.Workspaces = []workspaceStateEntry{*entry}
	return state, nil
}

func exportWorkspaceStateEntry(workspace *Workspace) (*workspaceStateEntry, error) {
	if workspace == nil {
		return nil, fmt.Errorf("workspace is nil")
	}
	entry := &workspaceStateEntry{
		Name:      workspace.Name,
		ActiveTab: workspace.ActiveTab,
		Tabs:      make([]workspaceStateTab, 0, len(workspace.Tabs)),
	}
	for _, tab := range workspace.Tabs {
		if tab == nil {
			continue
		}
		tabEntry := workspaceStateTab{
			Name:              tab.Name,
			ActivePaneID:      tab.ActivePaneID,
			ZoomedPaneID:      tab.ZoomedPaneID,
			LayoutPreset:      tab.LayoutPreset,
			FloatingVisible:   tab.FloatingVisible,
			AutoAcquireResize: tab.AutoAcquireResize,
			Panes:             make([]workspaceStatePane, 0, len(tab.Panes)),
			Floating:          make([]workspaceStateFloating, 0, len(tab.Floating)),
		}
		if tab.Root != nil {
			tabEntry.Root = exportWorkspaceStateNode(tab.Root)
		}
		paneIDs := make([]string, 0, len(tab.Panes))
		for paneID := range tab.Panes {
			paneIDs = append(paneIDs, paneID)
		}
		slices.Sort(paneIDs)
		for _, paneID := range paneIDs {
			pane := tab.Panes[paneID]
			if pane == nil {
				continue
			}
			tabEntry.Panes = append(tabEntry.Panes, workspaceStatePane{
				ID:            pane.ID,
				Title:         pane.Title,
				TerminalID:    pane.TerminalID,
				Name:          pane.Name,
				Command:       append([]string(nil), pane.Command...),
				Tags:          cloneStringMap(pane.Tags),
				TerminalState: pane.TerminalState,
				ExitCode:      pane.ExitCode,
				Mode:          pane.Mode,
				Offset:        pane.Offset,
				Pin:           pane.Pin,
				Readonly:      pane.Readonly,
			})
		}
		for _, floating := range tab.Floating {
			if floating == nil {
				continue
			}
			tabEntry.Floating = append(tabEntry.Floating, workspaceStateFloating{
				PaneID: floating.PaneID,
				Rect:   floating.Rect,
				Z:      floating.Z,
			})
		}
		entry.Tabs = append(entry.Tabs, tabEntry)
	}
	return entry, nil
}

func parseWorkspaceStateJSON(data []byte) (*workspaceStateFile, error) {
	var state workspaceStateFile
	if err := json.Unmarshal(data, &state); err != nil {
		decoder := json.NewDecoder(bytes.NewReader(data))
		if decodeErr := decoder.Decode(&state); decodeErr != nil {
			return nil, err
		}
	}
	if state.Version == 0 {
		state.Version = 1
	}
	if state.Version != 1 {
		return nil, fmt.Errorf("unsupported workspace state version %d", state.Version)
	}
	if len(state.Workspaces) == 0 && (state.Workspace.Name != "" || len(state.Workspace.Tabs) > 0) {
		state.Workspaces = []workspaceStateEntry{state.Workspace}
	}
	if len(state.Workspaces) == 0 {
		return nil, fmt.Errorf("workspace state has no workspaces")
	}
	if state.ActiveWorkspace < 0 || state.ActiveWorkspace >= len(state.Workspaces) {
		state.ActiveWorkspace = 0
	}
	if state.Workspace.Name == "" {
		state.Workspace = state.Workspaces[state.ActiveWorkspace]
	}
	return &state, nil
}

func buildWorkspaceSetFromState(state *workspaceStateFile, terminals []protocol.TerminalInfo) (*Workspace, map[string]Workspace, []string, int, error) {
	if state == nil {
		return nil, nil, nil, 0, fmt.Errorf("workspace state is nil")
	}
	store := make(map[string]Workspace, len(state.Workspaces))
	order := make([]string, 0, len(state.Workspaces))
	var activeWorkspace *Workspace
	for idx, entry := range state.Workspaces {
		workspace, err := buildWorkspaceFromStateEntry(entry, terminals)
		if err != nil {
			return nil, nil, nil, 0, err
		}
		store[workspace.Name] = *workspace
		order = append(order, workspace.Name)
		if idx == state.ActiveWorkspace {
			activeWorkspace = workspace
		}
	}
	if activeWorkspace == nil {
		return nil, nil, nil, 0, fmt.Errorf("active workspace missing")
	}
	return activeWorkspace, store, order, state.ActiveWorkspace, nil
}

func buildWorkspaceFromState(state *workspaceStateFile, terminals []protocol.TerminalInfo) (*Workspace, error) {
	workspace, _, _, _, err := buildWorkspaceSetFromState(state, terminals)
	return workspace, err
}

func buildWorkspaceFromStateEntry(entry workspaceStateEntry, terminals []protocol.TerminalInfo) (*Workspace, error) {
	workspace := &Workspace{
		Name:      entry.Name,
		ActiveTab: entry.ActiveTab,
		Tabs:      make([]*Tab, 0, len(entry.Tabs)),
	}
	for _, tabState := range entry.Tabs {
		tab := newTab(tabState.Name)
		tab.ActivePaneID = tabState.ActivePaneID
		tab.ZoomedPaneID = tabState.ZoomedPaneID
		tab.LayoutPreset = tabState.LayoutPreset
		tab.FloatingVisible = tabState.FloatingVisible
		tab.AutoAcquireResize = tabState.AutoAcquireResize
		for _, paneState := range tabState.Panes {
			pane := &Pane{
				ID:    paneState.ID,
				Title: paneState.Title,
				Viewport: &Viewport{
					TerminalID:    paneState.TerminalID,
					Name:          paneState.Name,
					Command:       append([]string(nil), paneState.Command...),
					Tags:          cloneStringMap(paneState.Tags),
					TerminalState: paneState.TerminalState,
					ExitCode:      paneState.ExitCode,
					Mode:          defaultViewportMode(paneState.Mode, ViewportModeFit),
					Offset:        paneState.Offset,
					Pin:           paneState.Pin,
					Readonly:      paneState.Readonly,
					renderDirty:   true,
				},
			}
			if info := findTerminalInfo(terminals, paneState.TerminalID); info != nil {
				pane.TerminalID = info.ID
				pane.Name = info.Name
				pane.Command = append([]string(nil), info.Command...)
				pane.Tags = cloneStringMap(info.Tags)
				pane.TerminalState = defaultTerminalState(info.State)
				pane.ExitCode = info.ExitCode
				if pane.Title == "" {
					pane.Title = paneTitleForTerminal(*info)
				}
			} else if pane.TerminalID != "" && pane.TerminalState == "running" {
				pane.TerminalState = "exited"
			}
			if pane.Title == "" {
				pane.Title = paneTitleForCommand(pane.Name, firstCommandWord(pane.Command), pane.TerminalID)
			}
			tab.Panes[pane.ID] = pane
		}
		if tabState.Root != nil {
			root, err := buildWorkspaceStateNode(tabState.Root, tab.Panes)
			if err != nil {
				return nil, err
			}
			tab.Root = root
		}
		for _, floating := range tabState.Floating {
			if _, ok := tab.Panes[floating.PaneID]; !ok {
				continue
			}
			tab.Floating = append(tab.Floating, &FloatingPane{
				PaneID: floating.PaneID,
				Rect:   floating.Rect,
				Z:      floating.Z,
			})
		}
		if tab.ActivePaneID == "" || tab.Panes[tab.ActivePaneID] == nil {
			tab.ActivePaneID = firstPaneID(tab.Panes)
		}
		workspace.Tabs = append(workspace.Tabs, tab)
	}
	if len(workspace.Tabs) == 0 {
		workspace.Tabs = []*Tab{newTab("1")}
		workspace.ActiveTab = 0
	}
	if workspace.ActiveTab < 0 || workspace.ActiveTab >= len(workspace.Tabs) {
		workspace.ActiveTab = 0
	}
	return workspace, nil
}

func exportWorkspaceStateNode(node *LayoutNode) *workspaceStateNode {
	if node == nil {
		return nil
	}
	if node.IsLeaf() {
		return &workspaceStateNode{PaneID: node.PaneID}
	}
	return &workspaceStateNode{
		Direction: node.Direction,
		Ratio:     node.Ratio,
		First:     exportWorkspaceStateNode(node.First),
		Second:    exportWorkspaceStateNode(node.Second),
	}
}

func buildWorkspaceStateNode(node *workspaceStateNode, panes map[string]*Pane) (*LayoutNode, error) {
	if node == nil {
		return nil, nil
	}
	if node.PaneID != "" {
		if panes[node.PaneID] == nil {
			return nil, fmt.Errorf("workspace state references missing pane %q", node.PaneID)
		}
		return NewLeaf(node.PaneID), nil
	}
	if node.First == nil || node.Second == nil {
		return nil, fmt.Errorf("workspace state split node requires both children")
	}
	first, err := buildWorkspaceStateNode(node.First, panes)
	if err != nil {
		return nil, err
	}
	second, err := buildWorkspaceStateNode(node.Second, panes)
	if err != nil {
		return nil, err
	}
	return &LayoutNode{
		Direction: node.Direction,
		Ratio:     node.Ratio,
		First:     first,
		Second:    second,
	}, nil
}
