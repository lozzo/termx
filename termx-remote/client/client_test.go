package client

import (
	"context"
	"testing"

	"github.com/lozzow/termx/internal/protocol"
)

func TestDaemonBoundaryIsMinimalShellNeutralCapability(t *testing.T) {
	service := New(listOnlyDaemon{})
	if service.Daemon() == nil {
		t.Fatal("expected minimal daemon boundary to be retained")
	}
}

type listOnlyDaemon struct{}

func (listOnlyDaemon) List(context.Context) (*protocol.ListResult, error) { return nil, nil }
