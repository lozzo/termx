package connect_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/web-control/internal/connect"
	"github.com/lozzow/termx/web-control/internal/store"
)

func TestManagedTicketOwnerPolicyAndNoRelayForFreePlan(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTestDB(t, "file:termx-connect-ticket-policy?mode=memory&cache=shared")
	clock := &mutableClock{value: time.Date(2026, 5, 3, 5, 41, 0, 0, time.UTC)}
	svc := connect.NewService(connect.Config{DB: db, Clock: clock})
	seedUserAndMachine(t, ctx, db, "usr_1", "mach_1")

	ticket, err := svc.CreateManagedTicket(ctx, connect.CreateManagedTicketInput{
		UserID:     "usr_1",
		MachineID:  "mach_1",
		TerminalID: "term_1",
		TTL:        time.Minute,
	})
	if err != nil {
		t.Fatalf("create ticket: %v", err)
	}
	if ticket.Path != connect.PathManaged {
		t.Fatalf("path = %q", ticket.Path)
	}
	if ticket.AllowRelay || ticket.RelayInUse {
		t.Fatalf("free managed ticket enabled relay: %+v", ticket)
	}
	if ticket.RelayBytesRemaining != 0 || ticket.RelayThrottled {
		t.Fatalf("free ticket exposed relay quota/throttle state: %+v", ticket)
	}
	if !ticket.ExpiresAt.Equal(clock.value.Add(time.Minute)) {
		t.Fatalf("expires_at = %v", ticket.ExpiresAt)
	}
	payload, err := json.Marshal(ticket)
	if err != nil {
		t.Fatalf("marshal ticket: %v", err)
	}
	lower := strings.ToLower(string(payload))
	for _, forbidden := range []string{"turn:", "relay_path", "terminal_data", "file_data", "api_data", "events_data", "http_runtime", "websocket"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("ticket leaked forbidden runtime/relay field %q: %s", forbidden, payload)
		}
	}
}

func TestManagedTicketRejectsWrongOwnerExpiredAndWrongMachine(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTestDB(t, "file:termx-connect-ticket-verify?mode=memory&cache=shared")
	clock := &mutableClock{value: time.Date(2026, 5, 3, 5, 42, 0, 0, time.UTC)}
	svc := connect.NewService(connect.Config{DB: db, Clock: clock})
	seedUserAndMachine(t, ctx, db, "usr_owner", "mach_1")
	seedUserAndMachine(t, ctx, db, "usr_other", "mach_2")

	if _, err := svc.CreateManagedTicket(ctx, connect.CreateManagedTicketInput{
		UserID:     "usr_other",
		MachineID:  "mach_1",
		TerminalID: "term_1",
	}); !errors.Is(err, connect.ErrMachineNotOwned) {
		t.Fatalf("wrong owner create err = %v", err)
	}
	ticket, err := svc.CreateManagedTicket(ctx, connect.CreateManagedTicketInput{
		UserID:     "usr_owner",
		MachineID:  "mach_1",
		TerminalID: "term_1",
		TTL:        time.Second,
	})
	if err != nil {
		t.Fatalf("create ticket: %v", err)
	}
	if _, err := svc.VerifyManagedTicket(ctx, connect.VerifyManagedTicketInput{
		TicketID:  ticket.ID,
		MachineID: "mach_2",
	}); !errors.Is(err, connect.ErrWrongMachine) {
		t.Fatalf("wrong machine verify err = %v", err)
	}
	clock.value = clock.value.Add(2 * time.Second)
	if _, err := svc.VerifyManagedTicket(ctx, connect.VerifyManagedTicketInput{
		TicketID:  ticket.ID,
		MachineID: "mach_1",
	}); !errors.Is(err, connect.ErrTicketExpired) {
		t.Fatalf("expired verify err = %v", err)
	}
}

