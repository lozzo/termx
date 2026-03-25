package workspace

import (
	"strings"
	"unicode/utf8"

	"github.com/lozzow/termx/tui/domain/types"
)

type PickerState struct {
	query             string
	rootNodes         []TreeNode
	activeWorkspaceID types.WorkspaceID
	activeTabID       types.TabID
	activePaneID      types.PaneID
	selectedIndex     int
	manuallyExpanded  map[string]bool
	manuallyCollapsed map[string]bool
}

type TreeRow struct {
	Node     TreeNode
	Depth    int
	Expanded bool
	Match    bool
}

func NewPickerState(state types.DomainState) *PickerState {
	activeTabID := types.TabID("")
	activePaneID := types.PaneID("")
	if ws, ok := state.Workspaces[state.ActiveWorkspaceID]; ok {
		activeTabID = ws.ActiveTabID
		if tab, ok := ws.Tabs[activeTabID]; ok {
			activePaneID = tab.ActivePaneID
		}
	}
	picker := &PickerState{
		rootNodes:         BuildPickerTree(state),
		activeWorkspaceID: state.ActiveWorkspaceID,
		activeTabID:       activeTabID,
		activePaneID:      activePaneID,
		manuallyExpanded:  make(map[string]bool),
		manuallyCollapsed: make(map[string]bool),
	}
	picker.selectedIndex = picker.defaultSelectionIndex()
	return picker
}

func (p *PickerState) SetQuery(query string) {
	p.query = strings.TrimSpace(strings.ToLower(query))
	p.resetSelectionForQuery()
}

func (p *PickerState) Query() string {
	return p.query
}

func (p *PickerState) AppendQuery(text string) {
	if text == "" {
		return
	}
	p.query += strings.TrimSpace(strings.ToLower(text))
	p.resetSelectionForQuery()
}

func (p *PickerState) BackspaceQuery() {
	if p.query == "" {
		return
	}
	_, size := utf8.DecodeLastRuneInString(p.query)
	p.query = p.query[:len(p.query)-size]
	p.clampSelection()
}

func (p *PickerState) OverlayKind() types.OverlayKind {
	return types.OverlayWorkspacePicker
}

func (p *PickerState) CloneOverlayData() types.OverlayData {
	if p == nil {
		return nil
	}
	clone := &PickerState{
		query:             p.query,
		rootNodes:         cloneTreeNodes(p.rootNodes),
		activeWorkspaceID: p.activeWorkspaceID,
		activeTabID:       p.activeTabID,
		activePaneID:      p.activePaneID,
		selectedIndex:     p.selectedIndex,
		manuallyExpanded:  make(map[string]bool, len(p.manuallyExpanded)),
		manuallyCollapsed: make(map[string]bool, len(p.manuallyCollapsed)),
	}
	for key, expanded := range p.manuallyExpanded {
		clone.manuallyExpanded[key] = expanded
	}
	for key, collapsed := range p.manuallyCollapsed {
		clone.manuallyCollapsed[key] = collapsed
	}
	return clone
}

func (p *PickerState) SelectedRow() (TreeRow, bool) {
	rows := p.VisibleRows()
	if len(rows) == 0 {
		return TreeRow{}, false
	}
	index := p.selectedIndex
	if index < 0 {
		index = 0
	}
	if index >= len(rows) {
		index = len(rows) - 1
	}
	return rows[index], true
}

func (p *PickerState) MoveSelection(delta int) {
	p.selectedIndex += delta
	p.clampSelection()
}

func (p *PickerState) ExpandSelected() bool {
	row, ok := p.SelectedRow()
	if !ok || !row.Node.isExpandable() {
		return false
	}
	delete(p.manuallyCollapsed, row.Node.Key)
	if row.Expanded && p.manuallyExpanded[row.Node.Key] {
		return false
	}
	p.manuallyExpanded[row.Node.Key] = true
	p.clampSelection()
	return true
}

func (p *PickerState) CollapseSelected() bool {
	row, ok := p.SelectedRow()
	if !ok || !row.Node.isExpandable() {
		return false
	}
	delete(p.manuallyExpanded, row.Node.Key)
	if !row.Expanded && p.manuallyCollapsed[row.Node.Key] {
		return false
	}
	p.manuallyCollapsed[row.Node.Key] = true
	p.clampSelection()
	return true
}

