package app

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/terminalcontrol"
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

func (s *terminalInteractionService) manager() *terminalcontrol.Manager {
	if s == nil || s.model == nil {
		return nil
	}
	return s.model.terminalControlManager()
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
	manager := s.manager()
	if manager == nil {
		return nil
	}
	return manager.Sync(ctx, s.controlRequest(req, target))
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

func (s *terminalInteractionService) controlRequest(req terminalInteractionRequest, target terminalInteractionTarget) terminalcontrol.SyncRequest {
	controlReq := terminalcontrol.SyncRequest{
		PaneID:                   target.paneID,
		TerminalID:               target.terminalID,
		ResizeIfNeeded:           req.ResizeIfNeeded,
		ExplicitTakeover:         req.ExplicitTakeover,
		ImplicitInteractiveOwner: req.ImplicitInteractiveOwner,
		ImplicitSessionLease:     req.ImplicitSessionLease,
	}
	if s == nil || s.model == nil || !req.ResizeIfNeeded {
		return controlReq
	}
	viewportRect, ok := s.model.terminalViewportRect(target.paneID, target.rect)
	if !ok {
		return controlReq
	}
	controlReq.TargetCols = uint16(maxInt(2, viewportRect.W))
	controlReq.TargetRows = uint16(maxInt(2, viewportRect.H))
	return controlReq
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
