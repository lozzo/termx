package state

import "strings"

func (store ShellStore) ApplyFloatingCommand(command FloatingCommand) (ShellStore, FloatingCommandResult) {
	store = store.EnsureDefaults()
	if command.Source == "" {
		command.Source = PaneCommandSourceKeyboard
	}
	switch command.Action {
	case FloatingCommandCreate:
		return store.createFloating(command)
	case FloatingCommandFocusRaise:
		return store.focusRaiseFloating(command.TargetID, command.Action)
	case FloatingCommandDeactivate:
		return store.deactivateFloating(command.Action)
	case FloatingCommandClose:
		return store.closeFloating(command.TargetID)
	case FloatingCommandCenter:
		return store.centerFloating(command)
	case FloatingCommandToggleCollapse:
		return store.toggleCollapseFloating(command.TargetID)
	case FloatingCommandSummon:
		return store.summonFloating(command)
	case FloatingCommandMove:
		return store.moveFloating(command)
	case FloatingCommandResize:
		return store.resizeFloating(command)
	case FloatingCommandToggleAll, FloatingCommandShowAll, FloatingCommandCollapseAll, FloatingCommandFit, FloatingCommandToggleAutoFit, FloatingCommandRefreshAutoFit:
		return store.applyFloatingGroupCommand(command)
	default:
		return store, floatingCommandInvalid(command.Action, "unknown action")
	}
}

func (store ShellStore) SummonFloatingByIndex(index int) (ShellStore, FloatingCommandResult) {
	return store.ApplyFloatingCommand(FloatingCommand{Action: FloatingCommandSummon, Index: index})
}

func (store ShellStore) BindFloatingTerminal(id string, terminalID string) ShellStore {
	store = store.EnsureDefaults()
	if terminalID == "" {
		return store
	}
	index := store.floatingIndexOrActive(id)
	if index < 0 {
		return store
	}
	if store.Floatings[index].Pane.ID == "" {
		store.Floatings[index].Pane = PaneState{ID: store.Floatings[index].ID + "-pane", Title: store.Floatings[index].Title, Kind: PaneTerminalLive}
	}
	store.Floatings[index].Pane.TerminalID = terminalID
	store.Floatings[index].Pane.Kind = PaneTerminalLive
	if store.Floatings[index].Pane.Title == "" {
		store.Floatings[index].Pane.Title = terminalID
	}
	store.Floatings[index].Active = true
	store.ActiveFloatingID = store.Floatings[index].ID
	store.Floatings[index].Z = store.nextFloatingZ() + 1
	return store.ensureFloatingDefaults()
}

func (store ShellStore) createFloating(command FloatingCommand) (ShellStore, FloatingCommandResult) {
	pane := command.Pane
	if pane.ID == "" {
		store.nextFloatingSeq++
		pane.ID = formatFloatingID(store.nextFloatingSeq) + "-pane"
	}
	if pane.Title == "" {
		pane.Title = "floating"
	}
	if pane.Kind == "" {
		pane.Kind = PaneEmpty
	}
	id := command.TargetID
	if id == "" {
		id = strings.TrimSuffix(pane.ID, "-pane")
		if id == "" || id == pane.ID {
			store.nextFloatingSeq++
			id = formatFloatingID(store.nextFloatingSeq)
		}
	}
	if store.floatingIndex(id) >= 0 {
		return store, floatingCommandInvalid(command.Action, "floating already exists")
	}
	rect := command.Rect
	if rect.W <= 0 {
		rect.W = 44
	}
	if rect.H <= 0 {
		rect.H = 12
	}
	if rect.X == 0 && rect.Y == 0 && command.BoundsW > 0 && command.BoundsH > 0 {
		rect = centerFloatingRect(rect, command.BoundsW, command.BoundsH)
	}
	rect = clampFloatingRect(rect, command.BoundsW, command.BoundsH)
	floating := FloatingPaneState{
		ID:      id,
		Title:   floatingTitle(command.Title, pane),
		Pane:    pane,
		Rect:    rect,
		Z:       store.nextFloatingZ() + 1,
		Active:  true,
		FitMode: FloatingFitManual,
	}
	store.Floatings = append(cloneFloatings(store.Floatings), floating)
	store.ActiveFloatingID = id
	store = store.ensureFloatingDefaults()
	return store, FloatingCommandResult{Status: FloatingCommandOK, Action: command.Action, ID: id}
}

