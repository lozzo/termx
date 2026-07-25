package postgres_test

import (
	"context"
	"testing"
	"time"

	cloudcatalog "github.com/muxvia/muxvia/private/cloud/control-plane/catalog"
	"github.com/muxvia/muxvia/private/cloud/control-plane/commerce"
	cloudentitlement "github.com/muxvia/muxvia/private/cloud/control-plane/entitlement"
	cloudpostgres "github.com/muxvia/muxvia/private/cloud/control-plane/postgres"
	"github.com/muxvia/muxvia/proto/cloudpb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

func TestCatalogHistoryAndEntitlementOverrideLifecycleSurviveRestart(t *testing.T) {
	ctx := context.Background()
	dsn := testPostgresDSN(t)
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	store, err := cloudpostgres.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	catalogs, _ := cloudcatalog.New(store, clock)
	if err := catalogs.Bootstrap(ctx, overrideCatalog(1, 1, 2)); err != nil {
		t.Fatal(err)
	}
	commerceService, _ := commerce.New(commerce.Config{Store: store, Catalog: catalogs, Now: clock})
	registered, err := commerceService.Register(ctx, &cloudpb.RegisterAccountRequest{Email: "override@example.com", Password: "secure-password"})
	if err != nil {
		t.Fatal(err)
	}
	account := registered.GetSession().GetAccount()
	overrides, _ := cloudentitlement.NewOverrideService(cloudentitlement.OverrideServiceConfig{Store: store, Plans: catalogs, Now: clock})
	put, err := overrides.Put(ctx, putDeviceLimitOverride(account.GetAccountId(), now.Add(-time.Minute), now.Add(time.Hour), 5, "request-now"), "operator-1")
	if err != nil || put.GetEntitlement().GetCapability().GetCloudDeviceLimit() != 5 || put.GetEntitlement().GetRevision() != 2 {
		t.Fatalf("immediate override = (%v, %v)", put, err)
	}
	if _, err := catalogs.Publish(ctx, overrideCatalog(2, 2, 3), "operator-1", "publish v2", "catalog-v2"); err != nil {
		t.Fatal(err)
	}
	revoked, err := overrides.Revoke(ctx, &cloudpb.RevokeEntitlementOverrideRequest{AccountId: account.GetAccountId(), OverrideId: put.GetOverride().GetOverrideId(), ExpectedRevision: put.GetOverride().GetRevision(), Reason: "grant complete", RequestId: "request-revoke"}, "operator-1")
	if err != nil || revoked.GetEntitlement().GetCapability().GetCloudDeviceLimit() != 2 {
		t.Fatalf("revoke must restore subscribed historical plan = (%v, %v)", revoked, err)
	}
	future, err := overrides.Put(ctx, putDeviceLimitOverride(account.GetAccountId(), now.Add(time.Minute), now.Add(2*time.Minute), 7, "request-future"), "operator-1")
	if err != nil || future.GetEntitlement().GetCapability().GetCloudDeviceLimit() != 2 {
		t.Fatalf("future override changed entitlement early = (%v, %v)", future, err)
	}
	_ = store.Close()

	store, err = cloudpostgres.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	catalogs, _ = cloudcatalog.New(store, clock)
	overrides, _ = cloudentitlement.NewOverrideService(cloudentitlement.OverrideServiceConfig{Store: store, Plans: catalogs, Now: clock})
	now = now.Add(90 * time.Second)
	if count, err := overrides.ReconcileDue(ctx, 10); err != nil || count != 1 {
		t.Fatalf("activation reconciliation = (%d, %v)", count, err)
	}
	active, _ := store.Entitlement(ctx, account.GetAccountId())
	if active.GetCapability().GetCloudDeviceLimit() != 7 {
		t.Fatalf("activated entitlement = %v", active)
	}
	now = now.Add(time.Minute)
	if count, err := overrides.ReconcileDue(ctx, 10); err != nil || count != 1 {
		t.Fatalf("expiration reconciliation = (%d, %v)", count, err)
	}
	expired, _ := store.Entitlement(ctx, account.GetAccountId())
	if expired.GetCapability().GetCloudDeviceLimit() != 2 || expired.GetRevision() <= active.GetRevision() {
		t.Fatalf("expired entitlement = %v", expired)
	}
	history, err := catalogs.Releases(ctx, 10)
	if err != nil || len(history) != 2 || !history[0].GetActive() || history[1].GetActive() {
		t.Fatalf("catalog history after restart = (%v, %v)", history, err)
	}
}

func putDeviceLimitOverride(accountID string, from, until time.Time, limit uint32, requestID string) *cloudpb.PutEntitlementOverrideRequest {
	return &cloudpb.PutEntitlementOverrideRequest{Override: &cloudpb.EntitlementOverrideProjection{AccountId: accountID, CapabilityMask: &fieldmaskpb.FieldMask{Paths: []string{"cloud_device_limit"}}, Capability: &cloudpb.PlanCapability{CloudDeviceLimit: limit}, EffectiveFromUnixMillis: from.UnixMilli(), EffectiveUntilUnixMillis: until.UnixMilli(), Reason: "support grant"}, RequestId: requestID}
}

func overrideCatalog(catalogVersion, planVersion uint64, deviceLimit uint32) *cloudpb.PlanCatalogContract {
	return &cloudpb.PlanCatalogContract{CatalogVersion: catalogVersion, Plans: []*cloudpb.PlanDefinition{
		{PlanId: "free", PlanVersion: planVersion, BillingPeriodDays: 30, Included: true, Capability: &cloudpb.PlanCapability{ManagedP2PEnabled: true, ManagedP2PMaxConcurrency: 1, CloudDeviceLimit: deviceLimit}, Price: &cloudpb.PlanPriceDefinition{Mode: cloudpb.CatalogPriceMode_CATALOG_PRICE_MODE_INCLUDED, Label: "Free"}, Presentation: &cloudpb.PlanPresentation{Name: "Free"}},
		{PlanId: "pro", PlanVersion: planVersion, BillingPeriodDays: 30, Capability: &cloudpb.PlanCapability{ManagedP2PEnabled: true, ManagedP2PMaxConcurrency: 4, CloudDeviceLimit: 10}, Price: &cloudpb.PlanPriceDefinition{Mode: cloudpb.CatalogPriceMode_CATALOG_PRICE_MODE_CONTACT, Label: "Contact"}, Presentation: &cloudpb.PlanPresentation{Name: "Pro"}},
	}}
}
