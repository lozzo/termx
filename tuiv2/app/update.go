package app

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
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

func (m *Model) handleUIStateMessage(msg tea.Msg) (tea.Cmd, bool) {
	switch typed := msg.(type) {
	case pickerItemsLoadedMsg:
		if m.modalHost != nil {
			if m.modalHost.Picker == nil {
				m.modalHost.Picker = &modal.PickerState{}
			}
			m.modalHost.Picker.Items = typed.Items
			m.modalHost.Picker.ApplyFilter()
			if m.modalHost.Picker.Selected >= len(m.modalHost.Picker.VisibleItems()) {
				m.modalHost.Picker.Selected = 0
			}
			if m.modalHost.Session != nil {
				m.markModalReady(m.modalHost.Session.Kind, m.modalHost.Session.RequestID)
			}
		}
		m.render.Invalidate()
		return nil, true
	case terminalManagerItemsLoadedMsg:
		if m.terminalPage == nil {
			m.openTerminalPool()
		}
		m.terminalPage.Items = typed.Items
		m.terminalPage.ApplyFilter()
		m.render.Invalidate()
		return nil, true
	case orchestrator.KillTerminalEffect:
		return m.effectCmd(typed), true
	case EffectAppliedMsg:
		m.applyEffectSideState(typed.Effect)
		return nil, true
	case orchestrator.TerminalAttachedMsg:
		m.clearPendingPaneAttach(typed.PaneID, typed.TerminalID)
		m.resetPaneScrollOffset(typed.TabID, typed.PaneID)
		if m.modalHost != nil && m.modalHost.Session != nil && m.modalHost.Session.Kind == input.ModePicker {
			m.closeModal(input.ModePicker, m.modalHost.Session.RequestID, input.ModeState{Kind: input.ModeNormal})
		}
		m.render.Invalidate()
		return batchCmds(m.saveStateCmd(), m.finalizeTerminalAttachCmd(typed.TabID, typed.PaneID, typed.TerminalID)), true
	case terminalAttachReadyMsg:
		return m.dequeueTerminalInputCmd(), true
	case orchestrator.SnapshotLoadedMsg:
		m.adjustCopyModeAfterSnapshotLoaded(typed.TerminalID)
		m.render.Invalidate()
		return m.maybeAutoFitFloatingPanesCmd(), true
	case hostDefaultColorsMsg:
		if m.runtime != nil {
			m.runtime.SetHostDefaultColors(typed.FG, typed.BG)
		}
		return nil, true
	case hostPaletteColorMsg:
		if m.runtime != nil {
			m.runtime.SetHostPaletteColor(typed.Index, typed.Color)
		}
		return nil, true
	case hostEmojiProbeMsg:
		if m.runtime == nil || !m.hostEmojiProbePending || m.cursorOut == nil || typed.Attempt <= 0 {
			return nil, true
		}
		m.debugLog("host_emoji_probe_send", "attempt", typed.Attempt)
		if err := m.cursorOut.WriteControlSequence(hostEmojiVariationProbeSequence); err != nil {
			m.debugLog("host_emoji_probe_write_failed", "attempt", typed.Attempt, "err", err)
			m.hostEmojiProbePending = false
			return nil, true
		}
		if typed.Attempt >= hostEmojiProbeMaxAttempts {
			return hostEmojiProbeGiveUpCmd(hostEmojiProbeRetryDelay), true
		}
		return m.hostEmojiProbeCmd(typed.Attempt+1, hostEmojiProbeRetryDelay), true
	case hostEmojiProbeGiveUpMsg:
		if !m.hostEmojiProbePending {
			return nil, true
		}
		m.debugLog("host_emoji_probe_give_up")
		m.hostEmojiProbePending = false
		// The host terminal did not respond to the DECXCPR probe (e.g.
		// macOS Terminal.app).  Fall back to raw mode so the emoji is
		// preserved as-is (♻️ instead of degrading to ♻).  The
		// continuation-space compensation in drawProtocolRowInRect
		// handles the case where the host only advances one column.
		if m.runtime != nil {
			m.runtime.SetHostAmbiguousEmojiVariationSelectorMode(shared.AmbiguousEmojiVariationSelectorRaw)
		}
		return nil, true
	case hostCursorPositionMsg:
		if m.runtime == nil || !m.hostEmojiProbePending {
			return nil, true
		}
		mode, ok := hostEmojiProbeModeFromReportedColumn(typed.X)
		if !ok {
			// DECXCPR can arrive late and report whatever column the host cursor had
			// reached by the time the terminal flushed the response. Only exact
			// one- and two-column answers are trustworthy for the ambiguous FE0F
			// width probe; anything else must be ignored so we can retry.
			m.debugLog("host_emoji_probe_response_ignored", "x", typed.X, "y", typed.Y)
			return nil, true
		}
		m.hostEmojiProbePending = false
		// Only the reported column matters for this probe. Some terminals answer
		// from a non-origin row after alt-screen transitions or delayed paints, so
		// rejecting non-zero Y would leave us stuck in the conservative fallback.
		m.debugLog("host_emoji_probe_response", "x", typed.X, "y", typed.Y, "mode", mode)
		m.runtime.SetHostAmbiguousEmojiVariationSelectorMode(mode)
		return nil, true
	case reattachFailedMsg:
		return m.openPickerIfUnattached(typed.paneID), true
	case clearErrorMsg:
		if typed.seq != m.errorSeq {
			return nil, true
		}
		m.err = nil
		m.render.Invalidate()
		return nil, true
	case clearOwnerConfirmMsg:
		if typed.seq != m.ownerSeq {
			return nil, true
		}
		m.ownerConfirmPaneID = ""
		m.render.Invalidate()
		return nil, true
	case clearNoticeMsg:
		if typed.seq != m.noticeSeq {
			return nil, true
		}
		m.notice = ""
		m.render.Invalidate()
		return nil, true
	case terminalTitleMsg:
		m.render.Invalidate()
		return nil, true
	default:
		return nil, false
	}
}

func hostEmojiProbeModeFromReportedColumn(x int) (shared.AmbiguousEmojiVariationSelectorMode, bool) {
	switch x {
	case 1:
		return shared.AmbiguousEmojiVariationSelectorAdvance, true
	case 2:
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
	cmd := m.applyEffects(m.enrichEffects(action, m.orchestrator.HandleSemanticAction(action)))
	cmd = batchCmds(cmd, m.resizeCmdForAction(action), m.saveCmdForAction(action))
	if m.isStickyMode() {
		cmd = tea.Batch(cmd, m.rearmPrefixTimeoutCmd())
	}
	return batchCmds(cmd, m.resizePendingPaneResizesCmd(), m.updateSessionViewCmd())
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
