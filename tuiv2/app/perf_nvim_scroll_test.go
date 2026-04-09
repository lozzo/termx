package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	xansi "github.com/charmbracelet/x/ansi"
	creackpty "github.com/creack/pty"
	"github.com/lozzow/termx"
	"github.com/lozzow/termx/perftrace"
	"github.com/lozzow/termx/protocol"
	unixtransport "github.com/lozzow/termx/transport/unix"
	"github.com/lozzow/termx/tuiv2/bridge"
	"github.com/lozzow/termx/tuiv2/shared"
)

type nvimScrollPerfReport struct {
	Scenario           string                 `json:"scenario"`
	GeneratedAt        time.Time              `json:"generated_at"`
	InitialOutputBytes int                    `json:"initial_output_bytes"`
	InitialSyncFrames  int                    `json:"initial_sync_frames"`
	Actions            []nvimScrollPerfAction `json:"actions"`
}

type nvimScrollPerfAction struct {
	Label       string             `json:"label"`
	InputBytes  int                `json:"input_bytes"`
	OutputBytes int                `json:"output_bytes"`
	SyncFrames  int                `json:"sync_frames"`
	OriginCount int                `json:"origin_count"`
	ClearCount  int                `json:"clear_count"`
	SettleMs    float64            `json:"settle_ms"`
	Sample      string             `json:"sample"`
	Metrics     perftrace.Snapshot `json:"metrics"`
}

type nvimPerfHarness struct {
	ctx      context.Context
	cancel   context.CancelFunc
	ptmx     *os.File
	recorder *ptyOutputRecorder
	errc     chan error
}

func TestPerfNvimScrollReport(t *testing.T) {
	if os.Getenv("TERMX_RUN_NVIM_TRACE") != "1" {
		t.Skip("set TERMX_RUN_NVIM_TRACE=1 to run the interactive nvim perf trace")
	}
	if testing.Short() {
		t.Skip("debug perf trace")
	}
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed")
	}

	harness := startNvimPerfHarness(t, "perf-nvim-scroll")
	defer harness.Close(t)

	waitForPTYOutputLength(t, harness.ctx, harness.recorder, 3000)
	waitForPTYQuiet(t, harness.ctx, harness.recorder, 300*time.Millisecond)
	harness.moveToMiddle(t)

	report := nvimScrollPerfReport{
		Scenario:           "nvim_scroll_bursts",
		GeneratedAt:        time.Now().UTC(),
		InitialOutputBytes: len(harness.recorder.Text()),
		InitialSyncFrames:  strings.Count(harness.recorder.Text(), synchronizedOutputBegin),
	}

	recorder := perftrace.Enable()
	defer perftrace.Disable()
	recorder.Reset()

	actions := []struct {
		label string
		seq   []byte
	}{
		{label: "down_single", seq: []byte{0x05}},
		{label: "up_single", seq: []byte{0x19}},
		{label: "down_burst_8", seq: bytesRepeat(0x05, 8)},
		{label: "up_burst_8", seq: bytesRepeat(0x19, 8)},
		{label: "alternating_16", seq: alternatingBytes(0x05, 0x19, 8)},
	}

	for _, action := range actions {
		recorder.Reset()
		report.Actions = append(report.Actions, harness.runAction(t, action.label, action.seq, recorder))
	}

	for _, action := range report.Actions {
		update, _ := action.Metrics.Event("app.update")
		renderFrame, _ := action.Metrics.Event("render.frame")
		runtimeVisible, _ := action.Metrics.Event("runtime.visible")
		vtermWrite, _ := action.Metrics.Event("vterm.write")
		directFlush, _ := action.Metrics.Event("cursor_writer.direct_flush")
		t.Logf(
			"%s settle_ms=%.2f out=%d sync=%d update_calls=%d update_ms=%.2f render_calls=%d render_ms=%.2f visible_calls=%d visible_ms=%.2f vterm_calls=%d vterm_ms=%.2f direct_flush_calls=%d direct_flush_ms=%.2f",
			action.Label,
			action.SettleMs,
			action.OutputBytes,
			action.SyncFrames,
			update.Count,
			update.TotalMs,
			renderFrame.Count,
			renderFrame.TotalMs,
			runtimeVisible.Count,
			runtimeVisible.TotalMs,
			vtermWrite.Count,
			vtermWrite.TotalMs,
			directFlush.Count,
			directFlush.TotalMs,
		)
	}

	if outPath := strings.TrimSpace(os.Getenv("TERMX_PERF_OUT")); outPath != "" {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			t.Fatalf("marshal perf report: %v", err)
		}
		if err := os.WriteFile(outPath, data, 0o644); err != nil {
			t.Fatalf("write perf report: %v", err)
		}
		t.Logf("perf report written to %s", outPath)
	}
}

