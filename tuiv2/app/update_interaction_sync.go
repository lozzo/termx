package app

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/workbench"
)

type terminalInteractionRequest struct {
	PaneID                   string
	TerminalID               string
	Rect                     workbench.Rect
	ResizeIfNeeded           bool
	ExplicitTakeover         bool
	ImplicitInteractiveOwner bool
	ImplicitSessionLease     bool
}

type terminalInteractionTarget struct {
	paneID     string
	terminalID string
	rect       workbench.Rect
}

func (m *Model) syncTerminalInteractionCmd(req terminalInteractionRequest) tea.Cmd {
	target, ok := m.resolveTerminalInteractionTarget(req)
	if !ok {
		return nil
	}
	return func() tea.Msg {
		if err := m.syncTerminalInteraction(context.Background(), req, target); err != nil {
			return err
		}
		return nil
	}
}

func (m *Model) syncTerminalInteraction(ctx context.Context, req terminalInteractionRequest, target terminalInteractionTarget) error {
	if m == nil || m.runtime == nil {
		return nil
	}
	if m.shouldAcquireSessionLease(req, target) {
		if err := m.acquireSessionLease(ctx, target.paneID, target.terminalID); err != nil {
			return err
		}
	}
	if m.shouldAcquireLocalOwnership(req, target) {
		if err := m.runtime.AcquireTerminalOwnership(target.paneID, target.terminalID); err != nil {
			return err
		}
	}
	if !req.ResizeIfNeeded {
		return nil
	}
	return m.resizeTerminalIfNeeded(ctx, target)
}

func (m *Model) resolveTerminalInteractionTarget(req terminalInteractionRequest) (terminalInteractionTarget, bool) {
	if m == nil || m.workbench == nil {
		return terminalInteractionTarget{}, false
	}
	targetPaneID := m.currentOrActionPaneID(req.PaneID)
	if targetPaneID == "" {
		return terminalInteractionTarget{}, false
	}
	pane, rect, ok := m.visiblePaneForInput(targetPaneID)
	if !ok || pane == nil || pane.TerminalID == "" {
		return terminalInteractionTarget{}, false
	}
	target := terminalInteractionTarget{
		paneID:     pane.ID,
		terminalID: pane.TerminalID,
		rect:       rect,
	}
	if req.TerminalID != "" {
		target.terminalID = req.TerminalID
	}
	if req.Rect.W > 0 && req.Rect.H > 0 {
		target.rect = req.Rect
	}
	return target, true
}

func (m *Model) shouldAcquireSessionLease(req terminalInteractionRequest, target terminalInteractionTarget) bool {
	if m == nil || m.sessionID == "" || m.sessionViewID == "" || m.runtime == nil || m.runtime.Client() == nil {
		return false
	}
	switch {
	case req.ExplicitTakeover:
		return true
	case req.ImplicitSessionLease:
		return m.implicitSessionLeaseNeedsAcquire(target.terminalID, target.paneID)
	default:
		return false
	}
}

func (m *Model) shouldAcquireLocalOwnership(req terminalInteractionRequest, target terminalInteractionTarget) bool {
	if m == nil || m.sessionID != "" || m.runtime == nil {
		return false
	}
	terminal := m.runtime.Registry().Get(target.terminalID)
	if terminal == nil {
		return false
	}
	if req.ExplicitTakeover {
		return terminal.OwnerPaneID != target.paneID
	}
	if !req.ImplicitInteractiveOwner {
		return false
	}
	cursorVisible := false
	switch {
	case terminal.VTerm != nil:
		cursorVisible = terminal.VTerm.CursorState().Visible
	case terminal.Snapshot != nil:
		cursorVisible = terminal.Snapshot.Cursor.Visible
	}
	if len(terminal.BoundPaneIDs) < 2 || !cursorVisible {
		return false
	}
	return terminal.OwnerPaneID != target.paneID
}

func (m *Model) acquireSessionLease(ctx context.Context, paneID, terminalID string) error {
	if m == nil || m.runtime == nil || m.runtime.Client() == nil || m.sessionID == "" || m.sessionViewID == "" {
		return nil
	}
	lease, err := m.runtime.Client().AcquireSessionLease(ctx, acquireSessionLeaseParams(m.sessionID, m.sessionViewID, paneID, terminalID))
	if err != nil {
		if isSessionLeaseUnsupported(err) {
			return sharedInputLeaseUnsupportedError()
		}
		return err
	}
	if lease != nil {
		if m.sessionLeases == nil {
			m.sessionLeases = make(map[string]protocol.LeaseInfo)
		}
		m.sessionLeases[lease.TerminalID] = *lease
	}
	m.runtime.ApplySessionLeases(m.sessionViewID, m.currentSessionLeases())
	return nil
}

func acquireSessionLeaseParams(sessionID, viewID, paneID, terminalID string) protocol.AcquireSessionLeaseParams {
	return protocol.AcquireSessionLeaseParams{
		SessionID:  sessionID,
		ViewID:     viewID,
		PaneID:     paneID,
		TerminalID: terminalID,
	}
}

func (m *Model) resizeTerminalIfNeeded(ctx context.Context, target terminalInteractionTarget) error {
	if m == nil || m.runtime == nil || m.runtime.Client() == nil {
		return nil
	}
	viewportRect, ok := m.terminalViewportRect(target.paneID, target.rect)
	if !ok {
		return nil
	}
	targetCols := uint16(maxInt(2, viewportRect.W))
	targetRows := uint16(maxInt(2, viewportRect.H))
	if !m.shouldForceTerminalResize(target.terminalID) && m.terminalAlreadySized(target.terminalID, targetCols, targetRows) {
		return nil
	}
	return m.runtime.ResizeTerminal(ctx, target.paneID, target.terminalID, targetCols, targetRows)
}

func (m *Model) shouldForceTerminalResize(terminalID string) bool {
	if m == nil || m.runtime == nil || terminalID == "" {
		return false
	}
	terminal := m.runtime.Registry().Get(terminalID)
	return terminal != nil && terminal.PendingOwnerResize
}
