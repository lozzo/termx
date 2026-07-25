// Package promotion 拥有有界优惠码、checkout reservation 与兑换记录。
//
// 经济字段发布后不可修改；停用只影响新 checkout。reservation 与订单必须在同一数据库事务写入，
// 支付终态再由 commerce journal 原子兑换或释放。
package promotion

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/muxvia/muxvia/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

var (
	// ErrInvalid 表示优惠经济字段、范围或时间窗非法。
	ErrInvalid = errors.New("invalid promotion")
	// ErrConflict 表示 code、revision、容量或 redemption 状态冲突。
	ErrConflict = errors.New("promotion conflict")
	// ErrNotFound 表示优惠码或兑换记录不存在。
	ErrNotFound = errors.New("promotion not found")
)

const reservationTTL = 20 * time.Minute

// Store 是优惠发布、订单 reservation 与到期释放的持久事务边界。
type Store interface {
	CreatePromotion(context.Context, *cloudpb.PromotionProjection, *cloudpb.OperatorMutationAuditProjection) error
	DisablePromotion(context.Context, *cloudpb.PromotionProjection, uint64, *cloudpb.OperatorMutationAuditProjection) error
	Promotions(context.Context, bool, int) ([]*cloudpb.PromotionProjection, error)
	PromotionRedemptions(context.Context, string, string, int) ([]*cloudpb.PromotionRedemptionProjection, error)
	ReservePromotionCheckout(context.Context, *cloudpb.OrderProjection, *cloudpb.CommerceAuditProjection, string, time.Time, time.Duration) (*cloudpb.PromotionRedemptionProjection, error)
	ReleaseExpiredPromotionReservations(context.Context, time.Time, int) (int, error)
}

// ExternalValidator 在本地 promotion 落库前核对正式 provider 的 Discount 真值。
// validator 只能读取 provider，不拥有 promotion 或 redemption 状态。
type ExternalValidator interface {
	ValidatePromotion(context.Context, *cloudpb.PromotionProjection) error
}

// Service 是 operator 与 Commerce 共用的优惠应用边界。
type Service struct {
	store    Store
	now      func() time.Time
	random   io.Reader
	external ExternalValidator
}

// New 创建优惠服务；Store 缺失时 fail closed。
func New(store Store, now func() time.Time, random io.Reader, external ExternalValidator) (*Service, error) {
	if store == nil {
		return nil, ErrInvalid
	}
	if now == nil {
		now = time.Now
	}
	if random == nil {
		random = rand.Reader
	}
	return &Service{store: store, now: now, random: random, external: external}, nil
}

// Create 发布一个经济字段不可变的优惠码。
func (service *Service) Create(ctx context.Context, request *cloudpb.CreatePromotionRequest, actorID string) (*cloudpb.CreatePromotionResponse, error) {
	if request == nil || request.GetPromotion() == nil || actorID == "" || request.GetRequestId() == "" {
		return nil, ErrInvalid
	}
	now := service.now().UTC()
	value := proto.Clone(request.GetPromotion()).(*cloudpb.PromotionProjection)
	value.Code = strings.ToUpper(strings.TrimSpace(value.GetCode()))
	value.ActorId, value.Reason = actorID, strings.TrimSpace(value.GetReason())
	if value.GetPromotionId() != "" || value.GetRevision() != 0 || value.GetState() != cloudpb.PromotionState_PROMOTION_STATE_UNSPECIFIED || value.GetCreatedAtUnixMillis() != 0 || value.GetUpdatedAtUnixMillis() != 0 {
		return nil, ErrInvalid
	}
	id, err := service.randomID("promotion")
	if err != nil {
		return nil, err
	}
	value.PromotionId, value.State, value.Revision = id, cloudpb.PromotionState_PROMOTION_STATE_ACTIVE, 1
	value.CreatedAtUnixMillis, value.UpdatedAtUnixMillis = now.UnixMilli(), now.UnixMilli()
	if err := Validate(value); err != nil {
		return nil, err
	}
	if service.external != nil {
		if err := service.external.ValidatePromotion(ctx, value); err != nil {
			return nil, err
		}
	}
	audit := operatorAudit(actorID, "promotion.create", id, "promotion", value.GetReason(), request.GetRequestId(), 0, 1, now)
	if err := service.store.CreatePromotion(ctx, value, audit); err != nil {
		return nil, err
	}
	return &cloudpb.CreatePromotionResponse{Promotion: proto.Clone(value).(*cloudpb.PromotionProjection)}, nil
}

