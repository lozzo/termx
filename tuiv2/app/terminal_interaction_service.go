package app

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/workbench"
)

type terminalInteractionService struct {
	model *Model
}

func (m *Model) terminalInteractionService() *terminalInteractionService {
	if m == nil {
		return nil
	}
	return &terminalInteractionService{model: m}
}

func (s *terminalInteractionService) syncCmd(req terminalInteractionRequest) tea.Cmd {
	target, ok := s.resolveTarget(req)
	if !ok {
		return nil
	}
	return func() tea.Msg {
		if err := s.sync(context.Background(), req, target); err != nil {
			return err
		}
		return nil
	}
}

func (s *terminalInteractionService) sync(ctx context.Context, req terminalInteractionRequest, target terminalInteractionTarget) error {
	if s == nil || s.model == nil || s.model.runtime == nil {
		return nil
	}
	if s.shouldAcquireSessionLease(req, target) {
		if err := s.acquireSessionLease(ctx, target.paneID, target.terminalID); err != nil {
			return err
		}
	}
	if s.shouldAcquireLocalOwnership(req, target) {
		if err := s.model.runtime.AcquireTerminalOwnership(target.paneID, target.terminalID); err != nil {
			return err
		}
	}
	if !req.ResizeIfNeeded {
		return nil
	}
	return s.resizeIfNeeded(ctx, target)
}

func (s *terminalInteractionService) resolveTarget(req terminalInteractionRequest) (terminalInteractionTarget, bool) {
	if s == nil || s.model == nil || s.model.workbench == nil {
		return terminalInteractionTarget{}, false
	}
	targetPaneID := s.model.currentOrActionPaneID(req.PaneID)
	if targetPaneID == "" {
		return terminalInteractionTarget{}, false
	}
	pane, rect, ok := s.model.visiblePaneForInput(targetPaneID)
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

func (s *terminalInteractionService) shouldAcquireSessionLease(req terminalInteractionRequest, target terminalInteractionTarget) bool {
	if s == nil || s.model == nil || s.model.sessionID == "" || s.model.sessionViewID == "" || s.model.runtime == nil || s.model.runtime.Client() == nil {
		return false
	}
	switch {
	case req.ExplicitTakeover:
		return true
	case req.ImplicitSessionLease:
		return s.model.implicitSessionLeaseNeedsAcquire(target.terminalID, target.paneID)
	default:
		return false
	}
}

func (s *terminalInteractionService) shouldAcquireLocalOwnership(req terminalInteractionRequest, target terminalInteractionTarget) bool {
	if s == nil || s.model == nil || s.model.sessionID != "" || s.model.runtime == nil {
		return false
	}
	return s.model.runtime.ShouldAcquireTerminalOwnership(target.terminalID, runtime.TerminalOwnershipRequest{
		PaneID:                   target.paneID,
		ExplicitTakeover:         req.ExplicitTakeover,
		ImplicitInteractiveOwner: req.ImplicitInteractiveOwner,
	})
}

func (s *terminalInteractionService) acquireSessionLease(ctx context.Context, paneID, terminalID string) error {
	if s == nil || s.model == nil || s.model.runtime == nil || s.model.runtime.Client() == nil || s.model.sessionID == "" || s.model.sessionViewID == "" {
		return nil
	}
	lease, err := s.model.runtime.Client().AcquireSessionLease(ctx, acquireSessionLeaseParams(s.model.sessionID, s.model.sessionViewID, paneID, terminalID))
	if err != nil {
		if isSessionLeaseUnsupported(err) {
			return sharedInputLeaseUnsupportedError()
		}
		return err
	}
	sessionRuntime := s.model.sessionRuntimeService()
	if lease != nil && sessionRuntime != nil {
		sessionRuntime.storeLease(*lease)
	}
	if sessionRuntime != nil {
		sessionRuntime.applyCurrentLeases()
	}
	return nil
}

func (s *terminalInteractionService) resizeIfNeeded(ctx context.Context, target terminalInteractionTarget) error {
	if s == nil || s.model == nil || s.model.runtime == nil || s.model.runtime.Client() == nil {
		return nil
	}
	viewportRect, ok := s.model.terminalViewportRect(target.paneID, target.rect)
	if !ok {
		return nil
	}
	targetCols := uint16(maxInt(2, viewportRect.W))
	targetRows := uint16(maxInt(2, viewportRect.H))
	decision := s.model.runtime.ResizeDecision(target.paneID, target.terminalID)
	if !decision.Force && s.model.terminalAlreadySized(target.terminalID, targetCols, targetRows) {
		return nil
	}
	return s.model.runtime.ResizeTerminal(ctx, target.paneID, target.terminalID, targetCols, targetRows)
}

func acquireSessionLeaseParams(sessionID, viewID, paneID, terminalID string) protocol.AcquireSessionLeaseParams {
	return protocol.AcquireSessionLeaseParams{
		SessionID:  sessionID,
		ViewID:     viewID,
		PaneID:     paneID,
		TerminalID: terminalID,
	}
}

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
