package runtime

import (
	"context"

	"github.com/lozzow/termx/protocol"
)

type TerminalService struct {
	client Client
}

func NewTerminalService(client Client) TerminalService {
	return TerminalService{client: client}
}

func (s TerminalService) Create(ctx context.Context, command []string, name string, size protocol.Size) (*protocol.CreateResult, error) {
	return s.client.Create(ctx, command, name, size)
}

func (s TerminalService) Attach(ctx context.Context, terminalID string, mode string) (*protocol.AttachResult, error) {
	return s.client.Attach(ctx, terminalID, mode)
}

func (s TerminalService) Snapshot(ctx context.Context, terminalID string, offset, limit int) (*protocol.Snapshot, error) {
	return s.client.Snapshot(ctx, terminalID, offset, limit)
}

func (s TerminalService) Events(ctx context.Context, params protocol.EventsParams) (<-chan protocol.Event, error) {
	return s.client.Events(ctx, params)
}

func (s TerminalService) Input(ctx context.Context, channel uint16, data []byte) error {
	return s.client.Input(ctx, channel, data)
}

func (s TerminalService) Resize(ctx context.Context, channel uint16, cols, rows uint16) error {
	return s.client.Resize(ctx, channel, cols, rows)
}

func (s TerminalService) Stream(channel uint16) (<-chan protocol.StreamFrame, func()) {
	return s.client.Stream(channel)
}

func (s TerminalService) Kill(ctx context.Context, terminalID string) error {
	return s.client.Kill(ctx, terminalID)
}
