package layout

import "github.com/lozzow/termx/tui/state/types"

type Node struct {
	PaneID    types.PaneID
	Direction types.SplitDirection
	Ratio     float64
	First     *Node
	Second    *Node
}

func NewLeaf(paneID types.PaneID) *Node {
	return &Node{PaneID: paneID}
}

func (n *Node) IsLeaf() bool {
	return n != nil && n.First == nil && n.Second == nil
}

func (n *Node) Split(target types.PaneID, dir types.SplitDirection, newPaneID types.PaneID) bool {
	if n == nil {
		return false
	}
	if n.IsLeaf() {
		if n.PaneID != target {
			return false
		}
		n.Direction = dir.Normalize()
		n.Ratio = 0.5
		n.First = &Node{PaneID: target}
		n.Second = &Node{PaneID: newPaneID}
		n.PaneID = ""
		return true
	}
	return n.First.Split(target, dir, newPaneID) || n.Second.Split(target, dir, newPaneID)
}

func (n *Node) Remove(target types.PaneID) *Node {
	if n == nil {
		return nil
	}
	if n.IsLeaf() {
		if n.PaneID == target {
			return nil
		}
		return n
	}
	n.First = n.First.Remove(target)
	n.Second = n.Second.Remove(target)
	switch {
	case n.First == nil:
		return n.Second
	case n.Second == nil:
		return n.First
	default:
		return n
	}
}

func (n *Node) Rects(root types.Rect) map[types.PaneID]types.Rect {
	out := make(map[types.PaneID]types.Rect)
	fillRects(n, root, out)
	return out
}

// fillRects 把 split tree 投影成每个 pane 的最终矩形。
// 这层必须保持纯函数，后续渲染和焦点导航才能共享同一套几何结果。
func fillRects(n *Node, root types.Rect, out map[types.PaneID]types.Rect) {
	if n == nil || root.Empty() {
		return
	}
	if n.IsLeaf() {
		out[n.PaneID] = root
		return
	}
	ratio := normalizedRatio(n.Ratio)
	if n.Direction.Normalize() == types.SplitDirectionHorizontal {
		firstH := splitSpan(root.H, ratio)
		fillRects(n.First, types.Rect{X: root.X, Y: root.Y, W: root.W, H: firstH}, out)
		fillRects(n.Second, types.Rect{X: root.X, Y: root.Y + firstH, W: root.W, H: root.H - firstH}, out)
		return
	}
	firstW := splitSpan(root.W, ratio)
	fillRects(n.First, types.Rect{X: root.X, Y: root.Y, W: firstW, H: root.H}, out)
	fillRects(n.Second, types.Rect{X: root.X + firstW, Y: root.Y, W: root.W - firstW, H: root.H}, out)
}

func normalizedRatio(ratio float64) float64 {
	if ratio <= 0 || ratio >= 1 {
		return 0.5
	}
	return ratio
}

// splitSpan 保证分割线两侧至少留下 1 个单元。
// 奇数宽高时把余量放到 second，和测试里的 pane rect 预期一致。
func splitSpan(span int, ratio float64) int {
	if span <= 1 {
		return span
	}
	first := int(float64(span) * ratio)
	if first < 1 {
		first = 1
	}
	if first >= span {
		first = span - 1
	}
	return first
}
