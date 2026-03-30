package app

import "github.com/lozzow/termx/tuiv2/render"

type visibleState = render.VisibleRenderState

func (m *Model) renderVisibleState() visibleState {
	if m == nil {
		return visibleState{}
	}
	return render.AdaptVisibleState(m.workbench, m.runtime)
}
