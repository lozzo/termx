package memory

import (
	"context"
	"io"
	"sync"

	"github.com/lozzow/termx/transport"
)

type Transport struct {
	incoming <-chan []byte
	outgoing chan<- []byte
	done     chan struct{}
	closeFn  func()
	once     sync.Once
}

func NewPair() (*Transport, *Transport) {
	aToB := make(chan []byte, 256)
	bToA := make(chan []byte, 256)
	aDone := make(chan struct{})
	bDone := make(chan struct{})

	a := &Transport{
		incoming: bToA,
		outgoing: aToB,
		done:     aDone,
	}
	b := &Transport{
		incoming: aToB,
		outgoing: bToA,
		done:     bDone,
	}
	a.closeFn = func() {
		close(aDone)
		close(aToB)
	}
	b.closeFn = func() {
		close(bDone)
		close(bToA)
	}
	return a, b
}

func (t *Transport) Send(frame []byte) error {
	select {
	case <-t.done:
		return io.EOF
	default:
	}
	data := append([]byte(nil), frame...)
	select {
	case <-t.done:
		return io.EOF
	case t.outgoing <- data:
		return nil
	}
}

func (t *Transport) Recv() ([]byte, error) {
	select {
	case <-t.done:
		return nil, io.EOF
	case frame, ok := <-t.incoming:
		if !ok {
			return nil, io.EOF
		}
		// Send already clones before enqueuing, so the receiver owns this buffer
		// and can consume it directly without another copy.
		return frame, nil
	}
}

func (t *Transport) Close() error {
	t.once.Do(t.closeFn)
	return nil
}

func (t *Transport) Done() <-chan struct{} {
	return t.done
}

type Listener struct {
	ch   chan transport.Transport
	done chan struct{}
	addr string
	once sync.Once
}

func NewListener(addr string) *Listener {
	return &Listener{
		ch:   make(chan transport.Transport),
		done: make(chan struct{}),
		addr: addr,
	}
}

func (l *Listener) Accept(ctx context.Context) (transport.Transport, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-l.done:
		return nil, transport.ErrListenerClosed
	case conn, ok := <-l.ch:
		if !ok {
			return nil, transport.ErrListenerClosed
		}
		return conn, nil
	}
}

func (l *Listener) Close() error {
	l.once.Do(func() {
		close(l.done)
		close(l.ch)
	})
	return nil
}

func (l *Listener) Addr() string {
	return l.addr
}

func (l *Listener) Dial() transport.Transport {
	client, server := NewPair()
	l.ch <- server
	return client
}
