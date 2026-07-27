package postgres

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type migration struct {
	version  int
	checksum string
	sql      string
}

// Migrate 在显式部署步骤中串行应用缺失 migration；Controller 正常启动不会调用它。
func (database *Database) Migrate(ctx context.Context) error {
	migrations, err := loadMigrations()
	if err != nil {
		return err
	}
	connection, err := database.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer connection.Release()
	if _, err := connection.Exec(ctx, `SELECT pg_advisory_lock(748392061)`); err != nil {
		return fmt.Errorf("lock Cloud migration: %w", err)
	}
	defer func() { _, _ = connection.Exec(context.Background(), `SELECT pg_advisory_unlock(748392061)`) }()
	if _, err := connection.Exec(ctx, `CREATE TABLE IF NOT EXISTS anytty_schema_migrations (version bigint PRIMARY KEY, checksum text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return fmt.Errorf("create migration ledger: %w", err)
	}
	for _, item := range migrations {
		var checksum string
		err := connection.QueryRow(ctx, `SELECT checksum FROM anytty_schema_migrations WHERE version=$1`, item.version).Scan(&checksum)
		if err == nil {
			if checksum != item.checksum {
				return fmt.Errorf("migration %d checksum mismatch", item.version)
			}
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		tx, err := connection.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, item.sql); err == nil {
			_, err = tx.Exec(ctx, `INSERT INTO anytty_schema_migrations(version, checksum) VALUES($1,$2)`, item.version, item.checksum)
		}
		if err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("apply migration %d: %w", item.version, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %d: %w", item.version, err)
		}
	}
	return nil
}

// VerifySchema 确认数据库已由独立 migration step 升到当前版本且 checksum 未漂移。
func (database *Database) VerifySchema(ctx context.Context) error {
	migrations, err := loadMigrations()
	if err != nil {
		return err
	}
	for _, item := range migrations {
		var checksum string
		if err := database.pool.QueryRow(ctx, `SELECT checksum FROM anytty_schema_migrations WHERE version=$1`, item.version).Scan(&checksum); err != nil {
			return fmt.Errorf("verify migration %d: %w", item.version, err)
		}
		if checksum != item.checksum {
			return fmt.Errorf("migration %d checksum mismatch", item.version)
		}
	}
	return nil
}

func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return nil, err
	}
	result := make([]migration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		prefix, _, ok := strings.Cut(entry.Name(), "_")
		if !ok {
			return nil, fmt.Errorf("migration filename %q has no numeric prefix", entry.Name())
		}
		version, err := strconv.Atoi(prefix)
		if err != nil || version <= 0 {
			return nil, fmt.Errorf("migration filename %q has invalid version", entry.Name())
		}
		payload, err := migrationFiles.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return nil, err
		}
		digest := sha256.Sum256(payload)
		result = append(result, migration{version: version, checksum: hex.EncodeToString(digest[:]), sql: string(payload)})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].version < result[j].version })
	return result, nil
}
