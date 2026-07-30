package apihttp

import (
	"crypto/sha256"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anytty/anytty/cloud/controller/account"
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
	state := captureLoginLimiter(limiter, netip.MustParseAddr("192.0.2.10"), "user@example.com")
	if state.globalCount != 25 || state.clientCount != 25 || state.accountCount != 25 {
		t.Fatalf("concurrent bucket counts = global %d, client %d, account %d; want 25 each", state.globalCount, state.clientCount, state.accountCount)
	}
}

func TestLoginLimiterAcceptedAttemptsIncrementAllBucketsExactlyOnce(t *testing.T) {
	now := time.Unix(1_500, 0).UTC()
	limiter := testLoginLimiter(t, loginLimiterConfig{
		globalLimit: 10, clientLimit: 10, accountLimit: 10,
		window: time.Minute, bucketTTL: 5 * time.Minute,
		maxClientBuckets: 2, maxAccountBuckets: 2, now: func() time.Time { return now },
	})
	client := netip.MustParseAddr("192.0.2.10")
	for want := uint32(1); want <= 2; want++ {
		if !limiter.allow(client, " User@Example.com ") {
			t.Fatalf("accepted attempt %d was rejected", want)
		}
		state := captureLoginLimiter(limiter, client, "user@example.com")
		if state.globalCount != want || state.clientCount != want || state.accountCount != want {
			t.Fatalf("bucket counts after attempt %d = global %d, client %d, account %d", want, state.globalCount, state.clientCount, state.accountCount)
		}
	}
}

func TestLoginLimiterRejectedAttemptsDoNotConsumeOrCreateSiblingBuckets(t *testing.T) {
	now := time.Unix(1_750, 0).UTC()
	newLimiter := func(clientLimit, accountLimit uint32, maxClients, maxAccounts int) *loginLimiter {
		return testLoginLimiter(t, loginLimiterConfig{
			globalLimit: 10, clientLimit: clientLimit, accountLimit: accountLimit,
			window: time.Minute, bucketTTL: 5 * time.Minute,
			maxClientBuckets: maxClients, maxAccountBuckets: maxAccounts, now: func() time.Time { return now },
		})
	}

	t.Run("client limit", func(t *testing.T) {
		limiter := newLimiter(1, 10, 10, 10)
		client := netip.MustParseAddr("192.0.2.1")
		if !limiter.allow(client, "first@example.com") || limiter.allow(client, "sibling@example.com") {
			t.Fatal("client limit setup did not produce one acceptance and one rejection")
		}
		state := captureLoginLimiter(limiter, client, "sibling@example.com")
		if state.globalCount != 1 || state.clientCount != 1 || !state.clientFound || state.accountFound || state.clientBuckets != 1 || state.accountBuckets != 1 {
			t.Fatalf("client rejection changed sibling state: %+v", state)
		}
	})

	t.Run("account limit", func(t *testing.T) {
		limiter := newLimiter(10, 1, 10, 10)
		if !limiter.allow(netip.MustParseAddr("192.0.2.1"), "shared@example.com") || limiter.allow(netip.MustParseAddr("192.0.2.2"), " SHARED@example.com ") {
			t.Fatal("account limit setup did not produce one acceptance and one rejection")
		}
		state := captureLoginLimiter(limiter, netip.MustParseAddr("192.0.2.2"), "shared@example.com")
		if state.globalCount != 1 || state.clientFound || !state.accountFound || state.accountCount != 1 || state.clientBuckets != 1 || state.accountBuckets != 1 {
			t.Fatalf("account rejection changed sibling state: %+v", state)
		}
	})

	for _, test := range []struct {
		name        string
		maxClients  int
		maxAccounts int
	}{
		{name: "client capacity", maxClients: 1, maxAccounts: 10},
		{name: "account capacity", maxClients: 10, maxAccounts: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			limiter := newLimiter(10, 10, test.maxClients, test.maxAccounts)
			if !limiter.allow(netip.MustParseAddr("192.0.2.1"), "first@example.com") || limiter.allow(netip.MustParseAddr("192.0.2.2"), "second@example.com") {
				t.Fatal("capacity setup did not produce one acceptance and one rejection")
			}
			state := captureLoginLimiter(limiter, netip.MustParseAddr("192.0.2.2"), "second@example.com")
			if state.globalCount != 1 || state.clientFound || state.accountFound || state.clientBuckets != 1 || state.accountBuckets != 1 {
				t.Fatalf("capacity rejection changed sibling state: %+v", state)
			}
		})
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

type loginLimiterSnapshot struct {
	globalCount    uint32
	clientCount    uint32
	accountCount   uint32
	clientFound    bool
	accountFound   bool
	clientBuckets  int
	accountBuckets int
}

func captureLoginLimiter(limiter *loginLimiter, client netip.Addr, login string) loginLimiterSnapshot {
	client = client.Unmap()
	digest := sha256.Sum256([]byte(account.NormalizeLogin(login)))
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	clientBucket, clientFound := limiter.clients[client]
	accountBucket, accountFound := limiter.accounts[digest]
	return loginLimiterSnapshot{
		globalCount: limiter.global.count, clientCount: clientBucket.count, accountCount: accountBucket.count,
		clientFound: clientFound, accountFound: accountFound,
		clientBuckets: len(limiter.clients), accountBuckets: len(limiter.accounts),
	}
}
