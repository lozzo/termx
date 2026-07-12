package webcontroller_test

import (
	"os"
	"path/filepath"
	"testing"

	webcontroller "github.com/lozzow/termx/private/cloud/web-controller"
)

func TestCatalogLoadsUnpublishedPricesWithoutInventingAmounts(t *testing.T) {
	catalog, err := webcontroller.LoadCatalog("config/plans.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Plans) != 3 || catalog.Plans[1].ID != "pro" || catalog.Plans[1].Price.Mode != "contact" {
		t.Fatalf("catalog = %#v", catalog)
	}
	if catalog.Plans[1].Price.MonthlyMinor != nil || catalog.Plans[1].Price.YearlyMinor != nil {
		t.Fatal("unpublished Pro price unexpectedly contains an amount")
	}
}

func TestCatalogRejectsAmountOnContactPlan(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plans.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"currency":"USD","plans":[{"id":"pro","name":"Pro","eyebrow":"x","description":"x","price":{"mode":"contact","label":"Preview","monthly_minor":100},"cta":{"label":"Join","href":"#"},"features":["Relay"]}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := webcontroller.LoadCatalog(path); err == nil {
		t.Fatal("contact plan with configured amount unexpectedly accepted")
	}
}
