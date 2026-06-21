package termxcorev2

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lozzow/termx/internal/protocol"
)

func TestProtocolServiceRoutesRemoteMethodsToTypedHook(t *testing.T) {
	now := time.Date(2026, 6, 22, 9, 30, 0, 0, time.UTC)
	fake := &fakeRemoteService{
		status: protocol.RemoteStatus{
			State:         "online",
			Detail:        "ready",
			DeviceID:      "machine-1",
			DeviceName:    "Mac",
			ControlURL:    "https://hub.example/control",
			HubURLs:       []string{"https://hub-a.example", "https://hub-b.example"},
			DataDir:       "/tmp/termx",
			Mode:          "hub",
			AllowLAN:      true,
			TerminalCount: 2,
			UpdatedAt:     now,
		},
		pairResult: protocol.RemotePairStartResult{
			Type:              "local",
			MachineID:         "machine-1",
			MachineName:       "Mac",
			LocalPairURL:      "http://127.0.0.1:18888/pair",
			PairSessionID:     "pair-1",
			PairSecret:        "secret",
			AnswerProofSecret: "proof",
			ExpiresAt:         now.Add(time.Minute),
		},
		localStatus: protocol.RemoteLocalStatus{
			Enabled:       true,
			HTTPURL:       "http://127.0.0.1:18888",
			LocalWebAddr:  "127.0.0.1:18888",
			LocalPairURL:  "http://127.0.0.1:18888/pair",
			ICETCPEnabled: true,
			ICETCPAddr:    "127.0.0.1:18889",
			ICETCPPort:    18889,
			UpdatedAt:     now,
		},
	}
	_, client, closeClient := newProtocolClientWithOptions(t, WithRemoteService(fake))
	defer closeClient()

	var status protocol.RemoteStatus
	if err := client.Call(context.Background(), "remote.status", map[string]any{}, &status); err != nil {
		t.Fatalf("remote.status: %v", err)
	}
	if status.State != "online" || status.DeviceID != "machine-1" || status.TerminalCount != 2 || !status.UpdatedAt.Equal(now) {
		t.Fatalf("unexpected remote status %#v", status)
	}

	pairParams := protocol.RemotePairStartParams{LocalPairURL: "http://local/pair", TTLSeconds: 30, AuthTTLSeconds: 60}
	var pair protocol.RemotePairStartResult
	if err := client.Call(context.Background(), "remote.pair.start", pairParams, &pair); err != nil {
		t.Fatalf("remote.pair.start: %v", err)
	}
	if pair.PairSessionID != "pair-1" || !pair.ExpiresAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("unexpected pair result %#v", pair)
	}
	if got := fake.lastPairStartParams(); got != pairParams {
		t.Fatalf("pair params should reach typed remote hook, got %#v want %#v", got, pairParams)
	}

	enableParams := protocol.RemoteLocalEnableParams{
		LocalWebAddr: "127.0.0.1:18888",
		ICETCPAddr:   "127.0.0.1:18889",
		HubURLs:      []string{"https://hub.example"},
		ControlURL:   "https://control.example",
		AccessToken:  "token",
		Region:       "local",
	}
	var local protocol.RemoteLocalStatus
	if err := client.Call(context.Background(), "remote.local.enable", enableParams, &local); err != nil {
		t.Fatalf("remote.local.enable: %v", err)
	}
	if !local.Enabled || local.LocalWebAddr != "127.0.0.1:18888" || local.ICETCPPort != 18889 {
		t.Fatalf("unexpected local enable status %#v", local)
	}
	if got := fake.lastLocalEnableParams(); got.LocalWebAddr != enableParams.LocalWebAddr || got.ICETCPAddr != enableParams.ICETCPAddr || strings.Join(got.HubURLs, ",") != strings.Join(enableParams.HubURLs, ",") || got.AccessToken != "token" {
		t.Fatalf("local enable params should reach typed remote hook, got %#v want %#v", got, enableParams)
	}

	var localStatus protocol.RemoteLocalStatus
	if err := client.Call(context.Background(), "remote.local.status", map[string]any{}, &localStatus); err != nil {
		t.Fatalf("remote.local.status: %v", err)
	}
	if !localStatus.Enabled || localStatus.HTTPURL != "http://127.0.0.1:18888" {
		t.Fatalf("unexpected local status %#v", localStatus)
	}
	var disabled protocol.RemoteLocalStatus
	if err := client.Call(context.Background(), "remote.local.disable", map[string]any{}, &disabled); err != nil {
		t.Fatalf("remote.local.disable: %v", err)
	}
	if !disabled.Enabled {
		t.Fatalf("fake disable should return configured status, got %#v", disabled)
	}

	if got := strings.Join(fake.callsSnapshot(), ","); got != "status,pair.start,local.enable,local.status,local.disable" {
		t.Fatalf("unexpected remote hook call order %s", got)
	}
}

