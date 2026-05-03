package runtime

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-core/internal/remote/bridge"
	"github.com/lozzow/termx/termx-core/internal/remote/cert"
	remoteconfig "github.com/lozzow/termx/termx-core/internal/remote/config"
	"github.com/lozzow/termx/termx-core/internal/remote/fileapi"
	"github.com/lozzow/termx/termx-core/internal/remote/identity"
	remotertc "github.com/lozzow/termx/termx-core/internal/remote/rtc"
	hubv1 "github.com/lozzow/termx/termx-core/remote/hubv1"
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

func TestManagerStartRegisteringWhenEndpointsConfigured(t *testing.T) {
	var gotPath string
	var gotAuth string
	var gotBody map[string]any
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"device":{"id":"device-1"}}`))
	}))
	defer control.Close()

	var hubRegisterPath string
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hubRegisterPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version":"remote.hub.v1","hub_id":"hub-local","agent_session_id":"agent-session-1","heartbeat_interval_seconds":15,"rtc_config":{"ice_servers":[]},"relay_policy":{"allow_relay":false,"allow_relay_transfer":false}}`))
	}))
	defer hub.Close()

	mgr := NewManager(remoteconfig.Config{
		Enabled:     true,
		DataDir:     t.TempDir(),
		DeviceName:  "device-b",
		ControlURL:  control.URL,
		HubURL:      hub.URL,
		AccessToken: "secret",
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
		t.Fatalf("expected online state, got %q", status.State)
	}
	if status.DeviceName != "device-b" {
		t.Fatalf("expected device name device-b, got %q", status.DeviceName)
	}
	if gotPath != "/api/devices/register" {
		t.Fatalf("expected control registration path /api/devices/register, got %q", gotPath)
	}
	if gotAuth != "Bearer secret" {
		t.Fatalf("expected bearer auth, got %q", gotAuth)
	}
	if gotBody["deviceId"] == "" {
		t.Fatalf("expected deviceId in control registration body, got %#v", gotBody)
	}
	if hubRegisterPath != "/api/v1/agents/register" {
		t.Fatalf("expected hub registration path /api/v1/agents/register, got %q", hubRegisterPath)
	}
}

func TestManagerReregistersHubWhenHeartbeatUnauthorized(t *testing.T) {
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"device":{"id":"device-1"}}`))
	}))
	defer control.Close()

	var registerCount atomic.Int32
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/agents/register":
			count := registerCount.Add(1)
			_, _ = w.Write([]byte(`{"version":"remote.hub.v1","hub_id":"hub-local","agent_session_id":"agent-session-` + string(rune('0'+count)) + `","heartbeat_interval_seconds":15,"rtc_config":{"ice_servers":[]},"relay_policy":{"allow_relay":false,"allow_relay_transfer":false}}`))
		case "/api/v1/agents/heartbeat":
			var req struct {
				AgentSessionID string `json:"agent_session_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode heartbeat: %v", err)
			}
			if req.AgentSessionID == "agent-session-1" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"unknown device or agent session"}`))
				return
			}
			_, _ = w.Write([]byte(`{"accepted":true,"next_heartbeat_seconds":15}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer hub.Close()

	mgr := NewManager(remoteconfig.Config{
		Enabled:     true,
		DataDir:     t.TempDir(),
		DeviceName:  "device-c",
		ControlURL:  control.URL,
		HubURL:      hub.URL,
		AccessToken: "secret",
	}, inventoryProviderStub{}, nil)
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	if err := mgr.syncHubPresence(context.Background()); err != nil {
		t.Fatalf("syncHubPresence returned error: %v", err)
	}

	mgr.mu.RLock()
	hubSessionID := mgr.hubSessionID
	mgr.mu.RUnlock()
	if hubSessionID != "agent-session-2" {
		t.Fatalf("expected hub session to refresh to agent-session-2, got %q", hubSessionID)
	}
	if got := registerCount.Load(); got != 2 {
		t.Fatalf("expected two hub registrations, got %d", got)
	}
}

func TestManagerProvidesTerminalManagementRouterForManagedRTC(t *testing.T) {
	manager := NewManager(remoteconfig.Config{}, managementProviderStub{}, nil)
	if manager.terminalManagementRouter() == nil {
		t.Fatal("expected managed runtime to provide terminal management router")
	}
	status, _, errMsg := manager.terminalManagementRouter().RouteTerminalManagementRequest(context.Background(), remotertc.TerminalManagementRequest{
		Method: "create",
		Path:   "create",
		Body:   json.RawMessage(`{"command":["/bin/sh"],"name":"managed shell"}`),
	})
	if status == http.StatusForbidden || errMsg == "terminal management is not allowed by connection policy" {
		t.Fatalf("managed terminal management router must not be nil or forbidden, got status=%d err=%q", status, errMsg)
	}
}