// Disable 以 expected revision 停用优惠；经济字段和既有 redemption 保持不变。
func (service *Service) Disable(ctx context.Context, request *cloudpb.DisablePromotionRequest, actorID string) (*cloudpb.DisablePromotionResponse, error) {
	if request == nil || request.GetPromotionId() == "" || request.GetExpectedRevision() == 0 || strings.TrimSpace(request.GetReason()) == "" || request.GetRequestId() == "" || actorID == "" {
		return nil, ErrInvalid
	}
	values, err := service.store.Promotions(ctx, true, 200)
	if err != nil {
		return nil, err
	}
	var value *cloudpb.PromotionProjection
	for _, current := range values {
		if current.GetPromotionId() == request.GetPromotionId() {
			value = proto.Clone(current).(*cloudpb.PromotionProjection)
			break
		}
	}
	if value == nil {
		return nil, ErrNotFound
	}
	if value.GetState() != cloudpb.PromotionState_PROMOTION_STATE_ACTIVE || value.GetRevision() != request.GetExpectedRevision() {
		return nil, ErrConflict
	}
	now := service.now().UTC()
	value.State, value.Revision, value.ActorId, value.Reason, value.UpdatedAtUnixMillis = cloudpb.PromotionState_PROMOTION_STATE_DISABLED, value.GetRevision()+1, actorID, strings.TrimSpace(request.GetReason()), now.UnixMilli()
	audit := operatorAudit(actorID, "promotion.disable", value.GetPromotionId(), "promotion", value.GetReason(), request.GetRequestId(), request.GetExpectedRevision(), value.GetRevision(), now)
	if err := service.store.DisablePromotion(ctx, value, request.GetExpectedRevision(), audit); err != nil {
		return nil, err
	}
	return &cloudpb.DisablePromotionResponse{Promotion: value}, nil
}

// ReserveCheckout 原子写入订单与优惠 reservation；空 code 不允许进入该方法。
func (service *Service) ReserveCheckout(ctx context.Context, order *cloudpb.OrderProjection, audit *cloudpb.CommerceAuditProjection, code string) (*cloudpb.PromotionRedemptionProjection, error) {
	if order == nil || strings.TrimSpace(code) == "" {
		return nil, ErrInvalid
	}
	return service.store.ReservePromotionCheckout(ctx, order, audit, strings.ToUpper(strings.TrimSpace(code)), service.now().UTC(), reservationTTL)
}

// ReconcileExpired 释放到期但尚未支付的 reservation。
func (service *Service) ReconcileExpired(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 500 {
		return 0, ErrInvalid
	}
	return service.store.ReleaseExpiredPromotionReservations(ctx, service.now().UTC(), limit)
}

// List 返回优惠发布记录。
func (service *Service) List(ctx context.Context, includeDisabled bool, limit int) ([]*cloudpb.PromotionProjection, error) {
	if limit < 1 || limit > 200 {
		return nil, ErrInvalid
	}
	return service.store.Promotions(ctx, includeDisabled, limit)
}

// Redemptions 返回按优惠或账号筛选的 reservation/兑换历史。
func (service *Service) Redemptions(ctx context.Context, promotionID, accountID string, limit int) ([]*cloudpb.PromotionRedemptionProjection, error) {
	if promotionID == "" && accountID == "" || limit < 1 || limit > 200 {
		return nil, ErrInvalid
	}
	return service.store.PromotionRedemptions(ctx, promotionID, accountID, limit)
}

