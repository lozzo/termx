package persist

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"sort"

	"github.com/lozzow/termx/tuiv2/workbench"
)

var ErrEmptyStateData = errors.New("persist: load called with empty data")

func Save(wb *workbench.Workbench) ([]byte, error) {
	if wb == nil {
		return nil, fmt.Errorf("persist: nil workbench")
	}

	file := WorkspaceStateFileV2{
		Version: 2,
		Data:    make([]WorkspaceEntryV2, 0, len(wb.ListWorkspaces())),
	}
	for _, name := range orderedWorkspaceNames(wb) {
		ws := workspaceByName(wb, name)
		if ws == nil {
			continue
		}
		file.Data = append(file.Data, exportWorkspace(ws))
	}

	return json.MarshalIndent(file, "", "  ")
}

func Load(data []byte) (*WorkspaceStateFileV2, error) {
	if len(data) == 0 {
		return nil, ErrEmptyStateData
	}

	var header struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return nil, fmt.Errorf("persist: failed to parse state file: %w", err)
	}

	switch header.Version {
	case 0, 1:
		return ImportV1(data)
	case 2:
		var file WorkspaceStateFileV2
		if err := json.Unmarshal(data, &file); err != nil {
			return nil, fmt.Errorf("persist: failed to parse state file: %w", err)
		}
		return &file, nil
	default:
		return nil, fmt.Errorf("persist: unsupported schema version %d", header.Version)
	}
}

func orderedWorkspaceNames(wb *workbench.Workbench) []string {
	if wb == nil {
		return nil
	}
	order := wb.ListWorkspaces()
	current := wb.CurrentWorkspace()
	if current == nil || current.Name == "" {
		return order
	}

	out := make([]string, 0, len(order))
	out = append(out, current.Name)
	for _, name := range order {
		if name != current.Name {
			out = append(out, name)
		}
	}
	return out
}

func workspaceByName(wb *workbench.Workbench, name string) *workbench.WorkspaceState {
	if wb == nil || name == "" {
		return nil
	}
	current := wb.CurrentWorkspace()
	currentName := ""
	if current != nil {
		currentName = current.Name
	}
	if !wb.SwitchWorkspace(name) {
		return nil
	}
	ws := wb.CurrentWorkspace()
	if currentName != "" {
		wb.SwitchWorkspace(currentName)
	}
	return ws
}

func exportWorkspace(ws *workbench.WorkspaceState) WorkspaceEntryV2 {
	entry := WorkspaceEntryV2{
		Name:      ws.Name,
		ActiveTab: ws.ActiveTab,
		Tabs:      make([]TabEntryV2, 0, len(ws.Tabs)),
	}
	for _, tab := range ws.Tabs {
		if tab == nil {
			continue
		}
		entry.Tabs = append(entry.Tabs, exportTab(tab))
	}
	return entry
}

func exportTab(tab *workbench.TabState) TabEntryV2 {
	entry := TabEntryV2{
		Name:         tab.Name,
		ActivePaneID: tab.ActivePaneID,
		ZoomedPaneID: tab.ZoomedPaneID,
		LayoutPreset: tab.LayoutPreset,
		Layout:       exportLayout(tab.Root),
		Panes:        make([]PaneEntryV2, 0, len(tab.Panes)),
		Floating:     make([]FloatingEntryV2, 0, len(tab.Floating)),
	}
	for _, paneID := range orderedPaneIDs(tab) {
		pane := tab.Panes[paneID]
		if pane == nil {
			continue
		}
		entry.Panes = append(entry.Panes, PaneEntryV2{
			ID:         pane.ID,
			Title:      pane.Title,
			TerminalID: pane.TerminalID,
		})
	}
	for _, floating := range tab.Floating {
		if floating == nil || floating.PaneID == "" {
			continue
		}
		if tab.Panes[floating.PaneID] == nil {
			continue
		}
		entry.Floating = append(entry.Floating, FloatingEntryV2{
			PaneID: floating.PaneID,
			Rect: RectEntryV2{
				X: floating.Rect.X,
				Y: floating.Rect.Y,
				W: floating.Rect.W,
				H: floating.Rect.H,
			},
			Z: floating.Z,
		})
	}
	return entry
}

func orderedPaneIDs(tab *workbench.TabState) []string {
	if tab == nil {
		return nil
	}
	seen := make(map[string]struct{}, len(tab.Panes))
	out := make([]string, 0, len(tab.Panes))

	// First, collect panes from the layout tree (tiled panes)
	if tab.Root != nil {
		for _, paneID := range tab.Root.LeafIDs() {
			if _, ok := tab.Panes[paneID]; !ok {
				continue
			}
			if _, exists := seen[paneID]; exists {
				continue
			}
			seen[paneID] = struct{}{}
			out = append(out, paneID)
		}
	}

	// Then, collect floating panes
	for _, floating := range tab.Floating {
		if floating == nil || floating.PaneID == "" {
			continue
		}
		paneID := floating.PaneID
		if _, ok := tab.Panes[paneID]; !ok {
			continue
		}
		if _, exists := seen[paneID]; exists {
			continue
		}
		seen[paneID] = struct{}{}
		out = append(out, paneID)
	}

	extras := make([]string, 0, len(tab.Panes)-len(out))
	for paneID := range tab.Panes {
		if _, exists := seen[paneID]; exists {
			continue
		}
		extras = append(extras, paneID)
	}
	sort.Strings(extras)
	return append(out, extras...)
}

func exportLayout(node *workbench.LayoutNode) *LayoutNodeEntry {
	if node == nil {
		return nil
	}
	entry := &LayoutNodeEntry{
		PaneID: node.PaneID,
		Ratio:  node.Ratio,
		First:  exportLayout(node.First),
		Second: exportLayout(node.Second),
	}
	if node.Direction != "" {
		entry.Direction = string(node.Direction)
	}
	return entry
}

func cloneStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	return append([]string(nil), values...)
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(values))
	maps.Copy(cloned, values)
	return cloned
}
