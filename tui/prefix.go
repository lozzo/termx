package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	uv "github.com/charmbracelet/ultraviolet"
)

func (m *Model) handlePrefixEvent(event uv.KeyPressEvent) tea.Cmd {
	return m.handlePrefixInput(prefixInputFromEvent(event))
}

func panePrefixActionForEvent(event uv.KeyPressEvent) panePrefixAction {
	return panePrefixActionForInput(prefixInputFromEvent(event))
}

func (m *Model) handleActivePrefixEvent(event uv.KeyPressEvent) tea.Cmd {
	return m.applyActivePrefixResult(m.dispatchPrefixEvent(event))
}

func (m *Model) dispatchPrefixEvent(event uv.KeyPressEvent) prefixDispatchResult {
	return m.dispatchPrefixInput(prefixInputFromEvent(event))
}

func (m *Model) modeEventResult(cmd tea.Cmd, keep bool) prefixDispatchResult {
	if m.directMode {
		return prefixDispatchResult{cmd: cmd, keep: true}
	}
	return prefixDispatchResult{cmd: cmd, keep: keep, rearm: keep}
}

func (m *Model) dispatchPaneModeEvent(event uv.KeyPressEvent) prefixDispatchResult {
	return m.dispatchPaneModeInput(prefixInputFromEvent(event))
}

func (m *Model) dispatchResizeModeEvent(event uv.KeyPressEvent) prefixDispatchResult {
	return m.dispatchResizeModeInput(prefixInputFromEvent(event))
}

func (m *Model) dispatchTabSubPrefixEvent(event uv.KeyPressEvent) prefixDispatchResult {
	return m.dispatchTabSubPrefixInput(prefixInputFromEvent(event))
}

func (m *Model) dispatchWorkspaceSubPrefixEvent(event uv.KeyPressEvent) prefixDispatchResult {
	return m.dispatchWorkspaceSubPrefixInput(prefixInputFromEvent(event))
}

func (m *Model) dispatchViewportSubPrefixEvent(event uv.KeyPressEvent) prefixDispatchResult {
	return m.dispatchViewportSubPrefixInput(prefixInputFromEvent(event))
}

func (m *Model) dispatchFloatingModeEvent(event uv.KeyPressEvent) prefixDispatchResult {
	return m.dispatchFloatingModeInput(prefixInputFromEvent(event))
}

func (m *Model) dispatchOffsetPanModeEvent(event uv.KeyPressEvent) prefixDispatchResult {
	return m.dispatchOffsetPanModeInput(prefixInputFromEvent(event))
}

func (m *Model) dispatchGlobalModeEvent(event uv.KeyPressEvent) prefixDispatchResult {
	return m.dispatchGlobalModeInput(prefixInputFromEvent(event))
}

func (m *Model) clearPrefixState() {
	_ = m.applyPrefixStateTransition(prefixStateTransition{kind: prefixStateTransitionClear})
}

func (m *Model) prefixFallbackCmd(fallback prefixFallback) tea.Cmd {
	switch fallback {
	case prefixFallbackFloatingCreate:
		return m.openFloatingTerminalPickerCmd(m.workspace.ActiveTab)
	default:
		return nil
	}
}

func (m *Model) enterPrefixMode(mode prefixMode, fallback prefixFallback) tea.Cmd {
	return m.applyPrefixStateTransition(prefixStateTransition{
		kind:     prefixStateTransitionEnter,
		mode:     mode,
		fallback: fallback,
	})
}

func isStickyPrefixMode(mode prefixMode) bool {
	return mode == prefixModeFloating || mode == prefixModeOffsetPan
}

func (m *Model) enterDirectMode(mode prefixMode) tea.Cmd {
	return m.applyPrefixStateTransition(prefixStateTransition{
		kind:   prefixStateTransitionEnter,
		mode:   mode,
		direct: true,
	})
}

func (m *Model) applyPrefixStateTransition(transition prefixStateTransition) tea.Cmd {
	switch transition.kind {
	case prefixStateTransitionClear:
		m.prefixActive = false
		m.directMode = false
		m.prefixMode = prefixModeRoot
		m.prefixFallback = prefixFallbackNone
		m.invalidateRender()
		return nil
	case prefixStateTransitionEnter:
		m.prefixActive = true
		m.directMode = transition.direct
		m.prefixMode = transition.mode
		m.prefixFallback = transition.fallback
		if transition.direct {
			m.prefixSeq++
		}
		if transition.mode == prefixModeFloating {
			if tab := m.currentTab(); tab != nil {
				floating := m.visibleFloatingPanes(tab)
				if len(floating) > 0 && !isFloatingPane(tab, tab.ActivePaneID) {
					tab.ActivePaneID = floating[len(floating)-1].PaneID
				}
			}
		}
		m.invalidateRender()
		return m.armPrefixTimeout()
	default:
		return nil
	}
}

