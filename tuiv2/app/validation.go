package app

import (
	"context"
	"strings"

	"github.com/lozzow/termx/protocol"
)

func (m *Model) validateUniqueTerminalName(name, exceptTerminalID string) error {
	normalizedName := strings.TrimSpace(name)
	if normalizedName == "" {
		return nil
	}
	terminals, err := m.terminalValidationSnapshot()
	if err != nil {
		return err
	}
	currentName := ""
	for _, terminal := range terminals {
		if terminal.ID != exceptTerminalID {
			continue
		}
		currentName = strings.TrimSpace(terminal.Name)
		break
	}
	if currentName != "" && currentName == normalizedName {
		return nil
	}
	for _, terminal := range terminals {
		if terminal.ID == exceptTerminalID {
			continue
		}
		if strings.TrimSpace(terminal.Name) == normalizedName {
			return inputError("terminal name already exists")
		}
	}
	return nil
}

func (m *Model) terminalValidationSnapshot() ([]protocol.TerminalInfo, error) {
	if m == nil || m.runtime == nil {
		return nil, nil
	}
	registry := m.runtime.Registry()
	snapshot := make([]protocol.TerminalInfo, 0)
	if registry != nil {
		ids := registry.IDs()
		snapshot = make([]protocol.TerminalInfo, 0, len(ids))
		for _, terminalID := range ids {
			terminal := registry.Get(terminalID)
			if terminal == nil {
				continue
			}
			snapshot = append(snapshot, protocol.TerminalInfo{
				ID:   terminal.TerminalID,
				Name: terminal.Name,
			})
		}
	}
	if m.runtime.Client() == nil {
		return snapshot, nil
	}
	terminals, err := m.runtime.ListTerminals(context.Background())
	if err == nil {
		return terminals, nil
	}
	if len(snapshot) == 0 {
		return nil, err
	}
	return snapshot, nil
}

func (m *Model) validateUniqueWorkspaceTabName(workspaceName, tabID, name string) error {
	if m == nil || m.workbench == nil {
		return nil
	}
	workspace := m.workbench.WorkspaceByName(strings.TrimSpace(workspaceName))
	if workspace == nil {
		return nil
	}
	normalizedName := strings.TrimSpace(name)
	if normalizedName == "" {
		return nil
	}
	currentName := ""
	for _, tab := range workspace.Tabs {
		if tab == nil || tab.ID != tabID {
			continue
		}
		currentName = strings.TrimSpace(tab.Name)
		break
	}
	if currentName != "" && currentName == normalizedName {
		return nil
	}
	for _, tab := range workspace.Tabs {
		if tab == nil || tab.ID == tabID {
			continue
		}
		if strings.TrimSpace(tab.Name) == normalizedName {
			return inputError("tab name already exists in this workspace")
		}
	}
	return nil
}
