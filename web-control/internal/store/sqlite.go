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
	if err := applyMigrationUpgrades(ctx, tx); err != nil {
		return err
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
		relay_throttle_bps INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
	)`,
	`CREATE TABLE IF NOT EXISTS subscriptions (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		plan_id TEXT NOT NULL REFERENCES plans(id),
		status TEXT NOT NULL,
		provider_order_id TEXT NOT NULL DEFAULT '',
		current_period_start TEXT,
		current_period_end TEXT,
		created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
	)`,
	`CREATE TABLE IF NOT EXISTS sessions (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		refresh_token_hash TEXT NOT NULL UNIQUE,
		expires_at TEXT NOT NULL,
		revoked_at TEXT,
		created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
	)`,
	`CREATE TABLE IF NOT EXISTS device_auth_codes (
		id TEXT PRIMARY KEY,
		device_code_hash TEXT NOT NULL UNIQUE,
		user_code_hash TEXT NOT NULL UNIQUE,
		status TEXT NOT NULL,
		client_name TEXT NOT NULL DEFAULT '',
		verification_uri TEXT NOT NULL DEFAULT '',
		approved_user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
		attempt_count INTEGER NOT NULL DEFAULT 0,
		locked_at TEXT,
		poll_interval_seconds INTEGER NOT NULL DEFAULT 5,
		expires_at TEXT NOT NULL,
		consumed_at TEXT,
		decided_at TEXT,
		reason TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS payment_orders (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		plan_id TEXT NOT NULL REFERENCES plans(id),
		provider_order_id TEXT NOT NULL UNIQUE,
		status TEXT NOT NULL,
		created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
		updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
	)`,
	`CREATE TABLE IF NOT EXISTS machines (
		id TEXT PRIMARY KEY,
		owner_user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
		machine_public_key TEXT NOT NULL,
		claim_token_hash TEXT NOT NULL DEFAULT '',
		display_name TEXT NOT NULL,
		hostname TEXT NOT NULL DEFAULT '',
		platform TEXT NOT NULL DEFAULT '',
		last_seen_at TEXT,
		created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
	)`,
	`CREATE TABLE IF NOT EXISTS machine_terminals (
		id TEXT NOT NULL,
		machine_id TEXT NOT NULL REFERENCES machines(id) ON DELETE CASCADE,
		name TEXT NOT NULL DEFAULT '',
		command_json TEXT NOT NULL DEFAULT '[]',
		cols INTEGER NOT NULL DEFAULT 0,
		rows INTEGER NOT NULL DEFAULT 0,
		state TEXT NOT NULL DEFAULT '',
		last_seen_at TEXT NOT NULL,
		PRIMARY KEY (machine_id, id)
	)`,
	`CREATE TABLE IF NOT EXISTS agent_registration_nonces (
		machine_id TEXT NOT NULL REFERENCES machines(id) ON DELETE CASCADE,
		nonce TEXT NOT NULL,
		created_at TEXT NOT NULL,
		PRIMARY KEY (machine_id, nonce)
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
		weight INTEGER NOT NULL DEFAULT 0,
		health_json TEXT NOT NULL DEFAULT '{}',
		last_heartbeat_at TEXT,
		expires_at TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
		created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
	)`,
	`CREATE TABLE IF NOT EXISTS hub_agents (
		machine_id TEXT NOT NULL REFERENCES machines(id) ON DELETE CASCADE,
		agent_id TEXT NOT NULL,
		hub_id TEXT NOT NULL REFERENCES hubs(id) ON DELETE CASCADE,
		status TEXT NOT NULL,
		terminal_count INTEGER NOT NULL DEFAULT 0,
		last_seen_at TEXT NOT NULL,
		expires_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		PRIMARY KEY (machine_id, agent_id)
	)`,
	`CREATE TABLE IF NOT EXISTS hub_agent_policies (
		machine_id TEXT NOT NULL REFERENCES machines(id) ON DELETE CASCADE,
		agent_id TEXT NOT NULL,
		force_offline INTEGER NOT NULL DEFAULT 0,
		reason TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL,
		PRIMARY KEY (machine_id, agent_id)
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
	`CREATE TABLE IF NOT EXISTS rendezvous_messages (
		id TEXT PRIMARY KEY,
		channel_id TEXT NOT NULL REFERENCES rendezvous_channels(id) ON DELETE CASCADE,
		type TEXT NOT NULL CHECK (type IN ('offer', 'answer', 'candidate')),
		payload TEXT NOT NULL,
		created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
	)`,
	`CREATE INDEX IF NOT EXISTS rendezvous_messages_channel_created_idx
		ON rendezvous_messages(channel_id, created_at, id)`,
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

func applyMigrationUpgrades(ctx context.Context, tx *sql.Tx) error {
	hasProviderOrderID, err := columnExists(ctx, tx, "subscriptions", "provider_order_id")
	if err != nil {
		return fmt.Errorf("inspect subscriptions schema: %w", err)
	}
	if !hasProviderOrderID {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE subscriptions ADD COLUMN provider_order_id TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("add subscriptions.provider_order_id: %w", err)
		}
	}
	hasMachineClaimTokenHash, err := columnExists(ctx, tx, "machines", "claim_token_hash")
	if err != nil {
		return fmt.Errorf("inspect machines schema: %w", err)
	}
	if !hasMachineClaimTokenHash {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE machines ADD COLUMN claim_token_hash TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("add machines.claim_token_hash: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS machine_terminals (
		id TEXT NOT NULL,
		machine_id TEXT NOT NULL REFERENCES machines(id) ON DELETE CASCADE,
		name TEXT NOT NULL DEFAULT '',
		command_json TEXT NOT NULL DEFAULT '[]',
		cols INTEGER NOT NULL DEFAULT 0,
		rows INTEGER NOT NULL DEFAULT 0,
		state TEXT NOT NULL DEFAULT '',
		last_seen_at TEXT NOT NULL,
		PRIMARY KEY (machine_id, id)
	)`); err != nil {
		return fmt.Errorf("create machine_terminals: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS agent_registration_nonces (
		machine_id TEXT NOT NULL REFERENCES machines(id) ON DELETE CASCADE,
		nonce TEXT NOT NULL,
		created_at TEXT NOT NULL,
		PRIMARY KEY (machine_id, nonce)
	)`); err != nil {
		return fmt.Errorf("create agent_registration_nonces: %w", err)
	}
	hasPlanRelayThrottleBps, err := columnExists(ctx, tx, "plans", "relay_throttle_bps")
	if err != nil {
		return fmt.Errorf("inspect plans schema: %w", err)
	}
	if !hasPlanRelayThrottleBps {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE plans ADD COLUMN relay_throttle_bps INTEGER NOT NULL DEFAULT 0`); err != nil {
			return fmt.Errorf("add plans.relay_throttle_bps: %w", err)
		}
	}
	if err := ensureHubRegistrySchema(ctx, tx); err != nil {
		return err
	}
	if err := ensureDeviceAuthSchema(ctx, tx); err != nil {
		return err
	}
	return nil
}

func columnExists(ctx context.Context, tx *sql.Tx, table string, column string) (bool, error) {
	switch table {
	case "subscriptions", "machines", "plans", "hubs":
	default:
		return false, fmt.Errorf("unsupported migration table %q", table)
	}
	var count int
	query := fmt.Sprintf("SELECT COUNT(1) FROM pragma_table_info('%s') WHERE name = ?", table)
	if err := tx.QueryRowContext(ctx, query, column).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func ensureHubRegistrySchema(ctx context.Context, tx *sql.Tx) error {
	for _, column := range []struct {
		name string
		stmt string
	}{
		{name: "health_json", stmt: `ALTER TABLE hubs ADD COLUMN health_json TEXT NOT NULL DEFAULT '{}'`},
		{name: "expires_at", stmt: `ALTER TABLE hubs ADD COLUMN expires_at TEXT NOT NULL DEFAULT ''`},
		{name: "weight", stmt: `ALTER TABLE hubs ADD COLUMN weight INTEGER NOT NULL DEFAULT 0`},
		{name: "updated_at", stmt: `ALTER TABLE hubs ADD COLUMN updated_at TEXT NOT NULL DEFAULT ''`},
	} {
		exists, err := columnExists(ctx, tx, "hubs", column.name)
		if err != nil {
			return fmt.Errorf("inspect hubs.%s: %w", column.name, err)
		}
		if !exists {
			if _, err := tx.ExecContext(ctx, column.stmt); err != nil {
				return fmt.Errorf("add hubs.%s: %w", column.name, err)
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS hub_agents (
		machine_id TEXT NOT NULL REFERENCES machines(id) ON DELETE CASCADE,
		agent_id TEXT NOT NULL,
		hub_id TEXT NOT NULL REFERENCES hubs(id) ON DELETE CASCADE,
		status TEXT NOT NULL,
		terminal_count INTEGER NOT NULL DEFAULT 0,
		last_seen_at TEXT NOT NULL,
		expires_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		PRIMARY KEY (machine_id, agent_id)
	)`); err != nil {
		return fmt.Errorf("create hub_agents: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS hub_agent_policies (
		machine_id TEXT NOT NULL REFERENCES machines(id) ON DELETE CASCADE,
		agent_id TEXT NOT NULL,
		force_offline INTEGER NOT NULL DEFAULT 0,
		reason TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL,
		PRIMARY KEY (machine_id, agent_id)
	)`); err != nil {
		return fmt.Errorf("create hub_agent_policies: %w", err)
	}
	return nil
}

func ensureDeviceAuthSchema(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS device_auth_codes (
		id TEXT PRIMARY KEY,
		device_code_hash TEXT NOT NULL UNIQUE,
		user_code_hash TEXT NOT NULL UNIQUE,
		status TEXT NOT NULL,
		client_name TEXT NOT NULL DEFAULT '',
		verification_uri TEXT NOT NULL DEFAULT '',
		approved_user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
		attempt_count INTEGER NOT NULL DEFAULT 0,
		locked_at TEXT,
		poll_interval_seconds INTEGER NOT NULL DEFAULT 5,
		expires_at TEXT NOT NULL,
		consumed_at TEXT,
		decided_at TEXT,
		reason TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("create device_auth_codes: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE device_auth_codes ADD COLUMN attempt_count INTEGER NOT NULL DEFAULT 0`); err != nil && !strings.Contains(err.Error(), "duplicate column") {
		return fmt.Errorf("add device_auth_codes.attempt_count: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE device_auth_codes ADD COLUMN locked_at TEXT`); err != nil && !strings.Contains(err.Error(), "duplicate column") {
		return fmt.Errorf("add device_auth_codes.locked_at: %w", err)
	}
	return nil
}
