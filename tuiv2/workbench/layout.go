package workbench

import "math"

type SplitDirection string

const (
	SplitHorizontal SplitDirection = "horizontal"
	SplitVertical   SplitDirection = "vertical"
)

type Direction string

const (
	DirectionLeft  Direction = "left"
	DirectionRight Direction = "right"
	DirectionUp    Direction = "up"
	DirectionDown  Direction = "down"
)

type Rect struct {
	X int
	Y int
	W int
	H int
}

type DividerHit struct {
	Node *LayoutNode
	Rect Rect
	Root Rect
}

type LayoutNode struct {
	PaneID    string
	Direction SplitDirection
	Ratio     float64
	First     *LayoutNode
	Second    *LayoutNode
}

func NewLeaf(paneID string) *LayoutNode {
	return &LayoutNode{PaneID: paneID}
}

func (n *LayoutNode) IsLeaf() bool {
	return n != nil && n.First == nil && n.Second == nil
}

func (n *LayoutNode) Remove(paneID string) *LayoutNode {
	if n == nil {
		return nil
	}
	if n.IsLeaf() {
		if n.PaneID == paneID {
			return nil
		}
		return n
	}
	n.First = n.First.Remove(paneID)
	n.Second = n.Second.Remove(paneID)
	switch {
	case n.First == nil:
		return n.Second
	case n.Second == nil:
		return n.First
	default:
		return n
	}
}

func (n *LayoutNode) LeafIDs() []string {
	if n == nil {
		return nil
	}
	if n.IsLeaf() {
		return []string{n.PaneID}
	}
	out := n.First.LeafIDs()
	out = append(out, n.Second.LeafIDs()...)
	return out
}

func (n *LayoutNode) Rects(root Rect) map[string]Rect {
	out := make(map[string]Rect)
	n.fillRects(root, out)
	return out
}

func (n *LayoutNode) fillRects(root Rect, out map[string]Rect) {
	if n == nil || root.W <= 0 || root.H <= 0 {
		return
	}
	if n.IsLeaf() {
		out[n.PaneID] = root
		return
	}
	firstRect, secondRect := splitRects(root, n.Direction, n.Ratio)
	n.First.fillRects(firstRect, out)
	n.Second.fillRects(secondRect, out)
}

func (n *LayoutNode) DividerAt(root Rect, x, y int) (DividerHit, bool) {
	if n == nil || n.IsLeaf() || root.W <= 0 || root.H <= 0 {
		return DividerHit{}, false
	}
	firstRect, secondRect := splitRects(root, n.Direction, n.Ratio)
	if hit, ok := n.First.DividerAt(firstRect, x, y); ok {
		return hit, true
	}
	if hit, ok := n.Second.DividerAt(secondRect, x, y); ok {
		return hit, true
	}
	dividerRect, ok := splitDividerRect(root, n.Direction, n.Ratio)
	if !ok {
		return DividerHit{}, false
	}
	if x < dividerRect.X || x >= dividerRect.X+dividerRect.W || y < dividerRect.Y || y >= dividerRect.Y+dividerRect.H {
		return DividerHit{}, false
	}
	return DividerHit{Node: n, Rect: dividerRect, Root: root}, true
}

func (n *LayoutNode) SetRatioFromDivider(root Rect, x, y, offsetX, offsetY int) bool {
	if n == nil || n.IsLeaf() {
		return false
	}
	oldRatio := effectiveRatio(n.Ratio)
	switch n.Direction {
	case SplitHorizontal:
		if root.H <= 1 {
			return false
		}
		firstH, secondH := splitSizes(root.H, oldRatio)
		if firstH <= 0 || secondH <= 0 {
			return false
		}
		nextFirst := y - offsetY - root.Y + 1
		if nextFirst < 1 {
			nextFirst = 1
		}
		if nextFirst >= root.H {
			nextFirst = root.H - 1
		}
		n.Ratio = float64(nextFirst) / float64(root.H)
	default:
		if root.W <= 1 {
			return false
		}
		firstW, secondW := splitSizes(root.W, oldRatio)
		if firstW <= 0 || secondW <= 0 {
			return false
		}
		nextFirst := x - offsetX - root.X + 1
		if nextFirst < 1 {
			nextFirst = 1
		}
		if nextFirst >= root.W {
			nextFirst = root.W - 1
		}
		n.Ratio = float64(nextFirst) / float64(root.W)
	}
	return math.Abs(n.Ratio-oldRatio) > 1e-9
}

func splitRects(root Rect, dir SplitDirection, ratio float64) (Rect, Rect) {
	ratio = effectiveRatio(ratio)
	if dir == SplitHorizontal {
		firstH, secondH := splitSizes(root.H, ratio)
		return Rect{X: root.X, Y: root.Y, W: root.W, H: firstH},
			Rect{X: root.X, Y: root.Y + firstH, W: root.W, H: secondH}
	}
	firstW, secondW := splitSizes(root.W, ratio)
	return Rect{X: root.X, Y: root.Y, W: firstW, H: root.H},
		Rect{X: root.X + firstW, Y: root.Y, W: secondW, H: root.H}
}

func splitDividerRect(root Rect, dir SplitDirection, ratio float64) (Rect, bool) {
	ratio = effectiveRatio(ratio)
	if dir == SplitHorizontal {
		firstH, secondH := splitSizes(root.H, ratio)
		if firstH <= 0 || secondH <= 0 {
			return Rect{}, false
		}
		return Rect{X: root.X, Y: root.Y + firstH - 1, W: root.W, H: 2}, true
	}
	firstW, secondW := splitSizes(root.W, ratio)
	if firstW <= 0 || secondW <= 0 {
		return Rect{}, false
	}
	return Rect{X: root.X + firstW - 1, Y: root.Y, W: 2, H: root.H}, true
}

func effectiveRatio(ratio float64) float64 {
	if ratio <= 0 || ratio >= 1 {
		return 0.5
	}
	return ratio
}

func splitSizes(total int, ratio float64) (int, int) {
	if total <= 0 {
		return 0, 0
	}
	first := int(math.Round(float64(total) * ratio))
	if first < 1 {
		first = 1
	}
	if first >= total {
		first = total - 1
	}
	return first, total - first
}
