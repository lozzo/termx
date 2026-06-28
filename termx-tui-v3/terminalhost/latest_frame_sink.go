package terminalhost

import (
	"log/slog"
	"os"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lozzow/termx/termx-shared/perftrace"
	"github.com/lozzow/termx/termx-tui-v3/render"
)

// LatestFrameSink 把真实 TTY 写帧变成 latest-only 背压边界。
// 前台只更新最新待写帧；后台写不过来时，中间帧会被新帧覆盖。
type LatestFrameSink struct {
	mu      sync.Mutex
	cond    *sync.Cond
	sink    render.FrameSink
	pending *render.Frame
	patches []render.Frame
	closed  bool
	wg      sync.WaitGroup

	logger        *slog.Logger
	diagEnabled   bool
	diagInterval  time.Duration
	lastDiag      time.Time
	highWaterMark int
}

func NewLatestFrameSink(sink render.FrameSink) *LatestFrameSink {
	writer := &LatestFrameSink{sink: sink}
	writer.cond = sync.NewCond(&writer.mu)
	writer.wg.Add(1)
	go writer.loop()
	return writer
}

func (sink *LatestFrameSink) SetLogger(logger *slog.Logger) {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	sink.logger = logger
	sink.diagEnabled = logger != nil && latestFrameSinkDiagnosticsEnabled()
	sink.diagInterval = latestFrameSinkDiagnosticsInterval()
}

func (sink *LatestFrameSink) NeedsCompleteFrame() bool {
	preference, ok := sink.sink.(render.FrameSinkPreference)
	if !ok {
		return true
	}
	return preference.NeedsCompleteFrame()
}

// SupportsFrameWriteCompletion 声明 LatestFrameSink 会在后台 writer 写完完整帧后
// 调用 Frame.OnWritten；runtime 依赖它把 live invalidation arm 绑定到真实输出完成。
func (sink *LatestFrameSink) SupportsFrameWriteCompletion() bool {
	return true
}

func (sink *LatestFrameSink) WriteFrame(frame render.Frame) error {
	cloned := frame.Clone()
	sink.mu.Lock()
	if sink.closed {
		sink.mu.Unlock()
		return nil
	}
	if cloned.Patch != nil {
		// 中文说明：增量滚动 patch 依赖顺序，不能像完整 live frame 一样丢中间帧。
		sink.patches = append(sink.patches, cloned)
	} else {
		// 中文说明：完整帧是绝对状态；它会覆盖之前还没写出的增量 patch。
		sink.pending = &cloned
		sink.patches = nil
	}
	sink.observeQueueLocked(cloned.Patch != nil)
	sink.cond.Signal()
	sink.mu.Unlock()
	return nil
}

func (sink *LatestFrameSink) Close() {
	sink.mu.Lock()
	sink.closed = true
	sink.pending = nil
	sink.patches = nil
	sink.cond.Broadcast()
	sink.mu.Unlock()
	sink.wg.Wait()
}

func (sink *LatestFrameSink) loop() {
	defer sink.wg.Done()
	for {
		frame, ok := sink.next()
		if !ok {
			return
		}
		if sink.sink != nil {
			_ = sink.sink.WriteFrame(frame)
		}
		perftrace.Count("tui.frame_sink_written", latestFrameApproxBytes(frame))
		if frame.Patch == nil && frame.OnWritten != nil {
			frame.OnWritten()
		}
	}
}

func (sink *LatestFrameSink) next() (render.Frame, bool) {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	for sink.pending == nil && len(sink.patches) == 0 && !sink.closed {
		sink.cond.Wait()
	}
	if sink.pending == nil && len(sink.patches) == 0 && sink.closed {
		return render.Frame{}, false
	}
	if sink.pending != nil {
		frame := *sink.pending
		sink.pending = nil
		return frame, true
	}
	frame := sink.patches[0]
	copy(sink.patches, sink.patches[1:])
	last := len(sink.patches) - 1
	sink.patches[last] = render.Frame{}
	if last == 0 {
		sink.patches = nil
		return frame, true
	}
	sink.patches = sink.patches[:last]
	return frame, true
}

func (sink *LatestFrameSink) observeQueueLocked(patch bool) {
	if !sink.diagEnabled || sink.logger == nil {
		return
	}
	queueLen := len(sink.patches)
	force := false
	if queueLen > sink.highWaterMark {
		sink.highWaterMark = queueLen
		force = queueLen == 1 || queueLen%128 == 0
	}
	now := time.Now()
	if !force && now.Sub(sink.lastDiag) < sink.diagInterval {
		return
	}
	sink.lastDiag = now
	var mem goruntime.MemStats
	goruntime.ReadMemStats(&mem)
	sink.logger.Debug("tui-v3 latest-frame-sink diagnostic",
		"patch", patch,
		"pending_complete", sink.pending != nil,
		"patch_queue_len", queueLen,
		"patch_queue_high_water", sink.highWaterMark,
		"queued_patch_bytes", latestFrameSinkPatchBytes(sink.patches),
		"heap_alloc", mem.HeapAlloc,
		"heap_objects", mem.HeapObjects,
		"goroutines", goruntime.NumGoroutine(),
	)
}

func latestFrameSinkDiagnosticsEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("TERMX_TUI_DIAG"))) {
	case "1", "true", "on", "yes", "debug":
		return true
	default:
		return false
	}
}

func latestFrameSinkDiagnosticsInterval() time.Duration {
	raw := strings.TrimSpace(os.Getenv("TERMX_TUI_DIAG_INTERVAL_MS"))
	if raw == "" {
		return time.Second
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return time.Second
	}
	return time.Duration(value) * time.Millisecond
}

func latestFrameSinkPatchBytes(frames []render.Frame) int {
	total := 0
	for _, frame := range frames {
		if frame.Patch == nil {
			continue
		}
		total += len(frame.Patch.LineANSI)
		for _, line := range frame.Patch.LinesANSI {
			total += len(line)
		}
	}
	return total
}

func latestFrameApproxBytes(frame render.Frame) int {
	if frame.Patch != nil {
		return latestFrameSinkPatchBytes([]render.Frame{frame})
	}
	total := 0
	for _, line := range frame.Lines {
		total += len(line)
	}
	for _, line := range frame.ANSILines {
		total += len(line)
	}
	return total
}
