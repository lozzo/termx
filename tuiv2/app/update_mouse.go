package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/render"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func (m *Model) handleMouseMsg(msg tea.MouseMsg) tea.Cmd {
	switch msg.Action {
	case tea.MouseActionPress:
		if msg.Button == tea.MouseButtonLeft {
			return m.handleMouseClick(msg)
		}
		if msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown {
			return m.handleMouseWheel(msg)
		}
		return m.forwardTerminalMouseInputCmd(msg)
	case tea.MouseActionMotion:
		if msg.Button == tea.MouseButtonLeft && m.mouseDragMode != mouseDragNone {
			return m.handleMouseDrag(msg.X, msg.Y)
		}
		return m.forwardTerminalMouseInputCmd(msg)
	case tea.MouseActionRelease:
		if msg.Button == tea.MouseButtonLeft && m.mouseDragMode != mouseDragNone {
			return m.handleMouseRelease()
		}
		return m.forwardTerminalMouseInputCmd(msg)
	}
	return nil
}

func (m *Model) handleMouseWheel(msg tea.MouseMsg) tea.Cmd {
	if m == nil || m.workbench == nil {
		return nil
	}
	x := msg.X
	y := msg.Y
	button := msg.Button
	delta := -1
	if button == tea.MouseButtonWheelUp {
		delta = 1
	}
	state := m.visibleRenderState()
	if handled, cmd := m.handleOverlayMouseWheel(state, delta); handled {
		return cmd
	}
	if handled, cmd := m.handleTerminalPoolMouseWheel(state, delta); handled {
		return cmd
	}
	if y < m.contentOriginY() {
		if delta > 0 {
			return m.switchCurrentTabByOffsetMouse(-1)
		}
		return m.switchCurrentTabByOffsetMouse(1)
	}
	if cmd := m.forwardTerminalMouseInputCmd(msg); cmd != nil {
		return cmd
	}

	contentY := y - m.contentOriginY()
	if contentY < 0 {
		return nil
	}
	if _, floating, ok := m.visiblePaneAt(x, contentY); ok {
		tab := m.workbench.CurrentTab()
		if tab == nil {
			return nil
		}
		if floating != nil {
			if tab.ActivePaneID != floating.ID {
				_ = m.workbench.FocusPane(tab.ID, floating.ID)
				m.workbench.ReorderFloatingPane(tab.ID, floating.ID, true)
			}
		}
		tab.ScrollOffset += delta
		if tab.ScrollOffset < 0 {
			tab.ScrollOffset = 0
		}
		m.render.Invalidate()
		return m.ensureActivePaneScrollbackCmd()
	}
	return nil
}

func (m *Model) handleMouseClickNonFloating(x, y int) tea.Cmd {
	if m.workbench == nil {
		return nil
	}

	tab := m.workbench.CurrentTab()
	if tab == nil {
		return nil
	}

	contentY := y - m.contentOriginY()
	if contentY < 0 {
		return nil
	}

	bodyRect := m.bodyRect()
	visible := m.workbench.VisibleWithSize(bodyRect)
	if visible == nil || visible.ActiveTab < 0 || visible.ActiveTab >= len(visible.Tabs) {
		return nil
	}

	visibleTab := visible.Tabs[visible.ActiveTab]
	for _, pane := range visibleTab.Panes {
		rect := pane.Rect
		if x >= rect.X && x < rect.X+rect.W && contentY >= rect.Y && contentY < rect.Y+rect.H {
			if region, ok := m.mousePaneChromeRegion(pane, x, contentY); ok {
				if pane.ID != tab.ActivePaneID {
					_ = m.workbench.FocusPane(tab.ID, pane.ID)
				}
				m.render.Invalidate()
				return m.handlePaneChromeRegion(region)
			}
			if cmd := m.handleEmptyPaneClick(pane, x, contentY); cmd != nil {
				if pane.ID != tab.ActivePaneID {
					_ = m.workbench.FocusPane(tab.ID, pane.ID)
				}
				m.render.Invalidate()
				return cmd
			}
		}
	}

	if tab.Root != nil {
		if hit, ok := tab.Root.DividerAt(bodyRect, x, contentY); ok {
			m.mouseDragPaneID = ""
			m.mouseDragMode = mouseDragResizeSplit
			m.mouseDragSplit = hit.Node
			m.mouseDragBounds = hit.Root
			m.mouseDragOffsetX = x - hit.Rect.X
			m.mouseDragOffsetY = contentY - hit.Rect.Y
			return nil
		}
	}

	tiled, _, ok := m.visiblePaneAt(x, contentY)
	if !ok || tiled == nil {
		return nil
	}
	if tiled.ID != tab.ActivePaneID {
		_ = m.workbench.FocusPane(tab.ID, tiled.ID)
		m.render.Invalidate()
	}
	return nil
}

