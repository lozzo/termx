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

	"github.com/lozzow/termx/termx-core/transport"
	"github.com/lozzow/termx/termx-remote/bridge"
	"github.com/lozzow/termx/termx-remote/cert"
	remoteconfig "github.com/lozzow/termx/termx-remote/config"
	"github.com/lozzow/termx/termx-remote/fileapi"
	"github.com/lozzow/termx/termx-remote/identity"
	"github.com/lozzow/termx/termx-remote/pairing"
	hubv1 "github.com/lozzow/termx/termx-remote/protocol/hubv1"
	remotertc "github.com/lozzow/termx/termx-remote/session/rtc"
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

	if err := mgr.syncHubPresence(context.Background(), hub.URL); err != nil {
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

func TestManagerDoesNotReregisterHubWhenHeartbeatForcedOffline(t *testing.T) {
	var registerCount atomic.Int32
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/agents/register":
			count := registerCount.Add(1)
			_, _ = w.Write([]byte(`{"version":"remote.hub.v1","hub_id":"hub-forced","agent_session_id":"agent-session-` + string(rune('0'+count)) + `","heartbeat_interval_seconds":15,"rtc_config":{"ice_servers":[]},"relay_policy":{"allow_relay":false,"allow_relay_transfer":false}}`))
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
	hubSessionID := mgr.hubSessionID
	stateSessionID := mgr.hubStateLocked(hub.URL).SessionID
	mgr.mu.RUnlock()
	if hubSessionID != "" || stateSessionID != "" {
		t.Fatalf("forced offline should clear hub sessions, legacy=%q state=%q", hubSessionID, stateSessionID)
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

func TestHubPairingLoopClaimsThroughLocalPairingManager(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	now := time.Date(2026, 5, 4, 10, 30, 0, 0, time.UTC)
	machineKey, err := identity.LoadOrCreateMachineKey(t.TempDir())
	if err != nil {
		t.Fatalf("LoadOrCreateMachineKey returned error: %v", err)
	}
	pairManager := pairing.NewManager(pairing.Config{
		MachineID:   "device-pair",
		MachineName: "Pair Device",
		MachineKey:  machineKey,
		Now: func() time.Time {
			return now
		},
	})
	session, err := pairManager.CreateSession(5 * time.Minute)
	if err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}
	appPublic, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey returned error: %v", err)
	}

	claimReady := make(chan struct{})
	resultReady := make(chan hubv1.PairingResult, 1)
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/agents/pairing/poll":
			writeTestJSON(w, map[string]any{
				"claim": map[string]any{
					"claim_id":               "claim-1",
					"machine_id":             "device-pair",
					"pair_session_id":        session.PairSessionID,
					"pair_secret":            session.PairSecret,
					"app_device_id":          "appweb-pair",
					"app_name":               "TermX Remote App",
					"app_public_key":         base64.StdEncoding.EncodeToString(appPublic),
					"requested_capabilities": []string{"terminal", "terminal_management"},
				},
			})
			closeOnce(claimReady)
		case "/api/v1/agents/pairing/result":
			var req hubv1.SubmitPairingResultRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode pairing result: %v", err)
			}
			resultReady <- req.Result
			w.WriteHeader(http.StatusNoContent)
			cancel()
		default:
			http.NotFound(w, r)
		}
	}))
	defer hub.Close()

	manager := NewManager(remoteconfig.Config{
		Enabled:    true,
		DataDir:    t.TempDir(),
		DeviceName: "Pair Device",
		HubURL:     hub.URL,
	}, inventoryProviderStub{}, nil)
	manager.SetPairClaimer(pairClaimerFunc(func(ctx context.Context, req pairing.ClaimRequest) (pairing.ClaimResponse, error) {
		_ = ctx
		return pairManager.ClaimSession(req)
	}))

	go manager.hubPairingLoop(ctx, hub.URL, "device-pair", "agent-session-1")

	select {
	case <-claimReady:
	case <-time.After(time.Second):
		t.Fatal("manager did not poll pairing claim")
	}
	var result hubv1.PairingResult
	select {
	case result = <-resultReady:
	case <-time.After(time.Second):
		t.Fatal("manager did not submit pairing result")
	}
	if result.ClaimID != "claim-1" || result.MachineID != "device-pair" || result.Error != "" {
		t.Fatalf("pairing result = %+v", result)
	}
	var envelope cert.AppCertificateEnvelope
	if err := json.Unmarshal(result.AppCertificate, &envelope); err != nil {
		t.Fatalf("decode app certificate: %v", err)
	}
	if envelope.Payload.MachineID != "device-pair" || envelope.Payload.AppDeviceID != "appweb-pair" {
		t.Fatalf("certificate payload = %+v", envelope.Payload)
	}
	if err := cert.VerifyAppCertificate(envelope, machineKey.PublicKey, now.Add(time.Minute)); err != nil {
		t.Fatalf("certificate does not verify: %v", err)
	}
}

