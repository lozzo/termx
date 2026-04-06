package app

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/orchestrator"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func (m *Model) isStickyMode() bool {
	kind := m.mode().Kind
	return kind == input.ModePane || kind == input.ModeResize || kind == input.ModeTab ||
		kind == input.ModeWorkspace || kind == input.ModeFloating ||
		kind == input.ModeGlobal
}

func (m *Model) rearmPrefixTimeoutCmd() tea.Cmd {
	m.prefixSeq++
	seq := m.prefixSeq
	return tea.Tick(prefixModeTimeout, func(time.Time) tea.Msg {
		return prefixTimeoutMsg{seq: seq}
	})
}

func (m *Model) handleTerminalInput(in input.TerminalInput) tea.Cmd {
	if len(in.Data) == 0 && in.Kind == input.TerminalInputPaste && in.Text != "" {
		if encoded := m.encodeActiveTerminalPaste(in.Text, in.PaneID); len(encoded) > 0 {
			in.Data = encoded
		}
	}
	if len(in.Data) == 0 {
		return nil
	}
	if m.isPaneAttachPending(in.PaneID) {
		m.pendingTerminalInputs = append(m.pendingTerminalInputs, in)
		return nil
	}
	if m.workbench != nil {
		if pane := m.workbench.ActivePane(); pane != nil && pane.TerminalID == "" {
			return m.openPickerIfUnattached(pane.ID)
		}
	}
	m.pendingTerminalInputs = append(m.pendingTerminalInputs, in)
	if m.terminalInputSending {
		return nil
	}
	return m.dequeueTerminalInputCmd()
}

func (m *Model) openPickerIfUnattached(paneID string) tea.Cmd {
	if m == nil || m.workbench == nil || m.modalHost == nil {
		return nil
	}
	pane := m.workbench.ActivePane()
	if pane == nil || pane.ID != paneID || pane.TerminalID != "" {
		return nil
	}
	if m.modalHost.Session != nil {
		return nil
	}
	m.openModal(input.ModePicker, paneID)
	m.resetPickerState()
	m.render.Invalidate()
	return m.applyEffects([]orchestrator.Effect{orchestrator.LoadPickerItemsEffect{}})
}

func (m *Model) dequeueTerminalInputCmd() tea.Cmd {
	if m == nil {
		return nil
	}
	if len(m.pendingTerminalInputs) == 0 {
		m.terminalInputSending = false
		return nil
	}
	next := m.pendingTerminalInputs[0]
	if m.isPaneAttachPending(next.PaneID) {
		m.terminalInputSending = false
		return nil
	}
	m.pendingTerminalInputs = m.pendingTerminalInputs[1:]
	m.terminalInputSending = true
	return func() tea.Msg {
		if err := m.prepareTerminalInput(context.Background(), next.PaneID); err != nil {
			return terminalInputSentMsg{err: err}
		}
		return terminalInputSentMsg{
			err: m.runtime.SendInput(context.Background(), next.PaneID, next.Data),
		}
	}
}

func (m *Model) prepareTerminalInput(ctx context.Context, paneID string) error {
	target, ok := m.resolveTerminalInteractionTarget(terminalInteractionRequest{PaneID: paneID})
	if !ok {
		return nil
	}
	return m.syncTerminalInteraction(ctx, terminalInteractionRequest{
		PaneID:               target.paneID,
		TerminalID:           target.terminalID,
		Rect:                 target.rect,
		ResizeIfNeeded:       true,
		ImplicitSessionLease: true,
	}, target)
}

func (m *Model) ensurePaneTerminalSize(ctx context.Context, paneID, terminalID string, rect workbench.Rect) error {
	return m.syncTerminalInteraction(ctx, terminalInteractionRequest{
		PaneID:         paneID,
		TerminalID:     terminalID,
		Rect:           rect,
		ResizeIfNeeded: true,
	}, terminalInteractionTarget{
		paneID:     paneID,
		terminalID: terminalID,
		rect:       rect,
	})
}

func (m *Model) implicitSessionLeaseNeedsAcquire(terminalID, paneID string) bool {
	if m == nil || terminalID == "" || paneID == "" || m.sessionViewID == "" {
		return false
	}
	lease, ok := m.sessionLeases[terminalID]
	if !ok {
		return false
	}
	return lease.PaneID == paneID && lease.ViewID != "" && lease.ViewID != m.sessionViewID
}

func (m *Model) terminalAlreadySized(terminalID string, cols, rows uint16) bool {
	if m == nil || m.runtime == nil || terminalID == "" || cols == 0 || rows == 0 {
		return false
	}
	terminal := m.runtime.Registry().Get(terminalID)
	if terminal == nil {
		return false
	}
	if terminal.Snapshot != nil && terminal.Snapshot.Size.Cols == cols && terminal.Snapshot.Size.Rows == rows {
		return true
	}
	if terminal.VTerm == nil {
		return false
	}
	currentCols, currentRows := terminal.VTerm.Size()
	return currentCols == int(cols) && currentRows == int(rows)
}

func (m *Model) visiblePaneForInput(paneID string) (*workbench.PaneState, workbench.Rect, bool) {
	if m == nil || m.workbench == nil {
		return nil, workbench.Rect{}, false
	}
	tabState := m.workbench.CurrentTab()
	if tabState == nil {
		return nil, workbench.Rect{}, false
	}
	if paneID == "" {
		if pane := m.workbench.ActivePane(); pane != nil {
			paneID = pane.ID
		}
	}
	if paneID == "" {
		return nil, workbench.Rect{}, false
	}
	pane := tabState.Panes[paneID]
	if pane == nil {
		return nil, workbench.Rect{}, false
	}
	visible := m.workbench.VisibleWithSize(m.bodyRect())
	if visible == nil || visible.ActiveTab < 0 || visible.ActiveTab >= len(visible.Tabs) {
		return nil, workbench.Rect{}, false
	}
	tab := visible.Tabs[visible.ActiveTab]
	for i := range visible.FloatingPanes {
		if visible.FloatingPanes[i].ID == paneID {
			return pane, visible.FloatingPanes[i].Rect, true
		}
	}
	for i := range tab.Panes {
		if tab.Panes[i].ID == paneID {
			return pane, tab.Panes[i].Rect, true
		}
	}
	return nil, workbench.Rect{}, false
}

func sharedInputLeaseUnsupportedError() error {
	return teaErr("connected termx daemon is too old for shared resize control; restart the daemon and reconnect")
}

type teaErr string

func (e teaErr) Error() string { return string(e) }

func (m *Model) markPendingPaneAttach(paneID, terminalID string) {
	if m == nil || paneID == "" {
		return
	}
	if m.pendingPaneAttaches == nil {
		m.pendingPaneAttaches = make(map[string]string)
	}
	m.pendingPaneAttaches[paneID] = terminalID
}

func (m *Model) clearPendingPaneAttach(paneID, terminalID string) {
	if m == nil || len(m.pendingPaneAttaches) == 0 || paneID == "" {
		return
	}
	current, ok := m.pendingPaneAttaches[paneID]
	if !ok {
		return
	}
	if terminalID != "" && current != "" && current != terminalID {
		return
	}
	delete(m.pendingPaneAttaches, paneID)
}

func (m *Model) isPaneAttachPending(paneID string) bool {
	if m == nil || paneID == "" || len(m.pendingPaneAttaches) == 0 {
		return false
	}
	_, ok := m.pendingPaneAttaches[paneID]
	return ok
}
