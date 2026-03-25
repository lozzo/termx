package tui

import (
	"context"
	"errors"
	"testing"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tui/domain/types"
)

type stubRuntimeTerminalServiceClient struct {
	createCalls   []runtimeCreateCall
	metadataCalls []runtimeMetadataCall
	killCalls     []string
}

func (c *stubRuntimeTerminalServiceClient) Close() error { return nil }

func (c *stubRuntimeTerminalServiceClient) Create(_ context.Context, command []string, name string, size protocol.Size) (*protocol.CreateResult, error) {
	c.createCalls = append(c.createCalls, runtimeCreateCall{
		command: append([]string(nil), command...),
		name:    name,
		size:    size,
	})
	return &protocol.CreateResult{TerminalID: "term-created", State: "running"}, nil
}

func (c *stubRuntimeTerminalServiceClient) SetTags(context.Context, string, map[string]string) error {
	return nil
}

func (c *stubRuntimeTerminalServiceClient) SetMetadata(_ context.Context, terminalID string, name string, tags map[string]string) error {
	cloned := make(map[string]string, len(tags))
	for key, value := range tags {
		cloned[key] = value
	}
	c.metadataCalls = append(c.metadataCalls, runtimeMetadataCall{
		terminalID: terminalID,
		name:       name,
		tags:       cloned,
	})
	return nil
}

func (c *stubRuntimeTerminalServiceClient) List(context.Context) (*protocol.ListResult, error) {
	return nil, nil
}

func (c *stubRuntimeTerminalServiceClient) Events(context.Context, protocol.EventsParams) (<-chan protocol.Event, error) {
	ch := make(chan protocol.Event)
	close(ch)
	return ch, nil
}

func (c *stubRuntimeTerminalServiceClient) Attach(context.Context, string, string) (*protocol.AttachResult, error) {
	return nil, nil
}

func (c *stubRuntimeTerminalServiceClient) Snapshot(context.Context, string, int, int) (*protocol.Snapshot, error) {
	return nil, nil
}

func (c *stubRuntimeTerminalServiceClient) Input(context.Context, uint16, []byte) error { return nil }

func (c *stubRuntimeTerminalServiceClient) Resize(context.Context, uint16, uint16, uint16) error {
	return nil
}

func (c *stubRuntimeTerminalServiceClient) Stream(uint16) (<-chan protocol.StreamFrame, func()) {
	ch := make(chan protocol.StreamFrame)
	close(ch)
	return ch, func() {}
}

func (c *stubRuntimeTerminalServiceClient) Kill(_ context.Context, terminalID string) error {
	c.killCalls = append(c.killCalls, terminalID)
	return nil
}

func TestRuntimeTerminalServiceDelegatesBasicTerminalActions(t *testing.T) {
	client := &stubRuntimeTerminalServiceClient{}
	service := newRuntimeTerminalService(client).(runtimeTerminalService)

	created, err := service.CreateTerminal(types.PaneID("pane-1"), []string{"sh", "-l"}, "main-shell")
	if err != nil {
		t.Fatalf("expected create terminal delegation to succeed, got %v", err)
	}
	if err := service.UpdateTerminalMetadata(types.TerminalID("term-1"), "api-dev", map[string]string{"env": "dev"}); err != nil {
		t.Fatalf("expected metadata delegation to succeed, got %v", err)
	}
	if err := service.StopTerminal(types.TerminalID("term-1")); err != nil {
		t.Fatalf("expected stop delegation to succeed, got %v", err)
	}

	if len(client.createCalls) != 1 || client.createCalls[0].name != "main-shell" {
		t.Fatalf("unexpected create delegation payload: %+v", client.createCalls)
	}
	if created.TerminalID != types.TerminalID("term-created") || created.State != types.TerminalRunStateRunning {
		t.Fatalf("unexpected create terminal result: %+v", created)
	}
	if len(client.metadataCalls) != 1 || client.metadataCalls[0].terminalID != "term-1" || client.metadataCalls[0].name != "api-dev" {
		t.Fatalf("unexpected metadata delegation payload: %+v", client.metadataCalls)
	}
	if len(client.killCalls) != 1 || client.killCalls[0] != "term-1" {
		t.Fatalf("unexpected kill delegation payload: %+v", client.killCalls)
	}
}

func TestRuntimeTerminalServiceReturnsExplicitErrorWhenTopologyActionsUnsupported(t *testing.T) {
	client := &stubRuntimeTerminalServiceClient{}
	service := newRuntimeTerminalService(client).(runtimeTerminalService)

	err := service.ConnectTerminalInNewTab(types.WorkspaceID("ws-1"), types.TerminalID("term-2"))
	if !errors.Is(err, errRuntimeTopologyUnsupported) {
		t.Fatalf("expected unsupported topology error for new-tab connect, got %v", err)
	}

	err = service.ConnectTerminalInFloatingPane(types.WorkspaceID("ws-1"), types.TabID("tab-1"), types.TerminalID("term-2"))
	if !errors.Is(err, errRuntimeTopologyUnsupported) {
		t.Fatalf("expected unsupported topology error for floating connect, got %v", err)
	}
}
