package bootstrap

import (
	"errors"
	"fmt"
	"sort"

	"github.com/lozzow/termx/tuiv2/persist"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/workbench"
)

// ErrEmptyData is returned by Restore when the supplied byte slice is nil or
// empty.
var ErrEmptyData = errors.New("bootstrap: restore called with empty data")

// Restore deserialises a V2 persisted state snapshot and populates wb with
// the recovered workspaces, tabs, and panes.
//
// On any error (empty data, invalid JSON, unsupported schema version) Restore
// returns an error immediately without modifying wb, so the caller can fall
// back to Startup.
//
// rt is accepted for future use and may be nil.
func Restore(data []byte, wb *workbench.Workbench, rt *runtime.Runtime) error {
	file, err := persist.Load(data)
	if err != nil {
		if errors.Is(err, persist.ErrEmptyStateData) {
			return ErrEmptyData
		}
		return err
	}
	return RestoreFile(file, wb, rt)
}

func RestoreFile(file *persist.WorkspaceStateFileV2, wb *workbench.Workbench, rt *runtime.Runtime) error {
	if file == nil {
		return ErrEmptyData
	}
	if file.Version != 2 {
		return fmt.Errorf("bootstrap: unsupported schema version %d (expected 2)", file.Version)
	}
	for _, wsEntry := range file.Data {
		ws := buildWorkspace(wsEntry)
		wb.AddWorkspace(ws.Name, ws)
	}
	return nil
}

// buildWorkspace converts a persisted WorkspaceEntryV2 into a live
// WorkspaceState.
func buildWorkspace(entry persist.WorkspaceEntryV2) *workbench.WorkspaceState {
	ws := &workbench.WorkspaceState{
		Name:      entry.Name,
		ActiveTab: entry.ActiveTab,
		Tabs:      make([]*workbench.TabState, 0, len(entry.Tabs)),
	}
	for _, tabEntry := range entry.Tabs {
		ws.Tabs = append(ws.Tabs, buildTab(tabEntry))
	}
	return ws
}

// buildTab converts a persisted TabEntryV2 into a live TabState.
func buildTab(entry persist.TabEntryV2) *workbench.TabState {
	tab := &workbench.TabState{
		// Tabs do not have a persisted ID in V2; derive a stable one from the
		// name so that callers always get a non-empty ID.
		ID:           "tab-" + entry.Name,
		Name:         entry.Name,
		Panes:        make(map[string]*workbench.PaneState, len(entry.Panes)),
		ActivePaneID: entry.ActivePaneID,
		ZoomedPaneID: entry.ZoomedPaneID,
		LayoutPreset: entry.LayoutPreset,
	}

	for _, paneEntry := range entry.Panes {
		pane := &workbench.PaneState{
			ID:         paneEntry.ID,
			Title:      paneEntry.Title,
			TerminalID: paneEntry.TerminalID,
		}
		tab.Panes[pane.ID] = pane
	}

	tab.Floating = buildFloatingEntries(entry.Floating, tab.Panes)
	tab.FloatingVisible = len(tab.Floating) > 0

	// Restore the layout tree if one was persisted; otherwise build a
	// simple linear chain from the pane list.
	if entry.Layout != nil {
		tab.Root = buildLayoutNode(entry.Layout)
	} else {
		tab.Root = buildDefaultLayout(tiledPaneEntries(entry.Panes, entry.Floating))
	}

	return tab
}

// buildLayoutNode recursively converts a LayoutNodeEntry into a LayoutNode.
func buildLayoutNode(entry *persist.LayoutNodeEntry) *workbench.LayoutNode {
	if entry == nil {
		return nil
	}
	node := &workbench.LayoutNode{
		PaneID: entry.PaneID,
		Ratio:  entry.Ratio,
		First:  buildLayoutNode(entry.First),
		Second: buildLayoutNode(entry.Second),
	}
	switch entry.Direction {
	case string(workbench.SplitHorizontal):
		node.Direction = workbench.SplitHorizontal
	case string(workbench.SplitVertical):
		node.Direction = workbench.SplitVertical
	}
	return node
}

// buildDefaultLayout constructs a leaf node for a single pane, or a balanced
// binary tree for multiple panes using vertical splits.
func buildDefaultLayout(panes []persist.PaneEntryV2) *workbench.LayoutNode {
	if len(panes) == 0 {
		return nil
	}
	if len(panes) == 1 {
		return workbench.NewLeaf(panes[0].ID)
	}
	// Binary split: first pane vs the rest.
	mid := len(panes) / 2
	return &workbench.LayoutNode{
		Direction: workbench.SplitVertical,
		Ratio:     0.5,
		First:     buildDefaultLayout(panes[:mid]),
		Second:    buildDefaultLayout(panes[mid:]),
	}
}

func buildFloatingEntries(entries []persist.FloatingEntryV2, panes map[string]*workbench.PaneState) []*workbench.FloatingState {
	if len(entries) == 0 {
		return nil
	}

	floating := make([]*workbench.FloatingState, 0, len(entries))
	for _, entry := range entries {
		if entry.PaneID == "" || panes[entry.PaneID] == nil {
			continue
		}
		floating = append(floating, &workbench.FloatingState{
			PaneID: entry.PaneID,
			Rect: workbench.Rect{
				X: entry.Rect.X,
				Y: entry.Rect.Y,
				W: entry.Rect.W,
				H: entry.Rect.H,
			},
			Z: entry.Z,
		})
	}
	sort.SliceStable(floating, func(i, j int) bool {
		return floating[i].Z < floating[j].Z
	})
	return floating
}

func tiledPaneEntries(panes []persist.PaneEntryV2, floating []persist.FloatingEntryV2) []persist.PaneEntryV2 {
	if len(panes) == 0 {
		return nil
	}
	if len(floating) == 0 {
		return panes
	}

	floatingIDs := make(map[string]struct{}, len(floating))
	for _, entry := range floating {
		if entry.PaneID != "" {
			floatingIDs[entry.PaneID] = struct{}{}
		}
	}

	tiled := make([]persist.PaneEntryV2, 0, len(panes))
	for _, pane := range panes {
		if _, isFloating := floatingIDs[pane.ID]; isFloating {
			continue
		}
		tiled = append(tiled, pane)
	}
	return tiled
}
