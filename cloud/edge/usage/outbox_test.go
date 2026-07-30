package usage_test

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/anytty/anytty/cloud/edge/usage"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"google.golang.org/protobuf/proto"
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

func TestOutboxPutAcceptsIdenticalReplayAndRejectsConflict(t *testing.T) {
	outbox, err := usage.Open(filepath.Join(t.TempDir(), "usage.db"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer outbox.Close()
	event := usageEvent("event-idempotent", time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC))
	if err := outbox.Put(event); err != nil {
		t.Fatal(err)
	}
	if err := outbox.Put(proto.Clone(event).(*cloudv1.UsageEvent)); err != nil {
		t.Fatalf("identical replay failed: %v", err)
	}
	conflict := proto.Clone(event).(*cloudv1.UsageEvent)
	conflict.EgressBytes++
	if err := outbox.Put(conflict); !errors.Is(err, usage.ErrEventConflict) {
		t.Fatalf("conflicting replay error = %v", err)
	}
	batch, err := outbox.Batch(10)
	if err != nil || len(batch) != 1 || !proto.Equal(batch[0], event) {
		t.Fatalf("durable event=%v err=%v", batch, err)
	}
}

func usageEvent(eventID string, started time.Time) *cloudv1.UsageEvent {
	return &cloudv1.UsageEvent{
		SchemaVersion: 1, EventId: eventID, EdgeId: "edge-r6", LeaseId: "lease-r6", AccountId: "account-r6", DaemonId: "daemon-r6",
		ClientId: "client-r6", SessionId: "session-r6", AllocationId: "allocation-" + eventID, Transport: cloudv1.RelayTransport_RELAY_TRANSPORT_UDP,
		IngressBytes: 10, EgressBytes: 20, StartedAt: timestamppb.New(started), EndedAt: timestamppb.New(started.Add(time.Second)),
	}
}
