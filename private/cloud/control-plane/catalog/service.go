// Package catalog 拥有 Muxvia Cloud 套餐目录的发布、版本校验与 active head。
//
// 目录版本一经写入即不可修改；订单和订阅通过 plan_id/plan_version 固化历史，
// 新交易只读取当前 active release。部署 JSON 只能作为数据库为空时的一次性 bootstrap。
package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/muxvia/muxvia/private/cloud/control-plane/entitlement"
	"github.com/muxvia/muxvia/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

var (
	// ErrNotFound 表示目录版本或 active head 尚不存在。
	ErrNotFound = errors.New("plan catalog release not found")
	// ErrConflict 表示版本已发布、请求重复或 active head 已改变。
	ErrConflict = errors.New("plan catalog release conflict")
	// ErrInvalid 表示目录违反机器能力、价格或 provider mapping 约束。
	ErrInvalid = errors.New("invalid plan catalog release")
)

// Store 是不可变目录 release 与 active head 的持久事务边界。
type Store interface {
	PublishCatalog(context.Context, *cloudpb.PlanCatalogReleaseProjection) error
	ActiveCatalog(context.Context) (*cloudpb.PlanCatalogReleaseProjection, error)
	CatalogRelease(context.Context, uint64) (*cloudpb.PlanCatalogReleaseProjection, error)
	CatalogReleases(context.Context, int) ([]*cloudpb.PlanCatalogReleaseProjection, error)
	CatalogPlan(context.Context, string, uint64) (*cloudpb.PlanDefinition, error)
}

// Service 是 operator、Commerce 与 Controller bootstrap 共用的目录应用边界。
type Service struct {
	store Store
	now   func() time.Time
}

// New 创建目录服务；Store 缺失时 fail closed。
func New(store Store, now func() time.Time) (*Service, error) {
	if store == nil {
		return nil, ErrInvalid
	}
	if now == nil {
		now = time.Now
	}
	return &Service{store: store, now: now}, nil
}

// Publish 校验并原子发布一个全新目录版本。actor/reason/request ID 是审计字段，不能由 Store 推导。
func (service *Service) Publish(ctx context.Context, contract *cloudpb.PlanCatalogContract, actorID, reason, requestID string) (*cloudpb.PlanCatalogReleaseProjection, error) {
	if err := Validate(contract); err != nil || strings.TrimSpace(actorID) == "" || strings.TrimSpace(reason) == "" || strings.TrimSpace(requestID) == "" {
		return nil, ErrInvalid
	}
	for _, nextPlan := range contract.GetPlans() {
		oldPlan, planErr := service.store.CatalogPlan(ctx, nextPlan.GetPlanId(), nextPlan.GetPlanVersion())
		if planErr == nil && !proto.Equal(oldPlan, nextPlan) {
			return nil, ErrConflict
		}
		if planErr != nil && !errors.Is(planErr, ErrNotFound) {
			return nil, planErr
		}
	}
	release := &cloudpb.PlanCatalogReleaseProjection{Catalog: proto.Clone(contract).(*cloudpb.PlanCatalogContract), Active: true, ActorId: strings.TrimSpace(actorID), Reason: strings.TrimSpace(reason), RequestId: strings.TrimSpace(requestID), PublishedAtUnixMillis: service.now().UTC().UnixMilli(), Revision: 1}
	if err := service.store.PublishCatalog(ctx, release); err != nil {
		return nil, err
	}
	return proto.Clone(release).(*cloudpb.PlanCatalogReleaseProjection), nil
}

