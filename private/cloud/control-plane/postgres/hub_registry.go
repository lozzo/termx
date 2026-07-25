// Package postgres 实现 Control Plane 领域 store 的标准 PostgreSQL adapter。
// 该包只负责 migration、事务和编码，不拥有 Hub generation、assignment 或 fencing 业务语义。
package postgres

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/muxvia/muxvia/private/cloud/control-plane/hubregistry"
	"github.com/muxvia/muxvia/private/cloud/control-plane/persistence"
	"github.com/muxvia/muxvia/proto/cloudpb"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Store 是单区域 Controller 的 PostgreSQL 持久 adapter。
type Store struct{ db *sql.DB }

var _ persistence.Store = (*Store)(nil)

// Open 建立 PostgreSQL 连接并在数据库 advisory lock 内应用 versioned migration。
// DSN 必须由部署 secret 提供；连接、Ping 或 migration 失败时不返回可用 Store。
func Open(ctx context.Context, dsn string) (*Store, error) {
	if err := ValidateDSN(dsn); err != nil {
		return nil, err
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(16)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(30 * time.Minute)
	store := &Store{db: db}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping Control Plane PostgreSQL: %w", err)
	}
	if err := store.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

// ValidateDSN 强制 Controller 使用 PostgreSQL URL，并要求所有非 loopback 连接显式启用 TLS。
// 函数不返回或包装原始 DSN，避免数据库密码进入日志和错误链。
func ValidateDSN(dsn string) error {
	if strings.TrimSpace(dsn) == "" {
		return fmt.Errorf("Control Plane PostgreSQL DSN is required")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" || parsed.Hostname() == "" {
		return fmt.Errorf("Control Plane PostgreSQL DSN must be a PostgreSQL URL")
	}
	host := parsed.Hostname()
	loopback := strings.EqualFold(host, "localhost")
	if address := net.ParseIP(host); address != nil && address.IsLoopback() {
		loopback = true
	}
	sslMode := strings.ToLower(parsed.Query().Get("sslmode"))
	if loopback {
		if sslMode == "" || sslMode == "disable" || sslMode == "require" || sslMode == "verify-ca" || sslMode == "verify-full" {
			return nil
		}
		return fmt.Errorf("Control Plane PostgreSQL sslmode is invalid")
	}
	switch sslMode {
	case "require", "verify-ca", "verify-full":
		return nil
	default:
		return fmt.Errorf("remote Control Plane PostgreSQL requires TLS")
	}
}

// Close 关闭 PostgreSQL 连接池。
func (store *Store) Close() error { return store.db.Close() }

func (store *Store) migrate(ctx context.Context) error {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(5589538654209)`); err != nil {
		return fmt.Errorf("lock PostgreSQL migrations: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS muxvia_schema_migrations(version BIGINT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL)`); err != nil {
		return err
	}
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		prefix, _, ok := strings.Cut(entry.Name(), "_")
		version, parseErr := strconv.ParseInt(prefix, 10, 64)
		if !ok || parseErr != nil || version <= 0 {
			return fmt.Errorf("invalid PostgreSQL migration name %q", entry.Name())
		}
		var applied bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM muxvia_schema_migrations WHERE version=$1)`, version).Scan(&applied); err != nil {
			return err
		}
		if applied {
			continue
		}
		body, readErr := migrationFiles.ReadFile("migrations/" + entry.Name())
		if readErr != nil {
			return readErr
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			return fmt.Errorf("apply PostgreSQL migration %s: %w", entry.Name(), err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO muxvia_schema_migrations(version,applied_at) VALUES($1,$2)`, version, time.Now().UTC()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type sqlExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

type sqlQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func execContext(ctx context.Context, executor sqlExecutor, query string, args ...any) (sql.Result, error) {
	return executor.ExecContext(ctx, rebind(query), args...)
}

func queryContext(ctx context.Context, queryer sqlQueryer, query string, args ...any) (*sql.Rows, error) {
	return queryer.QueryContext(ctx, rebind(query), args...)
}

