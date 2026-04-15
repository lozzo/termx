package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/orchestrator"
	"github.com/lozzow/termx/tuiv2/render"
	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/lozzow/termx/tuiv2/workbench"
)

var errorClearDelay = 3 * time.Second
var ownerConfirmDelay = 400 * time.Millisecond
var sharedTerminalSnapshotResyncDelay = 120 * time.Millisecond

const (
	defaultTerminalSnapshotScrollbackLimit = 500
	maxTerminalSnapshotScrollbackLimit     = 10000
	terminalScrollbackPrefetchMargin       = 8
)

func clearErrorCmd(seq uint64) tea.Cmd {
	return tea.Tick(errorClearDelay, func(time.Time) tea.Msg {
		return clearErrorMsg{seq: seq}
	})
}

func clearOwnerConfirmCmd(seq uint64) tea.Cmd {
	return tea.Tick(ownerConfirmDelay, func(time.Time) tea.Msg {
		return clearOwnerConfirmMsg{seq: seq}
	})
}

func renderErrorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (m *Model) bodyHeight() int {
	if m == nil {
		return render.FrameBodyHeight(0)
	}
	if m.immersiveZoomActive() {
		return maxInt(1, m.height)
	}
	return render.FrameBodyHeight(m.height)
}

func (m *Model) contentOriginY() int {
	if m.immersiveZoomActive() {
		return 0
	}
	return render.TopChromeRows
}

func (m *Model) bodyRect() workbench.Rect {
	if m == nil {
		return workbench.Rect{W: 1, H: render.FrameBodyHeight(0)}
	}
	return workbench.Rect{W: maxInt(1, m.width), H: m.bodyHeight()}
}

func (m *Model) allowVerticalScrollOptimization() bool {
	if m == nil || m.workbench == nil {
		return false
	}
	vm := m.renderVM()
	if vm.Surface.Kind != render.VisibleSurfaceWorkbench || vm.Overlay.Kind != render.VisibleOverlayNone || vm.Workbench == nil {
		return false
	}
	if len(vm.Workbench.FloatingPanes) > 0 {
		return false
	}
	activeTab := vm.Workbench.ActiveTab
	if activeTab < 0 || activeTab >= len(vm.Workbench.Tabs) {
		return false
	}
	panes := vm.Workbench.Tabs[activeTab].Panes
	if len(panes) == 0 {
		return false
	}
	if len(panes) == 1 {
		return true
	}
	body := m.bodyRect()
	if body.W <= 0 || body.H <= 0 {
		return false
	}
	rowOwners := make([]int, body.H)
	for _, pane := range panes {
		contentRect, ok := paneContentRectForVisible(pane)
		if !ok || contentRect.W <= 0 || contentRect.H <= 0 {
			return false
		}
		// The frame presenter scrolls whole terminal rows inside a scroll region.
		// Keep it to layouts where each affected row belongs to one full-width
		// pane, so a scroll in the top pane cannot drag a side-by-side neighbor.
		if pane.Rect.X != 0 || pane.Rect.W != body.W {
			return false
		}
		start := maxInt(0, contentRect.Y)
		end := minInt(body.H, contentRect.Y+contentRect.H)
		for row := start; row < end; row++ {
			rowOwners[row]++
			if rowOwners[row] > 1 {
				return false
			}
		}
	}
	return true
}

func (m *Model) immersiveZoomActive() bool {
	if m == nil || m.workbench == nil {
		return false
	}
	tab := m.workbench.CurrentTab()
	return tab != nil && tab.ZoomedPaneID != ""
}

func (m *Model) paneUsesImmersiveViewport(paneID string) bool {
	if m == nil || m.workbench == nil || strings.TrimSpace(paneID) == "" {
		return false
	}
	tab := m.workbench.CurrentTab()
	return tab != nil && tab.ZoomedPaneID == paneID
}

func (m *Model) terminalViewportRect(paneID string, rect workbench.Rect) (workbench.Rect, bool) {
	if rect.W <= 0 || rect.H <= 0 {
		return workbench.Rect{}, false
	}
	if m.paneUsesImmersiveViewport(paneID) {
		return rect, true
	}
	if visiblePane, ok := m.visiblePaneProjection(paneID); ok {
		// Resize PTYs against the same framed content rect that render uses. If
		// resize math and draw math diverge by even one gutter/divider
		// column, the terminal can legitimately paint into what render thinks is
		// border space.
		return paneContentRectForVisible(visiblePane)
	}
	return paneContentRect(rect)
}

func (m *Model) activePaneContentRect() (workbench.Rect, bool) {
	if m == nil || m.workbench == nil {
		return workbench.Rect{}, false
	}
	tab := m.workbench.CurrentTab()
	if tab == nil || tab.ActivePaneID == "" {
		return workbench.Rect{}, false
	}
	visible := m.workbench.VisibleWithSize(m.bodyRect())
	if visible == nil {
		return workbench.Rect{}, false
	}
	for _, pane := range visible.FloatingPanes {
		if pane.ID != tab.ActivePaneID {
			continue
		}
		return paneContentRectForVisible(pane)
	}
	if visible.ActiveTab < 0 || visible.ActiveTab >= len(visible.Tabs) {
		return workbench.Rect{}, false
	}
	for _, pane := range visible.Tabs[visible.ActiveTab].Panes {
		if pane.ID != tab.ActivePaneID {
			continue
		}
		return paneContentRectForVisible(pane)
	}
	return workbench.Rect{}, false
}

