package hubregistry_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/muxvia/muxvia/private/cloud/control-plane/hubregistry"
	postgrestest "github.com/muxvia/muxvia/private/cloud/control-plane/postgrestest"
	"github.com/muxvia/muxvia/proto/cloudpb"
)

func TestRegistryFencesGenerationAndCrossHubAssignment(t *testing.T) {
	now := time.Date(2026, 7, 20, 20, 0, 0, 0, time.UTC)
	store, err := postgrestest.Open(t, filepath.Join(t.TempDir(), "controller-postgres"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	registry, _ := hubregistry.New(store)
	fingerprints := map[string]string{}
	relayFingerprints := map[string]string{}
	for _, hubID := range []string{"hub-a", "hub-b"} {
		publicKey, _, _ := ed25519.GenerateKey(rand.Reader)
		relayPublicKey, _, _ := ed25519.GenerateKey(rand.Reader)
		fingerprints[hubID] = hubregistry.IdentityFingerprint(publicKey)
		relayFingerprints[hubID] = hubregistry.IdentityFingerprint(relayPublicKey)
		metadata := &cloudpb.EdgeDeploymentMetadata{EdgeDeploymentId: "edge-" + hubID, Region: "local-1", HubId: hubID, HubControlIdentityFingerprint: fingerprints[hubID], RelayId: "relay-" + hubID, RelayControlIdentityFingerprint: relayFingerprints[hubID]}
		if err := registry.RegisterDeployment(context.Background(), hubregistry.Deployment{Metadata: metadata, ControlPublicKey: publicKey, RelayControlPublicKey: relayPublicKey, Enabled: true, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := registry.AttachHub(context.Background(), hello("hub-a", fingerprints["hub-a"]), now)
	if err != nil || first.ControlGeneration != 1 {
		t.Fatalf("first generation = (%#v, %v)", first, err)
	}
	second, err := registry.AttachHub(context.Background(), hello("hub-a", fingerprints["hub-a"]), now.Add(time.Second))
	if err != nil || second.ControlGeneration != 2 {
		t.Fatalf("second generation = (%#v, %v)", second, err)
	}
	if err := registry.RequireCurrentGeneration(context.Background(), "hub-a", 1); !errors.Is(err, hubregistry.ErrStaleControlGeneration) {
		t.Fatalf("old generation = %v", err)
	}
	relayHello := &cloudpb.RelayHello{Deployment: &cloudpb.EdgeDeploymentMetadata{RelayId: "relay-hub-a", EdgeDeploymentId: "edge-hub-a", RelayControlIdentityFingerprint: relayFingerprints["hub-a"]}}
	relayAttached, err := registry.AttachRelay(context.Background(), relayHello, now.Add(2*time.Second))
	if err != nil || relayAttached.RelayControlGeneration != 1 || relayAttached.ControlGeneration != 2 {
		t.Fatalf("independent Relay generation = (%#v, %v)", relayAttached, err)
	}
	if err := registry.RequireCurrentRelayGeneration(context.Background(), "relay-hub-a", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Assign(context.Background(), assignment("hub-a", 1, now, now.Add(time.Minute)), now); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Assign(context.Background(), assignment("hub-b", 2, now, now.Add(time.Minute)), now); !errors.Is(err, hubregistry.ErrAssignmentFenceRequired) {
		t.Fatalf("unfenced migration = %v", err)
	}
	if _, err := registry.Fence(context.Background(), "daemon-1", "hub-a", 1, now); err != nil {
		t.Fatal(err)
	}
	moved, err := registry.Assign(context.Background(), assignment("hub-b", 2, now, now.Add(time.Minute)), now)
	if err != nil || moved.Value.GetHubId() != "hub-b" || moved.PreviousHubID != "hub-a" {
		t.Fatalf("moved assignment = (%#v, %v)", moved, err)
	}
}

func TestRegistryAllowsMigrationAfterLeaseExpiryAndPersists(t *testing.T) {
	now := time.Date(2026, 7, 20, 21, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "controller-postgres")
	store, _ := postgrestest.Open(t, path)
	registry, _ := hubregistry.New(store)
	registerTestDeployment(t, registry, "hub-a", now)
	registerTestDeployment(t, registry, "hub-b", now)
	if _, err := registry.Assign(context.Background(), assignment("hub-a", 1, now.Add(-time.Minute), now.Add(time.Second)), now); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	reopened, _ := postgrestest.Open(t, path)
	defer reopened.Close()
	registry, _ = hubregistry.New(reopened)
	moved, err := registry.Assign(context.Background(), assignment("hub-b", 2, now.Add(2*time.Second), now.Add(time.Minute)), now.Add(2*time.Second))
	if err != nil || moved.Value.GetHubId() != "hub-b" {
		t.Fatalf("expired migration = (%#v, %v)", moved, err)
	}
}

func TestOperatorLifecycleRequiresApprovalDrainAndEmptyAssignments(t *testing.T) {
	now := time.Date(2026, 7, 25, 10, 0, 0, 0, time.UTC)
	store, err := postgrestest.Open(t, filepath.Join(t.TempDir(), "operator-hub-postgres"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	registry, _ := hubregistry.New(store)
	hubPublic, _, _ := ed25519.GenerateKey(rand.Reader)
	relayPublic, _, _ := ed25519.GenerateKey(rand.Reader)
	created, err := registry.CreateDeployment(context.Background(), &cloudpb.CreateHubDeploymentRequest{
		HubId: "hub-new", EdgeDeploymentId: "edge-new", RelayId: "relay-new", Region: "cn-1", PublicLabel: "China 1",
		PublicHubUrl: "https://hub-new.example.test", HealthUrl: "https://hub-new.example.test/healthz", MaxAssignments: 2,
		HubControlPublicKey: hubPublic, RelayControlPublicKey: relayPublic, Reason: "new region", RequestId: "request-create",
	}, "operator-1", now)
	if err != nil || created.DirectoryRevision != 1 || created.Enabled || created.IdentityApproved {
		t.Fatalf("created deployment = (%#v, %v)", created, err)
	}
	if _, err := registry.AttachHub(context.Background(), hello("hub-new", hubregistry.IdentityFingerprint(hubPublic)), now); !errors.Is(err, hubregistry.ErrDeploymentNotFound) {
		t.Fatalf("pending identity attached: %v", err)
	}
	approved, err := registry.ApproveDeploymentIdentity(context.Background(), &cloudpb.ApproveHubDeploymentIdentityRequest{HubId: "hub-new", ExpectedRevision: 1, HubControlIdentityFingerprint: hubregistry.IdentityFingerprint(hubPublic), RelayControlIdentityFingerprint: hubregistry.IdentityFingerprint(relayPublic), Reason: "fingerprints reviewed", RequestId: "request-approve"}, "operator-1", now.Add(time.Second))
	if err != nil || !approved.Enabled || !approved.IdentityApproved || approved.DirectoryRevision != 2 {
		t.Fatalf("approved deployment = (%#v, %v)", approved, err)
	}
	updated, err := registry.UpdateDeployment(context.Background(), &cloudpb.UpdateHubDeploymentRequest{HubId: "hub-new", ExpectedRevision: 2, Region: "cn-1", PublicLabel: "China Primary", PublicHubUrl: "https://hub-new.example.test", HealthUrl: "https://hub-new.example.test/ready", MaxAssignments: 3, Reason: "capacity review", RequestId: "request-update"}, "operator-1", now.Add(2*time.Second))
	if err != nil || updated.MaxAssignments != 3 || updated.Metadata.GetPublicLabel() != "China Primary" || updated.DirectoryRevision != 3 {
		t.Fatalf("updated deployment = (%#v, %v)", updated, err)
	}
	if _, err := registry.Assign(context.Background(), &cloudpb.HubAssignment{DaemonDeviceId: "daemon-1", AccountId: "account-1", HubId: "hub-new", AssignmentEpoch: 1, NotBeforeUnixMillis: now.UnixMilli(), ExpiresAtUnixMillis: now.Add(time.Minute).UnixMilli()}, now); err != nil {
		t.Fatal(err)
	}
	draining, err := registry.SetDeploymentDraining(context.Background(), &cloudpb.SetHubDeploymentDrainRequest{HubId: "hub-new", ExpectedRevision: 3, Draining: true, Reason: "maintenance", RequestId: "request-drain"}, "operator-1", now.Add(3*time.Second))
	if err != nil || !draining.Draining || draining.DirectoryRevision != 4 {
		t.Fatalf("draining deployment = (%#v, %v)", draining, err)
	}
	if _, err := registry.Assign(context.Background(), &cloudpb.HubAssignment{DaemonDeviceId: "daemon-2", AccountId: "account-1", HubId: "hub-new", AssignmentEpoch: 1, NotBeforeUnixMillis: now.UnixMilli(), ExpiresAtUnixMillis: now.Add(time.Minute).UnixMilli()}, now.Add(4*time.Second)); !errors.Is(err, hubregistry.ErrDeploymentLifecycle) {
		t.Fatalf("draining accepted new assignment: %v", err)
	}
	disable := &cloudpb.DisableHubDeploymentRequest{HubId: "hub-new", ExpectedRevision: 4, Reason: "maintenance complete", RequestId: "request-disable"}
	if _, err := registry.DisableDeployment(context.Background(), disable, "operator-1", now.Add(5*time.Second)); !errors.Is(err, hubregistry.ErrDeploymentAssignmentsRemain) {
		t.Fatalf("disable with assignment = %v", err)
	}
	disabled, err := registry.DisableDeployment(context.Background(), disable, "operator-1", now.Add(2*time.Minute))
	if err != nil || disabled.Enabled || !disabled.Archived || disabled.DirectoryRevision != 5 {
		t.Fatalf("disabled deployment = (%#v, %v)", disabled, err)
	}
}

func registerTestDeployment(t *testing.T, registry *hubregistry.Registry, hubID string, now time.Time) {
	t.Helper()
	publicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	relayPublicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	metadata := &cloudpb.EdgeDeploymentMetadata{EdgeDeploymentId: "edge-" + hubID, Region: "local-1", HubId: hubID, HubControlIdentityFingerprint: hubregistry.IdentityFingerprint(publicKey), RelayId: "relay-" + hubID, RelayControlIdentityFingerprint: hubregistry.IdentityFingerprint(relayPublicKey)}
	if err := registry.RegisterDeployment(context.Background(), hubregistry.Deployment{Metadata: metadata, ControlPublicKey: publicKey, RelayControlPublicKey: relayPublicKey, Enabled: true, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
}

func hello(hubID, fingerprint string) *cloudpb.HubHello {
	return &cloudpb.HubHello{Deployment: &cloudpb.EdgeDeploymentMetadata{HubId: hubID, EdgeDeploymentId: "edge-" + hubID, HubControlIdentityFingerprint: fingerprint}}
}

func assignment(hubID string, epoch uint64, from, until time.Time) *cloudpb.HubAssignment {
	return &cloudpb.HubAssignment{DaemonDeviceId: "daemon-1", AccountId: "account-1", HubId: hubID, AssignmentEpoch: epoch, NotBeforeUnixMillis: from.UnixMilli(), ExpiresAtUnixMillis: until.UnixMilli()}
}
