package relay

import (
	"net"
	"testing"
	"time"

	"github.com/muxvia/muxvia/cloud/edge/policy"
)

func TestTrackedPacketConnEnforcesCombinedByteAndRateLimits(t *testing.T) {
	now := time.Now()
	limiter, err := testAdmissionLimiter(now, 100, 100)
	if err != nil {
		t.Fatal(err)
	}
	connection := &trackedPacketConn{limiter: limiter}
	if !connection.allow(60, true, now) {
		t.Fatal("first ingress within lease was rejected")
	}
	if connection.allow(41, false, now) {
		t.Fatal("combined ingress and egress exceeded max_bytes")
	}
	if !connection.allow(40, false, now.Add(time.Second)) {
		t.Fatal("remaining bytes within lease were rejected after rate refill")
	}
	ingress, egress := connection.counts()
	if ingress != 60 || egress != 40 {
		t.Fatalf("counters ingress=%d egress=%d", ingress, egress)
	}
}

func TestTrackedGeneratorDropsClosedUnclaimedSocket(t *testing.T) {
	generator := newTrackedGenerator(net.ParseIP("127.0.0.1"), "127.0.0.1")
	if err := generator.Validate(); err != nil {
		t.Fatal(err)
	}
	connection, _, err := generator.AllocatePacketConn("udp4", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(generator.conns) != 1 {
		t.Fatalf("tracked sockets=%d", len(generator.conns))
	}
	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}
	if len(generator.conns) != 0 {
		t.Fatalf("closed unclaimed socket remained tracked: %d", len(generator.conns))
	}
}

func TestTrackedPacketConnRejectsBurstAboveRate(t *testing.T) {
	now := time.Now()
	limiter, err := testAdmissionLimiter(now, 1000, 100)
	if err != nil {
		t.Fatal(err)
	}
	connection := &trackedPacketConn{limiter: limiter}
	if !connection.allow(80, true, now) {
		t.Fatal("initial rate budget was rejected")
	}
	if connection.allow(30, true, now) {
		t.Fatal("burst above rate budget was accepted")
	}
	if !connection.allow(30, true, now.Add(time.Second)) {
		t.Fatal("rate budget did not refill")
	}
}

func TestTrackedPacketConnsShareLeaseBudget(t *testing.T) {
	now := time.Now()
	limiter, err := testAdmissionLimiter(now, 100, 100)
	if err != nil {
		t.Fatal(err)
	}
	first, second := &trackedPacketConn{limiter: limiter}, &trackedPacketConn{limiter: limiter}
	if !first.allow(60, true, now) || second.allow(41, true, now) {
		t.Fatal("multiple allocations did not share one RelayLease budget")
	}
}

func testAdmissionLimiter(now time.Time, maxBytes, rate uint64) (*policy.AdmissionLimiter, error) {
	account, err := policy.NewRateLimiter(now.Add(time.Minute), rate, now)
	if err != nil {
		return nil, err
	}
	session, err := policy.NewRateLimiter(now.Add(time.Minute), rate, now)
	if err != nil {
		return nil, err
	}
	lease, err := policy.NewLeaseLimiter(now.Add(time.Minute), maxBytes, rate, now)
	if err != nil {
		return nil, err
	}
	return policy.NewAdmissionLimiter(account, session, lease)
}
