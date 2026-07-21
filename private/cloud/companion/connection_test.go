package companion_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/muxvia/muxvia/private/cloud/companion"
	"github.com/muxvia/muxvia/private/cloud/companion/cloudservice"
	"github.com/muxvia/muxvia/private/cloud/companion/session"
	"github.com/muxvia/muxvia/proto/cloudpb"
	"github.com/muxvia/muxvia/shared/cloudcompanion"
	"google.golang.org/protobuf/proto"
)

type testCredentialStore struct {
	mu      sync.Mutex
	secrets map[string][]byte
}

func (store *testCredentialStore) LoadSecret(_ context.Context, key string) ([]byte, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	value, ok := store.secrets[key]
	if !ok {
		return nil, session.ErrNotFound
	}
	return append([]byte(nil), value...), nil
}

func (store *testCredentialStore) StoreSecret(_ context.Context, key string, value []byte) error {
	store.mu.Lock()
	store.secrets[key] = append([]byte(nil), value...)
	store.mu.Unlock()
	return nil
}

func (store *testCredentialStore) DeleteSecret(_ context.Context, key string) error {
	store.mu.Lock()
	delete(store.secrets, key)
	store.mu.Unlock()
	return nil
}

type fakeControlPlane struct {
	mu  sync.Mutex
	now time.Time

	lastAuthorization string
	resolveResponse   *cloudpb.ResolvedEndpoint
	resolveErr        error
	planResponse      *cloudpb.ManagedRoutePlan
	planCount         int
	qualityCount      int
	outcomeCount      int
	refreshCount      int
	refreshErr        error
}

func (controlPlane *fakeControlPlane) BeginLogin(_ context.Context, _ *cloudpb.BeginLoginRequest) (*cloudpb.LoginFlow, error) {
	return &cloudpb.LoginFlow{FlowId: "login-1", VerificationUri: "https://login.example.test/device", UserCode: "ABCD-EFGH", ExpiresAtUnix: uint64(controlPlane.now.Add(5 * time.Minute).Unix()), PollIntervalMillis: 1000}, nil
}

func (controlPlane *fakeControlPlane) CompleteLogin(_ context.Context, _ *cloudpb.CompleteLoginRequest) (session.Session, error) {
	return session.New(session.Metadata{Kind: session.KindAccount, AccountID: "account-login", AccountLabel: "Login User", DeviceID: "client-login", ExpiresAt: controlPlane.now.Add(time.Hour)}, []byte("new-account-token"), controlPlane.now)
}

func (controlPlane *fakeControlPlane) BeginDeviceEnrollment(_ context.Context, _ *cloudpb.BeginDeviceEnrollmentRequest) (*cloudpb.DeviceEnrollmentChallenge, error) {
	return &cloudpb.DeviceEnrollmentChallenge{FlowId: "enroll-1", ChallengeId: "challenge-1", Challenge: bytes.Repeat([]byte{0x33}, 32), ExpiresAtUnix: uint64(controlPlane.now.Add(5 * time.Minute).Unix())}, nil
}

func (controlPlane *fakeControlPlane) CompleteDeviceEnrollment(_ context.Context, _ *cloudpb.CompleteDeviceEnrollmentRequest) (cloudservice.DeviceEnrollmentResult, error) {
	stored, err := session.New(session.Metadata{Kind: session.KindDevice, AccountID: "account-1", DeviceID: "daemon-enrolled", ExpiresAt: controlPlane.now.Add(time.Hour), HubID: "hub-1", HubURL: "https://hub.example.test", HubRegion: "local", HubDirectoryVersion: 1}, []byte("new-device-token"), controlPlane.now)
	if err != nil {
		return cloudservice.DeviceEnrollmentResult{}, err
	}
	return cloudservice.DeviceEnrollmentResult{Session: stored, ControlEnrollment: &cloudpb.DaemonControlEnrollment{AccountId: "account-1", DaemonDeviceId: "daemon-enrolled", AuthEpoch: 1, EnrolledAtUnixMillis: controlPlane.now.UnixMilli(), VerificationKeys: []*cloudpb.DaemonControlVerificationKey{{KeyId: "control-1", PublicKey: bytes.Repeat([]byte{0x41}, 32), NotBeforeUnixMillis: controlPlane.now.Add(-time.Hour).UnixMilli(), NotAfterUnixMillis: controlPlane.now.Add(time.Hour).UnixMilli()}}}}, nil
}

func (controlPlane *fakeControlPlane) RefreshSession(_ context.Context, authorization session.RefreshAuthorization) (session.Session, error) {
	controlPlane.mu.Lock()
	controlPlane.refreshCount++
	err := controlPlane.refreshErr
	controlPlane.mu.Unlock()
	if err != nil {
		return session.Session{}, err
	}
	metadata := authorization.Metadata()
	metadata.ExpiresAt = controlPlane.now.Add(time.Hour)
	return session.NewRefreshable(metadata, []byte("refreshed-access-token"), bytes.Repeat([]byte{0x72}, 32), controlPlane.now.Add(24*time.Hour), controlPlane.now)
}

