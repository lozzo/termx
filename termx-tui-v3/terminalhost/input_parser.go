package terminalhost

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/lozzow/termx/termx-tui-v3/input"
)

// InputParser 把 raw TTY bytes 转成 v3 自有 InputEvent。
type InputParser struct {
	pending []byte
}

func NewInputParser() *InputParser {
	return &InputParser{}
}

// Feed 接收一批 raw bytes，返回能完整解析的输入事件。
func (parser *InputParser) Feed(chunk []byte) []input.InputEvent {
	if len(chunk) == 0 {
		return nil
	}
	parser.pending = append(parser.pending, chunk...)
	var events []input.InputEvent
	for len(parser.pending) > 0 {
		event, consumed, complete := parseOneInput(parser.pending)
		if !complete {
			break
		}
		events = append(events, event)
		parser.pending = parser.pending[consumed:]
	}
	return events
}

func parseOneInput(buffer []byte) (input.InputEvent, int, bool) {
	if len(buffer) == 0 {
		return input.InputEvent{}, 0, false
	}
	first := buffer[0]
	switch first {
	case '\x1b':
		return parseEscape(buffer)
	case '\r', '\n':
		return input.InputEvent{
			Kind:   input.EventKindKey,
			Key:    input.KeyEnter,
			RawSeq: string(buffer[:1]),
		}, 1, true
	case '\t':
		return input.InputEvent{
			Kind:   input.EventKindKey,
			Key:    input.KeyTab,
			RawSeq: string(buffer[:1]),
		}, 1, true
	case 0x7f:
		return input.InputEvent{
			Kind:   input.EventKindKey,
			Key:    input.KeyBackspace,
			RawSeq: string(buffer[:1]),
		}, 1, true
	}
	if first < 0x20 {
		return input.InputEvent{
			Kind:   input.EventKindKey,
			Key:    input.KeyChar,
			Char:   string([]byte{first}),
			Ctrl:   first < 0x20,
			RawSeq: string(buffer[:1]),
		}, 1, true
	}
	r, size := utf8.DecodeRune(buffer)
	if r == utf8.RuneError && size == 1 && !utf8.FullRune(buffer) {
		return input.InputEvent{}, 0, false
	}
	if r == utf8.RuneError && size == 1 {
		return unknownKey(buffer[:1]), 1, true
	}
	return input.InputEvent{
		Kind:   input.EventKindKey,
		Key:    input.KeyChar,
		Char:   string(r),
		RawSeq: string(buffer[:size]),
	}, size, true
}

func parseEscape(buffer []byte) (input.InputEvent, int, bool) {
	if len(buffer) == 1 {
		return input.InputEvent{Kind: input.EventKindKey, Key: input.KeyEsc, RawSeq: "\x1b"}, 1, true
	}
	if buffer[1] == ']' {
		return parseOSC(buffer)
	}
	if buffer[1] == 'O' {
		return parseSS3(buffer)
	}
	if buffer[1] != '[' {
		event, consumed, ok := parseAltChar(buffer)
		if ok {
			return event, consumed, true
		}
		return input.InputEvent{Kind: input.EventKindKey, Key: input.KeyEsc, RawSeq: "\x1b"}, 1, true
	}
	if len(buffer) == 2 {
		return input.InputEvent{}, 0, false
	}
	if buffer[2] == '<' {
		return parseSGRMouse(buffer)
	}
	for i := 2; i < len(buffer); i++ {
		if buffer[i] >= 0x40 && buffer[i] <= 0x7e {
			if event, ok := csiKeyEvent(buffer[:i+1]); ok {
				return event, i + 1, true
			}
			return unknownKey(buffer[:i+1]), i + 1, true
		}
	}
	return input.InputEvent{}, 0, false
}

func parseOSC(buffer []byte) (input.InputEvent, int, bool) {
	if len(buffer) < 3 {
		return input.InputEvent{}, 0, false
	}
	end := -1
	st := false
	for i := 2; i < len(buffer); i++ {
		if buffer[i] == '\a' {
			end = i
			break
		}
		if buffer[i] == '\\' && i > 2 && buffer[i-1] == '\x1b' {
			end = i
			st = true
			break
		}
	}
	if end < 0 {
		return input.InputEvent{}, 0, false
	}
	consumed := end + 1
	bodyEnd := end
	if st {
		bodyEnd = end - 1
	}
	seq := buffer[:consumed]
	body := string(buffer[2:bodyEnd])
	if event, ok := oscThemeEvent(body, string(seq)); ok {
		return event, consumed, true
	}
	return input.InputEvent{Kind: input.EventKindKey, Key: input.KeyUnknown, RawSeq: string(seq)}, consumed, true
}

