package clientruntimeadapter

import (
	"context"
	"sync"
	"testing"

	clientprotocol "github.com/muxvia/muxvia/client/adapter/protocol"
	clientendpoint "github.com/muxvia/muxvia/client/endpoint"
	clientruntime "github.com/muxvia/muxvia/client/runtime"
	"github.com/muxvia/muxvia/proto/apipb"
	"github.com/muxvia/muxvia/tui/port"
	"google.golang.org/protobuf/proto"
)

func TestEndpointApplicationRouterAcquiresOwningEndpoint(t *testing.T) {
	local := newRouterReady("local", "/Users/local")
	remote := newRouterReady("al", "/root")
	runtime := &routerRuntime{sessions: map[clientendpoint.EndpointID]clientruntime.ApplicationReadyPeerSession{
		"al": remote,
	}}
	initial, err := clientprotocol.NewRuntimeApplicationClient(local, runtime)
	if err != nil {
		t.Fatal(err)
	}
	router, err := NewEndpointApplicationRouter("local", initial)
	if err != nil {
		t.Fatal(err)
	}
	defer router.Close()

	defaults, err := router.Defaults(context.Background(), port.PathDefaultsRequest{EndpointID: "al"})
	if err != nil {
		t.Fatal(err)
	}
	if defaults.EndpointID != "al" || defaults.DefaultCWD != "/root" || len(defaults.DefaultCommand) != 1 || defaults.DefaultCommand[0] != "/bin/zsh" {
		t.Fatalf("remote defaults = %#v", defaults)
	}
	if len(runtime.requests) != 1 || runtime.requests[0].EndpointID != "al" {
		t.Fatalf("acquire requests = %#v", runtime.requests)
	}

	if _, err := router.Defaults(context.Background(), port.PathDefaultsRequest{EndpointID: "al"}); err != nil {
		t.Fatal(err)
	}
	if len(runtime.requests) != 1 {
		t.Fatalf("cached remote session was reacquired: %#v", runtime.requests)
	}
	if err := router.Close(); err != nil {
		t.Fatal(err)
	}
	if remote.closeCount != 1 {
		t.Fatalf("remote consumer close count = %d", remote.closeCount)
	}
	if local.closeCount != 0 {
		t.Fatalf("router closed composition-owned initial client: %d", local.closeCount)
	}
}

type routerRuntime struct {
	mu       sync.Mutex
	sessions map[clientendpoint.EndpointID]clientruntime.ApplicationReadyPeerSession
	requests []clientruntime.ConnectRequest
}

func (runtime *routerRuntime) AcquireSession(_ context.Context, request clientruntime.ConnectRequest) (clientruntime.ApplicationReadyPeerSession, error) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	runtime.requests = append(runtime.requests, request)
	return runtime.sessions[request.EndpointID], nil
}

func (*routerRuntime) EnsureSession(context.Context, clientruntime.ConnectRequest) (clientruntime.SessionLease, error) {
	return clientruntime.SessionLease{}, nil
}

func (*routerRuntime) Disconnect(context.Context, clientruntime.DisconnectRequest) error { return nil }

func (*routerRuntime) WatchEndpoint(context.Context, clientendpoint.EndpointID) (<-chan clientruntime.EndpointEvent, error) {
	return make(chan clientruntime.EndpointEvent), nil
}

type routerReady struct {
	stamp      clientruntime.EndpointSessionStamp
	cwd        string
	done       chan struct{}
	closeOnce  sync.Once
	closeCount int
}

func newRouterReady(endpointID clientendpoint.EndpointID, cwd string) *routerReady {
	return &routerReady{stamp: clientruntime.EndpointSessionStamp{EndpointID: endpointID, RouteID: "route", Generation: 1}, cwd: cwd, done: make(chan struct{})}
}

func (ready *routerReady) Stamp() clientruntime.EndpointSessionStamp { return ready.stamp }
func (*routerReady) ObservedPath() string                            { return "test" }
func (*routerReady) Readiness() clientruntime.ReadyPeerSessionEvidence {
	return clientruntime.ReadyPeerSessionEvidence{}
}
func (ready *routerReady) Done() <-chan struct{} { return ready.done }
func (*routerReady) Err() error                  { return nil }
func (ready *routerReady) Close() error {
	ready.closeOnce.Do(func() {
		ready.closeCount++
		close(ready.done)
	})
	return nil
}
func (ready *routerReady) ExecuteApplication(_ context.Context, command *apipb.CommandEnvelope) (*apipb.ResultEnvelope, error) {
	return &apipb.ResultEnvelope{
		RequestId:     command.GetContext().GetRequestId(),
		OriginSession: proto.Clone(command.GetContext().GetSession()).(*apipb.EndpointSessionStamp),
		Result: &apipb.ResultEnvelope_TerminalDefaults{TerminalDefaults: &apipb.TerminalDefaultsResult{Defaults: &apipb.TerminalDefaults{
			DefaultCommand: []string{"/bin/zsh"}, DefaultCwd: ready.cwd,
		}}},
	}, nil
}
func (*routerReady) ApplicationEvents(context.Context) (<-chan *apipb.EventEnvelope, error) {
	return make(chan *apipb.EventEnvelope), nil
}

var _ clientruntime.ApplicationRuntime = (*routerRuntime)(nil)
var _ clientruntime.ApplicationReadyPeerSession = (*routerReady)(nil)
