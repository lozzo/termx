package app

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/input"
)

func (m *Model) resolveMouseWheelPolicy(msg tea.MouseMsg, repeat int) scrollPolicyDecision {
	decision := scrollPolicyDecision{Kind: scrollPolicyNone}
	if m == nil || m.workbench == nil {
		return decision
	}
	x := msg.X
	y := msg.Y
	step := -1
	if msg.Button == tea.MouseButtonWheelUp {
		step = 1
	}
	repeat = maxInt(1, repeat)
	localRepeat := repeat * localMouseWheelScrollLines
	delta := step * localRepeat
	decision.Delta = delta
	decision.LocalRepeat = localRepeat

	vm := m.renderVM()
	if handled, cmd := m.handleOverlayMouseWheel(vm, delta); handled {
		decision.Kind = scrollPolicyHandled
		decision.Cmd = cmd
		return decision
	}
	if handled, cmd := m.handleTerminalPoolMouseWheel(vm, delta); handled {
		decision.Kind = scrollPolicyHandled
		decision.Cmd = cmd
		return decision
	}
	if m.mode().Kind == input.ModeDisplay {
		decision.Kind = scrollPolicyCopyModeMove
		decision.Cmd = m.moveCopyCursorVertical(-delta)
		return decision
	}
	if y < m.contentOriginY() {
		decision.Kind = scrollPolicySwitchTab
		if delta > 0 {
			decision.Cmd = m.switchCurrentTabByOffsetMouse(-localRepeat)
		} else {
			decision.Cmd = m.switchCurrentTabByOffsetMouse(localRepeat)
		}
		return decision
	}
	if in, ok := m.terminalWheelInputForMouseMsg(msg, step, repeat); ok {
		decision.Kind = scrollPolicyForwardTerminal
		decision.ForwardedInput = in
		return decision
	}
	if in, ok := m.alternateScreenWheelInputForMouseMsg(msg, step, repeat); ok {
		decision.Kind = scrollPolicyForwardTerminal
		decision.ForwardedInput = in
		return decision
	}

	contentY := y - m.contentOriginY()
	if contentY < 0 {
		return decision
	}
	if _, floating, ok := m.visiblePaneAt(x, contentY); ok {
		tab := m.workbench.CurrentTab()
		if tab == nil {
			return decision
		}
		targetPaneID := tab.ActivePaneID
		if floating != nil {
			if tab.ActivePaneID != floating.ID {
				_ = m.workbench.FocusPane(tab.ID, floating.ID)
				m.workbench.ReorderFloatingPane(tab.ID, floating.ID, true)
			}
			targetPaneID = floating.ID
		}
		decision.TargetPaneID = targetPaneID
		decision.Kind = scrollPolicyLocalScrollback
		decision.Cmd = m.localScrollbackWheelCmd(targetPaneID, delta)
		return decision
	}
	return decision
}

func (m *Model) dispatchResolvedMouseWheel(decision scrollPolicyDecision) tea.Cmd {
	switch decision.Kind {
	case scrollPolicyHandled, scrollPolicyCopyModeMove, scrollPolicySwitchTab, scrollPolicyLocalScrollback:
		return decision.Cmd
	case scrollPolicyForwardTerminal:
		return m.handleForwardedTerminalWheelInput(decision.ForwardedInput)
	default:
		return nil
	}
}

func (m *Model) resolveTerminalInputDispatch(in input.TerminalInput) (input.TerminalInput, tea.Cmd, bool) {
	if m == nil {
		return input.TerminalInput{}, nil, false
	}
	if len(in.Data) == 0 && in.Kind == input.TerminalInputPaste && in.Text != "" {
		if encoded := m.encodeActiveTerminalPaste(in.Text, in.PaneID); len(encoded) > 0 {
			in.Data = encoded
		}
	}
	if len(in.Data) == 0 {
		return input.TerminalInput{}, nil, false
	}
	if m.isPaneAttachPending(in.PaneID) {
		return in, nil, true
	}
	if m.workbench != nil {
		if pane := m.workbench.ActivePane(); pane != nil && pane.TerminalID == "" {
			if cmd := m.openPickerIfUnattached(pane.ID); cmd != nil {
				return input.TerminalInput{}, cmd, false
			}
		}
	}
	return in, nil, true
}

func (m *Model) terminalInputCmdForKeyMsg(msg tea.KeyMsg, in input.TerminalInput) tea.Cmd {
	if m == nil {
		return nil
	}
	if in.PaneID == "" && m.workbench != nil {
		if pane := m.workbench.ActivePane(); pane != nil {
			in.PaneID = pane.ID
		}
	}
	if encoded := m.encodeActiveTerminalInput(msg, in.PaneID); len(encoded) > 0 {
		in.Data = encoded
	}
	return m.handleTerminalInput(in)
}
