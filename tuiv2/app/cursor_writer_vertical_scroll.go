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
