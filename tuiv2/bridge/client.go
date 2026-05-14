package bridge

import (
	"context"

	"github.com/lozzow/termx/termx-core/protocol"
)

type Client interface {
	Close() error
	Create(ctx context.Context, params protocol.CreateParams) (*protocol.CreateResult, error)
	SetTags(ctx context.Context, terminalID string, tags map[string]string) error
	SetMetadata(ctx context.Context, terminalID string, name string, tags map[string]string) error
	List(ctx context.Context) (*protocol.ListResult, error)
	Events(ctx context.Context, params protocol.EventsParams) (<-chan protocol.Event, error)
	Attach(ctx context.Context, params protocol.AttachParams) (*protocol.AttachResult, error)
	EnsureResize(ctx context.Context, params protocol.EnsureResizeParams) (*protocol.EnsureResizeResult, error)
	Snapshot(ctx context.Context, terminalID string, offset, limit int) (*protocol.Snapshot, error)
	GridViewport(ctx context.Context, terminalID string, offset, limit, cols int) (*protocol.GridViewport, error)
	Input(ctx context.Context, channel uint16, data []byte) error
	Resize(ctx context.Context, channel uint16, cols, rows uint16) error
	StreamReady(ctx context.Context, channel uint16) error
	Stream(channel uint16) (<-chan protocol.StreamFrame, func())
	Kill(ctx context.Context, terminalID string) error
	Remove(ctx context.Context, terminalID string) error
	Restart(ctx context.Context, terminalID string) error
}

type StorageClient interface {
	StorageGet(ctx context.Context, params protocol.StorageGetParams) (*protocol.StorageEntry, error)
	StoragePut(ctx context.Context, params protocol.StoragePutParams) (*protocol.StorageEntry, error)
	StorageDelete(ctx context.Context, params protocol.StorageDeleteParams) (*protocol.StorageDeleteResult, error)
	StorageList(ctx context.Context, params protocol.StorageListParams) (*protocol.StorageListResult, error)
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

func (c *ProtocolClient) Attach(ctx context.Context, params protocol.AttachParams) (*protocol.AttachResult, error) {
	return c.inner.AttachWithOptions(ctx, params)
}

func (c *ProtocolClient) EnsureResize(ctx context.Context, params protocol.EnsureResizeParams) (*protocol.EnsureResizeResult, error) {
	return c.inner.EnsureResize(ctx, params)
}

func (c *ProtocolClient) Snapshot(ctx context.Context, terminalID string, offset, limit int) (*protocol.Snapshot, error) {
	return c.inner.Snapshot(ctx, terminalID, offset, limit)
}

func (c *ProtocolClient) GridViewport(ctx context.Context, terminalID string, offset, limit, cols int) (*protocol.GridViewport, error) {
	return c.inner.GridViewport(ctx, terminalID, offset, limit, cols)
}

func (c *ProtocolClient) Input(ctx context.Context, channel uint16, data []byte) error {
	return c.inner.Input(ctx, channel, data)
}

func (c *ProtocolClient) Resize(ctx context.Context, channel uint16, cols, rows uint16) error {
	return c.inner.Resize(ctx, channel, cols, rows)
}

func (c *ProtocolClient) StreamReady(ctx context.Context, channel uint16) error {
	return c.inner.StreamReady(ctx, channel)
}

func (c *ProtocolClient) Stream(channel uint16) (<-chan protocol.StreamFrame, func()) {
	return c.inner.Stream(channel)
}

func (c *ProtocolClient) Kill(ctx context.Context, terminalID string) error {
	return c.inner.Kill(ctx, terminalID)
}

func (c *ProtocolClient) Remove(ctx context.Context, terminalID string) error {
	return c.inner.Remove(ctx, terminalID)
}

func (c *ProtocolClient) Restart(ctx context.Context, terminalID string) error {
	return c.inner.Restart(ctx, terminalID)
}

func (c *ProtocolClient) StorageGet(ctx context.Context, params protocol.StorageGetParams) (*protocol.StorageEntry, error) {
	return c.inner.StorageGet(ctx, params)
}

func (c *ProtocolClient) StoragePut(ctx context.Context, params protocol.StoragePutParams) (*protocol.StorageEntry, error) {
	return c.inner.StoragePut(ctx, params)
}

func (c *ProtocolClient) StorageDelete(ctx context.Context, params protocol.StorageDeleteParams) (*protocol.StorageDeleteResult, error) {
	return c.inner.StorageDelete(ctx, params)
}

func (c *ProtocolClient) StorageList(ctx context.Context, params protocol.StorageListParams) (*protocol.StorageListResult, error) {
	return c.inner.StorageList(ctx, params)
}
