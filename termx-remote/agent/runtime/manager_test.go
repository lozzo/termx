package runtime

import (
	"context"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-remote/bridge"
	remoteconfig "github.com/lozzow/termx/termx-remote/config"
	"github.com/lozzow/termx/termx-remote/fileapi"
	"github.com/lozzow/termx/termx-remote/identity"
	"github.com/lozzow/termx/termx-remote/pairing"
	pb "github.com/lozzow/termx/termx-remote/protocol/hubgrpc"
	hubv1 "github.com/lozzow/termx/termx-remote/protocol/hubv1"
	"github.com/lozzow/termx/termx-remote/protocol/runtimepb"
	remotertc "github.com/lozzow/termx/termx-remote/session/rtc"
	"github.com/lozzow/termx/termx-remote/session/token"
	"github.com/lozzow/termx/termx-shared/transport"
	"github.com/pion/webrtc/v4"
	"google.golang.org/protobuf/proto"
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

func TestManagerStartUsesGRPCHubLoopForLocalHub(t *testing.T) {
	mgr := NewManager(remoteconfig.Config{
		Enabled:    true,
		DataDir:    t.TempDir(),
		DeviceName: "device-b",
		HubURL:     "http://127.0.0.1:1",
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
	if status.State != StateRegistering {
		t.Fatalf("expected registering state before register ack, got %q detail=%q", status.State, status.Detail)
	}
	mgr.mu.RLock()
	state := mgr.hubStateLocked("http://127.0.0.1:1")
	mgr.mu.RUnlock()
	if state.SessionCancel == nil {
		t.Fatal("expected local hub to use gRPC signaling loop")
	}
	if len(status.Hubs) != 1 ||
		status.Hubs[0].Kind != HubKindLocal ||
		status.Hubs[0].Source != HubSourceConfig ||
		status.Hubs[0].Transport != HubTransportGRPC ||
		status.Hubs[0].State != HubConnectionConnecting {
		t.Fatalf("unexpected hub status before ack: %+v", status.Hubs)
	}

	mgr.updateFromGRPCRegisterAck("http://127.0.0.1:1", &pb.RegisterResponse{
		AgentSessionId: "agent-session-1",
		RelayPolicy:    &pb.RelayPolicy{AllowRelay: true},
	})
	stateValue, detail, err := mgr.reconcile(context.Background())
	if err != nil {
		t.Fatalf("reconcile after register ack: %v", err)
	}
	mgr.setStatus(stateValue, detail)
	status = mgr.Status()
	if status.State != StateOnline {
		t.Fatalf("expected online state after register ack, got %q detail=%q", status.State, status.Detail)
	}
	if len(status.Hubs) != 1 ||
		status.Hubs[0].State != HubConnectionConnected ||
		status.Hubs[0].LastAckAt.IsZero() ||
		!status.Hubs[0].AllowRelay {
		t.Fatalf("unexpected hub status after ack: %+v", status.Hubs)
	}
	mgr.Close()
}

func TestManagerCloseAllowsRestart(t *testing.T) {
	mgr := NewManager(remoteconfig.Config{
		Enabled:    true,
		DataDir:    t.TempDir(),
		DeviceName: "restart-device",
	}, nil, nil)
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("initial Start returned error: %v", err)
	}
	mgr.Close()
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("restart Start returned error: %v", err)
	}
	status := mgr.Status()
	if status.State != StateConfigured {
		t.Fatalf("expected configured state after restart, got %+v", status)
	}
	mgr.Close()
}

func TestDetachHubIgnoresEmptyURL(t *testing.T) {
	mgr := NewManager(remoteconfig.Config{
		Enabled:    true,
		DataDir:    t.TempDir(),
		DeviceName: "detach-device",
		HubURLs:    []string{"http://127.0.0.1:1", "http://127.0.0.1:2"},
	}, nil, nil)

	mgr.DetachHub("")

	mgr.mu.RLock()
	hubURLs := append([]string(nil), mgr.cfg.HubURLs...)
	mgr.mu.RUnlock()
	if !reflect.DeepEqual(hubURLs, []string{"http://127.0.0.1:1", "http://127.0.0.1:2"}) {
		t.Fatalf("empty detach changed hub URLs: %+v", hubURLs)
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
		Body:   mustMarshalRuntimeProto(t, &runtimepb.TerminalCreateRequest{Command: []string{"/bin/sh"}, Name: "cloud shell"}),
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
		AppDeviceID:           "app-device-pair",
		AppName:               "TermX Pair App",
		RequestedCapabilities: []string{"terminal", "terminal_management"},
	})
	if result.ClaimID != "claim-1" || result.MachineID != "device-pair" || result.MachineName != "Pair Device" || result.Error != "" {
		t.Fatalf("pairing result = %+v", result)
	}
	claims, err := token.Verify(result.SessionToken, machineSecret, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("session token did not verify: %v", err)
	}
	if claims.MachineID != "device-pair" || len(claims.Capabilities) != 0 || len(claims.Paths) != 0 {
		t.Fatalf("claims = %+v", claims)
	}
	if claims.AppDeviceID != "app-device-pair" || claims.AppName != "TermX Pair App" {
		t.Fatalf("app claims = %+v", claims)
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

func TestManagerDeepCopiesOfferICEServersBeforeAnswerer(t *testing.T) {
	manager, answerer, offer := newCloudOfferFixture(t)
	iceServers := []hubv1.RTCIceServerConfig{{
		URLs:       []string{"stun:stun.example.test:3478"},
		Username:   "user-1",
		Credential: "pass-1",
	}}

	answer := manager.answerCloudOffer(context.Background(), offer, iceServers)
	if answer.Error != "" {
		t.Fatalf("valid token offer returned error: %#v", answer)
	}
	iceServers[0].URLs[0] = "stun:mutated.example.test:3478"
	if got := answerer.gotICE[0].URLs[0]; got != "stun:stun.example.test:3478" {
		t.Fatalf("answerer ICE URL was aliased to caller slice: %q", got)
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

func TestManagerAcceptsCloudOfferWithMachineScopedSessionToken(t *testing.T) {
	manager, answerer, offer := newCloudOfferFixtureWithPaths(t, []string{"local"})

	answer := manager.answerCloudOffer(context.Background(), offer, nil)

	if answer.Error != "" {
		t.Fatalf("machine-scoped token offer returned error: %#v", answer)
	}
	if answerer.calls != 1 {
		t.Fatalf("answerer calls = %d, want 1", answerer.calls)
	}
}

func TestManagerAcceptsLocalOfferWithLocalOnlySessionToken(t *testing.T) {
	manager, answerer, offer := newCloudOfferFixtureWithPaths(t, []string{"local"})
	offer.Path = "local"

	answer := manager.answerCloudOffer(context.Background(), offer, nil)

	if answer.Error != "" {
		t.Fatalf("local token offer returned error: %#v", answer)
	}
	if answerer.calls != 1 {
		t.Fatalf("answerer calls = %d, want 1", answerer.calls)
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

func TestManagerAcceptsCloudOfferWithoutTerminalCapability(t *testing.T) {
	manager, answerer, offer := newCloudOfferFixtureWithCapabilities(t, []string{"file_manager"}, nil)

	answer := manager.answerCloudOffer(context.Background(), offer, nil)

	if answer.Error != "" {
		t.Fatalf("capabilities should not reject cloud offer: %#v", answer)
	}
	if answerer.calls != 1 {
		t.Fatalf("answerer calls = %d, want 1", answerer.calls)
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
	if !policy.AllowTerminalInventory {
		t.Fatalf("machine-scoped terminal list offer should allow terminal inventory API: %#v", policy)
	}
	if !policy.AllowTerminalManagement {
		t.Fatalf("machine-scoped terminal list offer should allow terminal management API: %#v", policy)
	}
}

func TestManagerDoesNotScopeCloudOfferPolicyToSessionTokenCapabilities(t *testing.T) {
	manager, answerer, offer := newCloudOfferFixtureWithCapabilities(t, []string{"terminal"}, nil)

	answer := manager.answerCloudOffer(context.Background(), offer, nil)

	if answer.Error != "" {
		t.Fatalf("valid terminal-only offer returned error: %#v", answer)
	}
	policy := answerer.gotOptions.ChannelPolicy
	if !policy.AllowTerminal || !policy.AllowEvents || !policy.AllowAPI || !policy.AllowFileManager || !policy.AllowRelayTransfer {
		t.Fatalf("runtime channels should be allowed regardless of token capabilities: %#v", policy)
	}
}

func TestManagerAllowsCloudTerminalInventoryForMachineScopedTerminalToken(t *testing.T) {
	provider := managementProviderStub{inventoryProviderStub: inventoryProviderStub{
		items: []TerminalInventoryItem{{ID: "term-1", Name: "shell", State: "running"}},
	}}
	manager, answerer, offer := newCloudOfferFixtureWithCapabilitiesAndTerminal(t, []string{"terminal"}, provider, "")

	answer := manager.answerCloudOffer(context.Background(), offer, nil)
	if answer.Error != "" {
		t.Fatalf("valid machine-scoped terminal-only offer returned error: %#v", answer)
	}
	if !answerer.gotOptions.ChannelPolicy.AllowTerminalInventory {
		t.Fatalf("machine-scoped terminal token should allow terminal inventory: %#v", answerer.gotOptions.ChannelPolicy)
	}
	if !answerer.gotOptions.ChannelPolicy.AllowTerminalManagement {
		t.Fatalf("terminal management should not depend on token capabilities: %#v", answerer.gotOptions.ChannelPolicy)
	}
}

func TestManagerAllowsCloudTerminalManagementWhenRouterExists(t *testing.T) {
	provider := managementProviderStub{inventoryProviderStub: inventoryProviderStub{
		items: []TerminalInventoryItem{{ID: "term-1", Name: "shell", State: "running"}},
	}}
	manager, answerer, offer := newCloudOfferFixtureWithCapabilities(t, []string{"terminal"}, provider)

	answer := manager.answerCloudOffer(context.Background(), offer, nil)
	if answer.Error != "" {
		t.Fatalf("valid terminal-only offer returned error: %#v", answer)
	}
	if !answerer.gotOptions.ChannelPolicy.AllowTerminalManagement {
		t.Fatalf("router should allow terminal management independent of capability: %#v", answerer.gotOptions.ChannelPolicy)
	}
	if answerer.gotOptions.Events == nil {
		t.Fatal("cloud runtime should provide machine event router when provider supports it")
	}
	if answerer.gotOptions.Storage == nil {
		t.Fatal("cloud runtime should provide storage router when provider supports it")
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
	return http.StatusOK, marshalRuntimeProtoForStub(&runtimepb.TerminalInventoryItem{TerminalId: "cloud-terminal-1"}), ""
}

func (s managementProviderStub) RouteStorageRequest(_ context.Context, req remotertc.StorageRequest) (int32, []byte, string) {
	if req.Path != "/storage/list" {
		return http.StatusNotFound, nil, "unknown storage route"
	}
	return http.StatusOK, marshalRuntimeProtoForStub(&runtimepb.StorageListResponse{}), ""
}

func (s managementProviderStub) SubscribeRemoteEvents(ctx context.Context, _ remotertc.EventFilters) (<-chan []byte, func(), error) {
	ch := make(chan []byte, 1)
	ch <- marshalRuntimeProtoForStub(&runtimepb.EventEnvelope{Type: "event"})
	close(ch)
	return ch, func() { _ = ctx }, nil
}

func mustMarshalRuntimeProto(t *testing.T, msg proto.Message) []byte {
	t.Helper()
	data, err := proto.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal runtime proto: %v", err)
	}
	return data
}

func marshalRuntimeProtoForStub(msg proto.Message) []byte {
	data, err := proto.Marshal(msg)
	if err != nil {
		panic(err)
	}
	return data
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
	return newCloudOfferFixtureWithCapabilitiesTerminalAndPaths(t, capabilities, provider, terminalID, []string{"hub"})
}

func newCloudOfferFixtureWithPaths(t *testing.T, paths []string) (*Manager, *cloudAnswererStub, hubv1.SignalingOffer) {
	t.Helper()
	return newCloudOfferFixtureWithCapabilitiesTerminalAndPaths(t, []string{"terminal", "file_manager"}, nil, "term-1", paths)
}

func newCloudOfferFixtureWithCapabilitiesTerminalAndPaths(t *testing.T, capabilities []string, provider InventoryProvider, terminalID string, paths []string) (*Manager, *cloudAnswererStub, hubv1.SignalingOffer) {
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
		Paths:        append([]string(nil), paths...),
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
