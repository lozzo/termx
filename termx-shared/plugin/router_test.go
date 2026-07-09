package plugin

import "testing"

func TestHookRouterFiltersSelfCausedAndScope(t *testing.T) {
	router := NewHookRouter(HookRouterConfig{MaxDepth: 8})
	event := HookEvent{
		EventID:       "event-1",
		Type:          "termx.client.panel.created",
		SourceHost:    HostClient,
		SourceSession: "tui-1",
		WorkspaceID:   "workspace-1",
		ObjectKind:    "panel",
		ObjectID:      "pane-1",
		Trace: MessageTrace{
			TraceID:   "trace-1",
			ActorPath: []PluginID{"plugin-a"},
		},
	}
	subs := []HookSubscription{
		{
			PluginID:   "plugin-a",
			Host:       HostClient,
			EventTypes: []EventType{"termx.client.panel.created"},
			Scope:      HookScope{ClientSessionID: "tui-1"},
			Delivery:   HookDelivery{Mode: DeliveryQueued},
		},
		{
			PluginID:   "plugin-b",
			Host:       HostClient,
			EventTypes: []EventType{"termx.client.panel.created"},
			Scope:      HookScope{ClientSessionID: "tui-1"},
			Delivery:   HookDelivery{Mode: DeliveryQueued},
		},
		{
			PluginID:   "plugin-c",
			Host:       HostClient,
			EventTypes: []EventType{"termx.client.panel.created"},
			Scope:      HookScope{ClientSessionID: "tui-2"},
			Delivery:   HookDelivery{Mode: DeliveryQueued},
		},
	}

	result := router.Dispatch(event, subs)
	if len(result.Deliveries) != 1 || result.Deliveries[0].PluginID != "plugin-b" {
		t.Fatalf("expected only plugin-b delivery, got %#v", result.Deliveries)
	}
	delivered := result.Deliveries[0].Event
	if delivered.Trace.Depth != 1 || delivered.Trace.LastPluginID != "plugin-b" || !delivered.Trace.ContainsActor("plugin-b") {
		t.Fatalf("delivery should advance trace for plugin-b, got %#v", delivered.Trace)
	}
	if !hasDrop(result.Drops, "plugin-a", DropSelfCaused) {
		t.Fatalf("expected self-caused drop, got %#v", result.Drops)
	}
	if !hasDrop(result.Drops, "plugin-c", DropScope) {
		t.Fatalf("expected scope drop, got %#v", result.Drops)
	}
}

func TestHookRouterDedupeIncludesSourceScope(t *testing.T) {
	router := NewHookRouter(HookRouterConfig{MaxDepth: 8})
	sub := HookSubscription{
		PluginID:   "plugin-a",
		Host:       HostClient,
		EventTypes: []EventType{"termx.client.panel.created"},
		Delivery:   HookDelivery{Mode: DeliveryQueued},
	}
	first := HookEvent{
		EventID:       "event-1",
		Type:          "termx.client.panel.created",
		SourceHost:    HostClient,
		SourceSession: "tui-1",
		ObjectKind:    "panel",
		ObjectID:      "pane-1",
		Trace:         MessageTrace{TraceID: "trace-1"},
	}
	second := first
	second.EventID = "event-2"
	second.SourceSession = "tui-2"

	if got := router.Dispatch(first, []HookSubscription{sub}); len(got.Deliveries) != 1 {
		t.Fatalf("first event should deliver, got %#v", got)
	}
	if got := router.Dispatch(second, []HookSubscription{sub}); len(got.Deliveries) != 1 {
		t.Fatalf("same object id in different session should deliver, got %#v", got)
	}
	if got := router.Dispatch(first, []HookSubscription{sub}); len(got.Deliveries) != 0 || !hasDrop(got.Drops, "plugin-a", DropDuplicate) {
		t.Fatalf("same scoped event should dedupe, got %#v", got)
	}
}

