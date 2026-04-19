package app

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/orchestrator"
	"github.com/lozzow/termx/tuiv2/sessionstate"
	"github.com/lozzow/termx/workbenchdoc"
)

func batchCmds(cmds ...tea.Cmd) tea.Cmd {
	filtered := make([]tea.Cmd, 0, len(cmds))
	for _, cmd := range cmds {
		if cmd != nil {
			filtered = append(filtered, cmd)
		}
	}
	switch len(filtered) {
	case 0:
		return nil
	case 1:
		return filtered[0]
	default:
		return tea.Batch(filtered...)
	}
}

func (m *Model) pullSessionCmd() tea.Cmd {
	if m == nil || m.sessionID == "" || m.runtime == nil || m.runtime.Client() == nil {
		return nil
	}
	sessionID := m.sessionID
	client := m.runtime.Client()
	return func() tea.Msg {
		snapshot, err := client.GetSession(context.Background(), sessionID)
		return sessionSnapshotMsg{Snapshot: snapshot, Err: err}
	}
}

func (m *Model) replaceSessionCmd() tea.Cmd {
	if m == nil || m.sessionID == "" || m.runtime == nil || m.runtime.Client() == nil || m.workbench == nil {
		return nil
	}
	client := m.runtime.Client()
	params := protocol.ReplaceSessionParams{
		SessionID:    m.sessionID,
		ViewID:       m.sessionViewID,
		BaseRevision: m.sessionRevision,
		Workbench:    m.exportSessionWorkbench(),
	}
	return func() tea.Msg {
		snapshot, err := client.ReplaceSession(context.Background(), params)
		if err == nil {
			return sessionSnapshotMsg{Snapshot: snapshot}
		}
		latest, latestErr := client.GetSession(context.Background(), params.SessionID)
		if latestErr == nil && latest != nil && isRevisionConflict(err) {
			return sessionSnapshotMsg{Snapshot: latest}
		}
		return sessionSnapshotMsg{Err: err}
	}
}

func (m *Model) updateSessionViewCmd() tea.Cmd {
	if m == nil || m.sessionID == "" || m.sessionViewID == "" || m.runtime == nil || m.runtime.Client() == nil {
		return nil
	}
	client := m.runtime.Client()
	params := m.currentSessionViewParams()
	return func() tea.Msg {
		view, err := client.UpdateSessionView(context.Background(), params)
		return sessionViewUpdatedMsg{View: view, Err: err}
	}
}

func (m *Model) reloadTerminalSnapshotCmd(terminalID string) tea.Cmd {
	if m == nil || m.runtime == nil || m.runtime.Client() == nil || terminalID == "" {
		return nil
	}
	return func() tea.Msg {
		snapshot, err := m.runtime.LoadSnapshot(context.Background(), terminalID, 0, defaultTerminalSnapshotScrollbackLimit)
		if err != nil {
			return err
		}
		return orchestrator.SnapshotLoadedMsg{TerminalID: terminalID, Snapshot: snapshot}
	}
}

func (m *Model) acquireSessionLeaseAndResizeCmd(paneID, terminalID string) tea.Cmd {
	return m.syncTerminalInteractionCmd(terminalInteractionRequest{
		PaneID:           paneID,
		TerminalID:       terminalID,
		ResizeIfNeeded:   true,
		ExplicitTakeover: true,
	})
}

func (m *Model) currentSessionLeases() []protocol.LeaseInfo {
	if m == nil || len(m.sessionLeases) == 0 {
		return nil
	}
	leases := make([]protocol.LeaseInfo, 0, len(m.sessionLeases))
	for _, lease := range m.sessionLeases {
		leases = append(leases, lease)
	}
	return leases
}

func (m *Model) currentSessionViewParams() protocol.UpdateSessionViewParams {
	params := protocol.UpdateSessionViewParams{
		SessionID: m.sessionID,
		ViewID:    m.sessionViewID,
	}
	if m.workbench != nil {
		if ws := m.workbench.CurrentWorkspace(); ws != nil {
			params.View.ActiveWorkspaceName = ws.Name
		}
		if tab := m.workbench.CurrentTab(); tab != nil {
			params.View.ActiveTabID = tab.ID
			params.View.FocusedPaneID = tab.ActivePaneID
		}
	}
	if m.width > 0 {
		params.View.WindowCols = uint16(m.width)
	}
	if m.height > 0 {
		params.View.WindowRows = uint16(m.height)
	}
	return params
}

func isRevisionConflict(err error) bool {
	return err != nil && strings.Contains(err.Error(), "revision conflict")
}

func isSessionLeaseUnsupported(err error) bool {
	return err != nil && strings.Contains(err.Error(), "unknown session method: session.acquire_lease")
}

func (m *Model) exportSessionWorkbench() *workbenchdoc.Doc {
	doc := sessionstate.ExportWorkbench(m.workbench)
	if m == nil || doc == nil || m.sessionSharedDoc == nil {
		return doc
	}
	doc.CurrentWorkspace = m.sessionSharedDoc.CurrentWorkspace
	for wsName, ws := range doc.Workspaces {
		if ws == nil {
			continue
		}
		baseWS := m.sessionSharedDoc.Workspaces[wsName]
		if baseWS == nil {
			continue
		}
		ws.ActiveTab = baseWS.ActiveTab
		baseTabs := make(map[string]*workbenchdoc.Tab, len(baseWS.Tabs))
		for _, tab := range baseWS.Tabs {
			if tab != nil {
				baseTabs[tab.ID] = tab
			}
		}
		for _, tab := range ws.Tabs {
			if tab == nil {
				continue
			}
			baseTab := baseTabs[tab.ID]
			if baseTab == nil {
				continue
			}
			tab.ActivePaneID = baseTab.ActivePaneID
			tab.ZoomedPaneID = baseTab.ZoomedPaneID
			tab.ScrollOffset = baseTab.ScrollOffset
		}
	}
	return doc
}

func (m *Model) reconcileSessionRuntime(ctx context.Context, oldBindings, nextBindings map[string]string) sessionRuntimeApplyResult {
	service := m.sessionRuntimeService()
	if service == nil {
		return sessionRuntimeApplyResult{}
	}
	return service.reconcileRuntime(ctx, oldBindings, nextBindings)
}
