package relay

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrMachineNotOwned      = errors.New("machine is not owned by user")
	ErrRelayNotAllowed      = errors.New("relay is not allowed for current plan")
	ErrRelaySessionLimit    = errors.New("relay session limit reached")
	ErrHubUnavailable       = errors.New("hub is unavailable")
	ErrRelaySessionNotFound = errors.New("relay session not found")
	ErrInvalidUsageDelta    = errors.New("usage deltas must be non-negative")
)

const (
	defaultLeaseTTL = 2 * time.Minute
	MaxLeaseTTL     = 10 * time.Minute
)

type Config struct {
	DB            *sql.DB
	Clock         Clock
	DevFreePolicy *PolicyOverride
}

type Service struct {
	db            *sql.DB
	clock         Clock
	devFreePolicy *PolicyOverride
}

type PolicyOverride struct {
	AllowRelay   bool
	MonthlyBytes int64
	SessionLimit int
	ThrottleBps  int64
}

func NewService(cfg Config) *Service {
	clock := cfg.Clock
	if clock == nil {
		clock = realClock{}
	}
	policy := cfg.DevFreePolicy
	if policy == nil {
		defaultPolicy := PolicyOverride{
			AllowRelay:   true,
			MonthlyBytes: devFreeMonthlyRelayBytes,
			SessionLimit: devFreeRelaySessionLimit,
			ThrottleBps:  defaultThrottleBps,
		}
		policy = &defaultPolicy
	}
	return &Service{db: cfg.DB, clock: clock, devFreePolicy: policy}
}

