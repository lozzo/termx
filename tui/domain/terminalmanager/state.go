package terminalmanager

import (
	"slices"

	"github.com/lozzow/termx/tui/domain/types"
)

// State 是 terminal manager overlay 的纯状态对象。
// 这里先只承载选择和列表投影，后续再补搜索、分组和详情面板。
type State struct {
	rows          []Row
	selectedIndex int
}

type Row struct {
	TerminalID types.TerminalID
	Label      string
	State      types.TerminalRunState
	Visible    bool
}

func NewState(domain types.DomainState, focus types.FocusState) *State {
	rows := buildRows(domain.Terminals)
	state := &State{rows: rows}
	state.selectedIndex = state.defaultSelectionIndex(domain, focus)
	return state
}

func (s *State) OverlayKind() types.OverlayKind {
	return types.OverlayTerminalManager
}

func (s *State) CloneOverlayData() types.OverlayData {
	if s == nil {
		return nil
	}
	clone := &State{
		rows:          append([]Row(nil), s.rows...),
		selectedIndex: s.selectedIndex,
	}
	return clone
}

func (s *State) VisibleRows() []Row {
	return append([]Row(nil), s.rows...)
}

func (s *State) MoveSelection(delta int) {
	s.selectedIndex += delta
	s.clampSelection()
}

func (s *State) SelectedTerminalID() (types.TerminalID, bool) {
	if len(s.rows) == 0 {
		return "", false
	}
	index := s.selectedIndex
	if index < 0 {
		index = 0
	}
	if index >= len(s.rows) {
		index = len(s.rows) - 1
	}
	return s.rows[index].TerminalID, true
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

func (s *State) defaultSelectionIndex(domain types.DomainState, focus types.FocusState) int {
	terminalID := focusedPaneTerminalID(domain, focus)
	if terminalID == "" {
		return 0
	}
	for index, row := range s.rows {
		if row.TerminalID == terminalID {
			return index
		}
	}
	return 0
}

func buildRows(terminals map[types.TerminalID]types.TerminalRef) []Row {
	rows := make([]Row, 0, len(terminals))
	for terminalID, terminal := range terminals {
		label := terminal.Name
		if label == "" {
			label = string(terminalID)
		}
		rows = append(rows, Row{
			TerminalID: terminalID,
			Label:      label,
			State:      terminal.State,
			Visible:    terminal.Visible,
		})
	}
	slices.SortFunc(rows, func(a, b Row) int {
		if a.Label == b.Label {
			switch {
			case a.TerminalID < b.TerminalID:
				return -1
			case a.TerminalID > b.TerminalID:
				return 1
			default:
				return 0
			}
		}
		if a.Label < b.Label {
			return -1
		}
		return 1
	})
	return rows
}

func focusedPaneTerminalID(domain types.DomainState, focus types.FocusState) types.TerminalID {
	workspace, ok := domain.Workspaces[focus.WorkspaceID]
	if !ok {
		return ""
	}
	tab, ok := workspace.Tabs[focus.TabID]
	if !ok {
		return ""
	}
	pane, ok := tab.Panes[focus.PaneID]
	if !ok {
		return ""
	}
	return pane.TerminalID
}
