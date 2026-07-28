package runtime_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	edgeruntime "github.com/anytty/anytty/cloud/edge/runtime"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestRelayRuntimeEnforcesConcurrencyAndFreezesUsage(t *testing.T) {
	state, err := edgeruntime.NewState(edgeruntime.StateConfig{MailboxSize: 32, DeltaBuffer: 32})
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	now := time.Now().UTC()
	claims := &cloudv1.RelayLeaseClaims{
		LeaseId: "lease-r6", AccountId: "account-r6", EdgeId: "edge-r6", DaemonId: "daemon-r6", ClientId: "client-r6", SessionId: "session-r6",
		MaxBytes: 4096, MaxRateBytesPerSecond: 1024, MaxConcurrentAllocations: 1, IssuedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(time.Minute)),
	}
	material := &cloudv1.RelayICEConfig{LeaseId: claims.GetLeaseId(), Username: "username-r6", Credential: "credential-r6", ExpiresAt: claims.GetExpiresAt()}
	if err := state.RegisterRelayLease(context.Background(), claims, material); err != nil {
		t.Fatal(err)
	}
	verified, password, ok, err := state.RelayAuth(context.Background(), material.GetUsername(), now)
	if err != nil || !ok || password != material.GetCredential() || verified.GetSessionId() != claims.GetSessionId() {
		t.Fatalf("RelayAuth claims=%v password=%q ok=%v err=%v", verified, password, ok, err)
	}
	admission, err := state.ReserveRelayAllocation(context.Background(), "reservation-a", material.GetUsername(), now)
	if err != nil || admission.MaxBytes != claims.GetMaxBytes() {
		t.Fatalf("admission=%+v err=%v", admission, err)
	}
	if _, err := state.ReserveRelayAllocation(context.Background(), "reservation-b", material.GetUsername(), now); err == nil {
		t.Fatal("second allocation exceeded the lease concurrency limit")
	}
	if err := state.ActivateRelayAllocation(context.Background(), "reservation-a", "allocation-a", cloudv1.RelayTransport_RELAY_TRANSPORT_UDP, now); err != nil {
		t.Fatal(err)
	}
	snapshot, err := state.Snapshot(context.Background())
	if err != nil || len(snapshot.GetAllocations()) != 1 {
		t.Fatalf("snapshot=%v err=%v", snapshot, err)
	}
	event, err := state.CloseRelayAllocation(context.Background(), "allocation-a", 120, 240, now.Add(time.Second))
	if err != nil || event.GetIngressBytes() != 120 || event.GetEgressBytes() != 240 || event.GetEventId() == "" {
		t.Fatalf("usage=%v err=%v", event, err)
	}
	if _, err := state.ReserveRelayAllocation(context.Background(), "reservation-c", material.GetUsername(), now.Add(2*time.Second)); err != nil {
		t.Fatalf("closed allocation did not release concurrency: %v", err)
	}
}

func TestRelayRuntimeRejectsExpiredCredential(t *testing.T) {
	state, _ := edgeruntime.NewState(edgeruntime.StateConfig{MailboxSize: 8, DeltaBuffer: 8})
	defer state.Close()
	now := time.Now().UTC()
	claims := &cloudv1.RelayLeaseClaims{LeaseId: "lease", AccountId: "account", EdgeId: "edge", DaemonId: "daemon", ClientId: "client", SessionId: "session", MaxBytes: 1, MaxRateBytesPerSecond: 1, MaxConcurrentAllocations: 1, IssuedAt: timestamppb.New(now.Add(-2 * time.Minute)), ExpiresAt: timestamppb.New(now.Add(-time.Minute))}
	material := &cloudv1.RelayICEConfig{LeaseId: "lease", Username: "expired", Credential: "secret", ExpiresAt: claims.GetExpiresAt()}
	if err := state.RegisterRelayLease(context.Background(), claims, material); err == nil {
		t.Fatal("expired credential was registered")
	}
}

