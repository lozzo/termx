package plugin

import "testing"

func TestDefaultSystemEventCatalogIsValidAndUnique(t *testing.T) {
	events := DefaultSystemEventCatalog()
	if len(events) == 0 {
		t.Fatal("expected default system events")
	}
	seen := map[EventType]EventSpec{}
	for _, event := range events {
		if event.Type == "" || event.SourceHost == "" || event.DefaultDelivery.Mode == "" || event.ObjectKind == "" {
			t.Fatalf("event spec must be complete, got %#v", event)
		}
		if _, ok := seen[event.Type]; ok {
			t.Fatalf("duplicate event %s", event.Type)
		}
		seen[event.Type] = event
	}
	if _, ok := seen[SystemEventDaemonTerminalOutputIdle]; !ok {
		t.Fatalf("missing daemon pty idle event")
	}
	if _, ok := seen[SystemEventClientPanelBound]; !ok {
		t.Fatalf("missing client panel bound event")
	}
	if _, err := BuildPluginCatalog(nil, CatalogBuildConfig{EventCatalog: events}); err != nil {
		t.Fatalf("default event catalog should pass host validation: %v", err)
	}
}

func TestDefaultSystemEventCatalogContracts(t *testing.T) {
	events := map[EventType]EventSpec{}
	for _, event := range DefaultSystemEventCatalog() {
		events[event.Type] = event
	}
	for _, tc := range []struct {
		eventType EventType
		host      HostPlacement
		delivery  HookDeliveryMode
		cap       Capability
		lossy     bool
		object    string
	}{
		{SystemEventDaemonTerminalCreated, HostDaemon, DeliveryQueued, CapabilityTerminalLifecycleRead, false, ObjectKindTerminal},
		{SystemEventDaemonTerminalExited, HostDaemon, DeliveryQueued, CapabilityTerminalLifecycleRead, false, ObjectKindTerminal},
		{SystemEventDaemonTerminalRemoved, HostDaemon, DeliveryQueued, CapabilityTerminalLifecycleRead, false, ObjectKindTerminal},
		{SystemEventDaemonTerminalResized, HostDaemon, DeliveryCoalesced, CapabilityTerminalSizeRead, true, ObjectKindTerminal},
		{SystemEventDaemonTerminalOutputActivity, HostDaemon, DeliveryCoalesced, CapabilityTerminalActivityRead, true, ObjectKindTerminal},
		{SystemEventDaemonTerminalOutputIdle, HostDaemon, DeliveryCoalesced, CapabilityTerminalActivityRead, true, ObjectKindTerminal},
		{SystemEventDaemonTerminalOutputResumed, HostDaemon, DeliveryCoalesced, CapabilityTerminalActivityRead, true, ObjectKindTerminal},
		{SystemEventClientPanelCreated, HostClient, DeliveryQueued, CapabilityClientPanelRead, false, ObjectKindPanel},
		{SystemEventClientPanelClosed, HostClient, DeliveryQueued, CapabilityClientPanelRead, false, ObjectKindPanel},
		{SystemEventClientPanelBound, HostClient, DeliveryQueued, CapabilityClientPanelRead, false, ObjectKindPanel},
		{SystemEventClientPanelResized, HostClient, DeliveryCoalesced, CapabilityClientPanelRead, true, ObjectKindPanel},
		{SystemEventClientPanelFocused, HostClient, DeliveryCoalesced, CapabilityClientPanelRead, true, ObjectKindPanel},
		{SystemEventClientFloatCreated, HostClient, DeliveryQueued, CapabilityClientFloatRead, false, ObjectKindFloat},
		{SystemEventClientFloatClosed, HostClient, DeliveryQueued, CapabilityClientFloatRead, false, ObjectKindFloat},
		{SystemEventClientFloatResized, HostClient, DeliveryCoalesced, CapabilityClientFloatRead, true, ObjectKindFloat},
		{SystemEventClientFloatFocused, HostClient, DeliveryCoalesced, CapabilityClientFloatRead, true, ObjectKindFloat},
		{SystemEventClientTabCreated, HostClient, DeliveryQueued, CapabilityClientTabRead, false, ObjectKindTab},
		{SystemEventClientTabActivated, HostClient, DeliveryQueued, CapabilityClientTabRead, false, ObjectKindTab},
	} {
		event, ok := events[tc.eventType]
		if !ok {
			t.Fatalf("missing event %s", tc.eventType)
		}
		if event.SourceHost != tc.host || event.DefaultDelivery.Mode != tc.delivery || event.Lossy != tc.lossy || event.ObjectKind != tc.object || !containsCap(event.RequiredCaps, tc.cap) {
			t.Fatalf("event %s contract mismatch: %#v", tc.eventType, event)
		}
	}
}

func TestDefaultSystemEventCatalogSupportsHookManifestResolution(t *testing.T) {
	terminalRef := TerminalRef{EndpointID: "remote-a", TerminalID: "term-1"}
	manifest := PluginManifest{
		ID:   "acme.watch",
		Name: "Watch",
		API:  1,
		Hosts: []PluginHostManifest{
			{
				Placement:    HostClient,
				ClientKinds:  []ClientKind{ClientKindTUI},
				Runner:       RunnerSpec{Type: RunnerStdioJSON, Command: []string{"acme-watch"}},
				Capabilities: []Capability{CapabilityTerminalActivityRead, CapabilityClientPanelRead},
				Contributions: PluginContributions{
					Hooks: []HookContribution{
						{
							EventType: SystemEventDaemonTerminalOutputIdle,
							Handler:   "on_idle",
							Scope:     HookScope{EndpointID: "remote-a", TerminalRef: &terminalRef},
						},
						{
							EventType: SystemEventClientPanelFocused,
							Handler:   "on_panel",
							Scope:     HookScope{ClientSessionID: "tui-1"},
						},
					},
				},
			},
		},
	}
	catalog, err := BuildPluginCatalog([]PluginManifest{manifest}, CatalogBuildConfig{
		EventCatalog: DefaultSystemEventCatalog(),
		Grants: []CapabilityGrant{
			{
				PluginID: "acme.watch",
				Host:     HostClient,
				Capabilities: []Capability{
					CapabilityTerminalActivityRead,
					CapabilityClientPanelRead,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("build catalog: %v", err)
	}
	if len(catalog.Subscriptions) != 2 {
		t.Fatalf("expected two hook subscriptions, got %#v", catalog.Subscriptions)
	}
	if catalog.Subscriptions[0].Delivery.Mode != DeliveryCoalesced || !containsCap(catalog.Subscriptions[0].ResolvedCaps, CapabilityTerminalActivityRead) {
		t.Fatalf("daemon activity hook should inherit coalesced delivery and activity cap, got %#v", catalog.Subscriptions[0])
	}
	if catalog.Subscriptions[1].Delivery.Mode != DeliveryCoalesced || !containsCap(catalog.Subscriptions[1].ResolvedCaps, CapabilityClientPanelRead) {
		t.Fatalf("client focus hook should inherit coalesced delivery and panel cap, got %#v", catalog.Subscriptions[1])
	}
}
