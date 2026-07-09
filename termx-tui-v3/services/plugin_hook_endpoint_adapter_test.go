package services

import (
	"testing"

	"github.com/lozzow/termx/termx-shared/plugin"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

func TestEnrichDaemonHookForEndpointKeepsDaemonOwnerAndAddsTerminalRef(t *testing.T) {
	event := plugin.HookEvent{
		EventID:          "event-1",
		Type:             plugin.SystemEventDaemonTerminalOutputIdle,
		SourceHost:       plugin.HostDaemon,
		DaemonID:         "daemon-a",
		DaemonTerminalID: "term-1",
		ObjectKind:       plugin.ObjectKindTerminal,
		ObjectID:         "term-1",
		Payload:          []byte(`{"idle":true}`),
	}
	enriched := EnrichDaemonHookForEndpoint(event, "remote-a")
	if enriched.SourceHost != plugin.HostDaemon || enriched.DaemonTerminalID != "term-1" {
		t.Fatalf("daemon owner fields must be preserved, got %#v", enriched)
	}
	if enriched.EndpointID != "remote-a" || enriched.TerminalRef == nil || enriched.TerminalRef.EndpointID != "remote-a" || enriched.TerminalRef.TerminalID != "term-1" {
		t.Fatalf("expected endpoint terminal ref, got %#v", enriched)
	}
	enriched.Payload[0] = 'x'
	if string(event.Payload) != `{"idle":true}` {
		t.Fatalf("enrichment must clone original payload, got %q", event.Payload)
	}
}

func TestEnrichDaemonHookForEndpointSeparatesSameTerminalIDAcrossEndpoints(t *testing.T) {
	event := plugin.HookEvent{
		EventID:          "event-1",
		Type:             plugin.SystemEventDaemonTerminalOutputIdle,
		SourceHost:       plugin.HostDaemon,
		DaemonTerminalID: "term-1",
	}
	west := EnrichDaemonHookForEndpoint(event, "west")
	east := EnrichDaemonHookForEndpoint(event, "east")
	if west.TerminalRef == nil || east.TerminalRef == nil || west.TerminalRef.Equal(*east.TerminalRef) {
		t.Fatalf("same daemon terminal id on different endpoints must stay distinct, west=%#v east=%#v", west.TerminalRef, east.TerminalRef)
	}
	local := EnrichDaemonHookForEndpoint(event, "")
	if local.EndpointID != plugin.EndpointID(state.DefaultEndpointID) || local.TerminalRef == nil || local.TerminalRef.EndpointID != plugin.EndpointID(state.DefaultEndpointID) {
		t.Fatalf("empty endpoint should normalize to local, got %#v", local)
	}
}

func TestEnrichDaemonHookForEndpointDoesNotRewriteClientEvent(t *testing.T) {
	event := plugin.HookEvent{
		EventID:    "event-client",
		Type:       plugin.SystemEventClientPanelFocused,
		SourceHost: plugin.HostClient,
		ObjectKind: plugin.ObjectKindPanel,
		ObjectID:   "pane-1",
	}
	enriched := EnrichDaemonHookForEndpoint(event, "remote-a")
	if enriched.EndpointID != "" || enriched.TerminalRef != nil || enriched.SourceHost != plugin.HostClient {
		t.Fatalf("client event should not be endpoint-enriched by daemon bridge, got %#v", enriched)
	}
}
