package termxcorev2

import (
	"context"
	"errors"
	"go/parser"
	"go/token"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-shared/transport"
)

func TestServerOptions(t *testing.T) {
	server := NewServer(
		WithSocketPath("/tmp/termx-core-v2-test.sock"),
		WithDefaultSize(100, 30),
		WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
		WithListenerFactory(func(string) (transport.Listener, error) {
			return newFakeListener("unused"), nil
		}),
	)
	if server.SocketPath() != "/tmp/termx-core-v2-test.sock" {
		t.Fatalf("unexpected socket path %q", server.SocketPath())
	}
	if server.DefaultSize() != (Size{Cols: 100, Rows: 30}) {
		t.Fatalf("unexpected default size %#v", server.DefaultSize())
	}
}

func TestServerRegistryPublishesEvents(t *testing.T) {
	server := NewServer(WithListenerFactory(func(string) (transport.Listener, error) {
		return newFakeListener("unused"), nil
	}), WithProcessFactory(newRecordingProcessFactory()))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := server.Events(ctx, EventFilter{})
	info, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-2",
		Name:    "demo",
		Command: []string{"sh"},
	})
	if err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if info.Size != server.DefaultSize() {
		t.Fatalf("expected default size, got %#v", info.Size)
	}
	if got, err := server.GetTerminal("term-2"); err != nil || got.ID != "term-2" || got.Name != "demo" {
		t.Fatalf("get terminal got=%#v err=%v", got, err)
	}
	assertEvent(t, events, EventTerminalCreated, "term-2")
	if err := server.RemoveTerminal("term-2"); err != nil {
		t.Fatalf("remove terminal: %v", err)
	}
	assertEvent(t, events, EventTerminalRemoved, "term-2")
	if _, err := server.GetTerminal("term-2"); !errors.Is(err, ErrTerminalNotFound) {
		t.Fatalf("expected ErrTerminalNotFound, got %v", err)
	}
}

func TestServerRegistryValidatesRecords(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{Command: []string{"sh"}}); !errors.Is(err, ErrInvalidTerminalID) {
		t.Fatalf("expected ErrInvalidTerminalID, got %v", err)
	}
	if _, err := server.RegisterTerminal(TerminalRecord{ID: "term-1"}); !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("expected ErrInvalidCommand, got %v", err)
	}
	if _, err := server.RegisterTerminal(TerminalRecord{ID: "term-1", Command: []string{"sh"}}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if _, err := server.RegisterTerminal(TerminalRecord{ID: "term-1", Command: []string{"sh"}}); !errors.Is(err, ErrDuplicateTerminal) {
		t.Fatalf("expected ErrDuplicateTerminal, got %v", err)
	}
}

func TestServerStorageVersioningListAndEvents(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := server.Events(ctx, EventFilter{
		Types:            []EventType{EventStorageChanged},
		StorageAppID:     "termx-tui-v3",
		StorageScope:     StorageScopePublic,
		StorageOwnerID:   "workspace-main",
		StorageKeyPrefix: "workbench/",
	})

	entry, err := server.StoragePut(context.Background(), StoragePutRequest{
		AppID:   "termx-tui-v3",
		Scope:   StorageScopePublic,
		OwnerID: "workspace-main",
		Key:     "workbench/root",
		Value:   []byte(`{"workspace":"main"}`),
	})
	if err != nil {
		t.Fatalf("storage put: %v", err)
	}
	if entry.Version != 1 || string(entry.Value) != `{"workspace":"main"}` {
		t.Fatalf("unexpected first entry %#v", entry)
	}
	event := assertEventValue(t, events, EventStorageChanged, "")
	if event.Storage == nil || event.Storage.AppID != "termx-tui-v3" || event.Storage.Key != "workbench/root" || event.Storage.Op != StorageOpPut || event.Storage.Version != 1 {
		t.Fatalf("unexpected storage event %#v", event)
	}

	if _, err := server.StoragePut(context.Background(), StoragePutRequest{
		AppID:           "termx-tui-v3",
		Scope:           StorageScopePublic,
		OwnerID:         "workspace-main",
		Key:             "workbench/root",
		Value:           []byte("stale"),
		CheckVersion:    true,
		ExpectedVersion: 99,
	}); !errors.Is(err, ErrStorageVersionConflict) {
		t.Fatalf("expected version conflict, got %v", err)
	}
	entry, err = server.StoragePut(context.Background(), StoragePutRequest{
		AppID:           "termx-tui-v3",
		Scope:           StorageScopePublic,
		OwnerID:         "workspace-main",
		Key:             "workbench/root",
		Value:           []byte(`{"workspace":"renamed"}`),
		CheckVersion:    true,
		ExpectedVersion: 1,
	})
	if err != nil {
		t.Fatalf("versioned storage put: %v", err)
	}
	if entry.Version != 2 {
		t.Fatalf("expected version 2, got %#v", entry)
	}

	_, err = server.StoragePut(context.Background(), StoragePutRequest{AppID: "termx-tui-v3", Scope: StorageScopePublic, OwnerID: "workspace-main", Key: "other/root", Value: []byte("ignored")})
	if err != nil {
		t.Fatalf("put other key: %v", err)
	}
	list := server.StorageList(context.Background(), "termx-tui-v3", StorageScopePublic, "workspace-main", "workbench/")
	if len(list) != 1 || list[0].Key != "workbench/root" || list[0].Version != 2 {
		t.Fatalf("unexpected storage list %#v", list)
	}
	deleted, err := server.StorageDelete(context.Background(), StorageDeleteRequest{
		AppID:           "termx-tui-v3",
		Scope:           StorageScopePublic,
		OwnerID:         "workspace-main",
		Key:             "workbench/root",
		CheckVersion:    true,
		ExpectedVersion: 2,
	})
	if err != nil {
		t.Fatalf("storage delete: %v", err)
	}
	if !deleted.Deleted || deleted.Version != 3 {
		t.Fatalf("unexpected delete result %#v", deleted)
	}
}

