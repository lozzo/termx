package persist

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/lozzow/termx/tuiv2/workbench"
)

var ErrEmptyStateData = errors.New("persist: load called with empty data")

func Save(wb *workbench.Workbench) ([]byte, error) {
	if wb == nil {
		return nil, fmt.Errorf("persist: nil workbench")
	}

	ordered := orderedWorkspaceNames(wb)
	file := WorkspaceStateFileV2{
		Version: 2,
		Data:    make([]WorkspaceEntryV2, 0, len(ordered)),
	}
	for _, name := range ordered {
		ws := wb.WorkspaceByName(name)
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
	currentName := wb.CurrentWorkspaceName()
	if currentName == "" {
		return order
	}

	out := make([]string, 0, len(order))
	out = append(out, currentName)
	for _, name := range order {
		if name != currentName {
			out = append(out, name)
		}
	}
	return out
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
	floatingVisible := tab.FloatingVisible
	entry := TabEntryV2{
		Name:            tab.Name,
		ActivePaneID:    tab.ActivePaneID,
		ZoomedPaneID:    tab.ZoomedPaneID,
		FloatingVisible: &floatingVisible,
		LayoutPreset:    tab.LayoutPreset,
		Layout:          exportLayout(tab.Root),
		Panes:           make([]PaneEntryV2, 0, len(tab.Panes)),
		Floating:        make([]FloatingEntryV2, 0, len(tab.Floating)),
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
			Z:       floating.Z,
			Display: string(floating.Display),
			FitMode: string(floating.FitMode),
			RestoreRect: RectEntryV2{
				X: floating.RestoreRect.X,
				Y: floating.RestoreRect.Y,
				W: floating.RestoreRect.W,
				H: floating.RestoreRect.H,
			},
			AutoFitCols: floating.AutoFitCols,
			AutoFitRows: floating.AutoFitRows,
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
	sort.Slice(extras, func(i, j int) bool {
		return shared.LessNumericStrings(extras[i], extras[j])
	})
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
