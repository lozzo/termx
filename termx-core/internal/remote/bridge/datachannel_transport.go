package bridge

import (
	"context"
	"io"
	"strings"
	"sync"

	"github.com/lozzow/termx/termx-core/transport"
	"github.com/pion/webrtc/v4"
)

type TransportSink interface {
	ServeRemoteTransport(ctx context.Context, t transport.Transport, remote string) error
}

type DataChannelTransport struct {
	dc      *webrtc.DataChannel
	recvCh  chan []byte
	done    chan struct{}
	closeFn sync.Once
}

func NewDataChannelTransport(dc *webrtc.DataChannel) *DataChannelTransport {
	t := &DataChannelTransport{
		dc:     dc,
		recvCh: make(chan []byte, 256),
		done:   make(chan struct{}),
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
	return t.dc.Send(append([]byte(nil), frame...))
}

func (t *DataChannelTransport) Recv() ([]byte, error) {
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
		close(t.done)
		close(t.recvCh)
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
		close(t.done)
		close(t.recvCh)
	})
}

func IsTerminalChannelLabel(label string) bool {
	return strings.HasPrefix(label, "terminal:")
}
