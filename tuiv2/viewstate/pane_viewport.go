package viewstate

import "github.com/lozzow/termx/tuiv2/workbench"

type PaneViewport struct {
	Workbench *workbench.Workbench

	BindingOffset       func(paneID string) (int, bool)
	SetBindingOffset    func(paneID string, offset int) bool
	AdjustBindingOffset func(paneID string, delta int) (int, bool)
}

func (v PaneViewport) BindingOffsetValue(paneID string) (int, bool) {
	if paneID == "" || v.BindingOffset == nil {
		return 0, false
	}
	offset, ok := v.BindingOffset(paneID)
	if offset < 0 {
		return 0, ok
	}
	return offset, ok
}

func (v PaneViewport) LegacyOffset(paneID string) int {
	if v.Workbench == nil || paneID == "" {
		return 0
	}
	for _, wsName := range v.Workbench.ListWorkspaces() {
		ws := v.Workbench.WorkspaceByName(wsName)
		if ws == nil {
			continue
		}
		for _, tab := range ws.Tabs {
			if tab == nil || tab.ActivePaneID != paneID || tab.Panes[paneID] == nil {
				continue
			}
			if tab.ScrollOffset < 0 {
				return 0
			}
			return tab.ScrollOffset
		}
	}
	return 0
}

func (v PaneViewport) Offset(paneID string) int {
	if offset, ok := v.BindingOffsetValue(paneID); ok {
		return offset
	}
	return v.LegacyOffset(paneID)
}

func (v PaneViewport) EffectiveTabOffset(tab *workbench.TabState) int {
	if tab == nil {
		return 0
	}
	if tab.ActivePaneID != "" {
		return v.Offset(tab.ActivePaneID)
	}
	if tab.ScrollOffset < 0 {
		return 0
	}
	return tab.ScrollOffset
}

func (v PaneViewport) SetOffset(paneID string, offset int) bool {
	if paneID == "" {
		return false
	}
	if v.SetBindingOffset != nil {
		return v.SetBindingOffset(paneID, offset)
	}
	return v.setLegacyOffset(paneID, offset)
}

func (v PaneViewport) AdjustOffset(paneID string, delta int) (int, bool) {
	if paneID == "" {
		return 0, false
	}
	if v.AdjustBindingOffset != nil {
		if _, ok := v.BindingOffsetValue(paneID); !ok {
			if legacy := v.LegacyOffset(paneID); legacy > 0 && v.SetBindingOffset != nil {
				_ = v.SetBindingOffset(paneID, legacy)
			}
		}
		return v.AdjustBindingOffset(paneID, delta)
	}
	return v.adjustLegacyOffset(paneID, delta)
}

func (v PaneViewport) Reset(paneID string) bool {
	if paneID == "" {
		return false
	}
	changed := v.SetOffset(paneID, 0)
	if v.Workbench != nil {
		if tabID, err := v.Workbench.ResolvePaneTab("", paneID); err == nil && v.Workbench.SetTabScrollOffset(tabID, 0) {
			changed = true
		}
	}
	return changed
}

func (v PaneViewport) setLegacyOffset(paneID string, offset int) bool {
	if v.Workbench == nil || paneID == "" {
		return false
	}
	for _, wsName := range v.Workbench.ListWorkspaces() {
		ws := v.Workbench.WorkspaceByName(wsName)
		if ws == nil {
			continue
		}
		for _, tab := range ws.Tabs {
			if tab == nil || tab.ActivePaneID != paneID || tab.Panes[paneID] == nil {
				continue
			}
			return v.Workbench.SetTabScrollOffset(tab.ID, offset)
		}
	}
	return false
}

func (v PaneViewport) adjustLegacyOffset(paneID string, delta int) (int, bool) {
	if v.Workbench == nil || paneID == "" {
		return 0, false
	}
	for _, wsName := range v.Workbench.ListWorkspaces() {
		ws := v.Workbench.WorkspaceByName(wsName)
		if ws == nil {
			continue
		}
		for _, tab := range ws.Tabs {
			if tab == nil || tab.ActivePaneID != paneID || tab.Panes[paneID] == nil {
				continue
			}
			return v.Workbench.AdjustTabScrollOffset(tab.ID, delta)
		}
	}
	return 0, false
}
