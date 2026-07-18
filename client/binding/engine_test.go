package binding

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/lozzow/termx/client/endpoint"
	clientruntime "github.com/lozzow/termx/client/runtime"
	"github.com/lozzow/termx/proto/apipb"
	"github.com/lozzow/termx/proto/bindingpb"
	"github.com/lozzow/termx/proto/wirepb"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

func TestEngineUsesProtoBytesOpaqueHandlesAndOrderedEvents(t *testing.T) {
	session := newBindingSession()
	host := &bindingHost{session: session}
	engine, err := NewEngineWithEventCapacity(host, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	openRequest := &bindingpb.OpenSessionRequest{
		RequestId: "open-1", EndpointId: "studio", RouteOverride: "cloud", Intent: bindingpb.ConnectIntent_CONNECT_INTENT_INTERACTIVE,
	}
	openRequest.ProtoReflect().SetUnknown(protowire.AppendVarint(protowire.AppendTag(nil, 99, protowire.VarintType), 7))
	openPayload, err := proto.Marshal(openRequest)
	if err != nil {
		t.Fatal(err)
	}
	openOperation, err := engine.OpenSession(openPayload)
	if err != nil {
		t.Fatal(err)
	}
	openEvent := nextBindingEvent(t, engine)
	if openEvent.GetSequence() != 1 || openEvent.GetAbiVersion() != ABIVersion || openEvent.GetOpenSession().GetOperationHandle() != openOperation {
		t.Fatalf("open event = %#v", openEvent)
	}
	if len(host.request.ProtoReflect().GetUnknown()) == 0 {
		t.Fatal("open request unknown fields were discarded")
	}
	sessionHandle := openEvent.GetOpenSession().GetSessionHandle()
	if sessionHandle == 0 || openEvent.GetOpenSession().GetSession().GetGeneration() != 3 {
		t.Fatalf("open result = %#v", openEvent.GetOpenSession())
	}
	if err := engine.Release(openOperation); err != nil {
		t.Fatal(err)
	}

	command := &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_TerminalList{TerminalList: &apipb.TerminalListCommand{}}}
	command.ProtoReflect().SetUnknown(protowire.AppendVarint(protowire.AppendTag(nil, 999, protowire.VarintType), 42))
	commandPayload, err := proto.Marshal(command)
	if err != nil {
		t.Fatal(err)
	}
	executeOperation, err := engine.Execute(sessionHandle, commandPayload)
	if err != nil {
		t.Fatal(err)
	}
	executeEvent := nextBindingEvent(t, engine)
	if executeEvent.GetSequence() != 2 || executeEvent.GetExecute().GetOperationHandle() != executeOperation || executeEvent.GetExecute().GetResult().GetTerminalList() == nil {
		t.Fatalf("execute event = %#v", executeEvent)
	}
	if len(session.command.ProtoReflect().GetUnknown()) == 0 {
		t.Fatal("CommandEnvelope unknown fields were discarded")
	}
	if err := engine.Release(executeOperation); err != nil {
		t.Fatal(err)
	}

	session.events <- &apipb.EventEnvelope{EventId: "event-1", Event: &apipb.EventEnvelope_StorageChanged{StorageChanged: &apipb.StorageChangedEvent{}}}
	applicationEvent := nextBindingEvent(t, engine)
	if applicationEvent.GetSequence() != 3 || applicationEvent.GetApplication().GetSessionHandle() != sessionHandle || applicationEvent.GetApplication().GetEvent().GetEventId() != "event-1" {
		t.Fatalf("application event = %#v", applicationEvent)
	}
	if err := engine.CloseSession(sessionHandle); err != nil {
		t.Fatal(err)
	}
	closedEvent := nextBindingEvent(t, engine)
	if closedEvent.GetSequence() != 4 || closedEvent.GetSessionClosed().GetSessionHandle() != sessionHandle {
		t.Fatalf("closed event = %#v", closedEvent)
	}
	if err := engine.Release(sessionHandle); err != nil {
		t.Fatal(err)
	}
}

