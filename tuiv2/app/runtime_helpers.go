package app

import (
	"errors"

	"github.com/lozzow/termx/tuiv2/workbench"
)

type teaErr string

func (e teaErr) Error() string { return string(e) }

func sharedInputLeaseUnsupportedError() error {
	return errors.New("shared input lease unsupported")
}

func (m *Model) isPaneAttachPending(paneID string) bool {
	if m == nil || paneID == "" {
		return false
	}
	_, ok := m.pendingPaneAttaches[paneID]
	return ok
}

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
	if m == nil || paneID == "" || m.pendingPaneAttaches == nil {
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

func (m *Model) visiblePaneForInput(paneID string) (*workbench.PaneState, workbench.Rect, bool) {
	if m == nil || m.workbench == nil {
		return nil, workbench.Rect{}, false
	}
	tab := m.workbench.CurrentTab()
	if tab == nil {
		return nil, workbench.Rect{}, false
	}
	if paneID == "" {
		paneID = tab.ActivePaneID
	}
	if paneID == "" {
		return nil, workbench.Rect{}, false
	}
	pane := tab.Panes[paneID]
	if pane == nil {
		return nil, workbench.Rect{}, false
	}
	visible := m.workbench.VisibleWithSize(m.bodyRect())
	if visible == nil {
		return pane, workbench.Rect{}, false
	}
	for _, floating := range visible.FloatingPanes {
		if floating.ID == paneID {
			return pane, floating.Rect, true
		}
	}
	if visible.ActiveTab >= 0 && visible.ActiveTab < len(visible.Tabs) {
		for _, tiled := range visible.Tabs[visible.ActiveTab].Panes {
			if tiled.ID == paneID {
				return pane, tiled.Rect, true
			}
		}
	}
	return pane, workbench.Rect{}, false
}

func (m *Model) terminalAlreadySized(terminalID string, cols, rows uint16) bool {
	if m == nil || m.runtime == nil || terminalID == "" {
		return false
	}
	terminal := m.runtime.Registry().Get(terminalID)
	if terminal == nil || terminal.Snapshot == nil {
		return false
	}
	return terminal.Snapshot.Size.Cols == cols && terminal.Snapshot.Size.Rows == rows
}
