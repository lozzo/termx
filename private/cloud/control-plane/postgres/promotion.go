package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/muxvia/muxvia/private/cloud/control-plane/commerce"
	"github.com/muxvia/muxvia/private/cloud/control-plane/promotion"
	"github.com/muxvia/muxvia/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

// CreatePromotion 原子发布不可变优惠经济字段并写运营审计。
func (store *Store) CreatePromotion(ctx context.Context, value *cloudpb.PromotionProjection, audit *cloudpb.OperatorMutationAuditProjection) error {
	body, err := marshal(value)
	if err != nil || audit == nil {
		return promotion.ErrInvalid
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := execContext(ctx, tx, `INSERT INTO promotions(promotion_id,code,state,revision,effective_from,effective_until,projection) VALUES(?,?,?,?,?,?,?)`, value.GetPromotionId(), value.GetCode(), value.GetState(), value.GetRevision(), value.GetEffectiveFromUnixMillis(), value.GetEffectiveUntilUnixMillis(), body); err != nil {
		return promotion.ErrConflict
	}
	if err := updateEntitlementAndOperatorAudit(ctx, tx, nil, audit); err != nil {
		return err
	}
	return tx.Commit()
}

// DisablePromotion 只切换状态/revision，不修改历史经济字段。
func (store *Store) DisablePromotion(ctx context.Context, value *cloudpb.PromotionProjection, expected uint64, audit *cloudpb.OperatorMutationAuditProjection) error {
	body, err := marshal(value)
	if err != nil || audit == nil {
		return promotion.ErrInvalid
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := execContext(ctx, tx, `UPDATE promotions SET state=?,revision=?,projection=? WHERE promotion_id=? AND revision=? AND state=?`, value.GetState(), value.GetRevision(), body, value.GetPromotionId(), expected, cloudpb.PromotionState_PROMOTION_STATE_ACTIVE)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return promotion.ErrConflict
	}
	if err := updateEntitlementAndOperatorAudit(ctx, tx, nil, audit); err != nil {
		return err
	}
	return tx.Commit()
}

// Promotions 返回按创建时间倒序的优惠发布记录。
func (store *Store) Promotions(ctx context.Context, includeDisabled bool, limit int) ([]*cloudpb.PromotionProjection, error) {
	include := 0
	if includeDisabled {
		include = 1
	}
	rows, err := queryContext(ctx, store.db, `SELECT projection FROM promotions WHERE state=? OR ?=1 ORDER BY effective_from DESC,promotion_id LIMIT ?`, cloudpb.PromotionState_PROMOTION_STATE_ACTIVE, include, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*cloudpb.PromotionProjection
	for rows.Next() {
		var body []byte
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		value := &cloudpb.PromotionProjection{}
		if err := proto.Unmarshal(body, value); err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

// PromotionRedemptions 返回优惠/账号范围内的 reservation 和兑换历史。
func (store *Store) PromotionRedemptions(ctx context.Context, promotionID, accountID string, limit int) ([]*cloudpb.PromotionRedemptionProjection, error) {
	rows, err := queryContext(ctx, store.db, `SELECT projection FROM promotion_redemptions WHERE (?='' OR promotion_id=?) AND (?='' OR account_id=?) ORDER BY updated_at DESC,redemption_id LIMIT ?`, promotionID, promotionID, accountID, accountID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*cloudpb.PromotionRedemptionProjection
	for rows.Next() {
		var body []byte
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		value := &cloudpb.PromotionRedemptionProjection{}
		if err := proto.Unmarshal(body, value); err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

// ReservePromotionCheckout 在同一事务锁定 code、校验容量、固化折扣、写订单和 reservation。
func (store *Store) ReservePromotionCheckout(ctx context.Context, order *cloudpb.OrderProjection, audit *cloudpb.CommerceAuditProjection, code string, now time.Time, ttl time.Duration) (*cloudpb.PromotionRedemptionProjection, error) {
	if order == nil || audit == nil || ttl <= 0 {
		return nil, promotion.ErrInvalid
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var promotionBody []byte
	if err := queryRowContext(ctx, tx, `SELECT projection FROM promotions WHERE code=? FOR UPDATE`, code).Scan(&promotionBody); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, promotion.ErrNotFound
		}
		return nil, err
	}
	value := &cloudpb.PromotionProjection{}
	if err := proto.Unmarshal(promotionBody, value); err != nil {
		return nil, err
	}
	discount, snapshot, err := promotion.Discount(value, order.GetPlanId(), order.GetPrice().GetCurrency(), order.GetSubtotalMinor(), now)
	if err != nil {
		return nil, err
	}
	var totalCount, accountCount int64
	if err := queryRowContext(ctx, tx, `SELECT COUNT(*) FROM promotion_redemptions WHERE promotion_id=? AND (state=? OR (state=? AND expires_at>?))`, value.GetPromotionId(), cloudpb.PromotionRedemptionState_PROMOTION_REDEMPTION_STATE_REDEEMED, cloudpb.PromotionRedemptionState_PROMOTION_REDEMPTION_STATE_RESERVED, now.UnixMilli()).Scan(&totalCount); err != nil {
		return nil, err
	}
	if err := queryRowContext(ctx, tx, `SELECT COUNT(*) FROM promotion_redemptions WHERE promotion_id=? AND account_id=? AND (state=? OR (state=? AND expires_at>?))`, value.GetPromotionId(), order.GetAccountId(), cloudpb.PromotionRedemptionState_PROMOTION_REDEMPTION_STATE_REDEEMED, cloudpb.PromotionRedemptionState_PROMOTION_REDEMPTION_STATE_RESERVED, now.UnixMilli()).Scan(&accountCount); err != nil {
		return nil, err
	}
	if totalCount >= int64(value.GetMaxRedemptions()) || accountCount >= int64(value.GetMaxRedemptionsPerAccount()) {
		return nil, promotion.ErrConflict
	}
	storedOrder := proto.Clone(order).(*cloudpb.OrderProjection)
	storedOrder.DiscountMinor, storedOrder.TotalMinor, storedOrder.Promotion = discount, storedOrder.GetSubtotalMinor()-discount, snapshot
	orderBody, err := marshal(storedOrder)
	if err != nil {
		return nil, err
	}
	if _, err := execContext(ctx, tx, `INSERT INTO commerce_orders(order_id,account_id,revision,projection) VALUES(?,?,?,?)`, storedOrder.GetOrderId(), storedOrder.GetAccountId(), storedOrder.GetRevision(), orderBody); err != nil {
		return nil, promotion.ErrConflict
	}
	redemption := &cloudpb.PromotionRedemptionProjection{RedemptionId: "redemption_" + storedOrder.GetOrderId(), PromotionId: value.GetPromotionId(), AccountId: storedOrder.GetAccountId(), OrderId: storedOrder.GetOrderId(), State: cloudpb.PromotionRedemptionState_PROMOTION_REDEMPTION_STATE_RESERVED, DiscountMinor: discount, ReservedAtUnixMillis: now.UnixMilli(), ExpiresAtUnixMillis: now.Add(ttl).UnixMilli(), UpdatedAtUnixMillis: now.UnixMilli(), Revision: 1}
	redemptionBody, err := marshal(redemption)
	if err != nil {
		return nil, err
	}
	if _, err := execContext(ctx, tx, `INSERT INTO promotion_redemptions(redemption_id,promotion_id,account_id,order_id,state,expires_at,updated_at,revision,projection) VALUES(?,?,?,?,?,?,?,?,?)`, redemption.GetRedemptionId(), redemption.GetPromotionId(), redemption.GetAccountId(), redemption.GetOrderId(), redemption.GetState(), redemption.GetExpiresAtUnixMillis(), redemption.GetUpdatedAtUnixMillis(), redemption.GetRevision(), redemptionBody); err != nil {
		return nil, promotion.ErrConflict
	}
	if err := insertCommerceAudit(ctx, tx, audit); err != nil {
		return nil, commerce.ErrConflict
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	*order = *proto.Clone(storedOrder).(*cloudpb.OrderProjection)
	return redemption, nil
}

// ReleaseExpiredPromotionReservations 有界释放未支付且已到期的 reservation。
func (store *Store) ReleaseExpiredPromotionReservations(ctx context.Context, now time.Time, limit int) (int, error) {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	rows, err := queryContext(ctx, tx, `SELECT redemption_id,projection FROM promotion_redemptions WHERE state=? AND expires_at<=? ORDER BY expires_at LIMIT ? FOR UPDATE`, cloudpb.PromotionRedemptionState_PROMOTION_REDEMPTION_STATE_RESERVED, now.UnixMilli(), limit)
	if err != nil {
		return 0, err
	}
	type pending struct {
		id    string
		value *cloudpb.PromotionRedemptionProjection
	}
	var values []pending
	for rows.Next() {
		var id string
		var body []byte
		if err := rows.Scan(&id, &body); err != nil {
			rows.Close()
			return 0, err
		}
		value := &cloudpb.PromotionRedemptionProjection{}
		if err := proto.Unmarshal(body, value); err != nil {
			rows.Close()
			return 0, err
		}
		values = append(values, pending{id: id, value: value})
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	for _, item := range values {
		item.value.State, item.value.UpdatedAtUnixMillis, item.value.Revision = cloudpb.PromotionRedemptionState_PROMOTION_REDEMPTION_STATE_EXPIRED, now.UnixMilli(), item.value.GetRevision()+1
		body, err := marshal(item.value)
		if err != nil {
			return 0, err
		}
		if _, err := execContext(ctx, tx, `UPDATE promotion_redemptions SET state=?,updated_at=?,revision=?,projection=? WHERE redemption_id=? AND state=?`, item.value.GetState(), item.value.GetUpdatedAtUnixMillis(), item.value.GetRevision(), body, item.id, cloudpb.PromotionRedemptionState_PROMOTION_REDEMPTION_STATE_RESERVED); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(values), nil
}
