package app

import (
	"strings"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/perftrace"
	"github.com/lozzow/termx/tuiv2/shared"
)

type hostCell struct {
	Content                 string
	Width                   int
	Style                   presentedStyle
	Erase                   bool
	Wide                    bool
	Continuation            bool
	HiddenEmojiCompensation bool
	HostWidthStabilizer     bool
	Owner                   hostOwnerID
}

type hostFrame struct {
	Width  int
	Height int
	Cells  [][]hostCell
}

type damageRect struct {
	rect
	OwnerKnown  bool
	Owner       hostOwnerID
	FullWidth   bool
	StableOwner bool
}

type rectScrollPlan struct {
	rect
	Owner      hostOwnerID
	DeltaY     int
	ReusedArea int
	Exposed    []rect
}

func (p *framePresenter) presentOwnerAwareDelta(lines []string, meta *presentMeta) string {
	if p == nil || !p.fullWidthLines || meta == nil || p.meta == nil {
		return ""
	}
	previousRows := make([]presentedRow, len(p.lines))
	nextRows := make([]presentedRow, len(lines))
	for i := range p.lines {
		previousRows[i] = p.presentedRow(i)
	}
	for i := range lines {
		nextRows[i] = parsePresentedRow(lines[i])
	}
	defer releasePresentedRows(nextRows)

	previous, ok := buildHostFrame(previousRows, p.meta)
	if !ok {
		return ""
	}
	next, ok := buildHostFrame(nextRows, meta)
	if !ok {
		return ""
	}
	rects := collectOwnerAwareDamageRects(previous, next, p.meta, meta)
	if len(rects) == 0 {
		return ""
	}
	var out strings.Builder
	usedScroll := false
	for _, damage := range rects {
		if plan, ok := detectRectScroll(previous, next, damage); ok && canUseRectScroll(previous, next, damage, plan) {
			if damage.FullWidth {
				emitFullWidthRectScroll(&out, next, plan)
				perftrace.Count("cursor_writer.present.mode.delta_rect_scroll_fullwidth", plan.ReusedArea)
				usedScroll = true
				continue
			}
			if shared.ExperimentalLRScrollEnabled() {
				emitLRMarginRectScroll(&out, next, plan)
				perftrace.Count("cursor_writer.present.mode.delta_rect_scroll_lr_margin", plan.ReusedArea)
				usedScroll = true
				continue
			}
		}
		emitHostRectRowDiff(&out, previous, next, damage, lines)
	}
	payload := out.String()
	if payload == "" {
		return ""
	}
	fullLen := joinedLinesLen(lines)
	if !usedScroll && fullLen > 0 && len(payload)*100 >= fullLen*95 {
		return ""
	}
	perftrace.Count("cursor_writer.present.mode.owner_aware_rects", len(rects))
	return payload
}

func buildHostFrame(rows []presentedRow, meta *presentMeta) (hostFrame, bool) {
	if meta == nil || len(rows) == 0 || len(rows) != len(meta.OwnerMap) {
		return hostFrame{}, false
	}
	width := 0
	for _, row := range meta.OwnerMap {
		if len(row) > width {
			width = len(row)
		}
	}
	if width <= 0 {
		return hostFrame{}, false
	}
	frame := hostFrame{
		Width:  width,
		Height: len(rows),
		Cells:  make([][]hostCell, len(rows)),
	}
	for y := range rows {
		cells, ok := flattenPresentedRowToHostCells(rows[y], width, meta.OwnerMap[y])
		if !ok {
			return hostFrame{}, false
		}
		frame.Cells[y] = cells
	}
	return frame, true
}

func flattenPresentedRowToHostCells(row presentedRow, width int, owners []hostOwnerID) ([]hostCell, bool) {
	if len(owners) < width {
		return nil, false
	}
	out := make([]hostCell, width)
	for x := 0; x < width; x++ {
		out[x] = hostCell{
			Content:                 " ",
			Width:                   1,
			Owner:                   owners[x],
			HiddenEmojiCompensation: row.hasHiddenEmojiCompensation,
			HostWidthStabilizer:     row.hasHostWidthStabilizer,
		}
	}
	col := 0
	for _, cell := range row.cells {
		w := maxInt(1, cell.Width)
		if col >= width {
			break
		}
		if col+w > width {
			return nil, false
		}
		out[col] = hostCell{
			Content:                 cell.Content,
			Width:                   w,
			Style:                   cell.Style,
			Erase:                   cell.Erase,
			Wide:                    w > 1,
			Owner:                   owners[col],
			HiddenEmojiCompensation: row.hasHiddenEmojiCompensation,
			HostWidthStabilizer:     row.hasHostWidthStabilizer || cell.ReanchorBefore,
		}
		if out[col].Content == "" {
			out[col].Content = " "
		}
		for i := 1; i < w; i++ {
			out[col+i] = hostCell{
				Content:                 " ",
				Width:                   1,
				Style:                   cell.Style,
				Owner:                   owners[col+i],
				Continuation:            true,
				HiddenEmojiCompensation: row.hasHiddenEmojiCompensation,
				HostWidthStabilizer:     row.hasHostWidthStabilizer || cell.ReanchorBefore,
			}
		}
		col += w
	}
	return out, true
}

