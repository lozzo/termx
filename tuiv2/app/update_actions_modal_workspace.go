package app

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
)

func (m *Model) handleWorkspacePickerModalAction(action input.SemanticAction) (bool, tea.Cmd) {
	if m == nil || m.modalHost == nil || m.modalHost.WorkspacePicker == nil {
		return false, nil
	}
	switch action.Kind {
	case input.ActionPickerUp:
		m.modalHost.WorkspacePicker.Move(-1)
		m.render.Invalidate()
		return true, nil
	case input.ActionPickerDown:
		m.modalHost.WorkspacePicker.Move(1)
		m.render.Invalidate()
		return true, nil
	case input.ActionCancelMode:
		m.closeModal(input.ModeWorkspacePicker, m.modalHost.Session.RequestID, input.ModeState{Kind: input.ModeNormal})
		m.render.Invalidate()
		return true, nil
	case input.ActionSubmitPrompt:
		return true, m.openSelectedWorkspaceTreeItem()
	case input.ActionCreateWorkspace:
		if m.workbench == nil {
			return true, nil
		}
		selected := m.selectedWorkspacePickerItem()
		if selected != nil {
			kind := selectedWorkspaceTreeItemKind(*selected)
			if kind == modal.WorkspacePickerItemTab || kind == modal.WorkspacePickerItemPane {
				return true, nil
			}
		}
		initialValue := ""
		if m.modalHost.WorkspacePicker != nil {
			initialValue = strings.TrimSpace(m.modalHost.WorkspacePicker.Query)
		}
		m.openCreateWorkspaceNamePromptWithValue(input.ModeWorkspacePicker, initialValue)
		return true, nil
	case input.ActionDeleteWorkspace:
		if m.workbench == nil {
			return true, nil
		}
		selected := m.selectedWorkspacePickerItem()
		if selected == nil || selected.CreateNew || selectedWorkspaceTreeItemKind(*selected) != modal.WorkspacePickerItemWorkspace {
			if selected != nil && selectedWorkspaceTreeItemKind(*selected) == modal.WorkspacePickerItemTab {
				if err := m.workbench.CloseTab(selected.TabID); err != nil {
					return true, m.showError(err)
				}
				m.modalHost.WorkspacePicker.Items = m.workspacePickerItems()
				m.modalHost.WorkspacePicker.ApplyFilter()
				normalizeModalSelection(&m.modalHost.WorkspacePicker.Selected, len(m.modalHost.WorkspacePicker.VisibleItems()))
				m.render.Invalidate()
				return true, nil
			}
			if selected != nil && selectedWorkspaceTreeItemKind(*selected) == modal.WorkspacePickerItemPane {
				return true, m.runWorkspaceTreePaneAction(input.ActionClosePane)
			}
			return true, nil
		}
		if err := m.workbench.DeleteWorkspace(selectedWorkspaceTreeWorkspaceName(*selected)); err != nil {
			return true, m.showError(err)
		}
		m.modalHost.WorkspacePicker.Items = m.workspacePickerItems()
		m.modalHost.WorkspacePicker.ApplyFilter()
		normalizeModalSelection(&m.modalHost.WorkspacePicker.Selected, len(m.modalHost.WorkspacePicker.VisibleItems()))
		m.render.Invalidate()
		return true, m.saveStateCmd()
	case input.ActionRenameWorkspace:
		selected := m.selectedWorkspacePickerItem()
		if selected == nil || selected.CreateNew {
			return true, nil
		}
		if selectedWorkspaceTreeItemKind(*selected) == modal.WorkspacePickerItemTab {
			m.openRenameTabPromptFor(selectedWorkspaceTreeWorkspaceName(*selected), selected.TabID, selected.Name)
			return true, nil
		}
		if selectedWorkspaceTreeItemKind(*selected) != modal.WorkspacePickerItemWorkspace {
			return true, nil
		}
		m.openRenameWorkspacePromptFor(selectedWorkspaceTreeWorkspaceName(*selected))
		return true, nil
	case input.ActionRenameTab:
		selected := m.selectedWorkspacePickerItem()
		if selected == nil || selectedWorkspaceTreeItemKind(*selected) != modal.WorkspacePickerItemTab {
			return true, nil
		}
		m.openRenameTabPromptFor(selectedWorkspaceTreeWorkspaceName(*selected), selected.TabID, selected.Name)
		return true, nil
	case input.ActionCloseTab:
		if m.workbench == nil {
			return true, nil
		}
		selected := m.selectedWorkspacePickerItem()
		if selected == nil || selectedWorkspaceTreeItemKind(*selected) != modal.WorkspacePickerItemTab {
			return true, nil
		}
		if err := m.workbench.CloseTab(selected.TabID); err != nil {
			return true, m.showError(err)
		}
		m.modalHost.WorkspacePicker.Items = m.workspacePickerItems()
		m.modalHost.WorkspacePicker.ApplyFilter()
		normalizeModalSelection(&m.modalHost.WorkspacePicker.Selected, len(m.modalHost.WorkspacePicker.VisibleItems()))
		m.render.Invalidate()
		return true, nil
	case input.ActionDetachPane:
		return true, m.runWorkspaceTreePaneAction(input.ActionDetachPane)
	case input.ActionZoomPane:
		return true, m.runWorkspaceTreePaneAction(input.ActionZoomPane)
	case input.ActionPrevWorkspace:
		if m.workbench == nil {
			return true, nil
		}
		if err := m.workbench.SwitchWorkspaceByOffset(-1); err != nil {
			return true, m.showError(err)
		}
		m.closeModal(input.ModeWorkspacePicker, m.modalHost.Session.RequestID, input.ModeState{Kind: input.ModeNormal})
		m.render.Invalidate()
		return true, m.saveStateCmd()
	case input.ActionNextWorkspace:
		if m.workbench == nil {
			return true, nil
		}
		if err := m.workbench.SwitchWorkspaceByOffset(1); err != nil {
			return true, m.showError(err)
		}
		m.closeModal(input.ModeWorkspacePicker, m.modalHost.Session.RequestID, input.ModeState{Kind: input.ModeNormal})
		m.render.Invalidate()
		return true, m.saveStateCmd()
	default:
		return false, nil
	}
}

