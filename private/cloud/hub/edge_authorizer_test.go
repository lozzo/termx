package hub_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lozzow/termx/private/cloud/control-plane/servicecredential"
	"github.com/lozzow/termx/private/cloud/hub"
)

type edgeClock struct{ now time.Time }

func (clock *edgeClock) Now() time.Time { return clock.now }

func TestEdgeAuthorizerUsesOnlyVersionedLocalProjection(t *testing.T) {
	now := time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := servicecredential.NewSigner("edge-key-1", privateKey, now.Add(-time.Hour), now.Add(48*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	ring, err := servicecredential.NewKeyRing(signer.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := servicecredential.NewEdgeAccessIssuer("control-plane.edge", signer)
	if err != nil {
		t.Fatal(err)
	}
	clock := &edgeClock{now: now}
	authorizer, err := hub.NewEdgeAuthorizer(hub.EdgeAuthorizerConfig{HubID: "hub-1", Issuer: "control-plane.edge", KeyRing: ring, Clock: clock, MaxStaleness: 5 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	token, err := issuer.IssueEdgeAccess("token-1", "hub-1", "account-1", "client-1", 7, time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authorizer.AuthorizeDirect(token, "account-1", "client-1", "daemon-1"); !errors.Is(err, hub.ErrPolicySnapshot) {
		t.Fatalf("missing snapshot error = %v", err)
	}
	snapshot := hub.AuthorizationSnapshot{Revision: 1, GeneratedAt: now, Accounts: []hub.AccountAuthorization{{AccountID: "account-1", AuthEpoch: 7, ManagedDirectEnabled: true}}, Devices: []hub.DeviceAuthorization{{DeviceID: "daemon-1", AccountID: "account-1"}, {DeviceID: "daemon-other", AccountID: "account-2"}}}
	if err := authorizer.ApplySnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	if _, err := authorizer.AuthorizeDirect(token, "account-1", "client-1", "daemon-1"); err != nil {
		t.Fatalf("AuthorizeDirect = %v", err)
	}
	if _, err := authorizer.AuthorizeDaemon(token, "account-1", "client-1"); !errors.Is(err, hub.ErrEdgeAuthorization) {
		t.Fatalf("client token used as daemon error = %v", err)
	}
	daemonToken, err := issuer.IssueEdgeAccessForPrincipal("daemon-token", "hub-1", "account-1", "daemon-1", servicecredential.EdgePrincipalDaemon, 7, time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authorizer.AuthorizeDirect(daemonToken, "account-1", "daemon-1", "daemon-1"); !errors.Is(err, hub.ErrEdgeAuthorization) {
		t.Fatalf("daemon token used as client error = %v", err)
	}
	if _, err := authorizer.AuthorizeDirect(token, "account-1", "client-1", "daemon-other"); !errors.Is(err, hub.ErrEdgeAuthorization) {
		t.Fatalf("cross-account target error = %v", err)
	}
	if err := authorizer.ApplySnapshot(snapshot); !errors.Is(err, hub.ErrPolicySnapshot) {
		t.Fatalf("revision rollback error = %v", err)
	}
	clock.now = now.Add(6 * time.Minute)
	if _, err := authorizer.AuthorizeDirect(token, "account-1", "client-1", "daemon-1"); !errors.Is(err, hub.ErrPolicySnapshot) {
		t.Fatalf("stale snapshot error = %v", err)
	}
	clock.now = now.Add(time.Minute)
	snapshot.Revision = 2
	snapshot.GeneratedAt = clock.now
	snapshot.Accounts[0].AuthEpoch = 8
	if err := authorizer.ApplySnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	if _, err := authorizer.AuthorizeDirect(token, "account-1", "client-1", "daemon-1"); !errors.Is(err, hub.ErrEdgeAuthorization) {
		t.Fatalf("revoked epoch error = %v", err)
	}
}

func TestEdgeAuthorizerPersistsAndReverifiesSignedSnapshot(t *testing.T) {
	now := time.Date(2026, 7, 12, 11, 0, 0, 0, time.UTC)
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := servicecredential.NewSigner("policy-key-1", privateKey, now.Add(-time.Hour), now.Add(48*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	ring, err := servicecredential.NewKeyRing(signer.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := servicecredential.NewEdgePolicyIssuer("control-plane.edge", signer)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := issuer.Issue("hub-1", 1, []servicecredential.EdgePolicyAccount{{AccountID: "account-1", AuthEpoch: 3, ManagedDirectEnabled: true}}, []servicecredential.EdgePolicyDevice{{DeviceID: "daemon-1", AccountID: "account-1"}}, time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "edge-policy.snapshot")
	store, err := hub.NewFileEdgeSnapshotStore(path)
	if err != nil {
		t.Fatal(err)
	}
	clock := &edgeClock{now: now}
	first, err := hub.NewEdgeAuthorizer(hub.EdgeAuthorizerConfig{HubID: "hub-1", Issuer: "control-plane.edge", KeyRing: ring, Clock: clock, MaxStaleness: time.Hour, SnapshotStore: store})
	if err != nil {
		t.Fatal(err)
	}
	if err := first.ApplySignedSnapshot(encoded); err != nil {
		t.Fatal(err)
	}
	restarted, err := hub.NewEdgeAuthorizer(hub.EdgeAuthorizerConfig{HubID: "hub-1", Issuer: "control-plane.edge", KeyRing: ring, Clock: clock, MaxStaleness: time.Hour, SnapshotStore: store})
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.RestoreSignedSnapshot(); err != nil {
		t.Fatal(err)
	}
	edgeIssuer, err := servicecredential.NewEdgeAccessIssuer("control-plane.edge", signer)
	if err != nil {
		t.Fatal(err)
	}
	token, err := edgeIssuer.IssueEdgeAccess("client-token", "hub-1", "account-1", "client-1", 3, time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.AuthorizeDirect(token, "account-1", "client-1", "daemon-1"); err != nil {
		t.Fatalf("restored authorization = %v", err)
	}
	if err := os.WriteFile(path, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	tampered, err := hub.NewEdgeAuthorizer(hub.EdgeAuthorizerConfig{HubID: "hub-1", Issuer: "control-plane.edge", KeyRing: ring, Clock: clock, MaxStaleness: time.Hour, SnapshotStore: store})
	if err != nil {
		t.Fatal(err)
	}
	if err := tampered.RestoreSignedSnapshot(); !errors.Is(err, hub.ErrPolicySnapshot) {
		t.Fatalf("tampered restore error = %v", err)
	}
}
