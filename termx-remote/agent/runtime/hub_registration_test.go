package runtime

import (
	"testing"

	remoteconfig "github.com/lozzow/termx/termx-remote/config"
)

func TestManagerGRPCRegistrationIncludesRuntimeIdentity(t *testing.T) {
	t.Parallel()

	manager := NewManager(remoteconfig.Config{
		Enabled:    true,
		DataDir:    t.TempDir(),
		DeviceName: "signed-agent",
	}, inventoryProviderStub{
		items: []TerminalInventoryItem{{ID: "term-1", State: "running"}},
	}, nil)
	if err := manager.Start(t.Context()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	got := manager.buildGRPCRegisterRequest()
	if got.GetDeviceId() == "" || got.GetAgentId() == "" || got.GetMachineId() == "" {
		t.Fatalf("registration missing identifiers: %+v", got)
	}
	if got.GetDisplayName() != "signed-agent" || got.GetVersion() != "termx-dev" {
		t.Fatalf("registration identity = %+v", got)
	}
	if len(got.GetTerminals()) != 1 || got.GetTerminals()[0].GetTerminalId() != "term-1" {
		t.Fatalf("registration terminals = %+v", got.GetTerminals())
	}
}
