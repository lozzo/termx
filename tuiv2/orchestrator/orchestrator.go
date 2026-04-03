package orchestrator

import (
	"context"
	"fmt"

	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/lozzow/termx/tuiv2/workbench"
)

type Orchestrator struct {
	workbench *workbench.Workbench
	runtime   *runtime.Runtime
	modalHost *modal.ModalHost
}

const (
	floatingMoveStep   = 2
	floatingResizeStep = 2
	floatingBoundsW    = 200
	floatingBoundsH    = 50
)

func New(wb *workbench.Workbench, rt *runtime.Runtime, mh *modal.ModalHost) *Orchestrator {
	return &Orchestrator{workbench: wb, runtime: rt, modalHost: mh}
}

func (o *Orchestrator) HandleSemanticAction(action input.SemanticAction) []Effect {
	switch action.Kind {
	case input.ActionOpenPicker:
		if o.modalHost != nil {
			o.modalHost.Open(input.ModePicker, action.TargetID)
		}
		return []Effect{
			SetInputModeEffect{Mode: input.ModeState{Kind: input.ModePicker, RequestID: action.TargetID}},
			OpenPickerEffect{RequestID: action.TargetID},
		}
	case input.ActionOpenWorkspacePicker:
		if o.modalHost != nil {
			o.modalHost.Open(input.ModeWorkspacePicker, action.TargetID)
			o.modalHost.WorkspacePicker = &modal.WorkspacePickerState{}
		}
		return []Effect{
			SetInputModeEffect{Mode: input.ModeState{Kind: input.ModeWorkspacePicker, RequestID: action.TargetID}},
			OpenWorkspacePickerEffect{RequestID: action.TargetID},
			LoadWorkspaceItemsEffect{},
		}
	case input.ActionSubmitPrompt:
		// action.TargetID 是用户在 picker 中选中的 terminalID；
		// action.PaneID 是发起请求的 pane。
		return []Effect{
			AttachTerminalEffect{
				PaneID:     action.PaneID,
				TerminalID: action.TargetID,
				Mode:       "collaborator",
			},
		}
	case input.ActionSplitPane, input.ActionSplitPaneHorizontal:
		tab := o.workbench.CurrentTab()
		if tab == nil {
			return nil
		}
		paneID := action.PaneID
		if paneID == "" {
			paneID = tab.ActivePaneID
		}
		newPaneID := "pane-" + shared.GenerateShortID()
		direction := workbench.SplitVertical
		if action.Kind == input.ActionSplitPaneHorizontal {
			direction = workbench.SplitHorizontal
		}
		_ = o.workbench.SplitPane(tab.ID, paneID, newPaneID, direction)
		if o.modalHost != nil {
			o.modalHost.Open(input.ModePicker, newPaneID)
		}
		return []Effect{
			InvalidateRenderEffect{},
			OpenPickerEffect{RequestID: newPaneID},
			SetInputModeEffect{Mode: input.ModeState{Kind: input.ModePicker, RequestID: newPaneID}},
		}
	case input.ActionFocusPaneLeft, input.ActionFocusPaneRight, input.ActionFocusPaneUp, input.ActionFocusPaneDown:
		tab := o.workbench.CurrentTab()
		if tab == nil || tab.Root == nil {
			return nil
		}
		paneID := action.PaneID
		if paneID == "" {
			paneID = tab.ActivePaneID
		}
		neighborID := findNeighborPane(tab.Root.Rects(workbench.Rect{W: 200, H: 50}), paneID, action.Kind)
		if neighborID != "" {
			_ = o.workbench.FocusPane(tab.ID, neighborID)
		}
		return []Effect{InvalidateRenderEffect{}}
	case input.ActionClosePane:
		tab := o.workbench.CurrentTab()
		if tab == nil {
			return nil
		}
		paneID := action.PaneID
		if paneID == "" {
			paneID = tab.ActivePaneID
		}
		terminalID, _ := o.workbench.ClosePane(tab.ID, paneID)
		if o.runtime != nil {
			o.runtime.UnbindPane(paneID, terminalID)
		}
		if current := o.workbench.CurrentTab(); current != nil && current.ID == tab.ID && current.ActivePaneID != "" {
			_ = o.workbench.FocusPane(tab.ID, current.ActivePaneID)
		}
		return []Effect{InvalidateRenderEffect{}}
	case input.ActionDetachPane:
		tab := o.workbench.CurrentTab()
		if tab == nil {
			return nil
		}
		paneID := action.PaneID
		if paneID == "" {
			paneID = tab.ActivePaneID
		}
		pane := tab.Panes[paneID]
		if pane == nil {
			return nil
		}
		terminalID := pane.TerminalID
		_ = o.workbench.BindPaneTerminal(tab.ID, paneID, "")
		if o.runtime != nil {
			o.runtime.UnbindPane(paneID, terminalID)
		}
		return []Effect{InvalidateRenderEffect{}}
	case input.ActionReconnectPane:
		tab := o.workbench.CurrentTab()
		if tab == nil {
			return nil
		}
		paneID := action.PaneID
		if paneID == "" {
			paneID = tab.ActivePaneID
		}
		pane := tab.Panes[paneID]
		if pane == nil {
			return nil
		}
		terminalID := pane.TerminalID
		_ = o.workbench.BindPaneTerminal(tab.ID, paneID, "")
		if o.runtime != nil {
			o.runtime.UnbindPane(paneID, terminalID)
		}
		if o.modalHost != nil {
			o.modalHost.Open(input.ModePicker, paneID)
		}
		return []Effect{
			InvalidateRenderEffect{},
			OpenPickerEffect{RequestID: paneID},
			SetInputModeEffect{Mode: input.ModeState{Kind: input.ModePicker, RequestID: paneID}},
		}
	case input.ActionClosePaneKill:
		tab := o.workbench.CurrentTab()
		if tab == nil {
			return nil
		}
		paneID := action.PaneID
		if paneID == "" {
			paneID = tab.ActivePaneID
		}
		terminalID, _ := o.workbench.ClosePane(tab.ID, paneID)
		if o.runtime != nil {
			o.runtime.UnbindPane(paneID, terminalID)
		}
		effects := []Effect{InvalidateRenderEffect{}}
		if terminalID != "" {
			effects = append(effects, KillTerminalEffect{TerminalID: terminalID})
		}
		return effects
	case input.ActionCreateTab:
		ws := o.workbench.CurrentWorkspace()
		if ws == nil {
			return nil
		}
		tabID := "tab-" + shared.GenerateShortID()
		tabName := fmt.Sprintf("%d", len(ws.Tabs)+1)
		paneID := "pane-" + shared.GenerateShortID()
		_ = o.workbench.CreateTab(ws.Name, tabID, tabName)
		_ = o.workbench.CreateFirstPane(tabID, paneID)
		_ = o.workbench.SwitchTab(ws.Name, len(ws.Tabs)-1)
		if o.modalHost != nil {
			o.modalHost.Open(input.ModePicker, paneID)
		}
		return []Effect{
			InvalidateRenderEffect{},
			OpenPickerEffect{RequestID: paneID},
			SetInputModeEffect{Mode: input.ModeState{Kind: input.ModePicker, RequestID: paneID}},
		}
	case input.ActionNextTab, input.ActionPrevTab:
		ws := o.workbench.CurrentWorkspace()
		if ws == nil || len(ws.Tabs) == 0 {
			return nil
		}
		delta := 1
		if action.Kind == input.ActionPrevTab {
			delta = -1
		}
		next := (ws.ActiveTab + delta + len(ws.Tabs)) % len(ws.Tabs)
		_ = o.workbench.SwitchTab(ws.Name, next)
		return []Effect{SwitchTabEffect{Delta: delta}}
	case input.ActionCloseTab:
		tabID := action.TabID
		if tabID == "" {
			tab := o.workbench.CurrentTab()
			if tab == nil {
				return nil
			}
			tabID = tab.ID
		}
		_ = o.workbench.CloseTab(tabID)
		return []Effect{InvalidateRenderEffect{}}
	case input.ActionZoomPane:
		tab := o.workbench.CurrentTab()
		if tab == nil {
			return nil
		}
		paneID := action.PaneID
		if paneID == "" {
			paneID = tab.ActivePaneID
		}
		if tab.ZoomedPaneID == paneID {
			tab.ZoomedPaneID = ""
		} else {
			tab.ZoomedPaneID = paneID
		}
		return []Effect{InvalidateRenderEffect{}}
	case input.ActionSwitchWorkspace:
		if o.workbench != nil {
			_ = o.workbench.SwitchWorkspace(action.Text)
		}
		if o.modalHost != nil && o.modalHost.Session != nil {
			o.modalHost.Close(input.ModeWorkspacePicker, o.modalHost.Session.RequestID)
		}
		return []Effect{
			SetInputModeEffect{Mode: input.ModeState{Kind: input.ModeNormal}},
			InvalidateRenderEffect{},
		}
	case input.ActionResizePaneLeft, input.ActionResizePaneRight, input.ActionResizePaneUp, input.ActionResizePaneDown:
		tab := o.workbench.CurrentTab()
		if tab == nil {
			return nil
		}
		paneID := action.PaneID
		if paneID == "" {
			paneID = tab.ActivePaneID
		}
		var dir workbench.Direction
		switch action.Kind {
		case input.ActionResizePaneLeft:
			dir = workbench.DirectionLeft
		case input.ActionResizePaneRight:
			dir = workbench.DirectionRight
		case input.ActionResizePaneUp:
			dir = workbench.DirectionUp
		default:
			dir = workbench.DirectionDown
		}
		_ = o.workbench.AdjustPaneRatio(tab.ID, paneID, dir, 0.05)
		return []Effect{InvalidateRenderEffect{}}
	case input.ActionResizePaneLargeLeft, input.ActionResizePaneLargeRight, input.ActionResizePaneLargeUp, input.ActionResizePaneLargeDown:
		tab := o.workbench.CurrentTab()
		if tab == nil {
			return nil
		}
		paneID := action.PaneID
		if paneID == "" {
			paneID = tab.ActivePaneID
		}
		var dir workbench.Direction
		switch action.Kind {
		case input.ActionResizePaneLargeLeft:
			dir = workbench.DirectionLeft
		case input.ActionResizePaneLargeRight:
			dir = workbench.DirectionRight
		case input.ActionResizePaneLargeUp:
			dir = workbench.DirectionUp
		default:
			dir = workbench.DirectionDown
		}
		_ = o.workbench.AdjustPaneRatio(tab.ID, paneID, dir, 0.15)
		return []Effect{InvalidateRenderEffect{}}
	case input.ActionBalancePanes:
		tab := o.workbench.CurrentTab()
		if tab == nil {
			return nil
		}
		o.workbench.BalancePanes(tab.ID)
		return []Effect{InvalidateRenderEffect{}}
	case input.ActionCycleLayout:
		tab := o.workbench.CurrentTab()
		if tab == nil {
			return nil
		}
		o.workbench.CycleLayout(tab.ID)
		return []Effect{InvalidateRenderEffect{}}
	case input.ActionCreateWorkspace:
		if o.workbench == nil {
			return nil
		}
		name := "workspace-" + shared.GenerateShortID()
		_ = o.workbench.CreateWorkspace(name)
		_ = o.workbench.SwitchWorkspace(name)
		if o.modalHost != nil && o.modalHost.Session != nil && o.modalHost.Session.Kind == input.ModeWorkspacePicker {
			o.modalHost.Close(input.ModeWorkspacePicker, o.modalHost.Session.RequestID)
		}
		return []Effect{
			SetInputModeEffect{Mode: input.ModeState{Kind: input.ModeNormal}},
			InvalidateRenderEffect{},
		}
	case input.ActionDeleteWorkspace:
		if o.workbench == nil {
			return nil
		}
		ws := o.workbench.CurrentWorkspace()
		if ws == nil {
			return nil
		}
		_ = o.workbench.DeleteWorkspace(ws.Name)
		return []Effect{
			SetInputModeEffect{Mode: input.ModeState{Kind: input.ModeNormal}},
			InvalidateRenderEffect{},
		}
	case input.ActionCreateFloatingPane:
		tab := o.workbench.CurrentTab()
		if tab == nil {
			return nil
		}
		paneID := "pane-" + shared.GenerateShortID()
		_ = o.workbench.CreateFloatingPane(tab.ID, paneID, workbench.Rect{X: 10, Y: 5, W: 80, H: 24})
		_ = o.workbench.FocusPane(tab.ID, paneID)
		if o.modalHost != nil {
			o.modalHost.Open(input.ModePicker, paneID)
		}
		return []Effect{
			InvalidateRenderEffect{},
			OpenPickerEffect{RequestID: paneID},
			SetInputModeEffect{Mode: input.ModeState{Kind: input.ModePicker, RequestID: paneID}},
		}
	case input.ActionFocusPrevFloatingPane, input.ActionFocusNextFloatingPane:
		tab := o.workbench.CurrentTab()
		if tab == nil {
			return nil
		}
		paneID := cycleFloatingPaneID(tab, action.PaneID, action.Kind)
		if paneID == "" {
			return nil
		}
		_ = o.workbench.FocusPane(tab.ID, paneID)
		o.workbench.ReorderFloatingPane(tab.ID, paneID, true)
		return []Effect{InvalidateRenderEffect{}}
	case input.ActionMoveFloatingLeft, input.ActionMoveFloatingRight, input.ActionMoveFloatingUp, input.ActionMoveFloatingDown:
		tab := o.workbench.CurrentTab()
		if tab == nil {
			return nil
		}
		paneID := activeFloatingPaneID(tab, action.PaneID)
		if paneID == "" {
			return nil
		}
		dx, dy := 0, 0
		switch action.Kind {
		case input.ActionMoveFloatingLeft:
			dx = -floatingMoveStep
		case input.ActionMoveFloatingRight:
			dx = floatingMoveStep
		case input.ActionMoveFloatingUp:
			dy = -floatingMoveStep
		case input.ActionMoveFloatingDown:
			dy = floatingMoveStep
		}
		if !o.workbench.MoveFloatingPaneBy(tab.ID, paneID, dx, dy) {
			return nil
		}
		o.workbench.ReorderFloatingPane(tab.ID, paneID, true)
		return []Effect{InvalidateRenderEffect{}}
	case input.ActionResizeFloatingLeft, input.ActionResizeFloatingRight, input.ActionResizeFloatingUp, input.ActionResizeFloatingDown:
		tab := o.workbench.CurrentTab()
		if tab == nil {
			return nil
		}
		paneID := activeFloatingPaneID(tab, action.PaneID)
		if paneID == "" {
			return nil
		}
		dw, dh := 0, 0
		switch action.Kind {
		case input.ActionResizeFloatingLeft:
			dw = -floatingResizeStep
		case input.ActionResizeFloatingRight:
			dw = floatingResizeStep
		case input.ActionResizeFloatingUp:
			dh = -floatingResizeStep
		case input.ActionResizeFloatingDown:
			dh = floatingResizeStep
		}
		if !o.workbench.ResizeFloatingPaneBy(tab.ID, paneID, dw, dh) {
			return nil
		}
		o.workbench.ReorderFloatingPane(tab.ID, paneID, true)
		return []Effect{InvalidateRenderEffect{}}
	case input.ActionCenterFloatingPane:
		tab := o.workbench.CurrentTab()
		if tab == nil {
			return nil
		}
		paneID := activeFloatingPaneID(tab, action.PaneID)
		if paneID == "" {
			return nil
		}
		if !o.workbench.CenterFloatingPane(tab.ID, paneID, workbench.Rect{W: floatingBoundsW, H: floatingBoundsH}) {
			return nil
		}
		o.workbench.ReorderFloatingPane(tab.ID, paneID, true)
		return []Effect{InvalidateRenderEffect{}}
	case input.ActionToggleFloatingVisibility:
		tab := o.workbench.CurrentTab()
		if tab == nil {
			return nil
		}
		tab.FloatingVisible = !tab.FloatingVisible
		return []Effect{InvalidateRenderEffect{}}
	case input.ActionCloseFloatingPane:
		tab := o.workbench.CurrentTab()
		if tab == nil {
			return nil
		}
		paneID := activeFloatingPaneID(tab, action.PaneID)
		if paneID == "" {
			return nil
		}
		terminalID, err := o.workbench.ClosePane(tab.ID, paneID)
		if err != nil {
			return nil
		}
		if o.runtime != nil {
			o.runtime.UnbindPane(paneID, terminalID)
		}
		return []Effect{InvalidateRenderEffect{}}
	case input.ActionKillTerminal:
		if action.TargetID == "" {
			return nil
		}
		return []Effect{KillTerminalEffect{TerminalID: action.TargetID}}
	default:
		return nil
	}
}

