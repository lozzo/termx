package remoteauth

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/anytty/anytty/proto/remoteauthpb"
)

func TestPairingClaimCodeUsesCanonicalCompressionAndBoundedDecode(t *testing.T) {
	compressible := bytes.Repeat([]byte("anytty-pairing-route-"), 100)
	code := EncodePairingClaimCode(compressible)
	envelope, err := base64.RawURLEncoding.DecodeString(code[len(PairingClaimCodePrefix):])
	if err != nil || len(envelope) == 0 || envelope[0] != pairingClaimEnvelopeDeflate {
		t.Fatalf("compressed envelope marker=%v err=%v", envelope, err)
	}
	decoded, err := DecodePairingClaimCode(code)
	if err != nil || !bytes.Equal(decoded, compressible) {
		t.Fatalf("compressed round trip len=%d err=%v", len(decoded), err)
	}
	if _, err := DecodePairingClaimCode(EncodePairingClaimCode(bytes.Repeat([]byte("x"), maxPairingClaimOfferBytes+1))); !errors.Is(err, ErrPairingClaimMalformed) {
		t.Fatalf("oversized decompression error = %v", err)
	}
	unknown := PairingClaimCodePrefix + base64.RawURLEncoding.EncodeToString([]byte{0x7f, 1, 2, 3})
	if _, err := DecodePairingClaimCode(unknown); !errors.Is(err, ErrPairingClaimMalformed) {
		t.Fatalf("unknown envelope error = %v", err)
	}
}

func TestManagedPairingClaimRequiresAndEmbedsDigestGrant(t *testing.T) {
	now := time.Date(2026, 7, 23, 9, 0, 0, 0, time.UTC)
	identity, err := NewIdentity("device-managed-claim", ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x40}, ed25519.SeedSize)))
	if err != nil {
		t.Fatal(err)
	}
	store, err := LoadAccessStore(t.TempDir(), identity, AccessStoreOptions{Now: fixedNow(now), Random: bytes.NewReader(bytes.Repeat([]byte{0x51}, 4096))})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	options := PairingIssueOptions{
		Scope: FullDaemonScope(), TicketTTL: 10 * time.Minute,
		Routes: []*remoteauthpb.EndpointRouteConfigV1{{
			SchemaVersion: 1, RouteId: "cloud", Enabled: true,
			Route: &remoteauthpb.EndpointRouteConfigV1_ManagedWebrtc{ManagedWebrtc: &remoteauthpb.ManagedWebRTCRouteConfig{TargetDeviceId: identity.DeviceID}},
		}},
	}
	if _, err := store.IssuePairingClaim(options); err == nil || !strings.Contains(err.Error(), "managed pairing grant issuer is unavailable") {
		t.Fatalf("managed claim without issuer error = %v", err)
	}
	var grantedDigest []byte
	if err := store.ConfigureManagedPairingRouteIssuer(func(digest []byte, expiresAt time.Time, issuedAt time.Time) ([]byte, []byte, error) {
		grantedDigest = append([]byte(nil), digest...)
		if !expiresAt.After(issuedAt) {
			t.Fatalf("managed grant lifetime = %v to %v", issuedAt, expiresAt)
		}
		return []byte("daemon-signed-bootstrap-grant"), []byte("edge-locator"), nil
	}); err != nil {
		t.Fatal(err)
	}
	issued, err := store.IssuePairingClaim(options)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest := sha256.Sum256(issued.Offer.GetClaim())
	managed := issued.Offer.GetRoutes()[0].GetManagedWebrtc()
	if !bytes.Equal(grantedDigest, wantDigest[:]) || !bytes.Equal(managed.GetBootstrapGrant(), []byte("daemon-signed-bootstrap-grant")) || !bytes.Equal(managed.GetEdgeLocator(), []byte("edge-locator")) {
		t.Fatalf("managed bootstrap grant digest=%x route=%#v", grantedDigest, managed)
	}
	if bytes.Contains(managed.GetBootstrapGrant(), issued.Offer.GetClaim()) {
		t.Fatal("managed bootstrap grant leaked the raw pairing claim")
	}
}

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
	claimDigest := sha256.Sum256(issued.Offer.GetClaim())
	if !store.AllowsPairingClaimDigest(claimDigest[:], clientA.PublicKey, now) || !store.AllowsPairingClaimDigest(claimDigest[:], clientB.PublicKey, now) {
		t.Fatal("unredeemed pairing claim did not admit a client holding its digest")
	}
	result, bundle, err := store.RedeemPairingClaim(issued.OfferPayload, clientA.PublicKey, "phone-a", now.Add(time.Minute))
	if err != nil || result.Scope.TerminalID != "terminal-a" || !bytes.Equal(bundle, issued.BundlePayload) {
		t.Fatalf("RedeemPairingClaim: result=%#v bundle=%v err=%v", result, bytes.Equal(bundle, issued.BundlePayload), err)
	}
	if _, _, err := store.RedeemPairingClaim(issued.OfferPayload, clientB.PublicKey, "phone-b", now.Add(2*time.Minute)); !errors.Is(err, ErrPairingTicketConsumed) {
		t.Fatalf("second client error = %v", err)
	}
	if !store.AllowsPairingClaimDigest(claimDigest[:], clientA.PublicKey, now.Add(2*time.Minute)) || store.AllowsPairingClaimDigest(claimDigest[:], clientB.PublicKey, now.Add(2*time.Minute)) {
		t.Fatal("redeemed pairing claim was not bound to the winning client key")
	}
	if _, _, err := store.RedeemPairingClaim(issued.OfferPayload, clientA.PublicKey, "phone-a", now.Add(11*time.Minute)); err != nil {
		t.Fatalf("same-client delivery recovery failed: %v", err)
	}
	if store.AllowsPairingClaimDigest(claimDigest[:], clientA.PublicKey, now.Add(26*time.Hour)) {
		t.Fatal("pairing claim remained available after delivery grace")
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
	issued, err := store.IssuePairingClaim(PairingIssueOptions{Scope: FullDaemonScope(), TicketTTL: time.Minute, Routes: []*remoteauthpb.EndpointRouteConfigV1{{SchemaVersion: 1, RouteId: "direct", Enabled: true, Route: &remoteauthpb.EndpointRouteConfigV1_DirectWebrtcTcp{DirectWebrtcTcp: &remoteauthpb.DirectWebRTCTCPRouteConfig{SignalingAddresses: []string{"127.0.0.1:4040"}, IceTcpAddresses: []string{"127.0.0.1:4041"}}}}}})
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
