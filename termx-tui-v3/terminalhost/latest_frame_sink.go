package terminalhost

import (
	"sync"

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
}

func NewLatestFrameSink(sink render.FrameSink) *LatestFrameSink {
	writer := &LatestFrameSink{sink: sink}
	writer.cond = sync.NewCond(&writer.mu)
	writer.wg.Add(1)
	go writer.loop()
	return writer
}

func (sink *LatestFrameSink) NeedsCompleteFrame() bool {
	preference, ok := sink.sink.(render.FrameSinkPreference)
	if !ok {
		return true
	}
	return preference.NeedsCompleteFrame()
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
	sink.patches = sink.patches[:len(sink.patches)-1]
	return frame, true
}
