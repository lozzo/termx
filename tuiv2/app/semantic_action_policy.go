package app

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/input"
)

type semanticActionPolicy struct {
	model *Model
}

func (m *Model) semanticActionPolicy() *semanticActionPolicy {
	if m == nil {
		return nil
	}
	return &semanticActionPolicy{model: m}
}

func (p *semanticActionPolicy) postEffectsCmd(action input.SemanticAction, effectsCmd tea.Cmd) tea.Cmd {
	if p == nil || p.model == nil {
		return effectsCmd
	}
	return batchCmds(effectsCmd, p.resizeCmd(action), p.saveCmd(action))
}

func (p *semanticActionPolicy) resizeCmd(action input.SemanticAction) tea.Cmd {
	if p == nil || p.model == nil {
		return nil
	}
	switch action.Kind {
	case input.ActionSplitPane,
		input.ActionSplitPaneHorizontal,
		input.ActionZoomPane,
		input.ActionResizePaneLeft,
		input.ActionResizePaneRight,
		input.ActionResizePaneUp,
		input.ActionResizePaneDown,
		input.ActionResizePaneLargeLeft,
		input.ActionResizePaneLargeRight,
		input.ActionResizePaneLargeUp,
		input.ActionResizePaneLargeDown,
		input.ActionBalancePanes,
		input.ActionCycleLayout:
		return p.model.resizeVisiblePanesCmd()
	case input.ActionResizeFloatingLeft,
		input.ActionResizeFloatingRight,
		input.ActionResizeFloatingUp,
		input.ActionResizeFloatingDown:
		return p.model.resizePaneIfNeededCmd(action.PaneID)
	default:
		return nil
	}
}

func (p *semanticActionPolicy) saveCmd(action input.SemanticAction) tea.Cmd {
	if p == nil || p.model == nil {
		return nil
	}
	switch action.Kind {
	case input.ActionSplitPane,
		input.ActionSplitPaneHorizontal,
		input.ActionResizePaneLeft,
		input.ActionResizePaneRight,
		input.ActionResizePaneUp,
		input.ActionResizePaneDown,
		input.ActionResizePaneLargeLeft,
		input.ActionResizePaneLargeRight,
		input.ActionResizePaneLargeUp,
		input.ActionResizePaneLargeDown,
		input.ActionBalancePanes,
		input.ActionCycleLayout:
		return p.model.saveStateCmd()
	default:
		return nil
	}
}
