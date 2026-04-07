package app

import (
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
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
		"frame-1" + anchor,
		"<CURSOR>",
		anchor,
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
		"frame-1" + anchor,
		"<CURSOR>",
		anchor,
		"\x1b[2K",
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
