package app

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/termx-core/protocol"
	"github.com/lozzow/termx/tuiv2/bootstrap"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/orchestrator"
	"github.com/lozzow/termx/tuiv2/terminalattach"
)

type terminalAttachService struct {
	model *Model
}

type terminalAttachRequest struct {
	tabID                 string
	paneID                string
	terminalID            string
	mode                  string
	offset                int
	limit                 int
	cleanupPreparedTarget bool
	previousSelection     terminalattach.Selection
}

func (m *Model) terminalAttachService() *terminalAttachService {
	if m == nil {
		return nil
	}
	return &terminalAttachService{model: m}
}

func (s *terminalAttachService) manager() *terminalattach.Manager {
	if s == nil || s.model == nil {
		return nil
	}
	return terminalattach.NewManager(s.model.workbench, s.model.runtime, s.model.orchestrator)
}

func (s *terminalAttachService) splitAndAttachCmd(paneID, terminalID string) tea.Cmd {
	if s == nil || s.model == nil || s.model.orchestrator == nil || paneID == "" || terminalID == "" {
		return nil
	}
	return s.prepareAndAttachCmd(terminalID, func() (string, string, error) {
		return s.model.orchestrator.PrepareSplitAttachTarget(paneID)
	})
}

func (s *terminalAttachService) createTabAndAttachCmd(terminalID string) tea.Cmd {
	if s == nil || s.model == nil || s.model.orchestrator == nil || terminalID == "" {
		return nil
	}
	return s.prepareAndAttachCmd(terminalID, func() (string, string, error) {
		return s.model.orchestrator.PrepareTabAttachTarget()
	})
}

func (s *terminalAttachService) createFloatingAndAttachCmd(terminalID string) tea.Cmd {
	if s == nil || s.model == nil || s.model.orchestrator == nil || terminalID == "" {
		return nil
	}
	return s.prepareAndAttachCmd(terminalID, func() (string, string, error) {
		return s.model.orchestrator.PrepareFloatingAttachTarget()
	})
}

func (s *terminalAttachService) attachCmd(tabID, paneID, terminalID string) tea.Cmd {
	return s.attachWithModeCmd(tabID, paneID, terminalID, "collaborator")
}

func (s *terminalAttachService) attachWithModeCmd(tabID, paneID, terminalID, mode string) tea.Cmd {
	if s == nil || s.model == nil || s.model.orchestrator == nil || paneID == "" || terminalID == "" {
		return nil
	}
	s.model.markPendingPaneAttach(paneID, terminalID)
	req := terminalAttachRequest{
		tabID:             tabID,
		paneID:            paneID,
		terminalID:        terminalID,
		mode:              mode,
		offset:            0,
		limit:             defaultTerminalSnapshotScrollbackLimit,
		previousSelection: s.currentWorkbenchSelection(),
	}
	return func() tea.Msg {
		return s.attachMsg(req)
	}
}

func (s *terminalAttachService) prepareAndAttachCmd(terminalID string, prepare func() (string, string, error)) tea.Cmd {
	if s == nil || s.model == nil || prepare == nil || terminalID == "" {
		return nil
	}
	selection := s.currentWorkbenchSelection()
	tabID, paneID, err := prepare()
	if err != nil {
		return func() tea.Msg { return err }
	}
	s.model.render.Invalidate()
	s.model.markPendingPaneAttach(paneID, terminalID)
	req := terminalAttachRequest{
		tabID:                 tabID,
		paneID:                paneID,
		terminalID:            terminalID,
		mode:                  "collaborator",
		offset:                0,
		limit:                 defaultTerminalSnapshotScrollbackLimit,
		cleanupPreparedTarget: true,
		previousSelection:     selection,
	}
	return func() tea.Msg { return s.attachMsg(req) }
}

