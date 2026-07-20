package hubcontrol_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/lozzow/termx/private/cloud/control-plane/hubcontrol"
	"github.com/lozzow/termx/private/cloud/control-plane/hubregistry"
	cloudsqlite "github.com/lozzow/termx/private/cloud/control-plane/sqlite"
	cloudtopology "github.com/lozzow/termx/private/cloud/control-plane/topology"
	"github.com/lozzow/termx/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

func TestHubControlUsesRealStreamGenerationAndPersistentReportCursor(t *testing.T) {
	now := time.Now().UTC()
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	store, err := cloudsqlite.Open(filepath.Join(t.TempDir(), "controller.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	registry, _ := hubregistry.New(store)
	topologyService, _ := cloudtopology.New(registry, store)
	metadata := &cloudpb.EdgeDeploymentMetadata{EdgeDeploymentId: "edge-1", Region: "local-1", HubId: "hub-1", HubControlIdentityFingerprint: hubregistry.IdentityFingerprint(publicKey), RelayId: "relay-1", RelayControlIdentityFingerprint: "relay-fingerprint"}
	if err := registry.RegisterDeployment(context.Background(), hubregistry.Deployment{Metadata: metadata, ControlPublicKey: publicKey, Enabled: true, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Assign(context.Background(), &cloudpb.HubAssignment{DaemonDeviceId: "daemon-1", AccountId: "account-1", HubId: "hub-1", AssignmentEpoch: 1, NotBeforeUnixMillis: now.Add(-time.Minute).UnixMilli(), ExpiresAtUnixMillis: now.Add(time.Hour).UnixMilli()}, now); err != nil {
		t.Fatal(err)
	}
	if err := topologyService.PutDeviceOwnership(context.Background(), &cloudpb.CloudDevicePolicy{AccountId: "account-1", DeviceId: "daemon-1", DeviceKind: cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_DAEMON}); err != nil {
		t.Fatal(err)
	}
	publisher := hubcontrol.NewPublisher()
	if err := publisher.PublishFull(&cloudpb.FullProjectionSnapshot{HubId: "hub-1", ProjectionRevision: 1, SnapshotDigest: []byte("digest-1"), GeneratedAtUnixMillis: now.UnixMilli(), ExpiresAtUnixMillis: now.Add(time.Hour).UnixMilli()}); err != nil {
		t.Fatal(err)
	}
	server, err := hubcontrol.NewServer(hubcontrol.ServerConfig{Registry: registry, CursorStore: store, Publisher: publisher, Topology: topologyService, Clock: time.Now, Random: rand.Reader, ChallengeTTL: time.Minute, EnvelopeTTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	firstContext, cancelFirst := context.WithCancel(context.Background())
	defer cancelFirst()
	firstResponse, firstReady, firstFull := openControl(t, firstContext, httpServer.URL, metadata, privateKey)
	defer firstResponse.Body.Close()
	if firstReady.GetControlGeneration() != 1 || firstFull.GetFullProjection().GetProjectionRevision() != 1 {
		t.Fatalf("first stream = ready=%#v full=%#v", firstReady, firstFull)
	}

	secondContext, cancelSecond := context.WithCancel(context.Background())
	defer cancelSecond()
	secondResponse, secondReady, secondFull := openControl(t, secondContext, httpServer.URL, metadata, privateKey)
	defer secondResponse.Body.Close()
	if secondReady.GetControlGeneration() != 2 || secondFull.GetControlGeneration() != 2 {
		t.Fatalf("replacement generation = ready=%d full=%d", secondReady.GetControlGeneration(), secondFull.GetControlGeneration())
	}
	closed := make(chan error, 1)
	go func() { closed <- readFrame(firstResponse.Body, &cloudpb.HubControlEnvelope{}) }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("new generation did not close old control stream")
	}

	delta := &cloudpb.PolicyDelta{HubId: "hub-1", ProjectionRevision: 2, PreviousProjectionRevision: 1, ResultingDigest: []byte("digest-2"), GeneratedAtUnixMillis: time.Now().UnixMilli(), ExpiresAtUnixMillis: time.Now().Add(time.Minute).UnixMilli()}
	resultingFull := &cloudpb.FullProjectionSnapshot{HubId: "hub-1", ProjectionRevision: 2, SnapshotDigest: []byte("digest-2"), GeneratedAtUnixMillis: now.UnixMilli(), ExpiresAtUnixMillis: now.Add(time.Hour).UnixMilli()}
	if err := publisher.PublishDelta(delta, resultingFull); err != nil {
		t.Fatal(err)
	}
	third := &cloudpb.HubControlEnvelope{}
	if err := readFrame(secondResponse.Body, third); err != nil || third.GetSenderSequence() != 3 || third.GetPolicyDelta().GetProjectionRevision() != 2 {
		t.Fatalf("delta frame = (%#v, %v)", third, err)
	}

	thirdContext, cancelThird := context.WithCancel(context.Background())
	defer cancelThird()
	thirdResponse, thirdReady, thirdFull := openControl(t, thirdContext, httpServer.URL, metadata, privateKey)
	defer thirdResponse.Body.Close()
	if thirdReady.GetControlGeneration() != 3 || thirdFull.GetFullProjection().GetProjectionRevision() != 2 || !bytes.Equal(thirdFull.GetFullProjection().GetSnapshotDigest(), delta.GetResultingDigest()) {
		t.Fatalf("reconnected full = ready=%#v full=%#v", thirdReady, thirdFull)
	}

	report := &cloudpb.HubRuntimeEnvelope{HubId: "hub-1", EdgeDeploymentId: "edge-1", ControlGeneration: 3, SenderRole: cloudpb.ControlSenderRole_CONTROL_SENDER_ROLE_HUB, SenderSequence: 1, IssuedAtUnixMillis: time.Now().UnixMilli(), ExpiresAtUnixMillis: time.Now().Add(time.Minute).UnixMilli(), Payload: &cloudpb.HubRuntimeEnvelope_Reconciliation{Reconciliation: &cloudpb.ReconciliationDigest{HubId: "hub-1", ProjectionRevision: 2, ProjectionDigest: []byte("digest-2"), ObservedAtUnixMillis: time.Now().UnixMilli()}}}
	result, status := reportRuntime(t, httpServer.URL, report)
	if status != http.StatusOK || result.GetAcceptedSenderSequence() != 1 || result.GetFullSnapshotRequired() {
		t.Fatalf("report = status=%d result=%#v", status, result)
	}
	replayed, status := reportRuntime(t, httpServer.URL, proto.Clone(report).(*cloudpb.HubRuntimeEnvelope))
	if status != http.StatusOK || replayed.GetAcceptedSenderSequence() != 1 {
		t.Fatalf("idempotent replay = status=%d result=%#v", status, replayed)
	}
	conflict := proto.Clone(report).(*cloudpb.HubRuntimeEnvelope)
	conflict.GetReconciliation().ProjectionDigest = []byte("conflict")
	if _, status := reportRuntime(t, httpServer.URL, conflict); status != http.StatusConflict {
		t.Fatalf("conflicting replay status = %d", status)
	}
	topology := &cloudpb.HubTopologySnapshot{HubId: "hub-1", ControlGeneration: 3, TopologyRevision: 1, ObservedAtUnixMillis: now.UnixMilli(), Presences: []*cloudpb.PresenceProjection{{DaemonDeviceId: "daemon-1", ControlOwnerHubId: "hub-1", AssignmentEpoch: 1, PresenceSessionId: "presence-1", Availability: cloudpb.Availability_AVAILABILITY_ONLINE, Freshness: cloudpb.Freshness_FRESHNESS_FRESH, ObservationSource: cloudpb.ObservationSource_OBSERVATION_SOURCE_DAEMON_INVENTORY, ObservedAtUnixMillis: now.UnixMilli(), FreshUntilUnixMillis: now.Add(time.Minute).UnixMilli(), DaemonRuntimeGeneration: "runtime-1"}}}
	topologyPayload, _ := proto.MarshalOptions{Deterministic: true}.Marshal(topology)
	topologyDigest := sha256.Sum256(topologyPayload)
	topology.TopologyDigest = topologyDigest[:]
	topologyEnvelope := &cloudpb.HubRuntimeEnvelope{HubId: "hub-1", EdgeDeploymentId: "edge-1", ControlGeneration: 3, SenderRole: cloudpb.ControlSenderRole_CONTROL_SENDER_ROLE_HUB, SenderSequence: 2, IssuedAtUnixMillis: time.Now().UnixMilli(), ExpiresAtUnixMillis: time.Now().Add(time.Minute).UnixMilli(), Payload: &cloudpb.HubRuntimeEnvelope_Topology{Topology: topology}}
	topologyResult, status := reportRuntime(t, httpServer.URL, topologyEnvelope)
	if status != http.StatusOK || topologyResult.GetAcceptedSenderSequence() != 2 {
		t.Fatalf("topology report = status=%d result=%#v", status, topologyResult)
	}
	accountID, projected, err := topologyService.Presence(context.Background(), "daemon-1")
	if err != nil || accountID != "account-1" || projected.GetAvailability() != cloudpb.Availability_AVAILABILITY_ONLINE {
		t.Fatalf("persisted topology = account=%q projection=%#v err=%v", accountID, projected, err)
	}
}

func openControl(t *testing.T, ctx context.Context, origin string, metadata *cloudpb.EdgeDeploymentMetadata, privateKey ed25519.PrivateKey) (*http.Response, *cloudpb.HubControlEnvelope, *cloudpb.HubControlEnvelope) {
	t.Helper()
	challengeResponse := &cloudpb.HubControlChallengeResponse{}
	status := unaryProto(t, context.Background(), origin+hubcontrol.ChallengePath, &cloudpb.HubControlChallengeRequest{HubId: metadata.GetHubId(), EdgeDeploymentId: metadata.GetEdgeDeploymentId(), HubControlIdentityFingerprint: metadata.GetHubControlIdentityFingerprint()}, challengeResponse)
	if status != http.StatusOK {
		t.Fatalf("challenge status = %d", status)
	}
	proof, err := hubcontrol.ChallengeProofBytes(challengeResponse.GetChallenge(), metadata)
	if err != nil {
		t.Fatal(err)
	}
	hello := &cloudpb.HubHello{Deployment: proto.Clone(metadata).(*cloudpb.EdgeDeploymentMetadata), ChallengeId: challengeResponse.GetChallenge().GetChallengeId(), ChallengeSignature: ed25519.Sign(privateKey, proof), SoftwareVersion: "test"}
	payload, _ := proto.Marshal(hello)
	request, _ := http.NewRequestWithContext(ctx, http.MethodPost, origin+hubcontrol.OpenPath, bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/x-protobuf")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		t.Fatalf("open status = %d: %s", response.StatusCode, body)
	}
	ready, full := &cloudpb.HubControlEnvelope{}, &cloudpb.HubControlEnvelope{}
	if err := readFrame(response.Body, ready); err != nil {
		t.Fatal(err)
	}
	if err := readFrame(response.Body, full); err != nil {
		t.Fatal(err)
	}
	return response, ready, full
}

func reportRuntime(t *testing.T, origin string, envelope *cloudpb.HubRuntimeEnvelope) (*cloudpb.ReportHubRuntimeResponse, int) {
	t.Helper()
	response := &cloudpb.ReportHubRuntimeResponse{}
	status := unaryProto(t, context.Background(), origin+hubcontrol.ReportPath, &cloudpb.ReportHubRuntimeRequest{Envelopes: []*cloudpb.HubRuntimeEnvelope{envelope}}, response)
	return response, status
}

func unaryProto(t *testing.T, ctx context.Context, endpoint string, input, output proto.Message) int {
	t.Helper()
	payload, _ := proto.Marshal(input)
	request, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/x-protobuf")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode == http.StatusOK {
		if err := proto.Unmarshal(body, output); err != nil {
			t.Fatal(err)
		}
	}
	return response.StatusCode
}

func readFrame(reader io.Reader, target proto.Message) error {
	var header [4]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return err
	}
	payload := make([]byte, binary.BigEndian.Uint32(header[:]))
	if _, err := io.ReadFull(reader, payload); err != nil {
		return err
	}
	return proto.Unmarshal(payload, target)
}
