package app

import (
	"context"
	"image/color"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	uv "github.com/charmbracelet/ultraviolet"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/muesli/cancelreader"
	xterm "golang.org/x/term"
)

var inputBurstBatchDelay = 2 * time.Millisecond

func startInputForwarder(program *tea.Program, input io.Reader) (func(), func() error, error) {
	if input == nil {
		return func() {}, func() error { return nil }, nil
	}

	restore := func() error { return nil }
	if file, ok := input.(interface{ Fd() uintptr }); ok && xterm.IsTerminal(int(file.Fd())) {
		state, err := xterm.MakeRaw(int(file.Fd()))
		if err != nil {
			return nil, nil, err
		}
		restore = func() error {
			return xterm.Restore(int(file.Fd()), state)
		}
	}

	reader, err := cancelreader.NewReader(input)
	if err != nil {
		_ = restore()
		return nil, nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	eventc := make(chan uv.Event, 128)
	terminalReader := uv.NewTerminalReader(reader, os.Getenv("TERM"))
	go func() {
		defer close(done)
		var (
			burst      inputBurstState
			pending    []tea.Msg
			burstTimer *time.Timer
			burstTick  <-chan time.Time
		)
		stopBurstTimer := func() {
			if burstTimer == nil {
				burstTick = nil
				return
			}
			if !burstTimer.Stop() {
				select {
				case <-burstTimer.C:
				default:
				}
			}
			burstTick = nil
		}
		flushBurstToPending := func() {
			msg, ok := burst.Flush()
			if !ok {
				return
			}
			pending = append(pending, msg)
		}
		startBurstTimer := func() {
			if inputBurstBatchDelay <= 0 {
				return
			}
			if burstTimer == nil {
				burstTimer = time.NewTimer(inputBurstBatchDelay)
			} else {
				stopBurstTimer()
				burstTimer.Reset(inputBurstBatchDelay)
			}
			burstTick = burstTimer.C
		}
		flushPending := func() {
			flushBurstToPending()
			stopBurstTimer()
			if len(pending) == 0 {
				return
			}
			msgs := append([]tea.Msg(nil), pending...)
			pending = pending[:0]
			if len(msgs) == 1 {
				program.Send(msgs[0])
				return
			}
			program.Send(interactionBatchMsg{Messages: msgs})
		}
		sendImmediate := func(msg tea.Msg) {
			flushPending()
			program.Send(msg)
		}
		queueKeyBurst := func(msg tea.KeyMsg) {
			if !burst.PushKey(msg) {
				flushBurstToPending()
				_ = burst.PushKey(msg)
			}
			startBurstTimer()
		}
		queueWheelBurst := func(msg tea.MouseMsg) {
			if !burst.PushMouseWheel(msg) {
				flushBurstToPending()
				_ = burst.PushMouseWheel(msg)
			}
			startBurstTimer()
		}
		streamDone := make(chan struct{})
		go func() {
			defer close(streamDone)
			_ = terminalReader.StreamEvents(ctx, eventc)
			close(eventc)
		}()
		for {
			select {
			case <-ctx.Done():
				flushPending()
				<-streamDone
				return
			case <-burstTick:
				flushPending()
			case event, ok := <-eventc:
				if !ok {
					flushPending()
					<-streamDone
					return
				}
				switch event := event.(type) {
				case uv.ForegroundColorEvent:
					sendImmediate(hostDefaultColorsMsg{FG: event.Color})
				case uv.BackgroundColorEvent:
					sendImmediate(hostDefaultColorsMsg{BG: event.Color})
				case uv.CursorPositionEvent:
					sendImmediate(hostCursorPositionMsg{X: event.X, Y: event.Y})
				case uv.UnknownOscEvent:
					if index, c, ok := parsePaletteColorEvent(string(event)); ok {
						sendImmediate(hostPaletteColorMsg{Index: index, Color: c})
					}
				case uv.PasteEvent:
					sendImmediate(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(event.Content), Paste: true})
				case uv.KeyPressEvent:
					if msg, ok := uvKeyToTeaKeyMsg(event); ok {
						queueKeyBurst(msg)
					}
				case uv.MouseClickEvent:
					if msg, ok := uvMouseEventToTeaMouseMsg(event, tea.MouseActionPress); ok {
						sendImmediate(msg)
					}
				case uv.MouseReleaseEvent:
					if msg, ok := uvMouseEventToTeaMouseMsg(event, tea.MouseActionRelease); ok {
						sendImmediate(msg)
					}
				case uv.MouseMotionEvent:
					if msg, ok := uvMouseEventToTeaMouseMsg(event, tea.MouseActionMotion); ok {
						sendImmediate(msg)
					}
				case uv.MouseWheelEvent:
					if msg, ok := uvMouseEventToTeaMouseMsg(event, tea.MouseActionPress); ok {
						queueWheelBurst(msg)
					}
				}
			}
		}
	}()

	stop := func() {
		cancel()
		if reader.Cancel() {
			select {
			case <-done:
			case <-time.After(500 * time.Millisecond):
			}
		}
		_ = reader.Close()
	}

	return stop, restore, nil
}

