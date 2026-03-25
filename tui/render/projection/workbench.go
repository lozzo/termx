package projection

import (
	"sort"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tui/domain/types"
)

// RuntimeTerminalStore 只暴露投影层当前需要的最小读接口，
// 让 render 能接 runtime terminal store，但不反向依赖 tui 包。
type RuntimeTerminalStore interface {
	Snapshot(terminalID types.TerminalID) (*protocol.Snapshot, bool)
}

type PaneProjection struct {
	PaneID    types.PaneID
	HasScreen bool
}

type OverlayProjection struct{}

type WorkbenchView struct {
	ActivePaneID types.PaneID
	Tiled        []PaneProjection
	Floating     []PaneProjection
	Overlay      OverlayProjection
}

// ProjectWorkbench 当前只投影当前 workspace/tab 的活跃 pane 和 floating 顺序，
// rect、终端内容、overlay 细节留给后续任务补齐。
func ProjectWorkbench(state types.AppState, screens RuntimeTerminalStore, width, height int) WorkbenchView {
	_ = width
	_ = height

	workspace, ok := state.Domain.Workspaces[state.Domain.ActiveWorkspaceID]
	if !ok {
		return WorkbenchView{}
	}
	tab, ok := workspace.Tabs[workspace.ActiveTabID]
	if !ok {
		return WorkbenchView{}
	}

	view := WorkbenchView{
		ActivePaneID: tab.ActivePaneID,
		Tiled:        make([]PaneProjection, 0, len(tab.Panes)),
		Floating:     make([]PaneProjection, 0, len(tab.FloatingOrder)),
	}
	for _, paneID := range orderedTiledPaneIDs(tab) {
		pane, ok := tab.Panes[paneID]
		if !ok {
			continue
		}
		view.Tiled = append(view.Tiled, projectPane(pane, screens))
	}
	for _, paneID := range tab.FloatingOrder {
		pane, ok := tab.Panes[paneID]
		if !ok || pane.Kind != types.PaneKindFloating {
			continue
		}
		view.Floating = append(view.Floating, projectPane(pane, screens))
	}
	return view
}

func projectPane(pane types.PaneState, screens RuntimeTerminalStore) PaneProjection {
	return PaneProjection{
		PaneID:    pane.ID,
		HasScreen: paneHasSnapshot(pane, screens),
	}
}

func paneHasSnapshot(pane types.PaneState, screens RuntimeTerminalStore) bool {
	if screens == nil || pane.TerminalID == "" {
		return false
	}
	snapshot, ok := screens.Snapshot(pane.TerminalID)
	return ok && snapshot != nil
}

func orderedTiledPaneIDs(tab types.TabState) []types.PaneID {
	ordered := make([]types.PaneID, 0, len(tab.Panes))
	seen := make(map[types.PaneID]struct{}, len(tab.Panes))
	collectSplitPaneIDs(tab.RootSplit, tab, seen, &ordered)

	var remaining []types.PaneID
	for paneID, pane := range tab.Panes {
		if pane.Kind != types.PaneKindTiled {
			continue
		}
		if _, ok := seen[paneID]; ok {
			continue
		}
		remaining = append(remaining, paneID)
	}
	sort.Slice(remaining, func(i, j int) bool {
		return remaining[i] < remaining[j]
	})
	return append(ordered, remaining...)
}

func collectSplitPaneIDs(node *types.SplitNode, tab types.TabState, seen map[types.PaneID]struct{}, ordered *[]types.PaneID) {
	if node == nil {
		return
	}
	if node.First == nil && node.Second == nil {
		pane, ok := tab.Panes[node.PaneID]
		if !ok || pane.Kind != types.PaneKindTiled {
			return
		}
		if _, ok := seen[node.PaneID]; ok {
			return
		}
		seen[node.PaneID] = struct{}{}
		*ordered = append(*ordered, node.PaneID)
		return
	}
	collectSplitPaneIDs(node.First, tab, seen, ordered)
	collectSplitPaneIDs(node.Second, tab, seen, ordered)
}
