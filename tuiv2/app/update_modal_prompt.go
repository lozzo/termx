package app

import (
	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) submitPromptCmd(paneID string) tea.Cmd {
	if m == nil || m.modalHost == nil || m.modalHost.Prompt == nil {
		return nil
	}
	prompt := m.modalHost.Prompt
	switch prompt.Kind {
	case "rename-tab":
		return m.submitRenameTabPrompt(prompt)
	case "rename-workspace":
		return m.submitRenameWorkspacePrompt(prompt)
	case "create-terminal-name":
		return m.submitCreateTerminalNamePrompt(prompt)
	case "edit-terminal-name":
		return m.submitEditTerminalNamePrompt(prompt)
	case "edit-terminal-tags":
		return m.submitEditTerminalTagsPrompt(prompt)
	case "create-terminal-tags":
		return m.submitCreateTerminalTagsPrompt(prompt, paneID)
	default:
		return nil
	}
}
