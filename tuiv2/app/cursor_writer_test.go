package app

import (
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	xansi "github.com/charmbracelet/x/ansi"
)

type cursorWriterProbeModel struct {
	view string
}

func (m cursorWriterProbeModel) Init() tea.Cmd {
	return tea.Tick(10*time.Millisecond, func(time.Time) tea.Msg {
		return tea.Quit()
	})
}

func (m cursorWriterProbeModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return m, nil
}

func (m cursorWriterProbeModel) View() string {
	return m.view
}

type cursorWriterProbeSink struct {
	mu     sync.Mutex
	writes []string
}

func (s *cursorWriterProbeSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writes = append(s.writes, string(append([]byte(nil), p...)))
	return len(p), nil
}

type cursorWriterProbeTTY struct {
	cursorWriterProbeSink
}

func (s *cursorWriterProbeTTY) Read([]byte) (int, error) {
	return 0, io.EOF
}

func (s *cursorWriterProbeTTY) Close() error {
	return nil
}

func (s *cursorWriterProbeTTY) Fd() uintptr {
	return 1
}

func TestOutputCursorWriterInterleavesCursorSequenceWithBubbleTeaWrites(t *testing.T) {
	sink := &cursorWriterProbeSink{}
	writer := newOutputCursorWriter(sink)
	writer.SetCursorSequence("<CURSOR>")

	program := tea.NewProgram(
		cursorWriterProbeModel{
			view: "│# lozzow@RedmiBook♻️: ~/Documents/workdir/termx <>                                                  (23:17:15)   │",
		},
		tea.WithInput(nil),
		tea.WithOutput(writer),
		tea.WithAltScreen(),
	)
	if _, err := program.Run(); err != nil {
		t.Fatalf("run bubbletea probe: %v", err)
	}

	sink.mu.Lock()
	writes := append([]string(nil), sink.writes...)
	sink.mu.Unlock()

	if len(writes) < 4 {
		t.Fatalf("expected multiple writes through output cursor writer, got %#v", writes)
	}

	frameChunk := -1
	cursorChunkAfterFrame := false
	totalCursorChunks := 0
	for i, write := range writes {
		if write == "<CURSOR>" {
			totalCursorChunks++
			if frameChunk >= 0 {
				cursorChunkAfterFrame = true
			}
			continue
		}
		if strings.Contains(write, "RedmiBook♻️") {
			frameChunk = i
		}
	}

	if frameChunk < 0 {
		t.Fatalf("expected probe frame chunk in writes, got %#v", writes)
	}
	if totalCursorChunks == 0 {
		t.Fatalf("expected cursor sequence to be injected at least once, got %#v", writes)
	}
	if !cursorChunkAfterFrame {
		t.Fatalf("expected cursor sequence after frame bytes, got %#v", writes)
	}
}

func TestOutputCursorWriterQueuesControlSequenceAfterNextWrite(t *testing.T) {
	sink := &cursorWriterProbeSink{}
	writer := newOutputCursorWriter(sink)
	writer.QueueControlSequenceAfterWrite("<PROBE>")

	if _, err := writer.Write([]byte("frame-1")); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if _, err := writer.Write([]byte("frame-2")); err != nil {
		t.Fatalf("second write: %v", err)
	}

	sink.mu.Lock()
	writes := append([]string(nil), sink.writes...)
	sink.mu.Unlock()

	if len(writes) != 3 {
		t.Fatalf("expected first frame, one queued probe, second frame; got %#v", writes)
	}
	if writes[0] != "frame-1" || writes[1] != "<PROBE>" || writes[2] != "frame-2" {
		t.Fatalf("unexpected queued write order %#v", writes)
	}
}

