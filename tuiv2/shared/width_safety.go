package shared

type WidthSafetyDecision struct {
	AmbiguousCompensation bool
	HostWidthStabilizer   bool
}

type WidthSafetyTransition struct {
	ReanchorBefore      bool
	HiddenCompensation  bool
	HostWidthStabilizer bool
}

type WidthSafetyTracker struct {
	lastVisibleDecision WidthSafetyDecision
	pendingReanchor     bool
}

func WidthSafetyForTerminalCell(content string, width int) WidthSafetyDecision {
	decision := WidthSafetyDecision{}
	if content == "" {
		return decision
	}
	if IsAmbiguousEmojiVariationSelectorCluster(content, width) {
		decision.AmbiguousCompensation = true
	}
	if width == 0 || (width == 1 && IsEastAsianAmbiguousWidthCluster(content) && !IsStableNarrowTerminalSymbol(content)) {
		decision.HostWidthStabilizer = true
	}
	return decision
}

func WidthSafetyForDisplayedCluster(content string, width int) WidthSafetyDecision {
	decision := WidthSafetyDecision{}
	if content == "" {
		return decision
	}
	if IsAmbiguousEmojiVariationSelectorCluster(content, width) {
		decision.AmbiguousCompensation = true
	}
	if IsHostWidthAmbiguousCluster(content, width) && !IsStableNarrowTerminalSymbol(content) {
		decision.HostWidthStabilizer = true
	}
	return decision
}

func (d WidthSafetyDecision) NeedsHiddenCompensation(eraseCount int) bool {
	return eraseCount == 1 && d.AmbiguousCompensation
}

func (t *WidthSafetyTracker) ObserveDisplayedCluster(content string, width int) WidthSafetyTransition {
	transition := WidthSafetyTransition{ReanchorBefore: t.pendingReanchor}
	t.lastVisibleDecision = WidthSafetyForDisplayedCluster(content, width)
	t.pendingReanchor = false
	return transition
}

func (t *WidthSafetyTracker) ObserveNonPrintingCluster(content string, width int) {
	t.lastVisibleDecision = WidthSafetyForDisplayedCluster(content, width)
}

func (t *WidthSafetyTracker) ObserveReanchorBeforeNextCluster() WidthSafetyTransition {
	transition := WidthSafetyTransition{
		HostWidthStabilizer: t.lastVisibleDecision.HostWidthStabilizer,
	}
	t.pendingReanchor = true
	return transition
}

func (t *WidthSafetyTracker) ObserveErase(eraseCount int) WidthSafetyTransition {
	if eraseCount <= 0 {
		eraseCount = 1
	}
	transition := WidthSafetyTransition{
		ReanchorBefore:     t.pendingReanchor,
		HiddenCompensation: t.lastVisibleDecision.NeedsHiddenCompensation(eraseCount),
	}
	t.pendingReanchor = false
	return transition
}
