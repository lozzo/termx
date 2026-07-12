package devcloud

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lozzow/termx/private/cloud/companion"
	"github.com/lozzow/termx/private/cloud/companion/cloudservice/httpapi"
	"github.com/lozzow/termx/private/cloud/companion/session"
	"github.com/lozzow/termx/proto/cloudpb"
	"github.com/lozzow/termx/shared/cloudcompanion"
)

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (clock *testClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *testClock) Advance(duration time.Duration) {
	clock.mu.Lock()
	clock.now = clock.now.Add(duration)
	clock.mu.Unlock()
}

type memoryCredentialStore struct {
	mu      sync.Mutex
	secrets map[string][]byte
}

func (store *memoryCredentialStore) LoadSecret(_ context.Context, key string) ([]byte, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	value, ok := store.secrets[key]
	if !ok {
		return nil, session.ErrNotFound
	}
	return append([]byte(nil), value...), nil
}

func (store *memoryCredentialStore) StoreSecret(_ context.Context, key string, value []byte) error {
	store.mu.Lock()
	store.secrets[key] = append([]byte(nil), value...)
	store.mu.Unlock()
	return nil
}

func (store *memoryCredentialStore) DeleteSecret(_ context.Context, key string) error {
	store.mu.Lock()
	delete(store.secrets, key)
	store.mu.Unlock()
	return nil
}

type capturedRequest struct {
	host          string
	path          string
	authorization string
	body          []byte
}

type captureTransport struct {
	mu       sync.Mutex
	requests []capturedRequest
}

func (transport *captureTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	var body []byte
	if request.Body != nil {
		var err error
		body, err = io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		request.Body = io.NopCloser(bytes.NewReader(body))
	}
	transport.mu.Lock()
	transport.requests = append(transport.requests, capturedRequest{
		host: request.URL.Host, path: request.URL.Path,
		authorization: request.Header.Get("Authorization"), body: append([]byte(nil), body...),
	})
	transport.mu.Unlock()
	return http.DefaultTransport.RoundTrip(request)
}

func (transport *captureTransport) snapshot() []capturedRequest {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	result := make([]capturedRequest, len(transport.requests))
	copy(result, transport.requests)
	return result
}

