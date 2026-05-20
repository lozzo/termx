package app

import (
	"github.com/lozzow/termx/tuiv2/viewstate"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func (m *Model) paneViewportBindingOffset(paneID string) (int, bool) {
	return m.paneViewport().BindingOffsetValue(paneID)
}

func (m *Model) paneViewport() viewstate.PaneViewport {
	if m == nil {
		return viewstate.PaneViewport{}
	}
	viewport := viewstate.PaneViewport{Workbench: m.workbench}
	if m.runtime == nil {
		return viewport
	}
	viewport.BindingOffset = func(paneID string) (int, bool) {
		if paneID == "" {
			return 0, false
		}
		binding := m.runtime.Binding(paneID)
		if binding == nil {
			return 0, false
		}
		return binding.Viewport.Offset, true
	}
	viewport.SetBindingOffset = func(paneID string, offset int) bool {
		return m.runtime.SetPaneViewportOffset(paneID, offset)
	}
	viewport.AdjustBindingOffset = func(paneID string, delta int) (int, bool) {
		return m.runtime.AdjustPaneViewportOffset(paneID, delta)
	}
	return viewport
}

func (m *Model) legacyPaneViewportOffset(paneID string) int {
	return m.paneViewport().LegacyOffset(paneID)
}

func (m *Model) paneViewportOffset(paneID string) int {
	return m.paneViewport().Offset(paneID)
}

func (m *Model) effectiveTabViewportOffset(tab *workbench.TabState) int {
	return m.paneViewport().EffectiveTabOffset(tab)
}

func (m *Model) setPaneViewportOffset(paneID string, offset int) bool {
	return m.paneViewport().SetOffset(paneID, offset)
}

func (m *Model) adjustPaneViewportOffset(paneID string, delta int) (int, bool) {
	return m.paneViewport().AdjustOffset(paneID, delta)
}

func (m *Model) resetPaneViewport(paneID string) {
	if m == nil || paneID == "" {
		return
	}
	if m.paneViewport().Reset(paneID) {
		m.render.Invalidate()
	}
}
