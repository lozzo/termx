package daemon

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"
	"time"

	core "github.com/lozzow/termx/core"
	"github.com/lozzow/termx/shared/remoteauth"
	"github.com/lozzow/termx/shared/transport"
	"github.com/lozzow/termx/shared/transport/memory"
)

func TestSessionAcceptorAuthenticatesBeforeServingScopedTransport(t *testing.T) {
	identity, grant, now := sessionFixture(t, remoteauth.Scope{TerminalID: "term-1"})
	core := &recordingCore{}
	clientConn, serverConn := memory.NewPair()
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- (SessionAcceptor{Core: core, Identity: identity, Revocations: remoteauth.NewRevocations(), Now: fixedSessionNow(now)}).
			ServeDataChannel(context.Background(), serverConn, sessionDTLSFingerprint())
	}()
	if _, err := (remoteauth.ClientHandshake{Now: fixedSessionNow(now)}).Authenticate(context.Background(), clientConn, remoteauth.ClientHandshakeRequest{
		ExpectedDeviceID: identity.DeviceID, ExpectedDeviceFingerprint: identity.Fingerprint,
		CapabilityGrant: grant, DaemonDTLSCertificateFingerprint: sessionDTLSFingerprint(),
	}); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if err := <-serverDone; err != nil {
		t.Fatalf("ServeDataChannel: %v", err)
	}
	if core.calls != 1 || core.scope.TerminalID != "term-1" || core.scope.AllowDaemon {
		t.Fatalf("unexpected scoped core call: calls=%d scope=%#v", core.calls, core.scope)
	}
}

func TestSessionAcceptorRejectsRevokedGrantBeforeCore(t *testing.T) {
	identity, grant, now := sessionFixture(t, remoteauth.Scope{AllowDaemon: true})
	revocations := remoteauth.NewRevocations()
	revocations.Revoke("grant-1")
	core := &recordingCore{}
	clientConn, serverConn := memory.NewPair()
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- (SessionAcceptor{Core: core, Identity: identity, Revocations: revocations, Now: fixedSessionNow(now)}).
			ServeDataChannel(context.Background(), serverConn, sessionDTLSFingerprint())
	}()
	_, clientErr := (remoteauth.ClientHandshake{Now: fixedSessionNow(now)}).Authenticate(context.Background(), clientConn, remoteauth.ClientHandshakeRequest{
		ExpectedDeviceID: identity.DeviceID, ExpectedDeviceFingerprint: identity.Fingerprint,
		CapabilityGrant: grant, DaemonDTLSCertificateFingerprint: sessionDTLSFingerprint(),
	})
	if clientErr == nil || !strings.Contains(clientErr.Error(), "REVOKED") {
		t.Fatalf("expected revoked capability rejection, got %v", clientErr)
	}
	if serverErr := <-serverDone; serverErr == nil || !strings.Contains(serverErr.Error(), "revoked") {
		t.Fatalf("expected daemon revoke error, got %v", serverErr)
	}
	if core.calls != 0 {
		t.Fatalf("core must not see unauthorized transport, calls=%d", core.calls)
	}
}

type recordingCore struct {
	calls int
	scope core.TransportScope
}

func (core *recordingCore) ServeScopedTransport(_ context.Context, conn transport.Transport, scope core.TransportScope) error {
	core.calls++
	core.scope = scope
	return conn.Close()
}

func sessionFixture(t *testing.T, scope remoteauth.Scope) (remoteauth.Identity, string, time.Time) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate identity: %v", err)
	}
	identity, err := remoteauth.NewIdentity("device-1", privateKey)
	if err != nil {
		t.Fatalf("NewIdentity: %v", err)
	}
	if !bytes.Equal(identity.PublicKey, publicKey) {
		t.Fatal("identity public key mismatch")
	}
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	grant, err := remoteauth.Issue(privateKey, remoteauth.Claims{
		GrantID: "grant-1", IssuerDeviceID: identity.DeviceID, Scope: scope,
		IssuedAt: now.Add(-time.Minute), NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
		RevocationID: "grant-1", Nonce: "session-test-nonce",
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	return identity, grant, now
}

func sessionDTLSFingerprint() string {
	return "sha-256:11:11:11:11:11:11:11:11:11:11:11:11:11:11:11:11:11:11:11:11:11:11:11:11:11:11:11:11:11:11:11:11"
}

func fixedSessionNow(now time.Time) func() time.Time {
	return func() time.Time { return now }
}
