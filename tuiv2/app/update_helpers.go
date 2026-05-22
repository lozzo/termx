package app

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/orchestrator"
	"github.com/lozzow/termx/tuiv2/render"
	appruntime "github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/lozzow/termx/tuiv2/workbench"
)

var errorClearDelay = 3 * time.Second
var ownerConfirmDelay = 400 * time.Millisecond
var sharedTerminalSnapshotResyncDelay = 120 * time.Millisecond

const (
	defaultTerminalSnapshotScrollbackLimit = 0
	terminalHistoryInitialPageLimit        = 500
	terminalScrollbackPageLimit            = 500
	terminalScrollbackPrefetchMargin       = 8
	terminalMaterializedScrollbackLimit    = 12000
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
	if os.Getenv("TERMX_ENABLE_HOST_VERTICAL_SCROLL") != "1" {
		return verticalScrollModeNone, "host_vertical_scroll_disabled"
	}
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
	for _, pane := range m.visibleFloatingPanesForInput(visible) {
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
	for _, pane := range m.visibleFloatingPanesForInput(visible) {
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
	if terminal == nil {
		return nil
	}
	if terminal.VTerm != nil && terminal.VTerm.Modes().AlternateScreen {
		return nil
	}
	if terminal.VTerm == nil && terminal.Snapshot != nil && terminal.Snapshot.Modes.AlternateScreen {
		return nil
	}
	copyModeState, copyModeActive := m.copyModeStateForPane(pane.ID)
	copyModeActive = copyModeActive && copyModeState.Snapshot != nil
	if copyModeActive {
		// A frozen copy-mode pane owns its own paged history requests. Letting
		// the live-pane prefetch path race here steals CommittedLoadingDepth
		// and converts a copy-mode older-page request into a runtime request.
		return nil
	}
	loaded := 0
	materializedRows := 0
	if terminal.Snapshot != nil {
		loaded = terminalLiveCommittedLoadedDepth(terminal)
		materializedRows = len(terminal.Snapshot.Scrollback)
	} else {
		loaded = terminal.CommittedLoadedDepth
	}
	if terminal.CommittedHistoryExhausted {
		if !terminalHasKnownScrollbackBeyond(terminal, loaded) {
			return nil
		}
		terminal.CommittedHistoryExhausted = false
	}
	want := viewportOffset + contentRect.H + terminalScrollbackPrefetchMargin
	visibleDepth := loaded
	if materializedRows > 0 && loaded > materializedRows {
		visibleDepth = materializedRows
	}
	if want <= visibleDepth {
		return nil
	}
	nextLimit := loaded + terminalScrollbackPageLimit
	if nextLimit < terminalHistoryInitialPageLimit {
		nextLimit = terminalHistoryInitialPageLimit
	}
	if nextLimit < want {
		nextLimit = minInt(want, loaded+terminalScrollbackPageLimit)
	}
	if nextLimit <= loaded || terminal.CommittedLoadingDepth >= nextLimit {
		return nil
	}
	terminal.CommittedLoadingDepth = nextLimit
	m.setHistoryLoadingOwner(pane.TerminalID, nextLimit, historyLoadingOwnerLive)
	return m.loadTerminalHistoryViewportCmd(pane.TerminalID, loaded, nextLimit-loaded, terminalCanonicalCols(terminal), false)
}

func (m *Model) ensureCopyModeScrollbackCmd(buffer copyModeBuffer) tea.Cmd {
	return m.copyModeScrollbackCmd(buffer, false)
}

func (m *Model) prefetchCopyModeScrollbackCmd(buffer copyModeBuffer) tea.Cmd {
	return m.copyModeScrollbackCmd(buffer, true)
}

func (m *Model) copyModeScrollbackCmd(buffer copyModeBuffer, force bool) tea.Cmd {
	if m == nil || m.workbench == nil || m.runtime == nil || m.copyMode.PaneID == "" || buffer.snapshot == nil {
		return nil
	}
	if snapshotUsesAlternateScreen(buffer.snapshot) {
		return nil
	}
	pane, _, ok := m.copyModePaneAndContentRect(m.copyMode.PaneID)
	if !ok || pane == nil || pane.ID != m.copyMode.PaneID || pane.TerminalID == "" {
		return nil
	}
	if !force && m.copyMode.Cursor.Row > terminalScrollbackPrefetchMargin && m.copyMode.ViewTopRow > terminalScrollbackPrefetchMargin {
		return nil
	}
	terminal := m.runtime.Registry().Get(pane.TerminalID)
	if terminal == nil {
		return nil
	}
	loaded := maxInt(snapshotScrollbackLoadedDepth(buffer.snapshot), m.copyMode.CommittedLoadedRows)
	if terminal.CommittedHistoryExhausted {
		if !terminalHasKnownScrollbackBeyond(terminal, loaded) {
			return nil
		}
		terminal.CommittedHistoryExhausted = false
	}
	nextLimit := loaded + terminalScrollbackPageLimit
	if nextLimit < terminalHistoryInitialPageLimit {
		nextLimit = terminalHistoryInitialPageLimit
	}
	if nextLimit <= loaded || terminal.CommittedLoadingDepth >= nextLimit {
		return nil
	}
	terminal.CommittedLoadingDepth = nextLimit
	m.setHistoryLoadingOwner(pane.TerminalID, nextLimit, historyLoadingOwnerCopyMode)
	return m.loadTerminalHistoryViewportCmd(pane.TerminalID, loaded, nextLimit-loaded, terminalCanonicalCols(terminal), true)
}

func terminalCanonicalCols(terminal *appruntime.TerminalRuntime) int {
	if terminal == nil {
		return 0
	}
	if terminal.Snapshot != nil && terminal.Snapshot.Size.Cols > 0 {
		return int(terminal.Snapshot.Size.Cols)
	}
	if terminal.VTerm != nil {
		cols, _ := terminal.VTerm.Size()
		if cols > 0 {
			return cols
		}
	}
	if terminal.ResizeOwnership != nil && terminal.ResizeOwnership.Size.Cols > 0 {
		return int(terminal.ResizeOwnership.Size.Cols)
	}
	return 0
}

func terminalLiveCommittedLoadedDepth(terminal *appruntime.TerminalRuntime) int {
	if terminal == nil {
		return 0
	}
	loaded := terminal.CommittedLoadedDepth
	if terminal.Snapshot != nil {
		loaded = maxInt(loaded, snapshotScrollbackLoadedDepth(terminal.Snapshot))
	}
	return loaded
}

func terminalHasKnownScrollbackBeyond(terminal *appruntime.TerminalRuntime, loaded int) bool {
	if terminal == nil {
		return false
	}
	known := terminalLiveCommittedLoadedDepth(terminal)
	return known > loaded
}

func (m *Model) loadTerminalHistoryViewportCmd(terminalID string, offset int, limit int, cols int, copyModeRequest bool) tea.Cmd {
	loadingLimit := offset + limit
	owner := historyLoadingOwnerLive
	if copyModeRequest {
		owner = historyLoadingOwnerCopyMode
	}
	return func() tea.Msg {
		snapshot, err := m.runtime.LoadGridViewport(context.Background(), terminalID, offset, limit, cols)
		if err != nil {
			m.clearHistoryLoadingOwner(terminalID, loadingLimit, owner)
			return err
		}
		if terminal := m.clearHistoryLoadingOwner(terminalID, loadingLimit, owner); terminal != nil {
			if snapshot != nil && !copyModeRequest && protocol.HasExplicitRowOwnership(snapshot.ScrollbackOwnership, len(snapshot.Scrollback)) {
				if loadedRows := snapshotScrollbackLoadedDepth(snapshot); loadedRows > terminal.CommittedLoadedDepth {
					terminal.CommittedLoadedDepth = loadedRows
				}
				if limit > 0 {
					terminal.CommittedHistoryExhausted = !snapshot.ScrollbackHasMore
				}
			}
		}
		return orchestrator.SnapshotLoadedMsg{
			TerminalID:      terminalID,
			Snapshot:        snapshot,
			Offset:          offset,
			Limit:           limit,
			Paged:           true,
			CopyModeRequest: copyModeRequest,
		}
	}
}

func (m *Model) setHistoryLoadingOwner(terminalID string, limit int, owner historyLoadingOwner) {
	if m == nil || terminalID == "" || limit <= 0 {
		return
	}
	if m.historyLoading == nil {
		m.historyLoading = make(map[string]historyLoadingState)
	}
	m.historyLoading[terminalID] = historyLoadingState{Limit: limit, Owner: owner}
}

func (m *Model) clearHistoryLoadingOwner(terminalID string, limit int, owner historyLoadingOwner) *appruntime.TerminalRuntime {
	if m == nil || m.runtime == nil || terminalID == "" || limit <= 0 {
		return nil
	}
	terminal := m.runtime.Registry().Get(terminalID)
	state, ok := m.historyLoading[terminalID]
	if !ok || state.Limit != limit || state.Owner != owner {
		return nil
	}
	delete(m.historyLoading, terminalID)
	if terminal != nil && terminal.CommittedLoadingDepth == limit {
		terminal.CommittedLoadingDepth = 0
	}
	return terminal
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
