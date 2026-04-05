package app

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/orchestrator"
	"github.com/lozzow/termx/tuiv2/render"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func (m *Model) Init() tea.Cmd {
	if err := m.bootstrapStartup(); err != nil {
		return func() tea.Msg { return err }
	}
	if m.cfg.AttachID != "" {
		return m.attachInitialTerminalCmd(m.cfg.AttachID)
	}
	if len(m.startup.PanesToReattach) > 0 {
		return m.reattachRestoredPanesCmd(m.startup.PanesToReattach)
	}
	// If startup opened a picker, immediately load the terminal list.
	if m.modalHost != nil && m.modalHost.Session != nil {
		return m.applyEffects([]orchestrator.Effect{orchestrator.LoadPickerItemsEffect{}})
	}
	return nil
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch typed := msg.(type) {
	case tea.MouseMsg:
		return m, m.handleMouseMsg(typed)
	case tea.KeyMsg:
		return m, m.handleKeyMsg(typed)
	case prefixTimeoutMsg:
		if typed.seq == m.prefixSeq && m.isStickyMode() {
			m.input.SetMode(input.ModeState{Kind: input.ModeNormal})
			m.render.Invalidate()
		}
		return m, nil
	case SemanticActionMsg:
		return m, m.dispatchSemanticActionCmd(typed.Action, true)
	case input.SemanticAction:
		return m, m.dispatchSemanticActionCmd(typed, true)
	case TerminalInputMsg:
		return m, m.handleTerminalInput(typed.Input)
	case input.TerminalInput:
		return m, m.handleTerminalInput(typed)
	case terminalInputSentMsg:
		next := m.dequeueTerminalInputCmd()
		if typed.err != nil {
			return m, tea.Batch(m.showError(typed.err), next)
		}
		return m, next
	case sequenceMsg:
		return m, m.nextSequenceCmd(typed)
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
				m.modalHost.MarkReady(m.modalHost.Session.Kind, m.modalHost.Session.RequestID)
			}
		}
		m.render.Invalidate()
		return m, nil
	case terminalManagerItemsLoadedMsg:
		if m.terminalPage == nil {
			m.terminalPage = &modal.TerminalManagerState{}
		}
		m.terminalPage.Items = typed.Items
		m.terminalPage.ApplyFilter()
		m.render.Invalidate()
		return m, nil
	case orchestrator.KillTerminalEffect:
		return m, m.effectCmd(typed)
	case EffectAppliedMsg:
		m.applyEffectSideState(typed.Effect)
		return m, nil
	case orchestrator.TerminalAttachedMsg:
		if m.modalHost != nil && m.modalHost.Session != nil && m.modalHost.Session.Kind == input.ModePicker {
			m.modalHost.Close(input.ModePicker, m.modalHost.Session.RequestID)
			m.input.SetMode(input.ModeState{Kind: input.ModeNormal})
		}
		m.render.Invalidate()
		return m, m.saveStateCmd()
	case orchestrator.SnapshotLoadedMsg:
		m.render.Invalidate()
		return m, m.maybeAutoFitFloatingPanesCmd()
	case hostDefaultColorsMsg:
		if m.runtime != nil {
			m.runtime.SetHostDefaultColors(typed.FG, typed.BG)
		}
		return m, nil
	case hostPaletteColorMsg:
		if m.runtime != nil {
			m.runtime.SetHostPaletteColor(typed.Index, typed.Color)
		}
		return m, nil
	case reattachFailedMsg:
		return m, m.openPickerIfUnattached(typed.paneID)
	case clearErrorMsg:
		if typed.seq != m.errorSeq {
			return m, nil
		}
		m.err = nil
		m.render.Invalidate()
		return m, nil
	case clearOwnerConfirmMsg:
		if typed.seq != m.ownerSeq {
			return m, nil
		}
		m.ownerConfirmPaneID = ""
		m.render.Invalidate()
		return m, nil
	case terminalTitleMsg:
		m.render.Invalidate()
		return m, nil
	case sessionSnapshotMsg:
		if typed.Snapshot != nil && typed.Snapshot.Session.Revision != m.sessionRevision {
			m.applySessionSnapshot(typed.Snapshot)
		}
		if typed.Err != nil {
			return m, m.showError(typed.Err)
		}
		return m, nil
	case sessionEventMsg:
		switch typed.Event.Type {
		case protocol.EventSessionDeleted:
			if typed.Event.SessionID == m.sessionID {
				return m, m.showError(fmt.Errorf("session %s was deleted", m.sessionID))
			}
		case protocol.EventSessionCreated, protocol.EventSessionUpdated:
			if typed.Event.SessionID == m.sessionID {
				revision := uint64(0)
				viewID := ""
				if typed.Event.Session != nil {
					revision = typed.Event.Session.Revision
					viewID = typed.Event.Session.ViewID
				}
				if revision > m.sessionRevision && viewID != m.sessionViewID {
					return m, m.pullSessionCmd()
				}
			}
		}
		return m, nil
	case sessionViewUpdatedMsg:
		if typed.View != nil && typed.View.ViewID != "" {
			m.sessionViewID = typed.View.ViewID
		}
		if typed.Err != nil {
			return m, m.showError(typed.Err)
		}
		return m, nil
	case InvalidateMsg:
		m.invalidatePending.Store(false)
		m.render.Invalidate()
		if m.invalidateDeferred.Swap(false) {
			m.queueInvalidate()
		}
		return m, m.maybeAutoFitFloatingPanesCmd()
	case RenderTickMsg:
		if m.render != nil {
			m.render.AdvanceCursorBlink()
		}
		return m, nil
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
		return m, batchCmds(m.resizeVisiblePanesCmd(), m.maybeAutoFitFloatingPanesCmd(), m.updateSessionViewCmd())
	case error:
		return m, m.showError(typed)
	default:
		return m, nil
	}
}

func (m *Model) dispatchSemanticActionCmd(action input.SemanticAction, allowLocal bool) tea.Cmd {
	if allowLocal {
		if handled, cmd := m.handleLocalAction(action); handled {
			return batchCmds(cmd, m.updateSessionViewCmd())
		}
	}
	if handled, cmd := m.handleModalAction(action); handled {
		return batchCmds(cmd, m.updateSessionViewCmd())
	}
	cmd := m.applyEffects(m.enrichEffects(action, m.orchestrator.HandleSemanticAction(action)))
	cmd = batchCmds(cmd, m.resizeCmdForAction(action))
	if m.isStickyMode() {
		cmd = tea.Batch(cmd, m.rearmPrefixTimeoutCmd())
	}
	return batchCmds(cmd, m.updateSessionViewCmd())
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
