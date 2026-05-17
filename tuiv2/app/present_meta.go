package app

import "github.com/lozzow/termx/tuiv2/render"

type hostOwnerID = uint32

type rect struct {
	Left   int
	Top    int
	Right  int
	Bottom int
}

type presentMeta struct {
	OwnerMap     [][]hostOwnerID
	RowOwners    []hostOwnerID
	Width        int
	VisibleRects map[hostOwnerID][]rect
}

type activeRect struct {
	rect rect
	seen bool
}

func presentMetaFromRender(meta *render.PresentMetadata) *presentMeta {
	if meta == nil {
		return nil
	}
	if len(meta.OwnerMap) == 0 && len(meta.RowOwners) == 0 {
		return nil
	}
	out := &presentMeta{
		OwnerMap:  meta.OwnerMap,
		RowOwners: meta.RowOwners,
		Width:     meta.Width,
	}
	if len(out.OwnerMap) > 0 {
		out.VisibleRects = visibleRectsFromOwnerMap(out.OwnerMap)
	} else {
		out.VisibleRects = visibleRectsFromRowOwners(out.RowOwners, out.Width)
	}
	return out
}

func retainPresentMeta(meta *presentMeta) *presentMeta {
	return meta
}

func clonePresentMeta(meta *presentMeta) *presentMeta {
	if meta == nil {
		return nil
	}
	out := &presentMeta{
		OwnerMap:     make([][]hostOwnerID, len(meta.OwnerMap)),
		RowOwners:    append([]hostOwnerID(nil), meta.RowOwners...),
		Width:        meta.Width,
		VisibleRects: make(map[hostOwnerID][]rect, len(meta.VisibleRects)),
	}
	for y := range meta.OwnerMap {
		if len(meta.OwnerMap[y]) == 0 {
			continue
		}
		out.OwnerMap[y] = append([]hostOwnerID(nil), meta.OwnerMap[y]...)
	}
	for owner, rects := range meta.VisibleRects {
		out.VisibleRects[owner] = append([]rect(nil), rects...)
	}
	return out
}

func visibleRectsFromRowOwners(rowOwners []hostOwnerID, width int) map[hostOwnerID][]rect {
	if len(rowOwners) == 0 || width <= 0 {
		return nil
	}
	result := make(map[hostOwnerID][]rect)
	start := 0
	for start < len(rowOwners) {
		owner := rowOwners[start]
		end := start
		for end+1 < len(rowOwners) && rowOwners[end+1] == owner {
			end++
		}
		if owner != 0 {
			result[owner] = append(result[owner], rect{
				Left:   0,
				Top:    start,
				Right:  width - 1,
				Bottom: end,
			})
		}
		start = end + 1
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func visibleRectsFromOwnerMap(ownerMap [][]hostOwnerID) map[hostOwnerID][]rect {
	if len(ownerMap) == 0 {
		return nil
	}
	result := make(map[hostOwnerID][]rect)
	active := make(map[hostOwnerID][]activeRect)
	flushUnseen := func(row int) {
		for owner, rects := range active {
			kept := rects[:0]
			for _, item := range rects {
				if item.seen {
					item.seen = false
					kept = append(kept, item)
					continue
				}
				closed := item.rect
				closed.Bottom = row - 1
				result[owner] = append(result[owner], closed)
			}
			if len(kept) == 0 {
				delete(active, owner)
				continue
			}
			active[owner] = kept
		}
	}
	for y, row := range ownerMap {
		for x := 0; x < len(row); {
			owner := row[x]
			start := x
			for x+1 < len(row) && row[x+1] == owner {
				x++
			}
			if owner != 0 {
				active[owner] = observeActiveOwnerSpan(active[owner], start, x, y)
			}
			x++
		}
		flushUnseen(y)
	}
	flushUnseen(len(ownerMap))
	for owner, rects := range active {
		for _, item := range rects {
			result[owner] = append(result[owner], item.rect)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func observeActiveOwnerSpan(active []activeRect, left, right, row int) []activeRect {
	for i := range active {
		current := &active[i]
		if current.rect.Left == left && current.rect.Right == right && current.rect.Bottom+1 == row {
			current.rect.Bottom = row
			current.seen = true
			return active
		}
	}
	return append(active, activeRect{
		rect: rect{
			Left:   left,
			Top:    row,
			Right:  right,
			Bottom: row,
		},
		seen: true,
	})
}
