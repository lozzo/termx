package app

import (
	"io"
	"regexp"
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
	drainHook            func()
	backlogActive        atomic.Bool
}

type pendingDirectFrame struct {
	scheduled  bool
	frame      string
	cursor     string
	afterWrite []string
}

type framePresenter struct {
	lines []string
	ready bool
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
)

const hideHostCursorSequence = "\x1b[?25l"

var directFrameBatchDelay = 4 * time.Millisecond
var directFrameIdleThreshold = 12 * time.Millisecond

func (p *framePresenter) Reset() {
	if p == nil {
		return
	}
	p.lines = nil
	p.ready = false
}

func (p *framePresenter) Present(frame string) string {
	if p == nil {
		return frame
	}
	lines := strings.Split(frame, "\n")
	if !p.ready {
		p.lines = append(p.lines[:0], lines...)
		p.ready = true
		return frame
	}
	if len(lines) != len(p.lines) {
		p.lines = append(p.lines[:0], lines...)
		return xansi.EraseEntireDisplay + frame
	}
	if payload := p.presentVerticalScroll(lines); payload != "" {
		p.lines = append(p.lines[:0], lines...)
		return payload
	}
	payload, changedCount := renderChangedRows(p.lines, lines)
	if changedCount == 0 {
		return ""
	}
	if changedCount*4 >= len(lines)*3 {
		p.lines = append(p.lines[:0], lines...)
		return xansi.EraseEntireDisplay + frame
	}
	p.lines = append(p.lines[:0], lines...)
	return payload
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
		out.WriteString(xansi.DL(plan.shift))
	case scrollDown:
		out.WriteString(xansi.IL(plan.shift))
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

func (w *outputCursorWriter) shouldFlushDirectFrameImmediatelyLocked() bool {
	if w == nil {
		return false
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
	writer := &outputCursorWriter{out: out}
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
