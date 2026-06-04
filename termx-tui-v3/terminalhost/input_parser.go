package terminalhost

import (
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
	}
	if first < 0x20 || first == 0x7f {
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
	switch buffer[2] {
	case 'A':
		return input.InputEvent{Kind: input.EventKindKey, Key: input.KeyUp, RawSeq: string(buffer[:3])}, 3, true
	case 'B':
		return input.InputEvent{Kind: input.EventKindKey, Key: input.KeyDown, RawSeq: string(buffer[:3])}, 3, true
	case 'C':
		return input.InputEvent{Kind: input.EventKindKey, Key: input.KeyRight, RawSeq: string(buffer[:3])}, 3, true
	case 'D':
		return input.InputEvent{Kind: input.EventKindKey, Key: input.KeyLeft, RawSeq: string(buffer[:3])}, 3, true
	case '5':
		if len(buffer) < 4 {
			return input.InputEvent{}, 0, false
		}
		if buffer[3] == '~' {
			return input.InputEvent{
				Kind:   input.EventKindKey,
				Key:    input.KeyPageUp,
				RawSeq: string(buffer[:4]),
			}, 4, true
		}
	case '6':
		if len(buffer) < 4 {
			return input.InputEvent{}, 0, false
		}
		if buffer[3] == '~' {
			return input.InputEvent{
				Kind:   input.EventKindKey,
				Key:    input.KeyPageDn,
				RawSeq: string(buffer[:4]),
			}, 4, true
		}
	case '<':
		return parseSGRMouse(buffer)
	}
	for i := 2; i < len(buffer); i++ {
		if buffer[i] >= 0x40 && buffer[i] <= 0x7e {
			return unknownKey(buffer[:i+1]), i + 1, true
		}
	}
	return input.InputEvent{}, 0, false
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
	if final == 'M' && code&3 == 0 {
		return input.MouseLeft, true
	}
	return "", false
}

func unknownKey(seq []byte) input.InputEvent {
	return input.InputEvent{
		Kind:   input.EventKindKey,
		Key:    input.KeyUnknown,
		RawSeq: string(seq),
	}
}