func TestOutputCursorWriterRestoresBubbleTeaCursorBeforeNextFrame(t *testing.T) {
	sink := &cursorWriterProbeSink{}
	writer := newOutputCursorWriter(sink)
	writer.SetCursorSequence("<CURSOR>")

	const anchor = "\x1b[;5H"
	if _, err := writer.Write([]byte("frame-1" + anchor)); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if _, err := writer.Write([]byte("frame-2")); err != nil {
		t.Fatalf("second write: %v", err)
	}

	sink.mu.Lock()
	writes := append([]string(nil), sink.writes...)
	sink.mu.Unlock()

	want := []string{
		hideHostCursorSequence,
		"frame-1" + anchor,
		"<CURSOR>",
		anchor,
		hideHostCursorSequence,
		"frame-2",
		"<CURSOR>",
	}
	if len(writes) != len(want) {
		t.Fatalf("expected restored frame write sequence %#v, got %#v", want, writes)
	}
	for i := range want {
		if writes[i] != want[i] {
			t.Fatalf("unexpected write %d: got %q want %q; full=%#v", i, writes[i], want[i], writes)
		}
	}
}

func TestOutputCursorWriterRestoresBubbleTeaCursorBeforeControlWrites(t *testing.T) {
	sink := &cursorWriterProbeSink{}
	writer := newOutputCursorWriter(sink)
	writer.SetCursorSequence("<CURSOR>")

	const anchor = "\x1b[;5H"
	if _, err := writer.Write([]byte("frame-1" + anchor)); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if _, err := writer.Write([]byte("\x1b[2K")); err != nil {
		t.Fatalf("control write: %v", err)
	}

	sink.mu.Lock()
	writes := append([]string(nil), sink.writes...)
	sink.mu.Unlock()

	want := []string{
		hideHostCursorSequence,
		"frame-1" + anchor,
		"<CURSOR>",
		anchor,
		hideHostCursorSequence,
		"\x1b[2K",
		"<CURSOR>",
	}
	if len(writes) != len(want) {
		t.Fatalf("expected restored control write sequence %#v, got %#v", want, writes)
	}
	for i := range want {
		if writes[i] != want[i] {
			t.Fatalf("unexpected write %d: got %q want %q; full=%#v", i, writes[i], want[i], writes)
		}
	}
}

func TestOutputCursorWriterRestoresBubbleTeaCursorBeforeFrameAfterControlWrite(t *testing.T) {
	sink := &cursorWriterProbeSink{}
	writer := newOutputCursorWriter(sink)
	writer.SetCursorSequence("<CURSOR>")

	const anchor = "\x1b[;5H"
	if _, err := writer.Write([]byte("frame-1" + anchor)); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if _, err := writer.Write([]byte("\x1b[2K")); err != nil {
		t.Fatalf("control write: %v", err)
	}
	if _, err := writer.Write([]byte("frame-2")); err != nil {
		t.Fatalf("second frame: %v", err)
	}

	sink.mu.Lock()
	writes := append([]string(nil), sink.writes...)
	sink.mu.Unlock()

	want := []string{
		hideHostCursorSequence,
		"frame-1" + anchor,
		"<CURSOR>",
		anchor,
		hideHostCursorSequence,
		"\x1b[2K",
		"<CURSOR>",
		anchor,
		hideHostCursorSequence,
		"frame-2",
		"<CURSOR>",
	}
	if len(writes) != len(want) {
		t.Fatalf("expected restored frame-after-control write sequence %#v, got %#v", want, writes)
	}
	for i := range want {
		if writes[i] != want[i] {
			t.Fatalf("unexpected write %d: got %q want %q; full=%#v", i, writes[i], want[i], writes)
		}
	}
}

func TestOutputCursorWriterWrapsTTYWritesWithSynchronizedOutput(t *testing.T) {
	sink := &cursorWriterProbeTTY{}
	writer := newOutputCursorWriter(sink)
	writer.QueueControlSequenceAfterWrite("<PROBE>")
	writer.SetCursorSequence("<CURSOR>")

	if _, err := writer.Write([]byte("frame")); err != nil {
		t.Fatalf("write frame: %v", err)
	}

	sink.mu.Lock()
	writes := append([]string(nil), sink.writes...)
	sink.mu.Unlock()

	want := []string{
		synchronizedOutputBegin,
		hideHostCursorSequence,
		"frame",
		"<PROBE>",
		"<CURSOR>",
		synchronizedOutputEnd,
	}
	if len(writes) != len(want) {
		t.Fatalf("expected synchronized frame write sequence %#v, got %#v", want, writes)
	}
	for i := range want {
		if writes[i] != want[i] {
			t.Fatalf("unexpected write %d: got %q want %q; full=%#v", i, writes[i], want[i], writes)
		}
	}
}

