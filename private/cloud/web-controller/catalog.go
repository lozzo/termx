package webcontroller

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/lozzow/termx/private/cloud/control-plane/entitlement"
	"github.com/lozzow/termx/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

// Catalog 描述 Web Controller 对用户公开的套餐目录投影。
// 它由私有产品配置生成，只表达托管云服务能力，不包含或改变 daemon terminal capability。
type Catalog struct {
	Version  int           `json:"version"`
	Currency string        `json:"currency"`
	Plans    []CatalogPlan `json:"plans"`
}

// CatalogPlan 是单个可展示套餐；价格和 CTA 均来自配置，BFF 不推导或虚构金额。
type CatalogPlan struct {
	ID                string                  `json:"id"`
	Version           uint64                  `json:"version"`
	BillingPeriodDays uint32                  `json:"billing_period_days"`
	Capability        *cloudpb.PlanCapability `json:"capabilities"`
	Name              string                  `json:"name"`
	Eyebrow           string                  `json:"eyebrow"`
	Description       string                  `json:"description"`
	Price             CatalogPrice            `json:"price"`
	CTA               CatalogCTA              `json:"cta"`
	Featured          bool                    `json:"featured"`
	Features          []string                `json:"features"`
}

// CatalogPrice 表示展示价格的发布状态。
// configured 才允许携带金额；contact/included 只展示明确标签，便于价格后续独立配置。
type CatalogPrice struct {
	Mode         string `json:"mode"`
	Label        string `json:"label"`
	MonthlyMinor *int64 `json:"monthly_minor,omitempty"`
	YearlyMinor  *int64 `json:"yearly_minor,omitempty"`
}

// CatalogCTA 是套餐目录允许公开的用户动作，不承载 checkout credential。
type CatalogCTA struct {
	Label string `json:"label"`
	Href  string `json:"href"`
}

// LoadCatalog 从部署配置读取并验证套餐目录。
// 配置缺失、重复 plan 或未发布价格携带金额都会 fail closed，防止页面展示错误商业承诺。
func LoadCatalog(path string) (Catalog, error) {
	body, err := os.ReadFile(strings.TrimSpace(path))
	if err != nil {
		return Catalog{}, fmt.Errorf("read Web Controller catalog: %w", err)
	}
	var catalog Catalog
	if err := json.Unmarshal(body, &catalog); err != nil {
		return Catalog{}, fmt.Errorf("decode Web Controller catalog: %w", err)
	}
	if err := validateCatalog(catalog); err != nil {
		return Catalog{}, err
	}
	return catalog, nil
}

func validateCatalog(catalog Catalog) error {
	if catalog.Version != 1 || strings.TrimSpace(catalog.Currency) == "" || len(catalog.Plans) == 0 {
		return fmt.Errorf("invalid Web Controller catalog header")
	}
	ids := make(map[string]struct{}, len(catalog.Plans))
	includedPlans := 0
	for _, plan := range catalog.Plans {
		if plan.ID == "" || plan.Version == 0 || plan.Name == "" || plan.Description == "" || plan.Price.Label == "" || plan.CTA.Label == "" || plan.CTA.Href == "" || len(plan.Features) == 0 {
			return fmt.Errorf("invalid Web Controller catalog plan %q", plan.ID)
		}
		if err := entitlement.ValidatePlanCapability(plan.Capability); err != nil {
			return fmt.Errorf("invalid Web Controller catalog plan %q capability: %w", plan.ID, err)
		}
		if _, exists := ids[plan.ID]; exists {
			return fmt.Errorf("duplicate Web Controller catalog plan %q", plan.ID)
		}
		ids[plan.ID] = struct{}{}
		switch plan.Price.Mode {
		case "configured":
			if plan.BillingPeriodDays == 0 || plan.Price.MonthlyMinor == nil && plan.Price.YearlyMinor == nil {
				return fmt.Errorf("configured catalog plan %q has no price", plan.ID)
			}
		case "included":
			includedPlans++
			if plan.BillingPeriodDays == 0 || plan.Price.MonthlyMinor != nil || plan.Price.YearlyMinor != nil {
				return fmt.Errorf("included catalog plan %q contains billing data", plan.ID)
			}
		case "contact":
			if plan.BillingPeriodDays == 0 {
				return fmt.Errorf("contact catalog plan %q has no billing period", plan.ID)
			}
			if plan.Price.MonthlyMinor != nil || plan.Price.YearlyMinor != nil {
				return fmt.Errorf("unpublished catalog plan %q contains a price", plan.ID)
			}
		default:
			return fmt.Errorf("invalid catalog price mode for plan %q", plan.ID)
		}
	}
	if includedPlans != 1 {
		return fmt.Errorf("Web Controller catalog must contain exactly one included plan")
	}
	return nil
}

// Plan 返回精确 plan ID 的深拷贝；缺失时不 fallback 到免费或 Pro。
func (catalog Catalog) Plan(planID string) (CatalogPlan, bool) {
	for _, plan := range catalog.Plans {
		if plan.ID != planID {
			continue
		}
		plan.Capability = entitlement.ClonePlanCapability(plan.Capability)
		plan.Features = append([]string(nil), plan.Features...)
		return plan, true
	}
	return CatalogPlan{}, false
}

// IncludedPlan 返回目录中唯一的 included 套餐。
// 它只用于展示未购买账号的默认商业关系；能力准入仍必须先生成 Subscription 和 Entitlement。
func (catalog Catalog) IncludedPlan() (CatalogPlan, bool) {
	var included CatalogPlan
	found := false
	for _, plan := range catalog.Plans {
		if plan.Price.Mode != "included" {
			continue
		}
		if found {
			return CatalogPlan{}, false
		}
		included = plan
		found = true
	}
	if !found {
		return CatalogPlan{}, false
	}
	included.Capability = entitlement.ClonePlanCapability(included.Capability)
	included.Features = append([]string(nil), included.Features...)
	return included, true
}

// Contract 返回供 Proto API、Subscription normalization 与测试共同消费的机器能力目录。
func (catalog Catalog) Contract() *cloudpb.PlanCatalogContract {
	contract := &cloudpb.PlanCatalogContract{CatalogVersion: uint64(catalog.Version)}
	for _, plan := range catalog.Plans {
		contract.Plans = append(contract.Plans, &cloudpb.PlanDefinition{
			PlanId: plan.ID, PlanVersion: plan.Version, BillingPeriodDays: plan.BillingPeriodDays,
			Capability: entitlement.ClonePlanCapability(plan.Capability),
		})
	}
	return proto.Clone(contract).(*cloudpb.PlanCatalogContract)
}
