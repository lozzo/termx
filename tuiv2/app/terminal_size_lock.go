package app

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/terminalmeta"
	"github.com/lozzow/termx/tuiv2/input"
)

const terminalSizeLockedNotice = "terminal size is locked"
const terminalSizeUnlockedNotice = "terminal size lock disabled"

func (m *Model) terminalSizeLocked(terminalID string) bool {
	if m == nil || m.runtime == nil || strings.TrimSpace(terminalID) == "" {
		return false
	}
	terminal := m.runtime.Registry().Get(terminalID)
	return terminal != nil && terminalmeta.SizeLocked(terminal.Tags)
}

func (m *Model) paneTerminalSizeLocked(paneID string) bool {
	if m == nil || strings.TrimSpace(paneID) == "" {
		return false
	}
	pane, _, ok := m.visiblePaneForInput(paneID)
	if !ok || pane == nil || strings.TrimSpace(pane.TerminalID) == "" {
		return false
	}
	return m.terminalSizeLocked(pane.TerminalID)
}

func (m *Model) currentTabHasLockedTerminal() bool {
	if m == nil || m.workbench == nil {
		return false
	}
	tab := m.workbench.CurrentTab()
	if tab == nil {
		return false
	}
	for _, pane := range tab.Panes {
		if pane == nil || strings.TrimSpace(pane.TerminalID) == "" {
			continue
		}
		if m.terminalSizeLocked(pane.TerminalID) {
			return true
		}
	}
	return false
}

func (m *Model) blocksSemanticActionForTerminalSizeLock(action input.SemanticAction) bool {
	if m == nil {
		return false
	}
	switch action.Kind {
	case input.ActionZoomPane,
		input.ActionResizePaneLeft,
		input.ActionResizePaneRight,
		input.ActionResizePaneUp,
		input.ActionResizePaneDown,
		input.ActionResizePaneLargeLeft,
		input.ActionResizePaneLargeRight,
		input.ActionResizePaneLargeUp,
		input.ActionResizePaneLargeDown,
		input.ActionResizeFloatingLeft,
		input.ActionResizeFloatingRight,
		input.ActionResizeFloatingUp,
		input.ActionResizeFloatingDown:
		return m.paneTerminalSizeLocked(m.currentOrActionPaneID(action.PaneID))
	case input.ActionBalancePanes,
		input.ActionCycleLayout:
		return m.currentTabHasLockedTerminal()
	default:
		return false
	}
}

func (m *Model) toggleTerminalSizeLockCmd(paneID string) tea.Cmd {
	if m == nil || m.workbench == nil || m.runtime == nil {
		return nil
	}
	target := m.currentOrActionPaneID(paneID)
	if target == "" {
		return nil
	}
	pane, _, ok := m.visiblePaneForInput(target)
	if !ok || pane == nil || strings.TrimSpace(pane.TerminalID) == "" {
		return nil
	}
	terminal := m.runtime.Registry().Get(pane.TerminalID)
	if terminal == nil {
		return nil
	}
	tags := cloneStringMap(terminal.Tags)
	if tags == nil {
		tags = make(map[string]string)
	}
	notice := terminalSizeLockedNotice
	if terminalmeta.SizeLocked(tags) {
		delete(tags, terminalmeta.SizeLockTag)
		notice = terminalSizeUnlockedNotice
	} else {
		tags[terminalmeta.SizeLockTag] = terminalmeta.SizeLockLock
	}
	terminalID := pane.TerminalID
	name := strings.TrimSpace(terminal.Name)
	return func() tea.Msg {
		client := m.runtime.Client()
		if client == nil {
			return context.Canceled
		}
		if err := client.SetMetadata(context.Background(), terminalID, name, tags); err != nil {
			return err
		}
		m.runtime.SetTerminalMetadata(terminalID, name, tags)
		if err := saveState(m.statePath, m.workbench, m.runtime); err != nil {
			return err
		}
		m.render.Invalidate()
		return terminalSizeLockToggledMsg{Notice: notice}
	}
}
