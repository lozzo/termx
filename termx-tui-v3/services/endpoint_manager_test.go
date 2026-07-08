package services

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-shared/connection"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

func TestEndpointManagerRoutesLocalServicesAndRestoresEndpointID(t *testing.T) {
	terminal := &FakeTerminalService{
		AttachResult: TerminalAttachResult{TerminalID: "term-1", Channel: 7},
		ListResult: TerminalListResult{Items: []TerminalPoolItem{
			{TerminalID: "term-1", Title: "shell"},
		}},
		SurfaceResult: TerminalSurfaceResult{Snapshot: state.LiveSurfaceSnapshot{TerminalID: "term-1"}},
		PathResult: PathListDirectoriesResult{
			BasePath: "/home/me",
			Entries:  []PathDirectoryEntry{{Name: "src", Path: "~/src/"}},
		},
		PathDefaultsResult: PathDefaultsResult{
			DefaultCommand: []string{"/bin/zsh"},
			DefaultCWD:     "/Users/me",
		},
	}
	core := &FakeCoreClient{
		LatestResponses: []HistoryResult{
			{Window: state.HistoryWindow{TerminalID: "term-1", Token: "tok"}},
		},
	}
	manager := NewEndpointManager(connection.DefaultRegistry(), EndpointServiceBundle{
		EndpointID: state.DefaultEndpointID,
		Terminal:   terminal,
		Core:       core,
		Surface:    terminal,
		LiveEvents: terminal,
		Path:       terminal,
	})

	list, err := manager.List(context.Background(), TerminalListRequest{EndpointID: state.DefaultEndpointID})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(terminal.Lists) != 1 || terminal.Lists[0].EndpointID != "" {
		t.Fatalf("expected endpoint stripped before terminal list, got %#v", terminal.Lists)
	}
	if len(list.Items) != 1 || list.Items[0].EndpointID != state.DefaultEndpointID {
		t.Fatalf("expected endpoint restored on list items, got %#v", list.Items)
	}

	attached, err := manager.Attach(context.Background(), TerminalAttachRequest{EndpointID: state.DefaultEndpointID, TerminalID: "term-1"})
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if len(terminal.Attaches) != 1 || terminal.Attaches[0].EndpointID != "" {
		t.Fatalf("expected endpoint stripped before attach, got %#v", terminal.Attaches)
	}
	if attached.EndpointID != state.DefaultEndpointID || attached.TerminalID != "term-1" {
		t.Fatalf("expected endpoint restored on attach result, got %#v", attached)
	}

	history, err := manager.HistoryLatest(context.Background(), HistoryLatestRequest{EndpointID: state.DefaultEndpointID, TerminalID: "term-1", RequestID: 12})
	if err != nil {
		t.Fatalf("history latest: %v", err)
	}
	if len(core.LatestRequests) != 1 || core.LatestRequests[0].EndpointID != "" {
		t.Fatalf("expected endpoint stripped before history latest, got %#v", core.LatestRequests)
	}
	if history.Window.EndpointID != state.DefaultEndpointID || history.RequestID != 12 {
		t.Fatalf("expected endpoint and request id restored on history result, got %#v", history)
	}

	surface, err := manager.LiveSurface(context.Background(), TerminalSurfaceRequest{EndpointID: state.DefaultEndpointID, TerminalID: "term-1"})
	if err != nil {
		t.Fatalf("live surface: %v", err)
	}
	if len(terminal.Surfaces) != 1 || terminal.Surfaces[0].EndpointID != "" {
		t.Fatalf("expected endpoint stripped before live surface, got %#v", terminal.Surfaces)
	}
	if surface.Snapshot.EndpointID != state.DefaultEndpointID {
		t.Fatalf("expected endpoint restored on live surface, got %#v", surface)
	}

	paths, err := manager.ListDirectories(context.Background(), PathListDirectoriesRequest{EndpointID: state.DefaultEndpointID, Prefix: "~/s", Limit: 5})
	if err != nil {
		t.Fatalf("path list: %v", err)
	}
	if len(terminal.PathRequests) != 1 || terminal.PathRequests[0].EndpointID != "" || terminal.PathRequests[0].Prefix != "~/s" {
		t.Fatalf("expected endpoint stripped before path list, got %#v", terminal.PathRequests)
	}
	if paths.EndpointID != state.DefaultEndpointID || len(paths.Entries) != 1 || paths.Entries[0].Path != "~/src/" {
		t.Fatalf("expected endpoint restored on path result, got %#v", paths)
	}

	defaults, err := manager.Defaults(context.Background(), PathDefaultsRequest{EndpointID: state.DefaultEndpointID})
	if err != nil {
		t.Fatalf("path defaults: %v", err)
	}
	if len(terminal.PathDefaultsRequests) != 1 || terminal.PathDefaultsRequests[0].EndpointID != "" {
		t.Fatalf("expected endpoint stripped before path defaults, got %#v", terminal.PathDefaultsRequests)
	}
	if defaults.EndpointID != state.DefaultEndpointID || defaults.DefaultCWD != "/Users/me" || strings.Join(defaults.DefaultCommand, " ") != "/bin/zsh" {
		t.Fatalf("expected endpoint restored on path defaults, got %#v", defaults)
	}
}