func (m *Model) handleMouseClick(msg tea.MouseMsg) tea.Cmd {
	x := msg.X
	y := msg.Y
	state := m.visibleRenderState()
	if handled, cmd := m.handleOverlayMouseClick(state, x, y); handled {
		return cmd
	}
	if handled, cmd := m.handleTerminalPoolMouseClick(state, x, y); handled {
		return cmd
	}
	if handled, cmd := m.handleTopChromeMouseClick(state, x, y); handled {
		return cmd
	}
	if handled, cmd := m.handleBottomChromeMouseClick(state, x, y); handled {
		return cmd
	}
	if cmd := m.forwardTerminalMouseInputCmd(msg); cmd != nil {
		return cmd
	}

	if m.workbench == nil {
		return nil
	}

	tab := m.workbench.CurrentTab()
	if tab == nil {
		return nil
	}

	contentY := y - m.contentOriginY()
	if contentY < 0 {
		return nil
	}

	paneID, rect, isResize := m.findFloatingPaneAt(tab, x, contentY)
	if paneID != "" {
		bodyRect := m.bodyRect()
		visible := m.workbench.VisibleWithSize(bodyRect)
		if visible != nil {
			for _, pane := range visible.FloatingPanes {
				if pane.ID != paneID {
					continue
				}
				if region, ok := m.mousePaneChromeRegion(pane, x, contentY); ok {
					if tab.ActivePaneID != paneID {
						_ = m.workbench.FocusPane(tab.ID, paneID)
					}
					m.workbench.ReorderFloatingPane(tab.ID, paneID, true)
					m.render.Invalidate()
					return m.handlePaneChromeRegion(region)
				}
				if cmd := m.handleEmptyPaneClick(pane, x, contentY); cmd != nil {
					if tab.ActivePaneID != paneID {
						_ = m.workbench.FocusPane(tab.ID, paneID)
					}
					m.workbench.ReorderFloatingPane(tab.ID, paneID, true)
					m.render.Invalidate()
					return cmd
				}
				break
			}
		}
		if tab.ActivePaneID != paneID {
			_ = m.workbench.FocusPane(tab.ID, paneID)
		}
		m.workbench.ReorderFloatingPane(tab.ID, paneID, true)

		m.mouseDragPaneID = paneID
		m.mouseDragSplit = nil
		m.mouseDragBounds = workbench.Rect{}
		if isResize {
			m.mouseDragMode = mouseDragResize
			m.mouseDragOffsetX = 0
			m.mouseDragOffsetY = 0
		} else if contentY == rect.Y {
			m.mouseDragMode = mouseDragMove
			m.mouseDragOffsetX = x - rect.X
			m.mouseDragOffsetY = contentY - rect.Y
		} else {
			m.mouseDragPaneID = ""
			m.mouseDragMode = mouseDragNone
		}
		m.render.Invalidate()
		return nil
	}

	return m.handleMouseClickNonFloating(x, y)
}

