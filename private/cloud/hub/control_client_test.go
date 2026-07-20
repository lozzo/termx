package hub

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/lozzow/termx/private/cloud/control-plane/hubcontrol"
	"github.com/lozzow/termx/private/cloud/control-plane/hubregistry"
	cloudsqlite "github.com/lozzow/termx/private/cloud/control-plane/sqlite"
	"github.com/lozzow/termx/proto/cloudpb"
)

func TestControlClientBootstrapsMemoryProjectionAndReportsReconciliation(t *testing.T) {
	now := time.Now().UTC()
	hubPublicKey, hubPrivateKey, _ := ed25519.GenerateKey(rand.Reader)
	controllerPublicKey, controllerPrivateKey, _ := ed25519.GenerateKey(rand.Reader)
	store, err := cloudsqlite.Open(filepath.Join(t.TempDir(), "controller.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	registry, _ := hubregistry.New(store)
	metadata := &cloudpb.EdgeDeploymentMetadata{EdgeDeploymentId: "edge-1", Region: "local-1", HubId: "hub-1", HubControlIdentityFingerprint: hubregistry.IdentityFingerprint(hubPublicKey), RelayId: "relay-1", RelayControlIdentityFingerprint: "relay-fingerprint"}
	if err := registry.RegisterDeployment(context.Background(), hubregistry.Deployment{Metadata: metadata, ControlPublicKey: hubPublicKey, Enabled: true, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	publisher := hubcontrol.NewPublisher()
	full, err := hubcontrol.BuildSignedFullProjection(hubcontrol.FullProjectionInput{
		HubID: "hub-1", Revision: 1, GeneratedAt: now, TTL: time.Hour, SigningKeyID: "controller-key", SigningKey: controllerPrivateKey,
		Accounts:    []*cloudpb.HubAccountPolicy{{AccountId: "account-1", AuthEpoch: 1, EntitlementStatus: cloudpb.EntitlementStatus_ENTITLEMENT_STATUS_ACTIVE, EntitlementEffectiveUntilUnixMillis: now.Add(time.Hour).UnixMilli(), Capability: &cloudpb.PlanCapability{ManagedP2PEnabled: true, ManagedP2PMaxConcurrency: 1, CloudDeviceLimit: 2}}},
		Devices:     []*cloudpb.CloudDevicePolicy{{AccountId: "account-1", DeviceId: "daemon-1", DeviceKind: cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_DAEMON, AuthEpoch: 1}},
		Assignments: []*cloudpb.HubAssignment{{DaemonDeviceId: "daemon-1", AccountId: "account-1", HubId: "hub-1", AssignmentEpoch: 1, NotBeforeUnixMillis: now.Add(-time.Minute).UnixMilli(), ExpiresAtUnixMillis: now.Add(10 * time.Minute).UnixMilli()}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := publisher.PublishFull(full); err != nil {
		t.Fatal(err)
	}
	controlServer, _ := hubcontrol.NewServer(hubcontrol.ServerConfig{Registry: registry, CursorStore: store, Publisher: publisher, Clock: time.Now, Random: rand.Reader, ChallengeTTL: time.Minute, EnvelopeTTL: time.Minute})
	httpServer := httptest.NewServer(controlServer.Handler())
	defer httpServer.Close()
	projection, err := NewProjection(ProjectionConfig{HubID: "hub-1", ControllerKeyID: "controller-key", ControllerPublicKey: controllerPublicKey, MaxStaleness: time.Hour, PolicySink: &projectionSink{}})
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewControlClient(ControlClientConfig{ControllerURL: httpServer.URL, Metadata: metadata, PrivateKey: hubPrivateKey, SoftwareVersion: "test", Projection: projection, MinBackoff: 10 * time.Millisecond, MaxBackoff: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- client.Run(ctx) }()
	waitProjectionRevision(t, projection, 1)
	waitCursor(t, store, 1)
	deltaNow := time.Now().UTC()
	deltaAssignment := &cloudpb.HubAssignment{DaemonDeviceId: "daemon-1", AccountId: "account-1", HubId: "hub-1", AssignmentEpoch: 2, NotBeforeUnixMillis: deltaNow.UnixMilli(), ExpiresAtUnixMillis: deltaNow.Add(time.Minute).UnixMilli()}
	delta, err := hubcontrol.BuildSignedDelta(hubcontrol.DeltaProjectionInput{HubID: "hub-1", Revision: 2, PreviousRevision: 1, GeneratedAt: deltaNow, TTL: 30 * time.Minute, SigningKeyID: "controller-key", SigningKey: controllerPrivateKey, AssignmentOperations: []*cloudpb.HubAssignmentDelta{{Operation: cloudpb.ProjectionDeltaOperation_PROJECTION_DELTA_OPERATION_UPSERT, DaemonDeviceId: "daemon-1", Assignment: deltaAssignment}}, ResultingAccounts: full.GetAccounts(), ResultingDevices: full.GetDevices(), ResultingAssignments: []*cloudpb.HubAssignment{deltaAssignment}})
	if err != nil {
		t.Fatal(err)
	}
	resultingFull, err := hubcontrol.BuildSignedFullProjection(hubcontrol.FullProjectionInput{HubID: "hub-1", Revision: 2, GeneratedAt: deltaNow, TTL: 30 * time.Minute, SigningKeyID: "controller-key", SigningKey: controllerPrivateKey, Accounts: full.GetAccounts(), Devices: full.GetDevices(), Assignments: []*cloudpb.HubAssignment{deltaAssignment}})
	if err != nil {
		t.Fatal(err)
	}
	if err := publisher.PublishDelta(delta, resultingFull); err != nil {
		t.Fatal(err)
	}
	waitProjectionRevision(t, projection, 2)
	waitCursor(t, store, 2)
	state := client.State()
	if !state.Attached || state.ControlGeneration != 1 || state.LastSequence != 3 {
		t.Fatalf("control client state = %#v", state)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v", err)
	}
}

func waitProjectionRevision(t *testing.T, projection *Projection, revision uint64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if projection.Snapshot().Revision == revision {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("projection revision = %d, want %d", projection.Snapshot().Revision, revision)
}

func waitCursor(t *testing.T, store *cloudsqlite.Store, sequence uint64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		accepted, _, err := store.ControlCursor(context.Background(), "hub-1", 1, cloudpb.ControlSenderRole_CONTROL_SENDER_ROLE_HUB)
		if err == nil && accepted == sequence {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	accepted, _, _ := store.ControlCursor(context.Background(), "hub-1", 1, cloudpb.ControlSenderRole_CONTROL_SENDER_ROLE_HUB)
	t.Fatalf("accepted cursor = %d, want %d", accepted, sequence)
}
