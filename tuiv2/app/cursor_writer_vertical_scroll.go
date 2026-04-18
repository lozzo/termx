package app

import (
	"strings"

	xansi "github.com/charmbracelet/x/ansi"
)

type verticalScrollDirection uint8

const (
	scrollNone verticalScrollDirection = iota
	scrollUp
	scrollDown
)

type verticalScrollPlan struct {
	direction verticalScrollDirection
	start     int
	end       int
	shift     int
	reused    int
}

type verticalScrollRectPlan struct {
	direction verticalScrollDirection
	start     int
	end       int
	shift     int
	reused    int
	left      int
	right     int
}

func detectVerticalScrollPlan(previous, next []string) (verticalScrollPlan, bool) {
	if len(previous) != len(next) || len(previous) < 6 {
		return verticalScrollPlan{}, false
	}
	best := verticalScrollPlan{}
	maxShift := len(previous) / 2
	for shift := 1; shift <= maxShift; shift++ {
		scanVerticalScrollRuns(previous, next, shift, scrollUp, &best)
		scanVerticalScrollRuns(previous, next, shift, scrollDown, &best)
	}
	if best.direction == scrollNone || best.reused < 4 {
		return verticalScrollPlan{}, false
	}
	return best, true
}

func scanVerticalScrollRuns(previous, next []string, shift int, direction verticalScrollDirection, best *verticalScrollPlan) {
	runStart := -1
	runLength := 0
	flush := func(endExclusive int) {
		if runLength == 0 {
			return
		}
		candidate := verticalScrollPlan{
			direction: direction,
			start:     runStart,
			shift:     shift,
			reused:    runLength,
			end:       endExclusive + shift - 1,
		}
		if betterVerticalScrollPlan(candidate, *best) {
			*best = candidate
		}
		runStart = -1
		runLength = 0
	}
	limit := len(previous) - shift
	for i := 0; i < limit; i++ {
		var matches bool
		switch direction {
		case scrollUp:
			matches = next[i] == previous[i+shift]
		case scrollDown:
			matches = next[i+shift] == previous[i]
		default:
			return
		}
		if matches {
			if runLength == 0 {
				runStart = i
			}
			runLength++
			continue
		}
		flush(i)
	}
	flush(limit)
}

func betterVerticalScrollPlan(candidate, current verticalScrollPlan) bool {
	if candidate.reused == 0 {
		return false
	}
	if current.reused == 0 {
		return true
	}
	if candidate.reused != current.reused {
		return candidate.reused > current.reused
	}
	if candidate.shift != current.shift {
		return candidate.shift < current.shift
	}
	if candidate.start != current.start {
		return candidate.start < current.start
	}
	return candidate.direction < current.direction
}

func applyVerticalScrollPlan(lines []string, plan verticalScrollPlan) []string {
	next := append([]string(nil), lines...)
	switch plan.direction {
	case scrollUp:
		copy(next[plan.start:plan.end-plan.shift+1], lines[plan.start+plan.shift:plan.end+1])
		for i := plan.end - plan.shift + 1; i <= plan.end; i++ {
			next[i] = ""
		}
	case scrollDown:
		copy(next[plan.start+plan.shift:plan.end+1], lines[plan.start:plan.end-plan.shift+1])
		for i := plan.start; i < plan.start+plan.shift; i++ {
			next[i] = ""
		}
	}
	return next
}

func renderVerticalScrollPlan(plan verticalScrollPlan, totalLines int) string {
	if plan.direction == scrollNone || plan.shift <= 0 || plan.start < 0 || plan.end >= totalLines {
		return ""
	}
	var out strings.Builder
	writeCSI(&out, 'r', plan.start+1, plan.end+1)
	out.WriteString(xansi.DECRST(xansi.ModeOrigin))
	writeCUP(&out, 1, plan.start+1)
	switch plan.direction {
	case scrollUp:
		// Prefer SU/SD over DL/IL here. Some host terminals are more likely to
		// leave stale rows behind when line insert/delete is used inside a
		// constrained scroll region, while the region-scroll sequences map more
		// directly to the intended "viewport moved by N rows" operation.
		writeCSI(&out, 'S', plan.shift)
	case scrollDown:
		writeCSI(&out, 'T', plan.shift)
	}
	out.WriteString("\x1b[r")
	out.WriteString(xansi.DECRST(xansi.ModeOrigin))
	return out.String()
}

