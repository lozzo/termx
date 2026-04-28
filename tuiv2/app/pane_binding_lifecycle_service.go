package app

import "github.com/lozzow/termx/tuiv2/sessionbind"

type paneBindingLifecycleService struct {
	model *Model
}

type paneBindingTarget = sessionbind.Target
type reconnectPaneResult = sessionbind.ReconnectResult
type bindDetachedTerminalRequest = sessionbind.BindDetachedTerminalRequest
type bindDetachedTerminalResult = sessionbind.BindDetachedTerminalResult

func (m *Model) paneBindingLifecycleService() *paneBindingLifecycleService {
	if m == nil {
		return nil
	}
	return &paneBindingLifecycleService{model: m}
}

func (s *paneBindingLifecycleService) close(paneID string) (paneBindingTarget, error) {
	return s.manager().Close(paneID)
}

func (s *paneBindingLifecycleService) detach(paneID string) (paneBindingTarget, error) {
	return s.manager().Detach(paneID)
}

func (s *paneBindingLifecycleService) reconnect(paneID string) (reconnectPaneResult, error) {
	return s.manager().Reconnect(paneID)
}

func (s *paneBindingLifecycleService) bindDetachedTerminal(req bindDetachedTerminalRequest) (bindDetachedTerminalResult, error) {
	return s.manager().BindDetachedTerminal(req)
}

func (s *paneBindingLifecycleService) manager() *sessionbind.Manager {
	if s == nil || s.model == nil {
		return sessionbind.NewManager(nil, nil)
	}
	return sessionbind.NewManager(s.model.workbench, s.model.runtime)
}
