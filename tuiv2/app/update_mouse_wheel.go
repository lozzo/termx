package app

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/render"
)

const localMouseWheelScrollLines = 3

func (m *Model) handleMouseWheel(msg tea.MouseMsg) tea.Cmd {
	return m.handleMouseWheelRepeated(msg, 1)
}

func (m *Model) handleMouseWheelBurstMsg(msg mouseWheelBurstMsg) tea.Cmd {
	return m.handleMouseWheelRepeated(msg.Msg, maxInt(1, msg.Repeat))
}

func (m *Model) handleMouseWheelRepeated(msg tea.MouseMsg, repeat int) tea.Cmd {
	if m == nil || m.workbench == nil {
		return nil
	}
	x := msg.X
	y := msg.Y
	button := msg.Button
	step := -1
	if button == tea.MouseButtonWheelUp {
		step = 1
	}
	repeat = maxInt(1, repeat)
	localRepeat := repeat * localMouseWheelScrollLines
	delta := step * localRepeat
	vm := m.renderVM()
	if handled, cmd := m.handleOverlayMouseWheel(vm, delta); handled {
		return cmd
	}
	if handled, cmd := m.handleTerminalPoolMouseWheel(vm, delta); handled {
		return cmd
	}
	if m.mode().Kind == input.ModeDisplay {
		return m.moveCopyCursorVertical(-delta)
	}
	if y < m.contentOriginY() {
		if delta > 0 {
			return m.switchCurrentTabByOffsetMouse(-localRepeat)
		}
		return m.switchCurrentTabByOffsetMouse(localRepeat)
	}
	if in, ok := m.terminalWheelInputForMouseMsg(msg, step, repeat); ok {
		return m.handleTerminalInput(in)
	}
	if in, ok := m.alternateScreenWheelInputForMouseMsg(msg, step, repeat); ok {
		return m.handleTerminalInput(in)
	}

	contentY := y - m.contentOriginY()
	if contentY < 0 {
		return nil
	}
	if _, floating, ok := m.visiblePaneAt(x, contentY); ok {
		tab := m.workbench.CurrentTab()
		if tab == nil {
			return nil
		}
		if floating != nil {
			if tab.ActivePaneID != floating.ID {
				_ = m.workbench.FocusPane(tab.ID, floating.ID)
				m.workbench.ReorderFloatingPane(tab.ID, floating.ID, true)
			}
		}
		if cmd := m.localScrollbackWheelCmd(tab.ID, delta); cmd != nil {
			return cmd
		}
		return nil
	}
	return nil
}

func (m *Model) localScrollbackWheelCmd(tabID string, delta int) tea.Cmd {
	if m == nil || m.workbench == nil || delta == 0 {
		return nil
	}
	if m.mode().Kind == input.ModeDisplay {
		return m.moveCopyCursorVertical(-delta)
	}
	// Align local wheel scroll behavior with tmux/zellij: first upward wheel
	// enters a local scroll/copy mode instead of staying on the live pane path.
	if delta > 0 && m.ensureCopyMode() {
		m.setMode(input.ModeState{Kind: input.ModeDisplay})
		m.render.Invalidate()
		return m.moveCopyCursorVertical(-delta)
	}
	if _, changed := m.workbench.AdjustTabScrollOffset(tabID, delta); changed {
		m.render.Invalidate()
		return m.ensureActivePaneScrollbackCmd()
	}
	return nil
}

func (m *Model) forwardTerminalWheelInputCmd(msg tea.MouseMsg, delta int) tea.Cmd {
	in, ok := m.terminalWheelInputForMouseMsg(msg, delta, 1)
	if !ok {
		return nil
	}
	return terminalWheelInputCmd(in)
}

func (m *Model) forwardAlternateScreenWheelCmd(msg tea.MouseMsg, delta int) tea.Cmd {
	in, ok := m.alternateScreenWheelInputForMouseMsg(msg, delta, 1)
	if !ok {
		return nil
	}
	return terminalWheelInputCmd(in)
}

func (m *Model) terminalWheelInputForMouseMsg(msg tea.MouseMsg, delta, repeat int) (input.TerminalInput, bool) {
	if m == nil || m.workbench == nil {
		return input.TerminalInput{}, false
	}
	if m.mode().Kind == input.ModeDisplay {
		return input.TerminalInput{}, false
	}
	vm := m.renderVM()
	if vm.Overlay.Kind != render.VisibleOverlayNone || vm.Surface.Kind == render.VisibleSurfaceTerminalPool {
		return input.TerminalInput{}, false
	}
	targetPaneID, contentRect, ok := m.activeContentMouseTarget(msg.X, msg.Y)
	if !ok {
		return input.TerminalInput{}, false
	}
	contentMsg := msg
	contentMsg.Y = msg.Y - m.contentOriginY()
	encoded := m.encodeTerminalMouseInput(contentMsg, targetPaneID, contentRect)
	if len(encoded) == 0 {
		return input.TerminalInput{}, false
	}
	return input.TerminalInput{
		Kind:           input.TerminalInputWheel,
		PaneID:         targetPaneID,
		Data:           encoded,
		Repeat:         maxInt(1, repeat),
		WheelDirection: delta,
	}, true
}

func (m *Model) alternateScreenWheelInputForMouseMsg(msg tea.MouseMsg, delta, repeat int) (input.TerminalInput, bool) {
	if m == nil || m.workbench == nil {
		return input.TerminalInput{}, false
	}
	targetPaneID, _, ok := m.activeContentMouseTarget(msg.X, msg.Y)
	if !ok {
		return input.TerminalInput{}, false
	}
	pane := m.activePaneForInput(targetPaneID)
	if pane == nil || pane.TerminalID == "" {
		return input.TerminalInput{}, false
	}
	modes := m.terminalModesForPane(pane)
	if (!modes.AlternateScreen && !modes.AlternateScroll) || modes.MouseTracking {
		return input.TerminalInput{}, false
	}
	encoded := encodeTerminalWheelFallback(msg, modes)
	if len(encoded) == 0 {
		return input.TerminalInput{}, false
	}
	return input.TerminalInput{
		Kind:           input.TerminalInputWheel,
		PaneID:         targetPaneID,
		Data:           encoded,
		Repeat:         maxInt(1, repeat),
		WheelDirection: delta,
	}, true
}

func terminalWheelInputCmd(in input.TerminalInput) tea.Cmd {
	if in.PaneID == "" || len(in.Data) == 0 || in.WheelDirection == 0 {
		return nil
	}
	return func() tea.Msg {
		in.Repeat = maxInt(1, in.Repeat)
		return in
	}
}
