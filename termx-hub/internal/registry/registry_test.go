package registry_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-hub/internal/registry"
)

func TestAgentRegisterHeartbeatExpiryAndCleanup(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := &mutableClock{value: time.Date(2026, 5, 3, 9, 41, 0, 0, time.UTC)}
	store := registry.New(registry.Config{Clock: clock, AgentTTL: time.Minute, Verifier: allowAuthorityVerifier{}})

	agent, err := store.Register(ctx, registry.RegisterInput{
		MachineID: "mach_1",
		AgentID:   "agent_1",
		Terminals: []registry.Terminal{{ID: "term_1", State: "running"}},
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if agent.Status != registry.AgentOnline {
		t.Fatalf("status = %q", agent.Status)
	}
	if agent.Path != registry.PathManaged {
		t.Fatalf("path = %q", agent.Path)
	}
	if agent.RelayInUse {
		t.Fatal("registry marked relay in use")
	}

	clock.value = clock.value.Add(30 * time.Second)
	if err := store.Heartbeat(ctx, registry.HeartbeatInput{AgentID: agent.ID, MachineID: "mach_1"}); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	if got, ok := store.GetAgent("agent_1"); !ok || !got.ExpiresAt.Equal(clock.value.Add(time.Minute)) {
		t.Fatalf("heartbeat did not refresh expiry: agent=%+v ok=%v", got, ok)
	}

	clock.value = clock.value.Add(time.Minute + time.Second)
	removed := store.CleanupExpired(ctx)
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if _, ok := store.GetAgent("agent_1"); ok {
		t.Fatal("expired agent remained online")
	}
}

func TestHeartbeatRejectsExpiredAgentBeforeCleanup(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := &mutableClock{value: time.Date(2026, 5, 3, 10, 17, 0, 0, time.UTC)}
	store := registry.New(registry.Config{Clock: clock, AgentTTL: time.Minute, Verifier: allowAuthorityVerifier{}})
	if _, err := store.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	clock.value = clock.value.Add(time.Minute + time.Second)
	if err := store.Heartbeat(ctx, registry.HeartbeatInput{
		AgentID:   "agent_1",
		MachineID: "mach_1",
	}); !errors.Is(err, registry.ErrAgentNotFound) {
		t.Fatalf("expired heartbeat err = %v", err)
	}
	if _, ok := store.GetAgent("agent_1"); ok {
		t.Fatal("expired heartbeat resurrected agent")
	}
}

func TestPollTimeoutOfferDeliveryAndAnswerCorrelation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := &mutableClock{value: time.Date(2026, 5, 3, 9, 42, 0, 0, time.UTC)}
	store := registry.New(registry.Config{Clock: clock, AgentTTL: time.Minute, Verifier: allowAuthorityVerifier{}})
	if _, err := store.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register: %v", err)
	}

	if _, err := store.Poll(ctx, registry.PollInput{
		AgentID:   "agent_1",
		MachineID: "mach_1",
		Timeout:   time.Millisecond,
	}); !errors.Is(err, registry.ErrPollTimeout) {
		t.Fatalf("poll timeout err = %v", err)
	}

	offer, err := store.SubmitOffer(ctx, registry.OfferInput{
		MachineID:  "mach_1",
		TerminalID: "term_1",
		TicketID:   "ticket_1",
		SDP:        minimalSDP("offer"),
	})
	if err != nil {
		t.Fatalf("submit offer: %v", err)
	}
	if offer.Path != registry.PathManaged || offer.RelayInUse {
		t.Fatalf("offer path/relay = %q/%v", offer.Path, offer.RelayInUse)
	}
	if offer.ContainsRuntimePayload() {
		t.Fatalf("offer stored runtime payload: %+v", offer)
	}

	polled, err := store.Poll(ctx, registry.PollInput{AgentID: "agent_1", MachineID: "mach_1", Timeout: time.Second})
	if err != nil {
		t.Fatalf("poll offer: %v", err)
	}
	if polled.ID != offer.ID || polled.SDP != minimalSDP("offer") || polled.TerminalID != "term_1" {
		t.Fatalf("polled offer = %+v", polled)
	}

	answer, err := store.SubmitAnswer(ctx, registry.AnswerInput{
		AgentID:   "agent_1",
		MachineID: "mach_1",
		OfferID:   offer.ID,
		SDP:       minimalSDP("answer"),
	})
	if err != nil {
		t.Fatalf("submit answer: %v", err)
	}
	if answer.OfferID != offer.ID || answer.SDP != minimalSDP("answer") {
		t.Fatalf("answer = %+v", answer)
	}
	loaded, err := store.GetAnswer(ctx, offer.ID)
	if err != nil {
		t.Fatalf("get answer: %v", err)
	}
	if loaded.ID != answer.ID || loaded.OfferID != offer.ID {
		t.Fatalf("loaded answer = %+v", loaded)
	}
	if _, err := store.SubmitAnswer(ctx, registry.AnswerInput{
		AgentID:   "agent_1",
		MachineID: "wrong_machine",
		OfferID:   offer.ID,
		SDP:       minimalSDP("answer"),
	}); !errors.Is(err, registry.ErrUnauthorizedAgent) {
		t.Fatalf("wrong machine answer err = %v", err)
	}
}

