package app

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/workbench"
)

type pendingPaneResize struct {
	TabID      string
	PaneID     string
	TerminalID string
}

func (m *Model) hasViewportSize() bool {
	return m != nil && m.width > 0 && m.height > 0
}

func (m *Model) markPendingPaneResize(tabID, paneID, terminalID string) {
	service := m.layoutResizeService()
	if service == nil {
		return
	}
	service.markPendingPaneResize(tabID, paneID, terminalID)
}

func (m *Model) clearPendingPaneResize(paneID, terminalID string) {
	service := m.layoutResizeService()
	if service == nil {
		return
	}
	service.clearPendingPaneResize(paneID, terminalID)
}

func (m *Model) paneResizeTarget(tabID, paneID string) (*workbench.PaneState, workbench.Rect, bool) {
	service := m.layoutResizeService()
	if service == nil {
		return nil, workbench.Rect{}, false
	}
	return service.paneResizeTarget(tabID, paneID)
}

func (m *Model) resizePendingPaneResizesCmd() tea.Cmd {
	service := m.layoutResizeService()
	if service == nil {
		return nil
	}
	return service.resizePendingCmd()
}
