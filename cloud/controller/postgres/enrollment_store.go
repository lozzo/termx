package postgres

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"time"

	"github.com/anytty/anytty/cloud/controller/enrollment"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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

// GetDaemonEnrollmentAccount 在消费前读取有效 token 的账号，用于完成外部准入预检查。
func (database *Database) GetDaemonEnrollmentAccount(ctx context.Context, digest []byte, now time.Time) (string, error) {
	var accountID string
	if err := database.pool.QueryRow(ctx, `SELECT account_id::text FROM daemon_enrollment_tokens WHERE token_digest=$1 AND consumed_at IS NULL AND expires_at>$2`, digest, now).Scan(&accountID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", enrollment.ErrEnrollmentInvalid
		}
		return "", err
	}
	return accountID, nil
}

// ConsumeDaemonEnrollment 原子消费注册 token，并为当前未注册的 DeviceIdentity 创建新 daemon。
// DELETED 行是旧 daemon_id 的永久墓碑，不会被重新激活。
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
	var daemonLimit int64
	if err := tx.QueryRow(ctx, `SELECT p.cloud_daemon_limit
FROM subscriptions s JOIN plans p ON p.plan_id=s.plan_id AND p.version=s.plan_version
WHERE s.account_id=$1 AND s.state IN ('active','cancel_at_period_end','past_due') AND s.period_start<=$2 AND s.period_end>$2
FOR UPDATE OF s`, accountID, now).Scan(&daemonLimit); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return enrollment.Daemon{}, enrollment.ErrDaemonUnavailable
		}
		return enrollment.Daemon{}, err
	}
	var daemonCount int64
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM daemons WHERE account_id=$1 AND state<>'deleted'`, accountID).Scan(&daemonCount); err != nil {
		return enrollment.Daemon{}, err
	}
	if daemonLimit <= 0 || daemonCount >= daemonLimit {
		return enrollment.Daemon{}, enrollment.ErrDaemonLimitExhausted
	}
	daemonID := uuid.NewString()
	err = tx.QueryRow(ctx, `INSERT INTO daemons(daemon_id,account_id,display_name,device_id,device_public_key,device_fingerprint,state,state_revision,created_at,updated_at)
VALUES($1,$2,$3,$4,$5,$6,'active',1,$7,$7) RETURNING daemon_id::text`, daemonID, accountID, daemonName, deviceID, []byte(publicKey), fingerprint, now).Scan(&daemonID)
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == "23505" {
			return enrollment.Daemon{}, enrollment.ErrDaemonIdentityConflict
		}
		return enrollment.Daemon{}, err
	}
	daemon, err := scanDaemon(tx.QueryRow(ctx, daemonSelect+` WHERE daemon.daemon_id=$1`, daemonID))
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

// ListDaemons 返回未删除的持久 daemon 列表；在线状态由上层和 Directory 合并。
func (database *Database) ListDaemons(ctx context.Context) ([]enrollment.Daemon, error) {
	return database.listDaemons(ctx, daemonSelect+` WHERE daemon.state<>'deleted' ORDER BY daemon.created_at,daemon.daemon_id`)
}

// ListDaemonsByAccount 返回账号持久 daemon identity；在线位置仍由 Directory 合并。
func (database *Database) ListDaemonsByAccount(ctx context.Context, accountID string) ([]enrollment.Daemon, error) {
	return database.listDaemons(ctx, daemonSelect+` WHERE daemon.account_id=$1 AND daemon.state<>'deleted' ORDER BY daemon.created_at,daemon.daemon_id`, accountID)
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

// ChangeDaemonState 用账号归属和 state revision CAS 提交生命周期变化。
func (database *Database) ChangeDaemonState(ctx context.Context, accountID, daemonID string, target cloudv1.DaemonState, expectedRevision uint64, reason string, now time.Time) (enrollment.Daemon, error) {
	tx, err := database.pool.Begin(ctx)
	if err != nil {
		return enrollment.Daemon{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current, err := scanDaemon(tx.QueryRow(ctx, daemonSelect+` WHERE daemon.daemon_id=$1 AND daemon.account_id=$2 FOR UPDATE OF daemon`, daemonID, accountID))
	if err != nil || current.StateRevision != expectedRevision || !validDaemonStateTransition(current.State, target) {
		return enrollment.Daemon{}, enrollment.ErrDaemonUnavailable
	}
	state, err := daemonStateName(target)
	if err != nil {
		return enrollment.Daemon{}, enrollment.ErrDaemonUnavailable
	}
	if _, err := tx.Exec(ctx, `UPDATE daemons SET state=$1,state_revision=state_revision+1,updated_at=$2 WHERE daemon_id=$3`, state, now, daemonID); err != nil {
		return enrollment.Daemon{}, err
	}
	if err := insertOperatorAudit(ctx, tx, accountID, "daemon.state.change", "daemon", daemonID, reason, "applied", now); err != nil {
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

// ListDaemonEdgeMeasurements 返回 daemon 最近一次对各 Edge 的汇总探测。
func (database *Database) ListDaemonEdgeMeasurements(ctx context.Context, daemonID string) ([]enrollment.EdgeMeasurement, error) {
	rows, err := database.pool.Query(ctx, `SELECT edge_id::text,reachable,connect_latency_ms,connection_failure_rate,sample_count,measured_at FROM daemon_edge_measurements WHERE daemon_id=$1 ORDER BY edge_id`, daemonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]enrollment.EdgeMeasurement, 0)
	for rows.Next() {
		var value enrollment.EdgeMeasurement
		if err := rows.Scan(&value.EdgeID, &value.Reachable, &value.ConnectLatencyMS, &value.ConnectionFailureRate, &value.SampleCount, &value.MeasuredAt); err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

// UpsertDaemonEdgeMeasurements 只替换每个 Edge 的最新汇总，不修改 daemon 生命周期 revision。
func (database *Database) UpsertDaemonEdgeMeasurements(ctx context.Context, daemonID string, measurements []enrollment.EdgeMeasurement) error {
	tx, err := database.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, value := range measurements {
		if _, err := tx.Exec(ctx, `INSERT INTO daemon_edge_measurements(daemon_id,edge_id,reachable,connect_latency_ms,connection_failure_rate,sample_count,measured_at)
