package runtime

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anytty/anytty/cloud/processhealth"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	grpc_health "google.golang.org/grpc/health"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestRuntimeShutdownDrainsStateOwnedGRPCStream(t *testing.T) {
	runtimeUnderTest, stream := startLifecycleStream(t, true)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := runtimeUnderTest.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	waitClosed(t, stream.closerCalled, "State did not invoke the registered stream closer")
	waitClosed(t, stream.exited, "gRPC stream did not drain normally")
	waitClosed(t, stream.serveDone, "gRPC Serve did not exit after graceful drain")
	if stream.canceled.Load() {
		t.Fatal("normal drain required grpc.Server.Stop")
	}
	if err := runtimeUnderTest.Shutdown(context.Background()); err != nil {
		t.Fatalf("repeated Shutdown: %v", err)
	}
}

func TestRuntimeShutdownStopsGRPCStreamAtDeadline(t *testing.T) {
	runtimeUnderTest, stream := startLifecycleStream(t, false)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := runtimeUnderTest.Shutdown(shutdownCtx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown error = %v", err)
	}
	waitClosed(t, stream.closerCalled, "State closer was not called before the gRPC deadline")
	waitClosed(t, stream.exited, "grpc.Server.Stop did not cancel the active stream")
	waitClosed(t, stream.serveDone, "gRPC Serve did not exit after forced stop")
	if !stream.canceled.Load() {
		t.Fatal("deadline fallback did not cancel the active stream")
	}
	if runtimeUnderTest.shutdownComplete || !runtimeUnderTest.health.Alive() {
		t.Fatalf("gRPC deadline finalized Runtime: complete=%v alive=%v", runtimeUnderTest.shutdownComplete, runtimeUnderTest.health.Alive())
	}
	if repeated := runtimeUnderTest.Shutdown(context.Background()); repeated != nil {
		t.Fatalf("repeated Shutdown error = %v", repeated)
	}
	if !runtimeUnderTest.shutdownComplete || runtimeUnderTest.health.Alive() {
		t.Fatalf("retry did not finalize Runtime: complete=%v alive=%v", runtimeUnderTest.shutdownComplete, runtimeUnderTest.health.Alive())
	}
}

func TestRuntimeShutdownHTTPDeadlineRetriesWithoutFinalizing(t *testing.T) {
	runtimeUnderTest := newShutdownRuntime(t)
	handlerStarted := make(chan struct{})
	releaseHandler := make(chan struct{})
	var releaseOnce sync.Once
	httpServer := &http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		close(handlerStarted)
		<-releaseHandler
		writer.WriteHeader(http.StatusNoContent)
	})}
	serverConn, clientConn := net.Pipe()
	listener := newPipeHTTPListener(serverConn)
	runtimeUnderTest.httpServer = httpServer
	runtimeUnderTest.waitGroup.Add(1)
	go func() {
		defer runtimeUnderTest.waitGroup.Done()
		_ = httpServer.Serve(listener)
	}()
	requestDone := make(chan error, 1)
	go func() {
		_, requestErr := io.WriteString(clientConn, "GET / HTTP/1.1\r\nHost: pipe\r\nConnection: close\r\n\r\n")
		if requestErr == nil {
			_, requestErr = io.ReadAll(clientConn)
		}
		requestDone <- requestErr
	}()
	<-handlerStarted
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(releaseHandler) })
		_ = httpServer.Close()
		_ = clientConn.Close()
		_ = listener.Close()
	})

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if err := runtimeUnderTest.Shutdown(shutdownCtx); !errors.Is(err, context.DeadlineExceeded) {
		releaseOnce.Do(func() { close(releaseHandler) })
		t.Fatalf("Shutdown error = %v", err)
	}
	if runtimeUnderTest.shutdownComplete || !runtimeUnderTest.health.Alive() || runtimeUnderTest.httpDone {
		releaseOnce.Do(func() { close(releaseHandler) })
		t.Fatalf("HTTP deadline finalized Runtime: complete=%v alive=%v httpDone=%v", runtimeUnderTest.shutdownComplete, runtimeUnderTest.health.Alive(), runtimeUnderTest.httpDone)
	}
	releaseOnce.Do(func() { close(releaseHandler) })
	if err := <-requestDone; err != nil {
		t.Fatal(err)
	}
	if err := runtimeUnderTest.Shutdown(context.Background()); err != nil {
		t.Fatalf("retry Shutdown: %v", err)
	}
	if !runtimeUnderTest.shutdownComplete || runtimeUnderTest.health.Alive() || !runtimeUnderTest.httpDone {
		t.Fatalf("retry did not finalize Runtime: complete=%v alive=%v httpDone=%v", runtimeUnderTest.shutdownComplete, runtimeUnderTest.health.Alive(), runtimeUnderTest.httpDone)
	}
}

type pipeHTTPListener struct {
	connections chan net.Conn
	closed      chan struct{}
	closeOnce   sync.Once
}

func newPipeHTTPListener(connection net.Conn) *pipeHTTPListener {
	connections := make(chan net.Conn, 1)
	connections <- connection
	return &pipeHTTPListener{connections: connections, closed: make(chan struct{})}
}

