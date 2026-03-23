package workspace

import (
	"slices"

	"github.com/lozzow/termx/tui/domain/types"
)

type TreeNodeKind string

const (
	TreeNodeKindCreate    TreeNodeKind = "create"
	TreeNodeKindWorkspace TreeNodeKind = "workspace"
	TreeNodeKindTab       TreeNodeKind = "tab"
	TreeNodeKindPane      TreeNodeKind = "pane"
)

type TreeNode struct {
	Key         string
	Kind        TreeNodeKind
	WorkspaceID types.WorkspaceID
	TabID       types.TabID
	PaneID      types.PaneID
	Label       string
	Children    []TreeNode
}

func BuildPickerTree(state types.DomainState) []TreeNode {
	out := []TreeNode{{
		Key:   "create-workspace",
		Kind:  TreeNodeKindCreate,
		Label: "+ create workspace",
	}}
	for _, workspaceID := range state.WorkspaceOrder {
		ws, ok := state.Workspaces[workspaceID]
		if !ok {
			continue
		}
		workspaceNode := TreeNode{
			Key:         string(workspaceID),
			Kind:        TreeNodeKindWorkspace,
			WorkspaceID: workspaceID,
			Label:       ws.Name,
		}
		for _, tabID := range ws.TabOrder {
			tab, ok := ws.Tabs[tabID]
			if !ok {
				continue
			}
			tabNode := TreeNode{
				Key:         string(workspaceID) + "/" + string(tabID),
				Kind:        TreeNodeKindTab,
				WorkspaceID: workspaceID,
				TabID:       tabID,
				Label:       tab.Name,
			}
			paneIDs := sortedPaneIDs(tab.Panes)
			for _, paneID := range paneIDs {
				pane := tab.Panes[paneID]
				tabNode.Children = append(tabNode.Children, TreeNode{
					Key:         string(workspaceID) + "/" + string(tabID) + "/" + string(paneID),
					Kind:        TreeNodeKindPane,
					WorkspaceID: workspaceID,
					TabID:       tabID,
					PaneID:      paneID,
					Label:       paneLabel(pane, state.Terminals),
				})
			}
			workspaceNode.Children = append(workspaceNode.Children, tabNode)
		}
		out = append(out, workspaceNode)
	}
	return out
}

func ResolveTreeJumpFocus(ws types.WorkspaceState, tabID types.TabID, paneID types.PaneID) (types.FocusState, bool) {
	tab, ok := ws.Tabs[tabID]
	if !ok {
		return types.FocusState{}, false
	}
	pane, ok := tab.Panes[paneID]
	if !ok {
		return types.FocusState{}, false
	}
	layer := types.FocusLayerTiled
	if pane.Kind == types.PaneKindFloating {
		layer = types.FocusLayerFloating
	}
	return types.FocusState{
		Layer:       layer,
		WorkspaceID: ws.ID,
		TabID:       tabID,
		PaneID:      paneID,
	}, true
}

func sortedPaneIDs(panes map[types.PaneID]types.PaneState) []types.PaneID {
	ids := make([]types.PaneID, 0, len(panes))
	for id := range panes {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

func paneLabel(pane types.PaneState, terminals map[types.TerminalID]types.TerminalRef) string {
	if pane.TerminalID != "" {
		if terminal, ok := terminals[pane.TerminalID]; ok && terminal.Name != "" {
			return terminal.Name
		}
	}
	switch pane.SlotState {
	case types.PaneSlotExited:
		return "program exited pane"
	case types.PaneSlotWaiting:
		return "waiting slot"
	default:
		return "unconnected pane"
	}
}
