package terminalattach

import (
	"context"
	"fmt"
	"strings"

	"github.com/lozzow/termx/tuiv2/orchestrator"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/lozzow/termx/tuiv2/workbench"
)

type Selection struct {
	WorkspaceName string
	ActiveTabID   string
	FocusedPaneID string
}

type Request struct {
	TabID                 string
	PaneID                string
	TerminalID            string
	Mode                  string
	Offset                int
	Limit                 int
	CleanupPreparedTarget bool
	PreviousSelection     Selection
}

type Manager struct {
	workbench    *workbench.Workbench
	runtime      *runtime.Runtime
	orchestrator *orchestrator.Orchestrator
}

type rollback struct {
	previousPaneTerminalID string
	previousPaneTitle      string
	previousSelection      Selection
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

func NewManager(wb *workbench.Workbench, rt *runtime.Runtime, orch *orchestrator.Orchestrator) *Manager {
	return &Manager{workbench: wb, runtime: rt, orchestrator: orch}
}

func (m *Manager) Execute(ctx context.Context, req Request) (orchestrator.TerminalAttachedMsg, error) {
	if m == nil || m.runtime == nil || m.workbench == nil || m.orchestrator == nil {
		return orchestrator.TerminalAttachedMsg{}, shared.UserVisibleError{Op: "attach terminal", Err: fmt.Errorf("service unavailable")}
	}
	plan, err := m.orchestrator.PlanAttachTerminal(req.TabID, req.PaneID, req.TerminalID, defaultAttachMode(req.Mode))
	if err != nil {
		return orchestrator.TerminalAttachedMsg{}, err
	}
	rollback := m.captureRollback(req, plan)
	terminal, err := m.runtime.AttachTerminal(ctx, plan.PaneID, plan.TerminalID, plan.Mode)
	if err != nil {
		return orchestrator.TerminalAttachedMsg{}, err
	}
	if _, err := m.runtime.LoadSnapshot(ctx, plan.TerminalID, req.Offset, req.Limit); err != nil {
		m.rollbackExecution(rollback)
		return orchestrator.TerminalAttachedMsg{}, err
	}
	if err := m.bindWorkbenchPaneTerminal(plan.TabID, plan.PaneID, plan.TerminalID); err != nil {
		m.rollbackExecution(rollback)
		return orchestrator.TerminalAttachedMsg{}, err
	}
	m.syncWorkbenchPaneTitle(plan.TerminalID, terminal)
	if err := m.runtime.StartStream(ctx, plan.TerminalID); err != nil {
		m.rollbackExecution(rollback)
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

func (m *Manager) captureRollback(req Request, plan orchestrator.TerminalAttachPlan) rollback {
	state := rollback{
		plan:                  plan,
		cleanupPreparedTarget: req.CleanupPreparedTarget,
		previousSelection:     req.PreviousSelection,
	}
	if m == nil || m.runtime == nil {
		return state
	}
	state.previousBinding = runtime.ClonePaneBinding(m.runtime.Binding(plan.PaneID))
	state.previousPaneTerminalID = m.paneTerminalID(plan.TabID, plan.PaneID)
	state.previousPaneTitle = m.paneTitle(plan.TabID, plan.PaneID)
	state.targetTabActivePaneID = m.activePaneID(plan.TabID)
	state.targetPaneTitles = m.paneTitlesByTerminal(plan.TerminalID)
	if state.previousPaneTerminalID != "" {
		state.previousControl = m.runtime.TerminalControlStatus(state.previousPaneTerminalID)
		state.previousLiveState = m.runtime.TerminalLiveStateSnapshot(state.previousPaneTerminalID)
	}
	state.targetControl = m.runtime.TerminalControlStatus(plan.TerminalID)
	state.targetAttachment = m.runtime.TerminalAttachmentSnapshot(plan.TerminalID)
	state.targetLiveState = m.runtime.TerminalLiveStateSnapshot(plan.TerminalID)
	return state
}

func (m *Manager) rollbackExecution(state rollback) {
	if m == nil || m.runtime == nil {
		return
	}
	m.runtime.UnbindPane(state.plan.PaneID, state.plan.TerminalID)
	if state.previousLiveState.TerminalID != "" {
		m.runtime.RestoreTerminalLiveState(state.previousLiveState.TerminalID, state.previousLiveState)
	}
	if state.targetLiveState.TerminalID != "" && state.targetLiveState.TerminalID != state.previousLiveState.TerminalID {
		m.runtime.RestoreTerminalLiveState(state.targetLiveState.TerminalID, state.targetLiveState)
	}
	m.runtime.RestorePaneBinding(state.plan.PaneID, state.previousBinding)
	if state.previousControl.TerminalID != "" {
		m.runtime.RestoreTerminalControlStatus(state.previousControl)
	}
	if state.targetControl.TerminalID != "" && state.targetControl.TerminalID != state.previousControl.TerminalID {
		m.runtime.RestoreTerminalControlStatus(state.targetControl)
	}
	m.runtime.RestoreTerminalAttachmentSnapshot(state.plan.TerminalID, state.targetAttachment)
	if m.workbench != nil {
		if state.cleanupPreparedTarget && state.plan.TabID != "" && state.plan.PaneID != "" {
			_, _ = m.workbench.ClosePane(state.plan.TabID, state.plan.PaneID)
		}
		if !state.cleanupPreparedTarget {
			_ = m.workbench.BindPaneTerminal(state.plan.TabID, state.plan.PaneID, state.previousPaneTerminalID)
			_ = m.workbench.SetPaneTitle(state.plan.TabID, state.plan.PaneID, state.previousPaneTitle)
			if state.targetTabActivePaneID != "" {
				_ = m.workbench.FocusPane(state.plan.TabID, state.targetTabActivePaneID)
			}
		}
		for ref, title := range state.targetPaneTitles {
			_ = m.workbench.SetPaneTitle(ref.tabID, ref.paneID, title)
		}
		m.restoreWorkbenchSelection(state.previousSelection)
	}
}

func (m *Manager) activePaneID(tabID string) string {
	if m == nil || m.workbench == nil || tabID == "" {
		return ""
	}
	for _, wsName := range m.workbench.ListWorkspaces() {
		ws := m.workbench.WorkspaceByName(wsName)
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

func (m *Manager) paneTerminalID(tabID, paneID string) string {
	if m == nil || m.workbench == nil || tabID == "" || paneID == "" {
		return ""
	}
	for _, wsName := range m.workbench.ListWorkspaces() {
		ws := m.workbench.WorkspaceByName(wsName)
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

func (m *Manager) paneTitle(tabID, paneID string) string {
	if m == nil || m.workbench == nil || tabID == "" || paneID == "" {
		return ""
	}
	for _, wsName := range m.workbench.ListWorkspaces() {
		ws := m.workbench.WorkspaceByName(wsName)
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

func (m *Manager) paneTitlesByTerminal(terminalID string) map[workbenchPaneRef]string {
	if m == nil || m.workbench == nil || terminalID == "" {
		return nil
	}
	titles := make(map[workbenchPaneRef]string)
	for _, wsName := range m.workbench.ListWorkspaces() {
		ws := m.workbench.WorkspaceByName(wsName)
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

func (m *Manager) restoreWorkbenchSelection(selection Selection) {
	if m == nil || m.workbench == nil {
		return
	}
	if selection.WorkspaceName != "" {
		_ = m.workbench.SwitchWorkspace(selection.WorkspaceName)
	}
	if selection.ActiveTabID != "" {
		for _, wsName := range m.workbench.ListWorkspaces() {
			ws := m.workbench.WorkspaceByName(wsName)
			if ws == nil {
				continue
			}
			for index, tab := range ws.Tabs {
				if tab == nil || tab.ID != selection.ActiveTabID {
					continue
				}
				_ = m.workbench.SwitchWorkspace(wsName)
				_ = m.workbench.SwitchTab(wsName, index)
				break
			}
		}
	}
	if selection.ActiveTabID != "" && selection.FocusedPaneID != "" {
		_ = m.workbench.FocusPane(selection.ActiveTabID, selection.FocusedPaneID)
	}
}

func (m *Manager) bindWorkbenchPaneTerminal(tabID, paneID, terminalID string) error {
	if m == nil || m.workbench == nil {
		return shared.UserVisibleError{Op: "attach terminal", Err: fmt.Errorf("workbench unavailable")}
	}
	if err := m.workbench.BindPaneTerminal(tabID, paneID, terminalID); err != nil {
		return err
	}
	if err := m.workbench.FocusPane(tabID, paneID); err != nil {
		return err
	}
	return nil
}

func (m *Manager) syncWorkbenchPaneTitle(terminalID string, terminal *runtime.TerminalRuntime) {
	if m == nil || m.workbench == nil || terminalID == "" || terminal == nil || terminal.Name == "" {
		return
	}
	m.workbench.SetPaneTitleByTerminalID(terminalID, terminal.Name)
}
