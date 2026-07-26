package postgres

import (
	"context"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/muxvia/muxvia/cloud/controller/account"
	"github.com/muxvia/muxvia/cloud/controller/commerce"
	cloudv1 "github.com/muxvia/muxvia/proto/cloud/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ListOperatorAccounts 分页读取账号 ID，再组装持久商业摘要；不接触 Directory。
func (database *Database) ListOperatorAccounts(ctx context.Context, page *cloudv1.PageRequest, now time.Time) ([]*cloudv1.AccountSummary, string, error) {
	limit := pageLimit(page)
	query := strings.TrimSpace(page.GetQuery())
	rows, err := database.pool.Query(ctx, `SELECT account_id::text FROM accounts WHERE ($1='' OR display_name ILIKE '%'||$1||'%' OR email ILIKE '%'||$1||'%') AND ($2='' OR account_id>$2::uuid) ORDER BY account_id LIMIT $3`, query, page.GetCursor(), limit+1)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	ids := make([]string, 0, limit+1)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, "", err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(ids) > limit {
		next = ids[limit-1]
		ids = ids[:limit]
	}
	result := make([]*cloudv1.AccountSummary, 0, len(ids))
	for _, id := range ids {
		value, err := database.GetOperatorAccount(ctx, id, now)
		if err != nil {
			return nil, "", err
		}
		result = append(result, value)
	}
	return result, next, nil
}

// GetOperatorAccount 返回一个账号的角色、daemon 数、Subscription、Entitlement 和用量。
func (database *Database) GetOperatorAccount(ctx context.Context, accountID string, now time.Time) (*cloudv1.AccountSummary, error) {
	record, err := database.AccountByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	commercial, err := database.GetAccountCommerce(ctx, accountID, now)
	if err != nil {
		return nil, err
	}
	var daemonCount uint64
	if err := database.pool.QueryRow(ctx, `SELECT count(*) FROM daemons WHERE account_id=$1 AND revoked=false`, accountID).Scan(&daemonCount); err != nil {
		return nil, err
	}
	return &cloudv1.AccountSummary{Account: record.Profile, Roles: record.Roles, DaemonCount: daemonCount, Subscription: commercial.GetSubscription(), Entitlement: commercial.GetEntitlement(), Usage: commercial.GetUsage()}, nil
}

// ListOperatorOrders 返回按 UUID 稳定游标分页的订单。
func (database *Database) ListOperatorOrders(ctx context.Context, page *cloudv1.PageRequest) ([]*cloudv1.OrderProjection, string, error) {
	limit := pageLimit(page)
	query := strings.TrimSpace(page.GetQuery())
	rows, err := database.pool.Query(ctx, orderSelect+` WHERE ($1='' OR o.order_id::text ILIKE '%'||$1||'%' OR o.account_id::text ILIKE '%'||$1||'%' OR o.provider ILIKE '%'||$1||'%') AND ($2='' OR o.order_id>$2::uuid) ORDER BY o.order_id LIMIT $3`, query, page.GetCursor(), limit+1)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	values := make([]*cloudv1.OrderProjection, 0, limit+1)
	for rows.Next() {
		value, err := scanOrder(rows)
		if err != nil {
			return nil, "", err
		}
		values = append(values, value)
	}
	next := ""
	if len(values) > limit {
		next = values[limit-1].GetOrderId()
		values = values[:limit]
	}
	return values, next, rows.Err()
}

// ListOperatorSubscriptions 返回订阅分页。
func (database *Database) ListOperatorSubscriptions(ctx context.Context, page *cloudv1.PageRequest) ([]*cloudv1.SubscriptionProjection, string, error) {
	limit := pageLimit(page)
	query := strings.TrimSpace(page.GetQuery())
	rows, err := database.pool.Query(ctx, subscriptionSelect+` WHERE ($1='' OR s.subscription_id::text ILIKE '%'||$1||'%' OR s.account_id::text ILIKE '%'||$1||'%' OR s.plan_id ILIKE '%'||$1||'%') AND ($2='' OR s.subscription_id>$2::uuid) ORDER BY s.subscription_id LIMIT $3`, query, page.GetCursor(), limit+1)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	values := make([]*cloudv1.SubscriptionProjection, 0, limit+1)
	for rows.Next() {
		value, err := scanSubscription(rows)
		if err != nil {
			return nil, "", err
		}
		values = append(values, value)
	}
	next := ""
	if len(values) > limit {
		next = values[limit-1].GetSubscriptionId()
		values = values[:limit]
	}
	return values, next, rows.Err()
}

// ListOperatorUsage 返回账号当前账期与 Edge 聚合；缺失记录表示尚无已结算用量。
func (database *Database) ListOperatorUsage(ctx context.Context, page *cloudv1.PageRequest, now time.Time) ([]*cloudv1.UsagePeriodProjection, []*cloudv1.EdgeUsageProjection, string, error) {
	limit := pageLimit(page)
	rows, err := database.pool.Query(ctx, `SELECT u.account_id::text,u.period_start,u.period_end,u.relay_ingress_bytes,u.relay_egress_bytes,u.revision,p.relay_max_bytes_per_period FROM usage_periods u JOIN subscriptions s ON s.account_id=u.account_id JOIN plans p ON p.plan_id=s.plan_id AND p.version=s.plan_version WHERE u.period_start<=$1 AND u.period_end>$1 AND ($2='' OR u.account_id>$2::uuid) ORDER BY u.account_id LIMIT $3`, now, page.GetCursor(), limit+1)
	if err != nil {
		return nil, nil, "", err
	}
	accounts := make([]*cloudv1.UsagePeriodProjection, 0, limit+1)
	for rows.Next() {
		var id string
		var start, end time.Time
		var ingress, egress, revision, quota uint64
		if err := rows.Scan(&id, &start, &end, &ingress, &egress, &revision, &quota); err != nil {
			rows.Close()
			return nil, nil, "", err
		}
		total := ingress + egress
		remaining := uint64(0)
		if total < quota {
			remaining = quota - total
		}
		accounts = append(accounts, &cloudv1.UsagePeriodProjection{AccountId: id, PeriodStart: timestamppb.New(start), PeriodEnd: timestamppb.New(end), RelayIngressBytes: ingress, RelayEgressBytes: egress, RelayTotalBytes: total, QuotaBytes: quota, RemainingBytes: remaining, Revision: revision})
	}
	if err := rows.Err(); err != nil {
		return nil, nil, "", err
	}
	rows.Close()
	next := ""
	if len(accounts) > limit {
		next = accounts[limit-1].GetAccountId()
		accounts = accounts[:limit]
	}
	edgeRows, err := database.pool.Query(ctx, `SELECT a.edge_id::text,e.name,e.region,a.ingress_bytes,a.egress_bytes,a.event_count,a.period_start FROM relay_usage_aggregates a JOIN edge_deployments e ON e.edge_id=a.edge_id WHERE a.period_start=date_trunc('month',$1::timestamptz) ORDER BY a.edge_id`, now)
	if err != nil {
		return nil, nil, "", err
	}
	defer edgeRows.Close()
	edges := make([]*cloudv1.EdgeUsageProjection, 0)
	for edgeRows.Next() {
		value := &cloudv1.EdgeUsageProjection{}
		var start time.Time
		if err := edgeRows.Scan(&value.EdgeId, &value.EdgeName, &value.Region, &value.IngressBytes, &value.EgressBytes, &value.EventCount, &start); err != nil {
			return nil, nil, "", err
		}
		value.PeriodStart = timestamppb.New(start)
		edges = append(edges, value)
	}
	return accounts, edges, next, edgeRows.Err()
}

// ListOperatorAudit 返回按时间倒序的持久管理事实。
func (database *Database) ListOperatorAudit(ctx context.Context, page *cloudv1.PageRequest) ([]*cloudv1.OperatorAuditEvent, string, error) {
	limit := pageLimit(page)
	query := strings.TrimSpace(page.GetQuery())
	statement := `SELECT e.audit_id::text,coalesce(e.actor_account_id::text,''),coalesce(a.display_name,'系统'),e.action,e.resource_type,e.resource_id,e.reason,e.result,e.correlation_id,e.occurred_at FROM operator_audit_events e LEFT JOIN accounts a ON a.account_id=e.actor_account_id WHERE ($1='' OR e.action ILIKE '%'||$1||'%' OR e.resource_id ILIKE '%'||$1||'%' OR a.display_name ILIKE '%'||$1||'%')`
	arguments := []any{query}
	if page.GetCursor() != "" {
		at, id, err := decodeAuditCursor(page.GetCursor())
		if err != nil {
			return nil, "", err
		}
		statement += ` AND (e.occurred_at,e.audit_id)<($2,$3::uuid)`
		arguments = append(arguments, at, id)
	}
	statement += ` ORDER BY e.occurred_at DESC,e.audit_id DESC LIMIT $` + strconv.Itoa(len(arguments)+1)
	arguments = append(arguments, limit+1)
	rows, err := database.pool.Query(ctx, statement, arguments...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	values := make([]*cloudv1.OperatorAuditEvent, 0, limit+1)
	for rows.Next() {
		value := &cloudv1.OperatorAuditEvent{}
		var at time.Time
		if err := rows.Scan(&value.AuditId, &value.ActorAccountId, &value.ActorDisplayName, &value.Action, &value.ResourceType, &value.ResourceId, &value.Reason, &value.Result, &value.CorrelationId, &at); err != nil {
			return nil, "", err
		}
		value.OccurredAt = timestamppb.New(at)
		values = append(values, value)
	}
	next := ""
	if len(values) > limit {
		next = encodeAuditCursor(values[limit-1].GetOccurredAt().AsTime(), values[limit-1].GetAuditId())
		values = values[:limit]
	}
	return values, next, rows.Err()
}

func encodeAuditCursor(at time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(at.UTC().Format(time.RFC3339Nano) + "\n" + id))
}

func decodeAuditCursor(value string) (time.Time, string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return time.Time{}, "", errors.New("invalid audit cursor")
	}
	parts := strings.SplitN(string(raw), "\n", 2)
	if len(parts) != 2 {
		return time.Time{}, "", errors.New("invalid audit cursor")
	}
	at, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, "", errors.New("invalid audit cursor")
	}
	if _, err := uuid.Parse(parts[1]); err != nil {
		return time.Time{}, "", errors.New("invalid audit cursor")
	}
	return at.UTC(), parts[1], nil
}

