package workbench

func removeFloatingPane(entries []*FloatingState, paneID string) []*FloatingState {
	if len(entries) == 0 || paneID == "" {
		return entries
	}
	kept := entries[:0]
	for _, entry := range entries {
		if entry == nil || entry.PaneID == paneID {
			continue
		}
		kept = append(kept, entry)
	}
	return kept
}

func normalizeFloatingDisplay(state FloatingDisplayState) FloatingDisplayState {
	switch state {
	case FloatingDisplayCollapsed, FloatingDisplayHidden:
		return state
	default:
		return FloatingDisplayExpanded
	}
}

func normalizeFloatingFitMode(mode FloatingFitMode) FloatingFitMode {
	if mode == FloatingFitAuto {
		return mode
	}
	return FloatingFitManual
}

func normalizeFloatingState(state *FloatingState) {
	if state == nil {
		return
	}
	state.Display = normalizeFloatingDisplay(state.Display)
	state.FitMode = normalizeFloatingFitMode(state.FitMode)
	if state.Display == FloatingDisplayExpanded && state.Rect.W > 0 && state.Rect.H > 0 {
		if state.RestoreRect.W <= 0 || state.RestoreRect.H <= 0 {
			state.RestoreRect = state.Rect
		}
	}
}

func normalizedFloatingState(state *FloatingState) FloatingState {
	if state == nil {
		return FloatingState{}
	}
	normalized := *state
	normalized.Display = normalizeFloatingDisplay(normalized.Display)
	normalized.FitMode = normalizeFloatingFitMode(normalized.FitMode)
	if normalized.Display == FloatingDisplayExpanded && normalized.Rect.W > 0 && normalized.Rect.H > 0 {
		if normalized.RestoreRect.W <= 0 || normalized.RestoreRect.H <= 0 {
			normalized.RestoreRect = normalized.Rect
		}
	}
	return normalized
}

func floatingStateVisible(state *FloatingState) bool {
	if state == nil {
		return false
	}
	return normalizeFloatingDisplay(state.Display) == FloatingDisplayExpanded
}

func visibleFloatingStates(entries []*FloatingState) []*FloatingState {
	if len(entries) == 0 {
		return nil
	}
	visible := make([]*FloatingState, 0, len(entries))
	for _, entry := range orderedFloating(entries) {
		if entry == nil {
			continue
		}
		normalized := normalizedFloatingState(entry)
		if !floatingStateVisible(&normalized) {
			continue
		}
		copy := normalized
		visible = append(visible, &copy)
	}
	return visible
}

func hasExpandedFloating(entries []*FloatingState) bool {
	for _, entry := range entries {
		if entry == nil {
			continue
		}
		normalized := normalizedFloatingState(entry)
		if normalized.Display == FloatingDisplayExpanded {
			return true
		}
	}
	return false
}