func (m *Model) handleMouseDrag(x, y int) tea.Cmd {
	if m.workbench == nil || m.mouseDragMode == mouseDragNone {
		return nil
	}

	tab := m.workbench.CurrentTab()
	if tab == nil {
		return nil
	}

	contentY := y - m.contentOriginY()
	if contentY < 0 {
		contentY = 0
	}

	switch m.mouseDragMode {
	case mouseDragMove:
		if m.mouseDragPaneID == "" {
			return nil
		}
		newX := x - m.mouseDragOffsetX
		newY := contentY - m.mouseDragOffsetY
		m.workbench.MoveFloatingPane(tab.ID, m.mouseDragPaneID, newX, newY)
		m.workbench.ClampFloatingPanesToBounds(m.bodyRect())
		m.render.Invalidate()
	case mouseDragResize:
		if m.mouseDragPaneID == "" {
			return nil
		}
		for _, floating := range tab.Floating {
			if floating != nil && floating.PaneID == m.mouseDragPaneID {
				newW := x - floating.Rect.X + 1
				newH := contentY - floating.Rect.Y + 1
				m.workbench.ResizeFloatingPane(tab.ID, m.mouseDragPaneID, newW, newH)
				m.workbench.ClampFloatingPanesToBounds(m.bodyRect())
				m.render.Invalidate()
				return m.resizePaneIfNeededCmd(m.mouseDragPaneID)
			}
		}
	case mouseDragResizeSplit:
		if m.mouseDragSplit == nil {
			return nil
		}
		if !m.workbench.ResizeSplit(tab.ID, m.mouseDragSplit, m.mouseDragBounds, x, contentY, m.mouseDragOffsetX, m.mouseDragOffsetY) {
			return nil
		}
		m.render.Invalidate()
		return m.resizeVisiblePanesCmd()
	}

	return nil
}

func (m *Model) handleMouseRelease() tea.Cmd {
	m.mouseDragPaneID = ""
	m.mouseDragOffsetX = 0
	m.mouseDragOffsetY = 0
	m.mouseDragMode = mouseDragNone
	m.mouseDragSplit = nil
	m.mouseDragBounds = workbench.Rect{}
	return nil
}

func (m *Model) findFloatingPaneAt(tab *workbench.TabState, x, y int) (string, workbench.Rect, bool) {
	if tab == nil || len(tab.Floating) == 0 {
		return "", workbench.Rect{}, false
	}

	for i := len(tab.Floating) - 1; i >= 0; i-- {
		floating := tab.Floating[i]
		if floating == nil {
			continue
		}

		rect := floating.Rect
		if x >= rect.X && x < rect.X+rect.W && y >= rect.Y && y < rect.Y+rect.H {
			isResize := x >= rect.X+rect.W-2 && y >= rect.Y+rect.H-2
			return floating.PaneID, rect, isResize
		}
	}

	return "", workbench.Rect{}, false
}

func (m *Model) mouseHitsOwnerButton(pane workbench.VisiblePane, x, contentY int) bool {
	region, ok := m.mousePaneChromeRegion(pane, x, contentY)
	if !ok {
		return false
	}
	return region.Kind == render.HitRegionPaneOwner
}

func (m *Model) becomeOwnerCmd(paneID string) tea.Cmd {
	if strings.TrimSpace(paneID) == "" {
		return nil
	}
	return func() tea.Msg {
		return input.SemanticAction{Kind: input.ActionBecomeOwner, PaneID: paneID}
	}
}

func (m *Model) handleOwnerActionClick(paneID string) tea.Cmd {
	if m == nil || strings.TrimSpace(paneID) == "" {
		return nil
	}
	if m.ownerConfirmPaneID == paneID {
		m.ownerConfirmPaneID = ""
		m.ownerSeq++
		return m.becomeOwnerCmd(paneID)
	}
	m.ownerConfirmPaneID = paneID
	m.ownerSeq++
	m.render.Invalidate()
	return clearOwnerConfirmCmd(m.ownerSeq)
}

func (m *Model) mousePaneChromeRegion(pane workbench.VisiblePane, x, contentY int) (render.HitRegion, bool) {
	if m == nil {
		return render.HitRegion{}, false
	}
	var runtimeState *render.VisibleRuntimeStateProxy
	if m.runtime != nil {
		runtimeState = m.runtime.Visible()
	}
	return render.HitRegionAt(render.PaneChromeHitRegions(pane, runtimeState, m.ownerConfirmPaneID), x, contentY)
}