func TestAnswerRequiresPolledOfferAndAssignedAgent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := &mutableClock{value: time.Date(2026, 5, 3, 10, 4, 0, 0, time.UTC)}
	store := registry.New(registry.Config{Clock: clock, AgentTTL: time.Minute, Verifier: allowAuthorityVerifier{}})
	if _, err := store.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register agent_1: %v", err)
	}
	if _, err := store.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_2"}); err != nil {
		t.Fatalf("register agent_2: %v", err)
	}
	offer, err := store.SubmitOffer(ctx, registry.OfferInput{
		MachineID:  "mach_1",
		TerminalID: "term_1",
		TicketID:   "ticket_1",
		SDP:        minimalSDP("offer"),
	})
	if err != nil {
		t.Fatalf("submit offer: %v", err)
	}
	if _, err := store.SubmitAnswer(ctx, registry.AnswerInput{
		AgentID:   "agent_1",
		MachineID: "mach_1",
		OfferID:   offer.ID,
		SDP:       minimalSDP("answer"),
	}); !errors.Is(err, registry.ErrOfferNotAssigned) {
		t.Fatalf("unpolled answer err = %v", err)
	}
	if _, err := store.Poll(ctx, registry.PollInput{AgentID: "agent_1", MachineID: "mach_1", Timeout: time.Second}); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if _, err := store.SubmitAnswer(ctx, registry.AnswerInput{
		AgentID:   "agent_2",
		MachineID: "mach_1",
		OfferID:   offer.ID,
		SDP:       minimalSDP("answer"),
	}); !errors.Is(err, registry.ErrOfferNotAssigned) {
		t.Fatalf("wrong assigned agent answer err = %v", err)
	}
	if _, err := store.SubmitAnswer(ctx, registry.AnswerInput{
		AgentID:   "agent_1",
		MachineID: "mach_1",
		OfferID:   offer.ID,
		SDP:       minimalSDP("answer"),
	}); err != nil {
		t.Fatalf("assigned answer: %v", err)
	}
}

func TestOfferRejectsRuntimePayloadMarkers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := registry.New(registry.Config{
		Clock:    fixedClock(time.Date(2026, 5, 3, 10, 5, 0, 0, time.UTC)),
		AgentTTL: time.Minute,
		Verifier: allowAuthorityVerifier{},
	})
	if _, err := store.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	for _, sdp := range []string{
		"v=0\r\nterminal_data=must-not-forward",
		"v=0\r\nfile_data=must-not-forward",
		"v=0\r\napi_data=must-not-forward",
		"v=0\r\nevents_data=must-not-forward",
		"v=0\r\na=dc:terminal:term_1",
		"v=0\r\na=dc:file:transfer_1",
	} {
		if _, err := store.SubmitOffer(ctx, registry.OfferInput{
			MachineID:  "mach_1",
			TerminalID: "term_1",
			TicketID:   "ticket_1",
			SDP:        sdp,
		}); !errors.Is(err, registry.ErrRuntimePayload) {
			t.Fatalf("runtime payload err = %v for %q", err, sdp)
		}
	}
}

