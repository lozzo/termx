package bridge

import (
	"context"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/lozzow/termx/termx-shared/transport"
	"github.com/pion/webrtc/v4"
)

type TransportSink interface {
	ServeRemoteTransport(ctx context.Context, t transport.Transport, remote string) error
}

const (
	sendBufferHigh = 512 * 1024
	sendBufferLow  = 128 * 1024
)

type dataChannel interface {
	OnMessage(func(webrtc.DataChannelMessage))
	OnClose(func())
	BufferedAmount() uint64
	SetBufferedAmountLowThreshold(uint64)
	OnBufferedAmountLow(func())
	Send([]byte) error
	Close() error
}

type DataChannelTransport struct {
	dc      dataChannel
	recvCh  chan []byte
	drainCh chan struct{}
	done    chan struct{}
	sendMu  sync.Mutex
	closeFn sync.Once
}

func NewDataChannelTransport(dc *webrtc.DataChannel) *DataChannelTransport {
	return newDataChannelTransport(dc)
}

func newDataChannelTransport(dc dataChannel) *DataChannelTransport {
	t := &DataChannelTransport{
		dc:      dc,
		recvCh:  make(chan []byte, 256),
		drainCh: make(chan struct{}, 1),
		done:    make(chan struct{}),
	}
	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		if len(msg.Data) == 0 {
			return
		}
		data := append([]byte(nil), msg.Data...)
		select {
		case <-t.done:
			return
		case t.recvCh <- data:
		}
	})
	dc.OnClose(func() {
		t.closeDone()
	})
	dc.SetBufferedAmountLowThreshold(sendBufferLow)
	dc.OnBufferedAmountLow(func() {
		select {
		case t.drainCh <- struct{}{}:
		default:
		}
	})
	return t
}

func (t *DataChannelTransport) Send(frame []byte) error {
	if t.dc == nil {
		return io.EOF
	}
	select {
	case <-t.done:
		return io.EOF
	default:
	}
	for t.dc.BufferedAmount() > sendBufferHigh {
		select {
		case <-t.drainCh:
		case <-t.done:
			return io.EOF
		case <-time.After(30 * time.Second):
			return context.DeadlineExceeded
		}
	}
	t.sendMu.Lock()
	defer t.sendMu.Unlock()
	select {
	case <-t.done:
		return io.EOF
	default:
	}
	return t.dc.Send(append([]byte(nil), frame...))
}

func (t *DataChannelTransport) Recv() ([]byte, error) {
	select {
	case <-t.done:
		return nil, io.EOF
	default:
	}
	select {
	case <-t.done:
		return nil, io.EOF
	case frame, ok := <-t.recvCh:
		if !ok {
			return nil, io.EOF
		}
		return frame, nil
	}
}

func (t *DataChannelTransport) Close() error {
	t.closeFn.Do(func() {
		t.sendMu.Lock()
		defer t.sendMu.Unlock()
		close(t.done)
		if t.dc != nil {
			_ = t.dc.Close()
		}
	})
	return nil
}

func (t *DataChannelTransport) Done() <-chan struct{} {
	return t.done
}

func (t *DataChannelTransport) closeDone() {
	t.closeFn.Do(func() {
		t.sendMu.Lock()
		defer t.sendMu.Unlock()
		close(t.done)
	})
}

func IsTerminalChannelLabel(label string) bool {
	return strings.HasPrefix(label, "terminal:")
}
