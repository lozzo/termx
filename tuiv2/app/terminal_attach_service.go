package app

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/bootstrap"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/orchestrator"
	"github.com/lozzow/termx/tuiv2/runtime"
)

type terminalAttachService struct {
	model *Model
}

type terminalAttachRequest struct {
	tabID                 string
	paneID                string
	terminalID            string
	mode                  string
	offset                int
	limit                 int
	cleanupPreparedTarget bool
	previousWorkspaceName string
	previousActiveTabID   string
	previousFocusedPaneID string
}

type terminalAttachRollback struct {
	previousPaneTerminalID string
	previousPaneTitle      string
	previousWorkspaceName  string
	previousActiveTabID    string
	previousFocusedPaneID  string
	targetTabActivePaneID  string
	cleanupPreparedTarget  bool
	targetPaneTitles       map[workbenchPaneRef]string
	plan                   orchestrator.TerminalAttachPlan
	previousBinding        *runtime.PaneBinding
	previousControl        runtime.TerminalControlStatus
	previousLiveState      runtime.TerminalLiveStateSnapshot
	targetControl          runtime.TerminalControlStatus
	targetAttachment       runtime.TerminalAttachmentSnapshot
	targetLiveState        runtime.TerminalLiveStateSnapshot
}

type workbenchPaneRef struct {
	tabID  string
	paneID string
}

func (m *Model) terminalAttachService() *terminalAttachService {
	if m == nil {
		return nil
	}
	return &terminalAttachService{model: m}
}

func (s *terminalAttachService) splitAndAttachCmd(paneID, terminalID string) tea.Cmd {
	if s == nil || s.model == nil || s.model.orchestrator == nil || paneID == "" || terminalID == "" {
		return nil
	}
	return s.prepareAndAttachCmd(terminalID, func() (string, string, error) {
		return s.model.orchestrator.PrepareSplitAttachTarget(paneID)
	})
}

func (s *terminalAttachService) createTabAndAttachCmd(terminalID string) tea.Cmd {
	if s == nil || s.model == nil || s.model.orchestrator == nil || terminalID == "" {
		return nil
	}
	return s.prepareAndAttachCmd(terminalID, func() (string, string, error) {
		return s.model.orchestrator.PrepareTabAttachTarget()
	})
}

func (s *terminalAttachService) createFloatingAndAttachCmd(terminalID string) tea.Cmd {
	if s == nil || s.model == nil || s.model.orchestrator == nil || terminalID == "" {
		return nil
	}
	return s.prepareAndAttachCmd(terminalID, func() (string, string, error) {
		return s.model.orchestrator.PrepareFloatingAttachTarget()
	})
}

func (s *terminalAttachService) attachCmd(tabID, paneID, terminalID string) tea.Cmd {
	return s.attachWithModeCmd(tabID, paneID, terminalID, "collaborator")
}

func (s *terminalAttachService) attachWithModeCmd(tabID, paneID, terminalID, mode string) tea.Cmd {
	if s == nil || s.model == nil || s.model.orchestrator == nil || paneID == "" || terminalID == "" {
		return nil
	}
	s.model.markPendingPaneAttach(paneID, terminalID)
	previousWorkspaceName, previousActiveTabID, previousFocusedPaneID := s.currentWorkbenchSelection()
	req := terminalAttachRequest{
		tabID:                 tabID,
		paneID:                paneID,
		terminalID:            terminalID,
		mode:                  mode,
		offset:                0,
		limit:                 defaultTerminalSnapshotScrollbackLimit,
		previousWorkspaceName: previousWorkspaceName,
		previousActiveTabID:   previousActiveTabID,
		previousFocusedPaneID: previousFocusedPaneID,
	}
	return func() tea.Msg {
		return s.attachMsg(req)
	}
}

func (s *terminalAttachService) prepareAndAttachCmd(terminalID string, prepare func() (string, string, error)) tea.Cmd {
	if s == nil || s.model == nil || prepare == nil || terminalID == "" {
		return nil
	}
	prevWorkspaceName, prevActiveTabID, prevFocusedPaneID := s.currentWorkbenchSelection()
	tabID, paneID, err := prepare()
	if err != nil {
		return func() tea.Msg { return err }
	}
	s.model.render.Invalidate()
	s.model.markPendingPaneAttach(paneID, terminalID)
	req := terminalAttachRequest{
		tabID:                 tabID,
		paneID:                paneID,
		terminalID:            terminalID,
		mode:                  "collaborator",
		offset:                0,
		limit:                 defaultTerminalSnapshotScrollbackLimit,
		cleanupPreparedTarget: true,
		previousWorkspaceName: prevWorkspaceName,
		previousActiveTabID:   prevActiveTabID,
		previousFocusedPaneID: prevFocusedPaneID,
	}
	return func() tea.Msg { return s.attachMsg(req) }
}