func TestEndpointManagerRejectsDisabledUnsupportedAndUnregisteredEndpoints(t *testing.T) {
	registry := connection.Registry{
		Version: 1,
		Default: connection.DefaultEndpointID,
		Connections: map[connection.EndpointID]connection.Config{
			connection.DefaultEndpointID: {
				ID:          connection.DefaultEndpointID,
				Label:       "Local",
				Transport:   connection.TransportLocal,
				ConnectMode: connection.ConnectAuto,
				Enabled:     true,
				Socket:      "auto",
			},
			"disabled": {
				ID:          "disabled",
				Label:       "Disabled Local",
				Transport:   connection.TransportLocal,
				ConnectMode: connection.ConnectAuto,
				Enabled:     false,
				Socket:      "auto",
			},
			"west": {
				ID:           "west",
				Label:        "West",
				Transport:    connection.TransportSSH,
				Address:      "root@example.com",
				ConnectMode:  connection.ConnectOnDemand,
				Enabled:      true,
				RemoteSocket: "auto",
			},
		},
	}
	terminal := &FakeTerminalService{ListResult: TerminalListResult{}}
	manager := NewEndpointManager(registry, EndpointServiceBundle{EndpointID: state.DefaultEndpointID, Terminal: terminal})

	if _, err := manager.List(context.Background(), TerminalListRequest{EndpointID: "disabled"}); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("expected disabled endpoint error, got %v", err)
	}
	if _, err := manager.List(context.Background(), TerminalListRequest{EndpointID: "west"}); err == nil || !strings.Contains(err.Error(), "transport \"ssh\"") {
		t.Fatalf("expected unsupported transport error, got %v", err)
	}
	if _, err := manager.List(context.Background(), TerminalListRequest{EndpointID: "missing"}); err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("expected unregistered endpoint error, got %v", err)
	}
	if len(terminal.Lists) != 0 {
		t.Fatalf("unsupported endpoints must not fallback to local service, got calls %#v", terminal.Lists)
	}
}

