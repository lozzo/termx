package core

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/anytty/anytty/internal/protocol"
	"github.com/anytty/anytty/proto/apipb"
	"github.com/anytty/anytty/proto/wire"
	"github.com/anytty/anytty/shared/transport/memory"
)

func TestProtocolSessionRejectsExcessRequestWithoutBlockingReadLoop(t *testing.T) {
	executor := &blockingProtocolExecutor{started: make(chan struct{}), release: make(chan struct{})}
	server := NewServer(
		WithProtocolSessionLimits(ProtocolSessionLimits{MaxInFlightRequests: 1}),
		WithApplicationExecutorFactory(func(ApplicationSessionPort) ApplicationExecutor { return executor }),
	)
	client, daemon := memory.NewPair()
	done := make(chan error, 1)
	go func() {
		done <- newProtocolSession(server, daemon, fullDaemonTransportScope()).run(context.Background())
	}()

	helloPayload, err := protocol.EncodeHelloPayload(protocol.Hello{Version: wire.Version, Client: "limit-test"})
	if err != nil {
		t.Fatal(err)
	}
	sendProtocolFrame(t, client, wire.TypeHello, helloPayload)
	if _, typ, _ := receiveProtocolFrame(t, client); typ != wire.TypeHello {
		t.Fatalf("hello response type = %d", typ)
	}
	commandPayload, err := protocol.EncodeApplicationCommand(&apipb.CommandEnvelope{})
	if err != nil {
		t.Fatal(err)
	}
	sendProtocolRequest(t, client, protocol.Request{ID: 1, Method: "api.execute", Params: commandPayload})
	select {
	case <-executor.started:
	case <-time.After(time.Second):
		t.Fatal("first request did not enter executor")
	}
	sendProtocolRequest(t, client, protocol.Request{ID: 2, Method: "api.execute", Params: commandPayload})
	_, typ, payload := receiveProtocolFrame(t, client)
	if typ != wire.TypeError {
		t.Fatalf("excess request response type = %d", typ)
	}
	message, err := protocol.DecodeErrorPayload(payload)
	if err != nil {
		t.Fatal(err)
	}
	if message.ID != 2 || message.Error.Code != protocolErrorExhausted {
		t.Fatalf("excess request error = %#v", message)
	}
	close(executor.release)
	_, typ, payload = receiveProtocolFrame(t, client)
	if typ != wire.TypeResponse {
		t.Fatalf("first request response type = %d", typ)
	}
	response, err := protocol.DecodeResponsePayload(payload)
	if err != nil || response.ID != 1 {
		t.Fatalf("first response = %#v, %v", response, err)
	}
	_ = client.Close()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, io.EOF) {
			t.Fatalf("session exit = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("session did not stop")
	}
}

func TestProtocolChannelAllocatorWrapsAcrossResourceKindsAndReleases(t *testing.T) {
	server := NewServer(WithProtocolSessionLimits(ProtocolSessionLimits{
		MaxResources: 8, MaxAttachments: 4, MaxFileTransfers: 4,
	}))
	session := newProtocolSession(server, nil, fullDaemonTransportScope())
	session.nextChannel = ^uint16(0) - 1
	attachmentChannel, err := session.reserveAttachmentChannel()
	if err != nil {
		t.Fatal(err)
	}
	fileChannel, err := session.reserveFileChannel()
	if err != nil {
		t.Fatal(err)
	}
	if attachmentChannel != ^uint16(0) || fileChannel != 1 {
		t.Fatalf("wrapped channels = attachment:%d file:%d", attachmentChannel, fileChannel)
	}
	session.releaseChannel(attachmentChannel, protocolChannelAttachment)
	session.nextChannel = ^uint16(0) - 1
	reused, err := session.reserveFileChannel()
	if err != nil {
		t.Fatal(err)
	}
	if reused != ^uint16(0) {
		t.Fatalf("released channel was not reusable: %d", reused)
	}
}

func TestProtocolChannelAllocatorReturnsExplicitExhaustion(t *testing.T) {
	server := NewServer(WithProtocolSessionLimits(ProtocolSessionLimits{
		MaxResources: int(^uint16(0)), MaxAttachments: int(^uint16(0)),
	}))
	session := newProtocolSession(server, nil, fullDaemonTransportScope())
	for channel := uint32(1); channel <= uint32(^uint16(0)); channel++ {
		session.channelKinds[uint16(channel)] = protocolChannelAttachment
	}
	_, err := session.reserveAttachmentChannel()
	if !errors.Is(err, ErrProtocolResourceExhausted) {
		t.Fatalf("channel exhaustion error = %v", err)
	}
}

func TestProtocolSessionAggregateResourceLimitSpansKinds(t *testing.T) {
	server := NewServer(WithProtocolSessionLimits(ProtocolSessionLimits{
		MaxResources: 2, MaxAttachments: 2, MaxFileTransfers: 2, MaxEventSubscriptions: 2,
	}))
	session := newProtocolSession(server, nil, fullDaemonTransportScope())
	attachmentChannel, err := session.reserveAttachmentChannel()
	if err != nil {
		t.Fatal(err)
	}
	fileChannel, err := session.reserveFileChannel()
	if err != nil {
		t.Fatal(err)
	}
	if err := session.reserveEventSubscription(); !errors.Is(err, ErrProtocolResourceExhausted) {
		t.Fatalf("aggregate resource exhaustion error = %v", err)
	}
	session.releaseChannel(attachmentChannel, protocolChannelAttachment)
	if err := session.reserveEventSubscription(); err != nil {
		t.Fatalf("released attachment did not restore aggregate capacity: %v", err)
	}
	session.releaseChannel(fileChannel, protocolChannelFileTransfer)
	session.releaseEventSubscription()
}

func TestProtocolSessionEventSubscriptionLimitReleasesCapacity(t *testing.T) {
	server := NewServer(WithProtocolSessionLimits(ProtocolSessionLimits{
		MaxResources: 1, MaxEventSubscriptions: 1,
	}))
	session := newProtocolSession(server, nil, fullDaemonTransportScope())
	encoder := func(Event, []byte) ([]byte, error) { return nil, nil }
	token, err := session.ApplicationEventSubscribe(context.Background(), EventFilter{}, encoder)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.ApplicationEventSubscribe(context.Background(), EventFilter{}, encoder); !errors.Is(err, ErrProtocolResourceExhausted) {
		t.Fatalf("event subscription exhaustion error = %v", err)
	}
	if err := session.ReleaseApplicationResource(context.Background(), token); err != nil {
		t.Fatal(err)
	}
	if _, err := session.ApplicationEventSubscribe(context.Background(), EventFilter{}, encoder); err != nil {
		t.Fatalf("released subscription did not restore capacity: %v", err)
	}
	session.stopEvents()
}

type blockingProtocolExecutor struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (executor *blockingProtocolExecutor) Execute(context.Context, *apipb.CommandEnvelope) *apipb.ResultEnvelope {
	executor.once.Do(func() { close(executor.started) })
	<-executor.release
	return &apipb.ResultEnvelope{Result: &apipb.ResultEnvelope_Acknowledge{Acknowledge: &apipb.AcknowledgeResult{}}}
}

func sendProtocolRequest(t *testing.T, connection *memory.Transport, request protocol.Request) {
	t.Helper()
	payload, err := protocol.EncodeRequestPayload(request)
	if err != nil {
		t.Fatal(err)
	}
	sendProtocolFrame(t, connection, wire.TypeRequest, payload)
}