func TestAuthorizeProactivelyRotatesRefreshableSession(t *testing.T) {
	now, service, controlPlane, _, manager := testService(t)
	refreshable, err := session.NewRefreshable(session.Metadata{Kind: session.KindAccount, AccountID: "account-1", AccountLabel: "Alice", DeviceID: "client-1", ExpiresAt: now.Add(5 * time.Minute)}, []byte("expiring-access-token"), bytes.Repeat([]byte{0x61}, 32), now.Add(24*time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Save(context.Background(), refreshable, now); err != nil {
		t.Fatal(err)
	}
	connection := service.NewConnection()
	if _, err := connection.Hello(context.Background(), helloRequest(cloudpb.CallerRole_CALLER_ROLE_TUI, cloudpb.CompanionCapability_COMPANION_CAPABILITY_SIGNALING)); err != nil {
		t.Fatal(err)
	}
	if _, err := connection.ResolveEndpoint(context.Background(), &cloudpb.ResolveEndpointRequest{EndpointId: "cloud-prod", TargetDeviceId: "daemon-1"}); err != nil {
		t.Fatal(err)
	}
	controlPlane.mu.Lock()
	defer controlPlane.mu.Unlock()
	if controlPlane.refreshCount != 1 {
		t.Fatalf("refresh count = %d", controlPlane.refreshCount)
	}
}

func (controlPlane *fakeControlPlane) capture(authorization session.Authorization) {
	controlPlane.mu.Lock()
	controlPlane.lastAuthorization = string(authorization.Bytes())
	controlPlane.mu.Unlock()
}

func (controlPlane *fakeControlPlane) PlanManagedRoute(_ context.Context, authorization session.Authorization, _ *cloudpb.PlanManagedRouteRequest) (*cloudpb.ManagedRoutePlan, error) {
	controlPlane.capture(authorization)
	controlPlane.mu.Lock()
	controlPlane.planCount++
	controlPlane.mu.Unlock()
	return controlPlane.planResponse, nil
}

func (controlPlane *fakeControlPlane) ReportPathQuality(_ context.Context, authorization session.Authorization, _ *cloudpb.ReportPathQualityRequest) (*cloudpb.ReportPathQualityResponse, error) {
	controlPlane.capture(authorization)
	controlPlane.mu.Lock()
	controlPlane.qualityCount++
	controlPlane.mu.Unlock()
	return &cloudpb.ReportPathQualityResponse{}, nil
}

func (controlPlane *fakeControlPlane) ReportConnectionOutcome(_ context.Context, authorization session.Authorization, _ *cloudpb.ReportConnectionOutcomeRequest) (*cloudpb.ReportConnectionOutcomeResponse, error) {
	controlPlane.capture(authorization)
	controlPlane.mu.Lock()
	controlPlane.outcomeCount++
	controlPlane.mu.Unlock()
	return &cloudpb.ReportConnectionOutcomeResponse{}, nil
}

type fakeHub struct {
	mu                sync.Mutex
	lastAuthorization string
	presence          *presenceSource
	signaling         *signalingSource
	completed         int
	leaseResponse     *cloudpb.RelayLease
	resolveResponse   *cloudpb.ResolvedEndpoint
	resolveErr        error
}

func (hub *fakeHub) BeginPresence(_ context.Context, authorization session.Authorization, _ *cloudpb.BeginPresenceRequest) (*cloudpb.PresenceChallenge, error) {
	hub.mu.Lock()
	hub.lastAuthorization = string(authorization.Bytes())
	hub.mu.Unlock()
	return &cloudpb.PresenceChallenge{PresenceSessionId: "presence-1", ChallengeId: "presence-challenge-1", Challenge: bytes.Repeat([]byte{0x44}, 32), ExpiresAtUnix: uint64(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC).Unix())}, nil
}

func (hub *fakeHub) ResolveEndpoint(_ context.Context, _ session.Authorization, _ *cloudpb.ResolveEndpointRequest) (*cloudpb.ResolvedEndpoint, error) {
	return hub.resolveResponse, hub.resolveErr
}

func (hub *fakeHub) ListManagedDevices(_ context.Context, _ session.Authorization, _ *cloudpb.ListManagedDevicesRequest) (*cloudpb.ListManagedDevicesResponse, error) {
	return &cloudpb.ListManagedDevicesResponse{}, nil
}

func (hub *fakeHub) AcquireRelayLease(_ context.Context, _ session.Authorization, _ *cloudpb.AcquireRelayLeaseRequest) (*cloudpb.RelayLease, error) {
	return hub.leaseResponse, nil
}

func (hub *fakeHub) OpenPresence(_ context.Context, _ session.Authorization, _ *cloudpb.OpenPresenceRequest) (cloudservice.PresenceSource, error) {
	return hub.presence, nil
}

func (hub *fakeHub) CreateSignalingSession(_ context.Context, _ session.Authorization, _ *cloudpb.CreateSignalingSessionRequest) (cloudservice.SignalingSource, error) {
	return hub.signaling, nil
}

func (hub *fakeHub) CompleteSignalingOffer(_ context.Context, _ session.Authorization, _ *cloudpb.CompleteSignalingOfferRequest) (*cloudpb.CompleteSignalingOfferResponse, error) {
	hub.mu.Lock()
	hub.completed++
	hub.mu.Unlock()
	return &cloudpb.CompleteSignalingOfferResponse{}, nil
}

func (hub *fakeHub) ReportDaemonRuntime(_ context.Context, _ session.Authorization, request *cloudpb.ReportDaemonRuntimeRequest) (*cloudpb.ReportDaemonRuntimeResponse, error) {
	return &cloudpb.ReportDaemonRuntimeResponse{ReportId: request.GetReportId(), DaemonRuntimeGeneration: request.GetDaemonRuntimeGeneration(), AcceptedRegistryRevision: request.GetRegistryRevision()}, nil
}

func (hub *fakeHub) ReportDaemonCommandResult(_ context.Context, _ session.Authorization, request *cloudpb.ReportDaemonCommandResultRequest) (*cloudpb.ReportDaemonCommandResultResponse, error) {
	return &cloudpb.ReportDaemonCommandResultResponse{AcceptedCommandId: request.GetResult().GetCommandId()}, nil
}

type presenceSource struct {
	items  chan *cloudpb.PresenceEvent
	done   chan struct{}
	closed sync.Once
}

func newPresenceSource(capacity int) *presenceSource {
	return &presenceSource{items: make(chan *cloudpb.PresenceEvent, capacity), done: make(chan struct{})}
}

func (source *presenceSource) Receive(ctx context.Context) (*cloudpb.PresenceEvent, error) {
	select {
	case event := <-source.items:
		return event, nil
	case <-source.done:
		return nil, io.EOF
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (source *presenceSource) Close() error {
	source.closed.Do(func() { close(source.done) })
	return nil
}

type signalingSource struct {
	items  chan *cloudpb.SignalingEvent
	done   chan struct{}
	closed sync.Once
}

func newSignalingSource(capacity int) *signalingSource {
	return &signalingSource{items: make(chan *cloudpb.SignalingEvent, capacity), done: make(chan struct{})}
}

func (source *signalingSource) Receive(ctx context.Context) (*cloudpb.SignalingEvent, error) {
	select {
	case event := <-source.items:
		return event, nil
	case <-source.done:
		return nil, io.EOF
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (source *signalingSource) Close() error {
	source.closed.Do(func() { close(source.done) })
	return nil
}

func TestConnectionRequiresHelloAndNegotiatesRoleCapabilities(t *testing.T) {
	now, service, _, _, _ := testService(t)
	connection := service.NewConnection()
	if _, err := connection.Status(context.Background(), &cloudpb.StatusRequest{}); !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL) {
		t.Fatalf("Status before Hello error = %v", err)
	}
	response, err := connection.Hello(context.Background(), helloRequest(cloudpb.CallerRole_CALLER_ROLE_CLI,
		cloudpb.CompanionCapability_COMPANION_CAPABILITY_ACCOUNT_SESSION,
		cloudpb.CompanionCapability_COMPANION_CAPABILITY_SIGNALING,
	))
	if err != nil {
		t.Fatal(err)
	}
	if response.GetSelectedProtocol() != cloudcompanion.ProtocolVersionMax || len(response.GetResponseNonce()) != 32 {
		t.Fatalf("Hello response = %#v", response)
	}
	if got := response.GetSupportedCapabilities(); len(got) != 1 || got[0] != cloudpb.CompanionCapability_COMPANION_CAPABILITY_ACCOUNT_SESSION {
		t.Fatalf("CLI capabilities = %v", got)
	}
	status, err := connection.Status(context.Background(), &cloudpb.StatusRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if status.GetState() != cloudpb.CompanionState_COMPANION_STATE_READY || status.GetAccountId() != "account-1" || status.GetSessionExpiresAtUnix() != uint64(now.Add(time.Hour).Unix()) {
		t.Fatalf("Status = %#v", status)
	}
	if strings.Contains(status.String(), "account-access-token") {
		t.Fatal("Status leaked account access token")
	}
	if _, err := connection.Hello(context.Background(), helloRequest(cloudpb.CallerRole_CALLER_ROLE_CLI)); !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL) {
		t.Fatalf("second Hello error = %v", err)
	}
}

func TestManagedOperationsDrivePrivateAdapters(t *testing.T) {
	now, service, controlPlane, hub, _ := testService(t)
	connection := service.NewConnection()
	if _, err := connection.Hello(context.Background(), helloRequest(cloudpb.CallerRole_CALLER_ROLE_TUI,
		cloudpb.CompanionCapability_COMPANION_CAPABILITY_SIGNALING,
		cloudpb.CompanionCapability_COMPANION_CAPABILITY_RELAY_LEASE,
		cloudpb.CompanionCapability_COMPANION_CAPABILITY_SMART_ROUTE,
		cloudpb.CompanionCapability_COMPANION_CAPABILITY_PATH_QUALITY,
	)); err != nil {
		t.Fatal(err)
	}
	resolved, err := connection.ResolveEndpoint(context.Background(), &cloudpb.ResolveEndpointRequest{EndpointId: "cloud-prod", TargetDeviceId: "daemon-1"})
	if err != nil || resolved.GetManagedSessionId() != "managed-1" {
		t.Fatalf("ResolveEndpoint = (%v, %v)", resolved, err)
	}

	streamContext, cancelStream := context.WithCancel(context.Background())
	defer cancelStream()
	stream, err := connection.CreateSignalingSession(streamContext, &cloudpb.CreateSignalingSessionRequest{
		EndpointId: "cloud-prod", ManagedSessionId: "managed-1", TargetDeviceId: "daemon-1",
		OfferSdp: "offer-sdp", RoutePreference: cloudpb.RoutePreference_ROUTE_PREFERENCE_STANDARD_RELAY,
	})
	if err != nil {
		t.Fatal(err)
	}
	hub.signaling.items <- &cloudpb.SignalingEvent{Payload: &cloudpb.SignalingEvent_Answer{Answer: &cloudpb.SignalingAnswer{SignalingSessionId: "signal-1", Sdp: "answer-sdp"}}}
	event, err := stream.Receive()
	if err != nil || event.GetAnswer().GetSdp() != "answer-sdp" {
		t.Fatalf("signaling Receive = (%v, %v)", event, err)
	}

	lease, err := connection.AcquireRelayLease(context.Background(), &cloudpb.AcquireRelayLeaseRequest{
		ManagedSessionId: "managed-1", TargetDeviceId: "daemon-1", RoutePreference: cloudpb.RoutePreference_ROUTE_PREFERENCE_STANDARD_RELAY,
	})
	if err != nil || lease.GetLeaseId() != "lease-1" {
		t.Fatalf("AcquireRelayLease = (%v, %v)", lease, err)
	}
	plan, err := connection.PlanManagedRoute(context.Background(), &cloudpb.PlanManagedRouteRequest{
		EndpointId: "cloud-prod", ManagedSessionId: "managed-1", TargetDeviceId: "daemon-1",
		RoutePreference: cloudpb.RoutePreference_ROUTE_PREFERENCE_SMART_ROUTE,
	})
	if err != nil || plan.GetPlanId() != "plan-1" || plan.GetSelectionReason() != cloudpb.RouteSelectionReason_ROUTE_SELECTION_REASON_DIRECT_UNSTABLE {
		t.Fatalf("PlanManagedRoute = (%v, %v)", plan, err)
	}
	_, err = connection.ReportPathQuality(context.Background(), &cloudpb.ReportPathQualityRequest{Summary: &cloudpb.PathQualitySummary{
		ManagedSessionId: "managed-1", ObservedPath: cloudpb.ObservedPath_OBSERVED_PATH_SINGLE_RELAY,
		RttP50Millis: 40, RttP95Millis: 80, JitterMillis: 5,
		LossBasisPoints: 10, ThroughputBps: 8_000, ConnectedMillis: 30_000,
		NetworkClass: "wifi", Region: "eu-west", SampleCount: 4,
		WindowStartedAtUnixMillis: uint64(now.Add(-time.Minute).UnixMilli()),
		WindowEndedAtUnixMillis:   uint64(now.Add(-30 * time.Second).UnixMilli()),
		PacketCount:               1_000, LossEventCount: 1,
	}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = connection.ReportConnectionOutcome(context.Background(), &cloudpb.ReportConnectionOutcomeRequest{Outcome: &cloudpb.ConnectionOutcome{ManagedSessionId: "managed-1", ObservedPath: cloudpb.ObservedPath_OBSERVED_PATH_SINGLE_RELAY}})
	if err != nil {
		t.Fatal(err)
	}
	controlPlane.mu.Lock()
	defer controlPlane.mu.Unlock()
	if controlPlane.lastAuthorization != "account-access-token" || controlPlane.planCount != 1 || controlPlane.qualityCount != 1 || controlPlane.outcomeCount != 1 {
		t.Fatalf("Control Plane calls auth=%q plan=%d quality=%d outcome=%d", controlPlane.lastAuthorization, controlPlane.planCount, controlPlane.qualityCount, controlPlane.outcomeCount)
	}
	if lease.GetExpiresAtUnix() != uint64(now.Add(5*time.Minute).Unix()) {
		t.Fatalf("lease expiry = %d", lease.GetExpiresAtUnix())
	}
}

func TestDaemonRuntimeReportRequiresNegotiatedDaemonCapabilityAndDeviceBinding(t *testing.T) {
	now, service, _, _, _ := testService(t)
	connection := service.NewConnection()
	if _, err := connection.Hello(context.Background(), helloRequest(cloudpb.CallerRole_CALLER_ROLE_DAEMON, cloudpb.CompanionCapability_COMPANION_CAPABILITY_DAEMON_RUNTIME)); err != nil {
		t.Fatal(err)
	}
	reportID := "runtime-1:0"
	request := &cloudpb.ReportDaemonRuntimeRequest{ReportId: reportID, HubId: "hub-1", AssignmentEpoch: 1, PresenceSessionId: "presence-1", DaemonRuntimeGeneration: "runtime-1", PeerSessions: &cloudpb.PeerSessionInventorySnapshot{ReportId: reportID, DaemonDeviceId: "daemon-1", ControlOwnerHubId: "hub-1", AssignmentEpoch: 1, ControlPresenceSessionId: "presence-1", DaemonRuntimeGeneration: "runtime-1", ObservedAtUnixMillis: now.UnixMilli()}}
	response, err := connection.ReportDaemonRuntime(context.Background(), request)
	if err != nil || response.GetReportId() != reportID || response.GetAcceptedRegistryRevision() != 0 {
		t.Fatalf("ReportDaemonRuntime = (%#v, %v)", response, err)
	}
	foreign := proto.Clone(request).(*cloudpb.ReportDaemonRuntimeRequest)
	foreign.PeerSessions.DaemonDeviceId = "daemon-other"
	if _, err := connection.ReportDaemonRuntime(context.Background(), foreign); !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL) {
		t.Fatalf("foreign daemon runtime error = %v", err)
	}
}

func TestBeginPresenceUsesDeviceSessionAndReturnsIndependentSession(t *testing.T) {
	_, service, _, hub, _ := testService(t)
	connection := service.NewConnection()
	if _, err := connection.Hello(context.Background(), helloRequest(
		cloudpb.CallerRole_CALLER_ROLE_DAEMON,
		cloudpb.CompanionCapability_COMPANION_CAPABILITY_DEVICE_PRESENCE,
	)); err != nil {
		t.Fatal(err)
	}
	challenge, err := connection.BeginPresence(context.Background(), &cloudpb.BeginPresenceRequest{DeviceId: "daemon-1"})
	if err != nil {
		t.Fatal(err)
	}
	if challenge.GetPresenceSessionId() != "presence-1" || challenge.GetPresenceSessionId() == "managed-1" {
		t.Fatalf("presence challenge = %#v", challenge)
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if hub.lastAuthorization != "device-cloud-token" {
		t.Fatalf("presence authorization = %q", hub.lastAuthorization)
	}
}

func TestPlanManagedRouteRequiresNegotiatedSmartRouteCapability(t *testing.T) {
	_, service, controlPlane, _, _ := testService(t)
	connection := service.NewConnection()
	if _, err := connection.Hello(context.Background(), helloRequest(
		cloudpb.CallerRole_CALLER_ROLE_TUI,
		cloudpb.CompanionCapability_COMPANION_CAPABILITY_SIGNALING,
	)); err != nil {
		t.Fatal(err)
	}
	_, err := connection.PlanManagedRoute(context.Background(), &cloudpb.PlanManagedRouteRequest{
		EndpointId: "cloud-prod", ManagedSessionId: "managed-1", TargetDeviceId: "daemon-1",
		RoutePreference: cloudpb.RoutePreference_ROUTE_PREFERENCE_SMART_ROUTE,
	})
	if !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_INCOMPATIBLE) {
		t.Fatalf("PlanManagedRoute without capability error = %v", err)
	}
	controlPlane.mu.Lock()
	planCount := controlPlane.planCount
	controlPlane.mu.Unlock()
	if planCount != 0 {
		t.Fatalf("unnegotiated SmartRoute reached Control Plane %d times", planCount)
	}
}

func TestLifecycleStoresOnlyPrivateSessionAndReturnsSummary(t *testing.T) {
	now, service, _, _, _ := testService(t)
	connection := service.NewConnection()
	if _, err := connection.Hello(context.Background(), helloRequest(cloudpb.CallerRole_CALLER_ROLE_CLI,
		cloudpb.CompanionCapability_COMPANION_CAPABILITY_ACCOUNT_SESSION,
		cloudpb.CompanionCapability_COMPANION_CAPABILITY_DEVICE_ENROLLMENT,
	)); err != nil {
		t.Fatal(err)
	}
	flow, err := connection.BeginLogin(context.Background(), &cloudpb.BeginLoginRequest{Method: cloudpb.LoginMethod_LOGIN_METHOD_DEVICE_CODE})
	if err != nil || flow.GetFlowId() != "login-1" {
		t.Fatalf("BeginLogin = (%v, %v)", flow, err)
	}
	login, err := connection.CompleteLogin(context.Background(), &cloudpb.CompleteLoginRequest{FlowId: flow.GetFlowId()})
	if err != nil || login.GetSession().GetAccountId() != "account-login" || login.GetSession().GetExpiresAtUnix() != uint64(now.Add(time.Hour).Unix()) {
		t.Fatalf("CompleteLogin = (%v, %v)", login, err)
	}
	if strings.Contains(login.String(), "new-account-token") {
		t.Fatal("login response leaked account token")
	}

	challenge, err := connection.BeginDeviceEnrollment(context.Background(), &cloudpb.BeginDeviceEnrollmentRequest{
		OneTimeCode: "one-time-code", DevicePublicKey: bytes.Repeat([]byte{1}, 32), Metadata: &cloudpb.DeviceMetadata{Platform: "darwin", MuxviaVersion: "test"},
	})
	if err != nil || challenge.GetChallengeId() != "challenge-1" {
		t.Fatalf("BeginDeviceEnrollment = (%v, %v)", challenge, err)
	}
	enrolled, err := connection.CompleteDeviceEnrollment(context.Background(), &cloudpb.CompleteDeviceEnrollmentRequest{
		FlowId: challenge.GetFlowId(), Proof: &cloudpb.DeviceProof{DeviceId: "daemon-enrolled", DevicePublicKey: bytes.Repeat([]byte{1}, 32), ChallengeId: challenge.GetChallengeId(), Signature: bytes.Repeat([]byte{2}, 64), SignedAtUnixNano: now.UnixNano()},
	})
	if err != nil || enrolled.GetSession().GetDeviceId() != "daemon-enrolled" || strings.Contains(enrolled.String(), "new-device-token") {
		t.Fatalf("CompleteDeviceEnrollment = (%v, %v)", enrolled, err)
	}
	if _, err := connection.Logout(context.Background(), &cloudpb.LogoutRequest{AccountSession: true, DeviceSession: true}); err != nil {
		t.Fatal(err)
	}
}

func TestClosingClientConnectionDoesNotStopDaemonPresence(t *testing.T) {
	_, service, _, hub, _ := testService(t)
	client := service.NewConnection()
	daemon := service.NewConnection()
	_, _ = client.Hello(context.Background(), helloRequest(cloudpb.CallerRole_CALLER_ROLE_TUI, cloudpb.CompanionCapability_COMPANION_CAPABILITY_SIGNALING))
	_, _ = daemon.Hello(context.Background(), helloRequest(cloudpb.CallerRole_CALLER_ROLE_DAEMON, cloudpb.CompanionCapability_COMPANION_CAPABILITY_DEVICE_PRESENCE))

	clientContext, cancelClient := context.WithCancel(context.Background())
	defer cancelClient()
	clientStream, err := client.CreateSignalingSession(clientContext, &cloudpb.CreateSignalingSessionRequest{EndpointId: "cloud", ManagedSessionId: "managed-1", TargetDeviceId: "daemon-1", OfferSdp: "offer", RoutePreference: cloudpb.RoutePreference_ROUTE_PREFERENCE_STANDARD_RELAY})
	if err != nil {
		t.Fatal(err)
	}
	daemonContext, cancelDaemon := context.WithCancel(context.Background())
	defer cancelDaemon()
	presenceStream, err := daemon.OpenPresence(daemonContext, validPresenceRequest())
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-hub.signaling.done:
	case <-time.After(time.Second):
		t.Fatal("client signaling source was not closed")
	}
	select {
	case <-hub.presence.done:
		t.Fatal("closing client connection stopped daemon presence")
	default:
	}
	hub.presence.items <- &cloudpb.PresenceEvent{Payload: &cloudpb.PresenceEvent_Ready{Ready: &cloudpb.PresenceReady{PresenceSessionId: "presence-1", HeartbeatSeconds: 30}}}
	event, err := presenceStream.Receive()
	if err != nil || event.GetReady().GetPresenceSessionId() != "presence-1" {
		t.Fatalf("daemon presence Receive = (%v, %v)", event, err)
	}
	_ = clientStream.Close()
}

func TestSignalingStreamFailsWithBackpressureWithoutPretendingSuccess(t *testing.T) {
	_, service, _, hub, _ := testService(t)
	connection := service.NewConnection()
	_, _ = connection.Hello(context.Background(), helloRequest(cloudpb.CallerRole_CALLER_ROLE_TUI, cloudpb.CompanionCapability_COMPANION_CAPABILITY_SIGNALING))
	hub.signaling.items <- validAnswerEvent("signal-1")
	hub.signaling.items <- validAnswerEvent("signal-2")
	hub.signaling.items <- validAnswerEvent("signal-3")
	stream, err := connection.CreateSignalingSession(context.Background(), &cloudpb.CreateSignalingSessionRequest{EndpointId: "cloud", ManagedSessionId: "managed-1", TargetDeviceId: "daemon-1", OfferSdp: "offer", RoutePreference: cloudpb.RoutePreference_ROUTE_PREFERENCE_STANDARD_RELAY})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-hub.signaling.done:
	case <-time.After(time.Second):
		t.Fatal("overflow did not close Hub source")
	}
	if _, err := stream.Receive(); err != nil {
		t.Fatalf("queued answer was lost: %v", err)
	}
	if _, err := stream.Receive(); !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_BACKPRESSURE) {
		t.Fatalf("overflow Receive error = %v", err)
	}
}

func TestPresenceStreamRejectsWrongTargetOffer(t *testing.T) {
	_, service, _, hub, _ := testService(t)
	connection := service.NewConnection()
	_, _ = connection.Hello(context.Background(), helloRequest(cloudpb.CallerRole_CALLER_ROLE_DAEMON, cloudpb.CompanionCapability_COMPANION_CAPABILITY_DEVICE_PRESENCE))
	stream, err := connection.OpenPresence(context.Background(), validPresenceRequest())
	if err != nil {
		t.Fatal(err)
	}
	hub.presence.items <- &cloudpb.PresenceEvent{Payload: &cloudpb.PresenceEvent_Offer{Offer: &cloudpb.SignalingOffer{
		SignalingSessionId: "signal-1", ManagedSessionId: "managed-1", SourceDeviceId: "client-1", TargetDeviceId: "another-daemon", Sdp: "offer",
	}}}
	if _, err := stream.Receive(); !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL) {
		t.Fatalf("wrong-target presence offer error = %v", err)
	}
}

func TestDaemonCanCompleteOnlyOfferConsumedFromOwnPresence(t *testing.T) {
	_, service, _, hub, _ := testService(t)
	connection := service.NewConnection()
	_, _ = connection.Hello(context.Background(), helloRequest(cloudpb.CallerRole_CALLER_ROLE_DAEMON,
		cloudpb.CompanionCapability_COMPANION_CAPABILITY_DEVICE_PRESENCE,
		cloudpb.CompanionCapability_COMPANION_CAPABILITY_SIGNALING,
	))
	stream, err := connection.OpenPresence(context.Background(), validPresenceRequest())
	if err != nil {
		t.Fatal(err)
	}
	complete := &cloudpb.CompleteSignalingOfferRequest{SignalingSessionId: "signal-1", Result: &cloudpb.CompleteSignalingOfferRequest_Answer{Answer: &cloudpb.SignalingAnswer{SignalingSessionId: "signal-1", Sdp: "answer"}}}
	if _, err := connection.CompleteSignalingOffer(context.Background(), complete); !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL) {
		t.Fatalf("unseen offer completion error = %v", err)
	}
	hub.presence.items <- &cloudpb.PresenceEvent{Payload: &cloudpb.PresenceEvent_Offer{Offer: &cloudpb.SignalingOffer{
		SignalingSessionId: "signal-1", ManagedSessionId: "managed-1", SourceDeviceId: "client-1", TargetDeviceId: "daemon-1", Sdp: "offer",
	}}}
	if _, err := stream.Receive(); err != nil {
		t.Fatal(err)
	}
	if _, err := connection.CompleteSignalingOffer(context.Background(), complete); err != nil {
		t.Fatal(err)
	}
	if _, err := connection.CompleteSignalingOffer(context.Background(), complete); !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL) {
		t.Fatalf("duplicate completion error = %v", err)
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if hub.completed != 1 {
		t.Fatalf("Hub completion count = %d", hub.completed)
	}
}

