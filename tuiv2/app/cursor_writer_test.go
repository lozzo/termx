package app

import (
	"fmt"
	"image/color"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/shared"
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

func TestOutputCursorWriterDiffOnCompositorOwnedRowSkipsExtraEraseLineRight(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	sink := &cursorWriterProbeTTY{}
	writer := newOutputCursorWriter(sink)

	frame1 := "\x1b[1;37;100mabc   \x1b[0m\x1b[K"
	frame2 := "\x1b[1;37;100mabZ   \x1b[0m\x1b[K"
	if err := writer.WriteFrame(frame1, "<CURSOR>"); err != nil {
		t.Fatalf("write initial frame: %v", err)
	}
	sink.mu.Lock()
	sink.writes = nil
	sink.mu.Unlock()

	if err := writer.WriteFrame(frame2, "<CURSOR>"); err != nil {
		t.Fatalf("write diff frame: %v", err)
	}

	sink.mu.Lock()
	got := strings.Join(sink.writes, "")
	sink.mu.Unlock()

	if strings.Contains(got, xansi.EraseLineRight) {
		t.Fatalf("expected compositor-owned row diff to avoid extra erase-line-right, got %q", got)
	}
	if !strings.Contains(got, "Z") {
		t.Fatalf("expected diff payload to contain changed grapheme, got %q", got)
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

func TestOutputCursorWriterDisableVerticalScrollEnvSkipsScrollRegionOptimization(t *testing.T) {
	t.Setenv("TERMX_DISABLE_VERTICAL_SCROLL", "1")

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

	if err := writer.WriteFrame(frame1, ""); err != nil {
		t.Fatalf("write initial frame: %v", err)
	}
	sink.mu.Lock()
	sink.writes = nil
	sink.mu.Unlock()

	if err := writer.WriteFrame(frame2, ""); err != nil {
		t.Fatalf("write scrolled frame: %v", err)
	}

	sink.mu.Lock()
	got := strings.Join(sink.writes, "")
	sink.mu.Unlock()

	if strings.Contains(got, "\x1b[4;9r") || strings.Contains(got, "\x1b[1S") || strings.Contains(got, "\x1b[1T") {
		t.Fatalf("expected vertical scroll optimization disabled, got %q", got)
	}
}

func TestOutputCursorWriterDebugFaultScrollDropRemainderEveryInjectsStaleScrollFrame(t *testing.T) {
	t.Setenv("TERMX_DEBUG_FAULT_SCROLL_DROP_REMAINDER_EVERY", "1")

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

	if err := writer.WriteFrame(frame1, ""); err != nil {
		t.Fatalf("write initial frame: %v", err)
	}
	sink.mu.Lock()
	sink.writes = nil
	sink.mu.Unlock()

	if err := writer.WriteFrame(frame2, ""); err != nil {
		t.Fatalf("write scrolled frame: %v", err)
	}

	sink.mu.Lock()
	got := strings.Join(sink.writes, "")
	sink.mu.Unlock()

	if !strings.Contains(got, "\x1b[1S") {
		t.Fatalf("expected scroll optimization to remain active under fault injection, got %q", got)
	}
	if strings.Contains(got, "row-07") {
		t.Fatalf("expected injected fault to drop scroll remainder repaint, got %q", got)
	}
}

func TestDetectVerticalScrollPlanFindsSingleRowScrollUpRegion(t *testing.T) {
	previous := []string{
		"hdr-1", "hdr-2", "hdr-3",
		"row-01", "row-02", "row-03", "row-04", "row-05", "row-06",
		"ftr-1",
	}
	next := []string{
		"hdr-1", "hdr-2", "hdr-3",
		"row-02", "row-03", "row-04", "row-05", "row-06", "row-07",
		"ftr-1",
	}

	plan, ok := detectVerticalScrollPlan(previous, next)
	if !ok {
		t.Fatal("expected vertical scroll plan")
	}
	if plan.direction != scrollUp {
		t.Fatalf("expected scrollUp plan, got %#v", plan)
	}
	if plan.shift != 1 || plan.start != 3 || plan.end != 8 {
		t.Fatalf("unexpected vertical scroll bounds %#v", plan)
	}
	if plan.reused != 5 {
		t.Fatalf("expected five reused rows in scroll plan, got %#v", plan)
	}
}

func TestDetectVerticalScrollPlanFindsSingleRowScrollDownRegion(t *testing.T) {
	previous := []string{
		"hdr-1", "hdr-2", "hdr-3",
		"row-02", "row-03", "row-04", "row-05", "row-06", "row-07",
		"ftr-1",
	}
	next := []string{
		"hdr-1", "hdr-2", "hdr-3",
		"row-01", "row-02", "row-03", "row-04", "row-05", "row-06",
		"ftr-1",
	}

	plan, ok := detectVerticalScrollPlan(previous, next)
	if !ok {
		t.Fatal("expected vertical scroll plan")
	}
	if plan.direction != scrollDown {
		t.Fatalf("expected scrollDown plan, got %#v", plan)
	}
	if plan.shift != 1 || plan.start != 3 || plan.end != 8 {
		t.Fatalf("unexpected vertical scroll bounds %#v", plan)
	}
	if plan.reused != 5 {
		t.Fatalf("expected five reused rows in scroll plan, got %#v", plan)
	}
}

func TestBetterVerticalScrollPlanPrefersLargerReuseThenSmallerShiftThenEarlierStart(t *testing.T) {
	current := verticalScrollPlan{direction: scrollDown, start: 4, end: 9, shift: 2, reused: 4}
	if !betterVerticalScrollPlan(verticalScrollPlan{direction: scrollUp, start: 3, end: 9, shift: 2, reused: 5}, current) {
		t.Fatal("expected larger reused run to win")
	}
	if !betterVerticalScrollPlan(verticalScrollPlan{direction: scrollUp, start: 3, end: 8, shift: 1, reused: 4}, current) {
		t.Fatal("expected smaller shift to win when reused count ties")
	}
	if !betterVerticalScrollPlan(verticalScrollPlan{direction: scrollUp, start: 2, end: 8, shift: 2, reused: 4}, current) {
		t.Fatal("expected earlier start to win when reused and shift tie")
	}
	if !betterVerticalScrollPlan(verticalScrollPlan{direction: scrollUp, start: 4, end: 9, shift: 2, reused: 4}, current) {
		t.Fatal("expected scrollUp to sort before scrollDown when all else ties")
	}
}

func TestApplyVerticalScrollPlanClearsInsertedGap(t *testing.T) {
	lines := []string{"hdr", "a", "b", "c", "d", "ftr"}
	plan := verticalScrollPlan{
		direction: scrollDown,
		start:     1,
		end:       4,
		shift:     1,
		reused:    3,
	}

	got := applyVerticalScrollPlan(lines, plan)
	want := []string{"hdr", "", "a", "b", "c", "ftr"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("unexpected scrolled lines %v want %v", got, want)
	}
}

func TestRenderVerticalScrollPlanBuildsExpectedCSISequences(t *testing.T) {
	tests := []struct {
		name string
		plan verticalScrollPlan
		want string
	}{
		{
			name: "scroll up",
			plan: verticalScrollPlan{direction: scrollUp, start: 3, end: 8, shift: 1},
			want: "\x1b[4;9r" + xansi.DECRST(xansi.ModeOrigin) + "\x1b[4;1H" + "\x1b[1S" + "\x1b[r" + xansi.DECRST(xansi.ModeOrigin),
		},
		{
			name: "scroll down",
			plan: verticalScrollPlan{direction: scrollDown, start: 2, end: 7, shift: 2},
			want: "\x1b[3;8r" + xansi.DECRST(xansi.ModeOrigin) + "\x1b[3;1H" + "\x1b[2T" + "\x1b[r" + xansi.DECRST(xansi.ModeOrigin),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := renderVerticalScrollPlan(tt.plan, 10); got != tt.want {
				t.Fatalf("unexpected vertical scroll CSI %q want %q", got, tt.want)
			}
		})
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

func TestBubbleTeaRestoreSequenceExtractsTrailingCRAndCSI(t *testing.T) {
	payload := []byte("body\x1b[12;8H\r\x1b[?25h")
	if got, want := bubbleTeaRestoreSequence(payload), "\x1b[12;8H\r\x1b[?25h"; got != want {
		t.Fatalf("expected trailing cursor restore suffix, got %q want %q", got, want)
	}
}

func TestStripEmbeddedCursorSequenceKeepsTrailingRestoreCursorSuffix(t *testing.T) {
	cursor := "\x1b[12;8H"
	payload := "body" + cursor + "\r\x1b[?25h"
	if got := stripEmbeddedCursorSequence(payload, cursor); got != payload {
		t.Fatalf("expected trailing restore suffix to remain untouched, got %q want %q", got, payload)
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

func TestOutputCursorWriterFrameLinesPathMovingFloatingPanePreservesUnderlyingStyles(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	buildModel := func(rect workbench.Rect) *Model {
		t.Helper()
		model := setupModel(t, modelOpts{width: 120, height: 36})
		base := model.runtime.Registry().GetOrCreate("term-1")
		base.Snapshot = cursorWriterNvimLikeSnapshot("term-1", 118, 30, "#444444")

		tab := model.workbench.CurrentTab()
		if tab == nil {
			t.Fatal("expected current tab")
		}
		if err := model.workbench.CreateFloatingPane(tab.ID, "float-1", rect); err != nil {
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
		return model
	}

	captureScreen := func(model *Model, positions []workbench.Rect) localvterm.ScreenData {
		t.Helper()
		sink := &cursorWriterProbeTTY{}
		writer := newOutputCursorWriter(sink)
		model.SetFrameWriter(writer)
		model.SetCursorWriter(writer)

		model.render.Invalidate()
		_ = model.View()

		tab := model.workbench.CurrentTab()
		if tab == nil {
			t.Fatal("expected current tab")
		}
		for _, rect := range positions {
			if !model.workbench.MoveFloatingPane(tab.ID, "float-1", rect.X, rect.Y) {
				t.Fatalf("expected move to %v to change pane", rect)
			}
			model.render.Invalidate()
			_ = model.View()
		}

		sink.mu.Lock()
		stream := strings.Join(sink.writes, "")
		sink.mu.Unlock()
		vt := localvterm.New(120, 36, 0, nil)
		if _, err := vt.Write([]byte(stream)); err != nil {
			t.Fatalf("replay stream into host vterm: %v", err)
		}
		return vt.ScreenContent()
	}

	start := workbench.Rect{X: 18, Y: 7, W: 54, H: 16}
	path := []workbench.Rect{
		{X: 19, Y: 7, W: 54, H: 16},
		{X: 21, Y: 7, W: 54, H: 16},
		{X: 23, Y: 7, W: 54, H: 16},
		{X: 25, Y: 7, W: 54, H: 16},
	}

	got := captureScreen(buildModel(start), path)
	want := captureScreen(buildModel(path[len(path)-1]), nil)
	assertScreenEqual(t, got, want)
}

func TestOutputCursorWriterFrameLinesPathMovingOverlappingFloatingPanesPreservesStyles(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	buildModel := func() *Model {
		t.Helper()
		model := setupModel(t, modelOpts{width: 120, height: 36})
		base := model.runtime.Registry().GetOrCreate("term-1")
		base.Snapshot = cursorWriterNvimLikeSnapshot("term-1", 118, 30, "#444444")

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
		return model
	}

	captureScreen := func(model *Model, positions []workbench.Rect) localvterm.ScreenData {
		t.Helper()
		sink := &cursorWriterProbeTTY{}
		writer := newOutputCursorWriter(sink)
		model.SetFrameWriter(writer)
		model.SetCursorWriter(writer)

		model.render.Invalidate()
		_ = model.View()

		tab := model.workbench.CurrentTab()
		if tab == nil {
			t.Fatal("expected current tab")
		}
		for _, rect := range positions {
			if !model.workbench.MoveFloatingPane(tab.ID, "float-1", rect.X, rect.Y) {
				t.Fatalf("expected move to %v to change pane", rect)
			}
			model.render.Invalidate()
			_ = model.View()
		}

		sink.mu.Lock()
		stream := strings.Join(sink.writes, "")
		sink.mu.Unlock()
		vt := localvterm.New(120, 36, 0, nil)
		if _, err := vt.Write([]byte(stream)); err != nil {
			t.Fatalf("replay stream into host vterm: %v", err)
		}
		return vt.ScreenContent()
	}

	path := []workbench.Rect{
		{X: 22, Y: 7, W: 54, H: 16},
		{X: 26, Y: 7, W: 54, H: 16},
		{X: 30, Y: 7, W: 54, H: 16},
		{X: 34, Y: 7, W: 54, H: 16},
		{X: 38, Y: 7, W: 54, H: 16},
		{X: 34, Y: 7, W: 54, H: 16},
		{X: 30, Y: 7, W: 54, H: 16},
		{X: 26, Y: 7, W: 54, H: 16},
	}

	got := captureScreen(buildModel(), path)
	want := captureScreen(buildModel(), []workbench.Rect{path[len(path)-1]})
	assertScreenEqual(t, got, want)
}

func TestOutputCursorWriterFrameLinesPathCodexInputUpdatesPreserveStyledInputBox(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	buildModel := func(input string) *Model {
		t.Helper()
		model := setupModel(t, modelOpts{width: 120, height: 36})
		base := model.runtime.Registry().GetOrCreate("term-1")
		base.Snapshot = cursorWriterCodexInputSnapshot("term-1", 118, 30, input)
		return model
	}

	captureScreen := func(model *Model, inputs []string) localvterm.ScreenData {
		t.Helper()
		sink := &cursorWriterProbeTTY{}
		writer := newOutputCursorWriter(sink)
		model.SetFrameWriter(writer)
		model.SetCursorWriter(writer)

		model.render.Invalidate()
		_ = model.View()

		terminal := model.runtime.Registry().Get("term-1")
		if terminal == nil {
			t.Fatal("expected terminal")
		}
		for i, inputText := range inputs {
			terminal.Snapshot = cursorWriterCodexInputSnapshot("term-1", 118, 30, inputText)
			touchRuntimeVisibleStateForTest(model.runtime, uint8(i+1))
			model.render.Invalidate()
			_ = model.View()
		}

		sink.mu.Lock()
		stream := strings.Join(sink.writes, "")
		sink.mu.Unlock()
		vt := localvterm.New(120, 36, 0, nil)
		if _, err := vt.Write([]byte(stream)); err != nil {
			t.Fatalf("replay stream into host vterm: %v", err)
		}
		return vt.ScreenContent()
	}

	sequence := []string{"h", "he", "hel", "hello", "hello world", "hello world!"}
	got := captureScreen(buildModel(""), sequence)
	want := captureScreen(buildModel(sequence[len(sequence)-1]), nil)
	assertScreenEqual(t, got, want)
}

func TestOutputCursorWriterFrameLinesPathLiveCodexInputUpdatesPreserveStyledInputBox(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	buildModel := func(input string) *Model {
		t.Helper()
		model := setupModel(t, modelOpts{width: 120, height: 36})
		terminal := model.runtime.Registry().GetOrCreate("term-1")
		vt := localvterm.New(118, 30, 100, nil)
		vt.LoadSnapshot(
			vtermCodexInputScreen(118, 30, input),
			localvterm.CursorState{Row: 27, Col: minInt(117, 8+len(input)), Visible: true},
			localvterm.TerminalModes{AutoWrap: true},
		)
		terminal.VTerm = vt
		terminal.SurfaceVersion = 1
		terminal.Snapshot = cursorWriterCodexInputSnapshot("term-1", 118, 30, input)
		return model
	}

	captureScreen := func(model *Model, inputs []string) localvterm.ScreenData {
		t.Helper()
		sink := &cursorWriterProbeTTY{}
		writer := newOutputCursorWriter(sink)
		model.SetFrameWriter(writer)
		model.SetCursorWriter(writer)

		model.render.Invalidate()
		_ = model.View()

		terminal := model.runtime.Registry().Get("term-1")
		if terminal == nil || terminal.VTerm == nil {
			t.Fatal("expected live terminal surface")
		}
		for i, inputText := range inputs {
			terminal.VTerm.LoadSnapshot(
				vtermCodexInputScreen(118, 30, inputText),
				localvterm.CursorState{Row: 27, Col: minInt(117, 8+len(inputText)), Visible: true},
				localvterm.TerminalModes{AutoWrap: true},
			)
			terminal.SurfaceVersion++
			model.runtime.RefreshSnapshotFromVTerm("term-1")
			touchRuntimeVisibleStateForTest(model.runtime, uint8(i+1))
			model.render.Invalidate()
			_ = model.View()
		}

		sink.mu.Lock()
		stream := strings.Join(sink.writes, "")
		sink.mu.Unlock()
		vt := localvterm.New(120, 36, 0, nil)
		if _, err := vt.Write([]byte(stream)); err != nil {
			t.Fatalf("replay stream into host vterm: %v", err)
		}
		return vt.ScreenContent()
	}

	sequence := []string{"h", "he", "hel", "hello", "hello world", "hello world!"}
	got := captureScreen(buildModel(""), sequence)
	want := captureScreen(buildModel(sequence[len(sequence)-1]), nil)
	assertScreenEqual(t, got, want)
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

func TestOutputCursorWriterRoundTripFocusSwitchFromTextPanePreservesNvimBackground(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-2",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "nvim", TerminalID: "term-nvim"},
				"pane-2": {ID: "pane-2", Title: "codex", TerminalID: "term-codex"},
			},
			Root: &workbench.LayoutNode{
				Direction: workbench.SplitVertical,
				Ratio:     0.5,
				First:     workbench.NewLeaf("pane-1"),
				Second:    workbench.NewLeaf("pane-2"),
			},
		}},
	})

	rt := runtime.New(&recordingBridgeClient{
		attachResult:       &protocol.AttachResult{Channel: 1, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{},
	})
	nvimTerm := rt.Registry().GetOrCreate("term-nvim")
	nvimTerm.Name = "nvim"
	nvimTerm.State = "running"
	nvimTerm.Channel = 1
	nvimTerm.Snapshot = cursorWriterNvimLikeSnapshot("term-nvim", 58, 30, "#444444")
	nvimBinding := rt.BindPane("pane-1")
	nvimBinding.Channel = 1
	nvimBinding.Connected = true

	codexTerm := rt.Registry().GetOrCreate("term-codex")
	codexTerm.Name = "codex"
	codexTerm.State = "running"
	codexTerm.Channel = 2
	codexTerm.Snapshot = cursorWriterDenseTextSnapshot("term-codex", 58, 30, 91)
	codexBinding := rt.BindPane("pane-2")
	codexBinding.Channel = 2
	codexBinding.Connected = true

	model := New(shared.Config{}, wb, rt)
	model.width = 120
	model.height = 36

	frames := []string{model.View()}
	if err := model.workbench.FocusPane("tab-1", "pane-1"); err != nil {
		t.Fatalf("FocusPane: %v", err)
	}
	model.render.Invalidate()
	frames = append(frames, model.View())

	got := replayCursorWriterScreen(t, 120, 36, frames)
	want := replayCursorWriterScreen(t, 120, 36, frames[len(frames)-1:])
	assertScreenEqual(t, got, want)
}

func TestOutputCursorWriterRoundTripFocusSwitchFromBottomTextPanePreservesTopNvimBackground(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-2",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "nvim", TerminalID: "term-nvim"},
				"pane-2": {ID: "pane-2", Title: "codex", TerminalID: "term-codex"},
			},
			Root: &workbench.LayoutNode{
				Direction: workbench.SplitHorizontal,
				Ratio:     0.5,
				First:     workbench.NewLeaf("pane-1"),
				Second:    workbench.NewLeaf("pane-2"),
			},
		}},
	})

	rt := runtime.New(&recordingBridgeClient{
		attachResult:       &protocol.AttachResult{Channel: 1, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{},
	})
	nvimTerm := rt.Registry().GetOrCreate("term-nvim")
	nvimTerm.Name = "nvim"
	nvimTerm.State = "running"
	nvimTerm.Channel = 1
	nvimTerm.Snapshot = cursorWriterNvimLikeSnapshot("term-nvim", 118, 14, "#444444")
	nvimBinding := rt.BindPane("pane-1")
	nvimBinding.Channel = 1
	nvimBinding.Connected = true

	codexTerm := rt.Registry().GetOrCreate("term-codex")
	codexTerm.Name = "codex"
	codexTerm.State = "running"
	codexTerm.Channel = 2
	codexTerm.Snapshot = cursorWriterDenseTextSnapshot("term-codex", 118, 14, 107)
	codexBinding := rt.BindPane("pane-2")
	codexBinding.Channel = 2
	codexBinding.Connected = true

	model := New(shared.Config{}, wb, rt)
	model.width = 120
	model.height = 36

	frames := []string{model.View()}
	if err := model.workbench.FocusPane("tab-1", "pane-1"); err != nil {
		t.Fatalf("FocusPane: %v", err)
	}
	model.render.Invalidate()
	frames = append(frames, model.View())

	got := replayCursorWriterScreen(t, 120, 36, frames)
	want := replayCursorWriterScreen(t, 120, 36, frames[len(frames)-1:])
	assertScreenEqual(t, got, want)
}

