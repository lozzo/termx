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
	return m.dispatchResolvedMouseWheel(m.resolveMouseWheelPolicy(msg, repeat))
}

func (m *Model) handleForwardedTerminalWheelInput(in input.TerminalInput) tea.Cmd {
	if m == nil {
		return nil
	}
	if len(in.Data) == 0 {
		return nil
	}
	if normalized, cmd, ok := m.resolveTerminalInputDispatch(in); !ok {
		return cmd
	} else {
		in = normalized
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
