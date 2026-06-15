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
	for _, seq := range []string{requestHostFG, requestHostBG, "\x1b]4;0;?\x07", "\x1b]4;15;?\x07"} {
		if !strings.Contains(gotEnter, seq) {
			t.Fatalf("missing theme probe sequence %q in %q", seq, gotEnter)
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

func TestHostThemeProbeCanBeDisabled(t *testing.T) {
	ops := &fakeTerminalOps{terminal: true}
	reader := newBlockingCancelReader()
	var output bytes.Buffer
	host := New(
		WithInput(strings.NewReader(""), 7),
		WithOutput(&output),
		WithTerminalOps(ops),
		WithThemeProbe(false),
		WithCancelReaderFactory(func(io.Reader) (CancelReader, error) {
			return reader, nil
		}),
	)
	if err := host.Enter(context.Background()); err != nil {
		t.Fatalf("enter: %v", err)
	}
	if got := output.String(); strings.Contains(got, requestHostFG) || strings.Contains(got, "\x1b]4;0;?\x07") {
		t.Fatalf("theme probe should be disabled, got %q", got)
	}
	if err := host.Close(); err != nil {
		t.Fatalf("close: %v", err)
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
		WithThemeProbe(false),
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

func TestInputParserConsumesOSCThemeResponses(t *testing.T) {
	parser := NewInputParser()
	got := parser.Feed([]byte("\x1b]10;rgb:aaaa/bbbb/cccc\a\x1b]11;#010203\x1b\\\x1b]4;5;rgb:4444/8888/cccc\a"))
	want := []input.InputEvent{
		{Kind: input.EventKindHostTheme, Theme: input.HostThemeEvent{DefaultFG: "#aabbcc"}, RawSeq: "\x1b]10;rgb:aaaa/bbbb/cccc\a"},
		{Kind: input.EventKindHostTheme, Theme: input.HostThemeEvent{DefaultBG: "#010203"}, RawSeq: "\x1b]11;#010203\x1b\\"},
		{Kind: input.EventKindHostTheme, Theme: input.HostThemeEvent{PaletteIndex: 5, PaletteColor: "#4488cc"}, RawSeq: "\x1b]4;5;rgb:4444/8888/cccc\a"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected OSC theme events\n got: %#v\nwant: %#v", got, want)
	}
	for _, event := range got {
		if event.Kind == input.EventKindKey {
			t.Fatalf("theme OSC response must not leak as key input: %#v", event)
		}
	}
}

func TestInputParserConvertsExtendedKeysAndModifiers(t *testing.T) {
	parser := NewInputParser()
	got := parser.Feed([]byte("\x1b[Z\x1b[H\x1b[F\x1b[2~\x1b[3~\x1bOP\x1b[15~\x1b[1;5D\x1b[1;3C"))
	want := []input.InputEvent{
		{Kind: input.EventKindKey, Key: input.KeyShiftTab, Shift: true, RawSeq: "\x1b[Z"},
		{Kind: input.EventKindKey, Key: input.KeyHome, RawSeq: "\x1b[H"},
		{Kind: input.EventKindKey, Key: input.KeyEnd, RawSeq: "\x1b[F"},
		{Kind: input.EventKindKey, Key: input.KeyInsert, RawSeq: "\x1b[2~"},
		{Kind: input.EventKindKey, Key: input.KeyDelete, RawSeq: "\x1b[3~"},
		{Kind: input.EventKindKey, Key: input.KeyF1, RawSeq: "\x1bOP"},
		{Kind: input.EventKindKey, Key: input.KeyF5, RawSeq: "\x1b[15~"},
		{Kind: input.EventKindKey, Key: input.KeyLeft, Ctrl: true, RawSeq: "\x1b[1;5D"},
		{Kind: input.EventKindKey, Key: input.KeyRight, Alt: true, RawSeq: "\x1b[1;3C"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected extended key events\n got: %#v\nwant: %#v", got, want)
	}
}

func TestInputParserConvertsSGRMouseButtonsAndRelease(t *testing.T) {
	parser := NewInputParser()
	got := parser.Feed([]byte("\x1b[<1;2;3M\x1b[<33;2;3M\x1b[<1;2;3m\x1b[<2;4;5M\x1b[<34;4;5M\x1b[<2;4;5m\x1b[<35;6;7M"))
	want := []input.InputEvent{
		{Kind: input.EventKindMouse, Mouse: input.MouseMiddle, Row: 3, Col: 2, RawSeq: "\x1b[<1;2;3M"},
		{Kind: input.EventKindMouse, Mouse: input.MouseMiddleDrag, Row: 3, Col: 2, RawSeq: "\x1b[<33;2;3M"},
		{Kind: input.EventKindMouse, Mouse: input.MouseMiddleUp, Row: 3, Col: 2, RawSeq: "\x1b[<1;2;3m"},
		{Kind: input.EventKindMouse, Mouse: input.MouseRight, Row: 5, Col: 4, RawSeq: "\x1b[<2;4;5M"},
		{Kind: input.EventKindMouse, Mouse: input.MouseRightDrag, Row: 5, Col: 4, RawSeq: "\x1b[<34;4;5M"},
		{Kind: input.EventKindMouse, Mouse: input.MouseRightUp, Row: 5, Col: 4, RawSeq: "\x1b[<2;4;5m"},
		{Kind: input.EventKindMouse, Mouse: input.MouseMove, Row: 7, Col: 6, RawSeq: "\x1b[<35;6;7M"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected SGR mouse events\n got: %#v\nwant: %#v", got, want)
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
	if err := sink.WriteFrame(render.Frame{Lines: []string{"one", "two"}, Metadata: render.RenderMetadata{Width: 3, Height: 2}}); err != nil {
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

func TestFrameSinkWritesFE0FLineWithModelColumnAnchors(t *testing.T) {
	var output bytes.Buffer
	sink := NewFrameSink(&output)
	line := render.Line{Cells: []render.Cell{
		{Text: "♻️", Width: 2, TerminalContent: true, Safe: true},
		{Text: "♻️", Width: 2, TerminalContent: true, Safe: true},
		{Text: "♻️", Width: 2, TerminalContent: true, Safe: true},
		{Text: "·", Width: 1, Safe: true},
		{Text: "·", Width: 1, Safe: true},
	}}
	frame := render.Frame{
		ANSILines: []string{line.ANSIString(render.DefaultTheme())},
		Metadata:  render.RenderMetadata{Width: 8, Height: 1},
	}
	if err := sink.WriteFrame(frame); err != nil {
		t.Fatalf("write frame: %v", err)
	}

	got := output.String()
	for _, part := range []string{
		"♻️\x1b[1X\x1b[3G♻️",
		"♻️\x1b[1X\x1b[5G♻️",
		"♻️\x1b[1X\x1b[7G·",
	} {
		if !strings.Contains(got, part) {
			t.Fatalf("FE0F frame should erase continuation cells and preserve model column anchors %q in %q", part, got)
		}
	}
}

func TestFrameSinkSkipsUnchangedRows(t *testing.T) {
	var output bytes.Buffer
	sink := NewFrameSink(&output)
	frame := render.Frame{Lines: []string{"one", "two"}, Metadata: render.RenderMetadata{Width: 3, Height: 2}}
	if err := sink.WriteFrame(frame); err != nil {
		t.Fatalf("write first frame: %v", err)
	}
	output.Reset()
	if err := sink.WriteFrame(frame); err != nil {
		t.Fatalf("write second frame: %v", err)
	}
	got := output.String()
	if got != "" {
		t.Fatalf("unchanged frame should not write to host output, got %q", got)
	}
}

func TestFrameSinkWritesOnlyChangedRows(t *testing.T) {
	var output bytes.Buffer
	sink := NewFrameSink(&output)
	if err := sink.WriteFrame(render.Frame{Lines: []string{"one", "two"}, Metadata: render.RenderMetadata{Width: 5, Height: 2}}); err != nil {
		t.Fatalf("write first frame: %v", err)
	}
	output.Reset()
	if err := sink.WriteFrame(render.Frame{Lines: []string{"one", "three"}, Metadata: render.RenderMetadata{Width: 5, Height: 2}}); err != nil {
		t.Fatalf("write second frame: %v", err)
	}
	got := output.String()
	if strings.Contains(got, clearScreen) {
		t.Fatalf("same-size changed frame should not clear the screen, got %q", got)
	}
	if strings.Contains(got, cursorPosition(1, 1)+clearLine+"one") || !strings.Contains(got, cursorPosition(2, 1)+clearLine+"three") {
		t.Fatalf("expected only second row repaint, got %q", got)
	}
}

func TestFrameSinkUsesScrollRegionForOneRowShiftUp(t *testing.T) {
	var output bytes.Buffer
	sink := NewFrameSink(&output)
	if err := sink.WriteFrame(render.Frame{
		Lines:    []string{"top", "one", "two", "three", "bottom"},
		Metadata: render.RenderMetadata{Width: 8, Height: 5},
	}); err != nil {
		t.Fatalf("write first frame: %v", err)
	}
	output.Reset()
	if err := sink.WriteFrame(render.Frame{
		Lines:    []string{"top", "two", "three", "four", "bottom"},
		Metadata: render.RenderMetadata{Width: 8, Height: 5},
	}); err != nil {
		t.Fatalf("write shifted frame: %v", err)
	}
	got := output.String()
	if strings.Contains(got, clearScreen) {
		t.Fatalf("one-row scroll should not clear screen, got %q", got)
	}
	for _, part := range []string{scrollRegion(2, 4), cursorPosition(4, 1) + scrollUpOne, resetScrollRegion, cursorPosition(4, 1) + clearLine + "four"} {
		if !strings.Contains(got, part) {
			t.Fatalf("missing scroll-region part %q in %q", part, got)
		}
	}
	if strings.Contains(got, cursorPosition(2, 1)+clearLine+"two") || strings.Contains(got, cursorPosition(3, 1)+clearLine+"three") {
		t.Fatalf("one-row scroll should not repaint shifted rows, got %q", got)
	}
}

func TestFrameSinkUsesScrollRegionForOneRowShiftDown(t *testing.T) {
	var output bytes.Buffer
	sink := NewFrameSink(&output)
	if err := sink.WriteFrame(render.Frame{
		Lines:    []string{"top", "two", "three", "four", "bottom"},
		Metadata: render.RenderMetadata{Width: 8, Height: 5},
	}); err != nil {
		t.Fatalf("write first frame: %v", err)
	}
	output.Reset()
	if err := sink.WriteFrame(render.Frame{
		Lines:    []string{"top", "one", "two", "three", "bottom"},
		Metadata: render.RenderMetadata{Width: 8, Height: 5},
	}); err != nil {
		t.Fatalf("write shifted frame: %v", err)
	}
	got := output.String()
	for _, part := range []string{scrollRegion(2, 4), cursorPosition(2, 1) + scrollDownOne, resetScrollRegion, cursorPosition(2, 1) + clearLine + "one"} {
		if !strings.Contains(got, part) {
			t.Fatalf("missing scroll-region part %q in %q", part, got)
		}
	}
	if strings.Contains(got, cursorPosition(3, 1)+clearLine+"two") || strings.Contains(got, cursorPosition(4, 1)+clearLine+"three") {
		t.Fatalf("one-row scroll should not repaint shifted rows, got %q", got)
	}
}

func TestFrameSinkWritesIncrementalScrollPatch(t *testing.T) {
	var output bytes.Buffer
	sink := NewFrameSink(&output)
	frame := render.Frame{
		Patch: &render.FramePatch{
			Rect:      render.Rect{X: 1, Y: 2, W: 10, H: 4},
			Dir:       render.FramePatchScrollUp,
			LineY:     5,
			LineX:     1,
			LineWidth: 10,
			LineANSI:  "new row",
		},
		Cursor:     render.Cursor{Visible: true, Shape: render.CursorShapeBlock},
		CursorRect: render.Rect{X: 3, Y: 4, W: 1, H: 1},
		Metadata:   render.RenderMetadata{Width: 20, Height: 10},
	}
	if err := sink.WriteFrame(frame); err != nil {
		t.Fatalf("write patch frame: %v", err)
	}
	got := output.String()
	for _, part := range []string{scrollRegion(3, 6), cursorPosition(6, 1) + scrollUpOne, resetScrollRegion, cursorPosition(6, 2) + eraseChars(10) + "new row"} {
		if !strings.Contains(got, part) {
			t.Fatalf("missing patch part %q in %q", part, got)
		}
	}
	if strings.Contains(got, clearScreen) || strings.Contains(got, clearLine) {
		t.Fatalf("incremental patch must not clear whole screen or line, got %q", got)
	}
}

func TestFrameSinkWritesMultiLineIncrementalScrollPatch(t *testing.T) {
	var output bytes.Buffer
	sink := NewFrameSink(&output)
	frame := render.Frame{
		Patch: &render.FramePatch{
			Rect:      render.Rect{X: 2, Y: 1, W: 8, H: 6},
			Dir:       render.FramePatchScrollUp,
			LineY:     5,
			LineX:     2,
			LineWidth: 8,
			LinesANSI: []string{"new a", "new b"},
		},
		Metadata: render.RenderMetadata{Width: 20, Height: 10},
	}
	if err := sink.WriteFrame(frame); err != nil {
		t.Fatalf("write patch frame: %v", err)
	}
	got := output.String()
	for _, part := range []string{scrollRegion(2, 7), cursorPosition(7, 1) + scrollUpOne + scrollUpOne, cursorPosition(6, 3) + eraseChars(8) + "new a", cursorPosition(7, 3) + eraseChars(8) + "new b"} {
		if !strings.Contains(got, part) {
			t.Fatalf("missing multi-line patch part %q in %q", part, got)
		}
	}
}

func TestFrameSinkWritesRewritePatchWithoutScrollRegion(t *testing.T) {
	var output bytes.Buffer
	sink := NewFrameSink(&output)
	frame := render.Frame{
		Patch: &render.FramePatch{
			Rect:      render.Rect{X: 2, Y: 1, W: 8, H: 3},
			Rewrite:   true,
			LineY:     1,
			LineX:     2,
			LineWidth: 8,
			LinesANSI: []string{"row a", "row b", "row c"},
		},
		Cursor:     render.Cursor{Visible: true, Shape: render.CursorShapeBlock},
		CursorRect: render.Rect{X: 4, Y: 2, W: 1, H: 1},
		Metadata:   render.RenderMetadata{Width: 20, Height: 10},
	}
	if err := sink.WriteFrame(frame); err != nil {
		t.Fatalf("write rewrite patch frame: %v", err)
	}
	got := output.String()
	for _, part := range []string{cursorPosition(2, 3) + eraseChars(8) + "row a", cursorPosition(3, 3) + eraseChars(8) + "row b", cursorPosition(4, 3) + eraseChars(8) + "row c"} {
		if !strings.Contains(got, part) {
			t.Fatalf("missing rewrite patch part %q in %q", part, got)
		}
	}
	if strings.Contains(got, scrollRegion(2, 4)) || strings.Contains(got, scrollUpOne) || strings.Contains(got, scrollDownOne) || strings.Contains(got, clearLine) {
		t.Fatalf("rewrite patch must not scroll or clear pane border columns, got %q", got)
	}
}

func TestFrameSinkWritesCursorOnlyChange(t *testing.T) {
	var output bytes.Buffer
	sink := NewFrameSink(&output)
	frame := render.Frame{Lines: []string{"one"}, Metadata: render.RenderMetadata{Width: 3, Height: 1}}
	if err := sink.WriteFrame(frame); err != nil {
		t.Fatalf("write first frame: %v", err)
	}
	output.Reset()
	frame.Cursor = render.Cursor{Visible: true, Shape: render.CursorShapeBar}
	frame.CursorRect = render.Rect{X: 1, Y: 0, W: 1, H: 1}
	if err := sink.WriteFrame(frame); err != nil {
		t.Fatalf("write cursor frame: %v", err)
	}
	got := output.String()
	if strings.Contains(got, clearScreen) || strings.Contains(got, "one") {
		t.Fatalf("cursor-only change should not repaint rows, got %q", got)
	}
	if !strings.Contains(got, cursorShapeBar+cursorPosition(1, 2)+showCursor) {
		t.Fatalf("cursor-only change should write cursor sequence, got %q", got)
	}
}

func TestFrameSinkWritesCursorOnlyPatchWithoutRowDiff(t *testing.T) {
	var output bytes.Buffer
	sink := NewFrameSink(&output)
	frame := render.Frame{
		Patch:      &render.FramePatch{CursorOnly: true},
		Cursor:     render.Cursor{Visible: true, Shape: render.CursorShapeBlock},
		CursorRect: render.Rect{X: 2, Y: 3, W: 1, H: 1},
	}
	if err := sink.WriteFrame(frame); err != nil {
		t.Fatalf("write cursor-only patch: %v", err)
	}
	got := output.String()
	if strings.Contains(got, clearScreen) || strings.Contains(got, clearLine) || strings.Contains(got, scrollUpOne) || strings.Contains(got, scrollDownOne) {
		t.Fatalf("cursor-only patch must not repaint or scroll, got %q", got)
	}
	if !strings.Contains(got, cursorShapeBlock+cursorPosition(4, 3)+showCursor) {
		t.Fatalf("cursor-only patch should only project cursor, got %q", got)
	}
}

func TestFrameSinkClearsOnFrameSizeChange(t *testing.T) {
	var output bytes.Buffer
	sink := NewFrameSink(&output)
	if err := sink.WriteFrame(render.Frame{Lines: []string{"one", "two"}, Metadata: render.RenderMetadata{Width: 3, Height: 2}}); err != nil {
		t.Fatalf("write first frame: %v", err)
	}
	output.Reset()
	if err := sink.WriteFrame(render.Frame{Lines: []string{"one", "two", "three"}, Metadata: render.RenderMetadata{Width: 5, Height: 3}}); err != nil {
		t.Fatalf("write resized frame: %v", err)
	}
	got := output.String()
	if !strings.Contains(got, cursorHome+clearScreen) || !strings.Contains(got, cursorPosition(3, 1)+clearLine+"three") {
		t.Fatalf("resized frame should repaint fully, got %q", got)
	}
}

func TestFrameSinkShowsVisibleCursorAtFrameCursorRect(t *testing.T) {
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
	if !strings.Contains(got, render.ANSIReset+cursorShapeBar+cursorPosition(3, 5)+showCursor+synchronizedOutputEnd) {
		t.Fatalf("FrameSink should show host cursor at global cursor rect, got %q", got)
	}
	if !strings.HasPrefix(got, synchronizedOutputBegin+hideCursor) {
		t.Fatalf("FrameSink should hide cursor during repaint before showing final cursor, got %q", got)
	}
}

func TestFrameSinkParksHiddenCursorForAnchorOnlyFrame(t *testing.T) {
	var output bytes.Buffer
	sink := NewFrameSink(&output)
	frame := render.Frame{
		Lines:      []string{"one", "two"},
		Cursor:     render.Cursor{Anchor: true, Shape: render.CursorShapeBar},
		CursorRect: render.Rect{X: 4, Y: 2, W: 1, H: 1},
	}
	if err := sink.WriteFrame(frame); err != nil {
		t.Fatalf("write frame: %v", err)
	}
	got := output.String()
	if !strings.Contains(got, render.ANSIReset+hideCursor+cursorShapeBar+cursorPosition(3, 5)+synchronizedOutputEnd) {
		t.Fatalf("FrameSink should park hidden host cursor at global anchor rect, got %q", got)
	}
	if strings.Contains(got, showCursor) {
		t.Fatalf("anchor-only cursor must remain hidden, got %q", got)
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
		t.Fatalf("FrameSink must reset style before final cursor sequence, got %q", got)
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
