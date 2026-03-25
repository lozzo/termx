package projection

import "github.com/lozzow/termx/tui/domain/types"

// LeafPaneIDsFromSplit 按布局树的先序叶子顺序导出 pane 列表。
// 这让投影层后续可直接复用布局顺序，而不反向依赖 layout 包实现细节。
func LeafPaneIDsFromSplit(root *types.SplitNode) []types.PaneID {
	if root == nil {
		return nil
	}
	if root.First == nil && root.Second == nil {
		if root.PaneID == "" {
			return nil
		}
		return []types.PaneID{root.PaneID}
	}
	out := LeafPaneIDsFromSplit(root.First)
	out = append(out, LeafPaneIDsFromSplit(root.Second)...)
	return out
}
