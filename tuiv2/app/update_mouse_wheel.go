package app

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/render"
	"github.com/lozzow/termx/tuiv2/shared"
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
		return m.handleForwardedTerminalWheelInput(in)
	}
	if in, ok := m.alternateScreenWheelInputForMouseMsg(msg, step, repeat); ok {
		return m.handleForwardedTerminalWheelInput(in)
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
		targetPaneID := tab.ActivePaneID
		if floating != nil {
			if tab.ActivePaneID != floating.ID {
				_ = m.workbench.FocusPane(tab.ID, floating.ID)
				m.workbench.ReorderFloatingPane(tab.ID, floating.ID, true)
			}
			targetPaneID = floating.ID
		}
		if cmd := m.localScrollbackWheelCmd(targetPaneID, delta); cmd != nil {
			return cmd
		}
		return nil
	}
	return nil
}

func (m *Model) handleForwardedTerminalWheelInput(in input.TerminalInput) tea.Cmd {
	if m == nil {
		return nil
	}
	if len(in.Data) == 0 {
		return nil
	}
	if m.isPaneAttachPending(in.PaneID) {
		return m.handleTerminalInput(in)
	}
	if m.workbench != nil {
		if pane := m.workbench.ActivePane(); pane != nil && pane.TerminalID == "" {
			return m.openPickerIfUnattached(pane.ID)
		}
	}
	if shared.RemoteLatencyProfileEnabled() {
		if m.terminalInputSending || m.interactionBatchActive || m.terminalInputs.HasPending() {
			return m.handleTerminalInput(in)
		}
		m.terminalInputSending = true
		if m.canDirectSendForwardedWheelInput(in) {
			return m.terminalInputDirectSendCmd(in)
		}
		return m.terminalInputSendCmd(in)
	}
	if m.terminalInputSending || m.interactionBatchActive {
		m.enqueueTerminalInput(in)
		return nil
	}
	if m.canDirectSendForwardedWheelInput(in) {
		return m.terminalInputDirectSendCmd(in)
	}
	return m.terminalInputSendCmd(in)
}

func (m *Model) canDirectSendForwardedWheelInput(in input.TerminalInput) bool {
	if m == nil || m.runtime == nil || m.sessionID != "" || in.Kind != input.TerminalInputWheel || in.PaneID == "" || len(in.Data) == 0 || in.WheelDirection == 0 {
		return false
	}
	target, ok := m.resolveTerminalInteractionTarget(terminalInteractionRequest{PaneID: in.PaneID})
	if !ok || target.terminalID == "" {
		return false
	}
	viewportRect, ok := m.terminalViewportRect(target.paneID, target.rect)
	if !ok {
		return false
	}
	cols := uint16(maxInt(2, viewportRect.W))
	rows := uint16(maxInt(2, viewportRect.H))
	if !m.terminalAlreadySized(target.terminalID, cols, rows) {
		return false
	}
	control := m.runtime.TerminalControlStatus(target.terminalID)
	if control.TerminalID == "" || control.PendingOwnerResize {
		return false
	}
	if len(control.BoundPaneIDs) > 1 && control.OwnerPaneID != target.paneID {
		return false
	}
	return true
}

func (m *Model) localScrollbackWheelCmd(paneID string, delta int) tea.Cmd {
	if m == nil || m.workbench == nil || paneID == "" || delta == 0 {
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
	if _, changed := m.adjustPaneViewportOffset(paneID, delta); changed {
		m.render.Invalidate()
		return m.ensureActivePaneScrollbackCmd()
	}
	return nil
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
