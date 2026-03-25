package projection

import "github.com/lozzow/termx/tui/domain/types"

// RuntimeTerminalStore 先保留为渲染层自己的占位接口，
// 避免新 render 包在骨架阶段反向依赖上层 tui 包而形成循环引用。
type RuntimeTerminalStore interface{}

type PaneProjection struct {
	PaneID types.PaneID
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
	_ = screens
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
	for _, pane := range tab.Panes {
		if pane.Kind != types.PaneKindTiled {
			continue
		}
		view.Tiled = append(view.Tiled, PaneProjection{PaneID: pane.ID})
	}
	for _, paneID := range tab.FloatingOrder {
		pane, ok := tab.Panes[paneID]
		if !ok || pane.Kind != types.PaneKindFloating {
			continue
		}
		view.Floating = append(view.Floating, PaneProjection{PaneID: pane.ID})
	}
	return view
}
