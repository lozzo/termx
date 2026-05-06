package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	remoteconfig "github.com/lozzow/termx/termx-remote/config"
)

func TestManagerControlRegistrationOmitsDeprecatedKeyMaterial(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	var got map[string]any
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/devices/register":
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatalf("decode control registration: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"device": map[string]any{"id": got["deviceId"]}})
		case "/api/v1/hubs":
			_ = json.NewEncoder(w).Encode(map[string]any{"hubs": []any{}})
		default:
			t.Fatalf("unexpected control path %s", r.URL.Path)
		}
	}))
	defer control.Close()

	manager := NewManager(remoteconfig.Config{
		Enabled:     true,
		DataDir:     dataDir,
		DeviceName:  "keyed-agent",
		ControlURL:  control.URL,
		AccessToken: "control-token",
	}, inventoryProviderStub{
		items: []TerminalInventoryItem{{ID: "term-1", State: "running"}},
	}, nil)

	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	status := manager.Status()
	if status.State != StateConfigured {
		t.Fatalf("expected configured state, got %q detail=%q", status.State, status.Detail)
	}
	if got["deviceId"] == "" || got["displayName"] != "keyed-agent" {
		t.Fatalf("control registration missing device identity fields: %#v", got)
	}
	if _, ok := got["machinePublicKey"]; ok {
		t.Fatalf("control registration must not include machinePublicKey: %#v", got)
	}
	if _, ok := got["terminals"]; ok {
		t.Fatalf("control registration leaked terminal inventory: %#v", got)
	}
	if _, ok := got["machinePrivateKey"]; ok {
		t.Fatalf("control registration leaked machinePrivateKey: %#v", got)
	}
}
