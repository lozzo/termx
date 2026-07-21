package policy_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/muxvia/muxvia/private/cloud/control-plane/commerce"
	"github.com/muxvia/muxvia/private/cloud/control-plane/policy"
	postgrestest "github.com/muxvia/muxvia/private/cloud/control-plane/postgrestest"
	"github.com/muxvia/muxvia/proto/cloudpb"
)

func TestHubAccountPolicyUsesPersistedAuthRevisionAndEntitlement(t *testing.T) {
	now := time.Date(2026, 7, 20, 20, 0, 0, 0, time.UTC)
	store, err := postgrestest.Open(t, filepath.Join(t.TempDir(), "controller-postgres"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service, err := commerce.New(commerce.Config{Store: store, Catalog: policyCatalog(), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	registered, err := service.Register(context.Background(), &cloudpb.RegisterAccountRequest{Email: "policy@example.com", Password: "secure-password"})
	if err != nil {
		t.Fatal(err)
	}
	accountID := registered.GetSession().GetAccount().GetAccountId()
	mapper, _ := policy.New(store)
	projection, err := mapper.HubAccountPolicy(context.Background(), accountID)
	if err != nil {
		t.Fatal(err)
	}
	if projection.GetAuthEpoch() != 1 || projection.GetEntitlementStatus() != cloudpb.EntitlementStatus_ENTITLEMENT_STATUS_ACTIVE || !projection.GetCapability().GetManagedP2PEnabled() || projection.GetCapability().GetManagedP2PMaxConcurrency() != 2 {
		t.Fatalf("policy = %v", projection)
	}
	changed, err := service.ChangePassword(context.Background(), accountID, &cloudpb.ChangeAccountPasswordRequest{CurrentPassword: "secure-password", NewPassword: "new-secure-password"})
	if err != nil {
		t.Fatal(err)
	}
	projection, _ = mapper.HubAccountPolicy(context.Background(), accountID)
	if projection.GetAuthEpoch() != changed.GetSession().GetAccount().GetAuthRevision() {
		t.Fatalf("policy auth epoch = %d", projection.GetAuthEpoch())
	}
	if _, err := service.Transition(context.Background(), &cloudpb.TransitionSubscriptionRequest{AccountId: accountID, ActorId: registered.GetSession().GetAccount().GetUserId(), Transition: cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_SUSPEND}); err != nil {
		t.Fatal(err)
	}
	projection, _ = mapper.HubAccountPolicy(context.Background(), accountID)
	if projection.GetEntitlementStatus() != cloudpb.EntitlementStatus_ENTITLEMENT_STATUS_SUSPENDED || !projection.GetCapability().GetManagedP2PEnabled() {
		t.Fatalf("suspended policy = %v", projection)
	}
}

func policyCatalog() *cloudpb.PlanCatalogContract {
	return &cloudpb.PlanCatalogContract{CatalogVersion: 1, Plans: []*cloudpb.PlanDefinition{{PlanId: "included", PlanVersion: 1, BillingPeriodDays: 30, Included: true, Price: &cloudpb.PlanPriceDefinition{Mode: cloudpb.CatalogPriceMode_CATALOG_PRICE_MODE_INCLUDED, Currency: "USD", Label: "Free"}, Capability: &cloudpb.PlanCapability{ManagedP2PEnabled: true, ManagedP2PMaxConcurrency: 2, CloudDeviceLimit: 3}}}}
}
