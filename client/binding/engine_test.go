package binding

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/lozzow/termx/client/endpoint"
	clientruntime "github.com/lozzow/termx/client/runtime"
	"github.com/lozzow/termx/proto/apipb"
	"github.com/lozzow/termx/proto/bindingpb"
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
	stamp     clientruntime.EndpointSessionStamp
	done      chan struct{}
	events    chan *apipb.EventEnvelope
	closeOnce sync.Once
	err       error
	command   *apipb.CommandEnvelope
	execute   func(context.Context, *apipb.CommandEnvelope) (*apipb.ResultEnvelope, error)
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