// Bootstrap 只在 active head 不存在时发布部署提供的初始目录；已有数据库真值绝不会被覆盖。
func (service *Service) Bootstrap(ctx context.Context, contract *cloudpb.PlanCatalogContract) error {
	if _, err := service.store.ActiveCatalog(ctx); err == nil {
		return nil
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	_, err := service.Publish(ctx, contract, "controller-bootstrap", "initial catalog bootstrap", fmt.Sprintf("bootstrap-catalog-%d", contract.GetCatalogVersion()))
	return err
}

// Active 返回当前新交易使用的不可变目录快照。
func (service *Service) Active(ctx context.Context) (*cloudpb.PlanCatalogContract, error) {
	release, err := service.store.ActiveCatalog(ctx)
	if err != nil {
		return nil, err
	}
	if err := Validate(release.GetCatalog()); err != nil {
		return nil, err
	}
	return proto.Clone(release.GetCatalog()).(*cloudpb.PlanCatalogContract), nil
}

// Release 返回指定历史版本；调用方不得据此切换 active head。
func (service *Service) Release(ctx context.Context, version uint64) (*cloudpb.PlanCatalogReleaseProjection, error) {
	if version == 0 {
		return nil, ErrInvalid
	}
	return service.store.CatalogRelease(ctx, version)
}

// Releases 按版本倒序返回有界历史。
func (service *Service) Releases(ctx context.Context, limit int) ([]*cloudpb.PlanCatalogReleaseProjection, error) {
	if limit < 1 || limit > 200 {
		return nil, ErrInvalid
	}
	return service.store.CatalogReleases(ctx, limit)
}

// Plan 返回任一历史 release 中精确的 plan_id/plan_version，供既有订单和订阅重算。
// 相同套餐版本若出现在多个 catalog release 中必须保持字节相等，否则 fail closed。
func (service *Service) Plan(ctx context.Context, planID string, planVersion uint64) (*cloudpb.PlanDefinition, error) {
	if strings.TrimSpace(planID) == "" || planVersion == 0 {
		return nil, ErrInvalid
	}
	return service.store.CatalogPlan(ctx, planID, planVersion)
}

// Validate 校验目录的能力、价格和 Creem mapping；不允许按 plan ID 隐式补值。
func Validate(contract *cloudpb.PlanCatalogContract) error {
	if contract == nil || contract.GetCatalogVersion() == 0 || len(contract.GetPlans()) == 0 {
		return ErrInvalid
	}
	ids := make(map[string]struct{}, len(contract.GetPlans()))
	included := 0
	for _, plan := range contract.GetPlans() {
		if plan == nil || strings.TrimSpace(plan.GetPlanId()) == "" || plan.GetPlanVersion() == 0 || plan.GetBillingPeriodDays() == 0 || plan.GetPrice() == nil || plan.GetPresentation() == nil || strings.TrimSpace(plan.GetPresentation().GetName()) == "" {
			return ErrInvalid
		}
		if _, exists := ids[plan.GetPlanId()]; exists {
			return ErrInvalid
		}
		ids[plan.GetPlanId()] = struct{}{}
		if err := entitlement.ValidatePlanCapability(plan.GetCapability()); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalid, err)
		}
		switch plan.GetPrice().GetMode() {
		case cloudpb.CatalogPriceMode_CATALOG_PRICE_MODE_INCLUDED:
			included++
			if !plan.GetIncluded() || plan.GetCreem() != nil || plan.GetPrice().GetMonthlyMinor() != 0 || plan.GetPrice().GetYearlyMinor() != 0 {
				return ErrInvalid
			}
		case cloudpb.CatalogPriceMode_CATALOG_PRICE_MODE_CONFIGURED:
			if plan.GetIncluded() || strings.TrimSpace(plan.GetPrice().GetCurrency()) == "" || plan.GetPrice().GetMonthlyMinor() <= 0 && plan.GetPrice().GetYearlyMinor() <= 0 ||
				plan.GetPrice().GetMonthlyMinor() > 0 && strings.TrimSpace(plan.GetCreem().GetMonthlyProductId()) == "" ||
				plan.GetPrice().GetYearlyMinor() > 0 && strings.TrimSpace(plan.GetCreem().GetYearlyProductId()) == "" {
				return ErrInvalid
			}
		case cloudpb.CatalogPriceMode_CATALOG_PRICE_MODE_CONTACT:
			if plan.GetIncluded() || plan.GetCreem() != nil || plan.GetPrice().GetMonthlyMinor() != 0 || plan.GetPrice().GetYearlyMinor() != 0 {
				return ErrInvalid
			}
		default:
			return ErrInvalid
		}
	}
	if included != 1 {
		return ErrInvalid
	}
	return nil
}
