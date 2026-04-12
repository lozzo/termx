package app

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/perftrace"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/orchestrator"
	"github.com/lozzow/termx/tuiv2/render"
	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func (m *Model) Init() tea.Cmd {
	var initCmd tea.Cmd
	if err := m.bootstrapStartup(); err != nil {
		return func() tea.Msg { return err }
	}
	if m.cfg.AttachID != "" {
		initCmd = m.attachInitialTerminalCmd(m.cfg.AttachID)
	} else if len(m.startup.PanesToReattach) > 0 {
		initCmd = m.reattachRestoredPanesCmd(m.startup.PanesToReattach)
	} else if m.modalHost != nil && m.modalHost.Session != nil {
		// If startup opened a picker, immediately load the terminal list.
		initCmd = m.applyEffects([]orchestrator.Effect{orchestrator.LoadPickerItemsEffect{}})
	}
	return batchCmds(initCmd, m.hostEmojiProbeCmd(1, hostEmojiProbeRetryDelay))
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	finish := perftrace.Measure("app.update")
	defer finish(0)
	if cmd, ok := m.handleInteractionMessage(msg); ok {
		return m, cmd
	}
	if cmd, ok := m.handleUIStateMessage(msg); ok {
		return m, cmd
	}
	if cmd, ok := m.handleTerminalEventMessage(msg); ok {
		return m, cmd
	}
	if cmd, ok := m.handleSessionMessage(msg); ok {
		return m, cmd
	}
	if cmd, ok := m.handleLifecycleMessage(msg); ok {
		return m, cmd
	}
	return m, nil
}

func (m *Model) hostEmojiProbeCmd(attempt int, delay time.Duration) tea.Cmd {
	if m == nil || !m.hostEmojiProbePending || m.cursorOut == nil || attempt <= 0 {
		return nil
	}
	return tea.Tick(delay, func(time.Time) tea.Msg {
		return hostEmojiProbeMsg{Attempt: attempt}
	})
}

func hostEmojiProbeGiveUpCmd(delay time.Duration) tea.Cmd {
	if delay <= 0 {
		return func() tea.Msg { return hostEmojiProbeGiveUpMsg{} }
	}
	return tea.Tick(delay, func(time.Time) tea.Msg {
		return hostEmojiProbeGiveUpMsg{}
	})
}

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

func (m *Model) handleTerminalEventMessage(msg tea.Msg) (tea.Cmd, bool) {
	switch typed := msg.(type) {
	case terminalEventMsg:
		switch typed.Event.Type {
		case protocol.EventTerminalResized:
			if m == nil || m.runtime == nil || typed.Event.TerminalID == "" {
				return nil, true
			}
			terminal := m.runtime.Registry().Get(typed.Event.TerminalID)
			if terminal == nil {
				return nil, true
			}
			// When a stream is active, the in-band resize frame already
			// updated the local VTerm dimensions.  Reloading the snapshot
			// here would race with the stream — the snapshot is sampled at
			// a single point in time and loading it resets the VTerm,
			// discarding any output the stream delivered between the
			// snapshot sample and the load.  This creates holes in the
			// cell grid that persist until htop redraws those areas.
			if terminal.Stream.Active {
				return nil, true
			}
			return m.reloadTerminalSnapshotCmd(typed.Event.TerminalID), true
		default:
			return nil, true
		}
	default:
		return nil, false
	}
}

func hostEmojiProbeModeFromReportedColumn(x int) (shared.AmbiguousEmojiVariationSelectorMode, bool) {
	switch x {
	case 1:
		// 中文说明：宿主只前进 1 列，说明不能安全保留原始 FE0F emoji；
		// 渲染端要走 strip 回退，显式补一个空格把模型宽度补齐。
		return shared.AmbiguousEmojiVariationSelectorStrip, true
	case 2:
		// 中文说明：宿主已经按 2 列推进，可以直接保留原始 grapheme。
		return shared.AmbiguousEmojiVariationSelectorRaw, true
	default:
		return "", false
	}
}

