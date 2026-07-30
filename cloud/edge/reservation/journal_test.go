package reservation_test

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/anytty/anytty/cloud/edge/reservation"
	"github.com/anytty/anytty/cloud/relayquota"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestJournalPersistsEveryReservationStageAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "relay.db")
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	request, grant := journalRequestAndGrant(t, now)
	journal := openJournal(t, path, 10)
	if err := journal.CreateRequested(request); err != nil {
		t.Fatal(err)
	}
	journal = reopenAtStage(t, journal, path, request.GetReservationId(), cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_REQUESTED)
	if err := journal.ApplyGrant(grant); err != nil {
		t.Fatal(err)
	}
	journal = reopenAtStage(t, journal, path, request.GetReservationId(), cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_HELD_UNEXPOSED)
	if err := journal.MarkExposed(request.GetReservationId()); err != nil {
		t.Fatal(err)
	}
	journal = reopenAtStage(t, journal, path, request.GetReservationId(), cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_EXPOSED)
	if err := journal.MarkRenewPending(request.GetReservationId(), 1); err != nil {
		t.Fatal(err)
	}
	journal = reopenAtStage(t, journal, path, request.GetReservationId(), cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_RENEW_PENDING)
	renewed := proto.Clone(grant).(*cloudv1.RelayGrant)
	renewed.RenewSequence = 1
	renewed.AuthorizedUntil = timestamppb.New(now.Add(3 * time.Minute))
	if err := journal.ApplyRenewedGrant(renewed); err != nil {
		t.Fatal(err)
	}
	journal = reopenAtStage(t, journal, path, request.GetReservationId(), cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_EXPOSED)
	if err := journal.MarkClosing(request.GetReservationId()); err != nil {
		t.Fatal(err)
	}
	journal = reopenAtStage(t, journal, path, request.GetReservationId(), cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_CLOSING)
	settlement := &cloudv1.RelaySettlement{ReservationId: request.GetReservationId(), Kind: cloudv1.RelaySettlementKind_RELAY_SETTLEMENT_KIND_EXACT, IngressBytes: 10, EgressBytes: 20, PolicyDigest: grant.GetPolicyDigest(), ObservedAt: timestamppb.New(now.Add(time.Minute))}
	if err := journal.PutSettlement(settlement); err != nil {
		t.Fatal(err)
	}
	journal = reopenAtStage(t, journal, path, request.GetReservationId(), cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_SETTLEMENT_DURABLE)
	defer journal.Close()
	ack := &cloudv1.RelaySettlementAck{ReservationId: request.GetReservationId(), Kind: cloudv1.RelaySettlementKind_RELAY_SETTLEMENT_KIND_EXACT, IngressBytes: 10, EgressBytes: 20, PolicyDigest: grant.GetPolicyDigest(), ObservedAt: settlement.GetObservedAt(), SettledAt: timestamppb.New(now.Add(2 * time.Minute)), Code: cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_APPLIED}
	uncommitted := proto.Clone(ack).(*cloudv1.RelaySettlementAck)
	uncommitted.Code = cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_UNSPECIFIED
	if err := journal.Ack(uncommitted); err == nil {
		t.Fatal("uncommitted response code deleted the durable settlement")
	}
	if count, err := journal.Len(); err != nil || count != 1 {
		t.Fatalf("journal count after invalid ACK=%d err=%v", count, err)
	}
	if err := journal.Ack(ack); err != nil {
		t.Fatal(err)
	}
	if count, err := journal.Len(); err != nil || count != 0 {
		t.Fatalf("journal count after ACK=%d err=%v", count, err)
	}
}