func TestManagerRejectsManagedOfferWithoutCertificateBeforeAnswering(t *testing.T) {
	manager, answerer, offer := newManagedOfferFixture(t)
	offer.AppCertificate = nil

	answer := manager.answerManagedOffer(context.Background(), offer, nil)

	if answer.Error == "" || !strings.Contains(answer.Error, "app certificate") {
		t.Fatalf("expected app certificate rejection, got %#v", answer)
	}
	if answerer.calls != 0 {
		t.Fatalf("answerer called %d times for unauthorized offer", answerer.calls)
	}
}

func TestManagerRejectsManagedOfferForUnknownTerminalBeforeAnswering(t *testing.T) {
	manager, answerer, offer := newManagedOfferFixture(t)
	offer.TerminalID = "term-missing"

	answer := manager.answerManagedOffer(context.Background(), offer, nil)

	if answer.Error == "" || !strings.Contains(answer.Error, "terminal") {
		t.Fatalf("expected terminal rejection, got %#v", answer)
	}
	if answerer.calls != 0 {
		t.Fatalf("answerer called %d times for unauthorized offer", answerer.calls)
	}
}

func TestManagerVerifiesManagedOfferSignatureAndRejectsReplay(t *testing.T) {
	manager, answerer, offer := newManagedOfferFixture(t)

	answer := manager.answerManagedOffer(context.Background(), offer, []hubv1.RTCIceServerConfig{
		{URLs: []string{"stun:stun.example.test:3478"}},
	})

	if answer.Error != "" {
		t.Fatalf("valid signed offer returned error: %#v", answer)
	}
	if answer.SDP != "v=0\r\ns=answer\r\n" || answer.SessionID != offer.SessionID {
		t.Fatalf("unexpected valid answer: %#v", answer)
	}
	if answerer.calls != 1 {
		t.Fatalf("answerer calls = %d, want 1", answerer.calls)
	}
	if answerer.gotOffer.TerminalID != "term-1" || answerer.gotOffer.AppCertificate == nil {
		t.Fatalf("answerer got unauthorized offer envelope: %#v", answerer.gotOffer)
	}
	if !answerer.gotOptions.ChannelPolicy.AllowTerminal || !answerer.gotOptions.ChannelPolicy.AllowFileManager {
		t.Fatalf("answerer policy = %#v", answerer.gotOptions.ChannelPolicy)
	}

	replayed := manager.answerManagedOffer(context.Background(), offer, nil)
	if replayed.Error == "" || !strings.Contains(replayed.Error, "nonce") {
		t.Fatalf("expected replay rejection, got %#v", replayed)
	}
	if answerer.calls != 1 {
		t.Fatalf("replayed offer reached answerer, calls=%d", answerer.calls)
	}
}

func TestManagerRejectsTamperedManagedOfferSignatureBeforeAnswering(t *testing.T) {
	manager, answerer, offer := newManagedOfferFixture(t)
	offer.SDP = "v=0\r\ns=tampered-offer\r\n"

	answer := manager.answerManagedOffer(context.Background(), offer, nil)

	if answer.Error == "" || !strings.Contains(answer.Error, "signature") {
		t.Fatalf("expected signature rejection, got %#v", answer)
	}
	if answerer.calls != 0 {
		t.Fatalf("tampered offer reached answerer, calls=%d", answerer.calls)
	}
}

func TestManagerRejectsTamperedManagedOfferCandidatesBeforeAnswering(t *testing.T) {
	manager, answerer, offer := newManagedOfferFixture(t)
	offer.ICECandidates = append(offer.ICECandidates, "candidate:tampered")

	answer := manager.answerManagedOffer(context.Background(), offer, nil)

	if answer.Error == "" || !strings.Contains(answer.Error, "signature") {
		t.Fatalf("expected signature rejection, got %#v", answer)
	}
	if answerer.calls != 0 {
		t.Fatalf("tampered candidate offer reached answerer, calls=%d", answerer.calls)
	}
}

