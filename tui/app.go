package tui

import "strings"

type App struct {
	workbench           *Workbench
	terminalCoordinator *TerminalCoordinator
	resizer             *Resizer
	renderLoop          *RenderLoop
}

func NewApp(workbench *Workbench, terminalCoordinator *TerminalCoordinator, resizer *Resizer, renderLoop *RenderLoop) *App {
	return &App{workbench: workbench, terminalCoordinator: terminalCoordinator, resizer: resizer, renderLoop: renderLoop}
}

func (a *App) RenderLoop() *RenderLoop {
	if a == nil {
		return nil
	}
	return a.renderLoop
}

func (a *App) Renderer() *Renderer {
	if a == nil || a.renderLoop == nil {
		return nil
	}
	return a.renderLoop.Renderer()
}

func (a *App) Resizer() *Resizer {
	if a == nil {
		return nil
	}
	return a.resizer
}

func (a *App) TerminalCoordinator() *TerminalCoordinator {
	if a == nil {
		return nil
	}
	return a.terminalCoordinator
}

func (a *App) Workbench() *Workbench {
	if a == nil {
		return nil
	}
	return a.workbench
}

func (a *App) ActivateTab(index int) bool {
	if a == nil || a.workbench == nil {
		return false
	}
	return a.workbench.ActivateTab(index)
}

func (a *App) FocusPane(paneID string) bool {
	if a == nil || a.workbench == nil {
		return false
	}
	return a.workbench.FocusPane(paneID)
}

func (a *App) SyncCurrentWorkspace(workspace Workspace) {
	if a == nil || a.workbench == nil {
		return
	}
	current := a.workbench.Current()
	if current == nil {
		return
	}
	*current = workspace
	a.workbench.SnapshotCurrent()
}

func (a *App) TerminalPickerContextForWorkspace(workspace Workspace) (terminalPickerAction, bool) {
	if a == nil {
		return terminalPickerAction{}, false
	}
	a.SyncCurrentWorkspace(workspace)
	return a.TerminalPickerContext()
}

func (a *App) TerminalPickerContext() (terminalPickerAction, bool) {
	if a == nil || a.workbench == nil {
		return terminalPickerAction{}, false
	}
	action := terminalPickerAction{Kind: terminalPickerActionReplace}
	workspace := a.workbench.CurrentWorkspace()
	if workspace != nil {
		action.TabIndex = workspace.ActiveTab
	}
	pane := activePane(a.workbench.CurrentTab())
	if pane == nil {
		action.Kind = terminalPickerActionBootstrap
		return action, true
	}
	if pane.Viewport == nil || pane.TerminalID == "" {
		return action, true
	}
	return action, false
}

func (a *App) HandleWorkspaceActivated(workspace Workspace, index int) (notice string, bootstrap bool) {
	if a == nil || a.workbench == nil {
		return "", true
	}
	order := a.workbench.Order()
	targetName := strings.TrimSpace(workspace.Name)
	if targetName == "" && index >= 0 && index < len(order) {
		targetName = order[index]
	}
	if targetName != "" {
		if idx := index; idx >= 0 && idx < len(order) {
			_ = a.workbench.SwitchTo(order[idx])
		} else {
			_ = a.workbench.SwitchTo(targetName)
		}
	}
	current := a.workbench.Current()
	if current == nil {
		return "", true
	}
	*current = workspace
	a.workbench.SnapshotCurrent()
	if targetName != "" {
		_ = a.workbench.SwitchTo(targetName)
	}
	return "", true
}
