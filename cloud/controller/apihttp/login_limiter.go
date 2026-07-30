package apihttp

import (
	"crypto/sha256"
	"errors"
	"net/netip"
	"sync"
	"time"

	"github.com/anytty/anytty/cloud/controller/account"
)

var errLoginRateLimited = errors.New("login attempt rate limit exceeded")

type loginLimiterConfig struct {
	globalLimit       uint32
	clientLimit       uint32
	accountLimit      uint32
	window            time.Duration
	bucketTTL         time.Duration
	maxClientBuckets  int
	maxAccountBuckets int
	now               func() time.Time
}

type loginBucket struct {
	count       uint32
	windowStart time.Time
	lastSeen    time.Time
}

// loginLimiter is deliberately scoped to the password login endpoint.
type loginLimiter struct {
	mu       sync.Mutex
	config   loginLimiterConfig
	global   loginBucket
	clients  map[netip.Addr]loginBucket
	accounts map[[sha256.Size]byte]loginBucket
}

func newLoginLimiter(config loginLimiterConfig) (*loginLimiter, error) {
	if config.globalLimit == 0 || config.clientLimit == 0 || config.accountLimit == 0 || config.window <= 0 || config.bucketTTL < config.window || config.maxClientBuckets <= 0 || config.maxAccountBuckets <= 0 {
		return nil, errors.New("bounded login limiter configuration is required")
	}
	if config.now == nil {
		config.now = time.Now
	}
	return &loginLimiter{config: config, clients: make(map[netip.Addr]loginBucket), accounts: make(map[[sha256.Size]byte]loginBucket)}, nil
}

func newDefaultLoginLimiter() *loginLimiter {
	limiter, err := newLoginLimiter(loginLimiterConfig{
		globalLimit: 300, clientLimit: 30, accountLimit: 10,
		window: time.Minute, bucketTTL: 10 * time.Minute,
		maxClientBuckets: 4096, maxAccountBuckets: 16384,
	})
	if err != nil {
		panic(err)
	}
	return limiter
}

func (limiter *loginLimiter) allow(client netip.Addr, login string) bool {
	if !client.IsValid() {
		return false
	}
	client = client.Unmap()
	accountDigest := sha256.Sum256([]byte(account.NormalizeLogin(login)))
	now := limiter.config.now().UTC()

	limiter.mu.Lock()
	defer limiter.mu.Unlock()

	globalBucket := advanceLoginBucket(limiter.global, now, limiter.config.window)
	if globalBucket.count >= limiter.config.globalLimit {
		return false
	}
	clientBucket, clientFound := limiter.clients[client]
	accountBucket, accountFound := limiter.accounts[accountDigest]
	if clientFound {
		clientBucket = advanceLoginBucket(clientBucket, now, limiter.config.window)
	}
	if accountFound {
		accountBucket = advanceLoginBucket(accountBucket, now, limiter.config.window)
	}
	if (clientFound && clientBucket.count >= limiter.config.clientLimit) || (accountFound && accountBucket.count >= limiter.config.accountLimit) {
		return false
	}

	pruneLoginBuckets(limiter.clients, now, limiter.config.bucketTTL)
	pruneLoginBuckets(limiter.accounts, now, limiter.config.bucketTTL)
	clientBucket, clientFound = limiter.clients[client]
	accountBucket, accountFound = limiter.accounts[accountDigest]
	if (!clientFound && len(limiter.clients) >= limiter.config.maxClientBuckets) || (!accountFound && len(limiter.accounts) >= limiter.config.maxAccountBuckets) {
		return false
	}

	clientBucket = advanceLoginBucket(clientBucket, now, limiter.config.window)
	accountBucket = advanceLoginBucket(accountBucket, now, limiter.config.window)
	if globalBucket.count >= limiter.config.globalLimit || clientBucket.count >= limiter.config.clientLimit || accountBucket.count >= limiter.config.accountLimit {
		return false
	}
	globalBucket.count++
	globalBucket.lastSeen = now
	clientBucket.count++
	clientBucket.lastSeen = now
	accountBucket.count++
	accountBucket.lastSeen = now
	limiter.global = globalBucket
	limiter.clients[client] = clientBucket
	limiter.accounts[accountDigest] = accountBucket
	return true
}

func advanceLoginBucket(bucket loginBucket, now time.Time, window time.Duration) loginBucket {
	if bucket.windowStart.IsZero() || !now.Before(bucket.windowStart.Add(window)) {
		bucket.count = 0
		bucket.windowStart = now
	}
	return bucket
}

func pruneLoginBuckets[K comparable](buckets map[K]loginBucket, now time.Time, ttl time.Duration) {
	for key, bucket := range buckets {
		if !bucket.lastSeen.IsZero() && !now.Before(bucket.lastSeen.Add(ttl)) {
			delete(buckets, key)
		}
	}
}