func oscThemeEvent(body string, raw string) (input.InputEvent, bool) {
	parts := strings.Split(body, ";")
	if len(parts) < 2 {
		return input.InputEvent{}, false
	}
	switch parts[0] {
	case "10":
		colorValue, ok := parseOSCColor(parts[1])
		if !ok {
			return input.InputEvent{}, false
		}
		return input.InputEvent{Kind: input.EventKindHostTheme, Theme: input.HostThemeEvent{DefaultFG: colorValue}, RawSeq: raw}, true
	case "11":
		colorValue, ok := parseOSCColor(parts[1])
		if !ok {
			return input.InputEvent{}, false
		}
		return input.InputEvent{Kind: input.EventKindHostTheme, Theme: input.HostThemeEvent{DefaultBG: colorValue}, RawSeq: raw}, true
	case "4":
		if len(parts) < 3 {
			return input.InputEvent{}, false
		}
		index, err := strconv.Atoi(parts[1])
		if err != nil || index < 0 || index > 255 {
			return input.InputEvent{}, false
		}
		colorValue, ok := parseOSCColor(parts[2])
		if !ok {
			return input.InputEvent{}, false
		}
		return input.InputEvent{Kind: input.EventKindHostTheme, Theme: input.HostThemeEvent{PaletteIndex: index, PaletteColor: colorValue}, RawSeq: raw}, true
	default:
		return input.InputEvent{}, false
	}
}

func parseOSCColor(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || value == "?" {
		return "", false
	}
	if strings.HasPrefix(value, "#") && len(value) == 7 {
		return strings.ToLower(value), true
	}
	if strings.HasPrefix(value, "rgb:") {
		parts := strings.Split(strings.TrimPrefix(value, "rgb:"), "/")
		if len(parts) != 3 {
			return "", false
		}
		r, okR := parseOSCColorComponent(parts[0])
		g, okG := parseOSCColorComponent(parts[1])
		b, okB := parseOSCColorComponent(parts[2])
		if !okR || !okG || !okB {
			return "", false
		}
		return fmt.Sprintf("#%02x%02x%02x", r, g, b), true
	}
	return "", false
}

func parseOSCColorComponent(value string) (int, bool) {
	if len(value) == 0 {
		return 0, false
	}
	parsed, err := strconv.ParseUint(value, 16, 16)
	if err != nil {
		return 0, false
	}
	max := uint64(1)<<(len(value)*4) - 1
	if max == 0 {
		return 0, false
	}
	return int((parsed*255 + max/2) / max), true
}

func parseSS3(buffer []byte) (input.InputEvent, int, bool) {
	if len(buffer) == 2 {
		return input.InputEvent{}, 0, false
	}
	seq := buffer[:3]
	event := input.InputEvent{Kind: input.EventKindKey, RawSeq: string(seq)}
	switch buffer[2] {
	case 'A':
		event.Key = input.KeyUp
	case 'B':
		event.Key = input.KeyDown
	case 'C':
		event.Key = input.KeyRight
	case 'D':
		event.Key = input.KeyLeft
	case 'F':
		event.Key = input.KeyEnd
	case 'H':
		event.Key = input.KeyHome
	case 'P':
		event.Key = input.KeyF1
	case 'Q':
		event.Key = input.KeyF2
	case 'R':
		event.Key = input.KeyF3
	case 'S':
		event.Key = input.KeyF4
	default:
		return unknownKey(seq), 3, true
	}
	return event, 3, true
}

func csiKeyEvent(seq []byte) (input.InputEvent, bool) {
	if len(seq) < 3 || seq[0] != '\x1b' || seq[1] != '[' {
		return input.InputEvent{}, false
	}
	body := string(seq[2 : len(seq)-1])
	final := seq[len(seq)-1]
	event := input.InputEvent{Kind: input.EventKindKey, RawSeq: string(seq)}
	switch final {
	case 'A', 'B', 'C', 'D', 'F', 'H', 'Z':
		parts := splitCSIParams(body)
		applyKeyModifier(&event, modifierParam(parts, 1))
		switch final {
		case 'A':
			event.Key = input.KeyUp
		case 'B':
			event.Key = input.KeyDown
		case 'C':
			event.Key = input.KeyRight
		case 'D':
			event.Key = input.KeyLeft
		case 'F':
			event.Key = input.KeyEnd
		case 'H':
			event.Key = input.KeyHome
		case 'Z':
			event.Key = input.KeyShiftTab
			event.Shift = true
		}
		return event, true
	case '~':
		parts := splitCSIParams(body)
		code, err := strconv.Atoi(parts[0])
		if err != nil {
			return input.InputEvent{}, false
		}
		key, ok := tildeKey(code)
		if !ok {
			return input.InputEvent{}, false
		}
		event.Key = key
		applyKeyModifier(&event, modifierParam(parts, 1))
		return event, true
	default:
		return input.InputEvent{}, false
	}
}