func (s *terminalAttachService) restartAndAttachCmd(paneID, terminalID string) tea.Cmd {
	if s == nil || s.model == nil || s.model.runtime == nil || s.model.orchestrator == nil || paneID == "" || terminalID == "" {
		return nil
	}
	s.model.markPendingPaneAttach(paneID, terminalID)
	return func() tea.Msg {
		client := s.model.runtime.Client()
		if client == nil {
			return paneAttachFailure(paneID, terminalID, teaErr("attach terminal: runtime client is nil"))
		}
		if err := client.Restart(context.Background(), terminalID); err != nil {
			return paneAttachFailure(paneID, terminalID, err)
		}
		return s.attachMsg(terminalAttachRequest{
			paneID:     paneID,
			terminalID: terminalID,
			mode:       "collaborator",
			offset:     0,
			limit:      defaultTerminalSnapshotScrollbackLimit,
		})
	}
}

func (s *terminalAttachService) createAndAttachCmd(paneID string, target modal.CreateTargetKind, params protocol.CreateParams) tea.Cmd {
	if s == nil || s.model == nil || s.model.runtime == nil {
		return nil
	}
	if paneID != "" {
		s.model.markPendingPaneAttach(paneID, "")
	}
	return func() tea.Msg {
		client := s.model.runtime.Client()
		if client == nil {
			return paneAttachFailure(paneID, "", context.Canceled)
		}
		created, err := client.Create(context.Background(), params)
		if err != nil {
			return paneAttachFailure(paneID, "", err)
		}
		s.primeCreatedTerminal(created, params)
		if paneID != "" {
			s.model.markPendingPaneAttach(paneID, created.TerminalID)
		}
		switch target {
		case modal.CreateTargetSplit:
			if paneID != "" {
				s.model.clearPendingPaneAttach(paneID, created.TerminalID)
			}
			if cmd := s.model.splitPaneAndAttachTerminalCmd(paneID, created.TerminalID); cmd != nil {
				return cmd()
			}
			return nil
		case modal.CreateTargetNewTab:
			if paneID != "" {
				s.model.clearPendingPaneAttach(paneID, created.TerminalID)
			}
			if cmd := s.model.createTabAndAttachTerminalCmd(created.TerminalID); cmd != nil {
				return cmd()
			}
			return nil
		case modal.CreateTargetFloating:
			if paneID != "" {
				s.model.clearPendingPaneAttach(paneID, created.TerminalID)
			}
			if cmd := s.model.createFloatingPaneAndAttachTerminalCmd(created.TerminalID); cmd != nil {
				return cmd()
			}
			return nil
		default:
			if cmd := s.attachCmd("", paneID, created.TerminalID); cmd != nil {
				return cmd()
			}
			return nil
		}
	}
}

func (s *terminalAttachService) reattachRestoredCmd(hint bootstrap.PaneReattachHint) tea.Cmd {
	if s == nil || s.model == nil || hint.PaneID == "" || hint.TerminalID == "" {
		return nil
	}
	return func() tea.Msg {
		msg := s.attachMsg(terminalAttachRequest{
			tabID:      hint.TabID,
			paneID:     hint.PaneID,
			terminalID: hint.TerminalID,
			mode:       "collaborator",
			offset:     0,
			limit:      defaultTerminalSnapshotScrollbackLimit,
		})
		switch msg.(type) {
		case nil:
			s.rollbackRestoredBinding(hint.TabID, hint.PaneID, hint.TerminalID)
			return reattachFailedMsg{tabID: hint.TabID, paneID: hint.PaneID}
		case error, paneAttachFailedMsg:
			s.rollbackRestoredBinding(hint.TabID, hint.PaneID, hint.TerminalID)
			return reattachFailedMsg{tabID: hint.TabID, paneID: hint.PaneID}
		default:
			return msg
		}
	}
}

func (s *terminalAttachService) handleAttachedMsg(attached orchestrator.TerminalAttachedMsg) tea.Cmd {
	if s == nil || s.model == nil {
		return nil
	}
	s.model.clearPendingPaneAttach(attached.PaneID, attached.TerminalID)
	s.model.resetPaneViewport(attached.PaneID)
	if s.model.modalHost != nil && s.model.modalHost.Session != nil && s.model.modalHost.Session.Kind == input.ModePicker {
		s.model.closeModal(input.ModePicker, s.model.modalHost.Session.RequestID, input.ModeState{Kind: input.ModeNormal})
	}
	s.model.render.Invalidate()
	return batchCmds(s.model.saveStateCmd(), s.finalizeAttachCmd(attached.TabID, attached.PaneID, attached.TerminalID))
}

