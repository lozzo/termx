package cert

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-core/internal/remote/identity"
)

func TestCanonicalPayloadIsStableAndRejectsInvalidPayload(t *testing.T) {
	payload := testPayload(t)

	first, err := CanonicalPayload(payload)
	if err != nil {
		t.Fatalf("CanonicalPayload returned error: %v", err)
	}
	second, err := CanonicalPayload(payload)
	if err != nil {
		t.Fatalf("CanonicalPayload second call returned error: %v", err)
	}
	if string(first) != string(second) {
		t.Fatalf("expected stable canonical payload, got\n%s\n%s", first, second)
	}
	if !strings.Contains(string(first), `"machine_id":"mach_test"`) {
		t.Fatalf("expected canonical payload to contain machine_id, got %s", first)
	}
	if strings.Contains(string(first), "\n") || strings.Contains(string(first), ": ") || strings.Contains(string(first), ", ") {
		t.Fatalf("expected compact canonical payload, got %q", string(first))
	}

	payload.Version = 0
	if _, err := CanonicalPayload(payload); err == nil {
		t.Fatal("expected invalid payload to be rejected")
	}
}

func TestCanonicalPayloadRejectsInvalidAppPublicKeyAndDuplicateCapabilities(t *testing.T) {
	payload := testPayload(t)
	payload.AppPublicKey = base64.StdEncoding.EncodeToString([]byte("short"))
	if _, err := CanonicalPayload(payload); err == nil {
		t.Fatal("expected short app public key to be rejected")
	}

	payload = testPayload(t)
	payload.Capabilities = []string{"terminal", "terminal"}
	if _, err := CanonicalPayload(payload); err == nil {
		t.Fatal("expected duplicate capabilities to be rejected")
	}
}

func TestSignAndVerifyAppCertificate(t *testing.T) {
	machineKey, err := identity.LoadOrCreateMachineKey(t.TempDir())
	if err != nil {
		t.Fatalf("LoadOrCreateMachineKey returned error: %v", err)
	}
	payload := testPayload(t)
	payload.MachinePublicKeyFingerprint = identity.MachinePublicKeyFingerprint(machineKey.PublicKey)

	envelope, err := SignAppCertificate(payload, machineKey)
	if err != nil {
		t.Fatalf("SignAppCertificate returned error: %v", err)
	}
	if envelope.Signature == "" {
		t.Fatal("expected signature to be set")
	}
	if err := VerifyAppCertificate(envelope, machineKey.PublicKey, time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("VerifyAppCertificate returned error: %v", err)
	}

	envelope.Payload.AppName = "Mallory iPhone"
	if err := VerifyAppCertificate(envelope, machineKey.PublicKey, time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("expected tampered certificate payload to be rejected")
	}
}

func TestSignAppCertificateStampsMachineFingerprint(t *testing.T) {
	machineKey, err := identity.LoadOrCreateMachineKey(t.TempDir())
	if err != nil {
		t.Fatalf("LoadOrCreateMachineKey returned error: %v", err)
	}
	payload := testPayload(t)
	payload.MachinePublicKeyFingerprint = "sha256:not-the-machine-key"

	envelope, err := SignAppCertificate(payload, machineKey)
	if err != nil {
		t.Fatalf("SignAppCertificate returned error: %v", err)
	}
	expected := identity.MachinePublicKeyFingerprint(machineKey.PublicKey)
	if envelope.Payload.MachinePublicKeyFingerprint != expected {
		t.Fatalf("expected signer to stamp machine fingerprint %q, got %q", expected, envelope.Payload.MachinePublicKeyFingerprint)
	}
	if err := VerifyAppCertificate(envelope, machineKey.PublicKey, time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("VerifyAppCertificate returned error for stamped certificate: %v", err)
	}
}

func TestVerifyAppCertificateRejectsWrongMachineAndExpiredCertificate(t *testing.T) {
	machineKey, err := identity.LoadOrCreateMachineKey(t.TempDir())
	if err != nil {
		t.Fatalf("LoadOrCreateMachineKey returned error: %v", err)
	}
	otherKey, err := identity.LoadOrCreateMachineKey(t.TempDir())
	if err != nil {
		t.Fatalf("LoadOrCreateMachineKey returned error: %v", err)
	}
	payload := testPayload(t)
	payload.MachinePublicKeyFingerprint = identity.MachinePublicKeyFingerprint(machineKey.PublicKey)

	envelope, err := SignAppCertificate(payload, machineKey)
	if err != nil {
		t.Fatalf("SignAppCertificate returned error: %v", err)
	}
	if err := VerifyAppCertificate(envelope, otherKey.PublicKey, time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("expected wrong machine public key to be rejected")
	}
	if err := VerifyAppCertificate(envelope, machineKey.PublicKey, time.Date(2027, 5, 2, 0, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("expected expired certificate to be rejected")
	}
}

func TestReplayWindowRejectsReusedNonceAndStaleTimestamp(t *testing.T) {
	window := NewReplayWindow(5 * time.Minute)
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)

	if err := window.Accept("nonce-1", now, now); err != nil {
		t.Fatalf("Accept returned error: %v", err)
	}
	if err := window.Accept("nonce-1", now, now); err == nil {
		t.Fatal("expected reused nonce to be rejected")
	}
	if err := window.Accept("nonce-2", now.Add(-10*time.Minute), now); err == nil {
		t.Fatal("expected stale timestamp to be rejected")
	}
	if err := window.Accept("nonce-3", now.Add(10*time.Minute), now); err == nil {
		t.Fatal("expected future timestamp outside skew to be rejected")
	}
	if err := window.Accept("", now, now); err == nil {
		t.Fatal("expected empty nonce to be rejected")
	}
}

func testPayload(t *testing.T) AppCertificatePayload {
	t.Helper()
	appPublic, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey returned error: %v", err)
	}
	return AppCertificatePayload{
		Version:                     1,
		CertID:                      "cert_test",
		MachineID:                   "mach_test",
		MachinePublicKeyFingerprint: "sha256:placeholder",
		AppDeviceID:                 "appdev_test",
		AppPublicKey:                base64.StdEncoding.EncodeToString(appPublic),
		AppName:                     "Alice iPhone",
		Capabilities:                []string{"terminal", "file_manager"},
		IssuedAt:                    time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		ExpiresAt:                   time.Date(2027, 5, 1, 0, 0, 0, 0, time.UTC),
	}
}
