package controller

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"io"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	cloudcommerce "github.com/muxvia/muxvia/private/cloud/control-plane/commerce"
	"github.com/muxvia/muxvia/private/cloud/control-plane/hubcontrol"
	"github.com/muxvia/muxvia/private/cloud/control-plane/hubregistry"
	postgrestest "github.com/muxvia/muxvia/private/cloud/control-plane/postgrestest"
	"github.com/muxvia/muxvia/private/cloud/control-plane/servicecredential"
	webcontroller "github.com/muxvia/muxvia/private/cloud/web-controller"
	"github.com/muxvia/muxvia/proto/cloudpb"
	"github.com/muxvia/muxvia/shared/cloudcompanion"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func TestControllerEnrollmentBindsControlKeyAndPersistsDaemonAuthority(t *testing.T) {
	now := time.Now().UTC()
	hubPublic, _, _ := ed25519.GenerateKey(rand.Reader)
	relayPublic, _, _ := ed25519.GenerateKey(rand.Reader)
	projectionPublic, projectionPrivate, _ := ed25519.GenerateKey(rand.Reader)
	daemonControlPublic, daemonControlPrivate, _ := ed25519.GenerateKey(rand.Reader)
	metadata := &cloudpb.EdgeDeploymentMetadata{EdgeDeploymentId: "edge-1", Region: "local-1", HubId: "hub-1", HubControlIdentityFingerprint: hubregistry.IdentityFingerprint(hubPublic), RelayId: "relay-1", RelayControlIdentityFingerprint: hubregistry.IdentityFingerprint(relayPublic)}
	databaseKey := filepath.Join(t.TempDir(), "controller-postgres")
	catalogPath := "../web-controller/config/plans.json"
	account := seedControllerAccount(t, databaseKey, catalogPath, now)
	runtime, err := Start(Config{
		PostgresDSN: postgrestest.DSN(t, databaseKey), PublicListen: "127.0.0.1:0", InternalControlListen: "127.0.0.1:0", OperatorListen: "127.0.0.1:0", CatalogPath: catalogPath,
		ProjectionKeyID: "controller-key", ProjectionPrivateKeyBase64: base64.RawStdEncoding.EncodeToString(projectionPrivate), DaemonControlKeyID: "daemon-control-key", DaemonControlPrivateKeyBase64: base64.RawStdEncoding.EncodeToString(daemonControlPrivate),
		Deployments:               []DeploymentConfig{{Metadata: metadata, HubControlPublicKeyBase64: base64.RawStdEncoding.EncodeToString(hubPublic), RelayControlPublicKeyBase64: base64.RawStdEncoding.EncodeToString(relayPublic)}},
		DevelopmentEnrollmentCode: "one-time-code", DevelopmentEnrollmentAccountID: account.GetAccountId(), DevelopmentEnrollmentHubID: "hub-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close(context.Background())
	devicePublic, devicePrivate, _ := ed25519.GenerateKey(rand.Reader)
	begin := &cloudpb.BeginDeviceEnrollmentRequest{OneTimeCode: "one-time-code", DevicePublicKey: devicePublic, Metadata: &cloudpb.DeviceMetadata{DisplayName: "Test daemon", Platform: "test/arm64", MuxviaVersion: "test"}}
	challenge := &cloudpb.DeviceEnrollmentChallenge{}
	postControllerProto(t, runtime.Manifest().PublicURL+"/v1/enrollment/begin", begin, challenge, http.StatusOK)
	signedAt := time.Now().UTC()
	signingBytes, err := cloudcompanion.EnrollmentProofSigningBytes(&cloudpb.DeviceEnrollmentProofInput{FlowId: challenge.GetFlowId(), ChallengeId: challenge.GetChallengeId(), Challenge: challenge.GetChallenge(), DeviceId: "daemon-enrolled", DevicePublicKey: devicePublic, SignedAtUnixNano: signedAt.UnixNano()})
	if err != nil {
		t.Fatal(err)
	}
	complete := &cloudpb.CompleteDeviceEnrollmentRequest{FlowId: challenge.GetFlowId(), Proof: &cloudpb.DeviceProof{DeviceId: "daemon-enrolled", DevicePublicKey: devicePublic, ChallengeId: challenge.GetChallengeId(), Signature: ed25519.Sign(devicePrivate, signingBytes), SignedAtUnixNano: signedAt.UnixNano()}}
	result := &cloudpb.DeviceEnrollmentServiceSession{}
	postControllerProto(t, runtime.Manifest().PublicURL+"/v1/enrollment/complete", complete, result, http.StatusOK)
	if result.GetSession().GetAccountId() != account.GetAccountId() || result.GetSession().GetDeviceId() != "daemon-enrolled" || !bytes.Equal(result.GetControlEnrollment().GetVerificationKeys()[0].GetPublicKey(), daemonControlPublic) {
		t.Fatalf("enrollment result = %v", result)
	}
	keyRing, _ := servicecredential.NewKeyRing(servicecredential.VerificationKey{ID: "controller-key", PublicKey: projectionPublic, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(24 * time.Hour)})
	if _, err := servicecredential.VerifyEdgeAccess(keyRing, result.GetAccessToken(), servicecredential.EdgeAccessExpectation{Issuer: "muxvia-cloud-controller", AudienceHubID: "hub-1", AccountID: account.GetAccountId(), ClientDeviceID: "daemon-enrolled", PrincipalKind: servicecredential.EdgePrincipalDaemon}, time.Now().UTC()); err != nil {
		t.Fatalf("verify daemon edge credential: %v", err)
	}
	owner, err := runtime.topology.Device(context.Background(), "daemon-enrolled")
	if err != nil || owner.AccountID != account.GetAccountId() || !bytes.Equal(owner.PublicKey, devicePublic) {
		t.Fatalf("persisted daemon ownership = (%v, %v)", owner, err)
	}
	assignment, err := runtime.registry.Assignment(context.Background(), "daemon-enrolled")
	if err != nil || assignment.Value.GetHubId() != "hub-1" {
		t.Fatalf("persisted assignment = (%v, %v)", assignment, err)
	}
	postControllerProto(t, runtime.Manifest().PublicURL+"/v1/enrollment/begin", begin, &cloudpb.CloudError{}, http.StatusForbidden)
}

func postControllerProto(t *testing.T, endpoint string, request, response proto.Message, expectedStatus int) {
	t.Helper()
	payload, err := proto.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	httpRequest, _ := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(payload))
	httpRequest.Header.Set("Content-Type", cloudProtoMediaType)
	httpResponse, err := http.DefaultClient.Do(httpRequest)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(httpResponse.Body)
	httpResponse.Body.Close()
	if httpResponse.StatusCode != expectedStatus {
		t.Fatalf("POST %s = %d: %s", endpoint, httpResponse.StatusCode, body)
	}
	if err := proto.Unmarshal(body, response); err != nil {
		t.Fatal(err)
	}
}

func TestControllerRetriesSamePolicyRevisionAfterBackpressure(t *testing.T) {
	publisher := hubcontrol.NewPublisher()
	updates, cancel := publisher.Subscribe("hub-1")
	defer cancel()
	for revision := uint64(1); revision <= 16; revision++ {
		if err := publisher.PublishFull(&cloudpb.FullProjectionSnapshot{HubId: "hub-1", ProjectionRevision: revision, SnapshotDigest: []byte{byte(revision)}}); err != nil {
			t.Fatal(err)
		}
	}
	runtime := &Runtime{publisher: publisher, policyDone: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		done <- runtime.publishFullWithRetry(&cloudpb.FullProjectionSnapshot{HubId: "hub-1", ProjectionRevision: 17, SnapshotDigest: []byte{17}})
	}()
	select {
	case err := <-done:
		t.Fatalf("publish returned before backpressure cleared: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	<-updates
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("policy publish was not retried")
	}
	if head, _ := publisher.Head("hub-1"); head.Revision != 17 {
		t.Fatalf("retried head = %#v", head)
	}
}

func TestControllerPeriodicallyRefreshesSignedProjection(t *testing.T) {
	hubPublic, _, _ := ed25519.GenerateKey(rand.Reader)
	relayPublic, _, _ := ed25519.GenerateKey(rand.Reader)
	_, projectionPrivate, _ := ed25519.GenerateKey(rand.Reader)
	_, daemonControlPrivate, _ := ed25519.GenerateKey(rand.Reader)
	metadata := &cloudpb.EdgeDeploymentMetadata{EdgeDeploymentId: "edge-1", Region: "local-1", HubId: "hub-1", HubControlIdentityFingerprint: hubregistry.IdentityFingerprint(hubPublic), RelayId: "relay-1", RelayControlIdentityFingerprint: hubregistry.IdentityFingerprint(relayPublic)}
	config := Config{PostgresDSN: postgrestest.DSN(t, filepath.Join(t.TempDir(), "controller-postgres")), PublicListen: "127.0.0.1:0", InternalControlListen: "127.0.0.1:0", OperatorListen: "127.0.0.1:0", CatalogPath: "../web-controller/config/plans.json", ProjectionKeyID: "controller-key", ProjectionPrivateKeyBase64: base64.RawStdEncoding.EncodeToString(projectionPrivate), DaemonControlKeyID: "daemon-control-key", DaemonControlPrivateKeyBase64: base64.RawStdEncoding.EncodeToString(daemonControlPrivate), Deployments: []DeploymentConfig{{Metadata: metadata, HubControlPublicKeyBase64: base64.RawStdEncoding.EncodeToString(hubPublic), RelayControlPublicKeyBase64: base64.RawStdEncoding.EncodeToString(relayPublic)}}}
	runtime, err := start(config, 20*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close(context.Background())
	deadline := time.Now().Add(time.Second)
	for {
		head, _ := runtime.publisher.Head("hub-1")
		if head.Revision >= 2 {
			full, _ := runtime.publisher.CurrentFull("hub-1")
			if full.GetExpiresAtUnixMillis() <= full.GetGeneratedAtUnixMillis() {
				t.Fatalf("refreshed projection expiry = %v", full)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("periodic projection refresh did not run: %#v", head)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestControllerKeepsListenersSeparateAndProjectionRevisionPersistent(t *testing.T) {
	now := time.Now().UTC()
	hubPublic, _, _ := ed25519.GenerateKey(rand.Reader)
	relayPublic, _, _ := ed25519.GenerateKey(rand.Reader)
	projectionPublic, projectionPrivate, _ := ed25519.GenerateKey(rand.Reader)
	_, daemonControlPrivate, _ := ed25519.GenerateKey(rand.Reader)
	_ = projectionPublic
	metadata := &cloudpb.EdgeDeploymentMetadata{EdgeDeploymentId: "edge-1", Region: "local-1", HubId: "hub-1", HubControlIdentityFingerprint: hubregistry.IdentityFingerprint(hubPublic), RelayId: "relay-1", RelayControlIdentityFingerprint: hubregistry.IdentityFingerprint(relayPublic)}
	databaseKey := filepath.Join(t.TempDir(), "controller-postgres")
	catalogPath := "../web-controller/config/plans.json"
	account := seedControllerAccount(t, databaseKey, catalogPath, now)
	config := Config{PostgresDSN: postgrestest.DSN(t, databaseKey), PublicListen: "127.0.0.1:0", InternalControlListen: "127.0.0.1:0", OperatorListen: "127.0.0.1:0", CatalogPath: catalogPath, ProjectionKeyID: "controller-key", ProjectionPrivateKeyBase64: base64.RawStdEncoding.EncodeToString(projectionPrivate), DaemonControlKeyID: "daemon-control-key", DaemonControlPrivateKeyBase64: base64.RawStdEncoding.EncodeToString(daemonControlPrivate), EnableTestPaymentProvider: true, Deployments: []DeploymentConfig{{Metadata: metadata, HubControlPublicKeyBase64: base64.RawStdEncoding.EncodeToString(hubPublic), RelayControlPublicKeyBase64: base64.RawStdEncoding.EncodeToString(relayPublic)}}, Devices: []*cloudpb.CloudDevicePolicy{{AccountId: account.GetAccountId(), DeviceId: "daemon-1", DeviceKind: cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_DAEMON, AuthEpoch: account.GetAuthRevision()}}, Assignments: []*cloudpb.HubAssignment{{DaemonDeviceId: "daemon-1", AccountId: account.GetAccountId(), HubId: "hub-1", AssignmentEpoch: 1, NotBeforeUnixMillis: now.Add(-time.Minute).UnixMilli(), ExpiresAtUnixMillis: now.Add(time.Hour).UnixMilli()}}}
	first, err := Start(config)
	if err != nil {
		t.Fatal(err)
	}
	manifest := first.Manifest()
	if manifest.PublicURL == manifest.InternalControlURL || manifest.PublicURL == manifest.OperatorURL || manifest.InternalControlURL == manifest.OperatorURL {
		t.Fatalf("Controller listeners are not separated: %#v", manifest)
	}
	response, err := http.Get(manifest.PublicURL + "/api/v1/catalog")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("catalog status = %d", response.StatusCode)
	}
	registerBody, _ := protojson.Marshal(&cloudpb.RegisterAccountRequest{Email: "runtime@example.com", Password: "secure-password"})
	registerRequest, _ := http.NewRequest(http.MethodPost, manifest.PublicURL+"/api/v1/account/register", bytes.NewReader(registerBody))
	registerRequest.Header.Set("Origin", manifest.PublicURL)
	registerResponse, err := http.DefaultClient.Do(registerRequest)
	if err != nil {
		t.Fatal(err)
	}
	registerResponse.Body.Close()
	if registerResponse.StatusCode != http.StatusCreated {
		t.Fatalf("register status = %d", registerResponse.StatusCode)
	}
	if head, ok := first.publisher.Head("hub-1"); !ok || head.Revision != 1 {
		t.Fatalf("first projection head = %#v, %v", head, ok)
	}
	seedLoginBody, _ := protojson.Marshal(&cloudpb.PasswordLoginRequest{Email: "controller-fixture@example.com", Password: "secure-password"})
	seedLoginRequest, _ := http.NewRequest(http.MethodPost, manifest.PublicURL+"/api/v1/account/login", bytes.NewReader(seedLoginBody))
	seedLoginRequest.Header.Set("Origin", manifest.PublicURL)
	seedLoginResponse, err := http.DefaultClient.Do(seedLoginRequest)
	if err != nil {
		t.Fatal(err)
	}
	seedLoginResponse.Body.Close()
	if seedLoginResponse.StatusCode != http.StatusOK {
		t.Fatalf("seed login status = %d", seedLoginResponse.StatusCode)
	}
	cookies := seedLoginResponse.Cookies()
	csrf := ""
	for _, cookie := range cookies {
		if cookie.Name == "muxvia_cloud_csrf" {
			csrf = cookie.Value
		}
	}
	checkoutBody, _ := protojson.Marshal(&cloudpb.CreateCheckoutRequest{PlanId: "pro", RequestedTransition: cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_UPGRADE})
	checkoutResponse := productMutation(t, manifest.PublicURL+"/api/v1/checkout", manifest.PublicURL, csrf, cookies, checkoutBody)
	checkoutContract := &cloudpb.CreateCheckoutResponse{}
	if err := protojson.Unmarshal(checkoutResponse, checkoutContract); err != nil {
		t.Fatal(err)
	}
	confirmBody, _ := protojson.Marshal(&cloudpb.ConfirmTestPaymentRequest{OrderId: checkoutContract.GetOrder().GetOrderId(), EventType: cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_SUCCEEDED})
	_ = productMutation(t, manifest.PublicURL+"/api/v1/checkout/test-payment", manifest.PublicURL, csrf, cookies, confirmBody)
	deadline := time.Now().Add(2 * time.Second)
	for {
		head, _ := first.publisher.Head("hub-1")
		if head.Revision >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("payment did not publish Hub policy: %#v", head)
		}
		time.Sleep(10 * time.Millisecond)
	}
	full, ok := first.publisher.CurrentFull("hub-1")
	if !ok || len(full.GetAccounts()) != 1 || full.GetAccounts()[0].GetAccountId() != account.GetAccountId() || !full.GetAccounts()[0].GetCapability().GetStandardRelayEnabled() {
		t.Fatalf("upgraded Hub policy = %v", full)
	}
	closeContext, cancel := context.WithTimeout(context.Background(), time.Second)
	if err := first.Close(closeContext); err != nil {
		cancel()
		t.Fatal(err)
	}
	cancel()
	second, err := Start(config)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close(context.Background())
	if head, ok := second.publisher.Head("hub-1"); !ok || head.Revision != 3 {
		t.Fatalf("restarted projection head = %#v, %v", head, ok)
	}
	loginBody, _ := protojson.Marshal(&cloudpb.PasswordLoginRequest{Email: "runtime@example.com", Password: "secure-password"})
	loginRequest, _ := http.NewRequest(http.MethodPost, second.Manifest().PublicURL+"/api/v1/account/login", bytes.NewReader(loginBody))
	loginRequest.Header.Set("Origin", second.Manifest().PublicURL)
	loginResponse, err := http.DefaultClient.Do(loginRequest)
	if err != nil {
		t.Fatal(err)
	}
	responseBody, _ := io.ReadAll(loginResponse.Body)
	loginResponse.Body.Close()
	if loginResponse.StatusCode != http.StatusOK {
		t.Fatalf("login after Controller restart = %d: %s", loginResponse.StatusCode, responseBody)
	}
}

func seedControllerAccount(t *testing.T, databaseKey, catalogPath string, now time.Time) *cloudpb.AccountProjection {
	t.Helper()
	store, err := postgrestest.Open(t, databaseKey)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := webcontroller.LoadCatalog(catalogPath)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	service, err := cloudcommerce.New(cloudcommerce.Config{Store: store, Catalog: catalog.Contract(), Now: func() time.Time { return now }})
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	registered, err := service.Register(context.Background(), &cloudpb.RegisterAccountRequest{Email: "controller-fixture@example.com", Password: "secure-password"})
	if closeErr := store.Close(); err == nil && closeErr != nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	return registered.GetSession().GetAccount()
}

func productMutation(t *testing.T, endpoint, origin, csrf string, cookies []*http.Cookie, body []byte) []byte {
	t.Helper()
	request, _ := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	request.Header.Set("Origin", origin)
	request.Header.Set("X-Muxvia-CSRF", csrf)
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	responseBody, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		t.Fatalf("product mutation %s = %d: %s", endpoint, response.StatusCode, responseBody)
	}
	return responseBody
}

func TestControllerCredentialWindowUsesAbsoluteDeploymentBounds(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	notBefore := now.Add(-2 * time.Hour)
	notAfter := now.Add(30 * 24 * time.Hour)
	gotBefore, gotAfter, err := credentialWindow(now, notBefore.UnixMilli(), notAfter.UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	if !gotBefore.Equal(notBefore) || !gotAfter.Equal(notAfter) {
		t.Fatalf("credential window = (%s, %s)", gotBefore, gotAfter)
	}
	if _, _, err := credentialWindow(now, notBefore.UnixMilli(), now.UnixMilli()); err == nil {
		t.Fatal("inactive credential window was accepted")
	}
}
