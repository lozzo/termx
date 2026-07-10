package relaylease_test

import (
	"crypto/ed25519"
	"errors"
	"testing"
	"time"

	"github.com/lozzow/termx/private/termx-cloud/control-plane/domain"
	"github.com/lozzow/termx/private/termx-cloud/control-plane/entitlement"
	"github.com/lozzow/termx/private/termx-cloud/control-plane/relaylease"
	"github.com/lozzow/termx/private/termx-cloud/control-plane/servicecredential"
)

type sessionSource struct {
	session domain.ManagedSession
}

func (source sessionSource) ManagedSession(accountID, sessionID string, now time.Time) (domain.ManagedSession, error) {
	if source.session.AccountID != accountID || source.session.ID != sessionID || !now.Before(source.session.ExpiresAt) {
		return domain.ManagedSession{}, errors.New("managed session not found")
	}
	return source.session, nil
}

func TestServiceIssuesEntitlementClampedLease(t *testing.T) {
	now := time.Date(2026, 7, 11, 8, 0, 0, 0, time.UTC)
	seed := make([]byte, ed25519.SeedSize)
	privateKey := ed25519.NewKeyFromSeed(seed)
	signer, _ := servicecredential.NewSigner("cp-key", privateKey, now.Add(-time.Hour), now.Add(time.Hour))
	issuer, _ := servicecredential.NewRelayLeaseIssuer("control-plane.test", signer)
	store := entitlement.NewStore()
	if err := store.Put(entitlement.Entitlement{
		AccountID: "account-1", Status: entitlement.StatusActive, ValidUntil: now.Add(time.Hour), UpdatedAt: now,
		Policy: entitlement.QuotaPolicy{AllowedRegions: []string{"eu-west"}, MaxLeaseDuration: 4 * time.Minute, MaxBytesPerLease: 500_000, MaxBitrateKbps: 6_000, MaxConcurrency: 1},
	}); err != nil {
		t.Fatal(err)
	}
	service, err := relaylease.NewService(sessionSource{session: domain.ManagedSession{
		ID: "managed-1", AccountID: "account-1", ClientDeviceID: "client-1", TargetDeviceID: "daemon-1",
		Hub: domain.HubAssignment{HubID: "hub-eu", Region: "eu-west"}, CreatedAt: now, ExpiresAt: now.Add(time.Hour),
	}}, store, issuer)
	if err != nil {
		t.Fatal(err)
	}
	lease, claims, err := service.Issue(relaylease.Command{
		LeaseID: "lease-1", AccountID: "account-1", ManagedSessionID: "managed-1", AudienceRelayPool: "pool-eu",
		Region: "eu-west", PathKind: servicecredential.RelayPathSingle, RequestedTTL: 9 * time.Minute, CredentialBindingID: "binding-1",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(lease.Bytes()) == 0 || claims.MaxBytes != 500_000 || claims.MaxBitrateKbps != 6_000 || claims.ExpiresAtUnix-claims.NotBeforeUnix != int64((4*time.Minute)/time.Second) {
		t.Fatalf("issued claims = %#v", claims)
	}

	expired := entitlement.Entitlement{AccountID: "account-1", Status: entitlement.StatusExpired, ValidUntil: now.Add(time.Hour), UpdatedAt: now, Policy: entitlement.QuotaPolicy{AllowedRegions: []string{"eu-west"}, MaxLeaseDuration: time.Minute, MaxBytesPerLease: 1, MaxBitrateKbps: 1, MaxConcurrency: 1}}
	if err := store.Put(expired); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Refresh(relaylease.RefreshCommand{PreviousLeaseID: "lease-1", Next: relaylease.Command{LeaseID: "lease-2", AccountID: "account-1", ManagedSessionID: "managed-1", AudienceRelayPool: "pool-eu", Region: "eu-west", PathKind: servicecredential.RelayPathSingle, CredentialBindingID: "binding-2"}}, now); !errors.Is(err, entitlement.ErrNotEntitled) {
		t.Fatalf("expired entitlement error = %v", err)
	}
}