func TestSignalingPayloadSizeLimit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	validSDP := minimalSDP("offer")
	oversizedSDP := validSDP + "\r\na=" + string(make([]byte, 32))
	store := registry.New(registry.Config{
		Clock:       fixedClock(time.Date(2026, 5, 3, 10, 6, 0, 0, time.UTC)),
		AgentTTL:    time.Minute,
		MaxSDPBytes: len(validSDP) + 8,
		Verifier:    allowAuthorityVerifier{},
	})
	if _, err := store.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := store.SubmitOffer(ctx, registry.OfferInput{
		MachineID:  "mach_1",
		TerminalID: "term_1",
		TicketID:   "ticket_1",
		SDP:        oversizedSDP,
	}); !errors.Is(err, registry.ErrPayloadTooLarge) {
		t.Fatalf("large offer err = %v", err)
	}
	offer, err := store.SubmitOffer(ctx, registry.OfferInput{
		MachineID:  "mach_1",
		TerminalID: "term_1",
		TicketID:   "ticket_1",
		SDP:        minimalSDP("offer"),
	})
	if err != nil {
		t.Fatalf("submit offer: %v", err)
	}
	if _, err := store.Poll(ctx, registry.PollInput{AgentID: "agent_1", MachineID: "mach_1", Timeout: time.Second}); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if _, err := store.SubmitAnswer(ctx, registry.AnswerInput{
		AgentID:   "agent_1",
		MachineID: "mach_1",
		OfferID:   offer.ID,
		SDP:       oversizedSDP,
	}); !errors.Is(err, registry.ErrPayloadTooLarge) {
		t.Fatalf("large answer err = %v", err)
	}
	if _, err := store.SubmitAnswer(ctx, registry.AnswerInput{
		AgentID:   "agent_1",
		MachineID: "mach_1",
		OfferID:   offer.ID,
		SDP:       "api_data=x",
	}); !errors.Is(err, registry.ErrRuntimePayload) {
		t.Fatalf("runtime answer err = %v", err)
	}
}

func TestSignalingPayloadRequiresBasicSDPShape(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := registry.New(registry.Config{
		Clock:    fixedClock(time.Date(2026, 5, 3, 10, 21, 0, 0, time.UTC)),
		AgentTTL: time.Minute,
		Verifier: allowAuthorityVerifier{},
	})
	if _, err := store.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := store.SubmitOffer(ctx, registry.OfferInput{
		MachineID:  "mach_1",
		TerminalID: "term_1",
		TicketID:   "ticket_1",
		SDP:        "not an sdp payload",
	}); !errors.Is(err, registry.ErrInvalidSDP) {
		t.Fatalf("invalid offer sdp err = %v", err)
	}
	offer, err := store.SubmitOffer(ctx, registry.OfferInput{
		MachineID:  "mach_1",
		TerminalID: "term_1",
		TicketID:   "ticket_1",
		SDP:        minimalSDP("offer"),
	})
	if err != nil {
		t.Fatalf("valid offer sdp: %v", err)
	}
	if _, err := store.Poll(ctx, registry.PollInput{AgentID: "agent_1", MachineID: "mach_1", Timeout: time.Second}); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if _, err := store.SubmitAnswer(ctx, registry.AnswerInput{
		AgentID:   "agent_1",
		MachineID: "mach_1",
		OfferID:   offer.ID,
		SDP:       "not an sdp payload",
	}); !errors.Is(err, registry.ErrInvalidSDP) {
		t.Fatalf("invalid answer sdp err = %v", err)
	}
	if _, err := store.SubmitAnswer(ctx, registry.AnswerInput{
		AgentID:   "agent_1",
		MachineID: "mach_1",
		OfferID:   offer.ID,
		SDP:       minimalSDP("answer"),
	}); err != nil {
		t.Fatalf("valid answer sdp: %v", err)
	}
}

func TestAgentRegistrationRejectsRebindAndVerifierDenial(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	verifier := &fakeAuthorityVerifier{
		register: map[string]error{
			"mach_1/agent_1": nil,
			"mach_2/agent_2": registry.ErrUnauthorizedAgent,
		},
	}
	store := registry.New(registry.Config{
		Clock:    fixedClock(time.Date(2026, 5, 3, 10, 7, 0, 0, time.UTC)),
		AgentTTL: time.Minute,
		Verifier: verifier,
	})
	if _, err := store.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register allowed agent: %v", err)
	}
	if _, err := store.Register(ctx, registry.RegisterInput{MachineID: "mach_2", AgentID: "agent_1"}); !errors.Is(err, registry.ErrAgentRebound) {
		t.Fatalf("agent rebind err = %v", err)
	}
	if _, err := store.Register(ctx, registry.RegisterInput{MachineID: "mach_2", AgentID: "agent_2"}); !errors.Is(err, registry.ErrUnauthorizedAgent) {
		t.Fatalf("verifier denied register err = %v", err)
	}
	if got := verifier.registerCalls; !slices.Equal(got, []string{"mach_1/agent_1", "mach_2/agent_2"}) {
		t.Fatalf("register verifier calls = %v", got)
	}
}

