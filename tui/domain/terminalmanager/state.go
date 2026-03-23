package terminalmanager

import (
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/lozzow/termx/tui/domain/types"
)

// State 是 terminal manager overlay 的纯状态对象。
// 这里先只承载选择和列表投影，后续再补搜索、分组和详情面板。
type State struct {
	rows          []Row
	query         string
	selectedIndex int
}

type Row struct {
	TerminalID types.TerminalID
	Label      string
	State      types.TerminalRunState
	Visible    bool
	SearchText string
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
		query:         s.query,
		selectedIndex: s.selectedIndex,
	}
	return clone
}

func (s *State) VisibleRows() []Row {
	rows := s.visibleRows()
	return append([]Row(nil), rows...)
}

func (s *State) Query() string {
	return s.query
}

func (s *State) AppendQuery(text string) {
	if text == "" {
		return
	}
	s.query += strings.TrimSpace(strings.ToLower(text))
	s.resetSelectionForQuery()
}

func (s *State) BackspaceQuery() {
	if s.query == "" {
		return
	}
	_, size := utf8.DecodeLastRuneInString(s.query)
	s.query = s.query[:len(s.query)-size]
	s.resetSelectionForQuery()
}

func (s *State) MoveSelection(delta int) {
	s.selectedIndex += delta
	s.clampSelection()
}

func (s *State) SelectedTerminalID() (types.TerminalID, bool) {
	rows := s.visibleRows()
	if len(rows) == 0 {
		return "", false
	}
	index := s.selectedIndex
	if index < 0 {
		index = 0
	}
	if index >= len(rows) {
		index = len(rows) - 1
	}
	return rows[index].TerminalID, true
}

func (s *State) clampSelection() {
	rows := s.visibleRows()
	if len(rows) == 0 {
		s.selectedIndex = 0
		return
	}
	if s.selectedIndex < 0 {
		s.selectedIndex = 0
		return
	}
	if s.selectedIndex >= len(rows) {
		s.selectedIndex = len(rows) - 1
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
			SearchText: searchText(terminalID, terminal),
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

func (s *State) visibleRows() []Row {
	if s.query == "" {
		return s.rows
	}
	rows := make([]Row, 0, len(s.rows))
	for _, row := range s.rows {
		if strings.Contains(row.SearchText, s.query) {
			rows = append(rows, row)
		}
	}
	return rows
}

func (s *State) resetSelectionForQuery() {
	if s.query == "" {
		s.clampSelection()
		return
	}
	rows := s.visibleRows()
	if len(rows) == 0 {
		s.selectedIndex = 0
		return
	}
	s.selectedIndex = 0
}

func searchText(terminalID types.TerminalID, terminal types.TerminalRef) string {
	parts := []string{strings.ToLower(string(terminalID)), strings.ToLower(terminal.Name)}
	if len(terminal.Command) > 0 {
		parts = append(parts, strings.ToLower(strings.Join(terminal.Command, " ")))
	}
	for key, value := range terminal.Tags {
		parts = append(parts, strings.ToLower(key), strings.ToLower(value))
	}
	return strings.Join(parts, " ")
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
