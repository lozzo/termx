package sessionstate

import (
	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/lozzow/termx/tuiv2/workbench"
	"github.com/lozzow/termx/workbenchdoc"
)

func ExportWorkbench(wb *workbench.Workbench) *workbenchdoc.Doc {
	doc := workbenchdoc.New()
	if wb == nil {
		return doc
	}
	doc.CurrentWorkspace = wb.CurrentWorkspaceName()
	doc.WorkspaceOrder = append([]string(nil), wb.ListWorkspaces()...)
	for _, name := range doc.WorkspaceOrder {
		ws := wb.WorkspaceByName(name)
		if ws == nil {
			continue
		}
		doc.Workspaces[name] = exportWorkspace(ws)
	}
	return doc
}

func ImportDoc(doc *workbenchdoc.Doc) *workbench.Workbench {
	wb := workbench.NewWorkbench()
	if doc == nil {
		return wb
	}
	for _, name := range doc.WorkspaceOrder {
		ws := doc.Workspaces[name]
		if ws == nil {
			continue
		}
		importWorkspaceInto(wb, name, ws)
	}
	if wb.CurrentWorkspace() == nil {
		for name, ws := range doc.Workspaces {
			if ws == nil {
				continue
			}
			importWorkspaceInto(wb, name, ws)
		}
	}
	if doc.CurrentWorkspace != "" {
		_ = wb.SwitchWorkspace(doc.CurrentWorkspace)
	}
	return wb
}

func PaneTerminalBindings(doc *workbenchdoc.Doc) map[string]string {
	out := make(map[string]string)
	if doc == nil {
		return out
	}
	for _, ws := range doc.Workspaces {
		if ws == nil {
			continue
		}
		for _, tab := range ws.Tabs {
			if tab == nil {
				continue
			}
			for paneID, pane := range tab.Panes {
				if pane == nil || paneID == "" || pane.TerminalID == "" {
					continue
				}
				out[paneID] = pane.TerminalID
			}
		}
	}
	return out
}

func exportWorkspace(ws *workbench.WorkspaceState) *workbenchdoc.Workspace {
	if ws == nil {
		return nil
	}
	out := &workbenchdoc.Workspace{
		Name:      ws.Name,
		ActiveTab: ws.ActiveTab,
		Tabs:      make([]*workbenchdoc.Tab, 0, len(ws.Tabs)),
	}
	for _, tab := range ws.Tabs {
		if tab == nil {
			continue
		}
		out.Tabs = append(out.Tabs, exportTab(tab))
	}
	return out
}

func exportTab(tab *workbench.TabState) *workbenchdoc.Tab {
	if tab == nil {
		return nil
	}
	out := &workbenchdoc.Tab{
		ID:              tab.ID,
		Name:            tab.Name,
		Root:            exportLayout(tab.Root),
		Panes:           make(map[string]*workbenchdoc.Pane, len(tab.Panes)),
		Floating:        make([]*workbenchdoc.FloatingPane, 0, len(tab.Floating)),
		FloatingVisible: tab.FloatingVisible,
		ActivePaneID:    tab.ActivePaneID,
		ZoomedPaneID:    tab.ZoomedPaneID,
		ScrollOffset:    tab.ScrollOffset,
		LayoutPreset:    tab.LayoutPreset,
	}
	for paneID, pane := range tab.Panes {
		if pane == nil {
			continue
		}
		out.Panes[paneID] = &workbenchdoc.Pane{
			ID:         pane.ID,
			Title:      pane.Title,
			TerminalID: pane.TerminalID,
		}
	}
	for _, floating := range tab.Floating {
		if floating == nil {
			continue
		}
		out.Floating = append(out.Floating, &workbenchdoc.FloatingPane{
			PaneID: floating.PaneID,
			Rect: workbenchdoc.Rect{
				X: floating.Rect.X,
				Y: floating.Rect.Y,
				W: floating.Rect.W,
				H: floating.Rect.H,
			},
			Z:       floating.Z,
			Display: string(floating.Display),
			FitMode: string(floating.FitMode),
			RestoreRect: workbenchdoc.Rect{
				X: floating.RestoreRect.X,
				Y: floating.RestoreRect.Y,
				W: floating.RestoreRect.W,
				H: floating.RestoreRect.H,
			},
			AutoFitCols: floating.AutoFitCols,
			AutoFitRows: floating.AutoFitRows,
		})
	}
	return out
}

