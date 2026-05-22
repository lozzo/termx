package termx

import (
	"context"

	"github.com/lozzow/termx/termx-shared/transport"
)

func (s *Server) ServeTransport(ctx context.Context, t transport.Transport, remote string) error {
	return s.handleTransport(ctx, t, remote)
}

type TransportScope struct {
	TerminalID        string
	MachineEventsOnly bool
}

func (s *Server) ServeScopedTransport(ctx context.Context, t transport.Transport, remote string, scope TransportScope) error {
	return s.handleTransportScoped(ctx, t, remote, transportScope(scope))
}
