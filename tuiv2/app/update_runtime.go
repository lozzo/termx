package app

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/bootstrap"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/orchestrator"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func (m *Model) splitPaneAndAttachTerminalCmd(paneID, terminalID string) tea.Cmd {
	if m == nil || m.orchestrator == nil || paneID == "" || terminalID == "" {
		return nil
	}
	tabID, newPaneID, err := m.orchestrator.PrepareSplitAttachTarget(paneID)
	if err != nil {
		return func() tea.Msg { return err }
	}
	m.render.Invalidate()
	return tea.Batch(m.attachPaneTerminalCmd(tabID, newPaneID, terminalID), m.saveStateCmd())
}

func (m *Model) createTabAndAttachTerminalCmd(terminalID string) tea.Cmd {
	if m == nil || m.orchestrator == nil || terminalID == "" {
		return nil
	}
	tabID, paneID, err := m.orchestrator.PrepareTabAttachTarget()
	if err != nil {
		return func() tea.Msg { return err }
	}
	m.render.Invalidate()
	return tea.Batch(m.attachPaneTerminalCmd(tabID, paneID, terminalID), m.saveStateCmd())
}

func (m *Model) createFloatingPaneAndAttachTerminalCmd(terminalID string) tea.Cmd {
	if m == nil || m.orchestrator == nil || terminalID == "" {
		return nil
	}
	tabID, paneID, err := m.orchestrator.PrepareFloatingAttachTarget()
	if err != nil {
		return func() tea.Msg { return err }
	}
	m.render.Invalidate()
	return tea.Batch(m.attachPaneTerminalCmd(tabID, paneID, terminalID), m.saveStateCmd())
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
		m.closeModal(input.ModePicker, m.modalHost.Session.RequestID, input.ModeState{Kind: input.ModeNormal})
	}
	paneID := pane.ID
	return m.attachPaneTerminalCmd("", paneID, terminalID)
}

