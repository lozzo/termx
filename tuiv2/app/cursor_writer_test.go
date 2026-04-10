package app

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/workbench"
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

func TestOutputCursorWriterDirectFrameDumpCapturesExactBufferedPayload(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	dumpPath := filepath.Join(t.TempDir(), "frames.bin")
	t.Setenv("TERMX_FRAME_DUMP", dumpPath)

	sink := &cursorWriterProbeTTY{}
	writer := newOutputCursorWriter(sink)
	frame := xansi.CHA(1) + "AAA❄️" + xansi.ECH(1) + xansi.CHA(6) + "BB" + "\x1b[0m\x1b[K"

	if err := writer.WriteFrame(frame, "<CURSOR>"); err != nil {
		t.Fatalf("write direct frame: %v", err)
	}

	sink.mu.Lock()
	writes := append([]string(nil), sink.writes...)
	sink.mu.Unlock()
	if len(writes) != 1 {
		t.Fatalf("expected single buffered write, got %#v", writes)
	}

	data, err := os.ReadFile(dumpPath)
	if err != nil {
		t.Fatalf("read frame dump: %v", err)
	}
	dump := string(data)
	if !strings.Contains(dump, "--- direct_frame ") {
		t.Fatalf("expected direct-frame dump header, got %q", dump)
	}
	if !strings.Contains(dump, writes[0]) {
		t.Fatalf("expected dump to include exact buffered output %q, got %q", writes[0], dump)
	}
	if !strings.Contains(dump, "❄️"+xansi.ECH(1)+xansi.CHA(6)+"BB") {
		t.Fatalf("expected dump to preserve FE0F + ECH + CHA bytes, got %q", dump)
	}
}

