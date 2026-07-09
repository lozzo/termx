package services

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/termx-proto/wire"
	"github.com/lozzow/termx/termx-shared/plugin"
)

func TestProtocolClientControlAdapterCallsTypedMethods(t *testing.T) {
	client := &fakeProtocolClientControlClient{
		registerResult: protocol.ClientSessionRegisterResult{Session: protocol.ClientSessionInfo{SessionID: "tui-1"}},
		listResult:     protocol.ClientSessionListResult{Sessions: []protocol.ClientSessionInfo{{SessionID: "tui-1"}}},
		callResult:     protocol.ClientControlCallResult{RequestID: "req-1", Deliveries: []protocol.ClientControlDelivery{{SessionID: "tui-1", Status: protocol.ClientControlStatusQueued}}},
		unwatchResult:  protocol.ClientControlUnwatchResult{SessionID: "tui-1", Channel: 42, Stopped: true},
		respondResult:  protocol.ClientControlResponseResult{RequestID: "req-1", Accepted: true},
	}
	adapter := ProtocolClientControlAdapter{Client: client}
	ctx := context.Background()

	if result, err := adapter.Register(ctx, protocol.ClientSessionRegisterParams{SessionID: "tui-1"}); err != nil || result.Session.SessionID != "tui-1" {
		t.Fatalf("register result=%#v err=%v", result, err)
	}
	if result, err := adapter.List(ctx, protocol.ClientSessionListParams{IncludeActions: true}); err != nil || len(result.Sessions) != 1 {
		t.Fatalf("list result=%#v err=%v", result, err)
	}
	if result, err := adapter.Call(ctx, protocol.ClientControlCallParams{RequestID: "req-1"}); err != nil || len(result.Deliveries) != 1 {
		t.Fatalf("call result=%#v err=%v", result, err)
	}
	if result, err := adapter.Unwatch(ctx, protocol.ClientControlUnwatchParams{SessionID: "tui-1", Channel: 42}); err != nil || !result.Stopped {
		t.Fatalf("unwatch result=%#v err=%v", result, err)
	}
	if result, err := adapter.Respond(ctx, protocol.ClientControlResponseParams{RequestID: "req-1", SessionID: "tui-1"}); err != nil || !result.Accepted {
		t.Fatalf("respond result=%#v err=%v", result, err)
	}

	want := []string{
		protocol.MethodClientSessionRegister,
		protocol.MethodClientSessionList,
		protocol.MethodClientControlCall,
		protocol.MethodClientControlUnwatch,
		protocol.MethodClientControlRespond,
	}
	if len(client.calls) != len(want) {
		t.Fatalf("unexpected call count %#v", client.calls)
	}
	for i, method := range want {
		if client.calls[i].method != method {
			t.Fatalf("call %d method=%q want %q", i, client.calls[i].method, method)
		}
	}
}

