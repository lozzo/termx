package vt

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/parser"
)

func (e *Emulator) tryFastSGRText(data []byte) bool {
	if len(data) == 0 || !e.canFastPrintASCII() || e.IsAltScreen() || e.isModeSet(ansi.ModeLineFeedNewLine) {
		return false
	}
	if !e.isModeSet(ansi.ModeAutoWrap) {
		return false
	}
	if e.scr == nil || e.scr.buf == nil {
		return false
	}
	if e.scr.damage != nil && !e.scr.damage.scrollbackOnly {
		return false
	}
	width := e.scr.Width()
	height := e.scr.Height()
	if width <= 0 || height <= 0 {
		return false
	}
	scroll := e.scr.ScrollRegion()
	if scroll.Min.X != 0 || scroll.Min.Y != 0 || scroll.Max.X != width || scroll.Max.Y != height {
		return false
	}
	rows := make([]uv.Line, height)
	wrapped := make([]bool, height)
	used := make([]int, height)
	for y := 0; y < height; y++ {
		rows[y] = cloneFastSGRLine(e.scr.buf.Line(y), width)
		wrapped[y] = e.scr.LineWrapped(y)
		used[y] = e.scr.LineUsed(y)
	}
	x, cursorY := e.scr.CursorPosition()
	if x < 0 {
		x = 0
	}
	if x >= width {
		x = width - 1
	}
	if cursorY < 0 {
		cursorY = 0
	}
	if cursorY >= height {
		cursorY = height - 1
	}
	style := e.scr.cursorPen()
	link := e.scr.cursorLink()
	nextPen := style
	atPhantom := e.atPhantom
	lastChar := e.lastChar
	scrollY := 0
	scrollback := e.Scrollback()
	scrollbackRows := []fastSGRScrollbackRow(nil)
	scrollbackDamages := []ScrollbackDamage(nil)

	flushScrollbackRow := func(row uv.Line, wasWrapped bool) {
		if e.scr.damage != nil && e.scr.damage.scrollbackOnly {
			damage := ScrollbackDamage{
				Y:       scrollY,
				Wrapped: wasWrapped,
			}
			if runs, ok := compactASCIIStyleRuns(row); ok && len(runs) > 0 {
				damage.Runs = runs
			} else {
				damage.ASCII = true
				damage.Text = lineASCIIText(row)
			}
			scrollbackDamages = append(scrollbackDamages, damage)
			if scrollback != nil {
				scrollbackRows = append(scrollbackRows, fastSGRScrollbackRow{line: row, wrapped: wasWrapped})
			}
			scrollY++
			return
		}
		scrollbackRows = append(scrollbackRows, fastSGRScrollbackRow{line: row, wrapped: wasWrapped})
		scrollY++
	}

	linefeed := func() {
		if cursorY == height-1 {
			scrolled := rows[0]
			scrolledWrapped := wrapped[0]
			scrolledUsed := clampLocalInt(used[0], 0, len(scrolled))
			flushScrollbackRow(scrolled[:scrolledUsed], scrolledWrapped)
			copy(rows, rows[1:])
			copy(wrapped, wrapped[1:])
			copy(used, used[1:])
			rows[height-1] = fastSGRBlankLine(width, style)
			wrapped[height-1] = false
			used[height-1] = 0
			return
		}
		cursorY++
	}
	printRun := func(run []byte) {
		for len(run) > 0 {
			if atPhantom {
				wrapped[cursorY] = true
				linefeed()
				x = 0
				atPhantom = false
			}
			available := width - x
			if available <= 0 {
				atPhantom = true
				continue
			}
			count := min(len(run), available)
			row := rows[cursorY]
			for i, b := range run[:count] {
				row[x+i] = uv.Cell{
					Content: printableASCIIStrings[b],
					Width:   1,
					Style:   style,
					Link:    link,
				}
			}
			used[cursorY] = max(used[cursorY], x+count)
			lastChar = rune(run[count-1])
			run = run[count:]
			if x+count >= width {
				x = width - 1
				atPhantom = true
			} else {
				x += count
			}
		}
	}

	var paramsScratch []int
	for i := 0; i < len(data); {
		start := i
		for i < len(data) && isPrintableASCII(data[i]) {
			i++
		}
		if i > start {
			printRun(data[start:i])
			continue
		}
		switch data[i] {
		case '\r':
			x = 0
			atPhantom = false
			i++
		case '\n':
			linefeed()
			atPhantom = false
			i++
		case ansi.ESC:
			next, ok := fastSGRSequenceEnd(data, i)
			if !ok {
				return false
			}
			paramsScratch = paramsScratch[:0]
			params, ok := parseSGRParams(data[i+2:next], paramsScratch)
			if !ok || !canApplyFastSGRStyle(params) {
				return false
			}
			readFastSGRStyle(params, &nextPen)
			style = nextPen
			i = next + 1
		default:
			return false
		}
	}

	for y := 0; y < height; y++ {
		row := rows[y]
		line := e.scr.buf.Line(y)
		if line == nil || len(line) != width {
			for col := 0; col < width; col++ {
				cell := row[col]
				e.scr.buf.Buffer.SetCell(col, y, &cell)
			}
		} else {
			copy(line, row)
		}
		e.scr.wrapped[y] = wrapped[y]
		e.scr.used[y] = used[y]
		e.scr.buf.TouchLine(0, y, width)
	}
	if scrollback != nil {
		for _, row := range scrollbackRows {
			scrollback.PushWrapped(row.line, row.wrapped)
		}
	}
	for _, damage := range scrollbackDamages {
		e.scr.damage.record(damage)
	}
	e.scr.cur.Pen = nextPen
	e.scr.setCursor(x, cursorY, false)
	e.atPhantom = atPhantom
	e.lastChar = lastChar
	e.lastState = parser.GroundState
	return true
}