VALUES($1,$2,$3,$4,$5,$6,$7)
ON CONFLICT(daemon_id,edge_id) DO UPDATE SET reachable=excluded.reachable,connect_latency_ms=excluded.connect_latency_ms,connection_failure_rate=excluded.connection_failure_rate,sample_count=excluded.sample_count,measured_at=excluded.measured_at`, daemonID, value.EdgeID, value.Reachable, value.ConnectLatencyMS, value.ConnectionFailureRate, value.SampleCount, value.MeasuredAt); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// ChangeDaemonEdgePreference 用 owner 和 preference revision CAS 更新软偏好；空 Edge 表示恢复自动选择。
func (database *Database) ChangeDaemonEdgePreference(ctx context.Context, accountID, daemonID, preferredEdgeID string, expectedRevision uint64, now time.Time) (enrollment.Daemon, error) {
	tx, err := database.pool.Begin(ctx)
	if err != nil {
		return enrollment.Daemon{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current, err := scanDaemon(tx.QueryRow(ctx, daemonSelect+` WHERE daemon.daemon_id=$1 AND daemon.account_id=$2 FOR UPDATE OF daemon`, daemonID, accountID))
	if err != nil || current.State == cloudv1.DaemonState_DAEMON_STATE_DELETED || current.EdgePreferenceRevision != expectedRevision {
		return enrollment.Daemon{}, enrollment.ErrDaemonUnavailable
	}
	var preferred any
	if preferredEdgeID != "" {
		preferred = preferredEdgeID
	}
	if _, err := tx.Exec(ctx, `UPDATE daemons SET preferred_edge_id=$1,edge_preference_revision=edge_preference_revision+1,edge_preference_updated_at=$2 WHERE daemon_id=$3`, preferred, now, daemonID); err != nil {
		return enrollment.Daemon{}, err
	}
	if err := insertOperatorAudit(ctx, tx, accountID, "daemon.edge.preference", "daemon", daemonID, "user edge preference", "applied", now); err != nil {
		return enrollment.Daemon{}, err
	}
	updated, err := scanDaemon(tx.QueryRow(ctx, daemonSelect+` WHERE daemon.daemon_id=$1`, daemonID))
	if err != nil {
		return enrollment.Daemon{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return enrollment.Daemon{}, err
	}
	return updated, nil
}

const daemonSelect = `SELECT daemon.daemon_id::text,daemon.account_id::text,account.display_name,daemon.display_name,daemon.device_id,daemon.device_public_key,daemon.device_fingerprint,daemon.state,daemon.state_revision,daemon.created_at,daemon.updated_at,COALESCE(daemon.preferred_edge_id::text,''),daemon.edge_preference_revision,daemon.edge_preference_updated_at FROM daemons daemon JOIN accounts account ON account.account_id=daemon.account_id`

func scanDaemon(row rowScanner) (enrollment.Daemon, error) {
	var daemon enrollment.Daemon
	var publicKey []byte
	var state string
	if err := row.Scan(&daemon.ID, &daemon.AccountID, &daemon.AccountName, &daemon.DisplayName, &daemon.DeviceID, &publicKey, &daemon.DeviceFingerprint, &state, &daemon.StateRevision, &daemon.CreatedAt, &daemon.UpdatedAt, &daemon.PreferredEdgeID, &daemon.EdgePreferenceRevision, &daemon.EdgePreferenceUpdatedAt); err != nil {
		return enrollment.Daemon{}, err
	}
	daemon.State = parseDaemonState(state)
	if daemon.State == cloudv1.DaemonState_DAEMON_STATE_UNSPECIFIED {
		return enrollment.Daemon{}, errors.New("daemon state is invalid")
	}
	daemon.DevicePublicKey = append(ed25519.PublicKey(nil), publicKey...)
	return daemon, nil
}

func daemonStateName(state cloudv1.DaemonState) (string, error) {
	switch state {
	case cloudv1.DaemonState_DAEMON_STATE_ACTIVE:
		return "active", nil
	case cloudv1.DaemonState_DAEMON_STATE_BLOCKED:
		return "blocked", nil
	case cloudv1.DaemonState_DAEMON_STATE_DELETED:
		return "deleted", nil
	default:
		return "", errors.New("daemon state is invalid")
	}
}

func parseDaemonState(state string) cloudv1.DaemonState {
	switch state {
	case "active":
		return cloudv1.DaemonState_DAEMON_STATE_ACTIVE
	case "blocked":
		return cloudv1.DaemonState_DAEMON_STATE_BLOCKED
	case "deleted":
		return cloudv1.DaemonState_DAEMON_STATE_DELETED
	default:
		return cloudv1.DaemonState_DAEMON_STATE_UNSPECIFIED
	}
}

func validDaemonStateTransition(current, target cloudv1.DaemonState) bool {
	return current == cloudv1.DaemonState_DAEMON_STATE_ACTIVE && (target == cloudv1.DaemonState_DAEMON_STATE_BLOCKED || target == cloudv1.DaemonState_DAEMON_STATE_DELETED) ||
		current == cloudv1.DaemonState_DAEMON_STATE_BLOCKED && (target == cloudv1.DaemonState_DAEMON_STATE_ACTIVE || target == cloudv1.DaemonState_DAEMON_STATE_DELETED)
}
