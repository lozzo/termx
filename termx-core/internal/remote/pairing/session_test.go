package pairing

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-core/internal/remote/cert"
	"github.com/lozzow/termx/termx-core/internal/remote/identity"
)

func TestCreateSessionReturnsQRCodeFieldsAndStrongSecret(t *testing.T) {
	machineKey, err := identity.LoadOrCreateMachineKey(t.TempDir())
	if err != nil {
		t.Fatalf("LoadOrCreateMachineKey returned error: %v", err)
	}
	manager := NewManager(Config{
		MachineID:    "mach_test",
		MachineName:  "MacBook Pro",
		MachineKey:   machineKey,
		LocalPairURL: "http://127.0.0.1:18888/api/local/pair",
		Now: func() time.Time {
			return time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
		},
	})

	session, err := manager.CreateSession(5 * time.Minute)
	if err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}
	if !strings.HasPrefix(session.PairSessionID, "pair_") {
		t.Fatalf("expected pair session id prefix, got %q", session.PairSessionID)
	}
	if session.MachineID != "mach_test" || session.MachineName != "MacBook Pro" {
		t.Fatalf("unexpected machine info: %#v", session)
	}
	if session.MachinePublicKeyFingerprint != identity.MachinePublicKeyFingerprint(machineKey.PublicKey) {
		t.Fatalf("unexpected machine fingerprint %q", session.MachinePublicKeyFingerprint)
	}
	if session.LocalPairURL != "http://127.0.0.1:18888/api/local/pair" {
		t.Fatalf("unexpected local pair url %q", session.LocalPairURL)
	}
	if session.ExpiresAt.Sub(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)) != 5*time.Minute {
		t.Fatalf("unexpected expiry %s", session.ExpiresAt)
	}
	secret, err := base64.RawURLEncoding.DecodeString(session.PairSecret)
	if err != nil {
		t.Fatalf("pair secret is not base64url: %v", err)
	}
	if len(secret) < 24 {
		t.Fatalf("expected at least 192-bit pair secret, got %d bytes", len(secret))
	}
}

func TestClaimSessionIssuesCertificateAndConsumesSecret(t *testing.T) {
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	manager, machineKey := testManager(t, &now)
	session, err := manager.CreateSession(5 * time.Minute)
	if err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}
	appPublic := testAppPublicKey(t)

	resp, err := manager.ClaimSession(ClaimRequest{
		PairSessionID:          session.PairSessionID,
		PairSecret:             session.PairSecret,
		AppDeviceID:            "appdev_test",
		AppName:                "Alice iPhone",
		AppPublicKey:           appPublic,
		RequestedCapabilities:  []string{"terminal", "file_manager"},
		CertificateTTL:         365 * 24 * time.Hour,
		CertificateIDGenerator: func() string { return "cert_test" },
	})
	if err != nil {
		t.Fatalf("ClaimSession returned error: %v", err)
	}
	if resp.MachineID != "mach_test" || resp.MachineName != "MacBook Pro" {
		t.Fatalf("unexpected machine info: %#v", resp)
	}
	if resp.MachinePublicKey != base64.StdEncoding.EncodeToString(machineKey.PublicKey) {
		t.Fatalf("unexpected machine public key %q", resp.MachinePublicKey)
	}
	if err := cert.VerifyAppCertificate(resp.AppCertificate, machineKey.PublicKey, now.Add(time.Hour)); err != nil {
		t.Fatalf("issued certificate did not verify: %v", err)
	}
	if resp.AppCertificate.Payload.CertID != "cert_test" {
		t.Fatalf("expected cert id cert_test, got %q", resp.AppCertificate.Payload.CertID)
	}
	if resp.AppCertificate.Payload.MachineID != "mach_test" {
		t.Fatalf("expected machine id mach_test, got %q", resp.AppCertificate.Payload.MachineID)
	}
	if resp.AppCertificate.Payload.AppPublicKey != appPublic {
		t.Fatalf("expected app public key to be embedded")
	}

	if _, err := manager.ClaimSession(ClaimRequest{
		PairSessionID:         session.PairSessionID,
		PairSecret:            session.PairSecret,
		AppDeviceID:           "appdev_test",
		AppName:               "Alice iPhone",
		AppPublicKey:          appPublic,
		RequestedCapabilities: []string{"terminal"},
	}); err == nil {
		t.Fatal("expected pair secret to be single-use")
	}
}

func TestClaimSessionAllowsTerminalManagementCapabilitySeparatelyFromFileManager(t *testing.T) {
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	manager, machineKey := testManager(t, &now)
	session, err := manager.CreateSession(5 * time.Minute)
	if err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}

	resp, err := manager.ClaimSession(ClaimRequest{
		PairSessionID:          session.PairSessionID,
		PairSecret:             session.PairSecret,
		AppDeviceID:            "appdev_management",
		AppName:                "Management App",
		AppPublicKey:           testAppPublicKey(t),
		RequestedCapabilities:  []string{"terminal", "file_manager", "terminal_management"},
		CertificateTTL:         time.Hour,
		CertificateIDGenerator: func() string { return "cert_management" },
	})
	if err != nil {
		t.Fatalf("ClaimSession returned error: %v", err)
	}
	if err := cert.VerifyAppCertificate(resp.AppCertificate, machineKey.PublicKey, now.Add(time.Minute)); err != nil {
		t.Fatalf("issued certificate did not verify: %v", err)
	}
	if got := strings.Join(resp.AppCertificate.Payload.Capabilities, ","); !strings.Contains(got, "terminal") || !strings.Contains(got, "file_manager") || !strings.Contains(got, "terminal_management") {
		t.Fatalf("expected independent management capability, got %q", got)
	}
}

