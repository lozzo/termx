package deviceauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lozzow/termx/web-control/internal/account"
)

const (
	defaultExpiry       = 10 * time.Minute
	maxExpiry           = 15 * time.Minute
	defaultPollInterval = 5 * time.Second
	maxActiveCodes      = 128
	maxDecisionAttempts = 5
	defaultRetention    = time.Hour
)

type Config struct {
	DB       *sql.DB
	Accounts *account.Service
	Clock    Clock
}

type Service struct {
	db       *sql.DB
	accounts *account.Service
	clock    Clock
}

func NewService(cfg Config) *Service {
	clock := cfg.Clock
	if clock == nil {
		clock = realClock{}
	}
	return &Service{db: cfg.DB, accounts: cfg.Accounts, clock: clock}
}

func (s *Service) Create(ctx context.Context, in CreateInput) (CreateResult, error) {
	if s == nil || s.db == nil {
		return CreateResult{}, errors.New("device auth service is not configured")
	}
	if count, err := s.activeCodeCount(ctx); err != nil {
		return CreateResult{}, err
	} else if count >= maxActiveCodes {
		return CreateResult{}, ErrRateLimited
	}
	expiresIn := in.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = defaultExpiry
	}
	if expiresIn > maxExpiry {
		expiresIn = maxExpiry
	}
	deviceCode := randomToken("tdc")
	userCode := randomUserCode()
	verificationURI := strings.TrimSpace(in.VerificationURI)
	if verificationURI == "" {
		verificationURI = "/device"
	}
	now := s.clock.Now().UTC()
	expiresAt := now.Add(expiresIn)
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO device_auth_codes(
			id, device_code_hash, user_code_hash, status, client_name, verification_uri,
			poll_interval_seconds, expires_at, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, randomToken("dau"), hashCode(deviceCode), hashCode(normalizeUserCode(userCode)), StatusPending, strings.TrimSpace(in.ClientName), verificationURI,
		int(defaultPollInterval.Seconds()), formatTime(expiresAt), formatTime(now), formatTime(now)); err != nil {
		return CreateResult{}, fmt.Errorf("create device auth code: %w", err)
	}
	return CreateResult{
		DeviceCode:              deviceCode,
		UserCode:                userCode,
		VerificationURI:         verificationURI,
		VerificationURIComplete: verificationURIComplete(verificationURI, userCode),
		ExpiresAt:               expiresAt,
		ExpiresInSeconds:        int(expiresIn.Seconds()),
		IntervalSeconds:         int(defaultPollInterval.Seconds()),
	}, nil
}

func (s *Service) Poll(ctx context.Context, in PollInput) (PollResult, error) {
	if s == nil || s.db == nil {
		return PollResult{}, errors.New("device auth service is not configured")
	}
	deviceCode := strings.TrimSpace(in.DeviceCode)
	if deviceCode == "" {
		return PollResult{}, errors.New("device code is required")
	}
	row, err := s.loadByDeviceCode(ctx, deviceCode)
	if err != nil {
		return PollResult{}, err
	}
	now := s.clock.Now().UTC()
	if !now.Before(row.ExpiresAt) {
		_ = s.markStatus(ctx, row.ID, StatusExpired, now)
		return PollResult{}, ErrExpired
	}
	switch row.Status {
	case StatusPending:
		return PollResult{Status: StatusPending}, nil
	case StatusRejected:
		return PollResult{}, ErrAccessDenied
	case StatusExpired:
		return PollResult{}, ErrExpired
	case StatusConsumed:
		return PollResult{}, ErrAlreadyConsumed
	case StatusApproved:
		if strings.TrimSpace(row.ApprovedUserID) == "" {
			return PollResult{}, errors.New("approved user is missing")
		}
		if s.accounts == nil {
			return PollResult{}, errors.New("account service is not configured")
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return PollResult{}, err
		}
		defer func() {
			if tx != nil {
				_ = tx.Rollback()
			}
		}()
		res, err := tx.ExecContext(ctx, `
			UPDATE device_auth_codes
			SET status = ?, consumed_at = ?, updated_at = ?
			WHERE id = ? AND status = ? AND consumed_at IS NULL
		`, StatusConsumed, formatTime(now), formatTime(now), row.ID, StatusApproved)
		if err != nil {
			return PollResult{}, err
		}
		rows, err := res.RowsAffected()
		if err != nil {
			return PollResult{}, err
		}
		if rows != 1 {
			return PollResult{}, ErrAlreadyConsumed
		}
		auth, err := s.accounts.IssueForUserIDInTx(ctx, tx, row.ApprovedUserID)
		if err != nil {
			return PollResult{}, err
		}
		if err := tx.Commit(); err != nil {
			return PollResult{}, err
		}
		tx = nil
		return PollResult{Status: StatusApproved, Auth: auth}, nil
	default:
		return PollResult{}, fmt.Errorf("unsupported device auth status %q", row.Status)
	}
}

