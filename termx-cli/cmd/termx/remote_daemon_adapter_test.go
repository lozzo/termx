package main

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	coreprotocol "github.com/lozzow/termx/internal/protocol"
	corev2 "github.com/lozzow/termx/termx-core-v2"
	"github.com/lozzow/termx/termx-proto/wire"
	remote "github.com/lozzow/termx/termx-remote"
	remoteprotocol "github.com/lozzow/termx/termx-remote/protocol"
	"github.com/lozzow/termx/termx-shared/transport/memory"
)

func TestCoreV2RemoteDaemonAdapterRoutesTerminalStorageEventsAndScope(t *testing.T) {
	factory := &remoteAdapterProcessFactory{}
	server := corev2.NewServer(corev2.WithProcessFactory(factory))
	adapter := newCoreV2RemoteDaemonAdapter(server)
	ctx := context.Background()

	created, err := adapter.Create(ctx, coreprotocol.CreateParams{
		ID:      "term-1",
		Name:    "remote",
		Command: []string{"shell", "-l"},
		Tags:    map[string]string{"termx.cwd": "/workspace"},
		Size:    coreprotocol.Size{Cols: 33, Rows: 12},
		Dir:     "/workspace",
		Env:     []string{"A=B"},
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if created.TerminalID != "term-1" || created.State != string(corev2.TerminalStateRunning) {
		t.Fatalf("unexpected create result %#v", created)
	}
	spec := factory.spawnedSpec("term-1")
	if spec.Dir != "/workspace" || !equalStringSlices(spec.Env, []string{"A=B"}) || spec.Size != (corev2.Size{Cols: 33, Rows: 12}) {
		t.Fatalf("adapter dropped create/process fields: %#v", spec)
	}

	if _, err := adapter.Create(ctx, coreprotocol.CreateParams{ID: "term-2", Command: []string{"shell"}}); err != nil {
		t.Fatalf("Create term-2 returned error: %v", err)
	}
	got, err := adapter.Get(ctx, "term-1")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if got.CWD != "/workspace" || got.Name != "remote" {
		t.Fatalf("unexpected terminal projection %#v", got)
	}
	list, err := adapter.List(ctx)
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(list.Terminals) != 2 {
		t.Fatalf("expected 2 terminals from core-v2 registry, got %#v", list.Terminals)
	}

	events, err := adapter.Events(ctx, coreprotocol.EventsParams{
		TerminalID: "term-1",
		Types:      []coreprotocol.EventType{coreprotocol.EventTerminalMetadataChanged},
	})
	if err != nil {
		t.Fatalf("Events returned error: %v", err)
	}
	if err := adapter.SetMetadata(ctx, "term-1", "renamed", map[string]string{"termx.cwd": "/workspace/new"}); err != nil {
		t.Fatalf("SetMetadata returned error: %v", err)
	}
	select {
	case event := <-events:
		if event.TerminalID != "term-1" || event.Type != coreprotocol.EventTerminalMetadataChanged {
			t.Fatalf("unexpected event projection %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for core-v2 metadata event")
	}

	entry, err := adapter.StoragePut(ctx, coreprotocol.StoragePutParams{
		AppID:   "remote-ui",
		Scope:   coreprotocol.StorageScopePrivate,
		OwnerID: "machine-1",
		Key:     "prefs/theme",
		Value:   []byte("dark"),
	})
	if err != nil {
		t.Fatalf("StoragePut returned error: %v", err)
	}
	gotEntry, err := adapter.StorageGet(ctx, coreprotocol.StorageGetParams{
		AppID:   entry.AppID,
		Scope:   entry.Scope,
		OwnerID: entry.OwnerID,
		Key:     entry.Key,
	})
	if err != nil {
		t.Fatalf("StorageGet returned error: %v", err)
	}
	if string(gotEntry.Value) != "dark" {
		t.Fatalf("unexpected storage value %q", gotEntry.Value)
	}
	listed, err := adapter.StorageList(ctx, coreprotocol.StorageListParams{
		AppID:   "remote-ui",
		Scope:   coreprotocol.StorageScopePrivate,
		OwnerID: "machine-1",
		Prefix:  "prefs/",
	})
	if err != nil {
		t.Fatalf("StorageList returned error: %v", err)
	}
	if len(listed.Entries) != 1 || listed.Entries[0].Key != "prefs/theme" {
		t.Fatalf("unexpected storage list %#v", listed)
	}
	deleted, err := adapter.StorageDelete(ctx, coreprotocol.StorageDeleteParams{
		AppID:   "remote-ui",
		Scope:   coreprotocol.StorageScopePrivate,
		OwnerID: "machine-1",
		Key:     "prefs/theme",
	})
	if err != nil {
		t.Fatalf("StorageDelete returned error: %v", err)
	}
	if !deleted.Deleted || deleted.Scope != coreprotocol.StorageScopePrivate {
		t.Fatalf("unexpected storage delete result %#v", deleted)
	}

	client, closeClient := newRemoteAdapterScopedClient(t, adapter, remote.TransportScope{TerminalID: "term-1"})
	defer closeClient()
	var termOne coreprotocol.TerminalInfo
	if err := client.Call(ctx, "get", coreprotocol.GetParams{TerminalID: "term-1"}, &termOne); err != nil {
		t.Fatalf("scoped transport get term-1 returned error: %v", err)
	}
	var termTwo coreprotocol.TerminalInfo
	err = client.Call(ctx, "get", coreprotocol.GetParams{TerminalID: "term-2"}, &termTwo)
	if err == nil || !strings.Contains(err.Error(), "transport scope") {
		t.Fatalf("expected scoped transport to reject term-2, got %v", err)
	}
}

func TestRemoteClientsUseCoreV2TypedRemoteProtocol(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	fake := &remoteClientFakeService{
		status: coreprotocol.RemoteStatus{
			State:         string(remoteprotocol.StateOnline),
			DeviceID:      "device-1",
			TerminalCount: 3,
			UpdatedAt:     now,
		},
		pairResult: coreprotocol.RemotePairStartResult{
			Type:          "local",
			MachineID:     "machine-1",
			MachineName:   "dev",
			LocalPairURL:  "http://127.0.0.1/pair",
			PairSessionID: "pair-1",
			PairSecret:    "secret",
			ExpiresAt:     now.Add(time.Minute),
		},
		localStatus: coreprotocol.RemoteLocalStatus{
			Enabled:       true,
			HTTPURL:       "http://127.0.0.1:8080",
			LocalWebAddr:  "127.0.0.1:8080",
			ICETCPEnabled: true,
			ICETCPAddr:    "127.0.0.1:9000",
			ICETCPPort:    9000,
			UpdatedAt:     now,
		},
	}
	server, _, closeClient := newCoreV2ProtocolClientForCLITestWithOptions(t, corev2.WithRemoteService(fake))
	defer closeClient()

	status, err := remoteStatusClient(context.Background(), server.SocketPath(), "")
	if err != nil {
		t.Fatalf("remoteStatusClient returned error: %v", err)
	}
	if status.State != remoteprotocol.StateOnline || status.DeviceID != "device-1" || status.TerminalCount != 3 {
		t.Fatalf("unexpected remote status %#v", status)
	}

	pair, err := pairStartClient(context.Background(), server.SocketPath(), "", remoteprotocol.PairStartParams{
		LocalPairURL:   "http://local/pair",
		TTLSeconds:     10,
		AuthTTLSeconds: 20,
	})
	if err != nil {
		t.Fatalf("pairStartClient returned error: %v", err)
	}
	if pair.MachineID != "machine-1" || fake.lastPairParams.LocalPairURL != "http://local/pair" || fake.lastPairParams.AuthTTLSeconds != 20 {
		t.Fatalf("pair conversion failed result=%#v params=%#v", pair, fake.lastPairParams)
	}

	local, err := remoteLocalEnableClient(context.Background(), server.SocketPath(), "", remoteprotocol.LocalEnableParams{
		LocalWebAddr: "127.0.0.1:8080",
		ICETCPAddr:   "127.0.0.1:9000",
		HubURLs:      []string{"https://hub"},
		ControlURL:   "https://control",
		AccessToken:  "token",
		Region:       "ap",
	})
	if err != nil {
		t.Fatalf("remoteLocalEnableClient returned error: %v", err)
	}
	if !local.Enabled || local.HTTPURL != "http://127.0.0.1:8080" || fake.lastLocalParams.AccessToken != "token" {
		t.Fatalf("local enable conversion failed status=%#v params=%#v", local, fake.lastLocalParams)
	}
	if _, err := remoteLocalStatusClient(context.Background(), server.SocketPath(), ""); err != nil {
		t.Fatalf("remoteLocalStatusClient returned error: %v", err)
	}
	if _, err := remoteLocalDisableClient(context.Background(), server.SocketPath(), ""); err != nil {
		t.Fatalf("remoteLocalDisableClient returned error: %v", err)
	}
}

func newRemoteAdapterScopedClient(t *testing.T, adapter *coreV2RemoteDaemonAdapter, scope remote.TransportScope) (*coreprotocol.Client, func()) {
	t.Helper()
	clientTransport, serverTransport := memory.NewPair()
	errCh := make(chan error, 1)
	go func() {
		errCh <- adapter.ServeScopedTransport(context.Background(), serverTransport, "webrtc:terminal:"+scope.TerminalID, scope)
	}()
	client := coreprotocol.NewClient(clientTransport)
	if err := client.Hello(context.Background(), coreprotocol.Hello{Version: wire.Version, Client: "remote-adapter-test"}); err != nil {
		t.Fatalf("hello: %v", err)
	}
	return client, func() {
		_ = client.Close()
		select {
		case err := <-errCh:
			if err != nil && !strings.Contains(err.Error(), "EOF") {
				t.Fatalf("scoped transport returned error: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("scoped transport did not stop")
		}
	}
}

type remoteAdapterProcessFactory struct {
	mu    sync.Mutex
	specs map[string]corev2.ProcessSpec
}

func (factory *remoteAdapterProcessFactory) Spawn(_ context.Context, spec corev2.ProcessSpec) (corev2.TerminalProcess, error) {
	factory.mu.Lock()
	if factory.specs == nil {
		factory.specs = make(map[string]corev2.ProcessSpec)
	}
	factory.specs[spec.TerminalID] = cloneRemoteAdapterProcessSpec(spec)
	factory.mu.Unlock()
	return newRemoteAdapterProcess(), nil
}

func (factory *remoteAdapterProcessFactory) spawnedSpec(terminalID string) corev2.ProcessSpec {
	factory.mu.Lock()
	defer factory.mu.Unlock()
	return cloneRemoteAdapterProcessSpec(factory.specs[terminalID])
}

type remoteAdapterProcess struct {
	outputCh chan []byte
	waitCh   chan corev2.ProcessExit
	close    sync.Once
}

func newRemoteAdapterProcess() *remoteAdapterProcess {
	return &remoteAdapterProcess{
		outputCh: make(chan []byte),
		waitCh:   make(chan corev2.ProcessExit, 1),
	}
}

func (process *remoteAdapterProcess) Input([]byte) error {
	return nil
}

func (process *remoteAdapterProcess) Resize(corev2.Size) error {
	return nil
}

func (process *remoteAdapterProcess) Output() <-chan []byte {
	return process.outputCh
}

func (process *remoteAdapterProcess) Kill() error {
	process.Close()
	return nil
}

func (process *remoteAdapterProcess) Wait() <-chan corev2.ProcessExit {
	return process.waitCh
}

func (process *remoteAdapterProcess) Close() error {
	process.close.Do(func() {
		close(process.outputCh)
		process.waitCh <- corev2.ProcessExit{Code: -1}
		close(process.waitCh)
	})
	return nil
}

type remoteClientFakeService struct {
	mu              sync.Mutex
	status          coreprotocol.RemoteStatus
	pairResult      coreprotocol.RemotePairStartResult
	localStatus     coreprotocol.RemoteLocalStatus
	lastPairParams  coreprotocol.RemotePairStartParams
	lastLocalParams coreprotocol.RemoteLocalEnableParams
}

func (service *remoteClientFakeService) Status(context.Context) (coreprotocol.RemoteStatus, error) {
	return service.status, nil
}

func (service *remoteClientFakeService) PairStart(_ context.Context, params coreprotocol.RemotePairStartParams) (coreprotocol.RemotePairStartResult, error) {
	service.mu.Lock()
	service.lastPairParams = params
	service.mu.Unlock()
	return service.pairResult, nil
}

func (service *remoteClientFakeService) LocalEnable(_ context.Context, params coreprotocol.RemoteLocalEnableParams) (coreprotocol.RemoteLocalStatus, error) {
	service.mu.Lock()
	service.lastLocalParams = params
	service.mu.Unlock()
	return service.localStatus, nil
}

func (service *remoteClientFakeService) LocalStatus(context.Context) (coreprotocol.RemoteLocalStatus, error) {
	return service.localStatus, nil
}

func (service *remoteClientFakeService) LocalDisable(context.Context) (coreprotocol.RemoteLocalStatus, error) {
	return service.localStatus, nil
}

func cloneRemoteAdapterProcessSpec(spec corev2.ProcessSpec) corev2.ProcessSpec {
	spec.Command = append([]string(nil), spec.Command...)
	spec.Env = append([]string(nil), spec.Env...)
	return spec
}

func equalStringSlices(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

var _ corev2.TerminalProcess = (*remoteAdapterProcess)(nil)