func TestHubPairingLoopRetriesPendingResultAfterSubmitFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	claim := hubv1.PairingClaim{
		ClaimID:               "claim-retry",
		MachineID:             "device-pair",
		PairSessionID:         "pair-session-1",
		PairSecret:            "pair-secret-1",
		AppDeviceID:           "appweb-pair",
		AppName:               "TermX Remote App",
		AppPublicKey:          base64.StdEncoding.EncodeToString([]byte("app-public")),
		RequestedCapabilities: []string{"terminal"},
	}
	var pollCount atomic.Int32
	var submitCount atomic.Int32
	resultReady := make(chan hubv1.PairingResult, 1)
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/agents/pairing/poll":
			if pollCount.Add(1) == 1 {
				writeTestJSON(w, map[string]any{"claim": claim})
				return
			}
			t.Fatalf("manager polled a new pairing claim before retrying pending result")
		case "/api/v1/agents/pairing/result":
			var req hubv1.SubmitPairingResultRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode pairing result: %v", err)
			}
			if submitCount.Add(1) == 1 {
				http.Error(w, "temporary submit failure", http.StatusInternalServerError)
				return
			}
			resultReady <- req.Result
			w.WriteHeader(http.StatusNoContent)
			cancel()
		default:
			http.NotFound(w, r)
		}
	}))
	defer hub.Close()

	manager := NewManager(remoteconfig.Config{
		Enabled:    true,
		DataDir:    t.TempDir(),
		DeviceName: "Pair Device",
		HubURL:     hub.URL,
	}, inventoryProviderStub{}, nil)
	claimCalls := atomic.Int32{}
	manager.SetPairClaimer(pairClaimerFunc(func(ctx context.Context, req pairing.ClaimRequest) (pairing.ClaimResponse, error) {
		_ = ctx
		claimCalls.Add(1)
		if req.PairSessionID != claim.PairSessionID || req.PairSecret != claim.PairSecret {
			t.Fatalf("claim request = %+v", req)
		}
		return pairing.ClaimResponse{
			MachineID:        claim.MachineID,
			MachineName:      "Pair Device",
			MachinePublicKey: "machine-public",
			AppCertificate: cert.AppCertificateEnvelope{
				Payload: cert.AppCertificatePayload{
					MachineID:    claim.MachineID,
					AppDeviceID:  claim.AppDeviceID,
					Capabilities: []string{"terminal"},
					ExpiresAt:    time.Date(2027, 5, 4, 10, 30, 0, 0, time.UTC),
				},
			},
		}, nil
	}))

	go manager.hubPairingLoop(ctx, hub.URL, "device-pair", "agent-session-1")

	var result hubv1.PairingResult
	select {
	case result = <-resultReady:
	case <-time.After(4 * time.Second):
		t.Fatal("manager did not retry pending pairing result")
	}
	if submitCount.Load() != 2 {
		t.Fatalf("submit count = %d, want 2", submitCount.Load())
	}
	if claimCalls.Load() != 1 {
		t.Fatalf("claim calls = %d, want 1", claimCalls.Load())
	}
	if result.ClaimID != "claim-retry" || result.MachineID != "device-pair" || result.Error != "" || len(result.AppCertificate) == 0 {
		t.Fatalf("pairing result = %+v", result)
	}
}

