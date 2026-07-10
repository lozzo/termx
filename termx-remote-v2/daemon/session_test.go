package daemon

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	termxcorev2 "github.com/lozzow/termx/termx-core-v2"
	"github.com/lozzow/termx/termx-shared/remoteauth"
	"github.com/lozzow/termx/termx-shared/transport"
)

func TestSessionAcceptorVerifiesGrantBeforeServingScopedTransport(t *testing.T) {
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	grant, err := remoteauth.Issue(privateKey, remoteauth.Claims{
		GrantID: "grant-1", DeviceID: "device-1", Scope: remoteauth.Scope{TerminalID: "term-1"},
		IssuedAt: now, ExpiresAt: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("issue grant: %v", err)
	}
	core := &recordingCore{}
	channel := newTestChannel()
	acceptor := SessionAcceptor{Core: core, DeviceFingerprint: remoteauth.Fingerprint(publicKey), Revocations: remoteauth.NewRevocations()}
	if err := acceptor.Serve(context.Background(), SessionRequest{
		Channel: channel, Grant: grant, Now: now,
	}); err != nil {
		t.Fatalf("serve authorized session: %v", err)
	}
	if core.calls != 1 || core.scope.TerminalID != "term-1" || core.scope.AllowDaemon {
		t.Fatalf("unexpected scoped core call: calls=%d scope=%#v", core.calls, core.scope)
	}
}

func TestSessionAcceptorRejectsInvalidOrRevokedGrantBeforeCore(t *testing.T) {
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	grant, _ := remoteauth.Issue(privateKey, remoteauth.Claims{
		GrantID: "grant-1", DeviceID: "device-1", Scope: remoteauth.Scope{AllowDaemon: true},
		IssuedAt: now, ExpiresAt: now.Add(time.Hour),
	})
	revocations := remoteauth.NewRevocations()
	revocations.Revoke("grant-1")
	core := &recordingCore{}
	acceptor := SessionAcceptor{Core: core, DeviceFingerprint: remoteauth.Fingerprint(publicKey), Revocations: revocations}
	if err := acceptor.Serve(context.Background(), SessionRequest{
		Channel: newTestChannel(), Grant: grant, Now: now,
	}); err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("expected revoked grant rejection, got %v", err)
	}
	if core.calls != 0 {
		t.Fatalf("core must not see unauthorized transport, calls=%d", core.calls)
	}
	if err := acceptor.Serve(context.Background(), SessionRequest{
		Channel: newTestChannel(), Grant: "legacy-session-token", Now: now,
	}); err == nil {
		t.Fatal("legacy token must not create remote session")
	}
	if core.calls != 0 {
		t.Fatalf("core must remain untouched after legacy token, calls=%d", core.calls)
	}
}

type recordingCore struct {
	calls int
	scope termxcorev2.TransportScope
}

func (core *recordingCore) ServeScopedTransport(_ context.Context, conn transport.Transport, scope termxcorev2.TransportScope) error {
	core.calls++
	core.scope = scope
	return conn.Close()
}

type testChannel struct {
	mu             sync.Mutex
	messageHandler func([]byte)
	closeHandler   func()
	closed         bool
}

func newTestChannel() *testChannel { return &testChannel{} }

func (channel *testChannel) SetMessageHandler(handler func([]byte)) { channel.messageHandler = handler }
func (channel *testChannel) SetCloseHandler(handler func())         { channel.closeHandler = handler }
func (channel *testChannel) BufferedAmount() uint64                 { return 0 }
func (channel *testChannel) SetBufferedAmountLowThreshold(uint64)   {}
func (channel *testChannel) SetBufferedAmountLowHandler(func())     {}
func (channel *testChannel) Send([]byte) error {
	channel.mu.Lock()
	defer channel.mu.Unlock()
	if channel.closed {
		return io.EOF
	}
	return nil
}
func (channel *testChannel) Close() error {
	channel.mu.Lock()
	if channel.closed {
		channel.mu.Unlock()
		return nil
	}
	channel.closed = true
	handler := channel.closeHandler
	channel.mu.Unlock()
	if handler != nil {
		handler()
	}
	return nil
}