func TestEngineCancellationProducesTypedResult(t *testing.T) {
	session := newBindingSession()
	started := make(chan struct{})
	release := make(chan struct{})
	session.execute = func(_ context.Context, _ *apipb.CommandEnvelope) (*apipb.ResultEnvelope, error) {
		close(started)
		<-release
		return terminalListBindingResult(), nil
	}
	engine, err := NewEngine(&bindingHost{session: session})
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	sessionHandle := openBindingSession(t, engine)
	payload, err := proto.Marshal(&apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_TerminalList{TerminalList: &apipb.TerminalListCommand{}}})
	if err != nil {
		t.Fatal(err)
	}
	operation, err := engine.Execute(sessionHandle, payload)
	if err != nil {
		t.Fatal(err)
	}
	<-started
	if err := engine.Release(operation); !errors.Is(err, ErrHandleActive) {
		t.Fatalf("active operation release error = %v", err)
	}
	if err := engine.Cancel(operation); err != nil {
		t.Fatal(err)
	}
	close(release)
	event := nextBindingEvent(t, engine)
	if event.GetExecute().GetOperationHandle() != operation || event.GetExecute().GetError().GetCode() != apipb.ApiErrorCode_API_ERROR_CODE_CANCELLED {
		t.Fatalf("cancel event = %#v", event)
	}
	if err := engine.Release(operation); err != nil {
		t.Fatal(err)
	}
}

