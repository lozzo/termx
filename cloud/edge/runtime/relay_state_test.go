package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/anytty/anytty/cloud/edge/policy"
	"github.com/anytty/anytty/cloud/relayquota"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestRelayReservationGroupCapsPhysicalAllocationsAndSettlesOnce(t *testing.T) {
	now := time.Date(2026, 7, 31, 1, 2, 3, 0, time.UTC)
	state := newRelayTestState(t, now)
	defer state.Close()
	grant, material := relayTestGrant(t, now, 1000)
	if err := state.RegisterRelayGrant(context.Background(), grant, material); err != nil {
		t.Fatal(err)
	}

	for index := 0; index < MaxPhysicalAllocationsPerReservation; index++ {
		physicalID, allocationID := "physical-"+string(rune('a'+index)), "allocation-"+string(rune('a'+index))
		admission, err := state.ReserveRelayAllocation(context.Background(), physicalID, material.GetUsername(), now)
		if err != nil {
			t.Fatalf("reserve %d: %v", index, err)
		}
		if admission.ReservationID != grant.GetReservationId() || admission.Limiter == nil {
			t.Fatalf("admission %d = %+v", index, admission)
		}
		if err := state.ActivateRelayAllocation(context.Background(), physicalID, allocationID, cloudv1.RelayTransport_RELAY_TRANSPORT_UDP, now); err != nil {
			t.Fatalf("activate %d: %v", index, err)
		}
	}
	if _, err := state.ReserveRelayAllocation(context.Background(), "physical-fifth", material.GetUsername(), now); err == nil {
		t.Fatal("fifth physical allocation was admitted")
	}
	if err := state.BeginRelaySessionClose(context.Background(), grant.GetSessionId()); err != nil {
		t.Fatal(err)
	}
	if _, err := state.ReserveRelayAllocation(context.Background(), "physical-after-close", material.GetUsername(), now); err == nil {
		t.Fatal("closing group admitted a new allocation")
	}
	for index := 0; index < MaxPhysicalAllocationsPerReservation; index++ {
		allocationID := "allocation-" + string(rune('a'+index))
		if err := state.BeginRelayAllocationClose(context.Background(), allocationID); err != nil {
			t.Fatal(err)
		}
		assertRelayCounts(t, state, 0, uint32(MaxPhysicalAllocationsPerReservation-index-1), uint32(index+1))
	}
	for index := 0; index < MaxPhysicalAllocationsPerReservation; index++ {
		allocationID := "allocation-" + string(rune('a'+index))
		if err := state.CloseRelayAllocation(context.Background(), allocationID, uint64(index+1), uint64(index+2)); err != nil {
			t.Fatal(err)
		}
	}
	settlement, err := state.RelaySessionSettlement(context.Background(), grant.GetSessionId(), now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if settlement.GetReservationId() != grant.GetReservationId() || settlement.GetIngressBytes() != 10 || settlement.GetEgressBytes() != 14 || settlement.GetKind() != cloudv1.RelaySettlementKind_RELAY_SETTLEMENT_KIND_EXACT {
		t.Fatalf("aggregate settlement = %v", settlement)
	}
	replayed, err := state.RelaySessionSettlement(context.Background(), grant.GetSessionId(), now.Add(time.Second))
	if err != nil || !proto.Equal(replayed, settlement) {
		t.Fatalf("settlement replay = %v, %v", replayed, err)
	}
}

func TestRelayRenewOnlyAdvancesExpiryAndSequence(t *testing.T) {
	now := time.Date(2026, 7, 31, 1, 2, 3, 0, time.UTC)
	state := newRelayTestState(t, now)
	defer state.Close()
	grant, material := relayTestGrant(t, now, 100)
	if err := state.RegisterRelayGrant(context.Background(), grant, material); err != nil {
		t.Fatal(err)
	}
	admission := mustAdmission(t, state, material.GetUsername(), now)
	if !admission.Limiter.Reserve(60, now) {
		t.Fatal("initial byte use failed")
	}
	renewedGrant := proto.Clone(grant).(*cloudv1.RelayGrant)
	renewedGrant.RenewSequence = 1
	renewedGrant.AuthorizedUntil = timestamppb.New(now.Add(2 * time.Minute))
	renewed, err := state.RenewRelayGrant(context.Background(), material.GetUsername(), renewedGrant)
	if err != nil {
		t.Fatal(err)
	}
	if renewed.GetReservationId() != material.GetReservationId() || !renewed.GetExpiresAt().AsTime().After(material.GetExpiresAt().AsTime()) {
		t.Fatalf("renewed material = %v", renewed)
	}
	_, _, _, _ = state.RelayAuth(context.Background(), material.GetUsername(), now)
	if admission.Limiter.Reserve(41, now) {
		t.Fatal("renewal reset the shared held byte budget")
	}
}

func assertRelayCounts(t *testing.T, state *State, pending, active, closing uint32) {
	t.Helper()
	if err := state.call(context.Background(), func(data *stateData) error {
		group := data.relayGroups["00000000-0000-4000-8000-000000000001"]
		if group == nil || group.pending != pending || group.active != active || group.closeActive != closing {
			t.Fatalf("group counts = %+v, want pending=%d active=%d closing=%d", group, pending, active, closing)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func mustAdmission(t *testing.T, state *State, username string, now time.Time) policy.RelayAdmission {
	t.Helper()
	admission, err := state.ReserveRelayAllocation(context.Background(), "physical-budget", username, now)
	if err != nil {
		t.Fatal(err)
	}
	return admission
}

func newRelayTestState(t *testing.T, now time.Time) *State {
	t.Helper()
	state, err := NewState(StateConfig{MailboxSize: 32, DeltaBuffer: 32, MaxSessions: 32, MaxPendingSignals: 32, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if err := state.ApplyDaemonStateSnapshot(context.Background(), &cloudv1.DaemonStateSnapshot{Daemons: []*cloudv1.DaemonStateRecord{{DaemonId: "daemon", State: cloudv1.DaemonState_DAEMON_STATE_ACTIVE, StateRevision: 1}}}); err != nil {
		t.Fatal(err)
	}
	return state
}

func relayTestGrant(t *testing.T, now time.Time, reserved uint64) (*cloudv1.RelayGrant, *cloudv1.RelayICEConfig) {
	t.Helper()
	policySnapshot := &cloudv1.RelayPolicySnapshot{AccountId: "account", SubscriptionId: "subscription", PlanId: "plan", RelayEnabled: true, EdgeId: "edge", DaemonId: "daemon"}
	digest, err := relayquota.PolicyDigest(policySnapshot)
	if err != nil {
		t.Fatal(err)
	}
	grant := &cloudv1.RelayGrant{ReservationId: "00000000-0000-4000-8000-000000000001", SessionId: "session", ReservedBytes: reserved, MaxRateBytesPerSecond: 1000, AuthorizedUntil: timestamppb.New(now.Add(time.Minute)), PolicyDigest: digest, Policy: policySnapshot}
	material := &cloudv1.RelayICEConfig{ReservationId: grant.GetReservationId(), Username: "v2:" + grant.GetReservationId() + ":session", Credential: "secret", ExpiresAt: grant.GetAuthorizedUntil()}
	return grant, material
}
