package hub_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/lozzow/termx/private/cloud/control-plane/servicecredential"
	"github.com/lozzow/termx/private/cloud/hub"
	"github.com/lozzow/termx/proto/cloudpb"
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
	snapshot := hub.AuthorizationSnapshot{Revision: 1, GeneratedAt: now, Accounts: []hub.AccountAuthorization{activeP2PAccount("account-1", 7, now)}, Devices: []hub.DeviceAuthorization{{DeviceID: "client-1", AccountID: "account-1", Kind: "client", DisplayName: "Client"}, {DeviceID: "daemon-1", AccountID: "account-1", Kind: "daemon", DisplayName: "Daemon"}, {DeviceID: "daemon-other", AccountID: "account-2", Kind: "daemon", DisplayName: "Other daemon"}}}
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
	if _, err := authorizer.AuthorizeDirect(token, "account-1", "client-1", "daemon-other"); !errors.Is(err, hub.ErrTargetUnavailable) {
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

func TestEdgeAuthorizerRestartStartsWithoutPolicyProjection(t *testing.T) {
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
	encoded, err := issuer.Issue("hub-1", 1, []servicecredential.EdgePolicyAccount{{AccountID: "account-1", AuthEpoch: 3, EntitlementStatus: cloudpb.EntitlementStatus_ENTITLEMENT_STATUS_ACTIVE, EntitlementEffectiveUntilUnix: now.Add(time.Hour).Unix(), Capability: &cloudpb.PlanCapability{ManagedP2PEnabled: true, ManagedP2PMaxConcurrency: 2, CloudDeviceLimit: 10}}}, []servicecredential.EdgePolicyDevice{{DeviceID: "client-1", AccountID: "account-1", Kind: "client", DisplayName: "Client"}, {DeviceID: "daemon-1", AccountID: "account-1", Kind: "daemon", DisplayName: "Daemon"}}, time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	clock := &edgeClock{now: now}
	first, err := hub.NewEdgeAuthorizer(hub.EdgeAuthorizerConfig{HubID: "hub-1", Issuer: "control-plane.edge", KeyRing: ring, Clock: clock, MaxStaleness: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if err := first.ApplySignedSnapshot(encoded); err != nil {
		t.Fatal(err)
	}
	restarted, err := hub.NewEdgeAuthorizer(hub.EdgeAuthorizerConfig{HubID: "hub-1", Issuer: "control-plane.edge", KeyRing: ring, Clock: clock, MaxStaleness: time.Hour})
	if err != nil {
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
	if _, err := restarted.AuthorizeDirect(token, "account-1", "client-1", "daemon-1"); !errors.Is(err, hub.ErrPolicySnapshot) {
		t.Fatalf("restarted Hub restored policy from disk: %v", err)
	}
}

func activeP2PAccount(accountID string, authEpoch uint64, now time.Time) hub.AccountAuthorization {
	return hub.AccountAuthorization{
		AccountID: accountID, AuthEpoch: authEpoch,
		EntitlementStatus:             cloudpb.EntitlementStatus_ENTITLEMENT_STATUS_ACTIVE,
		EntitlementEffectiveUntilUnix: now.Add(time.Hour).Unix(),
		Capability:                    &cloudpb.PlanCapability{ManagedP2PEnabled: true, ManagedP2PMaxConcurrency: 2, CloudDeviceLimit: 10},
	}
}
