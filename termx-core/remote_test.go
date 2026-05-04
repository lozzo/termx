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

func TestE2ERemotePairStart(t *testing.T) {
	_, client, cleanup := newE2EClient(t, WithRemoteConfig(RemoteConfig{
		Enabled:    true,
		DataDir:    t.TempDir(),
		DeviceName: "pair-machine",
	}))
	defer cleanup()

	result, err := client.RemotePairStart(context.Background(), protocol.PairStartParams{
		LocalPairURL: "http://127.0.0.1:18888/api/local/pair",
		TTLSeconds:   300,
	})
	if err != nil {
		t.Fatalf("RemotePairStart returned error: %v", err)
	}
	if result.Type != "termx_pair_v1" {
		t.Fatalf("expected pair type termx_pair_v1, got %q", result.Type)
	}
	if result.MachineID == "" {
		t.Fatal("expected machine id to be set")
	}
	if result.MachineName != "pair-machine" {
		t.Fatalf("expected machine name pair-machine, got %q", result.MachineName)
	}
	if !strings.HasPrefix(result.MachinePublicKeyFingerprint, "sha256:") {
		t.Fatalf("expected machine public key fingerprint, got %q", result.MachinePublicKeyFingerprint)
	}
	if result.LocalPairURL != "http://127.0.0.1:18888/api/local/pair" {
		t.Fatalf("unexpected local pair url %q", result.LocalPairURL)
	}
	if !strings.HasPrefix(result.PairSessionID, "pair_") {
		t.Fatalf("expected pair session id prefix, got %q", result.PairSessionID)
	}
	if result.PairSecret == "" {
		t.Fatal("expected pair secret to be set")
	}
	if result.ExpiresAt.IsZero() {
		t.Fatal("expected expiry to be set")
	}
}

func TestE2ERemotePairStartUsesLatestLocalPairURL(t *testing.T) {
	_, client, cleanup := newE2EClient(t, WithRemoteConfig(RemoteConfig{
		Enabled:    true,
		DataDir:    t.TempDir(),
		DeviceName: "pair-machine",
	}))
	defer cleanup()

	if _, err := client.RemotePairStart(context.Background(), protocol.PairStartParams{
		LocalPairURL: "http://127.0.0.1:18888/api/local/pair",
		TTLSeconds:   300,
	}); err != nil {
		t.Fatalf("first RemotePairStart returned error: %v", err)
	}

	result, err := client.RemotePairStart(context.Background(), protocol.PairStartParams{
		LocalPairURL: "http://192.168.1.23:18888/api/local/pair",
		TTLSeconds:   300,
	})
	if err != nil {
		t.Fatalf("second RemotePairStart returned error: %v", err)
	}
	if result.LocalPairURL != "http://192.168.1.23:18888/api/local/pair" {
		t.Fatalf("expected latest local pair url, got %q", result.LocalPairURL)
	}
}

var _ = protocol.Version

func TestRemoteTriggerSyncRegistersMachineWithoutTerminalInventory(t *testing.T) {
	type registerPayload struct {
		DeviceID  string `json:"deviceId"`
		Terminals []struct {
			ID string `json:"id"`
		} `json:"terminals,omitempty"`
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

	_, err := srv.Create(context.Background(), CreateOptions{
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
			if req.DeviceID == "" {
				t.Fatalf("remote sync registration omitted device id: %+v", req)
			}
			if len(req.Terminals) != 0 {
				t.Fatalf("remote sync leaked terminal inventory to control: %+v", req.Terminals)
			}
			return
		case <-deadline:
			t.Fatal("timed out waiting for remote sync registration")
		}
	}
}

func containsOperationNotPermitted(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "operation not permitted")
}
