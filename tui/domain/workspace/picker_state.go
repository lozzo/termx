package workspace

import (
	"strings"

	"github.com/lozzow/termx/tui/domain/types"
)

type PickerState struct {
	query             string
	rootNodes         []TreeNode
	activeWorkspaceID types.WorkspaceID
	activeTabID       types.TabID
	manuallyExpanded  map[string]bool
}

type TreeRow struct {
	Node     TreeNode
	Depth    int
	Expanded bool
	Match    bool
}

func NewPickerState(state types.DomainState) *PickerState {
	activeTabID := types.TabID("")
	if ws, ok := state.Workspaces[state.ActiveWorkspaceID]; ok {
		activeTabID = ws.ActiveTabID
	}
	return &PickerState{
		rootNodes:         BuildPickerTree(state),
		activeWorkspaceID: state.ActiveWorkspaceID,
		activeTabID:       activeTabID,
		manuallyExpanded:  make(map[string]bool),
	}
}

func (p *PickerState) SetQuery(query string) {
	p.query = strings.TrimSpace(strings.ToLower(query))
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
