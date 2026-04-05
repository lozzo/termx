package orchestrator

import (
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func (o *Orchestrator) handlePaneAction(action input.SemanticAction) []Effect {
	switch action.Kind {
	case input.ActionSplitPane, input.ActionSplitPaneHorizontal:
		tab := o.workbench.CurrentTab()
		if tab == nil {
			return nil
		}
		paneID := action.PaneID
		if paneID == "" {
			paneID = tab.ActivePaneID
		}
		newPaneID := shared.NextPaneID()
		direction := workbench.SplitVertical
		if action.Kind == input.ActionSplitPaneHorizontal {
			direction = workbench.SplitHorizontal
		}
		_ = o.workbench.SplitPane(tab.ID, paneID, newPaneID, direction)
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
		neighborID := findNeighborPane(tab.Root.Rects(workbench.Rect{W: floatingBoundsW, H: floatingBoundsH}), paneID, action.Kind)
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
		return []Effect{ClosePaneEffect{PaneID: paneID}}
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
		if terminalID != "" && o.runtime != nil {
			if terminal := o.runtime.Registry().Get(terminalID); terminal != nil && terminal.State == "exited" {
				return []Effect{
					InvalidateRenderEffect{},
					OpenPickerEffect{RequestID: paneID},
					SetInputModeEffect{Mode: input.ModeState{Kind: input.ModePicker, RequestID: paneID}},
				}
			}
		}
		_ = o.workbench.BindPaneTerminal(tab.ID, paneID, "")
		if o.runtime != nil {
			o.runtime.UnbindPane(paneID, terminalID)
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
		effects := []Effect{ClosePaneEffect{PaneID: paneID}}
		if terminalID != "" {
			effects = append(effects, KillTerminalEffect{TerminalID: terminalID})
		}
		return effects
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
	default:
		return nil
	}
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
