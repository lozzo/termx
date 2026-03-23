package terminalpicker

import (
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/lozzow/termx/tui/domain/types"
)

type RowKind string

const (
	RowKindCreate   RowKind = "create"
	RowKindTerminal RowKind = "terminal"
)

type State struct {
	rows          []Row
	query         string
	selectedIndex int
}

type Row struct {
	Kind       RowKind
	TerminalID types.TerminalID
	Label      string
	State      types.TerminalRunState
	Command    string
	Visible    bool
	Tags       map[string]string
	SearchText string
}

func NewState(domain types.DomainState, focus types.FocusState) *State {
	state := &State{
		rows: buildRows(domain),
	}
	state.selectedIndex = state.defaultSelectionIndex(domain, focus)
	return state
}

func (s *State) OverlayKind() types.OverlayKind {
	return types.OverlayTerminalPicker
}

func (s *State) CloneOverlayData() types.OverlayData {
	if s == nil {
		return nil
	}
	return &State{
		rows:          append([]Row(nil), s.rows...),
		query:         s.query,
		selectedIndex: s.selectedIndex,
	}
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

func (s *State) VisibleRows() []Row {
	if s.query == "" {
		return append([]Row(nil), s.rows...)
	}
	out := make([]Row, 0, len(s.rows))
	for _, row := range s.rows {
		if row.Kind == RowKindCreate || strings.Contains(row.SearchText, s.query) {
			out = append(out, row)
		}
	}
	return out
}

func (s *State) SelectedRow() (Row, bool) {
	rows := s.VisibleRows()
	if len(rows) == 0 {
		return Row{}, false
	}
	index := s.selectedIndex
	if index < 0 {
		index = 0
	}
	if index >= len(rows) {
		index = len(rows) - 1
	}
	return rows[index], true
}

func (s *State) SelectedTerminalID() (types.TerminalID, bool) {
	row, ok := s.SelectedRow()
	if !ok || row.Kind != RowKindTerminal {
		return "", false
	}
	return row.TerminalID, true
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

func (s *State) resetSelectionForQuery() {
	rows := s.VisibleRows()
	if len(rows) == 0 {
		s.selectedIndex = 0
		return
	}
	for index, row := range rows {
		if row.Kind == RowKindTerminal && row.SearchText != "" && strings.Contains(row.SearchText, s.query) {
			s.selectedIndex = index
			return
		}
	}
	s.selectedIndex = 0
}

func (s *State) clampSelection() {
	rows := s.VisibleRows()
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

func buildRows(domain types.DomainState) []Row {
	rows := []Row{{Kind: RowKindCreate, Label: "+ new terminal"}}
	terminalIDs := make([]types.TerminalID, 0, len(domain.Terminals))
	for terminalID := range domain.Terminals {
		terminalIDs = append(terminalIDs, terminalID)
	}
	slices.Sort(terminalIDs)
	for _, terminalID := range terminalIDs {
		terminal := domain.Terminals[terminalID]
		label := terminal.Name
		if label == "" {
			label = string(terminalID)
		}
		rows = append(rows, Row{
			Kind:       RowKindTerminal,
			TerminalID: terminalID,
			Label:      label,
			State:      terminal.State,
			Command:    strings.Join(terminal.Command, " "),
			Visible:    terminal.Visible,
			Tags:       mapsClone(terminal.Tags),
			SearchText: buildSearchText(domain, terminalID, terminal),
		})
	}
	return rows
}

func mapsClone(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func buildSearchText(domain types.DomainState, terminalID types.TerminalID, terminal types.TerminalRef) string {
	parts := []string{
		strings.ToLower(string(terminalID)),
		strings.ToLower(terminal.Name),
		strings.ToLower(strings.Join(terminal.Command, " ")),
	}
	for key, value := range terminal.Tags {
		parts = append(parts, strings.ToLower(key), strings.ToLower(value))
	}
	parts = append(parts, terminalLocations(domain, terminalID)...)
	return strings.Join(parts, " ")
}

func terminalLocations(domain types.DomainState, terminalID types.TerminalID) []string {
	parts := make([]string, 0)
	for _, workspace := range domain.Workspaces {
		for _, tab := range workspace.Tabs {
			for _, pane := range tab.Panes {
				if pane.TerminalID != terminalID {
					continue
				}
				parts = append(parts, strings.ToLower(workspace.Name), strings.ToLower(tab.Name), strings.ToLower(string(pane.ID)))
			}
		}
	}
	return parts
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
