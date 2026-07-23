package main

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

	managedadapter "github.com/muxvia/muxvia/client/adapter/managed"
	"github.com/muxvia/muxvia/private/cloud/control-plane/hubregistry"
	cloudpostgres "github.com/muxvia/muxvia/private/cloud/control-plane/postgres"
	"github.com/muxvia/muxvia/private/cloud/control-plane/servicecredential"
	"github.com/muxvia/muxvia/private/cloud/controller"
	"github.com/muxvia/muxvia/proto/cloudpb"
	"github.com/muxvia/muxvia/shared/cloudcompanion"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func TestDaemonEnrollmentSelectsOneOfTwoIndependentEdges(t *testing.T) {
	root := findRepoRoot(t)
	manifestPath := filepath.Join(t.TempDir(), "runtime.json")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx, []string{"--manifest", manifestPath, "--repo-root", root}) }()
	var manifest supervisorManifest
	if err := waitManifest(ctx, manifestPath, &manifest, done); err != nil {
		cancel()
		t.Fatal(err)
	}
	defer func() {
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Errorf("supervisor exit = %v", err)
		}
	}()
	if len(manifest.Edges) != 2 {
		t.Fatalf("Edge count = %d", len(manifest.Edges))
	}
	credentialsPayload, err := os.ReadFile(manifest.CredentialsPath)
	if err != nil {
		t.Fatal(err)
	}
	var credentials developmentCredentials
	if json.Unmarshal(credentialsPayload, &credentials) != nil {
		t.Fatal("decode development credentials")
	}
	cookies := loginDevelopmentAccount(t, manifest.Controller.PublicURL, credentials.AccountEmail, credentials.AccountPassword)
	createBody, _ := protojson.Marshal(&cloudpb.CreateDaemonEnrollmentRequest{})
	createRequest := authenticatedCommandRequest(http.MethodPost, manifest.Controller.PublicURL+"/api/v1/daemon-enrollments/create", createBody, manifest.Controller.PublicURL, cookies, true)
	createResponse, err := http.DefaultClient.Do(createRequest)
	if err != nil || createResponse.StatusCode != http.StatusCreated {
		if createResponse != nil {
			createResponse.Body.Close()
		}
		t.Fatalf("create daemon enrollment = (%v, %v)", createResponse, err)
	}
	projection := &cloudpb.DaemonEnrollmentProjection{}
	if protojson.Unmarshal(readResponse(t, createResponse), projection) != nil || projection.GetUserCode() == "" {
		t.Fatal("decode daemon enrollment projection")
	}
	createResponse.Body.Close()

	devicePublic, devicePrivate, _ := ed25519.GenerateKey(rand.Reader)
	begin := &cloudpb.BeginDeviceEnrollmentRequest{OneTimeCode: projection.GetUserCode(), DeviceId: "daemon-hub-selection", DevicePublicKey: devicePublic, Metadata: &cloudpb.DeviceMetadata{DisplayName: "Hub selection daemon", Platform: "test/amd64", MuxviaVersion: "test"}}
	challenge := &cloudpb.DeviceEnrollmentChallenge{}
	postEnrollmentProto(t, manifest.Controller.PublicURL+"/v1/enrollment/begin", begin, challenge, http.StatusOK)
	if len(challenge.GetHubCandidates()) != 2 {
		t.Fatalf("Hub candidates = %v", challenge.GetHubCandidates())
	}
	observations, err := managedadapter.ProbeEnrollmentCandidates(context.Background(), challenge.GetHubCandidates())
	if err != nil {
		t.Fatal(err)
	}
	for _, observation := range observations {
		if !observation.GetReachable() || observation.GetLatencyMillis() == 0 {
			t.Fatalf("real Edge health observation = %v", observation)
		}
	}
	approveBody, _ := protojson.Marshal(&cloudpb.ApproveDaemonEnrollmentRequest{UserCode: projection.GetUserCode()})
	approveRequest := authenticatedCommandRequest(http.MethodPost, manifest.Controller.PublicURL+"/api/v1/daemon-enrollments/approve", approveBody, manifest.Controller.PublicURL, cookies, true)
	approveResponse, err := http.DefaultClient.Do(approveRequest)
	if err != nil || approveResponse.StatusCode != http.StatusOK {
		if approveResponse != nil {
			approveResponse.Body.Close()
		}
		t.Fatalf("approve daemon enrollment = (%v, %v)", approveResponse, err)
	}
	approveResponse.Body.Close()
	signedAt := time.Now().UTC()
	signingBytes, err := cloudcompanion.EnrollmentProofSigningBytes(&cloudpb.DeviceEnrollmentProofInput{FlowId: challenge.GetFlowId(), ChallengeId: challenge.GetChallengeId(), Challenge: challenge.GetChallenge(), DeviceId: begin.GetDeviceId(), DevicePublicKey: devicePublic, SignedAtUnixNano: signedAt.UnixNano()})
	if err != nil {
		t.Fatal(err)
	}
	complete := &cloudpb.CompleteDeviceEnrollmentRequest{FlowId: challenge.GetFlowId(), Proof: &cloudpb.DeviceProof{DeviceId: begin.GetDeviceId(), DevicePublicKey: devicePublic, ChallengeId: challenge.GetChallengeId(), Signature: ed25519.Sign(devicePrivate, signingBytes), SignedAtUnixNano: signedAt.UnixNano()}, HubObservations: observations}
	result := &cloudpb.DeviceEnrollmentServiceSession{}
	postEnrollmentProto(t, manifest.Controller.PublicURL+"/v1/enrollment/complete", complete, result, http.StatusOK)
	selectedCandidate := enrollmentCandidateByID(challenge.GetHubCandidates(), result.GetHubId())
	if selectedCandidate == nil || result.GetHubUrl() != selectedCandidate.GetHubUrl() || result.GetHubRegion() != selectedCandidate.GetRegion() {
		t.Fatalf("selected Hub directory = %v candidates=%v", result, challenge.GetHubCandidates())
	}

	controllerConfig, err := controller.LoadConfig(processByName(t, manifest.Processes, "controller").ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	privateKey, err := base64.RawStdEncoding.DecodeString(controllerConfig.ProjectionPrivateKeyBase64)
	if err != nil || len(privateKey) != ed25519.PrivateKeySize {
		t.Fatal("decode Controller projection key")
	}
	keyRing, err := servicecredential.NewKeyRing(servicecredential.VerificationKey{ID: controllerConfig.ProjectionKeyID, PublicKey: ed25519.PrivateKey(privateKey).Public().(ed25519.PublicKey), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := servicecredential.VerifyEdgeAccess(keyRing, result.GetAccessToken(), servicecredential.EdgeAccessExpectation{Issuer: "muxvia-cloud-controller", AudienceHubID: result.GetHubId(), AccountID: result.GetSession().GetAccountId(), ClientDeviceID: begin.GetDeviceId(), PrincipalKind: servicecredential.EdgePrincipalDaemon}, time.Now().UTC()); err != nil {
		t.Fatalf("selected Hub token audience: %v", err)
	}
	store, err := cloudpostgres.Open(context.Background(), controllerConfig.PostgresDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	registry, _ := hubregistry.New(store)
	assignment, err := registry.Assignment(context.Background(), begin.GetDeviceId())
	if err != nil || assignment.Value.GetHubId() != result.GetHubId() || assignment.Value.GetAssignmentEpoch() != 1 {
		t.Fatalf("persisted selected assignment = (%v, %v)", assignment, err)
	}
}

func loginDevelopmentAccount(t *testing.T, origin, email, password string) map[string]*http.Cookie {
	t.Helper()
	body, _ := protojson.Marshal(&cloudpb.PasswordLoginRequest{Email: email, Password: password})
	request, _ := http.NewRequest(http.MethodPost, origin+"/api/v1/account/login", bytes.NewReader(body))
	request.Header.Set("Origin", origin)
	response, err := http.DefaultClient.Do(request)
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("development account login = (%v, %v)", response, err)
	}
	defer response.Body.Close()
	result := map[string]*http.Cookie{}
	for _, cookie := range response.Cookies() {
		result[cookie.Name] = cookie
	}
	return result
}

func postEnrollmentProto(t *testing.T, endpoint string, request, response proto.Message, expected int) {
	t.Helper()
	payload, err := proto.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	httpRequest, _ := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(payload))
	httpRequest.Header.Set("Content-Type", "application/x-protobuf")
	httpResponse, err := http.DefaultClient.Do(httpRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer httpResponse.Body.Close()
	body, _ := io.ReadAll(httpResponse.Body)
	if httpResponse.StatusCode != expected {
		t.Fatalf("POST %s status=%d body=%s", endpoint, httpResponse.StatusCode, body)
	}
	if err := proto.Unmarshal(body, response); err != nil {
		t.Fatal(err)
	}
}

func enrollmentCandidateByID(candidates []*cloudpb.HubEnrollmentCandidate, hubID string) *cloudpb.HubEnrollmentCandidate {
	for _, candidate := range candidates {
		if candidate.GetHubId() == hubID {
			return candidate
		}
	}
	return nil
}
