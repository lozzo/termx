package controller

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	cloudcatalog "github.com/muxvia/muxvia/private/cloud/control-plane/catalog"
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
		Deployments: []DeploymentConfig{{Metadata: metadata, HubControlPublicKeyBase64: base64.RawStdEncoding.EncodeToString(hubPublic), RelayControlPublicKeyBase64: base64.RawStdEncoding.EncodeToString(relayPublic), PublicHubURL: "http://127.0.0.1:41002", HealthURL: "http://127.0.0.1:41002/healthz", MaxAssignments: 100}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close(context.Background())
	runtime.enrollment.candidateProvider = func(context.Context, time.Time, string) ([]enrollmentHubCandidate, error) {
		return []enrollmentHubCandidate{{value: &cloudpb.HubEnrollmentCandidate{HubId: "hub-1", HubUrl: "http://127.0.0.1:41002", HealthUrl: "http://127.0.0.1:41002/healthz", Region: "local-1"}, maxAssignments: 100}}, nil
	}
	readyCandidates := runtime.enrollment.candidateProvider
	activation, err := runtime.enrollment.CreateDaemonEnrollment(context.Background(), account.GetAccountId(), account.GetUserId())
	if err != nil {
		t.Fatal(err)
	}
	devicePublic, devicePrivate, _ := ed25519.GenerateKey(rand.Reader)
	begin := &cloudpb.BeginDeviceEnrollmentRequest{OneTimeCode: activation.GetUserCode(), DeviceId: "daemon-enrolled", DevicePublicKey: devicePublic, Metadata: &cloudpb.DeviceMetadata{DisplayName: "Test daemon", Platform: "test/arm64", MuxviaVersion: "test"}}
	restarted, err := newEnrollmentService(enrollmentServiceConfig{
		Commerce: runtime.enrollment.commerce, Topology: runtime.topology, Registry: runtime.registry, EnrollmentStore: runtime.store, EdgeIssuer: runtime.enrollment.edgeIssuer,
		CandidateProvider: readyCandidates,
		ControlKeyID:      "daemon-control-key", ControlPublicKey: daemonControlPublic, ControlNotBefore: runtime.enrollment.controlNotBefore, ControlNotAfter: runtime.enrollment.controlNotAfter, Now: time.Now, NotifyPolicyChange: func(string) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.begin(context.Background(), begin); !errors.Is(err, errEnrollmentExpired) {
		t.Fatalf("Controller restart retained pending enrollment flow: %v", err)
	}
	runtime.enrollment.candidateProvider = func(context.Context, time.Time, string) ([]enrollmentHubCandidate, error) { return nil, nil }
	temporary := &cloudpb.CloudError{}
	postControllerProto(t, runtime.Manifest().PublicURL+"/v1/enrollment/begin", begin, temporary, http.StatusServiceUnavailable)
	if temporary.GetCode() != cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY || !temporary.GetRetryable() {
		t.Fatalf("candidate outage error = %v", temporary)
	}
	// 候选故障不得消费十分钟 MXD flow；恢复 Hub attachment 后同一请求继续进入等待批准。
	runtime.enrollment.candidateProvider = readyCandidates
	challenge := &cloudpb.DeviceEnrollmentChallenge{}
	postControllerProto(t, runtime.Manifest().PublicURL+"/v1/enrollment/begin", begin, challenge, http.StatusOK)
	resumedChallenge := &cloudpb.DeviceEnrollmentChallenge{}
	postControllerProto(t, runtime.Manifest().PublicURL+"/v1/enrollment/begin", begin, resumedChallenge, http.StatusOK)
	if !proto.Equal(challenge, resumedChallenge) {
		t.Fatalf("repeated enrollment begin changed challenge: first=%v resumed=%v", challenge, resumedChallenge)
	}
	signedAt := time.Now().UTC()
	observations := []*cloudpb.HubReachabilityObservation{{HubId: "hub-1", Reachable: true, LatencyMillis: 5}}
	observationsDigest, err := cloudcompanion.EnrollmentObservationsDigest(observations)
	if err != nil {
		t.Fatal(err)
	}
	signingBytes, err := cloudcompanion.EnrollmentProofSigningBytes(&cloudpb.DeviceEnrollmentProofInput{FlowId: challenge.GetFlowId(), ChallengeId: challenge.GetChallengeId(), Challenge: challenge.GetChallenge(), DeviceId: "daemon-enrolled", DevicePublicKey: devicePublic, SignedAtUnixNano: signedAt.UnixNano(), CandidateSetDigest: challenge.GetCandidateSetDigest(), PreferredHubId: "hub-1", HubObservationsDigest: observationsDigest, FlowRevision: challenge.GetFlowRevision()})
	if err != nil {
		t.Fatal(err)
	}
	complete := &cloudpb.CompleteDeviceEnrollmentRequest{FlowId: challenge.GetFlowId(), Proof: &cloudpb.DeviceProof{DeviceId: "daemon-enrolled", DevicePublicKey: devicePublic, ChallengeId: challenge.GetChallengeId(), Signature: ed25519.Sign(devicePrivate, signingBytes), SignedAtUnixNano: signedAt.UnixNano()}, HubObservations: observations, PreferredHubId: "hub-1", CandidateSetDigest: challenge.GetCandidateSetDigest(), FlowRevision: challenge.GetFlowRevision()}
	result := &cloudpb.DeviceEnrollmentServiceSession{}
	completed := make(chan struct{})
	go func() {
		defer close(completed)
		postControllerProto(t, runtime.Manifest().PublicURL+"/v1/enrollment/complete", complete, result, http.StatusOK)
	}()
	time.Sleep(20 * time.Millisecond)
	if _, err := runtime.enrollment.ApproveDaemonEnrollment(context.Background(), account.GetAccountId(), activation.GetUserCode()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-completed:
	case <-time.After(time.Second):
		t.Fatal("enrollment completion did not wake after Web approval")
	}
	if result.GetSession().GetAccountId() != account.GetAccountId() || result.GetSession().GetDeviceId() != "daemon-enrolled" || result.GetHubUrl() != "http://127.0.0.1:41002" || len(result.GetRefreshToken()) < 32 || result.GetRefreshExpiresAtUnixMillis() <= time.Now().UnixMilli() || !bytes.Equal(result.GetControlEnrollment().GetVerificationKeys()[0].GetPublicKey(), daemonControlPublic) {
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
	// 最终事务已提交但 HTTP 响应丢失时，同一 daemon 可以恢复 challenge 并领取完全相同的 credential，
	// 不能再次写 session 或轮换刚签发的 refresh token。
	retryChallenge := &cloudpb.DeviceEnrollmentChallenge{}
	postControllerProto(t, runtime.Manifest().PublicURL+"/v1/enrollment/begin", begin, retryChallenge, http.StatusOK)
	if !proto.Equal(challenge, retryChallenge) {
		t.Fatalf("completed enrollment challenge changed: first=%v retry=%v", challenge, retryChallenge)
	}
	retriedResult := &cloudpb.DeviceEnrollmentServiceSession{}
	postControllerProto(t, runtime.Manifest().PublicURL+"/v1/enrollment/complete", complete, retriedResult, http.StatusOK)
	if !proto.Equal(result, retriedResult) {
		t.Fatalf("completed enrollment response changed: first=%v retry=%v", result, retriedResult)
	}
	// 使用新的 MXD code 再次 enrollment，验证注册码只描述授权事务，不会创建第二台 daemon。
	replacementActivation, err := runtime.enrollment.CreateDaemonEnrollment(context.Background(), account.GetAccountId(), account.GetUserId())
	if err != nil {
		t.Fatal(err)
	}
	replacementBegin := proto.Clone(begin).(*cloudpb.BeginDeviceEnrollmentRequest)
	replacementBegin.OneTimeCode = replacementActivation.GetUserCode()
	replacementChallenge := &cloudpb.DeviceEnrollmentChallenge{}
	postControllerProto(t, runtime.Manifest().PublicURL+"/v1/enrollment/begin", replacementBegin, replacementChallenge, http.StatusOK)
	replacementSignedAt := time.Now().UTC()
	replacementObservationsDigest, err := cloudcompanion.EnrollmentObservationsDigest(observations)
	if err != nil {
		t.Fatal(err)
	}
	replacementSigningBytes, err := cloudcompanion.EnrollmentProofSigningBytes(&cloudpb.DeviceEnrollmentProofInput{FlowId: replacementChallenge.GetFlowId(), ChallengeId: replacementChallenge.GetChallengeId(), Challenge: replacementChallenge.GetChallenge(), DeviceId: begin.GetDeviceId(), DevicePublicKey: devicePublic, SignedAtUnixNano: replacementSignedAt.UnixNano(), CandidateSetDigest: replacementChallenge.GetCandidateSetDigest(), PreferredHubId: "hub-1", HubObservationsDigest: replacementObservationsDigest, FlowRevision: replacementChallenge.GetFlowRevision()})
	if err != nil {
		t.Fatal(err)
	}
	replacementComplete := &cloudpb.CompleteDeviceEnrollmentRequest{FlowId: replacementChallenge.GetFlowId(), Proof: &cloudpb.DeviceProof{DeviceId: begin.GetDeviceId(), DevicePublicKey: devicePublic, ChallengeId: replacementChallenge.GetChallengeId(), Signature: ed25519.Sign(devicePrivate, replacementSigningBytes), SignedAtUnixNano: replacementSignedAt.UnixNano()}, HubObservations: observations, PreferredHubId: "hub-1", CandidateSetDigest: replacementChallenge.GetCandidateSetDigest(), FlowRevision: replacementChallenge.GetFlowRevision()}
	if _, err := runtime.enrollment.ApproveDaemonEnrollment(context.Background(), account.GetAccountId(), replacementActivation.GetUserCode()); err != nil {
		t.Fatal(err)
	}
	replacementResult := &cloudpb.DeviceEnrollmentServiceSession{}
	postControllerProto(t, runtime.Manifest().PublicURL+"/v1/enrollment/complete", replacementComplete, replacementResult, http.StatusOK)
	if replacementResult.GetSession().GetDeviceId() != begin.GetDeviceId() {
		t.Fatalf("replacement enrollment changed daemon identity: %v", replacementResult)
	}
	// Web/Hub 目录和后续管理命令始终以稳定 DeviceIdentity 的 device_id 为目标。
	accountDevices, err := runtime.topology.ListAccountDevices(context.Background(), account.GetAccountId(), cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_DAEMON, true, 10)
	if err != nil || len(accountDevices) != 1 || accountDevices[0].GetDeviceId() != begin.GetDeviceId() {
		t.Fatalf("stable daemon device projection = (%v, %v)", accountDevices, err)
	}

	registered, err := runtime.enrollment.commerce.Register(context.Background(), &cloudpb.RegisterAccountRequest{Email: "controller-migration@example.com", Password: "secure-password"})
	if err != nil {
		t.Fatal(err)
	}
	nextAccount := registered.GetSession().GetAccount()
	migrationFlow, err := runtime.enrollment.CreateDaemonEnrollment(context.Background(), nextAccount.GetAccountId(), nextAccount.GetUserId())
	if err != nil {
		t.Fatal(err)
	}
	migrationBegin := proto.Clone(begin).(*cloudpb.BeginDeviceEnrollmentRequest)
	migrationBegin.OneTimeCode = migrationFlow.GetUserCode()
	migrationChallenge := &cloudpb.DeviceEnrollmentChallenge{}
	postControllerProto(t, runtime.Manifest().PublicURL+"/v1/enrollment/begin", migrationBegin, migrationChallenge, http.StatusOK)
	migrationSignedAt := time.Now().UTC()
	migrationObservations := []*cloudpb.HubReachabilityObservation{{HubId: "hub-1", Reachable: true, LatencyMillis: 5}}
	migrationObservationsDigest, err := cloudcompanion.EnrollmentObservationsDigest(migrationObservations)
	if err != nil {
		t.Fatal(err)
	}
	migrationSigningBytes, err := cloudcompanion.EnrollmentProofSigningBytes(&cloudpb.DeviceEnrollmentProofInput{FlowId: migrationChallenge.GetFlowId(), ChallengeId: migrationChallenge.GetChallengeId(), Challenge: migrationChallenge.GetChallenge(), DeviceId: "daemon-enrolled", DevicePublicKey: devicePublic, SignedAtUnixNano: migrationSignedAt.UnixNano(), CandidateSetDigest: migrationChallenge.GetCandidateSetDigest(), PreferredHubId: "hub-1", HubObservationsDigest: migrationObservationsDigest, FlowRevision: migrationChallenge.GetFlowRevision()})
	if err != nil {
		t.Fatal(err)
	}
	migrationComplete := &cloudpb.CompleteDeviceEnrollmentRequest{FlowId: migrationChallenge.GetFlowId(), Proof: &cloudpb.DeviceProof{DeviceId: "daemon-enrolled", DevicePublicKey: devicePublic, ChallengeId: migrationChallenge.GetChallengeId(), Signature: ed25519.Sign(devicePrivate, migrationSigningBytes), SignedAtUnixNano: migrationSignedAt.UnixNano()}, HubObservations: migrationObservations, PreferredHubId: "hub-1", CandidateSetDigest: migrationChallenge.GetCandidateSetDigest(), FlowRevision: migrationChallenge.GetFlowRevision()}
	if _, err := runtime.enrollment.ApproveDaemonEnrollment(context.Background(), nextAccount.GetAccountId(), migrationFlow.GetUserCode()); err != nil {
		t.Fatal(err)
	}
	// 活跃 daemon 不能仅凭另一个账号生成的 MXD 被抢占，即使请求持有本机 DeviceIdentity private key。
	postControllerProto(t, runtime.Manifest().PublicURL+"/v1/enrollment/complete", migrationComplete, &cloudpb.CloudError{}, http.StatusConflict)
	currentOwner, err := runtime.topology.Device(context.Background(), "daemon-enrolled")
	if err != nil {
		t.Fatal(err)
	}
	// 被撤销设备可能在用户决定迁移前已经超过 24 小时 assignment lease；迁移应续签同一 Hub，
	// 不能要求用户删除本地 DeviceIdentity，也不能让过期 lease 变成永久锁定。
	expiredAt := time.Now().UTC().Add(-time.Minute)
	expiredAssignment, err := runtime.registry.Assign(context.Background(), &cloudpb.HubAssignment{
		DaemonDeviceId: "daemon-enrolled", AccountId: account.GetAccountId(), HubId: assignment.Value.GetHubId(), AssignmentEpoch: assignment.Value.GetAssignmentEpoch() + 1,
		NotBeforeUnixMillis: expiredAt.Add(-time.Hour).UnixMilli(), ExpiresAtUnixMillis: expiredAt.UnixMilli(),
	}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.topology.PutDeviceOwnership(context.Background(), &cloudpb.CloudDevicePolicy{AccountId: currentOwner.AccountID, DeviceId: currentOwner.DeviceID, DeviceKind: currentOwner.Kind, AuthEpoch: currentOwner.AuthEpoch + 1, Revoked: true, PublicKey: append([]byte(nil), currentOwner.PublicKey...)}); err != nil {
		t.Fatal(err)
	}
	migrated := &cloudpb.DeviceEnrollmentServiceSession{}
	postControllerProto(t, runtime.Manifest().PublicURL+"/v1/enrollment/complete", migrationComplete, migrated, http.StatusOK)
	migratedOwner, err := runtime.topology.Device(context.Background(), "daemon-enrolled")
	if err != nil || migratedOwner.AccountID != nextAccount.GetAccountId() || migratedOwner.Revoked || !bytes.Equal(migratedOwner.PublicKey, devicePublic) {
		t.Fatalf("migrated daemon ownership = (%v, %v)", migratedOwner, err)
	}
	migratedAssignment, err := runtime.registry.Assignment(context.Background(), "daemon-enrolled")
	if err != nil || migratedAssignment.Value.GetAccountId() != nextAccount.GetAccountId() || migratedAssignment.Value.GetHubId() != "hub-1" || migratedAssignment.Value.GetAssignmentEpoch() != expiredAssignment.Value.GetAssignmentEpoch()+1 || migratedAssignment.Value.GetExpiresAtUnixMillis() <= time.Now().UnixMilli() {
		t.Fatalf("migrated daemon assignment = (%v, %v)", migratedAssignment, err)
	}
	if _, err := runtime.enrollment.commerce.RefreshDeviceSession(context.Background(), result.GetRefreshToken()); !errors.Is(err, cloudcommerce.ErrUnauthorized) {
		t.Fatalf("old account daemon refresh survived ownership migration: %v", err)
	}
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
	config := Config{PostgresDSN: postgrestest.DSN(t, filepath.Join(t.TempDir(), "controller-postgres")), PublicListen: "127.0.0.1:0", InternalControlListen: "127.0.0.1:0", OperatorListen: "127.0.0.1:0", CatalogPath: "../web-controller/config/plans.json", ProjectionKeyID: "controller-key", ProjectionPrivateKeyBase64: base64.RawStdEncoding.EncodeToString(projectionPrivate), DaemonControlKeyID: "daemon-control-key", DaemonControlPrivateKeyBase64: base64.RawStdEncoding.EncodeToString(daemonControlPrivate), Deployments: []DeploymentConfig{{Metadata: metadata, HubControlPublicKeyBase64: base64.RawStdEncoding.EncodeToString(hubPublic), RelayControlPublicKeyBase64: base64.RawStdEncoding.EncodeToString(relayPublic), PublicHubURL: "http://127.0.0.1:41002", HealthURL: "http://127.0.0.1:41002/healthz", MaxAssignments: 100}}}
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
	catalogPath := configuredTestCatalogPath(t)
	account := seedControllerAccount(t, databaseKey, catalogPath, now)
	config := Config{PostgresDSN: postgrestest.DSN(t, databaseKey), PublicListen: "127.0.0.1:0", InternalControlListen: "127.0.0.1:0", OperatorListen: "127.0.0.1:0", CatalogPath: catalogPath, ProjectionKeyID: "controller-key", ProjectionPrivateKeyBase64: base64.RawStdEncoding.EncodeToString(projectionPrivate), DaemonControlKeyID: "daemon-control-key", DaemonControlPrivateKeyBase64: base64.RawStdEncoding.EncodeToString(daemonControlPrivate), EnableTestPaymentProvider: true, Deployments: []DeploymentConfig{{Metadata: metadata, HubControlPublicKeyBase64: base64.RawStdEncoding.EncodeToString(hubPublic), RelayControlPublicKeyBase64: base64.RawStdEncoding.EncodeToString(relayPublic), PublicHubURL: "http://127.0.0.1:41002", HealthURL: "http://127.0.0.1:41002/healthz", MaxAssignments: 100}}, Devices: []*cloudpb.CloudDevicePolicy{{AccountId: account.GetAccountId(), DeviceId: "daemon-1", DeviceKind: cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_DAEMON, AuthEpoch: account.GetAuthRevision()}}, Assignments: []*cloudpb.HubAssignment{{DaemonDeviceId: "daemon-1", AccountId: account.GetAccountId(), HubId: "hub-1", AssignmentEpoch: 1, NotBeforeUnixMillis: now.Add(-time.Minute).UnixMilli(), ExpiresAtUnixMillis: now.Add(time.Hour).UnixMilli()}}}
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
	checkoutBody, _ := protojson.Marshal(&cloudpb.CreateCheckoutRequest{PlanId: "pro", RequestedTransition: cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_UPGRADE, BillingCadence: cloudpb.BillingCadence_BILLING_CADENCE_MONTHLY})
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
	catalogSource, _ := cloudcatalog.NewSnapshotSource(catalog.Contract())
	service, err := cloudcommerce.New(cloudcommerce.Config{Store: store, Catalog: catalogSource, Now: func() time.Time { return now }})
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

func configuredTestCatalogPath(t *testing.T) string {
	t.Helper()
	catalog, err := webcontroller.LoadCatalog("../web-controller/config/plans.json")
	if err != nil {
		t.Fatal(err)
	}
	monthly, yearly := int64(1000), int64(10000)
	for index := range catalog.Plans {
		if catalog.Plans[index].ID == "pro" {
			catalog.Plans[index].Price = webcontroller.CatalogPrice{Mode: "configured", Label: "$10", MonthlyMinor: &monthly, YearlyMinor: &yearly}
			catalog.Plans[index].CreemProductID = "prod_controller_runtime_test"
		}
	}
	body, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "plans.json")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
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
