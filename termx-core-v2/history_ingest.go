package termxcorev2

import (
	"strconv"
	"strings"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/termx-core-v2/history"
)

type historyOutputSegment struct {
	Cells                    []history.Cell
	Seal                     bool
	CarriageReturn           bool
	CursorForward            bool
	CursorBackward           bool
	CursorHorizontalAbsolute bool
	CursorUp                 bool
	CursorDown               bool
	CursorPosition           bool
	EraseInLine              bool
	EraseInDisplay           bool
	SetTailFill              bool
	SwitchAltScreen          bool
	EnterAltScreen           bool
	Count                    int
	Row                      int
	Column                   int
	EraseMode                int
	Style                    history.CellStyle
}

func parseHistoryOutput(output string) []historyOutputSegment {
	parser := historyANSIParser{}
	return parser.Parse(output)
}

type historyANSIParser struct {
	style              history.CellStyle
	linkURL            string
	linkArgs           string
	buffer             strings.Builder
	col                int
	pending            string
	segments           []historyOutputSegment
	screenCols         int
	screenRows         int
	screenRow          int
	screenCol          int
	atPhantom          bool
	rowFootprintStyle  history.CellStyle
	rowFootprintActive bool
}

// historyANSIParser 只解析会成为 logical-line payload 的文本样式元数据。
// 光标移动、清屏、alt-screen 等 VT 语义必须进入后续 EventRouter，不能从
// live surface 或这个 parser 的文本投影反推 committed history。
func (parser *historyANSIParser) Parse(output string) []historyOutputSegment {
	parser.segments = parser.segments[:0]
	output = normalizeTerminalOutput(output)
	if parser.pending != "" {
		output = parser.pending + output
		parser.pending = ""
	}
	for output != "" {
		switch {
		case strings.HasPrefix(output, "\x1b["):
			consumed := parser.consumeCSI(output)
			if consumed > 0 {
				output = output[consumed:]
				continue
			}
			parser.pending = output
			output = ""
		case strings.HasPrefix(output, "\x1b]"):
			consumed := parser.consumeOSC(output)
			if consumed > 0 {
				output = output[consumed:]
				continue
			}
			parser.pending = output
			output = ""
		case isStringControlStart(output):
			consumed := consumeStringControl(output)
			if consumed > 0 {
				output = output[consumed:]
				continue
			}
			parser.pending = output
			output = ""
		case output[0] == '\x1b':
			consumed := consumeEscapeSequence(output)
			if consumed <= 0 {
				parser.pending = output
				output = ""
				continue
			}
			output = output[consumed:]
		case output[0] == '\r':
			parser.flush()
			parser.segments = append(parser.segments, historyOutputSegment{CarriageReturn: true})
			parser.col = 0
			parser.screenCol = 0
			parser.atPhantom = false
			output = output[1:]
		case output[0] == '\b':
			parser.flush()
			parser.segments = append(parser.segments, historyOutputSegment{CursorBackward: true, Count: 1})
			if parser.col > 0 {
				parser.col--
			}
			if parser.screenCol > 0 {
				parser.screenCol--
			}
			parser.atPhantom = false
			output = output[1:]
		case output[0] == '\n':
			parser.flush()
			parser.finishPhysicalRow()
			parser.segments = append(parser.segments, historyOutputSegment{Seal: true})
			parser.col = 0
			parser.advancePhysicalLine(parser.style)
			output = output[1:]
		case output[0] == '\t':
			parser.writeTab()
			output = output[1:]
		default:
			next := strings.IndexAny(output, "\x1b\r\b\n\t")
			if next < 0 {
				parser.writeText(output)
				output = ""
				continue
			}
			parser.writeText(output[:next])
			output = output[next:]
		}
	}
	parser.flush()
	return cloneHistoryOutputSegments(parser.segments)
}

func (parser *historyANSIParser) SetScreenSize(cols int, rows int) {
	sizeChanged := parser.screenCols != cols || parser.screenRows != rows
	parser.screenCols = cols
	parser.screenRows = rows
	if parser.screenCols <= 0 || parser.screenRows <= 0 {
		parser.screenRow = 0
		parser.screenCol = 0
		parser.atPhantom = false
		parser.rowFootprintActive = false
		return
	}
	if !sizeChanged {
		return
	}
	if parser.screenRow >= parser.screenRows {
		parser.screenRow = parser.screenRows - 1
	}
	if parser.screenRow < 0 {
		parser.screenRow = 0
	}
	if parser.screenCol >= parser.screenCols {
		parser.screenCol = parser.screenCols - 1
	}
	if parser.screenCol < 0 {
		parser.screenCol = 0
	}
	parser.atPhantom = false
	parser.rowFootprintActive = false
}

