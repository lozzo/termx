package plugin

import "testing"

func TestBuildPluginCatalogResolvesManifestWithGrant(t *testing.T) {
	terminalRef := TerminalRef{EndpointID: "remote-a", TerminalID: "codex"}
	manifest := PluginManifest{
		ID:   "acme.deploy",
		Name: "Deploy Tools",
		API:  1,
		Hosts: []PluginHostManifest{
			{
				Placement:   HostClient,
				ClientKinds: []ClientKind{ClientKindTUI, ClientKindGUI},
				Runner:      RunnerSpec{Type: RunnerStdioJSON, Command: []string{"acme-deploy"}},
				Capabilities: []Capability{
					"client.panel.close",
					"terminal.kill",
					"terminal.activity.read",
				},
				Contributions: PluginContributions{
					Actions: []ActionContribution{
						{
							ID:                 "acme.deploy.panel.close_and_kill_terminal",
							Scope:              ActionScopeClient,
							ClientRequiredCaps: []Capability{"client.panel.close"},
							DaemonRequiredCaps: []Capability{"terminal.kill"},
							Danger:             DangerDestructive,
							ParamsSchema:       "builtin:empty",
						},
					},
					Keybindings: []KeybindingContribution{
						{
							Keys:     []string{"ctrl+w x"},
							ActionID: "acme.deploy.panel.close_and_kill_terminal",
						},
					},
					Hooks: []HookContribution{
						{
							EventType: "termx.daemon.terminal.output_idle",
							Handler:   "on_terminal_idle",
							Scope: HookScope{
								EndpointID:  "remote-a",
								TerminalRef: &terminalRef,
							},
						},
					},
				},
			},
		},
	}
	catalog, err := BuildPluginCatalog([]PluginManifest{manifest}, CatalogBuildConfig{
		EventCatalog: []EventSpec{
			{
				Type:            "termx.daemon.terminal.output_idle",
				SourceHost:      HostDaemon,
				DefaultDelivery: HookDelivery{Mode: DeliveryCoalesced},
				RequiredCaps:    []Capability{"terminal.activity.read"},
				Lossy:           true,
				ObjectKind:      "terminal",
			},
		},
		Grants: []CapabilityGrant{
			{
				PluginID: "acme.deploy",
				Host:     HostClient,
				Capabilities: []Capability{
					"client.panel.close",
					"terminal.kill",
					"terminal.activity.read",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("build catalog: %v", err)
	}
	if len(catalog.Diagnostics) != 0 {
		t.Fatalf("expected no diagnostics, got %#v", catalog.Diagnostics)
	}

	action, ok := catalog.Actions["acme.deploy.panel.close_and_kill_terminal"]
	if !ok {
		t.Fatalf("expected action in catalog")
	}
	if action.OwnerPluginID != "acme.deploy" || action.Scope != ActionScopeClient || action.Danger != DangerDestructive {
		t.Fatalf("unexpected action spec %#v", action)
	}
	if len(action.SupportedClientKinds) != 2 || action.SupportedClientKinds[0] != ClientKindTUI || action.SupportedClientKinds[1] != ClientKindGUI {
		t.Fatalf("client kinds should be inherited from client host, got %#v", action.SupportedClientKinds)
	}
	if len(catalog.Keybindings) != 1 || catalog.Keybindings[0].ActionID != action.ID || catalog.Keybindings[0].Keys[0] != "ctrl+w x" {
		t.Fatalf("expected keybinding to resolved action, got %#v", catalog.Keybindings)
	}
	if len(catalog.Keybindings[0].ClientKinds) != 2 || catalog.Keybindings[0].ClientKinds[0] != ClientKindTUI || catalog.Keybindings[0].ClientKinds[1] != ClientKindGUI {
		t.Fatalf("keybinding should inherit host client kinds, got %#v", catalog.Keybindings[0].ClientKinds)
	}
	if len(catalog.Subscriptions) != 1 {
		t.Fatalf("expected one hook subscription, got %#v", catalog.Subscriptions)
	}
	sub := catalog.Subscriptions[0]
	if sub.Host != HostClient || sub.Delivery.Mode != DeliveryCoalesced || !containsCap(sub.ResolvedCaps, "terminal.activity.read") {
		t.Fatalf("subscription should inherit event delivery and caps, got %#v", sub)
	}
	terminalRef.TerminalID = "mutated"
	if sub.Scope.TerminalRef == nil || sub.Scope.TerminalRef.TerminalID != "codex" {
		t.Fatalf("subscription scope must be cloned, got %#v", sub.Scope.TerminalRef)
	}
}

func TestBuildPluginCatalogDoesNotTrustManifestCapabilitiesAsGrant(t *testing.T) {
	manifest := manifestWithAction("acme.deploy", "acme.deploy.terminal.kill", []Capability{"terminal.kill"}, DangerDestructive)
	catalog, err := BuildPluginCatalog([]PluginManifest{manifest}, CatalogBuildConfig{})
	if err != nil {
		t.Fatalf("build catalog should not fail on missing grant: %v", err)
	}
	if _, ok := catalog.Actions["acme.deploy.terminal.kill"]; ok {
		t.Fatalf("action should not be registered without host grant")
	}
	if !hasDiagnostic(catalog.Diagnostics, "action", "acme.deploy.terminal.kill", DiagnosticMissingCapability) {
		t.Fatalf("expected missing capability diagnostic, got %#v", catalog.Diagnostics)
	}
}

func TestGrantMustMatchPluginAndHost(t *testing.T) {
	manifest := manifestWithAction("acme.deploy", "acme.deploy.terminal.kill", []Capability{"terminal.kill"}, DangerDestructive)
	for _, tc := range []struct {
		name  string
		grant CapabilityGrant
	}{
		{
			name:  "wrong-plugin",
			grant: CapabilityGrant{PluginID: "acme.other", Host: HostClient, Capabilities: []Capability{"terminal.kill"}},
		},
		{
			name:  "wrong-host",
			grant: CapabilityGrant{PluginID: "acme.deploy", Host: HostDaemon, Capabilities: []Capability{"terminal.kill"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			catalog, err := BuildPluginCatalog([]PluginManifest{manifest}, CatalogBuildConfig{Grants: []CapabilityGrant{tc.grant}})
			if err != nil {
				t.Fatalf("wrong grant should not be structural error: %v", err)
			}
			if _, ok := catalog.Actions["acme.deploy.terminal.kill"]; ok {
				t.Fatalf("action should not be registered with %s grant", tc.name)
			}
			if !hasDiagnostic(catalog.Diagnostics, "action", "acme.deploy.terminal.kill", DiagnosticMissingCapability) {
				t.Fatalf("expected missing capability diagnostic, got %#v", catalog.Diagnostics)
			}
		})
	}
}

func TestThirdPartyCannotContributeTermXNamespace(t *testing.T) {
	manifest := manifestWithAction("acme.deploy", "termx.client.panel.close", []Capability{"client.panel.close"}, DangerNone)
	if _, err := BuildPluginCatalog([]PluginManifest{manifest}, CatalogBuildConfig{}); err == nil {
		t.Fatalf("third-party termx namespace action should fail")
	}
}

func TestTrustedTermXPluginCanContributeTermXNamespace(t *testing.T) {
	manifest := manifestWithAction("termx.builtin.workspace", "termx.client.panel.close", []Capability{"client.panel.close"}, DangerNone)
	catalog, err := BuildPluginCatalog([]PluginManifest{manifest}, CatalogBuildConfig{
		TrustedTermXPlugins: []PluginID{"termx.builtin.workspace"},
		Grants: []CapabilityGrant{
			{PluginID: "termx.builtin.workspace", Host: HostClient, Capabilities: []Capability{"client.panel.close"}},
		},
	})
	if err != nil {
		t.Fatalf("trusted termx action should pass: %v", err)
	}
	if _, ok := catalog.Actions["termx.client.panel.close"]; !ok {
		t.Fatalf("trusted termx action should be registered")
	}
}

func TestTermXPluginIDRequiresHostTrust(t *testing.T) {
	manifest := manifestWithAction("termx.evil", "termx.evil.panel.close", []Capability{"client.panel.close"}, DangerNone)
	if _, err := BuildPluginCatalog([]PluginManifest{manifest}, CatalogBuildConfig{}); err == nil {
		t.Fatalf("termx plugin id should require trusted host config")
	}
}

func TestHookRequiresKnownEventAndSelfCausedGrant(t *testing.T) {
	manifest := PluginManifest{
		ID:   "acme.deploy",
		Name: "Deploy Tools",
		API:  1,
		Hosts: []PluginHostManifest{
			{
				Placement:    HostClient,
				ClientKinds:  []ClientKind{ClientKindTUI},
				Runner:       RunnerSpec{Type: RunnerStdioJSON, Command: []string{"acme-deploy"}},
				Capabilities: []Capability{"terminal.activity.read", CapabilityHookReceiveSelfCaused},
				Contributions: PluginContributions{
					Hooks: []HookContribution{
						{
							EventType:         "termx.daemon.terminal.output_idle",
							Handler:           "on_idle",
							Scope:             HookScope{EndpointID: "remote-a"},
							ReceiveSelfCaused: true,
						},
					},
				},
			},
		},
	}
	eventCatalog := []EventSpec{
		{
			Type:            "termx.daemon.terminal.output_idle",
			SourceHost:      HostDaemon,
			DefaultDelivery: HookDelivery{Mode: DeliveryCoalesced},
			RequiredCaps:    []Capability{"terminal.activity.read"},
		},
	}

	catalog, err := BuildPluginCatalog([]PluginManifest{manifest}, CatalogBuildConfig{
		EventCatalog: eventCatalog,
		Grants: []CapabilityGrant{
			{PluginID: "acme.deploy", Host: HostClient, Capabilities: []Capability{"terminal.activity.read"}},
		},
	})
	if err != nil {
		t.Fatalf("missing self-caused grant should be diagnostic, not structural error: %v", err)
	}
	if len(catalog.Subscriptions) != 0 {
		t.Fatalf("self-caused hook should not register without explicit grant")
	}
	if !hasDiagnostic(catalog.Diagnostics, "hook", "termx.daemon.terminal.output_idle", DiagnosticMissingCapability) {
		t.Fatalf("expected hook missing capability diagnostic, got %#v", catalog.Diagnostics)
	}

	catalog, err = BuildPluginCatalog([]PluginManifest{manifest}, CatalogBuildConfig{
		EventCatalog: eventCatalog,
		Grants: []CapabilityGrant{
			{
				PluginID:     "acme.deploy",
				Host:         HostClient,
				Capabilities: []Capability{"terminal.activity.read", CapabilityHookReceiveSelfCaused},
			},
		},
	})
	if err != nil {
		t.Fatalf("self-caused hook with grant should pass: %v", err)
	}
	if len(catalog.Subscriptions) != 1 || !catalog.Subscriptions[0].ReceiveSelfCaused {
		t.Fatalf("expected self-caused hook subscription, got %#v", catalog.Subscriptions)
	}

	unknown := manifest
	unknown.Hosts[0].Contributions.Hooks[0].EventType = "termx.daemon.terminal.unknown"
	if _, err := BuildPluginCatalog([]PluginManifest{unknown}, CatalogBuildConfig{EventCatalog: eventCatalog}); err == nil {
		t.Fatalf("unknown system event should fail")
	}
}

func TestThirdPartyWildcardHookRequiresTrustedGrant(t *testing.T) {
	manifest := PluginManifest{
		ID:   "acme.deploy",
		Name: "Deploy Tools",
		API:  1,
		Hosts: []PluginHostManifest{
			{
				Placement:    HostClient,
				ClientKinds:  []ClientKind{ClientKindTUI},
				Runner:       RunnerSpec{Type: RunnerStdioJSON, Command: []string{"acme-deploy"}},
				Capabilities: []Capability{"terminal.activity.read"},
				Contributions: PluginContributions{
					Hooks: []HookContribution{
						{
							EventType: "termx.daemon.terminal.output_idle",
							Handler:   "on_idle",
						},
					},
				},
			},
		},
	}
	config := CatalogBuildConfig{
		EventCatalog: []EventSpec{
			{
				Type:            "termx.daemon.terminal.output_idle",
				SourceHost:      HostDaemon,
				DefaultDelivery: HookDelivery{Mode: DeliveryCoalesced},
				RequiredCaps:    []Capability{"terminal.activity.read"},
			},
		},
		Grants: []CapabilityGrant{
			{PluginID: "acme.deploy", Host: HostClient, Capabilities: []Capability{"terminal.activity.read"}},
		},
	}
	catalog, err := BuildPluginCatalog([]PluginManifest{manifest}, config)
	if err != nil {
		t.Fatalf("wildcard should be diagnostic, not structural error: %v", err)
	}
	if len(catalog.Subscriptions) != 0 || !hasDiagnostic(catalog.Diagnostics, "hook", "termx.daemon.terminal.output_idle", DiagnosticUnsupportedContribution) {
		t.Fatalf("wildcard hook should be rejected for untrusted grant, got subs=%#v diagnostics=%#v", catalog.Subscriptions, catalog.Diagnostics)
	}

	config.Grants[0].Trusted = true
	catalog, err = BuildPluginCatalog([]PluginManifest{manifest}, config)
	if err != nil {
		t.Fatalf("trusted wildcard hook should pass: %v", err)
	}
	if len(catalog.Subscriptions) != 1 {
		t.Fatalf("trusted wildcard hook should register, got %#v", catalog.Subscriptions)
	}
}

func TestEventCatalogRequiresOwnerNamespaceMatch(t *testing.T) {
	manifest := manifestWithAction("acme.deploy", "acme.deploy.panel.close", []Capability{"client.panel.close"}, DangerNone)
	_, err := BuildPluginCatalog([]PluginManifest{manifest}, CatalogBuildConfig{
		EventCatalog: []EventSpec{
			{Type: "termx.client.panel.closed", SourceHost: HostDaemon, DefaultDelivery: HookDelivery{Mode: DeliveryQueued}},
		},
	})
	if err == nil {
		t.Fatalf("client event with daemon source host should fail")
	}

	_, err = BuildPluginCatalog([]PluginManifest{manifest}, CatalogBuildConfig{
		EventCatalog: []EventSpec{
			{Type: "termx.unknown.panel.closed", SourceHost: HostClient, DefaultDelivery: HookDelivery{Mode: DeliveryQueued}},
		},
	})
	if err == nil {
		t.Fatalf("unknown termx event owner should fail")
	}
}

func TestHookDeliveryCannotOverrideEventPolicy(t *testing.T) {
	manifest := PluginManifest{
		ID:   "acme.deploy",
		Name: "Deploy Tools",
		API:  1,
		Hosts: []PluginHostManifest{
			{
				Placement:    HostClient,
				ClientKinds:  []ClientKind{ClientKindTUI},
				Runner:       RunnerSpec{Type: RunnerStdioJSON, Command: []string{"acme-deploy"}},
				Capabilities: []Capability{"terminal.activity.read"},
				Contributions: PluginContributions{
					Hooks: []HookContribution{
						{
							EventType: "termx.daemon.terminal.output_idle",
							Handler:   "on_idle",
							Scope:     HookScope{EndpointID: "remote-a"},
							Delivery:  HookDelivery{Mode: DeliveryStrictQueued},
						},
					},
				},
			},
		},
	}
	_, err := BuildPluginCatalog([]PluginManifest{manifest}, CatalogBuildConfig{
		EventCatalog: []EventSpec{
			{
				Type:            "termx.daemon.terminal.output_idle",
				SourceHost:      HostDaemon,
				DefaultDelivery: HookDelivery{Mode: DeliveryCoalesced, QueueLimit: 8},
				RequiredCaps:    []Capability{"terminal.activity.read"},
				Lossy:           true,
			},
		},
		Grants: []CapabilityGrant{
			{PluginID: "acme.deploy", Host: HostClient, Capabilities: []Capability{"terminal.activity.read"}},
		},
	})
	if err == nil {
		t.Fatalf("hook delivery override should fail")
	}

	manifest.Hosts[0].Contributions.Hooks[0].Delivery = HookDelivery{Mode: DeliveryCoalesced, QueueLimit: 16}
	_, err = BuildPluginCatalog([]PluginManifest{manifest}, CatalogBuildConfig{
		EventCatalog: []EventSpec{
			{
				Type:            "termx.daemon.terminal.output_idle",
				SourceHost:      HostDaemon,
				DefaultDelivery: HookDelivery{Mode: DeliveryCoalesced, QueueLimit: 8},
				RequiredCaps:    []Capability{"terminal.activity.read"},
				Lossy:           true,
			},
		},
		Grants: []CapabilityGrant{
			{PluginID: "acme.deploy", Host: HostClient, Capabilities: []Capability{"terminal.activity.read"}},
		},
	})
	if err == nil {
		t.Fatalf("hook queue limit above event policy should fail")
	}

	manifest.Hosts[0].Contributions.Hooks[0].Delivery = HookDelivery{Mode: DeliveryCoalesced, QueueLimit: 4}
	catalog, err := BuildPluginCatalog([]PluginManifest{manifest}, CatalogBuildConfig{
		EventCatalog: []EventSpec{
			{
				Type:            "termx.daemon.terminal.output_idle",
				SourceHost:      HostDaemon,
				DefaultDelivery: HookDelivery{Mode: DeliveryCoalesced, QueueLimit: 8},
				RequiredCaps:    []Capability{"terminal.activity.read"},
				Lossy:           true,
			},
		},
		Grants: []CapabilityGrant{
			{PluginID: "acme.deploy", Host: HostClient, Capabilities: []Capability{"terminal.activity.read"}},
		},
	})
	if err != nil {
		t.Fatalf("hook queue limit within event policy should pass: %v", err)
	}
	if len(catalog.Subscriptions) != 1 || catalog.Subscriptions[0].Delivery.QueueLimit != 4 {
		t.Fatalf("expected bounded delivery override, got %#v", catalog.Subscriptions)
	}
}

func TestDestructiveActionRequiresExplicitCapability(t *testing.T) {
	manifest := manifestWithAction("acme.deploy", "acme.deploy.dangerous", nil, DangerDestructive)
	if _, err := BuildPluginCatalog([]PluginManifest{manifest}, CatalogBuildConfig{}); err == nil {
		t.Fatalf("destructive action without capability should fail")
	}
}

func TestKeybindingCanResolveActionDeclaredByLaterManifest(t *testing.T) {
	keybindingOnly := PluginManifest{
		ID:   "acme.keys",
		Name: "Keys",
		API:  1,
		Hosts: []PluginHostManifest{
			{
				Placement:    HostClient,
				ClientKinds:  []ClientKind{ClientKindTUI},
				Runner:       RunnerSpec{Type: RunnerStdioJSON, Command: []string{"keys"}},
				Capabilities: []Capability{"client.panel.close"},
				Contributions: PluginContributions{
					Keybindings: []KeybindingContribution{
						{Keys: []string{"ctrl+w"}, ActionID: "acme.actions.panel.close"},
					},
				},
			},
		},
	}
	actionOnly := manifestWithAction("acme.actions", "acme.actions.panel.close", []Capability{"client.panel.close"}, DangerNone)
	catalog, err := BuildPluginCatalog([]PluginManifest{keybindingOnly, actionOnly}, CatalogBuildConfig{
		Grants: []CapabilityGrant{
			{PluginID: "acme.actions", Host: HostClient, Capabilities: []Capability{"client.panel.close"}},
			{PluginID: "acme.keys", Host: HostClient, Capabilities: []Capability{"client.panel.close"}, Trusted: true},
		},
	})
	if err != nil {
		t.Fatalf("build catalog: %v", err)
	}
	if len(catalog.Keybindings) != 1 || catalog.Keybindings[0].ActionID != "acme.actions.panel.close" {
		t.Fatalf("keybinding should resolve after all actions are registered, got %#v", catalog.Keybindings)
	}
}

func TestKeybindingCannotBindOtherPluginActionWithoutTrustedGrant(t *testing.T) {
	keybindingOnly := PluginManifest{
		ID:   "acme.keys",
		Name: "Keys",
		API:  1,
		Hosts: []PluginHostManifest{
			{
				Placement:   HostClient,
				ClientKinds: []ClientKind{ClientKindTUI},
				Runner:      RunnerSpec{Type: RunnerStdioJSON, Command: []string{"keys"}},
				Contributions: PluginContributions{
					Keybindings: []KeybindingContribution{
						{Keys: []string{"ctrl+w"}, ActionID: "acme.actions.panel.close"},
					},
				},
			},
		},
	}
	actionOnly := manifestWithAction("acme.actions", "acme.actions.panel.close", []Capability{"client.panel.close"}, DangerNone)
	catalog, err := BuildPluginCatalog([]PluginManifest{keybindingOnly, actionOnly}, CatalogBuildConfig{
		Grants: []CapabilityGrant{
			{PluginID: "acme.actions", Host: HostClient, Capabilities: []Capability{"client.panel.close"}},
		},
	})
	if err != nil {
		t.Fatalf("untrusted cross-plugin keybinding should be diagnostic, not structural error: %v", err)
	}
	if len(catalog.Keybindings) != 0 || !hasDiagnostic(catalog.Diagnostics, "keybinding", "acme.actions.panel.close", DiagnosticUnsupportedContribution) {
		t.Fatalf("expected unsupported keybinding diagnostic, got keybindings=%#v diagnostics=%#v", catalog.Keybindings, catalog.Diagnostics)
	}
}

func TestClientKindsCannotExceedHostClientKinds(t *testing.T) {
	actionKinds := manifestWithAction("acme.deploy", "acme.deploy.panel.close", []Capability{"client.panel.close"}, DangerNone)
	actionKinds.Hosts[0].Contributions.Actions[0].SupportedClientKinds = []ClientKind{ClientKindWeb}
	if _, err := BuildPluginCatalog([]PluginManifest{actionKinds}, CatalogBuildConfig{}); err == nil {
		t.Fatalf("action client kinds outside host client kinds should fail")
	}

	keyKinds := manifestWithAction("acme.deploy", "acme.deploy.panel.close", []Capability{"client.panel.close"}, DangerNone)
	keyKinds.Hosts[0].Contributions.Keybindings = []KeybindingContribution{
		{Keys: []string{"ctrl+w"}, ActionID: "acme.deploy.panel.close", ClientKinds: []ClientKind{ClientKindWeb}},
	}
	if _, err := BuildPluginCatalog([]PluginManifest{keyKinds}, CatalogBuildConfig{}); err == nil {
		t.Fatalf("keybinding client kinds outside host client kinds should fail")
	}

	invalid := manifestWithAction("acme.deploy", "acme.deploy.panel.close", []Capability{"client.panel.close"}, DangerNone)
	invalid.Hosts[0].ClientKinds = []ClientKind{"terminal"}
	if _, err := BuildPluginCatalog([]PluginManifest{invalid}, CatalogBuildConfig{}); err == nil {
		t.Fatalf("invalid client kind should fail")
	}
}

func TestKeybindingRequiresClientAction(t *testing.T) {
	manifest := PluginManifest{
		ID:   "acme.deploy",
		Name: "Deploy Tools",
		API:  1,
		Hosts: []PluginHostManifest{
			{
				Placement:    HostOneShot,
				Runner:       RunnerSpec{Type: RunnerStdioJSON, Command: []string{"deploy"}},
				Capabilities: []Capability{"terminal.kill"},
				Contributions: PluginContributions{
					Actions: []ActionContribution{
						{
							ID:                 "acme.deploy.terminal.kill",
							Scope:              ActionScopeDaemon,
							DaemonRequiredCaps: []Capability{"terminal.kill"},
							Danger:             DangerDestructive,
						},
					},
				},
			},
			{
				Placement:    HostClient,
				ClientKinds:  []ClientKind{ClientKindTUI},
				Runner:       RunnerSpec{Type: RunnerStdioJSON, Command: []string{"deploy-client"}},
				Capabilities: []Capability{"terminal.kill"},
				Contributions: PluginContributions{
					Keybindings: []KeybindingContribution{
						{Keys: []string{"ctrl+k"}, ActionID: "acme.deploy.terminal.kill"},
					},
				},
			},
		},
	}
	catalog, err := BuildPluginCatalog([]PluginManifest{manifest}, CatalogBuildConfig{
		Grants: []CapabilityGrant{
			{PluginID: "acme.deploy", Host: HostOneShot, Capabilities: []Capability{"terminal.kill"}},
			{PluginID: "acme.deploy", Host: HostClient, Capabilities: []Capability{"terminal.kill"}},
		},
	})
	if err != nil {
		t.Fatalf("build catalog: %v", err)
	}
	if len(catalog.Keybindings) != 0 || !hasDiagnostic(catalog.Diagnostics, "keybinding", "acme.deploy.terminal.kill", DiagnosticUnsupportedContribution) {
		t.Fatalf("daemon action keybinding should be rejected by catalog, got keybindings=%#v diagnostics=%#v", catalog.Keybindings, catalog.Diagnostics)
	}
}

func TestOneShotHostCannotContributeHooksOrKeybindings(t *testing.T) {
	manifest := PluginManifest{
		ID:   "acme.deploy",
		Name: "Deploy Tools",
		API:  1,
		Hosts: []PluginHostManifest{
			{
				Placement: HostOneShot,
				Runner:    RunnerSpec{Type: RunnerStdioJSON, Command: []string{"deploy"}},
				Contributions: PluginContributions{
					Hooks: []HookContribution{{EventType: "termx.daemon.terminal.output_idle", Handler: "on_idle"}},
				},
			},
		},
	}
	if _, err := BuildPluginCatalog([]PluginManifest{manifest}, CatalogBuildConfig{}); err == nil {
		t.Fatalf("one-shot host hook contribution should fail")
	}

	manifest.Hosts[0].Contributions.Hooks = nil
	manifest.Hosts[0].Contributions.Keybindings = []KeybindingContribution{
		{Keys: []string{"ctrl+x"}, ActionID: "acme.deploy.action"},
	}
	if _, err := BuildPluginCatalog([]PluginManifest{manifest}, CatalogBuildConfig{}); err == nil {
		t.Fatalf("one-shot host keybinding contribution should fail")
	}
}

func TestThirdPartyCannotSelfDeclareBuiltinRunner(t *testing.T) {
	manifest := manifestWithAction("acme.deploy", "acme.deploy.panel.close", []Capability{"client.panel.close"}, DangerNone)
	manifest.Hosts[0].Runner = RunnerSpec{Type: RunnerBuiltin}
	if _, err := BuildPluginCatalog([]PluginManifest{manifest}, CatalogBuildConfig{}); err == nil {
		t.Fatalf("third-party builtin runner should fail")
	}
}

func manifestWithAction(pluginID PluginID, actionID ActionID, caps []Capability, danger DangerLevel) PluginManifest {
	return PluginManifest{
		ID:   pluginID,
		Name: "Test Plugin",
		API:  1,
		Hosts: []PluginHostManifest{
			{
				Placement:    HostClient,
				ClientKinds:  []ClientKind{ClientKindTUI},
				Runner:       RunnerSpec{Type: RunnerStdioJSON, Command: []string{"test-plugin"}},
				Capabilities: caps,
				Contributions: PluginContributions{
					Actions: []ActionContribution{
						{
							ID:                 actionID,
							Scope:              ActionScopeClient,
							ClientRequiredCaps: caps,
							Danger:             danger,
						},
					},
				},
			},
		},
	}
}

func containsCap(caps []Capability, cap Capability) bool {
	for _, candidate := range caps {
		if candidate == cap {
			return true
		}
	}
	return false
}

func hasDiagnostic(diagnostics []CatalogDiagnostic, kind, id string, reason CatalogDiagnosticReason) bool {
	for _, diag := range diagnostics {
		if diag.Kind == kind && diag.ID == id && diag.Reason == reason {
			return true
		}
	}
	return false
}
