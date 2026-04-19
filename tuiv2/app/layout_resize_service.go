package app

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/render"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/workbench"
)

type layoutResizeService struct {
	model *Model
}

const (
	layoutResizeFloatingMoveStep   = 2
	layoutResizeFloatingResizeStep = 2
	layoutResizeFloatingBoundsW    = 200
	layoutResizeFloatingBoundsH    = 50
)

func (m *Model) layoutResizeService() *layoutResizeService {
	if m == nil {
		return nil
	}
	return &layoutResizeService{model: m}
}

func (s *layoutResizeService) resizeVisibleCmd() tea.Cmd {
	if s == nil || s.model == nil || s.model.runtime == nil || s.model.workbench == nil {
		return nil
	}
	return func() tea.Msg {
		if err := s.resizeVisible(context.Background()); err != nil {
			return err
		}
		return renderRefreshMsg{}
	}
}

func (s *layoutResizeService) resizeVisible(ctx context.Context) error {
	if s == nil || s.model == nil || s.model.runtime == nil || s.model.workbench == nil {
		return nil
	}
	bodyRect := s.model.bodyRect()
	visible := s.model.workbench.VisibleWithSize(bodyRect)
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
		if s.model.sessionID != "" && pane.ID == tab.ActivePaneID {
			req.ImplicitSessionLease = true
		}
		if err := s.model.syncTerminalInteraction(ctx, req, target); err != nil {
			return err
		}
	}
	return nil
}