func (s *terminalAttachService) attachMsg(req terminalAttachRequest) tea.Msg {
	if s == nil || s.model == nil || s.model.orchestrator == nil || req.paneID == "" || req.terminalID == "" {
		return nil
	}
	attached, err := s.executeAttach(context.Background(), req)
	if err != nil {
		return paneAttachFailure(req.paneID, req.terminalID, err)
	}
	return attached
}

func (s *terminalAttachService) executeAttach(ctx context.Context, req terminalAttachRequest) (orchestrator.TerminalAttachedMsg, error) {
	if s == nil || s.model == nil || s.model.runtime == nil || s.model.workbench == nil || s.model.orchestrator == nil {
		return orchestrator.TerminalAttachedMsg{}, teaErr("attach terminal: service unavailable")
	}
	plan, err := s.model.orchestrator.PlanAttachTerminal(req.tabID, req.paneID, req.terminalID, defaultAttachMode(req.mode))
	if err != nil {
		return orchestrator.TerminalAttachedMsg{}, err
	}
	rollback := s.captureAttachRollback(req, plan)
	terminal, err := s.model.runtime.AttachTerminal(ctx, plan.PaneID, plan.TerminalID, plan.Mode)
	if err != nil {
		return orchestrator.TerminalAttachedMsg{}, err
	}
	if _, err := s.model.runtime.LoadSnapshot(ctx, plan.TerminalID, req.offset, req.limit); err != nil {
		s.rollbackAttachExecution(rollback)
		return orchestrator.TerminalAttachedMsg{}, err
	}
	if err := s.bindWorkbenchPaneTerminal(plan.TabID, plan.PaneID, plan.TerminalID); err != nil {
		s.rollbackAttachExecution(rollback)
		return orchestrator.TerminalAttachedMsg{}, err
	}
	s.syncWorkbenchPaneTitle(plan.TerminalID, terminal)
	if err := s.model.runtime.StartStream(ctx, plan.TerminalID); err != nil {
		s.rollbackAttachExecution(rollback)
		return orchestrator.TerminalAttachedMsg{}, err
	}
	return orchestrator.TerminalAttachedMsg{
		TabID:      plan.TabID,
		PaneID:     plan.PaneID,
		TerminalID: plan.TerminalID,
		Channel:    terminal.Channel,
	}, nil
}

func defaultAttachMode(mode string) string {
	if trimmed := strings.TrimSpace(mode); trimmed != "" {
		return trimmed
	}
	return "collaborator"
}

func (s *terminalAttachService) captureAttachRollback(req terminalAttachRequest, plan orchestrator.TerminalAttachPlan) terminalAttachRollback {
	rollback := terminalAttachRollback{
		plan:                  plan,
		cleanupPreparedTarget: req.cleanupPreparedTarget,
		previousWorkspaceName: req.previousWorkspaceName,
		previousActiveTabID:   req.previousActiveTabID,
		previousFocusedPaneID: req.previousFocusedPaneID,
	}
	if s == nil || s.model == nil || s.model.runtime == nil {
		return rollback
	}
	rollback.previousBinding = runtime.ClonePaneBinding(s.model.runtime.Binding(plan.PaneID))
	rollback.previousPaneTerminalID = s.paneTerminalID(plan.TabID, plan.PaneID)
	rollback.previousPaneTitle = s.paneTitle(plan.TabID, plan.PaneID)
	rollback.targetTabActivePaneID = s.activePaneID(plan.TabID)
	rollback.targetPaneTitles = s.paneTitlesByTerminal(plan.TerminalID)
	if rollback.previousPaneTerminalID != "" {
		rollback.previousControl = s.model.runtime.TerminalControlStatus(rollback.previousPaneTerminalID)
		rollback.previousLiveState = s.model.runtime.TerminalLiveStateSnapshot(rollback.previousPaneTerminalID)
	}
	rollback.targetControl = s.model.runtime.TerminalControlStatus(plan.TerminalID)
	rollback.targetAttachment = s.model.runtime.TerminalAttachmentSnapshot(plan.TerminalID)
	rollback.targetLiveState = s.model.runtime.TerminalLiveStateSnapshot(plan.TerminalID)
	return rollback
}