func activeFloatingPaneID(tab *workbench.TabState, paneID string) string {
	if tab == nil {
		return ""
	}
	target := paneID
	if target == "" {
		target = tab.ActivePaneID
	}
	if target == "" {
		return ""
	}
	for _, floating := range tab.Floating {
		if floating != nil && floating.PaneID == target {
			return target
		}
	}
	return ""
}

func cycleFloatingPaneID(tab *workbench.TabState, paneID string, kind input.ActionKind) string {
	if tab == nil || len(tab.Floating) == 0 {
		return ""
	}
	return cycleFloatingPaneIDFromEntries(tab, paneID, kind)
}

func cycleFloatingPaneIDFromEntries(tab *workbench.TabState, paneID string, kind input.ActionKind) string {
	ordered := make([]string, 0, len(tab.Floating))
	for _, floating := range workbenchOrderedFloating(tab.Floating) {
		if floating == nil || floating.PaneID == "" || tab.Panes[floating.PaneID] == nil {
			continue
		}
		ordered = append(ordered, floating.PaneID)
	}
	if len(ordered) == 0 {
		return ""
	}
	target := paneID
	if target == "" {
		target = activeFloatingPaneID(tab, "")
	}
	if target == "" {
		if kind == input.ActionFocusPrevFloatingPane {
			return ordered[len(ordered)-1]
		}
		return ordered[0]
	}
	currentIndex := -1
	for index, candidate := range ordered {
		if candidate == target {
			currentIndex = index
			break
		}
	}
	if currentIndex < 0 {
		if kind == input.ActionFocusPrevFloatingPane {
			return ordered[len(ordered)-1]
		}
		return ordered[0]
	}
	delta := 1
	if kind == input.ActionFocusPrevFloatingPane {
		delta = -1
	}
	next := (currentIndex + delta + len(ordered)) % len(ordered)
	return ordered[next]
}

