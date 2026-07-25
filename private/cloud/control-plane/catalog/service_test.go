package catalog

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/muxvia/muxvia/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

type memoryStore struct {
	releases map[uint64]*cloudpb.PlanCatalogReleaseProjection
	active   uint64
}

func (store *memoryStore) PublishCatalog(_ context.Context, release *cloudpb.PlanCatalogReleaseProjection) error {
	version := release.GetCatalog().GetCatalogVersion()
	if _, exists := store.releases[version]; exists {
		return ErrConflict
	}
	for _, current := range store.releases {
		current.Active = false
	}
	store.releases[version] = proto.Clone(release).(*cloudpb.PlanCatalogReleaseProjection)
	store.active = version
	return nil
}
func (store *memoryStore) ActiveCatalog(context.Context) (*cloudpb.PlanCatalogReleaseProjection, error) {
	value := store.releases[store.active]
	if value == nil {
		return nil, ErrNotFound
	}
	return proto.Clone(value).(*cloudpb.PlanCatalogReleaseProjection), nil
}
func (store *memoryStore) CatalogRelease(_ context.Context, version uint64) (*cloudpb.PlanCatalogReleaseProjection, error) {
	value := store.releases[version]
	if value == nil {
		return nil, ErrNotFound
	}
	return proto.Clone(value).(*cloudpb.PlanCatalogReleaseProjection), nil
}
func (store *memoryStore) CatalogReleases(_ context.Context, limit int) ([]*cloudpb.PlanCatalogReleaseProjection, error) {
	versions := make([]int, 0, len(store.releases))
	for version := range store.releases {
		versions = append(versions, int(version))
	}
	sort.Sort(sort.Reverse(sort.IntSlice(versions)))
	var result []*cloudpb.PlanCatalogReleaseProjection
	for _, version := range versions {
		if len(result) == limit {
			break
		}
		result = append(result, proto.Clone(store.releases[uint64(version)]).(*cloudpb.PlanCatalogReleaseProjection))
	}
	return result, nil
}
func (store *memoryStore) CatalogPlan(_ context.Context, planID string, planVersion uint64) (*cloudpb.PlanDefinition, error) {
	for _, release := range store.releases {
		for _, plan := range release.GetCatalog().GetPlans() {
			if plan.GetPlanId() == planID && plan.GetPlanVersion() == planVersion {
				return proto.Clone(plan).(*cloudpb.PlanDefinition), nil
			}
		}
	}
	return nil, ErrNotFound
}

func TestPublishKeepsImmutableHistoryAndMovesActiveHead(t *testing.T) {
	store := &memoryStore{releases: make(map[uint64]*cloudpb.PlanCatalogReleaseProjection)}
	now := time.Unix(100, 0).UTC()
	service, err := New(store, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	first := testCatalog(1, 1)
	if err := service.Bootstrap(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	illegal := testCatalog(2, 1)
	illegal.Plans[0].Capability.CloudDeviceLimit = 99
	if _, err := service.Publish(context.Background(), illegal, "operator-1", "mutate sold plan", "request-illegal"); !errors.Is(err, ErrConflict) {
		t.Fatalf("in-place plan mutation error = %v", err)
	}
	second := testCatalog(2, 2)
	if _, err := service.Publish(context.Background(), second, "operator-1", "new price", "request-2"); err != nil {
		t.Fatal(err)
	}
	active, err := service.Active(context.Background())
	if err != nil || active.GetCatalogVersion() != 2 {
		t.Fatalf("active catalog = (%v, %v)", active, err)
	}
	history, err := service.Releases(context.Background(), 10)
	if err != nil || len(history) != 2 || history[0].GetCatalog().GetCatalogVersion() != 2 || !history[0].GetActive() || history[1].GetActive() {
		t.Fatalf("catalog history = (%v, %v)", history, err)
	}
	if _, err := service.Publish(context.Background(), second, "operator-1", "duplicate", "request-3"); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate publish error = %v", err)
	}
}

func TestValidateRequiresCreemMappingOnlyForConfiguredPlans(t *testing.T) {
	value := testCatalog(1, 1)
	value.Plans[1].Price.Mode = cloudpb.CatalogPriceMode_CATALOG_PRICE_MODE_CONFIGURED
	value.Plans[1].Price.Currency = "USD"
	value.Plans[1].Price.MonthlyMinor = 900
	if err := Validate(value); !errors.Is(err, ErrInvalid) {
		t.Fatalf("configured plan without Creem mapping error = %v", err)
	}
	value.Plans[1].Creem = &cloudpb.CreemProductMapping{ProductId: "prod_test"}
	if err := Validate(value); err != nil {
		t.Fatal(err)
	}
	value.Plans[0].Creem = &cloudpb.CreemProductMapping{ProductId: "forbidden"}
	if err := Validate(value); !errors.Is(err, ErrInvalid) {
		t.Fatalf("included plan with Creem mapping error = %v", err)
	}
}

func testCatalog(catalogVersion, planVersion uint64) *cloudpb.PlanCatalogContract {
	return &cloudpb.PlanCatalogContract{CatalogVersion: catalogVersion, Plans: []*cloudpb.PlanDefinition{
		{PlanId: "free", PlanVersion: planVersion, BillingPeriodDays: 30, Included: true, Capability: &cloudpb.PlanCapability{ManagedP2PEnabled: true, ManagedP2PMaxConcurrency: 1, CloudDeviceLimit: 2}, Price: &cloudpb.PlanPriceDefinition{Mode: cloudpb.CatalogPriceMode_CATALOG_PRICE_MODE_INCLUDED, Label: "Free"}, Presentation: &cloudpb.PlanPresentation{Name: "Free"}},
		{PlanId: "pro", PlanVersion: planVersion, BillingPeriodDays: 30, Capability: &cloudpb.PlanCapability{ManagedP2PEnabled: true, ManagedP2PMaxConcurrency: 2, CloudDeviceLimit: 5}, Price: &cloudpb.PlanPriceDefinition{Mode: cloudpb.CatalogPriceMode_CATALOG_PRICE_MODE_CONTACT, Label: "Contact"}, Presentation: &cloudpb.PlanPresentation{Name: "Pro"}},
	}}
}
