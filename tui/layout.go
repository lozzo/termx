package tui

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

func (n *LayoutNode) Split(paneID string, dir SplitDirection, newPaneID string) bool {
	if n == nil {
		return false
	}
	if n.IsLeaf() {
		if n.PaneID != paneID {
			return false
		}
		n.Direction = dir
		n.Ratio = 0.5
		n.First = &LayoutNode{PaneID: paneID}
		n.Second = &LayoutNode{PaneID: newPaneID}
		n.PaneID = ""
		return true
	}
	return n.First.Split(paneID, dir, newPaneID) || n.Second.Split(paneID, dir, newPaneID)
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

func (n *LayoutNode) SwapWithNeighbor(paneID string, delta int) bool {
	if n == nil || delta == 0 {
		return false
	}
	leaves := n.leafNodes()
	if len(leaves) < 2 {
		return false
	}
	idx := -1
	for i, leaf := range leaves {
		if leaf != nil && leaf.PaneID == paneID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return false
	}
	target := idx + delta
	if target < 0 || target >= len(leaves) {
		return false
	}
	leaves[idx].PaneID, leaves[target].PaneID = leaves[target].PaneID, leaves[idx].PaneID
	return true
}

func (n *LayoutNode) ContainsPane(paneID string) bool {
	if n == nil {
		return false
	}
	if n.IsLeaf() {
		return n.PaneID == paneID
	}
	return n.First.ContainsPane(paneID) || n.Second.ContainsPane(paneID)
}

func (n *LayoutNode) AdjustPaneBoundary(paneID string, dir Direction, step, minSpan int, root Rect) bool {
	if n == nil || step <= 0 || root.W <= 0 || root.H <= 0 {
		return false
	}
	if minSpan <= 0 {
		minSpan = 1
	}
	return n.adjustPaneBoundary(paneID, dir, step, minSpan, root)
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

func (n *LayoutNode) Adjacent(paneID string, dir Direction, rects map[string]Rect) string {
	base, ok := rects[paneID]
	if !ok {
		return ""
	}
	bestID := ""
	bestDist := math.MaxFloat64
	for id, rect := range rects {
		if id == paneID {
			continue
		}
		if !isCandidate(base, rect, dir) {
			continue
		}
		dist := edgeDistance(base, rect, dir)
		if dist < bestDist {
			bestDist = dist
			bestID = id
		}
	}
	return bestID
}

func isCandidate(base, other Rect, dir Direction) bool {
	switch dir {
	case DirectionLeft:
		return other.X+other.W <= base.X && rangesOverlap(base.Y, base.Y+base.H, other.Y, other.Y+other.H)
	case DirectionRight:
		return other.X >= base.X+base.W && rangesOverlap(base.Y, base.Y+base.H, other.Y, other.Y+other.H)
	case DirectionUp:
		return other.Y+other.H <= base.Y && rangesOverlap(base.X, base.X+base.W, other.X, other.X+other.W)
	case DirectionDown:
		return other.Y >= base.Y+base.H && rangesOverlap(base.X, base.X+base.W, other.X, other.X+other.W)
	default:
		return false
	}
}

func edgeDistance(base, other Rect, dir Direction) float64 {
	switch dir {
	case DirectionLeft:
		return float64(base.X - (other.X + other.W))
	case DirectionRight:
		return float64(other.X - (base.X + base.W))
	case DirectionUp:
		return float64(base.Y - (other.Y + other.H))
	case DirectionDown:
		return float64(other.Y - (base.Y + base.H))
	default:
		return math.MaxFloat64
	}
}

func rangesOverlap(a0, a1, b0, b1 int) bool {
	return a0 < b1 && b0 < a1
}

func (n *LayoutNode) adjustPaneBoundary(paneID string, dir Direction, step, minSpan int, root Rect) bool {
	if n == nil || n.IsLeaf() {
		return false
	}
	firstRect, secondRect := n.splitRects(root)
	inFirst := n.First.ContainsPane(paneID)
	inSecond := n.Second.ContainsPane(paneID)

	if inFirst && n.First.adjustPaneBoundary(paneID, dir, step, minSpan, firstRect) {
		return true
	}
	if inSecond && n.Second.adjustPaneBoundary(paneID, dir, step, minSpan, secondRect) {
		return true
	}

	switch n.Direction {
	case SplitVertical:
		switch {
		case dir == DirectionRight && inFirst:
			return n.adjustRatio(step, root.W, minSpan)
		case dir == DirectionLeft && inSecond:
			return n.adjustRatio(-step, root.W, minSpan)
		}
	case SplitHorizontal:
		switch {
		case dir == DirectionDown && inFirst:
			return n.adjustRatio(step, root.H, minSpan)
		case dir == DirectionUp && inSecond:
			return n.adjustRatio(-step, root.H, minSpan)
		}
	}
	return false
}

func (n *LayoutNode) leafNodes() []*LayoutNode {
	if n == nil {
		return nil
	}
	if n.IsLeaf() {
		return []*LayoutNode{n}
	}
	out := n.First.leafNodes()
	out = append(out, n.Second.leafNodes()...)
	return out
}

func (n *LayoutNode) splitRects(root Rect) (Rect, Rect) {
	if n == nil {
		return Rect{}, Rect{}
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
		return Rect{X: root.X, Y: root.Y, W: root.W, H: firstH},
			Rect{X: root.X, Y: root.Y + firstH, W: root.W, H: root.H - firstH}
	}
	firstW := int(math.Round(float64(root.W) * ratio))
	if firstW < 1 {
		firstW = 1
	}
	if firstW >= root.W {
		firstW = root.W - 1
	}
	return Rect{X: root.X, Y: root.Y, W: firstW, H: root.H},
		Rect{X: root.X + firstW, Y: root.Y, W: root.W - firstW, H: root.H}
}

func (n *LayoutNode) adjustRatio(delta, span, minSpan int) bool {
	if n == nil || span <= 1 {
		return false
	}
	ratio := n.Ratio
	if ratio <= 0 || ratio >= 1 {
		ratio = 0.5
	}
	minRatio := float64(minSpan) / float64(span)
	if minRatio < 0 {
		minRatio = 0
	}
	if minRatio > 0.45 {
		minRatio = 0.45
	}
	next := ratio + float64(delta)/float64(span)
	if next < minRatio {
		next = minRatio
	}
	if next > 1-minRatio {
		next = 1 - minRatio
	}
	if math.Abs(next-ratio) < 0.0001 {
		return false
	}
	n.Ratio = next
	return true
}
