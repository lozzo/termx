package state

func (store ShellStore) applyFloatingGroupCommand(command FloatingCommand) (ShellStore, FloatingCommandResult) {
	switch command.Action {
	case FloatingCommandToggleAll:
		return store.toggleAllFloatings(command.Action)
	case FloatingCommandShowAll:
		return store.setAllFloatingsCollapsed(false, command.Action)
	case FloatingCommandCollapseAll:
		return store.setAllFloatingsCollapsed(true, command.Action)
	case FloatingCommandFit:
		return store.fitFloating(command)
	case FloatingCommandToggleAutoFit:
		return store.toggleAutoFitFloating(command)
	case FloatingCommandRefreshAutoFit:
		return store.refreshAutoFitFloating(command)
	default:
		return store, floatingCommandInvalid(command.Action, "unknown floating group action")
	}
}

func (store ShellStore) toggleAllFloatings(action FloatingCommandAction) (ShellStore, FloatingCommandResult) {
	if len(store.Floatings) == 0 {
		return store, floatingCommandInvalid(action, "no floating")
	}
	hasExpanded := false
	for _, floating := range store.Floatings {
		if !floating.Collapsed {
			hasExpanded = true
			break
		}
	}
	return store.setAllFloatingsCollapsed(hasExpanded, action)
}

func (store ShellStore) setAllFloatingsCollapsed(collapsed bool, action FloatingCommandAction) (ShellStore, FloatingCommandResult) {
	if len(store.Floatings) == 0 {
		return store, floatingCommandInvalid(action, "no floating")
	}
	for index := range store.Floatings {
		store.Floatings[index].Collapsed = collapsed
	}
	if collapsed {
		store.ActiveFloatingID = ""
	} else {
		store.ActiveFloatingID = topFloatingID(store.Floatings)
	}
	store = store.ensureFloatingDefaults()
	return store, FloatingCommandResult{Status: FloatingCommandOK, Action: action, ID: store.ActiveFloatingID}
}

func (store ShellStore) fitFloating(command FloatingCommand) (ShellStore, FloatingCommandResult) {
	index := store.floatingIndexOrActive(command.TargetID)
	if index < 0 {
		return store, floatingCommandInvalid(command.Action, "floating not found")
	}
	if command.FitCols <= 0 || command.FitRows <= 0 {
		return store, floatingCommandInvalid(command.Action, "fit size unavailable")
	}
	rect := floatingRectForContentSize(command.FitCols, command.FitRows)
	rect = centerFloatingRect(rect, command.BoundsW, command.BoundsH)
	store.Floatings[index].Rect = rect
	store.Floatings[index].Collapsed = false
	store.Floatings[index].FitMode = FloatingFitManual
	store.Floatings[index].AutoFit = FloatingAutoFitState{}
	return store.focusRaiseFloating(store.Floatings[index].ID, command.Action)
}

func (store ShellStore) toggleAutoFitFloating(command FloatingCommand) (ShellStore, FloatingCommandResult) {
	index := store.floatingIndexOrActive(command.TargetID)
	if index < 0 {
		return store, floatingCommandInvalid(command.Action, "floating not found")
	}
	floating := &store.Floatings[index]
	if floating.FitMode == FloatingFitAuto {
		floating.FitMode = FloatingFitManual
		floating.AutoFit = FloatingAutoFitState{}
		return store.focusRaiseFloating(floating.ID, command.Action)
	}
	if command.FitCols <= 0 || command.FitRows <= 0 {
		return store, floatingCommandInvalid(command.Action, "fit size unavailable")
	}
	floating.FitMode = FloatingFitAuto
	floating.AutoFit = FloatingAutoFitState{Cols: command.FitCols, Rows: command.FitRows}
	rect := floatingRectForContentSize(command.FitCols, command.FitRows)
	floating.Rect = centerFloatingRect(rect, command.BoundsW, command.BoundsH)
	floating.Collapsed = false
	return store.focusRaiseFloating(floating.ID, command.Action)
}

func (store ShellStore) refreshAutoFitFloating(command FloatingCommand) (ShellStore, FloatingCommandResult) {
	index := store.floatingIndex(command.TargetID)
	if index < 0 {
		return store, floatingCommandInvalid(command.Action, "floating not found")
	}
	floating := store.Floatings[index]
	if floating.FitMode != FloatingFitAuto {
		return store, floatingCommandInvalid(command.Action, "auto-fit disabled")
	}
	if command.FitCols <= 0 || command.FitRows <= 0 {
		return store, floatingCommandInvalid(command.Action, "fit size unavailable")
	}
	if floating.AutoFit.Cols == command.FitCols && floating.AutoFit.Rows == command.FitRows {
		return store, FloatingCommandResult{Status: FloatingCommandOK, Action: command.Action, ID: floating.ID}
	}
	store.Floatings[index].AutoFit = FloatingAutoFitState{Cols: command.FitCols, Rows: command.FitRows}
	rect := floatingRectForContentSize(command.FitCols, command.FitRows)
	store.Floatings[index].Rect = centerFloatingRect(rect, command.BoundsW, command.BoundsH)
	return store, FloatingCommandResult{Status: FloatingCommandOK, Action: command.Action, ID: floating.ID}
}

func floatingRectForContentSize(cols int, rows int) FloatingRect {
	return clampFloatingRect(FloatingRect{W: cols + 2, H: rows + 2}, 0, 0)
}