func TestOfferRequiresTicketVerifier(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	verifier := &fakeAuthorityVerifier{
		register: map[string]error{"mach_1/agent_1": nil},
		offer: map[string]error{
			"mach_1/term_1/ticket_allowed": nil,
			"mach_1/term_1/ticket_denied":  registry.ErrUnauthorizedTicket,
		},
	}
	store := registry.New(registry.Config{
		Clock:    fixedClock(time.Date(2026, 5, 3, 10, 8, 0, 0, time.UTC)),
		AgentTTL: time.Minute,
		Verifier: verifier,
	})
	if _, err := store.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := store.SubmitOffer(ctx, registry.OfferInput{
		MachineID:  "mach_1",
		TerminalID: "term_1",
		TicketID:   "ticket_denied",
		SDP:        minimalSDP("offer"),
	}); !errors.Is(err, registry.ErrUnauthorizedTicket) {
		t.Fatalf("denied offer err = %v", err)
	}
	offer, err := store.SubmitOffer(ctx, registry.OfferInput{
		MachineID:  "mach_1",
		TerminalID: "term_1",
		TicketID:   "ticket_allowed",
		SDP:        minimalSDP("offer"),
	})
	if err != nil {
		t.Fatalf("allowed offer: %v", err)
	}
	if offer.TicketID != "ticket_allowed" {
		t.Fatalf("offer ticket = %q", offer.TicketID)
	}
	if got := verifier.offerCalls; !slices.Equal(got, []string{"mach_1/term_1/ticket_denied", "mach_1/term_1/ticket_allowed"}) {
		t.Fatalf("offer verifier calls = %v", got)
	}
}

func TestSubmitOfferPreservesSignedSDPBytes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := registry.New(registry.Config{
		Clock:    fixedClock(time.Date(2026, 5, 3, 10, 8, 30, 0, time.UTC)),
		AgentTTL: time.Minute,
		Verifier: allowAuthorityVerifier{},
	})
	if _, err := store.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	signedSDP := minimalSDP("offer") + "\r\n"
	if _, err := store.SubmitOffer(ctx, registry.OfferInput{
		MachineID:  "mach_1",
		TerminalID: "term_1",
		TicketID:   "ticket_1",
		SDP:        signedSDP,
	}); err != nil {
		t.Fatalf("submit offer: %v", err)
	}
	offer, err := store.Poll(ctx, registry.PollInput{
		AgentID:   "agent_1",
		MachineID: "mach_1",
		Timeout:   time.Millisecond,
	})
	if err != nil {
		t.Fatalf("poll offer: %v", err)
	}
	if offer.SDP != signedSDP {
		t.Fatalf("polled SDP was mutated: got %q want %q", offer.SDP, signedSDP)
	}
}

func TestSubmitAnswerPreservesSDPBytes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := registry.New(registry.Config{
		Clock:    fixedClock(time.Date(2026, 5, 3, 10, 8, 45, 0, time.UTC)),
		AgentTTL: time.Minute,
		Verifier: allowAuthorityVerifier{},
	})
	if _, err := store.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	offer, err := store.SubmitOffer(ctx, registry.OfferInput{
		MachineID:  "mach_1",
		TerminalID: "term_1",
		TicketID:   "ticket_1",
		SDP:        minimalSDP("offer") + "\r\n",
	})
	if err != nil {
		t.Fatalf("submit offer: %v", err)
	}
	if _, err := store.Poll(ctx, registry.PollInput{
		AgentID:   "agent_1",
		MachineID: "mach_1",
		Timeout:   time.Millisecond,
	}); err != nil {
		t.Fatalf("poll offer: %v", err)
	}
	answerSDP := minimalSDP("answer") + "\r\n"
	if _, err := store.SubmitAnswer(ctx, registry.AnswerInput{
		AgentID:   "agent_1",
		MachineID: "mach_1",
		OfferID:   offer.ID,
		SDP:       answerSDP,
	}); err != nil {
		t.Fatalf("submit answer: %v", err)
	}
	answer, err := store.GetAnswer(ctx, offer.ID)
	if err != nil {
		t.Fatalf("get answer: %v", err)
	}
	if answer.SDP != answerSDP {
		t.Fatalf("answer SDP was mutated: got %q want %q", answer.SDP, answerSDP)
	}
}

