package cloud_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-remote/hub/cloud"
	"github.com/lozzow/termx/termx-remote/hub/registry"
)

func TestCloudSignalingOpaqueTicketAndAnswerFlow(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := fixedClock(time.Date(2026, 5, 3, 5, 44, 0, 0, time.UTC))
	reg := registry.New(registry.Config{Clock: clock})
	if _, err := reg.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	svc := cloud.NewService(cloud.Config{Registry: reg, Clock: clock})
	offer, err := svc.SubmitOffer(ctx, cloud.SubmitOfferInput{
		TicketID:   "opaque_ticket",
		MachineID:  "mach_1",
		TerminalID: "term_1",
		SDP:        minimalSDP("offer"),
	})
	if err != nil {
		t.Fatalf("submit offer: %v", err)
	}
	if offer.Path != cloud.PathCloud || offer.RelayInUse || offer.AllowRelay {
		t.Fatalf("offer path/relay = %+v", offer)
	}
	if offer.TicketID != "opaque_ticket" || offer.MachineID != "mach_1" || offer.TerminalID != "term_1" {
		t.Fatalf("offer identity = %+v", offer)
	}
	polled, err := svc.PollAgentOffer(ctx, cloud.PollAgentOfferInput{
		AgentID:   "agent_1",
		MachineID: "mach_1",
		Timeout:   time.Second,
	})
	if err != nil {
		t.Fatalf("poll agent offer: %v", err)
	}
	if polled.ID != offer.ID || polled.SDP != minimalSDP("offer") || polled.AllowRelay {
		t.Fatalf("polled offer = %+v", polled)
	}
	if err := svc.SubmitAnswer(ctx, cloud.SubmitAnswerInput{
		AgentID:   "agent_1",
		MachineID: "mach_1",
		OfferID:   offer.ID,
		SDP:       minimalSDP("answer"),
	}); err != nil {
		t.Fatalf("submit answer: %v", err)
	}
	answer, err := svc.GetAnswer(ctx, cloud.GetAnswerInput{
		OfferID:   offer.ID,
		TicketID:  "opaque_ticket",
		MachineID: "mach_1",
	})
	if err != nil {
		t.Fatalf("get answer: %v", err)
	}
	if answer.SDP != minimalSDP("answer") || answer.RelayInUse {
		t.Fatalf("answer = %+v", answer)
	}
	ticket, err := svc.ConsumeOfferTicket(ctx, cloud.GetAnswerInput{
		OfferID:   offer.ID,
		TicketID:  "opaque_ticket",
		MachineID: "mach_1",
	})
	if err != nil {
		t.Fatalf("consume opaque ticket: %v", err)
	}
	if ticket.ID != "opaque_ticket" || ticket.MachineID != "mach_1" || ticket.TerminalID != "term_1" || ticket.AllowRelay {
		t.Fatalf("opaque ticket metadata = %+v", ticket)
	}
}

func TestCloudServiceDefaultRelayAllowancePropagatesToOfferAndTicket(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := fixedClock(time.Date(2026, 5, 5, 14, 20, 0, 0, time.UTC))
	reg := registry.New(registry.Config{Clock: clock})
	if _, err := reg.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	svc := cloud.NewService(cloud.Config{Registry: reg, Clock: clock, AllowRelayByDefault: true})
	offer, err := svc.SubmitOffer(ctx, cloud.SubmitOfferInput{
		TicketID:   "ticket_with_relay",
		MachineID:  "mach_1",
		TerminalID: "term_1",
		SDP:        minimalSDP("offer"),
	})
	if err != nil {
		t.Fatalf("submit offer: %v", err)
	}
	if !offer.AllowRelay {
		t.Fatalf("submitted offer should allow relay when cloud config enables it: %+v", offer)
	}
	polled, err := svc.PollAgentOffer(ctx, cloud.PollAgentOfferInput{
		AgentID:   "agent_1",
		MachineID: "mach_1",
		Timeout:   time.Second,
	})
	if err != nil {
		t.Fatalf("poll agent offer: %v", err)
	}
	if !polled.AllowRelay {
		t.Fatalf("polled offer should preserve relay allowance: %+v", polled)
	}
	ticket, err := svc.ConsumeOfferTicket(ctx, cloud.GetAnswerInput{
		OfferID:   offer.ID,
		TicketID:  "ticket_with_relay",
		MachineID: "mach_1",
	})
	if err != nil {
		t.Fatalf("consume ticket: %v", err)
	}
	if !ticket.AllowRelay {
		t.Fatalf("ticket should preserve relay allowance: %+v", ticket)
	}
}

