package runtime

func (r *Runtime) PaneViewportOffset(paneID string) int {
	if r == nil || paneID == "" {
		return 0
	}
	binding := r.Binding(paneID)
	if binding == nil {
		return 0
	}
	if binding.Viewport.Offset < 0 {
		return 0
	}
	return binding.Viewport.Offset
}

func (r *Runtime) SetPaneViewportOffset(paneID string, offset int) bool {
	if r == nil || paneID == "" {
		return false
	}
	binding := r.BindPane(paneID)
	if binding == nil {
		return false
	}
	if offset < 0 {
		offset = 0
	}
	if binding.Viewport.Offset == offset {
		return false
	}
	binding.Viewport.Offset = offset
	r.touch()
	return true
}

func (r *Runtime) AdjustPaneViewportOffset(paneID string, delta int) (int, bool) {
	if r == nil || paneID == "" {
		return 0, false
	}
	binding := r.BindPane(paneID)
	if binding == nil {
		return 0, false
	}
	next := binding.Viewport.Offset + delta
	if next < 0 {
		next = 0
	}
	if binding.Viewport.Offset == next {
		return next, false
	}
	binding.Viewport.Offset = next
	r.touch()
	return next, true
}