func (m *Model) armPrefixTimeout() tea.Cmd {
	if !m.prefixActive || m.prefixTimeout <= 0 {
		return nil
	}
	m.prefixSeq++
	seq := m.prefixSeq
	timeout := m.prefixTimeout
	return tea.Tick(timeout, func(time.Time) tea.Msg {
		return prefixTimeoutMsg{seq: seq}
	})
}

func prefixRuntimePlanForResult(result prefixDispatchResult) prefixRuntimePlan {
	plan := prefixRuntimePlan{
		transition: result.state,
		cmd:        result.cmd,
	}
	switch {
	case result.state.kind != prefixStateTransitionNone:
		return plan
	case !result.keep:
		plan.clear = true
	default:
		plan.rearm = result.rearm
	}
	return plan
}

func (m *Model) applyPrefixRuntimePlan(plan prefixRuntimePlan) tea.Cmd {
	cmds := make([]tea.Cmd, 0, 3)
	switch {
	case plan.transition.kind != prefixStateTransitionNone:
		cmds = append(cmds, m.applyPrefixStateTransition(plan.transition))
	case plan.clear:
		cmds = append(cmds, m.applyPrefixStateTransition(prefixStateTransition{kind: prefixStateTransitionClear}))
	default:
		m.invalidateRender()
		if plan.rearm {
			cmds = append(cmds, m.armPrefixTimeout())
		}
	}
	cmds = append(cmds, plan.cmd)
	return batchTeaCmds(cmds...)
}

func (m *Model) applyActivePrefixResult(result prefixDispatchResult) tea.Cmd {
	return m.applyPrefixRuntimePlan(prefixRuntimePlanForResult(result))
}

func (m *Model) handleActivePrefixKey(msg tea.KeyMsg) tea.Cmd {
	return m.applyActivePrefixResult(m.dispatchPrefixKey(msg))
}

func (m *Model) prefixIntentForInput(input prefixInput) prefixIntent {
	return prefixIntent{
		mode:   m.prefixMode,
		direct: m.directMode,
		input:  input,
	}
}

func (m *Model) dispatchPrefixIntent(intent prefixIntent) prefixDispatchResult {
	switch intent.mode {
	case prefixModePane:
		return m.dispatchPaneModeInput(intent.input)
	case prefixModeResize:
		return m.applyResizeModeAction(resizeModeActionForInput(intent.input))
	case prefixModeTab:
		return m.applyTabModeAction(tabModeActionForInput(intent.input, intent.direct))
	case prefixModeWorkspace:
		return m.applyWorkspaceModeAction(workspaceModeActionForInput(intent.input))
	case prefixModeViewport:
		return m.applyViewportModeAction(viewportModeActionForInput(intent.input, intent.direct))
	case prefixModeFloating:
		return m.applyFloatingModeAction(floatingModeActionForInput(intent.input))
	case prefixModeOffsetPan:
		return m.applyOffsetPanModeAction(offsetPanModeActionForInput(intent.input))
	case prefixModeGlobal:
		return m.applyGlobalModeAction(globalModeActionForInput(intent.input))
	default:
		return m.dispatchRootPrefixInput(intent.input)
	}
}

func (m *Model) dispatchPrefixInput(input prefixInput) prefixDispatchResult {
	return m.dispatchPrefixIntent(m.prefixIntentForInput(input))
}

func (m *Model) dispatchPrefixKey(msg tea.KeyMsg) prefixDispatchResult {
	return m.dispatchPrefixInput(prefixInputFromKey(msg))
}

func (m *Model) dispatchRootPrefixInput(input prefixInput) prefixDispatchResult {
	if result, ok := m.rootPrefixShortcutResultForInput(input); ok {
		return result
	}
	return m.prefixCommandResult(m.handlePrefixInput(input), input, false)
}

func (m *Model) dispatchRootPrefixKey(msg tea.KeyMsg) prefixDispatchResult {
	return m.dispatchRootPrefixInput(prefixInputFromKey(msg))
}

func shouldKeepPrefixInput(input prefixInput) bool {
	if input.alt {
		switch input.token {
		case "h", "j", "k", "l", "H", "J", "K", "L", "left", "right", "up", "down":
			return true
		}
	}
	switch input.token {
	case "left", "right", "up", "down",
		"ctrl+left", "ctrl+right", "ctrl+up", "ctrl+down",
		"ctrl+h", "ctrl+j", "ctrl+k", "ctrl+l",
		"h", "j", "k", "l", "H", "J", "K", "L":
		return true
	}
	return false
}

func shouldKeepPrefixKey(msg tea.KeyMsg) bool {
	return shouldKeepPrefixInput(prefixInputFromKey(msg))
}

