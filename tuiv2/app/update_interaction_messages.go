package app

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/perftrace"
	"github.com/lozzow/termx/tuiv2/input"
	localvterm "github.com/lozzow/termx/vterm"
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
		if next == nil {
			m.scheduleSharedTerminalSnapshotResync(typed.paneID, typed.terminalID)
			return func() tea.Msg { return InvalidateMsg{} }, true
		}
		return next, true
	case sharedTerminalSnapshotResyncMsg:
		if typed.seq != m.terminalResyncSeq {
			return nil, true
		}
		if m.terminalInputSending || len(m.pendingTerminalInputs) > 0 {
			return nil, true
		}
		return m.sharedTerminalSnapshotResyncCmd(typed.terminalID), true
	case sequenceMsg:
		return m.nextSequenceCmd(typed), true
	case copyModeAutoScrollMsg:
		return m.handleCopyModeAutoScroll(typed.seq), true
	default:
		return nil, false
	}
}

func (m *Model) scheduleSharedTerminalSnapshotResync(paneID, terminalID string) {
	if !m.shouldResyncSharedTerminalSnapshot(paneID, terminalID) {
		return
	}
	m.terminalResyncSeq++
	seq := m.terminalResyncSeq
	msg := sharedTerminalSnapshotResyncMsg{
		seq:        seq,
		paneID:     paneID,
		terminalID: terminalID,
	}
	if sharedTerminalSnapshotResyncDelay <= 0 {
		m.sendAsync(msg)
		return
	}
	time.AfterFunc(sharedTerminalSnapshotResyncDelay, func() {
		if m == nil {
			return
		}
		m.sendAsync(msg)
	})
}

func (m *Model) shouldResyncSharedTerminalSnapshot(paneID, terminalID string) bool {
	if m == nil || m.runtime == nil || m.runtime.Client() == nil || terminalID == "" {
		return false
	}
	terminal := m.runtime.Registry().Get(terminalID)
	if terminal == nil || len(terminal.BoundPaneIDs) < 2 {
		return false
	}
	modes := localvterm.TerminalModes{}
	switch {
	case terminal.VTerm != nil:
		modes = terminal.VTerm.Modes()
	case terminal.Snapshot != nil:
		modes = localvterm.TerminalModes{
			AlternateScreen: terminal.Snapshot.Modes.AlternateScreen,
			MouseTracking:   terminal.Snapshot.Modes.MouseTracking,
			BracketedPaste:  terminal.Snapshot.Modes.BracketedPaste,
		}
	}
	if !modes.AlternateScreen && !modes.MouseTracking {
		return false
	}
	if paneID != "" {
		if pane, _, ok := m.visiblePaneForInput(paneID); !ok || pane == nil || pane.TerminalID != terminalID {
			return false
		}
	}
	return true
}

func (m *Model) sharedTerminalSnapshotResyncCmd(terminalID string) tea.Cmd {
	if m == nil || m.runtime == nil || m.runtime.Client() == nil || terminalID == "" {
		return nil
	}
	return func() tea.Msg {
		if _, err := m.runtime.LoadSnapshot(context.Background(), terminalID, 0, defaultTerminalSnapshotScrollbackLimit); err != nil {
			return err
		}
		return InvalidateMsg{}
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
