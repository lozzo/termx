package app

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/bootstrap"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func (m *Model) splitPaneAndAttachTerminalCmd(paneID, terminalID string) tea.Cmd {
	service := m.terminalAttachService()
	if service == nil {
		return nil
	}
	return service.splitAndAttachCmd(paneID, terminalID)
}

func (m *Model) createTabAndAttachTerminalCmd(terminalID string) tea.Cmd {
	service := m.terminalAttachService()
	if service == nil {
		return nil
	}
	return service.createTabAndAttachCmd(terminalID)
}

func (m *Model) createFloatingPaneAndAttachTerminalCmd(terminalID string) tea.Cmd {
	service := m.terminalAttachService()
	if service == nil {
		return nil
	}
	return service.createFloatingAndAttachCmd(terminalID)
}

func (m *Model) splitPaneAndBindTerminalCmd(paneID string, item modal.PickerItem) tea.Cmd {
	service := m.terminalBindingService()
	if service == nil {
		return nil
	}
	return service.splitAndBindCmd(paneID, item)
}

func (m *Model) createTabAndBindTerminalCmd(item modal.PickerItem) tea.Cmd {
	service := m.terminalBindingService()
	if service == nil {
		return nil
	}
	return service.createTabAndBindCmd(item)
}

func (m *Model) createFloatingPaneAndBindTerminalCmd(item modal.PickerItem) tea.Cmd {
	service := m.terminalBindingService()
	if service == nil {
		return nil
	}
	return service.createFloatingAndBindCmd(item)
}

func (m *Model) bindTerminalSelectionCmd(tabID, paneID string, item modal.PickerItem) tea.Cmd {
	service := m.terminalBindingService()
	if service == nil {
		return nil
	}
	return service.bindSelectionCmd(tabID, paneID, item)
}

func terminalSelectionState(item modal.PickerItem) string {
	state := strings.TrimSpace(item.TerminalState)
	if state != "" {
		return state
	}
	return strings.TrimSpace(item.State)
}

func appendUniqueValue(values []string, value string) []string {
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
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
	service := m.terminalAttachService()
	if service == nil {
		return nil
	}
	return service.attachCmd(tabID, paneID, terminalID)
}

func (m *Model) restartPaneTerminalCmd(paneID, terminalID string) tea.Cmd {
	service := m.terminalAttachService()
	if service == nil {
		return nil
	}
	return service.restartAndAttachCmd(paneID, terminalID)
}

func (m *Model) finalizeTerminalAttachCmd(tabID, paneID, terminalID string) tea.Cmd {
	service := m.terminalAttachService()
	if service == nil {
		return nil
	}
	return service.finalizeAttachCmd(tabID, paneID, terminalID)
}

func (m *Model) reattachRestoredPanesCmd(hints []bootstrap.PaneReattachHint) tea.Cmd {
	if m == nil || len(hints) == 0 {
		return nil
	}
	service := m.terminalAttachService()
	if service == nil {
		return nil
	}
	cmds := make([]tea.Cmd, 0, len(hints))
	for _, hint := range hints {
		if cmd := service.reattachRestoredCmd(hint); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if len(cmds) == 0 {
		return nil
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

func (m *Model) syncActivePaneOwnershipAndResizeCmd() tea.Cmd {
	return m.syncTerminalInteractionCmd(terminalInteractionRequest{ResizeIfNeeded: true})
}

func (m *Model) syncActivePaneTabSwitchTakeoverCmd() tea.Cmd {
	if !m.localActivePaneNeedsOwnershipForResize() {
		return nil
	}
	req := terminalInteractionRequest{
		ResizeIfNeeded:   true,
		ExplicitTakeover: true,
	}
	target, ok := m.resolveTerminalInteractionTarget(req)
	if !ok {
		return nil
	}
	return func() tea.Msg {
		if err := m.syncTerminalInteraction(context.Background(), req, target); err != nil {
			return err
		}
		if !m.pendingPaneResizeSatisfied(target.paneID, target.terminalID, target.rect) {
			m.markPendingPaneResize("", target.paneID, target.terminalID)
		}
		return nil
	}
}

func (m *Model) localActivePaneNeedsOwnershipForResize() bool {
	if m == nil || m.sessionID != "" || m.workbench == nil || m.runtime == nil {
		return false
	}
	pane, _, ok := m.visiblePaneForInput("")
	if !ok || pane == nil || pane.TerminalID == "" {
		return false
	}
	terminal := m.runtime.Registry().Get(pane.TerminalID)
	if terminal == nil {
		return false
	}
	if strings.TrimSpace(terminal.OwnerPaneID) == pane.ID {
		return false
	}
	if len(terminal.BoundPaneIDs) < 2 {
		return false
	}
	return true
}