func TestRelayRuntimeRenewsExistingCredentialAndLimiter(t *testing.T) {
	state, err := edgeruntime.NewState(edgeruntime.StateConfig{MailboxSize: 32, DeltaBuffer: 32})
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	now := time.Now().UTC()
	claims := &cloudv1.RelayLeaseClaims{
		LeaseId: "lease-renew", AccountId: "account-renew", EdgeId: "edge-renew", DaemonId: "daemon-renew", ClientId: "client-renew", SessionId: "session-renew",
		MaxBytes: 100, MaxRateBytesPerSecond: 1000, MaxConcurrentAllocations: 1, IssuedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(time.Second)),
	}
	material := &cloudv1.RelayICEConfig{LeaseId: claims.GetLeaseId(), Username: "credential-renew", Credential: "secret-renew", ExpiresAt: claims.GetExpiresAt()}
	if err := state.RegisterRelayLease(context.Background(), claims, material); err != nil {
		t.Fatal(err)
	}
	admission, err := state.ReserveRelayAllocation(context.Background(), "reservation-renew", material.GetUsername(), now)
	if err != nil {
		t.Fatal(err)
	}
	if !admission.Limiter.Reserve(60, now) {
		t.Fatal("initial Relay usage was rejected")
	}
	if err := state.ActivateRelayAllocation(context.Background(), "reservation-renew", "allocation-renew", cloudv1.RelayTransport_RELAY_TRANSPORT_UDP, now); err != nil {
		t.Fatal(err)
	}
	renewedClaims := proto.Clone(claims).(*cloudv1.RelayLeaseClaims)
	renewedClaims.IssuedAt = timestamppb.New(now.Add(500 * time.Millisecond))
	renewedClaims.ExpiresAt = timestamppb.New(now.Add(time.Minute))
	renewed, err := state.RenewRelayLease(context.Background(), material.GetUsername(), renewedClaims)
	if err != nil {
		t.Fatal(err)
	}
	if renewed.GetUsername() != material.GetUsername() || renewed.GetCredential() != material.GetCredential() || !renewed.GetExpiresAt().AsTime().Equal(renewedClaims.GetExpiresAt().AsTime()) {
		t.Fatalf("renewed material = %+v", renewed)
	}
	afterOldExpiry := now.Add(2 * time.Second)
	verified, password, ok, err := state.RelayAuth(context.Background(), material.GetUsername(), afterOldExpiry)
	if err != nil || !ok || password != material.GetCredential() || !verified.GetExpiresAt().AsTime().Equal(renewedClaims.GetExpiresAt().AsTime()) {
		t.Fatalf("renewed RelayAuth claims=%v password=%q ok=%v err=%v", verified, password, ok, err)
	}
	if admission.Limiter.Reserve(41, afterOldExpiry) || !admission.Limiter.Reserve(40, afterOldExpiry) {
		t.Fatal("renewed active allocation did not preserve its cumulative byte budget")
	}
	mismatched := proto.Clone(renewedClaims).(*cloudv1.RelayLeaseClaims)
	mismatched.ClientId = "another-client"
	mismatched.ExpiresAt = timestamppb.New(now.Add(2 * time.Minute))
	if _, err := state.RenewRelayLease(context.Background(), material.GetUsername(), mismatched); err == nil {
		t.Fatal("RelayLease renewal changed its bound client identity")
	}
}

func TestRelayRuntimeRevokesClosedSessionCredential(t *testing.T) {
	state, err := edgeruntime.NewState(edgeruntime.StateConfig{MailboxSize: 16, DeltaBuffer: 16})
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	now := time.Now().UTC()
	claims := &cloudv1.RelayLeaseClaims{
		LeaseId: "lease-revoke", AccountId: "account-revoke", EdgeId: "edge-revoke", DaemonId: "daemon-revoke", ClientId: "client-revoke", SessionId: "session-revoke",
		MaxBytes: 100, MaxRateBytesPerSecond: 100, MaxConcurrentAllocations: 1, IssuedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(time.Minute)),
	}
	material := &cloudv1.RelayICEConfig{LeaseId: claims.GetLeaseId(), Username: "credential-revoke", Credential: "secret-revoke", ExpiresAt: claims.GetExpiresAt()}
	if err := state.RegisterRelayLease(context.Background(), claims, material); err != nil {
		t.Fatal(err)
	}
	if err := state.RevokeRelaySession(context.Background(), claims.GetSessionId()); err != nil {
		t.Fatal(err)
	}
	if _, _, ok, err := state.RelayAuth(context.Background(), material.GetUsername(), now); err != nil || ok {
		t.Fatalf("revoked Relay credential remained usable: ok=%v err=%v", ok, err)
	}
	if _, err := state.ReserveRelayAllocation(context.Background(), "reservation-revoked", material.GetUsername(), now); err == nil {
		t.Fatal("revoked Relay credential created a new allocation")
	}
}

func TestRelayRuntimeAllowsUDPAndTCPAllocationForBothPeers(t *testing.T) {
	state, err := edgeruntime.NewState(edgeruntime.StateConfig{MailboxSize: 32, DeltaBuffer: 32})
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	now := time.Now().UTC()
	claims := &cloudv1.RelayLeaseClaims{
		LeaseId: "lease-four", AccountId: "account-four", EdgeId: "edge-four", DaemonId: "daemon-four", ClientId: "client-four", SessionId: "session-four",
		MaxBytes: 4096, MaxRateBytesPerSecond: 4096, MaxConcurrentAllocations: 4, IssuedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(time.Minute)),
	}
	material := &cloudv1.RelayICEConfig{LeaseId: claims.GetLeaseId(), Username: "username-four", Credential: "credential-four", ExpiresAt: claims.GetExpiresAt()}
	if err := state.RegisterRelayLease(context.Background(), claims, material); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 4; index++ {
		reservationID := fmt.Sprintf("reservation-%d", index)
		allocationID := fmt.Sprintf("allocation-%d", index)
		if _, err := state.ReserveRelayAllocation(context.Background(), reservationID, material.GetUsername(), now); err != nil {
			t.Fatalf("reserve allocation %d: %v", index, err)
		}
		transport := cloudv1.RelayTransport_RELAY_TRANSPORT_UDP
		if index%2 == 1 {
			transport = cloudv1.RelayTransport_RELAY_TRANSPORT_TCP
		}
		if err := state.ActivateRelayAllocation(context.Background(), reservationID, allocationID, transport, now); err != nil {
			t.Fatalf("activate allocation %d: %v", index, err)
		}
	}
	if _, err := state.ReserveRelayAllocation(context.Background(), "reservation-over-limit", material.GetUsername(), now); err == nil {
		t.Fatal("fifth allocation exceeded the shared RelayLease concurrency limit")
	}
}