func (parser *historyANSIParser) consumeCSI(input string) int {
	end := -1
	for i := 2; i < len(input); i++ {
		b := input[i]
		if b >= 0x40 && b <= 0x7e {
			end = i
			break
		}
	}
	if end < 0 {
		return 0
	}
	final := input[end]
	if (final == 'h' || final == 'l') && strings.HasPrefix(input[2:end], "?") {
		parser.flush()
		if enter, ok := parseAltScreenMode(input[2:end], final); ok {
			parser.segments = append(parser.segments, historyOutputSegment{
				SwitchAltScreen: true,
				EnterAltScreen:  enter,
			})
		}
		return end + 1
	}
	if final == 'J' {
		parser.flush()
		mode := 0
		params := parseSGRParams(input[2:end])
		if len(params) > 0 {
			mode = params[0]
		}
		parser.segments = append(parser.segments, historyOutputSegment{EraseInDisplay: true, EraseMode: mode})
		return end + 1
	}
	if final == 'K' {
		parser.flush()
		mode := 0
		params := parseSGRParams(input[2:end])
		if len(params) > 0 {
			mode = params[0]
		}
		parser.segments = append(parser.segments, historyOutputSegment{EraseInLine: true, EraseMode: mode, Style: parser.style})
		if mode == 0 || mode == 2 {
			parser.rowFootprintActive = false
		}
		return end + 1
	}
	switch final {
	case 'A':
		parser.flush()
		count := firstCSIParam(input[2:end], 1)
		parser.segments = append(parser.segments, historyOutputSegment{CursorUp: true, Count: count})
		parser.movePhysicalRowBy(-count)
		return end + 1
	case 'B':
		parser.flush()
		count := firstCSIParam(input[2:end], 1)
		parser.segments = append(parser.segments, historyOutputSegment{CursorDown: true, Count: count})
		parser.movePhysicalRowBy(count)
		return end + 1
	case 'C':
		parser.flush()
		count := firstCSIParam(input[2:end], 1)
		parser.segments = append(parser.segments, historyOutputSegment{CursorForward: true, Count: count})
		parser.col += count
		parser.movePhysicalColumnBy(count)
		return end + 1
	case 'D':
		parser.flush()
		count := firstCSIParam(input[2:end], 1)
		parser.segments = append(parser.segments, historyOutputSegment{CursorBackward: true, Count: count})
		parser.col -= count
		if parser.col < 0 {
			parser.col = 0
		}
		parser.movePhysicalColumnBy(-count)
		return end + 1
	case 'G':
		parser.flush()
		column := firstCSIParam(input[2:end], 1)
		parser.segments = append(parser.segments, historyOutputSegment{CursorHorizontalAbsolute: true, Count: column})
		parser.col = column - 1
		if parser.col < 0 {
			parser.col = 0
		}
		parser.setPhysicalColumn(column - 1)
		return end + 1
	case 'H', 'f':
		parser.flush()
		row, column := firstTwoCSIParams(input[2:end], 1, 1)
		parser.segments = append(parser.segments, historyOutputSegment{CursorPosition: true, Row: row, Column: column})
		parser.col = column - 1
		if parser.col < 0 {
			parser.col = 0
		}
		parser.setPhysicalPosition(row-1, column-1)
		return end + 1
	}
	if final != 'm' {
		return end + 1
	}
	parser.flush()
	parser.applySGR(input[2:end])
	return end + 1
}

func (parser *historyANSIParser) consumeOSC(input string) int {
	payload, consumed, ok := consumeOSCPayload(input)
	if !ok {
		return 0
	}
	if strings.HasPrefix(payload, "8;") {
		parser.flush()
		parser.applyOSC8(payload)
	}
	return consumed
}

func (parser *historyANSIParser) writeText(text string) {
	for text != "" {
		if asciiLen := historyParserASCIITextRun(text); asciiLen > 0 {
			parser.writeASCIIText(text[:asciiLen])
			text = text[asciiLen:]
			continue
		}
		cluster, width := xansi.FirstGraphemeCluster(text, xansi.GraphemeWidth)
		if cluster == "" {
			return
		}
		text = text[len(cluster):]
		if width > 0 {
			parser.beforePhysicalPrint(width)
		}
		parser.buffer.WriteString(cluster)
		if width > 0 {
			parser.col += width
			parser.advancePhysicalPrint(width)
		}
	}
}

