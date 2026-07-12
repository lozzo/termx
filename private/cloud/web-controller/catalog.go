package webcontroller

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
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
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Eyebrow     string       `json:"eyebrow"`
	Description string       `json:"description"`
	Price       CatalogPrice `json:"price"`
	CTA         CatalogCTA   `json:"cta"`
	Featured    bool         `json:"featured"`
	Features    []string     `json:"features"`
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
	for _, plan := range catalog.Plans {
		if plan.ID == "" || plan.Name == "" || plan.Description == "" || plan.Price.Label == "" || plan.CTA.Label == "" || plan.CTA.Href == "" || len(plan.Features) == 0 {
			return fmt.Errorf("invalid Web Controller catalog plan %q", plan.ID)
		}
		if _, exists := ids[plan.ID]; exists {
			return fmt.Errorf("duplicate Web Controller catalog plan %q", plan.ID)
		}
		ids[plan.ID] = struct{}{}
		switch plan.Price.Mode {
		case "configured":
			if plan.Price.MonthlyMinor == nil && plan.Price.YearlyMinor == nil {
				return fmt.Errorf("configured catalog plan %q has no price", plan.ID)
			}
		case "included", "contact":
			if plan.Price.MonthlyMinor != nil || plan.Price.YearlyMinor != nil {
				return fmt.Errorf("unpublished catalog plan %q contains a price", plan.ID)
			}
		default:
			return fmt.Errorf("invalid catalog price mode for plan %q", plan.ID)
		}
	}
	return nil
}
