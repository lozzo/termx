package fanout

import (
	"context"
	"sync"
	"sync/atomic"
)

type StreamMessageType int

const (
	StreamOutput StreamMessageType = iota + 1
	StreamSyncLost
	StreamClosed
)

type StreamMessage struct {
	Type         StreamMessageType
	Output       []byte
	DroppedBytes uint64
	ExitCode     *int
}

type Fanout struct {
	mu     sync.RWMutex
	subs   map[*subscriber]struct{}
	closed bool
}

type subscriber struct {
	ch           chan StreamMessage
	droppedBytes atomic.Uint64
}

func New() *Fanout {
	return &Fanout{subs: make(map[*subscriber]struct{})}
}

func (f *Fanout) Subscribe(ctx context.Context) <-chan StreamMessage {
	sub := &subscriber{ch: make(chan StreamMessage, 256)}
	f.mu.Lock()
	if f.closed {
		close(sub.ch)
		f.mu.Unlock()
		return sub.ch
	}
	f.subs[sub] = struct{}{}
	f.mu.Unlock()

	go func() {
		<-ctx.Done()
		f.mu.Lock()
		if _, ok := f.subs[sub]; ok {
			delete(f.subs, sub)
			close(sub.ch)
		}
		f.mu.Unlock()
	}()

	return sub.ch
}

func (f *Fanout) Broadcast(data []byte) {
	msg := append([]byte(nil), data...)
	f.mu.RLock()
	defer f.mu.RUnlock()
	for sub := range f.subs {
		if dropped := sub.droppedBytes.Load(); dropped > 0 {
			select {
			case sub.ch <- StreamMessage{Type: StreamSyncLost, DroppedBytes: dropped}:
				sub.droppedBytes.Store(0)
			default:
			}
		}

		select {
		case sub.ch <- StreamMessage{Type: StreamOutput, Output: msg}:
		default:
			sub.droppedBytes.Add(uint64(len(msg)))
		}
	}
}

func (f *Fanout) Close(exitCode *int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return
	}
	f.closed = true
	for sub := range f.subs {
		select {
		case sub.ch <- StreamMessage{Type: StreamClosed, ExitCode: copyIntPtr(exitCode)}:
		default:
		}
		close(sub.ch)
		delete(f.subs, sub)
	}
}

func copyIntPtr(v *int) *int {
	if v == nil {
		return nil
	}
	n := *v
	return &n
}
