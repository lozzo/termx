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
	"unicode/utf8"

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

type frameLinesWriter interface {
	WriteFrameLines(lines []string, cursor string) error
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

	directAltScreen       bool
	directMouseCell       bool
	directBracketedPaste  bool
	lastTTYWidth          int
	lastDirectCursor      string
	lastFlushAt           time.Time
	frameDumpPath         string
	disableVerticalScroll bool
	drainHook             func()
	interactiveFlushHint  func() bool
	backlogActive         atomic.Bool
}

type pendingDirectFrame struct {
	scheduled  bool
	frame      string
	lines      []string
	cursor     string
	afterWrite []string
}

type framePresenter struct {
	lines                              []string
	parsed                             []presentedRow
	scratchLines                       []string
	scratchParsed                      []presentedRow
	reclaim                            [][]presentedCell
	ready                              bool
	allowVerticalScroll                bool
	fullWidthLines                     bool
	debugFaultScrollDropRemainderEvery int
	verticalScrollCount                int
}

type presentedRow struct {
	raw       string
	cells     []presentedCell
	hasStyled bool
	hasWide   bool
	hasErase  bool
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

var (
	synchronizedOutputBegin = xansi.DECSET(xansi.ModeSynchronizedOutput)
	synchronizedOutputEnd   = xansi.DECRST(xansi.ModeSynchronizedOutput)
	presentedStyleCache     sync.Map
	presentedStyleDiffCache sync.Map
	presentedCellPool       sync.Pool
)

const hideHostCursorSequence = "\x1b[?25l"
const presentedResetStyleSequence = "\x1b[0m"
const maxPooledPresentedCellCapacity = 2048

var directFrameBatchDelay = 4 * time.Millisecond
var directFrameIdleThreshold = 12 * time.Millisecond

func (p *framePresenter) Reset() {
	if p == nil {
		return
	}
	releasePresentedRows(p.parsed)
	p.lines = nil
	p.parsed = nil
	p.scratchLines = nil
	p.scratchParsed = nil
	p.reclaim = nil
	p.ready = false
	p.allowVerticalScroll = true
	p.fullWidthLines = false
	p.verticalScrollCount = 0
}

func (p *framePresenter) Present(frame string) string {
	if p == nil {
		return frame
	}
	lines := splitFrameLines(frame, p.scratchLines[:0])
	return p.presentLines(lines)
}

func (p *framePresenter) PresentLines(lines []string) string {
	if p == nil {
		return strings.Join(lines, "\n")
	}
	return p.presentLines(lines)
}

func (p *framePresenter) presentLines(lines []string) string {
	if !p.ready {
		p.setLines(lines, true)
		p.ready = true
		return strings.Join(lines, "\n")
	}
	if len(lines) != len(p.lines) {
		releasePresentedRows(p.parsed)
		p.setLines(lines, true)
		return xansi.EraseEntireDisplay + strings.Join(lines, "\n")
	}
	if p.allowVerticalScroll {
		if payload := p.presentVerticalScroll(lines); payload != "" {
			releasePresentedRows(p.parsed)
			p.setLines(lines, true)
			return payload
		}
	}
	payload, changedCount, nextParsed, reclaim := p.renderChangedRows(lines)
	if changedCount == 0 {
		return ""
	}
	p.setLines(lines, false)
	copy(p.parsed, nextParsed)
	releasePresentedCellSlices(reclaim)
	if len(lines) > 6 && len(payload) >= joinedLinesLen(lines) {
		return xansi.EraseEntireDisplay + strings.Join(lines, "\n")
	}
	return payload
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

func (p *framePresenter) setLines(lines []string, resetParsed bool) {
	if p == nil {
		return
	}
	previousLines := p.lines
	p.lines = lines
	p.scratchLines = previousLines[:0]
	previousParsed := p.parsed
	if cap(p.scratchParsed) < len(lines) {
		p.parsed = make([]presentedRow, len(lines))
	} else {
		p.parsed = p.scratchParsed[:len(lines)]
	}
	p.scratchParsed = previousParsed[:0]
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
	p.verticalScrollCount++
	if p.debugFaultScrollDropRemainderEvery > 0 && p.verticalScrollCount%p.debugFaultScrollDropRemainderEvery == 0 {
		return out.String()
	}
	out.WriteString(remainder)
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

func (p *framePresenter) renderChangedRows(next []string) (string, int, []presentedRow, [][]presentedCell) {
	if p == nil || len(next) != len(p.lines) {
		return "", 0, nil, nil
	}
	nextParsed := ensurePresentedRows(p.scratchParsed, len(next))
	copy(nextParsed, p.parsed)
	changed := 0
	reclaim := p.reclaim[:0]
	var out strings.Builder
	out.Grow(len(next) * 16)
	for row := range next {
		if next[row] == p.lines[row] {
			continue
		}
		changed++
		prevRow := p.presentedRow(row)
		nextRow := parsePresentedRow(next[row])
		nextParsed[row] = nextRow
		if len(prevRow.cells) > 0 {
			reclaim = append(reclaim, prevRow.cells)
		}
		if !renderChangedRowDiff(&out, prevRow, nextRow, row, p.fullWidthLines) {
			writeCUP(&out, 1, row+1)
			out.WriteString(next[row])
		}
	}
	return out.String(), changed, nextParsed, reclaim
}

func ensurePresentedRows(rows []presentedRow, size int) []presentedRow {
	if cap(rows) < size {
		return make([]presentedRow, size)
	}
	rows = rows[:size]
	clear(rows)
	return rows
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
	if previous.hasErase || previous.hasWide || next.hasErase || next.hasWide {
		return false
	}
	prevCells := previous.cells
	nextCells := next.cells
	if !previous.hasStyled && !next.hasStyled && len(prevCells) == len(nextCells) {
		if renderChangedRowRuns(out, prevCells, nextCells, row, fullWidthLines, rowOwnsLineEnd(next)) {
			return true
		}
	}
	return renderChangedRowSuffix(out, previous, next, row, fullWidthLines, rowOwnsLineEnd(next))
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

func rowOwnsLineEnd(row presentedRow) bool {
	if row.raw == "" {
		return false
	}
	return strings.Contains(row.raw, "\x1b[K")
}

func writeOwnedLineEndClear(out *strings.Builder, style presentedStyle) {
	if out == nil {
		return
	}
	if style != (presentedStyle{}) {
		out.WriteString(presentedStyleDiffANSI(presentedStyle{}, style))
	}
	writeECH(out, 1)
	if style != (presentedStyle{}) {
		out.WriteString(presentedResetStyleSequence)
	}
}

func parsePresentedRow(row string) presentedRow {
	if row == "" {
		return presentedRow{raw: row}
	}
	if fast, ok := parsePresentedRowASCII(row); ok {
		return fast
	}
	return parsePresentedRowGeneric(row)
}

func parsePresentedRowASCII(row string) (presentedRow, bool) {
	style := presentedStyle{}
	cells := acquirePresentedCells(len(row))
	hasStyled := false
	hasErase := false
	fail := func() (presentedRow, bool) {
		releasePresentedCells(cells)
		return presentedRow{}, false
	}
	for i := 0; i < len(row); {
		b := row[i]
		if b == '\x1b' {
			if i+1 >= len(row) || row[i+1] != '[' {
				return fail()
			}
			j := i + 2
			for j < len(row) && (row[j] < '@' || row[j] > '~') {
				if row[j] >= utf8.RuneSelf {
					return fail()
				}
				j++
			}
			if j >= len(row) {
				return fail()
			}
			final := row[j]
			params := row[i+2 : j]
			switch final {
			case 'm':
				next, ok := style.withSGRASCII(params)
				if !ok {
					return fail()
				}
				style = next
			case 'X':
				count, ok := parseCSIIntASCII(params, 1)
				if !ok {
					return fail()
				}
				hasErase = true
				if style != (presentedStyle{}) {
					hasStyled = true
				}
				for k := 0; k < count; k++ {
					cells = append(cells, presentedCell{Content: " ", Width: 1, Style: style, Erase: true})
				}
			}
			i = j + 1
			continue
		}
		if b >= utf8.RuneSelf || b < 0x20 || b == 0x7f {
			return fail()
		}
		if style != (presentedStyle{}) {
			hasStyled = true
		}
		cells = append(cells, presentedCell{Content: row[i : i+1], Width: 1, Style: style})
		i++
	}
	return presentedRow{raw: row, cells: cells, hasStyled: hasStyled, hasErase: hasErase}, true
}

func parsePresentedRowGeneric(row string) presentedRow {
	parser := xansi.GetParser()
	defer xansi.PutParser(parser)
	state := byte(0)
	rest := row
	style := presentedStyle{}
	cells := acquirePresentedCells(xansi.StringWidth(row))
	hasStyled := false
	hasWide := false
	hasErase := false
	for len(rest) > 0 {
		seq, width, n, nextState := xansi.DecodeSequence(rest, state, parser)
		if n <= 0 {
			break
		}
		token := string(seq)
		if width > 0 {
			if style != (presentedStyle{}) {
				hasStyled = true
			}
			if width != 1 {
				hasWide = true
			}
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
				hasErase = true
				if style != (presentedStyle{}) {
					hasStyled = true
				}
				for i := 0; i < count; i++ {
					cells = append(cells, presentedCell{Content: " ", Width: 1, Style: style, Erase: true})
				}
			}
		}
		state = nextState
		rest = rest[n:]
	}
	return presentedRow{raw: row, cells: cells, hasStyled: hasStyled, hasWide: hasWide, hasErase: hasErase}
}

func acquirePresentedCells(capHint int) []presentedCell {
	if capHint < 0 {
		capHint = 0
	}
	if pooled, ok := presentedCellPool.Get().([]presentedCell); ok {
		if cap(pooled) >= capHint {
			return pooled[:0]
		}
		releasePresentedCells(pooled)
	}
	return make([]presentedCell, 0, capHint)
}

func releasePresentedRows(rows []presentedRow) {
	for i := range rows {
		releasePresentedCells(rows[i].cells)
		rows[i] = presentedRow{}
	}
}

func releasePresentedCellSlices(cells [][]presentedCell) {
	for _, cellSlice := range cells {
		releasePresentedCells(cellSlice)
	}
}

func releasePresentedCells(cells []presentedCell) {
	if len(cells) == 0 || cap(cells) > maxPooledPresentedCellCapacity {
		return
	}
	clear(cells)
	presentedCellPool.Put(cells[:0])
}

func parseCSIIntASCII(raw string, def int) (int, bool) {
	if raw == "" {
		return def, true
	}
	value := 0
	for i := 0; i < len(raw); i++ {
		if raw[i] == ';' {
			break
		}
		if raw[i] < '0' || raw[i] > '9' {
			return def, false
		}
		value = value*10 + int(raw[i]-'0')
	}
	return value, true
}

func (s presentedStyle) withSGRASCII(raw string) (presentedStyle, bool) {
	var local [8]int
	params := local[:0]
	value := 0
	hasValue := false
	for i := 0; i <= len(raw); i++ {
		if i == len(raw) || raw[i] == ';' {
			if hasValue {
				params = append(params, value)
			} else {
				params = append(params, 0)
			}
			value = 0
			hasValue = false
			continue
		}
		if raw[i] < '0' || raw[i] > '9' {
			return presentedStyle{}, false
		}
		value = value*10 + int(raw[i]-'0')
		hasValue = true
	}
	return s.withSGRInts(params), true
}

func writePresentedCells(out *strings.Builder, cells []presentedCell, startCol int) presentedStyle {
	if out == nil || len(cells) == 0 {
		return presentedStyle{}
	}
	current := presentedStyle{}
	first := true
	cursorCol := maxInt(1, startCol)
	needsReanchor := false
	for _, cell := range cells {
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
		out.WriteString(cell.Content)
		cursorCol += maxInt(1, cell.Width)
	}
	if current != (presentedStyle{}) {
		out.WriteString(presentedResetStyleSequence)
	}
	return current
}

func writeCUP(out *strings.Builder, col, row int) {
	writeCSI(out, 'H', row, col)
}

func writeCHA(out *strings.Builder, col int) {
	writeCSI(out, 'G', col)
}

func writeECH(out *strings.Builder, count int) {
	writeCSI(out, 'X', count)
}

func writeCSI(out *strings.Builder, final byte, params ...int) {
	if out == nil {
		return
	}
	out.WriteByte('\x1b')
	out.WriteByte('[')
	for i, param := range params {
		if i > 0 {
			out.WriteByte(';')
		}
		writeBuilderInt(out, param)
	}
	out.WriteByte(final)
}

func writeBuilderInt(out *strings.Builder, value int) {
	if out == nil {
		return
	}
	var scratch [24]byte
	buf := strconv.AppendInt(scratch[:0], int64(value), 10)
	_, _ = out.Write(buf)
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

type presentedStyleTransitionKey struct {
	From presentedStyle
	To   presentedStyle
}

func presentedStyleDiffANSI(from, to presentedStyle) string {
	if from == to {
		return ""
	}
	key := presentedStyleTransitionKey{From: from, To: to}
	if cached, ok := presentedStyleDiffCache.Load(key); ok {
		return cached.(string)
	}
	ansi := buildPresentedStyleDiffANSI(from, to)
	presentedStyleDiffCache.Store(key, ansi)
	return ansi
}

func buildPresentedStyleDiffANSI(from, to presentedStyle) string {
	if from == to {
		return ""
	}
	if to == (presentedStyle{}) {
		return presentedResetStyleSequence
	}
	var b strings.Builder
	b.WriteString("\x1b[")
	first := true
	appendPresentedStyleToggle(&b, &first, from.Bold, to.Bold, "1", "22")
	appendPresentedStyleToggle(&b, &first, from.Italic, to.Italic, "3", "23")
	appendPresentedStyleToggle(&b, &first, from.Underline, to.Underline, "4", "24")
	appendPresentedStyleToggle(&b, &first, from.Blink, to.Blink, "5", "25")
	appendPresentedStyleToggle(&b, &first, from.Reverse, to.Reverse, "7", "27")
	appendPresentedStyleToggle(&b, &first, from.Strikethrough, to.Strikethrough, "9", "29")
	if from.FGCode != to.FGCode {
		if to.FGCode == "" {
			appendPresentedStyleCode(&b, &first, "39")
		} else {
			appendPresentedStyleCode(&b, &first, to.FGCode)
		}
	}
	if from.BGCode != to.BGCode {
		if to.BGCode == "" {
			appendPresentedStyleCode(&b, &first, "49")
		} else {
			appendPresentedStyleCode(&b, &first, to.BGCode)
		}
	}
	if first {
		return ""
	}
	b.WriteByte('m')
	return b.String()
}

func appendPresentedStyleToggle(b *strings.Builder, first *bool, from, to bool, onCode, offCode string) {
	if from == to {
		return
	}
	if to {
		appendPresentedStyleCode(b, first, onCode)
		return
	}
	appendPresentedStyleCode(b, first, offCode)
}

func appendPresentedStyleCode(b *strings.Builder, first *bool, code string) {
	if b == nil || first == nil || code == "" {
		return
	}
	if !*first {
		b.WriteByte(';')
	}
	b.WriteString(code)
	*first = false
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
				next.FGCode = ansiSimpleColorCode(param)
			case 90 <= param && param <= 97:
				next.FGCode = ansiSimpleColorCode(param)
			case 40 <= param && param <= 47:
				next.BGCode = ansiSimpleColorCode(param)
			case 100 <= param && param <= 107:
				next.BGCode = ansiSimpleColorCode(param)
			}
		}
	}
	return next
}

func (s presentedStyle) withSGRInts(params []int) presentedStyle {
	if len(params) == 0 {
		return presentedStyle{}
	}
	next := s
	for i := 0; i < len(params); i++ {
		param := params[i]
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
			if i+1 >= len(params) {
				continue
			}
			mode := params[i+1]
			switch mode {
			case 5:
				if i+2 >= len(params) {
					continue
				}
				value := params[i+2]
				code := strconv.Itoa(param) + ";5;" + strconv.Itoa(value)
				if param == 38 {
					next.FGCode = code
				} else {
					next.BGCode = code
				}
				i += 2
			case 2:
				if i+4 >= len(params) {
					continue
				}
				r := params[i+2]
				g := params[i+3]
				b := params[i+4]
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
				next.FGCode = ansiSimpleColorCode(param)
			case 90 <= param && param <= 97:
				next.FGCode = ansiSimpleColorCode(param)
			case 40 <= param && param <= 47:
				next.BGCode = ansiSimpleColorCode(param)
			case 100 <= param && param <= 107:
				next.BGCode = ansiSimpleColorCode(param)
			}
		}
	}
	return next
}

