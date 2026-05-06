package pairing

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-remote/session/token"
)

func TestCreateSessionReturnsQRCodeFieldsAndStrongSecret(t *testing.T) {
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	manager, _, _ := testManager(&now)

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
	if session.LocalPairURL != "http://127.0.0.1:18888/api/local/pair" {
		t.Fatalf("unexpected local pair url %q", session.LocalPairURL)
	}
	if session.ExpiresAt.Sub(now) != 5*time.Minute {
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

func TestClaimSessionIssuesSessionTokenAndConsumesSecret(t *testing.T) {
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	manager, cfg, machineSecret := testManager(&now)
	session, err := manager.CreateSession(5 * time.Minute)
	if err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}

	resp, err := manager.ClaimSession(ClaimRequest{
		PairSessionID:         session.PairSessionID,
		PairSecret:            session.PairSecret,
		AppDeviceID:           "appdev_test",
		RequestedCapabilities: []string{"terminal", "file_manager"},
	})
	if err != nil {
		t.Fatalf("ClaimSession returned error: %v", err)
	}
	if resp.MachineID != cfg.MachineID || resp.MachineName != cfg.MachineName {
		t.Fatalf("unexpected machine info: %#v", resp)
	}
	if resp.SessionToken == "" {
		t.Fatal("session_token is empty")
	}
	claims, err := token.Verify(resp.SessionToken, machineSecret, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("session token did not verify: %v", err)
	}
	if claims.SessionID != session.PairSessionID || claims.MachineID != cfg.MachineID {
		t.Fatalf("claims = %+v", claims)
	}
	if got := strings.Join(claims.Capabilities, ","); got != "file_manager,terminal" {
		t.Fatalf("token capabilities = %q", got)
	}
	if resp.ExpiresAt.Sub(now) != defaultTokenTTL {
		t.Fatalf("unexpected token expiry %s", resp.ExpiresAt)
	}

	if _, err := manager.ClaimSession(ClaimRequest{
		PairSessionID:         session.PairSessionID,
		PairSecret:            session.PairSecret,
		AppDeviceID:           "appdev_test",
		RequestedCapabilities: []string{"terminal"},
	}); err == nil {
		t.Fatal("expected pair secret to be single-use")
	}
}

func TestClaimSessionAllowsTerminalManagementCapabilitySeparatelyFromFileManager(t *testing.T) {
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	manager, _, machineSecret := testManager(&now)
	session, err := manager.CreateSession(5 * time.Minute)
	if err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}

	resp, err := manager.ClaimSession(ClaimRequest{
		PairSessionID:         session.PairSessionID,
		PairSecret:            session.PairSecret,
		AppDeviceID:           "appdev_management",
		RequestedCapabilities: []string{"terminal", "file_manager", "terminal_management"},
	})
	if err != nil {
		t.Fatalf("ClaimSession returned error: %v", err)
	}
	claims, err := token.Verify(resp.SessionToken, machineSecret, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("session token did not verify: %v", err)
	}
	if got := strings.Join(claims.Capabilities, ","); got != "file_manager,terminal,terminal_management" {
		t.Fatalf("expected independent management capability, got %q", got)
	}
}

func TestClaimSessionRejectsExpiredOrWrongSecret(t *testing.T) {
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	manager, _, _ := testManager(&now)
	session, err := manager.CreateSession(5 * time.Minute)
	if err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}

	if _, err := manager.ClaimSession(ClaimRequest{
		PairSessionID:         session.PairSessionID,
		PairSecret:            "wrong-secret",
		AppDeviceID:           "appdev_test",
		RequestedCapabilities: []string{"terminal"},
	}); err == nil {
		t.Fatal("expected wrong secret to be rejected")
	}

	now = now.Add(6 * time.Minute)
	if _, err := manager.ClaimSession(ClaimRequest{
		PairSessionID:         session.PairSessionID,
		PairSecret:            session.PairSecret,
		AppDeviceID:           "appdev_test",
		RequestedCapabilities: []string{"terminal"},
	}); err == nil {
		t.Fatal("expected expired session to be rejected")
	}
}

func TestClaimSessionDoesNotConsumeSecretWhenCapabilitiesAreInvalid(t *testing.T) {
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	manager, _, machineSecret := testManager(&now)
	session, err := manager.CreateSession(5 * time.Minute)
	if err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}

	if _, err := manager.ClaimSession(ClaimRequest{
		PairSessionID:         session.PairSessionID,
		PairSecret:            session.PairSecret,
		AppDeviceID:           "appdev_test",
		RequestedCapabilities: []string{"terminal", "admin"},
	}); err == nil {
		t.Fatal("expected unsupported capability to be rejected")
	}

	resp, err := manager.ClaimSession(ClaimRequest{
		PairSessionID:         session.PairSessionID,
		PairSecret:            session.PairSecret,
		AppDeviceID:           "appdev_test",
		RequestedCapabilities: []string{"terminal"},
	})
	if err != nil {
		t.Fatalf("expected valid retry to consume session: %v", err)
	}
	claims, err := token.Verify(resp.SessionToken, machineSecret, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("session token did not verify: %v", err)
	}
	if len(claims.Capabilities) != 1 || claims.Capabilities[0] != "terminal" {
		t.Fatalf("expected only terminal capability, got %#v", claims.Capabilities)
	}
}

func TestClaimSessionUsesConfiguredTokenTTL(t *testing.T) {
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	manager, _, machineSecret := testManager(&now)
	manager.cfg.DefaultTokenTTL = time.Hour
	session, err := manager.CreateSession(5 * time.Minute)
	if err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}

	resp, err := manager.ClaimSession(ClaimRequest{
		PairSessionID:         session.PairSessionID,
		PairSecret:            session.PairSecret,
		AppDeviceID:           "appdev_test",
		RequestedCapabilities: []string{"terminal"},
	})
	if err != nil {
		t.Fatalf("ClaimSession returned error: %v", err)
	}
	if resp.ExpiresAt.Sub(now) != time.Hour {
		t.Fatalf("expected one-hour token expiry, got %s", resp.ExpiresAt)
	}
	if _, err := token.Verify(resp.SessionToken, machineSecret, now.Add(2*time.Hour)); err == nil {
		t.Fatal("expected token to expire after configured ttl")
	}
}

func testManager(now *time.Time) (*Manager, Config, []byte) {
	machineSecret := []byte("0123456789abcdef0123456789abcdef")
	cfg := Config{
		MachineID:     "mach_test",
		MachineName:   "MacBook Pro",
		MachineSecret: machineSecret,
		LocalPairURL:  "http://127.0.0.1:18888/api/local/pair",
		Now: func() time.Time {
			return *now
		},
	}
	return NewManager(cfg), cfg, machineSecret
}