func (s *terminalAttachService) restartAndAttachCmd(paneID, terminalID string) tea.Cmd {
	if s == nil || s.model == nil || s.model.runtime == nil || s.model.orchestrator == nil || paneID == "" || terminalID == "" {
		return nil
	}
	s.model.markPendingPaneAttach(paneID, terminalID)
	return func() tea.Msg {
		client := s.model.runtime.Client()
		if client == nil {
			return paneAttachFailure(paneID, terminalID, teaErr("attach terminal: runtime client is nil"))
		}
		if err := client.Restart(context.Background(), terminalID); err != nil {
			return paneAttachFailure(paneID, terminalID, err)
		}
		return s.attachMsg(terminalAttachRequest{
			paneID:     paneID,
			terminalID: terminalID,
			mode:       "collaborator",
			offset:     0,
			limit:      defaultTerminalSnapshotScrollbackLimit,
		})
	}
}

func (s *terminalAttachService) createAndAttachCmd(paneID string, target modal.CreateTargetKind, params protocol.CreateParams) tea.Cmd {
	if s == nil || s.model == nil || s.model.runtime == nil {
		return nil
	}
	if paneID != "" {
		s.model.markPendingPaneAttach(paneID, "")
	}
	return func() tea.Msg {
		client := s.model.runtime.Client()
		if client == nil {
			return paneAttachFailure(paneID, "", context.Canceled)
		}
		created, err := client.Create(context.Background(), params)
		if err != nil {
			return paneAttachFailure(paneID, "", err)
		}
		s.primeCreatedTerminal(created, params)
		if paneID != "" {
			s.model.markPendingPaneAttach(paneID, created.TerminalID)
		}
		switch target {
		case modal.CreateTargetSplit:
			if paneID != "" {
				s.model.clearPendingPaneAttach(paneID, created.TerminalID)
			}
			if cmd := s.model.splitPaneAndAttachTerminalCmd(paneID, created.TerminalID); cmd != nil {
				return cmd()
			}
			return nil
		case modal.CreateTargetNewTab:
			if paneID != "" {
				s.model.clearPendingPaneAttach(paneID, created.TerminalID)
			}
			if cmd := s.model.createTabAndAttachTerminalCmd(created.TerminalID); cmd != nil {
				return cmd()
			}
			return nil
		case modal.CreateTargetFloating:
			if paneID != "" {
				s.model.clearPendingPaneAttach(paneID, created.TerminalID)
			}
			if cmd := s.model.createFloatingPaneAndAttachTerminalCmd(created.TerminalID); cmd != nil {
				return cmd()
			}
			return nil
		default:
			if cmd := s.attachCmd("", paneID, created.TerminalID); cmd != nil {
				return cmd()
			}
			return nil
		}
	}
}

func (s *terminalAttachService) reattachRestoredCmd(hint bootstrap.PaneReattachHint) tea.Cmd {
	if s == nil || s.model == nil || hint.PaneID == "" || hint.TerminalID == "" {
		return nil
	}
	return func() tea.Msg {
		msg := s.attachMsg(terminalAttachRequest{
			tabID:      hint.TabID,
			paneID:     hint.PaneID,
			terminalID: hint.TerminalID,
			mode:       "collaborator",
			offset:     0,
			limit:      defaultTerminalSnapshotScrollbackLimit,
		})
		switch msg.(type) {
		case nil:
			s.rollbackRestoredBinding(hint.TabID, hint.PaneID, hint.TerminalID)
			return reattachFailedMsg{tabID: hint.TabID, paneID: hint.PaneID}
		case error, paneAttachFailedMsg:
			s.rollbackRestoredBinding(hint.TabID, hint.PaneID, hint.TerminalID)
			return reattachFailedMsg{tabID: hint.TabID, paneID: hint.PaneID}
		default:
			return msg
		}
	}
}

