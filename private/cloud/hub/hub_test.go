package hub_test

import (
	"context"
	"crypto/ed25519"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/lozzow/termx/private/cloud/control-plane/servicecredential"
	"github.com/lozzow/termx/private/cloud/hub"
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

func TestHubRoutesOfferCandidateAndAsyncAnswerWithSeparateAdmissions(t *testing.T) {
	fixture := newFixture(t, 4, 4)
	presenceTicket := fixture.issue(t, "presence-ticket", servicecredential.PrincipalDaemon, "daemon-1", "presence-1", "", []servicecredential.HubOperation{servicecredential.HubOperationPresence}, 4*time.Minute)
	presence, err := fixture.service.OpenPresence(context.Background(), hub.OpenPresenceRequest{Admission: presenceTicket, AccountID: "account-1", DeviceID: "daemon-1", PresenceSession: "presence-1"})
	if err != nil {
		t.Fatal(err)
	}
	defer presence.Close()

	clientTicket := fixture.issue(t, "client-ticket", servicecredential.PrincipalClient, "client-1", "managed-1", "daemon-1", []servicecredential.HubOperation{servicecredential.HubOperationOffer, servicecredential.HubOperationCandidate}, 3*time.Minute)
	client, err := fixture.service.CreateSession(context.Background(), hub.CreateSessionRequest{
		Admission: clientTicket, AccountID: "account-1", ClientDeviceID: "client-1", TargetDeviceID: "daemon-1",
		ManagedSessionID: "managed-1", SignalingSessionID: "signal-1", SDP: "offer-sdp",
		Candidates: []hub.Candidate{{Candidate: "candidate:offer"}}, RoutePreference: hub.RoutePreferenceStandardRelay, RelayOnly: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	offerEvent, err := presence.Receive(context.Background())
	if err != nil || offerEvent.Offer == nil || offerEvent.Offer.SignalingSessionID != "signal-1" || offerEvent.Offer.TargetDeviceID != "daemon-1" ||
		offerEvent.Offer.RoutePreference != hub.RoutePreferenceStandardRelay || !offerEvent.Offer.RelayOnly {
		t.Fatalf("presence offer = (%#v, %v)", offerEvent, err)
	}
	if err := client.SendCandidate(hub.Candidate{Candidate: "candidate:client-trickle"}); err != nil {
		t.Fatal(err)
	}
	candidateEvent, err := presence.Receive(context.Background())
	if err != nil || candidateEvent.Candidate == nil || candidateEvent.Candidate.SignalingSessionID != "signal-1" {
		t.Fatalf("presence candidate = (%#v, %v)", candidateEvent, err)
	}

	answerTicket := fixture.issue(t, "answer-ticket", servicecredential.PrincipalDaemon, "daemon-1", "managed-1", "", []servicecredential.HubOperation{servicecredential.HubOperationAnswer, servicecredential.HubOperationCandidate}, 2*time.Minute)
	daemon, err := fixture.service.CompleteAnswer(context.Background(), hub.CompleteAnswerRequest{
		Admission: answerTicket, AccountID: "account-1", DaemonDeviceID: "daemon-1", ManagedSessionID: "managed-1",
		SignalingSessionID: "signal-1", SDP: "answer-sdp", Candidates: []hub.Candidate{{Candidate: "candidate:answer"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	answerEvent, err := client.Receive(context.Background())
	if err != nil || answerEvent.Answer == nil || answerEvent.Answer.SDP != "answer-sdp" {
		t.Fatalf("client answer = (%#v, %v)", answerEvent, err)
	}
	if err := daemon.SendCandidate(hub.Candidate{Candidate: "candidate:daemon-trickle"}); err != nil {
		t.Fatal(err)
	}
	daemonCandidate, err := client.Receive(context.Background())
	if err != nil || daemonCandidate.Candidate == nil || daemonCandidate.Candidate.SignalingSessionID != "signal-1" {
		t.Fatalf("daemon candidate = (%#v, %v)", daemonCandidate, err)
	}
	if _, err := fixture.service.CompleteAnswer(context.Background(), hub.CompleteAnswerRequest{
		Admission: answerTicket, AccountID: "account-1", DaemonDeviceID: "daemon-1", ManagedSessionID: "managed-1", SignalingSessionID: "signal-1", SDP: "second",
	}); !errors.Is(err, hub.ErrAdmission) {
		t.Fatalf("replayed answer ticket error = %v", err)
	}
}

func TestHubCreatesEdgeSessionAndAcceptsAnswerFromOwningPresence(t *testing.T) {
	fixture := newFixture(t, 4, 4)
	presenceTicket := fixture.issue(t, "edge-presence", servicecredential.PrincipalDaemon, "daemon-1", "presence-edge", "", []servicecredential.HubOperation{servicecredential.HubOperationPresence}, 4*time.Minute)
	presence, err := fixture.service.OpenPresence(context.Background(), hub.OpenPresenceRequest{Admission: presenceTicket, AccountID: "account-1", DeviceID: "daemon-1", PresenceSession: "presence-edge"})
	if err != nil {
		t.Fatal(err)
	}
	defer presence.Close()
	edgeIssuer, err := servicecredential.NewEdgeAccessIssuer("control-plane.test", fixture.signer)
	if err != nil {
		t.Fatal(err)
	}
	token, err := edgeIssuer.IssueEdgeAccess("edge-token", "hub-eu", "account-1", "client-1", 1, time.Hour, fixture.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	daemonToken, err := edgeIssuer.IssueEdgeAccessForPrincipal("daemon-edge-token", "hub-eu", "account-1", "daemon-1", servicecredential.EdgePrincipalDaemon, 1, time.Hour, fixture.clock.Now())
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
	if _, err := fixture.service.CompleteEdgeAnswer(context.Background(), hub.CompleteEdgeAnswerRequest{EdgeToken: daemonToken, AccountID: "account-1", DaemonDeviceID: "daemon-1", PresenceSessionID: "presence-edge", SignalingSessionID: "signal-edge", SDP: "answer"}); err != nil {
		t.Fatal(err)
	}
	answer, err := client.Receive(context.Background())
	if err != nil || answer.Answer == nil || answer.Answer.SDP != "answer" {
		t.Fatalf("edge answer = (%#v, %v)", answer, err)
	}
}

func TestHubRejectsTicketReplayWrongTargetAndBackpressure(t *testing.T) {
	fixture := newFixture(t, 1, 1)
	presenceTicket := fixture.issue(t, "presence", servicecredential.PrincipalDaemon, "daemon-1", "presence-1", "", []servicecredential.HubOperation{servicecredential.HubOperationPresence}, time.Minute)
	presence, err := fixture.service.OpenPresence(context.Background(), hub.OpenPresenceRequest{Admission: presenceTicket, AccountID: "account-1", DeviceID: "daemon-1", PresenceSession: "presence-1"})
	if err != nil {
		t.Fatal(err)
	}
	defer presence.Close()
	if _, err := fixture.service.OpenPresence(context.Background(), hub.OpenPresenceRequest{Admission: presenceTicket, AccountID: "account-1", DeviceID: "daemon-1", PresenceSession: "presence-1"}); !errors.Is(err, hub.ErrAdmission) {
		t.Fatalf("presence replay error = %v", err)
	}

	wrongTarget := fixture.issue(t, "wrong-target", servicecredential.PrincipalClient, "client-1", "managed-wrong", "daemon-2", []servicecredential.HubOperation{servicecredential.HubOperationOffer}, time.Minute)
	if _, err := fixture.service.CreateSession(context.Background(), hub.CreateSessionRequest{Admission: wrongTarget, AccountID: "account-1", ClientDeviceID: "client-1", TargetDeviceID: "daemon-1", ManagedSessionID: "managed-wrong", SignalingSessionID: "wrong", SDP: "offer", RoutePreference: hub.RoutePreferenceDirectOnly}); !errors.Is(err, hub.ErrAdmission) {
		t.Fatalf("wrong target error = %v", err)
	}

	first := fixture.issue(t, "client-1", servicecredential.PrincipalClient, "client-1", "managed-1", "daemon-1", []servicecredential.HubOperation{servicecredential.HubOperationOffer}, time.Minute)
	if _, err := fixture.service.CreateSession(context.Background(), hub.CreateSessionRequest{Admission: first, AccountID: "account-1", ClientDeviceID: "client-1", TargetDeviceID: "daemon-1", ManagedSessionID: "managed-1", SignalingSessionID: "signal-1", SDP: "offer", RoutePreference: hub.RoutePreferenceDirectOnly}); err != nil {
		t.Fatal(err)
	}
	second := fixture.issue(t, "client-2", servicecredential.PrincipalClient, "client-2", "managed-2", "daemon-1", []servicecredential.HubOperation{servicecredential.HubOperationOffer}, time.Minute)
	if _, err := fixture.service.CreateSession(context.Background(), hub.CreateSessionRequest{Admission: second, AccountID: "account-1", ClientDeviceID: "client-2", TargetDeviceID: "daemon-1", ManagedSessionID: "managed-2", SignalingSessionID: "signal-2", SDP: "offer", RoutePreference: hub.RoutePreferenceDirectOnly}); !errors.Is(err, hub.ErrBackpressure) {
		t.Fatalf("presence backpressure error = %v", err)
	}
}

func TestHubRoutesStableFailureWithoutRawMessage(t *testing.T) {
	fixture := newFixture(t, 2, 2)
	presenceTicket := fixture.issue(t, "presence-failure", servicecredential.PrincipalDaemon, "daemon-1", "presence-failure", "", []servicecredential.HubOperation{servicecredential.HubOperationPresence}, time.Minute)
	presence, err := fixture.service.OpenPresence(context.Background(), hub.OpenPresenceRequest{Admission: presenceTicket, AccountID: "account-1", DeviceID: "daemon-1", PresenceSession: "presence-failure"})
	if err != nil {
		t.Fatal(err)
	}
	defer presence.Close()
	clientTicket := fixture.issue(t, "client-failure", servicecredential.PrincipalClient, "client-1", "managed-failure", "daemon-1", []servicecredential.HubOperation{servicecredential.HubOperationOffer}, time.Minute)
	client, err := fixture.service.CreateSession(context.Background(), hub.CreateSessionRequest{
		Admission: clientTicket, AccountID: "account-1", ClientDeviceID: "client-1", TargetDeviceID: "daemon-1",
		ManagedSessionID: "managed-failure", SignalingSessionID: "signal-failure", SDP: "offer", RoutePreference: hub.RoutePreferenceDirectOnly,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if _, err := presence.Receive(context.Background()); err != nil {
		t.Fatal(err)
	}
	failureTicket := fixture.issue(t, "answer-failure", servicecredential.PrincipalDaemon, "daemon-1", "managed-failure", "", []servicecredential.HubOperation{servicecredential.HubOperationAnswer}, time.Minute)
	if err := fixture.service.CompleteFailure(context.Background(), hub.CompleteFailureRequest{
		Admission: failureTicket, AccountID: "account-1", DaemonDeviceID: "daemon-1",
		ManagedSessionID: "managed-failure", SignalingSessionID: "signal-failure", Code: 12, Retryable: true,
	}); err != nil {
		t.Fatal(err)
	}
	event, err := client.Receive(context.Background())
	if err != nil || event.Failure == nil || event.Failure.Code != 12 || !event.Failure.Retryable {
		t.Fatalf("client failure = (%#v, %v)", event, err)
	}
	if _, err := client.Receive(context.Background()); !errors.Is(err, io.EOF) {
		t.Fatalf("terminal failure did not close session: %v", err)
	}
}

func TestHubCleanupExpiresPresenceAndAssociatedSession(t *testing.T) {
	fixture := newFixture(t, 2, 2)
	presenceTicket := fixture.issue(t, "presence", servicecredential.PrincipalDaemon, "daemon-1", "presence-1", "", []servicecredential.HubOperation{servicecredential.HubOperationPresence}, 30*time.Second)
	presence, err := fixture.service.OpenPresence(context.Background(), hub.OpenPresenceRequest{Admission: presenceTicket, AccountID: "account-1", DeviceID: "daemon-1", PresenceSession: "presence-1"})
	if err != nil {
		t.Fatal(err)
	}
	clientTicket := fixture.issue(t, "client", servicecredential.PrincipalClient, "client-1", "managed-1", "daemon-1", []servicecredential.HubOperation{servicecredential.HubOperationOffer}, 30*time.Second)
	client, err := fixture.service.CreateSession(context.Background(), hub.CreateSessionRequest{Admission: clientTicket, AccountID: "account-1", ClientDeviceID: "client-1", TargetDeviceID: "daemon-1", ManagedSessionID: "managed-1", SignalingSessionID: "signal", SDP: "offer", RoutePreference: hub.RoutePreferenceDirectOnly})
	if err != nil {
		t.Fatal(err)
	}
	fixture.clock.Advance(time.Minute)
	fixture.service.Cleanup()
	if _, err := presence.Receive(context.Background()); !errors.Is(err, io.EOF) {
		t.Fatalf("expired presence Receive error = %v", err)
	}
	event, err := client.Receive(context.Background())
	if err != nil || event.Closed == nil {
		t.Fatalf("expired client event = (%#v, %v)", event, err)
	}
}

type fixture struct {
	now     time.Time
	clock   *fakeClock
	signer  servicecredential.Signer
	service *hub.Service
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
	edgeAuthorizer, err := hub.NewEdgeAuthorizer(hub.EdgeAuthorizerConfig{HubID: "hub-eu", Issuer: "control-plane.test", KeyRing: ring, Clock: clock, MaxStaleness: 10 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if err := edgeAuthorizer.ApplySnapshot(hub.AuthorizationSnapshot{Revision: 1, GeneratedAt: now, Accounts: []hub.AccountAuthorization{{AccountID: "account-1", AuthEpoch: 1, ManagedDirectEnabled: true}}, Devices: []hub.DeviceAuthorization{{DeviceID: "daemon-1", AccountID: "account-1"}}}); err != nil {
		t.Fatal(err)
	}
	service, err := hub.New(hub.Config{HubID: "hub-eu", AdmissionIssuer: "control-plane.test", KeyRing: ring, Clock: clock, MaxPresenceTTL: 5 * time.Minute, MaxSignalingTTL: 5 * time.Minute, PresenceQueueSize: presenceQueue, ClientQueueSize: clientQueue, MaxSDPBytes: 1024, MaxCandidates: 8, MaxPresences: 16, MaxSessions: 32, MaxSessionsPerClient: 4, MaxReplayEntries: 64, EdgeAuthorizer: edgeAuthorizer})
	if err != nil {
		t.Fatal(err)
	}
	return fixture{now: now, clock: clock, signer: signer, service: service}
}

func (fixture fixture) issue(t *testing.T, ticketID string, principal servicecredential.PrincipalKind, deviceID, managedSessionID, targetDeviceID string, operations []servicecredential.HubOperation, ttl time.Duration) []byte {
	t.Helper()
	issuer, err := servicecredential.NewHubAdmissionIssuer("control-plane.test", fixture.signer)
	if err != nil {
		t.Fatal(err)
	}
	sessionKind := servicecredential.HubSessionManaged
	if len(operations) == 1 && operations[0] == servicecredential.HubOperationPresence {
		sessionKind = servicecredential.HubSessionPresence
	}
	ticket, err := issuer.Issue(servicecredential.HubAdmissionRequest{TicketID: ticketID, AudienceHubID: "hub-eu", PrincipalKind: principal, AccountID: "account-1", DeviceID: deviceID, SessionKind: sessionKind, SessionID: managedSessionID, TargetDeviceID: targetDeviceID, AllowedOperations: operations, TTL: ttl}, fixture.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	return ticket.Bytes()
}
