package app

import (
	"context"
	"fmt"
	"image/color"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	xansi "github.com/charmbracelet/x/ansi"
	creackpty "github.com/creack/pty"
	"github.com/lozzow/termx/frameaudit"
	"github.com/lozzow/termx/perftrace"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/lozzow/termx/tuiv2/workbench"
	localvterm "github.com/lozzow/termx/vterm"
	"github.com/rivo/uniseg"
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
	writer.mu.Lock()
	writer.lastFlushAt = time.Now()
	writer.mu.Unlock()

	if err := writer.WriteFrame("frame-a", "<CURSOR-A>"); err != nil {
		t.Fatalf("write frame a: %v", err)
	}

	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.writes) != 1 {
		t.Fatalf("expected interactive direct frame to flush immediately, got %#v", sink.writes)
	}
}

func TestOutputCursorWriterFlushesImmediatelyAfterResetFrameState(t *testing.T) {
	originalDelay := directFrameBatchDelay
	originalIdleThreshold := directFrameIdleThreshold
	directFrameBatchDelay = 20 * time.Millisecond
	directFrameIdleThreshold = time.Hour
	defer func() {
		directFrameBatchDelay = originalDelay
		directFrameIdleThreshold = originalIdleThreshold
	}()

	sink := &cursorWriterProbeSink{}
	writer := newOutputCursorWriter(sink)
	writer.mu.Lock()
	writer.lastFlushAt = time.Now()
	writer.mu.Unlock()

	if err := writer.WriteFrameLines([]string{"frame-a"}, ""); err != nil {
		t.Fatalf("write initial frame lines: %v", err)
	}
	writer.ResetFrameState()
	if err := writer.WriteFrameLines([]string{"frame-b"}, ""); err != nil {
		t.Fatalf("write post-reset frame lines: %v", err)
	}
	if writer.HasPendingFrame() {
		t.Fatal("expected no pending frame after post-reset immediate flush")
	}
}

func TestOutputCursorWriterResetFrameStateOnlyForcesOneImmediateFlush(t *testing.T) {
	originalDelay := directFrameBatchDelay
	originalIdleThreshold := directFrameIdleThreshold
	directFrameBatchDelay = 20 * time.Millisecond
	directFrameIdleThreshold = time.Hour
	defer func() {
		directFrameBatchDelay = originalDelay
		directFrameIdleThreshold = originalIdleThreshold
	}()

	sink := &cursorWriterProbeSink{}
	writer := newOutputCursorWriter(sink)
	writer.mu.Lock()
	writer.lastFlushAt = time.Now()
	writer.mu.Unlock()

	if err := writer.WriteFrameLines([]string{"frame-a"}, ""); err != nil {
		t.Fatalf("write initial frame lines: %v", err)
	}
	writer.ResetFrameState()
	if err := writer.WriteFrameLines([]string{"frame-b"}, ""); err != nil {
		t.Fatalf("write post-reset frame lines: %v", err)
	}
	if writer.HasPendingFrame() {
		t.Fatal("expected reset-triggered frame to flush immediately")
	}
	if err := writer.WriteFrameLines([]string{"frame-c"}, ""); err != nil {
		t.Fatalf("write subsequent frame lines: %v", err)
	}
	if !writer.HasPendingFrame() {
		t.Fatal("expected batching to resume after the first post-reset frame")
	}
}

func TestOutputCursorWriterRemoteProfileFlushesInteractiveFramesImmediately(t *testing.T) {
	t.Setenv("TERMX_REMOTE_LATENCY", "1")
	originalDelay := directFrameBatchDelay
	originalIdleThreshold := directFrameIdleThreshold
	originalRemoteDelay := remoteDirectFrameBatchDelay
	originalRemoteIdle := remoteDirectFrameIdleThreshold
	directFrameBatchDelay = 4 * time.Millisecond
	directFrameIdleThreshold = time.Hour
	remoteDirectFrameBatchDelay = 8 * time.Millisecond
	remoteDirectFrameIdleThreshold = 20 * time.Millisecond
	defer func() {
		directFrameBatchDelay = originalDelay
		directFrameIdleThreshold = originalIdleThreshold
		remoteDirectFrameBatchDelay = originalRemoteDelay
		remoteDirectFrameIdleThreshold = originalRemoteIdle
	}()

	sink := &cursorWriterProbeTTY{}
	writer := newOutputCursorWriter(sink)
	writer.SetInteractiveFlushHint(func() bool { return true })
	if !shared.RemoteLatencyProfileEnabled() {
		t.Fatal("expected remote latency profile enabled")
	}
	writer.mu.Lock()
	writer.lastFlushAt = time.Now()
	if !writer.shouldFlushDirectFrameImmediatelyLocked() {
		writer.mu.Unlock()
		t.Fatal("expected remote profile interactive frame to flush immediately")
	}
	writer.mu.Unlock()

	if err := writer.WriteFrame("frame-a", "<CURSOR-A>"); err != nil {
		t.Fatalf("write frame a: %v", err)
	}

	sink.mu.Lock()
	if len(sink.writes) != 1 {
		sink.mu.Unlock()
		t.Fatalf("expected remote profile to flush interactive frame immediately, got %#v", sink.writes)
	}
	sink.mu.Unlock()
	if writer.HasPendingFrame() {
		t.Fatal("expected remote interactive frame to drain immediately")
	}
}

func TestOutputCursorWriterRemoteProfileLowersBaseBatchDelay(t *testing.T) {
	t.Setenv("TERMX_REMOTE_LATENCY", "1")
	originalDelay := directFrameBatchDelay
	originalRemoteDelay := remoteDirectFrameBatchDelay
	directFrameBatchDelay = 4 * time.Millisecond
	remoteDirectFrameBatchDelay = 1500 * time.Microsecond
	defer func() {
		directFrameBatchDelay = originalDelay
		remoteDirectFrameBatchDelay = originalRemoteDelay
	}()

	writer := newOutputCursorWriter(&cursorWriterProbeSink{})
	writer.mu.Lock()
	got := writer.effectiveDirectFrameBatchDelayLocked()
	writer.mu.Unlock()

	if got != remoteDirectFrameBatchDelay {
		t.Fatalf("expected remote direct-frame batch delay %v, got %v", remoteDirectFrameBatchDelay, got)
	}
}

func TestOutputCursorWriterAdaptiveBatchDelayIncreasesAfterSlowFlushCost(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 4 * time.Millisecond
	defer func() { directFrameBatchDelay = originalDelay }()

	writer := newOutputCursorWriter(&cursorWriterProbeSink{})
	writer.mu.Lock()
	base := writer.effectiveDirectFrameBatchDelayLocked()
	for i := 0; i < directFrameAdaptiveSlowSamples; i++ {
		writer.observeDirectFlushCostLocked(directFrameDrainSlowThreshold + time.Millisecond)
	}
	got := writer.effectiveDirectFrameBatchDelayLocked()
	writer.mu.Unlock()
	if got <= base {
		t.Fatalf("expected adaptive direct-frame delay to increase, base=%v got=%v", base, got)
	}
}

