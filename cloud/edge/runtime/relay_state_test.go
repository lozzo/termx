package runtime_test

import (
	"context"
	"testing"
	"time"

	edgeruntime "github.com/muxvia/muxvia/cloud/edge/runtime"
	cloudv1 "github.com/muxvia/muxvia/proto/cloud/v1"
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
