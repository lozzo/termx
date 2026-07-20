package hub_test

import (
	"context"
	"crypto/ed25519"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/lozzow/termx/private/cloud/control-plane/servicecredential"
	"github.com/lozzow/termx/private/cloud/hub"
	"github.com/lozzow/termx/proto/cloudpb"
	"github.com/lozzow/termx/shared/cloudcompanion"
)

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (clock *fakeClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *fakeClock) Advance(duration time.Duration) {
	clock.mu.Lock()
	clock.now = clock.now.Add(duration)
	clock.mu.Unlock()
}

func TestHubCreatesEdgeSessionAndAcceptsAnswerFromOwningPresence(t *testing.T) {
	fixture := newFixture(t, 4, 4)
	presence, daemonToken := fixture.openEdgePresence(t)
	defer presence.Close()
	edgeIssuer, err := servicecredential.NewEdgeAccessIssuer("control-plane.test", fixture.signer)
	if err != nil {
		t.Fatal(err)
	}
	token, err := edgeIssuer.IssueEdgeAccess("edge-token", "hub-eu", "account-1", "client-1", 1, time.Hour, fixture.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	client, err := fixture.service.CreateEdgeSession(context.Background(), hub.CreateEdgeSessionRequest{EdgeToken: token, AccountID: "account-1", ClientDeviceID: "client-1", ClientConnectionID: "connection-1", TargetDeviceID: "daemon-1", SignalingSessionID: "signal-edge", SDP: "offer", RoutePreference: hub.RoutePreferenceDirectOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	event, err := presence.Receive(context.Background())
	if err != nil || event.Offer == nil || event.Offer.ManagedSessionID != "edge-signal-edge" {
		t.Fatalf("edge offer = (%#v, %v)", event, err)
	}
	if _, err := fixture.service.CompleteEdgeAnswer(context.Background(), hub.CompleteEdgeAnswerRequest{EdgeToken: daemonToken, AccountID: "account-1", DaemonDeviceID: "daemon-1", PresenceSessionID: "wrong-presence", SignalingSessionID: "signal-edge", SDP: "answer"}); !errors.Is(err, hub.ErrAdmission) {
		t.Fatalf("wrong presence answer error = %v", err)
	}
	if _, err := fixture.service.CompleteEdgeAnswer(context.Background(), hub.CompleteEdgeAnswerRequest{EdgeToken: daemonToken, AccountID: "account-1", DaemonDeviceID: "daemon-1", SignalingSessionID: "signal-edge", SDP: "answer"}); err != nil {
		t.Fatal(err)
	}
	answer, err := client.Receive(context.Background())
	if err != nil || answer.Answer == nil || answer.Answer.SDP != "answer" {
		t.Fatalf("edge answer = (%#v, %v)", answer, err)
	}
}

func TestHubEdgePresenceUsesFreshDeviceProofAndRejectsReplay(t *testing.T) {
	fixture := newFixture(t, 4, 4)
	token := fixture.issueDaemonEdgeToken(t)
	challenge, err := fixture.service.BeginEdgePresence(context.Background(), token, "account-1", "daemon-1")
	if err != nil {
		t.Fatal(err)
	}
	proof := fixture.signEdgePresence(t, challenge, fixture.daemonPublicKey, fixture.daemonPrivateKey, "daemon-1")
	presence, err := fixture.service.OpenEdgePresence(context.Background(), token, "account-1", proof)
	if err != nil {
		t.Fatal(err)
	}
	defer presence.Close()
	if !fixture.service.HasPresence("daemon-1") {
		t.Fatal("fresh Hub proof did not register daemon presence")
	}
	if _, err := fixture.service.OpenEdgePresence(context.Background(), token, "account-1", proof); !errors.Is(err, hub.ErrAdmission) {
		t.Fatalf("replayed Hub presence proof error = %v", err)
	}
}

func TestHubEdgePresenceRejectsWrongKeyRevocationAndStalePolicy(t *testing.T) {
	fixture := newFixture(t, 4, 4)
	token := fixture.issueDaemonEdgeToken(t)
	challenge, err := fixture.service.BeginEdgePresence(context.Background(), token, "account-1", "daemon-1")
	if err != nil {
		t.Fatal(err)
	}
	wrongPublicKey, wrongPrivateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	wrongProof := fixture.signEdgePresence(t, challenge, wrongPublicKey, wrongPrivateKey, "daemon-1")
	if _, err := fixture.service.OpenEdgePresence(context.Background(), token, "account-1", wrongProof); !errors.Is(err, hub.ErrAdmission) {
		t.Fatalf("wrong-key Hub presence proof error = %v", err)
	}

	revokedChallenge, err := fixture.service.BeginEdgePresence(context.Background(), token, "account-1", "daemon-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.edgeAuthorizer.ApplySnapshot(hub.AuthorizationSnapshot{
		Revision: 2, GeneratedAt: fixture.clock.Now(),
		Accounts: []hub.AccountAuthorization{activeHubP2PAccount("account-1", 1, fixture.clock.Now())},
		Devices:  []hub.DeviceAuthorization{{DeviceID: "client-1", AccountID: "account-1", Kind: "client", DisplayName: "Client"}, {DeviceID: "daemon-1", AccountID: "account-1", Kind: "daemon", DisplayName: "Daemon", PublicKey: fixture.daemonPublicKey, Revoked: true}},
	}); err != nil {
		t.Fatal(err)
	}
	revokedProof := fixture.signEdgePresence(t, revokedChallenge, fixture.daemonPublicKey, fixture.daemonPrivateKey, "daemon-1")
	if _, err := fixture.service.OpenEdgePresence(context.Background(), token, "account-1", revokedProof); !errors.Is(err, hub.ErrAdmission) {
		t.Fatalf("revoked Hub presence proof error = %v", err)
	}

	fresh := newFixture(t, 4, 4)
	fresh.clock.Advance(11 * time.Minute)
	if _, err := fresh.service.BeginEdgePresence(context.Background(), fresh.issueDaemonEdgeToken(t), "account-1", "daemon-1"); !errors.Is(err, hub.ErrAdmission) {
		t.Fatalf("stale-policy Hub presence begin error = %v", err)
	}
}

type fixture struct {
	now              time.Time
	clock            *fakeClock
	signer           servicecredential.Signer
	edgeAuthorizer   *hub.EdgeAuthorizer
	daemonPublicKey  ed25519.PublicKey
	daemonPrivateKey ed25519.PrivateKey
	service          *hub.Service
}

func newFixture(t *testing.T, presenceQueue, clientQueue int) fixture {
	t.Helper()
	now := time.Date(2026, 7, 11, 10, 0, 0, 0, time.UTC)
	privateKey := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	signer, err := servicecredential.NewSigner("cp-key", privateKey, now.Add(-time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	ring, err := servicecredential.NewKeyRing(signer.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	clock := &fakeClock{now: now}
	daemonPublicKey, daemonPrivateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	edgeAuthorizer, err := hub.NewEdgeAuthorizer(hub.EdgeAuthorizerConfig{HubID: "hub-eu", Issuer: "control-plane.test", KeyRing: ring, Clock: clock, MaxStaleness: 10 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if err := edgeAuthorizer.ApplySnapshot(hub.AuthorizationSnapshot{Revision: 1, GeneratedAt: now, Accounts: []hub.AccountAuthorization{activeHubP2PAccount("account-1", 1, now)}, Devices: []hub.DeviceAuthorization{{DeviceID: "client-1", AccountID: "account-1", Kind: "client", DisplayName: "Client"}, {DeviceID: "daemon-1", AccountID: "account-1", Kind: "daemon", DisplayName: "Daemon", PublicKey: daemonPublicKey}}}); err != nil {
		t.Fatal(err)
	}
	service, err := hub.New(hub.Config{HubID: "hub-eu", Clock: clock, MaxPresenceTTL: 5 * time.Minute, MaxSignalingTTL: 5 * time.Minute, PresenceChallengeTTL: time.Minute, MaxPresenceChallenges: 16, PresenceQueueSize: presenceQueue, ClientQueueSize: clientQueue, MaxSDPBytes: 1024, MaxCandidates: 8, MaxPresences: 16, MaxSessions: 32, MaxSessionsPerClient: 4, EdgeAuthorizer: edgeAuthorizer})
	if err != nil {
		t.Fatal(err)
	}
	return fixture{now: now, clock: clock, signer: signer, edgeAuthorizer: edgeAuthorizer, daemonPublicKey: daemonPublicKey, daemonPrivateKey: daemonPrivateKey, service: service}
}

func activeHubP2PAccount(accountID string, authEpoch uint64, now time.Time) hub.AccountAuthorization {
	return hub.AccountAuthorization{
		AccountID: accountID, AuthEpoch: authEpoch,
		EntitlementStatus:             cloudpb.EntitlementStatus_ENTITLEMENT_STATUS_ACTIVE,
		EntitlementEffectiveUntilUnix: now.Add(time.Hour).Unix(),
		Capability:                    &cloudpb.PlanCapability{ManagedP2PEnabled: true, ManagedP2PMaxConcurrency: 2, CloudDeviceLimit: 10},
	}
}

func (fixture fixture) issueDaemonEdgeToken(t *testing.T) []byte {
	t.Helper()
	issuer, err := servicecredential.NewEdgeAccessIssuer("control-plane.test", fixture.signer)
	if err != nil {
		t.Fatal(err)
	}
	token, err := issuer.IssueEdgeAccessForPrincipal("daemon-presence-token", "hub-eu", "account-1", "daemon-1", servicecredential.EdgePrincipalDaemon, 1, time.Hour, fixture.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func (fixture fixture) signEdgePresence(t *testing.T, challenge hub.EdgePresenceChallenge, publicKey ed25519.PublicKey, privateKey ed25519.PrivateKey, deviceID string) hub.EdgePresenceProof {
	t.Helper()
	signedAt := fixture.clock.Now().UTC()
	signingBytes, err := cloudcompanion.PresenceProofSigningBytes(&cloudpb.PresenceProofInput{
		PresenceSessionId: challenge.PresenceSessionID, ChallengeId: challenge.ChallengeID,
		Challenge: challenge.Value, DeviceId: deviceID, DevicePublicKey: publicKey, SignedAtUnixNano: signedAt.UnixNano(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return hub.EdgePresenceProof{
		PresenceSessionID: challenge.PresenceSessionID, ChallengeID: challenge.ChallengeID,
		DeviceID: deviceID, PublicKey: append([]byte(nil), publicKey...), Signature: ed25519.Sign(privateKey, signingBytes), SignedAt: signedAt,
	}
}

func (fixture fixture) openEdgePresence(t *testing.T) (*hub.Presence, []byte) {
	t.Helper()
	token := fixture.issueDaemonEdgeToken(t)
	challenge, err := fixture.service.BeginEdgePresence(context.Background(), token, "account-1", "daemon-1")
	if err != nil {
		t.Fatal(err)
	}
	presence, err := fixture.service.OpenEdgePresence(context.Background(), token, "account-1", fixture.signEdgePresence(t, challenge, fixture.daemonPublicKey, fixture.daemonPrivateKey, "daemon-1"))
	if err != nil {
		t.Fatal(err)
	}
	return presence, token
}