func TestCleanupKeepsQueuedOfferWhenAnotherAgentOnline(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := &mutableClock{value: time.Date(2026, 5, 3, 10, 9, 0, 0, time.UTC)}
	store := registry.New(registry.Config{
		Clock:    clock,
		AgentTTL: time.Minute,
		Verifier: allowAuthorityVerifier{},
	})
	if _, err := store.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_old"}); err != nil {
		t.Fatalf("register old: %v", err)
	}
	clock.value = clock.value.Add(30 * time.Second)
	if _, err := store.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_new"}); err != nil {
		t.Fatalf("register new: %v", err)
	}
	offer, err := store.SubmitOffer(ctx, registry.OfferInput{
		MachineID:  "mach_1",
		TerminalID: "term_1",
		TicketID:   "ticket_1",
		SDP:        minimalSDP("offer"),
	})
	if err != nil {
		t.Fatalf("submit offer: %v", err)
	}
	clock.value = clock.value.Add(31 * time.Second)
	if removed := store.CleanupExpired(ctx); removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	polled, err := store.Poll(ctx, registry.PollInput{AgentID: "agent_new", MachineID: "mach_1", Timeout: time.Second})
	if err != nil {
		t.Fatalf("poll preserved offer: %v", err)
	}
	if polled.ID != offer.ID {
		t.Fatalf("polled offer = %q, want %q", polled.ID, offer.ID)
	}
}

func TestCleanupExpiresStaleOffersAndAnswers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := &mutableClock{value: time.Date(2026, 5, 3, 10, 10, 0, 0, time.UTC)}
	store := registry.New(registry.Config{
		Clock:        clock,
		AgentTTL:     time.Hour,
		SignalingTTL: time.Second,
		Verifier:     allowAuthorityVerifier{},
	})
	if _, err := store.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	offer, err := store.SubmitOffer(ctx, registry.OfferInput{
		MachineID:  "mach_1",
		TerminalID: "term_1",
		TicketID:   "ticket_1",
		SDP:        minimalSDP("offer"),
	})
	if err != nil {
		t.Fatalf("submit offer: %v", err)
	}
	clock.value = clock.value.Add(2 * time.Second)
	if removed := store.CleanupExpired(ctx); removed != 1 {
		t.Fatalf("removed stale offer = %d, want 1", removed)
	}
	if _, err := store.Poll(ctx, registry.PollInput{AgentID: "agent_1", MachineID: "mach_1", Timeout: time.Millisecond}); !errors.Is(err, registry.ErrPollTimeout) {
		t.Fatalf("expired offer poll err = %v", err)
	}
	if _, err := store.GetAnswer(ctx, offer.ID); !errors.Is(err, registry.ErrOfferNotFound) {
		t.Fatalf("expired offer answer err = %v", err)
	}

	clock.value = time.Date(2026, 5, 3, 10, 11, 0, 0, time.UTC)
	offer, err = store.SubmitOffer(ctx, registry.OfferInput{
		MachineID:  "mach_1",
		TerminalID: "term_1",
		TicketID:   "ticket_2",
		SDP:        minimalSDP("offer"),
	})
	if err != nil {
		t.Fatalf("submit second offer: %v", err)
	}
	if _, err := store.Poll(ctx, registry.PollInput{AgentID: "agent_1", MachineID: "mach_1", Timeout: time.Second}); err != nil {
		t.Fatalf("poll second offer: %v", err)
	}
	if _, err := store.SubmitAnswer(ctx, registry.AnswerInput{
		AgentID:   "agent_1",
		MachineID: "mach_1",
		OfferID:   offer.ID,
		SDP:       minimalSDP("answer"),
	}); err != nil {
		t.Fatalf("submit answer: %v", err)
	}
	clock.value = clock.value.Add(2 * time.Second)
	if removed := store.CleanupExpired(ctx); removed != 2 {
		t.Fatalf("removed stale offer and answer = %d, want 2", removed)
	}
	if _, err := store.GetAnswer(ctx, offer.ID); !errors.Is(err, registry.ErrOfferNotFound) {
		t.Fatalf("stale answer err = %v", err)
	}
}

