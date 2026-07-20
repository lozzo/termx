package entitlement_test

import (
	"errors"
	"testing"
	"time"

	"github.com/lozzow/termx/private/cloud/control-plane/entitlement"
	"github.com/lozzow/termx/private/cloud/control-plane/servicecredential"
	"github.com/lozzow/termx/proto/cloudpb"
)

func TestNormalizeP2POnlyPlanDoesNotInferRelayFromValidity(t *testing.T) {
	now := time.Date(2026, 7, 20, 16, 0, 0, 0, time.UTC)
	value := normalizeFixture(t, now, cloudpb.SubscriptionStatus_SUBSCRIPTION_STATUS_ACTIVE, &cloudpb.PlanCapability{ManagedP2PEnabled: true, ManagedP2PMaxConcurrency: 1, CloudDeviceLimit: 2})
	if err := value.AuthorizeManagedP2P(now); err != nil {
		t.Fatalf("managed P2P authorization = %v", err)
	}
	if _, err := value.AuthorizeRelay(entitlement.RelayRequest{Region: "local-1", PathKind: servicecredential.RelayPathSingle}, now); !errors.Is(err, entitlement.ErrNotEntitled) {
		t.Fatalf("P2P-only Relay authorization = %v", err)
	}
}

func TestNormalizeP2PRelayPlanClampsPlanCapability(t *testing.T) {
	now := time.Date(2026, 7, 20, 17, 0, 0, 0, time.UTC)
	capability := &cloudpb.PlanCapability{
		ManagedP2PEnabled: true, ManagedP2PMaxConcurrency: 4, StandardRelayEnabled: true, CloudDeviceLimit: 10,
		Relay: &cloudpb.RelayServiceCapability{
			AllowedRegions: []string{"local-1"}, MaxLeaseSeconds: 300,
			MaxBytesPerLease: 256 << 20, MaxBitrateKbps: 100_000, MaxConcurrency: 4, MaxBytesPerPeriod: 10 << 30,
		},
	}
	value := normalizeFixture(t, now, cloudpb.SubscriptionStatus_SUBSCRIPTION_STATUS_ACTIVE, capability)
	allocation, err := value.AuthorizeRelay(entitlement.RelayRequest{Region: "local-1", PathKind: servicecredential.RelayPathSingle, RequestedTTL: 10 * time.Minute}, now)
	if err != nil {
		t.Fatal(err)
	}
	if allocation.TTL != 5*time.Minute || allocation.MaxBytes != 256<<20 || allocation.MaxBitrateKbps != 100_000 || allocation.MaxConcurrency != 4 {
		t.Fatalf("Relay allocation = %#v", allocation)
	}
	if _, err := value.AuthorizeRelay(entitlement.RelayRequest{Region: "another-region", PathKind: servicecredential.RelayPathSingle}, now); !errors.Is(err, entitlement.ErrQuotaPolicy) {
		t.Fatalf("wrong-region Relay authorization = %v", err)
	}
}

func TestSuspendedSubscriptionDeniesP2PAndRelayDespiteFuturePeriod(t *testing.T) {
	now := time.Date(2026, 7, 20, 18, 0, 0, 0, time.UTC)
	capability := &cloudpb.PlanCapability{
		ManagedP2PEnabled: true, ManagedP2PMaxConcurrency: 1, StandardRelayEnabled: true, CloudDeviceLimit: 2,
		Relay: &cloudpb.RelayServiceCapability{AllowedRegions: []string{"local-1"}, MaxLeaseSeconds: 60, MaxBytesPerLease: 1, MaxBitrateKbps: 1, MaxConcurrency: 1, MaxBytesPerPeriod: 1},
	}
	value := normalizeFixture(t, now, cloudpb.SubscriptionStatus_SUBSCRIPTION_STATUS_SUSPENDED, capability)
	if err := value.AuthorizeManagedP2P(now); !errors.Is(err, entitlement.ErrNotEntitled) {
		t.Fatalf("suspended P2P authorization = %v", err)
	}
	if _, err := value.AuthorizeRelay(entitlement.RelayRequest{Region: "local-1", PathKind: servicecredential.RelayPathSingle}, now); !errors.Is(err, entitlement.ErrNotEntitled) {
		t.Fatalf("suspended Relay authorization = %v", err)
	}
}

func TestTrialGraceAndPastDueNormalizeWithoutPlanInference(t *testing.T) {
	now := time.Date(2026, 7, 20, 18, 30, 0, 0, time.UTC)
	capability := &cloudpb.PlanCapability{ManagedP2PEnabled: true, ManagedP2PMaxConcurrency: 1, CloudDeviceLimit: 2}
	for _, status := range []cloudpb.SubscriptionStatus{cloudpb.SubscriptionStatus_SUBSCRIPTION_STATUS_TRIALING, cloudpb.SubscriptionStatus_SUBSCRIPTION_STATUS_GRACE} {
		if err := normalizeFixture(t, now, status, capability).AuthorizeManagedP2P(now); err != nil {
			t.Fatalf("status %s authorization = %v", status, err)
		}
	}
	if err := normalizeFixture(t, now, cloudpb.SubscriptionStatus_SUBSCRIPTION_STATUS_PAST_DUE, capability).AuthorizeManagedP2P(now); !errors.Is(err, entitlement.ErrNotEntitled) {
		t.Fatalf("past-due authorization = %v", err)
	}
}

func TestEntitlementStoreClonesGeneratedCapability(t *testing.T) {
	now := time.Date(2026, 7, 20, 19, 0, 0, 0, time.UTC)
	value := normalizeFixture(t, now, cloudpb.SubscriptionStatus_SUBSCRIPTION_STATUS_ACTIVE, &cloudpb.PlanCapability{ManagedP2PEnabled: true, ManagedP2PMaxConcurrency: 1, CloudDeviceLimit: 2})
	store := entitlement.NewStore()
	if err := store.Put(value); err != nil {
		t.Fatal(err)
	}
	value.Capability.ManagedP2PEnabled = false
	loaded, err := store.Entitlement(value.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Capability.GetManagedP2PEnabled() {
		t.Fatal("store capability was mutated by caller")
	}
	loaded.Capability.ManagedP2PEnabled = false
	again, _ := store.Entitlement(value.AccountID)
	if !again.Capability.GetManagedP2PEnabled() {
		t.Fatal("returned capability mutated store truth")
	}
}

func normalizeFixture(t *testing.T, now time.Time, status cloudpb.SubscriptionStatus, capability *cloudpb.PlanCapability) entitlement.Entitlement {
	t.Helper()
	subscription := &cloudpb.SubscriptionProjection{
		SubscriptionId: "subscription-1", AccountId: "account-1", SourceOrderId: "order-1",
		PlanId: "fixture", PlanVersion: 1, Status: status,
		CurrentPeriodStartUnixMillis: now.Add(-time.Hour).UnixMilli(), CurrentPeriodEndUnixMillis: now.Add(24 * time.Hour).UnixMilli(), UpdatedAtUnixMillis: now.UnixMilli(),
	}
	value, err := entitlement.Normalize(subscription, &cloudpb.PlanDefinition{PlanId: "fixture", PlanVersion: 1, BillingPeriodDays: 30, Capability: capability}, now)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
