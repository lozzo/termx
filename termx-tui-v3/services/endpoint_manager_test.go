package services

import (
	"context"
	"strings"
	"testing"

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
