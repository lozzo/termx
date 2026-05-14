package app

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) handleSessionMessage(msg tea.Msg) (tea.Cmd, bool) {
	switch typed := msg.(type) {
	case sessionSnapshotMsg:
		var applyErr error
		if shouldApplySessionSnapshot(typed.Snapshot) {
			applyErr = m.applySessionSnapshot(typed.Snapshot)
		}
		switch {
		case typed.Err != nil && applyErr != nil:
			return batchCmds(m.showError(typed.Err), m.showError(applyErr)), true
		case typed.Err != nil:
			return m.showError(typed.Err), true
		case applyErr != nil:
			return m.showError(applyErr), true
		}
		return nil, true
	case sessionEventMsg:
		if typed.Event.Deleted {
			if typed.Event.SessionID == m.sessionID {
				return m.showError(fmt.Errorf("session %s was deleted", m.sessionID)), true
			}
			return nil, true
		}
		if typed.Event.SessionID == m.sessionID {
			revision := typed.Event.Revision
			viewID := typed.Event.ViewID
			if viewID != m.sessionViewID && revision >= m.sessionRevision {
				return m.pullSessionCmd(), true
			}
		}
		return nil, true
	case sessionViewUpdatedMsg:
		if typed.View != nil && typed.View.ViewID != "" {
			m.sessionViewID = typed.View.ViewID
		}
		if typed.Err != nil {
			if isRevisionConflict(typed.Err) {
				return nil, true
			}
			return m.showError(typed.Err), true
		}
		return nil, true
	default:
		return nil, false
	}
}
