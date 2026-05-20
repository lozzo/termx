package termx

import (
	"context"
)

type ProtocolMethodHandler interface {
	HandleProtocolMethod(ctx context.Context, method string, params []byte) ([]byte, int, bool, error)
}

func WithProtocolMethodHandler(handler ProtocolMethodHandler) ServerOption {
	return func(cfg *serverConfig) {
		cfg.methodHandler = handler
	}
}

type TerminalInventoryObserver interface {
	TerminalInventoryChanged()
}

func WithTerminalInventoryObserver(observer TerminalInventoryObserver) ServerOption {
	return func(cfg *serverConfig) {
		cfg.terminalObserver = observer
	}
}

func (s *Server) notifyTerminalInventoryChanged() {
	if s == nil || s.cfg.terminalObserver == nil {
		return
	}
	s.cfg.terminalObserver.TerminalInventoryChanged()
}
