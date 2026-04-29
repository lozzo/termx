package protocol

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-core/transport/memory"
)

var errConcurrentSend = errors.New("concurrent send")

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
	if err := client.SetMetadata(ctx, "term-1", "dev-shell", map[string]string{"role": "shell", "team": "infra"}); err != nil {
		t.Fatalf("set metadata failed: %v", err)
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

func TestClientStreamCancelDropsLateFramesAndKeepsReadLoopAlive(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	clientTransport, serverTransport := memory.NewPair()
	defer clientTransport.Close()
	defer serverTransport.Close()

	serverDone := make(chan error, 1)
	go func() {
		serverDone <- runLateFrameAfterCancelServer(serverTransport)
	}()

	client := NewClient(clientTransport)
	defer client.Close()

	if err := client.Hello(ctx, Hello{Version: Version, Client: "test"}); err != nil {
		t.Fatalf("hello failed: %v", err)
	}

	attach, err := client.Attach(ctx, "term-1", "observer")
	if err != nil {
		t.Fatalf("attach failed: %v", err)
	}
	_, stop := client.Stream(attach.Channel)
	stop()

	stream, stop2 := client.Stream(attach.Channel)
	defer stop2()

	select {
	case frame := <-stream:
		t.Fatalf("expected late frame to be dropped after cancel, got %#v", frame)
	case <-time.After(200 * time.Millisecond):
	}

	if _, err := client.List(ctx); err != nil {
		t.Fatalf("expected client to stay usable after late frame, got %v", err)
	}

	if err := <-serverDone; err != nil {
		t.Fatalf("fake server failed: %v", err)
	}
}

func TestClientStreamCancelKeepsEarlyFramesWhenSameChannelIsReattached(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	clientTransport, serverTransport := memory.NewPair()
	defer clientTransport.Close()
	defer serverTransport.Close()

	serverDone := make(chan error, 1)
	go func() {
		serverDone <- runReusedChannelAttachServer(serverTransport)
	}()

	client := NewClient(clientTransport)
	defer client.Close()

	if err := client.Hello(ctx, Hello{Version: Version, Client: "test"}); err != nil {
		t.Fatalf("hello failed: %v", err)
	}

	first, err := client.Attach(ctx, "term-1", "observer")
	if err != nil {
		t.Fatalf("first attach failed: %v", err)
	}
	_, stop := client.Stream(first.Channel)
	stop()

	second, err := client.Attach(ctx, "term-1", "observer")
	if err != nil {
		t.Fatalf("second attach failed: %v", err)
	}
	stream, stop2 := client.Stream(second.Channel)
	defer stop2()

	select {
	case frame := <-stream:
		if frame.Type != TypeOutput || string(frame.Payload) != "replayed-after-reattach" {
			t.Fatalf("unexpected replayed frame: %#v", frame)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for replayed early frame on reused channel")
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

func TestClientSerializesConcurrentSends(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tr := newConcurrentUnsafeTransport()
	client := NewClient(tr)
	defer client.Close()

	if err := client.Hello(ctx, Hello{Version: Version, Client: "test"}); err != nil {
		t.Fatalf("hello failed: %v", err)
	}

	const workers = 8
	start := make(chan struct{})
	errCh := make(chan error, workers)

	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := client.List(ctx)
			errCh <- err
		}()
	}

	close(start)
	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("expected concurrent lists to succeed, got %v", err)
		}
	}
}

func TestClientStreamCoalescesAdjacentOutputFrames(t *testing.T) {
	stream := newClientStreamWithConfig(4, 0)
	defer stream.close()

	stream.send(StreamFrame{Type: TypeOutput, Payload: []byte("a")})
	waitForClientStreamState(t, stream, func() bool {
		return len(stream.queue) == 0
	})

	stream.send(StreamFrame{Type: TypeOutput, Payload: []byte("b")})
	stream.send(StreamFrame{Type: TypeOutput, Payload: []byte("c")})

	stream.mu.Lock()
	defer stream.mu.Unlock()
	if len(stream.queue) != 1 {
		t.Fatalf("expected one queued output frame, got %d", len(stream.queue))
	}
	frame := stream.queue[0]
	if frame.Type != TypeOutput || string(frame.Payload) != "bc" {
		t.Fatalf("expected merged output payload %q, got %#v", "bc", frame)
	}
}

