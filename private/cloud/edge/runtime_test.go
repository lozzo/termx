package edge

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/muxvia/muxvia/private/cloud/control-plane/hubcontrol"
	"github.com/muxvia/muxvia/private/cloud/control-plane/hubregistry"
	postgrestest "github.com/muxvia/muxvia/private/cloud/control-plane/postgrestest"
	"github.com/muxvia/muxvia/private/cloud/control-plane/relaycontrol"
	"github.com/muxvia/muxvia/private/cloud/control-plane/servicecredential"
	cloudtopology "github.com/muxvia/muxvia/private/cloud/control-plane/topology"
	"github.com/muxvia/muxvia/private/cloud/control-plane/usage"
	cloudhub "github.com/muxvia/muxvia/private/cloud/hub"
	cloudrelay "github.com/muxvia/muxvia/private/cloud/relay"
	"github.com/muxvia/muxvia/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

func TestEdgeRestartRequiresFullSyncAndKeepsOnlyUsageOutbox(t *testing.T) {
	now := time.Now().UTC()
	hubPublic, hubPrivate, _ := ed25519.GenerateKey(rand.Reader)
	relayPublic, relayPrivate, _ := ed25519.GenerateKey(rand.Reader)
	controllerPublic, controllerPrivate, _ := ed25519.GenerateKey(rand.Reader)
	store, _ := postgrestest.Open(t, filepath.Join(t.TempDir(), "controller-postgres"))
	defer store.Close()
	registry, _ := hubregistry.New(store)
	topologyService, _ := cloudtopology.New(registry, store)
	metadata := &cloudpb.EdgeDeploymentMetadata{EdgeDeploymentId: "edge-1", Region: "local-1", HubId: "hub-1", HubControlIdentityFingerprint: hubregistry.IdentityFingerprint(hubPublic), RelayId: "relay-1", RelayControlIdentityFingerprint: hubregistry.IdentityFingerprint(relayPublic)}
	if err := registry.RegisterDeployment(context.Background(), hubregistry.Deployment{Metadata: metadata, ControlPublicKey: hubPublic, RelayControlPublicKey: relayPublic, Enabled: true, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	publisher := hubcontrol.NewPublisher()
	full, err := hubcontrol.BuildSignedFullProjection(hubcontrol.FullProjectionInput{HubID: "hub-1", Revision: 1, GeneratedAt: now, TTL: 30 * time.Minute, SigningKeyID: "controller-key", SigningKey: controllerPrivate,
		Accounts:    []*cloudpb.HubAccountPolicy{{AccountId: "account-1", AuthEpoch: 1, EntitlementStatus: cloudpb.EntitlementStatus_ENTITLEMENT_STATUS_ACTIVE, EntitlementEffectiveUntilUnixMillis: now.Add(time.Hour).UnixMilli(), Capability: &cloudpb.PlanCapability{ManagedP2PEnabled: true, ManagedP2PMaxConcurrency: 1, CloudDeviceLimit: 2}}},
		Devices:     []*cloudpb.CloudDevicePolicy{{AccountId: "account-1", DeviceId: "client-1", DeviceKind: cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_CLIENT, AuthEpoch: 1}, {AccountId: "account-1", DeviceId: "daemon-1", DeviceKind: cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_DAEMON, AuthEpoch: 1}},
		Assignments: []*cloudpb.HubAssignment{{DaemonDeviceId: "daemon-1", AccountId: "account-1", HubId: "hub-1", AssignmentEpoch: 1, NotBeforeUnixMillis: now.Add(-time.Minute).UnixMilli(), ExpiresAtUnixMillis: now.Add(time.Hour).UnixMilli()}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := publisher.PublishFull(full); err != nil {
		t.Fatal(err)
	}
	control, err := hubcontrol.NewServer(hubcontrol.ServerConfig{Registry: registry, CursorStore: store, Publisher: publisher, Topology: topologyService, Clock: time.Now, Random: rand.Reader, ChallengeTTL: time.Minute, EnvelopeTTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	relayPublisher := relaycontrol.NewPublisher()
	relayControl, err := relaycontrol.NewServer(relaycontrol.ServerConfig{Registry: registry, CursorStore: store, Publisher: relayPublisher, Results: relayResultSink{}, Clock: time.Now, Random: rand.Reader, ChallengeTTL: time.Minute, EnvelopeTTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	var usageAvailable atomic.Bool
	mux := http.NewServeMux()
	mux.Handle("/v1/relay/control/", relayControl.Handler())
	mux.Handle("/", control.Handler())
	mux.HandleFunc(usage.InternalReportPath, func(writer http.ResponseWriter, request *http.Request) {
		if !usageAvailable.Load() {
			http.Error(writer, "usage unavailable", http.StatusServiceUnavailable)
			return
		}
		body, _ := io.ReadAll(request.Body)
		payload := &cloudpb.ReportRelayUsageRequest{}
		_ = proto.Unmarshal(body, payload)
		response := &cloudpb.ReportRelayUsageResponse{}
		for _, record := range payload.GetRecords() {
			response.Acknowledgements = append(response.Acknowledgements, &cloudpb.RelayUsageAck{EventId: record.GetEvent().GetEventId(), Sequence: record.GetEvent().GetSequence()})
		}
		encoded, _ := proto.Marshal(response)
		writer.Header().Set("Content-Type", "application/x-protobuf")
		_, _ = writer.Write(encoded)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	outboxPath := filepath.Join(t.TempDir(), "usage.outbox")
	config := Config{ControllerURL: server.URL, HubListen: "127.0.0.1:0", HealthListen: "127.0.0.1:0", RelayListen: "127.0.0.1:0", UsageOutboxPath: outboxPath, Metadata: metadata, HubControlPrivateKeyBase64: base64.RawStdEncoding.EncodeToString(hubPrivate), RelayControlPrivateKeyBase64: base64.RawStdEncoding.EncodeToString(relayPrivate), ControllerProjectionKeyID: "controller-key", ControllerProjectionPublicKeyBase64: base64.RawStdEncoding.EncodeToString(controllerPublic)}
	first, err := Start(config)
	if err != nil {
		t.Fatal(err)
	}
	ready, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	if err := first.WaitReady(ready); err != nil {
		cancel()
		t.Fatal(err)
	}
	cancel()
	if first.Manifest().ControlGeneration != 1 || first.Manifest().RelayControlGeneration != 1 || first.Manifest().ProjectionRevision != 1 {
		t.Fatalf("first Edge manifest = %#v", first.Manifest())
	}
	signer, err := servicecredential.NewSigner("controller-key", controllerPrivate, now.Add(-time.Minute), now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := servicecredential.NewEdgeAccessIssuer("muxvia-cloud-controller", signer)
	if err != nil {
		t.Fatal(err)
	}
	token, err := issuer.IssueEdgeAccess("token-1", "hub-1", "account-1", "client-1", 1, time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.authorizer.ReserveManagedP2P(token, "account-1", "client-1", "daemon-1", "reservation-1", "managed-1", 1, now.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := first.authorizer.ReserveManagedP2P(token, "account-1", "client-1", "daemon-1", "reservation-2", "managed-2", 1, now.Add(5*time.Minute)); !errors.Is(err, cloudhub.ErrP2PConcurrency) {
		t.Fatalf("signed policy concurrency = %v", err)
	}
	first.authorizer.ReleaseManagedP2P("reservation-1")
	suspended, err := hubcontrol.BuildSignedFullProjection(hubcontrol.FullProjectionInput{HubID: "hub-1", Revision: 2, GeneratedAt: time.Now().UTC(), TTL: 30 * time.Minute, SigningKeyID: "controller-key", SigningKey: controllerPrivate,
		Accounts:    []*cloudpb.HubAccountPolicy{{AccountId: "account-1", AuthEpoch: 1, EntitlementStatus: cloudpb.EntitlementStatus_ENTITLEMENT_STATUS_SUSPENDED, EntitlementEffectiveUntilUnixMillis: now.Add(time.Hour).UnixMilli(), Capability: &cloudpb.PlanCapability{ManagedP2PEnabled: true, ManagedP2PMaxConcurrency: 1, CloudDeviceLimit: 2}}},
		Devices:     full.GetDevices(),
		Assignments: full.GetAssignments(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := publisher.PublishFull(suspended); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for first.Manifest().ProjectionRevision < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if first.Manifest().ProjectionRevision != 2 {
		t.Fatalf("Edge did not apply suspended policy: %#v", first.Manifest())
	}
	if _, err := first.authorizer.ReserveManagedP2P(token, "account-1", "client-1", "daemon-1", "reservation-3", "managed-3", 1, now.Add(5*time.Minute)); !errors.Is(err, cloudhub.ErrP2PNotEntitled) {
		t.Fatalf("suspended signed policy admission = %v", err)
	}
	record := cloudrelay.UsageRecord{SignedLease: []byte("signed-lease"), Event: usage.Event{EventID: "usage-1", LeaseID: "lease-1", ManagedSessionID: "managed-1", RelayID: "relay-1", PathKind: servicecredential.RelayPathSingle, HopID: "relay-1", Sequence: 1, IntervalStartUnix: now.Unix(), IntervalEndUnix: now.Add(time.Second).Unix(), ActiveSeconds: 1, KeyID: "usage-key", Signature: []byte("signature")}}
	if err := first.usageOutbox.Enqueue(record); err != nil {
		t.Fatal(err)
	}
	closeContext, closeCancel := context.WithTimeout(context.Background(), 3*time.Second)
	if err := first.Close(closeContext); err != nil {
		closeCancel()
		t.Fatal(err)
	}
	closeCancel()

	second, err := Start(config)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close(context.Background())
	ready, cancel = context.WithTimeout(context.Background(), 3*time.Second)
	if err := second.WaitReady(ready); err != nil {
		cancel()
		t.Fatal(err)
	}
	cancel()
	if second.Manifest().ControlGeneration != 2 || second.Manifest().RelayControlGeneration != 2 || second.Manifest().ProjectionRevision != 2 {
		t.Fatalf("restarted Edge manifest = %#v", second.Manifest())
	}
	pending, err := second.usageOutbox.Pending()
	if err != nil || len(pending) != 1 || pending[0].Event.EventID != "usage-1" {
		t.Fatalf("restored usage outbox = (%#v, %v)", pending, err)
	}
	usageAvailable.Store(true)
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		pending, err = second.usageOutbox.Pending()
		if err == nil && len(pending) == 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil || len(pending) != 0 {
		t.Fatalf("restarted usage pump did not ack durable outbox: (%#v, %v)", pending, err)
	}
	server.CloseClientConnections()
	_ = server.Listener.Close()
	deadline = time.Now().Add(2 * time.Second)
	for second.control.State().Attached && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	response, err := http.Get(second.Manifest().HealthURL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent || !second.projection.Ready() {
		t.Fatalf("disconnected-fresh health = %d, projection ready=%v", response.StatusCode, second.projection.Ready())
	}
	closeContext, closeCancel = context.WithTimeout(context.Background(), 3*time.Second)
	if err := second.Close(closeContext); err != nil {
		closeCancel()
		t.Fatal(err)
	}
	closeCancel()
	third, err := Start(config)
	if err != nil {
		t.Fatal(err)
	}
	defer third.Close(context.Background())
	ready, cancel = context.WithTimeout(context.Background(), 200*time.Millisecond)
	if err := third.WaitReady(ready); !errors.Is(err, context.DeadlineExceeded) {
		cancel()
		t.Fatalf("Edge restart without Controller readiness = %v", err)
	}
	cancel()
	response, err = http.Get(third.Manifest().HealthURL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable || third.projection.Snapshot().Revision != 0 {
		t.Fatalf("Controller-less restart health=%d revision=%d", response.StatusCode, third.projection.Snapshot().Revision)
	}
}

type relayResultSink struct{}

func (relayResultSink) IngestRelayResult(context.Context, *cloudpb.RelayCommandResult, time.Time) error {
	return nil
}

func TestEdgeDeploymentWindowAndPublicHubURLStayExplicit(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	notBefore := now.Add(-2 * time.Hour)
	notAfter := now.Add(30 * 24 * time.Hour)
	gotBefore, gotAfter, err := credentialWindow(now, notBefore.UnixMilli(), notAfter.UnixMilli())
	if err != nil || !gotBefore.Equal(notBefore) || !gotAfter.Equal(notAfter) {
		t.Fatalf("credential window = (%s, %s, %v)", gotBefore, gotAfter, err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	publicURL, err := normalizedPublicHubURL("https://cn1.edge.muxvia.com:41102/", listener)
	if err != nil || publicURL != "https://cn1.edge.muxvia.com:41102" {
		t.Fatalf("public Hub URL = (%q, %v)", publicURL, err)
	}
	if _, err := normalizedPublicHubURL("https://user@cn1.edge.muxvia.com/path", listener); err == nil {
		t.Fatal("Hub URL with userinfo/path was accepted")
	}
}
