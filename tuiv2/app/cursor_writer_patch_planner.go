package app

import (
	"strings"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/perftrace"
)

type framePatchCandidateMode uint8

const (
	framePatchCandidateNone framePatchCandidateMode = iota
	framePatchCandidateDiff
	framePatchCandidateOwnerAware
	framePatchCandidateVerticalScrollRows
	framePatchCandidateVerticalScrollRect
	framePatchCandidateFullRepaint
)

type framePatchMetric struct {
	name  string
	count int
}

type framePatchCandidate struct {
	mode         framePatchCandidateMode
	payload      string
	faultPayload string
	metrics      []framePatchMetric

	changedCount         int
	updatedCount         int
	updates              []presentedRowUpdate
	reclaim              [][]presentedCell
	baselineChangedCount int
	baselineUpdates      []presentedRowUpdate
	baselineReclaim      [][]presentedCell
}

func (c framePatchCandidate) valid() bool {
	return c.mode != framePatchCandidateNone
}

func (c framePatchCandidate) byteCost() int {
	return normalizedFrameLen(c.payload)
}

func betterFramePatchCandidate(candidate, current framePatchCandidate) bool {
	if !candidate.valid() {
		return false
	}
	if !current.valid() {
		return true
	}
	if candidate.byteCost() != current.byteCost() {
		return candidate.byteCost() < current.byteCost()
	}
	return candidate.mode == framePatchCandidateDiff && current.mode != framePatchCandidateDiff
}

func emitFramePatchMetrics(metrics []framePatchMetric) {
	for _, metric := range metrics {
		perftrace.Count(metric.name, metric.count)
	}
}

func releaseDiscardedPresentedRowUpdates(updates []presentedRowUpdate) {
	for _, update := range updates {
		if !update.replace {
			continue
		}
		releasePresentedCells(update.parsed.cells)
	}
}

func (p *framePresenter) selectedFramePatchPayload(candidate framePatchCandidate) string {
	if p == nil || candidate.mode != framePatchCandidateVerticalScrollRows {
		return candidate.payload
	}
	p.verticalScrollCount++
	if p.debugFaultScrollDropRemainderEvery > 0 && candidate.faultPayload != "" && p.verticalScrollCount%p.debugFaultScrollDropRemainderEvery == 0 {
		return candidate.faultPayload
	}
	return candidate.payload
}

func (p *framePresenter) diffPatchCandidate(lines []string) framePatchCandidate {
	if p == nil {
		return framePatchCandidate{}
	}
	payload, changedCount, updatedCount, updates, reclaim := p.renderChangedRows(lines)
	return framePatchCandidate{
		mode:                 framePatchCandidateDiff,
		payload:              payload,
		changedCount:         changedCount,
		updatedCount:         updatedCount,
		updates:              updates,
		reclaim:              reclaim,
		baselineChangedCount: changedCount,
		baselineUpdates:      updates,
		baselineReclaim:      reclaim,
	}
}

func (p *framePresenter) planFramePatch(lines []string, meta *presentMeta) framePatchCandidate {
	diff := p.diffPatchCandidate(lines)
	if diff.updatedCount == 0 {
		return diff
	}
	best := diff
	fullFrame := strings.Join(lines, "\n")
	fullWireLen := normalizedFrameLen(fullFrame)
	if shouldFallbackToFullRepaint(diff.payload, fullWireLen, len(lines), diff.changedCount) {
		full := framePatchCandidate{
			mode:         framePatchCandidateFullRepaint,
			payload:      xansi.EraseEntireDisplay + fullFrame,
			updatedCount: diff.updatedCount,
			metrics: []framePatchMetric{
				{name: "cursor_writer.diff_full_repaint_fallback", count: fullWireLen},
				{name: "cursor_writer.present.mode.full_repaint_threshold", count: fullWireLen},
			},
		}
		if betterFramePatchCandidate(full, best) {
			best = full
		}
	}
	if p.ownerAwareDeltaEnabled && p.fullWidthLines && meta != nil && p.meta != nil && (shouldUseOwnerAwareDelta(meta) || shouldUseOwnerAwareDelta(p.meta)) {
		if candidate := p.ownerAwareDeltaCandidate(lines, meta); betterFramePatchCandidate(candidate, best) {
			best = candidate
		}
	}
	if p.verticalScrollMode.Enabled() {
		if candidate := p.verticalScrollCandidate(lines); betterFramePatchCandidate(candidate, best) {
			best = candidate
		}
	}
	if best.mode != framePatchCandidateDiff {
		best.updatedCount = diff.updatedCount
		best.baselineChangedCount = diff.changedCount
		best.baselineUpdates = diff.updates
		best.baselineReclaim = diff.reclaim
	}
	return best
}
