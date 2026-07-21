package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	cloudpostgres "github.com/muxvia/muxvia/private/cloud/control-plane/postgres"
	"github.com/muxvia/muxvia/private/cloud/control-plane/relayquota"
	"github.com/muxvia/muxvia/proto/cloudpb"
)

func TestRelayQuotaReserveReplayReleaseAndDelayedExpiry(t *testing.T) {
	now := time.Date(2026, 7, 21, 2, 0, 0, 0, time.UTC)
	store, err := cloudpostgres.Open(context.Background(), testPostgresDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	request := relayquota.ReserveRequest{
		LeaseID: "lease-1", AccountID: "account-1", ManagedSessionID: "managed-1",
		ClientDeviceID: "client-1", TargetDeviceID: "daemon-1", Region: "local-1", HubID: "hub-1", RelayID: "relay-1",
		PeriodStart: now.Add(-time.Hour), PeriodEnd: now.Add(24 * time.Hour), PeriodLimitBytes: 1_000,
		MaxBytesPerLease: 600, MaxConcurrency: 2, ExpiresAt: now.Add(time.Minute), ReleaseAfter: now.Add(3 * time.Minute),
	}
	reservation, period, created, err := store.Reserve(context.Background(), request, now)
	if err != nil {
		t.Fatal(err)
	}
	if !created || reservation.GetReservedBytes() != 600 || period.GetReservedBytes() != 600 || period.GetRemainingBytes() != 400 || period.GetActiveLeaseCount() != 1 {
		t.Fatalf("first reservation = (%v, %v)", reservation, period)
	}
	replay, replayPeriod, replayCreated, err := store.Reserve(context.Background(), request, now.Add(time.Second))
	if err != nil || replayCreated || replay.GetLeaseId() != "lease-1" || replayPeriod.GetRevision() != period.GetRevision() {
		t.Fatalf("exact replay = (%v, %v, %v)", replay, replayPeriod, err)
	}
	conflict := request
	conflict.Region = "other"
	if _, _, _, err := store.Reserve(context.Background(), conflict, now); !errors.Is(err, relayquota.ErrReservationConflict) {
		t.Fatalf("conflicting replay error = %v", err)
	}
	second := request
	second.LeaseID, second.ManagedSessionID, second.ClientDeviceID = "lease-2", "managed-2", "client-2"
	second.ExpiresAt, second.ReleaseAfter = now.Add(2*time.Minute), now.Add(4*time.Minute)
	secondReservation, secondPeriod, _, err := store.Reserve(context.Background(), second, now)
	if err != nil {
		t.Fatal(err)
	}
	if secondReservation.GetReservedBytes() != 400 || secondPeriod.GetRemainingBytes() != 0 || secondPeriod.GetActiveLeaseCount() != 2 {
		t.Fatalf("clamped reservation = (%v, %v)", secondReservation, secondPeriod)
	}
	third := request
	third.LeaseID, third.ManagedSessionID = "lease-3", "managed-3"
	if _, _, _, err := store.Reserve(context.Background(), third, now); !errors.Is(err, relayquota.ErrQuotaExhausted) {
		t.Fatalf("period/concurrency exhaustion = %v", err)
	}
	beforeGrace, err := store.Snapshot(context.Background(), "account-1", request.PeriodStart, request.PeriodEnd, now.Add(2*time.Minute))
	if err != nil || beforeGrace.GetPeriod().GetActiveLeaseCount() != 2 {
		t.Fatalf("reservation released before report grace = (%v, %v)", beforeGrace, err)
	}
	afterGrace, err := store.Snapshot(context.Background(), "account-1", request.PeriodStart, request.PeriodEnd, now.Add(3*time.Minute))
	if err != nil || afterGrace.GetPeriod().GetActiveLeaseCount() != 1 || afterGrace.GetPeriod().GetReservedBytes() != 400 {
		t.Fatalf("expired reservation not released = (%v, %v)", afterGrace, err)
	}
	released, releasedPeriod, err := store.Release(context.Background(), "account-1", "lease-2", now.Add(3*time.Minute))
	if err != nil || released.GetState() != cloudpb.RelayReservationState_RELAY_RESERVATION_STATE_RELEASED || releasedPeriod.GetReservedBytes() != 0 || releasedPeriod.GetRemainingBytes() != 1_000 {
		t.Fatalf("release = (%v, %v, %v)", released, releasedPeriod, err)
	}
}

func TestRelayQuotaSerializesConcurrentReservations(t *testing.T) {
	now := time.Date(2026, 7, 21, 2, 30, 0, 0, time.UTC)
	store, err := cloudpostgres.Open(context.Background(), testPostgresDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	const attempts = 8
	start := make(chan struct{})
	results := make(chan error, attempts)
	var group sync.WaitGroup
	for index := 0; index < attempts; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			<-start
			_, _, _, reserveErr := store.Reserve(context.Background(), relayquota.ReserveRequest{
				LeaseID: fmt.Sprintf("lease-%d", index), AccountID: "account-concurrent", ManagedSessionID: fmt.Sprintf("managed-%d", index),
				ClientDeviceID: fmt.Sprintf("client-%d", index), TargetDeviceID: "daemon-1", Region: "local-1", HubID: "hub-1", RelayID: "relay-1",
				PeriodStart: now.Add(-time.Hour), PeriodEnd: now.Add(24 * time.Hour), PeriodLimitBytes: 1_000,
				MaxBytesPerLease: 1_000, MaxConcurrency: 1, ExpiresAt: now.Add(time.Minute), ReleaseAfter: now.Add(3 * time.Minute),
			}, now)
			results <- reserveErr
		}(index)
	}
	close(start)
	group.Wait()
	close(results)
	succeeded := 0
	for reserveErr := range results {
		if reserveErr == nil {
			succeeded++
			continue
		}
		if !errors.Is(reserveErr, relayquota.ErrQuotaExhausted) {
			t.Fatalf("concurrent reservation error = %v", reserveErr)
		}
	}
	if succeeded != 1 {
		t.Fatalf("concurrent reservations succeeded = %d, want 1", succeeded)
	}
}
