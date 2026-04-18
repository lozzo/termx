package app

import (
	"strings"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/perftrace"
)

const maxIntralineShiftCount = 12

type intralineShiftOp struct {
	col   int
	count int
}

type intralineEraseOp struct {
	col        int
	count      int
	style      presentedStyle
	useLineEnd bool
}

func renderChangedRowIntralineEdit(out *strings.Builder, previous, next presentedRow, row int, fullWidthLines bool, ownsLineEnd bool) bool {
	if !safeForIntralineEdits(previous, next, fullWidthLines, ownsLineEnd) {
		return false
	}
	if previous.hasStyled || next.hasStyled {
		return false
	}
	if fullWidthLines {
		if op, ok := detectPresentedRowDelete(previous.cells, next.cells); ok {
			writeCUP(out, op.col+1, row+1)
			writeDCH(out, op.count)
			if tailStart := len(next.cells) - op.count; tailStart >= 0 && tailStart < len(next.cells) {
				writeCUP(out, tailStart+1, row+1)
				writePresentedCells(out, next.cells[tailStart:], tailStart+1)
			}
			perftrace.Count("cursor_writer.present.mode.delta_intraline_dch", op.count)
			return true
		}
		if op, ok := detectPresentedRowInsert(previous.cells, next.cells); ok {
			writeCUP(out, op.col+1, row+1)
			writeICH(out, op.count)
			writePresentedCells(out, next.cells[op.col:op.col+op.count], op.col+1)
			perftrace.Count("cursor_writer.present.mode.delta_intraline_ich", op.count)
			return true
		}
	}
	if op, ok := detectPresentedRowErase(previous.cells, next.cells, fullWidthLines, ownsLineEnd); ok {
		writeCUP(out, op.col+1, row+1)
		if op.style != (presentedStyle{}) {
			out.WriteString(presentedStyleDiffANSI(presentedStyle{}, op.style))
		}
		if op.useLineEnd {
			writeEL(out, 0)
			perftrace.Count("cursor_writer.present.mode.delta_intraline_el", op.count)
		} else {
			writeECH(out, op.count)
			perftrace.Count("cursor_writer.present.mode.delta_intraline_ech", op.count)
		}
		if op.style != (presentedStyle{}) {
			out.WriteString(presentedResetStyleSequence)
		}
		return true
	}
	return false
}

func safeForIntralineEdits(previous, next presentedRow, fullWidthLines bool, ownsLineEnd bool) bool {
	if previous.hasWide || next.hasWide ||
		previous.hasErase || next.hasErase ||
		previous.hasHiddenEmojiCompensation || next.hasHiddenEmojiCompensation ||
		previous.hasHostWidthStabilizer || next.hasHostWidthStabilizer {
		return false
	}
	if !fullWidthLines && !ownsLineEnd && !rowOwnsLineEnd(previous) {
		return false
	}
	return singleWidthStablePresentedCells(previous.cells) && singleWidthStablePresentedCells(next.cells)
}

func singleWidthStablePresentedCells(cells []presentedCell) bool {
	for _, cell := range cells {
		if cell.Width != 1 || cell.ReanchorBefore || cell.Erase {
			return false
		}
	}
	return true
}

func detectPresentedRowDelete(previous, next []presentedCell) (intralineShiftOp, bool) {
	if len(previous) != len(next) || len(previous) < 4 {
		return intralineShiftOp{}, false
	}
	limit := minInt(maxIntralineShiftCount, len(previous)-1)
	best := intralineShiftOp{}
	bestReuse := 0
	for count := 1; count <= limit; count++ {
		prefix := commonPresentedCellPrefix(previous, next)
		if prefix >= len(previous)-count {
			continue
		}
		if !presentedCellSlicesEqual(previous[prefix+count:], next[prefix:len(next)-count]) {
			continue
		}
		reuse := len(previous) - prefix - count
		if reuse <= 0 {
			continue
		}
		if reuse > bestReuse || (reuse == bestReuse && (best.count == 0 || count < best.count)) {
			best = intralineShiftOp{col: prefix, count: count}
			bestReuse = reuse
		}
	}
	return best, bestReuse > 0
}

