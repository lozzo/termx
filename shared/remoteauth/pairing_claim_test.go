package remoteauth

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"testing"
	"time"

	"github.com/muxvia/muxvia/proto/remoteauthpb"
)

func TestPairingClaimIsMemoryOnlySingleUseAndClientBound(t *testing.T) {
	now := time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
	identity, err := NewIdentity("device-claim", ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x41}, ed25519.SeedSize)))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	store, err := LoadAccessStore(dir, identity, AccessStoreOptions{Now: fixedNow(now), Random: bytes.NewReader(bytes.Repeat([]byte{0x52}, 4096))})
	if err != nil {
		t.Fatal(err)
	}
	issued, err := store.IssuePairingClaim(PairingIssueOptions{
		Label: "Claim daemon", Scope: Scope{TerminalID: "terminal-a"}, TicketTTL: 10 * time.Minute, GrantLifetime: time.Hour, Now: now,
		Routes: []*remoteauthpb.EndpointRouteConfigV1{{SchemaVersion: 1, RouteId: "direct", Enabled: true, Route: &remoteauthpb.EndpointRouteConfigV1_DirectWebrtcTcp{DirectWebrtcTcp: &remoteauthpb.DirectWebRTCTCPRouteConfig{SignalingAddresses: []string{"127.0.0.1:4040"}, IceTcpAddresses: []string{"127.0.0.1:4041"}}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(issued.Offer.GetClaim()) != pairingClaimBytes || len(issued.OfferPayload) > maxPairingClaimOfferBytes || issued.ClaimCode == "" {
		t.Fatalf("unexpected claim result: claim=%d payload=%d code=%q", len(issued.Offer.GetClaim()), len(issued.OfferPayload), issued.ClaimCode)
	}
	decodedCode, err := DecodePairingClaimCode(issued.ClaimCode)
	if err != nil || !bytes.Equal(decodedCode, issued.OfferPayload) {
		t.Fatalf("claim code round trip: decoded=%x err=%v", decodedCode, err)
	}
	if bytes.Contains(issued.OfferPayload, []byte(issued.Claims.TicketID)) || bytes.Contains(issued.OfferPayload, []byte("terminal-a")) {
		t.Fatal("claim offer leaked ticket or scope data")
	}
	clientA, _ := GenerateClientAccessIdentity("endpoint-a", bytes.NewReader(bytes.Repeat([]byte{0x61}, 64)))
	clientB, _ := GenerateClientAccessIdentity("endpoint-b", bytes.NewReader(bytes.Repeat([]byte{0x62}, 64)))
	result, bundle, err := store.RedeemPairingClaim(issued.OfferPayload, clientA.PublicKey, "phone-a", now.Add(time.Minute))
	if err != nil || result.Scope.TerminalID != "terminal-a" || !bytes.Equal(bundle, issued.BundlePayload) {
		t.Fatalf("RedeemPairingClaim: result=%#v bundle=%v err=%v", result, bytes.Equal(bundle, issued.BundlePayload), err)
	}
	if _, _, err := store.RedeemPairingClaim(issued.OfferPayload, clientB.PublicKey, "phone-b", now.Add(2*time.Minute)); !errors.Is(err, ErrPairingTicketConsumed) {
		t.Fatalf("second client error = %v", err)
	}
	if _, _, err := store.RedeemPairingClaim(issued.OfferPayload, clientA.PublicKey, "phone-a", now.Add(11*time.Minute)); err != nil {
		t.Fatalf("same-client delivery recovery failed: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reloaded, err := LoadAccessStore(dir, identity, AccessStoreOptions{Now: fixedNow(now.Add(3 * time.Minute))})
	if err != nil {
		t.Fatal(err)
	}
	defer reloaded.Close()
	if _, err := reloaded.ResolvePairingClaimForExchange(issued.OfferPayload, clientA.PublicKey, now.Add(3*time.Minute)); !errors.Is(err, ErrPairingClaimUnavailable) {
		t.Fatalf("claim survived daemon restart: %v", err)
	}
}

func TestPairingClaimRejectsExpiryAndWrongDaemon(t *testing.T) {
	now := time.Date(2026, 7, 23, 11, 0, 0, 0, time.UTC)
	identity, _ := NewIdentity("device-a", ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x31}, ed25519.SeedSize)))
	store, err := LoadAccessStore(t.TempDir(), identity, AccessStoreOptions{Now: fixedNow(now)})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	issued, err := store.IssuePairingClaim(PairingIssueOptions{Scope: FullDaemonScope(), TicketTTL: time.Minute, Routes: []*remoteauthpb.EndpointRouteConfigV1{{SchemaVersion: 1, RouteId: "cloud", Enabled: true, Route: &remoteauthpb.EndpointRouteConfigV1_ManagedWebrtc{ManagedWebrtc: &remoteauthpb.ManagedWebRTCRouteConfig{TargetDeviceId: identity.DeviceID}}}}})
	if err != nil {
		t.Fatal(err)
	}
	client, _ := GenerateClientAccessIdentity("endpoint-a", nil)
	if _, err := store.ResolvePairingClaimForExchange(issued.OfferPayload, client.PublicKey, now.Add(2*time.Minute)); !errors.Is(err, ErrPairingTicketExpired) {
		t.Fatalf("expired claim error = %v", err)
	}
	otherIdentity, _ := NewIdentity("device-b", ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x32}, ed25519.SeedSize)))
	other, err := LoadAccessStore(t.TempDir(), otherIdentity, AccessStoreOptions{Now: fixedNow(now)})
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if _, err := other.ResolvePairingClaimForExchange(issued.OfferPayload, client.PublicKey, now); !errors.Is(err, ErrPairingClaimUnavailable) {
		t.Fatalf("wrong daemon error = %v", err)
	}
}
