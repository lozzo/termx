package sessionruntime

import (
	"context"

	"github.com/lozzow/termx/tuiv2/runtime"
)

const collaboratorAttachMode = "collaborator"

type ApplyResult struct {
	FailedBindings map[string]error
}

type Manager struct {
	runtime       *runtime.Runtime
	snapshotLimit int
}

type attachRollback struct {
	paneID            string
	terminalID        string
	previousBinding   *runtime.PaneBinding
	previousControl   runtime.TerminalControlStatus
	previousLiveState runtime.TerminalLiveStateSnapshot
	targetControl     runtime.TerminalControlStatus
	targetAttachment  runtime.TerminalAttachmentSnapshot
	targetLiveState   runtime.TerminalLiveStateSnapshot
}

func NewManager(rt *runtime.Runtime, snapshotLimit int) *Manager {
	return &Manager{runtime: rt, snapshotLimit: snapshotLimit}
}

func (m *Manager) Reconcile(ctx context.Context, oldBindings, nextBindings map[string]string) ApplyResult {
	if m == nil || m.runtime == nil {
		return ApplyResult{}
	}
	result := ApplyResult{FailedBindings: make(map[string]error)}
	for paneID, terminalID := range oldBindings {
		if nextBindings[paneID] == terminalID {
			continue
		}
		m.runtime.UnbindPane(paneID, terminalID)
	}
	for paneID, terminalID := range nextBindings {
		if paneID == "" || terminalID == "" {
			continue
		}
		if oldBindings[paneID] == terminalID {
			if binding := m.runtime.Binding(paneID); binding != nil && binding.Connected {
				continue
			}
		}
		if err := m.attachAndBootstrap(ctx, paneID, oldBindings[paneID], terminalID); err != nil {
			result.FailedBindings[paneID] = err
		}
	}
	return result
}

func (m *Manager) attachAndBootstrap(ctx context.Context, paneID, previousTerminalID, terminalID string) error {
	if m == nil || m.runtime == nil || paneID == "" || terminalID == "" {
		return nil
	}
	rollback := m.captureAttachRollback(paneID, previousTerminalID, terminalID)
	if _, err := m.runtime.AttachTerminal(ctx, paneID, terminalID, collaboratorAttachMode); err != nil {
		return err
	}
	if _, err := m.runtime.LoadSnapshot(ctx, terminalID, 0, m.snapshotLimit); err != nil {
		m.rollbackAttachBootstrap(rollback)
		return err
	}
	if err := m.runtime.StartStream(ctx, terminalID); err != nil {
		m.rollbackAttachBootstrap(rollback)
		return err
	}
	return nil
}

func (m *Manager) captureAttachRollback(paneID, previousTerminalID, terminalID string) attachRollback {
	rollback := attachRollback{
		paneID:     paneID,
		terminalID: terminalID,
	}
	if m == nil || m.runtime == nil {
		return rollback
	}
	rollback.previousBinding = runtime.ClonePaneBinding(m.runtime.Binding(paneID))
	if previousTerminalID != "" {
		rollback.previousControl = m.runtime.TerminalControlStatus(previousTerminalID)
		rollback.previousLiveState = m.runtime.TerminalLiveStateSnapshot(previousTerminalID)
	}
	rollback.targetControl = m.runtime.TerminalControlStatus(terminalID)
	rollback.targetAttachment = m.runtime.TerminalAttachmentSnapshot(terminalID)
	rollback.targetLiveState = m.runtime.TerminalLiveStateSnapshot(terminalID)
	return rollback
}

func (m *Manager) rollbackAttachBootstrap(rollback attachRollback) {
	if m == nil || m.runtime == nil {
		return
	}
	m.runtime.UnbindPane(rollback.paneID, rollback.terminalID)
	if rollback.previousLiveState.TerminalID != "" {
		m.runtime.RestoreTerminalLiveState(rollback.previousLiveState.TerminalID, rollback.previousLiveState)
	}
	if rollback.targetLiveState.TerminalID != "" && rollback.targetLiveState.TerminalID != rollback.previousLiveState.TerminalID {
		m.runtime.RestoreTerminalLiveState(rollback.targetLiveState.TerminalID, rollback.targetLiveState)
	}
	m.runtime.RestorePaneBinding(rollback.paneID, rollback.previousBinding)
	if rollback.previousControl.TerminalID != "" {
		m.runtime.RestoreTerminalControlStatus(rollback.previousControl)
	}
	if rollback.targetControl.TerminalID != "" && rollback.targetControl.TerminalID != rollback.previousControl.TerminalID {
		m.runtime.RestoreTerminalControlStatus(rollback.targetControl)
	}
	m.runtime.RestoreTerminalAttachmentSnapshot(rollback.terminalID, rollback.targetAttachment)
}