func TestSignalingTTLIsEnforcedWithoutCleanup(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := &mutableClock{value: time.Date(2026, 5, 3, 10, 18, 0, 0, time.UTC)}
	store := registry.New(registry.Config{
		Clock:        clock,
		AgentTTL:     time.Hour,
		SignalingTTL: time.Second,
		Verifier:     allowAuthorityVerifier{},
	})
	if _, err := store.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	expiredQueued, err := store.SubmitOffer(ctx, registry.OfferInput{
		MachineID:  "mach_1",
		TerminalID: "term_1",
		TicketID:   "ticket_1",
		SDP:        minimalSDP("offer-1"),
	})
	if err != nil {
		t.Fatalf("submit expired queued offer: %v", err)
	}
	clock.value = clock.value.Add(2 * time.Second)
	if _, err := store.Poll(ctx, registry.PollInput{AgentID: "agent_1", MachineID: "mach_1", Timeout: time.Millisecond}); !errors.Is(err, registry.ErrPollTimeout) {
		t.Fatalf("expired queued poll err = %v", err)
	}
	if _, err := store.GetAnswer(ctx, expiredQueued.ID); !errors.Is(err, registry.ErrOfferNotFound) {
		t.Fatalf("expired queued answer lookup err = %v", err)
	}

	clock.value = time.Date(2026, 5, 3, 10, 19, 0, 0, time.UTC)
	expiredPolled, err := store.SubmitOffer(ctx, registry.OfferInput{
		MachineID:  "mach_1",
		TerminalID: "term_1",
		TicketID:   "ticket_2",
		SDP:        minimalSDP("offer-2"),
	})
	if err != nil {
		t.Fatalf("submit expired polled offer: %v", err)
	}
	if _, err := store.Poll(ctx, registry.PollInput{AgentID: "agent_1", MachineID: "mach_1", Timeout: time.Second}); err != nil {
		t.Fatalf("poll before expiry: %v", err)
	}
	clock.value = clock.value.Add(2 * time.Second)
	if _, err := store.SubmitAnswer(ctx, registry.AnswerInput{
		AgentID:   "agent_1",
		MachineID: "mach_1",
		OfferID:   expiredPolled.ID,
		SDP:       minimalSDP("answer-2"),
	}); !errors.Is(err, registry.ErrOfferNotFound) {
		t.Fatalf("expired offer answer err = %v", err)
	}

	clock.value = time.Date(2026, 5, 3, 10, 20, 0, 0, time.UTC)
	answered, err := store.SubmitOffer(ctx, registry.OfferInput{
		MachineID:  "mach_1",
		TerminalID: "term_1",
		TicketID:   "ticket_3",
		SDP:        minimalSDP("offer-3"),
	})
	if err != nil {
		t.Fatalf("submit answered offer: %v", err)
	}
	if _, err := store.Poll(ctx, registry.PollInput{AgentID: "agent_1", MachineID: "mach_1", Timeout: time.Second}); err != nil {
		t.Fatalf("poll answered offer: %v", err)
	}
	if _, err := store.SubmitAnswer(ctx, registry.AnswerInput{
		AgentID:   "agent_1",
		MachineID: "mach_1",
		OfferID:   answered.ID,
		SDP:       minimalSDP("answer-3"),
	}); err != nil {
		t.Fatalf("submit answer before expiry: %v", err)
	}
	clock.value = clock.value.Add(2 * time.Second)
	if _, err := store.GetAnswer(ctx, answered.ID); !errors.Is(err, registry.ErrOfferNotFound) {
		t.Fatalf("expired answer lookup err = %v", err)
	}
}

