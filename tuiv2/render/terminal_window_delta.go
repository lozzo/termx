package render

import "sort"

type terminalWindowDeltaPlan struct {
	scrollPlan   terminalWindowScrollPlan
	changedRows  []int
	explicitHint bool
}

type terminalWindowScrollDirection uint8

const (
	terminalWindowScrollNone terminalWindowScrollDirection = iota
	terminalWindowScrollUp
	terminalWindowScrollDown
)

type terminalWindowScrollPlan struct {
	direction terminalWindowScrollDirection
	start     int
	end       int
	shift     int
	reused    int
}

func planTerminalWindowDelta(previous, next terminalSourceWindowState, hint terminalScreenUpdateHint) terminalWindowDeltaPlan {
	if plan, ok := detectTerminalWindowScroll(previous, next); ok {
		return terminalWindowDeltaPlan{
			scrollPlan:  plan,
			changedRows: terminalWindowChangedRows(previous, next, plan),
		}
	}

	baseChangedRows := terminalWindowChangedRows(previous, next, terminalWindowScrollPlan{})
	if len(baseChangedRows) == 0 {
		return terminalWindowDeltaPlan{}
	}
	if plan, ok := detectTerminalWindowPartialScroll(previous, next, len(baseChangedRows)); ok {
		return terminalWindowDeltaPlan{
			scrollPlan:  plan,
			changedRows: terminalWindowChangedRows(previous, next, plan),
		}
	}
	if hintedRows, ok := explicitTerminalWindowChangedRowsHint(previous, next, hint); ok && len(hintedRows) > 0 && len(hintedRows) < len(baseChangedRows) {
		return terminalWindowDeltaPlan{
			changedRows:  append([]int(nil), hintedRows...),
			explicitHint: true,
		}
	}
	return terminalWindowDeltaPlan{changedRows: append([]int(nil), baseChangedRows...)}
}

func explicitTerminalWindowChangedRowsHint(previous, next terminalSourceWindowState, hint terminalScreenUpdateHint) ([]int, bool) {
	if !compatibleTerminalWindowStates(previous, next) || !previous.screenWindow || !next.screenWindow {
		return nil, false
	}
	if hint.FullReplace || hint.ScreenScroll != 0 || len(hint.ChangedRows) == 0 {
		return nil, false
	}
	height := len(next.exactRowHashes)
	changedRows := make([]int, 0, len(hint.ChangedRows))
	seen := make(map[int]struct{}, len(hint.ChangedRows))
	for _, line := range hint.ChangedRows {
		if line < 0 || line >= height {
			return nil, false
		}
		if _, ok := seen[line]; ok {
			continue
		}
		seen[line] = struct{}{}
		changedRows = append(changedRows, line)
	}
	if len(changedRows) == 0 {
		return nil, false
	}
	sort.Ints(changedRows)
	return changedRows, true
}

func detectTerminalWindowScroll(previous, next terminalSourceWindowState) (terminalWindowScrollPlan, bool) {
	if !compatibleTerminalWindowStates(previous, next) {
		return terminalWindowScrollPlan{}, false
	}
	height := len(next.exactRowHashes)
	for shift := 1; shift < height; shift++ {
		if terminalWindowMatchesScrollUp(previous, next, shift) {
			return terminalWindowScrollPlan{
				direction: terminalWindowScrollUp,
				start:     0,
				end:       height - 1,
				shift:     shift,
				reused:    height - shift,
			}, true
		}
		if terminalWindowMatchesScrollDown(previous, next, shift) {
			return terminalWindowScrollPlan{
				direction: terminalWindowScrollDown,
				start:     0,
				end:       height - 1,
				shift:     shift,
				reused:    height - shift,
			}, true
		}
	}
	return terminalWindowScrollPlan{}, false
}

func detectTerminalWindowPartialScroll(previous, next terminalSourceWindowState, totalChangedRows int) (terminalWindowScrollPlan, bool) {
	if !compatibleTerminalWindowStates(previous, next) || !previous.screenWindow || !next.screenWindow {
		return terminalWindowScrollPlan{}, false
	}
	best := terminalWindowScrollPlan{}
	height := len(next.exactRowHashes)
	for shift := 1; shift < height; shift++ {
		scanTerminalWindowScrollRuns(previous, next, shift, terminalWindowScrollUp, &best)
		scanTerminalWindowScrollRuns(previous, next, shift, terminalWindowScrollDown, &best)
	}
	if !partialTerminalWindowScrollWorthIt(previous, next, best, totalChangedRows) {
		return terminalWindowScrollPlan{}, false
	}
	return best, true
}

func compatibleTerminalWindowStates(previous, next terminalSourceWindowState) bool {
	return len(previous.exactRowHashes) > 0 &&
		len(previous.exactRowHashes) == len(next.exactRowHashes) &&
		len(previous.rowContentHashes) == len(next.rowContentHashes) &&
		len(previous.rowIndices) == len(next.rowIndices) &&
		len(previous.rowIdentityHashes) == len(next.rowIdentityHashes)
}

