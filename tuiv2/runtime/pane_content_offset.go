package runtime

func (r *Runtime) PaneContentOffset(paneID string) (int, int) {
	if r == nil || paneID == "" {
		return 0, 0
	}
	binding := r.Binding(paneID)
	if binding == nil {
		return 0, 0
	}
	return binding.ContentOffset.X, binding.ContentOffset.Y
}

func (r *Runtime) SetPaneContentOffset(paneID string, x, y int) bool {
	if r == nil || paneID == "" {
		return false
	}
	binding := r.BindPane(paneID)
	if binding == nil {
		return false
	}
	if binding.ContentOffset.X == x && binding.ContentOffset.Y == y {
		return false
	}
	binding.ContentOffset = PaneContentOffsetState{X: x, Y: y}
	r.touch()
	return true
}

func (r *Runtime) AdjustPaneContentOffset(paneID string, dx, dy int) (int, int, bool) {
	if r == nil || paneID == "" {
		return 0, 0, false
	}
	binding := r.BindPane(paneID)
	if binding == nil {
		return 0, 0, false
	}
	nextX := binding.ContentOffset.X + dx
	nextY := binding.ContentOffset.Y + dy
	if binding.ContentOffset.X == nextX && binding.ContentOffset.Y == nextY {
		return nextX, nextY, false
	}
	binding.ContentOffset = PaneContentOffsetState{X: nextX, Y: nextY}
	r.touch()
	return nextX, nextY, true
}

func (r *Runtime) ResetPaneContentOffset(paneID string) bool {
	return r.SetPaneContentOffset(paneID, 0, 0)
}