func TestGlobalRouteRequiresNegotiatedSmartRouteCapability(t *testing.T) {
	_, service, _, _, _ := testService(t)
	connection := service.NewConnection()
	_, _ = connection.Hello(context.Background(), helloRequest(cloudpb.CallerRole_CALLER_ROLE_TUI, cloudpb.CompanionCapability_COMPANION_CAPABILITY_RELAY_LEASE))
	_, err := connection.AcquireRelayLease(context.Background(), &cloudpb.AcquireRelayLeaseRequest{ManagedSessionId: "managed-1", TargetDeviceId: "daemon-1", RoutePreference: cloudpb.RoutePreference_ROUTE_PREFERENCE_GLOBAL_ACCELERATOR})
	if !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_INCOMPATIBLE) {
		t.Fatalf("Global Accelerator without SmartRoute error = %v", err)
	}
}

func TestHubErrorAndCloseTextAreRedacted(t *testing.T) {
	_, service, _, hub, _ := testService(t)
	connection := service.NewConnection()
	_, _ = connection.Hello(context.Background(), helloRequest(cloudpb.CallerRole_CALLER_ROLE_TUI, cloudpb.CompanionCapability_COMPANION_CAPABILITY_SIGNALING))
	stream, err := connection.CreateSignalingSession(context.Background(), &cloudpb.CreateSignalingSessionRequest{EndpointId: "cloud", ManagedSessionId: "managed-1", TargetDeviceId: "daemon-1", OfferSdp: "offer", RoutePreference: cloudpb.RoutePreference_ROUTE_PREFERENCE_STANDARD_RELAY})
	if err != nil {
		t.Fatal(err)
	}
	hub.signaling.items <- &cloudpb.SignalingEvent{Payload: &cloudpb.SignalingEvent_Error{Error: &cloudpb.CloudError{Code: cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY, Message: "failed with account-access-token"}}}
	event, err := stream.Receive()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(event.GetError().GetMessage(), "account-access-token") || event.GetError().GetMessage() != "managed cloud signaling failed" {
		t.Fatalf("sanitized signaling error = %#v", event.GetError())
	}
}

