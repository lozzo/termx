package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"time"

	"github.com/muxvia/muxvia/private/cloud/control-plane/commandoutbox"
	"github.com/muxvia/muxvia/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

// CreateCommand 原子保存 parent projection 与全部 child 索引。
// 相同 account+idempotency key 返回首次记录；command ID 冲突不会覆盖旧 operation。
func (store *Store) CreateCommand(ctx context.Context, record commandoutbox.Record) (commandoutbox.Record, bool, error) {
	if record.Projection == nil || record.Version != 1 || record.IdempotencyKey == "" {
		return commandoutbox.Record{}, false, commandoutbox.ErrCommandConflict
	}
	body, err := proto.Marshal(record.Projection)
	if err != nil {
		return commandoutbox.Record{}, false, err
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return commandoutbox.Record{}, false, err
	}
	defer tx.Rollback()
	result, err := execContext(ctx, tx, `INSERT INTO management_commands(command_id,account_id,idempotency_key,command_kind,delivery_state,execution_state,expires_at,updated_at,version,projection) VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(account_id,idempotency_key) DO NOTHING`, record.Projection.GetCommandId(), record.Projection.GetAccountId(), record.IdempotencyKey, record.Projection.GetCommandKind(), record.Projection.GetDeliveryState(), record.Projection.GetExecutionState(), record.Projection.GetExpiresAtUnixMillis(), record.Projection.GetUpdatedAtUnixMillis(), record.Version, body)
	if err != nil {
		return commandoutbox.Record{}, false, commandoutbox.ErrCommandConflict
	}
	inserted, _ := result.RowsAffected()
	if inserted == 0 {
		stored, err := scanCommandRecord(queryRowContext(ctx, tx, `SELECT projection,idempotency_key,version FROM management_commands WHERE account_id=? AND idempotency_key=?`, record.Projection.GetAccountId(), record.IdempotencyKey))
		return stored, false, err
	}
	for _, child := range record.Projection.GetChildren() {
		if _, err := execContext(ctx, tx, `INSERT INTO management_command_children(child_command_id,parent_command_id,target_hub_id) VALUES(?,?,?)`, child.GetChildCommandId(), record.Projection.GetCommandId(), child.GetTargetHubId()); err != nil {
			return commandoutbox.Record{}, false, commandoutbox.ErrCommandConflict
		}
	}
	if err := tx.Commit(); err != nil {
		return commandoutbox.Record{}, false, err
	}
	return cloneCommandRecord(record), true, nil
}