func queryRowContext(ctx context.Context, queryer sqlQueryer, query string, args ...any) *sql.Row {
	return queryer.QueryRowContext(ctx, rebind(query), args...)
}

func rebind(query string) string {
	var result strings.Builder
	result.Grow(len(query) + 16)
	index := 1
	for _, value := range query {
		if value == '?' {
			result.WriteByte('$')
			result.WriteString(strconv.Itoa(index))
			index++
			continue
		}
		result.WriteRune(value)
	}
	return result.String()
}

// PutDeployment upsert deployment 配置但保留已经签发的 generation。
func (store *Store) PutDeployment(ctx context.Context, value hubregistry.Deployment) error {
	metadata := value.Metadata
	_, err := execContext(ctx, store.db, `INSERT INTO hub_deployments(
hub_id,deployment_id,credential_fingerprint,control_public_key,relay_control_public_key,region,public_label,relay_id,relay_credential_fingerprint,public_hub_url,health_url,max_assignments,identity_approved,enabled,draining,archived,directory_revision,last_control_generation,last_relay_control_generation,updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(hub_id) DO UPDATE SET
deployment_id=excluded.deployment_id,credential_fingerprint=excluded.credential_fingerprint,control_public_key=excluded.control_public_key,
relay_control_public_key=excluded.relay_control_public_key,
region=excluded.region,public_label=excluded.public_label,relay_id=excluded.relay_id,relay_credential_fingerprint=excluded.relay_credential_fingerprint,
public_hub_url=excluded.public_hub_url,health_url=excluded.health_url,max_assignments=excluded.max_assignments,identity_approved=excluded.identity_approved,
enabled=excluded.enabled,draining=excluded.draining,archived=excluded.archived,directory_revision=excluded.directory_revision,updated_at=excluded.updated_at`, metadata.GetHubId(), metadata.GetEdgeDeploymentId(), metadata.GetHubControlIdentityFingerprint(), []byte(value.ControlPublicKey), []byte(value.RelayControlPublicKey), metadata.GetRegion(), metadata.GetPublicLabel(), metadata.GetRelayId(), metadata.GetRelayControlIdentityFingerprint(), value.PublicHubURL, value.HealthURL, value.MaxAssignments, boolInt(value.IdentityApproved), boolInt(value.Enabled), boolInt(value.Draining), boolInt(value.Archived), value.DirectoryRevision, value.ControlGeneration, value.RelayControlGeneration, value.UpdatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

// CreateDeployment 原子保存待批准 directory 与 operator audit；Hub/Relay generation 从零开始。
func (store *Store) CreateDeployment(ctx context.Context, value hubregistry.Deployment, audit *cloudpb.OperatorMutationAuditProjection) error {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	metadata := value.Metadata
	if _, err := execContext(ctx, tx, `INSERT INTO hub_deployments(
hub_id,deployment_id,credential_fingerprint,control_public_key,relay_control_public_key,region,public_label,relay_id,relay_credential_fingerprint,public_hub_url,health_url,max_assignments,identity_approved,enabled,draining,archived,directory_revision,last_control_generation,last_relay_control_generation,updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, metadata.GetHubId(), metadata.GetEdgeDeploymentId(), metadata.GetHubControlIdentityFingerprint(), []byte(value.ControlPublicKey), []byte(value.RelayControlPublicKey), metadata.GetRegion(), metadata.GetPublicLabel(), metadata.GetRelayId(), metadata.GetRelayControlIdentityFingerprint(), value.PublicHubURL, value.HealthURL, value.MaxAssignments, boolInt(value.IdentityApproved), boolInt(value.Enabled), boolInt(value.Draining), boolInt(value.Archived), value.DirectoryRevision, value.ControlGeneration, value.RelayControlGeneration, value.UpdatedAt.UTC().Format(time.RFC3339Nano)); err != nil {
		return hubregistry.ErrDeploymentConflict
	}
	if err := insertHubOperatorAudit(ctx, tx, audit); err != nil {
		return err
	}
	return tx.Commit()
}

// UpdateDeployment 用 directory revision CAS 保存目录或生命周期 mutation，并与 audit 同事务提交。
func (store *Store) UpdateDeployment(ctx context.Context, value hubregistry.Deployment, expected uint64, audit *cloudpb.OperatorMutationAuditProjection) error {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := execContext(ctx, tx, `UPDATE hub_deployments SET region=?,public_label=?,public_hub_url=?,health_url=?,max_assignments=?,identity_approved=?,enabled=?,draining=?,archived=?,directory_revision=?,updated_at=? WHERE hub_id=? AND directory_revision=? AND archived=0`, value.Metadata.GetRegion(), value.Metadata.GetPublicLabel(), value.PublicHubURL, value.HealthURL, value.MaxAssignments, boolInt(value.IdentityApproved), boolInt(value.Enabled), boolInt(value.Draining), boolInt(value.Archived), value.DirectoryRevision, value.UpdatedAt.UTC().Format(time.RFC3339Nano), value.Metadata.GetHubId(), expected)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return hubregistry.ErrDeploymentConflict
	}
	if err := insertHubOperatorAudit(ctx, tx, audit); err != nil {
		return err
	}
	return tx.Commit()
}

// ArchiveDeployment 在同一事务锁定 directory、检查有效 assignment 清零并保存 disable audit。
func (store *Store) ArchiveDeployment(ctx context.Context, value hubregistry.Deployment, expected uint64, now time.Time, audit *cloudpb.OperatorMutationAuditProjection) error {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current uint64
	if err := queryRowContext(ctx, tx, `SELECT directory_revision FROM hub_deployments WHERE hub_id=? FOR UPDATE`, value.Metadata.GetHubId()).Scan(&current); errors.Is(err, sql.ErrNoRows) {
		return hubregistry.ErrDeploymentNotFound
	} else if err != nil {
		return err
	}
	if current != expected {
		return hubregistry.ErrDeploymentConflict
	}
	var assignments uint64
	if err := queryRowContext(ctx, tx, `SELECT COUNT(*) FROM hub_assignments WHERE hub_id=? AND expires_at_unix_millis>?`, value.Metadata.GetHubId(), now.UnixMilli()).Scan(&assignments); err != nil {
		return err
	}
	if assignments != 0 {
		return hubregistry.ErrDeploymentAssignmentsRemain
	}
	result, err := execContext(ctx, tx, `UPDATE hub_deployments SET enabled=0,archived=1,directory_revision=?,updated_at=? WHERE hub_id=? AND directory_revision=? AND enabled=1 AND draining=1 AND archived=0`, value.DirectoryRevision, value.UpdatedAt.UTC().Format(time.RFC3339Nano), value.Metadata.GetHubId(), expected)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return hubregistry.ErrDeploymentLifecycle
	}
	if err := insertHubOperatorAudit(ctx, tx, audit); err != nil {
		return err
	}
	return tx.Commit()
}

func insertHubOperatorAudit(ctx context.Context, tx *sql.Tx, audit *cloudpb.OperatorMutationAuditProjection) error {
	if audit == nil {
		return hubregistry.ErrDeploymentConflict
	}
	body, err := marshal(audit)
	if err != nil {
		return err
	}
	if _, err := execContext(ctx, tx, `INSERT INTO operator_mutation_audit(audit_id,account_id,occurred_at,projection) VALUES(?,?,?,?)`, audit.GetAuditId(), audit.GetAccountId(), audit.GetOccurredAtUnixMillis(), body); err != nil {
		return hubregistry.ErrDeploymentConflict
	}
	return nil
}

// Deployment 读取一个 Hub deployment 深拷贝。
func (store *Store) Deployment(ctx context.Context, hubID string) (hubregistry.Deployment, error) {
	var value hubregistry.Deployment
	var metadata cloudpb.EdgeDeploymentMetadata
	var publicKey, relayPublicKey []byte
	var approved, enabled, draining, archived int
	var updated string
	err := queryRowContext(ctx, store.db, `SELECT deployment_id,credential_fingerprint,control_public_key,relay_control_public_key,region,public_label,relay_id,relay_credential_fingerprint,public_hub_url,health_url,max_assignments,identity_approved,enabled,draining,archived,directory_revision,last_control_generation,last_relay_control_generation,updated_at FROM hub_deployments WHERE hub_id=?`, hubID).Scan(&metadata.EdgeDeploymentId, &metadata.HubControlIdentityFingerprint, &publicKey, &relayPublicKey, &metadata.Region, &metadata.PublicLabel, &metadata.RelayId, &metadata.RelayControlIdentityFingerprint, &value.PublicHubURL, &value.HealthURL, &value.MaxAssignments, &approved, &enabled, &draining, &archived, &value.DirectoryRevision, &value.ControlGeneration, &value.RelayControlGeneration, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return hubregistry.Deployment{}, hubregistry.ErrDeploymentNotFound
	}
	if err != nil {
		return hubregistry.Deployment{}, err
	}
	metadata.HubId = hubID
	value.Metadata, value.ControlPublicKey, value.RelayControlPublicKey = &metadata, append(ed25519.PublicKey(nil), publicKey...), append(ed25519.PublicKey(nil), relayPublicKey...)
	value.IdentityApproved, value.Enabled, value.Draining, value.Archived = approved != 0, enabled != 0, draining != 0, archived != 0
	value.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return value, nil
}

// DeploymentByRelay 按唯一 relay_id 解析 deployment。
func (store *Store) DeploymentByRelay(ctx context.Context, relayID string) (hubregistry.Deployment, error) {
	var hubID string
	err := queryRowContext(ctx, store.db, `SELECT hub_id FROM hub_deployments WHERE relay_id=?`, relayID).Scan(&hubID)
	if errors.Is(err, sql.ErrNoRows) {
		return hubregistry.Deployment{}, hubregistry.ErrDeploymentNotFound
	}
	if err != nil {
		return hubregistry.Deployment{}, err
	}
	return store.Deployment(ctx, hubID)
}

// Deployments 按 hub_id 稳定返回全部 deployment。
func (store *Store) Deployments(ctx context.Context) ([]hubregistry.Deployment, error) {
	rows, err := queryContext(ctx, store.db, `SELECT hub_id FROM hub_deployments ORDER BY hub_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result := make([]hubregistry.Deployment, 0, len(ids))
	for _, id := range ids {
		value, err := store.Deployment(ctx, id)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, nil
}

// AdvanceControlGeneration 事务校验 deployment identity 并签发唯一递增 generation。
func (store *Store) AdvanceControlGeneration(ctx context.Context, hubID, deploymentID, fingerprint string, now time.Time) (hubregistry.Deployment, error) {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return hubregistry.Deployment{}, err
	}
	defer tx.Rollback()
	var storedDeployment, storedFingerprint string
	var approved, enabled, archived int
	var generation uint64
	if err = queryRowContext(ctx, tx, `SELECT deployment_id,credential_fingerprint,identity_approved,enabled,archived,last_control_generation FROM hub_deployments WHERE hub_id=? FOR UPDATE`, hubID).Scan(&storedDeployment, &storedFingerprint, &approved, &enabled, &archived, &generation); errors.Is(err, sql.ErrNoRows) {
		return hubregistry.Deployment{}, hubregistry.ErrDeploymentNotFound
	} else if err != nil {
		return hubregistry.Deployment{}, err
	}
	if approved == 0 || enabled == 0 || archived != 0 {
		return hubregistry.Deployment{}, hubregistry.ErrDeploymentNotFound
	}
	if storedDeployment != deploymentID || storedFingerprint != fingerprint {
		return hubregistry.Deployment{}, hubregistry.ErrDeploymentIdentity
	}
	generation++
	if _, err = execContext(ctx, tx, `UPDATE hub_deployments SET last_control_generation=?,updated_at=? WHERE hub_id=? AND last_control_generation=?`, generation, now.Format(time.RFC3339Nano), hubID, generation-1); err != nil {
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
	var approved, enabled, archived int
	err := queryRowContext(ctx, store.db, `SELECT last_control_generation,identity_approved,enabled,archived FROM hub_deployments WHERE hub_id=?`, hubID).Scan(&current, &approved, &enabled, &archived)
	if errors.Is(err, sql.ErrNoRows) {
		return false, hubregistry.ErrDeploymentNotFound
	}
	return err == nil && approved != 0 && enabled != 0 && archived == 0 && current == generation, err
}

// AdvanceRelayControlGeneration 按 relay_id 校验独立 identity 并推进 Relay generation。
func (store *Store) AdvanceRelayControlGeneration(ctx context.Context, relayID, deploymentID, fingerprint string, now time.Time) (hubregistry.Deployment, error) {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return hubregistry.Deployment{}, err
	}
	defer tx.Rollback()
	var hubID, storedDeployment, storedFingerprint string
	var approved, enabled, archived int
	var generation uint64
	err = queryRowContext(ctx, tx, `SELECT hub_id,deployment_id,relay_credential_fingerprint,identity_approved,enabled,archived,last_relay_control_generation FROM hub_deployments WHERE relay_id=? FOR UPDATE`, relayID).Scan(&hubID, &storedDeployment, &storedFingerprint, &approved, &enabled, &archived, &generation)
	if errors.Is(err, sql.ErrNoRows) || approved == 0 || enabled == 0 || archived != 0 {
		return hubregistry.Deployment{}, hubregistry.ErrDeploymentNotFound
	}
	if err != nil {
		return hubregistry.Deployment{}, err
	}
	if storedDeployment != deploymentID || storedFingerprint != fingerprint {
		return hubregistry.Deployment{}, hubregistry.ErrDeploymentIdentity
	}
	generation++
	result, err := execContext(ctx, tx, `UPDATE hub_deployments SET last_relay_control_generation=?,updated_at=? WHERE hub_id=? AND last_relay_control_generation=?`, generation, now.Format(time.RFC3339Nano), hubID, generation-1)
	if err != nil {
		return hubregistry.Deployment{}, err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return hubregistry.Deployment{}, hubregistry.ErrStaleControlGeneration
	}
	if err := tx.Commit(); err != nil {
		return hubregistry.Deployment{}, err
	}
	return store.Deployment(ctx, hubID)
}

// RelayControlGenerationCurrent 查询 Relay handler generation 是否仍为当前值。
func (store *Store) RelayControlGenerationCurrent(ctx context.Context, relayID string, generation uint64) (bool, error) {
	var current uint64
	var approved, enabled, archived int
	err := queryRowContext(ctx, store.db, `SELECT last_relay_control_generation,identity_approved,enabled,archived FROM hub_deployments WHERE relay_id=?`, relayID).Scan(&current, &approved, &enabled, &archived)
	if errors.Is(err, sql.ErrNoRows) {
		return false, hubregistry.ErrDeploymentNotFound
	}
	return err == nil && approved != 0 && enabled != 0 && archived == 0 && current == generation, err
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
	var approved, enabled, draining, archived int
	var maximum uint64
	if err := queryRowContext(ctx, tx, `SELECT identity_approved,enabled,draining,archived,max_assignments FROM hub_deployments WHERE hub_id=? FOR UPDATE`, next.GetHubId()).Scan(&approved, &enabled, &draining, &archived, &maximum); errors.Is(err, sql.ErrNoRows) {
		return hubregistry.Assignment{}, hubregistry.ErrDeploymentNotFound
	} else if err != nil {
		return hubregistry.Assignment{}, err
	}
	isRenewal := found && current.Value.GetHubId() == next.GetHubId()
	if approved == 0 || enabled == 0 || archived != 0 || draining != 0 && !isRenewal || maximum == 0 {
		return hubregistry.Assignment{}, hubregistry.ErrDeploymentLifecycle
	}
	if !isRenewal {
		var assigned uint64
		if err := queryRowContext(ctx, tx, `SELECT COUNT(*) FROM hub_assignments WHERE hub_id=? AND expires_at_unix_millis>?`, next.GetHubId(), now.UnixMilli()).Scan(&assigned); err != nil {
			return hubregistry.Assignment{}, err
		}
		if assigned >= maximum {
			return hubregistry.Assignment{}, hubregistry.ErrDeploymentLifecycle
		}
	}
	_, err = execContext(ctx, tx, `INSERT INTO hub_assignments(daemon_device_id,account_id,hub_id,assignment_epoch,not_before_unix_millis,expires_at_unix_millis,fence_satisfied,previous_hub_id,previous_epoch,updated_at)
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
	result, err := execContext(ctx, store.db, `UPDATE hub_assignments SET fence_satisfied=1,updated_at=? WHERE daemon_device_id=? AND hub_id=? AND assignment_epoch=?`, now.Format(time.RFC3339Nano), daemonDeviceID, sourceHubID, sourceEpoch)
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
	row := queryRowContext(ctx, store.db, `SELECT account_id,hub_id,assignment_epoch,not_before_unix_millis,expires_at_unix_millis,fence_satisfied,previous_hub_id,previous_epoch,updated_at FROM hub_assignments WHERE daemon_device_id=?`, daemonDeviceID)
	return scanAssignment(row, daemonDeviceID)
}

// AssignmentsForHub 返回指定 Hub 当前未过期 assignment。
func (store *Store) AssignmentsForHub(ctx context.Context, hubID string, now time.Time) ([]hubregistry.Assignment, error) {
	rows, err := queryContext(ctx, store.db, `SELECT daemon_device_id,account_id,hub_id,assignment_epoch,not_before_unix_millis,expires_at_unix_millis,fence_satisfied,previous_hub_id,previous_epoch,updated_at FROM hub_assignments WHERE hub_id=? AND not_before_unix_millis<=? AND expires_at_unix_millis>? ORDER BY daemon_device_id`, hubID, now.UnixMilli(), now.UnixMilli())
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
	err := queryRowContext(ctx, store.db, `SELECT accepted_sequence,accepted_digest FROM control_receive_cursors WHERE hub_id=? AND control_generation=? AND sender_role=?`, hubID, generation, senderRole).Scan(&sequence, &digest)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil, nil
	}
	return sequence, append([]byte(nil), digest...), err
}

// PutControlCursor 原子保存一个 generation/sender role 的连续接收位置。
func (store *Store) PutControlCursor(ctx context.Context, hubID string, generation uint64, senderRole cloudpb.ControlSenderRole, sequence uint64, digest []byte, now time.Time) error {
	_, err := execContext(ctx, store.db, `INSERT INTO control_receive_cursors(hub_id,control_generation,sender_role,accepted_sequence,accepted_digest,updated_at) VALUES(?,?,?,?,?,?) ON CONFLICT(hub_id,control_generation,sender_role) DO UPDATE SET accepted_sequence=excluded.accepted_sequence,accepted_digest=excluded.accepted_digest,updated_at=excluded.updated_at`, hubID, generation, senderRole, sequence, append([]byte(nil), digest...), now.UTC().Format(time.RFC3339Nano))
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
	err = queryRowContext(ctx, tx, `SELECT projection_revision FROM hub_projection_heads WHERE hub_id=? FOR UPDATE`, hubID).Scan(&current)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	next := current + 1
	_, err = execContext(ctx, tx, `INSERT INTO hub_projection_heads(hub_id,projection_revision,digest,published_at,acknowledged_at) VALUES(?,?,?,?,NULL) ON CONFLICT(hub_id) DO UPDATE SET projection_revision=excluded.projection_revision,digest=excluded.digest,published_at=excluded.published_at,acknowledged_at=NULL`, hubID, next, []byte{}, now.UTC().Format(time.RFC3339Nano))
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
	result, err := execContext(ctx, store.db, `UPDATE hub_projection_heads SET digest=? WHERE hub_id=? AND projection_revision=?`, append([]byte(nil), digest...), hubID, revision)
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
	value, err := scanAssignment(queryRowContext(ctx, tx, `SELECT account_id,hub_id,assignment_epoch,not_before_unix_millis,expires_at_unix_millis,fence_satisfied,previous_hub_id,previous_epoch,updated_at FROM hub_assignments WHERE daemon_device_id=? FOR UPDATE`, daemonDeviceID), daemonDeviceID)
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
