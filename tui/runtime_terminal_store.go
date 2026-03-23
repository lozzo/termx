package tui

import "github.com/lozzow/termx/tui/domain/types"

type RuntimeTerminalStore interface {
	Session(terminalID types.TerminalID) (TerminalRuntimeSession, bool)
}

type runtimeTerminalStore struct {
	sessions map[types.TerminalID]TerminalRuntimeSession
}

func NewRuntimeTerminalStore(sessions RuntimeSessions) RuntimeTerminalStore {
	store := runtimeTerminalStore{
		sessions: make(map[types.TerminalID]TerminalRuntimeSession, len(sessions.Terminals)),
	}
	for terminalID, session := range sessions.Terminals {
		store.sessions[terminalID] = session
	}
	return store
}

func (s runtimeTerminalStore) Session(terminalID types.TerminalID) (TerminalRuntimeSession, bool) {
	session, ok := s.sessions[terminalID]
	return session, ok
}

func activePane(state types.AppState) (types.PaneState, bool) {
	workspace, ok := state.Domain.Workspaces[state.Domain.ActiveWorkspaceID]
	if !ok {
		return types.PaneState{}, false
	}
	tab, ok := workspace.Tabs[workspace.ActiveTabID]
	if !ok {
		return types.PaneState{}, false
	}
	pane, ok := tab.Panes[tab.ActivePaneID]
	if !ok {
		return types.PaneState{}, false
	}
	return pane, true
}

func activeTerminalSession(state types.AppState, store RuntimeTerminalStore) (TerminalRuntimeSession, bool) {
	if store == nil {
		return TerminalRuntimeSession{}, false
	}
	pane, ok := activePane(state)
	if !ok || pane.TerminalID == "" {
		return TerminalRuntimeSession{}, false
	}
	return store.Session(pane.TerminalID)
}