func (s *layoutResizeService) ensurePaneTerminalSize(ctx context.Context, paneID, terminalID string, rect workbench.Rect) error {
	if s == nil || s.model == nil {
		return nil
	}
	return s.model.syncTerminalInteraction(ctx, terminalInteractionRequest{
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

func (s *layoutResizeService) resizePaneIfNeededCmd(paneID string) tea.Cmd {
	if s == nil || s.model == nil || s.model.runtime == nil || s.model.workbench == nil {
		return nil
	}
	target := s.model.currentOrActionPaneID(paneID)
	if target == "" {
		return nil
	}
	pane, rect, ok := s.model.visiblePaneForInput(target)
	if !ok || pane == nil || pane.TerminalID == "" {
		return nil
	}
	return func() tea.Msg {
		if err := s.ensurePaneTerminalSize(context.Background(), pane.ID, pane.TerminalID, rect); err != nil {
			return err
		}
		return renderRefreshMsg{}
	}
}

func (s *layoutResizeService) syncActivePaneTabSwitchTakeoverCmd() tea.Cmd {
	if s == nil || s.model == nil || !s.model.localActivePaneNeedsOwnershipForResize() {
		return nil
	}
	req := terminalInteractionRequest{
		ResizeIfNeeded:   true,
		ExplicitTakeover: true,
	}
	target, ok := s.model.resolveTerminalInteractionTarget(req)
	if !ok {
		return nil
	}
	return func() tea.Msg {
		if err := s.model.syncTerminalInteraction(context.Background(), req, target); err != nil {
			return err
		}
		if !s.pendingPaneResizeSatisfied(target.paneID, target.terminalID, target.rect) {
			s.markPendingPaneResize("", target.paneID, target.terminalID)
		}
		return nil
	}
}

func (s *layoutResizeService) markPendingPaneResize(tabID, paneID, terminalID string) {
	if s == nil || s.model == nil || paneID == "" || terminalID == "" {
		return
	}
	if s.model.pendingPaneResizes == nil {
		s.model.pendingPaneResizes = make(map[string]pendingPaneResize)
	}
	s.model.pendingPaneResizes[paneID] = pendingPaneResize{
		TabID:      tabID,
		PaneID:     paneID,
		TerminalID: terminalID,
	}
}

func (s *layoutResizeService) clearPendingPaneResize(paneID, terminalID string) {
	if s == nil || s.model == nil || len(s.model.pendingPaneResizes) == 0 || paneID == "" {
		return
	}
	current, ok := s.model.pendingPaneResizes[paneID]
	if !ok {
		return
	}
	if terminalID != "" && current.TerminalID != "" && current.TerminalID != terminalID {
		return
	}
	delete(s.model.pendingPaneResizes, paneID)
}

func (s *layoutResizeService) paneResizeTarget(tabID, paneID string) (*workbench.PaneState, workbench.Rect, bool) {
	if s == nil || s.model == nil || s.model.workbench == nil || paneID == "" || !s.model.hasViewportSize() {
		return nil, workbench.Rect{}, false
	}
	workspace := s.model.workbench.CurrentWorkspace()
	if workspace == nil {
		return nil, workbench.Rect{}, false
	}
	var tabState *workbench.TabState
	if tabID != "" {
		for _, tab := range workspace.Tabs {
			if tab != nil && tab.ID == tabID {
				tabState = tab
				break
			}
		}
	} else {
		current := s.model.workbench.CurrentTab()
		if current != nil && current.Panes[paneID] != nil {
			tabState = current
		}
		if tabState == nil {
			for _, tab := range workspace.Tabs {
				if tab != nil && tab.Panes[paneID] != nil {
					tabState = tab
					break
				}
			}
		}
	}
	if tabState == nil {
		return nil, workbench.Rect{}, false
	}
	pane := tabState.Panes[paneID]
	if pane == nil {
		return nil, workbench.Rect{}, false
	}
	visible := s.model.workbench.VisibleWithSize(s.model.bodyRect())
	if visible == nil {
		return nil, workbench.Rect{}, false
	}
	currentTab := s.model.workbench.CurrentTab()
	for _, floating := range tabState.Floating {
		if floating == nil || floating.PaneID != paneID {
			continue
		}
		if currentTab != nil && currentTab.ID == tabState.ID {
			for i := range visible.FloatingPanes {
				if visible.FloatingPanes[i].ID == paneID {
					return pane, visible.FloatingPanes[i].Rect, true
				}
			}
		}
		display := floating.Display
		if display == "" {
			display = workbench.FloatingDisplayExpanded
		}
		if display != workbench.FloatingDisplayExpanded || floating.Rect.W <= 0 || floating.Rect.H <= 0 {
			return nil, workbench.Rect{}, false
		}
		return pane, floating.Rect, true
	}
	for _, tab := range visible.Tabs {
		if tab.ID != tabState.ID {
			continue
		}
		for _, visiblePane := range tab.Panes {
			if visiblePane.ID == paneID && visiblePane.Rect.W > 0 && visiblePane.Rect.H > 0 {
				return pane, visiblePane.Rect, true
			}
		}
		return nil, workbench.Rect{}, false
	}
	return nil, workbench.Rect{}, false
}

func (s *layoutResizeService) resizePendingCmd() tea.Cmd {
	if s == nil || s.model == nil || s.model.runtime == nil || s.model.sessionID != "" || len(s.model.pendingPaneResizes) == 0 || !s.model.hasViewportSize() {
		return nil
	}
	pending := make([]pendingPaneResize, 0, len(s.model.pendingPaneResizes))
	for _, resize := range s.model.pendingPaneResizes {
		pending = append(pending, resize)
	}
	cmds := make([]tea.Cmd, 0, len(pending))
	for _, resize := range pending {
		pane, rect, ok := s.paneResizeTarget(resize.TabID, resize.PaneID)
		if !ok || pane == nil {
			s.clearPendingPaneResize(resize.PaneID, resize.TerminalID)
			continue
		}
		if pane.TerminalID == "" || pane.TerminalID != resize.TerminalID {
			s.clearPendingPaneResize(resize.PaneID, resize.TerminalID)
			continue
		}
		target := resize
		targetRect := rect
		cmds = append(cmds, func() tea.Msg {
			if err := s.ensurePaneTerminalSize(context.Background(), target.PaneID, target.TerminalID, targetRect); err != nil {
				return err
			}
			if s.pendingPaneResizeSatisfied(target.PaneID, target.TerminalID, targetRect) {
				s.clearPendingPaneResize(target.PaneID, target.TerminalID)
			}
			return renderRefreshMsg{}
		})
	}
	return batchCmds(cmds...)
}

func (s *layoutResizeService) pendingPaneResizeSatisfied(paneID, terminalID string, rect workbench.Rect) bool {
	if s == nil || s.model == nil || terminalID == "" {
		return false
	}
	viewportRect, ok := s.model.terminalViewportRect(paneID, rect)
	if !ok {
		return false
	}
	cols := uint16(maxInt(2, viewportRect.W))
	rows := uint16(maxInt(2, viewportRect.H))
	return s.model.terminalAlreadySized(terminalID, cols, rows)
}

func (s *layoutResizeService) applyWindowSizeMsg(typed tea.WindowSizeMsg) tea.Cmd {
	if s == nil || s.model == nil {
		return nil
	}
	oldBodyRect := s.model.bodyRect()
	newBodyRect := workbench.Rect{W: maxInt(1, typed.Width), H: render.FrameBodyHeight(typed.Height)}
	if s.model.workbench != nil {
		if s.model.width > 0 && s.model.height > 0 {
			s.model.workbench.ReflowFloatingPanes(oldBodyRect, newBodyRect)
		} else {
			s.model.workbench.ClampFloatingPanesToBounds(newBodyRect)
		}
	}
	s.model.width = typed.Width
	s.model.height = typed.Height
	if writer, ok := s.model.frameOut.(*outputCursorWriter); ok {
		writer.SetTTYWidth(typed.Width)
	}
	if writer, ok := s.model.cursorOut.(*outputCursorWriter); ok {
		writer.SetTTYWidth(typed.Width)
	}
	s.model.render.Invalidate()
	return batchCmds(s.resizeVisibleCmd(), s.resizePendingCmd(), s.model.maybeAutoFitFloatingPanesCmd(), s.model.updateSessionViewCmd())
}

func (s *layoutResizeService) moveFloatingPane(tabID, paneID string, x, y int) bool {
	if s == nil || s.model == nil || s.model.workbench == nil || tabID == "" || paneID == "" {
		return false
	}
	moved := s.model.workbench.MoveFloatingPane(tabID, paneID, x, y)
	clamped := s.model.workbench.ClampFloatingPanesToBounds(s.model.bodyRect())
	return moved || clamped
}

func (s *layoutResizeService) resizeFloatingPane(tabID, paneID string, width, height int) bool {
	if s == nil || s.model == nil || s.model.workbench == nil || tabID == "" || paneID == "" {
		return false
	}
	resized := s.model.workbench.ResizeFloatingPane(tabID, paneID, width, height)
	clamped := s.model.workbench.ClampFloatingPanesToBounds(s.model.bodyRect())
	return resized || clamped
}

func (s *layoutResizeService) resizeSplit(tabID string, split *workbench.LayoutNode, bounds workbench.Rect, x, y, offsetX, offsetY int) bool {
	if s == nil || s.model == nil || s.model.workbench == nil || tabID == "" || split == nil {
		return false
	}
	return s.model.workbench.ResizeSplit(tabID, split, bounds, x, y, offsetX, offsetY)
}

func (s *layoutResizeService) syncActionNow(action input.SemanticAction) error {
	if s == nil || s.model == nil {
		return nil
	}
	switch action.Kind {
	case input.ActionResizePaneLeft,
		input.ActionResizePaneRight,
		input.ActionResizePaneUp,
		input.ActionResizePaneDown,
		input.ActionResizePaneLargeLeft,
		input.ActionResizePaneLargeRight,
		input.ActionResizePaneLargeUp,
		input.ActionResizePaneLargeDown,
		input.ActionBalancePanes,
		input.ActionCycleLayout:
		if err := s.resizeVisible(context.Background()); err != nil {
			return err
		}
	case input.ActionResizeFloatingLeft,
		input.ActionResizeFloatingRight,
		input.ActionResizeFloatingUp,
		input.ActionResizeFloatingDown:
		pane, rect, ok := s.model.visiblePaneForInput(action.PaneID)
		if !ok || pane == nil || pane.TerminalID == "" {
			return nil
		}
		if err := s.ensurePaneTerminalSize(context.Background(), pane.ID, pane.TerminalID, rect); err != nil {
			return err
		}
	default:
		return nil
	}
	if s.model.render != nil {
		s.model.render.Invalidate()
	}
	return nil
}

func (s *layoutResizeService) resizeCmdForAction(action input.SemanticAction) tea.Cmd {
	if s == nil || s.model == nil {
		return nil
	}
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
		return s.resizeVisibleCmd()
	case input.ActionResizeFloatingLeft,
		input.ActionResizeFloatingRight,
		input.ActionResizeFloatingUp,
		input.ActionResizeFloatingDown:
		return s.resizePaneIfNeededCmd(action.PaneID)
	default:
		return nil
	}
}

func (s *layoutResizeService) bindingRole(paneID string) string {
	if s == nil || s.model == nil || s.model.runtime == nil || paneID == "" {
		return ""
	}
	if binding := s.model.runtime.Binding(paneID); binding != nil {
		return string(binding.Role)
	}
	return ""
}

func (s *layoutResizeService) terminalControlStatus(terminalID string) runtime.TerminalControlStatus {
	if s == nil || s.model == nil || s.model.runtime == nil || terminalID == "" {
		return runtime.TerminalControlStatus{}
	}
	return s.model.runtime.TerminalControlStatus(terminalID)
}

func (s *layoutResizeService) handleLocalAction(action input.SemanticAction) (bool, tea.Cmd) {
	if s == nil || s.model == nil || s.model.workbench == nil {
		return false, nil
	}
	switch action.Kind {
	case input.ActionResizePaneLeft, input.ActionResizePaneRight, input.ActionResizePaneUp, input.ActionResizePaneDown:
		return s.adjustPaneRatioAction(action, 0.05)
	case input.ActionResizePaneLargeLeft, input.ActionResizePaneLargeRight, input.ActionResizePaneLargeUp, input.ActionResizePaneLargeDown:
		return s.adjustPaneRatioAction(action, 0.15)
	case input.ActionBalancePanes:
		return s.balancePanesAction()
	case input.ActionCycleLayout:
		return s.cycleLayoutAction()
	case input.ActionMoveFloatingLeft, input.ActionMoveFloatingRight, input.ActionMoveFloatingUp, input.ActionMoveFloatingDown:
		return s.moveFloatingAction(action)
	case input.ActionResizeFloatingLeft, input.ActionResizeFloatingRight, input.ActionResizeFloatingUp, input.ActionResizeFloatingDown:
		return s.resizeFloatingAction(action)
	case input.ActionCenterFloatingPane:
		return s.centerFloatingAction(action)
	default:
		return false, nil
	}
}

func (s *layoutResizeService) adjustPaneRatioAction(action input.SemanticAction, delta float64) (bool, tea.Cmd) {
	tab, paneID, ok := s.currentPaneActionTarget(action.PaneID)
	if !ok {
		return true, nil
	}
	var dir workbench.Direction
	switch action.Kind {
	case input.ActionResizePaneLeft, input.ActionResizePaneLargeLeft:
		dir = workbench.DirectionLeft
	case input.ActionResizePaneRight, input.ActionResizePaneLargeRight:
		dir = workbench.DirectionRight
	case input.ActionResizePaneUp, input.ActionResizePaneLargeUp:
		dir = workbench.DirectionUp
	default:
		dir = workbench.DirectionDown
	}
	if err := s.model.workbench.AdjustPaneRatio(tab.ID, paneID, dir, delta); err != nil {
		return true, s.model.showError(err)
	}
	s.model.render.Invalidate()
	if err := s.resizeVisible(context.Background()); err != nil {
		return true, s.model.showError(err)
	}
	return true, s.model.saveStateCmd()
}

func (s *layoutResizeService) balancePanesAction() (bool, tea.Cmd) {
	tab := s.model.workbench.CurrentTab()
	if tab == nil {
		return true, nil
	}
	s.model.workbench.BalancePanes(tab.ID)
	s.model.render.Invalidate()
	if err := s.resizeVisible(context.Background()); err != nil {
		return true, s.model.showError(err)
	}
	return true, s.model.saveStateCmd()
}

func (s *layoutResizeService) cycleLayoutAction() (bool, tea.Cmd) {
	tab := s.model.workbench.CurrentTab()
	if tab == nil {
		return true, nil
	}
	s.model.workbench.CycleLayout(tab.ID)
	s.model.render.Invalidate()
	if err := s.resizeVisible(context.Background()); err != nil {
		return true, s.model.showError(err)
	}
	return true, s.model.saveStateCmd()
}

func (s *layoutResizeService) moveFloatingAction(action input.SemanticAction) (bool, tea.Cmd) {
	tab, paneID, ok := s.currentFloatingActionTarget(action.PaneID)
	if !ok {
		return true, nil
	}
	dx, dy := 0, 0
	switch action.Kind {
	case input.ActionMoveFloatingLeft:
		dx = -layoutResizeFloatingMoveStep
	case input.ActionMoveFloatingRight:
		dx = layoutResizeFloatingMoveStep
	case input.ActionMoveFloatingUp:
		dy = -layoutResizeFloatingMoveStep
	default:
		dy = layoutResizeFloatingMoveStep
	}
	if !s.model.workbench.MoveFloatingPaneBy(tab.ID, paneID, dx, dy) {
		return true, nil
	}
	s.model.workbench.ReorderFloatingPane(tab.ID, paneID, true)
	s.model.workbench.ClampFloatingPanesToBounds(s.model.bodyRect())
	s.model.render.Invalidate()
	return true, nil
}

func (s *layoutResizeService) resizeFloatingAction(action input.SemanticAction) (bool, tea.Cmd) {
	tab, paneID, ok := s.currentFloatingActionTarget(action.PaneID)
	if !ok {
		return true, nil
	}
	dw, dh := 0, 0
	switch action.Kind {
	case input.ActionResizeFloatingLeft:
		dw = -layoutResizeFloatingResizeStep
	case input.ActionResizeFloatingRight:
		dw = layoutResizeFloatingResizeStep
	case input.ActionResizeFloatingUp:
		dh = -layoutResizeFloatingResizeStep
	default:
		dh = layoutResizeFloatingResizeStep
	}
	if !s.model.workbench.ResizeFloatingPaneBy(tab.ID, paneID, dw, dh) {
		return true, nil
	}
	s.model.workbench.ReorderFloatingPane(tab.ID, paneID, true)
	s.model.workbench.ClampFloatingPanesToBounds(s.model.bodyRect())
	s.model.render.Invalidate()
	pane, rect, ok := s.model.visiblePaneForInput(paneID)
	if !ok || pane == nil || pane.TerminalID == "" {
		return true, nil
	}
	if err := s.ensurePaneTerminalSize(context.Background(), pane.ID, pane.TerminalID, rect); err != nil {
		return true, s.model.showError(err)
	}
	return true, nil
}

func (s *layoutResizeService) centerFloatingAction(action input.SemanticAction) (bool, tea.Cmd) {
	tab, paneID, ok := s.currentFloatingActionTarget(action.PaneID)
	if !ok {
		return true, nil
	}
	if !s.model.workbench.CenterFloatingPane(tab.ID, paneID, workbench.Rect{W: layoutResizeFloatingBoundsW, H: layoutResizeFloatingBoundsH}) {
		return true, nil
	}
	s.model.workbench.ReorderFloatingPane(tab.ID, paneID, true)
	s.model.render.Invalidate()
	return true, nil
}

func (s *layoutResizeService) currentPaneActionTarget(paneID string) (*workbench.TabState, string, bool) {
	if s == nil || s.model == nil || s.model.workbench == nil {
		return nil, "", false
	}
	tab := s.model.workbench.CurrentTab()
	if tab == nil {
		return nil, "", false
	}
	targetPaneID := s.model.currentOrActionPaneID(paneID)
	if targetPaneID == "" || tab.Panes[targetPaneID] == nil {
		return nil, "", false
	}
	return tab, targetPaneID, true
}

func (s *layoutResizeService) currentFloatingActionTarget(paneID string) (*workbench.TabState, string, bool) {
	if s == nil || s.model == nil || s.model.workbench == nil {
		return nil, "", false
	}
	tab := s.model.workbench.CurrentTab()
	if tab == nil {
		return nil, "", false
	}
	targetPaneID := paneID
	if targetPaneID == "" {
		targetPaneID = activeFloatingPaneID(tab)
	}
	if targetPaneID == "" {
		targetPaneID = tab.ActivePaneID
	}
	if targetPaneID == "" {
		return nil, "", false
	}
	found := false
	for _, floating := range tab.Floating {
		if floating != nil && floating.PaneID == targetPaneID {
			found = true
			break
		}
	}
	if !found {
		return nil, "", false
	}
	if targetPaneID == "" {
		return nil, "", false
	}
	return tab, targetPaneID, true
}
