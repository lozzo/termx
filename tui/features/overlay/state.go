package overlay

import (
	featureterminalpool "github.com/lozzow/termx/tui/features/terminalpool"
	"github.com/lozzow/termx/tui/core/types"
)

type Kind string

const (
	KindConnectPicker Kind = "connect-picker"
	KindHelp          Kind = "help"
	KindPrompt        Kind = "prompt"
)

type ActiveState struct {
	Kind     Kind
	Title    string
	Selected types.TerminalID
	Items    []featureterminalpool.Item
}

type State struct {
	Active ActiveState
}

func (s State) OpenConnectPicker(items []featureterminalpool.Item, selected types.TerminalID) State {
	s.Active = ActiveState{Kind: KindConnectPicker, Title: "connect", Items: append([]featureterminalpool.Item(nil), items...), Selected: selected}
	return s
}

func (s State) OpenHelp() State {
	s.Active = ActiveState{Kind: KindHelp, Title: "help"}
	return s
}

func (s State) OpenPrompt(title string) State {
	s.Active = ActiveState{Kind: KindPrompt, Title: title}
	return s
}

func (s State) SelectNext() State {
	if s.Active.Kind != KindConnectPicker || len(s.Active.Items) == 0 {
		return s
	}
	index := s.selectedIndex()
	if index == -1 || index == len(s.Active.Items)-1 {
		s.Active.Selected = s.Active.Items[0].ID
		return s
	}
	s.Active.Selected = s.Active.Items[index+1].ID
	return s
}

func (s State) SelectPrev() State {
	if s.Active.Kind != KindConnectPicker || len(s.Active.Items) == 0 {
		return s
	}
	index := s.selectedIndex()
	if index <= 0 {
		s.Active.Selected = s.Active.Items[len(s.Active.Items)-1].ID
		return s
	}
	s.Active.Selected = s.Active.Items[index-1].ID
	return s
}

func (s State) selectedIndex() int {
	for index, item := range s.Active.Items {
		if item.ID == s.Active.Selected {
			return index
		}
	}
	return -1
}

func (s State) Clear() State {
	return State{}
}
