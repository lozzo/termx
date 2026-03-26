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

func (s *State) SelectNext() {
	if s == nil {
		return
	}
	items := s.allItems()
	if len(items) == 0 {
		s.SelectedTerminalID = ""
		return
	}
	index := s.selectedIndex(items)
	if index == -1 || index == len(items)-1 {
		s.SelectedTerminalID = items[0].ID
		return
	}
	s.SelectedTerminalID = items[index+1].ID
}

func (s *State) SelectPrev() {
	if s == nil {
		return
	}
	items := s.allItems()
	if len(items) == 0 {
		s.SelectedTerminalID = ""
		return
	}
	index := s.selectedIndex(items)
	if index <= 0 {
		s.SelectedTerminalID = items[len(items)-1].ID
		return
	}
	s.SelectedTerminalID = items[index-1].ID
}

func (s State) hasSelection() bool {
	selected := s.SelectedTerminalID
	if selected == "" {
		return false
	}
	for _, item := range s.allItems() {
		if item.ID == selected {
			return true
		}
	}
	return false
}

func (s State) allItems() []Item {
	return append(append(append([]Item{}, s.Visible...), s.Parked...), s.Exited...)
}

func (s State) selectedIndex(items []Item) int {
	for index, item := range items {
		if item.ID == s.SelectedTerminalID {
			return index
		}
	}
	return -1
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