func (m *Model) visiblePaneProjection(paneID string) (workbench.VisiblePane, bool) {
	if m == nil || m.workbench == nil || strings.TrimSpace(paneID) == "" {
		return workbench.VisiblePane{}, false
	}
	visible := m.workbench.VisibleWithSize(m.bodyRect())
	if visible == nil {
		return workbench.VisiblePane{}, false
	}
	for _, pane := range visible.FloatingPanes {
		if pane.ID == paneID {
			return pane, true
		}
	}
	for _, tab := range visible.Tabs {
		for _, pane := range tab.Panes {
			if pane.ID == paneID {
				return pane, true
			}
		}
	}
	return workbench.VisiblePane{}, false
}

func (m *Model) ensureActivePaneScrollbackCmd() tea.Cmd {
	if m == nil || m.workbench == nil || m.runtime == nil {
		return nil
	}
	tab := m.workbench.CurrentTab()
	pane := m.workbench.ActivePane()
	if tab == nil || pane == nil || pane.TerminalID == "" || tab.ScrollOffset <= 0 {
		return nil
	}
	contentRect, ok := m.activePaneContentRect()
	if !ok {
		return nil
	}
	terminal := m.runtime.Registry().Get(pane.TerminalID)
	if terminal == nil || terminal.ScrollbackExhausted {
		return nil
	}
	if terminal.VTerm != nil && terminal.VTerm.Modes().AlternateScreen {
		return nil
	}
	if terminal.VTerm == nil && terminal.Snapshot != nil && terminal.Snapshot.Modes.AlternateScreen {
		return nil
	}
	loaded := terminal.ScrollbackLoadedLimit
	if terminal.Snapshot != nil && len(terminal.Snapshot.Scrollback) > loaded {
		loaded = len(terminal.Snapshot.Scrollback)
	}
	want := tab.ScrollOffset + contentRect.H + terminalScrollbackPrefetchMargin
	if want <= loaded {
		return nil
	}
	nextLimit := maxInt(defaultTerminalSnapshotScrollbackLimit, loaded)
	for nextLimit < want && nextLimit < maxTerminalSnapshotScrollbackLimit {
		nextLimit *= 2
	}
	if nextLimit > maxTerminalSnapshotScrollbackLimit {
		nextLimit = maxTerminalSnapshotScrollbackLimit
	}
	if nextLimit <= loaded || terminal.ScrollbackLoadingLimit >= nextLimit {
		return nil
	}
	terminal.ScrollbackLoadingLimit = nextLimit
	terminalID := pane.TerminalID
	return func() tea.Msg {
		snapshot, err := m.runtime.LoadSnapshot(context.Background(), terminalID, 0, nextLimit)
		if err != nil {
			return err
		}
		return orchestrator.SnapshotLoadedMsg{TerminalID: terminalID, Snapshot: snapshot}
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func paneAttachFailure(paneID, terminalID string, err error) tea.Msg {
	if err == nil {
		return nil
	}
	if paneID == "" && terminalID == "" {
		return err
	}
	return paneAttachFailedMsg{
		PaneID:     paneID,
		TerminalID: terminalID,
		Err:        err,
	}
}

func (m *Model) ensureRecoverablePane() (string, error) {
	if m == nil || m.workbench == nil {
		return "", fmt.Errorf("workbench unavailable")
	}
	if pane := m.workbench.ActivePane(); pane != nil && pane.ID != "" {
		return pane.ID, nil
	}

	ws := m.workbench.CurrentWorkspace()
	if ws == nil {
		return "", fmt.Errorf("no workspace available")
	}
	if tab := m.workbench.CurrentTab(); tab != nil {
		if len(tab.Panes) > 0 {
			if tab.ActivePaneID != "" {
				return tab.ActivePaneID, nil
			}
			return "", fmt.Errorf("current tab has no active pane")
		}
		paneID := shared.NextPaneID()
		if err := m.workbench.CreateFirstPane(tab.ID, paneID); err != nil {
			return "", err
		}
		m.render.Invalidate()
		return paneID, nil
	}

	tabID := shared.NextTabID()
	paneID := shared.NextPaneID()
	name := ws.NextAvailableTabName()
	if err := m.workbench.CreateTab(ws.Name, tabID, name); err != nil {
		return "", err
	}
	if err := m.workbench.CreateFirstPane(tabID, paneID); err != nil {
		return "", err
	}
	_ = m.workbench.SwitchTab(ws.Name, len(ws.Tabs)-1)
	m.render.Invalidate()
	return paneID, nil
}

func (m *Model) openPickerForPaneCmd(paneID string) tea.Cmd {
	if m == nil || m.modalHost == nil || paneID == "" {
		return nil
	}
	m.resetPickerState()
	m.startLoadingModal(input.ModePicker, paneID)
	m.render.Invalidate()
	return m.effectCmd(orchestrator.LoadPickerItemsEffect{})
}

func (m *Model) resetPaneScrollOffset(tabID, paneID string) {
	if m == nil || m.workbench == nil || paneID == "" {
		return
	}
	workspace := m.workbench.CurrentWorkspace()
	if workspace == nil {
		return
	}
	for _, tab := range workspace.Tabs {
		if tab == nil {
			continue
		}
		if tabID != "" && tab.ID != tabID {
			continue
		}
		if tab.Panes[paneID] == nil {
			continue
		}
		if tab.ScrollOffset != 0 {
			tab.ScrollOffset = 0
			m.render.Invalidate()
		}
		return
	}
}

func (m *Model) openTerminalManagerCmd() tea.Cmd {
	if m == nil {
		return nil
	}
	m.openTerminalPool()
	m.render.Invalidate()
	return m.loadTerminalManagerItemsCmd()
}