func (s *terminalAttachService) rollbackAttachExecution(rollback terminalAttachRollback) {
	if s == nil || s.model == nil || s.model.runtime == nil {
		return
	}
	s.model.runtime.UnbindPane(rollback.plan.PaneID, rollback.plan.TerminalID)
	if rollback.previousLiveState.TerminalID != "" {
		s.model.runtime.RestoreTerminalLiveState(rollback.previousLiveState.TerminalID, rollback.previousLiveState)
	}
	if rollback.targetLiveState.TerminalID != "" && rollback.targetLiveState.TerminalID != rollback.previousLiveState.TerminalID {
		s.model.runtime.RestoreTerminalLiveState(rollback.targetLiveState.TerminalID, rollback.targetLiveState)
	}
	s.model.runtime.RestorePaneBinding(rollback.plan.PaneID, rollback.previousBinding)
	if rollback.previousControl.TerminalID != "" {
		s.model.runtime.RestoreTerminalControlStatus(rollback.previousControl)
	}
	if rollback.targetControl.TerminalID != "" && rollback.targetControl.TerminalID != rollback.previousControl.TerminalID {
		s.model.runtime.RestoreTerminalControlStatus(rollback.targetControl)
	}
	s.model.runtime.RestoreTerminalAttachmentSnapshot(rollback.plan.TerminalID, rollback.targetAttachment)
	if s.model.workbench != nil {
		if rollback.cleanupPreparedTarget && rollback.plan.TabID != "" && rollback.plan.PaneID != "" {
			_, _ = s.model.workbench.ClosePane(rollback.plan.TabID, rollback.plan.PaneID)
		}
		if !rollback.cleanupPreparedTarget {
			_ = s.model.workbench.BindPaneTerminal(rollback.plan.TabID, rollback.plan.PaneID, rollback.previousPaneTerminalID)
			_ = s.model.workbench.SetPaneTitle(rollback.plan.TabID, rollback.plan.PaneID, rollback.previousPaneTitle)
			if rollback.targetTabActivePaneID != "" {
				_ = s.model.workbench.FocusPane(rollback.plan.TabID, rollback.targetTabActivePaneID)
			}
		}
		for ref, title := range rollback.targetPaneTitles {
			_ = s.model.workbench.SetPaneTitle(ref.tabID, ref.paneID, title)
		}
		s.restoreWorkbenchSelection(rollback.previousWorkspaceName, rollback.previousActiveTabID, rollback.previousFocusedPaneID)
	}
}

func (s *terminalAttachService) activePaneID(tabID string) string {
	if s == nil || s.model == nil || s.model.workbench == nil || tabID == "" {
		return ""
	}
	for _, wsName := range s.model.workbench.ListWorkspaces() {
		ws := s.model.workbench.WorkspaceByName(wsName)
		if ws == nil {
			continue
		}
		for _, tab := range ws.Tabs {
			if tab != nil && tab.ID == tabID {
				return tab.ActivePaneID
			}
		}
	}
	return ""
}

func (s *terminalAttachService) paneTerminalID(tabID, paneID string) string {
	if s == nil || s.model == nil || s.model.workbench == nil || tabID == "" || paneID == "" {
		return ""
	}
	for _, wsName := range s.model.workbench.ListWorkspaces() {
		ws := s.model.workbench.WorkspaceByName(wsName)
		if ws == nil {
			continue
		}
		for _, tab := range ws.Tabs {
			if tab == nil || tab.ID != tabID {
				continue
			}
			if pane := tab.Panes[paneID]; pane != nil {
				return pane.TerminalID
			}
			return ""
		}
	}
	return ""
}

func (s *terminalAttachService) paneTitle(tabID, paneID string) string {
	if s == nil || s.model == nil || s.model.workbench == nil || tabID == "" || paneID == "" {
		return ""
	}
	for _, wsName := range s.model.workbench.ListWorkspaces() {
		ws := s.model.workbench.WorkspaceByName(wsName)
		if ws == nil {
			continue
		}
		for _, tab := range ws.Tabs {
			if tab == nil || tab.ID != tabID {
				continue
			}
			if pane := tab.Panes[paneID]; pane != nil {
				return pane.Title
			}
			return ""
		}
	}
	return ""
}

