package app

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/perftrace"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/orchestrator"
	"github.com/lozzow/termx/tuiv2/shared"
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
	policy := m.semanticActionPolicy()
	options := effectApplyOptions{}
	if policy != nil {
		options.deferInvalidate = policy.deferInvalidate(action)
	}
	cmd := m.applyEffectsWithOptions(m.enrichEffects(action, m.orchestrator.HandleSemanticAction(action)), options)
	if policy != nil {
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