func TestManagerRejectsManagedOfferWithoutTerminalCapabilityBeforeAnswering(t *testing.T) {
	manager, answerer, offer := newManagedOfferFixtureWithCapabilities(t, []string{"file_manager"}, nil)

	answer := manager.answerManagedOffer(context.Background(), offer, nil)

	if answer.Error == "" || !strings.Contains(answer.Error, "terminal capability") {
		t.Fatalf("expected terminal capability rejection, got %#v", answer)
	}
	if answerer.calls != 0 {
		t.Fatalf("answerer called %d times for unauthorized offer", answerer.calls)
	}
}

func TestManagerScopesManagedOfferPolicyToCertificateCapabilities(t *testing.T) {
	manager, answerer, offer := newManagedOfferFixtureWithCapabilities(t, []string{"terminal"}, nil)

	answer := manager.answerManagedOffer(context.Background(), offer, nil)

	if answer.Error != "" {
		t.Fatalf("valid terminal-only offer returned error: %#v", answer)
	}
	policy := answerer.gotOptions.ChannelPolicy
	if !policy.AllowTerminal || !policy.AllowEvents {
		t.Fatalf("terminal capability should allow terminal/events, got %#v", policy)
	}
	if policy.AllowFileManager || policy.AllowTerminalManagement {
		t.Fatalf("terminal-only certificate overgranted channel policy: %#v", policy)
	}
}

func TestManagerAllowsManagedTerminalManagementOnlyWithCapabilityAndRouter(t *testing.T) {
	provider := managementProviderStub{inventoryProviderStub: inventoryProviderStub{
		items: []TerminalInventoryItem{{ID: "term-1", Name: "shell", State: "running"}},
	}}
	manager, answerer, offer := newManagedOfferFixtureWithCapabilities(t, []string{"terminal"}, provider)

	answer := manager.answerManagedOffer(context.Background(), offer, nil)
	if answer.Error != "" {
		t.Fatalf("valid terminal-only offer returned error: %#v", answer)
	}
	if answerer.gotOptions.ChannelPolicy.AllowTerminalManagement {
		t.Fatalf("terminal-only certificate overgranted terminal management: %#v", answerer.gotOptions.ChannelPolicy)
	}

	manager, answerer, offer = newManagedOfferFixtureWithCapabilities(t, []string{"terminal", "terminal_management"}, provider)
	answer = manager.answerManagedOffer(context.Background(), offer, nil)
	if answer.Error != "" {
		t.Fatalf("valid terminal-management offer returned error: %#v", answer)
	}
	if !answerer.gotOptions.ChannelPolicy.AllowTerminalManagement {
		t.Fatalf("terminal_management capability with router should allow management: %#v", answerer.gotOptions.ChannelPolicy)
	}
}

func TestHubSignalingLoopResetsSessionWhenAnswerSubmitUnauthorized(t *testing.T) {
	manager, _, offer := newManagedOfferFixture(t)
	var answerRequests atomic.Int32
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/agents/signaling/poll":
			if answerRequests.Load() == 0 {
				_ = json.NewEncoder(w).Encode(hubv1.SignalingPollResponse{Offer: &offer})
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case "/api/v1/agents/signaling/answer":
			answerRequests.Add(1)
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unknown agent session"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer hub.Close()

	manager.cfg.HubURL = hub.URL
	manager.hubSessionID = "agent-session-1"
	manager.signalingStarted = true
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		manager.hubSignalingLoop(ctx, offer.DeviceID, "agent-session-1", nil)
	}()

	waitForCondition(t, time.Second, func() bool {
		manager.mu.RLock()
		sessionID := manager.hubSessionID
		started := manager.signalingStarted
		manager.mu.RUnlock()
		return answerRequests.Load() == 1 && sessionID == "" && !started
	})
	select {
	case <-manager.syncCh:
	case <-time.After(time.Second):
		t.Fatal("expected unauthorized answer submit to trigger sync")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("expected signaling loop to stop after unauthorized answer submit")
	}
}

