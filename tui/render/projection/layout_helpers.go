package projection

import (
	"sort"

	"github.com/lozzow/termx/tui/domain/types"
)

// OrderedTiledPaneIDs 返回投影层使用的统一 tiled pane 顺序：
// 先按 split 树叶子顺序展开，再把未出现在 split 树里的 tiled pane 追加到末尾并按 ID 排序。
func OrderedTiledPaneIDs(tab types.TabState) []types.PaneID {
	ordered := make([]types.PaneID, 0, len(tab.Panes))
	seen := make(map[types.PaneID]struct{}, len(tab.Panes))
	collectOrderedTiledPaneIDs(tab.RootSplit, tab, seen, &ordered)

	var remaining []types.PaneID
	for paneID, pane := range tab.Panes {
		if pane.Kind != types.PaneKindTiled {
			continue
		}
		if _, ok := seen[paneID]; ok {
			continue
		}
		remaining = append(remaining, paneID)
	}
	sort.Slice(remaining, func(i, j int) bool {
		return remaining[i] < remaining[j]
	})
	return append(ordered, remaining...)
}

func collectOrderedTiledPaneIDs(node *types.SplitNode, tab types.TabState, seen map[types.PaneID]struct{}, ordered *[]types.PaneID) {
	if node == nil {
		return
	}
	if node.First == nil && node.Second == nil {
		pane, ok := tab.Panes[node.PaneID]
		if !ok || pane.Kind != types.PaneKindTiled {
			return
		}
		if _, ok := seen[node.PaneID]; ok {
			return
		}
		seen[node.PaneID] = struct{}{}
		*ordered = append(*ordered, node.PaneID)
		return
	}
	collectOrderedTiledPaneIDs(node.First, tab, seen, ordered)
	collectOrderedTiledPaneIDs(node.Second, tab, seen, ordered)
}
