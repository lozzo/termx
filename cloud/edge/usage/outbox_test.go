package usage_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/anytty/anytty/cloud/edge/usage"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestOutboxSurvivesRestartAndDeletesOnlyAcknowledgedEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.db")
	now := time.Now().UTC()
	first := usageEvent("event-a", now)
	second := usageEvent("event-b", now.Add(time.Second))
	outbox, err := usage.Open(path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := outbox.Put(second); err != nil {
		t.Fatal(err)
	}
	if err := outbox.Put(first); err != nil {
		t.Fatal(err)
	}
	if err := outbox.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := usage.Open(path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	batch, err := reopened.Batch(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch) != 2 || batch[0].GetEventId() != "event-a" || batch[1].GetEventId() != "event-b" {
		t.Fatalf("unexpected stable batch: %v", batch)
	}
	if err := reopened.Ack([]string{"event-a", "already-acked"}); err != nil {
		t.Fatal(err)
	}
	remaining, err := reopened.Batch(10)
	if err != nil || len(remaining) != 1 || remaining[0].GetEventId() != "event-b" {
		t.Fatalf("remaining=%v err=%v", remaining, err)
	}
}

func TestOutboxRejectsNonVersionedUsage(t *testing.T) {
	outbox, err := usage.Open(filepath.Join(t.TempDir(), "usage.db"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer outbox.Close()
	if err := outbox.Put(&cloudv1.UsageEvent{EventId: "incomplete"}); err == nil {
		t.Fatal("incomplete UsageEvent was persisted")
	}
}

func usageEvent(eventID string, started time.Time) *cloudv1.UsageEvent {
	return &cloudv1.UsageEvent{
		SchemaVersion: 1, EventId: eventID, EdgeId: "edge-r6", LeaseId: "lease-r6", AccountId: "account-r6", DaemonId: "daemon-r6",
		ClientId: "client-r6", SessionId: "session-r6", AllocationId: "allocation-" + eventID, Transport: cloudv1.RelayTransport_RELAY_TRANSPORT_UDP,
		IngressBytes: 10, EgressBytes: 20, StartedAt: timestamppb.New(started), EndedAt: timestamppb.New(started.Add(time.Second)),
	}
}
