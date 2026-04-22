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
	mode, _ := m.verticalScrollOptimizationMode()
	return mode != verticalScrollModeNone
}

func (m *Model) verticalScrollOptimizationMode() (verticalScrollMode, string) {
	if m == nil || m.workbench == nil {
		return verticalScrollModeNone, "model_unavailable"
	}
	vm := m.renderVM()
	return verticalScrollOptimizationModeForVisible(m.bodyRect(), vm.Surface.Kind, vm.Overlay.Kind, vm.Workbench)
}

func verticalScrollOptimizationModeForVisible(body workbench.Rect, surfaceKind render.VisibleSurfaceKind, overlayKind render.VisibleOverlayKind, visible *workbench.VisibleWorkbench) (verticalScrollMode, string) {
	if surfaceKind != render.VisibleSurfaceWorkbench || overlayKind != render.VisibleOverlayNone || visible == nil {
		if surfaceKind != render.VisibleSurfaceWorkbench {
			return verticalScrollModeNone, "non_workbench_surface"
		}
		if overlayKind != render.VisibleOverlayNone {
			return verticalScrollModeNone, "overlay_active"
		}
		return verticalScrollModeNone, "workbench_unavailable"
	}
	if len(visible.FloatingPanes) > 0 {
		return verticalScrollModeNone, "floating_visible"
	}
	activeTab := visible.ActiveTab
	if activeTab < 0 || activeTab >= len(visible.Tabs) {
		return verticalScrollModeNone, "no_active_tab"
	}
	panes := visible.Tabs[activeTab].Panes
	if len(panes) == 0 {
		return verticalScrollModeNone, "no_panes"
	}
	if len(panes) == 1 {
		return verticalScrollModeRowsAndRects, "single_pane"
	}
	if body.W <= 0 || body.H <= 0 {
		return verticalScrollModeNone, "invalid_body_rect"
	}
	contentRects := make([]workbench.Rect, 0, len(panes))
	fullWidthStacked := true
	rowOwners := make([]int, body.H)
	for _, pane := range panes {
		contentRect, ok := paneContentRectForVisible(pane)
		if !ok || contentRect.W <= 0 || contentRect.H <= 0 {
			return verticalScrollModeNone, "invalid_content_rect"
		}
		if contentRect.X < 0 || contentRect.Y < 0 || contentRect.X+contentRect.W > body.W || contentRect.Y+contentRect.H > body.H {
			return verticalScrollModeNone, "content_out_of_bounds"
		}
		if pane.Rect.X != 0 || pane.Rect.W != body.W {
			fullWidthStacked = false
		}
		for _, prev := range contentRects {
			if rectsOverlap(prev, contentRect) {
				return verticalScrollModeNone, "content_overlap"
			}
		}
		contentRects = append(contentRects, contentRect)
		if !fullWidthStacked {
			continue
		}
		start := maxInt(0, contentRect.Y)
		end := minInt(body.H, contentRect.Y+contentRect.H)
		for row := start; row < end; row++ {
			rowOwners[row]++
			if rowOwners[row] > 1 {
				return verticalScrollModeNone, "row_overlap"
			}
		}
	}
	if fullWidthStacked {
		return verticalScrollModeRowsAndRects, "stacked_full_width"
	}
	return verticalScrollModeRectsOnly, "tiled_partial_width"
}

func (m *Model) immersiveZoomActive() bool {
	if m == nil || m.workbench == nil {
		return false
	}
	tab := m.workbench.CurrentTab()
	return tab != nil && tab.ZoomedPaneID != ""
}

func (m *Model) activePaneAlternateScreen() bool {
	if m == nil || m.workbench == nil {
		return false
	}
	pane := m.workbench.ActivePane()
	if pane == nil || pane.TerminalID == "" {
		return false
	}
	return m.terminalModesForPane(pane).AlternateScreen
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
		// resize math and draw math diverge by even one column/divider
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
	pane := m.workbench.ActivePane()
	if pane == nil || pane.TerminalID == "" {
		return nil
	}
	viewportOffset := m.paneViewportOffset(pane.ID)
	if viewportOffset <= 0 {
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
	want := viewportOffset + contentRect.H + terminalScrollbackPrefetchMargin
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

func rectsOverlap(a, b workbench.Rect) bool {
	if a.W <= 0 || a.H <= 0 || b.W <= 0 || b.H <= 0 {
		return false
	}
	return a.X < b.X+b.W &&
		b.X < a.X+a.W &&
		a.Y < b.Y+b.H &&
		b.Y < a.Y+a.H
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

func (m *Model) openTerminalManagerCmd() tea.Cmd {
	if m == nil {
		return nil
	}
	m.openTerminalPool()
	m.render.Invalidate()
	return m.loadTerminalManagerItemsCmd()
}
