package postgres

import (
	"bytes"
	"context"
	"errors"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
)

// ReconcileBindingKeySet returns the stable revision for an unchanged digest and increments it for a replacement keyset.
func (database *Database) ReconcileBindingKeySet(ctx context.Context, digest []byte, now time.Time) (uint64, error) {
	if len(digest) == 0 {
		return 0, errors.New("binding keyset digest is required")
	}
	tx, err := database.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `INSERT INTO binding_keysets(purpose,keyset_sha256,revision,updated_at) VALUES('daemon-binding',$1,1,$2) ON CONFLICT (purpose) DO NOTHING`, digest, now); err != nil {
		return 0, err
	}
	var currentDigest []byte
	var revision uint64
	if err := tx.QueryRow(ctx, `SELECT keyset_sha256,revision FROM binding_keysets WHERE purpose='daemon-binding' FOR UPDATE`).Scan(&currentDigest, &revision); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, errors.New("binding keyset revision row is missing")
		}
		return 0, err
	}
	if !bytes.Equal(currentDigest, digest) {
		if revision >= math.MaxInt64 {
			return 0, errors.New("binding keyset revision is exhausted")
		}
		revision++
		if _, err := tx.Exec(ctx, `UPDATE binding_keysets SET keyset_sha256=$1,revision=$2,updated_at=$3 WHERE purpose='daemon-binding'`, digest, revision, now); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return revision, nil
}
