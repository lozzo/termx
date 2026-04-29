package app

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/termx-core/protocol"
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
		switch typed.Event.Type {
		case protocol.EventSessionDeleted:
			if typed.Event.SessionID == m.sessionID {
				return m.showError(fmt.Errorf("session %s was deleted", m.sessionID)), true
			}
		case protocol.EventSessionCreated, protocol.EventSessionUpdated:
			if typed.Event.SessionID == m.sessionID {
				revision := uint64(0)
				viewID := ""
				if typed.Event.Session != nil {
					revision = typed.Event.Session.Revision
					viewID = typed.Event.Session.ViewID
				}
				if viewID != m.sessionViewID && revision >= m.sessionRevision {
					return m.pullSessionCmd(), true
				}
			}
		}
		return nil, true
	case sessionViewUpdatedMsg:
		if typed.View != nil && typed.View.ViewID != "" {
			m.sessionViewID = typed.View.ViewID
		}
		if typed.Err != nil {
			return m.showError(typed.Err), true
		}
		return nil, true
	default:
		return nil, false
	}
}
