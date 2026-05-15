package app

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	xansi "github.com/charmbracelet/x/ansi"
	xterm "github.com/charmbracelet/x/term"
	"github.com/lozzow/termx/termx-shared/perftrace"
	tuiruntime "github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/shared"
)

type cursorSequenceWriter interface {
	SetCursorSequence(seq string)
	WriteControlSequence(seq string) error
	// QueueControlSequenceAfterWrite defers a control sequence until after the
	// next Bubble Tea frame write. Startup probes rely on this so the host sees
	// the probe only after alt-screen entry and the first frame are live.
	QueueControlSequenceAfterWrite(seq string)
}

type frameSequenceWriter interface {
	WriteFrame(frame, cursor string) error
}

type frameLinesWriter interface {
	WriteFrameLines(lines []string, cursor string) error
}

type frameBackpressureWriter interface {
	frameSequenceWriter
	HasPendingFrame() bool
	SetDrainHook(func([]tuiruntime.PendingStreamReady))
}

type frameStreamReadyWriter interface {
	SetNextFrameStreamReadies([]tuiruntime.PendingStreamReady)
}

type frameResetWriter interface {
	ResetFrameState()
}

type outputCursorWriter struct {
	out io.Writer
	tty xterm.File

	mu         sync.Mutex
	cursor     string
	afterWrite []string
	pending    pendingDirectFrame
	presenter  framePresenter

	bubbleTeaRestore string
	cursorProjected  bool

	directAltScreen         bool
	directMouseCell         bool
	directBracketedPaste    bool
	ttyWidth                int
	lastTTYWidth            int
	lastDirectCursor        string
	lastFlushAt             time.Time
	frameDumpPath           string
	forceImmediateNextFrame bool
	disableVerticalScroll   bool
	disableOwnerAwareDelta  bool
	forceFullFrameLines     bool
	useUVRenderer           bool
	uvRenderer              *uvTerminalFrameRenderer
	uvHostWidthFallbacks    int
	uvHostWidthBackoff      int
	uvSlowFrames            int
	uvSlowBackoff           int
	debugLogPath            string
	verticalScrollMode      verticalScrollMode
	drainHook               func([]tuiruntime.PendingStreamReady)
	nextStreamReadies       []tuiruntime.PendingStreamReady
	interactiveFlushHint    func() bool
	backlogActive           atomic.Bool
	adaptiveBatchLevel      uint8
	adaptiveSlowStreak      uint8
	adaptiveFastStreak      uint8
	flushTimer              *time.Timer
	flushTimerArmed         bool
	perfSampleHook          func(string)
	presentPlanLogHook      func(presentPlanLog)
}

type presentPlanLog struct {
	Rows                 int
	PreviousBytes        int
	NextBytes            int
	Mode                 string
	ChangedRows          int
	UpdatedRows          int
	BaselineChangedRows  int
	PayloadBytes         int
	FullWireBytes        int
	QuickRowsMs          float64
	DiffMs               float64
	OwnerAwareMs         float64
	VerticalScrollMs     float64
	PlanMs               float64
	RenderChangedRowsMs  float64
	OwnerAwareAttempted  bool
	VerticalAttempted    bool
	QuickCandidateUsed   bool
	QuickCandidateValid  bool
	OwnerAwareValid      bool
	VerticalValid        bool
	FullRepaintCandidate bool
}

type pendingDirectFrame struct {
	scheduled  bool
	frame      string
	lines      []string
	meta       *presentMeta
	cursor     string
	afterWrite []string
	readies    []tuiruntime.PendingStreamReady
}

type framePresenter struct {
	lines                              []string
	meta                               *presentMeta
	parsed                             []presentedRow
	scratchLines                       []string
	reclaim                            [][]presentedCell
	updates                            []presentedRowUpdate
	ready                              bool
	verticalScrollMode                 verticalScrollMode
	ownerAwareDeltaEnabled             bool
	fullWidthLines                     bool
	debugFaultScrollDropRemainderEvery int
	verticalScrollCount                int
	planLogHook                        func(presentPlanLog)
}

type presentedRow struct {
	raw                        string
	cells                      []presentedCell
	hasStyled                  bool
	hasWide                    bool
	hasErase                   bool
	hasHiddenEmojiCompensation bool
	hasHostWidthStabilizer     bool
}

type presentedRowUpdate struct {
	row     int
	parsed  presentedRow
	replace bool
}

type presentedCell struct {
	Content        string
	Width          int
	Style          presentedStyle
	Erase          bool
	ReanchorBefore bool
}

type presentedStyle struct {
	FGCode        string
	BGCode        string
	Bold          bool
	Italic        bool
	Underline     bool
	Blink         bool
	Reverse       bool
	Strikethrough bool
}

var (
	synchronizedOutputBegin = xansi.DECSET(xansi.ModeSynchronizedOutput)
	synchronizedOutputEnd   = xansi.DECRST(xansi.ModeSynchronizedOutput)
	hostAutoWrapOff         = xansi.ResetModeAutoWrap
	hostAutoWrapOn          = xansi.SetModeAutoWrap
	presentedCellPool       sync.Pool
)

var presentedStyleDiffCache = struct {
	mu sync.RWMutex
	m  map[presentedStyleTransitionKey]string
}{
	m: make(map[presentedStyleTransitionKey]string),
}

const hideHostCursorSequence = "\x1b[?25l"
const presentedResetStyleSequence = "\x1b[0m"
const maxPooledPresentedCellCapacity = 2048

var directFrameBatchDelay = 4 * time.Millisecond
var directFrameIdleThreshold = 12 * time.Millisecond
var remoteDirectFrameBatchDelay = 1500 * time.Microsecond
var remoteDirectFrameIdleThreshold = 6 * time.Millisecond

const (
	directFrameDrainSlowThreshold  = 16 * time.Millisecond
	directFrameDrainFastThreshold  = 4 * time.Millisecond
	directFrameAdaptiveMaxDelay    = 50 * time.Millisecond
	directFrameAdaptiveMaxLevel    = 4
	directFrameAdaptiveSlowSamples = 3
	directFrameAdaptiveFastSamples = 6
)

func (p *framePresenter) Reset() {
	if p == nil {
		return
	}
	releasePresentedRows(p.parsed)
	p.lines = nil
	p.parsed = nil
	p.scratchLines = nil
	p.reclaim = nil
	p.updates = nil
	p.ready = false
	p.verticalScrollMode = verticalScrollModeRowsAndRects
	p.ownerAwareDeltaEnabled = true
	p.fullWidthLines = false
	p.verticalScrollCount = 0
	p.meta = nil
}

func (p *framePresenter) Present(frame string) string {
	if p == nil {
		return frame
	}
	lines := splitFrameLines(frame, p.scratchLines[:0])
	return p.presentLines(lines, nil)
}

func (p *framePresenter) PresentLines(lines []string) string {
	if p == nil {
		return strings.Join(lines, "\n")
	}
	return p.presentLines(lines, nil)
}

func (p *framePresenter) PresentLinesWithMeta(lines []string, meta *presentMeta) string {
	if p == nil {
		return strings.Join(lines, "\n")
	}
	return p.presentLines(lines, meta)
}