func TestServerListenAndShutdownUseListenerFactory(t *testing.T) {
	listener := newFakeListener("/tmp/core-v2.sock")
	factoryCalled := make(chan string, 1)
	server := NewServer(
		WithSocketPath("/tmp/core-v2.sock"),
		WithListenerFactory(func(path string) (transport.Listener, error) {
			factoryCalled <- path
			return listener, nil
		}),
	)
	events := server.Events(context.Background(), EventFilter{Types: []EventType{EventServerListening}})
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe(context.Background())
	}()
	select {
	case path := <-factoryCalled:
		if path != "/tmp/core-v2.sock" {
			t.Fatalf("listener factory path = %q", path)
		}
	case <-time.After(time.Second):
		t.Fatal("listener factory was not called")
	}
	assertEvent(t, events, EventServerListening, "")
	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("listen returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("listen did not stop")
	}
	if !listener.closed() {
		t.Fatal("expected listener to be closed")
	}
	if _, err := server.RegisterTerminal(TerminalRecord{ID: "term-1", Command: []string{"sh"}}); !errors.Is(err, ErrServerClosed) {
		t.Fatalf("expected ErrServerClosed after shutdown, got %v", err)
	}
}

func TestServerAcceptedTransportClosesOnShutdown(t *testing.T) {
	listener := newFakeListener("/tmp/core-v2.sock")
	conn := newFakeTransport()
	server := NewServer(WithListenerFactory(func(string) (transport.Listener, error) {
		return listener, nil
	}))
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe(context.Background())
	}()
	listener.accept(conn)
	listener.waitAccepted(t)
	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	select {
	case <-conn.Done():
	case <-time.After(time.Second):
		t.Fatal("accepted transport was not closed")
	}
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("listen returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("listen did not stop")
	}
}

func TestCoreV2DoesNotImportLegacyRuntime(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	var offenders []string
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range file.Imports {
			importPath, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			if isLegacyRuntimeImport(importPath) {
				offenders = append(offenders, path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk core-v2: %v", err)
	}
	if len(offenders) > 0 {
		t.Fatalf("core-v2 must not import legacy runtime: %v", offenders)
	}
}

func isLegacyRuntimeImport(importPath string) bool {
	for _, legacy := range []string{
		"github.com/lozzow/termx/termx-core",
		"github.com/lozzow/termx/tuiv2",
	} {
		if importPath == legacy || strings.HasPrefix(importPath, legacy+"/") {
			return true
		}
	}
	return false
}

func assertEvent(t *testing.T, events <-chan Event, typ EventType, terminalID string) {
	t.Helper()
	_ = assertEventValue(t, events, typ, terminalID)
}

func assertEventValue(t *testing.T, events <-chan Event, typ EventType, terminalID string) Event {
	t.Helper()
	select {
	case event, ok := <-events:
		if !ok {
			t.Fatal("event channel closed")
		}
		if event.Type != typ || event.TerminalID != terminalID {
			t.Fatalf("unexpected event %#v", event)
		}
		return event
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s event", typ)
	}
	return Event{}
}

type fakeListener struct {
	addr     string
	acceptCh chan transport.Transport
	accepted chan struct{}
	done     chan struct{}
}

func newFakeListener(addr string) *fakeListener {
	return &fakeListener{
		addr:     addr,
		acceptCh: make(chan transport.Transport, 8),
		accepted: make(chan struct{}, 8),
		done:     make(chan struct{}),
	}
}

func (listener *fakeListener) Accept(ctx context.Context) (transport.Transport, error) {
	select {
	case conn := <-listener.acceptCh:
		listener.accepted <- struct{}{}
		return conn, nil
	case <-listener.done:
		return nil, transport.ErrListenerClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (listener *fakeListener) Close() error {
	select {
	case <-listener.done:
	default:
		close(listener.done)
	}
	return nil
}

func (listener *fakeListener) Addr() string {
	return listener.addr
}

func (listener *fakeListener) accept(conn transport.Transport) {
	listener.acceptCh <- conn
}

func (listener *fakeListener) waitAccepted(t *testing.T) {
	t.Helper()
	select {
	case <-listener.accepted:
	case <-time.After(time.Second):
		t.Fatal("listener did not accept transport")
	}
}

func (listener *fakeListener) closed() bool {
	select {
	case <-listener.done:
		return true
	default:
		return false
	}
}

type fakeTransport struct {
	done chan struct{}
}

func newFakeTransport() *fakeTransport {
	return &fakeTransport{done: make(chan struct{})}
}

func (transport *fakeTransport) Send([]byte) error {
	return nil
}

func (transport *fakeTransport) Recv() ([]byte, error) {
	<-transport.done
	return nil, io.EOF
}

func (transport *fakeTransport) Close() error {
	select {
	case <-transport.done:
	default:
		close(transport.done)
	}
	return nil
}

func (transport *fakeTransport) Done() <-chan struct{} {
	return transport.done
}
