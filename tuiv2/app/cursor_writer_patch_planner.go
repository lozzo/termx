package app

import (
	"strings"
	"time"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/termx-shared/perftrace"
)

type framePatchCandidateMode uint8

const (
	framePatchCandidateNone framePatchCandidateMode = iota
	framePatchCandidateDiff
	framePatchCandidateRawRows
	framePatchCandidateOwnerAware
	framePatchCandidateVerticalScrollRows
	framePatchCandidateVerticalScrollRect
	framePatchCandidateFullRepaint
)

const (
	broadRawRowPatchMinRows                = 8
	broadRawRowPatchMinChangedRows         = 8
	broadRawRowPatchChangedRowsNumerator   = 2
	broadRawRowPatchChangedRowsDenominator = 3
	broadRawRowPatchFullRepaintRowsPercent = 85
	broadRawRowPatchMinPayloadBytes        = 4096
	verticalScrollProbeMinChangedRows      = 4
	verticalScrollProbeMinPayloadBytes     = 20000
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
	renderChangedRowsMs  float64
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
	start := time.Now()
	payload, changedCount, updatedCount, updates, reclaim := p.renderChangedRows(lines)
	elapsed := float64(time.Since(start).Microseconds()) / 1000.0
	return framePatchCandidate{
		mode:                 framePatchCandidateDiff,
		payload:              payload,
		changedCount:         changedCount,
		updatedCount:         updatedCount,
		renderChangedRowsMs:  elapsed,
		updates:              updates,
		reclaim:              reclaim,
		baselineChangedCount: changedCount,
		baselineUpdates:      updates,
		baselineReclaim:      reclaim,
	}
}

func (p *framePresenter) quickFramePatchCandidate(lines []string) framePatchCandidate {
	if p == nil || len(lines) != len(p.lines) {
		return framePatchCandidate{}
	}
	rowDiffPayload, rowChanged := renderChangedRows(p.lines, lines)
	if rowChanged == 0 {
		return framePatchCandidate{
			mode: framePatchCandidateDiff,
		}
	}
	rowDiffBytes := normalizedFrameLen(rowDiffPayload)
	if !p.verticalScrollMode.Enabled() {
		if shouldUseBroadRawRowPatch(len(lines), rowChanged, rowDiffBytes) {
			return broadRawRowPatchCandidate(lines, rowDiffPayload, rowChanged)
		}
		return framePatchCandidate{}
	}
	if !shouldProbeVerticalScroll(len(lines), rowChanged, rowDiffBytes) {
		if shouldUseBroadRawRowPatch(len(lines), rowChanged, rowDiffBytes) {
			return broadRawRowPatchCandidate(lines, rowDiffPayload, rowChanged)
		}
		return framePatchCandidate{}
	}
	candidate := p.verticalScrollRowsCandidate(lines)
	if candidate.valid() {
		if rowDiffBytes > 0 && candidate.byteCost()*2 <= rowDiffBytes {
			candidate.updatedCount = rowChanged
			candidate.metrics = append(candidate.metrics, framePatchMetric{
				name:  "cursor_writer.present.mode.fast_scroll_candidate",
				count: rowChanged,
			})
			return candidate
		}
		if shouldUseBroadRawRowPatch(len(lines), rowChanged, rowDiffBytes) {
			return broadRawRowPatchCandidate(lines, rowDiffPayload, rowChanged)
		}
		return framePatchCandidate{}
	}
	if shouldUseBroadRawRowPatch(len(lines), rowChanged, rowDiffBytes) {
		return broadRawRowPatchCandidate(lines, rowDiffPayload, rowChanged)
	}
	candidate = p.verticalScrollRectCandidate(lines)
	if !candidate.valid() {
		return framePatchCandidate{}
	}
	if rowDiffBytes <= 0 || candidate.byteCost()*2 > rowDiffBytes {
		return framePatchCandidate{}
	}
	candidate.updatedCount = rowChanged
	candidate.metrics = append(candidate.metrics, framePatchMetric{
		name:  "cursor_writer.present.mode.fast_scroll_candidate",
		count: rowChanged,
	})
	return candidate
}