func (p *framePresenter) presentLines(lines []string, meta *presentMeta) string {
	if !p.ready {
		perftrace.Count("cursor_writer.present.mode.initial_full", len(lines))
		p.setLines(lines, true)
		p.ready = true
		p.meta = clonePresentMeta(meta)
		return strings.Join(lines, "\n")
	}
	if len(lines) != len(p.lines) {
		perftrace.Count("cursor_writer.present.mode.full_repaint_resize", len(lines))
		releasePresentedRows(p.parsed)
		p.setLines(lines, true)
		p.meta = clonePresentMeta(meta)
		return xansi.EraseEntireDisplay + strings.Join(lines, "\n")
	}
	plan := p.planFramePatch(lines, meta)
	if plan.updatedCount == 0 {
		p.updates = plan.updates[:0]
		p.reclaim = plan.reclaim[:0]
		p.meta = clonePresentMeta(meta)
		return ""
	}
	if plan.mode != framePatchCandidateDiff {
		releaseDiscardedPresentedRowUpdates(plan.baselineUpdates)
		p.updates = plan.baselineUpdates[:0]
		p.reclaim = plan.baselineReclaim[:0]
		selectedPayload := p.selectedFramePatchPayload(plan)
		fullLen := normalizedJoinedLinesWireLen(lines)
		if shouldCountFullRepaintAvoided(selectedPayload, fullLen, len(lines)) {
			perftrace.Count("cursor_writer.present.mode.full_repaint_avoided", fullLen)
		}
		emitFramePatchMetrics(plan.metrics)
		releasePresentedRows(p.parsed)
		p.setLines(lines, true)
		p.meta = clonePresentMeta(meta)
		return selectedPayload
	}
	p.lines = append(p.lines[:0], lines...)
	if p.scratchLines != nil {
		p.scratchLines = p.scratchLines[:0]
	}
	for _, update := range plan.updates {
		p.parsed[update.row] = update.parsed
	}
	p.updates = plan.updates[:0]
	p.reclaim = plan.reclaim[:0]
	releasePresentedCellSlices(plan.reclaim)
	if plan.changedCount == 0 {
		perftrace.Count("cursor_writer.present.mode.no_change", 0)
		p.meta = clonePresentMeta(meta)
		return ""
	}
	fullLen := normalizedJoinedLinesWireLen(lines)
	if shouldCountFullRepaintAvoided(plan.payload, fullLen, len(lines)) {
		perftrace.Count("cursor_writer.present.mode.full_repaint_avoided", fullLen)
	}
	p.meta = clonePresentMeta(meta)
	perftrace.Count("cursor_writer.present.mode.diff", plan.changedCount)
	return plan.payload
}

func joinedLinesLen(lines []string) int {
	if len(lines) == 0 {
		return 0
	}
	total := len(lines) - 1
	for _, line := range lines {
		total += len(line)
	}
	return total
}

func shouldUseOwnerAwareDelta(meta *presentMeta) bool {
	if meta == nil || len(meta.VisibleRects) == 0 {
		return false
	}
	width := 0
	for _, row := range meta.OwnerMap {
		if len(row) > width {
			width = len(row)
		}
	}
	if width == 0 {
		width = meta.Width
	}
	if width <= 0 {
		return false
	}
	for _, rects := range meta.VisibleRects {
		if len(rects) > 1 {
			return true
		}
		for _, r := range rects {
			if r.Left > 0 || r.Right < width-1 {
				return true
			}
		}
	}
	return false
}

func presentedLinesHaveWidthSafetyState(lines []string) bool {
	for _, line := range lines {
		if lineHasWidthSafetyState(line) {
			return true
		}
	}
	return false
}

func (p *framePresenter) setLines(lines []string, resetParsed bool) {
	if p == nil {
		return
	}
	p.lines = append(p.lines[:0], lines...)
	if p.scratchLines != nil {
		p.scratchLines = p.scratchLines[:0]
	}
	if cap(p.parsed) < len(lines) {
		p.parsed = make([]presentedRow, len(lines))
	} else {
		p.parsed = p.parsed[:len(lines)]
	}
	if resetParsed {
		clear(p.parsed)
	}
}

func (p *framePresenter) presentVerticalScroll(lines []string) string {
	return p.selectedFramePatchPayload(p.verticalScrollCandidate(lines))
}

func (p *framePresenter) verticalScrollCandidate(lines []string) framePatchCandidate {
	if candidate := p.verticalScrollRowsCandidate(lines); candidate.valid() {
		return candidate
	}
	return p.verticalScrollRectCandidate(lines)
}

func (p *framePresenter) verticalScrollRowsCandidate(lines []string) framePatchCandidate {
	if len(lines) < 6 || len(lines) != len(p.lines) {
		return framePatchCandidate{}
	}
	if p.verticalScrollMode.RowsAllowed() {
		plan, ok := detectVerticalScrollPlan(p.lines, lines)
		if ok {
			afterScroll := applyVerticalScrollPlan(p.lines, plan)
			remainder, _ := renderChangedRows(afterScroll, lines)
			prefix := renderVerticalScrollPlan(plan, len(lines))
			if prefix != "" {
				return framePatchCandidate{
					mode:         framePatchCandidateVerticalScrollRows,
					payload:      prefix + remainder,
					faultPayload: prefix,
					metrics: []framePatchMetric{
						{name: "cursor_writer.present.mode.vertical_scroll_rows", count: plan.reused},
						{name: "cursor_writer.present.mode.delta_rect_scroll_fullwidth", count: plan.reused},
					},
				}
			}
		}
	}
	return framePatchCandidate{}
}

func (p *framePresenter) verticalScrollRectCandidate(lines []string) framePatchCandidate {
	if len(lines) < 6 || len(lines) != len(p.lines) {
		return framePatchCandidate{}
	}
	best := framePatchCandidate{}
	if p.verticalScrollMode.RectsAllowed() && p.fullWidthLines && shared.ExperimentalLRScrollEnabled() {
		nextRows := make([]presentedRow, len(lines))
		for i := range lines {
			nextRows[i] = parsePresentedRow(lines[i])
		}
		defer releasePresentedRows(nextRows)
		previousRows := make([]presentedRow, len(p.lines))
		for i := range p.lines {
			previousRows[i] = p.presentedRow(i)
		}
		rectPlan, ok := detectVerticalScrollRectPlan(previousRows, nextRows)
		if ok {
			afterScroll := applyVerticalScrollRectPlan(previousRows, rectPlan)
			if len(afterScroll) == len(lines) {
				remainder, _ := renderChangedRows(afterScroll, lines)
				prefix := renderVerticalScrollRectPlan(rectPlan, len(lines))
				if prefix != "" {
					candidate := framePatchCandidate{
						mode:    framePatchCandidateVerticalScrollRect,
						payload: prefix + remainder,
						metrics: []framePatchMetric{
							{name: "cursor_writer.present.mode.vertical_scroll_rect", count: rectPlan.reused * (rectPlan.right - rectPlan.left + 1)},
							{name: "cursor_writer.present.mode.delta_rect_scroll_lr_margin", count: rectPlan.reused * (rectPlan.right - rectPlan.left + 1)},
						},
					}
					if betterFramePatchCandidate(candidate, best) {
						best = candidate
					}
				}
			}
		}
	}
	return best
}