func terminalWindowMatchesScrollUp(previous, next terminalSourceWindowState, shift int) bool {
	height := len(next.exactRowHashes)
	scrollUp := true
	for line := 0; line+shift < height; line++ {
		if previous.rowIndices[line+shift] != next.rowIndices[line] || previous.exactRowHashes[line+shift] != next.exactRowHashes[line] {
			scrollUp = false
			break
		}
	}
	if scrollUp {
		return true
	}
	if !previous.screenWindow || !next.screenWindow {
		return false
	}
	for line := 0; line+shift < height; line++ {
		if previous.rowIndices[line+shift] < 0 || next.rowIndices[line] < 0 || previous.rowIdentityHashes[line+shift] != next.rowIdentityHashes[line] {
			return false
		}
	}
	return true
}

func terminalWindowMatchesScrollDown(previous, next terminalSourceWindowState, shift int) bool {
	height := len(next.exactRowHashes)
	scrollDown := true
	for line := 0; line+shift < height; line++ {
		if previous.rowIndices[line] != next.rowIndices[line+shift] || previous.exactRowHashes[line] != next.exactRowHashes[line+shift] {
			scrollDown = false
			break
		}
	}
	if scrollDown {
		return true
	}
	if !previous.screenWindow || !next.screenWindow {
		return false
	}
	for line := 0; line+shift < height; line++ {
		if previous.rowIndices[line] < 0 || next.rowIndices[line+shift] < 0 || previous.rowIdentityHashes[line] != next.rowIdentityHashes[line+shift] {
			return false
		}
	}
	return true
}

func scanTerminalWindowScrollRuns(previous, next terminalSourceWindowState, shift int, direction terminalWindowScrollDirection, best *terminalWindowScrollPlan) {
	if best == nil {
		return
	}
	runStart := -1
	runLength := 0
	flush := func(endExclusive int) {
		if runLength == 0 {
			return
		}
		candidate := terminalWindowScrollPlan{
			direction: direction,
			start:     runStart,
			end:       endExclusive + shift - 1,
			shift:     shift,
			reused:    runLength,
		}
		if betterTerminalWindowScrollPlan(candidate, *best) {
			*best = candidate
		}
		runStart = -1
		runLength = 0
	}

	limit := len(previous.rowIdentityHashes) - shift
	for i := 0; i < limit; i++ {
		var previousLine int
		var nextLine int
		switch direction {
		case terminalWindowScrollUp:
			previousLine = i + shift
			nextLine = i
		case terminalWindowScrollDown:
			previousLine = i
			nextLine = i + shift
		default:
			return
		}
		matches := previous.rowIndices[previousLine] >= 0 &&
			next.rowIndices[nextLine] >= 0 &&
			previous.rowIdentityHashes[previousLine] == next.rowIdentityHashes[nextLine]
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

func betterTerminalWindowScrollPlan(candidate, current terminalWindowScrollPlan) bool {
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

func partialTerminalWindowScrollWorthIt(previous, next terminalSourceWindowState, plan terminalWindowScrollPlan, totalChangedRows int) bool {
	if plan.direction == terminalWindowScrollNone || plan.reused < 4 || totalChangedRows <= 0 {
		return false
	}
	residualRedrawRows := len(terminalWindowChangedRows(previous, next, plan))
	if residualRedrawRows <= 0 || residualRedrawRows >= totalChangedRows {
		return false
	}
	return totalChangedRows-residualRedrawRows >= 2
}

func terminalWindowChangedRows(previous, next terminalSourceWindowState, plan terminalWindowScrollPlan) []int {
	if !compatibleTerminalWindowStates(previous, next) {
		return nil
	}
	changedRows := make([]int, 0, len(next.exactRowHashes))
	for line := range next.exactRowHashes {
		if sourceLine, ok := plan.sourceLineFor(line); ok {
			if sourceLine >= 0 && sourceLine < len(previous.rowContentHashes) && previous.rowContentHashes[sourceLine] == next.rowContentHashes[line] {
				continue
			}
			changedRows = append(changedRows, line)
			continue
		}
		if previous.exactRowHashes[line] == next.exactRowHashes[line] {
			continue
		}
		changedRows = append(changedRows, line)
	}
	return changedRows
}

func (p terminalWindowScrollPlan) valid(height int) bool {
	if p.direction == terminalWindowScrollNone || p.shift <= 0 || p.reused <= 0 || height <= 0 {
		return false
	}
	if p.start < 0 || p.end < p.start || p.end >= height {
		return false
	}
	return p.end-p.start+1 > p.shift
}

func (p terminalWindowScrollPlan) wholeWindow(height int) bool {
	return p.valid(height) && p.start == 0 && p.end == height-1
}

func (p terminalWindowScrollPlan) sourceLineFor(line int) (int, bool) {
	if p.direction == terminalWindowScrollNone || line < 0 {
		return 0, false
	}
	switch p.direction {
	case terminalWindowScrollUp:
		if line < p.start || line > p.end-p.shift {
			return 0, false
		}
		return line + p.shift, true
	case terminalWindowScrollDown:
		if line < p.start+p.shift || line > p.end {
			return 0, false
		}
		return line - p.shift, true
	default:
		return 0, false
	}
}
