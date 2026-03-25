package layoutresolve

import "github.com/lozzow/termx/tui/domain/types"

type Action string

const (
	ActionConnectExisting Action = "connect_existing"
	ActionCreateNew       Action = "create_new"
	ActionSkip            Action = "skip"
)

type Row struct {
	Action Action
	Label  string
}

// State 描述 waiting pane 的最小 resolve 选择面。
// 第一轮只收口三种显式动作，后续再按需要补匹配策略和更多上下文。
type State struct {
	PaneID        types.PaneID
	Role          string
	Hint          string
	rows          []Row
	selectedIndex int
}

func NewState(paneID types.PaneID, role string, hint string) *State {
	return &State{
		PaneID: paneID,
		Role:   role,
		Hint:   hint,
		rows: []Row{
			{Action: ActionConnectExisting, Label: "connect existing"},
			{Action: ActionCreateNew, Label: "create new"},
			{Action: ActionSkip, Label: "skip"},
		},
	}
}

func (s *State) OverlayKind() types.OverlayKind {
	return types.OverlayLayoutResolve
}

func (s *State) CloneOverlayData() types.OverlayData {
	if s == nil {
		return nil
	}
	return &State{
		PaneID:        s.PaneID,
		Role:          s.Role,
		Hint:          s.Hint,
		rows:          append([]Row(nil), s.rows...),
		selectedIndex: s.selectedIndex,
	}
}

func (s *State) Rows() []Row {
	return append([]Row(nil), s.rows...)
}

func (s *State) MoveSelection(delta int) {
	s.selectedIndex += delta
	s.clampSelection()
}

func (s *State) SelectedRow() (Row, bool) {
	if len(s.rows) == 0 {
		return Row{}, false
	}
	index := s.selectedIndex
	if index < 0 {
		index = 0
	}
	if index >= len(s.rows) {
		index = len(s.rows) - 1
	}
	return s.rows[index], true
}

func (s *State) clampSelection() {
	if len(s.rows) == 0 {
		s.selectedIndex = 0
		return
	}
	if s.selectedIndex < 0 {
		s.selectedIndex = 0
		return
	}
	if s.selectedIndex >= len(s.rows) {
		s.selectedIndex = len(s.rows) - 1
	}
}