func (m *Model) attachPaneTerminalCmd(tabID, paneID, terminalID string) tea.Cmd {
	if m == nil || m.orchestrator == nil || paneID == "" || terminalID == "" {
		return nil
	}
	m.markPendingPaneAttach(paneID, terminalID)
	return func() tea.Msg {
		msgs, err := m.orchestrator.AttachAndLoadSnapshot(context.Background(), paneID, terminalID, "collaborator", 0, defaultTerminalSnapshotScrollbackLimit)
		if err != nil {
			return paneAttachFailure(paneID, terminalID, err)
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

func (m *Model) restartPaneTerminalCmd(paneID, terminalID string) tea.Cmd {
	if m == nil || m.runtime == nil || m.orchestrator == nil || paneID == "" || terminalID == "" {
		return nil
	}
	m.markPendingPaneAttach(paneID, terminalID)
	return func() tea.Msg {
		client := m.runtime.Client()
		if client == nil {
			return paneAttachFailure(paneID, terminalID, teaErr("attach terminal: runtime client is nil"))
		}
		if err := client.Restart(context.Background(), terminalID); err != nil {
			return paneAttachFailure(paneID, terminalID, err)
		}
		msgs, err := m.orchestrator.AttachAndLoadSnapshot(context.Background(), paneID, terminalID, "collaborator", 0, defaultTerminalSnapshotScrollbackLimit)
		if err != nil {
			return paneAttachFailure(paneID, terminalID, err)
		}
		cmds := make([]tea.Cmd, 0, len(msgs))
		for _, msg := range msgs {
			value := msg
			cmds = append(cmds, func() tea.Msg { return value })
		}
		return tea.Batch(cmds...)()
	}
}

func (m *Model) finalizeTerminalAttachCmd(tabID, paneID, terminalID string) tea.Cmd {
	if m == nil || paneID == "" || terminalID == "" {
		return nil
	}
	return func() tea.Msg {
		if m.sessionID == "" {
			if pane, rect, ok := m.paneResizeTarget(tabID, paneID); ok && pane != nil && pane.TerminalID == terminalID {
				if err := m.ensurePaneTerminalSize(context.Background(), paneID, terminalID, rect); err != nil {
					return err
				}
				m.clearPendingPaneResize(paneID, terminalID)
			} else {
				m.markPendingPaneResize(tabID, paneID, terminalID)
			}
		}
		return terminalAttachReadyMsg{paneID: paneID, terminalID: terminalID}
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
			switch msg.(type) {
			case error, paneAttachFailedMsg:
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
	return func() tea.Msg {
		if err := m.resizeVisiblePanes(context.Background()); err != nil {
			return err
		}
		return nil
	}
}

func (m *Model) resizeVisiblePanes(ctx context.Context) error {
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

	for _, pane := range panes {
		if pane.ID == "" || pane.TerminalID == "" {
			continue
		}
		target := terminalInteractionTarget{
			paneID:     pane.ID,
			terminalID: pane.TerminalID,
			rect:       pane.Rect,
		}
		req := terminalInteractionRequest{
			PaneID:         pane.ID,
			TerminalID:     pane.TerminalID,
			Rect:           pane.Rect,
			ResizeIfNeeded: true,
		}
		if m.sessionID != "" && pane.ID == tab.ActivePaneID {
			req.ImplicitSessionLease = true
		}
		if err := m.syncTerminalInteraction(ctx, req, target); err != nil {
			return err
		}
	}
	return nil
}

func (m *Model) syncZoomViewportCmd(paneID string, explicitTakeover bool) tea.Cmd {
	if m == nil {
		return nil
	}
	return func() tea.Msg {
		ctx := context.Background()
		if explicitTakeover && m.zoomShouldTakeOwnership(paneID) {
			req := terminalInteractionRequest{
				PaneID:           paneID,
				ResizeIfNeeded:   true,
				ExplicitTakeover: true,
			}
			if target, ok := m.resolveTerminalInteractionTarget(req); ok {
				if err := m.syncTerminalInteraction(ctx, req, target); err != nil {
					return err
				}
			}
		}
		if err := m.resizeVisiblePanes(ctx); err != nil {
			return err
		}
		if m.sessionID == "" {
			if cmd := m.saveStateCmd(); cmd != nil {
				return cmd()
			}
		}
		return nil
	}
}

func (m *Model) zoomShouldTakeOwnership(paneID string) bool {
	if m == nil || m.workbench == nil || m.runtime == nil || strings.TrimSpace(paneID) == "" {
		return false
	}
	tab := m.workbench.CurrentTab()
	if tab == nil {
		return false
	}
	pane := tab.Panes[paneID]
	if pane == nil || pane.TerminalID == "" {
		return false
	}
	if m.sessionID != "" {
		return true
	}
	terminal := m.runtime.Registry().Get(pane.TerminalID)
	if terminal == nil {
		return false
	}
	if len(terminal.BoundPaneIDs) > 1 || strings.TrimSpace(terminal.OwnerPaneID) != "" {
		return true
	}
	if binding := m.runtime.Binding(paneID); binding != nil && binding.Role == runtime.BindingRoleFollower {
		return true
	}
	return false
}

func (m *Model) resizePaneIfNeededCmd(paneID string) tea.Cmd {
	if m == nil || m.runtime == nil || m.workbench == nil {
		return nil
	}
	target := m.currentOrActionPaneID(paneID)
	if target == "" {
		return nil
	}
	pane, rect, ok := m.visiblePaneForInput(target)
	if !ok || pane == nil || pane.TerminalID == "" {
		return nil
	}
	return func() tea.Msg {
		if err := m.ensurePaneTerminalSize(context.Background(), pane.ID, pane.TerminalID, rect); err != nil {
			return err
		}
		return nil
	}
}

func (m *Model) resizeActivePaneIfNeededCmd() tea.Cmd {
	return m.resizePaneIfNeededCmd("")
}

func (m *Model) syncActivePaneInteractiveOwnershipCmd() tea.Cmd {
	return m.syncTerminalInteractionCmd(terminalInteractionRequest{
		ResizeIfNeeded:           true,
		ImplicitInteractiveOwner: true,
	})
}

func (m *Model) resizeCmdForAction(action input.SemanticAction) tea.Cmd {
	switch action.Kind {
	case input.ActionSplitPane,
		input.ActionSplitPaneHorizontal,
		input.ActionZoomPane,
		input.ActionResizePaneLeft,
		input.ActionResizePaneRight,
		input.ActionResizePaneUp,
		input.ActionResizePaneDown,
		input.ActionResizePaneLargeLeft,
		input.ActionResizePaneLargeRight,
		input.ActionResizePaneLargeUp,
		input.ActionResizePaneLargeDown,
		input.ActionBalancePanes,
		input.ActionCycleLayout:
		return m.resizeVisiblePanesCmd()
	case input.ActionResizeFloatingLeft,
		input.ActionResizeFloatingRight,
		input.ActionResizeFloatingUp,
		input.ActionResizeFloatingDown:
		return m.resizePaneIfNeededCmd(action.PaneID)
	default:
		return nil
	}
}

func (m *Model) saveCmdForAction(action input.SemanticAction) tea.Cmd {
	switch action.Kind {
	case input.ActionSplitPane,
		input.ActionSplitPaneHorizontal,
		input.ActionResizePaneLeft,
		input.ActionResizePaneRight,
		input.ActionResizePaneUp,
		input.ActionResizePaneDown,
		input.ActionResizePaneLargeLeft,
		input.ActionResizePaneLargeRight,
		input.ActionResizePaneLargeUp,
		input.ActionResizePaneLargeDown,
		input.ActionBalancePanes,
		input.ActionCycleLayout:
		return m.saveStateCmd()
	default:
		return nil
	}
}

func (m *Model) syncActivePaneOwnershipAndResizeCmd() tea.Cmd {
	return m.syncTerminalInteractionCmd(terminalInteractionRequest{ResizeIfNeeded: true})
}
