package creem

import (
	"context"
	"math"
	"strings"
	"time"

	"github.com/muxvia/muxvia/private/cloud/control-plane/promotion"
	"github.com/muxvia/muxvia/proto/cloudpb"
)

// CatalogSource 提供当前套餐到 Creem Product 的已发布映射。
type CatalogSource interface {
	Active(context.Context) (*cloudpb.PlanCatalogContract, error)
}

// PromotionValidator 在 Muxvia 登记 promotion 前回读并核对 Creem Discount。
type PromotionValidator struct {
	api     API
	catalog CatalogSource
}

// NewPromotionValidator 创建只读 provider 校验器；它不创建或删除 Creem Discount。
func NewPromotionValidator(api API, catalog CatalogSource) (*PromotionValidator, error) {
	if api == nil || catalog == nil {
		return nil, ErrInvalid
	}
	return &PromotionValidator{api: api, catalog: catalog}, nil
}

// ValidatePromotion 核对 code、经济字段、有效期、总额度和商品范围后才允许本地 mapping 落库。
func (validator *PromotionValidator) ValidatePromotion(ctx context.Context, value *cloudpb.PromotionProjection) error {
	if promotion.Validate(value) != nil {
		return promotion.ErrInvalid
	}
	discount, err := validator.api.Discount(ctx, value.GetCreemDiscountCode())
	if err != nil {
		return err
	}
	if discount.ID == "" || !strings.EqualFold(discount.Code, value.GetCreemDiscountCode()) || discount.Status != "active" && discount.Status != "scheduled" {
		return promotion.ErrInvalid
	}
	switch value.GetDiscountKind() {
	case cloudpb.PromotionDiscountKind_PROMOTION_DISCOUNT_KIND_FIXED:
		if discount.Type != "fixed" || discount.Amount != value.GetFixedMinor() || !strings.EqualFold(discount.Currency, value.GetCurrency()) {
			return promotion.ErrInvalid
		}
	case cloudpb.PromotionDiscountKind_PROMOTION_DISCOUNT_KIND_PERCENT:
		basisPoints := math.Round(discount.Percentage * 100)
		if discount.Type != "percentage" || basisPoints != float64(value.GetPercentBasisPoints()) {
			return promotion.ErrInvalid
		}
	default:
		return promotion.ErrInvalid
	}
	if discount.MaxRedemptions > 0 && discount.MaxRedemptions < value.GetMaxRedemptions() {
		return promotion.ErrInvalid
	}
	if discount.ExpiryDate != "" {
		expiresAt, parseErr := time.Parse(time.RFC3339, discount.ExpiryDate)
		if parseErr != nil || expiresAt.UnixMilli() < value.GetEffectiveUntilUnixMillis() {
			return promotion.ErrInvalid
		}
	}
	catalog, err := validator.catalog.Active(ctx)
	if err != nil {
		return err
	}
	requiredProducts := make(map[string]bool)
	for _, planID := range value.GetPlanIds() {
		var found bool
		for _, plan := range catalog.GetPlans() {
			if plan.GetPlanId() != planID {
				continue
			}
			found = true
			if plan.GetCreem().GetMonthlyProductId() != "" {
				requiredProducts[plan.GetCreem().GetMonthlyProductId()] = true
			}
			if plan.GetCreem().GetYearlyProductId() != "" {
				requiredProducts[plan.GetCreem().GetYearlyProductId()] = true
			}
		}
		if !found {
			return promotion.ErrInvalid
		}
	}
	for _, productID := range discount.AppliesToProducts {
		delete(requiredProducts, productID)
	}
	if len(requiredProducts) != 0 {
		return promotion.ErrInvalid
	}
	return nil
}
