package policy

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const relayRateBurstBytes = 64 * 1024

var ErrRelayGrantLimit = errors.New("Relay grant byte, rate, or expiry limit exceeded")

// GroupLimiter is the process-local byte/rate executor shared by every
// physical allocation belonging to one Controller reservation.
type GroupLimiter struct {
	mu        sync.Mutex
	expiresAt time.Time
	maxBytes  uint64
	used      uint64
	burst     uint64
	rate      *rate.Limiter
}

func NewGroupLimiter(expiresAt time.Time, maxBytes, maxRateBytesPerSecond uint64, now time.Time) (*GroupLimiter, error) {
	expiresAt, now = expiresAt.UTC(), now.UTC()
	if !expiresAt.After(now) || maxBytes == 0 || maxRateBytesPerSecond == 0 {
		return nil, errors.New("active Relay grant expiry, byte limit, and rate limit are required")
	}
	burst := uint64(relayRateBurstBytes)
	if maxBytes < burst {
		burst = maxBytes
	}
	return &GroupLimiter{
		expiresAt: expiresAt,
		maxBytes:  maxBytes,
		burst:     burst,
		rate:      rate.NewLimiter(rate.Limit(maxRateBytesPerSecond), int(burst)),
	}, nil
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
	limiter.expiresAt = expiresAt
	return nil
}

// Wait reserves the hard byte budget and then shapes the aggregate traffic rate.
// The caller's context must be canceled when its physical allocation closes.
func (limiter *GroupLimiter) Wait(ctx context.Context, count uint64) error {
	if limiter == nil || ctx == nil || count == 0 {
		return errors.New("Relay group limiter, context, and byte count are required")
	}
	now := time.Now().UTC()
	limiter.mu.Lock()
	if !limiter.expiresAt.After(now) || limiter.used >= limiter.maxBytes || count > limiter.maxBytes-limiter.used {
		limiter.mu.Unlock()
		return ErrRelayGrantLimit
	}
	expiresAt := limiter.expiresAt
	limiter.used += count
	limiter.mu.Unlock()

	waitContext, cancel := context.WithDeadline(ctx, expiresAt)
	defer cancel()
	remaining := count
	for remaining > 0 {
		chunk := remaining
		if chunk > limiter.burst {
			chunk = limiter.burst
		}
		if err := limiter.rate.WaitN(waitContext, int(chunk)); err != nil {
			limiter.refundBytes(count)
			return fmt.Errorf("shape Relay traffic: %w", err)
		}
		remaining -= chunk
	}
	return nil
}

// Reserve is the non-blocking form used by admission tests and callers that
// explicitly prefer rejection over shaping.
func (limiter *GroupLimiter) Reserve(count uint64, now time.Time) bool {
	if limiter == nil || count == 0 {
		return false
	}
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	now = now.UTC()
	if !limiter.expiresAt.After(now) || limiter.used >= limiter.maxBytes || count > limiter.maxBytes-limiter.used || count > limiter.burst {
		return false
	}
	if !limiter.rate.AllowN(now, int(count)) {
		return false
	}
	limiter.used += count
	return true
}

// Refund releases hard quota for bytes that the socket did not transfer. Rate
// tokens stay consumed, which is conservative and prevents short-write loops
// from manufacturing extra bandwidth.
func (limiter *GroupLimiter) Refund(count uint64) {
	if limiter == nil || count == 0 {
		return
	}
	limiter.refundBytes(count)
}

func (limiter *GroupLimiter) refundBytes(count uint64) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	if count > limiter.used {
		count = limiter.used
	}
	limiter.used -= count
}
