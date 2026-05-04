package cloud_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-hub/internal/cloud"
	"github.com/lozzow/termx/termx-hub/internal/registry"
)

func TestCloudSignalingTicketPolicyAndAnswerFlow(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := fixedClock(time.Date(2026, 5, 3, 5, 44, 0, 0, time.UTC))
	verifier := &fakeTicketVerifier{now: clock.Now(), tickets: map[string]cloud.Ticket{
		"ticket_allowed": {
			ID:         "ticket_allowed",
			MachineID:  "mach_1",
			TerminalID: "term_1",
			Path:       cloud.PathCloud,
			AllowRelay: false,
			ExpiresAt:  clock.Now().Add(time.Minute),
		},
		"ticket_expired": {
			ID:         "ticket_expired",
			MachineID:  "mach_1",
			TerminalID: "term_1",
			Path:       cloud.PathCloud,
			AllowRelay: false,
			ExpiresAt:  clock.Now().Add(-time.Second),
		},
	}}
	reg := registry.New(registry.Config{Clock: clock, Verifier: verifier})
	if _, err := reg.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	svc := cloud.NewService(cloud.Config{Registry: reg, Tickets: verifier, Clock: clock})
	if _, err := svc.SubmitOffer(ctx, cloud.SubmitOfferInput{
		TicketID:   "ticket_expired",
		MachineID:  "mach_1",
		TerminalID: "term_1",
		SDP:        minimalSDP("offer-expired"),
	}); !errors.Is(err, cloud.ErrTicketExpired) {
		t.Fatalf("expired ticket err = %v", err)
	}
	if _, err := svc.SubmitOffer(ctx, cloud.SubmitOfferInput{
		TicketID:   "ticket_allowed",
		MachineID:  "wrong_machine",
		TerminalID: "term_1",
		SDP:        minimalSDP("offer-wrong"),
	}); !errors.Is(err, cloud.ErrWrongMachine) {
		t.Fatalf("wrong machine err = %v", err)
	}
	verifier.terminalOverride = "wrong_terminal"
	if _, err := svc.SubmitOffer(ctx, cloud.SubmitOfferInput{
		TicketID:   "ticket_allowed",
		MachineID:  "mach_1",
		TerminalID: "term_1",
		SDP:        minimalSDP("offer-terminal-mismatch"),
	}); !errors.Is(err, cloud.ErrWrongTerminal) {
		t.Fatalf("wrong terminal err = %v", err)
	}
	verifier.terminalOverride = ""
	offer, err := svc.SubmitOffer(ctx, cloud.SubmitOfferInput{
		TicketID:   "ticket_allowed",
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
	polled, err := svc.PollAgentOffer(ctx, cloud.PollAgentOfferInput{
		AgentID:   "agent_1",
		MachineID: "mach_1",
		Timeout:   time.Second,
	})
	if err != nil {
		t.Fatalf("poll agent offer: %v", err)
	}
	if polled.ID != offer.ID || polled.SDP != minimalSDP("offer") {
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
		TicketID:  "ticket_allowed",
		MachineID: "mach_1",
	})
	if err != nil {
		t.Fatalf("get answer: %v", err)
	}
	if answer.SDP != minimalSDP("answer") || answer.RelayInUse {
		t.Fatalf("answer = %+v", answer)
	}
}

func TestCloudAnswerRequiresTicketAndMachine(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := fixedClock(time.Date(2026, 5, 3, 5, 54, 0, 0, time.UTC))
	verifier := &fakeTicketVerifier{now: clock.Now(), tickets: map[string]cloud.Ticket{
		"ticket_allowed": {
			ID:         "ticket_allowed",
			MachineID:  "mach_1",
			TerminalID: "term_1",
			Path:       cloud.PathCloud,
			ExpiresAt:  clock.Now().Add(time.Minute),
		},
		"ticket_wrong": {
			ID:         "ticket_wrong",
			MachineID:  "mach_2",
			TerminalID: "term_1",
			Path:       cloud.PathCloud,
			ExpiresAt:  clock.Now().Add(time.Minute),
		},
	}}
	reg := registry.New(registry.Config{Clock: clock, Verifier: verifier})
	if _, err := reg.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	svc := cloud.NewService(cloud.Config{Registry: reg, Tickets: verifier, Clock: clock})
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
		MachineID: "mach_2",
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
	verifier := &fakeTicketVerifier{now: clock.Now(), tickets: map[string]cloud.Ticket{
		"ticket_a": {
			ID:         "ticket_a",
			MachineID:  "mach_1",
			TerminalID: "term_1",
			Path:       cloud.PathCloud,
			ExpiresAt:  clock.Now().Add(time.Minute),
		},
		"ticket_b": {
			ID:         "ticket_b",
			MachineID:  "mach_1",
			TerminalID: "term_1",
			Path:       cloud.PathCloud,
			ExpiresAt:  clock.Now().Add(time.Minute),
		},
	}}
	reg := registry.New(registry.Config{Clock: clock, Verifier: verifier})
	if _, err := reg.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	svc := cloud.NewService(cloud.Config{Registry: reg, Tickets: verifier, Clock: clock})
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

func TestCloudSignalingSurfacesRelayCapability(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := fixedClock(time.Date(2026, 5, 3, 6, 6, 0, 0, time.UTC))
	verifier := &fakeTicketVerifier{now: clock.Now(), tickets: map[string]cloud.Ticket{
		"ticket_future_relay": {
			ID:         "ticket_future_relay",
			MachineID:  "mach_1",
			TerminalID: "term_1",
			Path:       cloud.PathCloud,
			AllowRelay: true,
			ExpiresAt:  clock.Now().Add(time.Minute),
		},
	}}
	reg := registry.New(registry.Config{Clock: clock, Verifier: verifier})
	if _, err := reg.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	svc := cloud.NewService(cloud.Config{Registry: reg, Tickets: verifier, Clock: clock})
	offer, err := svc.SubmitOffer(ctx, cloud.SubmitOfferInput{
		TicketID:   "ticket_future_relay",
		MachineID:  "mach_1",
		TerminalID: "term_1",
		SDP:        minimalSDP("offer"),
	})
	if err != nil {
		t.Fatalf("submit offer: %v", err)
	}
	if offer.Path != cloud.PathCloud || !offer.AllowRelay || offer.RelayInUse {
		t.Fatalf("offer relay capability = %+v", offer)
	}
	polled, err := svc.PollAgentOffer(ctx, cloud.PollAgentOfferInput{
		AgentID:   "agent_1",
		MachineID: "mach_1",
		Timeout:   time.Second,
	})
	if err != nil {
		t.Fatalf("poll offer: %v", err)
	}
	if polled.Path != cloud.PathCloud || !polled.AllowRelay || polled.RelayInUse {
		t.Fatalf("polled relay capability = %+v", polled)
	}
}

func TestCloudOfferPolicyCacheIsTTLBounded(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := &mutableClock{value: time.Date(2026, 5, 3, 18, 58, 0, 0, time.UTC)}
	verifier := &fakeTicketVerifier{now: clock.Now(), tickets: map[string]cloud.Ticket{
		"ticket_a": {
			ID:         "ticket_a",
			MachineID:  "mach_1",
			TerminalID: "term_1",
			Path:       cloud.PathCloud,
			AllowRelay: true,
			ExpiresAt:  clock.Now().Add(time.Minute),
		},
		"ticket_b": {
			ID:         "ticket_b",
			MachineID:  "mach_1",
			TerminalID: "term_1",
			Path:       cloud.PathCloud,
			AllowRelay: true,
			ExpiresAt:  clock.Now().Add(time.Minute),
		},
	}}
	reg := registry.New(registry.Config{Clock: clock, Verifier: verifier})
	if _, err := reg.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	svc := cloud.NewService(cloud.Config{
		Registry:  reg,
		Tickets:   verifier,
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

func TestCloudSignalingChecksWithoutConsumingUntilAnswer(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := fixedClock(time.Date(2026, 5, 3, 6, 12, 0, 0, time.UTC))
	verifier := &fakeTicketVerifier{now: clock.Now(), tickets: map[string]cloud.Ticket{
		"ticket_allowed": {
			ID:         "ticket_allowed",
			MachineID:  "mach_1",
			TerminalID: "term_1",
			Path:       cloud.PathCloud,
			ExpiresAt:  clock.Now().Add(time.Minute),
		},
	}}
	reg := registry.New(registry.Config{Clock: clock, Verifier: verifier})
	if _, err := reg.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	svc := cloud.NewService(cloud.Config{Registry: reg, Tickets: verifier, Clock: clock})
	if _, err := svc.SubmitOffer(ctx, cloud.SubmitOfferInput{
		TicketID:   "ticket_allowed",
		MachineID:  "mach_1",
		TerminalID: "term_1",
		SDP:        minimalSDP("offer"),
	}); err != nil {
		t.Fatalf("submit offer: %v", err)
	}
	if got := verifier.consumeCalls; len(got) != 0 {
		t.Fatalf("submit offer consumed ticket before answer: %v", got)
	}
	if got := verifier.offerChecks; len(got) != 3 || got[0] != "mach_1/term_1/ticket_allowed" ||
		got[1] != "mach_1/term_1/ticket_allowed" || got[2] != "mach_1/term_1/ticket_allowed" {
		t.Fatalf("registry offer verifier checks = %v", got)
	}
	offer, err := svc.PollAgentOffer(ctx, cloud.PollAgentOfferInput{AgentID: "agent_1", MachineID: "mach_1", Timeout: time.Second})
	if err != nil {
		t.Fatalf("poll offer: %v", err)
	}
	if err := svc.SubmitAnswer(ctx, cloud.SubmitAnswerInput{
		AgentID:   "agent_1",
		MachineID: "mach_1",
		OfferID:   offer.ID,
		SDP:       minimalSDP("answer"),
	}); err != nil {
		t.Fatalf("submit answer: %v", err)
	}
	if _, err := svc.ConsumeOfferTicket(ctx, cloud.GetAnswerInput{
		OfferID:   offer.ID,
		TicketID:  "ticket_allowed",
		MachineID: "mach_1",
	}); err != nil {
		t.Fatalf("consume after answer: %v", err)
	}
	if got := verifier.consumeCalls; len(got) != 1 || got[0] != "mach_1/term_1/ticket_allowed" {
		t.Fatalf("consume calls = %v", got)
	}
	wantEvents := []string{
		"check:mach_1/term_1/ticket_allowed",
		"check:mach_1/term_1/ticket_allowed",
		"check:mach_1/term_1/ticket_allowed",
		"consume:mach_1/term_1/ticket_allowed",
	}
	if got := verifier.events; len(got) != len(wantEvents) {
		t.Fatalf("verifier events = %v, want %v", got, wantEvents)
	} else {
		for i := range wantEvents {
			if got[i] != wantEvents[i] {
				t.Fatalf("verifier events = %v, want %v", got, wantEvents)
			}
		}
	}
}

func TestCloudSignalingRejectsRuntimePayload(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := fixedClock(time.Date(2026, 5, 3, 5, 45, 0, 0, time.UTC))
	verifier := &fakeTicketVerifier{now: clock.Now(), tickets: map[string]cloud.Ticket{
		"ticket_allowed": {
			ID:         "ticket_allowed",
			MachineID:  "mach_1",
			TerminalID: "term_1",
			Path:       cloud.PathCloud,
			ExpiresAt:  clock.Now().Add(time.Minute),
		},
	}}
	reg := registry.New(registry.Config{Clock: clock, Verifier: verifier})
	if _, err := reg.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	svc := cloud.NewService(cloud.Config{Registry: reg, Tickets: verifier, Clock: clock})
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

func TestCloudSignalingDoesNotConsumeTicketWhenNoAgentOnline(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := fixedClock(time.Date(2026, 5, 3, 6, 11, 0, 0, time.UTC))
	verifier := &fakeTicketVerifier{now: clock.Now(), tickets: map[string]cloud.Ticket{
		"ticket_retry": {
			ID:         "ticket_retry",
			MachineID:  "mach_1",
			TerminalID: "term_1",
			Path:       cloud.PathCloud,
			ExpiresAt:  clock.Now().Add(time.Minute),
		},
	}}
	reg := registry.New(registry.Config{Clock: clock, Verifier: verifier})
	svc := cloud.NewService(cloud.Config{Registry: reg, Tickets: verifier, Clock: clock})
	if _, err := svc.SubmitOffer(ctx, cloud.SubmitOfferInput{
		TicketID:   "ticket_retry",
		MachineID:  "mach_1",
		TerminalID: "term_1",
		SDP:        minimalSDP("offer-no-agent"),
	}); !errors.Is(err, registry.ErrAgentNotFound) {
		t.Fatalf("offline offer err = %v", err)
	}
	if _, err := reg.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := svc.SubmitOffer(ctx, cloud.SubmitOfferInput{
		TicketID:   "ticket_retry",
		MachineID:  "mach_1",
		TerminalID: "term_1",
		SDP:        minimalSDP("offer-after-agent-online"),
	}); err != nil {
		t.Fatalf("valid offer after offline reject should reuse unconsumed ticket: %v", err)
	}
}

func TestCloudSignalingRejectsTicketReuse(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := fixedClock(time.Date(2026, 5, 3, 5, 51, 0, 0, time.UTC))
	verifier := &fakeTicketVerifier{now: clock.Now(), tickets: map[string]cloud.Ticket{
		"ticket_once": {
			ID:         "ticket_once",
			MachineID:  "mach_1",
			TerminalID: "term_1",
			Path:       cloud.PathCloud,
			ExpiresAt:  clock.Now().Add(time.Minute),
		},
	}}
	reg := registry.New(registry.Config{Clock: clock, Verifier: verifier})
	if _, err := reg.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	svc := cloud.NewService(cloud.Config{Registry: reg, Tickets: verifier, Clock: clock})
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

type fakeTicketVerifier struct {
	tickets          map[string]cloud.Ticket
	used             map[string]bool
	now              time.Time
	terminalOverride string
	consumeCalls     []string
	offerChecks      []string
	events           []string
}

func (f *fakeTicketVerifier) CheckConnectionTicket(_ context.Context, in cloud.VerifyTicketInput) (cloud.Ticket, error) {
	key := in.MachineID + "/" + in.TerminalID + "/" + in.TicketID
	f.offerChecks = append(f.offerChecks, key)
	f.events = append(f.events, "check:"+key)
	ticket, ok := f.tickets[in.TicketID]
	if !ok {
		return cloud.Ticket{}, cloud.ErrTicketExpired
	}
	if ticket.MachineID != in.MachineID {
		return cloud.Ticket{}, cloud.ErrWrongMachine
	}
	terminalID := ticket.TerminalID
	if f.terminalOverride != "" {
		terminalID = f.terminalOverride
	}
	if in.TerminalID != "" && terminalID != in.TerminalID {
		return cloud.Ticket{}, cloud.ErrWrongTerminal
	}
	if !ticket.ExpiresAt.After(f.now) {
		return cloud.Ticket{}, cloud.ErrTicketExpired
	}
	return ticket, nil
}

func (f *fakeTicketVerifier) ConsumeConnectionTicket(_ context.Context, in cloud.VerifyTicketInput) (cloud.Ticket, error) {
	key := in.MachineID + "/" + in.TerminalID + "/" + in.TicketID
	f.consumeCalls = append(f.consumeCalls, key)
	f.events = append(f.events, "consume:"+key)
	ticket, ok := f.tickets[in.TicketID]
	if !ok {
		return cloud.Ticket{}, cloud.ErrTicketExpired
	}
	if ticket.MachineID != in.MachineID {
		return cloud.Ticket{}, cloud.ErrWrongMachine
	}
	terminalID := ticket.TerminalID
	if f.terminalOverride != "" {
		terminalID = f.terminalOverride
	}
	if in.TerminalID != "" && terminalID != in.TerminalID {
		return cloud.Ticket{}, cloud.ErrWrongTerminal
	}
	if !ticket.ExpiresAt.After(f.now) {
		return cloud.Ticket{}, cloud.ErrTicketExpired
	}
	if f.used == nil {
		f.used = make(map[string]bool)
	}
	if f.used[in.TicketID] {
		return cloud.Ticket{}, cloud.ErrTicketUsed
	}
	f.used[in.TicketID] = true
	return ticket, nil
}

func (f *fakeTicketVerifier) VerifyAgentRegistration(context.Context, registry.AgentRegistration) error {
	return nil
}

func (f *fakeTicketVerifier) VerifyOfferTicket(ctx context.Context, in registry.OfferTicket) error {
	_, err := f.CheckConnectionTicket(ctx, cloud.VerifyTicketInput{
		TicketID:   in.TicketID,
		MachineID:  in.MachineID,
		TerminalID: in.TerminalID,
	})
	return err
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
