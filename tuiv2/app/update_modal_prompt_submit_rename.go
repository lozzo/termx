package app

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
)

func (m *Model) submitRenameTabPrompt(prompt *modal.PromptState) tea.Cmd {
	if m == nil || prompt == nil || m.modalHost == nil {
		return nil
	}
	name := strings.TrimSpace(prompt.Value)
	if name == "" {
		name = strings.TrimSpace(prompt.Original)
	}
	if m.workbench == nil {
		return func() tea.Msg { return context.Canceled }
	}
	tab := m.workbench.CurrentTab()
	if tab == nil {
		return func() tea.Msg { return context.Canceled }
	}
	if err := m.workbench.RenameTab(tab.ID, name); err != nil {
		return func() tea.Msg { return err }
	}
	requestID := ""
	if m.modalHost.Session != nil {
		requestID = m.modalHost.Session.RequestID
	}
	m.modalHost.Close(input.ModePrompt, requestID)
	m.restorePromptReturnMode(prompt)
	m.render.Invalidate()
	return m.saveStateCmd()
}

func (m *Model) submitRenameWorkspacePrompt(prompt *modal.PromptState) tea.Cmd {
	if m == nil || prompt == nil || m.modalHost == nil {
		return nil
	}
	name := strings.TrimSpace(prompt.Value)
	if name == "" {
		name = strings.TrimSpace(prompt.Original)
	}
	original := strings.TrimSpace(prompt.Original)
	if m.workbench == nil {
		return func() tea.Msg { return context.Canceled }
	}
	if err := m.workbench.RenameWorkspace(original, name); err != nil {
		return func() tea.Msg { return err }
	}
	requestID := ""
	if m.modalHost.Session != nil {
		requestID = m.modalHost.Session.RequestID
	}
	m.modalHost.Close(input.ModePrompt, requestID)
	m.restorePromptReturnMode(prompt)
	m.render.Invalidate()
	return m.saveStateCmd()
}
