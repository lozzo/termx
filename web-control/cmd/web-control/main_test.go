package main

import (
	"context"
	"database/sql"
	"testing"
)

func TestOpenStoreFromEnvMigratesSQLite(t *testing.T) {
	t.Setenv("TERMX_WEB_CONTROL_SQLITE_DSN", "file:termx-web-control-main-test?mode=memory&cache=shared")
	db, err := openStoreFromEnv(context.Background())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	assertTableExists(t, db, "users")
	assertTableExists(t, db, "relay_sessions")
}

func assertTableExists(t *testing.T, db *sql.DB, table string) {
	t.Helper()

	var name string
	err := db.QueryRow("SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&name)
	if err != nil {
		t.Fatalf("table %s not found: %v", table, err)
	}
}