func shouldProbeVerticalScroll(rows, changedRows, payloadBytes int) bool {
	if changedRows < verticalScrollProbeMinChangedRows {
		return false
	}
	return payloadBytes >= verticalScrollProbeMinPayloadBytes || shouldProbeBroadVerticalScroll(rows, changedRows)
}

func shouldProbeBroadVerticalScroll(rows, changedRows int) bool {
	if rows < broadRawRowPatchMinRows || changedRows < verticalScrollProbeMinChangedRows {
		return false
	}
	return changedRows*broadRawRowPatchChangedRowsDenominator >= rows*broadRawRowPatchChangedRowsNumerator
}

func shouldUseBroadRawRowPatch(rows, changedRows, rowDiffBytes int) bool {
	if rows < broadRawRowPatchMinRows || changedRows < broadRawRowPatchMinChangedRows || rowDiffBytes < broadRawRowPatchMinPayloadBytes {
		return false
	}
	return changedRows*broadRawRowPatchChangedRowsDenominator >= rows*broadRawRowPatchChangedRowsNumerator
}

func shouldPromoteBroadRawRowsToFullRepaint(rawBytes, fullWireLen, rows, changedRows int) bool {
	if rawBytes <= 0 || fullWireLen <= 0 || rows <= 6 || changedRows <= 0 {
		return false
	}
	if changedRows*broadRawRowPatchChangedRowsDenominator < rows*broadRawRowPatchChangedRowsNumerator {
		return false
	}
	if shouldForceFullRepaintForBroadDamage(rows, changedRows) {
		return true
	}
	return rawBytes*100 >= fullWireLen*80
}

func shouldForceFullRepaintForBroadDamage(rows, changedRows int) bool {
	if rows <= 6 || changedRows <= 0 {
		return false
	}
	return changedRows*100 >= rows*broadRawRowPatchFullRepaintRowsPercent
}

func broadRawRowPatchCandidate(lines []string, payload string, changedRows int) framePatchCandidate {
	fullWireLen := normalizedJoinedLinesWireLen(lines)
	if shouldPromoteBroadRawRowsToFullRepaint(normalizedFrameLen(payload), fullWireLen, len(lines), changedRows) {
		return framePatchCandidate{
			mode:                 framePatchCandidateFullRepaint,
			payload:              xansi.EraseEntireDisplay + strings.Join(lines, "\n"),
			changedCount:         changedRows,
			updatedCount:         changedRows,
			baselineChangedCount: changedRows,
			metrics: []framePatchMetric{
				{name: "cursor_writer.present.mode.full_repaint_broad_raw_rows", count: fullWireLen},
			},
		}
	}
	return framePatchCandidate{
		mode:                 framePatchCandidateRawRows,
		payload:              payload,
		changedCount:         changedRows,
		updatedCount:         changedRows,
		baselineChangedCount: changedRows,
		metrics: []framePatchMetric{
			{name: "cursor_writer.present.mode.raw_rows_broad_damage", count: changedRows},
		},
	}
}

