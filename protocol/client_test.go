package protocol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/transport/memory"
)

func TestClientRequestStreamAndProtocolError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	clientTransport, serverTransport := memory.NewPair()
	defer clientTransport.Close()
	defer serverTransport.Close()

	serverDone := make(chan error, 1)
	go func() {
		serverDone <- runFakeProtocolServer(serverTransport)
	}()

	client := NewClient(clientTransport)
	defer client.Close()

	if err := client.Hello(ctx, Hello{Version: Version, Client: "test"}); err != nil {
		t.Fatalf("hello failed: %v", err)
	}

	created, err := client.Create(ctx, CreateParams{
		Command: []string{"bash", "--noprofile", "--norc"},
		Name:    "demo",
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if created.TerminalID != "term-1" || created.State != "running" {
		t.Fatalf("unexpected create result: %#v", created)
	}

	list, err := client.List(ctx)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(list.Terminals) != 1 || list.Terminals[0].ID != "term-1" {
		t.Fatalf("unexpected list result: %#v", list)
	}

	if err := client.SetTags(ctx, "term-1", map[string]string{"role": "shell"}); err != nil {
		t.Fatalf("set tags failed: %v", err)
	}

	attach, err := client.Attach(ctx, "term-1", "collaborator")
	if err != nil {
		t.Fatalf("attach failed: %v", err)
	}
	if attach.Channel != 7 {
		t.Fatalf("unexpected channel: %#v", attach)
	}

	stream, stop := client.Stream(attach.Channel)
	defer stop()

	if err := client.Input(ctx, attach.Channel, []byte("echo hi\n")); err != nil {
		t.Fatalf("input failed: %v", err)
	}

	select {
	case msg := <-stream:
		if msg.Type != TypeOutput || string(msg.Payload) != "stream-data" {
			t.Fatalf("unexpected stream frame: %#v", msg)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for stream output")
	}

	if err := client.Resize(ctx, attach.Channel, 100, 40); err != nil {
		t.Fatalf("resize failed: %v", err)
	}

	snap, err := client.Snapshot(ctx, "term-1", 0, 50)
	if err != nil {
		t.Fatalf("snapshot failed: %v", err)
	}
	if snap.TerminalID != "term-1" || len(snap.Scrollback) != 1 {
		t.Fatalf("unexpected snapshot result: %#v", snap)
	}

	err = client.Kill(ctx, "missing")
	if err == nil || !strings.Contains(err.Error(), "protocol error 404") {
		t.Fatalf("expected protocol error 404, got %v", err)
	}

	if _, err := client.List(ctx); err == nil {
		t.Fatal("expected list after server close to fail")
	}

	if err := <-serverDone; err != nil {
		t.Fatalf("fake server failed: %v", err)
	}
}

func TestClientEvents(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	clientTransport, serverTransport := memory.NewPair()
	defer clientTransport.Close()
	defer serverTransport.Close()

	serverDone := make(chan error, 1)
	go func() {
		serverDone <- runFakeEventServer(serverTransport)
	}()

	client := NewClient(clientTransport)
	defer client.Close()

	if err := client.Hello(ctx, Hello{Version: Version, Client: "test"}); err != nil {
		t.Fatalf("hello failed: %v", err)
	}

	events, err := client.Events(ctx, EventsParams{
		TerminalID: "term-1",
		Types:      []EventType{EventTerminalRemoved},
	})
	if err != nil {
		t.Fatalf("events subscribe failed: %v", err)
	}

	select {
	case evt, ok := <-events:
		if !ok {
			t.Fatal("expected event channel to stay open")
		}
		if evt.Type != EventTerminalRemoved || evt.TerminalID != "term-1" {
			t.Fatalf("unexpected event: %#v", evt)
		}
		if evt.Removed == nil || evt.Removed.Reason != "expired" {
			t.Fatalf("unexpected removed payload: %#v", evt)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for event")
	}

	if err := <-serverDone; err != nil {
		t.Fatalf("fake server failed: %v", err)
	}
}

func TestClientAttachBuffersFramesThatArriveBeforeStreamRegistration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	clientTransport, serverTransport := memory.NewPair()
	defer clientTransport.Close()
	defer serverTransport.Close()

	serverDone := make(chan error, 1)
	go func() {
		serverDone <- runBufferedAttachServer(serverTransport)
	}()

	client := NewClient(clientTransport)
	defer client.Close()

	if err := client.Hello(ctx, Hello{Version: Version, Client: "test"}); err != nil {
		t.Fatalf("hello failed: %v", err)
	}

	attach, err := client.Attach(ctx, "term-1", "collaborator")
	if err != nil {
		t.Fatalf("attach failed: %v", err)
	}

	stream, stop := client.Stream(attach.Channel)
	defer stop()

	select {
	case msg := <-stream:
		if msg.Type != TypeOutput || string(msg.Payload) != "early-output" {
			t.Fatalf("unexpected buffered stream frame: %#v", msg)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for buffered output")
	}

	select {
	case msg := <-stream:
		if msg.Type != TypeClosed {
			t.Fatalf("expected closed frame, got %#v", msg)
		}
		code, err := DecodeClosedPayload(msg.Payload)
		if err != nil {
			t.Fatalf("decode closed payload failed: %v", err)
		}
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for buffered closed frame")
	}

	if err := <-serverDone; err != nil {
		t.Fatalf("fake server failed: %v", err)
	}
}

func TestClientCloseWaitsForReadLoopAndUnblocksPendingRequest(t *testing.T) {
	clientTransport, serverTransport := memory.NewPair()
	defer serverTransport.Close()

	client := NewClient(clientTransport)

	errCh := make(chan error, 1)
	go func() {
		_, err := client.List(context.Background())
		errCh <- err
	}()

	frameReceived := make(chan struct{})
	go func() {
		_, _ = serverTransport.Recv()
		close(frameReceived)
	}()

	select {
	case <-frameReceived:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for request frame")
	}

	if err := client.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	select {
	case <-client.done:
	default:
		t.Fatal("expected Close to wait for readLoop shutdown")
	}

	select {
	case err := <-errCh:
		if !errors.Is(err, io.EOF) {
			t.Fatalf("expected EOF from pending request, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for pending request to fail")
	}
}

func runFakeProtocolServer(tr *memory.Transport) error {
	if err := expectHello(tr); err != nil {
		return err
	}
	if err := respondHello(tr); err != nil {
		return err
	}

	req, err := expectRequest(tr, "create")
	if err != nil {
		return err
	}
	createResult, _ := json.Marshal(CreateResult{TerminalID: "term-1", State: "running"})
	if err := sendResponse(tr, req.ID, createResult); err != nil {
		return err
	}

	req, err = expectRequest(tr, "list")
	if err != nil {
		return err
	}
	listResult, _ := json.Marshal(ListResult{
		Terminals: []TerminalInfo{{
			ID:    "term-1",
			Name:  "demo",
			State: "running",
		}},
	})
	if err := sendResponse(tr, req.ID, listResult); err != nil {
		return err
	}

	req, err = expectRequest(tr, "set-tags")
	if err != nil {
		return err
	}
	var setTags SetTagsParams
	if err := json.Unmarshal(req.Params, &setTags); err != nil {
		return err
	}
	if setTags.TerminalID != "term-1" || setTags.Tags["role"] != "shell" {
		return fmt.Errorf("unexpected set-tags params: %#v", setTags)
	}
	if err := sendResponse(tr, req.ID, json.RawMessage(`{}`)); err != nil {
		return err
	}

	req, err = expectRequest(tr, "attach")
	if err != nil {
		return err
	}
	attachResult, _ := json.Marshal(AttachResult{Mode: "collaborator", Channel: 7})
	if err := sendResponse(tr, req.ID, attachResult); err != nil {
		return err
	}

	channel, typ, payload, err := recvFrame(tr)
	if err != nil {
		return err
	}
	if channel != 7 || typ != TypeInput || string(payload) != "echo hi\n" {
		return fmt.Errorf("unexpected input frame: channel=%d type=%d payload=%q", channel, typ, string(payload))
	}
	if err := sendFrame(tr, 7, TypeOutput, []byte("stream-data")); err != nil {
		return err
	}

	channel, typ, payload, err = recvFrame(tr)
	if err != nil {
		return err
	}
	if channel != 7 || typ != TypeResize {
		return fmt.Errorf("unexpected resize frame: channel=%d type=%d", channel, typ)
	}
	cols, rows, err := DecodeResizePayload(payload)
	if err != nil {
		return err
	}
	if cols != 100 || rows != 40 {
		return fmt.Errorf("unexpected resize payload: %dx%d", cols, rows)
	}

	req, err = expectRequest(tr, "snapshot")
	if err != nil {
		return err
	}
	snapshotResult := json.RawMessage(`{
		"terminal_id":"term-1",
		"size":{"cols":80,"rows":24},
		"screen":{"is_alternate":false,"rows":[{"cells":[{"r":"h"},{"r":"i"}]}]},
		"scrollback":[{"cells":[{"r":"o"},{"r":"k"}]}],
		"cursor":{"row":0,"col":2,"visible":true,"shape":"block"},
		"modes":{"alternate_screen":false,"mouse_tracking":false,"bracketed_paste":false,"application_cursor":false,"auto_wrap":true},
		"timestamp":"2026-03-18T00:00:00Z"
	}`)
	if err := sendResponse(tr, req.ID, snapshotResult); err != nil {
		return err
	}

	req, err = expectRequest(tr, "kill")
	if err != nil {
		return err
	}
	if err := sendError(tr, req.ID, 404, "missing"); err != nil {
		return err
	}

	return tr.Close()
}

func runFakeEventServer(tr *memory.Transport) error {
	if err := expectHello(tr); err != nil {
		return err
	}
	if err := respondHello(tr); err != nil {
		return err
	}

	req, err := expectRequest(tr, "events")
	if err != nil {
		return err
	}
	var params EventsParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return err
	}
	if params.TerminalID != "term-1" || len(params.Types) != 1 || params.Types[0] != EventTerminalRemoved {
		return fmt.Errorf("unexpected events params: %#v", params)
	}

	if err := sendResponse(tr, req.ID, json.RawMessage(`{}`)); err != nil {
		return err
	}

	payload, _ := json.Marshal(Event{
		Type:       EventTerminalRemoved,
		TerminalID: "term-1",
		Timestamp:  time.Date(2026, 3, 18, 0, 0, 0, 0, time.UTC),
		Removed:    &TerminalRemovedData{Reason: "expired"},
	})
	if err := sendFrame(tr, 0, TypeEvent, payload); err != nil {
		return err
	}
	return tr.Close()
}

func runBufferedAttachServer(tr *memory.Transport) error {
	if err := expectHello(tr); err != nil {
		return err
	}
	if err := respondHello(tr); err != nil {
		return err
	}

	req, err := expectRequest(tr, "attach")
	if err != nil {
		return err
	}
	if err := sendFrame(tr, 7, TypeOutput, []byte("early-output")); err != nil {
		return err
	}
	if err := sendFrame(tr, 7, TypeClosed, EncodeClosedPayload(0)); err != nil {
		return err
	}
	attachResult, _ := json.Marshal(AttachResult{Mode: "collaborator", Channel: 7})
	return sendResponse(tr, req.ID, attachResult)
}

func expectHello(tr *memory.Transport) error {
	channel, typ, payload, err := recvFrame(tr)
	if err != nil {
		return err
	}
	if channel != 0 || typ != TypeHello {
		return fmt.Errorf("unexpected hello frame: channel=%d type=%d", channel, typ)
	}
	var msg Hello
	return json.Unmarshal(payload, &msg)
}

func respondHello(tr *memory.Transport) error {
	payload, _ := json.Marshal(Hello{Version: Version, Server: "fake"})
	return sendFrame(tr, 0, TypeHello, payload)
}

func expectRequest(tr *memory.Transport, method string) (Request, error) {
	channel, typ, payload, err := recvFrame(tr)
	if err != nil {
		return Request{}, err
	}
	if channel != 0 || typ != TypeRequest {
		return Request{}, fmt.Errorf("unexpected request frame: channel=%d type=%d", channel, typ)
	}
	var req Request
	if err := json.Unmarshal(payload, &req); err != nil {
		return Request{}, err
	}
	if req.Method != method {
		return Request{}, fmt.Errorf("unexpected method: %s", req.Method)
	}
	return req, nil
}

func sendResponse(tr *memory.Transport, id uint64, result json.RawMessage) error {
	payload, _ := json.Marshal(Response{ID: id, Result: result})
	return sendFrame(tr, 0, TypeResponse, payload)
}

func sendError(tr *memory.Transport, id uint64, code int, message string) error {
	payload, _ := json.Marshal(ErrorMessage{
		ID: id,
		Error: ProtocolError{
			Code:    code,
			Message: message,
		},
	})
	return sendFrame(tr, 0, TypeError, payload)
}

func sendFrame(tr *memory.Transport, channel uint16, typ uint8, payload []byte) error {
	frame, err := EncodeFrame(channel, typ, payload)
	if err != nil {
		return err
	}
	return tr.Send(frame)
}

func recvFrame(tr *memory.Transport) (uint16, uint8, []byte, error) {
	frame, err := tr.Recv()
	if err != nil {
		return 0, 0, nil, err
	}
	return DecodeFrame(frame)
}
