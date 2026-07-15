package servicecredential

import (
	"crypto/ed25519"
	"errors"
	"testing"
	"time"
)

func TestRelayLeaseBindsRouteAndQuota(t *testing.T) {
	now, signer := testSigner(t, "cp-2026-07", 0)
	ring, _ := NewKeyRing(signer.PublicKey())
	issuer, _ := NewRelayLeaseIssuer("control-plane.test", signer)
	lease, issued, err := issuer.Issue(validMeshLeaseRequest(), now)
	if err != nil {
		t.Fatal(err)
	}
	expected := RelayLeaseExpectation{
		Issuer: "control-plane.test", AudienceRelayPool: "relay-pool-global", AccountID: "account-1",
		ManagedSessionID: "managed-1", ClientDeviceID: "client-1", TargetDeviceID: "daemon-1",
		Region: "eu-west", PathKind: RelayPathMesh, RouteID: "route-1",
	}
	verified, err := VerifyRelayLease(ring, lease.Bytes(), expected, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if verified != issued {
		t.Fatalf("verified claims differ from issued claims")
	}
	expected.RouteID = "route-2"
	if _, err := VerifyRelayLease(ring, lease.Bytes(), expected, now.Add(time.Minute)); !errors.Is(err, ErrCredentialBinding) {
		t.Fatalf("route mismatch error = %v", err)
	}
	request := validMeshLeaseRequest()
	request.MaxInternalTransit = 2
	if _, _, err := issuer.Issue(request, now); !errors.Is(err, ErrCredentialBinding) {
		t.Fatalf("unbounded mesh error = %v", err)
	}
}

func testSigner(t *testing.T, keyID string, seedOffset byte) (time.Time, Signer) {
	t.Helper()
	now := time.Date(2026, 7, 11, 8, 0, 0, 0, time.UTC)
	seed := make([]byte, ed25519.SeedSize)
	for index := range seed {
		seed[index] = byte(index) + seedOffset
	}
	privateKey := ed25519.NewKeyFromSeed(seed)
	signer, err := NewSigner(keyID, privateKey, now.Add(-time.Hour), now.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	return now, signer
}

func validMeshLeaseRequest() RelayLeaseRequest {
	return RelayLeaseRequest{
		LeaseID: "lease-1", AudienceRelayPool: "relay-pool-global", AccountID: "account-1",
		ManagedSessionID: "managed-1", ClientDeviceID: "client-1", TargetDeviceID: "daemon-1",
		Region: "eu-west", PathKind: RelayPathMesh, RouteID: "route-1", RouteVersion: 3,
		ClientEdgeRelayID: "relay-client-edge", DaemonEdgeRelayID: "relay-daemon-edge", MaxInternalTransit: 1,
		TTL: 5 * time.Minute, MaxBytes: 1_000_000, MaxBitrateKbps: 10_000, MaxConcurrency: 1,
		CredentialBindingID: "turn-binding-1",
	}
}