func TestOutputCursorWriterFrameLinesPathFocusSwitchFromBottomTextPanePreservesTopNvimBackground(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	buildModel := func(activePaneID string) *Model {
		t.Helper()
		wb := workbench.NewWorkbench()
		wb.AddWorkspace("main", &workbench.WorkspaceState{
			Name:      "main",
			ActiveTab: 0,
			Tabs: []*workbench.TabState{{
				ID:           "tab-1",
				Name:         "tab 1",
				ActivePaneID: activePaneID,
				Panes: map[string]*workbench.PaneState{
					"pane-1": {ID: "pane-1", Title: "nvim", TerminalID: "term-nvim"},
					"pane-2": {ID: "pane-2", Title: "codex", TerminalID: "term-codex"},
				},
				Root: &workbench.LayoutNode{
					Direction: workbench.SplitHorizontal,
					Ratio:     0.5,
					First:     workbench.NewLeaf("pane-1"),
					Second:    workbench.NewLeaf("pane-2"),
				},
			}},
		})

		rt := runtime.New(&recordingBridgeClient{
			attachResult:       &protocol.AttachResult{Channel: 1, Mode: "collaborator"},
			snapshotByTerminal: map[string]*protocol.Snapshot{},
		})
		nvimTerm := rt.Registry().GetOrCreate("term-nvim")
		nvimTerm.Name = "nvim"
		nvimTerm.State = "running"
		nvimTerm.Channel = 1
		nvimTerm.Snapshot = cursorWriterNvimLikeSnapshot("term-nvim", 118, 14, "#444444")
		nvimBinding := rt.BindPane("pane-1")
		nvimBinding.Channel = 1
		nvimBinding.Connected = true

		codexTerm := rt.Registry().GetOrCreate("term-codex")
		codexTerm.Name = "codex"
		codexTerm.State = "running"
		codexTerm.Channel = 2
		codexTerm.Snapshot = cursorWriterDenseTextSnapshot("term-codex", 118, 14, 107)
		codexBinding := rt.BindPane("pane-2")
		codexBinding.Channel = 2
		codexBinding.Connected = true

		model := New(shared.Config{}, wb, rt)
		model.width = 120
		model.height = 36
		return model
	}

	captureScreen := func(model *Model, switchToPaneID string) localvterm.ScreenData {
		t.Helper()
		sink := &cursorWriterProbeTTY{}
		writer := newOutputCursorWriter(sink)
		model.SetFrameWriter(writer)
		model.SetCursorWriter(writer)

		model.render.Invalidate()
		_ = model.View()
		if switchToPaneID != "" {
			if err := model.workbench.FocusPane("tab-1", switchToPaneID); err != nil {
				t.Fatalf("FocusPane: %v", err)
			}
			model.render.Invalidate()
			_ = model.View()
		}

		sink.mu.Lock()
		stream := strings.Join(sink.writes, "")
		sink.mu.Unlock()
		vt := localvterm.New(120, 36, 0, nil)
		if _, err := vt.Write([]byte(stream)); err != nil {
			t.Fatalf("replay stream into host vterm: %v", err)
		}
		return vt.ScreenContent()
	}

	got := captureScreen(buildModel("pane-2"), "pane-1")
	want := captureScreen(buildModel("pane-1"), "")
	assertScreenEqual(t, got, want)
}