func (s *Service) CreateLease(ctx context.Context, in CreateLeaseInput) (RelayLease, error) {
	if s == nil || s.db == nil {
		return RelayLease{}, errors.New("relay service is not configured")
	}
	userID := strings.TrimSpace(in.UserID)
	machineID := strings.TrimSpace(in.MachineID)
	hubID := strings.TrimSpace(in.HubID)
	if userID == "" || machineID == "" || hubID == "" {
		return RelayLease{}, errors.New("user id, machine id, and hub id are required")
	}
	ttl := in.TTL
	if ttl <= 0 {
		ttl = defaultLeaseTTL
	}
	if ttl > MaxLeaseTTL {
		ttl = MaxLeaseTTL
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return RelayLease{}, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	if err := requireOwnedMachine(ctx, tx, userID, machineID); err != nil {
		return RelayLease{}, err
	}
	if err := requireHubOnline(ctx, tx, hubID); err != nil {
		return RelayLease{}, err
	}
	policy, err := s.loadRelayPolicy(ctx, tx, userID, s.clock.Now())
	if err != nil {
		return RelayLease{}, err
	}
	if !policy.allowRelay || policy.monthlyBytes <= 0 || policy.sessionLimit <= 0 {
		return RelayLease{}, ErrRelayNotAllowed
	}
	now := s.clock.Now().UTC()
	activeSessions, err := countActiveSessions(ctx, tx, userID, now)
	if err != nil {
		return RelayLease{}, err
	}
	if activeSessions >= policy.sessionLimit {
		return RelayLease{}, ErrRelaySessionLimit
	}
	used, err := monthlyUsage(ctx, tx, userID, now)
	if err != nil {
		return RelayLease{}, err
	}
	remaining := policy.monthlyBytes - used
	if remaining < 0 {
		remaining = 0
	}
	throttled := remaining <= 0
	rateLimitBps := int64(0)
	if throttled {
		rateLimitBps = policy.throttleBps
		if rateLimitBps <= 0 {
			rateLimitBps = defaultThrottleBps
		}
	}
	expiresAt := now.Add(ttl)
	if policy.periodEnd != nil && policy.periodEnd.Before(expiresAt) {
		expiresAt = policy.periodEnd.UTC()
	}
	lease := RelayLease{
		ID:                  randomID("rls"),
		UserID:              userID,
		MachineID:           machineID,
		HubID:               hubID,
		Path:                PathManaged,
		AllowRelay:          true,
		RelayInUse:          false,
		RelayThrottled:      throttled,
		RelayBytesRemaining: remaining,
		RelaySessionLimit:   policy.sessionLimit,
		RelayThrottleBps:    rateLimitBps,
		ExpiresAt:           expiresAt,
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO relay_sessions(id, user_id, machine_id, hub_id, status, rate_limit_bps, last_heartbeat_at, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, lease.ID, lease.UserID, lease.MachineID, lease.HubID, SessionLeased, nullableRateLimit(lease.RelayThrottleBps), formatTime(now), formatTime(lease.ExpiresAt), formatTime(now)); err != nil {
		return RelayLease{}, fmt.Errorf("create relay lease: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return RelayLease{}, err
	}
	tx = nil
	return lease, nil
}

func (s *Service) RecordHeartbeat(ctx context.Context, in HeartbeatInput) (HeartbeatResult, error) {
	if s == nil || s.db == nil {
		return HeartbeatResult{}, errors.New("relay service is not configured")
	}
	sessionID := strings.TrimSpace(in.SessionID)
	authenticatedHubID := strings.TrimSpace(in.AuthenticatedHubID)
	if sessionID == "" || authenticatedHubID == "" {
		return HeartbeatResult{}, errors.New("session id and authenticated hub id are required")
	}
	if in.BytesRXTotal < 0 || in.BytesTXTotal < 0 {
		return HeartbeatResult{}, ErrInvalidUsageDelta
	}
	now := s.clock.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return HeartbeatResult{}, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	session, err := loadSessionForHeartbeat(ctx, tx, sessionID, authenticatedHubID, now)
	if err != nil {
		return HeartbeatResult{}, err
	}
	if err := requireHubOnline(ctx, tx, authenticatedHubID); err != nil {
		return HeartbeatResult{}, err
	}
	policy, err := s.loadRelayPolicy(ctx, tx, session.UserID, now)
	if err != nil {
		return HeartbeatResult{}, err
	}
	if !policy.allowRelay || policy.monthlyBytes <= 0 || policy.sessionLimit <= 0 {
		return HeartbeatResult{}, ErrRelayNotAllowed
	}
	if in.BytesRXTotal < session.BytesRX || in.BytesTXTotal < session.BytesTX {
		return HeartbeatResult{}, ErrInvalidUsageDelta
	}
	rxDelta := in.BytesRXTotal - session.BytesRX
	txDelta := in.BytesTXTotal - session.BytesTX
	delta := rxDelta + txDelta
	if delta < rxDelta {
		return HeartbeatResult{}, ErrInvalidUsageDelta
	}
	if delta > 0 {
		if err := addMonthlyUsage(ctx, tx, session.UserID, now, delta); err != nil {
			return HeartbeatResult{}, err
		}
	}
	used, err := monthlyUsage(ctx, tx, session.UserID, now)
	if err != nil {
		return HeartbeatResult{}, err
	}
	remaining := policy.monthlyBytes - used
	if remaining < 0 {
		remaining = 0
	}
	throttled := remaining <= 0
	rateLimitBps := int64(0)
	if throttled {
		rateLimitBps = policy.throttleBps
		if rateLimitBps <= 0 {
			rateLimitBps = defaultThrottleBps
		}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE relay_sessions
		SET status = ?, bytes_rx = ?, bytes_tx = ?, rate_limit_bps = ?, last_heartbeat_at = ?
		WHERE id = ?
	`, SessionActive, in.BytesRXTotal, in.BytesTXTotal, nullableRateLimit(rateLimitBps), formatTime(now), session.ID); err != nil {
		return HeartbeatResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return HeartbeatResult{}, err
	}
	tx = nil
	return HeartbeatResult{
		SessionID:           session.ID,
		UserID:              session.UserID,
		MachineID:           session.MachineID,
		HubID:               session.HubID,
		Status:              SessionActive,
		RelayBytesUsed:      used,
		RelayBytesRemaining: remaining,
		RelayThrottled:      throttled,
		RateLimitBps:        rateLimitBps,
		LastHeartbeatAt:     now,
		ExpiresAt:           session.ExpiresAt,
	}, nil
}

func (s *Service) CleanupExpired(ctx context.Context, in CleanupInput) (int, error) {
	if s == nil || s.db == nil {
		return 0, errors.New("relay service is not configured")
	}
	now := in.Now.UTC()
	if now.IsZero() {
		now = s.clock.Now().UTC()
	}
	leasedAfter := in.ExpireLeasedAfter
	if leasedAfter <= 0 {
		leasedAfter = defaultLeaseTTL
	}
	activeAfter := in.ExpireActiveAfter
	if activeAfter <= 0 {
		activeAfter = in.HeartbeatTimeout
	}
	if activeAfter <= 0 {
		activeAfter = defaultLeaseTTL
	}
	leasedCutoff := now.Add(-leasedAfter)
	activeCutoff := now.Add(-activeAfter)
	result, err := s.db.ExecContext(ctx, `
		UPDATE relay_sessions
		SET status = ?
		WHERE status IN (?, ?)
			AND (
				expires_at <= ?
				OR (status = ? AND last_heartbeat_at <= ?)
				OR (status = ? AND last_heartbeat_at <= ?)
			)
	`, SessionExpired, SessionLeased, SessionActive, formatTime(now), SessionLeased, formatTime(leasedCutoff), SessionActive, formatTime(activeCutoff))
	if err != nil {
		return 0, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(rows), nil
}

type relayPolicy struct {
	allowRelay   bool
	monthlyBytes int64
	sessionLimit int
	throttleBps  int64
	periodEnd    *time.Time
}

const defaultThrottleBps = 256 * 1024

const (
	devFreeMonthlyRelayBytes = 512 * 1024 * 1024
	devFreeRelaySessionLimit = 1
)

func countActiveSessions(ctx context.Context, tx *sql.Tx, userID string, now time.Time) (int, error) {
	var count int
	err := tx.QueryRowContext(ctx, `
		SELECT COUNT(1)
		FROM relay_sessions
		WHERE user_id = ?
			AND status IN ('leased', 'active')
			AND expires_at > ?
	`, userID, formatTime(now)).Scan(&count)
	return count, err
}

func monthlyUsage(ctx context.Context, tx *sql.Tx, userID string, now time.Time) (int64, error) {
	var used sql.NullInt64
	month := now.UTC().Format("2006-01")
	err := tx.QueryRowContext(ctx, `
		SELECT bytes_used
		FROM relay_usage_monthly
		WHERE user_id = ? AND month = ?
	`, userID, month).Scan(&used)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if !used.Valid {
		return 0, nil
	}
	return used.Int64, nil
}

func addMonthlyUsage(ctx context.Context, tx *sql.Tx, userID string, now time.Time, delta int64) error {
	month := now.UTC().Format("2006-01")
	_, err := tx.ExecContext(ctx, `
		INSERT INTO relay_usage_monthly(user_id, month, bytes_used, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(user_id, month) DO UPDATE SET
			bytes_used = bytes_used + excluded.bytes_used,
			updated_at = excluded.updated_at
	`, userID, month, delta, formatTime(now))
	return err
}

type relaySession struct {
	ID        string
	UserID    string
	MachineID string
	HubID     string
	Status    string
	BytesRX   int64
	BytesTX   int64
	ExpiresAt time.Time
}

func loadSessionForHeartbeat(ctx context.Context, tx *sql.Tx, sessionID string, hubID string, now time.Time) (relaySession, error) {
	var session relaySession
	var expiresRaw string
	err := tx.QueryRowContext(ctx, `
		SELECT id, user_id, machine_id, hub_id, status, bytes_rx, bytes_tx, expires_at
		FROM relay_sessions
		WHERE id = ? AND hub_id = ? AND status IN ('leased', 'active') AND expires_at > ?
	`, sessionID, hubID, formatTime(now)).Scan(&session.ID, &session.UserID, &session.MachineID, &session.HubID, &session.Status, &session.BytesRX, &session.BytesTX, &expiresRaw)
	if errors.Is(err, sql.ErrNoRows) {
		return relaySession{}, ErrRelaySessionNotFound
	}
	if err != nil {
		return relaySession{}, err
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, expiresRaw)
	if err != nil {
		return relaySession{}, err
	}
	session.ExpiresAt = expiresAt.UTC()
	return session, nil
}

func requireOwnedMachine(ctx context.Context, tx *sql.Tx, userID string, machineID string) error {
	var owner sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT owner_user_id FROM machines WHERE id = ?`, machineID).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrMachineNotOwned
	}
	if err != nil {
		return err
	}
	if !owner.Valid || owner.String != userID {
		return ErrMachineNotOwned
	}
	return nil
}

func requireHubOnline(ctx context.Context, tx *sql.Tx, hubID string) error {
	var status string
	err := tx.QueryRowContext(ctx, `SELECT status FROM hubs WHERE id = ?`, hubID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrHubUnavailable
	}
	if err != nil {
		return err
	}
	if status != "online" {
		return ErrHubUnavailable
	}
	return nil
}

func (s *Service) loadRelayPolicy(ctx context.Context, tx *sql.Tx, userID string, now time.Time) (relayPolicy, error) {
	var monthlyBytes int64
	var sessionLimit int
	var throttleBps int64
	var end sql.NullString
	err := tx.QueryRowContext(ctx, `
		SELECT p.monthly_relay_bytes, p.relay_session_limit, p.relay_throttle_bps, s.current_period_end
		FROM subscriptions s
		JOIN plans p ON p.id = s.plan_id
		WHERE s.user_id = ?
			AND s.status = 'active'
			AND (s.current_period_end IS NULL OR s.current_period_end > ?)
		ORDER BY s.created_at DESC
		LIMIT 1
	`, userID, formatTime(now)).Scan(&monthlyBytes, &sessionLimit, &throttleBps, &end)
	if errors.Is(err, sql.ErrNoRows) {
		return s.devFreeRelayPolicy(), nil
	}
	if err != nil {
		return relayPolicy{}, err
	}
	if end.Valid {
		parsed, err := time.Parse(time.RFC3339Nano, end.String)
		if err != nil {
			return relayPolicy{}, err
		}
		if !now.Before(parsed) {
			return s.devFreeRelayPolicy(), nil
		}
	}
	policy := relayPolicy{
		allowRelay:   monthlyBytes > 0 && sessionLimit > 0,
		monthlyBytes: monthlyBytes,
		sessionLimit: sessionLimit,
		throttleBps:  throttleBps,
	}
	if !policy.allowRelay {
		return s.devFreeRelayPolicy(), nil
	}
	if end.Valid {
		parsed, err := time.Parse(time.RFC3339Nano, end.String)
		if err != nil {
			return relayPolicy{}, err
		}
		policy.periodEnd = &parsed
	}
	return policy, nil
}

func (s *Service) devFreeRelayPolicy() relayPolicy {
	if s == nil || s.devFreePolicy == nil || !s.devFreePolicy.AllowRelay {
		return relayPolicy{}
	}
	monthlyBytes := s.devFreePolicy.MonthlyBytes
	if monthlyBytes <= 0 {
		monthlyBytes = devFreeMonthlyRelayBytes
	}
	sessionLimit := s.devFreePolicy.SessionLimit
	if sessionLimit <= 0 {
		sessionLimit = devFreeRelaySessionLimit
	}
	throttleBps := s.devFreePolicy.ThrottleBps
	if throttleBps <= 0 {
		throttleBps = defaultThrottleBps
	}
	return relayPolicy{
		allowRelay:   true,
		monthlyBytes: monthlyBytes,
		sessionLimit: sessionLimit,
		throttleBps:  throttleBps,
	}
}

func nullableRateLimit(value int64) any {
	if value <= 0 {
		return nil
	}
	return value
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func randomID(prefix string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return prefix + "_" + base64.RawURLEncoding.EncodeToString(b[:])
}
