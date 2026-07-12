package relay_test

import (
	"path/filepath"
	"testing"

	"github.com/lozzow/termx/private/cloud/control-plane/servicecredential"
	"github.com/lozzow/termx/private/cloud/control-plane/usage"
	"github.com/lozzow/termx/private/cloud/relay"
)

func TestUsageOutboxSurvivesRestartAndAcksIdempotently(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.outbox")
	first, err := relay.NewUsageOutbox(path)
	if err != nil {
		t.Fatal(err)
	}
	event := usage.Event{EventID: "event-1", LeaseID: "lease-1", ManagedSessionID: "session-1", RelayID: "relay-1", PathKind: servicecredential.RelayPathSingle, HopID: "relay-1", Sequence: 1, IntervalStartUnix: 1, IntervalEndUnix: 2, ActiveSeconds: 1, KeyID: "usage-key", Signature: []byte("signed")}
	record := relay.UsageRecord{SignedLease: []byte("signed-lease"), Event: event}
	if err := first.Enqueue(record, record); err != nil {
		t.Fatal(err)
	}
	restarted, err := relay.NewUsageOutbox(path)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := restarted.Pending()
	if err != nil || len(pending) != 1 || pending[0].Event.EventID != event.EventID {
		t.Fatalf("restarted pending = (%#v, %v)", pending, err)
	}
	if err := restarted.Ack(event.EventID, event.Sequence); err != nil {
		t.Fatal(err)
	}
	pending, err = restarted.Pending()
	if err != nil || len(pending) != 0 {
		t.Fatalf("acked pending = (%#v, %v)", pending, err)
	}
}
