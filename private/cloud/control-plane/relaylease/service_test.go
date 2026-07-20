package relaylease_test

import (
	"context"
	"crypto/ed25519"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/lozzow/termx/private/cloud/control-plane/domain"
	"github.com/lozzow/termx/private/cloud/control-plane/entitlement"
	"github.com/lozzow/termx/private/cloud/control-plane/relaylease"
	"github.com/lozzow/termx/private/cloud/control-plane/relayquota"
	"github.com/lozzow/termx/private/cloud/control-plane/servicecredential"
	cloudsqlite "github.com/lozzow/termx/private/cloud/control-plane/sqlite"
	"github.com/lozzow/termx/proto/cloudpb"
)

type sessionSource struct {
	session domain.ManagedSession
}

func (source sessionSource) ManagedSession(_ context.Context, accountID, sessionID, clientDeviceID, targetDeviceID, hubID, region string, now time.Time) (domain.ManagedSession, error) {
	if source.session.AccountID != accountID || source.session.ID != sessionID || source.session.ClientDeviceID != clientDeviceID || source.session.TargetDeviceID != targetDeviceID || source.session.Hub.HubID != hubID || source.session.Hub.Region != region || !now.Before(source.session.ExpiresAt) {
		return domain.ManagedSession{}, errors.New("managed session not found")
	}
	return source.session, nil
}

type entitlementSource struct{ store *entitlement.Store }

func (source entitlementSource) Entitlement(_ context.Context, accountID string) (entitlement.Entitlement, error) {
	return source.store.Entitlement(accountID)
}

