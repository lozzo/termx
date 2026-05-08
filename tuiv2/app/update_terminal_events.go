package app

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/termx-core/protocol"
	"github.com/lozzow/termx/tuiv2/modal"
)

func (m *Model) handleTerminalEventMessage(msg tea.Msg) (tea.Cmd, bool) {
	switch typed := msg.(type) {
	case terminalEventMsg:
		switch typed.Event.Type {
		case protocol.EventTerminalCreated, protocol.EventTerminalStateChanged, protocol.EventTerminalMetadataChanged:
			return m.refreshTerminalInventoryCmd(), true
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
		case protocol.EventTerminalRemoved:
			if m == nil || typed.Event.TerminalID == "" {
				return nil, true
			}
			m.removeTerminalFromLocalState(typed.Event.TerminalID)
			return nil, true
		default:
			return nil, true
		}
	default:
		return nil, false
	}
}

func (m *Model) refreshTerminalInventoryCmd() tea.Cmd {
	if m == nil || m.runtime == nil {
		return nil
	}
	return func() tea.Msg {
		terminals, err := m.runtime.ListTerminals(context.Background())
		if err != nil {
			m.debugLog("terminal_inventory_refresh_failed", "err", err)
			return nil
		}
		return terminalInventoryLoadedMsg{Terminals: terminals}
	}
}

func (m *Model) applyTerminalInventoryPatch(terminals []protocol.TerminalInfo) {
	if m == nil {
		return
	}
	if m.runtime != nil {
		m.runtime.ApplyTerminalList(terminals)
	}
	if m.workbench != nil {
		for _, terminal := range terminals {
			if strings.TrimSpace(terminal.Name) == "" {
				continue
			}
			m.workbench.SetPaneTitleByTerminalID(terminal.ID, terminal.Name)
		}
	}
	m.patchTerminalManagerItems(terminals)
	m.patchPickerItems(terminals)
	if m.render != nil {
		m.render.Invalidate()
	}
}

func (m *Model) buildTerminalPickerItems(terminals []protocol.TerminalInfo) []modal.PickerItem {
	items := make([]modal.PickerItem, 0, len(terminals)+1)
	for _, terminal := range terminals {
		items = append(items, modal.PickerItem{
			TerminalID:    terminal.ID,
			Name:          terminal.Name,
			State:         terminal.State,
			TerminalState: terminal.State,
			ExitCode:      cloneIntPointer(terminal.ExitCode),
			Command:       strings.Join(terminal.Command, " "),
			CommandArgs:   append([]string(nil), terminal.Command...),
			Tags:          cloneStringMap(terminal.Tags),
			CreatedAt:     terminal.CreatedAt,
		})
	}
	items = append(items, modal.PickerItem{
		CreateNew:   true,
		Name:        "new terminal",
		Description: "Create a new terminal",
	})
	return items
}

func (m *Model) patchTerminalManagerItems(terminals []protocol.TerminalInfo) {
	if m == nil || m.terminalPage == nil {
		return
	}
	selectedID := selectedPickerItemTerminalID(m.terminalPage.SelectedItem())
	m.terminalPage.Items = m.buildTerminalManagerItems(terminals)
	m.terminalPage.ApplyFilter()
	selectVisibleTerminalManagerItem(m.terminalPage, selectedID)
}

func (m *Model) patchPickerItems(terminals []protocol.TerminalInfo) {
	if m == nil || m.modalHost == nil || m.modalHost.Picker == nil {
		return
	}
	selectedID := selectedPickerItemTerminalID(m.modalHost.Picker.SelectedItem())
	m.modalHost.Picker.Items = m.buildTerminalPickerItems(terminals)
	m.modalHost.Picker.ApplyFilter()
	selectVisiblePickerItem(m.modalHost.Picker, selectedID)
}

func selectedPickerItemTerminalID(item *modal.PickerItem) string {
	if item == nil || item.CreateNew {
		return ""
	}
	return item.TerminalID
}

func selectVisibleTerminalManagerItem(state *modal.TerminalManagerState, terminalID string) {
	if state == nil {
		return
	}
	if index := terminalManagerVisibleIndexByTerminalID(state.VisibleItems(), terminalID); index >= 0 {
		state.Selected = index
		return
	}
	normalizeModalSelection(&state.Selected, len(state.VisibleItems()))
}

func selectVisiblePickerItem(state *modal.PickerState, terminalID string) {
	if state == nil {
		return
	}
	if index := pickerVisibleIndexByTerminalID(state.VisibleItems(), terminalID); index >= 0 {
		state.Selected = index
		return
	}
	normalizeModalSelection(&state.Selected, len(state.VisibleItems()))
}

func pickerVisibleIndexByTerminalID(items []modal.PickerItem, terminalID string) int {
	if terminalID == "" {
		return -1
	}
	for index, item := range items {
		if item.TerminalID == terminalID {
			return index
		}
	}
	return -1
}