func TestJournalRestartFactsMapToZeroOrRecoverySettlement(t *testing.T) {
	now := time.Date(2026, 7, 31, 13, 0, 0, 0, time.UTC)
	for _, exposed := range []bool{false, true} {
		t.Run(map[bool]string{false: "unexposed-zero", true: "exposed-recovery"}[exposed], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "relay.db")
			request, grant := journalRequestAndGrant(t, now)
			journal := openJournal(t, path, 10)
			if err := journal.CreateRequested(request); err != nil {
				t.Fatal(err)
			}
			if err := journal.ApplyGrant(grant); err != nil {
				t.Fatal(err)
			}
			kind := cloudv1.RelaySettlementKind_RELAY_SETTLEMENT_KIND_EXACT
			if exposed {
				if err := journal.MarkExposed(request.GetReservationId()); err != nil {
					t.Fatal(err)
				}
				kind = cloudv1.RelaySettlementKind_RELAY_SETTLEMENT_KIND_RECOVERY_MAX
			}
			if err := journal.Close(); err != nil {
				t.Fatal(err)
			}
			journal = openJournal(t, path, 10)
			defer journal.Close()
			record, found, err := journal.Record(request.GetReservationId())
			if err != nil || !found {
				t.Fatalf("reopened record=%v found=%v err=%v", record, found, err)
			}
			if !exposed {
				if err := journal.MarkClosing(request.GetReservationId()); !errors.Is(err, reservation.ErrStage) {
					t.Fatalf("unexposed record entered closing: %v", err)
				}
			}
			settlement := &cloudv1.RelaySettlement{ReservationId: request.GetReservationId(), Kind: kind, PolicyDigest: grant.GetPolicyDigest(), ObservedAt: timestamppb.New(now.Add(time.Minute))}
			if err := journal.PutSettlement(settlement); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestJournalAcceptsAuthoritativeRecoveryAfterExactRace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "relay.db")
	now := time.Date(2026, 7, 31, 14, 0, 0, 0, time.UTC)
	request, grant := journalRequestAndGrant(t, now)
	journal := openJournal(t, path, 10)
	defer journal.Close()
	for _, operation := range []func() error{
		func() error { return journal.CreateRequested(request) },
		func() error { return journal.ApplyGrant(grant) },
		func() error { return journal.MarkExposed(request.GetReservationId()) },
		func() error { return journal.MarkClosing(request.GetReservationId()) },
	} {
		if err := operation(); err != nil {
			t.Fatal(err)
		}
	}
	exact := &cloudv1.RelaySettlement{ReservationId: request.GetReservationId(), Kind: cloudv1.RelaySettlementKind_RELAY_SETTLEMENT_KIND_EXACT, IngressBytes: 1, EgressBytes: 2, PolicyDigest: grant.GetPolicyDigest(), ObservedAt: timestamppb.New(now)}
	if err := journal.PutSettlement(exact); err != nil {
		t.Fatal(err)
	}
	recovery := &cloudv1.RelaySettlementAck{ReservationId: request.GetReservationId(), Kind: cloudv1.RelaySettlementKind_RELAY_SETTLEMENT_KIND_RECOVERY_MAX, RecoveryBytes: grant.GetReservedBytes(), PolicyDigest: grant.GetPolicyDigest(), ObservedAt: timestamppb.New(now.Add(time.Minute)), SettledAt: timestamppb.New(now.Add(time.Minute)), Code: cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_TERMINAL}
	if err := journal.Ack(recovery); err != nil {
		t.Fatal(err)
	}
	if count, _ := journal.Len(); count != 0 {
		t.Fatalf("authoritative recovery did not remove journal record: %d", count)
	}
}

func TestJournalLimitRejectsNewRelayWithoutEviction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "relay.db")
	journal := openJournal(t, path, 2)
	defer journal.Close()
	now := time.Date(2026, 7, 31, 15, 0, 0, 0, time.UTC)
	for index := 0; index < 2; index++ {
		request, _ := journalRequestAndGrant(t, now.Add(time.Duration(index)*time.Second))
		if err := journal.CreateRequested(request); err != nil {
			t.Fatal(err)
		}
	}
	third, _ := journalRequestAndGrant(t, now.Add(2*time.Second))
	if err := journal.CreateRequested(third); !errors.Is(err, reservation.ErrJournalFull) {
		t.Fatalf("full journal error=%v", err)
	}
	if count, err := journal.Len(); err != nil || count != 2 {
		t.Fatalf("full journal evicted records: count=%d err=%v", count, err)
	}
}

func openJournal(t *testing.T, path string, limit int) *reservation.Journal {
	t.Helper()
	journal, err := reservation.OpenWithLimit(path, time.Second, limit)
	if err != nil {
		t.Fatal(err)
	}
	return journal
}

func reopenAtStage(t *testing.T, journal *reservation.Journal, path, reservationID string, stage cloudv1.RelayJournalStage) *reservation.Journal {
	t.Helper()
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	journal = openJournal(t, path, 10)
	record, found, err := journal.Record(reservationID)
	if err != nil || !found || record.GetStage() != stage {
		t.Fatalf("reopen stage=%v found=%v err=%v want=%v", record.GetStage(), found, err, stage)
	}
	return journal
}

func journalRequestAndGrant(t *testing.T, now time.Time) (*cloudv1.RelayReserveRequest, *cloudv1.RelayGrant) {
	t.Helper()
	request := &cloudv1.RelayReserveRequest{ReservationId: uuid.NewString(), AccountId: uuid.NewString(), DaemonId: uuid.NewString(), ClientId: "client", SessionId: uuid.NewString(), ObservedAt: timestamppb.New(now)}
	requestDigest, err := relayquota.ReserveRequestDigest(request)
	if err != nil {
		t.Fatal(err)
	}
	request.RequestDigest = requestDigest
	policy := &cloudv1.RelayPolicySnapshot{AccountId: request.GetAccountId(), AccountRevision: 1, AccountState: "active", SubscriptionId: uuid.NewString(), SubscriptionRevision: 1, SubscriptionState: "active", PeriodStart: timestamppb.New(now.Add(-time.Hour)), PeriodEnd: timestamppb.New(now.Add(time.Hour)), PlanId: "plan", PlanVersion: 1, PlanRevision: 1, RelayEnabled: true, RelayMaxBytesPerPeriod: 1000, RelayMaxBytesPerSession: 100, RelayMaxRateBytesPerSecond: 1000, RelayMaxConcurrency: 2, AllowedRegions: []string{"test"}, EdgeId: uuid.NewString(), EdgeRevision: 1, EdgeEnabled: true, EdgeRegion: "test", DaemonId: request.GetDaemonId(), DaemonRevision: 1}
	policyDigest, err := relayquota.PolicyDigest(policy)
	if err != nil {
		t.Fatal(err)
	}
	grant := &cloudv1.RelayGrant{ReservationId: request.GetReservationId(), SessionId: request.GetSessionId(), ReservedBytes: 100, MaxRateBytesPerSecond: 1000, AuthorizedUntil: timestamppb.New(now.Add(2 * time.Minute)), PolicyDigest: policyDigest, Policy: policy}
	return request, grant
}
