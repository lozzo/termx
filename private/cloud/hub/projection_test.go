package hub

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/lozzow/termx/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

type projectionSink struct {
	mu        sync.Mutex
	revisions []uint64
}

type projectionClock struct {
	mu  sync.Mutex
	now time.Time
}

func (clock *projectionClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *projectionClock) Set(now time.Time) {
	clock.mu.Lock()
	clock.now = now
	clock.mu.Unlock()
}

func (sink *projectionSink) ApplySnapshot(snapshot AuthorizationSnapshot) error {
	sink.mu.Lock()
	sink.revisions = append(sink.revisions, snapshot.Revision)
	sink.mu.Unlock()
	return nil
}

type projectionFence struct{ called chan uint64 }

func (fence projectionFence) FenceAssignment(_ string, epoch uint64) { fence.called <- epoch }

func TestProjectionFullDeltaAndConflictAreAtomic(t *testing.T) {
	now := time.Now().UTC()
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	clock := &projectionClock{now: now}
	sink := &projectionSink{}
	projection, err := NewProjection(ProjectionConfig{HubID: "hub-1", ControllerKeyID: "controller-key", ControllerPublicKey: publicKey, Clock: clock, MaxStaleness: time.Hour, PolicySink: sink})
	if err != nil {
		t.Fatal(err)
	}
	full := fullFixture(t, privateKey, now, 1, now.Add(30*time.Minute))
	if err := projection.ApplyFull(full); err != nil {
		t.Fatal(err)
	}
	if !projection.Ready() || !projection.OwnsAssignment("daemon-1", 1) {
		t.Fatal("full projection did not become ready")
	}
	if err := projection.ApplyFull(proto.Clone(full).(*cloudpb.FullProjectionSnapshot)); err != nil {
		t.Fatalf("idempotent full = %v", err)
	}
	conflict := proto.Clone(full).(*cloudpb.FullProjectionSnapshot)
	conflict.SnapshotDigest = append([]byte(nil), conflict.SnapshotDigest...)
	conflict.SnapshotDigest[0] ^= 0xff
	if err := projection.ApplyFull(conflict); !errors.Is(err, ErrProjectionConflict) {
		t.Fatalf("same revision conflict = %v", err)
	}
	delta := deltaFixture(t, privateKey, projection, now.Add(time.Second), 2, 2)
	clock.Set(now.Add(time.Second))
	if err := projection.ApplyDelta(delta); err != nil {
		t.Fatal(err)
	}
	if !projection.OwnsAssignment("daemon-1", 2) || projection.Snapshot().Revision != 2 {
		t.Fatalf("delta snapshot = %#v", projection.Snapshot())
	}
	gap := deltaFixture(t, privateKey, projection, now.Add(2*time.Second), 4, 3)
	clock.Set(now.Add(2 * time.Second))
	if err := projection.ApplyDelta(gap); !errors.Is(err, ErrProjectionConflict) {
		t.Fatalf("revision gap = %v", err)
	}
	if projection.Snapshot().Revision != 2 {
		t.Fatal("failed delta mutated current projection")
	}
}

func TestProjectionAssignmentTimerFencesExactEpoch(t *testing.T) {
	now := time.Now().UTC()
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	clock := &projectionClock{now: now}
	fence := projectionFence{called: make(chan uint64, 1)}
	projection, _ := NewProjection(ProjectionConfig{HubID: "hub-1", ControllerKeyID: "controller-key", ControllerPublicKey: publicKey, Clock: clock, MaxStaleness: time.Hour, PolicySink: &projectionSink{}, AssignmentFence: fence})
	full := fullFixture(t, privateKey, now, 1, now.Add(50*time.Millisecond))
	if err := projection.ApplyFull(full); err != nil {
		t.Fatal(err)
	}
	clock.Set(now.Add(time.Second))
	select {
	case epoch := <-fence.called:
		if epoch != 1 {
			t.Fatalf("fenced epoch = %d", epoch)
		}
	case <-time.After(time.Second):
		t.Fatal("assignment expiry timer did not fence")
	}
	if projection.OwnsAssignment("daemon-1", 1) {
		t.Fatal("expired assignment remained active")
	}
}