func (p *framePresenter) planFramePatch(lines []string, meta *presentMeta) framePatchCandidate {
	planStart := time.Now()
	log := presentPlanLog{
		Rows:          len(lines),
		PreviousBytes: joinedLinesLen(p.lines),
		NextBytes:     joinedLinesLen(lines),
	}
	defer func() {
		log.PlanMs = float64(time.Since(planStart).Microseconds()) / 1000.0
		if p != nil && p.planLogHook != nil {
			p.planLogHook(log)
		}
	}()

	quickStart := time.Now()
	fast := p.quickFramePatchCandidate(lines)
	log.QuickRowsMs = float64(time.Since(quickStart).Microseconds()) / 1000.0
	log.QuickCandidateValid = fast.valid()
	if fast.valid() {
		log.Mode = fast.mode.String()
		log.ChangedRows = fast.changedCount
		log.UpdatedRows = fast.updatedCount
		log.BaselineChangedRows = fast.baselineChangedCount
		log.PayloadBytes = len(fast.payload)
		log.QuickCandidateUsed = true
		return fast
	}
	diffStart := time.Now()
	diff := p.diffPatchCandidate(lines)
	log.DiffMs = float64(time.Since(diffStart).Microseconds()) / 1000.0
	log.RenderChangedRowsMs = diff.renderChangedRowsMs
	log.Mode = diff.mode.String()
	log.ChangedRows = diff.changedCount
	log.UpdatedRows = diff.updatedCount
	log.BaselineChangedRows = diff.baselineChangedCount
	log.PayloadBytes = len(diff.payload)
	if diff.updatedCount == 0 {
		return diff
	}
	best := diff
	fullWireLen := normalizedJoinedLinesWireLen(lines)
	log.FullWireBytes = fullWireLen
	if shouldFallbackToFullRepaint(diff.payload, fullWireLen, len(lines), diff.changedCount) {
		log.FullRepaintCandidate = true
		full := framePatchCandidate{
			mode:         framePatchCandidateFullRepaint,
			payload:      xansi.EraseEntireDisplay + strings.Join(lines, "\n"),
			updatedCount: diff.updatedCount,
			metrics: []framePatchMetric{
				{name: "cursor_writer.diff_full_repaint_fallback", count: fullWireLen},
				{name: "cursor_writer.present.mode.full_repaint_threshold", count: fullWireLen},
			},
		}
		if shouldForceFullRepaintForBroadDamage(len(lines), diff.changedCount) || betterFramePatchCandidate(full, best) {
			best = full
		}
	}
	if p.ownerAwareDeltaEnabled && p.fullWidthLines && meta != nil && p.meta != nil && (shouldUseOwnerAwareDelta(meta) || shouldUseOwnerAwareDelta(p.meta)) && !presentedLinesHaveWidthSafetyState(p.lines) && !presentedLinesHaveWidthSafetyState(lines) {
		log.OwnerAwareAttempted = true
		ownerStart := time.Now()
		candidate := p.ownerAwareDeltaCandidate(lines, meta)
		log.OwnerAwareMs = float64(time.Since(ownerStart).Microseconds()) / 1000.0
		log.OwnerAwareValid = candidate.valid()
		if betterFramePatchCandidate(candidate, best) {
			best = candidate
		}
	}
	if p.verticalScrollMode.Enabled() && shouldProbeVerticalScroll(len(lines), diff.changedCount, normalizedFrameLen(diff.payload)) {
		log.VerticalAttempted = true
		verticalStart := time.Now()
		candidate := p.verticalScrollCandidate(lines)
		log.VerticalScrollMs = float64(time.Since(verticalStart).Microseconds()) / 1000.0
		log.VerticalValid = candidate.valid()
		if betterFramePatchCandidate(candidate, best) {
			best = candidate
		}
	}
	if best.mode != framePatchCandidateDiff {
		best.updatedCount = diff.updatedCount
		best.baselineChangedCount = diff.changedCount
		best.baselineUpdates = diff.updates
		best.baselineReclaim = diff.reclaim
	}
	log.Mode = best.mode.String()
	log.ChangedRows = best.changedCount
	log.UpdatedRows = best.updatedCount
	log.BaselineChangedRows = best.baselineChangedCount
	log.PayloadBytes = len(best.payload)
	return best
}

func (m framePatchCandidateMode) String() string {
	switch m {
	case framePatchCandidateDiff:
		return "diff"
	case framePatchCandidateRawRows:
		return "raw_rows"
	case framePatchCandidateOwnerAware:
		return "owner_aware"
	case framePatchCandidateVerticalScrollRows:
		return "vertical_scroll_rows"
	case framePatchCandidateVerticalScrollRect:
		return "vertical_scroll_rect"
	case framePatchCandidateFullRepaint:
		return "full_repaint"
	default:
		return "none"
	}
}