func TestOutputCursorWriterControlSequenceDumpCapturesRawProbeBytes(t *testing.T) {
	dumpPath := filepath.Join(t.TempDir(), "frames.bin")
	t.Setenv("TERMX_FRAME_DUMP", dumpPath)

	sink := &cursorWriterProbeSink{}
	writer := newOutputCursorWriter(sink)
	seq := xansi.SaveCursor + "❄️" + xansi.ECH(1) + xansi.RequestExtendedCursorPositionReport + xansi.RestoreCursor

	if err := writer.WriteControlSequence(seq); err != nil {
		t.Fatalf("write control sequence: %v", err)
	}

	data, err := os.ReadFile(dumpPath)
	if err != nil {
		t.Fatalf("read frame dump: %v", err)
	}
	dump := string(data)
	if !strings.Contains(dump, "--- control_sequence ") {
		t.Fatalf("expected control-sequence dump header, got %q", dump)
	}
	if !strings.Contains(dump, seq) {
		t.Fatalf("expected dump to include exact control sequence %q, got %q", seq, dump)
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

func TestOutputCursorWriterFlushesImmediatelyAfterInteractiveInput(t *testing.T) {
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
	writer.SetInteractiveFlushHint(func() bool { return true })

	if err := writer.WriteFrame("frame-a", "<CURSOR-A>"); err != nil {
		t.Fatalf("write frame a: %v", err)
	}

	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.writes) != 1 {
		t.Fatalf("expected interactive direct frame to flush immediately, got %#v", sink.writes)
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

func TestOutputCursorWriterDiffsChangedSpanAtCorrectAbsoluteColumn(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	sink := &cursorWriterProbeTTY{}
	writer := newOutputCursorWriter(sink)

	if err := writer.WriteFrame("abcdef", "<CURSOR>"); err != nil {
		t.Fatalf("write initial frame: %v", err)
	}
	sink.mu.Lock()
	sink.writes = nil
	sink.mu.Unlock()

	if err := writer.WriteFrame("abZdef", "<CURSOR>"); err != nil {
		t.Fatalf("write diff frame: %v", err)
	}

	sink.mu.Lock()
	got := strings.Join(sink.writes, "")
	sink.mu.Unlock()

	if !strings.Contains(got, "\x1b[1;3H") {
		t.Fatalf("expected span diff to target absolute column 3, got %q", got)
	}
	if strings.Contains(got, "abZdef") {
		t.Fatalf("expected span diff not to rewrite the full row, got %q", got)
	}
	if !strings.Contains(got, "Z") {
		t.Fatalf("expected span diff payload to contain changed grapheme, got %q", got)
	}
}

func TestOutputCursorWriterFallsBackToFullRowForStyledDiff(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	sink := &cursorWriterProbeTTY{}
	writer := newOutputCursorWriter(sink)

	frame1 := xansi.CHA(1) + "ABCD\x1b[0m\x1b[K"
	frame2 := xansi.CHA(1) + "A\x1b[0;31mB\x1b[0mCD\x1b[0m\x1b[K"
	if err := writer.WriteFrame(frame1, "<CURSOR>"); err != nil {
		t.Fatalf("write initial styled frame: %v", err)
	}
	sink.mu.Lock()
	sink.writes = nil
	sink.mu.Unlock()

	if err := writer.WriteFrame(frame2, "<CURSOR>"); err != nil {
		t.Fatalf("write styled diff frame: %v", err)
	}

	sink.mu.Lock()
	got := strings.Join(sink.writes, "")
	sink.mu.Unlock()

	if !strings.Contains(got, "\x1b[1;1H") {
		t.Fatalf("expected styled diff fallback to target absolute column 1, got %q", got)
	}
	if !strings.Contains(got, frame2) {
		t.Fatalf("expected styled diff fallback to rewrite the full styled row, got %q", got)
	}
}

func TestOutputCursorWriterFallsBackToFullRowForUnsafeEmojiRow(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	sink := &cursorWriterProbeTTY{}
	writer := newOutputCursorWriter(sink)

	frame1 := xansi.CHA(1) + "AAA❄️" + xansi.ECH(1) + xansi.CHA(6) + "BB" + "\x1b[0m\x1b[K"
	frame2 := xansi.CHA(1) + "AAA❄️" + xansi.ECH(1) + xansi.CHA(6) + "BC" + "\x1b[0m\x1b[K"
	if err := writer.WriteFrame(frame1, "<CURSOR>"); err != nil {
		t.Fatalf("write initial unsafe frame: %v", err)
	}
	sink.mu.Lock()
	sink.writes = nil
	sink.mu.Unlock()

	if err := writer.WriteFrame(frame2, "<CURSOR>"); err != nil {
		t.Fatalf("write unsafe diff frame: %v", err)
	}

	sink.mu.Lock()
	got := strings.Join(sink.writes, "")
	sink.mu.Unlock()

	if !strings.Contains(got, "\x1b[1;1H") {
		t.Fatalf("expected unsafe row fallback to target absolute column 1, got %q", got)
	}
	if !strings.Contains(got, frame2) {
		t.Fatalf("expected unsafe row fallback to rewrite the full row, got %q", got)
	}
}

func TestOutputCursorWriterDiffsDisjointSpansAsSeparateAbsolutePatches(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	sink := &cursorWriterProbeTTY{}
	writer := newOutputCursorWriter(sink)

	if err := writer.WriteFrame("abc123xyz", "<CURSOR>"); err != nil {
		t.Fatalf("write initial frame: %v", err)
	}
	sink.mu.Lock()
	sink.writes = nil
	sink.mu.Unlock()

	if err := writer.WriteFrame("abZ123xYz", "<CURSOR>"); err != nil {
		t.Fatalf("write diff frame: %v", err)
	}

	sink.mu.Lock()
	got := strings.Join(sink.writes, "")
	sink.mu.Unlock()

	if !strings.Contains(got, "\x1b[1;3H") {
		t.Fatalf("expected first patch to target absolute column 3, got %q", got)
	}
	if !strings.Contains(got, "\x1b[1;8H") {
		t.Fatalf("expected second patch to target absolute column 8, got %q", got)
	}
	if strings.Contains(got, "Z123xYz") {
		t.Fatalf("expected disjoint diff not to rewrite the unchanged middle suffix, got %q", got)
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

func TestOutputCursorWriterCopyModeRoundTripRepaintsScrollbackViewBackToLive(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	model := setupModel(t, modelOpts{width: 50, height: 14})
	seedCopyModeSnapshot(t, model, []string{"hist-0", "hist-1", "hist-2"}, []string{"live-0", "live-1", "live-2"})

	tab := model.workbench.CurrentTab()
	if tab == nil {
		t.Fatal("expected current tab")
	}
	tab.ScrollOffset = 1
	model.render.Invalidate()
	frame1 := model.View()

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	frame2 := model.View()

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCancelMode})
	frame3 := model.View()

	replay := func(frames []string) []string {
		sink := &cursorWriterProbeTTY{}
		writer := newOutputCursorWriter(sink)
		for _, frame := range frames {
			if err := writer.WriteFrame(frame, ""); err != nil {
				t.Fatalf("write frame: %v", err)
			}
		}
		sink.mu.Lock()
		stream := strings.Join(sink.writes, "")
		sink.mu.Unlock()
		vt := localvterm.New(50, 14, 0, nil)
		if _, err := vt.Write([]byte(stream)); err != nil {
			t.Fatalf("replay stream into host vterm: %v", err)
		}
		return vtermScreenLines(vt.ScreenContent())
	}

	got := replay([]string{frame1, frame2, frame3})
	want := replay([]string{frame3})
	for row := range want {
		if strings.TrimRight(got[row], " ") != strings.TrimRight(want[row], " ") {
			t.Fatalf("copy-mode round trip left stale host content on row %d\ngot=%#v\nwant=%#v", row, got, want)
		}
	}
}

func TestOutputCursorWriterRoundTripMovingStyledPaneMatchesFinalFrame(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	replay := func(frames []string) []string {
		sink := &cursorWriterProbeTTY{}
		writer := newOutputCursorWriter(sink)
		for _, frame := range frames {
			if err := writer.WriteFrame(frame, ""); err != nil {
				t.Fatalf("write frame: %v", err)
			}
		}
		sink.mu.Lock()
		stream := strings.Join(sink.writes, "")
		sink.mu.Unlock()
		vt := localvterm.New(120, 36, 0, nil)
		if _, err := vt.Write([]byte(stream)); err != nil {
			t.Fatalf("replay stream into host vterm: %v", err)
		}
		return vtermScreenLines(vt.ScreenContent())
	}

	frames := []string{
		benchmarkCursorWriterFrame(120, 36, 18, 7, 54, 16, true),
		benchmarkCursorWriterFrame(120, 36, 19, 7, 54, 16, true),
		benchmarkCursorWriterFrame(120, 36, 20, 7, 54, 16, true),
		benchmarkCursorWriterFrame(120, 36, 21, 7, 54, 16, true),
	}
	got := replay(frames)
	want := replay(frames[len(frames)-1:])
	for row := range want {
		if strings.TrimRight(got[row], " ") != strings.TrimRight(want[row], " ") {
			t.Fatalf("moving styled pane round trip diverged on row %d\ngot=%#v\nwant=%#v", row, got, want)
		}
	}
}

func TestOutputCursorWriterRoundTripMovingRealFloatingPaneMatchesFinalFrame(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	model := setupModel(t, modelOpts{width: 120, height: 36})
	base := model.runtime.Registry().GetOrCreate("term-1")
	base.Snapshot = cursorWriterStyledSnapshot("term-1", 118, 30)

	tab := model.workbench.CurrentTab()
	if tab == nil {
		t.Fatal("expected current tab")
	}
	if err := model.workbench.CreateFloatingPane(tab.ID, "float-1", workbench.Rect{X: 18, Y: 7, W: 54, H: 16}); err != nil {
		t.Fatalf("create floating pane: %v", err)
	}
	if err := model.workbench.BindPaneTerminal(tab.ID, "float-1", "term-float"); err != nil {
		t.Fatalf("bind floating pane terminal: %v", err)
	}
	floatTerminal := model.runtime.Registry().GetOrCreate("term-float")
	floatTerminal.Name = "float"
	floatTerminal.State = "running"
	floatTerminal.Snapshot = cursorWriterStyledSnapshot("term-float", 51, 14)
	model.runtime.BindPane("float-1").Connected = true

	replay := func(frames []string) []string {
		sink := &cursorWriterProbeTTY{}
		writer := newOutputCursorWriter(sink)
		for _, frame := range frames {
			if err := writer.WriteFrame(frame, ""); err != nil {
				t.Fatalf("write frame: %v", err)
			}
		}
		sink.mu.Lock()
		stream := strings.Join(sink.writes, "")
		sink.mu.Unlock()
		vt := localvterm.New(120, 36, 0, nil)
		if _, err := vt.Write([]byte(stream)); err != nil {
			t.Fatalf("replay stream into host vterm: %v", err)
		}
		return vtermScreenLines(vt.ScreenContent())
	}

	frames := []string{model.View()}
	positions := []workbench.Rect{
		{X: 19, Y: 7, W: 54, H: 16},
		{X: 20, Y: 7, W: 54, H: 16},
		{X: 21, Y: 7, W: 54, H: 16},
		{X: 22, Y: 7, W: 54, H: 16},
		{X: 23, Y: 7, W: 54, H: 16},
		{X: 24, Y: 7, W: 54, H: 16},
		{X: 23, Y: 7, W: 54, H: 16},
		{X: 22, Y: 7, W: 54, H: 16},
		{X: 21, Y: 7, W: 54, H: 16},
	}
	for _, rect := range positions {
		if !model.workbench.MoveFloatingPane(tab.ID, "float-1", rect.X, rect.Y) {
			t.Fatalf("expected move to %v to change pane", rect)
		}
		model.render.Invalidate()
		frames = append(frames, model.View())
	}

	got := replay(frames)
	want := replay(frames[len(frames)-1:])
	for row := range want {
		if strings.TrimRight(got[row], " ") != strings.TrimRight(want[row], " ") {
			t.Fatalf("real floating move round trip diverged on row %d\ngot=%#v\nwant=%#v", row, got, want)
		}
	}
}

func TestOutputCursorWriterRoundTripMovingOverlappingFloatingPanesMatchesFinalFrame(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	model := setupModel(t, modelOpts{width: 120, height: 36})
	base := model.runtime.Registry().GetOrCreate("term-1")
	base.Snapshot = cursorWriterStyledSnapshot("term-1", 118, 30)

	tab := model.workbench.CurrentTab()
	if tab == nil {
		t.Fatal("expected current tab")
	}
	mustBindFloat := func(paneID, terminalID string, rect workbench.Rect, cols, rows int) {
		t.Helper()
		if err := model.workbench.CreateFloatingPane(tab.ID, paneID, rect); err != nil {
			t.Fatalf("create floating pane %s: %v", paneID, err)
		}
		if err := model.workbench.BindPaneTerminal(tab.ID, paneID, terminalID); err != nil {
			t.Fatalf("bind floating pane %s terminal: %v", paneID, err)
		}
		terminal := model.runtime.Registry().GetOrCreate(terminalID)
		terminal.Name = terminalID
		terminal.State = "running"
		terminal.Snapshot = cursorWriterStyledSnapshot(terminalID, cols, rows)
		model.runtime.BindPane(paneID).Connected = true
	}
	mustBindFloat("float-1", "term-float-1", workbench.Rect{X: 18, Y: 7, W: 54, H: 16}, 51, 14)
	mustBindFloat("float-2", "term-float-2", workbench.Rect{X: 56, Y: 9, W: 44, H: 14}, 41, 12)

	replay := func(frames []string) []string {
		sink := &cursorWriterProbeTTY{}
		writer := newOutputCursorWriter(sink)
		for _, frame := range frames {
			if err := writer.WriteFrame(frame, ""); err != nil {
				t.Fatalf("write frame: %v", err)
			}
		}
		sink.mu.Lock()
		stream := strings.Join(sink.writes, "")
		sink.mu.Unlock()
		vt := localvterm.New(120, 36, 0, nil)
		if _, err := vt.Write([]byte(stream)); err != nil {
			t.Fatalf("replay stream into host vterm: %v", err)
		}
		return vtermScreenLines(vt.ScreenContent())
	}

	frames := []string{model.View()}
	positions := []workbench.Rect{
		{X: 22, Y: 7, W: 54, H: 16},
		{X: 26, Y: 7, W: 54, H: 16},
		{X: 30, Y: 7, W: 54, H: 16},
		{X: 34, Y: 7, W: 54, H: 16},
		{X: 38, Y: 7, W: 54, H: 16},
		{X: 34, Y: 7, W: 54, H: 16},
		{X: 30, Y: 7, W: 54, H: 16},
		{X: 26, Y: 7, W: 54, H: 16},
	}
	for _, rect := range positions {
		if !model.workbench.MoveFloatingPane(tab.ID, "float-1", rect.X, rect.Y) {
			t.Fatalf("expected move to %v to change pane", rect)
		}
		model.render.Invalidate()
		frames = append(frames, model.View())
	}

	got := replay(frames)
	want := replay(frames[len(frames)-1:])
	for row := range want {
		if strings.TrimRight(got[row], " ") != strings.TrimRight(want[row], " ") {
			t.Fatalf("overlapping floating move round trip diverged on row %d\ngot=%#v\nwant=%#v", row, got, want)
		}
	}
}

func cursorWriterStyledSnapshot(terminalID string, cols, rows int) *protocol.Snapshot {
	if cols <= 0 {
		cols = 1
	}
	if rows <= 0 {
		rows = 1
	}
	palette := []protocol.CellStyle{
		{FG: "#f8fafc", BG: "#0f172a", Bold: true},
		{FG: "#fde68a", BG: "#111827"},
		{FG: "#93c5fd", BG: "#0b1220"},
		{FG: "#86efac", BG: "#111827", Underline: true},
	}
	screen := make([][]protocol.Cell, 0, rows)
	for y := 0; y < rows; y++ {
		row := make([]protocol.Cell, 0, cols)
		for x := 0; x < cols; x++ {
			style := palette[(x+y)%len(palette)]
			ch := 'a' + rune((x+y)%26)
			if x%9 == 0 {
				ch = ' '
			}
			row = append(row, protocol.Cell{
				Content: string(ch),
				Width:   1,
				Style:   style,
			})
		}
		screen = append(screen, row)
	}
	return &protocol.Snapshot{
		TerminalID: terminalID,
		Size:       protocol.Size{Cols: uint16(cols), Rows: uint16(rows)},
		Screen:     protocol.ScreenData{Cells: screen},
		Cursor:     protocol.CursorState{Row: 0, Col: 0, Visible: true},
		Modes:      protocol.TerminalModes{AutoWrap: true},
	}
}
