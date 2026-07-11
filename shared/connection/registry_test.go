package connection

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingDefaultPathReturnsLocalConnection(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)

	registry, err := Load("")
	if err != nil {
		t.Fatalf("missing default registry should not fail: %v", err)
	}
	local, ok := registry.DefaultConnection()
	if !ok || local.ID != DefaultEndpointID || local.Transport != TransportLocal || local.ConnectMode != ConnectAuto || !local.Enabled || local.Socket != "auto" {
		t.Fatalf("missing default registry should return local connection, registry=%#v local=%#v ok=%v", registry, local, ok)
	}
}

func TestLoadExplicitMissingPathFails(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if err == nil {
		t.Fatalf("explicit missing registry path should fail")
	}
}

func TestParseConnectionsYAML(t *testing.T) {
	registry, err := Parse([]byte(`
version: 1
default: local

connections:
  local:
    label: "This Mac"
    enabled: true
    transport: local
    connect_mode: auto
    socket: auto

  cn-fast:
    label: "CN Fast"
    enabled: true
    transport: ssh
    connect_mode: on_demand
    address: "root@114.66.58.243"
    auth_ref: "ssh:cn-fast"
    remote_socket: auto

  us-west-slow:
    label: "US West Slow"
    enabled: false
    transport: ssh
    connect_mode: manual
    address: "root@155.94.155.192"
    remote_socket: "/run/user/1000/termx-v2-wire1.sock"

  mac-studio:
    label: "Studio"
    enabled: true
    transport: hub-p2p
    connect_mode: manual
    hub_device_id: "device_ed25519:studio"
    device_fingerprint: "SHA256:studio"
    grant_ref: "grant:studio"
    relay_mode: relay_only
`))
	if err != nil {
		t.Fatalf("parse registry: %v", err)
	}
	if registry.Default != DefaultEndpointID {
		t.Fatalf("expected default local, got %#v", registry.Default)
	}
	items := registry.List()
	if len(items) != 4 || items[0].ID != "cn-fast" || items[1].ID != DefaultEndpointID || items[2].ID != "mac-studio" || items[3].ID != "us-west-slow" {
		t.Fatalf("registry list must be sorted by endpoint id, got %#v", items)
	}
	fast := registry.Connections["cn-fast"]
	if fast.Label != "CN Fast" || fast.Transport != TransportSSH || fast.ConnectMode != ConnectOnDemand || fast.Address != "root@114.66.58.243" || fast.AuthRef != "ssh:cn-fast" || fast.RemoteSocket != "auto" {
		t.Fatalf("unexpected cn-fast config %#v", fast)
	}
	slow := registry.Connections["us-west-slow"]
	if slow.Enabled || slow.ConnectMode != ConnectManual || slow.RemoteSocket != "/run/user/1000/termx-v2-wire1.sock" {
		t.Fatalf("unexpected us-west-slow config %#v", slow)
	}
	studio := registry.Connections["mac-studio"]
	if studio.Label != "Studio" || studio.Transport != TransportHubP2P || studio.ConnectMode != ConnectManual || studio.HubDeviceID != "device_ed25519:studio" || studio.DeviceFingerprint != "SHA256:studio" || studio.GrantRef != "grant:studio" || studio.RelayMode != RelayOnly {
		t.Fatalf("unexpected mac-studio hub config %#v", studio)
	}
}

func TestParseRejectsUnknownFieldsAndInvalidValues(t *testing.T) {
	cases := []struct {
		name string
		data string
	}{
		{name: "unknown top field", data: "name: bad\n"},
		{name: "unknown connection field", data: "connections:\n  local:\n    label: local\n    transport: local\n    enabled: true\n    surprise: nope\n"},
		{name: "bad indentation", data: "connections:\n local:\n"},
		{name: "list", data: "connections:\n  - local\n"},
		{name: "bad bool", data: "connections:\n  local:\n    enabled: yes\n"},
		{name: "ssh missing address", data: "connections:\n  lab:\n    enabled: true\n    transport: ssh\n"},
		{name: "bad id", data: "connections:\n  bad/id:\n    transport: local\n"},
		{name: "managed rejects caller hub url", data: "connections:\n  peer:\n    enabled: true\n    transport: hub-p2p\n    hub_url: https://hub.example.com\n    hub_device_id: device_ed25519:peer\n    device_fingerprint: SHA256:peer\n    grant_ref: grant:peer\n"},
		{name: "hub missing device", data: "connections:\n  peer:\n    enabled: true\n    transport: hub-p2p\n    device_fingerprint: SHA256:peer\n    grant_ref: grant:peer\n"},
		{name: "hub missing fingerprint", data: "connections:\n  peer:\n    enabled: true\n    transport: hub-p2p\n    hub_device_id: device_ed25519:peer\n    grant_ref: grant:peer\n"},
		{name: "hub missing grant", data: "connections:\n  peer:\n    enabled: true\n    transport: hub-p2p\n    hub_device_id: device_ed25519:peer\n    device_fingerprint: SHA256:peer\n"},
		{name: "hub must not set address", data: "connections:\n  peer:\n    enabled: true\n    transport: hub-p2p\n    address: peer.example.com\n    hub_device_id: device_ed25519:peer\n    device_fingerprint: SHA256:peer\n    grant_ref: grant:peer\n"},
		{name: "hub bad relay mode", data: "connections:\n  peer:\n    enabled: true\n    transport: hub-p2p\n    hub_device_id: device_ed25519:peer\n    device_fingerprint: SHA256:peer\n    grant_ref: grant:peer\n    relay_mode: open\n"},
		{name: "disabled default", data: "default: lab\nconnections:\n  lab:\n    enabled: false\n    transport: ssh\n    address: lab.example.com\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Parse([]byte(tc.data)); err == nil {
				t.Fatalf("expected parse failure")
			}
		})
	}
}

