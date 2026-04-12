package app

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/protocol"
)

func (m *Model) handleTerminalEventMessage(msg tea.Msg) (tea.Cmd, bool) {
	switch typed := msg.(type) {
	case terminalEventMsg:
		switch typed.Event.Type {
		case protocol.EventTerminalResized:
			if m == nil || m.runtime == nil || typed.Event.TerminalID == "" {
				return nil, true
			}
			terminal := m.runtime.Registry().Get(typed.Event.TerminalID)
			if terminal == nil {
				return nil, true
			}
			// When a stream is active, the in-band resize frame already
			// updated the local VTerm dimensions. Reloading the snapshot
			// here would race with the stream and can punch holes in the
			// cell grid.
			if terminal.Stream.Active {
				return nil, true
			}
			return m.reloadTerminalSnapshotCmd(typed.Event.TerminalID), true
		default:
			return nil, true
		}
	default:
		return nil, false
	}
}
