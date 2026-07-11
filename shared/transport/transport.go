package transport

import (
	"context"
	"errors"
)

var ErrListenerClosed = errors.New("transport: listener closed")

type Transport interface {
	Send(frame []byte) error
	Recv() ([]byte, error)
	Close() error
	Done() <-chan struct{}
}

type Listener interface {
	Accept(ctx context.Context) (Transport, error)
	Close() error
	Addr() string
}
