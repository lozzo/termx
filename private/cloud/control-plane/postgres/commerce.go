package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/muxvia/muxvia/private/cloud/control-plane/commerce"
	"github.com/muxvia/muxvia/private/cloud/control-plane/promotion"
	"github.com/muxvia/muxvia/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

// CreateAccount 原子保存账号、首个 session、默认 Subscription、Entitlement 和审计。
func (store *Store) CreateAccount(ctx context.Context, account commerce.AccountRecord, session commerce.SessionRecord, subscription *cloudpb.SubscriptionProjection, entitlement *cloudpb.EntitlementProjection, audit *cloudpb.CommerceAuditProjection) error {
	if account.Projection == nil || subscription == nil || entitlement == nil || audit == nil {
		return commerce.ErrConflict
	}
	accountBody, err := marshal(account.Projection)
	if err != nil {
		return err
	}
	subscriptionBody, err := marshal(subscription)
	if err != nil {
		return err
	}
	entitlementBody, err := marshal(entitlement)
	if err != nil {
		return err
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = execContext(ctx, tx, `INSERT INTO commerce_accounts(account_id,email,projection,password_hash,auth_revision,operator_role) VALUES(?,?,?,?,?,?)`, account.Projection.GetAccountId(), account.Projection.GetEmail(), accountBody, append([]byte(nil), account.PasswordHash...), account.Projection.GetAuthRevision(), account.OperatorRole); err != nil {
		return conflict(err)
	}
	if err = insertSession(ctx, tx, session); err != nil {
		return conflict(err)
	}
	if _, err = execContext(ctx, tx, `INSERT INTO commerce_subscriptions(account_id,revision,projection) VALUES(?,?,?)`, subscription.GetAccountId(), subscription.GetRevision(), subscriptionBody); err != nil {
		return conflict(err)
	}
	if _, err = execContext(ctx, tx, `INSERT INTO commerce_entitlements(account_id,projection) VALUES(?,?)`, entitlement.GetAccountId(), entitlementBody); err != nil {
		return conflict(err)
	}
	if err = insertCommerceAudit(ctx, tx, audit); err != nil {
		return conflict(err)
	}
	return tx.Commit()
}

// AccountByEmail 按归一化邮箱读取账号和密码 verifier。
func (store *Store) AccountByEmail(ctx context.Context, email string) (commerce.AccountRecord, error) {
	return scanAccount(queryRowContext(ctx, store.db, `SELECT projection,password_hash,operator_role FROM commerce_accounts WHERE email=?`, email))
}

// Account 按账号 ID 读取账号和密码 verifier。
func (store *Store) Account(ctx context.Context, accountID string) (commerce.AccountRecord, error) {
	return scanAccount(queryRowContext(ctx, store.db, `SELECT projection,password_hash,operator_role FROM commerce_accounts WHERE account_id=?`, accountID))
}

// Accounts 返回 operator 使用的有界账号记录，按 account_id 稳定排序。
func (store *Store) Accounts(ctx context.Context, limit int) ([]commerce.AccountRecord, error) {
	rows, err := queryContext(ctx, store.db, `SELECT projection,password_hash,operator_role FROM commerce_accounts ORDER BY account_id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []commerce.AccountRecord
	for rows.Next() {
		var body, passwordHash []byte
		var operatorRole commerce.OperatorRole
		if err := rows.Scan(&body, &passwordHash, &operatorRole); err != nil {
			return nil, err
		}
		record := commerce.AccountRecord{Projection: &cloudpb.AccountProjection{}, PasswordHash: append([]byte(nil), passwordHash...), OperatorRole: operatorRole}
		if err := proto.Unmarshal(body, record.Projection); err != nil {
			return nil, err
		}
		result = append(result, record)
	}
	return result, rows.Err()
}

// SetOperatorRole 原子更新账号的后端运营角色；账号 Session 本身不携带角色。
func (store *Store) SetOperatorRole(ctx context.Context, accountID string, role commerce.OperatorRole) error {
	if accountID == "" || role > commerce.OperatorRoleAdmin {
		return commerce.ErrConflict
	}
	result, err := execContext(ctx, store.db, `UPDATE commerce_accounts SET operator_role=? WHERE account_id=?`, role, accountID)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return commerce.ErrNotFound
	}
	return nil
}

// Sessions 返回账号 session 元数据；token hash 只用于 Store 内部鉴权，不离开本方法。
func (store *Store) Sessions(ctx context.Context, accountID string, includeRevoked bool, limit int) ([]commerce.SessionRecord, error) {
	rows, err := queryContext(ctx, store.db, `SELECT session_id,account_id,client_device_id,access_hash,refresh_hash,access_expires_at,refresh_expires_at,revision,revoked FROM commerce_sessions WHERE account_id=? AND (revoked=0 OR ?=1) ORDER BY session_id LIMIT ?`, accountID, boolInt(includeRevoked), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []commerce.SessionRecord
	for rows.Next() {
		record, scanErr := scanSession(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, record)
	}
	return result, rows.Err()
}

// OperatorAudits 读取目标账号的持久运营审计；projection 是该表的唯一展示真值。
func (store *Store) OperatorAudits(ctx context.Context, accountID string, limit int) ([]*cloudpb.OperatorMutationAuditProjection, error) {
	rows, err := queryContext(ctx, store.db, `SELECT projection FROM operator_mutation_audit WHERE account_id=? ORDER BY occurred_at DESC,audit_id DESC LIMIT ?`, accountID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*cloudpb.OperatorMutationAuditProjection
	for rows.Next() {
		var body []byte
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		value := &cloudpb.OperatorMutationAuditProjection{}
		if err := proto.Unmarshal(body, value); err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

// PutSession 原子保存新的登录 session 和审计。
func (store *Store) PutSession(ctx context.Context, session commerce.SessionRecord, audit *cloudpb.CommerceAuditProjection) error {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := insertSession(ctx, tx, session); err != nil {
		return conflict(err)
	}
	if err := insertCommerceAudit(ctx, tx, audit); err != nil {
		return err
	}
	return tx.Commit()
}

// ReplaceDeviceSession 原子撤销同一安装设备的旧 Cloud session，再写入新 session 与审计。
// client_device_id 是设备维度真值；注册码和 session_id 只描述一次登录事务。
func (store *Store) ReplaceDeviceSession(ctx context.Context, session commerce.SessionRecord, audit *cloudpb.CommerceAuditProjection) error {
	if session.ClientDeviceID == "" || session.AccountID == "" {
		return commerce.ErrConflict
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = execContext(ctx, tx, `UPDATE commerce_sessions SET revoked=1 WHERE client_device_id=? AND revoked=0`, session.ClientDeviceID); err != nil {
		return err
	}
	if err = insertSession(ctx, tx, session); err != nil {
		return conflict(err)
	}
	if err = insertCommerceAudit(ctx, tx, audit); err != nil {
		return err
	}
	return tx.Commit()
}

// SessionByAccessHash 读取短期 access token 对应的 session。
func (store *Store) SessionByAccessHash(ctx context.Context, hash [sha256.Size]byte) (commerce.SessionRecord, error) {
	return scanSession(queryRowContext(ctx, store.db, `SELECT session_id,account_id,client_device_id,access_hash,refresh_hash,access_expires_at,refresh_expires_at,revision,revoked FROM commerce_sessions WHERE access_hash=?`, hash[:]))
}

// SessionByRefreshHash 读取 refresh token 对应的 session。
func (store *Store) SessionByRefreshHash(ctx context.Context, hash [sha256.Size]byte) (commerce.SessionRecord, error) {
	return scanSession(queryRowContext(ctx, store.db, `SELECT session_id,account_id,client_device_id,access_hash,refresh_hash,access_expires_at,refresh_expires_at,revision,revoked FROM commerce_sessions WHERE refresh_hash=?`, hash[:]))
}

// RotateSession 原子撤销旧 refresh session 并创建下一 revision。
func (store *Store) RotateSession(ctx context.Context, previousSessionID string, next commerce.SessionRecord, audit *cloudpb.CommerceAuditProjection) error {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := execContext(ctx, tx, `UPDATE commerce_sessions SET revoked=1 WHERE session_id=? AND account_id=? AND revoked=0 AND revision=?`, previousSessionID, next.AccountID, next.Revision-1)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return commerce.ErrConflict
	}
	if err := insertSession(ctx, tx, next); err != nil {
		return conflict(err)
	}
	if err := insertCommerceAudit(ctx, tx, audit); err != nil {
		return err
	}
	return tx.Commit()
}

// RevokeSession 撤销精确 session 或账号全部 session，并持久审计。
func (store *Store) RevokeSession(ctx context.Context, accountID, sessionID string, all bool, audit *cloudpb.CommerceAuditProjection) error {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var result sql.Result
	if all {
		result, err = execContext(ctx, tx, `UPDATE commerce_sessions SET revoked=1 WHERE account_id=? AND revoked=0`, accountID)
	} else {
		result, err = execContext(ctx, tx, `UPDATE commerce_sessions SET revoked=1 WHERE account_id=? AND session_id=? AND revoked=0`, accountID, sessionID)
	}
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed == 0 {
		return commerce.ErrNotFound
	}
	if err := insertCommerceAudit(ctx, tx, audit); err != nil {
		return err
	}
	return tx.Commit()
}

// RevokeOperatorSessions 原子执行运营 session revoke 与 operator audit。
// request_id 精确重放返回成功，任何 actor/reason/target 冲突都 fail closed。
func (store *Store) RevokeOperatorSessions(ctx context.Context, accountID, sessionID string, expectedRevision uint64, all bool, audit *cloudpb.OperatorMutationAuditProjection) error {
	if audit == nil || accountID == "" || audit.GetAccountId() != accountID || audit.GetAuditId() == "" || audit.GetRequestId() == "" {
		return commerce.ErrConflict
	}
	body, err := marshal(audit)
	if err != nil {
		return err
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var existingBody []byte
	existingErr := queryRowContext(ctx, tx, `SELECT projection FROM operator_mutation_audit WHERE audit_id=? FOR UPDATE`, audit.GetAuditId()).Scan(&existingBody)
	if existingErr == nil {
		existing := &cloudpb.OperatorMutationAuditProjection{}
		if proto.Unmarshal(existingBody, existing) != nil || existing.GetActorId() != audit.GetActorId() || existing.GetAction() != audit.GetAction() || existing.GetResourceId() != audit.GetResourceId() || existing.GetAccountId() != audit.GetAccountId() || existing.GetReason() != audit.GetReason() || existing.GetRequestId() != audit.GetRequestId() || existing.GetBeforeRevision() != audit.GetBeforeRevision() {
			return commerce.ErrConflict
		}
		return tx.Commit()
	}
	if !errors.Is(existingErr, sql.ErrNoRows) {
		return existingErr
	}
	var result sql.Result
	if all {
		result, err = execContext(ctx, tx, `UPDATE commerce_sessions SET revoked=1 WHERE account_id=? AND revoked=0`, accountID)
	} else {
		result, err = execContext(ctx, tx, `UPDATE commerce_sessions SET revoked=1 WHERE account_id=? AND session_id=? AND revision=? AND revoked=0`, accountID, sessionID, expectedRevision)
	}
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed == 0 {
		return commerce.ErrNotFound
	}
	if _, err := execContext(ctx, tx, `INSERT INTO operator_mutation_audit(audit_id,account_id,occurred_at,projection) VALUES(?,?,?,?)`, audit.GetAuditId(), audit.GetAccountId(), audit.GetOccurredAtUnixMillis(), body); err != nil {
		return conflict(err)
	}
	return tx.Commit()
}

// ChangePassword 原子替换账号 verifier、撤销旧 session、创建新 session 并记录审计。
func (store *Store) ChangePassword(ctx context.Context, account commerce.AccountRecord, session commerce.SessionRecord, audit *cloudpb.CommerceAuditProjection) error {
	if account.Projection == nil || account.Projection.GetAuthRevision() < 2 || session.AccountID != account.Projection.GetAccountId() {
		return commerce.ErrConflict
	}
	body, err := marshal(account.Projection)
	if err != nil {
		return err
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	updated, err := execContext(ctx, tx, `UPDATE commerce_accounts SET projection=?,password_hash=?,auth_revision=? WHERE account_id=? AND auth_revision=?`, body, append([]byte(nil), account.PasswordHash...), account.Projection.GetAuthRevision(), account.Projection.GetAccountId(), account.Projection.GetAuthRevision()-1)
	if err != nil {
		return err
	}
	if changed, _ := updated.RowsAffected(); changed != 1 {
		return commerce.ErrConflict
	}
	if _, err = execContext(ctx, tx, `UPDATE commerce_sessions SET revoked=1 WHERE account_id=? AND revoked=0`, account.Projection.GetAccountId()); err != nil {
		return err
	}
	if err = insertSession(ctx, tx, session); err != nil {
		return conflict(err)
	}
	if err = insertCommerceAudit(ctx, tx, audit); err != nil {
		return err
	}
	return tx.Commit()
}

// CreateOrder 原子保存 pending order 和创建审计。
func (store *Store) CreateOrder(ctx context.Context, order *cloudpb.OrderProjection, audit *cloudpb.CommerceAuditProjection) error {
	if order == nil || order.GetStatus() != cloudpb.OrderStatus_ORDER_STATUS_PENDING {
		return commerce.ErrConflict
	}
	body, err := marshal(order)
	if err != nil {
		return err
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = execContext(ctx, tx, `INSERT INTO commerce_orders(order_id,account_id,revision,projection) VALUES(?,?,?,?)`, order.GetOrderId(), order.GetAccountId(), order.GetRevision(), body); err != nil {
		return conflict(err)
	}
	if err = insertCommerceAudit(ctx, tx, audit); err != nil {
		return err
	}
	return tx.Commit()
}

// Order 读取持久订单投影。
func (store *Store) Order(ctx context.Context, orderID string) (*cloudpb.OrderProjection, error) {
	var body []byte
	if err := queryRowContext(ctx, store.db, `SELECT projection FROM commerce_orders WHERE order_id=?`, orderID).Scan(&body); err != nil {
		return nil, notFound(err)
	}
	value := &cloudpb.OrderProjection{}
	return value, proto.Unmarshal(body, value)
}

// CreatePaymentAttempt 原子保存 pending provider attempt 和创建审计。
func (store *Store) CreatePaymentAttempt(ctx context.Context, attempt *cloudpb.PaymentAttemptProjection, audit *cloudpb.CommerceAuditProjection) error {
	if attempt == nil || attempt.GetStatus() != cloudpb.PaymentAttemptStatus_PAYMENT_ATTEMPT_STATUS_PENDING || attempt.GetRevision() != 1 {
		return commerce.ErrConflict
	}
	body, err := marshal(attempt)
	if err != nil {
		return err
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = execContext(ctx, tx, `INSERT INTO commerce_payment_attempts(payment_attempt_id,order_id,account_id,revision,projection,provider,status,provider_reference,provider_transaction_reference,provider_subscription_reference,reconcile_after,reconcile_deadline,provider_created_at,provider_updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, attempt.GetPaymentAttemptId(), attempt.GetOrderId(), attempt.GetAccountId(), attempt.GetRevision(), body, attempt.GetProvider(), attempt.GetStatus(), attempt.GetProviderReference(), attempt.GetProviderTransactionReference(), attempt.GetProviderSubscriptionReference(), attempt.GetReconcileAfterUnixMillis(), attempt.GetReconcileDeadlineUnixMillis(), attempt.GetCreatedAtUnixMillis(), attempt.GetUpdatedAtUnixMillis()); err != nil {
		return conflict(err)
	}
	if err = insertCommerceAudit(ctx, tx, audit); err != nil {
		return err
	}
	return tx.Commit()
}

// UpdatePaymentAttempt 以 revision CAS 同步 provider 索引列与 Proto projection。
func (store *Store) UpdatePaymentAttempt(ctx context.Context, attempt *cloudpb.PaymentAttemptProjection, expectedRevision uint64) error {
	if attempt == nil || expectedRevision == 0 || attempt.GetRevision() != expectedRevision+1 {
		return commerce.ErrConflict
	}
	body, err := marshal(attempt)
	if err != nil {
		return err
	}
	result, err := execContext(ctx, store.db, `UPDATE commerce_payment_attempts SET revision=?,projection=?,provider=?,status=?,provider_reference=?,provider_transaction_reference=?,provider_subscription_reference=?,reconcile_after=?,reconcile_deadline=?,provider_updated_at=? WHERE payment_attempt_id=? AND revision=?`, attempt.GetRevision(), body, attempt.GetProvider(), attempt.GetStatus(), attempt.GetProviderReference(), attempt.GetProviderTransactionReference(), attempt.GetProviderSubscriptionReference(), attempt.GetReconcileAfterUnixMillis(), attempt.GetReconcileDeadlineUnixMillis(), attempt.GetUpdatedAtUnixMillis(), attempt.GetPaymentAttemptId(), expectedRevision)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return commerce.ErrConflict
	}
	return nil
}

// PaymentAttempt 读取一次持久 provider 尝试。
func (store *Store) PaymentAttempt(ctx context.Context, attemptID string) (*cloudpb.PaymentAttemptProjection, error) {
	var body []byte
	if err := queryRowContext(ctx, store.db, `SELECT projection FROM commerce_payment_attempts WHERE payment_attempt_id=?`, attemptID).Scan(&body); err != nil {
		return nil, notFound(err)
	}
	value := &cloudpb.PaymentAttemptProjection{}
	return value, proto.Unmarshal(body, value)
}

// PendingPaymentAttempts 返回 PostgreSQL 中到达轮询时间的 pending checkout 与 active provider subscription。
func (store *Store) PendingPaymentAttempts(ctx context.Context, provider string, before time.Time, limit int) ([]*cloudpb.PaymentAttemptProjection, error) {
	rows, err := queryContext(ctx, store.db, `SELECT projection FROM commerce_payment_attempts WHERE provider=? AND reconcile_after>0 AND reconcile_after<=? AND (status=? OR (status=? AND provider_subscription_reference<>'')) ORDER BY reconcile_after,payment_attempt_id LIMIT ?`, provider, before.UnixMilli(), cloudpb.PaymentAttemptStatus_PAYMENT_ATTEMPT_STATUS_PENDING, cloudpb.PaymentAttemptStatus_PAYMENT_ATTEMPT_STATUS_SUCCEEDED, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]*cloudpb.PaymentAttemptProjection, 0)
	for rows.Next() {
		var body []byte
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		value := &cloudpb.PaymentAttemptProjection{}
		if err := proto.Unmarshal(body, value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

// PaymentAttemptByProviderReference 精确解析一个 provider resource 到唯一 payment attempt。
func (store *Store) PaymentAttemptByProviderReference(ctx context.Context, provider, reference string) (*cloudpb.PaymentAttemptProjection, error) {
	var body []byte
	err := queryRowContext(ctx, store.db, `SELECT projection FROM commerce_payment_attempts WHERE provider=? AND (provider_reference=? OR provider_transaction_reference=? OR provider_subscription_reference=?) ORDER BY provider_updated_at DESC LIMIT 1`, provider, reference, reference, reference).Scan(&body)
	if err != nil {
		return nil, notFound(err)
	}
	value := &cloudpb.PaymentAttemptProjection{}
	return value, proto.Unmarshal(body, value)
}

// RecordPaymentEvent 先持久化 RECEIVED journal；相同 ID 返回已有 digest、状态和原提交结果。
func (store *Store) RecordPaymentEvent(ctx context.Context, record commerce.PaymentEventRecord) (commerce.PaymentEventRecord, bool, error) {
	if record.Event == nil || record.Event.GetProviderEventId() == "" {
		return commerce.PaymentEventRecord{}, false, commerce.ErrConflict
	}
	eventBody, err := marshal(record.Event)
	if err != nil {
		return commerce.PaymentEventRecord{}, false, err
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return commerce.PaymentEventRecord{}, false, err
	}
	defer tx.Rollback()
	result, err := execContext(ctx, tx, `INSERT INTO commerce_payment_events(provider_event_id,account_id,digest,event,state,result) VALUES(?,?,?,?,?,NULL) ON CONFLICT(provider_event_id) DO NOTHING`, record.Event.GetProviderEventId(), record.Event.GetAccountId(), record.Digest[:], eventBody, record.State)
	if err != nil {
		return commerce.PaymentEventRecord{}, false, err
	}
	inserted, _ := result.RowsAffected()
	stored, err := scanPaymentEvent(queryRowContext(ctx, tx, `SELECT digest,event,state,result FROM commerce_payment_events WHERE provider_event_id=?`, record.Event.GetProviderEventId()))
	if err != nil {
		return commerce.PaymentEventRecord{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return commerce.PaymentEventRecord{}, false, err
	}
	return stored, inserted == 0, nil
}

// RejectPaymentEvent 把已持久 RECEIVED 事件终结为 REJECTED，并记录拒绝原因审计。
func (store *Store) RejectPaymentEvent(ctx context.Context, eventID string, audit *cloudpb.CommerceAuditProjection) error {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	updated, err := execContext(ctx, tx, `UPDATE commerce_payment_events SET state=? WHERE provider_event_id=? AND state=?`, cloudpb.PaymentEventState_PAYMENT_EVENT_STATE_REJECTED, eventID, cloudpb.PaymentEventState_PAYMENT_EVENT_STATE_RECEIVED)
	if err != nil {
		return err
	}
	if changed, _ := updated.RowsAffected(); changed != 1 {
		return commerce.ErrConflict
	}
	if err := insertCommerceAudit(ctx, tx, audit); err != nil {
		return err
	}
	return tx.Commit()
}

func settlePromotionRedemption(ctx context.Context, tx *sql.Tx, event *cloudpb.NormalizedPaymentEvent, order *cloudpb.OrderProjection) error {
	if order.GetPromotion() == nil {
		return nil
	}
	var body []byte
	var state cloudpb.PromotionRedemptionState
	if err := queryRowContext(ctx, tx, `SELECT state,projection FROM promotion_redemptions WHERE order_id=? FOR UPDATE`, order.GetOrderId()).Scan(&state, &body); err != nil {
		return promotion.ErrConflict
	}
	value := &cloudpb.PromotionRedemptionProjection{}
	if err := proto.Unmarshal(body, value); err != nil {
		return err
	}
	var next cloudpb.PromotionRedemptionState
	switch event.GetEventType() {
	case cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_SUCCEEDED:
		if state != cloudpb.PromotionRedemptionState_PROMOTION_REDEMPTION_STATE_RESERVED || value.GetExpiresAtUnixMillis() <= event.GetOccurredAtUnixMillis() {
			return promotion.ErrConflict
		}
		next = cloudpb.PromotionRedemptionState_PROMOTION_REDEMPTION_STATE_REDEEMED
	case cloudpb.PaymentEventType_PAYMENT_EVENT_TYPE_FAILED:
		if state != cloudpb.PromotionRedemptionState_PROMOTION_REDEMPTION_STATE_RESERVED {
			return promotion.ErrConflict
		}
		next = cloudpb.PromotionRedemptionState_PROMOTION_REDEMPTION_STATE_RELEASED
	default:
		return nil
	}
	value.State, value.UpdatedAtUnixMillis, value.Revision = next, event.GetOccurredAtUnixMillis(), value.GetRevision()+1
	nextBody, err := marshal(value)
	if err != nil {
		return err
	}
	result, err := execContext(ctx, tx, `UPDATE promotion_redemptions SET state=?,updated_at=?,revision=?,projection=? WHERE redemption_id=? AND state=? AND revision=?`, value.GetState(), value.GetUpdatedAtUnixMillis(), value.GetRevision(), nextBody, value.GetRedemptionId(), state, value.GetRevision()-1)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return promotion.ErrConflict
	}
	return nil
}

// CommitPaymentEvent 原子提交订单、Subscription、Entitlement、journal 结果和审计。
func (store *Store) CommitPaymentEvent(ctx context.Context, eventID string, result *cloudpb.ApplyPaymentEventResponse, entitlement *cloudpb.EntitlementProjection, audit *cloudpb.CommerceAuditProjection) error {
	if result == nil || result.GetOrder() == nil || result.GetSubscription() == nil || result.GetPaymentAttempt() == nil || entitlement == nil || result.GetEventState() != cloudpb.PaymentEventState_PAYMENT_EVENT_STATE_APPLIED {
		return commerce.ErrConflict
	}
	resultBody, err := marshal(result)
	if err != nil {
		return err
	}
	orderBody, err := marshal(result.GetOrder())
	if err != nil {
		return err
	}
	subscriptionBody, err := marshal(result.GetSubscription())
	if err != nil {
		return err
	}
	entitlementBody, err := marshal(entitlement)
	if err != nil {
		return err
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stored, err := scanPaymentEvent(queryRowContext(ctx, tx, `SELECT digest,event,state,result FROM commerce_payment_events WHERE provider_event_id=?`, eventID))
	if err != nil {
		return err
	}
	if stored.State == cloudpb.PaymentEventState_PAYMENT_EVENT_STATE_APPLIED {
		if stored.Result != nil && proto.Equal(stored.Result, result) {
			return tx.Commit()
		}
		return commerce.ErrConflict
	}
	if stored.State != cloudpb.PaymentEventState_PAYMENT_EVENT_STATE_RECEIVED {
		return commerce.ErrConflict
	}
	order := result.GetOrder()
	updated, err := execContext(ctx, tx, `UPDATE commerce_orders SET revision=?,projection=? WHERE order_id=? AND account_id=? AND revision=?`, order.GetRevision(), orderBody, order.GetOrderId(), order.GetAccountId(), order.GetRevision()-1)
	if err != nil {
		return err
	}
	if changed, _ := updated.RowsAffected(); changed != 1 {
		return commerce.ErrConflict
	}
	if err := settlePromotionRedemption(ctx, tx, stored.Event, order); err != nil {
		return err
	}
	attempt := result.GetPaymentAttempt()
	var currentAttemptBody []byte
	var currentAttemptRevision uint64
	if err := queryRowContext(ctx, tx, `SELECT revision,projection FROM commerce_payment_attempts WHERE payment_attempt_id=? AND order_id=? AND account_id=?`, attempt.GetPaymentAttemptId(), attempt.GetOrderId(), attempt.GetAccountId()).Scan(&currentAttemptRevision, &currentAttemptBody); err != nil {
		return notFound(err)
	}
	if attempt.GetRevision() == currentAttemptRevision+1 {
		attemptBody, err := marshal(attempt)
		if err != nil {
			return err
		}
		updated, err = execContext(ctx, tx, `UPDATE commerce_payment_attempts SET revision=?,projection=?,status=?,provider_reference=?,provider_transaction_reference=?,provider_subscription_reference=?,reconcile_after=?,reconcile_deadline=?,provider_updated_at=? WHERE payment_attempt_id=? AND revision=?`, attempt.GetRevision(), attemptBody, attempt.GetStatus(), attempt.GetProviderReference(), attempt.GetProviderTransactionReference(), attempt.GetProviderSubscriptionReference(), attempt.GetReconcileAfterUnixMillis(), attempt.GetReconcileDeadlineUnixMillis(), attempt.GetUpdatedAtUnixMillis(), attempt.GetPaymentAttemptId(), currentAttemptRevision)
		if err != nil {
			return err
		}
		if changed, _ := updated.RowsAffected(); changed != 1 {
			return commerce.ErrConflict
		}
	} else if attempt.GetRevision() == currentAttemptRevision {
		currentAttempt := &cloudpb.PaymentAttemptProjection{}
		if err := proto.Unmarshal(currentAttemptBody, currentAttempt); err != nil {
			return err
		}
		if !proto.Equal(currentAttempt, attempt) {
			return commerce.ErrConflict
		}
	} else {
		return commerce.ErrConflict
	}
	subscription := result.GetSubscription()
	updated, err = execContext(ctx, tx, `UPDATE commerce_subscriptions SET revision=?,projection=? WHERE account_id=? AND revision=?`, subscription.GetRevision(), subscriptionBody, subscription.GetAccountId(), subscription.GetRevision()-1)
	if err != nil {
		return err
	}
	if changed, _ := updated.RowsAffected(); changed != 1 {
		return commerce.ErrConflict
	}
	if _, err = execContext(ctx, tx, `INSERT INTO commerce_entitlements(account_id,projection) VALUES(?,?) ON CONFLICT(account_id) DO UPDATE SET projection=excluded.projection`, entitlement.GetAccountId(), entitlementBody); err != nil {
		return err
	}
	updated, err = execContext(ctx, tx, `UPDATE commerce_payment_events SET state=?,result=? WHERE provider_event_id=? AND state=?`, cloudpb.PaymentEventState_PAYMENT_EVENT_STATE_APPLIED, resultBody, eventID, cloudpb.PaymentEventState_PAYMENT_EVENT_STATE_RECEIVED)
	if err != nil {
		return err
	}
	if changed, _ := updated.RowsAffected(); changed != 1 {
		return commerce.ErrConflict
	}
	if err := insertCommerceAudit(ctx, tx, audit); err != nil {
		return err
	}
	return tx.Commit()
}

// CommitSubscriptionTransition 使用 revision CAS 原子更新 Subscription、Entitlement 和审计。
func (store *Store) CommitSubscriptionTransition(ctx context.Context, subscription *cloudpb.SubscriptionProjection, entitlement *cloudpb.EntitlementProjection, audit *cloudpb.CommerceAuditProjection) error {
	if subscription == nil || entitlement == nil || subscription.GetRevision() < 2 {
		return commerce.ErrConflict
	}
	subscriptionBody, err := marshal(subscription)
	if err != nil {
		return err
	}
	entitlementBody, err := marshal(entitlement)
	if err != nil {
		return err
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	updated, err := execContext(ctx, tx, `UPDATE commerce_subscriptions SET revision=?,projection=? WHERE account_id=? AND revision=?`, subscription.GetRevision(), subscriptionBody, subscription.GetAccountId(), subscription.GetRevision()-1)
	if err != nil {
		return err
	}
	if changed, _ := updated.RowsAffected(); changed != 1 {
		return commerce.ErrConflict
	}
	if _, err = execContext(ctx, tx, `INSERT INTO commerce_entitlements(account_id,projection) VALUES(?,?) ON CONFLICT(account_id) DO UPDATE SET projection=excluded.projection`, entitlement.GetAccountId(), entitlementBody); err != nil {
		return err
	}
	if err := insertCommerceAudit(ctx, tx, audit); err != nil {
		return err
	}
	return tx.Commit()
}

// Subscription 返回账号当前持久商业真值。
func (store *Store) Subscription(ctx context.Context, accountID string) (*cloudpb.SubscriptionProjection, error) {
	value := &cloudpb.SubscriptionProjection{}
	return value, store.scanProjection(ctx, `SELECT projection FROM commerce_subscriptions WHERE account_id=?`, accountID, value)
}

// Entitlement 返回由当前 Subscription 归一化的准入投影。
func (store *Store) Entitlement(ctx context.Context, accountID string) (*cloudpb.EntitlementProjection, error) {
	value := &cloudpb.EntitlementProjection{}
	return value, store.scanProjection(ctx, `SELECT projection FROM commerce_entitlements WHERE account_id=?`, accountID, value)
}

// Orders 返回账号订单，按创建时间和 ID 稳定排序。
func (store *Store) Orders(ctx context.Context, accountID string) ([]*cloudpb.OrderProjection, error) {
	rows, err := queryContext(ctx, store.db, `SELECT projection FROM commerce_orders WHERE account_id=? ORDER BY order_id`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]*cloudpb.OrderProjection, 0)
	for rows.Next() {
		var body []byte
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		value := &cloudpb.OrderProjection{}
		if err := proto.Unmarshal(body, value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

// AllOrders 返回 operator 订单视图所需的全局订单投影；limit 是有界查询上限。
func (store *Store) AllOrders(ctx context.Context, limit int) ([]*cloudpb.OrderProjection, error) {
	return scanOrderRows(queryContext(ctx, store.db, `SELECT projection FROM commerce_orders ORDER BY order_id DESC LIMIT ?`, limit))
}

// Subscriptions 返回 operator 订阅视图所需的全局订阅投影；数据库 projection 仍是唯一真值。
func (store *Store) Subscriptions(ctx context.Context, limit int) ([]*cloudpb.SubscriptionProjection, error) {
	rows, err := queryContext(ctx, store.db, `SELECT projection FROM commerce_subscriptions ORDER BY account_id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]*cloudpb.SubscriptionProjection, 0)
	for rows.Next() {
		var body []byte
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		value := &cloudpb.SubscriptionProjection{}
		if err := proto.Unmarshal(body, value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func scanOrderRows(rows *sql.Rows, err error) ([]*cloudpb.OrderProjection, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]*cloudpb.OrderProjection, 0)
	for rows.Next() {
		var body []byte
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		value := &cloudpb.OrderProjection{}
		if err := proto.Unmarshal(body, value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

// PaymentAttempts 返回账号 provider 尝试，按 ID 稳定排序。
func (store *Store) PaymentAttempts(ctx context.Context, accountID string) ([]*cloudpb.PaymentAttemptProjection, error) {
	rows, err := queryContext(ctx, store.db, `SELECT projection FROM commerce_payment_attempts WHERE account_id=? ORDER BY payment_attempt_id`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]*cloudpb.PaymentAttemptProjection, 0)
	for rows.Next() {
		var body []byte
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		value := &cloudpb.PaymentAttemptProjection{}
		if err := proto.Unmarshal(body, value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

// PaymentEvents 返回账号隔离后的 provider journal 摘要。
func (store *Store) PaymentEvents(ctx context.Context, accountID string) ([]*cloudpb.PaymentEventProjection, error) {
	rows, err := queryContext(ctx, store.db, `SELECT event,state FROM commerce_payment_events WHERE account_id=? ORDER BY provider_event_id`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*cloudpb.PaymentEventProjection
	for rows.Next() {
		var body []byte
		var state cloudpb.PaymentEventState
		if err := rows.Scan(&body, &state); err != nil {
			return nil, err
		}
		event := &cloudpb.NormalizedPaymentEvent{}
		if err := proto.Unmarshal(body, event); err != nil {
			return nil, err
		}
		result = append(result, &cloudpb.PaymentEventProjection{Event: event, State: state})
	}
	return result, rows.Err()
}

// SubscriptionAdjustments 返回账号的人工订阅调整时间线。
func (store *Store) SubscriptionAdjustments(ctx context.Context, accountID string, limit int) ([]*cloudpb.SubscriptionAdjustmentProjection, error) {
	rows, err := queryContext(ctx, store.db, `SELECT projection FROM subscription_adjustments WHERE account_id=? ORDER BY resulting_subscription_revision DESC LIMIT ?`, accountID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]*cloudpb.SubscriptionAdjustmentProjection, 0)
	for rows.Next() {
		var body []byte
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		value := &cloudpb.SubscriptionAdjustmentProjection{}
		if err := proto.Unmarshal(body, value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

// CommitSubscriptionAdjustment 原子写 adjustment、CAS Subscription、重算 Entitlement 并审计。
func (store *Store) CommitSubscriptionAdjustment(ctx context.Context, adjustment *cloudpb.SubscriptionAdjustmentProjection, subscription *cloudpb.SubscriptionProjection, entitlement *cloudpb.EntitlementProjection, audit *cloudpb.OperatorMutationAuditProjection) error {
	if adjustment == nil || subscription == nil || entitlement == nil || audit == nil || adjustment.GetResultingSubscriptionRevision() != subscription.GetRevision() || subscription.GetRevision() != adjustment.GetExpectedSubscriptionRevision()+1 {
		return commerce.ErrConflict
	}
	adjustmentBody, err := marshal(adjustment)
	if err != nil {
		return err
	}
	subscriptionBody, err := marshal(subscription)
	if err != nil {
		return err
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := execContext(ctx, tx, `INSERT INTO subscription_adjustments(adjustment_id,account_id,request_id,resulting_subscription_revision,projection) VALUES(?,?,?,?,?)`, adjustment.GetAdjustmentId(), adjustment.GetAccountId(), adjustment.GetRequestId(), adjustment.GetResultingSubscriptionRevision(), adjustmentBody); err != nil {
		return commerce.ErrConflict
	}
	updated, err := execContext(ctx, tx, `UPDATE commerce_subscriptions SET revision=?,projection=? WHERE account_id=? AND revision=?`, subscription.GetRevision(), subscriptionBody, subscription.GetAccountId(), adjustment.GetExpectedSubscriptionRevision())
	if err != nil {
		return err
	}
	if changed, _ := updated.RowsAffected(); changed != 1 {
		return commerce.ErrConflict
	}
	if err := updateEntitlementAndOperatorAudit(ctx, tx, entitlement, audit); err != nil {
		return err
	}
	return tx.Commit()
}

// Audit 返回账号持久交易审计，按发生时间和 ID 稳定排序。
func (store *Store) Audit(ctx context.Context, accountID string) ([]*cloudpb.CommerceAuditProjection, error) {
	rows, err := queryContext(ctx, store.db, `SELECT projection FROM commerce_audit WHERE account_id=? ORDER BY occurred_at,audit_id`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]*cloudpb.CommerceAuditProjection, 0)
	for rows.Next() {
		var body []byte
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		value := &cloudpb.CommerceAuditProjection{}
		if err := proto.Unmarshal(body, value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

// RecordOperatorAudit 幂等保存一次不直接修改领域状态的运营审计。
// 同一 request_id 的 actor、动作、资源和原因一致时视为重放；首个 revision 边界保持不可变。
func (store *Store) RecordOperatorAudit(ctx context.Context, audit *cloudpb.OperatorMutationAuditProjection) error {
	if audit == nil || audit.GetAuditId() == "" || audit.GetAccountId() == "" || audit.GetRequestId() == "" {
		return commerce.ErrConflict
	}
	body, err := marshal(audit)
	if err != nil {
		return err
	}
	result, err := execContext(ctx, store.db, `INSERT INTO operator_mutation_audit(audit_id,account_id,occurred_at,projection) VALUES(?,?,?,?) ON CONFLICT (audit_id) DO NOTHING`, audit.GetAuditId(), audit.GetAccountId(), audit.GetOccurredAtUnixMillis(), body)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed == 1 {
		return nil
	}
	var existingBody []byte
	if err := queryRowContext(ctx, store.db, `SELECT projection FROM operator_mutation_audit WHERE audit_id=?`, audit.GetAuditId()).Scan(&existingBody); err != nil {
		return notFound(err)
	}
	existing := &cloudpb.OperatorMutationAuditProjection{}
	if err := proto.Unmarshal(existingBody, existing); err != nil || existing.GetActorId() != audit.GetActorId() || existing.GetAction() != audit.GetAction() || existing.GetResourceKind() != audit.GetResourceKind() || existing.GetResourceId() != audit.GetResourceId() || existing.GetAccountId() != audit.GetAccountId() || existing.GetReason() != audit.GetReason() || existing.GetRequestId() != audit.GetRequestId() {
		return commerce.ErrConflict
	}
	return nil
}

func (store *Store) scanProjection(ctx context.Context, query, id string, value proto.Message) error {
	var body []byte
	if err := queryRowContext(ctx, store.db, query, id).Scan(&body); err != nil {
		return notFound(err)
	}
	return proto.Unmarshal(body, value)
}

func insertSession(ctx context.Context, tx *sql.Tx, session commerce.SessionRecord) error {
	_, err := execContext(ctx, tx, `INSERT INTO commerce_sessions(session_id,account_id,client_device_id,access_hash,refresh_hash,access_expires_at,refresh_expires_at,revision,revoked) VALUES(?,?,?,?,?,?,?,?,?)`, session.SessionID, session.AccountID, session.ClientDeviceID, session.AccessTokenHash[:], session.RefreshTokenHash[:], session.AccessExpiresAt.UnixMilli(), session.RefreshExpiresAt.UnixMilli(), session.Revision, boolInt(session.Revoked))
	return err
}

func insertCommerceAudit(ctx context.Context, tx *sql.Tx, audit *cloudpb.CommerceAuditProjection) error {
	if audit == nil {
		return commerce.ErrConflict
	}
	body, err := marshal(audit)
	if err != nil {
		return err
	}
	_, err = execContext(ctx, tx, `INSERT INTO commerce_audit(audit_id,account_id,occurred_at,projection) VALUES(?,?,?,?)`, audit.GetAuditId(), audit.GetAccountId(), audit.GetOccurredAtUnixMillis(), body)
	return err
}

func scanAccount(row rowScanner) (commerce.AccountRecord, error) {
	var body, passwordHash []byte
	var operatorRole commerce.OperatorRole
	if err := row.Scan(&body, &passwordHash, &operatorRole); err != nil {
		return commerce.AccountRecord{}, notFound(err)
	}
	projection := &cloudpb.AccountProjection{}
	if err := proto.Unmarshal(body, projection); err != nil {
		return commerce.AccountRecord{}, err
	}
	return commerce.AccountRecord{Projection: projection, PasswordHash: append([]byte(nil), passwordHash...), OperatorRole: operatorRole}, nil
}

func scanSession(row rowScanner) (commerce.SessionRecord, error) {
	var record commerce.SessionRecord
	var accessHash, refreshHash []byte
	var accessExpiry, refreshExpiry int64
	var revoked int
	if err := row.Scan(&record.SessionID, &record.AccountID, &record.ClientDeviceID, &accessHash, &refreshHash, &accessExpiry, &refreshExpiry, &record.Revision, &revoked); err != nil {
		return commerce.SessionRecord{}, notFound(err)
	}
	if len(accessHash) != sha256.Size || len(refreshHash) != sha256.Size {
		return commerce.SessionRecord{}, fmt.Errorf("invalid commerce session token hash")
	}
	copy(record.AccessTokenHash[:], accessHash)
	copy(record.RefreshTokenHash[:], refreshHash)
	record.AccessExpiresAt = time.UnixMilli(accessExpiry).UTC()
	record.RefreshExpiresAt = time.UnixMilli(refreshExpiry).UTC()
	record.Revoked = revoked != 0
	return record, nil
}

func scanPaymentEvent(row rowScanner) (commerce.PaymentEventRecord, error) {
	var digest, eventBody, resultBody []byte
	var state cloudpb.PaymentEventState
	if err := row.Scan(&digest, &eventBody, &state, &resultBody); err != nil {
		return commerce.PaymentEventRecord{}, notFound(err)
	}
	if len(digest) != sha256.Size {
		return commerce.PaymentEventRecord{}, fmt.Errorf("invalid commerce payment digest")
	}
	record := commerce.PaymentEventRecord{Event: &cloudpb.NormalizedPaymentEvent{}, State: state}
	copy(record.Digest[:], digest)
	if err := proto.Unmarshal(eventBody, record.Event); err != nil {
		return commerce.PaymentEventRecord{}, err
	}
	if len(resultBody) > 0 {
		record.Result = &cloudpb.ApplyPaymentEventResponse{}
		if err := proto.Unmarshal(resultBody, record.Result); err != nil {
			return commerce.PaymentEventRecord{}, err
		}
	}
	return record, nil
}

func marshal(value proto.Message) ([]byte, error) {
	if value == nil {
		return nil, commerce.ErrConflict
	}
	return proto.MarshalOptions{Deterministic: true}.Marshal(value)
}

func notFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return commerce.ErrNotFound
	}
	return err
}

func conflict(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %v", commerce.ErrConflict, err)
}
