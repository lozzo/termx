package runtime

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/anytty/anytty/cloud/processhealth"
	"google.golang.org/grpc"
	grpc_health "google.golang.org/grpc/health"
)

func TestRuntimeShutdownConcurrentWaiterHonorsOwnDeadline(t *testing.T) {
	drainer := &blockingShutdownDrainer{entered: make(chan struct{}), release: make(chan struct{})}
	var releaseOnce sync.Once
	releaseDrainer := func() { releaseOnce.Do(func() { close(drainer.release) }) }
	t.Cleanup(releaseDrainer)
	runtime := newShutdownTestRuntime(&http.Server{})
	runtime.drainer = drainer

	ownerDone := make(chan error, 1)
	go func() { ownerDone <- runtime.Shutdown(context.Background()) }()
	<-drainer.entered

	waiterCtx, cancelWaiter := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancelWaiter()
	waiterDone := make(chan error, 1)
	go func() { waiterDone <- runtime.Shutdown(waiterCtx) }()
	select {
	case err := <-waiterDone:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("concurrent Shutdown error = %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		releaseDrainer()
		t.Fatal("concurrent Shutdown ignored its own deadline while waiting for the owner")
	}

	releaseDrainer()
	if err := <-ownerDone; err != nil {
		t.Fatalf("owner Shutdown error = %v", err)
	}
	if err := runtime.Shutdown(context.Background()); err != nil {
		t.Fatalf("completed Shutdown error = %v", err)
	}
}

func TestRuntimeShutdownHealthDeadlineRetriesAndCachesCompletion(t *testing.T) {
	httpServer, releaseHandler, requestDone := blockingHTTPServer(t)
	runtime := newShutdownTestRuntime(httpServer)

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancelShutdown()
	firstErr := runtime.Shutdown(shutdownCtx)
	if !errors.Is(firstErr, context.DeadlineExceeded) {
		releaseHandler()
		t.Fatalf("first Shutdown error = %v", firstErr)
	}
	if !runtime.grpcStopped {
		t.Fatal("completed gRPC shutdown phase was not retained")
	}
	if runtime.shutdownDone {
		t.Fatal("caller deadline was cached as final shutdown completion")
	}
	if !runtime.health.Alive() {
		t.Fatal("health was marked stopped before its server completed shutdown")
	}
	releaseHandler()
	if err := <-requestDone; err != nil {
		t.Fatalf("active health request error = %v", err)
	}
	if err := runtime.Shutdown(context.Background()); err != nil {
		t.Fatalf("retry Shutdown error = %v", err)
	}
	if !runtime.shutdownDone || runtime.health.Alive() {
		t.Fatal("successful retry did not complete shutdown")
	}
	if err := runtime.Shutdown(context.Background()); err != nil {
		t.Fatalf("cached successful Shutdown error = %v", err)
	}
}

func TestRuntimeShutdownCachesPersistentError(t *testing.T) {
	persistentErr := errors.New("persistent health shutdown failure")
	healthServer, serveDone := shutdownErrorHTTPServer(t, persistentErr)
	runtime := newShutdownTestRuntime(healthServer)

	if err := runtime.Shutdown(context.Background()); !errors.Is(err, persistentErr) {
		t.Fatalf("first Shutdown error = %v", err)
	}
	if !runtime.shutdownDone || runtime.health.Alive() {
		t.Fatal("persistent shutdown result was not recorded as final")
	}
	if err := runtime.Shutdown(context.Background()); !errors.Is(err, persistentErr) {
		t.Fatalf("repeated Shutdown lost persistent error: %v", err)
	}
	if err := <-serveDone; !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("health Serve error = %v", err)
	}
}

type blockingShutdownDrainer struct {
	entered chan struct{}
	release chan struct{}
}

func (drainer *blockingShutdownDrainer) BeginShutdown() {
	close(drainer.entered)
	<-drainer.release
}

func newShutdownTestRuntime(healthServer *http.Server) *Runtime {
	health := &processhealth.State{}
	health.SetAlive(true)
	health.SetReady(true)
	return &Runtime{
		grpcServer: grpc.NewServer(), grpcHealth: grpc_health.NewServer(),
		healthServer: healthServer, health: health,
	}
}

func blockingHTTPServer(t *testing.T) (*http.Server, func(), <-chan error) {
	t.Helper()
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseHandler := func() { releaseOnce.Do(func() { close(release) }) }
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		writer.WriteHeader(http.StatusNoContent)
	}))
	requestDone := make(chan error, 1)
	go func() {
		response, err := server.Client().Get(server.URL)
		if err == nil {
			_, _ = io.Copy(io.Discard, response.Body)
			err = response.Body.Close()
		}
		requestDone <- err
	}()
	<-started
	t.Cleanup(func() {
		releaseHandler()
		server.Close()
	})
	return server.Config, releaseHandler, requestDone
}

type shutdownErrorListener struct {
	net.Listener
	closeErr   error
	accepting  chan struct{}
	acceptOnce sync.Once
}

func (listener *shutdownErrorListener) Accept() (net.Conn, error) {
	listener.acceptOnce.Do(func() { close(listener.accepting) })
	return listener.Listener.Accept()
}

func (listener *shutdownErrorListener) Close() error {
	return errors.Join(listener.Listener.Close(), listener.closeErr)
}

func shutdownErrorHTTPServer(t *testing.T, closeErr error) (*http.Server, <-chan error) {
	t.Helper()
	baseListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listener := &shutdownErrorListener{
		Listener:  baseListener,
		closeErr:  closeErr,
		accepting: make(chan struct{}),
	}
	server := &http.Server{}
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	<-listener.accepting
	t.Cleanup(func() { _ = baseListener.Close() })
	return server, serveDone
}