func TestEndpointManagerLazilyDialsSSHTransport(t *testing.T) {
	registry := connection.Registry{
		Version: 1,
		Default: connection.DefaultEndpointID,
		Connections: map[connection.EndpointID]connection.Config{
			connection.DefaultEndpointID: {
				ID:          connection.DefaultEndpointID,
				Label:       "Local",
				Transport:   connection.TransportLocal,
				ConnectMode: connection.ConnectAuto,
				Enabled:     true,
				Socket:      "auto",
			},
			"west": {
				ID:           "west",
				Label:        "West",
				Transport:    connection.TransportSSH,
				Address:      "root@example.com",
				ConnectMode:  connection.ConnectOnDemand,
				Enabled:      true,
				RemoteSocket: "auto",
			},
		},
	}
	sshTerminal := &FakeTerminalService{ListResult: TerminalListResult{Items: []TerminalPoolItem{{TerminalID: "term-1", Title: "remote"}}}}
	dialCalls := 0
	manager := NewEndpointManagerWithDialers(registry, map[connection.TransportKind]EndpointDialer{
		connection.TransportSSH: func(_ context.Context, cfg connection.Config) (EndpointServiceBundle, error) {
			dialCalls++
			if cfg.ID != "west" || cfg.RemoteSocket != "auto" {
				t.Fatalf("unexpected ssh config %#v", cfg)
			}
			return EndpointServiceBundle{EndpointID: "west", Terminal: sshTerminal}, nil
		},
	})

	first, err := manager.List(context.Background(), TerminalListRequest{EndpointID: "west"})
	if err != nil {
		t.Fatalf("first list: %v", err)
	}
	second, err := manager.List(context.Background(), TerminalListRequest{EndpointID: "west"})
	if err != nil {
		t.Fatalf("second list: %v", err)
	}
	if dialCalls != 1 {
		t.Fatalf("expected one lazy dial, got %d", dialCalls)
	}
	if len(sshTerminal.Lists) != 2 || sshTerminal.Lists[0].EndpointID != "" || sshTerminal.Lists[1].EndpointID != "" {
		t.Fatalf("endpoint must be stripped before per-endpoint service, got %#v", sshTerminal.Lists)
	}
	if first.Items[0].EndpointID != "west" || second.Items[0].EndpointID != "west" {
		t.Fatalf("endpoint must be restored on ssh list rows, got first=%#v second=%#v", first.Items, second.Items)
	}
}

func TestEndpointManagerLazilyDialsHubP2PTransport(t *testing.T) {
	registry := connection.Registry{
		Version: 1,
		Default: connection.DefaultEndpointID,
		Connections: map[connection.EndpointID]connection.Config{
			connection.DefaultEndpointID: {
				ID:          connection.DefaultEndpointID,
				Label:       "Local",
				Transport:   connection.TransportLocal,
				ConnectMode: connection.ConnectAuto,
				Enabled:     true,
				Socket:      "auto",
			},
			"studio": {
				ID:          "studio",
				Label:       "Studio",
				Transport:   connection.TransportHubP2P,
				AuthRef:     "hub:personal",
				ConnectMode: connection.ConnectOnDemand,
				Enabled:     true,
				HubURL:      "https://hub.example.com",
				HubDeviceID: "device_ed25519:studio",
				RelayMode:   connection.RelayAuto,
			},
		},
	}
	hubTerminal := &FakeTerminalService{ListResult: TerminalListResult{Items: []TerminalPoolItem{{TerminalID: "term-1", Title: "remote"}}}}
	dialCalls := 0
	manager := NewEndpointManagerWithDialers(registry, map[connection.TransportKind]EndpointDialer{
		connection.TransportHubP2P: func(_ context.Context, cfg connection.Config) (EndpointServiceBundle, error) {
			dialCalls++
			if cfg.ID != "studio" || cfg.HubURL != "https://hub.example.com" || cfg.HubDeviceID != "device_ed25519:studio" || cfg.RelayMode != connection.RelayAuto {
				t.Fatalf("unexpected hub config %#v", cfg)
			}
			return EndpointServiceBundle{EndpointID: "studio", Terminal: hubTerminal}, nil
		},
	})

	result, err := manager.List(context.Background(), TerminalListRequest{EndpointID: "studio"})
	if err != nil {
		t.Fatalf("hub list: %v", err)
	}
	if dialCalls != 1 {
		t.Fatalf("expected one hub lazy dial, got %d", dialCalls)
	}
	if len(hubTerminal.Lists) != 1 || hubTerminal.Lists[0].EndpointID != "" {
		t.Fatalf("endpoint must be stripped before hub service, got %#v", hubTerminal.Lists)
	}
	if len(result.Items) != 1 || result.Items[0].EndpointID != "studio" || result.Items[0].TerminalID != "term-1" {
		t.Fatalf("endpoint must be restored on hub list rows, got %#v", result.Items)
	}
}

