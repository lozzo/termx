package app

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/protocol"
)

type sessionRuntimeService struct {
	model *Model
}

func (m *Model) sessionRuntimeService() *sessionRuntimeService {
	if m == nil {
		return nil
	}
	return &sessionRuntimeService{model: m}
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
	if s == nil || s.model == nil || s.model.sessionID == "" || s.model.sessionViewID == "" || s.model.runtime == nil || s.model.runtime.Client() == nil || terminalID == "" {
		return nil
	}
	params := protocol.ReleaseSessionLeaseParams{
		SessionID:  s.model.sessionID,
		ViewID:     s.model.sessionViewID,
		TerminalID: terminalID,
	}
	if err := s.model.runtime.Client().ReleaseSessionLease(ctx, params); err != nil {
		if isSessionLeaseUnsupported(err) {
			return fmt.Errorf("connected termx daemon is too old for shared resize control; restart the daemon and reconnect")
		}
		return err
	}
	s.removeLease(terminalID)
	s.applyCurrentLeases()
	return nil
}

func (s *sessionRuntimeService) reconcileRuntime(ctx context.Context, oldBindings, nextBindings map[string]string) {
	if s == nil || s.model == nil || s.model.runtime == nil {
		return
	}
	for paneID, terminalID := range oldBindings {
		if nextBindings[paneID] == terminalID {
			continue
		}
		s.model.runtime.UnbindPane(paneID, terminalID)
	}
	for paneID, terminalID := range nextBindings {
		if paneID == "" || terminalID == "" {
			continue
		}
		if oldBindings[paneID] == terminalID {
			if binding := s.model.runtime.Binding(paneID); binding != nil && binding.Connected {
				continue
			}
		}
		if _, err := s.model.runtime.AttachTerminal(ctx, paneID, terminalID, "collaborator"); err != nil {
			continue
		}
		_ = s.model.runtime.StartStream(ctx, terminalID)
	}
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
