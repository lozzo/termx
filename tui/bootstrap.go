package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) Init() tea.Cmd {
	if m.cfg.AttachID != "" {
		m.logger.Info("tui init attach bootstrap", "terminal_id", m.cfg.AttachID)
		return m.attachInitialTerminalCmd(0, m.cfg.AttachID)
	}
	if strings.TrimSpace(m.cfg.StartupLayout) != "" {
		m.logger.Info("tui init startup layout", "layout", m.cfg.StartupLayout)
		return m.loadLayoutCmd(m.cfg.StartupLayout, LayoutResolveCreate)
	}
	if strings.TrimSpace(m.cfg.WorkspaceStatePath) != "" || m.cfg.StartupAutoLayout {
		return m.startStartupBootstrapCmd()
	}
	if m.cfg.StartupPicker {
		m.logger.Info("tui init startup chooser")
		return m.openBootstrapTerminalPickerCmd(0)
	}
	m.logger.Info("tui init create first pane")
	return m.createPaneCmd(0, "", "")
}

func (m *Model) startStartupBootstrapCmd() tea.Cmd {
	return func() tea.Msg {
		if path := strings.TrimSpace(m.cfg.WorkspaceStatePath); path != "" {
			if exists, err := fileExists(path); err != nil {
				return errMsg{err}
			} else if exists {
				m.logger.Info("tui startup restoring workspace state", "path", path)
				if cmd := m.loadWorkspaceStateCmd(path); cmd != nil {
					msg := cmd()
					if failed, ok := msg.(errMsg); ok {
						m.logger.Warn("tui startup ignoring workspace state restore failure", "path", path, "error", failed.err)
					} else if loaded, ok := msg.(workspaceStateLoadedMsg); ok {
						if !workspaceHasPanes(&loaded.workspace) {
							m.logger.Info("tui startup restored empty workspace state; bootstrapping chooser", "path", path, "workspace", loaded.workspace.Name)
							loaded.bootstrap = true
							loaded.notice = "restored empty workspace state"
							return loaded
						}
						return msg
					}
				}
			}
		}

		if m.cfg.StartupAutoLayout {
			path, err := m.resolveAutoStartupLayoutPath()
			if err != nil {
				return errMsg{err}
			}
			if path != "" {
				m.logger.Info("tui startup auto layout discovered", "path", path)
				if cmd := m.loadLayoutCmd(path, LayoutResolveCreate); cmd != nil {
					return cmd()
				}
			}
		}

		if m.cfg.StartupPicker {
			m.logger.Info("tui startup falling back to chooser")
			if cmd := m.openBootstrapTerminalPickerCmd(0); cmd != nil {
				return cmd()
			}
			return nil
		}

		m.logger.Info("tui startup falling back to first pane creation")
		if cmd := m.createPaneCmd(0, "", ""); cmd != nil {
			return cmd()
		}
		return nil
	}
}

func (m *Model) startEmptyWorkspaceBootstrapCmd() tea.Cmd {
	m.logger.Info("tui empty workspace bootstrapping chooser", "workspace", m.workspace.Name)
	return m.openBootstrapTerminalPickerCmd(m.workspace.ActiveTab)
}