func startNvimPerfHarness(t *testing.T, name string) *nvimPerfHarness {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	socketPath := filepath.Join(t.TempDir(), name+".sock")
	srv := termx.NewServer(termx.WithSocketPath(socketPath))
	srvDone := make(chan error, 1)
	go func() { srvDone <- srv.ListenAndServe(ctx) }()
	t.Cleanup(func() {
		cancel()
		_ = srv.Shutdown(context.Background())
		select {
		case <-srvDone:
		case <-time.After(3 * time.Second):
		}
	})
	if err := waitTestSocket(socketPath, 5*time.Second); err != nil {
		t.Fatalf("server socket never appeared: %v", err)
	}

	ctrlTransport, err := unixtransport.Dial(socketPath)
	if err != nil {
		t.Fatalf("dial control client: %v", err)
	}
	ctrlClient := protocol.NewClient(ctrlTransport)
	if err := ctrlClient.Hello(ctx, protocol.Hello{Version: protocol.Version}); err != nil {
		t.Fatalf("hello control client: %v", err)
	}
	t.Cleanup(func() { _ = ctrlClient.Close() })

	tmpFile := filepath.Join(t.TempDir(), name+".txt")
	var lines []string
	for i := 1; i <= 300; i++ {
		lines = append(lines, fmt.Sprintf("line %03d %s", i, strings.Repeat("x", 40)))
	}
	if err := os.WriteFile(tmpFile, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	created, err := ctrlClient.Create(ctx, protocol.CreateParams{
		Command: []string{
			"nvim",
			"-u", "NONE",
			"-n",
			"-c", "set nomore nonumber norelativenumber laststatus=0 cmdheight=0 noshowmode nowrap",
			tmpFile,
		},
		Name: name,
		Size: protocol.Size{Cols: 120, Rows: 40},
	})
	if err != nil {
		t.Fatalf("create terminal: %v", err)
	}

	appTransport, err := unixtransport.Dial(socketPath)
	if err != nil {
		t.Fatalf("dial app client: %v", err)
	}
	appClient := protocol.NewClient(appTransport)
	if err := appClient.Hello(ctx, protocol.Hello{Version: protocol.Version}); err != nil {
		t.Fatalf("hello app client: %v", err)
	}
	t.Cleanup(func() { _ = appClient.Close() })

	ptmx, tty, err := creackpty.Open()
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted in this environment: %v", err)
		}
		t.Fatalf("open pty: %v", err)
	}
	t.Cleanup(func() {
		_ = ptmx.Close()
		_ = tty.Close()
	})

	if err := creackpty.Setsize(ptmx, &creackpty.Winsize{Cols: 120, Rows: 40}); err != nil {
		t.Fatalf("set pty size: %v", err)
	}

	errc := make(chan error, 1)
	go func() {
		errc <- runWithClientOptions(
			shared.Config{AttachID: created.TerminalID},
			bridge.NewProtocolClient(appClient),
			tty,
			tty,
			tea.WithContext(ctx),
		)
	}()

	outputRecorder := &ptyOutputRecorder{eventc: make(chan struct{}, 1024)}
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				outputRecorder.Append(string(buf[:n]))
			}
			if err != nil {
				return
			}
		}
	}()

	return &nvimPerfHarness{
		ctx:      ctx,
		cancel:   cancel,
		ptmx:     ptmx,
		recorder: outputRecorder,
		errc:     errc,
	}
}

func (h *nvimPerfHarness) runAction(t *testing.T, label string, seq []byte, recorder *perftrace.Recorder) nvimScrollPerfAction {
	t.Helper()
	before := len(h.recorder.Text())
	start := time.Now()
	if _, err := h.ptmx.Write(seq); err != nil {
		t.Fatalf("write %s: %v", label, err)
	}
	waitForPTYGrowthIfAny(t, h.ctx, h.recorder, before, 750*time.Millisecond)
	waitForPTYQuiet(t, h.ctx, h.recorder, 250*time.Millisecond)
	delta := h.recorder.Text()[before:]
	return nvimScrollPerfAction{
		Label:       label,
		InputBytes:  len(seq),
		OutputBytes: len(delta),
		SyncFrames:  strings.Count(delta, synchronizedOutputBegin),
		OriginCount: strings.Count(delta, xansi.MoveCursorOrigin),
		ClearCount:  strings.Count(delta, xansi.EraseEntireDisplay),
		SettleMs:    float64(time.Since(start)) / float64(time.Millisecond),
		Sample:      debugEscape(delta, 220),
		Metrics:     recorder.Snapshot(),
	}
}

func (h *nvimPerfHarness) moveToMiddle(t *testing.T) {
	t.Helper()
	before := len(h.recorder.Text())
	if _, err := h.ptmx.Write([]byte("50G")); err != nil {
		t.Fatalf("write middle-position command: %v", err)
	}
	waitForPTYGrowthIfAny(t, h.ctx, h.recorder, before, 2*time.Second)
	waitForPTYQuiet(t, h.ctx, h.recorder, 250*time.Millisecond)
}

func (h *nvimPerfHarness) Close(t *testing.T) {
	t.Helper()
	if h == nil {
		return
	}
	h.cancel()
	select {
	case err := <-h.errc:
		if err != nil && !errors.Is(err, tea.ErrProgramKilled) {
			t.Fatalf("runWithClientOptions returned unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for TUI shutdown")
	}
}

func waitForPTYGrowthIfAny(t *testing.T, ctx context.Context, recorder *ptyOutputRecorder, before int, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if len(recorder.Text()) > before {
			return true
		}
		select {
		case <-recorder.eventc:
		case <-ctx.Done():
			t.Fatalf("context expired waiting for PTY growth after %d bytes", before)
		case <-time.After(50 * time.Millisecond):
		}
	}
	return len(recorder.Text()) > before
}

func bytesRepeat(value byte, count int) []byte {
	if count <= 0 {
		return nil
	}
	buf := make([]byte, count)
	for i := range buf {
		buf[i] = value
	}
	return buf
}

func alternatingBytes(first, second byte, pairs int) []byte {
	if pairs <= 0 {
		return nil
	}
	buf := make([]byte, 0, pairs*2)
	for i := 0; i < pairs; i++ {
		buf = append(buf, first, second)
	}
	return buf
}