func requestTerminalPaletteQueries() string {
	var b strings.Builder
	for i := 0; i < 16; i++ {
		b.WriteString("\x1b]4;")
		b.WriteString(strconv.Itoa(i))
		b.WriteString(";?\x07")
	}
	return b.String()
}

func parsePaletteColorEvent(raw string) (int, color.Color, bool) {
	if raw == "" {
		return 0, nil, false
	}
	raw = strings.TrimPrefix(raw, "\x1b]")
	raw = strings.TrimSuffix(raw, "\x07")
	raw = strings.TrimSuffix(raw, "\x1b\\")
	parts := strings.Split(raw, ";")
	if len(parts) != 3 || parts[0] != "4" {
		return 0, nil, false
	}
	index, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil || index < 0 || index > 255 {
		return 0, nil, false
	}
	c := xansi.XParseColor(strings.TrimSpace(parts[2]))
	if c == nil {
		return 0, nil, false
	}
	return index, c, true
}

type inputBurstState struct {
	key    *tea.KeyMsg
	mouse  *tea.MouseMsg
	repeat int
}

func (b *inputBurstState) Reset() {
	if b == nil {
		return
	}
	b.key = nil
	b.mouse = nil
	b.repeat = 0
}

func (b *inputBurstState) PushKey(msg tea.KeyMsg) bool {
	if b == nil {
		return false
	}
	if b.repeat == 0 {
		key := msg
		b.key = &key
		b.mouse = nil
		b.repeat = 1
		return true
	}
	if b.key == nil || !sameTeaKeyMsg(*b.key, msg) {
		return false
	}
	b.repeat++
	return true
}

func (b *inputBurstState) PushMouseWheel(msg tea.MouseMsg) bool {
	if b == nil {
		return false
	}
	if b.repeat == 0 {
		mouse := msg
		b.mouse = &mouse
		b.key = nil
		b.repeat = 1
		return true
	}
	if b.mouse == nil || !sameTeaMouseMsg(*b.mouse, msg) {
		return false
	}
	b.repeat++
	return true
}

func (b *inputBurstState) Flush() (tea.Msg, bool) {
	if b == nil || b.repeat == 0 {
		return nil, false
	}
	repeat := maxInt(1, b.repeat)
	switch {
	case b.key != nil:
		msg := *b.key
		b.Reset()
		if repeat == 1 {
			return msg, true
		}
		return keyBurstMsg{Msg: msg, Repeat: repeat}, true
	case b.mouse != nil:
		msg := *b.mouse
		b.Reset()
		if repeat == 1 {
			return msg, true
		}
		return mouseWheelBurstMsg{Msg: msg, Repeat: repeat}, true
	default:
		b.Reset()
		return nil, false
	}
}

func sameTeaKeyMsg(left, right tea.KeyMsg) bool {
	if left.Type != right.Type || left.Alt != right.Alt || left.Paste != right.Paste || len(left.Runes) != len(right.Runes) {
		return false
	}
	for i := range left.Runes {
		if left.Runes[i] != right.Runes[i] {
			return false
		}
	}
	return true
}

func sameTeaMouseMsg(left, right tea.MouseMsg) bool {
	return left.X == right.X &&
		left.Y == right.Y &&
		left.Button == right.Button &&
		left.Action == right.Action &&
		left.Alt == right.Alt &&
		left.Ctrl == right.Ctrl &&
		left.Shift == right.Shift
}
