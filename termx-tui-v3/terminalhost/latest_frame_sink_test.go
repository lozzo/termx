package terminalhost

import (
	"sync"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-tui-v3/render"
)

func TestLatestFrameSinkDropsIntermediateFramesWhileWriterBusy(t *testing.T) {
	underlying := newBlockingRecordingFrameSink()
	sink := NewLatestFrameSink(underlying)
	defer sink.Close()

	if err := sink.WriteFrame(render.Frame{Lines: []string{"one"}}); err != nil {
		t.Fatalf("write first frame: %v", err)
	}
	select {
	case <-underlying.started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first underlying write")
	}
	if err := sink.WriteFrame(render.Frame{Lines: []string{"two"}}); err != nil {
		t.Fatalf("write second frame: %v", err)
	}
	if err := sink.WriteFrame(render.Frame{Lines: []string{"three"}}); err != nil {
		t.Fatalf("write third frame: %v", err)
	}
	close(underlying.release)

	if !underlying.waitFrames(2, time.Second) {
		t.Fatalf("timed out waiting for coalesced writes, frames=%#v", underlying.framesSnapshot())
	}
	got := underlying.framesSnapshot()
	if len(got) != 2 || got[0].Lines[0] != "one" || got[1].Lines[0] != "three" {
		t.Fatalf("expected first and latest frames only, got %#v", got)
	}
}

type blockingRecordingFrameSink struct {
	mu      sync.Mutex
	started chan struct{}
	release chan struct{}
	wrote   chan struct{}
	frames  []render.Frame
	writes  int
}

func newBlockingRecordingFrameSink() *blockingRecordingFrameSink {
	return &blockingRecordingFrameSink{
		started: make(chan struct{}),
		release: make(chan struct{}),
		wrote:   make(chan struct{}, 8),
	}
}

func (sink *blockingRecordingFrameSink) WriteFrame(frame render.Frame) error {
	sink.mu.Lock()
	sink.writes++
	writes := sink.writes
	if writes == 1 {
		close(sink.started)
	}
	sink.mu.Unlock()
	if writes == 1 {
		<-sink.release
	}
	sink.mu.Lock()
	sink.frames = append(sink.frames, frame.Clone())
	sink.mu.Unlock()
	select {
	case sink.wrote <- struct{}{}:
	default:
	}
	return nil
}

func (sink *blockingRecordingFrameSink) waitFrames(count int, timeout time.Duration) bool {
	deadline := time.After(timeout)
	for {
		sink.mu.Lock()
		ok := len(sink.frames) >= count
		sink.mu.Unlock()
		if ok {
			return true
		}
		select {
		case <-sink.wrote:
		case <-deadline:
			return false
		}
	}
}

func (sink *blockingRecordingFrameSink) framesSnapshot() []render.Frame {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	out := make([]render.Frame, len(sink.frames))
	for i, frame := range sink.frames {
		out[i] = frame.Clone()
	}
	return out
}
