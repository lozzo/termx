package connect

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
	ErrMachineNotOwned = errors.New("machine is not owned by user")
	ErrTicketExpired   = errors.New("managed ticket expired")
	ErrTicketUsed      = errors.New("managed ticket already used")
	ErrWrongMachine    = errors.New("managed ticket machine mismatch")
)

const (
	defaultTicketTTL = 2 * time.Minute
	MaxTicketTTL     = 5 * time.Minute
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

func (s *Service) CreateManagedTicket(ctx context.Context, in CreateManagedTicketInput) (ManagedTicket, error) {
	if s == nil || s.db == nil {
		return ManagedTicket{}, errors.New("connect service is not configured")
	}
	userID := strings.TrimSpace(in.UserID)
	machineID := strings.TrimSpace(in.MachineID)
	terminalID := strings.TrimSpace(in.TerminalID)
	if userID == "" || machineID == "" || terminalID == "" {
		return ManagedTicket{}, errors.New("user id, machine id, and terminal id are required")
	}
	ttl := in.TTL
	if ttl <= 0 {
		ttl = defaultTicketTTL
	}
	if ttl > MaxTicketTTL {
		ttl = MaxTicketTTL
	}
	now := s.clock.Now().UTC()
	ticket := ManagedTicket{
		ID:         randomID("ct"),
		UserID:     userID,
		MachineID:  machineID,
		TerminalID: terminalID,
		Path:       PathManaged,
		ExpiresAt:  now.Add(ttl),
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ManagedTicket{}, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	result, err := tx.ExecContext(ctx, `
		INSERT INTO connect_tickets(id, user_id, machine_id, terminal_id, path, allow_relay, expires_at, created_at)
		SELECT ?, ?, ?, ?, ?, ?, ?, ?
		WHERE EXISTS (
			SELECT 1 FROM machines WHERE id = ? AND owner_user_id = ?
		)
	`, ticket.ID, ticket.UserID, ticket.MachineID, ticket.TerminalID, ticket.Path, 0, formatTime(ticket.ExpiresAt), formatTime(now), ticket.MachineID, ticket.UserID)
	if err != nil {
		return ManagedTicket{}, fmt.Errorf("create managed ticket: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return ManagedTicket{}, err
	}
	if rows != 1 {
		return ManagedTicket{}, ErrMachineNotOwned
	}
	if err := tx.Commit(); err != nil {
		return ManagedTicket{}, err
	}
	tx = nil
	return ticket, nil
}

func (s *Service) VerifyManagedTicket(ctx context.Context, in VerifyManagedTicketInput) (ManagedTicket, error) {
	if s == nil || s.db == nil {
		return ManagedTicket{}, errors.New("connect service is not configured")
	}
	ticketID := strings.TrimSpace(in.TicketID)
	machineID := strings.TrimSpace(in.MachineID)
	if ticketID == "" || machineID == "" {
		return ManagedTicket{}, errors.New("ticket id and machine id are required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ManagedTicket{}, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	var ticket ManagedTicket
	var allowRelay int
	var expiresAt string
	var usedAt sql.NullString
	err = tx.QueryRowContext(ctx, `
		SELECT id, user_id, machine_id, terminal_id, path, allow_relay, expires_at, used_at
		FROM connect_tickets
		WHERE id = ?
	`, ticketID).Scan(&ticket.ID, &ticket.UserID, &ticket.MachineID, &ticket.TerminalID, &ticket.Path, &allowRelay, &expiresAt, &usedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ManagedTicket{}, ErrTicketExpired
	}
	if err != nil {
		return ManagedTicket{}, err
	}
	ticket.AllowRelay = allowRelay != 0
	if usedAt.Valid {
		return ManagedTicket{}, ErrTicketUsed
	}
	if ticket.MachineID != machineID {
		return ManagedTicket{}, ErrWrongMachine
	}
	parsed, err := time.Parse(time.RFC3339Nano, expiresAt)
	if err != nil {
		return ManagedTicket{}, err
	}
	ticket.ExpiresAt = parsed
	if !s.clock.Now().UTC().Before(ticket.ExpiresAt) {
		return ManagedTicket{}, ErrTicketExpired
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE connect_tickets
		SET used_at = ?
		WHERE id = ? AND used_at IS NULL
	`, formatTime(s.clock.Now()), ticket.ID)
	if err != nil {
		return ManagedTicket{}, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return ManagedTicket{}, err
	}
	if rows != 1 {
		return ManagedTicket{}, ErrTicketUsed
	}
	if err := tx.Commit(); err != nil {
		return ManagedTicket{}, err
	}
	tx = nil
	return ticket, nil
}

func (s *Service) requireOwnedMachine(ctx context.Context, userID string, machineID string) error {
	var owner sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT owner_user_id FROM machines WHERE id = ?`, machineID).Scan(&owner)
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
