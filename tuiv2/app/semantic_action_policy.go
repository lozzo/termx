package app

import (
	"context"

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
	if p.deferInvalidate(action) {
		if err := p.syncResizeNow(action); err != nil {
			return batchCmds(effectsCmd, p.model.showError(err), p.saveCmd(action))
		}
		return batchCmds(effectsCmd, p.saveCmd(action))
	}
	return batchCmds(effectsCmd, p.resizeCmd(action), p.saveCmd(action))
}

func (p *semanticActionPolicy) deferInvalidate(action input.SemanticAction) bool {
	if p == nil || p.model == nil {
		return false
	}
	switch action.Kind {
	case input.ActionResizePaneLeft,
		input.ActionResizePaneRight,
		input.ActionResizePaneUp,
		input.ActionResizePaneDown,
		input.ActionResizePaneLargeLeft,
		input.ActionResizePaneLargeRight,
		input.ActionResizePaneLargeUp,
		input.ActionResizePaneLargeDown,
		input.ActionBalancePanes,
		input.ActionCycleLayout,
		input.ActionResizeFloatingLeft,
		input.ActionResizeFloatingRight,
		input.ActionResizeFloatingUp,
		input.ActionResizeFloatingDown:
		return true
	default:
		return false
	}
}

func (p *semanticActionPolicy) syncResizeNow(action input.SemanticAction) error {
	if p == nil || p.model == nil {
		return nil
	}
	switch action.Kind {
	case input.ActionResizePaneLeft,
		input.ActionResizePaneRight,
		input.ActionResizePaneUp,
		input.ActionResizePaneDown,
		input.ActionResizePaneLargeLeft,
		input.ActionResizePaneLargeRight,
		input.ActionResizePaneLargeUp,
		input.ActionResizePaneLargeDown,
		input.ActionBalancePanes,
		input.ActionCycleLayout:
		if err := p.model.resizeVisiblePanes(context.Background()); err != nil {
			return err
		}
	case input.ActionResizeFloatingLeft,
		input.ActionResizeFloatingRight,
		input.ActionResizeFloatingUp,
		input.ActionResizeFloatingDown:
		pane, rect, ok := p.model.visiblePaneForInput(action.PaneID)
		if !ok || pane == nil || pane.TerminalID == "" {
			return nil
		}
		if err := p.model.ensurePaneTerminalSize(context.Background(), pane.ID, pane.TerminalID, rect); err != nil {
			return err
		}
	default:
		return nil
	}
	if p.model.render != nil {
		p.model.render.Invalidate()
	}
	return nil
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