func TestOutputCursorWriterStripsEmbeddedCursorFromFramePayload(t *testing.T) {
	sink := &cursorWriterProbeSink{}
	writer := newOutputCursorWriter(sink)
	writer.SetCursorSequence("<CURSOR>")

	if _, err := writer.Write([]byte("frame<CURSOR>\x1b[;5H")); err != nil {
		t.Fatalf("write frame: %v", err)
	}

	sink.mu.Lock()
	writes := append([]string(nil), sink.writes...)
	sink.mu.Unlock()

	want := []string{
		hideHostCursorSequence,
		"frame\x1b[;5H",
		"<CURSOR>",
	}
	if len(writes) != len(want) {
		t.Fatalf("expected stripped embedded-cursor write sequence %#v, got %#v", want, writes)
	}
	for i := range want {
		if writes[i] != want[i] {
			t.Fatalf("unexpected write %d: got %q want %q; full=%#v", i, writes[i], want[i], writes)
		}
	}
}

func TestOutputCursorWriterEnterAndExitDirectTerminal(t *testing.T) {
	sink := &cursorWriterProbeTTY{}
	writer := newOutputCursorWriter(sink)

	if err := writer.enterDirectTerminal(); err != nil {
		t.Fatalf("enter direct terminal: %v", err)
	}
	if err := writer.exitDirectTerminal(); err != nil {
		t.Fatalf("exit direct terminal: %v", err)
	}

	sink.mu.Lock()
	writes := append([]string(nil), sink.writes...)
	sink.mu.Unlock()

	want := []string{
		xansi.HideCursor,
		xansi.EnableAltScreenBuffer,
		xansi.EraseEntireDisplay + xansi.MoveCursorOrigin,
		xansi.HideCursor,
		xansi.EnableBracketedPaste,
		xansi.EnableMouseCellMotion + xansi.EnableMouseSgrExt,
		xansi.DisableBracketedPaste,
		xansi.ShowCursor,
		xansi.DisableMouseCellMotion + xansi.DisableMouseSgrExt,
		xansi.DisableAltScreenBuffer,
	}
	if len(writes) != len(want) {
		t.Fatalf("expected direct terminal lifecycle writes %#v, got %#v", want, writes)
	}
	for i := range want {
		if writes[i] != want[i] {
			t.Fatalf("unexpected write %d: got %q want %q; full=%#v", i, writes[i], want[i], writes)
		}
	}
}

func TestOutputCursorWriterWritesDirectFrame(t *testing.T) {
	sink := &cursorWriterProbeTTY{}
	writer := newOutputCursorWriter(sink)
	writer.QueueControlSequenceAfterWrite("<PROBE>")

	if err := writer.WriteFrame("frame", "<CURSOR>"); err != nil {
		t.Fatalf("write direct frame: %v", err)
	}

	sink.mu.Lock()
	writes := append([]string(nil), sink.writes...)
	sink.mu.Unlock()

	want := []string{
		synchronizedOutputBegin,
		hideHostCursorSequence,
		xansi.MoveCursorOrigin,
		"frame",
		"<PROBE>",
		"<CURSOR>",
		synchronizedOutputEnd,
	}
	if len(writes) != len(want) {
		t.Fatalf("expected direct frame write sequence %#v, got %#v", want, writes)
	}
	for i := range want {
		if writes[i] != want[i] {
			t.Fatalf("unexpected write %d: got %q want %q; full=%#v", i, writes[i], want[i], writes)
		}
	}
}

func TestTruncateFrameToWidthClipsEachRenderedLine(t *testing.T) {
	frame := "123456\nabcdef"
	if got, want := truncateFrameToWidth(frame, 4), "1234\nabcd"; got != want {
		t.Fatalf("expected direct frame truncation to clip each line, got %q want %q", got, want)
	}
}
