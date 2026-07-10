package usage_test

import (
	"crypto/ed25519"
	"errors"
	"testing"
	"time"

	"github.com/lozzow/termx/private/termx-cloud/control-plane/servicecredential"
	"github.com/lozzow/termx/private/termx-cloud/control-plane/usage"
)

func TestLedgerIsIdempotentAndDoesNotDoubleBillMeshHops(t *testing.T) {
	now := time.Date(2026, 7, 11, 8, 0, 0, 0, time.UTC)
	clientSigner := usageSigner(t, "relay-client-key", 0, now)
	daemonSigner := usageSigner(t, "relay-daemon-key", 32, now)
	ring, err := servicecredential.NewKeyRing(clientSigner.PublicKey(), daemonSigner.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := usage.NewLedger(ring, map[string]string{
		"relay-client": "relay-client-key",
		"relay-daemon": "relay-daemon-key",
	}, 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	lease := meshLease(now)
	first := usage.Event{
		EventID: "event-1", LeaseID: lease.LeaseID, ManagedSessionID: lease.ManagedSessionID,
		RelayID: "relay-client", RouteID: lease.RouteID, PathKind: lease.PathKind, HopID: "client-edge",
		Sequence: 1, IntervalStartUnix: now.Unix(), IntervalEndUnix: now.Add(10 * time.Second).Unix(),
		BytesUp: 100, BytesDown: 200, ActiveSeconds: 10,
	}
	first, err = usage.SignEvent(first, clientSigner, now)
	if err != nil {
		t.Fatal(err)
	}
	result, err := ledger.Apply(lease, first, now.Add(20*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if result.Aggregate.BytesUp != 100 || result.Aggregate.BytesDown != 200 {
		t.Fatalf("first aggregate = %#v", result.Aggregate)
	}
	duplicate, err := ledger.Apply(lease, first, now.Add(30*time.Second))
	if err != nil || !duplicate.Duplicate || duplicate.Aggregate != result.Aggregate {
		t.Fatalf("duplicate result = %#v, err = %v", duplicate, err)
	}

	second := usage.Event{
		EventID: "event-2", LeaseID: lease.LeaseID, ManagedSessionID: lease.ManagedSessionID,
		RelayID: "relay-daemon", RouteID: lease.RouteID, PathKind: lease.PathKind, HopID: "daemon-edge",
		Sequence: 1, IntervalStartUnix: now.Unix(), IntervalEndUnix: now.Add(10 * time.Second).Unix(),
		BytesUp: 110, BytesDown: 190, ActiveSeconds: 10,
	}
	second, err = usage.SignEvent(second, daemonSigner, now)
	if err != nil {
		t.Fatal(err)
	}
	result, err = ledger.Apply(lease, second, now.Add(40*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if result.Aggregate.BytesUp != 110 || result.Aggregate.BytesDown != 200 || result.Aggregate.ActiveSeconds != 10 {
		t.Fatalf("mesh aggregate = %#v, want per-window maxima", result.Aggregate)
	}
}

func TestLedgerRejectsConflictRollbackAndLateOutOfRangeReport(t *testing.T) {
	now := time.Date(2026, 7, 11, 8, 0, 0, 0, time.UTC)
	signer := usageSigner(t, "relay-key", 0, now)
	ring, _ := servicecredential.NewKeyRing(signer.PublicKey())
	ledger, _ := usage.NewLedger(ring, map[string]string{"relay": "relay-key"}, time.Minute)
	lease := meshLease(now)

	event := usage.Event{EventID: "event-2", LeaseID: lease.LeaseID, ManagedSessionID: lease.ManagedSessionID, RelayID: "relay", RouteID: lease.RouteID, PathKind: lease.PathKind, HopID: "hop", Sequence: 2, IntervalStartUnix: now.Unix(), IntervalEndUnix: now.Add(10 * time.Second).Unix(), BytesUp: 10, BytesDown: 20, ActiveSeconds: 10}
	event, _ = usage.SignEvent(event, signer, now)
	if _, err := ledger.Apply(lease, event, now.Add(20*time.Second)); err != nil {
		t.Fatal(err)
	}
	changed := event
	changed.EventID = "changed"
	changed.BytesUp = 11
	changed, _ = usage.SignEvent(changed, signer, now)
	if _, err := ledger.Apply(lease, changed, now.Add(30*time.Second)); !errors.Is(err, usage.ErrDuplicateConflict) {
		t.Fatalf("idempotency conflict error = %v", err)
	}
	rollback := event
	rollback.EventID = "event-1"
	rollback.Sequence = 1
	rollback.IntervalStartUnix = now.Add(10 * time.Second).Unix()
	rollback.IntervalEndUnix = now.Add(20 * time.Second).Unix()
	rollback, _ = usage.SignEvent(rollback, signer, now)
	if _, err := ledger.Apply(lease, rollback, now.Add(40*time.Second)); !errors.Is(err, usage.ErrSequenceRollback) {
		t.Fatalf("rollback error = %v", err)
	}
	delayed := event
	delayed.EventID = "event-3"
	delayed.Sequence = 3
	delayed.IntervalStartUnix = now.Add(20 * time.Second).Unix()
	delayed.IntervalEndUnix = now.Add(30 * time.Second).Unix()
	delayed, _ = usage.SignEvent(delayed, signer, now)
	if _, err := ledger.Apply(lease, delayed, time.Unix(lease.ExpiresAtUnix, 0).Add(30*time.Second)); err != nil {
		t.Fatalf("delayed report was rejected: %v", err)
	}
	late := delayed
	late.EventID = "event-4"
	late.Sequence = 4
	late.IntervalStartUnix = now.Add(30 * time.Second).Unix()
	late.IntervalEndUnix = now.Add(40 * time.Second).Unix()
	late, _ = usage.SignEvent(late, signer, now)
	if _, err := ledger.Apply(lease, late, time.Unix(lease.ExpiresAtUnix, 0).Add(2*time.Minute)); !errors.Is(err, usage.ErrUsageOutOfRange) {
		t.Fatalf("late report error = %v", err)
	}
}

func usageSigner(t *testing.T, keyID string, offset byte, now time.Time) servicecredential.Signer {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	for index := range seed {
		seed[index] = byte(index) + offset
	}
	signer, err := servicecredential.NewSigner(keyID, ed25519.NewKeyFromSeed(seed), now.Add(-time.Hour), now.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

func meshLease(now time.Time) servicecredential.RelayLeaseClaims {
	return servicecredential.RelayLeaseClaims{
		Version: 1, KeyID: "cp", LeaseID: "lease-1", Issuer: "cp", AudienceRelayPool: "pool",
		AccountID: "account", ManagedSessionID: "managed", ClientDeviceID: "client", TargetDeviceID: "daemon",
		Region: "eu-west", PathKind: servicecredential.RelayPathMesh, RouteID: "route-1", RouteVersion: 1,
		ClientEdgeRelayID: "relay-client", DaemonEdgeRelayID: "relay-daemon", MaxInternalTransit: 0,
		NotBeforeUnix: now.Unix(), ExpiresAtUnix: now.Add(5 * time.Minute).Unix(), MaxBytes: 1_000_000,
		MaxBitrateKbps: 100_000, MaxConcurrency: 1, CredentialBindingID: "binding",
	}
}