type fastSGRScrollbackRow struct {
	line    uv.Line
	wrapped bool
}

func fastSGRTextEstimatedRows(data []byte, width int) int {
	if len(data) == 0 || width <= 0 {
		return 0
	}
	rows := 1
	col := 0
	for _, b := range data {
		switch {
		case isPrintableASCII(b):
			col++
			if col >= width {
				rows++
				col = 0
			}
		case b == '\n':
			rows++
			col = 0
		case b == '\r':
			col = 0
		}
	}
	return rows
}

func cloneFastSGRLine(line uv.Line, width int) uv.Line {
	if width <= 0 {
		return nil
	}
	out := make(uv.Line, width)
	copy(out, line)
	return out
}

func fastSGRBlankLine(width int, style uv.Style) uv.Line {
	line := make(uv.Line, width)
	cell := uv.EmptyCell
	cell.Style.Bg = style.Bg
	for i := range line {
		line[i] = cell
	}
	return line
}

func lineASCIIText(line uv.Line) string {
	if text, ok := compactASCIIPlainLine(line); ok {
		return text
	}
	out := make([]byte, len(line))
	for i := range line {
		if len(line[i].Content) == 1 && line[i].Content[0] < 0x80 {
			out[i] = line[i].Content[0]
		} else {
			out[i] = ' '
		}
	}
	return string(out)
}

func fastSGRSequenceEnd(data []byte, start int) (int, bool) {
	if start+2 >= len(data) || data[start] != ansi.ESC || data[start+1] != '[' {
		return 0, false
	}
	for i := start + 2; i < len(data); i++ {
		b := data[i]
		if b == 'm' {
			return i, true
		}
		if (b >= '0' && b <= '9') || b == ';' {
			continue
		}
		return 0, false
	}
	return 0, false
}

func parseSGRParams(data []byte, scratch []int) ([]int, bool) {
	if len(data) == 0 {
		return scratch, true
	}
	start := 0
	for start <= len(data) {
		end := start
		for end < len(data) && data[end] != ';' {
			end++
		}
		if end == start {
			scratch = append(scratch, 0)
		} else {
			value := 0
			for _, b := range data[start:end] {
				if b < '0' || b > '9' {
					return nil, false
				}
				value = value*10 + int(b-'0')
			}
			if value < 0 {
				return nil, false
			}
			scratch = append(scratch, value)
		}
		if end == len(data) {
			break
		}
		start = end + 1
	}
	return scratch, true
}

func canApplyFastSGRStyle(params []int) bool {
	for i := 0; i < len(params); i++ {
		switch p := params[i]; p {
		case 0, 1, 2, 3, 4, 5, 6, 7, 8, 9,
			22, 23, 24, 25, 27, 28, 29,
			30, 31, 32, 33, 34, 35, 36, 37, 39,
			40, 41, 42, 43, 44, 45, 46, 47, 49,
			90, 91, 92, 93, 94, 95, 96, 97,
			100, 101, 102, 103, 104, 105, 106, 107:
		case 38, 48:
			if i+2 >= len(params) || params[i+1] != 5 || params[i+2] < 0 || params[i+2] > 255 {
				return false
			}
			i += 2
		default:
			return false
		}
	}
	return true
}

func readFastSGRStyle(params []int, pen *uv.Style) {
	if len(params) == 0 {
		*pen = uv.Style{}
		return
	}
	for i := 0; i < len(params); i++ {
		switch p := params[i]; p {
		case 0:
			*pen = uv.Style{}
		case 1:
			pen.Attrs |= uv.AttrBold
		case 2:
			pen.Attrs |= uv.AttrFaint
		case 3:
			pen.Attrs |= uv.AttrItalic
		case 4:
			pen.Underline = uv.UnderlineStyleSingle
		case 5:
			pen.Attrs |= uv.AttrBlink
		case 6:
			pen.Attrs |= uv.AttrRapidBlink
		case 7:
			pen.Attrs |= uv.AttrReverse
		case 8:
			pen.Attrs |= uv.AttrConceal
		case 9:
			pen.Attrs |= uv.AttrStrikethrough
		case 22:
			pen.Attrs &^= (uv.AttrBold | uv.AttrFaint)
		case 23:
			pen.Attrs &^= uv.AttrItalic
		case 24:
			pen.Underline = uv.UnderlineStyleNone
		case 25:
			pen.Attrs &^= (uv.AttrBlink | uv.AttrRapidBlink)
		case 27:
			pen.Attrs &^= uv.AttrReverse
		case 28:
			pen.Attrs &^= uv.AttrConceal
		case 29:
			pen.Attrs &^= uv.AttrStrikethrough
		case 30, 31, 32, 33, 34, 35, 36, 37:
			pen.Fg = ansi.Black + ansi.BasicColor(p-30)
		case 38:
			if i+2 < len(params) && params[i+1] == 5 {
				pen.Fg = ansi.IndexedColor(params[i+2])
				i += 2
			}
		case 39:
			pen.Fg = nil
		case 40, 41, 42, 43, 44, 45, 46, 47:
			pen.Bg = ansi.Black + ansi.BasicColor(p-40)
		case 48:
			if i+2 < len(params) && params[i+1] == 5 {
				pen.Bg = ansi.IndexedColor(params[i+2])
				i += 2
			}
		case 49:
			pen.Bg = nil
		case 90, 91, 92, 93, 94, 95, 96, 97:
			pen.Fg = ansi.BrightBlack + ansi.BasicColor(p-90)
		case 100, 101, 102, 103, 104, 105, 106, 107:
			pen.Bg = ansi.BrightBlack + ansi.BasicColor(p-100)
		default:
			return
		}
	}
}
