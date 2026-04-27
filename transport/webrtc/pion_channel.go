package webrtc

import (
	"io"
	"sync"

	pion "github.com/pion/webrtc/v4"
)

type PionChannel struct {
	dc       *pion.DataChannel
	incoming chan []byte
	done     chan struct{}
	once     sync.Once
}

func NewPionChannel(dc *pion.DataChannel) *PionChannel {
	ch := &PionChannel{
		dc:       dc,
		incoming: make(chan []byte, 256),
		done:     make(chan struct{}),
	}
	dc.OnMessage(func(msg pion.DataChannelMessage) {
		data := append([]byte(nil), msg.Data...)
		select {
		case <-ch.done:
			return
		case ch.incoming <- data:
		}
	})
	dc.OnClose(func() {
		_ = ch.Close()
	})
	dc.OnError(func(error) {
		_ = ch.Close()
	})
	return ch
}

func (c *PionChannel) Send(msg []byte) error {
	if c == nil || c.dc == nil {
		return io.EOF
	}
	return c.dc.Send(msg)
}

func (c *PionChannel) Recv() ([]byte, error) {
	if c == nil {
		return nil, io.EOF
	}
	select {
	case <-c.done:
		return nil, io.EOF
	case msg, ok := <-c.incoming:
		if !ok {
			return nil, io.EOF
		}
		return msg, nil
	}
}

func (c *PionChannel) Close() error {
	if c == nil {
		return nil
	}
	c.once.Do(func() {
		close(c.done)
		if c.dc != nil && c.dc.ReadyState() != pion.DataChannelStateClosed {
			_ = c.dc.Close()
		}
	})
	return nil
}

func (c *PionChannel) Done() <-chan struct{} {
	if c == nil {
		done := make(chan struct{})
		close(done)
		return done
	}
	return c.done
}
