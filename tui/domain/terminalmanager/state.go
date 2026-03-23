package terminalmanager

import (
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/lozzow/termx/tui/domain/types"
)

type RowKind string

const (
	RowKindHeader   RowKind = "header"
	RowKindCreate   RowKind = "create"
	RowKindTerminal RowKind = "terminal"
)

type Section string

const (
	SectionNew     Section = "NEW"
	SectionVisible Section = "VISIBLE"
	SectionParked  Section = "PARKED"
	SectionExited  Section = "EXITED"
)

// State 是 terminal manager overlay 的纯状态对象。
// 这里维护“可选条目”和“可渲染投影”两套视图，避免 UI 选择逻辑和 header 行耦在一起。
type State struct {
	rows          []Row
	query         string
	selectedIndex int
}

type Row struct {
	Kind               RowKind
	Section            Section
	TerminalID         types.TerminalID
	Label              string
	State              types.TerminalRunState
	Visible            bool
	SearchText         string
	Command            string
	ConnectedPaneCount int
}

type Detail struct {
	TerminalID         types.TerminalID
	Name               string
	State              types.TerminalRunState
	Visible            bool
	Command            string
	ConnectedPaneCount int
}

func NewState(domain types.DomainState, focus types.FocusState) *State {
	rows := buildRows(domain)
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
	terminalRows := s.visibleTerminalRows()
	out := []Row{
		{Kind: RowKindHeader, Section: SectionNew, Label: string(SectionNew)},
		{Kind: RowKindCreate, Section: SectionNew, Label: "+ new terminal"},
	}
	appendSection := func(section Section) {
		sectionRows := rowsInSection(terminalRows, section)
		if len(sectionRows) == 0 {
			return
		}
		out = append(out, Row{Kind: RowKindHeader, Section: section, Label: string(section)})
		out = append(out, sectionRows...)
	}
	appendSection(SectionVisible)
	appendSection(SectionParked)
	appendSection(SectionExited)
	return out
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

func (s *State) SelectedRow() (Row, bool) {
	rows := s.selectableRows()
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

func (s *State) SelectedDetail() (Detail, bool) {
	row, ok := s.SelectedRow()
	if !ok || row.Kind != RowKindTerminal {
		return Detail{}, false
	}
	return Detail{
		TerminalID:         row.TerminalID,
		Name:               row.Label,
		State:              row.State,
		Visible:            row.Visible,
		Command:            row.Command,
		ConnectedPaneCount: row.ConnectedPaneCount,
	}, true
}

func (s *State) clampSelection() {
	rows := s.selectableRows()
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
		return 1
	}
	rows := s.selectableRows()
	for index, row := range rows {
		if row.TerminalID == terminalID {
			return index
		}
	}
	return 1
}

func buildRows(domain types.DomainState) []Row {
	rows := make([]Row, 0, len(domain.Terminals))
	for terminalID, terminal := range domain.Terminals {
		label := terminal.Name
		if label == "" {
			label = string(terminalID)
		}
		connectedPaneCount := len(domain.Connections[terminalID].ConnectedPaneIDs)
		rows = append(rows, Row{
			Kind:               RowKindTerminal,
			Section:            classifySection(terminal, connectedPaneCount),
			TerminalID:         terminalID,
			Label:              label,
			State:              terminal.State,
			Visible:            terminal.Visible,
			SearchText:         searchText(terminalID, terminal),
			Command:            strings.Join(terminal.Command, " "),
			ConnectedPaneCount: connectedPaneCount,
		})
	}
	slices.SortFunc(rows, func(a, b Row) int {
		if a.Section != b.Section {
			return cmpSection(a.Section, b.Section)
		}
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

func (s *State) visibleTerminalRows() []Row {
	rows := s.rows
	if s.query == "" {
		return rows
	}
	filtered := make([]Row, 0, len(rows))
	for _, row := range rows {
		if strings.Contains(row.SearchText, s.query) {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

func (s *State) selectableRows() []Row {
	rows := make([]Row, 0, len(s.visibleTerminalRows())+1)
	rows = append(rows, Row{Kind: RowKindCreate, Section: SectionNew, Label: "+ new terminal"})
	rows = append(rows, s.visibleTerminalRows()...)
	return rows
}

func (s *State) resetSelectionForQuery() {
	if s.query == "" {
		s.clampSelection()
		return
	}
	rows := s.visibleTerminalRows()
	if len(rows) == 0 {
		s.selectedIndex = 0
		return
	}
	// `0` 保留给 create row，搜索命中后默认跳到第一条 terminal 结果。
	s.selectedIndex = 1
}

func rowsInSection(rows []Row, section Section) []Row {
	out := make([]Row, 0, len(rows))
	for _, row := range rows {
		if row.Section == section {
			out = append(out, row)
		}
	}
	return out
}

func classifySection(terminal types.TerminalRef, connectedPaneCount int) Section {
	if terminal.State == types.TerminalRunStateExited {
		return SectionExited
	}
	if terminal.Visible || connectedPaneCount > 0 {
		return SectionVisible
	}
	return SectionParked
}

func cmpSection(a, b Section) int {
	order := map[Section]int{
		SectionVisible: 1,
		SectionParked:  2,
		SectionExited:  3,
	}
	return order[a] - order[b]
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