func detectPresentedRowInsert(previous, next []presentedCell) (intralineShiftOp, bool) {
	if len(previous) != len(next) || len(previous) < 4 {
		return intralineShiftOp{}, false
	}
	limit := minInt(maxIntralineShiftCount, len(previous)-1)
	best := intralineShiftOp{}
	bestReuse := 0
	for count := 1; count <= limit; count++ {
		prefix := commonPresentedCellPrefix(previous, next)
		if prefix >= len(previous)-count {
			continue
		}
		if !presentedCellSlicesEqual(previous[prefix:len(previous)-count], next[prefix+count:]) {
			continue
		}
		reuse := len(previous) - prefix - count
		if reuse <= 0 {
			continue
		}
		if reuse > bestReuse || (reuse == bestReuse && (best.count == 0 || count < best.count)) {
			best = intralineShiftOp{col: prefix, count: count}
			bestReuse = reuse
		}
	}
	return best, bestReuse > 0
}

func detectPresentedRowErase(previous, next []presentedCell, fullWidthLines bool, ownsLineEnd bool) (intralineEraseOp, bool) {
	if len(previous) != len(next) || len(next) == 0 {
		return intralineEraseOp{}, false
	}
	prefix := commonPresentedCellPrefix(previous, next)
	if prefix >= len(next) {
		return intralineEraseOp{}, false
	}
	suffix := commonPresentedCellSuffix(previous[prefix:], next[prefix:])
	end := len(next) - suffix
	if end <= prefix {
		return intralineEraseOp{}, false
	}
	style := next[prefix].Style
	for i := prefix; i < end; i++ {
		if !presentedCellIsBlank(next[i]) || next[i].Style != style {
			return intralineEraseOp{}, false
		}
	}
	return intralineEraseOp{
		col:        prefix,
		count:      end - prefix,
		style:      style,
		useLineEnd: end == len(next) && (fullWidthLines || ownsLineEnd),
	}, true
}

func commonPresentedCellPrefix(previous, next []presentedCell) int {
	limit := minInt(len(previous), len(next))
	prefix := 0
	for prefix < limit && previous[prefix] == next[prefix] {
		prefix++
	}
	return prefix
}

func commonPresentedCellSuffix(previous, next []presentedCell) int {
	limit := minInt(len(previous), len(next))
	suffix := 0
	for suffix < limit && previous[len(previous)-1-suffix] == next[len(next)-1-suffix] {
		suffix++
	}
	return suffix
}

func presentedCellSlicesEqual(previous, next []presentedCell) bool {
	if len(previous) != len(next) {
		return false
	}
	for i := range previous {
		if previous[i] != next[i] {
			return false
		}
	}
	return true
}

func presentedCellIsBlank(cell presentedCell) bool {
	return cell.Content == " " && cell.Width == 1 && !cell.ReanchorBefore && !cell.Erase
}

func shouldFallbackToFullRepaint(payload string, fullLen, totalRows, changedRows int) bool {
	if payload == "" || fullLen <= 0 || totalRows <= 6 || changedRows <= 0 {
		return false
	}
	fragmented := changedRows*3 >= totalRows
	if !fragmented {
		return false
	}
	if len(payload)*100 >= fullLen*95 {
		return true
	}
	return !containsIntralineEdit(payload) && len(payload)*100 >= fullLen*90
}

func shouldCountFullRepaintAvoided(payload string, fullLen, totalRows int) bool {
	if payload == "" || fullLen <= 0 || totalRows <= 6 {
		return false
	}
	return len(payload)*100 >= fullLen*80
}

func containsIntralineEdit(payload string) bool {
	return strings.Contains(payload, xansi.DCH(1)) ||
		strings.Contains(payload, xansi.ICH(1)) ||
		strings.Contains(payload, xansi.ECH(1)) ||
		strings.Contains(payload, xansi.EL(0))
}
