package postgres

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"math"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/muxvia/muxvia/cloud/controller/account"
	"github.com/muxvia/muxvia/cloud/controller/edgeconfig"
	cloudv1 "github.com/muxvia/muxvia/proto/cloud/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestCommitRelayUsageIsIdempotentInPostgreSQL(t *testing.T) {
	databaseURL := os.Getenv("MUXVIA_CLOUD_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("MUXVIA_CLOUD_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	database, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	edgeID := uuid.NewString()
	if err := database.CreateEdge(ctx, edgeconfig.Edge{
		ID: edgeID, Name: "R6 usage Edge", Region: "test", Capacity: 10, PublicEndpoint: "edge.test:41102", Enabled: true, ConfigVersion: 1, Revision: 1,
		SignedConfig: &cloudv1.SignedEdgeDesiredConfig{KeyId: "test", Payload: []byte("payload"), Signature: []byte("signature")}, CreatedAt: now, UpdatedAt: now,
	}, []byte("usage-edge-claim-"+edgeID), now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	accountID := uuid.NewString()
	if _, err := database.EnsureBootstrapOperator(ctx, account.Record{
		Profile: &cloudv1.AccountProfile{
			AccountId: accountID, Email: "r6-usage-" + accountID + "@example.com", DisplayName: "R6 usage account",
			State: cloudv1.AccountState_ACCOUNT_STATE_ACTIVE, Revision: 1, CreatedAt: timestamppb.New(now), UpdatedAt: timestamppb.New(now),
		},
		PasswordHash: []byte("integration-test-hash"), Roles: []cloudv1.AccountRole{cloudv1.AccountRole_ACCOUNT_ROLE_USER},
	}); err != nil {
		t.Fatal(err)
	}
	enrollmentDigest := sha256.Sum256([]byte(uuid.NewString()))
	if _, err := database.CreateDaemonEnrollment(ctx, accountID, "R6 usage account", "R6 usage daemon", enrollmentDigest[:], now.Add(time.Hour), now); err != nil {
		t.Fatal(err)
	}
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	daemon, err := database.ConsumeDaemonEnrollment(ctx, enrollmentDigest[:], "device-"+uuid.NewString(), "fingerprint-"+uuid.NewString(), publicKey, now)
	if err != nil {
		t.Fatal(err)
	}
	event := &cloudv1.UsageEvent{
		SchemaVersion: 1, EventId: uuid.NewString(), EdgeId: edgeID, LeaseId: uuid.NewString(), AccountId: accountID, DaemonId: daemon.ID,
		ClientId: "client-r6", SessionId: uuid.NewString(), AllocationId: uuid.NewString(), Transport: cloudv1.RelayTransport_RELAY_TRANSPORT_UDP,
		IngressBytes: 120, EgressBytes: 240, StartedAt: timestamppb.New(now), EndedAt: timestamppb.New(now.Add(time.Second)),
	}
	for attempt := 0; attempt < 2; attempt++ {
		ack, err := database.CommitRelayUsage(ctx, edgeID, []*cloudv1.UsageEvent{event})
		if err != nil || len(ack) != 1 || ack[0] != event.GetEventId() {
			t.Fatalf("attempt %d ack=%v err=%v", attempt, ack, err)
		}
	}
	periodStart := monthStart(event.GetEndedAt().AsTime())
	var ingress, egress, eventCount int64
	if err := database.pool.QueryRow(ctx, `SELECT ingress_bytes,egress_bytes,event_count FROM relay_usage_aggregates WHERE account_id=$1 AND edge_id=$2 AND period_start=$3`, accountID, edgeID, periodStart).Scan(&ingress, &egress, &eventCount); err != nil {
		t.Fatal(err)
	}
	if ingress != int64(event.GetIngressBytes()) || egress != int64(event.GetEgressBytes()) || eventCount != 1 {
		t.Fatalf("duplicate usage changed aggregate: ingress=%d egress=%d count=%d", ingress, egress, eventCount)
	}
}

func TestValidateRelayUsageRejectsUnsignedCounterOutsideBigint(t *testing.T) {
	now := time.Now().UTC()
	edgeID := uuid.NewString()
	event := &cloudv1.UsageEvent{
		SchemaVersion: 1, EventId: uuid.NewString(), EdgeId: edgeID, LeaseId: uuid.NewString(), AccountId: uuid.NewString(), DaemonId: uuid.NewString(),
		ClientId: "client-r6", SessionId: uuid.NewString(), AllocationId: uuid.NewString(), Transport: cloudv1.RelayTransport_RELAY_TRANSPORT_UDP,
		IngressBytes: uint64(math.MaxInt64) + 1, StartedAt: timestamppb.New(now), EndedAt: timestamppb.New(now),
	}
	if err := validateRelayUsage(edgeID, event); err == nil {
		t.Fatal("counter outside PostgreSQL bigint was accepted")
	}
}