func (s *terminalAttachService) handleAttachedMsg(attached orchestrator.TerminalAttachedMsg) tea.Cmd {
	if s == nil || s.model == nil {
		return nil
	}
	s.model.clearPendingPaneAttach(attached.PaneID, attached.TerminalID)
	s.model.resetPaneViewport(attached.PaneID)
	if s.model.modalHost != nil && s.model.modalHost.Session != nil && s.model.modalHost.Session.Kind == input.ModePicker {
		s.model.closeModal(input.ModePicker, s.model.modalHost.Session.RequestID, input.ModeState{Kind: input.ModeNormal})
	}
	s.model.render.Invalidate()
	return batchCmds(s.model.saveStateCmd(), s.finalizeAttachCmd(attached.TabID, attached.PaneID, attached.TerminalID))
}

func (s *terminalAttachService) attachMsg(req terminalAttachRequest) tea.Msg {
	if s == nil || s.model == nil || s.model.orchestrator == nil || req.paneID == "" || req.terminalID == "" {
		return nil
	}
	attached, err := s.executeAttach(context.Background(), req)
	if err != nil {
		return paneAttachFailure(req.paneID, req.terminalID, err)
	}
	return attached
}

func (s *terminalAttachService) executeAttach(ctx context.Context, req terminalAttachRequest) (orchestrator.TerminalAttachedMsg, error) {
	manager := s.manager()
	if manager == nil {
		return orchestrator.TerminalAttachedMsg{}, teaErr("attach terminal: service unavailable")
	}
	return manager.Execute(ctx, terminalattach.Request{
		TabID:                 req.tabID,
		PaneID:                req.paneID,
		TerminalID:            req.terminalID,
		Mode:                  req.mode,
		Offset:                req.offset,
		Limit:                 req.limit,
		CleanupPreparedTarget: req.cleanupPreparedTarget,
		PreviousSelection:     req.previousSelection,
	})
}

func (s *terminalAttachService) currentWorkbenchSelection() terminalattach.Selection {
	if s == nil || s.model == nil || s.model.workbench == nil {
		return terminalattach.Selection{}
	}
	selection := terminalattach.Selection{}
	if ws := s.model.workbench.CurrentWorkspace(); ws != nil {
		selection.WorkspaceName = ws.Name
	}
	if tab := s.model.workbench.CurrentTab(); tab != nil {
		selection.ActiveTabID = tab.ID
		selection.FocusedPaneID = tab.ActivePaneID
	}
	return selection
}

func (s *terminalAttachService) primeCreatedTerminal(created *protocol.CreateResult, params protocol.CreateParams) {
	if s == nil || s.model == nil || s.model.runtime == nil || s.model.runtime.Registry() == nil || created == nil {
		return
	}
	terminal := s.model.runtime.Registry().GetOrCreate(created.TerminalID)
	if terminal == nil {
		return
	}
	terminal.Name = params.Name
	terminal.Tags = cloneStringMap(params.Tags)
	terminal.Command = append([]string(nil), params.Command...)
	terminal.State = created.State
}

func (s *terminalAttachService) rollbackRestoredBinding(tabID, paneID, terminalID string) {
	if s == nil || s.model == nil {
		return
	}
	s.model.clearPendingPaneAttach(paneID, terminalID)
	if s.model.workbench != nil && tabID != "" {
		_ = s.model.workbench.BindPaneTerminal(tabID, paneID, "")
	}
}

func (s *terminalAttachService) finalizeAttachCmd(tabID, paneID, terminalID string) tea.Cmd {
	if s == nil || s.model == nil || paneID == "" || terminalID == "" {
		return nil
	}
	return func() tea.Msg {
		if s.model.sessionID == "" {
			if pane, rect, ok := s.model.paneResizeTarget(tabID, paneID); ok && pane != nil && pane.TerminalID == terminalID {
				if err := s.model.ensurePaneTerminalSize(context.Background(), paneID, terminalID, rect); err != nil {
					return err
				}
				s.model.clearPendingPaneResize(paneID, terminalID)
			} else {
				s.model.markPendingPaneResize(tabID, paneID, terminalID)
			}
		}
		return terminalAttachReadyMsg{paneID: paneID, terminalID: terminalID}
	}
}
