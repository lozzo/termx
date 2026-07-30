package apihttp

import (
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLoginLimiterConcurrentAccountLimit(t *testing.T) {
	now := time.Unix(1_000, 0).UTC()
	limiter := testLoginLimiter(t, loginLimiterConfig{
		globalLimit: 200, clientLimit: 200, accountLimit: 25,
		window: time.Minute, bucketTTL: 5 * time.Minute,
		maxClientBuckets: 2, maxAccountBuckets: 2, now: func() time.Time { return now },
	})
	var allowed atomic.Int32
	var wait sync.WaitGroup
	for index := 0; index < 100; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if limiter.allow(netip.MustParseAddr("192.0.2.10"), "  User@Example.com ") {
				allowed.Add(1)
			}
		}()
	}
	wait.Wait()
	if got := allowed.Load(); got != 25 {
		t.Fatalf("allowed concurrent attempts = %d, want 25", got)
	}
}

func TestLoginLimiterWindowAndBucketTTL(t *testing.T) {
	now := time.Unix(2_000, 0).UTC()
	limiter := testLoginLimiter(t, loginLimiterConfig{
		globalLimit: 10, clientLimit: 10, accountLimit: 1,
		window: time.Minute, bucketTTL: 2 * time.Minute,
		maxClientBuckets: 1, maxAccountBuckets: 1, now: func() time.Time { return now },
	})
	if !limiter.allow(netip.MustParseAddr("192.0.2.1"), "first@example.com") || limiter.allow(netip.MustParseAddr("192.0.2.1"), "FIRST@example.com") {
		t.Fatal("normalized account bucket did not enforce its window")
	}
	now = now.Add(time.Minute)
	if !limiter.allow(netip.MustParseAddr("192.0.2.1"), "first@example.com") {
		t.Fatal("account bucket did not reset after its window")
	}
	now = now.Add(2 * time.Minute)
	if !limiter.allow(netip.MustParseAddr("192.0.2.2"), "second@example.com") {
		t.Fatal("expired client and account buckets were not evicted")
	}
}

func TestLoginLimiterBucketTableFullFailsClosed(t *testing.T) {
	now := time.Unix(3_000, 0).UTC()
	limiter := testLoginLimiter(t, loginLimiterConfig{
		globalLimit: 10, clientLimit: 10, accountLimit: 10,
		window: time.Minute, bucketTTL: 5 * time.Minute,
		maxClientBuckets: 1, maxAccountBuckets: 1, now: func() time.Time { return now },
	})
	if !limiter.allow(netip.MustParseAddr("192.0.2.1"), "first@example.com") {
		t.Fatal("first bucket was rejected")
	}
	if limiter.allow(netip.MustParseAddr("192.0.2.2"), "second@example.com") {
		t.Fatal("new keys were accepted after the hard bucket-table limit")
	}
	if !limiter.allow(netip.MustParseAddr("192.0.2.1"), "first@example.com") {
		t.Fatal("existing bounded buckets should remain usable")
	}
}

func TestLoginLimiterEnforcesGlobalAndClientBuckets(t *testing.T) {
	now := time.Unix(4_000, 0).UTC()
	global := testLoginLimiter(t, loginLimiterConfig{
		globalLimit: 2, clientLimit: 10, accountLimit: 10,
		window: time.Minute, bucketTTL: 5 * time.Minute,
		maxClientBuckets: 10, maxAccountBuckets: 10, now: func() time.Time { return now },
	})
	if !global.allow(netip.MustParseAddr("192.0.2.1"), "one@example.com") || !global.allow(netip.MustParseAddr("192.0.2.2"), "two@example.com") || global.allow(netip.MustParseAddr("192.0.2.3"), "three@example.com") {
		t.Fatal("global login bucket did not enforce its limit")
	}
	client := testLoginLimiter(t, loginLimiterConfig{
		globalLimit: 10, clientLimit: 2, accountLimit: 10,
		window: time.Minute, bucketTTL: 5 * time.Minute,
		maxClientBuckets: 10, maxAccountBuckets: 10, now: func() time.Time { return now },
	})
	address := netip.MustParseAddr("198.51.100.8")
	if !client.allow(address, "one@example.com") || !client.allow(address, "two@example.com") || client.allow(address, "three@example.com") {
		t.Fatal("client login bucket did not enforce its limit")
	}
}

func testLoginLimiter(t *testing.T, config loginLimiterConfig) *loginLimiter {
	t.Helper()
	limiter, err := newLoginLimiter(config)
	if err != nil {
		t.Fatal(err)
	}
	return limiter
}
