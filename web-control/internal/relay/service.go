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
	ErrMachineNotOwned    = errors.New("machine is not owned by user")
	ErrRelayNotAllowed    = errors.New("relay is not allowed for current plan")
	ErrRelayQuotaExceeded = errors.New("relay quota exceeded")
	ErrRelaySessionLimit  = errors.New("relay session limit reached")
	ErrHubUnavailable     = errors.New("hub is unavailable")
)

const (
	defaultLeaseTTL = 2 * time.Minute
	MaxLeaseTTL     = 10 * time.Minute
)

type Config struct {
	DB    *sql.DB
	Clock Clock
}

type Service struct {
	db    *sql.DB
	clock Clock
}

func NewService(cfg Config) *Service {
	clock := cfg.Clock
	if clock == nil {
		clock = realClock{}
	}
	return &Service{db: cfg.DB, clock: clock}
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
	policy, err := loadRelayPolicy(ctx, tx, userID, s.clock.Now())
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
	if remaining <= 0 {
		return RelayLease{}, ErrRelayQuotaExceeded
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
		RelayBytesRemaining: remaining,
		RelaySessionLimit:   policy.sessionLimit,
		RelayThrottleBps:    policy.throttleBps,
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

type relayPolicy struct {
	allowRelay   bool
	monthlyBytes int64
	sessionLimit int
	throttleBps  int64
	periodEnd    *time.Time
}

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

func loadRelayPolicy(ctx context.Context, tx *sql.Tx, userID string, now time.Time) (relayPolicy, error) {
	var monthlyBytes int64
	var sessionLimit int
	var end sql.NullString
	err := tx.QueryRowContext(ctx, `
		SELECT p.monthly_relay_bytes, p.relay_session_limit, s.current_period_end
		FROM subscriptions s
		JOIN plans p ON p.id = s.plan_id
		WHERE s.user_id = ?
			AND s.status = 'active'
			AND (s.current_period_end IS NULL OR s.current_period_end > ?)
		ORDER BY s.created_at DESC
		LIMIT 1
	`, userID, formatTime(now)).Scan(&monthlyBytes, &sessionLimit, &end)
	if errors.Is(err, sql.ErrNoRows) {
		return relayPolicy{}, nil
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
			return relayPolicy{}, nil
		}
	}
	policy := relayPolicy{
		allowRelay:   monthlyBytes > 0 && sessionLimit > 0,
		monthlyBytes: monthlyBytes,
		sessionLimit: sessionLimit,
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