func collectOwnerAwareDamageRects(previous, next hostFrame, previousMeta, nextMeta *presentMeta) []damageRect {
	if previous.Width != next.Width || previous.Height != next.Height || previousMeta == nil || nextMeta == nil {
		return nil
	}
	dirtyMask := make([][]bool, next.Height)
	dirtyOwners := make(map[hostOwnerID]struct{})
	for y := 0; y < next.Height; y++ {
		dirtyMask[y] = make([]bool, next.Width)
		for x := 0; x < next.Width; x++ {
			if hostCellsVisualEqual(previous.Cells[y][x], next.Cells[y][x]) {
				continue
			}
			markDamageCell(dirtyMask, previous, next, x, y)
			if owner := previous.Cells[y][x].Owner; owner != 0 {
				dirtyOwners[owner] = struct{}{}
			}
			if owner := next.Cells[y][x].Owner; owner != 0 {
				dirtyOwners[owner] = struct{}{}
			}
		}
	}
	if len(dirtyOwners) == 0 {
		return nil
	}
	seeds := make([]struct {
		owner hostOwnerID
		rect  rect
	}, 0, len(dirtyOwners)*4)
	for owner := range dirtyOwners {
		for _, current := range previousMeta.VisibleRects[owner] {
			seeds = append(seeds, struct {
				owner hostOwnerID
				rect  rect
			}{owner: owner, rect: current})
		}
		for _, current := range nextMeta.VisibleRects[owner] {
			seeds = append(seeds, struct {
				owner hostOwnerID
				rect  rect
			}{owner: owner, rect: current})
		}
	}
	if len(seeds) == 0 {
		return nil
	}
	rects := make([]damageRect, 0, len(seeds))
	for _, seed := range seeds {
		for _, current := range dirtyMaskToRectsInBounds(dirtyMask, seed.rect) {
			stable := rectStableForOwner(previousMeta.OwnerMap, nextMeta.OwnerMap, seed.owner, current)
			rects = append(rects, damageRect{
				rect:        current,
				OwnerKnown:  true,
				Owner:       seed.owner,
				FullWidth:   current.Left == 0 && current.Right == next.Width-1,
				StableOwner: stable,
			})
		}
	}
	return dedupeDamageRects(rects)
}

func markDamageCell(mask [][]bool, previous, next hostFrame, x, y int) {
	for dy := maxInt(0, y-1); dy <= minInt(len(mask)-1, y+1); dy++ {
		for dx := maxInt(0, x-1); dx <= minInt(len(mask[dy])-1, x+1); dx++ {
			mask[dy][dx] = true
		}
	}
	if previous.Cells[y][x].Wide || next.Cells[y][x].Wide || previous.Cells[y][x].Continuation || next.Cells[y][x].Continuation {
		if x > 0 {
			mask[y][x-1] = true
		}
		mask[y][x] = true
		if x+1 < len(mask[y]) {
			mask[y][x+1] = true
		}
	}
}

func dirtyMaskToRectsInBounds(mask [][]bool, bounds rect) []rect {
	type span struct {
		left  int
		right int
	}
	active := make(map[span]int)
	var out []rect
	flush := func(row int, keep map[span]struct{}) {
		for sp, top := range active {
			if keep != nil {
				if _, ok := keep[sp]; ok {
					continue
				}
			}
			out = append(out, rect{Left: sp.left, Top: top, Right: sp.right, Bottom: row - 1})
			delete(active, sp)
		}
	}
	for y := bounds.Top; y <= bounds.Bottom; y++ {
		rowKeep := make(map[span]struct{})
		for x := bounds.Left; x <= bounds.Right; {
			if !mask[y][x] {
				x++
				continue
			}
			start := x
			for x+1 <= bounds.Right && mask[y][x+1] {
				x++
			}
			sp := span{left: start, right: x}
			rowKeep[sp] = struct{}{}
			if _, ok := active[sp]; !ok {
				active[sp] = y
			}
			x++
		}
		flush(y, rowKeep)
	}
	flush(bounds.Bottom+1, nil)
	return out
}