func (parser *historyANSIParser) writeASCIIText(text string) {
	if text == "" {
		return
	}
	if parser.screenCols <= 0 || parser.screenRows <= 0 {
		parser.buffer.WriteString(text)
		parser.col += len(text)
		return
	}
	for len(text) > 0 {
		if parser.atPhantom {
			parser.finishPhysicalRow()
			parser.advancePhysicalLine(parser.style)
		}
		available := parser.screenCols - parser.screenCol
		if available <= 0 {
			parser.atPhantom = true
			continue
		}
		count := len(text)
		if count > available {
			count = available
		}
		parser.buffer.WriteString(text[:count])
		parser.col += count
		parser.screenCol += count
		text = text[count:]
		if parser.screenCol >= parser.screenCols {
			parser.screenCol = parser.screenCols - 1
			parser.atPhantom = true
		}
	}
}

func (parser *historyANSIParser) writeTab() {
	const tabStop = 8
	spaces := tabStop - parser.col%tabStop
	if spaces <= 0 {
		spaces = tabStop
	}
	// 中文说明：真实 terminal 的 tab 是“跳到下一个 tab stop”，不是零宽字符；
	// history 必须落成空格，否则 ls/补全这类列输出进 copy mode 会挤成一坨。
	parser.writeText(strings.Repeat(" ", spaces))
}

func (parser *historyANSIParser) flush() {
	if parser.buffer.Len() == 0 {
		return
	}
	text := parser.buffer.String()
	parser.buffer.Reset()
	parser.segments = append(parser.segments, historyOutputSegment{Cells: []history.Cell{{
		Text:       text,
		Width:      xansi.StringWidth(text),
		Style:      parser.style,
		LinkURL:    parser.linkURL,
		LinkParams: parser.linkArgs,
	}}})
}

func (parser *historyANSIParser) beforePhysicalPrint(width int) {
	if parser.screenCols <= 0 || parser.screenRows <= 0 {
		return
	}
	if parser.atPhantom {
		parser.finishPhysicalRow()
		parser.advancePhysicalLine(parser.style)
		return
	}
	if width > 1 && parser.screenCol > 0 && parser.screenCol+width > parser.screenCols {
		parser.finishPhysicalRow()
		parser.advancePhysicalLine(parser.style)
	}
}

func (parser *historyANSIParser) advancePhysicalPrint(width int) {
	if parser.screenCols <= 0 || parser.screenRows <= 0 || width <= 0 {
		return
	}
	parser.screenCol += width
	if parser.screenCol >= parser.screenCols {
		parser.screenCol = parser.screenCols - 1
		parser.atPhantom = true
	}
}

func (parser *historyANSIParser) finishPhysicalRow() {
	if parser.atPhantom {
		parser.rowFootprintActive = false
		return
	}
	if !parser.rowFootprintActive || parser.screenCols <= 0 || parser.screenCol >= parser.screenCols {
		parser.rowFootprintActive = false
		return
	}
	style := parser.rowFootprintStyle
	if !historyParserVisibleBlankStyle(style) {
		parser.rowFootprintActive = false
		return
	}
	if parser.screenCols-parser.screenCol <= 0 {
		parser.rowFootprintActive = false
		return
	}
	parser.flush()
	// 中文说明：滚屏新建物理行继承背景时，尾部是“背景延伸到 EOL”，
	// 不是 logical line 里的 N 个空格；否则 resize 本地 reflow 会多出空白行。
	parser.segments = append(parser.segments, historyOutputSegment{SetTailFill: true, Style: style})
	parser.rowFootprintActive = false
}

func (parser *historyANSIParser) advancePhysicalLine(fillStyle history.CellStyle) {
	if parser.screenCols <= 0 || parser.screenRows <= 0 {
		parser.screenCol = 0
		parser.atPhantom = false
		parser.rowFootprintActive = false
		return
	}
	if parser.screenRow >= parser.screenRows-1 {
		parser.screenRow = parser.screenRows - 1
		parser.rowFootprintStyle = historyParserBlankFootprintStyle(fillStyle)
		parser.rowFootprintActive = historyParserVisibleBlankStyle(parser.rowFootprintStyle)
	} else {
		parser.screenRow++
		parser.rowFootprintActive = false
	}
	parser.screenCol = 0
	parser.atPhantom = false
}

func (parser *historyANSIParser) movePhysicalColumnBy(delta int) {
	if parser.screenCols <= 0 {
		parser.atPhantom = false
		return
	}
	parser.screenCol += delta
	if parser.screenCol < 0 {
		parser.screenCol = 0
	}
	if parser.screenCol >= parser.screenCols {
		parser.screenCol = parser.screenCols - 1
	}
	parser.atPhantom = false
}