func TestCloudSubmitOfferRequiresOnlineMachine(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := fixedClock(time.Date(2026, 5, 3, 5, 49, 0, 0, time.UTC))
	reg := registry.New(registry.Config{Clock: clock})
	svc := cloud.NewService(cloud.Config{Registry: reg, Clock: clock})
	if _, err := svc.SubmitOffer(ctx, cloud.SubmitOfferInput{
		TicketID:   "opaque_ticket",
		MachineID:  "mach_1",
		TerminalID: "term_1",
		SDP:        minimalSDP("offer-no-agent"),
	}); !errors.Is(err, registry.ErrAgentNotFound) {
		t.Fatalf("offline offer err = %v", err)
	}
}

func TestCloudAnswerRequiresOpaqueTicketAndMachineCorrelation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := fixedClock(time.Date(2026, 5, 3, 5, 54, 0, 0, time.UTC))
	reg := registry.New(registry.Config{Clock: clock})
	if _, err := reg.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	svc := cloud.NewService(cloud.Config{Registry: reg, Clock: clock})
	offer, err := svc.SubmitOffer(ctx, cloud.SubmitOfferInput{
		TicketID:   "ticket_allowed",
		MachineID:  "mach_1",
		TerminalID: "term_1",
		SDP:        minimalSDP("offer"),
	})
	if err != nil {
		t.Fatalf("submit offer: %v", err)
	}
	if _, err := svc.PollAgentOffer(ctx, cloud.PollAgentOfferInput{AgentID: "agent_1", MachineID: "mach_1", Timeout: time.Second}); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if err := svc.SubmitAnswer(ctx, cloud.SubmitAnswerInput{
		AgentID:   "agent_1",
		MachineID: "mach_1",
		OfferID:   offer.ID,
		SDP:       minimalSDP("answer"),
	}); err != nil {
		t.Fatalf("answer: %v", err)
	}
	if _, err := svc.GetAnswer(ctx, cloud.GetAnswerInput{
		OfferID:   offer.ID,
		TicketID:  "ticket_wrong",
		MachineID: "mach_1",
	}); !errors.Is(err, cloud.ErrWrongMachine) {
		t.Fatalf("wrong ticket answer err = %v", err)
	}
	if _, err := svc.GetAnswer(ctx, cloud.GetAnswerInput{
		OfferID:   offer.ID,
		TicketID:  "ticket_allowed",
		MachineID: "wrong_machine",
	}); !errors.Is(err, cloud.ErrWrongMachine) {
		t.Fatalf("wrong machine answer err = %v", err)
	}
	if _, err := svc.GetAnswer(ctx, cloud.GetAnswerInput{OfferID: offer.ID}); !errors.Is(err, cloud.ErrWrongMachine) {
		t.Fatalf("missing ticket answer err = %v", err)
	}
	if _, err := svc.GetAnswer(ctx, cloud.GetAnswerInput{
		OfferID:   offer.ID,
		TicketID:  "ticket_allowed",
		MachineID: "mach_1",
	}); err != nil {
		t.Fatalf("authorized answer: %v", err)
	}
}

func TestCloudAnswerLookupScopesPublicSessionIDByTicket(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := fixedClock(time.Date(2026, 5, 3, 10, 43, 0, 0, time.UTC))
	reg := registry.New(registry.Config{Clock: clock})
	if _, err := reg.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	svc := cloud.NewService(cloud.Config{Registry: reg, Clock: clock})
	offerA, err := svc.SubmitOffer(ctx, cloud.SubmitOfferInput{
		SessionID:  "rtc_same",
		TicketID:   "ticket_a",
		MachineID:  "mach_1",
		TerminalID: "term_1",
		SDP:        minimalSDP("offer-a"),
	})
	if err != nil {
		t.Fatalf("submit offer a: %v", err)
	}
	if _, err := svc.SubmitOffer(ctx, cloud.SubmitOfferInput{
		SessionID:  "rtc_same",
		TicketID:   "ticket_b",
		MachineID:  "mach_1",
		TerminalID: "term_1",
		SDP:        minimalSDP("offer-b"),
	}); err != nil {
		t.Fatalf("submit offer b: %v", err)
	}
	polled, err := svc.PollAgentOffer(ctx, cloud.PollAgentOfferInput{
		AgentID:   "agent_1",
		MachineID: "mach_1",
		Timeout:   time.Second,
	})
	if err != nil {
		t.Fatalf("poll offer a: %v", err)
	}
	if polled.ID != offerA.ID {
		t.Fatalf("polled first offer = %s, want %s", polled.ID, offerA.ID)
	}
	if err := svc.SubmitAnswer(ctx, cloud.SubmitAnswerInput{
		AgentID:   "agent_1",
		MachineID: "mach_1",
		OfferID:   offerA.ID,
		SDP:       minimalSDP("answer-a"),
	}); err != nil {
		t.Fatalf("submit answer a: %v", err)
	}
	answer, err := svc.GetAnswer(ctx, cloud.GetAnswerInput{
		OfferID:   "rtc_same",
		TicketID:  "ticket_a",
		MachineID: "mach_1",
	})
	if err != nil {
		t.Fatalf("get answer by scoped public session id: %v", err)
	}
	if answer.OfferID != offerA.ID || answer.SDP != minimalSDP("answer-a") {
		t.Fatalf("answer = %+v, want offer %s", answer, offerA.ID)
	}
}

