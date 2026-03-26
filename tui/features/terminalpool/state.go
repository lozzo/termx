package terminalpool

import (
	corepool "github.com/lozzow/termx/tui/core/pool"
	coreterminal "github.com/lozzow/termx/tui/core/terminal"
	"github.com/lozzow/termx/tui/core/types"
)

type Item struct {
	ID    types.TerminalID
	Name  string
	State coreterminal.State
}

type State struct {
	Query              string
	SelectedTerminalID types.TerminalID
	Visible            []Item
	Parked             []Item
	Exited             []Item
}

func (s *State) ApplyGroups(groups corepool.Groups) {
	if s == nil {
		return
	}
	s.Visible = buildItems(groups.Visible)
	s.Parked = buildItems(groups.Parked)
	s.Exited = buildItems(groups.Exited)
	if !s.hasSelection() {
		s.SelectedTerminalID = firstItemID(s.Visible, s.Parked, s.Exited)
	}
}

func (s State) hasSelection() bool {
	selected := s.SelectedTerminalID
	if selected == "" {
		return false
	}
	for _, item := range append(append(append([]Item{}, s.Visible...), s.Parked...), s.Exited...) {
		if item.ID == selected {
			return true
		}
	}
	return false
}

func buildItems(items []coreterminal.Metadata) []Item {
	out := make([]Item, 0, len(items))
	for _, item := range items {
		out = append(out, Item{ID: item.ID, Name: item.Name, State: item.State})
	}
	return out
}

func firstItemID(groups ...[]Item) types.TerminalID {
	for _, group := range groups {
		if len(group) > 0 {
			return group[0].ID
		}
	}
	return ""
}