func TestHubP2PDefaultsAndIdentityClassification(t *testing.T) {
	registry, err := Parse([]byte(`
connections:
  studio:
    label: "Studio Mac"
    transport: hub-p2p
    hub_device_id: "device_ed25519:studio"
    device_fingerprint: "SHA256:studio"
    grant_ref: "grant:studio"
`))
	if err != nil {
		t.Fatalf("parse hub registry: %v", err)
	}
	studio := registry.Connections["studio"]
	if studio.ConnectMode != ConnectOnDemand || studio.RelayMode != RelayAuto || studio.DeviceFingerprint != "SHA256:studio" || studio.GrantRef != "grant:studio" {
		t.Fatalf("expected hub defaults, got %#v", studio)
	}
	renamed := studio
	renamed.Label = "Desk"
	if !studio.DisplayChanged(renamed) || studio.RequiresReconnect(renamed) {
		t.Fatalf("hub label change should be display-only")
	}
	movedIdentity := studio
	movedIdentity.DeviceFingerprint = "SHA256:other"
	if !studio.RequiresReconnect(movedIdentity) {
		t.Fatalf("hub device fingerprint change must require reconnect")
	}
	grantChanged := studio
	grantChanged.GrantRef = "grant:other"
	if !studio.RequiresReconnect(grantChanged) {
		t.Fatalf("hub grant ref change must require reconnect")
	}
	relayChanged := studio
	relayChanged.RelayMode = RelayDirect
	if !studio.RequiresReconnect(relayChanged) {
		t.Fatalf("relay policy change must require reconnect")
	}
}

func TestParseHubP2PSmartRouteMode(t *testing.T) {
	registry, err := Parse([]byte(`
connections:
  studio:
    transport: hub-p2p
    hub_device_id: "device_ed25519:studio"
    device_fingerprint: "SHA256:studio"
    grant_ref: "grant:studio"
    relay_mode: smart_route
`))
	if err != nil {
		t.Fatalf("parse smart route registry: %v", err)
	}
	if got := registry.Connections["studio"].RelayMode; got != RelaySmart {
		t.Fatalf("smart route relay mode = %q", got)
	}
}

func TestRegistryDefaultsAndValidation(t *testing.T) {
	registry, err := Parse([]byte(`
version: 1
connections:
  lab:
    label: ""
    transport: ssh
    address: "lab.example.com"
`))
	if err != nil {
		t.Fatalf("parse registry with defaults: %v", err)
	}
	if registry.Default != "lab" {
		t.Fatalf("single enabled endpoint should become default, got %q", registry.Default)
	}
	lab := registry.Connections["lab"]
	if lab.Label != "lab" || !lab.Enabled || lab.ConnectMode != ConnectOnDemand || lab.RemoteSocket != "auto" {
		t.Fatalf("expected non-local defaults, got %#v", lab)
	}
}

func TestConfigRuntimeChangeClassification(t *testing.T) {
	old := Config{
		ID:           "lab",
		Label:        "Lab",
		Transport:    TransportSSH,
		Address:      "lab.example.com",
		AuthRef:      "ssh:lab",
		ConnectMode:  ConnectOnDemand,
		Enabled:      true,
		RemoteSocket: "auto",
	}
	renamed := old
	renamed.Label = "Lab Renamed"
	if !old.DisplayChanged(renamed) || old.RequiresReconnect(renamed) {
		t.Fatalf("label change should be display-only")
	}
	disabled := old
	disabled.Enabled = false
	if old.RequiresReconnect(disabled) {
		t.Fatalf("enabled change must not hot-switch an active session")
	}
	manual := old
	manual.ConnectMode = ConnectManual
	if old.RequiresReconnect(manual) {
		t.Fatalf("connect mode change must only affect future connects")
	}
	moved := old
	moved.Address = "new.example.com"
	if !old.RequiresReconnect(moved) {
		t.Fatalf("address change must require reconnect")
	}
	authChanged := old
	authChanged.AuthRef = "ssh:other"
	if !old.RequiresReconnect(authChanged) {
		t.Fatalf("auth change must require reconnect")
	}
}

func TestLoadReadsDefaultPath(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	path := filepath.Join(configHome, "termx", DefaultFileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	if err := os.WriteFile(path, []byte(`
connections:
  local:
    label: "Configured Local"
    enabled: true
    transport: local
`), 0o644); err != nil {
		t.Fatalf("write registry: %v", err)
	}
	registry, err := Load("")
	if err != nil {
		t.Fatalf("load default path: %v", err)
	}
	local := registry.Connections[DefaultEndpointID]
	if local.Label != "Configured Local" || local.ConnectMode != ConnectAuto || local.Socket != "auto" {
		t.Fatalf("unexpected loaded local config %#v", local)
	}
}
