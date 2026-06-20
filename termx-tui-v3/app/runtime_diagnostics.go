package app

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	goruntime "runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lozzow/termx/termx-tui-v3/render"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

const (
	tuiDiagnosticsEnv        = "TERMX_TUI_DIAG"
	tuiInputTraceEnv         = "TERMX_TUI_INPUT_TRACE"
	tuiDiagnosticsInterval   = "TERMX_TUI_DIAG_INTERVAL_MS"
	tuiHeapProfileDirEnv     = "TERMX_TUI_HEAP_PROFILE_DIR"
	tuiHeapProfileEveryEnv   = "TERMX_TUI_HEAP_PROFILE_EVERY_MB"
	tuiHeapProfileDefaultMB  = uint64(128)
	tuiHeapProfileMinDeltaMB = uint64(8)
)

type runtimeDiagnostics struct {
	mu                sync.Mutex
	logger            *slog.Logger
	enabled           bool
	inputTraceEnabled bool
	interval          time.Duration
	lastLog           time.Time
	frameSeq          uint64
	patchSeq          uint64
	lastFloatingKey   string
	heapProfileDir    string
	heapProfileEvery  uint64
	lastHeapProfileAt uint64
}

func newRuntimeDiagnostics(logger *slog.Logger) *runtimeDiagnostics {
	if logger == nil {
		return nil
	}
	diag := &runtimeDiagnostics{
		logger:   logger,
		enabled:  diagnosticsEnabledFromEnv(tuiDiagnosticsEnv),
		interval: diagnosticsIntervalFromEnv(tuiDiagnosticsInterval, time.Second),
	}
	diag.inputTraceEnabled = diag.enabled || diagnosticsEnabledFromEnv(tuiInputTraceEnv)
	if dir := strings.TrimSpace(os.Getenv(tuiHeapProfileDirEnv)); dir != "" {
		diag.enabled = true
		diag.heapProfileDir = dir
		diag.heapProfileEvery = diagnosticsProfileEveryBytes()
	}
	return diag
}

func (runtime *AppRuntime) observeRuntimeMessage(msg Msg, effects []Effect) {
	if runtime.diagnostics == nil || !runtime.diagnostics.enabled {
		return
	}
	runtime.diagnostics.observeMessage(runtime, msg, effects)
}

func (runtime *AppRuntime) observeRuntimeFrame(frame render.Frame) {
	if runtime.diagnostics == nil || !runtime.diagnostics.enabled {
		return
	}
	runtime.diagnostics.observeFrame(runtime, frame)
}

func (runtime *AppRuntime) observeRuntimePatchFrame(frame render.Frame) {
	if runtime.diagnostics == nil || !runtime.diagnostics.enabled {
		return
	}
	runtime.diagnostics.observePatchFrame(runtime, frame)
}

func (diag *runtimeDiagnostics) observeMessage(runtime *AppRuntime, msg Msg, effects []Effect) {
	now := time.Now()
	force := false
	if key := floatingDiagnosticKey(runtime.state); key != diag.lastFloatingKey {
		diag.lastFloatingKey = key
		force = true
	}
	if !force && now.Sub(diag.lastLog) < diag.interval {
		diag.maybeWriteHeapProfile(runtime.state, "sample")
		return
	}
	diag.lastLog = now
	mem := diagnosticsMemStats()
	diag.logger.Debug("tui-v3 runtime diagnostic",
		"msg", fmt.Sprintf("%T", msg),
		"effects", len(effects),
		"root_generation", runtime.state.Generation,
		"queue_len", runtime.queueLength(),
		"viewport", diagnosticsViewport(runtime.state.Viewport),
		"floatings", len(runtime.state.Shell.ActiveFloatings()),
		"active_floating", runtime.state.Shell.ActiveFloatingID(),
		"copy_mode", runtime.state.CopyMode.Active,
		"copy_view", runtime.state.CopyMode.ViewID,
		"history_rows", len(runtime.state.History.Rows),
		"terminal_views", len(runtime.state.TerminalViews.Views),
		"goroutines", goruntime.NumGoroutine(),
		"heap_alloc", mem.HeapAlloc,
		"heap_sys", mem.HeapSys,
		"heap_objects", mem.HeapObjects,
		"next_gc", mem.NextGC,
		"num_gc", mem.NumGC,
	)
	diag.maybeWriteHeapProfile(runtime.state, "message")
}

