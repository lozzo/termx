package app

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/orchestrator"
)

func (m *Model) handleModalAction(action input.SemanticAction) (bool, tea.Cmd) {
	if m == nil {
		return false, nil
	}
	if m.mode().Kind == input.ModeTerminalManager && m.terminalPage != nil {
		switch action.Kind {
		case input.ActionPickerUp:
			m.terminalPage.Move(-1)
			m.render.Invalidate()
			return true, nil
		case input.ActionPickerDown:
			m.terminalPage.Move(1)
			m.render.Invalidate()
			return true, nil
		case input.ActionCancelMode:
			m.closeTerminalManager()
			return true, nil
		case input.ActionSubmitPrompt:
			if selected := m.selectedTerminalManagerItem(); selected != nil {
				m.closeTerminalManager()
				if terminalSelectionNeedsReferenceBinding(selected) {
					return true, m.bindTerminalSelectionCmd("", m.currentOrActionPaneID(action.PaneID), *selected)
				}
				return true, m.attachPaneTerminalCmd("", m.currentOrActionPaneID(action.PaneID), selected.TerminalID)
			}
			return true, nil
		case input.ActionAttachTab:
			if selected := m.selectedTerminalManagerItem(); selected != nil {
				m.closeTerminalManager()
				if terminalSelectionNeedsReferenceBinding(selected) {
					return true, m.createTabAndBindTerminalCmd(*selected)
				}
				return true, m.createTabAndAttachTerminalCmd(selected.TerminalID)
			}
			return true, nil
		case input.ActionAttachFloating:
			if selected := m.selectedTerminalManagerItem(); selected != nil {
				m.closeTerminalManager()
				if terminalSelectionNeedsReferenceBinding(selected) {
					return true, m.createFloatingPaneAndBindTerminalCmd(*selected)
				}
				return true, m.createFloatingPaneAndAttachTerminalCmd(selected.TerminalID)
			}
			return true, nil
		case input.ActionEditTerminal:
			if selected := m.selectedTerminalManagerItem(); selected != nil {
				m.openEditTerminalPrompt(selected)
				return true, nil
			}
			return true, nil
		case input.ActionKillTerminal:
			if selected := m.selectedTerminalManagerItem(); selected != nil {
				terminalID := selected.TerminalID
				items := m.terminalPage.Items
				filtered := items[:0]
				for _, item := range items {
					if item.TerminalID != terminalID {
						filtered = append(filtered, item)
					}
				}
				m.terminalPage.Items = filtered
				m.terminalPage.ApplyFilter()
				normalizeModalSelection(&m.terminalPage.Selected, len(m.terminalPage.VisibleItems()))
				m.render.Invalidate()
				return true, m.effectCmd(orchestrator.KillTerminalEffect{TerminalID: terminalID})
			}
			return true, nil
		default:
			return false, nil
		}
	}
	if m.modalHost == nil || m.modalHost.Session == nil {
		return false, nil
	}
	switch m.modalHost.Session.Kind {
	case input.ModePicker:
		if m.modalHost.Picker == nil {
			return false, nil
		}
		if m.modalHost.Session.RequestID == clipboardHistoryRequestID() {
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
		if m.modalHost.WorkspacePicker == nil {
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
			initialValue := ""
			if m.modalHost.WorkspacePicker != nil {
				initialValue = strings.TrimSpace(m.modalHost.WorkspacePicker.Query)
			}
			m.openCreateWorkspaceNamePromptWithValue(input.ModeNormal, initialValue)
			return true, nil
		case input.ActionDeleteWorkspace:
			if m.workbench == nil {
				return true, nil
			}
			selected := m.selectedWorkspacePickerItem()
			if selected == nil || selected.CreateNew || selectedWorkspaceTreeItemKind(*selected) != modal.WorkspacePickerItemWorkspace {
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
			if selected == nil || selected.CreateNew || selectedWorkspaceTreeItemKind(*selected) != modal.WorkspacePickerItemWorkspace {
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
		if m.modalHost.FloatingOverview == nil {
			return false, nil
		}
		switch action.Kind {
		case input.ActionPickerUp:
			m.modalHost.FloatingOverview.Move(-1)
			m.render.Invalidate()
			return true, nil
		case input.ActionPickerDown:
			m.modalHost.FloatingOverview.Move(1)
			m.render.Invalidate()
			return true, nil
		case input.ActionCancelMode:
			m.closeFloatingOverview()
			return true, nil
		case input.ActionSubmitPrompt:
			return true, m.focusFloatingOverviewSelection()
		case input.ActionExpandAllFloatingPanes:
			return true, m.expandAllFloatingPanes()
		case input.ActionCollapseAllFloatingPanes:
			return true, m.collapseAllFloatingPanes()
		case input.ActionCloseFloatingPane:
			if selected := m.modalHost.FloatingOverview.SelectedItem(); selected != nil {
				return true, m.closeFloatingPaneDirect(selected.PaneID)
			}
			return true, nil
		default:
			return false, nil
		}
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
		m.openCreateWorkspaceNamePromptWithValue(input.ModeNormal, selected.CreateName)
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
