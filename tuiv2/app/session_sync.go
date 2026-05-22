package app

import (
	"context"
	"errors"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/orchestrator"
	"github.com/lozzow/termx/tuiv2/sessiondoc"
	"github.com/lozzow/termx/tuiv2/sessionstore"
	"github.com/lozzow/termx/tuiv2/workbenchcodec"
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
	if m == nil || m.sessionID == "" || m.sessionStore == nil {
		return nil
	}
	sessionID := m.sessionID
	store := m.sessionStore
	return func() tea.Msg {
		snapshot, err := store.GetSession(context.Background(), sessionID)
		return sessionSnapshotMsg{Snapshot: snapshot, Err: err}
	}
}

func (m *Model) replaceSessionCmd() tea.Cmd {
	if m == nil || m.sessionID == "" || m.sessionStore == nil || m.workbench == nil {
		return nil
	}
	store := m.sessionStore
	params := sessionstore.ReplaceParams{
		SessionID:    m.sessionID,
		ViewID:       m.sessionViewID,
		BaseRevision: m.sessionRevision,
		Workbench:    m.exportSessionWorkbench(),
	}
	return func() tea.Msg {
		snapshot, err := store.ReplaceSession(context.Background(), params)
		if err == nil {
			return sessionSnapshotMsg{Snapshot: snapshot}
		}
		latest, latestErr := store.GetSession(context.Background(), params.SessionID)
		if latestErr == nil && latest != nil && isRevisionConflict(err) {
			return sessionSnapshotMsg{Snapshot: latest}
		}
		return sessionSnapshotMsg{Err: err}
	}
}

func (m *Model) updateSessionViewCmd() tea.Cmd {
	if m == nil || m.sessionID == "" || m.sessionViewID == "" || m.sessionStore == nil {
		return nil
	}
	store := m.sessionStore
	params := m.currentSessionViewParams()
	return func() tea.Msg {
		view, err := store.UpdateSessionView(context.Background(), params)
		return sessionViewUpdatedMsg{View: view, Err: err}
	}
}

func (m *Model) reloadTerminalSnapshotCmd(terminalID string) tea.Cmd {
	if m == nil || m.runtime == nil || m.runtime.Client() == nil || terminalID == "" {
		return nil
	}
	limit := defaultTerminalSnapshotScrollbackLimit
	if terminal := m.runtime.Registry().Get(terminalID); terminal != nil && terminal.CommittedLoadedDepth > limit {
		limit = terminal.CommittedLoadedDepth
	}
	return func() tea.Msg {
		snapshot, err := m.runtime.LoadSnapshot(context.Background(), terminalID, 0, limit)
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

func (m *Model) currentSessionLeases() []sessionstore.LeaseInfo {
	if m == nil || len(m.sessionLeases) == 0 {
		return nil
	}
	leases := make([]sessionstore.LeaseInfo, 0, len(m.sessionLeases))
	for _, lease := range m.sessionLeases {
		leases = append(leases, lease)
	}
	return leases
}

func (m *Model) currentSessionViewParams() sessionstore.UpdateViewParams {
	params := sessionstore.UpdateViewParams{
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
	if err == nil {
		return false
	}
	if errors.Is(err, sessionstore.ErrConflict) {
		return true
	}
	text := err.Error()
	return strings.Contains(text, "revision conflict") ||
		strings.Contains(text, "sessionstore: revision conflict") ||
		strings.Contains(text, "protocol error 409") ||
		strings.Contains(text, "storage version mismatch")
}

func (m *Model) exportSessionWorkbench() *sessiondoc.Doc {
	doc := workbenchcodec.ExportWorkbench(m.workbench)
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
		baseTabs := make(map[string]*sessiondoc.Tab, len(baseWS.Tabs))
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
