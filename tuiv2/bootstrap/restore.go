package bootstrap

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sort"

	"github.com/lozzow/termx/tuiv2/persist"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/shared"
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
	shared.ObserveWorkspaceID(entry.Name)
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
	tabID := shared.NextTabID()
	tab := &workbench.TabState{
		ID:           tabID,
		Name:         entry.Name,
		Panes:        make(map[string]*workbench.PaneState, len(entry.Panes)),
		LayoutPreset: entry.LayoutPreset,
	}

	paneIDMap := make(map[string]string, len(entry.Panes))
	for _, paneEntry := range entry.Panes {
		paneID := normalizedRestoredPaneID(paneEntry.ID, paneIDMap)
		paneIDMap[paneEntry.ID] = paneID
		pane := &workbench.PaneState{
			ID:         paneID,
			Title:      paneEntry.Title,
			TerminalID: paneEntry.TerminalID,
		}
		tab.Panes[pane.ID] = pane
	}

	tab.ActivePaneID = remapPaneID(entry.ActivePaneID, paneIDMap)
	tab.ZoomedPaneID = remapPaneID(entry.ZoomedPaneID, paneIDMap)
	tab.Floating = buildFloatingEntries(entry.Floating, tab.Panes, paneIDMap)
	tab.FloatingVisible = len(tab.Floating) > 0

	// Restore the layout tree if one was persisted; otherwise build a
	// simple linear chain from the pane list.
	if entry.Layout != nil {
		tab.Root = buildLayoutNode(entry.Layout, paneIDMap)
	} else {
		tab.Root = buildDefaultLayout(tiledPaneEntries(entry.Panes, entry.Floating, paneIDMap))
	}

	return tab
}

// buildLayoutNode recursively converts a LayoutNodeEntry into a LayoutNode.
func buildLayoutNode(entry *persist.LayoutNodeEntry, paneIDMap map[string]string) *workbench.LayoutNode {
	if entry == nil {
		return nil
	}
	node := &workbench.LayoutNode{
		PaneID: remapPaneID(entry.PaneID, paneIDMap),
		Ratio:  entry.Ratio,
		First:  buildLayoutNode(entry.First, paneIDMap),
		Second: buildLayoutNode(entry.Second, paneIDMap),
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

func buildFloatingEntries(entries []persist.FloatingEntryV2, panes map[string]*workbench.PaneState, paneIDMap map[string]string) []*workbench.FloatingState {
	if len(entries) == 0 {
		return nil
	}

	floating := make([]*workbench.FloatingState, 0, len(entries))
	for _, entry := range entries {
		paneID := remapPaneID(entry.PaneID, paneIDMap)
		if paneID == "" || panes[paneID] == nil {
			continue
		}
		floating = append(floating, &workbench.FloatingState{
			PaneID: paneID,
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

func tiledPaneEntries(panes []persist.PaneEntryV2, floating []persist.FloatingEntryV2, paneIDMap map[string]string) []persist.PaneEntryV2 {
	if len(panes) == 0 {
		return nil
	}
	if len(floating) == 0 {
		out := make([]persist.PaneEntryV2, 0, len(panes))
		for _, pane := range panes {
			pane.ID = remapPaneID(pane.ID, paneIDMap)
			out = append(out, pane)
		}
		return out
	}

	floatingIDs := make(map[string]struct{}, len(floating))
	for _, entry := range floating {
		if paneID := remapPaneID(entry.PaneID, paneIDMap); paneID != "" {
			floatingIDs[paneID] = struct{}{}
		}
	}

	tiled := make([]persist.PaneEntryV2, 0, len(panes))
	for _, pane := range panes {
		pane.ID = remapPaneID(pane.ID, paneIDMap)
		if _, isFloating := floatingIDs[pane.ID]; isFloating {
			continue
		}
		tiled = append(tiled, pane)
	}
	return tiled
}

func normalizedRestoredPaneID(raw string, used map[string]string) string {
	if restored, ok := normalizedObservedID(raw); ok {
		if !paneIDAllocated(used, restored) {
			shared.ObservePaneID(restored)
			return restored
		}
	}
	for {
		next := shared.NextPaneID()
		if paneIDAllocated(used, next) {
			continue
		}
		return next
	}
}

func remapPaneID(raw string, paneIDMap map[string]string) string {
	if raw == "" {
		return ""
	}
	if paneIDMap == nil {
		return raw
	}
	if mapped := paneIDMap[raw]; mapped != "" {
		return mapped
	}
	return raw
}

func normalizedObservedID(raw string) (string, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", false
	}
	if n, err := strconv.ParseUint(value, 10, 64); err == nil && n > 0 {
		return strconv.FormatUint(n, 10), true
	}
	if idx := strings.LastIndexByte(value, '-'); idx >= 0 && idx < len(value)-1 {
		suffix := value[idx+1:]
		if n, err := strconv.ParseUint(suffix, 10, 64); err == nil && n > 0 {
			return strconv.FormatUint(n, 10), true
		}
	}
	return "", false
}

func paneIDAllocated(used map[string]string, candidate string) bool {
	if candidate == "" || used == nil {
		return false
	}
	for _, value := range used {
		if value == candidate {
			return true
		}
	}
	return false
}