func TestDevCloudVerticalLoopAcrossRealServiceBoundaries(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	clock := &testClock{now: time.Date(2026, 7, 11, 8, 0, 0, 0, time.UTC)}
	runtime, err := Start(Config{Now: clock.Now, EnrollmentCode: "enroll-test-once"})
	if err != nil {
		t.Fatal(err)
	}
	runtimeClosed := false
	t.Cleanup(func() {
		if !runtimeClosed {
			shutdownContext, shutdownCancel := context.WithTimeout(context.Background(), time.Second)
			defer shutdownCancel()
			_ = runtime.Close(shutdownContext)
		}
	})
	manifest := runtime.Manifest()

	capture := &captureTransport{}
	httpClient := &http.Client{Transport: capture}
	clientAdapter, err := httpapi.New(httpapi.Config{
		ControlPlaneURL: manifest.ControlPlaneURL, HubURL: manifest.HubURL, HTTPClient: httpClient, Now: clock.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	daemonAdapter, err := httpapi.New(httpapi.Config{
		ControlPlaneURL: manifest.ControlPlaneURL, HubURL: manifest.HubURL, HTTPClient: httpClient, Now: clock.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	credentialStore := &memoryCredentialStore{secrets: make(map[string][]byte)}
	clientSessions, clientService := newTestCompanion(t, credentialStore, "client-profile", clock, clientAdapter,
		cloudpb.CompanionCapability_COMPANION_CAPABILITY_ACCOUNT_SESSION,
		cloudpb.CompanionCapability_COMPANION_CAPABILITY_SIGNALING,
	)
	daemonSessions, daemonService := newTestCompanion(t, credentialStore, "daemon-profile", clock, daemonAdapter,
		cloudpb.CompanionCapability_COMPANION_CAPABILITY_DEVICE_ENROLLMENT,
		cloudpb.CompanionCapability_COMPANION_CAPABILITY_DEVICE_PRESENCE,
		cloudpb.CompanionCapability_COMPANION_CAPABILITY_SIGNALING,
	)

	clientLifecycle := clientService.NewConnection()
	mustHello(t, clientLifecycle, cloudpb.CallerRole_CALLER_ROLE_CLI, cloudpb.CompanionCapability_COMPANION_CAPABILITY_ACCOUNT_SESSION)
	loginFlow, err := clientLifecycle.BeginLogin(ctx, &cloudpb.BeginLoginRequest{Method: cloudpb.LoginMethod_LOGIN_METHOD_DEVICE_CODE})
	if err != nil {
		t.Fatal(err)
	}
	login, err := clientLifecycle.CompleteLogin(ctx, &cloudpb.CompleteLoginRequest{FlowId: loginFlow.GetFlowId()})
	if err != nil || login.GetSession().GetDeviceId() != devClientDeviceID {
		t.Fatalf("CompleteLogin = (%v, %v)", login, err)
	}
	if _, err := daemonSessions.Load(ctx, session.KindAccount, clock.Now()); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("daemon profile loaded client account session: %v", err)
	}

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	daemonConnection := daemonService.NewConnection()
	mustHello(t, daemonConnection, cloudpb.CallerRole_CALLER_ROLE_DAEMON,
		cloudpb.CompanionCapability_COMPANION_CAPABILITY_DEVICE_ENROLLMENT,
		cloudpb.CompanionCapability_COMPANION_CAPABILITY_DEVICE_PRESENCE,
		cloudpb.CompanionCapability_COMPANION_CAPABILITY_SIGNALING,
	)
	enrollmentChallenge, err := daemonConnection.BeginDeviceEnrollment(ctx, &cloudpb.BeginDeviceEnrollmentRequest{
		OneTimeCode: manifest.EnrollmentCode, DevicePublicKey: publicKey,
		Metadata: &cloudpb.DeviceMetadata{DisplayName: "Vertical daemon", Platform: "test", TermxVersion: "test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	daemonDeviceID := "daemon-vertical"
	enrollmentProof := signEnrollmentProof(t, privateKey, publicKey, daemonDeviceID, enrollmentChallenge, clock.Now())
	enrollment, err := daemonConnection.CompleteDeviceEnrollment(ctx, &cloudpb.CompleteDeviceEnrollmentRequest{
		FlowId: enrollmentChallenge.GetFlowId(), Proof: enrollmentProof,
	})
	if err != nil || enrollment.GetSession().GetDeviceId() != daemonDeviceID {
		t.Fatalf("CompleteDeviceEnrollment = (%v, %v)", enrollment, err)
	}
	if _, err := clientSessions.Load(ctx, session.KindDevice, clock.Now()); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("client profile loaded daemon device session: %v", err)
	}

	presenceChallenge, err := daemonConnection.BeginPresence(ctx, &cloudpb.BeginPresenceRequest{DeviceId: daemonDeviceID})
	if err != nil {
		t.Fatal(err)
	}
	presenceRequest := signPresenceRequest(t, privateKey, publicKey, daemonDeviceID, presenceChallenge, clock.Now())
	presenceStream, err := daemonConnection.OpenPresence(ctx, presenceRequest)
	if err != nil {
		t.Fatal(err)
	}
	ready, err := presenceStream.Receive()
	if err != nil || ready.GetReady().GetPresenceSessionId() != presenceChallenge.GetPresenceSessionId() {
		t.Fatalf("presence ready = (%v, %v)", ready, err)
	}

	daemonStoredSession, err := daemonSessions.Load(ctx, session.KindDevice, clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer daemonStoredSession.Destroy()
	daemonAuthorization := daemonStoredSession.Authorization()
	defer daemonAuthorization.Destroy()
	if _, err := daemonAdapter.AcquirePresenceAdmission(ctx, daemonAuthorization, presenceRequest); !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED) {
		t.Fatalf("replayed presence proof error = %v", err)
	}
	assertRejectedPresenceProofs(t, ctx, clock, daemonAdapter, daemonAuthorization, daemonDeviceID, publicKey, privateKey)

	clientConnection := clientService.NewConnection()
	mustHello(t, clientConnection, cloudpb.CallerRole_CALLER_ROLE_TUI, cloudpb.CompanionCapability_COMPANION_CAPABILITY_SIGNALING)
	firstResolved := resolveOnline(t, ctx, clientConnection, "endpoint-first", daemonDeviceID)
	if firstResolved.GetManagedSessionId() == presenceChallenge.GetPresenceSessionId() {
		t.Fatal("PresenceSessionID was reused as ManagedSessionID")
	}
	firstOffer := &cloudpb.CreateSignalingSessionRequest{
		EndpointId: firstResolved.GetEndpointId(), ManagedSessionId: firstResolved.GetManagedSessionId(),
		TargetDeviceId: daemonDeviceID, OfferSdp: "offer-first", RoutePreference: cloudpb.RoutePreference_ROUTE_PREFERENCE_DIRECT_ONLY,
	}
	firstClientStream, err := clientConnection.CreateSignalingSession(ctx, firstOffer)
	if err != nil {
		t.Fatal(err)
	}
	firstDaemonEvent, err := presenceStream.Receive()
	if err != nil || firstDaemonEvent.GetOffer().GetManagedSessionId() == "" || firstDaemonEvent.GetOffer().GetManagedSessionId() == firstResolved.GetManagedSessionId() {
		t.Fatalf("first daemon offer = (%v, %v)", firstDaemonEvent, err)
	}
	firstCompletion := answerFor(firstDaemonEvent.GetOffer(), "answer-first")
	if _, err := daemonConnection.CompleteSignalingOffer(ctx, firstCompletion); err != nil {
		t.Fatal(err)
	}
	firstAnswer, err := firstClientStream.Receive()
	if err != nil || firstAnswer.GetAnswer().GetSdp() != "answer-first" {
		t.Fatalf("first client answer = (%v, %v)", firstAnswer, err)
	}
	_ = firstClientStream.Close()

	secondResolved := resolveOnline(t, ctx, clientConnection, "endpoint-second", daemonDeviceID)
	if secondResolved.GetManagedSessionId() == firstResolved.GetManagedSessionId() || secondResolved.GetManagedSessionId() == presenceChallenge.GetPresenceSessionId() {
		t.Fatal("managed and presence session identities were reused")
	}
	secondOffer := &cloudpb.CreateSignalingSessionRequest{
		EndpointId: secondResolved.GetEndpointId(), ManagedSessionId: secondResolved.GetManagedSessionId(),
		TargetDeviceId: daemonDeviceID, OfferSdp: "offer-second", RoutePreference: cloudpb.RoutePreference_ROUTE_PREFERENCE_DIRECT_ONLY,
	}
	secondClientStream, err := clientConnection.CreateSignalingSession(ctx, secondOffer)
	if err != nil {
		t.Fatal(err)
	}
	secondDaemonEvent, err := presenceStream.Receive()
	if err != nil || secondDaemonEvent.GetOffer().GetManagedSessionId() == "" || secondDaemonEvent.GetOffer().GetManagedSessionId() == secondResolved.GetManagedSessionId() {
		t.Fatalf("second daemon offer = (%v, %v)", secondDaemonEvent, err)
	}
	secondCompletion := answerFor(secondDaemonEvent.GetOffer(), "answer-second")
	wrongCompletion := answerFor(secondDaemonEvent.GetOffer(), "wrong-answer")
	wrongCompletion.SignalingSessionId = "signal-not-owned"
	wrongCompletion.GetAnswer().SignalingSessionId = "signal-not-owned"
	if _, err := daemonAdapter.CompleteSignalingOffer(ctx, daemonAuthorization, wrongCompletion); !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED) {
		t.Fatalf("cross-session answer error = %v", err)
	}
	if _, err := daemonConnection.CompleteSignalingOffer(ctx, secondCompletion); err != nil {
		t.Fatal(err)
	}
	secondAnswer, err := secondClientStream.Receive()
	if err != nil || secondAnswer.GetAnswer().GetSdp() != "answer-second" {
		t.Fatalf("second client answer = (%v, %v)", secondAnswer, err)
	}
	_ = secondClientStream.Close()
	failedResolved := resolveOnline(t, ctx, clientConnection, "endpoint-failure", daemonDeviceID)
	failedClientStream, err := clientConnection.CreateSignalingSession(ctx, &cloudpb.CreateSignalingSessionRequest{
		EndpointId: failedResolved.GetEndpointId(), ManagedSessionId: failedResolved.GetManagedSessionId(),
		TargetDeviceId: daemonDeviceID, OfferSdp: "offer-failure", RoutePreference: cloudpb.RoutePreference_ROUTE_PREFERENCE_DIRECT_ONLY,
	})
	if err != nil {
		t.Fatal(err)
	}
	failedDaemonEvent, err := presenceStream.Receive()
	if err != nil || failedDaemonEvent.GetOffer().GetManagedSessionId() == "" || failedDaemonEvent.GetOffer().GetManagedSessionId() == failedResolved.GetManagedSessionId() {
		t.Fatalf("failed daemon offer = (%v, %v)", failedDaemonEvent, err)
	}
	if _, err := daemonConnection.CompleteSignalingOffer(ctx, &cloudpb.CompleteSignalingOfferRequest{
		SignalingSessionId: failedDaemonEvent.GetOffer().GetSignalingSessionId(),
		Result: &cloudpb.CompleteSignalingOfferRequest_Error{Error: &cloudpb.CloudError{
			Code: cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_ROUTE_UNAVAILABLE, Retryable: true,
			Message: "capability-grant-sentinel terminal-payload-sentinel", CorrelationId: "private-correlation",
		}},
	}); err != nil {
		t.Fatal(err)
	}
	failedClientEvent, err := failedClientStream.Receive()
	if err != nil || failedClientEvent.GetError().GetCode() != cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_ROUTE_UNAVAILABLE || failedClientEvent.GetError().GetMessage() != "managed cloud signaling failed" {
		t.Fatalf("client signaling failure = (%v, %v)", failedClientEvent, err)
	}
	_ = failedClientStream.Close()

	clientStoredSession, err := clientSessions.Load(ctx, session.KindAccount, clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer clientStoredSession.Destroy()
	clientAuthorization := clientStoredSession.Authorization()
	defer clientAuthorization.Destroy()
	if _, err := daemonAdapter.BeginPresence(ctx, clientAuthorization, &cloudpb.BeginPresenceRequest{DeviceId: daemonDeviceID}); !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED) {
		t.Fatalf("account credential used as device credential error = %v", err)
	}

	controlDownResolved, err := clientAdapter.ResolveEndpoint(ctx, clientAuthorization, &cloudpb.ResolveEndpointRequest{EndpointId: "control-down-direct", TargetDeviceId: daemonDeviceID})
	if err != nil {
		t.Fatal(err)
	}
	controlShutdownContext, controlShutdownCancel := context.WithTimeout(context.Background(), time.Second)
	if err := runtime.controlServer.Shutdown(controlShutdownContext); err != nil {
		controlShutdownCancel()
		t.Fatal(err)
	}
	controlShutdownCancel()
	controlDownRequest := &cloudpb.CreateSignalingSessionRequest{EndpointId: controlDownResolved.GetEndpointId(), ManagedSessionId: controlDownResolved.GetManagedSessionId(), TargetDeviceId: daemonDeviceID, OfferSdp: "offer-control-down", RoutePreference: cloudpb.RoutePreference_ROUTE_PREFERENCE_DIRECT_ONLY}
	controlDownStream, err := clientAdapter.CreateSignalingSession(ctx, clientAuthorization, controlDownRequest)
	if err != nil {
		t.Fatalf("new direct signaling with Control Plane down = %v", err)
	}
	controlDownOffer, err := presenceStream.Receive()
	if err != nil || controlDownOffer.GetOffer() == nil || controlDownOffer.GetOffer().GetManagedSessionId() == controlDownResolved.GetManagedSessionId() {
		t.Fatalf("Control Plane down Hub-owned offer = (%v, %v)", controlDownOffer, err)
	}
	if _, err := daemonAdapter.CompleteSignalingOffer(ctx, daemonAuthorization, answerFor(controlDownOffer.GetOffer(), "answer-control-down")); err != nil {
		t.Fatalf("daemon answer with Control Plane down = %v", err)
	}
	controlDownAnswer, err := controlDownStream.Receive(ctx)
	if err != nil || controlDownAnswer.GetAnswer().GetSdp() != "answer-control-down" {
		t.Fatalf("Control Plane down client answer = (%v, %v)", controlDownAnswer, err)
	}
	_ = controlDownStream.Close()
	if _, err := clientAdapter.ResolveEndpoint(ctx, clientAuthorization, &cloudpb.ResolveEndpointRequest{EndpointId: "control-is-down", TargetDeviceId: daemonDeviceID}); !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_ROUTE_UNAVAILABLE) {
		t.Fatalf("closed Control Plane resolve error = %v", err)
	}

	hubDownRequest := &cloudpb.CreateSignalingSessionRequest{
		EndpointId: controlDownResolved.GetEndpointId(), ManagedSessionId: controlDownResolved.GetManagedSessionId(),
		TargetDeviceId: daemonDeviceID, OfferSdp: "offer-hub-down", RoutePreference: cloudpb.RoutePreference_ROUTE_PREFERENCE_DIRECT_ONLY,
	}
	_ = presenceStream.Close()
	_ = clientConnection.Close()
	_ = daemonConnection.Close()
	hubShutdownContext, hubShutdownCancel := context.WithTimeout(context.Background(), time.Second)
	if err := runtime.hubServer.Shutdown(hubShutdownContext); err != nil {
		hubShutdownCancel()
		t.Fatal(err)
	}
	hubShutdownCancel()
	if _, err := clientAdapter.CreateSignalingSession(ctx, clientAuthorization, hubDownRequest); !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_ROUTE_UNAVAILABLE) {
		t.Fatalf("closed Hub error = %v", err)
	}
	shutdownContext, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
	if err := runtime.Close(shutdownContext); err != nil {
		shutdownCancel()
		t.Fatal(err)
	}
	shutdownCancel()
	runtimeClosed = true

	assertCredentialVisibility(t, capture.snapshot(), manifest.HubURL, privateKey, clientAuthorization.Bytes(), daemonAuthorization.Bytes())
}

func TestDevCloudHubReturnsBackpressureAcrossHTTPStream(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	clock := &testClock{now: time.Date(2026, 7, 11, 9, 0, 0, 0, time.UTC)}
	runtime, err := start(Config{Now: clock.Now, EnrollmentCode: "enroll-backpressure"}, runtimeOptions{presenceQueueSize: 1, clientQueueSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		shutdownContext, shutdownCancel := context.WithTimeout(context.Background(), time.Second)
		defer shutdownCancel()
		_ = runtime.Close(shutdownContext)
	})
	manifest := runtime.Manifest()
	adapter, err := httpapi.New(httpapi.Config{ControlPlaneURL: manifest.ControlPlaneURL, HubURL: manifest.HubURL, Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	credentialStore := &memoryCredentialStore{secrets: make(map[string][]byte)}
	clientSessions, clientService := newTestCompanion(t, credentialStore, "backpressure-client", clock, adapter,
		cloudpb.CompanionCapability_COMPANION_CAPABILITY_ACCOUNT_SESSION,
	)
	daemonSessions, daemonService := newTestCompanion(t, credentialStore, "backpressure-daemon", clock, adapter,
		cloudpb.CompanionCapability_COMPANION_CAPABILITY_DEVICE_ENROLLMENT,
	)
	clientLifecycle := clientService.NewConnection()
	mustHello(t, clientLifecycle, cloudpb.CallerRole_CALLER_ROLE_CLI, cloudpb.CompanionCapability_COMPANION_CAPABILITY_ACCOUNT_SESSION)
	loginFlow, err := clientLifecycle.BeginLogin(ctx, &cloudpb.BeginLoginRequest{Method: cloudpb.LoginMethod_LOGIN_METHOD_DEVICE_CODE})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := clientLifecycle.CompleteLogin(ctx, &cloudpb.CompleteLoginRequest{FlowId: loginFlow.GetFlowId()}); err != nil {
		t.Fatal(err)
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	daemonLifecycle := daemonService.NewConnection()
	mustHello(t, daemonLifecycle, cloudpb.CallerRole_CALLER_ROLE_DAEMON, cloudpb.CompanionCapability_COMPANION_CAPABILITY_DEVICE_ENROLLMENT)
	enrollmentChallenge, err := daemonLifecycle.BeginDeviceEnrollment(ctx, &cloudpb.BeginDeviceEnrollmentRequest{
		OneTimeCode: manifest.EnrollmentCode, DevicePublicKey: publicKey,
		Metadata: &cloudpb.DeviceMetadata{Platform: "test", TermxVersion: "test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	deviceID := "daemon-backpressure"
	if _, err := daemonLifecycle.CompleteDeviceEnrollment(ctx, &cloudpb.CompleteDeviceEnrollmentRequest{
		FlowId: enrollmentChallenge.GetFlowId(),
		Proof:  signEnrollmentProof(t, privateKey, publicKey, deviceID, enrollmentChallenge, clock.Now()),
	}); err != nil {
		t.Fatal(err)
	}
	clientStoredSession, err := clientSessions.Load(ctx, session.KindAccount, clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer clientStoredSession.Destroy()
	clientAuthorization := clientStoredSession.Authorization()
	defer clientAuthorization.Destroy()
	daemonStoredSession, err := daemonSessions.Load(ctx, session.KindDevice, clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer daemonStoredSession.Destroy()
	daemonAuthorization := daemonStoredSession.Authorization()
	defer daemonAuthorization.Destroy()
	presenceChallenge, err := adapter.BeginPresence(ctx, daemonAuthorization, &cloudpb.BeginPresenceRequest{DeviceId: deviceID})
	if err != nil {
		t.Fatal(err)
	}
	presenceRequest := signPresenceRequest(t, privateKey, publicKey, deviceID, presenceChallenge, clock.Now())
	presenceAdmission, err := adapter.AcquirePresenceAdmission(ctx, daemonAuthorization, presenceRequest)
	if err != nil {
		t.Fatal(err)
	}
	presenceSource, err := adapter.OpenPresence(ctx, daemonAuthorization, presenceAdmission, presenceRequest)
	presenceAdmission.Destroy()
	if err != nil {
		t.Fatal(err)
	}
	defer presenceSource.Close()
	ready, err := presenceSource.Receive(ctx)
	if err != nil || ready.GetReady() == nil {
		t.Fatalf("presence ready = (%v, %v)", ready, err)
	}

	largeOffer := strings.Repeat("o", 900<<10)
	var signalingSources []interface{ Close() error }
	defer func() {
		for _, source := range signalingSources {
			_ = source.Close()
		}
	}()
	backpressureObserved := false
	for index := 0; index < 32; index++ {
		resolved, err := adapter.ResolveEndpoint(ctx, clientAuthorization, &cloudpb.ResolveEndpointRequest{
			EndpointId: "backpressure-endpoint", TargetDeviceId: deviceID,
		})
		if err != nil {
			t.Fatal(err)
		}
		request := &cloudpb.CreateSignalingSessionRequest{
			EndpointId: resolved.GetEndpointId(), ManagedSessionId: resolved.GetManagedSessionId(),
			TargetDeviceId: deviceID, OfferSdp: largeOffer, RoutePreference: cloudpb.RoutePreference_ROUTE_PREFERENCE_DIRECT_ONLY,
		}
		source, err := adapter.CreateSignalingSession(ctx, clientAuthorization, request)
		if err != nil {
			if !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_BACKPRESSURE) {
				t.Fatalf("Hub pressure error = %v", err)
			}
			backpressureObserved = true
			break
		}
		signalingSources = append(signalingSources, source)
	}
	if !backpressureObserved {
		t.Fatal("Hub stream did not return bounded backpressure")
	}
}

func newTestCompanion(t *testing.T, store session.OSCredentialStore, profile string, clock *testClock, adapter *httpapi.Adapter, capabilities ...cloudpb.CompanionCapability) (*session.Manager, *companion.Service) {
	t.Helper()
	manager, err := session.NewManager(store, profile)
	if err != nil {
		t.Fatal(err)
	}
	service, err := companion.NewService(companion.Config{
		CompanionVersion: "vertical-test", BuildChannel: "development", Capabilities: capabilities,
		StreamCapacity: 8, Now: clock.Now,
	}, manager, adapter, adapter)
	if err != nil {
		t.Fatal(err)
	}
	return manager, service
}

func mustHello(t *testing.T, connection *companion.Connection, role cloudpb.CallerRole, capabilities ...cloudpb.CompanionCapability) {
	t.Helper()
	if _, err := connection.Hello(context.Background(), &cloudpb.CompanionHelloRequest{
		ProtocolMin: cloudcompanion.ProtocolVersionMin, ProtocolMax: cloudcompanion.ProtocolVersionMax,
		TermxVersion: "vertical-test", CallerRole: role, RequestedCapabilities: capabilities,
		RequestNonce: bytes.Repeat([]byte{0x41}, 32),
	}); err != nil {
		t.Fatal(err)
	}
}

func signEnrollmentProof(t *testing.T, privateKey ed25519.PrivateKey, publicKey ed25519.PublicKey, deviceID string, challenge *cloudpb.DeviceEnrollmentChallenge, signedAt time.Time) *cloudpb.DeviceProof {
	t.Helper()
	input := &cloudpb.DeviceEnrollmentProofInput{
		FlowId: challenge.GetFlowId(), ChallengeId: challenge.GetChallengeId(), Challenge: challenge.GetChallenge(),
		DeviceId: deviceID, DevicePublicKey: publicKey, SignedAtUnixNano: signedAt.UnixNano(),
	}
	signingBytes, err := cloudcompanion.EnrollmentProofSigningBytes(input)
	if err != nil {
		t.Fatal(err)
	}
	return &cloudpb.DeviceProof{
		DeviceId: deviceID, DevicePublicKey: publicKey, ChallengeId: challenge.GetChallengeId(),
		Signature: ed25519.Sign(privateKey, signingBytes), SignedAtUnixNano: signedAt.UnixNano(),
	}
}

func signPresenceRequest(t *testing.T, privateKey ed25519.PrivateKey, publicKey ed25519.PublicKey, deviceID string, challenge *cloudpb.PresenceChallenge, signedAt time.Time) *cloudpb.OpenPresenceRequest {
	t.Helper()
	input := &cloudpb.PresenceProofInput{
		PresenceSessionId: challenge.GetPresenceSessionId(), ChallengeId: challenge.GetChallengeId(), Challenge: challenge.GetChallenge(),
		DeviceId: deviceID, DevicePublicKey: publicKey, SignedAtUnixNano: signedAt.UnixNano(),
	}
	signingBytes, err := cloudcompanion.PresenceProofSigningBytes(input)
	if err != nil {
		t.Fatal(err)
	}
	return &cloudpb.OpenPresenceRequest{
		PresenceSessionId: challenge.GetPresenceSessionId(),
		Proof: &cloudpb.DeviceProof{
			DeviceId: deviceID, DevicePublicKey: publicKey, ChallengeId: challenge.GetChallengeId(),
			Signature: ed25519.Sign(privateKey, signingBytes), SignedAtUnixNano: signedAt.UnixNano(),
		},
		Metadata: &cloudpb.DeviceMetadata{DisplayName: "Vertical daemon", Platform: "test", TermxVersion: "test"},
	}
}

func assertRejectedPresenceProofs(t *testing.T, ctx context.Context, clock *testClock, adapter *httpapi.Adapter, authorization session.Authorization, deviceID string, publicKey ed25519.PublicKey, privateKey ed25519.PrivateKey) {
	t.Helper()
	wrongPublicKey, wrongPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	wrongKeyChallenge, err := adapter.BeginPresence(ctx, authorization, &cloudpb.BeginPresenceRequest{DeviceId: deviceID})
	if err != nil {
		t.Fatal(err)
	}
	wrongKeyProof := signPresenceRequest(t, wrongPrivateKey, wrongPublicKey, deviceID, wrongKeyChallenge, clock.Now())
	if _, err := adapter.AcquirePresenceAdmission(ctx, authorization, wrongKeyProof); !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED) {
		t.Fatalf("wrong-key presence proof error = %v", err)
	}
	wrongDeviceChallenge, err := adapter.BeginPresence(ctx, authorization, &cloudpb.BeginPresenceRequest{DeviceId: deviceID})
	if err != nil {
		t.Fatal(err)
	}
	wrongDeviceProof := signPresenceRequest(t, privateKey, publicKey, "daemon-other", wrongDeviceChallenge, clock.Now())
	if _, err := adapter.AcquirePresenceAdmission(ctx, authorization, wrongDeviceProof); !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED) {
		t.Fatalf("wrong-device presence proof error = %v", err)
	}
}

func resolveOnline(t *testing.T, ctx context.Context, connection *companion.Connection, endpointID, targetDeviceID string) *cloudpb.ResolvedEndpoint {
	t.Helper()
	resolved, err := connection.ResolveEndpoint(ctx, &cloudpb.ResolveEndpointRequest{EndpointId: endpointID, TargetDeviceId: targetDeviceID})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.GetPresence() != cloudpb.PresenceState_PRESENCE_STATE_ONLINE || resolved.GetManagedSessionId() == "" {
		t.Fatalf("ResolveEndpoint = %v", resolved)
	}
	return resolved
}

func answerFor(offer *cloudpb.SignalingOffer, sdp string) *cloudpb.CompleteSignalingOfferRequest {
	return &cloudpb.CompleteSignalingOfferRequest{
		SignalingSessionId: offer.GetSignalingSessionId(),
		Result: &cloudpb.CompleteSignalingOfferRequest_Answer{Answer: &cloudpb.SignalingAnswer{
			SignalingSessionId: offer.GetSignalingSessionId(), Sdp: sdp,
		}},
	}
}

func assertCredentialVisibility(t *testing.T, requests []capturedRequest, hubOrigin string, privateKey, accountToken, deviceToken []byte) {
	t.Helper()
	hubHost := hubOrigin[len("http://"):]
	for _, request := range requests {
		if request.host == hubHost && request.path == httpapi.HubOpenPresencePath && request.authorization != "" {
			t.Fatalf("Hub presence request %s received cloud bearer authorization", request.path)
		}
		if request.host == hubHost && (request.path == httpapi.HubCreateSignalingPath || request.path == httpapi.HubCompleteSignalingPath) && request.authorization == "" {
			t.Fatalf("Hub managed request %s did not receive edge authorization", request.path)
		}
		for _, forbidden := range [][]byte{
			privateKey,
			[]byte(base64.StdEncoding.EncodeToString(privateKey)),
			[]byte(base64.RawURLEncoding.EncodeToString(privateKey)),
			[]byte("capability-grant-sentinel"),
			[]byte("terminal-payload-sentinel"),
		} {
			if len(forbidden) > 0 && bytes.Contains(request.body, forbidden) {
				t.Fatalf("cloud request %s contained forbidden private/data-plane material", request.path)
			}
		}
		if request.host == hubHost {
			for _, token := range [][]byte{accountToken, deviceToken} {
				if bytes.Contains(request.body, token) || bytes.Contains(request.body, []byte(base64.StdEncoding.EncodeToString(token))) || bytes.Contains(request.body, []byte(base64.RawURLEncoding.EncodeToString(token))) {
					t.Fatalf("Hub request %s contained a Control Plane session token", request.path)
				}
			}
		}
	}
}
