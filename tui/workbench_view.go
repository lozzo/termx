package tui

func (m *Model) visibleWorkbenchState() WorkbenchVisibleState {
	if m == nil {
		return WorkbenchVisibleState{}
	}
	if m.workbench == nil {
		workspace := &m.workspace
		state := WorkbenchVisibleState{Workspace: workspace}
		if workspace != nil {
			state.Tab = workspace.CurrentTab()
			if state.Tab != nil {
				state.ActivePane = state.Tab.Panes[state.Tab.ActivePaneID]
			}
		}
		return state
	}
	if current := m.workbench.Current(); current != nil {
		*current = *cloneWorkspace(m.workspace)
		m.workbench.SnapshotCurrent()
	}
	return m.workbench.VisibleState()
}
