package app

import "github.com/lozzow/termx/tuiv2/render"

type hostOwnerID uint32

type rect struct {
	Left   int
	Top    int
	Right  int
	Bottom int
}

type presentMeta struct {
	OwnerMap     [][]hostOwnerID
	VisibleRects map[hostOwnerID][]rect
}

func presentMetaFromRender(meta *render.PresentMetadata) *presentMeta {
	if meta == nil || len(meta.OwnerMap) == 0 {
		return nil
	}
	out := &presentMeta{
		OwnerMap: make([][]hostOwnerID, len(meta.OwnerMap)),
	}
	for y := range meta.OwnerMap {
		if len(meta.OwnerMap[y]) == 0 {
			continue
		}
		out.OwnerMap[y] = make([]hostOwnerID, len(meta.OwnerMap[y]))
		for x := range meta.OwnerMap[y] {
			out.OwnerMap[y][x] = hostOwnerID(meta.OwnerMap[y][x])
		}
	}
	out.VisibleRects = visibleRectsFromOwnerMap(out.OwnerMap)
	return out
}

func clonePresentMeta(meta *presentMeta) *presentMeta {
	if meta == nil {
		return nil
	}
	out := &presentMeta{
		OwnerMap:     make([][]hostOwnerID, len(meta.OwnerMap)),
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

func visibleRectsFromOwnerMap(ownerMap [][]hostOwnerID) map[hostOwnerID][]rect {
	if len(ownerMap) == 0 {
		return nil
	}
	type span struct {
		left  int
		right int
	}
	active := make(map[hostOwnerID]map[span]int)
	result := make(map[hostOwnerID][]rect)
	flushMissing := func(owner hostOwnerID, keep map[span]struct{}, row int) {
		for sp, top := range active[owner] {
			if _, ok := keep[sp]; ok {
				continue
			}
			result[owner] = append(result[owner], rect{
				Left:   sp.left,
				Top:    top,
				Right:  sp.right,
				Bottom: row - 1,
			})
			delete(active[owner], sp)
		}
		if len(active[owner]) == 0 {
			delete(active, owner)
		}
	}
	for y, row := range ownerMap {
		rowSpans := make(map[hostOwnerID][]span)
		for x := 0; x < len(row); {
			owner := row[x]
			start := x
			for x+1 < len(row) && row[x+1] == owner {
				x++
			}
			if owner != 0 {
				rowSpans[owner] = append(rowSpans[owner], span{left: start, right: x})
			}
			x++
		}
		for owner, spans := range rowSpans {
			if active[owner] == nil {
				active[owner] = make(map[span]int)
			}
			keep := make(map[span]struct{}, len(spans))
			for _, sp := range spans {
				keep[sp] = struct{}{}
				if _, ok := active[owner][sp]; !ok {
					active[owner][sp] = y
				}
			}
			flushMissing(owner, keep, y)
		}
		for owner := range active {
			if _, ok := rowSpans[owner]; ok {
				continue
			}
			flushMissing(owner, nil, y)
		}
	}
	lastRow := len(ownerMap) - 1
	for owner, spans := range active {
		for sp, top := range spans {
			result[owner] = append(result[owner], rect{
				Left:   sp.left,
				Top:    top,
				Right:  sp.right,
				Bottom: lastRow,
			})
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}