func fullFixture(t *testing.T, privateKey ed25519.PrivateKey, now time.Time, revision uint64, assignmentExpiry time.Time) *cloudpb.FullProjectionSnapshot {
	t.Helper()
	capability := &cloudpb.PlanCapability{ManagedP2PEnabled: true, ManagedP2PMaxConcurrency: 1, CloudDeviceLimit: 2}
	full := &cloudpb.FullProjectionSnapshot{
		HubId: "hub-1", ProjectionRevision: revision, GeneratedAtUnixMillis: now.UnixMilli(), ExpiresAtUnixMillis: now.Add(time.Hour).UnixMilli(), SigningKeyId: "controller-key",
		Accounts:    []*cloudpb.HubAccountPolicy{{AccountId: "account-1", AuthEpoch: 1, EntitlementStatus: cloudpb.EntitlementStatus_ENTITLEMENT_STATUS_ACTIVE, EntitlementEffectiveUntilUnixMillis: now.Add(time.Hour).UnixMilli(), Capability: capability}},
		Devices:     []*cloudpb.CloudDevicePolicy{{AccountId: "account-1", DeviceId: "daemon-1", DeviceKind: cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_DAEMON, AuthEpoch: 1}},
		Assignments: []*cloudpb.HubAssignment{{DaemonDeviceId: "daemon-1", AccountId: "account-1", HubId: "hub-1", AssignmentEpoch: revision, NotBeforeUnixMillis: now.Add(-time.Minute).UnixMilli(), ExpiresAtUnixMillis: assignmentExpiry.UnixMilli()}},
	}
	candidate, err := unsignedCandidate(full)
	if err != nil {
		t.Fatal(err)
	}
	full.SnapshotDigest, _ = digestCandidate(candidate)
	bytes, _ := fullSigningBytes(full)
	full.Signature = ed25519.Sign(privateKey, bytes)
	return full
}

func deltaFixture(t *testing.T, privateKey ed25519.PrivateKey, projection *Projection, now time.Time, revision, epoch uint64) *cloudpb.PolicyDelta {
	t.Helper()
	delta := &cloudpb.PolicyDelta{HubId: "hub-1", ProjectionRevision: revision, PreviousProjectionRevision: projection.Snapshot().Revision, GeneratedAtUnixMillis: now.UnixMilli(), ExpiresAtUnixMillis: now.Add(30 * time.Minute).UnixMilli(), SigningKeyId: "controller-key", AssignmentOperations: []*cloudpb.HubAssignmentDelta{{Operation: cloudpb.ProjectionDeltaOperation_PROJECTION_DELTA_OPERATION_UPSERT, DaemonDeviceId: "daemon-1", Assignment: &cloudpb.HubAssignment{DaemonDeviceId: "daemon-1", AccountId: "account-1", HubId: "hub-1", AssignmentEpoch: epoch, NotBeforeUnixMillis: now.UnixMilli(), ExpiresAtUnixMillis: now.Add(time.Minute).UnixMilli()}}}}
	projection.mu.Lock()
	candidate := projection.candidateLocked(revision, delta.GeneratedAtUnixMillis, delta.ExpiresAtUnixMillis)
	projection.mu.Unlock()
	if err := applyDeltaOperations(candidate, delta); err != nil {
		t.Fatal(err)
	}
	delta.ResultingDigest, _ = digestCandidate(candidate)
	clone := proto.Clone(delta).(*cloudpb.PolicyDelta)
	clone.Signature = nil
	payload, _ := (proto.MarshalOptions{Deterministic: true}).Marshal(clone)
	delta.Signature = ed25519.Sign(privateKey, append([]byte(projectionSignatureDomain+"delta\x00"), payload...))
	return delta
}

func unsignedCandidate(full *cloudpb.FullProjectionSnapshot) (*projectionCandidate, error) {
	candidate := &projectionCandidate{revision: full.GetProjectionRevision(), generatedAt: time.UnixMilli(full.GetGeneratedAtUnixMillis()).UTC(), expiresAt: time.UnixMilli(full.GetExpiresAtUnixMillis()).UTC(), accounts: map[string]*cloudpb.HubAccountPolicy{}, devices: map[string]*cloudpb.CloudDevicePolicy{}, assignments: map[string]*cloudpb.HubAssignment{}}
	for _, value := range full.GetAccounts() {
		candidate.accounts[value.GetAccountId()] = proto.Clone(value).(*cloudpb.HubAccountPolicy)
	}
	for _, value := range full.GetDevices() {
		candidate.devices[value.GetDeviceId()] = proto.Clone(value).(*cloudpb.CloudDevicePolicy)
	}
	for _, value := range full.GetAssignments() {
		candidate.assignments[value.GetDaemonDeviceId()] = proto.Clone(value).(*cloudpb.HubAssignment)
	}
	return candidate, validateProjectionMaps(candidate, full.GetHubId())
}