func splitCSIParams(body string) []string {
	if body == "" {
		return []string{""}
	}
	return strings.Split(body, ";")
}

func modifierParam(parts []string, index int) int {
	if index >= len(parts) {
		return 0
	}
	value, err := strconv.Atoi(parts[index])
	if err != nil {
		return 0
	}
	return value
}

func applyKeyModifier(event *input.InputEvent, encoded int) {
	if encoded <= 1 {
		return
	}
	mask := encoded - 1
	event.Shift = mask&1 != 0
	event.Alt = mask&2 != 0
	event.Ctrl = mask&4 != 0
}

func tildeKey(code int) (input.Key, bool) {
	switch code {
	case 1, 7:
		return input.KeyHome, true
	case 2:
		return input.KeyInsert, true
	case 3:
		return input.KeyDelete, true
	case 4, 8:
		return input.KeyEnd, true
	case 5:
		return input.KeyPageUp, true
	case 6:
		return input.KeyPageDn, true
	case 11:
		return input.KeyF1, true
	case 12:
		return input.KeyF2, true
	case 13:
		return input.KeyF3, true
	case 14:
		return input.KeyF4, true
	case 15:
		return input.KeyF5, true
	case 17:
		return input.KeyF6, true
	case 18:
		return input.KeyF7, true
	case 19:
		return input.KeyF8, true
	case 20:
		return input.KeyF9, true
	case 21:
		return input.KeyF10, true
	case 23:
		return input.KeyF11, true
	case 24:
		return input.KeyF12, true
	default:
		return "", false
	}
}

func parseAltChar(buffer []byte) (input.InputEvent, int, bool) {
	r, size := utf8.DecodeRune(buffer[1:])
	if r == utf8.RuneError && size == 1 && !utf8.FullRune(buffer[1:]) {
		return input.InputEvent{}, 0, false
	}
	if r == utf8.RuneError && size == 1 {
		return input.InputEvent{}, 0, false
	}
	return input.InputEvent{
		Kind:   input.EventKindKey,
		Key:    input.KeyChar,
		Char:   string(r),
		Alt:    true,
		RawSeq: string(buffer[:1+size]),
	}, 1 + size, true
}

func parseSGRMouse(buffer []byte) (input.InputEvent, int, bool) {
	end := -1
	for i := 3; i < len(buffer); i++ {
		if buffer[i] == 'M' || buffer[i] == 'm' {
			end = i
			break
		}
	}
	if end < 0 {
		return input.InputEvent{}, 0, false
	}
	seq := buffer[:end+1]
	parts := strings.Split(string(buffer[3:end]), ";")
	if len(parts) != 3 {
		return unknownKey(seq), len(seq), true
	}
	code, errCode := strconv.Atoi(parts[0])
	col, errCol := strconv.Atoi(parts[1])
	row, errRow := strconv.Atoi(parts[2])
	if errCode != nil || errCol != nil || errRow != nil {
		return unknownKey(seq), len(seq), true
	}
	button, ok := mouseButton(code, buffer[end])
	if !ok {
		return unknownKey(seq), len(seq), true
	}
	return input.InputEvent{
		Kind:   input.EventKindMouse,
		Mouse:  button,
		Row:    row,
		Col:    col,
		Shift:  code&4 != 0,
		Alt:    code&8 != 0,
		Ctrl:   code&16 != 0,
		RawSeq: string(seq),
	}, len(seq), true
}

func mouseButton(code int, final byte) (input.MouseButton, bool) {
	if code&64 != 0 {
		if code&1 != 0 {
			return input.MouseWheelDown, true
		}
		return input.MouseWheelUp, true
	}
	button := code & 3
	if final == 'm' {
		switch button {
		case 0:
			return input.MouseLeftUp, true
		case 1:
			return input.MouseMiddleUp, true
		case 2:
			return input.MouseRightUp, true
		default:
			return input.MouseRelease, true
		}
	}
	if final != 'M' {
		return "", false
	}
	if code&32 != 0 {
		switch button {
		case 0:
			return input.MouseLeftDrag, true
		case 1:
			return input.MouseMiddleDrag, true
		case 2:
			return input.MouseRightDrag, true
		default:
			return input.MouseMove, true
		}
	}
	switch button {
	case 0:
		return input.MouseLeft, true
	case 1:
		return input.MouseMiddle, true
	case 2:
		return input.MouseRight, true
	default:
		return input.MouseMove, true
	}
}

func unknownKey(seq []byte) input.InputEvent {
	return input.InputEvent{
		Kind:   input.EventKindKey,
		Key:    input.KeyUnknown,
		RawSeq: string(seq),
	}
}