// CreateDeviceRevokeCommand 原子提交持久设备 revoke 与 parent/child command。
// authority mutation 不会因为后续 runtime child 超时而回滚。
func (store *Store) CreateDeviceRevokeCommand(ctx context.Context, record commandoutbox.Record, deviceID string, expectedAuthEpoch uint64) (commandoutbox.Record, bool, error) {
	if record.Projection == nil || record.Version != 1 || record.IdempotencyKey == "" || deviceID == "" || expectedAuthEpoch == 0 {
		return commandoutbox.Record{}, false, commandoutbox.ErrCommandConflict
	}
	body, err := proto.Marshal(record.Projection)
	if err != nil {
		return commandoutbox.Record{}, false, err
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return commandoutbox.Record{}, false, err
	}
	defer tx.Rollback()
	stored, loadErr := scanCommandRecord(queryRowContext(ctx, tx, `SELECT projection,idempotency_key,version FROM management_commands WHERE account_id=? AND idempotency_key=?`, record.Projection.GetAccountId(), record.IdempotencyKey))
	if loadErr == nil {
		return stored, false, nil
	}
	if !errors.Is(loadErr, commandoutbox.ErrCommandNotFound) {
		return commandoutbox.Record{}, false, loadErr
	}
	updatedAt := time.UnixMilli(record.Projection.GetUpdatedAtUnixMillis()).UTC().Format(time.RFC3339Nano)
	updated, err := execContext(ctx, tx, `UPDATE cloud_device_ownership SET auth_epoch=auth_epoch+1,revoked=1,updated_at=? WHERE device_id=? AND account_id=? AND auth_epoch=? AND revoked=0`, updatedAt, deviceID, record.Projection.GetAccountId(), expectedAuthEpoch)
	if err != nil {
		return commandoutbox.Record{}, false, err
	}
	if changed, _ := updated.RowsAffected(); changed != 1 {
		return commandoutbox.Record{}, false, commandoutbox.ErrCommandConflict
	}
	if _, err := execContext(ctx, tx, `INSERT INTO management_commands(command_id,account_id,idempotency_key,command_kind,delivery_state,execution_state,expires_at,updated_at,version,projection) VALUES(?,?,?,?,?,?,?,?,?,?)`, record.Projection.GetCommandId(), record.Projection.GetAccountId(), record.IdempotencyKey, record.Projection.GetCommandKind(), record.Projection.GetDeliveryState(), record.Projection.GetExecutionState(), record.Projection.GetExpiresAtUnixMillis(), record.Projection.GetUpdatedAtUnixMillis(), record.Version, body); err != nil {
		return commandoutbox.Record{}, false, commandoutbox.ErrCommandConflict
	}
	for _, child := range record.Projection.GetChildren() {
		if _, err := execContext(ctx, tx, `INSERT INTO management_command_children(child_command_id,parent_command_id,target_hub_id) VALUES(?,?,?)`, child.GetChildCommandId(), record.Projection.GetCommandId(), child.GetTargetHubId()); err != nil {
			return commandoutbox.Record{}, false, commandoutbox.ErrCommandConflict
		}
	}
	if err := tx.Commit(); err != nil {
		return commandoutbox.Record{}, false, err
	}
	return cloneCommandRecord(record), true, nil
}

// Command 返回账号隔离的持久 command。
func (store *Store) Command(ctx context.Context, accountID, commandID string) (commandoutbox.Record, error) {
	return scanCommandRecord(queryRowContext(ctx, store.db, `SELECT projection,idempotency_key,version FROM management_commands WHERE account_id=? AND command_id=?`, accountID, commandID))
}

// CommandByIdempotency 返回账号内首次持久 operation。
func (store *Store) CommandByIdempotency(ctx context.Context, accountID, idempotencyKey string) (commandoutbox.Record, error) {
	return scanCommandRecord(queryRowContext(ctx, store.db, `SELECT projection,idempotency_key,version FROM management_commands WHERE account_id=? AND idempotency_key=?`, accountID, idempotencyKey))
}

// CommandByChild 通过全局唯一 child ID 返回 parent command。
func (store *Store) CommandByChild(ctx context.Context, childID string) (commandoutbox.Record, error) {
	return scanCommandRecord(queryRowContext(ctx, store.db, `SELECT c.projection,c.idempotency_key,c.version FROM management_commands c JOIN management_command_children child ON child.parent_command_id=c.command_id WHERE child.child_command_id=?`, childID))
}