func renderChangedRows(previous, next []string) (string, int) {
	if len(previous) != len(next) {
		return "", 0
	}
	changed := make([]int, 0, len(next))
	for i := range next {
		if next[i] != previous[i] {
			changed = append(changed, i)
		}
	}
	if len(changed) == 0 {
		return "", 0
	}
	var out strings.Builder
	for i := 0; i < len(changed); {
		start := changed[i]
		end := start
		for i+1 < len(changed) && changed[i+1] == end+1 {
			i++
			end = changed[i]
		}
		writeCUP(&out, 1, start+1)
		for row := start; row <= end; row++ {
			if row > start {
				out.WriteByte('\n')
			}
			out.WriteString(next[row])
		}
		i++
	}
	return out.String(), len(changed)
}

func (p *framePresenter) renderChangedRows(next []string) (string, int, int, []presentedRowUpdate, [][]presentedCell) {
	if p == nil || len(next) != len(p.lines) {
		return "", 0, 0, nil, nil
	}
	updates := p.updates[:0]
	changed := 0
	updated := 0
	reclaim := p.reclaim[:0]
	var out strings.Builder
	out.Grow(len(next) * 16)
	for row := range next {
		if next[row] == p.lines[row] {
			continue
		}
		prevRow := p.presentedRow(row)
		nextRow := parsePresentedRow(next[row])
		if presentedRowsEquivalent(prevRow, nextRow, p.fullWidthLines) {
			updated++
			releasePresentedCells(nextRow.cells)
			prevRow.raw = next[row]
			updates = append(updates, presentedRowUpdate{row: row, parsed: prevRow})
			continue
		}
		updated++
		changed++
		if len(prevRow.cells) > 0 {
			reclaim = append(reclaim, prevRow.cells)
		}
		updates = append(updates, presentedRowUpdate{row: row, parsed: nextRow, replace: true})
		if !renderChangedRowDiff(&out, prevRow, nextRow, row, p.fullWidthLines) {
			writeCUP(&out, 1, row+1)
			out.WriteString(next[row])
		}
	}
	return out.String(), changed, updated, updates, reclaim
}

func presentedRowsEquivalent(previous, next presentedRow, fullWidthLines bool) bool {
	if previous.hasHiddenEmojiCompensation || next.hasHiddenEmojiCompensation || previous.hasHostWidthStabilizer || next.hasHostWidthStabilizer {
		return previous.raw == next.raw
	}
	if len(previous.cells) != len(next.cells) {
		return false
	}
	for i := range next.cells {
		if previous.cells[i] != next.cells[i] {
			return false
		}
	}
	if fullWidthLines {
		return true
	}
	return rowOwnsLineEnd(previous) == rowOwnsLineEnd(next)
}

func splitFrameLines(frame string, dst []string) []string {
	start := 0
	for i := 0; i < len(frame); i++ {
		if frame[i] != '\n' {
			continue
		}
		dst = append(dst, frame[start:i])
		start = i + 1
	}
	return append(dst, frame[start:])
}

func (p *framePresenter) presentedRow(index int) presentedRow {
	if p == nil || index < 0 || index >= len(p.lines) {
		return presentedRow{}
	}
	if p.parsed[index].raw == p.lines[index] {
		return p.parsed[index]
	}
	row := parsePresentedRow(p.lines[index])
	p.parsed[index] = row
	return row
}

func renderChangedRowDiff(out *strings.Builder, previous, next presentedRow, row int, fullWidthLines bool) bool {
	if previous.raw == next.raw {
		return true
	}
	if previous.hasHiddenEmojiCompensation || next.hasHiddenEmojiCompensation || previous.hasHostWidthStabilizer || next.hasHostWidthStabilizer {
		return false
	}
	prevCells := previous.cells
	nextCells := next.cells
	ownsLineEnd := rowOwnsLineEnd(next)
	if !fullWidthLines && (previous.hasErase || next.hasErase) {
		return false
	}
	if renderChangedRowIntralineEdit(out, previous, next, row, fullWidthLines, ownsLineEnd) {
		return true
	}
	if !previous.hasWide && !next.hasWide && len(prevCells) == len(nextCells) {
		if renderChangedRowRuns(out, prevCells, nextCells, row, fullWidthLines, ownsLineEnd) {
			return true
		}
	}
	return renderChangedRowSuffix(out, previous, next, row, fullWidthLines, ownsLineEnd)
}

func renderChangedRowRuns(out *strings.Builder, previous, next []presentedCell, row int, fullWidthLines bool, ownsLineEnd bool) bool {
	if len(previous) != len(next) {
		return false
	}
	prevCol := 1
	nextCol := 1
	runStart := -1
	runStartCol := 1
	flush := func(end int) {
		if runStart < 0 || runStart >= end {
			return
		}
		writeCUP(out, runStartCol, row+1)
		lastStyle := writePresentedCells(out, next[runStart:end], runStartCol)
		if end == len(next) {
			if fullWidthLines {
				// Lines from RenderFrameLines() already serialize every column.
			} else if ownsLineEnd {
				writeOwnedLineEndClear(out, lastStyle)
			} else {
				out.WriteString(xansi.EraseLineRight)
			}
		}
		runStart = -1
	}
	for i := range next {
		same := previous[i] == next[i] && prevCol == nextCol
		if same {
			flush(i)
		} else if runStart < 0 {
			runStart = i
			runStartCol = nextCol
		}
		prevCol += maxInt(1, previous[i].Width)
		nextCol += maxInt(1, next[i].Width)
	}
	if prevCol != nextCol {
		return false
	}
	flush(len(next))
	return true
}

func renderChangedRowSuffix(out *strings.Builder, previous, next presentedRow, row int, fullWidthLines bool, ownsLineEnd bool) bool {
	prevCells := previous.cells
	nextCells := next.cells
	prefixIndex := 0
	prefixWidth := 0
	for prefixIndex < len(prevCells) && prefixIndex < len(nextCells) && prevCells[prefixIndex] == nextCells[prefixIndex] {
		prefixWidth += nextCells[prefixIndex].Width
		prefixIndex++
	}
	if prefixIndex == len(prevCells) && prefixIndex == len(nextCells) {
		return true
	}
	writeCUP(out, prefixWidth+1, row+1)
	if len(nextCells[prefixIndex:]) == 0 {
		if !fullWidthLines && !ownsLineEnd {
			out.WriteString(xansi.EraseLineRight)
		}
		return true
	}
	lastStyle := writePresentedCells(out, nextCells[prefixIndex:], prefixWidth+1)
	if fullWidthLines {
		return true
	}
	if ownsLineEnd {
		writeOwnedLineEndClear(out, lastStyle)
	} else {
		out.WriteString(xansi.EraseLineRight)
	}
	return true
}