func (diag *runtimeDiagnostics) observeFrame(runtime *AppRuntime, frame render.Frame) {
	diag.frameSeq++
	if time.Since(diag.lastLog) < diag.interval {
		diag.maybeWriteHeapProfile(runtime.state, "frame")
		return
	}
	diag.lastLog = time.Now()
	mem := diagnosticsMemStats()
	diag.logger.Debug("tui-v3 frame diagnostic",
		"frame_seq", diag.frameSeq,
		"root_generation", runtime.state.Generation,
		"lines", len(frame.Lines),
		"ansi_lines", len(frame.ANSILines),
		"styled_lines", len(frame.StyledLines),
		"hit_regions", len(frame.HitRegions),
		"frame_bytes", diagnosticsFrameBytes(frame),
		"metadata_width", frame.Metadata.Width,
		"metadata_height", frame.Metadata.Height,
		"queue_len", runtime.queueLength(),
		"heap_alloc", mem.HeapAlloc,
		"heap_sys", mem.HeapSys,
		"heap_objects", mem.HeapObjects,
	)
	diag.maybeWriteHeapProfile(runtime.state, "frame")
}

func (diag *runtimeDiagnostics) observePatchFrame(runtime *AppRuntime, frame render.Frame) {
	diag.patchSeq++
	patch := frame.Patch
	if patch == nil {
		return
	}
	mem := diagnosticsMemStats()
	diag.logger.Debug("tui-v3 copy-history patch diagnostic",
		"patch_seq", diag.patchSeq,
		"root_generation", runtime.state.Generation,
		"rewrite", patch.Rewrite,
		"cursor_only", patch.CursorOnly,
		"rect", diagnosticsRect(patch.Rect),
		"line_x", patch.LineX,
		"line_y", patch.LineY,
		"line_width", patch.LineWidth,
		"line_ansi_bytes", len(patch.LineANSI),
		"lines_ansi", len(patch.LinesANSI),
		"lines_ansi_bytes", diagnosticsStringSliceBytes(patch.LinesANSI),
		"copy_view", runtime.state.CopyMode.ViewID,
		"active_floating", runtime.state.Shell.ActiveFloatingID(),
		"heap_alloc", mem.HeapAlloc,
		"heap_objects", mem.HeapObjects,
	)
	diag.maybeWriteHeapProfile(runtime.state, "patch")
}

func (diag *runtimeDiagnostics) maybeWriteHeapProfile(root state.Root, reason string) {
	if diag.heapProfileDir == "" || diag.heapProfileEvery == 0 {
		return
	}
	mem := diagnosticsMemStats()
	if mem.HeapAlloc < diag.lastHeapProfileAt+diag.heapProfileEvery {
		return
	}
	if diag.lastHeapProfileAt != 0 && mem.HeapAlloc-diag.lastHeapProfileAt < tuiHeapProfileMinDeltaMB*1024*1024 {
		return
	}
	diag.writeHeapProfile(root, reason, mem)
}

