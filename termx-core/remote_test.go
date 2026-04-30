package termx

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-core/protocol"
)

func TestE2ERemoteStatus(t *testing.T) {
	_, client, cleanup := newE2EClient(t, WithRemoteConfig(RemoteConfig{
		Enabled:    true,
		DataDir:    t.TempDir(),
		DeviceName: "e2e-remote",
	}))
	defer cleanup()

	status, err := client.RemoteStatus(context.Background())
	if err != nil {
		t.Fatalf("RemoteStatus returned error: %v", err)
	}
	if status.State != string(RemoteStateConfigured) {
		t.Fatalf("expected configured state, got %q", status.State)
	}
	if status.DeviceName != "e2e-remote" {
		t.Fatalf("expected device name e2e-remote, got %q", status.DeviceName)
	}
	if status.DeviceID == "" {
		t.Fatal("expected device id to be set")
	}
}

var _ = protocol.Version

func TestRemoteTriggerSyncRegistersCreatedTerminal(t *testing.T) {
	type registerPayload struct {
		Terminals []struct {
			ID string `json:"id"`
		} `json:"terminals"`
	}

	requests := make(chan registerPayload, 8)
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/devices/register" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("expected bearer token, got %q", got)
		}
		var payload registerPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode register payload: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		select {
		case requests <- payload:
		default:
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"device":{"id":"device-1"}}`))
	}))
	defer control.Close()

	srv := NewServer(WithRemoteConfig(RemoteConfig{
		Enabled:     true,
		ControlURL:  control.URL,
		AccessToken: "secret",
		DataDir:     t.TempDir(),
		DeviceName:  "sync-test",
	}))

	created, err := srv.Create(context.Background(), CreateOptions{
		Command: []string{"bash", "--noprofile", "--norc"},
		Name:    "sync-test-terminal",
		Size:    Size{Cols: 80, Rows: 24},
	})
	if err != nil {
		if containsOperationNotPermitted(err) {
			t.Skipf("pty not permitted: %v", err)
		}
		t.Fatalf("Create returned error: %v", err)
	}

	deadline := time.After(5 * time.Second)
	for {
		select {
		case req := <-requests:
			for _, terminal := range req.Terminals {
				if terminal.ID == created.ID {
					return
				}
			}
		case <-deadline:
			t.Fatalf("timed out waiting for remote sync containing terminal %q", created.ID)
		}
	}
}

func containsOperationNotPermitted(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "operation not permitted")
}
