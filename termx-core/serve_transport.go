package termx

import (
	"context"

	"github.com/lozzow/termx/termx-core/transport"
)

func (s *Server) ServeTransport(ctx context.Context, t transport.Transport, remote string) error {
	return s.handleTransport(ctx, t, remote)
}
