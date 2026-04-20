package fanout

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/lozzow/termx/perftrace"
)

type StreamMessageType int

const (
	StreamOutput StreamMessageType = iota + 1
	StreamSyncLost
	StreamClosed
	StreamResize
	StreamBootstrapDone
	StreamScreenUpdate
)

type StreamMessage struct {
	Type              StreamMessageType
	Output            []byte
	OutputRateLimited bool
	Payload           []byte
	DroppedBytes      uint64
	ExitCode          *int
	Cols              uint16
	Rows              uint16
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

// Broadcast shares the provided payload with every subscriber. Callers must
// treat data as immutable after broadcasting.
func (f *Fanout) Broadcast(data []byte) {
	f.BroadcastMessage(StreamMessage{Type: StreamOutput, Output: data})
}

// BroadcastMessage shares the provided message with every subscriber. Callers
// must treat payload buffers as immutable after broadcasting.
func (f *Fanout) BroadcastMessage(msg StreamMessage) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	for sub := range f.subs {
		if buffered := len(sub.ch); buffered > 0 {
			perftrace.Count("fanout.subscriber.backlog.frames", buffered)
		}
		if dropped := sub.droppedBytes.Load(); dropped > 0 {
			select {
			case sub.ch <- StreamMessage{Type: StreamSyncLost, DroppedBytes: dropped}:
				sub.droppedBytes.Store(0)
			default:
			}
		}

		select {
		case sub.ch <- msg:
		default:
			if isPriorityMessage(msg) && enqueuePriorityMessage(sub, msg) {
				continue
			}
			sub.droppedBytes.Add(uint64(messagePayloadLen(msg)))
		}
	}
}

func (f *Fanout) BroadcastResize(cols, rows uint16) {
	f.BroadcastMessage(StreamMessage{Type: StreamResize, Cols: cols, Rows: rows})
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

func messagePayloadLen(msg StreamMessage) int {
	switch msg.Type {
	case StreamOutput:
		return len(msg.Output)
	case StreamScreenUpdate:
		return len(msg.Payload)
	default:
		return 0
	}
}

func isPriorityMessage(msg StreamMessage) bool {
	switch msg.Type {
	case StreamResize, StreamBootstrapDone, StreamClosed, StreamScreenUpdate:
		return true
	default:
		return false
	}
}

func isDroppableBufferedMessage(msg StreamMessage) bool {
	switch msg.Type {
	case StreamOutput, StreamSyncLost, StreamScreenUpdate:
		return true
	default:
		return false
	}
}

func enqueuePriorityMessage(sub *subscriber, msg StreamMessage) bool {
	if sub == nil {
		return false
	}
	select {
	case displaced := <-sub.ch:
		if !isDroppableBufferedMessage(displaced) {
			select {
			case sub.ch <- displaced:
			default:
			}
			return false
		}
		if displaced.Type == StreamSyncLost {
			sub.droppedBytes.Add(displaced.DroppedBytes)
		} else {
			sub.droppedBytes.Add(uint64(messagePayloadLen(displaced)))
		}
	default:
		return false
	}
	select {
	case sub.ch <- msg:
		return true
	default:
		return false
	}
}