func TestCloudOfferPolicyCacheIsTTLBounded(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := &mutableClock{value: time.Date(2026, 5, 3, 18, 58, 0, 0, time.UTC)}
	reg := registry.New(registry.Config{Clock: clock})
	if _, err := reg.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	svc := cloud.NewService(cloud.Config{
		Registry:  reg,
		Clock:     clock,
		OfferTTL:  time.Second,
		MaxOffers: 1,
	})
	first, err := svc.SubmitOffer(ctx, cloud.SubmitOfferInput{
		SessionID:  "rtc_a",
		TicketID:   "ticket_a",
		MachineID:  "mach_1",
		TerminalID: "term_1",
		SDP:        minimalSDP("offer-a"),
	})
	if err != nil {
		t.Fatalf("submit first: %v", err)
	}
	if _, err := svc.SubmitOffer(ctx, cloud.SubmitOfferInput{
		SessionID:  "rtc_b",
		TicketID:   "ticket_b",
		MachineID:  "mach_1",
		TerminalID: "term_1",
		SDP:        minimalSDP("offer-b"),
	}); err != nil {
		t.Fatalf("submit second: %v", err)
	}
	if _, err := svc.GetAnswer(ctx, cloud.GetAnswerInput{
		OfferID:   first.ID,
		TicketID:  "ticket_a",
		MachineID: "mach_1",
	}); !errors.Is(err, cloud.ErrWrongMachine) {
		t.Fatalf("evicted offer answer err = %v", err)
	}
	clock.value = clock.value.Add(2 * time.Second)
	removed := svc.CleanupExpired(ctx)
	if removed == 0 {
		t.Fatal("expected expired cloud offer policy cache entries to be cleaned")
	}
}

func TestCloudSignalingRejectsRuntimePayload(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := fixedClock(time.Date(2026, 5, 3, 5, 45, 0, 0, time.UTC))
	reg := registry.New(registry.Config{Clock: clock})
	if _, err := reg.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	svc := cloud.NewService(cloud.Config{Registry: reg, Clock: clock})
	if _, err := svc.SubmitOffer(ctx, cloud.SubmitOfferInput{
		TicketID:   "ticket_allowed",
		MachineID:  "mach_1",
		TerminalID: "term_1",
		SDP:        "v=0\r\nterminal_data=must-not-forward",
	}); !errors.Is(err, registry.ErrRuntimePayload) {
		t.Fatalf("runtime payload err = %v", err)
	}
	if _, err := svc.SubmitOffer(ctx, cloud.SubmitOfferInput{
		TicketID:   "ticket_allowed",
		MachineID:  "mach_1",
		TerminalID: "term_1",
		SDP:        minimalSDP("offer-after-runtime-reject"),
	}); err != nil {
		t.Fatalf("valid offer after runtime reject should reuse unconsumed ticket: %v", err)
	}
}

func TestCloudSignalingRejectsTicketReuse(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := fixedClock(time.Date(2026, 5, 3, 5, 51, 0, 0, time.UTC))
	reg := registry.New(registry.Config{Clock: clock})
	if _, err := reg.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	svc := cloud.NewService(cloud.Config{Registry: reg, Clock: clock})
	if _, err := svc.SubmitOffer(ctx, cloud.SubmitOfferInput{
		TicketID:   "ticket_once",
		MachineID:  "mach_1",
		TerminalID: "term_1",
		SDP:        minimalSDP("offer-1"),
	}); err != nil {
		t.Fatalf("first offer: %v", err)
	}
	if _, err := svc.SubmitOffer(ctx, cloud.SubmitOfferInput{
		TicketID:   "ticket_once",
		MachineID:  "mach_1",
		TerminalID: "term_1",
		SDP:        minimalSDP("offer-2"),
	}); !errors.Is(err, cloud.ErrTicketUsed) {
		t.Fatalf("second offer err = %v", err)
	}
}

type fixedClock time.Time

func (c fixedClock) Now() time.Time {
	return time.Time(c)
}

type mutableClock struct {
	value time.Time
}

func (c *mutableClock) Now() time.Time {
	return c.value
}

func minimalSDP(sessionID string) string {
	return "v=0\r\no=- " + sessionID + " 1 IN IP4 127.0.0.1\r\ns=-\r\nt=0 0\r\nm=application 9 UDP/DTLS/SCTP webrtc-datachannel"
}
