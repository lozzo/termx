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
	Label               string             `json:"label"`
	InputBytes          int                `json:"input_bytes"`
	OutputBytes         int                `json:"output_bytes"`
	SyncFrames          int                `json:"sync_frames"`
	OriginCount         int                `json:"origin_count"`
	ClearCount          int                `json:"clear_count"`
	FullRepaintCount    uint64             `json:"full_repaint_count"`
	RectScrollCount     uint64             `json:"rect_scroll_count"`
	LRMarginScrollCount uint64             `json:"lr_margin_scroll_count"`
	IntralineEditCount  uint64             `json:"intraline_edit_count"`
	FirstOutput         float64            `json:"first_output_ms"`
	SettleMs            float64            `json:"settle_ms"`
	TraceElapsedMs      float64            `json:"trace_elapsed_ms"`
	Sample              string             `json:"sample"`
	Phases              []nvimScrollPhase  `json:"phases"`
	Counters            []nvimScrollMetric `json:"counters"`
	Metrics             perftrace.Snapshot `json:"metrics"`
}

type nvimScrollPhase struct {
	Name          string  `json:"name"`
	Event         string  `json:"event"`
	Count         uint64  `json:"count"`
	Bytes         uint64  `json:"bytes"`
	TotalMs       float64 `json:"total_ms"`
	ShareOfSettle float64 `json:"share_of_settle"`
	ShareOfTrace  float64 `json:"share_of_trace"`
}

type nvimScrollMetric struct {
	Name  string `json:"name"`
	Event string `json:"event"`
	Count uint64 `json:"count"`
	Value uint64 `json:"value"`
	Unit  string `json:"unit,omitempty"`
}

type nvimPerfEventSpec struct {
	Name  string
	Event string
	Unit  string
}

var nvimPerfPhaseSpecs = []nvimPerfEventSpec{
	{Name: "app_update", Event: "app.update"},
	{Name: "app_view", Event: "app.view"},
	{Name: "server_vterm_total", Event: "vterm.write"},
	{Name: "server_vterm_flush_write", Event: "terminal.pending_vterm_flush.write"},
	{Name: "server_vterm_before_snapshot", Event: "vterm.write.before_snapshot"},
	{Name: "server_vterm_emulator", Event: "vterm.write.emulator"},
	{Name: "server_vterm_reconcile", Event: "vterm.write.reconcile"},
	{Name: "server_encode_total", Event: "terminal.screen_update.from_damage"},
	{Name: "server_encode_damage_state", Event: "terminal.screen_update.from_damage_state"},
	{Name: "server_encode_full_snapshot", Event: "terminal.screen_update.full_snapshot"},
	{Name: "server_encode_payload", Event: "terminal.screen_update.encode"},
	{Name: "server_encode_compare", Event: "terminal.screen_update.strategy_compare"},
	{Name: "runtime_snapshot_apply", Event: "runtime.stream.screen_update.snapshot_apply"},
	{Name: "runtime_load_partial", Event: "runtime.stream.screen_update.load_vterm_partial"},
	{Name: "runtime_load_full", Event: "runtime.stream.screen_update.load_vterm_full"},
	{Name: "runtime_invalidate", Event: "runtime.stream.screen_update.invalidate"},
	{Name: "runtime_visible", Event: "runtime.visible"},
	{Name: "runtime_output_load_vterm", Event: "runtime.stream.output.load_vterm"},
	{Name: "render_body", Event: "render.body"},
	{Name: "render_frame", Event: "render.frame"},
	{Name: "cursor_present", Event: "cursor_writer.present"},
	{Name: "cursor_write_frame", Event: "cursor_writer.write_frame"},
	{Name: "cursor_direct_flush", Event: "cursor_writer.direct_flush"},
}

