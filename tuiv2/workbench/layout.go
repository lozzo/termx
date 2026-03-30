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
	ratio := n.Ratio
	if ratio <= 0 || ratio >= 1 {
		ratio = 0.5
	}
	if n.Direction == SplitHorizontal {
		firstH := int(math.Round(float64(root.H) * ratio))
		if firstH < 1 {
			firstH = 1
		}
		if firstH >= root.H {
			firstH = root.H - 1
		}
		n.First.fillRects(Rect{X: root.X, Y: root.Y, W: root.W, H: firstH}, out)
		n.Second.fillRects(Rect{X: root.X, Y: root.Y + firstH, W: root.W, H: root.H - firstH}, out)
		return
	}
	firstW := int(math.Round(float64(root.W) * ratio))
	if firstW < 1 {
		firstW = 1
	}
	if firstW >= root.W {
		firstW = root.W - 1
	}
	n.First.fillRects(Rect{X: root.X, Y: root.Y, W: firstW, H: root.H}, out)
	n.Second.fillRects(Rect{X: root.X + firstW, Y: root.Y, W: root.W - firstW, H: root.H}, out)
}
