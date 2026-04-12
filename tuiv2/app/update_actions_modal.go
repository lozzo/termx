package app

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
)

func (m *Model) handleModalAction(action input.SemanticAction) (bool, tea.Cmd) {
	if m == nil {
		return false, nil
	}
	if m.mode().Kind == input.ModeTerminalManager && m.terminalPage != nil {
		return m.handleTerminalManagerModalAction(action)
	}
	if m.modalHost == nil || m.modalHost.Session == nil {
		return false, nil
	}
	switch m.modalHost.Session.Kind {
	case input.ModePicker:
		return m.handlePickerModalAction(action)
	case input.ModePrompt:
		switch action.Kind {
		case input.ActionCancelMode:
			m.closeModal(input.ModePrompt, m.modalHost.Session.RequestID, input.ModeState{})
			m.restorePromptReturnMode(m.modalHost.Prompt)
			m.render.Invalidate()
			return true, nil
		case input.ActionSubmitPrompt:
			return true, m.submitPromptCmd(action.PaneID)
		default:
			return false, nil
		}
	case input.ModeWorkspacePicker:
		return m.handleWorkspacePickerModalAction(action)
	case input.ModeHelp:
		switch action.Kind {
		case input.ActionCancelMode:
			m.closeModal(input.ModeHelp, m.modalHost.Session.RequestID, input.ModeState{Kind: input.ModeNormal})
			m.render.Invalidate()
			return true, nil
		default:
			return false, nil
		}
	case input.ModeFloatingOverview:
		return m.handleFloatingOverviewModalAction(action)
	default:
		if action.Kind == input.ActionCancelMode {
			m.setMode(input.ModeState{Kind: input.ModeNormal})
			m.render.Invalidate()
			return true, nil
		}
		return false, nil
	}
}

func (m *Model) selectedTerminalManagerItem() *modal.PickerItem {
	if m == nil || m.terminalPage == nil {
		return nil
	}
	selected := m.terminalPage.SelectedItem()
	if selected == nil || selected.CreateNew || selected.TerminalID == "" {
		return nil
	}
	return selected
}

func (m *Model) selectedWorkspacePickerItem() *modal.WorkspacePickerItem {
	if m == nil || m.modalHost == nil || m.modalHost.WorkspacePicker == nil {
		return nil
	}
	selected := m.modalHost.WorkspacePicker.SelectedItem()
	if selected == nil || strings.TrimSpace(selected.Name) == "" {
		return nil
	}
	return selected
}

func (m *Model) openSelectedWorkspaceTreeItem() tea.Cmd {
	if m == nil || m.workbench == nil || m.modalHost == nil || m.modalHost.WorkspacePicker == nil {
		return nil
	}
	selected := m.selectedWorkspacePickerItem()
	if selected == nil {
		return nil
	}
	if selected.CreateNew {
		m.openCreateWorkspaceNamePromptWithValue(input.ModeWorkspacePicker, selected.CreateName)
		return nil
	}
	switch selectedWorkspaceTreeItemKind(*selected) {
	case modal.WorkspacePickerItemWorkspace:
		workspaceName := selectedWorkspaceTreeWorkspaceName(*selected)
		if !m.workbench.SwitchWorkspace(workspaceName) {
			return m.showError(fmt.Errorf("workspace %q not found", workspaceName))
		}
	case modal.WorkspacePickerItemTab:
		workspaceName := selectedWorkspaceTreeWorkspaceName(*selected)
		if !m.workbench.SwitchWorkspace(workspaceName) {
			return m.showError(fmt.Errorf("workspace %q not found", workspaceName))
		}
		if err := m.workbench.SwitchTab(workspaceName, selected.TabIndex); err != nil {
			return m.showError(err)
		}
	case modal.WorkspacePickerItemPane:
		workspaceName := selectedWorkspaceTreeWorkspaceName(*selected)
		if !m.workbench.SwitchWorkspace(workspaceName) {
			return m.showError(fmt.Errorf("workspace %q not found", workspaceName))
		}
		if err := m.workbench.SwitchTab(workspaceName, selected.TabIndex); err != nil {
			return m.showError(err)
		}
		if err := m.workbench.FocusPane(selected.TabID, selected.PaneID); err != nil {
			return m.showError(err)
		}
	default:
		return nil
	}
	m.closeModal(input.ModeWorkspacePicker, m.modalHost.Session.RequestID, input.ModeState{Kind: input.ModeNormal})
	m.render.Invalidate()
	return nil
}

func selectedWorkspaceTreeItemKind(item modal.WorkspacePickerItem) modal.WorkspacePickerItemKind {
	switch {
	case item.CreateNew:
		return modal.WorkspacePickerItemCreate
	case item.Kind != "":
		return item.Kind
	case strings.TrimSpace(item.TabID) != "":
		return modal.WorkspacePickerItemTab
	default:
		return modal.WorkspacePickerItemWorkspace
	}
}

func selectedWorkspaceTreeWorkspaceName(item modal.WorkspacePickerItem) string {
	if name := strings.TrimSpace(item.WorkspaceName); name != "" {
		return name
	}
	if selectedWorkspaceTreeItemKind(item) == modal.WorkspacePickerItemWorkspace {
		return strings.TrimSpace(item.Name)
	}
	return ""
}

func terminalSelectionNeedsReferenceBinding(item *modal.PickerItem) bool {
	if item == nil || item.CreateNew || item.TerminalID == "" {
		return false
	}
	return terminalSelectionState(*item) != "running"
}

func (m *Model) closeTerminalManager() {
	if m == nil {
		return
	}
	m.closeTerminalPoolSurface()
	m.render.Invalidate()
}

func (m *Model) currentOrActionPaneID(paneID string) string {
	if strings.TrimSpace(paneID) != "" {
		return paneID
	}
	if m == nil || m.workbench == nil {
		return ""
	}
	if pane := m.workbench.ActivePane(); pane != nil {
		return pane.ID
	}
	return ""
}