func (w *outputCursorWriter) enterDirectTerminal() error {
	if w == nil || w.out == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.directAltScreen {
		return nil
	}
	if _, err := io.WriteString(w.out, xansi.HideCursor); err != nil {
		return err
	}
	if _, err := io.WriteString(w.out, xansi.EnableAltScreenBuffer); err != nil {
		return err
	}
	if _, err := io.WriteString(w.out, xansi.EraseEntireDisplay+xansi.MoveCursorOrigin); err != nil {
		return err
	}
	if _, err := io.WriteString(w.out, xansi.HideCursor); err != nil {
		return err
	}
	if _, err := io.WriteString(w.out, xansi.EnableBracketedPaste); err != nil {
		return err
	}
	if _, err := io.WriteString(w.out, xansi.EnableMouseCellMotion+xansi.EnableMouseSgrExt); err != nil {
		return err
	}
	w.directAltScreen = true
	w.directMouseCell = true
	w.directBracketedPaste = true
	w.pending = pendingDirectFrame{}
	w.presenter.Reset()
	w.presenter.verticalScrollMode = w.effectiveVerticalScrollModeLocked()
	w.presenter.ownerAwareDeltaEnabled = w.ownerAwareDeltaEnabledLocked()
	w.resetUVRendererLocked()
	w.resetUVFallbackStateLocked()
	w.lastTTYWidth = 0
	w.lastDirectCursor = ""
	w.stopFlushTimerLocked()
	return nil
}

func (w *outputCursorWriter) exitDirectTerminal() error {
	if w == nil || w.out == nil {
		return nil
	}
	w.mu.Lock()
	hook, err := w.flushPendingFrameLocked()
	if err != nil {
		w.mu.Unlock()
		return err
	}
	if w.directBracketedPaste {
		if _, err := io.WriteString(w.out, xansi.DisableBracketedPaste); err != nil {
			w.mu.Unlock()
			return err
		}
		w.directBracketedPaste = false
	}
	if _, err := io.WriteString(w.out, xansi.ShowCursor); err != nil {
		w.mu.Unlock()
		return err
	}
	if w.directMouseCell {
		if _, err := io.WriteString(w.out, xansi.DisableMouseCellMotion+xansi.DisableMouseSgrExt); err != nil {
			w.mu.Unlock()
			return err
		}
		w.directMouseCell = false
	}
	if w.directAltScreen {
		if _, err := io.WriteString(w.out, xansi.DisableAltScreenBuffer); err != nil {
			w.mu.Unlock()
			return err
		}
		w.directAltScreen = false
	}
	w.presenter.Reset()
	w.presenter.ownerAwareDeltaEnabled = w.ownerAwareDeltaEnabledLocked()
	w.resetUVRendererLocked()
	w.resetUVFallbackStateLocked()
	w.lastDirectCursor = ""
	w.stopFlushTimerLocked()
	w.mu.Unlock()
	if hook != nil {
		hook()
	}
	return nil
}

func (w *outputCursorWriter) WriteFrame(frame, cursor string) error {
	finish := perftrace.Measure("cursor_writer.write_frame")
	defer func() {
		finish(len(frame) + len(cursor))
	}()
	if w == nil || w.out == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pending.frame = w.fitFrameToTTY(frame)
	w.pending.lines = nil
	w.pending.meta = nil
	w.pending.cursor = cursor
	w.pending.afterWrite = append(w.pending.afterWrite, w.afterWrite...)
	w.pending.readies = append([]tuiruntime.PendingStreamReady(nil), w.nextStreamReadies...)
	w.nextStreamReadies = nil
	w.afterWrite = nil
	w.backlogActive.Store(true)
	if w.forceImmediateNextFrame {
		w.forceImmediateNextFrame = false
		// Full redraws after ResetFrameState() must reach the host immediately.
		// If they sit in the batch window, a later resize/drag frame can replace
		// the pending repaint and leave the host baseline stuck on the stale
		// pre-resize contents that the next diff assumes were already repaired.
		hook, err := w.flushPendingFrameLocked()
		w.mu.Unlock()
		if hook != nil {
			hook()
		}
		w.mu.Lock()
		return err
	}
	delay := w.effectiveDirectFrameBatchDelayLocked()
	if delay <= 0 {
		hook, err := w.flushPendingFrameLocked()
		w.mu.Unlock()
		if hook != nil {
			hook()
		}
		w.mu.Lock()
		return err
	}
	if w.shouldFlushDirectFrameImmediatelyLocked() {
		hook, err := w.flushPendingFrameLocked()
		w.mu.Unlock()
		if hook != nil {
			hook()
		}
		w.mu.Lock()
		return err
	}
	if w.pending.scheduled {
		return nil
	}
	w.pending.scheduled = true
	w.scheduleFlushLocked(delay)
	return nil
}

func (w *outputCursorWriter) WriteFrameLines(lines []string, cursor string) error {
	return w.WriteFrameLinesWithMeta(lines, cursor, nil)
}

func (w *outputCursorWriter) WriteFrameLinesWithMeta(lines []string, cursor string, meta *presentMeta) error {
	finish := perftrace.Measure("cursor_writer.write_frame")
	lineBytes := joinedLinesLen(lines) + len(cursor)
	defer func() {
		finish(lineBytes)
	}()
	if w == nil || w.out == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pending.frame = ""
	w.pending.lines = w.fitLinesToTTY(stripLeadingCHA1(lines))
	w.pending.meta = clonePresentMeta(meta)
	w.pending.cursor = cursor
	w.pending.afterWrite = append(w.pending.afterWrite, w.afterWrite...)
	w.pending.readies = append([]tuiruntime.PendingStreamReady(nil), w.nextStreamReadies...)
	w.nextStreamReadies = nil
	w.afterWrite = nil
	w.backlogActive.Store(true)
	w.debugLog(
		"cursor_writer.enqueue",
		"mode", "lines",
		"rows", len(w.pending.lines),
		"input_bytes", lineBytes,
		"pending_scheduled", w.pending.scheduled,
		"adaptive_level", w.adaptiveBatchLevel,
	)
	if w.forceImmediateNextFrame {
		w.forceImmediateNextFrame = false
		// ResetFrameState() clears the presenter baseline. The next frame must
		// reach the host immediately so geometry-change redraws cannot be
		// coalesced away behind a later diff frame that assumes the baseline was
		// already rebuilt on screen.
		hook, err := w.flushPendingFrameLocked()
		w.mu.Unlock()
		if hook != nil {
			hook()
		}
		w.mu.Lock()
		return err
	}
	delay := w.effectiveDirectFrameBatchDelayLocked()
	if delay <= 0 {
		hook, err := w.flushPendingFrameLocked()
		w.mu.Unlock()
		if hook != nil {
			hook()
		}
		w.mu.Lock()
		return err
	}
	if w.shouldFlushDirectFrameImmediatelyLocked() {
		hook, err := w.flushPendingFrameLocked()
		w.mu.Unlock()
		if hook != nil {
			hook()
		}
		w.mu.Lock()
		return err
	}
	if w.pending.scheduled {
		return nil
	}
	w.pending.scheduled = true
	w.debugLog("cursor_writer.schedule_flush", "delay_ms", delay.Milliseconds(), "adaptive_level", w.adaptiveBatchLevel)
	w.scheduleFlushLocked(delay)
	return nil
}

func (w *outputCursorWriter) flushPendingFrame() {
	if w == nil || w.out == nil {
		return
	}
	w.mu.Lock()
	w.flushTimerArmed = false
	hook, _ := w.flushPendingFrameLocked()
	w.mu.Unlock()
	if hook != nil {
		hook()
	}
}