var nvimPerfCounterSpecs = []nvimPerfEventSpec{
	{Name: "server_encoded_bytes", Event: "terminal.screen_update.encoded_bytes", Unit: "bytes"},
	{Name: "server_delta_payloads", Event: "terminal.screen_update.encode_mode.delta", Unit: "bytes"},
	{Name: "server_full_replace_payloads", Event: "terminal.screen_update.encode_mode.full_replace", Unit: "bytes"},
	{Name: "server_delta_only_shortcut", Event: "terminal.screen_update.delta_only_shortcut"},
	{Name: "server_full_snapshot_required", Event: "terminal.screen_update.requires_full_snapshot"},
	{Name: "runtime_full_replace_apply", Event: "runtime.stream.screen_update.full_replace"},
	{Name: "render_alt_screen_fast_path", Event: "render.body.alt_screen_fast_path", Unit: "cells"},
	{Name: "render_alt_screen_row_cache_hit", Event: "render.body.alt_screen_row_cache.hit", Unit: "rows"},
	{Name: "render_alt_screen_row_cache_miss", Event: "render.body.alt_screen_row_cache.miss", Unit: "rows"},
	{Name: "render_incremental_rows", Event: "render.body.canvas.incremental.rows", Unit: "cells"},
	{Name: "render_full_pane", Event: "render.body.canvas.incremental.full_pane", Unit: "cells"},
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
		appView, _ := action.Metrics.Event("app.view")
		renderFrame, _ := action.Metrics.Event("render.frame")
		renderBody, _ := action.Metrics.Event("render.body")
		runtimeVisible, _ := action.Metrics.Event("runtime.visible")
		vtermWrite, _ := action.Metrics.Event("vterm.write")
		serverEncode, _ := action.Metrics.Event("terminal.screen_update.from_damage")
		serverFullSnapshot, _ := action.Metrics.Event("terminal.screen_update.full_snapshot")
		serverDelta, _ := action.Metrics.Event("terminal.screen_update.encode_mode.delta")
		serverFullReplace, _ := action.Metrics.Event("terminal.screen_update.encode_mode.full_replace")
		cursorPresent, _ := action.Metrics.Event("cursor_writer.present")
		renderIncrementalRows, _ := action.Metrics.Event("render.body.canvas.incremental.rows")
		renderFullPane, _ := action.Metrics.Event("render.body.canvas.incremental.full_pane")
		directFlush, _ := action.Metrics.Event("cursor_writer.direct_flush")
		t.Logf(
			"%s first_output_ms=%.2f settle_ms=%.2f trace_ms=%.2f out=%d sync=%d update_calls=%d update_ms=%.2f app_view_calls=%d app_view_ms=%.2f server_encode_calls=%d server_encode_ms=%.2f server_full_snapshot_calls=%d delta_payloads=%d(%dB) full_replace_payloads=%d(%dB) render_body_calls=%d render_body_ms=%.2f render_frame_calls=%d render_frame_ms=%.2f visible_calls=%d visible_ms=%.2f vterm_calls=%d vterm_ms=%.2f cursor_present_calls=%d cursor_present_ms=%.2f incremental_rows=%d(%d) full_pane=%d(%d) direct_flush_calls=%d direct_flush_ms=%.2f",
			action.Label,
			action.FirstOutput,
			action.SettleMs,
			action.TraceElapsedMs,
			action.OutputBytes,
			action.SyncFrames,
			update.Count,
			update.TotalMs,
			appView.Count,
			appView.TotalMs,
			serverEncode.Count,
			serverEncode.TotalMs,
			serverFullSnapshot.Count,
			serverDelta.Count,
			serverDelta.Bytes,
			serverFullReplace.Count,
			serverFullReplace.Bytes,
			renderBody.Count,
			renderBody.TotalMs,
			renderFrame.Count,
			renderFrame.TotalMs,
			runtimeVisible.Count,
			runtimeVisible.TotalMs,
			vtermWrite.Count,
			vtermWrite.TotalMs,
			cursorPresent.Count,
			cursorPresent.TotalMs,
			renderIncrementalRows.Count,
			renderIncrementalRows.Bytes,
			renderFullPane.Count,
			renderFullPane.Bytes,
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
	firstOutputMs := 0.0
	if firstAt := h.recorder.FirstAppendAfter(before); !firstAt.IsZero() {
		firstOutputMs = float64(firstAt.Sub(start)) / float64(time.Millisecond)
	}
	waitForPTYQuiet(t, h.ctx, h.recorder, 250*time.Millisecond)
	delta := h.recorder.Text()[before:]
	snapshot := recorder.Snapshot()
	settleMs := float64(time.Since(start)) / float64(time.Millisecond)
	return nvimScrollPerfAction{
		Label:               label,
		InputBytes:          len(seq),
		OutputBytes:         len(delta),
		SyncFrames:          strings.Count(delta, synchronizedOutputBegin),
		OriginCount:         strings.Count(delta, xansi.MoveCursorOrigin),
		ClearCount:          strings.Count(delta, xansi.EraseEntireDisplay),
		FullRepaintCount:    perfEventCount(snapshot, "cursor_writer.present.mode.full_repaint_threshold"),
		RectScrollCount:     perfEventCount(snapshot, "cursor_writer.present.mode.delta_rect_scroll_fullwidth"),
		LRMarginScrollCount: perfEventCount(snapshot, "cursor_writer.present.mode.delta_rect_scroll_lr_margin"),
		IntralineEditCount:  perfEventCount(snapshot, "cursor_writer.present.mode.delta_intraline_dch") + perfEventCount(snapshot, "cursor_writer.present.mode.delta_intraline_ich") + perfEventCount(snapshot, "cursor_writer.present.mode.delta_intraline_ech") + perfEventCount(snapshot, "cursor_writer.present.mode.delta_intraline_el"),
		FirstOutput:         firstOutputMs,
		SettleMs:            settleMs,
		TraceElapsedMs:      snapshot.ElapsedMs,
		Sample:              debugEscape(delta, 220),
		Phases:              summarizeNvimPerfPhases(snapshot, settleMs),
		Counters:            summarizeNvimPerfMetrics(snapshot),
		Metrics:             snapshot,
	}
}

func perfEventCount(snapshot perftrace.Snapshot, name string) uint64 {
	event, ok := snapshot.Event(name)
	if !ok {
		return 0
	}
	return event.Count
}

func summarizeNvimPerfPhases(snapshot perftrace.Snapshot, settleMs float64) []nvimScrollPhase {
	phases := make([]nvimScrollPhase, 0, len(nvimPerfPhaseSpecs))
	for _, spec := range nvimPerfPhaseSpecs {
		event, ok := snapshot.Event(spec.Event)
		if !ok {
			continue
		}
		phase := nvimScrollPhase{
			Name:    spec.Name,
			Event:   spec.Event,
			Count:   event.Count,
			Bytes:   event.Bytes,
			TotalMs: event.TotalMs,
		}
		if settleMs > 0 {
			phase.ShareOfSettle = event.TotalMs / settleMs
		}
		if snapshot.ElapsedMs > 0 {
			phase.ShareOfTrace = event.TotalMs / snapshot.ElapsedMs
		}
		phases = append(phases, phase)
	}
	return phases
}

func summarizeNvimPerfMetrics(snapshot perftrace.Snapshot) []nvimScrollMetric {
	metrics := make([]nvimScrollMetric, 0, len(nvimPerfCounterSpecs))
	for _, spec := range nvimPerfCounterSpecs {
		event, ok := snapshot.Event(spec.Event)
		if !ok {
			continue
		}
		metrics = append(metrics, nvimScrollMetric{
			Name:  spec.Name,
			Event: spec.Event,
			Count: event.Count,
			Value: event.Bytes,
			Unit:  spec.Unit,
		})
	}
	return metrics
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