func TestResolveEndpointRejectsNonTLSHubURL(t *testing.T) {
	_, service, _, hub, _ := testService(t)
	hub.resolveResponse.HubUrl = "http://hub.example.test"
	connection := service.NewConnection()
	_, _ = connection.Hello(context.Background(), helloRequest(cloudpb.CallerRole_CALLER_ROLE_TUI, cloudpb.CompanionCapability_COMPANION_CAPABILITY_SIGNALING))
	_, err := connection.ResolveEndpoint(context.Background(), &cloudpb.ResolveEndpointRequest{EndpointId: "cloud-prod", TargetDeviceId: "daemon-1"})
	if !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL) {
		t.Fatalf("non-TLS Hub URL error = %v", err)
	}
}

func TestAdapterErrorMessageIsRedacted(t *testing.T) {
	_, service, _, hub, _ := testService(t)
	hub.resolveErr = errors.New("network failed with account-access-token")
	connection := service.NewConnection()
	_, _ = connection.Hello(context.Background(), helloRequest(cloudpb.CallerRole_CALLER_ROLE_TUI, cloudpb.CompanionCapability_COMPANION_CAPABILITY_SIGNALING))
	_, err := connection.ResolveEndpoint(context.Background(), &cloudpb.ResolveEndpointRequest{EndpointId: "cloud", TargetDeviceId: "daemon-1"})
	if err == nil || strings.Contains(err.Error(), "account-access-token") || !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY) {
		t.Fatalf("sanitized adapter error = %v", err)
	}
}

