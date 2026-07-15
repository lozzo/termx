package core

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/proto/wire"
	"github.com/lozzow/termx/shared/transport/memory"
)

func TestServeTransportRunsUnscopedProtocolSession(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	client, closeClient := newClientForServedTransport(t, server, TransportScope{}, false)
	defer closeClient()

	if _, err := client.Create(context.Background(), protocol.CreateParams{
		ID:      "term-1",
		Name:    "demo",
		Command: []string{"shell"},
		Size:    protocol.Size{Cols: 12, Rows: 4},
	}); err != nil {
		t.Fatalf("create through ServeTransport: %v", err)
	}
	list, err := client.List(context.Background())
	if err != nil {
		t.Fatalf("list through ServeTransport: %v", err)
	}
	if len(list.Terminals) != 1 || list.Terminals[0].ID != "term-1" {
		t.Fatalf("unexpected unscoped list result %#v", list)
	}
}

func TestServeScopedTransportRejectsZeroValueScope(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	clientTransport, serverTransport := memory.NewPair()
	defer clientTransport.Close()

	err := server.ServeScopedTransport(context.Background(), serverTransport, TransportScope{})
	if err == nil || !strings.Contains(err.Error(), "explicit capability") {
		t.Fatalf("zero-value remote scope must be rejected, got %v", err)
	}
}

func TestServeScopedTransportRestrictsTerminalMethods(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	registerScopedTestTerminal(t, server, "term-1")
	registerScopedTestTerminal(t, server, "term-2")

	client, closeClient := newClientForServedTransport(t, server, TransportScope{TerminalID: "term-1"}, true)
	defer closeClient()

	var info protocol.TerminalInfo
	if err := client.Call(context.Background(), "get", protocol.GetParams{TerminalID: "term-1"}, &info); err != nil {
		t.Fatalf("scoped get allowed terminal: %v", err)
	}
	if info.ID != "term-1" {
		t.Fatalf("scoped get returned wrong terminal %#v", info)
	}
	if err := client.Call(context.Background(), "get", protocol.GetParams{TerminalID: "term-2"}, &info); err == nil || !strings.Contains(err.Error(), "transport scope") {
		t.Fatalf("expected scoped get denial, got %v", err)
	}
	if _, err := client.List(context.Background()); err == nil || !strings.Contains(err.Error(), "transport scope") {
		t.Fatalf("expected scoped list denial, got %v", err)
	}

	attached, err := client.AttachWithOptions(context.Background(), protocol.AttachParams{
		TerminalID:   "term-1",
		ResizePolicy: protocol.ResizePolicyFollower,
		SurfaceID:    "surface",
		ViewID:       "view",
	})
	if err != nil {
		t.Fatalf("attach scoped terminal: %v", err)
	}
	if _, err := client.AttachWithOptions(context.Background(), protocol.AttachParams{TerminalID: "term-2"}); err == nil || !strings.Contains(err.Error(), "transport scope") {
		t.Fatalf("expected scoped attach denial, got %v", err)
	}
	if err := client.InputWithOptions(context.Background(), protocol.InputParams{
		TerminalID: "term-1",
		Channel:    attached.Channel,
		SurfaceID:  "surface",
		ViewID:     "view",
		Data:       []byte("ok"),
	}); err != nil {
		t.Fatalf("input scoped terminal: %v", err)
	}
	if err := client.InputWithOptions(context.Background(), protocol.InputParams{
		TerminalID: "term-2",
		Channel:    attached.Channel,
		Data:       []byte("deny"),
	}); err == nil || !strings.Contains(err.Error(), "transport scope") {
		t.Fatalf("expected scoped input denial before attachment lookup, got %v", err)
	}
}

func TestServeScopedTransportNarrowsBroadEventsToTerminal(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	registerScopedTestTerminal(t, server, "term-1")
	registerScopedTestTerminal(t, server, "term-2")

	client, closeClient := newClientForServedTransport(t, server, TransportScope{TerminalID: "term-1"}, true)
	defer closeClient()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err := client.Events(ctx, protocol.EventsParams{})
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	server.publishTerminalEvent(EventTerminalMetadataChanged, TerminalInfo{ID: "term-2", Name: "two"})
	server.publishTerminalEvent(EventTerminalMetadataChanged, TerminalInfo{ID: "term-1", Name: "one"})

	event := requireProtocolEvent(t, events)
	if event.TerminalID != "term-1" || event.Type != protocol.EventTerminalMetadataChanged {
		t.Fatalf("scoped broad event stream leaked wrong event %#v", event)
	}
	assertNoProtocolEvent(t, events)
}

