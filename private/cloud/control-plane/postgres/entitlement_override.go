package postgres

import (
	"context"
	"database/sql"
	"time"

	cloudentitlement "github.com/muxvia/muxvia/private/cloud/control-plane/entitlement"
	"github.com/muxvia/muxvia/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

// EntitlementOverrides 返回账号的类型化覆盖，按生效时间和 ID 稳定排序。
func (store *Store) EntitlementOverrides(ctx context.Context, accountID string, includeRevoked bool, limit int) ([]*cloudpb.EntitlementOverrideProjection, error) {
	include := 0
	if includeRevoked {
		include = 1
	}
	rows, err := queryContext(ctx, store.db, `SELECT projection FROM entitlement_overrides WHERE account_id=? AND (revoked_at=0 OR ?=1) ORDER BY effective_from,override_id LIMIT ?`, accountID, include, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*cloudpb.EntitlementOverrideProjection
	for rows.Next() {
		var body []byte
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		value := &cloudpb.EntitlementOverrideProjection{}
		if err := proto.Unmarshal(body, value); err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

// CommitEntitlementOverride 原子创建/更新覆盖、可选替换 Entitlement 并写运营审计。
func (store *Store) CommitEntitlementOverride(ctx context.Context, value *cloudpb.EntitlementOverrideProjection, expected uint64, next *cloudpb.EntitlementProjection, audit *cloudpb.OperatorMutationAuditProjection, now time.Time) error {
	if value == nil || audit == nil {
		return cloudentitlement.ErrOverrideInvalid
	}
	body, err := marshal(value)
	if err != nil {
		return err
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	activationApplied, expirationApplied := 0, 0
	if value.GetEffectiveFromUnixMillis() <= now.UnixMilli() {
		activationApplied = 1
	}
	if value.GetEffectiveUntilUnixMillis() <= now.UnixMilli() {
		expirationApplied = 1
	}
	if expected == 0 {
		_, err = execContext(ctx, tx, `INSERT INTO entitlement_overrides(override_id,account_id,revision,effective_from,effective_until,revoked_at,activation_applied,expiration_applied,projection) VALUES(?,?,?,?,?,?,?,?,?)`, value.GetOverrideId(), value.GetAccountId(), value.GetRevision(), value.GetEffectiveFromUnixMillis(), value.GetEffectiveUntilUnixMillis(), value.GetRevokedAtUnixMillis(), activationApplied, expirationApplied, body)
	} else {
		var result sql.Result
		result, err = execContext(ctx, tx, `UPDATE entitlement_overrides SET revision=?,effective_from=?,effective_until=?,revoked_at=?,activation_applied=?,expiration_applied=?,projection=? WHERE override_id=? AND account_id=? AND revision=? AND revoked_at=0`, value.GetRevision(), value.GetEffectiveFromUnixMillis(), value.GetEffectiveUntilUnixMillis(), value.GetRevokedAtUnixMillis(), activationApplied, expirationApplied, body, value.GetOverrideId(), value.GetAccountId(), expected)
		if err == nil {
			if changed, _ := result.RowsAffected(); changed != 1 {
				return cloudentitlement.ErrOverrideConflict
			}
		}
	}
	if err != nil {
		return cloudentitlement.ErrOverrideConflict
	}
	if err := updateEntitlementAndOperatorAudit(ctx, tx, next, audit); err != nil {
		return err
	}
	return tx.Commit()
}

// RevokeEntitlementOverride 原子 CAS 撤销覆盖、移除能力影响并写运营审计。
func (store *Store) RevokeEntitlementOverride(ctx context.Context, value *cloudpb.EntitlementOverrideProjection, expected uint64, next *cloudpb.EntitlementProjection, audit *cloudpb.OperatorMutationAuditProjection, _ time.Time) error {
	if value == nil || audit == nil || expected == 0 {
		return cloudentitlement.ErrOverrideInvalid
	}
	body, err := marshal(value)
	if err != nil {
		return err
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := execContext(ctx, tx, `UPDATE entitlement_overrides SET revision=?,revoked_at=?,activation_applied=1,expiration_applied=1,projection=? WHERE override_id=? AND account_id=? AND revision=? AND revoked_at=0`, value.GetRevision(), value.GetRevokedAtUnixMillis(), body, value.GetOverrideId(), value.GetAccountId(), expected)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return cloudentitlement.ErrOverrideConflict
	}
	if err := updateEntitlementAndOperatorAudit(ctx, tx, next, audit); err != nil {
		return err
	}
	return tx.Commit()
}

// EntitlementOverrideAccountsDue 返回需要自然生效或到期重算的去重账号。
func (store *Store) EntitlementOverrideAccountsDue(ctx context.Context, now time.Time, limit int) ([]string, error) {
	rows, err := queryContext(ctx, store.db, `SELECT DISTINCT account_id FROM entitlement_overrides WHERE revoked_at=0 AND ((activation_applied=0 AND effective_from<=?) OR (expiration_applied=0 AND effective_until<=?)) ORDER BY account_id LIMIT ?`, now.UnixMilli(), now.UnixMilli(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var accountID string
		if err := rows.Scan(&accountID); err != nil {
			return nil, err
		}
		result = append(result, accountID)
	}
	return result, rows.Err()
}

// ReconcileEntitlementOverrides 原子提交自然生效/到期后的 Entitlement，标记已处理边界并审计。
func (store *Store) ReconcileEntitlementOverrides(ctx context.Context, accountID string, next *cloudpb.EntitlementProjection, audit *cloudpb.OperatorMutationAuditProjection, now time.Time) error {
	if accountID == "" || next == nil || audit == nil || next.GetAccountId() != accountID {
		return cloudentitlement.ErrOverrideInvalid
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := execContext(ctx, tx, `UPDATE entitlement_overrides SET activation_applied=CASE WHEN effective_from<=? THEN 1 ELSE activation_applied END,expiration_applied=CASE WHEN effective_until<=? THEN 1 ELSE expiration_applied END WHERE account_id=? AND revoked_at=0`, now.UnixMilli(), now.UnixMilli(), accountID); err != nil {
		return err
	}
	if err := updateEntitlementAndOperatorAudit(ctx, tx, next, audit); err != nil {
		return err
	}
	return tx.Commit()
}

func updateEntitlementAndOperatorAudit(ctx context.Context, tx *sql.Tx, next *cloudpb.EntitlementProjection, audit *cloudpb.OperatorMutationAuditProjection) error {
	if next != nil {
		body, err := marshal(next)
		if err != nil {
			return err
		}
		result, err := execContext(ctx, tx, `UPDATE commerce_entitlements SET projection=? WHERE account_id=?`, body, next.GetAccountId())
		if err != nil {
			return err
		}
		if changed, _ := result.RowsAffected(); changed != 1 {
			return cloudentitlement.ErrOverrideNotFound
		}
	}
	auditBody, err := marshal(audit)
	if err != nil {
		return err
	}
	if _, err := execContext(ctx, tx, `INSERT INTO operator_mutation_audit(audit_id,account_id,occurred_at,projection) VALUES(?,?,?,?)`, audit.GetAuditId(), audit.GetAccountId(), audit.GetOccurredAtUnixMillis(), auditBody); err != nil {
		return cloudentitlement.ErrOverrideConflict
	}
	return nil
}
