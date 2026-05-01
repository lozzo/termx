package termx

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestE2ERemoteLocalWebHandlerStatusTerminalsAndPair(t *testing.T) {
	srv := NewServer(WithRemoteConfig(RemoteConfig{
		Enabled:    true,
		DataDir:    t.TempDir(),
		DeviceName: "local-web-machine",
	}))
	defer srv.Shutdown(context.Background())

	term, err := srv.Create(context.Background(), CreateOptions{
		Command: []string{"sh", "-c", "sleep 5"},
		Name:    "local-web-shell",
		Size:    Size{Cols: 100, Rows: 30},
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted in this environment: %v", err)
		}
		t.Fatalf("Create returned error: %v", err)
	}

	handler := srv.LocalWebHandler(LocalWebOptions{
		HTTPURL:       "http://127.0.0.1:7342",
		LocalPairURL:  "http://127.0.0.1:7342/api/local/pair",
		ICETCPEnabled: true,
		ICETCPPort:    7342,
		Assets: NewLocalWebStaticAssets(map[string]string{
			"index.html": "<!doctype html><title>TermX Local</title>",
		}),
	})

	status := localWebJSON(t, handler, http.MethodGet, "/api/local/status", nil)
	if status["machine_id"] == "" {
		t.Fatalf("expected machine_id in local status: %#v", status)
	}
	if status["device_id"] != nil {
		t.Fatalf("local status must not expose device_id: %#v", status)
	}
	if raw, _ := json.Marshal(status); strings.Contains(strings.ToLower(string(raw)), "turn:") {
		t.Fatalf("local status must not include TURN credentials: %s", raw)
	}

	terminals := localWebJSON(t, handler, http.MethodGet, "/api/local/terminals", nil)
	terminalList, ok := terminals["terminals"].([]any)
	if !ok || len(terminalList) != 1 {
		t.Fatalf("expected one local terminal, got %#v", terminals)
	}
	first, ok := terminalList[0].(map[string]any)
	if !ok {
		t.Fatalf("expected terminal object, got %#v", terminalList[0])
	}
	if first["terminal_id"] != term.ID {
		t.Fatalf("expected terminal_id %q, got %#v", term.ID, first)
	}
	if first["pane_id"] != nil || first["workspace_id"] != nil || first["tab_id"] != nil {
		t.Fatalf("local terminal response must not expose workspace/tab/pane: %#v", first)
	}

	session, err := srv.RemotePairStart(PairStartOptions{
		LocalPairURL: "http://127.0.0.1:7342/api/local/pair",
		TTL:          time.Minute,
	})
	if err != nil {
		t.Fatalf("RemotePairStart returned error: %v", err)
	}
	appPublic, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey returned error: %v", err)
	}
	pairBody := []byte(`{
		"pair_session_id":"` + session.PairSessionID + `",
		"pair_secret":"` + session.PairSecret + `",
		"app_device_id":"app-local-web",
		"app_name":"Local Web",
		"app_public_key":"` + base64.StdEncoding.EncodeToString(appPublic) + `",
		"requested_capabilities":["terminal","file_manager"]
	}`)
	pair := localWebJSON(t, handler, http.MethodPost, "/api/local/pair", pairBody)
	if pair["machine_id"] != session.MachineID {
		t.Fatalf("expected pair machine_id %q, got %#v", session.MachineID, pair)
	}
	if pair["machine_public_key_fingerprint"] != session.MachinePublicKeyFingerprint {
		t.Fatalf("expected pair machine fingerprint %q, got %#v", session.MachinePublicKeyFingerprint, pair)
	}
	if pair["expires_at"] == "" {
		t.Fatalf("expected pair expiration in response: %#v", pair)
	}
	if pair["machine_private_key"] != nil {
		t.Fatalf("local pair must not expose machine private key: %#v", pair)
	}
	if pair["app_certificate"] == nil {
		t.Fatalf("expected app_certificate in pair response: %#v", pair)
	}
}

func localWebJSON(t *testing.T, handler http.Handler, method, path string, body []byte) map[string]any {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s %s expected 200, got %d body=%q", method, path, rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %s %s response: %v\n%s", method, path, err, rec.Body.String())
	}
	return out
}