func (m *Model) handlePaneChromeRegion(region render.HitRegion) tea.Cmd {
	if m == nil {
		return nil
	}
	if region.Kind == render.HitRegionPaneOwner {
		return m.handleOwnerActionClick(region.PaneID)
	}
	return m.applyMouseSemanticAction(region.Action)
}

func (m *Model) ensureFloatingModeTarget() {
	if m == nil || m.workbench == nil {
		return
	}
	tab := m.workbench.CurrentTab()
	if tab == nil || len(tab.Floating) == 0 {
		return
	}
	if active := activeFloatingPaneID(tab); active != "" {
		return
	}
	paneID := topmostFloatingPaneID(tab)
	if paneID == "" {
		return
	}
	_ = m.workbench.FocusPane(tab.ID, paneID)
	m.workbench.ReorderFloatingPane(tab.ID, paneID, true)
}

func activeFloatingPaneID(tab *workbench.TabState) string {
	if tab == nil || tab.ActivePaneID == "" {
		return ""
	}
	for _, floating := range tab.Floating {
		if floating != nil && floating.PaneID == tab.ActivePaneID &&
			(floating.Display == "" || floating.Display == workbench.FloatingDisplayExpanded) {
			return tab.ActivePaneID
		}
	}
	return ""
}

func topmostFloatingPaneID(tab *workbench.TabState) string {
	if tab == nil || len(tab.Floating) == 0 {
		return ""
	}
	paneID := ""
	maxZ := 0
	for _, floating := range tab.Floating {
		if floating == nil || floating.PaneID == "" {
			continue
		}
		if floating.Display != "" && floating.Display != workbench.FloatingDisplayExpanded {
			continue
		}
		if paneID == "" || floating.Z >= maxZ {
			paneID = floating.PaneID
			maxZ = floating.Z
		}
	}
	return paneID
}

func (m *Model) handleOverlayMouseClick(state render.VisibleRenderState, x, y int) (bool, tea.Cmd) {
	if state.Overlay.Kind == render.VisibleOverlayNone {
		return false, nil
	}
	bodyY := y - m.contentOriginY()
	if bodyY < 0 {
		return true, nil
	}
	region, ok := render.HitRegionAt(render.OverlayHitRegions(state), x, bodyY)
	if !ok {
		return true, nil
	}
	switch state.Overlay.Kind {
	case render.VisibleOverlayPicker:
		if m.modalHost == nil || m.modalHost.Picker == nil {
			return true, nil
		}
		switch region.Kind {
		case render.HitRegionOverlayDismiss:
			return true, m.cancelActiveModal()
		case render.HitRegionPickerItem:
			m.modalHost.Picker.Selected = region.ItemIndex
			normalizeModalSelection(&m.modalHost.Picker.Selected, len(m.modalHost.Picker.VisibleItems()))
			m.render.Invalidate()
			if m.modalHost.Picker.SelectedItem() == nil {
				return true, nil
			}
			return true, m.dispatchOverlayRegionAction(input.SemanticAction{Kind: input.ActionSubmitPrompt})
		default:
			return true, m.dispatchOverlayRegionAction(region.Action)
		}
	case render.VisibleOverlayWorkspacePicker:
		if m.modalHost == nil || m.modalHost.WorkspacePicker == nil {
			return true, nil
		}
		switch region.Kind {
		case render.HitRegionOverlayDismiss:
			return true, m.cancelActiveModal()
		case render.HitRegionWorkspaceItem:
			m.modalHost.WorkspacePicker.Selected = region.ItemIndex
			normalizeModalSelection(&m.modalHost.WorkspacePicker.Selected, len(m.modalHost.WorkspacePicker.VisibleItems()))
			m.render.Invalidate()
			_, cmd := m.handleModalAction(input.SemanticAction{Kind: input.ActionSubmitPrompt})
			return true, cmd
		default:
			return true, m.dispatchOverlayRegionAction(region.Action)
		}
	case render.VisibleOverlayFloatingOverview:
		if m.modalHost == nil || m.modalHost.FloatingOverview == nil {
			return true, nil
		}
		switch region.Kind {
		case render.HitRegionOverlayDismiss:
			return true, m.cancelActiveModal()
		case render.HitRegionFloatingOverviewItem:
			m.modalHost.FloatingOverview.Selected = region.ItemIndex
			m.render.Invalidate()
			return true, m.dispatchOverlayRegionAction(input.SemanticAction{Kind: input.ActionSubmitPrompt})
		default:
			return true, m.dispatchOverlayRegionAction(region.Action)
		}
	case render.VisibleOverlayPrompt, render.VisibleOverlayHelp, render.VisibleOverlayTerminalManager:
		if region.Kind == render.HitRegionOverlayDismiss {
			return true, m.cancelActiveModal()
		}
		if state.Overlay.Kind == render.VisibleOverlayPrompt && region.Kind == render.HitRegionPromptInput {
			return true, m.handlePromptInputMouseClick(region, x)
		}
		return true, m.dispatchOverlayRegionAction(region.Action)
	default:
		return true, nil
	}
}

