package hub_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/muxvia/muxvia/private/cloud/control-plane/servicecredential"
	"github.com/muxvia/muxvia/private/cloud/hub"
	"github.com/muxvia/muxvia/proto/cloudpb"
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
	snapshot.Accounts[0].Capability.ManagedP2PMaxConcurrency = 1
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
	if _, err := authorizer.ReserveManagedP2P(token, "account-1", "client-1", "daemon-1", "pending-1", "managed-1", 1, now.Add(30*time.Second)); err != nil {
		t.Fatal(err)
	}
	valid := &cloudpb.ManagedPeerSessionProjection{Target: &cloudpb.ManagedPeerSessionTarget{DaemonDeviceId: "daemon-1", ManagedSessionId: "managed-1", SessionIncarnation: 1, AssignmentEpoch: 1, DaemonRuntimeGeneration: "runtime-1"}, ClientDeviceId: "client-1", ObservedDataPath: cloudpb.ObservedPath_OBSERVED_PATH_DIRECT, State: cloudpb.ManagedPeerSessionState_MANAGED_PEER_SESSION_STATE_READY}
	invalid := &cloudpb.ManagedPeerSessionProjection{Target: &cloudpb.ManagedPeerSessionTarget{DaemonDeviceId: "daemon-1", ManagedSessionId: "managed-invalid", SessionIncarnation: 2, AssignmentEpoch: 1, DaemonRuntimeGeneration: "runtime-1"}, ClientDeviceId: "unknown-client", ObservedDataPath: cloudpb.ObservedPath_OBSERVED_PATH_DIRECT, State: cloudpb.ManagedPeerSessionState_MANAGED_PEER_SESSION_STATE_READY}
	if err := authorizer.ReconcileManagedP2P("daemon-1", []*cloudpb.ManagedPeerSessionProjection{valid, invalid}); !errors.Is(err, hub.ErrEdgeAuthorization) {
		t.Fatalf("invalid full runtime replacement = %v", err)
	}
	clock.now = now.Add(time.Minute)
	if _, err := authorizer.ReserveManagedP2P(token, "account-1", "client-1", "daemon-1", "pending-2", "managed-2", 1, clock.now.Add(time.Minute)); err != nil {
		t.Fatalf("rejected runtime report mutated reservations: %v", err)
	}
	authorizer.ReleaseManagedP2P("pending-2")
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

func activeP2PAccount(accountID string, authEpoch uint64, now time.Time) hub.AccountAuthorization {
	return hub.AccountAuthorization{
		AccountID: accountID, AuthEpoch: authEpoch,
		EntitlementStatus:             cloudpb.EntitlementStatus_ENTITLEMENT_STATUS_ACTIVE,
		EntitlementEffectiveUntilUnix: now.Add(time.Hour).Unix(),
		Capability:                    &cloudpb.PlanCapability{ManagedP2PEnabled: true, ManagedP2PMaxConcurrency: 2, CloudDeviceLimit: 10},
	}
}
