package topology_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/lozzow/termx/private/cloud/control-plane/hubregistry"
	cloudsqlite "github.com/lozzow/termx/private/cloud/control-plane/sqlite"
	cloudtopology "github.com/lozzow/termx/private/cloud/control-plane/topology"
	"github.com/lozzow/termx/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

func TestTopologyDerivesAccountAndDegradesLostControlToUnknownStale(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	store, err := cloudsqlite.Open(filepath.Join(t.TempDir(), "topology.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	registry, _ := hubregistry.New(store)
	publicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	metadata := &cloudpb.EdgeDeploymentMetadata{EdgeDeploymentId: "edge-1", Region: "local", HubId: "hub-1", HubControlIdentityFingerprint: hubregistry.IdentityFingerprint(publicKey), RelayId: "relay-1", RelayControlIdentityFingerprint: "relay-fingerprint"}
	if err := registry.RegisterDeployment(ctx, hubregistry.Deployment{Metadata: metadata, ControlPublicKey: publicKey, Enabled: true, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Assign(ctx, &cloudpb.HubAssignment{DaemonDeviceId: "daemon-1", AccountId: "account-1", HubId: "hub-1", AssignmentEpoch: 1, NotBeforeUnixMillis: now.Add(-time.Minute).UnixMilli(), ExpiresAtUnixMillis: now.Add(time.Hour).UnixMilli()}, now); err != nil {
		t.Fatal(err)
	}
	service, _ := cloudtopology.New(registry, store)
	for _, policy := range []*cloudpb.CloudDevicePolicy{{AccountId: "account-1", DeviceId: "daemon-1", DeviceKind: cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_DAEMON, AuthEpoch: 1}, {AccountId: "account-1", DeviceId: "client-1", DeviceKind: cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_CLIENT, AuthEpoch: 1}} {
		if err := service.PutDeviceOwnership(ctx, policy); err != nil {
			t.Fatal(err)
		}
	}
	presence := &cloudpb.PresenceProjection{DaemonDeviceId: "daemon-1", ControlOwnerHubId: "hub-1", AssignmentEpoch: 1, PresenceSessionId: "presence-1", Availability: cloudpb.Availability_AVAILABILITY_ONLINE, Freshness: cloudpb.Freshness_FRESHNESS_FRESH, ObservationSource: cloudpb.ObservationSource_OBSERVATION_SOURCE_DAEMON_INVENTORY, ObservedAtUnixMillis: now.UnixMilli(), FreshUntilUnixMillis: now.Add(time.Minute).UnixMilli(), DaemonRuntimeGeneration: "runtime-1", RegistryRevision: 1}
	session := &cloudpb.ManagedPeerSessionProjection{Target: &cloudpb.ManagedPeerSessionTarget{DaemonDeviceId: "daemon-1", ManagedSessionId: "session-1", SessionIncarnation: 1, AssignmentEpoch: 1, ControlPresenceSessionId: "presence-1", DaemonRuntimeGeneration: "runtime-1"}, ClientDeviceId: "client-1", EstablishedPresenceSessionId: "presence-1", ControlOwnerHubId: "hub-1", ObservedDataPath: cloudpb.ObservedPath_OBSERVED_PATH_DIRECT, State: cloudpb.ManagedPeerSessionState_MANAGED_PEER_SESSION_STATE_READY, Freshness: cloudpb.Freshness_FRESHNESS_FRESH}
	snapshot := signedTopologySnapshot("hub-1", 1, 1, now, []*cloudpb.PresenceProjection{presence}, []*cloudpb.ManagedPeerSessionProjection{session})
	if err := service.Ingest(ctx, snapshot, now); err != nil {
		t.Fatal(err)
	}
	accountID, projected, err := service.Presence(ctx, "daemon-1")
	if err != nil || accountID != "account-1" || projected.GetAvailability() != cloudpb.Availability_AVAILABILITY_ONLINE || projected.GetFreshness() != cloudpb.Freshness_FRESHNESS_FRESH {
		t.Fatalf("stored Presence = account=%q projection=%#v err=%v", accountID, projected, err)
	}
	if err := service.MarkHubUnknown(ctx, "hub-1", 1, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	_, projected, err = service.Presence(ctx, "daemon-1")
	if err != nil || projected.GetAvailability() != cloudpb.Availability_AVAILABILITY_UNKNOWN || projected.GetFreshness() != cloudpb.Freshness_FRESHNESS_STALE || projected.GetObservationSource() != cloudpb.ObservationSource_OBSERVATION_SOURCE_CONTROL_STREAM_LOST {
		t.Fatalf("lost-control Presence = %#v err=%v", projected, err)
	}
	conflict := proto.Clone(snapshot).(*cloudpb.HubTopologySnapshot)
	conflict.Presences[0].Availability = cloudpb.Availability_AVAILABILITY_OFFLINE
	if err := service.Ingest(ctx, conflict, now); !errors.Is(err, cloudtopology.ErrTopologyRejected) {
		t.Fatalf("same revision digest conflict = %v", err)
	}
}

func signedTopologySnapshot(hubID string, generation, revision uint64, observedAt time.Time, presences []*cloudpb.PresenceProjection, sessions []*cloudpb.ManagedPeerSessionProjection) *cloudpb.HubTopologySnapshot {
	snapshot := &cloudpb.HubTopologySnapshot{HubId: hubID, ControlGeneration: generation, TopologyRevision: revision, ObservedAtUnixMillis: observedAt.UnixMilli(), Presences: presences, PeerSessions: sessions}
	payload, _ := proto.MarshalOptions{Deterministic: true}.Marshal(snapshot)
	digest := sha256.Sum256(payload)
	snapshot.TopologyDigest = digest[:]
	return snapshot
}
