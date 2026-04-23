package app

import tea "github.com/charmbracelet/bubbletea"

func (m *Model) routeMouseInteraction(msg tea.MouseMsg) interactionDecision {
	decision := interactionDecision{Kind: interactionDecisionIgnore, Msg: msg, X: msg.X, Y: msg.Y}
	switch msg.Action {
	case tea.MouseActionPress:
		if msg.Button == tea.MouseButtonLeft {
			decision.Kind = interactionDecisionMouseClick
			return decision
		}
		if msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown {
			decision.Kind = interactionDecisionMouseWheel
			return decision
		}
		decision.Kind = interactionDecisionForwardTerminalMouse
		return decision
	case tea.MouseActionMotion:
		if msg.Button == tea.MouseButtonLeft && m.copyMode.MouseSelecting {
			decision.Kind = interactionDecisionUpdateCopySelection
			return decision
		}
		if msg.Button == tea.MouseButtonNone && m.copyMode.MouseSelecting {
			decision.Kind = interactionDecisionStopCopySelection
			return decision
		}
		if msg.Button == tea.MouseButtonLeft && m.mouseDragMode != mouseDragNone {
			decision.Kind = interactionDecisionMouseDrag
			return decision
		}
		if msg.Button == tea.MouseButtonNone && m.mouseDragMode != mouseDragNone {
			decision.Kind = interactionDecisionMouseRelease
			return decision
		}
		decision.Kind = interactionDecisionForwardTerminalMouse
		return decision
	case tea.MouseActionRelease:
		if (msg.Button == tea.MouseButtonLeft || msg.Button == tea.MouseButtonNone) && m.copyMode.MouseSelecting {
			decision.Kind = interactionDecisionFinalizeCopySelection
			return decision
		}
		if (msg.Button == tea.MouseButtonLeft || msg.Button == tea.MouseButtonNone) && m.mouseDragMode != mouseDragNone {
			decision.Kind = interactionDecisionFinalizeMouseDrag
			return decision
		}
		decision.Kind = interactionDecisionForwardTerminalMouse
		return decision
	default:
		return decision
	}
}

func (m *Model) dispatchMouseInteraction(decision interactionDecision) tea.Cmd {
	switch decision.Kind {
	case interactionDecisionMouseClick:
		return m.handleMouseClick(decision.Msg)
	case interactionDecisionMouseWheel:
		return m.handleMouseWheel(decision.Msg)
	case interactionDecisionForwardTerminalMouse:
		return m.forwardTerminalMouseInputCmd(decision.Msg)
	case interactionDecisionUpdateCopySelection:
		return m.updateMouseCopySelection(decision.X, decision.Y)
	case interactionDecisionStopCopySelection:
		m.stopMouseCopySelection()
		return nil
	case interactionDecisionMouseDrag:
		return m.handleMouseDrag(decision.X, decision.Y)
	case interactionDecisionMouseRelease:
		return m.handleMouseRelease()
	case interactionDecisionFinalizeCopySelection:
		_ = m.updateMouseCopySelection(decision.X, decision.Y)
		m.stopMouseCopySelection()
		return nil
	case interactionDecisionFinalizeMouseDrag:
		cmd := tea.Cmd(nil)
		if dragCmd := m.handleMouseDrag(decision.X, decision.Y); dragCmd != nil {
			cmd = dragCmd
		}
		return batchCmds(cmd, m.handleMouseRelease())
	default:
		return nil
	}
}
