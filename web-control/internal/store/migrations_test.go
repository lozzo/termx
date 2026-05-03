package store_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/lozzow/termx/web-control/internal/store"
)

func TestOpenAndMigrateSQLiteCreatesCoreTables(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := store.OpenSQLite(ctx, "file:termx-web-control-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("second migrate should be idempotent: %v", err)
	}

	for _, table := range []string{
		"schema_migrations",
		"users",
		"plans",
		"subscriptions",
		"sessions",
		"payment_orders",
		"machines",
		"machine_terminals",
		"app_devices",
		"app_certificates",
		"hubs",
		"hub_agents",
		"hub_agent_policies",
		"connect_tickets",
		"rendezvous_channels",
		"rendezvous_messages",
		"relay_sessions",
		"relay_usage_monthly",
	} {
		assertTableExists(t, db, table)
	}
}

func TestMigrateUpgradesSlice1SubscriptionSchema(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := store.OpenSQLite(ctx, "file:termx-web-control-upgrade-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	for _, stmt := range slice1Schema {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("create slice 1 schema: %v", err)
		}
	}
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate slice 1 schema: %v", err)
	}

	assertColumnExists(t, db, "subscriptions", "provider_order_id")
	assertTableExists(t, db, "sessions")
	assertTableExists(t, db, "payment_orders")
	assertColumnExists(t, db, "plans", "relay_throttle_bps")
}

func TestMigrateUpgradesMachineClaimTokenSchema(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := store.OpenSQLite(ctx, "file:termx-web-control-machine-upgrade-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	for _, stmt := range slice2MachineSchema {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("create slice 2 machine schema: %v", err)
		}
	}
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate slice 2 machine schema: %v", err)
	}

	assertColumnExists(t, db, "machines", "claim_token_hash")
	assertTableExists(t, db, "machine_terminals")
	assertColumnExists(t, db, "hubs", "health_json")
	assertColumnExists(t, db, "hubs", "expires_at")
	assertTableExists(t, db, "hub_agents")
	assertTableExists(t, db, "hub_agent_policies")
}

func TestMigrateEnablesForeignKeys(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := store.OpenSQLite(ctx, "file:termx-web-control-fk-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var enabled int
	if err := db.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&enabled); err != nil {
		t.Fatalf("read foreign_keys pragma: %v", err)
	}
	if enabled != 1 {
		t.Fatalf("foreign_keys = %d, want 1", enabled)
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO subscriptions(id, user_id, plan_id, status)
		VALUES ('sub_missing_refs', 'missing_user', 'missing_plan', 'active')
	`)
	if err == nil {
		t.Fatal("insert subscription with missing user/plan succeeded, want foreign key failure")
	}
}

func assertTableExists(t *testing.T, db *sql.DB, table string) {
	t.Helper()

	var name string
	err := db.QueryRow("SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&name)
	if err != nil {
		t.Fatalf("table %s not found: %v", table, err)
	}
	if name != table {
		t.Fatalf("table name = %q, want %q", name, table)
	}
}

func assertColumnExists(t *testing.T, db *sql.DB, table string, column string) {
	t.Helper()

	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		t.Fatalf("table_info %s: %v", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name string
		var typ string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			t.Fatalf("scan table_info: %v", err)
		}
		if strings.EqualFold(name, column) {
			return
		}
	}
	t.Fatalf("column %s.%s not found", table, column)
}

var slice1Schema = []string{
	`CREATE TABLE users (
		id TEXT PRIMARY KEY,
		email TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL,
		role TEXT NOT NULL DEFAULT 'user',
		created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
	)`,
	`CREATE TABLE plans (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		monthly_relay_bytes INTEGER NOT NULL DEFAULT 0,
		relay_session_limit INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
	)`,
	`CREATE TABLE subscriptions (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		plan_id TEXT NOT NULL REFERENCES plans(id),
		status TEXT NOT NULL,
		current_period_start TEXT,
		current_period_end TEXT,
		created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
	)`,
}

var slice2MachineSchema = []string{
	`CREATE TABLE users (
		id TEXT PRIMARY KEY,
		email TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL,
		role TEXT NOT NULL DEFAULT 'user',
		created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
	)`,
	`CREATE TABLE machines (
		id TEXT PRIMARY KEY,
		owner_user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
		machine_public_key TEXT NOT NULL,
		display_name TEXT NOT NULL,
		hostname TEXT NOT NULL DEFAULT '',
		platform TEXT NOT NULL DEFAULT '',
		last_seen_at TEXT,
		created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
	)`,
}