// SetAccountState CAS 更新账号；禁用时同事务撤销全部 session 并写审计。
func (database *Database) SetAccountState(ctx context.Context, request *cloudv1.SetAccountStateRequest, actorID string, now time.Time) (*cloudv1.AccountProfile, error) {
	tx, err := database.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result, err := tx.Exec(ctx, `UPDATE accounts SET state=$1,revision=revision+1,updated_at=$2 WHERE account_id=$3 AND revision=$4`, accountStateName(request.GetState()), now, request.GetAccountId(), request.GetExpectedRevision())
	if err != nil {
		return nil, err
	}
	if result.RowsAffected() != 1 {
		return nil, commerce.ErrCommerceConflict
	}
	if request.GetState() == cloudv1.AccountState_ACCOUNT_STATE_DISABLED {
		if _, err := tx.Exec(ctx, `UPDATE account_sessions SET revoked_at=$1,revision=revision+1 WHERE account_id=$2 AND revoked_at IS NULL`, now, request.GetAccountId()); err != nil {
			return nil, err
		}
	}
	if err := insertOperatorAudit(ctx, tx, actorID, "account.state", "account", request.GetAccountId(), request.GetReason(), "applied", now); err != nil {
		return nil, err
	}
	record, err := scanAccountRecord(tx.QueryRow(ctx, accountSelect+` WHERE a.account_id=$1`, request.GetAccountId()))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return record.Profile, nil
}

