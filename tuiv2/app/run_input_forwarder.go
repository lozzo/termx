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
	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/muesli/cancelreader"
	xterm "golang.org/x/term"
)

var inputBurstBatchDelay = 2 * time.Millisecond
var remoteInputBurstBatchDelay = 500 * time.Microsecond
var mouseMotionBatchDelay = 4 * time.Millisecond
var remoteMouseMotionBatchDelay = 2 * time.Millisecond
var mouseWheelBatchDelay = 1 * time.Millisecond
var remoteMouseWheelBatchDelay time.Duration
var staleMouseMotionThreshold = 40 * time.Millisecond

type inputForwarderSink interface {
	Send(tea.Msg)
}

func startInputForwarder(program *tea.Program, input io.Reader) (func(), func() error, error) {
	return startInputForwarderWithSink(program, input)
}

func startInputForwarderWithSink(sink inputForwarderSink, input io.Reader) (func(), func() error, error) {
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
	burstDelay := effectiveInputBurstBatchDelay()
	go func() {
		defer close(done)
		var (
			burst       inputBurstState
			pending     []tea.Msg
			burstTimer  *time.Timer
			burstTick   <-chan time.Time
			mouseAccum  highFrequencyMouseAccumulator
			motionTimer *time.Timer
			motionTick  <-chan time.Time
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
			if burstDelay <= 0 {
				return
			}
			if burstTimer == nil {
				burstTimer = time.NewTimer(burstDelay)
			} else {
				stopBurstTimer()
				burstTimer.Reset(burstDelay)
			}
			burstTick = burstTimer.C
		}
		stopMotionTimer := func() {
			if motionTimer == nil {
				motionTick = nil
				return
			}
			if !motionTimer.Stop() {
				select {
				case <-motionTimer.C:
				default:
				}
			}
			motionTick = nil
		}
		flushHighFrequencyMouseToPending := func() {
			msgs := mouseAccum.Flush()
			if len(msgs) == 0 {
				return
			}
			pending = append(pending, msgs...)
		}
		dropHighFrequencyMouse := func() {
			mouseAccum.Reset()
			stopMotionTimer()
		}
		startHighFrequencyMouseTimer := func(delay time.Duration) {
			if delay <= 0 {
				return
			}
			if motionTimer == nil {
				motionTimer = time.NewTimer(delay)
			} else {
				stopMotionTimer()
				motionTimer.Reset(delay)
			}
			motionTick = motionTimer.C
		}
		flushPending := func() {
			flushBurstToPending()
			flushHighFrequencyMouseToPending()
			stopBurstTimer()
			stopMotionTimer()
			if len(pending) == 0 {
				return
			}
			msgs := append([]tea.Msg(nil), pending...)
			pending = pending[:0]
			if len(msgs) == 1 {
				sink.Send(msgs[0])
				return
			}
			sink.Send(interactionBatchMsg{Messages: msgs})
		}
		sendBoundaryImmediate := func(msg tea.Msg) {
			flushBurstToPending()
			dropHighFrequencyMouse()
			if len(pending) > 0 {
				msgs := append([]tea.Msg(nil), pending...)
				pending = pending[:0]
				if len(msgs) == 1 {
					sink.Send(msgs[0])
				} else {
					sink.Send(interactionBatchMsg{Messages: msgs})
				}
			}
			sink.Send(msg)
		}
		queueKeyBurst := func(msg tea.KeyMsg) {
			dropHighFrequencyMouse()
			if !burst.PushKey(msg) {
				flushBurstToPending()
				_ = burst.PushKey(msg)
			}
			startBurstTimer()
		}
		queueMotion := func(msg tea.MouseMsg) {
			queued := queuedMouseMsg{
				Seq:      nextMouseDebugSeq(),
				Kind:     "motion",
				QueuedAt: time.Now().UTC(),
				Msg:      msg,
			}
			noteQueuedMouseMotion(queued.Seq)
			appendMouseDebugLog("mouse_recv", "seq", queued.Seq, "kind", queued.Kind, "action", queued.Msg.Action, "button", queued.Msg.Button, "x", queued.Msg.X, "y", queued.Msg.Y)
			mouseAccum.QueueMotion(queued)
			startHighFrequencyMouseTimer(effectiveMouseMotionBatchDelay())
		}
		queueWheel := func(msg tea.MouseMsg) {
			mouseAccum.QueueWheel(msg)
			delay := effectiveMouseWheelBatchDelay()
			if delay <= 0 {
				flushPending()
				return
			}
			startHighFrequencyMouseTimer(delay)
		}
		sendMouseImmediate := func(kind string, msg tea.MouseMsg) {
			queued := queuedMouseMsg{
				Seq:      nextMouseDebugSeq(),
				Kind:     kind,
				QueuedAt: time.Now().UTC(),
				Msg:      msg,
			}
			appendMouseDebugLog("mouse_recv", "seq", queued.Seq, "kind", queued.Kind, "action", queued.Msg.Action, "button", queued.Msg.Button, "x", queued.Msg.X, "y", queued.Msg.Y)
			sendBoundaryImmediate(queued)
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
			case <-motionTick:
				flushPending()
			case event, ok := <-eventc:
				if !ok {
					flushPending()
					<-streamDone
					return
				}
				switch event := event.(type) {
				case uv.ForegroundColorEvent:
					sendBoundaryImmediate(hostDefaultColorsMsg{FG: event.Color})
				case uv.BackgroundColorEvent:
					sendBoundaryImmediate(hostDefaultColorsMsg{BG: event.Color})
				case uv.CursorPositionEvent:
					sendBoundaryImmediate(hostCursorPositionMsg{X: event.X, Y: event.Y})
				case uv.UnknownOscEvent:
					if index, c, ok := parsePaletteColorEvent(string(event)); ok {
						sendBoundaryImmediate(hostPaletteColorMsg{Index: index, Color: c})
					}
				case uv.PasteEvent:
					sendBoundaryImmediate(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(event.Content), Paste: true})
				case uv.KeyPressEvent:
					if msg, ok := uvKeyToTeaKeyMsg(event); ok {
						queueKeyBurst(msg)
					}
				case uv.MouseClickEvent:
					if msg, ok := uvMouseEventToTeaMouseMsg(event, tea.MouseActionPress); ok {
						sendMouseImmediate("press", msg)
					}
				case uv.MouseReleaseEvent:
					if msg, ok := uvMouseEventToTeaMouseMsg(event, tea.MouseActionRelease); ok {
						sendMouseImmediate("release", msg)
					}
				case uv.MouseMotionEvent:
					if msg, ok := uvMouseEventToTeaMouseMsg(event, tea.MouseActionMotion); ok {
						queueMotion(msg)
					}
				case uv.MouseWheelEvent:
					if msg, ok := uvMouseEventToTeaMouseMsg(event, tea.MouseActionPress); ok {
						queueWheel(msg)
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

func effectiveInputBurstBatchDelay() time.Duration {
	delay := inputBurstBatchDelay
	if shared.RemoteLatencyProfileEnabled() && (delay <= 0 || delay > remoteInputBurstBatchDelay) {
		delay = remoteInputBurstBatchDelay
	}
	return shared.DurationOverride("TERMX_INPUT_BURST_BATCH_DELAY", delay)
}

func effectiveMouseMotionBatchDelay() time.Duration {
	delay := mouseMotionBatchDelay
	if shared.RemoteLatencyProfileEnabled() && (delay <= 0 || delay > remoteMouseMotionBatchDelay) {
		delay = remoteMouseMotionBatchDelay
	}
	return shared.DurationOverride("TERMX_MOUSE_MOTION_BATCH_DELAY", delay)
}

func effectiveMouseWheelBatchDelay() time.Duration {
	delay := mouseWheelBatchDelay
	if shared.RemoteLatencyProfileEnabled() && (delay < 0 || delay > remoteMouseWheelBatchDelay) {
		delay = remoteMouseWheelBatchDelay
	}
	return shared.DurationOverride("TERMX_MOUSE_WHEEL_BATCH_DELAY", delay)
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