func (m *Model) runWorkspaceTreePaneAction(kind input.ActionKind) tea.Cmd {
	if m == nil || m.workbench == nil || m.modalHost == nil || m.modalHost.WorkspacePicker == nil {
		return nil
	}
	selected := m.selectedWorkspacePickerItem()
	if selected == nil || selectedWorkspaceTreeItemKind(*selected) != modal.WorkspacePickerItemPane {
		return nil
	}
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
	switch kind {
	case input.ActionZoomPane:
		if m.blocksSemanticActionForTerminalSizeLock(input.SemanticAction{Kind: input.ActionZoomPane, PaneID: selected.PaneID}) {
			return m.showNotice(terminalSizeLockedNotice)
		}
		tab := m.workbench.CurrentTab()
		if tab == nil {
			return nil
		}
		enteringZoom := tab.ZoomedPaneID != selected.PaneID
		if tab.ZoomedPaneID == selected.PaneID {
			tab.ZoomedPaneID = ""
		} else {
			tab.ZoomedPaneID = selected.PaneID
		}
		m.closeModal(input.ModeWorkspacePicker, m.modalHost.Session.RequestID, input.ModeState{Kind: input.ModeNormal})
		m.render.Invalidate()
		return m.syncZoomViewportCmd(selected.PaneID, enteringZoom)
	case input.ActionDetachPane, input.ActionClosePane:
		m.closeModal(input.ModeWorkspacePicker, m.modalHost.Session.RequestID, input.ModeState{Kind: input.ModeNormal})
		m.render.Invalidate()
		return m.dispatchSemanticActionCmd(input.SemanticAction{Kind: kind, PaneID: selected.PaneID}, false)
	default:
		return nil
	}
}