func TestEngineResourceStreamUsesProtoFramesAndOpaqueHandle(t *testing.T) {
	session := newBindingSession()
	stream := newBindingResourceStream()
	session.resourceStream = stream
	engine, err := NewEngine(&bindingHost{session: session})
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	sessionHandle := openBindingSession(t, engine)
	resource := &apipb.ResourceHandle{OpaqueToken: []byte{0, 7, 1}, Kind: apipb.ResourceKind_RESOURCE_KIND_FILE_TRANSFER}
	requestPayload, err := proto.Marshal(&bindingpb.OpenResourceStreamRequest{Resource: resource})
	if err != nil {
		t.Fatal(err)
	}
	streamHandle, err := engine.OpenResourceStream(sessionHandle, requestPayload)
	if err != nil {
		t.Fatal(err)
	}
	if streamHandle == 0 || !proto.Equal(session.resource, resource) {
		t.Fatalf("resource stream handle=%d resource=%v", streamHandle, session.resource)
	}
	dataPayload, err := proto.Marshal(&wirepb.FileTransferData{Offset: 0, Data: []byte("upload")})
	if err != nil {
		t.Fatal(err)
	}
	sendPayload, err := proto.Marshal(&bindingpb.ResourceStreamFrame{
		StreamHandle: streamHandle,
		Type:         bindingpb.ResourceStreamFrameType_RESOURCE_STREAM_FRAME_TYPE_FILE_DATA,
		Payload:      dataPayload,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.SendResourceStreamFrame(streamHandle, sendPayload); err != nil {
		t.Fatal(err)
	}
	var sentData wirepb.FileTransferData
	if err := proto.Unmarshal(stream.sentPayload, &sentData); err != nil {
		t.Fatal(err)
	}
	if stream.sentType != 0x21 || string(sentData.GetData()) != "upload" {
		t.Fatalf("sent frame type=%x payload=%q", stream.sentType, stream.sentPayload)
	}
	finishPayload, _ := proto.Marshal(&bindingpb.ResourceStreamFrame{StreamHandle: streamHandle, Type: bindingpb.ResourceStreamFrameType_RESOURCE_STREAM_FRAME_TYPE_FILE_FINISH_AUTO})
	if err := engine.SendResourceStreamFrame(streamHandle, finishPayload); err != nil {
		t.Fatal(err)
	}
	var finish wirepb.FileTransferFinish
	if err := proto.Unmarshal(stream.sentPayload, &finish); err != nil || finish.GetSize() != 6 || len(finish.GetSha256()) != sha256.Size {
		t.Fatalf("automatic finish = %#v err=%v", finish, err)
	}
	stream.frames <- bindingResourceFrame{typ: 0x24, payload: []byte("done")}
	event := nextBindingEvent(t, engine)
	if event.GetResourceStreamFrame().GetStreamHandle() != streamHandle ||
		event.GetResourceStreamFrame().GetType() != bindingpb.ResourceStreamFrameType_RESOURCE_STREAM_FRAME_TYPE_FILE_RESULT ||
		string(event.GetResourceStreamFrame().GetPayload()) != "done" {
		t.Fatalf("resource stream event = %#v", event)
	}
	if err := engine.CloseResourceStream(streamHandle); err != nil {
		t.Fatal(err)
	}
	closed := nextBindingEvent(t, engine)
	if closed.GetResourceStreamClosed().GetStreamHandle() != streamHandle {
		t.Fatalf("resource stream closed event = %#v", closed)
	}
	if err := engine.Release(streamHandle); err != nil {
		t.Fatal(err)
	}
}

func TestEngineCloseUnblocksBackpressuredProducer(t *testing.T) {
	session := newBindingSession()
	engine, err := NewEngineWithEventCapacity(&bindingHost{session: session}, 1)
	if err != nil {
		t.Fatal(err)
	}
	openPayload, _ := proto.Marshal(&bindingpb.OpenSessionRequest{
		RequestId: "close-backpressure", EndpointId: "studio", Intent: bindingpb.ConnectIntent_CONNECT_INTENT_INTERACTIVE,
	})
	if _, err := engine.OpenSession(openPayload); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for len(engine.events) != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	engine.mu.Lock()
	var sessionHandle uint64
	for handle := range engine.sessions {
		sessionHandle = handle
	}
	engine.mu.Unlock()
	session.events <- &apipb.EventEnvelope{EventId: "blocked-event", Event: &apipb.EventEnvelope_StorageChanged{StorageChanged: &apipb.StorageChangedEvent{}}}
	closed := make(chan struct{})
	go func() {
		_ = engine.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("engine close blocked behind full event queue")
	}
	if sessionHandle == 0 {
		t.Fatal("session was not established before close")
	}
}

func TestEngineAppliesBoundedBackpressureWithoutDroppingResults(t *testing.T) {
	session := newBindingSession()
	executed := make(chan struct{})
	session.execute = func(context.Context, *apipb.CommandEnvelope) (*apipb.ResultEnvelope, error) {
		close(executed)
		return terminalListBindingResult(), nil
	}
	engine, err := NewEngineWithEventCapacity(&bindingHost{session: session}, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	openPayload, _ := proto.Marshal(&bindingpb.OpenSessionRequest{
		RequestId: "open-backpressure", EndpointId: "studio", Intent: bindingpb.ConnectIntent_CONNECT_INTENT_INTERACTIVE,
	})
	if _, err := engine.OpenSession(openPayload); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for len(engine.events) != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	engine.mu.Lock()
	var sessionHandle uint64
	for handle := range engine.sessions {
		sessionHandle = handle
	}
	engine.mu.Unlock()
	if sessionHandle == 0 {
		t.Fatal("open session was not published before event backpressure")
	}
	commandPayload, _ := proto.Marshal(&apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_TerminalList{TerminalList: &apipb.TerminalListCommand{}}})
	if _, err := engine.Execute(sessionHandle, commandPayload); err != nil {
		t.Fatal(err)
	}
	<-executed
	openEvent := nextBindingEvent(t, engine)
	if openEvent.GetOpenSession().GetSessionHandle() != sessionHandle {
		t.Fatalf("open event = %#v", openEvent)
	}
	result := nextBindingEvent(t, engine)
	if result.GetExecute().GetResult().GetTerminalList() == nil {
		t.Fatalf("backpressured result was lost: %#v", result)
	}
}

func openBindingSession(t *testing.T, engine *Engine) uint64 {
	t.Helper()
	payload, err := proto.Marshal(&bindingpb.OpenSessionRequest{
		RequestId: "open", EndpointId: "studio", Intent: bindingpb.ConnectIntent_CONNECT_INTENT_INTERACTIVE,
	})
	if err != nil {
		t.Fatal(err)
	}
	operation, err := engine.OpenSession(payload)
	if err != nil {
		t.Fatal(err)
	}
	event := nextBindingEvent(t, engine)
	if event.GetOpenSession().GetError() != nil {
		t.Fatalf("open error = %#v", event.GetOpenSession().GetError())
	}
	if err := engine.Release(operation); err != nil {
		t.Fatal(err)
	}
	return event.GetOpenSession().GetSessionHandle()
}

func nextBindingEvent(t *testing.T, engine *Engine) *bindingpb.EventEnvelope {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	payload, err := engine.NextEvent(ctx)
	if err != nil {
		t.Fatal(err)
	}
	event := &bindingpb.EventEnvelope{}
	if err := proto.Unmarshal(payload, event); err != nil {
		t.Fatal(err)
	}
	return event
}

type bindingHost struct {
	session *bindingSession
	request *bindingpb.OpenSessionRequest
}

func (host *bindingHost) OpenSession(_ context.Context, request *bindingpb.OpenSessionRequest) (clientruntime.ApplicationReadySession, error) {
	host.request = proto.Clone(request).(*bindingpb.OpenSessionRequest)
	return host.session, nil
}

type bindingSession struct {
	stamp          clientruntime.EndpointSessionStamp
	done           chan struct{}
	events         chan *apipb.EventEnvelope
	closeOnce      sync.Once
	err            error
	command        *apipb.CommandEnvelope
	execute        func(context.Context, *apipb.CommandEnvelope) (*apipb.ResultEnvelope, error)
	resource       *apipb.ResourceHandle
	resourceStream clientruntime.ResourceStream
}

func (session *bindingSession) OpenResourceStream(resource *apipb.ResourceHandle) (clientruntime.ResourceStream, error) {
	session.resource = proto.Clone(resource).(*apipb.ResourceHandle)
	if session.resourceStream == nil {
		return nil, errors.New("resource stream unavailable")
	}
	return session.resourceStream, nil
}

type bindingResourceFrame struct {
	typ     uint8
	payload []byte
}

type bindingResourceStream struct {
	frames      chan bindingResourceFrame
	closed      chan struct{}
	closeOnce   sync.Once
	sentType    uint8
	sentPayload []byte
}

func newBindingResourceStream() *bindingResourceStream {
	return &bindingResourceStream{frames: make(chan bindingResourceFrame, 4), closed: make(chan struct{})}
}

func (stream *bindingResourceStream) Receive(ctx context.Context) (uint8, []byte, error) {
	select {
	case <-ctx.Done():
		return 0, nil, ctx.Err()
	case <-stream.closed:
		return 0, nil, io.EOF
	case frame := <-stream.frames:
		return frame.typ, append([]byte(nil), frame.payload...), nil
	}
}

func (stream *bindingResourceStream) Send(_ context.Context, typ uint8, payload []byte) error {
	stream.sentType = typ
	stream.sentPayload = append([]byte(nil), payload...)
	return nil
}

func (stream *bindingResourceStream) Close() error {
	stream.closeOnce.Do(func() { close(stream.closed) })
	return nil
}

func newBindingSession() *bindingSession {
	return &bindingSession{
		stamp: clientruntime.EndpointSessionStamp{EndpointID: endpoint.EndpointID("studio"), RouteID: endpoint.RouteID("cloud"), Generation: 3},
		done:  make(chan struct{}), events: make(chan *apipb.EventEnvelope, 8),
	}
}

func (session *bindingSession) Stamp() clientruntime.EndpointSessionStamp { return session.stamp }
func (session *bindingSession) ObservedPath() string                      { return "direct" }
func (session *bindingSession) Done() <-chan struct{}                     { return session.done }
func (session *bindingSession) Err() error                                { return session.err }
func (session *bindingSession) Close() error {
	session.closeOnce.Do(func() { close(session.done) })
	return nil
}
func (session *bindingSession) ExecuteApplication(ctx context.Context, command *apipb.CommandEnvelope) (*apipb.ResultEnvelope, error) {
	session.command = proto.Clone(command).(*apipb.CommandEnvelope)
	if session.execute != nil {
		return session.execute(ctx, command)
	}
	return terminalListBindingResult(), nil
}
func (session *bindingSession) ApplicationEvents(ctx context.Context) (<-chan *apipb.EventEnvelope, error) {
	out := make(chan *apipb.EventEnvelope, 8)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case <-session.done:
				return
			case event := <-session.events:
				select {
				case out <- event:
				case <-ctx.Done():
					return
				case <-session.done:
					return
				}
			}
		}
	}()
	return out, nil
}

func terminalListBindingResult() *apipb.ResultEnvelope {
	return &apipb.ResultEnvelope{Result: &apipb.ResultEnvelope_TerminalList{TerminalList: &apipb.TerminalListResult{}}}
}

var _ clientruntime.ApplicationReadySession = (*bindingSession)(nil)
