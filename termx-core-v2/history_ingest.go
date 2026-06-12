package termxcorev2

import (
	"strconv"
	"strings"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/termx-core-v2/history"
)

type historyOutputSegment struct {
	Cells          []history.Cell
	Seal           bool
	CarriageReturn bool
	EraseInLine    bool
	EraseMode      int
}

func parseHistoryOutput(output string) []historyOutputSegment {
	parser := historyANSIParser{}
	return parser.Parse(output)
}

type historyANSIParser struct {
	style    history.CellStyle
	linkURL  string
	linkArgs string
	buffer   strings.Builder
	pending  string
	segments []historyOutputSegment
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
			output = output[1:]
		case output[0] == '\n':
			parser.flush()
			parser.segments = append(parser.segments, historyOutputSegment{Seal: true})
			output = output[1:]
		default:
			next := strings.IndexAny(output, "\x1b\r\n")
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
	if final == 'K' {
		parser.flush()
		mode := 0
		params := parseSGRParams(input[2:end])
		if len(params) > 0 {
			mode = params[0]
		}
		parser.segments = append(parser.segments, historyOutputSegment{EraseInLine: true, EraseMode: mode})
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
	if text == "" {
		return
	}
	parser.buffer.WriteString(text)
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

func cloneHistoryOutputSegments(segments []historyOutputSegment) []historyOutputSegment {
	if len(segments) == 0 {
		return nil
	}
	out := make([]historyOutputSegment, len(segments))
	for i, segment := range segments {
		out[i].Seal = segment.Seal
		out[i].CarriageReturn = segment.CarriageReturn
		out[i].EraseInLine = segment.EraseInLine
		out[i].EraseMode = segment.EraseMode
		if len(segment.Cells) > 0 {
			out[i].Cells = make([]history.Cell, len(segment.Cells))
			copy(out[i].Cells, segment.Cells)
		}
	}
	return out
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