func testService(t *testing.T) (time.Time, *companion.Service, *fakeControlPlane, *fakeHub, *session.Manager) {
	t.Helper()
	now := time.Date(2026, 7, 11, 9, 0, 0, 0, time.UTC)
	store := &testCredentialStore{secrets: make(map[string][]byte)}
	manager, err := session.NewManager(store, "default")
	if err != nil {
		t.Fatal(err)
	}
	account, _ := session.New(session.Metadata{Kind: session.KindAccount, AccountID: "account-1", AccountLabel: "Alice", DeviceID: "client-1", ExpiresAt: now.Add(time.Hour)}, []byte("account-access-token"), now)
	device, _ := session.New(session.Metadata{Kind: session.KindDevice, AccountID: "account-1", DeviceID: "daemon-1", ExpiresAt: now.Add(time.Hour), HubID: "hub-1", HubURL: "https://hub.example.test", HubRegion: "local", HubDirectoryVersion: 1}, []byte("device-cloud-token"), now)
	if err := manager.Save(context.Background(), account, now); err != nil {
		t.Fatal(err)
	}
	if err := manager.Save(context.Background(), device, now); err != nil {
		t.Fatal(err)
	}
	controlPlane := &fakeControlPlane{
		now:             now,
		resolveResponse: &cloudpb.ResolvedEndpoint{EndpointId: "cloud-prod", TargetDeviceId: "daemon-1", Presence: cloudpb.PresenceState_PRESENCE_STATE_ONLINE, HubId: "hub-1", HubUrl: "https://hub.example.test", ManagedSessionId: "managed-1"},
		planResponse: &cloudpb.ManagedRoutePlan{
			PlanId: "plan-1", ManagedSessionId: "managed-1", TargetDeviceId: "daemon-1",
			SelectedPath:    cloudpb.ObservedPath_OBSERVED_PATH_SINGLE_RELAY,
			SelectionReason: cloudpb.RouteSelectionReason_ROUTE_SELECTION_REASON_DIRECT_UNSTABLE,
			ValidUntilUnix:  uint64(now.Add(time.Minute).Unix()), RelayOnly: true, RelayRegion: "eu-west",
			IceServers: []*cloudpb.IceServer{{Urls: []string{"turns:relay.example.test"}, Username: "user", Credential: "credential"}},
		},
	}
	hub := &fakeHub{presence: newPresenceSource(8), signaling: newSignalingSource(8), resolveResponse: controlPlane.resolveResponse, leaseResponse: &cloudpb.RelayLease{LeaseId: "lease-1", SignedLease: []byte("signed-lease"), ExpiresAtUnix: uint64(now.Add(5 * time.Minute).Unix()), PathKind: cloudpb.ObservedPath_OBSERVED_PATH_SINGLE_RELAY, IceServers: []*cloudpb.IceServer{{Urls: []string{"turn:relay.example.test"}, Username: "user", Credential: "credential"}}}}
	service, err := companion.NewService(companion.Config{
		CompanionVersion: "1.0.0", BuildChannel: "test", ExecutableSHA256: bytes.Repeat([]byte{0x31}, 32), StreamCapacity: 1,
		Capabilities: []cloudpb.CompanionCapability{
			cloudpb.CompanionCapability_COMPANION_CAPABILITY_ACCOUNT_SESSION,
			cloudpb.CompanionCapability_COMPANION_CAPABILITY_DEVICE_PRESENCE,
			cloudpb.CompanionCapability_COMPANION_CAPABILITY_SIGNALING,
			cloudpb.CompanionCapability_COMPANION_CAPABILITY_RELAY_LEASE,
			cloudpb.CompanionCapability_COMPANION_CAPABILITY_PATH_QUALITY,
			cloudpb.CompanionCapability_COMPANION_CAPABILITY_SMART_ROUTE,
			cloudpb.CompanionCapability_COMPANION_CAPABILITY_DEVICE_ENROLLMENT,
			cloudpb.CompanionCapability_COMPANION_CAPABILITY_DAEMON_RUNTIME,
		},
		Now: func() time.Time { return now }, NonceReader: bytes.NewReader(bytes.Repeat([]byte{0x5a}, 1024)),
	}, manager, controlPlane, hub)
	if err != nil {
		t.Fatal(err)
	}
	return now, service, controlPlane, hub, manager
}