func TestProtocolClientControlAdapterWatchDecodesMailboxFrames(t *testing.T) {
	stream := make(chan protocol.StreamFrame, 4)
	client := &fakeProtocolClientControlClient{
		watchResult: protocol.ClientControlWatchResult{SessionID: "tui-1", Channel: 42},
		stream:      stream,
	}
	adapter := ProtocolClientControlAdapter{Client: client, Buffer: 1}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	invocations, err := adapter.Watch(ctx, protocol.ClientControlWatchParams{SessionID: "tui-1"})
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	if client.streamChannel != 42 {
		t.Fatalf("stream channel=%d want 42", client.streamChannel)
	}

	invocation := protocol.ClientControlInvocation{
		RequestID:   "req-1",
		ActionID:    "acme.deploy.panel.close",
		Params:      []byte(`{"panel":"active"}`),
		Source:      protocol.ClientControlSource{PluginID: "acme.deploy", Kind: "one_shot"},
		Target:      protocol.ClientControlTarget{SessionID: "tui-1"},
		TraceParent: plugin.TraceParent{TraceID: "trace-1", Token: "token-1"},
	}
	payload, err := protocol.EncodeClientControlInvocationPayload(invocation)
	if err != nil {
		t.Fatalf("encode invocation: %v", err)
	}
	stream <- protocol.StreamFrame{Type: wire.TypeResize, Payload: []byte("ignored")}
	stream <- protocol.StreamFrame{Type: wire.TypeClientControl, Payload: payload}

	select {
	case got := <-invocations:
		if got.RequestID != invocation.RequestID || got.Source != invocation.Source || got.TraceParent != invocation.TraceParent {
			t.Fatalf("unexpected invocation %#v", got)
		}
	case <-ctx.Done():
		t.Fatalf("wait invocation: %v", ctx.Err())
	}

	stream <- protocol.StreamFrame{Type: wire.TypeClosed, Payload: wire.EncodeClosedPayload(0)}
	select {
	case _, ok := <-invocations:
		if ok {
			t.Fatal("mailbox should close after TypeClosed")
		}
	case <-ctx.Done():
		t.Fatalf("wait close: %v", ctx.Err())
	}
	if !client.stopped {
		t.Fatal("watch should stop protocol stream")
	}
	if len(client.calls) != 2 || client.calls[1].method != protocol.MethodClientControlUnwatch {
		t.Fatalf("watch close should unwatch server-side mailbox, calls=%#v", client.calls)
	}
	params, ok := client.calls[1].params.(protocol.ClientControlUnwatchParams)
	if !ok || params.SessionID != "tui-1" || params.Channel != 42 {
		t.Fatalf("unexpected unwatch params %#v", client.calls[1].params)
	}
}

type fakeProtocolClientControlCall struct {
	method string
	params any
}

type fakeProtocolClientControlClient struct {
	calls          []fakeProtocolClientControlCall
	registerResult protocol.ClientSessionRegisterResult
	listResult     protocol.ClientSessionListResult
	watchResult    protocol.ClientControlWatchResult
	callResult     protocol.ClientControlCallResult
	unwatchResult  protocol.ClientControlUnwatchResult
	respondResult  protocol.ClientControlResponseResult
	stream         chan protocol.StreamFrame
	streamChannel  uint16
	stopped        bool
}

func (client *fakeProtocolClientControlClient) Call(_ context.Context, method string, params any, out any) error {
	client.calls = append(client.calls, fakeProtocolClientControlCall{method: method, params: params})
	switch method {
	case protocol.MethodClientSessionRegister:
		ptr, ok := out.(*protocol.ClientSessionRegisterResult)
		if !ok {
			return fmt.Errorf("unexpected register out %T", out)
		}
		*ptr = client.registerResult
	case protocol.MethodClientSessionList:
		ptr, ok := out.(*protocol.ClientSessionListResult)
		if !ok {
			return fmt.Errorf("unexpected list out %T", out)
		}
		*ptr = client.listResult
	case protocol.MethodClientControlWatch:
		ptr, ok := out.(*protocol.ClientControlWatchResult)
		if !ok {
			return fmt.Errorf("unexpected watch out %T", out)
		}
		*ptr = client.watchResult
	case protocol.MethodClientControlCall:
		ptr, ok := out.(*protocol.ClientControlCallResult)
		if !ok {
			return fmt.Errorf("unexpected call out %T", out)
		}
		*ptr = client.callResult
	case protocol.MethodClientControlUnwatch:
		ptr, ok := out.(*protocol.ClientControlUnwatchResult)
		if !ok {
			return fmt.Errorf("unexpected unwatch out %T", out)
		}
		*ptr = client.unwatchResult
	case protocol.MethodClientControlRespond:
		ptr, ok := out.(*protocol.ClientControlResponseResult)
		if !ok {
			return fmt.Errorf("unexpected respond out %T", out)
		}
		*ptr = client.respondResult
	default:
		return fmt.Errorf("unexpected method %q", method)
	}
	return nil
}

func (client *fakeProtocolClientControlClient) Stream(channel uint16) (<-chan protocol.StreamFrame, func()) {
	client.streamChannel = channel
	if client.stream == nil {
		client.stream = make(chan protocol.StreamFrame)
	}
	return client.stream, func() { client.stopped = true }
}