func (parser *historyANSIParser) setPhysicalColumn(column int) {
	if parser.screenCols <= 0 {
		parser.screenCol = 0
		parser.atPhantom = false
		return
	}
	if column < 0 {
		column = 0
	}
	if column >= parser.screenCols {
		column = parser.screenCols - 1
	}
	parser.screenCol = column
	parser.atPhantom = false
}

func (parser *historyANSIParser) movePhysicalRowBy(delta int) {
	if parser.screenRows <= 0 {
		parser.screenRow = 0
		parser.atPhantom = false
		parser.rowFootprintActive = false
		return
	}
	parser.screenRow += delta
	if parser.screenRow < 0 {
		parser.screenRow = 0
	}
	if parser.screenRow >= parser.screenRows {
		parser.screenRow = parser.screenRows - 1
	}
	parser.atPhantom = false
	parser.rowFootprintActive = false
}

func (parser *historyANSIParser) setPhysicalPosition(row int, column int) {
	if parser.screenRows > 0 {
		if row < 0 {
			row = 0
		}
		if row >= parser.screenRows {
			row = parser.screenRows - 1
		}
		parser.screenRow = row
	}
	parser.setPhysicalColumn(column)
	parser.rowFootprintActive = false
}

func historyParserBlankFootprintStyle(style history.CellStyle) history.CellStyle {
	return history.CellStyle{BG: style.BG}
}

func historyParserVisibleBlankStyle(style history.CellStyle) bool {
	return style.BG != ""
}

func historyParserASCIITextRun(text string) int {
	for i := 0; i < len(text); i++ {
		if text[i] < 0x20 || text[i] >= 0x7f {
			return i
		}
	}
	return len(text)
}

func (parser *historyANSIParser) applySGR(paramsText string) {
	params := parseSGRParams(paramsText)
	if len(params) == 0 {
		parser.style = history.CellStyle{}
		return
	}
	for i := 0; i < len(params); i++ {
		switch param := params[i]; param {
		case 0:
			parser.style = history.CellStyle{}
		case 1:
			parser.style.Bold = true
		case 3:
			parser.style.Italic = true
		case 4:
			parser.style.Underline = true
		case 5:
			parser.style.Blink = true
		case 7:
			parser.style.Reverse = true
		case 9:
			parser.style.Strikethrough = true
		case 22:
			parser.style.Bold = false
		case 23:
			parser.style.Italic = false
		case 24:
			parser.style.Underline = false
		case 25:
			parser.style.Blink = false
		case 27:
			parser.style.Reverse = false
		case 29:
			parser.style.Strikethrough = false
		case 39:
			parser.style.FG = ""
		case 49:
			parser.style.BG = ""
		case 38, 48:
			color, next, ok := parseSGRExtendedColor(params, i+1)
			if ok {
				if param == 38 {
					parser.style.FG = color
				} else {
					parser.style.BG = color
				}
				i = next - 1
			}
		default:
			if param >= 30 && param <= 37 {
				parser.style.FG = "ansi:" + strconv.Itoa(param-30)
				continue
			}
			if param >= 90 && param <= 97 {
				parser.style.FG = "ansi:" + strconv.Itoa(param-90+8)
				continue
			}
			if param >= 40 && param <= 47 {
				parser.style.BG = "ansi:" + strconv.Itoa(param-40)
				continue
			}
			if param >= 100 && param <= 107 {
				parser.style.BG = "ansi:" + strconv.Itoa(param-100+8)
				continue
			}
		}
	}
}

func (parser *historyANSIParser) applyOSC8(payload string) {
	parts := strings.SplitN(payload, ";", 3)
	if len(parts) != 3 || parts[0] != "8" {
		return
	}
	parser.linkArgs = parts[1]
	parser.linkURL = parts[2]
}

func consumeOSCPayload(input string) (string, int, bool) {
	if !strings.HasPrefix(input, "\x1b]") {
		return "", 0, false
	}
	body := input[2:]
	bel := strings.IndexByte(body, '\a')
	st := strings.Index(body, "\x1b\\")
	switch {
	case bel < 0 && st < 0:
		return "", 0, false
	case st < 0 || (bel >= 0 && bel < st):
		return body[:bel], 2 + bel + 1, true
	default:
		return body[:st], 2 + st + 2, true
	}
}