func TestEndpointManagerPublishesTransportCloseAndRedials(t *testing.T) {
	registry := connection.Registry{
		Version: 1,
		Default: connection.DefaultEndpointID,
		Connections: map[connection.EndpointID]connection.Config{
			connection.DefaultEndpointID: {
				ID:          connection.DefaultEndpointID,
				Label:       "Local",
				Transport:   connection.TransportLocal,
				ConnectMode: connection.ConnectAuto,
				Enabled:     true,
				Socket:      "auto",
			},
			"west": {
				ID:           "west",
				Label:        "West",
				Transport:    connection.TransportSSH,
				Address:      "root@example.com",
				ConnectMode:  connection.ConnectOnDemand,
				Enabled:      true,
				RemoteSocket: "auto",
			},
		},
	}
	done := make(chan struct{})
	closeErr := errors.New("ssh transport closed: exit status 255")
	initialTerminal := &FakeTerminalService{ListResult: TerminalListResult{Items: []TerminalPoolItem{{TerminalID: "old"}}}}
	redialTerminal := &FakeTerminalService{ListResult: TerminalListResult{Items: []TerminalPoolItem{{TerminalID: "new"}}}}
	dialCalls := 0
	manager := NewEndpointManagerWithDialers(registry, map[connection.TransportKind]EndpointDialer{
		connection.TransportSSH: func(context.Context, connection.Config) (EndpointServiceBundle, error) {
			dialCalls++
			return EndpointServiceBundle{EndpointID: "west", Terminal: redialTerminal}, nil
		},
	}, EndpointServiceBundle{
		EndpointID: "west",
		Terminal:   initialTerminal,
		Lifecycle:  EndpointLifecycle{Done: done, Err: func() error { return closeErr }},
	})
	events, err := manager.WatchEndpointEvents(context.Background())
	if err != nil {
		t.Fatalf("watch endpoint events: %v", err)
	}

	close(done)

	var event EndpointRuntimeEvent
	select {
	case event = <-events:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for endpoint close event")
	}
	if event.EndpointID != "west" || event.Status != state.EndpointStatusOffline || event.ErrorKind != state.EndpointErrorTransportClosed || !strings.Contains(event.Message, "ssh transport closed") {
		t.Fatalf("unexpected endpoint close event %#v", event)
	}

	result, err := manager.List(context.Background(), TerminalListRequest{EndpointID: "west"})
	if err != nil {
		t.Fatalf("redial list: %v", err)
	}
	if dialCalls != 1 || len(result.Items) != 1 || result.Items[0].TerminalID != "new" {
		t.Fatalf("closed bundle should be discarded and redialed, calls=%d result=%#v", dialCalls, result)
	}
}

func TestEndpointManagerEndpointStoreUsesRegistryProjection(t *testing.T) {
	registry := connection.Registry{
		Version: 1,
		Default: connection.DefaultEndpointID,
		Connections: map[connection.EndpointID]connection.Config{
			connection.DefaultEndpointID: {
				ID:          connection.DefaultEndpointID,
				Label:       "This Mac",
				Transport:   connection.TransportLocal,
				ConnectMode: connection.ConnectAuto,
				Enabled:     true,
				Socket:      "auto",
			},
			"manual": {
				ID:          "manual",
				Label:       "Manual Box",
				Transport:   connection.TransportLocal,
				ConnectMode: connection.ConnectManual,
				Enabled:     true,
				Socket:      "/tmp/termx.sock",
			},
		},
	}
	manager := NewEndpointManager(registry)
	store := manager.EndpointStore()
	if len(store.Items) != 2 {
		t.Fatalf("expected two endpoint items, got %#v", store.Items)
	}
	manual, ok := store.Endpoint("manual")
	if !ok {
		t.Fatalf("manual endpoint missing from store %#v", store.Items)
	}
	if manual.DisplayLabel() != "Manual Box" || manual.DisplayStatus() != state.EndpointStatusManual || manual.Socket != "/tmp/termx.sock" {
		t.Fatalf("unexpected manual endpoint projection %#v", manual)
	}
}
