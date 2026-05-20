package terminalcontrol

import (
	"context"

	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/sessionstore"
)

type SessionLeaseHooks struct {
	SessionID string
	ViewID    string

	NeedsAcquire func(terminalID, paneID string) bool
	Store        func(lease sessionstore.LeaseInfo)
	Remove       func(terminalID string)
	Apply        func()
	Acquire      func(ctx context.Context, params sessionstore.AcquireLeaseParams) (*sessionstore.LeaseInfo, error)
	Release      func(ctx context.Context, params sessionstore.ReleaseLeaseParams) error
}

type SyncRequest struct {
	PaneID                   string
	TerminalID               string
	TargetCols               uint16
	TargetRows               uint16
	ResizeIfNeeded           bool
	ExplicitTakeover         bool
	ImplicitInteractiveOwner bool
	ImplicitSessionLease     bool
}

type Manager struct {
	runtime *runtime.Runtime
	lease   SessionLeaseHooks
}

func NewManager(rt *runtime.Runtime, lease SessionLeaseHooks) *Manager {
	return &Manager{runtime: rt, lease: lease}
}

func (m *Manager) Sync(ctx context.Context, req SyncRequest) error {
	if m == nil || m.runtime == nil {
		return nil
	}
	if m.shouldAcquireSessionLease(req) {
		if err := m.acquireSessionLease(ctx, req.PaneID, req.TerminalID); err != nil {
			return err
		}
	}
	if m.shouldAcquireLocalOwnership(req) {
		if err := m.runtime.AcquireTerminalOwnership(req.PaneID, req.TerminalID); err != nil {
			return err
		}
	}
	if !req.ResizeIfNeeded {
		return nil
	}
	return m.resizeIfNeeded(ctx, req)
}

func (m *Manager) ReleaseLease(ctx context.Context, terminalID string) error {
	if m == nil || terminalID == "" || m.lease.SessionID == "" || m.lease.ViewID == "" || m.lease.Release == nil {
		return nil
	}
	params := sessionstore.ReleaseLeaseParams{
		SessionID:  m.lease.SessionID,
		ViewID:     m.lease.ViewID,
		TerminalID: terminalID,
	}
	if err := m.lease.Release(ctx, params); err != nil {
		return err
	}
	if m.lease.Remove != nil {
		m.lease.Remove(terminalID)
	}
	if m.lease.Apply != nil {
		m.lease.Apply()
	}
	return nil
}

func (m *Manager) shouldAcquireSessionLease(req SyncRequest) bool {
	if m == nil || m.lease.Acquire == nil || m.lease.SessionID == "" || m.lease.ViewID == "" {
		return false
	}
	switch {
	case req.ExplicitTakeover:
		return true
	case req.ImplicitSessionLease:
		return m.lease.NeedsAcquire != nil && m.lease.NeedsAcquire(req.TerminalID, req.PaneID)
	default:
		return false
	}
}

func (m *Manager) shouldAcquireLocalOwnership(req SyncRequest) bool {
	if m == nil || m.runtime == nil || m.lease.SessionID != "" {
		return false
	}
	return m.runtime.ShouldAcquireTerminalOwnership(req.TerminalID, runtime.TerminalOwnershipRequest{
		PaneID:                   req.PaneID,
		ExplicitTakeover:         req.ExplicitTakeover,
		ImplicitInteractiveOwner: req.ImplicitInteractiveOwner,
	})
}

func (m *Manager) acquireSessionLease(ctx context.Context, paneID, terminalID string) error {
	if m == nil || m.lease.Acquire == nil || m.lease.SessionID == "" || m.lease.ViewID == "" {
		return nil
	}
	lease, err := m.lease.Acquire(ctx, sessionstore.AcquireLeaseParams{
		SessionID:  m.lease.SessionID,
		ViewID:     m.lease.ViewID,
		PaneID:     paneID,
		TerminalID: terminalID,
	})
	if err != nil {
		return err
	}
	if lease != nil && m.lease.Store != nil {
		m.lease.Store(*lease)
	}
	if m.lease.Apply != nil {
		m.lease.Apply()
	}
	return nil
}

func (m *Manager) resizeIfNeeded(ctx context.Context, req SyncRequest) error {
	if m == nil || m.runtime == nil || m.runtime.Client() == nil || req.PaneID == "" || req.TerminalID == "" || req.TargetCols == 0 || req.TargetRows == 0 {
		return nil
	}
	decision := m.runtime.ResizeDecision(req.PaneID, req.TerminalID)
	if !decision.Force && m.lease.ViewID == "" && m.terminalAlreadySized(req.TerminalID, req.TargetCols, req.TargetRows) {
		return nil
	}
	return m.runtime.ResizePaneForView(ctx, req.PaneID, req.TerminalID, req.TargetCols, req.TargetRows, m.lease.ViewID)
}

func (m *Manager) terminalAlreadySized(terminalID string, cols, rows uint16) bool {
	if m == nil || m.runtime == nil || terminalID == "" {
		return false
	}
	terminal := m.runtime.Registry().Get(terminalID)
	if terminal == nil || terminal.Snapshot == nil {
		return false
	}
	return terminal.Snapshot.Size.Cols == cols && terminal.Snapshot.Size.Rows == rows
}