func (m *Model) handleOverlayMouseWheel(state render.VisibleRenderState, delta int) (bool, tea.Cmd) {
	switch state.Overlay.Kind {
	case render.VisibleOverlayPicker:
		if m.modalHost != nil && m.modalHost.Picker != nil {
			m.modalHost.Picker.Move(-delta)
			m.render.Invalidate()
		}
		return true, nil
	case render.VisibleOverlayWorkspacePicker:
		if m.modalHost != nil && m.modalHost.WorkspacePicker != nil {
			m.modalHost.WorkspacePicker.Move(-delta)
			m.render.Invalidate()
		}
		return true, nil
	case render.VisibleOverlayFloatingOverview:
		if m.modalHost != nil && m.modalHost.FloatingOverview != nil {
			m.modalHost.FloatingOverview.Move(-delta)
			m.render.Invalidate()
		}
		return true, nil
	case render.VisibleOverlayPrompt, render.VisibleOverlayHelp, render.VisibleOverlayTerminalManager:
		return true, nil
	default:
		return false, nil
	}
}

func (m *Model) handleTerminalPoolMouseClick(state render.VisibleRenderState, x, y int) (bool, tea.Cmd) {
	if m.terminalPage == nil {
		return false, nil
	}
	bodyY := y - m.contentOriginY()
	if bodyY < 0 {
		return true, nil
	}
	region, ok := render.HitRegionAt(render.TerminalPoolHitRegions(state), x, bodyY)
	if !ok {
		return true, nil
	}
	switch region.Kind {
	case render.HitRegionTerminalPoolItem:
		m.terminalPage.Selected = region.ItemIndex
		normalizeModalSelection(&m.terminalPage.Selected, len(m.terminalPage.VisibleItems()))
		m.render.Invalidate()
		return true, nil
	case render.HitRegionTerminalPoolAction:
		handled, cmd := m.handleModalAction(region.Action)
		if handled {
			return true, cmd
		}
		return true, nil
	default:
		return true, nil
	}
}

func (m *Model) handleTerminalPoolMouseWheel(state render.VisibleRenderState, delta int) (bool, tea.Cmd) {
	if m.terminalPage == nil {
		return false, nil
	}
	m.terminalPage.Move(-delta)
	m.render.Invalidate()
	return true, nil
}

func (m *Model) handleTopChromeMouseClick(state render.VisibleRenderState, x, y int) (bool, tea.Cmd) {
	if y != 0 {
		return false, nil
	}
	region, ok := render.HitRegionAt(render.TabBarHitRegions(state), x, y)
	if !ok {
		return false, nil
	}
	switch region.Kind {
	case render.HitRegionTabSwitch:
		return true, m.switchTabByIndexMouse(region.TabIndex)
	default:
		if region.Action.Kind != "" {
			return true, m.applyMouseSemanticAction(region.Action)
		}
		return true, nil
	}
}