func (s *terminalAttachService) paneTitlesByTerminal(terminalID string) map[workbenchPaneRef]string {
	if s == nil || s.model == nil || s.model.workbench == nil || terminalID == "" {
		return nil
	}
	titles := make(map[workbenchPaneRef]string)
	for _, wsName := range s.model.workbench.ListWorkspaces() {
		ws := s.model.workbench.WorkspaceByName(wsName)
		if ws == nil {
			continue
		}
		for _, tab := range ws.Tabs {
			if tab == nil {
				continue
			}
			for paneID, pane := range tab.Panes {
				if pane == nil || pane.TerminalID != terminalID {
					continue
				}
				titles[workbenchPaneRef{tabID: tab.ID, paneID: paneID}] = pane.Title
			}
		}
	}
	if len(titles) == 0 {
		return nil
	}
	return titles
}

func (s *terminalAttachService) currentWorkbenchSelection() (string, string, string) {
	if s == nil || s.model == nil || s.model.workbench == nil {
		return "", "", ""
	}
	workspaceName := ""
	activeTabID := ""
	focusedPaneID := ""
	if ws := s.model.workbench.CurrentWorkspace(); ws != nil {
		workspaceName = ws.Name
	}
	if tab := s.model.workbench.CurrentTab(); tab != nil {
		activeTabID = tab.ID
		focusedPaneID = tab.ActivePaneID
	}
	return workspaceName, activeTabID, focusedPaneID
}

func (s *terminalAttachService) restoreWorkbenchSelection(workspaceName, activeTabID, focusedPaneID string) {
	if s == nil || s.model == nil || s.model.workbench == nil {
		return
	}
	if workspaceName != "" {
		_ = s.model.workbench.SwitchWorkspace(workspaceName)
	}
	if activeTabID != "" {
		for _, wsName := range s.model.workbench.ListWorkspaces() {
			ws := s.model.workbench.WorkspaceByName(wsName)
			if ws == nil {
				continue
			}
			for index, tab := range ws.Tabs {
				if tab == nil || tab.ID != activeTabID {
					continue
				}
				_ = s.model.workbench.SwitchWorkspace(wsName)
				_ = s.model.workbench.SwitchTab(wsName, index)
				break
			}
		}
	}
	if activeTabID != "" && focusedPaneID != "" {
		_ = s.model.workbench.FocusPane(activeTabID, focusedPaneID)
	}
}

func (s *terminalAttachService) bindWorkbenchPaneTerminal(tabID, paneID, terminalID string) error {
	if s == nil || s.model == nil || s.model.workbench == nil {
		return teaErr("attach terminal: workbench unavailable")
	}
	if err := s.model.workbench.BindPaneTerminal(tabID, paneID, terminalID); err != nil {
		return err
	}
	if err := s.model.workbench.FocusPane(tabID, paneID); err != nil {
		return err
	}
	return nil
}

func (s *terminalAttachService) syncWorkbenchPaneTitle(terminalID string, terminal *runtime.TerminalRuntime) {
	if s == nil || s.model == nil || s.model.workbench == nil || terminalID == "" || terminal == nil || terminal.Name == "" {
		return
	}
	s.model.workbench.SetPaneTitleByTerminalID(terminalID, terminal.Name)
}

func (s *terminalAttachService) primeCreatedTerminal(created *protocol.CreateResult, params protocol.CreateParams) {
	if s == nil || s.model == nil || s.model.runtime == nil || s.model.runtime.Registry() == nil || created == nil {
		return
	}
	terminal := s.model.runtime.Registry().GetOrCreate(created.TerminalID)
	if terminal == nil {
		return
	}
	terminal.Name = params.Name
	terminal.Tags = cloneStringMap(params.Tags)
	terminal.Command = append([]string(nil), params.Command...)
	terminal.State = created.State
}

func (s *terminalAttachService) rollbackRestoredBinding(tabID, paneID, terminalID string) {
	if s == nil || s.model == nil {
		return
	}
	s.model.clearPendingPaneAttach(paneID, terminalID)
	if s.model.workbench != nil && tabID != "" {
		_ = s.model.workbench.BindPaneTerminal(tabID, paneID, "")
	}
}

func (s *terminalAttachService) finalizeAttachCmd(tabID, paneID, terminalID string) tea.Cmd {
	if s == nil || s.model == nil || paneID == "" || terminalID == "" {
		return nil
	}
	return func() tea.Msg {
		if s.model.sessionID == "" {
			if pane, rect, ok := s.model.paneResizeTarget(tabID, paneID); ok && pane != nil && pane.TerminalID == terminalID {
				if err := s.model.ensurePaneTerminalSize(context.Background(), paneID, terminalID, rect); err != nil {
					return err
				}
				s.model.clearPendingPaneResize(paneID, terminalID)
			} else {
				s.model.markPendingPaneResize(tabID, paneID, terminalID)
			}
		}
		return terminalAttachReadyMsg{paneID: paneID, terminalID: terminalID}
	}
}
