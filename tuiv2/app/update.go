package app

import (
	tea "github.com/charmbracelet/bubbletea"
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
		if handled, cmd := m.handleLocalAction(typed.Action); handled {
			return m, cmd
		}
		if handled, cmd := m.handleModalAction(typed.Action); handled {
			return m, cmd
		}
		cmd := m.applyEffects(m.enrichEffects(typed.Action, m.orchestrator.HandleSemanticAction(typed.Action)))
		if m.isStickyMode() {
			cmd = tea.Batch(cmd, m.rearmPrefixTimeoutCmd())
		}
		return m, cmd
	case input.SemanticAction:
		if handled, cmd := m.handleLocalAction(typed); handled {
			return m, cmd
		}
		if handled, cmd := m.handleModalAction(typed); handled {
			return m, cmd
		}
		cmd := m.applyEffects(m.enrichEffects(typed, m.orchestrator.HandleSemanticAction(typed)))
		if m.isStickyMode() {
			cmd = tea.Batch(cmd, m.rearmPrefixTimeoutCmd())
		}
		return m, cmd
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
		return m, tea.Batch(m.saveStateCmd(), m.resizeVisiblePanesCmd())
	case orchestrator.SnapshotLoadedMsg:
		m.render.Invalidate()
		return m, nil
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
	case InvalidateMsg:
		m.invalidatePending.Store(false)
		m.render.Invalidate()
		return m, nil
	case RenderTickMsg:
		if m.render != nil && m.render.NeedsCursorTicks() {
			m.render.Invalidate()
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
		return m, m.resizeVisiblePanesCmd()
	case error:
		return m, m.showError(typed)
	default:
		return m, nil
	}
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