// SetAccountRole 原子添加或删除运营角色；禁止操作者移除自己的 admin 角色。
func (database *Database) SetAccountRole(ctx context.Context, request *cloudv1.SetAccountRoleRequest, actorID string, now time.Time) ([]cloudv1.AccountRole, error) {
	if request.GetAccountId() == actorID && request.GetRole() == cloudv1.AccountRole_ACCOUNT_ROLE_ADMIN && !request.GetEnabled() {
		return nil, account.ErrAccountConflict
	}
	tx, err := database.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	role := accountRoleName(request.GetRole())
	if request.GetEnabled() {
		_, err = tx.Exec(ctx, `INSERT INTO account_roles(account_id,role,created_at) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, request.GetAccountId(), role, now)
	} else {
		_, err = tx.Exec(ctx, `DELETE FROM account_roles WHERE account_id=$1 AND role=$2`, request.GetAccountId(), role)
	}
	if err != nil {
		return nil, err
	}
	if err := insertOperatorAudit(ctx, tx, actorID, "account.role", "account", request.GetAccountId(), request.GetReason(), "applied", now); err != nil {
		return nil, err
	}
	record, err := scanAccountRecord(tx.QueryRow(ctx, accountSelect+` WHERE a.account_id=$1`, request.GetAccountId()))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return record.Roles, nil
}

// AuditRuntimeCommand 持久记录实时命令结果，不提供命令重放来源。
func (database *Database) AuditRuntimeCommand(ctx context.Context, actorID, action, resourceID, reason string, result cloudv1.RuntimeCommandResult, now time.Time) error {
	tx, err := database.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := insertOperatorAudit(ctx, tx, actorID, action, strings.TrimSuffix(action, ".disconnect"), resourceID, reason, result.String(), now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// RelayBytesCurrentPeriod 返回当前自然月已提交总字节。
func (database *Database) RelayBytesCurrentPeriod(ctx context.Context, now time.Time) (uint64, error) {
	var value uint64
	err := database.pool.QueryRow(ctx, `SELECT coalesce(sum(relay_ingress_bytes+relay_egress_bytes),0) FROM usage_periods WHERE period_start<=$1 AND period_end>$1`, now).Scan(&value)
	return value, err
}

func pageLimit(page *cloudv1.PageRequest) int {
	if page == nil || page.GetPageSize() == 0 {
		return 50
	}
	if page.GetPageSize() > 200 {
		return 200
	}
	return int(page.GetPageSize())
}
