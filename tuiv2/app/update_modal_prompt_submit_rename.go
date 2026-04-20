package app

import (
	"context"
	"errors"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/shared"
)

func (m *Model) submitRenameTabPrompt(prompt *modal.PromptState) tea.Cmd {
	if m == nil || prompt == nil || m.modalHost == nil {
		return nil
	}
	name := strings.TrimSpace(prompt.ValueState().Value())
	if name == "" {
		name = strings.TrimSpace(prompt.Original)
	}
	if m.workbench == nil {
		return func() tea.Msg { return context.Canceled }
	}
	tabID := strings.TrimSpace(prompt.TabID)
	if tabID == "" {
		tab := m.workbench.CurrentTab()
		if tab == nil {
			return func() tea.Msg { return context.Canceled }
		}
		tabID = tab.ID
	}
	workspaceName := strings.TrimSpace(prompt.WorkspaceName)
	if workspaceName == "" {
		workspaceName = m.workbench.CurrentWorkspaceName()
	}
	if err := m.validateUniqueWorkspaceTabName(workspaceName, tabID, name); err != nil {
		return func() tea.Msg { return err }
	}
	if err := m.workbench.RenameTab(tabID, name); err != nil {
		return func() tea.Msg { return err }
	}
	requestID := ""
	if m.modalHost.Session != nil {
		requestID = m.modalHost.Session.RequestID
	}
	m.closeModal(input.ModePrompt, requestID, input.ModeState{})
	m.restorePromptReturnMode(prompt)
	m.render.Invalidate()
	return m.saveStateCmd()
}

func (m *Model) submitRenameWorkspacePrompt(prompt *modal.PromptState) tea.Cmd {
	if m == nil || prompt == nil || m.modalHost == nil {
		return nil
	}
	original := strings.TrimSpace(prompt.Original)
	name := strings.TrimSpace(prompt.ValueState().Value())
	if m.workbench == nil {
		return func() tea.Msg { return context.Canceled }
	}
	if original == "" {
		if name == "" {
			return m.showError(shared.UserVisibleError{Op: "create workspace", Err: errors.New("name is required")})
		}
		if err := m.workbench.CreateWorkspace(name); err != nil {
			return func() tea.Msg { return err }
		}
		if !m.workbench.SwitchWorkspace(name) {
			return func() tea.Msg { return context.Canceled }
		}
	} else {
		if name == "" {
			name = original
		}
		if err := m.workbench.RenameWorkspace(original, name); err != nil {
			return func() tea.Msg { return err }
		}
	}
	requestID := ""
	if m.modalHost.Session != nil {
		requestID = m.modalHost.Session.RequestID
	}
	m.closeModal(input.ModePrompt, requestID, input.ModeState{})
	m.restorePromptReturnMode(prompt)
	m.render.Invalidate()
	return m.saveStateCmd()
}