func consumeEscapeSequence(input string) int {
	if input == "\x1b" {
		return 0
	}
	for i := 1; i < len(input); i++ {
		b := input[i]
		if b >= 0x20 && b <= 0x2f {
			continue
		}
		if b >= 0x30 && b <= 0x7e {
			return i + 1
		}
		return i + 1
	}
	return 0
}

func isStringControlStart(input string) bool {
	return strings.HasPrefix(input, "\x1bP") ||
		strings.HasPrefix(input, "\x1b^") ||
		strings.HasPrefix(input, "\x1b_") ||
		strings.HasPrefix(input, "\x1bX")
}

func consumeStringControl(input string) int {
	if !isStringControlStart(input) {
		return 0
	}
	body := input[2:]
	bel := strings.IndexByte(body, '\a')
	st := strings.Index(body, "\x1b\\")
	switch {
	case bel < 0 && st < 0:
		return 0
	case st < 0 || (bel >= 0 && bel < st):
		return 2 + bel + 1
	default:
		return 2 + st + 2
	}
}

func parseSGRParams(text string) []int {
	if text == "" {
		return nil
	}
	parts := strings.Split(strings.ReplaceAll(text, ":", ";"), ";")
	params := make([]int, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			params = append(params, 0)
			continue
		}
		value, err := strconv.Atoi(part)
		if err != nil {
			continue
		}
		params = append(params, value)
	}
	return params
}

func firstTwoCSIParams(text string, defaultFirst int, defaultSecond int) (int, int) {
	params := parseSGRParams(text)
	first := defaultFirst
	second := defaultSecond
	if len(params) > 0 && params[0] > 0 {
		first = params[0]
	}
	if len(params) > 1 && params[1] > 0 {
		second = params[1]
	}
	return first, second
}

func cloneHistoryOutputSegments(segments []historyOutputSegment) []historyOutputSegment {
	if len(segments) == 0 {
		return nil
	}
	out := make([]historyOutputSegment, len(segments))
	for i, segment := range segments {
		out[i].Seal = segment.Seal
		out[i].CarriageReturn = segment.CarriageReturn
		out[i].CursorForward = segment.CursorForward
		out[i].CursorBackward = segment.CursorBackward
		out[i].CursorHorizontalAbsolute = segment.CursorHorizontalAbsolute
		out[i].CursorUp = segment.CursorUp
		out[i].CursorDown = segment.CursorDown
		out[i].CursorPosition = segment.CursorPosition
		out[i].EraseInLine = segment.EraseInLine
		out[i].EraseInDisplay = segment.EraseInDisplay
		out[i].SetTailFill = segment.SetTailFill
		out[i].SwitchAltScreen = segment.SwitchAltScreen
		out[i].EnterAltScreen = segment.EnterAltScreen
		out[i].Count = segment.Count
		out[i].Row = segment.Row
		out[i].Column = segment.Column
		out[i].EraseMode = segment.EraseMode
		out[i].Style = segment.Style
		if len(segment.Cells) > 0 {
			out[i].Cells = make([]history.Cell, len(segment.Cells))
			copy(out[i].Cells, segment.Cells)
		}
	}
	return out
}

func parseAltScreenMode(text string, final byte) (bool, bool) {
	modes := parseSGRParams(strings.TrimPrefix(text, "?"))
	for _, mode := range modes {
		switch mode {
		case 47, 1047, 1049:
			return final == 'h', true
		}
	}
	return false, false
}

func firstCSIParam(text string, fallback int) int {
	params := parseSGRParams(text)
	if len(params) == 0 || params[0] <= 0 {
		return fallback
	}
	return params[0]
}

func parseSGRExtendedColor(params []int, start int) (string, int, bool) {
	if start >= len(params) {
		return "", start, false
	}
	switch params[start] {
	case 5:
		if start+1 >= len(params) {
			return "", start, false
		}
		return "idx:" + strconv.Itoa(clampColorComponent(params[start+1])), start + 2, true
	case 2:
		if start+3 >= len(params) {
			return "", start, false
		}
		r := clampColorComponent(params[start+1])
		g := clampColorComponent(params[start+2])
		b := clampColorComponent(params[start+3])
		return "#" + hexByte(r) + hexByte(g) + hexByte(b), start + 4, true
	default:
		return "", start, false
	}
}

func clampColorComponent(value int) int {
	if value < 0 {
		return 0
	}
	if value > 255 {
		return 255
	}
	return value
}

func hexByte(value int) string {
	const digits = "0123456789abcdef"
	return string([]byte{digits[value>>4], digits[value&0x0f]})
}