func TestClaimSessionRejectsExpiredOrWrongSecret(t *testing.T) {
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	manager, _ := testManager(t, &now)
	session, err := manager.CreateSession(5 * time.Minute)
	if err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}
	appPublic := testAppPublicKey(t)

	if _, err := manager.ClaimSession(ClaimRequest{
		PairSessionID:         session.PairSessionID,
		PairSecret:            "wrong-secret",
		AppDeviceID:           "appdev_test",
		AppName:               "Alice iPhone",
		AppPublicKey:          appPublic,
		RequestedCapabilities: []string{"terminal"},
	}); err == nil {
		t.Fatal("expected wrong secret to be rejected")
	}

	now = now.Add(6 * time.Minute)
	if _, err := manager.ClaimSession(ClaimRequest{
		PairSessionID:         session.PairSessionID,
		PairSecret:            session.PairSecret,
		AppDeviceID:           "appdev_test",
		AppName:               "Alice iPhone",
		AppPublicKey:          appPublic,
		RequestedCapabilities: []string{"terminal"},
	}); err == nil {
		t.Fatal("expected expired session to be rejected")
	}
}

func TestClaimSessionDoesNotConsumeSecretWhenCertificateRequestIsInvalid(t *testing.T) {
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	manager, machineKey := testManager(t, &now)
	session, err := manager.CreateSession(5 * time.Minute)
	if err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}

	if _, err := manager.ClaimSession(ClaimRequest{
		PairSessionID:         session.PairSessionID,
		PairSecret:            session.PairSecret,
		AppDeviceID:           "appdev_test",
		AppName:               "Alice iPhone",
		AppPublicKey:          base64.StdEncoding.EncodeToString([]byte("short")),
		RequestedCapabilities: []string{"terminal"},
	}); err == nil {
		t.Fatal("expected invalid app public key to be rejected")
	}

	resp, err := manager.ClaimSession(ClaimRequest{
		PairSessionID:         session.PairSessionID,
		PairSecret:            session.PairSecret,
		AppDeviceID:           "appdev_test",
		AppName:               "Alice iPhone",
		AppPublicKey:          testAppPublicKey(t),
		RequestedCapabilities: []string{"terminal"},
	})
	if err != nil {
		t.Fatalf("expected valid retry to consume session: %v", err)
	}
	if err := cert.VerifyAppCertificate(resp.AppCertificate, machineKey.PublicKey, now.Add(time.Hour)); err != nil {
		t.Fatalf("issued certificate did not verify: %v", err)
	}
}

func TestClaimSessionRejectsUnsupportedCapabilitiesWithoutConsumingSecret(t *testing.T) {
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	manager, machineKey := testManager(t, &now)
	session, err := manager.CreateSession(5 * time.Minute)
	if err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}
	appPublic := testAppPublicKey(t)

	if _, err := manager.ClaimSession(ClaimRequest{
		PairSessionID:         session.PairSessionID,
		PairSecret:            session.PairSecret,
		AppDeviceID:           "appdev_test",
		AppName:               "Alice iPhone",
		AppPublicKey:          appPublic,
		RequestedCapabilities: []string{"terminal", "admin"},
	}); err == nil {
		t.Fatal("expected unsupported capability to be rejected")
	}

	resp, err := manager.ClaimSession(ClaimRequest{
		PairSessionID:         session.PairSessionID,
		PairSecret:            session.PairSecret,
		AppDeviceID:           "appdev_test",
		AppName:               "Alice iPhone",
		AppPublicKey:          appPublic,
		RequestedCapabilities: []string{"terminal"},
	})
	if err != nil {
		t.Fatalf("expected valid retry to consume session: %v", err)
	}
	if err := cert.VerifyAppCertificate(resp.AppCertificate, machineKey.PublicKey, now.Add(time.Hour)); err != nil {
		t.Fatalf("issued certificate did not verify: %v", err)
	}
	if len(resp.AppCertificate.Payload.Capabilities) != 1 || resp.AppCertificate.Payload.Capabilities[0] != "terminal" {
		t.Fatalf("expected only terminal capability, got %#v", resp.AppCertificate.Payload.Capabilities)
	}
}

func testManager(t *testing.T, now *time.Time) (*Manager, identity.MachineKey) {
	t.Helper()
	machineKey, err := identity.LoadOrCreateMachineKey(t.TempDir())
	if err != nil {
		t.Fatalf("LoadOrCreateMachineKey returned error: %v", err)
	}
	return NewManager(Config{
		MachineID:    "mach_test",
		MachineName:  "MacBook Pro",
		MachineKey:   machineKey,
		LocalPairURL: "http://127.0.0.1:18888/api/local/pair",
		Now: func() time.Time {
			return *now
		},
	}), machineKey
}

func testAppPublicKey(t *testing.T) string {
	t.Helper()
	appPublic, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey returned error: %v", err)
	}
	return base64.StdEncoding.EncodeToString(appPublic)
}
