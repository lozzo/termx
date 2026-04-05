package modal

import "github.com/lozzow/termx/tuiv2/workbench"

type FloatingOverviewState struct {
	Items    []FloatingOverviewItem
	Selected int
}

type FloatingOverviewItem struct {
	PaneID       string
	Title        string
	TerminalID   string
	Display      workbench.FloatingDisplayState
	FitMode      workbench.FloatingFitMode
	Rect         workbench.Rect
	ShortcutSlot int
}

func (s *FloatingOverviewState) Move(delta int) {
	if s == nil || len(s.Items) == 0 || delta == 0 {
		return
	}
	next := s.Selected + delta
	if next < 0 {
		next = 0
	}
	if next >= len(s.Items) {
		next = len(s.Items) - 1
	}
	s.Selected = next
}

func (s *FloatingOverviewState) SelectedItem() *FloatingOverviewItem {
	if s == nil || s.Selected < 0 || s.Selected >= len(s.Items) {
		return nil
	}
	return &s.Items[s.Selected]
}