func (s *Service) Approve(ctx context.Context, in DecisionInput) error {
	return s.decide(ctx, in, StatusApproved)
}

func (s *Service) Reject(ctx context.Context, in DecisionInput) error {
	return s.decide(ctx, in, StatusRejected)
}

func (s *Service) CleanupExpired(ctx context.Context, in CleanupInput) (CleanupResult, error) {
	if s == nil || s.db == nil {
		return CleanupResult{}, errors.New("device auth service is not configured")
	}
	now := in.Now.UTC()
	if now.IsZero() {
		now = s.clock.Now().UTC()
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE device_auth_codes
		SET status = ?, updated_at = ?
		WHERE status = ? AND expires_at <= ?
	`, StatusExpired, formatTime(now), StatusPending, formatTime(now))
	if err != nil {
		return CleanupResult{}, err
	}
	expired, err := result.RowsAffected()
	if err != nil {
		return CleanupResult{}, err
	}
	retention := in.Retention
	if retention <= 0 {
		retention = defaultRetention
	}
	deleteBefore := now.Add(-retention)
	deletedResult, err := s.db.ExecContext(ctx, `
		DELETE FROM device_auth_codes
		WHERE status IN (?, ?, ?, ?) AND updated_at <= ?
	`, StatusExpired, StatusRejected, StatusConsumed, StatusApproved, formatTime(deleteBefore))
	if err != nil {
		return CleanupResult{}, err
	}
	deleted, err := deletedResult.RowsAffected()
	if err != nil {
		return CleanupResult{}, err
	}
	return CleanupResult{Expired: expired, Deleted: deleted}, nil
}

func (s *Service) decide(ctx context.Context, in DecisionInput, status string) error {
	if s == nil || s.db == nil {
		return errors.New("device auth service is not configured")
	}
	userID := strings.TrimSpace(in.UserID)
	userCode := normalizeUserCode(in.UserCode)
	if userID == "" || userCode == "" {
		return errors.New("user id and user code are required")
	}
	now := s.clock.Now().UTC()
	row, err := s.loadByUserCode(ctx, userCode)
	if err != nil {
		if errors.Is(err, errDeviceAuthNotFound) {
			_ = s.recordUnknownAttempt(ctx, userID, now)
		}
		return err
	}
	if !now.Before(row.ExpiresAt) {
		_ = s.markStatus(ctx, row.ID, StatusExpired, now)
		return ErrExpired
	}
	if row.AttemptCount >= maxDecisionAttempts || !row.LockedAt.IsZero() {
		return ErrAttemptLocked
	}
	if row.Status != StatusPending {
		if row.Status == StatusRejected {
			return ErrAccessDenied
		}
		if row.Status == StatusConsumed {
			return ErrAlreadyConsumed
		}
		return fmt.Errorf("device auth code is %s", row.Status)
	}
	var approvedUserID any
	if status == StatusApproved {
		approvedUserID = userID
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE device_auth_codes
		SET status = ?, approved_user_id = ?, decided_at = ?, reason = ?, attempt_count = attempt_count + 1, updated_at = ?
		WHERE id = ? AND status = ?
	`, status, approvedUserID, formatTime(now), strings.TrimSpace(in.Reason), formatTime(now), row.ID, StatusPending)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return errors.New("device auth code decision lost race")
	}
	return nil
}

