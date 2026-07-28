package policy

import (
	"errors"
	"sync"
	"time"
)

// LeaseLimiter 是 Edge Runtime 为一个 RelayLease 创建的共享流量预算。
// 同一租约下的所有 allocation 必须复用同一实例，从而共同执行租约到期、总字节和速率上限。
type LeaseLimiter struct {
	mu         sync.Mutex
	expiresAt  time.Time
	maxBytes   uint64
	rate       uint64
	used       uint64
	tokens     float64
	lastRefill time.Time
}

// RateLimiter 是 Edge Runtime 按 account 或 session 共享的速率桶。
// 新租约只能延长有效窗口或收紧速率；它不拥有订阅、订单或持久额度。
type RateLimiter struct {
	mu         sync.Mutex
	expiresAt  time.Time
	rate       uint64
	tokens     float64
	lastRefill time.Time
}

// AdmissionLimiter 组合 account、session、lease 三层预算；任一层拒绝都原子归还前序预留。
type AdmissionLimiter struct {
	account *RateLimiter
	session *RateLimiter
	lease   *LeaseLimiter
}

// NewLeaseLimiter 冻结 Controller 已签名租约中的执行上限；非法或已过期上限不能进入数据面。
func NewLeaseLimiter(expiresAt time.Time, maxBytes, maxRateBytesPerSecond uint64, now time.Time) (*LeaseLimiter, error) {
	expiresAt, now = expiresAt.UTC(), now.UTC()
	if !expiresAt.After(now) || maxBytes == 0 || maxRateBytesPerSecond == 0 {
		return nil, errors.New("active Relay lease expiry, byte limit, and rate limit are required")
	}
	return &LeaseLimiter{
		expiresAt: expiresAt, maxBytes: maxBytes, rate: maxRateBytesPerSecond,
		tokens: float64(maxRateBytesPerSecond), lastRefill: now,
	}, nil
}

// Renew applies a Controller-authorized extension without resetting bytes used
// by the existing physical TURN allocation.
func (limiter *LeaseLimiter) Renew(expiresAt time.Time, maxBytes, maxRateBytesPerSecond uint64, now time.Time) error {
	if limiter == nil || maxBytes == 0 || maxRateBytesPerSecond == 0 {
		return errors.New("active Relay lease expiry, byte limit, and rate limit are required")
	}
	expiresAt, now = expiresAt.UTC(), now.UTC()
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	if !limiter.expiresAt.After(now) || !expiresAt.After(limiter.expiresAt) {
		return errors.New("Relay lease renewal must extend an active lease")
	}
	limiter.refill(now)
	limiter.expiresAt = expiresAt
	limiter.maxBytes = maxBytes
	limiter.rate = maxRateBytesPerSecond
	if limiter.tokens > float64(maxRateBytesPerSecond) {
		limiter.tokens = float64(maxRateBytesPerSecond)
	}
	return nil
}

// NewRateLimiter 为一个活跃 account/session 创建内存速率桶。
func NewRateLimiter(expiresAt time.Time, rate uint64, now time.Time) (*RateLimiter, error) {
	expiresAt, now = expiresAt.UTC(), now.UTC()
	if !expiresAt.After(now) || rate == 0 {
		return nil, errors.New("active rate limiter expiry and rate are required")
	}
	return &RateLimiter{expiresAt: expiresAt, rate: rate, tokens: float64(rate), lastRefill: now}, nil
}

// Restrict 用新的已签名租约延长共享桶存活期，并在重叠窗口内只允许收紧速率。
func (limiter *RateLimiter) Restrict(expiresAt time.Time, rate uint64, now time.Time) error {
	if limiter == nil || rate == 0 || !expiresAt.UTC().After(now.UTC()) {
		return errors.New("active rate limiter expiry and rate are required")
	}
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	if !limiter.expiresAt.After(now.UTC()) {
		limiter.rate, limiter.tokens, limiter.lastRefill = rate, float64(rate), now.UTC()
	} else if rate < limiter.rate {
		limiter.rate = rate
		if limiter.tokens > float64(rate) {
			limiter.tokens = float64(rate)
		}
	}
	if expiresAt.UTC().After(limiter.expiresAt) {
		limiter.expiresAt = expiresAt.UTC()
	}
	return nil
}

// Expired 供唯一 Runtime actor 清理不再被任何活跃租约使用的共享速率桶。
func (limiter *RateLimiter) Expired(now time.Time) bool {
	if limiter == nil {
		return true
	}
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	return !limiter.expiresAt.After(now.UTC())
}

// NewAdmissionLimiter 绑定当前 Relay allocation 必须共同通过的三层内存预算。
func NewAdmissionLimiter(account, session *RateLimiter, lease *LeaseLimiter) (*AdmissionLimiter, error) {
	if account == nil || session == nil || lease == nil {
		return nil, errors.New("account, session, and lease limiters are required")
	}
	return &AdmissionLimiter{account: account, session: session, lease: lease}, nil
}

// Reserve 按 account、session、lease 顺序占用预算，失败时归还已经占用的层级。
func (limiter *AdmissionLimiter) Reserve(count uint64, now time.Time) bool {
	if limiter == nil || !limiter.account.reserve(count, now) {
		return false
	}
	if !limiter.session.reserve(count, now) {
		limiter.account.refund(count)
		return false
	}
	if !limiter.lease.Reserve(count, now) {
		limiter.session.refund(count)
		limiter.account.refund(count)
		return false
	}
	return true
}

// Refund 归还底层 socket 未实际写出的三层预留。
func (limiter *AdmissionLimiter) Refund(count uint64) {
	if limiter == nil || count == 0 {
		return
	}
	limiter.lease.Refund(count)
	limiter.session.refund(count)
	limiter.account.refund(count)
}

// Reserve 原子占用租约级字节与 token；租约到期后即使 TURN allocation 尚未关闭也会拒绝转发。
func (limiter *LeaseLimiter) Reserve(count uint64, now time.Time) bool {
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

func (limiter *LeaseLimiter) refill(now time.Time) {
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

// Refund 归还底层 socket 未实际写出的预留字节和 token，不改变已经成功转发的 usage。
func (limiter *LeaseLimiter) Refund(count uint64) {
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

func (limiter *RateLimiter) reserve(count uint64, now time.Time) bool {
	if limiter == nil || count == 0 {
		return false
	}
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	now = now.UTC()
	if !limiter.expiresAt.After(now) {
		return false
	}
	elapsed := now.Sub(limiter.lastRefill).Seconds()
	if elapsed > 0 {
		limiter.tokens += elapsed * float64(limiter.rate)
		if maximum := float64(limiter.rate); limiter.tokens > maximum {
			limiter.tokens = maximum
		}
		limiter.lastRefill = now
	}
	if float64(count) > limiter.tokens {
		return false
	}
	limiter.tokens -= float64(count)
	return true
}

func (limiter *RateLimiter) refund(count uint64) {
	if limiter == nil || count == 0 {
		return
	}
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	limiter.tokens += float64(count)
	if maximum := float64(limiter.rate); limiter.tokens > maximum {
		limiter.tokens = maximum
	}
}