func detectVerticalScrollRectPlan(previous, next []presentedRow) (verticalScrollRectPlan, bool) {
	if len(previous) != len(next) || len(previous) < 6 {
		return verticalScrollRectPlan{}, false
	}
	prevFlat, cols, ok := flattenPresentedRowsForRectScroll(previous)
	if !ok {
		return verticalScrollRectPlan{}, false
	}
	nextFlat, nextCols, ok := flattenPresentedRowsForRectScroll(next)
	if !ok || nextCols != cols {
		return verticalScrollRectPlan{}, false
	}
	best := verticalScrollRectPlan{}
	maxShift := len(previous) / 2
	for shift := 1; shift <= maxShift; shift++ {
		scanVerticalScrollRectRuns(prevFlat, nextFlat, cols, shift, scrollUp, &best)
		scanVerticalScrollRectRuns(prevFlat, nextFlat, cols, shift, scrollDown, &best)
	}
	if best.direction == scrollNone || best.reused < 4 || best.left < 0 || best.right < best.left {
		return verticalScrollRectPlan{}, false
	}
	return best, true
}

func flattenPresentedRowsForRectScroll(rows []presentedRow) ([][]presentedCell, int, bool) {
	if len(rows) == 0 {
		return nil, 0, false
	}
	flats := make([][]presentedCell, len(rows))
	cols := -1
	for i, row := range rows {
		flat, ok := flattenPresentedRowForRectScroll(row)
		if !ok {
			return nil, 0, false
		}
		if cols < 0 {
			cols = len(flat)
		} else if len(flat) != cols {
			return nil, 0, false
		}
		flats[i] = flat
	}
	return flats, cols, cols > 0
}

func flattenPresentedRowForRectScroll(row presentedRow) ([]presentedCell, bool) {
	if row.hasWide || row.hasErase || row.hasHiddenEmojiCompensation || row.hasHostWidthStabilizer {
		return nil, false
	}
	flat := make([]presentedCell, 0, len(row.cells))
	for _, cell := range row.cells {
		if cell.Width != 1 || cell.ReanchorBefore || cell.Erase {
			return nil, false
		}
		flat = append(flat, cell)
	}
	return flat, true
}

func scanVerticalScrollRectRuns(previous, next [][]presentedCell, cols, shift int, direction verticalScrollDirection, best *verticalScrollRectPlan) {
	runStart := -1
	runLength := 0
	left := -1
	right := -1
	flush := func(endExclusive int) {
		if runLength == 0 {
			return
		}
		candidate, ok := buildVerticalScrollRectCandidate(previous, next, cols, runStart, runLength, shift, direction, left, right, endExclusive)
		if ok && betterVerticalScrollRectPlan(candidate, *best) {
			*best = candidate
		}
		runStart = -1
		runLength = 0
		left = -1
		right = -1
	}
	limit := len(previous) - shift
	for i := 0; i < limit; i++ {
		var currentRow, shiftedRow, targetRow []presentedCell
		switch direction {
		case scrollUp:
			currentRow = previous[i]
			shiftedRow = previous[i+shift]
			targetRow = next[i]
		case scrollDown:
			currentRow = previous[i+shift]
			shiftedRow = previous[i]
			targetRow = next[i+shift]
		default:
			return
		}
		startCol, endCol, ok := presentedRowRectShiftSpan(currentRow, shiftedRow, targetRow)
		if !ok {
			flush(i)
			continue
		}
		if runLength == 0 {
			runStart = i
			runLength = 1
			left = startCol
			right = endCol
			continue
		}
		nextLeft := minInt(left, startCol)
		nextRight := maxInt(right, endCol)
		if !presentedRowMatchesRectShift(currentRow, shiftedRow, targetRow, nextLeft, nextRight) {
			flush(i)
			runStart = i
			runLength = 1
			left = startCol
			right = endCol
			continue
		}
		runLength++
		left = nextLeft
		right = nextRight
	}
	flush(limit)
}

func buildVerticalScrollRectCandidate(previous, next [][]presentedCell, cols, runStart, runLength, shift int, direction verticalScrollDirection, left, right, endExclusive int) (verticalScrollRectPlan, bool) {
	if runStart < 0 || runLength <= 0 || shift <= 0 || left < 0 || right < left || cols <= 0 {
		return verticalScrollRectPlan{}, false
	}
	if left == 0 && right == cols-1 {
		return verticalScrollRectPlan{}, false
	}
	width := right - left + 1
	if width < 3 || width >= cols {
		return verticalScrollRectPlan{}, false
	}
	for i := runStart; i < runStart+runLength; i++ {
		var currentRow, shiftedRow, targetRow []presentedCell
		switch direction {
		case scrollUp:
			currentRow = previous[i]
			shiftedRow = previous[i+shift]
			targetRow = next[i]
		case scrollDown:
			currentRow = previous[i+shift]
			shiftedRow = previous[i]
			targetRow = next[i+shift]
		default:
			return verticalScrollRectPlan{}, false
		}
		if !presentedRowMatchesRectShift(currentRow, shiftedRow, targetRow, left, right) {
			return verticalScrollRectPlan{}, false
		}
	}
	return verticalScrollRectPlan{
		direction: direction,
		start:     runStart,
		end:       endExclusive + shift - 1,
		shift:     shift,
		reused:    runLength,
		left:      left,
		right:     right,
	}, true
}

