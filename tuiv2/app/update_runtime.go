package app

import (
	"context"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/bootstrap"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/orchestrator"
	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func (m *Model) splitPaneAndAttachTerminalCmd(paneID, terminalID string) tea.Cmd {
	if m == nil || m.workbench == nil || paneID == "" || terminalID == "" {
		return nil
	}
	tab := m.workbench.CurrentTab()
	if tab == nil {
		return nil
	}
	newPaneID := shared.NextPaneID()
	if err := m.workbench.SplitPane(tab.ID, paneID, newPaneID, workbench.SplitVertical); err != nil {
		return func() tea.Msg { return err }
	}
	_ = m.workbench.FocusPane(tab.ID, newPaneID)
	m.render.Invalidate()
	return tea.Batch(m.attachPaneTerminalCmd("", newPaneID, terminalID), m.saveStateCmd())
}

func (m *Model) createTabAndAttachTerminalCmd(terminalID string) tea.Cmd {
	if m == nil || m.workbench == nil || terminalID == "" {
		return nil
	}
	ws := m.workbench.CurrentWorkspace()
	if ws == nil {
		return nil
	}
	tabID := shared.NextTabID()
	paneID := shared.NextPaneID()
	name := strconv.Itoa(len(ws.Tabs) + 1)
	if err := m.workbench.CreateTab(ws.Name, tabID, name); err != nil {
		return func() tea.Msg { return err }
	}
	if err := m.workbench.CreateFirstPane(tabID, paneID); err != nil {
		return func() tea.Msg { return err }
	}
	_ = m.workbench.SwitchTab(ws.Name, len(ws.Tabs)-1)
	m.render.Invalidate()
	return tea.Batch(m.attachPaneTerminalCmd("", paneID, terminalID), m.saveStateCmd())
}

func (m *Model) createFloatingPaneAndAttachTerminalCmd(terminalID string) tea.Cmd {
	if m == nil || m.workbench == nil || terminalID == "" {
		return nil
	}
	tab := m.workbench.CurrentTab()
	if tab == nil {
		return nil
	}
	paneID := shared.NextPaneID()
	if err := m.workbench.CreateFloatingPane(tab.ID, paneID, workbench.Rect{X: 10, Y: 5, W: 80, H: 24}); err != nil {
		return func() tea.Msg { return err }
	}
	_ = m.workbench.FocusPane(tab.ID, paneID)
	m.render.Invalidate()
	return tea.Batch(m.attachPaneTerminalCmd("", paneID, terminalID), m.saveStateCmd())
}

func (m *Model) nextSequenceCmd(seq sequenceMsg) tea.Cmd {
	if len(seq) == 0 {
		return nil
	}
	return func() tea.Msg {
		return seq[0]
	}
}

func (m *Model) attachInitialTerminalCmd(terminalID string) tea.Cmd {
	if m == nil || m.workbench == nil || m.orchestrator == nil || terminalID == "" {
		return nil
	}
	pane := m.workbench.ActivePane()
	if pane == nil || pane.ID == "" {
		return nil
	}
	if m.modalHost != nil && m.modalHost.Session != nil && m.modalHost.Session.Kind == input.ModePicker {
		m.modalHost.Close(input.ModePicker, m.modalHost.Session.RequestID)
		m.input.SetMode(input.ModeState{Kind: input.ModeNormal})
	}
	paneID := pane.ID
	return m.attachPaneTerminalCmd("", paneID, terminalID)
}

func (m *Model) attachPaneTerminalCmd(tabID, paneID, terminalID string) tea.Cmd {
	if m == nil || m.orchestrator == nil || paneID == "" || terminalID == "" {
		return nil
	}
	return func() tea.Msg {
		msgs, err := m.orchestrator.AttachAndLoadSnapshot(context.Background(), paneID, terminalID, "collaborator", 0, 200)
		if err != nil {
			return err
		}
		for index := range msgs {
			if attached, ok := msgs[index].(orchestrator.TerminalAttachedMsg); ok {
				attached.TabID = tabID
				msgs[index] = attached
			}
		}
		cmds := make([]tea.Cmd, 0, len(msgs))
		for _, msg := range msgs {
			value := msg
			cmds = append(cmds, func() tea.Msg { return value })
		}
		return tea.Batch(cmds...)()
	}
}

func (m *Model) reattachRestoredPanesCmd(hints []bootstrap.PaneReattachHint) tea.Cmd {
	if m == nil || len(hints) == 0 {
		return nil
	}
	cmds := make([]tea.Cmd, 0, len(hints))
	for _, hint := range hints {
		h := hint
		cmds = append(cmds, func() tea.Msg {
			cmd := m.attachPaneTerminalCmd(h.TabID, h.PaneID, h.TerminalID)
			if cmd == nil {
				return reattachFailedMsg{tabID: h.TabID, paneID: h.PaneID}
			}
			msg := cmd()
			if _, ok := msg.(error); ok {
				if m.workbench != nil && h.TabID != "" {
					_ = m.workbench.BindPaneTerminal(h.TabID, h.PaneID, "")
				}
				return reattachFailedMsg{tabID: h.TabID, paneID: h.PaneID}
			}
			return msg
		})
	}
	return tea.Batch(cmds...)
}

type sequenceMsg []any

func (m *Model) resizeVisiblePanesCmd() tea.Cmd {
	if m == nil || m.runtime == nil || m.workbench == nil {
		return nil
	}
	bodyRect := m.bodyRect()
	visible := m.workbench.VisibleWithSize(bodyRect)
	if visible == nil || visible.ActiveTab < 0 || visible.ActiveTab >= len(visible.Tabs) {
		return nil
	}
	tab := visible.Tabs[visible.ActiveTab]
	panes := make([]workbench.VisiblePane, 0, len(tab.Panes)+len(visible.FloatingPanes))
	panes = append(panes, tab.Panes...)
	panes = append(panes, visible.FloatingPanes...)

	cmds := make([]tea.Cmd, 0, len(panes))
	for _, pane := range panes {
		if pane.ID == "" || pane.TerminalID == "" {
			continue
		}
		cols := uint16(maxInt(2, pane.Rect.W-2))
		rows := uint16(maxInt(2, pane.Rect.H-2))
		paneID := pane.ID
		cmds = append(cmds, func() tea.Msg {
			if err := m.runtime.ResizeTerminal(context.Background(), paneID, pane.TerminalID, cols, rows); err != nil {
				return err
			}
			return nil
		})
	}
	return tea.Batch(cmds...)
}