func (w *outputCursorWriter) flushPendingFrameLocked() (func(), error) {
	if w == nil || w.out == nil {
		return nil, nil
	}
	frame := w.pending.frame
	lines := w.pending.lines
	meta := w.pending.meta
	cursor := w.pending.cursor
	afterWrite := append([]string(nil), w.pending.afterWrite...)
	readies := append([]tuiruntime.PendingStreamReady(nil), w.pending.readies...)
	w.pending = pendingDirectFrame{}
	if frame == "" && len(lines) == 0 && len(afterWrite) == 0 {
		perftrace.Count("cursor_writer.direct_flush.empty", 0)
		w.backlogActive.Store(false)
		w.debugLog("cursor_writer.flush_empty")
		return w.frameDrainHookLocked(nil), nil
	}
	err := error(nil)
	flushStart := time.Now()
	if len(lines) > 0 {
		err = w.writeFrameLinesLocked(lines, meta, cursor, afterWrite)
	} else {
		err = w.writeFrameLocked(frame, cursor, afterWrite)
	}
	if err != nil {
		w.debugLog("cursor_writer.flush_error", "err", err)
		return nil, err
	}
	cost := time.Since(flushStart)
	w.observeDirectFlushCostLocked(cost)
	w.backlogActive.Store(false)
	w.lastFlushAt = time.Now()
	w.debugLog(
		"cursor_writer.flush_done",
		"elapsed_ms", float64(cost.Microseconds())/1000.0,
		"rows", len(lines),
		"frame_bytes", len(frame),
		"after_write", len(afterWrite),
		"adaptive_level", w.adaptiveBatchLevel,
	)
	return w.frameDrainHookLocked(readies), nil
}

func (w *outputCursorWriter) frameDrainHookLocked(readies []tuiruntime.PendingStreamReady) func() {
	if w == nil || w.drainHook == nil {
		return nil
	}
	hook := w.drainHook
	captured := append([]tuiruntime.PendingStreamReady(nil), readies...)
	return func() {
		hook(captured)
	}
}

func (w *outputCursorWriter) SetInteractiveFlushHint(hint func() bool) {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.interactiveFlushHint = hint
	w.mu.Unlock()
}

func (w *outputCursorWriter) SetTTYWidth(width int) {
	if w == nil || width <= 0 {
		return
	}
	w.mu.Lock()
	w.ttyWidth = width
	// Frames are rendered against the current WindowSizeMsg width, so matching
	// that width here lets the writer skip redundant truncate passes/syscalls.
	w.lastTTYWidth = width
	w.resetUVFallbackStateLocked()
	w.mu.Unlock()
}

func (w *outputCursorWriter) shouldFlushDirectFrameImmediatelyLocked() bool {
	if w == nil {
		return false
	}
	remoteProfile := shared.RemoteLatencyProfileEnabled()
	if w.interactiveFlushHint != nil && w.interactiveFlushHint() {
		if remoteProfile {
			perftrace.Count("cursor_writer.direct_flush.remote_interactive_bypass", 0)
		} else {
			perftrace.Count("cursor_writer.direct_flush.interactive_bypass", 0)
		}
		return true
	}
	threshold := w.effectiveDirectFrameIdleThresholdLocked()
	if threshold <= 0 {
		return true
	}
	if w.lastFlushAt.IsZero() {
		return true
	}
	return time.Since(w.lastFlushAt) >= threshold
}

func (w *outputCursorWriter) effectiveDirectFrameBatchDelayLocked() time.Duration {
	base := directFrameBatchDelay
	if w == nil || base <= 0 {
		return base
	}
	if shared.RemoteLatencyProfileEnabled() && (base <= 0 || base > remoteDirectFrameBatchDelay) {
		base = remoteDirectFrameBatchDelay
	}
	base = shared.DurationOverride("TERMX_DIRECT_FRAME_BATCH_DELAY", base)
	delay := base
	for i := 0; i < int(w.adaptiveBatchLevel); i++ {
		if delay >= directFrameAdaptiveMaxDelay {
			return directFrameAdaptiveMaxDelay
		}
		delay *= 2
	}
	if delay > directFrameAdaptiveMaxDelay {
		return directFrameAdaptiveMaxDelay
	}
	return delay
}

func (w *outputCursorWriter) effectiveDirectFrameIdleThresholdLocked() time.Duration {
	threshold := directFrameIdleThreshold
	if w == nil || threshold <= 0 {
		return threshold
	}
	if shared.RemoteLatencyProfileEnabled() && (threshold <= 0 || threshold > remoteDirectFrameIdleThreshold) {
		threshold = remoteDirectFrameIdleThreshold
	}
	threshold = shared.DurationOverride("TERMX_DIRECT_FRAME_IDLE_THRESHOLD", threshold)
	return threshold
}

func (w *outputCursorWriter) observeDirectFlushCostLocked(cost time.Duration) {
	if w == nil || cost <= 0 {
		return
	}
	switch {
	case cost >= directFrameDrainSlowThreshold:
		w.adaptiveSlowStreak++
		w.adaptiveFastStreak = 0
		if w.adaptiveSlowStreak < directFrameAdaptiveSlowSamples {
			return
		}
		w.adaptiveSlowStreak = 0
		if w.adaptiveBatchLevel < directFrameAdaptiveMaxLevel {
			w.adaptiveBatchLevel++
			perftrace.Count("cursor_writer.batch_delay.increase", 0)
		}
	case cost <= directFrameDrainFastThreshold:
		w.adaptiveFastStreak++
		w.adaptiveSlowStreak = 0
		if w.adaptiveFastStreak < directFrameAdaptiveFastSamples {
			return
		}
		w.adaptiveFastStreak = 0
		if w.adaptiveBatchLevel > 0 {
			w.adaptiveBatchLevel--
			perftrace.Count("cursor_writer.batch_delay.decrease", 0)
		}
	default:
		w.adaptiveSlowStreak = 0
		w.adaptiveFastStreak = 0
	}
}

func (w *outputCursorWriter) scheduleFlushLocked(delay time.Duration) {
	if w == nil || delay <= 0 {
		return
	}
	perftrace.Count("cursor_writer.schedule_timer", 0)
	if w.flushTimer == nil {
		w.flushTimer = time.AfterFunc(delay, w.flushPendingFrame)
		w.flushTimerArmed = true
		return
	}
	if w.flushTimerArmed {
		w.flushTimer.Stop()
	}
	w.flushTimer.Reset(delay)
	w.flushTimerArmed = true
}

func (w *outputCursorWriter) stopFlushTimerLocked() {
	if w == nil || w.flushTimer == nil {
		return
	}
	w.flushTimer.Stop()
	w.flushTimerArmed = false
}

