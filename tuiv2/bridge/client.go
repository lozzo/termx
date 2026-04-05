package bridge

import (
	"context"

	"github.com/lozzow/termx/protocol"
)

type Client interface {
	Close() error
	Create(ctx context.Context, params protocol.CreateParams) (*protocol.CreateResult, error)
	SetTags(ctx context.Context, terminalID string, tags map[string]string) error
	SetMetadata(ctx context.Context, terminalID string, name string, tags map[string]string) error
	List(ctx context.Context) (*protocol.ListResult, error)
	Events(ctx context.Context, params protocol.EventsParams) (<-chan protocol.Event, error)
	Attach(ctx context.Context, terminalID string, mode string) (*protocol.AttachResult, error)
	Snapshot(ctx context.Context, terminalID string, offset, limit int) (*protocol.Snapshot, error)
	Input(ctx context.Context, channel uint16, data []byte) error
	Resize(ctx context.Context, channel uint16, cols, rows uint16) error
	Stream(channel uint16) (<-chan protocol.StreamFrame, func())
	Kill(ctx context.Context, terminalID string) error
	CreateSession(ctx context.Context, params protocol.CreateSessionParams) (*protocol.SessionSnapshot, error)
	ListSessions(ctx context.Context) (*protocol.ListSessionsResult, error)
	GetSession(ctx context.Context, sessionID string) (*protocol.SessionSnapshot, error)
	AttachSession(ctx context.Context, params protocol.AttachSessionParams) (*protocol.SessionSnapshot, error)
	DetachSession(ctx context.Context, sessionID, viewID string) error
	ApplySession(ctx context.Context, params protocol.ApplySessionParams) (*protocol.SessionSnapshot, error)
	ReplaceSession(ctx context.Context, params protocol.ReplaceSessionParams) (*protocol.SessionSnapshot, error)
	UpdateSessionView(ctx context.Context, params protocol.UpdateSessionViewParams) (*protocol.ViewInfo, error)
}

type ProtocolClient struct {
	inner *protocol.Client
}

func NewProtocolClient(inner *protocol.Client) *ProtocolClient {
	return &ProtocolClient{inner: inner}
}

func (c *ProtocolClient) Close() error { return c.inner.Close() }

func (c *ProtocolClient) Create(ctx context.Context, params protocol.CreateParams) (*protocol.CreateResult, error) {
	return c.inner.Create(ctx, params)
}

func (c *ProtocolClient) SetTags(ctx context.Context, terminalID string, tags map[string]string) error {
	return c.inner.SetTags(ctx, terminalID, tags)
}

func (c *ProtocolClient) SetMetadata(ctx context.Context, terminalID string, name string, tags map[string]string) error {
	return c.inner.SetMetadata(ctx, terminalID, name, tags)
}

func (c *ProtocolClient) List(ctx context.Context) (*protocol.ListResult, error) {
	return c.inner.List(ctx)
}

func (c *ProtocolClient) Events(ctx context.Context, params protocol.EventsParams) (<-chan protocol.Event, error) {
	return c.inner.Events(ctx, params)
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

func (c *ProtocolClient) CreateSession(ctx context.Context, params protocol.CreateSessionParams) (*protocol.SessionSnapshot, error) {
	return c.inner.CreateSession(ctx, params)
}

func (c *ProtocolClient) ListSessions(ctx context.Context) (*protocol.ListSessionsResult, error) {
	return c.inner.ListSessions(ctx)
}

func (c *ProtocolClient) GetSession(ctx context.Context, sessionID string) (*protocol.SessionSnapshot, error) {
	return c.inner.GetSession(ctx, sessionID)
}

func (c *ProtocolClient) AttachSession(ctx context.Context, params protocol.AttachSessionParams) (*protocol.SessionSnapshot, error) {
	return c.inner.AttachSession(ctx, params)
}

func (c *ProtocolClient) DetachSession(ctx context.Context, sessionID, viewID string) error {
	return c.inner.DetachSession(ctx, sessionID, viewID)
}

func (c *ProtocolClient) ApplySession(ctx context.Context, params protocol.ApplySessionParams) (*protocol.SessionSnapshot, error) {
	return c.inner.ApplySession(ctx, params)
}

func (c *ProtocolClient) ReplaceSession(ctx context.Context, params protocol.ReplaceSessionParams) (*protocol.SessionSnapshot, error) {
	return c.inner.ReplaceSession(ctx, params)
}

func (c *ProtocolClient) UpdateSessionView(ctx context.Context, params protocol.UpdateSessionViewParams) (*protocol.ViewInfo, error) {
	return c.inner.UpdateSessionView(ctx, params)
}
