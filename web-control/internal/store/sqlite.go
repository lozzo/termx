package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
)

func OpenSQLite(ctx context.Context, dsn string) (*sql.DB, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, errors.New("sqlite dsn is required")
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// SQLite PRAGMAs are connection-local through database/sql. Keep the
	// skeleton opener conservative until a pooled connection hook is added.
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	return db, nil
}

func Migrate(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return errors.New("db is nil")
	}
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		return fmt.Errorf("enable foreign keys: %w", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration transaction: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	for _, stmt := range migrationStatements {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("apply migration: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migrations: %w", err)
	}
	tx = nil
	return nil
}

var migrationStatements = []string{
	`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
	)`,
	`CREATE TABLE IF NOT EXISTS users (
		id TEXT PRIMARY KEY,
		email TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL,
		role TEXT NOT NULL DEFAULT 'user',
		created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
	)`,
	`CREATE TABLE IF NOT EXISTS plans (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		monthly_relay_bytes INTEGER NOT NULL DEFAULT 0,
		relay_session_limit INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
	)`,
	`CREATE TABLE IF NOT EXISTS subscriptions (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		plan_id TEXT NOT NULL REFERENCES plans(id),
		status TEXT NOT NULL,
		current_period_start TEXT,
		current_period_end TEXT,
		created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
	)`,
	`CREATE TABLE IF NOT EXISTS machines (
		id TEXT PRIMARY KEY,
		owner_user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
		machine_public_key TEXT NOT NULL,
		display_name TEXT NOT NULL,
		hostname TEXT NOT NULL DEFAULT '',
		platform TEXT NOT NULL DEFAULT '',
		last_seen_at TEXT,
		created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
	)`,
	`CREATE TABLE IF NOT EXISTS app_devices (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		app_public_key TEXT NOT NULL,
		display_name TEXT NOT NULL,
		created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
	)`,
	`CREATE TABLE IF NOT EXISTS app_certificates (
		id TEXT PRIMARY KEY,
		machine_id TEXT NOT NULL REFERENCES machines(id) ON DELETE CASCADE,
		app_device_id TEXT NOT NULL REFERENCES app_devices(id) ON DELETE CASCADE,
		certificate_payload TEXT NOT NULL,
		certificate_signature TEXT NOT NULL,
		revoked_at TEXT,
		expires_at TEXT NOT NULL,
		created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
	)`,
	`CREATE TABLE IF NOT EXISTS hubs (
		id TEXT PRIMARY KEY,
		region TEXT NOT NULL,
		http_url TEXT NOT NULL,
		status TEXT NOT NULL,
		capacity INTEGER NOT NULL DEFAULT 0,
		last_heartbeat_at TEXT,
		created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
	)`,
	`CREATE TABLE IF NOT EXISTS connect_tickets (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		machine_id TEXT NOT NULL REFERENCES machines(id) ON DELETE CASCADE,
		terminal_id TEXT NOT NULL,
		path TEXT NOT NULL CHECK (path IN ('local', 'public_p2p', 'managed')),
		allow_relay INTEGER NOT NULL DEFAULT 0,
		used_at TEXT,
		expires_at TEXT NOT NULL,
		created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
	)`,
	`CREATE TABLE IF NOT EXISTS rendezvous_channels (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		machine_id TEXT NOT NULL REFERENCES machines(id) ON DELETE CASCADE,
		terminal_id TEXT NOT NULL,
		secret_hash TEXT NOT NULL,
		expires_at TEXT NOT NULL,
		created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
	)`,
	`CREATE TABLE IF NOT EXISTS relay_sessions (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		machine_id TEXT NOT NULL REFERENCES machines(id) ON DELETE CASCADE,
		hub_id TEXT NOT NULL REFERENCES hubs(id),
		status TEXT NOT NULL,
		rate_limit_bps INTEGER,
		bytes_rx INTEGER NOT NULL DEFAULT 0,
		bytes_tx INTEGER NOT NULL DEFAULT 0,
		last_heartbeat_at TEXT NOT NULL,
		expires_at TEXT NOT NULL,
		created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
	)`,
	`CREATE TABLE IF NOT EXISTS relay_usage_monthly (
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		month TEXT NOT NULL,
		bytes_used INTEGER NOT NULL DEFAULT 0,
		updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
		PRIMARY KEY (user_id, month)
	)`,
	`INSERT OR IGNORE INTO schema_migrations(version) VALUES (1)`,
}
