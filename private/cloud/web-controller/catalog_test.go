package webcontroller_test

import (
	"os"
	"path/filepath"
	"testing"

	webcontroller "github.com/lozzow/termx/private/cloud/web-controller"
	"github.com/lozzow/termx/proto/cloudpb"
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
	if catalog.Plans[0].Capability.GetStandardRelayEnabled() || !catalog.Plans[0].Capability.GetManagedP2PEnabled() || catalog.Plans[0].Capability.GetManagedP2PMaxConcurrency() != 1 || catalog.Plans[0].Capability.GetCloudDeviceLimit() != 2 {
		t.Fatalf("managed-free capability = %#v", catalog.Plans[0].Capability)
	}
	if !catalog.Plans[1].Capability.GetManagedP2PEnabled() || !catalog.Plans[1].Capability.GetStandardRelayEnabled() || catalog.Plans[1].Capability.GetRelay().GetMaxConcurrency() != 4 || catalog.Plans[1].Capability.GetRelay().GetMaxBytesPerPeriod() != 10<<30 {
		t.Fatalf("Pro capability = %#v", catalog.Plans[1].Capability)
	}
	contract := catalog.Contract()
	if len(contract.GetPlans()) != 3 || contract.GetPlans()[1].GetCapability().GetRelay().GetMaxBytesPerLease() != 256<<20 || contract.GetPlans()[1].GetPrice().GetMode() != cloudpb.CatalogPriceMode_CATALOG_PRICE_MODE_CONTACT || contract.GetPlans()[1].GetPresentation().GetName() != "Pro" {
		t.Fatalf("catalog contract = %#v", contract)
	}
}

func TestCatalogRejectsAmountOnContactPlan(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plans.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"currency":"USD","plans":[{"id":"pro","version":1,"billing_period_days":30,"capabilities":{"managed_p2p_enabled":true,"managed_p2p_max_concurrency":1,"cloud_device_limit":2},"name":"Pro","eyebrow":"x","description":"x","price":{"mode":"contact","label":"Preview","monthly_minor":100},"cta":{"label":"Join","href":"#"},"features":["P2P"]}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := webcontroller.LoadCatalog(path); err == nil {
		t.Fatal("contact plan with configured amount unexpectedly accepted")
	}
}