func TestServeScopedTransportMachineEventsOnly(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	registerScopedTestTerminal(t, server, "term-1")

	client, closeClient := newClientForServedTransport(t, server, TransportScope{MachineEventsOnly: true}, true)
	defer closeClient()

	var info protocol.TerminalInfo
	if err := client.Call(context.Background(), "get", protocol.GetParams{TerminalID: "term-1"}, &info); err == nil || !strings.Contains(err.Error(), "transport scope") {
		t.Fatalf("expected machine-events-only get denial, got %v", err)
	}
	if _, err := client.AttachWithOptions(context.Background(), protocol.AttachParams{TerminalID: "term-1"}); err == nil || !strings.Contains(err.Error(), "transport scope") {
		t.Fatalf("expected machine-events-only attach denial, got %v", err)
	}
	if err := client.Call(context.Background(), "events", protocol.EventsParams{StorageAppID: "app"}, nil); err == nil || !strings.Contains(err.Error(), "transport scope") {
		t.Fatalf("expected machine-events-only storage event denial, got %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err := client.Events(ctx, protocol.EventsParams{})
	if err != nil {
		t.Fatalf("machine events: %v", err)
	}
	server.publishTerminalEvent(EventTerminalMetadataChanged, TerminalInfo{ID: "term-1", Name: "one"})
	event := requireProtocolEvent(t, events)
	if event.TerminalID != "term-1" || event.Type != protocol.EventTerminalMetadataChanged {
		t.Fatalf("machine event stream should carry terminal event, got %#v", event)
	}
}

func TestClientAccessManagementRequiresLocalOwnerOrExplicitCapability(t *testing.T) {
	service := &scopeClientAccessService{}
	server := NewServer(WithClientAccessService(service))

	remoteDaemon, closeRemoteDaemon := newClientForServedTransport(t, server, TransportScope{PrincipalID: "remote", AllowDaemon: true}, true)
	var identity protocol.ClientAccessIdentityResult
	if err := remoteDaemon.Call(context.Background(), "remote.access.identity", map[string]any{}, &identity); err == nil || !strings.Contains(err.Error(), "client access management") {
		t.Fatalf("daemon scope implicitly gained ManageClientAccess: %v", err)
	}
	closeRemoteDaemon()

	manager, closeManager := newClientForServedTransport(t, server, TransportScope{PrincipalID: "manager-key", TerminalID: "term-1", ManageClientAccess: true}, true)
	if err := manager.Call(context.Background(), "remote.access.identity", map[string]any{}, &identity); err != nil {
		t.Fatalf("explicit ManageClientAccess was denied: %v", err)
	}
	closeManager()

	owner, closeOwner := newClientForServedTransport(t, server, TransportScope{}, false)
	if err := owner.Call(context.Background(), "remote.access.identity", map[string]any{}, &identity); err != nil {
		t.Fatalf("local owner access identity: %v", err)
	}
	closeOwner()
	if service.identityCalls != 2 {
		t.Fatalf("client access service calls = %d, want 2", service.identityCalls)
	}
}

func newClientForServedTransport(t *testing.T, server *Server, scope TransportScope, scoped bool) (*protocol.Client, func()) {
	t.Helper()
	clientTransport, serverTransport := memory.NewPair()
	errCh := make(chan error, 1)
	go func() {
		if scoped {
			errCh <- server.ServeScopedTransport(context.Background(), serverTransport, scope)
			return
		}
		errCh <- server.ServeTransport(context.Background(), serverTransport)
	}()
	client := protocol.NewClient(clientTransport)
	if err := client.Hello(context.Background(), protocol.Hello{Version: wire.Version, Client: "scope-test"}); err != nil {
		t.Fatalf("hello: %v", err)
	}
	return client, func() {
		_ = client.Close()
		select {
		case err := <-errCh:
			if err != nil && !strings.Contains(err.Error(), "EOF") {
				t.Fatalf("server transport returned error: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("server transport did not stop")
		}
	}
}

func registerScopedTestTerminal(t *testing.T, server *Server, id string) {
	t.Helper()
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      id,
		Name:    id,
		Command: []string{"shell"},
		Size:    Size{Cols: 12, Rows: 4},
	}); err != nil {
		t.Fatalf("register %s: %v", id, err)
	}
}

func assertNoProtocolEvent(t *testing.T, events <-chan protocol.Event) {
	t.Helper()
	select {
	case event := <-events:
		t.Fatalf("unexpected protocol event %#v", event)
	case <-time.After(50 * time.Millisecond):
	}
}

type scopeClientAccessService struct{ identityCalls int }

func (service *scopeClientAccessService) Identity(context.Context) (protocol.ClientAccessIdentityResult, error) {
	service.identityCalls++
	return protocol.ClientAccessIdentityResult{DeviceID: "device-1", DeviceFingerprint: "ed25519-sha256:daemon", DevicePublicKey: make([]byte, 32)}, nil
}

func (*scopeClientAccessService) CreateTicket(context.Context, protocol.ClientAccessTicketCreateParams) (protocol.ClientAccessTicketCreateResult, error) {
	return protocol.ClientAccessTicketCreateResult{}, nil
}

func (*scopeClientAccessService) List(context.Context) (protocol.ClientAccessListResult, error) {
	return protocol.ClientAccessListResult{}, nil
}

func (*scopeClientAccessService) Revoke(context.Context, protocol.ClientAccessRevokeParams) (protocol.ClientAccessRecord, error) {
	return protocol.ClientAccessRecord{}, nil
}