func TestHookScopeSeparatesDaemonLocalAndClientTerminalRef(t *testing.T) {
	router := NewHookRouter(HookRouterConfig{MaxDepth: 8})
	daemonSub := HookSubscription{
		PluginID: "plugin-daemon",
		Host:     HostDaemon,
		EventTypes: []EventType{
			"termx.daemon.terminal.output_idle",
		},
		Scope: HookScope{
			DaemonID:         "daemon-a",
			DaemonTerminalID: "term-1",
		},
		Delivery: HookDelivery{Mode: DeliveryCoalesced},
	}
	clientRef := TerminalRef{EndpointID: "remote-a", TerminalID: "term-1"}
	clientSub := HookSubscription{
		PluginID:   "plugin-client",
		Host:       HostClient,
		EventTypes: []EventType{"termx.daemon.terminal.output_idle"},
		Scope: HookScope{
			EndpointID:  "remote-a",
			TerminalRef: &clientRef,
		},
		Delivery: HookDelivery{Mode: DeliveryCoalesced},
	}
	daemonEvent := HookEvent{
		EventID:          "event-daemon",
		Type:             "termx.daemon.terminal.output_idle",
		SourceHost:       HostDaemon,
		DaemonID:         "daemon-a",
		DaemonTerminalID: "term-1",
		ObjectKind:       "terminal",
		ObjectID:         "term-1",
		Trace:            MessageTrace{TraceID: "trace-daemon"},
	}
	clientEvent := daemonEvent
	clientEvent.EventID = "event-client"
	clientEvent.SourceHost = HostClient
	clientEvent.SourceSession = "tui-1"
	clientEvent.EndpointID = "remote-a"
	clientEvent.TerminalRef = &clientRef
	clientEvent.Trace = MessageTrace{TraceID: "trace-client"}

	gotDaemon := router.Dispatch(daemonEvent, []HookSubscription{daemonSub, clientSub})
	if len(gotDaemon.Deliveries) != 1 || gotDaemon.Deliveries[0].PluginID != "plugin-daemon" {
		t.Fatalf("daemon event should match daemon-local scope only, got %#v", gotDaemon)
	}
	gotClient := router.Dispatch(clientEvent, []HookSubscription{daemonSub, clientSub})
	if len(gotClient.Deliveries) != 2 {
		t.Fatalf("client-enriched event should match both daemon and client scopes, got %#v", gotClient)
	}
}

func TestHookRouterDedupeAndScopeSeparateEndpointTerminalRefs(t *testing.T) {
	router := NewHookRouter(HookRouterConfig{MaxDepth: 8})
	remoteA := TerminalRef{EndpointID: "remote-a", TerminalID: "term-1"}
	remoteB := TerminalRef{EndpointID: "remote-b", TerminalID: "term-1"}
	subA := HookSubscription{
		PluginID:   "plugin-a",
		Host:       HostClient,
		EventTypes: []EventType{"termx.daemon.terminal.output_idle"},
		Scope: HookScope{
			EndpointID:  "remote-a",
			TerminalRef: &remoteA,
		},
		Delivery: HookDelivery{Mode: DeliveryCoalesced},
	}
	eventA := HookEvent{
		EventID:     "event-a",
		Type:        "termx.daemon.terminal.output_idle",
		SourceHost:  HostClient,
		EndpointID:  "remote-a",
		TerminalRef: &remoteA,
		ObjectKind:  "terminal",
		ObjectID:    "term-1",
		Trace:       MessageTrace{TraceID: "trace-endpoint"},
	}
	eventB := eventA
	eventB.EventID = "event-b"
	eventB.EndpointID = "remote-b"
	eventB.TerminalRef = &remoteB

	got := router.Dispatch(eventA, []HookSubscription{subA})
	if len(got.Deliveries) != 1 {
		t.Fatalf("remote-a event should deliver, got %#v", got)
	}
	got = router.Dispatch(eventB, []HookSubscription{subA})
	if len(got.Deliveries) != 0 || !hasDrop(got.Drops, "plugin-a", DropScope) {
		t.Fatalf("remote-b event should not match remote-a scope, got %#v", got)
	}

	subAll := subA
	subAll.Scope = HookScope{}
	router = NewHookRouter(HookRouterConfig{MaxDepth: 8})
	got = router.Dispatch(eventA, []HookSubscription{subAll})
	if len(got.Deliveries) != 1 {
		t.Fatalf("first endpoint should deliver for broad scope, got %#v", got)
	}
	got = router.Dispatch(eventB, []HookSubscription{subAll})
	if len(got.Deliveries) != 1 {
		t.Fatalf("same terminal id on different endpoint should not dedupe, got %#v", got)
	}
}

