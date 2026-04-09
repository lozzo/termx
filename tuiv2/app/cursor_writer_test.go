package app

import (
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	xansi "github.com/charmbracelet/x/ansi"
	localvterm "github.com/lozzow/termx/vterm"
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
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	sink := &cursorWriterProbeTTY{}
	writer := newOutputCursorWriter(sink)
	writer.QueueControlSequenceAfterWrite("<PROBE>")

	if err := writer.WriteFrame("frame-1\nframe-2", "<CURSOR>"); err != nil {
		t.Fatalf("write direct frame: %v", err)
	}

	sink.mu.Lock()
	writes := append([]string(nil), sink.writes...)
	sink.mu.Unlock()

	// 单次缓冲写入：所有序列合并为一次 io.WriteString
	wantSingle := synchronizedOutputBegin +
		hideHostCursorSequence +
		xansi.MoveCursorOrigin +
		"frame-1\r\nframe-2" +
		"<PROBE>" +
		"<CURSOR>" +
		synchronizedOutputEnd
	if len(writes) != 1 {
		t.Fatalf("expected single buffered write, got %d writes: %#v", len(writes), writes)
	}
	if writes[0] != wantSingle {
		t.Fatalf("unexpected buffered write:\n got %q\nwant %q", writes[0], wantSingle)
	}
}

func TestOutputCursorWriterCoalescesBurstDirectFrames(t *testing.T) {
	originalDelay := directFrameBatchDelay
	originalIdleThreshold := directFrameIdleThreshold
	directFrameBatchDelay = 20 * time.Millisecond
	directFrameIdleThreshold = time.Hour
	defer func() {
		directFrameBatchDelay = originalDelay
		directFrameIdleThreshold = originalIdleThreshold
	}()

	sink := &cursorWriterProbeTTY{}
	writer := newOutputCursorWriter(sink)
	writer.mu.Lock()
	writer.lastFlushAt = time.Now()
	writer.mu.Unlock()

	if err := writer.WriteFrame("frame-a", "<CURSOR-A>"); err != nil {
		t.Fatalf("write frame a: %v", err)
	}
	if err := writer.WriteFrame("frame-b", "<CURSOR-B>"); err != nil {
		t.Fatalf("write frame b: %v", err)
	}
	if err := writer.WriteFrame("frame-c", "<CURSOR-C>"); err != nil {
		t.Fatalf("write frame c: %v", err)
	}

	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		sink.mu.Lock()
		writes := append([]string(nil), sink.writes...)
		sink.mu.Unlock()
		if len(writes) >= 1 {
			got := strings.Join(writes, "")
			if strings.Contains(got, "frame-c") {
				if strings.Contains(got, "frame-a") || strings.Contains(got, "frame-b") {
					t.Fatalf("expected burst coalescing to keep only latest frame, got %#v", writes)
				}
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	t.Fatalf("timed out waiting for coalesced direct frame flush, got %#v", sink.writes)
}

func TestOutputCursorWriterDrainHookFiresAfterPendingFrameFlush(t *testing.T) {
	originalDelay := directFrameBatchDelay
	originalIdleThreshold := directFrameIdleThreshold
	directFrameBatchDelay = 20 * time.Millisecond
	directFrameIdleThreshold = time.Hour
	defer func() {
		directFrameBatchDelay = originalDelay
		directFrameIdleThreshold = originalIdleThreshold
	}()

	sink := &cursorWriterProbeTTY{}
	writer := newOutputCursorWriter(sink)
	writer.mu.Lock()
	writer.lastFlushAt = time.Now()
	writer.mu.Unlock()
	drained := make(chan struct{}, 1)
	writer.SetDrainHook(func() {
		select {
		case drained <- struct{}{}:
		default:
		}
	})

	if err := writer.WriteFrame("frame-a", "<CURSOR-A>"); err != nil {
		t.Fatalf("write frame a: %v", err)
	}
	if !writer.HasPendingFrame() {
		t.Fatal("expected pending frame backlog after buffered write")
	}

	select {
	case <-drained:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for drain hook")
	}
	if writer.HasPendingFrame() {
		t.Fatal("expected pending frame backlog cleared after flush")
	}
}

func TestOutputCursorWriterFlushesImmediatelyAfterIdle(t *testing.T) {
	originalDelay := directFrameBatchDelay
	originalIdleThreshold := directFrameIdleThreshold
	directFrameBatchDelay = 20 * time.Millisecond
	directFrameIdleThreshold = 10 * time.Millisecond
	defer func() {
		directFrameBatchDelay = originalDelay
		directFrameIdleThreshold = originalIdleThreshold
	}()

	sink := &cursorWriterProbeTTY{}
	writer := newOutputCursorWriter(sink)
	writer.mu.Lock()
	writer.lastFlushAt = time.Now().Add(-time.Second)
	writer.mu.Unlock()

	if err := writer.WriteFrame("frame-a", "<CURSOR-A>"); err != nil {
		t.Fatalf("write frame a: %v", err)
	}

	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.writes) != 1 {
		t.Fatalf("expected idle direct frame to flush immediately, got %#v", sink.writes)
	}
}

func TestOutputCursorWriterSkipsRedundantCursorOnlyDirectFrame(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	sink := &cursorWriterProbeTTY{}
	writer := newOutputCursorWriter(sink)

	if err := writer.WriteFrame("frame-a", "<CURSOR-A>"); err != nil {
		t.Fatalf("write frame a: %v", err)
	}
	if err := writer.WriteFrame("frame-a", "<CURSOR-A>"); err != nil {
		t.Fatalf("rewrite identical frame: %v", err)
	}

	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.writes) != 1 {
		t.Fatalf("expected redundant cursor-only direct frame to be skipped, got %#v", sink.writes)
	}
}

