package terminalhost

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-tui-v3/app"
	"github.com/lozzow/termx/termx-tui-v3/input"
	"github.com/lozzow/termx/termx-tui-v3/render"
)

type fakeTerminalOps struct {
	mu       sync.Mutex
	terminal bool
	cols     int
	rows     int
	raw      int
	restored int
}

func (ops *fakeTerminalOps) IsTerminal(uintptr) bool {
	return ops.terminal
}

func (ops *fakeTerminalOps) MakeRaw(uintptr) (TerminalState, error) {
	ops.mu.Lock()
	defer ops.mu.Unlock()
	ops.raw++
	return "state", nil
}

func (ops *fakeTerminalOps) Restore(uintptr, TerminalState) error {
	ops.mu.Lock()
	defer ops.mu.Unlock()
	ops.restored++
	return nil
}

func (ops *fakeTerminalOps) GetSize(uintptr) (int, int, error) {
	ops.mu.Lock()
	defer ops.mu.Unlock()
	return ops.cols, ops.rows, nil
}

func (ops *fakeTerminalOps) RawCount() int {
	ops.mu.Lock()
	defer ops.mu.Unlock()
	return ops.raw
}

func (ops *fakeTerminalOps) RestoredCount() int {
	ops.mu.Lock()
	defer ops.mu.Unlock()
	return ops.restored
}

type blockingCancelReader struct {
	events chan []byte
	done   chan struct{}
}

func newBlockingCancelReader() *blockingCancelReader {
	return &blockingCancelReader{
		events: make(chan []byte, 8),
		done:   make(chan struct{}),
	}
}

func (reader *blockingCancelReader) Read(p []byte) (int, error) {
	select {
	case data := <-reader.events:
		copy(p, data)
		return len(data), nil
	case <-reader.done:
		return 0, io.EOF
	}
}

func (reader *blockingCancelReader) Cancel() bool {
	select {
	case <-reader.done:
		return false
	default:
		close(reader.done)
		return true
	}
}

func TestHostImplementsAppTerminalHost(t *testing.T) {
	var _ app.TerminalHost = (*Host)(nil)
}

func TestHostEnterCloseRestoresTerminal(t *testing.T) {
	ops := &fakeTerminalOps{terminal: true}
	reader := newBlockingCancelReader()
	var output bytes.Buffer
	host := New(
		WithInput(strings.NewReader(""), 7),
		WithOutput(&output),
		WithTerminalOps(ops),
		WithCancelReaderFactory(func(io.Reader) (CancelReader, error) {
			return reader, nil
		}),
	)

	if err := host.Enter(context.Background()); err != nil {
		t.Fatalf("enter: %v", err)
	}
	if got := ops.RawCount(); got != 1 {
		t.Fatalf("expected raw mode once, got %d", got)
	}
	gotEnter := output.String()
	for _, seq := range []string{enterAltScreen, hideCursor, enableBracketPaste, enableMouseCell, enableMouseButton, enableMouseSGR} {
		if !strings.Contains(gotEnter, seq) {
			t.Fatalf("missing enter sequence %q in %q", seq, gotEnter)
		}
	}
	if err := host.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := host.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	if got := ops.RestoredCount(); got != 1 {
		t.Fatalf("expected restore once, got %d", got)
	}
	gotAll := output.String()
	for _, seq := range []string{disableMouseSGR, disableMouseButton, disableMouseCell, disableBracketPaste, showCursor, exitAltScreen} {
		if !strings.Contains(gotAll, seq) {
			t.Fatalf("missing exit sequence %q in %q", seq, gotAll)
		}
	}
}

func TestHostRejectsNonTerminal(t *testing.T) {
	host := New(WithTerminalOps(&fakeTerminalOps{}))
	if err := host.Enter(context.Background()); !errors.Is(err, ErrNotTerminal) {
		t.Fatalf("expected ErrNotTerminal, got %v", err)
	}
}

func TestHostReportsWindowSize(t *testing.T) {
	host := New(WithTerminalOps(&fakeTerminalOps{cols: 132, rows: 43}))
	cols, rows, err := host.Size()
	if err != nil {
		t.Fatalf("size: %v", err)
	}
	if cols != 132 || rows != 43 {
		t.Fatalf("unexpected size %dx%d", cols, rows)
	}
}

