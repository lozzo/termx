package app

import (
	"context"
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func (m *Model) loadTerminalManagerItemsCmd() tea.Cmd {
	if m == nil || m.runtime == nil {
		return nil
	}
	return func() tea.Msg {
		terminals, err := m.runtime.ListTerminals(context.Background())
		if err != nil {
			return err
		}
		return terminalManagerItemsLoadedMsg{Items: m.buildTerminalManagerItems(terminals)}
	}
}

func (m *Model) buildTerminalManagerItems(terminals []protocol.TerminalInfo) []modal.PickerItem {
	if len(terminals) == 0 {
		return nil
	}
	bindings := map[string][]workbench.TerminalBindingLocation(nil)
	if m != nil && m.workbench != nil {
		bindings = m.workbench.TerminalBindings()
	}
	items := make([]modal.PickerItem, 0, len(terminals))
	for _, terminal := range terminals {
		locations := bindings[terminal.ID]
		visibleCount := 0
		for _, location := range locations {
			if location.Visible {
				visibleCount++
			}
		}
		group := "parked"
		switch {
		case terminal.State == "exited":
			group = "exited"
		case visibleCount > 0:
			group = "visible"
		}
		items = append(items, modal.PickerItem{
			TerminalID:  terminal.ID,
			Name:        terminal.Name,
			State:       group,
			Command:     strings.Join(terminal.Command, " "),
			CommandArgs: append([]string(nil), terminal.Command...),
			Tags:        cloneStringMap(terminal.Tags),
			Location:    formatTerminalManagerLocation(locations),
			Observed:    visibleCount > 0,
			Orphan:      len(locations) == 0,
			Description: formatTerminalManagerDescription(terminal, len(locations)),
			CreatedAt:   terminal.CreatedAt,
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		leftGroup := terminalManagerGroupOrder(items[i].State)
		rightGroup := terminalManagerGroupOrder(items[j].State)
		if leftGroup != rightGroup {
			return leftGroup < rightGroup
		}
		leftName := strings.TrimSpace(items[i].Name)
		rightName := strings.TrimSpace(items[j].Name)
		if leftName == rightName {
			return items[i].TerminalID < items[j].TerminalID
		}
		if leftName == "" {
			return false
		}
		if rightName == "" {
			return true
		}
		return leftName < rightName
	})
	return items
}

func terminalManagerGroupOrder(state string) int {
	switch state {
	case "visible":
		return 0
	case "parked":
		return 1
	case "exited":
		return 2
	default:
		return 3
	}
}

func formatTerminalManagerLocation(locations []workbench.TerminalBindingLocation) string {
	if len(locations) == 0 {
		return "unbound"
	}
	index := 0
	for i, location := range locations {
		if location.Visible {
			index = i
			break
		}
	}
	location := locations[index]
	label := location.WorkspaceName + "/" + location.TabName + "/" + location.PaneID
	if len(locations) == 1 {
		return label
	}
	return label + " +" + strconv.Itoa(len(locations)-1)
}

func formatTerminalManagerDescription(terminal protocol.TerminalInfo, boundCount int) string {
	state := strings.TrimSpace(terminal.State)
	if state == "" {
		state = "unknown"
	}
	if terminal.ExitCode != nil {
		state += " (" + strconv.Itoa(*terminal.ExitCode) + ")"
	}
	paneWord := "panes"
	if boundCount == 1 {
		paneWord = "pane"
	}
	return state + " · " + strconv.Itoa(boundCount) + " " + paneWord + " bound"
}