func TestProtocolServiceRemoteMethodsRequireConfiguredHook(t *testing.T) {
	_, client, closeClient := newProtocolClient(t)
	defer closeClient()

	var status protocol.RemoteStatus
	err := client.Call(context.Background(), "remote.status", map[string]any{}, &status)
	if err == nil || !strings.Contains(err.Error(), ErrRemoteServiceUnavailable.Error()) {
		t.Fatalf("expected missing remote service error, got %v", err)
	}
}

func TestProtocolServiceRemoteHookCanBeInjectedAfterServerConstruction(t *testing.T) {
	fake := &fakeRemoteService{
		status: protocol.RemoteStatus{State: "online", DeviceID: "machine-dynamic"},
	}
	server, client, closeClient := newProtocolClientWithOptions(t)
	defer closeClient()

	server.SetRemoteService(fake)

	var status protocol.RemoteStatus
	if err := client.Call(context.Background(), "remote.status", map[string]any{}, &status); err != nil {
		t.Fatalf("remote.status after SetRemoteService: %v", err)
	}
	if status.DeviceID != "machine-dynamic" {
		t.Fatalf("unexpected remote status after SetRemoteService %#v", status)
	}
}

func newProtocolClientWithOptions(t *testing.T, opts ...ServerOption) (*Server, *protocol.Client, func()) {
	t.Helper()
	all := append([]ServerOption{WithProcessFactory(newRecordingProcessFactory())}, opts...)
	return newProtocolClientWithServer(t, NewServer(all...))
}

type fakeRemoteService struct {
	mu          sync.Mutex
	calls       []string
	status      protocol.RemoteStatus
	pairParams  protocol.RemotePairStartParams
	pairResult  protocol.RemotePairStartResult
	localParams protocol.RemoteLocalEnableParams
	localStatus protocol.RemoteLocalStatus
}

func (service *fakeRemoteService) Status(context.Context) (protocol.RemoteStatus, error) {
	service.recordCall("status")
	return service.status, nil
}

func (service *fakeRemoteService) PairStart(_ context.Context, params protocol.RemotePairStartParams) (protocol.RemotePairStartResult, error) {
	service.mu.Lock()
	service.calls = append(service.calls, "pair.start")
	service.pairParams = params
	service.mu.Unlock()
	return service.pairResult, nil
}

func (service *fakeRemoteService) LocalEnable(_ context.Context, params protocol.RemoteLocalEnableParams) (protocol.RemoteLocalStatus, error) {
	service.mu.Lock()
	service.calls = append(service.calls, "local.enable")
	service.localParams = params
	service.mu.Unlock()
	return service.localStatus, nil
}

func (service *fakeRemoteService) LocalStatus(context.Context) (protocol.RemoteLocalStatus, error) {
	service.recordCall("local.status")
	return service.localStatus, nil
}

func (service *fakeRemoteService) LocalDisable(context.Context) (protocol.RemoteLocalStatus, error) {
	service.recordCall("local.disable")
	return service.localStatus, nil
}

func (service *fakeRemoteService) recordCall(name string) {
	service.mu.Lock()
	defer service.mu.Unlock()
	service.calls = append(service.calls, name)
}

func (service *fakeRemoteService) callsSnapshot() []string {
	service.mu.Lock()
	defer service.mu.Unlock()
	return append([]string(nil), service.calls...)
}

func (service *fakeRemoteService) lastPairStartParams() protocol.RemotePairStartParams {
	service.mu.Lock()
	defer service.mu.Unlock()
	return service.pairParams
}

func (service *fakeRemoteService) lastLocalEnableParams() protocol.RemoteLocalEnableParams {
	service.mu.Lock()
	defer service.mu.Unlock()
	out := service.localParams
	out.HubURLs = append([]string(nil), out.HubURLs...)
	return out
}
