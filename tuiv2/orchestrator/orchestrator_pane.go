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
		target, ok := o.currentPaneTarget(action.PaneID)
		if !ok {
			return nil
		}
		return []Effect{ClosePaneEffect{PaneID: target.PaneID}}
	case input.ActionDetachPane:
		target, ok := o.currentPaneTarget(action.PaneID)
		if !ok {
			return nil
		}
		return []Effect{DetachPaneEffect{PaneID: target.PaneID}}
	case input.ActionReconnectPane:
		target, ok := o.currentPaneTarget(action.PaneID)
		if !ok {
			return nil
		}
		return []Effect{
			ReconnectPaneEffect{PaneID: target.PaneID},
			OpenPickerEffect{RequestID: target.PaneID},
			SetInputModeEffect{Mode: input.ModeState{Kind: input.ModePicker, RequestID: target.PaneID}},
		}
	case input.ActionClosePaneKill:
		target, ok := o.currentPaneTarget(action.PaneID)
		if !ok {
			return nil
		}
		effects := []Effect{ClosePaneEffect{PaneID: target.PaneID}}
		if target.TerminalID != "" {
			effects = append(effects, KillTerminalEffect{TerminalID: target.TerminalID})
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
		target, ok := o.currentPaneTarget(action.PaneID)
		if !ok {
			return nil
		}
		return []Effect{ResizePaneLayoutEffect{PaneID: target.PaneID, Kind: action.Kind, Delta: 0.05}}
	case input.ActionResizePaneLargeLeft, input.ActionResizePaneLargeRight, input.ActionResizePaneLargeUp, input.ActionResizePaneLargeDown:
		target, ok := o.currentPaneTarget(action.PaneID)
		if !ok {
			return nil
		}
		return []Effect{ResizePaneLayoutEffect{PaneID: target.PaneID, Kind: action.Kind, Delta: 0.15}}
	case input.ActionBalancePanes:
		if o.workbench.CurrentTab() == nil {
			return nil
		}
		return []Effect{BalancePanesEffect{}}
	case input.ActionCycleLayout:
		if o.workbench.CurrentTab() == nil {
			return nil
		}
		return []Effect{CycleLayoutEffect{}}
	default:
		return nil
	}
}

type paneActionTarget struct {
	TabID      string
	PaneID     string
	TerminalID string
}

func (o *Orchestrator) currentPaneTarget(paneID string) (paneActionTarget, bool) {
	if o == nil || o.workbench == nil {
		return paneActionTarget{}, false
	}
	tab := o.workbench.CurrentTab()
	if tab == nil {
		return paneActionTarget{}, false
	}
	targetPaneID := paneID
	if targetPaneID == "" {
		targetPaneID = tab.ActivePaneID
	}
	if targetPaneID == "" {
		return paneActionTarget{}, false
	}
	pane := tab.Panes[targetPaneID]
	if pane == nil {
		return paneActionTarget{}, false
	}
	return paneActionTarget{
		TabID:      tab.ID,
		PaneID:     targetPaneID,
		TerminalID: pane.TerminalID,
	}, true
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
