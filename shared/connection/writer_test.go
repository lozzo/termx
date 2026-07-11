package connection

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveRoundTripsManagedEndpointWithoutHubAssignment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "termx", "connections.yaml")
	registry := DefaultRegistry()
	registry.Connections["studio"] = Config{
		ID: "studio", Label: "Studio", Transport: TransportHubP2P, ConnectMode: ConnectOnDemand, Enabled: true,
		HubDeviceID: "device-studio", DeviceFingerprint: "ed25519-sha256:studio", GrantRef: "grant-studio", RelayMode: RelayDirect,
	}
	if err := Save(path, registry); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "hub_url") || strings.Contains(string(payload), "termx-grant-v1") {
		t.Fatalf("registry leaked service assignment or raw grant: %s", payload)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Connections["studio"].DialIdentity() != registry.Connections["studio"].DialIdentity() {
		t.Fatalf("managed dial identity changed: got=%#v want=%#v", loaded.Connections["studio"], registry.Connections["studio"])
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("registry mode = %v, err=%v", info.Mode().Perm(), err)
	}
}
