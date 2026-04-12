package app

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/perftrace"
	"github.com/lozzow/termx/tuiv2/input"
)

func (m *Model) handleInteractionMessage(msg tea.Msg) (tea.Cmd, bool) {
	switch typed := msg.(type) {
	case interactionBatchMsg:
		return m.handleInteractionBatch(typed.Messages), true
	case mouseWheelBurstMsg:
		return m.handleMouseWheelBurstMsg(typed), true
	case keyBurstMsg:
		return m.handleKeyBurstMsg(typed), true
	case tea.MouseMsg:
		return m.handleMouseMsg(typed), true
	case tea.KeyMsg:
		return m.handleKeyMsg(typed), true
	case prefixTimeoutMsg:
		if typed.seq == m.prefixSeq && m.isStickyMode() {
			m.setMode(input.ModeState{Kind: input.ModeNormal})
			m.render.Invalidate()
		}
		return nil, true
	case SemanticActionMsg:
		return m.dispatchSemanticActionCmd(typed.Action, true), true
	case input.SemanticAction:
		return m.dispatchSemanticActionCmd(typed, true), true
	case TerminalInputMsg:
		return m.handleTerminalInput(typed.Input), true
	case input.TerminalInput:
		return m.handleTerminalInput(typed), true
	case terminalInputSentMsg:
		next := m.dequeueTerminalInputCmd()
		if typed.err != nil {
			return tea.Batch(m.showError(typed.err), next), true
		}
		return next, true
	case sequenceMsg:
		return m.nextSequenceCmd(typed), true
	case copyModeAutoScrollMsg:
		return m.handleCopyModeAutoScroll(typed.seq), true
	default:
		return nil, false
	}
}

func (m *Model) handleInteractionBatch(messages []tea.Msg) tea.Cmd {
	if len(messages) == 0 {
		return nil
	}
	perftrace.Count("app.interaction.batch", len(messages))
	wasSending := m.terminalInputSending
	prevBatchState := m.interactionBatchActive
	m.interactionBatchActive = true
	defer func() {
		m.interactionBatchActive = prevBatchState
	}()
	cmds := make([]tea.Cmd, 0, len(messages))
	for _, msg := range messages {
		cmd, ok := m.handleInteractionMessage(msg)
		if !ok || cmd == nil {
			continue
		}
		cmds = append(cmds, cmd)
	}
	if !wasSending {
		if cmd := m.dequeueTerminalInputCmd(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return batchCmds(cmds...)
}