func TestHostPublishesResizeEventsFromSignal(t *testing.T) {
	ops := &fakeTerminalOps{terminal: true, cols: 90, rows: 30}
	reader := newBlockingCancelReader()
	resizeSignals := make(chan os.Signal, 2)
	host := New(
		WithInput(strings.NewReader(""), 7),
		WithOutput(io.Discard),
		WithTerminalOps(ops),
		WithCancelReaderFactory(func(io.Reader) (CancelReader, error) {
			return reader, nil
		}),
		WithResizeSignalChannel(resizeSignals),
	)
	if err := host.Enter(context.Background()); err != nil {
		t.Fatalf("enter: %v", err)
	}

	resizeSignals <- testSignal{}
	got := readEvent(t, host)
	if got.Kind != input.EventKindResize || got.Cols != 90 || got.Rows != 30 {
		t.Fatalf("expected resize event from signal, got %#v", got)
	}

	ops.mu.Lock()
	ops.cols = 120
	ops.rows = 40
	ops.mu.Unlock()
	resizeSignals <- testSignal{}
	got = readEvent(t, host)
	if got.Kind != input.EventKindResize || got.Cols != 120 || got.Rows != 40 {
		t.Fatalf("expected updated resize event from signal, got %#v", got)
	}

	if err := host.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestHostReadsInputEventsAndStopsOnClose(t *testing.T) {
	ops := &fakeTerminalOps{terminal: true}
	reader := newBlockingCancelReader()
	host := New(
		WithInput(strings.NewReader(""), 7),
		WithOutput(io.Discard),
		WithTerminalOps(ops),
		WithCancelReaderFactory(func(io.Reader) (CancelReader, error) {
			return reader, nil
		}),
	)
	if err := host.Enter(context.Background()); err != nil {
		t.Fatalf("enter: %v", err)
	}

	reader.events <- []byte("x")
	reader.events <- []byte("\x1b[5~")
	reader.events <- []byte("\x1b[C")
	got := []input.InputEvent{readEvent(t, host), readEvent(t, host), readEvent(t, host)}
	want := []input.InputEvent{
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "x", RawSeq: "x"},
		{Kind: input.EventKindKey, Key: input.KeyPageUp, RawSeq: "\x1b[5~"},
		{Kind: input.EventKindKey, Key: input.KeyRight, RawSeq: "\x1b[C"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected events\n got: %#v\nwant: %#v", got, want)
	}

	if err := host.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if got := ops.RestoredCount(); got != 1 {
		t.Fatalf("expected restore once, got %d", got)
	}
}

func TestHostRestoresTerminalWhenContextIsCanceled(t *testing.T) {
	ops := &fakeTerminalOps{terminal: true}
	reader := newBlockingCancelReader()
	ctx, cancel := context.WithCancel(context.Background())
	host := New(
		WithInput(strings.NewReader(""), 7),
		WithOutput(io.Discard),
		WithTerminalOps(ops),
		WithCancelReaderFactory(func(io.Reader) (CancelReader, error) {
			return reader, nil
		}),
	)
	if err := host.Enter(ctx); err != nil {
		t.Fatalf("enter: %v", err)
	}
	cancel()

	deadline := time.After(time.Second)
	for ops.RestoredCount() == 0 {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for context restore")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if err := host.Close(); err != nil {
		t.Fatalf("close after cancel: %v", err)
	}
	if got := ops.RestoredCount(); got != 1 {
		t.Fatalf("expected restore once, got %d", got)
	}
}

func TestInputParserConvertsMouseAndKeys(t *testing.T) {
	parser := NewInputParser()
	got := parser.Feed([]byte("\x1b[<64;10;4M\x1b[<65;11;5M\x1b[<0;12;6M\x1b[<32;13;6M\x1b[<0;13;6m\r好"))
	want := []input.InputEvent{
		{Kind: input.EventKindMouse, Mouse: input.MouseWheelUp, Row: 4, Col: 10, RawSeq: "\x1b[<64;10;4M"},
		{Kind: input.EventKindMouse, Mouse: input.MouseWheelDown, Row: 5, Col: 11, RawSeq: "\x1b[<65;11;5M"},
		{Kind: input.EventKindMouse, Mouse: input.MouseLeft, Row: 6, Col: 12, RawSeq: "\x1b[<0;12;6M"},
		{Kind: input.EventKindMouse, Mouse: input.MouseLeftDrag, Row: 6, Col: 13, RawSeq: "\x1b[<32;13;6M"},
		{Kind: input.EventKindMouse, Mouse: input.MouseLeftUp, Row: 6, Col: 13, RawSeq: "\x1b[<0;13;6m"},
		{Kind: input.EventKindKey, Key: input.KeyEnter, RawSeq: "\r"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "好", RawSeq: "好"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected parsed events\n got: %#v\nwant: %#v", got, want)
	}
}

func TestInputParserMarksCtrlFAndCtrlV(t *testing.T) {
	parser := NewInputParser()
	got := parser.Feed([]byte("\x06\x16"))
	want := []input.InputEvent{
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "\x06", Ctrl: true, RawSeq: "\x06"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "\x16", Ctrl: true, RawSeq: "\x16"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected parsed ctrl events\n got: %#v\nwant: %#v", got, want)
	}
}

func TestInputParserKeepsPartialEscapeUntilComplete(t *testing.T) {
	parser := NewInputParser()
	if got := parser.Feed([]byte("\x1b[5")); len(got) != 0 {
		t.Fatalf("expected no partial events, got %#v", got)
	}
	got := parser.Feed([]byte("~"))
	want := []input.InputEvent{{Kind: input.EventKindKey, Key: input.KeyPageUp, RawSeq: "\x1b[5~"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected completion %#v", got)
	}
}

func TestFrameSinkWritesFrameToOutput(t *testing.T) {
	var output bytes.Buffer
	sink := NewFrameSink(&output)
	if err := sink.WriteFrame(render.Frame{Lines: []string{"one", "two"}}); err != nil {
		t.Fatalf("write frame: %v", err)
	}
	got := output.String()
	if !strings.HasPrefix(got, synchronizedOutputBegin+hideCursor) || !strings.HasSuffix(got, synchronizedOutputEnd) {
		t.Fatalf("FrameSink should wrap frame in synchronized output and hide cursor before repaint, got %q", got)
	}
	for _, part := range []string{cursorHome, clearScreen, cursorPosition(1, 1) + clearLine + "one", cursorPosition(2, 1) + clearLine + "two"} {
		if !strings.Contains(got, part) {
			t.Fatalf("missing frame part %q in %q", part, got)
		}
	}
	if !strings.Contains(got, render.ANSIReset+hideCursor+synchronizedOutputEnd) {
		t.Fatalf("FrameSink should hide host cursor by default, got %q", got)
	}
	if strings.Contains(got, "\n") {
		t.Fatalf("FrameSink must not use linefeed row progression, got %q", got)
	}
}

func TestFrameSinkParksHiddenCursorAtFrameCursorRect(t *testing.T) {
	var output bytes.Buffer
	sink := NewFrameSink(&output)
	frame := render.Frame{
		Lines:      []string{"one", "two"},
		Cursor:     render.Cursor{Visible: true, Row: 0, Col: 0, Shape: render.CursorShapeBar},
		CursorRect: render.Rect{X: 4, Y: 2, W: 1, H: 1},
	}
	if err := sink.WriteFrame(frame); err != nil {
		t.Fatalf("write frame: %v", err)
	}
	got := output.String()
	if !strings.Contains(got, render.ANSIReset+hideCursor+cursorPosition(3, 5)+synchronizedOutputEnd) {
		t.Fatalf("FrameSink should park hidden host cursor at global cursor rect for IME anchor, got %q", got)
	}
	if strings.Contains(got, showCursor) {
		t.Fatalf("FrameSink must keep host cursor hidden during TUI frames, got %q", got)
	}
}

func TestFrameSinkPositionsFullWidthRowsWithoutLinefeeds(t *testing.T) {
	var output bytes.Buffer
	sink := NewFrameSink(&output)
	frame := render.Frame{Lines: []string{strings.Repeat("─", 8), "│      │"}, Metadata: render.RenderMetadata{Width: 8, Height: 2}}
	if err := sink.WriteFrame(frame); err != nil {
		t.Fatalf("write frame: %v", err)
	}
	got := output.String()
	if strings.Contains(got, "\n") {
		t.Fatalf("full-width frame must use absolute row positioning instead of linefeeds, got %q", got)
	}
	if !strings.Contains(got, cursorPosition(2, 1)+clearLine+"│      │") {
		t.Fatalf("expected second row absolute position, got %q", got)
	}
}

func TestFrameSinkPrefersANSIStyledFrame(t *testing.T) {
	var output bytes.Buffer
	sink := NewFrameSink(&output)
	frame := render.Frame{
		Lines:     []string{"plain"},
		ANSILines: []string{"\x1b[31mstyled\x1b[0m"},
		Metadata:  render.RenderMetadata{Width: 6, Height: 1},
	}
	if err := sink.WriteFrame(frame); err != nil {
		t.Fatalf("write frame: %v", err)
	}
	got := output.String()
	if !strings.Contains(got, "\x1b[31mstyled\x1b[0m") {
		t.Fatalf("expected ANSI styled line in output, got %q", got)
	}
	if strings.Contains(got, "plain") {
		t.Fatalf("FrameSink must not use plain snapshot when ANSI lines are present, got %q", got)
	}
	if !strings.Contains(got, render.ANSIReset+hideCursor) {
		t.Fatalf("FrameSink must reset style before hiding cursor at frame end, got %q", got)
	}
}

func readEvent(t *testing.T, host *Host) input.InputEvent {
	t.Helper()
	select {
	case event := <-host.InputEvents():
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for input event")
		return input.InputEvent{}
	}
}

type testSignal struct{}

func (testSignal) String() string {
	return "test-signal"
}

func (testSignal) Signal() {}