func TestManagedTicketIsSingleUse(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTestDB(t, "file:termx-connect-ticket-single-use?mode=memory&cache=shared")
	clock := &mutableClock{value: time.Date(2026, 5, 3, 5, 50, 0, 0, time.UTC)}
	svc := connect.NewService(connect.Config{DB: db, Clock: clock})
	seedUserAndMachine(t, ctx, db, "usr_owner", "mach_1")

	ticket, err := svc.CreateManagedTicket(ctx, connect.CreateManagedTicketInput{
		UserID:     "usr_owner",
		MachineID:  "mach_1",
		TerminalID: "term_1",
		TTL:        time.Minute,
	})
	if err != nil {
		t.Fatalf("create ticket: %v", err)
	}
	if _, err := svc.VerifyManagedTicket(ctx, connect.VerifyManagedTicketInput{
		TicketID:  ticket.ID,
		MachineID: "mach_1",
	}); err != nil {
		t.Fatalf("first verify: %v", err)
	}
	if _, err := svc.VerifyManagedTicket(ctx, connect.VerifyManagedTicketInput{
		TicketID:  ticket.ID,
		MachineID: "mach_1",
	}); !errors.Is(err, connect.ErrTicketUsed) {
		t.Fatalf("second verify err = %v", err)
	}
}

func TestManagedTicketTTLCapped(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTestDB(t, "file:termx-connect-ticket-ttl-cap?mode=memory&cache=shared")
	clock := &mutableClock{value: time.Date(2026, 5, 3, 5, 56, 0, 0, time.UTC)}
	svc := connect.NewService(connect.Config{DB: db, Clock: clock})
	seedUserAndMachine(t, ctx, db, "usr_owner", "mach_1")

	ticket, err := svc.CreateManagedTicket(ctx, connect.CreateManagedTicketInput{
		UserID:     "usr_owner",
		MachineID:  "mach_1",
		TerminalID: "term_1",
		TTL:        24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("create ticket: %v", err)
	}
	want := clock.value.Add(connect.MaxTicketTTL)
	if !ticket.ExpiresAt.Equal(want) {
		t.Fatalf("expires_at = %v, want capped %v", ticket.ExpiresAt, want)
	}
}

func TestManagedTicketCreateUsesOwnerConditionedInsert(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTestDB(t, "file:termx-connect-ticket-owner-conditioned?mode=memory&cache=shared")
	clock := &mutableClock{value: time.Date(2026, 5, 3, 5, 58, 0, 0, time.UTC)}
	svc := connect.NewService(connect.Config{DB: db, Clock: clock})
	seedUserAndMachine(t, ctx, db, "usr_owner", "mach_1")
	if _, err := db.ExecContext(ctx, `UPDATE machines SET owner_user_id = NULL WHERE id = 'mach_1'`); err != nil {
		t.Fatalf("clear owner: %v", err)
	}
	if _, err := svc.CreateManagedTicket(ctx, connect.CreateManagedTicketInput{
		UserID:     "usr_owner",
		MachineID:  "mach_1",
		TerminalID: "term_1",
		TTL:        time.Minute,
	}); !errors.Is(err, connect.ErrMachineNotOwned) {
		t.Fatalf("unowned create err = %v", err)
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(1) FROM connect_tickets WHERE machine_id = 'mach_1'`).Scan(&count); err != nil {
		t.Fatalf("count tickets: %v", err)
	}
	if count != 0 {
		t.Fatalf("created ticket for stale/unowned machine")
	}
}

func openTestDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	ctx := context.Background()
	db, err := store.OpenSQLite(ctx, dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func seedUserAndMachine(t *testing.T, ctx context.Context, db *sql.DB, userID string, machineID string) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO users(id, email, password_hash, role)
		VALUES (?, ?, 'hash', 'user')
	`, userID, userID+"@example.com"); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO machines(id, owner_user_id, machine_public_key, display_name)
		VALUES (?, ?, ?, ?)
	`, machineID, userID, "pub_"+machineID, "Machine "+machineID); err != nil {
		t.Fatalf("insert machine: %v", err)
	}
}

type mutableClock struct {
	value time.Time
}

func (c *mutableClock) Now() time.Time {
	return c.value
}