// Validate 校验固定/比例优惠、范围、容量和 Creem mapping。
func Validate(value *cloudpb.PromotionProjection) error {
	if value == nil || value.GetPromotionId() == "" || value.GetCode() == "" || value.GetCreemDiscountCode() == "" || value.GetRevision() == 0 || value.GetState() == cloudpb.PromotionState_PROMOTION_STATE_UNSPECIFIED || value.GetEffectiveFromUnixMillis() <= 0 || value.GetEffectiveUntilUnixMillis() <= value.GetEffectiveFromUnixMillis() || value.GetMaxRedemptions() == 0 || value.GetMaxRedemptionsPerAccount() == 0 || value.GetMaxRedemptionsPerAccount() > value.GetMaxRedemptions() || len(value.GetPlanIds()) == 0 || value.GetReason() == "" || value.GetActorId() == "" {
		return ErrInvalid
	}
	switch value.GetDiscountKind() {
	case cloudpb.PromotionDiscountKind_PROMOTION_DISCOUNT_KIND_FIXED:
		if value.GetFixedMinor() <= 0 || value.GetPercentBasisPoints() != 0 || value.GetCurrency() == "" {
			return ErrInvalid
		}
	case cloudpb.PromotionDiscountKind_PROMOTION_DISCOUNT_KIND_PERCENT:
		if value.GetPercentBasisPoints() == 0 || value.GetPercentBasisPoints() > 10_000 || value.GetFixedMinor() != 0 || value.GetCurrency() != "" {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	seen := make(map[string]struct{}, len(value.GetPlanIds()))
	for _, planID := range value.GetPlanIds() {
		if strings.TrimSpace(planID) == "" {
			return ErrInvalid
		}
		if _, exists := seen[planID]; exists {
			return ErrInvalid
		}
		seen[planID] = struct{}{}
	}
	return nil
}

// Discount 计算订单折扣并生成不可变 snapshot；调用方仍需在事务中重新校验 promotion revision/capacity。
func Discount(value *cloudpb.PromotionProjection, planID, currency string, subtotal int64, now time.Time) (int64, *cloudpb.PromotionSnapshot, error) {
	if Validate(value) != nil || value.GetState() != cloudpb.PromotionState_PROMOTION_STATE_ACTIVE || subtotal <= 0 || now.UnixMilli() < value.GetEffectiveFromUnixMillis() || now.UnixMilli() >= value.GetEffectiveUntilUnixMillis() || !contains(value.GetPlanIds(), planID) {
		return 0, nil, ErrConflict
	}
	var discount int64
	if value.GetDiscountKind() == cloudpb.PromotionDiscountKind_PROMOTION_DISCOUNT_KIND_FIXED {
		if value.GetCurrency() != currency {
			return 0, nil, ErrConflict
		}
		discount = value.GetFixedMinor()
	} else {
		discount = subtotal * int64(value.GetPercentBasisPoints()) / 10_000
	}
	if discount > subtotal {
		discount = subtotal
	}
	snapshot := &cloudpb.PromotionSnapshot{PromotionId: value.GetPromotionId(), Code: value.GetCode(), DiscountKind: value.GetDiscountKind(), FixedMinor: value.GetFixedMinor(), PercentBasisPoints: value.GetPercentBasisPoints(), Currency: value.GetCurrency(), CreemDiscountCode: value.GetCreemDiscountCode()}
	return discount, snapshot, nil
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func (service *Service) randomID(prefix string) (string, error) {
	value := make([]byte, 18)
	if _, err := io.ReadFull(service.random, value); err != nil {
		return "", err
	}
	return prefix + "_" + base64.RawURLEncoding.EncodeToString(value), nil
}

func operatorAudit(actorID, action, resourceID, kind, reason, requestID string, before, after uint64, now time.Time) *cloudpb.OperatorMutationAuditProjection {
	return &cloudpb.OperatorMutationAuditProjection{AuditId: "audit_" + requestID, ActorId: actorID, Action: action, ResourceKind: kind, ResourceId: resourceID, Reason: reason, RequestId: requestID, BeforeRevision: before, AfterRevision: after, OccurredAtUnixMillis: now.UnixMilli()}
}