func TestHubSignalingLoopMarksDegradedWhenAnswerSubmitFails(t *testing.T) {
	manager, _, offer := newManagedOfferFixture(t)
	var answerRequests atomic.Int32
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/agents/signaling/poll":
			if answerRequests.Load() == 0 {
				_ = json.NewEncoder(w).Encode(hubv1.SignalingPollResponse{Offer: &offer})
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case "/api/v1/agents/signaling/answer":
			answerRequests.Add(1)
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error":"temporary submit failure"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer hub.Close()

	manager.cfg.HubURL = hub.URL
	manager.hubSessionID = "agent-session-1"
	manager.setStatus(StateOnline, "before submit")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go manager.hubSignalingLoop(ctx, offer.DeviceID, "agent-session-1", nil)

	waitForCondition(t, time.Second, func() bool {
		status := manager.Status()
		return answerRequests.Load() == 1 &&
			status.State == StateDegraded &&
			strings.Contains(status.Detail, "submit hub answer")
	})
}

func TestHubSignalingLoopRetriesOriginalAnswerAfterTransientSubmitFailure(t *testing.T) {
	manager, answerer, offer := newManagedOfferFixture(t)
	var answerRequests atomic.Int32
	submitted := make(chan hubv1.SignalingAnswer, 2)
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/agents/signaling/poll":
			_ = json.NewEncoder(w).Encode(hubv1.SignalingPollResponse{Offer: &offer})
		case "/api/v1/agents/signaling/answer":
			var req hubv1.SubmitSignalingAnswerRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode answer submit: %v", err)
			}
			submitted <- req.Answer
			if answerRequests.Add(1) == 1 {
				w.WriteHeader(http.StatusBadGateway)
				_, _ = w.Write([]byte(`{"error":"temporary submit failure"}`))
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer hub.Close()

	manager.cfg.HubURL = hub.URL
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go manager.hubSignalingLoop(ctx, offer.DeviceID, "agent-session-1", nil)

	var first hubv1.SignalingAnswer
	select {
	case first = <-submitted:
	case <-time.After(time.Second):
		t.Fatal("expected first answer submit")
	}
	var second hubv1.SignalingAnswer
	select {
	case second = <-submitted:
	case <-time.After(time.Second):
		t.Fatal("expected retry answer submit")
	}
	cancel()

	if first.Error != "" || first.SDP != "v=0\r\ns=answer\r\n" {
		t.Fatalf("expected original first answer, got %#v", first)
	}
	if !reflect.DeepEqual(second, first) {
		t.Fatalf("expected retry to submit original answer %#v, got %#v", first, second)
	}
	if answerer.calls != 1 {
		t.Fatalf("expected retry not to re-answer replayed offer, answerer calls=%d", answerer.calls)
	}
}

func TestHubSignalingLoopDoesNotRetryCachedAnswerForChangedOfferPayload(t *testing.T) {
	manager, answerer, offer := newManagedOfferFixture(t)
	changedOffer := offer
	changedOffer.SDP = "v=0\r\ns=changed-offer\r\n"
	testHubSignalingLoopDoesNotRetryCachedAnswerForChangedOffer(t, manager, answerer, offer, changedOffer)
}

func TestHubSignalingLoopDoesNotRetryCachedAnswerWhenRawOfferWhitespaceChanges(t *testing.T) {
	manager, answerer, offer := newManagedOfferFixture(t)
	changedOffer := offer
	changedOffer.SDP = offer.SDP + " "
	testHubSignalingLoopDoesNotRetryCachedAnswerForChangedOffer(t, manager, answerer, offer, changedOffer)
}

func testHubSignalingLoopDoesNotRetryCachedAnswerForChangedOffer(t *testing.T, manager *Manager, answerer *managedAnswererStub, offer hubv1.SignalingOffer, changedOffer hubv1.SignalingOffer) {
	t.Helper()
	var pollRequests atomic.Int32
	var answerRequests atomic.Int32
	submitted := make(chan hubv1.SignalingAnswer, 2)
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/agents/signaling/poll":
			if pollRequests.Add(1) == 1 {
				_ = json.NewEncoder(w).Encode(hubv1.SignalingPollResponse{Offer: &offer})
				return
			}
			_ = json.NewEncoder(w).Encode(hubv1.SignalingPollResponse{Offer: &changedOffer})
		case "/api/v1/agents/signaling/answer":
			var req hubv1.SubmitSignalingAnswerRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode answer submit: %v", err)
			}
			submitted <- req.Answer
			if answerRequests.Add(1) == 1 {
				w.WriteHeader(http.StatusBadGateway)
				_, _ = w.Write([]byte(`{"error":"temporary submit failure"}`))
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer hub.Close()

	manager.cfg.HubURL = hub.URL
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go manager.hubSignalingLoop(ctx, offer.DeviceID, "agent-session-1", nil)

	var first hubv1.SignalingAnswer
	select {
	case first = <-submitted:
	case <-time.After(time.Second):
		t.Fatal("expected first answer submit")
	}
	var second hubv1.SignalingAnswer
	select {
	case second = <-submitted:
	case <-time.After(time.Second):
		t.Fatal("expected second answer submit")
	}
	cancel()

	if first.Error != "" || first.SDP != "v=0\r\ns=answer\r\n" {
		t.Fatalf("expected original first answer, got %#v", first)
	}
	if second.Error == "" || !strings.Contains(second.Error, "signature") {
		t.Fatalf("expected changed repeated offer to be re-authorized and rejected, got %#v", second)
	}
	if answerer.calls != 1 {
		t.Fatalf("expected changed invalid offer not to reach answerer again, calls=%d", answerer.calls)
	}
}

func TestManagedOfferEnvelopeDoesNotExposeBrowserWebRTCTypes(t *testing.T) {
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
	return http.StatusOK, []byte(`{"terminal_id":"managed-terminal-1"}`), ""
}

type managedAnswererStub struct {
	calls      int
	gotOffer   hubv1.SignalingOffer
	gotOptions remotertc.AnswerOptions
}

func (s *managedAnswererStub) AnswerOffer(
	_ context.Context,
	offer hubv1.SignalingOffer,
	_ []hubv1.RTCIceServerConfig,
	_ bridge.TransportSink,
	_ *fileapi.Manager,
	opts remotertc.AnswerOptions,
) (hubv1.SignalingAnswer, error) {
	s.calls++
	s.gotOffer = offer
	s.gotOptions = opts
	return hubv1.SignalingAnswer{
		SessionID: offer.SessionID,
		SDP:       "v=0\r\ns=answer\r\n",
	}, nil
}

func newManagedOfferFixture(t *testing.T) (*Manager, *managedAnswererStub, hubv1.SignalingOffer) {
	t.Helper()
	return newManagedOfferFixtureWithCapabilities(t, []string{"terminal", "file_manager"}, nil)
}

func newManagedOfferFixtureWithCapabilities(t *testing.T, capabilities []string, provider InventoryProvider) (*Manager, *managedAnswererStub, hubv1.SignalingOffer) {
	t.Helper()
	dataDir := t.TempDir()
	machineKey, err := identity.LoadOrCreateMachineKey(dataDir)
	if err != nil {
		t.Fatalf("LoadOrCreateMachineKey returned error: %v", err)
	}
	appPublic, appPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey returned error: %v", err)
	}
	now := time.Now().UTC().Round(0)
	envelope, err := cert.SignAppCertificate(cert.AppCertificatePayload{
		Version:      1,
		CertID:       "cert-managed-test",
		MachineID:    "device-managed-test",
		AppDeviceID:  "app-device-managed-test",
		AppPublicKey: base64.StdEncoding.EncodeToString(appPublic),
		AppName:      "Managed Test App",
		Capabilities: append([]string(nil), capabilities...),
		IssuedAt:     now.Add(-time.Minute),
		ExpiresAt:    now.Add(time.Hour),
	}, machineKey)
	if err != nil {
		t.Fatalf("SignAppCertificate returned error: %v", err)
	}
	certificateJSON, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal certificate: %v", err)
	}
	offer := hubv1.SignalingOffer{
		SessionID:      "rtc-managed-test",
		TicketID:       "ticket-managed-test",
		DeviceID:       "device-managed-test",
		TerminalID:     "term-1",
		SDP:            "v=0\r\ns=offer\r\n",
		ICECandidates:  []string{"candidate:host-test"},
		AppCertificate: certificateJSON,
	}
	signatureFields := remotertc.OfferSignatureFields{
		TicketID:   offer.TicketID,
		MachineID:  offer.DeviceID,
		TerminalID: offer.TerminalID,
		SDP:        offer.SDP,
		Candidates: offer.ICECandidates,
		Nonce:      "nonce-managed-test",
		Timestamp:  now,
	}
	offer.Signature = hubv1.OfferSignature{
		Algorithm: "ed25519",
		Nonce:     signatureFields.Nonce,
		Timestamp: signatureFields.Timestamp.Unix(),
		Value:     base64.StdEncoding.EncodeToString(ed25519.Sign(appPrivate, remotertc.CanonicalOfferSignatureMessage(signatureFields))),
	}
	answerer := &managedAnswererStub{}
	if provider == nil {
		provider = inventoryProviderStub{
			items: []TerminalInventoryItem{{ID: "term-1", Name: "shell", State: "running"}},
		}
	}
	manager := NewManager(remoteconfig.Config{
		Enabled:    true,
		DataDir:    dataDir,
		DeviceName: "managed-test-device",
	}, provider, nil)
	manager.identity = identity.DeviceIdentity{DeviceID: "device-managed-test", DisplayName: "managed-test-device"}
	manager.answerer = answerer
	return manager, answerer, offer
}

func waitForCondition(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	if condition() {
		return
	}
	t.Fatalf("condition not met within %s", timeout)
}
