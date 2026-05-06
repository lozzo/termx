package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-remote/discovery"
	remoteprotocol "github.com/lozzow/termx/termx-remote/protocol"
	pb "github.com/lozzow/termx/termx-remote/protocol/hubgrpc"
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

func TestLocalHubAcceptsGRPCAgentRegistration(t *testing.T) {
	service := NewService(remoteprotocol.Config{}, nil)
	t.Cleanup(func() { _, _ = service.LocalDisable(context.Background()) })

	status, err := service.LocalEnable(context.Background(), remoteprotocol.LocalEnableParams{
		LocalWebAddr: "127.0.0.1:0",
	})
	if err != nil {
		t.Fatalf("LocalEnable returned error: %v", err)
	}

	client, err := discovery.NewGRPCHubClient(status.HTTPURL, "local")
	if err != nil {
		t.Fatalf("new grpc hub client: %v", err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	stream, err := client.Connect(ctx)
	if err != nil {
		t.Fatalf("connect grpc hub: %v", err)
	}
	err = stream.Send(&pb.AgentToHub{Payload: &pb.AgentToHub_Register{
		Register: &pb.RegisterRequest{
			DeviceId:    "machine-local-test",
			MachineId:   "machine-local-test",
			AgentId:     "agent-local-test",
			DisplayName: "Local Test Agent",
			Version:     "test",
			Terminals: []*pb.Terminal{{
				TerminalId:    "terminal-1",
				Name:          "shell",
				RemoteEnabled: true,
			}},
		},
	}})
	if err != nil {
		t.Fatalf("send grpc register: %v", err)
	}
	msg, err := stream.Recv()
	if err != nil {
		t.Fatalf("recv grpc register ack: %v", err)
	}
	ack := msg.GetRegisterAck()
	if ack.GetAgentSessionId() == "" {
		t.Fatalf("register ack missing agent_session_id: %+v", ack)
	}
	if ack.GetRelayPolicy().GetAllowRelay() {
		t.Fatalf("local hub must not enable relay by default: %+v", ack.GetRelayPolicy())
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

func TestLocalEnableCanStartCloudManagerAfterDisabledDaemonStart(t *testing.T) {
	control := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/devices/register":
			_ = json.NewEncoder(w).Encode(map[string]any{"device": map[string]any{"id": "device-late-enable"}})
		case "/api/v1/hubs":
			_ = json.NewEncoder(w).Encode(map[string]any{"hubs": []map[string]any{}})
		default:
			http.NotFound(w, r)
		}
	})
	controlServer := httptest.NewServer(control)
	defer controlServer.Close()

	service := NewService(remoteprotocol.Config{
		DataDir:    t.TempDir(),
		DeviceName: "late-enable-agent-test",
	}, nil)
	t.Cleanup(func() { _ = service.Close(context.Background()) })
	if err := service.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if status := service.Status(); status.State != remoteprotocol.StateDisabled {
		t.Fatalf("expected disabled initial status, got %+v", status)
	}

	status, err := service.LocalEnable(context.Background(), remoteprotocol.LocalEnableParams{
		LocalWebAddr: "127.0.0.1:0",
		ControlURL:   controlServer.URL,
		AccessToken:  "late-token",
	})
	if err != nil {
		t.Fatalf("LocalEnable returned error: %v", err)
	}

	managerStatus := waitForLocalManagerOnline(t, service)
	if managerStatus.ControlURL != controlServer.URL {
		t.Fatalf("cloud control URL was not attached after late enable: %+v", managerStatus)
	}
	if managerStatus.HubURL == "" || !containsTestString(managerStatus.HubURLs, status.HTTPURL) {
		t.Fatalf("local hub was not registered after late enable: status=%+v local=%+v", managerStatus, status)
	}
}

func TestLocalEnableCanAttachCloudAfterLocalOnlyEnable(t *testing.T) {
	control := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/devices/register":
			_ = json.NewEncoder(w).Encode(map[string]any{"device": map[string]any{"id": "device-late-cloud"}})
		case "/api/v1/hubs":
			_ = json.NewEncoder(w).Encode(map[string]any{"hubs": []map[string]any{}})
		default:
			http.NotFound(w, r)
		}
	})
	controlServer := httptest.NewServer(control)
	defer controlServer.Close()

	service := NewService(remoteprotocol.Config{
		DataDir:    t.TempDir(),
		DeviceName: "late-cloud-agent-test",
	}, nil)
	t.Cleanup(func() { _ = service.Close(context.Background()) })
	if err := service.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	localStatus, err := service.LocalEnable(context.Background(), remoteprotocol.LocalEnableParams{
		LocalWebAddr: "127.0.0.1:0",
	})
	if err != nil {
		t.Fatalf("local-only LocalEnable returned error: %v", err)
	}
	waitForLocalManagerOnline(t, service)

	if _, err := service.LocalEnable(context.Background(), remoteprotocol.LocalEnableParams{
		LocalWebAddr: "127.0.0.1:0",
		ControlURL:   controlServer.URL,
		AccessToken:  "late-cloud-token",
	}); err != nil {
		t.Fatalf("cloud LocalEnable returned error: %v", err)
	}

	managerStatus := waitForLocalStatus(t, service, 3*time.Second, func(status remoteprotocol.Status) bool {
		return status.State == remoteprotocol.StateOnline &&
			status.ControlURL == controlServer.URL &&
			containsTestString(status.HubURLs, localStatus.HTTPURL)
	})
	if managerStatus.HubURL == "" {
		t.Fatalf("expected hub after late cloud enable, got %+v", managerStatus)
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

func TestLocalEmbeddedHubStartsRegistryCleanup(t *testing.T) {
	originalAgentTTL := localHubAgentTTL
	originalCleanupInterval := localHubCleanupInterval
	localHubAgentTTL = 30 * time.Millisecond
	localHubCleanupInterval = 10 * time.Millisecond
	t.Cleanup(func() {
		localHubAgentTTL = originalAgentTTL
		localHubCleanupInterval = originalCleanupInterval
	})

	service := NewService(remoteprotocol.Config{}, nil)
	t.Cleanup(func() { _, _ = service.LocalDisable(context.Background()) })

	status, err := service.LocalEnable(context.Background(), remoteprotocol.LocalEnableParams{
		LocalWebAddr: "127.0.0.1:0",
	})
	if err != nil {
		t.Fatalf("LocalEnable returned error: %v", err)
	}
	client, err := discovery.NewGRPCHubClient(status.HTTPURL, "local")
	if err != nil {
		t.Fatalf("new grpc hub client: %v", err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	stream, err := client.Connect(ctx)
	if err != nil {
		t.Fatalf("connect grpc hub: %v", err)
	}
	if err := stream.Send(&pb.AgentToHub{Payload: &pb.AgentToHub_Register{
		Register: &pb.RegisterRequest{
			DeviceId:  "machine-cleanup-test",
			MachineId: "machine-cleanup-test",
			AgentId:   "agent-cleanup-test",
		},
	}}); err != nil {
		t.Fatalf("send grpc register: %v", err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("recv grpc register ack: %v", err)
	}
	if err := stream.CloseSend(); err != nil {
		t.Fatalf("close grpc stream: %v", err)
	}

	service.localMu.Lock()
	runtime := service.local
	service.localMu.Unlock()
	if runtime == nil || runtime.registry == nil {
		t.Fatal("local runtime registry was not retained")
	}
	time.Sleep(200 * time.Millisecond)
	if removed := runtime.registry.CleanupExpired(context.Background()); removed != 0 {
		t.Fatalf("local hub cleanup was not running; manual cleanup removed %d entries", removed)
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

func containsTestString(values []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
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