func (p *PickerState) SelectedNode() (TreeNode, bool) {
	row, ok := p.SelectedRow()
	if !ok {
		return TreeNode{}, false
	}
	return row.Node, true
}

func (p *PickerState) VisibleRows() []TreeRow {
	rows := make([]TreeRow, 0, len(p.rootNodes))
	for _, node := range p.rootNodes {
		p.appendVisibleRows(&rows, node, 0)
	}
	return rows
}

// appendVisibleRows 根据默认展开规则和 query 命中路径投影当前树形视图。
// 这里先把“当前路径默认展开”和“命中 pane 时自动展开祖先”固定下来。
func (p *PickerState) appendVisibleRows(rows *[]TreeRow, node TreeNode, depth int) bool {
	match, descendantMatch := p.nodeMatches(node)
	expanded := p.shouldExpand(node, descendantMatch)
	*rows = append(*rows, TreeRow{
		Node:     node,
		Depth:    depth,
		Expanded: expanded,
		Match:    match,
	})
	if !expanded {
		return match || descendantMatch
	}
	for _, child := range node.Children {
		p.appendVisibleRows(rows, child, depth+1)
	}
	return match || descendantMatch
}

func (p *PickerState) shouldExpand(node TreeNode, descendantMatch bool) bool {
	if node.Kind == TreeNodeKindCreate || node.Kind == TreeNodeKindPane {
		return false
	}
	if p.manuallyCollapsed[node.Key] {
		return false
	}
	if descendantMatch && p.query != "" {
		return true
	}
	if p.manuallyExpanded[node.Key] {
		return true
	}
	return p.isActivePath(node)
}

func (p *PickerState) isActivePath(node TreeNode) bool {
	switch node.Kind {
	case TreeNodeKindWorkspace:
		return node.WorkspaceID == p.activeWorkspaceID
	case TreeNodeKindTab:
		return node.WorkspaceID == p.activeWorkspaceID && node.TabID == p.activeTabID
	default:
		return false
	}
}

func (p *PickerState) nodeMatches(node TreeNode) (bool, bool) {
	if p.query == "" {
		return false, false
	}
	match := strings.Contains(strings.ToLower(node.Label), p.query)
	descendant := false
	for _, child := range node.Children {
		childMatch, childDescendant := p.nodeMatches(child)
		if childMatch || childDescendant {
			descendant = true
		}
	}
	return match, descendant
}

func (p *PickerState) defaultSelectionIndex() int {
	rows := p.VisibleRows()
	for index, row := range rows {
		if row.Node.Kind == TreeNodeKindPane &&
			row.Node.WorkspaceID == p.activeWorkspaceID &&
			row.Node.TabID == p.activeTabID &&
			row.Node.PaneID == p.activePaneID {
			return index
		}
	}
	for index, row := range rows {
		if row.Node.Kind == TreeNodeKindTab &&
			row.Node.WorkspaceID == p.activeWorkspaceID &&
			row.Node.TabID == p.activeTabID {
			return index
		}
	}
	for index, row := range rows {
		if row.Node.Kind == TreeNodeKindWorkspace && row.Node.WorkspaceID == p.activeWorkspaceID {
			return index
		}
	}
	return 0
}

func (p *PickerState) clampSelection() {
	rows := p.VisibleRows()
	if len(rows) == 0 {
		p.selectedIndex = 0
		return
	}
	if p.selectedIndex < 0 {
		p.selectedIndex = 0
		return
	}
	if p.selectedIndex >= len(rows) {
		p.selectedIndex = len(rows) - 1
	}
}

func (p *PickerState) resetSelectionForQuery() {
	if p.query == "" {
		p.selectedIndex = p.defaultSelectionIndex()
		return
	}
	rows := p.VisibleRows()
	for index, row := range rows {
		if row.Match && row.Node.Kind != TreeNodeKindCreate {
			p.selectedIndex = index
			return
		}
	}
	p.clampSelection()
}

func cloneTreeNodes(nodes []TreeNode) []TreeNode {
	if len(nodes) == 0 {
		return nil
	}
	out := make([]TreeNode, 0, len(nodes))
	for _, node := range nodes {
		clone := node
		clone.Children = cloneTreeNodes(node.Children)
		out = append(out, clone)
	}
	return out
}

func (n TreeNode) isExpandable() bool {
	return n.Kind == TreeNodeKindWorkspace || n.Kind == TreeNodeKindTab
}