// OpenCommands 返回仍有 pending execution child 的有界 command，包含已过期但尚未收敛的记录。
func (store *Store) OpenCommands(ctx context.Context, limit int) ([]commandoutbox.Record, error) {
	if limit < 1 || limit > 256 {
		return nil, commandoutbox.ErrCommandConflict
	}
	rows, err := queryContext(ctx, store.db, `SELECT projection,idempotency_key,version FROM management_commands WHERE execution_state=? ORDER BY updated_at,command_id LIMIT ?`, cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_PENDING, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []commandoutbox.Record
	for rows.Next() {
		record, err := scanCommandRecord(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, record)
	}
	return result, rows.Err()
}

// ListCommands 返回账号隔离的有界 command；零值枚举表示不筛选。
func (store *Store) ListCommands(ctx context.Context, accountID string, kind cloudpb.ManagementCommandKind, execution cloudpb.CommandExecutionState, limit int) ([]commandoutbox.Record, error) {
	if accountID == "" || limit < 1 || limit > 256 {
		return nil, commandoutbox.ErrCommandConflict
	}
	rows, err := queryContext(ctx, store.db, `SELECT projection,idempotency_key,version FROM management_commands WHERE account_id=? AND (?=0 OR command_kind=?) AND (?=0 OR execution_state=?) ORDER BY updated_at DESC,command_id DESC LIMIT ?`, accountID, kind, kind, execution, execution, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []commandoutbox.Record
	for rows.Next() {
		record, err := scanCommandRecord(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, record)
	}
	return result, rows.Err()
}

// ApplyCommandResult 在同一事务中持久 result digest journal 与 parent projection CAS。
// exact replay 返回当前 projection；冲突 digest 或旧 version 不产生副作用。
func (store *Store) ApplyCommandResult(ctx context.Context, next commandoutbox.Record, expectedVersion uint64, childID, resultKind string, digest [sha256.Size]byte, resultBody []byte) (commandoutbox.Record, bool, error) {
	if next.Projection == nil || next.Version != expectedVersion+1 || childID == "" || resultKind == "" || len(resultBody) == 0 {
		return commandoutbox.Record{}, false, commandoutbox.ErrCommandConflict
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return commandoutbox.Record{}, false, err
	}
	defer tx.Rollback()
	var storedKind string
	var storedDigest []byte
	err = queryRowContext(ctx, tx, `SELECT result_kind,digest FROM management_command_results WHERE child_command_id=? AND result_kind=?`, childID, resultKind).Scan(&storedKind, &storedDigest)
	if err == nil {
		if storedKind != resultKind || !bytes.Equal(storedDigest, digest[:]) {
			return commandoutbox.Record{}, false, commandoutbox.ErrCommandConflict
		}
		stored, loadErr := scanCommandRecord(queryRowContext(ctx, tx, `SELECT c.projection,c.idempotency_key,c.version FROM management_commands c JOIN management_command_children child ON child.parent_command_id=c.command_id WHERE child.child_command_id=?`, childID))
		return stored, true, loadErr
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return commandoutbox.Record{}, false, err
	}
	body, err := proto.Marshal(next.Projection)
	if err != nil {
		return commandoutbox.Record{}, false, err
	}
	updated, err := execContext(ctx, tx, `UPDATE management_commands SET delivery_state=?,execution_state=?,expires_at=?,updated_at=?,version=?,projection=? WHERE command_id=? AND version=?`, next.Projection.GetDeliveryState(), next.Projection.GetExecutionState(), next.Projection.GetExpiresAtUnixMillis(), next.Projection.GetUpdatedAtUnixMillis(), next.Version, body, next.Projection.GetCommandId(), expectedVersion)
	if err != nil {
		return commandoutbox.Record{}, false, err
	}
	if changed, _ := updated.RowsAffected(); changed != 1 {
		return commandoutbox.Record{}, false, commandoutbox.ErrCommandConflict
	}
	if _, err := execContext(ctx, tx, `INSERT INTO management_command_results(child_command_id,result_kind,digest,result,created_at) VALUES(?,?,?,?,?)`, childID, resultKind, digest[:], append([]byte(nil), resultBody...), next.Projection.GetUpdatedAtUnixMillis()); err != nil {
		return commandoutbox.Record{}, false, commandoutbox.ErrCommandConflict
	}
	if migration := commandMigrationTarget(next.Projection, childID); migration != nil && resultKind == "hub" {
		result := &cloudpb.HubCommandResult{}
		if err := proto.Unmarshal(resultBody, result); err != nil || result.GetHubId() != migration.GetSourceHubId() || result.GetExecutionControlGeneration() != migration.GetSourceControlGeneration() {
			return commandoutbox.Record{}, false, commandoutbox.ErrCommandConflict
		}
		if result.GetResultCode() == cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_APPLIED || result.GetResultCode() == cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_ALREADY_SATISFIED {
			moved, err := execContext(ctx, tx, `UPDATE hub_assignments SET hub_id=?,assignment_epoch=?,not_before_unix_millis=?,expires_at_unix_millis=?,fence_satisfied=0,previous_hub_id=?,previous_epoch=?,updated_at=? WHERE daemon_device_id=? AND account_id=? AND hub_id=? AND assignment_epoch=?`, migration.GetTargetHubId(), migration.GetTargetAssignmentEpoch(), migration.GetTargetNotBeforeUnixMillis(), migration.GetTargetExpiresAtUnixMillis(), migration.GetSourceHubId(), migration.GetSourceAssignmentEpoch(), time.UnixMilli(result.GetCompletedAtUnixMillis()).UTC().Format(time.RFC3339Nano), migration.GetDaemonDeviceId(), next.Projection.GetAccountId(), migration.GetSourceHubId(), migration.GetSourceAssignmentEpoch())
			if err != nil {
				return commandoutbox.Record{}, false, err
			}
			if changed, _ := moved.RowsAffected(); changed != 1 {
				return commandoutbox.Record{}, false, commandoutbox.ErrCommandConflict
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return commandoutbox.Record{}, false, err
	}
	return cloneCommandRecord(next), false, nil
}

func commandMigrationTarget(projection *cloudpb.ManagementCommandProjection, childID string) *cloudpb.AssignmentMigrationTarget {
	if projection == nil || projection.GetCommandKind() != cloudpb.ManagementCommandKind_MANAGEMENT_COMMAND_KIND_MIGRATE_ASSIGNMENT {
		return nil
	}
	for _, child := range projection.GetChildren() {
		if child.GetChildCommandId() == childID {
			return child.GetTarget().GetAssignmentMigration()
		}
	}
	return nil
}

// ReplaceCommand 使用版本 CAS 保存 expiry 或其它非 receipt 状态推进。
func (store *Store) ReplaceCommand(ctx context.Context, next commandoutbox.Record, expectedVersion uint64) (commandoutbox.Record, error) {
	if next.Projection == nil || next.Version != expectedVersion+1 {
		return commandoutbox.Record{}, commandoutbox.ErrCommandConflict
	}
	body, err := proto.Marshal(next.Projection)
	if err != nil {
		return commandoutbox.Record{}, err
	}
	result, err := execContext(ctx, store.db, `UPDATE management_commands SET delivery_state=?,execution_state=?,expires_at=?,updated_at=?,version=?,projection=? WHERE command_id=? AND version=?`, next.Projection.GetDeliveryState(), next.Projection.GetExecutionState(), next.Projection.GetExpiresAtUnixMillis(), next.Projection.GetUpdatedAtUnixMillis(), next.Version, body, next.Projection.GetCommandId(), expectedVersion)
	if err != nil {
		return commandoutbox.Record{}, err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return commandoutbox.Record{}, commandoutbox.ErrCommandConflict
	}
	return cloneCommandRecord(next), nil
}

func scanCommandRecord(scanner interface{ Scan(...any) error }) (commandoutbox.Record, error) {
	var body []byte
	var record commandoutbox.Record
	if err := scanner.Scan(&body, &record.IdempotencyKey, &record.Version); errors.Is(err, sql.ErrNoRows) {
		return commandoutbox.Record{}, commandoutbox.ErrCommandNotFound
	} else if err != nil {
		return commandoutbox.Record{}, err
	}
	record.Projection = &cloudpb.ManagementCommandProjection{}
	if err := proto.Unmarshal(body, record.Projection); err != nil {
		return commandoutbox.Record{}, commandoutbox.ErrCommandConflict
	}
	return record, nil
}

func cloneCommandRecord(record commandoutbox.Record) commandoutbox.Record {
	result := record
	result.Projection = proto.Clone(record.Projection).(*cloudpb.ManagementCommandProjection)
	return result
}
