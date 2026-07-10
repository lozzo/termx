package remoteauth

import (
	"crypto/ed25519"
	"crypto/rand"
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
	grant, err := Issue(privateKey, Claims{GrantID: "grant-1", DeviceID: "device-1", Scope: Scope{TerminalID: "term-1"}, IssuedAt: now, ExpiresAt: now.Add(time.Hour)})
	if err != nil {
		t.Fatalf("issue grant: %v", err)
	}
	claims, err := Verify(grant, Fingerprint(publicKey), now.Add(time.Minute), nil)
	if err != nil {
		t.Fatalf("verify grant: %v", err)
	}
	if claims.DeviceFingerprint != Fingerprint(publicKey) || claims.Scope.TerminalID != "term-1" {
		t.Fatalf("grant lost identity or scope: %#v", claims)
	}
}

func TestGrantRejectsFingerprintMismatchExpiryRevocationAndLegacyToken(t *testing.T) {
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	otherPublicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	grant, err := Issue(privateKey, Claims{GrantID: "grant-1", DeviceID: "device-1", Scope: Scope{TerminalID: "term-1"}, IssuedAt: now, ExpiresAt: now.Add(time.Minute)})
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
		if _, err := Issue(privateKey, Claims{GrantID: "grant-1", DeviceID: "device-1", Scope: scope, IssuedAt: now, ExpiresAt: now.Add(time.Hour)}); err == nil {
			t.Fatalf("expected invalid scope %#v rejection", scope)
		}
	}
	if _, err := Issue(privateKey, Claims{GrantID: "grant-daemon", DeviceID: "device-1", Scope: Scope{AllowDaemon: true}, IssuedAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatalf("explicit daemon scope should be valid: %v", err)
	}
}
