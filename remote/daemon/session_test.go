package daemon

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"sync"
	"testing"
	"time"

	core "github.com/muxvia/muxvia/core"
	"github.com/muxvia/muxvia/proto/cloudpb"
	remotewebrtc "github.com/muxvia/muxvia/remote/webrtc"
	"github.com/muxvia/muxvia/shared/remoteauth"
	"github.com/muxvia/muxvia/shared/transport"
	"github.com/muxvia/muxvia/shared/transport/memory"
)

func TestSessionAcceptorAuthenticatesBeforeServingScopedTransport(t *testing.T) {
	identity, credential, store, now := sessionFixture(t, remoteauth.Scope{TerminalID: "term-1"})
	core := &recordingCore{}
	clientConn, serverConn := memory.NewPair()
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- (SessionAcceptor{Core: core, Identity: identity, AccessStore: store, Now: fixedSessionNow(now)}).
			ServeDataChannel(context.Background(), serverConn, sessionDTLSFingerprint())
	}()
	if _, err := (remoteauth.ClientHandshake{Now: fixedSessionNow(now)}).Authenticate(context.Background(), clientConn, remoteauth.ClientHandshakeRequest{
		ExpectedDeviceID: identity.DeviceID, ExpectedDeviceFingerprint: identity.Fingerprint,
		Credential: credential, ChannelBinding: sessionDTLSBinding(t),
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

func TestManagedSessionLifecycleUsesAuthHelloAndPeerTeardown(t *testing.T) {
	identity, credential, store, now := sessionFixture(t, remoteauth.Scope{TerminalID: "term-1"})
	runtime, err := NewManagedRuntime(identity.DeviceID, bytes.NewReader(bytes.Repeat([]byte{0x44}, 18)))
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.BindPresence("hub-1", 7, "presence-1", now); err != nil {
		t.Fatal(err)
	}
	core := &recordingObservedCore{runtime: runtime, now: now}
	owner := newFakeManagedOwner()
	clientConn, serverConn := memory.NewPair()
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- (SessionAcceptor{Core: core, Identity: identity, AccessStore: store, ManagedRuntime: runtime, Now: fixedSessionNow(now)}).ServeManagedDataChannel(context.Background(), serverConn, sessionDTLSFingerprint(), remotewebrtc.ManagedSessionContext{ManagedSessionID: "managed-1", SessionIncarnation: 1, ClientDeviceID: "client-1", PresenceSessionID: "presence-1", AssignmentEpoch: 7, ObservedPath: cloudpb.ObservedPath_OBSERVED_PATH_DIRECT}, owner)
	}()
	if _, err := (remoteauth.ClientHandshake{Now: fixedSessionNow(now)}).Authenticate(context.Background(), clientConn, remoteauth.ClientHandshakeRequest{ExpectedDeviceID: identity.DeviceID, ExpectedDeviceFingerprint: identity.Fingerprint, Credential: credential, ChannelBinding: sessionDTLSBinding(t)}); err != nil {
		t.Fatal(err)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
	if !core.ready || !owner.requested {
		t.Fatalf("managed lifecycle ready=%v ownerRequested=%v", core.ready, owner.requested)
	}
	inventory, err := runtime.Registry().Inventory("final", now)
	if err != nil || inventory.GetRegistryRevision() != 3 || len(inventory.GetSessions()) != 0 {
		t.Fatalf("final managed inventory = (%#v, %v)", inventory, err)
	}
}

func TestSessionAcceptorRejectsRevokedGrantBeforeCore(t *testing.T) {
	identity, credential, store, now := sessionFixture(t, remoteauth.Scope{AllowDaemon: true})
	claims, err := remoteauth.Verify(credential.CapabilityGrant, identity.Fingerprint, now, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RevokeGrant(claims.GrantID); err != nil {
		t.Fatal(err)
	}
	core := &recordingCore{}
	clientConn, serverConn := memory.NewPair()
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- (SessionAcceptor{Core: core, Identity: identity, AccessStore: store, Now: fixedSessionNow(now)}).
			ServeDataChannel(context.Background(), serverConn, sessionDTLSFingerprint())
	}()
	_, clientErr := (remoteauth.ClientHandshake{Now: fixedSessionNow(now)}).Authenticate(context.Background(), clientConn, remoteauth.ClientHandshakeRequest{
		ExpectedDeviceID: identity.DeviceID, ExpectedDeviceFingerprint: identity.Fingerprint,
		Credential: credential, ChannelBinding: sessionDTLSBinding(t),
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

func TestSessionAcceptorRejectsSignedGrantAbsentFromAccessStore(t *testing.T) {
	identity, credential, store, now := sessionFixture(t, remoteauth.Scope{AllowDaemon: true})
	unknown, err := remoteauth.Issue(identity.PrivateKey, remoteauth.Claims{
		GrantID: "grant-not-registered", IssuerDeviceID: identity.DeviceID,
		SubjectKeyFingerprint: credential.Identity.Fingerprint, Scope: remoteauth.Scope{AllowDaemon: true},
		IssuedAt: now.Add(-time.Minute), NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
		RevocationID: "grant-not-registered", Nonce: "unregistered-grant-nonce",
	})
	if err != nil {
		t.Fatal(err)
	}
	credential.CapabilityGrant = unknown
	recorded := &recordingCore{}
	clientConn, serverConn := memory.NewPair()
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- (SessionAcceptor{Core: recorded, Identity: identity, AccessStore: store, Now: fixedSessionNow(now)}).
			ServeDataChannel(context.Background(), serverConn, sessionDTLSFingerprint())
	}()
	_, clientErr := (remoteauth.ClientHandshake{Now: fixedSessionNow(now)}).Authenticate(context.Background(), clientConn, remoteauth.ClientHandshakeRequest{
		ExpectedDeviceID: identity.DeviceID, ExpectedDeviceFingerprint: identity.Fingerprint,
		Credential: credential, ChannelBinding: sessionDTLSBinding(t),
	})
	if clientErr == nil || !strings.Contains(clientErr.Error(), "REVOKED") {
		t.Fatalf("unregistered signed grant client error = %v", clientErr)
	}
	if serverErr := <-serverDone; serverErr == nil || !strings.Contains(serverErr.Error(), "revoked") {
		t.Fatalf("unregistered signed grant daemon error = %v", serverErr)
	}
	if recorded.calls != 0 {
		t.Fatalf("unregistered signed grant reached core %d times", recorded.calls)
	}
}

func TestSessionAcceptorMapsExplicitFilePermissions(t *testing.T) {
	identity, credential, store, now := sessionFixture(t, remoteauth.FullDaemonScope())
	recorded := &recordingCore{}
	clientConn, serverConn := memory.NewPair()
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- (SessionAcceptor{Core: recorded, Identity: identity, AccessStore: store, Now: fixedSessionNow(now)}).ServeDataChannel(context.Background(), serverConn, sessionDTLSFingerprint())
	}()
	if _, err := (remoteauth.ClientHandshake{Now: fixedSessionNow(now)}).Authenticate(context.Background(), clientConn, remoteauth.ClientHandshakeRequest{ExpectedDeviceID: identity.DeviceID, ExpectedDeviceFingerprint: identity.Fingerprint, Credential: credential, ChannelBinding: sessionDTLSBinding(t)}); err != nil {
		t.Fatal(err)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
	if !recorded.scope.FileReadMetadata || !recorded.scope.FileReadContent || !recorded.scope.FileWriteContent || !recorded.scope.FileMutate {
		t.Fatalf("file permissions lost: %#v", recorded.scope)
	}
}

func TestSessionAcceptorPairingModeNeverStartsCoreProtocol(t *testing.T) {
	identity, _, store, now := sessionFixture(t, remoteauth.FullDaemonScope())
	bundle, _, err := store.IssuePairingBundle(remoteauth.PairingIssueOptions{Scope: remoteauth.FullDaemonScope(), TicketTTL: time.Hour, GrantLifetime: 24 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	bundlePayload, err := remoteauth.EncodePairingBundle(bundle)
	if err != nil {
		t.Fatal(err)
	}
	clientIdentity, err := remoteauth.GenerateClientAccessIdentity("endpoint-1", rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	recorded := &recordingCore{}
	clientConn, serverConn := memory.NewPair()
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- (SessionAcceptor{Core: recorded, Identity: identity, AccessStore: store, Now: fixedSessionNow(now)}).
			ServeDataChannel(context.Background(), serverConn, sessionDTLSFingerprint())
	}()
	result, err := (remoteauth.ClientPairingHandshake{Now: fixedSessionNow(now)}).Redeem(context.Background(), clientConn, remoteauth.ClientPairingRequest{
		ExpectedDeviceID: identity.DeviceID, ExpectedDeviceFingerprint: identity.Fingerprint,
		PairingBundle: bundlePayload, Identity: clientIdentity, ChannelBinding: sessionDTLSBinding(t),
	})
	if err != nil || result.Grant == "" {
		t.Fatalf("pairing result=%#v err=%v", result, err)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
	if recorded.calls != 0 {
		t.Fatalf("pairing transport reached core %d times", recorded.calls)
	}
}

func TestPairingAcceptorRejectsCapabilityMode(t *testing.T) {
	identity, credential, store, now := sessionFixture(t, remoteauth.FullDaemonScope())
	binding, err := remoteauth.LocalUnixChannelBinding("/tmp/termx-session-pairing.sock")
	if err != nil {
		t.Fatal(err)
	}
	clientConn, serverConn := memory.NewPair()
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- (PairingAcceptor{Identity: identity, AccessStore: store, Now: fixedSessionNow(now)}).
			ServeBoundTransport(context.Background(), serverConn, binding)
	}()
	_, clientErr := (remoteauth.ClientHandshake{Now: fixedSessionNow(now)}).Authenticate(context.Background(), clientConn, remoteauth.ClientHandshakeRequest{
		ExpectedDeviceID: identity.DeviceID, ExpectedDeviceFingerprint: identity.Fingerprint,
		Credential: credential, ChannelBinding: binding,
	})
	if clientErr == nil || !strings.Contains(clientErr.Error(), "PROTOCOL") {
		t.Fatalf("expected pairing-only capability rejection, got %v", clientErr)
	}
	if serverErr := <-serverDone; serverErr == nil || !strings.Contains(serverErr.Error(), "pairing-only") {
		t.Fatalf("expected pairing-only daemon error, got %v", serverErr)
	}
}

func TestSessionAcceptorRejectsLocalUnixBindingBeforeCore(t *testing.T) {
	identity, _, store, now := sessionFixture(t, remoteauth.FullDaemonScope())
	binding, err := remoteauth.LocalUnixChannelBinding("/tmp/termx-generic-session.sock")
	if err != nil {
		t.Fatal(err)
	}
	clientConn, serverConn := memory.NewPair()
	defer clientConn.Close()
	recorded := &recordingCore{}
	err = (SessionAcceptor{Core: recorded, Identity: identity, AccessStore: store, Now: fixedSessionNow(now)}).
		ServeBoundTransport(context.Background(), serverConn, binding)
	if err == nil || !strings.Contains(err.Error(), "rejects local Unix") {
		t.Fatalf("generic local Unix ingress error = %v", err)
	}
	if recorded.calls != 0 {
		t.Fatalf("local Unix session reached core %d times", recorded.calls)
	}
}

type recordingCore struct {
	calls int
	scope core.TransportScope
}

type recordingObservedCore struct {
	runtime *ManagedRuntime
	now     time.Time
	ready   bool
}

func (server *recordingObservedCore) ServeScopedTransport(context.Context, transport.Transport, core.TransportScope) error {
	return nil
}

func (server *recordingObservedCore) ServeScopedTransportObserved(_ context.Context, _ transport.Transport, _ core.TransportScope, observer core.TransportLifecycleObserver) error {
	observer.HelloAccepted()
	inventory, err := server.runtime.Registry().Inventory("ready", server.now)
	server.ready = err == nil && inventory.GetRegistryRevision() == 2 && len(inventory.GetSessions()) == 1 && inventory.GetSessions()[0].GetState() == cloudpb.ManagedPeerSessionState_MANAGED_PEER_SESSION_STATE_READY
	return nil
}

type fakeManagedOwner struct {
	once      sync.Once
	done      chan struct{}
	requested bool
}

func newFakeManagedOwner() *fakeManagedOwner { return &fakeManagedOwner{done: make(chan struct{})} }

func (owner *fakeManagedOwner) RequestClose() {
	owner.once.Do(func() {
		owner.requested = true
		close(owner.done)
	})
}

func (owner *fakeManagedOwner) Done() <-chan struct{} { return owner.done }

func (core *recordingCore) ServeScopedTransport(_ context.Context, conn transport.Transport, scope core.TransportScope) error {
	core.calls++
	core.scope = scope
	return conn.Close()
}

func sessionFixture(t *testing.T, scope remoteauth.Scope) (remoteauth.Identity, remoteauth.ClientAccessCredential, *remoteauth.AccessStore, time.Time) {
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
	clientIdentity, err := remoteauth.GenerateClientAccessIdentity("endpoint-1", rand.Reader)
	if err != nil {
		t.Fatalf("GenerateClientAccessIdentity: %v", err)
	}
	store, err := remoteauth.LoadAccessStore(t.TempDir(), identity, remoteauth.AccessStoreOptions{Now: fixedSessionNow(now)})
	if err != nil {
		t.Fatalf("LoadAccessStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	bundle, _, err := store.IssuePairingBundle(remoteauth.PairingIssueOptions{Scope: scope, TicketTTL: time.Hour, GrantLifetime: time.Hour, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := remoteauth.EncodePairingBundle(bundle)
	if err != nil {
		t.Fatal(err)
	}
	result, err := store.RedeemPairingBundle(payload, clientIdentity.PublicKey, "session-fixture", now)
	if err != nil {
		t.Fatal(err)
	}
	return identity, remoteauth.ClientAccessCredential{
		Version: 1, EndpointID: clientIdentity.EndpointID, Identity: clientIdentity,
		CapabilityGrant: result.Grant, UpdatedAt: now,
	}, store, now
}

func sessionDTLSBinding(t *testing.T) remoteauth.ChannelBinding {
	t.Helper()
	binding, err := remoteauth.DTLSChannelBinding(sessionDTLSFingerprint())
	if err != nil {
		t.Fatal(err)
	}
	return binding
}

func sessionDTLSFingerprint() string {
	return "sha-256:11:11:11:11:11:11:11:11:11:11:11:11:11:11:11:11:11:11:11:11:11:11:11:11:11:11:11:11:11:11:11:11"
}

func fixedSessionNow(now time.Time) func() time.Time {
	return func() time.Time { return now }
}
