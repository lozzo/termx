// Package sqlite 实现 Control Plane 领域 store 的 development SQLite adapter。
// 该包只负责事务和编码，不拥有 Hub generation、assignment 或 fencing 业务语义。
package sqlite

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/lozzow/termx/private/cloud/control-plane/hubregistry"
	"github.com/lozzow/termx/proto/cloudpb"
	_ "modernc.org/sqlite"
)

// Store 是 development Controller 的单节点 SQLite 持久 adapter。
type Store struct{ db *sql.DB }

// Open 打开数据库并创建 HUB002 所需最小 schema。
func Open(path string) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("Control Plane SQLite path is required")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	store := &Store{db: db}
	if err := store.bootstrap(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

// Close 关闭 SQLite 连接。
func (store *Store) Close() error { return store.db.Close() }

func (store *Store) bootstrap() error {
	_, err := store.db.Exec(`
PRAGMA foreign_keys = ON;
PRAGMA journal_mode = WAL;
CREATE TABLE IF NOT EXISTS hub_deployments(
  hub_id TEXT PRIMARY KEY, deployment_id TEXT NOT NULL, credential_fingerprint TEXT NOT NULL,
  control_public_key BLOB NOT NULL, region TEXT NOT NULL, public_label TEXT NOT NULL,
  relay_id TEXT NOT NULL, relay_credential_fingerprint TEXT NOT NULL,
  enabled INTEGER NOT NULL, last_control_generation INTEGER NOT NULL, updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS hub_assignments(
  daemon_device_id TEXT PRIMARY KEY, account_id TEXT NOT NULL, hub_id TEXT NOT NULL,
  assignment_epoch INTEGER NOT NULL, not_before_unix_millis INTEGER NOT NULL,
  expires_at_unix_millis INTEGER NOT NULL, fence_satisfied INTEGER NOT NULL,
  previous_hub_id TEXT NOT NULL, previous_epoch INTEGER NOT NULL, updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS hub_projection_heads(
  hub_id TEXT PRIMARY KEY, projection_revision INTEGER NOT NULL, digest BLOB NOT NULL,
  published_at TEXT NOT NULL, acknowledged_at TEXT
);
CREATE TABLE IF NOT EXISTS control_receive_cursors(
  hub_id TEXT NOT NULL, control_generation INTEGER NOT NULL, sender_role INTEGER NOT NULL,
  accepted_sequence INTEGER NOT NULL, accepted_digest BLOB NOT NULL, updated_at TEXT NOT NULL,
  PRIMARY KEY(hub_id, control_generation, sender_role)
);
CREATE TABLE IF NOT EXISTS cloud_device_ownership(
  device_id TEXT PRIMARY KEY, account_id TEXT NOT NULL, device_kind INTEGER NOT NULL,
  auth_epoch INTEGER NOT NULL DEFAULT 1, revoked INTEGER NOT NULL DEFAULT 0,
  public_key BLOB NOT NULL DEFAULT X'', updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS hub_topology_heads(
  hub_id TEXT PRIMARY KEY, control_generation INTEGER NOT NULL, topology_revision INTEGER NOT NULL,
  topology_digest BLOB NOT NULL, observed_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS presence_topology(
  daemon_device_id TEXT PRIMARY KEY, account_id TEXT NOT NULL, hub_id TEXT NOT NULL,
  control_generation INTEGER NOT NULL, projection BLOB NOT NULL, updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS managed_peer_topology(
  daemon_device_id TEXT NOT NULL, managed_session_id TEXT NOT NULL, session_incarnation INTEGER NOT NULL,
  account_id TEXT NOT NULL, hub_id TEXT NOT NULL, control_generation INTEGER NOT NULL,
  projection BLOB NOT NULL, updated_at TEXT NOT NULL,
  PRIMARY KEY(daemon_device_id, managed_session_id, session_incarnation)
);
CREATE TABLE IF NOT EXISTS terminal_access_topology(
  daemon_device_id TEXT PRIMARY KEY, account_id TEXT NOT NULL, hub_id TEXT NOT NULL,
  control_generation INTEGER NOT NULL, access_projection_revision INTEGER NOT NULL,
  freshness INTEGER NOT NULL, inventory BLOB NOT NULL, updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS commerce_accounts(
  account_id TEXT PRIMARY KEY, email TEXT NOT NULL UNIQUE, projection BLOB NOT NULL,
  password_hash BLOB NOT NULL, auth_revision INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS commerce_sessions(
  session_id TEXT PRIMARY KEY, account_id TEXT NOT NULL, access_hash BLOB NOT NULL UNIQUE,
  refresh_hash BLOB NOT NULL UNIQUE, access_expires_at INTEGER NOT NULL,
  refresh_expires_at INTEGER NOT NULL, revision INTEGER NOT NULL, revoked INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS commerce_sessions_account ON commerce_sessions(account_id);
CREATE TABLE IF NOT EXISTS commerce_orders(
  order_id TEXT PRIMARY KEY, account_id TEXT NOT NULL, revision INTEGER NOT NULL,
  projection BLOB NOT NULL
);
CREATE INDEX IF NOT EXISTS commerce_orders_account ON commerce_orders(account_id);
CREATE TABLE IF NOT EXISTS commerce_payment_attempts(
  payment_attempt_id TEXT PRIMARY KEY, order_id TEXT NOT NULL, account_id TEXT NOT NULL,
  revision INTEGER NOT NULL, projection BLOB NOT NULL
);
CREATE INDEX IF NOT EXISTS commerce_payment_attempts_account ON commerce_payment_attempts(account_id);
CREATE TABLE IF NOT EXISTS commerce_payment_events(
  provider_event_id TEXT PRIMARY KEY, digest BLOB NOT NULL, event BLOB NOT NULL,
  state INTEGER NOT NULL, result BLOB
);
CREATE TABLE IF NOT EXISTS commerce_subscriptions(
  account_id TEXT PRIMARY KEY, revision INTEGER NOT NULL, projection BLOB NOT NULL
);
CREATE TABLE IF NOT EXISTS commerce_entitlements(
  account_id TEXT PRIMARY KEY, projection BLOB NOT NULL
);
CREATE TABLE IF NOT EXISTS relay_quota_periods(
  account_id TEXT NOT NULL, period_start_unix_millis INTEGER NOT NULL,
  period_end_unix_millis INTEGER NOT NULL, limit_bytes INTEGER NOT NULL,
  used_bytes INTEGER NOT NULL, revision INTEGER NOT NULL,
  PRIMARY KEY(account_id,period_start_unix_millis)
);
CREATE TABLE IF NOT EXISTS relay_lease_reservations(
  lease_id TEXT PRIMARY KEY, account_id TEXT NOT NULL, managed_session_id TEXT NOT NULL,
  client_device_id TEXT NOT NULL, target_device_id TEXT NOT NULL, region TEXT NOT NULL,
  period_start_unix_millis INTEGER NOT NULL, period_end_unix_millis INTEGER NOT NULL,
  reserved_bytes INTEGER NOT NULL, used_bytes INTEGER NOT NULL, state INTEGER NOT NULL,
  expires_at_unix_millis INTEGER NOT NULL, release_after_unix_millis INTEGER NOT NULL,
  revision INTEGER NOT NULL, projection BLOB NOT NULL
);
CREATE INDEX IF NOT EXISTS relay_reservations_account_period ON relay_lease_reservations(account_id,period_start_unix_millis,state);
CREATE TABLE IF NOT EXISTS commerce_audit(
  audit_id TEXT PRIMARY KEY, account_id TEXT NOT NULL, occurred_at INTEGER NOT NULL,
  projection BLOB NOT NULL
);
CREATE TABLE IF NOT EXISTS management_commands(
  command_id TEXT PRIMARY KEY, account_id TEXT NOT NULL, idempotency_key TEXT NOT NULL,
  command_kind INTEGER NOT NULL, delivery_state INTEGER NOT NULL, execution_state INTEGER NOT NULL,
  expires_at INTEGER NOT NULL, updated_at INTEGER NOT NULL, version INTEGER NOT NULL, projection BLOB NOT NULL,
  UNIQUE(account_id,idempotency_key)
);
CREATE INDEX IF NOT EXISTS management_commands_open ON management_commands(execution_state,updated_at);
CREATE TABLE IF NOT EXISTS management_command_children(
  child_command_id TEXT PRIMARY KEY, parent_command_id TEXT NOT NULL, target_hub_id TEXT NOT NULL,
  FOREIGN KEY(parent_command_id) REFERENCES management_commands(command_id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS management_command_results(
  child_command_id TEXT NOT NULL, result_kind TEXT NOT NULL, digest BLOB NOT NULL,
  result BLOB NOT NULL, created_at INTEGER NOT NULL,
  PRIMARY KEY(child_command_id,result_kind),
  FOREIGN KEY(child_command_id) REFERENCES management_command_children(child_command_id) ON DELETE CASCADE
);`)
	if err != nil {
		return err
	}
	if err := store.ensureColumn("cloud_device_ownership", "auth_epoch", "INTEGER NOT NULL DEFAULT 1"); err != nil {
		return err
	}
	if err := store.ensureColumn("cloud_device_ownership", "revoked", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	return store.ensureColumn("cloud_device_ownership", "public_key", "BLOB NOT NULL DEFAULT X''")
}

func (store *Store) ensureColumn(table, column, definition string) error {
	rows, err := store.db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull, primaryKey int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return err
		}
		if name == column {
			found = true
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if found {
		return nil
	}
	_, err = store.db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + column + ` ` + definition)
	return err
}

// PutDeployment upsert deployment 配置但保留已经签发的 generation。
func (store *Store) PutDeployment(ctx context.Context, value hubregistry.Deployment) error {
	metadata := value.Metadata
	_, err := store.db.ExecContext(ctx, `INSERT INTO hub_deployments(
hub_id,deployment_id,credential_fingerprint,control_public_key,region,public_label,relay_id,relay_credential_fingerprint,enabled,last_control_generation,updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(hub_id) DO UPDATE SET
deployment_id=excluded.deployment_id,credential_fingerprint=excluded.credential_fingerprint,control_public_key=excluded.control_public_key,
region=excluded.region,public_label=excluded.public_label,relay_id=excluded.relay_id,relay_credential_fingerprint=excluded.relay_credential_fingerprint,
enabled=excluded.enabled,updated_at=excluded.updated_at`, metadata.GetHubId(), metadata.GetEdgeDeploymentId(), metadata.GetHubControlIdentityFingerprint(), []byte(value.ControlPublicKey), metadata.GetRegion(), metadata.GetPublicLabel(), metadata.GetRelayId(), metadata.GetRelayControlIdentityFingerprint(), boolInt(value.Enabled), value.ControlGeneration, value.UpdatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

// Deployment 读取一个 Hub deployment 深拷贝。
func (store *Store) Deployment(ctx context.Context, hubID string) (hubregistry.Deployment, error) {
	var value hubregistry.Deployment
	var metadata cloudpb.EdgeDeploymentMetadata
	var publicKey []byte
	var enabled int
	var updated string
	err := store.db.QueryRowContext(ctx, `SELECT deployment_id,credential_fingerprint,control_public_key,region,public_label,relay_id,relay_credential_fingerprint,enabled,last_control_generation,updated_at FROM hub_deployments WHERE hub_id=?`, hubID).Scan(&metadata.EdgeDeploymentId, &metadata.HubControlIdentityFingerprint, &publicKey, &metadata.Region, &metadata.PublicLabel, &metadata.RelayId, &metadata.RelayControlIdentityFingerprint, &enabled, &value.ControlGeneration, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return hubregistry.Deployment{}, hubregistry.ErrDeploymentNotFound
	}
	if err != nil {
		return hubregistry.Deployment{}, err
	}
	metadata.HubId = hubID
	value.Metadata, value.ControlPublicKey, value.Enabled = &metadata, append(ed25519.PublicKey(nil), publicKey...), enabled != 0
	value.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return value, nil
}

// AdvanceControlGeneration 事务校验 deployment identity 并签发唯一递增 generation。
func (store *Store) AdvanceControlGeneration(ctx context.Context, hubID, deploymentID, fingerprint string, now time.Time) (hubregistry.Deployment, error) {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return hubregistry.Deployment{}, err
	}
	defer tx.Rollback()
	var storedDeployment, storedFingerprint string
	var enabled int
	var generation uint64
	if err = tx.QueryRowContext(ctx, `SELECT deployment_id,credential_fingerprint,enabled,last_control_generation FROM hub_deployments WHERE hub_id=?`, hubID).Scan(&storedDeployment, &storedFingerprint, &enabled, &generation); errors.Is(err, sql.ErrNoRows) {
		return hubregistry.Deployment{}, hubregistry.ErrDeploymentNotFound
	} else if err != nil {
		return hubregistry.Deployment{}, err
	}
	if enabled == 0 {
		return hubregistry.Deployment{}, hubregistry.ErrDeploymentNotFound
	}
	if storedDeployment != deploymentID || storedFingerprint != fingerprint {
		return hubregistry.Deployment{}, hubregistry.ErrDeploymentIdentity
	}
	generation++
	if _, err = tx.ExecContext(ctx, `UPDATE hub_deployments SET last_control_generation=?,updated_at=? WHERE hub_id=? AND last_control_generation=?`, generation, now.Format(time.RFC3339Nano), hubID, generation-1); err != nil {
		return hubregistry.Deployment{}, err
	}
	if err = tx.Commit(); err != nil {
		return hubregistry.Deployment{}, err
	}
	return store.Deployment(ctx, hubID)
}

// ControlGenerationCurrent 查询 handler generation 是否仍为 registry 当前值。
func (store *Store) ControlGenerationCurrent(ctx context.Context, hubID string, generation uint64) (bool, error) {
	var current uint64
	var enabled int
	err := store.db.QueryRowContext(ctx, `SELECT last_control_generation,enabled FROM hub_deployments WHERE hub_id=?`, hubID).Scan(&current, &enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return false, hubregistry.ErrDeploymentNotFound
	}
	return err == nil && enabled != 0 && current == generation, err
}

// MoveAssignment 在单事务中执行 epoch 与跨 Hub fence/expiry 约束。
func (store *Store) MoveAssignment(ctx context.Context, next *cloudpb.HubAssignment, now time.Time) (hubregistry.Assignment, error) {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return hubregistry.Assignment{}, err
	}
	defer tx.Rollback()
	current, found, err := assignmentTx(ctx, tx, next.GetDaemonDeviceId())
	if err != nil {
		return hubregistry.Assignment{}, err
	}
	previousHub, previousEpoch := "", uint64(0)
	if found {
		currentValue := current.Value
		if next.GetAccountId() != currentValue.GetAccountId() || next.GetAssignmentEpoch() != currentValue.GetAssignmentEpoch()+1 {
			return hubregistry.Assignment{}, hubregistry.ErrAssignmentConflict
		}
		previousHub, previousEpoch = currentValue.GetHubId(), currentValue.GetAssignmentEpoch()
		if next.GetHubId() != currentValue.GetHubId() && !current.FenceSatisfied && now.UnixMilli() < currentValue.GetExpiresAtUnixMillis() {
			return hubregistry.Assignment{}, hubregistry.ErrAssignmentFenceRequired
		}
	} else if next.GetAssignmentEpoch() != 1 {
		return hubregistry.Assignment{}, hubregistry.ErrAssignmentConflict
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO hub_assignments(daemon_device_id,account_id,hub_id,assignment_epoch,not_before_unix_millis,expires_at_unix_millis,fence_satisfied,previous_hub_id,previous_epoch,updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(daemon_device_id) DO UPDATE SET account_id=excluded.account_id,hub_id=excluded.hub_id,assignment_epoch=excluded.assignment_epoch,not_before_unix_millis=excluded.not_before_unix_millis,expires_at_unix_millis=excluded.expires_at_unix_millis,fence_satisfied=excluded.fence_satisfied,previous_hub_id=excluded.previous_hub_id,previous_epoch=excluded.previous_epoch,updated_at=excluded.updated_at`, next.GetDaemonDeviceId(), next.GetAccountId(), next.GetHubId(), next.GetAssignmentEpoch(), next.GetNotBeforeUnixMillis(), next.GetExpiresAtUnixMillis(), 0, previousHub, previousEpoch, now.Format(time.RFC3339Nano))
	if err != nil {
		return hubregistry.Assignment{}, err
	}
	if err = tx.Commit(); err != nil {
		return hubregistry.Assignment{}, err
	}
	return store.Assignment(ctx, next.GetDaemonDeviceId())
}

// FenceAssignment 原子标记当前精确 source epoch 已完成 fence。
func (store *Store) FenceAssignment(ctx context.Context, daemonDeviceID, sourceHubID string, sourceEpoch uint64, now time.Time) (hubregistry.Assignment, error) {
	result, err := store.db.ExecContext(ctx, `UPDATE hub_assignments SET fence_satisfied=1,updated_at=? WHERE daemon_device_id=? AND hub_id=? AND assignment_epoch=?`, now.Format(time.RFC3339Nano), daemonDeviceID, sourceHubID, sourceEpoch)
	if err != nil {
		return hubregistry.Assignment{}, err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return hubregistry.Assignment{}, hubregistry.ErrAssignmentConflict
	}
	return store.Assignment(ctx, daemonDeviceID)
}

// Assignment 返回一个 daemon assignment。
func (store *Store) Assignment(ctx context.Context, daemonDeviceID string) (hubregistry.Assignment, error) {
	row := store.db.QueryRowContext(ctx, `SELECT account_id,hub_id,assignment_epoch,not_before_unix_millis,expires_at_unix_millis,fence_satisfied,previous_hub_id,previous_epoch,updated_at FROM hub_assignments WHERE daemon_device_id=?`, daemonDeviceID)
	return scanAssignment(row, daemonDeviceID)
}

// AssignmentsForHub 返回指定 Hub 当前未过期 assignment。
func (store *Store) AssignmentsForHub(ctx context.Context, hubID string, now time.Time) ([]hubregistry.Assignment, error) {
	rows, err := store.db.QueryContext(ctx, `SELECT daemon_device_id,account_id,hub_id,assignment_epoch,not_before_unix_millis,expires_at_unix_millis,fence_satisfied,previous_hub_id,previous_epoch,updated_at FROM hub_assignments WHERE hub_id=? AND not_before_unix_millis<=? AND expires_at_unix_millis>? ORDER BY daemon_device_id`, hubID, now.UnixMilli(), now.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []hubregistry.Assignment
	for rows.Next() {
		var daemonID string
		var value hubregistry.Assignment
		var assignment cloudpb.HubAssignment
		var fenced int
		var updated string
		if err := rows.Scan(&daemonID, &assignment.AccountId, &assignment.HubId, &assignment.AssignmentEpoch, &assignment.NotBeforeUnixMillis, &assignment.ExpiresAtUnixMillis, &fenced, &value.PreviousHubID, &value.PreviousEpoch, &updated); err != nil {
			return nil, err
		}
		assignment.DaemonDeviceId = daemonID
		value.Value, value.FenceSatisfied = &assignment, fenced != 0
		value.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
		result = append(result, value)
	}
	return result, rows.Err()
}

// ControlCursor 返回 Hub runtime 上行方向当前已持久接受的连续 sequence 与最后 digest。
func (store *Store) ControlCursor(ctx context.Context, hubID string, generation uint64, senderRole cloudpb.ControlSenderRole) (uint64, []byte, error) {
	var sequence uint64
	var digest []byte
	err := store.db.QueryRowContext(ctx, `SELECT accepted_sequence,accepted_digest FROM control_receive_cursors WHERE hub_id=? AND control_generation=? AND sender_role=?`, hubID, generation, senderRole).Scan(&sequence, &digest)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil, nil
	}
	return sequence, append([]byte(nil), digest...), err
}

// PutControlCursor 原子保存一个 generation/sender role 的连续接收位置。
func (store *Store) PutControlCursor(ctx context.Context, hubID string, generation uint64, senderRole cloudpb.ControlSenderRole, sequence uint64, digest []byte, now time.Time) error {
	_, err := store.db.ExecContext(ctx, `INSERT INTO control_receive_cursors(hub_id,control_generation,sender_role,accepted_sequence,accepted_digest,updated_at) VALUES(?,?,?,?,?,?) ON CONFLICT(hub_id,control_generation,sender_role) DO UPDATE SET accepted_sequence=excluded.accepted_sequence,accepted_digest=excluded.accepted_digest,updated_at=excluded.updated_at`, hubID, generation, senderRole, sequence, append([]byte(nil), digest...), now.UTC().Format(time.RFC3339Nano))
	return err
}

// AllocateProjectionRevision 为单个 Hub 持久分配严格递增 revision。
// Controller 随后必须生成 full projection；崩溃造成的 revision gap 由 full sync 明确跨越。
func (store *Store) AllocateProjectionRevision(ctx context.Context, hubID string, now time.Time) (uint64, error) {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var current uint64
	err = tx.QueryRowContext(ctx, `SELECT projection_revision FROM hub_projection_heads WHERE hub_id=?`, hubID).Scan(&current)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	next := current + 1
	_, err = tx.ExecContext(ctx, `INSERT INTO hub_projection_heads(hub_id,projection_revision,digest,published_at,acknowledged_at) VALUES(?,?,?,?,NULL) ON CONFLICT(hub_id) DO UPDATE SET projection_revision=excluded.projection_revision,digest=excluded.digest,published_at=excluded.published_at,acknowledged_at=NULL`, hubID, next, []byte{}, now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return next, nil
}

// SetProjectionDigest 保存已发布 revision 的 digest；旧 revision 不得覆盖新 head。
func (store *Store) SetProjectionDigest(ctx context.Context, hubID string, revision uint64, digest []byte) error {
	result, err := store.db.ExecContext(ctx, `UPDATE hub_projection_heads SET digest=? WHERE hub_id=? AND projection_revision=?`, append([]byte(nil), digest...), hubID, revision)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return fmt.Errorf("projection head changed while publishing")
	}
	return nil
}

type rowScanner interface{ Scan(...any) error }

func scanAssignment(row rowScanner, daemonDeviceID string) (hubregistry.Assignment, error) {
	var value hubregistry.Assignment
	var assignment cloudpb.HubAssignment
	var fenced int
	var updated string
	err := row.Scan(&assignment.AccountId, &assignment.HubId, &assignment.AssignmentEpoch, &assignment.NotBeforeUnixMillis, &assignment.ExpiresAtUnixMillis, &fenced, &value.PreviousHubID, &value.PreviousEpoch, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return hubregistry.Assignment{}, hubregistry.ErrAssignmentConflict
	}
	if err != nil {
		return hubregistry.Assignment{}, err
	}
	assignment.DaemonDeviceId = daemonDeviceID
	value.Value, value.FenceSatisfied = &assignment, fenced != 0
	value.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return value, nil
}

func assignmentTx(ctx context.Context, tx *sql.Tx, daemonDeviceID string) (hubregistry.Assignment, bool, error) {
	value, err := scanAssignment(tx.QueryRowContext(ctx, `SELECT account_id,hub_id,assignment_epoch,not_before_unix_millis,expires_at_unix_millis,fence_satisfied,previous_hub_id,previous_epoch,updated_at FROM hub_assignments WHERE daemon_device_id=?`, daemonDeviceID), daemonDeviceID)
	if errors.Is(err, hubregistry.ErrAssignmentConflict) {
		return hubregistry.Assignment{}, false, nil
	}
	return value, err == nil, err
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
