package app

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/orchestrator"
)

func (m *Model) handlePickerModalAction(action input.SemanticAction) (bool, tea.Cmd) {
	if m == nil || m.modalHost == nil || m.modalHost.Picker == nil {
		return false, nil
	}
	if m.modalHost.Session != nil && m.modalHost.Session.RequestID == clipboardHistoryRequestID() {
		switch action.Kind {
		case input.ActionPickerUp:
			m.modalHost.Picker.Move(-1)
			m.render.Invalidate()
			return true, nil
		case input.ActionPickerDown:
			m.modalHost.Picker.Move(1)
			m.render.Invalidate()
			return true, nil
		case input.ActionCancelMode:
			m.closeModal(input.ModePicker, m.modalHost.Session.RequestID, input.ModeState{Kind: input.ModeDisplay})
			m.render.Invalidate()
			return true, nil
		case input.ActionSubmitPrompt:
			selected := m.modalHost.Picker.SelectedItem()
			if selected == nil || strings.TrimSpace(selected.TerminalID) == "" {
				return true, nil
			}
			entry := m.clipboardHistoryEntryByID(selected.TerminalID)
			m.closeModal(input.ModePicker, m.modalHost.Session.RequestID, input.ModeState{Kind: input.ModeNormal})
			m.leaveCopyMode()
			if entry == nil {
				return true, m.showError(fmt.Errorf("clipboard history entry not found"))
			}
			return true, m.pasteTextToActiveCmd(entry.Text)
		default:
			return false, nil
		}
	}
	switch action.Kind {
	case input.ActionPickerUp:
		m.modalHost.Picker.Move(-1)
		m.render.Invalidate()
		return true, nil
	case input.ActionPickerDown:
		m.modalHost.Picker.Move(1)
		m.render.Invalidate()
		return true, nil
	case input.ActionCancelMode:
		m.closeModal(input.ModePicker, m.modalHost.Session.RequestID, input.ModeState{Kind: input.ModeNormal})
		m.render.Invalidate()
		return true, nil
	case input.ActionSubmitPrompt:
		if selected := m.modalHost.Picker.SelectedItem(); selected != nil && selected.CreateNew {
			m.openCreateTerminalPrompt(action.PaneID, modal.CreateTargetReplace)
			return true, nil
		}
		selected := m.modalHost.Picker.SelectedItem()
		if selected == nil {
			return true, nil
		}
		if terminalSelectionNeedsReferenceBinding(selected) {
			m.closeModal(input.ModePicker, m.modalHost.Session.RequestID, input.ModeState{Kind: input.ModeNormal})
			m.render.Invalidate()
			return true, m.bindTerminalSelectionCmd("", m.currentOrActionPaneID(action.PaneID), *selected)
		}
		return false, nil
	case input.ActionPickerAttachSplit:
		selected := m.modalHost.Picker.SelectedItem()
		if selected == nil {
			return true, nil
		}
		if selected.CreateNew {
			m.openCreateTerminalPrompt(action.PaneID, modal.CreateTargetSplit)
			return true, nil
		}
		m.closeModal(input.ModePicker, m.modalHost.Session.RequestID, input.ModeState{Kind: input.ModeNormal})
		m.render.Invalidate()
		if terminalSelectionNeedsReferenceBinding(selected) {
			return true, m.splitPaneAndBindTerminalCmd(m.currentOrActionPaneID(action.PaneID), *selected)
		}
		return true, m.splitPaneAndAttachTerminalCmd(m.currentOrActionPaneID(action.PaneID), selected.TerminalID)
	case input.ActionEditTerminal:
		if selected := m.modalHost.Picker.SelectedItem(); selected != nil && !selected.CreateNew {
			m.openEditTerminalPrompt(selected)
		}
		return true, nil
	case input.ActionKillTerminal:
		if selected := m.modalHost.Picker.SelectedItem(); selected != nil && !selected.CreateNew {
			terminalID := selected.TerminalID
			items := m.modalHost.Picker.Items
			filtered := items[:0]
			for _, item := range items {
				if item.TerminalID != terminalID {
					filtered = append(filtered, item)
				}
			}
			m.modalHost.Picker.Items = filtered
			m.modalHost.Picker.ApplyFilter()
			normalizeModalSelection(&m.modalHost.Picker.Selected, len(m.modalHost.Picker.VisibleItems()))
			m.render.Invalidate()
			return true, m.effectCmd(orchestrator.KillTerminalEffect{TerminalID: terminalID})
		}
		return true, nil
	default:
		return false, nil
	}
}
