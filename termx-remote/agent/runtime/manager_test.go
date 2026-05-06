package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-core/transport"
	"github.com/lozzow/termx/termx-remote/bridge"
	remoteconfig "github.com/lozzow/termx/termx-remote/config"
	"github.com/lozzow/termx/termx-remote/fileapi"
	"github.com/lozzow/termx/termx-remote/identity"
	"github.com/lozzow/termx/termx-remote/pairing"
	hubv1 "github.com/lozzow/termx/termx-remote/protocol/hubv1"
	remotertc "github.com/lozzow/termx/termx-remote/session/rtc"
	"github.com/lozzow/termx/termx-remote/session/token"
	"github.com/pion/webrtc/v4"
)

type inventoryProviderStub struct {
	items []TerminalInventoryItem
}

func (s inventoryProviderStub) ListRemoteTerminals(context.Context) []TerminalInventoryItem {
	return append([]TerminalInventoryItem(nil), s.items...)
}

func TestManagerStartDisabled(t *testing.T) {
	mgr := NewManager(remoteconfig.Config{}, nil, nil)
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	status := mgr.Status()
	if status.State != StateDisabled {
		t.Fatalf("expected disabled state, got %q", status.State)
	}
}

func TestManagerStartConfiguredWithoutEndpoints(t *testing.T) {
	mgr := NewManager(remoteconfig.Config{
		Enabled:    true,
		DataDir:    t.TempDir(),
		DeviceName: "device-a",
	}, inventoryProviderStub{
		items: []TerminalInventoryItem{{ID: "1"}, {ID: "2"}},
	}, nil)
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	status := mgr.Status()
	if status.State != StateConfigured {
		t.Fatalf("expected configured state, got %q", status.State)
	}
	if status.DeviceID == "" {
		t.Fatal("expected device id to be populated")
	}
	if status.TerminalCount != 2 {
		t.Fatalf("expected terminal count 2, got %d", status.TerminalCount)
	}
}

func TestManagerStartDegradedWhenControlTokenMissing(t *testing.T) {
	mgr := NewManager(remoteconfig.Config{
		Enabled:    true,
		DataDir:    t.TempDir(),
		ControlURL: "https://control.example.test",
	}, nil, nil)
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	status := mgr.Status()
	if status.State != StateDegraded {
		t.Fatalf("expected degraded state, got %q", status.State)
	}
}

func TestManagerStartRegistersWithLocalHub(t *testing.T) {
	var got hubv1.HubRegisterRequest
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/agents/register" {
			t.Fatalf("unexpected hub path %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode hub register: %v", err)
		}
		_ = json.NewEncoder(w).Encode(hubv1.HubRegisterResponse{
			Version:                  "remote.hub.v1",
			HubID:                    "hub-local",
			AgentSessionID:           "agent-session-1",
			HeartbeatIntervalSeconds: 15,
		})
	}))
	defer hub.Close()

	mgr := NewManager(remoteconfig.Config{
		Enabled:    true,
		DataDir:    t.TempDir(),
		DeviceName: "device-b",
		HubURL:     hub.URL,
		Mode:       "local",
	}, inventoryProviderStub{
		items: []TerminalInventoryItem{{
			ID:      "term-1",
			Name:    "shell",
			State:   "running",
			Command: []string{"bash"},
			Cols:    80,
			Rows:    24,
		}},
	}, nil)
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	status := mgr.Status()
	if status.State != StateOnline {
		t.Fatalf("expected online state, got %q detail=%q", status.State, status.Detail)
	}
	if got.DeviceID == "" || got.AgentID == "" || got.DisplayName != "device-b" {
		t.Fatalf("registration missing identity fields: %+v", got)
	}
	if len(got.Terminals) != 1 || got.Terminals[0].ID != "term-1" {
		t.Fatalf("registration terminals = %+v", got.Terminals)
	}
}

