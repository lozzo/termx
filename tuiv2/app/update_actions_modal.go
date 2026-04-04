package app

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/orchestrator"
	"github.com/lozzow/termx/tuiv2/shared"
)

func (m *Model) handleModalAction(action input.SemanticAction) (bool, tea.Cmd) {
	if m == nil {
		return false, nil
	}
	if m.input.Mode().Kind == input.ModeTerminalManager && m.terminalPage != nil {
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
			if selected := m.selectedAttachableTerminalPageItem(); selected != nil {
				m.closeTerminalManager()
				return true, m.attachPaneTerminalCmd("", m.currentOrActionPaneID(action.PaneID), selected.TerminalID)
			}
			if m.selectedTerminalManagerItem() != nil {
				return true, m.showError(shared.UserVisibleError{Op: "attach terminal", Err: fmt.Errorf("selected terminal is exited")})
			}
			return true, nil
		case input.ActionAttachTab:
			if selected := m.selectedAttachableTerminalPageItem(); selected != nil {
				m.closeTerminalManager()
				return true, m.createTabAndAttachTerminalCmd(selected.TerminalID)
			}
			if m.selectedTerminalManagerItem() != nil {
				return true, m.showError(shared.UserVisibleError{Op: "attach terminal", Err: fmt.Errorf("selected terminal is exited")})
			}
			return true, nil
		case input.ActionAttachFloating:
			if selected := m.selectedAttachableTerminalPageItem(); selected != nil {
				m.closeTerminalManager()
				return true, m.createFloatingPaneAndAttachTerminalCmd(selected.TerminalID)
			}
			if m.selectedTerminalManagerItem() != nil {
				return true, m.showError(shared.UserVisibleError{Op: "attach terminal", Err: fmt.Errorf("selected terminal is exited")})
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
			m.modalHost.Close(input.ModePicker, m.modalHost.Session.RequestID)
			m.input.SetMode(input.ModeState{Kind: input.ModeNormal})
			m.render.Invalidate()
			return true, nil
		case input.ActionSubmitPrompt:
			if selected := m.modalHost.Picker.SelectedItem(); selected != nil && selected.CreateNew {
				m.openCreateTerminalPrompt(action.PaneID, modal.CreateTargetReplace)
				return true, nil
			}
			if m.modalHost.Picker.SelectedItem() == nil {
				return true, nil
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
			m.modalHost.Close(input.ModePicker, m.modalHost.Session.RequestID)
			m.input.SetMode(input.ModeState{Kind: input.ModeNormal})
			m.render.Invalidate()
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
			m.modalHost.Close(input.ModePrompt, m.modalHost.Session.RequestID)
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
			m.modalHost.Close(input.ModeWorkspacePicker, m.modalHost.Session.RequestID)
			m.input.SetMode(input.ModeState{Kind: input.ModeNormal})
			m.render.Invalidate()
			return true, nil
		case input.ActionSubmitPrompt:
			if selected := m.modalHost.WorkspacePicker.SelectedItem(); selected != nil {
				if selected.CreateNew {
					m.openCreateWorkspaceNamePrompt(input.ModeNormal)
					return true, nil
				}
				return true, func() tea.Msg {
					return input.SemanticAction{Kind: input.ActionSwitchWorkspace, Text: selected.Name}
				}
			}
			return true, nil
		case input.ActionCreateWorkspace:
			if m.workbench == nil {
				return true, nil
			}
			m.openCreateWorkspaceNamePrompt(input.ModeNormal)
			return true, nil
		case input.ActionDeleteWorkspace:
			if m.workbench == nil {
				return true, nil
			}
			ws := m.workbench.CurrentWorkspace()
			if ws == nil {
				return true, nil
			}
			if err := m.workbench.DeleteWorkspace(ws.Name); err != nil {
				return true, m.showError(err)
			}
			m.modalHost.Close(input.ModeWorkspacePicker, m.modalHost.Session.RequestID)
			m.input.SetMode(input.ModeState{Kind: input.ModeNormal})
			m.render.Invalidate()
			return true, tea.Batch(m.resizeVisiblePanesCmd(), m.saveStateCmd())
		case input.ActionRenameWorkspace:
			m.openRenameWorkspacePrompt()
			return true, nil
		case input.ActionPrevWorkspace:
			if m.workbench == nil {
				return true, nil
			}
			if err := m.workbench.SwitchWorkspaceByOffset(-1); err != nil {
				return true, m.showError(err)
			}
			m.modalHost.Close(input.ModeWorkspacePicker, m.modalHost.Session.RequestID)
			m.input.SetMode(input.ModeState{Kind: input.ModeNormal})
			m.render.Invalidate()
			return true, tea.Batch(m.resizeVisiblePanesCmd(), m.saveStateCmd())
		case input.ActionNextWorkspace:
			if m.workbench == nil {
				return true, nil
			}
			if err := m.workbench.SwitchWorkspaceByOffset(1); err != nil {
				return true, m.showError(err)
			}
			m.modalHost.Close(input.ModeWorkspacePicker, m.modalHost.Session.RequestID)
			m.input.SetMode(input.ModeState{Kind: input.ModeNormal})
			m.render.Invalidate()
			return true, tea.Batch(m.resizeVisiblePanesCmd(), m.saveStateCmd())
		default:
			return false, nil
		}
	case input.ModeHelp:
		switch action.Kind {
		case input.ActionCancelMode:
			m.modalHost.Close(input.ModeHelp, m.modalHost.Session.RequestID)
			m.input.SetMode(input.ModeState{Kind: input.ModeNormal})
			m.render.Invalidate()
			return true, nil
		default:
			return false, nil
		}
	default:
		if action.Kind == input.ActionCancelMode {
			m.input.SetMode(input.ModeState{Kind: input.ModeNormal})
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

func (m *Model) selectedAttachableTerminalPageItem() *modal.PickerItem {
	selected := m.selectedTerminalManagerItem()
	if selected == nil || selected.State == "exited" {
		return nil
	}
	return selected
}

func (m *Model) closeTerminalManager() {
	if m == nil {
		return
	}
	m.terminalPage = nil
	m.input.SetMode(input.ModeState{Kind: input.ModeNormal})
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
