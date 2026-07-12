package remoteauth

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestGrantRoundTripBindsDeviceFingerprintAndScope(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	grant, err := Issue(privateKey, Claims{GrantID: "grant-1", IssuerDeviceID: "device-1", Scope: Scope{TerminalID: "term-1"}, IssuedAt: now, ExpiresAt: now.Add(time.Hour)})
	if err != nil {
		t.Fatalf("issue grant: %v", err)
	}
	claims, err := Verify(grant, Fingerprint(publicKey), now.Add(time.Minute), nil)
	if err != nil {
		t.Fatalf("verify grant: %v", err)
	}
	if claims.IssuerDeviceFingerprint != Fingerprint(publicKey) || claims.Scope.TerminalID != "term-1" || claims.RevocationID != "grant-1" || claims.Nonce == "" {
		t.Fatalf("grant lost identity or scope: %#v", claims)
	}
}

func TestGrantRejectsFingerprintMismatchExpiryRevocationAndLegacyToken(t *testing.T) {
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	otherPublicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	grant, err := Issue(privateKey, Claims{GrantID: "grant-1", IssuerDeviceID: "device-1", Scope: Scope{TerminalID: "term-1"}, IssuedAt: now, ExpiresAt: now.Add(time.Minute)})
	if err != nil {
		t.Fatalf("issue grant: %v", err)
	}
	if _, err := Verify(grant, Fingerprint(otherPublicKey), now, nil); err == nil || !strings.Contains(err.Error(), "fingerprint") {
		t.Fatalf("expected fingerprint rejection, got %v", err)
	}
	if _, err := Verify(grant, Fingerprint(publicKey), now.Add(2*time.Minute), nil); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expected expiry rejection, got %v", err)
	}
	revocations := NewRevocations()
	revocations.Revoke("grant-1")
	if _, err := Verify(grant, Fingerprint(publicKey), now, revocations); err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("expected revocation rejection, got %v", err)
	}
	if _, err := Verify("legacy-session-token", Fingerprint(publicKey), now, nil); err == nil {
		t.Fatal("legacy session token must not be accepted as capability grant")
	}
}

func TestGrantRequiresExactlyOneExplicitScope(t *testing.T) {
	_, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	for _, scope := range []Scope{
		{},
		{AllowDaemon: true, TerminalID: "term-1"},
		{AllowDaemon: true, MachineEventsOnly: true},
		{TerminalID: "term-1", MachineEventsOnly: true},
	} {
		if _, err := Issue(privateKey, Claims{GrantID: "grant-1", IssuerDeviceID: "device-1", Scope: scope, IssuedAt: now, ExpiresAt: now.Add(time.Hour)}); err == nil {
			t.Fatalf("expected invalid scope %#v rejection", scope)
		}
	}
	if _, err := Issue(privateKey, Claims{GrantID: "grant-daemon", IssuerDeviceID: "device-1", Scope: Scope{AllowDaemon: true}, IssuedAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatalf("explicit daemon scope should be valid: %v", err)
	}
}

func TestGrantRejectsFilePermissionsOutsideDaemonScope(t *testing.T) {
	_, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	_, err := Issue(privateKey, Claims{GrantID: "grant-file", IssuerDeviceID: "device-1", Scope: Scope{TerminalID: "term-1", FileReadContent: true}, IssuedAt: now, ExpiresAt: now.Add(time.Hour)})
	if !errors.Is(err, ErrGrantScopeInvalid) {
		t.Fatalf("file permission scope error = %v", err)
	}
}

func TestVerifyRejectsSignedLegacyClaimsWithoutCurrentRequiredFields(t *testing.T) {
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	legacyClaims := struct {
		GrantID           string    `json:"grant_id"`
		DeviceID          string    `json:"device_id"`
		DeviceFingerprint string    `json:"device_fingerprint"`
		Scope             Scope     `json:"scope"`
		IssuedAt          time.Time `json:"issued_at"`
		ExpiresAt         time.Time `json:"expires_at"`
	}{
		GrantID: "legacy-grant", DeviceID: "device-1", DeviceFingerprint: Fingerprint(publicKey),
		Scope: Scope{AllowDaemon: true}, IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
	}
	payload, err := json.Marshal(legacyClaims)
	if err != nil {
		t.Fatalf("marshal legacy claims: %v", err)
	}
	parts := []string{grantPrefix, base64.RawURLEncoding.EncodeToString(payload), base64.RawURLEncoding.EncodeToString(publicKey)}
	signingInput := strings.Join(parts, ".")
	legacyGrant := signingInput + "." + base64.RawURLEncoding.EncodeToString(ed25519.Sign(privateKey, []byte(signingInput)))
	if _, err := Verify(legacyGrant, Fingerprint(publicKey), now, nil); !errors.Is(err, ErrGrantMalformed) {
		t.Fatalf("Verify legacy signed grant error = %v, want ErrGrantMalformed", err)
	}
}
