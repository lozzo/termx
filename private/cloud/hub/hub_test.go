package hub_test

import (
	"context"
	"crypto/ed25519"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/muxvia/muxvia/private/cloud/control-plane/servicecredential"
	"github.com/muxvia/muxvia/private/cloud/hub"
	"github.com/muxvia/muxvia/proto/cloudpb"
	"github.com/muxvia/muxvia/shared/cloudcompanion"
	"google.golang.org/protobuf/proto"
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

func TestRelayIntentSharesLeaseAndRotatesNearExpiry(t *testing.T) {
	fixture := newFixture(t, 4, 4)
	if err := fixture.service.CreateRelayIntent("managed-1", "account-1", "client-1", "daemon-1"); err != nil {
		t.Fatal(err)
	}
	client, err := fixture.service.RelayIntent("managed-1", "client-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.BindRelayIntentLease("managed-1", client.LeaseID, fixture.clock.Now().Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	daemon, err := fixture.service.RelayIntent("managed-1", "daemon-1")
	if err != nil || daemon.LeaseID != client.LeaseID {
		t.Fatalf("shared Relay intent = (%v, %v)", daemon, err)
	}
	fixture.clock.Advance(4*time.Minute + 31*time.Second)
	refreshed, err := fixture.service.RelayIntent("managed-1", "client-1")
	if err != nil || refreshed.LeaseID == client.LeaseID {
		t.Fatalf("rotated Relay intent = (%v, %v)", refreshed, err)
	}
	if other, err := fixture.service.RelayIntent("managed-1", "other"); !errors.Is(err, hub.ErrAdmission) || other.LeaseID != "" {
		t.Fatalf("unbound Relay intent = (%v, %v)", other, err)
	}
}

func TestHubManagedP2PReservationUsesPolicyLimitAndExactLifecycleRelease(t *testing.T) {
	fixture := newFixture(t, 8, 8)
	presence, daemonToken := fixture.openEdgePresence(t)
	defer presence.Close()
	edgeIssuer, err := servicecredential.NewEdgeAccessIssuer("control-plane.test", fixture.signer)
	if err != nil {
		t.Fatal(err)
	}
	clientToken, err := edgeIssuer.IssueEdgeAccess("edge-token", "hub-eu", "account-1", "client-1", 1, time.Hour, fixture.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	request := func(id string) hub.CreateEdgeSessionRequest {
		return hub.CreateEdgeSessionRequest{EdgeToken: clientToken, AccountID: "account-1", ClientDeviceID: "client-1", ClientConnectionID: "connection-1", TargetDeviceID: "daemon-1", SignalingSessionID: id, SDP: "offer", RoutePreference: hub.RoutePreferenceDirectOnly}
	}
	invalid := request("signal-invalid")
	invalid.SDP = ""
	if _, err := fixture.service.CreateEdgeSession(context.Background(), invalid); !errors.Is(err, hub.ErrInvalidSignal) {
		t.Fatalf("invalid offer = %v", err)
	}
	fixture.assignmentSource.Set(0)
	if _, err := fixture.service.CreateEdgeSession(context.Background(), request("signal-unassigned")); !errors.Is(err, hub.ErrAdmission) {
		t.Fatalf("unassigned offer = %v", err)
	}
	fixture.assignmentSource.Set(1)
	first, err := fixture.service.CreateEdgeSession(context.Background(), request("signal-1"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := fixture.service.CreateEdgeSession(context.Background(), request("signal-2"))
	if err != nil {
		t.Fatal(err)
	}
	relayRequest := request("signal-relay")
	relayRequest.RelayOnly = true
	relayRequest.RelayCorrelationID = "relay-managed-1"
	relayRequest.RoutePreference = hub.RoutePreferenceStandardRelay
	relaySession, err := fixture.service.CreateEdgeSession(context.Background(), relayRequest)
	if err != nil {
		t.Fatalf("Relay-only signaling consumed P2P capacity: %v", err)
	}
	_ = relaySession.Close()
	if _, err := fixture.service.CreateEdgeSession(context.Background(), request("signal-3")); !errors.Is(err, hub.ErrP2PConcurrency) {
		t.Fatalf("third P2P reservation = %v", err)
	}
	relayRequest.SignalingSessionID = "signal-relay-invalid"
	relayRequest.EdgeToken = []byte("invalid")
	if _, err := fixture.service.CreateEdgeSession(context.Background(), relayRequest); !errors.Is(err, hub.ErrEdgeAuthentication) {
		t.Fatalf("Relay-only signaling skipped client authorization: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	third, err := fixture.service.CreateEdgeSession(context.Background(), request("signal-3"))
	if err != nil {
		t.Fatalf("reservation after close = %v", err)
	}
	if err := fixture.service.CompleteEdgeFailure(context.Background(), hub.CompleteEdgeFailureRequest{EdgeToken: daemonToken, AccountID: "account-1", DaemonDeviceID: "daemon-1", SignalingSessionID: "signal-2", Code: 1}); err != nil {
		t.Fatal(err)
	}
	fourth, err := fixture.service.CreateEdgeSession(context.Background(), request("signal-4"))
	if err != nil {
		t.Fatalf("reservation after daemon failure = %v", err)
	}
	_ = second.Close()
	_ = third.Close()
	_ = fourth.Close()
}

func TestHubManagedP2PReservationTransfersFromSignalingToRuntimeInventory(t *testing.T) {
	fixture := newFixtureWithTTLs(t, 8, 8, 10*time.Minute, time.Minute)
	presence, daemonToken := fixture.openEdgePresence(t)
	defer presence.Close()
	edgeIssuer, _ := servicecredential.NewEdgeAccessIssuer("control-plane.test", fixture.signer)
	clientToken, _ := edgeIssuer.IssueEdgeAccess("edge-token", "hub-eu", "account-1", "client-1", 1, time.Hour, fixture.clock.Now())
	request := func(id string) hub.CreateEdgeSessionRequest {
		return hub.CreateEdgeSessionRequest{EdgeToken: clientToken, AccountID: "account-1", ClientDeviceID: "client-1", ClientConnectionID: "connection-1", TargetDeviceID: "daemon-1", SignalingSessionID: id, SDP: "offer", RoutePreference: hub.RoutePreferenceDirectOnly}
	}
	first, err := fixture.service.CreateEdgeSession(context.Background(), request("runtime-1"))
	if err != nil {
		t.Fatal(err)
	}
	offerEvent, err := presence.Receive(context.Background())
	if err != nil || offerEvent.Offer == nil {
		t.Fatalf("runtime offer = (%#v, %v)", offerEvent, err)
	}
	offer := offerEvent.Offer
	if _, err := fixture.service.CompleteEdgeAnswer(context.Background(), hub.CompleteEdgeAnswerRequest{EdgeToken: daemonToken, AccountID: "account-1", DaemonDeviceID: "daemon-1", SignalingSessionID: offer.SignalingSessionID, SDP: "answer"}); err != nil {
		t.Fatal(err)
	}
	if event, err := first.Receive(context.Background()); err != nil || event.Answer == nil {
		t.Fatalf("runtime answer = (%#v, %v)", event, err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	active := &cloudpb.ManagedPeerSessionProjection{Target: &cloudpb.ManagedPeerSessionTarget{DaemonDeviceId: "daemon-1", ManagedSessionId: offer.ManagedSessionID, SessionIncarnation: offer.SessionIncarnation, AssignmentEpoch: offer.AssignmentEpoch, ControlPresenceSessionId: offer.PresenceSessionID, DaemonRuntimeGeneration: "runtime-a"}, ClientDeviceId: "client-1", EstablishedPresenceSessionId: offer.PresenceSessionID, ControlOwnerHubId: "hub-eu", ObservedDataPath: cloudpb.ObservedPath_OBSERVED_PATH_DIRECT, State: cloudpb.ManagedPeerSessionState_MANAGED_PEER_SESSION_STATE_READY, Freshness: cloudpb.Freshness_FRESHNESS_FRESH}
	if _, err := fixture.service.ReportDaemonRuntime("daemon-1", runtimeRequest("runtime-a", 1, offer.PresenceSessionID, []*cloudpb.ManagedPeerSessionProjection{active}, fixture.clock.Now().UnixMilli())); err != nil {
		t.Fatal(err)
	}
	second, err := fixture.service.CreateEdgeSession(context.Background(), request("runtime-2"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.CreateEdgeSession(context.Background(), request("runtime-3")); !errors.Is(err, hub.ErrP2PConcurrency) {
		t.Fatalf("active runtime was not counted = %v", err)
	}
	_ = second.Close()
	fixture.clock.Advance(2 * time.Minute)
	fixture.service.Cleanup()
	second, err = fixture.service.CreateEdgeSession(context.Background(), request("runtime-2b"))
	if err != nil {
		t.Fatalf("new signaling beside active runtime = %v", err)
	}
	if _, err := fixture.service.CreateEdgeSession(context.Background(), request("runtime-3b")); !errors.Is(err, hub.ErrP2PConcurrency) {
		t.Fatalf("active runtime was released by signaling TTL = %v", err)
	}
	_ = second.Close()
	if _, err := fixture.service.ReportDaemonRuntime("daemon-1", runtimeRequest("runtime-a", 2, offer.PresenceSessionID, nil, fixture.clock.Now().UnixMilli())); err != nil {
		t.Fatal(err)
	}
	one, err := fixture.service.CreateEdgeSession(context.Background(), request("after-runtime-close-1"))
	if err != nil {
		t.Fatal(err)
	}
	two, err := fixture.service.CreateEdgeSession(context.Background(), request("after-runtime-close-2"))
	if err != nil {
		t.Fatalf("empty runtime inventory did not release reservation = %v", err)
	}
	_ = one.Close()
	_ = two.Close()
	migrated := proto.Clone(active).(*cloudpb.ManagedPeerSessionProjection)
	migrated.Target.ManagedSessionId = "managed-from-previous-hub"
	migrated.Target.SessionIncarnation++
	if _, err := fixture.service.ReportDaemonRuntime("daemon-1", runtimeRequest("runtime-a", 3, offer.PresenceSessionID, []*cloudpb.ManagedPeerSessionProjection{migrated}, fixture.clock.Now().UnixMilli())); err != nil {
		t.Fatal(err)
	}
	one, err = fixture.service.CreateEdgeSession(context.Background(), request("beside-reconstructed-runtime"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.CreateEdgeSession(context.Background(), request("over-reconstructed-runtime")); !errors.Is(err, hub.ErrP2PConcurrency) {
		t.Fatalf("runtime inventory without local signaling was not reconstructed = %v", err)
	}
	_ = one.Close()
}

func TestHubManagedP2PReservationRejectsRevokedEpochAndSuspendedPolicy(t *testing.T) {
	fixture := newFixture(t, 8, 8)
	presence, _ := fixture.openEdgePresence(t)
	defer presence.Close()
	edgeIssuer, _ := servicecredential.NewEdgeAccessIssuer("control-plane.test", fixture.signer)
	oldToken, _ := edgeIssuer.IssueEdgeAccess("old-token", "hub-eu", "account-1", "client-1", 1, time.Hour, fixture.clock.Now())
	request := hub.CreateEdgeSessionRequest{EdgeToken: oldToken, AccountID: "account-1", ClientDeviceID: "client-1", ClientConnectionID: "connection-1", TargetDeviceID: "daemon-1", SignalingSessionID: "old-epoch", SDP: "offer", RoutePreference: hub.RoutePreferenceDirectOnly}
	if err := fixture.edgeAuthorizer.ApplySnapshot(hub.AuthorizationSnapshot{Revision: 2, GeneratedAt: fixture.clock.Now(), Accounts: []hub.AccountAuthorization{activeHubP2PAccount("account-1", 2, fixture.clock.Now())}, Devices: []hub.DeviceAuthorization{{DeviceID: "client-1", AccountID: "account-1", Kind: "client", DisplayName: "Client"}, {DeviceID: "daemon-1", AccountID: "account-1", Kind: "daemon", DisplayName: "Daemon", PublicKey: fixture.daemonPublicKey}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.CreateEdgeSession(context.Background(), request); !errors.Is(err, hub.ErrPrincipalRevoked) {
		t.Fatalf("old auth epoch = %v", err)
	}
	newToken, _ := edgeIssuer.IssueEdgeAccess("new-token", "hub-eu", "account-1", "client-1", 2, time.Hour, fixture.clock.Now())
	suspended := activeHubP2PAccount("account-1", 2, fixture.clock.Now())
	suspended.EntitlementStatus = cloudpb.EntitlementStatus_ENTITLEMENT_STATUS_SUSPENDED
	if err := fixture.edgeAuthorizer.ApplySnapshot(hub.AuthorizationSnapshot{Revision: 3, GeneratedAt: fixture.clock.Now(), Accounts: []hub.AccountAuthorization{suspended}, Devices: []hub.DeviceAuthorization{{DeviceID: "client-1", AccountID: "account-1", Kind: "client", DisplayName: "Client"}, {DeviceID: "daemon-1", AccountID: "account-1", Kind: "daemon", DisplayName: "Daemon", PublicKey: fixture.daemonPublicKey}}}); err != nil {
		t.Fatal(err)
	}
	request.EdgeToken = newToken
	request.SignalingSessionID = "suspended"
	if _, err := fixture.service.CreateEdgeSession(context.Background(), request); !errors.Is(err, hub.ErrP2PNotEntitled) {
		t.Fatalf("suspended P2P = %v", err)
	}
	active := activeHubP2PAccount("account-1", 2, fixture.clock.Now())
	if err := fixture.edgeAuthorizer.ApplySnapshot(hub.AuthorizationSnapshot{Revision: 4, GeneratedAt: fixture.clock.Now(), Accounts: []hub.AccountAuthorization{active}, Devices: []hub.DeviceAuthorization{{DeviceID: "client-1", AccountID: "account-1", Kind: "client", DisplayName: "Client"}, {DeviceID: "daemon-1", AccountID: "account-1", Kind: "daemon", DisplayName: "Daemon", PublicKey: fixture.daemonPublicKey, Revoked: true}}}); err != nil {
		t.Fatal(err)
	}
	request.SignalingSessionID = "revoked-target"
	if _, err := fixture.service.CreateEdgeSession(context.Background(), request); !errors.Is(err, hub.ErrTargetUnavailable) {
		t.Fatalf("revoked target = %v", err)
	}
	if err := fixture.edgeAuthorizer.ApplySnapshot(hub.AuthorizationSnapshot{Revision: 5, GeneratedAt: fixture.clock.Now(), Accounts: []hub.AccountAuthorization{active}, Devices: []hub.DeviceAuthorization{{DeviceID: "client-1", AccountID: "account-1", Kind: "client", DisplayName: "Client", Revoked: true}, {DeviceID: "daemon-1", AccountID: "account-1", Kind: "daemon", DisplayName: "Daemon", PublicKey: fixture.daemonPublicKey}}}); err != nil {
		t.Fatal(err)
	}
	request.SignalingSessionID = "revoked-client"
	if _, err := fixture.service.CreateEdgeSession(context.Background(), request); !errors.Is(err, hub.ErrPrincipalRevoked) {
		t.Fatalf("revoked client = %v", err)
	}
	active.Revoked = true
	if err := fixture.edgeAuthorizer.ApplySnapshot(hub.AuthorizationSnapshot{Revision: 6, GeneratedAt: fixture.clock.Now(), Accounts: []hub.AccountAuthorization{active}, Devices: []hub.DeviceAuthorization{{DeviceID: "client-1", AccountID: "account-1", Kind: "client", DisplayName: "Client"}, {DeviceID: "daemon-1", AccountID: "account-1", Kind: "daemon", DisplayName: "Daemon", PublicKey: fixture.daemonPublicKey}}}); err != nil {
		t.Fatal(err)
	}
	request.SignalingSessionID = "revoked-account"
	if _, err := fixture.service.CreateEdgeSession(context.Background(), request); !errors.Is(err, hub.ErrPrincipalRevoked) {
		t.Fatalf("revoked account = %v", err)
	}
}

func TestHubManagedP2PReservationReleasesOnExpiryAndAssignmentFence(t *testing.T) {
	fixture := newFixtureWithTTLs(t, 8, 8, 5*time.Minute, time.Minute)
	presence, _ := fixture.openEdgePresence(t)
	edgeIssuer, _ := servicecredential.NewEdgeAccessIssuer("control-plane.test", fixture.signer)
	token, _ := edgeIssuer.IssueEdgeAccess("edge-token", "hub-eu", "account-1", "client-1", 1, time.Hour, fixture.clock.Now())
	request := func(id string) hub.CreateEdgeSessionRequest {
		return hub.CreateEdgeSessionRequest{EdgeToken: token, AccountID: "account-1", ClientDeviceID: "client-1", ClientConnectionID: "connection-1", TargetDeviceID: "daemon-1", SignalingSessionID: id, SDP: "offer", RoutePreference: hub.RoutePreferenceDirectOnly}
	}
	if _, err := fixture.service.CreateEdgeSession(context.Background(), request("expiring-1")); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.CreateEdgeSession(context.Background(), request("expiring-2")); err != nil {
		t.Fatal(err)
	}
	fixture.clock.Advance(2 * time.Minute)
	fixture.service.Cleanup()
	if _, err := fixture.service.CreateEdgeSession(context.Background(), request("after-expiry-1")); err != nil {
		t.Fatalf("reservation after expiry = %v", err)
	}
	if _, err := fixture.service.CreateEdgeSession(context.Background(), request("after-expiry-2")); err != nil {
		t.Fatalf("second reservation after expiry = %v", err)
	}
	fixture.assignmentSource.Set(2)
	fixture.service.FenceAssignment("daemon-1", 1)
	_ = presence.Close()
	nextPresence, _ := fixture.openEdgePresence(t)
	defer nextPresence.Close()
	if _, err := fixture.service.CreateEdgeSession(context.Background(), request("after-fence-1")); err != nil {
		t.Fatalf("reservation after assignment fence = %v", err)
	}
	if _, err := fixture.service.CreateEdgeSession(context.Background(), request("after-fence-2")); err != nil {
		t.Fatalf("second reservation after assignment fence = %v", err)
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

func TestHubPresenceRequiresAssignmentAndFencesExactEpoch(t *testing.T) {
	fixture := newFixture(t, 4, 4)
	token := fixture.issueDaemonEdgeToken(t)
	fixture.assignmentSource.Set(0)
	if _, err := fixture.service.BeginEdgePresence(context.Background(), token, "account-1", "daemon-1"); !errors.Is(err, hub.ErrPolicySnapshot) {
		t.Fatalf("unassigned presence error = %v", err)
	}

	fixture.assignmentSource.Set(1)
	first, _ := fixture.openEdgePresence(t)
	fixture.assignmentSource.Set(2)
	fixture.service.FenceAssignment("daemon-1", 1)
	if fixture.service.HasPresence("daemon-1") {
		t.Fatal("old assignment presence survived exact fence")
	}
	_ = first.Close()

	second, _ := fixture.openEdgePresence(t)
	defer second.Close()
	fixture.service.FenceAssignment("daemon-1", 1)
	if !fixture.service.HasPresence("daemon-1") {
		t.Fatal("stale assignment fence closed new epoch presence")
	}
	fixture.service.FenceAssignment("daemon-1", 2)
	if fixture.service.HasPresence("daemon-1") {
		t.Fatal("current assignment fence did not close presence")
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
	if _, err := fixture.service.OpenEdgePresence(context.Background(), token, "account-1", revokedProof); !errors.Is(err, hub.ErrPrincipalRevoked) {
		t.Fatalf("revoked Hub presence proof error = %v", err)
	}

	fresh := newFixture(t, 4, 4)
	fresh.clock.Advance(11 * time.Minute)
	if _, err := fresh.service.BeginEdgePresence(context.Background(), fresh.issueDaemonEdgeToken(t), "account-1", "daemon-1"); !errors.Is(err, hub.ErrPolicySnapshot) {
		t.Fatalf("stale-policy Hub presence begin error = %v", err)
	}
}

func TestHubEdgePresencePreservesRevocationClassification(t *testing.T) {
	fixture := newFixture(t, 4, 4)
	if err := fixture.edgeAuthorizer.ApplySnapshot(hub.AuthorizationSnapshot{
		Revision: 2, GeneratedAt: fixture.clock.Now(),
		Accounts: []hub.AccountAuthorization{activeHubP2PAccount("account-1", 1, fixture.clock.Now())},
		Devices:  []hub.DeviceAuthorization{{DeviceID: "client-1", AccountID: "account-1", Kind: "client", DisplayName: "Client"}, {DeviceID: "daemon-1", AccountID: "account-1", Kind: "daemon", DisplayName: "Daemon", PublicKey: fixture.daemonPublicKey, Revoked: true}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.BeginEdgePresence(context.Background(), fixture.issueDaemonEdgeToken(t), "account-1", "daemon-1"); !errors.Is(err, hub.ErrPrincipalRevoked) {
		t.Fatalf("revoked presence error = %v", err)
	}
}

type fixture struct {
	now              time.Time
	clock            *fakeClock
	signer           servicecredential.Signer
	edgeAuthorizer   *hub.EdgeAuthorizer
	assignmentSource *assignmentSource
	daemonPublicKey  ed25519.PublicKey
	daemonPrivateKey ed25519.PrivateKey
	service          *hub.Service
}

type assignmentSource struct {
	mu    sync.Mutex
	epoch uint64
}

func (source *assignmentSource) ActiveAssignment(deviceID string) (uint64, bool) {
	source.mu.Lock()
	defer source.mu.Unlock()
	return source.epoch, deviceID == "daemon-1" && source.epoch != 0
}

func (source *assignmentSource) Set(epoch uint64) {
	source.mu.Lock()
	source.epoch = epoch
	source.mu.Unlock()
}

func newFixture(t *testing.T, presenceQueue, clientQueue int) fixture {
	return newFixtureWithTTLs(t, presenceQueue, clientQueue, 5*time.Minute, 5*time.Minute)
}

func newFixtureWithTTLs(t *testing.T, presenceQueue, clientQueue int, presenceTTL, signalingTTL time.Duration) fixture {
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
	if err := edgeAuthorizer.ApplySnapshot(hub.AuthorizationSnapshot{Revision: 1, GeneratedAt: now, Accounts: []hub.AccountAuthorization{activeHubP2PAccount("account-1", 1, now)}, Devices: []hub.DeviceAuthorization{{DeviceID: "client-1", AccountID: "account-1", Kind: "client", DisplayName: "Client", AuthEpoch: 1}, {DeviceID: "daemon-1", AccountID: "account-1", Kind: "daemon", DisplayName: "Daemon", PublicKey: daemonPublicKey, AuthEpoch: 1}}}); err != nil {
		t.Fatal(err)
	}
	assignment := &assignmentSource{epoch: 1}
	service, err := hub.New(hub.Config{HubID: "hub-eu", Clock: clock, MaxPresenceTTL: presenceTTL, MaxSignalingTTL: signalingTTL, PresenceChallengeTTL: time.Minute, MaxPresenceChallenges: 16, PresenceQueueSize: presenceQueue, ClientQueueSize: clientQueue, MaxSDPBytes: 1024, MaxCandidates: 8, MaxPresences: 16, MaxSessions: 32, MaxSessionsPerClient: 4, EdgeAuthorizer: edgeAuthorizer, AssignmentSource: assignment})
	if err != nil {
		t.Fatal(err)
	}
	return fixture{now: now, clock: clock, signer: signer, edgeAuthorizer: edgeAuthorizer, assignmentSource: assignment, daemonPublicKey: daemonPublicKey, daemonPrivateKey: daemonPrivateKey, service: service}
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
