package runtime

import (
	"strings"
	"testing"
	"time"

	remoteconfig "github.com/lozzow/termx/termx-remote/config"
	"github.com/lozzow/termx/termx-remote/identity"
	"github.com/lozzow/termx/termx-remote/session/token"
)

func TestVerifySessionTokenAcceptsValidToken(t *testing.T) {
	manager, machineSecret := newSessionTokenTestManager(t)
	now := time.Now().UTC()
	sessionToken := issueSessionTokenForTest(t, machineSecret, token.Claims{
		SessionID:    "pair_1",
		MachineID:    "device-token-test",
		Capabilities: []string{"terminal"},
		IssuedAt:     now.Add(-time.Minute).Unix(),
		ExpiresAt:    now.Add(time.Hour).Unix(),
	})

	claims, err := manager.verifySessionToken("device-token-test", sessionToken)
	if err != nil {
		t.Fatalf("verifySessionToken returned error: %v", err)
	}
	if claims.SessionID != "pair_1" || claims.MachineID != "device-token-test" {
		t.Fatalf("claims = %+v", claims)
	}
}

func TestVerifySessionTokenRejectsExpiredToken(t *testing.T) {
	manager, machineSecret := newSessionTokenTestManager(t)
	now := time.Now().UTC()
	sessionToken := issueSessionTokenForTest(t, machineSecret, token.Claims{
		SessionID:    "pair_1",
		MachineID:    "device-token-test",
		Capabilities: []string{"terminal"},
		IssuedAt:     now.Add(-2 * time.Hour).Unix(),
		ExpiresAt:    now.Add(-time.Hour).Unix(),
	})

	_, err := manager.verifySessionToken("device-token-test", sessionToken)
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expected expired token error, got %v", err)
	}
}

func TestVerifySessionTokenRejectsWrongMachineID(t *testing.T) {
	manager, machineSecret := newSessionTokenTestManager(t)
	now := time.Now().UTC()
	sessionToken := issueSessionTokenForTest(t, machineSecret, token.Claims{
		SessionID:    "pair_1",
		MachineID:    "other-device",
		Capabilities: []string{"terminal"},
		IssuedAt:     now.Add(-time.Minute).Unix(),
		ExpiresAt:    now.Add(time.Hour).Unix(),
	})

	_, err := manager.verifySessionToken("device-token-test", sessionToken)
	if err == nil || !strings.Contains(err.Error(), "machine_id mismatch") {
		t.Fatalf("expected machine_id mismatch error, got %v", err)
	}
}

func TestVerifySessionTokenRejectsTamperedToken(t *testing.T) {
	manager, machineSecret := newSessionTokenTestManager(t)
	now := time.Now().UTC()
	sessionToken := issueSessionTokenForTest(t, machineSecret, token.Claims{
		SessionID:    "pair_1",
		MachineID:    "device-token-test",
		Capabilities: []string{"terminal"},
		IssuedAt:     now.Add(-time.Minute).Unix(),
		ExpiresAt:    now.Add(time.Hour).Unix(),
	})
	sessionToken = sessionToken[:len(sessionToken)-1] + "x"

	_, err := manager.verifySessionToken("device-token-test", sessionToken)
	if err == nil || !strings.Contains(err.Error(), "invalid session token") {
		t.Fatalf("expected tampered token error, got %v", err)
	}
}

func TestVerifySessionTokenRejectsEmptyToken(t *testing.T) {
	manager, _ := newSessionTokenTestManager(t)

	_, err := manager.verifySessionToken("device-token-test", "")
	if err == nil || !strings.Contains(err.Error(), "session_token is required") {
		t.Fatalf("expected empty token error, got %v", err)
	}
}

func newSessionTokenTestManager(t *testing.T) (*Manager, []byte) {
	t.Helper()
	dataDir := t.TempDir()
	machineSecret, err := identity.LoadOrCreateMachineSecret(dataDir)
	if err != nil {
		t.Fatalf("load machine secret: %v", err)
	}
	manager := NewManager(remoteconfig.Config{
		Enabled:    true,
		DataDir:    dataDir,
		DeviceName: "token-test-device",
	}, nil, nil)
	manager.identity = identity.DeviceIdentity{DeviceID: "device-token-test", DisplayName: "token-test-device"}
	return manager, machineSecret
}

func issueSessionTokenForTest(t *testing.T, machineSecret []byte, claims token.Claims) string {
	t.Helper()
	sessionToken, err := token.Issue(machineSecret, claims)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	return sessionToken
}