func (w *outputCursorWriter) writeFrameLocked(frame, cursor string, afterWrite []string) error {
	finish := perftrace.Measure("cursor_writer.direct_flush")
	writtenBytes := 0
	defer func() {
		finish(writtenBytes)
	}()
	totalStart := time.Now()
	presentFinish := perftrace.Measure("cursor_writer.present")
	presentStart := time.Now()
	w.presenter.fullWidthLines = false
	payload := w.presenter.Present(frame)
	presentFinish(len(payload))
	presentElapsed := time.Since(presentStart)
	syncOutput := w.tty != nil
	if cursor == "" {
		cursor = hideHostCursorSequence
	}
	if payload == "" && len(afterWrite) == 0 && cursor == w.lastDirectCursor {
		perftrace.Count("cursor_writer.direct_skip", 0)
		if w.debugLogEnabled() {
			w.debugLog(
				"cursor_writer.frame_skip",
				"mode", "frame",
				"input_bytes", len(frame),
				"payload_bytes", len(payload),
				"present_ms", float64(presentElapsed.Microseconds())/1000.0,
				"total_ms", float64(time.Since(totalStart).Microseconds())/1000.0,
			)
		}
		return nil
	}

	// 预估总长度，一次性写入以避免多次 syscall 和中间刷新
	estLen := normalizedFrameLen(payload) + len(cursor) + 64
	for _, seq := range afterWrite {
		estLen += len(seq)
	}
	var buf strings.Builder
	buf.Grow(estLen)
	if syncOutput {
		buf.WriteString(synchronizedOutputBegin)
	}
	buf.WriteString(hideHostCursorSequence)
	buf.WriteString(xansi.MoveCursorOrigin)
	writeHostFramePayload(&buf, payload)
	for _, seq := range afterWrite {
		buf.WriteString(seq)
	}
	buf.WriteString(cursor)
	if syncOutput {
		buf.WriteString(synchronizedOutputEnd)
	}
	w.bubbleTeaRestore = ""
	w.cursorProjected = false
	output := buf.String()
	writtenBytes = len(output)
	ioFinish := perftrace.Measure("cursor_writer.io_write")
	ioStart := time.Now()
	_, err := io.WriteString(w.out, output)
	ioFinish(writtenBytes)
	ioElapsed := time.Since(ioStart)
	if err == nil {
		w.appendFrameDumpLocked("direct_frame", output)
		w.lastDirectCursor = cursor
		if w.perfSampleHook != nil {
			w.perfSampleHook("writer_flush")
		}
	}
	w.debugLog(
		"cursor_writer.frame_write",
		"mode", "frame",
		"input_bytes", len(frame),
		"payload_bytes", len(payload),
		"output_bytes", writtenBytes,
		"present_ms", float64(presentElapsed.Microseconds())/1000.0,
		"io_ms", float64(ioElapsed.Microseconds())/1000.0,
		"total_ms", float64(time.Since(totalStart).Microseconds())/1000.0,
		"err", err,
	)
	return err
}

func (w *outputCursorWriter) writeFrameLinesLocked(lines []string, meta *presentMeta, cursor string, afterWrite []string) error {
	finish := perftrace.Measure("cursor_writer.direct_flush")
	writtenBytes := 0
	defer func() {
		finish(writtenBytes)
	}()
	totalStart := time.Now()
	presentFinish := perftrace.Measure("cursor_writer.present")
	presentStart := time.Now()
	useRawPayload := false
	renderLines := stripTrailingEraseLineRight(lines)
	payload := ""
	presentMode := "delta"
	uvFallbackReason := ""
	uvAttemptElapsed := time.Duration(0)
	fallbackPresentElapsed := time.Duration(0)
	planLog := presentPlanLog{}
	if w.useUVRenderer {
		if w.forceFullFrameLines {
			w.resetUVRendererLocked()
			perftrace.Count("cursor_writer.present.mode.full_repaint_forced", len(lines))
		}
		var ok bool
		uvRendererWasActive := w.uvRenderer != nil
		uvAttemptStart := time.Now()
		payload, ok, uvFallbackReason = w.presentFrameLinesWithUVLocked(renderLines, meta)
		uvAttemptElapsed = time.Since(uvAttemptStart)
		if ok {
			useRawPayload = true
			presentMode = "uv"
			w.observeUVSuccessLocked(uvAttemptElapsed, len(renderLines), len(payload))
		} else {
			perftrace.Count("cursor_writer.renderer.uv.fallback", len(lines))
			if uvRendererWasActive || w.uvRenderer != nil {
				w.resetUVRendererLocked()
				w.presenter.Reset()
				w.presenter.verticalScrollMode = w.effectiveVerticalScrollModeLocked()
				w.presenter.ownerAwareDeltaEnabled = w.ownerAwareDeltaEnabledLocked()
			}
			if uvFallbackReason == uvFallbackReasonHostWidthBackoff {
				presentMode = "delta_after_uv_backoff"
			} else {
				presentMode = "delta_after_uv_fallback"
			}
		}
	}
	if !useRawPayload {
		fallbackPresentStart := time.Now()
		w.presenter.fullWidthLines = true
		previousVerticalScrollMode := w.presenter.verticalScrollMode
		previousOwnerAwareDeltaEnabled := w.presenter.ownerAwareDeltaEnabled
		previousPlanLogHook := w.presenter.planLogHook
		w.presenter.planLogHook = func(log presentPlanLog) {
			planLog = log
			if w.presentPlanLogHook != nil {
				w.presentPlanLogHook(log)
			}
			if previousPlanLogHook != nil {
				previousPlanLogHook(log)
			}
		}
		if w.forceFullFrameLines {
			w.presenter.Reset()
			w.presenter.verticalScrollMode = previousVerticalScrollMode
			w.presenter.ownerAwareDeltaEnabled = previousOwnerAwareDeltaEnabled
			w.presenter.planLogHook = previousPlanLogHook
			w.presenter.fullWidthLines = true
			perftrace.Count("cursor_writer.present.mode.full_repaint_forced", len(lines))
		}
		payload = w.presenter.PresentLinesWithMeta(renderLines, meta)
		w.presenter.planLogHook = previousPlanLogHook
		w.presenter.verticalScrollMode = previousVerticalScrollMode
		w.presenter.ownerAwareDeltaEnabled = previousOwnerAwareDeltaEnabled
		fallbackPresentElapsed = time.Since(fallbackPresentStart)
	}
	presentFinish(len(payload))
	presentElapsed := time.Since(presentStart)
	syncOutput := w.tty != nil
	if cursor == "" {
		cursor = hideHostCursorSequence
	}
	if payload == "" && len(afterWrite) == 0 && cursor == w.lastDirectCursor {
		perftrace.Count("cursor_writer.direct_skip", 0)
		if w.debugLogEnabled() {
			w.debugLog(
				"cursor_writer.frame_skip",
				"mode", presentMode,
				"rows", len(lines),
				"input_bytes", joinedLinesLen(lines),
				"payload_bytes", len(payload),
				"present_ms", float64(presentElapsed.Microseconds())/1000.0,
				"plan_mode", planLog.Mode,
				"plan_rows", planLog.Rows,
				"plan_changed_rows", planLog.ChangedRows,
				"plan_updated_rows", planLog.UpdatedRows,
				"plan_baseline_changed_rows", planLog.BaselineChangedRows,
				"tail", debugFrameTail(lines),
				"payload_preview", debugPayloadPreviewIfEnabled(payload),
				"total_ms", float64(time.Since(totalStart).Microseconds())/1000.0,
			)
		}
		return nil
	}
	estLen := normalizedLinesLen(lines) + len(cursor) + 64
	for _, seq := range afterWrite {
		estLen += len(seq)
	}
	var buf strings.Builder
	buf.Grow(estLen)
	if syncOutput {
		buf.WriteString(synchronizedOutputBegin)
	}
	buf.WriteString(hideHostCursorSequence)
	buf.WriteString(xansi.MoveCursorOrigin)
	if useRawPayload {
		buf.WriteString(payload)
	} else {
		writeHostFramePayload(&buf, payload)
	}
	for _, seq := range afterWrite {
		buf.WriteString(seq)
	}
	buf.WriteString(cursor)
	if syncOutput {
		buf.WriteString(synchronizedOutputEnd)
	}
	w.bubbleTeaRestore = ""
	w.cursorProjected = false
	output := buf.String()
	writtenBytes = len(output)
	ioFinish := perftrace.Measure("cursor_writer.io_write")
	ioStart := time.Now()
	_, err := io.WriteString(w.out, output)
	ioFinish(writtenBytes)
	ioElapsed := time.Since(ioStart)
	if err == nil {
		if useRawPayload {
			w.appendFrameDumpLocked("direct_frame_uv", output)
		} else {
			w.appendFrameDumpLocked("direct_frame", output)
		}
		w.lastDirectCursor = cursor
		if w.perfSampleHook != nil {
			w.perfSampleHook("writer_flush")
		}
	}
	if w.debugLogEnabled() {
		w.debugLog(
			"cursor_writer.frame_write",
			"mode", presentMode,
			"rows", len(lines),
			"input_bytes", joinedLinesLen(lines),
			"payload_bytes", len(payload),
			"output_bytes", writtenBytes,
			"after_write", len(afterWrite),
			"present_ms", float64(presentElapsed.Microseconds())/1000.0,
			"uv_attempt_ms", float64(uvAttemptElapsed.Microseconds())/1000.0,
			"fallback_present_ms", float64(fallbackPresentElapsed.Microseconds())/1000.0,
			"uv_fallback_reason", uvFallbackReason,
			"uv_host_width_fallbacks", w.uvHostWidthFallbacks,
			"uv_host_width_backoff", w.uvHostWidthBackoff,
			"uv_slow_frames", w.uvSlowFrames,
			"uv_slow_backoff", w.uvSlowBackoff,
			"plan_mode", planLog.Mode,
			"plan_rows", planLog.Rows,
			"plan_changed_rows", planLog.ChangedRows,
			"plan_updated_rows", planLog.UpdatedRows,
			"plan_baseline_changed_rows", planLog.BaselineChangedRows,
			"plan_payload_bytes", planLog.PayloadBytes,
			"plan_full_wire_bytes", planLog.FullWireBytes,
			"plan_quick_rows_ms", planLog.QuickRowsMs,
			"plan_diff_ms", planLog.DiffMs,
			"plan_render_changed_rows_ms", planLog.RenderChangedRowsMs,
			"plan_owner_aware_ms", planLog.OwnerAwareMs,
			"plan_vertical_scroll_ms", planLog.VerticalScrollMs,
			"plan_total_ms", planLog.PlanMs,
			"plan_owner_aware_attempted", planLog.OwnerAwareAttempted,
			"plan_vertical_attempted", planLog.VerticalAttempted,
			"plan_quick_used", planLog.QuickCandidateUsed,
			"plan_quick_valid", planLog.QuickCandidateValid,
			"plan_owner_aware_valid", planLog.OwnerAwareValid,
			"plan_vertical_valid", planLog.VerticalValid,
			"plan_full_repaint_candidate", planLog.FullRepaintCandidate,
			"tail", debugFrameTail(lines),
			"payload_preview", debugPayloadPreviewIfEnabled(payload),
			"output_preview", debugPayloadPreviewIfEnabled(output),
			"io_ms", float64(ioElapsed.Microseconds())/1000.0,
			"total_ms", float64(time.Since(totalStart).Microseconds())/1000.0,
			"err", err,
		)
	}
	return err
}