func workbenchOrderedFloating(entries []*workbench.FloatingState) []*workbench.FloatingState {
	if len(entries) == 0 {
		return nil
	}
	ordered := append([]*workbench.FloatingState(nil), entries...)
	for i := 1; i < len(ordered); i++ {
		for j := i; j > 0 && ordered[j-1] != nil && ordered[j] != nil && ordered[j-1].Z > ordered[j].Z; j-- {
			ordered[j-1], ordered[j] = ordered[j], ordered[j-1]
		}
	}
	return ordered
}

func findNeighborPane(rects map[string]workbench.Rect, paneID string, kind input.ActionKind) string {
	current, ok := rects[paneID]
	if !ok {
		return ""
	}
	bestID := ""
	bestValue := 0
	for id, rect := range rects {
		if id == paneID {
			continue
		}
		switch kind {
		case input.ActionFocusPaneLeft:
			value := rect.X + rect.W
			if value <= current.X && (bestID == "" || value > bestValue) {
				bestID, bestValue = id, value
			}
		case input.ActionFocusPaneRight:
			value := rect.X
			if value >= current.X+current.W && (bestID == "" || value < bestValue) {
				bestID, bestValue = id, value
			}
		case input.ActionFocusPaneUp:
			value := rect.Y + rect.H
			if value <= current.Y && (bestID == "" || value > bestValue) {
				bestID, bestValue = id, value
			}
		case input.ActionFocusPaneDown:
			value := rect.Y
			if value >= current.Y+current.H && (bestID == "" || value < bestValue) {
				bestID, bestValue = id, value
			}
		}
	}
	return bestID
}