func (m *Model) handleBottomChromeMouseClick(state render.VisibleRenderState, x, y int) (bool, tea.Cmd) {
	if m == nil || y != m.height-1 {
		return false, nil
	}
	region, ok := render.HitRegionAt(render.StatusBarHitRegions(state), x, y)
	if !ok {
		return false, nil
	}
	if region.Action.Kind == "" {
		return true, nil
	}
	return true, m.applyMouseSemanticAction(region.Action)
}

func (m *Model) switchTabByIndexMouse(index int) tea.Cmd {
	if m == nil || m.workbench == nil {
		return nil
	}
	ws := m.workbench.CurrentWorkspace()
	if ws == nil {
		return nil
	}
	if err := m.workbench.SwitchTab(ws.Name, index); err != nil {
		return m.showError(err)
	}
	m.render.Invalidate()
	return batchCmds(m.maybeSyncActivePaneInteractiveOwnershipCmd(), m.saveStateCmd())
}

func (m *Model) switchCurrentTabByOffsetMouse(offset int) tea.Cmd {
	if err := m.switchCurrentTabByOffset(offset); err != nil {
		return m.showError(err)
	}
	m.render.Invalidate()
	return batchCmds(m.maybeSyncActivePaneInteractiveOwnershipCmd(), m.saveStateCmd())
}

func (m *Model) handleEmptyPaneClick(pane workbench.VisiblePane, x, contentY int) tea.Cmd {
	region, ok := render.HitRegionAt(render.EmptyPaneActionRegions(pane), x, contentY)
	if !ok {
		return nil
	}
	switch region.Kind {
	case render.HitRegionEmptyPaneAttach:
		return m.applyMouseSemanticAction(input.SemanticAction{Kind: input.ActionOpenPicker, TargetID: pane.ID, PaneID: pane.ID})
	case render.HitRegionEmptyPaneCreate:
		m.openCreateTerminalPrompt(pane.ID, modal.CreateTargetReplace)
		return nil
	case render.HitRegionEmptyPaneManager:
		return m.openTerminalManagerMouse()
	case render.HitRegionEmptyPaneClose:
		return m.applyMouseSemanticAction(input.SemanticAction{Kind: input.ActionClosePane, PaneID: pane.ID})
	default:
		return nil
	}
}

func (m *Model) openTerminalManagerMouse() tea.Cmd {
	if m == nil {
		return nil
	}
	m.terminalPage = &modal.TerminalManagerState{Title: "Terminal Pool"}
	m.input.SetMode(input.ModeState{Kind: input.ModeTerminalManager, RequestID: terminalPoolPageModeToken})
	m.render.Invalidate()
	return m.loadTerminalManagerItemsCmd()
}

func (m *Model) visiblePaneAt(x, contentY int) (*workbench.VisiblePane, *workbench.VisiblePane, bool) {
	if m == nil || m.workbench == nil {
		return nil, nil, false
	}
	bodyRect := m.bodyRect()
	visible := m.workbench.VisibleWithSize(bodyRect)
	if visible == nil {
		return nil, nil, false
	}
	for i := len(visible.FloatingPanes) - 1; i >= 0; i-- {
		pane := &visible.FloatingPanes[i]
		if x >= pane.Rect.X && x < pane.Rect.X+pane.Rect.W && contentY >= pane.Rect.Y && contentY < pane.Rect.Y+pane.Rect.H {
			return nil, pane, true
		}
	}
	if visible.ActiveTab < 0 || visible.ActiveTab >= len(visible.Tabs) {
		return nil, nil, false
	}
	for i := range visible.Tabs[visible.ActiveTab].Panes {
		pane := &visible.Tabs[visible.ActiveTab].Panes[i]
		if x >= pane.Rect.X && x < pane.Rect.X+pane.Rect.W && contentY >= pane.Rect.Y && contentY < pane.Rect.Y+pane.Rect.H {
			return pane, nil, true
		}
	}
	return nil, nil, false
}