func helloRequest(role cloudpb.CallerRole, capabilities ...cloudpb.CompanionCapability) *cloudpb.CompanionHelloRequest {
	return &cloudpb.CompanionHelloRequest{ProtocolMin: cloudcompanion.ProtocolVersionMin, ProtocolMax: cloudcompanion.ProtocolVersionMax, MuxviaVersion: "test", CallerRole: role, RequestedCapabilities: capabilities, RequestNonce: bytes.Repeat([]byte{0x11}, 32)}
}

func validPresenceRequest() *cloudpb.OpenPresenceRequest {
	return &cloudpb.OpenPresenceRequest{PresenceSessionId: "presence-1", Proof: &cloudpb.DeviceProof{DeviceId: "daemon-1", DevicePublicKey: bytes.Repeat([]byte{0x21}, 32), ChallengeId: "challenge", Signature: bytes.Repeat([]byte{0x22}, 64), SignedAtUnixNano: 1}, Metadata: &cloudpb.DeviceMetadata{Platform: "darwin", MuxviaVersion: "test"}}
}

func validAnswerEvent(sessionID string) *cloudpb.SignalingEvent {
	return &cloudpb.SignalingEvent{Payload: &cloudpb.SignalingEvent_Answer{Answer: &cloudpb.SignalingAnswer{SignalingSessionId: sessionID, Sdp: "answer"}}}
}
