package layout

import (
	"math"

	"github.com/lozzow/termx/tui/domain/types"
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

func (n *Node) ContainsPane(paneID types.PaneID) bool {
	if n == nil {
		return false
	}
	if n.IsLeaf() {
		return n.PaneID == paneID
	}
	return n.First.ContainsPane(paneID) || n.Second.ContainsPane(paneID)
}

func (n *Node) LeafIDs() []types.PaneID {
	if n == nil {
		return nil
	}
	if n.IsLeaf() {
		return []types.PaneID{n.PaneID}
	}
	out := n.First.LeafIDs()
	out = append(out, n.Second.LeafIDs()...)
	return out
}

func (n *Node) SwapWithNeighbor(paneID types.PaneID, delta int) bool {
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

func (n *Node) AdjustPaneBoundary(paneID types.PaneID, dir types.Direction, step, minSpan int, root types.Rect) bool {
	if n == nil || step <= 0 || root.W <= 0 || root.H <= 0 {
		return false
	}
	if minSpan <= 0 {
		minSpan = 1
	}
	return n.adjustPaneBoundary(paneID, dir, step, minSpan, root)
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
	ratio := normalizedRatio(n.Ratio)
	if n.Direction == types.SplitDirectionHorizontal {
		firstH := splitSpan(root.H, ratio)
		fillRects(n.First, types.Rect{X: root.X, Y: root.Y, W: root.W, H: firstH}, out)
		fillRects(n.Second, types.Rect{X: root.X, Y: root.Y + firstH, W: root.W, H: root.H - firstH}, out)
		return
	}
	firstW := splitSpan(root.W, ratio)
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

func (n *Node) adjustPaneBoundary(paneID types.PaneID, dir types.Direction, step, minSpan int, root types.Rect) bool {
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
	case types.SplitDirectionVertical:
		switch {
		case dir == types.DirectionRight && inFirst:
			return n.adjustRatio(step, root.W, minSpan)
		case dir == types.DirectionLeft && inSecond:
			return n.adjustRatio(-step, root.W, minSpan)
		}
	case types.SplitDirectionHorizontal:
		switch {
		case dir == types.DirectionDown && inFirst:
			return n.adjustRatio(step, root.H, minSpan)
		case dir == types.DirectionUp && inSecond:
			return n.adjustRatio(-step, root.H, minSpan)
		}
	}
	return false
}

func (n *Node) leafNodes() []*Node {
	if n == nil {
		return nil
	}
	if n.IsLeaf() {
		return []*Node{n}
	}
	out := n.First.leafNodes()
	out = append(out, n.Second.leafNodes()...)
	return out
}

func (n *Node) splitRects(root types.Rect) (types.Rect, types.Rect) {
	if n == nil {
		return types.Rect{}, types.Rect{}
	}
	ratio := normalizedRatio(n.Ratio)
	if n.Direction == types.SplitDirectionHorizontal {
		firstH := splitSpan(root.H, ratio)
		return types.Rect{X: root.X, Y: root.Y, W: root.W, H: firstH},
			types.Rect{X: root.X, Y: root.Y + firstH, W: root.W, H: root.H - firstH}
	}
	firstW := splitSpan(root.W, ratio)
	return types.Rect{X: root.X, Y: root.Y, W: firstW, H: root.H},
		types.Rect{X: root.X + firstW, Y: root.Y, W: root.W - firstW, H: root.H}
}

// adjustRatio 只做比例夹紧，不接触渲染状态。
// minSpan 会被换算成最小比例，确保分割线两侧都至少保留最小可用跨度。
func (n *Node) adjustRatio(delta, span, minSpan int) bool {
	if n == nil || span <= 1 {
		return false
	}
	ratio := normalizedRatio(n.Ratio)
	minRatio := float64(minSpan) / float64(span)
	if minRatio < 0 {
		minRatio = 0
	}
	// 比例上限不能逼近 0/1 太多，否则极小 root 下会让另一侧无法留下有效空间。
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

func normalizedRatio(ratio float64) float64 {
	if ratio <= 0 || ratio >= 1 {
		return 0.5
	}
	return ratio
}

func splitSpan(span int, ratio float64) int {
	first := int(float64(span) * ratio)
	if first < 1 {
		first = 1
	}
	if first >= span {
		first = span - 1
	}
	return first
}
