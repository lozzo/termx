package orchestrator

import (
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func (o *Orchestrator) handleFloatingAction(action input.SemanticAction) []Effect {
	switch action.Kind {
	case input.ActionCreateFloatingPane:
		tab := o.workbench.CurrentTab()
		if tab == nil {
			return nil
		}
		paneID := shared.NextPaneID()
		_ = o.workbench.CreateFloatingPane(tab.ID, paneID, workbench.Rect{X: 10, Y: 5, W: 80, H: 24})
		_ = o.workbench.FocusPane(tab.ID, paneID)
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
		target, ok := o.currentFloatingPaneTarget(action.PaneID)
		if !ok {
			return nil
		}
		return []Effect{MoveFloatingPaneEffect{PaneID: target.PaneID, Kind: action.Kind}}
	case input.ActionResizeFloatingLeft, input.ActionResizeFloatingRight, input.ActionResizeFloatingUp, input.ActionResizeFloatingDown:
		target, ok := o.currentFloatingPaneTarget(action.PaneID)
		if !ok {
			return nil
		}
		return []Effect{ResizeFloatingPaneEffect{PaneID: target.PaneID, Kind: action.Kind}}
	case input.ActionCenterFloatingPane:
		target, ok := o.currentFloatingPaneTarget(action.PaneID)
		if !ok {
			return nil
		}
		return []Effect{CenterFloatingPaneEffect{PaneID: target.PaneID}}
	case input.ActionToggleFloatingVisibility:
		tab := o.workbench.CurrentTab()
		if tab == nil {
			return nil
		}
		tab.FloatingVisible = !tab.FloatingVisible
		return []Effect{InvalidateRenderEffect{}}
	case input.ActionCloseFloatingPane:
		target, ok := o.currentFloatingPaneTarget(action.PaneID)
		if !ok {
			return nil
		}
		return []Effect{ClosePaneEffect{PaneID: target.PaneID}}
	default:
		return nil
	}
}

func (o *Orchestrator) currentFloatingPaneTarget(paneID string) (paneActionTarget, bool) {
	if o == nil || o.workbench == nil {
		return paneActionTarget{}, false
	}
	tab := o.workbench.CurrentTab()
	if tab == nil {
		return paneActionTarget{}, false
	}
	targetPaneID := activeFloatingPaneID(tab, paneID)
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