func TestOutputCursorWriterBatchedFocusSwitchAfterTextScrollPreservesTopNvimBackground(t *testing.T) {
	originalDelay := directFrameBatchDelay
	originalIdleThreshold := directFrameIdleThreshold
	directFrameBatchDelay = 20 * time.Millisecond
	directFrameIdleThreshold = time.Hour
	defer func() {
		directFrameBatchDelay = originalDelay
		directFrameIdleThreshold = originalIdleThreshold
	}()

	buildModel := func(activePaneID string, start int) *Model {
		t.Helper()
		wb := workbench.NewWorkbench()
		wb.AddWorkspace("main", &workbench.WorkspaceState{
			Name:      "main",
			ActiveTab: 0,
			Tabs: []*workbench.TabState{{
				ID:           "tab-1",
				Name:         "tab 1",
				ActivePaneID: activePaneID,
				Panes: map[string]*workbench.PaneState{
					"pane-1": {ID: "pane-1", Title: "nvim", TerminalID: "term-nvim"},
					"pane-2": {ID: "pane-2", Title: "codex", TerminalID: "term-codex"},
				},
				Root: &workbench.LayoutNode{
					Direction: workbench.SplitHorizontal,
					Ratio:     0.5,
					First:     workbench.NewLeaf("pane-1"),
					Second:    workbench.NewLeaf("pane-2"),
				},
			}},
		})

		rt := runtime.New(&recordingBridgeClient{
			attachResult:       &protocol.AttachResult{Channel: 1, Mode: "collaborator"},
			snapshotByTerminal: map[string]*protocol.Snapshot{},
		})
		nvimTerm := rt.Registry().GetOrCreate("term-nvim")
		nvimTerm.Name = "nvim"
		nvimTerm.State = "running"
		nvimTerm.Channel = 1
		nvimTerm.Snapshot = cursorWriterNvimLikeSnapshot("term-nvim", 118, 14, "#444444")
		nvimBinding := rt.BindPane("pane-1")
		nvimBinding.Channel = 1
		nvimBinding.Connected = true

		codexTerm := rt.Registry().GetOrCreate("term-codex")
		codexTerm.Name = "codex"
		codexTerm.State = "running"
		codexTerm.Channel = 2
		codexTerm.Snapshot = cursorWriterDenseTextSnapshot("term-codex", 118, 14, start)
		codexBinding := rt.BindPane("pane-2")
		codexBinding.Channel = 2
		codexBinding.Connected = true

		model := New(shared.Config{}, wb, rt)
		model.width = 120
		model.height = 36
		return model
	}

	sink := &cursorWriterProbeTTY{}
	writer := newOutputCursorWriter(sink)

	model := buildModel("pane-2", 1)
	model.SetFrameWriter(writer)
	model.SetCursorWriter(writer)
	model.render.Invalidate()
	_ = model.View()
	time.Sleep(40 * time.Millisecond)

	model.runtime.Registry().Get("term-codex").Snapshot = cursorWriterDenseTextSnapshot("term-codex", 118, 14, 107)
	touchRuntimeVisibleStateForTest(model.runtime, 1)
	model.render.Invalidate()
	_ = model.View()
	if err := model.workbench.FocusPane("tab-1", "pane-1"); err != nil {
		t.Fatalf("FocusPane: %v", err)
	}
	model.render.Invalidate()
	_ = model.View()
	time.Sleep(40 * time.Millisecond)

	sink.mu.Lock()
	stream := strings.Join(sink.writes, "")
	sink.mu.Unlock()
	vt := localvterm.New(120, 36, 0, nil)
	if _, err := vt.Write([]byte(stream)); err != nil {
		t.Fatalf("replay stream into host vterm: %v", err)
	}
	got := vt.ScreenContent()

	wantModel := buildModel("pane-1", 107)
	wantSink := &cursorWriterProbeTTY{}
	wantWriter := newOutputCursorWriter(wantSink)
	wantModel.SetFrameWriter(wantWriter)
	wantModel.SetCursorWriter(wantWriter)
	wantModel.render.Invalidate()
	_ = wantModel.View()
	time.Sleep(40 * time.Millisecond)
	wantSink.mu.Lock()
	wantStream := strings.Join(wantSink.writes, "")
	wantSink.mu.Unlock()
	wantVT := localvterm.New(120, 36, 0, nil)
	if _, err := wantVT.Write([]byte(wantStream)); err != nil {
		t.Fatalf("replay expected stream into host vterm: %v", err)
	}
	want := wantVT.ScreenContent()

	assertScreenEqual(t, got, want)
}

