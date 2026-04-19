package app

import (
	"strings"

	"github.com/lozzow/termx/tuiv2/runtime"
)

type paneBindingLifecycleService struct {
	model *Model
}

type paneBindingTarget struct {
	TabID      string
	PaneID     string
	TerminalID string
}

type reconnectPaneResult struct {
	Target             paneBindingTarget
	KeptExitedTerminal bool
}

type bindDetachedTerminalRequest struct {
	TabID      string
	PaneID     string
	TerminalID string
	Binding    runtime.DetachedTerminalBinding
}

type bindDetachedTerminalResult struct {
	Target            paneBindingTarget
	LoadSnapshotAfter bool
}

func (m *Model) paneBindingLifecycleService() *paneBindingLifecycleService {
	if m == nil {
		return nil
	}
	return &paneBindingLifecycleService{model: m}
}

func (s *paneBindingLifecycleService) close(paneID string) (paneBindingTarget, error) {
	target, err := s.currentPaneTarget(paneID)
	if err != nil || target.TabID == "" || target.PaneID == "" {
		return paneBindingTarget{}, err
	}
	if _, err := s.model.workbench.ClosePane(target.TabID, target.PaneID); err != nil {
		return paneBindingTarget{}, err
	}
	s.runtimeUnbind(target)
	if current := s.model.workbench.CurrentTab(); current != nil && current.ID == target.TabID && current.ActivePaneID != "" {
		_ = s.model.workbench.FocusPane(target.TabID, current.ActivePaneID)
	}
	return target, nil
}

func (s *paneBindingLifecycleService) detach(paneID string) (paneBindingTarget, error) {
	target, err := s.currentPaneTarget(paneID)
	if err != nil || target.TabID == "" || target.PaneID == "" {
		return paneBindingTarget{}, err
	}
	if err := s.detachBinding(target); err != nil {
		return paneBindingTarget{}, err
	}
	return target, nil
}

func (s *paneBindingLifecycleService) reconnect(paneID string) (reconnectPaneResult, error) {
	target, err := s.currentPaneTarget(paneID)
	if err != nil || target.TabID == "" || target.PaneID == "" {
		return reconnectPaneResult{}, err
	}
	if s.shouldKeepReconnectBinding(target.TerminalID) {
		return reconnectPaneResult{Target: target, KeptExitedTerminal: true}, nil
	}
	if err := s.detachBinding(target); err != nil {
		return reconnectPaneResult{}, err
	}
	return reconnectPaneResult{Target: target}, nil
}

func (s *paneBindingLifecycleService) bindDetachedTerminal(req bindDetachedTerminalRequest) (bindDetachedTerminalResult, error) {
	target, err := s.resolveTarget(req.TabID, req.PaneID)
	if err != nil {
		return bindDetachedTerminalResult{}, err
	}
	if target.TabID == "" || target.PaneID == "" || strings.TrimSpace(req.TerminalID) == "" {
		return bindDetachedTerminalResult{}, nil
	}
	name := strings.TrimSpace(req.Binding.Name)
	req.Binding.Name = name
	if err := s.model.workbench.BindPaneTerminal(target.TabID, target.PaneID, req.TerminalID); err != nil {
		return bindDetachedTerminalResult{}, err
	}
	_ = s.model.workbench.FocusPane(target.TabID, target.PaneID)
	if name != "" {
		s.model.workbench.SetPaneTitleByTerminalID(req.TerminalID, name)
	}
	if s.model.runtime != nil {
		s.model.runtime.UnbindPane(target.PaneID, target.TerminalID)
		s.model.runtime.BindDetachedTerminal(target.PaneID, req.TerminalID, req.Binding)
	}
	return bindDetachedTerminalResult{
		Target: paneBindingTarget{
			TabID:      target.TabID,
			PaneID:     target.PaneID,
			TerminalID: req.TerminalID,
		},
		LoadSnapshotAfter: s.model.runtime != nil && req.Binding.State == "exited",
	}, nil
}

func (s *paneBindingLifecycleService) detachBinding(target paneBindingTarget) error {
	if s == nil || s.model == nil || s.model.workbench == nil {
		return teaErr("pane lifecycle: workbench unavailable")
	}
	if target.TabID == "" || target.PaneID == "" {
		return nil
	}
	if err := s.model.workbench.BindPaneTerminal(target.TabID, target.PaneID, ""); err != nil {
		return err
	}
	s.runtimeUnbind(target)
	return nil
}

func (s *paneBindingLifecycleService) runtimeUnbind(target paneBindingTarget) {
	if s == nil || s.model == nil || s.model.runtime == nil || target.PaneID == "" {
		return
	}
	s.model.runtime.UnbindPane(target.PaneID, target.TerminalID)
}

func (s *paneBindingLifecycleService) shouldKeepReconnectBinding(terminalID string) bool {
	if s == nil || s.model == nil || s.model.runtime == nil || terminalID == "" {
		return false
	}
	terminal := s.model.runtime.Registry().Get(terminalID)
	return terminal != nil && terminal.State == "exited"
}

func (s *paneBindingLifecycleService) currentPaneTarget(paneID string) (paneBindingTarget, error) {
	if s == nil || s.model == nil || s.model.workbench == nil {
		return paneBindingTarget{}, teaErr("pane lifecycle: workbench unavailable")
	}
	tab := s.model.workbench.CurrentTab()
	if tab == nil {
		return paneBindingTarget{}, nil
	}
	targetPaneID := strings.TrimSpace(paneID)
	if targetPaneID == "" {
		targetPaneID = tab.ActivePaneID
	}
	if targetPaneID == "" {
		return paneBindingTarget{}, nil
	}
	pane := tab.Panes[targetPaneID]
	if pane == nil {
		return paneBindingTarget{}, nil
	}
	return paneBindingTarget{
		TabID:      tab.ID,
		PaneID:     targetPaneID,
		TerminalID: pane.TerminalID,
	}, nil
}

func (s *paneBindingLifecycleService) resolveTarget(tabID, paneID string) (paneBindingTarget, error) {
	if s == nil || s.model == nil || s.model.workbench == nil || paneID == "" {
		return paneBindingTarget{}, nil
	}
	workspace := s.model.workbench.CurrentWorkspace()
	if workspace == nil {
		return paneBindingTarget{}, teaErr("select terminal: no current workspace")
	}
	for _, tab := range workspace.Tabs {
		if tab == nil || tab.Panes[paneID] == nil {
			continue
		}
		if tabID != "" && tab.ID != tabID {
			continue
		}
		return paneBindingTarget{
			TabID:      tab.ID,
			PaneID:     paneID,
			TerminalID: tab.Panes[paneID].TerminalID,
		}, nil
	}
	if tabID != "" {
		return paneBindingTarget{}, teaErr("select terminal: pane " + paneID + " not found in tab " + tabID)
	}
	return paneBindingTarget{}, teaErr("select terminal: pane " + paneID + " not found")
}
