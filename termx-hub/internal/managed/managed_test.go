package managed_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-hub/internal/managed"
	"github.com/lozzow/termx/termx-hub/internal/registry"
)

func TestManagedSignalingTicketPolicyAndAnswerFlow(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := fixedClock(time.Date(2026, 5, 3, 5, 44, 0, 0, time.UTC))
	verifier := &fakeTicketVerifier{now: clock.Now(), tickets: map[string]managed.Ticket{
		"ticket_allowed": {
			ID:         "ticket_allowed",
			MachineID:  "mach_1",
			TerminalID: "term_1",
			Path:       managed.PathManaged,
			AllowRelay: false,
			ExpiresAt:  clock.Now().Add(time.Minute),
		},
		"ticket_expired": {
			ID:         "ticket_expired",
			MachineID:  "mach_1",
			TerminalID: "term_1",
			Path:       managed.PathManaged,
			AllowRelay: false,
			ExpiresAt:  clock.Now().Add(-time.Second),
		},
	}}
	reg := registry.New(registry.Config{Clock: clock, Verifier: verifier})
	if _, err := reg.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	svc := managed.NewService(managed.Config{Registry: reg, Tickets: verifier, Clock: clock})
	if _, err := svc.SubmitOffer(ctx, managed.SubmitOfferInput{
		TicketID:   "ticket_expired",
		MachineID:  "mach_1",
		TerminalID: "term_1",
		SDP:        minimalSDP("offer-expired"),
	}); !errors.Is(err, managed.ErrTicketExpired) {
		t.Fatalf("expired ticket err = %v", err)
	}
	if _, err := svc.SubmitOffer(ctx, managed.SubmitOfferInput{
		TicketID:   "ticket_allowed",
		MachineID:  "wrong_machine",
		TerminalID: "term_1",
		SDP:        minimalSDP("offer-wrong"),
	}); !errors.Is(err, managed.ErrWrongMachine) {
		t.Fatalf("wrong machine err = %v", err)
	}
	verifier.terminalOverride = "wrong_terminal"
	if _, err := svc.SubmitOffer(ctx, managed.SubmitOfferInput{
		TicketID:   "ticket_allowed",
		MachineID:  "mach_1",
		TerminalID: "term_1",
		SDP:        minimalSDP("offer-terminal-mismatch"),
	}); !errors.Is(err, managed.ErrWrongTerminal) {
		t.Fatalf("wrong terminal err = %v", err)
	}
	verifier.terminalOverride = ""
	offer, err := svc.SubmitOffer(ctx, managed.SubmitOfferInput{
		TicketID:   "ticket_allowed",
		MachineID:  "mach_1",
		TerminalID: "term_1",
		SDP:        minimalSDP("offer"),
	})
	if err != nil {
		t.Fatalf("submit offer: %v", err)
	}
	if offer.Path != managed.PathManaged || offer.RelayInUse || offer.AllowRelay {
		t.Fatalf("offer path/relay = %+v", offer)
	}
	polled, err := svc.PollAgentOffer(ctx, managed.PollAgentOfferInput{
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
	if err := svc.SubmitAnswer(ctx, managed.SubmitAnswerInput{
		AgentID:   "agent_1",
		MachineID: "mach_1",
		OfferID:   offer.ID,
		SDP:       minimalSDP("answer"),
	}); err != nil {
		t.Fatalf("submit answer: %v", err)
	}
	answer, err := svc.GetAnswer(ctx, managed.GetAnswerInput{
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

func TestManagedAnswerRequiresTicketAndMachine(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := fixedClock(time.Date(2026, 5, 3, 5, 54, 0, 0, time.UTC))
	verifier := &fakeTicketVerifier{now: clock.Now(), tickets: map[string]managed.Ticket{
		"ticket_allowed": {
			ID:         "ticket_allowed",
			MachineID:  "mach_1",
			TerminalID: "term_1",
			Path:       managed.PathManaged,
			ExpiresAt:  clock.Now().Add(time.Minute),
		},
		"ticket_wrong": {
			ID:         "ticket_wrong",
			MachineID:  "mach_2",
			TerminalID: "term_1",
			Path:       managed.PathManaged,
			ExpiresAt:  clock.Now().Add(time.Minute),
		},
	}}
	reg := registry.New(registry.Config{Clock: clock, Verifier: verifier})
	if _, err := reg.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	svc := managed.NewService(managed.Config{Registry: reg, Tickets: verifier, Clock: clock})
	offer, err := svc.SubmitOffer(ctx, managed.SubmitOfferInput{
		TicketID:   "ticket_allowed",
		MachineID:  "mach_1",
		TerminalID: "term_1",
		SDP:        minimalSDP("offer"),
	})
	if err != nil {
		t.Fatalf("submit offer: %v", err)
	}
	if _, err := svc.PollAgentOffer(ctx, managed.PollAgentOfferInput{AgentID: "agent_1", MachineID: "mach_1", Timeout: time.Second}); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if err := svc.SubmitAnswer(ctx, managed.SubmitAnswerInput{
		AgentID:   "agent_1",
		MachineID: "mach_1",
		OfferID:   offer.ID,
		SDP:       minimalSDP("answer"),
	}); err != nil {
		t.Fatalf("answer: %v", err)
	}
	if _, err := svc.GetAnswer(ctx, managed.GetAnswerInput{
		OfferID:   offer.ID,
		TicketID:  "ticket_wrong",
		MachineID: "mach_2",
	}); !errors.Is(err, managed.ErrWrongMachine) {
		t.Fatalf("wrong ticket answer err = %v", err)
	}
	if _, err := svc.GetAnswer(ctx, managed.GetAnswerInput{
		OfferID:   offer.ID,
		TicketID:  "ticket_allowed",
		MachineID: "wrong_machine",
	}); !errors.Is(err, managed.ErrWrongMachine) {
		t.Fatalf("wrong machine answer err = %v", err)
	}
	if _, err := svc.GetAnswer(ctx, managed.GetAnswerInput{OfferID: offer.ID}); !errors.Is(err, managed.ErrWrongMachine) {
		t.Fatalf("missing ticket answer err = %v", err)
	}
	if _, err := svc.GetAnswer(ctx, managed.GetAnswerInput{
		OfferID:   offer.ID,
		TicketID:  "ticket_allowed",
		MachineID: "mach_1",
	}); err != nil {
		t.Fatalf("authorized answer: %v", err)
	}
}

func TestManagedSignalingDoesNotSurfaceRelayBeforeTurnPolicy(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := fixedClock(time.Date(2026, 5, 3, 6, 6, 0, 0, time.UTC))
	verifier := &fakeTicketVerifier{now: clock.Now(), tickets: map[string]managed.Ticket{
		"ticket_future_relay": {
			ID:         "ticket_future_relay",
			MachineID:  "mach_1",
			TerminalID: "term_1",
			Path:       managed.PathManaged,
			AllowRelay: true,
			ExpiresAt:  clock.Now().Add(time.Minute),
		},
	}}
	reg := registry.New(registry.Config{Clock: clock, Verifier: verifier})
	if _, err := reg.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	svc := managed.NewService(managed.Config{Registry: reg, Tickets: verifier, Clock: clock})
	offer, err := svc.SubmitOffer(ctx, managed.SubmitOfferInput{
		TicketID:   "ticket_future_relay",
		MachineID:  "mach_1",
		TerminalID: "term_1",
		SDP:        minimalSDP("offer"),
	})
	if err != nil {
		t.Fatalf("submit offer: %v", err)
	}
	if offer.AllowRelay || offer.RelayInUse {
		t.Fatalf("slice 6 offer surfaced relay before TURN policy: %+v", offer)
	}
}

func TestManagedSignalingUsesRegistryVerifierWithoutDoubleConsuming(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := fixedClock(time.Date(2026, 5, 3, 6, 12, 0, 0, time.UTC))
	verifier := &fakeTicketVerifier{now: clock.Now(), tickets: map[string]managed.Ticket{
		"ticket_allowed": {
			ID:         "ticket_allowed",
			MachineID:  "mach_1",
			TerminalID: "term_1",
			Path:       managed.PathManaged,
			ExpiresAt:  clock.Now().Add(time.Minute),
		},
	}}
	reg := registry.New(registry.Config{Clock: clock, Verifier: verifier})
	if _, err := reg.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	svc := managed.NewService(managed.Config{Registry: reg, Tickets: verifier, Clock: clock})
	if _, err := svc.SubmitOffer(ctx, managed.SubmitOfferInput{
		TicketID:   "ticket_allowed",
		MachineID:  "mach_1",
		TerminalID: "term_1",
		SDP:        minimalSDP("offer"),
	}); err != nil {
		t.Fatalf("submit offer: %v", err)
	}
	if got := verifier.consumeCalls; len(got) != 1 || got[0] != "mach_1/term_1/ticket_allowed" {
		t.Fatalf("consume calls = %v", got)
	}
	if got := verifier.offerChecks; len(got) != 2 || got[0] != "mach_1/term_1/ticket_allowed" || got[1] != "mach_1/term_1/ticket_allowed" {
		t.Fatalf("registry offer verifier checks = %v", got)
	}
	wantEvents := []string{
		"check:mach_1/term_1/ticket_allowed",
		"consume:mach_1/term_1/ticket_allowed",
		"check:mach_1/term_1/ticket_allowed",
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

func TestManagedSignalingRejectsRuntimePayload(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := fixedClock(time.Date(2026, 5, 3, 5, 45, 0, 0, time.UTC))
	verifier := &fakeTicketVerifier{now: clock.Now(), tickets: map[string]managed.Ticket{
		"ticket_allowed": {
			ID:         "ticket_allowed",
			MachineID:  "mach_1",
			TerminalID: "term_1",
			Path:       managed.PathManaged,
			ExpiresAt:  clock.Now().Add(time.Minute),
		},
	}}
	reg := registry.New(registry.Config{Clock: clock, Verifier: verifier})
	if _, err := reg.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	svc := managed.NewService(managed.Config{Registry: reg, Tickets: verifier, Clock: clock})
	if _, err := svc.SubmitOffer(ctx, managed.SubmitOfferInput{
		TicketID:   "ticket_allowed",
		MachineID:  "mach_1",
		TerminalID: "term_1",
		SDP:        "v=0\r\nterminal_data=must-not-forward",
	}); !errors.Is(err, registry.ErrRuntimePayload) {
		t.Fatalf("runtime payload err = %v", err)
	}
	if _, err := svc.SubmitOffer(ctx, managed.SubmitOfferInput{
		TicketID:   "ticket_allowed",
		MachineID:  "mach_1",
		TerminalID: "term_1",
		SDP:        minimalSDP("offer-after-runtime-reject"),
	}); err != nil {
		t.Fatalf("valid offer after runtime reject should reuse unconsumed ticket: %v", err)
	}
}

func TestManagedSignalingDoesNotConsumeTicketWhenNoAgentOnline(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := fixedClock(time.Date(2026, 5, 3, 6, 11, 0, 0, time.UTC))
	verifier := &fakeTicketVerifier{now: clock.Now(), tickets: map[string]managed.Ticket{
		"ticket_retry": {
			ID:         "ticket_retry",
			MachineID:  "mach_1",
			TerminalID: "term_1",
			Path:       managed.PathManaged,
			ExpiresAt:  clock.Now().Add(time.Minute),
		},
	}}
	reg := registry.New(registry.Config{Clock: clock, Verifier: verifier})
	svc := managed.NewService(managed.Config{Registry: reg, Tickets: verifier, Clock: clock})
	if _, err := svc.SubmitOffer(ctx, managed.SubmitOfferInput{
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
	if _, err := svc.SubmitOffer(ctx, managed.SubmitOfferInput{
		TicketID:   "ticket_retry",
		MachineID:  "mach_1",
		TerminalID: "term_1",
		SDP:        minimalSDP("offer-after-agent-online"),
	}); err != nil {
		t.Fatalf("valid offer after offline reject should reuse unconsumed ticket: %v", err)
	}
}

func TestManagedSignalingRejectsTicketReuse(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := fixedClock(time.Date(2026, 5, 3, 5, 51, 0, 0, time.UTC))
	verifier := &fakeTicketVerifier{now: clock.Now(), tickets: map[string]managed.Ticket{
		"ticket_once": {
			ID:         "ticket_once",
			MachineID:  "mach_1",
			TerminalID: "term_1",
			Path:       managed.PathManaged,
			ExpiresAt:  clock.Now().Add(time.Minute),
		},
	}}
	reg := registry.New(registry.Config{Clock: clock, Verifier: verifier})
	if _, err := reg.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	svc := managed.NewService(managed.Config{Registry: reg, Tickets: verifier, Clock: clock})
	if _, err := svc.SubmitOffer(ctx, managed.SubmitOfferInput{
		TicketID:   "ticket_once",
		MachineID:  "mach_1",
		TerminalID: "term_1",
		SDP:        minimalSDP("offer-1"),
	}); err != nil {
		t.Fatalf("first offer: %v", err)
	}
	if _, err := svc.SubmitOffer(ctx, managed.SubmitOfferInput{
		TicketID:   "ticket_once",
		MachineID:  "mach_1",
		TerminalID: "term_1",
		SDP:        minimalSDP("offer-2"),
	}); !errors.Is(err, managed.ErrTicketUsed) {
		t.Fatalf("second offer err = %v", err)
	}
}

type fakeTicketVerifier struct {
	tickets          map[string]managed.Ticket
	used             map[string]bool
	now              time.Time
	terminalOverride string
	consumeCalls     []string
	offerChecks      []string
	events           []string
}

func (f *fakeTicketVerifier) VerifyManagedTicket(_ context.Context, in managed.VerifyTicketInput) (managed.Ticket, error) {
	key := in.MachineID + "/" + in.TerminalID + "/" + in.TicketID
	f.consumeCalls = append(f.consumeCalls, key)
	f.events = append(f.events, "consume:"+key)
	ticket, ok := f.tickets[in.TicketID]
	if !ok {
		return managed.Ticket{}, managed.ErrTicketExpired
	}
	if ticket.MachineID != in.MachineID {
		return managed.Ticket{}, managed.ErrWrongMachine
	}
	terminalID := ticket.TerminalID
	if f.terminalOverride != "" {
		terminalID = f.terminalOverride
	}
	if in.TerminalID != "" && terminalID != in.TerminalID {
		return managed.Ticket{}, managed.ErrWrongTerminal
	}
	if !ticket.ExpiresAt.After(f.now) {
		return managed.Ticket{}, managed.ErrTicketExpired
	}
	if f.used == nil {
		f.used = make(map[string]bool)
	}
	if f.used[in.TicketID] {
		return managed.Ticket{}, managed.ErrTicketUsed
	}
	f.used[in.TicketID] = true
	return ticket, nil
}

func (f *fakeTicketVerifier) VerifyAgentRegistration(context.Context, registry.AgentRegistration) error {
	return nil
}

func (f *fakeTicketVerifier) VerifyOfferTicket(ctx context.Context, in registry.OfferTicket) error {
	key := in.MachineID + "/" + in.TerminalID + "/" + in.TicketID
	f.offerChecks = append(f.offerChecks, key)
	f.events = append(f.events, "check:"+key)
	ticket, ok := f.tickets[in.TicketID]
	if !ok {
		return managed.ErrTicketExpired
	}
	if ticket.MachineID != in.MachineID {
		return managed.ErrWrongMachine
	}
	terminalID := ticket.TerminalID
	if f.terminalOverride != "" {
		terminalID = f.terminalOverride
	}
	if in.TerminalID != "" && terminalID != in.TerminalID {
		return managed.ErrWrongTerminal
	}
	if !ticket.ExpiresAt.After(f.now) {
		return managed.ErrTicketExpired
	}
	return nil
}

type fixedClock time.Time

func (c fixedClock) Now() time.Time {
	return time.Time(c)
}

func minimalSDP(sessionID string) string {
	return "v=0\r\no=- " + sessionID + " 1 IN IP4 127.0.0.1\r\ns=-\r\nt=0 0\r\nm=application 9 UDP/DTLS/SCTP webrtc-datachannel"
}