func TestManagerRejectsCloudOfferWithoutCertificateBeforeAnswering(t *testing.T) {
	manager, answerer, offer := newCloudOfferFixture(t)
	offer.AppCertificate = nil

	answer := manager.answerCloudOffer(context.Background(), offer, nil)

	if answer.Error == "" || !strings.Contains(answer.Error, "app certificate") {
		t.Fatalf("expected app certificate rejection, got %#v", answer)
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

func TestManagerVerifiesCloudOfferSignatureAndRejectsReplay(t *testing.T) {
	manager, answerer, offer := newCloudOfferFixture(t)

	answer := manager.answerCloudOffer(context.Background(), offer, []hubv1.RTCIceServerConfig{
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

	replayed := manager.answerCloudOffer(context.Background(), offer, nil)
	if replayed.Error == "" || !strings.Contains(replayed.Error, "nonce") {
		t.Fatalf("expected replay rejection, got %#v", replayed)
	}
	if answerer.calls != 1 {
		t.Fatalf("replayed offer reached answerer, calls=%d", answerer.calls)
	}
}

func TestManagerCopiesRelayTransferPolicyIntoAnswerOptions(t *testing.T) {
	manager, answerer, offer := newCloudOfferFixture(t)
	offer.AllowRelay = true
	offer.AllowRelayTransfer = true

	answer := manager.answerCloudOffer(context.Background(), offer, nil)

	if answer.Error != "" {
		t.Fatalf("valid relay-transfer offer returned error: %#v", answer)
	}
	if answerer.calls != 1 {
		t.Fatalf("answerer calls = %d, want 1", answerer.calls)
	}
	if !answerer.gotOptions.ChannelPolicy.AllowRelayTransfer {
		t.Fatalf("relay transfer policy was not passed to answer options: %#v", answerer.gotOptions.ChannelPolicy)
	}
}

func TestManagerRejectsTamperedCloudOfferSignatureBeforeAnswering(t *testing.T) {
	manager, answerer, offer := newCloudOfferFixture(t)
	offer.SDP = "v=0\r\ns=tampered-offer\r\n"

	answer := manager.answerCloudOffer(context.Background(), offer, nil)

	if answer.Error == "" || !strings.Contains(answer.Error, "signature") {
		t.Fatalf("expected signature rejection, got %#v", answer)
	}
	if answerer.calls != 0 {
		t.Fatalf("tampered offer reached answerer, calls=%d", answerer.calls)
	}
}

func TestManagerRejectsTamperedCloudOfferCandidatesBeforeAnswering(t *testing.T) {
	manager, answerer, offer := newCloudOfferFixture(t)
	offer.ICECandidates = append(offer.ICECandidates, "candidate:tampered")

	answer := manager.answerCloudOffer(context.Background(), offer, nil)

	if answer.Error == "" || !strings.Contains(answer.Error, "signature") {
		t.Fatalf("expected signature rejection, got %#v", answer)
	}
	if answerer.calls != 0 {
		t.Fatalf("tampered candidate offer reached answerer, calls=%d", answerer.calls)
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

func TestManagerAllowsMachineScopedCloudOfferWithTerminalCapabilityOnly(t *testing.T) {
	manager, answerer, offer := newCloudOfferFixtureWithCapabilitiesAndTerminal(t, []string{"terminal"}, managementProviderStub{}, "")

	answer := manager.answerCloudOffer(context.Background(), offer, nil)

	if answer.Error != "" {
		t.Fatalf("machine-scoped terminal capability should allow runtime connection, got %#v", answer)
	}
	if !answerer.gotOptions.ChannelPolicy.AllowTerminal || !answerer.gotOptions.ChannelPolicy.AllowAPI || answerer.gotOptions.ChannelPolicy.AllowTerminalManagement {
		t.Fatalf("terminal-only machine-scoped policy = %#v", answerer.gotOptions.ChannelPolicy)
	}
}

func TestManagerScopesCloudOfferPolicyToCertificateCapabilities(t *testing.T) {
	manager, answerer, offer := newCloudOfferFixtureWithCapabilities(t, []string{"terminal"}, nil)

	answer := manager.answerCloudOffer(context.Background(), offer, nil)

	if answer.Error != "" {
		t.Fatalf("valid terminal-only offer returned error: %#v", answer)
	}
	policy := answerer.gotOptions.ChannelPolicy
	if !policy.AllowTerminal || !policy.AllowEvents {
		t.Fatalf("terminal capability should allow terminal/events, got %#v", policy)
	}
	if !policy.AllowAPI || policy.AllowFileManager || policy.AllowTerminalManagement {
		t.Fatalf("terminal-only certificate overgranted channel policy: %#v", policy)
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
		t.Fatalf("terminal-only certificate overgranted terminal management: %#v", answerer.gotOptions.ChannelPolicy)
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

func TestManagerUsesOfferScopedCloudRTCConfig(t *testing.T) {
	manager, answerer, offer := newCloudOfferFixture(t)
	registrationICE := []hubv1.RTCIceServerConfig{{URLs: []string{"stun:registration.example:3478"}}}
	offer.RTCConfig.IceServers = []hubv1.RTCIceServerConfig{
		{URLs: []string{"stun:cloud.example:3478"}},
		{URLs: []string{"turn:cloud.example:3478?transport=udp"}, Username: "lease:1770000000", Credential: "turn-credential"},
	}
	offer.AllowRelay = true

	answer := manager.answerCloudOffer(context.Background(), offer, registrationICE)
	if answer.Error != "" {
		t.Fatalf("valid cloud relay offer returned error: %#v", answer)
	}
	if len(answerer.gotICE) != 2 || answerer.gotICE[1].Username != "lease:1770000000" {
		t.Fatalf("answerer ICE servers = %+v", answerer.gotICE)
	}
	if answerer.gotOffer.RTCConfig.IceServers[1].Credential != "turn-credential" {
		t.Fatalf("offer rtc config was not preserved: %+v", answerer.gotOffer.RTCConfig.IceServers)
	}
}

func TestHubSignalingLoopResetsSessionWhenAnswerSubmitUnauthorized(t *testing.T) {
	manager, _, offer := newCloudOfferFixture(t)
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
		manager.hubSignalingLoop(ctx, hub.URL, offer.DeviceID, "agent-session-1", nil)
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

func TestHubSignalingLoopKeepsActiveSessionContextWhenAnswerSubmitUnauthorized(t *testing.T) {
	manager, answerer, offer := newCloudOfferFixture(t)
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

	manager.host = transportSinkStub{}
	manager.cfg.HubURL = hub.URL
	manager.mu.Lock()
	manager.hubStateLocked(hub.URL).SessionID = "agent-session-1"
	manager.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	manager.ensureHubSignalingLoop(ctx, hub.URL)
	waitForCondition(t, time.Second, func() bool {
		return answerRequests.Load() == 1
	})
	sessionCtx := answerer.gotOptions.SessionContext
	if sessionCtx == nil {
		t.Fatal("cloud answer should receive a hub-scoped session context")
	}
	select {
	case <-manager.syncCh:
	case <-time.After(time.Second):
		t.Fatal("expected ordinary unauthorized answer submit to trigger sync")
	}
	select {
	case <-sessionCtx.Done():
		t.Fatal("ordinary unauthorized should not cancel active RTC session context")
	default:
	}
}

func TestHubSignalingLoopStopsWithoutSyncWhenPollForcedOffline(t *testing.T) {
	manager, _, offer := newCloudOfferFixture(t)
	var pollRequests atomic.Int32
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/agents/signaling/poll":
			pollRequests.Add(1)
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"poll_cloud_offer_failed","message":"agent forced offline"}`))
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
		manager.hubSignalingLoop(ctx, hub.URL, offer.DeviceID, "agent-session-1", nil)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("expected signaling loop to stop after forced offline")
	}
	if pollRequests.Load() != 1 {
		t.Fatalf("expected one poll before forced offline stop, got %d", pollRequests.Load())
	}
	select {
	case <-manager.syncCh:
		t.Fatal("forced offline must not trigger automatic hub re-registration")
	default:
	}
}

func TestHubSignalingLoopCancelsActiveSessionContextWhenForcedOffline(t *testing.T) {
	manager, answerer, offer := newCloudOfferFixture(t)
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
			_, _ = w.Write([]byte(`{"error":"submit_cloud_answer_failed","message":"agent forced offline"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer hub.Close()

	manager.host = transportSinkStub{}
	manager.cfg.HubURL = hub.URL
	manager.mu.Lock()
	manager.hubStateLocked(hub.URL).SessionID = "agent-session-1"
	manager.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	manager.ensureHubSignalingLoop(ctx, hub.URL)
	waitForCondition(t, time.Second, func() bool {
		return answerRequests.Load() == 1
	})
	sessionCtx := answerer.gotOptions.SessionContext
	if sessionCtx == nil {
		t.Fatal("cloud answer should receive a hub-scoped session context")
	}
	select {
	case <-sessionCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("forced offline should cancel active RTC session context")
	}
	select {
	case <-manager.syncCh:
		t.Fatal("forced offline must not trigger automatic hub re-registration")
	default:
	}
}

func TestHubSignalingLoopMarksDegradedWhenAnswerSubmitFails(t *testing.T) {
	manager, _, offer := newCloudOfferFixture(t)
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
	go manager.hubSignalingLoop(ctx, hub.URL, offer.DeviceID, "agent-session-1", nil)

	waitForCondition(t, time.Second, func() bool {
		status := manager.Status()
		return answerRequests.Load() == 1 &&
			status.State == StateDegraded &&
			strings.Contains(status.Detail, "submit hub answer")
	})
}

func TestHubSignalingLoopRetriesOriginalAnswerAfterTransientSubmitFailure(t *testing.T) {
	manager, answerer, offer := newCloudOfferFixture(t)
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
	go manager.hubSignalingLoop(ctx, hub.URL, offer.DeviceID, "agent-session-1", nil)

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
	manager, answerer, offer := newCloudOfferFixture(t)
	changedOffer := offer
	changedOffer.SDP = "v=0\r\ns=changed-offer\r\n"
	testHubSignalingLoopDoesNotRetryCachedAnswerForChangedOffer(t, manager, answerer, offer, changedOffer)
}

func TestHubSignalingLoopUsesHubScopedAnswerOptions(t *testing.T) {
	localManager, localAnswerer, localOffer := newCloudOfferFixture(t)
	localOptions := runSingleOfferSignalingLoop(t, localManager, localAnswerer, localOffer, func(hubURL string) {
		localManager.AddHubURLWithAnswerOptions(hubURL, remotertc.AnswerOptions{SettingEngine: noopSettingEngine{}})
	})
	if localOptions.SettingEngine == nil {
		t.Fatal("local hub signaling loop did not receive its scoped ICE TCP setting engine")
	}

	cloudManager, cloudAnswerer, cloudOffer := newCloudOfferFixture(t)
	cloudOptions := runSingleOfferSignalingLoop(t, cloudManager, cloudAnswerer, cloudOffer, func(hubURL string) {
		cloudManager.AddHubURLWithAnswerOptions("http://local.example.test", remotertc.AnswerOptions{SettingEngine: noopSettingEngine{}})
		cloudManager.AddHubURL(hubURL)
	})
	if cloudOptions.SettingEngine != nil {
		t.Fatal("cloud hub signaling loop inherited the local hub ICE TCP setting engine")
	}
}

func TestHubSignalingLoopDoesNotRetryCachedAnswerWhenRawOfferWhitespaceChanges(t *testing.T) {
	manager, answerer, offer := newCloudOfferFixture(t)
	changedOffer := offer
	changedOffer.SDP = offer.SDP + " "
	testHubSignalingLoopDoesNotRetryCachedAnswerForChangedOffer(t, manager, answerer, offer, changedOffer)
}

func testHubSignalingLoopDoesNotRetryCachedAnswerForChangedOffer(t *testing.T, manager *Manager, answerer *cloudAnswererStub, offer hubv1.SignalingOffer, changedOffer hubv1.SignalingOffer) {
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
	go manager.hubSignalingLoop(ctx, hub.URL, offer.DeviceID, "agent-session-1", nil)

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
	opts remotertc.AnswerOptions,
) (hubv1.SignalingAnswer, error) {
	s.calls++
	s.gotOffer = offer
	s.gotICE = append([]hubv1.RTCIceServerConfig(nil), iceServers...)
	s.gotOptions = opts
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

func runSingleOfferSignalingLoop(t *testing.T, manager *Manager, answerer *cloudAnswererStub, offer hubv1.SignalingOffer, configure func(hubURL string)) remotertc.AnswerOptions {
	t.Helper()
	submitted := make(chan hubv1.SignalingAnswer, 1)
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
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer hub.Close()

	manager.host = transportSinkStub{}
	configure(hub.URL)
	manager.mu.Lock()
	state := manager.hubStateLocked(hub.URL)
	state.SessionID = "agent-session-1"
	manager.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager.ensureHubSignalingLoop(ctx, hub.URL)
	select {
	case answer := <-submitted:
		cancel()
		if answer.Error != "" {
			t.Fatalf("expected successful answer, got %#v", answer)
		}
	case <-time.After(time.Second):
		t.Fatal("manager did not submit signaling answer")
	}
	if answerer.calls != 1 {
		t.Fatalf("answerer calls = %d, want 1", answerer.calls)
	}
	return answerer.gotOptions
}

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
		CertID:       "cert-cloud-test",
		MachineID:    "device-cloud-test",
		AppDeviceID:  "app-device-cloud-test",
		AppPublicKey: base64.StdEncoding.EncodeToString(appPublic),
		AppName:      "Cloud Test App",
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
		SessionID:      "rtc-cloud-test",
		TicketID:       "ticket-cloud-test",
		DeviceID:       "device-cloud-test",
		TerminalID:     terminalID,
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
		Nonce:      "nonce-cloud-test",
		Timestamp:  now,
	}
	offer.Signature = hubv1.OfferSignature{
		Algorithm: "ed25519",
		Nonce:     signatureFields.Nonce,
		Timestamp: signatureFields.Timestamp.Unix(),
		Value:     base64.StdEncoding.EncodeToString(ed25519.Sign(appPrivate, remotertc.CanonicalOfferSignatureMessage(signatureFields))),
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

type certFixtureOptions struct {
	MachineID string
	IssuedAt  time.Time
	ExpiresAt time.Time
}

func resignCloudOfferWithCert(t *testing.T, manager *Manager, offer hubv1.SignalingOffer, opts certFixtureOptions) hubv1.SignalingOffer {
	t.Helper()
	machineKey, err := identity.LoadOrCreateMachineKey(manager.cfg.DataDir)
	if err != nil {
		t.Fatalf("LoadOrCreateMachineKey returned error: %v", err)
	}
	appPublic, appPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey returned error: %v", err)
	}
	machineID := strings.TrimSpace(opts.MachineID)
	if machineID == "" {
		machineID = manager.identity.DeviceID
	}
	now := time.Now().UTC().Round(0)
	issuedAt := opts.IssuedAt
	if issuedAt.IsZero() {
		issuedAt = now.Add(-time.Minute)
	}
	expiresAt := opts.ExpiresAt
	if expiresAt.IsZero() {
		expiresAt = now.Add(time.Hour)
	}
	envelope, err := cert.SignAppCertificate(cert.AppCertificatePayload{
		Version:      1,
		CertID:       "cert-resigned-test",
		MachineID:    machineID,
		AppDeviceID:  "app-device-resigned-test",
		AppPublicKey: base64.StdEncoding.EncodeToString(appPublic),
		AppName:      "Resigned Test App",
		Capabilities: []string{"terminal", "file_manager"},
		IssuedAt:     issuedAt,
		ExpiresAt:    expiresAt,
	}, machineKey)
	if err != nil {
		t.Fatalf("SignAppCertificate returned error: %v", err)
	}
	certificateJSON, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal certificate: %v", err)
	}
	offer.AppCertificate = certificateJSON
	signatureFields := remotertc.OfferSignatureFields{
		TicketID:   offer.TicketID,
		MachineID:  offer.DeviceID,
		TerminalID: offer.TerminalID,
		SDP:        offer.SDP,
		Candidates: offer.ICECandidates,
		Nonce:      "nonce-resigned-test-" + machineID,
		Timestamp:  now,
	}
	offer.Signature = hubv1.OfferSignature{
		Algorithm: "ed25519",
		Nonce:     signatureFields.Nonce,
		Timestamp: signatureFields.Timestamp.Unix(),
		Value:     base64.StdEncoding.EncodeToString(ed25519.Sign(appPrivate, remotertc.CanonicalOfferSignatureMessage(signatureFields))),
	}
	return offer
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

type pairClaimerFunc func(context.Context, pairing.ClaimRequest) (pairing.ClaimResponse, error)

func (f pairClaimerFunc) ClaimPairSession(ctx context.Context, req pairing.ClaimRequest) (pairing.ClaimResponse, error) {
	return f(ctx, req)
}

func writeTestJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

func closeOnce(ch chan struct{}) {
	select {
	case <-ch:
	default:
		close(ch)
	}
}
