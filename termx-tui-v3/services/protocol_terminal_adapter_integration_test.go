package services

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/termx-proto/wire"
	"github.com/lozzow/termx/termx-shared/transport/memory"
)

func TestProtocolTerminalServiceAdapterWithRealProtocolClient(t *testing.T) {
	clientTransport, serverTransport := memory.NewPair()
	errCh := make(chan error, 1)
	seen := make(chan protocol.Size, 1)
	go func() {
		errCh <- runTerminalAdapterProtocolServer(serverTransport, seen)
	}()
	client := protocol.NewClient(clientTransport)
	defer func() { _ = client.Close() }()
	if err := client.Hello(context.Background(), protocol.Hello{Version: wire.Version, Client: "tui-v3-test"}); err != nil {
		t.Fatalf("hello: %v", err)
	}

	adapter := ProtocolTerminalServiceAdapter{Client: client}
	attached, err := adapter.Attach(context.Background(), TerminalAttachRequest{
		TerminalID:   "term-1",
		Cols:         80,
		Rows:         24,
		ResizePolicy: protocol.ResizePolicyOwner,
		SurfaceID:    "surface-1",
		ViewID:       "view-1",
	})
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if attached.Channel != 9 || !attached.CanResize {
		t.Fatalf("unexpected attach result %#v", attached)
	}
	if err := adapter.SendInput(context.Background(), TerminalInputRequest{TerminalID: "term-1", Channel: attached.Channel, Bytes: []byte("x")}); err != nil {
		t.Fatalf("input: %v", err)
	}
	if err := adapter.Resize(context.Background(), TerminalResizeRequest{
		TerminalID: "term-1",
		Channel:    attached.Channel,
		Cols:       100,
		Rows:       40,
		SurfaceID:  "surface-1",
		ViewID:     "view-1",
	}); err != nil {
		t.Fatalf("resize: %v", err)
	}

	select {
	case size := <-seen:
		if size != (protocol.Size{Cols: 100, Rows: 40}) {
			t.Fatalf("unexpected resize size %#v", size)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for resize")
	}
	_ = clientTransport.Close()
	select {
	case err := <-errCh:
		if err != nil && err != io.EOF {
			t.Fatalf("server returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not stop")
	}
}

func runTerminalAdapterProtocolServer(tr *memory.Transport, seen chan<- protocol.Size) error {
	if err := expectTerminalAdapterHello(tr); err != nil {
		return err
	}
	if err := sendTerminalAdapterHello(tr); err != nil {
		return err
	}
	req, err := expectTerminalAdapterRequest(tr, "attach")
	if err != nil {
		return err
	}
	params, err := terminalAdapterRequestParams[protocol.AttachParams](req)
	if err != nil {
		return err
	}
	if params.TerminalID != "term-1" || params.SurfaceID != "surface-1" || params.ViewID != "view-1" {
		return fmt.Errorf("unexpected attach params %#v", params)
	}
	if err := sendTerminalAdapterMethodResponse(tr, req, protocol.AttachResult{
		Mode:    "collaborator",
		Channel: 9,
		ResizeControl: &protocol.ResizeControl{
			CanResize: true,
			Reason:    protocol.ResizeControlReasonOwner,
		},
	}); err != nil {
		return err
	}
	channel, typ, payload, err := recvTerminalAdapterFrame(tr)
	if err != nil {
		return err
	}
	if channel != 9 || typ != wire.TypeInput || string(payload) != "x" {
		return fmt.Errorf("unexpected input frame channel=%d type=%d payload=%q", channel, typ, string(payload))
	}
	req, err = expectTerminalAdapterRequest(tr, "ensure_resize")
	if err != nil {
		return err
	}
	resize, err := terminalAdapterRequestParams[protocol.EnsureResizeParams](req)
	if err != nil {
		return err
	}
	seen <- protocol.Size{Cols: resize.Cols, Rows: resize.Rows}
	return sendTerminalAdapterMethodResponse(tr, req, protocol.EnsureResizeResult{
		Size:    protocol.Size{Cols: resize.Cols, Rows: resize.Rows},
		Resized: true,
		ResizeControl: &protocol.ResizeControl{
			CanResize: true,
			Reason:    protocol.ResizeControlReasonOwner,
		},
	})
}

func expectTerminalAdapterHello(tr *memory.Transport) error {
	channel, typ, payload, err := recvTerminalAdapterFrame(tr)
	if err != nil {
		return err
	}
	if channel != 0 || typ != wire.TypeHello {
		return fmt.Errorf("unexpected hello frame channel=%d type=%d", channel, typ)
	}
	_, err = protocol.DecodeHelloPayload(payload)
	return err
}

func sendTerminalAdapterHello(tr *memory.Transport) error {
	payload, err := protocol.EncodeHelloPayload(protocol.Hello{Version: wire.Version, Server: "fake"})
	if err != nil {
		return err
	}
	return sendTerminalAdapterFrame(tr, 0, wire.TypeHello, payload)
}

func expectTerminalAdapterRequest(tr *memory.Transport, method string) (protocol.Request, error) {
	channel, typ, payload, err := recvTerminalAdapterFrame(tr)
	if err != nil {
		return protocol.Request{}, err
	}
	if channel != 0 || typ != wire.TypeRequest {
		return protocol.Request{}, fmt.Errorf("unexpected request frame channel=%d type=%d", channel, typ)
	}
	req, err := protocol.DecodeRequestPayload(payload)
	if err != nil {
		return protocol.Request{}, err
	}
	if req.Method != method {
		return protocol.Request{}, fmt.Errorf("unexpected method %s", req.Method)
	}
	return req, nil
}

func sendTerminalAdapterMethodResponse(tr *memory.Transport, req protocol.Request, result any) error {
	resultPayload, err := protocol.EncodeMethodResult(req.Method, result)
	if err != nil {
		return err
	}
	payload, err := protocol.EncodeResponsePayload(protocol.Response{ID: req.ID, Result: resultPayload})
	if err != nil {
		return err
	}
	return sendTerminalAdapterFrame(tr, 0, wire.TypeResponse, payload)
}

func terminalAdapterRequestParams[T any](req protocol.Request) (T, error) {
	var zero T
	decoded, err := protocol.DecodeMethodParams(req.Method, req.Params)
	if err != nil {
		return zero, err
	}
	params, ok := decoded.(T)
	if !ok {
		return zero, fmt.Errorf("unexpected params type %T", decoded)
	}
	return params, nil
}

func sendTerminalAdapterFrame(tr *memory.Transport, channel uint16, typ uint8, payload []byte) error {
	frame, err := wire.EncodeFrame(channel, typ, payload)
	if err != nil {
		return err
	}
	return tr.Send(frame)
}

func recvTerminalAdapterFrame(tr *memory.Transport) (uint16, uint8, []byte, error) {
	frame, err := tr.Recv()
	if err != nil {
		return 0, 0, nil, err
	}
	return wire.DecodeFrame(frame)
}
