package app

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
)

func (m *Model) handleTerminalManagerModalAction(action input.SemanticAction) (bool, tea.Cmd) {
	if m == nil || m.terminalPage == nil {
		return false, nil
	}
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
			m.markTerminalManagerItemExited(terminalID)
			m.render.Invalidate()
			return true, m.killTerminalAndRefreshManagerCmd(terminalID)
		}
		return true, nil
	case input.ActionRemoveTerminal:
		if selected := m.selectedTerminalManagerItem(); selected != nil {
			terminalID := selected.TerminalID
			return true, m.removeTerminalCmd(terminalID, true)
		}
		return true, nil
	default:
		return false, nil
	}
}

func (m *Model) removeTerminalFromManagerItems(terminalID string) {
	if m == nil || m.terminalPage == nil || terminalID == "" {
		return
	}
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
}

func (m *Model) markTerminalManagerItemExited(terminalID string) {
	if m == nil || m.terminalPage == nil || terminalID == "" {
		return
	}
	for index := range m.terminalPage.Items {
		item := &m.terminalPage.Items[index]
		if item.TerminalID != terminalID {
			continue
		}
		item.State = "exited"
		item.TerminalState = "exited"
		item.Observed = false
		item.Description = "exited · refreshing"
		break
	}
	sortTerminalManagerItems(m.terminalPage.Items)
	m.terminalPage.ApplyFilter()
	if index := terminalManagerVisibleIndexByTerminalID(m.terminalPage.VisibleItems(), terminalID); index >= 0 {
		m.terminalPage.Selected = index
		return
	}
	normalizeModalSelection(&m.terminalPage.Selected, len(m.terminalPage.VisibleItems()))
}

func terminalManagerVisibleIndexByTerminalID(items []modal.PickerItem, terminalID string) int {
	if terminalID == "" {
		return -1
	}
	for index, item := range items {
		if item.TerminalID == terminalID {
			return index
		}
	}
	return -1
}

func (m *Model) killTerminalAndRefreshManagerCmd(terminalID string) tea.Cmd {
	if m == nil || m.runtime == nil || terminalID == "" {
		return nil
	}
	return func() tea.Msg {
		client := m.runtime.Client()
		if client != nil {
			_ = client.Kill(context.Background(), terminalID)
		}
		terminals, err := m.runtime.ListTerminals(context.Background())
		if err != nil {
			return err
		}
		return terminalManagerItemsLoadedMsg{Items: m.buildTerminalManagerItems(terminals)}
	}
}

func (m *Model) removeTerminalCmd(terminalID string, reloadManager bool) tea.Cmd {
	if m == nil || terminalID == "" {
		return nil
	}
	return func() tea.Msg {
		if m.runtime != nil && m.runtime.Client() != nil {
			if err := m.runtime.Client().Remove(context.Background(), terminalID); err != nil {
				return err
			}
		}
		m.removeTerminalFromLocalState(terminalID)
		if reloadManager && m.runtime != nil {
			terminals, err := m.runtime.ListTerminals(context.Background())
			if err != nil {
				return err
			}
			return terminalManagerItemsLoadedMsg{Items: m.buildTerminalManagerItems(terminals)}
		}
		return nil
	}
}

func (m *Model) removeTerminalFromLocalState(terminalID string) {
	if m == nil || terminalID == "" {
		return
	}
	if m.workbench != nil {
		for _, wsName := range m.workbench.ListWorkspaces() {
			ws := m.workbench.WorkspaceByName(wsName)
			if ws == nil {
				continue
			}
			for _, tab := range ws.Tabs {
				if tab == nil {
					continue
				}
				for paneID, pane := range tab.Panes {
					if pane == nil || pane.TerminalID != terminalID {
						continue
					}
					_ = m.workbench.BindPaneTerminal(tab.ID, paneID, "")
					if m.runtime != nil {
						m.runtime.UnbindPane(paneID, terminalID)
					}
				}
			}
		}
	}
	if m.runtime != nil {
		m.runtime.RemoveTerminal(terminalID)
	}
	if m.sessionLeases != nil {
		delete(m.sessionLeases, terminalID)
		if len(m.sessionLeases) == 0 {
			m.sessionLeases = nil
		}
	}
	m.removeTerminalFromManagerItems(terminalID)
	m.removeTerminalFromPickerItems(terminalID)
	m.render.Invalidate()
}
