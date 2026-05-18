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
		AppName:               "TermX Test App",
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
	if len(claims.Capabilities) != 0 {
		t.Fatalf("session token should not carry feature capabilities, got %#v", claims.Capabilities)
	}
	if claims.AppDeviceID != "appdev_test" || claims.AppName != "TermX Test App" {
		t.Fatalf("token app claims = %+v", claims)
	}
	openedProof, err := token.OpenAnswerProofKey(machineSecret, claims)
	if err != nil {
		t.Fatalf("answer proof key did not open: %v", err)
	}
	if openedProof != session.AnswerProofSecret {
		t.Fatalf("answer proof secret = %q", openedProof)
	}
	if resp.ExpiresAt.Sub(now) != 24*time.Hour {
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

func TestCreateSessionInvalidatesOlderUnusedSessionsForMachine(t *testing.T) {
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	manager, _, _ := testManager(&now)
	first, err := manager.CreateSession(5 * time.Minute)
	if err != nil {
		t.Fatalf("CreateSession first returned error: %v", err)
	}
	second, err := manager.CreateSession(5 * time.Minute)
	if err != nil {
		t.Fatalf("CreateSession second returned error: %v", err)
	}

	if _, err := manager.ClaimSession(ClaimRequest{
		PairSessionID: first.PairSessionID,
		PairSecret:    first.PairSecret,
		AppDeviceID:   "appdev_test",
	}); err == nil || !strings.Contains(err.Error(), "pair session not found") {
		t.Fatalf("expected first unused session to be invalidated, got %v", err)
	}
	if _, err := manager.ClaimSession(ClaimRequest{
		PairSessionID: second.PairSessionID,
		PairSecret:    second.PairSecret,
		AppDeviceID:   "appdev_test",
	}); err != nil {
		t.Fatalf("expected latest session to remain claimable: %v", err)
	}
}

func TestClaimSessionTokenIsMachineScoped(t *testing.T) {
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
		AppName:               "TermX Management App",
		RequestedCapabilities: []string{"terminal_management", "file_manager", "terminal"},
	})
	if err != nil {
		t.Fatalf("ClaimSession returned error: %v", err)
	}
	claims, err := token.Verify(resp.SessionToken, machineSecret, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("session token did not verify: %v", err)
	}
	if len(claims.Capabilities) != 0 {
		t.Fatalf("session token should not carry requested capabilities, got %#v", claims.Capabilities)
	}
	if len(claims.Paths) != 0 {
		t.Fatalf("paths = %#v", claims.Paths)
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

func TestCleanupExpiredRemovesStalePairSessions(t *testing.T) {
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	manager, _, _ := testManager(&now)
	if _, err := manager.CreateSession(5 * time.Minute); err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}

	now = now.Add(6 * time.Minute)
	if removed := manager.CleanupExpired(); removed != 1 {
		t.Fatalf("CleanupExpired removed %d sessions, want 1", removed)
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if len(manager.sessions) != 0 {
		t.Fatalf("expired sessions remained: %+v", manager.sessions)
	}
}

func TestClaimSessionDoesNotValidateRequestedCapabilities(t *testing.T) {
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	manager, _, machineSecret := testManager(&now)
	session, err := manager.CreateSession(5 * time.Minute)
	if err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}

	resp, err := manager.ClaimSession(ClaimRequest{
		PairSessionID:         session.PairSessionID,
		PairSecret:            session.PairSecret,
		AppDeviceID:           "appdev_test",
		RequestedCapabilities: []string{"terminal", "admin"},
	})
	if err != nil {
		t.Fatalf("requested capabilities should not block pairing: %v", err)
	}
	claims, err := token.Verify(resp.SessionToken, machineSecret, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("session token did not verify: %v", err)
	}
	if len(claims.Capabilities) != 0 {
		t.Fatalf("session token should not carry requested capabilities, got %#v", claims.Capabilities)
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
