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
		Candidates: []hub.Candidate{{Candidate: "candidate:offer"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	offerEvent, err := presence.Receive(context.Background())
	if err != nil || offerEvent.Offer == nil || offerEvent.Offer.SignalingSessionID != "signal-1" || offerEvent.Offer.TargetDeviceID != "daemon-1" {
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
	if _, err := fixture.service.CreateSession(context.Background(), hub.CreateSessionRequest{Admission: wrongTarget, AccountID: "account-1", ClientDeviceID: "client-1", TargetDeviceID: "daemon-1", ManagedSessionID: "managed-wrong", SignalingSessionID: "wrong", SDP: "offer"}); !errors.Is(err, hub.ErrAdmission) {
		t.Fatalf("wrong target error = %v", err)
	}

	first := fixture.issue(t, "client-1", servicecredential.PrincipalClient, "client-1", "managed-1", "daemon-1", []servicecredential.HubOperation{servicecredential.HubOperationOffer}, time.Minute)
	if _, err := fixture.service.CreateSession(context.Background(), hub.CreateSessionRequest{Admission: first, AccountID: "account-1", ClientDeviceID: "client-1", TargetDeviceID: "daemon-1", ManagedSessionID: "managed-1", SignalingSessionID: "signal-1", SDP: "offer"}); err != nil {
		t.Fatal(err)
	}
	second := fixture.issue(t, "client-2", servicecredential.PrincipalClient, "client-2", "managed-2", "daemon-1", []servicecredential.HubOperation{servicecredential.HubOperationOffer}, time.Minute)
	if _, err := fixture.service.CreateSession(context.Background(), hub.CreateSessionRequest{Admission: second, AccountID: "account-1", ClientDeviceID: "client-2", TargetDeviceID: "daemon-1", ManagedSessionID: "managed-2", SignalingSessionID: "signal-2", SDP: "offer"}); !errors.Is(err, hub.ErrBackpressure) {
		t.Fatalf("presence backpressure error = %v", err)
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
	client, err := fixture.service.CreateSession(context.Background(), hub.CreateSessionRequest{Admission: clientTicket, AccountID: "account-1", ClientDeviceID: "client-1", TargetDeviceID: "daemon-1", ManagedSessionID: "managed-1", SignalingSessionID: "signal", SDP: "offer"})
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
	service, err := hub.New(hub.Config{HubID: "hub-eu", AdmissionIssuer: "control-plane.test", KeyRing: ring, Clock: clock, MaxPresenceTTL: 5 * time.Minute, MaxSignalingTTL: 5 * time.Minute, PresenceQueueSize: presenceQueue, ClientQueueSize: clientQueue, MaxSDPBytes: 1024, MaxCandidates: 8, MaxPresences: 16, MaxSessions: 32, MaxSessionsPerClient: 4, MaxReplayEntries: 64})
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
	ticket, err := issuer.Issue(servicecredential.HubAdmissionRequest{TicketID: ticketID, AudienceHubID: "hub-eu", PrincipalKind: principal, AccountID: "account-1", DeviceID: deviceID, ManagedSessionID: managedSessionID, TargetDeviceID: targetDeviceID, AllowedOperations: operations, TTL: ttl}, fixture.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	return ticket.Bytes()
}