func exportLayout(node *workbench.LayoutNode) *workbenchdoc.LayoutNode {
	if node == nil {
		return nil
	}
	return &workbenchdoc.LayoutNode{
		PaneID:    node.PaneID,
		Direction: workbenchdoc.SplitDirection(node.Direction),
		Ratio:     node.Ratio,
		First:     exportLayout(node.First),
		Second:    exportLayout(node.Second),
	}
}

func importWorkspaceInto(wb *workbench.Workbench, name string, ws *workbenchdoc.Workspace) {
	if wb == nil || ws == nil {
		return
	}
	shared.ObserveWorkspaceID(name)
	wb.AddWorkspace(name, importWorkspace(ws))
}

func importWorkspace(ws *workbenchdoc.Workspace) *workbench.WorkspaceState {
	out := &workbench.WorkspaceState{
		Name:      ws.Name,
		ActiveTab: ws.ActiveTab,
		Tabs:      make([]*workbench.TabState, 0, len(ws.Tabs)),
	}
	for _, tab := range ws.Tabs {
		if tab == nil {
			continue
		}
		out.Tabs = append(out.Tabs, importTab(tab))
	}
	return out
}

func importTab(tab *workbenchdoc.Tab) *workbench.TabState {
	shared.ObserveTabID(tab.ID)
	out := &workbench.TabState{
		ID:              tab.ID,
		Name:            tab.Name,
		Root:            importLayout(tab.Root),
		Panes:           make(map[string]*workbench.PaneState, len(tab.Panes)),
		Floating:        make([]*workbench.FloatingState, 0, len(tab.Floating)),
		FloatingVisible: tab.FloatingVisible,
		ActivePaneID:    tab.ActivePaneID,
		ZoomedPaneID:    tab.ZoomedPaneID,
		ScrollOffset:    tab.ScrollOffset,
		LayoutPreset:    tab.LayoutPreset,
	}
	for paneID, pane := range tab.Panes {
		if pane == nil {
			continue
		}
		shared.ObservePaneID(paneID)
		out.Panes[paneID] = &workbench.PaneState{
			ID:         pane.ID,
			Title:      pane.Title,
			TerminalID: pane.TerminalID,
		}
	}
	for _, floating := range tab.Floating {
		if floating == nil {
			continue
		}
		out.Floating = append(out.Floating, &workbench.FloatingState{
			PaneID: floating.PaneID,
			Rect: workbench.Rect{
				X: floating.Rect.X,
				Y: floating.Rect.Y,
				W: floating.Rect.W,
				H: floating.Rect.H,
			},
			Z:       floating.Z,
			Display: workbench.FloatingDisplayState(floating.Display),
			FitMode: workbench.FloatingFitMode(floating.FitMode),
			RestoreRect: workbench.Rect{
				X: floating.RestoreRect.X,
				Y: floating.RestoreRect.Y,
				W: floating.RestoreRect.W,
				H: floating.RestoreRect.H,
			},
			AutoFitCols: floating.AutoFitCols,
			AutoFitRows: floating.AutoFitRows,
		})
	}
	return out
}

func importLayout(node *workbenchdoc.LayoutNode) *workbench.LayoutNode {
	if node == nil {
		return nil
	}
	return &workbench.LayoutNode{
		PaneID:    node.PaneID,
		Direction: workbench.SplitDirection(node.Direction),
		Ratio:     node.Ratio,
		First:     importLayout(node.First),
		Second:    importLayout(node.Second),
	}
}
