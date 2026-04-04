package app

import (
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
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
		case tea.KeyUp:
			if movePromptFormField(m.modalHost.Prompt, -1) {
				m.render.Invalidate()
			}
			return true, nil
		case tea.KeyDown, tea.KeyTab:
			if movePromptFormField(m.modalHost.Prompt, 1) {
				m.render.Invalidate()
			}
			return true, nil
		case tea.KeyShiftTab:
			if movePromptFormField(m.modalHost.Prompt, -1) {
				m.render.Invalidate()
			}
			return true, nil
		case tea.KeyRunes:
			if len(msg.Runes) > 0 {
				insertPromptRunes(m.modalHost.Prompt, msg.Runes)
				m.render.Invalidate()
			}
			return true, nil
		case tea.KeyBackspace:
			if deletePromptRuneBeforeCursor(m.modalHost.Prompt) {
				m.render.Invalidate()
			}
			return true, nil
		case tea.KeyLeft:
			if movePromptCursor(m.modalHost.Prompt, -1) {
				m.render.Invalidate()
			}
			return true, nil
		case tea.KeyRight:
			if movePromptCursor(m.modalHost.Prompt, 1) {
				m.render.Invalidate()
			}
			return true, nil
		case tea.KeyHome:
			if setPromptCursor(m.modalHost.Prompt, 0) {
				m.render.Invalidate()
			}
			return true, nil
		case tea.KeyEnd:
			if setPromptCursor(m.modalHost.Prompt, len([]rune(m.modalHost.Prompt.Value))) {
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

func promptCursor(prompt *modal.PromptState) int {
	if prompt == nil {
		return 0
	}
	cursor := promptEditableCursor(prompt)
	maxCursor := len([]rune(promptEditableValue(prompt)))
	if cursor < 0 {
		return 0
	}
	if cursor > maxCursor {
		return maxCursor
	}
	return cursor
}

func setPromptCursor(prompt *modal.PromptState, cursor int) bool {
	return setPromptEditableCursor(prompt, cursor)
}

func movePromptCursor(prompt *modal.PromptState, delta int) bool {
	return setPromptCursor(prompt, promptCursor(prompt)+delta)
}

func insertPromptRunes(prompt *modal.PromptState, runes []rune) {
	if prompt == nil || len(runes) == 0 {
		return
	}
	field := promptEditableField(prompt)
	value := []rune(promptEditableValue(prompt))
	cursor := promptCursor(prompt)
	next := make([]rune, 0, len(value)+len(runes))
	next = append(next, value[:cursor]...)
	next = append(next, runes...)
	next = append(next, value[cursor:]...)
	if field != nil {
		field.Value = string(next)
		field.Cursor = cursor + len(runes)
		return
	}
	prompt.Value = string(next)
	prompt.Cursor = cursor + len(runes)
}

func deletePromptRuneBeforeCursor(prompt *modal.PromptState) bool {
	if prompt == nil {
		return false
	}
	field := promptEditableField(prompt)
	value := []rune(promptEditableValue(prompt))
	cursor := promptCursor(prompt)
	if cursor <= 0 || len(value) == 0 {
		return false
	}
	value = append(value[:cursor-1], value[cursor:]...)
	if field != nil {
		field.Value = string(value)
		field.Cursor = cursor - 1
		return true
	}
	prompt.Value = string(value)
	prompt.Cursor = cursor - 1
	return true
}