func (m *Model) handleSessionMessage(msg tea.Msg) (tea.Cmd, bool) {
	switch typed := msg.(type) {
	case sessionSnapshotMsg:
		if shouldApplySessionSnapshot(typed.Snapshot) {
			m.applySessionSnapshot(typed.Snapshot)
		}
		if typed.Err != nil {
			return m.showError(typed.Err), true
		}
		return nil, true
	case sessionEventMsg:
		switch typed.Event.Type {
		case protocol.EventSessionDeleted:
			if typed.Event.SessionID == m.sessionID {
				return m.showError(fmt.Errorf("session %s was deleted", m.sessionID)), true
			}
		case protocol.EventSessionCreated, protocol.EventSessionUpdated:
			if typed.Event.SessionID == m.sessionID {
				revision := uint64(0)
				viewID := ""
				if typed.Event.Session != nil {
					revision = typed.Event.Session.Revision
					viewID = typed.Event.Session.ViewID
				}
				if viewID != m.sessionViewID && revision >= m.sessionRevision {
					return m.pullSessionCmd(), true
				}
			}
		}
		return nil, true
	case sessionViewUpdatedMsg:
		if typed.View != nil && typed.View.ViewID != "" {
			m.sessionViewID = typed.View.ViewID
		}
		if typed.Err != nil {
			return m.showError(typed.Err), true
		}
		return nil, true
	default:
		return nil, false
	}
}

func (m *Model) handleLifecycleMessage(msg tea.Msg) (tea.Cmd, bool) {
	switch typed := msg.(type) {
	case InvalidateMsg:
		m.invalidatePending.Store(false)
		m.render.Invalidate()
		if m.invalidateDeferred.Swap(false) {
			m.queueInvalidate()
		}
		return m.maybeAutoFitFloatingPanesCmd(), true
	case RenderTickMsg:
		if m.render != nil {
			m.render.AdvanceCursorBlink()
		}
		return nil, true
	case tea.WindowSizeMsg:
		oldBodyRect := m.bodyRect()
		newBodyRect := workbench.Rect{W: maxInt(1, typed.Width), H: render.FrameBodyHeight(typed.Height)}
		if m.workbench != nil {
			if m.width > 0 && m.height > 0 {
				m.workbench.ReflowFloatingPanes(oldBodyRect, newBodyRect)
			} else {
				m.workbench.ClampFloatingPanesToBounds(newBodyRect)
			}
		}
		m.width = typed.Width
		m.height = typed.Height
		m.render.Invalidate()
		return batchCmds(m.resizeVisiblePanesCmd(), m.resizePendingPaneResizesCmd(), m.maybeAutoFitFloatingPanesCmd(), m.updateSessionViewCmd()), true
	case error:
		return m.showError(typed), true
	default:
		return nil, false
	}
}

func shouldApplySessionSnapshot(snapshot *protocol.SessionSnapshot) bool {
	if snapshot == nil {
		return false
	}
	return snapshot.Session.ID != "" || snapshot.Workbench != nil || snapshot.View != nil || len(snapshot.Leases) > 0
}

func (m *Model) dispatchSemanticActionCmd(action input.SemanticAction, allowLocal bool) tea.Cmd {
	if allowLocal {
		if handled, cmd := m.handleLocalAction(action); handled {
			return batchCmds(cmd, m.resizePendingPaneResizesCmd(), m.updateSessionViewCmd())
		}
	}
	if handled, cmd := m.handleModalAction(action); handled {
		return batchCmds(cmd, m.resizePendingPaneResizesCmd(), m.updateSessionViewCmd())
	}
	if m.blocksSemanticActionForTerminalSizeLock(action) {
		return batchCmds(m.showNotice(terminalSizeLockedNotice), m.resizePendingPaneResizesCmd(), m.updateSessionViewCmd())
	}
	cmd := m.semanticActionEffectsCmd(action)
	if m.isStickyMode() {
		cmd = tea.Batch(cmd, m.rearmPrefixTimeoutCmd())
	}
	return batchCmds(cmd, m.resizePendingPaneResizesCmd(), m.updateSessionViewCmd())
}