func (o *Orchestrator) AttachAndLoadSnapshot(ctx context.Context, paneID, terminalID, mode string, offset, limit int) ([]any, error) {
	terminal, err := o.runtime.AttachTerminal(ctx, paneID, terminalID, mode)
	if err != nil {
		return nil, err
	}
	o.bindWorkbenchPaneTerminal(paneID, terminalID)
	snapshot, err := o.runtime.LoadSnapshot(ctx, terminalID, offset, limit)
	if err != nil {
		return nil, err
	}
	msgs := []any{
		TerminalAttachedMsg{PaneID: paneID, TerminalID: terminalID, Channel: terminal.Channel},
		SnapshotLoadedMsg{PaneID: paneID, TerminalID: terminalID, Snapshot: snapshot},
	}
	if o.modalHost != nil && o.modalHost.Session != nil {
		o.modalHost.MarkReady(o.modalHost.Session.Kind, o.modalHost.Session.RequestID)
	}
	return msgs, nil
}

func (o *Orchestrator) bindWorkbenchPaneTerminal(paneID, terminalID string) {
	if o == nil || o.workbench == nil || paneID == "" || terminalID == "" {
		return
	}
	workspace := o.workbench.CurrentWorkspace()
	if workspace == nil {
		return
	}
	for _, tab := range workspace.Tabs {
		if tab == nil || tab.Panes[paneID] == nil {
			continue
		}
		_ = o.workbench.BindPaneTerminal(tab.ID, paneID, terminalID)
		_ = o.workbench.FocusPane(tab.ID, paneID)
		return
	}
}
