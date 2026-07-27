package policy_test

import (
	"testing"
	"time"

	"github.com/anytty/anytty/cloud/edge/policy"
)

func TestLeaseLimiterSharesBudgetAndStopsAtExpiry(t *testing.T) {
	now := time.Now().UTC()
	limiter, err := policy.NewLeaseLimiter(now.Add(time.Second), 100, 100, now)
	if err != nil {
		t.Fatal(err)
	}
	if !limiter.Reserve(60, now) || limiter.Reserve(41, now) {
		t.Fatal("shared lease byte budget was not enforced")
	}
	limiter.Refund(10)
	if !limiter.Reserve(40, now.Add(500*time.Millisecond)) {
		t.Fatal("refunded shared lease budget was not reusable")
	}
	if limiter.Reserve(1, now.Add(time.Second)) {
		t.Fatal("expired lease continued forwarding")
	}
}

func TestAdmissionLimiterTakesStrictestAccountSessionAndLeaseRate(t *testing.T) {
	now := time.Now().UTC()
	account, _ := policy.NewRateLimiter(now.Add(time.Minute), 100, now)
	firstSession, _ := policy.NewRateLimiter(now.Add(time.Minute), 100, now)
	secondSession, _ := policy.NewRateLimiter(now.Add(time.Minute), 100, now)
	firstLease, _ := policy.NewLeaseLimiter(now.Add(time.Minute), 1000, 100, now)
	secondLease, _ := policy.NewLeaseLimiter(now.Add(time.Minute), 1000, 100, now)
	first, _ := policy.NewAdmissionLimiter(account, firstSession, firstLease)
	second, _ := policy.NewAdmissionLimiter(account, secondSession, secondLease)
	if !first.Reserve(80, now) || second.Reserve(30, now) {
		t.Fatal("separate sessions exceeded their shared account rate")
	}
	if !second.Reserve(30, now.Add(time.Second)) {
		t.Fatal("shared account rate did not refill")
	}
}
