package layout

import (
	"math"

	"github.com/lozzow/termx/tui/core/types"
)

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

// Split 只维护纯 layout tree，不关心 pane 的业务状态。
func (n *Node) Split(targetPaneID types.PaneID, direction types.SplitDirection, newPaneID types.PaneID) (*Node, bool) {
	if n == nil {
		return nil, false
	}
	if n.IsLeaf() {
		if n.PaneID != targetPaneID {
			return n, false
		}
		n.Direction = direction
		n.Ratio = 0.5
		n.First = &Node{PaneID: targetPaneID}
		n.Second = &Node{PaneID: newPaneID}
		n.PaneID = ""
		return n, true
	}
	if next, ok := n.First.Split(targetPaneID, direction, newPaneID); ok {
		n.First = next
		return n, true
	}
	if next, ok := n.Second.Split(targetPaneID, direction, newPaneID); ok {
		n.Second = next
		return n, true
	}
	return n, false
}

func (n *Node) Remove(targetPaneID types.PaneID) *Node {
	if n == nil {
		return nil
	}
	if n.IsLeaf() {
		if n.PaneID == targetPaneID {
			return nil
		}
		return n
	}
	n.First = n.First.Remove(targetPaneID)
	n.Second = n.Second.Remove(targetPaneID)
	switch {
	case n.First == nil:
		return n.Second
	case n.Second == nil:
		return n.First
	default:
		return n
	}
}

func (n *Node) Project(root types.Rect) map[types.PaneID]types.Rect {
	out := make(map[types.PaneID]types.Rect)
	n.project(root, out)
	return out
}

func (n *Node) project(root types.Rect, out map[types.PaneID]types.Rect) {
	if n == nil || root.W <= 0 || root.H <= 0 {
		return
	}
	if n.IsLeaf() {
		out[n.PaneID] = root
		return
	}
	ratio := n.Ratio
	if ratio <= 0 || ratio >= 1 {
		ratio = 0.5
	}
	if n.Direction == types.SplitHorizontal {
		firstH := boundedSplitSize(root.H, ratio)
		n.First.project(types.Rect{X: root.X, Y: root.Y, W: root.W, H: firstH}, out)
		n.Second.project(types.Rect{X: root.X, Y: root.Y + firstH, W: root.W, H: root.H - firstH}, out)
		return
	}
	firstW := boundedSplitSize(root.W, ratio)
	n.First.project(types.Rect{X: root.X, Y: root.Y, W: firstW, H: root.H}, out)
	n.Second.project(types.Rect{X: root.X + firstW, Y: root.Y, W: root.W - firstW, H: root.H}, out)
}

func boundedSplitSize(total int, ratio float64) int {
	if total <= 1 {
		return total
	}
	size := int(math.Round(float64(total) * ratio))
	if size < 1 {
		return 1
	}
	if size >= total {
		return total - 1
	}
	return size
}