func TestOutputCursorWriterDiffsLaterRowsAtCorrectAbsoluteRow(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	sink := &cursorWriterProbeTTY{}
	writer := newOutputCursorWriter(sink)

	if err := writer.WriteFrame("row-1\nrow-2\nrow-3\nrow-4", "<CURSOR>"); err != nil {
		t.Fatalf("write initial frame: %v", err)
	}
	sink.mu.Lock()
	sink.writes = nil
	sink.mu.Unlock()

	if err := writer.WriteFrame("row-1\nrow-2\nrow-3\nROW-4", "<CURSOR>"); err != nil {
		t.Fatalf("write diff frame: %v", err)
	}

	sink.mu.Lock()
	got := strings.Join(sink.writes, "")
	sink.mu.Unlock()

	if !strings.Contains(got, "\x1b[4;1H") {
		t.Fatalf("expected later-row diff to target absolute row 4, got %q", got)
	}
	if strings.Contains(got, "\x1b[1;4H") {
		t.Fatalf("expected not to swap CUP row/column when diffing later rows, got %q", got)
	}
}

func TestOutputCursorWriterFullRepaintAfterScrollKeepsMiddleRowsInPlace(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	sink := &cursorWriterProbeTTY{}
	writer := newOutputCursorWriter(sink)

	frame1 := strings.Join([]string{
		"HDR-1........................",
		"HDR-2........................",
		"HDR-3........................",
		"row-01.......................",
		"row-02.......................",
		"row-03.......................",
		"row-04.......................",
		"row-05.......................",
		"row-06.......................",
		"FTR-1........................",
	}, "\n")
	frame2 := strings.Join([]string{
		"HDR-1........................",
		"HDR-2........................",
		"HDR-3........................",
		"row-02.......................",
		"row-03.......................",
		"row-04.......................",
		"row-05.......................",
		"row-06.......................",
		"row-07.......................",
		"FTR-1........................",
	}, "\n")
	frame3 := strings.Join([]string{
		"HDR-1........................",
		"HDR-2........................",
		"HDR-3........................",
		"mid-01.......................",
		"mid-02.......................",
		"MIDDLE-HELLO.................",
		"mid-04.......................",
		"mid-05.......................",
		"mid-06.......................",
		"FTR-2........................",
	}, "\n")

	for _, frame := range []string{frame1, frame2, frame3} {
		if err := writer.WriteFrame(frame, ""); err != nil {
			t.Fatalf("write frame %q: %v", frame, err)
		}
	}

	sink.mu.Lock()
	stream := strings.Join(sink.writes, "")
	sink.mu.Unlock()

	vt := localvterm.New(32, 10, 0, nil)
	if _, err := vt.Write([]byte(stream)); err != nil {
		t.Fatalf("replay writer output into host vterm: %v", err)
	}

	lines := vtermScreenLines(vt.ScreenContent())
	if got := strings.TrimRight(lines[5], " "); !strings.Contains(got, "MIDDLE-HELLO") {
		t.Fatalf("expected full repaint after scroll to keep middle text on row 6, got row6=%q full=%#v", got, lines)
	}
	if got := strings.TrimRight(lines[0], " "); strings.Contains(got, "MIDDLE-HELLO") {
		t.Fatalf("expected middle text not to jump to top row, got row1=%q full=%#v", got, lines)
	}
}

func TestTruncateFrameToWidthClipsEachRenderedLine(t *testing.T) {
	frame := "123456\nabcdef"
	if got, want := truncateFrameToWidth(frame, 4), "1234\nabcd"; got != want {
		t.Fatalf("expected direct frame truncation to clip each line, got %q want %q", got, want)
	}
}

func vtermScreenLines(screen localvterm.ScreenData) []string {
	lines := make([]string, len(screen.Cells))
	for y, row := range screen.Cells {
		var b strings.Builder
		for _, cell := range row {
			content := cell.Content
			if content == "" {
				content = " "
			}
			b.WriteString(content)
		}
		lines[y] = b.String()
	}
	return lines
}

func TestNormalizeFrameForTTYUsesCRLF(t *testing.T) {
	frame := "a\nb\nc"
	if got, want := normalizeFrameForTTY(frame), "a\r\nb\r\nc"; got != want {
		t.Fatalf("expected direct frame output to normalize line endings, got %q want %q", got, want)
	}
}