func (listener *pipeHTTPListener) Accept() (net.Conn, error) {
	select {
	case connection := <-listener.connections:
		return connection, nil
	case <-listener.closed:
		return nil, net.ErrClosed
	}
}

func (listener *pipeHTTPListener) Close() error {
	listener.closeOnce.Do(func() { close(listener.closed) })
	return nil
}

func (*pipeHTTPListener) Addr() net.Addr { return pipeHTTPAddress("pipe") }

type pipeHTTPAddress string

func (pipeHTTPAddress) Network() string        { return "pipe" }
func (address pipeHTTPAddress) String() string { return string(address) }

type lifecycleStreamServer interface {
	Hold(grpc.ServerStream) error
}

type lifecycleStream struct {
	state          *State
	releaseOnClose bool
	started        chan struct{}
	closerCalled   chan struct{}
	release        chan struct{}
	exited         chan struct{}
	serveDone      chan struct{}
	closeOnce      sync.Once
	canceled       atomic.Bool
}

func (stream *lifecycleStream) Hold(serverStream grpc.ServerStream) error {
	defer close(stream.exited)
	request := &emptypb.Empty{}
	if err := serverStream.RecvMsg(request); err != nil {
		return err
	}
	session := &cloudv1.ClientSessionSummary{
		SessionId: "session", AccountId: "account", DaemonId: "daemon", ClientId: "client",
		Product: cloudv1.ClientProduct_CLIENT_PRODUCT_TUI, Generation: 1,
	}
	if err := stream.state.AttachSession(serverStream.Context(), session, stream.close); err != nil {
		return err
	}
	close(stream.started)
	if stream.releaseOnClose {
		select {
		case <-stream.release:
			return nil
		case <-serverStream.Context().Done():
			stream.canceled.Store(true)
			return serverStream.Context().Err()
		}
	}
	<-serverStream.Context().Done()
	stream.canceled.Store(true)
	return serverStream.Context().Err()
}

func (stream *lifecycleStream) close() {
	stream.closeOnce.Do(func() {
		close(stream.closerCalled)
		if stream.releaseOnClose {
			close(stream.release)
		}
	})
}

var lifecycleStreamService = grpc.ServiceDesc{
	ServiceName: "anytty.test.Lifecycle",
	HandlerType: (*lifecycleStreamServer)(nil),
	Streams: []grpc.StreamDesc{{
		StreamName:    "Hold",
		Handler:       func(service any, stream grpc.ServerStream) error { return service.(lifecycleStreamServer).Hold(stream) },
		ServerStreams: true,
		ClientStreams: true,
	}},
}

func startLifecycleStream(t *testing.T, releaseOnClose bool) (*Runtime, *lifecycleStream) {
	t.Helper()
	state, err := NewState(StateConfig{MailboxSize: 8, DeltaBuffer: 8})
	if err != nil {
		t.Fatal(err)
	}
	if err := state.ApplyDaemonStateSnapshot(context.Background(), &cloudv1.DaemonStateSnapshot{Daemons: []*cloudv1.DaemonStateRecord{{DaemonId: "daemon", State: cloudv1.DaemonState_DAEMON_STATE_ACTIVE, StateRevision: 1}}}); err != nil {
		t.Fatal(err)
	}
	if err := state.UpsertAgent(context.Background(), &cloudv1.AgentPresence{DaemonId: "daemon", AccountId: "account", BootId: "boot", ConnectionId: "connection", Generation: 1, BindingId: "binding", BindingIssuedAt: timestamppb.Now()}); err != nil {
		t.Fatal(err)
	}
	grpcServer := grpc.NewServer()
	stream := &lifecycleStream{
		state: state, releaseOnClose: releaseOnClose, started: make(chan struct{}), closerCalled: make(chan struct{}),
		release: make(chan struct{}), exited: make(chan struct{}), serveDone: make(chan struct{}),
	}
	grpcServer.RegisterService(&lifecycleStreamService, stream)
	listener := bufconn.Listen(1024 * 1024)
	go func() {
		_ = grpcServer.Serve(listener)
		close(stream.serveDone)
	}()
	client, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = client.Close()
		grpcServer.Stop()
		state.Close()
		_ = listener.Close()
	})
	clientStream, err := client.NewStream(context.Background(), &lifecycleStreamService.Streams[0], "/anytty.test.Lifecycle/Hold")
	if err != nil {
		t.Fatal(err)
	}
	if err := clientStream.SendMsg(&emptypb.Empty{}); err != nil {
		t.Fatal(err)
	}
	waitClosed(t, stream.started, "gRPC stream did not start")
	return newLifecycleRuntimeWithState(grpcServer, state), stream
}

func newLifecycleRuntimeWithState(grpcServer *grpc.Server, state *State) *Runtime {
	runCtx, cancel := context.WithCancel(context.Background())
	health := &processhealth.State{}
	health.SetAlive(true)
	return &Runtime{
		ctx: runCtx, cancel: cancel, readyChanges: make(chan struct{}, 1), state: state, health: health,
		grpcHealth: grpc_health.NewServer(), grpcServer: grpcServer, httpServer: &http.Server{},
	}
}

func waitClosed(t *testing.T, channel <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-channel:
	case <-time.After(2 * time.Second):
		t.Fatal(message)
	}
}