func TestOutputCursorWriterAdaptiveBatchDelayRecoversAfterFastFlushCost(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 4 * time.Millisecond
	defer func() { directFrameBatchDelay = originalDelay }()

	writer := newOutputCursorWriter(&cursorWriterProbeSink{})
	writer.mu.Lock()
	for i := 0; i < directFrameAdaptiveSlowSamples*2; i++ {
		writer.observeDirectFlushCostLocked(directFrameDrainSlowThreshold + time.Millisecond)
	}
	raised := writer.effectiveDirectFrameBatchDelayLocked()
	for i := 0; i < directFrameAdaptiveFastSamples*3; i++ {
		writer.observeDirectFlushCostLocked(directFrameDrainFastThreshold / 2)
	}
	recovered := writer.effectiveDirectFrameBatchDelayLocked()
	writer.mu.Unlock()
	if recovered >= raised {
		t.Fatalf("expected adaptive direct-frame delay to recover, raised=%v recovered=%v", raised, recovered)
	}
	if recovered != directFrameBatchDelay {
		t.Fatalf("expected adaptive direct-frame delay to recover to base, base=%v recovered=%v", directFrameBatchDelay, recovered)
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

func TestOutputCursorWriterStyledRowUsesRunDiffInsteadOfLongSuffixRewrite(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	sink := &cursorWriterProbeTTY{}
	writer := newOutputCursorWriter(sink)

	styleRow := func(left, middle, right string) string {
		return "\x1b[31m" + left + "\x1b[32m" + middle + "\x1b[34m" + right + "\x1b[0m"
	}
	frame1 := strings.Join([]string{
		styleRow(strings.Repeat("A", 20), strings.Repeat("B", 20), strings.Repeat("C", 20)),
		styleRow(strings.Repeat("D", 20), strings.Repeat("E", 20), strings.Repeat("F", 20)),
		styleRow(strings.Repeat("G", 20), strings.Repeat("H", 20), strings.Repeat("I", 20)),
	}, "\n")
	frame2 := strings.Join([]string{
		styleRow(strings.Repeat("A", 20), "BBBBBBBBBBZBBBBBBBBB", strings.Repeat("C", 20)),
		styleRow(strings.Repeat("D", 20), strings.Repeat("E", 20), "FFFFFFFFFFYFFFFFFFFF"),
		styleRow(strings.Repeat("G", 20), strings.Repeat("H", 20), strings.Repeat("I", 20)),
	}, "\n")

	if err := writer.WriteFrame(frame1, ""); err != nil {
		t.Fatalf("write initial styled frame: %v", err)
	}
	sink.mu.Lock()
	sink.writes = nil
	sink.mu.Unlock()

	if err := writer.WriteFrame(frame2, ""); err != nil {
		t.Fatalf("write updated styled frame: %v", err)
	}

	sink.mu.Lock()
	got := strings.Join(sink.writes, "")
	sink.mu.Unlock()

	if len(got) == 0 {
		t.Fatal("expected styled diff payload")
	}
	if len(got) >= len(frame2) {
		t.Fatalf("expected styled run diff payload smaller than full frame, got len=%d full=%d", len(got), len(frame2))
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

func TestOutputCursorWriterFrameLinesUsesDCHForShiftDelete(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	previous := []string{"abcdefghijkl"}
	next := []string{"abdefghijklZ"}

	sink := &cursorWriterProbeTTY{}
	writer := newOutputCursorWriter(sink)
	if err := writer.WriteFrameLines(previous, ""); err != nil {
		t.Fatalf("write initial frame lines: %v", err)
	}
	sink.mu.Lock()
	sink.writes = nil
	sink.mu.Unlock()

	if err := writer.WriteFrameLines(next, ""); err != nil {
		t.Fatalf("write diff frame lines: %v", err)
	}

	sink.mu.Lock()
	got := strings.Join(sink.writes, "")
	sink.mu.Unlock()

	if !strings.Contains(got, "\x1b[1P") {
		t.Fatalf("expected DCH-based row delta, got %q", got)
	}
	screen := replayCursorWriterLineScreen(t, 12, 1, [][]string{previous, next})
	want := replayCursorWriterLineScreen(t, 12, 1, [][]string{next})
	assertScreenEqual(t, screen, want)
}

func TestOutputCursorWriterFrameLinesUsesICHForShiftInsert(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	previous := []string{"abdefghijklZ"}
	next := []string{"abcdefghijkl"}

	sink := &cursorWriterProbeTTY{}
	writer := newOutputCursorWriter(sink)
	if err := writer.WriteFrameLines(previous, ""); err != nil {
		t.Fatalf("write initial frame lines: %v", err)
	}
	sink.mu.Lock()
	sink.writes = nil
	sink.mu.Unlock()

	if err := writer.WriteFrameLines(next, ""); err != nil {
		t.Fatalf("write diff frame lines: %v", err)
	}

	sink.mu.Lock()
	got := strings.Join(sink.writes, "")
	sink.mu.Unlock()

	if !strings.Contains(got, "\x1b[1@") {
		t.Fatalf("expected ICH-based row delta, got %q", got)
	}
	screen := replayCursorWriterLineScreen(t, 12, 1, [][]string{previous, next})
	want := replayCursorWriterLineScreen(t, 12, 1, [][]string{next})
	assertScreenEqual(t, screen, want)
}

func TestOutputCursorWriterFrameLinesUsesECHForInteriorErase(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	previous := []string{"abcWXYZdef"}
	next := []string{"abc    def"}

	sink := &cursorWriterProbeTTY{}
	writer := newOutputCursorWriter(sink)
	if err := writer.WriteFrameLines(previous, ""); err != nil {
		t.Fatalf("write initial frame lines: %v", err)
	}
	sink.mu.Lock()
	sink.writes = nil
	sink.mu.Unlock()

	if err := writer.WriteFrameLines(next, ""); err != nil {
		t.Fatalf("write diff frame lines: %v", err)
	}

	sink.mu.Lock()
	got := strings.Join(sink.writes, "")
	sink.mu.Unlock()

	if !strings.Contains(got, xansi.ECH(4)) {
		t.Fatalf("expected ECH-based row delta, got %q", got)
	}
	screen := replayCursorWriterLineScreen(t, 10, 1, [][]string{previous, next})
	want := replayCursorWriterLineScreen(t, 10, 1, [][]string{next})
	assertScreenEqual(t, screen, want)
}

func TestOutputCursorWriterFrameLinesUsesELForTrailingErase(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	previous := []string{"abcXYZdef0"}
	next := []string{"abc       "}

	sink := &cursorWriterProbeTTY{}
	writer := newOutputCursorWriter(sink)
	if err := writer.WriteFrameLines(previous, ""); err != nil {
		t.Fatalf("write initial frame lines: %v", err)
	}
	sink.mu.Lock()
	sink.writes = nil
	sink.mu.Unlock()

	if err := writer.WriteFrameLines(next, ""); err != nil {
		t.Fatalf("write diff frame lines: %v", err)
	}

	sink.mu.Lock()
	got := strings.Join(sink.writes, "")
	sink.mu.Unlock()

	if !strings.Contains(got, "\x1b[0K") {
		t.Fatalf("expected EL-based row delta, got %q", got)
	}
	screen := replayCursorWriterLineScreen(t, 10, 1, [][]string{previous, next})
	want := replayCursorWriterLineScreen(t, 10, 1, [][]string{next})
	assertScreenEqual(t, screen, want)
}

func TestOutputCursorWriterFrameLinesWideRowSkipsIntralineEditSequences(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	previous := []string{"ab界defghi "}
	next := []string{"ab甲defghi "}

	sink := &cursorWriterProbeTTY{}
	writer := newOutputCursorWriter(sink)
	if err := writer.WriteFrameLines(previous, ""); err != nil {
		t.Fatalf("write initial wide frame lines: %v", err)
	}
	sink.mu.Lock()
	sink.writes = nil
	sink.mu.Unlock()

	if err := writer.WriteFrameLines(next, ""); err != nil {
		t.Fatalf("write diff wide frame lines: %v", err)
	}

	sink.mu.Lock()
	got := strings.Join(sink.writes, "")
	sink.mu.Unlock()

	for _, seq := range []string{xansi.DCH(1), xansi.ICH(1), xansi.ECH(1), xansi.EL(0)} {
		if strings.Contains(got, seq) {
			t.Fatalf("expected wide row to stay on conservative fallback, got %q", got)
		}
	}
	screen := replayCursorWriterLineScreen(t, 11, 1, [][]string{previous, next})
	want := replayCursorWriterLineScreen(t, 11, 1, [][]string{next})
	assertScreenEqual(t, screen, want)
}

func TestOutputCursorWriterOwnerAwareDiffOnlyRewritesLeftPaneSegment(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	previous := []string{
		"TOP-ROW-0",
		"aaaa RRRR",
		"bbbb RRRR",
		"BOT-ROW-0",
	}
	next := []string{
		"TOP-ROW-0",
		"aaZZ RRRR",
		"bbYY RRRR",
		"BOT-ROW-0",
	}
	meta := ownerAwareTestMeta(
		[]hostOwnerID{1, 1, 1, 1, 1, 1, 1, 1, 1},
		[]hostOwnerID{10, 10, 10, 10, 0, 20, 20, 20, 20},
		[]hostOwnerID{10, 10, 10, 10, 0, 20, 20, 20, 20},
		[]hostOwnerID{2, 2, 2, 2, 2, 2, 2, 2, 2},
	)

	sink := &cursorWriterProbeTTY{}
	writer := newOutputCursorWriter(sink)
	if err := writer.WriteFrameLinesWithMeta(previous, "", meta); err != nil {
		t.Fatalf("write initial frame lines: %v", err)
	}
	sink.mu.Lock()
	sink.writes = nil
	sink.mu.Unlock()

	if err := writer.WriteFrameLinesWithMeta(next, "", meta); err != nil {
		t.Fatalf("write owner-aware diff frame lines: %v", err)
	}

	sink.mu.Lock()
	got := strings.Join(sink.writes, "")
	sink.mu.Unlock()

	if strings.Contains(got, "RRRR") {
		t.Fatalf("expected owner-aware diff to avoid rewriting unchanged right pane, got %q", got)
	}
	screen := replayCursorWriterLineScreenWithMeta(t, 9, 4, [][]string{previous, next}, []*presentMeta{meta, meta})
	want := replayCursorWriterLineScreenWithMeta(t, 9, 4, [][]string{next}, []*presentMeta{meta})
	assertScreenEqual(t, screen, want)
}

func TestOutputCursorWriterOwnerAwareFloatCutoutStaysLocalWhenLRScrollDisabled(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	previous := []string{
		"TOP-ROW-000",
		"lefta FLOAT ",
		"leftb FLOAT ",
		"leftc right ",
		"BOT-ROW-000",
	}
	next := []string{
		"TOP-ROW-000",
		"leftZ FLOAT ",
		"leftY FLOAT ",
		"leftX right ",
		"BOT-ROW-000",
	}
	meta := ownerAwareTestMeta(
		[]hostOwnerID{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1},
		[]hostOwnerID{10, 10, 10, 10, 10, 0, 30, 30, 30, 30, 30, 0},
		[]hostOwnerID{10, 10, 10, 10, 10, 0, 30, 30, 30, 30, 30, 0},
		[]hostOwnerID{10, 10, 10, 10, 10, 0, 20, 20, 20, 20, 20, 0},
		[]hostOwnerID{2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2},
	)

	sink := &cursorWriterProbeTTY{}
	writer := newOutputCursorWriter(sink)
	if err := writer.WriteFrameLinesWithMeta(previous, "", meta); err != nil {
		t.Fatalf("write initial owner-aware frame lines: %v", err)
	}
	sink.mu.Lock()
	sink.writes = nil
	sink.mu.Unlock()

	if err := writer.WriteFrameLinesWithMeta(next, "", meta); err != nil {
		t.Fatalf("write float-cutout owner-aware frame lines: %v", err)
	}

	sink.mu.Lock()
	got := strings.Join(sink.writes, "")
	sink.mu.Unlock()

	if strings.Contains(got, xansi.SetModeLeftRightMargin) {
		t.Fatalf("expected LR-margin scroll to stay disabled by default, got %q", got)
	}
	if strings.Contains(got, "right") || strings.Contains(got, "FLOAT") {
		t.Fatalf("expected owner-aware diff to stay local to changed visible left-pane segments, got %q", got)
	}
	screen := replayCursorWriterLineScreenWithMeta(t, 12, 5, [][]string{previous, next}, []*presentMeta{meta, meta})
	want := replayCursorWriterLineScreenWithMeta(t, 12, 5, [][]string{next}, []*presentMeta{meta})
	assertScreenEqual(t, screen, want)
}

func TestOutputCursorWriterOwnerAwareRectScrollUsesLRMarginsWhenEnabled(t *testing.T) {
	t.Setenv("TERMX_EXPERIMENTAL_LR_SCROLL", "1")
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	previous := []string{
		"TOP-ROW-0",
		"aaaa RRRR",
		"bbbb RRRR",
		"cccc RRRR",
		"dddd RRRR",
		"eeee RRRR",
		"ffff RRRR",
		"BOT-ROW-0",
	}
	next := []string{
		"TOP-ROW-0",
		"bbbb RRRR",
		"cccc RRRR",
		"dddd RRRR",
		"eeee RRRR",
		"ffff RRRR",
		"gggg RRRR",
		"BOT-ROW-0",
	}
	meta := ownerAwareTestMeta(
		[]hostOwnerID{1, 1, 1, 1, 1, 1, 1, 1, 1},
		[]hostOwnerID{10, 10, 10, 10, 0, 20, 20, 20, 20},
		[]hostOwnerID{10, 10, 10, 10, 0, 20, 20, 20, 20},
		[]hostOwnerID{10, 10, 10, 10, 0, 20, 20, 20, 20},
		[]hostOwnerID{10, 10, 10, 10, 0, 20, 20, 20, 20},
		[]hostOwnerID{10, 10, 10, 10, 0, 20, 20, 20, 20},
		[]hostOwnerID{10, 10, 10, 10, 0, 20, 20, 20, 20},
		[]hostOwnerID{2, 2, 2, 2, 2, 2, 2, 2, 2},
	)

	sink := &cursorWriterProbeTTY{}
	writer := newOutputCursorWriter(sink)
	writer.SetVerticalScrollMode(verticalScrollModeRectsOnly)
	if err := writer.WriteFrameLinesWithMeta(previous, "", meta); err != nil {
		t.Fatalf("write initial owner-aware frame lines: %v", err)
	}
	sink.mu.Lock()
	sink.writes = nil
	sink.mu.Unlock()
	perftrace.Enable()
	perftrace.Reset()
	defer perftrace.Disable()

	if err := writer.WriteFrameLinesWithMeta(next, "", meta); err != nil {
		t.Fatalf("write owner-aware scroll frame lines: %v", err)
	}

	sink.mu.Lock()
	got := strings.Join(sink.writes, "")
	sink.mu.Unlock()

	if !strings.Contains(got, xansi.SetModeLeftRightMargin) {
		t.Fatalf("expected owner-aware narrow scroll to enable LR margins, got %q", got)
	}
	if !strings.Contains(got, "\x1b[1;4s") {
		t.Fatalf("expected owner-aware narrow scroll to set pane column margins, got %q", got)
	}
	if !strings.Contains(got, "\x1b[1S") {
		t.Fatalf("expected owner-aware narrow scroll to emit SU, got %q", got)
	}
	snapshot := perftrace.SnapshotCurrent()
	if event, ok := snapshot.Event("cursor_writer.present.mode.delta_rect_scroll_lr_margin"); !ok || event.Count == 0 {
		t.Fatalf("expected owner-aware LR-margin scroll perf event, got %#v", snapshot.Events)
	}
	screen := replayCursorWriterLineScreenWithMeta(t, 9, 8, [][]string{previous, next}, []*presentMeta{meta, meta})
	want := replayCursorWriterLineScreenWithMeta(t, 9, 8, [][]string{next}, []*presentMeta{meta})
	assertScreenEqual(t, screen, want)
}

func TestFramePresenterOwnerAwareDeltaFallsBackForHostWidthSafetyRows(t *testing.T) {
	previous := []string{
		"é" + xansi.CHA(2) + "X" + xansi.CHA(6) + "│",
	}
	next := []string{
		"è" + xansi.CHA(2) + "Y" + xansi.CHA(6) + "│",
	}
	meta := ownerAwareTestMeta([]hostOwnerID{10, 10, 10, 10, 10, 20})

	var presenter framePresenter
	presenter.fullWidthLines = true
	presenter.setLines(previous, true)
	presenter.meta = clonePresentMeta(meta)

	if payload := presenter.presentOwnerAwareDelta(next, meta); payload != "" {
		t.Fatalf("expected owner-aware delta to bail out on width-unsafe rows, got %q", payload)
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

	if len(got) == 0 {
		t.Fatal("expected unsafe emoji row to produce a diff payload")
	}
	screen := replayCursorWriterScreen(t, 16, 2, []string{frame1, frame2})
	want := replayCursorWriterScreen(t, 16, 2, []string{frame2})
	assertScreenEqual(t, screen, want)
}

func TestOutputCursorWriterFrameLinesFallsBackToFullRowForUnsafeEmojiRow(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	sink := &cursorWriterProbeTTY{}
	writer := newOutputCursorWriter(sink)

	line1 := xansi.CHA(1) + "AAA❄️" + xansi.ECH(1) + xansi.CHA(6) + "BB" + "\x1b[0m\x1b[K"
	line2 := xansi.CHA(1) + "AAA❄️" + xansi.ECH(1) + xansi.CHA(6) + "BC" + "\x1b[0m\x1b[K"
	if err := writer.WriteFrameLines([]string{line1}, ""); err != nil {
		t.Fatalf("write initial unsafe line frame: %v", err)
	}
	sink.mu.Lock()
	sink.writes = nil
	sink.mu.Unlock()

	if err := writer.WriteFrameLines([]string{line2}, ""); err != nil {
		t.Fatalf("write unsafe line diff frame: %v", err)
	}

	sink.mu.Lock()
	got := strings.Join(sink.writes, "")
	sink.mu.Unlock()

	if len(got) == 0 {
		t.Fatal("expected unsafe emoji line row to produce a diff payload")
	}
	screen := replayCursorWriterLineScreen(t, 16, 2, [][]string{{line1}, {line2}})
	want := replayCursorWriterLineScreen(t, 16, 2, [][]string{{line2}})
	assertScreenEqual(t, screen, want)
}

func TestOutputCursorWriterWriteFrameKeepsAmbiguousWidthRowAlignedOnWideHost(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	sink := &cursorWriterProbeTTY{}
	writer := newOutputCursorWriter(sink)

	frame1 := "é" + xansi.CHA(2) + "X" + xansi.CHA(6) + "│" + "\x1b[0m\x1b[K"
	frame2 := "è" + xansi.CHA(2) + "X" + xansi.CHA(6) + "│" + "\x1b[0m\x1b[K"
	if err := writer.WriteFrame(frame1, ""); err != nil {
		t.Fatalf("write initial ambiguous-width frame: %v", err)
	}
	if err := writer.WriteFrame(frame2, ""); err != nil {
		t.Fatalf("write updated ambiguous-width frame: %v", err)
	}

	sink.mu.Lock()
	writes := append([]string(nil), sink.writes...)
	sink.mu.Unlock()

	host := replayCursorWriterFakeHost(t, 6, 1, writes, 2)
	if got := host.cells[0][1]; got != "X" {
		t.Fatalf("expected ambiguous-width row update to keep the next cell anchored, got %q in %q", got, host.lines()[0])
	}
	if got := host.cells[0][5]; got != "│" {
		t.Fatalf("expected ambiguous-width row update to keep the right border anchored, got %q in %q", got, host.lines()[0])
	}
}

func TestOutputCursorWriterWriteFrameLinesKeepsAmbiguousWidthRowAlignedOnWideHost(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	sink := &cursorWriterProbeTTY{}
	writer := newOutputCursorWriter(sink)

	line1 := "é" + xansi.CHA(2) + "X" + xansi.CHA(6) + "│" + "\x1b[0m\x1b[K"
	line2 := "è" + xansi.CHA(2) + "X" + xansi.CHA(6) + "│" + "\x1b[0m\x1b[K"
	if err := writer.WriteFrameLines([]string{line1}, ""); err != nil {
		t.Fatalf("write initial ambiguous-width line frame: %v", err)
	}
	if err := writer.WriteFrameLines([]string{line2}, ""); err != nil {
		t.Fatalf("write updated ambiguous-width line frame: %v", err)
	}

	sink.mu.Lock()
	writes := append([]string(nil), sink.writes...)
	sink.mu.Unlock()

	host := replayCursorWriterFakeHost(t, 6, 1, writes, 2)
	if got := host.cells[0][1]; got != "X" {
		t.Fatalf("expected ambiguous-width line update to keep the next cell anchored, got %q in %q", got, host.lines()[0])
	}
	if got := host.cells[0][5]; got != "│" {
		t.Fatalf("expected ambiguous-width line update to keep the right border anchored, got %q in %q", got, host.lines()[0])
	}
}

func TestOutputCursorWriterWriteFrameKeepsPrintableZeroWidthRowAlignedOnZeroWidthHost(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	sink := &cursorWriterProbeTTY{}
	writer := newOutputCursorWriter(sink)

	frame1 := "\u00ad" + xansi.CHA(2) + "X" + "\x1b[0m\x1b[K"
	frame2 := "\u00ad" + xansi.CHA(2) + "Y" + "\x1b[0m\x1b[K"
	if err := writer.WriteFrame(frame1, ""); err != nil {
		t.Fatalf("write initial printable-zero-width frame: %v", err)
	}
	if err := writer.WriteFrame(frame2, ""); err != nil {
		t.Fatalf("write updated printable-zero-width frame: %v", err)
	}

	sink.mu.Lock()
	writes := append([]string(nil), sink.writes...)
	sink.mu.Unlock()

	host := replayCursorWriterFakeHost(t, 4, 1, writes, 0)
	if got := host.cells[0][1]; got != "Y" {
		t.Fatalf("expected printable-zero-width row update to keep the next cell anchored, got %q in %q", got, host.lines()[0])
	}
}

func TestOutputCursorWriterWideStyledRowUsesDiffWhenNoEraseIsPresent(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	sink := &cursorWriterProbeTTY{}
	writer := newOutputCursorWriter(sink)

	frame1 := "\x1b[31mLEFT\x1b[0m \x1b[32m界面\x1b[0m \x1b[34mRIGHT\x1b[0m"
	frame2 := "\x1b[31mLEFT\x1b[0m \x1b[32m界面\x1b[0m \x1b[34mRIGHZ\x1b[0m"
	if err := writer.WriteFrame(frame1, ""); err != nil {
		t.Fatalf("write initial wide frame: %v", err)
	}
	sink.mu.Lock()
	sink.writes = nil
	sink.mu.Unlock()

	if err := writer.WriteFrame(frame2, ""); err != nil {
		t.Fatalf("write updated wide frame: %v", err)
	}

	sink.mu.Lock()
	got := strings.Join(sink.writes, "")
	sink.mu.Unlock()

	if len(got) == 0 {
		t.Fatal("expected wide diff payload")
	}
	if strings.Contains(got, frame2) {
		t.Fatalf("expected wide row diff not to rewrite the full target row, got %q", got)
	}

	screen := replayCursorWriterScreen(t, 40, 4, []string{frame1, frame2})
	want := replayCursorWriterScreen(t, 40, 4, []string{frame2})
	assertScreenEqual(t, screen, want)
}

func TestOutputCursorWriterSkipsSemanticallyEquivalentStyledFrame(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	sink := &cursorWriterProbeTTY{}
	writer := newOutputCursorWriter(sink)

	frame1 := "\x1b[31mA\x1b[0mB"
	frame2 := "\x1b[31mA\x1b[mB"
	if err := writer.WriteFrame(frame1, ""); err != nil {
		t.Fatalf("write initial styled frame: %v", err)
	}
	sink.mu.Lock()
	sink.writes = nil
	sink.mu.Unlock()

	if err := writer.WriteFrame(frame2, ""); err != nil {
		t.Fatalf("write semantically equivalent styled frame: %v", err)
	}

	sink.mu.Lock()
	got := append([]string(nil), sink.writes...)
	sink.mu.Unlock()

	if len(got) != 0 {
		t.Fatalf("expected semantically equivalent styled frame to be skipped, got %#v", got)
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
	frame1 := []string{
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
	}
	frame2 := []string{
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
	}

	var presenter framePresenter
	presenter.verticalScrollMode = verticalScrollModeRowsAndRects
	presenter.debugFaultScrollDropRemainderEvery = 1
	presenter.setLines(frame1, true)

	got := presenter.presentVerticalScroll(frame2)

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

func TestDetectVerticalScrollRectPlanFindsSideBySideScrollUpRegion(t *testing.T) {
	previousLines := []string{
		"hdr0AAAA",
		"aaaa1111",
		"bbbb2222",
		"cccc3333",
		"dddd4444",
		"eeee5555",
		"ffff6666",
		"ftr7ZZZZ",
	}
	nextLines := []string{
		"hdr0AAAA",
		"bbbb1111",
		"cccc2222",
		"dddd3333",
		"eeee4444",
		"ffff5555",
		"gggg6666",
		"ftr7ZZZZ",
	}
	previous := make([]presentedRow, len(previousLines))
	next := make([]presentedRow, len(nextLines))
	for i := range previousLines {
		previous[i] = parsePresentedRow(previousLines[i])
		next[i] = parsePresentedRow(nextLines[i])
	}
	defer releasePresentedRows(previous)
	defer releasePresentedRows(next)

	plan, ok := detectVerticalScrollRectPlan(previous, next)
	if !ok {
		t.Fatal("expected vertical scroll rect plan")
	}
	if plan.direction != scrollUp {
		t.Fatalf("expected scrollUp rect plan, got %#v", plan)
	}
	if plan.shift != 1 || plan.start != 1 || plan.end != 6 {
		t.Fatalf("unexpected rect scroll row bounds %#v", plan)
	}
	if plan.left != 0 || plan.right != 3 {
		t.Fatalf("unexpected rect scroll columns %#v", plan)
	}
	if plan.reused != 5 {
		t.Fatalf("expected five reused rows in rect scroll plan, got %#v", plan)
	}
}

func TestRenderVerticalScrollRectPlanBuildsExpectedCSISequences(t *testing.T) {
	plan := verticalScrollRectPlan{
		direction: scrollUp,
		start:     1,
		end:       6,
		shift:     1,
		reused:    5,
		left:      0,
		right:     3,
	}
	want := xansi.SaveCursor +
		xansi.SetModeLeftRightMargin +
		xansi.DECSLRM(1, 4) +
		"\x1b[2;7r" +
		xansi.DECRST(xansi.ModeOrigin) +
		"\x1b[2;1H" +
		"\x1b[1S" +
		"\x1b[r" +
		xansi.ResetModeLeftRightMargin +
		xansi.DECRST(xansi.ModeOrigin) +
		xansi.RestoreCursor
	if got := renderVerticalScrollRectPlan(plan, 8); got != want {
		t.Fatalf("unexpected rect vertical scroll CSI %q want %q", got, want)
	}
}

func TestOutputCursorWriterChoosesCheapestPatchForSideBySidePaneScroll(t *testing.T) {
	t.Setenv("TERMX_EXPERIMENTAL_LR_SCROLL", "1")
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	previous := []string{
		"hdr0AAAA",
		"aaaa1111",
		"bbbb2222",
		"cccc3333",
		"dddd4444",
		"eeee5555",
		"ffff6666",
		"ftr7ZZZZ",
	}
	next := []string{
		"hdr0AAAA",
		"bbbb1111",
		"cccc2222",
		"dddd3333",
		"eeee4444",
		"ffff5555",
		"gggg6666",
		"ftr7ZZZZ",
	}

	sink := &cursorWriterProbeTTY{}
	writer := newOutputCursorWriter(sink)
	writer.SetVerticalScrollMode(verticalScrollModeRectsOnly)

	if err := writer.WriteFrameLines(previous, ""); err != nil {
		t.Fatalf("write initial frame: %v", err)
	}
	sink.mu.Lock()
	sink.writes = nil
	sink.mu.Unlock()
	perftrace.Reset()

	if err := writer.WriteFrameLines(next, ""); err != nil {
		t.Fatalf("write scrolled frame: %v", err)
	}

	sink.mu.Lock()
	got := strings.Join(sink.writes, "")
	sink.mu.Unlock()

	fallbackSink := &cursorWriterProbeTTY{}
	fallbackWriter := newOutputCursorWriter(fallbackSink)
	fallbackWriter.SetVerticalScrollMode(verticalScrollModeNone)
	if err := fallbackWriter.WriteFrameLines(previous, ""); err != nil {
		t.Fatalf("write fallback initial frame: %v", err)
	}
	fallbackSink.mu.Lock()
	fallbackSink.writes = nil
	fallbackSink.mu.Unlock()
	if err := fallbackWriter.WriteFrameLines(next, ""); err != nil {
		t.Fatalf("write fallback scrolled frame: %v", err)
	}
	fallbackSink.mu.Lock()
	fallback := strings.Join(fallbackSink.writes, "")
	fallbackSink.mu.Unlock()

	if len(got) != len(fallback) {
		t.Fatalf("expected planner to match diff-only bytes for this rect-scroll hotspot, got=%d fallback=%d stream=%q", len(got), len(fallback), got)
	}
	screen := replayCursorWriterLineScreen(t, 8, 8, [][]string{previous, next})
	want := replayCursorWriterLineScreen(t, 8, 8, [][]string{next})
	assertScreenEqual(t, screen, want)
}

func TestOutputCursorWriterSideBySideScrollFallsBackWithoutLRMarginGate(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	previous := []string{
		"hdr0|RIGHT|",
		"aaaa|RIGHT|",
		"bbbb|RIGHT|",
		"cccc|RIGHT|",
		"dddd|RIGHT|",
		"eeee|RIGHT|",
		"ffff|RIGHT|",
		"ftr7|RIGHT|",
	}
	next := []string{
		"hdr0|RIGHT|",
		"bbbb|RIGHT|",
		"cccc|RIGHT|",
		"dddd|RIGHT|",
		"eeee|RIGHT|",
		"ffff|RIGHT|",
		"gggg|RIGHT|",
		"ftr7|RIGHT|",
	}

	sink := &cursorWriterProbeTTY{}
	writer := newOutputCursorWriter(sink)
	writer.SetVerticalScrollMode(verticalScrollModeRectsOnly)

	if err := writer.WriteFrameLines(previous, ""); err != nil {
		t.Fatalf("write initial frame: %v", err)
	}
	sink.mu.Lock()
	sink.writes = nil
	sink.mu.Unlock()

	if err := writer.WriteFrameLines(next, ""); err != nil {
		t.Fatalf("write scrolled frame: %v", err)
	}

	sink.mu.Lock()
	got := strings.Join(sink.writes, "")
	sink.mu.Unlock()

	if strings.Contains(got, xansi.SetModeLeftRightMargin) {
		t.Fatalf("expected LR-margin scroll gate to stay off by default, got %q", got)
	}
	if strings.Contains(got, xansi.EraseEntireDisplay) {
		t.Fatalf("expected safe fallback to stay incremental, got %q", got)
	}
	if strings.Contains(got, "|RIGHT|") {
		t.Fatalf("expected fallback row diff not to rewrite the unchanged right pane, got %q", got)
	}
	screen := replayCursorWriterLineScreen(t, 11, 8, [][]string{previous, next})
	want := replayCursorWriterLineScreen(t, 11, 8, [][]string{next})
	assertScreenEqual(t, screen, want)
}

func TestOutputCursorWriterSideBySideScrollRemoteLatencyModeStaysWithinDiffOnlyBudget(t *testing.T) {
	t.Setenv("TERMX_EXPERIMENTAL_LR_SCROLL", "")
	t.Setenv("TERMX_REMOTE_LATENCY", "1")
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	previous := []string{
		"hdr0AAAA",
		"aaaa1111",
		"bbbb2222",
		"cccc3333",
		"dddd4444",
		"eeee5555",
		"ffff6666",
		"ftr7ZZZZ",
	}
	next := []string{
		"hdr0AAAA",
		"bbbb1111",
		"cccc2222",
		"dddd3333",
		"eeee4444",
		"ffff5555",
		"gggg6666",
		"ftr7ZZZZ",
	}

	sink := &cursorWriterProbeTTY{}
	writer := newOutputCursorWriter(sink)
	writer.SetVerticalScrollMode(verticalScrollModeRectsOnly)

	if err := writer.WriteFrameLines(previous, ""); err != nil {
		t.Fatalf("write initial frame: %v", err)
	}
	sink.mu.Lock()
	sink.writes = nil
	sink.mu.Unlock()
	perftrace.Reset()

	if err := writer.WriteFrameLines(next, ""); err != nil {
		t.Fatalf("write scrolled frame: %v", err)
	}

	sink.mu.Lock()
	got := strings.Join(sink.writes, "")
	sink.mu.Unlock()

	fallbackSink := &cursorWriterProbeTTY{}
	fallbackWriter := newOutputCursorWriter(fallbackSink)
	fallbackWriter.SetVerticalScrollMode(verticalScrollModeNone)
	if err := fallbackWriter.WriteFrameLines(previous, ""); err != nil {
		t.Fatalf("write fallback initial frame: %v", err)
	}
	fallbackSink.mu.Lock()
	fallbackSink.writes = nil
	fallbackSink.mu.Unlock()
	if err := fallbackWriter.WriteFrameLines(next, ""); err != nil {
		t.Fatalf("write fallback scrolled frame: %v", err)
	}
	fallbackSink.mu.Lock()
	fallback := strings.Join(fallbackSink.writes, "")
	fallbackSink.mu.Unlock()

	if len(got) != len(fallback) {
		t.Fatalf("expected remote planner path to match diff-only bytes for this rect-scroll hotspot, got=%d fallback=%d stream=%q", len(got), len(fallback), got)
	}
	screen := replayCursorWriterLineScreen(t, 8, 8, [][]string{previous, next})
	want := replayCursorWriterLineScreen(t, 8, 8, [][]string{next})
	assertScreenEqual(t, screen, want)
}

func TestOutputCursorWriterPrefersRowScrollWhenRowsAreAllowed(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()
	perftrace.Enable()
	perftrace.Reset()
	defer perftrace.Disable()

	previous := []string{
		"hdr0AAAA",
		"aaaa1111",
		"bbbb2222",
		"cccc3333",
		"dddd4444",
		"eeee5555",
		"ffff6666",
		"ftr7ZZZZ",
	}
	next := []string{
		"hdr0AAAA",
		"bbbb2222",
		"cccc3333",
		"dddd4444",
		"eeee5555",
		"ffff6666",
		"gggg7777",
		"ftr7ZZZZ",
	}

	sink := &cursorWriterProbeTTY{}
	writer := newOutputCursorWriter(sink)
	writer.SetVerticalScrollMode(verticalScrollModeRowsAndRects)

	if err := writer.WriteFrameLines(previous, ""); err != nil {
		t.Fatalf("write initial frame: %v", err)
	}
	sink.mu.Lock()
	sink.writes = nil
	sink.mu.Unlock()
	perftrace.Reset()

	if err := writer.WriteFrameLines(next, ""); err != nil {
		t.Fatalf("write scrolled frame: %v", err)
	}

	sink.mu.Lock()
	got := strings.Join(sink.writes, "")
	sink.mu.Unlock()

	if strings.Contains(got, xansi.SetModeLeftRightMargin) {
		t.Fatalf("expected full-width scroll to avoid rect-scroll margin mode, got %q", got)
	}
	if !strings.Contains(got, "\x1b[1S") {
		t.Fatalf("expected row scroll optimization to emit SU, got %q", got)
	}
	snapshot := perftrace.SnapshotCurrent()
	if event, ok := snapshot.Event("cursor_writer.present.mode.vertical_scroll_rows"); !ok || event.Count == 0 {
		t.Fatalf("expected rows scroll perf event, got %#v", snapshot.Events)
	}
	if event, ok := snapshot.Event("cursor_writer.present.mode.vertical_scroll_rect"); ok && event.Count > 0 {
		t.Fatalf("expected rect scroll path to stay idle when row scroll already matched, got %#v", snapshot.Events)
	}
}

func TestOutputCursorWriterKeepsRowScrollOptimizationWhenCursorMoves(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()
	perftrace.Enable()
	perftrace.Reset()
	defer perftrace.Disable()

	previous, next := benchmarkCursorWriterScrollFrames(120, 40)
	cursorA := "\x1b[10;20H"
	cursorB := "\x1b[11;20H"

	sink := &cursorWriterProbeTTY{}
	writer := newOutputCursorWriter(sink)
	writer.SetVerticalScrollMode(verticalScrollModeRowsAndRects)

	if err := writer.WriteFrameLines(previous, cursorA); err != nil {
		t.Fatalf("write initial frame: %v", err)
	}
	sink.mu.Lock()
	sink.writes = nil
	sink.mu.Unlock()
	perftrace.Reset()

	if err := writer.WriteFrameLines(next, cursorB); err != nil {
		t.Fatalf("write scrolled frame: %v", err)
	}

	sink.mu.Lock()
	got := strings.Join(sink.writes, "")
	sink.mu.Unlock()

	if !strings.Contains(got, "\x1b[1S") {
		t.Fatalf("expected row scroll optimization to stay active when cursor moves, got %q", got)
	}
	snapshot := perftrace.SnapshotCurrent()
	if event, ok := snapshot.Event("cursor_writer.present.mode.vertical_scroll_rows"); !ok || event.Count == 0 {
		t.Fatalf("expected rows scroll perf event when cursor moves, got %#v", snapshot.Events)
	}
	if event, ok := snapshot.Event("cursor_writer.present.mode.fast_scroll_candidate"); !ok || event.Count == 0 {
		t.Fatalf("expected fast scroll candidate event when cursor moves, got %#v", snapshot.Events)
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

func TestNormalizedFrameLenCountsInsertedCRBytes(t *testing.T) {
	frame := "ab\ncd\n"
	if got, want := normalizedFrameLen(frame), len("ab\ncd\n")+2; got != want {
		t.Fatalf("unexpected normalized frame len %d want %d", got, want)
	}
}

func TestNormalizedLinesLenCountsLineBreakSeparators(t *testing.T) {
	lines := []string{"ab", "cde", ""}
	if got, want := normalizedLinesLen(lines), len("ab")+len("cde")+len("")+2; got != want {
		t.Fatalf("unexpected normalized lines len %d want %d", got, want)
	}
}

func TestFitFrameToTTYTruncatesOnlyWhenTTYWidthChanges(t *testing.T) {
	ptmx, tty, err := creackpty.Open()
	if err != nil {
		t.Fatalf("open pty: %v", err)
	}
	defer ptmx.Close()
	defer tty.Close()
	if err := creackpty.Setsize(ptmx, &creackpty.Winsize{Cols: 4, Rows: 10}); err != nil {
		t.Fatalf("set pty size: %v", err)
	}

	writer := newOutputCursorWriter(io.Discard)
	writer.tty = tty

	if got, want := writer.fitFrameToTTY("123456\nabcdef"), "1234\nabcd"; got != want {
		t.Fatalf("unexpected truncated frame %q want %q", got, want)
	}
	if got, want := writer.fitFrameToTTY("wxyz123"), "wxyz123"; got != want {
		t.Fatalf("expected cached width fast path to keep frame unchanged, got %q want %q", got, want)
	}
}

func TestFitLinesToTTYTruncatesOnlyWhenTTYWidthChanges(t *testing.T) {
	ptmx, tty, err := creackpty.Open()
	if err != nil {
		t.Fatalf("open pty: %v", err)
	}
	defer ptmx.Close()
	defer tty.Close()
	if err := creackpty.Setsize(ptmx, &creackpty.Winsize{Cols: 4, Rows: 10}); err != nil {
		t.Fatalf("set pty size: %v", err)
	}

	writer := newOutputCursorWriter(io.Discard)
	writer.tty = tty

	got := writer.fitLinesToTTY([]string{"123456", "abcdef"})
	want := []string{"1234", "abcd"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("unexpected truncated lines %q want %q", got, want)
	}
	got = writer.fitLinesToTTY([]string{"wxyz123", "longer"})
	want = []string{"wxyz123", "longer"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("expected cached width fast path to keep lines unchanged, got %q want %q", got, want)
	}
}

func TestStripTrailingEraseLineRightDropsTerminalResetOnlyOnLastLine(t *testing.T) {
	lines := []string{"a\x1b[0m\x1b[K", "b\x1b[0m\x1b[K"}
	got := stripTrailingEraseLineRight(lines)
	want := []string{"a\x1b[0m", "b\x1b[0m"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("unexpected stripped lines %q want %q", got, want)
	}
}

func TestStripLeadingCHA1DropsRedundantLineAnchors(t *testing.T) {
	lines := []string{xansi.CHA(1) + "abc", "\x1b[Gdef", "ghi"}
	got := stripLeadingCHA1(lines)
	want := []string{"abc", "def", "ghi"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("unexpected stripped lines %q want %q", got, want)
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

func TestShouldFallbackToFullRepaintRequiresNearFullAndBroadDamage(t *testing.T) {
	if shouldFallbackToFullRepaint(strings.Repeat("x", 90), 100, 20, 2) {
		t.Fatal("expected sparse damage to avoid full repaint fallback")
	}
	if !shouldFallbackToFullRepaint(strings.Repeat("x", 96), 100, 20, 16) {
		t.Fatal("expected near-full broad damage to fall back to full repaint")
	}
}

func TestPresentedStyleWithSGRASCIIParsesSimpleAttributes(t *testing.T) {
	style, ok := (presentedStyle{}).withSGRASCII("31;44;1")
	if !ok {
		t.Fatal("expected sgr parse success")
	}
	if style.FGCode != "31" || style.BGCode != "44" || !style.Bold {
		t.Fatalf("unexpected parsed style %#v", style)
	}
}

func TestParsePresentedRowASCIICapturesStyledEraseCells(t *testing.T) {
	row, ok := parsePresentedRowASCII("\x1b[31mA\x1b[2X\x1b[0m")
	if !ok {
		t.Fatal("expected ascii row parse success")
	}
	if !row.hasStyled || !row.hasErase {
		t.Fatalf("expected styled erase row flags, got %#v", row)
	}
	if len(row.cells) != 3 {
		t.Fatalf("expected one glyph and two erase cells, got %#v", row.cells)
	}
	if row.cells[0].Content != "A" || row.cells[0].Style.FGCode != "31" {
		t.Fatalf("unexpected styled leading cell %#v", row.cells[0])
	}
	if !row.cells[1].Erase || !row.cells[2].Erase {
		t.Fatalf("expected trailing erase cells, got %#v", row.cells)
	}
}

func TestParsePresentedRowGenericMarksWideCells(t *testing.T) {
	row := parsePresentedRow("界")
	if !row.hasWide {
		t.Fatalf("expected wide-row marker, got %#v", row)
	}
	if len(row.cells) != 1 || row.cells[0].Width != 2 {
		t.Fatalf("expected one wide cell, got %#v", row.cells)
	}
}

func TestParsePresentedRowGenericCapturesReanchorBeforeNextCell(t *testing.T) {
	row := parsePresentedRow("é" + xansi.CHA(2) + "X")
	if len(row.cells) != 2 {
		t.Fatalf("expected two cells, got %#v", row.cells)
	}
	if !row.hasHostWidthStabilizer {
		t.Fatalf("expected ambiguous-width row to be marked as host-width-stabilized, got %#v", row)
	}
	if row.cells[0].ReanchorBefore {
		t.Fatalf("expected first cell not to carry a re-anchor flag, got %#v", row.cells[0])
	}
	if !row.cells[1].ReanchorBefore {
		t.Fatalf("expected second cell to preserve the compositor's explicit re-anchor, got %#v", row.cells[1])
	}
}

func TestParsePresentedRowGenericDoesNotOvermarkOrdinaryReanchor(t *testing.T) {
	row := parsePresentedRow("A" + xansi.CHA(2) + "B")
	if row.hasHostWidthStabilizer {
		t.Fatalf("expected ordinary row re-anchor not to be treated as a host-width stabilizer, got %#v", row)
	}
}

func TestRowOwnsLineEndDetectsEraseLineSuffix(t *testing.T) {
	if !rowOwnsLineEnd(presentedRow{raw: "body\x1b[K"}) {
		t.Fatal("expected erase-line suffix to claim line end")
	}
	if rowOwnsLineEnd(presentedRow{raw: "body"}) {
		t.Fatal("expected plain row not to claim line end")
	}
}

func TestWriteOwnedLineEndClearPreservesStyleAndResets(t *testing.T) {
	var out strings.Builder
	style := presentedStyle{FGCode: "31"}
	writeOwnedLineEndClear(&out, style)
	want := presentedStyleDiffANSI(presentedStyle{}, style) + "\x1b[1X" + presentedResetStyleSequence
	if got := out.String(); got != want {
		t.Fatalf("unexpected owned-line-end clear %q want %q", got, want)
	}
}

func TestWritePresentedCellsEmitsStyledEraseAndReset(t *testing.T) {
	var out strings.Builder
	style := presentedStyle{FGCode: "31"}
	cells := []presentedCell{
		{Content: "A", Width: 1, Style: style},
		{Content: " ", Width: 1, Style: style, Erase: true},
	}
	finalStyle := writePresentedCells(&out, cells, 1)
	if finalStyle != style {
		t.Fatalf("expected final style %#v, got %#v", style, finalStyle)
	}
	want := presentedStyleDiffANSI(presentedStyle{}, style) + "A" + "\x1b[1X" + presentedResetStyleSequence
	if got := out.String(); got != want {
		t.Fatalf("unexpected presented cell payload %q want %q", got, want)
	}
}

func TestWritePresentedCellsPreservesExplicitReanchorBeforeFollowingCell(t *testing.T) {
	var out strings.Builder
	cells := []presentedCell{
		{Content: "é", Width: 1},
		{Content: "X", Width: 1, ReanchorBefore: true},
	}
	writePresentedCells(&out, cells, 1)
	if got, want := out.String(), "é"+xansi.CHA(2)+"X"; got != want {
		t.Fatalf("unexpected presented cell payload %q want %q", got, want)
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
	_ = model.workbench.SetTabScrollOffset(tab.ID, 1)
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

func TestOutputCursorWriterFrameLinesPathStackedPanesUseVerticalScrollOptimization(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()
	dumpPath := filepath.Join(t.TempDir(), "stacked-scroll.dump")
	t.Setenv("TERMX_FRAME_DUMP", dumpPath)

	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "top", TerminalID: "term-top"},
				"pane-2": {ID: "pane-2", Title: "bottom", TerminalID: "term-bottom"},
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
	topTerm := rt.Registry().GetOrCreate("term-top")
	topTerm.Name = "top"
	topTerm.State = "running"
	topTerm.Channel = 1
	topTerm.Snapshot = cursorWriterDenseTextSnapshot("term-top", 118, 14, 1)
	topBinding := rt.BindPane("pane-1")
	topBinding.Channel = 1
	topBinding.Connected = true

	bottomTerm := rt.Registry().GetOrCreate("term-bottom")
	bottomTerm.Name = "bottom"
	bottomTerm.State = "running"
	bottomTerm.Channel = 2
	bottomTerm.Snapshot = cursorWriterDenseTextSnapshot("term-bottom", 118, 14, 101)
	bottomBinding := rt.BindPane("pane-2")
	bottomBinding.Channel = 2
	bottomBinding.Connected = true

	model := New(shared.Config{}, wb, rt)
	model.width = 120
	model.height = 36

	sink := &cursorWriterProbeTTY{}
	writer := newOutputCursorWriter(sink)
	model.SetFrameWriter(writer)
	model.SetCursorWriter(writer)

	model.render.Invalidate()
	_ = model.View()

	sink.mu.Lock()
	sink.writes = nil
	sink.mu.Unlock()

	topTerm.Snapshot = cursorWriterDenseTextSnapshot("term-top", 118, 14, 2)
	touchRuntimeVisibleStateForTest(model.runtime, 1)
	model.render.Invalidate()
	_ = model.View()

	dumpData, err := os.ReadFile(dumpPath)
	if err != nil {
		t.Fatalf("read frame dump: %v", err)
	}
	entries, err := frameaudit.ParseDump(dumpData)
	if err != nil {
		t.Fatalf("parse frame dump: %v", err)
	}
	report, err := frameaudit.AuditEntries(entries, 120, 36)
	if err != nil {
		t.Fatalf("audit frame dump: %v", err)
	}
	if report.Summary.Noops != 0 {
		t.Fatalf("expected no noop frames in stacked scroll audit, got %#v", report.Summary)
	}
	if len(report.Entries) != 2 {
		t.Fatalf("expected exactly 2 dumped frames, got %#v", report.Entries)
	}
	if report.Entries[1].Bytes >= 1024 {
		t.Fatalf("expected stacked scroll delta frame to stay below 1KiB, got %#v", report.Entries[1])
	}
}

func TestDebugOutputCursorWriterLocalScrollbackAudit(t *testing.T) {
	if os.Getenv("TERMX_RUN_SCROLL_AUDIT") != "1" {
		t.Skip("set TERMX_RUN_SCROLL_AUDIT=1 to run local scrollback frame audit")
	}
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	makeSnapshot := func(terminalID string, cols, rows int) *protocol.Snapshot {
		scrollback := make([][]protocol.Cell, 0, rows)
		screen := make([][]protocol.Cell, 0, rows)
		for i := 0; i < rows; i++ {
			scrollback = append(scrollback, repeatProtocolCells(fmt.Sprintf("hist %03d %s", i, strings.Repeat("x", maxInt(0, cols-10))), cols))
			screen = append(screen, repeatProtocolCells(fmt.Sprintf("live %03d %s", i, strings.Repeat("y", maxInt(0, cols-10))), cols))
		}
		return &protocol.Snapshot{
			TerminalID: terminalID,
			Size:       protocol.Size{Cols: uint16(cols), Rows: uint16(rows)},
			Scrollback: scrollback,
			Screen:     protocol.ScreenData{Cells: screen},
			Cursor:     protocol.CursorState{Row: rows - 1, Col: 0, Visible: true},
			Modes:      protocol.TerminalModes{AutoWrap: true},
		}
	}

	runAudit := func(name string, wb *workbench.Workbench, rt *runtime.Runtime) {
		t.Helper()
		model := New(shared.Config{}, wb, rt)
		model.width = 120
		model.height = 36
		sink := &cursorWriterProbeTTY{}
		writer := newOutputCursorWriter(sink)
		model.SetFrameWriter(writer)
		model.SetCursorWriter(writer)

		model.render.Invalidate()
		scrollBeforeLines, _ := model.render.RenderFrameLinesRef()
		_ = model.View()

		sink.mu.Lock()
		sink.writes = nil
		sink.mu.Unlock()

		tab := model.workbench.CurrentTab()
		if tab == nil {
			t.Fatal("expected current tab")
		}
		_ = model.workbench.SetTabScrollOffset(tab.ID, localMouseWheelScrollLines)
		model.render.Invalidate()
		scrollAfterLines, _ := model.render.RenderFrameLinesRef()
		_ = model.View()

		sink.mu.Lock()
		stream := strings.Join(sink.writes, "")
		sink.mu.Unlock()
		plan, ok := detectVerticalScrollPlan(scrollBeforeLines, scrollAfterLines)
		t.Logf("%s detect_plan=%v plan=%#v has_SU=%v has_SD=%v bytes=%d", name, ok, plan, strings.Contains(stream, "\x1b[1S") || strings.Contains(stream, "\x1b[3S"), strings.Contains(stream, "\x1b[1T") || strings.Contains(stream, "\x1b[3T"), len(stream))
	}

	t.Run("single-pane", func(t *testing.T) {
		wb := workbench.NewWorkbench()
		wb.AddWorkspace("main", &workbench.WorkspaceState{
			Name:      "main",
			ActiveTab: 0,
			Tabs: []*workbench.TabState{{
				ID:           "tab-1",
				Name:         "tab 1",
				ActivePaneID: "pane-1",
				Panes: map[string]*workbench.PaneState{
					"pane-1": {ID: "pane-1", Title: "shell", TerminalID: "term-1"},
				},
				Root: workbench.NewLeaf("pane-1"),
			}},
		})
		rt := runtime.New(&recordingBridgeClient{snapshotByTerminal: map[string]*protocol.Snapshot{}})
		term := rt.Registry().GetOrCreate("term-1")
		term.Name = "shell"
		term.State = "running"
		term.Channel = 1
		term.Snapshot = makeSnapshot("term-1", 118, 30)
		binding := rt.BindPane("pane-1")
		binding.Channel = 1
		binding.Connected = true
		runAudit("single-pane", wb, rt)
	})

	t.Run("split-pane", func(t *testing.T) {
		wb := workbench.NewWorkbench()
		wb.AddWorkspace("main", &workbench.WorkspaceState{
			Name:      "main",
			ActiveTab: 0,
			Tabs: []*workbench.TabState{{
				ID:           "tab-1",
				Name:         "tab 1",
				ActivePaneID: "pane-1",
				Panes: map[string]*workbench.PaneState{
					"pane-1": {ID: "pane-1", Title: "left", TerminalID: "term-1"},
					"pane-2": {ID: "pane-2", Title: "right", TerminalID: "term-2"},
				},
				Root: &workbench.LayoutNode{
					Direction: workbench.SplitVertical,
					Ratio:     0.5,
					First:     workbench.NewLeaf("pane-1"),
					Second:    workbench.NewLeaf("pane-2"),
				},
			}},
		})
		rt := runtime.New(&recordingBridgeClient{snapshotByTerminal: map[string]*protocol.Snapshot{}})
		left := rt.Registry().GetOrCreate("term-1")
		left.Name = "left"
		left.State = "running"
		left.Channel = 1
		left.Snapshot = makeSnapshot("term-1", 57, 30)
		right := rt.Registry().GetOrCreate("term-2")
		right.Name = "right"
		right.State = "running"
		right.Channel = 2
		right.Snapshot = makeSnapshot("term-2", 57, 30)
		leftBinding := rt.BindPane("pane-1")
		leftBinding.Channel = 1
		leftBinding.Connected = true
		rightBinding := rt.BindPane("pane-2")
		rightBinding.Channel = 2
		rightBinding.Connected = true
		runAudit("split-pane", wb, rt)
	})
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

func TestOutputCursorWriterFrameLinesPathSplitDragShrinkThenGrowPreservesAltScreenBlankTail(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	buildModel := func() *Model {
		t.Helper()
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
		return model
	}

	captureHostScreen := func(stream string) localvterm.ScreenData {
		t.Helper()
		vt := localvterm.New(120, 36, 0, nil)
		if _, err := vt.Write([]byte(stream)); err != nil {
			t.Fatalf("replay stream into host vterm: %v", err)
		}
		return vt.ScreenContent()
	}

	captureDragScreen := func(model *Model) (localvterm.ScreenData, renderFrameLines) {
		t.Helper()
		sink := &cursorWriterProbeTTY{}
		writer := newOutputCursorWriter(sink)
		model.SetFrameWriter(writer)
		model.SetCursorWriter(writer)

		model.render.Invalidate()
		_ = model.View()

		events := []tea.MouseMsg{
			{
				X:      59,
				Y:      screenYForBodyY(model, 10),
				Button: tea.MouseButtonLeft,
				Action: tea.MouseActionPress,
			},
			{
				X:      49,
				Y:      screenYForBodyY(model, 10),
				Button: tea.MouseButtonLeft,
				Action: tea.MouseActionMotion,
			},
			{
				X:      59,
				Y:      screenYForBodyY(model, 10),
				Button: tea.MouseButtonLeft,
				Action: tea.MouseActionMotion,
			},
			{
				X:      59,
				Y:      screenYForBodyY(model, 10),
				Button: tea.MouseButtonLeft,
				Action: tea.MouseActionRelease,
			},
		}
		for _, event := range events {
			next, cmd := model.Update(event)
			model = next.(*Model)
			drainCmd(t, model, cmd, 20)
			model.render.Invalidate()
			_ = model.View()
		}

		sink.mu.Lock()
		stream := strings.Join(sink.writes, "")
		sink.mu.Unlock()
		return captureHostScreen(stream), captureRenderFrameLines(t, model)
	}

	captureStaticScreen := func(frame renderFrameLines) localvterm.ScreenData {
		t.Helper()
		sink := &cursorWriterProbeTTY{}
		writer := newOutputCursorWriter(sink)
		if err := writer.WriteFrameLinesWithMeta(frame.lines, "", frame.meta); err != nil {
			t.Fatalf("write final frame lines: %v", err)
		}
		sink.mu.Lock()
		stream := strings.Join(sink.writes, "")
		sink.mu.Unlock()
		return captureHostScreen(stream)
	}

	got, finalFrame := captureDragScreen(buildModel())
	want := captureStaticScreen(finalFrame)
	assertScreenEqual(t, got, want)
}

func TestOutputCursorWriterFrameLinesPathSplitDragShrinkPreviewPreservesAltScreenBlankTail(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	buildModel := func() *Model {
		t.Helper()
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
		return model
	}

	captureHostScreen := func(stream string) localvterm.ScreenData {
		t.Helper()
		vt := localvterm.New(120, 36, 0, nil)
		if _, err := vt.Write([]byte(stream)); err != nil {
			t.Fatalf("replay stream into host vterm: %v", err)
		}
		return vt.ScreenContent()
	}

	model := buildModel()
	sink := &cursorWriterProbeTTY{}
	writer := newOutputCursorWriter(sink)
	model.SetFrameWriter(writer)
	model.SetCursorWriter(writer)

	model.render.Invalidate()
	_ = model.View()

	events := []tea.MouseMsg{
		{
			X:      59,
			Y:      screenYForBodyY(model, 10),
			Button: tea.MouseButtonLeft,
			Action: tea.MouseActionPress,
		},
		{
			X:      49,
			Y:      screenYForBodyY(model, 10),
			Button: tea.MouseButtonLeft,
			Action: tea.MouseActionMotion,
		},
	}
	for _, event := range events {
		next, cmd := model.Update(event)
		model = next.(*Model)
		drainCmd(t, model, cmd, 20)
		model.render.Invalidate()
		_ = model.View()
	}

	sink.mu.Lock()
	stream := strings.Join(sink.writes, "")
	sink.mu.Unlock()
	got := captureHostScreen(stream)
	finalFrame := captureRenderFrameLines(t, model)
	sink = &cursorWriterProbeTTY{}
	writer = newOutputCursorWriter(sink)
	if err := writer.WriteFrameLinesWithMeta(finalFrame.lines, "", finalFrame.meta); err != nil {
		t.Fatalf("write final shrink frame lines: %v", err)
	}
	sink.mu.Lock()
	wantStream := strings.Join(sink.writes, "")
	sink.mu.Unlock()
	want := captureHostScreen(wantStream)
	assertScreenEqual(t, got, want)
}

func TestOutputCursorWriterFrameLinesPathLiveSplitDragHighlightedBlankTailPreservesBackground(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	buildHighlightedScreen := func(cols, rows int) localvterm.ScreenData {
		screen := make([][]localvterm.Cell, rows)
		for y := 0; y < rows; y++ {
			screen[y] = make([]localvterm.Cell, cols)
			lineBG := ""
			if y == 10 {
				lineBG = "#3a3a3a"
			}
			label := ""
			if y == 10 {
				label = "cursor line"
			} else if y == 0 {
				label = "header"
			}
			for x := 0; x < cols; x++ {
				cell := localvterm.Cell{Content: " ", Width: 1, Style: localvterm.CellStyle{BG: lineBG}}
				if x < len(label) {
					cell.Content = string(label[x])
					cell.Style.FG = "#ffffff"
				}
				screen[y][x] = cell
			}
		}
		return localvterm.ScreenData{
			Cells:             screen,
			IsAlternateScreen: true,
		}
	}

	buildModel := func() *Model {
		t.Helper()
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
		vt := localvterm.New(58, 30, 100, nil)
		vt.LoadSnapshot(
			buildHighlightedScreen(58, 30),
			localvterm.CursorState{Row: 10, Col: len("cursor line"), Visible: true},
			localvterm.TerminalModes{AlternateScreen: true, MouseTracking: true, BracketedPaste: true},
		)
		nvimTerm.VTerm = vt
		nvimTerm.SurfaceVersion = 1
		nvimTerm.Snapshot = snapshotFromScreenData("term-nvim", buildHighlightedScreen(58, 30), localvterm.CursorState{Row: 10, Col: len("cursor line"), Visible: true})
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
		return model
	}

	captureHostScreen := func(stream string) localvterm.ScreenData {
		t.Helper()
		vt := localvterm.New(120, 36, 0, nil)
		if _, err := vt.Write([]byte(stream)); err != nil {
			t.Fatalf("replay stream into host vterm: %v", err)
		}
		return vt.ScreenContent()
	}

	captureDragScreen := func(model *Model) (localvterm.ScreenData, renderFrameLines) {
		t.Helper()
		sink := &cursorWriterProbeTTY{}
		writer := newOutputCursorWriter(sink)
		model.SetFrameWriter(writer)
		model.SetCursorWriter(writer)

		model.render.Invalidate()
		_ = model.View()

		events := []tea.MouseMsg{
			{X: 59, Y: screenYForBodyY(model, 10), Button: tea.MouseButtonLeft, Action: tea.MouseActionPress},
			{X: 49, Y: screenYForBodyY(model, 10), Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion},
			{X: 59, Y: screenYForBodyY(model, 10), Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion},
			{X: 59, Y: screenYForBodyY(model, 10), Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease},
		}
		for _, event := range events {
			next, cmd := model.Update(event)
			model = next.(*Model)
			drainCmd(t, model, cmd, 20)
			model.render.Invalidate()
			_ = model.View()
		}

		sink.mu.Lock()
		stream := strings.Join(sink.writes, "")
		sink.mu.Unlock()
		return captureHostScreen(stream), captureRenderFrameLines(t, model)
	}

	got, finalFrame := captureDragScreen(buildModel())
	sink := &cursorWriterProbeTTY{}
	writer := newOutputCursorWriter(sink)
	if err := writer.WriteFrameLinesWithMeta(finalFrame.lines, "", finalFrame.meta); err != nil {
		t.Fatalf("write final live frame lines: %v", err)
	}
	sink.mu.Lock()
	wantStream := strings.Join(sink.writes, "")
	sink.mu.Unlock()
	want := captureHostScreen(wantStream)
	assertScreenEqual(t, got, want)
}

func TestOutputCursorWriterFrameLinesPathLiveSplitDragHighlightedBlankTailKeepsPreviewEdgeBackground(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	buildHighlightedScreen := func(cols, rows int) localvterm.ScreenData {
		screen := make([][]localvterm.Cell, rows)
		for y := 0; y < rows; y++ {
			screen[y] = make([]localvterm.Cell, cols)
			lineBG := ""
			if y == 10 {
				lineBG = "#3a3a3a"
			}
			label := ""
			if y == 10 {
				label = "cursor line"
			} else if y == 0 {
				label = "header"
			}
			for x := 0; x < cols; x++ {
				cell := localvterm.Cell{Content: " ", Width: 1, Style: localvterm.CellStyle{BG: lineBG}}
				if x < len(label) {
					cell.Content = string(label[x])
					cell.Style.FG = "#ffffff"
				}
				screen[y][x] = cell
			}
		}
		return localvterm.ScreenData{
			Cells:             screen,
			IsAlternateScreen: true,
		}
	}

	buildModel := func() *Model {
		t.Helper()
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
		vt := localvterm.New(58, 30, 100, nil)
		vt.LoadSnapshot(
			buildHighlightedScreen(58, 30),
			localvterm.CursorState{Row: 10, Col: len("cursor line"), Visible: true},
			localvterm.TerminalModes{AlternateScreen: true, MouseTracking: true, BracketedPaste: true},
		)
		nvimTerm.VTerm = vt
		nvimTerm.SurfaceVersion = 1
		nvimTerm.Snapshot = snapshotFromScreenData("term-nvim", buildHighlightedScreen(58, 30), localvterm.CursorState{Row: 10, Col: len("cursor line"), Visible: true})
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
		return model
	}

	model := buildModel()
	sink := &cursorWriterProbeTTY{}
	writer := newOutputCursorWriter(sink)
	model.SetFrameWriter(writer)
	model.SetCursorWriter(writer)

	model.render.Invalidate()
	_ = model.View()

	events := []tea.MouseMsg{
		{X: 59, Y: screenYForBodyY(model, 10), Button: tea.MouseButtonLeft, Action: tea.MouseActionPress},
		{X: 49, Y: screenYForBodyY(model, 10), Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion},
	}
	for _, event := range events {
		next, cmd := model.Update(event)
		model = next.(*Model)
		drainCmd(t, model, cmd, 20)
		model.render.Invalidate()
		_ = model.View()
	}

	sink.mu.Lock()
	stream := strings.Join(sink.writes, "")
	sink.mu.Unlock()
	host := localvterm.New(120, 36, 0, nil)
	if _, err := host.Write([]byte(stream)); err != nil {
		t.Fatalf("replay preview frame into host vterm: %v", err)
	}

	visible := model.workbench.VisibleWithSize(model.bodyRect())
	if visible == nil || visible.ActiveTab < 0 || visible.ActiveTab >= len(visible.Tabs) {
		t.Fatalf("expected visible workbench, got %#v", visible)
	}
	var pane workbench.VisiblePane
	found := false
	for _, candidate := range visible.Tabs[visible.ActiveTab].Panes {
		if candidate.ID == "pane-1" {
			pane = candidate
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected visible pane-1 after split drag preview")
	}
	contentRect, ok := paneContentRectForVisible(pane)
	if !ok {
		t.Fatalf("expected content rect for pane-1, got %#v", pane)
	}
	targetX := contentRect.X + maxInt(0, contentRect.W-2)
	targetY := screenYForBodyY(model, contentRect.Y+10)
	screen := host.ScreenContent()
	if targetY < 0 || targetY >= len(screen.Cells) || targetX < 0 || targetX >= len(screen.Cells[targetY]) {
		t.Fatalf("target cell out of bounds x=%d y=%d", targetX, targetY)
	}
	if got := screen.Cells[targetY][targetX].Style.BG; got != "#3a3a3a" {
		t.Fatalf("expected preview edge cell to keep highlighted bg, got %#v at x=%d y=%d", screen.Cells[targetY][targetX], targetX, targetY)
	}
}

func TestOutputCursorWriterFrameLinesPathMovingFloatingPanePreservesUnderlyingExtentHints(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	baseSnapshot := func(terminalID string) *protocol.Snapshot {
		return &protocol.Snapshot{
			TerminalID: terminalID,
			Size:       protocol.Size{Cols: 18, Rows: 4},
			Screen: protocol.ScreenData{Cells: [][]protocol.Cell{
				repeatProtocolCells("top line", 18),
				repeatProtocolCells("mid", 18),
				repeatProtocolCells("bot", 18),
				repeatProtocolCells("$", 18),
			}},
			Cursor: protocol.CursorState{Row: 3, Col: 1, Visible: true},
			Modes:  protocol.TerminalModes{AutoWrap: true},
		}
	}

	buildModel := func(rect workbench.Rect) *Model {
		t.Helper()
		model := setupModel(t, modelOpts{width: 120, height: 36})
		base := model.runtime.Registry().GetOrCreate("term-1")
		base.Name = "base"
		base.State = "running"
		base.Snapshot = baseSnapshot("term-1")

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
		{X: 24, Y: 7, W: 54, H: 16},
		{X: 30, Y: 7, W: 54, H: 16},
		{X: 36, Y: 7, W: 54, H: 16},
	}

	got := captureScreen(buildModel(start), path)
	want := captureScreen(buildModel(path[len(path)-1]), nil)
	assertScreenEqual(t, got, want)
}

func TestOutputCursorWriterFrameLinesPathMovingSharedFloatingFollowerPreservesExtentHints(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	sharedSnapshot := func(terminalID string) *protocol.Snapshot {
		return &protocol.Snapshot{
			TerminalID: terminalID,
			Size:       protocol.Size{Cols: 18, Rows: 4},
			Screen: protocol.ScreenData{Cells: [][]protocol.Cell{
				repeatProtocolCells("nvim 1.png", 18),
				repeatProtocolCells("nvim 1.png", 18),
				repeatProtocolCells("lozzow@host", 18),
				repeatProtocolCells("$", 18),
			}},
			Cursor: protocol.CursorState{Row: 3, Col: 1, Visible: true},
			Modes:  protocol.TerminalModes{AutoWrap: true},
		}
	}

	buildModel := func(rect workbench.Rect) *Model {
		t.Helper()
		model := setupModel(t, modelOpts{width: 120, height: 36})
		tab := model.workbench.CurrentTab()
		if tab == nil {
			t.Fatal("expected current tab")
		}
		if err := model.workbench.BindPaneTerminal(tab.ID, "pane-1", "term-shared"); err != nil {
			t.Fatalf("bind shared terminal to pane-1: %v", err)
		}
		if err := model.workbench.CreateFloatingPane(tab.ID, "float-1", rect); err != nil {
			t.Fatalf("create floating pane: %v", err)
		}
		if err := model.workbench.BindPaneTerminal(tab.ID, "float-1", "term-shared"); err != nil {
			t.Fatalf("bind shared floating terminal: %v", err)
		}

		terminal := model.runtime.Registry().GetOrCreate("term-shared")
		terminal.Name = "shared"
		terminal.State = "running"
		terminal.Channel = 1
		terminal.OwnerPaneID = "pane-1"
		terminal.BoundPaneIDs = []string{"pane-1", "float-1"}
		terminal.Snapshot = sharedSnapshot("term-shared")

		ownerBinding := model.runtime.BindPane("pane-1")
		ownerBinding.Channel = 1
		ownerBinding.Connected = true
		ownerBinding.Role = runtime.BindingRoleOwner

		followerBinding := model.runtime.BindPane("float-1")
		followerBinding.Channel = 2
		followerBinding.Connected = true
		followerBinding.Role = runtime.BindingRoleFollower
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

	start := workbench.Rect{X: 18, Y: 12, W: 34, H: 10}
	path := []workbench.Rect{
		{X: 26, Y: 12, W: 34, H: 10},
		{X: 34, Y: 12, W: 34, H: 10},
		{X: 42, Y: 12, W: 34, H: 10},
	}

	got := captureScreen(buildModel(start), path)
	want := captureScreen(buildModel(path[len(path)-1]), nil)
	assertScreenEqual(t, got, want)
}

func TestOutputCursorWriterFrameLinesPathSharedFollowerPreservesExtentHintsAfterAltScreenExit(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	nvimSnapshot := cursorWriterNvimLikeSnapshot("term-shared", 18, 10, "#444444")
	shellSnapshot := &protocol.Snapshot{
		TerminalID: "term-shared",
		Size:       protocol.Size{Cols: 18, Rows: 10},
		Screen: protocol.ScreenData{Cells: [][]protocol.Cell{
			repeatProtocolCells("nvim 1.png", 18),
			repeatProtocolCells("nvim 1.png", 18),
			repeatProtocolCells("lozzow@host", 18),
			repeatProtocolCells("$", 18),
		}},
		Cursor: protocol.CursorState{Row: 3, Col: 1, Visible: true},
		Modes:  protocol.TerminalModes{AutoWrap: true},
	}

	buildModel := func(snapshot *protocol.Snapshot) *Model {
		t.Helper()
		model := setupModel(t, modelOpts{width: 120, height: 36})
		tab := model.workbench.CurrentTab()
		if tab == nil {
			t.Fatal("expected current tab")
		}
		if err := model.workbench.BindPaneTerminal(tab.ID, "pane-1", "term-shared"); err != nil {
			t.Fatalf("bind shared terminal to pane-1: %v", err)
		}
		if err := model.workbench.CreateFloatingPane(tab.ID, "float-1", workbench.Rect{X: 18, Y: 12, W: 34, H: 10}); err != nil {
			t.Fatalf("create floating pane: %v", err)
		}
		if err := model.workbench.BindPaneTerminal(tab.ID, "float-1", "term-shared"); err != nil {
			t.Fatalf("bind shared floating terminal: %v", err)
		}

		terminal := model.runtime.Registry().GetOrCreate("term-shared")
		terminal.Name = "shared"
		terminal.State = "running"
		terminal.Channel = 1
		terminal.OwnerPaneID = "float-1"
		terminal.BoundPaneIDs = []string{"pane-1", "float-1"}
		terminal.Snapshot = snapshot

		followerBinding := model.runtime.BindPane("pane-1")
		followerBinding.Channel = 1
		followerBinding.Connected = true
		followerBinding.Role = runtime.BindingRoleFollower

		ownerBinding := model.runtime.BindPane("float-1")
		ownerBinding.Channel = 2
		ownerBinding.Connected = true
		ownerBinding.Role = runtime.BindingRoleOwner
		return model
	}

	captureScreen := func(model *Model, nextSnapshot *protocol.Snapshot) localvterm.ScreenData {
		t.Helper()
		sink := &cursorWriterProbeTTY{}
		writer := newOutputCursorWriter(sink)
		model.SetFrameWriter(writer)
		model.SetCursorWriter(writer)

		model.render.Invalidate()
		_ = model.View()

		if nextSnapshot != nil {
			terminal := model.runtime.Registry().Get("term-shared")
			if terminal == nil {
				t.Fatal("expected shared terminal")
			}
			terminal.Snapshot = nextSnapshot
			touchRuntimeVisibleStateForTest(model.runtime, 7)
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

	got := captureScreen(buildModel(nvimSnapshot), shellSnapshot)
	want := captureScreen(buildModel(shellSnapshot), nil)
	assertScreenEqual(t, got, want)
}

func TestOutputCursorWriterFrameLinesPathSharedFollowerPreservesExtentHintsAfterVTermAltScreenExit(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	const exitPayload = "\x1b[?1049l\x1b[?25h\x1b[?1002l\x1b[Hnvim 1.png\r\nnvim 1.png\r\nlozzow@host\r\n$ "

	buildModel := func(applyExit bool) *Model {
		t.Helper()
		client := &recordingBridgeClient{
			snapshotByTerminal: map[string]*protocol.Snapshot{
				"term-shared": cursorWriterNvimLikeSnapshot("term-shared", 18, 10, "#444444"),
			},
		}
		model := setupModel(t, modelOpts{client: client, width: 120, height: 36})
		tab := model.workbench.CurrentTab()
		if tab == nil {
			t.Fatal("expected current tab")
		}
		if err := model.workbench.BindPaneTerminal(tab.ID, "pane-1", "term-shared"); err != nil {
			t.Fatalf("bind shared terminal to pane-1: %v", err)
		}
		if err := model.workbench.CreateFloatingPane(tab.ID, "float-1", workbench.Rect{X: 18, Y: 12, W: 34, H: 10}); err != nil {
			t.Fatalf("create floating pane: %v", err)
		}
		if err := model.workbench.BindPaneTerminal(tab.ID, "float-1", "term-shared"); err != nil {
			t.Fatalf("bind shared floating terminal: %v", err)
		}

		terminal := model.runtime.Registry().GetOrCreate("term-shared")
		terminal.Name = "shared"
		terminal.State = "running"
		terminal.Channel = 1
		terminal.OwnerPaneID = "float-1"
		terminal.BoundPaneIDs = []string{"pane-1", "float-1"}

		followerBinding := model.runtime.BindPane("pane-1")
		followerBinding.Channel = 1
		followerBinding.Connected = true
		followerBinding.Role = runtime.BindingRoleFollower

		ownerBinding := model.runtime.BindPane("float-1")
		ownerBinding.Channel = 2
		ownerBinding.Connected = true
		ownerBinding.Role = runtime.BindingRoleOwner

		if _, err := model.runtime.LoadSnapshot(context.Background(), "term-shared", 0, 10); err != nil {
			t.Fatalf("load snapshot: %v", err)
		}
		if applyExit {
			if terminal.VTerm == nil {
				t.Fatal("expected hydrated vterm")
			}
			if _, err := terminal.VTerm.Write([]byte(exitPayload)); err != nil {
				t.Fatalf("apply alt-screen exit payload: %v", err)
			}
			if !model.runtime.RefreshSnapshotFromVTerm("term-shared") {
				t.Fatal("expected refresh snapshot from vterm")
			}
			touchRuntimeVisibleStateForTest(model.runtime, 9)
		}
		return model
	}

	captureScreen := func(model *Model, applyExitAfterFirstView bool) localvterm.ScreenData {
		t.Helper()
		sink := &cursorWriterProbeTTY{}
		writer := newOutputCursorWriter(sink)
		model.SetFrameWriter(writer)
		model.SetCursorWriter(writer)

		model.render.Invalidate()
		_ = model.View()

		if applyExitAfterFirstView {
			terminal := model.runtime.Registry().Get("term-shared")
			if terminal == nil || terminal.VTerm == nil {
				t.Fatal("expected shared terminal with vterm")
			}
			if _, err := terminal.VTerm.Write([]byte(exitPayload)); err != nil {
				t.Fatalf("apply alt-screen exit payload: %v", err)
			}
			if !model.runtime.RefreshSnapshotFromVTerm("term-shared") {
				t.Fatal("expected refresh snapshot from vterm")
			}
			touchRuntimeVisibleStateForTest(model.runtime, 10)
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

	got := captureScreen(buildModel(false), true)
	want := captureScreen(buildModel(true), false)
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

func TestOutputCursorWriterFrameLinesPathSideBySideNvimScrollMatchesFinalScreen(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	buildModel := func(start int) *Model {
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
					"pane-2": {ID: "pane-2", Title: "notes", TerminalID: "term-notes"},
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
		nvimTerm.Snapshot = cursorWriterNvimScrollingSnapshot("term-nvim", 58, 30, start, "#444444")
		nvimBinding := rt.BindPane("pane-1")
		nvimBinding.Channel = 1
		nvimBinding.Connected = true

		notesTerm := rt.Registry().GetOrCreate("term-notes")
		notesTerm.Name = "notes"
		notesTerm.State = "running"
		notesTerm.Channel = 2
		notesTerm.Snapshot = cursorWriterDenseTextSnapshot("term-notes", 58, 30, 91)
		notesBinding := rt.BindPane("pane-2")
		notesBinding.Channel = 2
		notesBinding.Connected = true

		model := New(shared.Config{}, wb, rt)
		model.width = 120
		model.height = 36
		return model
	}

	captureFrames := func(model *Model, starts []int) ([]renderFrameLines, localvterm.ScreenData) {
		t.Helper()
		frames := make([]renderFrameLines, 0, len(starts)+1)
		frames = append(frames, captureRenderFrameLines(t, model))
		terminal := model.runtime.Registry().Get("term-nvim")
		if terminal == nil {
			t.Fatal("expected nvim terminal")
		}
		for _, start := range starts {
			terminal.Snapshot = cursorWriterNvimScrollingSnapshot("term-nvim", 58, 30, start, "#444444")
			touchRuntimeVisibleStateForTest(model.runtime, uint8(start))
			frames = append(frames, captureRenderFrameLines(t, model))
		}
		return frames, replayCursorWriterRenderFrames(t, 120, 36, frames)
	}

	gotFrames, got := captureFrames(buildModel(0), []int{1, 2, 3, 4, 5, 6, 7, 8})
	if len(gotFrames) == 0 {
		t.Fatal("expected captured frames")
	}
	wantFrames, want := captureFrames(buildModel(8), nil)
	if len(wantFrames) != 1 {
		t.Fatalf("expected one final frame, got %d", len(wantFrames))
	}
	assertScreenEqual(t, got, want)
}

func TestOutputCursorWriterForceFullFrameLinesBypassesRowDiff(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	sink := &cursorWriterProbeTTY{}
	writer := newOutputCursorWriter(sink)

	initial := []string{"alpha", "bravo"}
	next := []string{"alpHZ", "bravo"}
	if err := writer.WriteFrameLinesWithMeta(initial, "", nil); err != nil {
		t.Fatalf("write initial frame lines: %v", err)
	}
	sink.mu.Lock()
	sink.writes = nil
	sink.mu.Unlock()

	writer.SetForceFullFrameLines(true)
	if err := writer.WriteFrameLinesWithMeta(next, "", nil); err != nil {
		t.Fatalf("write conservative full frame lines: %v", err)
	}

	sink.mu.Lock()
	got := strings.Join(sink.writes, "")
	sink.mu.Unlock()

	if !strings.Contains(got, "alpHZ") || !strings.Contains(got, "bravo") {
		t.Fatalf("expected forced conservative repaint to include full frame rows, got %q", got)
	}
	screen := replayCursorWriterLineScreen(t, 5, 2, [][]string{initial, next})
	want := replayCursorWriterLineScreen(t, 5, 2, [][]string{next})
	assertScreenEqual(t, screen, want)
}

func TestOutputCursorWriterFrameLinesPathSideBySideNvimScrollMatchesEachTmuxStep(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()
	if testing.Short() {
		t.Skip("tmux-backed writer parity is skipped with -short")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}

	buildModel := func(start int) *Model {
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
					"pane-2": {ID: "pane-2", Title: "notes", TerminalID: "term-notes"},
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
		nvimTerm.Snapshot = cursorWriterNvimScrollingSnapshot("term-nvim", 58, 30, start, "#444444")
		nvimBinding := rt.BindPane("pane-1")
		nvimBinding.Channel = 1
		nvimBinding.Connected = true

		notesTerm := rt.Registry().GetOrCreate("term-notes")
		notesTerm.Name = "notes"
		notesTerm.State = "running"
		notesTerm.Channel = 2
		notesTerm.Snapshot = cursorWriterDenseTextSnapshot("term-notes", 58, 30, 91)
		notesBinding := rt.BindPane("pane-2")
		notesBinding.Channel = 2
		notesBinding.Connected = true

		model := New(shared.Config{}, wb, rt)
		model.width = 120
		model.height = 36
		return model
	}

	frames := func(model *Model, starts []int) []renderFrameLines {
		t.Helper()
		out := make([]renderFrameLines, 0, len(starts)+1)
		out = append(out, captureRenderFrameLines(t, model))
		terminal := model.runtime.Registry().Get("term-nvim")
		if terminal == nil {
			t.Fatal("expected nvim terminal")
		}
		for _, start := range starts {
			terminal.Snapshot = cursorWriterNvimScrollingSnapshot("term-nvim", 58, 30, start, "#444444")
			touchRuntimeVisibleStateForTest(model.runtime, uint8(start))
			out = append(out, captureRenderFrameLines(t, model))
		}
		return out
	}

	actualFrames := frames(buildModel(0), []int{1, 2, 3, 4, 5, 6, 7, 8})
	actualSink := &cursorWriterProbeTTY{}
	actualWriter := newOutputCursorWriter(actualSink)
	actualTmux := startTmuxReplayHarness(t, 120, 36)

	lastWrite := 0
	for i, frame := range actualFrames {
		if err := actualWriter.WriteFrameLinesWithMeta(frame.lines, "", frame.meta); err != nil {
			t.Fatalf("write actual frame %d: %v", i, err)
		}
		actualSink.mu.Lock()
		stream := strings.Join(actualSink.writes[lastWrite:], "")
		lastWrite = len(actualSink.writes)
		actualSink.mu.Unlock()
		actualTmux.Append(t, stream)
		got := actualTmux.Capture(t)

		expectedSink := &cursorWriterProbeTTY{}
		expectedWriter := newOutputCursorWriter(expectedSink)
		if err := expectedWriter.WriteFrameLinesWithMeta(frame.lines, "", frame.meta); err != nil {
			t.Fatalf("write expected frame %d: %v", i, err)
		}
		expectedSink.mu.Lock()
		expectedStream := strings.Join(expectedSink.writes, "")
		expectedSink.mu.Unlock()
		expectedTmux := startTmuxReplayHarness(t, 120, 36)
		expectedTmux.Append(t, expectedStream)
		want := expectedTmux.Capture(t)
		expectedTmux.Close()

		if len(got) != len(want) {
			t.Fatalf("frame %d: tmux height mismatch got=%d want=%d", i, len(got), len(want))
		}
		for row := range want {
			if got[row] == want[row] {
				continue
			}
			t.Fatalf("frame %d row %d diverged\nactual=%q\nexpect=%q\nstream=%q",
				i, row+1, got[row], want[row], debugEscape(stream, 320))
		}
	}
}

func TestModelViewSideBySideNvimScrollMatchesEachTmuxStep(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()
	if testing.Short() {
		t.Skip("tmux-backed writer parity is skipped with -short")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}

	buildModel := func(start int) *Model {
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
					"pane-2": {ID: "pane-2", Title: "notes", TerminalID: "term-notes"},
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
		nvimTerm.Snapshot = cursorWriterNvimScrollingSnapshot("term-nvim", 58, 30, start, "#444444")
		nvimBinding := rt.BindPane("pane-1")
		nvimBinding.Channel = 1
		nvimBinding.Connected = true

		notesTerm := rt.Registry().GetOrCreate("term-notes")
		notesTerm.Name = "notes"
		notesTerm.State = "running"
		notesTerm.Channel = 2
		notesTerm.Snapshot = cursorWriterDenseTextSnapshot("term-notes", 58, 30, 91)
		notesBinding := rt.BindPane("pane-2")
		notesBinding.Channel = 2
		notesBinding.Connected = true

		model := New(shared.Config{}, wb, rt)
		model.width = 120
		model.height = 36
		return model
	}

	actualSink := &cursorWriterProbeTTY{}
	actualWriter := newOutputCursorWriter(actualSink)
	actualModel := buildModel(0)
	actualModel.SetFrameWriter(actualWriter)
	actualModel.SetCursorWriter(actualWriter)
	actualTmux := startTmuxReplayHarness(t, 120, 36)

	steps := []int{0, 1, 2, 3, 4, 5, 6, 7, 8}
	lastWrite := 0
	for i, start := range steps {
		if i > 0 {
			terminal := actualModel.runtime.Registry().Get("term-nvim")
			if terminal == nil {
				t.Fatal("expected nvim terminal")
			}
			terminal.Snapshot = cursorWriterNvimScrollingSnapshot("term-nvim", 58, 30, start, "#444444")
			touchRuntimeVisibleStateForTest(actualModel.runtime, uint8(start))
		}
		actualModel.render.Invalidate()
		_ = actualModel.View()

		actualSink.mu.Lock()
		stream := strings.Join(actualSink.writes[lastWrite:], "")
		lastWrite = len(actualSink.writes)
		actualSink.mu.Unlock()
		actualTmux.Append(t, stream)
		got := actualTmux.Capture(t)

		expectedSink := &cursorWriterProbeTTY{}
		expectedWriter := newOutputCursorWriter(expectedSink)
		expectedModel := buildModel(start)
		expectedModel.SetFrameWriter(expectedWriter)
		expectedModel.SetCursorWriter(expectedWriter)
		expectedModel.render.Invalidate()
		_ = expectedModel.View()
		expectedSink.mu.Lock()
		expectedStream := strings.Join(expectedSink.writes, "")
		expectedSink.mu.Unlock()
		expectedTmux := startTmuxReplayHarness(t, 120, 36)
		expectedTmux.Append(t, expectedStream)
		want := expectedTmux.Capture(t)
		expectedTmux.Close()

		if len(got) != len(want) {
			t.Fatalf("frame %d: tmux height mismatch got=%d want=%d", i, len(got), len(want))
		}
		for row := range want {
			if got[row] == want[row] {
				continue
			}
			t.Fatalf("frame %d row %d diverged\nactual=%q\nexpect=%q\nstream=%q",
				i, row+1, got[row], want[row], debugEscape(stream, 320))
		}
	}
}

func TestModelViewSinglePaneNvimScrollMatchesEachTmuxStep(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()
	if testing.Short() {
		t.Skip("tmux-backed writer parity is skipped with -short")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}

	buildModel := func(start int) *Model {
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
				},
				Root: workbench.NewLeaf("pane-1"),
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
		nvimTerm.Snapshot = cursorWriterNvimScrollingSnapshot("term-nvim", 117, 30, start, "#444444")
		nvimBinding := rt.BindPane("pane-1")
		nvimBinding.Channel = 1
		nvimBinding.Connected = true

		model := New(shared.Config{}, wb, rt)
		model.width = 120
		model.height = 36
		return model
	}

	actualSink := &cursorWriterProbeTTY{}
	actualWriter := newOutputCursorWriter(actualSink)
	actualModel := buildModel(0)
	actualModel.SetFrameWriter(actualWriter)
	actualModel.SetCursorWriter(actualWriter)
	actualTmux := startTmuxReplayHarness(t, 120, 36)

	steps := []int{0, 1, 2, 3, 4, 5, 6, 7, 8}
	lastWrite := 0
	for i, start := range steps {
		if i > 0 {
			terminal := actualModel.runtime.Registry().Get("term-nvim")
			if terminal == nil {
				t.Fatal("expected nvim terminal")
			}
			terminal.Snapshot = cursorWriterNvimScrollingSnapshot("term-nvim", 117, 30, start, "#444444")
			touchRuntimeVisibleStateForTest(actualModel.runtime, uint8(start))
		}
		actualModel.render.Invalidate()
		_ = actualModel.View()

		actualSink.mu.Lock()
		stream := strings.Join(actualSink.writes[lastWrite:], "")
		lastWrite = len(actualSink.writes)
		actualSink.mu.Unlock()
		actualTmux.Append(t, stream)
		got := actualTmux.Capture(t)

		expectedSink := &cursorWriterProbeTTY{}
		expectedWriter := newOutputCursorWriter(expectedSink)
		expectedModel := buildModel(start)
		expectedModel.SetFrameWriter(expectedWriter)
		expectedModel.SetCursorWriter(expectedWriter)
		expectedModel.render.Invalidate()
		_ = expectedModel.View()
		expectedSink.mu.Lock()
		expectedStream := strings.Join(expectedSink.writes, "")
		expectedSink.mu.Unlock()
		expectedTmux := startTmuxReplayHarness(t, 120, 36)
		expectedTmux.Append(t, expectedStream)
		want := expectedTmux.Capture(t)
		expectedTmux.Close()

		if len(got) != len(want) {
			t.Fatalf("frame %d: tmux height mismatch got=%d want=%d", i, len(got), len(want))
		}
		for row := range want {
			if got[row] == want[row] {
				continue
			}
			t.Fatalf("frame %d row %d diverged\nactual=%q\nexpect=%q\nstream=%q",
				i, row+1, got[row], want[row], debugEscape(stream, 320))
		}
	}
}

func TestOutputCursorWriterFrameLinesPathSharedFloatingNvimScrollMatchesFinalScreen(t *testing.T) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	for _, ownerPaneID := range []string{"pane-1", "float-1"} {
		t.Run(ownerPaneID, func(t *testing.T) {
			buildModel := func(start int) *Model {
				t.Helper()
				model := setupModel(t, modelOpts{width: 120, height: 36})
				tab := model.workbench.CurrentTab()
				if tab == nil {
					t.Fatal("expected current tab")
				}
				if err := model.workbench.BindPaneTerminal(tab.ID, "pane-1", "term-shared"); err != nil {
					t.Fatalf("bind shared terminal to pane-1: %v", err)
				}
				if err := model.workbench.CreateFloatingPane(tab.ID, "float-1", workbench.Rect{X: 18, Y: 12, W: 34, H: 10}); err != nil {
					t.Fatalf("create floating pane: %v", err)
				}
				if err := model.workbench.BindPaneTerminal(tab.ID, "float-1", "term-shared"); err != nil {
					t.Fatalf("bind shared floating terminal: %v", err)
				}

				terminal := model.runtime.Registry().GetOrCreate("term-shared")
				terminal.Name = "shared"
				terminal.State = "running"
				terminal.Channel = 1
				terminal.OwnerPaneID = ownerPaneID
				terminal.BoundPaneIDs = []string{"pane-1", "float-1"}
				terminal.Snapshot = cursorWriterNvimScrollingSnapshot("term-shared", 18, 10, start, "#444444")

				ownerBinding := model.runtime.BindPane(ownerPaneID)
				ownerBinding.Channel = 1
				ownerBinding.Connected = true
				ownerBinding.Role = runtime.BindingRoleOwner

				followerID := "pane-1"
				if ownerPaneID == "pane-1" {
					followerID = "float-1"
				}
				followerBinding := model.runtime.BindPane(followerID)
				followerBinding.Channel = 2
				followerBinding.Connected = true
				followerBinding.Role = runtime.BindingRoleFollower
				return model
			}

			captureFrames := func(model *Model, starts []int) ([]renderFrameLines, localvterm.ScreenData) {
				t.Helper()
				frames := make([]renderFrameLines, 0, len(starts)+1)
				frames = append(frames, captureRenderFrameLines(t, model))
				terminal := model.runtime.Registry().Get("term-shared")
				if terminal == nil {
					t.Fatal("expected shared terminal")
				}
				for _, start := range starts {
					terminal.Snapshot = cursorWriterNvimScrollingSnapshot("term-shared", 18, 10, start, "#444444")
					touchRuntimeVisibleStateForTest(model.runtime, uint8(start))
					frames = append(frames, captureRenderFrameLines(t, model))
				}
				return frames, replayCursorWriterRenderFrames(t, 120, 36, frames)
			}

			gotFrames, got := captureFrames(buildModel(0), []int{1, 2, 3, 4, 5, 6, 7, 8})
			if len(gotFrames) == 0 {
				t.Fatal("expected captured frames")
			}
			wantFrames, want := captureFrames(buildModel(8), nil)
			if len(wantFrames) != 1 {
				t.Fatalf("expected one final frame, got %d", len(wantFrames))
			}
			assertScreenEqual(t, got, want)
		})
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

func repeatProtocolCells(text string, cols int) []protocol.Cell {
	if cols <= 0 {
		return nil
	}
	row := make([]protocol.Cell, 0, cols)
	for i := 0; i < cols; i++ {
		cell := protocol.Cell{Content: " ", Width: 1}
		if i < len(text) {
			cell.Content = string(text[i])
		}
		row = append(row, cell)
	}
	return row
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

func cursorWriterNvimScrollingSnapshot(terminalID string, cols, rows, start int, bg string) *protocol.Snapshot {
	if cols <= 0 {
		cols = 1
	}
	if rows <= 0 {
		rows = 1
	}
	screen := make([][]protocol.Cell, 0, rows)
	for y := 0; y < rows; y++ {
		label := "line " + strconv.Itoa(start+y+1)
		row := make([]protocol.Cell, 0, cols)
		for x := 0; x < cols; x++ {
			cell := protocol.Cell{
				Content: " ",
				Width:   1,
				Style:   protocol.CellStyle{BG: bg},
			}
			switch {
			case y == 0 && x < len(label):
				cell.Content = string(label[x])
				cell.Style.FG = "#ffffff"
			case y > 0 && x == 0:
				cell.Content = "~"
				cell.Style.FG = "#4f5258"
			case y == rows-1 && x > cols-12 && x < cols-3:
				cell.Content = string("SCROLL"[minInt(5, x-(cols-11))])
				cell.Style.FG = "#94a3b8"
			}
			row = append(row, cell)
		}
		screen = append(screen, row)
	}
	return &protocol.Snapshot{
		TerminalID: terminalID,
		Size:       protocol.Size{Cols: uint16(cols), Rows: uint16(rows)},
		Screen:     protocol.ScreenData{Cells: screen},
		Cursor:     protocol.CursorState{Row: minInt(rows-1, 1), Col: minInt(cols-1, 8), Visible: true},
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

func snapshotFromScreenData(terminalID string, screen localvterm.ScreenData, cursor localvterm.CursorState) *protocol.Snapshot {
	rows := len(screen.Cells)
	cols := 0
	if rows > 0 {
		cols = len(screen.Cells[0])
	}
	protocolRows := make([][]protocol.Cell, rows)
	for y := range screen.Cells {
		protocolRows[y] = make([]protocol.Cell, len(screen.Cells[y]))
		for x, cell := range screen.Cells[y] {
			protocolRows[y][x] = protocol.Cell{
				Content: cell.Content,
				Width:   cell.Width,
				Style: protocol.CellStyle{
					FG:            cell.Style.FG,
					BG:            cell.Style.BG,
					Bold:          cell.Style.Bold,
					Italic:        cell.Style.Italic,
					Underline:     cell.Style.Underline,
					Blink:         cell.Style.Blink,
					Reverse:       cell.Style.Reverse,
					Strikethrough: cell.Style.Strikethrough,
				},
			}
		}
	}
	return &protocol.Snapshot{
		TerminalID: terminalID,
		Size:       protocol.Size{Cols: uint16(cols), Rows: uint16(rows)},
		Screen: protocol.ScreenData{
			Cells:             protocolRows,
			IsAlternateScreen: screen.IsAlternateScreen,
		},
		Cursor: protocol.CursorState{
			Row:     cursor.Row,
			Col:     cursor.Col,
			Visible: cursor.Visible,
			Shape:   string(cursor.Shape),
			Blink:   cursor.Blink,
		},
		Modes: protocol.TerminalModes{
			AlternateScreen: screen.IsAlternateScreen,
			MouseTracking:   true,
			BracketedPaste:  true,
		},
	}
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

func replayCursorWriterLineScreen(t *testing.T, width, height int, frames [][]string) localvterm.ScreenData {
	t.Helper()
	sink := &cursorWriterProbeTTY{}
	writer := newOutputCursorWriter(sink)
	for _, frame := range frames {
		if err := writer.WriteFrameLines(frame, ""); err != nil {
			t.Fatalf("write frame lines: %v", err)
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

func replayCursorWriterLineScreenWithMeta(t *testing.T, width, height int, frames [][]string, metas []*presentMeta) localvterm.ScreenData {
	t.Helper()
	if len(frames) != len(metas) {
		t.Fatalf("frames/meta length mismatch: %d vs %d", len(frames), len(metas))
	}
	sink := &cursorWriterProbeTTY{}
	writer := newOutputCursorWriter(sink)
	for i, frame := range frames {
		if err := writer.WriteFrameLinesWithMeta(frame, "", metas[i]); err != nil {
			t.Fatalf("write frame lines with meta: %v", err)
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

type renderFrameLines struct {
	lines []string
	meta  *presentMeta
}

func captureRenderFrameLines(t *testing.T, model *Model) renderFrameLines {
	t.Helper()
	model.render.Invalidate()
	result := model.render.Render()
	return renderFrameLines{
		lines: append([]string(nil), result.Lines...),
		meta:  presentMetaFromRender(result.Meta),
	}
}

func replayCursorWriterRenderFrames(t *testing.T, width, height int, frames []renderFrameLines) localvterm.ScreenData {
	t.Helper()
	sink := &cursorWriterProbeTTY{}
	writer := newOutputCursorWriter(sink)
	for _, frame := range frames {
		if err := writer.WriteFrameLinesWithMeta(frame.lines, "", frame.meta); err != nil {
			t.Fatalf("write render frame lines with meta: %v", err)
		}
	}
	sink.mu.Lock()
	stream := strings.Join(sink.writes, "")
	sink.mu.Unlock()
	vt := localvterm.New(width, height, 0, nil)
	if _, err := vt.Write([]byte(stream)); err != nil {
		t.Fatalf("replay render frame stream: %v", err)
	}
	return vt.ScreenContent()
}

func ownerAwareTestMeta(rows ...[]hostOwnerID) *presentMeta {
	meta := &presentMeta{
		OwnerMap: make([][]hostOwnerID, len(rows)),
	}
	for y := range rows {
		meta.OwnerMap[y] = append([]hostOwnerID(nil), rows[y]...)
	}
	meta.VisibleRects = visibleRectsFromOwnerMap(meta.OwnerMap)
	return meta
}

type cursorWriterFakeHost struct {
	width  int
	height int
	cells  [][]string
}

func newCursorWriterFakeHost(width, height int) *cursorWriterFakeHost {
	cells := make([][]string, height)
	for y := 0; y < height; y++ {
		cells[y] = make([]string, width)
		for x := 0; x < width; x++ {
			cells[y][x] = " "
		}
	}
	return &cursorWriterFakeHost{width: width, height: height, cells: cells}
}

func cursorWriterFakeHostUsesAmbiguousWidth(content string, width int) bool {
	if !shared.IsHostWidthAmbiguousCluster(content, width) {
		return false
	}
	if shared.IsStableNarrowTerminalSymbol(content) {
		return false
	}
	return true
}

func replayCursorWriterFakeHost(t *testing.T, width, height int, writes []string, ambiguousWidth int) *cursorWriterFakeHost {
	t.Helper()
	host := newCursorWriterFakeHost(width, height)
	for _, write := range writes {
		host.apply(write, ambiguousWidth)
	}
	return host
}

type cursorWriterTextCluster struct {
	Content string
	Width   int
}

func cursorWriterSplitTextClusters(s string) []cursorWriterTextCluster {
	graphemes := uniseg.NewGraphemes(s)
	out := make([]cursorWriterTextCluster, 0, len(s))
	lastBase := -1
	for graphemes.Next() {
		cluster := graphemes.Str()
		width := xansi.StringWidth(cluster)
		if width <= 0 {
			if lastBase >= 0 {
				out[lastBase].Content += cluster
				continue
			}
			width = 1
		}
		out = append(out, cursorWriterTextCluster{Content: cluster, Width: width})
		lastBase = len(out) - 1
	}
	return out
}

func (h *cursorWriterFakeHost) apply(frame string, ambiguousWidth int) {
	if h == nil {
		return
	}
	row, col := 0, 0
	for i := 0; i < len(frame); {
		switch frame[i] {
		case '\x1b':
			consumed, nextRow, nextCol := cursorWriterConsumeFakeHostEscape(h, frame[i:], row, col)
			if consumed <= 0 {
				i++
				continue
			}
			i += consumed
			row, col = nextRow, nextCol
		case '\r':
			col = 0
			i++
		case '\n':
			row++
			col = 0
			i++
		default:
			clusters := cursorWriterSplitTextClusters(frame[i:])
			if len(clusters) == 0 {
				i++
				continue
			}
			cluster := clusters[0]
			if esc := strings.IndexByte(cluster.Content, '\x1b'); esc >= 0 {
				if esc == 0 {
					i++
					continue
				}
				cluster.Content = cluster.Content[:esc]
				cluster.Width = xansi.StringWidth(cluster.Content)
			}
			width := cluster.Width
			ambiguous := cursorWriterFakeHostUsesAmbiguousWidth(cluster.Content, cluster.Width)
			if ambiguous {
				width = ambiguousWidth
			}
			if !ambiguous && width <= 0 {
				width = maxInt(1, xansi.StringWidth(cluster.Content))
			}
			h.put(row, col, cluster.Content)
			for step := 1; step < width; step++ {
				h.put(row, col+step, "")
			}
			col += width
			i += len(cluster.Content)
		}
	}
}

func cursorWriterConsumeFakeHostEscape(host *cursorWriterFakeHost, src string, row, col int) (int, int, int) {
	if len(src) < 2 || src[0] != '\x1b' || src[1] != '[' {
		return 0, row, col
	}
	i := 2
	for i < len(src) {
		b := src[i]
		if b >= 0x40 && b <= 0x7e {
			params := src[2:i]
			switch b {
			case 'C':
				col += cursorWriterFakeHostFirstParam(params, 1)
			case 'G':
				col = maxInt(0, cursorWriterFakeHostFirstParam(params, 1)-1)
			case 'H':
				parts := strings.Split(strings.TrimPrefix(params, "?"), ";")
				if len(parts) >= 1 {
					row = maxInt(0, cursorWriterFakeHostParseParam(parts[0], 1)-1)
				}
				if len(parts) >= 2 {
					col = maxInt(0, cursorWriterFakeHostParseParam(parts[1], 1)-1)
				}
			case 'X':
				count := cursorWriterFakeHostFirstParam(params, 1)
				for step := 0; step < count; step++ {
					host.put(row, col+step, " ")
				}
			}
			return i + 1, row, col
		}
		i++
	}
	return 0, row, col
}

func cursorWriterFakeHostFirstParam(params string, fallback int) int {
	params = strings.TrimPrefix(params, "?")
	if params == "" {
		return fallback
	}
	if idx := strings.IndexByte(params, ';'); idx >= 0 {
		params = params[:idx]
	}
	return cursorWriterFakeHostParseParam(params, fallback)
}

func cursorWriterFakeHostParseParam(raw string, fallback int) int {
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func (h *cursorWriterFakeHost) put(row, col int, content string) {
	if h == nil || row < 0 || row >= h.height || col < 0 || col >= h.width {
		return
	}
	h.cells[row][col] = content
}

func (h *cursorWriterFakeHost) lines() []string {
	if h == nil {
		return nil
	}
	lines := make([]string, 0, h.height)
	for _, row := range h.cells {
		var b strings.Builder
		for _, cell := range row {
			if cell == "" {
				cell = " "
			}
			b.WriteString(cell)
		}
		lines = append(lines, b.String())
	}
	return lines
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
