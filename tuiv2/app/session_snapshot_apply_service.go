package app

import (
	"context"
	"fmt"

	"github.com/lozzow/termx/termx-core/protocol"
	"github.com/lozzow/termx/tuiv2/orchestrator"
	"github.com/lozzow/termx/tuiv2/workbenchcodec"
)

type sessionSnapshotApplyService struct {
	model *Model
}

func (m *Model) sessionSnapshotApplyService() *sessionSnapshotApplyService {
	if m == nil {
		return nil
	}
	return &sessionSnapshotApplyService{model: m}
}

func (s *sessionSnapshotApplyService) apply(snapshot *protocol.SessionSnapshot) error {
	if s == nil || s.model == nil || snapshot == nil {
		return nil
	}
	m := s.model
	projection := m.captureLocalViewProjection()
	currentViewID := m.sessionViewID
	if snapshot.Session.ID != "" {
		m.sessionID = snapshot.Session.ID
	}
	if snapshot.View != nil {
		m.sessionViewID = snapshot.View.ViewID
		if shouldAdoptSnapshotViewProjection(currentViewID, snapshot.View.ViewID, projection) {
			projection.WorkspaceName = snapshot.View.ActiveWorkspaceName
			projection.ActiveTabID = snapshot.View.ActiveTabID
			projection.FocusedPaneID = snapshot.View.FocusedPaneID
		}
	}
	m.sessionRevision = snapshot.Session.Revision
	if snapshot.Workbench != nil {
		m.sessionSharedDoc = snapshot.Workbench.Clone()
	}
	if len(snapshot.Leases) > 0 {
		m.sessionLeases = make(map[string]protocol.LeaseInfo, len(snapshot.Leases))
		for _, lease := range snapshot.Leases {
			if lease.TerminalID != "" {
				m.sessionLeases[lease.TerminalID] = lease
			}
		}
	} else {
		m.sessionLeases = nil
	}
	if snapshot.Workbench == nil {
		if service := m.sessionRuntimeService(); service != nil {
			service.applyCurrentLeases()
		}
		m.render.Invalidate()
		return nil
	}

	oldBindings := workbenchcodec.PaneTerminalBindings(workbenchcodec.ExportWorkbench(m.workbench))
	nextBindings := workbenchcodec.PaneTerminalBindings(snapshot.Workbench)
	nextWorkbench := workbenchcodec.ImportDoc(snapshot.Workbench)

	var applyErr error
	if m.runtime != nil {
		result := m.reconcileSessionRuntime(context.Background(), oldBindings, nextBindings)
		clearFailedSessionPaneBindings(nextWorkbench, result.FailedBindings)
		if service := m.sessionRuntimeService(); service != nil {
			service.applyCurrentLeases()
		}
		if len(result.FailedBindings) > 0 {
			applyErr = fmt.Errorf("session snapshot applied with %d unattached pane(s)", len(result.FailedBindings))
		}
	}

	m.workbench = nextWorkbench
	m.applyLocalViewProjection(projection)
	m.orchestrator = orchestrator.New(m.workbench)
	m.render.Invalidate()
	return applyErr
}

func clearFailedSessionPaneBindings(wb interface {
	ResolvePaneTab(tabID, paneID string) (string, error)
	BindPaneTerminal(tabID, paneID, terminalID string) error
}, failures map[string]error) {
	if wb == nil || len(failures) == 0 {
		return
	}
	for paneID := range failures {
		tabID, err := wb.ResolvePaneTab("", paneID)
		if err != nil || tabID == "" {
			continue
		}
		_ = wb.BindPaneTerminal(tabID, paneID, "")
	}
}
