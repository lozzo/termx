package policy_test

import (
	"testing"
	"time"

	"github.com/anytty/anytty/cloud/edge/policy"
)

func TestGroupLimiterSharesBudgetAndStopsAtExpiry(t *testing.T) {
	now := time.Date(2026, 7, 31, 1, 2, 3, 0, time.UTC)
	limiter, err := policy.NewGroupLimiter(now.Add(time.Second), 100, 100, now)
	if err != nil {
		t.Fatal(err)
	}
	if !limiter.Reserve(60, now) || limiter.Reserve(41, now) {
		t.Fatal("shared reservation byte budget was not enforced")
	}
	limiter.Refund(10)
	if !limiter.Reserve(40, now.Add(500*time.Millisecond)) {
		t.Fatal("refunded group budget was not reusable")
	}
	if limiter.Reserve(1, now.Add(time.Second)) {
		t.Fatal("expired grant continued forwarding")
	}
}

func TestGroupLimiterRenewalOnlyExtendsExpiry(t *testing.T) {
	now := time.Date(2026, 7, 31, 1, 2, 3, 0, time.UTC)
	oldExpiry := now.Add(time.Second)
	limiter, err := policy.NewGroupLimiter(oldExpiry, 100, 1000, now)
	if err != nil {
		t.Fatal(err)
	}
	if !limiter.Reserve(60, now) {
		t.Fatal("initial group usage was rejected")
	}
	if err := limiter.Renew(now.Add(time.Minute), now.Add(500*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	afterOldExpiry := oldExpiry.Add(time.Millisecond)
	if limiter.Reserve(41, afterOldExpiry) || !limiter.Reserve(40, afterOldExpiry) {
		t.Fatal("renewal changed the held byte budget")
	}
}
