package postgres

import (
	"context"
	"crypto/ed25519"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/muxvia/muxvia/cloud/controller/enrollment"
)

// CreateDaemonEnrollment 在同一事务确保开发阶段账号存在并写入一次性 token 摘要。
func (database *Database) CreateDaemonEnrollment(ctx context.Context, accountID, accountName, daemonName string, digest []byte, expiresAt, now time.Time) (string, error) {
	tx, err := database.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `INSERT INTO accounts(account_id,display_name,state,revision,created_at,updated_at) VALUES($1,$2,'active',1,$3,$3) ON CONFLICT (account_id) DO NOTHING`, accountID, accountName, now); err != nil {
		return "", err
	}
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

// ConsumeDaemonEnrollment 原子消费注册 token 并创建唯一 DeviceIdentity。
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
	if _, err := tx.Exec(ctx, `INSERT INTO daemons(daemon_id,account_id,display_name,device_id,device_public_key,device_fingerprint,revoked,revision,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,false,1,$7,$7)`, daemonID, accountID, daemonName, deviceID, []byte(publicKey), fingerprint, now); err != nil {
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

// GetDaemon 读取持久 identity，不查询或缓存实时 Edge 归属。
func (database *Database) GetDaemon(ctx context.Context, daemonID string) (enrollment.Daemon, error) {
	return scanDaemon(database.pool.QueryRow(ctx, daemonSelect+` WHERE daemon.daemon_id=$1`, daemonID))
}

// ListDaemons 返回持久 daemon 列表；在线状态由上层和 Directory 合并。
func (database *Database) ListDaemons(ctx context.Context) ([]enrollment.Daemon, error) {
	rows, err := database.pool.Query(ctx, daemonSelect+` ORDER BY daemon.created_at,daemon.daemon_id`)
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
