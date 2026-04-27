package agent

import (
	"context"
	"errors"
	"io"

	"github.com/lozzow/termx/transport"
)

// Proxy forwards full termx protocol frames between two transports until the
// context is canceled or either side disconnects.
func Proxy(ctx context.Context, left, right transport.Transport) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer left.Close()
	defer right.Close()

	errCh := make(chan error, 2)
	go proxyOneWay(ctx, left, right, errCh)
	go proxyOneWay(ctx, right, left, errCh)

	select {
	case <-ctx.Done():
		return nil
	case err := <-errCh:
		if err == nil || errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}
}

func proxyOneWay(ctx context.Context, src, dst transport.Transport, errCh chan<- error) {
	for {
		select {
		case <-ctx.Done():
			errCh <- nil
			return
		default:
		}
		frame, err := src.Recv()
		if err != nil {
			errCh <- err
			return
		}
		if err := dst.Send(frame); err != nil {
			errCh <- err
			return
		}
	}
}
