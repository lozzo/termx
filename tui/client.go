package tui

import (
	"context"

	"github.com/lozzow/termx/protocol"
)

type Client interface {
	Close() error
	Create(ctx context.Context, command []string, name string, size protocol.Size) (*protocol.CreateResult, error)
	List(ctx context.Context) (*protocol.ListResult, error)
	Attach(ctx context.Context, terminalID string, mode string) (*protocol.AttachResult, error)
	Snapshot(ctx context.Context, terminalID string, offset, limit int) (*protocol.Snapshot, error)
	Input(ctx context.Context, channel uint16, data []byte) error
	Resize(ctx context.Context, channel uint16, cols, rows uint16) error
	Stream(channel uint16) (<-chan protocol.StreamFrame, func())
	Kill(ctx context.Context, terminalID string) error
}

type ProtocolClient struct {
	inner *protocol.Client
}

func NewProtocolClient(inner *protocol.Client) *ProtocolClient {
	return &ProtocolClient{inner: inner}
}

func (c *ProtocolClient) Close() error { return c.inner.Close() }

func (c *ProtocolClient) Create(ctx context.Context, command []string, name string, size protocol.Size) (*protocol.CreateResult, error) {
	return c.inner.Create(ctx, protocol.CreateParams{
		Command: command,
		Name:    name,
		Size:    size,
	})
}

func (c *ProtocolClient) List(ctx context.Context) (*protocol.ListResult, error) {
	return c.inner.List(ctx)
}

func (c *ProtocolClient) Attach(ctx context.Context, terminalID string, mode string) (*protocol.AttachResult, error) {
	return c.inner.Attach(ctx, terminalID, mode)
}

func (c *ProtocolClient) Snapshot(ctx context.Context, terminalID string, offset, limit int) (*protocol.Snapshot, error) {
	return c.inner.Snapshot(ctx, terminalID, offset, limit)
}

func (c *ProtocolClient) Input(ctx context.Context, channel uint16, data []byte) error {
	return c.inner.Input(ctx, channel, data)
}

func (c *ProtocolClient) Resize(ctx context.Context, channel uint16, cols, rows uint16) error {
	return c.inner.Resize(ctx, channel, cols, rows)
}

func (c *ProtocolClient) Stream(channel uint16) (<-chan protocol.StreamFrame, func()) {
	return c.inner.Stream(channel)
}

func (c *ProtocolClient) Kill(ctx context.Context, terminalID string) error {
	return c.inner.Kill(ctx, terminalID)
}