func newOutputCursorWriter(out io.Writer) *outputCursorWriter {
	if out == nil {
		return nil
	}
	writer := &outputCursorWriter{
		out:                   out,
		frameDumpPath:         os.Getenv("TERMX_FRAME_DUMP"),
		disableVerticalScroll: os.Getenv("TERMX_DISABLE_VERTICAL_SCROLL") == "1",
		useUVRenderer:         globalUVRendererEnabled(false),
		verticalScrollMode:    verticalScrollModeRowsAndRects,
	}
	if writer.useUVRenderer {
		perftrace.Count("cursor_writer.renderer.uv.enabled", 0)
	}
	writer.presenter.verticalScrollMode = writer.effectiveVerticalScrollModeLocked()
	writer.presenter.ownerAwareDeltaEnabled = true
	writer.presenter.debugFaultScrollDropRemainderEvery = parsePositiveIntEnv("TERMX_DEBUG_FAULT_SCROLL_DROP_REMAINDER_EVERY")
	if tty, ok := out.(xterm.File); ok {
		writer.tty = tty
	}
	return writer
}

func (w *outputCursorWriter) SetDebugLogPath(path string) {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.debugLogPath = rendererDebugLogPath(path, w.useUVRenderer)
	w.mu.Unlock()
}

func (w *outputCursorWriter) SetUVRendererEnabled(enabled bool) {
	if w == nil {
		return
	}
	w.mu.Lock()
	if w.useUVRenderer != enabled {
		w.useUVRenderer = enabled
		w.resetUVRendererLocked()
		w.resetUVFallbackStateLocked()
		w.debugLog("cursor_writer.uv.configure", "enabled", enabled)
	}
	if w.useUVRenderer {
		perftrace.Count("cursor_writer.renderer.uv.enabled", 0)
	}
	w.mu.Unlock()
}

func (w *outputCursorWriter) debugLog(event string, kv ...any) {
	if w == nil || w.debugLogPath == "" {
		return
	}
	appendDebugLogLine(w.debugLogPath, event, kv...)
}

func (w *outputCursorWriter) debugLogEnabled() bool {
	return w != nil && w.debugLogPath != ""
}

func debugFrameTail(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	start := len(lines) - 4
	if start < 0 {
		start = 0
	}
	out := make([]string, 0, len(lines)-start)
	for _, line := range lines[start:] {
		plain := strings.TrimRight(xansi.Strip(line), " ")
		if xansi.StringWidth(plain) > 180 {
			plain = xansi.Truncate(plain, 180, "")
		}
		out = append(out, plain)
	}
	return strings.Join(out, "\\n")
}

func debugPayloadPreviewIfEnabled(payload string) string {
	if !debugEnvEnabled("TERMX_RENDERER_DEBUG_PAYLOAD") {
		return ""
	}
	return debugPayloadPreview(payload, 360)
}

func debugPayloadPreview(payload string, limit int) string {
	if payload == "" || limit <= 0 {
		return ""
	}
	preview := payload
	if len(preview) > limit {
		preview = preview[:limit] + "..."
	}
	replacer := strings.NewReplacer(
		"\x1b", "\\x1b",
		"\r", "\\r",
		"\n", "\\n",
		"\t", "\\t",
	)
	return replacer.Replace(preview)
}

func (w *outputCursorWriter) SetVerticalScrollEnabled(enabled bool) {
	if !enabled {
		w.SetVerticalScrollMode(verticalScrollModeNone)
		return
	}
	w.SetVerticalScrollMode(verticalScrollModeRowsAndRects)
}

