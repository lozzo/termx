package runtime

import (
	"context"

	"github.com/lozzow/termx/protocol"
)

type stubClient struct {
	createResult *protocol.CreateResult
	createErr    error
	createCalls  int

	removeCalls []string
	removeErr   error

	killCalls []string
	killErr   error
}

func (c *stubClient) Create(ctx context.Context, command []string, name string, size protocol.Size) (*protocol.CreateResult, error) {
	c.createCalls++
	if c.createErr != nil {
		return nil, c.createErr
	}
	if c.createResult != nil {
		return c.createResult, nil
	}
	return &protocol.CreateResult{TerminalID: "term-default", State: "running"}, nil
}

func (c *stubClient) Attach(ctx context.Context, terminalID string, mode string) (*protocol.AttachResult, error) {
	return &protocol.AttachResult{Mode: mode, Channel: 7}, nil
}

func (c *stubClient) Snapshot(ctx context.Context, terminalID string, offset, limit int) (*protocol.Snapshot, error) {
	return &protocol.Snapshot{TerminalID: terminalID}, nil
}

func (c *stubClient) List(ctx context.Context) (*protocol.ListResult, error) {
	return &protocol.ListResult{}, nil
}

func (c *stubClient) Events(ctx context.Context, params protocol.EventsParams) (<-chan protocol.Event, error) {
	return make(chan protocol.Event), nil
}

func (c *stubClient) Input(ctx context.Context, channel uint16, data []byte) error {
	return nil
}

func (c *stubClient) Resize(ctx context.Context, channel uint16, cols, rows uint16) error {
	return nil
}

func (c *stubClient) Stream(channel uint16) (<-chan protocol.StreamFrame, func()) {
	return make(chan protocol.StreamFrame), func() {}
}

func (c *stubClient) Kill(ctx context.Context, terminalID string) error {
	c.killCalls = append(c.killCalls, terminalID)
	return c.killErr
}

func (c *stubClient) Remove(ctx context.Context, terminalID string) error {
	c.removeCalls = append(c.removeCalls, terminalID)
	return c.removeErr
}
