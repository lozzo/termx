package viewstate

import "github.com/lozzow/termx/tuiv2/workbench"

type Projection struct {
	WorkspaceName   string
	ActiveTabID     string
	FocusedPaneID   string
	ZoomedPaneByTab map[string]string
	ViewportByPane  map[string]int
}

type CaptureOptions struct {
	PaneViewportOffset         func(paneID string) (int, bool)
	EffectiveTabViewportOffset func(tab *workbench.TabState) int
}

type ApplyOptions struct {
	SetPaneViewportOffset func(paneID string, offset int) bool
}

func Capture(wb *workbench.Workbench, opts CaptureOptions) Projection {
	proj := Projection{
		ZoomedPaneByTab: make(map[string]string),
		ViewportByPane:  make(map[string]int),
	}
	if wb == nil {
		return proj
	}
	if ws := wb.CurrentWorkspace(); ws != nil {
		proj.WorkspaceName = ws.Name
	}
	if tab := wb.CurrentTab(); tab != nil {
		proj.ActiveTabID = tab.ID
		proj.FocusedPaneID = tab.ActivePaneID
	}
	for _, wsName := range wb.ListWorkspaces() {
		ws := wb.WorkspaceByName(wsName)
		if ws == nil {
			continue
		}
		for _, tab := range ws.Tabs {
			if tab == nil {
				continue
			}
			proj.ZoomedPaneByTab[tab.ID] = tab.ZoomedPaneID
			for paneID := range tab.Panes {
				if offset, ok := capturePaneViewport(opts, paneID); ok {
					proj.ViewportByPane[paneID] = offset
				}
			}
			if tab.ActivePaneID != "" {
				if _, ok := proj.ViewportByPane[tab.ActivePaneID]; !ok {
					proj.ViewportByPane[tab.ActivePaneID] = effectiveTabViewportOffset(opts, tab)
				}
			}
		}
	}
	return proj
}

func Apply(wb *workbench.Workbench, proj Projection, opts ApplyOptions) {
	if wb == nil {
		return
	}
	for _, wsName := range wb.ListWorkspaces() {
		ws := wb.WorkspaceByName(wsName)
		if ws == nil {
			continue
		}
		for _, tab := range ws.Tabs {
			if tab == nil {
				continue
			}
			if zoomed, ok := proj.ZoomedPaneByTab[tab.ID]; ok && (zoomed == "" || tab.Panes[zoomed] != nil) {
				tab.ZoomedPaneID = zoomed
			}
			for paneID := range tab.Panes {
				scroll, ok := proj.ViewportByPane[paneID]
				if !ok || opts.SetPaneViewportOffset == nil {
					continue
				}
				_ = opts.SetPaneViewportOffset(paneID, scroll)
			}
		}
	}
	if proj.WorkspaceName != "" {
		_ = wb.SwitchWorkspace(proj.WorkspaceName)
	}
	if proj.ActiveTabID != "" {
		if ws := wb.CurrentWorkspace(); ws != nil {
			for index, tab := range ws.Tabs {
				if tab != nil && tab.ID == proj.ActiveTabID {
					_ = wb.SwitchTab(ws.Name, index)
					break
				}
			}
		}
	}
	if proj.FocusedPaneID != "" {
		if tab := wb.CurrentTab(); tab != nil && tab.Panes[proj.FocusedPaneID] != nil {
			_ = wb.FocusPane(tab.ID, proj.FocusedPaneID)
		}
	}
}

func capturePaneViewport(opts CaptureOptions, paneID string) (int, bool) {
	if paneID == "" || opts.PaneViewportOffset == nil {
		return 0, false
	}
	return opts.PaneViewportOffset(paneID)
}

func effectiveTabViewportOffset(opts CaptureOptions, tab *workbench.TabState) int {
	if opts.EffectiveTabViewportOffset == nil {
		return 0
	}
	return opts.EffectiveTabViewportOffset(tab)
}