func TestClientStreamOverflowQueuesSyncLostInsteadOfSilentDrop(t *testing.T) {
	stream := newClientStreamWithConfig(2, 0)
	defer stream.close()

	payload := bytes.Repeat([]byte("x"), MaxFrameSize/2+1)
	stream.send(StreamFrame{Type: TypeOutput, Payload: payload})
	waitForClientStreamState(t, stream, func() bool {
		return len(stream.queue) == 0
	})

	stream.send(StreamFrame{Type: TypeOutput, Payload: payload})
	stream.send(StreamFrame{Type: TypeOutput, Payload: payload})
	stream.send(StreamFrame{Type: TypeOutput, Payload: payload})

	waitForClientStreamState(t, stream, func() bool {
		if len(stream.queue) != 3 {
			return false
		}
		return stream.queue[2].Type == TypeSyncLost
	})

	stream.mu.Lock()
	defer stream.mu.Unlock()
	if len(stream.queue) != 3 {
		t.Fatalf("expected queued overflow state, got %d frames", len(stream.queue))
	}
	frame := stream.queue[2]
	if frame.Type != TypeSyncLost {
		t.Fatalf("expected sync-lost frame after overflow, got %#v", frame)
	}
	dropped, err := DecodeSyncLostPayload(frame.Payload)
	if err != nil {
		t.Fatalf("decode sync-lost payload: %v", err)
	}
	if dropped != uint64(len(payload)) {
		t.Fatalf("expected dropped byte count %d, got %d", len(payload), dropped)
	}
	if stream.pendingDroppedBytes != 0 {
		t.Fatalf("expected dropped bytes to be flushed into sync-lost frame, got %d", stream.pendingDroppedBytes)
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

	req, err = expectRequest(tr, "set_tags")
	if err != nil {
		return err
	}
	var setTags SetTagsParams
	if err := json.Unmarshal(req.Params, &setTags); err != nil {
		return err
	}
	if setTags.TerminalID != "term-1" || setTags.Tags["role"] != "shell" {
		return fmt.Errorf("unexpected set_tags params: %#v", setTags)
	}
	if err := sendResponse(tr, req.ID, json.RawMessage(`{}`)); err != nil {
		return err
	}

	req, err = expectRequest(tr, "set_metadata")
	if err != nil {
		return err
	}
	var setMetadata SetMetadataParams
	if err := json.Unmarshal(req.Params, &setMetadata); err != nil {
		return err
	}
	if setMetadata.TerminalID != "term-1" || setMetadata.Name != "dev-shell" || setMetadata.Tags["team"] != "infra" {
		return fmt.Errorf("unexpected set_metadata params: %#v", setMetadata)
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

func waitForClientStreamState(t *testing.T, stream *clientStream, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		stream.mu.Lock()
		ok := cond()
		stream.mu.Unlock()
		if ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	stream.mu.Lock()
	defer stream.mu.Unlock()
	t.Fatalf("timed out waiting for client stream state: queued=%d dropped=%d", len(stream.queue), stream.pendingDroppedBytes)
}

type concurrentUnsafeTransport struct {
	inFlight atomic.Int32
	recvCh   chan []byte
	done     chan struct{}
	once     sync.Once
}

func newConcurrentUnsafeTransport() *concurrentUnsafeTransport {
	return &concurrentUnsafeTransport{
		recvCh: make(chan []byte, 32),
		done:   make(chan struct{}),
	}
}

func (t *concurrentUnsafeTransport) Send(frame []byte) error {
	select {
	case <-t.done:
		return io.EOF
	default:
	}
	if !t.inFlight.CompareAndSwap(0, 1) {
		return errConcurrentSend
	}
	defer t.inFlight.Store(0)

	time.Sleep(20 * time.Millisecond)

	channel, typ, payload, err := DecodeFrame(frame)
	if err != nil {
		return err
	}
	if channel != 0 {
		return fmt.Errorf("unexpected non-control channel %d", channel)
	}

	switch typ {
	case TypeHello:
		resp, err := json.Marshal(Hello{Version: Version, Server: "test"})
		if err != nil {
			return err
		}
		reply, err := EncodeFrame(0, TypeHello, resp)
		if err != nil {
			return err
		}
		t.recvCh <- reply
		return nil
	case TypeRequest:
		var req Request
		if err := json.Unmarshal(payload, &req); err != nil {
			return err
		}
		if req.Method != "list" {
			return fmt.Errorf("unexpected method %q", req.Method)
		}
		result, err := json.Marshal(ListResult{})
		if err != nil {
			return err
		}
		replyPayload, err := json.Marshal(Response{ID: req.ID, Result: result})
		if err != nil {
			return err
		}
		reply, err := EncodeFrame(0, TypeResponse, replyPayload)
		if err != nil {
			return err
		}
		t.recvCh <- reply
		return nil
	default:
		return fmt.Errorf("unexpected frame type %d", typ)
	}
}

func (t *concurrentUnsafeTransport) Recv() ([]byte, error) {
	select {
	case <-t.done:
		return nil, io.EOF
	case frame, ok := <-t.recvCh:
		if !ok {
			return nil, io.EOF
		}
		return frame, nil
	}
}

func (t *concurrentUnsafeTransport) Close() error {
	t.once.Do(func() {
		close(t.done)
		close(t.recvCh)
	})
	return nil
}

func (t *concurrentUnsafeTransport) Done() <-chan struct{} {
	return t.done
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

func runLateFrameAfterCancelServer(tr *memory.Transport) error {
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
	attachResult, _ := json.Marshal(AttachResult{Mode: "observer", Channel: 7})
	if err := sendResponse(tr, req.ID, attachResult); err != nil {
		return err
	}
	time.Sleep(50 * time.Millisecond)
	if err := sendFrame(tr, 7, TypeOutput, []byte("late-output")); err != nil {
		return err
	}

	req, err = expectRequest(tr, "list")
	if err != nil {
		return err
	}
	listResult, _ := json.Marshal(ListResult{
		Terminals: []TerminalInfo{{ID: "term-1", Name: "demo", State: "running"}},
	})
	if err := sendResponse(tr, req.ID, listResult); err != nil {
		return err
	}
	return nil
}

func runReusedChannelAttachServer(tr *memory.Transport) error {
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
	firstResult, _ := json.Marshal(AttachResult{Mode: "observer", Channel: 7})
	if err := sendResponse(tr, req.ID, firstResult); err != nil {
		return err
	}

	req, err = expectRequest(tr, "attach")
	if err != nil {
		return err
	}
	if err := sendFrame(tr, 7, TypeOutput, []byte("replayed-after-reattach")); err != nil {
		return err
	}
	secondResult, _ := json.Marshal(AttachResult{Mode: "observer", Channel: 7})
	return sendResponse(tr, req.ID, secondResult)
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
