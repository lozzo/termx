package runtime

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/transport/memory"
)

func TestTerminalServiceDelegatesCreateAndAttach(t *testing.T) {
	client := &stubClient{}
	service := NewTerminalService(client)

	created, err := service.Create(context.Background(), []string{"/bin/sh"}, "shell", protocol.Size{Cols: 80, Rows: 24})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if created.TerminalID != "term-1" {
		t.Fatalf("expected term-1, got %#v", created)
	}

	attached, err := service.Attach(context.Background(), "term-1", "rw")
	if err != nil {
		t.Fatalf("Attach returned error: %v", err)
	}
	if attached.Channel != 7 {
		t.Fatalf("expected channel 7, got %#v", attached)
	}
}

func TestTerminalServiceCreateUsesProtocolCreateContract(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	clientTransport, serverTransport := memory.NewPair()
	defer clientTransport.Close()
	defer serverTransport.Close()

	serverDone := make(chan error, 1)
	go func() {
		serverDone <- runCreateContractServer(serverTransport)
	}()

	client := protocol.NewClient(clientTransport)
	defer client.Close()

	service := NewTerminalService(protocolRuntimeClient{inner: client})
	created, err := service.Create(ctx, []string{"/bin/sh"}, "shell-2", protocol.Size{Cols: 90, Rows: 30})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if created.TerminalID != "term-2" {
		t.Fatalf("expected term-2, got %#v", created)
	}

	if err := <-serverDone; err != nil {
		t.Fatalf("server failed: %v", err)
	}
}

func TestTerminalServiceKillUsesProtocolKillContract(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	clientTransport, serverTransport := memory.NewPair()
	defer clientTransport.Close()
	defer serverTransport.Close()

	serverDone := make(chan error, 1)
	go func() {
		serverDone <- runKillContractServer(serverTransport)
	}()

	client := protocol.NewClient(clientTransport)
	defer client.Close()

	service := NewTerminalService(protocolRuntimeClient{inner: client})
	if err := service.Kill(ctx, "term-9"); err != nil {
		t.Fatalf("Kill returned error: %v", err)
	}

	if err := <-serverDone; err != nil {
		t.Fatalf("server failed: %v", err)
	}
}

func TestExecuteWorkbenchActionDelegatesCreateAndKill(t *testing.T) {
	client := &stubClient{}
	service := NewTerminalService(client)

	createResult, err := ExecuteWorkbenchAction(context.Background(), service, PendingWorkbenchAction{
		Kind:    PendingWorkbenchActionCreateTerminal,
		Command: []string{"/bin/sh"},
		Name:    "shell-2",
		Size:    protocol.Size{Cols: 80, Rows: 24},
	})
	if err != nil {
		t.Fatalf("create action returned error: %v", err)
	}
	if createResult.TerminalID != "term-1" {
		t.Fatalf("expected created terminal id, got %#v", createResult)
	}

	if _, err := ExecuteWorkbenchAction(context.Background(), service, PendingWorkbenchAction{
		Kind:       PendingWorkbenchActionKillTerminal,
		TerminalID: "term-1",
	}); err != nil {
		t.Fatalf("kill action returned error: %v", err)
	}
}

type protocolRuntimeClient struct {
	inner *protocol.Client
}

func (c protocolRuntimeClient) Close() error { return c.inner.Close() }

func (c protocolRuntimeClient) Create(ctx context.Context, command []string, name string, size protocol.Size) (*protocol.CreateResult, error) {
	return c.inner.Create(ctx, protocol.CreateParams{
		Command: command,
		Name:    name,
		Size:    size,
	})
}

func (c protocolRuntimeClient) SetTags(ctx context.Context, terminalID string, tags map[string]string) error {
	return c.inner.SetTags(ctx, terminalID, tags)
}

func (c protocolRuntimeClient) SetMetadata(ctx context.Context, terminalID string, name string, tags map[string]string) error {
	return c.inner.SetMetadata(ctx, terminalID, name, tags)
}

func (c protocolRuntimeClient) List(ctx context.Context) (*protocol.ListResult, error) {
	return c.inner.List(ctx)
}

func (c protocolRuntimeClient) Events(ctx context.Context, params protocol.EventsParams) (<-chan protocol.Event, error) {
	return c.inner.Events(ctx, params)
}

func (c protocolRuntimeClient) Attach(ctx context.Context, terminalID string, mode string) (*protocol.AttachResult, error) {
	return c.inner.Attach(ctx, terminalID, mode)
}

func (c protocolRuntimeClient) Snapshot(ctx context.Context, terminalID string, offset, limit int) (*protocol.Snapshot, error) {
	return c.inner.Snapshot(ctx, terminalID, offset, limit)
}

func (c protocolRuntimeClient) Input(ctx context.Context, channel uint16, data []byte) error {
	return c.inner.Input(ctx, channel, data)
}

func (c protocolRuntimeClient) Resize(ctx context.Context, channel uint16, cols, rows uint16) error {
	return c.inner.Resize(ctx, channel, cols, rows)
}

func (c protocolRuntimeClient) Stream(channel uint16) (<-chan protocol.StreamFrame, func()) {
	return c.inner.Stream(channel)
}

func (c protocolRuntimeClient) Kill(ctx context.Context, terminalID string) error {
	return c.inner.Kill(ctx, terminalID)
}

func runCreateContractServer(tr *memory.Transport) error {
	req, err := readRequest(tr)
	if err != nil {
		return err
	}
	if req.Method != "create" {
		return nil
	}

	var params protocol.CreateParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return err
	}
	if len(params.Command) != 1 || params.Command[0] != "/bin/sh" || params.Name != "shell-2" {
		return tcontractError("unexpected create params")
	}
	return writeResponse(tr, req.ID, protocol.CreateResult{TerminalID: "term-2", State: "running"})
}

func runKillContractServer(tr *memory.Transport) error {
	req, err := readRequest(tr)
	if err != nil {
		return err
	}
	if req.Method != "kill" {
		return tcontractError("unexpected method")
	}
	var params protocol.GetParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return err
	}
	if params.TerminalID != "term-9" {
		return tcontractError("unexpected terminal id")
	}
	return writeResponse(tr, req.ID, map[string]any{})
}

func readRequest(tr *memory.Transport) (protocol.Request, error) {
	frame, err := tr.Recv()
	if err != nil {
		return protocol.Request{}, err
	}
	channel, kind, payload, err := protocol.DecodeFrame(frame)
	if err != nil {
		return protocol.Request{}, err
	}
	if channel != 0 || kind != protocol.TypeRequest {
		return protocol.Request{}, tcontractError("unexpected frame")
	}
	var req protocol.Request
	return req, json.Unmarshal(payload, &req)
}

func writeResponse(tr *memory.Transport, id uint64, result any) error {
	payload, err := json.Marshal(result)
	if err != nil {
		return err
	}
	framePayload, err := json.Marshal(protocol.Response{ID: id, Result: payload})
	if err != nil {
		return err
	}
	frame, err := protocol.EncodeFrame(0, protocol.TypeResponse, framePayload)
	if err != nil {
		return err
	}
	return tr.Send(frame)
}

type tcontractError string

func (e tcontractError) Error() string { return string(e) }
