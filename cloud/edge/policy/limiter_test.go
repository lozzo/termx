package policy_test

import (
	"context"
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

func TestGroupLimiterWaitShapesInsteadOfRejecting(t *testing.T) {
	now := time.Now().UTC()
	limiter, err := policy.NewGroupLimiter(now.Add(5*time.Second), 1<<20, 64*1024, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := limiter.Wait(context.Background(), 64*1024); err != nil {
		t.Fatalf("consume initial burst: %v", err)
	}
	started := time.Now()
	if err := limiter.Wait(context.Background(), 8*1024); err != nil {
		t.Fatalf("shape bytes after burst: %v", err)
	}
	elapsed := time.Since(started)
	if elapsed < 80*time.Millisecond || elapsed > time.Second {
		t.Fatalf("8 KiB at 64 KiB/s waited %s, want approximately 125ms", elapsed)
	}
}

func TestGroupLimiterCanceledWaitRefundsHardQuota(t *testing.T) {
	now := time.Now().UTC()
	limiter, err := policy.NewGroupLimiter(now.Add(5*time.Second), 128*1024, 64*1024, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := limiter.Wait(context.Background(), 64*1024); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	started := time.Now()
	if err := limiter.Wait(ctx, 64*1024); err == nil {
		t.Fatal("rate wait ignored cancellation")
	}
	if elapsed := time.Since(started); elapsed > 300*time.Millisecond {
		t.Fatalf("canceled rate wait returned after %s", elapsed)
	}
	if !limiter.Reserve(64*1024, time.Now().Add(2*time.Second)) {
		t.Fatal("canceled rate wait leaked the hard byte reservation")
	}
}
