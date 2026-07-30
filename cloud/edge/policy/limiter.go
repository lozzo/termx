package policy

import (
	"errors"
	"sync"
	"time"
)

// GroupLimiter is the process-local byte/rate executor shared by every
// physical allocation belonging to one Controller reservation.
type GroupLimiter struct {
	mu         sync.Mutex
	expiresAt  time.Time
	maxBytes   uint64
	rate       uint64
	used       uint64
	tokens     float64
	lastRefill time.Time
}

func NewGroupLimiter(expiresAt time.Time, maxBytes, maxRateBytesPerSecond uint64, now time.Time) (*GroupLimiter, error) {
	expiresAt, now = expiresAt.UTC(), now.UTC()
	if !expiresAt.After(now) || maxBytes == 0 || maxRateBytesPerSecond == 0 {
		return nil, errors.New("active Relay grant expiry, byte limit, and rate limit are required")
	}
	return &GroupLimiter{expiresAt: expiresAt, maxBytes: maxBytes, rate: maxRateBytesPerSecond, tokens: float64(maxRateBytesPerSecond), lastRefill: now}, nil
}

// Renew changes only the Controller-authorized expiry; held bytes, slot count,
// byte usage, and rate budget are never reset or increased locally.
func (limiter *GroupLimiter) Renew(expiresAt time.Time, now time.Time) error {
	if limiter == nil {
		return errors.New("Relay group limiter is required")
	}
	expiresAt, now = expiresAt.UTC(), now.UTC()
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	if !limiter.expiresAt.After(now) || !expiresAt.After(limiter.expiresAt) {
		return errors.New("Relay renewal must extend an active grant")
	}
	limiter.refill(now)
	limiter.expiresAt = expiresAt
	return nil
}

func (limiter *GroupLimiter) Reserve(count uint64, now time.Time) bool {
	if limiter == nil || count == 0 {
		return false
	}
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	now = now.UTC()
	if !limiter.expiresAt.After(now) || limiter.used >= limiter.maxBytes || count > limiter.maxBytes-limiter.used {
		return false
	}
	limiter.refill(now)
	if float64(count) > limiter.tokens {
		return false
	}
	limiter.tokens -= float64(count)
	limiter.used += count
	return true
}

func (limiter *GroupLimiter) Refund(count uint64) {
	if limiter == nil || count == 0 {
		return
	}
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	if count > limiter.used {
		count = limiter.used
	}
	limiter.used -= count
	limiter.tokens += float64(count)
	if maximum := float64(limiter.rate); limiter.tokens > maximum {
		limiter.tokens = maximum
	}
}

func (limiter *GroupLimiter) refill(now time.Time) {
	elapsed := now.Sub(limiter.lastRefill).Seconds()
	if elapsed <= 0 {
		return
	}
	limiter.tokens += elapsed * float64(limiter.rate)
	if maximum := float64(limiter.rate); limiter.tokens > maximum {
		limiter.tokens = maximum
	}
	limiter.lastRefill = now
}