func TestServiceIssuesEntitlementClampedLease(t *testing.T) {
	now := time.Date(2026, 7, 11, 8, 0, 0, 0, time.UTC)
	seed := make([]byte, ed25519.SeedSize)
	privateKey := ed25519.NewKeyFromSeed(seed)
	signer, _ := servicecredential.NewSigner("cp-key", privateKey, now.Add(-time.Hour), now.Add(time.Hour))
	issuer, _ := servicecredential.NewRelayLeaseIssuer("control-plane.test", signer)
	store := entitlement.NewStore()
	if err := store.Put(entitlement.Entitlement{
		AccountID: "account-1", Status: entitlement.StatusActive, EffectiveFrom: now.Add(-time.Minute), EffectiveUntil: now.Add(time.Hour), UpdatedAt: now,
		SourceSubscriptionID: "subscription-1", SourcePlanID: "relay-test", SourcePlanVersion: 1,
		Capability: &cloudpb.PlanCapability{ManagedP2PEnabled: true, ManagedP2PMaxConcurrency: 1, StandardRelayEnabled: true, CloudDeviceLimit: 2, Relay: &cloudpb.RelayServiceCapability{AllowedRegions: []string{"eu-west"}, MaxLeaseSeconds: uint32((4 * time.Minute) / time.Second), MaxBytesPerLease: 500_000, MaxBitrateKbps: 6_000, MaxConcurrency: 1, MaxBytesPerPeriod: 1_000_000}},
	}); err != nil {
		t.Fatal(err)
	}
	quotaStore, err := cloudsqlite.Open(filepath.Join(t.TempDir(), "quota.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer quotaStore.Close()
	service, err := relaylease.NewService(sessionSource{session: domain.ManagedSession{
		ID: "managed-1", AccountID: "account-1", ClientDeviceID: "client-1", TargetDeviceID: "daemon-1",
		Hub: domain.HubAssignment{HubID: "hub-eu", Region: "eu-west"}, CreatedAt: now, ExpiresAt: now.Add(time.Hour),
	}}, entitlementSource{store: store}, quotaStore, issuer, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	lease, claims, err := service.Issue(context.Background(), relaylease.Command{
		LeaseID: "lease-1", AccountID: "account-1", ManagedSessionID: "managed-1", AudienceRelayPool: "pool-eu",
		HubID: "hub-eu", Region: "eu-west", PathKind: servicecredential.RelayPathSingle, RequestedTTL: 9 * time.Minute, CredentialBindingID: "binding-1", ClientDeviceID: "client-1", TargetDeviceID: "daemon-1",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(lease.Bytes()) == 0 || claims.MaxBytes != 500_000 || claims.MaxBitrateKbps != 6_000 || claims.ExpiresAtUnix-claims.NotBeforeUnix != int64((4*time.Minute)/time.Second) {
		t.Fatalf("issued claims = %#v", claims)
	}
	replayed, replayClaims, err := service.Issue(context.Background(), relaylease.Command{
		LeaseID: "lease-1", AccountID: "account-1", ManagedSessionID: "managed-1", AudienceRelayPool: "pool-eu",
		HubID: "hub-eu", Region: "eu-west", PathKind: servicecredential.RelayPathSingle, RequestedTTL: 9 * time.Minute, CredentialBindingID: "binding-1", ClientDeviceID: "client-1", TargetDeviceID: "daemon-1",
	}, now.Add(30*time.Second))
	if err != nil || string(replayed.Bytes()) != string(lease.Bytes()) || replayClaims.ExpiresAtUnix != claims.ExpiresAtUnix {
		t.Fatalf("lease replay = (%v, %#v, %v)", replayed, replayClaims, err)
	}
	refresh := relaylease.RefreshCommand{PreviousLeaseID: "lease-1", Next: relaylease.Command{LeaseID: "lease-2", AccountID: "account-1", ManagedSessionID: "managed-1", AudienceRelayPool: "pool-eu", HubID: "hub-eu", Region: "eu-west", PathKind: servicecredential.RelayPathSingle, CredentialBindingID: "binding-2", ClientDeviceID: "client-1", TargetDeviceID: "daemon-1"}}
	if _, _, err := service.Refresh(context.Background(), refresh, now.Add(4*time.Minute+30*time.Second)); !errors.Is(err, relayquota.ErrQuotaExhausted) {
		t.Fatalf("refresh before report grace release = %v", err)
	}
	refreshed, refreshedClaims, err := service.Refresh(context.Background(), refresh, now.Add(5*time.Minute+time.Second))
	if err != nil || len(refreshed.Bytes()) == 0 || refreshedClaims.LeaseID != "lease-2" {
		t.Fatalf("refresh after report grace = (%v, %#v, %v)", refreshed, refreshedClaims, err)
	}
	if err := service.Cancel(context.Background(), "account-1", "lease-2", now.Add(5*time.Minute+2*time.Second)); err != nil {
		t.Fatalf("cancel unused refreshed lease = %v", err)
	}
	quota, err := quotaStore.Snapshot(context.Background(), "account-1", now.Add(-time.Minute), now.Add(time.Hour), now.Add(5*time.Minute+2*time.Second))
	if err != nil || quota.GetPeriod().GetReservedBytes() != 0 || quota.GetPeriod().GetRemainingBytes() != 1_000_000 {
		t.Fatalf("quota after cancel = (%v, %v)", quota, err)
	}

	expired := entitlement.Entitlement{AccountID: "account-1", Status: entitlement.StatusExpired, EffectiveFrom: now.Add(-time.Minute), EffectiveUntil: now.Add(time.Hour), UpdatedAt: now, SourceSubscriptionID: "subscription-1", SourcePlanID: "relay-test", SourcePlanVersion: 1, Capability: &cloudpb.PlanCapability{ManagedP2PEnabled: true, ManagedP2PMaxConcurrency: 1, StandardRelayEnabled: true, CloudDeviceLimit: 2, Relay: &cloudpb.RelayServiceCapability{AllowedRegions: []string{"eu-west"}, MaxLeaseSeconds: 60, MaxBytesPerLease: 1, MaxBitrateKbps: 1, MaxConcurrency: 1, MaxBytesPerPeriod: 1}}}
	if err := store.Put(expired); err != nil {
		t.Fatal(err)
	}
	refresh.PreviousLeaseID = "lease-2"
	refresh.Next.LeaseID = "lease-3"
	if _, _, err := service.Refresh(context.Background(), refresh, now.Add(6*time.Minute)); !errors.Is(err, entitlement.ErrNotEntitled) {
		t.Fatalf("expired entitlement error = %v", err)
	}
}
