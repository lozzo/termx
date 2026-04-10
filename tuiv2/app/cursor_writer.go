package app

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	xansi "github.com/charmbracelet/x/ansi"
	xterm "github.com/charmbracelet/x/term"
	"github.com/lozzow/termx/perftrace"
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

type frameBackpressureWriter interface {
	frameSequenceWriter
	HasPendingFrame() bool
	SetDrainHook(func())
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

	directAltScreen      bool
	directMouseCell      bool
	directBracketedPaste bool
	lastTTYWidth         int
	lastDirectCursor     string
	lastFlushAt          time.Time
	frameDumpPath        string
	drainHook            func()
	interactiveFlushHint func() bool
	backlogActive        atomic.Bool
}

type pendingDirectFrame struct {
	scheduled  bool
	frame      string
	cursor     string
	afterWrite []string
}

type framePresenter struct {
	lines  []string
	parsed []presentedRow
	ready  bool
}

type presentedRow struct {
	raw   string
	cells []presentedCell
}

type presentedCell struct {
	Content string
	Width   int
	Style   presentedStyle
	Erase   bool
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

var (
	synchronizedOutputBegin = xansi.DECSET(xansi.ModeSynchronizedOutput)
	synchronizedOutputEnd   = xansi.DECRST(xansi.ModeSynchronizedOutput)
	trailingControlSuffixRE = regexp.MustCompile(`(?:\r|\x1b\[[0-9;?]*[ -/]*[@-~])+$`)
	presentedStyleCache     sync.Map
)

const hideHostCursorSequence = "\x1b[?25l"
const presentedResetStyleSequence = "\x1b[0m"

var directFrameBatchDelay = 4 * time.Millisecond
var directFrameIdleThreshold = 12 * time.Millisecond

func (p *framePresenter) Reset() {
	if p == nil {
		return
	}
	p.lines = nil
	p.parsed = nil
	p.ready = false
}

func (p *framePresenter) Present(frame string) string {
	if p == nil {
		return frame
	}
	lines := strings.Split(frame, "\n")
	if !p.ready {
		p.setLines(lines, true)
		p.ready = true
		return frame
	}
	if len(lines) != len(p.lines) {
		p.setLines(lines, true)
		return xansi.EraseEntireDisplay + frame
	}
	if payload := p.presentVerticalScroll(lines); payload != "" {
		p.setLines(lines, true)
		return payload
	}
	payload, changedCount, nextParsed := p.renderChangedRows(lines)
	if changedCount == 0 {
		return ""
	}
	if len(lines) > 6 && len(payload) >= len(frame) {
		p.setLines(lines, true)
		return xansi.EraseEntireDisplay + frame
	}
	p.setLines(lines, false)
	copy(p.parsed, nextParsed)
	return payload
}

func (p *framePresenter) setLines(lines []string, resetParsed bool) {
	if p == nil {
		return
	}
	p.lines = append(p.lines[:0], lines...)
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
	if len(lines) < 6 || len(lines) != len(p.lines) {
		return ""
	}
	plan, ok := detectVerticalScrollPlan(p.lines, lines)
	if !ok {
		return ""
	}
	afterScroll := applyVerticalScrollPlan(p.lines, plan)
	remainder, changedCount := renderChangedRows(afterScroll, lines)
	if changedCount >= plan.reused {
		return ""
	}
	var out strings.Builder
	out.WriteString(renderVerticalScrollPlan(plan, len(lines)))
	out.WriteString(remainder)
	return out.String()
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
	out.WriteString(xansi.DECSTBM(plan.start+1, plan.end+1))
	out.WriteString(xansi.DECRST(xansi.ModeOrigin))
	out.WriteString(xansi.CUP(1, plan.start+1))
	switch plan.direction {
	case scrollUp:
		// Prefer SU/SD over DL/IL here. Some host terminals are more likely to
		// leave stale rows behind when line insert/delete is used inside a
		// constrained scroll region, while the region-scroll sequences map more
		// directly to the intended "viewport moved by N rows" operation.
		out.WriteString(xansi.SU(plan.shift))
	case scrollDown:
		out.WriteString(xansi.SD(plan.shift))
	}
	out.WriteString("\x1b[r")
	out.WriteString(xansi.DECRST(xansi.ModeOrigin))
	return out.String()
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
		out.WriteString(xansi.CUP(1, start+1))
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

func (p *framePresenter) renderChangedRows(next []string) (string, int, []presentedRow) {
	if p == nil || len(next) != len(p.lines) {
		return "", 0, nil
	}
	nextParsed := make([]presentedRow, len(next))
	copy(nextParsed, p.parsed)
	changed := 0
	var out strings.Builder
	for row := range next {
		if next[row] == p.lines[row] {
			continue
		}
		changed++
		prevRow := p.presentedRow(row)
		nextRow := parsePresentedRow(next[row])
		nextParsed[row] = nextRow
		span, ok := renderChangedRowDiff(prevRow, nextRow, row)
		if !ok {
			out.WriteString(xansi.CUP(1, row+1))
			out.WriteString(next[row])
			continue
		}
		out.WriteString(span)
	}
	return out.String(), changed, nextParsed
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

func renderChangedRowDiff(previous, next presentedRow, row int) (string, bool) {
	if previous.raw == next.raw {
		return "", true
	}
	if !canUseSuffixDiff(previous) || !canUseSuffixDiff(next) {
		return "", false
	}
	prevCells := previous.cells
	nextCells := next.cells
	if canUseRunDiff(previous) && canUseRunDiff(next) && len(prevCells) == len(nextCells) {
		if spans, ok := renderChangedRowRuns(prevCells, nextCells, row); ok {
			return spans, true
		}
	}
	return renderChangedRowSuffix(previous, next, row)
}

func canUseSuffixDiff(row presentedRow) bool {
	for _, cell := range row.cells {
		if cell.Erase || cell.Width != 1 {
			return false
		}
	}
	return true
}

func canUseRunDiff(row presentedRow) bool {
	for _, cell := range row.cells {
		if cell.Style != (presentedStyle{}) {
			return false
		}
	}
	return true
}

func renderChangedRowRuns(previous, next []presentedCell, row int) (string, bool) {
	if len(previous) != len(next) {
		return "", false
	}
	prevCol := 1
	nextCol := 1
	runStart := -1
	runStartCol := 1
	var out strings.Builder
	flush := func(end int) {
		if runStart < 0 || runStart >= end {
			return
		}
		out.WriteString(xansi.CUP(runStartCol, row+1))
		out.WriteString(serializePresentedCells(next[runStart:end], runStartCol))
		if end == len(next) {
			out.WriteString(xansi.EraseLineRight)
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
		return "", false
	}
	flush(len(next))
	return out.String(), true
}

func renderChangedRowSuffix(previous, next presentedRow, row int) (string, bool) {
	prevCells := previous.cells
	nextCells := next.cells
	prefixIndex := 0
	prefixWidth := 0
	for prefixIndex < len(prevCells) && prefixIndex < len(nextCells) && prevCells[prefixIndex] == nextCells[prefixIndex] {
		prefixWidth += nextCells[prefixIndex].Width
		prefixIndex++
	}
	if prefixIndex == len(prevCells) && prefixIndex == len(nextCells) {
		return "", true
	}
	span := serializePresentedCells(nextCells[prefixIndex:], prefixWidth+1)
	if span == "" {
		return xansi.CUP(prefixWidth+1, row+1) + xansi.EraseLineRight, true
	}
	return xansi.CUP(prefixWidth+1, row+1) + span + xansi.EraseLineRight, true
}

func parsePresentedRow(row string) presentedRow {
	if row == "" {
		return presentedRow{raw: row}
	}
	parser := xansi.GetParser()
	defer xansi.PutParser(parser)
	state := byte(0)
	rest := row
	style := presentedStyle{}
	cells := make([]presentedCell, 0, xansi.StringWidth(row))
	for len(rest) > 0 {
		seq, width, n, nextState := xansi.DecodeSequence(rest, state, parser)
		if n <= 0 {
			break
		}
		token := string(seq)
		if width > 0 {
			cells = append(cells, presentedCell{Content: token, Width: width, Style: style})
		} else if len(token) > 0 && token[0] == '\x1b' {
			switch xansi.Cmd(parser.Command()).Final() {
			case 'm':
				style = style.withSGR(parser.Params())
			case 'X':
				count, ok := parser.Param(0, 1)
				if !ok || count <= 0 {
					count = 1
				}
				for i := 0; i < count; i++ {
					cells = append(cells, presentedCell{Content: " ", Width: 1, Style: style, Erase: true})
				}
			}
		}
		state = nextState
		rest = rest[n:]
	}
	return presentedRow{raw: row, cells: cells}
}

func serializePresentedCells(cells []presentedCell, startCol int) string {
	if len(cells) == 0 {
		return ""
	}
	var out strings.Builder
	current := presentedStyle{}
	first := true
	cursorCol := maxInt(1, startCol)
	needsReanchor := false
	for _, cell := range cells {
		if needsReanchor {
			out.WriteString(xansi.CHA(cursorCol))
			needsReanchor = false
		}
		if first || cell.Style != current {
			out.WriteString(cell.Style.ansi())
			current = cell.Style
			first = false
		}
		if cell.Erase {
			out.WriteString(xansi.ECH(maxInt(1, cell.Width)))
			cursorCol += maxInt(1, cell.Width)
			needsReanchor = true
			continue
		}
		out.WriteString(cell.Content)
		cursorCol += maxInt(1, cell.Width)
	}
	if current != (presentedStyle{}) {
		out.WriteString(presentedResetStyleSequence)
	}
	return out.String()
}

func (s presentedStyle) ansi() string {
	if cached, ok := presentedStyleCache.Load(s); ok {
		return cached.(string)
	}
	var b strings.Builder
	b.WriteString("\x1b[0")
	if s.FGCode != "" {
		b.WriteByte(';')
		b.WriteString(s.FGCode)
	}
	if s.BGCode != "" {
		b.WriteByte(';')
		b.WriteString(s.BGCode)
	}
	if s.Bold {
		b.WriteString(";1")
	}
	if s.Italic {
		b.WriteString(";3")
	}
	if s.Underline {
		b.WriteString(";4")
	}
	if s.Blink {
		b.WriteString(";5")
	}
	if s.Reverse {
		b.WriteString(";7")
	}
	if s.Strikethrough {
		b.WriteString(";9")
	}
	b.WriteByte('m')
	ansi := b.String()
	presentedStyleCache.Store(s, ansi)
	return ansi
}

func (s presentedStyle) withSGR(params xansi.Params) presentedStyle {
	if len(params) == 0 {
		return presentedStyle{}
	}
	next := s
	for i := 0; i < len(params); i++ {
		param, _, ok := params.Param(i, 0)
		if !ok {
			continue
		}
		switch param {
		case 0:
			next = presentedStyle{}
		case 1:
			next.Bold = true
		case 3:
			next.Italic = true
		case 4:
			next.Underline = true
		case 5:
			next.Blink = true
		case 7:
			next.Reverse = true
		case 9:
			next.Strikethrough = true
		case 22:
			next.Bold = false
		case 23:
			next.Italic = false
		case 24:
			next.Underline = false
		case 25:
			next.Blink = false
		case 27:
			next.Reverse = false
		case 29:
			next.Strikethrough = false
		case 39:
			next.FGCode = ""
		case 49:
			next.BGCode = ""
		case 38, 48:
			modeIndex := i + 1
			mode, _, ok := params.Param(modeIndex, 0)
			if !ok {
				continue
			}
			switch mode {
			case 5:
				value, _, ok := params.Param(i+2, 0)
				if !ok {
					continue
				}
				code := strconv.Itoa(param) + ";5;" + strconv.Itoa(value)
				if param == 38 {
					next.FGCode = code
				} else {
					next.BGCode = code
				}
				i += 2
			case 2:
				r, _, okR := params.Param(i+2, 0)
				g, _, okG := params.Param(i+3, 0)
				b, _, okB := params.Param(i+4, 0)
				if !okR || !okG || !okB {
					continue
				}
				code := strconv.Itoa(param) + ";2;" + strconv.Itoa(r) + ";" + strconv.Itoa(g) + ";" + strconv.Itoa(b)
				if param == 38 {
					next.FGCode = code
				} else {
					next.BGCode = code
				}
				i += 4
			}
		default:
			switch {
			case 30 <= param && param <= 37:
				next.FGCode = strconv.Itoa(param)
			case 90 <= param && param <= 97:
				next.FGCode = strconv.Itoa(param)
			case 40 <= param && param <= 47:
				next.BGCode = strconv.Itoa(param)
			case 100 <= param && param <= 107:
				next.BGCode = strconv.Itoa(param)
			}
		}
	}
	return next
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
	w.lastTTYWidth = 0
	w.lastDirectCursor = ""
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
	w.lastDirectCursor = ""
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
	w.pending.cursor = cursor
	w.pending.afterWrite = append(w.pending.afterWrite, w.afterWrite...)
	w.afterWrite = nil
	w.backlogActive.Store(true)
	if directFrameBatchDelay <= 0 {
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
	time.AfterFunc(directFrameBatchDelay, func() {
		w.flushPendingFrame()
	})
	return nil
}

func (w *outputCursorWriter) flushPendingFrame() {
	if w == nil || w.out == nil {
		return
	}
	w.mu.Lock()
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
	cursor := w.pending.cursor
	afterWrite := append([]string(nil), w.pending.afterWrite...)
	w.pending = pendingDirectFrame{}
	if frame == "" && len(afterWrite) == 0 {
		perftrace.Count("cursor_writer.direct_flush.empty", 0)
		w.backlogActive.Store(false)
		return w.drainHook, nil
	}
	err := w.writeFrameLocked(frame, cursor, afterWrite)
	if err != nil {
		return nil, err
	}
	w.backlogActive.Store(false)
	w.lastFlushAt = time.Now()
	return w.drainHook, nil
}

func (w *outputCursorWriter) SetInteractiveFlushHint(hint func() bool) {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.interactiveFlushHint = hint
	w.mu.Unlock()
}

func (w *outputCursorWriter) shouldFlushDirectFrameImmediatelyLocked() bool {
	if w == nil {
		return false
	}
	if w.interactiveFlushHint != nil && w.interactiveFlushHint() {
		perftrace.Count("cursor_writer.direct_flush.interactive_bypass", 0)
		return true
	}
	if directFrameIdleThreshold <= 0 {
		return true
	}
	if w.lastFlushAt.IsZero() {
		return true
	}
	return time.Since(w.lastFlushAt) >= directFrameIdleThreshold
}

func (w *outputCursorWriter) writeFrameLocked(frame, cursor string, afterWrite []string) error {
	finish := perftrace.Measure("cursor_writer.direct_flush")
	writtenBytes := 0
	defer func() {
		finish(writtenBytes)
	}()
	payload := normalizeFrameForTTY(w.presenter.Present(frame))
	syncOutput := w.tty != nil
	if cursor == "" {
		cursor = hideHostCursorSequence
	}
	if payload == "" && len(afterWrite) == 0 && cursor == w.lastDirectCursor {
		perftrace.Count("cursor_writer.direct_skip", 0)
		return nil
	}

	// 预估总长度，一次性写入以避免多次 syscall 和中间刷新
	estLen := len(payload) + len(cursor) + 64
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
	buf.WriteString(payload)
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
	_, err := io.WriteString(w.out, output)
	if err == nil {
		w.appendFrameDumpLocked("direct_frame", output)
		w.lastDirectCursor = cursor
	}
	return err
}

func (w *outputCursorWriter) fitFrameToTTY(frame string) string {
	if w == nil || w.tty == nil || frame == "" {
		return frame
	}
	width, _, err := xterm.GetSize(w.tty.Fd())
	if err != nil || width <= 0 {
		return frame
	}
	// 如果宽度未变（coordinator 已经按该宽度渲染），跳过逐行截断
	if width == w.lastTTYWidth {
		return frame
	}
	w.lastTTYWidth = width
	return truncateFrameToWidth(frame, width)
}

func truncateFrameToWidth(frame string, width int) string {
	if frame == "" || width <= 0 {
		return frame
	}
	lines := strings.Split(frame, "\n")
	for i := range lines {
		lines[i] = xansi.Truncate(lines[i], width, "")
	}
	return strings.Join(lines, "\n")
}

func normalizeFrameForTTY(frame string) string {
	if frame == "" {
		return frame
	}
	return strings.ReplaceAll(frame, "\n", "\r\n")
}

func newOutputCursorWriter(out io.Writer) *outputCursorWriter {
	if out == nil {
		return nil
	}
	writer := &outputCursorWriter{
		out:           out,
		frameDumpPath: os.Getenv("TERMX_FRAME_DUMP"),
	}
	if tty, ok := out.(xterm.File); ok {
		writer.tty = tty
	}
	return writer
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

func (w *outputCursorWriter) SetDrainHook(hook func()) {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.drainHook = hook
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
	if w == nil || w.tty == nil {
		return nil
	}
	return w.tty.Close()
}

func (w *outputCursorWriter) Fd() uintptr {
	if w == nil || w.tty == nil {
		return 0
	}
	return w.tty.Fd()
}

var _ xterm.File = (*outputCursorWriter)(nil)

func frameLikeWritePayload(p []byte) bool {
	return strings.Trim(xansi.Strip(string(p)), "\r\n") != ""
}

func bubbleTeaRestoreSequence(p []byte) string {
	if len(p) == 0 {
		return ""
	}
	return trailingControlSuffixRE.FindString(string(p))
}

func stripEmbeddedCursorSequence(payload, cursor string) string {
	if payload == "" || cursor == "" {
		return payload
	}
	trailing := bubbleTeaRestoreSequence([]byte(payload))
	body := strings.TrimSuffix(payload, trailing)
	if !strings.HasSuffix(body, cursor) {
		return payload
	}
	return strings.TrimSuffix(body, cursor) + trailing
}
