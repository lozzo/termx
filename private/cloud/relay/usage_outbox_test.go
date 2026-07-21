package relay_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/muxvia/muxvia/private/cloud/control-plane/servicecredential"
	"github.com/muxvia/muxvia/private/cloud/control-plane/usage"
	"github.com/muxvia/muxvia/private/cloud/relay"
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

func TestFlushUsageOutboxPersistsSameSecondTrafficBeforeClearingCounters(t *testing.T) {
	fixture := newRelayFixture(t, 2, 10_000, 1_000)
	activation, err := fixture.authority.ActivateLease(fixture.activationRequest)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := fixture.authority.AuthenticateTURN(activation.ClientCredential.Username, "muxvia-relay", "source"); !ok {
		t.Fatal("TURN auth failed")
	}
	if err := fixture.authority.ConfirmAllocation("source", "allocation", activation.ClientCredential.Username); err != nil {
		t.Fatal(err)
	}
	if err := fixture.authority.RecordTraffic("allocation", 100, 200); err != nil {
		t.Fatal(err)
	}
	outbox, _ := relay.NewUsageOutbox(filepath.Join(t.TempDir(), "same-second.outbox"))
	if err := fixture.authority.FlushUsageOutbox(outbox, "edge_shutdown"); err != nil {
		t.Fatal(err)
	}
	pending, err := outbox.Pending()
	if err != nil || len(pending) != 1 || pending[0].Event.BytesUp != 100 || pending[0].Event.BytesDown != 200 || pending[0].Event.IntervalEndUnix != pending[0].Event.IntervalStartUnix+1 {
		t.Fatalf("same-second durable usage = (%#v, %v)", pending, err)
	}
	fixture.clock.Advance(time.Second)
	if events, err := fixture.authority.DrainUsage(""); err != nil || len(events) != 0 {
		t.Fatalf("flushed counters were emitted twice = (%#v, %v)", events, err)
	}
}

func TestTargetedFinalUsageReleasesZeroByteLeaseExactlyOnce(t *testing.T) {
	fixture := newRelayFixture(t, 2, 10_000, 1_000)
	if _, err := fixture.authority.ActivateLease(fixture.activationRequest); err != nil {
		t.Fatal(err)
	}
	outbox, _ := relay.NewUsageOutbox(filepath.Join(t.TempDir(), "targeted-final.outbox"))
	if err := fixture.authority.FlushUsageOutboxFor(outbox, "remote_revoke", "lease-1", ""); err != nil {
		t.Fatal(err)
	}
	pending, err := outbox.Pending()
	if err != nil || len(pending) != 1 || pending[0].Event.LeaseID != "lease-1" || pending[0].Event.TerminationReason != "remote_revoke" || pending[0].Event.BytesUp != 0 || pending[0].Event.BytesDown != 0 || pending[0].Event.Sequence != 1 {
		t.Fatalf("targeted final usage = (%#v, %v)", pending, err)
	}
	if err := fixture.authority.FlushUsageOutboxFor(outbox, "remote_revoke", "lease-1", ""); err != nil {
		t.Fatal(err)
	}
	pending, err = outbox.Pending()
	if err != nil || len(pending) != 1 {
		t.Fatalf("duplicate final usage = (%#v, %v)", pending, err)
	}
}