func (store ShellStore) focusRaiseFloating(id string, action FloatingCommandAction) (ShellStore, FloatingCommandResult) {
	index := store.floatingIndexOrActive(id)
	if index < 0 {
		return store, floatingCommandInvalid(action, "floating not found")
	}
	id = store.Floatings[index].ID
	store.Floatings[index].Z = store.nextFloatingZ() + 1
	store.ActiveFloatingID = id
	store = store.ensureFloatingDefaults()
	return store, FloatingCommandResult{Status: FloatingCommandOK, Action: action, ID: id}
}

func (store ShellStore) deactivateFloating(action FloatingCommandAction) (ShellStore, FloatingCommandResult) {
	if store.ActiveFloatingID == "" {
		return store, FloatingCommandResult{Status: FloatingCommandOK, Action: action}
	}
	store.ActiveFloatingID = ""
	store = store.ensureFloatingDefaults()
	return store, FloatingCommandResult{Status: FloatingCommandOK, Action: action}
}

func (store ShellStore) closeFloating(id string) (ShellStore, FloatingCommandResult) {
	index := store.floatingIndexOrActive(id)
	if index < 0 {
		return store, floatingCommandInvalid(FloatingCommandClose, "floating not found")
	}
	id = store.Floatings[index].ID
	next := make([]FloatingPaneState, 0, len(store.Floatings)-1)
	for i, floating := range store.Floatings {
		if i != index {
			next = append(next, floating)
		}
	}
	store.Floatings = next
	store.ActiveFloatingID = topFloatingID(store.Floatings)
	store = store.ensureFloatingDefaults()
	return store, FloatingCommandResult{Status: FloatingCommandOK, Action: FloatingCommandClose, ID: id}
}

func (store ShellStore) centerFloating(command FloatingCommand) (ShellStore, FloatingCommandResult) {
	index := store.floatingIndexOrActive(command.TargetID)
	if index < 0 {
		return store, floatingCommandInvalid(command.Action, "floating not found")
	}
	rect := store.Floatings[index].Rect
	store.Floatings[index].Rect = centerFloatingRect(rect, command.BoundsW, command.BoundsH)
	return store.focusRaiseFloating(store.Floatings[index].ID, command.Action)
}

func (store ShellStore) toggleCollapseFloating(id string) (ShellStore, FloatingCommandResult) {
	index := store.floatingIndexOrActive(id)
	if index < 0 {
		return store, floatingCommandInvalid(FloatingCommandToggleCollapse, "floating not found")
	}
	store.Floatings[index].Collapsed = !store.Floatings[index].Collapsed
	return store.focusRaiseFloating(store.Floatings[index].ID, FloatingCommandToggleCollapse)
}

func (store ShellStore) summonFloating(command FloatingCommand) (ShellStore, FloatingCommandResult) {
	index := command.Index
	if command.TargetID != "" {
		index = store.floatingIndex(command.TargetID)
	}
	if index < 0 || index >= len(store.Floatings) {
		return store, floatingCommandInvalid(command.Action, "floating not found")
	}
	store.Floatings[index].Collapsed = false
	return store.focusRaiseFloating(store.Floatings[index].ID, command.Action)
}

func (store ShellStore) moveFloating(command FloatingCommand) (ShellStore, FloatingCommandResult) {
	index := store.floatingIndexOrActive(command.TargetID)
	if index < 0 {
		return store, floatingCommandInvalid(command.Action, "floating not found")
	}
	rect := store.Floatings[index].Rect
	rect.X += command.DeltaX
	rect.Y += command.DeltaY
	store.Floatings[index].Rect = clampFloatingRect(rect, command.BoundsW, command.BoundsH)
	store.Floatings[index].FitMode = FloatingFitManual
	store.Floatings[index].AutoFit = FloatingAutoFitState{}
	return store.focusRaiseFloating(store.Floatings[index].ID, command.Action)
}