func TestCleanupRedeliversUnansweredOfferAfterAssignedAgentExpires(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := &mutableClock{value: time.Date(2026, 5, 3, 10, 12, 0, 0, time.UTC)}
	store := registry.New(registry.Config{
		Clock:        clock,
		AgentTTL:     time.Minute,
		SignalingTTL: time.Hour,
		Verifier:     allowAuthorityVerifier{},
	})
	if _, err := store.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register agent_1: %v", err)
	}
	if _, err := store.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_2"}); err != nil {
		t.Fatalf("register agent_2: %v", err)
	}
	offer, err := store.SubmitOffer(ctx, registry.OfferInput{
		MachineID:  "mach_1",
		TerminalID: "term_1",
		TicketID:   "ticket_1",
		SDP:        minimalSDP("offer"),
	})
	if err != nil {
		t.Fatalf("submit offer: %v", err)
	}
	first, err := store.Poll(ctx, registry.PollInput{AgentID: "agent_1", MachineID: "mach_1", Timeout: time.Second})
	if err != nil {
		t.Fatalf("first poll: %v", err)
	}
	if first.ID != offer.ID {
		t.Fatalf("first offer = %q, want %q", first.ID, offer.ID)
	}
	clock.value = clock.value.Add(time.Minute + time.Second)
	if _, err := store.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_2"}); err != nil {
		t.Fatalf("refresh agent_2: %v", err)
	}
	if removed := store.CleanupExpired(ctx); removed != 1 {
		t.Fatalf("removed expired assigned agent = %d, want 1", removed)
	}
	second, err := store.Poll(ctx, registry.PollInput{AgentID: "agent_2", MachineID: "mach_1", Timeout: time.Second})
	if err != nil {
		t.Fatalf("redelivery poll: %v", err)
	}
	if second.ID != offer.ID || second.AssignedAgentID != "agent_2" {
		t.Fatalf("redelivered offer = %+v", second)
	}
}

func TestDuplicateAnswersRejected(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := registry.New(registry.Config{
		Clock:    fixedClock(time.Date(2026, 5, 3, 10, 13, 0, 0, time.UTC)),
		AgentTTL: time.Minute,
		Verifier: allowAuthorityVerifier{},
	})
	if _, err := store.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	offer, err := store.SubmitOffer(ctx, registry.OfferInput{
		MachineID:  "mach_1",
		TerminalID: "term_1",
		TicketID:   "ticket_1",
		SDP:        minimalSDP("offer"),
	})
	if err != nil {
		t.Fatalf("submit offer: %v", err)
	}
	if _, err := store.Poll(ctx, registry.PollInput{AgentID: "agent_1", MachineID: "mach_1", Timeout: time.Second}); err != nil {
		t.Fatalf("poll: %v", err)
	}
	answer, err := store.SubmitAnswer(ctx, registry.AnswerInput{
		AgentID:   "agent_1",
		MachineID: "mach_1",
		OfferID:   offer.ID,
		SDP:       minimalSDP("answer-1"),
	})
	if err != nil {
		t.Fatalf("first answer: %v", err)
	}
	if _, err := store.SubmitAnswer(ctx, registry.AnswerInput{
		AgentID:   "agent_1",
		MachineID: "mach_1",
		OfferID:   offer.ID,
		SDP:       minimalSDP("answer-2"),
	}); !errors.Is(err, registry.ErrAnswerAlreadyExists) {
		t.Fatalf("duplicate answer err = %v", err)
	}
	loaded, err := store.GetAnswer(ctx, offer.ID)
	if err != nil {
		t.Fatalf("get answer: %v", err)
	}
	if loaded.ID != answer.ID || loaded.SDP != minimalSDP("answer-1") {
		t.Fatalf("duplicate answer overwrote first answer: %+v", loaded)
	}
}

