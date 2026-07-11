package entitlement_test

import (
	"errors"
	"testing"
	"time"

	"github.com/lozzow/termx/private/cloud/control-plane/entitlement"
	"github.com/lozzow/termx/private/cloud/control-plane/servicecredential"
)

func TestAuthorizeRelayClampsQuotaWithoutTerminalSideEffects(t *testing.T) {
	now := time.Date(2026, 7, 11, 8, 0, 0, 0, time.UTC)
	value := entitlement.Entitlement{
		AccountID: "account-1", Status: entitlement.StatusActive, ValidUntil: now.Add(3 * time.Minute),
		Policy: entitlement.QuotaPolicy{
			AllowedRegions: []string{"eu-west"}, AllowRelayMesh: false, MaxLeaseDuration: 5 * time.Minute,
			MaxBytesPerLease: 1_000_000, MaxBitrateKbps: 8_000, MaxConcurrency: 2,
		},
	}
	allocation, err := value.AuthorizeRelay(entitlement.RelayRequest{Region: "eu-west", PathKind: servicecredential.RelayPathSingle, RequestedTTL: 10 * time.Minute}, now)
	if err != nil {
		t.Fatal(err)
	}
	if allocation.TTL != 3*time.Minute || allocation.MaxBytes != 1_000_000 || allocation.MaxConcurrency != 2 {
		t.Fatalf("allocation = %#v", allocation)
	}
	if _, err := value.AuthorizeRelay(entitlement.RelayRequest{Region: "eu-west", PathKind: servicecredential.RelayPathMesh}, now); !errors.Is(err, entitlement.ErrQuotaPolicy) {
		t.Fatalf("mesh policy error = %v", err)
	}
	value.Status = entitlement.StatusExpired
	if _, err := value.AuthorizeRelay(entitlement.RelayRequest{Region: "eu-west", PathKind: servicecredential.RelayPathSingle}, now); !errors.Is(err, entitlement.ErrNotEntitled) {
		t.Fatalf("expired entitlement error = %v", err)
	}
}