func (m *Model) semanticActionEffectsCmd(action input.SemanticAction) tea.Cmd {
	if m == nil || m.orchestrator == nil {
		return nil
	}
	cmd := m.applyEffects(m.enrichEffects(action, m.orchestrator.HandleSemanticAction(action)))
	if policy := m.semanticActionPolicy(); policy != nil {
		return policy.postEffectsCmd(action, cmd)
	}
	return cmd
}

func (m *Model) showError(err error) tea.Cmd {
	if m == nil {
		return nil
	}
	m.errorSeq++
	m.err = err
	m.render.Invalidate()
	return clearErrorCmd(m.errorSeq)
}

func (m *Model) handleKeyMsg(msg tea.KeyMsg) tea.Cmd {
	if m != nil && m.input != nil {
		if result := m.input.TryRepeatedPassthrough(msg); result.TerminalInput != nil {
			inputMsg := *result.TerminalInput
			if inputMsg.PaneID == "" && m.workbench != nil {
				if pane := m.workbench.ActivePane(); pane != nil {
					inputMsg.PaneID = pane.ID
				}
			}
			if encoded := m.encodeActiveTerminalInput(msg, inputMsg.PaneID); len(encoded) > 0 {
				inputMsg.Data = encoded
			}
			return m.handleTerminalInput(inputMsg)
		}
	}
	if handled, cmd := m.handleModalKeyMsg(msg); handled {
		return cmd
	}
	if handled, cmd := m.handleEmptyPaneKeyMsg(msg); handled {
		return cmd
	}
	if handled, cmd := m.handleExitedPaneKeyMsg(msg); handled {
		return cmd
	}
	if action, ok := m.restartActionForKeyMsg(msg); ok {
		return func() tea.Msg { return action }
	}
	result := m.input.RouteKeyMsg(msg)
	if result.Action != nil {
		action := *result.Action
		if m.modalHost != nil && m.modalHost.Session != nil && m.modalHost.Session.Kind == input.ModePicker && m.modalHost.Picker != nil {
			if selected := m.modalHost.Picker.SelectedItem(); selected != nil && action.Kind == input.ActionSubmitPrompt {
				action.TargetID = selected.TerminalID
			}
		}
		if action.PaneID == "" && m.workbench != nil {
			if pane := m.workbench.ActivePane(); pane != nil {
				action.PaneID = pane.ID
			}
		}
		return func() tea.Msg { return action }
	}
	if result.TerminalInput != nil {
		inputMsg := *result.TerminalInput
		if inputMsg.PaneID == "" && m.workbench != nil {
			if pane := m.workbench.ActivePane(); pane != nil {
				inputMsg.PaneID = pane.ID
			}
		}
		if encoded := m.encodeActiveTerminalInput(msg, inputMsg.PaneID); len(encoded) > 0 {
			inputMsg.Data = encoded
		}
		return m.handleTerminalInput(inputMsg)
	}
	return nil
}

func (m *Model) restartActionForKeyMsg(msg tea.KeyMsg) (input.SemanticAction, bool) {
	if m == nil || m.input == nil || m.mode().Kind != input.ModeNormal {
		return input.SemanticAction{}, false
	}
	if msg.Type != tea.KeyRunes || len(msg.Runes) != 1 || msg.Runes[0] != 'R' {
		return input.SemanticAction{}, false
	}
	if m.workbench == nil || m.runtime == nil {
		return input.SemanticAction{}, false
	}
	pane := m.workbench.ActivePane()
	if pane == nil || pane.ID == "" || pane.TerminalID == "" {
		return input.SemanticAction{}, false
	}
	terminal := m.runtime.Registry().Get(pane.TerminalID)
	if terminal == nil || terminal.State != "exited" {
		return input.SemanticAction{}, false
	}
	return input.SemanticAction{Kind: input.ActionRestartTerminal, PaneID: pane.ID}, true
}
