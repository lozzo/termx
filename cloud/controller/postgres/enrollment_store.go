package postgres

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/anytty/anytty/cloud/controller/enrollment"
)

// CreateDaemonEnrollment 只为已存在的活动账号写入一次性 token 摘要，不隐式创建残缺账号。
func (database *Database) CreateDaemonEnrollment(ctx context.Context, accountID, _ string, daemonName string, digest []byte, expiresAt, now time.Time) (string, error) {
	tx, err := database.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var state string
	if err := tx.QueryRow(ctx, `SELECT state FROM accounts WHERE account_id=$1`, accountID).Scan(&state); err != nil {
		return "", err
	}
	if state != "active" {
		return "", enrollment.ErrDaemonUnavailable
	}
	if _, err := tx.Exec(ctx, `INSERT INTO daemon_enrollment_tokens(token_digest,account_id,daemon_name,expires_at,created_at) VALUES($1,$2,$3,$4,$5)`, digest, accountID, daemonName, expiresAt, now); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return accountID, nil
}

// ConsumeDaemonEnrollment 原子消费注册 token，并让同一 DeviceIdentity 的后一次 enrollment 接管原 daemon。
// 重复注册保留 daemon_id/created_at，清除 revoked，并更新账号、名称和 revision；相同 device_id 不能更换公钥。
func (database *Database) ConsumeDaemonEnrollment(ctx context.Context, digest []byte, deviceID, fingerprint string, publicKey ed25519.PublicKey, now time.Time) (enrollment.Daemon, error) {
	tx, err := database.pool.Begin(ctx)
	if err != nil {
		return enrollment.Daemon{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var accountID, daemonName string
	if err := tx.QueryRow(ctx, `UPDATE daemon_enrollment_tokens SET consumed_at=$2 WHERE token_digest=$1 AND consumed_at IS NULL AND expires_at>$2 RETURNING account_id::text,daemon_name`, digest, now).Scan(&accountID, &daemonName); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return enrollment.Daemon{}, enrollment.ErrEnrollmentInvalid
		}
		return enrollment.Daemon{}, err
	}
	daemonID := uuid.NewString()
	var resolvedDaemonID string
	err = tx.QueryRow(ctx, `
INSERT INTO daemons(daemon_id,account_id,display_name,device_id,device_public_key,device_fingerprint,revoked,revision,created_at,updated_at)
VALUES($1,$2,$3,$4,$5,$6,false,1,$7,$7)
ON CONFLICT (device_id) DO UPDATE SET
  account_id=EXCLUDED.account_id,
  display_name=EXCLUDED.display_name,
  revoked=false,
  revision=daemons.revision+1,
  updated_at=EXCLUDED.updated_at
WHERE daemons.device_public_key=EXCLUDED.device_public_key
  AND daemons.device_fingerprint=EXCLUDED.device_fingerprint
RETURNING daemon_id::text`, daemonID, accountID, daemonName, deviceID, []byte(publicKey), fingerprint, now).Scan(&resolvedDaemonID)
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.Is(err, pgx.ErrNoRows) || errors.As(err, &postgresError) && postgresError.Code == "23505" {
			return enrollment.Daemon{}, enrollment.ErrDaemonIdentityConflict
		}
		return enrollment.Daemon{}, err
	}
	daemon, err := scanDaemon(tx.QueryRow(ctx, daemonSelect+` WHERE daemon.daemon_id=$1`, resolvedDaemonID))
	if err != nil {
		return enrollment.Daemon{}, err
	}
	if daemon.DeviceID != deviceID || daemon.DeviceFingerprint != fingerprint || !bytes.Equal(daemon.DevicePublicKey, publicKey) {
		return enrollment.Daemon{}, enrollment.ErrDaemonIdentityConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return enrollment.Daemon{}, err
	}
	return daemon, nil
}

// GetDaemon 读取持久 identity，不查询或缓存实时 Edge 归属。
func (database *Database) GetDaemon(ctx context.Context, daemonID string) (enrollment.Daemon, error) {
	return scanDaemon(database.pool.QueryRow(ctx, daemonSelect+` WHERE daemon.daemon_id=$1`, daemonID))
}

// ListDaemons 返回持久 daemon 列表；在线状态由上层和 Directory 合并。
func (database *Database) ListDaemons(ctx context.Context) ([]enrollment.Daemon, error) {
	return database.listDaemons(ctx, daemonSelect+` ORDER BY daemon.created_at,daemon.daemon_id`)
}

// ListDaemonsByAccount 返回账号持久 daemon identity；在线位置仍由 Directory 合并。
func (database *Database) ListDaemonsByAccount(ctx context.Context, accountID string) ([]enrollment.Daemon, error) {
	return database.listDaemons(ctx, daemonSelect+` WHERE daemon.account_id=$1 ORDER BY daemon.created_at,daemon.daemon_id`, accountID)
}

func (database *Database) listDaemons(ctx context.Context, query string, args ...any) ([]enrollment.Daemon, error) {
	rows, err := database.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]enrollment.Daemon, 0)
	for rows.Next() {
		daemon, err := scanDaemon(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, daemon)
	}
	return result, rows.Err()
}

// RevokeDaemon 用账号归属和 revision CAS 持久撤销 identity，并在同一事务写入审计。
func (database *Database) RevokeDaemon(ctx context.Context, accountID, daemonID string, expectedRevision uint64, reason string, now time.Time) (enrollment.Daemon, error) {
	tx, err := database.pool.Begin(ctx)
	if err != nil {
		return enrollment.Daemon{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result, err := tx.Exec(ctx, `UPDATE daemons SET revoked=true,revision=revision+1,updated_at=$1 WHERE daemon_id=$2 AND account_id=$3 AND revision=$4 AND revoked=false`, now, daemonID, accountID, expectedRevision)
	if err != nil {
		return enrollment.Daemon{}, err
	}
	if result.RowsAffected() != 1 {
		return enrollment.Daemon{}, enrollment.ErrDaemonUnavailable
	}
	if err := insertOperatorAudit(ctx, tx, accountID, "daemon.revoke", "daemon", daemonID, reason, "applied", now); err != nil {
		return enrollment.Daemon{}, err
	}
	daemon, err := scanDaemon(tx.QueryRow(ctx, daemonSelect+` WHERE daemon.daemon_id=$1`, daemonID))
	if err != nil {
		return enrollment.Daemon{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return enrollment.Daemon{}, err
	}
	return daemon, nil
}

const daemonSelect = `SELECT daemon.daemon_id::text,daemon.account_id::text,account.display_name,daemon.display_name,daemon.device_id,daemon.device_public_key,daemon.device_fingerprint,daemon.revoked,daemon.revision,daemon.created_at,daemon.updated_at FROM daemons daemon JOIN accounts account ON account.account_id=daemon.account_id`

func scanDaemon(row rowScanner) (enrollment.Daemon, error) {
	var daemon enrollment.Daemon
	var publicKey []byte
	if err := row.Scan(&daemon.ID, &daemon.AccountID, &daemon.AccountName, &daemon.DisplayName, &daemon.DeviceID, &publicKey, &daemon.DeviceFingerprint, &daemon.Revoked, &daemon.Revision, &daemon.CreatedAt, &daemon.UpdatedAt); err != nil {
		return enrollment.Daemon{}, err
	}
	daemon.DevicePublicKey = append(ed25519.PublicKey(nil), publicKey...)
	return daemon, nil
}