func presentedRowRectShiftSpan(current, shifted, target []presentedCell) (int, int, bool) {
	if len(current) == 0 || len(current) != len(shifted) || len(current) != len(target) {
		return 0, 0, false
	}
	start := -1
	end := -1
	for i := range target {
		if target[i] == current[i] {
			continue
		}
		if target[i] != shifted[i] {
			return 0, 0, false
		}
		if start < 0 {
			start = i
		}
		end = i
	}
	if start < 0 {
		return 0, 0, false
	}
	return start, end, true
}

func presentedRowMatchesRectShift(current, shifted, target []presentedCell, left, right int) bool {
	if len(current) != len(shifted) || len(current) != len(target) {
		return false
	}
	for i := range target {
		if i >= left && i <= right {
			if target[i] != shifted[i] {
				return false
			}
			continue
		}
		if target[i] != current[i] {
			return false
		}
	}
	return true
}

func betterVerticalScrollRectPlan(candidate, current verticalScrollRectPlan) bool {
	if candidate.reused == 0 || candidate.right < candidate.left {
		return false
	}
	if current.reused == 0 || current.right < current.left {
		return true
	}
	candidateArea := candidate.reused * (candidate.right - candidate.left + 1)
	currentArea := current.reused * (current.right - current.left + 1)
	if candidateArea != currentArea {
		return candidateArea > currentArea
	}
	if candidate.reused != current.reused {
		return candidate.reused > current.reused
	}
	if candidate.shift != current.shift {
		return candidate.shift < current.shift
	}
	if candidate.left != current.left {
		return candidate.left < current.left
	}
	if candidate.start != current.start {
		return candidate.start < current.start
	}
	return candidate.direction < current.direction
}

func applyVerticalScrollRectPlan(rows []presentedRow, plan verticalScrollRectPlan) []string {
	flats, _, ok := flattenPresentedRowsForRectScroll(rows)
	if !ok {
		return nil
	}
	after := make([][]presentedCell, len(flats))
	for i, row := range flats {
		after[i] = append([]presentedCell(nil), row...)
	}
	blank := presentedCell{Content: " ", Width: 1}
	switch plan.direction {
	case scrollUp:
		for row := plan.start; row <= plan.end-plan.shift; row++ {
			copy(after[row][plan.left:plan.right+1], flats[row+plan.shift][plan.left:plan.right+1])
		}
		for row := plan.end - plan.shift + 1; row <= plan.end; row++ {
			for col := plan.left; col <= plan.right; col++ {
				after[row][col] = blank
			}
		}
	case scrollDown:
		for row := plan.end; row >= plan.start+plan.shift; row-- {
			copy(after[row][plan.left:plan.right+1], flats[row-plan.shift][plan.left:plan.right+1])
		}
		for row := plan.start; row < plan.start+plan.shift; row++ {
			for col := plan.left; col <= plan.right; col++ {
				after[row][col] = blank
			}
		}
	default:
		return nil
	}
	lines := make([]string, len(after))
	for i := range after {
		lines[i] = serializePresentedFlatRow(after[i])
	}
	return lines
}

func serializePresentedFlatRow(cells []presentedCell) string {
	if len(cells) == 0 {
		return ""
	}
	var out strings.Builder
	writePresentedCells(&out, cells, 1)
	return out.String()
}

func renderVerticalScrollRectPlan(plan verticalScrollRectPlan, totalLines int) string {
	if plan.direction == scrollNone || plan.shift <= 0 || plan.start < 0 || plan.end >= totalLines || plan.left < 0 || plan.right < plan.left {
		return ""
	}
	var out strings.Builder
	out.WriteString(xansi.SaveCursor)
	out.WriteString(xansi.SetModeLeftRightMargin)
	out.WriteString(xansi.DECSLRM(plan.left+1, plan.right+1))
	writeCSI(&out, 'r', plan.start+1, plan.end+1)
	out.WriteString(xansi.DECRST(xansi.ModeOrigin))
	writeCUP(&out, plan.left+1, plan.start+1)
	switch plan.direction {
	case scrollUp:
		writeCSI(&out, 'S', plan.shift)
	case scrollDown:
		writeCSI(&out, 'T', plan.shift)
	}
	out.WriteString("\x1b[r")
	out.WriteString(xansi.ResetModeLeftRightMargin)
	out.WriteString(xansi.DECRST(xansi.ModeOrigin))
	out.WriteString(xansi.RestoreCursor)
	return out.String()
}