func TestOutputCursorWriterFrameLinesPathMovingFloatingPanePreservesUnderlyingNvimBackground(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	buildModel := func(rect workbench.Rect) *Model {
		t.Helper()
		model := setupModel(t, modelOpts{width: 120, height: 36})
		base := model.runtime.Registry().GetOrCreate("term-1")
		base.Snapshot = cursorWriterNvimLikeSnapshot("term-1", 118, 30, "#444444")

		tab := model.workbench.CurrentTab()
		if tab == nil {
			t.Fatal("expected current tab")
		}
		if err := model.workbench.CreateFloatingPane(tab.ID, "float-1", rect); err != nil {
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
		return model
	}

	captureScreen := func(model *Model, positions []workbench.Rect) localvterm.ScreenData {
		t.Helper()
		sink := &cursorWriterProbeTTY{}
		writer := newOutputCursorWriter(sink)
		model.SetFrameWriter(writer)
		model.SetCursorWriter(writer)

		model.render.Invalidate()
		_ = model.View()

		tab := model.workbench.CurrentTab()
		if tab == nil {
			t.Fatal("expected current tab")
		}
		for _, rect := range positions {
			if !model.workbench.MoveFloatingPane(tab.ID, "float-1", rect.X, rect.Y) {
				t.Fatalf("expected move to %v to change pane", rect)
			}
			model.render.Invalidate()
			_ = model.View()
		}

		sink.mu.Lock()
		stream := strings.Join(sink.writes, "")
		sink.mu.Unlock()
		vt := localvterm.New(120, 36, 0, nil)
		if _, err := vt.Write([]byte(stream)); err != nil {
			t.Fatalf("replay stream into host vterm: %v", err)
		}
		return vt.ScreenContent()
	}

	start := workbench.Rect{X: 18, Y: 7, W: 54, H: 16}
	path := []workbench.Rect{
		{X: 19, Y: 7, W: 54, H: 16},
		{X: 21, Y: 7, W: 54, H: 16},
		{X: 23, Y: 7, W: 54, H: 16},
		{X: 25, Y: 7, W: 54, H: 16},
	}

	got := captureScreen(buildModel(start), path)
	want := captureScreen(buildModel(path[len(path)-1]), nil)
	assertScreenEqual(t, got, want)
}

func TestOutputCursorWriterFrameLinesPathStreamingFloatingPanePreservesUnderlyingNvimBackground(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	buildModel := func(start int) *Model {
		t.Helper()
		model := setupModel(t, modelOpts{width: 120, height: 36})
		base := model.runtime.Registry().GetOrCreate("term-1")
		base.Snapshot = cursorWriterNvimLikeSnapshot("term-1", 118, 30, "#444444")

		tab := model.workbench.CurrentTab()
		if tab == nil {
			t.Fatal("expected current tab")
		}
		rect := workbench.Rect{X: 18, Y: 7, W: 54, H: 16}
		if err := model.workbench.CreateFloatingPane(tab.ID, "float-1", rect); err != nil {
			t.Fatalf("create floating pane: %v", err)
		}
		if err := model.workbench.BindPaneTerminal(tab.ID, "float-1", "term-float"); err != nil {
			t.Fatalf("bind floating pane terminal: %v", err)
		}
		floatTerminal := model.runtime.Registry().GetOrCreate("term-float")
		floatTerminal.Name = "float"
		floatTerminal.State = "running"
		floatTerminal.Snapshot = cursorWriterScrollingSnapshot("term-float", 51, 14, start)
		model.runtime.BindPane("float-1").Connected = true
		return model
	}

	captureScreen := func(model *Model, starts []int) localvterm.ScreenData {
		t.Helper()
		sink := &cursorWriterProbeTTY{}
		writer := newOutputCursorWriter(sink)
		model.SetFrameWriter(writer)
		model.SetCursorWriter(writer)

		model.render.Invalidate()
		_ = model.View()

		floatTerminal := model.runtime.Registry().Get("term-float")
		if floatTerminal == nil {
			t.Fatal("expected floating terminal")
		}
		for _, start := range starts {
			floatTerminal.Snapshot = cursorWriterScrollingSnapshot("term-float", 51, 14, start)
			touchRuntimeVisibleStateForTest(model.runtime, uint8(start))
			model.render.Invalidate()
			_ = model.View()
		}

		sink.mu.Lock()
		stream := strings.Join(sink.writes, "")
		sink.mu.Unlock()
		vt := localvterm.New(120, 36, 0, nil)
		if _, err := vt.Write([]byte(stream)); err != nil {
			t.Fatalf("replay stream into host vterm: %v", err)
		}
		return vt.ScreenContent()
	}

	got := captureScreen(buildModel(0), []int{1, 2, 3, 4, 5, 6, 7, 8})
	want := captureScreen(buildModel(8), nil)
	assertScreenEqual(t, got, want)
}

func TestOutputCursorWriterFrameLinesPathStreamingWideFloatingPaneMatchesFinalScreen(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	buildModel := func(start int) *Model {
		t.Helper()
		model := setupModel(t, modelOpts{width: 120, height: 36})
		base := model.runtime.Registry().GetOrCreate("term-1")
		base.Snapshot = cursorWriterStyledSnapshot("term-1", 118, 30)

		tab := model.workbench.CurrentTab()
		if tab == nil {
			t.Fatal("expected current tab")
		}
		rect := workbench.Rect{X: 18, Y: 7, W: 54, H: 16}
		if err := model.workbench.CreateFloatingPane(tab.ID, "float-1", rect); err != nil {
			t.Fatalf("create floating pane: %v", err)
		}
		if err := model.workbench.BindPaneTerminal(tab.ID, "float-1", "term-float"); err != nil {
			t.Fatalf("bind floating pane terminal: %v", err)
		}
		floatTerminal := model.runtime.Registry().GetOrCreate("term-float")
		floatTerminal.Name = "float"
		floatTerminal.State = "running"
		floatTerminal.Snapshot = cursorWriterWideScrollingSnapshot("term-float", 51, 14, start)
		model.runtime.BindPane("float-1").Connected = true
		return model
	}

	captureScreen := func(model *Model, starts []int) localvterm.ScreenData {
		t.Helper()
		sink := &cursorWriterProbeTTY{}
		writer := newOutputCursorWriter(sink)
		model.SetFrameWriter(writer)
		model.SetCursorWriter(writer)

		model.render.Invalidate()
		_ = model.View()

		floatTerminal := model.runtime.Registry().Get("term-float")
		if floatTerminal == nil {
			t.Fatal("expected floating terminal")
		}
		for _, start := range starts {
			floatTerminal.Snapshot = cursorWriterWideScrollingSnapshot("term-float", 51, 14, start)
			touchRuntimeVisibleStateForTest(model.runtime, uint8(start))
			model.render.Invalidate()
			_ = model.View()
		}

		sink.mu.Lock()
		stream := strings.Join(sink.writes, "")
		sink.mu.Unlock()
		vt := localvterm.New(120, 36, 0, nil)
		if _, err := vt.Write([]byte(stream)); err != nil {
			t.Fatalf("replay stream into host vterm: %v", err)
		}
		return vt.ScreenContent()
	}

	got := captureScreen(buildModel(0), []int{1, 2, 3, 4, 5, 6, 7, 8})
	want := captureScreen(buildModel(8), nil)
	assertScreenEqual(t, got, want)
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

func cursorWriterNvimLikeSnapshot(terminalID string, cols, rows int, bg string) *protocol.Snapshot {
	if cols <= 0 {
		cols = 1
	}
	if rows <= 0 {
		rows = 1
	}
	screen := make([][]protocol.Cell, 0, rows)
	for y := 0; y < rows; y++ {
		row := make([]protocol.Cell, 0, cols)
		label := "line " + strconv.Itoa(y+1)
		for x := 0; x < cols; x++ {
			cell := protocol.Cell{
				Content: " ",
				Width:   1,
				Style:   protocol.CellStyle{BG: bg},
			}
			if y == 0 && x < len(label) {
				cell.Content = string(label[x])
				cell.Style.FG = "#ffffff"
			}
			if y > 0 && x == 0 {
				cell.Content = "~"
				cell.Style.FG = "#4f5258"
			}
			row = append(row, cell)
		}
		screen = append(screen, row)
	}
	return &protocol.Snapshot{
		TerminalID: terminalID,
		Size:       protocol.Size{Cols: uint16(cols), Rows: uint16(rows)},
		Screen:     protocol.ScreenData{Cells: screen},
		Cursor:     protocol.CursorState{Row: 0, Col: 0, Visible: true},
		Modes:      protocol.TerminalModes{AlternateScreen: true, MouseTracking: true, BracketedPaste: true},
	}
}

func cursorWriterDenseTextSnapshot(terminalID string, cols, rows, start int) *protocol.Snapshot {
	if cols <= 0 {
		cols = 1
	}
	if rows <= 0 {
		rows = 1
	}
	screen := make([][]protocol.Cell, 0, rows)
	for y := 0; y < rows; y++ {
		line := "final block line " + strconv.Itoa(start+y)
		row := make([]protocol.Cell, 0, cols)
		for x := 0; x < cols; x++ {
			cell := protocol.Cell{
				Content: " ",
				Width:   1,
				Style:   protocol.CellStyle{FG: "#f8fafc", BG: "#0f172a"},
			}
			if x < len(line) {
				cell.Content = string(line[x])
				if x < 5 {
					cell.Style = protocol.CellStyle{FG: "#fde68a", BG: "#111827", Bold: true}
				}
			}
			row = append(row, cell)
		}
		screen = append(screen, row)
	}
	return &protocol.Snapshot{
		TerminalID: terminalID,
		Size:       protocol.Size{Cols: uint16(cols), Rows: uint16(rows)},
		Screen:     protocol.ScreenData{Cells: screen},
		Cursor:     protocol.CursorState{Row: rows - 1, Col: minInt(cols-1, 24), Visible: true},
		Modes:      protocol.TerminalModes{AutoWrap: true},
	}
}

func cursorWriterCodexInputSnapshot(terminalID string, cols, rows int, input string) *protocol.Snapshot {
	if cols <= 0 {
		cols = 1
	}
	if rows <= 0 {
		rows = 1
	}
	screen := make([][]protocol.Cell, 0, rows)
	for y := 0; y < rows; y++ {
		row := make([]protocol.Cell, 0, cols)
		line := "codex output line " + strconv.Itoa(y+1)
		for x := 0; x < cols; x++ {
			cell := protocol.Cell{
				Content: " ",
				Width:   1,
				Style:   protocol.CellStyle{FG: "#e5e7eb", BG: "#111827"},
			}
			if x < len(line) && y < rows-4 {
				cell.Content = string(line[x])
			}
			row = append(row, cell)
		}
		screen = append(screen, row)
	}
	boxRow := maxInt(0, rows-3)
	boxLeft := minInt(6, maxInt(0, cols-1))
	boxRight := maxInt(boxLeft+1, cols-6)
	label := "> " + input
	for x := boxLeft; x < boxRight && x < cols; x++ {
		screen[boxRow][x] = protocol.Cell{
			Content: " ",
			Width:   1,
			Style:   protocol.CellStyle{FG: "#f9fafb", BG: "#4b5563"},
		}
		idx := x - boxLeft
		if idx < len(label) {
			screen[boxRow][x].Content = string(label[idx])
		}
	}
	return &protocol.Snapshot{
		TerminalID: terminalID,
		Size:       protocol.Size{Cols: uint16(cols), Rows: uint16(rows)},
		Screen:     protocol.ScreenData{Cells: screen},
		Cursor:     protocol.CursorState{Row: boxRow, Col: minInt(cols-1, boxLeft+len(label)), Visible: true},
		Modes:      protocol.TerminalModes{AutoWrap: true},
	}
}

func vtermCodexInputScreen(cols, rows int, input string) localvterm.ScreenData {
	if cols <= 0 {
		cols = 1
	}
	if rows <= 0 {
		rows = 1
	}
	screen := make([][]localvterm.Cell, rows)
	for y := 0; y < rows; y++ {
		row := make([]localvterm.Cell, cols)
		line := "codex output line " + strconv.Itoa(y+1)
		for x := 0; x < cols; x++ {
			row[x] = localvterm.Cell{
				Content: " ",
				Width:   1,
				Style:   localvterm.CellStyle{FG: "#e5e7eb", BG: "#111827"},
			}
			if x < len(line) && y < rows-4 {
				row[x].Content = string(line[x])
			}
		}
		screen[y] = row
	}
	boxRow := maxInt(0, rows-3)
	boxLeft := minInt(6, maxInt(0, cols-1))
	boxRight := maxInt(boxLeft+1, cols-6)
	label := "> " + input
	for x := boxLeft; x < boxRight && x < cols; x++ {
		screen[boxRow][x] = localvterm.Cell{
			Content: " ",
			Width:   1,
			Style:   localvterm.CellStyle{FG: "#f9fafb", BG: "#4b5563"},
		}
		idx := x - boxLeft
		if idx < len(label) {
			screen[boxRow][x].Content = string(label[idx])
		}
	}
	return localvterm.ScreenData{Cells: screen}
}

func cursorWriterScrollingSnapshot(terminalID string, cols, rows, start int) *protocol.Snapshot {
	if cols <= 0 {
		cols = 1
	}
	if rows <= 0 {
		rows = 1
	}
	screen := make([][]protocol.Cell, 0, rows)
	for y := 0; y < rows; y++ {
		label := " row-" + strings.ToUpper(strconv.FormatInt(int64(start+y), 16)) + " "
		row := make([]protocol.Cell, 0, cols)
		for x := 0; x < cols; x++ {
			style := protocol.CellStyle{FG: "#f8fafc", BG: "#1f2937"}
			ch := " "
			if x < len(label) {
				ch = string(label[x])
			} else if (x+y)%11 == 0 {
				ch = string('a' + rune((start+x+y)%26))
				style = protocol.CellStyle{FG: "#fde68a", BG: "#0f172a", Bold: true}
			}
			row = append(row, protocol.Cell{
				Content: ch,
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
		Cursor:     protocol.CursorState{Row: rows - 1, Col: minInt(cols-1, 8), Visible: true},
		Modes:      protocol.TerminalModes{AutoWrap: true},
	}
}

func cursorWriterWideScrollingSnapshot(terminalID string, cols, rows, start int) *protocol.Snapshot {
	if cols <= 0 {
		cols = 1
	}
	if rows <= 0 {
		rows = 1
	}
	screen := make([][]protocol.Cell, 0, rows)
	for y := 0; y < rows; y++ {
		label := fmt.Sprintf("第%02d行 已完成 🔒 现在滚动到这里", start+y)
		screen = append(screen, protocolStyledWideRowFromText(label, cols, protocol.CellStyle{FG: "#f8fafc", BG: "#1f2937"}))
	}
	return &protocol.Snapshot{
		TerminalID: terminalID,
		Size:       protocol.Size{Cols: uint16(cols), Rows: uint16(rows)},
		Screen:     protocol.ScreenData{Cells: screen},
		Cursor:     protocol.CursorState{Row: rows - 1, Col: minInt(cols-1, 8), Visible: true},
		Modes:      protocol.TerminalModes{AutoWrap: true},
	}
}

func protocolStyledWideRowFromText(text string, cols int, style protocol.CellStyle) []protocol.Cell {
	if cols <= 0 {
		return nil
	}
	row := make([]protocol.Cell, cols)
	for i := range row {
		row[i] = protocol.Cell{Content: " ", Width: 1, Style: style}
	}
	col := 0
	for _, r := range text {
		if col >= cols {
			break
		}
		width := xansi.StringWidth(string(r))
		if width <= 0 {
			continue
		}
		if col+width > cols {
			break
		}
		row[col] = protocol.Cell{Content: string(r), Width: width, Style: style}
		for i := 1; i < width && col+i < cols; i++ {
			row[col+i] = protocol.Cell{Content: "", Width: 0, Style: style}
		}
		col += width
	}
	return row
}

func replayCursorWriterScreen(t *testing.T, width, height int, frames []string) localvterm.ScreenData {
	t.Helper()
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
	vt := localvterm.New(width, height, 0, nil)
	if _, err := vt.Write([]byte(stream)); err != nil {
		t.Fatalf("replay stream into host vterm: %v", err)
	}
	return vt.ScreenContent()
}

func assertScreenEqual(t *testing.T, got, want localvterm.ScreenData) {
	t.Helper()
	if err := screenDiffError(got, want); err != nil {
		t.Fatal(err)
	}
}

func screenDiffError(got, want localvterm.ScreenData) error {
	if len(got.Cells) != len(want.Cells) {
		return fmt.Errorf("screen height mismatch: got=%d want=%d", len(got.Cells), len(want.Cells))
	}
	for y := range want.Cells {
		if len(got.Cells[y]) != len(want.Cells[y]) {
			return fmt.Errorf("screen width mismatch on row %d: got=%d want=%d", y, len(got.Cells[y]), len(want.Cells[y]))
		}
		for x := range want.Cells[y] {
			if got.Cells[y][x] != want.Cells[y][x] {
				return fmt.Errorf("screen diverged at (%d,%d): got=%#v want=%#v", x, y, got.Cells[y][x], want.Cells[y][x])
			}
		}
	}
	return nil
}

func TestOutputCursorWriterFrameLinesPathCopyModeScrollbackThenFocusSwitchClearsStaleState(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	// Setup: two panes (nvim top, codex bottom), nvim in copy mode scrolled to history
	buildModel := func() *Model {
		t.Helper()
		wb := workbench.NewWorkbench()
		wb.AddWorkspace("main", &workbench.WorkspaceState{
			Name:      "main",
			ActiveTab: 0,
			Tabs: []*workbench.TabState{{
				ID:           "tab-1",
				Name:         "tab 1",
				ActivePaneID: "pane-1",
				Panes: map[string]*workbench.PaneState{
					"pane-1": {ID: "pane-1", Title: "nvim", TerminalID: "term-nvim"},
					"pane-2": {ID: "pane-2", Title: "codex", TerminalID: "term-codex"},
				},
				Root: &workbench.LayoutNode{
					Direction: workbench.SplitHorizontal,
					Ratio:     0.5,
					First:     workbench.NewLeaf("pane-1"),
					Second:    workbench.NewLeaf("pane-2"),
				},
			}},
		})

		rt := runtime.New(&recordingBridgeClient{
			attachResult:       &protocol.AttachResult{Channel: 1, Mode: "collaborator"},
			snapshotByTerminal: map[string]*protocol.Snapshot{},
		})
		nvimTerm := rt.Registry().GetOrCreate("term-nvim")
		nvimTerm.Name = "nvim"
		nvimTerm.State = "running"
		nvimTerm.Channel = 1
		// Snapshot with scrollback history
		nvimSnap := cursorWriterNvimLikeSnapshot("term-nvim", 118, 14, "#444444")
		scrollback := make([][]protocol.Cell, 50)
		for i := range scrollback {
			row := make([]protocol.Cell, 118)
			label := "history-line-" + strconv.Itoa(i)
			for x := range row {
				row[x] = protocol.Cell{Content: " ", Width: 1, Style: protocol.CellStyle{BG: "#444444"}}
				if x < len(label) {
					row[x].Content = string(label[x])
					row[x].Style.FG = "#aaaaaa"
				}
			}
			scrollback[i] = row
		}
		nvimSnap.Scrollback = scrollback
		nvimTerm.Snapshot = nvimSnap
		nvimBinding := rt.BindPane("pane-1")
		nvimBinding.Channel = 1
		nvimBinding.Connected = true

		codexTerm := rt.Registry().GetOrCreate("term-codex")
		codexTerm.Name = "codex"
		codexTerm.State = "running"
		codexTerm.Channel = 2
		codexTerm.Snapshot = cursorWriterDenseTextSnapshot("term-codex", 118, 14, 1)
		codexBinding := rt.BindPane("pane-2")
		codexBinding.Channel = 2
		codexBinding.Connected = true

		model := New(shared.Config{}, wb, rt)
		model.width = 120
		model.height = 36
		return model
	}

	// Capture the "got" screen: enter copy mode, scroll to history, then switch focus
	model := buildModel()
	sink := &cursorWriterProbeTTY{}
	writer := newOutputCursorWriter(sink)
	model.SetFrameWriter(writer)
	model.SetCursorWriter(writer)

	// Render initial frame
	model.render.Invalidate()
	_ = model.View()

	// Enter copy/display mode and scroll up into scrollback
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	model.render.Invalidate()
	_ = model.View()

	// Scroll up 40 lines into scrollback history
	for i := 0; i < 40; i++ {
		model.moveCopyCursorVertical(1)
	}
	model.render.Invalidate()
	_ = model.View()

	// Now switch focus to codex pane (simulating mouse click on codex)
	if err := model.workbench.FocusPane("tab-1", "pane-2"); err != nil {
		t.Fatalf("FocusPane: %v", err)
	}
	model.render.Invalidate()
	_ = model.View()

	sink.mu.Lock()
	stream := strings.Join(sink.writes, "")
	sink.mu.Unlock()
	vt := localvterm.New(120, 36, 0, nil)
	if _, err := vt.Write([]byte(stream)); err != nil {
		t.Fatalf("replay stream into host vterm: %v", err)
	}
	got := vt.ScreenContent()

	// Build the expected screen: codex focused, no copy mode, fresh render
	wantModel := buildModel()
	if err := wantModel.workbench.FocusPane("tab-1", "pane-2"); err != nil {
		t.Fatalf("FocusPane want: %v", err)
	}
	wantSink := &cursorWriterProbeTTY{}
	wantWriter := newOutputCursorWriter(wantSink)
	wantModel.SetFrameWriter(wantWriter)
	wantModel.SetCursorWriter(wantWriter)
	wantModel.render.Invalidate()
	_ = wantModel.View()

	wantSink.mu.Lock()
	wantStream := strings.Join(wantSink.writes, "")
	wantSink.mu.Unlock()
	wantVT := localvterm.New(120, 36, 0, nil)
	if _, err := wantVT.Write([]byte(wantStream)); err != nil {
		t.Fatalf("replay expected stream into host vterm: %v", err)
	}
	want := wantVT.ScreenContent()

	assertScreenEqual(t, got, want)
}

func touchRuntimeVisibleStateForTest(rt *runtime.Runtime, marker uint8) {
	if rt == nil {
		return
	}
	rt.SetHostPaletteColor(255, color.RGBA{R: marker, G: marker ^ 0x5a, B: marker ^ 0xa5, A: 0xff})
}