func TestHookRouterUsesTerminalRefEndpointWhenTopLevelEndpointMissing(t *testing.T) {
	router := NewHookRouter(HookRouterConfig{MaxDepth: 8})
	remoteA := TerminalRef{EndpointID: "remote-a", TerminalID: "term-1"}
	remoteB := TerminalRef{EndpointID: "remote-b", TerminalID: "term-1"}
	subA := HookSubscription{
		PluginID:   "plugin-a",
		Host:       HostClient,
		EventTypes: []EventType{"termx.daemon.terminal.output_idle"},
		Scope: HookScope{
			EndpointID:  "remote-a",
			TerminalRef: &remoteA,
		},
		Delivery: HookDelivery{Mode: DeliveryCoalesced},
	}
	eventA := HookEvent{
		EventID:     "event-a",
		Type:        "termx.daemon.terminal.output_idle",
		SourceHost:  HostClient,
		TerminalRef: &remoteA,
		ObjectKind:  "terminal",
		ObjectID:    "term-1",
		Trace:       MessageTrace{TraceID: "trace-terminal-ref-only"},
	}
	eventB := eventA
	eventB.EventID = "event-b"
	eventB.TerminalRef = &remoteB

	got := router.Dispatch(eventA, []HookSubscription{subA})
	if len(got.Deliveries) != 1 {
		t.Fatalf("terminal ref endpoint should satisfy scope when top-level endpoint is empty, got %#v", got)
	}
	got = router.Dispatch(eventB, []HookSubscription{subA})
	if len(got.Deliveries) != 0 || !hasDrop(got.Drops, "plugin-a", DropScope) {
		t.Fatalf("different terminal ref endpoint should not match scope, got %#v", got)
	}

	router = NewHookRouter(HookRouterConfig{MaxDepth: 8})
	broad := subA
	broad.Scope = HookScope{}
	got = router.Dispatch(eventA, []HookSubscription{broad})
	if len(got.Deliveries) != 1 {
		t.Fatalf("first terminal-ref-only event should deliver, got %#v", got)
	}
	got = router.Dispatch(eventB, []HookSubscription{broad})
	if len(got.Deliveries) != 1 {
		t.Fatalf("terminal-ref-only events on different endpoints should not dedupe, got %#v", got)
	}
}

func TestHookRouterEnforcesTraceBudgetAndDepth(t *testing.T) {
	router := NewHookRouter(HookRouterConfig{MaxDepth: 1, MaxTraceDeliveries: 1})
	subA := HookSubscription{PluginID: "plugin-a", EventTypes: []EventType{"event"}, Delivery: HookDelivery{Mode: DeliveryQueued}}
	subB := HookSubscription{PluginID: "plugin-b", EventTypes: []EventType{"event"}, Delivery: HookDelivery{Mode: DeliveryQueued}}
	event := HookEvent{EventID: "event-1", Type: "event", Trace: MessageTrace{TraceID: "trace-budget"}}

	got := router.Dispatch(event, []HookSubscription{subA, subB})
	if len(got.Deliveries) != 1 || !hasDrop(got.Drops, "plugin-b", DropTraceBudget) {
		t.Fatalf("expected one delivery and one trace budget drop, got %#v", got)
	}

	deep := event
	deep.EventID = "event-deep"
	deep.Trace = MessageTrace{TraceID: "trace-deep", Depth: 1}
	got = router.Dispatch(deep, []HookSubscription{subA})
	if len(got.Deliveries) != 0 || !hasDrop(got.Drops, "plugin-a", DropMaxDepth) {
		t.Fatalf("expected max depth drop, got %#v", got)
	}
}

func TestDeliveryModeLossyClassification(t *testing.T) {
	if DeliveryStrictQueued.Lossy() || DeliveryQueued.Lossy() {
		t.Fatalf("strict and queued modes must not be lossy")
	}
	if !DeliveryLatest.Lossy() || !DeliveryCoalesced.Lossy() {
		t.Fatalf("latest and coalesced modes must be lossy")
	}
}

func TestValidateHookSubscriptionRequiresRouterFields(t *testing.T) {
	valid := HookSubscription{
		PluginID:   "plugin-a",
		Host:       HostClient,
		EventTypes: []EventType{"termx.client.panel.created"},
		Delivery:   HookDelivery{Mode: DeliveryQueued},
	}
	if err := ValidateHookSubscription(valid); err != nil {
		t.Fatalf("valid subscription should pass: %v", err)
	}

	cases := []struct {
		name string
		sub  HookSubscription
	}{
		{name: "plugin", sub: HookSubscription{Host: HostClient, EventTypes: valid.EventTypes, Delivery: valid.Delivery}},
		{name: "host", sub: HookSubscription{PluginID: valid.PluginID, EventTypes: valid.EventTypes, Delivery: valid.Delivery}},
		{name: "events", sub: HookSubscription{PluginID: valid.PluginID, Host: HostClient, Delivery: valid.Delivery}},
		{name: "delivery", sub: HookSubscription{PluginID: valid.PluginID, Host: HostClient, EventTypes: valid.EventTypes}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateHookSubscription(tc.sub); err == nil {
				t.Fatalf("expected validation error for %s", tc.name)
			}
		})
	}
}

func hasDrop(drops []HookDrop, pluginID PluginID, reason HookDropReason) bool {
	for _, drop := range drops {
		if drop.PluginID == pluginID && drop.Reason == reason {
			return true
		}
	}
	return false
}
