package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	remoteprotocol "github.com/lozzow/termx/termx-remote/protocol"
)

func TestLocalEnableStartsHub(t *testing.T) {
	service := NewService(remoteprotocol.Config{}, nil)
	t.Cleanup(func() { _, _ = service.LocalDisable(context.Background()) })

	status, err := service.LocalEnable(context.Background(), remoteprotocol.LocalEnableParams{
		LocalWebAddr: "127.0.0.1:0",
	})
	if err != nil {
		t.Fatalf("LocalEnable returned error: %v", err)
	}
	if !status.Enabled || status.HTTPURL == "" || status.ICETCPPort == 0 {
		t.Fatalf("unexpected local status: %+v", status)
	}

	resp, body, err := localHubRequest(context.Background(), http.MethodGet, status.HTTPURL+"/api/health", nil)
	if err != nil {
		t.Fatalf("GET /api/health: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/health status = %d, body = %s", resp.StatusCode, string(body))
	}
}

func TestLocalHubAcceptsAgentRegistration(t *testing.T) {
	service := NewService(remoteprotocol.Config{}, nil)
	t.Cleanup(func() { _, _ = service.LocalDisable(context.Background()) })

	status, err := service.LocalEnable(context.Background(), remoteprotocol.LocalEnableParams{
		LocalWebAddr: "127.0.0.1:0",
	})
	if err != nil {
		t.Fatalf("LocalEnable returned error: %v", err)
	}

	payload := map[string]any{
		"version":         "remote.hub.v1",
		"device_id":       "machine-local-test",
		"agent_id":        "agent-local-test",
		"display_name":    "Local Test Agent",
		"runtime_version": "test",
		"terminals": []map[string]any{
			{"id": "terminal-1", "state": "running"},
		},
	}
	resp, body, err := localHubRequest(context.Background(), http.MethodPost, status.HTTPURL+"/api/v1/agents/register", payload)
	if err != nil {
		t.Fatalf("POST /api/v1/agents/register: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("register status = %d, body = %s", resp.StatusCode, string(body))
	}
	var decoded struct {
		AgentSessionID string `json:"agent_session_id"`
		RelayPolicy    struct {
			AllowRelay bool `json:"allow_relay"`
		} `json:"relay_policy"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode register response: %v", err)
	}
	if decoded.AgentSessionID == "" {
		t.Fatalf("register response missing agent_session_id: %s", string(body))
	}
	if decoded.RelayPolicy.AllowRelay {
		t.Fatalf("local hub must not enable relay by default: %s", string(body))
	}
}

func TestLocalEnableRegistersRealAgentWithHub(t *testing.T) {
	service := NewService(remoteprotocol.Config{
		Enabled:    true,
		DataDir:    t.TempDir(),
		DeviceName: "local-agent-test",
	}, nil)
	t.Cleanup(func() { _ = service.Close(context.Background()) })
	if err := service.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	status, err := service.LocalEnable(context.Background(), remoteprotocol.LocalEnableParams{
		LocalWebAddr: "127.0.0.1:0",
	})
	if err != nil {
		t.Fatalf("LocalEnable returned error: %v", err)
	}
	managerStatus := waitForLocalManagerOnline(t, service)

	session, err := service.PairStart(remoteprotocol.PairStartParams{
		LocalPairURL: status.LocalPairURL,
		TTLSeconds:   int(time.Minute.Seconds()),
	})
	if err != nil {
		t.Fatalf("PairStart returned error: %v", err)
	}
	claim := map[string]any{
		"machine_id":             session.MachineID,
		"pair_session_id":        session.PairSessionID,
		"pair_secret":            session.PairSecret,
		"app_device_id":          "app-local-agent-test",
		"app_name":               "Local Agent Test",
		"requested_capabilities": []string{"terminal"},
	}
	resp, body, err := localHubRequest(context.Background(), http.MethodPost, status.HTTPURL+"/api/v1/pairing/claims", claim)
	if err != nil {
		t.Fatalf("POST /api/v1/pairing/claims: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("pairing claim status = %d, body = %s; manager status = %+v", resp.StatusCode, string(body), managerStatus)
	}
	var pairResp struct {
		MachineID    string `json:"machine_id"`
		SessionToken string `json:"session_token"`
	}
	if err := json.Unmarshal(body, &pairResp); err != nil {
		t.Fatalf("decode pairing response: %v", err)
	}
	if pairResp.MachineID != session.MachineID || pairResp.SessionToken == "" {
		t.Fatalf("pairing response did not come from real agent: %s", string(body))
	}
}

func TestLocalDisableStopsHub(t *testing.T) {
	service := NewService(remoteprotocol.Config{}, nil)

	status, err := service.LocalEnable(context.Background(), remoteprotocol.LocalEnableParams{
		LocalWebAddr: "127.0.0.1:0",
	})
	if err != nil {
		t.Fatalf("LocalEnable returned error: %v", err)
	}
	if _, err := service.LocalDisable(context.Background()); err != nil {
		t.Fatalf("LocalDisable returned error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	resp, body, err := localHubRequest(ctx, http.MethodGet, status.HTTPURL+"/api/health", nil)
	if err == nil {
		resp.Body.Close()
		t.Fatalf("expected disabled local hub to reject requests, got status %d body %s", resp.StatusCode, string(body))
	}
}

func TestLocalDisableDetachesManagerFromHub(t *testing.T) {
	service := NewService(remoteprotocol.Config{
		Enabled:    true,
		DataDir:    t.TempDir(),
		DeviceName: "local-disable-agent-test",
	}, nil)
	t.Cleanup(func() { _ = service.Close(context.Background()) })
	if err := service.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	localStatus, err := service.LocalEnable(context.Background(), remoteprotocol.LocalEnableParams{
		LocalWebAddr: "127.0.0.1:0",
	})
	if err != nil {
		t.Fatalf("LocalEnable returned error: %v", err)
	}
	waitForLocalManagerOnline(t, service)
	if _, err := service.LocalDisable(context.Background()); err != nil {
		t.Fatalf("LocalDisable returned error: %v", err)
	}

	waitForLocalStatus(t, service, time.Second, func(status remoteprotocol.Status) bool {
		return status.State == remoteprotocol.StateConfigured && status.HubURL == ""
	})
	status := service.Status()
	if status.HubURL != "" {
		t.Fatalf("manager still points at disabled local hub %q after disabling %q", status.HubURL, localStatus.HTTPURL)
	}
}

func waitForLocalManagerOnline(t *testing.T, service *Service) remoteprotocol.Status {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var status remoteprotocol.Status
	for time.Now().Before(deadline) {
		status = service.Status()
		if status.State == remoteprotocol.StateOnline && status.HubURL != "" {
			return status
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("manager did not register with local hub; last status = %+v", status)
	return status
}

func waitForLocalStatus(t *testing.T, service *Service, timeout time.Duration, condition func(remoteprotocol.Status) bool) remoteprotocol.Status {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var status remoteprotocol.Status
	for time.Now().Before(deadline) {
		status = service.Status()
		if condition(status) {
			return status
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("condition not met before timeout; last status = %+v", status)
	return status
}

func TestLocalEnableIdempotent(t *testing.T) {
	service := NewService(remoteprotocol.Config{}, nil)
	t.Cleanup(func() { _, _ = service.LocalDisable(context.Background()) })

	first, err := service.LocalEnable(context.Background(), remoteprotocol.LocalEnableParams{
		LocalWebAddr: "127.0.0.1:0",
	})
	if err != nil {
		t.Fatalf("first LocalEnable returned error: %v", err)
	}
	second, err := service.LocalEnable(context.Background(), remoteprotocol.LocalEnableParams{
		LocalWebAddr: "127.0.0.1:0",
	})
	if err != nil {
		t.Fatalf("second LocalEnable returned error: %v", err)
	}
	if first.HTTPURL != second.HTTPURL || first.ICETCPPort != second.ICETCPPort {
		t.Fatalf("LocalEnable not idempotent: first=%+v second=%+v", first, second)
	}
}

func localHubRequest(ctx context.Context, method string, url string, payload any) (*http.Response, []byte, error) {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, nil, err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, nil, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := http.Client{Timeout: time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		resp.Body.Close()
		return nil, nil, err
	}
	resp.Body = io.NopCloser(strings.NewReader(string(raw)))
	return resp, raw, nil
}
