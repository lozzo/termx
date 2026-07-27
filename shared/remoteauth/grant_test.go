package remoteauth

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestGrantV2RoundTripBindsDaemonSubjectAndManageCapability(t *testing.T) {
	daemonPublic, daemonPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	client, err := GenerateClientAccessIdentity("endpoint-1", rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	grant, err := Issue(daemonPrivate, Claims{
		GrantID: "grant-1", IssuerDeviceID: "device-1", SubjectKeyFingerprint: client.Fingerprint,
		Scope: Scope{TerminalID: "term-1", ManageClientAccess: true}, IssuedAt: now, ExpiresAt: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	claims, err := Verify(grant, Fingerprint(daemonPublic), now.Add(time.Minute), nil)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.Version != 2 || claims.SubjectKeyFingerprint != client.Fingerprint || claims.Scope.TerminalID != "term-1" || !claims.Scope.ManageClientAccess || claims.RevocationID != "grant-1" || claims.Nonce == "" {
		t.Fatalf("grant lost v2 bindings: %#v", claims)
	}
}

func TestGrantV2AcceptsMuxviaEnvelopeDuringBrandMigration(t *testing.T) {
	daemonPublic, daemonPrivate, _ := ed25519.GenerateKey(rand.Reader)
	client, _ := GenerateClientAccessIdentity("endpoint-legacy", rand.Reader)
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	grant, err := Issue(daemonPrivate, Claims{
		GrantID: "grant-legacy", IssuerDeviceID: "device-legacy", SubjectKeyFingerprint: client.Fingerprint,
		Scope: FullDaemonScope(), IssuedAt: now, ExpiresAt: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(grant, ".")
	parts[0] = "muxvia-grant-v2"
	parts[3] = base64.RawURLEncoding.EncodeToString(ed25519.Sign(daemonPrivate, []byte(strings.Join(parts[:3], "."))))
	if _, err := Verify(strings.Join(parts, "."), Fingerprint(daemonPublic), now.Add(time.Minute), nil); err != nil {
		t.Fatalf("legacy muxvia grant rejected during migration: %v", err)
	}
}

func TestGrantV2RejectsMissingSubjectV1ExpiryAndRevocation(t *testing.T) {
	daemonPublic, daemonPrivate, _ := ed25519.GenerateKey(rand.Reader)
	client, _ := GenerateClientAccessIdentity("endpoint-1", rand.Reader)
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	if _, err := Issue(daemonPrivate, Claims{
		GrantID: "missing-subject", IssuerDeviceID: "device-1", Scope: Scope{AllowDaemon: true},
		IssuedAt: now, ExpiresAt: now.Add(time.Hour),
	}); !errors.Is(err, ErrGrantMalformed) {
		t.Fatalf("missing subject error = %v", err)
	}
	grant, err := Issue(daemonPrivate, Claims{
		GrantID: "grant-1", IssuerDeviceID: "device-1", SubjectKeyFingerprint: client.Fingerprint,
		Scope: Scope{AllowDaemon: true}, IssuedAt: now, ExpiresAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Verify("anytty-grant-v1.legacy.key.signature", Fingerprint(daemonPublic), now, nil); !errors.Is(err, ErrGrantMalformed) {
		t.Fatalf("v1 grant error = %v", err)
	}
	if _, err := Verify(grant, Fingerprint(daemonPublic), now.Add(2*time.Minute), nil); !errors.Is(err, ErrGrantExpired) {
		t.Fatalf("expiry error = %v", err)
	}
	revocations := revocationCheckerFunc(func(revocationID string) bool { return revocationID == "grant-1" })
	if _, err := Verify(grant, Fingerprint(daemonPublic), now, revocations); !errors.Is(err, ErrGrantRevoked) {
		t.Fatalf("revocation error = %v", err)
	}
}

type revocationCheckerFunc func(string) bool

func (check revocationCheckerFunc) Revoked(revocationID string) bool { return check(revocationID) }

func TestGrantScopeKeepsManageClientAccessIndependent(t *testing.T) {
	_, daemonPrivate, _ := ed25519.GenerateKey(rand.Reader)
	client, _ := GenerateClientAccessIdentity("endpoint-1", rand.Reader)
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	for _, scope := range []Scope{
		{},
		{ManageClientAccess: true},
		{AllowDaemon: true, TerminalID: "term-1"},
		{TerminalID: "term-1", FileReadContent: true},
		{TerminalID: string([]byte{0xff})},
	} {
		if _, err := Issue(daemonPrivate, Claims{
			GrantID: "grant-invalid", IssuerDeviceID: "device-1", SubjectKeyFingerprint: client.Fingerprint,
			Scope: scope, IssuedAt: now, ExpiresAt: now.Add(time.Hour),
		}); !errors.Is(err, ErrGrantScopeInvalid) {
			t.Fatalf("scope %#v error = %v", scope, err)
		}
	}
	for _, scope := range []Scope{
		{AllowDaemon: true},
		{AllowDaemon: true, ManageClientAccess: true},
		{TerminalID: "term-1", ManageClientAccess: true},
	} {
		if _, err := Issue(daemonPrivate, Claims{
			GrantID: "grant-valid", IssuerDeviceID: "device-1", SubjectKeyFingerprint: client.Fingerprint,
			Scope: scope, IssuedAt: now, ExpiresAt: now.Add(time.Hour),
		}); err != nil {
			t.Fatalf("scope %#v should be valid: %v", scope, err)
		}
	}
}