func (m *Model) forwardTerminalMouseInputCmd(msg tea.MouseMsg) tea.Cmd {
	if m == nil || m.workbench == nil {
		return nil
	}
	state := m.visibleRenderState()
	if state.Overlay.Kind != render.VisibleOverlayNone || state.Surface.Kind == render.VisibleSurfaceTerminalPool {
		return nil
	}
	targetPaneID, contentRect, ok := m.activeContentMouseTarget(msg.X, msg.Y)
	if !ok {
		return nil
	}
	contentMsg := msg
	contentMsg.Y = msg.Y - m.contentOriginY()
	encoded := m.encodeTerminalMouseInput(contentMsg, targetPaneID, contentRect)
	if len(encoded) == 0 {
		return nil
	}
	return func() tea.Msg {
		return input.TerminalInput{PaneID: targetPaneID, Data: encoded}
	}
}

func (m *Model) activeContentMouseTarget(screenX, screenY int) (string, workbench.Rect, bool) {
	if m == nil || m.workbench == nil || screenY < m.contentOriginY() {
		return "", workbench.Rect{}, false
	}
	tab := m.workbench.CurrentTab()
	if tab == nil || strings.TrimSpace(tab.ActivePaneID) == "" {
		return "", workbench.Rect{}, false
	}
	contentY := screenY - m.contentOriginY()
	tiled, floating, ok := m.visiblePaneAt(screenX, contentY)
	if !ok {
		return "", workbench.Rect{}, false
	}
	var pane *workbench.VisiblePane
	if floating != nil {
		pane = floating
	} else {
		pane = tiled
	}
	if pane == nil || pane.ID != tab.ActivePaneID {
		return "", workbench.Rect{}, false
	}
	contentRect, ok := paneContentRect(pane.Rect)
	if !ok || !pointInMouseRect(contentRect, screenX, contentY) {
		return "", workbench.Rect{}, false
	}
	return pane.ID, contentRect, true
}

func paneContentRect(rect workbench.Rect) (workbench.Rect, bool) {
	contentRect := workbench.Rect{
		X: rect.X + 1,
		Y: rect.Y + 1,
		W: rect.W - 2,
		H: rect.H - 2,
	}
	if contentRect.W <= 0 || contentRect.H <= 0 {
		return workbench.Rect{}, false
	}
	return contentRect, true
}

func pointInMouseRect(rect workbench.Rect, x, y int) bool {
	return rect.W > 0 && rect.H > 0 &&
		x >= rect.X && x < rect.X+rect.W &&
		y >= rect.Y && y < rect.Y+rect.H
}

func (m *Model) dispatchOverlayRegionAction(action input.SemanticAction) tea.Cmd {
	if m == nil || action.Kind == "" {
		return nil
	}
	action = m.resolveMouseActionScope(action)
	if handled, cmd := m.handleModalAction(action); handled {
		return cmd
	}
	return m.applyMouseSemanticAction(action)
}

func (m *Model) handlePromptInputMouseClick(region render.HitRegion, screenX int) tea.Cmd {
	if m == nil || m.modalHost == nil || m.modalHost.Prompt == nil {
		return nil
	}
	if m.modalHost.Prompt.IsForm() && region.ItemIndex >= 0 && region.ItemIndex < len(m.modalHost.Prompt.Fields) {
		m.modalHost.Prompt.ActiveField = region.ItemIndex
	}
	cursor := screenX - region.Rect.X
	if cursor < 0 {
		cursor = 0
	}
	if setPromptCursor(m.modalHost.Prompt, cursor) {
		m.render.Invalidate()
	}
	return nil
}

func (m *Model) resolveMouseActionScope(action input.SemanticAction) input.SemanticAction {
	if m == nil {
		return action
	}
	if action.Kind == input.ActionSubmitPrompt && m.modalHost != nil && m.modalHost.Session != nil &&
		m.modalHost.Session.Kind == input.ModePicker && m.modalHost.Picker != nil {
		if selected := m.modalHost.Picker.SelectedItem(); selected != nil && action.TargetID == "" {
			action.TargetID = selected.TerminalID
		}
	}
	if action.PaneID == "" && m.workbench != nil {
		if pane := m.workbench.ActivePane(); pane != nil {
			action.PaneID = pane.ID
		}
	}
	return action
}