func TestGetAgentClonesTerminals(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := registry.New(registry.Config{
		Clock:    fixedClock(time.Date(2026, 5, 3, 10, 14, 0, 0, time.UTC)),
		AgentTTL: time.Minute,
		Verifier: allowAuthorityVerifier{},
	})
	if _, err := store.Register(ctx, registry.RegisterInput{
		MachineID: "mach_1",
		AgentID:   "agent_1",
		Terminals: []registry.Terminal{{ID: "term_1", State: "running"}},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	first, ok := store.GetAgent("agent_1")
	if !ok {
		t.Fatal("agent missing")
	}
	first.Terminals[0].State = "mutated"
	second, ok := store.GetAgent("agent_1")
	if !ok {
		t.Fatal("agent missing after mutation")
	}
	if second.Terminals[0].State != "running" {
		t.Fatalf("registry terminal state mutated through returned slice: %+v", second.Terminals)
	}
}

func TestRegisterVerifierDoesNotBlockAgentReads(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	verifier := newBlockingAuthorityVerifier("mach_1/agent_blocked")
	store := registry.New(registry.Config{
		Clock:    fixedClock(time.Date(2026, 5, 3, 10, 15, 0, 0, time.UTC)),
		AgentTTL: time.Minute,
		Verifier: verifier,
	})
	if _, err := store.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_existing"}); err != nil {
		t.Fatalf("register existing: %v", err)
	}
	registerDone := make(chan error, 1)
	go func() {
		_, err := store.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_blocked"})
		registerDone <- err
	}()
	select {
	case <-verifier.entered:
	case <-time.After(time.Second):
		t.Fatal("blocking verifier was not called")
	}

	readDone := make(chan bool, 1)
	go func() {
		_, ok := store.GetAgent("agent_existing")
		readDone <- ok
	}()
	var readOK bool
	select {
	case readOK = <-readDone:
	case <-time.After(50 * time.Millisecond):
		verifier.release()
		if err := <-registerDone; err != nil {
			t.Fatalf("blocked register after release: %v", err)
		}
		<-readDone
		t.Fatal("GetAgent blocked behind a verifier call")
	}
	verifier.release()
	if err := <-registerDone; err != nil {
		t.Fatalf("blocked register after release: %v", err)
	}
	if !readOK {
		t.Fatal("existing agent missing during blocked registration")
	}
}

func TestRegisterRechecksRebindAfterVerifierReturns(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	verifier := newBlockingAuthorityVerifier("mach_1/agent_race")
	store := registry.New(registry.Config{
		Clock:    fixedClock(time.Date(2026, 5, 3, 10, 16, 0, 0, time.UTC)),
		AgentTTL: time.Minute,
		Verifier: verifier,
	})
	registerDone := make(chan error, 1)
	go func() {
		_, err := store.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_race"})
		registerDone <- err
	}()
	select {
	case <-verifier.entered:
	case <-time.After(time.Second):
		t.Fatal("blocking verifier was not called")
	}
	if _, err := store.Register(ctx, registry.RegisterInput{MachineID: "mach_2", AgentID: "agent_race"}); err != nil {
		t.Fatalf("competing register: %v", err)
	}
	verifier.release()
	if err := <-registerDone; !errors.Is(err, registry.ErrAgentRebound) {
		t.Fatalf("racing register err = %v", err)
	}
	agent, ok := store.GetAgent("agent_race")
	if !ok {
		t.Fatal("competing agent missing")
	}
	if agent.MachineID != "mach_2" {
		t.Fatalf("agent machine = %q, want mach_2", agent.MachineID)
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

type allowAuthorityVerifier struct{}

func (allowAuthorityVerifier) VerifyAgentRegistration(context.Context, registry.AgentRegistration) error {
	return nil
}

func (allowAuthorityVerifier) VerifyOfferTicket(context.Context, registry.OfferTicket) error {
	return nil
}

type fakeAuthorityVerifier struct {
	register      map[string]error
	offer         map[string]error
	registerCalls []string
	offerCalls    []string
}

type blockingAuthorityVerifier struct {
	blockKey string
	entered  chan struct{}
	released chan struct{}
	once     sync.Once
}

func newBlockingAuthorityVerifier(blockKey string) *blockingAuthorityVerifier {
	return &blockingAuthorityVerifier{
		blockKey: blockKey,
		entered:  make(chan struct{}),
		released: make(chan struct{}),
	}
}

func (f *blockingAuthorityVerifier) VerifyAgentRegistration(ctx context.Context, req registry.AgentRegistration) error {
	if req.MachineID+"/"+req.AgentID != f.blockKey {
		return nil
	}
	f.once.Do(func() { close(f.entered) })
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-f.released:
		return nil
	}
}

func (f *blockingAuthorityVerifier) VerifyOfferTicket(context.Context, registry.OfferTicket) error {
	return nil
}

func (f *blockingAuthorityVerifier) release() {
	select {
	case <-f.released:
	default:
		close(f.released)
	}
}

func (f *fakeAuthorityVerifier) VerifyAgentRegistration(_ context.Context, req registry.AgentRegistration) error {
	key := req.MachineID + "/" + req.AgentID
	f.registerCalls = append(f.registerCalls, key)
	if err, ok := f.register[key]; ok {
		return err
	}
	return registry.ErrUnauthorizedAgent
}

func (f *fakeAuthorityVerifier) VerifyOfferTicket(_ context.Context, req registry.OfferTicket) error {
	key := req.MachineID + "/" + req.TerminalID + "/" + req.TicketID
	f.offerCalls = append(f.offerCalls, key)
	if err, ok := f.offer[key]; ok {
		return err
	}
	return registry.ErrUnauthorizedTicket
}