func (store ShellStore) resizeFloating(command FloatingCommand) (ShellStore, FloatingCommandResult) {
	index := store.floatingIndexOrActive(command.TargetID)
	if index < 0 {
		return store, floatingCommandInvalid(command.Action, "floating not found")
	}
	rect := store.Floatings[index].Rect
	rect.W += command.DeltaW
	rect.H += command.DeltaH
	store.Floatings[index].Rect = clampFloatingRect(rect, command.BoundsW, command.BoundsH)
	store.Floatings[index].FitMode = FloatingFitManual
	store.Floatings[index].AutoFit = FloatingAutoFitState{}
	return store.focusRaiseFloating(store.Floatings[index].ID, command.Action)
}

func (store ShellStore) floatingIndex(id string) int {
	for index, floating := range store.Floatings {
		if floating.ID == id {
			return index
		}
	}
	return -1
}

func (store ShellStore) floatingIndexOrActive(id string) int {
	if id != "" {
		return store.floatingIndex(id)
	}
	if store.ActiveFloatingID != "" {
		return store.floatingIndex(store.ActiveFloatingID)
	}
	if len(store.Floatings) == 0 {
		return -1
	}
	topID := topFloatingID(store.Floatings)
	return store.floatingIndex(topID)
}

func (store ShellStore) nextFloatingZ() int {
	maxZ := 0
	for _, floating := range store.Floatings {
		if floating.Z > maxZ {
			maxZ = floating.Z
		}
	}
	return maxZ
}

func topFloatingID(floatings []FloatingPaneState) string {
	if len(floatings) == 0 {
		return ""
	}
	top := floatings[0]
	for _, floating := range floatings {
		if floating.Z >= top.Z {
			top = floating
		}
	}
	return top.ID
}

func floatingTitle(title string, pane PaneState) string {
	if title != "" {
		return title
	}
	if pane.Title != "" {
		return pane.Title
	}
	if pane.ID != "" {
		return pane.ID
	}
	return "floating"
}

func centerFloatingRect(rect FloatingRect, boundsW int, boundsH int) FloatingRect {
	if boundsW > 0 {
		rect.X = maxIntState(0, (boundsW-rect.W)/2)
	}
	if boundsH > 0 {
		rect.Y = maxIntState(0, (boundsH-rect.H)/2)
	}
	return clampFloatingRect(rect, boundsW, boundsH)
}

func clampFloatingRect(rect FloatingRect, boundsW int, boundsH int) FloatingRect {
	const minW = 16
	const minH = 4
	rect.W = maxIntState(minW, rect.W)
	rect.H = maxIntState(minH, rect.H)
	if boundsW > 0 {
		rect.W = minIntState(rect.W, maxIntState(minW, boundsW))
		rect.X = clampIntState(rect.X, 0, maxIntState(0, boundsW-rect.W))
	} else if rect.X < 0 {
		rect.X = 0
	}
	if boundsH > 0 {
		rect.H = minIntState(rect.H, maxIntState(minH, boundsH))
		rect.Y = clampIntState(rect.Y, 0, maxIntState(0, boundsH-rect.H))
	} else if rect.Y < 0 {
		rect.Y = 0
	}
	return rect
}

func clampIntState(value int, min int, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func minIntState(left int, right int) int {
	if left < right {
		return left
	}
	return right
}

func maxIntState(left int, right int) int {
	if left > right {
		return left
	}
	return right
}

func formatFloatingID(seq uint64) string {
	if seq == 0 {
		return "floating-0"
	}
	return "floating-" + formatToastID(seq)[len("toast-"):]
}

func floatingCommandInvalid(action FloatingCommandAction, reason string) FloatingCommandResult {
	return FloatingCommandResult{Status: FloatingCommandInvalid, Action: action, Reason: reason}
}