func (m *Model) modeResult(cmd tea.Cmd, keep bool) prefixDispatchResult {
	return prefixDispatchResult{cmd: cmd, keep: keep, rearm: keep}
}

func (m *Model) prefixCommandResult(cmd tea.Cmd, input prefixInput, directKeepOnNoop bool) prefixDispatchResult {
	keep := shouldKeepPrefixInput(input)
	if cmd == nil && !keep && directKeepOnNoop {
		return prefixDispatchResult{keep: true}
	}
	return m.modeResult(cmd, keep)
}

func (m *Model) dispatchPaneModeInput(input prefixInput) prefixDispatchResult {
	if input.token == "esc" {
		return prefixDispatchResult{}
	}
	return m.prefixCommandResult(m.handlePrefixInput(input), input, m.directMode)
}

func (m *Model) dispatchPaneModeKey(msg tea.KeyMsg) prefixDispatchResult {
	return m.dispatchPaneModeInput(prefixInputFromKey(msg))
}

func (m *Model) dispatchResizeModeInput(input prefixInput) prefixDispatchResult {
	return m.applyResizeModeAction(resizeModeActionForInput(input))
}

func (m *Model) dispatchResizeModeKey(msg tea.KeyMsg) prefixDispatchResult {
	return m.dispatchResizeModeInput(prefixInputFromKey(msg))
}

func (m *Model) dispatchTabSubPrefixInput(input prefixInput) prefixDispatchResult {
	return m.applyTabModeAction(tabModeActionForInput(input, m.directMode))
}

func (m *Model) dispatchTabSubPrefixKey(msg tea.KeyMsg) prefixDispatchResult {
	return m.dispatchTabSubPrefixInput(prefixInputFromKey(msg))
}

func (m *Model) dispatchWorkspaceSubPrefixInput(input prefixInput) prefixDispatchResult {
	return m.applyWorkspaceModeAction(workspaceModeActionForInput(input))
}

func (m *Model) dispatchWorkspaceSubPrefixKey(msg tea.KeyMsg) prefixDispatchResult {
	return m.dispatchWorkspaceSubPrefixInput(prefixInputFromKey(msg))
}

func (m *Model) dispatchViewportSubPrefixInput(input prefixInput) prefixDispatchResult {
	return m.applyViewportModeAction(viewportModeActionForInput(input, m.directMode))
}

func (m *Model) dispatchViewportSubPrefixKey(msg tea.KeyMsg) prefixDispatchResult {
	return m.dispatchViewportSubPrefixInput(prefixInputFromKey(msg))
}

func (m *Model) dispatchFloatingModeInput(input prefixInput) prefixDispatchResult {
	return m.applyFloatingModeAction(floatingModeActionForInput(input))
}

func (m *Model) dispatchFloatingModeKey(msg tea.KeyMsg) prefixDispatchResult {
	return m.dispatchFloatingModeInput(prefixInputFromKey(msg))
}

func (m *Model) dispatchOffsetPanModeInput(input prefixInput) prefixDispatchResult {
	return m.applyOffsetPanModeAction(offsetPanModeActionForInput(input))
}

func (m *Model) dispatchOffsetPanModeKey(msg tea.KeyMsg) prefixDispatchResult {
	return m.dispatchOffsetPanModeInput(prefixInputFromKey(msg))
}

func (m *Model) dispatchGlobalModeInput(input prefixInput) prefixDispatchResult {
	return m.applyGlobalModeAction(globalModeActionForInput(input))
}

func (m *Model) dispatchGlobalModeKey(msg tea.KeyMsg) prefixDispatchResult {
	return m.dispatchGlobalModeInput(prefixInputFromKey(msg))
}

func (m *Model) handlePrefixKey(msg tea.KeyMsg) tea.Cmd {
	return m.handlePrefixInput(prefixInputFromKey(msg))
}

func (m *Model) handlePrefixInput(input prefixInput) tea.Cmd {
	if action, ok := floatingAltActionForInput(input); ok {
		return m.applyFloatingModeAction(action).cmd
	}
	return m.applyPanePrefixAction(panePrefixActionForInput(input))
}

func batchTeaCmds(cmds ...tea.Cmd) tea.Cmd {
	filtered := make([]tea.Cmd, 0, len(cmds))
	for _, cmd := range cmds {
		if cmd != nil {
			filtered = append(filtered, cmd)
		}
	}
	switch len(filtered) {
	case 0:
		return nil
	case 1:
		return filtered[0]
	default:
		return tea.Batch(filtered...)
	}
}

func (m *Model) activatePrefix() tea.Cmd {
	m.prefixActive = true
	m.prefixMode = prefixModeRoot
	m.prefixFallback = prefixFallbackNone
	m.invalidateRender()
	return m.armPrefixTimeout()
}