func TestManagerReregistersLocalHubWhenHeartbeatUnauthorized(t *testing.T) {
	var registerCount atomic.Int32
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/agents/register":
			count := registerCount.Add(1)
			_ = json.NewEncoder(w).Encode(hubv1.HubRegisterResponse{
				Version:                  "remote.hub.v1",
				HubID:                    "hub-local",
				AgentSessionID:           "agent-session-" + string(rune('0'+count)),
				HeartbeatIntervalSeconds: 15,
			})
		case "/api/v1/agents/heartbeat":
			var req hubv1.HubHeartbeatRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode heartbeat: %v", err)
			}
			if req.AgentSessionID == "agent-session-1" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"unknown device or agent session"}`))
				return
			}
			_ = json.NewEncoder(w).Encode(hubv1.HubHeartbeatResponse{Accepted: true, NextHeartbeatSeconds: 15})
		default:
			http.NotFound(w, r)
		}
	}))
	defer hub.Close()

	mgr := NewManager(remoteconfig.Config{
		Enabled:    true,
		DataDir:    t.TempDir(),
		DeviceName: "device-c",
		HubURL:     hub.URL,
		Mode:       "local",
	}, inventoryProviderStub{}, nil)
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	if err := mgr.syncHubPresence(context.Background(), hub.URL); err != nil {
		t.Fatalf("syncHubPresence returned error: %v", err)
	}

	mgr.mu.RLock()
	stateSessionID := mgr.hubStateLocked(hub.URL).HTTPSessionID
	mgr.mu.RUnlock()
	if stateSessionID != "agent-session-2" {
		t.Fatalf("expected hub session to refresh to agent-session-2, got %q", stateSessionID)
	}
	if got := registerCount.Load(); got != 2 {
		t.Fatalf("expected two hub registrations, got %d", got)
	}
}

func TestManagerDoesNotReregisterHubWhenHeartbeatForcedOffline(t *testing.T) {
	var registerCount atomic.Int32
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/agents/register":
			count := registerCount.Add(1)
			_ = json.NewEncoder(w).Encode(hubv1.HubRegisterResponse{
				Version:                  "remote.hub.v1",
				HubID:                    "hub-forced",
				AgentSessionID:           "agent-session-" + string(rune('0'+count)),
				HeartbeatIntervalSeconds: 15,
			})
		case "/api/v1/agents/heartbeat":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"agent_heartbeat_failed","message":"agent forced offline"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer hub.Close()

	mgr := NewManager(remoteconfig.Config{
		Enabled:    true,
		DataDir:    t.TempDir(),
		DeviceName: "device-forced",
		HubURL:     hub.URL,
		Mode:       "local",
	}, inventoryProviderStub{}, nil)
	mgr.identity = identity.DeviceIdentity{DeviceID: "device-forced", DisplayName: "device-forced"}

	if err := mgr.syncHubPresence(context.Background(), hub.URL); err != nil {
		t.Fatalf("initial syncHubPresence returned error: %v", err)
	}
	err := mgr.syncHubPresence(context.Background(), hub.URL)
	if err == nil || !strings.Contains(err.Error(), "forced offline") {
		t.Fatalf("expected forced offline error, got %v", err)
	}

	mgr.mu.RLock()
	state := mgr.hubStateLocked(hub.URL)
	stateSessionID := state.HTTPSessionID
	forcedOffline := state.ForcedOffline
	mgr.mu.RUnlock()
	if stateSessionID != "" || !forcedOffline {
		t.Fatalf("forced offline should clear hub session and mark state, session=%q forced=%v", stateSessionID, forcedOffline)
	}
	if got := registerCount.Load(); got != 1 {
		t.Fatalf("forced offline must not re-register hub, got %d registrations", got)
	}
}

func TestManagerProvidesTerminalManagementRouterForCloudRTC(t *testing.T) {
	manager := NewManager(remoteconfig.Config{}, managementProviderStub{}, nil)
	if manager.terminalManagementRouter() == nil {
		t.Fatal("expected cloud runtime to provide terminal management router")
	}
	status, _, errMsg := manager.terminalManagementRouter().RouteTerminalManagementRequest(context.Background(), remotertc.TerminalManagementRequest{
		Method: "create",
		Path:   "create",
		Body:   json.RawMessage(`{"command":["/bin/sh"],"name":"cloud shell"}`),
	})
	if status == http.StatusForbidden || errMsg == "terminal management is not allowed by connection policy" {
		t.Fatalf("cloud terminal management router must not be nil or forbidden, got status=%d err=%q", status, errMsg)
	}
}

func TestClaimHubPairingClaimsThroughPairingManager(t *testing.T) {
	now := time.Date(2026, 5, 4, 10, 30, 0, 0, time.UTC)
	machineSecret := []byte("0123456789abcdef0123456789abcdef")
	pairManager := pairing.NewManager(pairing.Config{
		MachineID:     "device-pair",
		MachineName:   "Pair Device",
		MachineSecret: machineSecret,
		Now: func() time.Time {
			return now
		},
	})
	session, err := pairManager.CreateSession(5 * time.Minute)
	if err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}
	manager := NewManager(remoteconfig.Config{Enabled: true, DataDir: t.TempDir(), DeviceName: "Pair Device"}, inventoryProviderStub{}, nil)
	manager.SetPairClaimer(pairClaimerFunc(func(ctx context.Context, req pairing.ClaimRequest) (pairing.ClaimResponse, error) {
		_ = ctx
		return pairManager.ClaimSession(req)
	}))

	result := manager.claimHubPairing(context.Background(), hubv1.PairingClaim{
		ClaimID:               "claim-1",
		MachineID:             "device-pair",
		PairSessionID:         session.PairSessionID,
		PairSecret:            session.PairSecret,
		RequestedCapabilities: []string{"terminal", "terminal_management"},
	})
	if result.ClaimID != "claim-1" || result.MachineID != "device-pair" || result.MachineName != "Pair Device" || result.Error != "" {
		t.Fatalf("pairing result = %+v", result)
	}
	claims, err := token.Verify(result.SessionToken, machineSecret, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("session token did not verify: %v", err)
	}
	if claims.MachineID != "device-pair" || strings.Join(claims.Capabilities, ",") != "terminal,terminal_management" {
		t.Fatalf("claims = %+v", claims)
	}
}

func TestManagerAcceptsCloudOfferWithValidSessionToken(t *testing.T) {
	manager, answerer, offer := newCloudOfferFixture(t)

	answer := manager.answerCloudOffer(context.Background(), offer, []hubv1.RTCIceServerConfig{
		{URLs: []string{"stun:stun.example.test:3478"}},
	})

	if answer.Error != "" {
		t.Fatalf("valid token offer returned error: %#v", answer)
	}
	if answer.SDP != "v=0\r\ns=answer\r\n" || answer.SessionID != offer.SessionID {
		t.Fatalf("unexpected valid answer: %#v", answer)
	}
	if answerer.calls != 1 {
		t.Fatalf("answerer calls = %d, want 1", answerer.calls)
	}
	if answerer.gotOffer.TerminalID != "term-1" || answerer.gotOffer.SessionToken == "" {
		t.Fatalf("answerer got unauthorized offer envelope: %#v", answerer.gotOffer)
	}
	if !answerer.gotOptions.ChannelPolicy.AllowTerminal || !answerer.gotOptions.ChannelPolicy.AllowFileManager {
		t.Fatalf("answerer policy = %#v", answerer.gotOptions.ChannelPolicy)
	}
}

func TestManagerRejectsCloudOfferWithoutSessionTokenBeforeAnswering(t *testing.T) {
	manager, answerer, offer := newCloudOfferFixture(t)
	offer.SessionToken = ""

	answer := manager.answerCloudOffer(context.Background(), offer, nil)

	if answer.Error == "" || !strings.Contains(answer.Error, "session_token") {
		t.Fatalf("expected session_token rejection, got %#v", answer)
	}
	if answerer.calls != 0 {
		t.Fatalf("answerer called %d times for unauthorized offer", answerer.calls)
	}
}

func TestManagerRejectsCloudOfferForUnknownTerminalBeforeAnswering(t *testing.T) {
	manager, answerer, offer := newCloudOfferFixture(t)
	offer.TerminalID = "term-missing"

	answer := manager.answerCloudOffer(context.Background(), offer, nil)

	if answer.Error == "" || !strings.Contains(answer.Error, "terminal") {
		t.Fatalf("expected terminal rejection, got %#v", answer)
	}
	if answerer.calls != 0 {
		t.Fatalf("answerer called %d times for unauthorized offer", answerer.calls)
	}
}

func TestManagerRejectsCloudOfferWithoutTerminalCapabilityBeforeAnswering(t *testing.T) {
	manager, answerer, offer := newCloudOfferFixtureWithCapabilities(t, []string{"file_manager"}, nil)

	answer := manager.answerCloudOffer(context.Background(), offer, nil)

	if answer.Error == "" || !strings.Contains(answer.Error, "terminal capability") {
		t.Fatalf("expected terminal capability rejection, got %#v", answer)
	}
	if answerer.calls != 0 {
		t.Fatalf("answerer called %d times for unauthorized offer", answerer.calls)
	}
}

func TestManagerAllowsMachineScopedCloudOfferForTerminalList(t *testing.T) {
	provider := managementProviderStub{inventoryProviderStub: inventoryProviderStub{
		items: []TerminalInventoryItem{{ID: "term-1", Name: "shell", State: "running"}},
	}}
	manager, answerer, offer := newCloudOfferFixtureWithCapabilitiesAndTerminal(t, []string{"terminal", "file_manager", "terminal_management"}, provider, "")

	answer := manager.answerCloudOffer(context.Background(), offer, nil)

	if answer.Error != "" {
		t.Fatalf("valid machine-scoped cloud offer returned error: %#v", answer)
	}
	policy := answerer.gotOptions.ChannelPolicy
	if policy.TerminalID != "" {
		t.Fatalf("machine-scoped offer should not pin a terminal: %#v", policy)
	}
	if !policy.AllowTerminal || !policy.AllowEvents || !policy.AllowFileManager || !policy.AllowAPI {
		t.Fatalf("machine-scoped offer should allow machine-level terminal/events/files: %#v", policy)
	}
	if !policy.AllowTerminalManagement {
		t.Fatalf("machine-scoped terminal list offer should allow terminal management API: %#v", policy)
	}
}

func TestManagerScopesCloudOfferPolicyToSessionTokenCapabilities(t *testing.T) {
	manager, answerer, offer := newCloudOfferFixtureWithCapabilities(t, []string{"terminal"}, nil)

	answer := manager.answerCloudOffer(context.Background(), offer, nil)

	if answer.Error != "" {
		t.Fatalf("valid terminal-only offer returned error: %#v", answer)
	}
	policy := answerer.gotOptions.ChannelPolicy
	if !policy.AllowTerminal || !policy.AllowEvents {
		t.Fatalf("terminal capability should allow terminal/events, got %#v", policy)
	}
	if !policy.AllowAPI || policy.AllowFileManager || policy.AllowTerminalManagement || policy.AllowRelayTransfer {
		t.Fatalf("terminal-only token overgranted channel policy: %#v", policy)
	}
}

func TestManagerAllowsCloudTerminalManagementOnlyWithCapabilityAndRouter(t *testing.T) {
	provider := managementProviderStub{inventoryProviderStub: inventoryProviderStub{
		items: []TerminalInventoryItem{{ID: "term-1", Name: "shell", State: "running"}},
	}}
	manager, answerer, offer := newCloudOfferFixtureWithCapabilities(t, []string{"terminal"}, provider)

	answer := manager.answerCloudOffer(context.Background(), offer, nil)
	if answer.Error != "" {
		t.Fatalf("valid terminal-only offer returned error: %#v", answer)
	}
	if answerer.gotOptions.ChannelPolicy.AllowTerminalManagement {
		t.Fatalf("terminal-only token overgranted terminal management: %#v", answerer.gotOptions.ChannelPolicy)
	}

	manager, answerer, offer = newCloudOfferFixtureWithCapabilities(t, []string{"terminal", "terminal_management"}, provider)
	answer = manager.answerCloudOffer(context.Background(), offer, nil)
	if answer.Error != "" {
		t.Fatalf("valid terminal-management offer returned error: %#v", answer)
	}
	if !answerer.gotOptions.ChannelPolicy.AllowTerminalManagement {
		t.Fatalf("terminal_management capability with router should allow management: %#v", answerer.gotOptions.ChannelPolicy)
	}
	if answerer.gotOptions.Events == nil {
		t.Fatal("cloud runtime should provide machine event router when provider supports it")
	}
}

func TestManagerUsesHubScopedAnswerOptions(t *testing.T) {
	manager, answerer, offer := newCloudOfferFixture(t)
	hubCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager.mu.Lock()
	state := manager.hubStateLocked("hub://test")
	state.RTCServers = []hubv1.RTCIceServerConfig{{URLs: []string{"stun:registration.example:3478"}}}
	state.AnswerOptions = remotertc.AnswerOptions{SettingEngine: noopSettingEngine{}}
	state.SessionContext = hubCtx
	manager.mu.Unlock()

	answer, err := manager.answerOffer(context.Background(), "hub://test", offer)
	if err != nil {
		t.Fatalf("answerOffer returned error: %v", err)
	}
	if answer.Error != "" {
		t.Fatalf("valid scoped offer returned error: %#v", answer)
	}
	if answerer.calls != 1 {
		t.Fatalf("answerer calls = %d, want 1", answerer.calls)
	}
	if answerer.gotOptions.SettingEngine == nil || answerer.gotOptions.SessionContext == nil {
		t.Fatalf("hub-scoped answer options were not passed: %#v", answerer.gotOptions)
	}
	if len(answerer.gotICE) != 1 || answerer.gotICE[0].URLs[0] != "stun:registration.example:3478" {
		t.Fatalf("answerer ICE servers = %+v", answerer.gotICE)
	}
}

func TestCloudOfferEnvelopeDoesNotExposeBrowserWebRTCTypes(t *testing.T) {
	typ := reflect.TypeOf(hubv1.SignalingOffer{})
	for i := 0; i < typ.NumField(); i++ {
		fieldType := typ.Field(i).Type.String()
		if strings.Contains(fieldType, "RTCPeerConnection") || strings.Contains(fieldType, "RTCDataChannel") {
			t.Fatalf("hubv1.SignalingOffer field %s leaks browser WebRTC type %s", typ.Field(i).Name, fieldType)
		}
	}
}

type managementProviderStub struct {
	inventoryProviderStub
}

func (s managementProviderStub) RouteTerminalManagementRequest(_ context.Context, req remotertc.TerminalManagementRequest) (int32, []byte, string) {
	if req.Path != "create" {
		return http.StatusNotFound, nil, "unknown terminal management route"
	}
	return http.StatusOK, []byte(`{"terminal_id":"cloud-terminal-1"}`), ""
}

func (s managementProviderStub) SubscribeRemoteEvents(ctx context.Context, _ remotertc.EventFilters) (<-chan []byte, func(), error) {
	ch := make(chan []byte, 1)
	ch <- []byte(`{"type":"event"}`)
	close(ch)
	return ch, func() { _ = ctx }, nil
}

type cloudAnswererStub struct {
	calls      int
	gotOffer   hubv1.SignalingOffer
	gotICE     []hubv1.RTCIceServerConfig
	gotOptions remotertc.AnswerOptions
}

func (s *cloudAnswererStub) AnswerOffer(
	_ context.Context,
	offer hubv1.SignalingOffer,
	iceServers []hubv1.RTCIceServerConfig,
	_ bridge.TransportSink,
	_ *fileapi.Manager,
	opts any,
) (hubv1.SignalingAnswer, error) {
	s.calls++
	s.gotOffer = offer
	s.gotICE = append([]hubv1.RTCIceServerConfig(nil), iceServers...)
	s.gotOptions, _ = opts.(remotertc.AnswerOptions)
	return hubv1.SignalingAnswer{
		SessionID: offer.SessionID,
		SDP:       "v=0\r\ns=answer\r\n",
	}, nil
}

type transportSinkStub struct{}

func (transportSinkStub) ServeRemoteTransport(context.Context, transport.Transport, string) error {
	return nil
}

type noopSettingEngine struct{}

func (noopSettingEngine) Apply(*webrtc.SettingEngine) {}

func newCloudOfferFixture(t *testing.T) (*Manager, *cloudAnswererStub, hubv1.SignalingOffer) {
	t.Helper()
	return newCloudOfferFixtureWithCapabilities(t, []string{"terminal", "file_manager"}, nil)
}

func newCloudOfferFixtureWithCapabilities(t *testing.T, capabilities []string, provider InventoryProvider) (*Manager, *cloudAnswererStub, hubv1.SignalingOffer) {
	t.Helper()
	return newCloudOfferFixtureWithCapabilitiesAndTerminal(t, capabilities, provider, "term-1")
}

func newCloudOfferFixtureWithCapabilitiesAndTerminal(t *testing.T, capabilities []string, provider InventoryProvider, terminalID string) (*Manager, *cloudAnswererStub, hubv1.SignalingOffer) {
	t.Helper()
	dataDir := t.TempDir()
	machineSecret, err := identity.LoadOrCreateMachineSecret(dataDir)
	if err != nil {
		t.Fatalf("load machine secret: %v", err)
	}
	now := time.Now().UTC().Round(0)
	sessionToken := issueSessionTokenForTest(t, machineSecret, token.Claims{
		SessionID:    "pair-cloud-test",
		MachineID:    "device-cloud-test",
		Capabilities: append([]string(nil), capabilities...),
		IssuedAt:     now.Add(-time.Minute).Unix(),
		ExpiresAt:    now.Add(time.Hour).Unix(),
	})
	offer := hubv1.SignalingOffer{
		SessionID:    "rtc-cloud-test",
		MachineID:    "device-cloud-test",
		TerminalID:   terminalID,
		SDP:          "v=0\r\ns=offer\r\n",
		Candidates:   []string{"candidate:host-test"},
		SessionToken: sessionToken,
	}
	answerer := &cloudAnswererStub{}
	if provider == nil {
		provider = inventoryProviderStub{
			items: []TerminalInventoryItem{{ID: "term-1", Name: "shell", State: "running"}},
		}
	}
	manager := NewManager(remoteconfig.Config{
		Enabled:    true,
		DataDir:    dataDir,
		DeviceName: "cloud-test-device",
	}, provider, nil)
	manager.identity = identity.DeviceIdentity{DeviceID: "device-cloud-test", DisplayName: "cloud-test-device"}
	manager.answerer = answerer
	return manager, answerer, offer
}

type pairClaimerFunc func(context.Context, pairing.ClaimRequest) (pairing.ClaimResponse, error)

func (f pairClaimerFunc) ClaimPairSession(ctx context.Context, req pairing.ClaimRequest) (pairing.ClaimResponse, error) {
	return f(ctx, req)
}
