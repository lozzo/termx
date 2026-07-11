package servicecredential

import (
	"crypto/ed25519"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestHubAdmissionBindsPrincipalSessionAndOperation(t *testing.T) {
	now, signer := testSigner(t, "cp-2026-07", 0)
	ring, err := NewKeyRing(signer.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := NewHubAdmissionIssuer("control-plane.test", signer)
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := issuer.Issue(HubAdmissionRequest{
		TicketID:          "ticket-1",
		AudienceHubID:     "hub-eu-1",
		PrincipalKind:     PrincipalClient,
		AccountID:         "account-1",
		DeviceID:          "client-1",
		SessionKind:       HubSessionManaged,
		SessionID:         "managed-1",
		TargetDeviceID:    "daemon-1",
		AllowedOperations: []HubOperation{HubOperationCandidate, HubOperationOffer},
		TTL:               2 * time.Minute,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	expected := HubAdmissionExpectation{
		Issuer:         "control-plane.test",
		AudienceHubID:  "hub-eu-1",
		PrincipalKind:  PrincipalClient,
		AccountID:      "account-1",
		DeviceID:       "client-1",
		SessionKind:    HubSessionManaged,
		SessionID:      "managed-1",
		TargetDeviceID: "daemon-1",
		Operation:      HubOperationOffer,
	}
	claims, err := VerifyHubAdmission(ring, ticket.Bytes(), expected, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if got := claims.AllowedOperations; len(got) != 2 || got[0] != HubOperationOffer || got[1] != HubOperationCandidate {
		t.Fatalf("canonical operations = %v", got)
	}
	expected.TargetDeviceID = "daemon-2"
	if _, err := VerifyHubAdmission(ring, ticket.Bytes(), expected, now.Add(time.Minute)); !errors.Is(err, ErrCredentialBinding) {
		t.Fatalf("target mismatch error = %v", err)
	}
	if strings.Contains(ticket.String(), string(ticket.Bytes())) {
		t.Fatal("ticket String leaked credential body")
	}
}

func TestAdmissionAndRelayLeaseCannotBeConfused(t *testing.T) {
	now, signer := testSigner(t, "cp-2026-07", 0)
	ring, _ := NewKeyRing(signer.PublicKey())
	admissionIssuer, _ := NewHubAdmissionIssuer("control-plane.test", signer)
	ticket, err := admissionIssuer.Issue(HubAdmissionRequest{
		TicketID: "ticket", AudienceHubID: "hub", PrincipalKind: PrincipalDaemon,
		AccountID: "account", DeviceID: "daemon", SessionKind: HubSessionPresence, SessionID: "presence",
		AllowedOperations: []HubOperation{HubOperationPresence}, TTL: time.Minute,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = VerifyRelayLease(ring, ticket.Bytes(), RelayLeaseExpectation{}, now)
	if !errors.Is(err, ErrMalformedCredential) {
		t.Fatalf("ticket as lease error = %v, want malformed", err)
	}

	leaseIssuer, _ := NewRelayLeaseIssuer("control-plane.test", signer)
	lease, _, err := leaseIssuer.Issue(validMeshLeaseRequest(), now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = VerifyHubAdmission(ring, lease.Bytes(), HubAdmissionExpectation{}, now)
	if !errors.Is(err, ErrMalformedCredential) {
		t.Fatalf("lease as ticket error = %v, want malformed", err)
	}
}

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

func TestKeyRotationOverlapAndEmergencyRevoke(t *testing.T) {
	now, oldSigner := testSigner(t, "old", 0)
	_, newSigner := testSigner(t, "new", 32)
	ring, err := NewKeyRing(oldSigner.PublicKey(), newSigner.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	issuer, _ := NewHubAdmissionIssuer("control-plane.test", oldSigner)
	ticket, err := issuer.Issue(HubAdmissionRequest{
		TicketID: "ticket", AudienceHubID: "hub", PrincipalKind: PrincipalDaemon,
		AccountID: "account", DeviceID: "daemon", SessionKind: HubSessionPresence, SessionID: "presence",
		AllowedOperations: []HubOperation{HubOperationPresence}, TTL: time.Minute,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	expected := HubAdmissionExpectation{Issuer: "control-plane.test", AudienceHubID: "hub", PrincipalKind: PrincipalDaemon, AccountID: "account", DeviceID: "daemon", SessionKind: HubSessionPresence, SessionID: "presence", Operation: HubOperationPresence}
	if _, err := VerifyHubAdmission(ring, ticket.Bytes(), expected, now.Add(30*time.Second)); err != nil {
		t.Fatalf("overlap verification failed: %v", err)
	}
	if err := ring.Revoke("old"); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyHubAdmission(ring, ticket.Bytes(), expected, now.Add(30*time.Second)); !errors.Is(err, ErrRevokedKey) {
		t.Fatalf("revoked key error = %v", err)
	}
}

func TestControlPlaneCredentialCanonicalVector(t *testing.T) {
	now, signer := testSigner(t, "cp-vector-1", 0)
	issuer, _ := NewHubAdmissionIssuer("control-plane.test", signer)
	ticket, err := issuer.Issue(HubAdmissionRequest{
		TicketID: "ticket-vector", AudienceHubID: "hub-vector", PrincipalKind: PrincipalClient,
		AccountID: "account-vector", DeviceID: "client-vector", SessionKind: HubSessionManaged, SessionID: "managed-vector", TargetDeviceID: "daemon-vector",
		AllowedOperations: []HubOperation{HubOperationOffer, HubOperationCandidate}, TTL: 90 * time.Second,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	const expected = "TXHA1.eyJ2ZXJzaW9uIjoxLCJrZXlfaWQiOiJjcC12ZWN0b3ItMSIsInRpY2tldF9pZCI6InRpY2tldC12ZWN0b3IiLCJpc3N1ZXIiOiJjb250cm9sLXBsYW5lLnRlc3QiLCJhdWRpZW5jZV9odWJfaWQiOiJodWItdmVjdG9yIiwicHJpbmNpcGFsX2tpbmQiOiJjbGllbnQiLCJhY2NvdW50X2lkIjoiYWNjb3VudC12ZWN0b3IiLCJkZXZpY2VfaWQiOiJjbGllbnQtdmVjdG9yIiwic2Vzc2lvbl9raW5kIjoibWFuYWdlZCIsInNlc3Npb25faWQiOiJtYW5hZ2VkLXZlY3RvciIsInRhcmdldF9kZXZpY2VfaWQiOiJkYWVtb24tdmVjdG9yIiwiYWxsb3dlZF9vcGVyYXRpb25zIjpbIm9mZmVyIiwiY2FuZGlkYXRlIl0sImlzc3VlZF9hdF91bml4IjoxNzgzNzU2ODAwLCJleHBpcmVzX2F0X3VuaXgiOjE3ODM3NTY4OTB9.ilWUwQlkNg2VymBOs20vEnCxjsSbnhZ4bZK1IUPrnwsYLxyTCYOfvGT6BSjAMvdf03-Q5S5HO0626BkGcG6_Ag"
	if got := string(ticket.Bytes()); got != expected {
		t.Fatalf("canonical vector changed:\n%s", got)
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
