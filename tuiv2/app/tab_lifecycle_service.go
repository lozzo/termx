package app

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
)

type tabLifecycleService struct {
	model *Model
}

type tabTerminalBinding struct {
	paneID     string
	terminalID string
}

func (m *Model) tabLifecycleService() *tabLifecycleService {
	if m == nil {
		return nil
	}
	return &tabLifecycleService{model: m}
}

func (s *tabLifecycleService) closeAndSaveCmd(tabID string, kill bool) tea.Cmd {
	if s == nil || s.model == nil || s.model.workbench == nil || tabID == "" {
		return nil
	}
	bindings, terminalIDs := s.snapshotTabBindings(tabID)
	return func() tea.Msg {
		if err := s.close(tabID, bindings, terminalIDs, kill); err != nil {
			return err
		}
		if cmd := s.model.saveStateCmd(); cmd != nil {
			return cmd()
		}
		return nil
	}
}

func (s *tabLifecycleService) close(tabID string, bindings []tabTerminalBinding, terminalIDs []string, kill bool) error {
	if s == nil || s.model == nil || s.model.workbench == nil {
		return nil
	}
	if err := s.model.workbench.CloseTab(tabID); err != nil {
		return err
	}
	if s.model.runtime != nil {
		for _, binding := range bindings {
			s.model.runtime.UnbindPane(binding.paneID, binding.terminalID)
		}
	}
	if kill && s.model.runtime != nil && s.model.runtime.Client() != nil {
		for _, terminalID := range terminalIDs {
			_ = s.model.runtime.Client().Kill(context.Background(), terminalID)
		}
	}
	s.model.render.Invalidate()
	return nil
}

func (s *tabLifecycleService) snapshotTabBindings(tabID string) ([]tabTerminalBinding, []string) {
	if s == nil || s.model == nil || s.model.workbench == nil || tabID == "" {
		return nil, nil
	}
	seenTerminals := make(map[string]struct{})
	var bindings []tabTerminalBinding
	var terminalIDs []string
	for _, wsName := range s.model.workbench.ListWorkspaces() {
		ws := s.model.workbench.WorkspaceByName(wsName)
		if ws == nil {
			continue
		}
		for _, tab := range ws.Tabs {
			if tab == nil || tab.ID != tabID {
				continue
			}
			for paneID, pane := range tab.Panes {
				if pane == nil {
					continue
				}
				bindings = append(bindings, tabTerminalBinding{
					paneID:     paneID,
					terminalID: pane.TerminalID,
				})
				if pane.TerminalID == "" {
					continue
				}
				if _, ok := seenTerminals[pane.TerminalID]; ok {
					continue
				}
				seenTerminals[pane.TerminalID] = struct{}{}
				terminalIDs = append(terminalIDs, pane.TerminalID)
			}
			return bindings, terminalIDs
		}
	}
	return bindings, terminalIDs
}
