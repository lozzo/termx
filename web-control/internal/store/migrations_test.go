package store_test

import (
	"context"
	"database/sql"
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
		"machines",
		"app_devices",
		"app_certificates",
		"hubs",
		"connect_tickets",
		"rendezvous_channels",
		"relay_sessions",
		"relay_usage_monthly",
	} {
		assertTableExists(t, db, table)
	}
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
