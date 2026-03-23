package layout

import "github.com/lozzow/termx/tui/domain/types"

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
		n.Direction = dir
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

// fillRects 把布局树稳定投影成每个 pane 的矩形区域。
// 这层逻辑后续会直接影响焦点导航和渲染，所以先保持纯函数。
func fillRects(n *Node, root types.Rect, out map[types.PaneID]types.Rect) {
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
	if n.Direction == types.SplitDirectionHorizontal {
		firstH := int(float64(root.H) * ratio)
		if firstH < 1 {
			firstH = 1
		}
		if firstH >= root.H {
			firstH = root.H - 1
		}
		fillRects(n.First, types.Rect{X: root.X, Y: root.Y, W: root.W, H: firstH}, out)
		fillRects(n.Second, types.Rect{X: root.X, Y: root.Y + firstH, W: root.W, H: root.H - firstH}, out)
		return
	}
	firstW := int(float64(root.W) * ratio)
	if firstW < 1 {
		firstW = 1
	}
	if firstW >= root.W {
		firstW = root.W - 1
	}
	fillRects(n.First, types.Rect{X: root.X, Y: root.Y, W: firstW, H: root.H}, out)
	fillRects(n.Second, types.Rect{X: root.X + firstW, Y: root.Y, W: root.W - firstW, H: root.H}, out)
}

func (n *Node) Adjacent(target types.PaneID, dir types.Direction, rects map[types.PaneID]types.Rect) types.PaneID {
	base, ok := rects[target]
	if !ok {
		return ""
	}
	var bestID types.PaneID
	bestDist := int(^uint(0) >> 1)
	for paneID, rect := range rects {
		if paneID == target || !isCandidate(base, rect, dir) {
			continue
		}
		dist := edgeDistance(base, rect, dir)
		if dist < bestDist {
			bestDist = dist
			bestID = paneID
		}
	}
	return bestID
}

func isCandidate(base, other types.Rect, dir types.Direction) bool {
	switch dir {
	case types.DirectionLeft:
		return other.X+other.W <= base.X && rangesOverlap(base.Y, base.Y+base.H, other.Y, other.Y+other.H)
	case types.DirectionRight:
		return other.X >= base.X+base.W && rangesOverlap(base.Y, base.Y+base.H, other.Y, other.Y+other.H)
	case types.DirectionUp:
		return other.Y+other.H <= base.Y && rangesOverlap(base.X, base.X+base.W, other.X, other.X+other.W)
	case types.DirectionDown:
		return other.Y >= base.Y+base.H && rangesOverlap(base.X, base.X+base.W, other.X, other.X+other.W)
	default:
		return false
	}
}

func rangesOverlap(a0, a1, b0, b1 int) bool {
	return a0 < b1 && b0 < a1
}

func edgeDistance(base, other types.Rect, dir types.Direction) int {
	switch dir {
	case types.DirectionLeft:
		return base.X - (other.X + other.W)
	case types.DirectionRight:
		return other.X - (base.X + base.W)
	case types.DirectionUp:
		return base.Y - (other.Y + other.H)
	case types.DirectionDown:
		return other.Y - (base.Y + base.H)
	default:
		return 0
	}
}
