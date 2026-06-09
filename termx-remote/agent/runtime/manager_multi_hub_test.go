package runtime

import (
	"context"
	"testing"
	"time"

	remoteconfig "github.com/lozzow/termx/termx-remote/config"
)

func TestManagerStartsGRPCLoopsForTwoHubs(t *testing.T) {
	dataDir := t.TempDir()
	hubURLs := []string{"http://127.0.0.1:1", "http://127.0.0.1:2"}
	manager := NewManager(remoteconfig.Config{
		Enabled:    true,
		DataDir:    dataDir,
		DeviceName: "multi-hub-agent",
		HubURLs:    hubURLs,
		Mode:       "local",
	}, inventoryProviderStub{}, nil)
	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	defer manager.Close()

	for _, hubURL := range hubURLs {
		waitForHubLoop(t, manager, hubURL)
	}
	first := manager.buildGRPCRegisterRequest()
	second := manager.buildGRPCRegisterRequest()
	if first.GetAgentId() == "" || first.GetAgentId() != second.GetAgentId() {
		t.Fatalf("expected shared agent id for multi-hub registration, first=%q second=%q", first.GetAgentId(), second.GetAgentId())
	}
}

func waitForHubLoop(t *testing.T, manager *Manager, hubURL string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		manager.mu.RLock()
		state := manager.hubStates[hubURL]
		ready := state != nil && state.SessionCancel != nil
		manager.mu.RUnlock()
		if ready {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("missing gRPC signaling loop for %s", hubURL)
}