func ansiSimpleColorCode(param int) string {
	switch param {
	case 30:
		return "30"
	case 31:
		return "31"
	case 32:
		return "32"
	case 33:
		return "33"
	case 34:
		return "34"
	case 35:
		return "35"
	case 36:
		return "36"
	case 37:
		return "37"
	case 40:
		return "40"
	case 41:
		return "41"
	case 42:
		return "42"
	case 43:
		return "43"
	case 44:
		return "44"
	case 45:
		return "45"
	case 46:
		return "46"
	case 47:
		return "47"
	case 90:
		return "90"
	case 91:
		return "91"
	case 92:
		return "92"
	case 93:
		return "93"
	case 94:
		return "94"
	case 95:
		return "95"
	case 96:
		return "96"
	case 97:
		return "97"
	case 100:
		return "100"
	case 101:
		return "101"
	case 102:
		return "102"
	case 103:
		return "103"
	case 104:
		return "104"
	case 105:
		return "105"
	case 106:
		return "106"
	case 107:
		return "107"
	default:
		return strconv.Itoa(param)
	}
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
	w.presenter.allowVerticalScroll = !w.disableVerticalScroll
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
	w.pending.lines = nil
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

func (w *outputCursorWriter) WriteFrameLines(lines []string, cursor string) error {
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
	w.pending.lines = w.fitLinesToTTY(lines)
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
	lines := w.pending.lines
	cursor := w.pending.cursor
	afterWrite := append([]string(nil), w.pending.afterWrite...)
	w.pending = pendingDirectFrame{}
	if frame == "" && len(lines) == 0 && len(afterWrite) == 0 {
		perftrace.Count("cursor_writer.direct_flush.empty", 0)
		w.backlogActive.Store(false)
		return w.drainHook, nil
	}
	err := error(nil)
	if len(lines) > 0 {
		err = w.writeFrameLinesLocked(lines, cursor, afterWrite)
	} else {
		err = w.writeFrameLocked(frame, cursor, afterWrite)
	}
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
	presentFinish := perftrace.Measure("cursor_writer.present")
	w.presenter.fullWidthLines = false
	payload := w.presenter.Present(frame)
	presentFinish(len(payload))
	syncOutput := w.tty != nil
	if cursor == "" {
		cursor = hideHostCursorSequence
	}
	if payload == "" && len(afterWrite) == 0 && cursor == w.lastDirectCursor {
		perftrace.Count("cursor_writer.direct_skip", 0)
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
	writeNormalizedFrame(&buf, payload)
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
	_, err := io.WriteString(w.out, output)
	ioFinish(writtenBytes)
	if err == nil {
		w.appendFrameDumpLocked("direct_frame", output)
		w.lastDirectCursor = cursor
	}
	return err
}

func (w *outputCursorWriter) writeFrameLinesLocked(lines []string, cursor string, afterWrite []string) error {
	finish := perftrace.Measure("cursor_writer.direct_flush")
	writtenBytes := 0
	defer func() {
		finish(writtenBytes)
	}()
	presentFinish := perftrace.Measure("cursor_writer.present")
	w.presenter.fullWidthLines = true
	payload := w.presenter.PresentLines(stripTrailingEraseLineRight(lines))
	presentFinish(len(payload))
	syncOutput := w.tty != nil
	if cursor == "" {
		cursor = hideHostCursorSequence
	}
	if payload == "" && len(afterWrite) == 0 && cursor == w.lastDirectCursor {
		perftrace.Count("cursor_writer.direct_skip", 0)
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
	writeNormalizedFrame(&buf, payload)
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
	_, err := io.WriteString(w.out, output)
	ioFinish(writtenBytes)
	if err == nil {
		w.appendFrameDumpLocked("direct_frame", output)
		w.lastDirectCursor = cursor
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
	}
	writer.presenter.allowVerticalScroll = !writer.disableVerticalScroll
	writer.presenter.debugFaultScrollDropRemainderEvery = parsePositiveIntEnv("TERMX_DEBUG_FAULT_SCROLL_DROP_REMAINDER_EVERY")
	if tty, ok := out.(xterm.File); ok {
		writer.tty = tty
	}
	return writer
}

func (w *outputCursorWriter) SetVerticalScrollEnabled(enabled bool) {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.presenter.allowVerticalScroll = enabled && !w.disableVerticalScroll
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
