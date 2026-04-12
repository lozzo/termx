package app

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) syncTerminalInteractionCmd(req terminalInteractionRequest) tea.Cmd {
	service := m.terminalInteractionService()
	if service == nil {
		return nil
	}
	return service.syncCmd(req)
}

func (m *Model) syncTerminalInteraction(ctx context.Context, req terminalInteractionRequest, target terminalInteractionTarget) error {
	service := m.terminalInteractionService()
	if service == nil {
		return nil
	}
	return service.sync(ctx, req, target)
}

func (m *Model) resolveTerminalInteractionTarget(req terminalInteractionRequest) (terminalInteractionTarget, bool) {
	service := m.terminalInteractionService()
	if service == nil {
		return terminalInteractionTarget{}, false
	}
	return service.resolveTarget(req)
}
