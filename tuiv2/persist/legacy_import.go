package persist

import (
	"encoding/json"
	"fmt"
)

type legacyWorkspaceStateFile struct {
	Version         int                         `json:"version"`
	ActiveWorkspace int                         `json:"active_workspace,omitempty"`
	Workspaces      []legacyWorkspaceStateEntry `json:"workspaces,omitempty"`
	Workspace       legacyWorkspaceStateEntry   `json:"workspace,omitempty"`
}

type legacyWorkspaceStateEntry struct {
	Name      string                    `json:"name"`
	ActiveTab int                       `json:"active_tab"`
	Tabs      []legacyWorkspaceStateTab `json:"tabs"`
}

type legacyWorkspaceStateTab struct {
	Name         string                     `json:"name"`
	ActivePaneID string                     `json:"active_pane_id,omitempty"`
	ZoomedPaneID string                     `json:"zoomed_pane_id,omitempty"`
	LayoutPreset int                        `json:"layout_preset,omitempty"`
	Root         *legacyWorkspaceStateNode  `json:"root,omitempty"`
	Panes        []legacyWorkspaceStatePane `json:"panes"`
}

type legacyWorkspaceStatePane struct {
	ID         string            `json:"id"`
	Title      string            `json:"title,omitempty"`
	TerminalID string            `json:"terminal_id,omitempty"`
	Name       string            `json:"name,omitempty"`
	Command    []string          `json:"command,omitempty"`
	Tags       map[string]string `json:"tags,omitempty"`
}

type legacyWorkspaceStateNode struct {
	PaneID    string                    `json:"pane_id,omitempty"`
	Direction string                    `json:"direction,omitempty"`
	Ratio     float64                   `json:"ratio,omitempty"`
	First     *legacyWorkspaceStateNode `json:"first,omitempty"`
	Second    *legacyWorkspaceStateNode `json:"second,omitempty"`
}

func ImportV1(data []byte) (*WorkspaceStateFileV2, error) {
	if len(data) == 0 {
		return nil, ErrEmptyStateData
	}

	var legacy legacyWorkspaceStateFile
	if err := json.Unmarshal(data, &legacy); err != nil {
		return nil, fmt.Errorf("persist: failed to parse legacy state file: %w", err)
	}
	if legacy.Version == 0 {
		legacy.Version = 1
	}
	if legacy.Version != 1 {
		return nil, fmt.Errorf("persist: unsupported legacy schema version %d", legacy.Version)
	}
	if len(legacy.Workspaces) == 0 && (legacy.Workspace.Name != "" || len(legacy.Workspace.Tabs) > 0) {
		legacy.Workspaces = []legacyWorkspaceStateEntry{legacy.Workspace}
	}
	if len(legacy.Workspaces) == 0 {
		return nil, fmt.Errorf("persist: legacy state has no workspaces")
	}
	if legacy.ActiveWorkspace < 0 || legacy.ActiveWorkspace >= len(legacy.Workspaces) {
		legacy.ActiveWorkspace = 0
	}

	ordered := make([]legacyWorkspaceStateEntry, 0, len(legacy.Workspaces))
	ordered = append(ordered, legacy.Workspaces[legacy.ActiveWorkspace])
	for index, workspace := range legacy.Workspaces {
		if index != legacy.ActiveWorkspace {
			ordered = append(ordered, workspace)
		}
	}

	file := &WorkspaceStateFileV2{
		Version: 2,
		Data:    make([]WorkspaceEntryV2, 0, len(ordered)),
	}
	for _, workspace := range ordered {
		entry := WorkspaceEntryV2{
			Name:      workspace.Name,
			ActiveTab: workspace.ActiveTab,
			Tabs:      make([]TabEntryV2, 0, len(workspace.Tabs)),
		}
		for _, tab := range workspace.Tabs {
			tabEntry := TabEntryV2{
				Name:         tab.Name,
				ActivePaneID: tab.ActivePaneID,
				ZoomedPaneID: tab.ZoomedPaneID,
				LayoutPreset: tab.LayoutPreset,
				Layout:       importLegacyLayout(tab.Root),
				Panes:        make([]PaneEntryV2, 0, len(tab.Panes)),
			}
			for _, pane := range tab.Panes {
				tabEntry.Panes = append(tabEntry.Panes, PaneEntryV2{
					ID:         pane.ID,
					Title:      pane.Title,
					TerminalID: pane.TerminalID,
				})
			}
			entry.Tabs = append(entry.Tabs, tabEntry)
		}
		file.Data = append(file.Data, entry)
	}

	return file, nil
}

func importLegacyLayout(node *legacyWorkspaceStateNode) *LayoutNodeEntry {
	if node == nil {
		return nil
	}
	return &LayoutNodeEntry{
		PaneID:    node.PaneID,
		Direction: node.Direction,
		Ratio:     node.Ratio,
		First:     importLegacyLayout(node.First),
		Second:    importLegacyLayout(node.Second),
	}
}
