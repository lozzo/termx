package app

import (
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/input"
)

func (m *Model) handleModalKeyMsg(msg tea.KeyMsg) (bool, tea.Cmd) {
	if m == nil {
		return false, nil
	}
	if m.input.Mode().Kind == input.ModeTerminalManager && m.terminalPage != nil {
		return m.handleTerminalManagerQueryKeyMsg(msg)
	}
	if m.modalHost == nil || m.modalHost.Session == nil {
		return false, nil
	}
	switch m.modalHost.Session.Kind {
	case input.ModePrompt:
		if m.modalHost.Prompt == nil {
			return false, nil
		}
		switch msg.Type {
		case tea.KeyRunes:
			if len(msg.Runes) > 0 {
				m.modalHost.Prompt.Value += string(msg.Runes)
				m.render.Invalidate()
			}
			return true, nil
		case tea.KeyBackspace:
			if deleteLastRune(&m.modalHost.Prompt.Value) {
				m.render.Invalidate()
			}
			return true, nil
		case tea.KeyEnter:
			return true, func() tea.Msg {
				return input.SemanticAction{Kind: input.ActionSubmitPrompt, PaneID: m.modalHost.Prompt.PaneID}
			}
		case tea.KeyEsc:
			return true, func() tea.Msg { return input.SemanticAction{Kind: input.ActionCancelMode} }
		default:
			return false, nil
		}
	case input.ModePicker:
		return m.handlePickerQueryKeyMsg(msg)
	case input.ModeWorkspacePicker:
		return m.handleWorkspacePickerQueryKeyMsg(msg)
	case input.ModeTerminalManager:
		return m.handleTerminalManagerQueryKeyMsg(msg)
	default:
		return false, nil
	}
}

func (m *Model) handlePickerQueryKeyMsg(msg tea.KeyMsg) (bool, tea.Cmd) {
	if m.modalHost == nil || m.modalHost.Picker == nil {
		return false, nil
	}
	switch msg.Type {
	case tea.KeyRunes:
		if len(msg.Runes) == 0 {
			return true, nil
		}
		m.modalHost.Picker.Query += string(msg.Runes)
	case tea.KeyBackspace:
		if !deleteLastRune(&m.modalHost.Picker.Query) {
			return true, nil
		}
	default:
		return false, nil
	}
	m.modalHost.Picker.ApplyFilter()
	normalizeModalSelection(&m.modalHost.Picker.Selected, len(m.modalHost.Picker.VisibleItems()))
	m.render.Invalidate()
	return true, nil
}

func (m *Model) handleWorkspacePickerQueryKeyMsg(msg tea.KeyMsg) (bool, tea.Cmd) {
	if m.modalHost == nil || m.modalHost.WorkspacePicker == nil {
		return false, nil
	}
	switch msg.Type {
	case tea.KeyRunes:
		if len(msg.Runes) == 0 {
			return true, nil
		}
		m.modalHost.WorkspacePicker.Query += string(msg.Runes)
	case tea.KeyBackspace:
		if !deleteLastRune(&m.modalHost.WorkspacePicker.Query) {
			return true, nil
		}
	default:
		return false, nil
	}
	m.modalHost.WorkspacePicker.ApplyFilter()
	normalizeModalSelection(&m.modalHost.WorkspacePicker.Selected, len(m.modalHost.WorkspacePicker.VisibleItems()))
	m.render.Invalidate()
	return true, nil
}

func (m *Model) handleTerminalManagerQueryKeyMsg(msg tea.KeyMsg) (bool, tea.Cmd) {
	if m.terminalPage == nil {
		return false, nil
	}
	switch msg.Type {
	case tea.KeyRunes:
		if len(msg.Runes) == 0 {
			return true, nil
		}
		m.terminalPage.Query += string(msg.Runes)
	case tea.KeyBackspace:
		if !deleteLastRune(&m.terminalPage.Query) {
			return true, nil
		}
	default:
		return false, nil
	}
	m.terminalPage.ApplyFilter()
	normalizeModalSelection(&m.terminalPage.Selected, len(m.terminalPage.VisibleItems()))
	m.render.Invalidate()
	return true, nil
}

func deleteLastRune(value *string) bool {
	if value == nil || *value == "" {
		return false
	}
	_, size := utf8.DecodeLastRuneInString(*value)
	if size > 0 {
		*value = (*value)[:len(*value)-size]
	} else {
		*value = ""
	}
	return true
}

func normalizeModalSelection(selected *int, count int) {
	if selected == nil {
		return
	}
	if count <= 0 || *selected < 0 || *selected >= count {
		*selected = 0
	}
}
