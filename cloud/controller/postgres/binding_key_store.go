package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"math"
	"time"

	"github.com/anytty/anytty/cloud/controller/bindingkeys"
	"github.com/anytty/anytty/cloud/ticket"
	"github.com/jackc/pgx/v5"
)

const bindingKeySetPurpose = "daemon-binding"

// ReconcileBindingKeySet serializes the one Controller keyset owner, rejects historical
// digests, and returns the database-owned metadata for the current digest.
func (database *Database) ReconcileBindingKeySet(ctx context.Context, digest []byte, now time.Time, ttl time.Duration) (bindingkeys.Metadata, error) {
	if len(digest) != sha256.Size {
		return bindingkeys.Metadata{}, errors.New("binding keyset SHA-256 digest is required")
	}
	if now.IsZero() || ttl <= 0 || ttl > ticket.MaxKeyBundleTTL {
		return bindingkeys.Metadata{}, errors.New("binding keyset time and TTL up to 24 hours are required")
	}
	now = now.UTC().Truncate(time.Microsecond)
	tx, err := database.pool.Begin(ctx)
	if err != nil {
		return bindingkeys.Metadata{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(748392062)`); err != nil {
		return bindingkeys.Metadata{}, err
	}

	metadata, err := currentBindingKeySet(ctx, tx)
	if errors.Is(err, pgx.ErrNoRows) {
		metadata = bindingkeys.Metadata{KeySetSHA256: append([]byte(nil), digest...), Revision: 1, IssuedAt: now, ExpiresAt: now.Add(ttl)}
		if _, err := tx.Exec(ctx, `INSERT INTO binding_keyset_history(purpose,keyset_sha256,revision,first_seen_at) VALUES($1,$2,$3,$4)`, bindingKeySetPurpose, digest, metadata.Revision, now); err != nil {
			return bindingkeys.Metadata{}, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO binding_keysets(purpose,keyset_sha256,revision,issued_at,expires_at) VALUES($1,$2,$3,$4,$5)`, bindingKeySetPurpose, digest, metadata.Revision, metadata.IssuedAt, metadata.ExpiresAt); err != nil {
			return bindingkeys.Metadata{}, err
		}
	} else if err != nil {
		return bindingkeys.Metadata{}, err
	} else if bytes.Equal(metadata.KeySetSHA256, digest) {
		refreshAt := metadata.IssuedAt.Add(metadata.ExpiresAt.Sub(metadata.IssuedAt) / 2)
		if !now.Before(refreshAt) {
			metadata.IssuedAt = now
			if candidate := now.Add(ttl); candidate.After(metadata.ExpiresAt) {
				metadata.ExpiresAt = candidate
			}
			if _, err := tx.Exec(ctx, `UPDATE binding_keysets SET issued_at=$1,expires_at=$2 WHERE purpose=$3 AND keyset_sha256=$4`, metadata.IssuedAt, metadata.ExpiresAt, bindingKeySetPurpose, digest); err != nil {
				return bindingkeys.Metadata{}, err
			}
		}
	} else {
		var historicalRevision uint64
		err := tx.QueryRow(ctx, `SELECT revision FROM binding_keyset_history WHERE purpose=$1 AND keyset_sha256=$2`, bindingKeySetPurpose, digest).Scan(&historicalRevision)
		if err == nil {
			return bindingkeys.Metadata{}, bindingkeys.ErrKeySetReplay
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return bindingkeys.Metadata{}, err
		}
		if metadata.Revision >= math.MaxInt64 {
			return bindingkeys.Metadata{}, errors.New("binding keyset revision is exhausted")
		}
		metadata = bindingkeys.Metadata{KeySetSHA256: append([]byte(nil), digest...), Revision: metadata.Revision + 1, IssuedAt: now, ExpiresAt: now.Add(ttl)}
		if _, err := tx.Exec(ctx, `INSERT INTO binding_keyset_history(purpose,keyset_sha256,revision,first_seen_at) VALUES($1,$2,$3,$4)`, bindingKeySetPurpose, digest, metadata.Revision, now); err != nil {
			return bindingkeys.Metadata{}, err
		}
		if _, err := tx.Exec(ctx, `UPDATE binding_keysets SET keyset_sha256=$1,revision=$2,issued_at=$3,expires_at=$4 WHERE purpose=$5`, digest, metadata.Revision, metadata.IssuedAt, metadata.ExpiresAt, bindingKeySetPurpose); err != nil {
			return bindingkeys.Metadata{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return bindingkeys.Metadata{}, err
	}
	return metadata, nil
}

func currentBindingKeySet(ctx context.Context, tx pgx.Tx) (bindingkeys.Metadata, error) {
	var metadata bindingkeys.Metadata
	err := tx.QueryRow(ctx, `SELECT keyset_sha256,revision,issued_at,expires_at FROM binding_keysets WHERE purpose=$1 FOR UPDATE`, bindingKeySetPurpose).Scan(
		&metadata.KeySetSHA256, &metadata.Revision, &metadata.IssuedAt, &metadata.ExpiresAt,
	)
	metadata.IssuedAt = metadata.IssuedAt.UTC()
	metadata.ExpiresAt = metadata.ExpiresAt.UTC()
	return metadata, err
}