func dedupeDamageRects(rects []damageRect) []damageRect {
	if len(rects) == 0 {
		return nil
	}
	seen := make(map[damageRect]struct{}, len(rects))
	out := make([]damageRect, 0, len(rects))
	for _, current := range rects {
		if current.Right < current.Left || current.Bottom < current.Top {
			continue
		}
		if _, ok := seen[current]; ok {
			continue
		}
		seen[current] = struct{}{}
		out = append(out, current)
	}
	return out
}

func rectStableForOwner(previous, next [][]hostOwnerID, owner hostOwnerID, bounds rect) bool {
	for y := bounds.Top; y <= bounds.Bottom; y++ {
		for x := bounds.Left; x <= bounds.Right; x++ {
			if previous[y][x] != owner || next[y][x] != owner {
				return false
			}
		}
	}
	return true
}

func detectRectScroll(previous, next hostFrame, damage damageRect) (rectScrollPlan, bool) {
	height := damage.Bottom - damage.Top + 1
	width := damage.Right - damage.Left + 1
	if height < 6 || width < 4 {
		return rectScrollPlan{}, false
	}
	best := rectScrollPlan{}
	maxShift := minInt(12, height/2)
	for dy := -maxShift; dy <= maxShift; dy++ {
		if dy == 0 {
			continue
		}
		reused := 0
		for dstY := damage.Top; dstY <= damage.Bottom; dstY++ {
			srcY := dstY + dy
			if dy > 0 {
				srcY = dstY + dy
			} else {
				srcY = dstY + dy
			}
			if srcY < damage.Top || srcY > damage.Bottom {
				continue
			}
			if hostCellSlicesEqual(previous.Cells[srcY][damage.Left:damage.Right+1], next.Cells[dstY][damage.Left:damage.Right+1]) {
				reused += width
			}
		}
		if reused <= best.ReusedArea {
			continue
		}
		best = rectScrollPlan{
			rect:       damage.rect,
			Owner:      damage.Owner,
			DeltaY:     dy,
			ReusedArea: reused,
			Exposed:    exposedRectsForRectScroll(damage.rect, dy),
		}
	}
	if best.ReusedArea*100 < width*height*70 {
		return rectScrollPlan{}, false
	}
	return best, best.ReusedArea > 0
}

func exposedRectsForRectScroll(bounds rect, deltaY int) []rect {
	if deltaY > 0 {
		return []rect{{
			Left:   bounds.Left,
			Top:    bounds.Bottom - deltaY + 1,
			Right:  bounds.Right,
			Bottom: bounds.Bottom,
		}}
	}
	return []rect{{
		Left:   bounds.Left,
		Top:    bounds.Top,
		Right:  bounds.Right,
		Bottom: bounds.Top - deltaY - 1,
	}}
}

func canUseRectScroll(previous, next hostFrame, damage damageRect, plan rectScrollPlan) bool {
	if plan.ReusedArea <= 0 || !damage.StableOwner {
		return false
	}
	if rectHasUnsafeCells(previous, damage.rect) || rectHasUnsafeCells(next, damage.rect) {
		return false
	}
	if !damage.FullWidth && !shared.ExperimentalLRScrollEnabled() {
		return false
	}
	return true
}

func rectHasUnsafeCells(frame hostFrame, bounds rect) bool {
	for y := bounds.Top; y <= bounds.Bottom; y++ {
		for x := bounds.Left; x <= bounds.Right; x++ {
			cell := frame.Cells[y][x]
			if cell.Wide || cell.Continuation || cell.HiddenEmojiCompensation || cell.HostWidthStabilizer || cell.Erase {
				return true
			}
		}
	}
	return false
}

func emitFullWidthRectScroll(out *strings.Builder, next hostFrame, plan rectScrollPlan) {
	writeCSI(out, 'r', plan.Top+1, plan.Bottom+1)
	writeCUP(out, 1, plan.Top+1)
	if plan.DeltaY > 0 {
		writeCSI(out, 'S', plan.DeltaY)
	} else {
		writeCSI(out, 'T', -plan.DeltaY)
	}
	for _, exposed := range plan.Exposed {
		emitRectRepaint(out, next, exposed)
	}
	out.WriteString("\x1b[r")
}