type authCodeRow struct {
	ID             string
	Status         string
	ApprovedUserID string
	ExpiresAt      time.Time
	AttemptCount   int
	LockedAt       time.Time
}

var errDeviceAuthNotFound = errors.New("device auth code not found")

func (s *Service) loadByDeviceCode(ctx context.Context, code string) (authCodeRow, error) {
	return s.loadByHash(ctx, "device_code_hash", hashCode(code))
}

func (s *Service) loadByUserCode(ctx context.Context, code string) (authCodeRow, error) {
	return s.loadByHash(ctx, "user_code_hash", hashCode(code))
}

func (s *Service) loadByHash(ctx context.Context, column string, hash string) (authCodeRow, error) {
	var row authCodeRow
	var expiresAt string
	var lockedAt sql.NullString
	query := fmt.Sprintf(`SELECT id, status, COALESCE(approved_user_id, ''), expires_at, attempt_count, locked_at FROM device_auth_codes WHERE %s = ?`, column)
	if err := s.db.QueryRowContext(ctx, query, hash).Scan(&row.ID, &row.Status, &row.ApprovedUserID, &expiresAt, &row.AttemptCount, &lockedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return authCodeRow{}, errDeviceAuthNotFound
		}
		return authCodeRow{}, err
	}
	parsed, err := time.Parse(time.RFC3339Nano, expiresAt)
	if err != nil {
		return authCodeRow{}, err
	}
	row.ExpiresAt = parsed
	if lockedAt.Valid && strings.TrimSpace(lockedAt.String) != "" {
		locked, err := time.Parse(time.RFC3339Nano, lockedAt.String)
		if err != nil {
			return authCodeRow{}, err
		}
		row.LockedAt = locked
	}
	return row, nil
}

func (s *Service) activeCodeCount(ctx context.Context) (int, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM device_auth_codes WHERE status = ?`, StatusPending).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (s *Service) recordUnknownAttempt(ctx context.Context, userID string, now time.Time) error {
	var id string
	var attempts int
	err := s.db.QueryRowContext(ctx, `
		SELECT id, attempt_count
		FROM device_auth_codes
		WHERE status = ? AND expires_at > ?
		ORDER BY created_at DESC
		LIMIT 1
	`, StatusPending, formatTime(now)).Scan(&id, &attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	nextAttempts := attempts + 1
	var lockedAt any
	if nextAttempts >= maxDecisionAttempts {
		lockedAt = formatTime(now)
	}
	_, err = s.db.ExecContext(ctx, `
		UPDATE device_auth_codes
		SET attempt_count = ?, locked_at = ?, updated_at = ?
		WHERE id = ?
	`, nextAttempts, lockedAt, formatTime(now), id)
	return err
}

func (s *Service) markStatus(ctx context.Context, id string, status string, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE device_auth_codes SET status = ?, updated_at = ? WHERE id = ?`, status, formatTime(now), id)
	return err
}

func randomToken(prefix string) string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	}
	return prefix + "_" + base64.RawURLEncoding.EncodeToString(raw[:])
}

func randomUserCode() string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "TERM-XDEV-CODE"
	}
	encoded := strings.ToUpper(hex.EncodeToString(raw[:]))
	return encoded[:4] + "-" + encoded[4:8] + "-" + encoded[8:12] + "-" + encoded[12:16]
}

func normalizeUserCode(code string) string {
	return strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(code), " ", "-"))
}

func hashCode(code string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(code)))
	return hex.EncodeToString(sum[:])
}

func verificationURIComplete(base string, userCode string) string {
	if strings.Contains(base, "?") {
		return base + "&user_code=" + userCode
	}
	return strings.TrimRight(base, "/") + "?user_code=" + userCode
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}