func (diag *runtimeDiagnostics) writeHeapProfile(root state.Root, reason string, mem goruntime.MemStats) {
	if diag == nil || diag.heapProfileDir == "" {
		return
	}
	diag.mu.Lock()
	defer diag.mu.Unlock()
	if err := os.MkdirAll(diag.heapProfileDir, 0o755); err != nil {
		diag.logger.Warn("tui-v3 heap profile directory unavailable", "dir", diag.heapProfileDir, "error", err)
		return
	}
	diag.lastHeapProfileAt = mem.HeapAlloc
	path := filepath.Join(diag.heapProfileDir, fmt.Sprintf("termx-tui-v3-%s-gen%d-heap%d.pprof", sanitizeHeapProfileReason(reason), root.Generation, mem.HeapAlloc))
	file, err := os.Create(path)
	if err != nil {
		diag.logger.Warn("tui-v3 heap profile create failed", "path", path, "error", err)
		return
	}
	defer file.Close()
	goruntime.GC()
	if err := pprof.WriteHeapProfile(file); err != nil {
		diag.logger.Warn("tui-v3 heap profile write failed", "path", path, "error", err)
		return
	}
	diag.logger.Info("tui-v3 heap profile written", "path", path, "heap_alloc", mem.HeapAlloc, "reason", reason)
}

func (diag *runtimeDiagnostics) RequestHeapProfile(root state.Root, reason string) {
	if diag == nil || diag.heapProfileDir == "" {
		return
	}
	diag.writeHeapProfile(root, reason, diagnosticsMemStats())
}

func sanitizeHeapProfileReason(reason string) string {
	reason = strings.TrimSpace(strings.ToLower(reason))
	if reason == "" {
		return "sample"
	}
	var builder strings.Builder
	for _, r := range reason {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			builder.WriteRune(r)
		}
	}
	if builder.Len() == 0 {
		return "sample"
	}
	return builder.String()
}

func (runtime *AppRuntime) queueLength() int {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return len(runtime.queue)
}

func diagnosticsEnabledFromEnv(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "on", "yes", "debug":
		return true
	default:
		return false
	}
}

func diagnosticsIntervalFromEnv(name string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return time.Duration(value) * time.Millisecond
}

func diagnosticsProfileEveryBytes() uint64 {
	raw := strings.TrimSpace(os.Getenv(tuiHeapProfileEveryEnv))
	if raw == "" {
		return tuiHeapProfileDefaultMB * 1024 * 1024
	}
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || value == 0 {
		return tuiHeapProfileDefaultMB * 1024 * 1024
	}
	return value * 1024 * 1024
}

func diagnosticsMemStats() goruntime.MemStats {
	var mem goruntime.MemStats
	goruntime.ReadMemStats(&mem)
	return mem
}

func diagnosticsFrameBytes(frame render.Frame) int {
	return diagnosticsStringSliceBytes(frame.Lines) + diagnosticsStringSliceBytes(frame.ANSILines)
}

func diagnosticsStringSliceBytes(values []string) int {
	total := 0
	for _, value := range values {
		total += len(value)
	}
	return total
}

func diagnosticsViewport(viewport state.ViewportStore) string {
	if !viewport.Valid {
		return "invalid"
	}
	return fmt.Sprintf("%dx%d", viewport.Cols, viewport.Rows)
}

func diagnosticsRect(rect render.Rect) string {
	return fmt.Sprintf("%d,%d %dx%d", rect.X, rect.Y, rect.W, rect.H)
}

func floatingDiagnosticKey(root state.Root) string {
	shell := root.Shell.EnsureDefaults()
	var builder strings.Builder
	builder.WriteString(shell.ActiveFloatingID())
	for _, floating := range shell.ActiveFloatings() {
		builder.WriteString("|")
		builder.WriteString(floating.ID)
		builder.WriteString(":")
		builder.WriteString(floating.Pane.ID)
		builder.WriteString(":")
		if binding, ok := root.TerminalViews.FloatingBinding(floating.ID); ok {
			builder.WriteString(binding.TerminalID)
		}
		builder.WriteString(":")
		builder.WriteString(fmt.Sprintf("%d,%d,%d,%d", floating.Rect.X, floating.Rect.Y, floating.Rect.W, floating.Rect.H))
		if floating.Active {
			builder.WriteString(":active")
		}
		if floating.Collapsed {
			builder.WriteString(":collapsed")
		}
	}
	return builder.String()
}