func (w *outputCursorWriter) SetVerticalScrollMode(mode verticalScrollMode) {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.verticalScrollMode = mode
	w.presenter.verticalScrollMode = w.effectiveVerticalScrollModeLocked()
	w.mu.Unlock()
}

func (w *outputCursorWriter) SetOwnerAwareDeltaEnabled(enabled bool) {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.disableOwnerAwareDelta = !enabled
	w.presenter.ownerAwareDeltaEnabled = w.ownerAwareDeltaEnabledLocked()
	w.mu.Unlock()
}

func (w *outputCursorWriter) SetForceFullFrameLines(enabled bool) {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.forceFullFrameLines = enabled
	w.mu.Unlock()
}

func (w *outputCursorWriter) effectiveVerticalScrollModeLocked() verticalScrollMode {
	if w == nil || w.disableVerticalScroll {
		return verticalScrollModeNone
	}
	return w.verticalScrollMode
}

func (w *outputCursorWriter) ownerAwareDeltaEnabledLocked() bool {
	return w != nil && !w.disableOwnerAwareDelta
}

func (w *outputCursorWriter) SetPerfSampleHook(hook func(string)) {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.perfSampleHook = hook
	w.mu.Unlock()
}

func parsePositiveIntEnv(key string) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return 0
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0
	}
	return value
}

func (w *outputCursorWriter) SetCursorSequence(seq string) {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.cursor = seq
	w.mu.Unlock()
}

func (w *outputCursorWriter) WriteControlSequence(seq string) error {
	if w == nil || w.out == nil || seq == "" {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	_, err := io.WriteString(w.out, seq)
	if err == nil {
		w.appendFrameDumpLocked("control_sequence", seq)
	}
	return err
}

func (w *outputCursorWriter) QueueControlSequenceAfterWrite(seq string) {
	if w == nil || seq == "" {
		return
	}
	w.mu.Lock()
	w.afterWrite = append(w.afterWrite, seq)
	w.mu.Unlock()
}

func (w *outputCursorWriter) HasPendingFrame() bool {
	if w == nil {
		return false
	}
	return w.backlogActive.Load()
}

func (w *outputCursorWriter) SetDrainHook(hook func([]tuiruntime.PendingStreamReady)) {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.drainHook = hook
	w.mu.Unlock()
}

func (w *outputCursorWriter) SetNextFrameStreamReadies(readies []tuiruntime.PendingStreamReady) {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.nextStreamReadies = append(w.nextStreamReadies[:0], readies...)
	w.mu.Unlock()
}

func (w *outputCursorWriter) ResetFrameState() {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.presenter.Reset()
	w.presenter.verticalScrollMode = w.effectiveVerticalScrollModeLocked()
	w.presenter.ownerAwareDeltaEnabled = w.ownerAwareDeltaEnabledLocked()
	w.resetUVRendererLocked()
	w.resetUVFallbackStateLocked()
	w.lastDirectCursor = ""
	w.lastTTYWidth = 0
	w.pending = pendingDirectFrame{}
	w.afterWrite = nil
	w.backlogActive.Store(false)
	w.forceImmediateNextFrame = true
	w.stopFlushTimerLocked()
	w.mu.Unlock()
}

func (w *outputCursorWriter) appendFrameDumpLocked(kind, payload string) {
	if w == nil || w.frameDumpPath == "" || payload == "" {
		return
	}
	f, err := os.OpenFile(w.frameDumpPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()

	header := fmt.Sprintf("--- %s %s len=%d ---\n", kind, time.Now().Format(time.RFC3339Nano), len(payload))
	_, _ = io.WriteString(f, header)
	_, _ = io.WriteString(f, payload)
	_, _ = io.WriteString(f, "\n")
}

func (w *outputCursorWriter) Write(p []byte) (int, error) {
	finish := perftrace.Measure("cursor_writer.bt_write")
	writtenBytes := 0
	defer func() {
		finish(writtenBytes)
	}()
	if w == nil || w.out == nil {
		return 0, nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	frameLike := frameLikeWritePayload(p)
	syncOutput := w.tty != nil
	if syncOutput {
		writtenBytes += len(synchronizedOutputBegin)
		if _, err := io.WriteString(w.out, synchronizedOutputBegin); err != nil {
			return 0, err
		}
	}
	if w.cursorProjected && w.bubbleTeaRestore != "" {
		writtenBytes += len(w.bubbleTeaRestore)
		if _, err := io.WriteString(w.out, w.bubbleTeaRestore); err != nil {
			if syncOutput {
				_, _ = io.WriteString(w.out, synchronizedOutputEnd)
			}
			return 0, err
		}
		w.cursorProjected = false
	}
	cursor := w.cursor
	payload := string(p)
	if cursor != "" {
		payload = stripEmbeddedCursorSequence(payload, cursor)
	}
	if cursor != "" {
		writtenBytes += len(hideHostCursorSequence)
		if _, err := io.WriteString(w.out, hideHostCursorSequence); err != nil {
			if syncOutput {
				_, _ = io.WriteString(w.out, synchronizedOutputEnd)
			}
			return 0, err
		}
	}
	writtenBytes += len(payload)
	n, err := io.WriteString(w.out, payload)
	if err != nil {
		if syncOutput {
			_, _ = io.WriteString(w.out, synchronizedOutputEnd)
		}
		return n, err
	}
	afterWrite := append([]string(nil), w.afterWrite...)
	w.afterWrite = nil
	if frameLike {
		w.bubbleTeaRestore = bubbleTeaRestoreSequence([]byte(payload))
	}
	for _, seq := range afterWrite {
		if seq == "" {
			continue
		}
		writtenBytes += len(seq)
		if _, err := io.WriteString(w.out, seq); err != nil {
			return n, err
		}
	}
	if cursor == "" {
		if syncOutput {
			writtenBytes += len(synchronizedOutputEnd)
			if _, err := io.WriteString(w.out, synchronizedOutputEnd); err != nil {
				return n, err
			}
		}
		return n, nil
	}
	// 中文说明：tmux/zellij 都会在一次输出结束后把真实终端光标留在 pane/
	// 输入框的最终位置。这里即使 Bubble Tea 这次只写了控制序列，也要把 host
	// cursor 重新投回去，否则输入法候选框会跟着框架内部的临时光标跑偏。
	writtenBytes += len(cursor)
	if _, err := io.WriteString(w.out, cursor); err != nil {
		return n, err
	}
	w.cursorProjected = w.bubbleTeaRestore != ""
	if syncOutput {
		writtenBytes += len(synchronizedOutputEnd)
		if _, err := io.WriteString(w.out, synchronizedOutputEnd); err != nil {
			return len(p), err
		}
	}
	return len(p), nil
}

func (w *outputCursorWriter) Read(p []byte) (int, error) {
	if w == nil || w.tty == nil {
		return 0, io.EOF
	}
	return w.tty.Read(p)
}

func (w *outputCursorWriter) Close() error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	w.stopFlushTimerLocked()
	tty := w.tty
	w.mu.Unlock()
	if tty == nil {
		return nil
	}
	return tty.Close()
}

func (w *outputCursorWriter) Fd() uintptr {
	if w == nil || w.tty == nil {
		return 0
	}
	return w.tty.Fd()
}

var _ xterm.File = (*outputCursorWriter)(nil)
