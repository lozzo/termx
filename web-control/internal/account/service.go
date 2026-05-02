package account

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type Config struct {
	DB       *sql.DB
	Clock    Clock
	Tokens   TokenIssuer
	Payments PaymentProvider
}

type Service struct {
	db       *sql.DB
	clock    Clock
	tokens   TokenIssuer
	payments PaymentProvider
}

type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func NewService(cfg Config) *Service {
	clock := cfg.Clock
	if clock == nil {
		clock = realClock{}
	}
	return &Service{db: cfg.DB, clock: clock, tokens: cfg.Tokens, payments: cfg.Payments}
}

func (s *Service) Register(ctx context.Context, in RegisterInput) (AuthResult, error) {
	email, err := normalizeEmail(in.Email)
	if err != nil {
		return AuthResult{}, err
	}
	if len(in.Password) < 8 {
		return AuthResult{}, errors.New("password must be at least 8 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		return AuthResult{}, err
	}
	user := User{ID: randomID("usr"), Email: email, Role: "user"}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AuthResult{}, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	if _, err := tx.ExecContext(ctx, `INSERT INTO users(id, email, password_hash, role) VALUES (?, ?, ?, ?)`, user.ID, user.Email, string(hash), user.Role); err != nil {
		return AuthResult{}, fmt.Errorf("insert user: %w", err)
	}
	if err := s.ensureSeedPlansWithQuerier(ctx, tx); err != nil {
		return AuthResult{}, err
	}
	result, err := s.issueAuthInTx(ctx, tx, user)
	if err != nil {
		return AuthResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return AuthResult{}, err
	}
	tx = nil
	return result, nil
}

func (s *Service) Login(ctx context.Context, in LoginInput) (AuthResult, error) {
	email, err := normalizeEmail(in.Email)
	if err != nil {
		return AuthResult{}, err
	}
	user, hash, err := s.loadUserByEmail(ctx, email)
	if err != nil {
		return AuthResult{}, errors.New("invalid email or password")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(in.Password)); err != nil {
		return AuthResult{}, errors.New("invalid email or password")
	}
	return s.issueAuth(ctx, user)
}

func (s *Service) Refresh(ctx context.Context, refreshToken string) (AuthResult, error) {
	if strings.TrimSpace(refreshToken) == "" {
		return AuthResult{}, errors.New("refresh token is required")
	}
	if s.tokens == nil {
		return AuthResult{}, errors.New("token issuer is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AuthResult{}, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	user, err := s.rotateRefreshInTx(ctx, tx, s.tokens.HashRefreshToken(refreshToken))
	if err != nil {
		return AuthResult{}, err
	}
	result, err := s.issueAuthInTx(ctx, tx, user)
	if err != nil {
		return AuthResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return AuthResult{}, err
	}
	tx = nil
	return result, nil
}

func (s *Service) Me(ctx context.Context, accessToken string) (AuthResult, error) {
	if strings.TrimSpace(accessToken) == "" {
		return AuthResult{}, errors.New("access token is required")
	}
	if s.tokens == nil {
		return AuthResult{}, errors.New("token issuer is required")
	}
	userID, err := s.tokens.VerifyAccess(accessToken, s.clock.Now())
	if err != nil {
		return AuthResult{}, err
	}
	user, err := s.loadUserByID(ctx, userID)
	if err != nil {
		return AuthResult{}, err
	}
	plan, sub, err := s.currentPlan(ctx, user.ID)
	if err != nil {
		return AuthResult{}, err
	}
	return AuthResult{User: user, Plan: plan, Subscription: sub}, nil
}

func (s *Service) CreateSubscriptionOrder(ctx context.Context, userID string, planID string) (PaymentOrder, error) {
	if _, ok := planCatalog[planID]; !ok || planID == PlanRegisteredFree {
		return PaymentOrder{}, errors.New("unsupported paid plan")
	}
	if s.payments == nil {
		return PaymentOrder{}, errors.New("payment provider is required")
	}
	if err := s.ensureSeedPlans(ctx); err != nil {
		return PaymentOrder{}, err
	}
	order, err := s.payments.CreateOrder(ctx, userID, planID)
	if err != nil {
		return PaymentOrder{}, err
	}
	result := PaymentOrder{
		ID:              randomID("ord"),
		UserID:          userID,
		PlanID:          planID,
		ProviderOrderID: order.ID,
		Status:          order.Status,
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO payment_orders(id, user_id, plan_id, provider_order_id, status)
		VALUES (?, ?, ?, ?, ?)
	`, result.ID, result.UserID, result.PlanID, result.ProviderOrderID, result.Status); err != nil {
		return PaymentOrder{}, err
	}
	return result, nil
}

func (s *Service) SyncPayment(ctx context.Context, orderID string) (PaymentSyncResult, error) {
	if s.payments == nil {
		return PaymentSyncResult{}, errors.New("payment provider is required")
	}
	order, err := s.loadPaymentOrder(ctx, orderID)
	if err != nil {
		return PaymentSyncResult{}, err
	}
	providerOrder, err := s.payments.GetOrder(ctx, order.ProviderOrderID)
	if err != nil {
		return PaymentSyncResult{}, err
	}
	if providerOrder.ID != order.ProviderOrderID || providerOrder.UserID != order.UserID || providerOrder.PlanID != order.PlanID {
		return PaymentSyncResult{}, errors.New("payment provider order mismatch")
	}
	order.Status = providerOrder.Status
	status := subscriptionStatusForPayment(order.Status)
	if status == "" {
		if order.Status == PaymentPending {
			if _, err := s.db.ExecContext(ctx, `UPDATE payment_orders SET status = ?, updated_at = ? WHERE id = ?`, order.Status, formatTime(s.clock.Now()), order.ID); err != nil {
				return PaymentSyncResult{}, err
			}
			return PaymentSyncResult{Order: order, Plan: planCatalog[PlanRegisteredFree]}, nil
		}
		return PaymentSyncResult{}, errors.New("unsupported payment status")
	}
	periodStart := s.clock.Now()
	periodEnd := periodStart.AddDate(0, 1, 0)
	sub := Subscription{
		ID:              randomID("sub"),
		UserID:          order.UserID,
		PlanID:          order.PlanID,
		Status:          status,
		ProviderOrderID: order.ProviderOrderID,
		PeriodStart:     &periodStart,
		PeriodEnd:       &periodEnd,
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PaymentSyncResult{}, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	if _, err := tx.ExecContext(ctx, `UPDATE payment_orders SET status = ?, updated_at = ? WHERE id = ?`, order.Status, formatTime(s.clock.Now()), order.ID); err != nil {
		return PaymentSyncResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO subscriptions(id, user_id, plan_id, status, provider_order_id, current_period_start, current_period_end)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, sub.ID, sub.UserID, sub.PlanID, sub.Status, sub.ProviderOrderID, formatTime(periodStart), formatTime(periodEnd)); err != nil {
		return PaymentSyncResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return PaymentSyncResult{}, err
	}
	tx = nil

	plan := planForSubscription(sub.Status, sub.PlanID)
	return PaymentSyncResult{Order: order, Plan: plan, Subscription: sub}, nil
}

func (s *Service) issueAuth(ctx context.Context, user User) (AuthResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AuthResult{}, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	result, err := s.issueAuthInTx(ctx, tx, user)
	if err != nil {
		return AuthResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return AuthResult{}, err
	}
	tx = nil
	return result, nil
}

func (s *Service) issueAuthInTx(ctx context.Context, tx *sql.Tx, user User) (AuthResult, error) {
	if s.tokens == nil {
		return AuthResult{}, errors.New("token issuer is required")
	}
	access, err := s.tokens.IssueAccess(user.ID, s.clock.Now())
	if err != nil {
		return AuthResult{}, err
	}
	refresh, refreshHash, err := s.tokens.NewRefreshToken()
	if err != nil {
		return AuthResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO sessions(id, user_id, refresh_token_hash, expires_at)
		VALUES (?, ?, ?, ?)
	`, randomID("ses"), user.ID, refreshHash, formatTime(s.clock.Now().Add(30*24*time.Hour))); err != nil {
		return AuthResult{}, err
	}
	plan, sub, err := s.currentPlanWithQuerier(ctx, tx, user.ID)
	if err != nil {
		return AuthResult{}, err
	}
	return AuthResult{User: user, Plan: plan, Subscription: sub, AccessToken: access, RefreshToken: refresh}, nil
}

func (s *Service) rotateRefreshInTx(ctx context.Context, tx *sql.Tx, refreshHash string) (User, error) {
	var sessionID string
	var userID string
	var expiresAt string
	err := tx.QueryRowContext(ctx, `
		SELECT id, user_id, expires_at
		FROM sessions
		WHERE refresh_token_hash = ? AND revoked_at IS NULL
	`, refreshHash).Scan(&sessionID, &userID, &expiresAt)
	if err != nil {
		return User{}, errors.New("invalid refresh token")
	}
	expiry, err := time.Parse(time.RFC3339Nano, expiresAt)
	if err != nil {
		return User{}, err
	}
	if !s.clock.Now().Before(expiry) {
		return User{}, errors.New("refresh token expired")
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE sessions
		SET revoked_at = ?
		WHERE id = ? AND revoked_at IS NULL
	`, formatTime(s.clock.Now()), sessionID)
	if err != nil {
		return User{}, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return User{}, err
	}
	if rows != 1 {
		return User{}, errors.New("invalid refresh token")
	}
	user, err := s.loadUserByIDTx(ctx, tx, userID)
	if err != nil {
		return User{}, err
	}
	return user, nil
}

func (s *Service) currentPlan(ctx context.Context, userID string) (Plan, *Subscription, error) {
	return s.currentPlanWithQuerier(ctx, s.db, userID)
}

func (s *Service) currentPlanWithQuerier(ctx context.Context, q queryer, userID string) (Plan, *Subscription, error) {
	if err := s.ensureSeedPlansWithQuerier(ctx, q); err != nil {
		return Plan{}, nil, err
	}
	var sub Subscription
	var start sql.NullString
	var end sql.NullString
	err := q.QueryRowContext(ctx, `
		SELECT id, user_id, plan_id, status, provider_order_id, current_period_start, current_period_end
		FROM subscriptions
		WHERE user_id = ?
		ORDER BY created_at DESC
		LIMIT 1
	`, userID).Scan(&sub.ID, &sub.UserID, &sub.PlanID, &sub.Status, &sub.ProviderOrderID, &start, &end)
	if errors.Is(err, sql.ErrNoRows) {
		plan := planCatalog[PlanRegisteredFree]
		return plan, nil, nil
	}
	if err != nil {
		return Plan{}, nil, err
	}
	if start.Valid {
		if parsed, err := time.Parse(time.RFC3339Nano, start.String); err == nil {
			sub.PeriodStart = &parsed
		}
	}
	if end.Valid {
		if parsed, err := time.Parse(time.RFC3339Nano, end.String); err == nil {
			sub.PeriodEnd = &parsed
		}
	}
	if sub.PeriodEnd != nil && !s.clock.Now().Before(*sub.PeriodEnd) {
		plan := planCatalog[PlanRegisteredFree]
		return plan, &sub, nil
	}
	plan := planForSubscription(sub.Status, sub.PlanID)
	return plan, &sub, nil
}

func (s *Service) ensureSeedPlans(ctx context.Context) error {
	return s.ensureSeedPlansWithQuerier(ctx, s.db)
}

func (s *Service) ensureSeedPlansWithQuerier(ctx context.Context, q queryer) error {
	for _, plan := range planCatalog {
		if _, err := q.ExecContext(ctx, `
			INSERT OR IGNORE INTO plans(id, name, monthly_relay_bytes, relay_session_limit, relay_throttle_bps)
			VALUES (?, ?, ?, ?, ?)
		`, plan.ID, plan.Name, plan.MonthlyRelayBytes, plan.RelaySessionLimit, plan.RelayThrottleBps); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) loadUserByEmail(ctx context.Context, email string) (User, string, error) {
	var user User
	var hash string
	err := s.db.QueryRowContext(ctx, `SELECT id, email, role, password_hash FROM users WHERE email = ?`, email).Scan(&user.ID, &user.Email, &user.Role, &hash)
	return user, hash, err
}

func (s *Service) loadUserByID(ctx context.Context, id string) (User, error) {
	var user User
	err := s.db.QueryRowContext(ctx, `SELECT id, email, role FROM users WHERE id = ?`, id).Scan(&user.ID, &user.Email, &user.Role)
	return user, err
}

func (s *Service) loadUserByIDTx(ctx context.Context, tx *sql.Tx, id string) (User, error) {
	var user User
	err := tx.QueryRowContext(ctx, `SELECT id, email, role FROM users WHERE id = ?`, id).Scan(&user.ID, &user.Email, &user.Role)
	return user, err
}

func (s *Service) loadPaymentOrder(ctx context.Context, id string) (PaymentOrder, error) {
	var order PaymentOrder
	err := s.db.QueryRowContext(ctx, `
		SELECT id, user_id, plan_id, provider_order_id, status
		FROM payment_orders
		WHERE id = ?
	`, id).Scan(&order.ID, &order.UserID, &order.PlanID, &order.ProviderOrderID, &order.Status)
	return order, err
}

func normalizeEmail(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if _, err := mail.ParseAddress(value); err != nil {
		return "", errors.New("valid email is required")
	}
	return value, nil
}

func subscriptionStatusForPayment(status string) string {
	switch status {
	case PaymentPaid:
		return SubscriptionActive
	case PaymentFailed:
		return SubscriptionPastDue
	case PaymentExpired:
		return SubscriptionExpired
	default:
		return ""
	}
}

func planForSubscription(status string, planID string) Plan {
	if status != SubscriptionActive {
		return planCatalog[PlanRegisteredFree]
	}
	return planCatalog[planID]
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

var planCatalog = map[string]Plan{
	PlanRegisteredFree: {
		ID:             PlanRegisteredFree,
		Name:           "Registered Free",
		AllowPublicP2P: true,
		AllowRelay:     false,
	},
	PlanPro: {
		ID:                 PlanPro,
		Name:               "Pro",
		AllowPublicP2P:     true,
		AllowRelay:         true,
		MonthlyRelayBytes:  5 * 1024 * 1024 * 1024,
		RelaySessionLimit:  2,
		RelayThrottleBps:   256 * 1024,
		RelayTransferFiles: true,
	},
}
