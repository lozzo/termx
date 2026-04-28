package app

import (
	"github.com/lozzow/termx/tuiv2/viewstate"
	"github.com/lozzow/termx/tuiv2/workbench"
)

type localViewProjection = viewstate.Projection

func (m *Model) captureLocalViewProjection() localViewProjection {
	if m == nil {
		return localViewProjection{}
	}
	return viewstate.Capture(m.workbench, viewstate.CaptureOptions{
		PaneViewportOffset: func(paneID string) (int, bool) {
			return m.paneViewportBindingOffset(paneID)
		},
		EffectiveTabViewportOffset: func(tab *workbench.TabState) int {
			return m.effectiveTabViewportOffset(tab)
		},
	})
}

func (m *Model) applyLocalViewProjection(proj localViewProjection) {
	if m == nil {
		return
	}
	viewstate.Apply(m.workbench, proj, viewstate.ApplyOptions{
		SetPaneViewportOffset: func(paneID string, offset int) bool {
			return m.setPaneViewportOffset(paneID, offset)
		},
	})
}
