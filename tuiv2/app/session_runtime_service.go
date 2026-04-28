package app

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/sessionruntime"
)

type sessionRuntimeService struct {
	model *Model
}

type sessionRuntimeApplyResult = sessionruntime.ApplyResult

func (m *Model) sessionRuntimeService() *sessionRuntimeService {
	if m == nil {
		return nil
	}
	return &sessionRuntimeService{model: m}
}

func (s *sessionRuntimeService) manager() *sessionruntime.Manager {
	if s == nil || s.model == nil || s.model.runtime == nil {
		return nil
	}
	return sessionruntime.NewManager(s.model.runtime, defaultTerminalSnapshotScrollbackLimit)
}

func (s *sessionRuntimeService) releaseLeaseCmd(terminalID string) tea.Cmd {
	if s == nil || s.model == nil || s.model.sessionID == "" || s.model.sessionViewID == "" || s.model.runtime == nil || s.model.runtime.Client() == nil || terminalID == "" {
		return nil
	}
	return func() tea.Msg {
		if err := s.releaseLease(context.Background(), terminalID); err != nil {
			return err
		}
		return nil
	}
}

func (s *sessionRuntimeService) releaseLease(ctx context.Context, terminalID string) error {
	if s == nil || s.model == nil || terminalID == "" {
		return nil
	}
	manager := s.model.terminalControlManager()
	if manager == nil {
		return nil
	}
	return manager.ReleaseLease(ctx, terminalID)
}

func (s *sessionRuntimeService) reconcileRuntime(ctx context.Context, oldBindings, nextBindings map[string]string) sessionRuntimeApplyResult {
	manager := s.manager()
	if manager == nil {
		return sessionRuntimeApplyResult{}
	}
	return manager.Reconcile(ctx, oldBindings, nextBindings)
}

func (s *sessionRuntimeService) storeLease(lease protocol.LeaseInfo) {
	if s == nil || s.model == nil || lease.TerminalID == "" {
		return
	}
	if s.model.sessionLeases == nil {
		s.model.sessionLeases = make(map[string]protocol.LeaseInfo)
	}
	s.model.sessionLeases[lease.TerminalID] = lease
}

func (s *sessionRuntimeService) removeLease(terminalID string) {
	if s == nil || s.model == nil || s.model.sessionLeases == nil || terminalID == "" {
		return
	}
	delete(s.model.sessionLeases, terminalID)
	if len(s.model.sessionLeases) == 0 {
		s.model.sessionLeases = nil
	}
}

func (s *sessionRuntimeService) applyCurrentLeases() {
	if s == nil || s.model == nil || s.model.runtime == nil {
		return
	}
	s.model.runtime.ApplySessionLeases(s.model.sessionViewID, s.model.currentSessionLeases())
}
