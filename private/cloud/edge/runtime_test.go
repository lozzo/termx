package edge

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/lozzow/termx/private/cloud/control-plane/hubcontrol"
	"github.com/lozzow/termx/private/cloud/control-plane/hubregistry"
	cloudsqlite "github.com/lozzow/termx/private/cloud/control-plane/sqlite"
	cloudtopology "github.com/lozzow/termx/private/cloud/control-plane/topology"
	"github.com/lozzow/termx/private/cloud/control-plane/usage"
	cloudrelay "github.com/lozzow/termx/private/cloud/relay"
	"github.com/lozzow/termx/proto/cloudpb"
)

func TestEdgeRestartRequiresFullSyncAndKeepsOnlyUsageOutbox(t *testing.T) {
	now := time.Now().UTC()
	hubPublic, hubPrivate, _ := ed25519.GenerateKey(rand.Reader)
	controllerPublic, controllerPrivate, _ := ed25519.GenerateKey(rand.Reader)
	store, _ := cloudsqlite.Open(filepath.Join(t.TempDir(), "controller.db"))
	defer store.Close()
	registry, _ := hubregistry.New(store)
	topologyService, _ := cloudtopology.New(registry, store)
	metadata := &cloudpb.EdgeDeploymentMetadata{EdgeDeploymentId: "edge-1", Region: "local-1", HubId: "hub-1", HubControlIdentityFingerprint: hubregistry.IdentityFingerprint(hubPublic), RelayId: "relay-1", RelayControlIdentityFingerprint: "relay-fingerprint"}
	if err := registry.RegisterDeployment(context.Background(), hubregistry.Deployment{Metadata: metadata, ControlPublicKey: hubPublic, Enabled: true, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	publisher := hubcontrol.NewPublisher()
	full, err := hubcontrol.BuildSignedFullProjection(hubcontrol.FullProjectionInput{HubID: "hub-1", Revision: 1, GeneratedAt: now, TTL: 30 * time.Minute, SigningKeyID: "controller-key", SigningKey: controllerPrivate,
		Accounts:    []*cloudpb.HubAccountPolicy{{AccountId: "account-1", AuthEpoch: 1, EntitlementStatus: cloudpb.EntitlementStatus_ENTITLEMENT_STATUS_ACTIVE, EntitlementEffectiveUntilUnixMillis: now.Add(time.Hour).UnixMilli(), Capability: &cloudpb.PlanCapability{ManagedP2PEnabled: true, ManagedP2PMaxConcurrency: 1, CloudDeviceLimit: 2}}},
		Devices:     []*cloudpb.CloudDevicePolicy{{AccountId: "account-1", DeviceId: "daemon-1", DeviceKind: cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_DAEMON, AuthEpoch: 1}},
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
	server := httptest.NewServer(control.Handler())
	defer server.Close()
	outboxPath := filepath.Join(t.TempDir(), "usage.outbox")
	config := Config{ControllerURL: server.URL, HubListen: "127.0.0.1:0", HealthListen: "127.0.0.1:0", RelayListen: "127.0.0.1:0", UsageOutboxPath: outboxPath, Metadata: metadata, HubControlPrivateKeyBase64: base64.RawStdEncoding.EncodeToString(hubPrivate), ControllerProjectionKeyID: "controller-key", ControllerProjectionPublicKeyBase64: base64.RawStdEncoding.EncodeToString(controllerPublic)}
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
	if first.Manifest().ControlGeneration != 1 || first.Manifest().ProjectionRevision != 1 {
		t.Fatalf("first Edge manifest = %#v", first.Manifest())
	}
	record := cloudrelay.UsageRecord{SignedLease: []byte("signed-lease"), Event: usage.Event{EventID: "usage-1", Sequence: 1, Signature: []byte("signature")}}
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
	if second.Manifest().ControlGeneration != 2 || second.Manifest().ProjectionRevision != 1 {
		t.Fatalf("restarted Edge manifest = %#v", second.Manifest())
	}
	pending, err := second.usageOutbox.Pending()
	if err != nil || len(pending) != 1 || pending[0].Event.EventID != "usage-1" {
		t.Fatalf("restored usage outbox = (%#v, %v)", pending, err)
	}
	server.CloseClientConnections()
	_ = server.Listener.Close()
	deadline := time.Now().Add(2 * time.Second)
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
