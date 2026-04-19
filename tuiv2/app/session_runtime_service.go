package app

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/runtime"
)

type sessionRuntimeService struct {
	model *Model
}

type sessionRuntimeApplyResult struct {
	failedBindings map[string]error
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

func (s *sessionRuntimeService) reconcileRuntime(ctx context.Context, oldBindings, nextBindings map[string]string) sessionRuntimeApplyResult {
	if s == nil || s.model == nil || s.model.runtime == nil {
		return sessionRuntimeApplyResult{}
	}
	result := sessionRuntimeApplyResult{failedBindings: make(map[string]error)}
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
		if err := s.attachAndBootstrap(ctx, paneID, oldBindings[paneID], terminalID); err != nil {
			result.failedBindings[paneID] = err
		}
	}
	return result
}

type runtimeAttachRollback struct {
	paneID            string
	terminalID        string
	previousBinding   *runtime.PaneBinding
	previousControl   runtime.TerminalControlStatus
	previousLiveState runtime.TerminalLiveStateSnapshot
	targetControl     runtime.TerminalControlStatus
	targetAttachment  runtime.TerminalAttachmentSnapshot
	targetLiveState   runtime.TerminalLiveStateSnapshot
}

func (s *sessionRuntimeService) attachAndBootstrap(ctx context.Context, paneID, previousTerminalID, terminalID string) error {
	if s == nil || s.model == nil || s.model.runtime == nil || paneID == "" || terminalID == "" {
		return nil
	}
	rollback := s.captureAttachRollback(paneID, previousTerminalID, terminalID)
	if _, err := s.model.runtime.AttachTerminal(ctx, paneID, terminalID, "collaborator"); err != nil {
		return err
	}
	if _, err := s.model.runtime.LoadSnapshot(ctx, terminalID, 0, defaultTerminalSnapshotScrollbackLimit); err != nil {
		s.rollbackAttachBootstrap(rollback)
		return err
	}
	if err := s.model.runtime.StartStream(ctx, terminalID); err != nil {
		s.rollbackAttachBootstrap(rollback)
		return err
	}
	return nil
}

func (s *sessionRuntimeService) captureAttachRollback(paneID, previousTerminalID, terminalID string) runtimeAttachRollback {
	rollback := runtimeAttachRollback{
		paneID:     paneID,
		terminalID: terminalID,
	}
	if s == nil || s.model == nil || s.model.runtime == nil {
		return rollback
	}
	rollback.previousBinding = runtime.ClonePaneBinding(s.model.runtime.Binding(paneID))
	if previousTerminalID != "" {
		rollback.previousControl = s.model.runtime.TerminalControlStatus(previousTerminalID)
		rollback.previousLiveState = s.model.runtime.TerminalLiveStateSnapshot(previousTerminalID)
	}
	rollback.targetControl = s.model.runtime.TerminalControlStatus(terminalID)
	rollback.targetAttachment = s.model.runtime.TerminalAttachmentSnapshot(terminalID)
	rollback.targetLiveState = s.model.runtime.TerminalLiveStateSnapshot(terminalID)
	return rollback
}

func (s *sessionRuntimeService) rollbackAttachBootstrap(rollback runtimeAttachRollback) {
	if s == nil || s.model == nil || s.model.runtime == nil {
		return
	}
	s.model.runtime.UnbindPane(rollback.paneID, rollback.terminalID)
	if rollback.previousLiveState.TerminalID != "" {
		s.model.runtime.RestoreTerminalLiveState(rollback.previousLiveState.TerminalID, rollback.previousLiveState)
	}
	if rollback.targetLiveState.TerminalID != "" && rollback.targetLiveState.TerminalID != rollback.previousLiveState.TerminalID {
		s.model.runtime.RestoreTerminalLiveState(rollback.targetLiveState.TerminalID, rollback.targetLiveState)
	}
	s.model.runtime.RestorePaneBinding(rollback.paneID, rollback.previousBinding)
	if rollback.previousControl.TerminalID != "" {
		s.model.runtime.RestoreTerminalControlStatus(rollback.previousControl)
	}
	if rollback.targetControl.TerminalID != "" && rollback.targetControl.TerminalID != rollback.previousControl.TerminalID {
		s.model.runtime.RestoreTerminalControlStatus(rollback.targetControl)
	}
	s.model.runtime.RestoreTerminalAttachmentSnapshot(rollback.terminalID, rollback.targetAttachment)
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
