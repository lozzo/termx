package sqlite_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/muxvia/muxvia/private/cloud/control-plane/relayquota"
	"github.com/muxvia/muxvia/private/cloud/control-plane/servicecredential"
	cloudsqlite "github.com/muxvia/muxvia/private/cloud/control-plane/sqlite"
	"github.com/muxvia/muxvia/private/cloud/control-plane/usage"
	"github.com/muxvia/muxvia/proto/cloudpb"
)

func TestRelayUsageJournalSettlesReservationIdempotentlyAcrossRestart(t *testing.T) {
	now := time.Date(2026, 7, 21, 3, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "usage.db")
	store, err := cloudsqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	request := relayquota.ReserveRequest{LeaseID: "lease-1", AccountID: "account-1", ManagedSessionID: "managed-1", ClientDeviceID: "client-1", TargetDeviceID: "daemon-1", Region: "local-1", HubID: "hub-1", RelayID: "relay-1", PeriodStart: now.Add(-time.Hour), PeriodEnd: now.Add(24 * time.Hour), PeriodLimitBytes: 1_000, MaxBytesPerLease: 600, MaxConcurrency: 2, ExpiresAt: now.Add(5 * time.Minute), ReleaseAfter: now.Add(7 * time.Minute)}
	if _, _, _, err := store.Reserve(context.Background(), request, now); err != nil {
		t.Fatal(err)
	}
	leaseSigner, _ := servicecredential.NewSigner("lease-key", ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x11}, ed25519.SeedSize)), now.Add(-time.Hour), now.Add(time.Hour))
	leaseIssuer, _ := servicecredential.NewRelayLeaseIssuer("termx-cloud-controller-relay", leaseSigner)
	lease, claims, err := leaseIssuer.Issue(servicecredential.RelayLeaseRequest{LeaseID: "lease-1", AudienceRelayPool: "pool-local-1", AccountID: "account-1", ManagedSessionID: "managed-1", ClientDeviceID: "client-1", TargetDeviceID: "daemon-1", Region: "local-1", PathKind: servicecredential.RelayPathSingle, TTL: 5 * time.Minute, MaxBytes: 600, MaxBitrateKbps: 1_000, MaxConcurrency: 2, CredentialBindingID: "binding-relay-1"}, now)
	if err != nil {
		t.Fatal(err)
	}
	usageSigner, _ := servicecredential.NewSigner("usage-key", ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x22}, ed25519.SeedSize)), now.Add(-time.Hour), now.Add(time.Hour))
	ring, _ := servicecredential.NewKeyRing(usageSigner.PublicKey())
	apply := func(event usage.Event, at time.Time) (*cloudpb.RelayUsageAck, *cloudpb.RelayQuotaPeriod, *cloudpb.RelayUsageAggregate, error) {
		signed, signErr := usage.SignEvent(event, usageSigner, at)
		if signErr != nil {
			return nil, nil, nil, signErr
		}
		digest, verifyErr := usage.VerifyEvent(ring, "usage-key", claims, signed, at.Add(time.Second), 2*time.Minute)
		if verifyErr != nil {
			return nil, nil, nil, verifyErr
		}
		wire, _ := usage.ToProto(signed)
		return store.ApplyRelayUsage(context.Background(), &cloudpb.RelayUsageRecord{SignedLease: lease.Bytes(), Event: wire}, claims, signed, digest, at.Add(time.Second))
	}
	firstEvent := usage.Event{EventID: "event-1", LeaseID: "lease-1", ManagedSessionID: "managed-1", RelayID: "relay-1", PathKind: servicecredential.RelayPathSingle, HopID: "relay-1", Sequence: 1, IntervalStartUnix: now.Unix(), IntervalEndUnix: now.Add(time.Second).Unix(), BytesUp: 100, BytesDown: 200, ActiveSeconds: 1}
	ack, period, aggregate, err := apply(firstEvent, now)
	if err != nil || ack.GetDuplicate() || period.GetUsedBytes() != 300 || period.GetReservedBytes() != 300 || period.GetRemainingBytes() != 400 || aggregate.GetBytesUp() != 100 || aggregate.GetBytesDown() != 200 {
		t.Fatalf("first settlement = (%v, %v, %v, %v)", ack, period, aggregate, err)
	}
	duplicate, _, _, err := apply(firstEvent, now)
	if err != nil || !duplicate.GetDuplicate() {
		t.Fatalf("duplicate settlement = (%v, %v)", duplicate, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = cloudsqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	secondEvent := usage.Event{EventID: "event-2", LeaseID: "lease-1", ManagedSessionID: "managed-1", RelayID: "relay-1", PathKind: servicecredential.RelayPathSingle, HopID: "relay-1", Sequence: 2, IntervalStartUnix: now.Add(time.Second).Unix(), IntervalEndUnix: now.Add(2 * time.Second).Unix(), BytesUp: 40, BytesDown: 60, ActiveSeconds: 1, TerminationReason: "allocation_closed"}
	ack, period, aggregate, err = apply(secondEvent, now.Add(time.Second))
	if err != nil || ack.GetDuplicate() || period.GetUsedBytes() != 400 || period.GetReservedBytes() != 0 || period.GetRemainingBytes() != 600 || period.GetActiveLeaseCount() != 0 || aggregate.GetBytesUp() != 140 || aggregate.GetBytesDown() != 260 {
		t.Fatalf("final settlement after restart = (%v, %v, %v, %v)", ack, period, aggregate, err)
	}
	rollback := secondEvent
	rollback.EventID = "event-rollback"
	rollback.Sequence = 1
	if _, _, _, err := apply(rollback, now.Add(2*time.Second)); !errors.Is(err, usage.ErrSequenceRollback) && !errors.Is(err, usage.ErrDuplicateConflict) {
		t.Fatalf("sequence rollback error = %v", err)
	}
}
