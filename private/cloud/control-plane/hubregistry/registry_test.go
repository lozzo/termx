package hubregistry_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/lozzow/termx/private/cloud/control-plane/hubregistry"
	cloudsqlite "github.com/lozzow/termx/private/cloud/control-plane/sqlite"
	"github.com/lozzow/termx/proto/cloudpb"
)

func TestRegistryFencesGenerationAndCrossHubAssignment(t *testing.T) {
	now := time.Date(2026, 7, 20, 20, 0, 0, 0, time.UTC)
	store, err := cloudsqlite.Open(filepath.Join(t.TempDir(), "controller.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	registry, _ := hubregistry.New(store)
	fingerprints := map[string]string{}
	for _, hubID := range []string{"hub-a", "hub-b"} {
		publicKey, _, _ := ed25519.GenerateKey(rand.Reader)
		fingerprints[hubID] = hubregistry.IdentityFingerprint(publicKey)
		metadata := &cloudpb.EdgeDeploymentMetadata{EdgeDeploymentId: "edge-" + hubID, Region: "local-1", HubId: hubID, HubControlIdentityFingerprint: fingerprints[hubID], RelayId: "relay-" + hubID, RelayControlIdentityFingerprint: "relay-fingerprint-" + hubID}
		if err := registry.RegisterDeployment(context.Background(), hubregistry.Deployment{Metadata: metadata, ControlPublicKey: publicKey, Enabled: true, UpdatedAt: now}); err != nil {
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
	path := filepath.Join(t.TempDir(), "controller.db")
	store, _ := cloudsqlite.Open(path)
	registry, _ := hubregistry.New(store)
	if _, err := registry.Assign(context.Background(), assignment("hub-a", 1, now.Add(-time.Minute), now.Add(time.Second)), now); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	reopened, _ := cloudsqlite.Open(path)
	defer reopened.Close()
	registry, _ = hubregistry.New(reopened)
	moved, err := registry.Assign(context.Background(), assignment("hub-b", 2, now.Add(2*time.Second), now.Add(time.Minute)), now.Add(2*time.Second))
	if err != nil || moved.Value.GetHubId() != "hub-b" {
		t.Fatalf("expired migration = (%#v, %v)", moved, err)
	}
}

func hello(hubID, fingerprint string) *cloudpb.HubHello {
	return &cloudpb.HubHello{Deployment: &cloudpb.EdgeDeploymentMetadata{HubId: hubID, EdgeDeploymentId: "edge-" + hubID, HubControlIdentityFingerprint: fingerprint}}
}

func assignment(hubID string, epoch uint64, from, until time.Time) *cloudpb.HubAssignment {
	return &cloudpb.HubAssignment{DaemonDeviceId: "daemon-1", AccountId: "account-1", HubId: hubID, AssignmentEpoch: epoch, NotBeforeUnixMillis: from.UnixMilli(), ExpiresAtUnixMillis: until.UnixMilli()}
}
