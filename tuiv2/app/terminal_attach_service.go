package app

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/bootstrap"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/orchestrator"
)

type terminalAttachService struct {
	model *Model
}

func (m *Model) terminalAttachService() *terminalAttachService {
	if m == nil {
		return nil
	}
	return &terminalAttachService{model: m}
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
	if s == nil || s.model == nil || s.model.orchestrator == nil || paneID == "" || terminalID == "" {
		return nil
	}
	s.model.markPendingPaneAttach(paneID, terminalID)
	return func() tea.Msg {
		return s.attachMsg(tabID, paneID, terminalID)
	}
}

func (s *terminalAttachService) prepareAndAttachCmd(terminalID string, prepare func() (string, string, error)) tea.Cmd {
	if s == nil || s.model == nil || prepare == nil || terminalID == "" {
		return nil
	}
	tabID, paneID, err := prepare()
	if err != nil {
		return func() tea.Msg { return err }
	}
	s.model.render.Invalidate()
	return batchCmds(s.attachCmd(tabID, paneID, terminalID), s.model.saveStateCmd())
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
		return s.attachMsg("", paneID, terminalID)
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
		msg := s.attachMsg(hint.TabID, hint.PaneID, hint.TerminalID)
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

func (s *terminalAttachService) attachMsg(tabID, paneID, terminalID string) tea.Msg {
	if s == nil || s.model == nil || s.model.orchestrator == nil || paneID == "" || terminalID == "" {
		return nil
	}
	msgs, err := s.model.orchestrator.AttachAndLoadSnapshot(context.Background(), paneID, terminalID, "collaborator", 0, defaultTerminalSnapshotScrollbackLimit)
	if err != nil {
		return paneAttachFailure(paneID, terminalID, err)
	}
	for index := range msgs {
		if attached, ok := msgs[index].(orchestrator.TerminalAttachedMsg); ok {
			attached.TabID = tabID
			msgs[index] = attached
		}
	}
	cmds := make([]tea.Cmd, 0, len(msgs))
	for _, msg := range msgs {
		value := msg
		cmds = append(cmds, func() tea.Msg { return value })
	}
	return tea.Batch(cmds...)()
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