func emitLRMarginRectScroll(out *strings.Builder, next hostFrame, plan rectScrollPlan) {
	out.WriteString(xansi.SaveCursor)
	out.WriteString(xansi.SetModeLeftRightMargin)
	out.WriteString(xansi.DECSLRM(plan.Left+1, plan.Right+1))
	writeCSI(out, 'r', plan.Top+1, plan.Bottom+1)
	writeCUP(out, plan.Left+1, plan.Top+1)
	if plan.DeltaY > 0 {
		writeCSI(out, 'S', plan.DeltaY)
	} else {
		writeCSI(out, 'T', -plan.DeltaY)
	}
	for _, exposed := range plan.Exposed {
		emitRectRepaint(out, next, exposed)
	}
	out.WriteString("\x1b[r")
	out.WriteString(xansi.ResetModeLeftRightMargin)
	out.WriteString(xansi.RestoreCursor)
}

func emitRectRepaint(out *strings.Builder, next hostFrame, bounds rect) {
	for y := bounds.Top; y <= bounds.Bottom; y++ {
		writeCUP(out, bounds.Left+1, y+1)
		writeHostCells(out, next.Cells[y][bounds.Left:bounds.Right+1], bounds.Left+1)
	}
}

func emitHostRectRowDiff(out *strings.Builder, previous, next hostFrame, damage damageRect, nextLines []string) {
	for y := damage.Top; y <= damage.Bottom; y++ {
		prevSeg := previous.Cells[y][damage.Left : damage.Right+1]
		nextSeg := next.Cells[y][damage.Left : damage.Right+1]
		if hostCellSlicesEqual(prevSeg, nextSeg) {
			continue
		}
		if segmentUnsafe(prevSeg) || segmentUnsafe(nextSeg) {
			start, end := expandUnsafeSegment(next.Cells[y], damage.Left, damage.Right)
			writeCUP(out, start+1, y+1)
			writeHostCells(out, next.Cells[y][start:end+1], start+1)
			if start == 0 && end == len(next.Cells[y])-1 {
				out.WriteString(xansi.EraseLineRight)
			}
			continue
		}
		for i := 0; i < len(nextSeg); {
			if hostCellsVisualEqual(prevSeg[i], nextSeg[i]) {
				i++
				continue
			}
			start := i
			for i < len(nextSeg) && !hostCellsVisualEqual(prevSeg[i], nextSeg[i]) {
				i++
			}
			writeCUP(out, damage.Left+start+1, y+1)
			writeHostCells(out, nextSeg[start:i], damage.Left+start+1)
		}
		_ = nextLines
	}
}

func expandUnsafeSegment(row []hostCell, left, right int) (int, int) {
	for left > 0 && row[left].Continuation {
		left--
	}
	for right+1 < len(row) && row[right+1].Continuation {
		right++
	}
	return left, right
}

func segmentUnsafe(cells []hostCell) bool {
	for _, cell := range cells {
		if cell.Wide || cell.Continuation || cell.HiddenEmojiCompensation || cell.HostWidthStabilizer {
			return true
		}
	}
	return false
}

func writeHostCells(out *strings.Builder, cells []hostCell, startCol int) {
	current := presentedStyle{}
	first := true
	cursorCol := startCol
	needsReanchor := false
	for _, cell := range cells {
		if cell.Continuation {
			cursorCol++
			continue
		}
		if needsReanchor {
			writeCHA(out, cursorCol)
			needsReanchor = false
		}
		if first || cell.Style != current {
			out.WriteString(presentedStyleDiffANSI(current, cell.Style))
			current = cell.Style
			first = false
		}
		if cell.Erase {
			writeECH(out, maxInt(1, cell.Width))
			cursorCol += maxInt(1, cell.Width)
			needsReanchor = true
			continue
		}
		content := cell.Content
		if content == "" {
			content = " "
		}
		out.WriteString(content)
		cursorCol += maxInt(1, cell.Width)
	}
	if current != (presentedStyle{}) {
		out.WriteString(presentedResetStyleSequence)
	}
}

func hostCellsVisualEqual(previous, next hostCell) bool {
	return previous.Content == next.Content &&
		previous.Width == next.Width &&
		previous.Style == next.Style &&
		previous.Erase == next.Erase &&
		previous.Wide == next.Wide &&
		previous.Continuation == next.Continuation &&
		previous.HiddenEmojiCompensation == next.HiddenEmojiCompensation &&
		previous.HostWidthStabilizer == next.HostWidthStabilizer
}

func hostCellSlicesEqual(previous, next []hostCell) bool {
	if len(previous) != len(next) {
		return false
	}
	for i := range previous {
		if !hostCellsVisualEqual(previous[i], next[i]) {
			return false
		}
	}
	return true
}
