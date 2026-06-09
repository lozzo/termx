package client

import (
	"context"

	"github.com/lozzow/termx/internal/protocol"
)

// Daemon is the shell-neutral core boundary used by remote product code.
type Daemon interface {
	List(ctx context.Context) (*protocol.ListResult, error)
}

// Service is the initial remote client facade around a daemon boundary.
type Service struct {
	daemon Daemon
}

// New creates a remote client facade using a shell-neutral daemon boundary.
func New(daemon Daemon) *Service {
	return &Service{daemon: daemon}
}

// Daemon returns the daemon boundary used by this service.
func (s *Service) Daemon() Daemon {
	if s == nil {
		return nil
	}
	return s.daemon
}
